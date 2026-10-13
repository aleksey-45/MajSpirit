package ws

import (
	"crypto/rand"
	"encoding/hex"
)

// newSessionID 返回 16 字节随机数的 hex，用于在日志里区分连接。
func newSessionID() string {
	var b [16]byte
	rand.Read(b[:]) // Go 1.24+ 起 crypto/rand.Read 不返回错误（失败即崩溃）
	return hex.EncodeToString(b[:])
}
