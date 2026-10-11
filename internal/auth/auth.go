// Package auth 负责两件事：密码哈希，与登录令牌的签发和校验。
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// MaxPasswordBytes 是 bcrypt 的硬限制：它只看前 72 字节，超出部分静默丢弃。
// 也就是说 "A×72" 和 "A×72"+任意后缀 是同一个密码，拿短密码永远测不出来。
//
// 导出是因为密码策略在 internal/user，那里也要卡这个上限；共用同一个常量就不会
// 出现"策略放行、哈希拒绝"这种自相矛盾的状态。
const MaxPasswordBytes = 72

// HashPassword 用 bcrypt 哈希明文密码（bcrypt 自带随机盐，所以同一个密码两次
// 调用会得到不同的哈希，但都能校验通过）。
//
// 这里只管 bcrypt 的字节上限，不管密码策略——策略是产品决定，归
// internal/user.ValidatePassword。分开是为了让测试能拿一个不含数字的串
// （如 "right-password"）去测哈希本身。
func HashPassword(plain string) (string, error) {
	if len(plain) > MaxPasswordBytes {
		return "", fmt.Errorf("密码长度 %d 字节，超过 bcrypt 的 %d 字节上限"+
			"（超出部分会被静默丢弃，所以直接拒绝）", len(plain), MaxPasswordBytes)
	}

	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("哈希密码: %w", err)
	}
	return string(h), nil
}

// VerifyPassword 校验明文是否匹配哈希。
// bcrypt 的比较本身是常量时间的，不需要再包一层 subtle.ConstantTimeCompare。
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// dummyHash 是明文 "RoFtaCD" 的哈希，用来在"用户不存在"时也花掉一次 bcrypt 的时间。
//
// 它的成本必须等于 bcrypt.DefaultCost，否则两条登录路径的耗时就会分叉，响应时间
// 变成一个可以逐字节枚举用户名的旁路（实测：成本对时 65.0ms vs 64.2ms 无差别，
// 成本抄成 4 就是 1.0ms，差 65 倍）。TestDummyHashIsUsable 盯着这一点，
// 报错里直接给出重新生成的代码。
//
// 绝不能让它出现在 users.password_hash 里——它是公开值，谁都能拿明文验过去。
const dummyHash = "$2a$10$UD7zMhmVdLBW109Cdt2hHOjNqi1KrHxNwkiEsY4Dw0h2arAc0ynMu"

// VerifyDummy 在"用户不存在"时调用，消耗与真实校验相当的时间。
// 返回值被刻意丢弃：这里的唯一目的就是花掉那 65ms。
func VerifyDummy(plain string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(plain))
}

// tokenIssuer 是签发者标识，会被写进 token 并在校验时比对。
// 没有它的话，同一个 secret 签出来的、给别的用途的 token 也能通过这里的校验——
// 和 docs/牌山可验证性.md §3.6 的"域分隔串"是同一个思路。
const tokenIssuer = "majspirit/user"

// ErrInvalidToken 是所有令牌校验失败的统一出口。
// 对外不区分"签名错/过期/格式错"：调用方要的只是"这张票不作数"，细分原因只会给
// 攻击者提供信息。要排查时看 %w 包住的底层错误。
var ErrInvalidToken = errors.New("令牌无效")

// IssueToken 用 HS256 签一个 JWT。
func IssueToken(userID int64, secret []byte, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("JWT secret 为空——拒绝签发")
	}
	if ttl <= 0 {
		return "", fmt.Errorf("ttl 必须为正，得到 %v", ttl)
	}

	// jti：没有它的话，claims 只有 sub/iss/iat/exp，而 iat/exp 是秒精度、HS256
	// 签名又是确定性的 ⇒ 同一用户同一秒内登入两次会拿到逐字节相同的 token
	// （实测过）。不是漏洞，但意味着永远无法区分会话，"只踢掉某个设备"就做不了。
	id, err := randomID()
	if err != nil {
		return "", fmt.Errorf("生成 token ID: %w", err)
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		ID: id,
		// userID 放 Subject 而不是自定义字段：这是所有 JWT 库和网关都理解的标准位置。
		Subject:   strconv.FormatInt(userID, 10),
		Issuer:    tokenIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// randomID 生成 16 字节随机 ID，hex 编码。
// 必须每次调用都不同——它存在的全部意义就是让两次签发产生不同的结果。
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ParseToken 校验并解析 token，返回 userID。
// 校验四件事：签名、算法、签发者、过期时间，少任何一条都能被绕过。
func ParseToken(token string, secret []byte) (int64, error) {
	if len(secret) == 0 {
		return 0, fmt.Errorf("%w: JWT secret 为空", ErrInvalidToken)
	}

	var claims jwt.RegisteredClaims

	_, err := jwt.ParseWithClaims(token, &claims,
		func(t *jwt.Token) (any, error) {
			// 这一处的实际作用（变异测试的结论，别把它当防线）：
			// 它拦不住 HS512（HS512 也是 *SigningMethodHMAC），也不是 alg:none 的
			// 防线（库要求 none 的 key 是 UnsafeAllowNoneSignatureType，而我们传
			// []byte）。保留它是因为它是类型层面的检查，不依赖 alg 字符串的拼写。
			//
			// 真正有牙的是下面的 WithValidMethods：删掉它，
			// TestParseTokenRejectsOtherAlgorithms 会挂。
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("意外的签名算法 %v", t.Header["alg"])
			}
			return secret, nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(tokenIssuer),
		// 不写这句的话，一个不带 exp 的 token 永不过期。
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: sub 不是合法 userID: %v", ErrInvalidToken, err)
	}
	return id, nil
}
