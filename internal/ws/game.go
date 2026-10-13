package ws

// game.go —— 对局指令（协议 §5.3）。
//
// 这些指令不经过房间：网关自己按人找到那一场对局，直接投给对局的 actor。
// 合法性判断在 internal/game，这里只把 wire 上的字段解成它要的类型。

import (
	"encoding/json"

	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

// 杠的三种形态（协议 §5.3 的 kind）：wire 上的字面量。
const (
	kanOpen   = "open"
	kanClosed = "closed"
	kanAdded  = "added"
)

func (c *Client) gameSync() {
	if g := c.gameOr(protocol.CmdGameSync); g != nil {
		c.report(protocol.CmdGameSync, g.Sync(c.user.ID))
	}
}

func (c *Client) gameDiscard(data json.RawMessage) {
	var cmd gameDiscardCommand
	if !c.decode(protocol.CmdGameDiscard, data, &cmd) {
		return
	}
	t, ok := c.oneTile(protocol.CmdGameDiscard, cmd.Tile)
	if !ok {
		return
	}
	if g := c.gameOr(protocol.CmdGameDiscard); g != nil {
		c.report(protocol.CmdGameDiscard, g.Discard(c.user.ID, t))
	}
}

// gameRiichi 是立直宣言**同时**把宣言牌打出去（协议 §5.3）。
func (c *Client) gameRiichi(data json.RawMessage) {
	var cmd gameDiscardCommand // 载荷形状与 discard 相同：{tile}
	if !c.decode(protocol.CmdGameRiichi, data, &cmd) {
		return
	}
	t, ok := c.oneTile(protocol.CmdGameRiichi, cmd.Tile)
	if !ok {
		return
	}
	if g := c.gameOr(protocol.CmdGameRiichi); g != nil {
		c.report(protocol.CmdGameRiichi, g.Riichi(c.user.ID, t))
	}
}

// gameChi 收手里要用的那两张：被吃的那张由服务端从牌河推（协议 §5.3）。
func (c *Client) gameChi(data json.RawMessage) {
	var cmd gameChiCommand
	if !c.decode(protocol.CmdGameChi, data, &cmd) {
		return
	}
	if len(cmd.Tiles) != 2 {
		c.sendError(CodeBadRequest, "吃要给出手里那两张牌", protocol.CmdGameChi)
		return
	}
	a, ok := c.oneTile(protocol.CmdGameChi, cmd.Tiles[0])
	if !ok {
		return
	}
	b, ok := c.oneTile(protocol.CmdGameChi, cmd.Tiles[1])
	if !ok {
		return
	}
	if g := c.gameOr(protocol.CmdGameChi); g != nil {
		c.report(protocol.CmdGameChi, g.Chi(c.user.ID, [2]mahjong.Tile{a, b}))
	}
}

func (c *Client) gamePon() {
	if g := c.gameOr(protocol.CmdGamePon); g != nil {
		c.report(protocol.CmdGamePon, g.Pon(c.user.ID))
	}
}

// gameKan 的 open 不带牌：那张牌在别人的牌河里，服务端自己取（协议 §5.3）。
func (c *Client) gameKan(data json.RawMessage) {
	var cmd gameKanCommand
	if !c.decode(protocol.CmdGameKan, data, &cmd) {
		return
	}

	var t mahjong.Tile
	switch cmd.Kind {
	case kanOpen:
	case kanClosed, kanAdded:
		ok := false
		if t, ok = c.oneTile(protocol.CmdGameKan, cmd.Tile); !ok {
			return
		}
	default:
		c.sendError(CodeBadRequest, "kan 的 kind 只能是 open/closed/added", protocol.CmdGameKan)
		return
	}

	if g := c.gameOr(protocol.CmdGameKan); g != nil {
		c.report(protocol.CmdGameKan, g.Kan(c.user.ID, cmd.Kind, t))
	}
}

func (c *Client) gameWin() {
	if g := c.gameOr(protocol.CmdGameWin); g != nil {
		c.report(protocol.CmdGameWin, g.DeclareWin(c.user.ID))
	}
}

func (c *Client) gamePass() {
	if g := c.gameOr(protocol.CmdGamePass); g != nil {
		c.report(protocol.CmdGamePass, g.Pass(c.user.ID))
	}
}
