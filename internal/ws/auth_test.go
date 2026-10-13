package ws

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/auth"
	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// fakeAuth 是 Authenticator 的假实现。
//
// 它换来的是：不用启动真的用户系统、不用造真 JWT，就能精确摆出
// "票无效"和"库挂了"两种返回 —— 这两种在服务端要走同一条对客户端的路子（4401），
// 但日志里必须分得开，只有假实现能稳定地摆出这两种。
type fakeAuth struct {
	u     user.User
	err   error
	calls int
}

func (f *fakeAuth) Authenticate(context.Context, string) (user.User, error) {
	f.calls++
	return f.u, f.err
}

// newTestServer 起一个真的 HTTP 服务端（端口随机），并把认证超时压到很短。
//
// 端口随机 + 生命周期跟着测试走：go test ./... 既不会撞"端口被占"，
// 也不需要谁先手动把服务端起起来。
func newTestServer(t *testing.T, auth Authenticator, timeout time.Duration) string {
	t.Helper()

	h := NewHandler(auth)
	h.timeout = timeout

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// mustDial 连上测试服务端。
func mustDial(t *testing.T, ctx context.Context, url string) *websocket.Conn {
	t.Helper()

	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("连不上 %s: %v", url, err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

// readEnvelope 读一条消息并解出信封。
func readEnvelope(t *testing.T, ctx context.Context, c *websocket.Conn) (Envelope, []byte) {
	t.Helper()

	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("读消息失败: %v", err)
	}
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("不是信封: %v（原文 %s）", err, data)
	}
	return env, data
}

// TestAuthRejectsBadToken 是这一步的核心之一：错票必须以 AUTH_FAILED + 4401 结束。
//
// 只断言"收到 error"是不够的 —— 关闭码才是客户端用来区分"票过期该重新登录"
// 和"网络抖了一下"的依据。少了它，客户端只能一律重连，那会让失效的票反复重试。
func TestAuthRejectsBadToken(t *testing.T) {
	url := newTestServer(t, &fakeAuth{err: auth.ErrInvalidToken}, time.Second)
	ctx, logs := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	if env, _ := readEnvelope(t, ctx, c); env.Type != EventHello {
		t.Fatalf("第一条应该是 hello, 实际 %q", env.Type)
	}

	if err := wsjson.Write(ctx, c, Envelope{
		Type: CmdAuth,
		Data: json.RawMessage(`{"token":"坏票"}`),
	}); err != nil {
		t.Fatalf("发 auth 失败: %v", err)
	}

	env, _ := readEnvelope(t, ctx, c)
	if env.Type != EventError {
		t.Fatalf("期望 error, 实际 %q", env.Type)
	}
	var e errorEvent
	if err := json.Unmarshal(env.Data, &e); err != nil {
		t.Fatalf("解 error 的 data 失败: %v", err)
	}
	if e.Code != CodeAuthFailed {
		t.Errorf("code = %q, 期望 %q", e.Code, CodeAuthFailed)
	}
	if e.Ref != CmdAuth {
		t.Errorf("ref = %q, 期望 %q（前端靠它定位是哪条指令出的错）", e.Ref, CmdAuth)
	}

	// 服务端随后关闭。读到这里应该拿到关闭帧。
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != statusAuthFailed {
		t.Errorf("关闭码 = %d, 期望 %d（err=%v）", websocket.CloseStatus(err), statusAuthFailed, err)
	}

	if !hasLog(logs(), "认证失败") {
		t.Errorf("日志里没有认证失败记录:\n%s", strings.Join(logs(), "\n"))
	}
}

// TestAuthAcceptsGoodToken 是上面那条的对照：票好就必须放行，而且先回 authed。
// 只测失败路径的话，一个"永远拒绝"的实现也能绿。
func TestAuthAcceptsGoodToken(t *testing.T) {
	fake := &fakeAuth{u: user.User{ID: 42, Username: "aleksey"}}
	url := newTestServer(t, fake, time.Second)
	ctx, _ := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c) // hello

	if err := wsjson.Write(ctx, c, Envelope{
		Type: CmdAuth,
		Data: json.RawMessage(`{"token":"好票"}`),
	}); err != nil {
		t.Fatalf("发 auth 失败: %v", err)
	}

	env, _ := readEnvelope(t, ctx, c)
	if env.Type != EventAuthed {
		t.Fatalf("期望 authed, 实际 %q", env.Type)
	}
	var a authedEvent
	if err := json.Unmarshal(env.Data, &a); err != nil {
		t.Fatalf("解 authed 的 data 失败: %v", err)
	}
	if a.UserID != 42 || a.Username != "aleksey" {
		t.Errorf("authed = %+v, 期望 {42 aleksey}", a)
	}
	if fake.calls != 1 {
		t.Errorf("Authenticate 被调了 %d 次, 期望 1 次", fake.calls)
	}
}

// TestAuthRequiredBeforeAuth 守住"协议只要求拒绝，没要求断开"这一条。
//
// 一个把 auth 排在其他消息后面的客户端（比如先发 ping 探活）不该被踢掉 ——
// 它还有时间正确认证。所以这里断言：回了 AUTH_REQUIRED，然后紧接着
// 发正确的 auth 仍然能成功。
func TestAuthRequiredBeforeAuth(t *testing.T) {
	url := newTestServer(t, &fakeAuth{u: user.User{ID: 7, Username: "seven"}}, 2*time.Second)
	ctx, _ := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c) // hello

	// 认证前先发一条别的指令。
	if err := wsjson.Write(ctx, c, Envelope{
		Type: "room.create",
		Data: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("发 room.create 失败: %v", err)
	}

	env, _ := readEnvelope(t, ctx, c)
	if env.Type != EventError {
		t.Fatalf("期望 error, 实际 %q", env.Type)
	}
	var e errorEvent
	if err := json.Unmarshal(env.Data, &e); err != nil {
		t.Fatalf("解 error 的 data 失败: %v", err)
	}
	if e.Code != CodeAuthRequired {
		t.Errorf("code = %q, 期望 %q", e.Code, CodeAuthRequired)
	}
	if e.Ref != "room.create" {
		t.Errorf("ref = %q, 期望 room.create", e.Ref)
	}

	// 连接还活着：补上正确的 auth 应该能通过。
	if err := wsjson.Write(ctx, c, Envelope{
		Type: CmdAuth,
		Data: json.RawMessage(`{"token":"好票"}`),
	}); err != nil {
		t.Fatalf("补发 auth 失败（连接被关掉了？）: %v", err)
	}
	if env, _ := readEnvelope(t, ctx, c); env.Type != EventAuthed {
		t.Errorf("补发 auth 后期望 authed, 实际 %q", env.Type)
	}
}

// TestAuthTimeout 守住首帧方案唯一的真风险：连上不认证的连接必须被清掉。
//
// 没有这条，任何人开一万个空连接就能把 goroutine 和 fd 吃光，
// 而且服务端连"被打了"都不知道。
func TestAuthTimeout(t *testing.T) {
	url := newTestServer(t, &fakeAuth{}, 100*time.Millisecond)
	ctx, _ := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c) // hello

	// 什么都不发，等服务端自己关。
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != statusAuthFailed {
		t.Errorf("关闭码 = %d, 期望 %d（err=%v）", websocket.CloseStatus(err), statusAuthFailed, err)
	}
}

// TestClientGoesAwayIsNotTimeout 守住"别把对方走了当成对方超时"。
//
// 客户端主动断开时，c.Read 返回的错误里 ctx.Err() 还是 nil（finishRead 只在
// ctx 到期时替换错误），所以那条路径必须落进"协议错误/正常结束"而不是"认证超时"。
// 搞混的后果：日志里全是假的"认证超时"，真的超时反而看不出来。
func TestClientGoesAwayIsNotTimeout(t *testing.T) {
	url := newTestServer(t, &fakeAuth{}, 10*time.Second) // 故意设得长长的
	ctx, logs := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	readEnvelope(t, ctx, c) // hello

	// 客户端直接掐断，不辞而别。
	c.CloseNow()

	// 给服务端一点时间把日志写出来。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if findLog(logs(), "连接结束") != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if line := findLog(logs(), "连接结束"); strings.Contains(line, "认证超时") {
		t.Errorf("客户端断开被当成了认证超时:\n%s", line)
	}
}

func hasLog(lines []string, substr string) bool { return findLog(lines, substr) != "" }

func findLog(lines []string, substr string) string {
	for _, l := range lines {
		if strings.Contains(l, substr) {
			return l
		}
	}
	return ""
}
