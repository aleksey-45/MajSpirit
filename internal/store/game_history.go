package store

// game_history.go —— 过往牌局的**列表**（阶段五）。单局存档在 game_archive.go。
//
// 与 user.go 的分工一样：这里只管 SQL 与从数据库读出来的字段，不管 HTTP、
// 不管毫秒与 hex（那是 internal/history 的事）。表结构见 migrations/0001_init.sql 与 0003。
//
// 写路径在 game_record.go（BeginGame / FinishGame），用例层在 internal/history/write.go。
// 注意那张表**还没有接到对局结束的回调上** —— 打完一场暂时不会落库。

import (
	"context"
	"fmt"
	"math"
	"time"
)

// GameSummary 是"某位玩家在某一场的座位与结果"，/api/history 的一行。
//
// 进行中的对局上 FinishedAt / Placement 是 nil：那一行已经写进库了
// （开局时写 games + game_seats），但牌局还没结束。
type GameSummary struct {
	GameID       int64
	StartedAt    time.Time
	FinishedAt   *time.Time
	Seat         int
	Placement    *int
	InitialScore int
	FinalScore   int
	Points       int
}

// GameHistory 查某位玩家参与过的牌局，按 game_id 倒序。
//
// cursor 为 0 表示第一页；非 0 时只取 id 比它小的。
//
// 游标用 id 而不是 started_at：同一秒开两桌、或者时钟回拨，都会让时间戳游标
// 跳行或重复；id 是 BIGSERIAL，单调，一个整数就够。代价是换分库或回填 id 之后
// 不再等价于时间序 —— 本项目没有这个需求。
//
// 也不用 OFFSET：翻到第 N 页要重扫前 N-1 页，而且翻页期间新插入的牌局会让某些行
// 被跳过。keyset 两个问题都没有。
func (p *Pool) GameHistory(ctx context.Context, userID, cursor int64, limit int) ([]GameSummary, error) {
	// "没有游标"在**这里**换成 int64 的最大值，不写成 SQL 里的
	// `($2 = 0 OR g.id < $2)`。两种写法结果一样，执行计划差 6 倍：OR 那条推不进索引，
	// 深翻页时 games 那一侧要从最新往回走 —— 而躲开这件事正是用 keyset 的原因。
	// 实测数据见本文件末尾。
	if cursor == 0 {
		cursor = math.MaxInt64
	}

	// 小整数列一律在 SQL 里 ::int。不是为了绕开什么，是让读回来的类型只有
	// int / int64 两种，调用点与测试不用各自记住哪一列是 int2。
	const q = `
SELECT g.id,
       g.started_at,
       g.finished_at,
       gs.seat::int,
       gs.placement::int,
       gs.initial_score,
       gs.final_score,
       gs.points
FROM game_seats gs
JOIN games g ON g.id = gs.game_id
WHERE gs.user_id = $1
  AND g.id < $2
ORDER BY g.id DESC
LIMIT $3
`
	rows, err := p.pool.Query(ctx, q, userID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("查询过往牌局: %w", err)
	}
	defer rows.Close()

	var out []GameSummary
	for rows.Next() {
		var s GameSummary
		if err := rows.Scan(&s.GameID, &s.StartedAt, &s.FinishedAt, &s.Seat,
			&s.Placement, &s.InitialScore, &s.FinalScore, &s.Points); err != nil {
			return nil, fmt.Errorf("读取过往牌局: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历过往牌局: %w", err)
	}
	return out, nil
}

// ── 游标谓词为什么长这样：实测 ──────────────────────────────────────────
//
// 2026-10-13，测试库造 100 个用户 / 25000 场 / 10 万个 game_seats，
// 取一个座位最多的用户（1000 场）跑 EXPLAIN (ANALYZE)。复现脚本是
// bin/explain-cursor.sql。**同一台机器、同一次会话里对比**：
//
//	写法                        首页      中段翻页
//	($2 = 0 OR g.id < $2)       0.23ms    1.13ms    ← Filter 掉的 5001 行
//	g.id < COALESCE(...)        0.17ms    0.18ms
//	g.id < $2（Go 里把 0 换掉）  ≈ 上面那一行
//
// 差别全在计划的第二支：OR 那条进不了 Index Cond，只能在
// `Index Scan Backward using games_pkey` 上做 Filter，于是要**从最新的 id 往回
// 走到游标处**才凑得出 21 行；越往后翻走的越多，keyset 就退化成 OFFSET 了。
// COALESCE 那条是 `Index Cond`，直接从游标处起扫。
//
// 注意这一档要用 `SET plan_cache_mode = force_generic_plan` 才看得见：pgx 复用
// 语句、PG 前 5 次走 custom plan（$2 被折成常量，OR 消失），第 6 次之后才可能切到
// generic plan —— 也就是"上线才炸"那一类。
//
// 所以选了第三种写法：常量在 Go 里（就是 math.MaxInt64），SQL 只剩一条范围条件，
// 两种 plan_cache_mode 下都一样。
