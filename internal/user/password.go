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
// 只用于注册和改密码，不要用在登录上——策略变了，老用户会当场登不进来。
func ValidatePassword(plain string) error {
	// 顺序有讲究：先扫字符集再量长度。反过来先量长度的话，一个 100 字节的中文
	// 密码会得到"最多 72 个字符"，真问题反而被盖住。
	//
	// 范围卡在可打印区是为了把控制字符挡在外面；扫完之后整个串都是 ASCII，
	// 所以下面 len() 当字符数成立。
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
