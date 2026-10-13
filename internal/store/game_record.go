package store

// game_record.go —— 牌局历史的**写**（阶段五）。
//
// 与读路径（game_history.go / game_archive.go）对称：这里也只管 SQL，不管
// "谁在什么时候调用"。调用时机（承诺必须早于发牌、明细异步写）见
// docs/local/阶段五-落库设计.md §3。
//
// 两个方法各一个事务：
//
//	BeginGame   开局写头：games + 四行 game_seats + 承诺
//	FinishGame  结束时写全部明细：6 张表 + 回填 games.finished_at 与座位结果
//
// 分两次写不是选择，是承诺的性质决定的：承诺必须在**牌山生成之前**入库，
// 而那时的明细一行都还不存在。

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/aleksey-45/majspirit/internal/mahjong"
)

// BeginParams 是开一场要先写进去的东西。
//
// 只有这些：其余（每小局的牌山、手牌、动作、算分）都要打完才有。
type BeginParams struct {
	// Rules 是 mahjong.DefaultRules() 序列化后的 JSON，原样进 games.rules。
	// store 不解析它 —— 存快照是为了"以后改了默认规则，旧牌局还能复核出来"。
	Rules []byte
	// ServerSeed 是这一场的服务器种子。调用方传种子、不传承诺：承诺是
	// Commit(gameID, seed) 算出来的，而 gameID 只有数据库能给。
	ServerSeed [32]byte
	// Seats 是四个座位的 userID（按 seat 0..3）。
	Seats [4]int64
	// InitialScore 是持ち点。由调用方给（25000 是规则层的取值，不该写死在数据层）。
	InitialScore int
}

// GameRecord 是一场打完之后要写进去的全部明细。
//
// 它照数据库的列来定，不是照对局的内部状态：两者差一层翻译（int64 毫秒、
// 结构化役种 → JSONB），那一层在 internal/history。
type GameRecord struct {
	GameID int64
	// Seats 按 seat 0..3：最终分、顺位、M-League ポイント。开局那四行在这里被回填。
	Seats  [4]SeatResult
	Rounds []RoundRecord
}

// SeatResult 是一场的结算：写回 game_seats 的三列。
type SeatResult struct {
	FinalScore int
	Placement  int
	Points     int
}

// RoundRecord 是一小局的全部分明细，对应 game_rounds 与挂在其下的四张表。
type RoundRecord struct {
	RoundIndex   int
	RoundWind    string
	HandNumber   int
	DealerSeat   int
	EndedBy      string // "win" / "draw"
	WinnerSeat   *int
	IsTsumo      *bool
	WinningTile  *string
	RiichiSticks int

	// InitialHands 按 seat 0..3，四家各 13 张（发牌那一刻，庄家的第 14 张是它的第一次摸牌）。
	InitialHands [4]string

	Wall   WallRecord
	Events []EventRecord
	Wins   []WinRecord
}

// WallRecord 是 game_walls 的一行 —— 复核这一局牌山所需的全部材料。
//
// Tiles 是**按摸牌顺序**的 136 组紧凑记法（mahjong.Wall.Sequence()），必须在这里
// 抓下来：摸过牌之后 Wall.Tiles() 只剩没摸走的部分。
type WallRecord struct {
	Tiles          string
	WallHash       []byte
	ServerSeed     []byte
	ClientSeeds    [][]byte
	Commitment     []byte
	GoVersion      string
	DoraIndicators []string
	UraIndicators  []string
}

// EventRecord 是 game_events 的一行。Seq 是**小局内**从 1 开始的序号。
type EventRecord struct {
	Seq    int
	Seat   int
	Action string
	Tile   *string
	Detail []byte // JSONB
}

// WinRecord 是 game_wins 的一行（算分明细）。
type WinRecord struct {
	WinSeq      int
	WinnerSeat  int
	LoserSeat   *int
	IsTsumo     bool
	WinningTile string
	Han         int
	Fu          int
	Yakuman     int
	Yaku        []byte // JSONB
	Dora        []byte // JSONB
	Payments    []byte // JSONB
	PaoSeat     *int
	PaoYaku     *string
}

// BeginGame 开一场，返回数据库生成的 gameID。
//
// 返回值不是可选的：承诺是 Commit(gameID, serverSeed) 算出来的，而 gameID 只有
// 数据库能给；对局那边也要拿它当 Options.GameID。所以这一步必须在洗牌之前完成，
// 而且**失败就不该开局** —— 一个没有承诺的牌局，事后复核不出任何东西。
//
// 为什么要两条语句：承诺要绑 gameID，所以在 id 出来之前算不出承诺。两条都在同一个
// 事务里，中间那个"有 games 行、没承诺"的状态外面看不见。
func (p *Pool) BeginGame(ctx context.Context, in BeginParams) (int64, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("开启开局事务: %w", err)
	}
	// 保险丝：任何提前 return 都会回滚（Commit 之后再 Rollback 是无害的）。
	defer func() { _ = tx.Rollback(ctx) }()

	const gameQ = `
INSERT INTO games (rules)
VALUES ($1::jsonb)
RETURNING id
`
	var id int64
	if err := tx.QueryRow(ctx, gameQ, in.Rules).Scan(&id); err != nil {
		return 0, fmt.Errorf("插入牌局: %w", err)
	}

	// committed_at 用 SQL 的 now()：它和这一行、这个事务是同一个时间点。
	commitment := mahjong.Commit(id, in.ServerSeed)
	if _, err := tx.Exec(ctx,
		`UPDATE games SET commitment = $2, committed_at = now() WHERE id = $1`,
		id, commitment[:]); err != nil {
		return 0, fmt.Errorf("写入承诺: %w", err)
	}

	// 座位一行一条：四条 INSERT 比拼一条多值 INSERT 好读，而这段代码只在开局跑一次。
	const seatQ = `
INSERT INTO game_seats (game_id, seat, user_id, initial_score)
VALUES ($1, $2, $3, $4)
`
	for seat, uid := range in.Seats {
		if _, err := tx.Exec(ctx, seatQ, id, seat, uid, in.InitialScore); err != nil {
			return 0, fmt.Errorf("插入座位 %d: %w", seat, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("提交开局事务: %w", err)
	}
	return id, nil
}

// FinishGame 把一场的全部明细写进去，最后回填 games.finished_at 与座位结果。
//
// 全在一个事务里：写一半失败就什么都不留，不会出现"有小局没有动作"这种存档。
//
// **幂等**：已经写过的场次直接返回 nil。这不是锦上添花 —— 写路径是异步的、带重试的
// （设计 §3.2），而重试分不清"上一次提交失败了"和"提交成功但回执丢了"。没有这条，
// 一次成功的写入会被重试判成失败，然后日志里多一条永远修不好的错误。
func (p *Pool) FinishGame(ctx context.Context, rec GameRecord) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("开启结束事务: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 先落 finished_at。这一条同时兼作"这场存在吗"的检查：UPDATE 没有命中，
	// 说明记录指向一场从来没开过的牌局（调用方的 bug），必须报出来而不是静默写一堆孤儿行。
	tag, err := tx.Exec(ctx, `UPDATE games SET finished_at = now() WHERE id = $1`, rec.GameID)
	if err != nil {
		return fmt.Errorf("回填牌局结束时间: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoGame
	}

	// 幂等判据：明细一旦写过就一定有 game_rounds。上一条 UPDATE 在重复写入时
	// 也会命中，所以这个检查必须放在它后面。
	var recorded bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM game_rounds WHERE game_id = $1)`, rec.GameID).Scan(&recorded); err != nil {
		return fmt.Errorf("检查牌局是否已落库: %w", err)
	}
	if recorded {
		return nil
	}

	for _, r := range rec.Rounds {
		if err := writeRound(ctx, tx, rec.GameID, r); err != nil {
			return err
		}
	}

	const seatQ = `
UPDATE game_seats
SET final_score = $3, placement = $4, points = $5
WHERE game_id = $1 AND seat = $2
`
	for seat, s := range rec.Seats {
		if _, err := tx.Exec(ctx, seatQ, rec.GameID, seat, s.FinalScore, s.Placement, s.Points); err != nil {
			return fmt.Errorf("回填座位 %d 的结果: %w", seat, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("提交结束事务: %w", err)
	}
	return nil
}

// writeRound 写一小局：汇总行，加上初始手牌、牌山、动作、算分四张明细表。
func writeRound(ctx context.Context, tx pgx.Tx, gameID int64, r RoundRecord) error {
	const roundQ = `
INSERT INTO game_rounds (game_id, round_index, round_wind, hand_number, dealer_seat,
                         ended_by, winner_seat, is_tsumo, winning_tile, riichi_sticks)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`
	if _, err := tx.Exec(ctx, roundQ, gameID, r.RoundIndex, r.RoundWind, r.HandNumber,
		r.DealerSeat, r.EndedBy, r.WinnerSeat, r.IsTsumo, r.WinningTile, r.RiichiSticks); err != nil {
		return fmt.Errorf("写入小局 %d: %w", r.RoundIndex, err)
	}

	const handQ = `
INSERT INTO game_initial_hands (game_id, round_index, seat, tiles)
VALUES ($1, $2, $3, $4)
`
	for seat, tiles := range r.InitialHands {
		if _, err := tx.Exec(ctx, handQ, gameID, r.RoundIndex, seat, tiles); err != nil {
			return fmt.Errorf("写入小局 %d 的初始手牌 %d: %w", r.RoundIndex, seat, err)
		}
	}

	const wallQ = `
INSERT INTO game_walls (game_id, round_index, tiles, wall_hash, server_seed, commitment,
                        client_seeds, go_version, dora_indicators, ura_indicators)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
`
	if _, err := tx.Exec(ctx, wallQ, gameID, r.RoundIndex, r.Wall.Tiles, r.Wall.WallHash,
		r.Wall.ServerSeed, r.Wall.Commitment, r.Wall.ClientSeeds, r.Wall.GoVersion,
		r.Wall.DoraIndicators, r.Wall.UraIndicators); err != nil {
		return fmt.Errorf("写入小局 %d 的牌山: %w", r.RoundIndex, err)
	}

	// 动作逐条 Exec。一场最多几百条，都在同一个事务里，本地是毫秒级；
	// 要更快就上 CopyFrom，但那是有了真实数据量之后才谈的事。
	const eventQ = `
INSERT INTO game_events (game_id, round_index, seq, seat, action, tile, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
`
	for _, e := range r.Events {
		if _, err := tx.Exec(ctx, eventQ, gameID, r.RoundIndex, e.Seq, e.Seat,
			e.Action, e.Tile, e.Detail); err != nil {
			return fmt.Errorf("写入小局 %d 的第 %d 条动作: %w", r.RoundIndex, e.Seq, err)
		}
	}

	const winQ = `
INSERT INTO game_wins (game_id, round_index, win_seq, winner_seat, loser_seat, is_tsumo,
                       winning_tile, han, fu, yakuman, yaku, dora, payments, pao_seat, pao_yaku)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12::jsonb, $13::jsonb, $14, $15)
`
	for _, w := range r.Wins {
		if _, err := tx.Exec(ctx, winQ, gameID, r.RoundIndex, w.WinSeq, w.WinnerSeat,
			w.LoserSeat, w.IsTsumo, w.WinningTile, w.Han, w.Fu, w.Yakuman,
			w.Yaku, w.Dora, w.Payments, w.PaoSeat, w.PaoYaku); err != nil {
			return fmt.Errorf("写入小局 %d 的算分明细: %w", r.RoundIndex, err)
		}
	}
	return nil
}
