# ConfigFlow Go 后端架构总览

面向以后给 `server/` 写代码的贡献者。本文描述**当前代码**（分支 `go`，N8-11 完成、O13 终审之后），
包路径均相对 `server/`。规则类文档的分工：

| 文档 | 内容 |
|---|---|
| 根目录 `AGENTS.md`「Go 后端代码风格」 | 命名、错误、context、日志、`messages.go`、测试写法 |
| `PORTING.md` | 功能对等标准、类型规则、验收方式（移植期规则，仍然有效） |
| `AGENT-BRIEF.md` / `WORKFLOW.md` | 环境（只在测试服务器跑）、分支、审查、门禁流程 |
| `design/o3-concurrency.md` | 锁层级、后台循环、取消与关停的权威约定 |
| `design/n7-n8.md` | 存储模型与记录编解码规则 R1–R9 |
| `design/a14-publish.md`、`design/a16-a18-sync-follow-ha.md`、`design/r12-release.md` | 发布、规则同步/DNS 联动/HA、路由器发布的详细设计 |
| `parity/behaviour-changes.md` | 有意与 Python 不同的行为（新增差异必须登记） |

文中若与代码不一致，以代码为准，并顺手修正本文。

---

## 1. 总体结构

### 1.1 进程与端口

Go 服务是单个二进制 `cmd/configflow`，取代 Flask + waitress + nginx 三件套：

| 监听 | 默认 | 来源 | 内容 |
|---|---|---|---|
| 后端 API | `0.0.0.0:5001` | `CONFIGFLOW_BACKEND_HOST` / `CONFIGFLOW_BACKEND_PORT`（`server.BackendAddressFromEnv`） | `httpx.App`：全部 `/api/*`、`/mcp/` 等 380 个 method+path |
| Web 前端 | 80 | `CONFIGFLOW_WEB_PORT`，否则 `DATA_DIR/web-listener.json`（设置页保存），否则 80；另开 IPv6 监听 | 只有给了 `-frontend` / `CONFIGFLOW_FRONTEND_DIR` 才启用。`httpx.Edge` 是 `docker/nginx.conf` 的内置替代：静态文件、SPA 回退、gzip，`/api` 进程内转给 App。端口可在设置页热切换（`internal/weblistener`） |

同进程外的协作者：

- **Sub-Store**（Node.js，镜像内置于 `/opt/sub-store`，只监听回环 `SUB_STORE_BACKEND_API_PORT`，默认 3001）。
  订阅解析交给它，Go 只实现客户端和它的生命周期：`internal/substore` 负责按需/常驻启动、5 分钟空闲停止
  （循环 `sub-store-idle`）、版本更新；开发环境用 `SUB_STORE_URL` 指向外部实例。
- **Agent**（`backend/agents/go-agent`，独立仓内 Go 程序，跑在 Mihomo/OxiDNS/MosDNS 主机上）。Agent 主动
  `POST /api/agents/register`、`POST /api/agents/<id>/heartbeat`；面板通过 `agents.Client` 反向调用 Agent 的 HTTP API
  （能力查询、上传部署包、激活/回滚、日志、监控面板代理等）。协议以 Agent 源码为准，面板侧必须保持兼容。
  Agent 二进制由面板以静态文件 `/api/static/agents/` 提供下载，安装脚本由 `internal/installer` 渲染。
- **node-probe**（`backend/node-probe` 编译出的独立 worker）：节点测速子进程，由 `internal/health` 调用。
- **网络设备**：RouterOS REST / iKuai，经 `internal/devhttp`（不走代理、不跟随重定向、可按设备关闭 TLS 校验）。

> 发布镜像（`docker/Dockerfile`）没有 Python：tini 是 1 号进程，`configflow-start.sh` 先按需运行
> `configflow-autoupdate`（autoupdate 镜像），再 exec Go 服务；Go 服务用 `substore.Supervisor` 自己托管
> Sub-Store 子进程（按需启停、崩溃退避重启、关停时在请求收尾后停止，`lifecycle.SpawnService`）。

### 1.2 数据目录

`DATA_DIR`（`repo.DataDir`：环境变量，否则存在 `/data` 就用它，否则 `.`），启动时 `repo.ResolveDataDir`
展开 `~` 并解析符号链接。**一个数据目录只允许一个服务进程**：`internal/instance` 持有
`DATA_DIR/.configflow-server.lock`（外加 Python 调度器的生命周期锁，防止新旧版本同时跑），最多等 30 秒，失败退出 1。

主要内容：

| 路径 | 所有者 | 说明 |
|---|---|---|
| `configflow.sqlite3` | `internal/store`（经 `internal/repo`） | 配置文档：系统设置、共享资源、各配置空间（profile）；Agent 运行记录在同库的 `agent_state` 表 |
| `backups/configflow-v<N>-<UTC>.sqlite3` | `internal/repo` | 存储版本升级前的在线备份，保留到下一次升级 |
| `profiles/<id>/…`、生成物、缓存目录 | `internal/repo` 及各领域包 | 例如 `stats_history.json`、订阅/规则缓存、`.generated-previews/` |
| `task-status.json` | `internal/taskstatus` | 定时任务的最近结果（不进备份、不进修订） |
| `notifications.json` | `internal/notifications` | 通知回执 |
| `traffic/`、`traffic_history.sqlite3`、`oxidns_rankings.sqlite3` | `agents/trafficstore`、`agents/metrics`、`agents/monitor` | Agent 上报的统计；各自一个 owner |
| `web-listener.json` | `internal/weblistener` | 设置页保存的 Web 端口 |
| `app.log` | `internal/logx` | 与 Python 相同的行格式和轮转，日志页与诊断读取它 |
| `.agent-rule-sync/…` | `agents/rulesync` | 部署内容基线 |

原则：**每个文件一个 owner 包**，其他包通过 owner 的 API 访问，不直接读写。旧版（SQLite 之前）数据目录 Go 拒绝打开，
需要先用最后一个 Python 版本升级一次。

### 1.3 启动与关停（概要，细节见第 5 节）

`main` → `run`（`cmd/configflow/service.go`）：

1. 内存调优（cgroup 上限 × `CONFIGFLOW_MEMORY_LIMIT_RATIO`，未设 `GOMEMLIMIT` 时）、读取关停宽限期；
2. `instance.Acquire` → `logx.Setup` → `auth.Configure` → `repo.OpenDefault`（迁移、损坏处理）→ `repo.SetRepository`；
3. `lifecycle.Configure`；注册 Startup 步骤；`server.NewApp()`（执行全部 wire 函数、注册全部路由、装鉴权门禁）；
4. `lifecycle.RunStartup`：`load-config` → `subscription-cache-prune` → `agent-manager-init` → `metrics-load-history`
   （另有 webdav worker 的 Startup）；
5. 开始监听 API，可选启动 Web；`lifecycle.Start` 启动周期循环；
6. 等 SIGTERM/SIGINT → `lifecycle.Stop(grace)`。

---

## 2. 包分层与依赖方向

依赖只能自上而下。下层需要上层能力时用**注册钩子**（`repo.RegisterHooks`、`agents.RegisterHooks`、
`convert.MihomoHooks` 等，由上层在 wire 时注入），不得反向 import；`internal/server` 负责把它们组合起来。

```
cmd/configflow                       进程入口：环境、实例锁、监听、信号
  └─ internal/server                 组装：wirePackages → routePackages → auth.Install
       ├─ internal/api/*             HTTP 翻译层（一个 Flask blueprint 一个包）
       ├─ internal/mcp               MCP JSON-RPC，经 httpx.App.Invoke 调 REST
       ├─ 领域 owner（发布/设备/任务） deploy、agents/*、routers、gateway/*、setup、clients、discovery、refresh …
       ├─ 生成器                     configgen → convert、mosdns、oxidns、subs、rules
       ├─ 存储                       repo → store、model
       ├─ HTTP 基础                  httpx、auth、jsonresp、jsondecode、redact、localhttp
       └─ 运行时基础                 lifecycle、lockx、ctxx、clock、logx
叶子工具：yamlx、yamlbridge、netipx、weburl、clientrange、rulesetfmt、jsonstore、utilx、version、resource
```

### 2.1 基础层

| 包 | 职责 |
|---|---|
| `internal/model` | 存储文档的 Go 记录类型（`Document`、`Config`、`Shared`、`System`、`Agent`、`Router`…）和 `Fields`/`Map`/`Dynamic`/`model.Int`/`model.String`。保留未知成员与未改成员的原始字节和顺序；不做 I/O。`model.Marshal` 是存储数据唯一的 JSON 写出器 |
| `internal/store` | SQLite 引擎（`modernc.org/sqlite`，纯 Go）：表结构、`Head`（store_id + generation + commit_id）、行级修订；每次操作新开连接，不解释字段值 |
| `internal/repo` | 配置仓库，`configflow.sqlite3` 及旁边文件的唯一 owner：事务、`.config.lock`（L3）、请求内 `Scope` 快照、三路合并、导入导出、默认值和校验 |
| `internal/lifecycle` | 启动步骤、周期循环、受追踪的分离任务（`Spawn`）、分阶段关停；`Detach`/`BindRequest`/`SpawnerFrom` |
| `internal/lockx` | 可取消的进程内锁（`Mutex`、`RWMutex`、`Keyed`、`Flight`），运行时检查 L1→L2→L3 顺序，`Violations()` |
| `internal/ctxx`、`internal/clock` | 保留生命周期的 Detach 实现；共享时钟接口（测试用 `lifecycle/lifecycletest`） |
| `internal/logx` | 日志（格式同 Python：`时间 - logger 名 - 级别 - 消息`），唯一允许的日志出口 |
| `internal/yamlx` | yaml.v3 之上的无 schema YAML：`Parse`、`Marshal`（两空格、LF、不折行）、Node 帮助函数、`Equal`、`FromJSON`/`ToJSON`、保留注释的 `Edit` |
| `internal/jsonresp` / `internal/jsondecode` | 响应 JSON 编码（不转义 HTML；`EncodeUTF8` 保留 U+2028/2029 原文）；严格成员名的 DTO 解码 |

### 2.2 HTTP 层

| 包 | 职责 |
|---|---|
| `internal/httpx` | Werkzeug 兼容的 URL 匹配（`Router.GET/POST/…(path, endpoint, handler)`，`<id>`、`<path:x>` 等转换器）、`Ctx`（请求参数、`Bind`）、`Response` 构造器（`JSON`、`JSONStatus`、`Message`、`File`、`Stream`…）、请求管线（CORS、before 钩子、错误处理、全局 token 脱敏、`X-ConfigFlow-Profile`）、`Edge`、进程内调用 `Invoke`、请求体上限 `LimitRequestBody` |
| `internal/auth` | HS256 登录令牌、Bearer 解析、全局门禁 `Gate`（`auth.Install` 插到 before 链首位）、公开路径表（`publicExactPaths`/`publicPrefixes`/`publicEndpoints`） |
| `internal/redact` | 不得离开服务器的凭据集合和替换遍历，httpx 对每个 JSON 响应调用 |
| `internal/api/<blueprint>` | 每个包一个 `Register(*httpx.Router)`。只做 HTTP 翻译：解析请求、调 owner、映射状态码和中文消息。业务、锁、持久化都不在这里 |

请求管线（`httpx.App.ServeHTTP`）：`repo.WithScope(lifecycle.BindRequest(req.Context()))` → 路由匹配 →
before 链（鉴权门禁、profile 选择、首请求通知 `lifecycle.RequestSeen`）→ handler → 错误处理器 →
finalize（脱敏、CORS、头）→ 写回；结束时 `repo.ResetConfigContext`。handler 内 panic 会被兜底成 500，但
**不要依赖它**（风格规则禁止 panic）。

### 2.3 领域层（owner 包）

| 领域 | 包 | 职责 |
|---|---|---|
| Agent 注册表 | `internal/agents` | `Manager`（`agents.Get(ctx)`）：注册、心跳（内存 runtime state）、部署记录状态机、对 Agent 的调用（`Client`）、钩子表 `Hooks` |
| Agent 子领域 | `agents/metrics`、`agents/trafficstore`、`agents/monitor` | 监控历史（内存 + 定时落盘）、流量摘要、监控面板/DNS 统计/排行 |
|  | `agents/rulesync` | 规则自动同步（按部署回执对比，热加载或重新发布） |
|  | `agents/dnsfollow`、`agents/dnsha` | Mihomo→OxiDNS DNS 联动；OxiDNS 主备（成对发布、VIP 切换） |
|  | `agents/upgrades`、`agents/ipv6` | Agent 升级与地址跟随；IPv6 就绪检查与发布后验证 |
| 发布 | `internal/deploy`（+ `deploy/pushdiff`、`deploy/contentsync`） | 准备不可变部署包、预览、发布回执、上传到 Agent；推送差异；内容同步检查 |
| 生成 | `internal/configgen` | 手动下载/预览的配置生成与打包（`Engine`：mihomo/surge/loon/mosdns/oxidns） |
|  | `internal/convert`（+ `convert/mihomoenv`） | Mihomo/Surge/Loon 转换器、节点转换、dialer、EasyTier、DNS 导出 |
|  | `internal/mosdns`、`internal/oxidns` | MosDNS、OxiDNS 配置生成与编辑器（保留用户 YAML） |
|  | `internal/subs`、`internal/rules`、`internal/rulefetch`、`internal/provider`、`internal/aggregations` | 订阅节点读取/过滤/链式代理；规则语法与匹配；规则下载；provider 下载的校验与 last-good 回退；聚合 provider |
| 订阅与刷新 | `internal/substore`、`internal/refresh` | Sub-Store 客户端与运行时；订阅/规则源定时刷新与健康条 |
| 路由器 | `internal/routers`（+ `routersplit`、`routerdoc`、`devhttp`） | 路由器记录、期望状态、漂移、发布（RouterOS/iKuai）、地址列表/策略路由规则同步、DNS 守卫、IPv6 |
| Linux 网关 | `internal/gateway`（+ `draft`、`host`、`installation`、`onboarding`、`gwbase`） | 选择性接管的 Mihomo 网关：设置、provider、补丁、向导、安装 |
| 其他 | `setup`/`setupflow`、`clients`、`devicenotes`、`discovery`、`health`、`installer`、`webdav`、`tasks`/`taskstatus`、`notifications`、`importer`、`configexport` | 首次向导、客户端设备、域名发现、节点测速、安装脚本、WebDAV 备份、定时任务页、通知、导入、导出脱敏 |

依赖方向的几个固定点（新代码不要打破）：

- `deploy` import `agents`，`agents` 不 import `deploy`；registry 需要 deploy 的地方走 `agents.Hooks`。
- `api/*` 下面的包不 import 任何 `api/*`。
- `agents` import `notifications`，所以共享的纯文本助手放在叶子包 `agents/diag`。
- `gateway` 与 `routersplit` 共享的三个符号放在 `gateway/gwbase`，避免互相 import。
- `repo` 只 import 少数纯函数包（`convert`、`rules` 的校验/默认值等）；需要领域行为时用 `repo.Hooks` / `RecordHooks`
  （如提交后回调 `ConfigurationCommittedRecords` 由 `webdav` 注册、路由器 job lock、导出脱敏）。
- `lifecycle` import `repo` 只为给每轮循环/任务装一个新的 `repo.Scope`。

### 2.4 组装（`internal/server`）

`server.NewApp()` 依次：

1. 执行 `wirePackages`（钩子、循环、provider 的注册；**顺序有意义**，例如 IPv6 的 Manager-init 钩子最后注册）；
2. 按 `routePackages` 顺序注册路由（对应 `backend/routes/__init__.py` 的 blueprint 顺序），再注册
   `lateRoutePackages`（`api/tasks`、`api/notifications`，它们要在所有任务 provider 注册完之后）；
3. `auth.Install(app)`。

跨包组合（例如 deploy 的网关守卫需要 `gateway/host` 的实时检查，而 host 不能 import deploy）写在 `server.go` / `setup.go` 中。

---

## 3. 关键数据流

### 3.1 配置编辑 → 存储

```
前端 PUT/POST ─► api/<pkg> handler（c.Context() 原样传下）
              ─► owner 入口：ctx = lifecycle.Detach(ctx)；ctx.Err() 检查
              ─► repo.UpdateConfig / UpdateSharedConfigs / Repository.Update / CommitConfig
                   └─ L3 .config.lock → 事务内工作副本 → store.Commit（只写字节变化的行，revision+1）
              ─► 释放 L3 后：ConfigurationCommittedRecords（WebDAV 自动备份排队）、通知、推送提示
```

- 读：`repo.Config(ctx, profileID)` 返回请求内快照；同一请求多次读到同一个 `*model.Config`，写成功后该 profile 的快照失效。
- 编辑器类保存（携带“读取时的状态”）走三路合并（见 4.4），并发编辑别的成员或列表项不会被覆盖。
- 校验失败返回 400 + 中文消息；锁超时 `Timed out waiting for profile repository lock`（`CONFIGFLOW_LOCK_TIMEOUT_SECONDS`，默认 300 s）。

### 3.2 生成 → 发布到 Agent

手动下载/预览：`api/generate` → `configgen.Build` / `RenderConfig`（按 `Engine` 调 `convert.MihomoConfig`、
`convert.SurgeConfig`、`convert.LoonConfig`、`mosdns.Generate`、`oxidns.Generate`）→ 文件或 zip。

推送到 Agent（`api/agents` 的 push-config / generate-config 等，A14）：

```
deploy.PushConfig
  → agents.Manager.ReserveDeployment：记录 "preparing"，L1 键 (agent, deployment)，冲突 409
  → deploy.PrepareContent：生成主配置、provider bundle、规则物化、MosDNS 转换、网关/分流守卫
  → deploy.Build：不可变 Bundle（sha256、source_sha256、config_version）
  → deploy.PublishDirect：GET 能力 → (OxiDNS) 校验 → 记录 "uploading" submitted=true（先落库再 POST）
       → POST /api/deployments（gzip tar）→ 记录上传回执
  → 之后由 GET 轮询推进：verifying … ready → activate / rollback / recover（agents 状态机）
```

状态机、崩溃恢复（例如 `preparing` 且未提交且键未持有 → `Preparation interrupted before upload…`）见
`design/a14-publish.md` 第 3 节。Surge 等无部署包的类型直接 `POST /api/config/update`。

`deploy/contentsync` 在 Agent 列表页后台比较“当前应发布的内容”和 Agent 已应用内容（`applied_content`），
`deploy/pushdiff` 给“待推送”对话框算差异；两者都不做发布。

### 3.3 发布到路由器

`internal/routers`：从配置计算期望状态（规则地址列表、策略路由、NAT、DNS 守卫、DHCP/固定 IP、IPv6）→ 读设备现状 →
计划 → 写设备 → 读回 → 写回执与 repo。设备写在 L2 `router_operation_lock`（每连接）下，写路径用分离 ctx、10 分钟上限；
定时循环 `router-rule-sync`（30 s）做规则同步。详见 `design/r12-release.md`。

### 3.4 心跳与指标

```
Agent POST /api/agents/<id>/heartbeat（公开路径，Agent token 自鉴权）
  → agents.Manager.ObserveHeartbeat：更新内存 runtime state（在线 = 120 s 新鲜度）
       只有 version / config_version 变化才写 repo（在释放 runtime 互斥锁之后）
  → AddMetricsJSON 钩子 → agents/metrics：样本只进内存（每 Agent 24 h）
  → AddressFollowTyped 钩子 → agents/upgrades：Agent 地址变化跟随
  → 回复 domain_discovery_enabled / traffic_history_enabled 等开关
metrics-history-flush 循环（30 s 轮询，间隔可在定时任务页改）把新点追加到文件；关停时还会冲两次（见第 5 节）
```

心跳也会触发 `lifecycle.RequestSeen`，从而启动 `AfterFirstRequest` 类循环（`agent-rule-sync`）。
流量摘要由 `api/agents/traffic.go` 写入 `agents/trafficstore`（按 Agent、按天分桶，只存域名计数）。

### 3.5 规则同步

两条独立链：

- **Agent 规则同步**（`agents/rulesync`，循环 `agent-rule-sync`，首请求后启动，5 s）：每轮轮询 Agent，最多并发 2 个
  worker（`Spawn`）。以最近一次成功部署的回执为基线，比较引用的规则集是否变化：只是内容变化 → 让 Agent 原生接口下载并热重载；
  结构变化 → 走一次受限的发布。状态 `pending/running/succeeded/failed/blocked/idle` 写在 Agent 记录上，状态迁移见
  `design/a16-a18-sync-follow-ha.md` 3.1。
- **路由器规则同步**（`routers`，循环 `router-rule-sync`）：把分流规则里的 IP 同步到 RouterOS 地址列表 / iKuai IP 组。

规则源本身由 `refresh`（循环 `scheduled-refresh`，30 s 检查到期）下载进缓存。

### 3.6 DNS 联动与主备（HA）

- **DNS 联动**（`agents/dnsfollow`，循环 `dns-follow`，提交/Agent 上线时唤醒，进行中 5 s，空闲兜底 10 分钟）：Mihomo Agent 成功发布后，
  按绑定为对应 OxiDNS 重新生成并发布配置。整轮持有 L1 锁；忙时编辑接口返回 409 `DNS 联动正在处理，请稍后重试`。
- **OxiDNS 主备**（`agents/dnsha`，循环 `oxidns-ha-publish`，同上唤醒，进行中 2 s）：成对发布的日志（journal）推进
  `preparing → standby → primary → handoff → keepalived → done`；VIP 由 Agent 侧 keepalived 承载。锁顺序：先 pair，再按排序的成员键（都是 L1 try）。
  HA 成员的发布改走成对发布，`agents` 通过 `Hooks.HADeployment` / `HAActivate` 等钩子转交给 `dnsha`。

---

## 4. 存储

### 4.1 SQLite 表（`internal/store`）

`configflow.sqlite3`，`application_id = 0x43464C57`（"CFLW"），`storage_version = 2`（O17）：

| 表 | 主键 | 内容 |
|---|---|---|
| `meta` | `key` | `storage_version`、`store_id`、`generation`、`root_order` 等 |
| `sections` | `(scope, owner)` | `scope ∈ {root, system, shared, profile}`；profile 的 owner 是配置空间 id；`position` 决定顺序 |
| `fields` | `(scope, owner, field)` | 一个字段一行：`value`（JSON 文本）、`position`、`revision ≥ 1` |
| `agent_state` | `(agent_id, member)` | Agent 运行状态与历史（`model.AgentStateMembers`：部署、已应用内容、版本、规则同步/联动/HA 进度、IPv6 校验、恢复标记），一个成员一行 |

文档在内存中是一个 JSON 形状的整体；`store` 只验证 JSON 语法和行信封，不解释值。`repo` 读出后把 `agent_state` 合并进
`system.agents`（`repo/staterows`，按原字节），提交前再拆出去，所以领域代码只看到一份完整的 `model.Agent`；配置行与状态行
同一事务提交（`design/o17-runtime-state.md`）。`Head` 用来判定缓存是否过期：`Generation`/`CommitID` 只随配置行变化，
`State`/`StateCommitID` 只随状态行变化；`==` 比较两者，只读配置的缓存才可以用 `Head.SameConfig`。提交后 `repo.OnCommit`
按「配置 / 状态」通知订阅者（后台循环的唤醒），WebDAV 等配置观察者只看配置提交。Python 只能读 v1，不能读 v2。

其他 SQLite 文件（流量历史、排行、安装器）各属一个 owner，单连接。

存储版本：新增字段**不**升版本（R1 保留未知成员，缺失按默认读）。只有旧代码可能误读或丢数据时才升 `storage_version`，
迁移在打开时、`.config.lock` 下、任何 Startup 之前执行，先在线备份到 `DATA_DIR/backups/`（`design/o3-concurrency.md` 3.7）。

### 4.2 记录类型与编解码规则（R1–R9，`design/n7-n8.md`）

最常用到的几条：

- **R1** 未知成员原位保留（位置和字节）。**R2** 未改动的成员按读入的原始字节写回（保留 `null`、`1.0`、转义、嵌套顺序）。
- **R3** 原来存在的成员一直存在；原来没有的只在非零时追加。删除成员要显式把指针设为 nil / `Fields.Delete`。
- **R4** 默认值不是零值的（如 `enabled` 缺省为 true）用指针或带访问器的命名类型，不能用裸 `bool`。
- **R5** 存储里类型错误的成员：原样保留，读作零值，每次加载记一次警告（只记路径不记值），不让加载失败。
  `model.String` 接受数字 token，`model.Int` 接受整数值的任意写法和严格的带引号十进制整数；布尔严格。
- **R6** 以用户数据为键的映射用有序 `model.Map[V]`。**R7** 无固定 schema 的子树用 `Dynamic`（→ `*yaml.Node`），只携带的用 `json.RawMessage`；不用 `map[string]any`。
- **R8** 整数成员用 `model.Int`，可能超出 int64 的用 `json.Number`。**R9** 模型类型里不出现 `any`。

### 4.3 请求内快照（`repo.Scope`）

`httpx` 为每个请求装一个 `Scope`，`lifecycle` 为每轮循环/每个任务装一个新的。`repo.Config` 在 Scope 内缓存
`*model.Config`，直到本 Scope 内写成功或 `ResetConfigContext`。没有 Scope 时每次都读库、保存也没有合并基线。
写事务通过 ctx 传递：事务回调拿到的 ctx 读的是工作副本；用它再开写事务会得到 `ErrInUse`
（`不能在配置事务中再次开启写事务`）。**永远传你拿到的那个 ctx。**

### 4.4 三路合并

`repo.MergeJSON(current, baseline, proposed)`（`repo/merge_nodes.go`，设计 n7-n8 2.5）：只把调用方
`baseline → proposed` 的差量应用到当前值。规则：proposed 等于 baseline 保留 current；映射按键递归合并
（current 的顺序，新键追加）；序列按项合并；比较用 `yamlx.Equal`（`1`、`1.0`、`true` 是三个不同值）。
未变化时调用方保留 current 的原字节（R2）。`repo.SaveSharedRecord` 等按段保存（`section_snapshots.go`）都走它。

---

## 5. 并发与生命周期

权威文档：`design/o3-concurrency.md`（接口、2.4 映射表、3.x 状态与关停）和 `design/o3-startup-shutdown.md`。

### 5.1 lifecycle Runtime

- `lifecycle.RegisterStartup(Startup{Name, Run})`：一次性、按注册顺序、监听前同步执行。
- `lifecycle.RegisterPeriodic(Periodic{Name, Start, FirstDelay, Interval, Round, Wake, OnStart})`：间隔从**本轮结束**开始计；
  `Start` 为 `AtBoot` 或 `AfterFirstRequest`；`Wake *Signal` 可打断等待（合并通知）。每轮的 ctx 带新的 repo Scope。
- `lifecycle.Spawn(ctx, name, fn)` / `lifecycle.SpawnerFrom(ctx).Spawn(...)`：超出调用者生命周期的工作**必须**用它
  （受追踪、关停时被等待和取消、panic 记日志）；`SpawnerFrom` 保持发起者所在的 Runtime。不要裸 `go`。
- `lifecycle.Detach(ctx)`：去掉请求的取消/截止时间，保留值和**本代 Runtime 的关停取消**。owner 在入口自己 Detach，handler 不 Detach。
- `lifecycle.RegisterShutdown(ShutdownHook{Name, Phase, Run})`：`PhaseStopSignal` → `PhaseAfterDrain` → `PhaseClose`。
- `lifecycle.ClockFrom(ctx)`：调度等待用的时钟，测试用 `lifecycletest.Clock` 推进。

当前注册的周期循环（`grep -rn RegisterPeriodic internal`）：

| 名称 | 包 | 节奏 |
|---|---|---|
| `scheduled-refresh` | `refresh` | 30 s 后，按最近到期时间（1 s–10 分钟），配置提交唤醒 |
| `router-rule-sync` | `routers` | 30 s 后，按最近到期的路由器（1 s–10 分钟），配置提交唤醒 |
| `domain-discovery-auto` | `discovery` | 120 s 后，600 s |
| `oxidns-ranking-history` | `agents/monitor` | 60 s |
| `metrics-history-flush` | `agents/metrics` | 30 s 轮询 + Wake |
| `sub-store-idle` | `substore` | 5 s（仅内置 Sub-Store 存在时） |
| `dns-follow` | `agents/dnsfollow` | `agents.Waker`：提交与 Agent 上线唤醒，进行中 5 s，空闲 10 分钟 |
| `oxidns-ha-publish` | `agents/dnsha` | 同上，进行中 2 s |
| `ipv6-verification-scheduler` | `agents/ipv6` | 提交唤醒，校验进行中 5 s，空闲 10 分钟；验证任务经 Spawn 限并发 |
| `agent-rule-sync` | `agents/rulesync` | 首请求后；提交、Agent 上线、规则源刷新完成唤醒，进行中 5 s，空闲 1 分钟 |

WebDAV 自动备份 worker 由 Startup 用 Spawn 启动（1 s 去抖）。

### 5.2 锁层级

只能按此顺序获取：**L1 任务（只 try）→ L2 设备/会话 → L3 仓库 → L4 叶子存储 → L5 内存。**

1. L1 只 `TryLock`：忙就返回 owner 的 409 或跳过本轮，从不等待，因此不可能参与死锁。多个 L1 按固定顺序取。
2. L2 会等待；L2 内只能再取 L3 及以下。典型发布者：L1 任务 → L2 操作 → 设备 I/O → L3 写回读回结果。
3. **L3 下不做网络、设备、子进程 I/O**；提交后回调都在释放后执行。
4. L4（单文件存储的 `sync.Mutex`）内不取其他锁、不调用外部；L5 内存锁不跨 I/O、回调、channel 发送或其他锁。

`lockx` 在所有构建里运行时检查 L1–L3 顺序，测试断言 `lockx.Violations() == 0`。文件锁（flock）只允许在
`instance` 和 `repo`。这些规则由 `internal/lifecycle/gate_test.go::TestConcurrencyFacadeBoundary` 静态检查
（裸 `go`、Flock、`time.Sleep`/`AfterFunc` 等只能出现在显式豁免的 file:function）。

### 5.3 关停顺序（SIGTERM / SIGINT）

`CONFIGFLOW_SHUTDOWN_GRACE_SECONDS`，默认 8 s（容器停止时限 10 s 内完成）：

| 时刻 | 动作 |
|---|---|
| 0 | 关闭 `Stopping`，停止所有循环计时器；执行 `PhaseStopSignal` 钩子（监控历史第一次保存） |
| 0 | 并行 `http.Server.Shutdown`（API 和 Web），在途请求继续 |
| ≤ grace−2 s | 等在途 handler、本轮循环、Spawn 的任务结束 |
| grace−2 s | 取消所有任务/循环 ctx（包括 Detach 出来的），再等至多 0.5 s |
| 之后 | `PhaseAfterDrain`（监控历史第二次保存、关闭 WebDAV worker）→ `PhaseClose`（关连接、刷日志、释放实例锁） |

第一次保存期间再收到信号会被忽略；保存完再收到立即强制退出。空闲时提前结束。
Go 不会添加 Python 没有的崩溃恢复动作；各种“进行中”状态重启后的处理见 o3 3.5。

---

## 6. 测试与门禁体系

**所有测试、构建、`go vet` 都在测试服务器（.32 / .64）上用 `rt` 跑，绝不在 Mac 上跑。**

### 6.1 层次

| 层 | 位置 / 命令 | 说明 |
|---|---|---|
| 单元测试 | 各包 `*_test.go`；`rt -- bash -lc 'cd server && go test -race ./internal/<pkg>/...'` | 表驱动、`t.TempDir()`、不共享全局可变状态；生命周期用 `lifecycletest.Clock` |
| 路由测试 | `internal/httpx/httpxtest`：`httpxtest.New(t, Options{Register: …, Auth: true})` | 走完整请求管线（鉴权、脱敏、profile 选择），临时数据目录 |
| 设备/Agent 假体 | `agents/agentfake`、`routers/routerfake` | 只在回环上；至少一个测试比较原始请求字节和头（HR 常设检查项） |
| 模糊测试 | `Fuzz*` | 新增解析/编码路径要带 fuzz target；门禁里每个跑 60 s |
| Python 参考 golden | `internal/testgolden`（`tools/pyref`） | 纯函数的 Python 参考结果录成 golden，普通 `go test` 不启动 Python；源码或依赖变化报 `Python 参考已变化，请重新录制` |
| Flask↔Go 差分 | `internal/**/difftest`，`CONFIGFLOW_DIFF=1` | 真起 Flask 与 Go，比较状态码、JSON 结构、中文消息、写入的存储 |
| 黑盒工具 | `tools/configdiff`、`tools/routerdiff`、`tools/faults` | 公开进程级差分（不 import Go 业务包）、路由器网络命名空间、故障注入（SIGKILL 等） |
| 生产快照差分 | `scripts/proddiff.sh`（`tools/proddiff`） | 只读测试服务器上的离线快照，要求 `new=0 stale=0`；快照内容不得进代码/测试/报告 |
| 遗留门禁 | `internal/lifecycle/legacy_gate_test.go::TestLegacyFacadeBoundary` | 禁止 import 已删除的 `jsonx`/`pystr`/`pyyaml` 及退役符号，无白名单 |

构建标签：`configflow_difftest`（故障探针、事件、测试清单）、`configflow_proddiff`（proddiff 模式）。生产二进制不带标签。

### 6.2 `scripts/gate.sh`（详见 `scripts/gate.md`）

- **core**：gofmt、全仓 vet、两种 lifecycle 构建模式、全仓 `-race`、给定的 fuzz 目标（各 60 s，最多 8 个并行）。
- **compare**：观察器自检 + proddiff、所有 `/difftest` 包（非 race）、自动发现的 synthetic live 测试（引用
  `CONFIGFLOW_DIFF*`/`CONFIGFLOW_PY` 等环境的普通测试，必须全部 run 且 pass，不许 skip）。
- 合并前协调者还跑一次 **preflight**：两套标签的 vet + 全量非 race `go test ./...`。

分工：普通任务的作者只跑自己改动的包及直接调用方（`gofmt`、`go vet ./...`、`go test -race`、相关 difftest、新 fuzz 60 s），
**不跑 gate.sh**；协调者按批次跑整套门禁（`WORKFLOW.md`「Merge batching」）。只改文档/测试工具的提交可以免整套门禁。

---

## 7. 操作指南

### 7.1 新增一个路由

1. 选定 owner 包（业务逻辑放这里，不放在 `api/*`）。入口签名 `func X(ctx context.Context, …) (…, error)`；
   会写状态、取锁、起任务的入口第一行 `ctx = lifecycle.Detach(ctx)`，并在每个提交点前检查 `ctx.Err()`。
2. 在 `internal/api/<blueprint>` 加 handler：`func h(c *httpx.Ctx) (*httpx.Response, error)`，把 `c.Context()` 原样传给 owner；
   在包的 `Register` 里 `r.POST("/api/…/<id>", "<blueprint>.<endpoint>", h)`。新 blueprint 包要加进
   `internal/server/server.go` 的 `routePackages`（依赖全部 provider 的放 `lateRoutePackages`）。
3. 中文消息写进该包 `messages.go` 的常量；测试引用同一个常量。
4. 鉴权：默认受全局门禁保护。确实需要公开（Agent 自鉴权、provider 下载）才改 `internal/auth` 的公开表，并加测试。
5. 请求体：用 `c.Bind` / `jsondecode`；非对象、错误类型、缺字段返回 400 + 明确消息。L1 锁和校验的先后顺序要写清楚（o3 2.5）。
6. 更新 `parity/routes.tsv`；前端有入口变更时同步 `doc/`（根 AGENTS.md）。
7. 测试：`httpxtest` 覆盖成功路径 + 每类错误一个代表（坏 JSON、非对象、错类型、缺字段、冲突 409、取消）；
   写入类加“Runtime 关停后不落盘、请求断开后仍完成”的测试（HR 第四条常设检查项）。

### 7.2 新增一个生成器（新配置格式）

1. 生成函数放在对应领域包（客户端格式放 `internal/convert`，DNS 类有自己的包），输入是 typed `*model.Config`，输出文本；
   YAML 用 `yamlx` 构建和 `Marshal`，不手拼字符串，不用 `map[string]any`。
2. 在 `configgen.Engine` 加常量、`Valid` 和 `RenderConfig` 的分支；需要推送到 Agent 时再接入 `deploy` 的准备流程
   （`PrepareContent`/`Build`）和 Agent 能力判断。
3. 输出若引用规则/provider URL，统一用传入的 `baseURL`；token 不进日志（`weburl` 脱敏函数）。
4. 测试：代表性语料的结构化比较（解析后 `yamlx.Equal` / `testjson.Equal`，不比字节，除非消费方逐字节读取）、
   R5 坏数据输入、空配置、fuzz 一个解析/渲染入口；进入部署的还要用 `agentfake` 走一遍上传。
   新格式没有 Python 参考，不需要 difftest，但要写明期望来源。

### 7.3 新增一个后台任务

1. 周期性：在 owner 包的 wire 函数里 `lifecycle.RegisterPeriodic(lifecycle.Periodic{Name: "…", Start: lifecycle.AtBoot,
   FirstDelay: …, Interval: …, Round: …})`，并把 wire 函数加进 `server.wirePackages`。一次性启动工作用 `RegisterStartup`。
   被事件触发的一次性工作用 `lifecycle.SpawnerFrom(ctx).Spawn(ctx, name, fn)`。
2. 定时任务页要显示的，在 `internal/tasks` 加行，结果写 `taskstatus`；“立即运行”和循环共享同一把 L1 键，忙则 409。
3. 选锁级别（o3 2.4 表）；L3 下不做 I/O；不要新建 flock。
4. 空闲时别空转：依赖存储的循环用 `agents.Waker`（`repo.OnCommit` + Agent 上线唤醒，进行中才短间隔），按时间到期的循环让
   `Interval` 返回离下次到期的时间并配 `Wake`（生产空闲 CPU 曾因 5 s 循环重算占 15%）。
5. 关停：Round 必须响应 ctx；需要关停时保存的状态注册 `ShutdownHook`。
6. 测试：`lifecycletest.Clock` 驱动轮次；忙锁跳过；panic 被记录不扩散；真实 `Runtime.Start/Shutdown` 下关停后不写；
   `lockx.Violations() == 0`；`TestConcurrencyFacadeBoundary` 通过（不要为绕过它加宽豁免）。

---

## 8. 常见坑

- **R5 存储类型**：存储里的值可能是 Python 写的任意类型（`"60"`、`60.0`、`true`）。读不要失败，按模型类型的规则读，
  原字节写回。不要用 `bool` 表示缺省为 true 的开关（R4）；不要把 R5 的零值当作“用户设置了零/删除”的证据，指针读成 nil 也不是删除命令。
- **开关真值**：缺失 → 默认值；显式 `false`/`null`/`0`/`""`/`[]`/`{}` → 关闭；其他 → 开启。用共享 helper，不要各写一份。
- **不仿真 Python**：不移植 CPython/PyYAML/requests 的行为，不造“Python 异常类/文本”，不用 `any` 参数模仿 Python 调用形状，
  不复制共享 helper 的私有版本。Python 的 bug 在 Go 里修掉，并记到 `parity/behaviour-changes.md`。
- **`encoding/json` 的行为**：
  - 默认把 `<>&` 转义为 `<` 等，写存储/响应时用 `SetEscapeHTML(false)`（`jsonresp.Encode`、`model.Marshal` 已处理）；
  - 总是把 U+2028/U+2029 转义；需要保留原文（按字节计长度的记录）用 `jsonresp.EncodeUTF8`；
  - nil slice 编码成 `null`、nil map 成 `null`；对外响应要 `[]`/`{}` 时初始化为空值；`model.Marshal` 把 nil slice 写成 `[]`；
  - 测试里和 Python 字节比较时**不要**用 `encoding/json` 重新编码期望值（曾因此误判），用 `testjson` 做结构比较。
  - `json.RawMessage` / `MarshalJSON` 输出会被压缩，要保留原字节走 `model` 的写出器（R1/R2）。
- **Detach 之后要检查 ctx**：Detach 保留了关停取消，但如果快路径从不看 ctx，关停后仍会提交。每个写 owner 在 Detach 后、
  每个提交点（repo 事务、文件替换、设备写）前检查 `ctx.Err()`；没有 ctx 的叶子存储要加 ctx。
- **handler 不 Detach，owner Detach**：handler 传 `c.Context()`；只有纯读和只读 handler 等 L2 锁时用请求 ctx。外部 I/O 一律自带超时。
- **不要嵌套写事务**：在 `repo` 事务回调里调用会开写事务的函数会得到 `ErrInUse`；需要组合时改成同一事务内的纯函数。
- **YAML/JSON 走 facade**：YAML 用 `yamlx`（及 `yamlbridge`），JSON 存储用 `model`，响应用 `jsonresp`/`httpx.JSON`；不直接 `yaml.Marshal`。
- **中文消息逐字节一致**：前端和 Agent 会匹配这些文本。改动用户可见消息要同时改常量、测试和前端（如需要）。
- **不泄露**：日志、错误、响应不出现密码、token、订阅链接；URL 用 `weburl` 的脱敏渲染，凭据替换用 `internal/redact`（只隐藏密码/token，不隐藏用户名）。
- **测试数据只用合成值**：`example.com`、`192.0.2.0/24`、`198.51.100.0/24`、`2001:db8::/32`；不写真实主机名/IP/token/机场域名。
- **测试替身不能改掉被测参数**（例如假客户端强制短超时，掩盖了缺失的写超时）。
- **负载下的时间/分配断言**：`-race` 与大量 fuzz 并行时分配和时序会抖动，断言要取多次最小值或在 race 下放宽；单机出现的时序失败先在另一台服务器复测再判定。
- **不要在 Mac 上跑测试或构建**；`rt` 的 `--no-sync` 复用上次主机，需要多条命令落在同一台时用 `RT_HOST` 固定。
