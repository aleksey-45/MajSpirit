package ws

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/auth"

	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// TestReadLimitRejectsOversizedMessage 守住"超大消息按 1009 关连接"。
//
// 库默认只允许 32768 字节（read.go:107），而协议 §6.2 的 game.snapshot
// 粗估 2–4 KB —— 现在够用，但一旦某条消息长大，症状是客户端随机掉线，
// 本地测小消息永远复现不了。所以这条测的是"上限确实存在且生效"，
// 不是"上限的具体数值"。
func TestReadLimitRejectsOversizedMessage(t *testing.T) {
	url := newTestServer(t, &fakeAuth{u: user.User{ID: 1, Username: "x"}}, 5*time.Second)
	ctx, _ := baseCtx(t, 10*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c) // hello
	if err := wsjson.Write(ctx, c, Envelope{
		Type: CmdAuth,
		Data: json.RawMessage(`{"token":"好票"}`),
	}); err != nil {
		t.Fatalf("发 auth 失败: %v", err)
	}
	if env, _ := readEnvelope(t, ctx, c); env.Type != EventAuthed {
		t.Fatalf("期望 authed, 实际 %q", env.Type)
	}

	// 超过 readLimit 的一条消息。raw 不编码成 JSON，直接当文本帧发出去 ——
	// 服务端在读的层就拒，轮不到解信封。
	oversized := strings.Repeat("a", readLimit+1024)
	if err := c.Write(ctx, websocket.MessageText, []byte(oversized)); err != nil {
		t.Fatalf("发超大消息失败: %v", err)
	}

	_, _, err := c.Read(ctx)
	if got := websocket.CloseStatus(err); got != websocket.StatusMessageTooBig {
		t.Errorf("关闭码 = %d, 期望 %d（1009 消息过大）；err=%v", got, websocket.StatusMessageTooBig, err)
	}
	// 不在这里断言服务端日志：这一刻服务端的读循环可能还没走到记录那一步，
	// 断言它等于给测试加一个随机失败的时序依赖。这条测试的不变量只有关闭码。
}

// TestErrorFramePrecedesCloseFrame 守住帧的顺序：**先给答案，再关门**。
//
// 这条是实测踩出来的 bug：把写路径收进 channel 之后，如果关闭帧由调用方
// 直接从自己的 goroutine 发，它会插到已排队的 error 前面 —— 客户端先收到
// 关闭帧、那条 error 被随后的 CloseNow 掐掉。症状是"错票只看到 4401，
// 看不到 AUTH_FAILED 的原因"。
//
// 断言方式刻意选"两条都读到"，而不是"关闭码是 4401"：
// 后者在顺序反了的时候照样会过（关闭码本身没变）。
func TestErrorFramePrecedesCloseFrame(t *testing.T) {
	url := newTestServer(t, &fakeAuth{err: auth.ErrInvalidToken}, time.Second)
	ctx, _ := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c) // hello
	if err := wsjson.Write(ctx, c, Envelope{
		Type: CmdAuth,
		Data: json.RawMessage(`{"token":"坏票"}`),
	}); err != nil {
		t.Fatalf("发 auth 失败: %v", err)
	}

	// ① error 帧必须在关闭帧之前到达。
	env, _ := readEnvelope(t, ctx, c)
	if env.Type != EventError {
		t.Fatalf("第一条应该是 error（先给答案），实际 %q —— 顺序反了", env.Type)
	}
	var e errorEvent
	if err := json.Unmarshal(env.Data, &e); err != nil {
		t.Fatalf("解 error 的 data 失败: %v", err)
	}
	if e.Code != CodeAuthFailed {
		t.Errorf("code = %q, 期望 %q", e.Code, CodeAuthFailed)
	}

	// ② 然后才是关闭帧。
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != statusAuthFailed {
		t.Errorf("关闭码 = %d, 期望 %d（err=%v）", websocket.CloseStatus(err), statusAuthFailed, err)
	}
}
