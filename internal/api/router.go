package api

import (
	"context"
	"net/http"

	"github.com/aleksey-45/majspirit/internal/history"
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

// HistoryService 是历史查询那部分（任务书 §6）。
//
// 同样定义在消费方。它返回 internal/history 的类型 —— 和 UserService 返回
// user.User 一个道理：接口在这里，**类型**在用例层，两边共用一个具名类型，
// 结构化类型才连得上，而 api 仍然不 import store。
type HistoryService interface {
	History(ctx context.Context, userID, cursor int64) (history.Page, error)
	Game(ctx context.Context, gameID int64) (history.Game, error)
}

// handlers 把依赖收在一个地方，各个 handler 只是它的方法。
type handlers struct {
	svc  UserService
	hist HistoryService
}

// NewRouter 返回挂了 /api/* 的 mux；cmd/server 把它挂在 "/api/" 上。
func NewRouter(svc UserService, hist HistoryService) http.Handler {
	h := &handlers{svc: svc, hist: hist}
	mux := http.NewServeMux()

	// Go 1.22 起支持 "METHOD /path" 模式：方法不对时 mux 自动回 405，
	// 不用在 handler 里自己判 r.Method。
	mux.HandleFunc("POST /api/register", h.register)
	mux.HandleFunc("POST /api/login", h.login)
	mux.Handle("POST /api/logout", h.requireAuth(http.HandlerFunc(h.logout)))
	mux.Handle("GET /api/me", h.requireAuth(http.HandlerFunc(h.me)))

	// 历史牌局（任务书 §6）。两条都要鉴权，但**不按参与者过滤**：
	// 已结束的对局本来就该公开 —— 手牌这时已经揭示，藏它反而让人怀疑。
	// 代价：以后要做"仅参与者可见 / 分享链接"时必须改这一处，注释里写清了。
	// 进行中的对局不受影响：它只有头两行，明细行还没写（见 internal/history）。
	mux.Handle("GET /api/history", h.requireAuth(http.HandlerFunc(h.history)))
	mux.Handle("GET /api/games/{gameId}", h.requireAuth(http.HandlerFunc(h.game)))

	return mux
}
