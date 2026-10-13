package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aleksey-45/majspirit/internal/auth"
	"github.com/aleksey-45/majspirit/internal/user"
)

// api_test.go —— 用假 UserService：不启动服务、不碰数据库。
//
// 只写 §2.4.d 列的三条。handler 里每个 if 的表格、中间件的分支穷举、
// writeJSON 的编码 —— 都不写：那些去掉之后三个月内不会发现坏掉。

// fakeService 是一个够用的内存用户系统。
type fakeService struct {
	mu      sync.Mutex
	users   map[string]fakeUser // 用户名 -> 密码 + id
	tokens  map[string]string   // 令牌 -> 用户名
	nextID  int64
	authErr error // 非 nil 时 Authenticate 直接返回它，用来测 500 那一条
}

type fakeUser struct {
	password string
	id       int64
}

func newFakeService() *fakeService {
	return &fakeService{users: map[string]fakeUser{}, tokens: map[string]string{}}
}

func (f *fakeService) Register(_ context.Context, username, password string) (user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.users[username]; ok {
		return user.User{}, user.ErrUsernameTaken
	}
	f.nextID++
	f.users[username] = fakeUser{password: password, id: f.nextID}
	return user.User{ID: f.nextID, Username: username}, nil
}

func (f *fakeService) Login(_ context.Context, username, password string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	u, ok := f.users[username]
	if !ok || u.password != password {
		return "", user.ErrBadCredentials
	}
	tok := "tok-" + username
	f.tokens[tok] = username
	return tok, nil
}

func (f *fakeService) Logout(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tokens, token)
	return nil
}

func (f *fakeService) Authenticate(_ context.Context, token string) (user.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.authErr != nil {
		return user.User{}, f.authErr
	}
	username, ok := f.tokens[token]
	if !ok {
		return user.User{}, auth.ErrInvalidToken
	}
	return user.User{ID: f.users[username].id, Username: username}, nil
}

func do(t *testing.T, h http.Handler, method, path, body, authz string) *httptest.ResponseRecorder {
	t.Helper()

	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	if authz != "" {
		r.Header.Set("Authorization", authz)
	}

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, r)
	return rr
}

// 拿 A 的票访问 /api/me，必须返回 A —— 不是"某个登录用户"，也不是 B。
//
// 这是 IDOR（越权）的回归：把中间件里那句 Authenticate 换成"随便取一个用户"，
// 或者把 userFrom 改成从 query/请求体取 id，这条就会挂。
func TestMeReturnsTheTokenOwner(t *testing.T) {
	svc := newFakeService()
	router := NewRouter(svc, newFakeHistory())
	ctx := context.Background()

	for _, name := range []string{"alice", "bob"} {
		if _, err := svc.Register(ctx, name, "abc1234"); err != nil {
			t.Fatalf("Register %s: %v", name, err)
		}
	}
	tokA, err := svc.Login(ctx, "alice", "abc1234")
	if err != nil {
		t.Fatalf("Login alice: %v", err)
	}
	tokB, err := svc.Login(ctx, "bob", "abc1234")
	if err != nil {
		t.Fatalf("Login bob: %v", err)
	}

	for _, tc := range []struct{ tok, want string }{{tokA, "alice"}, {tokB, "bob"}} {
		rr := do(t, router, "GET", "/api/me", "", "Bearer "+tc.tok)
		if rr.Code != http.StatusOK {
			t.Fatalf("状态码 %d，期望 200（body=%s）", rr.Code, rr.Body.String())
		}
		var got userResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("解析响应: %v", err)
		}
		if got.Username != tc.want {
			t.Errorf("票属于 %s，/api/me 却返回 %q", tc.want, got.Username)
		}
	}
}

// 客户端带多余字段必须被拒 —— 否则它就能试着自己指定 id 之类的东西。
func TestRejectsUnknownJSONFields(t *testing.T) {
	rr := do(t, NewRouter(newFakeService(), newFakeHistory()), "POST", "/api/register",
		`{"username":"alice","password":"abc1234","id":1}`, "")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("状态码 %d，期望 400 —— decodeJSON 是不是漏了 DisallowUnknownFields？", rr.Code)
	}
}

// 500 的两半：内部细节只进日志，响应体只给一句通用话。
//
// 这条同时盯住两件事——细节丢了（维护者查不出来）和细节漏了（客户端拿到 SQL 和表名）
// 都是缺陷，所以两半都要断言。
func TestInternalErrorLeaksNothingToClient(t *testing.T) {
	var logged bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })

	// 模拟一条真的会出现的错误：里面有表名
	const secret = `pq: relation "users" does not exist`
	svc := newFakeService()
	svc.authErr = errors.New(secret)

	rr := do(t, NewRouter(svc, newFakeHistory()), "GET", "/api/me", "", "Bearer whatever")

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 %d，期望 500", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "users") || strings.Contains(body, "relation") || strings.Contains(body, "pq:") {
		t.Errorf("响应体泄漏了内部细节: %s", body)
	}
	if !strings.Contains(logged.String(), "users") {
		t.Errorf("细节没有进日志，维护者无从查起: %s", logged.String())
	}
}
