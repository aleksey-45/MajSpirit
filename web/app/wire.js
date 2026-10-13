// 线层 + 会话：连 WebSocket、收发信封、回 pong、按 seq 缺口补一次全量快照。
//
// 它同时持有 fold 的 view —— 分两个文件不换来任何可测性，而"谁该在 seq 有洞时
// 发 game.sync"这件事本身就是传输策略。
//
// 不碰 DOM：socket 由 openSocket 注入，所以这些策略能用假 socket 在 node 里测
// （回 pong、补同步、关闭码），不必开浏览器也不必起服务端。

import { newView, applyEvent } from './state.js';

// 协议 §4.6 的关闭码表。页面拿它把 4408 这种数字翻译成人话。
export const CLOSE_REASON = {
  1000: '正常关闭',
  1001: '对端离开',
  1006: '没有收到关闭帧（连接被直接掐断）',
  1009: '消息过大',
  1011: '服务端内部错误',
  4400: '协议错误（无法解析的消息）',
  4401: '未认证 / 认证失败',
  4403: '被服务端拒绝（同一账号在别处登录）',
  4408: '心跳超时',
};

export const closeReason = (code) => CLOSE_REASON[code] || '未知关闭码';

export function envelope(type, data) {
  return JSON.stringify({ type, data: data ?? {} });
}

// createSession 是整个客户端会话：一条连接 + 一份 view。
//
// onLog 收 {kind, text, raw}：kind ∈ up/down/sys/close/err，text 是给人看的一行，
// raw 是原始 JSON（调试视图要能看字节）。页面只管贴，不做判断。
// url 可以传字符串或函数 —— 页面上的地址框随时会被改，所以取的是连接那一刻的值。
export function createSession(opts) {
  const {
    url,
    openSocket = (u) => new WebSocket(u),
    onView = () => {},
    onLog = () => {},
  } = opts;
  const urlOf = typeof url === 'function' ? url : () => url;

  let view = newView();
  let ws = null;
  let state = 'idle'; // idle | connecting | open | closed
  // seq 补同步只补一次：洞不补上就一直要，但别每来一条事件都发一遍 game.sync。
  let syncArmed = true;
  // 调试页有一项就是"取消自动回 pong，看 4408"（实现指南第 4 步的验收动作）。
  let autoPong = true;

  const log = (kind, text, raw) => onLog({ kind, text, raw });

  // 这一条 room.joined 是不是我们自己要来的。服务端**重连时也会补发** room.joined
  // （internal/ws/handler.go 的 resume），两种来源要分开：回执不用补同步，补发的必须补。
  let joining = false;

  function send(type, data) {
    if (!ws || ws.readyState !== 1) return false;
    const text = envelope(type, data);
    if (type === 'room.join') joining = true;
    ws.send(text);
    log('up', type, text);
    return true;
  }

  function handle(raw) {
    let env;
    try {
      env = JSON.parse(raw);
    } catch {
      log('down', '(坏 JSON)', String(raw));
      return;
    }
    log('down', env.type, String(raw));
    applyEvent(view, env);

    if (env.type === 'ping' && autoPong) send('pong', { serverTime: Date.now() });

    // 重连（协议 §4.5）：服务端会补发 room.joined，让客户端把房间视图重建起来
    // （internal/ws/handler.go 的 resume 就是干这个的）。但对局那一半**只有客户端问** ——
    // resume 的注释写得很明白："协议 §4.5 的重连流程里只有客户端的 game.sync"。
    //
    // 页面刚刷新时 view.game 是 null，所以下面那条 seq 缺口的路根本不会触发；不在这里
    // 主动问一次，症状就是"刷新后座位还在、牌桌一片空白"。
    // 只对**服务端补发**的那条问：自己 join 的回执照样问一次也白问（服务端会回
    // ILLEGAL_ACTION"现在没有对局"），大厅里每进一次房就多一条噪声。
    if (env.type === 'room.joined') {
      const pushed = !joining;
      joining = false;
      if (pushed && !view.game) {
        log('sys', '回到房间 → 请求 game.sync（没对局时服务端会回"现在没有对局"）');
        send('game.sync', {});
      }
    }

    // 协议 §4.4：seq 有洞就请全量快照，不猜缺了什么。只在牌局里发 ——
    // 不在牌局时服务端只会回一个 NOT_IN_ROOM。
    if (view.sync.needsSync && syncArmed && view.game) {
      syncArmed = false;
      log('sys', 'seq 有洞 → 请求 game.sync');
      send('game.sync', {});
    }
    if (!view.sync.needsSync) syncArmed = true;

    onView(view, env);
  }

  return {
    get view() {
      return view;
    },
    get state() {
      return state;
    },
    set autoPong(on) {
      autoPong = !!on;
    },

    // connect 用一张票开一条连接；票为空就只连不认证（那是 4401 的演示动作）。
    connect(token) {
      if (ws) {
        try {
          ws.close(1000, '换一条连接');
        } catch {
          /* 已经关了 */
        }
      }
      view = newView();
      syncArmed = true;
      state = 'connecting';
      ws = openSocket(urlOf());
      ws.onopen = () => {
        state = 'open';
        log('sys', '已升级到 WebSocket');
        if (token) send('auth', { token });
        else log('sys', '票是空的，跳过 auth（5 秒后会被 4401 关掉）');
      };
      ws.onmessage = (ev) => handle(ev.data);
      ws.onclose = (ev) => {
        state = 'closed';
        log('close', `关闭 code=${ev.code} ${closeReason(ev.code)}`);
      };
      ws.onerror = () => log('err', '连接出错，细节看浏览器控制台与服务端日志');
    },

    // sendRaw 给调试页用：它要能发坏 JSON（那是 4400 的演示动作）。
    sendRaw(text) {
      if (!ws || ws.readyState !== 1) return false;
      ws.send(text);
      log('up', '(原样)', text);
      return true;
    },

    send,

    close() {
      if (ws) {
        try {
          ws.close(1000, '页面主动断开');
        } catch {
          /* 已经关了 */
        }
      }
    },
  };
}
