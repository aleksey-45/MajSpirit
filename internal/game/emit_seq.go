package game

// emit_seq.go —— 事件怎么发出去。事件体与投影见 emit.go。
//
// 发号用的是开局时传进来的**房间那个** Emitter：对局事件接着大厅事件往下排。见
// protocol.Emitter 的契约。

import "github.com/aleksey-45/majspirit/internal/protocol"

// broadcast 把一条事件发给四个座位。
func (g *Game) broadcast(f fact) {
	msgs := make([]protocol.Message, 0, len(g.players))
	for seat, p := range g.players {
		msgs = append(msgs, protocol.Message{UserID: p.UserID, Env: project(seat, f)})
	}
	g.emit(msgs...)
}

// emitTo 只发给一个座位（有隐藏信息的事件走这条）。
func (g *Game) emitTo(seat int, f fact) {
	g.emit(protocol.Message{UserID: g.players[seat].UserID, Env: project(seat, f)})
}

// emit 发一条事件的全部收件人，并记下投递失败的那几个。
//
// 发送失败不进规则层：发不出去一条 game.drew 不是 ILLEGAL_ACTION，也不影响牌局。
func (g *Game) emit(msgs ...protocol.Message) {
	for i, err := range g.stream.Emit(msgs) {
		if err != nil {
			g.log.Info("推送失败", "user", msgs[i].UserID, "type", string(msgs[i].Env.Type), "err", err)
		}
	}
}
