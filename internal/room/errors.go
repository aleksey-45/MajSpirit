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

	// ErrNotPlaying 表示这个房间现在不在对局中。
	ErrNotPlaying = errors.New("不在对局中")
	// ErrNotYourTurn 表示现在不是这个座位该行动。
	ErrNotYourTurn = errors.New("还没轮到你")
	// ErrNotHolding 表示手里没有这张牌。
	ErrNotHolding = errors.New("手里没有这张牌")
	// ErrNoWin 表示现在不能和牌：牌型不对、没有役、或者振听。
	//
	// 三种原因合成一个对外错误是有意的 —— 服务端不该告诉客户端"你有役但振听了"，
	// 那等于把别家看不到的信息漏出去。细节只进日志。
	ErrNoWin = errors.New("现在不能和牌")
)
