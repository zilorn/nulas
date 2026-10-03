# Nulas

基于 [MetaCubeX/mihomo · Meta](https://github.com/MetaCubeX/mihomo/tree/Meta) 的网页快速配置面板。使用 **SolidStart + SolidJS + TypeScript** 前端与 **Go** 后端，通过 Mihomo REST API 管理核心运行参数。当前为可运行的基础框架，未复制上游源码，不包含内核二进制。

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

需要 Node.js 22+、pnpm 和 Go 1.23+。Linux/macOS 上可一键启动（Bash 4.3+；macOS 默认 Bash 版本较旧时需使用新版 Bash）：

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

打开 http://localhost:3000。开发服务器将 `/api` 转发至 `127.0.0.1:8080`，浏览器无需跨域访问后端。

已有 Mihomo 服务时，用以下方式启动后端（密钥使用实际配置值，不要提交到 Git）：

```sh
cd backend
MIHOMO_CONTROLLER=http://127.0.0.1:9090 MIHOMO_SECRET=your-secret go run .
```

Mihomo 本身需要启用 `external-controller` 和对应 `secret`。外部控制接口地址和密钥由后端环境变量提供；托管内核的随机密钥只保存在后端内存与私有临时配置文件中。面板“后端已连接”表示 Nulas API 在线，不代表内核在线；内核错误会显示在任务结果中。

## 已实现

- 编辑并保存 mixed-port、mode、allow-lan、ipv6、log-level。
- 配置管理界面：创建命名核心配置，导入本地文件、粘贴 JSON / 顶层标量 YAML 或从 HTTP/HTTPS 地址一次性下载，按名称搜索并载入快速配置页。最多保存 100 份，名称不可重复，随服务状态原子持久保存；载入不自动调用内核，也不改变已提交任务的快照。
- 导入仅支持上述五项核心参数，文件最多 6 KB，API 请求最多 8 KB。缺省参数使用 7890 / rule / false / false / info。YAML 支持普通顶层键值、字符串引号和普通标量后的注释，不支持嵌套、锚点或多文档；包含节点、规则、订阅、控制接口密钥等额外字段会明确拒绝，避免丢失内容。当前不支持完整 Mihomo 配置导入或配置库编辑、删除。
- 网络导入由后端直接下载，15 秒超时，最多 5 次重定向，不允许 HTTPS 降级至 HTTP；响应须为 HTTP 200，下载内容最多 6 KB，下载后沿用同一配置校验。地址可访问后端能够连接的网络（包括本机及局域网），不支持地址中的用户名、密码或片段；地址及查询令牌不持久保存，也不转发控制接口凭据。下载或校验失败不会新增配置。此功能为一次性导入，不提供订阅更新。
- 配置和任务原子写入磁盘，配置写入校验，限制请求大小。
- `后台生成`：保存任务的配置快照为 `.data/<job-id>.yaml`。文件采用 JSON 形式（属于 YAML 1.2 的子集），只包含核心参数，不是含节点与规则的完整代理配置。
- `应用到内核`：后台调用官方 [`PATCH /configs`](https://wiki.metacubex.one/api/#configs) 接口，仅修改运行参数，不替换原有节点、规则及控制接口配置。这些运行修改重启内核后可能被内核配置文件覆盖。
- 任务状态 `queued → running → succeeded / failed` 持久保存；网页每两秒更新状态，关闭网页不会取消操作。
- 后端重启后恢复排队任务；之前执行中的任务标记失败，避免重复执行不确定的外部操作。操作失败可重新提交。

后台任务需要 Go 服务持续运行。队列为单进程单 worker，每次仅允许一个待处理任务；不要启动多个进程共享数据目录。最多保留 1000 条任务，达到上限后需停止服务并备份、归档状态数据。当前不包含订阅、节点、规则编辑、系统代理或 TUN 管理。托管内核使用基础直连配置，不提供代理节点。

## 配置变量

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `NULAS_ADDR` | `127.0.0.1:8080` | 后端监听地址 |
| `NULAS_WEB_DIR` | `../web/.output/public` | SolidStart 构建产物路径 |
| `NULAS_DATA_DIR` | `.data`（相对工作目录） | 配置、任务和生成文件 |
| `MIHOMO_CONTROLLER` | 空 | 内核控制 API，例如 http://127.0.0.1:9090 |
| `MIHOMO_SECRET` | 空 | 内核 API 密钥 |

## 验证与构建

一键构建（Bash）：

```sh
./scripts/build.sh
```

脚本安装锁定依赖，运行前端类型检查，构建静态网页和 Go 后端。产物为 `web/.output/public/` 与 `bin/nulas`，不会下载或启动 Mihomo。构建完成后启动生产服务：

```sh
cd backend
../bin/nulas
```

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

`deploy/nulas.service` 提供 Linux systemd 后端服务模板。先构建后端，将二进制放在 `/opt/nulas/bin/nulas`，创建专用 `nulas` 用户，并配置 `/etc/nulas.env`（限制为管理员可读）。调整模板中的路径后安装并启用该服务。它会在开机时启动并在异常退出后重启。模板未自动安装，不会修改当前系统服务。

生产网页由 Go 自行托管，无需 Nginx 或单独的 Node 服务。先运行 `cd web && pnpm build`，再运行 Go 服务，打开 http://127.0.0.1:8080。默认从 `../web/.output/public`（相对后端工作目录）加载网页；部署时通过 `NULAS_WEB_DIR` 指定绝对路径。页面与 API 使用同一端口，后台任务在同一 Go 服务中执行。当前服务仅供本机使用；远程认证和 TLS 尚未实现。

## 目录

- `AGENTS.md`：英文项目要求、常用命令、保护边界和 Git 提交要求。
- `web/`：SolidStart 网页与样式。
- `backend/`：Go API、后台 worker、持久化和测试。
- `deploy/`：后台运行与生产代理模板。

项目名称保持 Nulas，遵循上游 README 对非官方下游项目命名的要求。若未来引入上游源码，请保留上游许可和版权声明。

## 当前验证与依赖状况

已验证 TypeScript 检查、pnpm 静态构建、Go 测试（含 race 检测）、Go vet、Go 托管页面，以及浏览器保存配置、后台生成和刷新后的持久化。内核应用使用模拟控制接口测试；未操作本机实际 Mihomo 实例。按需下载脚本已测试平台选择与解压，核实官方 Release 文件名和摘要，未下载或执行实际内核。

当前 SolidStart 1.x / Vinxi 依赖审计存在 14 条传递依赖告警（11 high、3 moderate）；兼容范围内自动修复未消除，强制修复建议会破坏框架版本。生产只发布静态文件，由 Go 托管，不运行 Nitro/Vinxi 服务；开发服务器保持本机使用，后续需跟进框架更新。

### 自动安装与启动内核

未设置 `MIHOMO_CONTROLLER` 时，后端启动即创建持久化任务：检查本地可执行文件，必要时调用官方 SHA256 校验下载器，然后自动启动内核。无需打开浏览器。Python 3 和 GitHub 访问能力用于首次下载；已有内核不会重复下载。

后端使用独立的 `.data/managed-core/` 数据目录和每次启动生成的私有临时配置文件，不覆盖用户配置。配置绑定本机代理端口及 `127.0.0.1:9090` 控制接口，生成随机密钥，禁用 TUN，并使用 `MATCH,DIRECT` 基础规则（[官方配置文档](https://wiki.metacubex.one/en/example/conf/)）。它不设置系统代理、不添加节点或订阅。代理端口沿用启动任务的配置快照，局域网访问初始关闭。

只有通过携带密钥的控制接口版本检查后，面板才显示“内核运行中”并启用应用操作。端口冲突、下载和启动失败会显示在面板及任务记录中；启动后的意外退出也会显示失败。失败或中断的安装/启动任务需要明确点击“重试安装 / 启动”，不会自动重放。正常停止后端会停止托管内核并删除临时密钥配置。正常重启后端会重新启动内核。

设置 `MIHOMO_CONTROLLER` 时，后端只连接外部内核，不启动或停止它。默认内核及安装脚本路径相对 `backend/` 为 `../.runtime/core` 和 `../scripts/install_core.py`；其他部署布局可使用 `NULAS_CORE_DIR`、`NULAS_CORE_INSTALLER`、`NULAS_PYTHON` 覆盖。
