# Nulas

基于 [MetaCubeX/mihomo · Meta](https://github.com/MetaCubeX/mihomo/tree/Meta) 的网页快速配置面板。使用 **Vite + SolidStart 2（SSR）+ SolidJS + TypeScript** 前端与 **Go** 后端，通过 Mihomo REST API 管理核心运行参数。当前为可运行的基础框架，未复制上游源码，不包含内核二进制。

## 快速使用（Linux 后台服务）

需要 Node.js 24+、pnpm、Go 1.23+、Python 3 和可用的用户级 systemd。以普通用户在项目根目录执行：

```sh
./scripts/build.sh           # 一键安装前端依赖、检查类型、构建前后端
export PATH="$PWD/bin:$PATH" # 当前终端可直接使用 nulas
nulas install               # 安装前后端组合服务，不自动启动
nulas start                 # 同时启动后端与前端
nulas status                # 查看组合服务状态
```

打开 [http://127.0.0.1:8080](http://127.0.0.1:8080)。使用 `nulas stop` 同时停止前后端，`nulas restart` 同时重启；`nulas --help` 查看命令。可将项目 `bin` 的绝对路径加入 shell 配置的 PATH，以便新终端直接使用 `nulas`。服务依赖当前项目路径和构建产物，请保留项目目录；更新代码后重新执行 `./scripts/build.sh` 与 `nulas restart`。迁移目录或更换 Node 路径后先重新执行 `nulas install`。

默认后端会按需下载并启动托管 Mihomo；已有内核时，在启动服务前把 `MIHOMO_CONTROLLER` 和 `MIHOMO_SECRET` 等配置写入 `~/.config/nulas/service.env`，权限设为 `600`。安装器不会复制终端环境中的凭据。安装和构建不会启动服务或下载内核。CLI 仅管理当前用户的 Nulas 组合服务，开机自启另见下方“系统设置与前后端开机自启”。

只需前台运行时，构建后执行 `./scripts/start.sh`；开发环境使用 `./scripts/dev.sh`，详见“本地启动”。

## 按需下载内核

仓库不包含上游源码或二进制。已有内核时可直接连接；需要内核时运行（Python 3.10+）：

```sh
python3 scripts/install_core.py
# 或指定上游稳定 Release
python3 scripts/install_core.py --version v1.19.0
```

脚本从上游 GitHub Releases 获取官方编译文件，自动匹配 Linux/macOS/Windows 的 amd64/arm64 平台，并校验压缩包 SHA256。上游没有提供 digest 时需要传入自行核实的 `--sha256`。仅使用 Python 标准库，不克隆或编译上游源码。内核存放在忽略目录 `.runtime/core/`，不会自动执行或覆盖已有内核。升级可通过 `--output` 指定新目录。

下载后使用你自己的完整配置启动内核，例如：

```sh
.runtime/core/mihomo -d /path/to/core-data -f /path/to/config.yaml
```

Windows 使用 `mihomo.exe`。Nulas 后端默认自动安装并启动本地内核；指定 `MIHOMO_CONTROLLER` 时连接已有服务。手动下载脚本本身只负责安装。

## 本地启动

需要 Node.js 24+、pnpm 和 Go 1.23+。Linux/macOS 上可一键启动（Bash 4.3+；macOS 默认 Bash 版本较旧时需使用新版 Bash）：

```sh
./scripts/dev.sh
```

脚本安装锁定的前端依赖，编译并同时启动前后端。后端默认检查、安装并启动本地内核，按 Ctrl+C 会停止前后端和托管内核；任一服务退出也会停止另一项服务。可从任意工作目录调用脚本。开发代理固定使用 `127.0.0.1:8080`，请勿自定义 `NULAS_ADDR`。后端沿用环境变量配置，默认数据仍保存到 `backend/.data`。

也可使用两个终端分别运行：

```sh
cd backend
go run .
```

```sh
cd web
pnpm install --frozen-lockfile
pnpm dev
```

打开 http://localhost:3000。开发服务器将 `/api` 转发至 `127.0.0.1:8080`，浏览器无需跨域访问后端。开发代理在 SolidStart 外层接收 `/api` 并保留浏览器的 Host，避免内部路由代理改写 Host 导致同源写入被拒绝。后端继续校验 Origin 的协议与地址，拒绝外站写入；不信任客户端提供的转发头。修改开发配置后需重启前端开发服务。

已有 Mihomo 服务时，用以下方式启动后端（密钥使用实际配置值，不要提交到 Git）：

```sh
cd backend
MIHOMO_CONTROLLER=http://127.0.0.1:9090 MIHOMO_SECRET=your-secret go run .
```

Mihomo 本身需要启用 `external-controller` 和对应 `secret`。外部控制接口地址和密钥由后端环境变量提供；托管内核的随机密钥只保存在后端内存与私有临时配置文件中。面板“后端已连接”表示 Nulas API 在线，不代表内核在线；内核错误会显示在任务结果中。

## 已实现

- 编辑并保存 mixed-port、mode、allow-lan、ipv6、log-level。
- 配置管理界面：创建命名核心配置，导入本地文件、粘贴 JSON / 顶层标量 YAML 或从 HTTP/HTTPS 地址一次性下载，按名称搜索并载入快速配置页。最多保存 100 份，名称不可重复，随服务状态原子持久保存；载入不自动调用内核，也不改变已提交任务的快照。
- 导入支持核心参数文件，以及包含节点、代理组、规则、DNS、hosts、sniffer 和 providers 的完整 Mihomo YAML/JSON 对象。使用 `go.yaml.in/yaml/v3` 校验语法与重复字段，支持嵌套、注释和锚点，不支持多文档、Base64 节点订阅或加密格式。完整文档及节点凭据仅保存在后端状态中，不通过配置列表、创建响应或任务接口返回。导入时不访问节点或 providers；Mihomo 的语义校验在应用时执行，失败会显示在任务中。配置库尚不支持编辑、删除。
- 网络导入的配置名称可选：手动填写时优先使用该名称，留空则读取文件中的 `name`；文件没有名称时使用“网络导入配置”。名称仍不可重复。网络导入由后端直接下载，发送 Mihomo 兼容的 `User-Agent: clash.meta` 以请求 Clash 格式（服务是否支持取决于提供方），15 秒超时，最多 5 次重定向，不允许 HTTPS 降级至 HTTP；响应须为 HTTP 200，下载内容不设大小上限，下载后沿用同一配置校验。地址可访问后端能够连接的网络（包括本机及局域网），不支持地址中的用户名、密码或片段；地址及查询令牌不持久保存，也不转发控制接口凭据。下载或校验失败不会新增配置。返回 HTML 网页时提示使用文件直链，Base64 解码后为非 UTF-8 二进制数据时提示可能存在加密或封装；无法识别的 YAML 键值会提示行号，错误不附带下载正文或地址。Base64 编码的节点订阅及明文节点链接会被识别并提示不支持，不会作为 YAML 解析或保存，也不会在错误中暴露节点凭据。此功能为一次性配置文件导入，不提供订阅自动更新。完整配置中的 provider URL 会随文档保存在服务器，后续 provider 更新由 Mihomo 管理。
- 配置和任务原子写入磁盘，配置写入校验，限制请求大小。
- 核心参数配置仍可载入快速配置页，后台生成只包含五项参数。完整配置在配置库直接选择“生成”或“直接应用”：任务保存独立的完整文档快照，生成到 `.data/<job-id>.yaml` 时保留原文；应用调用 Mihomo `PUT /configs?force=true` 的 `payload`，重载节点、代理组、规则、DNS 及 providers，不覆盖用户的原始配置文件。应用副本固定本机监听，禁用 TUN、iptables、透明代理、自定义入站监听及 NTP 系统时间写入，移除 DNS 服务监听和导入的控制接口字段；控制接口及凭据保持不变。HTTP providers 使用每次任务独立的 `.nulas/` 相对缓存路径，避免覆盖导入文件所指定的缓存。已有 TUN 或透明代理运行时也可直接应用，无需先手动关闭；重载会按应用副本关闭现有 TUN 和透明代理，系统代理设置不修改。原始配置仍完整保留在状态和生成文件中。
- `应用到内核`：后台调用官方 [`PATCH /configs`](https://wiki.metacubex.one/api/#configs) 接口，仅修改运行参数，不替换原有节点、规则及控制接口配置。这些运行修改重启内核后可能被内核配置文件覆盖。
- 独立后台任务页面 `/tasks`，展示任务结果、提交时间与状态筛选。
- 节点管理页面 `/nodes`：通过官方 [`GET /proxies`、`PUT /proxies/{name}` 与 `PATCH /configs`](https://wiki.metacubex.one/api/) 读取当前内核配置，选择手动代理组（Selector）的成员，并立即切换规则、全局、直连模式。全局模式仅显示 GLOBAL 组；规则模式隐藏 GLOBAL，使用规则指定的代理组；直连模式仅显示直连说明，不提供节点选择。代理组侧栏与节点列表分别在固定高度区域内滚动，移动端代理组横向滚动。规则模式额外通过 `GET /rules` 读取首个启用且目标不是 PASS / PASS-RULE 的 MATCH 规则，显示未命中前面规则时的兜底出口；没有此类 MATCH 时显示内核默认 DIRECT。兜底出口是现有代理组时可进入该组选择成员，自动组只展示；改变 MATCH 的目标本身需修改完整配置后显式应用，不提供规则编辑。规则读取失败单独显示，不影响节点列表。密钥留在服务端，接口响应仅返回展示字段，响应最多 2 MB。操作失败明确显示错误，不自动重试外部操作。运行选择可能在内核重启或重载配置后恢复，模式切换不覆盖快速配置草稿或配置库。
- 任务状态 `queued → running → succeeded / failed` 持久保存；网页每两秒更新状态，关闭网页不会取消操作。
- 后端重启后恢复排队任务；之前执行中的任务标记失败，避免重复执行不确定的外部操作。操作失败可重新提交。

后台任务需要 Go 服务持续运行。队列为单进程单 worker，每次仅允许一个待处理任务；不要启动多个进程共享数据目录。最多保留 1000 条任务，达到上限后需停止服务并备份、归档状态数据。当前不包含订阅自动更新、节点新增/编辑/删除、规则编辑、特权系统代理管理（普通用户系统代理仅支持 Linux GNOME 桌面；TUN 管理仅支持 Linux 托管内核）。托管内核初始使用基础直连配置；可在配置库显式应用完整配置以载入节点与规则。

## 配置变量

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `NULAS_ADDR` | `127.0.0.1:8080` | 后端监听地址 |
| `NULAS_SSR_URL` | `http://127.0.0.1:3001` | Go 转发页面请求的本机 SSR 服务地址（仅支持 HTTP 回环 IP） |
| `NULAS_WEB_DIR` | 空 | 显式启用旧版静态网页托管；不能用于 SSR 构建 |
| `NULAS_DATA_DIR` | `.data`（相对工作目录） | 配置、任务和生成文件 |
| `MIHOMO_CONTROLLER` | 空 | 内核控制 API，例如 http://127.0.0.1:9090 |
| `MIHOMO_SECRET` | 空 | 内核 API 密钥 |

## 验证与构建

一键构建（Bash）：

```sh
./scripts/build.sh
```

脚本安装锁定依赖，运行前端类型检查，构建客户端资源、SSR 服务和 Go 后端。产物为 `web/.output/` 与 `bin/nulas`，不会下载或启动 Mihomo。构建完成后启动生产服务（Bash 4.3+）：

```sh
./scripts/start.sh
```

脚本同时启动 Go 与 Node SSR 服务。Node 固定绑定 `127.0.0.1:3001`，Go 默认绑定 `127.0.0.1:8080`；任一进程退出都会停止另一项服务，Ctrl+C 同时停止两者。旧部署须移除 `NULAS_WEB_DIR`。

打开 http://127.0.0.1:8080。以下命令可单独执行验证与构建：

```sh
cd backend
go test -race ./...
go vet ./...
go build -o ../bin/nulas .
```

```sh
cd web
pnpm typecheck
pnpm build
```

## 长期后台运行

`deploy/nulas.service` 和 `deploy/nulas-web.service` 分别提供 Linux systemd 后端与 SSR 服务模板。先构建，将二进制放在 `/opt/nulas/bin/nulas`，将整个 `web/.output/` 放在 `/opt/nulas/web/.output/`，安装 Node.js 24+，创建专用 `nulas` 用户，并配置 `/etc/nulas.env`（限制为管理员可读）。检查 Node 可执行文件与其他路径后安装并启用两项服务。模板未自动安装，不会修改当前系统服务。Go 任务 worker 与 Node 独立运行，SSR 服务停止不会取消后台任务。

生产环境使用 Go 作为统一入口：`/api/*` 由 Go 处理，页面、客户端资源及框架请求转发到本机 Node SSR 服务，无需 Nginx。SSR 服务不可用时页面返回明确的 502，API 仍然可用。`NULAS_SSR_URL` 可调整 SSR 地址，必须是 HTTP 回环 IP 地址。`NULAS_WEB_DIR` 仅保留旧版静态产物兼容模式，设置后优先于 SSR；新构建不生成可独立托管的 SPA 入口。

分别启动服务时使用两个终端：

```sh
cd web
pnpm start
```

```sh
cd backend
../bin/nulas
```

打开 http://127.0.0.1:8080。SSR 生成页面结构与表单，浏览器接管后读取配置、节点和任务并更新状态；首次 SSR 不调用安装、应用或节点切换操作。当前服务仅供本机使用；远程认证和 TLS 尚未实现。

## 目录

- `AGENTS.md`：英文项目要求、常用命令、保护边界和 Git 提交要求。
- `web/`：SolidStart 网页与样式。
- `backend/`：Go API、后台 worker、持久化和测试。
- `deploy/`：后台运行与生产代理模板。

项目名称保持 Nulas，遵循上游 README 对非官方下游项目命名的要求。若未来引入上游源码，请保留上游许可和版权声明。

## 当前验证与依赖状况

已验证 TypeScript 检查、pnpm 静态构建、Go 测试（含 race 检测）、Go vet、Go 托管页面，以及浏览器保存配置、后台生成和刷新后的持久化。内核应用使用模拟控制接口测试；未操作本机实际 Mihomo 实例。按需下载脚本已测试平台选择与解压，核实官方 Release 文件名和摘要，未下载或执行实际内核。

前端已迁移至 SolidStart 2、Vite 8 和 Nitro 3，移除 Vinxi。Node.js 24+ 是开发、构建与 SSR 运行要求；Nitro 3 当前使用带 beta 后缀的官方版本，后续升级需重新验证构建和请求转发。历史静态构建验证记录保留在下文，不能作为本次 SSR 验证结果。

### 自动安装与启动内核

未设置 `MIHOMO_CONTROLLER` 时，后端启动即创建持久化任务：检查本地可执行文件，必要时调用官方 SHA256 校验下载器，然后自动启动内核。无需打开浏览器。Python 3 和 GitHub 访问能力用于首次下载；已有内核不会重复下载。

后端使用独立的 `.data/managed-core/` 数据目录和每次启动生成的私有临时配置文件，不覆盖用户配置。配置绑定本机代理端口及 `127.0.0.1:9090` 控制接口，生成随机密钥，启动时默认关闭 TUN，并使用 `MATCH,DIRECT` 基础规则（[官方配置文档](https://wiki.metacubex.one/en/example/conf/)）。它不设置系统代理、不添加节点或订阅。代理端口沿用启动任务的配置快照，局域网访问初始关闭。

只有通过携带密钥的控制接口版本检查后，面板才显示“内核运行中”并启用应用操作。端口冲突、下载和启动失败会显示在面板及任务记录中；启动后的意外退出也会显示失败。失败或中断的安装/启动任务需要明确点击“重试安装 / 启动”，不会自动重放。正常停止后端会停止托管内核并删除临时密钥配置。正常重启后端会重新启动内核，并直接加载最后成功应用的持久快照；尚无应用记录时使用基础直连配置。

设置 `MIHOMO_CONTROLLER` 时，后端只连接外部内核，不启动或停止它。默认内核及安装脚本路径相对 `backend/` 为 `../.runtime/core` 和 `../scripts/install_core.py`；其他部署布局可使用 `NULAS_CORE_DIR`、`NULAS_CORE_INSTALLER`、`NULAS_PYTHON` 覆盖。

### 本次页面与节点功能验证

已通过前端类型检查与静态构建、Go vet 与编译、节点/模式/跨域保护测试及其余可运行的 Go race 测试。实际开发代理已验证 localhost 与 127.0.0.1 的同源请求正常进入参数校验，外站写入仍被拒绝。使用临时数据目录和模拟 Mihomo 验证生产页面直接访问、配置创建/载入、节点选择、模式切换和后台生成，并在浏览器检查节点与任务页面。未修改实际内核节点或模式。完整测试中的 `TestManagedCoreLifecycle` 和 `TestManagedCorePortConflict` 因本机 9090 已被已有内核占用而未通过，随后跳过这两项运行其余测试；没有停止已有服务。

配置管理导航使用响应式地址参数，支持从快速配置页直接切换、返回与刷新。在隔离的模拟 API 环境中已通过浏览器验证上述路径，前端类型检查与静态构建通过。

### SolidStart 2 SSR 迁移验证

已通过冻结锁文件安装、依赖兼容性检查、TypeScript 检查、Vite 客户端 / SSR / Nitro 构建，以及完整 `go test -race ./...`、`go vet ./...` 和后端编译。使用临时数据目录和未启动的外部控制接口地址运行开发与生产启动脚本，验证首页、配置管理参数、节点与任务页面的 SSR HTML、客户端资源加载、同源保存、后台生成与跨站写入拒绝；开发代理同时验证 localhost 与 127.0.0.1，未知 API 返回 JSON 404。浏览器验证配置管理导航、任务记录与客户端接管，无客户端错误或警告。SSR 停机返回 502、API 保持可用由集成测试覆盖；systemd 模板尚未安装或实机启用。未下载、启动或修改实际 Mihomo 内核。

本次依赖审计剩余 1 条 high 告警：SolidStart 的传递依赖 `braces@3.0.3`（[GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)）。审计建议的 3.0.4 尚未在 npm 注册表发布，未强制覆盖依赖；后续需跟进官方修复。`pnpm start` 默认固定绑定 `127.0.0.1:3001`；自定义回环端口可使用 `NITRO_HOST=127.0.0.1 NITRO_PORT=<port> node .output/server/index.mjs` 并同步设置 Go 的 `NULAS_SSR_URL`。

### 完整配置导入与应用

已在 main 验证本地与网络完整 YAML 导入、嵌套字段及锚点、重复字段拒绝、原文生成、独立任务快照和重启恢复、控制接口重载请求、失败状态及凭据隔离。应用副本通过已安装 Mihomo 的 `-t` 校验，使用临时目录且不下载 providers、不连接节点或启动监听。前端类型检查和生产构建、Go vet 和后端构建通过。完整 Go race 测试中两项托管内核测试仍因现有 9090 端口占用失败，排除这两项后的全套测试通过。未使用真实订阅地址验证远端响应，未修改正在运行的内核配置或系统网络。


2026-10-03：完整配置改为直接应用，移除 TUN／透明代理状态预检，应用副本仍禁用这些功能。前端类型检查、生产构建、Go vet 和后端构建通过；控制接口拒绝与中断任务不重放测试通过。Go race 测试排除四项依赖主机网络状态的测试后通过：两项托管内核测试受现有 9090 端口占用影响，两项 TUN 测试受现有运行网卡影响。未对当前运行内核执行完整配置重载。

### TUN 模式（Linux 托管内核）

快速配置页的“系统设置”提供虚拟网卡模式开关，齿轮入口显示 TUN 状态、权限检查与授权说明。此功能仅支持 Nulas 启动的 Linux 内核，外部控制器和其他操作系统不提供 TUN 管理。TUN 运行状态不保存到配置模板，服务重启后默认关闭，不自动重放上次开启操作。

需要用户介入时，页面显示针对当前内核绝对路径的命令：

```sh
sudo setcap cap_net_admin,cap_net_raw+ep /absolute/path/to/mihomo
getcap /absolute/path/to/mihomo
```

仅给 Mihomo 二进制授予权限，不要以 root 运行 Nulas 或 Node。授权后在原启动终端按 Ctrl+C 停止服务，然后以普通用户重新执行原启动命令（生产环境为 `scripts/start.sh`）。页面上的“检查并开启 TUN”会检查运行中子进程的有效权限及 `/dev/net/tun` 可访问性，通过后才提交开启任务。给文件授权不会改变已运行进程的权限。容器还需要宿主提供 TUN 设备、网络命名空间与能力，文件系统挂载或 capability bounding set 也可能阻止授权生效；页面检查失败时请先解决环境问题。内核升级或替换后需重新授权。撤销文件授权可运行 `sudo setcap -r /absolute/path/to/mihomo` 并重新启动服务。

开启任务只 PATCH TUN 设置：gVisor 协议栈、自动路由和出口网卡检测，关闭自动防火墙重定向及 DNS 劫持。由 Mihomo 创建 `Nulas` 网卡并设置路由，不运行 sudo、不更改系统代理或防火墙。初始托管配置为 `MATCH,DIRECT`；此开关沿用当前内核节点、规则与 DNS，不提供代理节点。TUN 开启时也可在配置库直接应用完整配置；重载会关闭 TUN，需要时可在应用成功后重新开启。配置回读与本机运行网卡检查通过后任务才成功；这不保证端到端网络可达。检查失败会尝试关闭并报告失败，关闭失败需用户检查内核日志与主机网络。开启前请了解自动路由会影响本机流量，必要时使用“关闭 TUN”恢复。

任务通过现有单 worker 持久队列执行，网页关闭不影响操作；中断的运行任务在重启后标为失败。执行结果在后台任务页可见。实现依据 [Mihomo TUN 文档](https://wiki.metacubex.one/en/config/inbound/tun/) 和 [配置接口源码](https://github.com/MetaCubeX/mihomo/blob/Meta/hub/route/configs.go)。

验证：TUN 权限不足拒绝、外部内核保护、状态不一致拒绝、关闭请求范围、开启失败回退、任务中断恢复与配置快照拒绝的测试通过；排除占用 9090 端口的两个托管启动测试后，Go race 全套测试、Go vet、后端构建、TypeScript 检查及生产构建通过。使用模拟 API 在浏览器检查了授权命令和重新检查流程。尚未授予实际内核权限或创建真实 TUN 网卡，未验证实机流量转发。

### 节点模式与滚动验证

已通过 `pnpm typecheck`、`pnpm test:nodes`、生产构建、Go vet 与后端编译。模式测试覆盖规则 / 全局 / 直连、未知模式及缺失 GLOBAL；兜底接口测试覆盖 MATCH 顺序、禁用规则、PASS、无 MATCH 和无效响应。Go race 全套运行中两项已有托管内核测试因真实服务占用 9090 端口失败，跳过 `TestManagedCoreLifecycle`、`TestManagedCorePortConflict` 后其余全部通过。使用临时数据与模拟控制器验证独立滚动、三种模式过滤、返回规则模式保留代理组、兜底组导航和成员切换摘要更新，浏览器无错误。未修改真实内核模式、节点或系统网络。


### 系统设置与前后端开机自启

快速配置页沿用白色卡片、绿色开关样式，提供虚拟网卡模式、系统代理、开机自启三个选项；不提供静默启动选项。开关状态来自后端回读，未满足运行条件时显示具体原因。设置作用于后端所在主机，不影响访问网页的其他设备。

系统代理仅支持具有桌面会话与 `gsettings` 的 Linux GNOME 普通用户环境，只影响遵循 GNOME 代理设置的应用，不是所有服务器流量的全局代理。无桌面服务器禁用此选项。开启要求本机回环 Mihomo 控制器及可连接的混合代理端口，端口取自内核当前配置而不是网页编辑值。代理任务通过持久队列执行，保存原 mode、HTTP/HTTPS/SOCKS 地址、端口与 HTTP 认证开关，再设置回环代理并逐项回读；关闭恢复原设置。开启失败尝试恢复，恢复失败保留快照并显示失败。服务中断不重放运行中的操作，页面提供恢复原代理设置入口。外部改变端点时不会继续显示 Nulas 代理已开启；恢复会使用开启前的快照。不会修改环境变量、其他桌面代理、系统级配置、防火墙或浏览器自身的独立代理设置。

开机自启通过 Linux 用户级 systemd 服务同时运行 Go 后台和 Node SSR 前端，沿用 `scripts/start.sh` 的进程管理。先构建并以运行 Nulas 的普通用户安装服务：

```bash
scripts/build.sh
export PATH="$PWD/bin:$PATH"
nulas install
loginctl enable-linger
```

`enable-linger` 可能需要主机管理员授权；应用不会代替用户提权。没有 linger 的用户服务只能保证登录后启动，因此页面不把它标为已开启机自启。安装脚本仅写入带 Nulas 标记的用户服务文件并重新加载，不开启、不启动、不停止现有服务，也不会覆盖其他来源的同名服务。安装后在网页开启“开机自启”，只改变下次开机的注册状态；关闭不会停止当前服务。迁移项目路径或更换 Node 安装位置后，重新运行 `nulas install`。CLI 安装复用原有 Python 安装器，需要 Python 3；仍可直接运行 `python3 scripts/install_service.py`。

可选部署环境变量放入 `~/.config/nulas/service.env`（systemd EnvironmentFile 格式，权限设为 `600`）；例如 `NULAS_DATA_DIR`、`NULAS_ADDR`、`MIHOMO_CONTROLLER`、`MIHOMO_SECRET`。相对路径以启动脚本的 `backend/` 为基准，默认继续使用原 `backend/.data`。安装器记录 Node 的所在目录，不自动复制当前终端中的控制器凭据或其他环境变量。使用自定义部署配置时，先配置此文件再启用自启。不要把凭据文件提交到仓库。

CLI 服务管理（Linux 普通用户，使用同一个 `nulas.service` 同时管理后端与 SSR 前端）：

```bash
nulas install  # 安装/更新服务，不启动或启用开机自启
nulas status   # 查看服务状态与近期日志；未运行时返回非零退出码
nulas start    # 启动前后端
nulas stop     # 停止前后端
nulas restart  # 重启前后端
```

这些命令可从任意目录运行；保留构建产物 `bin/nulas` 在项目中，可将项目的 `bin` 加入 shell 的 PATH，或在 PATH 目录创建指向它的符号链接。`nulas --help` 查看帮助；无参数运行仍只启动前台后端，`scripts/start.sh` 前台启动两项服务。安装服务后需要可用的用户 systemd 会话；不使用 `sudo nulas`。停止或重启会中断当前服务和托管内核，正在执行的任务重启后标记失败，待处理任务可以恢复；不会停止外部 Mihomo 控制器。CLI 不更改开机自启设置。命令报错返回非零退出码，不把失败显示为成功。旧的系统级双服务模板请继续使用对应的系统级管理命令，CLI 仅管理用户级组合服务。

日志查看：`journalctl --user -u nulas.service`。手动启动已安装服务：`systemctl --user start nulas.service`，先停止原终端中的服务以免端口冲突。服务不自动打开浏览器，前后端任一项退出时停止另一项，再由 systemd 按失败重启策略管理。TUN 开启状态不会被自启重放。

API：`GET /api/runtime/system` 返回能力、实测状态与说明；`PUT /api/runtime/system` 接受 `{"startup": true/false}`；系统代理操作使用 `/api/jobs` 的 `proxy-enable`、`proxy-disable`，结果可在后台任务页查看。实现依据 [systemd 用户 linger 文档](https://www.freedesktop.org/software/systemd/man/252/loginctl.html) 与 [GNOME 代理说明](https://help.gnome.org/gnome-help/net-proxy.html.en)。

验证：新增代理快照持久化、原设置恢复、失败回滚、外部端点变更检测、远程控制器拒绝、自启 linger 条件和请求验证测试均通过。TypeScript 检查、生产构建、Go vet、后端构建和 Python 测试通过；Go race 测试排除两个因现有服务占用 9090 而失败的托管启动用例后通过。使用模拟接口在浏览器验证了浅色设置面板与 TUN 权限不足提示；未实际切换主机代理、授予 TUN 权限、安装系统服务或重启主机。

### 已应用配置持久保存

应用成功后，后端将独立配置快照与成功任务一起原子保存。托管内核下次启动直接使用该快照，保留完整配置中的节点、代理组、规则、DNS 与 providers；后续快速配置应用只更新核心参数，不丢弃完整文档。只保存编辑内容、载入模板或生成文件不会替换已应用快照，应用失败也保留上一份成功配置。升级时可从已有成功应用任务恢复快照，运行中断的任务仍标为失败，不重放控制接口请求。

配置管理显示最后成功应用的名称、核心参数、时间和“已应用 · 已保存”，对应库卡片也显示标记，页面每两秒更新状态。快速配置的应用结果即使没有配置库模板，也会在该页面独立显示。此状态表示 Nulas 最后成功应用的记录，外部修改内核不会同步；外部控制器的进程重启仍由其自身配置管理负责。托管启动继续使用回环监听、新生成的服务端密钥，TUN 与透明代理默认关闭，不恢复系统代理或重放其他系统操作。完整文档与凭据仍只保存在后端。

验证：成功快照持久化、失败保留、完整配置与参数叠加、旧记录迁移、中断任务不重放、凭据隔离和启动安全设置测试通过；前端类型检查与生产构建、Go vet 和后端构建通过。Go race 全套中四项已有环境依赖测试受当前 9090 端口占用和运行中的 TUN 网卡影响，排除这四项后其余测试通过。未重启或重载实际运行内核，实际进程重启恢复尚未实机验证。

CLI 验证：`./scripts/build.sh` 一键完成冻结依赖安装、TypeScript 检查、前端生产构建和后端构建；CLI 单元测试、Go vet、Python 安装器测试通过。构建后的二进制在临时配置目录和模拟 systemctl 下验证了 install/start/stop/restart/status、失败退出码、同名服务保护及不初始化后端数据。全量 Go race 测试有四个已有失败（`TestManagedCoreLifecycle`、`TestManagedCorePortConflict` 的 9090 端口占用，以及 `TestTUNDisableOnlyPatchesTUNAndVerifies`、`TestTUNFailedListenerCreationRollsBack` 的主机 TUN 状态影响），修改前版本同样复现；排除这四项后其余测试通过。未安装或启停主机真实服务，实际 systemd 生命周期仍需部署环境验证。

## 桌面托盘与开关保存

快速配置页的“系统设置”提供托盘开关，支持 Windows 通知区域、macOS 菜单栏和 Linux 桌面托盘。托盘菜单可打开面板、节点管理及后台任务，使用默认浏览器，不引入桌面浏览器外壳。先在后端使用的 Python 环境安装可选依赖：

```sh
python3 -m pip install -r scripts/requirements-tray.txt
# Windows
python -m pip install -r scripts/requirements-tray.txt
```

macOS/Windows 使用 pystray 的原生后端；Linux 需要 GTK/AppIndicator 的 PyGObject 运行环境（通常由发行版包提供）及桌面托盘区域。GNOME 通常还需要 AppIndicator 扩展；纯 Xorg fallback 不支持菜单，因此不启用。无桌面会话的 systemd 服务、容器和 SSH 会话不能保证显示图标。应从当前用户的桌面会话启动后端。实现依据 [pystray 的平台说明](https://pystray.readthedocs.io/en/latest/usage.html)。安装或桌面支持不足时显示失败并提供重试，不会将保存偏好显示为运行成功。

托盘默认关闭，偏好保存到 `NULAS_DATA_DIR/state.json` 的 `preferences`，后端重启时尝试恢复一次；失败不循环重试。关闭托盘仅移除图标，不停止核心或任务。后端正常退出或通信管道关闭会清理托盘进程。托盘仅支持 HTTP 回环地址；端口沿用后端实际监听端口。`NULAS_PYTHON` 可指定解释器（Windows 默认 `python`，其他平台默认 `python3`），`NULAS_TRAY_SCRIPT` 可指定脚本路径（默认相对后端目录 `../scripts/tray.py`）。辅助进程不接收 Mihomo 控制接口凭据。

系统代理、虚拟网卡和开机自启的最后请求偏好也保存在同一原子状态文件中，不依赖浏览器。代理/TUN 偏好随任务入队一起保存，自启偏好在系统操作前保存；执行失败仍保留请求偏好和可见失败。实际开关状态始终从系统/核心读取，说明中可查看与保存偏好的差异。旧状态文件无对应字段时表示尚未保存选择，不覆盖原配置。

保存开启偏好不意味着自动接管网络：重启不重放系统代理/TUN 操作，TUN 仍默认关闭；队列任务继续遵守待执行可恢复、执行中标记失败的规则。系统代理的原设置备份和端口持久保存以供恢复；自启实际注册由 systemd 持久保存。完整配置应用可能关闭 TUN，此时偏好与实际状态会有差异，需要手动开启。系统代理仍仅支持 Linux GNOME，TUN 与开机自启仍仅支持 Linux，跨平台托盘支持不改变这些边界。

本次验证：前端类型检查、生产 SSR 构建、Go vet、Python 测试、托盘辅助进程协议/生命周期与开关持久化 race 测试，以及 Linux amd64、Windows amd64、macOS arm64 的 Go 构建通过。浏览器已验证托盘启动失败提示、重试入口和刷新后偏好保留。完整 Go race 测试中 `TestManagedCoreLifecycle`、`TestManagedCorePortConflict` 受本机 9090 端口已占用影响，`TestTUNDisableOnlyPatchesTUNAndVerifies`、`TestTUNFailedListenerCreationRollsBack` 受本机已有 Nulas TUN 网卡影响；排除这四项后其余测试通过。未改动现存服务或网卡。当前环境未安装 pystray，三平台真实托盘显示/菜单未实测，交叉编译不代表原生桌面验收。
