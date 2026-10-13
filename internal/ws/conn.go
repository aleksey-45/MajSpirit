package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// hello 是连上后服务端发的第一条消息（协议.md §6.1）。
// 第 1 步 sessionId 是假的——只要够随机、能区分连接就够。
type hello struct {
	ProtocolVersion int    `json:"protocolVersion"`
	ServerTime      int64  `json:"serverTime"`
	SessionID       string `json:"sessionId"`
}

// Handler 返回挂在 "GET /ws" 上的 http.Handler。
func Handler() http.Handler {
	return http.HandlerFunc(handleConn)
}

func handleConn(w http.ResponseWriter, r *http.Request) {
	// ① 升级。这一步把 HTTP 连接抢过来变成 WebSocket。
	//    Origin 检查库默认就做（同源才放行，没有 Origin 头的客户端放行），
	//    所以它返回 error 时多半是跨域或不是 WebSocket 请求——直接 return，库已经写过响应了。
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		slog.Info("websocket 升级失败", "err", err)
		return
	}
	defer c.CloseNow() // 兜底：任何路径退出都保证连接被关掉

	// ② 升级之后 r.Context() 不可直接用了——连接已经从 HTTP 服务器手里被抢走，
	//    那个 ctx 会在 handler 返回时被取消，而我们要它活到连接断开。
	//    但也不能拿 context.Background() 当底：那样进程退出（Ctrl+C / SIGTERM）时
	//    这条连接收不到取消，main 里的优雅关闭对它不起作用。
	//    WithoutCancel 正好是这个意思：保留请求 ctx 上的值，剥掉它的取消信号。
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()

	session := newSessionID()

	log := slog.With("session", session)
	log.Info("websocket 连接建立", "remote", r.RemoteAddr)
	defer log.Info("websocket 连接断开")

	// ③ 立刻发 hello。先算 session 再建 log，两边用同一个值 ——
	//    上次分头算了一次，日志和客户端各拿一个，对不上就没法排查。
	if err := writeEvent(ctx, c, EventHello, hello{
		ProtocolVersion: 1,
		ServerTime:      time.Now().UnixMilli(),
		SessionID:       session,
	}); err != nil {
		log.Info("发 hello 失败", "err", err)
		return
	}

	// ④ 读循环。第 1 步什么都不处理，只负责两件事：
	//    1. 阻塞住——handler 一返回，Go 的 http 服务端会认为这个请求结束了；
	//    2. 感知客户端断开（读到 error 就退出）。
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			// 正常断开（浏览器关页面）也会走到这里，不是异常，所以用 Info 不用 Error。
			log.Info("读取结束", "err", err)
			return
		}
		log.Info("收到消息", "type", typ.String(), "data", string(data))
	}
}

// writeEvent 把 payload 包进信封发出去。
//
// 集中在一处的原因：以后会有 game.drew、room.joined 等一长串事件，
// 每个都手写一遍 Envelope{...} 并 wsjson.Write，只要有一处漏了 Data 或写错 Type，
// 就变成一条客户端看不懂的消息。包信封这件事只该有一个出口。
//
// 注意 Envelope.Data 的类型是 json.RawMessage（[]byte 的别名），
// 而 wsjson.Write 会再 marshal 一次 —— payload 会被 marshal 两遍。
// 在这个数据量下无所谓：一条消息几百字节，两遍 marshal 是微秒级。
// 真到要省的时候，做法是自己 json.Marshal(payload) 再塞进 Envelope.Data。
func writeEvent(ctx context.Context, c *websocket.Conn, typ Command, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("编码 %s 的 data: %w", typ, err)
	}
	return wsjson.Write(ctx, c, Envelope{Type: typ, Data: raw})
}
