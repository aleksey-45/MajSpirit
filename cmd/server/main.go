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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aleksey-45/majspirit/internal/store"
)

func main() {
	// signal.NotifyContext 返回的 ctx 会在收到 Ctrl+C（os.Interrupt）
	// 或 SIGTERM 时被取消。SIGTERM 是 Linux 上 docker stop / k8s 用的信号，
	// 本地测不出来，但部署后每个容器都要靠它优雅退出。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// main 里不写业务逻辑，只做这一件事：调 run，出错就报。
	//
	// 业务必须在 run 里而不是这里——os.Exit **会跳过所有 defer**，
	// 写在 main 里的话连接池永远关不掉。
	//
	// 用 slog + os.Exit(1) 而不是 panic：配置缺失是可预期的失败，
	// panic 会打一整串调用栈，把真正有用的那行信息淹掉。
	if err := run(ctx); err != nil {
		slog.Error("服务端退出", "err", err)
		os.Exit(1)
	}
}

// run 是真正的 main。返回 error 而不是自己 os.Exit(1)，这样它的 defer 一定会执行。
func run(ctx context.Context) error {
	// ① 读配置。两个变量都没有默认值——忘了设必须立刻炸。
	addr := mustEnv("MAJSPIRIT_ADDR")
	dsn := mustEnv("MAJSPIRIT_DATABASE_URL")

	// ② 连数据库
	pool, err := store.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// ③ 跑迁移。失败就不该起服务——理由见文件头。
	if err := pool.Migrate(ctx); err != nil {
		return fmt.Errorf("迁移数据库: %w", err)
	}
	slog.Info("数据库已就绪")

	// ④ 路由。目前只有一个健康检查，§2.4 会在这里挂上真正的接口。
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// 健康检查要连数据库一起探：进程活着但连不上库，
		// 对负载均衡来说和死了没区别，不该报 200。
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "数据库不可用", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})

	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
		// 不设它的话，一个只发半个请求头的连接就能占住一个 goroutine 不放手
		// （Slowloris 攻击）。这是 net/http 上少数几个「默认值不安全」的地方。
		ReadHeaderTimeout: 5 * time.Second,
	}

	// ⑤ 在后台起服务。ListenAndServe 会阻塞，所以放 goroutine；
	// 主流程在下面等信号。
	errc := make(chan error, 1) // 缓冲 1：即使没人来得及接收，那个 goroutine 也不会卡住
	go func() {
		slog.Info("HTTP 服务已启动", "addr", addr)
		err := srv.ListenAndServe()
		// 优雅关闭时 ListenAndServe 会返回 ErrServerClosed，那不是错误。
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
			return
		}
		errc <- nil
	}()

	// ⑥ 等两件事之一：收到退出信号，或者服务自己挂了。
	select {
	case <-ctx.Done():
		slog.Info("收到退出信号，开始优雅关闭")
	case err := <-errc:
		if err != nil {
			return fmt.Errorf("HTTP 服务: %w", err)
		}
		return nil
	}

	// ⑦ 优雅关闭：停止接受新连接，给正在处理的请求留 10 秒。
	//
	// 这里**不能**直接用 ctx——它此刻已经被取消了，基于它派生超时会立刻到期，
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

// mustEnv 读环境变量，没设就直接退出。
//
// 名字里的 must 是 Go 的习惯说法：做不到就别继续。
//
// **刻意不返回 error，也不提供默认值。**
// 返回一个像 "Not set: XXX" 的字符串是错的——那个字符串会一路往下传，
// 最后变成 store.New 里一句莫名其妙的「解析数据库连接串失败」，
// 而你真正要看到的信息（**你忘了设哪个变量**）已经丢掉了。
//
// 配置错了必须在启动那一秒炸，而不是等到第一个请求打进来才以奇怪的方式失败。
//
// 它调用 os.Exit 是安全的：它只在 run 的开头被调用，那时还什么都没打开，
// 没有任何 defer 需要执行。
func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		fmt.Fprintf(os.Stderr,
			"缺少环境变量 %s\n本地开发：cp .env.example .env，然后用 make run（它会源入 .env）\n",
			name)
		os.Exit(1)
	}
	return v
}
