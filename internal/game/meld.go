package game

import (
	"time"

	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

// meld.go —— 鸣牌：吃与碰（协议 §6.6 的 claimWindow options）。
//
// 杠不在这里：**杠要摸岭上牌，而岭上牌在王牌里，`mahjong.Wall` 还没有王牌**。
// 宝牌指示牌同理。两者是同一件事 —— 先把王牌（岭上 4 张 + 宝牌指示 8 张）加进
// 规则层，杠、新宝牌、里宝才能一起做。这条是硬依赖，不是懒。
//
// 优先级：**荣和 > 碰 > 吃**（协议 §6.6），同级之间按"离放铳者由近到远"。
// 所以裁决不能"谁先回答谁赢"—— 得等所有人答完（或窗口到点），再按优先级挑。

// noTile 是"没有这张牌"的哨兵。副露之后没有摸牌，`lastDrawn` 得有个明确的值，
// 否则"摸切"会把上一巡摸到的牌误判成这一张。
const noTile mahjong.Tile = 255

// claimOptions 返回这个座位对这张打出牌能做什么。
//
// 吃只有下家能，碰谁都能 —— 这条不对称是规则的形状，不是实现的选择。
func (g *Game) claimOptions(seat, from int, tile mahjong.Tile) []string {
	hand := g.hands[seat].Tiles()
	var opts []string

	if g.canRon(seat, tile) {
		opts = append(opts, "ron")
	}
	if mahjong.CanPon(hand, tile) {
		opts = append(opts, "pon")
	}
	if mahjong.CanKanOpen(hand, tile) {
		opts = append(opts, "kan")
	}
	// 吃：只有放铳者的下家。
	if seat == (from+1)%4 && len(mahjong.ChiOptions(hand, tile)) > 0 {
		opts = append(opts, "chi")
	}
	if len(opts) == 0 {
		return nil
	}
	return append(opts, "pass")
}

// doPon 处理 game.pon：用手里的两张同种牌碰出刻子。
func (g *Game) doPon(userID int64) error {
	seat := g.seatOf(userID)
	if seat < 0 {
		return ErrNotInGame
	}
	if g.claim == nil || !g.claim.has(seat, "pon") {
		return ErrNoClaim
	}
	g.claim.pon[seat] = true
	g.claim.answered[seat] = true
	g.maybeResolveClaim()
	return nil
}

// doChi 处理 game.chi：下家用两张牌吃成顺子。tiles 是**手里那两张**，含哪两张由客户端说。
//
// 让客户端指定而不由服务端挑，是因为手里可能有好几种吃法，挑哪一种只有玩家知道。
func (g *Game) doChi(userID int64, tiles [2]mahjong.Tile) error {
	seat := g.seatOf(userID)
	if seat < 0 {
		return ErrNotInGame
	}
	if g.claim == nil || !g.claim.has(seat, "chi") {
		return ErrNoClaim
	}
	hand := g.hands[seat].Tiles()
	if !validChi(hand, tiles, g.claim.tile) {
		return ErrNotHolding
	}
	g.claim.chi[seat] = tiles
	g.claim.answered[seat] = true
	g.maybeResolveClaim()
	return nil
}

// validChi 校验这两张牌确实在手里、而且和被打出的牌构成顺子。
//
// 用 ChiOptions 的返回值比对，不自己再写一遍"是不是顺子"—— 那种复述早晚和
// 规则层漂开，而漂开的方向是"服务端接受了非法的一吃"。
func validChi(hand []mahjong.Tile, tiles [2]mahjong.Tile, called mahjong.Tile) bool {
	if !contains(hand, tiles[0]) || !contains(hand, tiles[1]) {
		return false
	}
	for _, opt := range mahjong.ChiOptions(hand, called) {
		if (opt[0] == tiles[0] && opt[1] == tiles[1]) ||
			(opt[0] == tiles[1] && opt[1] == tiles[0]) {
			return true
		}
	}
	return false
}

// contains 是**精确**匹配：赤五和普通五是两张不同的牌，玩家说吃哪张就得吃哪张。
func contains(hand []mahjong.Tile, t mahjong.Tile) bool {
	for _, x := range hand {
		if x == t {
			return true
		}
	}
	return false
}

// applyPon 把碰落到状态上：手里去掉两张、刻子进副露、被打出的牌归鸣牌者。
func (g *Game) applyPon(seat int, tile mahjong.Tile) mahjong.Meld {
	src := g.claimSource(seat, tile)
	for range 2 {
		g.hands[seat].Remove(src)
	}
	return mahjong.Meld{Kind: mahjong.MeldTriplet, IsOpen: true,
		Tiles: [4]mahjong.Tile{src, src, src, tile}}
}

// applyChi 把吃落到状态上。顺子按点数排好，免得"吃 4 万用 2、3 万"和
// "吃 4 万用 3、5 万"在数据里长得不一样。
func (g *Game) applyChi(seat int, tiles [2]mahjong.Tile, called mahjong.Tile) mahjong.Meld {
	for _, t := range tiles {
		g.hands[seat].Remove(t)
	}
	all := []mahjong.Tile{tiles[0], tiles[1], called}
	sortByKind(all)
	return mahjong.Meld{Kind: mahjong.MeldRun, IsOpen: true,
		Tiles: [4]mahjong.Tile{all[0], all[1], all[2]}}
}

// claimSource 返回手里那张"和被打出的牌同种"的具体牌 —— 碰要用它，
// 而不是用被打出的那一张：赤五进副露还是留在手里，是两张不同的牌。
func (g *Game) claimSource(seat int, tile mahjong.Tile) mahjong.Tile {
	for _, x := range g.hands[seat].Tiles() {
		if x.Kind() == tile.Kind() {
			return x
		}
	}
	return tile
}

func sortByKind(ts []mahjong.Tile) {
	for i := 1; i < len(ts); i++ {
		for j := i; j > 0 && ts[j].Kind() < ts[j-1].Kind(); j-- {
			ts[j], ts[j-1] = ts[j-1], ts[j]
		}
	}
}

// has 报告这个座位在这个窗口里有没有某项选择。
func (c *claim) has(seat int, what string) bool {
	for _, o := range c.options[seat] {
		if o == what {
			return true
		}
	}
	return false
}

// beginMeldTurn 鸣牌之后的打牌权：**不摸牌**，直接打。
//
// 这就是"鸣完由谁打"那条规矩 —— 吃碰之后轮到鸣牌者，而且他必须打一张。
func (g *Game) beginMeldTurn(seat int) {
	g.turn = seat
	g.drawn = false // 没摸牌，所以不能自摸
	g.awaitingDiscard = true
	g.lastDrawn = noTile // 没有摸切可言
	g.deadline = time.Now().Add(g.opt.TurnLimit)
	g.broadcast(fact{typ: protocol.EventGameTurn, data: gameTurnView{
		Seat:       seat,
		DeadlineMs: time.Now().Add(g.opt.TurnBase).UnixMilli(),
	}})
}

// growMelds 把副露记进状态并广播。
func (g *Game) growMelds(seat int, m mahjong.Meld, from int, taken mahjong.Tile) {
	g.melds[seat] = append(g.melds[seat], m)
	g.broadcast(fact{typ: protocol.EventGameMeld, data: gameMeldView{
		Seat:     seat,
		Kind:     meldKindName(m.Kind),
		Tiles:    formatTiles(m.TilesUsed()),
		FromSeat: from,
		Taken:    formatTile(taken),
	}})
	g.log.Info("鸣牌", "seat", seat, "kind", meldKindName(m.Kind),
		"tiles", formatTiles(m.TilesUsed()))
	// 帕奥责任就在**这一刻**记下：副露里只留下"有一组白板"，看不出那张牌是谁打的。
	// 只有副露（碰/明杠）才可能有责任者 —— 暗杠与加杠没有放铳者。
	if m.IsOpen {
		g.notePao(seat, from)
	}
}

func meldKindName(k mahjong.MeldKind) string {
	switch k {
	case mahjong.MeldRun:
		return "chi"
	case mahjong.MeldTriplet:
		return "pon"
	case mahjong.MeldKan:
		return "kan"
	default:
		return "pair"
	}
}

// sameKindIn 报告 list 里有没有和 t **同种**的牌。
//
// 用种类而不是精确相等：规则层给的候选（ClosedKanTiles / AddedKanTiles）是按种类
// 算出来的，赤五和普通五是同一种。
func sameKindIn(list []mahjong.Tile, t mahjong.Tile) bool {
	for _, x := range list {
		if x.Kind() == t.Kind() {
			return true
		}
	}
	return false
}
