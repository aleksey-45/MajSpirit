package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrUsernameTaken = errors.New("用户名已被占用")
	ErrNoUser        = errors.New("用户不存在")
)

// UserRecord 是 users 表的一行。
//
// 它带 PasswordHash，而 internal/user 的 User 不带 —— 两者之间的转换就是
// "哈希不出数据层"那条边界的执行者。
type UserRecord struct {
	ID           int64
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// CreateUser 插入一行，返回数据库生成的那几个字段。
//
// 用户名冲突被翻译成 ErrUsernameTaken —— 调用方不该知道 23505 是什么。
func (p *Pool) CreateUser(ctx context.Context, username, passwordHash string) (UserRecord, error) {
	const q = `
INSERT INTO users (username, password_hash)
VALUES ($1, $2)
RETURNING id, username, created_at
`
	var rec UserRecord
	err := p.pool.QueryRow(ctx, q, username, passwordHash).Scan(
		&rec.ID, &rec.Username, &rec.CreatedAt)
	if err != nil {
		// 唯一约束冲突。匹配错误**码**而不是错误消息——消息是本地化的，
		// 本机 PostgreSQL 报的就是中文。
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return UserRecord{}, ErrUsernameTaken
		}
		return UserRecord{}, fmt.Errorf("插入用户: %w", err)
	}
	return rec, nil
}

// UserByUsername 按用户名精确查一行（大小写敏感）。
//
// 写 username = $1，不要加 lower()：唯一索引建在 username 上，
// lower(username) 是另一个表达式，用不上那个索引。
func (p *Pool) UserByUsername(ctx context.Context, username string) (UserRecord, error) {
	const q = `
SELECT id, username, password_hash, created_at
FROM users
WHERE username = $1
`
	var rec UserRecord
	err := p.pool.QueryRow(ctx, q, username).Scan(
		&rec.ID, &rec.Username, &rec.PasswordHash, &rec.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserRecord{}, ErrNoUser
		}
		return UserRecord{}, fmt.Errorf("按用户名查询用户: %w", err)
	}
	return rec, nil
}

// UserByID 按主键查一行。
//
// 令牌里只有 userID（放在 sub 里），所以 /api/me 要靠它换回用户名。
func (p *Pool) UserByID(ctx context.Context, id int64) (UserRecord, error) {
	const q = `
SELECT id, username, password_hash, created_at
FROM users
WHERE id = $1
`
	var rec UserRecord
	err := p.pool.QueryRow(ctx, q, id).Scan(
		&rec.ID, &rec.Username, &rec.PasswordHash, &rec.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserRecord{}, ErrNoUser
		}
		return UserRecord{}, fmt.Errorf("按 id 查询用户: %w", err)
	}
	return rec, nil
}
