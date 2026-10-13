package history

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/store"
)

// history_test.go —— 用例层。用假 Repo：不碰数据库、不启动服务。
//
// 假 Repo 按 SQL 的语义翻页（只返回 id 小于游标的、最多 limit 条），所以断言的
// 是**行为**（一页几条、有没有下一页），不是"我调了几次、传了几个"。
//
// 写侧那几个字段（beginParams / finishErrs / finishBlock）是给 write_test.go 用的：
// 那里要验的是"队列满了会不会阻塞"和"重试几次就放弃"，都得能让假库按剧本卡住或失败。

const startedMs int64 = 1_700_000_000_000

type fakeRepo struct {
	// ↓ 读
	rows      []store.GameSummary // 已按 game_id 倒序，和 SQL 的 ORDER BY 一样
	archive   store.GameArchive
	err       error
	lastLimit int

	// ↓ 写。mu 保护写侧那几个字段：worker 在另一个 goroutine 里改它们。
	mu           sync.Mutex
	beginParams  store.BeginParams
	beginID      int64
	beginErr     error
	finishErrs   []error // 依次返回的错误；用完就成功
	failAlways   bool
	finished     []store.GameRecord
	finishCalls  int
	finishBlock  chan struct{} // 非 nil 时每次 FinishGame 先等它（用来压满队列）
	finishEnter  chan struct{} // 每次进入 FinishGame 前发一个（用来等"worker 真的进去了"）
	finishSignal chan struct{} // 每次 FinishGame 返回后发一个
}

func (f *fakeRepo) GameHistory(_ context.Context, _ int64, cursor int64, limit int) ([]store.GameSummary, error) {
	f.lastLimit = limit
	if f.err != nil {
		return nil, f.err
	}
	out := []store.GameSummary{}
	for _, s := range f.rows {
		if cursor != 0 && s.GameID >= cursor {
			continue
		}
		out = append(out, s)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (f *fakeRepo) GameArchive(_ context.Context, _ int64) (store.GameArchive, error) {
	if f.err != nil {
		return store.GameArchive{}, f.err
	}
	return f.archive, nil
}

func (f *fakeRepo) BeginGame(_ context.Context, in store.BeginParams) (int64, error) {
	f.mu.Lock()
	f.beginParams = in
	f.mu.Unlock()

	if f.beginErr != nil {
		return 0, f.beginErr
	}
	if f.beginID == 0 {
		return 42, nil
	}
	return f.beginID, nil
}

func (f *fakeRepo) FinishGame(ctx context.Context, rec store.GameRecord) error {
	if f.finishEnter != nil {
		f.finishEnter <- struct{}{}
	}
	if f.finishBlock != nil {
		select {
		case <-f.finishBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	f.mu.Lock()
	f.finishCalls++
	var err error
	switch {
	case f.failAlways:
		err = errors.New("库不可用")
	case len(f.finishErrs) > 0:
		err = f.finishErrs[0]
		f.finishErrs = f.finishErrs[1:]
	}
	if err == nil {
		f.finished = append(f.finished, rec)
	}
	f.mu.Unlock()

	if f.finishSignal != nil {
		f.finishSignal <- struct{}{}
	}
	return err
}

// rows 造 n 场牌局，game_id 从 n 递减到 1（倒序，和真实查询一致）。
func rows(n int) []store.GameSummary {
	out := make([]store.GameSummary, 0, n)
	for id := int64(n); id >= 1; id-- {
		at := time.UnixMilli(startedMs)
		out = append(out, store.GameSummary{
			GameID: id, StartedAt: at, FinishedAt: &at,
			Seat: 2, Placement: intPtr(1),
			InitialScore: 25000, FinalScore: 41200, Points: 61200,
		})
	}
	return out
}

func intPtr(n int) *int { return &n }

// 一页恒 20 条；有第 21 条时 nextCursor 指向**本页最后一条**的 id；没有下一页时是 nil。
//
// 三条断言各对应一处可错的实现：
//   - 条数：多查一条之后忘了裁剪（回 21 条）
//   - 游标位置：指向第 21 条而不是第 20 条（下一页会跳掉一条）
//   - 边界：恰好 20 条时也说"还有更多"（前端永远翻不到头）
//
// 删掉它会漏掉：整页翻页全错，而且错得很安静 —— 列表看起来是满的。
func TestHistoryPageSizeAndCursor(t *testing.T) {
	ctx := context.Background()

	// 21 场：第一页 20 条 + 还有下一页。
	repo := &fakeRepo{rows: rows(21)}
	page, err := New(repo).History(ctx, 1, 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(page.Games) != 20 {
		t.Fatalf("第一页 %d 条，期望 20", len(page.Games))
	}
	// 游标要指向**本页最后一条**（21..2 里的 2），不是第 21 条（1）：
	// 指错了下一页就会跳掉一条，而列表看起来还是满的。
	last := page.Games[len(page.Games)-1].GameID
	if page.NextCursor == nil || *page.NextCursor != last {
		t.Fatalf("nextCursor = %v，期望 %d（本页最后一条的 gameId）", page.NextCursor, last)
	}

	// 用游标取下一页：只剩 1 条，且不能再有下一页。
	next, err := New(repo).History(ctx, 1, *page.NextCursor)
	if err != nil {
		t.Fatalf("第二页: %v", err)
	}
	if len(next.Games) != 1 || next.Games[0].GameID != 1 {
		t.Fatalf("第二页 = %+v，期望只剩 game_id 1", next.Games)
	}
	if next.NextCursor != nil {
		t.Errorf("最后一页还有 nextCursor = %v", *next.NextCursor)
	}

	// 恰好 20 场：一页装得下，nextCursor 必须是 nil。
	exact := &fakeRepo{rows: rows(20)}
	page, err = New(exact).History(ctx, 1, 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(page.Games) != 20 {
		t.Fatalf("恰好 20 场时回了 %d 条", len(page.Games))
	}
	if page.NextCursor != nil {
		t.Errorf("恰好装下时不该有下一页，得到 %v", *page.NextCursor)
	}
}

// 一场都没有：Games 必须是空切片而不是 nil，序列化出来是 []。
//
// 删掉它会漏掉：前端拿到 `"games":null`，于是"没有历史"那条分支走不到
// （直接在渲染里抛异常），而服务端日志里什么都没有。
func TestHistoryEmptyPageIsArrayNotNull(t *testing.T) {
	page, err := New(&fakeRepo{}).History(context.Background(), 1, 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if page.Games == nil {
		t.Fatal("空列表是 nil 切片，序列化会变成 null")
	}

	b, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("编码: %v", err)
	}
	if !strings.Contains(string(b), `"games":[]`) {
		t.Errorf("空列表编码成了 %s", b)
	}
	if !strings.Contains(string(b), `"nextCursor":null`) {
		t.Errorf("没有下一页时应该是 null: %s", b)
	}
}

// 响应体：时间是毫秒，points 原样（不除 1000），进行中的牌局两个字段是 null。
//
// 删掉它会漏掉：时间变成 RFC3339 字符串（前端按毫秒硬算就全错）、或者有人
// "顺手"把 points 除以 1000（单位从点变成 pt，数值小一千倍）。
func TestSummaryWireShape(t *testing.T) {
	repo := &fakeRepo{rows: []store.GameSummary{{
		GameID: 7, StartedAt: time.UnixMilli(startedMs),
		Seat: 3, InitialScore: 25000, FinalScore: 25000, Points: 61200,
	}}}
	page, err := New(repo).History(context.Background(), 1, 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	got := page.Games[0]
	if got.StartedAt != startedMs {
		t.Errorf("startedAt = %d，期望毫秒 %d", got.StartedAt, startedMs)
	}
	if got.FinishedAt != nil {
		t.Errorf("进行中的牌局 finishedAt = %d，期望 null", *got.FinishedAt)
	}
	if got.Placement != nil {
		t.Errorf("进行中的牌局 placement = %d，期望 null", *got.Placement)
	}
	if got.Points != 61200 {
		t.Errorf("points = %d，期望 61200（单位是点，不做换算）", got.Points)
	}
}

// 单局详情：字节转 hex、NULL 位置保持 NULL、空的动作与算分是 []、小局带牌山。
//
// 删掉它会漏掉：clientSeeds 的 NULL 被填成空串（验牌脚本按下标取就错位）、
// 或者 hex 编码丢了（字节被 Go 编成 base64，玩家没法直接比对）。
func TestGameWireShape(t *testing.T) {
	at := time.UnixMilli(startedMs + 1000)
	repo := &fakeRepo{archive: store.GameArchive{
		GameID: 7, StartedAt: time.UnixMilli(startedMs), FinishedAt: &at,
		Rules: []byte(`{"kiriageMangan":true}`),
		Seats: []store.ArchiveSeat{{Seat: 0, UserID: 42, Username: "aleksey", InitialScore: 25000}},
		Rounds: []store.ArchiveRound{{
			RoundIndex: 1, RoundWind: "1z", EndedBy: "draw", RiichiSticks: 1,
			InitialHands: []string{"123m", "456m", "789m", "111z"},
			Wall: &store.ArchiveWall{
				Tiles: "1m5p", WallHash: []byte{0x10, 0xb5},
				ServerSeed: []byte{0x6f, 0x1a}, Commitment: []byte{0xa3, 0xc9},
				ClientSeeds:    [][]byte{{0x01}, nil, {0x03}, {0x04}},
				GoVersion:      "go1.27.0",
				DoraIndicators: []string{"3s"}, UraIndicators: []string{"7m"},
			},
		}},
	}}

	g, err := New(repo).Game(context.Background(), 7)
	if err != nil {
		t.Fatalf("Game: %v", err)
	}

	if g.StartedAt != startedMs || g.FinishedAt == nil || *g.FinishedAt != startedMs+1000 {
		t.Errorf("时间是 %d / %v，期望 %d / %d", g.StartedAt, g.FinishedAt, startedMs, startedMs+1000)
	}
	if string(g.Rules) != `{"kiriageMangan":true}` {
		t.Errorf("rules 没原样透出: %s", g.Rules)
	}
	if len(g.Seats) != 1 || g.Seats[0].Username != "aleksey" || g.Seats[0].UserID != 42 {
		t.Errorf("座位不对: %+v", g.Seats)
	}

	if len(g.Rounds) != 1 {
		t.Fatalf("小局 %d 个，期望 1", len(g.Rounds))
	}
	r := g.Rounds[0]
	if len(r.InitialHands) != 4 || r.InitialHands[2] != "789m" {
		t.Errorf("初始手牌错位: %v", r.InitialHands)
	}
	if r.Wall == nil {
		t.Fatal("没有牌山")
	}
	if r.Wall.WallHash != "10b5" || r.Wall.ServerSeed != "6f1a" || r.Wall.Commitment != "a3c9" {
		t.Errorf("字节没转成 hex: %+v", r.Wall)
	}
	if len(r.Wall.ClientSeeds) != 4 {
		t.Fatalf("clientSeeds %d 项，期望恒 4 项", len(r.Wall.ClientSeeds))
	}
	if r.Wall.ClientSeeds[0] == nil || *r.Wall.ClientSeeds[0] != "01" {
		t.Errorf("clientSeeds[0] = %v，期望 \"01\"", r.Wall.ClientSeeds[0])
	}
	if r.Wall.ClientSeeds[1] != nil {
		t.Errorf("clientSeeds[1] = %v，期望 null", *r.Wall.ClientSeeds[1])
	}
	if r.Wall.GoVersion != "go1.27.0" {
		t.Errorf("goVersion = %q", r.Wall.GoVersion)
	}

	// 没有动作、没有算分：必须是 []，不是 null。
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("编码: %v", err)
	}
	if !strings.Contains(string(b), `"events":[]`) {
		t.Errorf("空动作序列编码成了 null: %s", b)
	}
	if !strings.Contains(string(b), `"wins":[]`) {
		t.Errorf("空算分编码成了 null: %s", b)
	}

	// 进行中的牌局：没有小局，但 seats 要照给。
	live := &fakeRepo{archive: store.GameArchive{
		GameID: 8, StartedAt: time.UnixMilli(startedMs),
		Seats: []store.ArchiveSeat{{Seat: 0, UserID: 1, Username: "a"}},
	}}
	lg, err := New(live).Game(context.Background(), 8)
	if err != nil {
		t.Fatalf("Game（进行中）: %v", err)
	}
	if lg.FinishedAt != nil {
		t.Error("进行中的牌局 finishedAt 应该是 null")
	}
	if lg.Rounds == nil || len(lg.Rounds) != 0 {
		t.Errorf("进行中的牌局 rounds 应该是空数组，得到 %v", lg.Rounds)
	}
}

// 库里没有这场牌局 → 翻译成这一层的 ErrNoGame，而且**不**把 store.ErrNoGame
// 直接透出去（api 认的是 history 的词汇）。
//
// 删掉它会漏掉：api 里那句 errors.Is(err, history.ErrNoGame) 永远不成立，
// 于是"牌局不存在"变成 500 —— 一个本该是 404 的响应。
func TestGameMapsStoreNoGame(t *testing.T) {
	_, err := New(&fakeRepo{err: store.ErrNoGame}).Game(context.Background(), 1)
	if !errors.Is(err, ErrNoGame) {
		t.Fatalf("err = %v，期望 history.ErrNoGame", err)
	}
	if errors.Is(err, store.ErrNoGame) {
		t.Error("原始错误不该留在链上 —— 那会把数据层的词汇漏到 api")
	}

	// 其它错误照旧包着往上抛，不能被误判成"不存在"。
	boom := errors.New("连接超时")
	_, err = New(&fakeRepo{err: boom}).Game(context.Background(), 1)
	if !errors.Is(err, boom) || errors.Is(err, ErrNoGame) {
		t.Errorf("库挂了被当成了别的: %v", err)
	}
}
