package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool 包一层 pgxpool，把「怎么连」和「怎么用」分开。
type Pool struct{ *pgxpool.Pool }

// New 从连接串建池。dsn 来自环境变量 MAJSPIRIT_DATABASE_URL。
func New(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析数据库连接串: %w", err)
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("建立连接池: %w", err)
	}
	// 池是**惰性**的：不 Ping 的话，配置错误要等第一次查询才暴露，
	// 而那时你可能已经在某个 HTTP handler 里，看不出是启动配置错了。
	if err := p.Ping(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("连接数据库: %w", err)
	}
	return &Pool{p}, nil
}
