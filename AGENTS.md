# Nulas project instructions

## Purpose and stack
Nulas is a browser-based quick configuration dashboard for https://github.com/MetaCubeX/mihomo/tree/Meta. It is an independent web frontend integrating the Mihomo REST API. The frontend uses Vite, SolidStart 2 with SSR, SolidJS, TypeScript and CSS. The backend uses Go's standard library plus go.yaml.in/yaml/v3 for configuration parsing, an HTTP API, atomic JSON persistence and a single background worker. It generates Mihomo-compatible configuration and optionally applies it to an existing Mihomo controller.

## Commands
- `cd web && pnpm install --frozen-lockfile`: install locked frontend dependencies.
- `cd web && pnpm dev`: start the frontend at localhost:4589 (proxies /api to the Go backend).
- `cd web && pnpm typecheck`: check TypeScript.
- `cd web && pnpm build`: build the production frontend.
- Node.js 24+ is required. Build the frontend and backend, then run `scripts/start.sh` to start both services. Go serves the API and proxies page requests to the loopback Node SSR service on port 4668. `NULAS_SSR_URL` selects the SSR origin; `NULAS_WEB_DIR` is an explicit legacy static fallback. Do not introduce Nginx.
- `cd backend && go run .`: start the API at 127.0.0.1:4669.
- `nulas config [port|ssr-port|dev-port] [PORT]`: inspect or save Web/API, production SSR and development frontend ports; restart to apply. All launchers read the same user configuration.
- `cd backend && go test -race ./...`: run backend tests and race detection.
- `cd backend && go vet ./...`: inspect Go code.
- `python3 scripts/install_core.py`: download an official prebuilt core on demand into ignored `.runtime/core/`.
- `python3 -m unittest discover -s scripts`: verify runtime asset selection and extraction.
- `cd backend && go build -o ../bin/nulas .`: build the backend.

## Cross-platform desktop support and saved settings
- Desktop tray integration must support Windows, macOS and Linux without replacing the browser frontend. Keep native event loops on the main thread, avoid shell-specific launch assumptions, and document desktop/session dependencies. Linux requires a tray host and GTK/AppIndicator; headless services cannot display a tray. Unsupported features must show their actual limitations (system proxy is currently Linux GNOME; TUN and startup management are currently Linux only).
- Store user switch preferences (tray, system proxy, TUN and startup) in the backend's atomic `state.json`, never solely in browser storage. Persist network switch intent together with the durable job before executing side effects, and startup intent before invoking system tools. Preserve older state files; absent preferences mean no recorded choice.
- Saved preferences and observed runtime state are separate. Read back system/core state, expose mismatch/failure, and never show saved intent as confirmed success. Restore the optional tray on backend startup, but do not automatically replay system proxy/TUN operations on restart; TUN starts disabled. Startup registration remains persisted by the OS. Keep proxy recovery snapshots until restoration succeeds; failure to persist must prevent new external actions.
- Optional tray dependencies: `python3 -m pip install -r scripts/requirements-tray.txt` (Windows: `python -m pip ...`). `NULAS_PYTHON` chooses the interpreter and `NULAS_TRAY_SCRIPT` chooses the helper path. Never auto-install desktop dependencies or change host networking for verification.

## Boundaries to preserve
- Keep SolidStart; do not replace it with React or add Tauri browser dependencies.
- Preserve the CLAUDE.md symlink to this file.
- Do not modify the upstream project, existing system proxy settings, firewall, routes or TUN devices as part of scaffolding.
- Do not overwrite user configuration or runtime data, commit secrets, vendor upstream Mihomo source, bundle a Mihomo binary, or silently claim a core operation succeeded.
- Keep controller credentials on the server. Keep loopback binding and same-origin API defaults; remote deployment requires separately implemented authentication and TLS.
- Preserve atomic persistence, bounded requests, validation, durable job snapshots and visible failure states. Background operations must not depend on an open browser.
- An interrupted running job must be marked failed on restart, never blindly replay an external side effect. Pending jobs may resume.
- Node management reads the connected core and selects members of existing manual proxy groups. Full Mihomo YAML/JSON profiles can be imported, generated and explicitly applied with durable document snapshots. Keep document credentials server-side and use loopback, non-privileged application settings without changing controller credentials. Explicit full-profile application may disable existing TUN/transparent-proxy sessions; explain this behavior in the UI. This starter does not implement subscription auto-refresh, node creation/editing/deletion, privileged system proxy control, external-controller or non-Linux TUN management, or full configuration editing. Document such limitations honestly.

## Completion and Git
Run relevant checks and document any unavailable verification. Commit completed work using `feat: <message>`, `fix: <message>` or `docs: <message>` with English messages. Add concise implementation details to the commit body when useful. Split very large tasks into coherent commits. After committing, report the commit hashes and messages to the user in a table. Do not amend or discard unrelated user work.
