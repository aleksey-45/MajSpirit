package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aleksey-45/majspirit/internal/auth"
	"github.com/aleksey-45/majspirit/internal/store"
)

// User 是用例层认识的用户。它没有 PasswordHash —— 哈希不出数据层。
type User struct {
	ID        int64
	Username  string
	CreatedAt time.Time
}

var (
	ErrUsernameTaken  = errors.New("用户名已被占用")
	ErrBadCredentials = errors.New("用户名或密码不正确")
)

// Repo 是 Service 唯一的外部依赖。
type Repo interface {
	CreateUser(ctx context.Context, username, passwordHash string) (store.UserRecord, error)
	UserByUsername(ctx context.Context, username string) (store.UserRecord, error)
	UserByID(ctx context.Context, id int64) (store.UserRecord, error)
}

type Service struct {
	repo     Repo
	secret   []byte
	tokenTTL time.Duration
	revoker  *auth.Revoker
}

func New(repo Repo, secret []byte, tokenTTL time.Duration, revoker *auth.Revoker) *Service {
	return &Service{repo: repo, secret: secret, tokenTTL: tokenTTL, revoker: revoker}
}

// Register 建一个新用户。
//
// 顺序有讲究：先校验、再哈希、最后落库。格式不对的输入不必花 65ms 跑 bcrypt，
// 也不必去数据库走一趟。
func (s *Service) Register(ctx context.Context, username, password string) (User, error) {
	// 原样透传：它已经用 %w 包住了 ErrInvalidUsername / ErrWeakPassword，
	// 而且消息是写给用户的（"首尾不能有空格"）。
	if err := ValidateUsername(username); err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, err
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		// 到这里说明调用方跳过了 ValidatePassword —— 是程序 bug，不是用户输错了，
		// 上层该把它映射成 500，而不是带着 bcrypt 的字样报给用户。
		return User{}, fmt.Errorf("哈希密码: %w", err)
	}

	// 唯一性靠数据库的唯一索引。不要"先查再插"——两次查询之间是竞态。
	rec, err := s.repo.CreateUser(ctx, username, hash)
	if err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			return User{}, ErrUsernameTaken // 翻译成本层哨兵，api 层才不用 import store
		}
		return User{}, fmt.Errorf("创建用户: %w", err)
	}
	return toUser(rec), nil
}

// Login 验证凭证，成功时返回一张新签发的令牌。
//
// 刻意**不校验**用户名/密码的格式：策略一收紧，老用户就当场登不进来了。
// 登录只该有"通过 / 不通过"两种结果。
func (s *Service) Login(ctx context.Context, username, password string) (string, error) {
	rec, err := s.repo.UserByUsername(ctx, username)
	if err != nil {
		if !errors.Is(err, store.ErrNoUser) {
			return "", fmt.Errorf("查询用户: %w", err)
		}
		// 用户不存在也要跑一次 bcrypt：否则这条路径 0ms、密码错误那条 65ms，
		// 光量时间就能枚举用户名。返回的错误与下面那条**是同一个值**。
		auth.VerifyDummy(password)
		return "", ErrBadCredentials
	}

	if !auth.VerifyPassword(rec.PasswordHash, password) {
		return "", ErrBadCredentials
	}

	tok, err := auth.IssueToken(rec.ID, s.secret, s.tokenTTL)
	if err != nil {
		return "", fmt.Errorf("签发令牌: %w", err)
	}
	return tok, nil
}

// Logout 把令牌记进黑名单，让它立刻失效。
//
// 幂等：令牌已经坏掉（伪造 / 过期）时返回 nil —— 用户的目的（这张票不作数）已经
// 达成，报错只会让他重试卡住，而重试也不会变好。
func (s *Service) Logout(ctx context.Context, token string) error {
	// 先验签再收：不验的话，任何人 POST 一个随机字符串就能往黑名单里塞一条垃圾，
	// 反复发就能把内存撑爆。
	if _, err := auth.ParseToken(token, s.secret); err != nil {
		return nil
	}

	// keep 用配置的 TTL 而不是精确的剩余有效期：后者要再解析一次 claims。
	// TTL 是上界——条目留得比必要久一点，但绝不会短，所以不会误放行。
	s.revoker.Revoke(token, s.tokenTTL)
	return nil
}

// Authenticate 把"这张票能不能用"变成"这是谁"。鉴权中间件与 /api/me 都用它。
//
// 有它之后 api 层不必知道 auth.Revoker 的存在：Logout 与这里共用同一个黑名单。
func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	id, err := auth.ParseToken(token, s.secret)
	if err != nil {
		return User{}, err // 已经是 auth.ErrInvalidToken，上层 errors.Is 就能判
	}
	if s.revoker.IsRevoked(token) {
		return User{}, auth.ErrInvalidToken
	}

	// 令牌里只有 id，用户名要回库里取。
	rec, err := s.repo.UserByID(ctx, id)
	if err != nil {
		// 票本身是好的，但它指向的用户没了。对调用方来说这和"过期/被注销"
		// 没有区别——都是"这张票不作数"，所以翻译成 401 而不让它漏出去：
		// 漏出去的话 api 层落进 default 分支，用户会收到一个假的 500。
		if errors.Is(err, store.ErrNoUser) {
			return User{}, auth.ErrInvalidToken
		}
		return User{}, fmt.Errorf("查询用户: %w", err)
	}
	return toUser(rec), nil
}

// toUser 丢掉 PasswordHash，把数据层的记录转成用例层的类型。
func toUser(rec store.UserRecord) User {
	return User{ID: rec.ID, Username: rec.Username, CreatedAt: rec.CreatedAt}
}
