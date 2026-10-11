package user

import (
	"errors"
	"strings"
	"testing"

	"github.com/aleksey-45/majspirit/internal/auth"
)

// 密码有两条独立的约束：长度（需求是"大于六位"，不是"至少六位"）和字符类。
// 表里每个分支只留一行，外加两个长度边界。
//
// 字符集（ASCII 可打印）故意不在这些表里测：那是
// TestCharsetIsIdenticalForUsernameAndPassword 的活，它把 0x00–0x7F 逐个
// 码点走了一遍。在这边再抄一遍汉字、emoji、换行，只会让表看起来很长，
// 鉴别力一点不涨——变异分数不会因为这些行改变。

func TestValidatePasswordAccepts(t *testing.T) {
	cases := []struct {
		why string
		pw  string
	}{
		{"最短的合法密码：正好 7 位（下界）", "abc1234"},
		{"大写字母也算字母（大小写是两个子条件）", "ABC1234"},
		{"符号可以随便用", "a1!@#$%^&*()"},
		{"空格算符号，可以出现在中间", "a 12345"},
		{"正好 72 字符（bcrypt 上限）", strings.Repeat("a1", 36)},
	}

	for _, tc := range cases {
		t.Run("接受/"+tc.why, func(t *testing.T) {
			if err := ValidatePassword(tc.pw); err != nil {
				t.Errorf("ValidatePassword(%q) = %v，期望通过", tc.pw, err)
			}
		})
	}
}

func TestValidatePasswordRejects(t *testing.T) {
	cases := []struct {
		why string
		pw  string
	}{
		{"6 位，差一位（需求是大于六位）", "abc123"},
		{"够长但一个数字都没有", "abcdefg"},
		{"够长但一个字母都没有", "1234567"},
		// 这一行代表整个字符集分支。它还确认了这条路径也走 ErrWeakPassword，
		// 而不只是"返回了某个错误"。
		{"含有控制字符（换行）", "abc123\n"},
		{"73 字符，超 bcrypt 上限一位", strings.Repeat("a1", 36) + "x"},
	}

	for _, tc := range cases {
		t.Run("拒绝/"+tc.why, func(t *testing.T) {
			err := ValidatePassword(tc.pw)
			if err == nil {
				t.Fatalf("ValidatePassword(%q) 通过了，但它不该通过", tc.pw)
			}
			// 调用方（api 层）靠 errors.Is 把它映射成 400，
			// 而不是去匹配错误字符串。
			if !errors.Is(err, ErrWeakPassword) {
				t.Errorf("应该能用 errors.Is 判成 ErrWeakPassword，实际: %v", err)
			}
		})
	}
}

// 策略上限必须和 bcrypt 的硬限制是同一个数。
//
// 不共用常量的话会出现"ValidatePassword 放行、HashPassword 拒绝"这种
// 自相矛盾的状态：用户照着提示改了密码，还是注册失败，而且报的是
// 一条看不懂的字节数错误。
//
// 这条测试会被 bcrypt 那一侧的改动挂掉——这正是它的用途。
func TestPasswordLimitsAgreeWithBcrypt(t *testing.T) {
	atLimit := strings.Repeat("a1", auth.MaxPasswordBytes/2)
	if len(atLimit) != auth.MaxPasswordBytes {
		t.Fatalf("测试自己写错了：构造出 %d 字节，期望 %d", len(atLimit), auth.MaxPasswordBytes)
	}

	if err := ValidatePassword(atLimit); err != nil {
		t.Errorf("策略拒绝了正好 %d 字节的密码: %v", auth.MaxPasswordBytes, err)
	}
	if _, err := auth.HashPassword(atLimit); err != nil {
		t.Errorf("bcrypt 拒绝了正好 %d 字节的密码: %v", auth.MaxPasswordBytes, err)
	}

	overLimit := atLimit + "x"
	if err := ValidatePassword(overLimit); err == nil {
		t.Errorf("策略接受了 %d 字节的密码", len(overLimit))
	}
	if _, err := auth.HashPassword(overLimit); err == nil {
		t.Errorf("bcrypt 接受了 %d 字节的密码——它会静默丢弃超出的部分", len(overLimit))
	}
}
