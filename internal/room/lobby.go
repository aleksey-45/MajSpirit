package room

// lobby.go —— 大厅：谁坐在哪、进出的规矩、什么时候能开局。
// 对局开始之前的一切都在这里；对局本身（手牌、牌河、副露）属于 Game。

import (
	"time"

	"github.com/aleksey-45/majspirit/internal/protocol"
	"github.com/aleksey-45/majspirit/internal/user"
)

// seat 是大厅里的一个座位。
//
// 对局开始之后的手牌、牌河、副露**不属于这里** —— 那些是 Game 的状态（阶段四 §4.3）。
// 大厅与牌桌是两件事，混在一起会让"房间还在等人"和"这一手谁打"互相干扰。
type seat struct {
	user      user.User
	ready     bool
	connected bool

	// offlineAt 是掉线的时刻。connected 为 true 时无意义。
	offlineAt time.Time
}

func (r *Room) doJoin(u user.User) error {
	if r.phase != phaseWaiting {
		return ErrPlaying
	}
	// 已经在房间里 → 当成重连（协议 §4.5：断线重连回到原座位）。
	if s := r.seatOf(u.ID); s >= 0 {
		return r.attach(s)
	}
	n := r.firstFreeSeat()
	if n < 0 {
		return ErrRoomFull
	}

	r.seats[n] = &seat{user: u, connected: true}
	r.emitTo(n, fact{typ: protocol.EventRoomJoined, data: roomJoined{
		RoomID: r.id,
		MySeat: n,
		Seats:  r.seatStates(),
	}})
	r.broadcast(fact{typ: protocol.EventRoomSeats, data: roomSeats{Seats: r.seatStates()}})
	return nil
}

func (r *Room) doLeave(userID int64) error {
	s := r.seatOf(userID)
	if s < 0 {
		return ErrNotInRoom
	}

	r.seats[s] = nil
	r.broadcast(fact{typ: protocol.EventRoomLeft, data: roomLeft{Seat: s}})
	// 房主走了要把房主身份交出去，而 isOwner 只在 room.seats 里，所以结构变化
	// 一律补一条全量座位表 —— 客户端不必自己推。
	r.reowner()
	r.broadcast(fact{typ: protocol.EventRoomSeats, data: roomSeats{Seats: r.seatStates()}})
	r.destroyIfEmpty()
	return nil
}

func (r *Room) doReady(userID int64, ready bool) error {
	if r.phase != phaseWaiting {
		return ErrPlaying
	}
	s := r.seatOf(userID)
	if s < 0 {
		return ErrNotInRoom
	}
	if r.seats[s].ready == ready {
		return nil // 重复设置同一个值不算错，客户端重发是常事
	}

	r.seats[s].ready = ready
	r.broadcast(fact{typ: protocol.EventRoomReadyChanged, data: roomReadyChanged{Seat: s, Ready: ready}})
	return nil
}

func (r *Room) doStart(userID int64) error {
	if r.phase != phaseWaiting {
		return ErrPlaying
	}
	if userID != r.ownerID {
		return ErrNotOwner
	}
	if r.occupied() != 4 {
		return ErrNotEnough
	}
	if !r.allReady() {
		return ErrNotReady
	}

	r.phase = phasePlaying
	r.log.Info("开局", "players", 4)
	// 阶段四 §4.3 的接缝就在这里：下面要把对局跑起来（洗牌、发牌、game.started、
	// 然后进入带等待的状态机）。现在只切状态 —— 别处不许自己造一个"开局"。
	return nil
}

// reowner 把房主身份交给编号最小的在座者。只改字段，不发消息 ——
// 调用方随后会广播一条全量座位表。
func (r *Room) reowner() {
	if s := r.seatOf(r.ownerID); s >= 0 {
		return
	}
	for _, s := range r.seats {
		if s != nil {
			r.ownerID = s.user.ID
			return
		}
	}
}

// ── 状态查询（只在 run() 的 goroutine 里调）────────────────────────────

func (r *Room) seatOf(userID int64) int {
	for i, s := range r.seats {
		if s != nil && s.user.ID == userID {
			return i
		}
	}
	return -1
}

func (r *Room) firstFreeSeat() int {
	for i, s := range r.seats {
		if s == nil {
			return i
		}
	}
	return -1
}

func (r *Room) occupied() int {
	n := 0
	for _, s := range r.seats {
		if s != nil {
			n++
		}
	}
	return n
}

func (r *Room) allReady() bool {
	for _, s := range r.seats {
		if s == nil || !s.ready {
			return false
		}
	}
	return true
}

// seatStates 返回四个座位，空座位是 null。
//
// 用 null 而不是"省略这一项"或"userId = 0"：空的座位和"一个 id 恰好是 0 的人"
// 在客户端是两件事，而 0 不是合法的用户 id（自增从 1 开始）。
func (r *Room) seatStates() []*seatState {
	out := make([]*seatState, len(r.seats))
	for i, s := range r.seats {
		if s == nil {
			continue
		}
		out[i] = &seatState{
			Seat:      i,
			UserID:    s.user.ID,
			Username:  s.user.Username,
			Ready:     s.ready,
			Connected: s.connected,
			IsOwner:   s.user.ID == r.ownerID,
		}
	}
	return out
}
