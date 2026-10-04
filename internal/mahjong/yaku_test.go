package mahjong

import (
	"testing"
)

// ── 测试辅助 ────────────────────────────────────────────────────────────
//
// 役种测试必须用**拆法唯一**的手牌。多解的手牌会让断言变成"运气"——
// 同一个牌型的不同拆法可能命中不同的役，这正是 Decompose 要返回全部拆法的原因。

func judge(t *testing.T, hand string, ctx YakuContext) []YakuHit {
	t.Helper()
	tiles := MustParseTiles(hand)
	decs, err := Decompose(tiles)
	if err != nil {
		t.Fatalf("Decompose(%q) 报错：%v", hand, err)
	}
	if len(decs) != 1 {
		t.Fatalf("%q 有 %d 种拆法，役种测试需要唯一拆法的手牌", hand, len(decs))
	}
	if ctx.WinningTile == 0 {
		ctx.WinningTile = tiles[len(tiles)-1]
	}
	return DetectYaku(decs[0], ctx)
}

// menzen 构造一个默认的「门清、无立直、东场东家」上下文。
func menzen() YakuContext {
	return YakuContext{
		IsMenzen:  true,
		RoundWind: MustTile(SuitHonor, HonorEast),
		SeatWind:  MustTile(SuitHonor, HonorEast),
	}
}

func has(hits []YakuHit, name string) bool {
	for _, h := range hits {
		if h.Yaku.Name == name {
			return true
		}
	}
	return false
}

func hanOf(hits []YakuHit, name string) int {
	for _, h := range hits {
		if h.Yaku.Name == name {
			return h.Han
		}
	}
	return -1 // 没命中
}

func totalHan(hits []YakuHit) int {
	n := 0
	for _, h := range hits {
		n += h.Han
	}
	return n
}

func names(hits []YakuHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Yaku.Name)
	}
	return out
}

// openRun 构造一个副露的顺子。
func openRun(start Tile) Meld {
	return Meld{Kind: MeldRun, Tiles: [4]Tile{start, start + 1, start + 2}, IsOpen: true}
}

// ── 回归测试：先前修掉的 5 个 bug ────────────────────────────────────────

// bug ①：役牌看的是**刻子**，不是雀头。
//
// 雀头是场风/自风只给 2 符（连风牌雀头），不构成役。
// 旧实现比较的是 d.Pair，会把下面这手牌判成有役。
func TestYakuhaiIsTripletNotPair(t *testing.T) {
	// 东雀头 + 三组万顺（**注意不能凑齐 1-9，否则是一气通贯**）+ 一筒顺。
	// 东是场风也是自风，但只作雀头。
	hits := judge(t, "123m456m789s234p11z", menzen())
	if len(hits) != 0 {
		t.Errorf("雀头是役牌不构成役，应返回空；实际命中 %v", names(hits))
	}
}

// bug ③：岭上开花 / 抢杠 / 海底摸月 / 河底捞鱼 **副露也成立**，不是门清限定。
// 旧实现给了 HanOpen = -1。下面用有副露的手牌逐一验证。
func TestIncidentalYakuSurviveMelds(t *testing.T) {
	const hand = "234m567m789s11p" // 11 张 = 3 面子 + 雀头，另有 1 个副露
	base := YakuContext{
		Melds:     []Meld{openRun(MustTile(SuitMan, 2))}, // 与手牌无关，只要有副露即可
		IsMenzen:  false,
		RoundWind: MustTile(SuitHonor, HonorEast),
		SeatWind:  MustTile(SuitHonor, HonorEast),
	}

	tests := []struct {
		name string
		set  func(*YakuContext)
		yaku string
	}{
		{"岭上开花", func(c *YakuContext) { c.IsRinshan = true }, "岭上开花"},
		{"抢杠", func(c *YakuContext) { c.IsChankan = true }, "抢杠"},
		{"海底摸月", func(c *YakuContext) { c.IsHaitei = true }, "海底摸月"},
		{"河底捞鱼", func(c *YakuContext) { c.IsHoutei = true }, "河底捞鱼"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := base
			tt.set(&ctx)
			hits := judge(t, hand, ctx)
			if got := hanOf(hits, tt.yaku); got != 1 {
				t.Errorf("%s 副露后应为 1 翻，实际 %d（命中 %v）", tt.yaku, got, names(hits))
			}
		})
	}
}

// bug ④：YakuHit.Han 必须真的填上。旧实现只填了 Yaku，Han 恒为 0，
// 上层累加会得到「0 翻却有役」。
func TestYakuHitHanIsFilled(t *testing.T) {
	ctx := menzen()
	ctx.IsRiichi = true
	hits := judge(t, "123m456m789m234s55p", ctx)

	if len(hits) == 0 {
		t.Fatal("应至少命中立直")
	}
	for _, h := range hits {
		if h.Yaku.Yakuman == 0 && h.Han <= 0 {
			t.Errorf("役 %q 的 Han 是 %d——必须填上实际翻数", h.Yaku.Name, h.Han)
		}
	}
	if totalHan(hits) == 0 {
		t.Error("总翻数为 0，但命中了役")
	}
}

// bug ⑤：断幺九不能漏掉字牌。字牌点数是 1-7，只判 Rank()==1||9 会让
// 白(5)/发(6)/中(7) 溜过去。
func TestTanyaoRejectsHonorTriplets(t *testing.T) {
	// 白刻子 + 中张顺子
	hits := judge(t, "234m567m234s555z88p", menzen())

	if has(hits, "断幺九") {
		t.Errorf("含白刻子不可能是断幺九；实际命中 %v", names(hits))
	}
	if !has(hits, "役牌·白") {
		t.Errorf("应命中役牌·白；实际命中 %v", names(hits))
	}

	// 对照组：纯中张
	pure := judge(t, "234m567m234s567s88p", menzen())
	if !has(pure, "断幺九") {
		t.Errorf("全中张应命中断幺九；实际命中 %v", names(pure))
	}
}

// bug 修正：平和要求**两面听**，且雀头**非役牌**——但不能简单排除所有字牌。
func TestPinfu(t *testing.T) {
	t.Run("两面听成立", func(t *testing.T) {
		// 234s 听 2s：手里是 3s4s，听 2s 或 5s → 两面
		ctx := menzen()
		ctx.WinningTile = MustTile(SuitSou, 2)
		hits := judge(t, "123m456m789m234s55p", ctx)
		if !has(hits, "平和") {
			t.Errorf("两面听应成立平和；实际命中 %v", names(hits))
		}
	})

	t.Run("嵌张不成立", func(t *testing.T) {
		// 345s 听 4s：手里是 3s5s，只等 4s → 嵌张
		ctx := menzen()
		ctx.WinningTile = MustTile(SuitSou, 4)
		hits := judge(t, "123m456m789m345s55p", ctx)
		if has(hits, "平和") {
			t.Errorf("嵌张听不成立平和；实际命中 %v", names(hits))
		}
	})

	t.Run("非役牌的字牌雀头是合法的", func(t *testing.T) {
		// 东场自风为东，北(4z)雀头既不是场风也不是自风 → 不破平和
		ctx := menzen()
		ctx.WinningTile = MustTile(SuitSou, 2)
		hits := judge(t, "123m456m789m234s44z", ctx)
		if !has(hits, "平和") {
			t.Errorf("北雀头（非役牌）应成立平和；实际命中 %v", names(hits))
		}
	})

	t.Run("役牌雀头破平和", func(t *testing.T) {
		ctx := menzen()
		ctx.WinningTile = MustTile(SuitSou, 2)
		hits := judge(t, "123m456m789m234s11z", ctx) // 东雀头
		if has(hits, "平和") {
			t.Errorf("役牌雀头不成立平和；实际命中 %v", names(hits))
		}
	})

	t.Run("有刻子就不成立", func(t *testing.T) {
		ctx := menzen()
		ctx.WinningTile = MustTile(SuitSou, 2)
		hits := judge(t, "111m456m789m234s55p", ctx)
		if has(hits, "平和") {
			t.Errorf("含刻子不成立平和；实际命中 %v", names(hits))
		}
	})
}

// ── 门清限定与降翻两列 ──────────────────────────────────────────────────

// 门清限定役在副露后**完全不成立**，不是降翻。
func TestMenzenLimitedYakuVanishWhenOpen(t *testing.T) {
	const hand = "234m567m789s11p" // 11 张暗牌
	open := YakuContext{
		Melds:     []Meld{openRun(MustTile(SuitMan, 2))},
		IsMenzen:  false,
		IsRiichi:  true, // 立直本来是门清限定
		IsTsumo:   true,
		RoundWind: MustTile(SuitHonor, HonorEast),
		SeatWind:  MustTile(SuitHonor, HonorEast),
	}
	hits := judge(t, hand, open)

	for _, name := range []string{"立直", "门前清自摸和", "平和", "一杯口"} {
		if has(hits, name) {
			t.Errorf("门清限定役 %q 在副露后不该成立；实际命中 %v", name, names(hits))
		}
	}
}

// 降翻役（食い下がり）在门清与副露时翻数不同。
func TestKuisagariHanColumns(t *testing.T) {
	// 三色同顺：123m / 123s / 123p，另加 456m，雀头 77z
	tests := []struct {
		name     string
		hand     string
		openMeld []Meld
		wantHan  int
	}{
		{"门清 2 翻", "123m123s123p456m77z", nil, 2},
		{
			"副露 1 翻",
			"123m123s456m77z", // 123p 拿出去副露了
			[]Meld{openRun(MustTile(SuitPin, 1))},
			1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := YakuContext{
				Melds:     tt.openMeld,
				IsMenzen:  len(tt.openMeld) == 0,
				RoundWind: MustTile(SuitHonor, HonorEast),
				SeatWind:  MustTile(SuitHonor, HonorEast),
			}
			hits := judge(t, tt.hand, ctx)
			if got := hanOf(hits, "三色同顺"); got != tt.wantHan {
				t.Errorf("三色同顺应为 %d 翻，实际 %d；命中 %v", tt.wantHan, got, names(hits))
			}
		})
	}
}

// 连风牌（场风 == 自风）时两个役牌役**都成立**，共 2 翻。
func TestDoubleWindYakuhai(t *testing.T) {
	hits := judge(t, "111z234m456m789s22p", menzen()) // 东刻子，东场东家

	if !has(hits, "役牌·场风") || !has(hits, "役牌·自风") {
		t.Fatalf("连风牌应同时命中场风与自风；实际命中 %v", names(hits))
	}
	if got := totalHan(hits); got != 2 {
		t.Errorf("连风牌共 2 翻，实际 %d；命中 %v", got, names(hits))
	}
}

// ── 无役 / 役满优先 / 单役种没有双倍 ────────────────────────────────────

func TestNoYakuReturnsEmpty(t *testing.T) {
	// 全顺子但**单骑听**（不是两面）→ 不成立平和；
	// 也没有断幺九（含 1m / 东），没有一气通贯（万只到 456）
	ctx := menzen()
	ctx.WinningTile = MustTile(SuitHonor, HonorNorth)
	hits := judge(t, "123m456m789s234p44z", ctx)

	if len(hits) != 0 {
		t.Errorf("无役应返回空切片；实际命中 %v", names(hits))
	}
}

// 命中役满时**不再返回普通役**——役满的翻数与符数都不参与计算。
func TestYakumanSuppressesNormalYaku(t *testing.T) {
	// 白発中三组刻子 + 234m + 东雀头
	hits := judge(t, "555z666z777z234m11z", menzen())

	if !has(hits, "大三元") {
		t.Fatalf("应命中大三元；实际命中 %v", names(hits))
	}
	if len(hits) != 1 {
		t.Errorf("役满时应只返回役满役；实际命中 %v", names(hits))
	}
	for _, name := range []string{"役牌·白", "役牌·发", "役牌·中", "役牌·场风"} {
		if has(hits, name) {
			t.Errorf("役满时不该再返回普通役 %q", name)
		}
	}
}

// M-League：**单役种没有双倍役满**。国士十三面、四暗刻单骑一律 1 倍；
// 倍数只能来自复合（见 docs/规则集.md §3.4）。
func TestSingleYakumanIsOneMultiplier(t *testing.T) {
	tests := []struct {
		name string
		hand string
		yaku string
	}{
		// 国士十三面听牌形 + 一张成对 → 仍然是普通国士，1 倍
		{"国士（十三面听牌也不加倍）", "19m19s19p12345677z", "国士无双"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := judge(t, tt.hand, menzen())
			if len(hits) != 1 {
				t.Fatalf("期望恰好 1 个役；实际 %v", names(hits))
			}
			if hits[0].Yaku.Name != tt.yaku {
				t.Fatalf("期望 %q，实际 %q", tt.yaku, hits[0].Yaku.Name)
			}
			if hits[0].Yaku.Yakuman != 1 {
				t.Errorf("%s 应为 1 倍役满，实际 %d 倍", tt.yaku, hits[0].Yaku.Yakuman)
			}
			if hits[0].Han != 0 {
				t.Errorf("役满役的 Han 应为 0（翻数不参与计算），实际 %d", hits[0].Han)
			}
		})
	}
}

// 役满之间可以复合，按个数倍算。
func TestYakumanCompounds(t *testing.T) {
	// 白発中三组刻子 + 东刻子 + 南雀头，而且四组全是暗刻、雀头单骑：
	// 大三元 + 字一色 + **四暗刻** = 3 倍役满
	hits := judge(t, "555z666z777z111z22z", menzen())

	for _, name := range []string{"大三元", "字一色", "四暗刻"} {
		if !has(hits, name) {
			t.Errorf("应命中 %q；实际命中 %v", name, names(hits))
		}
	}
	total := 0
	for _, h := range hits {
		total += h.Yaku.Yakuman
	}
	if total != 3 {
		t.Errorf("三组役满复合应为 3 倍，实际 %d 倍（命中 %v）", total, names(hits))
	}
}

// ── M-League 特有：绿一色不要求含发 ─────────────────────────────────────

func TestRyuuiisouWithoutHatsu(t *testing.T) {
	// 只由 2/3/4/6/8 索构成，**不含发**
	hits := judge(t, "234s234s666s888s22s", menzen())

	if !has(hits, "绿一色") {
		t.Errorf("M-League 的绿一色不要求含发；实际命中 %v", names(hits))
	}
}
