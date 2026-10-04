package mahjong

import (
	"errors"
	"sort"
)

var (
	ErrTileNotFound = errors.New("tile not found")
	ErrHandFull     = errors.New("hand is full")
)

type Hand struct {
	tiles []Tile
}

func NewHand(tiles []Tile) *Hand { // 拷贝入参
	return &Hand{
		tiles: append([]Tile(nil), tiles...),
	}
}

func (h *Hand) Add(t Tile) error {
	if !t.Valid() {
		return errors.New("invalid tile")
	}
	h.tiles = append(h.tiles, t)
	return nil
}

// Remove 删除**指定的那一张**。失败返回可被 errors.Is 匹配的哨兵错误。
//
// 这里是**精确匹配**，不走 Kind：赤五万与普通五万在牌面上看得出来，
// 玩家出牌时必须能指定要打哪一张，所以 Remove(赤五万) 不会动到普通五万。
func (h *Hand) Remove(t Tile) error {
	for i, tile := range h.tiles {
		if tile == t {
			h.tiles = append(h.tiles[:i], h.tiles[i+1:]...)
			return nil
		}
	}
	return ErrTileNotFound
}
func (h *Hand) Len() int {
	return len(h.tiles)
}

// Count 统计某种牌的数量。**按种类**：手里有赤五万时 Count(五万) 返回 1。
// 规则判定要的就是这个口径；想单独数赤宝牌用 CountAka。
func (h *Hand) Count(t Tile) int {
	k := t.Kind()
	count := 0
	for _, tile := range h.tiles {
		if tile.Kind() == k {
			count++
		}
	}
	return count
}

// Tiles 返回排序后的副本。按**种类**升序，同种类时赤牌在前——
// 和 FormatTiles 的顺序一致，便于测试里直接比对字符串。
func (h *Hand) Tiles() []Tile {
	tiles := append([]Tile(nil), h.tiles...)
	sort.Slice(tiles, func(i, j int) bool {
		if ki, kj := tiles[i].Kind(), tiles[j].Kind(); ki != kj {
			return ki < kj
		}
		return tiles[i] > tiles[j]
	})
	return tiles
}

// Counts 统计每种牌的数量，**按种类**（赤五万算作五万）。
// 与 CountTiles 的口径必须一致：两处都喂给 Decompose。
func (h *Hand) Counts() Counts {
	counts := Counts{}
	for _, tile := range h.tiles {
		counts[tile.Kind()]++
	}
	return counts
}

// CountAka 返回手里的赤宝牌张数。
func (h *Hand) CountAka() int { return CountAka(h.tiles) }

func (h *Hand) CanWin() bool {
	return CanWin(h.Tiles())
}
func (h *Hand) Clone() *Hand {
	return &Hand{
		tiles: append([]Tile(nil), h.tiles...),
	}
}
