package mahjong

import (
	"errors"
	"slices"
	"testing"
)

// Hand 的三处拷贝语义（NewHand 拷入参、Tiles() 返回副本、Clone() 深拷贝）是本文件
// 的重点。它们出错的表现都是**静默的**：房间层做快照时改了副本，原手牌跟着变，
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

// Remove **只删一张**：手上有三张一万，打出一张后还剩两张。
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

// Remove 失败时要不报错地…不，要报错、可被 errors.Is 匹配，而且**不改动手牌**。
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

// Tiles() 返回**排序后的副本**——排序是契约的一部分（界面与日志都依赖它）。
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

func TestHandCount(t *testing.T) {
	h := NewHand(MustParseTiles("111m22s3z"))

	tests := []struct {
		tile Tile
		want int
	}{
		{MustTile(SuitMan, 1), 3},
		{MustTile(SuitMan, 2), 0},
		{MustTile(SuitSou, 2), 2},
		{MustTile(SuitHonor, HonorWest), 1},
		{MustTile(SuitHonor, HonorRed), 0},
	}
	for _, tt := range tests {
		if got := h.Count(tt.tile); got != tt.want {
			t.Errorf("Count(%v) = %d，期望 %d", tt.tile, got, tt.want)
		}
	}
}

func TestHandCounts(t *testing.T) {
	h := NewHand(MustParseTiles("123m456s789p11z"))

	counts := h.Counts()
	tests := []struct {
		tile Tile
		want int
	}{
		{MustTile(SuitMan, 1), 1},
		{MustTile(SuitSou, 4), 1},
		{MustTile(SuitPin, 9), 1},
		{MustTile(SuitHonor, HonorEast), 2},
		{MustTile(SuitMan, 9), 0},
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

// Counts() 与 Count() 是同一个事实的两个入口，不允许出现分歧。
func TestHandCountsAgreesWithCount(t *testing.T) {
	h := NewHand(MustParseTiles("11123m456s77z"))
	counts := h.Counts()

	for kind := Tile(0); kind < NumTileKinds; kind++ {
		if counts[kind] != h.Count(kind) {
			t.Errorf("牌 %v：Counts()[%d] = %d，Count() = %d",
				kind, kind, counts[kind], h.Count(kind))
		}
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

// Hand.CanWin 是包级 CanWin 的便利封装，两者必须一致。
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
			tiles := MustParseTiles(tt.hand)
			h := NewHand(tiles)

			if got := h.CanWin(); got != tt.want {
				t.Errorf("Hand.CanWin() = %v，期望 %v", got, tt.want)
			}
			if got := CanWin(tiles); got != tt.want {
				t.Errorf("CanWin() = %v，期望 %v", got, tt.want)
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
