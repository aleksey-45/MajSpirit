#!/usr/bin/env bash
# 客户端纯函数用例（tiles.js / state.js）。
#
# 不挂进「验证四连」：那四条是 Go 的，而 Go 跑不了 JS；客户端正确性的最终判据
# 仍然是一次真实四人对局。这里只兜住 fold 里那几处"看起来正常工作"的错：
# seq 缺口、别家摸牌、快照覆盖、时钟校准、鸣牌窗口、被鸣的牌。
set -euo pipefail

cd "$(dirname "$0")/../web"

if ! command -v node >/dev/null 2>&1; then
  echo "找不到 node —— 这台机器上它在 nvm 里（v24.17.0），检查 PATH" >&2
  exit 1
fi

exec node --test
