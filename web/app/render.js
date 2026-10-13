// 渲染：把 view 画成 DOM。
//
// boardHTML(view) 是**纯函数**（view → HTML 字符串），所以牌桌布局能脱离浏览器用
// node 用例盯着 —— 这套几何在样张阶段踩过三次重叠（横放牌占位、中央面板太宽、
// 读秒条压手牌），全靠这种"量一遍"的方式抓出来。
// render(view) 只负责把它塞进 DOM 并缩放。

import { parseTiles, tileName, isRed } from './tiles.js';

// 逻辑棋盘 1040×1040，整块按容器宽缩放。四个座位绕中心转，各自在局部坐标里摆牌。
export const BOARD = 1040;
const HALF = BOARD / 2;
const SEAT_ANGLES = [0, -90, 180, 90];
const L = { riverY: 120, handOuter: 448 };

// 自家一套尺寸，三家小一档；牌河的内沿都在 riverY，所以四条牌河是同一圈。
const SIZE = {
  me: { hand: { w: 54, h: 72, gap: 4, drawGap: 18 }, river: { w: 43, h: 59, gap: 4, vgap: 5 }, meld: { w: 34, h: 47, gap: 3, groupGap: 8 } },
  other: { hand: { w: 38, h: 51, gap: 2, drawGap: 12 }, river: { w: 34, h: 47, gap: 3, vgap: 4 }, meld: { w: 27, h: 38, gap: 3, groupGap: 6 } },
};

const WINDS = ['東', '南', '西', '北'];
const WIND_OF = { '1z': '東', '2z': '南', '3z': '西', '4z': '北' };
const RIVER_COLS = 6;

// 用户名是别家输进来的，不能直接拼进 HTML（协议 §7 的同一套判据：不可信对端）。
const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

const tileURL = (code) => `/tiles/${tileName(code)}.svg`;

function cell(code, x, y, w, h, cls, attrs = '') {
  const extra = isRed(code) ? ' red' : '';
  return `<span class="cell${cls ? ' ' + cls : ''}"${attrs} style="left:${x}px;top:${y}px;width:${w}px;height:${h}px">`
    + `<span class="tb fill${extra}"><img src="${tileURL(code)}" alt="${esc(code)}"></span></span>`;
}

function backCell(x, y, w, h) {
  return `<span class="cell" style="left:${x}px;top:${y}px;width:${w}px;height:${h}px">`
    + '<span class="tb fill back"></span></span>';
}

// 横放：占位盒是旋转后的真实尺寸（高×宽），里面的牌盒保持自然尺寸再转 90°。
function yokoCell(code, x, y, w, h, attrs = '') {
  const extra = isRed(code) ? ' red' : '';
  return `<span class="cell yoko"${attrs} style="left:${x}px;top:${y}px;width:${h}px;height:${w}px">`
    + `<span class="tb${extra}" style="width:${w}px;height:${h}px"><img src="${tileURL(code)}" alt="${esc(code)}"></span></span>`;
}

// 一行牌先算每张的占位宽（横放 = 牌高）再累加游标 —— 否则横放牌会压住下一张。
function rowHTML(items, gap, w, h, y, attrs = '') {
  const widths = items.map((it) => (it.yoko ? h : w));
  const total = widths.reduce((a, b) => a + b, 0) + gap * Math.max(items.length - 1, 0);
  let x = -total / 2;
  return items
    .map((it, i) => {
      const out = it.yoko ? yokoCell(it.tile, x, y, w, h, attrs) : cell(it.tile, x, y, w, h, '', attrs);
      x += widths[i] + gap;
      return out;
    })
    .join('');
}

function riverHTML(pile, R) {
  let out = '';
  for (let r = 0; r * RIVER_COLS < pile.length; r++) {
    const row = pile.slice(r * RIVER_COLS, (r + 1) * RIVER_COLS);
    const y = L.riverY + r * (R.h + R.vgap);
    // data-river 让 DOM 自描述"这是牌河"，用例和样式都不用靠尺寸去猜。
    out += rowHTML(row.map((t) => ({ tile: t.tile, yoko: !!t.riichi })), R.gap, R.w, R.h, y, ' data-river="1"');
  }
  return out;
}

// handHTML：自家画牌面、可点；别家只有张数（协议 §1.2），画牌背。
//
// drawGap 是"摸到的那张与它左边那张之间的间隔"（替换掉普通的 gap），
// 不是"额外再加一段" —— 否则画出来的间隔是 gap + drawGap，和名字不符。
function handHTML(hand, drawn, S, opts) {
  const me = !!opts.me;
  const count = me ? hand.length + (drawn ? 1 : 0) : (opts.concealed || 0);
  const split = count > 1 && count % 3 === 2;
  const lead = (i) => (i === 0 ? 0 : split && i === count - 1 ? S.drawGap : S.gap);
  let total = count * S.w;
  for (let i = 0; i < count; i++) total += lead(i);
  const top = L.handOuter - S.h;
  let x = -total / 2;
  let out = '';
  for (let i = 0; i < count; i++) {
    x += lead(i);
    if (!me) {
      out += backCell(x, top, S.w, S.h);
    } else {
      const code = i < hand.length ? hand[i] : drawn;
      // data-tile 让页面知道点的是哪一张；打牌/立直都从这儿来。
      out += cell(code, x, top, S.w, S.h, i === count - 1 && drawn ? 'drawn' : '', ` data-tile="${esc(code)}"`);
    }
    x += S.w;
  }
  return { html: out, width: total };
}

// 副露贴在手牌外侧，底边与手牌对齐；横放的那张靠 calledIndex 定位。
// 暗杠（closed）四张都盖着 —— 服务端专门发这个标记就是让前端决定盖还是亮。
function meldHTML(melds, handWidth, M) {
  const groups = melds.map((m) => {
    const items = m.tiles.map((tile, i) => ({ tile, yoko: i === m.calledIndex, closed: m.closed === true }));
    const widths = items.map((it) => (it.yoko ? M.h : M.w));
    return { items, widths, w: widths.reduce((a, b) => a + b, 0) + M.gap * Math.max(items.length - 1, 0) };
  });
  if (!groups.length) return '';
  const totalW = groups.reduce((a, g) => a + g.w, 0) + M.groupGap * Math.max(groups.length - 1, 0);
  let x = -handWidth / 2 - 26 - totalW;
  let out = '';
  for (const g of groups) {
    g.items.forEach((it) => {
      const cw = it.yoko ? M.h : M.w;
      const ch = it.yoko ? M.w : M.h;
      if (it.closed) out += backCell(x, L.handOuter - ch, cw, ch);
      else if (it.yoko) out += yokoCell(it.tile, x, L.handOuter - ch, M.w, M.h, ' data-meld="1"');
      else out += cell(it.tile, x, L.handOuter - ch, M.w, M.h, '', ' data-meld="1"');
      x += cw + M.gap;
    });
    x += M.groupGap;
  }
  return out;
}

function seatHTML(view, seat) {
  const g = view.game;
  const rel = (seat - g.mySeat + 4) % 4;
  const K = seat === g.mySeat ? SIZE.me : SIZE.other;
  if (seat === g.mySeat) {
    const h = handHTML(g.hand, g.drawn, K.hand, { me: true });
    return `<div class="seat" style="transform:rotate(${SEAT_ANGLES[rel]}deg)">`
      + riverHTML(g.discards[seat] || [], K.river)
      + h.html
      + meldHTML(g.melds[seat] || [], h.width, K.meld)
      + '</div>';
  }
  // 别家的暗牌张数 = 13 - 3 × 自己的副露数（协议不下发牌面，只能数出来）。
  const concealed = 13 - 3 * (g.melds[seat] || []).length;
  const h = handHTML([], null, K.hand, { me: false, concealed });
  return `<div class="seat" style="transform:rotate(${SEAT_ANGLES[rel]}deg)">`
    + riverHTML(g.discards[seat] || [], K.river)
    + h.html
    + meldHTML(g.melds[seat] || [], h.width, K.meld)
    + '</div>';
}

function doraHTML(view) {
  const g = view.game;
  if (!g || !g.doraIndicators.length) return '';
  const n = g.doraIndicators.length;
  const label = `宝牌指示 ×${n}${n > 1 ? `（开杠 ${n - 1}）` : ''}`;
  // 最多 5 张（初始 1 + 全场开杠上限 4）。5×48+4×8 > 面板内宽，所以那一行靠
  // aspect-ratio + flex-shrink 自己缩 —— 尺寸不在这里算，写在 table.css 里。
  const tiles = g.doraIndicators.map((c) => `<span class="tb"><img src="${tileURL(c)}" alt="${esc(c)}"></span>`).join('');
  const ura = g.uraIndicators.length
    ? `<div class="l3">里宝牌 ×${g.uraIndicators.length}</div><div class="dora ura">${g.uraIndicators
        .map((c) => `<span class="tb"><img src="${tileURL(c)}" alt="${esc(c)}"></span>`)
        .join('')}</div>`
    : '';
  return `<div class="l3">${label}</div><div class="dora">${tiles}</div>${ura}`;
}

// 房间被解散的原因。服务端现在发 playerTimeout（掉线保座超时）和
// playerLeft（对局中退出，按协议 §5.2 视为认输）；其它值原样显示 ——
// 服务端加新原因不该让客户端把消息吞掉（协议 §2.1 的同一个判据）。
const ROOM_CLOSED_REASON = {
  playerTimeout: '有人掉线，保座超时',
  playerLeft: '有人中途退出（视为认输）',
};

function centerHTML(view) {
  const g = view.game;
  if (!g) {
    if (view.roomClosed) {
      const why = ROOM_CLOSED_REASON[view.roomClosed.reason] || view.roomClosed.reason || '';
      return `<div class="center closed">
        <div class="l1">房间已关闭</div>
        <div class="l2">${esc(view.roomClosed.roomId || '')}</div>
        <div class="l2">${esc(why)}</div>
        <div class="l3">room.create / room.join</div>
      </div>`;
    }
    const seats = (view.room && view.room.seats) || [];
    const filled = seats.filter(Boolean).length;
    const ready = seats.filter((s) => s && s.ready).length;
    return `<div class="center">
      <div class="l1">${esc((view.room && view.room.roomId) || '未入房')}</div>
      <div class="l2">${filled}/4 人　准备 ${ready}</div>
      <div class="l3">${view.room ? '房主开局' : 'room.create / room.join'}</div>
    </div>`;
  }
  const wind = WIND_OF[g.roundWind] || '';
  return `<div class="center">
    <div class="l1">${wind}${g.roundIndex}局 ${g.handNumber}本場</div>
    <div class="l2">牌山 剩余 ${g.remaining ?? '—'}</div>
    ${doraHTML(view)}
    <div class="sticks"><span class="kyo">▮ 供托 ${g.riichiSticks}</span><span class="hon">▮ 本場 ${g.handNumber}</span></div>
  </div>`;
}

// 座位牌放在不随座位旋转的 HUD 层：侧家的名字和分数是正着的。
const CORNERS = [
  { left: 830, top: 976, cls: 'me' },
  { left: 870, top: 10, cls: 'other' },
  { left: 10, top: 10, cls: 'other' },
  { left: 10, top: 987, cls: 'other' },
];

function badgeHTML(view, seat) {
  const g = view.game;
  const room = view.room;
  const mySeat = g ? g.mySeat : room ? room.mySeat : 0;
  const rel = (seat - mySeat + 4) % 4;
  const where = CORNERS[rel];
  const member = room && room.seats ? room.seats[seat] : null;
  const name = member ? member.username : '—';
  const wind = g ? WINDS[(seat - g.dealerSeat + 4) % 4] : '';
  const dealer = g && seat === g.dealerSeat;
  const score = g ? g.scores[seat] : null;
  const tags = [];
  if (g && g.riichiSeats.includes(seat)) tags.push('立直');
  if (g && g.ippatsuSeats.includes(seat)) tags.push('一发');
  if (member && member.connected === false) tags.push('离线');
  if (!g && member && member.ready) tags.push('准备');
  if (!g && member && member.isOwner) tags.push('房主');
  const turn = g && g.turn && g.turn.seat === seat;
  // 结算期间把分数变化直接挂在分数旁边：面板可能被挡着，四家的加减要随时看得见。
  const deltas = view.result && Array.isArray(view.result.deltas) ? view.result.deltas : null;
  const d = deltas ? deltas[seat] : null;
  const deltaHTML = typeof d === 'number' && d !== 0
    ? `<span class="sd ${d > 0 ? 'plus' : 'minus'}">${d > 0 ? '+' : ''}${d}</span>` : '';
  return `<div class="badge ${where.cls}${turn ? ' active' : ''}" style="left:${where.left}px;top:${where.top}px">
    <div class="n">P${seat} ${wind}${dealer ? '家' : ''} ${esc(name)}</div>
    <div class="s">${score === null ? '—' : score}${deltaHTML}</div>
    <div class="r">${tags.join('·')}</div>
  </div>`;
}

// timerText 只画自家手番的读秒（协议 §9.1：基础 5 秒 + 3×10 秒读秒）。
//
// 用的是服务端给的绝对截止时间 + clockOffsetMs，不是本地时钟（协议 §2.4）；
// 读秒不进 deadlineMs，所以基础时间用完之后本地自己往后数。
export function timerText(view, now = Date.now()) {
  const g = view.game;
  if (!g || !g.turn || g.turn.seat !== g.mySeat) return '';
  const remain = g.turn.deadlineMs - (now + view.clockOffsetMs);
  if (remain > 0) {
    const bars = Math.max(0, Math.min(10, Math.round((remain / 5000) * 10)));
    return `[${'█'.repeat(bars)}${'░'.repeat(10 - bars)}] 基础 ${(remain / 1000).toFixed(1)}s`;
  }
  const over = -remain;
  const n = Math.min(3, Math.floor(over / 10000) + 1);
  const left = 10000 - (over % 10000);
  const bars = Math.max(0, Math.min(10, Math.round((left / 10000) * 10)));
  return `[${'█'.repeat(bars)}${'░'.repeat(10 - bars)}] 读秒 ${n}/3 · ${(left / 1000).toFixed(1)}s`;
}

const ACTION_ORDER = ['ron', 'pon', 'chi', 'kan', 'riichi', 'pass'];
const ACTION_NAME = { ron: '荣和', pon: '碰', chi: '吃', kan: '杠', riichi: '立直', pass: '跳过' };

// actionHTML：亮的是服务端给的 options（协议 §6.6：options 已按座位裁剪）；
// 自家手番时亮立直/摸切；其余灰掉。客户端不自己判断"能不能"，只呈现。
export function actionHTML(view, ui = {}) {
  const g = view.game;
  const enabled = new Set();
  // 自家能做什么，优先级跟 kanKind 一样：**服务端发了就只按它**，没发才退回"轮到我就点亮"。
  // 规则判定只有一份实现（internal/mahjong），前端不该为了"立直按钮什么时候亮"再算一遍
  // 听牌/门清/点数 —— 两份必然漂，而漂的方向是"点了没反应"。
  const acts = g && g.turn && g.turn.seat === g.mySeat ? g.turn.actions : null;
  if (g && g.claim) for (const o of g.claim.options) enabled.add(o);
  else if (acts) {
    for (const a of acts) {
      if (a === 'tsumo' || a === 'ron') enabled.add('ron');
      else if (a === 'kan_closed' || a === 'kan_added' || a === 'kan') enabled.add('kan');
      else if (a !== 'discard') enabled.add(a);
    }
    // 摸切不是"规则允许不允许"的事：它就是把手牌打出去（跟点那张牌同一个动作），
    // 所以轮到自家就一直给 —— 不然 actions 少写一个词，玩家就没法出牌了。
    enabled.add('pass');
  } else if (g && g.turn && g.turn.seat === g.mySeat) {
    enabled.add('riichi');
    enabled.add('pass');
    // 暗杠/加杠**不在鸣牌窗口里** —— 自己回合主动发起，点了之后再点一张手牌
    // （服务端 doKan 只认"轮到自己且等着打牌"的那两种）。合不合法由服务端判。
    enabled.add('kan');
  }
  const labels = { ...ACTION_NAME, pass: g && g.claim ? '跳过' : '摸切' };
  const keys = ACTION_ORDER.map((k) => {
    const on = enabled.has(k);
    const armed = ui.armed === k ? ' armed' : '';
    return `<span class="key k-${k}${on ? '' : ' off'}${armed}" data-cmd="${k}">[ ${labels[k]} ]</span>`;
  }).join('');
  // 只留"模式提示"（吃/立直要先点牌）。默认状态那句"自家手番……"删掉了：
  // 手番由绿框高亮和读秒条表示，不需要再写一行字。
  const cap = ui.hint ? `<span class="cap" id="hint">${esc(ui.hint)}</span>` : '';
  return keys + cap;
}

/* ── 结算面板 ──
   协议 §6.8 有三种形状，画法各不相同：和了（含报菜名与包）、荒牌流局（听牌家与听牌料）、
   终局（顺位与ポイント）。数据在 fold 里就是原始载荷，所以这里只做排版。 */

// 面板里的小牌是行内的（.result 会把 .tb 缩到 32×43），和牌那张描金边。
function handRow(codes, mark = -1) {
  return codes
    .map((c, i) => `<span class="tb${isRed(c) ? ' red' : ''}${i === mark ? ' win' : ''}"><img src="${tileURL(c)}" alt="${esc(c)}"></span>`)
    .join('');
}

const seatName = (view, seat) => {
  const m = view.room && view.room.seats ? view.room.seats[seat] : null;
  const who = m ? m.username : 'P' + seat;
  const g = view.game;
  const wind = g ? WINDS[(seat - g.dealerSeat + 4) % 4] : '';
  return `P${seat} ${wind} ${who}`;
};

// 分数变化：win 的 roundEnd 给 deltas 数组，payments 是按座位的对象（协议 §6.8），
// 两个都在就优先用 deltas（它天然有序）。
//
// 画成四格大号数字：结算时最该一眼看到的就是"谁加减了多少"，
// 挤成一行小字等于没画。和牌者那一格描金边。
function deltaRow(view, r, winnerSeat = -1) {
  let deltas = Array.isArray(r.deltas) ? r.deltas : null;
  if (!deltas && r.wins && r.wins[0] && r.wins[0].payments) {
    deltas = [0, 1, 2, 3].map((s) => r.wins[0].payments[String(s)] ?? 0);
  }
  if (!deltas) return '';
  return '<div class="deltas">' + deltas
    .map((d, s) => {
      const cls = d > 0 ? 'plus' : d < 0 ? 'minus' : 'zero';
      const win = s === winnerSeat ? ' win' : '';
      return `<span class="d${win}"><i>${esc(seatName(view, s))}</i><b class="${cls}">${d > 0 ? '+' : ''}${d}</b></span>`;
    })
    .join('') + '</div>';
}

// 和牌者的手牌。协议 §6.8 里 `roundEnd.hand` 与 `winDeclared.hand` 都有，但服务端这一版
// **两个都没发**（view.go 的 gameRoundEndView / gameWinDeclaredView 里没有 hand 字段）。
// 所以：我是和牌者就用我自己的手牌（那本来就是我该看到的），别人和牌只能如实说"没发"。
function winHand(view, r, winnerSeat) {
  const w = view.game && view.game.win;
  const fromServer = r.hand || (w && w.hand);
  if (fromServer) return { codes: parseTiles(fromServer), mine: false };
  const g = view.game;
  if (g && winnerSeat === g.mySeat) {
    // 揭示的是 14 张 = 手里 13 张 + 和牌张。自摸时和牌张就是刚摸的那张，荣和时它是别人的
    // 弃牌 —— 两种情况都是"13 张不动，再补一张"，所以不去重（手里本来就有同种牌也要补）。
    const winTile = (w && w.winningTile) || null;
    const codes = g.hand.slice();
    if (winTile) codes.push(winTile);
    else if (g.drawn) codes.push(g.drawn);
    return { codes, mine: true };
  }
  return { codes: [], mine: false };
}

// 和牌者的副露：优先 winDeclared.melds（协议 §6.8），服务端这一版没发就退回桌面上那份 ——
// 副露本来就是公开信息，桌面上一直有。
function winMelds(view, winnerSeat) {
  const w = view.game && view.game.win;
  const fromWin = w && (w.seat ?? w.winnerSeat) === winnerSeat && Array.isArray(w.melds) ? w.melds : null;
  const table = (view.game && view.game.melds && view.game.melds[winnerSeat]) || [];
  return (fromWin || table)
    .map((m) => {
      const codes = Array.isArray(m.tiles) ? m.tiles : parseTiles(m.tiles || '');
      return codes.length ? `<span class="meldgrp">${handRow(codes)}</span>` : '';
    })
    .join('');
}

function winHTML(view, r) {
  const w = (r.wins && r.wins[0]) || {};
  const { codes: hand, mine } = winHand(view, r, w.winnerSeat);
  const mark = hand.lastIndexOf(w.winningTile);
  const yaku = (w.yaku || []).map((y) => `${esc(y.name)} ${y.han}`).join(' · ');
  const dora = w.dora || {};
  const doraLine = `ドラ ${dora.dora ?? 0} · 裏 ${dora.ura ?? 0} · 赤 ${dora.aka ?? 0}`;
  const score = w.yakuman
    ? `役満${w.yakuman > 1 ? '×' + w.yakuman : ''}`
    : `${w.fu ?? '?'}符 ${w.han ?? '?'}番`;
  const pay = w.isTsumo ? 'ツモ' : `ロン ← ${esc(seatName(view, w.loserSeat))}`;
  // 包里那把大额支出不是 bug，要写出来（协议 §6.8 特意提醒过）。
  const pao = w.paoSeat !== null && w.paoSeat !== undefined
    ? `<div class="pao">包：${esc(seatName(view, w.paoSeat))}（${esc(w.paoYaku || '')}）由他独付</div>` : '';
  const ura = (r.uraIndicators || []).length
    ? `<div class="line ura"><span>里宝牌指示</span><span class="dora-inline">${handRow(r.uraIndicators)}</span></div>` : '';
  return `<div class="result win">
    <div class="head"><span class="tag">和了</span> ${esc(seatName(view, w.winnerSeat))} · ${pay} · 和牌张 <b>${esc(w.winningTile || '')}</b></div>
    ${hand.length
    ? `<div class="hand">${handRow(hand, mark)}${winMelds(view, w.winnerSeat)}${mine ? '<span class="winname">（你自己的手牌）</span>' : ''}</div>`
    : '<div class="line dim">和牌手牌：服务端这一版没发（协议 §6.8 的 hand 字段）</div>'}
    <div class="line yaku">${yaku || '（没有役？）'}</div>
    <div class="line"><span class="dim">${doraLine}</span><span class="pts">${score} · ${scoreOf(w)}点</span></div>
    ${pao}${ura}
    ${deltaRow(view, r, w.winnerSeat)}
    <div class="line dim">供托 ${r.riichiSticks ?? 0} 归和牌者${r.dealerRepeats ? ' · 连庄' : ''}</div>
  </div>`;
}

// scoreOf 从 payments 里取和牌者的进账：这是"要付多少"的权威数字（算分全在服务端）。
function scoreOf(w) {
  const p = w.payments || {};
  return p[String(w.winnerSeat)] ?? 0;
}

function drawHTML(view, r) {
  const hands = (r.tenpaiHands || []).map((h) => `
    <div class="line">${esc(seatName(view, h.seat))} 听牌
      <span class="hand">${handRow(parseTiles(h.hand || ''))}</span>
      <span class="dim">待 ${esc((h.waiting || []).join(' '))}</span>
    </div>`).join('');
  return `<div class="result draw">
    <div class="head"><span class="tag">荒牌流局</span> 牌山摸完，无人和牌</div>
    <div class="line">听牌 <b class="plus">${(r.tenpaiSeats || []).map((s) => 'P' + s).join(' ') || '无'}</b>
      · 未听 <b class="minus">${(r.notenSeats || []).map((s) => 'P' + s).join(' ') || '无'}</b>
      <span class="dim">（听牌料 3000 点由未听者出）</span></div>
    ${hands}
    ${deltaRow(view, r)}
  </div>`;
}

function gameEndHTML(view, r) {
  const order = [0, 1, 2, 3]
    .map((s) => ({ s, p: (r.placements || [])[s] ?? 0, pts: (r.points || [])[s] ?? 0, score: (r.finalScores || [])[s] ?? 0 }))
    .sort((a, b) => a.p - b.p);
  const rows = order.map((x) => `<div class="line"><span class="rank">${x.p}位</span>
      ${esc(seatName(view, x.s))} <b>${x.score}</b>
      <span class="${x.pts > 0 ? 'plus' : x.pts < 0 ? 'minus' : 'zero'}">${x.pts > 0 ? '+' : ''}${(x.pts / 1000).toFixed(1)} pt</span>
    </div>`).join('');
  // 没有 bankrupt：M-League 不飞人，整场只会因打满或协议结束（协议 §6.8）。
  const why = { all_last_round: '打满终局', agreed: '协议结束' }[r.reason] || r.reason;
  return `<div class="result end"><div class="head"><span class="tag">终局</span> ${esc(why || '')}</div>${rows}</div>`;
}

// resultHTML：没有结算时返回空串。三种形状按 kind / endedBy 分派。
export function resultHTML(view) {
  const r = view.result;
  if (!r) return '';
  if (r.kind === 'gameEnd') return gameEndHTML(view, r);
  if (r.endedBy === 'draw') return drawHTML(view, r);
  return winHTML(view, r);
}

// boardHTML 是纯的：view → 一串 HTML。牌桌能脱离浏览器被用例盯着，就靠这个。
export function boardHTML(view) {
  const g = view.game;
  const seats = [0, 1, 2, 3].map((s) => (g ? seatHTML(view, s) : '')).join('');
  const badges = [0, 1, 2, 3].map((s) => badgeHTML(view, s)).join('');
  return `<div class="board">
    <div class="tbl"></div><div class="tbl-in"></div>
    <div class="cross"></div><div class="cross h"></div>
    ${centerHTML(view)}
    ${seats}
    <div class="hud">${badges}</div>
    <div class="timer" style="top:${HALF + L.handOuter - SIZE.me.hand.h - 36}px">${esc(timerText(view))}</div>
  </div>`;
}

// fit 把逻辑棋盘缩到容器里。
//
// 两道约束取小：宽度（容器多宽）和高度（视口剩下的地方，再扣掉下方动作条）。
// 只按宽度缩的话，方桌在 1080p 上会高过视口 —— 自家手牌正好在桌子最下面，
// 于是"最该看清的那一排"要滚动才能看到。
export function fit(zone, reserve = 8) {
  if (!zone) return;
  const kx = zone.clientWidth / BOARD;
  const room = window.innerHeight - zone.getBoundingClientRect().top - reserve;
  const k = room > 160 ? Math.min(kx, room / BOARD) : kx;
  const board = zone.querySelector('.board');
  if (board) {
    board.style.transform = `scale(${k})`;
    // 方桌装不满一个宽屏容器：按高度缩完之后把左右剩下的地方对半分掉，
    // 否则整张桌子贴在左边、右边空一大片（1080p 上实测差了 300 多像素）。
    board.style.left = Math.max(0, Math.round((zone.clientWidth - BOARD * k) / 2)) + 'px';
  }
  zone.style.height = Math.round(BOARD * k) + 'px';
}

// render 把整张桌子重画一遍。DOM 很小，不做 diff。
//
// 结算面板**不画在棋盘里**：画在棋盘里就会跟着棋盘一起缩（1080p 上只有 0.85 倍，
// 字剩 12px），而结算正是要看清的那一屏。所以它单独一层、按窗口实际像素排版。
export function render(view, ui = {}, root = {}) {
  const zone = root.zone || document.getElementById('zone');
  const actions = root.actions || document.getElementById('actions');
  const result = root.result || document.getElementById('result');
  if (!zone) return;
  zone.innerHTML = boardHTML(view);
  if (actions) actions.innerHTML = actionHTML(view, ui);
  if (result) {
    const html = resultHTML(view);
    result.innerHTML = html;
    result.classList.toggle('show', !!html);
  }
  fit(zone, actions ? actions.offsetHeight + 8 : 8);
}
