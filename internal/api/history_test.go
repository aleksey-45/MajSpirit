package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/aleksey-45/majspirit/internal/history"
)

// history_test.go —— /api/history 与 /api/games/{id}：用假 HistoryService，
// 不启动服务、不碰数据库。
//
// 这里只验"HTTP 语义"那一层：谁的身份、参数怎么校验、错误码怎么映射。
// "查出来的数据对不对"在 internal/history 与 internal/store 各自的用例里验。

// fakeHistory 记录 handler 传下来的参数，好断言"取的是票的主人"这类事。
type fakeHistory struct {
	page history.Page
	game history.Game
	err  error // 非 nil 时两个方法都直接返回它（404 / 500 两条路都靠它）

	gotUserID int64
	gotCursor int64
	gotGameID int64
}

func newFakeHistory() *fakeHistory {
	return &fakeHistory{page: history.Page{Games: []history.Summary{}}}
}

func (f *fakeHistory) History(_ context.Context, userID, cursor int64) (history.Page, error) {
	f.gotUserID, f.gotCursor = userID, cursor
	if f.err != nil {
		return history.Page{}, f.err
	}
	return f.page, nil
}

func (f *fakeHistory) Game(_ context.Context, gameID int64) (history.Game, error) {
	f.gotGameID = gameID
	if f.err != nil {
		return history.Game{}, f.err
	}
	return f.game, nil
}

// loginAs 注册并登入一个账号，返回能放进 Authorization 头的值。
func loginAs(t *testing.T, svc *fakeService, name string) string {
	t.Helper()
	ctx := context.Background()

	if _, err := svc.Register(ctx, name, "abc1234"); err != nil {
		t.Fatalf("Register %s: %v", name, err)
	}
	tok, err := svc.Login(ctx, name, "abc1234")
	if err != nil {
		t.Fatalf("Login %s: %v", name, err)
	}
	return "Bearer " + tok
}

// 没票就是 401，和别的 /api 端点一致。
func TestHistoryNeedsAuth(t *testing.T) {
	router := NewRouter(newFakeService(), newFakeHistory())

	for _, path := range []string{"/api/history", "/api/games/1"} {
		rr := do(t, router, "GET", path, "", "")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("%s 无票 = %d，期望 401", path, rr.Code)
		}
	}
}

// 查谁由**票**决定，不由查询参数决定。
//
// 这条是 IDOR 的回归：handler 里把 userFrom(ctx) 换成从 query 取
// （"顺手支持一下查别人的"），这条就会挂 —— 而那种改法在浏览器里看不出来。
func TestHistoryUsesTheTokenOwner(t *testing.T) {
	svc := newFakeService()
	hist := newFakeHistory()
	router := NewRouter(svc, hist)

	// alice 先注册，id 是 1；bob 是 2。查询串里塞 2 不该改变任何事。
	auth := loginAs(t, svc, "alice")
	loginAs(t, svc, "bob")

	rr := do(t, router, "GET", "/api/history?userId=2&id=2", "", auth)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200（body=%s）", rr.Code, rr.Body.String())
	}
	if hist.gotUserID != 1 {
		t.Errorf("handler 拿用户 id %d，期望 1（票属于 alice，不是 query 里那个 2）", hist.gotUserID)
	}
}

// 游标：缺省是 0；正整数原样透下去；不是数字或是负数一律 400。
//
// 删掉它会漏掉：非法游标被当成 0 —— 于是"下一页"永远从第一页开始，
// 前端会看到同一个列表重复出现。
func TestHistoryCursorValidation(t *testing.T) {
	svc := newFakeService()
	hist := newFakeHistory()
	router := NewRouter(svc, hist)
	auth := loginAs(t, svc, "alice")

	for _, tc := range []struct {
		query      string
		wantCode   int
		wantCursor int64
	}{
		{"", http.StatusOK, 0},
		{"?cursor=7", http.StatusOK, 7},
		{"?cursor=abc", http.StatusBadRequest, 0},
		{"?cursor=-1", http.StatusBadRequest, 0},
	} {
		hist.gotCursor = -999
		rr := do(t, router, "GET", "/api/history"+tc.query, "", auth)
		if rr.Code != tc.wantCode {
			t.Errorf("%q = %d，期望 %d（body=%s）", tc.query, rr.Code, tc.wantCode, rr.Body.String())
			continue
		}
		if tc.wantCode == http.StatusBadRequest {
			continue
		}
		if hist.gotCursor != tc.wantCursor {
			t.Errorf("%q 传到 service 的游标是 %d，期望 %d", tc.query, hist.gotCursor, tc.wantCursor)
		}
	}
}

// 列表正文是 history.Page 原样编码：空列表要序列化成 []，不是 null。
//
// 删掉它会漏掉：前端拿到 null，于是"没有历史"那条分支永远走不到（渲染直接抛异常）。
func TestHistoryPageShape(t *testing.T) {
	svc := newFakeService()
	hist := newFakeHistory()
	router := NewRouter(svc, hist)
	auth := loginAs(t, svc, "alice")

	rr := do(t, router, "GET", "/api/history", "", auth)
	if rr.Code != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200", rr.Code)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if got := string(body["games"]); got != "[]" {
		t.Errorf("空列表编码成了 %s，期望 []", got)
	}
	if got := string(body["nextCursor"]); got != "null" {
		t.Errorf("没有下一页时 nextCursor = %s，期望 null", got)
	}
}

// gameId 必须是正整数：不是数字或者 <= 0 一律 400，不能拿去查库。
//
// 删掉它会漏掉：`/api/games/abc` 变成 500（ParseInt 的错误被当成内部错误），
// 而 400 与 500 对调用方是两件事。
func TestGameIDValidation(t *testing.T) {
	svc := newFakeService()
	hist := newFakeHistory()
	hist.game = history.Game{GameID: 7, Seats: []history.Seat{}, Rounds: []history.Round{}}
	router := NewRouter(svc, hist)
	auth := loginAs(t, svc, "alice")

	rr := do(t, router, "GET", "/api/games/7", "", auth)
	if rr.Code != http.StatusOK {
		t.Fatalf("合法 gameId = %d，期望 200（body=%s）", rr.Code, rr.Body.String())
	}
	if hist.gotGameID != 7 {
		t.Errorf("传给 service 的 gameId = %d，期望 7", hist.gotGameID)
	}

	for _, path := range []string{"/api/games/abc", "/api/games/0", "/api/games/-3"} {
		if rr := do(t, router, "GET", path, "", auth); rr.Code != http.StatusBadRequest {
			t.Errorf("%s = %d，期望 400", path, rr.Code)
		}
	}

	// 方法不对由 mux 回 405，不用 handler 自己判（Go 1.22 的 "METHOD /path"）。
	if rr := do(t, router, "POST", "/api/games/7", "{}", auth); rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/games/7 = %d，期望 405", rr.Code)
	}
}

// 不存在的牌局 → 404；并且用例层的 ErrNoGame 要认得出来（不是被兜成 500）。
func TestGameMissingIs404(t *testing.T) {
	svc := newFakeService()
	hist := newFakeHistory()
	hist.err = history.ErrNoGame
	router := NewRouter(svc, hist)
	auth := loginAs(t, svc, "alice")

	if rr := do(t, router, "GET", "/api/games/999", "", auth); rr.Code != http.StatusNotFound {
		t.Errorf("不存在的牌局 = %d，期望 404（body=%s）", rr.Code, rr.Body.String())
	}
}

// 其余错误兜到 500，且细节不进响应体（库的表名、列名不许漏给客户端）。
func TestHistoryInternalErrorIsOpaque(t *testing.T) {
	svc := newFakeService()
	hist := newFakeHistory()
	hist.err = errors.New(`查询牌局: relation "games" does not exist`)
	router := NewRouter(svc, hist)
	auth := loginAs(t, svc, "alice")

	rr := do(t, router, "GET", "/api/games/1", "", auth)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 %d，期望 500", rr.Code)
	}
	if body := rr.Body.String(); strings.Contains(body, "games") || strings.Contains(body, "relation") {
		t.Errorf("响应体里漏出了数据库细节: %s", body)
	}
}
