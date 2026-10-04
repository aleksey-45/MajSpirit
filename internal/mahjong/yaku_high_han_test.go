package mahjong

import "testing"

// 本文件补齐 yaku_test.go 没覆盖到的役：二翻、三翻及六翻，以及其余役满。
// 仍然只用**拆法唯一**的手牌（judge 会强制这一点）。

// openKan 构造一个副露的杠。
func openKan(t Tile) Meld {
	return Meld{Kind: MeldKan, Tiles: [4]Tile{t, t, t, t}, IsOpen: true}
}

// openPon 构造一个副露的碰。
func openPon(t Tile) Meld {
	return Meld{Kind: MeldTriplet, Tiles: [4]Tile{t, t, t}, IsOpen: true}
}

// ── 二翻役 ──────────────────────────────────────────────────────────────

func TestToitoiAndSanankou(t *testing.T) {
	// 三组暗刻 + 一组副露碰 + 雀头
	ctx := YakuContext{
		Melds:     []Meld{openPon(MustTile(SuitSou, 4))},
		RoundWind: MustTile(SuitHonor, HonorEast),
		SeatWind:  MustTile(SuitHonor, HonorEast),
	}
	hits := judge(t, "111m222m444m44p", ctx)

	if !has(hits, "对对和") {
		t.Errorf("四组刻子应成立对对和；实际命中 %v", names(hits))
	}
	if !has(hits, "三暗刻") {
		t.Errorf("三组暗刻应成立三暗刻；实际命中 %v", names(hits))
	}
}

func TestSankantsuAndSuukantsu(t *testing.T) {
	t.Run("三杠子", func(t *testing.T) {
		ctx := YakuContext{
			Melds: []Meld{
				openKan(MustTile(SuitMan, 1)),
				openKan(MustTile(SuitSou, 5)),
				openKan(MustTile(SuitPin, 9)),
			},
			RoundWind: MustTile(SuitHonor, HonorEast),
			SeatWind:  MustTile(SuitHonor, HonorEast),
		}
		// 5 张暗牌 = 1 面子 + 雀头
		hits := judge(t, "123m44p", ctx)
		if !has(hits, "三杠子") {
			t.Errorf("三个杠应成立三杠子；实际命中 %v", names(hits))
		}
	})

	t.Run("四杠子", func(t *testing.T) {
		ctx := YakuContext{
			Melds: []Meld{
				openKan(MustTile(SuitMan, 1)),
				openKan(MustTile(SuitSou, 5)),
				openKan(MustTile(SuitPin, 9)),
				openKan(MustTile(SuitHonor, HonorWhite)),
			},
			RoundWind: MustTile(SuitHonor, HonorEast),
			SeatWind:  MustTile(SuitHonor, HonorEast),
		}
		// 2 张暗牌 = 只剩雀头
		hits := judge(t, "44p", ctx)
		if !has(hits, "四杠子") {
			t.Fatalf("四个杠应成立四杠子；实际命中 %v", names(hits))
		}
		if hits[0].Yaku.Yakuman != 1 {
			t.Errorf("四杠子是 1 倍役满（M-League 没有双倍），实际 %d 倍", hits[0].Yaku.Yakuman)
		}
	})
}

func TestSanshokuDoukou(t *testing.T) {
	// 万/索/筒各一组 111 刻子
	hits := judge(t, "111m111s111p234m55z", menzen())

	if !has(hits, "三色同刻") {
		t.Errorf("三花色同数字刻子应成立三色同刻；实际命中 %v", names(hits))
	}
}

func TestShousangen(t *testing.T) {
	// 白刻子 + 发刻子 + 中雀头
	hits := judge(t, "555z666z77z234m567m", menzen())

	if !has(hits, "小三元") {
		t.Errorf("两组三元刻子 + 三元雀头应成立小三元；实际命中 %v", names(hits))
	}
	if has(hits, "大三元") {
		t.Errorf("只有两组三元刻子，不该成立大三元；实际命中 %v", names(hits))
	}
}

// 混老头：只有幺九牌与字牌，与七对子复合。
func TestHonroutou(t *testing.T) {
	hits := judge(t, "11m99m11s99s11p99p11z", menzen())

	if !has(hits, "混老头") {
		t.Errorf("全是幺九与字牌应成立混老头；实际命中 %v", names(hits))
	}
	if !has(hits, "七对子") {
		t.Errorf("应同时成立七对子；实际命中 %v", names(hits))
	}
	if has(hits, "清老头") {
		t.Errorf("含字牌不该成立清老头；实际命中 %v", names(hits))
	}
}

func TestChantaVsJunchan(t *testing.T) {
	t.Run("含字牌是混全带幺九", func(t *testing.T) {
		// 每组面子与雀头都含幺九，且有字牌
		hits := judge(t, "123m789m123s789s11z", menzen())
		if !has(hits, "混全带幺九") {
			t.Errorf("应成立混全带幺九；实际命中 %v", names(hits))
		}
		if has(hits, "纯全带幺九") {
			t.Errorf("含字牌不该成立纯全带幺九；实际命中 %v", names(hits))
		}
	})

	t.Run("无字牌是纯全带幺九", func(t *testing.T) {
		hits := judge(t, "123m789m123s789s99p", menzen())
		if !has(hits, "纯全带幺九") {
			t.Errorf("应成立纯全带幺九；实际命中 %v", names(hits))
		}
		if has(hits, "混全带幺九") {
			t.Errorf("无字牌不该成立混全带幺九；实际命中 %v", names(hits))
		}
	})
}

// ── 三翻及六翻 ──────────────────────────────────────────────────────────

// 二杯口与一杯口**不叠加**：两对同型顺子只算二杯口 3 翻。
func TestRyanpeikouExcludesIipeikou(t *testing.T) {
	ctx := menzen()
	ctx.WinningTile = MustTile(SuitPin, 5)
	hits := judge(t, "123m123m234m234m55p", ctx)

	if got := hanOf(hits, "二杯口"); got != 3 {
		t.Errorf("二杯口应为 3 翻，实际 %d；命中 %v", got, names(hits))
	}
	if has(hits, "一杯口") {
		t.Errorf("二杯口成立时不该再算一杯口；实际命中 %v", names(hits))
	}
}

func TestHonitsuAndChinitsu(t *testing.T) {
	tests := []struct {
		name     string
		hand     string
		want     string
		wantHan  int
		notYakus []string
	}{
		{
			name: "混一色（一种花色 + 字牌）", hand: "123m456m111z222z33z",
			want: "混一色", wantHan: 3, notYakus: []string{"清一色"},
		},
		{
			name: "清一色（只有一种花色）", hand: "123m456m789m234m55m",
			want: "清一色", wantHan: 6, notYakus: []string{"混一色"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := judge(t, tt.hand, menzen())
			if got := hanOf(hits, tt.want); got != tt.wantHan {
				t.Errorf("%s 应为 %d 翻，实际 %d；命中 %v", tt.want, tt.wantHan, got, names(hits))
			}
			for _, n := range tt.notYakus {
				if has(hits, n) {
					t.Errorf("不该同时命中 %q；实际命中 %v", n, names(hits))
				}
			}
		})
	}
}

// ── 其余役满 ────────────────────────────────────────────────────────────

func TestSuuankouRequiresTsumoOrTanki(t *testing.T) {
	const hand = "111m222m444s555s55p" // 四组暗刻 + 雀头

	t.Run("荣和单骑成立", func(t *testing.T) {
		ctx := menzen()
		ctx.WinningTile = MustTile(SuitPin, 5) // 和牌张就是雀头 → 单骑
		hits := judge(t, hand, ctx)
		if !has(hits, "四暗刻") {
			t.Errorf("单骑荣和应成立四暗刻；实际命中 %v", names(hits))
		}
	})

	t.Run("荣和非单骑不成立", func(t *testing.T) {
		ctx := menzen()
		ctx.WinningTile = MustTile(SuitSou, 5) // 和牌张补成刻子 → 那组算明刻
		hits := judge(t, hand, ctx)
		if has(hits, "四暗刻") {
			t.Errorf("荣和非单骑时那组刻子算明刻，不该成立四暗刻；实际命中 %v", names(hits))
		}
		if !has(hits, "三暗刻") {
			t.Errorf("应降为三暗刻；实际命中 %v", names(hits))
		}
	})

	t.Run("自摸成立", func(t *testing.T) {
		ctx := menzen()
		ctx.IsTsumo = true
		ctx.WinningTile = MustTile(SuitSou, 4)
		hits := judge(t, hand, ctx)
		if !has(hits, "四暗刻") {
			t.Errorf("自摸应成立四暗刻；实际命中 %v", names(hits))
		}
	})
}

func TestWindYakuman(t *testing.T) {
	tests := []struct {
		name string
		hand string
		want string
	}{
		{"小四喜", "111z222z333z234m44z", "小四喜"},
		{"大四喜", "111z222z333z444z55m", "大四喜"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := judge(t, tt.hand, menzen())
			if !has(hits, tt.want) {
				t.Errorf("应成立 %s；实际命中 %v", tt.want, names(hits))
			}
			for _, h := range hits {
				if h.Yaku.Yakuman != 1 {
					t.Errorf("%s 应是 1 倍役满，实际 %d 倍", h.Yaku.Name, h.Yaku.Yakuman)
				}
			}
		})
	}
}

func TestChinroutouAndChuuren(t *testing.T) {
	t.Run("清老头", func(t *testing.T) {
		// 只有幺九数牌，没有字牌
		ctx := menzen()
		ctx.WinningTile = MustTile(SuitPin, 9)
		hits := judge(t, "111m999m111s999s99p", ctx)
		if !has(hits, "清老头") {
			t.Errorf("全是幺九数牌应成立清老头；实际命中 %v", names(hits))
		}
		if has(hits, "混老头") {
			t.Errorf("无字牌不该成立混老头；实际命中 %v", names(hits))
		}
	})

	t.Run("九莲宝灯", func(t *testing.T) {
		// 1112345678999 同花色 + 任意一张（这里多一张 5m）
		hits := judge(t, "11123455678999m", menzen())
		if !has(hits, "九莲宝灯") {
			t.Errorf("应成立九莲宝灯；实际命中 %v", names(hits))
		}
	})
}

func TestTenhouAndChiihou(t *testing.T) {
	const hand = "123m456m789m123s55p"

	t.Run("天和", func(t *testing.T) {
		ctx := menzen()
		ctx.IsTsumo = true
		ctx.IsTenhou = true
		if hits := judge(t, hand, ctx); !has(hits, "天和") {
			t.Errorf("应成立天和；实际命中 %v", names(hits))
		}
	})

	t.Run("地和", func(t *testing.T) {
		ctx := menzen()
		ctx.IsTsumo = true
		ctx.IsChiihou = true
		if hits := judge(t, hand, ctx); !has(hits, "地和") {
			t.Errorf("应成立地和；实际命中 %v", names(hits))
		}
	})
}
