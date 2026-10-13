package ws

import "testing"

// TestStatusCodesMatchProtocol 把关闭码的数字钉死。
//
// 为什么单独写一条：别的测试断言的是 `CloseStatus(err) == statusAuthFailed`，
// 一旦把常量本身改成别的值，实现和断言会一起漂、测试照样绿 —— 变异测试实测过
// （把 4401 改成 1000，三条测试全绿）。协议里的数字必须有一处字面量守着。
//
// 依据：协议.md §4.6（4400 协议错误 / 4401 未认证或认证失败），
// 以及 §4.1（5 秒未认证与认证失败都用 4401）。
func TestStatusCodesMatchProtocol(t *testing.T) {
	if statusAuthFailed != 4401 {
		t.Errorf("认证失败/超时的关闭码 = %d, 协议 §4.6 定的是 4401", statusAuthFailed)
	}
	if statusProtocolError != 4400 {
		t.Errorf("协议错误的关闭码 = %d, 协议 §4.6 定的是 4400", statusProtocolError)
	}
}
