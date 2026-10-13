package ws

import (
	"errors"
	"net"
	"testing"
	"time"
)

// send_test.go —— Send 的契约：**投递方绝不等待**。
//
// 这条契约是阶段四"一个人的网卡不能拖住整桌"的地基：房间的广播循环是全房间唯一的
// goroutine，它只要在一个慢连接上多等一会儿，另外三家就一起停。

// TestSendIsNonBlocking 队列满时必须立刻返回 ErrSlowConsumer，而不是等下去。
//
// 注意它用的是 fakeClient —— 那个 Client 的 `ws` 是 nil。这条能成立，恰恰因为
// Send 完全不碰网络、只碰队列；哪天有人在 Send 里加一次网络调用，这里会立刻炸。
//
// 变异：把 Send 换回 select + ctx 超时（原来的写法），这条测试会挂 10 秒。
func TestSendIsNonBlocking(t *testing.T) {
	c := fakeClient()
	for range cap(c.send) {
		c.send <- Envelope{Type: EventHello}
	}

	start := time.Now()
	err := c.Send(Envelope{Type: EventHello})
	elapsed := time.Since(start)

	if !errors.Is(err, ErrSlowConsumer) {
		t.Errorf("队列满时 err = %v, 期望 ErrSlowConsumer", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("Send 在队列满时阻塞了 %v —— 投递方不该等网络", elapsed)
	}
}

// TestSendDistinguishesClosedFromFull 这两种失败要能分开：
// 「连接已经关了」不用管；「队列满」说明对端已经不读了，是另一回事。
// 合成一个错误的话，调用方就没有依据决定该怎么办。
func TestSendDistinguishesClosedFromFull(t *testing.T) {
	closed := fakeClient()
	close(closed.done) // 直接关 done：fakeClient 的 ws 是 nil，走 Close 会炸

	if err := closed.Send(Envelope{Type: EventHello}); !errors.Is(err, net.ErrClosed) {
		t.Errorf("关闭后 Send 返回 %v, 期望 net.ErrClosed", err)
	}

	full := fakeClient()
	for range cap(full.send) {
		full.send <- Envelope{Type: EventHello}
	}
	err := full.Send(Envelope{Type: EventHello})
	if errors.Is(err, net.ErrClosed) {
		t.Errorf("队列满时不该报成已关闭：%v", err)
	}
	if !errors.Is(err, ErrSlowConsumer) {
		t.Errorf("队列满时 err = %v, 期望 ErrSlowConsumer", err)
	}
}

// TestSendKeepsOrderWhenNotFull 是上面两条的对照面：队列有空位时，
// 非阻塞改造不能顺手把"投递成功"也搞坏。少了这条，一个"永远返回错误"的
// Send 也能让上面两条绿。
func TestSendKeepsOrderWhenNotFull(t *testing.T) {
	c := fakeClient()
	want := []Command{EventHello, EventAuthed, EventPong}

	for _, cmd := range want {
		if err := c.Send(Envelope{Type: cmd}); err != nil {
			t.Fatalf("队列没满，Send 不该失败: %v", err)
		}
	}
	for i, cmd := range want {
		select {
		case got := <-c.send:
			if got.Type != cmd {
				t.Errorf("第 %d 条是 %q, 期望 %q（顺序即投递顺序）", i, got.Type, cmd)
			}
		default:
			t.Fatalf("第 %d 条没进队列", i)
		}
	}
}
