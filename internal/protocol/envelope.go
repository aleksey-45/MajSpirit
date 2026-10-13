package protocol

import "encoding/json"

// Envelope 是两个方向共用的信封（协议.md §4.2）。
//
// Type 决定 Data 该怎么解 —— 所以解析分两步：先解 Envelope，再按 Type 解 Data。
// 不做"一个万能 struct 装下所有消息的字段"：那样每加一种消息都要改同一个结构体，
// 而且字段全是指针或 any，没有一个字段是可读的。
type Envelope struct {
	Type Command         `json:"type"`
	Data json.RawMessage `json:"data"`

	// Seq 只有服务端→客户端的事件才有（协议 §1.4：客户端靠它检测丢消息）。
	// 指令方向没有，所以 omitempty —— 零值 0 不写出，字段直接消失。
	// 注意：seq 从 1 开始，所以 0 永远不是一个合法的序号，omitempty 不会误删真的序号。
	Seq int `json:"seq,omitempty"`
}

// Command 是消息类型的名字。
//
// 用一个命名类型而不是裸 string：函数签名上能看出"这里要的是消息名"，
// 而不是一个随便什么字符串。指令和事件共用它 —— 两个方向的词表允许同名，
// 但同名必须同义（比如 ping 是双向同一个动作）。
type Command string

// ── 会话（协议 §5.1、§6.1）────────────────────────────────────────────
const (
	CmdAuth Command = "auth"
	CmdPing Command = "ping"

	EventHello  Command = "hello"
	EventAuthed Command = "authed"
	EventPing   Command = "ping"
	EventPong   Command = "pong"
	EventError  Command = "error"
)

// ── 房间（协议 §5.2、§6.2）────────────────────────────────────────────
const (
	CmdRoomCreate Command = "room.create"
	CmdRoomJoin   Command = "room.join"
	CmdRoomLeave  Command = "room.leave"
	CmdRoomReady  Command = "room.ready"
	CmdRoomStart  Command = "room.start"

	EventRoomCreated      Command = "room.created"
	EventRoomJoined       Command = "room.joined"
	EventRoomSeats        Command = "room.seats"
	EventRoomReadyChanged Command = "room.readyChanged"
	EventRoomLeft         Command = "room.left"
	EventRoomSeatVacated  Command = "room.seatVacated"
	EventRoomClosed       Command = "room.closed"
)

// ── 对局（协议 §5.3、§6.3~§6.9）──────────────────────────────────────
const (
	CmdGameSync    Command = "game.sync"
	CmdGameDiscard Command = "game.discard"
	CmdGameRiichi  Command = "game.riichi"
	CmdGameChi     Command = "game.chi"
	CmdGamePon     Command = "game.pon"
	CmdGameKan     Command = "game.kan"
	CmdGameWin     Command = "game.win"
	CmdGamePass    Command = "game.pass"

	EventGameStarted      Command = "game.started"
	EventGameSnapshot     Command = "game.snapshot"
	EventGameDrew         Command = "game.drew"
	EventGameDiscarded    Command = "game.discarded"
	EventGameTurn         Command = "game.turn"
	EventGameMeld         Command = "game.meld"
	EventGameClaimWindow  Command = "game.claimWindow"
	EventGameClaimClosed  Command = "game.claimClosed"
	EventGameRiichi       Command = "game.riichi"
	EventGameRiichiStick  Command = "game.riichiStick"
	EventGameIppatsu      Command = "game.ippatsu"
	EventGameDoraRevealed Command = "game.doraRevealed"
	EventGameWinDeclared  Command = "game.winDeclared"
	EventGameRoundEnd     Command = "game.roundEnd"
	EventGameGameEnd      Command = "game.gameEnd"
)
