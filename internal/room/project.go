package room

import (
	"encoding/json"

	"github.com/aleksey-45/majspirit/internal/protocol"
)

// project.go —— 一条事实施加到各个座位上的信封。发消息的入口在 emit.go。

// fact 是一条已经发生的**事实**：房间里刚刚发生了这件事。
//
// 可见性由这两个字段二选一：
//
//	data    大厅事件用。四个座位收到的**是同一份字节** —— 大厅没有隐藏信息。
//	forSeat 对局事件用。按座位现构造，`myHand`、别人摸到的牌都只给本人。
//
// 隐藏信息只能从 forSeat 里产生。写进 data 就是泄漏，而且是对所有人广播。
type fact struct {
	typ     protocol.Command
	data    any
	forSeat func(seat int) any
}

// project 返回 seat 这个收件人应当收到的信封。
//
// 它是**唯一**能把多家可见状态编进一条外发消息的地方（协议 §7.2 的核对表就是它的
// 规格），而且是纯函数：不碰网络、不碰库、不碰并发，所以"别家的牌有没有被编进字节"
// 可以直接单测。
//
// 不填 seq：一条事件的各个收件人各有自己的号，而这里只看得见一个座位。
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
