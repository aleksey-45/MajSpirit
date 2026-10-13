package store

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aleksey-45/majspirit/internal/mahjong"
)

// game_record_test.go —— 牌局历史的写。
//
// 核心是同一条往返：**用 BeginGame/FinishGame 写进去，再用 GameArchive/GameHistory
// 读回来**，逐字段对上。读写是两段独立的 SQL，列顺序、类型、NULL 语义任何一处对不上，
// 只有在真库上走一遍才发现 —— 分开测各自的行是抓不到的。
//
// 打真 PostgreSQL（会 DROP SCHEMA），与其余 store 用例同规矩。

func intp(n int) *int       { return &n }
func strp(s string) *string { return &s }
func boolp(b bool) *bool    { return &b }

// beginTestGame 开一场，返回 gameID、四个座位与这一场的服务器种子。
func beginTestGame(t *testing.T, p *Pool) (int64, [4]int64, [32]byte) {
	t.Helper()

	seats := [4]int64{
		mkUser(t, p, "p0"), mkUser(t, p, "p1"), mkUser(t, p, "p2"), mkUser(t, p, "p3"),
	}
	seed := [32]byte{0x6f, 0x1a}
	id, err := p.BeginGame(context.Background(), BeginParams{
		Rules:        []byte(`{"kiriageMangan":true,"akaDora":1}`),
		ServerSeed:   seed,
		Seats:        seats,
		InitialScore: 25000,
	})
	if err != nil {
		t.Fatalf("BeginGame: %v", err)
	}
	return id, seats, seed
}

// fullRecord 造一份"像真打完的一场"的记录：两小局，第一局荣和带帕奥，第二局荒牌流局。
func fullRecord(gameID int64, commitment []byte) GameRecord {
	tiles := mahjong.NewWallFromSeed([32]byte{9}).Sequence()
	return GameRecord{
		GameID: gameID,
		Seats: [4]SeatResult{
			{FinalScore: 41200, Placement: 1, Points: 61200},
			{FinalScore: 18900, Placement: 3, Points: -14100},
			{FinalScore: 25100, Placement: 2, Points: -15900},
			{FinalScore: 14800, Placement: 4, Points: -31200},
		},
		Rounds: []RoundRecord{{
			RoundIndex: 1, RoundWind: "1z", HandNumber: 0, DealerSeat: 0,
			EndedBy: "win", WinnerSeat: intp(2), IsTsumo: boolp(false), WinningTile: strp("5p"),
			RiichiSticks: 1,
			// 四家各不相同，好让"按 seat 对齐"可被证伪。
			InitialHands: [4]string{"123m456s789p11z", "11122m333s", "456p789p11122z", "19m19p19s1234567z"},
			Wall: WallRecord{
				Tiles: tiles, WallHash: []byte{0x10, 0xb5},
				ServerSeed: []byte{0x6f, 0x1a}, Commitment: commitment,
				ClientSeeds: [][]byte{{0x01}, nil, {0x03}, {0x04}},
				GoVersion:   "go1.27.0",
				// 指示牌是列表：杠一次追加一张，可能有重复，不能去重。
				DoraIndicators: []string{"3s", "3s"}, UraIndicators: []string{"7m"},
			},
			Events: []EventRecord{
				{Seq: 1, Seat: 0, Action: "draw", Tile: strp("9m"), Detail: []byte(`{"tsumogiri":false}`)},
				{Seq: 2, Seat: 0, Action: "discard", Tile: strp("9m"), Detail: []byte(`{"tsumogiri":true}`)},
				// 立直这条动作本身不带牌。
				{Seq: 3, Seat: 2, Action: "riichi", Tile: nil, Detail: []byte(`{}`)},
				{Seq: 4, Seat: 2, Action: "ron", Tile: strp("5p"), Detail: []byte(`{}`)},
			},
			Wins: []WinRecord{{
				WinSeq: 1, WinnerSeat: 2, LoserSeat: intp(0), IsTsumo: false, WinningTile: "5p",
				Han: 13, Fu: 40, Yakuman: 0,
				Yaku:     []byte(`[{"name":"立直","han":1}]`),
				Dora:     []byte(`{"dora":1,"ura":1,"aka":0}`),
				Payments: []byte(`{"0":-8000,"1":-40300,"2":48300,"3":0}`),
				PaoSeat:  intp(1), PaoYaku: strp("大三元"),
			}},
		}, {
			RoundIndex: 2, RoundWind: "1z", HandNumber: 1, DealerSeat: 2,
			EndedBy: "draw", RiichiSticks: 0,
			InitialHands: [4]string{"19m19p19s1234567z", "123m456s789p11z", "11122m333s", "456p789p11122z"},
			Wall: WallRecord{
				Tiles: tiles, WallHash: []byte{0x10, 0xb5}, ServerSeed: []byte{0x6f, 0x1a},
				Commitment: commitment, GoVersion: "go1.27.0",
				DoraIndicators: []string{"3s"}, UraIndicators: []string{},
			},
			// 流局：没有动作、没有算分（切片为 nil，落库时一行都不写）。
		}},
	}
}

// 写进去、读回来，逐字段对上。
//
// 这条同时钉住三件事，删掉任何一件都会漏掉一类缺陷：
//   - 列的顺序/类型：INSERT 与 SELECT 是两段独立的 SQL，错位只有往返才看得出
//   - NULL 语义：流局的 winner_seat / is_tsumo，以及 client_seeds 中间那一项
//   - 座位结果回填：终局分数、顺位、ポイント写在 update 里，不在插入里
func TestGameRecordRoundTrip(t *testing.T) {
	p := gamePool(t)
	ctx := context.Background()

	gameID, seats, seed := beginTestGame(t, p)

	// 承诺必须在**开局那一刻**就落库 —— 这是整套可验证性的支点。
	var commitment []byte
	var committedAt *string
	if err := p.pool.QueryRow(ctx,
		`SELECT commitment, committed_at::text FROM games WHERE id = $1`, gameID).
		Scan(&commitment, &committedAt); err != nil {
		t.Fatalf("读回承诺: %v", err)
	}
	// 期望值独立算一遍，不跟实现比对实现：承诺必须**绑 gameID**，
	// 所以换一个 gameID 算出来的值必须不同（下面那半条）。
	want := mahjong.Commit(gameID, seed)
	if !bytes.Equal(commitment, want[:]) {
		t.Errorf("承诺不对：库里 %x，按 Commit(gameID, seed) 算是 %x", commitment, want)
	}
	if other := mahjong.Commit(gameID+1, seed); bytes.Equal(commitment, other[:]) {
		t.Error("承诺没绑 gameID —— 换个 id 算出同一个值")
	}
	if committedAt == nil {
		t.Error("committed_at 是 NULL —— 承诺就没有「先于牌局」这个时间点了")
	}

	if err := p.FinishGame(ctx, fullRecord(gameID, commitment)); err != nil {
		t.Fatalf("FinishGame: %v", err)
	}

	arch, err := p.GameArchive(ctx, gameID)
	if err != nil {
		t.Fatalf("GameArchive: %v", err)
	}
	if arch.FinishedAt == nil {
		t.Error("finished_at 没回填")
	}
	if !strings.Contains(string(arch.Rules), "kiriageMangan") {
		t.Errorf("rules 没存对: %s", arch.Rules)
	}

	if len(arch.Seats) != 4 {
		t.Fatalf("座位 %d 个，期望 4", len(arch.Seats))
	}
	// 终局结果来自 FinishGame 里的 UPDATE，不是插入时的默认值。
	if arch.Seats[0].FinalScore != 41200 || arch.Seats[0].Points != 61200 {
		t.Errorf("0 号座结果不对: %+v", arch.Seats[0])
	}
	if arch.Seats[0].Placement == nil || *arch.Seats[0].Placement != 1 {
		t.Errorf("0 号座顺位不对: %v", arch.Seats[0].Placement)
	}
	if arch.Seats[0].InitialScore != 25000 {
		t.Errorf("初始分 = %d，期望 25000", arch.Seats[0].InitialScore)
	}
	if arch.Seats[0].UserID != seats[0] {
		t.Errorf("0 号座的 user_id = %d，期望 %d", arch.Seats[0].UserID, seats[0])
	}

	if len(arch.Rounds) != 2 {
		t.Fatalf("小局 %d 个，期望 2", len(arch.Rounds))
	}
	r1, r2 := arch.Rounds[0], arch.Rounds[1]

	if r1.EndedBy != "win" || r1.WinnerSeat == nil || *r1.WinnerSeat != 2 {
		t.Errorf("第 1 小局的结末方式/和牌者不对: %s / %v", r1.EndedBy, r1.WinnerSeat)
	}
	if r2.EndedBy != "draw" || r2.WinnerSeat != nil || r2.IsTsumo != nil || r2.WinningTile != nil {
		t.Errorf("荒牌流局那三列应该是 NULL: %v %v %v", r2.WinnerSeat, r2.IsTsumo, r2.WinningTile)
	}
	if r1.HandNumber != 0 || r2.HandNumber != 1 || r2.DealerSeat != 2 {
		t.Errorf("本场/庄家不对: %d %d %d", r1.HandNumber, r2.HandNumber, r2.DealerSeat)
	}

	// 初始手牌按 seat 对齐。
	if got := r1.InitialHands[2]; got != "456p789p11122z" {
		t.Errorf("2 号座的初始手牌 = %q（错位了？）", got)
	}
	if got := r2.InitialHands[0]; got != "19m19p19s1234567z" {
		t.Errorf("第 2 小局 0 号座的手牌 = %q", got)
	}

	// 牌山：指纹是字节，指示牌是列表（两张 3s 不许去重）。
	if r1.Wall == nil || string(r1.Wall.WallHash) != "\x10\xb5" {
		t.Fatalf("牌山指纹没存回: %+v", r1.Wall)
	}
	if len(r1.Wall.DoraIndicators) != 2 {
		t.Errorf("宝牌指示牌 = %v，期望两张（杠了追加，不许去重）", r1.Wall.DoraIndicators)
	}
	if len(r1.Wall.ClientSeeds) != 4 || r1.Wall.ClientSeeds[0] == nil || r1.Wall.ClientSeeds[1] != nil {
		t.Error("client_seeds 的 NULL 位置不对：第 1 位有值、第 2 位是 NULL")
	}
	if r1.Wall.Tiles != mahjong.NewWallFromSeed([32]byte{9}).Sequence() {
		t.Error("牌山文本没原样存回")
	}

	// 动作按 seq 有序，不带牌的那条是 NULL。
	if len(r1.Events) != 4 {
		t.Fatalf("第 1 小局有 %d 条动作，期望 4", len(r1.Events))
	}
	for i, e := range r1.Events {
		if e.Seq != i+1 {
			t.Errorf("第 %d 条动作的 seq = %d", i, e.Seq)
		}
	}
	if r1.Events[2].Tile != nil {
		t.Error("立直那条动作不该带牌")
	}
	if !strings.Contains(string(r1.Events[1].Detail), "tsumogiri") {
		t.Errorf("动作的 detail 没原样存回: %s", r1.Events[1].Detail)
	}
	if len(r2.Events) != 0 {
		t.Errorf("第 2 小局没有动作，却查出 %d 条", len(r2.Events))
	}

	// 算分：番符、帕奥责任者、payments 原样。
	if len(r1.Wins) != 1 {
		t.Fatalf("第 1 小局有 %d 条算分，期望 1", len(r1.Wins))
	}
	w := r1.Wins[0]
	if w.Han != 13 || w.Fu != 40 || w.LoserSeat == nil || *w.LoserSeat != 0 {
		t.Errorf("算分明细不对: %+v", w)
	}
	if w.PaoSeat == nil || *w.PaoSeat != 1 || w.PaoYaku == nil || *w.PaoYaku != "大三元" {
		t.Errorf("帕奥责任者没存回: %v / %v", w.PaoSeat, w.PaoYaku)
	}
	if !strings.Contains(string(w.Payments), "-40300") {
		t.Errorf("payments 没原样存回: %s", w.Payments)
	}
	if len(r2.Wins) != 0 {
		t.Errorf("流局不该有算分行，却查出 %d 条", len(r2.Wins))
	}

	// 列表那一侧也要看得到：终局结果与 finished_at。
	rows, err := p.GameHistory(ctx, seats[0], 0, 10)
	if err != nil {
		t.Fatalf("GameHistory: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("历史里 %d 场，期望 1", len(rows))
	}
	if rows[0].FinishedAt == nil {
		t.Error("历史里这场还是「进行中」—— finished_at 没回填")
	}
	if rows[0].Placement == nil || *rows[0].Placement != 1 || rows[0].Points != 61200 {
		t.Errorf("历史里的顺位/ポイント不对: %+v", rows[0])
	}
}

// FinishGame 必须幂等：同一份记录写两次，第二次当成功，且一行都不多。
//
// 写路径是异步 + 带重试的（设计 §3.2），而重试分不清"上次提交失败了"和"提交成功
// 但回执丢了"。删掉这条幂等判据，一次成功的写入会被重试判成失败，日志里从此多一条
// 永远修不好的错误；而如果只是简单地再写一遍，动作流水会翻倍。
func TestFinishGameIsIdempotent(t *testing.T) {
	p := gamePool(t)
	ctx := context.Background()

	gameID, _, seed := beginTestGame(t, p)
	commitment := mahjong.Commit(gameID, seed)
	rec := fullRecord(gameID, commitment[:])

	if err := p.FinishGame(ctx, rec); err != nil {
		t.Fatalf("第一次 FinishGame: %v", err)
	}
	if err := p.FinishGame(ctx, rec); err != nil {
		t.Fatalf("第二次 FinishGame 应该当成成功（重试就是这条路径）: %v", err)
	}

	var rounds, events, wins int
	if err := p.pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM game_rounds WHERE game_id = $1),
		        (SELECT count(*) FROM game_events WHERE game_id = $1),
		        (SELECT count(*) FROM game_wins   WHERE game_id = $1)`, gameID).
		Scan(&rounds, &events, &wins); err != nil {
		t.Fatalf("统计: %v", err)
	}
	if rounds != 2 || events != 4 || wins != 1 {
		t.Errorf("重复写入后：小局 %d / 动作 %d / 算分 %d，期望 2 / 4 / 1", rounds, events, wins)
	}
}

// 记录指向一场没开过的牌局：必须报 ErrNoGame，而且一行都不能留下。
//
// 删掉那条 RowsAffected 检查会怎样：明细行其实插不进去（外键会拦住），但错误会变成
// 一句 PostgreSQL 的 23503，调用方再也分不出"牌局不存在"和"库挂了"。
func TestFinishGameUnknownGame(t *testing.T) {
	p := gamePool(t)
	ctx := context.Background()

	err := p.FinishGame(ctx, fullRecord(999999, make([]byte, 32)))
	if !errors.Is(err, ErrNoGame) {
		t.Fatalf("err = %v，期望 ErrNoGame", err)
	}

	var games, rounds int
	if err := p.pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM games), (SELECT count(*) FROM game_rounds)`).
		Scan(&games, &rounds); err != nil {
		t.Fatalf("统计: %v", err)
	}
	if games != 0 || rounds != 0 {
		t.Errorf("失败的一次写入留下了数据: games=%d rounds=%d", games, rounds)
	}
}

// 开局这一步也是原子的：四条座位里有一条写不进去（user_id 不存在），
// 那么连 games 那一行都不该留下。
//
// 删掉事务会怎样：库里出现一场没有座位的牌局 —— 它对谁也查不到（历史按座位查），
// 但永远占着一个 id，而且谁都说不清它是什么。
func TestBeginGameIsAtomic(t *testing.T) {
	p := gamePool(t)
	ctx := context.Background()

	seats := [4]int64{mkUser(t, p, "p0"), 0, mkUser(t, p, "p2"), mkUser(t, p, "p3")}
	_, err := p.BeginGame(ctx, BeginParams{
		Rules: []byte(`{}`), ServerSeed: [32]byte{1}, Seats: seats, InitialScore: 25000,
	})
	if err == nil {
		t.Fatal("user_id = 0 违反外键，应该报错")
	}

	var games, gameSeats int
	if err := p.pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM games), (SELECT count(*) FROM game_seats)`).
		Scan(&games, &gameSeats); err != nil {
		t.Fatalf("统计: %v", err)
	}
	if games != 0 || gameSeats != 0 {
		t.Errorf("失败的开局留下了数据: games=%d game_seats=%d", games, gameSeats)
	}
}
