package room

import (
	"errors"
	"sync"
	"testing"
	"time"

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

// 掉线不能让位子当场没掉：120 秒内随时回来都还在原座（协议 §4.5）。
func TestDisconnectHoldsSeatThenVacates(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)
	if err := r.Join(testUser(2, "乙")); err != nil {
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

	// 窗口过去：座位释放，广播 room.seatVacated。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := n.find(1, protocol.EventRoomSeatVacated); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m, ok := n.find(1, protocol.EventRoomSeatVacated)
	if !ok {
		t.Fatal("保座超时没有释放座位")
	}
	if m["seat"] != float64(1) || m["reason"] != "timeout" {
		t.Errorf("seatVacated = %v, 期望 seat 1 / reason timeout", m)
	}
	if s := latestSeat(t, n, 1, 1); s != nil {
		t.Error("超时之后座位还占着")
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

	// 重连之后那个保座定时器必须不再开火 —— 否则过一会儿座位会被凭空放掉。
	time.Sleep(3 * fastHold)
	if s := latestSeat(t, n, 1, 1); s == nil {
		t.Fatal("重连之后保座定时器还在跑，把座位放掉了")
	}
	if n.count(1, protocol.EventRoomSeatVacated) != 0 {
		t.Error("重连之后不该出现 room.seatVacated")
	}
}

// seq 是房间级的一条流：同一条事件四个人拿到同一个号。
// 各发各的号，客户端靠"序号有洞"检测丢消息的能力就没了（协议 §1.4）。
func TestSeqIsSharedAcrossSeats(t *testing.T) {
	_, r, n := newTestRoom(t, fastHold)
	if err := r.Join(testUser(2, "乙")); err != nil {
		t.Fatal(err)
	}

	// 找一条四个人都该收到的同类型事件，比较它的 seq。
	seatsA := eventsOf(t, n, 1, protocol.EventRoomSeats)
	seatsB := eventsOf(t, n, 2, protocol.EventRoomSeats)
	if len(seatsA) == 0 || len(seatsB) == 0 {
		t.Fatal("两边都没收到 room.seats")
	}
	lastA, lastB := seatsA[len(seatsA)-1], seatsB[len(seatsB)-1]
	if lastA.Seq != lastB.Seq {
		t.Errorf("同一条事件的两个收件人拿到不同 seq：%d vs %d", lastA.Seq, lastB.Seq)
	}

	// 每个收件人自己的 seq 也必须严格递增。
	last := 0
	for _, e := range n.all(1) {
		if e.Seq <= last {
			t.Fatalf("seq 没有递增：%d 之后出现 %d", last, e.Seq)
		}
		last = e.Seq
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
	h := NewHub(n, nil)
	defer h.Close()
	r := newRoom("TEST02", testUser(1, "甲"), h, fastHold)

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
	h := NewHub(n, nil)
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

func eventsOf(t *testing.T, n *fakeNotifier, userID int64, typ protocol.Command) []protocol.Envelope {
	t.Helper()
	var out []protocol.Envelope
	for _, e := range n.all(userID) {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}
