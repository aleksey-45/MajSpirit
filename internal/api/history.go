package api

import (
	"net/http"
	"strconv"
)

// history 处理 GET /api/history —— 我的过往牌局与顺位（协议 §3.5）。
func (h *handlers) history(w http.ResponseWriter, r *http.Request) {
	cursor, ok := cursorOf(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "cursor 要是上一页返回的 nextCursor")
		return
	}

	// 取的是中间件塞进 context 的用户，不是查询参数：能查谁由票决定，
	// 不由请求体决定。想查别人请走 /api/games/{id}（那边是公开的）。
	page, err := h.hist.History(r.Context(), userFrom(r.Context()).ID, cursor)
	if err != nil {
		writeHistoryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// cursorOf 解析 ?cursor=。缺省是 0，表示第一页。
//
// 游标就是上一页最后一条的 gameId（一个整数），不是不透明串也不是时间戳：
// 见 store.GameHistory 里为什么不用 started_at。
func cursorOf(r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}
