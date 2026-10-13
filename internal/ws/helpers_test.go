package ws

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// 测试用的公共搭台：httptest + 打日志的 ctx + 假认证器。

// testCtxKey 用来把"这条日志属于哪次测试"标出来。
//
// 服务端日志走 slog.Default()，而 -p 1 下别的包的测试也在并发写日志，
// 不筛就会把别人的记录也算进来。
type testCtxKey struct{}

// baseCtx 起一个带超时的 ctx，并把 slog 的输出抓进内存。
//
// 换掉全局 logger 是有副作用的（并发跑的其它测试也会写进来），所以只在自己
// 需要读日志的测试里用，并且用 ctx 值筛出属于自己那条连接的记录。
func baseCtx(t *testing.T, d time.Duration) (context.Context, func() []string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)

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
