package user

import (
	"errors"
	"strings"
	"testing"
)

// 用户名有四个拒绝分支：字符集、空/全空格、首尾空格、超长。
// 和密码那边一样，每个分支只留一行，字符集交给下面那条 128 码点的测试。
//
// 表短了不代表覆盖掉了：真正保证覆盖的是变异测试——
// 把任一分支敲掉，下面这些行里必有一条会挂。

func TestValidateUsernameAccepts(t *testing.T) {
	cases := []struct {
		why  string
		name string
	}{
		{"最短：正好 1 个字符（下界）", "a"},
		{"正好 32 个字符（上界）", strings.Repeat("a", 32)},
		{"中间的空格不算首尾空格，不该被误伤", "li si"},
		{"全数字也行——用户名没有强度要求", "12345"},
	}

	for _, tc := range cases {
		t.Run("接受/"+tc.why, func(t *testing.T) {
			if err := ValidateUsername(tc.name); err != nil {
				t.Errorf("ValidateUsername(%q) = %v，期望通过", tc.name, err)
			}
		})
	}
}

func TestValidateUsernameRejects(t *testing.T) {
	cases := []struct {
		why  string
		name string
	}{
		// 这一行还盯着一件事：全空格必须在进数据库之前被挡住，
		// 否则用户拿到的是 migrations 里 CHECK (btrim(username) <> '')
		// 抛出的 23514，而不是一句看得懂的话。
		{"全是空格", "   "},
		{"开头有空格", " alice"},
		{"含有控制字符（换行）", "alice\n"},
		{"33 个字符，超上限一位", strings.Repeat("a", 33)},
	}

	for _, tc := range cases {
		t.Run("拒绝/"+tc.why, func(t *testing.T) {
			err := ValidateUsername(tc.name)
			if err == nil {
				t.Fatalf("ValidateUsername(%q) 通过了，但它不该通过", tc.name)
			}
			// api 层靠 errors.Is 把它映射成 400，而不是匹配错误字符串
			if !errors.Is(err, ErrInvalidUsername) {
				t.Errorf("应该能用 errors.Is 判成 ErrInvalidUsername，实际: %v", err)
			}
		})
	}
}

// 需求原话是"用户名和密码一样"，这条把它变成可执行的断言。
//
// 做法：遍历全部 128 个 ASCII 码点，为每个码点构造一个除了这个字符之外
// 什么都对的串，再看两个校验器是不是都放行。
//
// 期望值 0x20–0x7E 是独立写出来的，不是调用被测代码算出来的——
// 否则就成了自证。也正因为如此，谁要是只改了两边中的一处，这条一定挂。
//
// 探针串的构造有讲究，不能用"整体是否通过"随便判断：
//
//   - 密码还要额外满足"至少一个字母""至少一个数字""长度 ≥7"，
//     所以首尾钉 'a'、开头再塞一个 '1'；
//   - 用户名要求首尾不是空格，所以首尾也必须钉 'a'——不然测 0x20
//     的时候会撞上"首尾不能有空格"那条规则，把字符集的问题盖住。
//
// 这样所有其它条件都提前满足了，唯一还能让它们失败的只剩字符集本身。
func TestCharsetIsIdenticalForUsernameAndPassword(t *testing.T) {
	for c := 0; c < 128; c++ {
		ch := string(rune(c))
		wantAllowed := c >= 0x20 && c <= 0x7E

		pw := "a1" + strings.Repeat(ch, 10) + "a"  // 长度 13，有字母有数字
		name := "a" + strings.Repeat(ch, 10) + "a" // 长度 12，首尾非空格

		if got := ValidatePassword(pw) == nil; got != wantAllowed {
			t.Errorf("0x%02x 出现在密码里：放行=%v，期望 %v", c, got, wantAllowed)
		}
		if got := ValidateUsername(name) == nil; got != wantAllowed {
			t.Errorf("0x%02x 出现在用户名里：放行=%v，期望 %v", c, got, wantAllowed)
		}
	}
}
