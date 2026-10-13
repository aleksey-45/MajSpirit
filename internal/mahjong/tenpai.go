package mahjong

// tenpai.go —— 听牌判定与听牌料（docs/规则集.md §13.1、§11.1）。
//
// 听牌本身不需要独立算法：遍历 34 种牌，逐张问「加上这一张能否和牌」。
// 所以它的正确性完全建立在 Decompose 之上——Decompose 错了，听牌判定一起错。
//
// 三处依赖它：振听（§2.2）、荒牌流局的听牌料（§11.1）、立直宣言的合法性（协议 §5.3）。
// **三处都不看役**：听一张自己没有役的牌，仍然是听牌；振听的人照样算听牌。

// Waits 返回能让 hand 和牌的全部牌种，按编码升序；空切片表示未听牌。
//
// hand 只传**暗牌**，张数必须是 3k+1（13 / 10 / 7 / 4 / 1）：张数本身决定了还差几个
// 面子，所以和 Decompose 一样，本函数不需要知道副露有几个。
//
// 传已经和牌的 14 张会得到空——那不是「未听牌」，是「已经和了」，两者由调用方区分。
//
// 手里已经攥着 4 张的牌种不会出现在结果里：第 5 张物理上不存在。这一条不需要特判，
// CanWinWith 走到 CountTiles 就会因为超过 4 张失败。
func Waits(hand []Tile) []Tile {
	if len(hand)%3 != 1 {
		return nil
	}
	if _, err := CountTiles(hand); err != nil {
		return nil
	}

	var waits []Tile
	for k := Tile(0); k < NumTileKinds; k++ {
		if CanWinWith(hand, k) {
			waits = append(waits, k)
		}
	}
	return waits
}

// IsTenpai 报告是否听牌，等价于 len(Waits(hand)) > 0。
//
// 振听**不影响**它：振听的人照样算听牌，照样收听牌料（§11.1）。
func IsTenpai(hand []Tile) bool { return len(Waits(hand)) > 0 }

// ContainsWait 报告 tiles 里有没有 waits 中的牌种。
//
// 振听（§2.2）的三种形式里，有两种最后都落到这一个问题上：
//   - 永久振听：自己的舍牌里含当前听牌张
//   - 同巡振听：别人打出自己的听牌张、自己放过（见逃）
//
// 立直后振听是这两种在立直状态下的叠加，问的还是同一件事。
//
// 判的是**牌种**：舍牌里的赤五万同样算五万。直接比较 Tile 会在有人打过赤牌时漏判。
func ContainsWait(tiles, waits []Tile) bool {
	if len(tiles) == 0 || len(waits) == 0 {
		return false
	}

	var want [NumTileKinds]bool
	for _, w := range waits {
		if w.Valid() {
			want[w.Kind()] = true
		}
	}
	for _, t := range tiles {
		if t.Valid() && want[t.Kind()] {
			return true
		}
	}
	return false
}

// tenpaiStick 是荒牌流局时未听牌方合计支付的点数（§11.1）。
const tenpaiStick = 3000

// TenpaiPayments 返回荒牌流局时四家的收支（docs/规则集.md §11.1）：
// 未听牌者合计支付 3000，由听牌者均分。0 人或 4 人听牌时谁都不付。
//
// 结果恒满足 sum(收支) == 0——这是流局，没有钱进出牌桌。
//
// 振听不影响入参：振听的人照样算听牌、照样收钱，所以这一层看不到振听。
func TenpaiPayments(tenpai [4]bool) [4]int {
	n := 0
	for _, t := range tenpai {
		if t {
			n++
		}
	}
	if n == 0 || n == 4 {
		return [4]int{}
	}

	pay := tenpaiStick / (4 - n) // 未听牌者每人支付
	recv := tenpaiStick / n      // 听牌者每人收到

	var out [4]int
	for seat, t := range tenpai {
		if t {
			out[seat] = recv
			continue
		}
		out[seat] = -pay
	}
	return out
}
