package game

// match.go —— 一场（半荘）：流局与和了的结算、连庄与本场、下一局、整场结束。
//
// 连庄的规矩全在 advanceRound 里，一句话：**只有子家和牌才清本场**（规则集 §10.3）。

import (
	"time"

	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

func (g *Game) endRoundByExhaustion() {
	g.deadline = time.Time{}

	var tenpai [4]bool
	for seat := range tenpai {
		// 听牌判定与振听无关：振听的人照样算听牌、照样收听牌料（规则集 §11.1）。
		tenpai[seat] = mahjong.IsTenpai(g.hands[seat].Tiles())
	}
	deltas := mahjong.TenpaiPayments(tenpai)

	g.applyDeltas(deltas)
	dealerRepeats := tenpai[g.dealer]
	g.advanceRound(dealerRepeats, false)

	g.broadcast(fact{typ: protocol.EventGameRoundEnd, data: g.roundEndView("draw", nil, deltas, dealerRepeats, tenpai)})
	g.log.Info("荒牌流局", "round", g.round, "tenpai", tenpai, "deltas", deltas)

	g.nextRound()
}

// settleWin 和了：算点、记账、连庄判定，然后进下一局。
func (g *Game) settleWin(winner, loser int, tile mahjong.Tile, tsumo bool, res mahjong.WinResult) {
	g.deadline = time.Time{}

	deltas := res.Deltas
	g.applyDeltas(deltas)
	dealerRepeats := winner == g.dealer
	g.advanceRound(dealerRepeats, !dealerRepeats)

	g.broadcast(fact{typ: protocol.EventGameWinDeclared, data: gameWinDeclaredView{
		WinnerSeat:  winner,
		WinningTile: formatTile(tile),
		IsTsumo:     tsumo,
	}})
	g.broadcast(fact{typ: protocol.EventGameRoundEnd, data: g.roundEndView(
		"win", []roundWinView{g.roundWinView(winner, loser, tile, tsumo, res)}, deltas, dealerRepeats, [4]bool{})})

	g.log.Info("和了", "winner", winner, "tsumo", tsumo,
		"han", res.Han, "fu", res.Fu, "yakuman", res.Yakuman, "deltas", deltas)

	g.nextRound()
}

func (g *Game) applyDeltas(deltas [4]int) {
	for i, d := range deltas {
		g.scores[i] += d
	}
}

// advanceRound 决定谁坐下一局的庄、本场怎么走（规则集 §10.3）。
//
//	庄家和牌 / 庄家听牌流局 → 连庄，本场 +1
//	子家和牌                → 亲移下家，**本场清零**
//	庄家不听流局            → 亲移下家，本场 +1
func (g *Game) advanceRound(dealerRepeats bool, childWon bool) {
	g.honba++
	if dealerRepeats {
		return
	}
	g.dealer = (g.dealer + 1) % 4
	if childWon {
		g.honba = 0
	}
}

func (g *Game) nextRound() {
	g.round++
	if g.round > g.opt.Rounds {
		g.endGame()
		return
	}
	g.startRound()
}

// endGame 整场结束：定顺位、算 M-League ポイント，然后通知房间。
func (g *Game) endGame() {
	placements := placementsOf(g.scores)
	points := [4]int{}
	for seat := range points {
		// 规则集 §10.4：points = (最终分 − **30000**) + 顺位点 × 1000。
		// 原点 30000 不是起始分 25000 —— 写成后者会让每个人的ポイント整体偏高
		// 5000，而四个人之间的**相对**顺序看不出差别，所以这个错很容易活到线上。
		points[seat] = g.scores[seat] - originScore + placementPoints[placements[seat]]*1000
	}

	g.broadcast(fact{typ: protocol.EventGameGameEnd, data: gameGameEndView{
		GameID:      g.opt.GameID,
		FinalScores: g.scores,
		Placements:  placements,
		Points:      points,
		Reason:      "all_last_round",
	}})
	g.log.Info("整场结束", "scores", g.scores, "placements", placements)

	g.deadline = time.Time{}
	if g.opt.OnEnd != nil {
		g.opt.OnEnd()
	}
}
