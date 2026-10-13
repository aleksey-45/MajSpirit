package ws

import (
	"context"
	"encoding/json"
	"errors"
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

const (
	// 不读的客户端会填满发送缓冲区，此后 Write 一直阻塞。
	// 库的写超时只让这次写失败（setupWriteTimeout 不关连接，跟读超时相反），
	// 所以失败后由我们关连接。
	writeTimeout = 10 * time.Second

	sendTimeout = 10 * time.Second

	// Close 等"写 goroutine 处理完关闭"的上限，超过就硬断。
	closeWait = 2 * time.Second

	// 单独给"发出关闭帧"这一步的上限：它不能被对端的回复拖住。
	closeFrameWait = 200 * time.Millisecond
)

// Client 是一条已认证的连接，收拢读、写、心跳三个 goroutine 共用的状态。
// 名字避开 Conn：coder/websocket 已有一个导出类型叫 Conn。
type Client struct {
	ws   *websocket.Conn
	auth Authenticator
	log  *slog.Logger

	// 唯一的写入口。
	send chan Envelope

	// 关闭后：Send 返回错误，写循环与心跳退出。
	done chan struct{}

	// 心跳的回执：读循环读到 pong 消息时投一下，唤醒正在等的心跳。
	pong chan struct{}

	// 关闭有四个触发点，而重复 close(channel) 会 panic。
	closeOnce sync.Once

	// 写 goroutine 发完关闭帧后关闭：Close 要等它，
	// 否则调用方的 defer CloseNow 会把还没出门的关闭帧一起掐掉。
	writeClosed chan struct{}

	// 写 goroutine 起过没有。没起过就不能等 writeClosed。
	writeStarted atomic.Bool

	// 认证时限（协议 §4.1）。做成字段是为了测试能缩短它。
	authTimeout time.Duration

	// 心跳间隔与等 pong 的时限（协议 §4.3）。同样是字段：测试不能真等 15 秒。
	pingEvery time.Duration
	pongWait  time.Duration

	// Close 在这里记下要发的关闭帧，实际写入由写 goroutine 在排空队列后做。
	closeMu     sync.Mutex
	closeCode   websocket.StatusCode
	closeReason string

	// 认证成功后填上，只由 handler 的主 goroutine 读写。
	user user.User
}

func newClient(c *websocket.Conn, auth Authenticator, log *slog.Logger) *Client {
	return &Client{
		ws:          c,
		auth:        auth,
		log:         log,
		send:        make(chan Envelope, 32), // 缓冲：投递方不该为了等一次网络写而阻塞
		done:        make(chan struct{}),
		pong:        make(chan struct{}, 1),
		writeClosed: make(chan struct{}),
		authTimeout: authTimeout,
		pingEvery:   pingInterval,
		pongWait:    pongTimeout,
	}
}

// startWriter 起写 goroutine，必须先于任何 Close（否则 Close 等不到东西）。
func (c *Client) startWriter(ctx context.Context) {
	c.writeStarted.Store(true)
	go c.writeLoop(ctx)
}

// Send 把一条消息交给写 goroutine。满了不丢弃也不无限等：
// 静默丢一条会让客户端的 seq 出现看不见的洞（协议 §1.4），
// 无限等会拖住投递方。
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
// 关闭帧由写 goroutine 发（直写会插到已排队的消息前面）；
// 返回前要等它发完，否则调用方的 defer CloseNow 会把帧一起掐掉。
// code 传 0 表示不发关闭帧。
func (c *Client) Close(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		c.closeMu.Lock()
		c.closeCode, c.closeReason = code, reason
		c.closeMu.Unlock()
		close(c.done)
	})

	select {
	case <-c.writeClosed:
	case <-time.After(closeWait):
		c.log.Info("等待关闭帧超时，直接断开")
		_ = c.ws.CloseNow()
	}
}

// writeLoop 是唯一碰 wsjson.Write 的 goroutine：帧不会交错，顺序即 Send 的顺序，
// 关闭帧排在队尾。
func (c *Client) writeLoop(ctx context.Context) {
	// 任何退出路径都要发关闭帧并放 Close 走。
	defer func() {
		c.finishClose()
		close(c.writeClosed)
	}()

	// WithoutCancel 保留父 ctx 的值、剥掉取消信号，再加写超时。
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
			// 先把排队的消息发完，再让 defer 收尾。
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

// finishClose 由写 goroutine 调：发关闭帧，然后断开。
//
// 这里用库的 Close（它写完帧会等对端回关闭回复），但只等一个很短的上限：
// 那个等待要拿读锁，而读循环正持有它等数据 —— 干等会把关闭流程卡死
// （实测：心跳超时之后 2 秒内没有任何后续动作）。
//
// 也不能先 CloseNow 再发帧：socket 一关帧就写不出去，客户端只看到 EOF。
// 写帧本身不碰读锁，所以"先发、短等、再硬断"是唯一既保帧又不卡死的顺序。
func (c *Client) finishClose() {
	c.closeMu.Lock()
	code, reason := c.closeCode, c.closeReason
	c.closeMu.Unlock()

	if code != 0 {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = c.ws.Close(code, reason)
		}()
		select {
		case <-done:
		case <-time.After(closeFrameWait):
		}
	}
	_ = c.ws.CloseNow()
}

// authLoop 在时限内等第一条 auth 指令，成功返回 nil。
//
// 返回非 nil 时连接已经被关掉（并给了对端关闭码），调用方直接返回即可。
//
// 超时用显式定时器与读并发等，不交给带截止时间的 ctx：
// coder/websocket 的 setupReadTimeout（conn.go:187）在读的 ctx 到期时会顺手
// c.close() 关掉整条连接，而 writeFrame 里有一段
//
//	if c.isClosed() && opcode == opClose { err = nil }
//
// 于是关闭帧既不写、又返回 nil，客户端只拿到 EOF 而不是协议 §4.1 要的 4401。
func (c *Client) authLoop(ctx context.Context, session string) error {
	readCtx := context.Background()

	// hello 是协议 §4.1 时序里的第一条，无条件先发。
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

	// 库不允许并发 Read，所以一次只跑一个读。挂住的读由 Close 唤醒，
	// 因此不能每轮循环都新起一个（会越漏越多）。
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
			c.log.Info("认证超时")
			c.Close(statusAuthFailed, "认证超时")
			return context.DeadlineExceeded

		case r := <-reads:
			if r.err != nil {
				// 客户端正常关闭也走这条，不能记成"认证超时"。
				c.Close(0, "")
				return r.err
			}

			if r.env.Type != CmdAuth {
				// 回 AUTH_REQUIRED 但不断开：协议只要求"拒绝"。
				// 断开的话，把 auth 排在别的消息后面的客户端就永远进不来。
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

// authenticate 验票：错票与库故障对客户端都是 AUTH_FAILED + 4401
// （两者的处置动作相同：重新登录），细节只进日志。
func (c *Client) authenticate(ctx context.Context, token string) (user.User, error) {
	u, err := c.auth.Authenticate(ctx, token)
	if err != nil {
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

// readLoop 读已认证连接上的指令。用 Background 的 ctx：带取消的 ctx 一旦被取消，
// 库会顺带关掉连接；连接的生命周期由 Close 和心跳负责。
//
// 读失败分两种：对端不守协议（回 4400 把原因告诉它），以及对端断开了
// （连接已经没了，发关闭帧没意义，code 传 0）。
func (c *Client) readLoop() {
	readCtx := context.Background()

	for {
		env, data, err := readCommand(readCtx, c.ws)
		if err != nil {
			c.log.Info("读取结束", "err", err)
			if errors.Is(err, errProtocol) {
				c.Close(statusProtocolError, "协议错误")
				return
			}
			c.Close(0, "")
			return
		}
		// 心跳的回执：读到 pong 就把等待唤醒。
		if env.Type == EventPong {
			select {
			case c.pong <- struct{}{}:
			default:
			}
			continue
		}
		// 协议 §4.3 的另一个方向：客户端主动 ping 时服务端回 pong。
		// 它不能落进下面那条"只记日志"的分支 —— 客户端靠它探活，
		// 不回和连接已死对客户端是同一个现象。
		if env.Type == CmdPing {
			if err := c.Send(Envelope{
				Type: EventPong,
				Data: mustJSON(pongEvent{ServerTime: time.Now().UnixMilli()}),
			}); err != nil {
				return
			}
			continue
		}
		// 其余指令的分派留给后续步骤（房间层）。现在只记下来。
		c.log.Info("收到指令", "type", string(env.Type), "data", string(data))
	}
}

// heartbeat 按协议 §4.3 发应用层 ping 消息，等对端的 pong 消息。
//
// 不用库的传输层 Ping：它在超时那一刻会把整条连接关掉（setupWriteTimeout 的
// c.close()），于是"超时 → 发 4408 关闭帧"这条路上的帧根本没地方写；
// 而且它绕过写 goroutine，等于多了一个写者。应用层 ping 走 Send，
// 和其它消息共用一条写路径，超时也只影响这一次等待。
//
// 一次不回就关：此时对端已经 pingEvery+pongWait 没有响应。
// 协议 §4.3 写的是"连续两次未回 pong"，实现取一次。
func (c *Client) heartbeat(ctx context.Context) {
	t := time.NewTicker(c.pingEvery)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-t.C:
			if err := c.Send(Envelope{
				Type: EventPing,
				Data: mustJSON(pingEvent{ServerTime: time.Now().UnixMilli()}),
			}); err != nil {
				return // 连接已经在关了
			}

			select {
			case <-c.pong:
			case <-c.done:
				return
			case <-ctx.Done():
				return
			case <-time.After(c.pongWait):
				c.log.Info("心跳超时")
				c.Close(statusHeartbeatTimeout, "心跳超时")
				return
			}
		}
	}
}

// errProtocol 标记"对端发的东西不守协议"，调用方据此回 4400 而不是静默断开。
var errProtocol = errors.New("协议错误")

// readCommand 读一条消息并解出信封，顺带把原始字节还给调用方（现在只用于日志）。
// 只收文本帧：协议 §2.1 定的是 JSON。
func readCommand(ctx context.Context, c *websocket.Conn) (Envelope, []byte, error) {
	typ, data, err := c.Read(ctx)
	if err != nil {
		return Envelope{}, nil, err
	}
	if typ != websocket.MessageText {
		return Envelope{}, data, fmt.Errorf("%w: 收到非文本帧 %v", errProtocol, typ)
	}

	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, data, fmt.Errorf("%w: 解析信封: %v", errProtocol, err)
	}
	return env, data, nil
}

// mustJSON 编码信封里的 data。编码失败说明塞了个不能编码的类型，是程序 bug。
func mustJSON(payload any) json.RawMessage {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("编码消息体失败（结构体定义有问题）: %v", err))
	}
	return raw
}
