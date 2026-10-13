package ws

import "github.com/aleksey-45/majspirit/internal/protocol"

// 信封与消息名住在 internal/protocol：`room` 也要往外发信封，而它不该 import 本包。
// 这里留别名，本包内部与测试的写法一行都不用改。
type (
	Envelope = protocol.Envelope
	Command  = protocol.Command
)

// 本包用到的会话消息名（协议 §5.1、§6.1）。
const (
	CmdAuth = protocol.CmdAuth
	CmdPing = protocol.CmdPing

	EventHello  = protocol.EventHello
	EventAuthed = protocol.EventAuthed
	EventPing   = protocol.EventPing
	EventPong   = protocol.EventPong
	EventError  = protocol.EventError
)
