package game

import "github.com/aleksey-45/majspirit/internal/mahjong"

// pao.go —— 帕奥（包 / 責任払い）：大三元、大四喜、四槓子三种役满，由**喂成它的
// 那一家独自承担全额**（规则集 §8.5）。
//
// 责任必须记在**鸣牌那一刻**：副露里只留下"有一组白板"，看不出那张牌是谁打的。
// 事后从副露反推是推不出来的 —— 这是这个文件唯一的存在理由。
//
// 触发点是"完成役满的那一次鸣牌"：
//
//	大三元  第三组三元牌的碰/明杠
//	大四喜  第四组风牌的碰/明杠
//	四槓子  第四次杠，且必须是碰/明杠（暗杠、加杠没有放铳者，也就没有责任者）

// paoInfo 是某个座位的责任：谁喂的、喂成了什么。
type paoInfo struct {
	seat int    // 责任者（打牌的那家）
	yaku string // 被喂成的役满名，与规则层的役名一致
}

// notePao 在鸣牌之后检查是否刚好凑成某一种"有责任者"的役满。
//
// 调在**每次副露之后**，靠"刚好达到阈值"触发：三元牌只有三种所以第三组就是最后一次，
// 风牌四组、杠封顶四个（`MaxKans`），都不可能超。所以不需要额外的"已经记过"标志。
func (g *Game) notePao(seat, from int) {
	switch {
	case g.tripletsOf(seat, honorWhite, honorGreen, honorRed) >= 3:
		g.pao[seat] = &paoInfo{seat: from, yaku: "大三元"}
	case g.tripletsOf(seat, honorEast, honorSouth, honorWest, honorNorth) >= 4:
		g.pao[seat] = &paoInfo{seat: from, yaku: "大四喜"}
	case g.kanCount(seat) >= mahjong.MaxKans:
		g.pao[seat] = &paoInfo{seat: from, yaku: "四槓子"}
	}
}

// tripletsOf 数这个座位的副露里有几组是指定的那几种牌（刻子或杠都算）。
func (g *Game) tripletsOf(seat int, kinds ...mahjong.Tile) int {
	n := 0
	for _, m := range g.melds[seat] {
		if m.Kind != mahjong.MeldTriplet && m.Kind != mahjong.MeldKan {
			continue
		}
		for _, k := range kinds {
			if m.Tile().Kind() == k.Kind() {
				n++
				break
			}
		}
	}
	return n
}

// 字牌种类（编码见 mahjong/tile.go：27..33 是东南西北白发中）。
const (
	honorEast  = mahjong.Tile(27)
	honorSouth = mahjong.Tile(28)
	honorWest  = mahjong.Tile(29)
	honorNorth = mahjong.Tile(30)
	honorWhite = mahjong.Tile(31)
	honorGreen = mahjong.Tile(32)
	honorRed   = mahjong.Tile(33)
)

// paoSeatOf 返回荣和这个座位时的帕奥责任者；没有责任者返回 `mahjong.NoSeat`。
func (g *Game) paoSeatOf(seat int) int {
	if p := g.pao[seat]; p != nil {
		return p.seat
	}
	return mahjong.NoSeat
}

// paoYakuOf 返回被喂成的那个役满名；没有则空串。
func (g *Game) paoYakuOf(seat int) string {
	if p := g.pao[seat]; p != nil {
		return p.yaku
	}
	return ""
}
