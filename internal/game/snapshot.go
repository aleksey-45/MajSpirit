package game

import "github.com/aleksey-45/majspirit/internal/protocol"

// snapshot.go —— game.sync：把**这个座位**现在该看到的一切发给他。
//
// 断线重连必须靠它：服务端是权威，客户端重连之后手上什么都没有。协议 §7.2 的
// 隐藏信息契约在这里有第二个落点 —— 快照里的 `myHand` 只有本人那 13 张，
// 别家的手牌一张都不许进字节。
//
// 只发**公开**信息和本人信息：
//
//	公开   牌河、四家的副露、分数、宝牌指示牌、谁立直了、台上供托、当前行动者
//	本人   自己的手牌、正在等自己回应时的选项
//
// 别人的手牌不在其中，也不需要"发完再让客户端藏起来"。

// Sync 是 game.sync：让服务端给这个座位重发一份完整快照。
//
// 它走指令通道而不是直接读状态：对局状态是单 goroutine 独占的，
// 从外面读就是数据竞争（`-race` 会当场抓住）。
func (g *Game) Sync(userID int64) error {
	return g.call(command{kind: cmdSync, id: userID})
}

func (g *Game) doSync(userID int64) error {
	seat := g.seatOf(userID)
	if seat < 0 {
		return ErrNotInGame
	}
	// 内容按座位现构造，而且**只发给本人** —— 快照里有手牌，
	// 广播出去就是把四家的牌都发了一遍。
	g.emitTo(seat, fact{typ: protocol.EventGameSnapshot, forSeat: func(viewer int) any {
		return g.snapshotFor(viewer)
	}})
	return nil
}

// snapshotFor 拼出 seat 视角的快照。纯函数：只读状态，不发消息。
func (g *Game) snapshotFor(seat int) gameSnapshotView {
	v := gameSnapshotView{
		GameID:       g.opt.GameID,
		RoundIndex:   g.round,
		HandNumber:   g.honba,
		DealerSeat:   g.dealer,
		MySeat:       seat,
		MyHand:       formatTiles(g.hands[seat].Tiles()),
		Scores:       g.scores,
		Riichi:       g.riichi,
		RiichiSticks: g.riichiSticks,
		Turn:         g.turn,
		Remaining:    g.wall.Remaining(),
		Discards:     [4][]string{},
		Melds:        [4][]snapshotMeldView{},
	}

	for i := range 4 {
		v.Discards[i] = formatTiles(g.discards[i])
		// 四家的副露都是公开信息：碰出来、杠出来的牌桌上看得见。
		v.Melds[i] = make([]snapshotMeldView, 0, len(g.melds[i]))
		for _, m := range g.melds[i] {
			v.Melds[i] = append(v.Melds[i], snapshotMeldView{
				Kind:   meldKindName(m.Kind),
				Tiles:  formatTiles(m.TilesUsed()),
				Closed: !m.IsOpen,
			})
		}
	}

	v.DoraIndicators = formatTiles(g.wall.DoraIndicators())

	// 正在等什么，以及**自己**能不能回应。
	if !g.deadline.IsZero() {
		v.DeadlineMs = g.deadline.UnixMilli()
	}
	if g.claim != nil {
		v.Claim = &snapshotClaim{
			Trigger:  g.claim.trigger,
			FromSeat: g.claim.from,
			Tile:     formatTile(g.claim.tile),
		}
		// 选项只给有资格的那个座位 —— 快照本身按座位构造，正好用得上。
		if opts, ok := g.claim.options[seat]; ok {
			v.Claim.MyOptions = opts
		}
	}
	// 没在等自己回应时，明确告诉客户端"该你打牌"还是"在等别人"。
	v.AwaitingMe = g.awaitingDiscard && g.turn == seat

	return v
}

// snapshotMeldView 是快照里的一个面子。带 `closed` 是因为暗杠的四张要盖着显示 ——
// 但**牌面本身仍然是公开的**（规则上暗杠要亮出两端，本项目的口径是全部公开）。
type snapshotMeldView struct {
	Kind   string   `json:"kind"`
	Tiles  []string `json:"tiles"`
	Closed bool     `json:"closed"`
}

type snapshotClaim struct {
	Trigger   string   `json:"trigger"`
	FromSeat  int      `json:"fromSeat"`
	Tile      string   `json:"tile"`
	MyOptions []string `json:"myOptions,omitempty"`
}

type gameSnapshotView struct {
	GameID         int64                 `json:"gameId"`
	RoundIndex     int                   `json:"roundIndex"`
	HandNumber     int                   `json:"handNumber"`
	DealerSeat     int                   `json:"dealerSeat"`
	MySeat         int                   `json:"mySeat"`
	MyHand         []string              `json:"myHand"`
	Melds          [4][]snapshotMeldView `json:"melds"`
	Discards       [4][]string           `json:"discards"`
	Scores         [4]int                `json:"scores"`
	Riichi         [4]bool               `json:"riichi"`
	RiichiSticks   int                   `json:"riichiSticks"`
	Turn           int                   `json:"turn"`
	AwaitingMe     bool                  `json:"awaitingMe"`
	DeadlineMs     int64                 `json:"deadlineMs,omitempty"`
	Remaining      int                   `json:"remaining"`
	DoraIndicators []string              `json:"doraIndicators"`
	Claim          *snapshotClaim        `json:"claim,omitempty"`
}
