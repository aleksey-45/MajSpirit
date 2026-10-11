package mahjong

import "testing"

// fu_test.go —— 符数计算。规则见 docs/规则集.md §5，验收清单见 docs/local/实现指南.md §1.7。
//
// 期望值来源：
//   - §5 的加符表（副底 20、面子符、雀头符、待ち符、门清荣和、自摸）
//   - §12 例 4 的逐项拆解（36 → 40）——文档把每一项都写出来了，是最好的一条
//   - §5 明写「连风牌也只算 2 符」这条 M-League 特例

// 带副露的手牌要分开给：Decompose 只收暗牌。
func fuOfWithMelds(t *testing.T, concealed string, melds []Meld, ctx YakuContext) int {
	t.Helper()
	decs, err := Decompose(MustParseTiles(concealed))
	if err != nil {
		t.Fatalf("Decompose(%q): %v", concealed, err)
	}
	if len(decs) != 1 {
		t.Fatalf("Decompose(%q) 得到 %d 种拆法，期望恰好 1 种（夹具必须无歧义）",
			concealed, len(decs))
	}
	ctx.Melds = melds
	return CalcFu(decs[0], ctx)
}

// 无副露的手牌。
func fuOf(t *testing.T, hand string, ctx YakuContext) int {
	t.Helper()
	return fuOfWithMelds(t, hand, nil, ctx)
}

func openMeld(kind MeldKind, tiles ...Tile) Meld {
	m := Meld{Kind: kind, IsOpen: true}
	copy(m.Tiles[:], tiles)
	return m
}

func closedMeld(kind MeldKind, tiles ...Tile) Meld {
	m := Meld{Kind: kind}
	copy(m.Tiles[:], tiles)
	return m
}

// 面子符的完整表：明/暗 × 中张/幺九 × 刻子/杠。
//
// 这里直接测未导出的 meldFu，和 BasePoints 被导出供测试直接用是同一个理由：
// 这张表本身就是规格（§5），不是实现结构。绕过它去测 CalcFu 的话，
// 10 符取整会把 2/4/4/8 的差别全部抹平——四种组合累加后都是 30 符。
func TestMeldFuTable(t *testing.T) {
	chuchan, yaochu := MustTile(SuitMan, 2), MustTile(SuitMan, 1)

	tests := []struct {
		name      string
		kind      MeldKind
		tile      Tile
		concealed bool
		want      int
	}{
		{"明刻·中张", MeldTriplet, chuchan, false, 2},
		{"暗刻·中张", MeldTriplet, chuchan, true, 4},
		{"明刻·幺九", MeldTriplet, yaochu, false, 4},
		{"暗刻·幺九", MeldTriplet, yaochu, true, 8},

		{"明杠·中张", MeldKan, chuchan, false, 8},
		{"暗杠·中张", MeldKan, chuchan, true, 16},
		{"明杠·幺九", MeldKan, yaochu, false, 16},
		{"暗杠·幺九", MeldKan, yaochu, true, 32},

		// 顺子恒为 0，与明暗、中张幺九都无关
		{"明顺子", MeldRun, chuchan, false, 0},
		{"暗顺子·幺九起", MeldRun, yaochu, true, 0},

		// 对子不参与面子符（七对子是固定 25 符，走另一条路）
		{"对子", MeldPair, chuchan, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := 3
			if tt.kind == MeldKan {
				n = 4
			} else if tt.kind == MeldPair {
				n = 2
			}
			m := Meld{Kind: tt.kind}
			for i := 0; i < n; i++ {
				m.Tiles[i] = tt.tile
			}
			if got := meldFu(m, tt.concealed); got != tt.want {
				t.Errorf("meldFu(%v, concealed=%v) = %d，期望 %d",
					tt.kind, tt.concealed, got, tt.want)
			}
		})
	}
}

// 副底 20：全顺子 + 非役牌雀头 + 两面听 + 非门清 → 恰好 20 符。
func TestCalcFuBaseIs20(t *testing.T) {
	got := fuOf(t, "234m567m234s456s88p", YakuContext{
		WinningTile: MustTile(SuitSou, 4), // 两面
	})
	if got != 20 {
		t.Errorf("全顺子两面听非门清 = %d 符，期望 20（只有副底）", got)
	}
}

// 门清荣和 +10。同一副牌只翻这一个布尔量，30 - 20 = 10。
func TestCalcFuMenzenRonBonus(t *testing.T) {
	hand := "234m567m234s456s88p"
	win := MustTile(SuitSou, 4)

	closed := fuOf(t, hand, YakuContext{WinningTile: win, IsMenzen: true})
	open := fuOf(t, hand, YakuContext{WinningTile: win})

	if open != 20 {
		t.Errorf("非门清 = %d 符，期望 20", open)
	}
	if closed != 30 {
		t.Errorf("门清荣和 = %d 符，期望 30（副底 20 + 门清荣和 10）", closed)
	}
}

// 自摸 +2。用非平和的手牌，免得撞上「平和自摸固定 20」那条特例。
//
// 期望值刻意选在跨过 10 符取整边界的位置：30 → 30，32 → 40。
// 如果只差 2 符但两边都落在同一个十位区间，这条测试就测不出东西。
func TestCalcFuTsumoBonus(t *testing.T) {
	// 暗刻 111m(幺九 8) + 明刻 222p(中张 2) + 两顺子 + 雀头 55p
	concealed := "111m345s678s55p"
	ctx := YakuContext{WinningTile: MustTile(SuitSou, 3)} // 345s 的两面
	melds := []Meld{openMeld(MeldTriplet, MustTile(SuitPin, 2), MustTile(SuitPin, 2), MustTile(SuitPin, 2))}

	ctx.IsTsumo = false
	ron := fuOfWithMelds(t, concealed, melds, ctx)
	ctx.IsTsumo = true
	tsumo := fuOfWithMelds(t, concealed, melds, ctx)

	if ron != 30 {
		t.Errorf("荣和 = %d 符，期望 30（副底 20 + 暗刻幺九 8 + 明刻中张 2）", ron)
	}
	if tsumo != 40 {
		t.Errorf("自摸 = %d 符，期望 40（32 取整）；"+
			"若得到 30 说明自摸的 +2 丢了，或取整在累加之前就做了", tsumo)
	}
}

// 平和自摸固定 20 符，不加自摸的 2 符。
func TestCalcFuPinfuTsumoIs20(t *testing.T) {
	hand := "234m567m234s456s88p"
	win := MustTile(SuitSou, 4)

	// 先确认这手牌确实是平和：门清、四顺子、雀头非役牌、两面听
	decs, err := Decompose(MustParseTiles(hand))
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	ctx := YakuContext{WinningTile: win, IsMenzen: true, IsTsumo: true}
	if h := newHand(decs[0], ctx); !h.isPinfu() {
		t.Fatal("夹具不是平和，这条测试就没有意义了")
	}

	if got := CalcFu(decs[0], ctx); got != 20 {
		t.Errorf("平和自摸 = %d 符，期望 20（不是 22，也不是 30）", got)
	}
}

// 七对子固定 25 符，不取整，而且不看和牌张/自摸/门清。
func TestCalcFuChiitoitsuIs25(t *testing.T) {
	// 七对子专用夹具：全部是对子且组不出任何顺子，保证只有一种拆法
	const hand = "1188m1199s1199p11z"

	ctxs := []YakuContext{
		{WinningTile: MustTile(SuitMan, 1), IsMenzen: true},
		{WinningTile: MustTile(SuitMan, 1), IsMenzen: true, IsTsumo: true},
		{WinningTile: MustTile(SuitHonor, HonorEast)},
	}
	for i, ctx := range ctxs {
		if got := fuOf(t, hand, ctx); got != 25 {
			t.Errorf("第 %d 组：七对子 = %d 符，期望 25", i, got)
		}
	}
}

// 国士无双是役满，符数不参与计算。
func TestCalcFuKokushiIs0(t *testing.T) {
	got := fuOf(t, "19m19s19p12345677z", YakuContext{WinningTile: MustTile(SuitMan, 1)})
	if got != 0 {
		t.Errorf("国士无双 = %d 符，期望 0（役满不看符数）", got)
	}
}

// 荣和的那张牌补成的刻子算明刻（§5）。
//
// §12 例 4 抓不住这一条，因为那条的合计是 36，掺进这一条错误后变成 38，
// 两者都取整到 40。所以专门造一个卡在取整边界上的夹具：
//
//	副底 20 + 111m 明刻幺九 4 + 999m 暗刻幺九 8 + 222s 暗刻中张 4 + 55z 雀头白 2 = 40
//
// 把 333s 错算成暗刻会变成 42 → 50，一眼就能看见。
func TestCalcFuRonCompletedTripletIsOpen(t *testing.T) {
	concealed := "999m222s333s55z"
	melds := []Meld{openMeld(MeldTriplet,
		MustTile(SuitMan, 1), MustTile(SuitMan, 1), MustTile(SuitMan, 1))}

	got := fuOfWithMelds(t, concealed, melds, YakuContext{
		WinningTile: MustTile(SuitSou, 3), // 荣和 3s，补成了 333s
		RoundWind:   MustTile(SuitHonor, HonorEast),
		SeatWind:    MustTile(SuitHonor, HonorSouth),
	})

	if got != 40 {
		t.Errorf("荣和补成的刻子 = %d 符，期望 40；"+
			"得到 50 说明那张牌组成的 333s 被算成了暗刻（该 +2 的给了 +4）", got)
	}

	// 反向对照：自摸时同一个 333s 是暗刻，因为和牌张不是从别人手里来的。
	// 这时加上自摸的 2 符：20 + 4 + 8 + 4 + 4 + 2 + 2(自摸) = 44 → 50。
	tsumo := fuOfWithMelds(t, concealed, melds, YakuContext{
		WinningTile: MustTile(SuitSou, 3),
		IsTsumo:     true,
		RoundWind:   MustTile(SuitHonor, HonorEast),
		SeatWind:    MustTile(SuitHonor, HonorSouth),
	})
	if tsumo != 50 {
		t.Errorf("自摸时同一手牌 = %d 符，期望 50（333s 算暗刻，再加自摸 2 符）", tsumo)
	}
}

// §12 例 4：副露·对对和。文档把每一项都写出来了，逐项核对。
//
//	手牌（副露 2 组）：111m(碰) 999m(碰) 222s 333s 55z   荣和 3s
//	副底 20 + 111m 明刻幺九 4 + 999m 明刻幺九 4 + 222s 暗刻中张 4
//	    + 333s 荣和的牌组成→明刻中张 2 + 55z 雀头·白 2 + 双碰待ち 0 = 36 → 40
func TestCalcFuWorkedExample4(t *testing.T) {
	// 暗牌部分：222s 333s 55z（8 张 = 3×2 + 2）
	concealed := "222s333s55z"
	melds := []Meld{
		openMeld(MeldTriplet, MustTile(SuitMan, 1), MustTile(SuitMan, 1), MustTile(SuitMan, 1)),
		openMeld(MeldTriplet, MustTile(SuitMan, 9), MustTile(SuitMan, 9), MustTile(SuitMan, 9)),
	}

	got := fuOfWithMelds(t, concealed, melds, YakuContext{
		WinningTile: MustTile(SuitSou, 3), // 荣和 3s：它补上的 333s 算明刻
		RoundWind:   MustTile(SuitHonor, HonorEast),
		SeatWind:    MustTile(SuitHonor, HonorSouth),
	})

	if got != 40 {
		t.Errorf("例 4 = %d 符，期望 40（各项合计 36 后取整）\n"+
			"常见错误：荣和补成的刻子算成暗刻 → 38 仍是 40，但换成 2s 荣和就会露出来；\n"+
			"或雀头 55z（白）的 +2 漏掉 → 34 → 40，也看不出来", got)
	}

	// 反向对照：和牌张换成 2s，则 222s 变成明刻、333s 保持暗刻。
	// 两者的面子符相同（都是中张、一明一暗），但位置互换了 ——
	// 如果 isConcealedMeld 只按「是不是和牌张」判断而不管在哪一组，这条会一样；
	// 如果它写成了「所有刻子都算暗」，上面那条就会失败。
	swapped := fuOfWithMelds(t, concealed, melds, YakuContext{
		WinningTile: MustTile(SuitSou, 2),
		RoundWind:   MustTile(SuitHonor, HonorEast),
		SeatWind:    MustTile(SuitHonor, HonorSouth),
	})
	if swapped != 40 {
		t.Errorf("和牌张换成 2s 后 = %d 符，期望 40（对称情形）", swapped)
	}
}

// 连风牌雀头只算 2 符，不是 4（M-League 特例，见 §14.2）。
//
// 期望值同样选在取整边界上：38 + 2 = 40 → 40，38 + 4 = 42 → 50。
func TestCalcFuDoubleWindPairIsOnly2(t *testing.T) {
	// 111m 暗刻幺九 8 + 三顺子 + 東東雀头（场风自风都是东）
	hand := "111m234s567s456p11z"
	ethan := MustTile(SuitHonor, HonorEast)

	got := fuOf(t, hand, YakuContext{
		WinningTile: MustTile(SuitSou, 4), // 234s 的两面
		IsMenzen:    true,
		RoundWind:   ethan,
		SeatWind:    ethan, // 连风
	})

	// 副底 20 + 暗刻幺九 8 + 连风雀头 2 + 门清荣和 10 = 40
	if got != 40 {
		t.Errorf("连风牌雀头 = %d 符，期望 40；"+
			"得到 50 说明连风被算成了 4 符（场风 2 + 自风 2）", got)
	}
}

// 待ち符：单骑/嵌张/边张各 +2，两面与双碰 +0。
//
// 表格特意用同一副暗牌 + 不同和牌张，让唯一的变量就是待ち形。
// IsMenzen 一律为 false，把门清荣和的 +10 摘掉，20 / 30 的对比才干净。
func TestCalcFuWaitShapes(t *testing.T) {
	tests := []struct {
		name string
		hand string
		win  string
		want int
	}{
		// 234s 456s 里的 456s：手里 56s 和 4s → 两面；手里 46s 和 5s → 嵌张
		{"两面 4s", "234m567m234s456s88p", "4s", 20},
		{"两面 7s", "234m567m234s456s88p", "7s", 20},
		{"嵌张 5s", "234m567m234s456s88p", "5s", 30},
		{"单骑 8p", "234m567m234s456s88p", "8p", 30},

		// 123m：手里 23m 和 1m → 两面；手里 12m 和 3m → 边张；手里 13m 和 2m → 嵌张
		{"边张 3m", "123m567m234s456s88p", "3m", 30},
		{"两面 1m", "123m567m234s456s88p", "1m", 20},
		{"嵌张 2m", "123m567m234s456s88p", "2m", 30},

		// 789m：手里 78m 和 9m → 两面；手里 89m 和 7m → 边张
		{"边张 7m", "789m234s567s456p88p", "7m", 30},
		{"两面 9m", "789m234s567s456p88p", "9m", 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var win Tile
			for _, s := range []string{tt.win} {
				win = MustParseTiles(s)[0]
			}
			got := fuOf(t, tt.hand, YakuContext{WinningTile: win})
			if got != tt.want {
				t.Errorf("%s = %d 符，期望 %d", tt.name, got, tt.want)
			}
		})
	}
}
