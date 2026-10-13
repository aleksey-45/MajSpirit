package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aleksey-45/majspirit/internal/mahjong"
)

// game_history_test.go —— 牌局历史的读。
//
// 和 user_test.go 一样打真 PostgreSQL（会 DROP SCHEMA），所以走 testPool 与
// MAJSPIRIT_TEST_DATABASE_URL。
//
// 夹具是**手写 SQL** 而不是调用写路径：写路径（BeginGame/FinishGame）还没实现，
// 而这里要验的是"读得对不对"，不是"写得对不对"。手写 INSERT 还有个好处 ——
// 它把表结构当成契约钉住：改了列名或类型，这个文件会先炸。

// gamePool 清空 schema、跑迁移，返回一个空库。
func gamePool(t *testing.T) *Pool {
	t.Helper()

	p := testPool(t)
	clean(t, p)
	if err := p.Migrate(context.Background()); err != nil {
		t.Fatalf("迁移: %v", err)
	}
	return p
}

func mkUser(t *testing.T, p *Pool, name string) int64 {
	t.Helper()

	var id int64
	err := p.pool.QueryRow(context.Background(),
		`INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id`,
		name).Scan(&id)
	if err != nil {
		t.Fatalf("插入用户 %s: %v", name, err)
	}
	return id
}

// mkGame 插一场牌局的头两行（games + game_seats），和真实的写路径一样：
// 开局只写这两张表。finished 决定 finished_at / placement 是否为 NULL。
func mkGame(t *testing.T, p *Pool, players [4]int64, finished bool) int64 {
	t.Helper()
	ctx := context.Background()

	var id int64
	// finished_at 直接用 SQL 的 now()：它在一个语句里是稳定的，所以
	// finished_at = started_at 一定满足 games_finished_after_started 这条 CHECK。
	// 用 Go 的 time.Now() 反而会踩到它 —— 那个时刻早于事务的 now()。
	err := p.pool.QueryRow(ctx, `
INSERT INTO games (rules, started_at, commitment, committed_at, finished_at)
VALUES ('{"kiriageMangan":true,"akaDora":1}'::jsonb, now(), '\xaabb'::bytea, now(),
        CASE WHEN $1 THEN now() ELSE NULL END)
RETURNING id`, finished).Scan(&id)
	if err != nil {
		t.Fatalf("插入牌局: %v", err)
	}

	for seat, uid := range players {
		var placement *int
		final, points := 0, 0
		if finished {
			pl := seat + 1
			placement = &pl
			final = 25000 + (4-seat)*1000
			points = final - 30000 + (5-pl)*1000
		}
		if _, err := p.pool.Exec(ctx, `
INSERT INTO game_seats (game_id, seat, user_id, initial_score, final_score, placement, points)
VALUES ($1, $2, $3, 25000, $4, $5, $6)`, id, seat, uid, final, placement, points); err != nil {
			t.Fatalf("插入座位 %d: %v", seat, err)
		}
	}
	return id
}

// mkRound 插一小局：rounds + 四家初始手牌 + 牌山 + 动作流水 + 算分明细。
//
// tiles 传的就是**按摸牌顺序**的紧凑记法（Wall.Sequence()），136 组 ——
// 和真实写路径存进去的格式一样。
func mkRound(t *testing.T, p *Pool, gameID int64, roundIndex int, tiles string, dora []string) {
	t.Helper()
	ctx := context.Background()

	winner := 2
	won := true
	tile := "5p"
	if _, err := p.pool.Exec(ctx, `
INSERT INTO game_rounds (game_id, round_index, round_wind, hand_number, dealer_seat,
                         ended_by, winner_seat, is_tsumo, winning_tile, riichi_sticks)
VALUES ($1, $2, '1z', $3, 0, 'win', $4, $5, $6, 1)`,
		gameID, roundIndex, roundIndex-1, winner, won, tile); err != nil {
		t.Fatalf("插入小局: %v", err)
	}

	// 四家手牌故意各不相同，好让"按 seat 对齐"这件事可被证伪。
	hands := [4]string{"123m456s789p11z", "11122m333s", "456p789p11122z", "19m19p19s1234567z"}
	for seat, h := range hands {
		if _, err := p.pool.Exec(ctx, `
INSERT INTO game_initial_hands (game_id, round_index, seat, tiles) VALUES ($1, $2, $3, $4)`,
			gameID, roundIndex, seat, h); err != nil {
			t.Fatalf("插入初始手牌 %d: %v", seat, err)
		}
	}

	// client_seeds 写成 "有值 / NULL / 有值 / 有值"：验牌脚本要能按下标取，
	// 缺的那一位必须是 null 而不是空串或错位。
	if _, err := p.pool.Exec(ctx, `
INSERT INTO game_walls (game_id, round_index, tiles, wall_hash, server_seed, commitment,
                        client_seeds, go_version, dora_indicators, ura_indicators)
VALUES ($1, $2, $3, '\x10b5'::bytea, '\x6f1a'::bytea, '\xa3c9'::bytea,
        ARRAY['\x01'::bytea, NULL, '\x03'::bytea, '\x04'::bytea],
        'go1.27.0', $4, ARRAY['7m'])`,
		gameID, roundIndex, tiles, dora); err != nil {
		t.Fatalf("插入牌山: %v", err)
	}

	for seq, action := range []string{"draw", "discard", "riichi"} {
		var eventTile any = "9m"
		detail := `{"tsumogiri":false}`
		if action == "riichi" {
			eventTile = nil // 立直这条动作本身不带牌
		}
		if _, err := p.pool.Exec(ctx, `
INSERT INTO game_events (game_id, round_index, seq, seat, action, tile, detail)
VALUES ($1, $2, $3, 0, $4, $5, $6::jsonb)`,
			gameID, roundIndex, seq+1, action, eventTile, detail); err != nil {
			t.Fatalf("插入动作: %v", err)
		}
	}

	var loser any = 0
	paoSeat := 1
	paoYaku := "大三元"
	if _, err := p.pool.Exec(ctx, `
INSERT INTO game_wins (game_id, round_index, win_seq, winner_seat, loser_seat, is_tsumo,
                       winning_tile, han, fu, yakuman, yaku, dora, payments, pao_seat, pao_yaku)
VALUES ($1, $2, 1, 2, $3, false, '5p', 13, 40, 0,
        '[{"name":"立直","han":1}]'::jsonb, '{"dora":1,"ura":1,"aka":0}'::jsonb,
        '{"0":-8000,"1":-40300,"2":48300,"3":0}'::jsonb, $4, $5)`,
		gameID, roundIndex, loser, paoSeat, paoYaku); err != nil {
		t.Fatalf("插入算分明细: %v", err)
	}
}

// 列表按 id 倒序，游标翻页不重不漏，且只含自己的牌局。
//
// 删掉它会漏掉：倒序写反（最旧的排最前）、游标写成 <=（翻页时最后一条重复出现）、
// 漏了 user_id 过滤（把别人的牌局混进来）。
func TestGameHistoryKeysetPaging(t *testing.T) {
	p := gamePool(t)
	ctx := context.Background()

	alice := mkUser(t, p, "alice")
	bob := mkUser(t, p, "bob")
	others := [3]int64{mkUser(t, p, "c1"), mkUser(t, p, "c2"), mkUser(t, p, "c3")}

	// alice 在前三场（座位 0），bob 在第四场。id 递增 = 时间递增。
	first := mkGame(t, p, [4]int64{alice, others[0], others[1], others[2]}, true)
	second := mkGame(t, p, [4]int64{alice, others[0], others[1], others[2]}, true)
	third := mkGame(t, p, [4]int64{alice, others[0], others[1], others[2]}, true)
	bobs := mkGame(t, p, [4]int64{bob, others[0], others[1], others[2]}, true)

	// 一页 2 条，alice 有 3 场 —— 于是必定有第二页。
	page1, err := p.GameHistory(ctx, alice, 0, 2)
	if err != nil {
		t.Fatalf("第一页: %v", err)
	}
	if got := ids(page1); !equal(got, []int64{third, second}) {
		t.Fatalf("第一页 = %v，期望 [%d %d]（按 id 倒序）", got, third, second)
	}

	// 用第一页最后一条的 id 当游标取下一页：只剩最旧的那一场，且不含已出现过的。
	page2, err := p.GameHistory(ctx, alice, page1[len(page1)-1].GameID, 2)
	if err != nil {
		t.Fatalf("第二页: %v", err)
	}
	if got := ids(page2); !equal(got, []int64{first}) {
		t.Fatalf("第二页 = %v，期望只有 %d（游标要用 < 而不是 <=）", got, first)
	}

	// 游标是**排他**的：给一个比所有牌局都大的值，三场都还在。
	if rows, err := p.GameHistory(ctx, alice, bobs, 10); err != nil {
		t.Fatalf("用大游标取: %v", err)
	} else if len(rows) != 3 {
		t.Fatalf("用大游标取到 %d 场，期望 3（游标排他，不是「从这里开始」）", len(rows))
	}

	all, err := p.GameHistory(ctx, alice, 0, 10)
	if err != nil {
		t.Fatalf("全部: %v", err)
	}
	for _, row := range all {
		if row.GameID == bobs {
			t.Fatal("alice 的列表里出现了 bob 的牌局 —— user_id 过滤丢了")
		}
	}
	if len(all) != 3 {
		t.Fatalf("alice 有 %d 场，期望 3", len(all))
	}

	// points 原样出来，不做任何换算（单位是"点"，1000 点 = 1 pt）。
	if all[0].Points == 0 {
		t.Error("points 是 0：夹具写的是非零值，说明这一列没被读出来")
	}
	if all[0].FinishedAt == nil {
		t.Error("已结束的牌局 finished_at 却是 NULL")
	}
}

// 进行中的牌局也要出现在列表里，但 finished_at / placement 为 NULL。
//
// 删掉它会漏掉：把"还没结束"的对局从历史里静默丢掉（玩家打完才发现它不在列表里），
// 或者把 NULL 的 placement 读成 0（显示成"第 0 名"）。
func TestGameHistoryKeepsUnfinishedGame(t *testing.T) {
	p := gamePool(t)
	ctx := context.Background()

	alice := mkUser(t, p, "alice")
	others := [3]int64{mkUser(t, p, "c1"), mkUser(t, p, "c2"), mkUser(t, p, "c3")}
	live := mkGame(t, p, [4]int64{alice, others[0], others[1], others[2]}, false)

	rows, err := p.GameHistory(ctx, alice, 0, 10)
	if err != nil {
		t.Fatalf("GameHistory: %v", err)
	}
	if len(rows) != 1 || rows[0].GameID != live {
		t.Fatalf("进行中的牌局没被查出来: %+v", rows)
	}
	if rows[0].FinishedAt != nil {
		t.Error("进行中的牌局 finished_at 应该是 NULL")
	}
	if rows[0].Placement != nil {
		t.Errorf("进行中的牌局 placement 应该是 NULL，得到 %d", *rows[0].Placement)
	}
	if rows[0].InitialScore != 25000 {
		t.Errorf("initial_score = %d，期望 25000", rows[0].InitialScore)
	}
}

// 单局存档：四家手牌按 seat 对齐、牌山字段逐一解出来、动作与算分挂到对的小局上。
//
// 删掉它会漏掉：初始手牌错位（把二的牌画到三家）、client_seeds 的 NULL 变成空串、
// 动作流水串到别的小局、BYTEA 被当成字符串读。
func TestGameArchiveGroupsByRound(t *testing.T) {
	p := gamePool(t)
	ctx := context.Background()

	alice := mkUser(t, p, "alice")
	bob := mkUser(t, p, "bob")
	carol := mkUser(t, p, "carol")
	dave := mkUser(t, p, "dave")
	game := mkGame(t, p, [4]int64{alice, bob, carol, dave}, true)

	// 牌山用真的洗牌结果：136 组、按摸牌顺序，正是写路径要存的格式。
	tiles := mahjong.NewWallFromSeed([32]byte{7}).Sequence()
	if n := len(mahjong.MustParseTiles(tiles)); n != 136 {
		t.Fatalf("夹具的牌山是 %d 张，期望 136（Sequence 的契约）", n)
	}
	mkRound(t, p, game, 1, tiles, []string{"3s"})
	mkRound(t, p, game, 2, tiles, []string{"3s", "7m"}) // 第二局杠过，指示牌多一张

	arch, err := p.GameArchive(ctx, game)
	if err != nil {
		t.Fatalf("GameArchive: %v", err)
	}

	if arch.GameID != game || arch.FinishedAt == nil {
		t.Fatalf("牌局头不对: id=%d finished=%v", arch.GameID, arch.FinishedAt)
	}
	if !strings.Contains(string(arch.Rules), "kiriageMangan") {
		t.Errorf("rules 没原样透出: %s", arch.Rules)
	}

	if len(arch.Seats) != 4 {
		t.Fatalf("座位 %d 个，期望 4", len(arch.Seats))
	}
	for i, s := range arch.Seats {
		if s.Seat != i {
			t.Errorf("第 %d 项是 seat %d —— 座位没按顺序", i, s.Seat)
		}
		if s.Placement == nil || *s.Placement != i+1 {
			t.Errorf("seat %d 的 placement 不对: %v", i, s.Placement)
		}
	}
	if arch.Seats[0].Username != "alice" || arch.Seats[0].UserID != alice {
		t.Errorf("0 号座是 %+v，期望 alice/%d", arch.Seats[0], alice)
	}

	if len(arch.Rounds) != 2 {
		t.Fatalf("小局 %d 个，期望 2", len(arch.Rounds))
	}
	r1, r2 := arch.Rounds[0], arch.Rounds[1]
	if r1.RoundIndex != 1 || r2.RoundIndex != 2 {
		t.Fatalf("小局顺序不对: %d, %d", r1.RoundIndex, r2.RoundIndex)
	}
	if r1.EndedBy != "win" || r1.WinnerSeat == nil || *r1.WinnerSeat != 2 {
		t.Errorf("第 1 小局的结末方式/和牌者不对: %s / %v", r1.EndedBy, r1.WinnerSeat)
	}

	// 手牌按 seat 对齐：2 号座是第三家那 13 张，不是别人的。
	if got := r1.InitialHands[2]; got != "456p789p11122z" {
		t.Errorf("2 号座的初始手牌是 %q，期望 456p789p11122z（错位了？）", got)
	}
	if len(r1.InitialHands) != 4 {
		t.Errorf("initialHands 有 %d 项，期望恒 4 项", len(r1.InitialHands))
	}

	if r1.Wall == nil {
		t.Fatal("第 1 小局没有牌山")
	}
	if r1.Wall.Tiles != tiles {
		t.Error("牌山文本没有原样存回")
	}
	if string(r1.Wall.WallHash) != "\x10\xb5" {
		t.Errorf("wall_hash = %x，期望 10b5", r1.Wall.WallHash)
	}
	if string(r1.Wall.ServerSeed) != "\x6f\x1a" || string(r1.Wall.Commitment) != "\xa3\xc9" {
		t.Errorf("种子/承诺解错了: %x / %x", r1.Wall.ServerSeed, r1.Wall.Commitment)
	}
	if len(r1.Wall.ClientSeeds) != 4 {
		t.Fatalf("client_seeds 有 %d 项，期望 4", len(r1.Wall.ClientSeeds))
	}
	if r1.Wall.ClientSeeds[0] == nil || r1.Wall.ClientSeeds[1] != nil {
		t.Error("client_seeds 的 NULL 位置不对：第 1 位有值、第 2 位是 NULL")
	}
	if r1.Wall.GoVersion != "go1.27.0" {
		t.Errorf("go_version = %q", r1.Wall.GoVersion)
	}
	if len(r1.Wall.UraIndicators) != 1 || r1.Wall.UraIndicators[0] != "7m" {
		t.Errorf("里宝指示牌 = %v", r1.Wall.UraIndicators)
	}
	// 第二局多一张指示牌：数组是"杠了就追加"，不是替换。
	if len(r2.Wall.DoraIndicators) != 2 {
		t.Errorf("第 2 小局的宝牌指示牌 = %v，期望 2 张", r2.Wall.DoraIndicators)
	}

	if len(r1.Events) != 3 {
		t.Fatalf("第 1 小局有 %d 条动作，期望 3", len(r1.Events))
	}
	for i, e := range r1.Events {
		if e.Seq != i+1 {
			t.Errorf("第 %d 条动作的 seq = %d，期望 %d", i, e.Seq, i+1)
		}
	}
	if r1.Events[2].Tile != nil {
		t.Error("立直这条动作不该带牌，读回来却是非 NULL")
	}
	if !strings.Contains(string(r1.Events[0].Detail), "tsumogiri") {
		t.Errorf("动作的 detail 没原样透出: %s", r1.Events[0].Detail)
	}
	if len(r2.Events) != 3 {
		t.Errorf("第 2 小局的动作串到别处了: %d 条", len(r2.Events))
	}

	if len(r1.Wins) != 1 {
		t.Fatalf("第 1 小局有 %d 条算分，期望 1", len(r1.Wins))
	}
	w := r1.Wins[0]
	if w.WinnerSeat != 2 || w.LoserSeat == nil || *w.LoserSeat != 0 || w.IsTsumo {
		t.Errorf("算分明细的座位不对: %+v", w)
	}
	if w.Han != 13 || w.Fu != 40 {
		t.Errorf("番符 = %d/%d，期望 13/40", w.Han, w.Fu)
	}
	if w.PaoSeat == nil || *w.PaoSeat != 1 || w.PaoYaku == nil || *w.PaoYaku != "大三元" {
		t.Errorf("帕奥责任者没读出来: %v / %v", w.PaoSeat, w.PaoYaku)
	}
	if !strings.Contains(string(w.Payments), "-40300") {
		t.Errorf("payments 没原样透出: %s", w.Payments)
	}
	if len(r2.Wins) != 1 {
		t.Errorf("第 2 小局的算分串到别处了: %d 条", len(r2.Wins))
	}
}

// 进行中的牌局：头两行在，明细一行没有。这正是协议 §3.6 那条隐藏信息规则
// 不需要判断分支的原因（设计 §3.3）。
//
// 删掉它会漏掉：详情读不到进行中的牌局（回 404），或者 nil 切片被序列化成 null
// 而不是空数组。
func TestGameArchiveOfLiveGameHasNoRounds(t *testing.T) {
	p := gamePool(t)
	ctx := context.Background()

	alice := mkUser(t, p, "alice")
	others := [3]int64{mkUser(t, p, "c1"), mkUser(t, p, "c2"), mkUser(t, p, "c3")}
	live := mkGame(t, p, [4]int64{alice, others[0], others[1], others[2]}, false)

	arch, err := p.GameArchive(ctx, live)
	if err != nil {
		t.Fatalf("进行中的牌局应该查得到: %v", err)
	}
	if len(arch.Rounds) != 0 {
		t.Errorf("进行中的牌局有 %d 个小局 —— 明细行不该在这个时候就存在", len(arch.Rounds))
	}
	if len(arch.Seats) != 4 {
		t.Errorf("座位 %d 个，期望 4", len(arch.Seats))
	}
	if arch.FinishedAt != nil {
		t.Error("进行中的牌局 finished_at 应该是 NULL")
	}
}

// 不存在的 gameId：必须是 ErrNoGame（api 靠它回 404），不是别的错误。
//
// 删掉它会漏掉：把"没这场牌局"和"库挂了"混成同一个错误，于是 404 变成 500。
func TestGameArchiveMissing(t *testing.T) {
	p := gamePool(t)

	_, err := p.GameArchive(context.Background(), 999999)
	if !errors.Is(err, ErrNoGame) {
		t.Fatalf("err = %v，期望 ErrNoGame", err)
	}
}

// 初始手牌的座位越界必须**报错**，不能静默跳过。
//
// seat 是四张明细表里唯一没有约束的列（主键是 (game_id, round_index, seat)，
// 所以 seat 可以是 9），也是唯一真会发生的坏数据。跳过的话详情会安静地少一张手牌，
// 而谁都不知道。
//
// 删掉它会漏掉：坏数据被当成"这一家没有手牌"。
func TestGameArchiveRejectsOutOfRangeSeat(t *testing.T) {
	p := gamePool(t)
	ctx := context.Background()

	alice := mkUser(t, p, "alice")
	others := [3]int64{mkUser(t, p, "c1"), mkUser(t, p, "c2"), mkUser(t, p, "c3")}
	game := mkGame(t, p, [4]int64{alice, others[0], others[1], others[2]}, true)
	mkRound(t, p, game, 1, mahjong.NewWallFromSeed([32]byte{7}).Sequence(), []string{"3s"})

	if _, err := p.pool.Exec(ctx, `
INSERT INTO game_initial_hands (game_id, round_index, seat, tiles) VALUES ($1, 1, 9, '123m')`,
		game); err != nil {
		t.Fatalf("插入越界座位: %v", err)
	}

	_, err := p.GameArchive(ctx, game)
	if err == nil {
		t.Fatal("座位 9 的手牌被静默丢掉了 —— 应该报错")
	}
	if !strings.Contains(err.Error(), "越界") {
		t.Errorf("错误消息没说清是座位越界: %v", err)
	}
}

func ids(rows []GameSummary) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.GameID
	}
	return out
}

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
