package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// main_test.go —— 启动与关闭。
//
// 这里测的是手测不到的那条路径：在 Windows 上，从 Git Bash 发
// `kill -INT` 给原生 Windows 进程不会走 signal handler，所以优雅退出
// 永远测不出来。但 Linux（也就是部署环境）里 docker stop / k8s 都会发
// SIGTERM，每个容器都要靠它干净退出。

// testConfig 拼一个能启动的配置。
//
// 配置是传进去的，所以这里一行 t.Setenv 都不需要——也就没有
// "忘了设某个变量 ⇒ os.Exit 杀掉整个测试进程、一行输出都没有"这种事。
func testConfig(t *testing.T, addr string) config {
	t.Helper()

	dsn := os.Getenv("MAJSPIRIT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MAJSPIRIT_TEST_DATABASE_URL 未设置，跳过需要数据库的启动/关闭测试")
	}
	return config{
		Addr:        addr,
		DatabaseURL: dsn,
		JWTSecret:   []byte("test-secret-只是测试用"),
		TokenTTL:    time.Hour,
	}
}

// loadConfig 的守卫。
//
// 它守着 §2.2 验收里的一句话："未设置 secret 时服务拒绝启动，而不是回落到
// 内置默认值"。两个容易写错的地方：
//
//   - 一次报出所有缺失项。只报第一个的话，你会修一个、跑一次、再发现一个。
//   - 任何一个缺失都算失败，绝不回落默认值。
func TestLoadConfigReportsEveryMissingVar(t *testing.T) {
	names := []string{
		"MAJSPIRIT_ADDR",
		"MAJSPIRIT_DATABASE_URL",
		"MAJSPIRIT_JWT_SECRET",
	}
	for _, n := range names {
		t.Setenv(n, "")
	}

	cfg, err := loadConfig()
	if err == nil {
		t.Fatalf("三个变量都没设，loadConfig 却成功了：%+v", cfg)
	}
	for _, n := range names {
		if !strings.Contains(err.Error(), n) {
			t.Errorf("错误信息里没提 %s：%v", n, err)
		}
	}
}

func TestRunShutsDownOnContextCancel(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:18080")

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()

	waitUp(t, "http://127.0.0.1:18080/healthz", done)

	// 模拟收到 SIGTERM
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run 返回了错误: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ctx 取消后 run 没有在 10 秒内返回 —— 优雅退出没生效")
	}
}

// 健康检查要连数据库一起探：进程活着但连不上库，不该报 200。
func TestHealthzChecksDatabase(t *testing.T) {
	cfg := testConfig(t, "127.0.0.1:18081")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- run(ctx, cfg) }()
	defer func() {
		cancel()
		<-done
	}()

	waitUp(t, "http://127.0.0.1:18081/healthz", done)
}

// waitUp 轮询直到服务开始响应，或者超时。
//
// 必须同时盯着 done。这是被一次真实的偶发失败教出来的：
// run 如果提前失败返回（端口被占、连不上库、迁移等锁等到 ctx 超时……），
// 错误会躺在 done 里没人读，而这里会空转满 10 秒，最后报一句
// "10 秒内没有起来"——把真正的原因整个盖掉，顺便让这个包慢 10 秒。
//
// 那种报错最坏的地方不是慢，是误导：看到"没有起来"你会去查端口、
// 查监听，而真正的原因可能在完全另一个地方。
func waitUp(t *testing.T, url string, done <-chan error) {
	t.Helper()

	// 每次探测必须有超时。没有它的话，只要有个东西占着端口
	// "接受连接却不回响应"，client.Get 就会一直挂着，下面那个 10 秒
	// deadline 等于不存在——而且因为挂在这一行，连 done 都再也检查不到了。
	// 这一条是被一次真实的挂死教出来的，不是假想的。
	client := &http.Client{Timeout: time.Second}

	deadline := time.Now().Add(10 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastProbeErr error

	for time.Now().Before(deadline) {
		// 非阻塞地看一眼 run 是不是已经退出了
		select {
		case err := <-done:
			t.Fatalf("%s 没能起来：run 提前返回了 %v"+
				"（最后一次探测：%v）", url, err, lastProbeErr)
		default:
		}

		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			t.Fatalf("%s 返回了 %d，期望 200", url, resp.StatusCode)
		}
		lastProbeErr = err

		<-ticker.C
	}
	t.Fatalf("%s 在 10 秒内没有起来（最后一次探测：%v）——"+
		"进程还活着但没在监听。如果同时有别的测试在连同一个数据库，"+
		"先怀疑它卡在迁移的 advisory lock 上。", url, lastProbeErr)
}
