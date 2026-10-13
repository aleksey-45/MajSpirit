package game

import (
	"time"

	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

// claim.go —— 鸣牌窗口（协议 §6.6）。
//
// 一张牌打出来之后，其余三家可能想吃/碰/荣和。这是个**等待**：谁在等、等什么、
// 什么时候截止。等待本身就是对局状态的一部分 —— 不为此开 goroutine，也不为每个
// 询问开一个定时器，就记在 `g.claim` 里，到点了 select 会回来。
//
//   - 窗口**只推给有资格的座位**，不能广播后各自过滤 —— "某人收到了 options"
//     这件事本身就暴露了他可能有和牌机会（协议 §7.3 第 2 条）。
//   - 优先级 **荣和 > 碰 > 吃**，同级按"离放铳者由近到远"。所以不能"谁先回答谁赢"，
//     得等所有人答完（或到点），再按优先级挑。一炮多响取**头跳**，一局只可能
//     有 0 或 1 家和牌，「三家和了」在本项目不可达。
//
// 杠要摸岭上牌、宝牌指示牌也在王牌里，而 `mahjong.Wall` 还没有王牌 —— 那两件事
// 一起做，见 meld.go 顶部。

// claim 是一次进行中的鸣牌窗口。
type claim struct {
	from     int
	tile     mahjong.Tile
	eligible []int  // 有资格的座位，按"离放铳者由近到远"排好
	trigger  string // "discard" 或 "kan"（抢杠）—— 只影响事件里的字段和收尾
	kanSeat  int    // >= 0 表示这是抢杠窗口：没人抢就由这个座位把杠完成
	options  map[int][]string
	answered map[int]bool
	deadline time.Time

	ron map[int]bool
	pon map[int]bool
	kan map[int]bool
	chi map[int][2]mahjong.Tile
}

// openClaimWindow 在有人打出一张牌之后开窗口。
//
// 没人能鸣就不开窗口，直接轮到下家 —— 那是最常见的情况，不该为它造一次等待。
func (g *Game) openClaimWindow(from int, tile mahjong.Tile) {
	c := &claim{
		from:     from,
		tile:     tile,
		trigger:  "discard",
		kanSeat:  -1,
		options:  map[int][]string{},
		answered: map[int]bool{},
		ron:      map[int]bool{},
		pon:      map[int]bool{},
		kan:      map[int]bool{},
		chi:      map[int][2]mahjong.Tile{},
	}
	for offset := 1; offset < 4; offset++ {
		seat := (from + offset) % 4
		if opts := g.claimOptions(seat, from, tile); opts != nil {
			c.eligible = append(c.eligible, seat)
			c.options[seat] = opts
		}
	}
	if len(c.eligible) == 0 {
		g.nextTurn()
		return
	}

	c.deadline = time.Now().Add(g.opt.ClaimLimit)
	g.claim = c
	g.deadline = c.deadline

	// 窗口内容四家一样，被裁的是**收件人名单**，所以走 data 而不是 forSeat。
	deadlineMs := c.deadline.UnixMilli()
	for _, seat := range c.eligible {
		g.emitTo(seat, fact{typ: protocol.EventGameClaimWindow, data: gameClaimWindowView{
			Trigger:    c.trigger,
			FromSeat:   from,
			Tile:       formatTile(tile),
			Options:    c.options[seat],
			DeadlineMs: deadlineMs,
		}})
	}
}

// canRon 报告这个座位能不能荣和这张牌：牌型成立、有役、不振听。三道门都在
// `mahjong.Win` 里（它同时判牌型、役、和 `WinInput.Furiten`）。
//
// 这里不自己抄一遍 —— 抄的那份会和规则层漂开，而漂开的方向是"让不该和的人和了"。
func (g *Game) canRon(seat int, tile mahjong.Tile) bool {
	in := g.winInput(seat, tile, false)
	_, err := mahjong.Win(in, mahjong.DefaultRules())
	return err == nil
}

// doPass 处理 game.pass：放弃这次机会。
func (g *Game) doPass(userID int64) error {
	seat := g.seatOf(userID)
	if seat < 0 {
		return ErrNotInGame
	}
	if g.claim == nil || !g.claim.answers(seat) {
		// 没有窗口，或者这个座位本来就没资格 —— 客户端可能慢一步发，不算错。
		return nil
	}
	g.claim.answered[seat] = true
	g.maybeResolveClaim()
	return nil
}

// claimRon 记录一次荣和宣告。
//
// 头跳只认最近的那一家：如果宣告的正是**最近那个能荣和的座位**，后面的人答什么
// 都不改变结果 —— 立刻结账，不让三家白看 3 秒。否则得等（更近的人可能也要和）。
func (g *Game) claimRon(seat int) {
	c := g.claim
	c.ron[seat] = true
	c.answered[seat] = true
	if seat == c.nearest("ron") {
		g.resolveClaim()
		return
	}
	g.maybeResolveClaim()
}

// nearest 返回按远近排在最前、且有这项选择的座位。
func (c *claim) nearest(what string) int {
	for _, seat := range c.eligible {
		if c.has(seat, what) {
			return seat
		}
	}
	return -1
}

// answers 报告这个座位有没有资格回答这个窗口。
func (c *claim) answers(seat int) bool {
	_, ok := c.options[seat]
	return ok
}

// maybeResolveClaim 所有人都回答了就地裁决；还有人没答就继续等（到点算过）。
func (g *Game) maybeResolveClaim() {
	if g.claim == nil {
		return
	}
	for _, seat := range g.claim.eligible {
		if !g.claim.answered[seat] {
			return
		}
	}
	g.resolveClaim()
}

// onClaimTimeout 窗口到点：没回答的视为 `pass`（协议 §6.6）。
func (g *Game) onClaimTimeout() {
	if g.claim == nil {
		return
	}
	g.log.Info("鸣牌窗口超时", "tile", formatTile(g.claim.tile))
	g.resolveClaim()
}

// resolveClaim 按优先级裁决并收尾。
func (g *Game) resolveClaim() {
	c := g.claim
	g.claim = nil
	g.deadline = time.Time{}

	seat, kind := c.pick()

	closed := gameClaimClosedView{Trigger: c.trigger, Seat: -1}
	if seat >= 0 {
		closed.ResolvedBy, closed.Seat = kind, seat
	}
	g.broadcast(fact{typ: protocol.EventGameClaimClosed, data: closed})

	switch kind {
	case "ron":
		in := g.winInput(seat, c.tile, false)
		res, err := mahjong.Win(in, mahjong.DefaultRules())
		if err != nil {
			// canRon 已经用同一个函数判过一次，这里失败说明状态在两次之间被改过。
			g.log.Error("已经判过能荣和，结账时却失败", "seat", seat, "err", err)
			g.nextTurn()
			return
		}
		g.settleWin(seat, c.from, c.tile, false, res)

	case "kan":
		g.applyOpenKan(seat, c.tile, c.from)
		g.afterKan(seat)

	case "pon":
		m := g.applyPon(seat, c.tile)
		g.growMelds(seat, m, c.from, c.tile)
		g.beginMeldTurn(seat)

	case "chi":
		m := g.applyChi(seat, c.chi[seat], c.tile)
		g.growMelds(seat, m, c.from, c.tile)
		g.beginMeldTurn(seat)

	default:
		// 抢杠窗口里没人抢：杠照成。普通鸣牌窗口没人鸣：轮到下家。
		if c.kanSeat >= 0 {
			g.completeAddedKan(c.kanSeat, c.tile)
			return
		}
		g.nextTurn()
	}
}

// pick 按优先级挑赢家：荣和 > 碰/杠 > 吃（协议 §6.6）；同级里离放铳者近的优先
// （eligible 建窗口时就按远近排好了）。
func (c *claim) pick() (int, string) {
	for _, seat := range c.eligible {
		if c.ron[seat] {
			return seat, "ron"
		}
	}
	// 碰与杠同一档：都是"把那张牌收进自己的刻子"，规则没有先后的依据。
	for _, seat := range c.eligible {
		if c.kan[seat] {
			return seat, "kan"
		}
		if c.pon[seat] {
			return seat, "pon"
		}
	}
	for _, seat := range c.eligible {
		if _, ok := c.chi[seat]; ok {
			return seat, "chi"
		}
	}
	return -1, ""
}

// nextTurn 没有人鸣：轮到下家摸牌。
func (g *Game) nextTurn() {
	g.turn = (g.turn + 1) % 4
	g.draw()
}
