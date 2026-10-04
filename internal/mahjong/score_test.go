package mahjong

import (
	"math/rand/v2"
	"strconv"
	"testing"
)

// score_test.go —— 基本点、取整、四家收支。
//
// **期望值一律照抄 docs/规则集.md，不用公式重算。**
// 拿实现里同一条公式重算期望值就变成同义反复，测不出公式本身的错误。
// 这里的真值来源分三档，一档比一档硬：
//
//   - §9 的四张点数速查表 —— 独立脚本按公式核对过 132 项（仍在用公式，最弱）
//   - §12 的六个完整算例 —— 手算
//   - §8.5 的复合役满例子 —— **M-League 官方原文**（最强）

// 四张速查表用同一套座位安排，让表看起来和文档一样：
//
//	子家：座位 1 和牌、座位 0 是庄、座位 2 放铳
//	庄家：座位 0 和牌兼庄、座位 2 放铳
//
// 「庄家 = 座位 0」这个巧合**只在庄家的表里**成立，另有
// TestCalcScoreDealerIsNotAlwaysSeatZero 专门守住这一点。
const (
	childWinner  = 1
	childDealer  = 0
	dealerWinner = 0
	dealerSeat   = 0
	loserSeat    = 2
)

// ── §9.1 子家荣和 ────────────────────────────────────────────────────────
//
// 每个 fu 行是 {1 翻, 2 翻, 3 翻, 4 翻} 四个格子的支付额。
// **0 表示文档里的「—」**（该符翻组合不存在，例如 20 符 1 翻）——
// 真实支付额永远不为 0，所以这个哨兵值不会和真值混淆。
var ronChildTable = []struct {
	fu   int
	want [4]int
}{
	{20, [4]int{0, 1300, 2600, 5200}},
	{25, [4]int{0, 1600, 3200, 6400}},
	{30, [4]int{1000, 2000, 3900, 8000}}, // 4 翻是切上满贯
	{40, [4]int{1300, 2600, 5200, 8000}},
	{50, [4]int{1600, 3200, 6400, 8000}},
	{60, [4]int{2000, 3900, 8000, 8000}}, // 3 翻是切上满贯
	{70, [4]int{2300, 4500, 8000, 8000}},
	{80, [4]int{2600, 5200, 8000, 8000}},
	{90, [4]int{2900, 5800, 8000, 8000}},
	{100, [4]int{3200, 6400, 8000, 8000}},
	{110, [4]int{3600, 7100, 8000, 8000}},
}

func TestScoreTableRonChild(t *testing.T) {
	for _, row := range ronChildTable {
		for han := 1; han <= 4; han++ {
			want := row.want[han-1]
			if want == 0 {
				continue // 文档里的「—」
			}
			t.Run(scoreCaseName("子家荣和", row.fu, han), func(t *testing.T) {
				got := CalcScore(ScoreInput{
					Han:        han,
					Fu:         row.fu,
					WinnerSeat: childWinner,
					LoserSeat:  loserSeat,
					DealerSeat: childDealer,
				}, DefaultRules())

				// 荣和：只有放铳者出钱，另两家一动都不动
				assertSeats(t, got, [4]int{0, want, -want, 0})
			})
		}
	}
}

// ── §9.2 子家自摸 ────────────────────────────────────────────────────────

var tsumoChildTable = []struct {
	fu          int
	childShare  [4]int // 子家支付（和牌者收 2 份）
	dealerShare [4]int // 庄家支付（和牌者收 1 份）
}{
	{20, [4]int{0, 400, 700, 1300}, [4]int{0, 700, 1300, 2600}},
	{25, [4]int{0, 400, 800, 1600}, [4]int{0, 800, 1600, 3200}},
	{30, [4]int{300, 500, 1000, 2000}, [4]int{500, 1000, 2000, 4000}},
	{40, [4]int{400, 700, 1300, 2000}, [4]int{700, 1300, 2600, 4000}},
	{50, [4]int{400, 800, 1600, 2000}, [4]int{800, 1600, 3200, 4000}},
	{60, [4]int{500, 1000, 2000, 2000}, [4]int{1000, 2000, 4000, 4000}},
	{70, [4]int{600, 1200, 2000, 2000}, [4]int{1200, 2300, 4000, 4000}},
}

func TestScoreTableTsumoChild(t *testing.T) {
	for _, row := range tsumoChildTable {
		for han := 1; han <= 4; han++ {
			child, dealer := row.childShare[han-1], row.dealerShare[han-1]
			if child == 0 {
				continue
			}
			t.Run(scoreCaseName("子家自摸", row.fu, han), func(t *testing.T) {
				got := CalcScore(ScoreInput{
					Han:        han,
					Fu:         row.fu,
					WinnerSeat: childWinner,
					LoserSeat:  NoSeat,
					DealerSeat: childDealer,
					IsTsumo:    true,
				}, DefaultRules())

				// 座位 1 和牌、座位 0 是庄、座位 2/3 是子家
				assertSeats(t, got, [4]int{-dealer, dealer + child + child, -child, -child})
			})
		}
	}
}

// ── §9.3 庄家荣和 ────────────────────────────────────────────────────────

var ronDealerTable = []struct {
	fu   int
	want [4]int
}{
	{20, [4]int{0, 2000, 3900, 7700}},
	{25, [4]int{0, 2400, 4800, 9600}},
	{30, [4]int{1500, 2900, 5800, 12000}},
	{40, [4]int{2000, 3900, 7700, 12000}},
	{50, [4]int{2400, 4800, 9600, 12000}},
	{60, [4]int{2900, 5800, 12000, 12000}},
	{70, [4]int{3400, 6800, 12000, 12000}},
}

func TestScoreTableRonDealer(t *testing.T) {
	for _, row := range ronDealerTable {
		for han := 1; han <= 4; han++ {
			want := row.want[han-1]
			if want == 0 {
				continue
			}
			t.Run(scoreCaseName("庄家荣和", row.fu, han), func(t *testing.T) {
				got := CalcScore(ScoreInput{
					Han:        han,
					Fu:         row.fu,
					WinnerSeat: dealerWinner,
					LoserSeat:  loserSeat,
					DealerSeat: dealerSeat,
				}, DefaultRules())
				assertSeats(t, got, [4]int{want, 0, -want, 0})
			})
		}
	}
}

// ── §9.4 庄家自摸（三家各付） ───────────────────────────────────────────

var tsumoDealerTable = []struct {
	fu   int
	each [4]int
}{
	{20, [4]int{0, 700, 1300, 2600}},
	{25, [4]int{0, 800, 1600, 3200}},
	{30, [4]int{500, 1000, 2000, 4000}},
	{40, [4]int{700, 1300, 2600, 4000}},
	{50, [4]int{800, 1600, 3200, 4000}},
	{60, [4]int{1000, 2000, 4000, 4000}},
}

func TestScoreTableTsumoDealer(t *testing.T) {
	for _, row := range tsumoDealerTable {
		for han := 1; han <= 4; han++ {
			each := row.each[han-1]
			if each == 0 {
				continue
			}
			t.Run(scoreCaseName("庄家自摸", row.fu, han), func(t *testing.T) {
				got := CalcScore(ScoreInput{
					Han:        han,
					Fu:         row.fu,
					WinnerSeat: dealerWinner,
					LoserSeat:  NoSeat,
					DealerSeat: dealerSeat,
					IsTsumo:    true,
				}, DefaultRules())
				assertSeats(t, got, [4]int{each * 3, -each, -each, -each})
			})
		}
	}
}

// ── 四张表的「满贯以上」行 ──────────────────────────────────────────────
//
// 期望值仍然是文档里的字面量，不是「基本点 × 倍数」重算出来的。

func TestScoreTableCaps(t *testing.T) {
	// han 用 0 表示走役满分支（Yakuman = 1）
	tests := []struct {
		name    string
		han     int
		yakuman int
		dealer  bool
		tsumo   bool
		want    [4]int
	}{
		// 子家荣和：5 翻=8000 · 6-7 翻=12000 · 8-10 翻=16000 · 11-12 翻=24000 · 役满=32000
		{"子家荣和 5翻", 5, 0, false, false, [4]int{0, 8000, -8000, 0}},
		{"子家荣和 6翻", 6, 0, false, false, [4]int{0, 12000, -12000, 0}},
		{"子家荣和 7翻", 7, 0, false, false, [4]int{0, 12000, -12000, 0}},
		{"子家荣和 8翻", 8, 0, false, false, [4]int{0, 16000, -16000, 0}},
		{"子家荣和 10翻", 10, 0, false, false, [4]int{0, 16000, -16000, 0}},
		{"子家荣和 11翻", 11, 0, false, false, [4]int{0, 24000, -24000, 0}},
		{"子家荣和 12翻", 12, 0, false, false, [4]int{0, 24000, -24000, 0}},
		// 13 翻**不**是累计役满——M-League 封在三倍满
		{"子家荣和 13翻仍是三倍满", 13, 0, false, false, [4]int{0, 24000, -24000, 0}},
		{"子家荣和 役满", 0, 1, false, false, [4]int{0, 32000, -32000, 0}},

		// 子家自摸：5 翻=2000/4000 · 6-7=3000/6000 · 8-10=4000/8000 · 11-12=6000/12000 · 役满=8000/16000
		{"子家自摸 5翻", 5, 0, false, true, [4]int{-4000, 8000, -2000, -2000}},
		{"子家自摸 6翻", 6, 0, false, true, [4]int{-6000, 12000, -3000, -3000}},
		{"子家自摸 8翻", 8, 0, false, true, [4]int{-8000, 16000, -4000, -4000}},
		{"子家自摸 11翻", 11, 0, false, true, [4]int{-12000, 24000, -6000, -6000}},
		{"子家自摸 役满", 0, 1, false, true, [4]int{-16000, 32000, -8000, -8000}},

		// 庄家荣和：5 翻=12000 · 6-7=18000 · 8-10=24000 · 11-12=36000 · 役满=48000
		{"庄家荣和 5翻", 5, 0, true, false, [4]int{12000, 0, -12000, 0}},
		{"庄家荣和 6翻", 6, 0, true, false, [4]int{18000, 0, -18000, 0}},
		{"庄家荣和 8翻", 8, 0, true, false, [4]int{24000, 0, -24000, 0}},
		{"庄家荣和 11翻", 11, 0, true, false, [4]int{36000, 0, -36000, 0}},
		{"庄家荣和 役满", 0, 1, true, false, [4]int{48000, 0, -48000, 0}},

		// 庄家自摸：5 翻=4000 · 6-7=6000 · 8-10=8000 · 11-12=12000 · 役满=16000
		{"庄家自摸 5翻", 5, 0, true, true, [4]int{12000, -4000, -4000, -4000}},
		{"庄家自摸 6翻", 6, 0, true, true, [4]int{18000, -6000, -6000, -6000}},
		{"庄家自摸 8翻", 8, 0, true, true, [4]int{24000, -8000, -8000, -8000}},
		{"庄家自摸 11翻", 11, 0, true, true, [4]int{36000, -12000, -12000, -12000}},
		{"庄家自摸 役满", 0, 1, true, true, [4]int{48000, -16000, -16000, -16000}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := ScoreInput{
				Han:        tt.han,
				Fu:         30, // 满贯以上符数不参与计算，给个合法值即可
				Yakuman:    tt.yakuman,
				WinnerSeat: childWinner,
				LoserSeat:  loserSeat,
				DealerSeat: childDealer,
			}
			if tt.dealer {
				in.WinnerSeat, in.DealerSeat = dealerWinner, dealerSeat
			}
			if tt.tsumo {
				in.IsTsumo = true
				in.LoserSeat = NoSeat
			}
			assertSeats(t, CalcScore(in, DefaultRules()), tt.want)
		})
	}
}

// ── §7.2 基本点 ─────────────────────────────────────────────────────────

func TestBasePointsFormula(t *testing.T) {
	rules := DefaultRules()
	tests := []struct {
		fu, han int
		want    int
	}{
		{30, 1, 240},   // 30 × 2^3
		{30, 2, 480},   // 30 × 2^4
		{30, 3, 960},   // 30 × 2^5
		{20, 2, 320},   // 20 × 2^4
		{20, 3, 640},   //
		{25, 2, 400},   // 七对子
		{40, 2, 640},   //
		{110, 2, 1760}, // 110 × 2^4，仍未到满贯
	}

	for _, tt := range tests {
		t.Run(scoreCaseName("基本点", tt.fu, tt.han), func(t *testing.T) {
			if got := BasePoints(tt.han, tt.fu, 0, rules); got != tt.want {
				t.Errorf("BasePoints(%d 翻, %d 符) = %d，期望 %d",
					tt.han, tt.fu, got, tt.want)
			}
		})
	}
}

func TestBasePointsCaps(t *testing.T) {
	rules := DefaultRules()
	tests := []struct {
		name    string
		han     int
		fu      int
		yakuman int
		want    int
	}{
		{"满贯", 5, 30, 0, 2000},
		{"跳满", 6, 30, 0, 3000},
		{"跳满上限", 7, 30, 0, 3000},
		{"倍满", 8, 30, 0, 4000},
		{"倍满上限", 10, 30, 0, 4000},
		{"三倍满", 11, 30, 0, 6000},
		{"三倍满上限", 12, 30, 0, 6000},
		// M-League 不采用累计役满：再高的实翻也封在三倍满
		{"13 翻仍封在三倍满", 13, 30, 0, 6000},
		{"20 翻仍封在三倍满", 20, 30, 0, 6000},
		{"役满", 0, 0, 1, 8000},
		{"两倍役满", 0, 0, 2, 16000},
		{"三倍役满", 0, 0, 3, 24000},
		{"役满时翻数作废", 11, 30, 1, 8000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BasePoints(tt.han, tt.fu, tt.yakuman, rules); got != tt.want {
				t.Errorf("BasePoints(%d 翻, %d 符, %d 倍役满) = %d，期望 %d",
					tt.han, tt.fu, tt.yakuman, got, tt.want)
			}
		})
	}
}

// 切上满贯只影响 30 符 4 翻与 60 符 3 翻两个临界值，四张速查表里也只差那四格。
func TestKiriageMangan(t *testing.T) {
	on := DefaultRules()
	off := DefaultRules()
	off.KiriageMangan = false

	tests := []struct {
		name   string
		fu     int
		han    int
		stric  int // 严格值（关掉切上）
		kiriag int // 切上后的值
	}{
		{"30 符 4 翻", 30, 4, 7700, 8000},
		{"60 符 3 翻", 60, 3, 7700, 8000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BasePoints(tt.han, tt.fu, 0, off); got != 1920 {
				t.Errorf("关闭切上时基本点 = %d，期望 1920（严格值未超过满贯线）", got)
			}
			if got := BasePoints(tt.han, tt.fu, 0, on); got != 2000 {
				t.Errorf("开启切上时基本点 = %d，期望 2000", got)
			}

			in := ScoreInput{Fu: tt.fu, Han: tt.han, WinnerSeat: childWinner, LoserSeat: loserSeat, DealerSeat: childDealer}
			if got := CalcScore(in, off)[childWinner]; got != tt.stric {
				t.Errorf("关闭切上时子家荣和 = %d，期望 %d", got, tt.stric)
			}
			if got := CalcScore(in, on)[childWinner]; got != tt.kiriag {
				t.Errorf("开启切上时子家荣和 = %d，期望 %d", got, tt.kiriag)
			}
		})
	}

	// 只有这两格受影响：邻近的 30 符 3 翻 / 40 符 4 翻 开关不变
	for _, c := range []struct{ fu, han int }{{30, 3}, {40, 4}, {60, 4}, {20, 4}} {
		if a, b := BasePoints(c.han, c.fu, 0, on), BasePoints(c.han, c.fu, 0, off); a != b {
			t.Errorf("%d 符 %d 翻 不该受切上满贯影响，开关为 %d / %d", c.fu, c.han, b, a)
		}
	}
}

func TestCeil100(t *testing.T) {
	tests := []struct{ in, want int }{
		{0, 0}, {1, 100}, {99, 100}, {100, 100}, {101, 200},
		{1920, 2000}, {3840, 3900}, {7680, 7700}, {2560, 2600},
		{4480, 4500}, {7040, 7100}, {2240, 2300},
	}
	for _, tt := range tests {
		if got := Ceil100(tt.in); got != tt.want {
			t.Errorf("Ceil100(%d) = %d，期望 %d", tt.in, got, tt.want)
		}
	}
}

// ── §8.5 帕奥（包） ─────────────────────────────────────────────────────

// M-League 官方原文给的复合役满例子（【M】第 8 章第 1 条 3）。
//
// **一本场，西家让南家碰出大三元，南家手牌是大三元 + 字一色（2 倍役满），南家自摸**：
//
//	東家16000、西家32300+8000、北家8000 とする。
//
// 这一条的价值在于它是**外部权威**：不是我们自己按公式推的，是官方给的数字。
// 尤其能锁住「帕奥时本场棒全部由责任者出」——按普通自摸三家分摊会得到
// 西家 −40100，和官方的 −40300 差 200。
func TestCalcScorePaoOfficialExample(t *testing.T) {
	in := ScoreInput{
		Yakuman:    2, // 大三元 + 字一色
		WinnerSeat: 1, // 南家
		DealerSeat: 0, // 东家
		LoserSeat:  NoSeat,
		IsTsumo:    true,
		Honba:      1, // 一本场
		PaoSeat:    2, // 西家
		PaoYakuman: 1, // 大三元那一个
	}

	// 东家（庄）−16000 / 南家（和牌者）+64300 / 西家（帕奥者）−40300 / 北家 −8000
	assertSeats(t, CalcScore(in, DefaultRules()), [4]int{-16000, 64300, -40300, -8000})
}

// 帕奥自摸：责任者一人独付全额，另两家**完全不付**这一个役满。
func TestCalcScorePaoTsumo(t *testing.T) {
	in := ScoreInput{
		Yakuman:    1,
		WinnerSeat: 1, // 南家（子家）
		DealerSeat: 0,
		LoserSeat:  NoSeat,
		IsTsumo:    true,
		PaoSeat:    2,
		PaoYakuman: 1,
	}
	// 子家自摸一个役满 = 8000 / 16000，全额由帕奥者出 = 32000
	assertSeats(t, CalcScore(in, DefaultRules()), [4]int{0, 32000, -32000, 0})
}

// 帕奥者自己放铳：一人全付。
func TestCalcScorePaoSeatDealsIn(t *testing.T) {
	in := ScoreInput{
		Yakuman:    1,
		WinnerSeat: 1,
		DealerSeat: 0,
		LoserSeat:  3, // 帕奥者放铳
		PaoSeat:    3,
		PaoYakuman: 1,
	}
	// 子家荣和一个役满 = 32000，帕奥者一人出
	assertSeats(t, CalcScore(in, DefaultRules()), [4]int{0, 32000, 0, -32000})
}

// 别人放铳：帕奥者与放铳者各付一半（【慣】，官方只写「折半払い」）。
func TestCalcScorePaoOtherDiscarderHalves(t *testing.T) {
	in := ScoreInput{
		Yakuman:    1,
		WinnerSeat: 1,
		DealerSeat: 0,
		LoserSeat:  2, // 放铳者
		PaoSeat:    3, // 帕奥者
		PaoYakuman: 1,
	}
	// 子家荣和 32000 拆成 16000 + 16000
	assertSeats(t, CalcScore(in, DefaultRules()), [4]int{0, 32000, -16000, -16000})
}

// 关掉 Rules.Pao 时，同一个输入退回普通役满结算。
func TestCalcScorePaoDisabled(t *testing.T) {
	rules := DefaultRules()
	rules.Pao = false

	in := ScoreInput{
		Yakuman:    1,
		WinnerSeat: 1,
		DealerSeat: 0,
		LoserSeat:  NoSeat,
		IsTsumo:    true,
		PaoSeat:    2,
		PaoYakuman: 1,
	}
	// 普通子家自摸一个役满：庄家 16000、两子各 8000
	assertSeats(t, CalcScore(in, rules), [4]int{-16000, 32000, -8000, -8000})
}

// ── 本场棒与供托 ─────────────────────────────────────────────────────────

func TestCalcScoreHonba(t *testing.T) {
	t.Run("荣和由放铳者出 300/本场", func(t *testing.T) {
		got := CalcScore(ScoreInput{
			Han: 1, Fu: 30, WinnerSeat: childWinner, LoserSeat: loserSeat, DealerSeat: childDealer,
			Honba: 2,
		}, DefaultRules())
		// 1000 + 600
		assertSeats(t, got, [4]int{0, 1600, -1600, 0})
	})

	t.Run("自摸三家各出 100/本场", func(t *testing.T) {
		got := CalcScore(ScoreInput{
			Han: 1, Fu: 30, WinnerSeat: childWinner, LoserSeat: NoSeat, DealerSeat: childDealer,
			IsTsumo: true, Honba: 3,
		}, DefaultRules())
		// 300/500 加本场后：庄家 500+300、两子各 300+300 → 和牌者拿 500+300+300+300+300+300
		assertSeats(t, got, [4]int{-800, 2000, -600, -600})
	})

	t.Run("帕奥时本场棒全部由责任者出", func(t *testing.T) {
		a := CalcScore(ScoreInput{
			Yakuman: 1, WinnerSeat: 1, DealerSeat: 0, LoserSeat: NoSeat, IsTsumo: true,
			PaoSeat: 2, PaoYakuman: 1, Honba: 1,
		}, DefaultRules())
		assertSeats(t, a, [4]int{0, 32300, -32300, 0})

		// 对照：无帕奥时同一个役满本场由三家分摊
		b := CalcScore(ScoreInput{
			Yakuman: 1, WinnerSeat: 1, DealerSeat: 0, LoserSeat: NoSeat, IsTsumo: true,
			Honba: 1,
		}, DefaultRules())
		assertSeats(t, b, [4]int{-16100, 32300, -8100, -8100})
	})
}

func TestCalcScoreRiichiSticks(t *testing.T) {
	// 立直棒不是转账：它在宣言时就已经扣进供托，所以四家总和会增加。
	in := ScoreInput{
		WinnerSeat: childWinner, LoserSeat: loserSeat, DealerSeat: childDealer,
		RiichiSticks: 3,
	}
	got := CalcScore(in, DefaultRules())

	// 翻数符数都是 0，所以除了供托没有任何支付
	assertSeats(t, got, [4]int{0, 3000, 0, 0})
	if sum := sumSeats(got); sum != 3000 {
		t.Errorf("四家总和 = %d，期望 3000（= 1000 × 立直棒数）", sum)
	}
}

// ── 座位排列 ─────────────────────────────────────────────────────────────

// 历史实现里把「座位 0 = 庄家」写死过。这条测试把庄家挪到别的座位上核对。
func TestCalcScoreDealerIsNotAlwaysSeatZero(t *testing.T) {
	// 庄家在座位 2（不是 0），和牌者也是座位 2 → 庄家荣和
	got := CalcScore(ScoreInput{
		Han: 1, Fu: 30,
		WinnerSeat: 2, LoserSeat: 1, DealerSeat: 2,
	}, DefaultRules())

	if got[2] != 1500 {
		t.Errorf("庄家荣和 = %d，期望 1500（庄家在座位 2）", got[2])
	}
	assertSeats(t, got, [4]int{0, -1500, 1500, 0})

	// 同一手牌、同一个和牌座位，只把庄家挪走 → 变成子家荣和 1000。
	// 这一对照就是这条测试的全部意义：唯一变的是 DealerSeat。
	child := CalcScore(ScoreInput{
		Han: 1, Fu: 30,
		WinnerSeat: 2, LoserSeat: 1, DealerSeat: 0,
	}, DefaultRules())
	assertSeats(t, child, [4]int{0, -1000, 1000, 0})
}

// 荣和只有放铳者付钱。自摸传 NoSeat 时不该有任何点数流动。
func TestCalcScoreRonOnlyLoserPays(t *testing.T) {
	got := CalcScore(ScoreInput{
		Han: 4, Fu: 40, WinnerSeat: childWinner, LoserSeat: loserSeat, DealerSeat: childDealer,
	}, DefaultRules())
	for seat := 0; seat < 4; seat++ {
		if seat == childWinner || seat == loserSeat {
			continue
		}
		if got[seat] != 0 {
			t.Errorf("荣和时座位 %d 不该有收支，得到 %d", seat, got[seat])
		}
	}

	// 自摸传 NoSeat（或忘了设 IsTsumo）时无人付钱
	silent := CalcScore(ScoreInput{
		Han: 4, Fu: 40, WinnerSeat: childWinner, LoserSeat: NoSeat, DealerSeat: childDealer,
	}, DefaultRules())
	assertSeats(t, silent, [4]int{})
}

// ── 守恒式 ───────────────────────────────────────────────────────────────

// sum(收支) == 1000 × 立直棒数。
//
// 这条不变量比任何单点断言都强：它同时覆盖了「漏了某一家」「某一家付了两次」
// 「帕奥拆账算错」「取整方向反了」这些错误。
func TestCalcScoreConservation(t *testing.T) {
	rules := DefaultRules()
	r := rand.New(rand.NewPCG(2026, 10))

	fus := []int{20, 25, 30, 40, 50, 60, 70, 80, 90, 100, 110}

	for i := 0; i < 20000; i++ {
		winner := r.IntN(4)
		dealer := r.IntN(4)
		tsumo := r.IntN(2) == 0

		in := ScoreInput{
			WinnerSeat:   winner,
			DealerSeat:   dealer,
			IsTsumo:      tsumo,
			LoserSeat:    NoSeat,
			Fu:           fus[r.IntN(len(fus))],
			Han:          1 + r.IntN(13),
			Honba:        r.IntN(5),
			RiichiSticks: r.IntN(5),
		}
		if !tsumo {
			in.LoserSeat = (winner + 1 + r.IntN(3)) % 4 // 放铳者必然不是和牌者
		}
		if r.IntN(4) == 0 {
			// 役满：倍数 1..3，其中一部分带帕奥
			in.Yakuman = 1 + r.IntN(3)
			if r.IntN(2) == 0 {
				in.PaoYakuman = 1 + r.IntN(in.Yakuman)
				in.PaoSeat = (winner + 1 + r.IntN(3)) % 4 // 责任者必然不是和牌者
			}
		}

		got := CalcScore(in, rules)

		want := 1000 * in.RiichiSticks
		if sum := sumSeats(got); sum != want {
			t.Fatalf("第 %d 组收支不守恒：\n输入 %+v\n得到 %v（总和 %d，期望 %d）",
				i, in, got, sum, want)
		}
	}
}

// ── 辅助 ─────────────────────────────────────────────────────────────────

// assertSeats 逐座位比对。want 为空表示期望四个座位全为 0。
func assertSeats(t *testing.T, got [4]int, want [4]int) {
	t.Helper()
	if got == want {
		return
	}
	t.Errorf("收支不符：\n得到 %v（总和 %d）\n期望 %v（总和 %d）",
		got, sumSeats(got), want, sumSeats(want))
}

func sumSeats(s [4]int) int { return s[0] + s[1] + s[2] + s[3] }

// scoreCaseName 让 t.Run 的名字带上符翻，失败时一眼能定位到速查表的哪一格。
func scoreCaseName(prefix string, fu, han int) string {
	return prefix + " " + strconv.Itoa(fu) + "符" + strconv.Itoa(han) + "翻"
}
