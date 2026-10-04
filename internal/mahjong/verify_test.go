package mahjong

import (
	"crypto/sha256"
	"encoding/binary"
	"slices"
	"testing"
)

// verify_test.go —— 牌山可验证性。协议见 docs/牌山可验证性.md。
//
// **期望值全部独立算**：这个文件不复用 verify.go 里的任何辅助函数，
// 而是照文档 §3.1 的字节表用字面拼接重新实现一遍公式。
//
// 为什么必须这样：调实现去比对实现是同义反复——**多套一层哈希、少写一段
// 域分隔串、把 roundIndex 截成 1 字节，这些错误它一个都抓不到。**
// 而这套机制的全部意义就是"第三方能独立复算"，所以测试本身就该是那个第三方。

// ── 独立的规格实现（照抄文档，不看 verify.go）────────────────────────────

// specDomain 照抄文档 §3.3 的域分隔串。
const specDomain = "majspirit/wall/v1"

// specInt64 照文档：整数一律 8 字节 big-endian，**定宽**。
func specInt64(x int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(x))
	return b
}

// specCommitInput 照文档 §3.1 拼承诺值的哈希输入。
func specCommitInput(gameID int64, serverSeed [32]byte) []byte {
	buf := append([]byte(specDomain), specInt64(gameID)...)
	return append(buf, serverSeed[:]...)
}

// specDeriveInput 照文档 §3.1 拼派生种子的哈希输入。
func specDeriveInput(gameID int64, roundIndex int, serverSeed [32]byte,
	clientSeeds [4][32]byte) []byte {
	buf := append([]byte(specDomain), specInt64(gameID)...)
	buf = append(buf, specInt64(int64(roundIndex))...)
	buf = append(buf, serverSeed[:]...)
	for _, s := range clientSeeds {
		buf = append(buf, s[:]...)
	}
	return buf
}

func specCommit(gameID int64, serverSeed [32]byte) [32]byte {
	return sha256.Sum256(specCommitInput(gameID, serverSeed))
}

func specDeriveSeed(gameID int64, roundIndex int, serverSeed [32]byte,
	clientSeeds [4][32]byte) [32]byte {
	return sha256.Sum256(specDeriveInput(gameID, roundIndex, serverSeed, clientSeeds))
}

// ── 夹具 ────────────────────────────────────────────────────────────────

func seedFrom(b ...byte) [32]byte {
	var s [32]byte
	copy(s[:], b)
	return s
}

func seedsFrom(b ...byte) [4][32]byte {
	var cs [4][32]byte
	for i, x := range b {
		cs[i][0] = x
	}
	return cs
}

// ── §3.1 字节表：长度是契约的一部分 ────────────────────────────────────

// 这个测试只钉**文档里的字节数**，不碰实现内部的结构。
// 实现有没有照做，由下面的公式比对负责。
func TestWallVerifyDocumentedInputLayout(t *testing.T) {
	seed := seedFrom(1, 2, 3)
	cs := seedsFrom(0xA0, 0xA1, 0xA2, 0xA3)

	// 域分隔串(17) ‖ gameID(8) ‖ serverSeed(32)
	if got, want := len(specCommitInput(42, seed)), 57; got != want {
		t.Errorf("文档的承诺输入 = %d 字节，字节表写的是 %d", got, want)
	}
	// 域分隔串(17) ‖ gameID(8) ‖ roundIndex(8) ‖ serverSeed(32) ‖ 4×clientSeed(128)
	if got, want := len(specDeriveInput(42, 7, seed, cs)), 193; got != want {
		t.Errorf("文档的派生输入 = %d 字节，字节表写的是 %d", got, want)
	}

	// 域分隔串本身也要钉住——改它等于改协议版本
	if got, want := len(specDomain), 17; got != want {
		t.Errorf("域分隔串 %q 是 %d 字节，字节表写的是 %d", specDomain, got, want)
	}
}

// ── 实现必须等于文档的公式 ★ 这条抓双重哈希 ────────────────────────────

func TestWallVerifyMatchesDocumentedFormula(t *testing.T) {
	seed := seedFrom(0x11, 0x22, 0x33)
	cs := seedsFrom(1, 2, 3, 4)

	t.Run("Commit", func(t *testing.T) {
		for _, gid := range []int64{0, 1, -1, 20261004} {
			got, want := Commit(gid, seed), specCommit(gid, seed)
			if got != want {
				t.Errorf("Commit(%d) 与文档公式不一致：\n实现 %x\n文档 %x\n"+
					"（最常见的原因是外面多套了一层 sha256.Sum256）", gid, got, want)
			}
		}
	})

	t.Run("DeriveSeed", func(t *testing.T) {
		for _, gid := range []int64{0, 20261004} {
			for _, rnd := range []int{0, 1, 255, 256} {
				got, want := DeriveSeed(gid, rnd, seed, cs), specDeriveSeed(gid, rnd, seed, cs)
				if got != want {
					t.Errorf("DeriveSeed(%d, %d) 与文档公式不一致：\n实现 %x\n文档 %x",
						gid, rnd, got, want)
				}
			}
		}
	})
}

// ── 承诺自洽（§11） ─────────────────────────────────────────────────────

func TestWallVerifyCommitIsStable(t *testing.T) {
	seed := seedFrom(9, 9, 9)
	if a, b := Commit(7, seed), Commit(7, seed); a != b {
		t.Errorf("同一个输入两次 Commit 结果不同：%x vs %x", a, b)
	}
}

func TestWallVerifyCommitChangesWithOneByte(t *testing.T) {
	const gid = 7
	base := seedFrom(0, 0, 0, 0)

	// 改 serverSeed 的任意一个字节，结果都必须变
	for i := 0; i < 32; i++ {
		other := base
		other[i] = 1
		if Commit(gid, base) == Commit(gid, other) {
			t.Errorf("改 serverSeed 第 %d 字节，承诺值没变", i)
		}
	}

	// 改 gameID 也必须变
	if Commit(gid, base) == Commit(gid+1, base) {
		t.Error("改 gameID，承诺值没变——承诺没有绑定牌局")
	}
}

// ── 派生顺序敏感（§11） ─────────────────────────────────────────────────

func TestWallVerifySeedOrderMatters(t *testing.T) {
	const (
		gid = 20261004
		rnd = 0
	)
	seed := seedFrom(0x5A)
	base := seedsFrom(1, 2, 3, 4)

	t.Run("交换任意两人的种子", func(t *testing.T) {
		want := DeriveSeed(gid, rnd, seed, base)
		for i := 0; i < 4; i++ {
			for j := i + 1; j < 4; j++ {
				swapped := base
				swapped[i], swapped[j] = swapped[j], swapped[i]
				if DeriveSeed(gid, rnd, seed, swapped) == want {
					t.Errorf("交换座位 %d 与 %d 的 clientSeed，派生结果没变——"+
						"clientSeeds 被当成集合处理了", i, j)
				}
			}
		}
	})

	t.Run("循环移位", func(t *testing.T) {
		want := DeriveSeed(gid, rnd, seed, base)
		shifted := [4][32]byte{base[1], base[2], base[3], base[0]}
		if DeriveSeed(gid, rnd, seed, shifted) == want {
			t.Error("四个种子整体移位后派生结果没变")
		}
	})
}

// ── 派生绑定上下文（§11） ───────────────────────────────────────────────

func TestWallVerifySeedBindsContext(t *testing.T) {
	seed := seedFrom(0x77)
	cs := seedsFrom(1, 2, 3, 4)
	base := DeriveSeed(100, 5, seed, cs)

	if DeriveSeed(101, 5, seed, cs) == base {
		t.Error("改 gameID，派生结果没变")
	}
	if DeriveSeed(100, 6, seed, cs) == base {
		t.Error("改 roundIndex，派生结果没变")
	}
	if DeriveSeed(100, 5, seedFrom(0x78), cs) == base {
		t.Error("改 serverSeed，派生结果没变")
	}

	cs2 := cs
	cs2[3][31] = 1
	if DeriveSeed(100, 5, seed, cs2) == base {
		t.Error("改最后一个 clientSeed 的最后一个字节，派生结果没变")
	}
}

// roundIndex 必须定宽。写成 byte() 会让 0 / 256 / 512 撞在一起。
func TestWallVerifyRoundIndexIsNotTruncated(t *testing.T) {
	seed := seedFrom(0x33)
	cs := seedsFrom(1, 2, 3, 4)

	seen := map[[32]byte]int{}
	for _, rnd := range []int{0, 1, 255, 256, 257, 512, 65536} {
		got := DeriveSeed(42, rnd, seed, cs)
		if prev, dup := seen[got]; dup {
			t.Errorf("roundIndex %d 与 %d 派生出同一个种子 %x —— roundIndex 被截断了",
				prev, rnd, got)
		}
		seen[got] = rnd
	}
}

// ── 重放可复现（§11）★ 模拟玩家第 9 节的第 4 步 ────────────────────────

func TestWallVerifyReplayReproducesWall(t *testing.T) {
	const gid = 20261004
	serverSeed := seedFrom(0xDE, 0xAD, 0xBE, 0xEF)
	clientSeeds := seedsFrom(0x11, 0x22, 0x33, 0x44)

	// ── 服务器侧：① 开赛前承诺、③ 每小局派生并洗牌
	commitment := Commit(gid, serverSeed)
	original := NewWallFromSeed(DeriveSeed(gid, 3, serverSeed, clientSeeds))

	// ── 玩家侧：拿到 commitment / server_seed / client_seeds / tiles 之后自己复算
	t.Run("第 2 步 承诺比对", func(t *testing.T) {
		if specCommit(gid, serverSeed) != commitment {
			t.Error("用文档公式重算的承诺与公布的不一致——种子被事后替换了")
		}
	})

	t.Run("第 4 步 重放逐张一致", func(t *testing.T) {
		replayed := NewWallFromSeed(specDeriveSeed(gid, 3, serverSeed, clientSeeds))
		if !slices.Equal(original.Tiles(), replayed.Tiles()) {
			t.Error("用自己的 finalSeed 重放，牌山与存档不一致")
		}
		if len(replayed.Tiles()) != NumTiles {
			t.Errorf("重放出 %d 张，期望 %d 张", len(replayed.Tiles()), NumTiles)
		}
	})

	t.Run("第 4 步 指纹也一致", func(t *testing.T) {
		replayed := NewWallFromSeed(specDeriveSeed(gid, 3, serverSeed, clientSeeds))
		if WallHash(original) != sha256.Sum256(replayed.Bytes()) {
			t.Error("WallHash 与独立算出的字节摘要不一致")
		}
	})

	// 同一局不同小局的牌山必须不同（roundIndex 起作用）
	t.Run("不同小局牌山不同", func(t *testing.T) {
		other := NewWallFromSeed(DeriveSeed(gid, 4, serverSeed, clientSeeds))
		if slices.Equal(original.Tiles(), other.Tiles()) {
			t.Error("roundIndex 只差 1，牌山却完全相同")
		}
	})
}

// ── WallHash ────────────────────────────────────────────────────────────

func TestWallVerifyWallHashIsOverRawBytes(t *testing.T) {
	w := NewWallFromSeed(seedFrom(1, 2, 3))
	if WallHash(w) != sha256.Sum256(w.Bytes()) {
		t.Error("WallHash 不是 SHA256(w.Bytes())")
	}

	// 反例：同花色组合相同但顺序不同的两副牌山，指纹必须不同。
	// 用 FormatTiles 做哈希输入就会让它们撞上。
	a := []Tile{0, 1, 2}
	b := []Tile{2, 1, 0}
	if FormatTiles(a) != FormatTiles(b) {
		t.Fatal("夹具失效：这两者的紧凑记法应该相同")
	}
	if sha256.Sum256([]byte{0, 1, 2}) == sha256.Sum256([]byte{2, 1, 0}) {
		t.Error("顺序不同的牌山字节摘要相同——哈希输入丢掉了顺序")
	}
}
