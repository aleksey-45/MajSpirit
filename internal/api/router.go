package api

import (
	"context"
	"net/http"

	"github.com/aleksey-45/majspirit/internal/user"
)

// UserService 是 handler 需要的那部分用户系统。
//
// 接口定义在消费方（这里），不是 internal/user —— 和 user.Repo 同一条规矩：
// 接口属于使用它的包。有了它，handler 的测试不必启动真的用户系统。
type UserService interface {
	Register(ctx context.Context, username, password string) (user.User, error)
	Login(ctx context.Context, username, password string) (string, error)
	Logout(ctx context.Context, token string) error
	Authenticate(ctx context.Context, token string) (user.User, error)
}

// handlers 把依赖收在一个地方，各个 handler 只是它的方法。
type handlers struct{ svc UserService }

// NewRouter 返回挂了 /api/* 的 mux；cmd/server 把它挂在 "/api/" 上。
func NewRouter(svc UserService) http.Handler {
	h := &handlers{svc: svc}
	mux := http.NewServeMux()

	// Go 1.22 起支持 "METHOD /path" 模式：方法不对时 mux 自动回 405，
	// 不用在 handler 里自己判 r.Method。
	mux.HandleFunc("POST /api/register", h.register)
	mux.HandleFunc("POST /api/login", h.login)
	mux.Handle("POST /api/logout", h.requireAuth(http.HandlerFunc(h.logout)))
	mux.Handle("GET /api/me", h.requireAuth(http.HandlerFunc(h.me)))

	return mux
}
