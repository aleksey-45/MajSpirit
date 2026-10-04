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
	// NumTileKinds 是**种类**数，也是 Counts 的数组长度。
	// 赤宝牌与普通五万同属一种，所以不计入这里。
	NumTileKinds = 34
	numCopies    = 4
	NumTiles     = NumTileKinds * numCopies

	HandSize        = 13
	WinningHandSize = 14

	numNumberKinds = 27
	numHonorKinds  = NumTileKinds - numNumberKinds

	// 赤宝牌编号接在 34 种之后。它们是**同一种类的另一种写码**，不是新种类：
	// 五万和赤五万在牌型上的作用完全一样，区别只在宝牌计数。
	AkaMan5 = 34 // 赤五万
	AkaSou5 = 35 // 赤五索
	AkaPin5 = 36 // 赤五筒

	// NumTileCodes 是合法**写码**个数（37），只有 Valid 用它。
	NumTileCodes = AkaPin5 + 1
)

// Tile 表示一种牌（34 种）或一张赤宝牌，不区分同种牌的四张副本。
// 编码 0..33：0-8 万、9-17 条、18-26 饼、27-33 东 南 西 北 白 发 中；34-36 赤五万/赤五索/赤五筒。
//
// **规则判定一律先过 Kind()**：和牌形、役种、符数只看种类，赤属性是叠加在上面的
// 独立维度。这条规则只有一个例外——宝牌计数要数赤宝牌张数（CountAka）。
type Tile uint8

// Valid 报告写码是否合法。本类型最底层的判断，不调用其他方法。
func (t Tile) Valid() bool { return t < NumTileCodes }

// IsAka 报告是否为赤宝牌。不要求 Valid，越界值一律返回 false。
func (t Tile) IsAka() bool { return t >= AkaMan5 && t <= AkaPin5 }

// Kind 把赤宝牌归一到对应的普通牌，其余原样返回。
// **除了宝牌计数，所有规则判定都必须用它，而不是直接比较 Tile。**
func (t Tile) Kind() Tile {
	if !t.IsAka() {
		return t
	}
	return Tile(int(t-AkaMan5)*9 + 5 - 1) // 0/1/2 → 五万 4 / 五索 13 / 五筒 22
}

// NewTile 按花色与点数构造牌。数牌 rank 取 1-9，字牌取 1-7，越界返回错误。
// 数牌 rank 取 0 表示该花色的赤五；字牌没有赤牌，`0z` 是错误。
func NewTile(suit Suit, rank int) (Tile, error) {
	switch suit {
	case SuitMan, SuitSou, SuitPin:
		if rank == 0 {
			return Tile(AkaMan5 + int(suit)), nil
		}
		if rank < 1 || rank > 9 {
			return 0, fmt.Errorf("mahjong: 数牌点数 %d 越界，应为 1-9（0 为赤五）", rank)
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
	switch k := t.Kind(); {
	case k < 9:
		return SuitMan
	case k < 18:
		return SuitSou
	case k < numNumberKinds:
		return SuitPin
	default:
		return SuitHonor
	}
}

// Rank 返回点数：数牌 1-9，字牌 1-7。仅对合法牌有意义。
// 赤五万返回 5，不是 0——点数与"是不是赤的"是两回事。
func (t Tile) Rank() int { return int(t.Kind())%9 + 1 }

// IsNumber 报告是否为数牌。只有数牌能组成顺子。
func (t Tile) IsNumber() bool { return t.Valid() && t.Kind() < numNumberKinds }

// IsHonor 报告是否为字牌。字牌不能组成顺子。
func (t Tile) IsHonor() bool { return t.Valid() && t.Kind() >= numNumberKinds }

// 牌面显示名。
var (
	numberNames = [3][9]string{
		{"一万", "二万", "三万", "四万", "五万", "六万", "七万", "八万", "九万"},
		{"一索", "二索", "三索", "四索", "五索", "六索", "七索", "八索", "九索"},
		{"一筒", "二筒", "三筒", "四筒", "五筒", "六筒", "七筒", "八筒", "九筒"},
	}
	honorNames = [numHonorKinds]string{"东风", "南风", "西风", "北风", "白板", "发财", "红中"}
)

// String 返回中文牌名，例如 "一万"、"东风"、"赤五万"；非法牌返回 "Tile(n)"。
func (t Tile) String() string {
	if !t.Valid() {
		return fmt.Sprintf("Tile(%d)", uint8(t))
	}
	// 两个名字表的长度不同（9 和 7），所以只能分支取，不能先取一个再覆盖：
	// 先算 honorNames 的话，九万（点数 9）会直接越界。
	var name string
	if t.IsNumber() {
		name = numberNames[t.Suit()][t.Rank()-1]
	} else {
		name = honorNames[t.Rank()-1]
	}
	if t.IsAka() {
		return "赤" + name
	}
	return name
}

// FullSet 返回一副完整的 136 张牌。
//
// 每种花色的五各有一张是赤宝牌（M-League【M】第 2 章第 2 条 2），
// 赤牌**替换**普通牌而不是额外加入，所以总数仍是 136。
//
// 赤牌张数在这里写死为 1。`Rules.AkaDora` 描述的也是这个值，它存在只是为了
// 让「我们用了这条」在代码里看得见——本项目不做「无赤 / 三赤」变体。
// 真要做变体时，这个函数要改成 `FullSetFor(rules Rules)`。
func FullSet() []Tile {
	tiles := make([]Tile, 0, NumTiles)
	for kind := Tile(0); kind < NumTileKinds; kind++ {
		for i := 0; i < numCopies; i++ {
			tiles = append(tiles, kind)
		}
	}
	// 每种花色的五的第一张副本换成赤牌。换哪一张副本无所谓——
	// 换完之后整副牌要交给 Shuffle，副本之间的顺序本来就不保留。
	for _, aka := range []Tile{AkaMan5, AkaSou5, AkaPin5} {
		tiles[int(aka.Kind())*numCopies] = aka
	}
	return tiles
}
