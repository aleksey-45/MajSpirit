package mahjong

// yaku_one_han.go —— 一翻役（docs/规则集.md §3.1）。
// ── 一翻役 ──────────────────────────────────────────────────────────────

func (h *hand) detectOne(hits *[]YakuHit) {
	add := func(y Yaku) {
		if n := y.han(h.menzen()); n > 0 {
			*hits = append(*hits, YakuHit{Yaku: y, Han: n})
		}
	}

	// 状况役。注意岭上/抢杠/海底/河底副露也成立，不是门清限定。
	if h.ctx.IsRiichi {
		add(yakuRiichi)
	}
	if h.ctx.IsIppatsu {
		add(yakuIppatsu)
	}
	if h.ctx.IsRinshan {
		add(yakuRinshan)
	}
	if h.ctx.IsChankan {
		add(yakuChankan)
	}
	if h.ctx.IsHaitei {
		add(yakuHaitei)
	}
	if h.ctx.IsHoutei {
		add(yakuHoutei)
	}
	if h.menzen() && h.ctx.IsTsumo {
		add(yakuMenzenTsumo)
	}

	// 役牌：看刻子，不看雀头。雀头是连风牌只给符数，不是役。
	// 连风牌（场风 == 自风）时两个役都成立，共 2 翻。
	for _, m := range h.tripletLike() {
		t := m.Tile()
		if y, ok := yakuhaiOf(t); ok {
			add(y)
		}
		if isWind(t) && t == h.ctx.RoundWind {
			add(yakuYakuhaiRound)
		}
		if isWind(t) && t == h.ctx.SeatWind {
			add(yakuYakuhaiSeat)
		}
	}

	if h.isTanyao() {
		add(yakuTanyao)
	}
	if h.isPinfu() {
		add(yakuPinfu)
	}
	// 一杯口的「一对」情形；两对及以上在 detectGeqThree 里按二杯口处理。
	if h.menzen() && h.identicalRunPairs() == 1 {
		add(yakuIipeikou)
	}
}

// isTanyao 判定断幺九：全部牌都是中张（2-8），没有幺九牌也没有字牌。
//
// 注意字牌点数是 1-7，所以不能只判 Rank()==1||9——白(5)/发(6)/中(7) 会漏过去。
func (h *hand) isTanyao() bool {
	return allOf(h.tiles(), func(t Tile) bool {
		return t.IsNumber() && t.Rank() >= 2 && t.Rank() <= 8
	})
}

// isPinfu 判定平和。四个条件缺一不可：
// 门清、四面子全顺子、雀头非役牌、两面听。
func (h *hand) isPinfu() bool {
	if !h.menzen() {
		return false
	}
	for _, m := range h.melds {
		if !m.IsSequence() {
			return false
		}
	}
	// 雀头不能是役牌。不能简单排除所有字牌——
	// 东场自风为东时，北雀头是合法的平和。
	if h.isYakuhaiTile(h.pair()) {
		return false
	}
	return h.isRyanmen()
}

// isRyanmen 判定和牌张是否构成两面听。
func (h *hand) isRyanmen() bool {
	t := h.ctx.WinningTile
	for _, m := range h.melds {
		if !m.IsSequence() {
			continue
		}
		a := m.Tile()
		switch {
		case t == a+2: // 手里是 a, a+1，听 a-1 或 a+2
			if a.Rank() >= 2 {
				return true
			}
		case t == a: // 手里是 a+1, a+2，听 a 或 a+3
			if a.Rank() <= 6 {
				return true
			}
		}
	}
	return false
}

// identicalRunPairs 返回「完全相同的顺子」能配成多少对。
func (h *hand) identicalRunPairs() int {
	pairs := 0
	for _, n := range h.runsByStart() {
		pairs += n / 2
	}
	return pairs
}
