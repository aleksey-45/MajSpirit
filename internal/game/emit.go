package game

// emit.go —— 事件体怎么变成发给某个座位的信封：事实、按座位投影。

import (
	"encoding/json"

	"github.com/aleksey-45/majspirit/internal/protocol"
)

// Notifier 与"发号"都不在这里：投递用的是 protocol.Emitter，而且必须是**房间那个**
// （开局时传进来）—— 见 emit_seq.go。这里只负责"谁该看到什么"。
//
// fact 是一条已经发生的**事实**：牌桌上刚刚发生了这件事。
//
// 可见性由这两个字段二选一：
//
//	data    所有人看到的是同一份字节（牌河、分数、和牌信息）。
//	forSeat 按座位现构造：`myHand`、别人摸到的牌、鸣牌候选都只给该看到的人。
//
// 隐藏信息只能从 forSeat 里产生。写进 data 就是对所有人广播 —— 那是泄漏。
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
// 必须是纯函数：不碰网络、不碰库、不碰并发。
//
// 它不填 seq：一条事件的四个收件人必须共用一个号，而这里只看得见一个座位。
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
		panic("对局事件体编码失败（结构体定义有问题）: " + err.Error())
	}
	return raw
}
