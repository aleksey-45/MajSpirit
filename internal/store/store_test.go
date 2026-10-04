package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// store_test.go —— 连接池。
//
// 需要真实数据库的用例靠 MAJSPIRIT_DATABASE_URL 开关：
//
//	set -a; . ./.env; set +a; go test ./internal/store/ -v
//
// 没设就跳过——这样在没装数据库的机器上 `go test ./...` 仍然全绿。
// 这是 §2 起与 §1 最大的不同：**纯度没了，测试要开始区分"需不需要外部世界"**。

func integrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("MAJSPIRIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("MAJSPIRIT_DATABASE_URL 未设置，跳过集成测试")
	}
	return dsn
}

// 这条是 §2.1 验收的核心：连接串能用，池能建起来，Ping 通得过。
func TestNewConnects(t *testing.T) {
	dsn := integrationDSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	defer p.Close()

	// 再查一次，确认不只是 Ping 通了
	var user, db string
	if err := p.QueryRow(ctx, "SELECT current_user, current_database()").Scan(&user, &db); err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if user == "" || db == "" {
		t.Fatalf("current_user/current_database 为空: %q / %q", user, db)
	}
	t.Logf("连接成功: %s @ %s", user, db)
}

// 迁移还没跑过时，库里应该一张表都没有。
// 这条同时验证了：角色有建表权限（是库的 owner），所以迁移能跑。
func TestNewRoleCanCreateTables(t *testing.T) {
	dsn := integrationDSN(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	defer p.Close()

	// 在一个回滚的事务里建表：验证权限，但不留下任何东西。
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin 失败: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `CREATE TABLE _perm_probe (id int primary key)`); err != nil {
		t.Fatalf("没有建表权限: %v", err)
	}
	t.Log("角色有建表权限（能跑迁移）")
}

// 连接串写错必须**报错**，而不是返回一个能用的池。
func TestNewRejectsBadDSN(t *testing.T) {
	ctx := context.Background()

	for _, dsn := range []string{
		"",
		"://不是连接串",
		"postgres://u:p@[::1]:99999/x",
	} {
		p, err := New(ctx, dsn)
		if err == nil {
			p.Close()
			t.Errorf("New(%q) 没有报错，但它不该成功", dsn)
			continue
		}
		// 错误信息要能看出是"连数据库"，不能是裸的解析错误
		if !strings.Contains(err.Error(), "数据库") && !strings.Contains(err.Error(), "连接串") {
			t.Errorf("New(%q) 的错误信息不够清楚: %v", dsn, err)
		}
	}
}

// ⚠️ 记录一个**已知缺陷**：New 没有设连接超时。
//
// 调用方传 context.Background() 时，遇到黑洞地址（防火墙丢包而不是拒绝连接），
// Ping 会挂到 OS 的 TCP 超时（约 2 分钟）才返回，而不是快速失败。
// 这条测试故意用短 ctx，所以现在能过——但**它掩盖了那个缺陷**，
// 一旦去掉 ctx 超时就会挂两分钟。
//
// 修法：DSN 里加 `connect_timeout=5`，或在 New 内部派生一个带超时的 ctx。
func TestNewHonorsContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 192.0.2.0/24 是 TEST-NET-1，保证不可路由 —— 典型的"黑洞"
	start := time.Now()
	p, err := New(ctx, "postgres://u:p@192.0.2.1:5432/x?sslmode=disable")
	elapsed := time.Since(start)

	if err == nil {
		p.Close()
		t.Fatal("连到不可路由地址竟然成功了？")
	}
	if elapsed > 5*time.Second {
		t.Errorf("用了 %v 才失败，ctx 超时没被尊重", elapsed)
	}
	t.Logf("按预期失败，耗时 %v", elapsed)
}
