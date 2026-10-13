package game

import (
	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

// riichi.go —— 立直（规则集 §3）。
//
// 这一片做的是**宣言、供托、手牌锁定、里宝**。还没做的两条，都要求记住"见过什么"：
//
//   - **一发**（§3.4）：宣言后一圈内和牌才成立，中间任何鸣牌打断它。
//     要的是"宣言之后过了几条舍牌、有没有人鸣过"。
//   - **振听**（§2.2）：现在只有永久振听（自己的舍牌含自己的听牌张）。
//     同一巡振听与立直后振听都要记"这张我听牌了但没和" —— 那是另一张状态表。
//
// 拆开是因为它们各自独立可测；挤在一个提交里，坏了不知道坏在哪。

// riichiBet 是立直的供托（1000 点）。
const riichiBet = 1000

// doRiichi 处理 game.riichi：宣告立直，同时把宣言牌打出去。
//
// 宣言与打牌是**同一个动作**（协议 §5.3 的 game.riichi 带 tile），因为立直的前提就是
// "打掉这张之后听牌"。拆成两条指令的话，中间那一瞬的手牌是个不该存在的状态 ——
// 那时候他既没听牌也没打牌，而别人已经开始看这张牌了。
func (g *Game) doRiichi(userID int64, tile mahjong.Tile) error {
	seat := g.seatOf(userID)
	if seat < 0 {
		return ErrNotInGame
	}
	if g.riichi[seat] {
		return ErrAlreadyRiichi
	}
	if seat != g.turn || !g.awaitingDiscard {
		return ErrNotYourTurn
	}
	// 门清：副露过就不能立直。暗杠不算破门清，所以走规则层的 IsMenzen。
	if !mahjong.IsMenzen(g.melds[seat]) {
		return ErrNotMenzen
	}
	if g.scores[seat] < riichiBet {
		return ErrNoPoints
	}
	// 牌山要剩至少 4 张：剩得比这还少，立直之后根本轮不到他第二次摸牌。
	if g.wall.Remaining() < 4 {
		return ErrWallTooShort
	}
	if !mahjong.IsTenpai(withoutOne(g.hands[seat].Tiles(), tile)) {
		return ErrNotTenpai
	}

	g.riichi[seat] = true
	g.scores[seat] -= riichiBet
	g.riichiSticks++

	g.broadcast(fact{typ: protocol.EventGameRiichi, data: gameRiichiView{
		Seat: seat, Tile: formatTile(tile),
	}})
	g.broadcast(fact{typ: protocol.EventGameRiichiStick, data: gameRiichiStickView{
		Sticks: g.riichiSticks, NewStick: true, Seat: seat,
	}})
	g.log.Info("立直", "seat", seat, "discard", formatTile(tile), "sticks", g.riichiSticks)

	// 宣言牌本身不受"只能摸切"的限制 —— 它是宣言的一部分，可以是手里任何一张。
	return g.discardTile(seat, tile)
}

// withoutOne 返回去掉一张指定牌之后的手牌。
//
// 只删一张：手里有两张一样的时，"打掉这张听不听牌"和"打掉那张听不听牌"是同一个
// 问题，而连删两张会把一个本来能听牌的形删成不听。
func withoutOne(hand []mahjong.Tile, t mahjong.Tile) []mahjong.Tile {
	out := make([]mahjong.Tile, 0, len(hand))
	dropped := false
	for _, x := range hand {
		if !dropped && x == t {
			dropped = true
			continue
		}
		out = append(out, x)
	}
	return out
}
