package mahjong

// Rules 是本局采用的规则口径。
//
// 本项目照搬 M-League（见 docs/规则集.md §14），**不支持多规则集**，
// 所以这里的字段不是"给用户配的开关"。那它们为什么存在？因为 Rules 还有一个
// 身份：它会被存进 `games.rules`（见 migrations/0001_init.sql），
// 而那份快照的全部意义是一句话——
//
//	"以后改了默认规则，旧牌局还能复核出来。"
//
// 这句话决定了**什么样的字段配留在这里**：算分时真的会读它。
// 一个算分不读的字段，在快照里兑现不了上面那句承诺——回放时它一个字节都救不回来。
//
// 所以那些"M-League 定死了、永远只会是同一个值"的规则轴（人和、累计役满、
// 单役种双倍役满、食い断）**不在这里留字段**。它们的取舍记在
// docs/规则集.md §14 的差异清单里，那里带【M】的章节出处，比代码注释全。
//
// 同一条原则文档里已经写过一次（§14.4）：不给「場ゾロ」加开关——
// 因为开关会邀请人去拨它，而拨它是 bug。这里只是把它贯彻到底。
type Rules struct {
	KiriageMangan bool // 切上满贯（M-League：采用）
	AkaDora       int  // 赤宝牌每种张数（M-League：各 1 张）
	Pao           bool // 帕奥／包（M-League：有，含四槓子）
}

// DefaultRules 返回 M-League 的规则。
func DefaultRules() Rules {
	return Rules{KiriageMangan: true, AkaDora: 1, Pao: true}
}

// NoSeat 表示「没有这个座位」——自摸时没有放铳者，也没有帕奥责任者。
const NoSeat = -1

// ScoreInput 是一次和牌算分所需的全部输入。
//
// 为什么是一个结构体而不是一长串参数：算分要同时知道谁和牌、谁放铳、谁是庄，
// 而这三个座位号决定了每一笔付给谁。散成位置参数时，调用方极容易把
// winnerSeat 和 dealerSeat 传反——那种错误不会报错，只会把点数算到别人头上。
type ScoreInput struct {
	// 翻数与符数。Han 是总翻数（役 + 宝牌 + 里宝 + 赤宝），不含場ゾロ
	// （見 docs/规则集.md §0.1：本项目的 Han 一律是实翻）。
	Han     int
	Fu      int
	Yakuman int // 役满倍数，0 表示非役满

	WinnerSeat int
	LoserSeat  int // 荣和时的放铳者；自摸传 NoSeat
	DealerSeat int // 本小局的庄家。seat 0 是起家，不是庄家

	IsTsumo bool

	Honba        int // 本场数
	RiichiSticks int // 场上累积的立直棒数量，全部归和牌者

	// 帕奥（包，見 docs/规则集.md §8.5）。
	// PaoSeat 非 NoSeat 时，PaoYakuman 那一个役满改由 PaoSeat 单独承担。
	//
	// 前置条件：0 <= PaoYakuman <= Yakuman。 PaoYakuman 是「被喂成的那一个」
	// 役满的倍数，其余部分照常结算；无帕奥时传 NoSeat / 0。
	PaoSeat    int
	PaoYakuman int // 被喂成的那个役满的倍数（不是总倍数）
}

// 基本点的封顶档位（docs/规则集.md §7.2）。
const (
	baseMangan    = 2000
	baseHaneman   = 3000
	baseBaiman    = 4000
	baseSanbaiman = 6000 // 三倍满。M-League 不采用累计役满，实翻 11+ 一律封到这里
	baseYakuman   = 8000
)

// BasePoints 返回基本点（已按 docs/规则集.md §7 封顶）。
//
//	基本点 = 符 × 2^(2+翻)，超过上限则按档位取值
//
// 导出是为了让测试能直接对着【基本点】断言，而不必绕过支付规则间接验证——
// 点数速查表的错往往出在这一层，不是出在支付规则。
func BasePoints(han, fu, yakuman int, rules Rules) int {
	if yakuman > 0 {
		return baseYakuman * yakuman
	}
	switch {
	case han >= 11:
		return baseSanbaiman
	case han >= 8:
		return baseBaiman
	case han >= 6:
		return baseHaneman
	case han >= 5:
		return baseMangan
	}

	base := fu << uint(2+han)
	if base > baseMangan {
		return baseMangan
	}
	// 切上满贯只影响两个临界值：30 符 4 翻、60 符 3 翻
	if rules.KiriageMangan {
		if (fu == 30 && han == 4) || (fu == 60 && han == 3) {
			return baseMangan
		}
	}
	return base
}

// Ceil100 向上取整到百位。
//
// 每一笔支付要分别调用，不是先求和再取整（docs/规则集.md §8）。
func Ceil100(n int) int { return (n + 99) / 100 * 100 }

// CalcScore 返回四个座位各自的收支，按座位索引（含本场与供托）。
// 返回值可以直接加到四家的分数上。
//
// 规则见 docs/规则集.md §7、§8。
//
// 守恒式：`sum(返回值) == 1000 * RiichiSticks`。
// 立直棒不是转账——它在宣言时就已经扣进供托，这里只是记回和牌者名下，
// 所以四家总和会多出那么多。不含立直棒时退化成 `sum == 0`。
//
// 三条最容易写错的地方：
//
//  1. 每笔支付分别向上取整到百位，不是先求和再取整。
//     子家 30 符 4 翻 自摸，基本点 1920 → 2000 / 2000 / 3900（不是 7700 那种算法）。
//  2. 荣和只有放铳者付钱，其余两家不动。
//  3. 帕奥时本场棒全部由责任者出，不是三家分摊
//     （【M】第 8 章第 1 条 2「積み場は鳴かせた者が負担する」）。
func CalcScore(in ScoreInput, rules Rules) [4]int {
	var out [4]int

	// 帕奥：那一个役满改由责任者单独承担，其余部分照常结算。
	// 先算「不含帕奥役满」的部分，再把帕奥那一份单独记到责任者头上。
	paoBase := 0
	restYakuman := in.Yakuman
	if rules.Pao && in.PaoSeat != NoSeat && in.PaoYakuman > 0 {
		paoBase = baseYakuman * in.PaoYakuman
		restYakuman = in.Yakuman - in.PaoYakuman
	}

	// 除帕奥之外的部分：按常规分摊
	if restYakuman > 0 || paoBase == 0 {
		base := BasePoints(in.Han, in.Fu, restYakuman, rules)
		applyNormalPayment(&out, in, base)
	}

	// 帕奥部分
	if paoBase > 0 {
		dealer := in.WinnerSeat == in.DealerSeat
		switch {
		case in.IsTsumo:
			// 自摸：帕奥者一人独付全额，另两家不付这一个役满
			total := ceilTotal(paoBase, dealer, true)
			pay(&out, in.PaoSeat, -total)
			pay(&out, in.WinnerSeat, total)
		case in.LoserSeat == in.PaoSeat:
			// 帕奥者放铳：一人全付
			total := ceilTotal(paoBase, dealer, false)
			pay(&out, in.PaoSeat, -total)
			pay(&out, in.WinnerSeat, total)
		default:
			// 别人放铳：帕奥者与放铳者各付一半（【慣】，官方只写「折半払い」）
			total := ceilTotal(paoBase, dealer, false)
			half := total / 2
			pay(&out, in.PaoSeat, -half)
			pay(&out, in.LoserSeat, -(total - half))
			pay(&out, in.WinnerSeat, total)
		}
	}

	// 本场棒：总额恒为 300×本场。
	// 无帕奥时荣和由放铳者一人出、自摸三家各出 100×本场；
	// 有帕奥时全部由帕奥责任者一个人出（见下）。
	if in.Honba > 0 {
		total := 300 * in.Honba
		switch {
		case paoBase > 0:
			// 【M】第 8 章第 1 条 2：「積み場は鳴かせた者が負担する」。
			// 官方第 8 章第 1 条 3 的复合役满例子可以直接核对：一本场、南家自摸、
			// 西家是帕奥者时，西家支出 −40300 = 32300 + 8000，
			// 其中 32300 = 32000（大三元）+ 300（本场棒，全部由他出）。
			pay(&out, in.PaoSeat, -total)
			pay(&out, in.WinnerSeat, total)
		case in.IsTsumo:
			for seat := 0; seat < 4; seat++ {
				if seat == in.WinnerSeat {
					continue
				}
				pay(&out, seat, -100*in.Honba)
				pay(&out, in.WinnerSeat, 100*in.Honba)
			}
		case in.LoserSeat != NoSeat:
			pay(&out, in.LoserSeat, -total)
			pay(&out, in.WinnerSeat, total)
		}
	}

	// 立直棒：全部归和牌者。
	// 注意这不是一笔转账——立直棒在宣言时就已经从某人分数里扣掉进了供托，
	// 这里只是把它记回和牌者名下，所以四家总和会增加 1000×立直棒数。
	if in.RiichiSticks > 0 {
		pay(&out, in.WinnerSeat, 1000*in.RiichiSticks)
	}

	return out
}

// applyNormalPayment 把「除帕奥之外」的那部分按子/庄、自摸/荣和分摊。
func applyNormalPayment(out *[4]int, in ScoreInput, base int) {
	dealer := in.WinnerSeat == in.DealerSeat

	if in.IsTsumo {
		if dealer {
			each := Ceil100(base * 2) // 庄家自摸：三家各付 B×2
			for seat := 0; seat < 4; seat++ {
				if seat == in.WinnerSeat {
					continue
				}
				pay(out, seat, -each)
				pay(out, in.WinnerSeat, each)
			}
			return
		}
		child := Ceil100(base)          // 子家自摸：子家付 B×1
		fromDealer := Ceil100(base * 2) // 庄家付 B×2
		for seat := 0; seat < 4; seat++ {
			if seat == in.WinnerSeat {
				continue
			}
			amount := child
			if seat == in.DealerSeat {
				amount = fromDealer
			}
			pay(out, seat, -amount)
			pay(out, in.WinnerSeat, amount)
		}
		return
	}

	// 荣和：只有放铳者付钱
	if in.LoserSeat == NoSeat {
		return
	}
	mult := 4
	if dealer {
		mult = 6
	}
	amount := Ceil100(base * mult)
	pay(out, in.LoserSeat, -amount)
	pay(out, in.WinnerSeat, amount)
}

// ceilTotal 返回某个役满按子/庄、自摸/荣和应付的总额（未分摊前的口径）。
func ceilTotal(base int, dealer, tsumo bool) int {
	switch {
	case dealer && tsumo:
		return Ceil100(base*2) * 3
	case dealer:
		return Ceil100(base * 6)
	case tsumo:
		// 子家自摸：子家 B×1、庄家 B×2
		return Ceil100(base) + Ceil100(base) + Ceil100(base*2)
	default:
		return Ceil100(base * 4)
	}
}

// pay 给某个座位的收支累加一笔。WinnerSeat 可能与其他角色重合，所以用累加而不是赋值。
func pay(out *[4]int, seat, amount int) {
	if seat < 0 || seat >= 4 {
		return
	}
	out[seat] += amount
}
