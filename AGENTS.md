# Nulas project instructions

## Purpose and stack
Nulas is a browser-based quick configuration dashboard for https://github.com/MetaCubeX/mihomo/tree/Meta. It is an independent web frontend integrating the Mihomo REST API. The frontend uses SolidStart, SolidJS, TypeScript and CSS. The backend uses Go's standard library, an HTTP API, atomic JSON persistence and a single background worker. It generates Mihomo-compatible configuration and optionally applies it to an existing Mihomo controller.

## Commands
- `cd web && pnpm install --frozen-lockfile`: install locked frontend dependencies.
- `cd web && pnpm dev`: start the frontend at localhost:3000 (proxies /api to the Go backend).
- `cd web && pnpm typecheck`: check TypeScript.
- `cd web && pnpm build`: build the production frontend.
- Build the frontend, then run the Go service to serve production assets and API on the same port. `NULAS_WEB_DIR` selects the asset directory; do not introduce Nginx.
- `cd backend && go run .`: start the API at 127.0.0.1:8080.
- `cd backend && go test -race ./...`: run backend tests and race detection.
- `cd backend && go vet ./...`: inspect Go code.
- `python3 scripts/install_core.py`: download an official prebuilt core on demand into ignored `.runtime/core/`.
- `python3 -m unittest discover -s scripts`: verify runtime asset selection and extraction.
- `cd backend && go build -o ../bin/nulas .`: build the backend.

## Boundaries to preserve
- Keep SolidStart; do not replace it with React or add Tauri browser dependencies.
- Preserve the CLAUDE.md symlink to this file.
- Do not modify the upstream project, existing system proxy settings, firewall, routes or TUN devices as part of scaffolding.
- Do not overwrite user configuration or runtime data, commit secrets, vendor upstream Mihomo source, bundle a Mihomo binary, or silently claim a core operation succeeded.
- Keep controller credentials on the server. Keep loopback binding and same-origin API defaults; remote deployment requires separately implemented authentication and TLS.
- Preserve atomic persistence, bounded requests, validation, durable job snapshots and visible failure states. Background operations must not depend on an open browser.
- An interrupted running job must be marked failed on restart, never blindly replay an external side effect. Pending jobs may resume.
- This starter does not implement subscriptions, node management, privileged system proxy/TUN control, or full Mihomo configuration management. Document such limitations honestly.

## Completion and Git
Run relevant checks and document any unavailable verification. Commit completed work using `feat: <message>`, `fix: <message>` or `docs: <message>` with English messages. Add concise implementation details to the commit body when useful. Split very large tasks into coherent commits. After committing, report the commit hashes and messages to the user in a table. Do not amend or discard unrelated user work.
