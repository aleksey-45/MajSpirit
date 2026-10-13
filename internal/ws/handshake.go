package ws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// readCommand 读一条消息并解出信封，顺带把原始字节还给调用方（现在只用于日志）。
//
// 只收文本帧：协议 §2.1 定的是 JSON，二进制帧在这个协议里没有位置，
// 放行的话等于给"用另一种编码绕过协议"开口子。
func readCommand(ctx context.Context, c *websocket.Conn) (Envelope, []byte, error) {
	typ, data, err := c.Read(ctx)
	if err != nil {
		return Envelope{}, nil, err
	}
	if typ != websocket.MessageText {
		return Envelope{}, data, fmt.Errorf("收到非文本帧: %v", typ)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, data, fmt.Errorf("解析信封: %w", err)
	}
	return env, data, nil
}

// writeEvent 把 payload 包进信封发出去。
//
// 集中在一处的原因：以后会有 game.drew、room.joined 等一长串事件，
// 每个都手写一遍 Envelope{...} 并 wsjson.Write，只要有一处漏了 Data 或写错 Type，
// 就变成一条客户端看不懂的消息。包信封这件事只该有一个出口。
//
// 注意 Envelope.Data 的类型是 json.RawMessage（[]byte 的别名），
// 而 wsjson.Write 会再 marshal 一次 —— payload 会被 marshal 两遍。
// 在这个数据量下无所谓：一条消息几百字节，两遍 marshal 是微秒级。
// 真到要省的时候，做法是自己 json.Marshal(payload) 再塞进 Envelope.Data。
func writeEvent(ctx context.Context, c *websocket.Conn, typ Command, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("编码 %s 的 data: %w", typ, err)
	}
	return wsjson.Write(ctx, c, Envelope{Type: typ, Data: raw})
}
