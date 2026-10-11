package user

import (
	"errors"
	"fmt"
	"strings"
)

// maxUsernameLen 是上界。下界 1 和下面那条"不能为空"是同一件事，
// 所以不另写检查——那会是一段永远走不到的分支。
const maxUsernameLen = 32

// ErrInvalidUsername 是用户名不合规的统一出口，用 errors.Is 判断。
var ErrInvalidUsername = errors.New("用户名不符合要求")

// ValidateUsername 检查用户名：字符集与密码相同（ASCII 可打印 0x20–0x7E）、
// 长度 1–32、不能为空。
//
// 首尾空格是拒绝而不是 trim：存进去的必须就是用户敲的那个字符串，否则"注册时
// 是什么、登录时就得敲什么"这条会断。
//
// 全空格也在这里挡住——数据库那条 CHECK 会拒，但用户看到的会是 23514 而不是人话。
//
// 只用于注册和改密码，不要用在登录上。
func ValidateUsername(name string) error {
	// 与 ValidatePassword 里那条范围判断重复是有意的：一致性由
	// TestCharsetIsIdenticalForUsernameAndPassword 逐个码点钉住。
	for _, r := range name {
		if r < 0x20 || r > 0x7E {
			return fmt.Errorf("%w：只能使用字母、数字和符号", ErrInvalidUsername)
		}
	}

	// 上面已保证全是 ASCII，所以 TrimSpace 只可能吃掉空格，len() 就是字符数。
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("%w：不能为空", ErrInvalidUsername)
	}
	if trimmed != name {
		return fmt.Errorf("%w：首尾不能有空格", ErrInvalidUsername)
	}
	if len(name) > maxUsernameLen {
		return fmt.Errorf("%w：最多 %d 个字符（当前 %d 个）",
			ErrInvalidUsername, maxUsernameLen, len(name))
	}
	return nil
}
