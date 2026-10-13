// 可见状态镜像：把服务端事件折叠成渲染要的形状（协议 §9.3）。
//
// 这里只做「收到什么就记什么」，一个规则判定都不做：能不能打、能不能鸣、谁和了，
// 全是服务端的事。fold 里只有三处"推理"，每一处都写明了为什么必须推理：
//   - game.discarded 要把那张牌从自家手牌里去掉（协议只给牌名，不给下标）
//   - game.meld 要把被鸣的牌从牌河删掉（协议没说要不要删，日麻惯例是删）
//   - game.riichi 自己当一张打出的牌记（协议 §6.7 说它"等同"一条 discarded）
//
// 状态是原地改的：渲染是全量重画，不需要不可变快照，拷一层只是噪音。

import { parseTiles } from './tiles.js';

// newView 出一份空状态。每次新连接都用它，别复用上一局的。
export function newView() {
  return {
    protocolVersion: 0,
    user: null,
    // 服务端时间 − 本地时间。倒计时算的是 deadlineMs − (Date.now() + clockOffsetMs)。
    clockOffsetMs: 0,
    lastError: null,
    lastEvent: null,
    sync: { lastSeq: null, needsSync: false },
    room: null,
    game: null,
    result: null,
    // 房间被服务端解散时留下的"为什么"。room.closed 之后 room/game 都清空，
    // 界面得有个东西解释"我怎么回大厅了"，所以这条提示单独留着。
    roomClosed: null,
  };
}

// seats 恒 4 项、空座 null：渲染层不必为"数组可能短"写特判。
// 协议 §6.2 只给了有人的样例，这是客户端这边的读法（规格 §7 第 1 条）。
function seats4(list) {
  const out = [null, null, null, null];
  for (const s of list || []) {
    if (s && Number.isInteger(s.seat) && s.seat >= 0 && s.seat < 4) out[s.seat] = s;
  }
  return out;
}

// 牌河的元素统一成 {tile, riichi?}：增量事件本来就是对象，快照只给字符串。
function river(list) {
  return (list || []).map((t) => (typeof t === 'string' ? { tile: t } : { ...t }));
}

// 副露统一成 {kind, tiles:[码], calledIndex, closed, fromSeat}：横放的那张靠下标定位。
// 被鸣的那张服务端叫 taken（协议示例里叫 calledTile，快照里可能给下标 called），三个都收。
// tiles 服务端发的是**数组**（["5z","5z","5z"]），也兼容紧凑记法（"5z5z5z"）。
function meld(m) {
  const codes = Array.isArray(m.tiles) ? m.tiles.slice() : parseTiles(m.tiles ?? '');
  let idx = Number.isInteger(m.called) ? m.called : -1;
  if (idx < 0) {
    const taken = m.taken ?? m.calledTile;
    if (taken) idx = codes.indexOf(taken);
  }
  return { kind: m.kind, tiles: codes, calledIndex: idx, closed: m.closed === true, fromSeat: m.fromSeat };
}

function four(fn) {
  return [0, 1, 2, 3].map((i) => fn((i)));
}

// turnOf 归一化"轮到谁 + 我此刻能做什么"。
//
// 服务端正在给自己回合加合法性下发（协议里还没有这条）：`actions` 说能做什么，
// `kanClosed`/`kanAdded` 说哪几张牌能杠。字段可能挂在 `turn` 里（增量事件的形状），
// 也可能像 `awaitingMe` 那样挂在顶层（快照的形状）—— 两种都收。
//
// **收不到就是 null，不是空数组**：null = "服务端没发"，渲染层据此退回"轮到我就点亮"的
// 旧行为；空数组 = "服务端发了，一个动作都没有"（那就一个键都不该亮）。
function listOf(...vals) {
  for (const v of vals) if (Array.isArray(v)) return v.slice();
  return null;
}

function turnOf(d) {
  const t = d.turn;
  const seat = typeof t === 'number' ? t : (t && typeof t.seat === 'number' ? t.seat : null);
  if (seat === null) return null;
  const inner = t && typeof t === 'object' ? t : {};
  return {
    seat,
    deadlineMs: inner.deadlineMs ?? d.deadlineMs ?? null,
    actions: listOf(inner.actions, d.actions),
    kanClosed: listOf(inner.kanClosed, d.kanClosed),
    kanAdded: listOf(inner.kanAdded, d.kanAdded),
  };
}

// startGame 服务 game.started。**game.snapshot 另有形状**（见 snapshotGame）：
// 虽然协议 §6.3 说快照是 started 的超集，服务端实际发的不是 —— 数组 vs 紧凑记法、
// turn 是裸座位号、立直是 [4]bool。按"超集"去解会静默解出一个空视图。
function startGame(d, room) {
  return {
    gameId: d.gameId,
    roundIndex: d.roundIndex,
    roundWind: d.roundWind,
    handNumber: d.handNumber ?? 0,
    dealerSeat: d.dealerSeat,
    mySeat: d.mySeat ?? (room ? room.mySeat : null),
    hand: parseTiles(d.myHand ?? ''),
    // drawn 只对自家有意义：别家的摸牌不下发牌面（协议 §6.4）。
    drawn: null,
    scores: (d.scores || []).slice(),
    riichiSticks: d.riichiSticks ?? 0,
    remaining: d.remaining ?? null,
    doraIndicators: (d.doraIndicators || []).slice(),
    // 里宝牌指示牌单独一份：它和表宝牌张数相同，但只在立直且和牌时揭示
    // （协议 §6.8 的 roundEnd.uraIndicators），不能混进表宝牌那一行。
    uraIndicators: (d.uraIndicators || []).slice(),
    discards: four((i) => river((d.discards || [])[i])),
    melds: four((i) => ((d.melds || [])[i] || []).map(meld)),
    turn: turnOf(d),
    claim: d.claim ? claimOf(d.claim) : null,
    riichiSeats: (d.riichiSeats || []).slice(),
    ippatsuSeats: (d.ippatsuSeats || []).slice(),
    win: null,
  };
}

// snapshot 与 started **不是同一种形状**（服务端 snapshot.go 的 gameSnapshotView）：
// myHand/melds/discards 是数组，turn 是裸座位号，立直是 [4]bool，窗口选项叫 myOptions，
// 而且**没有 roundWind**。形状不同就不能共用一条解析路径 —— 上一版共用，结果是一补洞
// （seq 有洞 → game.sync）整个视图就空了：手牌没解出来、牌河变空、轮次读成 undefined。
function snapshotGame(d, room) {
  // 先按 started 的形状铺好公共字段（gameId/分数/宝牌/供托/剩余都同名），再逐项覆盖。
  const g = startGame({ ...d, myHand: '' }, room);
  g.hand = Array.isArray(d.myHand) ? d.myHand.slice() : parseTiles(d.myHand ?? '');
  // 牌河：服务端实现给的是**裸码**（["9m","1p"]），协议文档的例子里是 {tile, riichi} 对象。
  // river() 两种都收，所以这里复用它而不是自己 map。
  g.discards = four((s) => river(d.discards?.[s]));
  g.melds = four((s) => (Array.isArray(d.melds?.[s]) ? d.melds[s].map(meld) : []));
  // 立直在快照里是 [4]bool，在增量事件里是"谁立直了"。统一成一份座位表。
  g.riichiSeats = Array.isArray(d.riichi) ? [0, 1, 2, 3].filter((s) => d.riichi[s]) : (d.riichiSeats || []);
  g.turn = turnOf(d);
  // awaitingMe 是快照独有的：它说"现在等你自己打牌"。turn 只说明轮到谁。
  g.awaitingMe = d.awaitingMe === true;
  g.claim = d.claim ? claimOf({ ...d.claim, options: d.claim.myOptions || [] }) : null;
  return g;
}

function claimOf(d) {
  return {
    trigger: d.trigger,
    fromSeat: d.fromSeat,
    tile: d.tile,
    options: (d.options || []).slice(),
    deadlineMs: d.deadlineMs,
  };
}

// calibrate 记下服务端与本地的时差。
//
// 协议只给服务端当前时间、没有往返测量，所以这个偏移量会偏晚约半个 RTT ——
// 对"回合 5 秒 + 读秒 3×10 秒"的倒计时够用，不值得为它多打一次 ping。
function calibrate(view, serverTime) {
  if (typeof serverTime === 'number') view.clockOffsetMs = serverTime - Date.now();
}

// dropFromHand 处理"自己打出了一张牌"。
//
// 手牌是 13 张（暗牌）+ drawn 单独一张，不是 14 张数组 —— 这样渲染只要画
// "手牌 + 隔开的 drawn"，不用回头找哪张是刚摸的。所以打牌要把 drawn 并回手牌。
//
// 协议 §5.3 的 game.discard 只给牌名、不给下标，所以打的是哪一张必须靠 tsumogiri：
// 真了就是刚摸的那张，否则从手牌里删一张同名的 —— 同名副本本来就不可区分，
// 赤五用 0s/5s 区分，不受这条影响。
function dropFromHand(g, tile, tsumogiri) {
  if (tsumogiri === true || g.drawn === tile) {
    g.drawn = null;
    return;
  }
  const i = g.hand.indexOf(tile);
  if (i >= 0) g.hand.splice(i, 1);
  if (g.drawn !== null) {
    g.hand.push(g.drawn);
    g.drawn = null;
  }
}

// applyEvent 应用一条事件，返回同一个 view。
export function applyEvent(view, env) {
  if (!env || typeof env.type !== 'string') return view;
  const d = env.data || {};
  view.lastEvent = { seq: Number.isInteger(env.seq) ? env.seq : null, type: env.type };

  // seq 只在带 seq 的事件上推进。第一个带 seq 的事件当基线：房间的序号在我们连上
  // 之前可能已经在涨（room.join 之前房间就存在），那不是丢消息。
  if (Number.isInteger(env.seq)) {
    if (view.sync.lastSeq !== null && env.seq !== view.sync.lastSeq + 1) view.sync.needsSync = true;
    view.sync.lastSeq = env.seq;
  }

  switch (env.type) {
    case 'hello':
      view.protocolVersion = d.protocolVersion ?? 0;
      calibrate(view, d.serverTime);
      break;

    case 'authed':
      view.user = { userId: d.userId, username: d.username };
      break;

    case 'ping':
    case 'pong':
      calibrate(view, d.serverTime);
      break;

    case 'error':
      view.lastError = { code: d.code, message: d.message, ref: d.ref ?? null };
      break;

    case 'room.created':
      view.room = { roomId: d.roomId, mySeat: d.seat ?? 0, seats: seats4([]) };
      view.roomClosed = null;
      break;

    case 'room.joined':
      view.room = { roomId: d.roomId, mySeat: d.mySeat, seats: seats4(d.seats) };
      view.roomClosed = null;
      break;

    case 'room.seats':
      if (view.room) view.room.seats = seats4(d.seats);
      break;

    case 'room.readyChanged': {
      const s = view.room && view.room.seats[d.seat];
      if (s) s.ready = !!d.ready;
      break;
    }

    case 'room.left':
    case 'room.seatVacated': {
      // 座位空出来（主动离开或保座超时，协议 §4.5）。
      if (!view.room) break;
      view.room.seats[d.seat] = null;
      // 空的是**我自己的**座位：我不在这间房了，房间状态整个清掉 ——
      // 留着 roomId 会让牌桌继续画一间已经不属于我的房。
      if (d.seat === view.room.mySeat) view.room = null;
      break;
    }

    case 'room.closed':
      // 房间被服务端解散（服务端现在唯一的理由是有人掉线保座超时）。
      // 房间与对局一起清掉 —— 留着座位牌会让人以为还在房间里；
      // 但"为什么没了"要留下，否则界面只是莫名其妙回到大厅。
      view.room = null;
      view.game = null;
      view.result = null;
      view.roomClosed = { roomId: d.roomId ?? null, reason: d.reason ?? null };
      break;

    case 'game.started':
      view.game = startGame(d, view.room);
      view.result = null;
      break;

    case 'game.snapshot':
      view.game = snapshotGame(d, view.room);
      // 快照就是对账的结果：seq 的洞到此为止。
      view.sync.needsSync = false;
      break;

    case 'game.drew': {
      const g = view.game;
      if (!g) break;
      g.remaining = d.remaining ?? g.remaining;
      // tile 只出现在摸牌者自己收到的版本里（协议 §6.4）—— 别家只知道"他摸了一张"。
      // 摸到的那张不进 hand：手牌恒 13 张，drawn 是第 14 张，渲染时才隔开画。
      if (d.tile && d.seat === g.mySeat) g.drawn = d.tile;
      break;
    }

    case 'game.discarded': {
      const g = view.game;
      if (!g) break;
      g.remaining = d.remaining ?? g.remaining;
      g.discards[d.seat].push({ tile: d.tile, riichi: !!d.riichi });
      if (d.seat === g.mySeat) dropFromHand(g, d.tile, d.tsumogiri);
      break;
    }

    case 'game.riichi': {
      const g = view.game;
      if (!g) break;
      // §6.7：宣言牌「等同一条 game.discarded{riichi:true}」，且服务端不补发 discarded
      // （规格 §7 第 2 条定了是"代替"）—— 所以这里自己把它当一张打出的牌记。
      g.discards[d.seat].push({ tile: d.tile, riichi: true });
      if (d.seat === g.mySeat) dropFromHand(g, d.tile, false);
      break;
    }

    case 'game.riichiStick': {
      const g = view.game;
      if (!g) break;
      g.riichiSticks = d.sticks ?? g.riichiSticks;
      if (Array.isArray(d.scores)) g.scores = d.scores.slice();
      if (!g.riichiSeats.includes(d.seat)) g.riichiSeats.push(d.seat);
      break;
    }

    case 'game.ippatsu': {
      const g = view.game;
      if (!g) break;
      const i = g.ippatsuSeats.indexOf(d.seat);
      if (d.active && i < 0) g.ippatsuSeats.push(d.seat);
      if (!d.active && i >= 0) g.ippatsuSeats.splice(i, 1);
      break;
    }

    case 'game.meld': {
      const g = view.game;
      if (!g) break;
      g.remaining = d.remaining ?? g.remaining;
      g.melds[d.seat].push(meld(d));
      // 被鸣走的那张从牌河里消失（日麻惯例 + 参考实现 skip 并补位）。
      // 加杠/暗杠动的是自己已经摆出来的副露，不碰牌河。
      //
      // 字段名与 kind 按服务端**实际**发的：kind 只有 chi/pon/kan，被鸣的那张叫 taken
      // （不是协议示例里的 calledTile），暗杠用 closed:true 标出来。
      const taken = d.taken || d.calledTile;
      const fromRiver = d.closed !== true && (d.kind === 'chi' || d.kind === 'pon'
        || d.kind === 'kan' || d.kind === 'openKan');
      if (fromRiver) {
        const pile = g.discards[d.fromSeat] || [];
        for (let i = pile.length - 1; i >= 0; i--) {
          if (!taken || pile[i].tile === taken) {
            pile.splice(i, 1);
            break;
          }
        }
      }
      break;
    }

    case 'game.doraRevealed': {
      const g = view.game;
      if (!g) break;
      // 指示牌是**列表**、**追加不替换**、**不去重**：两张 3s 指示牌意味着宝牌翻倍，
      // 去重直接把番数算错（协议 §6.5）。全场开杠上限 4，所以表宝牌最多 5 张。
      // 服务端用 kind=dora/ura 区分（协议示例里的 isUra 它没发）。
      const isUra = d.isUra === true || d.kind === 'ura';
      (isUra ? g.uraIndicators : g.doraIndicators).push(d.indicator);
      break;
    }

    case 'game.claimWindow': {
      const g = view.game;
      if (!g) break;
      // 窗口只推给有资格的座位（协议 §6.6），所以收到就是"目前能这么做"。
      g.claim = claimOf(d);
      break;
    }

    case 'game.claimClosed': {
      const g = view.game;
      if (!g) break;
      // 窗口只认服务端的 claimClosed，客户端不自己判结束（协议 §6.6）。
      g.claim = null;
      break;
    }

    case 'game.turn': {
      const g = view.game;
      if (!g) break;
      // 服务端加了合法性下发之后，game.turn 会多带 actions / kanClosed / kanAdded ——
      // 这里整体交给 turnOf，字段有就有、没有就是 null（渲染层据此决定点不点亮）。
      g.turn = turnOf({ turn: { seat: d.seat, deadlineMs: d.deadlineMs }, ...d });
      break;
    }

    case 'game.winDeclared': {
      const g = view.game;
      if (!g) break;
      g.win = {
        // 服务端发的字段叫 winnerSeat（协议示例里是 seat），两个都收。
        seat: d.winnerSeat ?? d.seat,
        isTsumo: !!d.isTsumo,
        winningTile: d.winningTile,
        fromSeat: d.fromSeat ?? null,
        hand: d.hand ?? '',
        melds: d.melds || [],
      };
      break;
    }

    case 'game.roundEnd': {
      const g = view.game;
      view.result = { ...d, kind: 'roundEnd' };
      if (g) {
        if (Array.isArray(d.newScores)) g.scores = d.newScores.slice();
        if (typeof d.riichiSticks === 'number') g.riichiSticks = d.riichiSticks;
        // 里宝牌只在立直且和牌时一次性揭示（协议 §6.8）。
        if (Array.isArray(d.uraIndicators)) g.uraIndicators = d.uraIndicators.slice();
      }
      break;
    }

    case 'game.gameEnd':
      view.result = { ...d, kind: 'gameEnd' };
      break;

    default:
      // 未知事件忽略：服务端加事件不该把客户端打成白屏（协议 §2.1 的同一个判据）。
      break;
  }
  return view;
}
