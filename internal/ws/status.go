package ws

import "github.com/coder/websocket"

// 关闭码（协议.md §4.6）。WebSocket 规范把 3000–4999 留给应用自定义。
const (
	// statusProtocolError 是无法解析的消息、非法 JSON。
	statusProtocolError = websocket.StatusCode(4400)
	// statusAuthFailed 覆盖两种情形：5 秒内没认证，和认证失败。
	// 协议 §4.1 对这两者都给 4401，所以这里只有一个常量，不要拆成两个自造码。
	statusAuthFailed = websocket.StatusCode(4401)
	// statusHeartbeatTimeout 是心跳超时（协议 §4.3：连续未回 pong 判死）。
	statusHeartbeatTimeout = websocket.StatusCode(4408)
	// statusReplaced 是同一账号在别处登录，这条连接被顶掉（协议 §4.6）。
	statusReplaced = websocket.StatusCode(4403)
)
