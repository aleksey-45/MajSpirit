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
//
// 也就是说 "A×72" 和 "A×72"+"任意后缀" 会得到同一个哈希、都能登录成功。
// 拿短密码测永远测不出来，所以这里显式拒绝。
//
// 导出是因为密码策略（最短几位、要哪些字符类）在 internal/user，
// 而那里也要卡这个上限。两边共用同一个常量，就不会出现"策略放行、
// 哈希拒绝"这种自相矛盾的状态。
const MaxPasswordBytes = 72

// HashPassword 用 bcrypt 哈希明文密码。
//
// bcrypt 自带随机盐，所以同一个密码两次调用会得到不同的哈希，
// 但两次都能校验通过。这是判断"到底有没有加盐"的直观方法。
//
// 这里只管 bcrypt 的字节上限，不管密码策略。策略（最短几位、要哪些
// 字符类）是产品决定，归 internal/user 的 ValidatePassword。分开的理由：
// 这一层要是也开始挑三拣四，测试就没法用一个 "right-password" 这种
// 不含数字的串来测哈希本身了。
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
// bcrypt 的比较是常量时间的，不需要额外包一层 subtle.ConstantTimeCompare。
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// dummyHash 是明文 "RoFtaCD" 的 bcrypt 哈希。
const dummyHash = "$2a$10$UD7zMhmVdLBW109Cdt2hHOjNqi1KrHxNwkiEsY4Dw0h2arAc0ynMu"

// VerifyDummy 在"用户不存在"时调用，消耗与真实校验相当的时间。
//
// 返回值被刻意丢弃：这里的唯一目的就是花掉那 65ms。
func VerifyDummy(plain string) {
	_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(plain))
}

// tokenIssuer 是签发者标识，会被写进 token 并在校验时比对。
//
// 没有它的话，同一个 secret 签出来的、给别的用途的 token 也能通过这里的校验。
// 和 docs/牌山可验证性.md §3.6 讲的"域分隔串"是同一个思路：
// 让不同用途的东西在密码学上不相交。
const tokenIssuer = "majspirit/user"

// ErrInvalidToken 是所有令牌校验失败的统一出口。
//
// 对外不区分"签名错"、"过期"、"格式错"——调用方要的只是"这张票不作数"，
// 而细分原因只会给攻击者提供信息。要排查时看 `%w` 包住的底层错误。
var ErrInvalidToken = errors.New("令牌无效")

// IssueToken 用 HS256 签一个 JWT。
func IssueToken(userID int64, secret []byte, ttl time.Duration) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("JWT secret 为空——拒绝签发")
	}
	if ttl <= 0 {
		return "", fmt.Errorf("ttl 必须为正，得到 %v", ttl)
	}

	// jti：一个随机 ID，让"同一秒内签发的两张票"不是同一张票。
	//
	// 不加它的后果是实测出来的：claims 只有 sub/iss/iat/exp，而 iat/exp 是
	// **秒精度**、HS256 签名又是确定性的 ⇒ 同一个用户在同一秒内登入两次，
	// 拿到的是**逐字节相同**的 token。
	//
	// 这不是安全漏洞（token 仍然不可预测，仍然需要 secret），但它意味着：
	// 用户在两台设备上同秒登入，本质上拿到的是同一个会话；而且没有 jti
	// 就**永远无法区分会话**——将来想"只踢掉某个设备"或者做会话列表，做不了。
	id, err := randomID()
	if err != nil {
		return "", fmt.Errorf("生成 token ID: %w", err)
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		ID: id,
		// userID 放 Subject 而不是自定义字段：这是一个已经被所有 JWT 库
		// 和网关理解的标准位置，不需要额外约定。
		Subject:   strconv.FormatInt(userID, 10),
		Issuer:    tokenIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// randomID 生成 16 字节的随机 ID，hex 编码。
//
// 这里**必须**是真随机，而且必须每次调用都不同——它存在的全部意义就是
// "让两次签发产生不同的结果"。（对比 dummyHash：那里随机只是为了拿到一个
// 没人能反推的明文，取值本身不影响任何行为。）
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ParseToken 校验并解析 token，返回 userID。
//
// 校验四件事：签名、算法、签发者、过期时间。少任何一条都能被绕过——
// 尤其是算法，见下面的注释。
func ParseToken(token string, secret []byte) (int64, error) {
	if len(secret) == 0 {
		return 0, fmt.Errorf("%w: JWT secret 为空", ErrInvalidToken)
	}

	var claims jwt.RegisteredClaims

	_, err := jwt.ParseWithClaims(token, &claims,
		func(t *jwt.Token) (any, error) {
			// 必须显式断言算法，这是最经典的一个坑。
			//
			// JWT 的 header 里写着用什么算法签的，而这个 header 是攻击者
			// 可以改的。把 alg 改成 "none" 并删掉签名段，天真的实现会
			// 认为"这个 token 声明它不需要签名"，于是放行。
			//
			// 说清楚这一处的实际作用（变异测试的结论）：
			//
			//   - 它拦不住 HS512 —— HS512 也是 *SigningMethodHMAC，
			//     类型断言会放它过去。真正拦住 HS512 的是下面的 WithValidMethods。
			//   - 它也不是 alg:none 的防线 —— 库自己就挡住了 none
			//     （它要求 key 是 UnsafeAllowNoneSignatureType）。
			//
			// 那为什么还留？因为它是类型层面的检查，不依赖 alg 字符串的
			// 拼写。将来有人改了 WithValidMethods 的写法、或引入了新的签名
			// 方法时，这一层还在。安全代码里冗余是好事。
			//
			// 被测试真正覆盖的是 WithValidMethods：
			// TestParseTokenRejectsOtherAlgorithms 删掉它会挂。
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
