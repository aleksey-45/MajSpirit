import test from 'node:test';
import assert from 'node:assert/strict';

import { newView, applyEvent } from './state.js';

const ev = (type, data, seq) => (seq === undefined ? { type, data } : { type, seq, data });

// started 造一份"已在牌局中"的状态：13 张手牌、四家分数、宝牌 3s。
function started() {
  const v = newView();
  applyEvent(v, ev('authed', { userId: 42, username: 'aleksey' }));
  applyEvent(v, ev('room.joined', { roomId: '7KQ2ZP', mySeat: 2, seats: [] }, 1));
  applyEvent(v, ev('game.started', {
    gameId: 1, roundIndex: 1, roundWind: '1z', handNumber: 0, dealerSeat: 0, mySeat: 2,
    myHand: '123m456m789m11z55s', scores: [25000, 25000, 25000, 25000],
    riichiSticks: 0, remaining: 84, doraIndicators: ['3s'], firstTurn: 0,
  }, 2));
  return v;
}

test('seq：第一个带序号的事件当基线，之后才算洞', () => {
  const v = newView();
  applyEvent(v, ev('room.joined', { roomId: 'X', mySeat: 0, seats: [] }, 7));
  assert.equal(v.sync.lastSeq, 7);
  assert.equal(v.sync.needsSync, false, '连上之前房间的序号已经在涨，那不是丢消息');

  applyEvent(v, ev('room.seats', { seats: [] }, 8));
  assert.equal(v.sync.needsSync, false);
  applyEvent(v, ev('room.seats', { seats: [] }, 10));
  assert.equal(v.sync.needsSync, true, 'seq 有洞 → 要 game.sync');
});

test('连接类事件不带 seq，不能当成洞', () => {
  const v = newView();
  applyEvent(v, ev('hello', { protocolVersion: 1, serverTime: Date.now() }));
  applyEvent(v, ev('ping', { serverTime: Date.now() }));
  applyEvent(v, ev('pong', { serverTime: Date.now() }));
  assert.equal(v.sync.lastSeq, null);
  assert.equal(v.sync.needsSync, false);
});

test('game.drew：别家摸牌不下发牌面，只有张数变化', () => {
  const v = started();
  applyEvent(v, ev('game.drew', { seat: 1, remaining: 83 }, 3));
  assert.equal(v.game.hand.length, 13, '别家的摸牌不该进自家手牌');
  assert.equal(v.game.drawn, null);
  assert.equal(v.game.remaining, 83);
});

test('game.drew：自己摸牌进 drawn，手牌仍 13 张', () => {
  const v = started();
  assert.equal(v.game.hand.length, 13);
  applyEvent(v, ev('game.drew', { seat: 2, tile: '7p', remaining: 83 }, 3));
  assert.equal(v.game.drawn, '7p');
  assert.equal(v.game.hand.length, 13, 'drawn 是第 14 张，不并进 hand');
});

test('打牌张数守恒：摸切 / 手切两条路手牌都不多不少', () => {
  const v = started();
  // 摸 7p 后摸切
  applyEvent(v, ev('game.drew', { seat: 2, tile: '7p', remaining: 83 }, 3));
  applyEvent(v, ev('game.discarded', { seat: 2, tile: '7p', tsumogiri: true, remaining: 83 }, 4));
  assert.equal(v.game.drawn, null);
  assert.equal(v.game.hand.length, 13);
  assert.deepEqual(v.game.discards[2], [{ tile: '7p', riichi: false }]);

  // 摸 4z 后手切 1z：删 1z、4z 并回手牌
  applyEvent(v, ev('game.drew', { seat: 2, tile: '4z', remaining: 82 }, 5));
  applyEvent(v, ev('game.discarded', { seat: 2, tile: '1z', tsumogiri: false, remaining: 82 }, 6));
  assert.equal(v.game.hand.length, 13);
  assert.equal(v.game.hand.filter((t) => t === '1z').length, 1, '两张东去掉一张');
  assert.equal(v.game.hand.includes('4z'), true, '摸到的那张并回手牌');
  assert.equal(v.game.drawn, null);
});

test('别家打牌不动自家手牌', () => {
  const v = started();
  applyEvent(v, ev('game.discarded', { seat: 0, tile: '1z', tsumogiri: false, remaining: 83 }, 3));
  assert.equal(v.game.hand.length, 13);
  assert.deepEqual(v.game.discards[0], [{ tile: '1z', riichi: false }]);
});

test('game.snapshot 是整体覆盖，不是追加', () => {
  const v = started();
  applyEvent(v, ev('game.drew', { seat: 2, tile: '7p', remaining: 83 }, 3));
  assert.equal(v.game.drawn, '7p');

  applyEvent(v, ev('game.snapshot', {
    gameId: 1, roundIndex: 2, roundWind: '2z', handNumber: 1, dealerSeat: 1, mySeat: 2,
    myHand: '19m19p19s1234567z', scores: [24000, 26000, 25000, 25000],
    remaining: 30, doraIndicators: ['3s', '7z'],
    discards: [['9m'], [], [{ tile: '5p', riichi: true }], []],
    melds: [[], [], [], [{ kind: 'pon', tiles: '5z5z5z', fromSeat: 0 }]],
    riichiSeats: [2], ippatsuSeats: [],
  }, 20));

  assert.equal(v.game.hand.length, 13, '快照是"这一份"，不是再叠一份');
  assert.equal(v.game.hand[0], '1m');
  assert.equal(v.game.drawn, null);
  assert.equal(v.game.roundIndex, 2);
  assert.deepEqual(v.game.discards[0], [{ tile: '9m' }], '快照只给字符串，统一成对象');
  assert.deepEqual(v.game.discards[2], [{ tile: '5p', riichi: true }]);
  assert.deepEqual(v.game.melds[3][0].tiles, ['5z', '5z', '5z']);
  assert.equal(v.game.melds[3][0].calledIndex, -1, '快照没给被鸣的是哪张，就不横放');
  assert.equal(v.sync.needsSync, false, '快照就是对账的结果，seq 的洞到此为止');
});

test('clockOffsetMs 用服务端时间校准，不看本地时钟', () => {
  const v = newView();
  applyEvent(v, ev('hello', { protocolVersion: 1, serverTime: Date.now() + 5000 }));
  assert.ok(v.clockOffsetMs > 4800 && v.clockOffsetMs <= 5000, `偏移 ${v.clockOffsetMs}`);
});

test('鸣牌窗口：只认服务端的 claimClosed，别的射击不清它', () => {
  const v = started();
  applyEvent(v, ev('game.claimWindow', {
    trigger: 'discard', fromSeat: 3, tile: '4p',
    options: ['chi', 'pon', 'ron', 'pass'], deadlineMs: 1759459230000,
  }, 3));
  assert.deepEqual(v.game.claim.options, ['chi', 'pon', 'ron', 'pass']);

  applyEvent(v, ev('game.turn', { seat: 0, deadlineMs: 1759459240000 }, 4));
  assert.notEqual(v.game.claim, null, '窗口没关之前不能自己清掉');

  applyEvent(v, ev('game.claimClosed', { trigger: 'discard', resolvedBy: null, seat: null }, 5));
  assert.equal(v.game.claim, null);
});

test('game.meld：被鸣的牌从牌河消失；加杠不碰牌河', () => {
  const v = started();
  applyEvent(v, ev('game.discarded', { seat: 0, tile: '9m', tsumogiri: false, remaining: 83 }, 3));
  applyEvent(v, ev('game.discarded', { seat: 0, tile: '5z', tsumogiri: false, remaining: 82 }, 4));
  assert.equal(v.game.discards[0].length, 2);

  applyEvent(v, ev('game.meld', {
    seat: 1, kind: 'pon', tiles: '5z5z5z', fromSeat: 0, calledTile: '5z', remaining: 82,
  }, 5));
  assert.deepEqual(v.game.discards[0].map((x) => x.tile), ['9m'], '碰走的那张不该留在牌河');
  assert.equal(v.game.melds[1].length, 1);
  assert.equal(v.game.melds[1][0].calledIndex, 0, '被鸣的那张要能定位，渲染才画得横放');

  applyEvent(v, ev('game.discarded', { seat: 3, tile: '7z', tsumogiri: false, remaining: 81 }, 6));
  applyEvent(v, ev('game.meld', {
    seat: 1, kind: 'addedKan', tiles: '5z5z5z5z', fromSeat: 1, calledTile: '5z', remaining: 81,
  }, 7));
  assert.deepEqual(v.game.discards[3].map((x) => x.tile), ['7z'], '加杠动的是自己的副露，不是牌河');
});

test('game.riichi 的宣言牌就是一张打出的牌（服务端不补发 discarded）', () => {
  const v = started();
  applyEvent(v, ev('game.riichi', { seat: 0, tile: '9m' }, 3));
  assert.deepEqual(v.game.discards[0], [{ tile: '9m', riichi: true }]);

  applyEvent(v, ev('game.riichiStick', { seat: 0, sticks: 1, scores: [24000, 25000, 25000, 25000] }, 4));
  assert.equal(v.game.riichiSticks, 1);
  assert.deepEqual(v.game.riichiSeats, [0]);
  assert.deepEqual(v.game.scores, [24000, 25000, 25000, 25000], '供托扣的 1000 点要落到分数上');
});

test('room.closed：房间与对局一起清掉，但留下"为什么"', () => {
  const v = started(); // 有房间、有对局
  applyEvent(v, ev('room.closed', { roomId: '7KQ2ZP', reason: 'playerTimeout' }, 30));
  assert.equal(v.room, null, '留着座位牌会让人以为还在房间里');
  assert.equal(v.game, null);
  assert.equal(v.result, null);
  assert.deepEqual(v.roomClosed, { roomId: '7KQ2ZP', reason: 'playerTimeout' });

  // 重新建房/加入要把这条提示清掉，否则新房间一开就挂着上一间的讣告
  applyEvent(v, ev('room.joined', { roomId: 'NEW', mySeat: 0, seats: [] }, 31));
  assert.equal(v.roomClosed, null);
  assert.equal(v.room.roomId, 'NEW');
});

test('game.meld 按服务端实际发的字段：kind 只有 chi/pon/kan，被鸣的那张叫 taken', () => {
  const v = started();
  // 先摆出牌河：P0 打 5z（会被 P1 碰走），P3 打 1p（会被 P0 大明杠）
  applyEvent(v, ev('game.discarded', { seat: 0, tile: '5z', tsumogiri: false, remaining: 60 }, 3));
  applyEvent(v, ev('game.discarded', { seat: 3, tile: '1p', tsumogiri: false, remaining: 59 }, 4));

  applyEvent(v, ev('game.meld', { seat: 1, kind: 'pon', tiles: ['5z', '5z', '5z'], fromSeat: 0, taken: '5z', closed: false }, 5));
  assert.deepEqual(v.game.discards[0].map((t) => t.tile), [], '被碰走的那张从牌河消失');
  assert.deepEqual(v.game.melds[1][0].tiles, ['5z', '5z', '5z'], 'tiles 是数组也收');
  assert.equal(v.game.melds[1][0].calledIndex, 0, 'taken 用来定位横放的那张');

  applyEvent(v, ev('game.meld', { seat: 0, kind: 'kan', tiles: ['1p', '1p', '1p', '1p'], fromSeat: 3, taken: '1p', closed: false }, 6));
  assert.deepEqual(v.game.discards[3].map((t) => t.tile), [], '大明杠的 kind 是 kan（不是协议示例里的 openKan），也要删');

  // 暗杠：动的是自己摆出来的四张，牌河一张都不该少
  applyEvent(v, ev('game.discarded', { seat: 2, tile: '9s', tsumogiri: false, remaining: 58 }, 7));
  applyEvent(v, ev('game.meld', { seat: 2, kind: 'kan', tiles: ['9s', '9s', '9s', '9s'], fromSeat: 2, taken: '', closed: true }, 8));
  assert.deepEqual(v.game.discards[2].map((t) => t.tile), ['9s'], '暗杠不碰牌河');
  assert.equal(v.game.melds[2][0].closed, true, '暗杠要留着 closed，渲染层靠它决定盖还是亮');
});

test('game.doraRevealed 用 kind=dora/ura 区分（协议示例里的 isUra 服务端没发）', () => {
  const v = started();
  applyEvent(v, ev('game.doraRevealed', { kind: 'dora', indicator: '1m', count: 1 }, 3));
  applyEvent(v, ev('game.doraRevealed', { kind: 'ura', indicator: '7m', count: 0 }, 4));
  assert.deepEqual(v.game.doraIndicators, ['3s', '1m']);
  assert.deepEqual(v.game.uraIndicators, ['7m'], '里宝别混进表宝牌');
});

test('game.winDeclared 的座位字段叫 winnerSeat（协议示例里是 seat）', () => {
  const v = started();
  applyEvent(v, ev('game.winDeclared', { winnerSeat: 2, winningTile: '6s', isTsumo: false }, 3));
  assert.equal(v.game.win.seat, 2);
  assert.equal(v.game.win.winningTile, '6s');
});

test('game.snapshot 是另一套形状：数组手牌/牌河、裸座位号、myOptions', () => {
  const v = newView();
  applyEvent(v, ev('room.joined', { roomId: 'X', mySeat: 2, seats: [] }, 1));
  applyEvent(v, ev('game.snapshot', {
    gameId: 1, roundIndex: 3, handNumber: 1, dealerSeat: 1, mySeat: 2,
    myHand: ['1m', '2m', '3m', '4p', '5p'],
    melds: [[], [], [{ kind: 'pon', tiles: ['5z', '5z', '5z'], closed: false }], []],
    discards: [['9m', '1p'], [], ['3s'], []],
    scores: [25000, 24000, 26000, 25000], riichi: [false, true, false, false],
    riichiSticks: 1, turn: 2, awaitingMe: true, deadlineMs: 123, remaining: 40,
    doraIndicators: ['3s'],
    claim: { trigger: 'discard', fromSeat: 0, tile: '4p', myOptions: ['chi', 'pass'] },
  }, 9));

  const g = v.game;
  assert.deepEqual(g.hand, ['1m', '2m', '3m', '4p', '5p'], '快照的手牌是数组，不是紧凑记法');
  assert.deepEqual(g.discards[0], [{ tile: '9m' }, { tile: '1p' }], '牌河是码的数组，要包成对象');
  assert.deepEqual(g.melds[2][0].tiles, ['5z', '5z', '5z']);
  assert.deepEqual(g.riichiSeats, [1], '立直是 [4]bool，不是座位表');
  assert.equal(g.turn.seat, 2, 'turn 是裸座位号');
  assert.equal(g.turn.deadlineMs, 123, '截止时间在顶层');
  assert.equal(g.turn.actions, null, '快照没发合法性 → null（不是空表，渲染层据此退回旧行为）');
  assert.equal(g.awaitingMe, true, 'awaitingMe 是快照独有的');
  assert.deepEqual(g.claim.options, ['chi', 'pass'], '窗口选项字段叫 myOptions');
  assert.deepEqual(g.doraIndicators, ['3s'], '快照里有宝牌指示牌');
  assert.equal(g.riichiSticks, 1);
  assert.equal(v.sync.needsSync, false, '快照就是对账，洞到此为止');
});

test('room.seats 只给两项也补齐成四项，空座为 null', () => {
  const v = newView();
  applyEvent(v, ev('room.joined', {
    roomId: 'X', mySeat: 1,
    seats: [
      { seat: 0, userId: 1, username: 'a', ready: false, connected: true, isOwner: true },
      { seat: 1, userId: 2, username: 'b', ready: true, connected: true, isOwner: false },
    ],
  }, 1));
  assert.equal(v.room.seats.length, 4);
  assert.equal(v.room.seats[0].username, 'a');
  assert.equal(v.room.seats[2], null);

  applyEvent(v, ev('room.readyChanged', { seat: 1, ready: false }, 2));
  assert.equal(v.room.seats[1].ready, false);

  applyEvent(v, ev('room.seatVacated', { seat: 0, reason: 'timeout' }, 3));
  assert.equal(v.room.seats[0], null);
});

test('结算：roundEnd 落分数，gameEnd 落顺位', () => {
  const v = started();
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win', wins: [{ winnerSeat: 2, han: 3, fu: 40 }],
    deltas: [0, 0, 3900, 0], newScores: [21100, 25000, 28900, 25000], riichiSticks: 0,
  }, 3));
  assert.equal(v.result.kind, 'roundEnd');
  assert.deepEqual(v.game.scores, [21100, 25000, 28900, 25000]);

  applyEvent(v, ev('game.gameEnd', {
    gameId: 1, finalScores: [41200, 18900, 25100, 14800],
    placements: [1, 3, 2, 4], points: [61200, -14100, -15900, -31200], reason: 'all_last_round',
  }, 200));
  assert.equal(v.result.kind, 'gameEnd');
  assert.deepEqual(v.result.placements, [1, 3, 2, 4]);
});

test('宝牌指示是列表：追加、不去重；里宝牌走另一条', () => {
  const v = started(); // 开局 1 张指示牌
  applyEvent(v, ev('game.doraRevealed', { indicator: '3s', isUra: false, kanCount: 1 }, 3));
  assert.deepEqual(v.game.doraIndicators, ['3s', '3s'], '两张 3s = 宝牌翻倍，去重就把番数算错');

  // 全场开杠上限 4 → 表宝牌最多 5 张
  for (const [i, ind] of ['7z', '1m', '5p'].entries()) {
    applyEvent(v, ev('game.doraRevealed', { indicator: ind, isUra: false, kanCount: i + 2 }, 4 + i));
  }
  assert.deepEqual(v.game.doraIndicators, ['3s', '3s', '7z', '1m', '5p']);

  applyEvent(v, ev('game.doraRevealed', { indicator: '9s', isUra: true, kanCount: 4 }, 7));
  assert.deepEqual(v.game.uraIndicators, ['9s'], '里宝牌不能混进表宝牌那一行');
  assert.equal(v.game.doraIndicators.length, 5);

  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win', wins: [{ winnerSeat: 0, han: 4, fu: 30 }],
    uraIndicators: ['9s', '2m'],
  }, 8));
  assert.deepEqual(v.game.uraIndicators, ['9s', '2m'], '立直和牌时一次性揭示全部里宝牌');
});

test('未知事件忽略，不抛错也不改状态', () => {
  const v = started();
  const before = JSON.stringify(v.game);
  applyEvent(v, ev('game.somethingNew', { x: 1 }, 3));
  assert.equal(JSON.stringify(v.game), before);
  assert.equal(v.lastEvent.type, 'game.somethingNew');

  applyEvent(v, { type: 'x' }); // 连 data 都没有
  applyEvent(v, null);
  applyEvent(v, {});
  assert.equal(JSON.stringify(v.game), before);
});
