package ws

// dispatch.go —— 已认证连接上的指令落到哪个 actor，以及领域错误怎么变成协议错误码。
//
// 指令表就是协议 §5：房间那几条在 room.go，对局那几条在 game.go。
// 错误码表在协议 §2.6。

import (
	"encoding/json"
	"errors"

	"github.com/aleksey-45/majspirit/internal/game"
	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
	"github.com/aleksey-45/majspirit/internal/room"
)

// dispatch 处理一条已认证的指令。
func (c *Client) dispatch(env protocol.Envelope) {
	switch env.Type {
	// ── 房间（协议 §5.2）──
	case protocol.CmdRoomCreate:
		c.roomCreate()
	case protocol.CmdRoomJoin:
		c.roomJoin(env.Data)
	case protocol.CmdRoomLeave:
		c.roomLeave()
	case protocol.CmdRoomReady:
		c.roomReady(env.Data)
	case protocol.CmdRoomStart:
		c.roomStart()

	// ── 对局（协议 §5.3）──
	case protocol.CmdGameSync:
		c.gameSync()
	case protocol.CmdGameDiscard:
		c.gameDiscard(env.Data)
	case protocol.CmdGameRiichi:
		c.gameRiichi(env.Data)
	case protocol.CmdGameChi:
		c.gameChi(env.Data)
	case protocol.CmdGamePon:
		c.gamePon()
	case protocol.CmdGameKan:
		c.gameKan(env.Data)
	case protocol.CmdGameWin:
		c.gameWin()
	case protocol.CmdGamePass:
		c.gamePass()

	default:
		c.sendError(CodeUnknownCommand, "不认识的指令", env.Type)
	}
}

// ── 取上下文 ────────────────────────────────────────────────────────────

// roomOr 返回这个人所在的房间；不在任何房间就回错误并返回 nil。
func (c *Client) roomOr(ref protocol.Command) *room.Room {
	r, ok := c.hub.ByUser(c.user.ID)
	if !ok {
		c.sendError(CodeNotInRoom, "不在任何房间", ref)
		return nil
	}
	return r
}

// gameOr 返回这个人正在打的那一场；没有就回错误并返回 nil。
//
// "在房间里但没有对局"是 ILLEGAL_ACTION（协议 §2.6：动作合法但当前不成立）。
func (c *Client) gameOr(ref protocol.Command) *game.Game {
	r := c.roomOr(ref)
	if r == nil {
		return nil
	}
	g := r.Game()
	if g == nil {
		c.sendError(CodeIllegalAction, "现在没有对局", ref)
		return nil
	}
	return g
}

// ── 解析与回错 ──────────────────────────────────────────────────────────

// decode 解一条指令的 data。解不开就回 BAD_REQUEST（协议 §2.6）。
//
// 不拒绝未知字段：协议 §2.1 要求两边都忽略不认识的字段。HTTP 那边相反 —— 那边的
// 请求体是我们自己的接口，多一个字段就是客户端写错了。
func (c *Client) decode(ref protocol.Command, data json.RawMessage, dst any) bool {
	if len(data) == 0 {
		data = []byte("{}") // data 不可省略，但可以是 {}
	}
	if err := json.Unmarshal(data, dst); err != nil {
		c.sendError(CodeBadRequest, "指令格式不对", ref)
		return false
	}
	return true
}

// oneTile 解一张牌。类型对、值不对（比如 "5x"）算 BAD_REQUEST。
func (c *Client) oneTile(ref protocol.Command, s string) (mahjong.Tile, bool) {
	tiles, err := mahjong.ParseTiles(s)
	if err != nil || len(tiles) != 1 {
		c.sendError(CodeBadRequest, "牌面写法不对", ref)
		return 0, false
	}
	return tiles[0], true
}

// sendError 回一条 error 事件（协议 §6.1）。连接不关 —— 关闭码只留给协议错误、
// 认证、顶号、心跳这四件"这条连接不能用了"的事（协议 §4.6）。
func (c *Client) sendError(code ErrorCode, msg string, ref protocol.Command) {
	if err := c.Send(Envelope{
		Type: protocol.EventError,
		Data: mustJSON(errorEvent{Code: code, Message: msg, Ref: ref}),
	}); err != nil {
		c.log.Info("回错误事件失败", "code", code, "err", err)
	}
}

// report 把领域错误翻译成协议错误后回给客户端；成功时什么都不做。
func (c *Client) report(ref protocol.Command, err error) {
	if err == nil {
		return
	}
	code, msg := codeOf(err)
	if code == CodeInternal {
		// 没映射到 = 这里漏了一个哨兵。日志要看得见，细节不发给客户端（协议 §2.6）。
		c.log.Error("指令失败", "type", string(ref), "err", err)
	}
	c.sendError(code, msg, ref)
}

// codeOf 把一个领域的哨兵翻译成协议错误码（协议 §2.6 的表）。
//
// 用 errors.Is 比而不是 ==：哨兵会被包过（room/game 内部都加了自己的上下文）。
func codeOf(err error) (ErrorCode, string) {
	switch {
	// 房间
	case errors.Is(err, room.ErrRoomFull):
		return CodeRoomFull, "房间已经满了"
	case errors.Is(err, room.ErrPlaying):
		return CodeRoomStarted, "对局已经开始了"
	case errors.Is(err, room.ErrNotOwner):
		return CodeNotRoomOwner, "只有房主能开局"
	case errors.Is(err, room.ErrNotReady):
		return CodeIllegalAction, "还有人没准备"
	case errors.Is(err, room.ErrNotEnough):
		return CodeIllegalAction, "人还没坐满"
	case errors.Is(err, room.ErrNotInRoom):
		return CodeNotInRoom, "不在这个房间里"
	case errors.Is(err, room.ErrClosed), errors.Is(err, room.ErrHubClosed):
		return CodeNotInRoom, "房间已经没了"

	// 对局
	case errors.Is(err, game.ErrNotInGame):
		return CodeIllegalAction, "不在这一场对局里"
	case errors.Is(err, game.ErrNotYourTurn):
		return CodeNotYourTurn, "还没轮到你"
	case errors.Is(err, game.ErrNotHolding):
		return CodeTileNotInHand, "手里没有这张牌"
	case errors.Is(err, game.ErrNoWin):
		return CodeIllegalAction, "现在不能和牌"
	case errors.Is(err, game.ErrNoClaim):
		return CodeIllegalAction, "现在没有可以鸣的牌"
	case errors.Is(err, game.ErrMaxKan):
		return CodeIllegalAction, "杠数已达上限"
	case errors.Is(err, game.ErrClosed):
		return CodeIllegalAction, "这一场已经打完了"
	case errors.Is(err, game.ErrAlreadyRiichi):
		return CodeIllegalAction, "已经立直了"
	case errors.Is(err, game.ErrNotMenzen):
		return CodeIllegalAction, "副露过就不能立直"
	case errors.Is(err, game.ErrNotTenpai):
		return CodeIllegalAction, "打掉这张就不听牌了"
	case errors.Is(err, game.ErrNoPoints):
		return CodeIllegalAction, "点数不够供托"
	case errors.Is(err, game.ErrWallTooShort):
		return CodeIllegalAction, "牌山剩得不够立直"
	case errors.Is(err, game.ErrRiichiLocked):
		return CodeIllegalAction, "立直之后只能摸切"
	}
	return CodeInternal, "服务端内部错误"
}
