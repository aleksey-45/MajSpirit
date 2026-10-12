package api

import (
	"net/http"
)

// register 处理 POST /api/register。
func (h *handlers) register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	u, err := h.svc.Register(r.Context(), in.Username, in.Password)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, userResponse{ID: u.ID, Username: u.Username})
}
