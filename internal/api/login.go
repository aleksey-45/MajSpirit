package api

import "net/http"

// login 处理 POST /api/login。
func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tok, err := h.svc.Login(r.Context(), in.Username, in.Password)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{Token: tok})
}
