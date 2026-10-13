// static-server.mjs —— 把 web/ 当静态站点发出来，**只有前端**，没有 /api 也没有 /ws。
//
// 为什么需要它：页面是 `go:embed` 进二进制的，所以前端改一行也得重新 `go build` 才看得见。
// 后端在改（编译不过）、或者只想验前端时，用它配 `NO_NET=1` 就能跑：
//
//   node scripts/static-server.mjs 8099 &
//   NO_NET=1 BASE=http://127.0.0.1:8099 node scripts/browser-check.mjs
//
// 三个路径映射**照抄 web.go**（`/app/`→app、`/tiles/`→public/tiles、`/`→index.html）——
// 映射写歪了，验的就不是真正会被服务端发出去的那份页面。
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { extname, join, normalize } from 'node:path';

const ROOT = fileURLToPath(new URL('../web/', import.meta.url));
const PORT = Number(process.argv[2] || 8099);
const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.json': 'application/json; charset=utf-8',
};

function resolvePath(urlPath) {
  const p = decodeURIComponent((urlPath || '/').split('?')[0]);
  if (p === '/' || p === '/index.html') return join(ROOT, 'index.html');
  if (p.startsWith('/app/')) return join(ROOT, 'app', p.slice('/app/'.length));
  if (p.startsWith('/tiles/')) return join(ROOT, 'public', 'tiles', p.slice('/tiles/'.length));
  if (p.startsWith('/public/')) return join(ROOT, 'public', p.slice('/public/'.length));
  return null;
}

createServer(async (req, res) => {
  const file = resolvePath(req.url);
  // 只发 web/ 底下的东西：不校验的话 `..%2f` 能读到仓库里任何文件
  if (!file || !normalize(file).startsWith(normalize(ROOT))) {
    res.writeHead(404).end('not found');
    return;
  }
  try {
    const body = await readFile(file);
    res.writeHead(200, { 'Content-Type': TYPES[extname(file)] || 'application/octet-stream', 'Cache-Control': 'no-store' });
    res.end(body);
  } catch {
    res.writeHead(404).end('not found');
  }
}).listen(PORT, '127.0.0.1', () => {
  console.log(`静态站点 http://127.0.0.1:${PORT}/ （只有前端：没有 /api 与 /ws）`);
});
