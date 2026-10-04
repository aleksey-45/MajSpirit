package store

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// migrate_test.go —— 迁移执行器。设计见 docs/local/实现指南.md §2.1。
//
// 这些用例会 **DROP SCHEMA**，所以连的是 MAJSPIRIT_TEST_DATABASE_URL，
// 而且 testPool 会拒绝在库名不以 _test 结尾的库上跑。

// testPool 连到测试库。
func testPool(t *testing.T) *Pool {
	t.Helper()

	dsn := os.Getenv("MAJSPIRIT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MAJSPIRIT_TEST_DATABASE_URL 未设置，跳过迁移集成测试")
	}

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("解析测试库连接串: %v", err)
	}
	if name := strings.TrimPrefix(u.Path, "/"); !strings.HasSuffix(name, "_test") {
		t.Fatalf("拒绝在 %q 上跑：库名必须以 _test 结尾（这些用例会 DROP SCHEMA）", name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	p, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("连接测试库: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// clean 把 public schema 清回"什么都没建"的状态。
func clean(t *testing.T, p *Pool) {
	t.Helper()
	if _, err := p.Exec(context.Background(),
		`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("重置 schema: %v", err)
	}
}

// tables 返回 public schema 里的表名集合。
func tables(t *testing.T, p *Pool) map[string]bool {
	t.Helper()

	rows, err := p.Query(context.Background(),
		`SELECT table_name FROM information_schema.tables WHERE table_schema = 'public'`)
	if err != nil {
		t.Fatalf("查表: %v", err)
	}
	defer rows.Close()

	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历表: %v", err)
	}
	return got
}

// migratedVersions 返回 schema_migrations 里的版本号。
func migratedVersions(t *testing.T, p *Pool) []string {
	t.Helper()

	rows, err := p.Query(context.Background(),
		`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatalf("查版本表: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("遍历版本号: %v", err)
	}
	return got
}

// 真实迁移跑完，8 张业务表 + schema_migrations 都在。
func TestMigrateCreatesAllTables(t *testing.T) {
	p := testPool(t)
	clean(t, p)

	if err := p.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	got := tables(t, p)
	want := []string{
		"users",
		"games", "game_seats", "game_rounds",
		"game_initial_hands", "game_walls", "game_wins", "game_events",
		"schema_migrations",
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("缺少表 %s", name)
		}
	}
	if len(got) != len(want) {
		t.Errorf("public schema 里有 %d 张表，期望 %d：%v", len(got), len(want), got)
	}
}

// 跑第二遍必须无害 —— 这就是"幂等"。
func TestMigrateIsIdempotent(t *testing.T) {
	p := testPool(t)
	clean(t, p)
	ctx := context.Background()

	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("第一次 Migrate: %v", err)
	}
	first := migratedVersions(t, p)

	// 第二遍：任何报错都说明幂等没做到
	if err := p.Migrate(ctx); err != nil {
		t.Fatalf("第二次 Migrate 报错 —— 幂等性没做到: %v", err)
	}

	second := migratedVersions(t, p)
	if len(first) != len(second) {
		t.Errorf("第二遍改了版本表：%v → %v", first, second)
	}
}

// ★ 核心：如果「写版本号」失败，前面那些**已经成功的 DDL** 也必须一起回滚。
//
// 怎么造出"DDL 成功、版本号写入失败"：让迁移自己把 schema_migrations 删掉，
// 随后那句 INSERT 就会找不到表而失败。
//
// 这条能区分两种实现：
//
//   - 事务包对了   → DROP 和 CREATE 一起回滚，两样都回到原状
//   - DDL 先提交了 → must_disappear 留下、schema_migrations 没了 → 灾难
//
// ⚠️ 别用"SQL 语法错"来测原子性：那种错误在第一步就失败，版本号那句根本
// 没执行到，所以**分两个事务的实现也能通过**。
func TestMigrateIsAtomic(t *testing.T) {
	p := testPool(t)
	clean(t, p)
	ctx := context.Background()

	fsys := fstest.MapFS{
		"0001_ok.sql": &fstest.MapFile{Data: []byte(
			`CREATE TABLE good_one (id int primary key);`)},
		// DDL 全部合法，但会让随后的「写版本号」失败
		"0002_evil.sql": &fstest.MapFile{Data: []byte(
			`CREATE TABLE must_disappear (id int primary key);
			 DROP TABLE schema_migrations;`)},
	}

	err := p.migrate(ctx, fsys)
	if err == nil {
		t.Fatal("版本号写入失败时，migrate 竟然返回成功了")
	}
	t.Logf("按预期失败: %v", err)

	got := tables(t, p)
	if got["must_disappear"] {
		t.Error("must_disappear 还在 —— DDL 没有跟着版本号的失败一起回滚")
	}
	if !got["schema_migrations"] {
		t.Fatal("schema_migrations 没了 —— DDL 被单独提交了，这是最坏的情况")
	}
	if !got["good_one"] {
		t.Error("good_one 被连累回滚了 —— 0001 早就提交过了，不该受影响")
	}

	versions := migratedVersions(t, p)
	if len(versions) != 1 || versions[0] != "0001_ok.sql" {
		t.Errorf("版本表 = %v，期望只有 0001_ok.sql", versions)
	}
}

// 并发启动多个实例，不能重复执行、不能报错。
//
// 这条验证的是 advisory lock。去掉那把锁它就会挂 ——
// 多半表现为 "tuple concurrently updated" 或重复建表。
func TestMigrateConcurrent(t *testing.T) {
	p := testPool(t)
	clean(t, p)

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = p.Migrate(context.Background())
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("第 %d 个并发 Migrate 失败: %v", i, err)
		}
	}

	versions := migratedVersions(t, p)
	if len(versions) != 1 {
		t.Errorf("并发跑完版本表有 %d 行，期望 1（说明重复执行了）：%v",
			len(versions), versions)
	}
}

// 迁移文件一个都没有时必须报错，不能"成功地什么都不做"。
func TestMigrateRejectsEmptyFS(t *testing.T) {
	p := testPool(t)
	clean(t, p)

	err := p.migrate(context.Background(), fstest.MapFS{
		"readme.txt": &fstest.MapFile{Data: []byte("not a migration")},
	})
	if err == nil {
		t.Fatal("没有任何 .sql 时应该报错，而不是静默通过")
	}
	t.Logf("按预期报错: %v", err)
}
