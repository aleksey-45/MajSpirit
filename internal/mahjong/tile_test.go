package mahjong

import "testing"

// 各花色的边界牌，逐个写死期望值。
// 不依赖 Suit/Rank 的往返，避免实现里的对称错误互相掩盖。
func TestTileSuitRankString(t *testing.T) {
	tests := []struct {
		tile Tile
		suit Suit
		rank int
		name string
	}{
		{0, SuitMan, 1, "一万"},
		{4, SuitMan, 5, "五万"},
		{8, SuitMan, 9, "九万"},
		{9, SuitSou, 1, "一索"},
		{17, SuitSou, 9, "九索"},
		{18, SuitPin, 1, "一筒"},
		{26, SuitPin, 9, "九筒"},
		{27, SuitHonor, HonorEast, "东风"},
		{28, SuitHonor, HonorSouth, "南风"},
		{29, SuitHonor, HonorWest, "西风"},
		{30, SuitHonor, HonorNorth, "北风"},
		{31, SuitHonor, HonorWhite, "白板"},
		{32, SuitHonor, HonorGreen, "发财"},
		{33, SuitHonor, HonorRed, "红中"},
		{AkaMan5, SuitMan, 5, "赤五万"},
		{AkaSou5, SuitSou, 5, "赤五索"},
		{AkaPin5, SuitPin, 5, "赤五筒"},
	}

	for _, tt := range tests {
		if got := tt.tile.Suit(); got != tt.suit {
			t.Errorf("Tile(%d).Suit() = %d，期望 %d", tt.tile, got, tt.suit)
		}
		if got := tt.tile.Rank(); got != tt.rank {
			t.Errorf("Tile(%d).Rank() = %d，期望 %d", tt.tile, got, tt.rank)
		}
		if got := tt.tile.String(); got != tt.name {
			t.Errorf("Tile(%d).String() = %q，期望 %q", tt.tile, got, tt.name)
		}
	}
}

// 34 种牌逐个做「编码 → 花色+点数 → 重新编码」的往返。
func TestNewTileRoundTrip(t *testing.T) {
	for kind := Tile(0); kind < NumTileKinds; kind++ {
		got, err := NewTile(kind.Suit(), kind.Rank())
		if err != nil {
			t.Fatalf("Tile(%d) 往返时出错: %v", kind, err)
		}
		if got != kind {
			t.Errorf("Tile(%d) 往返得到 Tile(%d)", kind, got)
		}
	}
}

// 非法入参必须报错。重点覆盖 rank 为 0 与超大值时的 uint8 回绕：
// 这类输入在旧实现下会静默返回一张完全不同的合法牌。
func TestNewTileRejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		suit Suit
		rank int
	}{
		{"数牌点数 10", SuitMan, 10},
		{"条点数 -1", SuitSou, -1},
		{"字牌点数 0", SuitHonor, 0},
		{"字牌点数 8", SuitHonor, 8},
		{"字牌点数 9", SuitHonor, 9},
		{"数牌点数回绕 34", SuitMan, 34},
		{"数牌点数回绕 257", SuitMan, 257},
		{"条点数回绕 25", SuitSou, 25},
		{"非法花色 4", Suit(4), 1},
		{"非法花色 255", Suit(255), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tile, err := NewTile(tt.suit, tt.rank)
			if err == nil {
				t.Fatalf("NewTile(%d, %d) 应报错，却返回了 Tile(%d) = %s",
					tt.suit, tt.rank, tile, tile)
			}
		})
	}
}

// 合法写码是 0..36（34 种 + 3 种赤宝牌）。
func TestValid(t *testing.T) {
	for code := Tile(0); code < NumTileCodes; code++ {
		if !code.Valid() {
			t.Errorf("Tile(%d).Valid() 应为 true", code)
		}
	}
	for _, v := range []Tile{NumTileCodes, 40, 100, 255} {
		if v.Valid() {
			t.Errorf("Tile(%d).Valid() 应为 false", v)
		}
	}
}

// 赤宝牌必须归一到对应的普通五，而且 Kind 对非赤牌是恒等。
func TestKind(t *testing.T) {
	for code, want := range map[Tile]Tile{
		AkaMan5: 4,  // 五万
		AkaSou5: 13, // 五索
		AkaPin5: 22, // 五筒
	} {
		if got := code.Kind(); got != want {
			t.Errorf("Tile(%d).Kind() = %d，期望 %d", code, got, want)
		}
		if !code.IsAka() {
			t.Errorf("Tile(%d).IsAka() 应为 true", code)
		}
		// 归一之后必须落到合法的普通牌上，且不再是赤牌
		k := code.Kind()
		if !k.Valid() || k.IsAka() {
			t.Errorf("Tile(%d).Kind() 归一到 Tile(%d)，不是普通牌", code, k)
		}
		if k.Rank() != 5 {
			t.Errorf("Tile(%d).Kind().Rank() = %d，期望 5", code, k.Rank())
		}
	}

	for code := Tile(0); code < NumTileKinds; code++ {
		if code.Kind() != code {
			t.Errorf("Tile(%d).Kind() 对非赤牌应恒等，得到 %d", code, code.Kind())
		}
		if code.IsAka() {
			t.Errorf("Tile(%d).IsAka() 应为 false", code)
		}
	}
	for _, v := range []Tile{NumTileCodes, 200, 255} {
		if v.IsAka() {
			t.Errorf("Tile(%d) 越界，IsAka() 应为 false", v)
		}
	}
}

// rank 0 表示该花色的赤五；字牌没有赤牌。
func TestNewTileAka(t *testing.T) {
	for suit, want := range map[Suit]Tile{
		SuitMan: AkaMan5, SuitSou: AkaSou5, SuitPin: AkaPin5,
	} {
		got, err := NewTile(suit, 0)
		if err != nil {
			t.Fatalf("NewTile(%d, 0) 出错: %v", suit, err)
		}
		if got != want {
			t.Errorf("NewTile(%d, 0) = Tile(%d)，期望 Tile(%d)", suit, got, want)
		}
		if got.Suit() != suit {
			t.Errorf("NewTile(%d, 0).Suit() = %d", suit, got.Suit())
		}
	}

	if _, err := NewTile(SuitHonor, 0); err == nil {
		t.Error("NewTile(SuitHonor, 0) 应报错：字牌没有赤牌")
	}
}

func TestFullSet(t *testing.T) {
	tiles := FullSet()
	if len(tiles) != NumTiles {
		t.Fatalf("FullSet() 返回 %d 张，期望 %d 张", len(tiles), NumTiles)
	}

	var counts [NumTileKinds]int
	aka := 0
	for _, tile := range tiles {
		if !tile.Valid() {
			t.Fatalf("FullSet() 里出现非法牌 Tile(%d)", tile)
		}
		counts[tile.Kind()]++ // 赤五万与五万同一种
		if tile.IsAka() {
			aka++
		}
	}
	for kind, n := range counts {
		if n != numCopies {
			t.Errorf("Tile(%d) 有 %d 张，期望 %d 张", kind, n, numCopies)
		}
	}

	// 赤宝牌是替换而不是额外加入：总数仍是 136，每种花色的五仍是 4 张。
	if aka != 3 {
		t.Errorf("FullSet() 里有 %d 张赤宝牌，期望 3 张", aka)
	}
	for _, want := range []Tile{AkaMan5, AkaSou5, AkaPin5} {
		n := 0
		for _, tile := range tiles {
			if tile == want {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s 在 FullSet() 里有 %d 张，期望 1 张", want, n)
		}
	}
}

func TestIsNumberIsHonor(t *testing.T) {
	for kind := Tile(0); kind < NumTileKinds; kind++ {
		isNum, isHonor := kind.IsNumber(), kind.IsHonor()
		if isNum == isHonor {
			t.Errorf("Tile(%d): IsNumber=%v IsHonor=%v，应恰好一个为真",
				kind, isNum, isHonor)
		}
		if want := kind < numNumberKinds; isNum != want {
			t.Errorf("Tile(%d).IsNumber() = %v，期望 %v", kind, isNum, want)
		}
	}

	// 赤宝牌是数牌——漏掉 Kind 归一的话它会掉进字牌分支
	for _, aka := range []Tile{AkaMan5, AkaSou5, AkaPin5} {
		if !aka.IsNumber() {
			t.Errorf("%s.IsNumber() 应为 true", aka)
		}
		if aka.IsHonor() {
			t.Errorf("%s.IsHonor() 应为 false", aka)
		}
	}

	// 非法牌不属于任何一类
	for _, v := range []Tile{NumTileCodes, 255} {
		if v.IsNumber() || v.IsHonor() {
			t.Errorf("Tile(%d) 非法，IsNumber/IsHonor 都应为 false", v)
		}
	}
}

// 全部 37 个写码的显示名必须互不相同，防止名字表里出现复制粘贴错误。
func TestTileNamesAreUnique(t *testing.T) {
	seen := make(map[string]Tile, NumTileCodes)
	for code := Tile(0); code < NumTileCodes; code++ {
		name := code.String()
		if prev, dup := seen[name]; dup {
			t.Errorf("Tile(%d) 与 Tile(%d) 的显示名都是 %q", code, prev, name)
		}
		seen[name] = code
	}
}

func TestTileStringInvalid(t *testing.T) {
	if got := Tile(99).String(); got != "Tile(99)" {
		t.Errorf("Tile(99).String() = %q，期望 %q", got, "Tile(99)")
	}
}

func TestMustTilePanicsOnInvalid(t *testing.T) {
	tests := []struct {
		name string
		suit Suit
		rank int
	}{
		{"字牌点数 0", SuitHonor, 0},
		{"数牌点数 10", SuitMan, 10},
		{"非法花色", Suit(9), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("MustTile(%d, %d) 应当 panic", tt.suit, tt.rank)
				}
			}()
			MustTile(tt.suit, tt.rank)
		})
	}
}

// 这里删掉过一条 TestMustTileMatchesNewTile：遍历 34 种牌比对 MustTile 与 NewTile。
//
// 它测的是"包装器和被包装的函数一致"，而 MustTile 就是 NewTile 包一层 panic——
// 这是自证。它不是没抓住任何东西（"忘了 return t"确实会被它抓），
// 而是那个缺陷**别的测试会先炸**：拿变异验证过，让 MustTile 返回错值会挂掉
// 24 个用例，其中 23 个跟它无关。既然失败模式已经被覆盖，这条就只剩余下的维护成本。
