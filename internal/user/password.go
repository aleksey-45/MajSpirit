package user

import (
	"errors"
	"fmt"

	"github.com/aleksey-45/majspirit/internal/auth"
)

const (
	// minPasswordLen 对应"大于六位"：至少 7 个字符。
	minPasswordLen = 7

	// maxPasswordLen 直接用 bcrypt 的硬限制，不另写一个 72。
	// 两个包各写一遍的话，早晚有一个被改、另一个没人记得。
	maxPasswordLen = auth.MaxPasswordBytes
)

// ErrWeakPassword 是密码不合规的统一出口，用 errors.Is 判断，别比字符串。
var ErrWeakPassword = errors.New("密码不符合要求")

// ValidatePassword 检查明文密码是否满足注册策略：
//
//   - 至少一个 ASCII 字母（a-z / A-Z）
//   - 至少一个 ASCII 数字（0-9）
//   - 其余位置可以是符号，但必须落在 ASCII 可打印区（0x20–0x7E）
//   - 长度大于 6，且不超过 72
//
// 只用于注册和改密码，绝不能用在登录上。 登录时校验策略有两个坏处：
// 一是策略一收紧，老用户当场就登不进来了；二是会把"这个系统的密码规则"
// 白送给任何一个随便试的人。登录只该有"比对通过 / 不通过"两种结果。
//
// 关于"只允许 ASCII 可打印字符"这个决定：它换来的是长度单位和 bcrypt 的
// 字节上限天然一致——全是一字节一个字符，不用在 rune 和 byte 之间换算，
// 也就不会出现"7 个汉字 = 21 字节，到底算不算够 7 位"这种没人算得明白的
// 边界。代价是不能拿中文当密码。
//
// 要放开的话不能只改这几行：还得处理 Unicode 规范化（"é" 有 U+00E9 和
// U+0065 U+0301 两种写法，不统一就会"注册得了、登录不上"），并且重新解释
// auth.MaxPasswordBytes 这个按字节算的上限。
//
// 下面那条范围判断和 ValidateUsername 里的长得一模一样，是有意分开写的：
// 需求是"用户名和密码用同一套字符集"，而这件事由
// TestCharsetIsIdenticalForUsernameAndPassword 逐个码点钉住。它比"两边调
// 同一个函数"更强——它独立规定了范围，而不是自证。
//
// 顺带一提：这里不检查密码是否等于用户名。要加就加在这儿，
// 别散到调用方去。
func ValidatePassword(plain string) error {
	// 先扫字符集，再量长度。
	// 顺序有讲究：反过来先量长度的话，一个 100 字节的中文密码会得到
	// "最多 72 个字符"——真问题（里面有非 ASCII）反而被盖住了。
	//
	// 另外，扫完之后可以确定整个串都是 ASCII，所以下面用 len() 当字符数
	// 是成立的，不需要 utf8.RuneCountInString。
	// 非法 UTF-8 字节在这里会被 range 解成 U+FFFD，落进范围外被拒。
	var hasLetter, hasDigit bool
	for _, r := range plain {
		// 只允许 ASCII 可打印字符。控制字符（\n \t NUL DEL…）为什么必须
		// 挡住：它们能把用户输入变成往日志、HTTP 头、CSV 导出里注入的
		// 载体，只要有一处忘了转义就中招。这种洞不靠"记得转义"来堵。
		if r < 0x20 || r > 0x7E {
			return fmt.Errorf("%w：只能使用字母、数字和符号", ErrWeakPassword)
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}

	if len(plain) < minPasswordLen {
		return fmt.Errorf("%w：至少需要 %d 个字符", ErrWeakPassword, minPasswordLen)
	}
	if len(plain) > maxPasswordLen {
		return fmt.Errorf("%w：最多 %d 个字符", ErrWeakPassword, maxPasswordLen)
	}
	if !hasLetter {
		return fmt.Errorf("%w：至少需要一个字母", ErrWeakPassword)
	}
	if !hasDigit {
		return fmt.Errorf("%w：至少需要一个数字", ErrWeakPassword)
	}
	return nil
}
