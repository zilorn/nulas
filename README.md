# Nulas

基于 [MetaCubeX/mihomo · Meta](https://github.com/MetaCubeX/mihomo/tree/Meta) 的网页快速配置面板。前端使用 **Vite + SolidStart 2（SSR）+ SolidJS + TypeScript**，后端使用 **Go** 标准库，通过 Mihomo REST API 读取并修改核心运行参数。Nulas 是非官方下游项目，不复制上游源码，不捆绑内核二进制。

## 介绍

- **快速配置**：编辑 mixed-port、mode、allow-lan、ipv6、log-level，生成配置并应用到内核。
- **配置库**：导入本地文件、粘贴 JSON / 顶层标量 YAML，或从 HTTP/HTTPS 地址一次性下载完整 Mihomo 配置，按名称搜索、载入、生成或直接应用。
- **节点管理**：读取已连接内核，选择手动代理组（Selector）成员，切换规则 / 全局 / 直连模式并显示 MATCH 兜底出口。
- **后台任务**：安装、生成、应用、TUN、系统代理等操作进入持久化单 worker 队列，关闭网页不中断。
- **系统设置**：虚拟网卡（TUN）、系统代理、开机自启与桌面托盘开关，偏好写入后端原子状态文件。
- **托管内核**：默认按需下载官方内核并以其基础直连配置启动；设置 `MIHOMO_CONTROLLER` 时改为连接已有内核。

核心凭据只保存在服务端，API 默认仅监听回环地址。当前版本面向本机使用，远程部署所需的认证与 TLS 尚未实现。

## 安装

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

打开 [http://127.0.0.1:8080](http://127.0.0.1:8080)。`nulas stop` / `nulas restart` 同时停止或重启前后端，`nulas --help` 查看命令。可将项目 `bin` 的绝对路径加入 shell 配置的 PATH。服务依赖当前项目路径与构建产物，更新代码后重新执行 `./scripts/build.sh` 与 `nulas restart`；迁移目录或更换 Node 路径后先重新执行 `nulas install`。

使用 `nulas config port 9090` 保存 Web 页面与 API 的端口，再运行 `nulas restart`（前台模式请停止后重新启动）。`nulas config port` 查询保存值，未配置时为 `8080`。允许范围为 `1–65535`；端口必须可用，普通用户通常不能绑定低于 `1024` 的端口，生产启动脚本的 SSR 服务占用 `3001`。该配置不改变 Mihomo 的代理端口，也不改变开发服务器的 `3000` 端口。

端口通过原子写入保存至当前用户配置目录的 `nulas/server.json`（Linux 默认 `~/.config/nulas/server.json`，遵循 `XDG_CONFIG_HOME`；macOS/Windows 使用系统用户配置目录），从任意工作目录执行命令均使用同一文件。CLI 与服务应使用同一用户和配置目录。`NULAS_ADDR` 优先于保存值；若服务的 `service.env` 中设置了它，请移除该覆盖以使用保存端口。默认地址仍为 `127.0.0.1:8080`。

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

开发页面为 http://localhost:3000，开发服务器将 `/api` 转发至 `127.0.0.1:8080`（请勿自定义 `NULAS_ADDR`）。已有 Mihomo 服务时：

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
| `nulas install` | 安装 / 更新 Linux 用户级组合服务，不启动、不开启自启（需要 Python 3） |
| `nulas status` | 查看服务状态与近期日志；未运行时返回非零退出码 |
| `nulas start` / `stop` / `restart` | 启动、停止、重启前后端 |
| `nulas`（无参数） | 前台只运行后端 |
| `nulas --help` | 显示用法 |

命令可从任意目录运行，操作对象是当前用户的 Nulas 组合服务，不会操作系统级同名服务或其他 Mihomo 实例。

### 页面功能

- **快速配置**：保存五项核心参数；`应用到内核` 调用官方 [`PATCH /configs`](https://wiki.metacubex.one/api/#configs)，只改运行参数、不替换节点与规则，这些修改在重启内核后可能被内核配置文件覆盖。
- **配置库**：最多 100 份、名称不可重复，随服务状态原子持久化。导入同时支持核心参数文件与包含节点、代理组、规则、DNS、hosts、sniffer、providers 的完整 YAML/JSON；使用 `go.yaml.in/yaml/v3` 校验语法与重复字段，不支持多文档、Base64 节点订阅或加密格式，完整文档与节点凭据仅保存在后端状态，不会通过列表、创建响应或任务接口返回。网络导入可选填写名称，后端直接下载（`User-Agent: clash.meta`、15 秒超时、最多 5 次重定向、禁止 HTTPS 降级），不设内容大小上限，地址与令牌不持久保存。配置库目前不支持编辑与删除。
- **生成 / 直接应用完整配置**：任务保存独立的完整文档快照，生成到 `.data/<job-id>.yaml` 时保留原文；直接应用调用 `PUT /configs?force=true` 的 `payload`，重载节点、代理组、规则、DNS 与 providers，不覆盖用户原始配置文件。应用副本固定本机监听，禁用 TUN、iptables、透明代理、自定义入站监听与 NTP 写入，移除 DNS 服务监听和导入的控制接口字段，凭据保持不变；HTTP providers 使用每次任务独立的 `.nulas/` 相对缓存路径。应用会关闭内核现有的 TUN 与透明代理，但不会修改系统代理设置。应用成功后快照会原子保存，托管内核下次启动直接加载该快照；载入模板或生成文件不会替换已应用快照。
- **节点管理（`/nodes`）**：通过 `GET /proxies`、`PUT /proxies/{name}` 与 `PATCH /configs` 选择手动代理组（Selector）成员并切换模式；全局模式只显示 GLOBAL，规则模式隐藏 GLOBAL 并通过 `GET /rules` 显示 MATCH 兜底出口（无此类规则时显示内核默认 DIRECT），直连模式不提供节点选择。改变 MATCH 目标本身需要修改完整配置后显式应用。响应最多 2 MB，密钥留在服务端。
- **后台任务（`/tasks`）**：状态按 `queued → running → succeeded / failed` 持久保存，网页每两秒更新；失败可重新提交。操作失败不会自动重试。
- **系统设置**：虚拟网卡（TUN）、系统代理、开机自启与托盘开关。开关状态来自后端回读，未满足运行条件时显示具体原因；设置作用于后端所在主机。

### 配置变量

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `NULAS_ADDR` | `127.0.0.1:8080`（或 CLI 保存的端口） | Web/API 监听地址，显式设置时覆盖 CLI 端口 |
| `NULAS_SSR_URL` | `http://127.0.0.1:3001` | Go 转发页面请求的本机 SSR 地址（仅支持 HTTP 回环 IP） |
| `NULAS_WEB_DIR` | 空 | 显式启用旧版静态网页托管；不能用于 SSR 构建 |
| `NULAS_DATA_DIR` | `.data`（相对工作目录） | 配置、任务和生成文件 |
| `MIHOMO_CONTROLLER` | 空 | 内核控制 API，例如 `http://127.0.0.1:9090` |
| `MIHOMO_SECRET` | 空 | 内核 API 密钥 |
| `NULAS_CORE_DIR` / `NULAS_CORE_INSTALLER` / `NULAS_PYTHON` | `../.runtime/core` / `../scripts/install_core.py` / `python3` | 托管内核路径与解释器覆盖 |
| `NULAS_TRAY_SCRIPT` | `../scripts/tray.py` | 托盘辅助脚本路径 |

### 桌面托盘（可选）

托盘默认关闭，提供打开面板、节点管理与后台任务入口，使用默认浏览器。先在后端使用的 Python 环境安装可选依赖：

```sh
python3 -m pip install -r scripts/requirements-tray.txt
# Windows
python -m pip install -r scripts/requirements-tray.txt
```

macOS/Windows 使用 pystray 原生后端；Linux 需要 GTK/AppIndicator 的 PyGObject 运行环境与桌面托盘区域，GNOME 通常还需 AppIndicator 扩展。安装或桌面支持不足时会显示失败并提供重试入口。

## 注意事项

- **仅限本机**：API 与 SSR 默认绑定回环地址，同源写入校验、拒绝外站请求，不信任客户端转发头。远程部署必须自行实现认证与 TLS；不要把控制接口凭据放进前端或提交到仓库。
- **不要提权**：只给 Mihomo 二进制授予必要能力（如 `sudo setcap cap_net_admin,cap_net_raw+ep <mihomo>`），不要以 root 运行 Nulas 或 Node，也不要使用 `sudo nulas`。
- **平台限制**：系统代理仅支持具有桌面会话与 `gsettings` 的 Linux GNOME 普通用户环境；TUN 管理仅支持 Nulas 启动的 Linux 托管内核（外部控制器与其他系统不提供）；开机自启为 Linux 用户级 systemd。跨平台托盘不会改变这些边界。
- **偏好与实测分离**：开关偏好保存在 `NULAS_DATA_DIR/state.json` 的 `preferences`，重启不重放系统代理 / TUN 操作，TUN 始终默认关闭；页面显示的实测状态与保存偏好可能不同，失败会显式标注。
- **任务与持久化**：单进程单 worker，每次仅一个待处理任务，不要启动多个进程共享同一数据目录；最多保留 1000 条任务，达到上限后需停止服务并归档状态数据。中断的运行任务在重启后标记失败、绝不重放外部副作用，排队任务可以恢复。
- **数据目录**：`.data/`、`.runtime/` 与 `bin/` 已被忽略，请勿提交；升级或迁移前保留状态文件以恢复已应用配置和代理快照。
- **尚未实现**：订阅自动刷新、节点新增 / 编辑 / 删除、规则编辑、配置库编辑与删除、特权（非 GNOME）系统代理控制、非 Linux TUN 管理、外部控制器 TUN 管理、完整配置在线编辑；网络导入为一次性下载，不提供订阅自动更新。完整配置中的 provider URL 会保存在服务器，由 Mihomo 负责后续更新。
- **配置应用的影响**：完整配置应用会关闭内核现有的 TUN 与透明代理；由 Mihomo 创建的 `Nulas` 网卡与自动路由会影响本机流量，请确认理解后再开启 TUN。
- **验证状况**：TypeScript 检查、生产构建、Go 测试（race）、`go vet`、Python 测试与托盘协议测试均通过；内核交互使用模拟控制接口验证。实机系统代理切换、真实 TUN 网卡流量、三平台托盘显示、主机 systemd 服务安装与真实订阅地址尚未实测。Go race 全套中有依赖主机环境的用例（9090 端口占用、已有 TUN 网卡）可能失败，属已知环境依赖。

## 目录

- `AGENTS.md`：项目要求、常用命令、保护边界与提交规范（`CLAUDE.md` 为指向它的符号链接）。
- `web/`：SolidStart 网页、样式与前端测试。
- `backend/`：Go API、后台 worker、持久化与测试。
- `scripts/`：构建、启动、内核下载、服务安装与托盘辅助脚本。
- `deploy/`：可选的 systemd 服务模板（未自动安装）。

项目名称保持 Nulas，遵循上游 README 对非官方下游项目命名的要求。若未来引入上游源码，请保留上游许可与版权声明。
