#!/usr/bin/env bash
# 全仓测试 + 竞态检测。上一级目录有 Makefile，但这台机器没装 make。
#
# 为什么要这个脚本：-race 需要 cgo，cgo 需要 C 编译器，所以必须显式开
# CGO_ENABLED=1（Windows 上默认是 0，不设的话 -race 直接报 "-race requires cgo"）。
#
# -p 1 的理由和 Makefile 里那条一样：internal/store 的用例会 DROP SCHEMA，
# 和别的包并行会偶发「关系 users 不存在」。
#
# 为什么自己找 go/gcc 而不直接敲命令：PowerShell 和 Git Bash 读的不是同一份 PATH
# （用户 PATH 与系统 PATH 的合并方式不同，见 AGENTS.md），所以「在 PowerShell 里
# 能跑 go test」不代表「bash scripts/test-race.sh 能跑」。脚本自己兜底，省得每次
# 都要先修环境。
set -euo pipefail

# 这个脚本是 bash 脚本，必须在 Git Bash 里跑。从 PowerShell 直接敲 ".\scripts\test-race.sh"
# 会走 Windows 的 .sh 关联（通常没有），或报一堆无法识别的命令——先明确拦一下。
if [ -z "${BASH_VERSION:-}" ]; then
  echo "这个脚本是 bash 脚本，请在 Git Bash 里跑：" >&2
  echo "  bash scripts/test-race.sh ./..." >&2
  exit 1
fi

# 更常见的一种：在 PowerShell 里敲 bash，解析到的是 WSL 的 bash（C:\Windows\System32\bash.exe），
# 于是一下子跑进了 Linux —— 那里没装 Go，工作区变成 /mnt/e/...，报出来的却是「找不到 go」，
# 看起来像环境坏了。实测踩过，所以在这里直接点名。
# 识别办法用 uname：Git Bash 报 MINGW64_NT-*，WSL 报 *-microsoft-standard-WSL*。
if [ -n "${WSL_DISTRO_NAME:-}${WSL_INTEROP:-}" ] || uname -r 2>/dev/null | grep -qi microsoft; then
  echo "检测到这是 WSL 的 bash，不是 Git Bash —— 项目在 Windows 上，这里跑不了。" >&2
  echo "开「Git Bash」窗口，或在 PowerShell 里用完整路径：" >&2
  echo '  & "D:\Tools\Git\bin\bash.exe" scripts/test-race.sh ./...' >&2
  exit 1
fi

# find_bin 名称 兜底路径... —— 先在 PATH 里找，找不到再试兜底路径。
find_bin() {
  local name=$1
  shift
  local p
  if p=$(command -v "$name" 2>/dev/null); then
    printf '%s' "$p"
    return 0
  fi
  for p in "$@"; do
    if [ -x "$p" ]; then
      printf '%s' "$p"
      return 0
    fi
  done
  return 1
}

GO=$(find_bin go \
  "/c/Program Files/Go/bin/go.exe" \
  "/c/Go/bin/go.exe" \
  "$HOME/AppData/Local/Programs/Go/bin/go.exe" \
  "/d/Program Files/Go/bin/go.exe") || {
  echo "找不到 go。装了的话把它的 bin 目录加进 PATH，或在脚本里补一条兜底路径。" >&2
  exit 1
}

GCC=$(find_bin gcc \
  "/d/Program Files/Dev-Cpp/MinGW 12.2/mingw64/bin/gcc.exe") || {
  echo "找不到 gcc —— -race 需要它。装在：" >&2
  echo '  D:\Program Files\Dev-Cpp\MinGW 12.2\mingw64\bin' >&2
  echo "改过用户 PATH 之后要新开一个 Git Bash 窗口才生效。" >&2
  exit 1
}

# gcc 不在 PATH 上时 cgo 找不到它，所以顺手把它那个目录加到 PATH 前面。
if ! command -v gcc >/dev/null 2>&1; then
  PATH="$(dirname "$GCC"):$PATH"
  export PATH
fi

# -count=1 绕过测试缓存：那次缓存会把「没跑」显示成「跑过了」。
CGO_ENABLED=1 "$GO" test -p 1 -race -count=1 "$@"
