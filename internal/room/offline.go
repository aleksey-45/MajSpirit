package room

// offline.go —— 断线与保座（协议 §4.5）。
//
// 一条规则贯穿这个文件：**掉线不等于离开**。座位留着 120 秒，期间随时回来都还是
// 原座；超时才释放。而"掉线"这件事必须让另外三家看见，否则他们只看到一个不动的
// 座位，不知道出了什么事。

import (
	"time"

	"github.com/aleksey-45/majspirit/internal/protocol"
)

// defaultOfflineHold 是断线后保留座位的时长（协议 §4.5）。
//
// 写成常量而不是配置项：同一场对局里四个人面对不同的保座窗口，出了争议没法查证。
const defaultOfflineHold = 120 * time.Second

func (r *Room) doDisconnected(userID int64) error {
	s := r.seatOf(userID)
	if s < 0 {
		return ErrNotInRoom
	}
	if !r.seats[s].connected {
		return nil
	}

	r.seats[s].connected = false
	r.seats[s].offlineAt = time.Now()
	r.log.Info("座位掉线，保留", "seat", s, "userId", userID)
	// 掉线要让另外三家看见（协议 §4.5）：不然他们只看到一个不动的座位，
	// 不知道出了什么事。
	r.broadcast(fact{typ: protocol.EventRoomSeats, data: roomSeats{Seats: r.seatStates()}})
	return nil
}

func (r *Room) doReconnected(userID int64) error {
	s := r.seatOf(userID)
	if s < 0 {
		return ErrNotInRoom
	}
	return r.attach(s)
}

// attach 把座位标回在线。重复调用无害。
func (r *Room) attach(s int) error {
	if r.seats[s].connected {
		return nil
	}
	r.seats[s].connected = true
	r.seats[s].offlineAt = time.Time{}
	r.log.Info("座位重连", "seat", s, "userId", r.seats[s].user.ID)
	r.broadcast(fact{typ: protocol.EventRoomSeats, data: roomSeats{Seats: r.seatStates()}})
	return nil
}

// evictExpired 处理保座超时：**关掉整个房间**。
//
// 为什么不是"释放座位、剩下的三个人继续"（2026-10-13 改的）：麻将少一个人就打不了，
// 而我们还没有「中断对局 / 托管 / CPU 替打」那一套。留三个人守着一个打不了的房间，
// 比明确告诉他们"这局散了"更糟。
//
// 以后要做掉线续局时，改的是这里 —— 那时该做的是**冻结对局等人回来**，
// 而不是把座位让给别人。
func (r *Room) evictExpired() {
	now := time.Now()
	for i, s := range r.seats {
		if s == nil || s.connected || now.Before(s.offlineAt.Add(r.tune.hold)) {
			continue
		}
		r.log.Info("保座超时，关闭房间", "seat", i, "userId", s.user.ID)
		// 先说清是谁超时了，再说房间没了 —— 顺序反了的话，剩下的人只知道
		// "房间关了"，不知道是因为谁。
		r.broadcast(fact{typ: protocol.EventRoomSeatVacated, data: roomSeatVacated{
			Seat:   i,
			Reason: "timeout",
		}})
		r.closeRoom(reasonPlayerTimeout)
		return
	}
}

// reasonPlayerTimeout 是"有人掉线且没回来"这条关房理由（协议 §6.2 的 room.closed）。
const reasonPlayerTimeout = "playerTimeout"

// earliestOfflineDeadline 返回最早的保座到期时刻。
func (r *Room) earliestOfflineDeadline() (time.Time, bool) {
	var best time.Time
	found := false
	for _, s := range r.seats {
		if s == nil || s.connected {
			continue
		}
		d := s.offlineAt.Add(r.tune.hold)
		if !found || d.Before(best) {
			best, found = d, true
		}
	}
	return best, found
}
