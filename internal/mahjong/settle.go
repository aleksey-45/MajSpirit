package mahjong

import (
	"errors"
	"fmt"
)

// settle.go —— 一次和牌的完整判定与结算（docs/规则集.md §13.1 的 ①–⑨）。
//
//	① 和牌形判定        Decompose
//	② 必须有役          DetectYaku 返回空 → 不能和（§2.1）
//	③ 不能处于振听      荣和时检查（§2.2）
//	④ 选出点数最高的拆法  高点法
//	⑤ 役 → 翻数          DetectYaku
//	⑥ 宝牌              CountDora + CountAka（**役满时跳过**）
//	⑦ 符                CalcFu（**役满时跳过**）
//	⑧⑨ 基本点与收支      BasePoints / CalcScore
//
// 这个文件存在的理由不是「少写几行」，而是**这九步的顺序本身就是规则**：
// 宝牌排在役之后（否则会做出「只有宝牌也能和牌」的游戏），符排在役满之后（役满不计符）。
// 把它们拆给调用方，等于让每个调用方各排一次顺序，而排错不会报错、只会算错分。
//
// 剩下的两件事规则层判不了，由房间层填进来：**振听**（要看舍牌与见逃历史）和
// **帕奥责任者**（必须在鸣牌那一刻记下，§8.5）。

var (
	// ErrNotWinningHand 表示牌型不是和牌型（①）。
	ErrNotWinningHand = errors.New("mahjong: 不是和牌型")

	// ErrNoYaku 表示牌型合法但没有任何役（②）。宝牌不是役，攥着四个宝牌也一样不能和。
	ErrNoYaku = errors.New("mahjong: 和牌没有役")

	// ErrFuriten 表示振听，不能荣和（③）。振听不限制自摸——自摸时这个错误不会出现。
	ErrFuriten = errors.New("mahjong: 振听")
)

// WinInput 是一次和牌判定的全部输入。
type WinInput struct {
	// Hand 是**暗牌，且含 WinningTile**：荣和时房间层要先把那张牌加进去再调这里。
	// 没有副露时 14 张，有 M 组副露时 14−3M 张。
	Hand        []Tile
	Melds       []Meld
	WinningTile Tile

	// 局面标志，全部由房间层按当时的牌局状态填。
	IsTsumo   bool
	IsRiichi  bool
	IsIppatsu bool
	IsRinshan bool
	IsChankan bool
	IsHaitei  bool
	IsHoutei  bool
	IsTenhou  bool
	IsChiihou bool

	RoundWind Tile
	SeatWind  Tile

	DoraIndicators []Tile
	UraIndicators  []Tile // 里宝牌指示牌；只在 IsRiichi 时计入（§4）

	// Furiten 由房间层判定后传入，规则层拿不到舍牌与见逃历史（§2.2）。
	Furiten bool

	// 结算需要的事实，语义与 ScoreInput 的同名字段一致。
	WinnerSeat   int
	LoserSeat    int // 荣和的放铳者；自摸传 NoSeat
	DealerSeat   int // 本小局的庄家。seat 0 是起家，不是庄家
	Honba        int
	RiichiSticks int

	// PaoSeat 是帕奥责任者（§8.5）。**没有帕奥时必须显式传 NoSeat** ——
	// 0 是合法座位号，零值不等于「没有」；忘了填会让 0 号座平白付一个役满。
	// 与 ScoreInput.PaoSeat 同一约定。
	PaoSeat int
}

// WinResult 是一次和牌的判定结果。
type WinResult struct {
	Decomposition Decomposition // 高点法选中的那一种拆法
	Yaku          []YakuHit     // 命中的全部役；役满时只含役满役
	Dora          int           // 宝牌 + 里宝 + 赤宝；役满时为 0
	Han           int           // 役翻数 + Dora，不含場ゾロ；役满时为 0
	Fu            int           // 符数；役满时为 0（不参与计算）
	Yakuman       int           // 役满倍数，0 表示非役满
	PaoYakuman    int           // 被喂成的那个役满的倍数，不是总倍数（§8.5）
	Deltas        [4]int        // 四家收支，含本场与供托
}

// Win 走完一次和牌的 ①–⑨，返回拆法、役、翻符与四家收支。
//
// 三种失败都由包级哨兵区分：ErrNotWinningHand（牌型不对，或张数对不上）、
// ErrNoYaku（有型无役）、ErrFuriten（振听且是荣和）。
func Win(in WinInput, rules Rules) (WinResult, error) {
	counts, err := CountTiles(in.Hand)
	if err != nil {
		return WinResult{}, err
	}

	// 张数先对一遍。荣和时忘了把和牌张加进 Hand 是个很容易犯的错，那种错到下面
	// 会表现成「不是和牌型」——和真正的牌型不对长得一模一样，真正的原因被盖掉。
	if len(in.Hand)+3*len(in.Melds) != WinningHandSize {
		return WinResult{}, fmt.Errorf("%w: 暗牌 %d 张 + %d 组副露，凑不满 %d 张",
			ErrNotWinningHand, len(in.Hand), len(in.Melds), WinningHandSize)
	}
	if !in.WinningTile.Valid() {
		return WinResult{}, fmt.Errorf("%w: 非法的和牌张 %d", ErrNotWinningHand, in.WinningTile)
	}
	if counts[in.WinningTile.Kind()] == 0 {
		return WinResult{}, fmt.Errorf("%w: 和牌张 %s 不在暗牌里",
			ErrNotWinningHand, in.WinningTile)
	}

	// ③ 振听只限制荣和。
	if in.Furiten && !in.IsTsumo {
		return WinResult{}, ErrFuriten
	}

	// ① 和牌形。**全部**拆法都要留着：同一副牌可能既是七对子又是二杯口，
	// 两种点法分数不同，④ 就是在它们之间选。
	//
	// Hand 已经在上面过了一次 CountTiles，而 Decompose 的校验用的是同一个函数，
	// 所以它这里不可能再报错——写 `_` 比留一条永远走不到的错误分支更诚实。
	decomps, _ := Decompose(in.Hand)
	if len(decomps) == 0 {
		return WinResult{}, ErrNotWinningHand
	}

	ctx := YakuContext{
		WinningTile: in.WinningTile,
		Melds:       in.Melds,
		IsMenzen:    IsMenzen(in.Melds),
		IsTsumo:     in.IsTsumo,
		IsRiichi:    in.IsRiichi,
		IsIppatsu:   in.IsIppatsu,
		IsRinshan:   in.IsRinshan,
		IsChankan:   in.IsChankan,
		IsHaitei:    in.IsHaitei,
		IsHoutei:    in.IsHoutei,
		IsTenhou:    in.IsTenhou,
		IsChiihou:   in.IsChiihou,
		RoundWind:   in.RoundWind,
		SeatWind:    in.SeatWind,
	}

	// ⑥ 宝牌。它只认牌面、不认拆法，所以整手牌只算一次。
	//
	// AllTiles 必须收**原始牌**：Decomposition.Tiles() 是从 Counts 建出来的，
	// 赤宝牌在那里已经被归一成普通牌，拿它数赤牌会一张都数不到。
	all := AllTiles(in.Hand, in.Melds)
	dora := CountDora(all, in.DoraIndicators) + CountAka(all)
	if in.IsRiichi {
		// 里宝牌不立直不翻开（§4）。
		dora += CountDora(all, in.UraIndicators)
	}

	var best WinResult
	found := false

	for _, d := range decomps {
		// ⑤ 役。空表示这一种拆法无役——② 无役不能和，但别的拆法可能还有役，
		// 所以是 continue 而不是直接失败。
		hits := DetectYaku(d, ctx)
		if len(hits) == 0 {
			continue
		}

		r := WinResult{Decomposition: d, Yaku: hits}
		for _, h := range hits {
			r.Han += h.Han
			r.Yakuman += h.Yaku.Yakuman
		}
		if r.Yakuman == 0 {
			// ⑥ 宝牌加在役之后；⑦ 符在这里算。
			// 役满两条都跳过：役满不计宝牌，也不计符。
			r.Dora = dora
			r.Han += dora
			r.Fu = CalcFu(d, ctx)
		}

		if !found || higherValue(r, best, rules) {
			best, found = r, true
		}
	}
	if !found {
		return WinResult{}, ErrNoYaku
	}

	best.PaoYakuman = paoYakuman(best.Yaku, in.PaoSeat)

	// ⑧⑨ 基本点与四家收支（含本场与供托）。
	best.Deltas = CalcScore(ScoreInput{
		Han:     best.Han,
		Fu:      best.Fu,
		Yakuman: best.Yakuman,

		WinnerSeat: in.WinnerSeat,
		LoserSeat:  in.LoserSeat,
		DealerSeat: in.DealerSeat,
		IsTsumo:    in.IsTsumo,

		Honba:        in.Honba,
		RiichiSticks: in.RiichiSticks,

		PaoSeat:    in.PaoSeat,
		PaoYakuman: best.PaoYakuman,
	}, rules)

	return best, nil
}

// higherValue 是 ④ 的高点法：同一副牌有多种拆法时取**点数**最高的那一种。
//
// 比的是基本点，不是「先比翻再比符」——两者不等价：4 翻 25 符（1600）低于
// 3 翻 70 符（2240），按翻数先比会选错。基本点相同再比翻数（封顶之后这种很常见），
// 让结果唯一、可复现。
func higherValue(a, b WinResult, rules Rules) bool {
	pa := BasePoints(a.Han, a.Fu, a.Yakuman, rules)
	pb := BasePoints(b.Han, b.Fu, b.Yakuman, rules)
	if pa != pb {
		return pa > pb
	}
	if a.Han != b.Han {
		return a.Han > b.Han
	}
	return a.Fu > b.Fu
}

// paoYakuman 返回被喂成的那个役满的倍数；没有责任者、或命中的都不是那三种时返回 0。
//
// M-League 只对三种役满适用包（§8.5）：大三元、大四喜、四槓子。
// **谁是责任者不在这里判**——房间层在鸣牌那一刻就记下了（那时手牌还没变）。
// 这里只回答「命中的役满里有没有那三种」，以及被喂成的是哪一个。
func paoYakuman(hits []YakuHit, paoSeat int) int {
	if paoSeat == NoSeat {
		return 0
	}
	for _, h := range hits {
		switch h.Yaku {
		case yakuDaisangen, yakuDaisuushii, yakuSuukantsu:
			return h.Yaku.Yakuman
		}
	}
	return 0
}
