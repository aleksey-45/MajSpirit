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
// 这张表少一个 RATE_LIMITED：还没有限流，等真有了再加。
type ErrorCode string

const (
	CodeBadRequest     ErrorCode = "BAD_REQUEST"
	CodeUnknownCommand ErrorCode = "UNKNOWN_COMMAND"
	CodeAuthRequired   ErrorCode = "AUTH_REQUIRED"
	CodeAuthFailed     ErrorCode = "AUTH_FAILED"
	CodeNotInRoom      ErrorCode = "NOT_IN_ROOM"
	CodeAlreadyInRoom  ErrorCode = "ALREADY_IN_ROOM"
	CodeRoomNotFound   ErrorCode = "ROOM_NOT_FOUND"
	CodeRoomFull       ErrorCode = "ROOM_FULL"
	CodeRoomStarted    ErrorCode = "ROOM_STARTED"
	CodeNotRoomOwner   ErrorCode = "NOT_ROOM_OWNER"
	CodeNotYourTurn    ErrorCode = "NOT_YOUR_TURN"
	CodeIllegalAction  ErrorCode = "ILLEGAL_ACTION"
	CodeTileNotInHand  ErrorCode = "TILE_NOT_IN_HAND"
	CodeInternal       ErrorCode = "INTERNAL"
)

// ── 指令的 data（协议 §5.2、§5.3）───────────────────────────────────────
//
// 字段名与协议一致。*bool 区分"没给"与"给了 false"。

type roomJoinCommand struct {
	RoomID string `json:"roomId"`
}

type roomReadyCommand struct {
	Ready *bool `json:"ready"`
}

type gameDiscardCommand struct {
	Tile string `json:"tile"`
}

type gameChiCommand struct {
	Tiles []string `json:"tiles"`
}

type gameKanCommand struct {
	Kind string `json:"kind"`
	Tile string `json:"tile,omitempty"`
}

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
