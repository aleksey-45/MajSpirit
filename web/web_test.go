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

// TestAppHandlerServesScript 守住"客户端脚本真的发得出去"。
//
// 这条路 404 的话牌桌页整块是死的，而浏览器只会报一行不显眼的 module 解析失败。
func TestAppHandlerServesScript(t *testing.T) {
	rec := httptest.NewRecorder()
	AppHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/app/state.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /app/state.js = %d, 期望 200", rec.Code)
	}
	// module 脚本的 MIME 不对，浏览器会拒绝执行（严格 MIME 检查）。
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, 期望含 javascript", ct)
	}
	if !strings.Contains(rec.Body.String(), "export function applyEvent") {
		t.Error("发出去的不是 state.js")
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q，期望 no-store（改了脚本刷新就该生效）", got)
	}
}

// TestTilesHandlerServesSVG 守住牌面能取到：它是牌桌页唯一的图片资源。
func TestTilesHandlerServesSVG(t *testing.T) {
	rec := httptest.NewRecorder()
	TilesHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/tiles/Man1.svg", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /tiles/Man1.svg = %d, 期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/svg+xml") {
		t.Errorf("Content-Type = %q, 期望 image/svg+xml", ct)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Error("发出去的不是 SVG")
	}
}

// TestTilesHandlerRefusesNonSVG 守住"牌面路由只发牌面"。
//
// 那个目录里还有 SOURCE.md 与 LICENSE.md；漏出去不算安全事故，但会让"这条路由
// 负责什么"变成一句空话 —— 后来往目录里丢任何文件都会自动出现在公网路径上。
func TestTilesHandlerRefusesNonSVG(t *testing.T) {
	for _, path := range []string{"/tiles/SOURCE.md", "/tiles/LICENSE.md", "/tiles/"} {
		rec := httptest.NewRecorder()
		TilesHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d，期望 404", path, rec.Code)
		}
	}
}
