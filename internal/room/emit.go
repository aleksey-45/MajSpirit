package room

// emit.go —— 把房间里发生的事实发出去。发号见 protocol.Emitter。

import "github.com/aleksey-45/majspirit/internal/protocol"

// emitTo 只发给一个座位。调用方保证这个座位有人 —— 现在唯一的调用点就是刚坐下
// 之后那两行，隔三行远。
func (r *Room) emitTo(seat int, f fact) {
	r.emit(protocol.Message{UserID: r.seats[seat].user.ID, Env: project(seat, f)})
}

// broadcast 发给所有在座的人。
func (r *Room) broadcast(f fact) {
	msgs := make([]protocol.Message, 0, len(r.seats))
	for i, s := range r.seats {
		if s == nil {
			continue
		}
		msgs = append(msgs, protocol.Message{UserID: s.user.ID, Env: project(i, f)})
	}
	r.emit(msgs...)
}

// emit 发一条事件的全部收件人，并记下投递失败的那几个。
//
// 一条事件必须一次发完：分两次调用就是两条流上的两个位置。
//
// 发送失败不进规则层 —— 发不出去一条 room.seats 不是 ILLEGAL_ACTION，也不影响
// 任何裁决。日志只带 userId：一批里每个收件人各占一项，而座位要现查。
func (r *Room) emit(msgs ...protocol.Message) {
	for i, err := range r.stream.Emit(msgs) {
		if err != nil {
			r.log.Info("推送失败", "user", msgs[i].UserID, "type", string(msgs[i].Env.Type), "err", err)
		}
	}
}
