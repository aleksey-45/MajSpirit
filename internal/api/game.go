package api

import (
	"net/http"
	"strconv"
)

// game 处理 GET /api/games/{gameId} —— 一局的可读存档（协议 §3.6）。
//
// 这里不做参与者判断：已结束的对局对任何登录用户公开。那是个决定（不是疏忽），
// 理由与以后要改的地方写在 router.go 注册这两条路由处 —— 同一个结论只写一处。
//
// 进行中的对局也能查，但它只有 games + game_seats 两行：明细行要牌局结束才写，
// 所以"别家手牌"不是被过滤掉的，是根本不存在。
func (h *handlers) game(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("gameId"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "gameId 要是正整数")
		return
	}

	g, err := h.hist.Game(r.Context(), id)
	if err != nil {
		writeHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}
