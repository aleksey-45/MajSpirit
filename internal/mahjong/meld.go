package mahjong

import "slices"

// meld.go —— 吃 / 碰 / 杠的成立判定与立直宣言的合法性（docs/规则集.md §11.2、§8.5）。
//
// 这里只判**牌面**。下面这些事实规则层拿不到，全部由房间层把关：
//
//   - 吃只能吃上家——那是座位关系，不是牌面
//   - 立直要 1000 点以上、牌山还剩至少 4 张
//   - 帕奥责任者在鸣牌那一刻就定下来了（§8.5），不能等和牌时回头算

// MaxKans 是一局里开杠总数的上限（docs/规则集.md §11.2）。
//
// M-League 用这条替代「四杠散了」：**第 5 个杠这个动作本身被拒**，牌局继续，
// 不是流局。四个杠可以集中在一个人身上（四槓子），也可以分散。
const MaxKans = 4

// ChiOptions 返回用手牌里的哪两张可以吃 called，两张按升序。
//
// 返回**组合列表**而不是布尔，有两个用处：房间层用它填 claimWindow 的候选，
// 判定玩家发来的那两张牌时只要在这个列表里查一下。顺序先排好，比较才不会比出假差异。
//
// 字牌不能吃；顺子不跨花色，所以 1-2 与 9 附近的形状要卡在花色边界内。
func ChiOptions(hand []Tile, called Tile) [][2]Tile {
	k := int(called.Kind())
	if !Tile(k).IsNumber() {
		return nil
	}
	counts, err := CountTiles(hand)
	if err != nil {
		return nil
	}

	base := k / 9 * 9 // 这一花色第一张的编码
	rank := k - base  // 0..8

	var out [][2]Tile
	// called 分别充当顺子的第 1 / 第 2 / 第 3 张。
	for _, off := range [3][2]int{{-2, -1}, {-1, 1}, {1, 2}} {
		r1, r2 := rank+off[0], rank+off[1]
		if r1 < 0 || r2 > 8 {
			continue
		}
		t1, t2 := Tile(base+r1), Tile(base+r2)
		if counts[t1] == 0 || counts[t2] == 0 {
			continue
		}
		out = append(out, [2]Tile{t1, t2})
	}
	return out
}

// CanPon 报告能否碰：手里有两张 called。
func CanPon(hand []Tile, called Tile) bool { return countOfKind(hand, called) >= 2 }

// CanKanOpen 报告能否大明杠：手里有三张 called。
func CanKanOpen(hand []Tile, called Tile) bool { return countOfKind(hand, called) >= 3 }

// countOfKind 数 hand 里有几张 called 这一种。非法输入一律返回 0。
func countOfKind(hand []Tile, called Tile) int {
	if !called.Valid() {
		return 0
	}
	counts, err := CountTiles(hand)
	if err != nil {
		return 0
	}
	return counts[called.Kind()]
}

// ClosedKanTiles 返回手里能暗杠的牌种，升序。
//
// 暗杠不破门清（§14.2），所以立直之后、门清状态下都可能用得上。
func ClosedKanTiles(hand []Tile) []Tile {
	counts, err := CountTiles(hand)
	if err != nil {
		return nil
	}

	var out []Tile
	for k := Tile(0); k < NumTileKinds; k++ {
		if counts[k] == numCopies {
			out = append(out, k)
		}
	}
	return out
}

// AddedKanTiles 返回能加杠的牌种：已经**碰出来**的刻子，加上手里第 4 张。
//
// 只认明刻。手里暗着的三张还没成为一个 Meld（它在 Decomposition 里），那一条路
// 走暗杠，由 ClosedKanTiles 覆盖——两者判的是同一件事的两个不同状态。
func AddedKanTiles(hand []Tile, melds []Meld) []Tile {
	counts, err := CountTiles(hand)
	if err != nil {
		return nil
	}

	var out []Tile
	for _, m := range melds {
		if m.Kind != MeldTriplet || !m.IsOpen {
			continue
		}
		if k := m.Tile().Kind(); counts[k] > 0 {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

// IsMenzen 报告门清：没有任何副露。
//
// 暗杠不算副露（§14.2），所以判据是「没有 IsOpen 的面子」，不是「没有面子」——
// 按后者写会让立直后暗杠的人当场失去门清，役种表里所有 -1 列全部失效。
func IsMenzen(melds []Meld) bool {
	for _, m := range melds {
		if m.IsOpen {
			return false
		}
	}
	return true
}

// RiichiDiscards 返回宣告立直时能打出的牌种，升序；空表示不能立直。
//
// hand 是宣言当时的暗牌（3k+2 张，含刚摸的那张）。协议 §5.3 把立直做成
// 「宣言 + 打牌」一条指令，所以判定必须同时看「打哪张」和「打完听不听」——
// 只看「现在听不听」会放过「打了之后不听」的宣言。
//
// 返回的是**牌种**不是具体的牌：手里同时有 5m 和赤 5m 时，两张打完都听同一批牌，
// 选哪张由房间层按赤宝牌的取舍决定。
//
// 门清这一条在这里判（melds 就在手边）；点数与牌山余量由房间层把关。
func RiichiDiscards(hand []Tile, melds []Meld) []Tile {
	if !IsMenzen(melds) || len(hand)%3 != 2 {
		return nil
	}
	counts, err := CountTiles(hand)
	if err != nil {
		return nil
	}

	var out []Tile
	for k := Tile(0); k < NumTileKinds; k++ {
		if counts[k] == 0 {
			continue
		}
		if IsTenpai(withoutOne(hand, k)) {
			out = append(out, k)
		}
	}
	return out
}

// withoutOne 返回去掉一张 kind 之后的牌。输入顺序保持不变。
func withoutOne(hand []Tile, kind Tile) []Tile {
	out := make([]Tile, 0, len(hand))
	removed := false
	for _, t := range hand {
		if !removed && t.Kind() == kind {
			removed = true
			continue
		}
		out = append(out, t)
	}
	return out
}
