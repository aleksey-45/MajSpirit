package game

// emit.go —— 事件体怎么变成发给某个座位的信封：事实、按座位投影。
// 发号与投递见 emit_seq.go。

import (
	"encoding/json"

	"github.com/aleksey-45/majspirit/internal/protocol"
)

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
// 它是**唯一**能把多家可见状态编进一条外发消息的地方（协议 §7.2 的核对表），
// 而且是纯函数：不碰网络、不碰库、不碰并发。
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
		panic("对局事件体编码失败（结构体定义有问题）: " + err.Error())
	}
	return raw
}
