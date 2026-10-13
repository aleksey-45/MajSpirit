package ws

import (
	"crypto/rand"
	"encoding/hex"
)

// newSessionID 返回 16 字节随机数的 hex，用于在日志里区分连接。
//
// 用 crypto/rand 而不是 math/rand：sessionId 会出现在日志和将来的重连逻辑里，
// 可预测的 ID 会给"猜别人的连接"留口子。
func newSessionID() string {
	var b [16]byte
	rand.Read(b[:]) // crypto/rand 在 Go 1.24+ 保证不返回错误（失败即崩溃），所以这里不判
	return hex.EncodeToString(b[:])
}
