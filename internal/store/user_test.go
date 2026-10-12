package store

import (
	"context"
	"errors"
	"testing"
)

// user_test.go —— users 表的读写。
//
// 这些用例**会写数据**，所以连的是 MAJSPIRIT_TEST_DATABASE_URL（testPool 会拒绝
// 在库名不以 _test 结尾的库上跑），而不是 store_test.go 那个只读的开发库。

// userPool 清空 schema、跑迁移，返回一张空的 users 表。
func userPool(t *testing.T) *Pool {
	t.Helper()

	p := testPool(t)
	clean(t, p)
	if err := p.Migrate(context.Background()); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return p
}

// 三个函数各自的正常路径。它们没有别的地方覆盖，删了这条就没人走过了。
func TestCreateUserThenFind(t *testing.T) {
	p := userPool(t)
	ctx := context.Background()

	rec, err := p.CreateUser(ctx, "alice", "hash-1")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if rec.ID == 0 {
		t.Error("CreateUser 没返回数据库生成的 id —— RETURNING 是不是丢了？")
	}
	if rec.CreatedAt.IsZero() {
		t.Error("CreateUser 没返回 created_at")
	}

	byName, err := p.UserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if byName.ID != rec.ID || byName.PasswordHash != "hash-1" {
		t.Errorf("按用户名查到 %+v，期望 id=%d hash=hash-1", byName, rec.ID)
	}

	byID, err := p.UserByID(ctx, rec.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if byID.Username != "alice" {
		t.Errorf("按 id 查到 %q，期望 alice", byID.Username)
	}
}

// 重名必须被翻译成 ErrUsernameTaken，而不是把 23505 透出去。
//
// 这是"靠唯一索引而不是先查再插"的地基：并发的最后一道防线就在数据库这里。
// 改坏那个错误码判断，这条会挂。
func TestCreateUserRejectsDuplicate(t *testing.T) {
	p := userPool(t)
	ctx := context.Background()

	if _, err := p.CreateUser(ctx, "alice", "hash-1"); err != nil {
		t.Fatalf("第一次 CreateUser: %v", err)
	}

	_, err := p.CreateUser(ctx, "alice", "hash-2")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("重名插入返回 %v，期望 ErrUsernameTaken", err)
	}
}

// 大小写敏感：Alice 与 alice 是两个账号。
func TestUserByUsernameIsCaseSensitive(t *testing.T) {
	p := userPool(t)
	ctx := context.Background()

	if _, err := p.CreateUser(ctx, "Alice", "hash-1"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err := p.UserByUsername(ctx, "alice"); !errors.Is(err, ErrNoUser) {
		t.Errorf("用 alice 查 Alice 的账号返回 %v，期望 ErrNoUser（精确匹配）", err)
	}
	// 反过来也要建得出来，否则唯一索引就不是大小写敏感的
	if _, err := p.CreateUser(ctx, "alice", "hash-2"); err != nil {
		t.Errorf("alice 应该能和 Alice 共存: %v", err)
	}
}

// 两个查询各自的"没有这一行"分支。
func TestUserNotFound(t *testing.T) {
	p := userPool(t)
	ctx := context.Background()

	if _, err := p.UserByUsername(ctx, "nobody"); !errors.Is(err, ErrNoUser) {
		t.Errorf("UserByUsername 返回 %v，期望 ErrNoUser", err)
	}
	if _, err := p.UserByID(ctx, 999999); !errors.Is(err, ErrNoUser) {
		t.Errorf("UserByID 返回 %v，期望 ErrNoUser", err)
	}
}
