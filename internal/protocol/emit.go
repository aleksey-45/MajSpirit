package protocol

import "sync"

// emit.go —— 给事件发号并投递（协议 §1.4）。
//
// 它和信封放在同一个包里，是因为 seq 是协议级的约束，不是某个 actor 的内部细节：
// 客户端拿它判断"有没有丢消息"，而这条判据要成立，发号的口径就必须说清楚。

// Message 是一条待发消息：收件人 + 信封。
//
// 收件人用 userId 而不是座位号：座位是房间/对局自己的事，投递方只认识用户。
type Message struct {
	UserID int64
	Env    Envelope
}

// Sender 是底层投递能力（internal/ws 的连接注册表满足它）。
//
// 用函数类型而不是接口：这里只需要一个方法，而它的实现在 room/game 里已经各有
// 一个同名接口描述过（Notifier）。再在叶子包立第三个名字换不来任何可测性 ——
// 换个假实现只要换一个函数字面量。
type Sender func(userID int64, env Envelope) error

// Emitter 给一个房间的事件发号并投递。
//
// 号是**按收件人**各一条流，不是"一条事件一个号"：客户端记录的只是"我收到的最大
// seq"，所以只有"发给我自己的号连续"才让 §1.4 那条判据成立。按事件发号会怎样：
// 一条只发给某一个人的事件（room.joined、game.claimWindow、game.snapshot）也要
// 吃掉一个号，别人从此看到一个洞 —— 而什么都没丢。客户端会照着协议 §4.4 去拉快照，
// 一局里每开一次鸣牌窗就拉一次。
//
// 所以：谁的号谁用，从 1 开始，只增不减；只有这个收件人自己漏收了才会出现洞。
//
// 号必须在**投递之前**发，而且不能挪到 ws 的写循环里：投递可能失败（对端的写队列
// 满），那条消息就此消失，客户端只有看到洞才知道要拉快照。在写循环里发号等于
// "丢的消息连号都不占"，客户端反而看不出来。
//
// 房间与对局是两个 actor，都往同一个 Emitter 里发 —— 也必须共用一个：各自造一个的话，
// 对局要从 1 重新发号，而客户端的大厅基线已经涨到 7，开局那一刻就是一个假洞。
//
// 那把锁护住"取号 + 入队"这一对动作，让号的大小顺序就是到达的顺序。只护取号会
// 乱序（后取的号先到），客户端白拉一次快照。临界区里只有 channel 入队
// （Client.Send 从不等待），没有网络、没有磁盘。
type Emitter struct {
	mu   sync.Mutex
	next map[int64]int
	send Sender
}

// NewEmitter 收一个投递函数。同一个房间的**所有**发事件者必须共用同一个 Emitter，
// 分两个就又是两条流了。
func NewEmitter(send Sender) *Emitter {
	return &Emitter{next: make(map[int64]int), send: send}
}

// Emit 给每个收件人编下一条属于他自己的号，然后依次投递。
//
// 一次调用通常就是一条事件的全部收件人。返回的切片与 msgs 等长，第 i 项是第 i 个
// 收件人的投递错误。失败只往上抛、不进规则层：一条 room.seats 发不出去不是任何人的
// 非法动作，也不影响房间里任何裁决；谁记日志由调用方决定（它有房间、事件类型这些
// 上下文）。
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
