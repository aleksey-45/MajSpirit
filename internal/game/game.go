package game

// game.go —— 对局这个 actor 的骨架：结构、构造、run 循环、收哪些指令、座位查询。
//
// 一局的推进在 round.go，一场的结算与连庄在 match.go，交给规则层的输入在 input.go，
// 顺位与ポイント在 rank.go，发消息在 emit.go，事件体在 view.go。

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aleksey-45/majspirit/internal/mahjong"
	"github.com/aleksey-45/majspirit/internal/protocol"
)

const (
	startScore           = 25000
	defaultRoundsPerGame = 4

	// 回合时限（协议 §9.1）：基础 5 秒 + 3×10 秒读秒，不可运行时配置。
	//
	// 两个数分开：下发给客户端的 deadlineMs 只是**基础**那一截（前端自己画读秒条），
	// 服务端等满 35 秒才裁定。把读秒也塞进 deadlineMs 会让前端画不出读秒条。
	defaultTurnBase  = 5 * time.Second
	defaultTurnLimit = 35 * time.Second

	// 鸣牌窗口（协议 §9.1）：3 秒，没有读秒。
	defaultClaimLimit = 3 * time.Second
)

// Player 是座位上的玩家。Game 只要"座位 → 用户"这一个映射，不碰连接。
type Player struct {
	UserID   int64
	Username string
}

// Options 是开一场需要的参数。
type Options struct {
	Rounds     int           // 一场打几小局，0 表示 4
	TurnBase   time.Duration // 下发给客户端的 deadlineMs 只算这一截
	TurnLimit  time.Duration // 服务端等满它才裁定
	ClaimLimit time.Duration // 鸣牌窗口；到点没回答的算过
	GameID     int64
	// Seed 为 nil 时用 crypto/rand 取服务器种子（赛后要公布它给人复算）。
	Seed *[32]byte

	// Wall 非 nil 时第一小局直接用这副牌山（测试摆牌用），否则按种子洗。
	Wall *mahjong.Wall

	// OnEnd 在一场打完时调用。**它在 Game 自己的 goroutine 里跑**，所以只允许
	// 投一条指令给房间，不许碰任何房间状态。
	OnEnd func()
}

// View 是发给客户端的座位视图（协议 §6.2 的 seatState 那一套在房间层，
// 这里只要"座位 → 用户"）。
type Game struct {
	players [4]Player
	log     *slog.Logger
	opt     Options

	// stream 是开局时传进来的**房间的**事件流，不是对局自己造的。
	stream *protocol.Emitter

	commands chan command
	closing  chan struct{}
	stopped  chan struct{}
	once     sync.Once

	// closed 在 shutdown 第一行置上：光靠 select 里的 closing 不够（run 的 select
	// 两支都就绪时随机挑），会在"刚关闭"和"还能投指令"之间留一个随机窗口。
	closed atomic.Bool

	// ↓↓↓ 以下字段只在 run() 的 goroutine 里读写 ↓↓↓

	round        int // 1..opt.Rounds
	dealer       int
	honba        int
	scores       [4]int
	riichiSticks int

	wall     *mahjong.Wall
	hands    [4]*mahjong.Hand
	discards [4][]mahjong.Tile
	melds    [4][]mahjong.Meld

	turn int
	// drawn 与 awaitingDiscard 是两件事，不能合成一个：
	// **吃碰之后要打牌，但没有摸牌**。合成一个的话，鸣完就能宣告自摸。
	drawn           bool
	awaitingDiscard bool
	lastDrawn       mahjong.Tile

	deadline time.Time
	// claim 非 nil 表示正在等鸣牌回应。此时 deadline 是**窗口**的截止时刻。
	claim *claim
	// rinshanDraw 标记当前这张是岭上摸的 —— 岭上开花那个役就看它。
	rinshanDraw bool
	// pao 是帕奥责任者（规则集 §8.5）。必须记在**鸣牌那一刻**，见 pao.go。
	pao [4]*paoInfo
	// riichi 是已经宣告立直的座位（规则集 §3）。手牌从此锁定。
	riichi [4]bool
}

// New 开一场并起它的 goroutine。
//
// stream 必须是**房间的**事件流（见 protocol.Emitter）；传 nil 时事件全部丢失。
func New(players [4]Player, stream *protocol.Emitter, opt Options, log *slog.Logger) *Game {
	if opt.Rounds == 0 {
		opt.Rounds = defaultRoundsPerGame
	}
	if opt.TurnBase == 0 {
		opt.TurnBase = defaultTurnBase
	}
	if opt.TurnLimit == 0 {
		opt.TurnLimit = defaultTurnLimit
	}
	if opt.ClaimLimit == 0 {
		opt.ClaimLimit = defaultClaimLimit
	}
	if log == nil {
		log = slog.Default()
	}

	g := &Game{
		players:  players,
		stream:   stream,
		log:      log.With("game", opt.GameID),
		opt:      opt,
		commands: make(chan command),
		closing:  make(chan struct{}),
		stopped:  make(chan struct{}),
		round:    1,
	}
	for i := range g.scores {
		g.scores[i] = startScore
	}
	// 开局**在起 goroutine 之前**做完：那一刻还没有并发，谁碰都安全；而且调用方
	// 一拿到 Game 就已经收到了 game.started 与庄家的第一次摸牌，不必猜"起来了没"。
	g.startRound()

	go g.run()
	return g
}

func (g *Game) run() {
	defer close(g.stopped)

	for {
		var timer *time.Timer
		var expired <-chan time.Time
		if !g.deadline.IsZero() {
			timer = time.NewTimer(time.Until(g.deadline))
			expired = timer.C
		}

		select {
		case c := <-g.commands:
			if timer != nil {
				timer.Stop()
			}
			g.handle(c)
		case <-expired:
			// 两类等待共用这一个字段：正在等鸣牌就是窗口到期，否则是回合到期。
			if g.claim != nil {
				g.onClaimTimeout()
			} else {
				g.onTurnTimeout()
			}
		case <-g.closing:
			return
		}
	}
}

// ── 外部入口 ────────────────────────────────────────────────────────────

// Discard 是 game.discard。
func (g *Game) Discard(userID int64, t mahjong.Tile) error {
	return g.call(command{kind: cmdDiscard, id: userID, tile: t})
}

// DeclareWin 是 game.win。自摸与荣和走同一条指令，服务端按上下文判断是哪种
// （协议 §5.3）—— 玩家不需要知道自己在做什么分类。
func (g *Game) DeclareWin(userID int64) error {
	return g.call(command{kind: cmdWin, id: userID})
}

// Pass 是 game.pass：放弃这次鸣牌机会（协议 §5.3）。
//
// 没有窗口时报错没有意义 —— 客户端可能慢一步发，那不算错。
func (g *Game) Pass(userID int64) error {
	return g.call(command{kind: cmdPass, id: userID})
}

// Chi 是 game.chi：下家吃。tiles 是**手里那两张**，含哪两张由客户端说 ——
// 手里可能有好几种吃法，挑哪一种只有玩家知道。
func (g *Game) Chi(userID int64, tiles [2]mahjong.Tile) error {
	return g.call(command{kind: cmdChi, id: userID, chi: tiles})
}

// Pon 是 game.pon。
func (g *Game) Pon(userID int64) error {
	return g.call(command{kind: cmdPon, id: userID})
}

// Riichi 是 game.riichi：宣告立直，同时把宣言牌打出去（协议 §5.3）。
func (g *Game) Riichi(userID int64, tile mahjong.Tile) error {
	return g.call(command{kind: cmdRiichi, id: userID, tile: tile})
}

// Kan 是 game.kan。kind 取 "open" / "added" / "closed"（协议 §5.3）。
//
// 明杠走鸣牌窗口，不从这里走 —— 这里只收加杠与暗杠。
func (g *Game) Kan(userID int64, kind string, tile mahjong.Tile) error {
	return g.call(command{kind: cmdKan, id: userID, text: kind, tile: tile})
}

// Close 停掉这一场并等 goroutine 退出。可以重复调用。
func (g *Game) Close() {
	g.shutdown()
	<-g.stopped
}

func (g *Game) shutdown() {
	g.once.Do(func() {
		g.closed.Store(true)
		close(g.closing)
	})
}

func (g *Game) call(c command) error {
	if g.closed.Load() {
		return ErrClosed
	}
	c.reply = make(chan error, 1)
	select {
	case g.commands <- c:
		return <-c.reply
	case <-g.stopped:
		return ErrClosed
	}
}

type cmdKind uint8

const (
	cmdDiscard cmdKind = iota
	cmdWin
	cmdPass
	cmdChi
	cmdPon
	cmdKan
	cmdRiichi
	cmdSync
)

type command struct {
	kind  cmdKind
	id    int64
	tile  mahjong.Tile
	chi   [2]mahjong.Tile
	text  string
	reply chan error
}

func (g *Game) handle(c command) {
	var err error
	switch c.kind {
	case cmdDiscard:
		err = g.doDiscard(c.id, c.tile)
	case cmdWin:
		err = g.doWin(c.id)
	case cmdPass:
		err = g.doPass(c.id)
	case cmdChi:
		err = g.doChi(c.id, c.chi)
	case cmdPon:
		err = g.doPon(c.id)
	case cmdKan:
		err = g.doKan(c.id, c.text, c.tile)
	case cmdRiichi:
		err = g.doRiichi(c.id, c.tile)
	case cmdSync:
		err = g.doSync(c.id)
	}
	c.reply <- err
}

func (g *Game) seatOf(userID int64) int {
	for i, p := range g.players {
		if p.UserID == userID {
			return i
		}
	}
	return -1
}
