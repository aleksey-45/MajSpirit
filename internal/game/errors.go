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

// ErrNoClaim 表示现在没有可以鸣的牌，或者这个座位没有鸣它的资格。
var ErrNoClaim = errors.New("现在没有可以鸣的牌")

// ErrMaxKan 表示杠数到了上限（四个）。这是规则上限，不是牌山不够。
var ErrMaxKan = errors.New("杠数已达上限")

// 立直（规则集 §3）：
var (
	ErrAlreadyRiichi = errors.New("已经立直了")
	ErrNotMenzen     = errors.New("副露过就不能立直")
	ErrNotTenpai     = errors.New("打掉这张就不听牌了")
	ErrNoPoints      = errors.New("点数不够供托")
	ErrWallTooShort  = errors.New("牌山剩得不够立直")
	ErrRiichiLocked  = errors.New("立直之后只能摸切")
)
