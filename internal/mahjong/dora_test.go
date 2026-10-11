package mahjong

import "testing"

// dora_test.go —— 宝牌与赤宝牌。规则见 docs/规则集.md §4。

// 指示牌 → 宝牌。字牌是两段独立循环，这是最容易写成单一取模然后错掉的地方。
func TestDoraFromIndicator(t *testing.T) {
	tests := []struct {
		name string
		in   Tile
		want Tile
	}{
		{"一万 → 二万", MustTile(SuitMan, 1), MustTile(SuitMan, 2)},
		{"九万 → 一万", MustTile(SuitMan, 9), MustTile(SuitMan, 1)},
		{"四索 → 五索", MustTile(SuitSou, 4), MustTile(SuitSou, 5)},
		{"九筒 → 一筒", MustTile(SuitPin, 9), MustTile(SuitPin, 1)},

		// 风牌段：北(30) 的下一个是东(27)，不是白(31)
		{"东 → 南", MustTile(SuitHonor, HonorEast), MustTile(SuitHonor, HonorSouth)},
		{"北 → 东", MustTile(SuitHonor, HonorNorth), MustTile(SuitHonor, HonorEast)},
		// 三元牌段：中(33) 的下一个是白(31)，不是东(27)
		{"白 → 发", MustTile(SuitHonor, HonorWhite), MustTile(SuitHonor, HonorGreen)},
		{"中 → 白", MustTile(SuitHonor, HonorRed), MustTile(SuitHonor, HonorWhite)},

		// 赤宝牌也会被翻开当指示牌，必须先归一到种类
		{"赤五万 → 六万", AkaMan5, MustTile(SuitMan, 6)},
		{"赤五筒 → 六筒", AkaPin5, MustTile(SuitPin, 6)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DoraFromIndicator(tt.in); got != tt.want {
				t.Errorf("DoraFromIndicator(%s) = %s，期望 %s", tt.in, got, tt.want)
			}
		})
	}
}

// 非法指示牌原样返回，不 panic、不给出看似合理的假结果。
func TestDoraFromIndicatorInvalid(t *testing.T) {
	for _, v := range []Tile{NumTileCodes, 100, 255} {
		if got := DoraFromIndicator(v); got != v {
			t.Errorf("DoraFromIndicator(Tile(%d)) = Tile(%d)，期望原样返回", v, got)
		}
	}
}

func TestCountDora(t *testing.T) {
	tests := []struct {
		name  string
		hand  string
		indic []string
		want  int
	}{
		{"无指示牌", "123m456s789p11z", nil, 0},
		{"九万指示一万", "123m456s789p11z", []string{"9m"}, 1},
		// 两张相同的指示牌意味着宝牌翻倍，不能去重
		{"同一指示牌重复两次", "123m456s789p11z", []string{"9m", "9m"}, 2},
		{"指示牌不在手里", "123m456s789p11z", []string{"5m"}, 0},
		{"两张不同指示牌", "123m456s789p11z", []string{"9m", "5s"}, 2},
		// 赤五万按种类参与普通宝牌：五万是宝牌时它照样算一张
		{"赤五万按种类算宝牌", "0m", []string{"4m"}, 1},
		{"赤五万不是宝牌时不算", "0m", []string{"9m"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indic := make([]Tile, 0, len(tt.indic))
			for _, s := range tt.indic {
				indic = append(indic, MustParseTiles(s)[0])
			}
			if got := CountDora(MustParseTiles(tt.hand), indic); got != tt.want {
				t.Errorf("CountDora(%q, %v) = %d，期望 %d", tt.hand, tt.indic, got, tt.want)
			}
		})
	}
}

func TestCountAka(t *testing.T) {
	tests := []struct {
		hand string
		want int
	}{
		{"", 0},
		{"123m456s789p11z", 0},
		{"0m", 1},
		{"0m0s0p", 3},
		{"05m", 1},
		{"5m", 0},
		{"0m0s0p123m456s789p11z", 3},
		// 每种花色只有一张赤牌，所以同花色不可能出现两张
		{"0m0m", 2}, // 函数本身不校验牌山，只管数
	}

	for _, tt := range tests {
		if got := CountAka(MustParseTiles(tt.hand)); got != tt.want {
			t.Errorf("CountAka(%q) = %d，期望 %d", tt.hand, got, tt.want)
		}
	}
}

// Hand 上的三个查询口径必须自洽：Counts/Count 按种类，CountAka 单独数赤牌。
func TestHandAkaQueries(t *testing.T) {
	h := NewHand(MustParseTiles("0m5m5m123s"))

	if got := h.CountAka(); got != 1 {
		t.Errorf("Hand.CountAka() = %d，期望 1", got)
	}
	// Count 按种类：赤五万算作五万，所以一共 3 张五万
	if got := h.Count(MustTile(SuitMan, 5)); got != 3 {
		t.Errorf("Hand.Count(五万) = %d，期望 3（含赤五万）", got)
	}
	if got := h.Count(AkaMan5); got != 3 {
		t.Errorf("Hand.Count(赤五万) = %d，期望 3；Count 按种类，和 Count 的参数是不是赤牌无关", got)
	}
	if got := h.Counts()[4]; got != 3 { // 4 是五万的编号
		t.Errorf("Hand.Counts()[五万] = %d，期望 3", got)
	}

	// Remove 是精确匹配：删赤五万不会动到普通五万
	if err := h.Remove(AkaMan5); err != nil {
		t.Fatalf("Remove(赤五万): %v", err)
	}
	if got := h.CountAka(); got != 0 {
		t.Errorf("删掉赤五万后 CountAka = %d，期望 0", got)
	}
	if got := h.Count(MustTile(SuitMan, 5)); got != 2 {
		t.Errorf("删掉赤五万后还剩 %d 张五万，期望 2", got)
	}
	if err := h.Remove(AkaMan5); err != ErrTileNotFound {
		t.Errorf("再删一次赤五万应返回 ErrTileNotFound，得到 %v", err)
	}
}

// 赤五万必须和普通五万拆出完全一样的牌型——这是「赤属性不参与牌型」的直接检验。
func TestAkaCountsAsItsKindForShape(t *testing.T) {
	pairs := []struct{ normal, aka string }{
		{"456m123m789m123s11p", "406m123m789m123s11p"}, // 顺子里的五万
		{"555m456s789p11z", "550m456s789p11z"},         // 刻子里的五万
		{"456m123s789s123p22p", "406m123s789s123p22p"}, // 门清嵌张
	}

	for _, p := range pairs {
		normal, aka := MustParseTiles(p.normal), MustParseTiles(p.aka)

		cn, err := CountTiles(normal)
		if err != nil {
			t.Fatalf("CountTiles(%q): %v", p.normal, err)
		}
		ca, err := CountTiles(aka)
		if err != nil {
			t.Fatalf("CountTiles(%q): %v", p.aka, err)
		}
		if cn != ca {
			t.Errorf("%q 与 %q 的 Counts 不同：\n%v\n%v", p.normal, p.aka, cn, ca)
		}

		dn, err := Decompose(normal)
		if err != nil {
			t.Fatalf("Decompose(%q): %v", p.normal, err)
		}
		da, err := Decompose(aka)
		if err != nil {
			t.Fatalf("Decompose(%q): %v", p.aka, err)
		}
		if len(dn) != len(da) {
			t.Errorf("%q 有 %d 种拆法，%q 有 %d 种", p.normal, len(dn), p.aka, len(da))
		}
		if CanWin(normal) != CanWin(aka) {
			t.Errorf("%q 与 %q 的和牌判定不一致", p.normal, p.aka)
		}
	}
}

// 静默陷阱的回归测试。
//
// ctx.WinningTile 不经过 Counts，如果它没被归一，waitFu 拿赤五万去和面子的牌
// 做相等比较会一张都比不中，嵌张的 +2 直接丢掉。30 符和 40 符点数不一样，
// 所以这条测试能真的抓到这个 bug——不像大多数 +2 符的错误会被取整吃掉。
func TestCalcFuWithAkaWinningTileEmbedWait(t *testing.T) {
	tiles := MustParseTiles("46m0m123s789s123p22p") // 和牌张是赤五万，嵌张 4-6 万
	decs, err := Decompose(tiles)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if len(decs) != 1 {
		t.Fatalf("期望恰好 1 种拆法，得到 %d 种", len(decs))
	}

	// 门清荣和：副底 20 + 嵌张 2 + 门清荣和 10 = 32 → 40
	ctx := YakuContext{WinningTile: AkaMan5, IsMenzen: true}
	if got := CalcFu(decs[0], ctx); got != 40 {
		t.Errorf("门清荣和嵌张（和牌张赤五万）符数 = %d，期望 40；"+
			"得到 30 说明 ctx.WinningTile 没被归一，嵌张的 +2 丢了", got)
	}

	// 同一手牌、和牌张换成普通五万，结果必须一模一样
	plain := MustParseTiles("46m5m123s789s123p22p")
	pdecs, err := Decompose(plain)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	ctx.WinningTile = MustTile(SuitMan, 5)
	if got, want := CalcFu(decs[0], ctx), CalcFu(pdecs[0], ctx); got != want {
		t.Errorf("和牌张为赤五万时 %d 符，为普通五万时 %d 符，应当相同", got, want)
	}
}

// 4 张普通五万 + 1 张赤五万 = 同种 5 张。归一漏掉的话这里会放过。
func TestCountTilesRejectsFifthFiveAcrossAka(t *testing.T) {
	for _, s := range []string{"55550m", "5555m0m", "0m5555m"} {
		if _, err := CountTiles(MustParseTiles(s)); err == nil {
			t.Errorf("CountTiles(%q) 应报错：同种超过 4 张", s)
		}
	}
	for _, s := range []string{"555m0m", "550m5m"} {
		if _, err := CountTiles(MustParseTiles(s)); err != nil {
			t.Errorf("CountTiles(%q) 应成功（共 4 张）: %v", s, err)
		}
	}
}

// AllTiles 必须把副露也算进去——漏掉的话，有人鸣牌时宝牌就会少数。
func TestAllTilesIncludesMelds(t *testing.T) {
	// 暗牌 8 张：456m（其中五万是赤牌）+ 789m + 东东
	concealed := MustParseTiles("406m789m11z")
	meld := Meld{Kind: MeldRun, IsOpen: true, Tiles: [4]Tile{
		MustTile(SuitPin, 1), MustTile(SuitPin, 2), MustTile(SuitPin, 3),
	}}
	all := AllTiles(concealed, []Meld{meld})

	if len(all) != 11 {
		t.Fatalf("AllTiles 返回 %d 张，期望 11 张（暗牌 8 + 副露 3）", len(all))
	}
	// 副露里的宝牌同样计数
	if got := CountDora(all, []Tile{MustTile(SuitPin, 9)}); got != 1 { // 九筒指示一筒
		t.Errorf("副露里的一筒没被数成宝牌：CountDora = %d，期望 1", got)
	}
	// 赤五万：既是赤宝牌，又是「四万指示五万」下的普通宝牌，两个 +1 都要给
	if got := CountDora(all, []Tile{MustTile(SuitMan, 4)}); got != 1 {
		t.Errorf("赤五万应算一张普通宝牌：CountDora = %d，期望 1", got)
	}
	if got := CountAka(all); got != 1 {
		t.Errorf("CountAka = %d，期望 1", got)
	}
}

// 这条锁住 AllTiles 为什么不收 Decomposition：
// Decomposition 是从 Counts 建出来的（CountTiles 已把赤牌归一），所以
// `Decomposition.Tiles()` 里永远数不到赤宝牌。用它的签名会让人写出
// 「赤宝牌翻数恒为 0」且测试还过得去的代码。
func TestDecompositionLosesAka(t *testing.T) {
	decs, err := Decompose(MustParseTiles("406m789m11z"))
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if got := CountAka(decs[0].Tiles()); got != 0 {
		t.Errorf("Decomposition.Tiles() 里出现了 %d 张赤宝牌，期望 0；"+
			"如果这里变成非 0，说明拆法开始保留赤属性，AllTiles 的签名可以重新考虑", got)
	}
}

// 紧凑记法必须能原样往返——牌山入库、协议传输、人工核对全靠它。
func TestAkaCompactNotationRoundTrip(t *testing.T) {
	for _, s := range []string{
		"0m", "0s", "0p", "05m", "055m", "0m0s0p", "123m0p11z", "406m123s789p11z",
	} {
		tiles, err := ParseTiles(s)
		if err != nil {
			t.Fatalf("ParseTiles(%q): %v", s, err)
		}
		if got := FormatTiles(tiles); got != s {
			t.Errorf("ParseTiles(%q) → FormatTiles = %q", s, got)
		}
	}
}

// 牌山序列同样要能原样读回，赤宝牌不能在这一步丢掉。
func TestWallSequenceRoundTripKeepsAka(t *testing.T) {
	w := NewWallFromSeed(testSeed)
	seq := w.Sequence()

	back, err := ParseTiles(seq)
	if err != nil {
		t.Fatalf("ParseTiles(Wall.Sequence()): %v", err)
	}

	orig := w.Tiles()
	if len(back) != len(orig) {
		t.Fatalf("还原出 %d 张，原文 %d 张", len(back), len(orig))
	}
	for i, tile := range orig {
		if back[i] != tile {
			t.Fatalf("第 %d 张还原成 %s，原文是 %s", i, back[i], tile)
		}
	}
	if got := CountAka(back); got != 3 {
		t.Errorf("还原后赤宝牌有 %d 张，期望 3 张", got)
	}
}

// 赤五万在牌山里的位置必须影响指纹——否则服务器可以在公布承诺之后才决定赤牌发给谁。
func TestWallFingerprintBindsAka(t *testing.T) {
	w := NewWallFromSeed(testSeed)
	tiles := w.Tiles()

	// 把第一张赤五万换成等价的普通五万：牌型意义完全相同，指纹必须不同
	idx := -1
	for i, tile := range tiles {
		if tile.IsAka() {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("牌山里没有赤宝牌，无法验证指纹是否绑定赤牌位置")
	}

	tampered := append([]Tile(nil), tiles...)
	tampered[idx] = tampered[idx].Kind()

	if string(w.Bytes()) == string(mustBytes(tampered)) {
		t.Error("把赤五万换成普通五万后牌山字节不变——指纹绑不住赤牌位置")
	}
}

func mustBytes(tiles []Tile) []byte {
	out := make([]byte, len(tiles))
	for i, tile := range tiles {
		out[i] = byte(tile)
	}
	return out
}
