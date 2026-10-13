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

// offlineHold 是断线后保留座位的时长（协议 §4.5）。
//
// 写成常量而不是配置项：同一场对局里四个人面对不同的保座窗口，出了争议没法查证。
const offlineHold = 120 * time.Second

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

// evictExpired 释放保座超时的座位。
func (r *Room) evictExpired() {
	now := time.Now()
	changed := false

	for i, s := range r.seats {
		if s == nil || s.connected || now.Before(s.offlineAt.Add(r.hold)) {
			continue
		}
		r.log.Info("保座超时，释放座位", "seat", i, "userId", s.user.ID)
		r.seats[i] = nil
		changed = true
		r.broadcast(fact{typ: protocol.EventRoomSeatVacated, data: roomSeatVacated{
			Seat:   i,
			Reason: "timeout",
		}})
	}

	if !changed {
		return
	}
	r.reowner()
	r.broadcast(fact{typ: protocol.EventRoomSeats, data: roomSeats{Seats: r.seatStates()}})
	r.destroyIfEmpty()
}

// earliestOfflineDeadline 返回最早的保座到期时刻。
func (r *Room) earliestOfflineDeadline() (time.Time, bool) {
	var best time.Time
	found := false
	for _, s := range r.seats {
		if s == nil || s.connected {
			continue
		}
		d := s.offlineAt.Add(r.hold)
		if !found || d.Before(best) {
			best, found = d, true
		}
	}
	return best, found
}
