package mahjong

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// Wall 是牌山：一副洗好的 136 张牌，按摸牌顺序排列。
type Wall struct {
	tiles []Tile
}

// NewWallFromSeed 用确定性种子构造牌山。
//
// 同一个种子必须永远产出同一副牌山——这是牌局可验证性的前提。
// 种子要用满全部 32 字节：只取其中一部分会让候选空间小到能被穷举，
// 玩家拿到承诺值就能在开赛前算出整副牌。
func NewWallFromSeed(seed [32]byte) *Wall {
	r := rand.New(rand.NewChaCha8(seed))
	tiles := FullSet()
	Shuffle(tiles, r)
	return &Wall{tiles: tiles}
}

// NewWall 用注入的随机源构造牌山，供测试与不需要留存的场合使用。
// 需要存档的牌局一律走 NewWallFromSeed。
func NewWall(r *rand.Rand) *Wall {
	tiles := FullSet()
	Shuffle(tiles, r)
	return &Wall{tiles: tiles}
}

// Shuffle 用 r 的确定性字节流就地重排 tiles。
//
// 算法固定为降序的 Fisher-Yates，刻意不用标准库的 shuffle——它从字节流映射到
// 下标的方式属于实现细节，Go 不保证跨版本不变，而验证要求多年后仍能重放。
// 取模带来的轻微分布偏差对不发奖的牌局可以接受。
func Shuffle(tiles []Tile, r *rand.Rand) {
	for i := len(tiles) - 1; i > 0; i-- {
		j := int(r.Uint64() % uint64(i+1))
		tiles[i], tiles[j] = tiles[j], tiles[i]
	}
}

// Remaining 返回尚未摸走的张数。
func (w *Wall) Remaining() int { return len(w.tiles) }

// Empty 报告牌山是否已摸空。
func (w *Wall) Empty() bool { return w.Remaining() == 0 }

// Draw 摸一张牌，牌山已空时 ok 为 false。
//
// 摸空是正常业务分支（该小局流局结束），不是异常，所以不 panic。
func (w *Wall) Draw() (Tile, bool) {
	if w.Empty() {
		return 0, false
	}
	t := w.tiles[0]
	w.tiles = w.tiles[1:] // 只前移切片头，不复制底层数组
	return t, true
}

// Tiles 返回完整摸牌顺序的副本。
func (w *Wall) Tiles() []Tile {
	out := make([]Tile, len(w.tiles))
	copy(out, w.tiles)
	return out
}

// Bytes 返回 136 字节的原始摸牌顺序，供哈希使用。
//
// 不要用 FormatTiles 代替：那个按花色分组排序，会丢掉顺序，
// 导致所有同花色组合的牌山共用同一个指纹。
func (w *Wall) Bytes() []byte {
	out := make([]byte, len(w.tiles))
	for i, t := range w.tiles {
		out[i] = byte(t)
	}
	return out
}

// Sequence 返回按摸牌顺序逐张写成的紧凑记法，供人工核对与入库。
//
// 例如 "1m5p3s2z"，共 136 组。输出可被 ParseTiles 原样读回。
func (w *Wall) Sequence() string {
	var b strings.Builder
	b.Grow(len(w.tiles) * 2)
	for _, t := range w.tiles {
		b.WriteByte(byte('0' + t.Rank()))
		b.WriteByte(suitChar(t.Suit()))
	}
	return b.String()
}

// Deal 给四家各发 13 张初始手牌，返回的索引即座位号。
//
// 庄家的第 14 张不在这里发——它作为庄家的第一次摸牌由房间层调用 Draw 取得。
// 这样 Deal 不必知道庄家是谁，庄家轮换（第 N 小局的庄家为玩家 N）交给房间层。
//
// 发牌后牌山剩余 136 - 13*4 = 84 张。
func (w *Wall) Deal() ([4][]Tile, error) {
	const need = HandSize * 4
	if w.Remaining() < need {
		return [4][]Tile{}, fmt.Errorf(
			"mahjong: 牌山余牌不足，需要 %d 张，实际 %d 张", need, w.Remaining())
	}

	var hands [4][]Tile
	for seat := range 4 {
		hands[seat] = make([]Tile, HandSize)
		for j := range HandSize {
			t, ok := w.Draw()
			if !ok {
				// 前置检查已保证够发，这里只是防御
				return [4][]Tile{}, fmt.Errorf("mahjong: 牌山意外摸空")
			}
			hands[seat][j] = t
		}
	}
	return hands, nil
}
