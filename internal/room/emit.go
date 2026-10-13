package room

// emit.go —— 把房间里发生的事实发出去。
//
// 发号不在这里：一条事件流由房间级的一个 protocol.Emitter 统一发号，而**对局用的是
// 同一个**（开局时一起交给它）。房间与对局各发各的号，客户端就会在一条连接上看到
// 两个号系交错 —— 开局那一刻必出一个"洞"，中途有人掉线再插一条又是洞，
// 于是它会不停地拉快照。分界线不是"谁在发"，是"哪个房间"。

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

// emit 发一条事件的全部收件人（一个号）并记下投递失败的那几个。
//
// 一条事件必须一次发完：分两次调用就是两个号，四个人的流从此错开。
//
// 发送失败绝不进规则层：发不出去一条 room.seats 不是 ILLEGAL_ACTION，
// 也不影响房间里任何裁决。ErrSlowConsumer 说明那个对端已经不读了 ——
// 那条连接自己的写超时会把连接关掉，随后走 Disconnected，这里什么都不用做。
//
// 记日志只带 userId：一批消息里每个收件人各占一项，而座位号要现查（对局那边
// 调的是同一个 Emitter，它手上没有房间的座位表）。
func (r *Room) emit(msgs ...protocol.Message) {
	for i, err := range r.stream.Emit(msgs) {
		if err != nil {
			r.log.Info("推送失败", "user", msgs[i].UserID, "type", string(msgs[i].Env.Type), "err", err)
		}
	}
}
