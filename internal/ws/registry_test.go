package ws

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/user"
)

// registry_test.go —— 注册表的三件事：发消息、摘连接、并发下的正确性。
//
// 前两条不需要网络：Registry.Send 只是把消息投进 Client.send，
// 所以直接构造 Client 就能测，不必起服务端。

func fakeClient() *Client { return newClient(nil, nil, slog.Default()) }

func TestRegistrySend(t *testing.T) {
	r := NewRegistry()
	u := user.User{ID: 7, Username: "seven"}
	c := fakeClient()

	if err := r.Send(u.ID, Envelope{Type: EventHello}); err != ErrOffline {
		t.Errorf("没登记时应返回 ErrOffline, 实际 %v", err)
	}

	r.Add(u, c)

	msg := Envelope{Type: EventAuthed}
	if err := r.Send(u.ID, msg); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	select {
	case got := <-c.send:
		if got.Type != msg.Type {
			t.Errorf("收到 %q, 期望 %q", got.Type, msg.Type)
		}
	default:
		t.Error("消息没进写队列")
	}
}

// TestRegistryRemoveKeepsReplacement 守住"顶号"之后摘连接的那个坑。
//
// 旧连接被新连接顶掉后，它自己也会走到 Remove。如果 Remove 无条件删，
// 删掉的是 map 里那条**新**连接 —— 新连接从此收不到任何消息，
// 而且不报错、不断线，只是"静默收不到"。
func TestRegistryRemoveKeepsReplacement(t *testing.T) {
	r := NewRegistry()
	u := user.User{ID: 7, Username: "seven"}
	old, fresh := fakeClient(), fakeClient()

	r.Add(u, old)
	if got := r.Add(u, fresh); got != old {
		t.Fatalf("Add 应返回被顶掉的旧连接, 实际 %v", got)
	}

	// 旧连接随后自己退出，来摘自己。
	r.Remove(u.ID, old)

	if r.Len() != 1 {
		t.Fatalf("摘掉旧连接后应还剩 1 条, 实际 %d", r.Len())
	}
	if err := r.Send(u.ID, Envelope{Type: EventHello}); err != nil {
		t.Errorf("新连接被旧连接一起摘掉了: %v", err)
	}
}

// TestRegistryConcurrent 并发读写。跑在 -race 下才有意义 ——
// 阶段四的房间层会有多个 goroutine 同时查、加、摘。
func TestRegistryConcurrent(t *testing.T) {
	r := NewRegistry()
	done := make(chan struct{})

	for i := range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			u := user.User{ID: int64(i % 3)} // 故意让多个 goroutine 抢同一个用户
			for range 200 {
				c := fakeClient()
				r.Add(u, c)
				_ = r.Send(u.ID, Envelope{Type: EventHello})
				r.Remove(u.ID, c)
			}
		}()
	}
	for range 8 {
		<-done
	}
}

// TestRegistrySlowClientDoesNotBlockOthers 守住"一个慢客户端不拖住其他人"。
//
// 这条测试换过一次靶子，值得记下来：它原来防的是「在锁里发送」—— 那时 Send 会
// 阻塞，锁一拿就是全网停摆。Send 改成非阻塞之后那个风险从根上消失了，
// 所以它现在防的是**退化成阻塞**：队列满了必须立刻返回 ErrSlowConsumer，
// 而不是等上几秒（阶段四房间里那个唯一的 goroutine 会被它拖住）。
func TestRegistrySlowClientDoesNotBlockOthers(t *testing.T) {
	r := NewRegistry()
	slow, fast := fakeClient(), fakeClient()
	r.Add(user.User{ID: 1}, slow)
	r.Add(user.User{ID: 2}, fast)

	// 灌满 slow 的写队列，且不起写 goroutine。
	for range cap(slow.send) {
		slow.send <- Envelope{Type: EventHello}
	}

	slowErr := make(chan error, 1)
	go func() { slowErr <- r.Send(1, Envelope{Type: EventHello}) }()

	select {
	case err := <-slowErr:
		if !errors.Is(err, ErrSlowConsumer) {
			t.Errorf("队列满时应返回 ErrSlowConsumer, 实际 %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("队列满了却阻塞住 —— 房间那个唯一的 goroutine 会被一个不读的对端拖垮")
	}

	done := make(chan error, 1)
	go func() { done <- r.Send(2, Envelope{Type: EventHello}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("给另一个用户发消息失败: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("一个慢客户端把别人的消息堵住了")
	}
}
