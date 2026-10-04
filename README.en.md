# Nulas

[简体中文](README.md) · **English**

A browser-based quick configuration dashboard for [MetaCubeX/mihomo · Meta](https://github.com/MetaCubeX/mihomo/tree/Meta). The frontend uses **Vite + SolidStart 2 (SSR) + SolidJS + TypeScript**, and the backend uses the **Go** standard library to read and change core runtime parameters through the Mihomo REST API. Nulas is an unofficial downstream project: it does not copy upstream source and does not bundle a core binary.

## Introduction

- **Quick configuration**: Edit mixed-port, mode, allow-lan, ipv6 and log-level, generate a profile and apply it to the core.
- **Profiles**: Import local files, paste JSON / top-level scalar YAML, or download a full Mihomo profile from an HTTP/HTTPS URL, with immediate and scheduled refreshes, and search, load, generate or apply by name.
- **Nodes**: Read the connected core, select members of manual proxy groups (Selector), switch between rule / global / direct modes and show the MATCH fallback outbound, with latency tests for a single node and for the current group in bulk.
- **Background tasks**: Install, generate, apply, TUN and system proxy operations enter a durable single-worker queue that survives closing the page.
- **System settings**: TUN, system proxy, start at login and desktop tray switches, with preferences written to the backend's atomic state file.
- **Managed core**: Downloads an official core on demand and starts it with its minimal direct configuration; setting `MIHOMO_CONTROLLER` connects to an existing core instead.

Core credentials stay on the server and the API listens on the loopback address by default. This version targets local use; the authentication and TLS required for remote deployment are not implemented.

## Installation

### Quick install (Windows / macOS / Linux)

Existing dependencies that meet the version requirements are reused instead of being installed again. The installer clones this repository, installs the locked frontend dependencies, checks types, builds the frontend and backend, and finally adds `nulas` to the user PATH. It does not start services, enable the tray or change host networking.

Linux / macOS (runs under bash, zsh or fish):

```sh
curl -fsSL https://raw.githubusercontent.com/zilorn/nulas/main/scripts/install.sh | bash
```

Windows PowerShell:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/zilorn/nulas/main/scripts/install.ps1 -OutFile "$env:TEMP\nulas-install.ps1"
& "$env:TEMP\nulas-install.ps1"
```

You can also download and inspect the script first:

```sh
curl -fsSL https://raw.githubusercontent.com/zilorn/nulas/main/scripts/install.sh -o /tmp/nulas-install.sh
sh /tmp/nulas-install.sh
```

The entry point is a shell script; it prepares Python as needed, and the Python standard library then handles cross-platform downloads, builds and updates. Piping supports interactive dependency installation, and a non-interactive environment without administrator rights fails explicitly.

You can also clone the repository, inspect the scripts and then run `sh scripts/install.sh` or `./scripts/install.ps1`. Those URLs require the corresponding code to be published on the GitHub `main` branch. If the PowerShell execution policy blocks scripts, allow the inspected installer to run according to your local policy.

- **Platform detection**: Supports x64 and arm64 on Linux / macOS / Windows. On Linux it installs missing Git / Python through apt (Ubuntu / Debian), pacman (Arch), dnf (Fedora), zypper (openSUSE) or apk (Alpine); package manager operations may require sudo. On musl environments such as Alpine, install a suitable Node.js and Go version beforehand; the automatically downloaded official Linux Node.js packages require glibc.
- **Dependency reuse**: Uses existing Git, Python 3.10+, Node.js 24+ and Go 1.23+. pnpm is reused only when its version exactly matches the current `packageManager`; otherwise it is installed into a separate build directory for that version. When a suitable Node or Go is missing, the official package is downloaded with SHA256 verification and installed inside the Nulas directory without overwriting system versions.
- **Platform package managers**: On macOS, missing Git / Python is installed with Homebrew, and a missing Homebrew triggers its [official installer](https://docs.brew.sh/Installation) (which may request administrator authorization and install the Xcode command line tools). On Windows, missing Git / Python is installed with winget (the system must already have App Installer; the installer may trigger system permission prompts). Desktop tray dependencies are never installed automatically.
- **Install directory**: Defaults to `~/.local/share/nulas` on Linux / macOS and `%LOCALAPPDATA%\Nulas` on Windows. On Unix use `sh scripts/install.sh --home /absolute/path`; in PowerShell use `./scripts/install.ps1 -HomeDir 'D:\Apps\Nulas'`. Use `--repository` / `--branch` (`-Repository` / `-Branch` in PowerShell) to choose a source; updates always track the branch selected at install time.
- **PATH**: On Unix, existing configuration is preserved: the path is appended to bash's `.bashrc`, `.profile` / existing login configuration, zsh's `${ZDOTDIR:-$HOME}/.zshrc`, and fish's `conf.d/nulas.fish`. On Windows it writes to the current user PATH and broadcasts the environment change. Reopen your terminal; already running parent terminals may need a restart. The installer and `nulas run` work without Bash 4.3.

After installation:

```sh
nulas run                      # Run the frontend and backend in the foreground on any platform; Ctrl+C stops it
# Optional user-level systemd on Linux:
nulas install
nulas start
```

Open [http://127.0.0.1:4669](http://127.0.0.1:4669). The quick-installed Linux service uses a stable launcher that reads the newest built version on restart. Windows / macOS do not currently offer background service installation; use `nulas run` there.

### Uninstall

```sh
nulas uninstall                # Linux: stop the service, disable start at login and remove the user-level service
nulas remove                   # Remove the quick-installed software; on Linux it also removes the matching service
```

`uninstall` keeps the software and all configuration, and you can reinstall the service later with `nulas install`. `remove` supports Linux / macOS / Windows and deletes only `source/`, `releases/`, `tools/`, `bin/` and `installation.json` from the quick-install directory, and cleans up the current-user PATH entries added by the installer. It keeps `data/`, `runtime/`, user port configuration, `service.env` and other files, and does not uninstall shared system dependencies. Reopen the terminal for the PATH change to take effect. On Windows use the `nulas.cmd` generated by the installer; when an older launcher reports that a refresh is required, run `nulas update` once and retry.

Before uninstalling, stop foreground `nulas run` and `nulas update --watch` with Ctrl+C and disable any enabled system proxy / TUN in the page. Uninstalling does not change system network settings. It returns a non-zero exit code when service removal or software file deletion fails instead of reporting success. It refuses to delete development repositories, services installed by other installations, symlinked software directories, and directories that are currently being installed or updated; a manually built development repository only supports `uninstall`, and you manage its software files yourself. The retained data can be reused by a later installation, and a complete data wipe requires a separate backup and manual deletion.

### Update discovery and automatic updates

```sh
nulas update --check            # Fetch the remote branch and show the current / latest commit without building or switching
nulas update                    # Download and build the new version, then switch atomically on success
nulas update --auto on          # Persistently enable automatic updates
nulas update --auto off         # Disable automatic updates (off by default)
nulas update --auto status      # Show the automatic update preference, installed commit and latest failure
nulas update --watch            # Standalone foreground update scheduler, useful when only the backend runs
```

**Software updates** in the page sidebar provides quiet checks, an update-available notice and **Install update**. The page checks automatically when opened (the backend limits this to at most once every 6 hours based on the last check or task, and failures do not trigger frequent retries); manual checks are not limited. Checks and installs run as durable background tasks and continue after the browser closes; progress, failures and restart notices appear only in the sidebar, with no popups, no automatic navigation, no automatic installation and no automatic restarts. Installation still uses the existing separate build directory and atomic switch mechanism and requires a manual restart to take effect; task details are also available on the background tasks page. Manually built versions show the update limitation.

The quick install provides the commands above; a manually built development repository keeps updating and building itself, or you can use the quick install to create a separate managed installation.

`nulas run` (including the Linux service generated by the quick install) includes the update scheduler: it reads saved preferences at startup and, when enabled, checks immediately and then checks and installs new commits every 6 hours. Scheduled updates stop when the service or scheduler stops; preference changes made while running take effect at the next check. Automatic update failures are written to the installation metadata and shown in the log.

Updates build in a separate checkout under `releases/`; a failed type check or build keeps the current version. Remote branch history rewrites are rejected, and a directory lock prevents concurrent installations. After success, old versions and failed build directories are kept, the running service keeps using the original version, and the change takes effect after `nulas restart` (Linux service) or stopping and rerunning `nulas run`. The service is never restarted automatically; after a restart, saved enabled preferences restore the system proxy / TUN.

Configuration and tasks live in the installation directory's `data/` and the core lives in `runtime/core/`, separate from version directories; explicitly set `NULAS_DATA_DIR` / `NULAS_CORE_DIR` still take precedence. Installation metadata and the automatic update preference are stored in `installation.json`. Do not delete the current version directory manually; old versions are not cleaned up automatically so they remain available for recovery. If an install / update is forcibly interrupted, first confirm that no install process is running, then inspect and clean `.update-lock`; interrupted updates are not replayed automatically. A failed first installation leaves the files in place; after fixing dependencies, inspect and move the unfinished installation directory aside before retrying instead of overwriting an existing installation.

Automatic updates compile and run new code from the selected repository branch, so enable them only for trusted sources. They update Nulas; the Mihomo core is still installed on demand through the existing mechanism.

### Requirements

| Dependency | Version / notes |
| --- | --- |
| Node.js | 24+ (development, builds and the SSR runtime) |
| pnpm | Matches `packageManager` in `web/package.json` |
| Go | 1.23+ |
| Python 3 | Downloads the core on demand, installs the user-level service and runs the tray helper script |
| Bash | 4.3+ (start scripts; use a newer Bash when the macOS system Bash is too old) |
| systemd | Optional; needed only for the Linux user-level background service and start at login |

Installing and building never start services and never download or run a core.

### Build

```sh
./scripts/build.sh
```

The script installs locked dependencies, runs the frontend type check, builds the client assets, the SSR service and the Go backend, and produces `web/.output/` and `bin/nulas`.

### Downloading the core on demand

When you already have a core you can connect to it directly without downloading. To download one:

```sh
python3 scripts/install_core.py
# Or pin an upstream stable release
python3 scripts/install_core.py --version v1.19.0
```

The script fetches official builds from the upstream GitHub Releases, matches Linux/macOS/Windows amd64/arm64 platforms automatically and verifies the archive SHA256; when upstream provides no digest you must pass a `--sha256` you verified yourself. It uses only the Python standard library, does not clone or compile upstream source, and stores the core in the ignored `.runtime/core/` directory without executing it or overwriting an existing core. You can then start it with your own configuration:

```sh
.runtime/core/mihomo -d /path/to/core-data -f /path/to/config.yaml
```

Use `mihomo.exe` on Windows. The Nulas backend installs and starts a local core automatically by default; when `MIHOMO_CONTROLLER` is set it only connects to an existing service.

### Installing the background service (Linux user-level systemd)

Run as a regular user from the project root:

```sh
./scripts/build.sh
export PATH="$PWD/bin:$PATH"   # Use nulas directly in the current terminal
nulas install                  # Install the combined frontend/backend service without starting it or enabling start at login
nulas start                    # Start the backend and frontend
nulas status                   # Show the combined service status
```

Open [http://127.0.0.1:4669](http://127.0.0.1:4669). `nulas stop` / `nulas restart` stop or restart the frontend and backend together, and `nulas --help` lists the commands. You can add the absolute path of the project's `bin` directory to your shell configuration's PATH. The service depends on the current project path and build output, so rerun `./scripts/build.sh` and `nulas restart` after updating the code; after moving the directory or changing the Node path, rerun `nulas install` first.

The footer on every page links to this project's GitHub repository and shows “Built from [git hash]”. The revision is written from Git HEAD at frontend build time, shown as the first 7 characters with the full hash on hover, or “Unknown build revision” when no Git information is available. When building from a source archive, provide the matching commit hash (7–40 hexadecimal characters) through the `NULAS_BUILD_HASH` environment variable.

All three Nulas ports are configurable in the CLI: Web/API defaults to `4669`, SSR to `4668` and the development frontend to `4589`:

Use the Web/API port in production. `4668` is the internal SSR port Go uses to forward page requests; opening it directly shows pages but cannot call the API, and running only `pnpm start` does not start the Go backend. On success, `nulas start` / `nulas restart` display the browser entry point configured in the CLI and a note about the SSR port; when `service.env` overrides ports, trust the service startup log. If you see “Could not refresh status”, an API response containing HTML or an “internal SSR port” notice, query the entry port with `nulas config port` and start the frontend and backend with `nulas start`, `nulas run` or `scripts/start.sh`.

```sh
nulas config                   # Show all saved values; unsaved entries show their defaults
nulas config port 4769         # Web pages and API
nulas config ssr-port 4768     # Production SSR frontend
nulas config dev-port 4689     # Development frontend
nulas config ssr-port          # Query one entry (port / dev-port behave the same)
nulas restart                 # Restart the Linux service; restart foreground and development modes after stopping
```

Allowed values are `1–65535`, the port must be free, and Web/API and SSR must use different ports; unprivileged users usually cannot bind ports below `1024`. `scripts/start.sh`, `nulas run`, `pnpm start` and the development server read the same configuration, and the development `/api` proxy follows the Web/API port automatically. These settings do not change Mihomo's controller or proxy ports.

Ports are saved with atomic writes to `nulas/server.json` in the current user's configuration directory (Linux defaults to `~/.config/nulas/server.json` and honors `XDG_CONFIG_HOME`; macOS/Windows use the system user configuration directory), and commands run from any working directory use the same file. The CLI and the service should use the same user and configuration directory. The `port` and other fields saved in older files are preserved, and missing entries use the new defaults. `NULAS_ADDR` and `NULAS_SSR_URL` take precedence over the saved Web/API and SSR ports; if the service's `service.env` sets them, remove those overrides to use the saved values. The production launcher starts Node on the loopback address and port from `NULAS_SSR_URL`; the launcher sets `NITRO_HOST` / `NITRO_PORT` itself. `nulas config --json` lets scripts read saved values and defaults.

When you already have a core, write `MIHOMO_CONTROLLER`, `MIHOMO_SECRET` and related values into `~/.config/nulas/service.env` with mode `600` before starting the service; the installer never copies credentials from the terminal environment. The page switch controls start at login and systemd persists the registration; user services without linger start only after login, so run `loginctl enable-linger` when needed (this may require host administrator authorization and the application never escalates privileges for you), or run `python3 scripts/install_service.py` directly.

### Foreground and development startup

To run in the foreground only (Bash 4.3+):

```sh
./scripts/start.sh     # Production mode: start Go and Node SSR together
./scripts/dev.sh       # Development mode: install locked dependencies and start both together
```

You can also use two terminals:

```sh
cd backend && go run .
```

```sh
cd web && pnpm install --frozen-lockfile && pnpm dev
```

The development page is http://127.0.0.1:4589, and the development server forwards `/api` to `127.0.0.1:4669` (it reads the CLI port configuration automatically and also supports a `NULAS_ADDR` override). With an existing Mihomo service:

```sh
cd backend
MIHOMO_CONTROLLER=http://127.0.0.1:9090 MIHOMO_SECRET=your-secret go run .
```

Mihomo must have `external-controller` and the matching `secret` enabled.

### System-level deployment (optional)

`deploy/nulas.service` and `deploy/nulas-web.service` are Linux systemd templates for the backend and SSR services. You install them yourself and they never modify existing system services: build first, place the binary at `/opt/nulas/bin/nulas`, the whole `web/.output/` at `/opt/nulas/web/.output/` and the whole `scripts/` at `/opt/nulas/scripts/`, install Node.js 24+ and Python 3, create a dedicated `nulas` user and configure a permission-restricted `/etc/nulas.env`.

The backend template explicitly sets `NULAS_CORE_DIR=/var/lib/nulas/core`, `NULAS_CORE_INSTALLER=/opt/nulas/scripts/install_core.py` and `NULAS_TRAY_SCRIPT=/opt/nulas/scripts/tray.py`, avoiding development-relative paths. systemd's `StateDirectory=nulas` creates `/var/lib/nulas` owned by the service user; state, managed cores and version downloads all stay inside this writable directory while `ProtectSystem=strict` keeps the installation directory read-only. Without `MIHOMO_CONTROLLER`, the service uses a managed core and needs network access to download the official core on first startup. `/etc/nulas.env` can override the template's environment variables; an overridden core directory must be writable inside the service sandbox. System-level services have no user desktop session and cannot display the tray or control the GNOME system proxy; use a user-level service within a desktop session for these features.

The backend template uses `NoNewPrivileges=false` so managed Mihomo can acquire file capabilities granted with `setcap` when it starts; Nulas still runs as the unprivileged `nulas` user, without ambient capabilities granted to the backend. The SSR template keeps `NoNewPrivileges=true`. If TUN is not needed, a backend service drop-in can set `NoNewPrivileges=true` for additional hardening, but this blocks the file capability authorization flow above. For TUN, run `setcap` on the actual managed core path shown in the page, then restart `nulas.service` so the new core process acquires the capabilities; repeat authorization and restart after replacing or upgrading the core binary. For existing deployments, after updating the service template or drop-in, run `sudo systemctl daemon-reload` and `sudo systemctl restart nulas.service`, then check TUN readiness in the page.

In production Go is the single entry point: `/api/*` is handled by Go, and pages, client assets and framework requests are forwarded to the local Node SSR service without Nginx; when SSR is unavailable, pages return an explicit 502 while the API stays available. The Go task worker runs independently of Node, so stopping SSR does not cancel background tasks.

## Usage

### CLI commands

| Command | Description |
| --- | --- |
| `nulas run` | Quick install: run the frontend and backend in the foreground on any platform and schedule updates according to saved preferences |
| `nulas update` | Quick install: check for, build and install a new version; takes effect after a restart |
| `nulas update --check` | Discover updates only |
| `nulas update --auto on/off/status` | Enable, disable or show the automatic update preference |
| `nulas update --watch` | Standalone foreground automatic update scheduler |
| `nulas install` | Install / update the Linux user-level combined service without starting it or enabling start at login (requires Python 3) |
| `nulas uninstall` | Stop, disable and remove the Linux user-level service, keeping the software and data |
| `nulas remove` | Remove the quick-installed software and PATH configuration, keeping user data, cores and configuration |
| `nulas status` | Show service status and recent logs; returns a non-zero exit code when not running |
| `nulas start` / `stop` / `restart` | Start, stop or restart the frontend and backend |
| `nulas config [port\|ssr-port\|dev-port] [PORT]` | Show all ports, or query / save one entry; takes effect after a restart |
| `nulas` (no arguments) | Run only the backend in the foreground |
| `nulas --help` | Show usage |

Commands run from any directory, and service commands act on the current user's combined Nulas service, never on a system-level service with the same name or other Mihomo instances.

### Page features

- **Quick configuration**: Saves the five core parameters; **Apply to core** calls the official [`PATCH /configs`](https://wiki.metacubex.one/api/#configs) and changes runtime parameters only, without replacing nodes or rules. A snapshot is saved after a successful application and the managed core restores it on restart; external cores manage their own restart configuration.
- **Profiles**: Up to 100 entries with unique names, persisted atomically with service state. Import supports core parameter files and full YAML/JSON profiles containing nodes, proxy groups, rules, DNS, hosts, sniffer and providers; `go.yaml.in/yaml/v3` validates syntax and duplicate fields, and multiple documents, Base64 node subscriptions and encrypted formats are unsupported. Full documents and node credentials stay only in backend state and are never returned through list, create or task interfaces. Remote imports accept an optional name; the backend downloads directly (`User-Agent: clash.meta`, 15-second timeout, at most 5 redirects, no HTTPS downgrade) with no content size limit, and refresh URLs and tokens stay only in private backend state rather than being returned through profile or task interfaces.
- **Profile refresh**: Supports immediate refreshes and scheduled refreshes at 1–720 hour intervals (0 disables, and it is off by default); a single background worker runs the schedule, so refreshes continue after the browser closes while the backend keeps running. The job and URL snapshot are persisted before each refresh; a failed download or validation keeps the original profile and shows the failure. A successful refresh only changes the profile library and does not modify the quick configuration draft, submitted jobs or applied snapshots, and it never applies to Mihomo automatically. After a restart one due schedule runs rather than every missed cycle; interrupted running jobs are marked failed and wait for the next cycle or a manual retry. The library allows changing the refresh URL and interval; editing and deleting contents is not supported yet. Older remote profiles have no saved URL and need one added under “Refresh settings”. Job history is trimmed automatically across all actions, so completed records cannot block refreshes.
- **Generate / apply a full profile directly**: Pending jobs keep independent full-document snapshots; completed jobs discard their documents and refresh URLs. Generation to `.data/<job-id>.yaml` preserves the original text; only the latest 10 successful generated files are retained (copy files to another name or directory for long-term storage). Apply jobs no longer create extra output files; applying directly uses the `payload` of `PUT /configs?force=true` to reload nodes, proxy groups, rules, DNS and providers without overwriting the user's original configuration file. The applied copy listens for proxies according to the `allow-lan` setting: when it is disabled only the local machine is served, and when it is enabled the LAN can connect through the mixed proxy port (7890 by default); the Web/API and controller interfaces stay local, TUN, iptables, transparent proxying, custom inbound listeners and NTP writes are disabled, DNS service listeners and imported controller fields are removed, and credentials are unchanged; HTTP providers use a per-job `.nulas/` relative cache path. Applying disables the core's existing TUN and transparent proxies without changing system proxy settings. After a successful application the snapshot is saved atomically and the managed core loads it directly on its next start; loading a template or generating a file does not replace the applied snapshot.
- **Nodes (`/nodes`)**: Selects members of manual proxy groups (Selector) and switches modes through `GET /proxies`, `PUT /proxies/{name}` and `PATCH /configs`; global mode shows only GLOBAL, rule mode hides GLOBAL and shows the MATCH fallback outbound through `GET /rules` (the core default DIRECT when no such rule exists), and direct mode offers no node selection. Changing the MATCH target itself requires editing the full profile and applying it explicitly. Latency tests cover a single node and the current group in bulk (matching members after a search) through `POST /api/nodes/delay`, which calls the core's `GET /proxies/{name}/delay` with a fixed target of `https://www.gstatic.com/generate_204`, an expected HTTP 204 and a 5-second timeout, running at most 4 tests at once. It shows millisecond latency and a per-node failure reason; this is connection latency rather than download bandwidth, and a group member that is itself a policy group is tested according to its current policy. Latency tests never switch nodes automatically and are not written to configuration or background tasks; leaving the page stops new test submissions, while tests already sent to the core end on the core's timeout. Results live only in the current page and clear when the list is refreshed. REJECT / REJECT-DROP / PASS offer no latency test. Responses are limited to 2 MB and secrets stay on the server.
- **Background tasks (`/tasks`)**: State is persisted as `queued → running → succeeded / failed` and the page refreshes every two seconds; failed tasks can be resubmitted. Failed operations are not retried automatically.
- **System settings**: TUN, system proxy, start at login and tray switches. Switch state is read back from the backend, unmet runtime requirements show a specific reason, and settings apply to the host running the backend.

### Configuration variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `NULAS_INSTALL_HOME` | Installation directory set by the quick installer | CLI update management; usually no manual configuration needed |
| `NULAS_ADDR` | `127.0.0.1:4669` (or the port saved in the CLI) | Web/API listen address; an explicit value overrides the CLI port |
| `NULAS_SSR_URL` | `http://127.0.0.1:4668` (or the SSR port saved in the CLI) | Local SSR address Go forwards page requests to (HTTP loopback IP only) |
| `NULAS_WEB_DIR` | empty | Explicitly enables legacy static web hosting; cannot be used with SSR builds |
| `NULAS_DATA_DIR` | `.data` (relative to the working directory) | Configuration, tasks and generated files |
| `MIHOMO_CONTROLLER` | empty | Core controller API, for example `http://127.0.0.1:9090` |
| `MIHOMO_SECRET` | empty | Core API secret |
| `NULAS_CORE_DIR` / `NULAS_CORE_INSTALLER` / `NULAS_PYTHON` | `../.runtime/core` / `../scripts/install_core.py` / `python3` | Managed core paths and interpreter overrides |
| `NULAS_TRAY_SCRIPT` | `../scripts/tray.py` | Tray helper script path |

### Desktop tray (optional)

The tray is off by default and provides entries for the panel, node management and background tasks, using the default browser. Enabling the tray on Linux checks dependencies and installs GTK3, PyGObject and AyatanaAppIndicator3 through apt-get, dnf or pacman when they are missing (requesting administrator authorization through pkexec, or using authorized sudo). Python tray packages are installed into a separate environment under the user cache directory `~/.cache/nulas/tray-pythonX.Y` without changing the system Python, and suitable existing dependencies are reused. Installation continues in the background while the interface shows progress or the failure reason, and you can disable the tray or retry. Dependencies are not installed without a desktop session; unsupported distributions require manual installation. `NULAS_PYTHON` should point to a Python that can load the distribution's GI bindings.

On macOS/Windows, install the optional dependencies manually into the Python environment the backend uses:

```sh
python3 -m pip install -r scripts/requirements-tray.txt
# Windows
python -m pip install -r scripts/requirements-tray.txt
```

The automatic installation logic is verified with mocked package managers, covering dependency reuse, isolated environments, rejection without a desktop session and cancelled installs; real system package installation and desktop authorization/tray display have not been verified on real hardware.

macOS/Windows use the native pystray backend; Linux needs a PyGObject runtime for GTK/AppIndicator plus a desktop tray area, and GNOME usually also needs the AppIndicator extension. Insufficient installation or desktop support shows a failure with a retry entry point. Turning the tray off interrupts an unfinished unprivileged installation; a system package manager that already received administrator authorization may still finish the current transaction.

## Notes

- **Local use only**: The API and SSR bind to the loopback address by default, allow request Host values only for `127.0.0.1`, `localhost` or `[::1]` with the actual API listening port or configured development port (fixed at startup), rejecting external domains used for DNS rebinding. Writes also validate same-origin Origin values, and client forwarding headers are not trusted. The API has no authentication; local processes can still call it directly. Remote deployment requires implementing authentication and TLS yourself; never put controller credentials in the frontend or commit them to the repository.
- **Do not escalate privileges**: Grant only the capabilities the Mihomo binary needs (for example `sudo setcap cap_net_admin,cap_net_raw+ep <mihomo>`), never run Nulas or Node as root, and do not use `sudo nulas`.
- **Platform limitations**: The system proxy supports only unprivileged Linux GNOME environments with a desktop session and `gsettings`; TUN management supports only Linux managed cores started by Nulas (external controllers and other systems are not supported); start at login is Linux user-level systemd. The cross-platform tray does not change these boundaries.
- **Preferences and observed state are separate**: Switch preferences are stored under `preferences` in `NULAS_DATA_DIR/state.json`. After startup jobs finish, saved enabled system proxy / TUN preferences are restored through newly created and durably persisted background jobs; TUN stays disabled until its restoration check passes. A previously failed or interrupted network operation requires a manual retry, queued operations keep running without duplicate restoration, and the observed state shown in the page can differ from saved preferences with failures labeled explicitly.
- **Tasks and persistence**: One process and one worker; you can submit only one pending task at a time and startup recovery tasks run in order. Do not start multiple processes sharing one data directory. Normally at most 1000 tasks are retained by automatically trimming the oldest completed records, while keeping active tasks and the latest system proxy / TUN outcomes to preserve manual retry rules. Legacy states with more than 1000 active tasks retain them all until completion. Completed tasks retain metadata only; the successfully applied full snapshot is stored independently. Startup automatically compacts old state and job YAML files, removing files only after a successful atomic state write; deletion failures are logged and retried on later writes. State is still rewritten atomically in full, and its size also depends on the profile library, applied document and active task contents. Interrupted running jobs are marked failed after a restart and never replay external side effects, while queued jobs can resume.
- **Data directories**: `.data/`, `.runtime/` and `bin/` are ignored; do not commit them. Keep the state files before an upgrade or migration so applied profiles and proxy snapshots can be restored.
- **Not implemented yet**: Node creation / editing / deletion, rule editing, profile editing and deletion, privileged (non-GNOME) system proxy control, non-Linux TUN management, external controller TUN management, and online full-profile editing. Remote profiles support immediate and scheduled refreshes and accept only Mihomo YAML/JSON documents, not Base64 node subscriptions. Provider URLs in a full profile are stored on the server and Mihomo handles later refreshes.
- **Effects of applying a profile**: Applying a full profile disables the core's existing TUN and transparent proxies; the `Nulas` network interface and automatic routes created by Mihomo affect local traffic, so make sure you understand this before enabling TUN.
- **Profile refresh verification**: Frontend type checking and the production build, Go vet and the backend build pass; the profile refresh API, background scheduling, restart recovery, URL snapshot isolation, keeping the original profile on failure, rollback on persistence failure, credential hiding and the task history limit have been verified with race tests. Except for two existing managed core tests that bind port 9090 and cannot pass because the port is in use, the remaining backend race tests pass; the page interactions for “immediate refresh” and saving scheduled refresh settings were verified in a temporary data environment.
- **Verification status**: The 17 Python tests for these installation / update changes, the CLI race tests, frontend type checking and the production build, Go vet, the Linux backend build, and Windows amd64 / macOS arm64 backend cross-compilation pass. Four managed core / TUN cases in the full Go race suite fail because port 9090 is in use on this machine and because of TUN state dependencies; host networking was not changed to work around them. Core interfaces are verified with a mocked controller; the Windows / macOS installers, real package manager installations, real automatic upgrades, tray display on all three platforms and real TUN traffic have not been verified on real hardware.

## Layout

- `AGENTS.md`: project requirements, common commands, protected boundaries and commit conventions (`CLAUDE.md` is a symlink to it).
- `web/`: SolidStart pages, styles and frontend tests.
- `backend/`: Go API, background worker, persistence and tests.
- `scripts/`: build, start, core download, service installation and tray helper scripts.
- `deploy/`: optional systemd service templates (not installed automatically).

The project keeps the name Nulas, following the upstream README requirement for naming unofficial downstream projects. If upstream source is ever introduced, keep the upstream license and copyright notices.

### Core management

Open **Core management** (`/core`) in the sidebar to browse [official Mihomo stable releases](https://github.com/MetaCubeX/mihomo/releases) page by page, read release notes, switch to an older version, or update to the latest stable release in one click. GitHub network access is required; rate limits, unsupported platforms, missing SHA256 digests and failed download verification show a failure result. Only managed cores started by the Nulas production entry point are supported; external controllers must be managed separately.

An operation first saves a background task for a specific version, and then a single background worker downloads, verifies and restarts, so closing the page does not affect execution. Versions live in `.runtime/core/versions/<tag>/` and the atomically written `active-version` selects one; the older `.runtime/core/mihomo` and user configuration are kept, and downloaded versions can be reused. When the startup or controller interface check fails it tries to restore the previous version, and a failed recovery shows the actual error. The restart briefly interrupts connections, loads the last successfully applied profile, keeps TUN disabled until you re-enable it manually, and does not modify system proxy settings. After Nulas restarts, interrupted running jobs are marked failed and never replayed automatically, while waiting jobs can continue.

### Interface language

The sidebar language selector supports Simplified Chinese and English. On first use it follows supported browser language preferences, falling back to Chinese. Manual selection is saved in a browser cookie for one year. SSR and initial hydration use Chinese; the selected language is applied after mounting. Page titles, accessible labels and date formatting follow the selected language. Backend task logs, runtime diagnostics and third-party errors remain in their original language, and operation notices already shown keep the language used when they were triggered. See the project skill [nulas-i18n](.agents/skills/nulas-i18n/SKILL.md) for localization maintenance rules, and run `cd web && pnpm test:i18n` to check translation coverage and placeholders.
