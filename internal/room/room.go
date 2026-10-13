package room

// room.go —— 房间这个 actor 的骨架：怎么起、怎么停、收哪些指令、怎么往里投。
//
// 座位与开局在 lobby.go，断线保座在 offline.go，往外发消息在 emit.go。

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aleksey-45/majspirit/internal/protocol"
	"github.com/aleksey-45/majspirit/internal/user"
)

type phase uint8

const (
	phaseWaiting phase = iota // 等人、准备
	phasePlaying              // 对局中
)

type cmdKind uint8

const (
	cmdJoin cmdKind = iota
	cmdLeave
	cmdReady
	cmdStart
	cmdDisconnected
	cmdReconnected
)

type command struct {
	kind  cmdKind
	user  user.User
	id    int64
	ready bool
	reply chan error
}

// Room 是一个房间。
//
// **它的全部状态只被 run() 那一个 goroutine 碰**：外部不直接读写任何字段，
// 只往 commands 投指令。于是这个包里一把锁都不需要 —— 不存在忘记加锁、
// 加锁顺序错、死锁，也不存在"一次操作做到一半被别人插进来"。
type Room struct {
	id     string
	hub    *Hub
	notify Notifier
	log    *slog.Logger

	commands chan command
	closing  chan struct{} // 请求关闭（Close 或房间自己销毁）
	stopped  chan struct{} // run 退出时关闭
	once     sync.Once

	// closed 在 shutdown 的第一行就置上。光靠 select 里的 closing 不够：
	// "房间刚销毁"和"还能投指令"之间会留一个随机窗口（run 的 select 两支都就绪时
	// 随机挑），于是同一条指令有时成功有时报 ErrClosed。
	closed atomic.Bool

	// hold 默认是 offlineHold。做成字段是为了测试不必真等 2 分钟。
	hold time.Duration

	// ↓↓↓ 以下字段只在 run() 的 goroutine 里读写 ↓↓↓

	seats   [4]*seat
	phase   phase
	seq     int
	ownerID int64
}

// newRoom 建一个房间并把 owner 放进 seat 0。它**在起 goroutine 之前**就发掉了
// room.created —— 那一刻还没有并发，谁碰都安全。
//
// hold 是保座时长，生产传 offlineHold；测试传一个很短的，并且必须在这里传进去 ——
// 房间起来之后再改这个字段就是数据竞争。
func newRoom(id string, owner user.User, hub *Hub, hold time.Duration) *Room {
	r := &Room{
		id:       id,
		hub:      hub,
		notify:   hub.notify,
		log:      slog.Default().With("room", id),
		commands: make(chan command),
		closing:  make(chan struct{}),
		stopped:  make(chan struct{}),
		hold:     hold,
	}
	r.seats[0] = &seat{user: owner, connected: true}
	r.ownerID = owner.ID
	r.emitTo(0, fact{typ: protocol.EventRoomCreated, data: roomCreated{RoomID: id, Seat: 0}})

	go r.run()
	return r
}

// ID 返回房间号。
func (r *Room) ID() string { return r.id }

func (r *Room) run() {
	defer close(r.stopped)

	for {
		// 每一轮重算"最近一个保座到期时刻"。用一个临时 timer 而不是常驻的
		// time.Timer + Reset：Reset 那套要求先 Stop 再排空 channel，四份状态
		// 乘在一起很容易写错，而这里的开销只是一次 NewTimer。
		var timer *time.Timer
		var expired <-chan time.Time
		if d, ok := r.earliestOfflineDeadline(); ok {
			timer = time.NewTimer(time.Until(d))
			expired = timer.C
		}

		select {
		case c := <-r.commands:
			if timer != nil {
				timer.Stop()
			}
			r.handle(c)
		case <-expired:
			r.evictExpired()
		case <-r.closing:
			return
		}
	}
}

// ── 外部入口。全部是"投一条指令 + 等回复" ────────────────────────────────

func (r *Room) Join(u user.User) error { return r.call(command{kind: cmdJoin, user: u}) }

func (r *Room) Leave(userID int64) error { return r.call(command{kind: cmdLeave, id: userID}) }

func (r *Room) SetReady(userID int64, ready bool) error {
	return r.call(command{kind: cmdReady, id: userID, ready: ready})
}

func (r *Room) Start(userID int64) error { return r.call(command{kind: cmdStart, id: userID}) }

// Disconnected 报告某人的连接断了。座位留着（协议 §4.5 的 120 秒保座窗口）。
func (r *Room) Disconnected(userID int64) error {
	return r.call(command{kind: cmdDisconnected, id: userID})
}

// Reconnected 报告某人连回来了。
func (r *Room) Reconnected(userID int64) error {
	return r.call(command{kind: cmdReconnected, id: userID})
}

// Close 停掉房间并等它的 goroutine 退出。可以重复调用。
func (r *Room) Close() {
	r.shutdown()
	<-r.stopped
}

// call 投一条指令并等回复。
//
// commands 是**无缓冲**的：一次投递只有被 run() 接住才算成功。房间退出后
// commands **不会**被关闭（关闭它才是灾难 —— 往已关闭的 channel 发送会 panic），
// 靠 stopped 判断。
func (r *Room) call(c command) error {
	if r.closed.Load() {
		return ErrClosed
	}
	c.reply = make(chan error, 1)
	select {
	case r.commands <- c:
		return <-c.reply
	case <-r.stopped:
		return ErrClosed
	}
}

func (r *Room) shutdown() {
	r.once.Do(func() {
		r.closed.Store(true)
		close(r.closing)
	})
}

// ── 指令处理。以下全部在 run() 的 goroutine 里跑 ──────────────────────────

func (r *Room) handle(c command) {
	var err error
	switch c.kind {
	case cmdJoin:
		err = r.doJoin(c.user)
	case cmdLeave:
		err = r.doLeave(c.id)
	case cmdReady:
		err = r.doReady(c.id, c.ready)
	case cmdStart:
		err = r.doStart(c.id)
	case cmdDisconnected:
		err = r.doDisconnected(c.id)
	case cmdReconnected:
		err = r.doReconnected(c.id)
	}
	c.reply <- err
}

// destroyIfEmpty 房间空了就摘掉自己。
//
// 先摘表再关自己：反过来的话，中间那一瞬还有人在 Hub 里查到它，然后往一个
// 正在退出的房间投指令。
func (r *Room) destroyIfEmpty() {
	if r.occupied() > 0 {
		return
	}
	r.log.Info("房间空了，销毁")
	r.hub.remove(r.id)
	r.shutdown()
}
