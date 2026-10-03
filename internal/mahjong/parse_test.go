package mahjong

import (
	"slices"
	"testing"
)

func TestParseTiles(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Tile
	}{
		{"空字符串", "", nil},
		{"单张", "1m", []Tile{MustTile(SuitMan, 1)}},
		{"一段", "123m", []Tile{
			MustTile(SuitMan, 1), MustTile(SuitMan, 2), MustTile(SuitMan, 3),
		}},
		{"多花色", "123m456s789p11z", []Tile{
			MustTile(SuitMan, 1), MustTile(SuitMan, 2), MustTile(SuitMan, 3),
			MustTile(SuitSou, 4), MustTile(SuitSou, 5), MustTile(SuitSou, 6),
			MustTile(SuitPin, 7), MustTile(SuitPin, 8), MustTile(SuitPin, 9),
			MustTile(SuitHonor, HonorEast), MustTile(SuitHonor, HonorEast),
		}},
		{"字牌全部", "1234567z", []Tile{
			MustTile(SuitHonor, HonorEast), MustTile(SuitHonor, HonorSouth),
			MustTile(SuitHonor, HonorWest), MustTile(SuitHonor, HonorNorth),
			MustTile(SuitHonor, HonorWhite), MustTile(SuitHonor, HonorGreen),
			MustTile(SuitHonor, HonorRed),
		}},
		{"含空格", "1m 2m 3m", []Tile{
			MustTile(SuitMan, 1), MustTile(SuitMan, 2), MustTile(SuitMan, 3),
		}},
		{"含换行", "123m\n456s", []Tile{
			MustTile(SuitMan, 1), MustTile(SuitMan, 2), MustTile(SuitMan, 3),
			MustTile(SuitSou, 4), MustTile(SuitSou, 5), MustTile(SuitSou, 6),
		}},
		// 解析必须保序，不能排序：顺序无关性测试需要能构造乱序手牌
		{"保序", "321m", []Tile{
			MustTile(SuitMan, 3), MustTile(SuitMan, 2), MustTile(SuitMan, 1),
		}},
		// 同一个花色字母可以重复出现
		{"重复花色字母", "12m3m", []Tile{
			MustTile(SuitMan, 1), MustTile(SuitMan, 2), MustTile(SuitMan, 3),
		}},
		// 语法合法，语义（同种上限 4 张）由 CountTiles 负责
		{"五张同种", "11111m", []Tile{
			MustTile(SuitMan, 1), MustTile(SuitMan, 1), MustTile(SuitMan, 1),
			MustTile(SuitMan, 1), MustTile(SuitMan, 1),
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTiles(tt.input)
			if err != nil {
				t.Fatalf("ParseTiles(%q) 意外出错: %v", tt.input, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ParseTiles(%q) = %v，期望 %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseTilesRejectsInvalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"点数 0", "10m"},     // 0 不是合法点数
		{"点数 0 单独出现", "0m"}, // 同上
		{"结尾有数字无花色", "123m4"},
		{"花色前没有数字", "m"},
		{"非法字符", "123x"},
		{"非法字符分号", "123m;"},
		{"字牌点数 8 越界", "8z"},
		{"字牌点数 9 越界", "9z"},
		{"全角字符", "１２３ｍ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTiles(tt.input)
			if err == nil {
				t.Fatalf("ParseTiles(%q) 应报错，却返回了 %v", tt.input, got)
			}
			for _, tile := range got {
				if !tile.Valid() {
					t.Fatalf("ParseTiles(%q) 报错前返回了非法牌 Tile(%d)", tt.input, tile)
				}
			}
		})
	}
}

// 解析出的每一张牌都必须合法——这条不变量比逐条用例更能兜住漏洞。
func TestParseTilesNeverReturnsInvalidTile(t *testing.T) {
	inputs := []string{
		"", "1m", "123m456s789p11z", "1234567z", "10m", "0m", "8z", "9z",
		"123m4", "m", "x", "1m2m3m", " 1m \n 2m ",
	}

	for _, in := range inputs {
		got, _ := ParseTiles(in) // 报不报错都要看返回值
		for i, tile := range got {
			if !tile.Valid() {
				t.Errorf("ParseTiles(%q)[%d] = Tile(%d)，不是合法的牌", in, i, tile)
			}
		}
	}
}

func TestFormatTiles(t *testing.T) {
	tests := []struct {
		name  string
		tiles []Tile
		want  string
	}{
		{"空", nil, ""},
		{"单张", []Tile{MustTile(SuitMan, 1)}, "1m"},
		{"一段", []Tile{
			MustTile(SuitMan, 1), MustTile(SuitMan, 2), MustTile(SuitMan, 3),
		}, "123m"},
		{"字牌", []Tile{
			MustTile(SuitHonor, HonorEast), MustTile(SuitHonor, HonorWhite),
		}, "15z"},
		// 输出按万→条→饼→字牌分组，组内按点数升序
		{"乱序输入被分组排序", []Tile{
			MustTile(SuitMan, 3), MustTile(SuitSou, 2), MustTile(SuitMan, 1),
		}, "13m2s"},
		{"多花色", []Tile{
			MustTile(SuitHonor, HonorEast), MustTile(SuitPin, 9),
			MustTile(SuitMan, 1), MustTile(SuitSou, 5),
		}, "1m5s9p1z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatTiles(tt.tiles); got != tt.want {
				t.Errorf("FormatTiles(%v) = %q，期望 %q", tt.tiles, got, tt.want)
			}
		})
	}
}

// 往返：解析结果经过一次格式化后必须稳定。
//
// 注意写的是 ParseTiles(FormatTiles(x)) == x，而不是 FormatTiles(ParseTiles(s)) == s。
// 后者对非规范输入不成立（如 "321m" 会变成 "123m"）。
func TestParseFormatRoundTrip(t *testing.T) {
	inputs := []string{
		"1m",
		"123m",
		"123m456s789p11z",
		"1112345678999m",
		"19m19s19p1234567z",
		"1234567z",
	}

	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			first := MustParseTiles(in)
			got := ParseTilesOrFail(t, FormatTiles(first))
			if !slices.Equal(got, first) {
				t.Errorf("%q 往返后变为 %q：%v != %v",
					in, FormatTiles(got), got, first)
			}
		})
	}
}

// ParseTilesOrFail 是测试内的辅助函数。
func ParseTilesOrFail(t *testing.T, s string) []Tile {
	t.Helper()
	tiles, err := ParseTiles(s)
	if err != nil {
		t.Fatalf("ParseTiles(%q) 出错: %v", s, err)
	}
	return tiles
}

func TestMustParseTilesPanics(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"非法字符", "123x"},
		{"花色前无数字", "m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("MustParseTiles(%q) 应当 panic", tt.input)
				}
			}()
			MustParseTiles(tt.input)
		})
	}
}
