package ws

import (
	"bytes"
	"encoding/json"
	"math/big"
	"testing"
)

// TestEnvelopeWireFormat 盯住线上形状：外层是 {"type":…, "data":…}。
//
// 防的是两种具体的坏法：
//   - Data 用了普通 []byte（不是 json.RawMessage）→ 会被编码成 base64 字符串，
//     客户端拿到一坨看不懂的东西，而且这里不测就没人喊；
//   - Seq 忘了 omitempty → 每条指令都白带一个 "seq":0，
//     客户端无法区分"没有序号"和"序号是 0"。
func TestEnvelopeWireFormat(t *testing.T) {
	var session struct {
		ID   string `json:"sessionId"`
		Port int    `json:"port"`
	}
	session.ID = "abc123"
	session.Port = 8080

	raw, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("构造 data 失败: %v", err)
	}

	got, err := json.Marshal(Envelope{Type: EventHello, Data: raw})
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("解出来不是对象: %v（原文 %s）", err, got)
	}
	if _, ok := m["seq"]; ok {
		t.Errorf("Seq 为零值时不该出现 seq 字段: %s", got)
	}

	var typ string
	if err := json.Unmarshal(m["type"], &typ); err != nil {
		t.Fatalf("type 不是字符串: %v", err)
	}
	if typ != string(EventHello) {
		t.Errorf("type = %q, 期望 %q", typ, EventHello)
	}

	// data 必须是对象本身，不能是被 base64 过的字符串。
	var back struct {
		ID   string `json:"sessionId"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal(m["data"], &back); err != nil {
		t.Fatalf("data 不是对象（普通 []byte 会被 base64 编码，典型症状就是这里失败）: %v（data=%s）", err, m["data"])
	}
	if back.ID != "abc123" || back.Port != 8080 {
		t.Errorf("data 往返后变了: %+v", back)
	}
}

// TestEnvelopeSeqSet 是上一条的对照：序号非零时必须写出来，而且是数字不是字符串。
// 只测"零值不写"会漏掉"永远不写"这种实现。
func TestEnvelopeSeqSet(t *testing.T) {
	got, err := json.Marshal(Envelope{Type: EventHello, Seq: 17})
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("解出来不是对象: %v", err)
	}
	raw, ok := m["seq"]
	if !ok {
		t.Fatalf("Seq 为 17 时必须写出 seq 字段: %s", got)
	}

	var seq json.Number
	if err := json.Unmarshal(raw, &seq); err != nil {
		t.Fatalf("seq 不是数字: %v（原文 %s）", err, raw)
	}
	if seq.String() != "17" {
		t.Errorf("seq = %s, 期望 17", seq)
	}
}

// TestEnvelopeRoundTrip 往返一致：写出去再解回来，Type 和 Data 字节相同。
func TestEnvelopeRoundTrip(t *testing.T) {
	payload := json.RawMessage(`{"seat":2,"tile":"5p"}`)
	in := Envelope{Type: "game.discarded", Data: payload, Seq: 17}

	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}

	var out Envelope
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if out.Type != in.Type {
		t.Errorf("Type = %q, 期望 %q", out.Type, in.Type)
	}
	if !bytes.Equal(out.Data, payload) {
		t.Errorf("Data = %s, 期望 %s", out.Data, payload)
	}
	if out.Seq != in.Seq {
		t.Errorf("Seq = %d, 期望 %d", out.Seq, in.Seq)
	}
}

// TestEnvelopeKeepsLargeInteger 是这个设计的存在理由。
//
// JSON 没有整数类型，解到 any / map[string]any 里一律变 float64，
// 于是超过 2^53 的整数会被静默改写（9007199254740993 → 9007199254740992）。
// Envelope.Data 用 json.RawMessage 就是为了让这段字节原样留着，
// 等知道 Type 之后再解进 int64 —— 这条测试会挂掉任何"先解成 map 再重编码"的实现。
//
// 项目里暂时没有这么大的 ID（gameId 是自增序列，点数上限 64000），
// 但 JSON 里超过 2^53 的整数是没法表示的事实，所以这条守的是编码层的性质，
// 不是某个字段当前的取值范围。
func TestEnvelopeKeepsLargeInteger(t *testing.T) {
	// 注意别把这个变量命名成 big —— 会把 math/big 包遮住。
	const bigVal = "9007199254740993" // 2^53 + 1，float64 表示不了

	wire := []byte(`{"type":"game.roundEnd","data":{"gameId":` + bigVal + `}}`)

	var env Envelope
	if err := json.Unmarshal(wire, &env); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}

	var data struct {
		GameID int64 `json:"gameId"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("解 data 失败: %v", err)
	}

	want, ok := new(big.Int).SetString(bigVal, 10)
	if !ok {
		t.Fatalf("测试自己写错了数字: %s", bigVal)
	}
	if big.NewInt(data.GameID).Cmp(want) != 0 {
		t.Fatalf("gameId 精度丢了: 得到 %d, 期望 %s", data.GameID, bigVal)
	}
}
