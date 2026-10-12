package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/aleksey-45/majspirit/internal/auth"
	"github.com/aleksey-45/majspirit/internal/user"
)

type ctxKey int

const (
	ctxUser ctxKey = iota
	ctxToken
)

// requireAuth 从 Authorization: Bearer <token> 取票、验票，把结果塞进 context。
//
// 签名用 http.Handler → http.Handler 而不是 HandlerFunc → HandlerFunc：
// 前者是 Go 中间件的通用契约，以后加日志或 panic 恢复能直接串起来。
func (h *handlers) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := bearerToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "缺少令牌")
			return
		}

		u, err := h.svc.Authenticate(r.Context(), tok)
		if err != nil {
			// 这里的 401 意思是"票不管用"，不是"账号密码错了"。消息必须和
			// /api/login 那句区分开——前端才知道该清掉本地票、跳回登录页，
			// 而不是把用户按在登录页上让他重新输密码。
			if errors.Is(err, auth.ErrInvalidToken) {
				writeError(w, http.StatusUnauthorized, "登录已失效，请重新登入")
				return
			}
			writeServiceError(w, err) // 其它错误（比如库挂了）→ 500
			return
		}

		// 塞两个：/api/me 要 user，/api/logout 要**原始串**（拿去进黑名单）。
		ctx := context.WithValue(r.Context(), ctxUser, u)
		ctx = context.WithValue(ctx, ctxToken, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerToken 取出 Authorization 头里 Bearer 后面那串。
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	v := r.Header.Get("Authorization")
	// 大小写不敏感：规范里 scheme 是 case-insensitive 的，客户端可能发 "bearer"。
	if len(v) <= len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", false
	}
	return v[len(prefix):], true
}

// userFrom 取出中间件放进来的用户。不在中间件后面调用时返回零值。
func userFrom(ctx context.Context) user.User {
	u, _ := ctx.Value(ctxUser).(user.User)
	return u
}

// tokenFrom 取出中间件放进来的原始令牌。
func tokenFrom(ctx context.Context) string {
	tok, _ := ctx.Value(ctxToken).(string)
	return tok
}
