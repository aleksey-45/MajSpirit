package user

import (
	"errors"
	"fmt"
	"strings"
)

// maxUsernameLen 是用户名长度的上界。下界是 1，它和下面那条"不能为空"
// 是同一件事，所以只需要卡上界——不然会留下一段永远走不到的分支。
const maxUsernameLen = 32

// ErrInvalidUsername 是用户名不合规的统一出口，用 errors.Is 判断。
var ErrInvalidUsername = errors.New("用户名不符合要求")

// ValidateUsername 检查用户名是否满足规则：
//
//   - 字符集和密码完全一样：ASCII 可打印区（0x20–0x7E）
//   - 长度在 [1, 32] 以内
//   - 不能为空，且首尾不能有空格
//
// 最后一条是对"和密码一样"的一处有意收紧，理由和密码不同：密码是私密的，
// 多打一个空格只坑自己；用户名是给别人看的，`alice` 和 `alice `（尾随一个
// 空格）在界面上长得一模一样，却是两个账号。这类冒充空间是隐形的，
// 比 `Alice` / `alice` 那种（至少肉眼能分辨）危险得多。
//
// 首尾空格是拒绝，不是 trim。trim 掉的话，"注册时是什么、存进去就是
// 什么"这条就断了；将来一旦忘了在哪一层 trim，用户就登不进来了。
//
// 登录路径不校验这条。 理由和密码一样，而且更强：用户名规则一旦收紧，
// 老账号的名字可能不再合规，校验了就是把人锁在门外。登录只做精确匹配，
// 匹配不上就是"用户名或密码错误"。
//
// "全是空格"这条不是可选项：migrations/0001_init.sql 里有这么一条约束，
//
//	CHECK (btrim(username) <> '')
//
// 不在这里拦住的话，用户会拿到一个 23514（check_violation），
// 而不是一条看得懂的错误。
//
// 上面那条约束写成缩进代码块、而不是行内反引号，是因为 gofmt 的文档注释
// 格式化会把两个连续的单引号当成 TeX 式的右引号换成 `”`，把 SQL 改成
// 一个语法都不对的样子。缩进块是唯一免疫的写法。
func ValidateUsername(name string) error {
	// 这条范围判断和 ValidatePassword 里的长得一模一样，是有意分开写的。
	// 两边是否真的一致，由 TestCharsetIsIdenticalForUsernameAndPassword
	// 逐个 ASCII 码点钉住。
	for _, r := range name {
		if r < 0x20 || r > 0x7E {
			return fmt.Errorf("%w：只能使用字母、数字和符号", ErrInvalidUsername)
		}
	}

	// 到这里字符已经全部落在 0x20–0x7E，所以 TrimSpace 只可能吃掉空格
	// （\t \n \r 那些早就被上面拦下了）。
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		// 字符集已经过关，所以"trim 完是空"只能是"整串都是空格"
		return fmt.Errorf("%w：不能为空", ErrInvalidUsername)
	}
	if trimmed != name {
		return fmt.Errorf("%w：首尾不能有空格", ErrInvalidUsername)
	}

	// 长度是闭区间 [1, 32]。下界 1 已经被上面两条覆盖了，这里只卡上界。
	// 同上，字符集决定了 len() 就是字符数。
	if len(name) > maxUsernameLen {
		return fmt.Errorf("%w：最多 %d 个字符（当前 %d 个）",
			ErrInvalidUsername, maxUsernameLen, len(name))
	}
	return nil
}
