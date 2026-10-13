package room

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/game"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

// room_test.go —— 大厅：座位、准备、开局、掉线保座。
//
// 全部不碰网络：Notifier 是假的，四个"玩家"就是四个 user.User。

const fastHold = 120 * time.Millisecond

// latestSeat 返回某个视角下**最近一条** room.seats 里第 i 个座位的状态，空座位返回 nil。
//
// 测试一律通过"客户端收到了什么"来断言，不直接读房间字段 —— 房间状态是单 goroutine
// 独占的，从测试 goroutine 直接读就是数据竞争，`-race` 会当场抓住。而经事件断言
// 顺带也验了"客户端看到的就是我们想要的"。
func latestSeat(t *testing.T, n *fakeNotifier, viewer int64, i int) map[string]any {
	t.Helper()
	m, ok := n.find(viewer, protocol.EventRoomSeats)
	if !ok {
		t.Fatal("没有 room.seats")
	}
	return userAt(t, seatsOf(t, m), i)
}

func TestNewRoomPutsOwnerInSeatZero(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)

	m, ok := n.find(1, protocol.EventRoomCreated)
	if !ok {
		t.Fatal("房主没收到 room.created")
	}
	if m["roomId"] != r.ID() {
		t.Errorf("room.created.roomId = %v, 期望 %s", m["roomId"], r.ID())
	}
	if m["seat"] != float64(0) {
		t.Errorf("room.created.seat = %v, 期望 0（房主坐 0 号）", m["seat"])
	}
}

func TestJoinFillsSeatsInOrder(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)

	for i := int64(2); i <= 4; i++ {
		if err := r.Join(testUser(i, "玩家")); err != nil {
			t.Fatalf("第 %d 个人加入失败: %v", i, err)
		}
	}

	// 加入者拿到自己的座位号。
	if m, ok := n.find(4, protocol.EventRoomJoined); !ok {
		t.Fatal("加入者没收到 room.joined")
	} else if m["mySeat"] != float64(3) {
		t.Errorf("mySeat = %v, 期望 3（按最小空位）", m["mySeat"])
	}

	// 其余三人靠 room.seats 知道有人进来了。
	seats, ok := n.find(1, protocol.EventRoomSeats)
	if !ok {
		t.Fatal("老玩家没收到 room.seats")
	}
	arr := seatsOf(t, seats)
	for i := 0; i < 4; i++ {
		if userAt(t, arr, i) == nil {
			t.Fatalf("第 %d 个座位应该是有人，实际是空的", i)
		}
	}
	if owner := userAt(t, arr, 0); owner["isOwner"] != true {
		t.Error("0 号座（房主）的 isOwner 应当为 true")
	}
}

func TestFifthPlayerIsRejected(t *testing.T) {
	_, r, _ := newTestRoom(t, fastHold)

	for i := int64(2); i <= 4; i++ {
		if err := r.Join(testUser(i, "玩家")); err != nil {
			t.Fatalf("加入失败: %v", err)
		}
	}
	if err := r.Join(testUser(5, "第五个")); !errors.Is(err, ErrRoomFull) {
		t.Errorf("第五个人加入返回 %v, 期望 ErrRoomFull", err)
	}
}

// 已经在房间里的人再"加入"一次 = 重连（协议 §4.5），不是占第二个座位。
// 认错的话，一个人断线重连就把房间变成两个人的，剩下两个座位说没就没了。
func TestJoinAgainIsReconnectNotSecondSeat(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)

	if err := r.Join(testUser(2, "乙")); err != nil {
		t.Fatal(err)
	}
	if err := r.Join(testUser(2, "乙")); err != nil {
		t.Fatalf("重连不该失败: %v", err)
	}
	// 关键断言：乙没有占第二格 —— 后面这个人必须拿到 2 号座。
	// 只看"第五个人被拒"抓不到这个错：占错了格也照样会在第五个人那里满。
	if err := r.Join(testUser(3, "丙")); err != nil {
		t.Fatal(err)
	}
	m, ok := n.find(3, protocol.EventRoomJoined)
	if !ok {
		t.Fatal("丙没收到 room.joined")
	}
	if m["mySeat"] != float64(2) {
		t.Errorf("丙的座位 = %v, 期望 2（说明乙重连时没占新座位）", m["mySeat"])
	}
}

func TestStartGuards(t *testing.T) {
	_, r, _ := newTestRoom(t, fastHold)

	if err := r.Start(1); !errors.Is(err, ErrNotEnough) {
		t.Errorf("人没齐就开局 = %v, 期望 ErrNotEnough", err)
	}

	for i := int64(2); i <= 4; i++ {
		if err := r.Join(testUser(i, "玩家")); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Start(2); !errors.Is(err, ErrNotOwner) {
		t.Errorf("非房主开局 = %v, 期望 ErrNotOwner", err)
	}
	if err := r.Start(1); !errors.Is(err, ErrNotReady) {
		t.Errorf("有人没准备就开局 = %v, 期望 ErrNotReady", err)
	}

	// 只有三家准备也不行 —— 少一家就必须拒，否则会开出一个三缺一的局。
	for i := int64(2); i <= 3; i++ {
		if err := r.SetReady(i, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Start(1); !errors.Is(err, ErrNotReady) {
		t.Errorf("三缺一就开局 = %v, 期望 ErrNotReady", err)
	}

	if err := r.SetReady(4, true); err != nil {
		t.Fatal(err)
	}
	// 房主自己也得准备 —— 「四人全 ready」是四个人，不是另外三个。
	if err := r.SetReady(1, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(1); err != nil {
		t.Fatalf("四家齐、全准备、房主开局，应当成功: %v", err)
	}
	if err := r.Start(1); !errors.Is(err, ErrPlaying) {
		t.Errorf("重复开局 = %v, 期望 ErrPlaying", err)
	}
}

func TestReadyChangedIsBroadcast(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)
	if err := r.Join(testUser(2, "乙")); err != nil {
		t.Fatal(err)
	}

	if err := r.SetReady(2, true); err != nil {
		t.Fatal(err)
	}
	m, ok := n.find(1, protocol.EventRoomReadyChanged)
	if !ok {
		t.Fatal("准备状态变化没有广播")
	}
	if m["seat"] != float64(1) || m["ready"] != true {
		t.Errorf("readyChanged = %v, 期望 seat 1 / ready true", m)
	}

	// 重发同一个值不该再广播一次（客户端重发是常事，不该刷屏）。
	before := n.count(1, protocol.EventRoomReadyChanged)
	if err := r.SetReady(2, true); err != nil {
		t.Fatal(err)
	}
	if after := n.count(1, protocol.EventRoomReadyChanged); after != before {
		t.Errorf("重复设置同一个值也广播了：%d → %d", before, after)
	}
}

func TestLeaveFreesSeatAndEmptyRoomDies(t *testing.T) {
	h, r, n := newTestRoom(t, fastHold)
	if err := r.Join(testUser(2, "乙")); err != nil {
		t.Fatal(err)
	}

	if err := r.Leave(2); err != nil {
		t.Fatal(err)
	}
	if m, ok := n.find(1, protocol.EventRoomLeft); !ok || m["seat"] != float64(1) {
		t.Errorf("room.left = %v, 期望 seat 1", m)
	}
	if _, ok := n.find(1, protocol.EventRoomSeats); !ok {
		t.Error("结构变化之后应当补一条全量座位表")
	}

	// 房间空了就该销毁：留着就是一张查得到、投进去没人理的表。
	if err := r.Leave(1); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Lookup(r.ID()); ok {
		t.Error("人都走光了，房间还挂在 Hub 上")
	}
	if err := r.Join(testUser(9, "迟到")); !errors.Is(err, ErrClosed) {
		t.Errorf("往已销毁的房间投指令 = %v, 期望 ErrClosed", err)
	}
}

// 房主走了要把身份交出去。漏掉的话，剩下三家会看到一个"没有房主"的房间 ——
// 谁都不能开局，而且没人能修。
func TestOwnerHandoverOnLeave(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)
	for i := int64(2); i <= 3; i++ {
		if err := r.Join(testUser(i, "玩家")); err != nil {
			t.Fatal(err)
		}
	}

	if err := r.Leave(1); err != nil {
		t.Fatal(err)
	}

	m, ok := n.find(2, protocol.EventRoomSeats)
	if !ok {
		t.Fatal("没收到座位表")
	}
	arr := seatsOf(t, m)
	if userAt(t, arr, 0) != nil {
		t.Error("0 号座应当空了")
	}
	if owner := userAt(t, arr, 1); owner["isOwner"] != true {
		t.Errorf("房主身份应当交给 1 号座, 实际 %v", owner)
	}
	if err := r.Start(2); !errors.Is(err, ErrNotEnough) {
		t.Errorf("新房东开局（人不够）= %v, 期望 ErrNotEnough —— 说明他确实拿到了房主身份", err)
	}
}

// 掉线不能让位子当场没掉：120 秒内随时回来都还在原座（协议 §4.5）；
// **过了窗口没回来就关掉整个房间**（2026-10-13 定的）。
//
// 为什么不是"释放座位、剩下三个人继续"：麻将少一个人就打不了，而我们还没有
// 「中断对局 / 托管 / CPU 替打」那一套。让三个人守着一个打不了的房间，
// 比明确告诉他们"这局散了"更糟。
func TestDisconnectTimeoutClosesRoom(t *testing.T) {
	h, r, n := newTestRoom(t, fastHold)
	if err := r.Join(testUser(2, "乙")); err != nil {
		t.Fatal(err)
	}
	if err := r.Join(testUser(3, "丙")); err != nil {
		t.Fatal(err)
	}

	if err := r.Disconnected(2); err != nil {
		t.Fatal(err)
	}

	// 另外三家必须看见"他掉线了"—— 协议 §4.5 明确要求掉线可见。
	if s := latestSeat(t, n, 1, 1); s == nil || s["connected"] != false {
		t.Errorf("座位 1 应当是 connected=false, 实际 %v", s)
	}

	// 保座窗口内：座位还在。
	time.Sleep(fastHold / 3)
	if s := latestSeat(t, n, 1, 1); s == nil {
		t.Error("保座窗口内座位就没了")
	}

	// 等超时。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := n.find(1, protocol.EventRoomClosed); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 先说是谁超时了，再说房间没了 —— 顺序反了的话剩下的人只知道"房间关了"。
	if m, ok := n.find(1, protocol.EventRoomSeatVacated); !ok {
		t.Error("没说是谁超时了")
	} else if m["seat"] != float64(1) || m["reason"] != "timeout" {
		t.Errorf("seatVacated = %v, 期望 seat 1 / reason timeout", m)
	}

	m, ok := n.find(1, protocol.EventRoomClosed)
	if !ok {
		t.Fatal("保座超时没有关房")
	}
	if m["reason"] != reasonPlayerTimeout {
		t.Errorf("room.closed.reason = %v, 期望 %s", m["reason"], reasonPlayerTimeout)
	}
	if m["roomId"] != r.ID() {
		t.Errorf("room.closed.roomId = %v, 期望 %s", m["roomId"], r.ID())
	}

	// 还在座的那两个人也要收到 —— 他们才是需要知道"这局散了"的人。
	if _, ok := n.find(3, protocol.EventRoomClosed); !ok {
		t.Error("另一个在座的人没收到 room.closed")
	}

	// 摘表 + 关停：查不到，也投不进指令。
	if _, ok := h.Lookup(r.ID()); ok {
		t.Error("关了房还挂在 Hub 上")
	}
	if err := r.Join(testUser(4, "丁")); !errors.Is(err, ErrClosed) {
		t.Errorf("往已关闭的房间投指令 = %v, 期望 ErrClosed", err)
	}
}

func TestReconnectWithinHoldKeepsSeat(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)
	if err := r.Join(testUser(2, "乙")); err != nil {
		t.Fatal(err)
	}

	if err := r.Disconnected(2); err != nil {
		t.Fatal(err)
	}
	if err := r.Reconnected(2); err != nil {
		t.Fatal(err)
	}
	if s := latestSeat(t, n, 1, 1); s == nil || s["connected"] != true {
		t.Fatalf("重连后座位 1 应当是 connected=true, 实际 %v", s)
	}

	// 回来的人必须重新拿到 roomId 与自己的座位：刷新过的页面手上什么都没有，
	// 而 room.joined 是客户端重建房间视图的唯一来源（协议 §4.5）。
	m, ok := n.find(2, protocol.EventRoomJoined)
	if !ok {
		t.Fatal("重连没有补发 room.joined —— 刷新过的页面画不出座位")
	}
	if m["roomId"] != r.ID() || m["mySeat"] != float64(1) {
		t.Errorf("room.joined = %v, 期望 roomId %s / mySeat 1", m, r.ID())
	}

	// 重连之后那个保座定时器必须不再开火 —— 否则过一会儿座位会被凭空放掉。
	time.Sleep(3 * fastHold)
	if s := latestSeat(t, n, 1, 1); s == nil {
		t.Fatal("重连之后保座定时器还在跑，把座位放掉了")
	}
	if n.count(1, protocol.EventRoomSeatVacated) != 0 {
		t.Error("重连之后不该出现 room.seatVacated")
	}
}

// 对局中离开算认输（协议 §5.2），后果与掉线超时一样：**整个房间关掉**。
//
// 不这么做的话，座位被清掉而那个对局照旧打到底 —— 它的座位表是开局时拷的一份，
// 会一直推着三个人的牌局，直到自己算出个结果，而四个人已经回不去那个房间了。
func TestMidGameLeaveCountsAsSurrender(t *testing.T) {
	h, r, n := newTestRoom(t, fastHold)
	fillAndReady(t, r)
	if err := r.Start(1); err != nil {
		t.Fatalf("开局: %v", err)
	}
	g := r.Game()
	if g == nil {
		t.Fatal("开局之后房间手上没有对局")
	}

	if err := r.Leave(3); err != nil {
		t.Fatalf("中局离开: %v", err)
	}

	// 先说他走了，再说房间没了（和保座超时同一个顺序，理由也一样）。
	if m, ok := n.find(1, protocol.EventRoomLeft); !ok || m["seat"] != float64(2) {
		t.Errorf("没广播 room.left: %v", m)
	}
	m, ok := n.find(1, protocol.EventRoomClosed)
	if !ok {
		t.Fatal("中局离开没有关房")
	}
	if m["reason"] != reasonPlayerLeft {
		t.Errorf("room.closed.reason = %v, 期望 %s", m["reason"], reasonPlayerLeft)
	}
	if _, ok := h.Lookup(r.ID()); ok {
		t.Error("关了房还挂在 Hub 上")
	}

	// 对局必须跟着走：它有自己的秒表，留下来就一直往外推牌局。
	waitGameGone(t, r, g)
}

// 房间被关掉（保座超时、认输、进程退出）时，手上那一场对局也要停。
func TestRoomCloseStopsRunningGame(t *testing.T) {
	_, r, _ := newTestRoom(t, fastHold)
	fillAndReady(t, r)
	if err := r.Start(1); err != nil {
		t.Fatalf("开局: %v", err)
	}
	g := r.Game()
	if g == nil {
		t.Fatal("开局之后房间手上没有对局")
	}

	r.Close()
	waitGameGone(t, r, g)
}

// waitGameGone 等房间把对局收走：房间手上不再有它，而且它不再受理指令。
//
// 只断"指针是 nil"不够 —— stopGame 是先摘指针再 Close 的，指针没了只说明房间动过手。
func waitGameGone(t *testing.T, r *Room, g *game.Game) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := g.Sync(1)
		if errors.Is(err, game.ErrClosed) && r.Game() == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("房间已经没了，对局 actor 还在跑（Sync = %v, Game = %v）", err, r.Game() != nil)
		}
		time.Sleep(time.Millisecond)
	}
}

// Hub 要能按人找到房间：网关靠它投对局指令、判断重连、拦住"同时进两个房间"。
//
// 关键是它跟着座位表走 —— **离开之后必须查不到**，否则指令会被投进一个他已经
// 离开的房间，而那不报错。
func TestHubFindsRoomByUser(t *testing.T) {
	n := newFakeNotifier()
	h := NewHub(n.Send, nil)
	defer h.Close()

	r, err := h.Create(testUser(1, "甲"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := h.ByUser(1); !ok || got != r {
		t.Error("房主按人查不到自己的房间")
	}
	if _, ok := h.ByUser(2); ok {
		t.Error("不在任何房间里的人被查到了")
	}

	if err := r.Join(testUser(2, "乙")); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.ByUser(2); !ok || got != r {
		t.Error("加入之后按人查不到房间")
	}

	if err := r.Leave(2); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.ByUser(2); ok {
		t.Error("离开之后还能按人查到房间")
	}

	// Hub 关掉之后也要立刻给答案，不能挂在那里等一个不会来的回复。
	h.Close()
	if _, ok := h.ByUser(1); ok {
		t.Error("Hub 关了还查得到房间")
	}
}

// seq 是**每个收件人自己**的一条流：他收到的每一条事件都比上一条恰好大 1。
//
// 这条判据就是协议 §1.4 说"有洞就是丢消息"的全部依据，所以它必须按收件人成立。
// 按事件发号（一条只发给某个人的事件，比如 room.joined，也吃掉一个公共号）
// 会让别人看到一个假洞 —— 客户端照着 §4.4 去拉快照，而其实什么都没丢。
func TestSeqIsContiguousPerRecipient(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)
	for i := int64(2); i <= 4; i++ {
		if err := r.Join(testUser(i, "玩家")); err != nil {
			t.Fatal(err)
		}
	}
	// 造几条只发给一个人的事件（room.joined、room.left）。
	if err := r.Leave(3); err != nil {
		t.Fatal(err)
	}

	for id := int64(1); id <= 4; id++ {
		last := 0
		for _, e := range n.all(id) {
			if e.Seq != last+1 {
				t.Fatalf("用户 %d 的流有洞：seq %d 之后是 %d（%s）", id, last, e.Seq, e.Type)
			}
			last = e.Seq
		}
		if last == 0 {
			t.Fatalf("用户 %d 一条事件都没收到", id)
		}
	}
}

// failingNotifier 模拟"每个对端都不读了"。
type failingNotifier struct {
	mu    sync.Mutex
	calls int
}

func (f *failingNotifier) Send(int64, protocol.Envelope) error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return errors.New("对端消费不过来")
}

// 推送失败**绝不进规则层**：发不出去一条 room.seats 不是房间的错误，也不影响任何
// 裁决。这条要是破了，一个掉线的客户端就能让别人的加入操作失败。
func TestNotifyFailureDoesNotBreakRoom(t *testing.T) {
	n := &failingNotifier{}
	h := NewHub(n.Send, nil)
	defer h.Close()
	r := newRoom("TEST02", testUser(1, "甲"), h,
		tuning{hold: fastHold, turnLimit: time.Second, turnBase: time.Second})

	for i := int64(2); i <= 4; i++ {
		if err := r.Join(testUser(i, "玩家")); err != nil {
			t.Fatalf("推送失败不该让加入失败: %v", err)
		}
	}
	// 房间状态必须完好：四个人满了，第五个仍然被拒。
	if err := r.Join(testUser(5, "第五个")); !errors.Is(err, ErrRoomFull) {
		t.Errorf("推送一直失败之后房间状态乱了: %v", err)
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if n.calls == 0 {
		t.Error("这个 notifier 一次都没被调用 —— 测试没测到推送失败这条路")
	}
}

// 各种"这个人不在房间里"的入口都要报 ErrNotInRoom，而不是默默成功。
func TestOperationsRejectNonMembers(t *testing.T) {
	_, r, _ := newTestRoom(t, fastHold)

	cases := []struct {
		name string
		err  error
	}{
		{"leave", r.Leave(99)},
		{"ready", r.SetReady(99, true)},
		{"disconnected", r.Disconnected(99)},
		{"reconnected", r.Reconnected(99)},
	}
	for _, c := range cases {
		if !errors.Is(c.err, ErrNotInRoom) {
			t.Errorf("%s 一个不在房间里的人 = %v, 期望 ErrNotInRoom", c.name, c.err)
		}
	}
}

// Hub 关掉之后，查房间、数房间、建房间都必须**立刻**给出确定答案，
// 而不是挂在那里等一个永远不会来的回复。
func TestClosedHubAnswersImmediately(t *testing.T) {
	n := newFakeNotifier()
	h := NewHub(n.Send, nil)
	r, err := h.Create(testUser(1, "甲"))
	if err != nil {
		t.Fatal(err)
	}

	h.Close()

	if _, ok := h.Lookup(r.ID()); ok {
		t.Error("Hub 关了还能查到房间")
	}
	if got := h.Len(); got != 0 {
		t.Errorf("Hub 关了 Len() = %d, 期望 0", got)
	}
	if _, err := h.Create(testUser(2, "乙")); !errors.Is(err, ErrHubClosed) {
		t.Errorf("Create = %v, 期望 ErrHubClosed", err)
	}
	if err := r.Join(testUser(2, "乙")); !errors.Is(err, ErrClosed) {
		t.Errorf("房间也应当已经关了, Join = %v", err)
	}
}

// 房间与对局共用一条流：大厅最后一个号与对局第一个号之间不能断。
//
// 对局自己发号（各造一个 Emitter）的话，game.started 会从 1 重新开始，而客户端
// 大厅那段基线已经涨到 7 —— 开局那一刻就是一个假洞，它照协议 §4.4 去拉快照。
func TestGameEventsContinueTheRoomStream(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)
	fillAndReady(t, r)
	if err := r.Start(1); err != nil {
		t.Fatalf("开局: %v", err)
	}

	if n.count(1, protocol.EventGameStarted) == 0 {
		t.Fatal("没有收到 game.started —— 这条用例什么都没测到")
	}
	for id := int64(1); id <= 4; id++ {
		last := 0
		for _, e := range n.all(id) {
			if e.Seq != last+1 {
				t.Fatalf("用户 %d 在开局处断了：seq %d 之后是 %d（%s）", id, last, e.Seq, e.Type)
			}
			last = e.Seq
		}
	}
}

// fillAndReady 把 2~4 号坐进来并让四个人全部准备。
func fillAndReady(t *testing.T, r *Room) {
	t.Helper()
	for i := int64(2); i <= 4; i++ {
		if err := r.Join(testUser(i, "玩家")); err != nil {
			t.Fatalf("加入: %v", err)
		}
	}
	for i := int64(1); i <= 4; i++ {
		if err := r.SetReady(i, true); err != nil {
			t.Fatalf("准备: %v", err)
		}
	}
}
