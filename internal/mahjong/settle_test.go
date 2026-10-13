package mahjong

import (
	"errors"
	"testing"
)

// settle_test.go —— ①–⑨ 的端到端用例。
//
// 期望值几乎全部来自 docs/规则集.md §12 的六个算例：它们是**行业标准值**，
// 不是本项目的自定约定，所以可以直接硬编码。算错分没有"看起来对"这一说。

func mustWin(t *testing.T, in WinInput) WinResult {
	t.Helper()
	res, err := Win(in, DefaultRules())
	if err != nil {
		t.Fatalf("Win 失败: %v", err)
	}
	return res
}

func hasYaku(res WinResult, name string) bool {
	for _, h := range res.Yaku {
		if h.Yaku.Name == name {
			return true
		}
	}
	return false
}

// 例 1：门清荣和·平和 —— 2 翻 30 符，子家 2000 点。
func TestWinExample1PinfuRon(t *testing.T) {
	res := mustWin(t, WinInput{
		Hand:        mustTiles(t, "234m567m234s456s88p"),
		WinningTile: MustTile(SuitSou, 4),
		WinnerSeat:  1, LoserSeat: 2, DealerSeat: 0,
	})

	if res.Han != 2 || res.Fu != 30 {
		t.Errorf("翻符 = %d 翻 %d 符, 期望 2 翻 30 符", res.Han, res.Fu)
	}
	if !hasYaku(res, "平和") || !hasYaku(res, "断幺九") {
		t.Errorf("命中的役 = %v, 期望含平和与断幺九", yakuNames(res))
	}
	if want := [4]int{0, 2000, -2000, 0}; res.Deltas != want {
		t.Errorf("收支 = %v, 期望 %v（另两家不动）", res.Deltas, want)
	}
}

// 例 2：子家自摸·平和 —— 平和自摸固定 20 符，400 / 700。
//
// 单独一条的理由：20 符是个特例。写成"副底 20 + 自摸 2"会得到 22→30 符，
// 与速查表冲突，而那个错在荣和的用例里看不出来。
//
// 手牌特意带一张幺九（1m）：例 1 那副牌同时是断幺九，拿它来测"2 翻"会多出 1 翻。
func TestWinExample2PinfuTsumo(t *testing.T) {
	res := mustWin(t, WinInput{
		Hand:        mustTiles(t, "123m456m789p55p234s"),
		WinningTile: MustTile(SuitSou, 2),
		IsTsumo:     true,
		WinnerSeat:  1, LoserSeat: NoSeat, DealerSeat: 0,
	})

	if res.Han != 2 || res.Fu != 20 {
		t.Errorf("翻符 = %d 翻 %d 符, 期望 2 翻 20 符（平和自摸不加 2 符）", res.Han, res.Fu)
	}
	if !hasYaku(res, "门前清自摸和") || !hasYaku(res, "平和") {
		t.Errorf("命中的役 = %v, 期望含门前清自摸和与平和", yakuNames(res))
	}
	// 子家各付 400、庄家付 700，和牌者净收 1500（文档里的「400 / 700」是**每家的支出**）。
	if want := [4]int{-700, 1500, -400, -400}; res.Deltas != want {
		t.Errorf("收支 = %v, 期望 %v", res.Deltas, want)
	}
}

// 例 3：七对子 —— 2 翻 25 符固定。
func TestWinExample3Chiitoitsu(t *testing.T) {
	res := mustWin(t, WinInput{
		Hand:        mustTiles(t, "1188m2299s3344p55z"),
		WinningTile: mustTiles(t, "5z")[0],
		WinnerSeat:  1, LoserSeat: 2, DealerSeat: 0,
	})

	if res.Decomposition.Shape != ShapeChiitoitsu {
		t.Errorf("形态 = %v, 期望七对子", res.Decomposition.Shape)
	}
	if res.Han != 2 || res.Fu != 25 {
		t.Errorf("翻符 = %d 翻 %d 符, 期望 2 翻 25 符", res.Han, res.Fu)
	}
	if res.Deltas[1] != 1600 {
		t.Errorf("子家七对子荣和 = %d, 期望 1600", res.Deltas[1])
	}
}

// 例 4：副露·对对和 —— 36 符取整成 40 符，子家 2600 点。
//
// 同时压三件事：副露里的明刻符（幺九 +4）、暗刻符、**荣和那张牌补成的刻子算明刻**（+2）。
func TestWinExample4OpenToitoi(t *testing.T) {
	res := mustWin(t, WinInput{
		Hand:        mustTiles(t, "222s333s55z"),
		Melds:       []Meld{testMeld(t, MeldTriplet, true, "111m"), testMeld(t, MeldTriplet, true, "999m")},
		WinningTile: MustTile(SuitSou, 3),
		WinnerSeat:  1, LoserSeat: 2, DealerSeat: 0,
	})

	if res.Han != 2 || res.Fu != 40 {
		t.Errorf("翻符 = %d 翻 %d 符, 期望 2 翻 40 符（36 取整）", res.Han, res.Fu)
	}
	if !hasYaku(res, "对对和") {
		t.Errorf("命中的役 = %v, 期望含对对和", yakuNames(res))
	}
	if res.Deltas[1] != 2600 {
		t.Errorf("子家荣和 = %d, 期望 2600", res.Deltas[1])
	}
}

// 例 5：切上满贯的唯一常见场景 —— 4 翻 30 符。
//
// 严格按公式 30×2^6 = 1920 未到 2000，是切上满贯把它抬到 8000 的。
// 不覆盖这条，切上满贯写没写都测不出来。
func TestWinExample5KiriageMangan(t *testing.T) {
	res := mustWin(t, WinInput{
		Hand:        mustTiles(t, "234m567m234s456s88p"),
		WinningTile: MustTile(SuitSou, 4),
		IsRiichi:    true,
		IsIppatsu:   true,
		WinnerSeat:  1, LoserSeat: 2, DealerSeat: 0,
	})

	if res.Han != 4 || res.Fu != 30 {
		t.Errorf("翻符 = %d 翻 %d 符, 期望 4 翻 30 符", res.Han, res.Fu)
	}
	if res.Deltas[1] != 8000 {
		t.Errorf("切上满贯后子家荣和 = %d, 期望 8000", res.Deltas[1])
	}
}

// 例 6：役满 —— 跳过符与翻数。
func TestWinExample6Yakuman(t *testing.T) {
	const kokushi = "119m19s19p1234567z"

	child := mustWin(t, WinInput{
		Hand:        mustTiles(t, kokushi),
		WinningTile: MustTile(SuitMan, 1),
		WinnerSeat:  1, LoserSeat: 2, DealerSeat: 0,
	})
	if child.Yakuman != 1 || child.Han != 0 || child.Fu != 0 {
		t.Errorf("役满 = %d 倍、%d 翻、%d 符（应为 1 倍役满且翻符均为 0）",
			child.Yakuman, child.Han, child.Fu)
	}
	if child.Deltas[1] != 32000 {
		t.Errorf("子家役满荣和 = %d, 期望 32000", child.Deltas[1])
	}

	dealer := mustWin(t, WinInput{
		Hand:        mustTiles(t, kokushi),
		WinningTile: MustTile(SuitMan, 1),
		WinnerSeat:  0, LoserSeat: 1, DealerSeat: 0,
	})
	if dealer.Deltas[0] != 48000 {
		t.Errorf("庄家役满荣和 = %d, 期望 48000", dealer.Deltas[0])
	}
}

// 役满不计宝牌。攥着一堆宝牌也不能把 32000 变成更多 ——
// 这条和「宝牌不算役」是一对，两个都错了才会被发现，所以两个都要有。
func TestWinYakumanIgnoresDora(t *testing.T) {
	res := mustWin(t, WinInput{
		Hand:           mustTiles(t, "119m19s19p1234567z"),
		WinningTile:    MustTile(SuitMan, 1),
		DoraIndicators: mustTiles(t, "9m9m"), // 指示 1m，手里两张
		WinnerSeat:     1, LoserSeat: 2, DealerSeat: 0,
	})

	if res.Dora != 0 || res.Han != 0 {
		t.Errorf("役满时宝牌/翻数 = %d/%d, 期望都是 0", res.Dora, res.Han)
	}
	if res.Deltas[1] != 32000 {
		t.Errorf("收支 = %d, 期望 32000（役满不因宝牌变大）", res.Deltas[1])
	}
}

// 立直宝牌与赤宝牌都算进翻数，而且加在役之后。
//
// 手牌是"234s 4s 赤5s 6s"：赤五索按普通五索参与牌型，同时自己多算一翻赤宝牌。
// 只按普通牌数宝牌，或者把赤牌当独立种类拆不出顺子，两种错都会在这里露出来。
func TestWinCountsDoraAndAka(t *testing.T) {
	res := mustWin(t, WinInput{
		Hand:           mustTiles(t, "234m567m234s406s88p"),
		WinningTile:    MustTile(SuitSou, 4),
		IsRiichi:       true,
		DoraIndicators: mustTiles(t, "2s"), // 宝牌是 3s，手里一张
		WinnerSeat:     1, LoserSeat: 2, DealerSeat: 0,
	})

	for _, name := range []string{"立直", "平和", "断幺九"} {
		if !hasYaku(res, name) {
			t.Fatalf("命中的役 = %v, 期望含%s", yakuNames(res), name)
		}
	}
	// 宝牌 1 张（3s）+ 赤宝牌 1 张（赤五索）。
	if res.Dora != 2 {
		t.Errorf("宝牌数 = %d, 期望 2（一张 3s + 一张赤五索）", res.Dora)
	}
	if want := 3 + res.Dora; res.Han != want {
		t.Errorf("总翻 = %d, 期望 %d（立直+平和+断幺九 加宝牌）", res.Han, want)
	}
}

// 高点法：22334455667788p 既能拆成七对子，也能拆成两套一杯口。
// 后者是清一色 6 + 二杯口 3 = 9 翻，前者只有 2 + 6 = 8 翻，必须选后者。
func TestWinPicksHighestValueDecomposition(t *testing.T) {
	res := mustWin(t, WinInput{
		Hand:        mustTiles(t, "22334455667788p"),
		WinningTile: MustTile(SuitPin, 4),
		WinnerSeat:  1, LoserSeat: 2, DealerSeat: 0,
	})

	if res.Decomposition.Shape != ShapeStandard {
		t.Errorf("形态 = %v, 期望标准型（七对子那条路翻数更低）", res.Decomposition.Shape)
	}
	if !hasYaku(res, "二杯口") || hasYaku(res, "七对子") {
		t.Errorf("命中的役 = %v, 期望二杯口而不是七对子", yakuNames(res))
	}
	if res.Han < 9 {
		t.Errorf("总翻 = %d, 期望至少 9（清一色 6 + 二杯口 3）", res.Han)
	}
}

// 本场棒与立直棒要传到 CalcScore：前者只由放铳者出，后者全归和牌者。
// 漏传的症状是"点数是对的，只是永远是 0 本场"。
func TestWinCarriesHonbaAndRiichiSticks(t *testing.T) {
	res := mustWin(t, WinInput{
		Hand:         mustTiles(t, "234m567m234s456s88p"),
		WinningTile:  MustTile(SuitSou, 4),
		Honba:        1,
		RiichiSticks: 2,
		WinnerSeat:   1, LoserSeat: 2, DealerSeat: 0,
	})

	// 2000（手牌）+ 300（一本场）+ 2000（两根立直棒）
	if want := [4]int{0, 4300, -2300, 0}; res.Deltas != want {
		t.Errorf("收支 = %v, 期望 %v", res.Deltas, want)
	}
}

// ② 没有役就不能和。宝牌不算役，所以手里有宝牌也救不了。
func TestWinRejectsNoYaku(t *testing.T) {
	_, err := Win(WinInput{
		Hand:        mustTiles(t, "456m789m234s55z"),
		Melds:       []Meld{testMeld(t, MeldRun, true, "123p")},
		WinningTile: mustTiles(t, "5z")[0],
		// 宝牌给足，看它会不会被当成役。
		DoraIndicators: mustTiles(t, "4m4m4m4m"),
		WinnerSeat:     1, LoserSeat: 2, DealerSeat: 0,
	}, DefaultRules())

	if !errors.Is(err, ErrNoYaku) {
		t.Fatalf("err = %v, 期望 ErrNoYaku", err)
	}
}

// ③ 振听只限制荣和：同一条手牌，荣和失败、自摸成立。
func TestWinFuritenBlocksRonOnly(t *testing.T) {
	in := WinInput{
		Hand:        mustTiles(t, "234m567m234s456s88p"),
		WinningTile: MustTile(SuitSou, 4),
		Furiten:     true,
		WinnerSeat:  1, LoserSeat: 2, DealerSeat: 0,
	}

	if _, err := Win(in, DefaultRules()); !errors.Is(err, ErrFuriten) {
		t.Errorf("荣和时 err = %v, 期望 ErrFuriten", err)
	}

	in.IsTsumo = true
	in.LoserSeat = NoSeat
	if _, err := Win(in, DefaultRules()); err != nil {
		t.Errorf("自摸时不应受振听影响, err = %v", err)
	}
}

// ① 张数对不上（荣和时忘了把和牌张加进 Hand）必须报成"牌型不对"，
// 而不是在下面表现成别的错。
func TestWinRejectsBadShape(t *testing.T) {
	tests := []struct {
		name string
		in   WinInput
	}{
		{"只有 13 张", WinInput{
			Hand:        mustTiles(t, "234m567m234s456s8p"),
			WinningTile: MustTile(SuitPin, 8),
		}},
		{"14 张但不成形", WinInput{
			Hand:        mustTiles(t, "1123456m1234567z"),
			WinningTile: MustTile(SuitMan, 1),
		}},
		{"和牌张不在手里", WinInput{
			Hand:        mustTiles(t, "234m567m234s456s88p"),
			WinningTile: MustTile(SuitMan, 9),
		}},
		{"非法的和牌张", WinInput{
			Hand:        mustTiles(t, "234m567m234s456s88p"),
			WinningTile: Tile(200),
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.in.WinnerSeat, tt.in.LoserSeat, tt.in.DealerSeat = 1, 2, 0
			_, err := Win(tt.in, DefaultRules())
			if !errors.Is(err, ErrNotWinningHand) {
				t.Errorf("err = %v, 期望 ErrNotWinningHand", err)
			}
		})
	}
}

// 手牌里有非法牌时报的是"牌非法"，不能被下面那条"和牌张不在手里"抢先盖掉——
// 两者的原因完全不同，盖掉之后排查方向会整个跑偏。
func TestWinRejectsInvalidHand(t *testing.T) {
	in := WinInput{
		Hand:        append([]Tile{200}, mustTiles(t, "234m567m234s456s88p")...),
		WinningTile: MustTile(SuitSou, 4),
		WinnerSeat:  1, LoserSeat: 2, DealerSeat: 0,
	}
	_, err := Win(in, DefaultRules())
	if err == nil {
		t.Fatal("手牌里有非法牌，Win 应当报错")
	}
	if errors.Is(err, ErrNotWinningHand) {
		t.Errorf("err = %v，不该被归成「不是和牌型」", err)
	}
}

// 高点法比的是**点数**，不是"先比翻再比符"。
//
// 这里直接测这个决策函数，因为它要挡住的差异在真实牌型上很难同时出现：
// 3 翻 70 符封顶后是满贯 2000，而 4 翻 25 符只有 1600 —— 按翻数先比就会选错那个。
func TestHigherValue(t *testing.T) {
	rules := DefaultRules()
	tests := []struct {
		name string
		a, b WinResult
		want bool
	}{
		{"点数高的赢，哪怕翻数低", WinResult{Han: 3, Fu: 70}, WinResult{Han: 4, Fu: 25}, true},
		{"反过来也一样", WinResult{Han: 4, Fu: 25}, WinResult{Han: 3, Fu: 70}, false},
		{"封顶后点数相同，翻数高的赢", WinResult{Han: 5, Fu: 30}, WinResult{Han: 4, Fu: 40}, true},
		{"点数翻数都相同，符数高的赢", WinResult{Han: 2, Fu: 40}, WinResult{Han: 2, Fu: 30}, true},
		{"役满压过一切普通役", WinResult{Yakuman: 1}, WinResult{Han: 13, Fu: 120}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := higherValue(tt.a, tt.b, rules); got != tt.want {
				t.Errorf("higherValue(%+v, %+v) = %v, 期望 %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// 帕奥只对三种役满成立（§8.5）。这里把三种都摆出来，是因为只测大三元的话，
// 把判定改成「命中任何役满都算包」照样能过——而国士无双吃包是实打实的算错钱。
func TestWinPaoYakumanKinds(t *testing.T) {
	tests := []struct {
		name  string
		in    WinInput
		wantP int // 期望的 PaoYakuman
	}{
		{
			name: "大三元",
			in: WinInput{
				Hand:        mustTiles(t, "666z777z234m55m"),
				Melds:       []Meld{testMeld(t, MeldTriplet, true, "555z")},
				WinningTile: mustTiles(t, "5m")[0],
			},
			wantP: 1,
		},
		{
			// 四风各一刻子 + 白雀头：大四喜与字一色复合，包只覆盖大四喜那一个。
			name: "大四喜（与字一色复合）",
			in: WinInput{
				Hand:        mustTiles(t, "111z222z333z444z55z"),
				WinningTile: mustTiles(t, "5z")[0],
			},
			wantP: 1,
		},
		{
			name: "四槓子",
			in: WinInput{
				Hand: mustTiles(t, "55m"),
				Melds: []Meld{
					testMeld(t, MeldKan, true, "1111m"),
					testMeld(t, MeldKan, false, "2222s"),
					testMeld(t, MeldKan, true, "3333p"),
					testMeld(t, MeldKan, true, "5555z"),
				},
				WinningTile: MustTile(SuitMan, 5),
			},
			wantP: 1,
		},
		{
			// 国士不吃包。给一个责任者也必须算成 0，否则钱会从错的人身上出。
			name: "国士无双不算包",
			in: WinInput{
				Hand:        mustTiles(t, "119m19s19p1234567z"),
				WinningTile: MustTile(SuitMan, 1),
			},
			wantP: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			in.WinnerSeat, in.LoserSeat, in.DealerSeat, in.PaoSeat = 1, 2, 0, 2

			res := mustWin(t, in)
			if res.Yakuman == 0 {
				t.Fatalf("这副牌应当是役满，实际役满 %d、役 %v", res.Yakuman, yakuNames(res))
			}
			if res.PaoYakuman != tt.wantP {
				t.Errorf("PaoYakuman = %d, 期望 %d", res.PaoYakuman, tt.wantP)
			}
		})
	}
}

// 帕奥的赔付：责任者放铳时一人全付，别人放铳时与放铳者各付一半。
func TestWinPaoPayment(t *testing.T) {
	in := WinInput{
		Hand:        mustTiles(t, "666z777z234m55m"),
		Melds:       []Meld{testMeld(t, MeldTriplet, true, "555z")},
		WinningTile: mustTiles(t, "5m")[0],
		WinnerSeat:  1, DealerSeat: 0,
	}

	// 没有责任者：按普通役满结算，放铳者独付。
	in.LoserSeat, in.PaoSeat = 2, NoSeat
	if want := [4]int{0, 32000, -32000, 0}; mustWin(t, in).Deltas != want {
		t.Errorf("无帕奥时收支 = %v, 期望 %v", mustWin(t, in).Deltas, want)
	}

	// 责任者放铳：还是一个人付全额。
	in.PaoSeat = 2
	if want := [4]int{0, 32000, -32000, 0}; mustWin(t, in).Deltas != want {
		t.Errorf("帕奥者放铳时收支 = %v, 期望 %v", mustWin(t, in).Deltas, want)
	}

	// 别人放铳：责任者与放铳者各付一半。
	in.LoserSeat = 3
	if want := [4]int{0, 32000, -16000, -16000}; mustWin(t, in).Deltas != want {
		t.Errorf("别人放铳时收支 = %v, 期望 %v（各半）", mustWin(t, in).Deltas, want)
	}
}

// yakuNames 只在断言失败时用，所以格式怎么好看怎么来。
func yakuNames(res WinResult) []string {
	names := make([]string, len(res.Yaku))
	for i, h := range res.Yaku {
		names[i] = h.Yaku.Name
	}
	return names
}
