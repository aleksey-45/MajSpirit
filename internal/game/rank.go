package game

// rank.go —— 顺位与 M-League ポイント（规则集 §10.4）。
//
// 这部分**不属于规则层**：规则集只算一局的钱，顺位点是一场比赛的结算方式。

var placementPoints = [5]int{0, 50, 10, -10, -30}

// originScore 是ポイント的**原点**（规则集 §10.4）。它不是起始分 25000。
const originScore = 30000

// placementsOf 按分数定顺位，返回 each seat → 名次（1..4）。
//
// 同分时**起家优先**（座位号小的在前）—— 用稳定排序实现，比较里只比分数。
func placementsOf(scores [4]int) [4]int {
	order := []int{0, 1, 2, 3}
	// 插入排序：只有四个元素，而且稳定的性质一眼看得出来 —— sort.SliceStable
	// 也对，但在这里读的人要多想一步"它稳定吗"。
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && scores[order[j]] > scores[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	var placements [4]int
	for rank, seat := range order {
		placements[seat] = rank + 1
	}
	return placements
}
