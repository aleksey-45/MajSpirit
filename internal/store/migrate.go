package store

import (
	"context"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aleksey-45/majspirit/migrations"
)

// migrateLockID 是 advisory lock 的编号。
//
// 它没有任何内在含义，只是"本项目迁移"这件事在整个数据库里的唯一标识
// （0x4D616A53 = "MajS"）。换个数字就等于换一把锁，所以写死、不随机。
const migrateLockID int64 = 0x4D616A53

// Migrate 按文件名顺序，应用所有尚未执行过的迁移。
//
// 三条性质缺一不可：
//
//   - **幂等**：已应用过的文件会被跳过，所以重复启动安全。
//   - **原子**：每个迁移的 DDL 与它对应的版本号写入在同一个事务里，
//     失败一起回滚——不会留下"表建好了但版本号没记"的半成品。
//   - **并发安全**：用 advisory lock 串行化。两个实例同时启动时，
//     后到的那个拿到锁之后会发现迁移已经做完了。
func (p *Pool) Migrate(ctx context.Context) error {
	return p.migrate(ctx, migrations.FS)
}

// migrate 是 Migrate 的实现，多收一个 fsys。
//
// 之所以把它单独拆出来：**原子性只能靠"注入一个必定失败的迁移"来验证**，
// 而 embed 进来的 FS 是只读且固定的。拆出这一层之后，测试可以用
// fstest.MapFS 造一个坏迁移，而对外签名保持不动。
func (p *Pool) migrate(ctx context.Context, fsys fs.FS) error {
	// ① 独占一条连接。
	//
	// 必须这样，因为 advisory lock 是**会话级**的：锁归那条连接所有，
	// 也只有那条连接能解开它。
	//
	// 如果改用 p.Exec(...)，池可能把"加锁"和"解锁"分到两条不同连接上——
	// 那样 pg_advisory_unlock 会返回 false 并留下一条警告，**锁永远解不开**。
	// 之后任何一次迁移（本进程或别的实例）都会永久阻塞。
	//
	// 这比"没有锁"更糟：没有锁只是不互斥，漏掉的锁是**静默死锁**。
	// 类型上也做了约束：下面所有操作都只走 conn，不碰 p。
	conn, err := p.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("获取连接: %w", err)
	}
	defer conn.Release() // 注册在先 → 最后执行，保证解锁发生在归还之前

	// ② 排队。拿不到锁就在这里阻塞，直到另一个实例做完。
	// 会话结束时锁会自动释放，所以进程被 kill 也不会留下死锁。
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrateLockID); err != nil {
		return fmt.Errorf("获取迁移锁: %w", err)
	}
	defer func() {
		// 解锁必须用 WithoutCancel：ctx 已经取消时，仍然要把锁放掉。
		// 否则锁会留在这条池化连接上，后面任何一次迁移都会永久阻塞。
		_, _ = conn.Exec(context.WithoutCancel(ctx),
			"SELECT pg_advisory_unlock($1)", migrateLockID)
	}()

	// ③ 版本表。IF NOT EXISTS 让它天然幂等。
	//
	// 这张表是**执行器的元数据**，不属于任何一次迁移，所以不写在 .sql 里——
	// 它必须在读任何迁移之前就存在。
	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("建版本表: %w", err)
	}

	// ④ 已经应用过哪些
	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return err
	}

	// ⑤ 待应用的迁移文件（已排序）
	names, err := migrationNames(fsys)
	if err != nil {
		return err
	}

	// ⑥ 逐个应用
	for _, name := range names {
		if applied[name] {
			continue
		}
		if err := applyOne(ctx, conn, fsys, name); err != nil {
			return err
		}
	}
	return nil
}

// appliedVersions 读回 schema_migrations 里已应用的版本号。
func appliedVersions(ctx context.Context, conn *pgxpool.Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("查询已应用的迁移: %w", err)
	}
	// 必须 Close，否则这条连接不会归还到池里。
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("读取版本号: %w", err)
		}
		applied[version] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历版本号: %w", err)
	}
	return applied, nil
}

// migrationNames 返回 fsys 里所有 .sql 的文件名。
//
// fs.ReadDir 保证按文件名排序，而迁移文件名是零填充的（0001_、0002_…），
// 所以字典序就是数字序——执行顺序不需要额外处理。
func migrationNames(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("列出迁移文件: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}

	// 一个都没有就报错，绝不"成功地什么都不做"。
	// 这种情况多半说明 embed 配错了，而静默通过会让人以为迁移没问题。
	if len(names) == 0 {
		return nil, fmt.Errorf("没有任何 .sql 迁移文件")
	}
	return names, nil
}

// applyOne 应用一个迁移文件。
//
// 关键就是下面三行的**顺序和事务归属**：
//
//	tx.Exec(迁移 SQL)       ← 建表
//	tx.Exec(写版本号)       ← 记账
//	tx.Commit()             ← 两者一起生效
//
// 少任何一步、或者分到两个事务里，都会留下"表建好了但版本号没记"的状态：
// 下次启动重跑同一个文件，直接撞上 CREATE TABLE 报错，而且**无法自动恢复**。
func applyOne(ctx context.Context, conn *pgxpool.Conn, fsys fs.FS, name string) error {
	sqlBytes, err := fs.ReadFile(fsys, name)
	if err != nil {
		return fmt.Errorf("读取 %s: %w", name, err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启事务 %s: %w", name, err)
	}
	// 保险丝：任何提前 return 都会回滚。
	// Commit 之后再调 Rollback 是无害的（返回 ErrTxClosed）。
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("应用 %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
		return fmt.Errorf("记录 %s 的版本号: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交 %s: %w", name, err)
	}
	return nil
}
