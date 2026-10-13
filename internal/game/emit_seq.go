package game

// emit_seq.go —— seq 的分配、广播与投递。事件体与 Notifier 见 emit.go。

import "github.com/aleksey-45/majspirit/internal/protocol"

func (g *Game) nextSeq() int {
	g.seq++
	return g.seq
}

func (g *Game) broadcast(f fact) {
	seq := g.nextSeq()
	for seat, p := range g.players {
		env := project(seat, f)
		env.Seq = seq
		g.deliver(p.UserID, seat, env)
	}
}

func (g *Game) emitTo(seat int, f fact) {
	env := project(seat, f)
	env.Seq = g.nextSeq()
	g.deliver(g.players[seat].UserID, seat, env)
}

// deliver 把信封交给投递方。
//
// **发送失败绝不进规则层**：发不出去一条 game.drew 不是 ILLEGAL_ACTION，
// 也不影响牌局。ErrSlowConsumer 说明那个对端已经不读了 —— 那条连接自己的写超时
// 会把它关掉，房间随后会告诉我们对局要保座。
func (g *Game) deliver(userID int64, seat int, env protocol.Envelope) {
	if err := g.notify.Send(userID, env); err != nil {
		g.log.Info("推送失败", "seat", seat, "type", string(env.Type), "err", err)
	}
}
