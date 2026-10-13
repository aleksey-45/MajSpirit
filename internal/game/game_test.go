package game

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

// game_test.go —— 一整场对局。四个"玩家"就是四个 Player，不碰网络。

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

type fakeNotifier struct {
	mu  sync.Mutex
	got map[int64][]protocol.Envelope
}

func newFakeNotifier() *fakeNotifier {
	return &fakeNotifier{got: make(map[int64][]protocol.Envelope)}
}

func (f *fakeNotifier) Send(userID int64, msg protocol.Envelope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := msg
	cp.Data = append(json.RawMessage(nil), msg.Data...)
	f.got[userID] = append(f.got[userID], cp)
	return nil
}

func (f *fakeNotifier) all(userID int64) []protocol.Envelope {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.Envelope(nil), f.got[userID]...)
}

func (f *fakeNotifier) count(userID int64, typ protocol.Command) int {
	n := 0
	for _, e := range f.all(userID) {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// find 取该用户收到的**最后一条**指定类型的事件，解码成 map 便于断言字段。
func (f *fakeNotifier) find(userID int64, typ protocol.Command) (map[string]any, bool) {
	list := f.all(userID)
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Type != typ {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(list[i].Data, &m); err != nil {
			return nil, false
		}
		return m, true
	}
	return nil, false
}

func (f *fakeNotifier) raws(userID int64, typ protocol.Command) []string {
	var out []string
	for _, e := range f.all(userID) {
		if e.Type == typ {
			out = append(out, string(e.Data))
		}
	}
	return out
}

// userOf 返回座位对应的 userID。测试里四个座位就是 1..4 号用户。
func userOf(seat int) int64 { return int64(seat + 1) }

func players() [4]Player {
	var p [4]Player
	for i := range p {
		p[i] = Player{UserID: userOf(i), Username: "玩家"}
	}
	return p
}

// newTestGame 起一场。opt 里没填的走默认，测试一律把等待调短。
func newTestGame(t *testing.T, opt Options) (*Game, *fakeNotifier) {
	t.Helper()
	n := newFakeNotifier()
	if opt.TurnBase == 0 {
		opt.TurnBase = time.Second
	}
	if opt.TurnLimit == 0 {
		opt.TurnLimit = time.Second
	}
	g := New(players(), n, opt, nil)
	t.Cleanup(g.Close)
	return g, n
}

// 开局后每个人只该看到**自己的** 13 张。
//
// 断言的是序列化后的**字节**，不是结构体字段 —— 泄漏可能来自任何地方
// （紧凑记法字符串、明牌数组、甚至日志字段），只看字段会漏。
func TestStartedHidesOtherHands(t *testing.T) {
	_, n := newTestGame(t, Options{})

	hands := map[int]string{}
	for seat := range 4 {
		m, ok := n.find(userOf(seat), protocol.EventGameStarted)
		if !ok {
			t.Fatalf("座位 %d 没收到 game.started", seat)
		}
		if m["mySeat"] != float64(seat) {
			t.Errorf("座位 %d 收到的 mySeat = %v", seat, m["mySeat"])
		}
		hand, _ := m["myHand"].(string)
		if hand == "" {
			t.Fatalf("座位 %d 没拿到 myHand", seat)
		}
		hands[seat] = hand
	}

	for a := range 4 {
		for b := range 4 {
			if a != b && hands[a] == hands[b] {
				t.Fatalf("座位 %d 与 %d 拿到同一手牌 %s", a, b, hands[a])
			}
		}
	}

	for viewer := range 4 {
		for seat, hand := range hands {
			if seat == viewer {
				continue
			}
			for _, raw := range n.raws(userOf(viewer), protocol.EventGameStarted) {
				// 先挖掉自己那份，否则"别家手牌恰好是自己手牌的子串"会误报。
				rest := strings.Replace(raw, hands[viewer], "", 1)
				if strings.Contains(rest, hand) {
					t.Fatalf("座位 %d 收到的 game.started 里出现了座位 %d 的手牌 %s\n%s",
						viewer, seat, hand, raw)
				}
			}
		}
	}
}

// 摸到的牌只给摸牌者本人：别家只知道"他摸了一张"，不知道是什么。
func TestDrewTileOnlyGoesToDrawer(t *testing.T) {
	g, n := newTestGame(t, Options{})

	// 走几步（摸切），让每个人都摸过牌，才有得比。
	for range 8 {
		if !discardDrawn(t, g, n) {
			break
		}
	}

	for seat := range 4 {
		sawOwn := false
		for _, raw := range n.raws(userOf(seat), protocol.EventGameDrew) {
			var m map[string]any
			if err := json.Unmarshal([]byte(raw), &m); err != nil {
				t.Fatal(err)
			}
			drawer := int(m["seat"].(float64))
			_, hasTile := m["tile"]

			// 别家的摸牌事件**照样要发**给所有人（不然他们不知道牌局动了），
			// 但里面不能有 tile —— 那才是隐藏信息。
			if drawer != seat && hasTile {
				t.Fatalf("座位 %d 看到了座位 %d 摸到的牌: %s", seat, drawer, raw)
			}
			if drawer == seat && hasTile {
				sawOwn = true
			}
		}
		if !sawOwn {
			t.Errorf("座位 %d 没拿到自己摸的牌", seat)
		}
	}
}

// 回合超时要自动摸切往下走 —— "每个等待都必须有超时"的现场验证。
func TestTurnTimeoutKeepsGameMoving(t *testing.T) {
	_, n := newTestGame(t, Options{TurnBase: 30 * time.Millisecond, TurnLimit: 30 * time.Millisecond})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if n.count(userOf(0), protocol.EventGameDiscarded) >= 3 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := n.count(userOf(0), protocol.EventGameDiscarded); got < 3 {
		t.Fatalf("超时没有把牌局推下去，只打出 %d 张", got)
	}
	m, _ := n.find(userOf(0), protocol.EventGameDiscarded)
	if m["tsumogiri"] != true {
		t.Errorf("超时应当摸切, 实际 tsumogiri = %v", m["tsumogiri"])
	}
}

// 荒牌流局：牌山摸空 → 听牌料 → roundEnd。顺带验牌山是逐张递减到 0 的。
func TestExhaustionDraw(t *testing.T) {
	g, n := newTestGame(t, Options{Rounds: 1})

	for range 200 {
		if _, ok := n.find(userOf(0), protocol.EventGameRoundEnd); ok {
			break
		}
		if !discardDrawn(t, g, n) {
			break
		}
	}

	m, ok := n.find(userOf(0), protocol.EventGameRoundEnd)
	if !ok {
		t.Fatal("牌山摸完了却没结束")
	}
	if m["endedBy"] != "draw" {
		t.Fatalf("endedBy = %v, 期望 draw", m["endedBy"])
	}

	// 发牌后剩 84 张，所以正好摸 84 次、打 84 张；第 85 次摸牌时牌山已空。
	if got := n.count(userOf(0), protocol.EventGameDiscarded); got != 84 {
		t.Errorf("打出了 %d 张, 期望 84（136 − 52）", got)
	}

	var sum int
	for _, d := range m["deltas"].([]any) {
		sum += int(d.(float64))
	}
	if sum != 0 {
		t.Errorf("流局不该有净进出，deltas 之和 = %d", sum)
	}
	if _, ok := m["tenpaiSeats"]; !ok {
		t.Error("流局必须报出听牌情况，否则没人知道听牌料怎么分的")
	}
}

// 自摸和了：摆一副确定的牌，庄家一开局就摸到和牌张。
//
// 用 rigWall 摆牌而不是搜种子 —— 搜出来的种子会随洗牌算法的任何改动一起碎，
// 而且碎的时候看不出是"摆牌变了"还是"和牌逻辑坏了"。
func TestTsumoWinAndPlacements(t *testing.T) {
	hands := [4][]mahjong.Tile{
		// 庄家 13 张，摸 2s 成和：123m456m789p234s55p。
		// 平和自摸固定 20 符（不加那 2 符），是全套符数里唯一的特例，值得挑它。
		mahjong.MustParseTiles("123m456m789p55p34s"),
		mahjong.MustParseTiles("111222333444z6z"),
		mahjong.MustParseTiles("777788889999m2z"),
		mahjong.MustParseTiles("1111s22223333p7z"),
	}
	ended := make(chan struct{}, 1)
	g, n := newTestGame(t, Options{
		Rounds: 1,
		Wall:   rigWall(t, hands, mahjong.MustParseTiles("2s")[0]),
		OnEnd:  func() { ended <- struct{}{} },
	})

	// 庄家一摸到手就能自摸（123m456m789p55p234s：平和 + 门前清自摸和）。
	if err := g.DeclareWin(userOf(0)); err != nil {
		t.Fatalf("自摸被拒: %v", err)
	}

	declared, ok := n.find(userOf(0), protocol.EventGameWinDeclared)
	if !ok {
		t.Fatal("没有 game.winDeclared")
	}
	if declared["isTsumo"] != true || declared["winnerSeat"] != float64(0) {
		t.Errorf("winDeclared = %v", declared)
	}

	end, ok := n.find(userOf(0), protocol.EventGameRoundEnd)
	if !ok {
		t.Fatal("没有 game.roundEnd")
	}
	if end["endedBy"] != "win" {
		t.Fatalf("endedBy = %v, 期望 win", end["endedBy"])
	}
	wins := end["wins"].([]any)
	if len(wins) != 1 {
		t.Fatalf("一局只可能有 1 家和牌（头跳），实际 %d", len(wins))
	}
	w := wins[0].(map[string]any)
	if w["winnerSeat"] != float64(0) || w["isTsumo"] != true {
		t.Errorf("win = %v", w)
	}
	// 平和 1 + 门前清自摸和 1 = 2 翻；**平和自摸固定 20 符**，不另加那 2 符。
	if w["han"] != float64(2) || w["fu"] != float64(20) {
		t.Errorf("翻符 = %v 翻 %v 符, 期望 2 翻 20 符", w["han"], w["fu"])
	}
	if w["loserSeat"] != nil {
		t.Errorf("自摸的 loserSeat 应当是 null, 实际 %v", w["loserSeat"])
	}

	// 庄家自摸 2 翻 20 符：基本点 320，三家各付 700。
	want := []float64{27100, 24300, 24300, 24300}
	for i, sc := range end["newScores"].([]any) {
		if sc != want[i] {
			t.Errorf("最终分 = %v, 期望 %v", end["newScores"], want)
			break
		}
	}

	// 一小局打完就是整场结束：顺位、ポイント、OnEnd 回调。
	ge, ok := n.find(userOf(0), protocol.EventGameGameEnd)
	if !ok {
		t.Fatal("没有 game.gameEnd")
	}
	if ge["placements"].([]any)[0] != float64(1) {
		t.Errorf("庄家第一，placements = %v", ge["placements"])
	}
	var sum float64
	for _, p := range ge["points"].([]any) {
		sum += p.(float64)
	}
	if sum != 0 {
		t.Errorf("ポイント之和应当为 0，实际 %v（points = %v）", sum, ge["points"])
	}
	if got := ge["points"].([]any)[0]; got != float64(47100) {
		t.Errorf("一位 = %v, 期望 47100（27100 − 30000 + 50000）", got)
	}

	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Error("整场结束后没有回调通知房间")
	}
}

// rigWall 摆一副完全确定的牌山：前 52 张就是四家的手牌、第 53 张是庄家的第一次摸牌，
// 其余按一副完整的牌补足到 136 张。
func rigWall(t *testing.T, hands [4][]mahjong.Tile, dealerDraw mahjong.Tile) *mahjong.Wall {
	t.Helper()

	tiles := make([]mahjong.Tile, 0, mahjong.NumTiles)
	var used mahjong.Counts
	put := func(ts []mahjong.Tile) {
		for _, x := range ts {
			tiles = append(tiles, x)
			used[x.Kind()]++
		}
	}
	for seat := range hands {
		if n := len(hands[seat]); n != mahjong.HandSize {
			t.Fatalf("座位 %d 摆了 %d 张，应当 %d 张", seat, n, mahjong.HandSize)
		}
		put(hands[seat])
	}
	put([]mahjong.Tile{dealerDraw})

	// 剩下的位置用一副完整的牌补齐。这一步同时是校验：摆牌里某种牌超过 4 张时
	// 补不出 136 张，NewWallFromTiles 会报出来。
	for _, x := range mahjong.FullSet() {
		if used[x.Kind()] > 0 {
			used[x.Kind()]--
			continue
		}
		tiles = append(tiles, x)
	}

	wall, err := mahjong.NewWallFromTiles(tiles)
	if err != nil {
		t.Fatalf("摆牌失败: %v", err)
	}
	return wall
}

// discardDrawn 走一步：把当前行动者刚摸到的那张打出去。
//
// 它就是测试里的"假玩家" —— 不做决策，只把回合交出去。返回 false 表示牌山已空。
func discardDrawn(t *testing.T, g *Game, n *fakeNotifier) bool {
	t.Helper()

	turn, ok := n.find(userOf(0), protocol.EventGameTurn)
	if !ok {
		return false
	}
	seat := int(turn["seat"].(float64))

	drew, ok := n.find(userOf(seat), protocol.EventGameDrew)
	if !ok {
		return false
	}
	raw, _ := drew["tile"].(string)
	if raw == "" {
		t.Fatalf("座位 %d 是摸牌者，却拿不到 tile", seat)
	}
	ts, err := mahjong.ParseTiles(raw)
	if err != nil || len(ts) != 1 {
		t.Fatalf("摸到的牌 %q 解不出来: %v", raw, err)
	}
	return g.Discard(userOf(seat), ts[0]) == nil
}
