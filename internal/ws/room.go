package ws

// room.go —— 房间指令（协议 §5.2）。
//
// 只做三件事：解 data、挡住"一个人同时在两个房间"、把领域错误交给 report。
// 座位与开局的判定在 internal/room —— 那里才看得见房间状态。

import (
	"encoding/json"
	"strings"

	"github.com/aleksey-45/majspirit/internal/protocol"
)

// roomCreate 建一个房间并占 seat 0。room.created 由房间自己发，这里不补。
func (c *Client) roomCreate() {
	// 一个人只能在一个房间里：不拦的话两个房间都会以为他在座。
	if _, ok := c.hub.ByUser(c.user.ID); ok {
		c.sendError(CodeAlreadyInRoom, "已经在别的房间里", protocol.CmdRoomCreate)
		return
	}
	if _, err := c.hub.Create(c.user); err != nil {
		c.report(protocol.CmdRoomCreate, err)
	}
}

// roomJoin 按房间号加入（已经在里面就是重连，房间自己认）。
func (c *Client) roomJoin(data json.RawMessage) {
	var cmd roomJoinCommand
	if !c.decode(protocol.CmdRoomJoin, data, &cmd) {
		return
	}
	// 房间号是给人口头报、手输的，大小写不该决定成败。
	r, ok := c.hub.Lookup(strings.ToUpper(strings.TrimSpace(cmd.RoomID)))
	if !ok {
		c.sendError(CodeRoomNotFound, "房间号不存在", protocol.CmdRoomJoin)
		return
	}
	// 已经在别的房间里：同 roomCreate。
	if cur, ok := c.hub.ByUser(c.user.ID); ok && cur != r {
		c.sendError(CodeAlreadyInRoom, "已经在别的房间里", protocol.CmdRoomJoin)
		return
	}
	c.report(protocol.CmdRoomJoin, r.Join(c.user))
}

func (c *Client) roomLeave() {
	r := c.roomOr(protocol.CmdRoomLeave)
	if r == nil {
		return
	}
	c.report(protocol.CmdRoomLeave, r.Leave(c.user.ID))
}

func (c *Client) roomReady(data json.RawMessage) {
	var cmd roomReadyCommand
	if !c.decode(protocol.CmdRoomReady, data, &cmd) {
		return
	}
	// ready 缺字段不算 false：那会把"客户端漏发了"变成"他取消准备了"。
	if cmd.Ready == nil {
		c.sendError(CodeBadRequest, "room.ready 要带 ready", protocol.CmdRoomReady)
		return
	}
	r := c.roomOr(protocol.CmdRoomReady)
	if r == nil {
		return
	}
	c.report(protocol.CmdRoomReady, r.SetReady(c.user.ID, *cmd.Ready))
}

func (c *Client) roomStart() {
	r := c.roomOr(protocol.CmdRoomStart)
	if r == nil {
		return
	}
	c.report(protocol.CmdRoomStart, r.Start(c.user.ID))
}
