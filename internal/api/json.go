package api

import (
	"encoding/json"
	"net/http"
)

const maxBodyBytes = 64 << 10 // 64 KiB，够任何正常请求

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	// 三件事缺一不可：
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes) // ① 限长，否则 1GB 的 JSON 吃掉内存
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	// ③ 客户端带多余字段就拒。少了它，请求体里塞个 "id":1 会被**静默接受**，
	// 客户端就能试探着往结构体上靠。TestRejectsUnknownJSONFields 盯着这一条。
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}
