package ws

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aleksey-45/majspirit/internal/room"
)

// TestAuthTimeoutCloseCodeOnWire 用裸 TCP 看字节，确认超时路径真的把 4401 发了出去。
//
// 为什么不用 coder/websocket 的客户端测这件事：踩过一次——
// 库在读的 ctx 到期时会顺手 c.close() 把整条连接关掉，而 writeFrame 里
//
//	if c.isClosed() && opcode == opClose { err = nil }
//
// 会让随后的 Close(4401) 既不写帧、又返回 nil。用库的客户端去测，
// 拿到的是 EOF，看不出"帧没写"还是"帧写了但被截断"；只有裸字节能证明。
// （修复办法见 handler.go 里 authenticate 的注释：超时用显式定时器，别交给读的 ctx。）
func TestAuthTimeoutCloseCodeOnWire(t *testing.T) {
	registry := NewRegistry()
	hub := room.NewHub(registry.Send, nil)
	defer hub.Close()

	h := NewHandler(&fakeAuth{}, registry, hub)
	h.timeout = 100 * time.Millisecond

	srv := httptest.NewServer(h)
	defer srv.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// 手写握手：只为了拿到一条干净的 TCP 连接，然后逐帧读原始字节。
	addr := strings.TrimPrefix(srv.URL, "http://")
	var key [16]byte
	rand.Read(key[:])
	fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\n"+
		"Connection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n"+
		"Origin: http://%s\r\n\r\n",
		addr, base64.StdEncoding.EncodeToString(key[:]), addr)

	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("握手失败: %v", err)
		}
		if strings.TrimSpace(line) == "" {
			break // 响应头结束
		}
	}

	// 什么都不发（不认证），等服务端到点关。前两帧应该是 hello 和关闭帧。
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))

	var opcodes []byte
	var closeCode uint16
	for i := 0; i < 2; i++ {
		var hdr [2]byte
		if _, err := br.Read(hdr[:]); err != nil {
			t.Fatalf("第 %d 帧读头失败: %v", i, err)
		}
		length := int(hdr[1] & 0x7f)
		payload := make([]byte, length)
		if _, err := br.Read(payload); err != nil {
			t.Fatalf("第 %d 帧读体失败: %v", i, err)
		}

		op := hdr[0] & 0x0f
		opcodes = append(opcodes, op)
		if op == 0x8 { // 关闭帧
			if length < 2 {
				t.Fatalf("关闭帧太短，没有关闭码（len=%d）", length)
			}
			closeCode = uint16(payload[0])<<8 | uint16(payload[1])
		}
	}

	if len(opcodes) != 2 || opcodes[0] != 0x1 || opcodes[1] != 0x8 {
		t.Fatalf("帧序列 = %#v, 期望 [0x1(hello) 0x8(关闭)]", opcodes)
	}
	if closeCode != uint16(statusAuthFailed) {
		t.Errorf("关闭码 = %d, 期望 %d", closeCode, statusAuthFailed)
	}
}
