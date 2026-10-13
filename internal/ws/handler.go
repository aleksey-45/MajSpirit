package ws

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/coder/websocket"
)

const (
	// protocolVersion 在 hello 里宣告；客户端发现不认识的版本应提示刷新（协议 §1.5）。
	// 取常量而不是到处写 1：协议版本是两侧共用的一个事实，写两遍迟早漂移。
	protocolVersion = 1

	// authTimeout 是连上后必须完成认证的时限（协议 §4.1）。
	// 首帧鉴权方案唯一的真风险就在这里：没有它，谁都能开一堆空连接占着
	// goroutine 和 fd，而且服务端还不会察觉。
	authTimeout = 5 * time.Second

	// pingInterval / pongTimeout 是心跳的节奏（协议 §4.3）。
	pingInterval = 15 * time.Second
	pongTimeout  = 5 * time.Second

	// readLimit 是单条消息的字节上限。
	//
	// 库的默认值是 32768（read.go:107），超了返回 ErrMessageTooBig 并以 1009 关连接。
	// 协议 §6.2 的 game.snapshot 带四家手牌 + 四家牌河 + 副露 + 宝牌指示牌，
	// 逐座位定制后粗估 2–4 KB —— 默认值够用，但它不是按本协议算出来的：
	// 消息一长大（比如阶段六的数据统计），症状是客户端带着 1009 随机掉线，
	// 而小消息测不出。取 1 MiB 留出余量；上限本身是防内存打爆的唯一一道。
	readLimit = 1 << 20
)

// Authenticator 是 ws 需要的认证能力。
//
// 定义在这里而不是 internal/user：接口属于使用它的包，和 user.Repo、
// api.UserService 同一条规矩。有了它，"错票必须 4401 关闭"这条不必启动
// 真的用户系统和数据库，一个返回 auth.ErrInvalidToken 的假实现就够。
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (user.User, error)
}

// Handler 挂 /ws，负责一条连接从握手到关闭的整个过程。
//
// timeout 是字段而不是常量，唯一原因是测试：验证"5 秒不认证就关"这条
// 如果真等 5 秒，测试套件会被拖慢，所以测试里把它改成几十毫秒。
type Handler struct {
	auth    Authenticator
	timeout time.Duration

	// pingInterval / pongTimeout 同样是字段：心跳测试若真等 15+5 秒，
	// 一条用例就够拖垮整个套件。零值表示用上面的默认常量。
	pingEvery time.Duration
	pongWait  time.Duration
}

func NewHandler(auth Authenticator) *Handler {
	return &Handler{
		auth:      auth,
		timeout:   authTimeout,
		pingEvery: pingInterval,
		pongWait:  pongTimeout,
	}
}

// ServeHTTP 实现 http.Handler。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 升级。Origin 检查库默认就做（同源放行、没有 Origin 头的客户端放行），
	// 失败时库自己写过响应了，这里直接返回即可。
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		slog.Info("websocket 升级失败", "err", err)
		return
	}
	defer c.CloseNow() // 兜底：任何路径退出都保证连接被关掉

	// 读上限要在读之前设。
	c.SetReadLimit(readLimit)

	// 升级之后 r.Context() 不可直接用：它的取消跟着 handler 返回，而这条连接
	// 要活到断开。也不能用 Background 当底，那样进程退出时它收不到取消。
	// WithoutCancel 保留请求 ctx 上的值、剥掉取消信号。
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()

	// sessionId 只算一次，日志和 hello 共用。
	session := newSessionID()
	log := slog.With("session", session)
	log.Info("websocket 连接建立", "remote", r.RemoteAddr)

	client := newClient(c, h.auth, log)
	client.authTimeout = h.timeout
	defer client.Close(0, "")

	// 写 goroutine 从认证阶段就起来：hello、error、authed 也走 Send，
	// 于是"一条写路径"从第一条消息起就成立。
	client.startWriter(ctx)

	if err := client.authLoop(ctx, session); err != nil {
		// authLoop 已经给对端发了关闭码，这里只收尾记一笔。
		log.Info("连接结束", "stage", "auth", "err", err)
		return
	}

	u := client.user
	log.Info("认证通过", "userId", u.ID, "username", u.Username)
	defer log.Info("websocket 连接断开")

	// 读留在主 goroutine：它是最长的那个，出错就收摊。
	// 心跳独立成 goroutine —— Ping 要等读把 pong 读出来，两者必须并发。
	go client.heartbeat(ctx)
	client.readLoop()
}
