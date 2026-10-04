package mahjong

// yaku.go —— 役种判定的对外接口：类型、役种表、DetectYaku。
// 役种定义。规则与翻数一律以 docs/规则集.md §3 为准。
//
// HanOpen 为 -1 表示**门清限定**——副露后完全不成立，不是降翻。
//
// M-League 与多数平台的差异已写进下面的表：单役种**没有双倍役满**
// （四暗刻单骑、国士十三面、纯正九莲、大四喜、四槓子都是 1 倍）；
// 没有流し満貫与人和；绿一色不要求含发。
type Yaku struct {
	Name    string // 役种名称
	Han     int    // 门清时的翻数
	HanOpen int    // 副露时的翻数；-1 表示门清限定
	Yakuman int    // 役满倍数，0 表示非役满
}

// YakuHit 是一次判定命中的役。
//
// 役满役的 Han 恒为 0——役满时翻数不参与计算，倍数看 Yaku.Yakuman。
type YakuHit struct {
	Yaku Yaku
	Han  int
}

// YakuContext 是判定役种所需的全部外部信息。
type YakuContext struct {
	WinningTile Tile   // 和牌的那张。平和需要它判两面听；三暗刻/四暗刻需要它判明暗
	Melds       []Meld // **已副露**的面子（含杠）。暗牌拆出来的面子在 Decomposition 里

	IsMenzen  bool // 门清（**暗杠不影响门清**）
	IsTsumo   bool // 自摸
	IsRiichi  bool // 立直
	IsIppatsu bool // 一发
	IsRinshan bool // 岭上开花
	IsChankan bool // 抢杠
	IsHaitei  bool // 海底摸月
	IsHoutei  bool // 河底捞鱼
	IsTenhou  bool // 天和（庄家配牌即和）
	IsChiihou bool // 地和（子家第一巡自摸）

	RoundWind Tile // 场风
	SeatWind  Tile // 自风
}

// ── 役种表 ──────────────────────────────────────────────────────────────

var (
	yakuRiichi       = Yaku{Name: "立直", Han: 1, HanOpen: -1}
	yakuIppatsu      = Yaku{Name: "一发", Han: 1, HanOpen: -1}
	yakuMenzenTsumo  = Yaku{Name: "门前清自摸和", Han: 1, HanOpen: -1}
	yakuPinfu        = Yaku{Name: "平和", Han: 1, HanOpen: -1}
	yakuTanyao       = Yaku{Name: "断幺九", Han: 1, HanOpen: 1}
	yakuIipeikou     = Yaku{Name: "一杯口", Han: 1, HanOpen: -1}
	yakuYakuhaiWhite = Yaku{Name: "役牌·白", Han: 1, HanOpen: 1}
	yakuYakuhaiGreen = Yaku{Name: "役牌·发", Han: 1, HanOpen: 1}
	yakuYakuhaiRed   = Yaku{Name: "役牌·中", Han: 1, HanOpen: 1}
	yakuYakuhaiRound = Yaku{Name: "役牌·场风", Han: 1, HanOpen: 1}
	yakuYakuhaiSeat  = Yaku{Name: "役牌·自风", Han: 1, HanOpen: 1}
	yakuRinshan      = Yaku{Name: "岭上开花", Han: 1, HanOpen: 1}
	yakuChankan      = Yaku{Name: "抢杠", Han: 1, HanOpen: 1}
	yakuHaitei       = Yaku{Name: "海底摸月", Han: 1, HanOpen: 1}
	yakuHoutei       = Yaku{Name: "河底捞鱼", Han: 1, HanOpen: 1}

	yakuSanshokuDoujun = Yaku{Name: "三色同顺", Han: 2, HanOpen: 1}
	yakuIkkitsuukan    = Yaku{Name: "一气通贯", Han: 2, HanOpen: 1}
	yakuChanta         = Yaku{Name: "混全带幺九", Han: 2, HanOpen: 1}
	yakuChiitoitsu     = Yaku{Name: "七对子", Han: 2, HanOpen: -1}
	yakuToitoi         = Yaku{Name: "对对和", Han: 2, HanOpen: 2}
	yakuSanankou       = Yaku{Name: "三暗刻", Han: 2, HanOpen: 2}
	yakuSankantsu      = Yaku{Name: "三杠子", Han: 2, HanOpen: 2}
	yakuSanshokuDoukou = Yaku{Name: "三色同刻", Han: 2, HanOpen: 2}
	yakuHonroutou      = Yaku{Name: "混老头", Han: 2, HanOpen: 2}
	yakuShousangen     = Yaku{Name: "小三元", Han: 2, HanOpen: 2}

	yakuRyanpeikou = Yaku{Name: "二杯口", Han: 3, HanOpen: -1}
	yakuJunchan    = Yaku{Name: "纯全带幺九", Han: 3, HanOpen: 2}
	yakuHonitsu    = Yaku{Name: "混一色", Han: 3, HanOpen: 2}
	yakuChinitsu   = Yaku{Name: "清一色", Han: 6, HanOpen: 5}

	yakuKokushi     = Yaku{Name: "国士无双", Yakuman: 1}
	yakuSuuankou    = Yaku{Name: "四暗刻", Yakuman: 1}
	yakuDaisangen   = Yaku{Name: "大三元", Yakuman: 1}
	yakuShousuushii = Yaku{Name: "小四喜", Yakuman: 1}
	yakuDaisuushii  = Yaku{Name: "大四喜", Yakuman: 1}
	yakuTsuuiisou   = Yaku{Name: "字一色", Yakuman: 1}
	yakuRyuuiisou   = Yaku{Name: "绿一色", Yakuman: 1}
	yakuChinroutou  = Yaku{Name: "清老头", Yakuman: 1}
	yakuChuuren     = Yaku{Name: "九莲宝灯", Yakuman: 1}
	yakuSuukantsu   = Yaku{Name: "四杠子", Yakuman: 1}
	yakuTenhou      = Yaku{Name: "天和", Yakuman: 1}
	yakuChiihou     = Yaku{Name: "地和", Yakuman: 1}
)

// han 返回这个役在当前门清/副露状态下的实际翻数；0 表示不成立。
func (y Yaku) han(menzen bool) int {
	if y.HanOpen < 0 { // 门清限定
		if !menzen {
			return 0
		}
		return y.Han
	}
	if menzen {
		return y.Han
	}
	return y.HanOpen
}

// ── 判定入口 ────────────────────────────────────────────────────────────

// normalized 返回把赤宝牌归一成普通牌之后的 context。
//
// **规则判定只认种类**：赤五万必须表现得和五万一模一样，否则「碰了赤五万」会在
// 三色同顺、役牌、三暗刻这些地方判错。但调用方（房间层）还要保留赤属性来数赤宝牌
// 翻数，所以归一**只在规则层内部做**——`YakuContext` 是按值传递的，
// `DetectYaku` / `CalcFu` 改的是自己的副本，调用方拿到的仍是原始牌。
//
// 放在这两个入口而不是让调用方自觉，是因为漏掉的症状是**静默**的：
// 荣和赤五万补成刻子时少算一组明刻符，多算一组暗刻符，还顺便让三暗刻错误成立。
// 赤宝牌好坑啊
func (ctx YakuContext) normalized() YakuContext {
	ctx.WinningTile = ctx.WinningTile.Kind()
	if len(ctx.Melds) == 0 {
		return ctx
	}
	melds := make([]Meld, len(ctx.Melds))
	copy(melds, ctx.Melds)
	for i := range melds {
		for j := range melds[i].Tiles {
			melds[i].Tiles[j] = melds[i].Tiles[j].Kind()
		}
	}
	ctx.Melds = melds
	return ctx
}

// DetectYaku 返回**命中的全部役种**，而不是最高分的那个——多个役可以叠加，总翻数是求和。
//
// 返回空切片表示**无役**，上层据此判定「不能和牌」。
//
// 两条容易搞错的规则：
//   - **宝牌不在这里。** 宝牌不是役，在点数换算时才加。放进来会导致「只有宝牌也能和牌」。
//   - **役满优先。** 只要命中役满，就不再返回普通役。
func DetectYaku(d Decomposition, ctx YakuContext) []YakuHit {
	h := newHand(d, ctx.normalized())

	if hits := h.yakuman(); len(hits) > 0 {
		return hits
	}

	var hits []YakuHit
	h.detectOne(&hits)
	h.detectTwo(&hits)
	h.detectGeqThree(&hits)
	return hits
}
