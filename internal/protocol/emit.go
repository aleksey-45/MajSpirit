package protocol

import "sync"

// emit.go —— 事件发号与投递（协议 §1.4、§4.4）。取舍见协议 §1.4 的注。

// Message 是一条待发消息：收件人 + 信封。收件人用 userId，座位是房间自己的事。
type Message struct {
	UserID int64
	Env    Envelope
}

// Sender 是底层投递能力（internal/ws 的连接注册表满足它）。
type Sender func(userID int64, env Envelope) error

// Emitter 给一个房间的事件发号并投递。同一房间的所有发事件者共用它。
//
// 契约（协议 §1.4、§4.4）：
//
//   - 号按收件人各一条流，从 1 起只增不减。一条事件只发给几个人时，没收到的人
//     不该出现洞。
//   - 号在投递之前发。投递可能失败（写队列满），失败的那条要靠号被客户端发现。
//   - 锁罩住"取号 + 投递"整段，否则号的大小顺序和到达顺序会不一致。
//
// 临界区里只有 channel 入队（ws.Client.Send 从不等待），没有网络、没有磁盘。
type Emitter struct {
	mu   sync.Mutex
	next map[int64]int
	send Sender
}

// NewEmitter 收一个投递函数。
func NewEmitter(send Sender) *Emitter {
	return &Emitter{next: make(map[int64]int), send: send}
}

// Emit 给每个收件人编下一条属于他自己的号，然后依次投递。
//
// 返回的切片与 msgs 等长，第 i 项是第 i 个收件人的投递错误；全部成功时为 nil。
// 记日志由调用方做 —— 只有它知道座位、事件类型这些上下文。
func (e *Emitter) Emit(msgs []Message) []error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var errs []error
	for i, m := range msgs {
		e.next[m.UserID]++
		m.Env.Seq = e.next[m.UserID]
		if err := e.send(m.UserID, m.Env); err != nil {
			if errs == nil {
				errs = make([]error, len(msgs))
			}
			errs[i] = err
		}
	}
	return errs
}
