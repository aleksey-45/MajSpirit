package ws

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/aleksey-45/majspirit/internal/room"
	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/coder/websocket"
)

const (
	// 协议版本，在 hello 里宣告（协议 §1.5）。
	protocolVersion = 1

	// 连上后必须完成认证的时限（协议 §4.1）。没有它，空连接能一直占着
	// goroutine 和 fd。
	authTimeout = 5 * time.Second

	// 心跳节奏（协议 §4.3）。
	pingInterval = 15 * time.Second
	pongTimeout  = 5 * time.Second

	// 放开单条消息的上限：库默认 32768（read.go:107），超了以 1009 关连接，
	// 而协议 §6.2 的 game.snapshot 逐座位定制后会更大。
	readLimit = 1 << 20
)

// Authenticator 是 ws 需要的认证能力。
//
// 定义在这里而不是 internal/user：接口属于使用它的包，和 user.Repo、
// api.UserService 同一条规矩。有假实现就不必为了测"错票 4401"造真 JWT。
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (user.User, error)
}

// Handler 挂 /ws，负责一条连接从握手到关闭的整个过程。
//
// 三个时限做成字段而不是直接引用常量，是为了测试能缩短它们
// （真等 5 秒或 15+5 秒会把套件拖垮）。
type Handler struct {
	auth     Authenticator
	registry *Registry
	hub      *room.Hub
	timeout  time.Duration

	pingEvery time.Duration
	pongWait  time.Duration
}

// NewHandler 收一个共享的 Registry 与一个共享的 Hub，两个都必须跨连接共用。
//
// 指令走 连接 → Hub → 房间/对局，事件走 房间/对局 → Registry → 连接：
// Hub 的投递方就是这个 Registry（cmd/server 用 registry.Send 造 Hub）。
func NewHandler(auth Authenticator, registry *Registry, hub *room.Hub) *Handler {
	if registry == nil {
		registry = NewRegistry()
	}
	if hub == nil {
		hub = room.NewHub(registry.Send, nil)
	}
	return &Handler{
		auth:      auth,
		registry:  registry,
		hub:       hub,
		timeout:   authTimeout,
		pingEvery: pingInterval,
		pongWait:  pongTimeout,
	}
}

// ServeHTTP 实现 http.Handler。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Origin 检查库默认就做；失败时库自己写过响应了，这里直接返回。
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		slog.Info("websocket 升级失败", "err", err)
		return
	}
	defer c.CloseNow() // 兜底

	c.SetReadLimit(readLimit)

	// 升级之后 r.Context() 的取消跟着 handler 返回，而这条连接要活到断开；
	// 但也不能用 Background，那样进程退出时它收不到取消。
	// WithoutCancel 保留请求 ctx 上的值、剥掉取消信号。
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()

	session := newSessionID()
	log := slog.With("session", session)
	log.Info("websocket 连接建立", "remote", r.RemoteAddr)

	client := newClient(c, h.auth, h.hub, log)
	client.authTimeout = h.timeout
	client.pingEvery = h.pingEvery
	client.pongWait = h.pongWait
	defer client.Close(0, "")

	// 写 goroutine 从认证阶段就起来，hello/error/authed 也走 Send，
	// 于是"一条写路径"从第一条消息起就成立。
	client.startWriter(ctx)

	if err := client.authLoop(ctx, session); err != nil {
		log.Info("连接结束", "stage", "auth", "err", err)
		return
	}

	u := client.user
	log.Info("认证通过", "userId", u.ID, "username", u.Username)
	defer log.Info("websocket 连接断开")

	// 同一账号只保留一条连接：新的顶掉旧的，旧的以 4403 关闭（协议 §4.6）。
	// 关闭放在锁外（Add 返回旧连接而不是就地关）—— 它可能等写 goroutine。
	if old := h.registry.Add(u, client); old != nil {
		log.Info("同一账号的旧连接被顶掉")
		old.Close(statusReplaced, "在别处登录")
	}

	// 先摘连接、再报告掉线。摘表返回的是"摘掉的确实是这条连接"：被顶号的旧连接
	// 随后也会走到这里，那时注册表里已经是新连接 —— 照报就会把有人管的座位标成离线。
	defer func() {
		if h.registry.Remove(u.ID, client) {
			h.reportGone(u.ID)
		}
	}()

	// 座位还留着的话，这次连接就是重连（协议 §4.5）。
	h.resume(u)

	// 心跳独立成 goroutine：Ping 要等读把 pong 读出来，两者必须并发。
	go client.heartbeat(ctx)
	client.readLoop()
}

// resume 把这条连接接回他原来那个房间（如果还有）。
//
// 协议 §4.5 的重连流程里只有客户端的 game.sync，房间这一半由服务端补发 room.joined
// （带 roomId 与 mySeat；room.seats 只说谁在座）。座位还在线时也照发：客户端此刻
// 手上什么都没有，那正是它重建视图的来源。
func (h *Handler) resume(u user.User) {
	r, ok := h.hub.ByUser(u.ID)
	if !ok {
		return
	}
	// 房间刚关掉之类的失败不算错：那条 room.closed 已经在路上了。
	if err := r.Reconnected(u.ID); err != nil {
		slog.Info("重连房间失败", "userId", u.ID, "err", err)
	}
}

// reportGone 告诉房间这个人的连接断了（座位按协议 §4.5 保留 120 秒）。
// 房间已经没了就什么都不用做。
func (h *Handler) reportGone(userID int64) {
	r, ok := h.hub.ByUser(userID)
	if !ok {
		return
	}
	if err := r.Disconnected(userID); err != nil {
		slog.Info("报告掉线失败", "userId", userID, "err", err)
	}
}
