package game

import (
	"time"

	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

// kan.go —— 杠：明杠、加杠、暗杠，以及岭上摸牌与抢杠。
//
// 三种杠的**入口不同**，出口相同：
//
//	明杠  别人打出的牌 + 手里三张。走鸣牌窗口（选项里有 "kan"）。
//	加杠  自己碰过的牌又摸到第四张。走**抢杠窗口** —— 别人可以荣这张牌。
//	暗杠  自己摸到第四张。不经过任何窗口（别人看不到，也没什么可鸣的）。
//
// 出口都是"从岭上摸一张，然后打一张"。杠数封顶 `mahjong.MaxKans`（四个）——
// 这是规则上限，不是牌山的：岭上牌摸得完不完是另一回事（见 wall.go）。

// kanKind 是杠的三种形态，取值与协议 §5.3 的 `game.kan.type` 一致。
const (
	kanOpen   = "open"
	kanAdded  = "added"
	kanClosed = "closed"
)

// doKan 处理 game.kan。
//
// 明杠**不走这里**：它是对别人弃牌的回应，走鸣牌窗口的 "kan" 选项。这里只处理
// 轮到自己时能做的两种 —— 加杠与暗杠。
func (g *Game) doKan(userID int64, kind string, tile mahjong.Tile) error {
	seat := g.seatOf(userID)
	if seat < 0 {
		return ErrNotInGame
	}
	if kind == kanOpen {
		return ErrNoClaim // 明杠只能在鸣牌窗口里宣告
	}
	if seat != g.turn || !g.awaitingDiscard {
		return ErrNotYourTurn
	}
	if g.kanCount(seat) >= mahjong.MaxKans {
		return ErrMaxKan
	}

	switch kind {
	case kanClosed:
		if !sameKindIn(mahjong.ClosedKanTiles(g.hands[seat].Tiles()), tile) {
			return ErrNotHolding
		}
		g.applyClosedKan(seat, tile)
		g.afterKan(seat)

	case kanAdded:
		if !sameKindIn(mahjong.AddedKanTiles(g.hands[seat].Tiles(), g.melds[seat]), tile) {
			return ErrNotHolding
		}
		// 加杠先开**抢杠窗口**：别人可以荣这一张。没人荣才真的杠成。
		g.openChankanWindow(seat, tile)

	default:
		return ErrNotHolding
	}
	return nil
}

// kanCount 返回这个座位已经杠了几次（明杠、加杠、暗杠都算）。
func (g *Game) kanCount(seat int) int {
	n := 0
	for _, m := range g.melds[seat] {
		if m.Kind == mahjong.MeldKan {
			n++
		}
	}
	return n
}

// applyClosedKan 暗杠：手里四张收进副露，IsOpen 为 false。
func (g *Game) applyClosedKan(seat int, tile mahjong.Tile) {
	src := g.claimSource(seat, tile)
	for range 4 {
		g.hands[seat].Remove(src)
	}
	g.melds[seat] = append(g.melds[seat], mahjong.Meld{
		Kind: mahjong.MeldKan, IsOpen: false,
		Tiles: [4]mahjong.Tile{src, src, src, src},
	})
	g.broadcast(fact{typ: protocol.EventGameMeld, data: gameMeldView{
		Seat: seat, Kind: "kan", Closed: true,
		Tiles:    formatTiles([]mahjong.Tile{src, src, src, src}),
		FromSeat: seat, Taken: formatTile(src),
	}})
	g.log.Info("暗杠", "seat", seat, "tile", formatTile(src))
}

// applyAddedKan 加杠：把手里的第四张加到已有的碰上面。
//
// 副露里那个面子必须**就地升级**成杠，不能新加一个 —— 新加的话，同一个牌就有
// 一个碰和一个杠两个面子，算番时会重复计。
func (g *Game) applyAddedKan(seat int, tile mahjong.Tile) {
	src := g.claimSource(seat, tile)
	g.hands[seat].Remove(src)

	for i, m := range g.melds[seat] {
		if m.Kind == mahjong.MeldTriplet && m.Tile().Kind() == tile.Kind() {
			up := m
			up.Kind = mahjong.MeldKan
			up.Tiles[3] = src
			g.melds[seat][i] = up
			g.broadcast(fact{typ: protocol.EventGameMeld, data: gameMeldView{
				Seat: seat, Kind: "kan", Tiles: formatTiles(up.TilesUsed()),
				FromSeat: seat, Taken: formatTile(src),
			}})
			g.log.Info("加杠", "seat", seat, "tile", formatTile(src))
			return
		}
	}
}

// applyOpenKan 明杠：手里三张 + 别人打出的那一张。
func (g *Game) applyOpenKan(seat int, tile mahjong.Tile, from int) {
	src := g.claimSource(seat, tile)
	for range 3 {
		g.hands[seat].Remove(src)
	}
	m := mahjong.Meld{Kind: mahjong.MeldKan, IsOpen: true,
		Tiles: [4]mahjong.Tile{src, src, src, tile}}
	g.growMelds(seat, m, from, tile)
}

// openChankanWindow 加杠时的抢杠窗口：**只给能荣和这一张的座位**发，选项只有 ron/pass。
//
// 抢杠是加杠独有的风险：杠的那张牌从"已经碰掉的副露"里出来，别人本来不能荣它
// （碰完就过去了），只有加杠这一瞬间能抢。所以这个窗口不能复用普通鸣牌窗口的选项。
func (g *Game) openChankanWindow(seat int, tile mahjong.Tile) {
	c := &claim{
		from:     seat,
		tile:     tile,
		trigger:  "kan",
		kanSeat:  seat,
		options:  map[int][]string{},
		answered: map[int]bool{},
		ron:      map[int]bool{},
		pon:      map[int]bool{},
		chi:      map[int][2]mahjong.Tile{},
	}
	for offset := 1; offset < 4; offset++ {
		other := (seat + offset) % 4
		if g.canRon(other, tile) {
			c.eligible = append(c.eligible, other)
			c.options[other] = []string{"ron", "pass"}
		}
	}

	if len(c.eligible) == 0 {
		g.completeAddedKan(seat, tile)
		return
	}

	c.deadline = time.Now().Add(g.opt.ClaimLimit)
	g.claim = c
	g.deadline = c.deadline
	for _, other := range c.eligible {
		g.emitTo(other, fact{typ: protocol.EventGameClaimWindow, data: gameClaimWindowView{
			Trigger:    "kan",
			FromSeat:   seat,
			Tile:       formatTile(tile),
			Options:    c.options[other],
			DeadlineMs: c.deadline.UnixMilli(),
		}})
	}
}

// completeAddedKan 抢杠窗口里没人抢：杠成，然后摸岭上。
func (g *Game) completeAddedKan(seat int, tile mahjong.Tile) {
	g.applyAddedKan(seat, tile)
	g.afterKan(seat)
}

// afterKan 杠的公共出口：摸一张岭上牌，然后等打牌。
//
// **不换人**：杠是自己回合里的动作，摸完岭上还是自己打。
func (g *Game) afterKan(seat int) {
	g.turn = seat
	g.drawRinshan()
}

// drawRinshan 摸岭上牌。与普通摸牌的区别只有两点：牌从王牌来、盖 `IsRinshan` 标记
// （岭上开花那个役就看它）。
func (g *Game) drawRinshan() {
	seat := g.turn
	t, ok := g.wall.DrawRinshan()
	if !ok {
		// 岭上牌每次杠都会从牌山末尾补足，摸不出来说明牌山被改坏了。
		g.log.Error("岭上摸牌失败", "seat", seat)
		return
	}

	g.lastDrawn = t
	g.hands[seat].Add(t)
	g.drawn = true
	g.awaitingDiscard = true
	g.rinshanDraw = true

	remaining := g.wall.Remaining()
	g.broadcast(fact{typ: protocol.EventGameDrew, forSeat: func(viewer int) any {
		v := gameDrewView{Seat: seat, Remaining: remaining, Rinshan: true}
		if viewer == seat {
			v.Tile = formatTile(t)
		}
		return v
	}})

	// 杠一次多翻一张宝牌指示牌。这条是**公开**信息，所以走 data 广播。
	inds := g.wall.DoraIndicators()
	g.broadcast(fact{typ: protocol.EventGameDoraRevealed, data: gameDoraView{
		Kind:      "dora",
		Indicator: formatTile(inds[len(inds)-1]),
		Count:     len(inds),
	}})

	g.deadline = time.Now().Add(g.opt.TurnLimit)
	g.broadcast(fact{typ: protocol.EventGameTurn, data: gameTurnView{
		Seat:       seat,
		DeadlineMs: time.Now().Add(g.opt.TurnBase).UnixMilli(),
	}})
}
