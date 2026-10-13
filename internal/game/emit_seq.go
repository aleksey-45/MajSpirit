package game

// emit_seq.go —— 事件怎么发出去。事件体与投影见 emit.go。
//
// 发号不在这里：房间开的这一场用的是**房间那个** Emitter（开局时交给 Game），
// 于是 seq 是房间级的一条线，对局事件接着大厅事件往下排（协议 §1.4）。
// 对局自己发号的话，客户端在 game.started 那一刻必看到一个洞 ——
// 它会当成丢消息去拉快照，而其实什么都没丢。

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

// emit 发一条事件的全部收件人（共用一个号）。
//
// 发送失败绝不进规则层：发不出去一条 game.drew 不是 ILLEGAL_ACTION，
// 也不影响牌局。ErrSlowConsumer 说明那个对端已经不读了 —— 那条连接自己的写超时
// 会把它关掉，房间随后会按保座处理（协议 §4.5）。
func (g *Game) emit(msgs ...protocol.Message) {
	for i, err := range g.stream.Emit(msgs) {
		if err != nil {
			g.log.Info("推送失败", "user", msgs[i].UserID, "type", string(msgs[i].Env.Type), "err", err)
		}
	}
}
