package room

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/protocol"
	"github.com/aleksey-45/majspirit/internal/user"
)

// TestMain 把日志调成静音：房间会在正常路径上打不少 Info（掉线、保座超时、
// 房间销毁），测试输出里全是它们反而看不见真正的失败。
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

// fakeNotifier 记下每个用户收到的信封。
//
// 它换来的是：整套房间逻辑不必连任何东西就能测 —— 四个假玩家走完大厅流程，
// 不碰端口、不碰网络 goroutine。这正是 `room` 不 import `ws` 要换的东西。
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
	// 深拷贝 data：调用方可能复用底层数组。
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

// seatsOf 取出 room.seats / room.joined 里的座位数组。
func seatsOf(t *testing.T, m map[string]any) []any {
	t.Helper()
	raw, ok := m["seats"].([]any)
	if !ok {
		t.Fatalf("payload 里没有 seats 数组: %v", m)
	}
	return raw
}

// userAt 取出座位数组里某一项，空座位返回 nil。
func userAt(t *testing.T, seats []any, i int) map[string]any {
	t.Helper()
	if i >= len(seats) {
		t.Fatalf("座位数组只有 %d 项", len(seats))
	}
	if seats[i] == nil {
		return nil
	}
	m, ok := seats[i].(map[string]any)
	if !ok {
		t.Fatalf("第 %d 个座位不是对象: %v", i, seats[i])
	}
	return m
}

func testUser(id int64, name string) user.User {
	return user.User{ID: id, Username: name}
}

// newTestRoom 起一个 Hub + 一个房间。hold 是保座时长（测试不能真等 2 分钟）。
func newTestRoom(t *testing.T, hold time.Duration) (*Hub, *Room, *fakeNotifier) {
	t.Helper()
	return newTestRoomWith(t, tuning{hold: hold, turnLimit: time.Second, turnBase: time.Second})
}

// newTestRoomWith 同上，但整套参数由调用方给 —— 对局用例要改回合时限、摆牌、改局数。
func newTestRoomWith(t *testing.T, tune tuning) (*Hub, *Room, *fakeNotifier) {
	t.Helper()
	n := newFakeNotifier()
	h := NewHub(n, nil)
	t.Cleanup(h.Close)

	r := newRoom("TEST01", testUser(1, "甲"), h, tune)
	return h, r, n
}
