package room

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/aleksey-45/majspirit/internal/protocol"
	"github.com/aleksey-45/majspirit/internal/user"
)

// hubOp 是对房间表的一次操作。表本身作为参数传给闭包，于是它**永远不会**离开
// run() 那个 goroutine —— 想从外面碰它就只能再投一个 op 进来。
type hubOp func(rooms map[string]*Room)

// Hub 是房间的注册表：创建、查找、摘除。
//
// 它自己也是一个 actor（一个 goroutine 独占房间表）。理由和 Room 一样：
// 创建、查找、销毁三处都碰这张表，串行化比在三处各想一遍锁简单 ——
// 而且"房间销毁时把它从表里摘掉"这件事天然发生在同一个 goroutine 里。
//
// notify 是它下面所有房间共用的投递函数（就是 internal/ws 的注册表）。
//
// 房间不各存一份投递方，而是各自拿它造一个 Emitter：投递是共用的，序号流不是 ——
// 序号必须**按房间**分开（协议 §1.4）。
type Hub struct {
	ops     chan hubOp
	closing chan struct{}
	stopped chan struct{}
	once    sync.Once

	// closed 在 Close 的第一行就置上。光靠 select 里的 closing 不够：
	// 关闭与"还能受理"之间会留一个随机窗口（run 的 select 两支都就绪时随机挑），
	// 于是同一个 Create 有时成功有时报错 —— 那种偶发最难查。
	closed atomic.Bool

	log  *slog.Logger
	send protocol.Sender
}

// NewHub 起一个 Hub。log 为 nil 时用默认 logger。
func NewHub(send protocol.Sender, log *slog.Logger) *Hub {
	if log == nil {
		log = slog.Default()
	}
	h := &Hub{
		ops:     make(chan hubOp),
		closing: make(chan struct{}),
		stopped: make(chan struct{}),
		log:     log,
		send:    send,
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	defer close(h.stopped)
	rooms := make(map[string]*Room)

	for {
		select {
		case op := <-h.ops:
			op(rooms)
		case <-h.closing:
			// 先把 closing 关掉再逐个关房间：房间退出时可能要回来摘自己，
			// 那时 post 会因为 closing 已关而立刻返回，不会卡住。
			for _, r := range rooms {
				r.Close()
			}
			return
		}
	}
}

// Create 新建一个房间，owner 占 seat 0。
func (h *Hub) Create(owner user.User) (*Room, error) {
	type result struct {
		room *Room
		err  error
	}
	reply := make(chan result, 1)

	ok := h.post(func(rooms map[string]*Room) {
		id, err := newRoomID()
		if err != nil {
			reply <- result{err: err}
			return
		}
		// 撞号在这里重试：6 位 32 进制有 10 亿种，真撞上说明运气不好，不是错误。
		for rooms[id] != nil {
			if id, err = newRoomID(); err != nil {
				reply <- result{err: err}
				return
			}
		}
		r := newRoom(id, owner, h, productionTuning())
		rooms[id] = r
		reply <- result{room: r}
	})
	if !ok {
		return nil, ErrHubClosed
	}

	res := <-reply
	return res.room, res.err
}

// Lookup 按 ID 找房间。
func (h *Hub) Lookup(id string) (*Room, bool) {
	reply := make(chan *Room, 1)
	if !h.post(func(rooms map[string]*Room) { reply <- rooms[id] }) {
		return nil, false
	}
	r := <-reply
	return r, r != nil
}

// Len 是当前房间数，给测试和诊断用。
func (h *Hub) Len() int {
	reply := make(chan int, 1)
	if !h.post(func(rooms map[string]*Room) { reply <- len(rooms) }) {
		return 0
	}
	return <-reply
}

// ByUser 找这个人坐在哪个房间。
//
// 扫一遍房间表，而不是维护一张 userId → room 的索引：座位变化发生在房间自己的
// goroutine 里，Hub 收不到通知，要维护索引就得让每个座位变化都回投一条指令 ——
// 漏一处就是"把指令投进他已经离开的房间"，而这种错不报错、只表现为行为诡异。
//
// 扫的是每个房间的只读快照（Room.HasUser），不是房间的座位表 —— 那张表外面读不得。
func (h *Hub) ByUser(userID int64) (*Room, bool) {
	reply := make(chan *Room, 1)
	if !h.post(func(rooms map[string]*Room) {
		var found *Room
		for _, r := range rooms {
			if r.HasUser(userID) {
				found = r
				break
			}
		}
		reply <- found
	}) {
		return nil, false
	}
	r := <-reply
	return r, r != nil
}

// Close 关掉 Hub 和它下面所有房间。
func (h *Hub) Close() {
	h.once.Do(func() {
		h.closed.Store(true)
		close(h.closing)
	})
	<-h.stopped
}

// remove 由房间自己调（房间空了或被销毁时）。
func (h *Hub) remove(id string) {
	h.post(func(rooms map[string]*Room) { delete(rooms, id) })
}

// post 投一个 op。Hub 已经关了时直接返回 false，不阻塞调用方 ——
// 关闭路径上最忌讳的就是"叫别人别来了"之后自己还在等。
func (h *Hub) post(op hubOp) bool {
	if h.closed.Load() {
		return false
	}
	select {
	case h.ops <- op:
		return true
	case <-h.closing:
		return false
	}
}

var (
	// ErrHubClosed 表示 Hub 已经关闭。
	ErrHubClosed = errors.New("房间注册表已关闭")
)
