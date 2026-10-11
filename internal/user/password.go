package user

import (
	"errors"
	"fmt"

	"github.com/aleksey-45/majspirit/internal/auth"
)

const (
	// minPasswordLen 对应"大于六位"：至少 7 个字符。
	minPasswordLen = 7

	// maxPasswordLen 直接用 bcrypt 的硬限制，不另写一个 72——两个包各写一遍的话，
	// 早晚有一个被改、另一个没人记得。
	maxPasswordLen = auth.MaxPasswordBytes
)

// ErrWeakPassword 是密码不合规的统一出口，用 errors.Is 判断，别比字符串。
var ErrWeakPassword = errors.New("密码不符合要求")

// ValidatePassword 检查明文密码：至少一个 ASCII 字母、至少一个 ASCII 数字、
// 其余位置可以是符号但必须落在 ASCII 可打印区（0x20–0x7E）、长度 7–72。
//
// 只用于注册和改密码，绝不能用在登录上——策略一收紧，老用户当场就登不进来；
// 而且报出"密码不符合要求"等于把本系统的密码规则白送给随便试的人。
//
// 只允许 ASCII 可打印字符是有代价的（不能拿中文当密码），换来的是长度单位和
// bcrypt 的字节上限天然一致，不用在 rune 和 byte 之间换算，也就没有"7 个汉字 =
// 21 字节算不算够 7 位"这种边界。要放开不能只改这几行：还得处理 Unicode 规范化
// （é 有 U+00E9 和 U+0065 U+0301 两种写法，不统一就会注册得了、登录不上）。
//
// 不检查密码是否等于用户名。要加就加在这儿，别散到调用方。
func ValidatePassword(plain string) error {
	// 顺序有讲究：先扫字符集再量长度。反过来先量长度的话，一个 100 字节的中文
	// 密码会得到"最多 72 个字符"，真问题（里面有非 ASCII）反而被盖住。
	//
	// 控制字符（\n \t NUL DEL）必须挡住：它们能把用户输入变成往日志、HTTP 头、
	// CSV 导出里注入的载体，只要有一处忘了转义就中招。
	//
	// 扫完之后可以确定整个串都是 ASCII，所以下面 len() 当字符数成立；非法 UTF-8
	// 字节在这里会被 range 解成 U+FFFD，同样落进范围外被拒。
	var hasLetter, hasDigit bool
	for _, r := range plain {
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
