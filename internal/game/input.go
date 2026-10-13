package game

// input.go —— 把对局状态翻译成 internal/mahjong 要的输入。
//
// 这一层薄得几乎只是搬字段，但它挡住了两类错：和牌张该不该先加进手牌、
// 自摸时"放铳者"是个哨兵而不是某个座位。

import (
	"crypto/rand"

	"github.com/aleksey-45/majspirit/internal/mahjong"
)

func (g *Game) winInput(seat int, tile mahjong.Tile, tsumo bool) mahjong.WinInput {
	hand := g.hands[seat].Tiles()
	if !tsumo {
		hand = append(hand, tile)
	}

	in := mahjong.WinInput{
		Hand:        hand,
		WinningTile: tile,
		IsTsumo:     tsumo,
		RoundWind:   mahjong.Tile(27), // 东
		SeatWind:    seatWind(seat, g.dealer),
		Furiten:     g.isFuriten(seat),

		WinnerSeat:     seat,
		DealerSeat:     g.dealer,
		Honba:          g.honba,
		IsRinshan:      g.rinshanDraw,
		DoraIndicators: g.wall.DoraIndicators(),
		PaoSeat:        mahjong.NoSeat, // 帕奥责任者还没接（必须在鸣牌那一刻记下）

		// 海底摸月 / 河底捞鱼：牌山摸空之后的最后一张。判据是**可摸的牌山为空**，
		// 不是"这局快结束了" —— 王牌不算，所以不能拿"余牌少"近似。
		IsHaitei: tsumo && g.wall.Empty(),
		IsHoutei: !tsumo && g.wall.Empty(),
	}
	in.RiichiSticks = g.riichiSticks
	if tsumo {
		in.LoserSeat = mahjong.NoSeat
	} else {
		in.LoserSeat = g.turn // 放铳者
	}
	return in
}

// isFuriten 报告是否永久振听：自己的舍牌里有自己当前的听牌张（规则集 §2.2）。
//
// 同巡振听（见逃）与立直后振听要记"见过什么、有没有放过"，那要等立直和鸣牌窗口
// 做完才有意义 —— 现在还没有"放过"这个动作。
func (g *Game) isFuriten(seat int) bool {
	waits := mahjong.Waits(g.hands[seat].Tiles())
	return mahjong.ContainsWait(g.discards[seat], waits)
}

// seatWind 返回座位的自风（东 1 南 2 西 3 北 4 → 编码 27..30）。
func seatWind(seat, dealer int) mahjong.Tile {
	return mahjong.Tile(27 + (seat-dealer+4)%4)
}

// serverSeedFor 派生这一小局的牌山种子。
//
// clientSeeds 目前全零 —— 玩家提交种子那一段（commit-reveal 的前半）还没接。
// 服务器种子赛后要公布，别人才复算得出整副牌山。
func (g *Game) serverSeedFor(round int) [32]byte {
	var server [32]byte
	if g.opt.Seed != nil {
		server = *g.opt.Seed
	} else if _, err := rand.Read(server[:]); err != nil {
		// crypto/rand 失败说明系统熵源坏了，不是可以继续玩的状态。
		panic("取服务器种子失败: " + err.Error())
	}
	return mahjong.DeriveSeed(g.opt.GameID, round, server, [4][32]byte{})
}
