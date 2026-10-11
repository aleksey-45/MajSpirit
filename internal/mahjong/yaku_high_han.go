package mahjong

// yaku_high_han.go —— 二翻、三翻及六翻役（docs/规则集.md §3.2、§3.3）。
// ── 二翻役 ──────────────────────────────────────────────────────────────

func (h *hand) detectTwo(hits *[]YakuHit) {
	add := func(y Yaku) {
		if n := y.han(h.menzen()); n > 0 {
			*hits = append(*hits, YakuHit{Yaku: y, Han: n})
		}
	}

	if h.d.Shape == ShapeChiitoitsu {
		add(yakuChiitoitsu)
	} else {
		if h.isToitoi() {
			add(yakuToitoi)
		}
		if h.concealedTripletCount() >= 3 {
			add(yakuSanankou)
		}
		if h.countKans() >= 3 {
			add(yakuSankantsu)
		}
		if h.isSanshokuDoukou() {
			add(yakuSanshokuDoukou)
		}
		if h.isShousangen() {
			add(yakuShousangen)
		}
	}

	if h.isHonroutou() {
		add(yakuHonroutou)
	}
	if h.isSanshokuDoujun() {
		add(yakuSanshokuDoujun)
	}
	if h.isIkkitsuukan() {
		add(yakuIkkitsuukan)
	}
	if h.isChanta() {
		add(yakuChanta)
	}
}

// concealedTripletCount 返回暗刻的个数。
//
// 荣和时「和牌那张组成的刻子算明刻」：和牌张不是雀头时，包含它的那个刻子在荣和时算明。
func (h *hand) concealedTripletCount() int {
	n := 0
	for _, m := range h.d.Melds { // 暗牌拆出来的面子（副露不在 d.Melds 里）
		if !m.IsTripletLike() {
			continue
		}
		if !h.ctx.IsTsumo && m.Tile() == h.ctx.WinningTile && m.Tile() != h.pair() {
			continue
		}
		n++
	}
	return n
}

func (h *hand) isToitoi() bool {
	if h.d.Shape != ShapeStandard || len(h.melds) != 4 {
		return false
	}
	for _, m := range h.melds {
		if !m.IsTripletLike() {
			return false
		}
	}
	return true
}

func (h *hand) isSanshokuDoukou() bool {
	seen := map[int]map[Suit]bool{}
	for _, m := range h.tripletLike() {
		t := m.Tile()
		if t.IsHonor() {
			continue
		}
		if seen[t.Rank()] == nil {
			seen[t.Rank()] = map[Suit]bool{}
		}
		seen[t.Rank()][t.Suit()] = true
	}
	for _, suits := range seen {
		if len(suits) == 3 {
			return true
		}
	}
	return false
}

func (h *hand) isSanshokuDoujun() bool {
	seen := map[int]map[Suit]bool{}
	for _, m := range h.melds {
		if !m.IsSequence() {
			continue
		}
		t := m.Tile()
		if seen[t.Rank()] == nil {
			seen[t.Rank()] = map[Suit]bool{}
		}
		seen[t.Rank()][t.Suit()] = true
	}
	for _, suits := range seen {
		if len(suits) == 3 {
			return true
		}
	}
	return false
}

func (h *hand) isIkkitsuukan() bool {
	starts := h.runsByStart()
	for _, suit := range []Suit{SuitMan, SuitSou, SuitPin} {
		if starts[MustTile(suit, 1)] > 0 && starts[MustTile(suit, 4)] > 0 &&
			starts[MustTile(suit, 7)] > 0 {
			return true
		}
	}
	return false
}

func (h *hand) isShousangen() bool {
	dragons := 0
	for _, m := range h.tripletLike() {
		if isDragon(m.Tile()) {
			dragons++
		}
	}
	return dragons == 2 && isDragon(h.pair())
}

func (h *hand) isHonroutou() bool {
	return allOf(h.tiles(), isYaochu)
}

// isChanta 判定混全带幺九：每个面子与雀头都含幺九牌，且有字牌。
// 无字牌的那种是纯全带幺九（见 isJunchan），两者互斥。
func (h *hand) isChanta() bool {
	if _, hasHonor := h.suitSet(); !hasHonor {
		return false
	}
	return h.allSetsHaveYaochu()
}

func (h *hand) isJunchan() bool {
	if _, hasHonor := h.suitSet(); hasHonor {
		return false
	}
	return h.allSetsHaveYaochu()
}

func (h *hand) allSetsHaveYaochu() bool {
	if !isYaochu(h.pair()) {
		return false
	}
	for _, m := range h.melds {
		if !meldHasYaochu(m) {
			return false
		}
	}
	return len(h.melds) > 0
}

// ── 三翻及六翻 ──────────────────────────────────────────────────────────

func (h *hand) detectGeqThree(hits *[]YakuHit) {
	add := func(y Yaku) {
		if n := y.han(h.menzen()); n > 0 {
			*hits = append(*hits, YakuHit{Yaku: y, Han: n})
		}
	}

	if h.menzen() && h.identicalRunPairs() >= 2 {
		add(yakuRyanpeikou)
	}
	if h.isJunchan() {
		add(yakuJunchan)
	}

	suits, hasHonor := h.suitSet()
	switch {
	case len(suits) == 1 && !hasHonor:
		add(yakuChinitsu)
	case len(suits) == 1 && hasHonor:
		add(yakuHonitsu)
	}
}
