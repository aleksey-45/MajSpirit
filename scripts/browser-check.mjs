// 浏览器检查：用 Chrome 的 CDP 驱动真实渲染，验"人手点不出来"的那几件事 ——
// 四个关闭码在页面上的表现、牌桌视图到底画出来了没有、控制台有没有报错。
//
// 这台机器上没有 Playwright/Puppeteer，也不打算为它装一条 npm 链：Chrome 自带 CDP，
// 而 node 24 自带 WebSocket，两边一接就够了。
//
// 用法（两步，浏览器由外面起，脚本自己不 spawn，免得踩上沙箱对管道 stdio 的限制）：
//
//   "/c/Program Files/Google/Chrome/Application/chrome.exe" \
//     --headless=new --remote-debugging-port=9222 --user-data-dir=/tmp/mjs-chrome \
//     --no-first-run --no-default-browser-check about:blank &
//   bash scripts/serve.sh --bg
//   node scripts/browser-check.mjs
//
// 环境变量：CDP（默认 http://127.0.0.1:9222）、BASE（默认 http://127.0.0.1:8080）、
//           SHOTS（截图目录，默认 bin/shots）。

import { mkdirSync, writeFileSync } from 'node:fs';

const CDP = process.env.CDP || 'http://127.0.0.1:9222';
const BASE = process.env.BASE || 'http://127.0.0.1:8080';
// NO_NET=1 → 只跑不需要后端的那些（配合 `python -m http.server 8099 --directory web`
// 就能在没有服务端、甚至后端编译不过的时候验前端）。
const NET = !process.env.NO_NET;
const SHOTS = process.env.SHOTS || 'bin/shots';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const results = [];
const check = (name, ok, detail = '') => {
  results.push({ name, ok, detail });
  console.log(`${ok ? '✔' : '✖'} ${name}${detail ? '  ' + detail : ''}`);
};

// ── 极简 CDP 客户端：一个 page 目标，一条 WebSocket ──
async function cdp() {
  const list = await (await fetch(`${CDP}/json/list`)).json();
  const page = list.find((t) => t.type === 'page');
  if (!page) throw new Error('Chrome 里没有 page 目标（用 --headless=new 起一个 about:blank）');

  const ws = new WebSocket(page.webSocketDebuggerUrl);
  let id = 0;
  const pending = new Map();
  const listeners = [];
  const errors = [];

  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (msg.id && pending.has(msg.id)) {
      const { res, rej } = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) rej(new Error(JSON.stringify(msg.error)));
      else res(msg.result);
      return;
    }
    for (const fn of listeners) fn(msg);
  };
  await new Promise((res, rej) => {
    ws.onopen = res;
    ws.onerror = () => rej(new Error(`连不上 CDP：${page.webSocketDebuggerUrl}`));
  });

  const send = (method, params = {}) => {
    const i = ++id;
    ws.send(JSON.stringify({ id: i, method, params }));
    return new Promise((res, rej) => pending.set(i, { res, rej }));
  };
  const on = (fn) => listeners.push(fn);
  return { send, on, errors, close: () => ws.close() };
}

const main = async () => {
  mkdirSync(SHOTS, { recursive: true });

  // 先拿一个测试账号：页面的取票按钮走的是 /api/register + /api/login，
  // 这里也用同两个接口，省得把 TTY 输入也自动化（那不是被测对象）。
  const username = 'ui' + Date.now().toString(36);
  const post = async (path, body) => {
    const r = await fetch(BASE + path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    return { status: r.status, body: await r.text() };
  };
  // NO_NET=1：只验前端。前端本来就不需要后端 —— 样张（?fx=N）、结算、排版、命令行
  // 全是本地算出来的，那正是 ?fx 存在的意义。要验 /api 与 /ws 才需要 NET。
  let token = '';
  if (NET) {
    const reg = await post('/api/register', { username, password: 'Passw0rd-12345' });
    const login = await post('/api/login', { username, password: 'Passw0rd-12345' });
    check('测试账号可用（register 201 / login 200）', reg.status === 201 && login.status === 200, `register=${reg.status} login=${login.status}`);
    token = JSON.parse(login.body).token;
  }

  const c = await cdp();
  const errors = [];
  c.on((msg) => {
    if (msg.method === 'Runtime.exceptionThrown') {
      errors.push('exception: ' + (msg.params.exceptionDetails?.exception?.description || msg.params.exceptionDetails?.text));
    }
    if (msg.method === 'Runtime.consoleAPICalled' && msg.params.type === 'error') {
      errors.push('console.error: ' + msg.params.args.map((a) => a.value ?? a.description).join(' '));
    }
    if (msg.method === 'Log.entryAdded' && msg.params.entry.level === 'error') {
      errors.push(`log: ${msg.params.entry.text} (${msg.params.entry.url || ''})`);
    }
  });
  await c.send('Page.enable');
  await c.send('Runtime.enable');
  await c.send('Log.enable');
  await c.send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });

  const evalJs = async (expression) => {
    const r = await c.send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
    if (r.exceptionDetails) throw new Error('页面里抛了：' + JSON.stringify(r.exceptionDetails.exception?.description || r.exceptionDetails.text));
    return r.result.value;
  };
  const shot = async (name) => {
    const { data } = await c.send('Page.captureScreenshot', { format: 'png' });
    writeFileSync(`${SHOTS}/${name}.png`, Buffer.from(data, 'base64'));
  };
  const goto = async (url) => {
    await c.send('Page.navigate', { url });
    for (let i = 0; i < 60; i++) {
      await sleep(100);
      if (await evalJs('document.readyState === "complete"')) break;
    }
    await sleep(300); // module 脚本是 defer 的，等它跑完一轮
  };
  const setVal = (id, v) => evalJs(`(()=>{const e=document.getElementById(${JSON.stringify(id)});e.value=${JSON.stringify(v)};e.dispatchEvent(new Event('input'));return e.value})()`);
  const click = (id) => evalJs(`(()=>{document.getElementById(${JSON.stringify(id)}).click();return true})()`);
  const banner = () => evalJs('document.getElementById("close").textContent');
  const clearBanner = () => evalJs('(()=>{const b=document.getElementById("close");b.textContent="";b.classList.remove("show");return true})()');
  const waitBanner = async (ms = 8000) => {
    for (let i = 0; i < ms / 100; i++) {
      await sleep(100);
      const t = await banner();
      if (t && t.includes('关闭码')) return t;
    }
    return (await banner()) || '（没有关闭码）';
  };
  const stateText = () => evalJs('document.getElementById("state").textContent');
  const waitState = async (want, ms = 8000) => {
    for (let i = 0; i < ms / 100; i++) {
      await sleep(100);
      if ((await stateText()) === want) return true;
    }
    return false;
  };

  // ── 0. 页面本身：默认停在大厅；切到牌桌要画得出来、缩得对 ──
  await goto(BASE + '/');
  // 票存在 sessionStorage 里（这是客户端有意的：四标签页跑四账号）。CDP 这个标签跨运行
  // 活着，所以上一轮留下的票会让页面一加载就自动连上、并被服务端补发的 room.joined
  // 拉回牌桌 —— 先清掉，让"默认停在大厅"这条有确定的前提。
  await evalJs('sessionStorage.clear()');
  await goto(BASE + '/');
  check('页面加载：默认停在大厅（不是牌桌）',
    await evalJs('!document.getElementById("view-lobby").classList.contains("hidden")'));
  await click('tab-table');
  const moduleOK = await evalJs('typeof window !== "undefined" && !!document.querySelector("#zone .board")');
  check('牌桌视图画出了 .board', moduleOK);
  const size = await evalJs(`(()=>{
    const z=document.getElementById('zone'), b=z.querySelector('.board'), a=document.getElementById('actions');
    const bh = b ? Math.round(b.getBoundingClientRect().height) : 0;
    const bottom = Math.round(z.getBoundingClientRect().top + bh + a.offsetHeight);
    return { zone: z.clientWidth, boardH: bh, scale: b ? b.style.transform : '',
             bottom, innerHeight: window.innerHeight,
             noScroll: document.documentElement.scrollHeight <= window.innerHeight + 1 };
  })()`);
  // 方桌按 min(宽, 高) 缩：只按宽度缩的话，自家手牌（桌子最下面那排）要滚动才看得到。
  check('缩放：整张桌子 + 动作条都在视口内，页面不出滚动条',
    size.bottom <= size.innerHeight + 1 && size.noScroll,
    JSON.stringify(size));
  await shot('01-table-empty');

  // ── 1~5. 四个关闭码 + 调试日志（**要真服务端**：这四个码都是服务端关的）──
  if (NET) await closeCodeSteps(token);

  // @ts-ignore —— 提成函数只为 NO_NET 能整块跳过，不为了复用
  async function closeCodeSteps(token) {
  // ── 1. 4400：连上后发非法 JSON ──
  await setVal('token', token);
  await click('connect');
  check('连接成功（状态灯 = 已连接）', await waitState('已连接'));
  await clearBanner();
  await click('bad');
  const r4400 = await waitBanner(5000);
  check('4400 协议错误：页面显示关闭码', r4400.includes('4400'), r4400.trim());

  // ── 2. 4403：同一张票再开一条原始连接顶号 ──
  await setVal('token', token);
  await click('connect');
  await waitState('已连接');
  await clearBanner();
  await click('replace');
  const r4403 = await waitBanner(6000);
  check('4403 顶号：主连接被关且显示关闭码', r4403.includes('4403'), r4403.trim());

  // ── 3. 4401：不发 auth，5 秒后被关 ──
  await setVal('token', '');
  await clearBanner();
  await click('connect');
  const r4401 = await waitBanner(9000);
  check('4401 未认证：5 秒后关闭且显示关闭码', r4401.includes('4401'), r4401.trim());

  // ── 4. 4408：取消自动回 pong，最长 20 秒 ──
  await setVal('token', token);
  await evalJs('(()=>{const e=document.getElementById("autoPong");e.checked=false;e.dispatchEvent(new Event("change"));return e.checked})()');
  await clearBanner();
  await click('connect');
  await waitState('已连接');
  const r4408 = await waitBanner(26000);
  check('4408 心跳超时：显示关闭码', r4408.includes('4408'), r4408.trim());
  await evalJs('(()=>{const e=document.getElementById("autoPong");e.checked=true;e.dispatchEvent(new Event("change"));return true})()');

  // ── 5. 调试视图：日志与四个关闭码都记下来了吗 ──
  await click('tab-debug');
  const logRows = await evalJs('document.querySelectorAll("#log .entry").length');
  const closeRows = await evalJs('[...document.querySelectorAll("#log .entry.close")].map(e=>e.textContent).join(" | ")');
  check('调试视图：关闭事件都进了日志', logRows > 0 && /4400/.test(closeRows) && /4403/.test(closeRows) && /4401/.test(closeRows), `共 ${logRows} 行`);
  await shot('02-debug-log');
  }

  // ── 6. ?fx=1：整张牌桌（假事件）──
  await click('tab-table');
  await goto(BASE + '/?fx=1');
  const cells = await evalJs('document.querySelectorAll("#zone .cell").length');
  const backs = await evalJs('document.querySelectorAll("#zone .tb.back").length');
  const clickable = await evalJs('document.querySelectorAll("#zone .cell[data-tile]").length');
  const yoko = await evalJs('document.querySelectorAll("#zone .cell.yoko").length');
  const keyOn = await evalJs('document.querySelectorAll("#actions .key:not(.off)").length');
  const dora = await evalJs('document.querySelectorAll("#zone .center .dora .tb").length');
  check('牌桌渲染：4 家牌盒 / 牌背 / 可点手牌 / 横放', cells >= 50 && backs >= 30 && clickable === 14 && yoko >= 1,
    `cell=${cells} back=${backs} 可点=${clickable} 横放=${yoko}`);
  check('宝牌指示是列表（这里 3 张）与亮起的键帽', dora === 3 && keyOn === 3,
    `dora=${dora} 亮键=${keyOn}（自家手番：立直/摸切/杠）`);
  // 手番提示已删：那行字和座位牌上的"手番"都不该再有（高亮和读秒条负责表示手番）
  const hint = await evalJs('document.getElementById("hint")?.textContent ?? ""');
  const badgesText = await evalJs('[...document.querySelectorAll("#zone .badge")].map(b=>b.textContent).join("|")');
  check('自家手番的提示文案已删（绿框高亮与读秒条还在）',
    !/手番/.test(hint) && !/手番/.test(badgesText) && (await evalJs('!!document.querySelector("#zone .badge.active")')),
    `hint="${hint}"`);
  await shot('03-table-fx');

  // ── 7. 结算面板：?fx=2 和了（报菜名）/ ?fx=3 荒牌流局 ──
  await goto(BASE + '/?fx=2');
  const winText = await evalJs('document.getElementById("result").textContent ?? ""');
  const winCells = await evalJs('document.querySelectorAll("#zone .cell").length');
  check('结算 · 和了：报菜名 + 番符 + 点数 + 供托',
    /和了/.test(winText) && /立直 1 · 平和 1 · 断幺九 1 · 赤ドラ 1/.test(winText)
      && /30符 4番/.test(winText) && /8700点/.test(winText) && /归和牌者/.test(winText) && /里宝牌指示/.test(winText),
    winText.replace(/\s+/g, ' ').slice(0, 90));
  check('结算面板不遮住牌桌（四家的牌还在画）', winCells >= 50, `cell=${winCells}`);
  // 面板在不缩放的浮层里：宽度按窗口算（~min(94%,1000px)），不是 640×0.85 ≈ 544
  const panel = await evalJs(`(()=>{const p=document.querySelector('#result .result');const r=p.getBoundingClientRect();
    return {w:Math.round(r.width), fontSize:getComputedStyle(p).fontSize, scroll:p.scrollHeight>p.clientHeight};})()`);
  check('结算面板不随棋盘缩放（宽度按窗口算、字号 ≥16px）',
    panel.w > 800 && parseFloat(panel.fontSize) >= 16, JSON.stringify(panel));
  // 点数变化要"非常明显"：面板里四格大号数字 + 和牌者描金边，座位牌上再挂一份
  const deltaBoxes = await evalJs('document.querySelectorAll("#result .deltas .d").length');
  const winBox = await evalJs('document.querySelector("#result .deltas .d.win")?.textContent ?? ""');
  const badgeDeltas = await evalJs('document.querySelectorAll("#zone .badge .sd").length');
  check('结算 · 分数变化：面板四格大字 + 和牌者高亮 + 座位牌各挂一份',
    deltaBoxes === 4 && /\+8700/.test(winBox) && badgeDeltas === 2,
    `格子=${deltaBoxes} 和牌者格="${winBox}" 座位牌挂=${badgeDeltas}`);
  await shot('04-result-win');

  await goto(BASE + '/?fx=3');
  const drawText = await evalJs('document.getElementById("result").textContent ?? ""');
  check('结算 · 荒牌流局：听牌家 / 未听家 / 听牌料',
    /荒牌流局/.test(drawText) && /听牌 P0 P2/.test(drawText) && /未听 P1 P3/.test(drawText)
      && /待 3s 6s/.test(drawText) && /\+1500/.test(drawText),
    drawText.replace(/\s+/g, ' ').slice(0, 90));
  await shot('05-result-draw');

  // ── 7.4 服务端下发合法性（⑨）：轮到自家，但只有 actions 里列的动作能点 ──
  await goto(BASE + '/?fx=5');
  const acts = await evalJs(`(()=>{
    const on = [...document.querySelectorAll('#actions .key')].filter(k=>!k.classList.contains('off')).map(k=>k.dataset.cmd);
    return { on, kan: !!document.querySelector('#actions .key.k-kan') && !document.querySelector('#actions .key.k-kan').classList.contains('off') };
  })()`);
  check('服务端下发的 actions 赢过"轮到我全点亮"（没给 kan_* 杠就灰着）',
    acts.on.includes('riichi') && acts.on.includes('pass') && !acts.kan, JSON.stringify(acts));
  await shot('12-server-actions');

  // ── 7.5 房间被解散（服务端加的 room.closed）：牌桌清干净，但要说清"为什么" ──
  await goto(BASE + '/?fx=4');
  const closedText = await evalJs('document.querySelector("#zone .center")?.textContent ?? ""');
  const closedCells = await evalJs('document.querySelectorAll("#zone .cell").length');
  const closedBadges = await evalJs('document.querySelectorAll("#zone .badge").length');
  check('房间解散：中央面板解释原因、牌桌清干净、座位牌还在',
    /房间已关闭/.test(closedText) && /有人掉线/.test(closedText) && closedCells === 0 && closedBadges === 4,
    `${closedText.replace(/\s+/g, ' ').trim()} cell=${closedCells} badge=${closedBadges}`);
  await shot('07-room-closed');

  // ── 8. 两种窗口尺寸：宽的时候侧栏吃掉余量，窄的时候挪到桌子下面 ──
  await goto(BASE + '/?fx=1');
  const wide = await evalJs(`(()=>{const z=document.getElementById('zone'), s=document.querySelector('.col-side');
    const b=z.querySelector('.board'), r=b.getBoundingClientRect();
    return { zone:z.clientWidth, side:Math.round(s.getBoundingClientRect().width),
             boardW:Math.round(r.width), padLeft:Math.round(r.left - z.getBoundingClientRect().left), scale:b.style.transform };})()`);
  check('宽窗口：桌子居中、侧栏吃掉余量（不是固定 280）',
    wide.side > 300 && wide.padLeft > 10 && Math.abs(wide.padLeft * 2 + wide.boardW - wide.zone) < 3,
    JSON.stringify(wide));

  await c.send('Emulation.setDeviceMetricsOverride', { width: 900, height: 700, deviceScaleFactor: 1, mobile: false });
  await goto(BASE + '/?fx=1');
  const narrow = await evalJs(`(()=>{const z=document.getElementById('zone'), s=document.querySelector('.col-side');
    const b=z.querySelector('.board'), r=b.getBoundingClientRect();
    return { zone:z.clientWidth, boardW:Math.round(r.width), padLeft:Math.round(r.left - z.getBoundingClientRect().left),
             sideTop:Math.round(s.getBoundingClientRect().top), zoneBottom:Math.round(z.getBoundingClientRect().bottom),
             hScroll:document.documentElement.scrollWidth > window.innerWidth, scale:b.style.transform };})()`);
  // 窄而矮的窗口里"拿满宽度"是做不到的：高度才是瓶颈，桌子只能那么大。
  // 真正要保证的是：布局摞起来、桌子居中（两侧余量相等）、不出横向滚动。
  check('窄窗口：事件流挪到桌子下面、桌子居中、无横向滚动',
    narrow.sideTop >= narrow.zoneBottom - 2 && !narrow.hScroll
      && Math.abs(narrow.padLeft * 2 + narrow.boardW - narrow.zone) < 3,
    JSON.stringify(narrow));
  await shot('06-narrow');
  await c.send('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false });

  // ── 9. 命令行：help 有回话 ──
  await goto(BASE + '/?fx=1');
  await evalJs('(()=>{const b=document.getElementById("cmd");b.value="help";b.dispatchEvent(new KeyboardEvent("keydown",{key:"Enter",bubbles:true}));return true})()');
  const help = await evalJs('[...document.querySelectorAll(".js-log")][0].textContent');
  check('命令行：help 有回话', /discard/.test(help));

  // ── 10. 大厅：登录 → 开始游戏（只能按房间码）→ 历史牌局 ──
  await goto(BASE + '/');
  await shot('09-login');   // 未登录：只有账号和密码
  const beforeLogin = await evalJs(`(()=>({
    loginVisible: document.getElementById('login-box').offsetParent !== null,
    lobbyVisible: document.getElementById('lobby-box').offsetParent !== null,
  }))()`);
  check('登录页：只显示账号密码那一块，不显示大厅',
    beforeLogin.loginVisible && !beforeLogin.lobbyVisible, JSON.stringify(beforeLogin));
  await evalJs(`(()=>{document.getElementById('lobby-user').value=${JSON.stringify(username)};document.getElementById('lobby-pass').value='Passw0rd-12345';return true})()`);
  if (NET) {
    await click('lobby-login');
    let loggedIn = false;
    for (let i = 0; i < 50; i++) {
      await sleep(100);
      if ((await evalJs('document.getElementById("state").textContent')) === '已连接') { loggedIn = true; break; }
    }
    const who = await evalJs('document.getElementById("lobby-who").textContent');
    check('大厅：登录后自动连上 /ws，并显示当前账号', loggedIn && who === username.trim(), `${who}｜${await stateText()}`);
    // 登录成功后：账号密码那块收起来；大厅里**没有**密码框（密码只活在登录那一步）
    // 判据用 offsetParent（真的没渲染），不是 classList —— 只查 class 会在"类切了但没样式"
    // 的时候照样绿，那正是这里踩过的坑。
    const afterLogin = await evalJs(`(()=>({
      loginVisible: document.getElementById('login-box').offsetParent !== null,
      lobbyVisible: document.getElementById('lobby-box').offsetParent !== null,
      passInLobby: !!document.querySelector('#lobby-box input[type=password]'),
    }))()`);
    check('大厅：登录后只显示大厅（登录框真的收起来了），且大厅里没有密码框',
      !afterLogin.loginVisible && afterLogin.lobbyVisible && !afterLogin.passInLobby, JSON.stringify(afterLogin));

    await evalJs('(()=>{document.getElementById("lobby-room").value="ZZZZZZ";return true})()');
    await click('lobby-join');
    // 真回包：桥接上之后"房间号不存在"会由服务端回一条 error（ROOM_NOT_FOUND）。
    // 这一步曾经只能验"发出去了"，现在能验整条往返。
    let joinErr = '';
    for (let i = 0; i < 30; i++) {
      await sleep(100);
      joinErr = await evalJs('document.getElementById("lobby-msg").textContent');
      if (/ROOM_NOT_FOUND|不存在/.test(joinErr)) break;
    }
    check('大厅：开始游戏拿到服务端回包（房间不存在 → 提示不存在的房间）',
      /ROOM_NOT_FOUND|不存在/.test(joinErr), joinErr);

    // 建房走调试视图的命令行（大厅只有"按码加入"）→ room.created → 自动切牌桌
    await evalJs('(()=>{const b=document.getElementById("cmd");b.value="create";b.dispatchEvent(new KeyboardEvent("keydown",{key:"Enter",bubbles:true}));return true})()');
    let onTable = false;
    let roomId = '';
    for (let i = 0; i < 40; i++) {
      await sleep(100);
      onTable = await evalJs('!document.getElementById("view-table").classList.contains("hidden")');
      roomId = (await evalJs('(document.querySelector("#zone .center .l1")||{}).textContent || ""')).trim();
      if (onTable && /^[A-Z0-9]{6}$/.test(roomId)) break;
    }
    check('建房：room.created 一到就切牌桌，中央面板显示房间号',
      onTable && /^[A-Z0-9]{6}$/.test(roomId), `视图=${onTable ? '牌桌' : '不在牌桌'} 房间号="${roomId}"`);
    await shot('10-room-created');

    // F5（协议 §4.5 的重连）：票在 sessionStorage 里，刷新后应当
    //   服务端 resume 补发 room.joined（房间视图重建）→ 客户端主动补一次 game.sync。
    // 这一跳少了，症状就是"刷新后座位还在、牌桌一片空白"。
    await c.send('Page.reload');
    let backOnTable = false;
    let backRoomId = '';
    for (let i = 0; i < 50; i++) {
      await sleep(100);
      backOnTable = await evalJs('!document.getElementById("view-table").classList.contains("hidden")');
      backRoomId = (await evalJs('(document.querySelector("#zone .center .l1")||{}).textContent || ""')).trim();
      if (backOnTable && backRoomId === roomId) break;
    }
    const afterReloadLog = await evalJs('[...document.querySelectorAll(".js-log")].map(b=>b.textContent).join("")');
    check('刷新（F5）后回到原房间，并主动补了一次 game.sync',
      backOnTable && backRoomId === roomId && /game\.sync/.test(afterReloadLog),
      `视图=${backOnTable ? '牌桌' : '不在牌桌'} 房间号="${backRoomId}"（刷新前 "${roomId}"）`);
    await shot('11-after-reload');

    await click('lobby-history');
    await sleep(600);
    const histMsg = await evalJs('document.getElementById("lobby-msg").textContent');
    // 接口已经实现（阶段五）：新账号查到的是"还没有打过牌局"。判据写成"三种正当结果之一，
    // 且绝不是查询失败" —— 这样接口做了没做都成立，而真的出错（500）会当场红。
    check('大厅：历史牌局要么列出牌局、要么说清为什么没有（不是"查询失败"）',
      /还没有打过牌局|共 \d+ 局|阶段五/.test(histMsg) && !/查询失败/.test(histMsg), histMsg);
    await shot('08-lobby');
  }

  // 房间码不合法就地提示：**纯前端**分支（不碰网络），NO_NET 下也跑
  await evalJs('(()=>{document.getElementById("lobby-room").value="abc";return true})()');
  await click('lobby-join');
  const badCode = await evalJs('document.getElementById("lobby-msg").textContent');
  check('大厅：房间码不合法就地提示（不发指令）', /6 位/.test(badCode), badCode);

  // /api/history 是阶段五的接口，上面有一条用例**故意**去点它 —— 它的 404 是预期内的，
  // 不能因为"接口还没做"把控制台检查搞红。除此之外的任何报错都算失败。
  const expected404 = /\/(api\/history|api\/games)/;
  const realErrors = errors.filter((e) => !expected404.test(e));
  check('控制台没有报错（历史牌局的 404 是预期内的）', realErrors.length === 0,
    realErrors.slice(0, 3).join(' || ') || `干净（忽略 ${errors.length - realErrors.length} 条预期 404）`);
  c.close();

  const failed = results.filter((r) => !r.ok).length;
  console.log(`\n${results.length - failed}/${results.length} 通过${failed ? `，失败 ${failed}` : ''}`);
  console.log('截图：' + SHOTS);
  process.exit(failed ? 1 : 0);
};

main().catch((err) => {
  console.error('检查脚本自己崩了：', err);
  process.exit(2);
});
