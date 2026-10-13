// Package protocol 是前后端通信的**信封与消息名**，对应 docs/local/协议.md §4.2、§5、§6。
//
// 它是一个叶子包：不 import 本项目任何东西，也不 import 标准库以外的任何东西。
// 这一点是它存在的理由 —— `internal/room` 要往外发信封，而它不该 import
// `internal/ws`（房间逻辑不碰 WebSocket，那样才能脱离网络测试）；
// `internal/ws` 也要用同一套名字。两边共用同一个类型，就不能把它放在其中一边。
//
// 这里只有信封、名字，以及给事件流发号的 Emitter（协议 §1.4）：每种消息的
// `data` 结构体定义在产生它的包里 —— 会话消息（hello/authed/error/ping/pong）在
// `internal/ws`，房间与对局消息在 `internal/room`。wire 契约本身以协议文档为准，
// 代码里的结构体只是它的实现。
package protocol
