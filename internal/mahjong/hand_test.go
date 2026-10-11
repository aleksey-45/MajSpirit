package mahjong

import (
	"errors"
	"slices"
	"testing"
)

// Hand 的三处拷贝语义（NewHand 拷入参、Tiles() 返回副本、Clone() 深拷贝）是本文件
// 的重点。它们出错的表现都是静默的：房间层做快照时改了副本，原手牌跟着变，
// 而且两处代码各自看都没问题。

func TestNewHandCopiesInput(t *testing.T) {
	src := MustParseTiles("123m")
	h := NewHand(src)

	src[0] = MustTile(SuitHonor, HonorRed) // 改源切片

	got := h.Tiles()
	want := MustParseTiles("123m")
	if !slices.Equal(got, want) {
		t.Errorf("改源切片影响了手牌：%v，期望 %v", got, want)
	}
}

func TestHandAddAndLen(t *testing.T) {
	h := NewHand(MustParseTiles("123m"))
	if h.Len() != 3 {
		t.Fatalf("Len() = %d，期望 3", h.Len())
	}

	h.Add(MustTile(SuitMan, 4))
	if h.Len() != 4 {
		t.Errorf("Add 之后 Len() = %d，期望 4", h.Len())
	}
	if !slices.Equal(h.Tiles(), MustParseTiles("1234m")) {
		t.Errorf("Add 之后手牌 = %v，期望 1234m", h.Tiles())
	}
}

// Add 拒绝越界的牌，而且**不能改动手牌**。
//
// 这个分支不是防御性摆设：Counts 是定长数组 [NumTileKinds]int，
// 一张越界的牌进了手牌之后，Counts() 会在 counts[tile.Kind()] 上直接越界 panic，
// 而且 panic 的地点离肇事地点很远（Add 在 hand.go，炸在 win.go）。
func TestHandAddRejectsInvalidTile(t *testing.T) {
	h := NewHand(MustParseTiles("123m"))

	if err := h.Add(Tile(200)); err == nil {
		t.Fatal("Add 应该拒绝越界的牌")
	}
	if h.Len() != 3 {
		t.Errorf("拒绝之后手牌不该变：Len() = %d，期望 3", h.Len())
	}
}

// Remove 只删一张：手上有三张一万，打出一张后还剩两张。
func TestHandRemoveDeletesOnlyOneCopy(t *testing.T) {
	h := NewHand(MustParseTiles("111m234m"))

	if err := h.Remove(MustTile(SuitMan, 1)); err != nil {
		t.Fatalf("Remove 报错：%v", err)
	}
	if n := h.Count(MustTile(SuitMan, 1)); n != 2 {
		t.Errorf("打出一张后应剩 2 张一万，实际 %d 张", n)
	}
	if h.Len() != 5 {
		t.Errorf("Len() = %d，期望 5", h.Len())
	}
}

// Remove 失败时要不报错地…不，要报错、可被 errors.Is 匹配，而且不改动手牌。
func TestHandRemoveMissingTile(t *testing.T) {
	h := NewHand(MustParseTiles("123m"))
	before := h.Tiles()

	err := h.Remove(MustTile(SuitHonor, HonorRed))
	if err == nil {
		t.Fatal("删不存在的牌应报错")
	}
	if !errors.Is(err, ErrTileNotFound) {
		t.Errorf("错误应能被 errors.Is(err, ErrTileNotFound) 匹配，实际 %v", err)
	}
	if after := h.Tiles(); !slices.Equal(after, before) {
		t.Errorf("删除失败却改动了手牌：%v → %v", before, after)
	}
}

// Tiles() 返回排序后的副本——排序是契约的一部分（界面与日志都依赖它）。
func TestHandTilesSortedCopy(t *testing.T) {
	h := NewHand(MustParseTiles("321m"))

	got := h.Tiles()
	want := MustParseTiles("123m")
	if !slices.Equal(got, want) {
		t.Errorf("Tiles() = %v，期望排序后的 %v", got, want)
	}

	// 两件事都要成立：不能改写手牌，也不能把同一个切片交出去
	got[0] = MustTile(SuitHonor, HonorRed)
	if again := h.Tiles(); !slices.Equal(again, want) {
		t.Errorf("改返回值影响了手牌：%v，期望 %v", again, want)
	}
	if h.Len() != 3 {
		t.Errorf("Len() = %d，期望 3", h.Len())
	}
}

// 同种类时赤牌排前面。
//
// 这条是契约的一部分，不是审美：FormatTiles 用同一套顺序（赤五万写成 "0"，
// 所以赤五万 + 五万 + 七万 = "057m"），而测试与日志经常直接比字符串。
// 两边顺序一旦分叉，"拿字符串比对"这条路就悄悄断了。
func TestHandTilesPutsAkaFirstWithinKind(t *testing.T) {
	h := NewHand([]Tile{
		MustTile(SuitMan, 5), // 普通五万
		MustTile(SuitMan, 0), // 赤五万
		MustTile(SuitMan, 7),
	})

	got := h.Tiles()
	if !got[0].IsAka() {
		t.Errorf("同种类的赤牌应排在最前，实际 %v", FormatTiles(got))
	}
	if s := FormatTiles(got); s != "057m" {
		t.Errorf("FormatTiles(Tiles()) = %q，期望 %q —— 两边顺序分叉了", s, "057m")
	}
}

// Count 按种类数（赤五万算五万），命中、未命中、多数三种情形各一行。
func TestHandCount(t *testing.T) {
	h := NewHand(MustParseTiles("111m22s3z"))

	tests := []struct {
		tile Tile
		want int
	}{
		{MustTile(SuitMan, 1), 3},
		{MustTile(SuitSou, 2), 2},
		{MustTile(SuitHonor, HonorWest), 1},
		{MustTile(SuitMan, 2), 0}, // 不在手里
	}
	for _, tt := range tests {
		if got := h.Count(tt.tile); got != tt.want {
			t.Errorf("Count(%v) = %d，期望 %d", tt.tile, got, tt.want)
		}
	}
}

// Counts 是直方图。同上，每种计数取值各留一行，剩下的交给最后那个总和断言——
// 它对着 Len() 而不是对着表里的数字，是一条独立的不变量。
func TestHandCounts(t *testing.T) {
	h := NewHand(MustParseTiles("123m456s789p11z"))

	counts := h.Counts()
	tests := []struct {
		tile Tile
		want int
	}{
		{MustTile(SuitMan, 1), 1},
		{MustTile(SuitHonor, HonorEast), 2},
		{MustTile(SuitMan, 9), 0}, // 不在手里
	}
	for _, tt := range tests {
		if got := counts[tt.tile]; got != tt.want {
			t.Errorf("counts[%v] = %d，期望 %d", tt.tile, got, tt.want)
		}
	}

	total := 0
	for _, c := range counts {
		total += c
	}
	if total != h.Len() {
		t.Errorf("直方图总和 %d 与 Len() %d 不符", total, h.Len())
	}
}

func TestHandCloneIsIndependent(t *testing.T) {
	h := NewHand(MustParseTiles("123m"))
	clone := h.Clone()

	if !slices.Equal(clone.Tiles(), h.Tiles()) {
		t.Fatalf("Clone 的内容应与原手牌相同")
	}

	clone.Add(MustTile(SuitMan, 9))
	clone.Remove(MustTile(SuitMan, 1))

	if h.Len() != 3 {
		t.Errorf("改 clone 影响了原手牌：Len() = %d，期望 3", h.Len())
	}
	if !slices.Equal(h.Tiles(), MustParseTiles("123m")) {
		t.Errorf("改 clone 影响了原手牌内容：%v", h.Tiles())
	}
}

// Hand.CanWin 是包级 CanWin 的便利封装，这里只验封装本身；
// 包级 CanWin 的规则判定归 win_test.go 的 TestCanWin，没必要在这边再抄一遍。
func TestHandCanWin(t *testing.T) {
	tests := []struct {
		hand string
		want bool
	}{
		{"123m456m789m123s11p", true},
		{"1133557799m1133p", true}, // 七对子
		{"123m456m789m123s1p", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.hand, func(t *testing.T) {
			h := NewHand(MustParseTiles(tt.hand))
			if got := h.CanWin(); got != tt.want {
				t.Errorf("Hand.CanWin() = %v，期望 %v", got, tt.want)
			}
		})
	}
}

// 空手牌不能 panic。
func TestHandEmptyIsUsable(t *testing.T) {
	h := NewHand(nil)
	if h.Len() != 0 {
		t.Errorf("Len() = %d，期望 0", h.Len())
	}
	if len(h.Tiles()) != 0 {
		t.Errorf("Tiles() 应为空")
	}
	if h.Count(MustTile(SuitMan, 1)) != 0 {
		t.Errorf("空手牌的 Count 应为 0")
	}
	if h.CanWin() {
		t.Errorf("空手牌不能和")
	}
}
