package mahjong

// acceptance_test.go —— docs/算法验证指南.md §11「阶段验收清单」的横切验证。
//
// 为什么不重复各 *_test.go：牌与记法、牌山、手牌、役种、符数、点数那六组已经
// 逐条覆盖了（见各自的文件）。这里只补 §1.9 点名的那三项横切手段——
// **不变量（§2）、构造法（§3）、穷举比对（§4）**——外加 §2.5 数值范围与 §5 模糊测试。
//
// 清单里唯一无法在此验证的是「可验证性」组（Commit / DeriveSeed / 重放）：
// verify.go 目前只有 package 声明。

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

// ── 共用工具 ────────────────────────────────────────────────────────────

// expandCounts 把「第 i 种牌几张」展开成牌切片，kinds[i] 是第 i 种牌的编码。
func expandCounts(counts []int, kinds []Tile) []Tile {
	out := make([]Tile, 0, 14)
	for i, n := range counts {
		for j := 0; j < n; j++ {
			out = append(out, kinds[i])
		}
	}
	return out
}

// forEachMultiset 枚举「kinds 种牌、每种最多 maxPer 张、合计 total 张」的全部组合。
//
// 为什么不用 rand 抽样：§4 的全部价值在于**穷举**——抽样漏掉的那个组合，
// 恰好就是剪枝写错时唯一会暴露的那个。
func forEachMultiset(kinds, total, maxPer int, fn func(counts []int)) {
	counts := make([]int, kinds)

	var rec func(i, left int)
	rec = func(i, left int) {
		if i == kinds-1 {
			if left <= maxPer {
				counts[i] = left
				fn(counts)
				counts[i] = 0
			}
			return
		}
		hi := min(maxPer, left)
		for n := 0; n <= hi; n++ {
			counts[i] = n
			rec(i+1, left-n)
		}
		counts[i] = 0
	}
	rec(0, total)
}

// ── §2.1 拆解的健全性 ───────────────────────────────────────────────────

// 国士无双的 13 种幺九牌。
var orphanKinds = []Tile{0, 8, 9, 17, 18, 26, 27, 28, 29, 30, 31, 32, 33}

// validMeldShape 校验一个面子的形态是否合法。
//
// 顺子这里有两个容易漏的条件：**同花色**与**不能含字牌**。
// 只看 ts[1] == ts[0]+1 是不够的——9m 的编号是 8，加 1 就是 1s 的编号 9，
// 而 27（东）加 1 会落到 28（南）。
func validMeldShape(m Meld) error {
	ts := m.TilesUsed()

	same := func() bool {
		for i := 1; i < len(ts); i++ {
			if ts[i] != ts[0] {
				return false
			}
		}
		return true
	}

	switch m.Kind {
	case MeldRun:
		if len(ts) != 3 {
			return fmt.Errorf("顺子应有 3 张，得到 %d", len(ts))
		}
		if ts[0].IsHonor() {
			return fmt.Errorf("字牌不能组顺子: %s", FormatTiles(ts))
		}
		if ts[0].Suit() != ts[1].Suit() || ts[1].Suit() != ts[2].Suit() {
			return fmt.Errorf("顺子跨花色: %s", FormatTiles(ts))
		}
		if ts[1] != ts[0]+1 || ts[2] != ts[0]+2 {
			return fmt.Errorf("顺子不连续: %s", FormatTiles(ts))
		}
	case MeldTriplet:
		if len(ts) != 3 || !same() {
			return fmt.Errorf("刻子必须是 3 张相同: %s", FormatTiles(ts))
		}
	case MeldKan:
		if len(ts) != 4 || !same() {
			return fmt.Errorf("杠必须是 4 张相同: %s", FormatTiles(ts))
		}
	case MeldPair:
		if len(ts) != 2 || !same() {
			return fmt.Errorf("对子必须是 2 张相同: %s", FormatTiles(ts))
		}
	default:
		return fmt.Errorf("未知面子类型 %d", m.Kind)
	}
	return nil
}

// validateDecomposition 校验一种拆法自洽：面子形态合法 + 牌不增不减。
func validateDecomposition(t *testing.T, input []Tile, d Decomposition) {
	t.Helper()

	want, err := CountTiles(input)
	if err != nil {
		t.Fatalf("输入本身非法: %s: %v", FormatTiles(input), err)
	}

	switch d.Shape {
	case ShapeKokushi:
		// Decomposition 对国士**只带雀头**（那一对幺九），不带其余 12 张——
		// 所以这里做不了多重集比对，只能反过来验证输入确实是国士形。
		if !bruteKokushi(want, len(input)) {
			t.Errorf("拆法标记为国士，输入却不是国士形: %s", FormatTiles(input))
		}
		return

	case ShapeChiitoitsu:
		if len(d.Melds) != 6 {
			t.Errorf("七对子应有 6 个 MeldPair，得到 %d", len(d.Melds))
		}
		// 七对子的每一对必须互不相同（否则就是四张一样的，不成立）
		for k, n := range mustCounts(t, d.Tiles()) {
			if n != 0 && n != 2 {
				t.Errorf("七对子里第 %d 种牌有 %d 张，应恰好 2 张", k, n)
			}
		}

	case ShapeStandard:
		if got, want := len(d.Melds), (len(input)-2)/3; got != want {
			t.Errorf("%d 张牌应拆成 %d 个面子，得到 %d", len(input), want, got)
		}

	default:
		t.Errorf("未知和牌型 %d", d.Shape)
	}

	for i, m := range d.Melds {
		if err := validMeldShape(m); err != nil {
			t.Errorf("第 %d 个面子非法: %v", i, err)
		}
	}

	// 牌不增不减。**这一步是健全性的核心**：抓"凭空造牌"和"吃掉牌"。
	got := mustCounts(t, d.Tiles())
	if got != want {
		t.Errorf("拆法的牌与输入不一致:\n输入 %s\n拆法 %s",
			FormatTiles(input), FormatTiles(d.Tiles()))
	}
}

func mustCounts(t *testing.T, tiles []Tile) Counts {
	t.Helper()
	c, err := CountTiles(tiles)
	if err != nil {
		t.Fatalf("CountTiles(%s): %v", FormatTiles(tiles), err)
	}
	return c
}

// ── §4.3 独立暴力实现 ───────────────────────────────────────────────────

// bruteChiitoitsu 按计数判七对子。故意不看 Decompose 的结果。
func bruteChiitoitsu(c Counts, n int) bool {
	if n != 14 {
		return false
	}
	pairs := 0
	for _, k := range c {
		switch k {
		case 0:
		case 2:
			pairs++
		default:
			return false
		}
	}
	return pairs == 7
}

// bruteKokushi 按计数判国士无双。
func bruteKokushi(c Counts, n int) bool {
	if n != 14 {
		return false
	}
	doubled := 0
	for _, t := range orphanKinds {
		switch c[t] {
		case 1:
		case 2:
			doubled++
		default:
			return false
		}
	}
	return doubled == 1
}

// canWinBruteForce 是 §4.3 要求的**独立暴力实现**。
//
// 与 Decompose 的关键差别在顺子的枚举方式：
//
//	Decompose：拿到最小的那张牌，**只能把它当顺子的起点**
//	           （因为更小的位置已经空了，所以那个剪枝是对的）
//	这里：    让最小的那张牌处在顺子的**任意位置**（起点 i-2 / i-1 / i）
//
// 这里**不依赖**上面那个剪枝论证，所以两者的失效位置不同：优化版剪枝写错会漏判，
// 这里枚举不全也会漏判，但漏的是不同的路径，能互相暴露。
func canWinBruteForce(tiles []Tile) bool {
	if len(tiles) < 2 || len(tiles)%3 != 2 {
		return false
	}
	counts, err := CountTiles(tiles)
	if err != nil {
		return false
	}
	if bruteChiitoitsu(counts, len(tiles)) || bruteKokushi(counts, len(tiles)) {
		return true
	}

	for p := 0; p < NumTileKinds; p++ {
		if counts[p] < 2 {
			continue
		}
		counts[p] -= 2
		ok := bruteRemoveMelds(&counts)
		counts[p] += 2
		if ok {
			return true
		}
	}
	return false
}

// bruteRemoveMelds 不断取出任意一个合法面子，直到取空。
// 每次都处理**当前最小**的那张牌，但允许它处在顺子的任意位置。
func bruteRemoveMelds(c *Counts) bool {
	i := -1
	for k := 0; k < NumTileKinds; k++ {
		if c[k] > 0 {
			i = k
			break
		}
	}
	if i < 0 {
		return true
	}

	if c[i] >= 3 {
		c[i] -= 3
		ok := bruteRemoveMelds(c)
		c[i] += 3
		if ok {
			return true
		}
	}

	for start := i - 2; start <= i; start++ {
		// 顺子不能跨花色，也不能含字牌：起点的点数必须 ≤ 7
		if start < 0 || start >= numNumberKinds || start%9 > 6 {
			continue
		}
		if c[start] > 0 && c[start+1] > 0 && c[start+2] > 0 {
			c[start]--
			c[start+1]--
			c[start+2]--
			ok := bruteRemoveMelds(c)
			c[start]++
			c[start+1]++
			c[start+2]++
			if ok {
				return true
			}
		}
	}
	return false
}

// ── §4 穷举比对 ─────────────────────────────────────────────────────────

func mustDecompose(t *testing.T, tiles []Tile) []Decomposition {
	t.Helper()
	decs, err := Decompose(tiles)
	if err != nil {
		t.Fatalf("Decompose(%s): %v", FormatTiles(tiles), err)
	}
	return decs
}

// compareExhaustive 在一个穷举域上逐组比对：优化实现 vs 暴力实现，
// 并对每个拆法做健全性校验。
func compareExhaustive(t *testing.T, kinds []Tile, maxPer int, wantTotal int) int {
	t.Helper()

	r := rand.New(rand.NewPCG(1, 2))
	n := 0
	forEachMultiset(len(kinds), 14, maxPer, func(counts []int) {
		n++
		tiles := expandCounts(counts, kinds)

		got := CanWin(tiles)
		want := canWinBruteForce(tiles)
		if got != want {
			t.Fatalf("实现不一致:\n%s\n优化实现=%v 暴力实现=%v",
				FormatTiles(tiles), got, want)
		}

		if got {
			decs := mustDecompose(t, tiles)
			if len(decs) == 0 {
				t.Fatalf("CanWin=true 但 Decompose 返回空: %s", FormatTiles(tiles))
			}
			for _, d := range decs {
				validateDecomposition(t, tiles, d)
			}
		}

		// §2.4 顺序无关性：打乱输入，判定必须不变。
		// 抽样做，全量会把这一条的成本翻倍。
		if n%7 == 0 {
			shuffled := append([]Tile(nil), tiles...)
			Shuffle(shuffled, r)
			if CanWin(shuffled) != got {
				t.Fatalf("顺序无关性失败:\n%s\n%s", FormatTiles(tiles), FormatTiles(shuffled))
			}
		}
	})

	if n != wantTotal {
		t.Errorf("枚举了 %d 组，期望 %d 组", n, wantTotal)
	}
	return n
}

// 单花色域：只含万子，9 种牌、每种最多 4 张、共 14 张。
// (1+x+x²+x³+x⁴)^9 展开后 x¹⁴ 的系数 = 118800。
func TestAcceptanceExhaustiveSingleSuit(t *testing.T) {
	kinds := make([]Tile, 0, 9)
	for r := 1; r <= 9; r++ {
		kinds = append(kinds, MustTile(SuitMan, r))
	}
	compareExhaustive(t, kinds, 4, 118800)
}

// 跨花色边界域：7p 8p 9p 1z 2z 3z。
//
// 这个域专抓单花色域**抓不到**的东西：
//   - 9p(26) 与 1z(27) 在编号上相邻，顺子绝不能跨过去
//   - 含字牌，能验证"字牌不能组顺子"
//   - 含 789p，能验证"顺子不能跨花色"
//
// 注：每种最多 4 张这个现实约束下是 1506 组，不是文档里那个 11628
// （11628 是不限张数的组合数，而 14 > 4 所以约束确实生效）。
func TestAcceptanceExhaustiveSuitBoundary(t *testing.T) {
	kinds := []Tile{
		MustTile(SuitPin, 7), MustTile(SuitPin, 8), MustTile(SuitPin, 9),
		MustTile(SuitHonor, HonorEast), MustTile(SuitHonor, HonorSouth), MustTile(SuitHonor, HonorWest),
	}
	compareExhaustive(t, kinds, 4, 1506)
}

// ── §3 构造法 ───────────────────────────────────────────────────────────

// randomWinningHand 随机造 4 个面子 + 1 个雀头，结果**必然**是和牌型。
//
// 反向测试（拿一手牌问"能不能和"）有个根本缺陷：无法区分"不和"是**漏判**
// 还是**本来就该不和**。构造法给的是确定的**正例**。
func randomWinningHand(r *rand.Rand) []Tile {
	tiles := make([]Tile, 0, 14)
	for i := 0; i < 4; i++ {
		if r.IntN(2) == 0 {
			suit := Suit(r.IntN(3))
			start := r.IntN(7) + 1 // 1..7，保证 start+2 ≤ 9
			for k := 0; k < 3; k++ {
				tiles = append(tiles, MustTile(suit, start+k))
			}
		} else {
			tk := Tile(r.IntN(NumTileKinds))
			for k := 0; k < 3; k++ {
				tiles = append(tiles, tk)
			}
		}
	}
	pair := Tile(r.IntN(NumTileKinds))
	return append(tiles, pair, pair)
}

// randomBoundaryWinningHand 提高边界情况的权重（§3「构造法的进阶用法」）。
//
// 纯随机很少命中这些：顺子起点在第 7 张、字牌面子、同种恰好 4 张、
// 以及"同一副牌有多种拆法"的形状。
func randomBoundaryWinningHand(r *rand.Rand) []Tile {
	tiles := make([]Tile, 0, 14)
	honors := []Tile{
		MustTile(SuitHonor, HonorEast), MustTile(SuitHonor, HonorSouth),
		MustTile(SuitHonor, HonorWest), MustTile(SuitHonor, HonorNorth),
		MustTile(SuitHonor, HonorWhite), MustTile(SuitHonor, HonorGreen),
		MustTile(SuitHonor, HonorRed),
	}

	for i := 0; i < 4; i++ {
		switch r.IntN(4) {
		case 0: // 顺子起点在第 7 张：789m / 789s / 789p
			suit := Suit(r.IntN(3))
			for k := 0; k < 3; k++ {
				tiles = append(tiles, MustTile(suit, 7+k))
			}
		case 1: // 顺子起点在第 1 张：123m / 123s / 123p
			suit := Suit(r.IntN(3))
			for k := 0; k < 3; k++ {
				tiles = append(tiles, MustTile(suit, 1+k))
			}
		case 2: // 字牌刻子
			tk := honors[r.IntN(len(honors))]
			for k := 0; k < 3; k++ {
				tiles = append(tiles, tk)
			}
		default: // 中张刻子
			suit := Suit(r.IntN(3))
			tk := MustTile(suit, 2+r.IntN(7))
			for k := 0; k < 3; k++ {
				tiles = append(tiles, tk)
			}
		}
	}

	if r.IntN(2) == 0 {
		tk := honors[r.IntN(len(honors))]
		return append(tiles, tk, tk)
	}
	tk := Tile(r.IntN(NumTileKinds))
	return append(tiles, tk, tk)
}

func TestAcceptanceConstructedHandsAlwaysWin(t *testing.T) {
	const rounds = 100000

	tests := []struct {
		name string
		gen  func(*rand.Rand) []Tile
	}{
		{"均匀", randomWinningHand},
		{"边界加权", randomBoundaryWinningHand},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := rand.New(rand.NewPCG(42, 43))
			checked, skipped := 0, 0

			for i := 0; i < rounds; i++ {
				hand := tt.gen(r)

				// 构造时可能造出同种 5 张（比如抽到两次「东东东」），
				// 那种牌在现实里不存在，跳过
				if _, err := CountTiles(hand); err != nil {
					skipped++
					continue
				}
				checked++

				if !CanWin(hand) {
					t.Fatalf("构造出的和牌型被判为不能和:\n%s", FormatTiles(hand))
				}
			}
			t.Logf("核对了 %d 手，跳过 %d 手（同种超 4 张）", checked, skipped)
		})
	}
}

// 构造法的反向：从和牌型里拿走一张，那张牌必须是**听牌张**。
func TestAcceptanceConstructedWaitsAreReal(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	checked := 0

	for i := 0; i < 20000 && checked < 5000; i++ {
		hand := randomWinningHand(r)
		if _, err := CountTiles(hand); err != nil {
			continue
		}
		checked++

		// 拿走任意一张，剩下的 13 张 + 那张牌必须能和
		cut := r.IntN(len(hand))
		winning := hand[cut]
		rest := append(append([]Tile(nil), hand[:cut]...), hand[cut+1:]...)

		if !CanWinWith(rest, winning) {
			t.Fatalf("从和牌型里拿走一张后，那张牌不再是听牌张:\n原手 %s\n剩 %s + %s",
				FormatTiles(hand), FormatTiles(rest), winning)
		}
	}
	t.Logf("核对了 %d 组", checked)
}

// ── §2.5 数值范围 ───────────────────────────────────────────────────────

// 符数输出必须落在合法集合内：0（役满）、25（七对子）、或 20..110 的十的倍数。
func TestAcceptanceFuInLegalSet(t *testing.T) {
	legal := func(fu int) bool {
		if fu == 0 || fu == 25 {
			return true
		}
		return fu >= 20 && fu <= 110 && fu%10 == 0
	}

	r := rand.New(rand.NewPCG(11, 12))
	checked := 0

	for i := 0; i < 20000 && checked < 3000; i++ {
		hand := randomBoundaryWinningHand(r)
		// 构造法会造出「同种 5 张」这种现实中不存在的牌（比如两次抽到东东东），
		// 必须跳过——否则 Decompose 会按契约返回错误。
		if _, err := CountTiles(hand); err != nil {
			continue
		}
		winning := hand[r.IntN(len(hand))]

		decs := mustDecompose(t, hand)
		for _, d := range decs {
			ctx := YakuContext{
				WinningTile: winning,
				IsMenzen:    r.IntN(2) == 0,
				IsTsumo:     r.IntN(2) == 0,
				RoundWind:   MustTile(SuitHonor, HonorEast),
				SeatWind:    MustTile(SuitHonor, HonorSouth),
			}
			got := CalcFu(d, ctx)
			if !legal(got) {
				t.Fatalf("符数落在合法集合外: %d\n手牌 %s\n拆法 %+v",
					got, FormatTiles(hand), d)
			}
		}
		checked++
	}
	t.Logf("核对了 %d 手", checked)
}

// ── §5 模糊测试 ─────────────────────────────────────────────────────────

// FuzzCanWin 用随机字节喂拆解算法。
//
//	go test ./internal/mahjong/ -run '^$' -fuzz FuzzCanWin -fuzztime 60s
//
// 不变量有两条：**不 panic**，以及与独立暴力实现**判定一致**。
func FuzzCanWin(f *testing.F) {
	for _, s := range []string{
		"123m456s789p11z", "1112345678999m", "11223344556677m",
		"19m19s19p1234567z", "11122233344455m", "11112222333344m",
	} {
		tiles := MustParseTiles(s)
		b := make([]byte, len(tiles))
		for i, tk := range tiles {
			b[i] = byte(tk)
		}
		f.Add(b)
	}
	f.Add([]byte{})                   // 空
	f.Add([]byte{255, 255, 255, 255}) // 越界
	f.Add(make([]byte, 14))           // 全一万

	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > WinningHandSize {
			b = b[:WinningHandSize]
		}
		tiles := make([]Tile, len(b))
		for i, x := range b {
			tiles[i] = Tile(int(x) % NumTileKinds)
		}

		if _, err := CountTiles(tiles); err != nil {
			return // 非法输入，优化实现的契约是返回错误
		}

		got := CanWin(tiles)
		want := canWinBruteForce(tiles)
		if got != want {
			t.Fatalf("实现不一致:\n%s\n优化实现=%v 暴力实现=%v",
				FormatTiles(tiles), got, want)
		}

		if got {
			for _, d := range mustDecompose(t, tiles) {
				validateDecomposition(t, tiles, d)
			}
		}
	})
}
