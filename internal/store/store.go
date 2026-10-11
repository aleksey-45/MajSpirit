package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// connectTimeout 是"建立连接"这一步的上限。
//
// 不设的话，黑洞地址（防火墙丢包而不是拒绝连接）会挂到操作系统的 TCP 超时，本机
// 实测约 2 分钟。它在启动路径上，所以表现是"启动卡住"而不是"启动失败"——日志里
// 什么都没有，编排系统看到的是"还在启动中"，而挂住不可恢复、失败可以重试。
//
// 写在 ConnConfig 而不是拼进 DSN，好让 DSN 里的 connect_timeout= 仍能覆盖它。
const connectTimeout = 2 * time.Second

// Pool 包一层 pgxpool，把「怎么连」和「怎么用」分开。
//
// 底层的池子是命名字段、不导出的：内嵌 *pgxpool.Pool 会把 Query / Exec / Begin /
// Acquire 全部透出去，别的包就能绕过本包直接发 SQL——那正是分层要防的事。
// 对外只暴露下面这几个方法，加方法是一个要过脑子的动作。
type Pool struct{ pool *pgxpool.Pool }

// Close 关闭池子并释放所有连接。启动中途失败时也要调它，否则连接会漏。
func (p *Pool) Close() { p.pool.Close() }

// Ping 探一次数据库。健康检查用它。
func (p *Pool) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// New 从连接串建池。dsn 来自环境变量 MAJSPIRIT_DATABASE_URL。
func New(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析数据库连接串: %w", err)
	}
	if cfg.ConnConfig.ConnectTimeout == 0 {
		cfg.ConnConfig.ConnectTimeout = connectTimeout
	}

	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("建立连接池: %w", err)
	}
	// 池是惰性的：不 Ping 的话，配置错误要等第一次查询才暴露，而那时你可能已经
	// 在某个 HTTP handler 里，看不出是启动配置错了。
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("连接数据库: %w", err)
	}
	return &Pool{pool: p}, nil
}
