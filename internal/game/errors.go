package game

// errors.go —— 对局对外能报的几种失败。

import "errors"

var (
	ErrClosed      = errors.New("对局已结束")
	ErrNotInGame   = errors.New("不在这一场对局里")
	ErrNotYourTurn = errors.New("还没轮到你")
	ErrNotHolding  = errors.New("手里没有这张牌")
	// ErrNoWin 表示现在不能和牌：牌型不对、没有役、或者振听。
	//
	// 三种原因合成一个对外错误是有意的 —— 服务端不该告诉客户端"你有役但振听了"，
	// 那等于把别家看不到的信息漏出去。细节只进日志。
	ErrNoWin = errors.New("现在不能和牌")
)
