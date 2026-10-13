package mahjong

import (
	"crypto/sha256"
	"maps"
	"math/rand/v2"
	"slices"
	"testing"
)

// testSeed 是测试用的固定种子。内容随意，但必须写死以保证可复现。
var testSeed = [32]byte{
	0x4d, 0x61, 0x6a, 0x53, 0x70, 0x69, 0x72, 0x69, 0x74, // "MajSpirit"
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
	0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
	0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16,
}

// countsOf 统计每种牌的张数，用于校验「是排列」「张数守恒」这类不变量。
func countsOf(tiles []Tile) map[Tile]int {
	m := make(map[Tile]int, NumTileKinds)
	for _, t := range tiles {
		m[t]++
	}
	return m
}

// fullSetCounts 是完整牌堆的多重集：每种 4 张。任何洗牌结果都必须与它相等。
func fullSetCounts() map[Tile]int { return countsOf(FullSet()) }

func TestWallIsComplete(t *testing.T) {
	w := NewWallFromSeed(testSeed)

	if got := w.Remaining(); got != NumTiles-DeadWallSize {
		t.Errorf("新牌山剩余 %d 张，期望 %d 张", got, NumTiles)
	}
	if w.Empty() {
		t.Error("新牌山不应为空")
	}
	if !maps.Equal(countsOf(w.Tiles()), fullSetCounts()) {
		t.Error("新牌山的牌种构成与完整牌堆不一致")
	}
}

func TestWallIsDeterministic(t *testing.T) {
	a := NewWallFromSeed(testSeed).Bytes()
	b := NewWallFromSeed(testSeed).Bytes()

	if !slices.Equal(a, b) {
		t.Error("同一个种子两次构造结果不同——牌山不可复现，验证无从谈起")
	}
}

func TestWallDiffersForDifferentSeeds(t *testing.T) {
	var a, b [32]byte
	a[0], b[0] = 1, 2

	if slices.Equal(NewWallFromSeed(a).Bytes(), NewWallFromSeed(b).Bytes()) {
		t.Error("不同种子产出了相同牌山")
	}
}

// 种子必须用满全部 32 字节。逐个字节改变种子，牌山都必须随之改变——
// 只用首字节（或其他任何一部分）会让候选空间小到能被穷举，
// 玩家拿到承诺值就能在开赛前算出整副牌。
func TestWallSeedUsesAllBytes(t *testing.T) {
	var base [32]byte
	baseWall := NewWallFromSeed(base).Bytes()

	for i := range base {
		alt := base
		alt[i] = 0xFF

		if slices.Equal(NewWallFromSeed(alt).Bytes(), baseWall) {
			t.Errorf("只改变种子第 %d 字节，牌山却完全相同——该字节没被使用", i)
		}
	}
}

// 牌山指纹回归。
//
// 这条测试的失效场景很具体：Go 升级后标准库行为变化，历史牌局全部验证不过——
// 而在用户投诉之前，只有这里会先一步报出来。
//
// 更新常量时必须说明原因，否则它就从"护栏"退化成"橡皮图章"。
//
// 2026-10-03 变更记录：引入赤宝牌（Tile 34/35/36）后，FullSet 把每种花色的五
// 替换一张成赤牌，所以同一副洗好的牌多出 3 个不同的字节。洗牌序列本身没变，
// 已核对：把新牌山逐张 Kind() 归一之后，指纹恰好等于上面那个旧常量
// 10b5c2b0…226e9f7。也就是说这次只动了牌的编码，没动 Shuffle。
func TestWallFingerprint(t *testing.T) {
	got := sha256.Sum256(NewWallFromSeed(testSeed).Bytes())

	want := [32]byte{
		0xc0, 0x74, 0x20, 0xb5, 0xeb, 0x1c, 0xc6, 0xd6,
		0xe8, 0xb6, 0xde, 0x58, 0xe9, 0x0c, 0xe1, 0x6b,
		0x09, 0x48, 0x1c, 0x9b, 0x20, 0xfd, 0x5e, 0x73,
		0x27, 0x56, 0x28, 0x37, 0x6b, 0x73, 0x5b, 0xed,
	}
	if got != want {
		t.Fatalf("牌山指纹变了：\n得到 %x\n期望 %x\n"+
			"三种可能：① Go 升级导致洗牌实现变了；② 你改动了 Shuffle 或 FullSet；"+
			"③ 你改了赤宝牌的写码。确认属于哪一种之后再更新这个常量", got, want)
	}
}

func TestShuffleIsPermutation(t *testing.T) {
	want := fullSetCounts()

	for _, first := range []byte{0, 1, 7, 127, 255} {
		var seed [32]byte
		seed[0] = first

		if got := countsOf(NewWallFromSeed(seed).Tiles()); !maps.Equal(got, want) {
			t.Errorf("种子首字节 %d：洗牌前后牌的多重集不同", first)
		}
	}
}

func TestNewWallWithInjectedRand(t *testing.T) {
	// 相同随机源状态必须产出相同牌山
	w1 := NewWall(rand.New(rand.NewPCG(1, 2)))
	w2 := NewWall(rand.New(rand.NewPCG(1, 2)))

	if !slices.Equal(w1.Bytes(), w2.Bytes()) {
		t.Error("相同随机源状态产出了不同牌山")
	}
	if !maps.Equal(countsOf(w1.Tiles()), fullSetCounts()) {
		t.Error("注入随机源的洗牌结果不是排列")
	}
}

func TestWallDrawConsumesAll(t *testing.T) {
	w := NewWallFromSeed(testSeed)

	counts := make(map[Tile]int, NumTileKinds)
	for i := 0; i < NumTiles-DeadWallSize; i++ {
		tile, ok := w.Draw()
		if !ok {
			t.Fatalf("第 %d 次摸牌失败，还剩 %d 张", i+1, w.Remaining())
		}
		counts[tile]++
	}

	// 可摸的摸完之后，摸出来的 + 剩下的（王牌）合起来恰好是完整牌堆。
	// 这一条同时验证了「确实在消耗」和「洗牌没弄丢也没多出牌」。
	combined := countsOf(w.Tiles())
	for t, n := range counts {
		combined[t] += n
	}
	if !maps.Equal(combined, fullSetCounts()) {
		t.Error("摸完可摸的牌之后，摸出来的加剩下的与完整牌堆不一致")
	}
	if !w.Empty() {
		t.Errorf("摸完 %d 张后牌山仍非空，剩余 %d 张", NumTiles, w.Remaining())
	}
	if _, ok := w.Draw(); ok {
		t.Error("空牌山不应还能摸出牌")
	}
}

func TestWallDrawFollowsOrder(t *testing.T) {
	w := NewWallFromSeed(testSeed)
	order := w.Tiles()[:NumTiles-DeadWallSize]

	for i := range order {
		got, ok := w.Draw()
		if !ok {
			t.Fatalf("第 %d 次摸牌失败", i+1)
		}
		if got != order[i] {
			t.Fatalf("第 %d 次摸到 %s，期望 %s（摸牌顺序应与 Tiles 一致）", i+1, got, order[i])
		}
		if want := NumTiles - DeadWallSize - i - 1; w.Remaining() != want {
			t.Fatalf("摸走 %d 张后剩余 %d 张，期望 %d 张", i+1, w.Remaining(), want)
		}
	}
}

func TestWallTilesReturnsCopy(t *testing.T) {
	w := NewWallFromSeed(testSeed)
	before := w.Bytes()

	got := w.Tiles()
	got[0] = Tile(99)

	if !slices.Equal(w.Bytes(), before) {
		t.Error("改动 Tiles() 的返回值影响了牌山")
	}
}

func TestWallBytesReturnsCopy(t *testing.T) {
	w := NewWallFromSeed(testSeed)
	before := w.Bytes()

	got := w.Bytes()
	got[0] = 99

	if !slices.Equal(w.Bytes(), before) {
		t.Error("改动 Bytes() 的返回值影响了牌山")
	}
}

func TestWallBytesMatchesTiles(t *testing.T) {
	w := NewWallFromSeed(testSeed)

	raw := w.Bytes()
	tiles := w.Tiles()

	if len(raw) != len(tiles) {
		t.Fatalf("Bytes() 长度 %d，Tiles() 长度 %d", len(raw), len(tiles))
	}
	for i, tile := range tiles {
		if raw[i] != byte(tile) {
			t.Fatalf("第 %d 个字节 %d 与对应牌 %s 不符", i, raw[i], tile)
		}
	}
}

// Sequence 的输出必须能被 ParseTiles 原样读回——两个模块互为逆运算。
func TestWallSequenceRoundTrips(t *testing.T) {
	w := NewWallFromSeed(testSeed)
	seq := w.Sequence()

	if got, want := len(seq), NumTiles*2; got != want {
		t.Errorf("Sequence() 长度 %d，期望 %d（每张牌 2 个字符）", got, want)
	}

	got, err := ParseTiles(seq)
	if err != nil {
		t.Fatalf("Sequence() 的输出无法被 ParseTiles 读回: %v", err)
	}
	if !slices.Equal(got, w.Tiles()) {
		t.Error("往返后牌山的顺序或内容不一致")
	}
}

func TestWallDeal(t *testing.T) {
	w := NewWallFromSeed(testSeed)
	before := w.Remaining()

	hands, err := w.Deal()
	if err != nil {
		t.Fatalf("Deal 出错: %v", err)
	}

	var total int
	for seat, hand := range hands {
		if len(hand) != HandSize {
			t.Errorf("座位 %d 发了 %d 张，期望 %d 张", seat, len(hand), HandSize)
		}
		total += len(hand)
	}

	// 守恒：发到手的张数必须等于牌山减少的张数。
	// 这条能抓住「发了 52 张、到手的只有 51 张」这类丢牌 bug——
	// 只断言某家有 13 张是抓不住的。
	if consumed := before - w.Remaining(); total != consumed {
		t.Errorf("发到 %d 张，牌山却减少 %d 张——有牌丢失", total, consumed)
	}
	if want := NumTiles - DeadWallSize - HandSize*4; w.Remaining() != want {
		t.Errorf("发牌后剩余 %d 张，期望 %d 张", w.Remaining(), want)
	}
}

// 庄家的第 14 张不在 Deal 里发，而是作为它的第一次摸牌。
func TestDealerDrawsFourteenthTile(t *testing.T) {
	w := NewWallFromSeed(testSeed)

	hands, err := w.Deal()
	if err != nil {
		t.Fatalf("Deal 出错: %v", err)
	}

	dealer := 0
	tile, ok := w.Draw()
	if !ok {
		t.Fatal("庄家摸第 14 张时失败")
	}

	if got := len(hands[dealer]) + 1; got != WinningHandSize {
		t.Errorf("庄家手牌 %d 张，期望 %d 张", got, WinningHandSize)
	}
	if !tile.Valid() {
		t.Errorf("庄家摸到了非法的牌 Tile(%d)", tile)
	}
	if got, want := w.Remaining(), NumTiles-DeadWallSize-HandSize*4-1; got != want {
		t.Errorf("庄家摸牌后剩余 %d 张，期望 %d 张", got, want)
	}
}

func TestWallDealConservesTilesOverFourSeats(t *testing.T) {
	w := NewWallFromSeed(testSeed)

	hands, err := w.Deal()
	if err != nil {
		t.Fatalf("Deal 出错: %v", err)
	}

	// 四家手牌加上剩余的牌山，合起来必须还原出完整牌堆
	var all []Tile
	for _, hand := range hands {
		all = append(all, hand...)
	}
	all = append(all, w.Tiles()...)

	if len(all) != NumTiles {
		t.Fatalf("四家手牌加牌山共 %d 张，期望 %d 张", len(all), NumTiles)
	}
	if !maps.Equal(countsOf(all), fullSetCounts()) {
		t.Error("发牌后所有可见的牌合起来不构成完整牌堆")
	}
}

func TestWallDealTooShort(t *testing.T) {
	w := NewWallFromSeed(testSeed)

	const need = HandSize * 4
	// 摸到只剩 need-1 张
	for w.Remaining() > need-1 {
		w.Draw()
	}
	if got := w.Remaining(); got != need-1 {
		t.Fatalf("前置条件错误：剩余 %d 张，期望 %d 张", got, need-1)
	}

	if _, err := w.Deal(); err == nil {
		t.Errorf("余牌 %d 张时 Deal 应当报错", need-1)
	}
}

// 王牌：14 张永远不按常规摸走，可摸张数因此是 136−14−52 = 70。
//
// 这一条防的是"杠从空气里变出牌来"：每次杠摸 1 张岭上、同时从牌山末尾补 1 张
// 进王牌，所以**可摸张数净减 1**。少了补牌这一步，四个杠就凭空多摸四张，
// 验牌时那四张的来源对不上。
func TestDeadWallRinshanAndIndicators(t *testing.T) {
	w := NewWallFromSeed(testSeed)
	if got := len(w.DoraIndicators()); got != 1 {
		t.Errorf("开局宝牌指示牌 %d 张，期望 1 张", got)
	}
	if got := len(w.UraIndicators()); got != 1 {
		t.Errorf("开局里宝指示牌 %d 张，期望 1 张", got)
	}

	before := w.Remaining()
	for i := 1; i <= 4; i++ {
		if _, ok := w.DrawRinshan(); !ok {
			t.Fatalf("第 %d 次岭上摸牌失败", i)
		}
		if got := len(w.DoraIndicators()); got != 1+i {
			t.Errorf("杠 %d 次后宝牌指示牌 %d 张，期望 %d 张", i, got, 1+i)
		}
		if got := w.Remaining(); got != before-i {
			t.Errorf("杠 %d 次后可摸 %d 张，期望 %d 张（每次杠净减 1）", i, got, before-i)
		}
	}
	if w.Kans() != 4 {
		t.Errorf("杠数 = %d，期望 4", w.Kans())
	}

	// 岭上牌是**摸不完的**：每次杠都从牌山末尾补一张进王牌。真正封顶的是规则里的
	// `MaxKans`（四个杠）和宝牌指示牌只有 5 张。所以这里断言的是后者：
	// 一直杠下去，指示牌也不会超出那 5 张。
	for range 4 {
		w.DrawRinshan()
		if got := len(w.DoraIndicators()); got > indicatorSize {
			t.Fatalf("宝牌指示牌 %d 张，最多只该有 %d 张", got, indicatorSize)
		}
	}
}

// 承诺值必须覆盖**整副**牌：只 hash 可摸的那 122 张，服务端就能事后摆布王牌。
func TestWallHashCoversDeadWall(t *testing.T) {
	a := NewWallFromSeed(testSeed)

	// 换掉王牌里的一张（模拟服务端想改宝牌指示牌），指纹必须变。
	tiles := FullSet()
	Shuffle(tiles, rand.New(rand.NewChaCha8(testSeed)))
	n := len(tiles) - DeadWallSize
	tiles[n], tiles[0] = tiles[0], tiles[n]

	b, err := NewWallFromTiles(tiles)
	if err != nil {
		t.Fatal(err)
	}
	if WallHash(a) == WallHash(b) {
		t.Error("王牌换了牌，指纹却没变——承诺值没覆盖王牌")
	}
}

// 岭上摸牌**不能改动宝牌指示牌**。
//
// 这条抓的是 Go 切片的一个陷阱：`newWall` 把一副 136 张的切片切成四段，四段共享
// 同一个底层数组；`rinshan` 的容量一直伸到数组末尾，所以 `append(rinshan, x)`
// 会**就地写入** —— 那个位置正是 `dora[0]`。三索引切片（`a[i:j:k]`）把容量卡死，
// append 才会去申请新数组。
func TestRinshanDoesNotCorruptDoraIndicators(t *testing.T) {
	w := NewWallFromSeed(testSeed)
	before := append([]Tile(nil), w.DoraIndicators()...)

	if _, ok := w.DrawRinshan(); !ok {
		t.Fatal("岭上摸牌失败")
	}

	after := w.DoraIndicators()
	for i, want := range before {
		if after[i] != want {
			t.Fatalf("杠之后宝牌指示牌被改了：%v → %v", before, after)
		}
	}
}
