package api

import "net/http"

// logout 处理 POST /api/logout。票已经由中间件验过了。
func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	// Logout 要的是**原始串**（拿去记进黑名单），不是 userID ——
	// 中间件把两个都放进 context 就是为了这个。
	if err := h.svc.Logout(r.Context(), tokenFrom(r.Context())); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
