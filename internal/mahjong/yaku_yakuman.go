package mahjong

// yaku_yakuman.go —— 役满。命中役满后普通役不再返回（docs/规则集.md §3.4）。
// ── 役满 ────────────────────────────────────────────────────────────────

func (h *hand) yakuman() []YakuHit {
	var hits []YakuHit
	add := func(y Yaku) { hits = append(hits, YakuHit{Yaku: y}) }

	if h.d.Shape == ShapeKokushi {
		add(yakuKokushi)
		return hits
	}

	tiles := h.tiles()
	triplets := h.tripletLike()

	if h.ctx.IsTenhou {
		add(yakuTenhou)
	}
	if h.ctx.IsChiihou {
		add(yakuChiihou)
	}

	// 四暗刻：四组暗刻，且不能有任何副露。
	// 荣和时「和牌那张组成的刻子算明刻」，所以荣和只有单骑（和牌张就是雀头）才成立。
	if h.d.Shape == ShapeStandard && len(h.ctx.Melds) == 0 && len(triplets) == 4 {
		if h.ctx.IsTsumo || h.ctx.WinningTile == h.pair() {
			add(yakuSuuankou)
		}
	}

	if h.countKans() == 4 {
		add(yakuSuukantsu)
	}

	dragons := 0
	for _, m := range triplets {
		if isDragon(m.Tile()) {
			dragons++
		}
	}
	if dragons == 3 {
		add(yakuDaisangen)
	}

	winds := 0
	for _, m := range triplets {
		if isWind(m.Tile()) {
			winds++
		}
	}
	switch {
	case winds == 4:
		add(yakuDaisuushii)
	case winds == 3 && isWind(h.pair()):
		add(yakuShousuushii)
	}

	if allOf(tiles, Tile.IsHonor) {
		add(yakuTsuuiisou)
	}

	// 绿一色：只由 2/3/4/6/8 索构成。M-League 不要求含发。
	if allOf(tiles, func(t Tile) bool {
		if t.Suit() != SuitSou {
			return false
		}
		switch t.Rank() {
		case 2, 3, 4, 6, 8:
			return true
		}
		return false
	}) {
		add(yakuRyuuiisou)
	}

	if allOf(tiles, isTerminal) {
		add(yakuChinroutou)
	}

	if h.isChuuren() {
		add(yakuChuuren)
	}

	return hits
}

func (h *hand) countKans() int {
	n := 0
	for _, m := range h.melds {
		if m.IsQuad() {
			n++
		}
	}
	return n
}

// isChuuren 判定九莲宝灯。只按牌种与张数判，与拆法无关。
func (h *hand) isChuuren() bool {
	if !h.menzen() || len(h.ctx.Melds) > 0 {
		return false
	}
	tiles := h.tiles()
	if len(tiles) != WinningHandSize {
		return false
	}
	suit := tiles[0].Suit()
	if suit == SuitHonor {
		return false
	}
	counts := Counts{}
	for _, t := range tiles {
		if t.Suit() != suit {
			return false
		}
		counts[t]++
	}
	base := [9]int{3, 1, 1, 1, 1, 1, 1, 1, 3}
	extra := 0
	for i, want := range base {
		got := counts[MustTile(suit, i+1)]
		if got < want {
			return false
		}
		extra += got - want
	}
	return extra == 1
}
