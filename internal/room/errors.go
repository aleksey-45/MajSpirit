package room

import "errors"

var (
	// ErrRoomNotFound 表示房间号不存在（或已经销毁）。
	ErrRoomNotFound = errors.New("房间不存在")
	// ErrRoomFull 表示四个座位都有人了。
	ErrRoomFull = errors.New("房间已满")
	// ErrNotInRoom 表示这个人不在这个房间里。
	ErrNotInRoom = errors.New("不在这个房间里")
	// ErrPlaying 表示对局已经开始，大厅操作不再受理。
	ErrPlaying = errors.New("对局已经开始")
	// ErrNotOwner 表示只有房主能开局。
	ErrNotOwner = errors.New("只有房主能开局")
	// ErrNotEnough 表示人数不够。
	ErrNotEnough = errors.New("需要四个人才能开局")
	// ErrNotReady 表示还有人没准备。
	ErrNotReady = errors.New("还有玩家没准备")
	// ErrClosed 表示房间已经关闭。关闭之后往 commands 里发消息会 panic，
	// 所以每个入口都要先看这个。
	ErrClosed = errors.New("房间已关闭")
)
