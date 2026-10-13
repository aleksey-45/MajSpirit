package mahjong

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// 王牌是牌山末尾**永远不按常规摸走**的 14 张，由三段组成：
//
//	岭上 4 张     杠之后从这里补摸
//	宝牌指示 5 张  开局翻 1 张，每杠多翻 1 张，最多 5 张
//	里宝指示 5 张  只有立直且和牌时才揭示
//
// 4+5+5=14。这三段的**位置**是规则，不是实现细节：宝牌指示牌从哪一张开始翻、
// 杠一次往后挪一张，验牌的人就是照着这条复算的。
const (
	DeadWallSize  = 14
	rinshanSize   = 4
	indicatorSize = 5
)

// Wall 是牌山。可摸的那一段与王牌分开存，但 `Bytes`/`Sequence` 仍按**原始摸牌顺序**
// 拼回 136 张 —— 承诺值必须覆盖整副牌（含王牌、含赤五落在哪一张）。只算可摸的那
// 一部分，就给了服务端事后摆布王牌的余地。
type Wall struct {
	live    []Tile // 可摸的牌山
	rinshan []Tile // 岭上牌，固定 4 张
	dora    []Tile // 宝牌指示牌，固定 5 张
	ura     []Tile // 里宝指示牌，固定 5 张

	kans int // 已摸走的岭上牌张数 = 已翻开的**额外**宝牌指示牌数
}

// newWall 把一副洗好的 136 张切成"可摸"与"王牌"两段。
func newWall(tiles []Tile) *Wall {
	n := len(tiles) - DeadWallSize
	return &Wall{
		live:    tiles[:n],
		rinshan: tiles[n : n+rinshanSize],
		dora:    tiles[n+rinshanSize : n+rinshanSize+indicatorSize],
		ura:     tiles[n+rinshanSize+indicatorSize:],
	}
}

// NewWallFromSeed 用确定性种子构造牌山。
// 同一个种子必须永远产出同一副牌山——这是牌局可验证性的前提。
// 种子要用满全部 32 字节：只取其中一部分会让候选空间小到能被穷举，
// 玩家拿到承诺值就能在开赛前算出整副牌。
func NewWallFromSeed(seed [32]byte) *Wall {
	r := rand.New(rand.NewChaCha8(seed))
	tiles := FullSet()
	Shuffle(tiles, r)
	return newWall(tiles)
}

// NewWall 用注入的随机源构造牌山，供测试与不需要留存的场合使用。
// 需要存档的牌局一律走 NewWallFromSeed。
func NewWall(r *rand.Rand) *Wall {
	tiles := FullSet()
	Shuffle(tiles, r)
	return newWall(tiles)
}

// Shuffle 用 r 的确定性字节流就地重排 tiles。
// 算法固定为降序的 Fisher-Yates，刻意不用标准库的 shuffle——它从字节流映射到
// 下标的方式属于实现细节，Go 不保证跨版本不变，而验证要求多年后仍能重放。
// 取模带来的轻微分布偏差对不发奖的牌局可以接受。
func Shuffle(tiles []Tile, r *rand.Rand) {
	for i := len(tiles) - 1; i > 0; i-- {
		j := int(r.Uint64() % uint64(i+1))
		tiles[i], tiles[j] = tiles[j], tiles[i]
	}
}

// Remaining 返回**可摸**的张数，不含王牌。
func (w *Wall) Remaining() int { return len(w.live) }

// Empty 报告可摸的牌山是否已摸空。
func (w *Wall) Empty() bool { return w.Remaining() == 0 }

// Draw 摸一张牌，牌山已空时 ok 为 false。
// 摸空是正常业务分支（该小局流局结束），不是异常。
func (w *Wall) Draw() (Tile, bool) {
	if w.Empty() {
		return 0, false
	}
	t := w.live[0]
	w.live = w.live[1:] // 只前移切片头，不复制底层数组
	return t, true
}

// DrawRinshan 摸一张岭上牌（杠之后），并把牌山末尾的一张补进王牌。
//
// 补牌这一步是规则的一部分，不是实现细节：**杠会让可摸张数净减 1**。
// 少了它，四个杠就能凭空多摸四张，验牌时那四张的来源对不上。
func (w *Wall) DrawRinshan() (Tile, bool) {
	if len(w.rinshan) == 0 {
		return 0, false
	}
	t := w.rinshan[0]
	w.rinshan = w.rinshan[1:]
	w.kans++

	if n := len(w.live); n > 0 {
		w.rinshan = append(w.rinshan, w.live[n-1])
		w.live = w.live[:n-1]
	}
	return t, true
}

// Kans 返回已经摸走的岭上牌张数。
func (w *Wall) Kans() int { return w.kans }

// DoraIndicators 返回当前已翻开的宝牌指示牌：开局 1 张，每杠多 1 张，最多 5 张。
func (w *Wall) DoraIndicators() []Tile {
	return w.dora[:min(1+w.kans, len(w.dora))]
}

// UraIndicators 返回里宝指示牌，张数与宝牌指示牌同步。
//
// 它**只该在立直且和牌时**交给规则层 —— 提前发给客户端等于把整副牌的走向漏出去。
func (w *Wall) UraIndicators() []Tile {
	return w.ura[:min(1+w.kans, len(w.ura))]
}

// all 返回**整副牌**的当前顺序：可摸的那一段在前，王牌三段依次接在后面。
//
// 刚构造出来时它就是洗好的原始摸牌顺序 —— 承诺值、指纹、存档都按这个口径。
// 摸过牌之后它只剩没摸走的部分，所以它只适合"刚洗好就哈希"这个用法。
func (w *Wall) all() []Tile {
	out := make([]Tile, 0, len(w.live)+DeadWallSize)
	out = append(out, w.live...)
	out = append(out, w.rinshan...)
	out = append(out, w.dora...)
	out = append(out, w.ura...)
	return out
}

// Tiles 返回完整摸牌顺序的副本（含王牌）。
func (w *Wall) Tiles() []Tile { return w.all() }

// Bytes 返回原始摸牌顺序的字节，供哈希使用。
// 不要用 FormatTiles 代替：那个按花色分组排序，会丢掉顺序，
// 导致所有同花色组合的牌山共用同一个指纹。
//
// 赤宝牌的写码（34/35/36）与普通牌不同，所以赤五落在哪一张上也被算进指纹。
// 这一条不能丢：否则服务器可以在公布承诺值之后才决定赤五发给谁。
//
// **王牌也算进去**：14 张王牌的摆放决定了杠宝牌与里宝，不算就等于允许服务端
// 事后再挑。
func (w *Wall) Bytes() []byte {
	all := w.all()
	out := make([]byte, len(all))
	for i, t := range all {
		out[i] = byte(t)
	}
	return out
}

// Sequence 返回按摸牌顺序逐张写成的紧凑记法，供人工核对与入库。
//
// 例如 "1m5p3s2z"，共 136 组（含王牌）。赤宝牌写作 `0`。输出可被 ParseTiles 原样读回。
func (w *Wall) Sequence() string {
	all := w.all()
	var b strings.Builder
	b.Grow(len(all) * 2)
	for _, t := range all {
		if t.IsAka() {
			b.WriteByte('0')
		} else {
			b.WriteByte(byte('0' + t.Rank()))
		}
		b.WriteByte(suitChar(t.Suit()))
	}
	return b.String()
}

// NewWallFromTiles 用**给定的摸牌顺序**构造牌山。
//
// 要求 136 张、每种恰好 4 张（比"不超过 4 张"更严：这是一副完整的牌）。
//
// 两个用途：
//
//   - 测试要摆一副**确定**的牌。搜一个能开出目标手牌的种子也行，但那种测试会
//     随着洗牌算法的任何改动一起碎，而且碎的时候看不出是"摆牌变了"还是"逻辑坏了"。
//   - 从存档的摸牌顺序复原牌局（`Wall.Sequence()` 存进库的就是它）。
func NewWallFromTiles(tiles []Tile) (*Wall, error) {
	if len(tiles) != NumTiles {
		return nil, fmt.Errorf("mahjong: 牌山需要 %d 张，实际 %d 张", NumTiles, len(tiles))
	}
	var counts Counts
	for _, t := range tiles {
		if !t.Valid() {
			return nil, fmt.Errorf("mahjong: 牌山里出现非法牌 %d", t)
		}
		counts[t.Kind()]++
	}
	for k, c := range counts {
		if c != numCopies {
			return nil, fmt.Errorf("mahjong: %s 有 %d 张，一副牌里应有 %d 张",
				Tile(k), c, numCopies)
		}
	}
	return newWall(append([]Tile(nil), tiles...)), nil
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
