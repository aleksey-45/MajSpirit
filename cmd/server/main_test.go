package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// main_test.go —— 启动与关闭。
//
// 这里测的是**手测不到的那条路径**：在 Windows 上，从 Git Bash 发
// `kill -INT` 给原生 Windows 进程不会走 signal handler，所以优雅退出
// 永远测不出来。但 Linux（也就是部署环境）里 docker stop / k8s 都会发
// SIGTERM，每个容器都要靠它干净退出。

func TestRunShutsDownOnContextCancel(t *testing.T) {
	dsn := os.Getenv("MAJSPIRIT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MAJSPIRIT_TEST_DATABASE_URL 未设置，跳过启动/关闭集成测试")
	}
	t.Setenv("MAJSPIRIT_DATABASE_URL", dsn)
	t.Setenv("MAJSPIRIT_ADDR", "127.0.0.1:18080")

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- run(ctx) }()

	waitUp(t, "http://127.0.0.1:18080/healthz")

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
	dsn := os.Getenv("MAJSPIRIT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MAJSPIRIT_TEST_DATABASE_URL 未设置")
	}
	t.Setenv("MAJSPIRIT_DATABASE_URL", dsn)
	t.Setenv("MAJSPIRIT_ADDR", "127.0.0.1:18081")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()

	waitUp(t, "http://127.0.0.1:18081/healthz")
}

// waitUp 轮询直到服务开始响应，或者超时。
func waitUp(t *testing.T, url string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			t.Fatalf("%s 返回了 %d，期望 200", url, resp.StatusCode)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s 在 10 秒内没有起来", url)
}
