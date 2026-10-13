package ws

import (
	"context"
	"encoding/json"
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
}

func NewHandler(auth Authenticator) *Handler {
	return &Handler{auth: auth, timeout: authTimeout}
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
	// 实验：临时注掉

	// 升级之后 r.Context() 不可直接用了——连接已经从 HTTP 服务器手里被抢走，
	// 那个 ctx 会在 handler 返回时被取消，而我们要它活到连接断开。
	// 但也不能拿 context.Background() 当底：那样进程退出（Ctrl+C / SIGTERM）时
	// 这条连接收不到取消，main 里的优雅关闭对它不起作用。
	// WithoutCancel 正好是这个意思：保留请求 ctx 上的值，剥掉它的取消信号。
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()

	// sessionId 只算一次，日志和 hello 共用。它唯一的用途就是让客户端报的值
	// 能在日志里搜到，分头算两次等于这个字段白设。
	session := newSessionID()
	log := slog.With("session", session)
	log.Info("websocket 连接建立", "remote", r.RemoteAddr)

	u, err := h.authenticate(ctx, c, session, log)
	if err != nil {
		// authenticate 已经给对端发了关闭码，这里只收尾记一笔。
		log.Info("连接结束", "stage", "auth", "err", err)
		return
	}

	log.Info("认证通过", "userId", u.ID, "username", u.Username)
	defer log.Info("websocket 连接断开")

	// 已认证。第 3 步到此为止：把收到的指令记下来，谁处理它们留给后续步骤。
	//
	// 读用 Background 而不是 ctx：带取消的 ctx 一旦被取消，库会顺手关掉连接。
	// 进程退出时那条路现在的行为是"连接被库关掉、读报错返回"，第 4 步接房间层
	// 时要把这条收干净（届时会有连接注册表，能显式关）。
	for {
		env, data, err := readCommand(context.Background(), c)
		if err != nil {
			// 客户端关页面、网线拔了都走这里，属于正常结束，用 Info 不用 Error。
			log.Info("读取结束", "err", err)
			return
		}
		log.Info("收到指令", "type", string(env.Type), "data", string(data))
	}
}

// authenticate 在 h.timeout 内等第一条 auth 指令，成功返回用户。
//
// 返回错误时连接已经被关掉，调用方直接返回即可。
//
// session 由调用方算好传进来，不在这里再算一次（这个错犯过两次）。
//
// 超时不能用"带截止时间的 ctx 去读"这种直观写法实现。踩过的坑：
// coder/websocket 的 setupReadTimeout（conn.go:187）在读的 ctx 到期时会顺手
// c.close() 把整条连接关掉，而 writeFrame 里有一段
//
//	if c.isClosed() && opcode == opClose { err = nil }
//
// 于是后续那个 Close(4401) 既不写关闭帧、又返回 nil —— 客户端只看到 EOF，
// CloseStatus 拿到 -1，而协议 §4.1 明确要求 4401。实测对照过：
// "认证失败"那条路（不涉及 ctx 到期）关闭帧正常上线，只有超时这条路丢帧。
//
// 解法是把读和计时器并发等：到点由我们主动关，连接还是活的，关闭帧发得出去。
// 读用 context.Background()：库对 ctx.Done() == nil 的 ctx 不设内部超时
// （setupReadTimeout 第一行的 return false），连接的生命周期交回给这里管。
func (h *Handler) authenticate(ctx context.Context, c *websocket.Conn, session string, log *slog.Logger) (user.User, error) {
	// 读和写都用一个不带取消的 ctx：超时由下面的定时器负责，
	// 让库自己去关连接会让关闭码发不出去（见上面那段）。
	writeCtx := context.WithoutCancel(ctx)
	readCtx := context.Background()

	if err := writeEvent(writeCtx, c, EventHello, hello{
		ProtocolVersion: protocolVersion,
		ServerTime:      time.Now().UnixMilli(),
		SessionID:       session,
	}); err != nil {
		closeConn(c, websocket.StatusInternalError, "发送 hello 失败")
		return user.User{}, err
	}

	deadline := time.After(h.timeout)

	// 一次只跑一个读。超时后这个 goroutine 会一直挂在读上，直到连接被真正关掉
	// （Close 会把它唤醒）—— 所以不能每轮循环都新起一个，否则泄漏的是无限多个。
	type readResult struct {
		env Envelope
		err error
	}
	reads := make(chan readResult, 1)
	go func() {
		env, _, err := readCommand(readCtx, c)
		reads <- readResult{env, err}
	}()

	for {
		select {
		case <-deadline:
			// 到点了，连接还是活的 —— 关闭帧发得出去。
			closeConn(c, statusAuthFailed, "认证超时")
			return user.User{}, context.DeadlineExceeded

		case r := <-reads:
			if r.err != nil {
				// 客户端正常关闭也走这条：对已经断掉的连接再关一次是无害的
				// （Close 幂等）。但绝不能把它记成"认证超时"，否则日志里
				// 全是假信号，真的超时反而看不出来。
				closeConn(c, statusProtocolError, "协议错误")
				return user.User{}, r.err
			}

			if r.env.Type != CmdAuth {
				// 认证前发别的：回 AUTH_REQUIRED，但连接留着 —— 他还有时间正确认证。
				// 直接关掉的话，一个把 auth 排在别的消息后面的客户端就永远进不来，
				// 而协议只要求"拒绝"，没要求"断开"。
				_ = writeEvent(writeCtx, c, EventError, errorEvent{
					Code:    CodeAuthRequired,
					Message: "请先认证",
					Ref:     r.env.Type,
				})
				go func() {
					env, _, err := readCommand(readCtx, c)
					reads <- readResult{env, err}
				}()
				continue
			}

			var cmd authCommand
			if err := json.Unmarshal(r.env.Data, &cmd); err != nil {
				closeConn(c, statusProtocolError, "auth 的 data 不是对象")
				return user.User{}, err
			}

			u, err := h.auth.Authenticate(ctx, cmd.Token)
			if err != nil {
				// 日志里分得清是票坏了还是库挂了（排查要这个），但发给客户端的
				// 只有一个答案：客户端对这两种情况的处置是同一个动作——重新登录。
				// 所以不造第二个错误码，也别把库的错误细节发出去。
				log.Info("认证失败", "err", err)
				_ = writeEvent(writeCtx, c, EventError, errorEvent{
					Code:    CodeAuthFailed,
					Message: "认证失败",
					Ref:     CmdAuth,
				})
				closeConn(c, statusAuthFailed, "认证失败")
				return user.User{}, err
			}

			if err := writeEvent(writeCtx, c, EventAuthed, authedEvent{
				UserID:   u.ID,
				Username: u.Username,
			}); err != nil {
				closeConn(c, websocket.StatusInternalError, "发送 authed 失败")
				return user.User{}, err
			}
			return u, nil
		}
	}
}
