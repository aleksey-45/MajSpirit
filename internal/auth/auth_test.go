package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// auth_test.go —— 密码与令牌。
//
// 这个文件里几乎每一条都对应一个真实的攻击手法，不是"覆盖分支"。
// 改 `auth.go` 的时候，先看这些用例为什么长这样。

var testSecret = []byte("test-secret-至少32字节-不该进仓库-这只是测试用")

// ── 密码 ─────────────────────────────────────────────────────────────

func TestHashPasswordIsSalted(t *testing.T) {
	const pw = "correct horse battery staple"

	h1, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	h2, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword 第二次: %v", err)
	}

	// 同一个密码两次哈希必须不同——这就是"有盐"的定义。
	// 相同的话说明没加盐，一张彩虹表就能同时破解所有用户。
	if h1 == h2 {
		t.Error("两次哈希相同 —— 没有加盐")
	}

	// 但两个哈希都必须能验过
	if !VerifyPassword(h1, pw) || !VerifyPassword(h2, pw) {
		t.Error("哈希无法校验原密码")
	}
}

func TestVerifyPasswordRejectsWrongInput(t *testing.T) {
	h, err := HashPassword("right-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	// 三种错法：完全不同、差一个字符、只差大小写。
	// 三者走的是同一条分支（bcrypt 比较失败），所以不必要再堆
	// "少一个字符""多一个空格"的变体——那些不会多验出任何东西。
	for _, wrong := range []string{
		"",
		"wrong-password",
		"Right-Password", // 密码是大小写敏感的
	} {
		if VerifyPassword(h, wrong) {
			t.Errorf("VerifyPassword(%q) 通过了，但它不该通过", wrong)
		}
	}
}

// bcrypt 只看前 72 字节，超出部分静默丢弃。
// 不显式拒绝的话，"X×72" 和 "X×72"+任意后缀 会是同一个密码。
func TestHashPasswordRejectsOver72Bytes(t *testing.T) {
	ok := strings.Repeat("A", MaxPasswordBytes)
	if _, err := HashPassword(ok); err != nil {
		t.Fatalf("正好 %d 字节应该被接受: %v", MaxPasswordBytes, err)
	}

	tooLong := strings.Repeat("A", MaxPasswordBytes+1)
	_, err := HashPassword(tooLong)
	if err == nil {
		t.Fatal("超过 72 字节应该被拒绝 —— 否则后缀会被静默丢弃")
	}
	if !strings.Contains(err.Error(), "72") {
		t.Errorf("错误信息里应该提到 72，实际: %v", err)
	}
}

// dummyHash 现在是硬编码的，所以它的正确性全靠这条盯着。
//
// 硬编码没有安全问题，唯一的风险是成本漂移：bcrypt 校验花多少时间，
// 完全由哈希串里写的那个成本决定。成本一旦和 bcrypt.DefaultCost 分叉，
// "用户不存在"就会比"密码错误"快（或慢）一个数量级——登录接口重新泄露了
// "这个用户名到底存不存在"，而代码里没有任何地方看起来是坏的。
//
// 这条是真有牙的：把 dummyHash 里的 "$2a$10$" 改成 "$2a$04$"，
// 它会挂（实测 1.0ms vs 65.0ms）。
func TestDummyHashIsUsable(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyHash))
	if err != nil {
		t.Fatalf("dummyHash 不是合法的 bcrypt 哈希（%v）。\n"+
			"这种情况下 CompareHashAndPassword 会立刻返回，\n"+
			"VerifyDummy 就变相成了空函数，耗时旁路重新打开。", err)
	}

	if cost != bcrypt.DefaultCost {
		t.Fatalf("dummyHash 的成本是 %d，但 HashPassword 用的是 %d ——\n"+
			"两条路径耗时不同，登录接口就泄露了\"用户名是否存在\"。\n\n"+
			"重新生成（明文随意，改了记得同步 auth.go 里那行注释）：\n\n"+
			"    h, _ := bcrypt.GenerateFromPassword([]byte(\"RoFtaCD\"), bcrypt.DefaultCost)\n"+
			"    fmt.Printf(\"%%q\\n\", h)\n\n"+
			"然后粘回 auth.go 的 dummyHash。", cost, bcrypt.DefaultCost)
	}
}

// VerifyDummy 必须是真的跑了一次 bcrypt，不能是个空函数——
// 它存在的全部意义就是消耗时间，好让"用户不存在"和"密码错误"耗时接近。
func TestVerifyDummyActuallyHashes(t *testing.T) {
	start := time.Now()
	VerifyDummy("anything")
	elapsed := time.Since(start)

	// bcrypt DefaultCost(10) 是几十毫秒量级；1ms 是个很松的下界，
	// 只用来抓"被人改成空函数"这种情况。
	if elapsed < time.Millisecond {
		t.Errorf("VerifyDummy 只用了 %v —— 它看起来没有真的跑 bcrypt", elapsed)
	}
}

// ── 令牌：正常路径 ────────────────────────────────────────────────────

func TestIssueParseRoundTrip(t *testing.T) {
	for _, want := range []int64{1, 42, 1 << 40} {
		tok, err := IssueToken(want, testSecret, time.Hour)
		if err != nil {
			t.Fatalf("IssueToken(%d): %v", want, err)
		}

		got, err := ParseToken(tok, testSecret)
		if err != nil {
			t.Fatalf("ParseToken: %v", err)
		}
		if got != want {
			t.Errorf("userID = %d，期望 %d", got, want)
		}
	}
}

func TestIssueTokenRejectsBadArgs(t *testing.T) {
	if _, err := IssueToken(1, nil, time.Hour); err == nil {
		t.Error("空 secret 应该被拒绝 —— 否则等于签了一张谁都能伪造的票")
	}
	// 0 和负数走的是同一条分支（ttl <= 0），只留一个。
	if _, err := IssueToken(1, testSecret, 0); err == nil {
		t.Error("ttl=0 应该被拒绝 —— 那会签出一个立刻过期的 token")
	}
}

// jti 的守卫。
//
// 没有 jti 的话，claims 只有 sub/iss/iat/exp，而 iat/exp 是秒精度、
// HS256 签名是确定性的 ⇒ 同一用户在同一秒内登入两次会拿到逐字节相同的 token。
// 实测过：两次调用只差几微秒，token 一模一样。
//
// 这条几乎不可能偶发通过：它要 100 次调用全部落在同一秒才可能漏掉重复，
// 而 100 次 HS256 签名远不到一秒。
func TestIssueTokenIsUniqueWithinSameSecond(t *testing.T) {
	const n = 100
	seen := make(map[string]bool, n)

	for i := range n {
		tok, err := IssueToken(7, testSecret, time.Hour)
		if err != nil {
			t.Fatalf("第 %d 次签发: %v", i, err)
		}
		if seen[tok] {
			t.Fatalf("第 %d 次签发出了重复的 token —— jti 丢了吗？", i)
		}
		seen[tok] = true
	}
}

// ── 令牌：攻击路径 ────────────────────────────────────────────────────

// 这里删掉过一条 TestParseTokenRejectsAlgNone：手工拼一个 alg:none 的 token，
// 看会不会被放行。
//
// 删的理由是变异测试的结果——把 auth.go 里两处算法 guard 全删掉，它照样过。
// 真正挡住 none 的是 golang-jwt 自己（它要求 key 是
// UnsafeAllowNoneSignatureType，而我们返回 []byte）。
//
// 也就是说它测的是库的默认行为，不是我的代码。而一条测不出自己代码坏掉的
// 测试，代价不只是白写：它给的安全感是假的，将来真出问题时你会因为它绿着
// 而放过。真正盯住这条防线的是下面那条。
//
// 这条才是真正在测 `jwt.WithValidMethods` 的。
//
// HS512 和 HS256 同属 HMAC 家族，库默认两者都接受。所以去掉
// `jwt.WithValidMethods([]string{"HS256"})` 之后，一个用同一个 secret
// 签出来的 HS512 token 会被放行——而本服务端只该认自己签发时用的那一种。
//
// 严格说这不算可利用的漏洞（攻击者仍然需要 secret 才能签），但
// "只接受自己用的那一种算法"是个不需要商量的约定，而且它挡住了一整类
// 算法替换攻击（RS256→HS256 那种）。变异测试确认：删掉 WithValidMethods，
// 这条会挂。
func TestParseTokenRejectsOtherAlgorithms(t *testing.T) {
	claims := jwt.RegisteredClaims{
		Subject:   "1",
		Issuer:    tokenIssuer,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString(testSecret)
	if err != nil {
		t.Fatalf("签 HS512 测试 token: %v", err)
	}

	if _, err := ParseToken(tok, testSecret); err == nil {
		t.Fatal("HS512 签的 token 被接受了 —— 服务端应该只认 HS256")
	}
}

func TestParseTokenRejectsTamperedPayload(t *testing.T) {
	tok, err := IssueToken(1, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	// 把 sub 改成 999，保留原签名 —— 签名立刻对不上
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token 应该有三段，实际 %d", len(parts))
	}
	forgedPayload := base64.RawURLEncoding.EncodeToString(
		[]byte(fmt.Sprintf(`{"sub":"999","iss":%q,"exp":%d}`,
			tokenIssuer, time.Now().Add(time.Hour).Unix())))
	forged := parts[0] + "." + forgedPayload + "." + parts[2]

	if _, err := ParseToken(forged, testSecret); err == nil {
		t.Fatal("改了 payload 的 token 被接受了 —— 签名没起作用")
	}
}

func TestParseTokenRejectsWrongSecret(t *testing.T) {
	tok, _ := IssueToken(1, testSecret, time.Hour)

	if _, err := ParseToken(tok, []byte("完全不同的另一个 secret")); err == nil {
		t.Fatal("用别的 secret 竟然验过了")
	}
	if _, err := ParseToken(tok, nil); err == nil {
		t.Fatal("空 secret 竟然验过了")
	}
}

func TestParseTokenRejectsExpired(t *testing.T) {
	// 签一个已经过期的：ttl 为负会被 IssueToken 拒绝，
	// 所以手工签一个过去的时间点。
	claims := jwt.RegisteredClaims{
		Subject:   "1",
		Issuer:    tokenIssuer,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testSecret)
	if err != nil {
		t.Fatalf("签测试 token: %v", err)
	}

	if _, err := ParseToken(tok, testSecret); err == nil {
		t.Fatal("过期的 token 被接受了")
	}
}

// 不带 exp 的 token 默认永不过期。WithExpirationRequired 就是为它准备的。
func TestParseTokenRejectsMissingExpiry(t *testing.T) {
	claims := jwt.RegisteredClaims{Subject: "1", Issuer: tokenIssuer}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testSecret)
	if err != nil {
		t.Fatalf("签测试 token: %v", err)
	}

	if _, err := ParseToken(tok, testSecret); err == nil {
		t.Fatal("没有 exp 的 token 被接受了 —— 它会永不过期")
	}
}

// 同一个 secret 签的、但 issuer 不同的 token，也必须拒绝。
// 对应 docs/牌山可验证性.md 里"域分隔"的思路：让不同用途的东西不相交。
func TestParseTokenRejectsWrongIssuer(t *testing.T) {
	claims := jwt.RegisteredClaims{
		Subject:   "1",
		Issuer:    "some-other-service",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(testSecret)
	if err != nil {
		t.Fatalf("签测试 token: %v", err)
	}

	if _, err := ParseToken(tok, testSecret); err == nil {
		t.Fatal("别的 issuer 签的 token 被接受了 —— 域分隔没起作用")
	}
}

// 顺带盯住 ErrInvalidToken 这个哨兵：api 层要靠 errors.Is 把"票不作数"
// 映射成 401，所以"校验失败时包的确实是它"必须有人断言。
func TestParseTokenRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "not-a-token", "a.b", "a.b.c.d", "....."} {
		_, err := ParseToken(bad, testSecret)
		if err == nil {
			t.Errorf("ParseToken(%q) 竟然成功了", bad)
			continue
		}
		if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("ParseToken(%q) 返回了 %v，期望能用 errors.Is 判成 ErrInvalidToken", bad, err)
		}
	}
}

// ── 黑名单 ────────────────────────────────────────────────────────────

func TestRevoker(t *testing.T) {
	r := NewRevoker()

	if r.IsRevoked("t1") {
		t.Error("空黑名单说 t1 已注销")
	}

	r.Revoke("t1", time.Hour)
	if !r.IsRevoked("t1") {
		t.Error("刚注销的 token 说没注销")
	}
	if r.IsRevoked("t2") {
		t.Error("没注销过的 token 被判定为已注销 —— 那会把所有人踢下线")
	}

	// 已经到期的条目不该再拦人（它本来就已经失效了）
	r.Revoke("expired", 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if r.IsRevoked("expired") {
		t.Error("已过期的条目还在拦人")
	}
}

func TestRevokerCleanup(t *testing.T) {
	r := NewRevoker()
	r.Revoke("a", 10*time.Millisecond)
	r.Revoke("b", 10*time.Millisecond)
	r.Revoke("c", time.Hour)

	time.Sleep(20 * time.Millisecond)

	if n := r.Cleanup(); n != 2 {
		t.Errorf("Cleanup 清了 %d 条，期望 2", n)
	}
	if r.Len() != 1 {
		t.Errorf("清理后剩 %d 条，期望 1", r.Len())
	}
}

func TestRevokerIgnoresBadInput(t *testing.T) {
	r := NewRevoker()
	r.Revoke("", time.Hour)   // 空 token 不记
	r.Revoke("x", 0)          // 非正 TTL 不记
	r.Revoke("y", -time.Hour) // 同上

	if r.Len() != 0 {
		t.Errorf("黑名单里有 %d 条，期望 0", r.Len())
	}
}

// 黑名单会被多个请求 goroutine 同时访问 —— `-race` 下这条才有意义。
func TestRevokerConcurrent(t *testing.T) {
	r := NewRevoker()
	const n = 50

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok := fmt.Sprintf("token-%d", i)
			r.Revoke(tok, time.Hour)
			_ = r.IsRevoked(tok)
			_ = r.IsRevoked("never-revoked")
		}()
	}
	wg.Wait()

	if r.Len() != n {
		t.Errorf("黑名单里有 %d 条，期望 %d", r.Len(), n)
	}
}
