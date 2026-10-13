package ws

import (
	"errors"
	"sync"

	"github.com/aleksey-45/majspirit/internal/user"
)

// ErrOffline 表示该用户没有在线连接。
var ErrOffline = errors.New("用户不在线")

// Registry 记住"谁连在哪条 Client 上"。
//
// 用途是把消息发给某个用户（阶段四的房间层要按座位推事件），以及连接断开时
// 把它摘掉 —— 摘不干净就会向已关闭的连接发消息。
//
// 同一个用户只保留一条连接：新连接顶掉旧的，旧的以 4403 关闭（协议 §4.6）。
// 这条决定写在协议里，改实现要一起改。
type Registry struct {
	mu      sync.Mutex
	clients map[int64]*Client
}

func NewRegistry() *Registry {
	return &Registry{clients: make(map[int64]*Client)}
}

// Add 登记一条连接，返回被顶掉的旧连接（没有则 nil）。
//
// 旧连接是返回给调用方、而不是在这里关：Client.Close 可能等写 goroutine，
// 在锁里等等于把注册表的锁交给网络，所有用户的消息都被堵住。
func (r *Registry) Add(u user.User, c *Client) *Client {
	r.mu.Lock()
	defer r.mu.Unlock()

	old := r.clients[u.ID]
	r.clients[u.ID] = c
	return old
}

// Remove 摘掉一条连接。
//
// 只在"登记的还是它"时删。被新连接顶掉的旧连接随后也会走到这里，
// 那时 map 里已经是新连接 —— 照删就会把新连接摘掉。这个错不报错，
// 只表现为"刚连上就收不到消息"，很难查。
func (r *Registry) Remove(userID int64, c *Client) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.clients[userID] == c {
		delete(r.clients, userID)
	}
}

// Send 给某个用户发一条消息；不在线返回 ErrOffline。
//
// 拿锁只为取出指针，发送在锁外做：Client.Send 可能阻塞（对端消费不过来）。
func (r *Registry) Send(userID int64, env Envelope) error {
	r.mu.Lock()
	c, ok := r.clients[userID]
	r.mu.Unlock()

	if !ok {
		return ErrOffline
	}
	return c.Send(env)
}

// Len 是当前登记数，给测试和诊断用。
func (r *Registry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.clients)
}
