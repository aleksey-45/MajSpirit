package mahjong

import (
	"errors"
	"slices"
)

type Counts [NumTileKinds]int

// CountTiles 统计每种牌的张数，并校验非法牌与「同种超过 4 张」。
//
// 这是规则层唯一的入口：Decompose / CanWin / Waits / 全部役种与符数都只读
// 返回值，所以赤宝牌在这里归一成种类之后，下游就再也见不到赤属性了。
// 反过来说，这里漏掉 `.Kind()` 的后果是「手里有赤五万就判不出和牌形」。
func CountTiles(tiles []Tile) (Counts, error) {
	if len(tiles) > WinningHandSize {
		return Counts{}, ErrHandFull
	}
	counts := Counts{}
	for _, t := range tiles {
		if !t.Valid() {
			return Counts{}, errors.New("invalid tile")
		}
		k := t.Kind() // 赤五万与五万同一种，必须落进同一个桶
		counts[k]++
		if counts[k] > 4 {
			return Counts{}, errors.New("tile count exceeds 4")
		}
	}
	return counts, nil
}

type MeldKind uint8

const (
	MeldRun     MeldKind = iota // 顺子
	MeldTriplet                 // 刻子
	MeldKan                     // 杠
	MeldPair                    // 对子——只用于七对子的表示
)

type Meld struct {
	Kind   MeldKind
	Tiles  [4]Tile // 顺子/刻子用前 3 个，杠用满 4 个，对子用前 2 个
	IsOpen bool    // 副露（含荣和组成的那一组）为 true
}

// NumTiles 返回这个面子实际占用的张数。
func (m Meld) NumTiles() int {
	switch m.Kind {
	case MeldKan:
		return 4
	case MeldPair:
		return 2
	default:
		return 3
	}
}

// TilesUsed 返回这个面子实际占用的牌，长度由 Kind 决定。
//
// Tiles 是定长 [4]Tile，非杠面子的第 4 个元素是零值 Tile(0)——也就是「一万」。
// 直接遍历整个数组会凭空多出一张一万，所以一律走这个方法或 Tile()。
func (m Meld) TilesUsed() []Tile { return m.Tiles[:m.NumTiles()] }

// Tile 返回代表这个面子的牌：顺子返回最小的一张，刻子/杠/对子返回那一张。
func (m Meld) Tile() Tile { return m.Tiles[0] }

// IsTripletLike 报告是否「三张一样」——刻子或杠。
// 对对和、三暗刻、混老头这类判定关心的是这个，不区分杠。
func (m Meld) IsTripletLike() bool { return m.Kind == MeldTriplet || m.Kind == MeldKan }

// Shape 是拆法的形态。和牌形只有三种（docs/规则集.md §3.5）。
type Shape uint8

const (
	ShapeStandard   Shape = iota // 4 面子 + 1 雀头
	ShapeChiitoitsu              // 七对子
	ShapeKokushi                 // 国士无双
)

// Decomposition 是一种拆法。三种形态的字段约定：
//
//	标准型   —— Pair 是雀头，Melds 是若干面子
//	七对子   —— Pair 是其中一对，Melds 是另外 6 个 MeldPair（合计 7 对）
//	国士无双 —— Pair 是成对的那张幺九牌，Melds 为空
//	            （国士是役满且不与普通役复合，不需要逐张表示其余 12 张）
type Decomposition struct {
	Shape Shape
	Pair  Tile
	Melds []Meld
}

// Tiles 返回这个拆法用到的全部牌（已排序），不含副露。
func (d Decomposition) Tiles() []Tile {
	tiles := []Tile{d.Pair, d.Pair}
	for _, m := range d.Melds {
		tiles = append(tiles, m.TilesUsed()...)
	}
	slices.Sort(tiles)
	return tiles
}

// IsSpecial 报告是否为七对子或国士无双。
func (d Decomposition) IsSpecial() bool { return d.Shape != ShapeStandard }

func (m Meld) IsSequence() bool {
	return m.Kind == MeldRun
}

func (m Meld) IsTriplet() bool {
	return m.Kind == MeldTriplet
}

func (m Meld) IsQuad() bool {
	return m.Kind == MeldKan
}

// dfs 是一个递归函数，尝试把 counts 里剩余的牌拆成面子。
// melds 是当前已经拆出的面子，pair 是雀头。

// dfs 把 counts 里剩余的牌全部拆成面子，每凑出一种完整拆法就追加到 decomps。
//
// counts 是就地修改的，调用方负责回溯恢复；melds 是当前已经拆出的面子。
//
// 关键性质：取编号最小的那张剩余的牌，在任何一个合法拆法里它必然属于
// ① 以它开头的顺子，或 ② 它自己的刻子，没有第三种可能。所以每层只试这两种归属。
func dfs(decomps *[]Decomposition, counts *Counts, pair Tile, melds []Meld) {
	i := 0
	for i < NumTileKinds && counts[i] == 0 {
		i++
	}
	if i == NumTileKinds {
		// 全部用完即是一种拆法。这里不需要再记一个面子计数：
		// 进来是 3k+2 张，拿走雀头后剩 3k，之后每步恰好减 3，减到 0 时必然正好 k 个面子。
		// 所以同一套逻辑覆盖 14 / 11 / 8 / 5 / 2 张（对应 0 到 4 个副露）的全部情况。
		*decomps = append(*decomps, Decomposition{ // 必须拷贝
			Pair:  pair,
			Melds: append([]Meld(nil), melds...),
		})
		return
	}

	t := Tile(i)

	// 分支一：刻子
	if counts[i] >= 3 {
		counts[i] -= 3
		dfs(decomps, counts, pair, append(melds, Meld{
			Kind:  MeldTriplet,
			Tiles: [4]Tile{t, t, t},
		}))
		counts[i] += 3
	}

	// 分支二：顺子
	if i < numNumberKinds && i%9 <= 6 && counts[i+1] > 0 && counts[i+2] > 0 {
		counts[i]--
		counts[i+1]--
		counts[i+2]--
		dfs(decomps, counts, pair, append(melds, Meld{
			Kind:  MeldRun,
			Tiles: [4]Tile{t, t + 1, t + 2},
		}))
		counts[i]++
		counts[i+1]++
		counts[i+2]++
	}
}

// Decompose 返回全部普通和牌型的拆法，而不是任意一种。
//
// tiles 只需包含暗牌：有 M 个副露时它是 14-3M 张，张数本身就决定了要拆出几个面子，
// 所以本函数不需要知道副露有几个。杠不参与拆解——杠是"摆到边上 + 补一张岭上牌"，
// 手上少 3 张，与碰完全等价，MeldKan 只应出现在副露列表里。
//
// 七对子与国士无双也在返回值中（见 Shape）——DetectYaku 只拿到 Decomposition，
// 不产出它们的话，七对子这个 2 翻役永远判不出来。
func Decompose(tiles []Tile) ([]Decomposition, error) {
	n := len(tiles)
	if n < 2 || n%3 != 2 { // 形状不是 3k+2，不可能是普通和牌型
		return nil, nil
	}
	counts, err := CountTiles(tiles)
	if err != nil {
		return nil, err
	}

	var decomps []Decomposition

	// 七对子与国士无双都要求门清，即 14 张暗牌。
	if n == WinningHandSize {
		if d, ok := chiitoitsuDecomposition(&counts); ok {
			decomps = append(decomps, d)
		}
		if isKokushi(&counts) {
			decomps = append(decomps, Decomposition{
				Shape: ShapeKokushi,
				Pair:  kokushiPair(&counts),
			})
		}
	}

	for i := range counts { // 枚举雀头
		if counts[i] >= 2 {
			counts[i] -= 2 // 取出雀头，剩余的牌尝试拆成顺子/刻子
			dfs(&decomps, &counts, Tile(i), nil)
			counts[i] += 2 // 回溯恢复
		}
	}
	return decomps, nil
}

// chiitoitsuDecomposition 构造七对子的拆法。
//
// 表示方式与标准型保持一致：Pair 是其中一对，Melds 是另外 6 对（合计 7 对）。
// 这样 Decomposition.Tiles() 这类"全部牌"的操作对三种形态一视同仁。
func chiitoitsuDecomposition(counts *Counts) (Decomposition, bool) {
	if !isChiitoitsu(counts) {
		return Decomposition{}, false
	}
	d := Decomposition{Shape: ShapeChiitoitsu}
	first := true
	for i, c := range counts {
		if c != 2 {
			continue
		}
		t := Tile(i)
		if first {
			d.Pair = t
			first = false
			continue
		}
		d.Melds = append(d.Melds, Meld{Kind: MeldPair, Tiles: [4]Tile{t, t}})
	}
	return d, true
}

// kokushiPair 返回国士无双里成对的那张幺九牌。
func kokushiPair(counts *Counts) Tile {
	for i, c := range counts {
		if c == 2 {
			return Tile(i)
		}
	}
	return 0
}

// isKokushiKind 报告是否为幺九牌（老头牌或字牌），即国士无双需要的 13 种之一。
func isKokushiKind(t Tile) bool {
	return t == 0 || t == 8 || t == 9 || t == 17 || t == 18 || t == 26 || t >= 27
}

// isKokushi 判定国士无双：13 种幺九牌各一张，其中恰好一种成对。
func isKokushi(counts *Counts) bool {
	pairs := 0
	for i, c := range counts {
		if !isKokushiKind(Tile(i)) {
			if c != 0 {
				return false
			}
			continue
		}
		switch c {
		case 1:
		case 2:
			pairs++
		default:
			return false
		}
	}
	return pairs == 1
}

// isChiitoitsu 判定七对子：七个不同种类的对子。
// 同种四张不算两个对子，所以这里不能用"对子数 = 总数/2"来判。
func isChiitoitsu(counts *Counts) bool {
	pairs := 0
	for _, c := range counts {
		switch c {
		case 0:
		case 2:
			pairs++
		default:
			return false
		}
	}
	return pairs == 7
}

// CanWin 报告牌型是否是合法和牌型（普通型 / 七对子 / 国士无双）。
//
// 只回答"形状"。它不回答"有没有役"，也不回答"是不是振听"——那两个条件由房间层
// 另行判定，见 docs/规则集.md §2。
func CanWin(tiles []Tile) bool {
	n := len(tiles)
	if n < 2 || n%3 != 2 {
		return false
	}
	counts, err := CountTiles(tiles)
	if err != nil {
		return false
	}
	// 七对子与国士无双都要求门清，即 14 张暗牌、没有任何副露。
	if n == WinningHandSize && (isKokushi(&counts) || isChiitoitsu(&counts)) {
		return true
	}
	// 普通型走 Decompose，保证 CanWin 与 Decompose 永远一致（同一份判定逻辑）。
	decomps, err := Decompose(tiles)
	if err != nil {
		return false
	}
	return len(decomps) > 0
}

// CanWinWith 判定「手牌 + 一张新牌」是否构成和牌型，是荣和与自摸判定的入口。
//
// 不修改 hand：先拷贝再追加。直接 hand = append(hand, t) 会就地改写调用者的底层数组。
func CanWinWith(hand []Tile, t Tile) bool {
	trial := make([]Tile, 0, len(hand)+1)
	trial = append(trial, hand...)
	trial = append(trial, t)
	return CanWin(trial)
}
