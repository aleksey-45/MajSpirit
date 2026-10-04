package mahjong

// CalcFu 计算符数，规则见 docs/规则集.md §5。
//
// 和牌张从 ctx.WinningTile 取（原来的独立参数已去掉——两个来源会不一致）。
//
// 三个必须单独处理的点：
//   - **七对子固定 25 符**，不参与取整
//   - **平和自摸固定 20 符**（不加自摸的 2 符）
//   - **取整只做一次**，在所有加符项累加完之后向上取整到 10 符
func CalcFu(d Decomposition, ctx YakuContext) int {
	switch d.Shape {
	case ShapeChiitoitsu:
		return 25 // 固定，不取整
	case ShapeKokushi:
		return 0 // 役满，符数不参与计算
	}

	// 赤宝牌在这里归一成普通牌。下面 waitFu 与 isConcealedMeld 都在拿
	// ctx.WinningTile 和面子的牌做**相等比较**，留着赤牌编号就一张都比不中：
	// 待ち形会误判成两面（该 +2 的变成 +0），荣和补成的刻子会误判成暗刻（+2 变 +4）。
	h := newHand(d, ctx.normalized())
	ctx = h.ctx // 之后的 ctx 一律指归一后的版本，避免两处来源不一致

	// 平和自摸是特例：20 符，不加自摸符
	if ctx.IsTsumo && h.isPinfu() {
		return 20
	}

	fu := 20 // 副底

	for _, m := range h.melds {
		fu += meldFu(m, h.isConcealedMeld(m))
	}

	// 雀头符：役牌 +2。
	// **连风牌（场风==自风）在 M-League 也只算 2 符**，不是 4
	// （docs/规则集.md §14.2；官方第 6 章第 3 条 2 注）。
	if h.isYakuhaiTile(d.Pair) {
		fu += 2
	}

	fu += waitFu(d, ctx.WinningTile)

	if ctx.IsTsumo {
		fu += 2
	}
	if ctx.IsMenzen && !ctx.IsTsumo {
		fu += 10 // 门清荣和
	}

	return roundUp10(fu)
}

// isConcealedMeld 报告一个面子在符数计算里是否算「暗」。
//
// d.Melds 是暗牌拆出来的，默认是暗的；但**荣和补上的那一张组成的刻子算明刻**
// ——这条同时影响三暗刻/四暗刻是否成立，以及符数。
func (h *hand) isConcealedMeld(m Meld) bool {
	if m.IsOpen {
		return false
	}
	if h.ctx.IsTsumo {
		return true
	}
	// 荣和：包含和牌张的那个刻子/杠算明
	if m.IsTripletLike() && m.Tile() == h.ctx.WinningTile && m.Tile() != h.pair() {
		return false
	}
	return true
}

// meldFu 返回一个面子的符数。**顺子与对子都恒为 0。**
//
// MeldPair 只在七对子里出现，那种形态固定 25 符、由 CalcFu 提前返回，
// 正常路径下走不到这里。仍然显式返回 0，是为了让「哪天那个提前返回被改掉」
// 表现为一个显眼的错，而不是六对雀头静默地多算 24 符。
func meldFu(m Meld, concealed bool) int {
	if m.IsSequence() || m.Kind == MeldPair {
		return 0
	}
	var fu int
	switch {
	case m.IsQuad() && concealed:
		fu = 16
	case m.IsQuad():
		fu = 8
	case concealed:
		fu = 4
	default:
		fu = 2
	}
	if isYaochu(m.Tile()) {
		fu *= 2 // 幺九（1、9、字牌）翻倍
	}
	return fu
}

// waitFu 返回待ち形带来的符数。
//
//	单骑 +2　嵌张 +2　边张 +2　两面 0　双碰 0
func waitFu(d Decomposition, t Tile) int {
	// 单骑：和牌张就是雀头
	if t == d.Pair {
		return 2
	}
	for _, m := range d.Melds {
		if !m.IsSequence() {
			continue
		}
		a := m.Tile()
		switch {
		case t == a+1:
			return 2 // 嵌张
		case t == a && a.Rank() == 7:
			return 2 // 边张：手里 8、9 听 7
		case t == a+2 && a.Rank() == 1:
			return 2 // 边张：手里 1、2 听 3
		}
	}
	return 0 // 两面 / 双碰
}

// roundUp10 向上取整到 10 符。**只在整个累加完成后调用一次**——分次取整会累积误差。
func roundUp10(fu int) int { return (fu + 9) / 10 * 10 }
