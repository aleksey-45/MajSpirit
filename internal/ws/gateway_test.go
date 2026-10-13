package ws

// gateway_test.go —— 网关的端到端：真 Handler + 真 Hub + 真房间与对局，只有认证是假的。
//
// 覆盖：四个账号打完整一场、错误码表（协议 §2.6）、顶号与重连。
// 四个假玩家只会两件事：自己的摸牌摸切，鸣牌窗口一律过。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/protocol"
	"github.com/aleksey-45/majspirit/internal/room"
	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// tokenAuth 按 token 认人：四个账号就是四个 token。
// fakeAuth 只会返回同一个用户，四个座位会挤成一个人。
type tokenAuth map[string]user.User

func (a tokenAuth) Authenticate(_ context.Context, token string) (user.User, error) {
	u, ok := a[token]
	if !ok {
		return user.User{}, errors.New("坏票")
	}
	return u, nil
}

// newGatewayServer 起一个真的 httptest 服务端（真 Handler、共享的 Registry 与 Hub）。
func newGatewayServer(t *testing.T) string {
	t.Helper()

	auth := tokenAuth{
		"t1": {ID: 1, Username: "甲"},
		"t2": {ID: 2, Username: "乙"},
		"t3": {ID: 3, Username: "丙"},
		"t4": {ID: 4, Username: "丁"},
		"t5": {ID: 5, Username: "戊"},
	}
	registry := NewRegistry()
	hub := room.NewHub(registry.Send, nil)
	t.Cleanup(hub.Close)

	srv := httptest.NewServer(NewHandler(auth, registry, hub))
	t.Cleanup(srv.Close)

	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// wire 是一条已认证的测试连接：events 里是它收到的后续事件。
type wire struct {
	t      *testing.T
	ctx    context.Context
	conn   *websocket.Conn
	events chan Envelope

	mu   sync.Mutex
	gaps []string // seq 不连续的地方（正常情况下永远是空的）
}

// dial 连上并按协议 §4.1 走完握手：hello → auth → authed。
func dial(t *testing.T, url, token string) *wire {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	conn.SetReadLimit(readLimit)

	if env := readOne(t, ctx, conn); env.Type != EventHello {
		t.Fatalf("第一条应当是 hello，实际 %s", env.Type)
	}
	writeCmd(t, ctx, conn, CmdAuth, map[string]any{"token": token})
	if env := readOne(t, ctx, conn); env.Type != EventAuthed {
		t.Fatalf("auth 之后应当是 authed，实际 %s", env.Type)
	}

	w := &wire{t: t, ctx: ctx, conn: conn, events: make(chan Envelope, 1024)}
	go w.pump()
	return w
}

// pump 把这条连接收到的后续事件泵进 events，顺带按收件人的流守住 seq 连续
// —— 有洞就记下来，测试末尾断言（协议 §1.4、§4.4）。
func (w *wire) pump() {
	defer close(w.events)

	last := 0
	for {
		env, _, err := readCommand(w.ctx, w.conn)
		if err != nil {
			return
		}
		// 心跳回执（协议 §4.3）：不回的话 15 秒后服务端按 4408 关连接。
		if env.Type == protocol.EventPing {
			if err := wsjson.Write(w.ctx, w.conn, Envelope{Type: protocol.EventPong}); err != nil {
				return
			}
			continue
		}
		if env.Seq != 0 { // hello / authed / ping / pong / error 不带 seq
			if env.Seq != last+1 {
				w.noteGap(last, env)
			}
			last = env.Seq
		}
		select {
		case w.events <- env:
		case <-w.ctx.Done():
			return
		}
	}
}

func (w *wire) noteGap(last int, env Envelope) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.gaps = append(w.gaps, "seq "+strconv.Itoa(last)+" 之后是 "+strconv.Itoa(env.Seq)+"（"+string(env.Type)+"）")
}

func (w *wire) assertNoGaps() {
	w.t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, g := range w.gaps {
		w.t.Errorf("这条连接的 seq 有洞：%s", g)
	}
}

func (w *wire) send(typ protocol.Command, data any) {
	w.t.Helper()
	writeCmd(w.t, w.ctx, w.conn, typ, data)
}

// await 等到一条指定类型的事件，途中的别的类型直接丢掉。
func (w *wire) await(typ protocol.Command) Envelope {
	w.t.Helper()
	return w.awaitIf(func(env Envelope) bool { return env.Type == typ }, string(typ))
}

// awaitReady 等"我这个座位已经准备好"的那一条广播。
//
// 不能只看类型：room.readyChanged 是广播，先到的可能是别人的那一条，于是循环以为
// 自己准备好了，而房间还没收到自己的指令 —— 房主随后开局的就会被拒。
func (w *wire) awaitReady(seat int) {
	w.t.Helper()
	w.awaitIf(func(env Envelope) bool {
		if env.Type != protocol.EventRoomReadyChanged {
			return false
		}
		m := w.data(env)
		return m["seat"] == float64(seat) && m["ready"] == true
	}, "room.readyChanged(自己这一位)")
}

// awaitIf 等到一条满足条件的事件。
func (w *wire) awaitIf(match func(Envelope) bool, what string) Envelope {
	w.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case env, ok := <-w.events:
			if !ok {
				w.t.Fatalf("等 %s 时连接已经关了", what)
			}
			if match(env) {
				return env
			}
		case <-deadline:
			w.t.Fatalf("等 %s 超时", what)
		}
	}
}

// data 把事件体解成 map，便于断言字段。
func (w *wire) data(env Envelope) map[string]any {
	w.t.Helper()
	var m map[string]any
	if err := json.Unmarshal(env.Data, &m); err != nil {
		w.t.Fatalf("事件体解不开（%s）: %v", env.Type, err)
	}
	return m
}

// expectError 等一条错误事件并核对码与来源指令（协议 §6.1）。
func (w *wire) expectError(ref protocol.Command, code ErrorCode) {
	w.t.Helper()
	m := w.data(w.await(EventError))
	if m["code"] != string(code) || m["ref"] != string(ref) {
		w.t.Errorf("%s 的错误 = %v，期望 %s / %s", ref, m, code, ref)
	}
}

func readOne(t *testing.T, ctx context.Context, c *websocket.Conn) Envelope {
	t.Helper()
	env, _, err := readCommand(ctx, c)
	if err != nil {
		t.Fatalf("读事件失败: %v", err)
	}
	return env
}

func writeCmd(t *testing.T, ctx context.Context, c *websocket.Conn, typ protocol.Command, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("编码 %s 的 data 失败: %v", typ, err)
	}
	if err := wsjson.Write(ctx, c, Envelope{Type: typ, Data: raw}); err != nil {
		t.Fatalf("发 %s 失败: %v", typ, err)
	}
}

// TestFourAccountsPlayAFullGame 把房间与对局接到 WebSocket 上之后，四个账号能真打一场。
//
// 四个假玩家只做两件事：自己的摸牌摸切，鸣牌窗口一律过。四小局都走荒牌流局 ——
// 对网关来说这是最有代表性的一条路径：每局走完牌山、听牌料、连庄判定，最后 gameEnd。
func TestFourAccountsPlayAFullGame(t *testing.T) {
	url := newGatewayServer(t)
	conns := [4]*wire{
		dial(t, url, "t1"), dial(t, url, "t2"), dial(t, url, "t3"), dial(t, url, "t4"),
	}

	// ── 大厅：建房、加入、准备、开局 ──
	conns[0].send(protocol.CmdRoomCreate, map[string]any{})
	roomID, _ := conns[0].data(conns[0].await(protocol.EventRoomCreated))["roomId"].(string)
	if roomID == "" {
		t.Fatal("room.created 里没有 roomId")
	}

	for i := 1; i < 4; i++ {
		conns[i].send(protocol.CmdRoomJoin, map[string]any{"roomId": roomID})
		m := conns[i].data(conns[i].await(protocol.EventRoomJoined))
		if m["roomId"] != roomID || m["mySeat"] != float64(i) {
			t.Fatalf("座位 %d 的 room.joined = %v", i, m)
		}
	}
	for i := range conns {
		conns[i].send(protocol.CmdRoomReady, map[string]any{"ready": true})
		conns[i].awaitReady(i)
	}
	conns[0].send(protocol.CmdRoomStart, map[string]any{})
	for i := range conns {
		m := conns[i].data(conns[i].await(protocol.EventGameStarted))
		if m["mySeat"] != float64(i) {
			t.Errorf("座位 %d 的 game.started.mySeat = %v", i, m["mySeat"])
		}
		if hand, _ := m["myHand"].(string); hand == "" {
			t.Errorf("座位 %d 的 game.started 里没有自己的手牌", i)
		}
	}

	// ── 打：把四条连接的事件并成一条流，按事件驱动四个假玩家 ──
	all := make(chan struct {
		seat int
		env  Envelope
	}, 256)
	for i := range conns {
		go func() {
			for env := range conns[i].events {
				all <- struct {
					seat int
					env  Envelope
				}{i, env}
			}
		}()
	}

	ended := map[int]bool{}
	rounds := map[int]int{} // 每个座位收到的 game.roundEnd 数
	draws := 0              // 四个人的摸牌次数之和
	deadline := time.After(120 * time.Second)
	for len(ended) < 4 {
		select {
		case ev := <-all:
			w := conns[ev.seat]
			switch ev.env.Type {
			case protocol.EventGameDrew:
				// 只有摸牌者本人那份带 tile（协议 §6.4）；别家的 event 里没有它。
				tile, _ := w.data(ev.env)["tile"].(string)
				if tile == "" {
					continue
				}
				draws++
				w.send(protocol.CmdGameDiscard, map[string]any{"tile": tile})

			case protocol.EventGameClaimWindow:
				w.send(protocol.CmdGamePass, map[string]any{})

			case protocol.EventGameRoundEnd:
				rounds[ev.seat]++

			case protocol.EventGameGameEnd:
				m := w.data(ev.env)
				points, _ := m["points"].([]any)
				if len(points) != 4 {
					t.Errorf("game.gameEnd.points = %v", m["points"])
				}
				sum := 0.0
				for _, p := range points {
					sum += p.(float64)
				}
				if sum != 0 {
					t.Errorf("ポイント之和应当是 0，实际 %v", sum)
				}
				ended[ev.seat] = true
			}
		case <-deadline:
			t.Fatalf("两分钟没打完：四个人收到的 game.gameEnd 数 = %d", len(ended))
		}
	}

	// 一场是四小局，每小局都要把牌山摸空（荒牌流局）。少了这两条断言，
	// 一个"开局就结束"的退化也能让上面的循环转完。
	for i := range conns {
		if rounds[i] != 4 {
			t.Errorf("座位 %d 收到 %d 次 game.roundEnd，期望 4", i, rounds[i])
		}
	}
	if draws < 200 {
		t.Errorf("四个人一共只摸了 %d 次牌，四小局摸空牌山该有 200 次以上", draws)
	}

	// 打完这一场，四个人的流都不能有洞（协议 §1.4、§4.4）。
	for i := range conns {
		conns[i].assertNoGaps()
	}
}

// 错误码逐条走一遍（协议 §2.6）。
func TestGatewayErrorCodes(t *testing.T) {
	url := newGatewayServer(t)
	a, b := dial(t, url, "t1"), dial(t, url, "t2")

	// 不在任何房间。
	a.send(protocol.CmdRoomReady, map[string]any{"ready": true})
	a.expectError(protocol.CmdRoomReady, CodeNotInRoom)
	a.send(protocol.CmdGameDiscard, map[string]any{"tile": "5p"})
	a.expectError(protocol.CmdGameDiscard, CodeNotInRoom)

	// 房间号不存在（大小写不该决定成败，所以这里用小写）。
	a.send(protocol.CmdRoomJoin, map[string]any{"roomId": "zzzzzz"})
	a.expectError(protocol.CmdRoomJoin, CodeRoomNotFound)

	// 建房，然后第二次建房要被拦：一个人不能同时在两个房间。
	a.send(protocol.CmdRoomCreate, map[string]any{})
	roomID, _ := a.data(a.await(protocol.EventRoomCreated))["roomId"].(string)
	a.send(protocol.CmdRoomCreate, map[string]any{})
	a.expectError(protocol.CmdRoomCreate, CodeAlreadyInRoom)

	// 人没满就开局 = ILLEGAL_ACTION；不是房主开局 = NOT_ROOM_OWNER
	// （doStart 先查房主，再查人数）。
	a.send(protocol.CmdRoomStart, map[string]any{})
	a.expectError(protocol.CmdRoomStart, CodeIllegalAction)
	b.send(protocol.CmdRoomJoin, map[string]any{"roomId": roomID})
	b.await(protocol.EventRoomJoined)
	b.send(protocol.CmdRoomStart, map[string]any{})
	b.expectError(protocol.CmdRoomStart, CodeNotRoomOwner)

	// 在房间里但没有对局。
	a.send(protocol.CmdGameSync, map[string]any{})
	a.expectError(protocol.CmdGameSync, CodeIllegalAction)

	// 字段缺失与字段值不对都是 BAD_REQUEST。
	a.send(protocol.CmdRoomReady, map[string]any{})
	a.expectError(protocol.CmdRoomReady, CodeBadRequest)
	a.send(protocol.CmdGameKan, map[string]any{"kind": "banana"})
	a.expectError(protocol.CmdGameKan, CodeBadRequest)
	a.send(protocol.CmdGameDiscard, map[string]any{"tile": "5x"})
	a.expectError(protocol.CmdGameDiscard, CodeBadRequest)
	a.send(protocol.CmdGameChi, map[string]any{"tiles": []string{"3s"}})
	a.expectError(protocol.CmdGameChi, CodeBadRequest)

	// 不认识的指令：回 UNKNOWN_COMMAND，而且连接留着（下面这条还要能用）。
	a.send(protocol.Command("game.dance"), map[string]any{})
	a.expectError(protocol.Command("game.dance"), CodeUnknownCommand)
	a.send(protocol.CmdRoomReady, map[string]any{"ready": true})
	a.awaitReady(0)

	// 满四人之后再进一个 → ROOM_FULL（a 与 b 已经在里面）。
	c, d := dial(t, url, "t3"), dial(t, url, "t4")
	for _, w := range []*wire{c, d} {
		w.send(protocol.CmdRoomJoin, map[string]any{"roomId": roomID})
		w.await(protocol.EventRoomJoined)
	}
	fifth := dial(t, url, "t5")
	fifth.send(protocol.CmdRoomJoin, map[string]any{"roomId": roomID})
	fifth.expectError(protocol.CmdRoomJoin, CodeRoomFull)
}

// 顶号 + 重连：刷新页面/换标签页之后，座位必须还在，而且不能被标成离线。
func TestReconnectKeepsSeatAndIsNotAnOffline(t *testing.T) {
	url := newGatewayServer(t)

	owner := dial(t, url, "t1")
	owner.send(protocol.CmdRoomCreate, map[string]any{})
	roomID, _ := owner.data(owner.await(protocol.EventRoomCreated))["roomId"].(string)

	other := dial(t, url, "t2")
	other.send(protocol.CmdRoomJoin, map[string]any{"roomId": roomID})
	other.await(protocol.EventRoomJoined)

	// 同一个账号再开一条连接 → 旧的被顶掉（协议 §4.6 的 4403）。
	again := dial(t, url, "t1")

	// 新连接要重新拿到房间与自己的座位：刷新过的页面手上什么都没有。
	m := again.data(again.await(protocol.EventRoomJoined))
	if m["roomId"] != roomID || m["mySeat"] != float64(0) {
		t.Errorf("重连的 room.joined = %v", m)
	}

	// 旧连接被顶掉的过程中会摘自己 —— 那不算掉线，座位不能变成 connected=false
	// （少了 Registry.Remove 的返回值判断，这里就会收到一条假的掉线广播）。
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case env := <-other.events:
			if env.Type != protocol.EventRoomSeats {
				continue
			}
			seats, _ := other.data(env)["seats"].([]any)
			if len(seats) == 0 || seats[0] == nil {
				continue
			}
			if seats[0].(map[string]any)["connected"] == false {
				t.Fatalf("顶号被当成掉线了：%v", seats[0])
			}
		case <-deadline:
			// 观察窗口内没有出现假的掉线广播。
			again.send(protocol.CmdRoomReady, map[string]any{"ready": true})
			again.awaitReady(0)
			return
		}
	}
}
