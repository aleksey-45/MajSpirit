package mahjong

// dora.go —— 宝牌计数（docs/规则集.md §4）。
//
// 为什么单独一个文件：宝牌不是役，它不参与「能不能和牌」的判定，
// 只在这一步加翻数——
//
//	⑥ 汇总翻数 → 役翻数 + 宝牌 + 里宝 + 赤宝
//
// 所以它必须发生在 DetectYaku 之后、CalcScore 之前。放错位置会做出
// 「只有宝牌也能和牌」的游戏；役满时这一步整个跳过。
//
// 赤宝牌数翻数是 `CountDora(tiles, indicators) + CountAka(tiles)`：
// 一张赤五万在「五万是宝牌」时同时是宝牌和赤宝牌，两个 +1 都要给。

// DoraFromIndicator 返回一张指示牌所指示的宝牌。
//
//	风牌   东→南→西→北→东   （编号 27..30）
//	三元牌 白→发→中→白       （编号 31..33）
//
// 记法上的 4z（北）后面是 1z（东），但 30+1 会落到 31（白）——
// 这正是最容易写成 `(t+1) % NumTileKinds` 然后错掉的地方。
func DoraFromIndicator(indicator Tile) Tile {
	if !indicator.Valid() {
		return indicator
	}
	// 指示牌自己可能是赤宝牌（牌山里那三张也会被翻开），先归一到种类。
	indicator = indicator.Kind()
	if indicator.IsHonor() {
		if isWind(indicator) {
			// 东(27) 南(28) 西(29) 北(30) 循环
			return Tile(27 + (int(indicator)-27+1)%4)
		}
		// 白(31) 发(32) 中(33) 循环
		return Tile(31 + (int(indicator)-31+1)%3)
	}
	// 数牌：同花色内 1..9 循环（九万 → 一万）
	base := int(indicator) / 9 * 9
	return Tile(base + (int(indicator)-base+1)%9)
}

// CountDora 数出 tiles 里有多少张由 indicators 指示的宝牌。
//
// 指示牌列表不能去重：两张相同的指示牌意味着宝牌翻倍，所以要按重数计。
// 杠同样按实际张数计（TilesUsed 已经处理了「杠用满 4 张」）。
//
// 赤宝牌按种类参与这里：赤五万在「五万是宝牌」时照样算一张宝牌，
// 它额外多出的那一张由 CountAka 计——两者相加才是赤牌的总价值。
//
// 里宝牌用同一个函数，只是把 indicators 换成里宝牌指示牌——并且只在立直和牌时才调用
// （里宝牌不立直不翻开）。
func CountDora(tiles []Tile, indicators []Tile) int {
	if len(indicators) == 0 {
		return 0
	}

	// 每种牌被指示了多少次；不去重，所以用计数而不是布尔
	var multiplier [NumTileKinds]int
	for _, ind := range indicators {
		if d := DoraFromIndicator(ind); d.Valid() {
			multiplier[d]++
		}
	}

	n := 0
	for _, t := range tiles {
		if t.Valid() {
			n += multiplier[t.Kind()]
		}
	}
	return n
}

// CountAka 返回 tiles 里的赤宝牌张数，每张 +1 翻。
//
// 和普通宝牌分别计：赤五万既是赤宝牌、也可能是宝牌，两个 +1 都要给。
// 因此数翻数时是 `CountDora(...) + CountAka(...)`。
// 张数上限由牌山保证（每种一张，M-League【M】第 2 章第 2 条 2）。
func CountAka(tiles []Tile) int {
	n := 0
	for _, t := range tiles {
		if t.IsAka() {
			n++
		}
	}
	return n
}

// AllTiles 返回一次和牌用到的全部牌：暗牌（含和牌张）+ 副露。
//
// 数宝牌必须用全部牌——副露里的宝牌同样计数。写成「只看手牌」会漏掉副露里的，
// 而那种错误只在有人鸣牌时才出现。
//
// concealed 必须传原始牌，不能传 `Decomposition.Tiles()`。
// Decomposition 是从 Counts 建出来的（见 CountTiles），赤宝牌在那里已经被归一成
// 普通牌，所以拿它来数赤宝牌会一张都数不到。这个签名不给 Decomposition 留位置，
// 就是为了让这个错误写不出来。

func AllTiles(concealed []Tile, melds []Meld) []Tile {
	tiles := make([]Tile, 0, len(concealed)+3*len(melds))
	tiles = append(tiles, concealed...)
	for _, m := range melds {
		tiles = append(tiles, m.TilesUsed()...)
	}
	return tiles
}
