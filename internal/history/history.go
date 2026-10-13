package history

// history.go —— 用例：取一页过往牌局、取一场存档。
//
// 响应体结构（以及数据库的行 → 响应体的翻译）在 types.go，写路径（开局落库与
// 结束时的异步落库）在 write.go。这里只有依赖、分页规则和两个读用例。
//
// 响应体与协议 §3.5 / §3.6 对齐；与 §3.6 的一处差异（每小局多一个 wall 对象）
// 写在 docs/local/阶段五-落库设计.md §4.2。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aleksey-45/majspirit/internal/store"
)

// pageSize 是一页多少场。定值、不做 ?limit=：协议 §3.5 只定义了 cursor，
// 多一个可调参数就多一组边界（0 怎么办、超了怎么办），而现在的收益是零。
const pageSize = 20

// ErrNoGame 表示这个 gameId 不存在。api 靠它回 404。
//
// 翻译自 store.ErrNoGame：api 只认这一层的词汇，不许 import store ——
// 与 user.ErrUsernameTaken 翻自 store.ErrUsernameTaken 是同一条规矩。
var ErrNoGame = errors.New("牌局不存在")

// Repo 是历史牌局用到的全部数据访问：两个查询 + 两个事务。
//
// 接口定义在消费方（这里），*store.Pool 用结构化类型满足它 —— 它不需要知道
// Repo 存在。与 user.Repo 同一条规矩。
//
// 读和写放同一个接口，是因为一个 *store.Pool 就同时满足两边；拆成两个接口只会让
// New 多收一个参数，而调用方从来不会只想要其中一半。
//
// 查询返回 store 的行类型而不是在这里另立一套：多一层字段一样的结构只是把字段抄一遍，
// 换不来任何可测性。真正的翻译在 types.go 里（数据库的行 → 响应体）。
type Repo interface {
	GameHistory(ctx context.Context, userID, cursor int64, limit int) ([]store.GameSummary, error)
	GameArchive(ctx context.Context, gameID int64) (store.GameArchive, error)
	BeginGame(ctx context.Context, in store.BeginParams) (int64, error)
	FinishGame(ctx context.Context, rec store.GameRecord) error
}

// Service 是历史牌局的用例入口。
//
// 它带一个写 worker（write.go）：记录入队即返回，写库在另一个 goroutine 里。
type Service struct {
	repo Repo
	log  *slog.Logger

	// queue 只由 enqueue 投、由 run 取。有界的原因见 write.go 的文件头。
	queue chan store.GameRecord
}

// New 造一个用例服务并起它的写 worker。repo 一般是 *store.Pool。
func New(repo Repo) *Service {
	s := &Service{
		repo:  repo,
		log:   slog.Default(),
		queue: make(chan store.GameRecord, queueSize),
	}
	go s.run()
	return s
}

// History 取某位玩家的一页过往牌局，按时间倒序（= games.id 倒序）。
//
// cursor 为 0 表示第一页；非 0 时取 id 比它小的那些。详见 store.GameHistory。
func (s *Service) History(ctx context.Context, userID, cursor int64) (Page, error) {
	// 多查一条：第 pageSize+1 条只用来回答"还有没有下一页"，不返回给调用方。
	// 比再发一条 COUNT(*) 便宜，而且不会因为两次查询之间新增牌局而自相矛盾。
	rows, err := s.repo.GameHistory(ctx, userID, cursor, pageSize+1)
	if err != nil {
		return Page{}, fmt.Errorf("读取过往牌局: %w", err)
	}

	page := Page{Games: make([]Summary, 0, pageSize)}
	if len(rows) > pageSize {
		rows = rows[:pageSize]
		last := rows[len(rows)-1].GameID
		page.NextCursor = &last
	}
	for _, r := range rows {
		page.Games = append(page.Games, Summary{
			GameID:       r.GameID,
			StartedAt:    r.StartedAt.UnixMilli(),
			FinishedAt:   msPtr(r.FinishedAt),
			Seat:         r.Seat,
			Placement:    r.Placement,
			InitialScore: r.InitialScore,
			FinalScore:   r.FinalScore,
			Points:       r.Points,
		})
	}
	return page, nil
}

// Game 取一场牌局的存档。
//
// 进行中的对局也能查：库里只有 games + game_seats 两行，于是 Rounds 为空。
// 协议 §3.6 那条"进行中不许下发别家手牌"因此没有可判断的分支 —— 它由写库时机
// 保证，不由这里的 if 保证（设计 §3.3）。
func (s *Service) Game(ctx context.Context, gameID int64) (Game, error) {
	arch, err := s.repo.GameArchive(ctx, gameID)
	if err != nil {
		if errors.Is(err, store.ErrNoGame) {
			return Game{}, ErrNoGame
		}
		return Game{}, fmt.Errorf("读取牌局存档: %w", err)
	}

	g := Game{
		GameID:     arch.GameID,
		StartedAt:  arch.StartedAt.UnixMilli(),
		FinishedAt: msPtr(arch.FinishedAt),
		Rules:      json.RawMessage(arch.Rules),
		Seats:      make([]Seat, 0, len(arch.Seats)),
		Rounds:     make([]Round, 0, len(arch.Rounds)),
	}
	for _, s := range arch.Seats {
		g.Seats = append(g.Seats, Seat{
			Seat:         s.Seat,
			UserID:       s.UserID,
			Username:     s.Username,
			InitialScore: s.InitialScore,
			FinalScore:   s.FinalScore,
			Placement:    s.Placement,
			Points:       s.Points,
		})
	}
	for _, r := range arch.Rounds {
		g.Rounds = append(g.Rounds, Round{
			RoundIndex:   r.RoundIndex,
			RoundWind:    r.RoundWind,
			HandNumber:   r.HandNumber,
			DealerSeat:   r.DealerSeat,
			EndedBy:      r.EndedBy,
			WinnerSeat:   r.WinnerSeat,
			IsTsumo:      r.IsTsumo,
			WinningTile:  r.WinningTile,
			RiichiSticks: r.RiichiSticks,
			InitialHands: r.InitialHands,
			Wall:         wall(r.Wall),
			Events:       events(r.Events),
			Wins:         wins(r.Wins),
		})
	}
	return g, nil
}
