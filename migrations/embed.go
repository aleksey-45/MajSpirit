// Package migrations 只做一件事：把 migrations/*.sql 编译进二进制。
//
// 为什么必须 embed
//
// Dockerfile 的运行阶段只 COPY 了二进制：
//
//	COPY --from=build /out/majspirit /majspirit
//
// 镜像里没有 SQL 文件。所以不能用 os.ReadFile 去读磁盘——那在本地能跑，
// 进容器就找不到文件，而且是在启动时才炸。
//
// 为什么这个文件必须待在 migrations/ 里
//
// //go:embed 只能引用本包目录及其子目录，不能写 ../。
// 所以想让 embed 覆盖根目录下的 migrations/，.go 文件就得放在这里。
package migrations

import "embed"

// FS 是 migrations/ 下的全部 .sql。
//
// fs.ReadDir(FS, ".") 返回的名字已按文件名排序，而迁移文件名是零填充的
// （0001_、0002_…），所以字典序就是执行顺序——顺序这件事不需要额外处理。
//
//go:embed *.sql
var FS embed.FS
