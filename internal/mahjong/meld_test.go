package mahjong

import (
	"slices"
	"testing"
)

// meld_test.go —— 吃 / 碰 / 杠的牌面判定与立直宣言的合法性。
//
// 座位、分数、牌山余量这些不在这一层，所以这里不测它们。

func testMeld(t *testing.T, kind MeldKind, open bool, s string) Meld {
	t.Helper()
	tiles := ParseTilesOrFail(t, s)
	if len(tiles) > 4 {
		t.Fatalf("一个面子最多 4 张，给了 %q", s)
	}
	m := Meld{Kind: kind, IsOpen: open}
	copy(m.Tiles[:], tiles)
	return m
}

func chiToStrings(opts [][2]Tile) []string {
	out := make([]string, len(opts))
	for i, p := range opts {
		out[i] = FormatTiles(p[:])
	}
	return out
}

func TestChiOptions(t *testing.T) {
	tests := []struct {
		name   string
		hand   string
		called string
		want   []string
	}{
		// called 可以当顺子的第 1 / 第 2 / 第 3 张，三种都要能列出来。
		{"三种形状都在", "1245s", "3s", []string{"12s", "24s", "45s"}},
		{"边张只有一种", "23s", "1s", []string{"23s"}},
		{"另一头的边张", "78s", "9s", []string{"78s"}},
		{"缺一张就吃不了", "34s", "1s", nil},
		{"手里有两张同一种也能吃", "1122s", "3s", []string{"12s"}},
		{"字牌不能吃", "1234z", "1z", nil},
		// 9m 和 1s 都在手里，也不能拿它们去和 8m 组顺子——顺子不跨花色。
		{"顺子不跨花色", "9m1s2s", "8m", nil},
		{"上家打的牌不在手里也没关系", "24m", "3m", []string{"24m"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := chiToStrings(ChiOptions(mustTiles(t, tt.hand), mustTiles(t, tt.called)[0]))
			if !slices.Equal(got, tt.want) {
				t.Errorf("ChiOptions(%s, %s) = %v, 期望 %v",
					tt.hand, tt.called, got, tt.want)
			}
		})
	}
}

// 赤五万必须表现得和五万一模一样，否则「手里有赤五万就吃不了 4s」。
// 而且返回的必须是**归一后**的牌种：房间层要拿它去填 claimWindow 的候选。
func TestChiOptionsNormalizesAka(t *testing.T) {
	called := MustTile(SuitSou, 4)
	opts := ChiOptions(mustTiles(t, "0s3s"), called)
	if len(opts) != 1 {
		t.Fatalf("期望 1 种吃法，实际 %v", chiToStrings(opts))
	}
	if got, want := FormatTiles(opts[0][:]), "35s"; got != want {
		t.Errorf("吃法 = %s, 期望 %s", got, want)
	}
	if five := MustTile(SuitSou, 5); opts[0][1] != five {
		t.Errorf("返回的是 %s，期望归一成普通五索", opts[0][1])
	}
}

func TestCanPonAndOpenKan(t *testing.T) {
	tests := []struct {
		name      string
		hand      string
		called    string
		wantPon   bool
		wantKanOp bool
	}{
		{"两张能碰", "22m", "2m", true, false},
		{"三张能碰也能大明杠", "222m", "2m", true, true},
		{"一张都不够", "2m", "2m", false, false},
		{"赤五万算五万（碰）", "05m", "5m", true, false},
		{"赤五万算五万（大明杠）", "055m", "5m", true, true},
		{"字牌同样能碰", "111z", "1z", true, true},
		{"别的牌不算数", "333m", "4m", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hand := mustTiles(t, tt.hand)
			called := mustTiles(t, tt.called)[0]
			if got := CanPon(hand, called); got != tt.wantPon {
				t.Errorf("CanPon = %v, 期望 %v", got, tt.wantPon)
			}
			if got := CanKanOpen(hand, called); got != tt.wantKanOp {
				t.Errorf("CanKanOpen = %v, 期望 %v", got, tt.wantKanOp)
			}
		})
	}
}

func TestClosedKanTiles(t *testing.T) {
	tests := []struct {
		name string
		hand string
		want string
	}{
		{"四张在手才能暗杠", "1111m", "1m"},
		{"三张不行", "111m", ""},
		{"两种牌都能暗杠", "1111m2222s", "1m2s"},
		{"赤五万凑满四张也算", "055m5m", "5m"},
		{"空手牌", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClosedKanTiles(mustTiles(t, tt.hand))
			var want []Tile
			if tt.want != "" {
				want = mustTiles(t, tt.want)
			}
			if !slices.Equal(got, want) {
				t.Errorf("ClosedKanTiles(%s) = %s, 期望 %s",
					tt.hand, FormatTiles(got), FormatTiles(want))
			}
		})
	}
}

func TestAddedKanTiles(t *testing.T) {
	pon := testMeld(t, MeldTriplet, true, "111m")
	closedTriplet := testMeld(t, MeldTriplet, false, "111m")
	openKan := testMeld(t, MeldKan, true, "1111m")

	tests := []struct {
		name  string
		hand  string
		melds []Meld
		want  string
	}{
		{"碰过的一刻子加第四张", "1m", []Meld{pon}, "1m"},
		{"碰过但手里没有第四张", "2m", []Meld{pon}, ""},
		{"没有副露", "1111m", nil, ""},
		// 手里暗着的三张不是 Meld，它走暗杠那条路（ClosedKanTiles）。
		{"手牌里的暗刻不算加杠", "1m", []Meld{closedTriplet}, ""},
		{"已经是杠了不能再加", "1m", []Meld{openKan}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AddedKanTiles(mustTiles(t, tt.hand), tt.melds)
			var want []Tile
			if tt.want != "" {
				want = mustTiles(t, tt.want)
			}
			if !slices.Equal(got, want) {
				t.Errorf("AddedKanTiles(%s) = %s, 期望 %s",
					tt.hand, FormatTiles(got), FormatTiles(want))
			}
		})
	}
}

// 暗杠不是副露。按「没有面子」写会让立直后暗杠的人当场失去门清，
// 役种表里所有门清限定（HanOpen = -1）全部失效。
func TestIsMenzenKeepsClosedKan(t *testing.T) {
	closedKan := testMeld(t, MeldKan, false, "1111m")
	openPon := testMeld(t, MeldTriplet, true, "111m")

	if !IsMenzen(nil) {
		t.Error("没有副露就是门清")
	}
	if !IsMenzen([]Meld{closedKan}) {
		t.Error("暗杠不破门清")
	}
	if IsMenzen([]Meld{closedKan, openPon}) {
		t.Error("有一组明刻就不是门清")
	}
}

func TestRiichiDiscards(t *testing.T) {
	openPon := testMeld(t, MeldTriplet, true, "111m")

	tests := []struct {
		name  string
		hand  string
		melds []Meld
		want  string
	}{
		// 只听 5s（嵌张），所以只有打掉那张不要的 1z 才还能立直 ——
		// 这条盯的是「宣言 + 打牌」必须一起判：只看"现在听不听"会放过打别的牌。
		{"只听一张时只有那一张能立直", "234m567m234s46s88p1z", nil, "1z"},
		// 国士十三面反过来：13 种幺九牌随便打哪一张，都还能听回那一张。
		{"国士十三面：十三种都能立直", "19m19s19p1234567z1z", nil, "19m19s19p1234567z"},
		// 已经成和牌形的手牌，打哪张都还听 —— 返回的是**牌种**，不是 14 个元素。
		{"成和牌形的手牌到处都能立直", "234m567m234s456s88p", nil, "234567m23456s8p"},
		{"副露不能立直", "19m19s19p1234567z1z", []Meld{openPon}, ""},
		{"张数不是 3k+2", "19m19s19p1234567z", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RiichiDiscards(mustTiles(t, tt.hand), tt.melds)
			var want []Tile
			if tt.want != "" {
				want = mustTiles(t, tt.want)
			}
			if !slices.Equal(got, want) {
				t.Errorf("RiichiDiscards(%s) = %s, 期望 %s",
					tt.hand, FormatTiles(got), FormatTiles(want))
			}
		})
	}
}

// 非法牌不能 panic：这几个函数都要用 called.Kind() 或 counts[...] 去索引数组，
// 少了守卫就是越界。牌从网络来，一行守卫换掉一次崩溃是很划算的交易。
func TestMeldHandlesInvalidTiles(t *testing.T) {
	bad := []Tile{200}
	hand := mustTiles(t, "234m567m234s456s88p")
	called := MustTile(SuitMan, 3)

	if got := ChiOptions(bad, called); got != nil {
		t.Errorf("ChiOptions(非法手牌) = %v, 期望 nil", got)
	}
	if got := ChiOptions(hand, Tile(200)); got != nil {
		t.Errorf("ChiOptions(非法被吃牌) = %v, 期望 nil", got)
	}
	if CanPon(hand, Tile(200)) || CanKanOpen(hand, Tile(200)) {
		t.Error("非法被鸣牌不应当判成能吃/能杠")
	}
	if CanPon(bad, called) || CanKanOpen(bad, called) {
		t.Error("非法手牌不应当判成能吃/能杠")
	}
	if got := ClosedKanTiles(bad); got != nil {
		t.Errorf("ClosedKanTiles(非法手牌) = %v, 期望 nil", got)
	}
	if got := AddedKanTiles(bad, []Meld{testMeld(t, MeldTriplet, true, "111m")}); got != nil {
		t.Errorf("AddedKanTiles(非法手牌) = %v, 期望 nil", got)
	}
	// 这一条要凑成 3k+2 张才会走到 CountTiles，否则被张数守卫先挡掉。
	twelve := append([]Tile{200}, mustTiles(t, "234m567m234s456s8p")...)
	if got := RiichiDiscards(twelve, nil); got != nil {
		t.Errorf("RiichiDiscards(非法手牌) = %v, 期望 nil", got)
	}
}

// MaxKans 的取值是规则里写死的一条（§11.2），改小改大都会改变牌局合法性。
func TestMaxKans(t *testing.T) {
	if MaxKans != 4 {
		t.Errorf("MaxKans = %d, 期望 4（【M】第 4 章第 5 条 4）", MaxKans)
	}
}
