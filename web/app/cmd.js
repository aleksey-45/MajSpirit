// 命令行 → 协议指令。
//
// 「极客风」推到极致就是按钮越少、命令行越多；而对调试客户端来说，命令行还有个硬好处：
// **按钮布局还没做完的动作，靠命令行照样能发出去**（暗杠、加杠、换房间）。
//
// 纯函数：一行文本进，一条指令或一个提示出（指令由页面发）。
// 这里**不判断合法性** —— 能不能吃、能不能杠是服务端的事（协议 §9.3）。

const TILE = /^[0-9][mspz]$/;

const HELP = 'discard 7p · riichi 5p · chi 3s 4s · pon · kan closed 5p · win · pass · sync'
  + ' · create · join 7KQ2ZP · ready · start · help';

// kanKind 决定 game.kan 发哪种 wire 形式（协议 §5.3 的 kind）。
//
// 优先级三档，越靠前越"轮不到前端猜"：
//   1. 服务端给了牌表（kanClosed / kanAdded）→ 查表，查不到就返回 null（**别猜**）；
//   2. 服务端只给了动作（actions 里只有 kan_closed 或只有 kan_added）→ 按它定；
//   3. 都没有（旧服务端）→ 退回"自己碰过这张就是加杠"的推断。
//
// 第 3 档是**前端里的一份麻将逻辑**（复刻 mahjong.AddedKanTiles），只在旧服务端上才走。
// 服务端把牌表发下来之后，这一档就再也不执行了。
export function kanKind(tile, server, melds) {
  const closed = (server && server.kanClosed) || [];
  const added = (server && server.kanAdded) || [];
  if (closed.length || added.length) {
    if (added.includes(tile)) return 'added';
    if (closed.includes(tile)) return 'closed';
    return null; // 服务端列了牌，但没有这一张 —— 不猜
  }

  const acts = (server && server.actions) || [];
  const hasClosed = acts.includes('kan_closed');
  const hasAdded = acts.includes('kan_added');
  if (hasClosed !== hasAdded) return hasAdded ? 'added' : 'closed';

  const own = Array.isArray(melds) ? melds : [];
  return own.some((m) => m && m.kind === 'pon' && (m.tiles || []).includes(tile)) ? 'added' : 'closed';
}

// parseCommand 返回 {type, data, note?}：type 为 null 表示这条不发给服务端（只提示）。
// 认不出来就只回一句 help，不猜（发错指令比不发的代价大：服务端会当非法动作）。
export function parseCommand(line) {
  const parts = String(line ?? '').trim().split(/\s+/).filter(Boolean);
  if (!parts.length) return { type: null, note: HELP };
  const [head, ...rest] = parts;
  const cmd = head.toLowerCase();

  const tile = () => {
    const t = rest[0];
    return TILE.test(t || '') ? t : null;
  };

  switch (cmd) {
    case 'help':
    case '?':
      return { type: null, note: HELP };

    case 'discard': {
      const t = tile();
      return t ? { type: 'game.discard', data: { tile: t } } : { type: null, note: 'discard 后面要一张牌，如 7p / 0s' };
    }

    case 'riichi': {
      const t = tile();
      return t ? { type: 'game.riichi', data: { tile: t } } : { type: null, note: 'riichi 后面要一张宣言牌，如 5s' };
    }

    case 'chi': {
      const ts = rest.filter((t) => TILE.test(t));
      return ts.length === 2
        ? { type: 'game.chi', data: { tiles: ts } }
        : { type: null, note: 'chi 要两张手牌，如 chi 3s 4s（被吃的那张由服务端从牌河推出）' };
    }

    case 'pon':
      return { type: 'game.pon', data: {} };

    case 'kan': {
      // closed / added 需要 tile；open（大明杠）不需要 —— 牌在牌河里（协议 §5.3）。
      const kind = rest[0] && ['open', 'closed', 'added'].includes(rest[0]) ? rest[0] : 'open';
      const t = rest[1] && TILE.test(rest[1]) ? rest[1] : null;
      if (kind !== 'open' && !t) return { type: null, note: `kan ${kind} 要带上那张牌，如 kan ${kind} 5p` };
      return { type: 'game.kan', data: t ? { kind, tile: t } : { kind } };
    }

    case 'win':
      return { type: 'game.win', data: {} };

    case 'pass':
      return { type: 'game.pass', data: {} };

    case 'sync':
      return { type: 'game.sync', data: {} };

    case 'create':
      return { type: 'room.create', data: {} };

    case 'join': {
      const id = (rest[0] || '').toUpperCase();
      return /^[A-Z0-9]{6}$/.test(id) ? { type: 'room.join', data: { roomId: id } } : { type: null, note: 'join 后面要 6 位房间号，如 join 7KQ2ZP' };
    }

    case 'leave':
      return { type: 'room.leave', data: {} };

    case 'ready':
      return { type: 'room.ready', data: { ready: rest[0] !== 'false' && rest[0] !== '0' } };

    case 'start':
      return { type: 'room.start', data: {} };

    case 'ping':
      return { type: 'ping', data: {} };

    default:
      return { type: null, note: `不认识 ${head} —— ${HELP}` };
  }
}
