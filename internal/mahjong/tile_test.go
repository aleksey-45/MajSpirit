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
		{"数牌点数 0", SuitMan, 0},
		{"数牌点数 10", SuitMan, 10},
		{"条点数 0", SuitSou, 0},
		{"条点数 -1", SuitSou, -1},
		{"饼点数 0", SuitPin, 0},
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

func TestValid(t *testing.T) {
	for kind := Tile(0); kind < NumTileKinds; kind++ {
		if !kind.Valid() {
			t.Errorf("Tile(%d).Valid() 应为 true", kind)
		}
	}
	for _, v := range []Tile{34, 35, 100, 255} {
		if v.Valid() {
			t.Errorf("Tile(%d).Valid() 应为 false", v)
		}
	}
}

func TestFullSet(t *testing.T) {
	tiles := FullSet()
	if len(tiles) != NumTiles {
		t.Fatalf("FullSet() 返回 %d 张，期望 %d 张", len(tiles), NumTiles)
	}

	var counts [NumTileKinds]int
	for _, tile := range tiles {
		if !tile.Valid() {
			t.Fatalf("FullSet() 里出现非法牌 Tile(%d)", tile)
		}
		counts[tile]++
	}
	for kind, n := range counts {
		if n != numCopies {
			t.Errorf("Tile(%d) 有 %d 张，期望 %d 张", kind, n, numCopies)
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

	// 非法牌不属于任何一类
	for _, v := range []Tile{34, 255} {
		if v.IsNumber() || v.IsHonor() {
			t.Errorf("Tile(%d) 非法，IsNumber/IsHonor 都应为 false", v)
		}
	}
}

// 34 种牌的显示名必须互不相同，防止名字表里出现复制粘贴错误。
func TestTileNamesAreUnique(t *testing.T) {
	seen := make(map[string]Tile, NumTileKinds)
	for kind := Tile(0); kind < NumTileKinds; kind++ {
		name := kind.String()
		if prev, dup := seen[name]; dup {
			t.Errorf("Tile(%d) 与 Tile(%d) 的显示名都是 %q", kind, prev, name)
		}
		seen[name] = kind
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

// MustTile 与 NewTile 必须给出相同结果，二者的换算逻辑只能有一处。
func TestMustTileMatchesNewTile(t *testing.T) {
	for kind := Tile(0); kind < NumTileKinds; kind++ {
		suit, rank := kind.Suit(), kind.Rank()

		want, err := NewTile(suit, rank)
		if err != nil {
			t.Fatalf("NewTile(%d, %d) 出错: %v", suit, rank, err)
		}
		if got := MustTile(suit, rank); got != want {
			t.Errorf("MustTile(%d, %d) = %d，NewTile 给出 %d", suit, rank, got, want)
		}
	}
}
