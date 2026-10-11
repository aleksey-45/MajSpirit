// Package migrations 把 migrations/*.sql 编译进二进制。
//
// //go:embed 只能引用本包目录及子目录（不能写 ../），所以这个 .go 文件必须待在
// migrations/ 里。为什么不改成运行时读磁盘：见 Dockerfile 运行阶段那行 COPY。
package migrations

import "embed"

// FS 是 migrations/ 下的全部 .sql。名字已按文件名排序（0001_、0002_…），
// 字典序就是执行顺序。
//
//go:embed *.sql
var FS embed.FS
