package ws

// hello 是连上后服务端发的第一条（协议 §6.1）。
// sessionId 不参与鉴权，只用于把日志和客户端对上 —— 所以它是随机的，不承诺唯一。
type hello struct {
	ProtocolVersion int    `json:"protocolVersion"`
	ServerTime      int64  `json:"serverTime"`
	SessionID       string `json:"sessionId"`
}

// pingEvent / pongEvent 是心跳消息（协议 §4.3、§6.1）。
// 带 serverTime 是为了让客户端能校准倒计时（协议 §2.4）。
type pingEvent struct {
	ServerTime int64 `json:"serverTime"`
}

type pongEvent struct {
	ServerTime int64 `json:"serverTime"`
}

// 错误码是给机器看的标识，message 才是给人看的（协议 §2.6）。
type ErrorCode string

const (
	CodeAuthRequired ErrorCode = "AUTH_REQUIRED"
	CodeAuthFailed   ErrorCode = "AUTH_FAILED"
	CodeInternal     ErrorCode = "INTERNAL"
)

type authCommand struct {
	Token string `json:"token"`
}

type authedEvent struct {
	UserID   int64  `json:"userId"`
	Username string `json:"username"`
}

// errorEvent 的 Ref 是触发它的指令名，方便前端定位（协议 §6.1）。
type errorEvent struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Ref     Command   `json:"ref,omitempty"`
}
