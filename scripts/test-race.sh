#!/usr/bin/env bash
# 全仓测试 + 竞态检测。上一级目录有 Makefile，但这台机器没装 make。
#
# 为什么要这个脚本而不是手打：-race 需要 cgo，cgo 需要 C 编译器，
# 所以要显式开 CGO_ENABLED=1（默认是 0，不设的话 -race 直接报
# "-race requires cgo"）。编译器是 Dev-Cpp 自带的 MinGW-w64，已进用户 PATH。
#
# -p 1 的理由和 Makefile 里那条一样：internal/store 的用例会 DROP SCHEMA，
# 和别的包并行会偶发「关系 users 不存在」。
set -euo pipefail

if ! command -v gcc >/dev/null 2>&1; then
  echo "找不到 gcc —— -race 需要它。装在：" >&2
  echo '  D:\Program Files\Dev-Cpp\MinGW 12.2\mingw64\bin' >&2
  echo "新开一个 Git Bash 窗口即可（该目录已进用户 PATH）。" >&2
  exit 1
fi

# -count=1 绕过测试缓存：不缓存才跑得动，也才是真的重跑了一遍。
CGO_ENABLED=1 go test -p 1 -race -count=1 "$@"
