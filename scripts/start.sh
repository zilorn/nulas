#!/usr/bin/env bash
set -euo pipefail

# Separate process groups let cleanup stop both services and all of their descendants.
set -m
ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
if (( BASH_VERSINFO[0] < 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] < 3) )); then
  echo "生产脚本需要 Bash 4.3+。" >&2
  exit 1
fi
for tool in node; do
  command -v "$tool" >/dev/null 2>&1 || { echo "缺少依赖：$tool（需要 Node.js 24+）" >&2; exit 1; }
done
if (( $(node -p "process.versions.node.split('.')[0]") < 24 )); then
  echo "需要 Node.js 24+。" >&2
  exit 1
fi
if [[ -n "${NULAS_WEB_DIR:-}" ]]; then
  echo "SSR 启动请取消 NULAS_WEB_DIR；该变量只用于旧版静态网页。" >&2
  exit 1
fi
[[ -x "$ROOT_DIR/bin/nulas" && -f "$ROOT_DIR/web/.output/server/index.mjs" ]] || {
  echo "请先运行 scripts/build.sh。" >&2
  exit 1
}
web_addr="${NULAS_ADDR:-127.0.0.1:$("$ROOT_DIR/bin/nulas" config port)}"
ssr_host="$(node "$ROOT_DIR/web/scripts/server-config.mjs" ssr-host)"
ssr_port="$(node "$ROOT_DIR/web/scripts/server-config.mjs" ssr-port)"
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
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

cd "$ROOT_DIR/backend"
"$ROOT_DIR/bin/nulas" &
backend_pid=$!
cd "$ROOT_DIR/web"
NITRO_HOST="$ssr_host" NITRO_PORT="$ssr_port" node .output/server/index.mjs &
frontend_pid=$!
printf '\n生产网页与 API：http://%s\n按 Ctrl+C 停止前后端；任一服务退出时会停止另一项服务。\n' "$web_addr"
status=0
wait -n "$backend_pid" "$frontend_pid" || status=$?
echo "生产服务已退出（状态码：$status）。" >&2
exit "$status"
