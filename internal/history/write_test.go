package history

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/store"
)

// write_test.go —— 写路径：种子、异步入队、重试与放弃。
//
// 假的库能按剧本"卡住"或"失败"，所以这里验的是时序与失败语义，不是 SQL。

// shrinkBackoff 把重试等待压到毫秒级，测试结束恢复。
func shrinkBackoff(t *testing.T) {
	t.Helper()

	old := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = old })
}

func rec(gameID int64) store.GameRecord { return store.GameRecord{GameID: gameID} }

// 开一场：返回给对局的种子必须**就是**落库的那个。
//
// 删掉它会漏掉：对局用一个种子洗牌、库里存另一个 —— 事后谁按库里的种子重放都对不上，
// 而这是整套可验证性的唯一支点。
func TestBeginGamePersistsTheSeedItReturns(t *testing.T) {
	repo := &fakeRepo{beginID: 7}
	res, err := New(repo).BeginGame(context.Background(), BeginRequest{
		Rules:        []byte(`{"kiriageMangan":true}`),
		Seats:        [4]int64{11, 12, 13, 14},
		InitialScore: 25000,
	})
	if err != nil {
		t.Fatalf("BeginGame: %v", err)
	}
	if res.GameID != 7 {
		t.Errorf("gameID = %d，期望 7（要用数据库给的那个）", res.GameID)
	}
	if res.Seed == ([32]byte{}) {
		t.Error("没取种子：全零的种子洗出来的牌山每一场都一样")
	}
	if repo.beginParams.ServerSeed != res.Seed {
		t.Errorf("落库的种子 %x 与返回给对局的 %x 不是同一个", repo.beginParams.ServerSeed, res.Seed)
	}
	if repo.beginParams.Seats != [4]int64{11, 12, 13, 14} {
		t.Errorf("座位没传对: %v", repo.beginParams.Seats)
	}
	if repo.beginParams.InitialScore != 25000 {
		t.Errorf("初始分 = %d，期望 25000", repo.beginParams.InitialScore)
	}
}

// 调用方给了种子就用给的 —— 测试摆牌与复现要靠它。
func TestBeginGameUsesGivenSeed(t *testing.T) {
	repo := &fakeRepo{}
	seed := [32]byte{7}

	res, err := New(repo).BeginGame(context.Background(), BeginRequest{Seed: &seed})
	if err != nil {
		t.Fatalf("BeginGame: %v", err)
	}
	if res.Seed != seed || repo.beginParams.ServerSeed != seed {
		t.Errorf("种子没原样用: %x / %x", res.Seed, repo.beginParams.ServerSeed)
	}
}

// 库写不进去：错误必须抛上去，让房间**不要开局**。
//
// 删掉它会漏掉：开局失败还照发牌 —— 一场没有承诺的牌局。
func TestBeginGamePropagatesStoreError(t *testing.T) {
	boom := errors.New("库不可用")

	_, err := New(&fakeRepo{beginErr: boom}).BeginGame(context.Background(), BeginRequest{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v，期望包着 %v", err, boom)
	}
}

// 队列满时 enqueue 必须**立刻**返回 false，不许等。
//
// 删掉它会漏掉：对局的 goroutine 卡在写库队列上 —— 那是一张牌桌卡住，
// 比丢一局历史贵得多。
func TestEnqueueDropsInsteadOfBlocking(t *testing.T) {
	release := make(chan struct{})
	repo := &fakeRepo{
		finishBlock:  release,
		finishEnter:  make(chan struct{}),
		finishSignal: make(chan struct{}, queueSize+8),
	}
	s := New(repo)

	// 第一条被 worker 取走、卡在"库里"。
	if !s.enqueue(rec(1)) {
		t.Fatal("空队列居然丢了第一条")
	}
	<-repo.finishEnter

	// 把队列填满。
	for i := 0; i < queueSize; i++ {
		if !s.enqueue(rec(int64(i + 2))) {
			t.Fatalf("填到第 %d 条就被丢了，队列不该这么小", i)
		}
	}

	// 再来一条：必须立刻被丢，而不是阻塞。
	done := make(chan bool, 1)
	go func() { done <- s.enqueue(rec(9999)) }()
	select {
	case ok := <-done:
		if ok {
			t.Error("队列满了还收下了")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("enqueue 在队列满时阻塞了")
	}
	close(release)
}

// 重试几次就放弃，而且**不耽误下一条**。
//
// 删掉它会漏掉两种坏法：一直重试（worker 永远卡在一场坏记录上，后面的全都不写），
// 或者一次失败就退出（worker 死掉，之后再也写不进历史）。
func TestWriteWorkerGivesUpAndKeepsGoing(t *testing.T) {
	shrinkBackoff(t)
	repo := &fakeRepo{
		failAlways:   true,
		finishSignal: make(chan struct{}, 2*maxAttempts),
	}
	s := New(repo)

	s.enqueue(rec(1))
	s.enqueue(rec(2))

	// 两场各试 maxAttempts 次；收满这些信号说明它既没少试也没多试。
	for i := 0; i < 2*maxAttempts; i++ {
		select {
		case <-repo.finishSignal:
		case <-time.After(3 * time.Second):
			t.Fatalf("只收到 %d 次写尝试，期望 %d 次", i, 2*maxAttempts)
		}
	}

	// 再多等一会儿：真要是在死循环重试，这里会多出信号。
	time.Sleep(50 * time.Millisecond)
	repo.mu.Lock()
	calls := repo.finishCalls
	repo.mu.Unlock()
	if calls != 2*maxAttempts {
		t.Errorf("一共试了 %d 次，期望 %d 次（每场 %d 次就放弃）", calls, 2*maxAttempts, maxAttempts)
	}
}

// 失败两次之后成功：记录必须真的落进去 —— 重试是有用的，不是走个过场。
func TestWriteWorkerRetriesThenSucceeds(t *testing.T) {
	shrinkBackoff(t)
	repo := &fakeRepo{
		finishErrs:   []error{errors.New("超时"), errors.New("超时")},
		finishSignal: make(chan struct{}, maxAttempts),
	}
	s := New(repo)

	s.enqueue(rec(7))
	for i := 0; i < 3; i++ {
		select {
		case <-repo.finishSignal:
		case <-time.After(3 * time.Second):
			t.Fatalf("只收到 %d 次写尝试", i)
		}
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.finished) != 1 || repo.finished[0].GameID != 7 {
		t.Fatalf("重试之后没写进去: %+v", repo.finished)
	}
}

// 记录指向一场没开过的牌局：立刻放弃，不重试（重试多少次结果都一样）。
func TestWriteWorkerDoesNotRetryMissingGame(t *testing.T) {
	shrinkBackoff(t)
	repo := &fakeRepo{
		finishErrs:   []error{store.ErrNoGame, store.ErrNoGame, store.ErrNoGame},
		finishSignal: make(chan struct{}, maxAttempts),
	}
	s := New(repo)

	s.enqueue(rec(404))
	select {
	case <-repo.finishSignal:
	case <-time.After(3 * time.Second):
		t.Fatal("没等到写尝试")
	}
	time.Sleep(50 * time.Millisecond)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.finishCalls != 1 {
		t.Errorf("试了 %d 次，期望 1 次（牌局不存在，重试没有意义）", repo.finishCalls)
	}
}
