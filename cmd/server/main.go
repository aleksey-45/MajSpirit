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
	"github.com/aleksey-45/majspirit/internal/store"
	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/aleksey-45/majspirit/internal/ws"
)

// defaultTokenTTL 是登录令牌的有效期。
const defaultTokenTTL = 24 * time.Hour

// config 是 run 需要的全部配置。
//
// 显式传进来而不是让 run 自己读环境变量：run 要被测试，而"读环境变量 + 里面有
// os.Exit"意味着一忘设变量就把整个测试进程杀掉，一行输出都没有。
type config struct {
	Addr        string
	DatabaseURL string
	JWTSecret   []byte
	TokenTTL    time.Duration
}

// loadConfig 从环境变量读配置，并且一次报出所有缺失项。
//
// 只报第一个的话，你会修一个、跑一次、再发现一个。
//
// 刻意不提供任何默认值：回落到内置默认值意味着"忘了配"和"配对了"在运行时
// 长得一模一样，而它们本该在启动那一秒就被区分开。
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
	// signal.NotifyContext 返回的 ctx 会在收到 Ctrl+C（os.Interrupt）
	// 或 SIGTERM 时被取消。SIGTERM 是 Linux 上 docker stop / k8s 用的信号，
	// 本地测不出来，但部署后每个容器都要靠它优雅退出。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// main 里不写业务逻辑，只做两件事：读配置、调 run。
	//
	// 业务必须在 run 里而不是这里——os.Exit 会跳过所有 defer，写在 main 里的话
	// 连接池永远关不掉。所以 os.Exit 也只出现在这里，不出现在任何可测函数里。
	//
	// 用 slog + os.Exit(1) 而不是 panic：配置缺失是可预期的失败，panic 会打一整串
	// 调用栈，把真正有用的那行信息淹掉。
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
		// 健康检查要连数据库一起探：进程活着但连不上库，
		// 对负载均衡来说和死了没区别，不该报 200。
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "数据库不可用", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})

	// ④ 用户系统与 /api/*。
	//
	// 依赖在这里组装：pool 满足 user.Repo（结构化类型，不需要它知道），
	// revoker 建一次、由 Logout 与 Authenticate 共用。
	svc := user.New(pool, cfg.JWTSecret, cfg.TokenTTL, auth.NewRevoker())
	mux.Handle("/api/", api.NewRouter(svc))
	// WebSocket 入口。刻意不在 /api/ 下（协议 §2.1），也不走 api.NewRouter：
	// ws 与 api 互不认识，由这里把它们分开挂上，各自不需要知道对方存在。
	// svc 同时满足 api.UserService 和 ws.Authenticator（结构化类型，不需要它知道）。
	mux.Handle("GET /ws", ws.NewHandler(svc))

	srv := &http.Server{
		Handler: mux,
		// 不设它的话，一个只发半个请求头的连接就能占住一个 goroutine 不放手
		// （Slowloris 攻击）。这是 net/http 上少数几个「默认值不安全」的地方。
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ⑤ 先同步把端口绑上，成功了再打日志、再在后台服务。
	//
	// 这里刻意不用 srv.ListenAndServe()：它把"绑定"和"开工"揉在一起，
	// 于是"已启动"那句日志只能写在它前面——而那时候还没绑上。
	// 端口被占时，日志先说"HTTP 服务已启动"，紧接着报 bind 失败，
	// 自相矛盾。谁在查"服务到底起没起"，都会被这句日志带进沟里。
	//
	// 拆开之后还有个好处：绑定失败是 run 的一个普通错误返回，
	// 不绕 goroutine 和 errc 通道，调用方和测试都能立刻看到。
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", cfg.Addr, err)
	}

	// ⑥ Serve 会阻塞，所以放 goroutine；主流程在下面等信号。
	errc := make(chan error, 1) // 缓冲 1：即使没人来得及接收，那个 goroutine 也不会卡住
	go func() {
		err := srv.Serve(ln)
		// 优雅关闭时 Serve 会返回 ErrServerClosed，那不是错误。
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()
	slog.Info("HTTP 服务已启动", "addr", ln.Addr().String())

	// ⑦ 等两件事之一：收到退出信号，或者服务自己挂了。
	select {
	case <-ctx.Done():
		slog.Info("收到退出信号，开始优雅关闭")
	case err := <-errc:
		if err != nil {
			return fmt.Errorf("HTTP 服务: %w", err)
		}
		return nil
	}

	// ⑧ 优雅关闭：停止接受新连接，给正在处理的请求留 10 秒。
	//
	// 这里不能直接用 ctx——它此刻已经被取消了，基于它派生超时会立刻到期，
	// Shutdown 会马上返回，等于没等。所以用 WithoutCancel 剥掉取消状态、只保留值，
	// 再自己加一个超时。
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭: %w", err)
	}
	slog.Info("已退出")
	return nil
}
