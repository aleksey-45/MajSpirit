package mahjong

import (
	"slices"
	"testing"
)

// tenpai_test.go —— 听牌判定与听牌料。
//
// 听牌判定的正确性完全建立在 Decompose 之上，所以这里不重复测拆解本身，
// 只测**边界**：多种待ち形、张数不是 3k+1、手里攥着四张自摸牌、副露后只剩 4 张。

// mustTiles 是这一组测试的简写。
func mustTiles(t *testing.T, s string) []Tile {
	t.Helper()
	return ParseTilesOrFail(t, s)
}

func TestWaits(t *testing.T) {
	tests := []struct {
		name string
		hand string
		want string // 期望的听牌，紧凑记法；空串表示未听牌
	}{
		{"两面听", "123m456m789m56s88p", "4s7s"},
		// 同一副牌有好几种听法是最常见的形态，别按"一种听"去写期望。
		{"三面听", "234m567m234s56s88p", "147s"},
		{"嵌张听", "234m567m234s46s88p", "5s"},
		{"边张听", "234m567m234s12s88p", "3s"},
		{"单骑听", "234m567m234s456s8p", "8p"},
		{"双碰听", "123m456m789m44s88p", "4s8p"},
		{"七对子听", "1188m2299s3344p5z", "5z"},
		// 国士十三面：13 种幺九牌各一张，13 面听。
		{"国士十三面", "19m19s19p1234567z", "19m19s19p1234567z"},
		// 副露 3 组之后暗牌只剩 4 张，张数本身说明了要拆几个面子。
		{"副露后只剩 4 张", "2345m", "2m5m"},
		{"单张暗牌", "5z", "5z"},
		// 只听一张，而那一张自己攥着四张 —— 第 5 张物理上不存在，所以不是听牌。
		// 挑字牌是有意的：数牌的四张还能拆进顺子（1111m 能拆成 111m + 123m），
		// 那样它就不是"只听那一张"，这条测试也就测不到东西了。
		{"四张全在手里", "1111z234m567m234s", ""},
		{"已经和牌", "234m567m234s456s88p", ""},
		{"张数不是 3k+1", "234m567m234s56s", ""},
		{"空手牌", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Waits(mustTiles(t, tt.hand))
			var want []Tile
			if tt.want != "" {
				want = mustTiles(t, tt.want)
			}
			if !slices.Equal(got, want) {
				t.Errorf("Waits(%s) = %s, 期望 %s",
					tt.hand, FormatTiles(got), FormatTiles(want))
			}
		})
	}
}

// IsTenpai 与 Waits 必须永远一致：两个入口判出不同答案时，听牌料和立直合法性
// 就会各按各的算。这条在 Waits 返回空切片而不是 nil 时也会抓到。
func TestIsTenpaiAgreesWithWaits(t *testing.T) {
	hands := []string{
		"234m567m234s56s88p", "234m567m234s456s8p", "1111m234m567m234s",
		"1188m2299s3344p5z", "19m19s1p1234567z", "19m19s19p1234567z",
	}
	for _, s := range hands {
		hand := mustTiles(t, s)
		if got, want := IsTenpai(hand), len(Waits(hand)) > 0; got != want {
			t.Errorf("IsTenpai(%s) = %v, 但 len(Waits)>0 = %v", s, got, want)
		}
	}
}

// ContainsWait 判的是牌种。舍牌里那张赤五万同样是五万 ——
// 直接比较 Tile 会在有人打过赤牌时漏判振听，而漏判振听等于允许他荣和。
func TestContainsWaitIgnoresAka(t *testing.T) {
	discards := mustTiles(t, "0m")
	waits := mustTiles(t, "5m")
	if !ContainsWait(discards, waits) {
		t.Error("舍牌里有赤五万，听五万，应当判为振听")
	}

	// 反向也一样：听着赤五万，舍牌里是普通五万。
	if !ContainsWait(mustTiles(t, "5m"), mustTiles(t, "0m")) {
		t.Error("舍牌里有五万，听赤五万，应当判为振听")
	}

	if ContainsWait(mustTiles(t, "5m"), mustTiles(t, "6m")) {
		t.Error("五万不是听牌张，不该判为振听")
	}
	if ContainsWait(nil, waits) || ContainsWait(mustTiles(t, "5m"), nil) {
		t.Error("空输入应当返回 false")
	}
}

// 非法牌不能 panic。牌从网络来，而 Waits / ContainsWait 内部要拿它去索引数组。
// 守卫只有一行，而 panic 在服务端等于把整条连接掀掉——代价不对等，所以留着并钉住。
func TestTenpaiHandlesInvalidTiles(t *testing.T) {
	bad := []Tile{200}

	if got := Waits(bad); got != nil {
		t.Errorf("Waits(非法牌) = %v, 期望 nil", got)
	}
	if IsTenpai(bad) {
		t.Error("Waits 返回空时 IsTenpai 必须是 false")
	}
	if ContainsWait(bad, mustTiles(t, "5m")) || ContainsWait(mustTiles(t, "5m"), bad) {
		t.Error("非法牌不应当匹配上任何听牌张")
	}
}

// TenpaiPayments 的四种人数分配是独立的四个事实（§11.1 的表）：
// 改错任何一行只有那一行会挂，所以四行都要在。
func TestTenpaiPayments(t *testing.T) {
	tests := []struct {
		name   string
		tenpai [4]bool
		want   [4]int
	}{
		{"1 人听牌", [4]bool{true, false, false, false}, [4]int{3000, -1000, -1000, -1000}},
		{"2 人听牌", [4]bool{true, true, false, false}, [4]int{1500, 1500, -1500, -1500}},
		{"3 人听牌", [4]bool{true, true, true, false}, [4]int{1000, 1000, 1000, -3000}},
		{"全听牌", [4]bool{true, true, true, true}, [4]int{0, 0, 0, 0}},
		{"全未听", [4]bool{false, false, false, false}, [4]int{0, 0, 0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TenpaiPayments(tt.tenpai)
			if got != tt.want {
				t.Errorf("TenpaiPayments(%v) = %v, 期望 %v", tt.tenpai, got, tt.want)
			}
			sum := 0
			for _, v := range got {
				sum += v
			}
			if sum != 0 {
				t.Errorf("流局不进钱也不出钱，收支之和应为 0，实际 %d", sum)
			}
		})
	}
}
