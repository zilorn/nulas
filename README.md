# Nulas

基于 [MetaCubeX/mihomo · Meta](https://github.com/MetaCubeX/mihomo/tree/Meta) 的网页快速配置面板。前端使用 **Vite + SolidStart 2（SSR）+ SolidJS + TypeScript**，后端使用 **Go** 标准库，通过 Mihomo REST API 读取并修改核心运行参数。Nulas 是非官方下游项目，不复制上游源码，不捆绑内核二进制。

## 介绍

- **快速配置**：编辑 mixed-port、mode、allow-lan、ipv6、log-level，生成配置并应用到内核。
- **配置库**：导入本地文件、粘贴 JSON / 顶层标量 YAML，或从 HTTP/HTTPS 地址下载完整 Mihomo 配置，支持立即更新与定时更新，按名称搜索、载入、生成或直接应用。
- **节点管理**：读取已连接内核，选择手动代理组（Selector）成员，切换规则 / 全局 / 直连模式并显示 MATCH 兜底出口，支持单节点和当前组批量延迟测速。
- **后台任务**：安装、生成、应用、TUN、系统代理等操作进入持久化单 worker 队列，关闭网页不中断。
- **系统设置**：虚拟网卡（TUN）、系统代理、开机自启与桌面托盘开关，偏好写入后端原子状态文件。
- **托管内核**：默认按需下载官方内核并以其基础直连配置启动；设置 `MIHOMO_CONTROLLER` 时改为连接已有内核。

核心凭据只保存在服务端，API 默认仅监听回环地址。当前版本面向本机使用，远程部署所需的认证与 TLS 尚未实现。

## 安装

### 快速安装（Windows / macOS / Linux）

已有依赖且版本符合要求时直接复用，不重复安装。安装器克隆本仓库、安装锁定的前端依赖、检查类型并编译前后端，最后将 `nulas` 加入用户 PATH。不启动服务、不启用托盘、不修改主机网络。

Linux / macOS（bash、zsh、fish 均可执行）：

```sh
curl -fsSL https://raw.githubusercontent.com/zilorn/nulas/main/scripts/install.sh | bash
```

Windows PowerShell：

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/zilorn/nulas/main/scripts/install.ps1 -OutFile "$env:TEMP\nulas-install.ps1"
& "$env:TEMP\nulas-install.ps1"
```

也可先下载检查，再执行：

```sh
curl -fsSL https://raw.githubusercontent.com/zilorn/nulas/main/scripts/install.sh -o /tmp/nulas-install.sh
sh /tmp/nulas-install.sh
```

安装入口是 Shell 脚本；它按需准备 Python，再由 Python 标准库处理跨平台下载、构建与更新。管道方式支持交互式安装依赖，非交互环境缺少管理员权限时会明确失败。

也可以先克隆仓库并检查脚本，再执行 `sh scripts/install.sh` 或 `./scripts/install.ps1`。这些地址需要对应代码已发布到 GitHub 的 `main` 分支。PowerShell 执行策略若禁止脚本，请按本机策略允许已检查的安装脚本运行。

- **系统检测**：支持 Linux / macOS / Windows 的 x64、arm64。Linux 支持 apt（Ubuntu / Debian）、pacman（Arch）、dnf（Fedora）、zypper（openSUSE）、apk（Alpine）的缺失 Git / Python 安装；包管理操作可能要求 sudo。Alpine 等 musl 环境请预先安装满足版本的 Node.js、Go；自动下载的 Linux Node.js 官方包要求 glibc。
- **依赖复用**：使用现有 Git、Python 3.10+、Node.js 24+、Go 1.23+；pnpm 仅在版本与当前 `packageManager` 完全一致时复用，否则安装到该版本的独立构建目录。缺少合格 Node / Go 时下载官方包并校验 SHA256，安装在 Nulas 目录，不覆盖系统版本。
- **平台包管理器**：macOS 缺少 Git / Python 时使用 Homebrew，缺少 Homebrew 时调用其[官方安装器](https://docs.brew.sh/Installation)（可能请求管理员授权与安装 Xcode 命令行工具）；Windows 缺少 Git / Python 时使用 winget（需系统已有 App Installer，安装器可能触发系统权限提示）。不自动安装桌面托盘依赖。
- **安装目录**：Linux / macOS 默认 `~/.local/share/nulas`，Windows 默认 `%LOCALAPPDATA%\Nulas`。Unix 可使用 `sh scripts/install.sh --home /absolute/path`；PowerShell 使用 `./scripts/install.ps1 -HomeDir 'D:\Apps\Nulas'`。可用 `--repository` / `--branch`（PowerShell 为 `-Repository` / `-Branch`）指定来源；更新始终跟踪安装时指定的分支。
- **PATH**：Unix 保留现有配置，向 bash 的 `.bashrc`、`.profile` / 已有登录配置、zsh 的 `${ZDOTDIR:-$HOME}/.zshrc` 追加路径，同时配置 fish 的 `conf.d/nulas.fish`；Windows 写入当前用户 PATH 并广播环境变化。请重新打开终端；已经运行的父终端可能需重新启动。无需 Bash 4.3 即可运行安装器与 `nulas run`。

安装完成后：

```sh
nulas run                      # 跨平台前台启动前后端，Ctrl+C 停止
# Linux 用户级 systemd，可选：
nulas install
nulas start
```

打开 [http://127.0.0.1:4669](http://127.0.0.1:4669)。快速安装的 Linux 服务使用稳定启动器，重启时读取最新已构建版本。Windows / macOS 暂不提供后台服务安装，使用 `nulas run`。

### 卸载

```sh
nulas uninstall                # Linux：停止服务、关闭自启并移除用户级服务
nulas remove                   # 卸载快速安装的软件；Linux 同时卸载对应服务
```

`uninstall` 保留软件和所有配置，之后可用 `nulas install` 重新安装服务。`remove` 支持 Linux / macOS / Windows，仅删除快速安装目录中的 `source/`、`releases/`、`tools/`、`bin/` 和 `installation.json`，清理安装器添加的当前用户 PATH 配置；保留 `data/`、`runtime/`、用户端口配置、`service.env` 及其他文件，不卸载共享系统依赖。重新打开终端使 PATH 更新生效。Windows 请使用安装器生成的 `nulas.cmd`；旧启动器提示需要刷新时执行一次 `nulas update` 后重试。

卸载前先用 Ctrl+C 停止前台的 `nulas run` 和 `nulas update --watch`，并在页面关闭已启用的系统代理 / TUN。卸载不会修改系统网络设置。服务停用失败或软件文件删除失败时返回非零退出码，不会报告卸载成功。拒绝删除开发仓库、其他安装的服务、符号链接软件目录，以及正在安装 / 更新的目录；手工构建的开发仓库只支持 `uninstall`，软件文件由用户自行管理。保留的数据可供重新安装使用，彻底清理数据需另行备份并手动删除。

### 更新发现与自动更新

```sh
nulas update --check            # 获取远端分支，显示当前 / 最新提交，不编译、不切换
nulas update                    # 下载并编译新版本，成功后原子切换
nulas update --auto on          # 持久保存开启自动更新
nulas update --auto off         # 关闭自动更新（默认关闭）
nulas update --auto status      # 查看自动更新偏好、已安装提交与最近失败
nulas update --watch            # 独立前台更新调度器，适合只启动后端的情况
```

快速安装提供上述命令；手工构建的开发仓库继续自行更新、构建，也可用快速安装建立独立受管理安装。

`nulas run`（包括快速安装生成的 Linux 服务）自带更新调度器：启动时检查已保存的偏好，开启后立即检查，此后每 6 小时检查并安装新提交。服务或调度器停止后不会继续定时更新；运行期间更改偏好在下次检查时生效。自动更新的失败写入安装元数据，并在日志中显示。

更新在 `releases/` 的独立检出目录编译；类型检查或构建失败保留当前版本。拒绝远端分支历史重写，使用目录锁避免并发安装。成功后保留旧版本与失败的构建目录，运行中的服务继续使用原版本，执行 `nulas restart`（Linux 服务）或停止并重新运行 `nulas run` 后生效。不会自动重启服务或重放代理 / TUN 操作。

配置和任务保存在安装目录的 `data/`，内核保存在 `runtime/core/`，与版本目录分离；显式设置的 `NULAS_DATA_DIR` / `NULAS_CORE_DIR` 仍优先。安装元数据与自动更新偏好保存在 `installation.json`。不要手动删除当前版本目录；旧版本不会自动清理，以便恢复。安装 / 更新被强制中断后，先确认没有安装进程在运行，再检查并清理 `.update-lock`；不自动重放中断更新。首次安装失败保留现场，修复依赖后应检查并移走未完成的安装目录再重试，不覆盖已有安装。

自动更新会编译并使用所选仓库分支的新代码，请仅对可信来源开启；它更新的是 Nulas，Mihomo 内核仍按原有机制按需安装。

### 环境要求

| 依赖 | 版本 / 说明 |
| --- | --- |
| Node.js | 24+（开发、构建与 SSR 运行） |
| pnpm | 与 `web/package.json` 的 `packageManager` 一致 |
| Go | 1.23+ |
| Python 3 | 按需下载内核、安装用户级服务与运行托盘辅助脚本 |
| Bash | 4.3+（启动脚本；macOS 自带 Bash 较旧时请使用新版） |
| systemd | 可选，仅 Linux 用户级后台服务与开机自启需要 |

安装与构建过程不会启动服务、不会下载或执行内核。

### 构建

```sh
./scripts/build.sh
```

脚本安装锁定依赖、运行前端类型检查，构建客户端资源、SSR 服务与 Go 后端，产物为 `web/.output/` 与 `bin/nulas`。

### 按需下载内核

已有内核时可直接连接，无需下载。需要内核时执行：

```sh
python3 scripts/install_core.py
# 或指定上游稳定 Release
python3 scripts/install_core.py --version v1.19.0
```

脚本从上游 GitHub Releases 获取官方编译文件，自动匹配 Linux/macOS/Windows 的 amd64/arm64 平台并校验压缩包 SHA256；上游未提供 digest 时需传入自行核实的 `--sha256`。仅使用 Python 标准库，不克隆或编译上游源码，内核存放在忽略目录 `.runtime/core/`，不自动执行或覆盖已有内核。随后可用自己的配置启动：

```sh
.runtime/core/mihomo -d /path/to/core-data -f /path/to/config.yaml
```

Windows 使用 `mihomo.exe`。Nulas 后端默认自动安装并启动本地内核；指定 `MIHOMO_CONTROLLER` 时只连接已有服务。

### 安装后台服务（Linux 用户级 systemd）

以普通用户在项目根目录执行：

```sh
./scripts/build.sh
export PATH="$PWD/bin:$PATH"   # 当前终端直接使用 nulas
nulas install                  # 安装前后端组合服务，不启动、不启用自启
nulas start                    # 启动后端与前端
nulas status                   # 查看组合服务状态
```

打开 [http://127.0.0.1:4669](http://127.0.0.1:4669)。`nulas stop` / `nulas restart` 同时停止或重启前后端，`nulas --help` 查看命令。可将项目 `bin` 的绝对路径加入 shell 配置的 PATH。服务依赖当前项目路径与构建产物，更新代码后重新执行 `./scripts/build.sh` 与 `nulas restart`；迁移目录或更换 Node 路径后先重新执行 `nulas install`。

所有页面的页脚提供本项目 GitHub 仓库入口，并显示「由 [git hash] 构建」。版本在前端构建时从 Git HEAD 写入，显示前 7 位，悬停可查看完整 hash；无 Git 信息时显示「构建版本未知」。从源码归档构建时，可通过 `NULAS_BUILD_HASH` 环境变量提供对应提交的 hash（7–40 位十六进制字符）。

三个 Nulas 端口均可在 CLI 中自定义，默认 Web/API 为 `4669`、SSR 为 `4668`、开发前端为 `4589`：

生产环境请访问 Web/API 端口。`4668` 是 Go 转发页面请求所用的内部 SSR 端口，直接打开它虽然能看到页面，但无法调用 API；仅运行 `pnpm start` 也不会启动 Go 后端。`nulas start` / `nulas restart` 成功后会显示当前 CLI 配置的浏览器入口和 SSR 端口说明；若 `service.env` 覆盖了端口，以服务启动日志为准。若出现“状态更新失败”、API 返回 HTML 或“SSR 内部端口”提示，请通过 `nulas config port` 查询入口端口，并用 `nulas start`、`nulas run` 或 `scripts/start.sh` 启动前后端。

```sh
nulas config                   # 显示全部保存值；未保存的项目显示默认值
nulas config port 4769         # Web 页面与 API
nulas config ssr-port 4768     # 生产 SSR 前端
nulas config dev-port 4689     # 开发前端
nulas config ssr-port          # 查询单项（port / dev-port 同理）
nulas restart                 # Linux 服务重启；前台与开发模式停止后重新启动
```

允许范围为 `1–65535`，端口必须可用，Web/API 与 SSR 必须使用不同端口；普通用户通常不能绑定低于 `1024` 的端口。`scripts/start.sh`、`nulas run`、`pnpm start` 与开发服务器会读取同一配置，开发 `/api` 代理自动跟随 Web/API 端口。这些设置不改变 Mihomo 的控制端口或代理端口。

端口通过原子写入保存至当前用户配置目录的 `nulas/server.json`（Linux 默认 `~/.config/nulas/server.json`，遵循 `XDG_CONFIG_HOME`；macOS/Windows 使用系统用户配置目录），从任意工作目录执行命令均使用同一文件。CLI 与服务应使用同一用户和配置目录。旧文件中保存的 `port` 和其他字段会保留，缺少的项目使用新默认值。`NULAS_ADDR` 和 `NULAS_SSR_URL` 分别优先于保存的 Web/API 和 SSR 端口；若服务的 `service.env` 中设置了它们，请移除覆盖以使用保存值。生产启动器按 `NULAS_SSR_URL` 的回环地址和端口启动 Node；`NITRO_HOST` / `NITRO_PORT` 由启动器统一设置。`nulas config --json` 可供脚本读取保存值和默认值。

已有内核时，在启动服务前把 `MIHOMO_CONTROLLER`、`MIHOMO_SECRET` 等写入 `~/.config/nulas/service.env` 并设为权限 `600`；安装器不会复制终端环境中的凭据。开机自启由网页开关控制、注册状态由 systemd 持久保存；没有 linger 的用户服务只在登录后启动，必要时执行 `loginctl enable-linger`（可能需要主机管理员授权，应用不会代为提权），也可直接运行 `python3 scripts/install_service.py`。

### 前台与开发启动

只需前台运行时（Bash 4.3+）：

```sh
./scripts/start.sh     # 生产模式，同时启动 Go 与 Node SSR
./scripts/dev.sh       # 开发模式，安装锁定依赖并同时启动前后端
```

也可使用两个终端：

```sh
cd backend && go run .
```

```sh
cd web && pnpm install --frozen-lockfile && pnpm dev
```

开发页面为 http://127.0.0.1:4589，开发服务器将 `/api` 转发至 `127.0.0.1:4669`（自动读取 CLI 端口配置，也支持 `NULAS_ADDR` 覆盖）。已有 Mihomo 服务时：

```sh
cd backend
MIHOMO_CONTROLLER=http://127.0.0.1:9090 MIHOMO_SECRET=your-secret go run .
```

Mihomo 需要启用 `external-controller` 和对应的 `secret`。

### 系统级部署（可选）

`deploy/nulas.service` 与 `deploy/nulas-web.service` 提供 Linux systemd 后端与 SSR 服务模板，需自行安装、不会修改现有系统服务：先构建，把二进制放到 `/opt/nulas/bin/nulas`、整个 `web/.output/` 放到 `/opt/nulas/web/.output/`，安装 Node.js 24+，创建专用 `nulas` 用户并配置权限受限的 `/etc/nulas.env`。

生产环境以 Go 作为统一入口：`/api/*` 由 Go 处理，页面、客户端资源与框架请求转发到本机 Node SSR 服务，无需 Nginx；SSR 不可用时页面返回明确的 502，API 仍然可用。Go 任务 worker 与 Node 独立运行，SSR 停止不会取消后台任务。

## 使用

### CLI 命令

| 命令 | 说明 |
| --- | --- |
| `nulas run` | 快速安装：跨平台前台运行前后端，并按已保存偏好调度更新 |
| `nulas update` | 快速安装：检查并编译安装新版本，重启后生效 |
| `nulas update --check` | 仅发现更新 |
| `nulas update --auto on/off/status` | 开启、关闭或查看自动更新偏好 |
| `nulas update --watch` | 独立前台自动更新调度器 |
| `nulas install` | 安装 / 更新 Linux 用户级组合服务，不启动、不开启自启（需要 Python 3） |
| `nulas uninstall` | 停止、禁用并移除 Linux 用户级服务，保留软件与数据 |
| `nulas remove` | 卸载快速安装的软件及 PATH 配置，保留用户数据、内核和配置 |
| `nulas status` | 查看服务状态与近期日志；未运行时返回非零退出码 |
| `nulas start` / `stop` / `restart` | 启动、停止、重启前后端 |
| `nulas config [port\|ssr-port\|dev-port] [PORT]` | 显示全部端口，或查询 / 保存单项，重启后生效 |
| `nulas`（无参数） | 前台只运行后端 |
| `nulas --help` | 显示用法 |

命令可从任意目录运行，服务命令操作对象是当前用户的 Nulas 组合服务，不会操作系统级同名服务或其他 Mihomo 实例。

### 页面功能

- **快速配置**：保存五项核心参数；`应用到内核` 调用官方 [`PATCH /configs`](https://wiki.metacubex.one/api/#configs)，只改运行参数、不替换节点与规则，成功应用后保存快照，托管内核重启会恢复；外部内核的重启配置由其自身管理。
- **配置库**：最多 100 份、名称不可重复，随服务状态原子持久化。导入同时支持核心参数文件与包含节点、代理组、规则、DNS、hosts、sniffer、providers 的完整 YAML/JSON；使用 `go.yaml.in/yaml/v3` 校验语法与重复字段，不支持多文档、Base64 节点订阅或加密格式，完整文档与节点凭据仅保存在后端状态，不会通过列表、创建响应或任务接口返回。网络导入可选填写名称，后端直接下载（`User-Agent: clash.meta`、15 秒超时、最多 5 次重定向、禁止 HTTPS 降级），不设内容大小上限，更新地址与令牌仅保存在后端私有状态中，不通过配置或任务接口返回。
- **配置更新**：支持立即更新与按 1–720 小时间隔定时更新（0 为关闭，默认关闭）；计划由单一后台 worker 执行，关闭浏览器不影响更新，后端须保持运行。更新前持久保存任务及地址快照，下载或校验失败保留原配置并显示失败；成功更新仅修改配置库，不修改快速配置草稿、已提交任务或已应用快照，不自动应用到 Mihomo。重启后补执行一次到期计划，不补跑所有错过的周期；中断的运行任务标记失败，等待下一周期或手动重试。配置库可修改更新地址及间隔，尚不支持内容编辑与删除。旧网络配置未保存地址，需在“更新设置”中补充。后台最多保存 1000 条任务，达到上限时新的更新会淘汰最早已完成的配置更新记录，保留其他操作记录；若仍无空间则显示失败。
- **生成 / 直接应用完整配置**：任务保存独立的完整文档快照，生成到 `.data/<job-id>.yaml` 时保留原文；直接应用调用 `PUT /configs?force=true` 的 `payload`，重载节点、代理组、规则、DNS 与 providers，不覆盖用户原始配置文件。应用副本按 `allow-lan` 设置代理监听：关闭时仅本机，开启时允许局域网通过混合代理端口连接（默认 7890）；Web/API 与控制接口仍仅本机，禁用 TUN、iptables、透明代理、自定义入站监听与 NTP 写入，移除 DNS 服务监听和导入的控制接口字段，凭据保持不变；HTTP providers 使用每次任务独立的 `.nulas/` 相对缓存路径。应用会关闭内核现有的 TUN 与透明代理，但不会修改系统代理设置。应用成功后快照会原子保存，托管内核下次启动直接加载该快照；载入模板或生成文件不会替换已应用快照。
- **节点管理（`/nodes`）**：通过 `GET /proxies`、`PUT /proxies/{name}` 与 `PATCH /configs` 选择手动代理组（Selector）成员并切换模式；全局模式只显示 GLOBAL，规则模式隐藏 GLOBAL 并通过 `GET /rules` 显示 MATCH 兜底出口（无此类规则时显示内核默认 DIRECT），直连模式不提供节点选择。改变 MATCH 目标本身需要修改完整配置后显式应用。支持单节点测速及当前组（搜索后为匹配成员）批量测速，通过 `POST /api/nodes/delay` 调用内核 `GET /proxies/{name}/delay`，目标固定为 `https://www.gstatic.com/generate_204`、期望 HTTP 204、超时 5 秒，同时最多 4 个测试。显示毫秒延迟与逐节点失败原因；这是连接延迟而非下载带宽，组成员若为策略组则按其当前策略测试。测速不自动切换节点，不写入配置或后台任务；页面离开后停止提交新测试，已到达内核的测试由内核超时结束。结果仅在当前页面保留，刷新列表后清空。REJECT / REJECT-DROP / PASS 不提供测速。响应最多 2 MB，密钥留在服务端。
- **后台任务（`/tasks`）**：状态按 `queued → running → succeeded / failed` 持久保存，网页每两秒更新；失败可重新提交。操作失败不会自动重试。
- **系统设置**：虚拟网卡（TUN）、系统代理、开机自启与托盘开关。开关状态来自后端回读，未满足运行条件时显示具体原因；设置作用于后端所在主机。

### 配置变量

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `NULAS_INSTALL_HOME` | 快速安装器设置的安装目录 | CLI 更新管理；通常无需手工设置 |
| `NULAS_ADDR` | `127.0.0.1:4669`（或 CLI 保存的端口） | Web/API 监听地址，显式设置时覆盖 CLI 端口 |
| `NULAS_SSR_URL` | `http://127.0.0.1:4668`（或 CLI 保存的 SSR 端口） | Go 转发页面请求的本机 SSR 地址（仅支持 HTTP 回环 IP） |
| `NULAS_WEB_DIR` | 空 | 显式启用旧版静态网页托管；不能用于 SSR 构建 |
| `NULAS_DATA_DIR` | `.data`（相对工作目录） | 配置、任务和生成文件 |
| `MIHOMO_CONTROLLER` | 空 | 内核控制 API，例如 `http://127.0.0.1:9090` |
| `MIHOMO_SECRET` | 空 | 内核 API 密钥 |
| `NULAS_CORE_DIR` / `NULAS_CORE_INSTALLER` / `NULAS_PYTHON` | `../.runtime/core` / `../scripts/install_core.py` / `python3` | 托管内核路径与解释器覆盖 |
| `NULAS_TRAY_SCRIPT` | `../scripts/tray.py` | 托盘辅助脚本路径 |

### 桌面托盘（可选）

托盘默认关闭，提供打开面板、节点管理与后台任务入口，使用默认浏览器。Linux 开启托盘时会检查依赖，缺失时自动通过 apt-get、dnf 或 pacman 安装 GTK3、PyGObject 与 AyatanaAppIndicator3（通过 pkexec 请求管理员授权，或使用已授权的 sudo）。Python 托盘包安装在用户缓存目录 `~/.cache/nulas/tray-pythonX.Y` 的独立环境中，不改动系统 Python；已有可用依赖时直接复用。安装在后台继续，界面显示进度或失败原因，可关闭托盘或重试。无桌面会话时不会安装依赖；不支持的发行版需手动安装。`NULAS_PYTHON` 应指向能加载发行版 GI 绑定的 Python。

macOS/Windows 仍需在后端使用的 Python 环境手动安装可选依赖：

```sh
python3 -m pip install -r scripts/requirements-tray.txt
# Windows
python -m pip install -r scripts/requirements-tray.txt
```

自动安装逻辑通过模拟包管理器验证，包含依赖复用、隔离环境、无桌面拒绝与取消安装；真实系统包安装和桌面授权/托盘显示尚未实机验证。

macOS/Windows 使用 pystray 原生后端；Linux 需要 GTK/AppIndicator 的 PyGObject 运行环境与桌面托盘区域，GNOME 通常还需 AppIndicator 扩展。安装或桌面支持不足时会显示失败并提供重试入口。关闭托盘会中断尚未完成的普通用户安装；已获管理员授权的系统包管理器可能继续完成当前事务。

## 注意事项

- **仅限本机**：API 与 SSR 默认绑定回环地址，同源写入校验、拒绝外站请求，不信任客户端转发头。远程部署必须自行实现认证与 TLS；不要把控制接口凭据放进前端或提交到仓库。
- **不要提权**：只给 Mihomo 二进制授予必要能力（如 `sudo setcap cap_net_admin,cap_net_raw+ep <mihomo>`），不要以 root 运行 Nulas 或 Node，也不要使用 `sudo nulas`。
- **平台限制**：系统代理仅支持具有桌面会话与 `gsettings` 的 Linux GNOME 普通用户环境；TUN 管理仅支持 Nulas 启动的 Linux 托管内核（外部控制器与其他系统不提供）；开机自启为 Linux 用户级 systemd。跨平台托盘不会改变这些边界。
- **偏好与实测分离**：开关偏好保存在 `NULAS_DATA_DIR/state.json` 的 `preferences`，重启不重放系统代理 / TUN 操作，TUN 始终默认关闭；页面显示的实测状态与保存偏好可能不同，失败会显式标注。
- **任务与持久化**：单进程单 worker，每次仅一个待处理任务，不要启动多个进程共享同一数据目录；最多保留 1000 条任务，配置更新会在满额时淘汰已完成的更新记录；若仍满额则需停止服务并归档状态数据。中断的运行任务在重启后标记失败、绝不重放外部副作用，排队任务可以恢复。
- **数据目录**：`.data/`、`.runtime/` 与 `bin/` 已被忽略，请勿提交；升级或迁移前保留状态文件以恢复已应用配置和代理快照。
- **尚未实现**：节点新增 / 编辑 / 删除、规则编辑、配置库编辑与删除、特权（非 GNOME）系统代理控制、非 Linux TUN 管理、外部控制器 TUN 管理、完整配置在线编辑；网络配置支持立即更新与定时更新，仅接受 Mihomo YAML/JSON 文档，不支持 Base64 节点订阅。完整配置中的 provider URL 会保存在服务器，由 Mihomo 负责后续更新。
- **配置应用的影响**：完整配置应用会关闭内核现有的 TUN 与透明代理；由 Mihomo 创建的 `Nulas` 网卡与自动路由会影响本机流量，请确认理解后再开启 TUN。
- **配置更新验证**：前端类型检查与生产构建、Go vet 与后端构建通过；配置更新的接口、后台调度、重启恢复、地址快照隔离、失败保留原配置、持久化失败回滚、凭据隐藏及任务历史上限已通过 race 测试。除两项固定监听 9090 的既有托管内核测试因端口占用无法通过外，其余后端 race 测试通过；临时数据环境下已验证“立即更新”和保存定时更新设置的页面交互。
- **验证状况**：本次安装 / 更新改动的 17 项 Python 测试、CLI race 测试、前端类型检查与生产构建、Go vet、Linux 后端构建，以及 Windows amd64 / macOS arm64 后端交叉编译通过。Go race 全套中的 4 项托管内核 / TUN 用例因本机 9090 端口占用及 TUN 状态依赖失败，未改动主机网络来规避。内核接口使用模拟控制器验证；Windows / macOS 安装器、包管理器实际安装、真实自动升级、三平台托盘显示与真实 TUN 流量尚未实机验证。

## 目录

- `AGENTS.md`：项目要求、常用命令、保护边界与提交规范（`CLAUDE.md` 为指向它的符号链接）。
- `web/`：SolidStart 网页、样式与前端测试。
- `backend/`：Go API、后台 worker、持久化与测试。
- `scripts/`：构建、启动、内核下载、服务安装与托盘辅助脚本。
- `deploy/`：可选的 systemd 服务模板（未自动安装）。

项目名称保持 Nulas，遵循上游 README 对非官方下游项目命名的要求。若未来引入上游源码，请保留上游许可与版权声明。

### 内核管理

打开侧边栏「内核管理」（`/core`），可分页查看 [Mihomo 官方稳定版本](https://github.com/MetaCubeX/mihomo/releases)、查看发布说明、切换到历史版本，或一键更新到最新稳定版。需要 GitHub 网络访问；请求限额、平台不支持、缺少 SHA256 或下载校验失败会显示失败结果。仅支持 Nulas 生产入口启动的托管内核，外部控制器需自行管理。

操作先保存明确版本的后台任务，再由单一后台 worker 下载、校验和重启，关闭页面不影响执行。版本存放在 `.runtime/core/versions/<tag>/`，通过原子写入的 `active-version` 选择；旧的 `.runtime/core/mihomo` 与用户配置保留，已下载的版本可复用。启动及控制接口检查失败时尝试恢复原版本，恢复失败会显示实际错误。重启会短暂中断连接，加载最后成功应用的配置，TUN 保持关闭，需手动重新开启；不会修改系统代理设置。重启 Nulas 后，中断的运行任务标记为失败，不自动重放；等待中的任务可以继续。
