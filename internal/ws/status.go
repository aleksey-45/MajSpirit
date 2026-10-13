package ws

import "github.com/coder/websocket"

// 关闭码（协议.md §4.6）。WebSocket 规范把 3000–4999 留给应用自定义。
const (
	// statusProtocolError 是无法解析的消息、非法 JSON。
	statusProtocolError = websocket.StatusCode(4400)
	// statusAuthFailed 覆盖两种情形：5 秒内没认证，和认证失败。
	// 协议 §4.1 对这两者都给 4401，所以这里只有一个常量，不要拆成两个自造码。
	statusAuthFailed = websocket.StatusCode(4401)
)

// closeConn 发关闭帧，告诉对端为什么关。
//
// 没有 ctx 参数：Conn.Close 内部自己用 context.Background() 起了 5 秒超时
// （close.go:171 的 writeClose），和调用方的 ctx 无关。所以超时路径上
// authCtx 已经过期也不影响它 —— 这一点实测过，别照直觉给它加个 ctx。
//
// 代价：Close 会等对端回关闭帧（最多 5 秒）。对端直接断线的话，
// 它提前返回；对端赖着不回的极端情况会占住这个 goroutine 5 秒，
// 第 5 步接房间层时要回来处理。
func closeConn(c *websocket.Conn, code websocket.StatusCode, reason string) {
	_ = c.Close(code, reason)
}
