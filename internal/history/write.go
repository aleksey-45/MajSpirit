package history

// write.go —— 写路径。读路径在 history.go / types.go。
//
// 两半的时序刻意不同（设计 §3）：
//
//	BeginGame   **同步**、在发牌之前。承诺必须先于牌山生成，异步就没这个保证了。
//	FinishGame  **异步**、入队就返回。一场明细是几百行 INSERT，不能塞进对局的
//	            goroutine —— 那是纯 IO，没有并发要论证，只有延迟要躲。
//
// 失败语义也是不对称的，而且是有意的：开局写不进去就不开局（房主收到 error）；
// 结束时写不进去就**丢这一场历史**（重试几次，然后记一条日志）。玩家已经看到
// game.gameEnd 了，为了一局历史把队列堵死或者把内存占住，换来的是一张更贵的单。

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/aleksey-45/majspirit/internal/store"
)

const (
	// queueSize 是待写记录的上限。有界不是省内存，是拒绝"无界队列把 OOM 当成
	// 背压"那种写法：无界只是把"丢数据"推迟成"进程被杀"。
	queueSize = 64

	// maxAttempts 是同一场记录最多写几次。写不满就放弃 —— 见文件头。
	maxAttempts = 3

	// writeTimeout 是一次落库的上限。没有它，卡住的库会让 worker 永远不回来，
	// 后面的记录一直排队。
	writeTimeout = 10 * time.Second
)

// retryBackoff 是第一次重试前的等待，之后每次翻倍。
//
// 用 var 而不是 const：测试要把它压到几毫秒，否则"重试三次"那条用例要跑半秒多。
var retryBackoff = 200 * time.Millisecond

// BeginRequest 是开一场要落库的东西。
type BeginRequest struct {
	// Rules 是规则快照（mahjong.DefaultRules() 的 JSON），原样进 games.rules。
	Rules []byte
	// Seats 是四个座位的 userID（按 seat 0..3）。
	Seats [4]int64
	// InitialScore 是持ち点（本项目是 25000，由规则层给）。
	InitialScore int
	// Seed 为 nil 时用 crypto/rand 取。**对局必须用返回的那个种子洗牌** ——
	// 否则落库的种子和真正洗出这副牌山的种子不是一个，事后复核必然对不上。
	Seed *[32]byte
}

// BeginResult 是开局落库的结果，直接喂给对局。
type BeginResult struct {
	GameID int64
	Seed   [32]byte
}

// BeginGame 开一场：取种子、落库（含承诺），返回 gameID 与种子。
//
// 它**必须**在对局洗牌之前返回。调用方（房间）拿到结果才能开对局；写失败就不开局 ——
// 一个没有承诺的牌局，事后复核不出任何东西（牌山可验证性 §8）。
func (s *Service) BeginGame(ctx context.Context, in BeginRequest) (BeginResult, error) {
	seed := [32]byte{}
	if in.Seed != nil {
		seed = *in.Seed
	} else if _, err := rand.Read(seed[:]); err != nil {
		return BeginResult{}, fmt.Errorf("取服务器种子: %w", err)
	}

	id, err := s.repo.BeginGame(ctx, store.BeginParams{
		Rules:        in.Rules,
		ServerSeed:   seed,
		Seats:        in.Seats,
		InitialScore: in.InitialScore,
	})
	if err != nil {
		return BeginResult{}, fmt.Errorf("开局落库: %w", err)
	}
	return BeginResult{GameID: id, Seed: seed}, nil
}

// enqueue 把一场写完的记录交给 worker，返回是否收下。
//
// 队列满就丢，**不阻塞**：调用方是对局的 goroutine，卡在那里就是一张牌桌卡住。
// 丢的后果是这一场查不到历史 —— 记一条日志，让这件事在运维上看得到。
func (s *Service) enqueue(rec store.GameRecord) bool {
	select {
	case s.queue <- rec:
		return true
	default:
		s.log.Error("牌局记录队列已满，这一场不落库", "game", rec.GameID, "queue", queueSize)
		return false
	}
}

// run 是唯一碰队列的 goroutine：一条一条写，写完再取下一条。
//
// 串行是刻意的：并发写库换不来吞吐（一场几百行、本地毫秒级），却让"重试"和
// "顺序"都变成要论证的事。
func (s *Service) run() {
	for rec := range s.queue {
		s.writeOne(rec)
	}
}

// writeOne 写一场，失败就退避重试，重试用完就放弃。
//
// 每次重试都新开一个 context.Background 派生的 ctx：这个 goroutine 不属于任何一个
// HTTP 请求或对局，拿它们的 ctx 会在对方结束时把自己的重试一起取消掉。
func (s *Service) writeOne(rec store.GameRecord) {
	for attempt := 1; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		err := s.repo.FinishGame(ctx, rec)
		cancel()

		if err == nil {
			return
		}
		if attempt >= maxAttempts {
			s.log.Error("牌局落库失败，这一场不写了",
				"game", rec.GameID, "attempts", attempt, "err", err)
			return
		}
		// 幂等保证了重试是安全的：store.FinishGame 对已经写过的场次直接返回 nil，
		// 所以"提交成功但回执丢了"这种情况不会在重试时变成一条假错误。
		if errors.Is(err, store.ErrNoGame) {
			// 这场根本没开过（调用方的 bug），重试多少次都是一样的结果。
			s.log.Error("记录指向不存在的牌局，不重试", "game", rec.GameID)
			return
		}
		time.Sleep(retryBackoff << (attempt - 1))
	}
}
