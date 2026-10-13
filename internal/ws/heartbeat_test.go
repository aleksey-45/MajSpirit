package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// heartbeat_test.go —— 心跳的两条路径，以及"对方不守协议"时连接会被关掉。
//
// 这些在写之前没有任何证据：默认心跳间隔 15 秒，其它测试几毫秒就结束了，
// 谁都没真的触发过一次心跳。

// TestClientAnswersPingStaysConnected 正常心跳：客户端回 pong，连接必须活着。
//
// 判据是"能读到并回复若干条 ping"，不是"连接没关"：后者在心跳代码被整个删掉
// 时也成立，那种实现会照样绿。
func TestClientAnswersPingStaysConnected(t *testing.T) {
	url := newTestServerWithHeartbeat(t, &fakeAuth{u: user.User{ID: 1, Username: "x"}},
		2*time.Second, 30*time.Millisecond, 500*time.Millisecond)
	ctx, _ := baseCtx(t, 10*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c) // hello
	writeAuth(t, ctx, c, "好票")
	if env, _ := readEnvelope(t, ctx, c); env.Type != EventAuthed {
		t.Fatalf("期望 authed, 实际 %q", env.Type)
	}

	// 回几条 pong，跨过好几个心跳周期。中途任何一次读失败都说明连接被误关了。
	for pings := 0; pings < 4; {
		env, _ := readEnvelope(t, ctx, c)
		if env.Type != EventPing {
			continue
		}
		pings++
		if err := wsjson.Write(ctx, c, Envelope{
			Type: EventPong,
			Data: mustJSON(pongEvent{ServerTime: time.Now().UnixMilli()}),
		}); err != nil {
			t.Fatalf("第 %d 次回 pong 失败: %v", pings, err)
		}
	}
}

// TestSilentClientGetsHeartbeatTimeout 对端不回 pong：必须被 4408 关掉。
//
// 这条防的是"假活连接"：对端进程冻死或网线拔掉时，服务端这边的 socket
// 看起来仍是好的 —— 靠心跳发现，并把 goroutine、fd、发送缓冲收回来。
func TestSilentClientGetsHeartbeatTimeout(t *testing.T) {
	url := newTestServerWithHeartbeat(t, &fakeAuth{u: user.User{ID: 1, Username: "x"}},
		2*time.Second, 50*time.Millisecond, 50*time.Millisecond)
	ctx, _ := baseCtx(t, 10*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c) // hello
	writeAuth(t, ctx, c, "好票")
	if env, _ := readEnvelope(t, ctx, c); env.Type != EventAuthed {
		t.Fatalf("期望 authed, 实际 %q", env.Type)
	}

	// 只收不发：服务端的 ping 没人回 pong，服务端到点该以 4408 关闭。
	// 收到的 ping 消息本身不重要，要等的是关闭帧 —— 所以读到出错为止。
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sawPing := false
	var closeErr error
	for {
		env, err := readEnvelopeOrErr(pctx, c)
		if err != nil {
			closeErr = err
			break
		}
		if env.Type == EventPing {
			sawPing = true
		}
	}

	if !sawPing {
		t.Fatal("服务端一次 ping 都没发 —— 心跳没跑起来，这条测试什么都没测到")
	}
	if got := websocket.CloseStatus(closeErr); got != statusHeartbeatTimeout {
		t.Errorf("关闭码 = %d, 期望 %d（4408 心跳超时）；err=%v", got, statusHeartbeatTimeout, closeErr)
	}
}

// TestClientPingGetsPong 客户端主动 ping 时必须立刻拿到 pong（协议 §4.3）。
//
// 这跟上面那条是**两个方向**：那条里服务端是提问方、客户端只回答 pong；
// 这条里客户端提问。少了回 pong 这一支，客户端自己的探活永远等不到答案，
// 只能把一条好连接判成断线。
func TestClientPingGetsPong(t *testing.T) {
	url := newTestServer(t, &fakeAuth{u: user.User{ID: 1, Username: "x"}}, 2*time.Second)
	ctx, _ := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c) // hello
	writeAuth(t, ctx, c, "好票")
	if env, _ := readEnvelope(t, ctx, c); env.Type != EventAuthed {
		t.Fatalf("期望 authed, 实际 %q", env.Type)
	}

	if err := wsjson.Write(ctx, c, Envelope{Type: CmdPing, Data: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("发 ping 失败: %v", err)
	}

	env, _ := readEnvelope(t, ctx, c)
	if env.Type != EventPong {
		t.Fatalf("期望 pong, 实际 %q", env.Type)
	}

	var p pongEvent
	if err := json.Unmarshal(env.Data, &p); err != nil {
		t.Fatalf("解 pong 的 data 失败: %v", err)
	}
	// serverTime 是客户端校准倒计时的唯一依据（协议 §2.4），缺了它 pong 就白回。
	if p.ServerTime <= 0 {
		t.Errorf("pong.serverTime = %d, 期望一个正的毫秒时间戳", p.ServerTime)
	}
}

// TestBinaryFrameIsProtocolError 非文本帧按 4400 关连接（协议 §2.1 定的是 JSON）。
func TestBinaryFrameIsProtocolError(t *testing.T) {
	url := newTestServer(t, &fakeAuth{u: user.User{ID: 1, Username: "x"}}, 2*time.Second)
	ctx, _ := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c)
	writeAuth(t, ctx, c, "好票")
	if env, _ := readEnvelope(t, ctx, c); env.Type != EventAuthed {
		t.Fatalf("期望 authed, 实际 %q", env.Type)
	}

	if err := c.Write(ctx, websocket.MessageBinary, []byte{0x01, 0x02}); err != nil {
		t.Fatalf("发二进制帧失败: %v", err)
	}

	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != statusProtocolError {
		t.Errorf("关闭码 = %d, 期望 %d（4400 协议错误）；err=%v",
			websocket.CloseStatus(err), statusProtocolError, err)
	}
}

// TestMalformedJSONIsProtocolError 信封都解不出来的消息按 4400 关连接。
func TestMalformedJSONIsProtocolError(t *testing.T) {
	url := newTestServer(t, &fakeAuth{u: user.User{ID: 1, Username: "x"}}, 2*time.Second)
	ctx, _ := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c)
	writeAuth(t, ctx, c, "好票")
	if env, _ := readEnvelope(t, ctx, c); env.Type != EventAuthed {
		t.Fatalf("期望 authed, 实际 %q", env.Type)
	}

	if err := c.Write(ctx, websocket.MessageText, []byte("{不是 JSON")); err != nil {
		t.Fatalf("发坏 JSON 失败: %v", err)
	}

	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != statusProtocolError {
		t.Errorf("关闭码 = %d, 期望 %d（4400 协议错误）；err=%v",
			websocket.CloseStatus(err), statusProtocolError, err)
	}
}
