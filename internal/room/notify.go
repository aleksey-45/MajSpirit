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
// 可见性由这两个字段二选一：
//
//	data    大厅事件用。四个座位收到的**是同一份字节** —— 大厅没有隐藏信息。
//	forSeat 对局事件用。按座位现构造，`myHand`、别人摸到的牌都只给本人。
//
// 隐藏信息**只能**从 forSeat 里产生。写进 data 就是泄漏，而且是对所有人广播。
type fact struct {
	typ     protocol.Command
	data    any
	forSeat func(seat int) any
}

// project 返回 seat 这个收件人应当收到的信封。
//
// **全服务端只有这里能把多家可见状态编进一条外发消息。** 协议 §7.2 那张核对表
// 就是它的规格；配套的单测直接断言**序列化后的字节**里没有别家的手牌。
//
// 必须是**纯函数**：不碰网络、不碰库、不碰并发。这样"别家的牌有没有被编进字节里"
// 可以直接单测，而不必端到端抓包。
func project(seat int, f fact) protocol.Envelope {
	payload := f.data
	if f.forSeat != nil {
		payload = f.forSeat(seat)
	}
	return protocol.Envelope{Type: f.typ, Data: mustJSON(payload)}
}

// mustJSON 编码事件体。编码失败说明塞了个不能编码的类型，是程序 bug。
func mustJSON(payload any) json.RawMessage {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic("房间事件体编码失败（结构体定义有问题）: " + err.Error())
	}
	return raw
}
