package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// testCtxKey 用来把"这条日志属于哪次测试"标出来。
//
// 服务端日志走 slog.Default()，而 -p 1 下别的包的测试也在并发写日志，
// 不筛就会把别人的记录也算进来。
type testCtxKey struct{}

// captureLogs 把 slog.Default() 的记录抓进内存，并给 ctx 打上本次测试的标记。
//
// 换掉全局 logger 是有副作用的（并发跑的其它测试也会写进来），所以
// 只在自己需要读日志的测试里用，并且用 ctx 值筛出属于自己这条连接的记录。
func captureLogs(t *testing.T, ctx context.Context) (context.Context, func() []string) {
	t.Helper()

	var mu sync.Mutex
	var lines []string

	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{mu: &mu, lines: &lines}, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	return context.WithValue(ctx, testCtxKey{}, t.Name()), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), lines...)
	}
}

type lockedWriter struct {
	mu    *sync.Mutex
	lines *[]string
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.lines = append(*w.lines, strings.TrimSpace(string(p)))
	return len(p), nil
}

// TestHelloIsEnveloped 走一次真的握手，验证线上形状。
//
// 用 httptest 而不是连 localhost:8080：它起的是真 TCP 服务端，但端口随机、
// 生命周期跟着测试走 —— 所以 go test ./... 永远撞不上"端口被占"，
// 也不需要谁先手动把服务端起起来。（早先对着固定端口测过一次，测试文件得
// 依赖"服务端已经在跑"，那是给仓库埋雷。）
func TestHelloIsEnveloped(t *testing.T) {
	srv := httptest.NewServer(Handler())
	defer srv.Close()

	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx, logs := captureLogs(t, ctx)

	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("连不上 %s: %v", url, err)
	}
	defer c.CloseNow()

	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("读第一条消息失败: %v", err)
	}

	// 第一层：信封。
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("第一条消息不是信封: %v（原文 %s）", err, data)
	}
	if env.Type != EventHello {
		t.Errorf("type = %q, 期望 %q", env.Type, EventHello)
	}

	// 第二层：data。
	var h struct {
		ProtocolVersion int    `json:"protocolVersion"`
		ServerTime      int64  `json:"serverTime"`
		SessionID       string `json:"sessionId"`
	}
	if err := json.Unmarshal(env.Data, &h); err != nil {
		t.Fatalf("解 hello 的 data 失败: %v（data=%s）", err, env.Data)
	}
	if h.ProtocolVersion != 1 {
		t.Errorf("protocolVersion = %d, 期望 1", h.ProtocolVersion)
	}
	if h.ServerTime <= 0 {
		t.Errorf("serverTime = %d, 期望正的毫秒时间戳", h.ServerTime)
	}
	if len(h.SessionID) != 32 {
		t.Errorf("sessionId = %q, 期望 32 个 hex 字符", h.SessionID)
	}

	// sessionId 唯一的用途就是让"客户端报的值"和"服务端日志"对得上 ——
	// 所以这里必须两边比，而不是只看那串东西长得像不像。
	// 实测教训：conn.go 里曾把 newSessionID() 调了两次（log 一次、hello 一次），
	// 两边是两个不同的值；只断言长度和格式的测试照样绿。
	var seen []string
	for _, line := range logs() {
		if strings.Contains(line, "连接建立") {
			seen = append(seen, line)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("期望正好一条「连接建立」日志，实际 %d 条: %v", len(seen), logs())
	}
	if !strings.Contains(seen[0], h.SessionID) {
		t.Errorf("日志里的 session 和 hello 发的不是同一个:\n  日志: %s\n  hello: %s", seen[0], h.SessionID)
	}

	t.Logf("会话 %s 的 hello: %s", h.SessionID, data)
}
