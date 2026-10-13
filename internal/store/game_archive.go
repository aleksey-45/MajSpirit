package store

// game_archive.go —— 单局存档的**读**（阶段五）：一场牌局的全部明细。
//
// 一次详情 = 7 张表 7 条查询（不 N+1），在 Go 里按 round_index 拼回小局上。
// 毫秒、hex、NULL 这些怎么给客户端，是 internal/history 的事，这里只从数据库读出字段。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrNoGame 表示这个 gameId 不存在。api 靠它回 404。
var ErrNoGame = errors.New("牌局不存在")

// GameArchive 是一场牌局的完整存档，/api/games/{id} 的原材料。
//
// 它照数据库的表结构组织（Rounds 里嵌 Events/Wins/Hands/Wall），因为怎么拼由外键
// 决定，改不了。
type GameArchive struct {
	GameID     int64
	StartedAt  time.Time
	FinishedAt *time.Time
	Rules      []byte // games.rules 的原始 JSONB，原样透出
	Seats      []ArchiveSeat
	Rounds     []ArchiveRound
}

// ArchiveSeat 是 game_seats 的一行，带上用户名（详情里要显示"谁"）。
type ArchiveSeat struct {
	Seat         int
	UserID       int64
	Username     string
	InitialScore int
	FinalScore   int
	Placement    *int
	Points       int
}

// ArchiveRound 是 game_rounds 的一行，加上属于它的四张明细表。
type ArchiveRound struct {
	RoundIndex   int
	RoundWind    string
	HandNumber   int
	DealerSeat   int
	EndedBy      string
	WinnerSeat   *int
	IsTsumo      *bool
	WinningTile  *string
	RiichiSticks int

	// InitialHands 恒为 4 项、按 seat 0..3 索引；缺的那一项是空串。
	// 定长是因为它是"四家的手牌"，让调用点自己按 map 取反而容易错位。
	InitialHands []string

	Wall   *ArchiveWall
	Events []ArchiveEvent
	Wins   []ArchiveWin
}

// ArchiveWall 是 game_walls 的一行 —— 复核这一局牌山所需的全部材料。
type ArchiveWall struct {
	Tiles          string
	WallHash       []byte
	ServerSeed     []byte
	ClientSeeds    [][]byte // 按 seat 0..3；整列可为 NULL，元素也可为 NULL
	Commitment     []byte
	GoVersion      string
	DoraIndicators []string
	UraIndicators  []string
}

// ArchiveEvent 是 game_events 的一行。
type ArchiveEvent struct {
	Seq    int
	Seat   int
	Action string
	Tile   *string
	Detail []byte
}

// ArchiveWin 是 game_wins 的一行（算分明细）。
type ArchiveWin struct {
	WinSeq      int
	WinnerSeat  int
	LoserSeat   *int
	IsTsumo     bool
	WinningTile string
	Han         int
	Fu          int
	Yakuman     int
	Yaku        []byte
	Dora        []byte
	Payments    []byte
	PaoSeat     *int
	PaoYaku     *string
}

// GameArchive 读一场牌局的全部明细。
//
// 头一条查 games，其余六条各查一张明细表，再按 round_index 拼回小局上。
// 不做 N+1：小局数、事件数都不该影响查询条数。
//
// 不套事务。一次牌局的六张明细表是在结束那一个事务里一起写的，所以读者看到的
// 只会是"全有"或"全无"；进行中的对局本来就只有 games + game_seats 两行
// （设计见 阶段五-落库设计.md §3.3）。
func (p *Pool) GameArchive(ctx context.Context, gameID int64) (GameArchive, error) {
	const headQ = `
SELECT id, started_at, finished_at, rules
FROM games
WHERE id = $1
`
	var arch GameArchive
	err := p.pool.QueryRow(ctx, headQ, gameID).Scan(
		&arch.GameID, &arch.StartedAt, &arch.FinishedAt, &arch.Rules)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return GameArchive{}, ErrNoGame
		}
		return GameArchive{}, fmt.Errorf("查询牌局: %w", err)
	}

	if arch.Seats, err = p.gameSeats(ctx, gameID); err != nil {
		return GameArchive{}, err
	}
	if arch.Rounds, err = p.gameRounds(ctx, gameID); err != nil {
		return GameArchive{}, err
	}

	// 指向切片元素的指针：下面四条查询直接往对应的小局里挂明细。
	// 这里之后不再 append arch.Rounds，所以指针不会失效。
	//
	// 四条查询的 round_index 必定命中：四张明细表都有外键指回 game_rounds，
	// 而我们刚把这个 game 的全部小局都读进来了。命中不了就意味着库坏了 ——
	// 那时让查找直接 panic，比静默少一张牌好。
	byRound := make(map[int]*ArchiveRound, len(arch.Rounds))
	for i := range arch.Rounds {
		byRound[arch.Rounds[i].RoundIndex] = &arch.Rounds[i]
	}
	if err := p.attachHands(ctx, gameID, byRound); err != nil {
		return GameArchive{}, err
	}
	if err := p.attachWalls(ctx, gameID, byRound); err != nil {
		return GameArchive{}, err
	}
	if err := p.attachEvents(ctx, gameID, byRound); err != nil {
		return GameArchive{}, err
	}
	if err := p.attachWins(ctx, gameID, byRound); err != nil {
		return GameArchive{}, err
	}
	return arch, nil
}

func (p *Pool) gameSeats(ctx context.Context, gameID int64) ([]ArchiveSeat, error) {
	// 内连接 users：game_seats.user_id 是 NOT NULL + 外键 ON DELETE RESTRICT，
	// 用户删不掉，所以这里不会因为连接而少行。
	const q = `
SELECT gs.seat::int, gs.user_id, u.username,
       gs.initial_score, gs.final_score, gs.placement::int, gs.points
FROM game_seats gs
JOIN users u ON u.id = gs.user_id
WHERE gs.game_id = $1
ORDER BY gs.seat
`
	rows, err := p.pool.Query(ctx, q, gameID)
	if err != nil {
		return nil, fmt.Errorf("查询牌局座位: %w", err)
	}
	defer rows.Close()

	var out []ArchiveSeat
	for rows.Next() {
		var s ArchiveSeat
		if err := rows.Scan(&s.Seat, &s.UserID, &s.Username,
			&s.InitialScore, &s.FinalScore, &s.Placement, &s.Points); err != nil {
			return nil, fmt.Errorf("读取牌局座位: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历牌局座位: %w", err)
	}
	return out, nil
}

func (p *Pool) gameRounds(ctx context.Context, gameID int64) ([]ArchiveRound, error) {
	const q = `
SELECT round_index::int, round_wind, hand_number::int, dealer_seat::int,
       ended_by, winner_seat::int, is_tsumo, winning_tile, riichi_sticks::int
FROM game_rounds
WHERE game_id = $1
ORDER BY round_index
`
	rows, err := p.pool.Query(ctx, q, gameID)
	if err != nil {
		return nil, fmt.Errorf("查询小局: %w", err)
	}
	defer rows.Close()

	var out []ArchiveRound
	for rows.Next() {
		// InitialHands 先摆成定长 4 项，attachHands 再往里填。
		r := ArchiveRound{InitialHands: make([]string, 4)}
		if err := rows.Scan(&r.RoundIndex, &r.RoundWind, &r.HandNumber, &r.DealerSeat,
			&r.EndedBy, &r.WinnerSeat, &r.IsTsumo, &r.WinningTile, &r.RiichiSticks); err != nil {
			return nil, fmt.Errorf("读取小局: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历小局: %w", err)
	}
	return out, nil
}

func (p *Pool) attachHands(ctx context.Context, gameID int64, byRound map[int]*ArchiveRound) error {
	const q = `
SELECT round_index::int, seat::int, tiles
FROM game_initial_hands
WHERE game_id = $1
ORDER BY round_index, seat
`
	rows, err := p.pool.Query(ctx, q, gameID)
	if err != nil {
		return fmt.Errorf("查询初始手牌: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var roundIndex, seat int
		var tiles string
		if err := rows.Scan(&roundIndex, &seat, &tiles); err != nil {
			return fmt.Errorf("读取初始手牌: %w", err)
		}
		// 座位越界要报错，不能顺手跳过：这一列是四张明细表里**唯一**没有约束的
		// （主键是 (game_id, round_index, seat)，seat 可以是 9）。跳过的话详情
		// 会安静地少一张手牌，而谁都不知道。
		r := byRound[roundIndex]
		if seat < 0 || seat >= len(r.InitialHands) {
			return fmt.Errorf("初始手牌的座位越界: game=%d round=%d seat=%d",
				gameID, roundIndex, seat)
		}
		r.InitialHands[seat] = tiles
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历初始手牌: %w", err)
	}
	return nil
}

func (p *Pool) attachWalls(ctx context.Context, gameID int64, byRound map[int]*ArchiveRound) error {
	// COALESCE 只用在 go_version 上：空串比 *string 少一层解引用，而"没记版本"
	// 与"记了空串"在展示上没有区别。
	const q = `
SELECT round_index::int, tiles, wall_hash, server_seed, commitment, client_seeds,
       COALESCE(go_version, ''), dora_indicators, ura_indicators
FROM game_walls
WHERE game_id = $1
ORDER BY round_index
`
	rows, err := p.pool.Query(ctx, q, gameID)
	if err != nil {
		return fmt.Errorf("查询牌山: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var roundIndex int
		var w ArchiveWall
		if err := rows.Scan(&roundIndex, &w.Tiles, &w.WallHash, &w.ServerSeed, &w.Commitment,
			&w.ClientSeeds, &w.GoVersion, &w.DoraIndicators, &w.UraIndicators); err != nil {
			return fmt.Errorf("读取牌山: %w", err)
		}
		byRound[roundIndex].Wall = &w
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历牌山: %w", err)
	}
	return nil
}

func (p *Pool) attachEvents(ctx context.Context, gameID int64, byRound map[int]*ArchiveRound) error {
	const q = `
SELECT round_index::int, seq, seat::int, action, tile, detail
FROM game_events
WHERE game_id = $1
ORDER BY round_index, seq
`
	rows, err := p.pool.Query(ctx, q, gameID)
	if err != nil {
		return fmt.Errorf("查询动作流水: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var roundIndex int
		var e ArchiveEvent
		if err := rows.Scan(&roundIndex, &e.Seq, &e.Seat, &e.Action, &e.Tile, &e.Detail); err != nil {
			return fmt.Errorf("读取动作流水: %w", err)
		}
		r := byRound[roundIndex]
		r.Events = append(r.Events, e)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历动作流水: %w", err)
	}
	return nil
}

func (p *Pool) attachWins(ctx context.Context, gameID int64, byRound map[int]*ArchiveRound) error {
	const q = `
SELECT round_index::int, win_seq::int, winner_seat::int, loser_seat::int, is_tsumo,
       winning_tile, han::int, fu::int, yakuman::int,
       yaku, dora, payments, pao_seat::int, pao_yaku
FROM game_wins
WHERE game_id = $1
ORDER BY round_index, win_seq
`
	rows, err := p.pool.Query(ctx, q, gameID)
	if err != nil {
		return fmt.Errorf("查询算分明细: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var roundIndex int
		var w ArchiveWin
		if err := rows.Scan(&roundIndex, &w.WinSeq, &w.WinnerSeat, &w.LoserSeat, &w.IsTsumo,
			&w.WinningTile, &w.Han, &w.Fu, &w.Yakuman,
			&w.Yaku, &w.Dora, &w.Payments, &w.PaoSeat, &w.PaoYaku); err != nil {
			return fmt.Errorf("读取算分明细: %w", err)
		}
		r := byRound[roundIndex]
		r.Wins = append(r.Wins, w)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("遍历算分明细: %w", err)
	}
	return nil
}
