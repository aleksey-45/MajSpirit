package protocol

import (
	"errors"
	"testing"
	"time"
)

// collect 记下每个收件人收到的信封。
func collect() (*Emitter, func(int64) []Envelope) {
	got := map[int64][]Envelope{}
	em := NewEmitter(func(userID int64, env Envelope) error {
		got[userID] = append(got[userID], env)
		return nil
	})
	return em, func(userID int64) []Envelope { return got[userID] }
}

// TestEmitNumbersEachRecipientSeparately 钉住"号按收件人各自连续"。
//
// 删掉它会漏掉：只发给某些人的事件也去吃公共号，别人看到假洞。
func TestEmitNumbersEachRecipientSeparately(t *testing.T) {
	em, got := collect()

	// 一条广播：三个人各拿 1。
	em.Emit([]Message{
		{UserID: 1, Env: Envelope{Type: CmdAuth}},
		{UserID: 2, Env: Envelope{Type: CmdAuth}},
		{UserID: 3, Env: Envelope{Type: CmdAuth}},
	})
	// 一条只发给 2 的事件。
	em.Emit([]Message{{UserID: 2, Env: Envelope{Type: CmdPing}}})

	for userID := int64(1); userID <= 3; userID++ {
		envs := got(userID)
		for i, env := range envs {
			if env.Seq != i+1 {
				t.Errorf("用户 %d 的第 %d 条事件 seq=%d，想要 %d（自己的流必须无洞）",
					userID, i, env.Seq, i+1)
			}
		}
	}
	if n := len(got(2)); n != 2 {
		t.Errorf("用户 2 收到 %d 条，想要 2 条", n)
	}
	if n := len(got(1)); n != 1 {
		t.Errorf("用户 1 收到 %d 条，想要 1 条（只发给 2 的那条不该算进他的流）", n)
	}
}

// TestEmitReportsPerRecipientFailure 钉住返回值与收件人一一对应。
//
// 删掉它会漏掉：失败的位置错位，日志指着一个没出问题的收件人说推送失败。
func TestEmitReportsPerRecipientFailure(t *testing.T) {
	boom := errors.New("写队列满")
	em := NewEmitter(func(userID int64, _ Envelope) error {
		if userID == 2 {
			return boom
		}
		return nil
	})

	errs := em.Emit([]Message{
		{UserID: 1, Env: Envelope{Type: CmdAuth}},
		{UserID: 2, Env: Envelope{Type: CmdAuth}},
		{UserID: 3, Env: Envelope{Type: CmdAuth}},
	})

	if len(errs) != 3 {
		t.Fatalf("错误切片长 %d，想要 3（与收件人一一对应）", len(errs))
	}
	if errs[0] != nil || errs[2] != nil {
		t.Errorf("没失败的位置不该有错: %v", errs)
	}
	if !errors.Is(errs[1], boom) {
		t.Errorf("第 2 个收件人的错误是 %v，想要 %v", errs[1], boom)
	}

	// 全部成功时不分配。
	if errs := em.Emit([]Message{{UserID: 1, Env: Envelope{Type: CmdPing}}}); errs != nil {
		t.Errorf("全部成功时返回了 %v，想要 nil", errs)
	}
}

// TestEmitKeepsSeqOrderWhileSending 是那把锁的存在理由。
//
// 删掉它（或把锁缩到只罩取号）会漏掉：两个发事件者并发时号序与到达序不一致，
// 而它平时跑一百遍都是绿的。
func TestEmitKeepsSeqOrderWhileSending(t *testing.T) {
	reached := make(chan int, 2)
	release := make(chan struct{})

	em := NewEmitter(func(_ int64, env Envelope) error {
		reached <- env.Seq
		if env.Seq == 1 {
			// 假装第一条的投递很慢（真实的慢来自对端不读）。
			<-release
		}
		return nil
	})

	go em.Emit([]Message{{UserID: 1, Env: Envelope{Type: CmdAuth}}})
	if seq := <-reached; seq != 1 {
		t.Fatalf("第一条拿到 seq=%d，想要 1", seq)
	}

	// 第一个还在临界区里，第二个 Emit 必须被挡在门外 —— 挡住了就说明
	// "取号 + 投递"是原子的。
	go em.Emit([]Message{{UserID: 1, Env: Envelope{Type: CmdAuth}}})
	select {
	case seq := <-reached:
		t.Fatalf("第一个还没发完，seq=%d 就已经在投递了（锁没罩住投递）", seq)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)
	if seq := <-reached; seq != 2 {
		t.Errorf("第二个拿到 seq=%d，想要 2", seq)
	}
}
