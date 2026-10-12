package user

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/auth"
	"github.com/aleksey-45/majspirit/internal/store"
)

// service_test.go —— 用例层。用假 Repo，完全不碰数据库。
//
// 这正是 Repo 接口存在的理由：下面"并发注册"和"两条失败路径耗时接近"这两条，
// 连真库跑会又慢又脆。

var testSecret = []byte("test-secret-至少32字节-只是测试用")

// fakeRepo 是一张够用的内存 users 表。
//
// 它的唯一性检查必须在锁里，也就是和数据库的原子插入同语义 —— 否则
// TestRegisterConcurrentSameUsername 就退化成"测假货"了，真库上的竞态照样漏。
type fakeRepo struct {
	mu     sync.Mutex
	nextID int64
	byName map[string]store.UserRecord
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byName: make(map[string]store.UserRecord)}
}

func (r *fakeRepo) CreateUser(_ context.Context, username, passwordHash string) (store.UserRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.byName[username]; ok {
		return store.UserRecord{}, store.ErrUsernameTaken
	}
	r.nextID++
	rec := store.UserRecord{
		ID:           r.nextID,
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now(),
	}
	r.byName[username] = rec
	return rec, nil
}

func (r *fakeRepo) UserByUsername(_ context.Context, username string) (store.UserRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec, ok := r.byName[username]
	if !ok {
		return store.UserRecord{}, store.ErrNoUser
	}
	return rec, nil
}

func (r *fakeRepo) UserByID(_ context.Context, id int64) (store.UserRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, rec := range r.byName {
		if rec.ID == id {
			return rec, nil
		}
	}
	return store.UserRecord{}, store.ErrNoUser
}

// delete 把用户从假仓储里拿掉，用来模拟"票还在、账号没了"。
func (r *fakeRepo) delete(username string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byName, username)
}

func newService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	return New(repo, testSecret, time.Hour, auth.NewRevoker()), repo
}

// ── Register ─────────────────────────────────────────────────────────

func TestRegisterAccepts(t *testing.T) {
	s, repo := newService()
	ctx := context.Background()

	got, err := s.Register(ctx, "alice", "abc1234")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got.ID == 0 || got.Username != "alice" {
		t.Errorf("Register 返回 %+v", got)
	}

	// 落库的必须是哈希：把 CreateUser 的参数从 hash 写成 password 的话，
	// 这条会挂。User 里没有 PasswordHash 是编译期保证的，这里验的是存储侧。
	rec, err := repo.UserByUsername(ctx, "alice")
	if err != nil {
		t.Fatalf("UserByUsername: %v", err)
	}
	if rec.PasswordHash == "abc1234" {
		t.Fatal("明文密码进了库")
	}
	if !auth.VerifyPassword(rec.PasswordHash, "abc1234") {
		t.Error("存下来的哈希验不过原密码")
	}
}

// 校验错误要原样透传：api 层靠 errors.Is 判 ErrInvalidUsername / ErrWeakPassword，
// 而且消息是写给用户的（"首尾不能有空格"），不能被换成一句通用的"凭证不正确"。
func TestRegisterRejectsBadInput(t *testing.T) {
	s, _ := newService()
	ctx := context.Background()

	if _, err := s.Register(ctx, " alice", "abc1234"); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("首尾空格的用户名返回 %v，期望 ErrInvalidUsername", err)
	}
	if _, err := s.Register(ctx, "alice", "abc123"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("6 位密码返回 %v，期望 ErrWeakPassword", err)
	}
	// 注册失败不该被误判成登录失败
	if _, err := s.Register(ctx, "alice", "abc123"); errors.Is(err, ErrBadCredentials) {
		t.Error("注册的校验错误被包成了 ErrBadCredentials")
	}
}

// 仓储的重名错误必须被翻译成本层的哨兵，否则 api 层要 import internal/store
// 才能判错 —— 分层就破了。
func TestRegisterTranslatesDuplicate(t *testing.T) {
	s, _ := newService()
	ctx := context.Background()

	if _, err := s.Register(ctx, "alice", "abc1234"); err != nil {
		t.Fatalf("第一次 Register: %v", err)
	}

	_, err := s.Register(ctx, "alice", "abc1234")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("重名返回 %v，期望 ErrUsernameTaken", err)
	}
	if errors.Is(err, store.ErrUsernameTaken) {
		t.Error("store 的哨兵直接漏到用例层外面了")
	}
}

// 并发注册同一个用户名：恰好 1 个成功，其余全是 ErrUsernameTaken。
//
// 验的是"仓储报了冲突之后，服务层不漏出别的错误、也没人绕过它照常成功"。
//
// 它**不**证明"唯一性来自数据库而不是先查再插"——那是 store 的
// TestCreateUserRejectsDuplicate 在真库上守的。这里用的是假 Repo，它的 CreateUser
// 本身也是原子的，所以服务层就算多加一次"先查再插"，这条照样能过（实测过）。
// 而那只是冗余查询、不是 bug：数据库的唯一索引仍然兜住了正确性。
func TestRegisterConcurrentSameUsername(t *testing.T) {
	s, _ := newService()

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Register(context.Background(), "alice", "abc1234")
		}()
	}
	wg.Wait()

	ok := 0
	for i, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrUsernameTaken):
		default:
			t.Errorf("第 %d 个返回了意料之外的错误: %v", i, err)
		}
	}
	if ok != 1 {
		t.Errorf("成功了 %d 个，期望恰好 1 个", ok)
	}
}

// ── Login ────────────────────────────────────────────────────────────

// 两条失败路径必须返回**同一个错误值**，不只是同一句话 ——
// api 层可能会拿它做分支，而任何区别都是给攻击者的探针。
func TestLoginDoesNotDistinguishNonexistentFromWrongPassword(t *testing.T) {
	s, _ := newService()
	ctx := context.Background()

	if _, err := s.Register(ctx, "alice", "abc1234"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, wrongPw := s.Login(ctx, "alice", "wrong1234")
	_, noSuch := s.Login(ctx, "nobody", "abc1234")

	if wrongPw == nil || noSuch == nil {
		t.Fatalf("两次登录都该失败，实际 %v / %v", wrongPw, noSuch)
	}
	if wrongPw != noSuch {
		t.Errorf("两条路径的错误值不同：密码错 → %v，用户不存在 → %v", wrongPw, noSuch)
	}
}

// 两条失败路径都要真的跑过 bcrypt。
//
// 这本来是个 timing 断言（墙钟＝flaky，一般不该写），但它守的是一个真攻击面：
// 把 Login 里那行 VerifyDummy 删掉，"用户不存在"就是 ~0ms、"密码错误"是 ~65ms，
// 一个数量级的差别，光量时间就能枚举用户名。
//
// 判据只给一个很松的下界：抓的是"压根没跑 bcrypt"这个量级差，不是精确耗时。
func TestLoginPathsBothRunBcrypt(t *testing.T) {
	s, _ := newService()
	ctx := context.Background()

	if _, err := s.Register(ctx, "alice", "abc1234"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	timeIt := func(username string) time.Duration {
		start := time.Now()
		if _, err := s.Login(ctx, username, "wrong1234"); err == nil {
			t.Fatal("这次登录本该失败")
		}
		return time.Since(start)
	}

	const floor = 10 * time.Millisecond // bcrypt DefaultCost 是几十毫秒量级
	if d := timeIt("alice"); d < floor {
		t.Errorf("密码错误的路径只用了 %v —— 没跑 bcrypt？", d)
	}
	if d := timeIt("nobody"); d < floor {
		t.Errorf("用户不存在的路径只用了 %v —— VerifyDummy 被删了？", d)
	}
}

func TestLoginIssuesUsableToken(t *testing.T) {
	s, _ := newService()
	ctx := context.Background()

	if _, err := s.Register(ctx, "alice", "abc1234"); err != nil {
		t.Fatalf("Register: %v", err)
	}

	tok, err := s.Login(ctx, "alice", "abc1234")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// 拿回来的票必须真的是这个人的
	got, err := s.Authenticate(ctx, tok)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got.Username != "alice" {
		t.Errorf("票认出来的是 %q，期望 alice", got.Username)
	}
}

// ── Logout / Authenticate ────────────────────────────────────────────

// 登出幂等：坏令牌也返回 nil —— 用户的目的（这张票不作数）已经达成，
// 报错只会让他重试卡住，而重试也不会变好。
func TestLogoutIsIdempotent(t *testing.T) {
	s, _ := newService()
	ctx := context.Background()

	if _, err := s.Register(ctx, "alice", "abc1234"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tok, err := s.Login(ctx, "alice", "abc1234")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	if err := s.Logout(ctx, tok); err != nil {
		t.Fatalf("第一次登出: %v", err)
	}
	if err := s.Logout(ctx, tok); err != nil {
		t.Errorf("第二次登出返回 %v，期望 nil", err)
	}
	if err := s.Logout(ctx, "not-a-token"); err != nil {
		t.Errorf("登出一个坏令牌返回 %v，期望 nil", err)
	}
}

// 注销之后那张票必须真的不能用 —— 这条盯的是 Authenticate 有没有查黑名单。
func TestAuthenticateRejectsRevokedToken(t *testing.T) {
	s, _ := newService()
	ctx := context.Background()

	if _, err := s.Register(ctx, "alice", "abc1234"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tok, err := s.Login(ctx, "alice", "abc1234")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := s.Authenticate(ctx, tok); err != nil {
		t.Fatalf("注销之前就该能用: %v", err)
	}

	if err := s.Logout(ctx, tok); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := s.Authenticate(ctx, tok); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("注销之后 Authenticate 返回 %v，期望 ErrInvalidToken", err)
	}
}

// 票是好的，但它指向的用户没了。
//
// 必须翻译成 auth.ErrInvalidToken（api 映射成 401），不能让 store.ErrNoUser
// 漏出去——那会落进 writeServiceError 的 default 分支，回一个**假的 500**：
// 前端以为服务器坏了，日志里刷一堆假错误，用户连登出都做不到。
func TestAuthenticateTreatsDeletedUserAsInvalidToken(t *testing.T) {
	s, repo := newService()
	ctx := context.Background()

	if _, err := s.Register(ctx, "alice", "abc1234"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tok, err := s.Login(ctx, "alice", "abc1234")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	repo.delete("alice")

	_, err = s.Authenticate(ctx, tok)
	if !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("已删用户的票返回 %v，期望 auth.ErrInvalidToken", err)
	}
	// 也不能把仓储的哨兵漏到用例层外面
	if errors.Is(err, store.ErrNoUser) {
		t.Error("store.ErrNoUser 直接漏到用例层外面了")
	}
}
