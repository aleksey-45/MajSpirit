package store

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aleksey-45/majspirit/migrations"
)

// migrateLockID 是 advisory lock 的编号。
//
// 它没有任何内在含义，只是"本项目迁移"这件事在整个数据库里的唯一标识
// （0x4D616A53 = "MajS"）。换个数字就等于换一把锁，所以写死、不随机。
const migrateLockID int64 = 0x4D616A53

// 等锁的上限与轮询间隔。
//
// 两个实例同时启动时"等一会儿"是设计目的；无限期等下去不是。
// 持锁那个实例要是卡住了（比如它的某个 DDL 在等另一把锁），后到的实例会
// 永远挂在启动路径上：健康检查起不来，编排系统看到的是"还在启动中"，
// 而不是"失败了"——挂住是不可恢复的，失败是可重试的。
// lockPollEvery 是轮询间隔。
//
// lockWaitTimeout 用 var 而不是 const：测试要把它压到几百毫秒才跑得动，
// 否则光"拿不到锁会放弃"这一条用例就要跑满 30 秒。这是本文件唯一一个
// 为测试留的口子，而且它只影响"等多久"，不影响任何判定。
var lockWaitTimeout = 30 * time.Second

const lockPollEvery = 200 * time.Millisecond

// acquireMigrateLock 用 try 轮询拿锁。
//
// 为什么不用"阻塞式 pg_advisory_lock + 超时 ctx"：
// 取消一个正在排队的 advisory lock 有竞态——取消请求可能在锁刚好发放
// 之后才到。于是客户端收到错误、以为没拿到，锁却已经在这条会话手上了，
// 而且没有任何人去解它（defer 的解锁是"拿到了"之后才注册的）。
// 那会变成所有人一起卡死。try 版本没有这个窗口：要么明确拿到，要么明确没拿到。
func acquireMigrateLock(ctx context.Context, conn *pgxpool.Conn) error {
	deadline := time.Now().Add(lockWaitTimeout)

	for {
		var got bool
		if err := conn.QueryRow(ctx,
			"SELECT pg_try_advisory_lock($1)", migrateLockID).Scan(&got); err != nil {
			return fmt.Errorf("获取迁移锁: %w", err)
		}
		if got {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("等了 %v 还没拿到迁移锁（另一个实例正在迁移，或者卡住了）。"+
				"查一下谁占着：SELECT pid FROM pg_locks WHERE locktype='advisory' "+
				"AND objid=%d", lockWaitTimeout, migrateLockID)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("等待迁移锁: %w", ctx.Err())
		case <-time.After(lockPollEvery):
		}
	}
}

// Migrate 按文件名顺序，应用所有尚未执行过的迁移。
//
// 三条性质缺一不可：
//
//   - 幂等：已应用过的文件会被跳过，所以重复启动安全。
//   - 原子：每个迁移的 DDL 与它对应的版本号写入在同一个事务里，
//     失败一起回滚——不会留下"表建好了但版本号没记"的半成品。
//   - 并发安全：用 advisory lock 串行化。两个实例同时启动时，
//     后到的那个拿到锁之后会发现迁移已经做完了。
func (p *Pool) Migrate(ctx context.Context) error {
	return p.migrate(ctx, migrations.FS)
}

// migrate 是 Migrate 的实现，多收一个 fsys。
//
// 之所以把它单独拆出来：原子性只能靠"注入一个必定失败的迁移"来验证，
// 而 embed 进来的 FS 是只读且固定的。拆出这一层之后，测试可以用
// fstest.MapFS 造一个坏迁移，而对外签名保持不动。
func (p *Pool) migrate(ctx context.Context, fsys fs.FS) error {
	// ① 独占一条连接。
	//
	// 必须这样，因为 advisory lock 是会话级的：锁归那条连接所有，
	// 也只有那条连接能解开它。
	//
	// 如果改用 p.Exec(...)，池可能把"加锁"和"解锁"分到两条不同连接上——
	// 那样 pg_advisory_unlock 会返回 false 并留下一条警告，锁永远解不开。
	// 之后任何一次迁移（本进程或别的实例）都会永久阻塞。
	//
	// 这比"没有锁"更糟：没有锁只是不互斥，漏掉的锁是静默死锁。
	// 类型上也做了约束：下面所有操作都只走 conn，不碰 p。
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("获取连接: %w", err)
	}
	defer conn.Release() // 注册在先 → 最后执行，保证解锁发生在归还之前

	// ② 排队等锁。拿不到就在这里等，直到另一个实例做完——但有上限。
	if err := acquireMigrateLock(ctx, conn); err != nil {
		return err
	}
	defer func() {
		// 解锁必须用 WithoutCancel：ctx 已经取消时，仍然要把锁放掉。
		// 否则锁会留在这条池化连接上，后面任何一次迁移都会永久阻塞。
		_, _ = conn.Exec(context.WithoutCancel(ctx),
			"SELECT pg_advisory_unlock($1)", migrateLockID)
	}()

	// ③ 版本表。IF NOT EXISTS 让它天然幂等。
	//
	// 这张表是执行器的元数据，不属于任何一次迁移，所以不写在 .sql 里——
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
// 关键就是下面三行的顺序和事务归属：
//
//	tx.Exec(迁移 SQL)       ← 建表
//	tx.Exec(写版本号)       ← 记账
//	tx.Commit()             ← 两者一起生效
//
// 少任何一步、或者分到两个事务里，都会留下"表建好了但版本号没记"的状态：
// 下次启动重跑同一个文件，直接撞上 CREATE TABLE 报错，而且无法自动恢复。
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
