package game

import "github.com/aleksey-45/majspirit/internal/mahjong"

// view.go —— 对局事件的 data 结构体（协议 §6.3~§6.9）。
//
// 字段名以协议文档为准，这里只是它的实现。**带隐藏信息的字段一律通过 `fact.forSeat`
// 按座位填**（`myHand`、`game.drew` 的 `tile`），不能走 `data` —— `data` 对四个座位
// 是同一份字节，放进去就是泄漏。协议 §7.2 那张核对表就是规格。

// gameStartedView 是 game.started：每个座位收到的 `myHand` 不同。
type gameStartedView struct {
	GameID       int64  `json:"gameId"`
	RoundIndex   int    `json:"roundIndex"`
	RoundWind    string `json:"roundWind"`
	HandNumber   int    `json:"handNumber"`
	DealerSeat   int    `json:"dealerSeat"`
	MySeat       int    `json:"mySeat"`
	MyHand       string `json:"myHand"`
	Scores       [4]int `json:"scores"`
	RiichiSticks int    `json:"riichiSticks"`
	Remaining    int    `json:"remaining"`
	FirstTurn    int    `json:"firstTurn"`
}

// gameDrewView 是 game.drew：`tile` 只出现在摸牌者本人收到的那一份里。
//
// omitempty 在这里是**语义**，不是省字节：别家的版本根本没有这个字段，
// 而不是"tile 是空字符串"。
type gameDrewView struct {
	Seat      int    `json:"seat"`
	Tile      string `json:"tile,omitempty"`
	Remaining int    `json:"remaining"`
	// Rinshan 标记这张是杠后从岭上摸的 —— 客户端据此区分"摸牌"与"岭上摸牌"。
	Rinshan bool `json:"rinshan,omitempty"`
}

type gameDiscardedView struct {
	Seat      int    `json:"seat"`
	Tile      string `json:"tile"`
	Tsumogiri bool   `json:"tsumogiri"`
	Riichi    bool   `json:"riichi"`
	Remaining int    `json:"remaining"`
}

type gameTurnView struct {
	Seat       int   `json:"seat"`
	DeadlineMs int64 `json:"deadlineMs"`
}

type gameWinDeclaredView struct {
	WinnerSeat  int    `json:"winnerSeat"`
	WinningTile string `json:"winningTile"`
	IsTsumo     bool   `json:"isTsumo"`
}

// gameClaimWindowView 只推给**有资格回应**的座位（协议 §6.6 / §7.3 第 2 条）。
//
// 窗口内容本身四家一样，所以走 `data` 而不是 `forSeat` —— 被裁的是**收件人名单**，
// 不是内容。名单本身就是隐私："某人收到了 options"这件事暴露了他可能有和牌机会。
type gameClaimWindowView struct {
	Trigger    string   `json:"trigger"` // discard / kan
	FromSeat   int      `json:"fromSeat"`
	Tile       string   `json:"tile"`
	Options    []string `json:"options"`
	DeadlineMs int64    `json:"deadlineMs"`
}

// gameClaimClosedView 窗口关掉了。`resolvedBy` 为 null 表示四家都过（或超时）。
//
// 没人鸣时 `seat` 没有意义，取值 -1。
type gameClaimClosedView struct {
	Trigger    string `json:"trigger"`
	ResolvedBy any    `json:"resolvedBy"`
	Seat       int    `json:"seat"`
}

// gameRoundEndView 是一局结束。和了与流局共用它，靠 `endedBy` 区分。
type gameRoundEndView struct {
	RoundIndex    int              `json:"roundIndex"`
	EndedBy       string           `json:"endedBy"` // win / draw —— 没有 abortive
	Wins          []roundWinView   `json:"wins"`    // 一局只可能有 0 或 1 家（头跳）
	Deltas        [4]int           `json:"deltas"`
	NewScores     [4]int           `json:"newScores"`
	RiichiSticks  int              `json:"riichiSticks"`
	DealerRepeats bool             `json:"dealerRepeats"`
	TenpaiSeats   []int            `json:"tenpaiSeats"`
	NotenSeats    []int            `json:"notenSeats"`
	TenpaiHands   []tenpaiHandView `json:"tenpaiHands,omitempty"`
}

// roundWinView 是一次和了的明细。`loserSeat` 自摸时为 null。
//
// 用指针而不是哨兵 -1：协议里自摸是 `null`，而 -1 会被前端当成一个座位号。
type roundWinView struct {
	WinnerSeat  int            `json:"winnerSeat"`
	LoserSeat   *int           `json:"loserSeat"`
	IsTsumo     bool           `json:"isTsumo"`
	WinningTile string         `json:"winningTile"`
	Han         int            `json:"han"`
	Fu          int            `json:"fu"`
	Yakuman     int            `json:"yakuman"`
	Yaku        []yakuView     `json:"yaku"`
	Dora        doraView       `json:"dora"`
	Payments    map[string]int `json:"payments"`
	// PaoSeat / PaoYaku 是帕奥责任（规则集 §8.5）。非 null 时 payments 里会出现
	// **某一座独自承担全额**的大额支出 —— 前端要能解释这个数字，别当成 bug。
	PaoSeat *int   `json:"paoSeat"`
	PaoYaku string `json:"paoYaku"`
}

type yakuView struct {
	Name string `json:"name"`
	Han  int    `json:"han"`
}

type doraView struct {
	Dora int `json:"dora"`
	Ura  int `json:"ura"`
	Aka  int `json:"aka"`
}

// tenpaiHandView 只在荒牌流局时揭示，且只揭示**听牌家**的手牌（协议 §7.2）。
type tenpaiHandView struct {
	Seat    int      `json:"seat"`
	Hand    string   `json:"hand"`
	Waiting []string `json:"waiting"`
}

type gameGameEndView struct {
	GameID      int64  `json:"gameId"`
	FinalScores [4]int `json:"finalScores"`
	Placements  [4]int `json:"placements"`
	Points      [4]int `json:"points"`
	Reason      string `json:"reason"`
}

// roundWinView 把和了的结果整理成协议要的形状。
func (g *Game) roundWinView(winner, loser int, tile mahjong.Tile, tsumo bool, res mahjong.WinResult) roundWinView {
	view := roundWinView{
		WinnerSeat:  winner,
		IsTsumo:     tsumo,
		WinningTile: formatTile(tile),
		Han:         res.Han,
		Fu:          res.Fu,
		Yakuman:     res.Yakuman,
		Dora:        doraView{Dora: res.Dora},
		Payments:    map[string]int{},
	}
	if !tsumo {
		view.LoserSeat = &loser
	}
	if p := g.pao[winner]; p != nil {
		view.PaoSeat = &p.seat
		view.PaoYaku = p.yaku
	}
	for _, h := range res.Yaku {
		view.Yaku = append(view.Yaku, yakuView{Name: h.Yaku.Name, Han: h.Han})
	}
	for seat, d := range res.Deltas {
		if d != 0 {
			view.Payments[seatName(seat)] = d
		}
	}
	return view
}

// roundEndView 拼一局结束的载荷。流局时额外揭示**听牌家**的手牌（协议 §7.2）。
func (g *Game) roundEndView(endedBy string, wins []roundWinView, deltas [4]int,
	dealerRepeats bool, tenpai [4]bool) gameRoundEndView {

	view := gameRoundEndView{
		RoundIndex:    g.round,
		EndedBy:       endedBy,
		Wins:          wins,
		Deltas:        deltas,
		NewScores:     g.scores,
		RiichiSticks:  g.riichiSticks,
		DealerRepeats: dealerRepeats,
	}
	if endedBy != "draw" {
		return view
	}
	for seat, ok := range tenpai {
		if ok {
			view.TenpaiSeats = append(view.TenpaiSeats, seat)
			view.TenpaiHands = append(view.TenpaiHands, tenpaiHandView{
				Seat:    seat,
				Hand:    mahjong.FormatTiles(g.hands[seat].Tiles()),
				Waiting: formatTiles(mahjong.Waits(g.hands[seat].Tiles())),
			})
			continue
		}
		view.NotenSeats = append(view.NotenSeats, seat)
	}
	return view
}

// formatTile 把一张牌写成紧凑记法（赤五写作 0）。
func formatTile(t mahjong.Tile) string { return mahjong.FormatTiles([]mahjong.Tile{t}) }

func formatTiles(ts []mahjong.Tile) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = formatTile(t)
	}
	return out
}

// seatName 是 payments 这类"按座位索引的对象"的键名。
func seatName(seat int) string { return string(rune('0' + seat)) }

// gameMeldView 是一次鸣牌。`tiles` 是**组成面子的那几张**（含从手里出的），
// `taken` 是从谁那里拿到的那一张 —— 前端要把拿来的那张横过来摆。
type gameMeldView struct {
	Seat     int      `json:"seat"`
	Kind     string   `json:"kind"` // chi / pon / kan
	Tiles    []string `json:"tiles"`
	FromSeat int      `json:"fromSeat"`
	Taken    string   `json:"taken"`
	// Closed 只对暗杠为 true —— 前端据此决定那四张是盖着还是亮着。
	Closed bool `json:"closed"`
}

// gameDoraView 是新翻开的宝牌指示牌。它是**公开**信息（杠了大家都看得到），走广播。
//
// 里宝指示牌不走这条事件 —— 它只在立直且和牌时随结算揭示，提前发等于漏牌。
type gameDoraView struct {
	Kind      string `json:"kind"` // dora / ura
	Indicator string `json:"indicator"`
	Count     int    `json:"count"`
}

// gameRiichiView 是立直宣言。`tile` 是宣言牌 —— 别人据此知道他要打哪一张。
type gameRiichiView struct {
	Seat int    `json:"seat"`
	Tile string `json:"tile"`
}

// gameRiichiStickView 是供托变化。`sticks` 是台上总数，和牌者一人收走。
type gameRiichiStickView struct {
	Sticks   int  `json:"sticks"`
	NewStick bool `json:"newStick"`
	Seat     int  `json:"seat"`
}
