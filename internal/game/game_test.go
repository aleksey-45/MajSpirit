package game

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"slices"
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
	if opt.ClaimLimit == 0 {
		opt.ClaimLimit = time.Second
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

	for i := range 200 {
		if _, ok := n.find(userOf(0), protocol.EventGameRoundEnd); ok {
			break
		}
		if !discardDrawn(t, g, n) {
			t.Logf("第 %d 步推不动：discard=%d window=%d closed=%d",
				i, n.count(userOf(0), protocol.EventGameDiscarded),
				n.count(userOf(0), protocol.EventGameClaimWindow),
				n.count(userOf(0), protocol.EventGameClaimClosed))
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

	// 发牌后剩 70 张（136 − 52 − 14 张王牌），所以正好摸 70 次、打 70 张。
	if got := n.count(userOf(0), protocol.EventGameDiscarded); got != 70 {
		t.Errorf("打出了 %d 张, 期望 70（136 − 52 − 王牌 14）", got)
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

// 荣和：别人打出的牌成全了自己 —— 窗口、裁决、结算一条线走完。
//
// 同时验协议 §7.3 第 2 条：**窗口只推给有资格的座位**。"某人收到了 options"这件事
// 本身就暴露了他可能有和牌机会，所以不能广播之后各自过滤。
func TestRonClaimWindow(t *testing.T) {
	hands := [4][]mahjong.Tile{
		// 庄家摸到 8p，摸切打出去。
		mahjong.MustParseTiles("111222333444z5z"),
		// 下家 234m567m234s456s8p + 8p = 断幺九。和牌张补的是**对子**（単騎），
		// 所以没有平和 —— 这是「同一副牌，自摸和荣和的役可能不同」的实例。
		mahjong.MustParseTiles("234m567m234s456s8p"),
		mahjong.MustParseTiles("111188889999m6z"),
		mahjong.MustParseTiles("1111s77779999s7z"),
	}
	g, n := newTestGame(t, Options{
		Rounds: 1,
		Wall:   rigWall(t, hands, mahjong.MustParseTiles("8p")[0]),
	})

	if err := g.Discard(userOf(0), mahjong.MustParseTiles("8p")[0]); err != nil {
		t.Fatalf("打牌失败: %v", err)
	}

	w, ok := n.find(userOf(1), protocol.EventGameClaimWindow)
	if !ok {
		t.Fatal("能荣和的人没收到鸣牌窗口")
	}
	if w["tile"] != "8p" || w["fromSeat"] != float64(0) || w["trigger"] != "discard" {
		t.Errorf("窗口内容 = %v", w)
	}
	opts, _ := w["options"].([]any)
	if len(opts) != 2 || opts[0] != "ron" || opts[1] != "pass" {
		t.Errorf("options = %v, 期望 [ron pass]", w["options"])
	}

	// 没资格的座位**一条都不该收到** —— 收件人名单本身就是隐私。
	for _, seat := range []int{0, 2, 3} {
		if n.count(userOf(seat), protocol.EventGameClaimWindow) != 0 {
			t.Errorf("座位 %d 没有荣和机会，却收到了鸣牌窗口", seat)
		}
	}

	if err := g.DeclareWin(userOf(1)); err != nil {
		t.Fatalf("荣和被拒: %v", err)
	}

	closed, ok := n.find(userOf(0), protocol.EventGameClaimClosed)
	if !ok {
		t.Fatal("窗口没有收尾事件")
	}
	if closed["resolvedBy"] != "ron" || closed["seat"] != float64(1) {
		t.Errorf("claimClosed = %v", closed)
	}

	end, ok := n.find(userOf(1), protocol.EventGameRoundEnd)
	if !ok {
		t.Fatal("没有 game.roundEnd")
	}
	if end["endedBy"] != "win" {
		t.Fatalf("endedBy = %v", end["endedBy"])
	}
	win := end["wins"].([]any)[0].(map[string]any)
	if win["winnerSeat"] != float64(1) || win["isTsumo"] != false {
		t.Errorf("win = %v", win)
	}
	// 自摸是 null，荣和是**放铳者的座位号** —— 0 是合法座位，不能拿哨兵顶。
	if win["loserSeat"] != float64(0) {
		t.Errorf("loserSeat = %v, 期望 0（放铳的是庄家）", win["loserSeat"])
	}
	// 断幺九 1 翻；门清荣和 20+10，単騎 +2 → 32 → 40 符。子家荣和：放铳者付 1300。
	if win["han"] != float64(1) || win["fu"] != float64(40) {
		t.Errorf("翻符 = %v 翻 %v 符, 期望 1 翻 40 符", win["han"], win["fu"])
	}
	want := []float64{23700, 26300, 25000, 25000}
	for i, sc := range end["newScores"].([]any) {
		if sc != want[i] {
			t.Errorf("最终分 = %v, 期望 %v", end["newScores"], want)
			break
		}
	}
}

// 吃碰：优先级是**荣和 > 碰 > 吃**，同级才看远近。碰的人比吃的人远，照样是碰赢。
//
// 顺带验"鸣完由谁打"：鸣牌者不摸牌就得打一张，而且**不能宣告自摸**（没摸牌）——
// 这就是 `drawn` 和 `awaitingDiscard` 不能合成一个字段的原因。
func TestPonBeatsChiAndMelderDiscards(t *testing.T) {
	five := mahjong.MustParseTiles("5m")[0]
	hands := [4][]mahjong.Tile{
		mahjong.MustParseTiles("111222333444z5z"),     // 庄家摸 5m 后摸切
		mahjong.MustParseTiles("34m111s222s333s1p9p"), // 下家能吃
		mahjong.MustParseTiles("55m111p222p333p1s9s"), // 对家能碰（比吃的座位远）
		mahjong.MustParseTiles("666677778888m9p"),
	}
	g, n := newTestGame(t, Options{
		Rounds: 1,
		Wall:   rigWall(t, hands, five),
	})

	if err := g.Discard(userOf(0), five); err != nil {
		t.Fatalf("打牌失败: %v", err)
	}

	// 两边各自看到自己的选项，而且只有自己能吃的那个选项。
	chiWin, ok := n.find(userOf(1), protocol.EventGameClaimWindow)
	if !ok {
		t.Fatal("下家没收到窗口")
	}
	if opts := chiWin["options"].([]any); len(opts) != 2 || opts[0] != "chi" {
		t.Errorf("下家的 options = %v, 期望 [chi pass]", chiWin["options"])
	}
	ponWin, ok := n.find(userOf(2), protocol.EventGameClaimWindow)
	if !ok {
		t.Fatal("对家没收到窗口")
	}
	if opts := ponWin["options"].([]any); len(opts) != 2 || opts[0] != "pon" {
		t.Errorf("对家的 options = %v, 期望 [pon pass]", ponWin["options"])
	}

	// 两家都宣告。**吃的座位更近**，但碰的优先级更高。
	if err := g.Chi(userOf(1), [2]mahjong.Tile{
		mahjong.MustParseTiles("3m")[0], mahjong.MustParseTiles("4m")[0]}); err != nil {
		t.Fatalf("吃失败: %v", err)
	}
	if err := g.Pon(userOf(2)); err != nil {
		t.Fatalf("碰失败: %v", err)
	}

	meld, ok := n.find(userOf(0), protocol.EventGameMeld)
	if !ok {
		for _, e := range n.all(userOf(0)) {
			t.Logf("%s %s", e.Type, e.Data)
		}
		t.Fatal("没有 game.meld")
	}
	if meld["seat"] != float64(2) || meld["kind"] != "pon" {
		t.Errorf("meld = %v, 期望座位 2 碰（碰压过吃）", meld)
	}
	if meld["fromSeat"] != float64(0) || meld["taken"] != "5m" {
		t.Errorf("meld 的来处 = %v / %v", meld["fromSeat"], meld["taken"])
	}
	if tiles := meld["tiles"].([]any); len(tiles) != 3 {
		t.Errorf("碰出来应当是 3 张，实际 %v", meld["tiles"])
	}

	closed, _ := n.find(userOf(0), protocol.EventGameClaimClosed)
	if closed["resolvedBy"] != "pon" || closed["seat"] != float64(2) {
		t.Errorf("claimClosed = %v", closed)
	}

	// 打牌权归鸣牌者。
	turn, _ := n.find(userOf(0), protocol.EventGameTurn)
	if turn["seat"] != float64(2) {
		t.Errorf("鸣牌之后该由座位 2 打，实际 %v", turn["seat"])
	}
	// 没摸牌就不能自摸 —— 这一条是"鸣完不能和"的现场验证。
	if err := g.DeclareWin(userOf(2)); err == nil {
		t.Error("鸣牌之后没摸牌，却允许了自摸")
	}
	// 但必须能打一张，否则回合就卡死了。
	if err := g.Discard(userOf(2), mahjong.MustParseTiles("1p")[0]); err != nil {
		t.Errorf("鸣牌者应当能直接打牌，实际 %v", err)
	}
}

// 暗杠：手里四张收进副露 → 从**岭上**摸一张 → 多翻一张宝牌指示牌 → 还是自己打。
//
// 三件事一起验，因为它们是一次动作的三个结果：只验"副露多了个杠"会漏掉
// "摸到了不该摸的牌"或者"新宝牌没发出去"。
func TestClosedKanDrawsRinshanAndRevealsDora(t *testing.T) {
	hands := [4][]mahjong.Tile{
		mahjong.MustParseTiles("1111m234567s999p"), // 四张一万，能暗杠
		mahjong.MustParseTiles("2222m333s444s555p"),
		mahjong.MustParseTiles("3333m666s777s888p"),
		mahjong.MustParseTiles("4444m999s111p222p"),
	}
	g, n := newTestGame(t, Options{
		Rounds: 1,
		Wall:   rigWall(t, hands, mahjong.MustParseTiles("1z")[0]),
	})

	if err := g.Kan(userOf(0), "closed", mahjong.MustParseTiles("1m")[0]); err != nil {
		t.Fatalf("暗杠被拒: %v", err)
	}

	meld, ok := n.find(userOf(0), protocol.EventGameMeld)
	if !ok {
		t.Fatal("没有 game.meld")
	}
	if meld["kind"] != "kan" || meld["closed"] != true {
		t.Errorf("meld = %v, 期望暗杠（closed=true）", meld)
	}

	// 岭上摸的那张：带 rinshan 标记，而且只给摸牌者本人看到牌面。
	var sawRinshan bool
	for _, raw := range n.raws(userOf(0), protocol.EventGameDrew) {
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		if m["rinshan"] == true {
			sawRinshan = true
			if _, has := m["tile"]; !has {
				t.Error("岭上摸牌者应当看到自己摸到什么")
			}
		}
	}
	if !sawRinshan {
		t.Error("杠之后没有从岭上摸牌")
	}
	for _, seat := range []int{1, 2, 3} {
		for _, raw := range n.raws(userOf(seat), protocol.EventGameDrew) {
			var m map[string]any
			if err := json.Unmarshal([]byte(raw), &m); err != nil {
				t.Fatal(err)
			}
			if m["rinshan"] == true {
				if _, has := m["tile"]; has {
					t.Errorf("座位 %d 看到了别人岭上摸的牌", seat)
				}
			}
		}
	}

	// 新宝牌是**公开**信息：四个人都该收到，count 从 1 变 2。
	dora, ok := n.find(userOf(2), protocol.EventGameDoraRevealed)
	if !ok {
		t.Fatal("杠了却没有新宝牌指示牌")
	}
	if dora["kind"] != "dora" || dora["count"] != float64(2) {
		t.Errorf("doraRevealed = %v, 期望 dora / count 2", dora)
	}

	// 杠不换人：摸完岭上还是自己打。
	turn, _ := n.find(userOf(0), protocol.EventGameTurn)
	if turn["seat"] != float64(0) {
		t.Errorf("杠之后该由自己打，实际座位 %v", turn["seat"])
	}
	if err := g.Discard(userOf(0), mahjong.MustParseTiles("2s")[0]); err != nil {
		t.Errorf("杠之后应当能直接打牌，实际 %v", err)
	}

	// 不是自己的回合不能暗杠。
	if err := g.Kan(userOf(1), "closed", mahjong.MustParseTiles("2m")[0]); err == nil {
		t.Error("不是自己的回合，却允许了暗杠")
	}
}

// rigWall 摆一副完全确定的牌山：前 52 张就是四家的手牌、第 53 张是庄家的第一次摸牌，
// 其余按一副完整的牌补足到 136 张。
func rigWall(t *testing.T, hands [4][]mahjong.Tile, dealerDraw mahjong.Tile) *mahjong.Wall {
	t.Helper()
	return rigWallBurned(t, hands, dealerDraw, 0)
}

// rigWallBurned 同 rigWall，但在四家手牌**前面**垫 prefix 张废牌。
//
// 配合 `Wall.Burn(prefix)` 用：烧掉那 prefix 张之后，牌山刚好只剩这 52 张手牌 +
// 庄家的第一摸。这是摆出"牌山只剩最后一张"的唯一办法 —— 一副牌每种恰好 4 张是
// 硬约束，凑不出天然的短牌山，而海底摸月/河底捞鱼只在那个局面上成立。
func rigWallBurned(t *testing.T, hands [4][]mahjong.Tile, dealerDraw mahjong.Tile, prefix int) *mahjong.Wall {
	t.Helper()

	// 先数清楚"四家手牌 + 庄家一摸"占了哪些牌，剩下的全是废牌。
	var used mahjong.Counts
	for seat := range hands {
		if n := len(hands[seat]); n != mahjong.HandSize {
			t.Fatalf("座位 %d 摆了 %d 张，应当 %d 张", seat, n, mahjong.HandSize)
		}
		for _, x := range hands[seat] {
			used[x.Kind()]++
		}
	}
	used[dealerDraw.Kind()]++

	var rest []mahjong.Tile
	for _, x := range mahjong.FullSet() {
		if used[x.Kind()] > 0 {
			used[x.Kind()]--
			continue
		}
		rest = append(rest, x)
	}
	if prefix > len(rest) {
		t.Fatalf("垫牌 %d 张，但只剩 %d 张废牌", prefix, len(rest))
	}

	// 布局：废牌 prefix 张 ‖ 四家手牌 ‖ 庄家的第一摸 ‖ 剩下的废牌。
	// 末尾 14 张会被 NewWallFromTiles 切成王牌，所以它们不参与"可摸"。
	tiles := make([]mahjong.Tile, 0, mahjong.NumTiles)
	tiles = append(tiles, rest[:prefix]...)
	for seat := range hands {
		tiles = append(tiles, hands[seat]...)
	}
	tiles = append(tiles, dealerDraw)
	tiles = append(tiles, rest[prefix:]...)

	wall, err := mahjong.NewWallFromTiles(tiles)
	if err != nil {
		t.Fatalf("摆牌失败: %v", err)
	}
	return wall
}

// 海底摸月：牌山摸空的那一张自摸。
//
// 测它需要摆出"牌山只剩一张"的局面，而一副牌每种恰好 4 张是硬约束 —— 所以先
// rigWallBurned 把手牌垫到第 70 张，再 Burn 掉前面的废牌。为了这个用例才加的
// `Wall.Burn`，它不该出现在生产路径上。
func TestHaiteiTsumo(t *testing.T) {
	hands := [4][]mahjong.Tile{
		mahjong.MustParseTiles("123m456m789p55p34s"),
		mahjong.MustParseTiles("111222333444z6z"),
		mahjong.MustParseTiles("777788889999m2z"),
		mahjong.MustParseTiles("1111s22223333p7z"),
	}
	// 122 张可摸 − 52 张手牌 − 庄家这一摸 = 69 张要先烧掉。
	wall := rigWallBurned(t, hands, mahjong.MustParseTiles("2s")[0], 69)
	if got := wall.Burn(69); got != 69 {
		t.Fatalf("烧掉 %d 张，期望 69 张", got)
	}

	g, n := newTestGame(t, Options{Rounds: 1, Wall: wall})
	if err := g.DeclareWin(userOf(0)); err != nil {
		t.Fatalf("海底自摸被拒: %v", err)
	}

	end, _ := n.find(userOf(0), protocol.EventGameRoundEnd)
	win := end["wins"].([]any)[0].(map[string]any)

	var names []string
	for _, y := range win["yaku"].([]any) {
		names = append(names, y.(map[string]any)["name"].(string))
	}
	if !slices.Contains(names, "海底摸月") {
		t.Errorf("役 = %v，应当有海底摸月", names)
	}
	// 平和 + 门前清自摸和 + 海底摸月 = 3 翻 20 符（平和自摸固定 20 符）。
	if win["han"] != float64(3) || win["fu"] != float64(20) {
		t.Errorf("翻符 = %v 翻 %v 符, 期望 3 翻 20 符（役 %v）", win["han"], win["fu"], names)
	}
}

// 河底捞鱼：牌山摸空之后打出的那一张被人荣和。
func TestHouteiRon(t *testing.T) {
	hands := [4][]mahjong.Tile{
		mahjong.MustParseTiles("111222333444z5z"), // 庄家摸 8p 后打出去
		mahjong.MustParseTiles("234m567m234s456s8p"),
		mahjong.MustParseTiles("111188889999m6z"),
		mahjong.MustParseTiles("1111s22223333p7z"),
	}
	eight := mahjong.MustParseTiles("8p")[0]
	wall := rigWallBurned(t, hands, eight, 69)
	wall.Burn(69)

	g, n := newTestGame(t, Options{Rounds: 1, Wall: wall})
	if err := g.Discard(userOf(0), eight); err != nil {
		t.Fatalf("打牌失败: %v", err)
	}
	if err := g.DeclareWin(userOf(1)); err != nil {
		t.Fatalf("河底荣和被拒: %v", err)
	}

	end, _ := n.find(userOf(1), protocol.EventGameRoundEnd)
	win := end["wins"].([]any)[0].(map[string]any)
	if win["loserSeat"] != float64(0) {
		t.Errorf("loserSeat = %v, 期望 0", win["loserSeat"])
	}
	var names []string
	for _, y := range win["yaku"].([]any) {
		names = append(names, y.(map[string]any)["name"].(string))
	}
	if !slices.Contains(names, "河底捞鱼") {
		t.Errorf("役 = %v，应当有河底捞鱼", names)
	}
}

// 帕奥责任必须记在**鸣牌那一刻**：副露里只留下"有一组白板"，看不出那张牌是谁打的。
//
// 直接单测这条记录规则，不摆一整局出来 —— 摆牌只能验到"某一局恰好成立"，
// 而这里要验的是**阈值在哪一刻被跨过**：第三组三元牌才算，第二组不算。
func TestPaoIsNotedAtTheMeldThatCompletesIt(t *testing.T) {
	g := &Game{}
	dragon := func(k mahjong.Tile) mahjong.Meld {
		return mahjong.Meld{Kind: mahjong.MeldTriplet, IsOpen: true,
			Tiles: [4]mahjong.Tile{k, k, k}}
	}
	white := mahjong.MustParseTiles("5z")[0]
	green := mahjong.MustParseTiles("6z")[0]
	red := mahjong.MustParseTiles("7z")[0]

	// 两家各喂一组：还不构成大三元。
	g.melds[1] = []mahjong.Meld{dragon(white)}
	g.notePao(1, 0)
	if got := g.paoSeatOf(1); got != mahjong.NoSeat {
		t.Errorf("只有一组三元牌就记了责任者：%d", got)
	}

	g.melds[1] = append(g.melds[1], dragon(green))
	g.notePao(1, 2)
	if got := g.paoSeatOf(1); got != mahjong.NoSeat {
		t.Errorf("两组三元牌就记了责任者：%d", got)
	}

	// 第三组：责任落在**打出这一张**的那家（座位 3）身上。
	g.melds[1] = append(g.melds[1], dragon(red))
	g.notePao(1, 3)
	if got, want := g.paoSeatOf(1), 3; got != want {
		t.Errorf("责任者 = %d，期望 %d（喂第三组的那家）", got, want)
	}
	if got := g.paoYakuOf(1); got != "大三元" {
		t.Errorf("被喂成的役满 = %q，期望 大三元", got)
	}
	// 别人没碰过大三元，就没有责任者。
	if got := g.paoSeatOf(2); got != mahjong.NoSeat {
		t.Errorf("座位 2 不该有责任者：%d", got)
	}
}

// discardDrawn 走一步：把当前行动者刚摸到的那张打出去。
//
// 它就是测试里的"假玩家" —— 不做决策，只把回合交出去。返回 false 表示牌山已空。
func discardDrawn(t *testing.T, g *Game, n *fakeNotifier) bool {
	t.Helper()

	// 有人能鸣时会开窗口，此时没有新的 game.turn。假玩家一律「过」。
	//
	// 对**四个座位无脑过一遍**，不靠数事件判断窗口开没开：窗口只推给有资格的座位，
	// 而 claimClosed 是广播给所有人的 —— 拿两个计数相减必然错位。
	// 没有窗口时 Pass 是空操作（doPass 对没资格的座位返回 nil）。
	for seat := range 4 {
		if err := g.Pass(userOf(seat)); err != nil {
			t.Fatalf("座位 %d 过牌失败: %v", seat, err)
		}
	}

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
