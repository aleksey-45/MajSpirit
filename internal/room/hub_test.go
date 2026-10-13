package room

import (
	"strings"
	"sync"
	"testing"

	"github.com/aleksey-45/majspirit/internal/protocol"
)

// hub_test.go —— 房间注册表：房间号可念、并发创建不撞号、关掉之后不再受理。

func TestCreateMakesReadableIDs(t *testing.T) {
	n := newFakeNotifier()
	h := NewHub(n.Send, nil)
	defer h.Close()

	seen := make(map[string]bool)
	for range 200 {
		r, err := h.Create(testUser(1, "甲"))
		if err != nil {
			t.Fatalf("建房间失败: %v", err)
		}

		id := r.ID()
		if len(id) != idLen {
			t.Fatalf("房间号 %q 长度 = %d, 期望 %d", id, len(id), idLen)
		}
		// 房间号是要**口头报出来**的（验收、开黑、客服都靠嘴传），
		// 0/O 与 1/I 在口头传递里必然出错，所以一个都不许出现。
		if strings.ContainsAny(id, "0O1I") {
			t.Fatalf("房间号 %q 含 0/O/1/I", id)
		}
		for _, c := range id {
			if !strings.ContainsRune(idAlphabet, c) {
				t.Fatalf("房间号 %q 出现字母表外的字符 %q", id, c)
			}
		}
		if seen[id] {
			t.Fatalf("房间号 %q 重复了", id)
		}
		seen[id] = true

		if r.ID() != id {
			t.Fatalf("r.ID() = %q, 期望 %q", r.ID(), id)
		}
	}
}

func TestLookupFindsAndMisses(t *testing.T) {
	n := newFakeNotifier()
	h := NewHub(n.Send, nil)
	defer h.Close()

	r, err := h.Create(testUser(1, "甲"))
	if err != nil {
		t.Fatal(err)
	}

	got, ok := h.Lookup(r.ID())
	if !ok || got != r {
		t.Fatalf("Lookup(%q) = %v, %v, 期望拿到同一个房间", r.ID(), got, ok)
	}
	if _, ok := h.Lookup("不存在"); ok {
		t.Error("不存在的房间号不该查得到")
	}
}

// TestHubConcurrentCreate 是阶段四 §4.1 的验收项：并发创建 100 个房间不出错。
//
// 这条要在 `-race` 下跑才有意义 —— 没有 actor 的话，"创建"和"销毁"会同时改同一张
// map，而这个错在普通跑法下多半是绿的（就像真的 map 并发写一样：偶尔崩，多数时候
// 看起来没事）。
func TestHubConcurrentCreate(t *testing.T) {
	n := newFakeNotifier()
	h := NewHub(n.Send, nil)

	const workers, each = 10, 10

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		ids = make(map[string]bool)
	)
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range each {
				r, err := h.Create(testUser(int64(w)+1, "并发"))
				if err != nil {
					t.Errorf("并发建房间失败: %v", err)
					return
				}
				mu.Lock()
				if ids[r.ID()] {
					t.Errorf("并发下房间号撞了: %s", r.ID())
				}
				ids[r.ID()] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if got := len(ids); got != workers*each {
		t.Errorf("建了 %d 个房间, 期望 %d", got, workers*each)
	}
	if got := h.Len(); got != workers*each {
		t.Errorf("Hub.Len() = %d, 期望 %d", got, workers*each)
	}

	h.Close()
	// 关完之后不再受理，而且要**立刻**返回，不能等 —— 关闭路径上最忌讳
	// "叫别人别来了"之后自己还在阻塞。
	if _, err := h.Create(testUser(99, "迟到")); err != ErrHubClosed {
		t.Errorf("Hub 关闭后 Create = %v, 期望 ErrHubClosed", err)
	}
}

// 关 Hub 要把它下面所有房间一起关掉，否则那些 goroutine 会一直挂着。
func TestCloseStopsRooms(t *testing.T) {
	n := newFakeNotifier()
	h := NewHub(n.Send, nil)

	r, err := h.Create(testUser(1, "甲"))
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Join(testUser(2, "乙")); err != nil {
		t.Fatal(err)
	}

	h.Close()

	if err := r.Join(testUser(3, "丙")); err != ErrClosed {
		t.Errorf("Hub 关掉之后，它下面的房间还在受理指令: %v", err)
	}
	if _, ok := n.find(2, protocol.EventRoomSeats); !ok {
		t.Error("加入没有被广播")
	}
}
