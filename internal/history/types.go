package history

// types.go —— **响应体结构**（协议 §3.5 / §3.6），以及从 store 的行生成它的几个函数。
//
// 用例（取一页、取一场）在 history.go；两者分开是因为"响应里有哪些字段"和
// "怎么取数"各自会变：加一个字段不该碰到取数逻辑，换一条查询也不该碰到 wire 契约。
//
// JSON tag 写在这里而不是 api：这一层的职责**就是**把数据库的行翻成响应体
// （毫秒、hex、NULL、按小局分组），再往 api 抄一层字段只会多一份要同步的阅读量。
// 对照 user.User → api.meResponse 那条：那里的结构体存在是因为要联合
// user + token 两处信息，不是为了换名。

import (
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/aleksey-45/majspirit/internal/store"
)

// Summary 是 /api/history 的一行。
//
// Points 的单位是点而不是 pt：61200 表示 +61.2 pt（1000 点 = 1 pt）。
// 服务端不做除法 —— 用整数传是为了避开浮点误差（规则集 §10.4）。
type Summary struct {
	GameID       int64  `json:"gameId"`
	StartedAt    int64  `json:"startedAt"`
	FinishedAt   *int64 `json:"finishedAt"`
	Seat         int    `json:"seat"`
	Placement    *int   `json:"placement"`
	InitialScore int    `json:"initialScore"`
	FinalScore   int    `json:"finalScore"`
	Points       int    `json:"points"`
}

// Page 是一页过往牌局。NextCursor 非 nil 时表示还有更多。
type Page struct {
	Games      []Summary `json:"games"`
	NextCursor *int64    `json:"nextCursor"`
}

// Game 是 /api/games/{id} 的正文。
//
// Rules 是 games.rules 的原样 JSONB（mahjong.DefaultRules() 的序列化）。
// 存它是为了"以后改了默认规则，旧牌局还能复核出来"，所以里面只有算分会读的字段。
type Game struct {
	GameID     int64           `json:"gameId"`
	StartedAt  int64           `json:"startedAt"`
	FinishedAt *int64          `json:"finishedAt"`
	Rules      json.RawMessage `json:"rules"`
	Seats      []Seat          `json:"seats"`
	Rounds     []Round         `json:"rounds"`
}

// Seat 是详情里的一个座位。
type Seat struct {
	Seat         int    `json:"seat"`
	UserID       int64  `json:"userId"`
	Username     string `json:"username"`
	InitialScore int    `json:"initialScore"`
	FinalScore   int    `json:"finalScore"`
	Placement    *int   `json:"placement"`
	Points       int    `json:"points"`
}

// Round 是一小局的存档。
type Round struct {
	RoundIndex   int     `json:"roundIndex"`
	RoundWind    string  `json:"roundWind"`
	HandNumber   int     `json:"handNumber"`
	DealerSeat   int     `json:"dealerSeat"`
	EndedBy      string  `json:"endedBy"`
	WinnerSeat   *int    `json:"winnerSeat"`
	IsTsumo      *bool   `json:"isTsumo"`
	WinningTile  *string `json:"winningTile"`
	RiichiSticks int     `json:"riichiSticks"`

	// InitialHands 恒为 4 项、按 seat 0..3 索引（发牌那一刻四家各 13 张）。
	InitialHands []string `json:"initialHands"`

	// Wall 是复核这一局牌山所需的全部材料。NULL 表示这一局没有存到牌山行。
	Wall   *Wall   `json:"wall"`
	Events []Event `json:"events"`
	Wins   []Win   `json:"wins"`
}

// Wall 对应 game_walls 的一行。字节一律 hex：可读，也能直接粘进任何脚本。
//
// Tiles 按摸牌顺序逐张写成 136 组紧凑记法（mahjong.Wall.Sequence()），
// 绝不是 FormatTiles 那种分组排序：顺序就是信息，用分组记法验证必然失败。
type Wall struct {
	Tiles       string    `json:"tiles"`
	WallHash    string    `json:"wallHash"`
	ServerSeed  string    `json:"serverSeed"`
	ClientSeeds []*string `json:"clientSeeds"`
	Commitment  string    `json:"commitment"`
	GoVersion   string    `json:"goVersion"`

	DoraIndicators []string `json:"doraIndicators"`
	UraIndicators  []string `json:"uraIndicators"`
}

// Event 是一条动作流水。Detail 是原样 JSONB（例如鸣牌种类、是否摸切）。
type Event struct {
	Seq    int             `json:"seq"`
	Seat   int             `json:"seat"`
	Action string          `json:"action"`
	Tile   *string         `json:"tile"`
	Detail json.RawMessage `json:"detail"`
}

// Win 是一次和牌的算分明细。
//
// PaoSeat / PaoYaku 非 null 时，Payments 里会有一笔某一座独付全额的大额支出
// （大三元 / 大四喜 / 四槓子，规则集 §8.5）。展示时必须解释它，别当成算错。
type Win struct {
	WinSeq      int             `json:"winSeq"`
	WinnerSeat  int             `json:"winnerSeat"`
	LoserSeat   *int            `json:"loserSeat"`
	IsTsumo     bool            `json:"isTsumo"`
	WinningTile string          `json:"winningTile"`
	Han         int             `json:"han"`
	Fu          int             `json:"fu"`
	Yakuman     int             `json:"yakuman"`
	Yaku        json.RawMessage `json:"yaku"`
	Dora        json.RawMessage `json:"dora"`
	Payments    json.RawMessage `json:"payments"`
	PaoSeat     *int            `json:"paoSeat"`
	PaoYaku     *string         `json:"paoYaku"`
}

// ── 数据库的行 → 响应体 ───────────────────────────────────────────────

func msPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}

func wall(w *store.ArchiveWall) *Wall {
	if w == nil {
		return nil
	}
	return &Wall{
		Tiles:          w.Tiles,
		WallHash:       hex.EncodeToString(w.WallHash),
		ServerSeed:     hex.EncodeToString(w.ServerSeed),
		ClientSeeds:    clientSeeds(w.ClientSeeds),
		Commitment:     hex.EncodeToString(w.Commitment),
		GoVersion:      w.GoVersion,
		DoraIndicators: nonNil(w.DoraIndicators),
		UraIndicators:  nonNil(w.UraIndicators),
	}
}

// clientSeeds 恒返回 4 项、按 seat 0..3 索引；缺的（整列为 NULL 或该元素为 NULL）
// 给 null。定长是为了让验牌脚本按下标取，不必先判断数组长度。
func clientSeeds(seeds [][]byte) []*string {
	out := make([]*string, 4)
	for i := range out {
		if i < len(seeds) && seeds[i] != nil {
			s := hex.EncodeToString(seeds[i])
			out[i] = &s
		}
	}
	return out
}

// nonNil 保证空切片序列化成 []，而不是 null —— 前端拿到 null 就得写一条特判。
func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func events(in []store.ArchiveEvent) []Event {
	out := make([]Event, 0, len(in))
	for _, e := range in {
		out = append(out, Event{
			Seq:    e.Seq,
			Seat:   e.Seat,
			Action: e.Action,
			Tile:   e.Tile,
			Detail: json.RawMessage(e.Detail),
		})
	}
	return out
}

func wins(in []store.ArchiveWin) []Win {
	out := make([]Win, 0, len(in))
	for _, w := range in {
		out = append(out, Win{
			WinSeq:      w.WinSeq,
			WinnerSeat:  w.WinnerSeat,
			LoserSeat:   w.LoserSeat,
			IsTsumo:     w.IsTsumo,
			WinningTile: w.WinningTile,
			Han:         w.Han,
			Fu:          w.Fu,
			Yakuman:     w.Yakuman,
			Yaku:        json.RawMessage(w.Yaku),
			Dora:        json.RawMessage(w.Dora),
			Payments:    json.RawMessage(w.Payments),
			PaoSeat:     w.PaoSeat,
			PaoYaku:     w.PaoYaku,
		})
	}
	return out
}
