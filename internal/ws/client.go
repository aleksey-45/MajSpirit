package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aleksey-45/majspirit/internal/user"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// 写路径上的时限。
const (
	// writeTimeout 是单条消息的写入上限。
	//
	// 一个不读的客户端会把 TCP 发送缓冲区填满，此后 Write 一直阻塞。
	// 库的写超时只让**这次写失败**（setupWriteTimeout 不关连接，跟读超时相反），
	// 所以失败后由我们自己关 —— 一个写不进去的连接留着没意义。
	writeTimeout = 10 * time.Second

	// sendTimeout 是 Send 等待写 goroutine 取走消息的上限。
	// send channel 满说明对端消费不过来，等下去会把调用它的 goroutine 一起拖住。
	sendTimeout = 10 * time.Second

	// closeWait 是 Close 等待"关闭帧已发出"的上限。超过就硬断 ——
	// 宁可对端收不到关闭原因，也不能让 handler 卡在这里不返回。
	closeWait = 2 * time.Second
)

// Client 是一条已认证的连接。
//
// 它收拢三个 goroutine（读、写、心跳）共用的那批状态：写入口、关闭信号、
// 只关一次的保证。没有这个名字，这些状态就得在 ServeHTTP 里当局部变量
// 逐个传给每个 goroutine。
//
// 名字避开 Conn：coder/websocket 已有一个导出类型叫 Conn，同包里再用
// ws.Conn 会让读代码的人反复分辨说的是哪个。
type Client struct {
	ws   *websocket.Conn
	auth Authenticator
	log  *slog.Logger

	// send 是唯一的写入口。任何 goroutine 想发消息都得走 Send。
	send chan Envelope

	// done 关闭后：Send 立刻返回错误，写循环与心跳退出。
	// 用关闭而不是逐个发信号 —— "已关闭"是广播语义。
	done chan struct{}

	// closeOnce 保证 done 只关一次；重复 close(channel) 会 panic。
	closeOnce sync.Once

	// writeClosed 由写 goroutine 发完关闭帧后关闭。Close 等它，
	// 否则调用方的 defer CloseNow 会把还没出门的关闭帧一起掐掉。
	writeClosed chan struct{}

	// writeStarted 标出写 goroutine 是否起过。没起过就别等 writeClosed，
	// 那只会等满超时（测试里直接构造 Client 的场景）。
	writeStarted atomic.Bool

	// authTimeout 是连上后必须完成认证的时限（协议 §4.1）。
	// 做成字段而不是直接引用常量，唯一原因是测试：真等 5 秒会把套件拖慢。
	authTimeout time.Duration

	// closeMu 保护下面两个字段：Close 在这里记下"要发什么关闭帧"，
	// 真正的写入由写 goroutine 在排空队列之后执行。
	closeMu     sync.Mutex
	closeCode   websocket.StatusCode
	closeReason string

	// user 在认证成功后填上。只在 handler 的主 goroutine 里读写，
	// 所以不需要同步。
	user user.User
}

func newClient(c *websocket.Conn, auth Authenticator, log *slog.Logger) *Client {
	return &Client{
		ws:   c,
		auth: auth,
		log:  log,
		// 缓冲：投递方（将来是房间的 goroutine）不该为了等一次网络写而阻塞。
		send:        make(chan Envelope, 32),
		done:        make(chan struct{}),
		writeClosed: make(chan struct{}),
		authTimeout: authTimeout,
	}
}

// startWriter 起写 goroutine。它必须先于任何 Close 调用，
// 否则 Close 没东西可等（writeStarted 为假）。
func (c *Client) startWriter(ctx context.Context) {
	c.writeStarted.Store(true)
	go c.writeLoop(ctx)
}

// Send 把一条消息交给写 goroutine。
//
// 两种失败都返回错误而不是静默处理：
//   - done 已关闭 → net.ErrClosed；
//   - channel 满（对端消费不过来）→ context.DeadlineExceeded。
//
// 满了**不丢弃**：协议 §1.4 的事件带 seq，客户端靠连续性检测丢消息。
// 静默丢一条会让客户端进入一个它自己都没觉察的错状态，比断开重连糟得多。
// 也不无限等待：投递方将来是房间的 goroutine，用一局牌去等一个慢客户端不划算。
func (c *Client) Send(env Envelope) error {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()

	select {
	case c.send <- env:
		return nil
	case <-c.done:
		return net.ErrClosed
	case <-ctx.Done():
		return fmt.Errorf("发送 %s: 对端消费不过来: %w", env.Type, ctx.Err())
	}
}

// Close 幂等：广播关闭，等写 goroutine 发完关闭帧，再断开。
//
// 关闭帧记在 c.closeCode 上、由写 goroutine 发出，而不是在这里直写：
// 直写会让它插到已排队的消息前面（客户端先收到关闭帧、排在后面的 error
// 被随后的 CloseNow 掐掉）。
//
// 返回前必须等关闭帧真正发出去。不等的话调用方的 defer CloseNow 会立刻
// 掐掉 socket，客户端读到的是 EOF 而不是关闭码。
//
// code 传 0 表示不发关闭帧（对端已经断开时再发一个没意义）。
func (c *Client) Close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		c.closeMu.Lock()
		c.closeCode, c.closeReason = code, reason
		c.closeMu.Unlock()
		close(c.done)
	})

	if !c.writeStarted.Load() {
		// 写 goroutine 没起过（测试直接构造 Client），没人会关 writeClosed。
		return
	}
	select {
	case <-c.writeClosed:
	case <-time.After(closeWait):
		c.log.Info("等待关闭帧超时，直接断开")
		_ = c.ws.CloseNow()
	}
}

// writeLoop 是唯一碰 wsjson.Write 的 goroutine。
//
// 所有出站消息都经它，所以帧不会交错、顺序就是 Send 的顺序，
// 关闭帧排在队尾 —— "先给答案、再关门"是结构性的，不靠时序碰巧。
func (c *Client) writeLoop(ctx context.Context) {
	// 任何退出路径都要发关闭帧并放 Close 走，少一条就有人等满超时。
	defer func() {
		c.finishClose()
		close(c.writeClosed)
	}()

	// WithoutCancel 保留父 ctx 上的值、剥掉取消信号，再加写超时。
	// 直接用 Background 会丢掉父 ctx 上的值。
	write := func(env Envelope) bool {
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
		defer cancel()
		if err := wsjson.Write(wctx, c.ws, env); err != nil {
			c.log.Info("写失败", "type", string(env.Type), "err", err)
			return false
		}
		return true
	}

	for {
		select {
		case <-ctx.Done():
			return
		case env := <-c.send:
			if !write(env) {
				c.Close(0, "")
				return
			}
		case <-c.done:
			// 先把已经排队的消息发完（顺序不能丢），再让 defer 收尾。
			for {
				select {
				case env := <-c.send:
					if !write(env) {
						return
					}
				default:
					return
				}
			}
		}
	}
}

// finishClose 由写 goroutine 调：发关闭帧再断开。
//
// 这里可以直接用库的 Close（它内部写控制帧）：能走到这里的一定是写 goroutine，
// 而且队列已经排空，所以此刻没有别的写者 —— 单写者约定没被破坏。
func (c *Client) finishClose() {
	c.closeMu.Lock()
	code, reason := c.closeCode, c.closeReason
	c.closeMu.Unlock()

	if code != 0 {
		// 库的 Close 会等对端回关闭帧（最多 5 秒）。等它是值得的：
		// 收到回帧才说明关闭码真的到了对面，而不是丢在半路。
		_ = c.ws.Close(code, reason)
	}
	_ = c.ws.CloseNow()
}

// authLoop 在时限内等第一条 auth 指令，成功返回 nil。
//
// 返回非 nil 时连接已经被关掉（并给了对端关闭码），调用方直接返回即可。
//
// 超时不交给"带截止时间的 ctx 去读"，而是用显式定时器与读并发等：
// coder/websocket 的 setupReadTimeout（conn.go:187）在读的 ctx 到期时会顺手
// c.close() 关掉整条连接，而 writeFrame 里有一段
//
//	if c.isClosed() && opcode == opClose { err = nil }
//
// 于是随后的关闭帧既不写、又返回 nil —— 客户端只看到 EOF、CloseStatus 是 -1，
// 而协议 §4.1 要求 4401。auth_wire_test.go 用裸字节能看出这个差别。
func (c *Client) authLoop(ctx context.Context, session string) error {
	// 读用 Background：带取消的 ctx 一旦被取消，库会连连接一起关掉。
	// 超时由下面的定时器负责，连接的生命周期交回这里管。
	readCtx := context.Background()

	// hello 是协议 §4.1 时序里的第一条，必须无条件先发 —— 客户端靠它拿
	// protocolVersion / serverTime / sessionId，也靠它判断"连上了"。
	if err := c.Send(Envelope{
		Type: EventHello,
		Data: mustJSON(hello{
			ProtocolVersion: protocolVersion,
			ServerTime:      time.Now().UnixMilli(),
			SessionID:       session,
		}),
	}); err != nil {
		c.Close(0, "")
		return err
	}

	deadline := time.After(c.authTimeout)

	// 一次只跑一个读（库不允许并发 Read）。超时后这个 goroutine 会一直挂在读上，
	// 直到 Close 把连接真正断开（Close 会唤醒它）—— 所以不能每轮循环都新起一个。
	type readResult struct {
		env Envelope
		err error
	}
	reads := make(chan readResult, 1)
	startRead := func() {
		go func() {
			env, _, err := readCommand(readCtx, c.ws)
			reads <- readResult{env, err}
		}()
	}
	startRead()

	for {
		select {
		case <-deadline:
			// 到点了，连接还是活的 —— 关闭帧发得出去。
			c.log.Info("认证超时")
			c.Close(statusAuthFailed, "认证超时")
			return context.DeadlineExceeded

		case r := <-reads:
			if r.err != nil {
				// 客户端正常关闭也走这条。绝不能把它记成"认证超时"，
				// 否则日志里全是假信号，真的超时反而看不出来。
				// code 传 0：连接已经断了，再发关闭帧没意义。
				c.Close(0, "")
				return r.err
			}

			if r.env.Type != CmdAuth {
				// 认证前发别的：回 AUTH_REQUIRED，但连接留着 —— 他还有时间正确认证。
				// 直接关掉的话，一个把 auth 排在别的消息后面的客户端就永远进不来，
				// 而协议只要求"拒绝"，没要求"断开"。
				if err := c.Send(Envelope{
					Type: EventError,
					Data: mustJSON(errorEvent{
						Code:    CodeAuthRequired,
						Message: "请先认证",
						Ref:     r.env.Type,
					}),
				}); err != nil {
					c.Close(0, "")
					return err
				}
				startRead()
				continue
			}

			var cmd authCommand
			if err := json.Unmarshal(r.env.Data, &cmd); err != nil {
				c.Close(statusProtocolError, "auth 的 data 不是对象")
				return err
			}

			u, err := c.authenticate(ctx, cmd.Token)
			if err != nil {
				return err
			}
			c.user = u

			if err := c.Send(Envelope{
				Type: EventAuthed,
				Data: mustJSON(authedEvent{UserID: u.ID, Username: u.Username}),
			}); err != nil {
				c.Close(0, "")
				return err
			}
			return nil
		}
	}
}

// authenticate 验票。用假实现就能测"错票必须 4401"，不必造真 JWT（见 Authenticator）。
func (c *Client) authenticate(ctx context.Context, token string) (user.User, error) {
	u, err := c.auth.Authenticate(ctx, token)
	if err != nil {
		// 日志里分得清是票坏了还是库挂了（排查要这个），但发给客户端的只有一个答案：
		// 客户端对这两种情况的处置是同一个动作 —— 重新登录。
		// 所以不造第二个错误码，也别把库的错误细节发出去。
		c.log.Info("认证失败", "err", err)
		_ = c.Send(Envelope{
			Type: EventError,
			Data: mustJSON(errorEvent{
				Code:    CodeAuthFailed,
				Message: "认证失败",
				Ref:     CmdAuth,
			}),
		})
		c.Close(statusAuthFailed, "认证失败")
		return user.User{}, err
	}
	return u, nil
}

// readLoop 读已认证连接上的指令。
//
// 用 Background 的 ctx：带取消的 ctx 一旦被取消，库会顺带关掉连接。
// 连接的生命周期由 Close 和心跳负责，不靠 ctx。
func (c *Client) readLoop() {
	readCtx := context.Background()

	for {
		env, data, err := readCommand(readCtx, c.ws)
		if err != nil {
			// 客户端关页面、网线拔了都走这里，属于正常结束。
			c.log.Info("读取结束", "err", err)
			c.Close(0, "")
			return
		}
		// 指令的分派留给后续步骤（房间层）。现在只记下来。
		c.log.Info("收到指令", "type", string(env.Type), "data", string(data))
	}
}

// writeLoop 是唯一碰 wsjson.Write 的 goroutine。
//
// heartbeat 定时发控制帧 ping 并等 pong（协议 §4.3）。
//
// Ping 自己不读连接，它等的是读 goroutine 把 pong 读出来（库的注释里写着
// "must be called concurrently with Reader"）。所以这里失败有两种可能：
// 对端没回，或者读 goroutine 先退出了 —— 两种都该关连接，日志里靠读循环
// 那条"读取结束"区分。
//
// 超时**一次就关**：Ping 失败意味着对端已经 pingInterval+pongTimeout 这么久
// 没有任何响应。再等一轮换不来任何判断力。协议 §4.3 那句"连续两次未回 pong"
// 是指标不是义务 —— 这条差异要写回协议。
func (c *Client) heartbeat(ctx context.Context) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pongTimeout)
			err := c.ws.Ping(pctx)
			cancel()
			if err != nil {
				c.log.Info("心跳失败", "err", err)
				c.Close(statusHeartbeatTimeout, "心跳超时")
				return
			}
		}
	}
}

// readCommand 读一条消息并解出信封，顺带把原始字节还给调用方（现在只用于日志）。
//
// 只收文本帧：协议 §2.1 定的是 JSON，二进制帧在这个协议里没有位置，
// 放行的话等于给"用另一种编码绕过协议"开口子。
func readCommand(ctx context.Context, c *websocket.Conn) (Envelope, []byte, error) {
	typ, data, err := c.Read(ctx)
	if err != nil {
		return Envelope{}, nil, err
	}
	if typ != websocket.MessageText {
		return Envelope{}, data, fmt.Errorf("收到非文本帧: %v", typ)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, data, fmt.Errorf("解析信封: %w", err)
	}
	return env, data, nil
}

// mustJSON 编码信封里的 data。传进去的都是自家定义的结构体，
// 编码失败说明类型写错了（比如塞了个 channel），那是程序 bug 不是可恢复的运行时错误，
// 所以 panic 而不是让每个调用点都多一个 error 分支。
func mustJSON(payload any) json.RawMessage {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("编码消息体失败（结构体定义有问题）: %v", err))
	}
	return raw
}
