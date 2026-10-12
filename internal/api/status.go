package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/aleksey-45/majspirit/internal/auth"
	"github.com/aleksey-45/majspirit/internal/user"
)

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
