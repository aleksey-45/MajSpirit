package mahjong

// yaku_hand.go —— 内部合并视图（暗牌拆法 + 副露）与通用判定工具。
import "slices"

// ── 合并视图 ────────────────────────────────────────────────────────────

// hand 把暗牌拆法与副露合成一个视图。
//
// 为什么要合并：**大量役横跨副露与暗牌**——三色同顺可能是一组暗顺 + 两组副露顺，
// 混一色要数全部牌，三杠子要数所有杠。只看 d.Melds 或只看 ctx.Melds 都会漏。
type hand struct {
	d     Decomposition
	ctx   YakuContext
	melds []Meld // 全部面子：暗 + 副露
	all   []Tile // 全部牌（含副露），惰性缓存
}

func newHand(d Decomposition, ctx YakuContext) hand {
	melds := make([]Meld, 0, len(d.Melds)+len(ctx.Melds))
	melds = append(melds, d.Melds...)
	melds = append(melds, ctx.Melds...)
	return hand{d: d, ctx: ctx, melds: melds}
}

func (h *hand) tiles() []Tile {
	if h.all == nil {
		t := h.d.Tiles()
		for _, m := range h.ctx.Melds {
			t = append(t, m.TilesUsed()...)
		}
		slices.Sort(t)
		h.all = t
	}
	return h.all
}

func (h *hand) menzen() bool { return h.ctx.IsMenzen }
func (h *hand) pair() Tile   { return h.d.Pair }

// suitSet 返回手牌里出现过的**数牌**花色，以及是否含字牌。
func (h *hand) suitSet() (map[Suit]bool, bool) {
	suits := map[Suit]bool{}
	hasHonor := false
	for _, t := range h.tiles() {
		if t.IsHonor() {
			hasHonor = true
		} else {
			suits[t.Suit()] = true
		}
	}
	return suits, hasHonor
}

// tripletLike 返回全部「三张一样」的面子（刻子或杠）。
func (h *hand) tripletLike() []Meld {
	var out []Meld
	for _, m := range h.melds {
		if m.IsTripletLike() {
			out = append(out, m)
		}
	}
	return out
}

// runsByStart 统计每个起点的顺子个数。
func (h *hand) runsByStart() map[Tile]int {
	out := map[Tile]int{}
	for _, m := range h.melds {
		if m.IsSequence() {
			out[m.Tile()]++
		}
	}
	return out
}

func isDragon(t Tile) bool { return t.IsHonor() && t.Rank() >= HonorWhite }
func isWind(t Tile) bool   { return t.IsHonor() && t.Rank() <= HonorNorth }
func isTerminal(t Tile) bool {
	return t.IsNumber() && (t.Rank() == 1 || t.Rank() == 9)
}
func isYaochu(t Tile) bool { return isTerminal(t) || t.IsHonor() }

// meldHasYaochu 报告一个面子是否含幺九牌（含字牌）。
func meldHasYaochu(m Meld) bool {
	for _, t := range m.TilesUsed() {
		if isYaochu(t) {
			return true
		}
	}
	return false
}

// yakuhaiOf 返回牌种 t 对应的三元牌役；不是三元牌时返回 false。
func yakuhaiOf(t Tile) (Yaku, bool) {
	switch t.Rank() {
	case HonorWhite:
		if t.IsHonor() {
			return yakuYakuhaiWhite, true
		}
	case HonorGreen:
		if t.IsHonor() {
			return yakuYakuhaiGreen, true
		}
	case HonorRed:
		if t.IsHonor() {
			return yakuYakuhaiRed, true
		}
	}
	return Yaku{}, false
}

// isYakuhaiTile 报告 t 是否为本局的役牌（三元牌、场风、自风）。
func (h *hand) isYakuhaiTile(t Tile) bool {
	if _, ok := yakuhaiOf(t); ok {
		return true
	}
	return isWind(t) && (t == h.ctx.RoundWind || t == h.ctx.SeatWind)
}

func allOf(tiles []Tile, pred func(Tile) bool) bool {
	for _, t := range tiles {
		if !pred(t) {
			return false
		}
	}
	return len(tiles) > 0
}
