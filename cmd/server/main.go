// Command server 启动 MajSpirit（雀灵麻将）服务端。
//
// 启动顺序（顺序本身是有意的）：
//
//	读配置 → 连数据库 → 跑迁移 → 起 HTTP → 等信号 → 优雅关闭
//
// 「跑迁移」必须在「起 HTTP」之前：结构没建好就开始收请求，
// 那些请求会以各种莫名其妙的方式失败，而且你很难定位到是 schema 的问题。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aleksey-45/majspirit/internal/api"
	"github.com/aleksey-45/majspirit/internal/auth"
	"github.com/aleksey-45/majspirit/internal/history"
	"github.com/aleksey-45/majspirit/internal/room"
	"github.com/aleksey-45/majspirit/internal/store"
	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/aleksey-45/majspirit/internal/ws"
	"github.com/aleksey-45/majspirit/web"
)

// defaultTokenTTL 是登录令牌的有效期。
const defaultTokenTTL = 24 * time.Hour

// config 是 run 需要的全部配置。
//
// 显式传进来而不是让 run 自己读环境变量：run 要被测试，而"读环境变量 + 里面有
// os.Exit"意味着忘设一个变量就把测试进程杀掉。
type config struct {
	Addr        string
	DatabaseURL string
	JWTSecret   []byte
	TokenTTL    time.Duration
}

// loadConfig 从环境变量读配置，一次报出所有缺失项。
//
// 不提供默认值：回落到默认值会让"忘了配"和"配对了"在运行时长得一样，
// 而它们本该在启动那一秒被区分开。
func loadConfig() (config, error) {
	var missing []string
	get := func(name string) string {
		v := os.Getenv(name)
		if v == "" {
			missing = append(missing, name)
		}
		return v
	}

	cfg := config{
		Addr:        get("MAJSPIRIT_ADDR"),
		DatabaseURL: get("MAJSPIRIT_DATABASE_URL"),
		JWTSecret:   []byte(get("MAJSPIRIT_JWT_SECRET")),
		TokenTTL:    defaultTokenTTL,
	}
	if len(missing) > 0 {
		return config{}, fmt.Errorf(
			"缺少环境变量 %s（本地开发：cp .env.example .env，然后 make run）",
			strings.Join(missing, "、"))
	}
	return cfg, nil
}

func main() {
	// 收到 Ctrl+C（os.Interrupt）或 SIGTERM 时取消。
	// SIGTERM 本地测不出来，但容器停服靠它。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// main 里不写业务：os.Exit 会跳过所有 defer，写在 main 里连接池就关不掉。
	// 所以 os.Exit 只出现在这里，不出现在任何可测函数里。
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("配置错误", "err", err)
		os.Exit(1)
	}

	if err := run(ctx, cfg); err != nil {
		slog.Error("服务端退出", "err", err)
		os.Exit(1)
	}
}

// run 是真正的 main。返回 error 而不是自己 os.Exit(1)，这样它的 defer 一定会执行。
func run(ctx context.Context, cfg config) error {
	// ① 连数据库
	pool, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	// ② 跑迁移。失败就不该起服务——理由见文件头。
	if err := pool.Migrate(ctx); err != nil {
		return fmt.Errorf("迁移数据库: %w", err)
	}
	slog.Info("数据库已就绪")

	// ③ 路由。
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		// 连数据库一起探：进程活着但连不上库，对负载均衡和死了没区别。
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "数据库不可用", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})

	// §3.3 的调试页。`{$}` 把它钉死在根路径上：用 `GET /` 会和 `/api/`
	// 冲突到 panic —— 后者方法更宽、路径更具体，谁都不比谁更具体（实测过）。
	mux.Handle("GET /{$}", web.Handler())

	// ④ 用户系统与 /api/*。依赖在这里组装：pool 满足 user.Repo（结构化类型），
	// revoker 建一次、由 Logout 与 Authenticate 共用。hist 同理满足 history.Repo。
	svc := user.New(pool, cfg.JWTSecret, cfg.TokenTTL, auth.NewRevoker())
	hist := history.New(pool)
	mux.Handle("/api/", api.NewRouter(svc, hist))
	// WebSocket 入口不在 /api/ 下（协议 §2.1），也不走 api.NewRouter：
	// ws 与 api 互不认识，由这里分开挂上。svc 同时满足 ws.Authenticator。
	//
	// 连接注册表与房间注册表都只建一次、跨所有连接共用：指令走
	// 连接 → Hub → 房间/对局，事件走 房间/对局 → Registry → 连接。
	registry := ws.NewRegistry()
	hub := room.NewHub(registry.Send, nil)
	// 进程退出时房间要一起关掉（房间会带走它手上那一场对局）。
	defer hub.Close()

	mux.Handle("GET /ws", ws.NewHandler(svc, registry, hub))

	srv := &http.Server{
		Handler: mux,
		// 不设它，一个只发半个请求头的连接就能占住一个 goroutine（Slowloris）。
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ⑤ 先同步绑端口，成功了再打日志、再在后台服务。
	//
	// 不用 srv.ListenAndServe()：它把绑定和开工揉在一起，于是"已启动"只能写在
	// 绑定之前 —— 端口被占时会先打"已启动"再报 bind 失败，自相矛盾。
	// 拆开后绑定失败是 run 的普通错误返回，调用方和测试都能立刻看到。
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", cfg.Addr, err)
	}

	// ⑥ Serve 阻塞，所以放 goroutine；主流程在下面等信号。
	errc := make(chan error, 1) // 缓冲 1：没人来得及接收时那个 goroutine 也不会卡住
	go func() {
		err := srv.Serve(ln)
		// 优雅关闭时 Serve 返回 ErrServerClosed，那不是错误。
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()
	slog.Info("HTTP 服务已启动", "addr", ln.Addr().String())

	// ⑦ 等退出信号，或服务自己挂了。
	select {
	case <-ctx.Done():
		slog.Info("收到退出信号，开始优雅关闭")
	case err := <-errc:
		if err != nil {
			return fmt.Errorf("HTTP 服务: %w", err)
		}
		return nil
	}

	// ⑧ 停止接受新连接，给正在处理的请求留 10 秒。
	//
	// 不能直接用 ctx：它此刻已被取消，基于它派生超时会立刻到期，等于没等。
	// WithoutCancel 剥掉取消状态、保留值，再自己加超时。
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭: %w", err)
	}
	slog.Info("已退出")
	return nil
}
