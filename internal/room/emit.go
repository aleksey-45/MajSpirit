package room

// emit.go —— 把房间里发生的事实变成发出去的信封。
//
// seq 的分配规则在这里：它是**房间级共享的一条流**，同一条事件四个人拿到同一个
// 序号，不管谁收没收到。按座位各发一号的话，客户端靠"序号有洞"检测丢消息的能力
// 就没了（协议 §1.4）。

import "github.com/aleksey-45/majspirit/internal/protocol"

// nextSeq 取下一条事件序号。seq 从 1 开始，所以 0 永远不是合法序号。
func (r *Room) nextSeq() int {
	r.seq++
	return r.seq
}

// emitTo 只发给一个座位。调用方保证这个座位有人 —— 现在唯一的调用点就是刚坐下
// 之后那两行，隔三行远。
func (r *Room) emitTo(seat int, f fact) {
	env := project(seat, f)
	env.Seq = r.nextSeq()
	r.deliver(seat, env)
}

// broadcast 发给所有在座的人。
func (r *Room) broadcast(f fact) {
	seq := r.nextSeq()
	for i, s := range r.seats {
		if s == nil {
			continue
		}
		env := project(i, f)
		env.Seq = seq
		r.deliver(i, env)
	}
}

// deliver 把信封交给投递方。
//
// **发送失败绝不进规则层**：发不出去一条 room.seats 不是 ILLEGAL_ACTION，
// 也不影响房间里任何裁决。ErrSlowConsumer 说明那个对端已经不读了 ——
// 那条连接自己的写超时会把连接关掉，随后走 Disconnected，这里什么都不用做。
func (r *Room) deliver(seat int, env protocol.Envelope) {
	if err := r.notify.Send(r.seats[seat].user.ID, env); err != nil {
		r.log.Info("推送失败", "seat", seat, "type", string(env.Type), "err", err)
	}
}
