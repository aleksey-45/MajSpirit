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
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed index.html
var page []byte

//go:embed app
var appDir embed.FS

//go:embed public
var publicDir embed.FS

// appFS / tilesFS 是两条静态资源的根，各去掉一层目录前缀。
// 牌面的根钉在 public/tiles 上而不是 public：源文件（SOURCE.md / LICENSE.md）
// 没有发出去的理由，少一层就少暴露一层。
// fs.Sub 只可能因为「embed 的目录名写错」失败，那是程序 bug 不是运行时故障，
// 所以直接 panic —— 与 mahjong 的 MustTile 同一个判据。
var (
	appFS   = subDir(appDir, "app")
	tilesFS = subDir(publicDir, "public/tiles")
)

func subDir(f embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic("web: embed 里没有 " + dir + " 目录：" + err.Error())
	}
	return sub
}

// Handler 返回调试页的处理器。它只管一个页面，路径由调用方用 pattern 钉住。
func Handler() http.Handler {
	return noStore(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	}))
}

// AppHandler 提供 /app/ 下的客户端脚本（web/app/*.js），挂在 `GET /app/` 上。
func AppHandler() http.Handler {
	return noStore(http.StripPrefix("/app/", http.FileServer(http.FS(appFS))))
}

// TilesHandler 提供 /tiles/ 下的牌面 SVG（web/public/tiles/），挂在 `GET /tiles/` 上。
//
// 只发 .svg：那个目录里还躺着 SOURCE.md 与 LICENSE.md（资源的出处与许可记录），
// 牌面路由没有理由把它们也发出去。牌面是牌桌页唯一的图片资源，
// 这条路 404 的话整张桌子都是空框。
func TilesHandler() http.Handler {
	svgs := http.FileServer(http.FS(tilesFS))
	return noStore(http.StripPrefix("/tiles/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ".svg") {
			http.NotFound(w, r)
			return
		}
		svgs.ServeHTTP(w, r)
	})))
}

// noStore 让每次刷新都拿到刚改过的那份。
//
// 不只是 HTML：脚本和牌面也一样。embed 里的文件时间戳是零值，FileServer 因此
// 不发 Last-Modified/ETag，浏览器可以据此启发式缓存 —— 开发期最难查的就是
// 「我改了，但页面没变」。
func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
