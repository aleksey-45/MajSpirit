import test from 'node:test';
import assert from 'node:assert/strict';

import { createSession, envelope, closeReason } from './wire.js';

// fakeSocket 就是浏览器 WebSocket 的那几个面：readyState / send / close / 四个 onXxx。
// 有了它，"回 pong""发 game.sync"这些策略不用开浏览器也不用起服务端就能验。
function fakeSocket() {
  const sent = [];
  const sock = {
    readyState: 0,
    sent,
    send(text) {
      sent.push(text);
    },
    close(code) {
      this.readyState = 3;
      if (this.onclose) this.onclose({ code: code ?? 1000, reason: '' });
    },
    // ↓ 测试驱动用
    open() {
      this.readyState = 1;
      if (this.onopen) this.onopen();
    },
    deliver(env) {
      if (this.onmessage) this.onmessage({ data: typeof env === 'string' ? env : JSON.stringify(env) });
    },
  };
  return sock;
}

function session() {
  const sock = fakeSocket();
  const logs = [];
  const seen = [];
  const s = createSession({
    url: 'ws://x/ws',
    openSocket: () => sock,
    onLog: (e) => logs.push(e),
    onView: (v, env) => seen.push(env.type),
  });
  return { s, sock, logs, seen, types: () => sock.sent.map((t) => JSON.parse(t).type) };
}

test('envelope 就是协议 §4.2 的信封，data 不可省略', () => {
  assert.equal(envelope('ping'), '{"type":"ping","data":{}}');
  assert.equal(envelope('auth', { token: 't' }), '{"type":"auth","data":{"token":"t"}}');
});

test('连上自动发 auth；票为空就只连不认证（那是 4401 的演示动作）', () => {
  const a = session();
  a.s.connect('tok');
  a.sock.open();
  assert.deepEqual(a.types(), ['auth']);

  const b = session();
  b.s.connect('');
  b.sock.open();
  assert.deepEqual(b.types(), [], '空票不发 auth');
  assert.equal(b.logs.some((l) => l.text.includes('4401')), true, '要在日志里说清会发生什么');
});

test('收到 ping 回 pong；关掉自动回 pong 就不回（4408 的演示动作）', () => {
  const a = session();
  a.s.connect('tok');
  a.sock.open();
  a.sock.deliver({ type: 'ping', data: { serverTime: 1 } });
  assert.equal(a.types().includes('pong'), true);

  const b = session();
  b.s.autoPong = false;
  b.s.connect('tok');
  b.sock.open();
  b.sock.deliver({ type: 'ping', data: { serverTime: 1 } });
  assert.equal(b.types().includes('pong'), false);
});

test('seq 有洞且在对局中 → 补一次 game.sync，快照到了才重新武装', () => {
  const { s, sock, types } = session();
  s.connect('tok');
  sock.open();
  s.send('room.join', { roomId: 'X' }); // 自己发起的进房：回执不该顺带补同步
  sock.deliver({ type: 'room.joined', seq: 1, data: { roomId: 'X', mySeat: 0, seats: [] } });
  sock.deliver({ type: 'game.started', seq: 2, data: { gameId: 1, myHand: '123m', mySeat: 0, scores: [1, 1, 1, 1] } });

  sock.deliver({ type: 'game.turn', seq: 5, data: { seat: 0, deadlineMs: 1 } });
  assert.equal(types().filter((t) => t === 'game.sync').length, 1);
  sock.deliver({ type: 'game.turn', seq: 6, data: { seat: 1, deadlineMs: 2 } });
  assert.equal(types().filter((t) => t === 'game.sync').length, 1, '洞没补上也不再补发第二次');

  sock.deliver({ type: 'game.snapshot', seq: 7, data: { gameId: 1, myHand: '123m', mySeat: 0, scores: [1, 1, 1, 1] } });
  sock.deliver({ type: 'game.turn', seq: 10, data: { seat: 2, deadlineMs: 3 } });
  assert.equal(types().filter((t) => t === 'game.sync').length, 2, '快照之后又有洞，要再补');
});

test('回到房间（服务端补发的 room.joined）时主动问一次 game.sync —— 刷新后牌桌才不会空', () => {
  const { s, sock, types } = session();
  s.connect('tok');
  sock.open();
  // 页面刚刷新：没有发过 room.join，这条 room.joined 是服务端 resume 补发的
  sock.deliver({ type: 'room.joined', seq: 1, data: { roomId: 'X', mySeat: 0, seats: [] } });
  assert.equal(types().filter((t) => t === 'game.sync').length, 1, '不问这一次，牌桌就是空白');

  // 已经有对局了（快照到了）就不再问：这时该走 seq 缺口那条路
  sock.deliver({ type: 'game.snapshot', seq: 2, data: { gameId: 1, mySeat: 0, myHand: ['1m'], scores: [1, 1, 1, 1] } });
  sock.deliver({ type: 'room.joined', seq: 3, data: { roomId: 'X', mySeat: 0, seats: [] } });
  assert.equal(types().filter((t) => t === 'game.sync').length, 1, '手里已经有对局，不必再问');
});

test('自己 join 的回执不触发补同步（大厅里每进一次房都多发一条是噪声）', () => {
  const { s, sock, types } = session();
  s.connect('tok');
  sock.open();
  s.send('room.join', { roomId: 'X' });
  sock.deliver({ type: 'room.joined', seq: 1, data: { roomId: 'X', mySeat: 0, seats: [] } });
  assert.equal(types().includes('game.sync'), false);
});

test('不在对局里、也不是回到房间 → 不发 game.sync（那时服务端只会回 NOT_IN_ROOM）', () => {
  const { s, sock, types } = session();
  s.connect('tok');
  sock.open();
  // 大厅里 room.seats 出现 seq 洞：没有对局可同步，问也是白问
  sock.deliver({ type: 'room.seats', seq: 1, data: { seats: [] } });
  sock.deliver({ type: 'room.seats', seq: 4, data: { seats: [] } });
  assert.equal(types().includes('game.sync'), false);
});

test('坏 JSON 只记一行日志，不断开也不抛错', () => {
  const { s, sock, logs } = session();
  s.connect('tok');
  sock.open();
  assert.doesNotThrow(() => sock.deliver('{这不是 JSON'));
  assert.equal(logs.some((l) => l.kind === 'down' && l.text.includes('坏 JSON')), true);
  assert.equal(s.state, 'open', '连接该留着');
});

test('关闭码翻成人话（协议 §4.6）', () => {
  assert.equal(closeReason(4408), '心跳超时');
  assert.equal(closeReason(4401), '未认证 / 认证失败');
  assert.equal(closeReason(9999), '未知关闭码');

  const { s, sock, logs } = session();
  s.connect('tok');
  sock.open();
  sock.close(4408);
  assert.equal(logs.some((l) => l.kind === 'close' && l.text.includes('心跳超时')), true);
  assert.equal(s.state, 'closed');
});

test('sendRaw 原样发（调试页要能发坏 JSON 演 4400）', () => {
  const { s, sock } = session();
  s.connect('tok');
  sock.open();
  s.sendRaw('{这不是 JSON');
  assert.equal(sock.sent[sock.sent.length - 1], '{这不是 JSON');
});

test('换一条连接时状态清零（别把上一局的牌带过来）', () => {
  const { s, sock } = session();
  s.connect('tok');
  sock.open();
  sock.deliver({ type: 'game.started', seq: 1, data: { gameId: 1, myHand: '123m', mySeat: 0, scores: [1, 1, 1, 1] } });
  assert.equal(s.view.game.hand.length, 3);

  s.connect('tok'); // 新连接
  assert.equal(s.view.game, null);
  assert.equal(s.view.sync.lastSeq, null);
  assert.equal(s.view.sync.needsSync, false);
});
