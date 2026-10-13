package room

import (
	"encoding/json"

	"github.com/aleksey-45/majspirit/internal/protocol"
)

// Notifier 是房间对外发消息的能力。
//
// 定义在这里（消费方）而不是 internal/ws：接口属于使用它的包，和 user.Repo、
// api.UserService、ws.Authenticator 同一条规矩。换来的是房间逻辑能**脱离网络测试** ——
// 四个假玩家跑完一整局，不碰端口、不碰 goroutine。
//
// 消息类型用协议信封本身，不另造一个 room.Message：房间推出去的东西就是协议 §4.2
// 的信封，多一层映射只会多一个漂移点（seq 谁分配、data 怎么编码，两处各写一遍
// 必然不一致）。
type Notifier interface {
	Send(userID int64, msg protocol.Envelope) error
}

// fact 是一条已经发生的**事实**：房间里刚刚发生了这件事。
//
// 它是"事实"，不含可见性 —— 谁该看到什么由 project 决定。
type fact struct {
	typ  protocol.Command
	data any
}

// project 返回 seat 这个收件人应当收到的信封。
//
// **全服务端只有这里能把多家可见状态编进一条外发消息。** 目前大厅事件对四个座位
// 完全一样（大厅没有隐藏信息）；对局事件要在这里按座位裁剪 —— 别家的手牌、别人
// 摸到的牌、`claimWindow` 的候选都只给该看到的人，协议 §7.2 那张核对表就是它的规格。
//
// 参数 seat 现在用不上，留着是因为它就是那个裁剪点：签名先立在这里，
// 免得日后有人图省事在 broadcast 里直接拼信封。
//
// 必须是**纯函数**：不碰网络、不碰库、不碰并发。这样"别家的牌有没有被编进字节里"
// 可以直接单测，而不必端到端抓包。
func project(seat int, f fact) protocol.Envelope {
	_ = seat
	return protocol.Envelope{Type: f.typ, Data: mustJSON(f.data)}
}

// mustJSON 编码事件体。编码失败说明塞了个不能编码的类型，是程序 bug。
func mustJSON(payload any) json.RawMessage {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic("房间事件体编码失败（结构体定义有问题）: " + err.Error())
	}
	return raw
}
