package ws

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/user"
)

// TestHelloIsEnveloped 走一次真的握手，验证第一条消息的形状。
//
// 第 3 步起 hello 的发送时机变了：认证阶段由服务端主动发，
// 顺序仍和协议 §4.1 一致 —— 连上 → hello → auth → authed。
func TestHelloIsEnveloped(t *testing.T) {
	url := newTestServer(t, &fakeAuth{u: user.User{ID: 1, Username: "x"}}, time.Second)
	ctx, logs := baseCtx(t, 5*time.Second)
	c := mustDial(t, ctx, url)

	env, _ := readEnvelope(t, ctx, c)
	if env.Type != EventHello {
		t.Fatalf("type = %q, 期望 %q", env.Type, EventHello)
	}

	// 第二层：data。
	var h hello
	if err := json.Unmarshal(env.Data, &h); err != nil {
		t.Fatalf("解 hello 的 data 失败: %v（data=%s）", err, env.Data)
	}
	if h.ProtocolVersion != protocolVersion {
		t.Errorf("protocolVersion = %d, 期望 %d", h.ProtocolVersion, protocolVersion)
	}
	if h.ServerTime <= 0 {
		t.Errorf("serverTime = %d, 期望正的毫秒时间戳", h.ServerTime)
	}
	if len(h.SessionID) != 32 {
		t.Errorf("sessionId = %q, 期望 32 个 hex 字符", h.SessionID)
	}

	// sessionId 唯一的用途就是让"客户端报的值"和"服务端日志"对得上 ——
	// 所以这里必须两边比，而不是只看那串东西长得像不像。
	// 实测教训：曾经把 newSessionID() 调了两次（log 一次、hello 一次），
	// 两边是两个不同的值；只断言长度和格式的测试照样绿。
	line := findLog(logs(), "连接建立")
	if line == "" {
		t.Fatalf("没有「连接建立」日志")
	}
	if !strings.Contains(line, h.SessionID) {
		t.Errorf("日志里的 session 和 hello 发的不是同一个:\n  日志: %s\n  hello: %s", line, h.SessionID)
	}
}
