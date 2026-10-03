package mahjong

import "fmt"

// Suit 表示牌的花色。
type Suit uint8

const (
	SuitMan   Suit = iota // 万
	SuitSou               // 条（索）
	SuitPin               // 饼（筒）
	SuitHonor             // 字牌
)

// 字牌的点数。
const (
	HonorEast  = 1 // 东
	HonorSouth = 2 // 南
	HonorWest  = 3 // 西
	HonorNorth = 4 // 北
	HonorWhite = 5 // 白
	HonorGreen = 6 // 发
	HonorRed   = 7 // 中
)

const (
	NumTileKinds = 34
	numCopies    = 4
	NumTiles     = NumTileKinds * numCopies

	HandSize        = 13
	WinningHandSize = 14

	numNumberKinds = 27
	numHonorKinds  = NumTileKinds - numNumberKinds
)

// Tile 表示一种牌（34 种之一），不区分同种牌的四张副本。
// 编码 0..33：0-8 万、9-17 条、18-26 饼、27-33 东 南 西 北 白 发 中。
type Tile uint8

// Valid 报告种类编号是否合法。本类型最底层的判断，不调用其他方法。
func (t Tile) Valid() bool { return t < NumTileKinds }

// NewTile 按花色与点数构造牌。数牌 rank 取 1-9，字牌取 1-7，越界返回错误。
func NewTile(suit Suit, rank int) (Tile, error) {
	switch suit {
	case SuitMan, SuitSou, SuitPin:
		if rank < 1 || rank > 9 {
			return 0, fmt.Errorf("mahjong: 数牌点数 %d 越界，应为 1-9", rank)
		}
	case SuitHonor:
		if rank < 1 || rank > numHonorKinds {
			return 0, fmt.Errorf("mahjong: 字牌点数 %d 越界，应为 1-%d", rank, numHonorKinds)
		}
	default:
		return 0, fmt.Errorf("mahjong: 非法花色 %d", suit)
	}
	return Tile(int(suit)*9 + rank - 1), nil
}

// MustTile 与 NewTile 相同，但参数非法时 panic。
// 仅用于测试与写死的常量，不可用于来自网络或用户输入的数值。
func MustTile(suit Suit, rank int) Tile {
	t, err := NewTile(suit, rank)
	if err != nil {
		panic(err)
	}
	return t
}

// Suit 返回花色。仅对合法牌有意义。
func (t Tile) Suit() Suit {
	switch {
	case t < 9:
		return SuitMan
	case t < 18:
		return SuitSou
	case t < numNumberKinds:
		return SuitPin
	default:
		return SuitHonor
	}
}

// Rank 返回点数：数牌 1-9，字牌 1-7。仅对合法牌有意义。
// 数牌段与字牌段长度都是 9 的倍数，同一个取模表达式对两者都成立。
func (t Tile) Rank() int { return int(t)%9 + 1 }

// IsNumber 报告是否为数牌。只有数牌能组成顺子。
func (t Tile) IsNumber() bool { return t.Valid() && t < numNumberKinds }

// IsHonor 报告是否为字牌。字牌不能组成顺子。
func (t Tile) IsHonor() bool { return t.Valid() && t >= numNumberKinds }

// 牌面显示名。
var (
	numberNames = [3][9]string{
		{"一万", "二万", "三万", "四万", "五万", "六万", "七万", "八万", "九万"},
		{"一索", "二索", "三索", "四索", "五索", "六索", "七索", "八索", "九索"},
		{"一筒", "二筒", "三筒", "四筒", "五筒", "六筒", "七筒", "八筒", "九筒"},
	}
	honorNames = [numHonorKinds]string{"东风", "南风", "西风", "北风", "白板", "发财", "红中"}
)

// String 返回中文牌名，例如 "一万"、"东风"；非法牌返回 "Tile(n)"。
func (t Tile) String() string {
	if !t.Valid() {
		return fmt.Sprintf("Tile(%d)", uint8(t))
	}
	if t.IsHonor() {
		return honorNames[t.Rank()-1]
	}
	return numberNames[t.Suit()][t.Rank()-1]
}

// FullSet 返回一副完整的 136 张牌，按种类升序排列。
func FullSet() []Tile {
	tiles := make([]Tile, 0, NumTiles)
	for kind := Tile(0); kind < NumTileKinds; kind++ {
		for i := 0; i < numCopies; i++ {
			tiles = append(tiles, kind)
		}
	}
	return tiles
}
