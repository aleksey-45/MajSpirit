package ws

import (
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/coder/websocket"
)

// TestSecondConnectionReplacesFirst 守住"顶号"：同一账号连第二条时，
// 第一条以 4403 关闭（协议 §4.6）。
//
// 只有真连两条才测得到 —— 这条路径横跨注册表、handler 和关闭帧。
func TestSecondConnectionReplacesFirst(t *testing.T) {
	url := newTestServer(t, &fakeAuth{u: user.User{ID: 42, Username: "aleksey"}}, 2*time.Second)
	ctx, _ := baseCtx(t, 10*time.Second)

	first := mustDial(t, ctx, url)
	readEnvelope(t, ctx, first)
	writeAuth(t, ctx, first, "好票")
	if env, _ := readEnvelope(t, ctx, first); env.Type != EventAuthed {
		t.Fatalf("第一条连接的 authed 没来, 实际 %q", env.Type)
	}

	second := mustDial(t, ctx, url)
	readEnvelope(t, ctx, second)
	writeAuth(t, ctx, second, "好票")
	if env, _ := readEnvelope(t, ctx, second); env.Type != EventAuthed {
		t.Fatalf("第二条连接的 authed 没来, 实际 %q", env.Type)
	}

	// 第一条应该被顶掉：读到关闭帧，码是 4403。
	_, _, err := first.Read(ctx)
	if got := websocket.CloseStatus(err); got != statusReplaced {
		t.Errorf("第一条连接的关闭码 = %d, 期望 %d（4403 在别处登录）；err=%v",
			got, statusReplaced, err)
	}
}
