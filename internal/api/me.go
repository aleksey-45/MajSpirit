package api

import "net/http"

// me 处理 GET /api/me。用户已经由中间件取好了，这里不再查库。
func (h *handlers) me(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	writeJSON(w, http.StatusOK, userResponse{ID: u.ID, Username: u.Username})
}
