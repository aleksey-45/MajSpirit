package auth

import (
	"sync"
	"time"
)

// Revoker 是"已注销令牌"的黑名单。
//
// # 为什么需要它
//
// JWT 是无状态的：服务端签出去就"忘"了，没有任何办法让它失效。
// 所以严格说，JWT 没有真正的登出——能做的只有记下"这张票作废了"，
// 每次校验时查一下。
//
// 三条路，按代价递增（§2.2 的决策）：
//
//	① 前端把 token 丢掉        最省事，但它到期前仍然有效，等于没登出
//	② 内存黑名单（本文件）      单机够用，重启丢失
//	③ Redis 黑名单             多实例、可持久化
//
// 本项目选 ②。
//
// # 这个实现的三条局限——刻意的，不是遗漏
//
//  1. 进程重启后黑名单清空：那些 token 会"复活"到自然过期。
//  2. 多实例不共享：A 实例注销的 token，在 B 实例上仍然有效。
//  3. 只增不减：靠 Cleanup 定期修剪；不调用它也不会错，但会一直占内存。
//
// 修 1 和 2 都得上 Redis。在一个单机开发的项目里引 Redis 是过度设计，
// 但必须把局限写下来——而不是假装登出已经解决了。
type Revoker struct {
	mu    sync.Mutex
	until map[string]time.Time
}

// NewRevoker 建一个空的黑名单。
func NewRevoker() *Revoker {
	return &Revoker{until: make(map[string]time.Time)}
}

// Revoke 把 token 记进黑名单，keep 是它最多还能活多久。
//
// 收 TTL 而不是精确的过期时刻：调用方（Logout）手上只有 token 字符串，
// 要拿精确过期时刻得再解析一次。用 TTL 是个上界——条目留得比必要久一点，
// 但绝不会短，所以不会误放行。代价只是多占一点内存。
func (r *Revoker) Revoke(token string, keep time.Duration) {
	if token == "" || keep <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.until[token] = time.Now().Add(keep)
}

// IsRevoked 报告 token 是否已被注销。
//
// 顺手删掉自己那条已经过期的记录——它本来就已经失效了，留着没意义。
func (r *Revoker) IsRevoked(token string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	exp, ok := r.until[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(r.until, token)
		return false
	}
	return true
}

// Cleanup 删掉所有已过期的条目，返回清掉的条数。
//
// 由调用方定期调（比如一小时一次）。不调用也能工作——IsRevoked 会顺手
// 清理它自己查到的那条——但那些再也没人查的条目会一直留着。
func (r *Revoker) Cleanup() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	n := 0
	for tok, exp := range r.until {
		if now.After(exp) {
			delete(r.until, tok)
			n++
		}
	}
	return n
}

// Len 返回当前条目数（测试与观测用）。
func (r *Revoker) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.until)
}
