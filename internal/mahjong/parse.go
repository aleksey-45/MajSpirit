package mahjong

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// ParseTiles 解析紧凑记法表示的牌组，例如 "123m456s789p11z"。
//
// m/s/p/z 分别表示万/条/饼/字牌，字母前的数字是点数，空白字符会被忽略。
// 同一个花色字母可以重复出现，解析结果保持书写顺序（不排序）。
//
// 本函数只负责语法，不校验张数上限："11111m" 会成功返回 5 张一万，
// 同种超过 4 张由 CountTiles 负责——否则就没有合法的输入能用来测它。
func ParseTiles(s string) ([]Tile, error) {
	var (
		res     []Tile
		pending []int
	)

	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			// 忽略空白

		case isCharNumber(r):
			pending = append(pending, int(r-'0'))

		case isCharSuit(r):
			if len(pending) == 0 {
				return nil, fmt.Errorf("mahjong: 解析 %q 失败: 花色 %q 前没有点数", s, r)
			}
			suit := suitOfChar(r)
			for _, rank := range pending {
				tile, err := NewTile(suit, rank)
				if err != nil {
					return nil, fmt.Errorf("mahjong: 解析 %q 失败: %w", s, err)
				}
				res = append(res, tile)
			}
			pending = pending[:0]

		default:
			return nil, fmt.Errorf("mahjong: 解析 %q 失败: 非法字符 %q", s, r)
		}
	}

	// 收尾：结尾挂着数字却没有花色字母，是语法错误
	if len(pending) > 0 {
		return nil, fmt.Errorf("mahjong: 解析 %q 失败: 结尾有未指定花色的点数", s)
	}
	return res, nil
}

// MustParseTiles 与 ParseTiles 相同，但解析失败时 panic。
// 仅用于测试与写死的常量，不可用于来自网络或用户输入的字符串。
func MustParseTiles(s string) []Tile {
	tiles, err := ParseTiles(s)
	if err != nil {
		panic(err)
	}
	return tiles
}

// FormatTiles 把牌组格式化为紧凑记法，与 ParseTiles 互为逆运算。
// 输出按万、条、饼、字牌分组，组内按点数升序，例如 "123m456s789p11z"。
// 非法牌会被跳过：本函数同时用于日志与调试输出，不该因为一张坏牌就 panic。
func FormatTiles(tiles []Tile) string {
	var b strings.Builder

	for _, suit := range []Suit{SuitMan, SuitSou, SuitPin, SuitHonor} {
		group := make([]Tile, 0, len(tiles))
		for _, t := range tiles {
			if t.Valid() && t.Suit() == suit {
				group = append(group, t)
			}
		}
		if len(group) == 0 {
			continue
		}
		slices.Sort(group)
		for _, t := range group {
			b.WriteByte(byte('0' + t.Rank()))
		}
		b.WriteByte(suitChar(suit))
	}
	return b.String()
}

func isCharSuit(r rune) bool {
	return r == 'm' || r == 's' || r == 'p' || r == 'z'
}

func isCharNumber(r rune) bool {
	return r >= '0' && r <= '9'
}

// suitOfChar 把记法中的花色字母转成 Suit。调用前须先用 isCharSuit 确认。
func suitOfChar(r rune) Suit {
	switch r {
	case 'm':
		return SuitMan
	case 's':
		return SuitSou
	case 'p':
		return SuitPin
	default:
		return SuitHonor
	}
}

// suitChar 是 suitOfChar 的逆。
func suitChar(s Suit) byte {
	switch s {
	case SuitMan:
		return 'm'
	case SuitSou:
		return 's'
	case SuitPin:
		return 'p'
	default:
		return 'z'
	}
}
