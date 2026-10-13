import test from 'node:test';
import assert from 'node:assert/strict';

import { newView, applyEvent } from './state.js';
import { boardHTML, actionHTML, timerText, resultHTML, BOARD } from './render.js';

const ev = (type, data, seq) => (seq === undefined ? { type, data } : { type, seq, data });

// cells 把一段座位 HTML 里的牌盒解出来（顺序即渲染顺序）。
// 牌河/副露/自家手牌在 DOM 上各自有标记，所以分类不靠尺寸也不靠"有没有图"去猜
// （副露也是明牌，这一点一开始就猜错过）。
const cells = (chunk) =>
  [...chunk.matchAll(/<span class="cell([^"]*)"([^>]*)>/g)].map((m) => {
    const attrs = m[2];
    const px = (name) => {
      const r = new RegExp(`${name}:(-?[\\d.]+)px`).exec(attrs);
      return r ? +r[1] : null;
    };
    const data = (name) => {
      const r = new RegExp(`data-${name}="([^"]*)"`).exec(attrs);
      return r ? r[1] : null;
    };
    return {
      cls: m[1],
      tile: data('tile'),
      river: data('river') === '1',
      meld: data('meld') === '1',
      left: px('left'),
      top: px('top'),
      w: px('width'),
      h: px('height'),
    };
  });

const backs = (cs) => cs.filter((c) => !c.tile && !c.river && !c.meld);

const seatsOf = (html) => html.split('<div class="seat"').slice(1);

// 一桌假数据：自家 seat2 手番，牌河里 seat0 打出了 7 张（用来验"6 张一行"）。
function table(opts = {}) {
  const v = newView();
  applyEvent(v, ev('authed', { userId: 42, username: 'aleksey' }));
  applyEvent(v, ev('room.joined', {
    roomId: '7KQ2ZP', mySeat: 2,
    seats: [
      { seat: 0, userId: 1, username: '<img src=x onerror=alert(1)>', ready: true, connected: true, isOwner: true },
      { seat: 1, userId: 2, username: 'kaguya', ready: true, connected: true, isOwner: false },
      { seat: 2, userId: 42, username: 'me', ready: true, connected: true, isOwner: false },
      { seat: 3, userId: 4, username: 'ryu', ready: true, connected: true, isOwner: false },
    ],
  }, 1));
  applyEvent(v, ev('game.started', {
    gameId: 1, roundIndex: 1, roundWind: '1z', handNumber: 0, dealerSeat: 0, mySeat: 2,
    myHand: '123m456m789m11z55s', scores: [25000, 25000, 25000, 25000],
    riichiSticks: 0, remaining: 84, doraIndicators: ['3s'],
  }, 2));
  // seat0 连打 7 张，第一张是立直宣言牌
  const pile = ['9m', '1p', '3s', '7z', '5p', '2m', '8s'];
  pile.forEach((tile, i) => {
    applyEvent(v, ev(i === 0 ? 'game.riichi' : 'game.discarded', { seat: 0, tile, tsumogiri: false, remaining: 83 - i }, 3 + i));
  });
  if (opts.meld) {
    // 碰的必定是刚打出的那张，所以 tiles 里得真有它 —— 夹具也要自洽，
    // 否则 calledIndex 找不到、横放标记就没了（第一版就写成了碰 5z 却荣 8s）。
    applyEvent(v, ev('game.meld', { seat: 1, kind: 'pon', tiles: '5z5z5z', fromSeat: 0, calledTile: '5z', remaining: 76 }, 10));
  }
  if (opts.drawn) {
    applyEvent(v, ev('game.drew', { seat: 2, tile: '7p', remaining: 75 }, 11));
  }
  // 自家手番（不给这一条，动作键帽就一个都不该亮）
  applyEvent(v, ev('game.turn', { seat: 2, deadlineMs: 1_000_000 }, 30));
  return v;
}

test('自家手牌画牌面且可点，别家只画牌背（协议 §1.2）', () => {
  const html = boardHTML(table());
  const seats = seatsOf(html);
  assert.equal(seats.length, 4, '四个座位都要画');

  const mine = cells(seats[2]).filter((c) => c.tile);
  assert.equal(mine.length, 13, '自家 13 张手牌');
  assert.deepEqual(mine.map((c) => c.tile).slice(0, 3), ['1m', '2m', '3m'], '组内顺序要保留');

  // 牌背不挂 data-tile（牌面根本没下发），所以「总数 − 有牌面的」就是牌背数
  const other = cells(seats[0]);
  assert.equal(backs(other).length, 13, '别家 13 张牌背');
  assert.equal(other.filter((c) => c.tile).length, 0, '别家不下发牌面');
  assert.equal(seats[0].includes('tb fill back'), true, '别家画的是牌背');

  assert.equal(html.includes('<img src=x'), false, '用户名要转义：别家输进来的东西不能当 HTML');
  assert.equal(html.includes('&lt;img'), true, '原样保留但已被转义');
});

test('副露过的手牌张数按 13 − 3×副露数画牌背', () => {
  const seats = seatsOf(boardHTML(table({ meld: true })));
  const seat1 = cells(seats[1]);
  assert.equal(backs(seat1).length, 10, '碰过一组 → 暗牌 10 张');
  assert.equal(seat1.filter((c) => c.meld).length, 3, '副露摆出来，是明牌');
  assert.equal(seats[1].includes('cell yoko'), true, '被鸣的那张要横放');
});

test('牌河 6 张一行，第 7 张换行', () => {
  // 第一张是立直宣言牌（横放），占位宽是 47 不是 34 —— 所以按 data-river 取，不按宽度猜
  const river = cells(seatsOf(boardHTML(table()))[0]).filter((c) => c.river);
  assert.equal(river.length, 7);
  const row1 = river.slice(0, 6);
  assert.equal(new Set(row1.map((c) => c.top)).size, 1, '前 6 张同一行');
  assert.equal(river[6].top > row1[0].top, true, '第 7 张换行');
  assert.equal(river[6].top - row1[0].top, 47 + 4, '行距 = 牌高 + vgap');
});

test('立直宣言牌横放，且它本身就是牌河里的一张（服务端不补发 discarded）', () => {
  const seat0 = seatsOf(boardHTML(table()))[0];
  const yoko = [...seat0.matchAll(/class="cell yoko"[^>]*style="left:(-?[\d.]+)px;top:(-?[\d.]+)px;width:(\d+)px;height:(\d+)px"/g)];
  assert.equal(yoko.length, 1, '恰好一张横放');
  // 横放牌的占位盒是旋转后的尺寸：宽 = 牌高(47)、高 = 牌宽(34)
  assert.equal(yoko[0][3], '47');
  assert.equal(yoko[0][4], '34');
});

test('摸到的那张与手牌隔开', () => {
  const withDraw = cells(seatsOf(boardHTML(table({ drawn: true })))[2]).filter((c) => c.tile);
  assert.equal(withDraw.length, 14, '13 张手牌 + 摸到的第 14 张');
  assert.equal(withDraw[13].tile, '7p');
  assert.equal(withDraw[13].cls.includes('drawn'), true, '渲染要能认出这是刚摸的');
  const gap = withDraw[13].left - (withDraw[12].left + withDraw[12].w);
  assert.equal(gap, 18, '摸牌额外隔开 drawGap');

  const noDraw = cells(seatsOf(boardHTML(table()))[2]).filter((c) => c.tile);
  assert.equal(noDraw.length, 13);
});

test('动作键帽：亮的只有服务端给的 options，其余灰掉', () => {
  const v = table();
  // 自家手番：亮立直/摸切/杠。杠在这里是**自己回合的暗杠或加杠** ——
  // 它不在鸣牌窗口里（大明杠才走窗口），所以不能只按 options 点亮。
  let act = actionHTML(v);
  assert.equal((act.match(/class="key k-\w+"/g) || []).length, 3);
  assert.equal((act.match(/off/g) || []).length, 3);
  assert.match(act, /class="key k-kan"/, '自家手番时杠要能点');

  applyEvent(v, ev('game.claimWindow', { trigger: 'discard', fromSeat: 3, tile: '4p', options: ['chi', 'pon', 'ron', 'pass'], deadlineMs: 1 }, 12));
  act = actionHTML(v);
  assert.equal((act.match(/class="key k-\w+"/g) || []).length, 4, 'options 里的亮');
  assert.equal((act.match(/off/g) || []).length, 2, '窗口里没给的（立直/杠）要灰掉');
  assert.equal(act.includes('摸切'), false, '鸣牌窗口里那个键叫跳过');
  assert.equal(act.includes('[ 跳过 ]'), true);

  applyEvent(v, ev('game.claimClosed', { trigger: 'discard', resolvedBy: null }, 13));
  act = actionHTML(v);
  assert.equal((act.match(/off/g) || []).length, 3, '窗口一关就只剩自家手番那三个键');
});

test('读秒用服务端截止时间，基础 5 秒之后才算读秒', () => {
  const v = table();
  const deadline = 1_000_000;
  applyEvent(v, ev('game.turn', { seat: 2, deadlineMs: deadline }, 12));
  v.clockOffsetMs = 0;

  assert.match(timerText(v, deadline - 2000), /基础 2\.0s/);
  assert.match(timerText(v, deadline + 1), /读秒 1\/3/);
  assert.match(timerText(v, deadline + 10001), /读秒 2\/3/);
  assert.match(timerText(v, deadline + 30001), /读秒 3\/3/);

  applyEvent(v, ev('game.turn', { seat: 1, deadlineMs: deadline }, 13));
  assert.equal(timerText(v, deadline - 1000), '', '不是自家手番就不画读秒条');
});

test('里宝牌单独一行，不混进表宝牌', () => {
  const v = table();
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win', wins: [{ winnerSeat: 0 }], uraIndicators: ['9s', '2m'],
  }, 12));
  const html = boardHTML(v);
  assert.equal(html.includes('里宝牌 ×2'), true);
  assert.equal(html.includes('dora ura'), true);
});

test('结算 · 和了：报菜名、番符、点数、赤五、和牌张都在', () => {
  const v = table();
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win',
    wins: [{
      winnerSeat: 2, loserSeat: 0, isTsumo: false, winningTile: '5s',
      han: 3, fu: 40, yakuman: 0,
      yaku: [{ name: '立直', han: 1 }, { name: '平和', han: 1 }, { name: '断幺九', han: 1 }],
      dora: { dora: 1, ura: 1, aka: 0 }, paoSeat: null, paoYaku: null,
      payments: { 0: -3900, 1: 0, 2: 3900, 3: 0 },
    }],
    hand: '123m456m789m11z0s5s', deltas: [0, 0, 3900, 0], newScores: [21100, 25000, 28900, 25000],
    riichiSticks: 1, dealerRepeats: false, uraIndicators: ['7m'],
  }, 40));

  const html = resultHTML(v);
  assert.match(html, /和了/);
  assert.match(html, /立直 1 · 平和 1 · 断幺九 1/, '报菜名就是 yaku 数组，不重算');
  assert.match(html, /40符 3番/);
  assert.match(html, /3900点/, '点数取 payments 里和牌者的进账（算分全在服务端）');
  assert.match(html, /ドラ 1 · 裏 1 · 赤 0/);
  assert.match(html, /供托 1 归和牌者/);
  assert.match(html, /里宝牌指示/);
  assert.match(html, /ロン ← P0/, '荣和要写放铳的是谁');
  assert.match(html, /和牌张 <b>5s<\/b>/, '和牌张写在标题里：挤在手牌那一行会折行');
  assert.equal(html.includes('tb red'), true, '赤五描红框');
  assert.equal(html.includes('tb win'), true, '和牌张描金边');
  // 面板画在棋盘**外面**的浮层里：画在棋盘里就会跟着棋盘一起缩（0.85 倍、字剩 12px）
  assert.equal(boardHTML(v).includes('class="result'), false, '结算面板不在棋盘里');
  assert.equal(resultHTML(v).startsWith('<div class="result'), true);
  // 分数变化要"一眼看到"：四格大号数字，和牌者那格描金边
  assert.equal((html.match(/<span class="d(?: win)?">/g) || []).length, 4, '四家各一格');
  assert.match(html, /<span class="d win"><i>[^<]*<\/i><b class="plus">\+3900<\/b><\/span>/);
  assert.match(html, /<b class="zero">0<\/b>/, '不增不减也要占一格，不然四格对不齐');
});

test('结算期间座位牌上直接挂分数变化', () => {
  const v = table();
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win',
    wins: [{ winnerSeat: 2, loserSeat: 0, isTsumo: false, winningTile: '6s', han: 1, fu: 30,
      yakuman: 0, yaku: [{ name: '立直', han: 1 }], dora: { dora: 0, ura: 0, aka: 0 },
      paoSeat: null, paoYaku: null, payments: { 0: -7700, 1: 0, 2: 8700, 3: 0 } }],
    hand: '123m456m789m11z0s5s6s', deltas: [-7700, 0, 8700, 0],
    newScores: [16300, 26800, 32900, 25000], riichiSticks: 1,
  }, 40));
  const html = boardHTML(v);
  assert.match(html, /<span class="sd minus">-7700<\/span>/, '输的那家分数旁边挂红的');
  assert.match(html, /<span class="sd plus">\+8700<\/span>/, '赢的那家挂绿的');
  assert.equal((html.match(/class="sd /g) || []).length, 2, '不增不减的两家不挂东西');
});

test('自家手番那行提示文案已删（手番靠绿框高亮和读秒条表示）', () => {
  const v = table();
  assert.equal(boardHTML(v).includes('手番'), false, '座位牌上不再写"手番"两个字');
  const act = actionHTML(v);
  assert.equal(act.includes('自家手番'), false);
  assert.equal(act.includes('点手牌打出去'), false);
  assert.equal(act.includes('class="cap"'), false, '没话可说时连那个位置都不留');
  // 但模式提示要留：吃/立直点了之后得知道下一步点哪儿
  assert.match(actionHTML(v, { armed: 'chi', hint: '吃：点两张手牌' }), /吃：点两张手牌/);
});

test('结算 · 包（帕奥）：要写清谁独付，别让人当成 bug', () => {
  const v = table();
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win',
    wins: [{
      winnerSeat: 2, loserSeat: null, isTsumo: true, winningTile: '5p', han: 13, fu: 0, yakuman: 0,
      yaku: [{ name: '大三元', han: 13 }], dora: { dora: 0, ura: 0, aka: 0 },
      paoSeat: 1, paoYaku: '大三元', payments: { 0: -16000, 1: -40300, 2: 64300, 3: -8000 },
    }],
    hand: '555z666z777z11m123p', deltas: [-16000, -40300, 64300, -8000], newScores: [8000, 0, 64300, 17000],
    riichiSticks: 0,
  }, 40));
  const html = resultHTML(v);
  assert.match(html, /包：P1/, '包要指到具体座位');
  assert.match(html, /大三元/);
  assert.match(html, /64300点/);
  assert.match(html, /ツモ/);
});

test('结算 · 和了：副露也要摆出来（roundEnd 只给暗牌，副露在 winDeclared 里）', () => {
  const v = table();
  applyEvent(v, ev('game.winDeclared', {
    seat: 2, isTsumo: true, winningTile: '3s', fromSeat: null,
    hand: '123m456m11z33s', melds: [{ kind: 'pon', tiles: '5z5z5z', fromSeat: 0 }],
  }, 39));
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win',
    wins: [{ winnerSeat: 2, loserSeat: null, isTsumo: true, winningTile: '3s', han: 2, fu: 40,
      yakuman: 0, yaku: [{ name: '役牌 白', han: 1 }], dora: { dora: 1, ura: 0, aka: 0 },
      paoSeat: null, paoYaku: null, payments: { 0: -1300, 1: -1300, 2: 3900, 3: -1300 } }],
    hand: '123m456m11z33s', deltas: [-1300, -1300, 3900, -1300], newScores: [22700, 25500, 28100, 23700],
    riichiSticks: 0,
  }, 40));

  const html = resultHTML(v);
  assert.equal(html.includes('meldgrp'), true, '碰的那组要摆出来');
  assert.equal((html.match(/Haku\.svg/g) || []).length, 3, '副露三张（5z 的牌面是 Haku.svg）');
  assert.match(html, /ツモ/);
});

test('结算 · 荒牌流局：听牌家、待牌、听牌料', () => {
  const v = table();
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'draw', wins: [],
    tenpaiSeats: [0, 2], notenSeats: [1, 3],
    tenpaiHands: [
      { seat: 0, hand: '123m456m789m11z', waiting: ['3s', '6s'] },
      { seat: 2, hand: '19m19p19s1234z', waiting: ['1p'] },
    ],
    deltas: [1500, -1500, 1500, -1500], newScores: [26500, 23500, 25700, 23500],
  }, 41));

  const html = resultHTML(v);
  assert.match(html, /荒牌流局/);
  assert.match(html, /听牌 <b class="plus">P0 P2<\/b>/);
  assert.match(html, /未听 <b class="minus">P1 P3<\/b>/);
  assert.match(html, /待 3s 6s/);
  assert.match(html, /待 1p/);
  assert.match(html, /\+1500/);
  assert.match(html, /-1500/);
  // 流局时未听牌家的手牌绝不能出现（协议 §7.2）
  assert.equal(html.includes('123m456m789m11z'), false, '听牌家的手牌是短的紧凑记法，原串不该原样出现');
});

test('结算 · 终局：顺位与ポイント（整数点 ÷1000）', () => {
  const v = table();
  applyEvent(v, ev('game.gameEnd', {
    gameId: 1, finalScores: [41200, 18900, 25100, 14800],
    placements: [1, 3, 2, 4], points: [61200, -14100, -15900, -31200], reason: 'all_last_round',
  }, 200));

  const html = resultHTML(v);
  assert.match(html, /终局/);
  assert.match(html, /打满终局/);
  assert.match(html, /1位/);
  assert.match(html, /\+61\.2 pt/, '61200 是「点」，除以 1000 才是 pt');
  assert.match(html, /-14\.1 pt/);
  assert.match(html, /41200/);
  // 顺位要按名次排，不按座位
  assert.ok(html.indexOf('1位') < html.indexOf('4位'), '一位排在四位前面');
});

test('没有结算时不画面板', () => {
  assert.equal(resultHTML(table()), '');
  assert.equal(boardHTML(table()).includes('class="result'), false);
});

test('结算 · 和了：服务端没发 hand 时不许画空白（我是和牌者就用我自己的手牌）', () => {
  const v = table(); // mySeat = 2
  applyEvent(v, ev('game.winDeclared', { winnerSeat: 2, winningTile: '6s', isTsumo: false }, 39));
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win',
    wins: [{ winnerSeat: 2, loserSeat: 0, isTsumo: false, winningTile: '6s', han: 3, fu: 40,
      yakuman: 0, yaku: [{ name: '立直', han: 1 }], dora: { dora: 0, ura: 0, aka: 0 },
      paoSeat: null, paoYaku: null, payments: { 0: -3900, 1: 0, 2: 3900, 3: 0 } }],
    deltas: [-3900, 0, 3900, 0], newScores: [21100, 25000, 28900, 25000], riichiSticks: 0,
    // 服务端这一版（view.go）没有 hand 字段 —— 用例专门按"没有"来喂
  }, 40));

  const html = resultHTML(v);
  assert.equal(html.includes('和牌手牌：服务端这一版没发'), false, '我是和牌者，不该说没发');
  assert.match(html, /（你自己的手牌）/, '用我自己的手牌顶上');
  assert.equal((html.match(/\.svg/g) || []).length >= 13, true, '手牌真的画出来了（13 张）');
  assert.match(html, /tb win/, '和牌张仍要描金边');
});

test('结算 · 和了：别家和牌且服务端没发 hand 时，如实说没发而不是画空', () => {
  const v = table();
  applyEvent(v, ev('game.winDeclared', { winnerSeat: 0, winningTile: '3p', isTsumo: true }, 39));
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win',
    wins: [{ winnerSeat: 0, loserSeat: null, isTsumo: true, winningTile: '3p', han: 2, fu: 30,
      yakuman: 0, yaku: [{ name: '門前清自摸和', han: 1 }], dora: { dora: 0, ura: 0, aka: 0 },
      paoSeat: null, paoYaku: null, payments: { 0: 3900, 1: -1300, 2: -1300, 3: -1300 } }],
    deltas: [3900, -1300, -1300, -1300], newScores: [28900, 23700, 22900, 23700], riichiSticks: 0,
  }, 41));

  const html = resultHTML(v);
  assert.match(html, /和牌手牌：服务端这一版没发/);
  assert.match(html, /ツモ/);
  assert.match(html, /門前清自摸和 1/);
});

test('结算 · 和了：副露退回桌面上那份（副露本来就是公开信息）', () => {
  const v = table();
  // P2（我）碰过 5z：桌面上已经有这组副露
  applyEvent(v, ev('game.meld', { seat: 2, kind: 'pon', tiles: ['5z', '5z', '5z'], fromSeat: 0, taken: '5z', closed: false }, 39));
  applyEvent(v, ev('game.roundEnd', {
    roundIndex: 1, endedBy: 'win',
    wins: [{ winnerSeat: 2, loserSeat: 0, isTsumo: false, winningTile: '6s', han: 2, fu: 30,
      yakuman: 0, yaku: [{ name: '役牌 白', han: 1 }], dora: { dora: 0, ura: 0, aka: 0 },
      paoSeat: null, paoYaku: null, payments: { 0: -2000, 1: 0, 2: 2000, 3: 0 } }],
    deltas: [-2000, 0, 2000, 0], newScores: [23000, 25000, 27000, 25000], riichiSticks: 0,
  }, 40));
  const html = resultHTML(v);
  assert.equal(html.includes('meldgrp'), true, 'winDeclared.melds 没发，就用桌面上那一组');
  assert.equal((html.match(/Haku\.svg/g) || []).length, 3);
});

test('服务端发了 actions 就只按它点亮（前端不再自己猜合法性）', () => {
  const v = table();
  // 轮到我，但服务端只说能打牌和立直 → 杠要灰着
  applyEvent(v, ev('game.turn', {
    seat: 2, deadlineMs: 1, actions: ['discard', 'riichi'],
    kanClosed: [], kanAdded: [],
  }, 12));
  let act = actionHTML(v);
  assert.match(act, /class="key k-riichi"/, '立直亮');
  assert.match(act, /class="key k-pass"/, '摸切一直给：它跟点牌出牌是同一个动作');
  assert.equal(/class="key k-kan"/.test(act), false, '服务端没给 kan_*，杠必须灰着');
  assert.equal(/class="key k-ron"/.test(act), false, '没给 tsumo/ron，就不能亮和牌');

  // 服务端给了和牌与暗杠 → 亮起来
  applyEvent(v, ev('game.turn', {
    seat: 2, deadlineMs: 1, actions: ['discard', 'tsumo', 'kan_closed'],
    kanClosed: ['9s'], kanAdded: [],
  }, 13));
  act = actionHTML(v);
  assert.match(act, /class="key k-ron"/);
  assert.match(act, /class="key k-kan"/);
  assert.deepEqual(v.game.turn.kanClosed, ['9s'], '牌表要留在 turn 上，点杠时查它');

  // actions 是空表 = 服务端说"你什么都不能做"，不是"没发"
  applyEvent(v, ev('game.turn', { seat: 2, deadlineMs: 1, actions: [] }, 14));
  act = actionHTML(v);
  assert.equal(/class="key k-riichi"/.test(act), false, '空表一个都不亮');
  assert.match(act, /class="key k-pass"/, '只有摸切还在（不是规则，是出牌这个动作本身）');
});

test('服务端没发 actions（旧服务端）→ 退回"轮到我就点亮"', () => {
  const v = table();
  const act = actionHTML(v);
  assert.match(act, /class="key k-riichi"/);
  assert.match(act, /class="key k-kan"/);
  assert.equal(v.game.turn.actions, null, '收不到就是 null，不是空表');
});

test('暗杠四张盖着画（服务端发 closed 就是让前端决定盖还是亮）', () => {
  const v = table();
  applyEvent(v, ev('game.meld', { seat: 2, kind: 'kan', tiles: ['9s', '9s', '9s', '9s'], fromSeat: 2, taken: '', closed: true }, 39));
  const html = boardHTML(v);
  const backs = [...html.matchAll(/<span class="tb fill back">/g)].length;
  // 别家三家的牌背 + 暗杠四张
  assert.equal(backs, 3 * 13 + 4, '暗杠那四张也要算成牌背');
  assert.equal(html.includes('data-meld="1"'), false, '暗杠不该出现明牌标记');
});

test('重连快照之后牌桌不空（手牌/牌河/副露都画得出来）', () => {
  const v = newView();
  applyEvent(v, ev('room.joined', { roomId: 'X', mySeat: 2, seats: [] }, 1));
  applyEvent(v, ev('game.snapshot', {
    gameId: 1, roundIndex: 3, handNumber: 1, dealerSeat: 1, mySeat: 2,
    myHand: ['1m', '2m', '3m', '4p', '5p'],
    melds: [[], [], [{ kind: 'pon', tiles: ['5z', '5z', '5z'], closed: false }], []],
    discards: [['9m', '1p'], [], ['3s'], []],
    scores: [25000, 24000, 26000, 25000], riichi: [false, true, false, false],
    riichiSticks: 1, turn: 2, awaitingMe: true, deadlineMs: Date.now() + 3000, remaining: 40,
    doraIndicators: ['3s'],
  }, 9));

  const html = boardHTML(v);
  assert.equal((html.match(/data-tile=/g) || []).length, 5, '手牌画出来了');
  assert.equal((html.match(/data-river="1"/g) || []).length, 3, '牌河画出来了');
  assert.equal((html.match(/data-meld="1"/g) || []).length, 3, '副露画出来了');
  assert.match(html, /Sou3\.svg/, '宝牌指示牌在中央面板上（3s 的资源名是 Sou3.svg）');
});

test('房间被解散后：中央面板说清"为什么"，四个座位牌回到空', () => {
  const v = table();
  applyEvent(v, ev('room.closed', { roomId: '7KQ2ZP', reason: 'playerTimeout' }, 40));
  const html = boardHTML(v);
  assert.match(html, /房间已关闭/);
  assert.match(html, /有人掉线，保座超时/, '原因要翻成人话');
  assert.match(html, /7KQ2ZP/, '哪一间也要写出来');
  assert.equal(html.includes('未入房'), false, '不能显示成"从没进过房"');
  assert.equal((html.match(/class="badge/g) || []).length, 4, '四个座位牌还在（只是没人）');
  assert.equal(html.includes('>—<'), true);

  // 服务端将来加新原因：不许吞掉，原样显示
  const v2 = table();
  applyEvent(v2, ev('room.closed', { roomId: 'X', reason: 'somethingNew' }, 41));
  assert.match(boardHTML(v2), /somethingNew/);

  // playerLeft：对局中退出＝认输（协议 §5.2），服务端的第二种 reason
  const v3 = table();
  applyEvent(v3, ev('room.closed', { roomId: 'X', reason: 'playerLeft' }, 42));
  assert.match(boardHTML(v3), /有人中途退出（视为认输）/);
});

test('大厅（还没有牌局）也能画：座位牌 + 房间号', () => {
  const v = newView();
  applyEvent(v, ev('room.joined', {
    roomId: '7KQ2ZP', mySeat: 2,
    seats: [{ seat: 0, userId: 1, username: 'a', ready: true, isOwner: true }],
  }, 1));
  const html = boardHTML(v);
  assert.equal(html.includes('7KQ2ZP'), true);
  assert.equal(html.includes('1/4 人'), true);
  assert.equal(html.includes('房主'), true);
  assert.equal(seatsOf(html).filter((s) => s.length > 40).length, 0, '没牌局时不该有牌');
});

test('空状态不炸（连房间都没进）', () => {
  const html = boardHTML(newView());
  assert.equal(html.includes('未入房'), true);
  assert.equal(html.includes(`width: ${BOARD}px`) || html.length > 100, true);
});
