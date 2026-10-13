package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/aleksey-45/majspirit/internal/auth"
	"github.com/aleksey-45/majspirit/internal/history"
	"github.com/aleksey-45/majspirit/internal/user"
)

// writeHistoryError 把历史用例层的错误翻成 HTTP。
//
// 兜底直接复用 writeServiceError：500 的口径与"细节只进日志"只该有一处，
// 抄第二份迟早会漂移。
func writeHistoryError(w http.ResponseWriter, err error) {
	if errors.Is(err, history.ErrNoGame) {
		writeError(w, http.StatusNotFound, "牌局不存在")
		return
	}
	writeServiceError(w, err)
}

// writeServiceError 把业务错误翻成 HTTP。这是错误链的终点：再往上没有能
// 做决定的层了，所以 500 也在这里兜。
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, user.ErrWeakPassword), errors.Is(err, user.ErrInvalidUsername):
		// 400 的消息是写给用户的（"首尾不能有空格"），可以原样给
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, user.ErrUsernameTaken):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, user.ErrBadCredentials), errors.Is(err, auth.ErrInvalidToken):
		// 401 给固定文案：不透露"是用户名错还是密码错"
		writeError(w, http.StatusUnauthorized, "用户名或密码不正确")
	default:
		slog.Error("接口内部错误", "err", err) // 细节只进日志
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
	}
}
