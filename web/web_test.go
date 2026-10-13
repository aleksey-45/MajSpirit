package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandlerServesPage 守住"这一页真的发得出去"。
//
// 路由本身（挂在哪个 pattern 上、会不会盖住 /api/）归 cmd/server 的端到端用例，
// 这里只盯这个 Handler 自己负责的两件事：头部正确、内容就是那个页面。
func TestHandlerServesPage(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, 期望 200", rec.Code)
	}
	// 没有这个头，浏览器有机会把 HTML 当纯文本渲染出来 —— 页面看着像坏了。
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, 期望 text/html", ct)
	}
	body := rec.Body.String()
	// 页面自己拼 WebSocket 地址，服务端这边没有第二处能核对这个路径。
	for _, want := range []string{"new WebSocket(", "'/ws'"} {
		if !strings.Contains(body, want) {
			t.Errorf("调试页里找不到 %q", want)
		}
	}
}
