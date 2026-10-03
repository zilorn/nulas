#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
for tool in node pnpm go; do
  command -v "$tool" >/dev/null 2>&1 || { echo "缺少依赖：$tool（需要 Node.js 22+、pnpm、Go 1.23+）" >&2; exit 1; }
done

cd "$ROOT_DIR/web"
pnpm install --frozen-lockfile
pnpm typecheck
pnpm build

mkdir -p "$ROOT_DIR/bin"
cd "$ROOT_DIR/backend"
go build -o "$ROOT_DIR/bin/nulas" .
printf '\n构建完成：%s/bin/nulas\n网页产物：%s/web/.output/public\n启动：cd "%s/backend" && ../bin/nulas\n' "$ROOT_DIR" "$ROOT_DIR" "$ROOT_DIR"
