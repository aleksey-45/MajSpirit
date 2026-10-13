package room

// room.go —— 房间这个 actor 的骨架：怎么起、怎么停、收哪些指令、怎么往里投。
//
// 座位与开局在 lobby.go，断线保座在 offline.go，往外发消息在 emit.go。

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aleksey-45/majspirit/internal/game"
	"github.com/aleksey-45/majspirit/internal/mahjong"
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

	// cmdGameEnded 是对局那边打完之后投回来的唯一一条指令。
	// 对局指令（game.discard / game.win）**不经过这里** —— 它们由 ws 层直接投给
	// internal/game 的 actor，房间不必替它转发（转发只是多一跳）。
	cmdGameEnded
)

type command struct {
	kind  cmdKind
	user  user.User
	id    int64
	ready bool
	reply chan error
}

// tuning 是房间的几个可注入参数。
//
// 做成**构造参数**而不是可写字段：房间起来之后 run() 的 goroutine 正在读它们，
// 从外面改就是数据竞争（`-race` 会当场抓住）。测试要的只是"不用真等 120 秒"，
// 以及"这副牌我说了算"。
type tuning struct {
	hold time.Duration // 断线保座窗口（协议 §4.5）

	// 对局那几个参数：房间自己不读它们，只在开局时原样交给 internal/game。
	// 留在这里是为了让测试从**一个入口**把整局调快、把牌摆好。
	turnLimit time.Duration
	turnBase  time.Duration
	rounds    int
	gameID    int64         // 0 表示用进程内自增
	seed      *[32]byte     // nil 表示用 crypto/rand
	wall      *mahjong.Wall // 非 nil 时第一小局直接用这副牌山（测试摆牌用）
}

// productionTuning 是生产口径。
//
// 对局那几个字段留零值：**它们的默认值由 internal/game 定**（回合时限、局数都是
// 对局的规矩）。在这里再抄一份的话，协议 §9.1 的数字就有两处可改，早晚漂。
func productionTuning() tuning {
	return tuning{hold: defaultOfflineHold}
}

// Room 是一个房间。
//
// **它的全部状态只被 run() 那一个 goroutine 碰**：外部不直接读写任何字段，
// 只往 commands 投指令。于是这个包里一把锁都不需要 —— 不存在忘记加锁、
// 加锁顺序错、死锁，也不存在"一次操作做到一半被别人插进来"。
type Room struct {
	id  string
	hub *Hub
	log *slog.Logger

	// stream 是这个房间的事件流：房间与它开的对局共用这一个（协议 §1.4）。
	stream *protocol.Emitter

	commands chan command
	closing  chan struct{} // 请求关闭（Close 或房间自己销毁）
	stopped  chan struct{} // run 退出时关闭
	once     sync.Once

	// closed 在 shutdown 的第一行就置上。光靠 select 里的 closing 不够：
	// "房间刚销毁"和"还能投指令"之间会留一个随机窗口（run 的 select 两支都就绪时
	// 随机挑），于是同一条指令有时成功有时报 ErrClosed。
	closed atomic.Bool

	// tune 在构造后就只读，所以可以从任何 goroutine 看。
	tune tuning

	// games 是当前那一场对局。对局是另一个 actor，有自己的 goroutine，
	// 所以这里用原子指针：run() 里存/清它，ws 层从外面读它去投 game.* 指令。
	games atomic.Pointer[game.Game]

	// who 是"这个房间里有哪些人"的只读快照，同样给外面看（Hub.ByUser）。
	// 座位表归 run() 独占，从外面读它就是竞态，所以座位一变就整份换掉。
	who atomic.Pointer[map[int64]struct{}]

	// ↓↓↓ 以下字段只在 run() 的 goroutine 里读写 ↓↓↓

	seats   [4]*seat
	phase   phase
	ownerID int64
}

// Game 返回当前那一场对局；不在对局中返回 nil。
//
// ws 层拿它去投 game.* 指令 —— 那些指令不经过房间（见 internal/game 的 doc.go）。
func (r *Room) Game() *game.Game { return r.games.Load() }

// HasUser 报告这个人在不在这个房间。任何 goroutine 都可以调（只读快照）。
//
// 给 Hub.ByUser 用：网关靠"这个人在哪个房间"投对局指令、判断重连。
func (r *Room) HasUser(userID int64) bool {
	m := r.who.Load()
	if m == nil {
		return false
	}
	_, ok := (*m)[userID]
	return ok
}

// newRoom 建一个房间并把 owner 放进 seat 0。它**在起 goroutine 之前**就发掉了
// room.created —— 那一刻还没有并发，谁碰都安全。
//
// tune 必须在**这里**传进来：房间起来之后再改那些字段就是数据竞争。
func newRoom(id string, owner user.User, hub *Hub, tune tuning) *Room {
	r := &Room{
		id:       id,
		hub:      hub,
		log:      slog.Default().With("room", id),
		stream:   protocol.NewEmitter(hub.send),
		commands: make(chan command),
		closing:  make(chan struct{}),
		stopped:  make(chan struct{}),
		tune:     tune,
	}
	r.seats[0] = &seat{user: owner, connected: true}
	r.ownerID = owner.ID
	r.refreshWho()
	r.emitTo(0, fact{typ: protocol.EventRoomCreated, data: roomCreated{RoomID: id, Seat: 0}})

	go r.run()
	return r
}

// ID 返回房间号。
func (r *Room) ID() string { return r.id }

func (r *Room) run() {
	defer close(r.stopped)

	for {
		// 每一轮重算"最近的等待什么时候到期"。用一个临时 timer 而不是常驻的
		// time.Timer + Reset：Reset 那套要求先 Stop 再排空 channel，状态乘起来
		// 很容易写错，而这里的开销只是一次 NewTimer。
		var timer *time.Timer
		var expired <-chan time.Time
		if d, ok := r.nextDeadline(); ok {
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
			r.onDeadline()
		case <-r.closing:
			// 退出前带走对局（见 stopGame）。
			r.stopGame()
			return
		}
	}
}

// nextDeadline 返回最近一个"该我处理"的时刻。
//
// 现在只剩保座一条：对局的回合/窗口到期由**对局自己的** actor 管（它有自己的
// select 循环）。两类等待挤进一个循环是拆分之前的事 —— 那时对局还是房间的字段。
func (r *Room) nextDeadline() (time.Time, bool) {
	return r.earliestOfflineDeadline()
}

// onDeadline 处理到期的等待。
func (r *Room) onDeadline() {
	r.evictExpired()
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
// commands 无缓冲：一次投递只有被 run() 接住才算成功。房间退出后 commands 不会被
// 关闭（关闭它才是灾难 —— 往已关闭的 channel 发送会 panic），靠 stopped 判断。
func (r *Room) call(c command) error {
	if r.closed.Load() {
		return ErrClosed
	}
	c.reply = make(chan error, 1)
	select {
	case r.commands <- c:
		return <-c.reply
	case <-r.closing:
		// 与 stopGame 互等的入口：对局的 goroutine 可能正卡在 postGameEnded
		// 的这一次 call 上，而 run 正等它退出。
		return ErrClosed
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
	case cmdGameEnded:
		r.onGameEnded()
	}
	c.reply <- err
}

// startGame 把四个座位交给 internal/game，开一场。
//
// 房间只在两个时刻碰对局：开这一下，以及 OnEnd 回调那一下。之后就各跑各的
// goroutine —— 房间不必懂"这一手谁打"。
func (r *Room) startGame() {
	players := [4]game.Player{}
	for seat, s := range r.seats {
		players[seat] = game.Player{UserID: s.user.ID, Username: s.user.Username}
	}

	g := game.New(players, r.stream, game.Options{
		Rounds:    r.tune.rounds,
		TurnBase:  r.tune.turnBase,
		TurnLimit: r.tune.turnLimit,
		GameID:    r.tune.gameID,
		Seed:      r.tune.seed,
		Wall:      r.tune.wall,
		OnEnd:     r.postGameEnded,
	}, r.log)
	r.games.Store(g)
}

// postGameEnded 由对局在自己的 goroutine 里调。
//
// 只能投一条指令，不能碰房间状态 —— 那是另一个 goroutine 的地盘。
func (r *Room) postGameEnded() {
	_ = r.call(command{kind: cmdGameEnded})
}

// onGameEnded 一场打完了：回大厅，座位和人留着可以再来一场。
func (r *Room) onGameEnded() {
	r.games.Store(nil)
	r.phase = phaseWaiting
	for _, s := range r.seats {
		if s != nil {
			s.ready = false
		}
	}
	r.log.Info("整场结束，回大厅")
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

// stopGame 带走手上那一场对局。
//
// 正常打完由对局自己收工（game.endGame 里的自退）；这里是"房间先没了"的唯一
// 补刀点。不会和 postGameEnded 互等：call 在 closing 关上后立刻返回 ErrClosed。
func (r *Room) stopGame() {
	if g := r.games.Swap(nil); g != nil {
		g.Close()
	}
}

// closeRoom 告诉房间里还剩下的人"这里没了"，然后销毁。
//
// 先广播再摘表：摘完之后没人在这个房间里了，剩下的人唯一能得到的解释就是这条事件。
func (r *Room) closeRoom(reason string) {
	r.log.Info("关闭房间", "reason", reason)
	r.broadcast(fact{typ: protocol.EventRoomClosed, data: roomClosed{
		RoomID: r.id,
		Reason: reason,
	}})
	r.hub.remove(r.id)
	r.shutdown()
}
