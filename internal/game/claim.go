package game

import (
	"time"

	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

// claim.go —— 鸣牌窗口（协议 §6.6）。
//
// 一张牌打出来之后，其余三家可能想吃/碰/杠/荣和。这是个**等待**：谁在等、等什么、
// 什么时候截止。等待本身就是对局状态的一部分 —— 不为此开 goroutine，也不为每个
// 询问开一个定时器，就记在 `g.claim` 里，到点了 select 会回来。
//
// 这一片只做**荣和**：吃/碰/杠要先把副露表示和"鸣完由谁打"接上（阶段四 §4.3 的
// 下一片），现在窗口里只有 `ron` / `pass` 两个选项。判定逻辑照着协议 §6.6 来：
//
//   - 窗口**只推给有资格的座位**，不能广播后各自过滤 —— "某人收到了 options"
//     这件事本身就暴露了他可能有和牌机会（协议 §7.3 第 2 条）。
//   - 一炮多响取**头跳**：放铳者的下家 → 对家 → 上家，只认最近一家。
//     所以一局只可能有 0 或 1 家和牌，「三家和了」在本项目不可达。

// claim 是一次进行中的鸣牌窗口。
type claim struct {
	from     int // 打牌的人
	tile     mahjong.Tile
	eligible []int // 能荣和的座位，按"离放铳者由近到远"排好 —— 头跳就是取第一个响应的
	answered map[int]bool
	ron      map[int]bool // 宣告了荣和的座位
	deadline time.Time
}

// openClaimWindow 在有人打出一张牌之后开窗口。
//
// 没人能荣和就不开窗口，直接轮到下家 —— 那是最常见的情况，不该为它造一次等待。
func (g *Game) openClaimWindow(from int, tile mahjong.Tile) {
	var eligible []int
	for offset := 1; offset < 4; offset++ {
		seat := (from + offset) % 4
		if g.canRon(seat, tile) {
			eligible = append(eligible, seat)
		}
	}
	if len(eligible) == 0 {
		g.nextTurn()
		return
	}

	g.claim = &claim{
		from:     from,
		tile:     tile,
		eligible: eligible,
		answered: make(map[int]bool, len(eligible)),
		ron:      make(map[int]bool, len(eligible)),
		deadline: time.Now().Add(g.opt.ClaimLimit),
	}
	g.deadline = g.claim.deadline

	// 只推给有资格的座位。窗口内容对每家**完全一样**（荣和不需要按座位换内容），
	// 所以走 data 而不是 forSeat —— 但收件人名单是裁过的。
	deadlineMs := g.claim.deadline.UnixMilli()
	for _, seat := range eligible {
		g.emitTo(seat, fact{typ: protocol.EventGameClaimWindow, data: gameClaimWindowView{
			Trigger:    "discard",
			FromSeat:   from,
			Tile:       formatTile(tile),
			Options:    []string{"ron", "pass"},
			DeadlineMs: deadlineMs,
		}})
	}
}

// canRon 报告这个座位能不能荣和这张牌。
//
// 三道门都要过：牌型成立、有役、不振听。第三道用规则集 §2.2 的判据 ——
// **自己舍牌里有自己的听牌张**就是永久振听。
func (g *Game) canRon(seat int, tile mahjong.Tile) bool {
	if seat == g.turn && g.drawn {
		return false // 自己刚打出去的那张（不会发生，但别让判定绕过回合）
	}
	in := g.winInput(seat, tile, false)
	if _, err := mahjong.Win(in, mahjong.DefaultRules()); err != nil {
		return false
	}
	return true
}

// doPass 处理 game.pass：放弃这次荣和。
func (g *Game) doPass(userID int64) error {
	seat := g.seatOf(userID)
	if seat < 0 {
		return ErrNotInGame
	}
	if g.claim == nil || !g.claim.answers(seat) {
		// 没有窗口，或者这个座位本来就没资格 —— 两种情况都是"过"了个不存在的东西，
		// 不算错（客户端可能慢一步发）。
		return nil
	}
	g.claim.answered[seat] = true
	g.maybeResolveClaim()
	return nil
}

// claimRon 记录一次荣和宣告。回答齐了就立刻结账，不必等满 3 秒。
//
// 不等满窗口是有意的：头跳只认最近那一家，最近的宣告之后剩下的人答什么都不改变
// 结果 —— 还等下去只是让三家白看 3 秒。
func (g *Game) claimRon(seat int) {
	g.claim.ron[seat] = true
	g.claim.answered[seat] = true
	g.resolveClaim()
}

// answers 报告这个座位有没有资格回答这个窗口。
func (c *claim) answers(seat int) bool {
	for _, s := range c.eligible {
		if s == seat {
			return true
		}
	}
	return false
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

// resolveClaim 裁决并收尾。
//
// 头跳：`eligible` 已经按"离放铳者由近到远"排好，取第一个宣告荣和的。
// 因为只有荣和一个动作，这里不需要"荣和 > 碰/杠 > 吃"那套优先级 ——
// 等吃碰杠进来时再加，那时的规则是同级之间按逆时针顺序。
func (g *Game) resolveClaim() {
	c := g.claim
	g.claim = nil
	g.deadline = time.Time{}

	winner := -1
	for _, seat := range c.eligible {
		if c.ron[seat] {
			winner = seat
			break
		}
	}

	resolvedBy := any(nil)
	if winner >= 0 {
		resolvedBy = "ron"
	}
	g.broadcast(fact{typ: protocol.EventGameClaimClosed, data: gameClaimClosedView{
		Trigger:    "discard",
		ResolvedBy: resolvedBy,
		Seat:       winner,
	}})

	if winner < 0 {
		g.nextTurn()
		return
	}

	// 荣和的和牌张**不在**手里，winInput 会替我们加上（协议 §5.3 的 game.win
	// 由服务端按上下文判断自摸还是荣和）。
	in := g.winInput(winner, c.tile, false)
	res, err := mahjong.Win(in, mahjong.DefaultRules())
	if err != nil {
		// canRon 已经用同一个函数判过一次，这里失败说明状态在两次之间被改过。
		g.log.Error("已经判过能荣和，结账时却失败", "seat", winner, "err", err)
		g.nextTurn()
		return
	}
	g.settleWin(winner, c.from, c.tile, false, res)
}

// nextTurn 没有人鸣：轮到下家摸牌。
func (g *Game) nextTurn() {
	g.turn = (g.turn + 1) % 4
	g.draw()
}
