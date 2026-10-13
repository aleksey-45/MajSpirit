package game

// round.go —— 一小局的推进：发牌、摸打、回合超时、和了。
//
// 状态机是**线性的摸打循环**，会被"等玩家回应"打断（协议 §6.6）。等待本身就是
// 状态：谁在等、等什么、什么时候截止 —— 这也是对局要自己一个 goroutine 的原因。

import (
	"time"

	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

func (g *Game) startRound() {
	if g.round == 1 && g.opt.Wall != nil {
		g.wall = g.opt.Wall // 测试摆好的牌
	} else {
		seed := g.serverSeedFor(g.round)
		g.wall = mahjong.NewWallFromSeed(seed)
	}

	dealt, err := g.wall.Deal()
	if err != nil {
		// 新牌山有 136 张，发 52 张不可能失败；真失败说明规则层被改坏了。
		panic("发牌失败: " + err.Error())
	}
	for seat := range g.hands {
		g.hands[seat] = mahjong.NewHand(dealt[seat])
		g.discards[seat] = nil
	}
	g.turn = g.dealer
	g.drawn = false

	remaining := g.wall.Remaining()
	g.broadcast(fact{typ: protocol.EventGameStarted, forSeat: func(seat int) any {
		// myHand 只给本人 —— 协议 §7.2 核对表的第一行。
		return gameStartedView{
			GameID:       g.opt.GameID,
			RoundIndex:   g.round,
			RoundWind:    "1z",
			HandNumber:   g.honba,
			DealerSeat:   g.dealer,
			MySeat:       seat,
			MyHand:       mahjong.FormatTiles(g.hands[seat].Tiles()),
			Scores:       g.scores,
			RiichiSticks: g.riichiSticks,
			Remaining:    remaining,
			FirstTurn:    g.dealer,
		}
	}})

	g.log.Info("开局", "round", g.round, "dealer", g.dealer, "honba", g.honba)
	g.draw()
}

// draw 当前行动者摸一张。牌山摸空就是荒牌流局 —— 正常业务分支，不是异常。
func (g *Game) draw() {
	t, ok := g.wall.Draw()
	if !ok {
		g.endRoundByExhaustion()
		return
	}

	g.lastDrawn = t
	g.hands[g.turn].Add(t)
	g.drawn = true

	seat, remaining := g.turn, g.wall.Remaining()
	g.broadcast(fact{typ: protocol.EventGameDrew, forSeat: func(viewer int) any {
		// tile 只给摸牌者本人 —— 别家只知道"他摸了一张"，不知道是什么（协议 §7.2）。
		v := gameDrewView{Seat: seat, Remaining: remaining}
		if viewer == seat {
			v.Tile = formatTile(t)
		}
		return v
	}})

	g.deadline = time.Now().Add(g.opt.TurnLimit)
	g.broadcast(fact{typ: protocol.EventGameTurn, data: gameTurnView{
		Seat:       seat,
		DeadlineMs: time.Now().Add(g.opt.TurnBase).UnixMilli(),
	}})
}

func (g *Game) doDiscard(userID int64, t mahjong.Tile) error {
	seat := g.seatOf(userID)
	if seat < 0 {
		return ErrNotInGame
	}
	if seat != g.turn || !g.drawn {
		return ErrNotYourTurn
	}
	if err := g.hands[seat].Remove(t); err != nil {
		return ErrNotHolding
	}

	g.drawn = false
	g.discards[seat] = append(g.discards[seat], t)
	g.deadline = time.Time{}

	g.broadcast(fact{typ: protocol.EventGameDiscarded, data: gameDiscardedView{
		Seat:      seat,
		Tile:      formatTile(t),
		Tsumogiri: t == g.lastDrawn,
		Remaining: g.wall.Remaining(),
	}})

	// 荣和窗口在下一片接进来；现在这张牌没人能鸣，直接过给下家。
	g.turn = (g.turn + 1) % 4
	g.draw()
	return nil
}

func (g *Game) doWin(userID int64) error {
	seat := g.seatOf(userID)
	if seat < 0 {
		return ErrNotInGame
	}
	if seat != g.turn || !g.drawn {
		return ErrNotYourTurn
	}

	in := g.winInput(seat, g.lastDrawn, true)
	res, err := mahjong.Win(in, mahjong.DefaultRules())
	if err != nil {
		return ErrNoWin // 牌型不对 / 无役 / 振听，三种对外合成一个
	}
	g.settleWin(seat, mahjong.NoSeat, g.lastDrawn, true, res)
	return nil
}

// onTurnTimeout 回合超时：按摸切处理（摸到什么打什么）。
//
// 掉线的人卡在这里也不会拖住牌局 —— 这正是"每个等待都必须有超时"那条规矩。
func (g *Game) onTurnTimeout() {
	if !g.drawn {
		return
	}
	g.log.Info("回合超时，摸切", "seat", g.turn)
	// 手上一定有这张：它就是这一摸加进来的。
	if err := g.doDiscard(g.players[g.turn].UserID, g.lastDrawn); err != nil {
		g.log.Info("超时摸切失败", "err", err)
	}
}
