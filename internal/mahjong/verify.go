package mahjong

import (
	"crypto/sha256"
	"encoding/binary"
)

// wallDomain 是本协议的**域分隔串**（domain separation），见 docs/牌山可验证性.md §3.3。
//
// 作用：让本协议的哈希输入与程序里任何其他哈希输入**不相交**。没有它，
// 一个精心构造的输入可能让两处哈希撞上（跨协议攻击）；将来协议升到 v2 时，
// 前缀一变，新旧记录也不会混淆。
//
// ⚠️ **改这个串等于改协议版本**，必须同步 docs/牌山可验证性.md §3.3——
// 否则第三方照文档复算会得到不同结果，进而误判"服务器作弊"。
const wallDomain = "majspirit/wall/v1"

// int64ToBytes 把一个 int64 转成 8 字节的 big-endian。
//
// 整数字段一律定宽 8 字节：哈希输入是一串**没有结构**的字节，读的人必须能
// 唯一地切回各字段。定宽是最简单的切法，顺带也避免了截断——写成
// `byte(roundIndex)` 的话 0 和 256 会派生出完全相同的种子。
func int64ToBytes(x int64) []byte {
	res := make([]byte, 8)
	binary.BigEndian.PutUint64(res, uint64(x))
	return res
}

// Commit 返回承诺值。开赛前公布它，牌局结束后再公布 serverSeed。
//
// 哈希输入 = 域分隔串(17) ‖ gameID(8) ‖ serverSeed(32)，共 **57 字节**。
//
// 为什么这样能防作弊：先公布哈希等于把服务器**锁死**在一个种子上——
// SHA256 不可逆，公布了承诺也推不出种子；而牌局结束后公布种子，任何人都能
// 复算这个哈希与当初的承诺比对，**服务器没可能在赛后换一个对自己有利的种子**。
//
// 为什么必须混进 gameID：否则同一个种子在不同牌局会产生同一个承诺，
// 承诺就不再绑定具体牌局。
func Commit(gameID int64, serverSeed [32]byte) [32]byte {
	buf := make([]byte, 0, len(wallDomain)+8+32)
	buf = append(buf, wallDomain...)
	buf = append(buf, int64ToBytes(gameID)...)
	buf = append(buf, serverSeed[:]...)
	return sha256.Sum256(buf)
}

// DeriveSeed 把服务器种子与四位玩家的种子按座位顺序混合成最终种子。
//
// 哈希输入 = 域分隔串(17) ‖ gameID(8) ‖ roundIndex(8)
//
//	‖ serverSeed(32) ‖ clientSeed₀(32) ‖ … ‖ clientSeed₃(32)，共 **193 字节**。
//
// ⚠️ **clientSeeds 必须按座位顺序处理，不能当集合。** 当成集合的话，
// 交换两个人的种子结果不变——那就失去了"每个玩家都参与"的意义。
func DeriveSeed(gameID int64, roundIndex int, serverSeed [32]byte,
	clientSeeds [4][32]byte) [32]byte {
	buf := make([]byte, 0, len(wallDomain)+8+8+32+4*32)
	buf = append(buf, wallDomain...)
	buf = append(buf, int64ToBytes(gameID)...)
	buf = append(buf, int64ToBytes(int64(roundIndex))...)
	buf = append(buf, serverSeed[:]...)
	for _, s := range clientSeeds {
		buf = append(buf, s[:]...)
	}
	return sha256.Sum256(buf)
}

// WallHash 返回牌山指纹，与数据库里记录的值比对即可核验整局。
//
// 必须是 `SHA256(w.Bytes())`——**不能用 `FormatTiles`**：那个按花色分组排序，
// 会丢掉摸牌顺序，让所有同花色组合的牌山共用同一个指纹。
func WallHash(w *Wall) [32]byte {
	return sha256.Sum256(w.Bytes())
}
