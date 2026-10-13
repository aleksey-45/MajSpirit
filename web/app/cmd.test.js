import test from 'node:test';
import assert from 'node:assert/strict';

import { parseCommand, kanKind } from './cmd.js';

test('命令行覆盖协议 §5 的指令，牌一律是 2 字符紧凑记法', () => {
  const cases = [
    { in: 'discard 7p', want: { type: 'game.discard', data: { tile: '7p' } } },
    { in: 'discard 0s', want: { type: 'game.discard', data: { tile: '0s' } } }, // 赤五
    { in: 'riichi 5s', want: { type: 'game.riichi', data: { tile: '5s' } } },
    { in: 'chi 3s 4s', want: { type: 'game.chi', data: { tiles: ['3s', '4s'] } } },
    { in: 'pon', want: { type: 'game.pon', data: {} } },
    { in: 'kan', want: { type: 'game.kan', data: { kind: 'open' } } },
    { in: 'kan closed 5p', want: { type: 'game.kan', data: { kind: 'closed', tile: '5p' } } },
    { in: 'kan added 1z', want: { type: 'game.kan', data: { kind: 'added', tile: '1z' } } },
    { in: 'win', want: { type: 'game.win', data: {} } },
    { in: 'pass', want: { type: 'game.pass', data: {} } },
    { in: 'sync', want: { type: 'game.sync', data: {} } },
    { in: 'create', want: { type: 'room.create', data: {} } },
    { in: 'join 7kq2zp', want: { type: 'room.join', data: { roomId: '7KQ2ZP' } } },
    { in: 'leave', want: { type: 'room.leave', data: {} } },
    { in: 'ready', want: { type: 'room.ready', data: { ready: true } } },
    { in: 'ready false', want: { type: 'room.ready', data: { ready: false } } },
    { in: 'start', want: { type: 'room.start', data: {} } },
    { in: '  DISCARD   7p  ', want: { type: 'game.discard', data: { tile: '7p' } } },
  ];
  for (const c of cases) {
    const got = parseCommand(c.in);
    assert.equal(got.type, c.want.type, c.in);
    assert.deepEqual(got.data, c.want.data, c.in);
  }
});

test('坏的输入只回提示，不构造指令', () => {
  const bad = [
    'discard',        // 少了牌
    'discard 7x',     // 花色不对
    'discard 77p',    // 不是记法里的单张
    'chi 3s',         // 只给一张
    'kan closed',     // 暗杠没带牌
    'join ABC',       // 房间号长度不对
    'foobar',
    '',
    null,
  ];
  for (const line of bad) {
    const got = parseCommand(line);
    assert.equal(got.type, null, `${JSON.stringify(line)} 不该构造出指令`);
    assert.equal(typeof got.note, 'string', `${JSON.stringify(line)} 要给人一句话`);
  }
});

test('help 与不认识的东西都指回帮助', () => {
  assert.match(parseCommand('help').note, /discard/);
  assert.match(parseCommand('???').note, /不认识/);
  assert.match(parseCommand('').note, /discard/);
});

test('kanKind：服务端给了牌表就查表，查不到不猜', () => {
  const server = { kanClosed: ['9s'], kanAdded: ['5z'] };
  assert.equal(kanKind('5z', server, []), 'added');
  assert.equal(kanKind('9s', server, []), 'closed');
  assert.equal(kanKind('1p', server, []), null, '服务端列了牌但没有这张 → 别猜、别发');
  // 牌表优先于推断：手里有碰也听服务端的
  assert.equal(kanKind('5z', { kanClosed: ['5z'] }, [{ kind: 'pon', tiles: ['5z', '5z', '5z'] }]), 'closed');
});

test('kanKind：只有 actions 时按它定，没有才退回"碰过就是加杠"', () => {
  const pon = [{ kind: 'pon', tiles: ['5z', '5z', '5z'] }];
  assert.equal(kanKind('5z', { actions: ['kan_added'] }, pon), 'added');
  assert.equal(kanKind('5z', { actions: ['kan_closed'] }, pon), 'closed', '只给了 kan_closed 就别按碰过推断');
  // 旧服务端（什么合法性都不发）：退回前端自己的推断 —— 这一档是唯一一份"前端里的麻将逻辑"
  assert.equal(kanKind('5z', null, pon), 'added');
  assert.equal(kanKind('9s', null, pon), 'closed');
  assert.equal(kanKind('9s', null, null), 'closed', 'melds 缺了也不许炸');
  assert.equal(kanKind('3s', null, [{ kind: 'chi', tiles: ['3s', '4s', '5s'] }]), 'closed', '吃过的不是加杠');
  // 命令行那条路不受影响
  assert.equal(parseCommand('kan closed 5p').data.kind, 'closed');
  assert.equal(parseCommand('kan closed 5p').data.tile, '5p');
  assert.equal(parseCommand('kan open').data.tile, undefined, '大明杠的牌在牌河里，不带 tile');
});
