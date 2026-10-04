package mahjong

import (
	"slices"
	"testing"
)

// meldTiles 返回一个面子实际占用的牌。
//
// Meld.Tiles 是定长 [4]Tile：顺子/刻子只用前 3 个，杠用满 4 个。所以读取时
// **必须靠 Kind 判断长度**，不能直接遍历整个数组——第 4 个元素在非杠面子里
// 是零值 Tile(0)，也就是「一万」，直接遍历会凭空多出一张一万。
func meldTiles(m Meld) []Tile {
	if m.Kind == MeldKan {
		return m.Tiles[:4]
	}
	return m.Tiles[:3]
}

// reconstruct 把一个拆法还原成牌组（已排序），用于校验「这个拆法确实来自这手牌」。
//
// 直接用 Decomposition.Tiles()——它就是这件事的正式实现，
// 测试里再写一遍等于把同一个逻辑维护两份。
func reconstruct(d Decomposition) []Tile { return d.Tiles() }

// sorted 返回排序后的副本。
func sorted(tiles []Tile) []Tile {
	out := slices.Clone(tiles)
	slices.Sort(out)
	return out
}

// ── 正反例表 ────────────────────────────────────────────────────────────
//
// 期望值来自 docs/local/实现指南.md §1.5 的验收表，以及 docs/规则集.md §3.5（和牌形只有三种）。

func TestCanWin(t *testing.T) {
	tests := []struct {
		name string
		hand string
		want bool
	}{
		// 标准型：4 面子 + 1 雀头
		{"平和型", "123m456m789m123s11p", true},
		{"刻顺混合", "111m234m567m789s22p", true},
		{"四刻子", "111m222m333m444s55p", true},
		{"字牌刻子", "111z222z333z444z55z", true},
		{"多解", "111222333m456s77p", true},

		// 四张同种**是合法的**，不是错误：
		// 111m 刻子 + 123m 顺子，四张一万各有归属。
		{"暗牌四张同种", "111123m456m789m11p", true},

		// 顺子边界：789m 合法（i%9 == 6，还没越界）
		{"七万起顺子", "789m111p222p333p44z", true},

		// 七对子与国士无双
		{"七对子", "1133557799m1133p", true},
		{"七对子与标准型等价", "11223344556677m", true},
		{"国士无双", "19m19s19p12345677z", true},

		// 以下都是「不和」
		{"十三张（国士十三面是听牌形，不是和牌形）", "19m19s19p1234567z", false},
		{"十二张", "123m456m789m123s1p", false},
		{"十五张", "123m456m789m123s111p", false},
		{"凑不出雀头", "123m456m789m123s12p", false},
		{"有雀头但余张不成面子", "11m111s222s333s135p", false},
		{"顺子跨花色：八万九万一条不成立", "89m1s111p222p333p44z", false},
		{"顺子跨花色：九万一条二条不成立", "9m12s111p222p333p44z", false},
		{"字牌不组顺子：东南西", "123z111m222m333m44p", false},
		{"同种超过四张", "11111m234m567m899s", false},

		// 空与单张：形状检查必须挡住，不能 panic
		{"空手牌", "", false},
		{"单张", "1m", false},
		// ⚠️ 注意这一条：2 张是 3k+2，会被当成「四个副露 + 这个雀头」而判为完整。
		// CanWin 的参数里没有副露，只能按**张数**推断要凑几个面子——
		// 「四杠子只剩雀头」与「误传了两张」在这一层无法区分，是调用方的责任。
		{"两张同种（= 四个副露 + 雀头，四杠子的形状）", "11m", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanWin(MustParseTiles(tt.hand)); got != tt.want {
				t.Errorf("CanWin(%q) = %v，期望 %v", tt.hand, got, tt.want)
			}
		})
	}
}

// ── 多解：必须全部产出，且内容各不相同 ──────────────────────────────────

// 111222333m 既能拆成三个刻子，也能拆成三个顺子。
// 这条同时是一个**切片别名哨兵**：如果 dfs 在叶子处直接存 melds 而不拷贝，
// 两种拆法会共用底层数组，后者把前者覆盖掉——表现为「返回 2 个一模一样的拆法」。
func TestDecomposeReturnsAllDecompositions(t *testing.T) {
	decs, err := Decompose(MustParseTiles("111222333m456s77p"))
	if err != nil {
		t.Fatalf("Decompose 报错：%v", err)
	}
	if len(decs) != 2 {
		t.Fatalf("期望 2 种拆法（三刻子 / 三顺子），实际 %d 种", len(decs))
	}

	kinds := map[MeldKind]int{}
	for _, d := range decs {
		if len(d.Melds) != 4 {
			t.Errorf("每个拆法应有 4 个面子，实际 %d 个", len(d.Melds))
		}
		kinds[d.Melds[0].Kind]++
	}
	if kinds[MeldTriplet] != 1 || kinds[MeldRun] != 1 {
		t.Errorf("期望恰好一种「三刻子」拆法与一种「三顺子」拆法，"+
			"实际首面子类型分布为 %v——出现两个同型说明切片被覆盖了", kinds)
	}
}

// ── 不变量：每个拆法都必须恰好还原出这手牌 ──────────────────────────────
//
// 这是最强的一条性质测试——它能一次抓住「漏牌」「多牌」「面子张数错」
// 「雀头弄丢」等一整类 bug，而不需要为每种牌型写期望值。

func TestDecomposeReconstructsHand(t *testing.T) {
	hands := []string{
		"123m456m789m123s11p",
		"111m234m567m789s22p",
		"111m222m333m444s55p",
		"111z222z333z444z55z",
		"111222333m456s77p",
		"111123m456m789m11p",
		"11223344556677m",
		"123m456m789m11p", // 11 张（1 个副露）
		"123m456m11p",     // 8 张（2 个副露）
		"123m11p",         // 5 张（3 个副露）
		"11p",             // 2 张（4 个副露，如四杠子）
	}

	for _, hand := range hands {
		t.Run(hand, func(t *testing.T) {
			tiles := MustParseTiles(hand)
			decs, err := Decompose(tiles)
			if err != nil {
				t.Fatalf("Decompose 报错：%v", err)
			}
			if len(decs) == 0 {
				t.Fatalf("没有拆出任何和牌型")
			}
			want := sorted(tiles)
			for i, d := range decs {
				if got := reconstruct(d); !slices.Equal(got, want) {
					t.Errorf("第 %d 个拆法还原出的牌组与输入不符\n  输入 %v\n  还原 %v",
						i, want, got)
				}
				// 每个面子必须是 3 张（顺子/刻子）；杠不参与拆解，这里不该出现
				for j, m := range d.Melds {
					if n := len(meldTiles(m)); n != 3 {
						t.Errorf("第 %d 个拆法的第 %d 个面子有 %d 张牌，期望 3 张", i, j, n)
					}
					if m.Kind == MeldKan {
						t.Errorf("第 %d 个拆法里出现了杠——杠是宣言行为，不在暗牌里", i)
					}
				}
			}
		})
	}
}

// Decompose 必须是确定性的：同样的输入多次调用结果一致。
func TestDecomposeIsDeterministic(t *testing.T) {
	tiles := MustParseTiles("111222333m456s77p")
	first, err := Decompose(tiles)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		again, err := Decompose(tiles)
		if err != nil {
			t.Fatal(err)
		}
		if len(again) != len(first) {
			t.Fatalf("第 %d 次调用返回 %d 种拆法，首次是 %d 种", i, len(again), len(first))
		}
		for j := range first {
			if !slices.Equal(reconstruct(first[j]), reconstruct(again[j])) {
				t.Errorf("第 %d 次调用的第 %d 个拆法与首次不一致", i, j)
			}
		}
	}
}

// ── 七对子与国士：**也在 Decompose 的返回值里** ──────────────────────────
//
// 这条是后来改的设计：DetectYaku 只拿到 Decomposition，不产出这两种形态的话，
// 七对子这个 2 翻役永远判不出来。所以 Decompose 现在返回三种 Shape。
func TestSpecialShapes(t *testing.T) {
	tests := []struct {
		name      string
		hand      string
		wantShape []Shape
	}{
		{"纯七对子", "1133557799m1133p", []Shape{ShapeChiitoitsu}},
		{"纯国士", "19m19s19p12345677z", []Shape{ShapeKokushi}},
		// 11223344556677m 既是七对子，也能拆成三种标准型。
		{"七对子同时是标准型", "11223344556677m",
			[]Shape{ShapeChiitoitsu, ShapeStandard, ShapeStandard, ShapeStandard}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decs, err := Decompose(MustParseTiles(tt.hand))
			if err != nil {
				t.Fatalf("Decompose 报错：%v", err)
			}
			if len(decs) != len(tt.wantShape) {
				t.Fatalf("返回 %d 种拆法，期望 %d 种", len(decs), len(tt.wantShape))
			}
			// 特殊形态排在前面（Decompose 先判它们）
			for i, want := range tt.wantShape {
				if decs[i].Shape != want {
					t.Errorf("第 %d 个拆法的 Shape = %v，期望 %v", i, decs[i].Shape, want)
				}
			}
			if !CanWin(MustParseTiles(tt.hand)) {
				t.Errorf("CanWin 应认出 %q", tt.hand)
			}
		})
	}
}

// 同种四张不算两个对子——七对子要求七个**不同种类**的对子。
func TestChiitoitsuRejectsFourOfAKind(t *testing.T) {
	counts := Counts{}
	counts[0] = 4
	for i := 1; i <= 6; i++ {
		counts[i] = 2
	}
	if isChiitoitsu(&counts) {
		t.Error("四张相同被算成了两个对子")
	}
}

// 雀头枚举必须**穷尽所有可能**，不能只试第一种就返回。
//
// 11223344556677m 是最好的例子：它是七对子，同时有三种标准拆法，
// 雀头分别取 1m / 4m / 7m：
//
//	雀头 1m → 234m 234m 567m 567m
//	雀头 4m → 123m 123m 567m 567m
//	雀头 7m → 123m 123m 456m 456m
//
// 只返回一种的实现会让**役种判定算出错误的结果**——例如只有第一种拆法时
// 看不到「一杯口」（123m 123m），只有第三种时看不到「一气通贯」的可能。
func TestMultiplePairChoices(t *testing.T) {
	decs, err := Decompose(MustParseTiles("11223344556677m"))
	if err != nil {
		t.Fatalf("Decompose 报错：%v", err)
	}
	// 1 种七对子 + 3 种标准型（雀头分别取 1m / 4m / 7m）
	if len(decs) != 4 {
		t.Fatalf("期望 4 种拆法（1 七对子 + 3 标准型），实际 %d 种", len(decs))
	}

	got := map[Tile]bool{}
	for _, d := range decs {
		if d.Shape != ShapeStandard {
			continue
		}
		got[d.Pair] = true
	}
	want := []Tile{MustTile(SuitMan, 1), MustTile(SuitMan, 4), MustTile(SuitMan, 7)}
	for _, pair := range want {
		if !got[pair] {
			t.Errorf("缺少以 %v 作雀头的拆法；实际雀头集合 %v", pair, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("雀头种类应为 %d 种，实际 %d 种：%v", len(want), len(got), got)
	}
}

// 一盃口型（123m 123m）与一气通贯型（123m 456m 789m）都必须能拆出来，
// 否则役种判定会漏掉它们。
func TestDecomposeProducesYakuRelevantShapes(t *testing.T) {
	t.Run("一盃口型", func(t *testing.T) {
		decs, err := Decompose(MustParseTiles("123m123m456s789s11p"))
		if err != nil {
			t.Fatal(err)
		}
		if !hasTwoIdenticalRuns(decs) {
			t.Error("应能拆出两组完全相同的顺子（123m 123m），否则判不出一盃口")
		}
	})
	t.Run("一气通贯型", func(t *testing.T) {
		decs, err := Decompose(MustParseTiles("123m456m789m123s11p"))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, d := range decs {
			if hasRuns(d, 0, 3, 6) { // 一万起、四万起、七万起
				found = true
			}
		}
		if !found {
			t.Error("应能拆出 123m + 456m + 789m，否则判不出一气通贯")
		}
	})
}

// hasTwoIdenticalRuns 报告某个拆法里是否存在两个完全相同的顺子。
func hasTwoIdenticalRuns(decs []Decomposition) bool {
	for _, d := range decs {
		seen := map[Tile]int{}
		for _, m := range d.Melds {
			if m.Kind == MeldRun {
				seen[m.Tiles[0]]++
			}
		}
		for _, n := range seen {
			if n >= 2 {
				return true
			}
		}
	}
	return false
}

// hasRuns 报告某个拆法是否同时含有以 starts 里每个编号开头的顺子。
func hasRuns(d Decomposition, starts ...Tile) bool {
	seen := map[Tile]bool{}
	for _, m := range d.Melds {
		if m.Kind == MeldRun {
			seen[m.Tiles[0]] = true
		}
	}
	for _, s := range starts {
		if !seen[s] {
			return false
		}
	}
	return true
}

// ── 有副露时的暗牌张数：14-3M，张数本身决定要拆几个面子 ─────────────────

func TestDecomposePartialHands(t *testing.T) {
	tests := []struct {
		hand     string
		wantMeld int
	}{
		{"123m456m789m11p", 3}, // 1 个副露 → 11 张
		{"123m456m11p", 2},     // 2 个副露 → 8 张
		{"123m11p", 1},         // 3 个副露 → 5 张
		{"11p", 0},             // 4 个副露 → 2 张，只剩雀头
	}

	for _, tt := range tests {
		t.Run(tt.hand, func(t *testing.T) {
			decs, err := Decompose(MustParseTiles(tt.hand))
			if err != nil {
				t.Fatalf("Decompose 报错：%v", err)
			}
			if len(decs) != 1 {
				t.Fatalf("期望 1 种拆法，实际 %d 种", len(decs))
			}
			if got := len(decs[0].Melds); got != tt.wantMeld {
				t.Errorf("期望 %d 个面子，实际 %d 个", tt.wantMeld, got)
			}
		})
	}
}

// ── CanWinWith ──────────────────────────────────────────────────────────

func TestCanWinWith(t *testing.T) {
	tests := []struct {
		name string
		hand string
		tile string
		want bool
	}{
		{"两面听成和", "123m456m789m123s1p", "1p", true},
		{"补上的牌凑不出雀头", "123m456m789m123s1p", "9p", false},
		{"国士十三面听牌成和", "19m19s19p1234567z", "1m", true},
		{"国士听牌补错张", "19m19s19p234567z", "1z", false},
		{"搭子补一张成顺子", "123m456m789m23s11p", "1s", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hand := MustParseTiles(tt.hand)
			tile := MustParseTiles(tt.tile)[0]
			if got := CanWinWith(hand, tile); got != tt.want {
				t.Errorf("CanWinWith(%q, %q) = %v，期望 %v", tt.hand, tt.tile, got, tt.want)
			}
		})
	}
}

// CanWinWith 不得改写调用者的切片。最自然的错误实现是 hand = append(hand, t)，
// 那会就地写进 hand 的底层数组——手牌会越打越多，而且症状是静默的。
func TestCanWinWithDoesNotMutateHand(t *testing.T) {
	hand := MustParseTiles("123m456m789m123s1p")
	before := slices.Clone(hand)

	for _, s := range []string{"1p", "2p", "9s", "1z"} {
		if CanWinWith(hand, MustParseTiles(s)[0]) {
			// 结果本身不重要，这里只关心副作用
		}
	}

	if !slices.Equal(hand, before) {
		t.Errorf("hand 被改写了\n  之前 %v\n  之后 %v", before, hand)
	}
	if len(hand) != 13 {
		t.Errorf("hand 长度应为 13，实际 %d", len(hand))
	}
}

// CanWinWith(hand, t) 必须与 CanWin(hand + [t]) 完全一致——
// 两者是同一个判定的两个入口，不允许出现分歧。
func TestCanWinWithAgreesWithCanWin(t *testing.T) {
	hands := []string{
		"123m456m789m123s1p",
		"19m19s19p1234567z",
		"1133557799m113p",
		"111m222m333m44s5p",
	}
	for _, h := range hands {
		t.Run(h, func(t *testing.T) {
			hand := MustParseTiles(h)
			for kind := Tile(0); kind < NumTileKinds; kind++ {
				with := CanWinWith(hand, kind)

				trial := append(slices.Clone(hand), kind)
				direct := CanWin(trial)

				if with != direct {
					t.Errorf("牌 %v：CanWinWith = %v，CanWin(hand+该牌) = %v——两个入口不一致",
						kind, with, direct)
				}
			}
		})
	}
}

// ── CountTiles 的校验 ───────────────────────────────────────────────────

func TestCountTilesValidation(t *testing.T) {
	t.Run("同种超过四张", func(t *testing.T) {
		if _, err := CountTiles(MustParseTiles("11111m")); err == nil {
			t.Error("同种 5 张应报错")
		}
	})
	t.Run("非法牌编号", func(t *testing.T) {
		if _, err := CountTiles([]Tile{200}); err == nil {
			t.Error("Tile(200) 应报错")
		}
	})
	t.Run("超过十四张", func(t *testing.T) {
		tooMany := make([]Tile, WinningHandSize+1)
		if _, err := CountTiles(tooMany); err == nil {
			t.Error("超过 14 张应报错")
		}
	})
	t.Run("合法输入", func(t *testing.T) {
		// 3 + 3 + 3 + 2 + 2 = 13 张
		counts, err := CountTiles(MustParseTiles("123m456s789p1122z"))
		if err != nil {
			t.Fatalf("不该报错：%v", err)
		}
		if got := counts[MustTile(SuitMan, 1)]; got != 1 {
			t.Errorf("一万应计 1 张，实际 %d", got)
		}
		if got := counts[MustTile(SuitHonor, HonorEast)]; got != 2 {
			t.Errorf("东应计 2 张，实际 %d", got)
		}
		if got := counts[MustTile(SuitHonor, HonorSouth)]; got != 2 {
			t.Errorf("南应计 2 张，实际 %d", got)
		}
		total := 0
		for _, c := range counts {
			total += c
		}
		if total != 13 {
			t.Errorf("计数总和应为 13，实际 %d", total)
		}
	})
}

// ── Decompose 的错误与边界 ──────────────────────────────────────────────

func TestDecomposeRejectsBadInput(t *testing.T) {
	t.Run("形状不是 3k+2 时返回空而不是报错", func(t *testing.T) {
		for _, hand := range []string{"", "1m", "123m", "1234m", "123m456m789m123s1p"} {
			decs, err := Decompose(MustParseTiles(hand))
			if err != nil {
				t.Errorf("%q：形状不对时应返回空切片，不该报错，实际 %v", hand, err)
			}
			if len(decs) != 0 {
				t.Errorf("%q：应返回空切片，实际 %d 种拆法", hand, len(decs))
			}
		}
	})
	t.Run("张数合法但牌本身非法时透出错误", func(t *testing.T) {
		// 5 张正好是 3k+2，所以会走到 CountTiles，被「同种超过 4 张」挡住
		if _, err := Decompose(MustParseTiles("11111m")); err == nil {
			t.Error("11111m 应报错（同种超过 4 张）")
		}
		// 14 张里混一个非法编号：形状检查会放行，由 CountTiles 挡住
		bad := MustParseTiles("123m456m789m123s11p")
		bad[len(bad)-1] = Tile(200)
		if _, err := Decompose(bad); err == nil {
			t.Error("含非法牌编号的手牌应报错")
		}
	})
	t.Run("牌数不足时不 panic", func(t *testing.T) {
		// 边界：空切片、单张、两张。shape 检查必须在索引 counts 之前生效。
		for _, tiles := range [][]Tile{nil, {}, {0}, {0, 0}} {
			if _, err := Decompose(tiles); err != nil {
				t.Errorf("%v：不该报错，实际 %v", tiles, err)
			}
		}
	})
}

// ── 顺子边界：i%9 <= 6 与「字牌不能组顺子」这两条必须同时成立 ────────────

func TestRunBoundaries(t *testing.T) {
	tests := []struct {
		name string
		hand string
		want bool
	}{
		// 每个花色的合法起点：1 到 7
		{"一万起顺子", "123m111p222p333p44z", true},
		{"七万起顺子（i%9==6，最后合法起点）", "789m111p222p333p44z", true},
		{"一索起顺子", "123s111p222p333p44z", true},
		{"七筒起顺子（i%9==6 的另一个花色）", "789p111m222m333m44z", true},

		// 非法起点：八万起会越到索去
		{"八万起顺子不成立", "89m1s111p222p333p44z", false},
		{"九万起顺子不成立", "9m12s111p222p333p44z", false},
		{"八筒起顺子不成立", "89p1s111m222m333m44z", false},

		// 字牌段内部编号连续，但绝不允许组顺子
		{"东南西不成立", "123z111m222m333m44p", false},
		{"北白发不成立", "456z111m222m333m44p", false},
		{"东南北不成立（编号不连续）", "124z111m222m333m44p", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanWin(MustParseTiles(tt.hand)); got != tt.want {
				t.Errorf("CanWin(%q) = %v，期望 %v", tt.hand, got, tt.want)
			}
		})
	}
}

// Meld 的四个谓词。**IsTriplet 与 IsTripletLike 的区别是有意义的**：
// 対々和 / 三暗刻看 IsTripletLike（杠也算一组「三张一样」），
// 而符数计算必须分清刻子和杠（2/4 与 8/16）。
func TestMeldPredicates(t *testing.T) {
	tests := []struct {
		kind          MeldKind
		name          string
		seq, tri      bool
		quad, triLike bool
	}{
		{MeldRun, "顺子", true, false, false, false},
		{MeldTriplet, "刻子", false, true, false, true},
		{MeldKan, "杠", false, false, true, true},
		{MeldPair, "对子（只在七对子里出现）", false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Meld{Kind: tt.kind}
			if got := m.IsSequence(); got != tt.seq {
				t.Errorf("IsSequence() = %v，期望 %v", got, tt.seq)
			}
			if got := m.IsTriplet(); got != tt.tri {
				t.Errorf("IsTriplet() = %v，期望 %v", got, tt.tri)
			}
			if got := m.IsQuad(); got != tt.quad {
				t.Errorf("IsQuad() = %v，期望 %v", got, tt.quad)
			}
			if got := m.IsTripletLike(); got != tt.triLike {
				t.Errorf("IsTripletLike() = %v，期望 %v", got, tt.triLike)
			}
		})
	}
}

// IsSpecial 报告「不是标准型」。三种形态各取一手真牌验证，不只是比 Shape 常量。
func TestDecompositionIsSpecial(t *testing.T) {
	tests := []struct {
		name string
		hand string
		want bool
	}{
		{"标准型", "123m456m789m123s11z", false},
		{"七对子", "11223344556677m", true},
		{"国士无双", "19m19s19p12345677z", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decs, err := Decompose(MustParseTiles(tt.hand))
			if err != nil {
				t.Fatalf("Decompose(%q): %v", tt.hand, err)
			}
			if len(decs) == 0 {
				t.Fatalf("Decompose(%q) 没有返回任何拆法", tt.hand)
			}
			// 同一手牌可能拆出多种形态，只要有一种符合期望即可
			got := false
			for _, d := range decs {
				if d.IsSpecial() {
					got = true
				}
			}
			if got != tt.want {
				t.Errorf("%q 存在特殊形态拆法 = %v，期望 %v（共 %d 种拆法）",
					tt.hand, got, tt.want, len(decs))
			}
		})
	}
}
