#!/usr/bin/env bash
# 本地起服务端。等价于 `make run`，但这台机器没装 make，而 .env 是 bash 语法。
#
# 用法：
#   bash scripts/serve.sh            # 前台跑，Ctrl+C 停
#   bash scripts/serve.sh --bg       # 后台跑，日志进 bin/serve.log
#   bash scripts/serve.sh --stop     # 停掉后台那个
#
# 两个踩过的坑，决定了这个脚本长这样：
#
# 1. 不用 `go run`，先 go build 再跑二进制。
#    go run 会再套一层：真正的监听进程是它编出来的临时可执行文件，go run 自己的
#    PID 只是外壳。按外壳杀，端口照样占着 —— 实测 --stop 报「已停」而 healthz 还是 200。
#
# 2. 停的时候按**端口**找进程，不按 PID 文件。
#    Git Bash 的 `$!` 是 MSYS 的 PID（比如 89），而 powershell 的 Stop-Process 要的是
#    Windows PID（比如 24144）—— 两个命名空间，拿 MSYS PID 去杀必然失败。
#    "谁占着端口"是两边都认的事实，所以以它为准。
#
# 另外 main 刻意不提供任何默认配置（loadConfig 一次报出所有缺失项），
# 所以必须先读 .env；忘了读的症状是一句「缺少环境变量 ...」然后退出，那是设计不是 bug。
set -euo pipefail

cd "$(dirname "$0")/.."

BIN=bin/majspirit

if [ ! -f .env ]; then
  echo "没有 .env —— 照 .env.example 建一个（凭据见 AGENTS.md 的环境一节）" >&2
  exit 1
fi

# set -a：接下来定义的变量自动导出，供子进程读到。
set -a
# shellcheck disable=SC1091
. ./.env
set +a

PORT="${MAJSPIRIT_ADDR##*:}"   # MAJSPIRIT_ADDR 形如 ":8080" 或 "127.0.0.1:8080"

# listener_pid 输出占用该端口的 Windows PID（没有则空）。
listener_pid() {
  netstat -ano | grep LISTENING | grep ":${PORT} " | awk '{print $NF}' | head -1 || true
}

stop_server() {
  local pid
  pid=$(listener_pid)
  if [ -z "${pid:-}" ]; then
    echo "端口 $PORT 上没有进程"
    return
  fi
  powershell -NoProfile -Command "Stop-Process -Id $pid -Force" 2>/dev/null || true

  # 停完确认一次：报"已停"却还占着端口，是这个脚本第一版的真实症状。
  sleep 1
  if [ -z "$(listener_pid)" ]; then
    echo "已停 PID=$pid，端口 $PORT 已释放"
  else
    echo "PID=$pid 停不掉，端口 $PORT 仍被占用" >&2
    exit 1
  fi
}

case "${1:-}" in
  --stop)
    stop_server
    ;;

  --bg)
    mkdir -p bin
    echo "编译..."
    go build -o "$BIN" ./cmd/server
    nohup "$BIN" > bin/serve.log 2>&1 &

    for _ in $(seq 1 20); do
      sleep 1
      if curl -s -o /dev/null -m 1 "http://localhost:${PORT}/healthz"; then
        echo "已启动，监听进程 PID=$(listener_pid)，端口 $PORT"
        echo "日志：bin/serve.log"
        echo "停止：bash scripts/serve.sh --stop"
        exit 0
      fi
    done
    echo "20 秒内没起来，看 bin/serve.log：" >&2
    tail -20 bin/serve.log >&2
    exit 1
    ;;

  "")
    echo "前台运行（Ctrl+C 停）"
    go build -o "$BIN" ./cmd/server
    exec "$BIN"
    ;;

  *)
    echo "不认识的参数：$1（只支持 --bg / --stop）" >&2
    exit 1
    ;;
esac
