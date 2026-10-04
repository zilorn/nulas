#!/usr/bin/env bash
set -euo pipefail

# Separate process groups let cleanup stop pnpm and all of its descendants.
set -m
ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
if (( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 3) )); then
  echo "开发脚本需要 Bash 4.3+。" >&2
  exit 1
fi
for tool in node pnpm go; do
  command -v "$tool" >/dev/null 2>&1 || { echo "缺少依赖：$tool（需要 Node.js 24+、pnpm、Go 1.23+）" >&2; exit 1; }
done
if (( $(node -p "process.versions.node.split('.')[0]") < 24 )); then
  echo "需要 Node.js 24+。" >&2
  exit 1
fi

cd "$ROOT_DIR/web"
pnpm install --frozen-lockfile
DEV_DIR="$(mktemp -d "${TMPDIR:-/tmp}/nulas-dev.XXXXXXXX")"
backend_pid=""
frontend_pid=""
cleanup() {
  trap - EXIT INT TERM
  for pid in "$backend_pid" "$frontend_pid"; do
    if [[ -n "$pid" ]]; then
      kill -TERM -- "-$pid" 2>/dev/null || true
    fi
  done
  for pid in "$backend_pid" "$frontend_pid"; do
    if [[ -n "$pid" ]]; then
      wait "$pid" 2>/dev/null || true
    fi
  done
  rm -rf -- "$DEV_DIR"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd "$ROOT_DIR/backend"
go build -o "$DEV_DIR/nulas" .
web_host="127.0.0.1"
if [[ "$("$DEV_DIR/nulas" config lan)" == "true" ]]; then web_host="0.0.0.0"; fi
web_addr="${NULAS_ADDR:-$web_host:$("$DEV_DIR/nulas" config port)}"
dev_port="$("$DEV_DIR/nulas" config dev-port)"
"$DEV_DIR/nulas" &
backend_pid=$!
cd "$ROOT_DIR/web"
pnpm dev &
frontend_pid=$!
printf '\n开发网页：http://127.0.0.1:%s\n后端 API：http://%s\n按 Ctrl+C 停止前后端；任一服务退出时会停止另一项服务。\n' "$dev_port" "$web_addr"
status=0
wait -n "$backend_pid" "$frontend_pid" || status=$?
echo "开发服务已退出（状态码：$status）。" >&2
exit "$status"
