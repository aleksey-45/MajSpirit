package room

import (
	"crypto/rand"
	"errors"
)

// idAlphabet 刻意去掉了 0/O 与 1/I：房间号是要**口头报出来**的
// （验收、开黑、客服都靠嘴传），这四个字符在口头传递里必然出错。
//
// 长度是 32 —— 256 % 32 == 0，所以取模没有偏置。
const idAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// idLen 是房间号长度。6 位 32 进制 ≈ 10 亿种，撞号概率可以忽略；
// 而 6 个字符正好是一口气能念完、能记住的长度。
const idLen = 6

func newRoomID() (string, error) {
	buf := make([]byte, idLen)
	if _, err := rand.Read(buf); err != nil {
		return "", errors.New("生成房间号失败: " + err.Error())
	}
	for i, b := range buf {
		buf[i] = idAlphabet[int(b)%len(idAlphabet)]
	}
	return string(buf), nil
}
