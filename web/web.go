// Package web 提供 §3.3 的调试客户端。
//
// 页面用 embed 编进二进制，不由磁盘 serve：验收时要敲的启动命令是
// `bash scripts/serve.sh --bg`，工作目录不固定，而"文件在、但相对路径算错于是 404"
// 是那种会让人怀疑服务端坏了的假故障。
//
// 必须由 Go serve 而不是双击 HTML 打开：file:// 下发出去的是 `Origin: null`，
// coder/websocket 的默认同源检查会直接 403（协议 §4.1 的握手要求同源）。
package web

import (
	_ "embed"
	"net/http"
)

//go:embed index.html
var page []byte

// Handler 返回调试页的处理器。它只管一个页面，路径由调用方用 pattern 钉住。
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// 调试页要每次刷新都拿到刚改过的那份；缓存住会让你以为服务端没生效。
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(page)
	})
}
