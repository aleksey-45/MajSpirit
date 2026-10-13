// scripts/room-smoke.mjs —— 四个 bot 账号在真服务端上打完整一场（房间 → 对局 → 顺位）。
//
// 四个 bot 只会两件事：自己的摸牌摸切，鸣牌窗口一律过。四小局都走荒牌流局，
// 不需要任何决策，但房间、发牌、摸打、鸣牌窗、听牌料、连庄、顺位、gameEnd 全会走到。
//
// 用法（先起服务端，再跑它）：
//
//   bash scripts/serve.sh --bg
//   node scripts/room-smoke.mjs
//
// 环境变量：BASE（默认 http://127.0.0.1:8080）。退出码非 0 表示没打完。

const BASE = process.env.BASE || 'http://127.0.0.1:8080';
const WS_URL = BASE.replace(/^http/, 'ws') + '/ws';
const TIMEOUT_MS = Number(process.env.TIMEOUT_MS || 90000);

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const stamp = Date.now().toString(36);

function log(text) {
  console.log(text);
}

// ── HTTP：注册 + 登入 ───────────────────────────────────────────────────

async function post(path, body, token) {
  const res = await fetch(BASE + path, {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
      ...(token ? { authorization: `Bearer ${token}` } : {}),
    },
    body: JSON.stringify(body),
  });
  return res;
}

// makeAccount 注册一个新账号并登入，返回 token。
//
// 用户名带时间戳：同一个库上反复跑这个脚本不该因为重名而失败。
async function makeAccount(i) {
  const username = `smk${stamp}${i}`;
  const password = 'smoke-pass-1234';

  const reg = await post('/api/register', { username, password });
  if (!reg.ok && reg.status !== 409) {
    throw new Error(`注册 ${username} 失败: HTTP ${reg.status} ${await reg.text()}`);
  }
  const login = await post('/api/login', { username, password });
  if (!login.ok) {
    throw new Error(`登入 ${username} 失败: HTTP ${login.status} ${await login.text()}`);
  }
  return { username, token: (await login.json()).token };
}

// ── 连接：信封 + 按序取事件 ─────────────────────────────────────────────

function connect(token) {
  const sock = new WebSocket(WS_URL);
  const queue = [];
  const state = { gaps: [], lastSeq: 0, closed: null };

  sock.addEventListener('message', (ev) => {
    const env = JSON.parse(ev.data);
    // 心跳回执（协议 §4.3）。不回的话 15 秒后服务端按 4408 关连接 ——
    // 一局要跑十几秒，这条不是可选项。
    if (env.type === 'ping') {
      sock.send(JSON.stringify({ type: 'pong', data: {} }));
      return;
    }
    // seq 按收件人各自连续（协议 §1.4）；有洞就说明这条连接真的漏了消息。
    if (env.seq) {
      if (env.seq !== state.lastSeq + 1) state.gaps.push(`${state.lastSeq} → ${env.seq}`);
      state.lastSeq = env.seq;
    }
    queue.push(env);
  });
  sock.addEventListener('close', (ev) => {
    state.closed = { code: ev.code, reason: ev.reason };
  });

  const c = {
    state,
    send(type, data = {}) {
      sock.send(JSON.stringify({ type, data }));
    },
    // next 按**到达顺序**取下一条满足条件的事件。
    async next(what, pred, ms = 10000) {
      const until = Date.now() + ms;
      for (;;) {
        const i = queue.findIndex((env) => pred(env));
        if (i >= 0) return queue.splice(i, 1)[0];
        if (state.closed) {
          throw new Error(`等 ${what} 时连接关了：${JSON.stringify(state.closed)}`);
        }
        if (Date.now() > until) throw new Error(`等 ${what} 超时`);
        await sleep(2);
      }
    },
    // take 取下一条事件（不挑类型），bot 的驱动循环用它。
    take(ms = 10000) {
      return c.next('下一条事件', () => true, ms);
    },
    close() {
      try {
        sock.close();
      } catch {
        /* 已经关了 */
      }
    },
  };
  return c;
}

async function authed(c, token) {
  const hello = await c.next('hello', (e) => e.type === 'hello');
  if (hello.data.protocolVersion !== 1) {
    throw new Error(`协议版本 = ${hello.data.protocolVersion}，期望 1`);
  }
  c.send('auth', { token });
  const ok = await c.next('authed', (e) => e.type === 'authed' || e.type === 'error');
  if (ok.type !== 'authed') throw new Error(`认证失败：${JSON.stringify(ok.data)}`);
  return ok.data;
}

// ── 四个 bot ────────────────────────────────────────────────────────────

async function main() {
  const accounts = [];
  for (let i = 1; i <= 4; i++) accounts.push(await makeAccount(i));
  log(`账号就绪：${accounts.map((a) => a.username).join(' ')}`);

  const conns = accounts.map((a) => connect(a.token));
  const me = [];
  for (let i = 0; i < 4; i++) me.push(await authed(conns[i], accounts[i].token));
  log(`四人已认证：${me.map((u, i) => `${accounts[i].username}(#${u.userId})`).join(' ')}`);

  // ── 大厅 ──
  conns[0].send('room.create');
  const created = await conns[0].next('room.created', (e) => e.type === 'room.created');
  const roomId = created.data.roomId;
  log(`房间 ${roomId} 已创建（房主 = ${accounts[0].username}，seat 0）`);

  for (let i = 1; i < 4; i++) {
    conns[i].send('room.join', { roomId });
    const joined = await conns[i].next('room.joined', (e) => e.type === 'room.joined');
    if (joined.data.mySeat !== i) {
      throw new Error(`seat ${i} 的 room.joined.mySeat = ${joined.data.mySeat}`);
    }
    log(`  ${accounts[i].username} 加入 → seat ${joined.data.mySeat}`);
  }

  for (let i = 0; i < 4; i++) {
    conns[i].send('room.ready', { ready: true });
    await conns[i].next('自己准备好了', (e) => {
      return e.type === 'room.readyChanged' && e.data.seat === i && e.data.ready === true;
    });
  }
  log('四家已准备');

  conns[0].send('room.start');
  for (let i = 0; i < 4; i++) {
    const started = await conns[i].next('game.started', (e) => e.type === 'game.started');
    if (started.data.mySeat !== i) throw new Error(`game.started.mySeat = ${started.data.mySeat}`);
    if (i === 0) {
      log(
        `开局：第 ${started.data.roundIndex} 小局，庄家 seat ${started.data.dealerSeat}，` +
          `牌山余 ${started.data.remaining}，宝牌指示 ${JSON.stringify(started.data.doraIndicators ?? [])}`,
      );
      log(`  座位 0 的手牌：${started.data.myHand}`);
    }
  }

  // ── 打完整一场 ──
  const summary = { discards: 0, rounds: 0, ended: new Set(), placements: null };
  const done = new Promise((resolve) => {
    const check = () => {
      if (summary.ended.size === 4) resolve();
    };

    conns.forEach((c, seat) => {
      (async () => {
        for (;;) {
          const env = await c.take(TIMEOUT_MS);
          switch (env.type) {
            case 'game.drew':
              if (env.data.tile) {
                summary.discards++;
                c.send('game.discard', { tile: env.data.tile });
              }
              break;
            case 'game.claimWindow':
              c.send('game.pass');
              break;
            case 'game.riichi':
              break;
            case 'game.roundEnd': {
              summary.rounds++;
              const d = env.data;
              const scores = d.newScores ? `分数 ${d.newScores.join(' / ')}` : '';
              log(`第 ${d.roundIndex ?? '?'} 小局结束（${d.endedBy}）${scores}`);
              break;
            }
            case 'game.gameEnd': {
              const d = env.data;
              summary.ended.add(seat);
              if (!summary.placements) {
                summary.placements = d.placements;
                log(`终局：顺序 ${d.placements.join(',')}　最终分 ${d.finalScores.join(' / ')}`);
                log(`  ポイント ${d.points.join(' / ')}（和应当为 0）`);
              }
              check();
              return;
            }
            case 'room.closed':
              throw new Error(`房间被服务端关掉了：${JSON.stringify(env.data)}`);
            case 'error':
              throw new Error(`服务端报错：${JSON.stringify(env.data)}`);
            default:
              break;
          }
        }
      })().catch((err) => {
        console.error(`seat ${seat}: ${err.message}`);
        process.exitCode = 1;
        resolve();
      });
    });
  });

  const timeout = sleep(TIMEOUT_MS).then(() => {
    if (summary.ended.size < 4) {
      console.error(`超时：只有 ${summary.ended.size} 家收到 game.gameEnd`);
      process.exitCode = 1;
    }
  });
  await Promise.race([done, timeout]);

  // ── 结果 ──
  let gaps = 0;
  for (const c of conns) {
    gaps += c.state.gaps.length;
    for (const g of c.state.gaps) console.error(`seq 有洞：${g}`);
    c.close();
  }

  const ok = summary.ended.size === 4 && gaps === 0 && !process.exitCode;
  log(
    `合计：${summary.rounds} 次 roundEnd（四家各四小局 = 16）、${summary.discards} 次摸切、` +
      `seq 洞 ${gaps} 个`,
  );
  log(ok ? '全部通过：四个账号在真服务端上打完了整场' : '有失败项，见上面的输出');
  if (!ok) process.exitCode = 1;
}

main().catch((err) => {
  console.error(err.message);
  process.exitCode = 1;
});
