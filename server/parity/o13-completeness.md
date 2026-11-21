# O13 功能完整性终审（Go 后端 vs Python 后端）

- 审计者：fresh Opus（未移植过任何代码），2026-11-21/22
- 树：分支 `go-audit-o13`，基于 `go` = `9a5bebf0a`（N8-11 完成）
- 方法：不采用各移植报告的结论，直接对照源码与运行结果。只读；测试服务器 `.64` 上运行，临时脚本不入库。
  1. Flask `app.url_map`（导入 `backend.app`，临时 DATA_DIR）重新枚举全部 method+path；Go 用临时测试调用
     `server.NewApp()` 导出 `Router.Routes()`；三方（Flask / routes.tsv / Go）逐行比对。
  2. 新数据目录下同时启动 Flask（`python -m backend.app`）和 Go（`cmd/configflow`），
     - 对全部 156 个 GET 路由（`<profile_id>`=default，其他参数用不存在的合成 ID）比较状态码与 JSON 字段结构（键路径+类型）；
     - 同一组 19 个写操作（服务器域名、规则拉取代理、订阅聚合、WebDAV 设置、配置令牌增删、任务间隔、Sub-Store 模式、
       自定义 Mihomo/Surge/Loon、节点、规则、代理组、路由器、配置空间、Web 端口）比较状态码、响应键，
       再比较 `configflow.sqlite3` 的 `fields`/`meta` 解析后内容和 `/api/config/export`；
     - Flask 导出导入 Go、Go 导出导入 Flask，再比较两边导出；
     - MCP `tools/list` 比较工具名、描述、inputSchema。
  3. 前端：从 `frontend/src`（排除测试）提取全部 axios 调用（方法+URL，展开 `profilePath`/`mihomoPath`/`hostPath` 等辅助函数）
     和 URL 字面量，与 Go 路由模式匹配；提取前端读写的全部 snake_case 字段名，检查 Go 非测试源码是否出现。
  4. 设置/存储：Python 非测试源码里全部 snake_case 字符串字面量（1971 个）与 Go 非测试源码交叉，逐个判定未出现者。
  5. 环境变量：Python/docker 中读取的变量名与 Go 交叉；镜像、入口、静态资源对照 `docker/*`。

合成数据仅用 192.0.2.0/24、198.51.100.0/24、127.0.0.1、example.com；未连接任何生产主机。

## 汇总

| 类别 | 核对项数 | ok | 缺失 | 部分 | 有意不做 |
|---|---|---|---|---|---|
| 1 路由 | 381（Flask url_map） | 380 | 0 | 0 | 1（Flask 默认 `/static`） |
| 2 后台任务与启动钩子 | 21 + 7 | 26 | 0 | 0 | 2（P18 排队监视、测速托管一次性清理） |
| 3 前端调用 | 299 调用 + 6 URL 字面量 + 652 字段名 | 全部 | 0 | 0 | 0 |
| 4 设置键与存储 | schema、19 写操作、导出/导入、WebDAV、80 个仅 Python 字面量 | 全部 | 0 | 0 | 遗留迁移（已批准） |
| 5 其他入口 | 环境变量、CLI、静态/SPA、Agent 下载、MCP、镜像 | 除镜像外全部 | 1（#40 镜像/entrypoint，含 auto-update） | 0 | Python 运行时调优（已批准） |

**产品代码中未发现缺失或占位的功能。** 唯一缺口是已登记的 packages #40（发布镜像/entrypoint），切换前必须完成。

## 1. 路由

Flask url_map 枚举 381 个 method+path；`routes.tsv` 380 行与之逐行一致，唯一差异是 Flask 自动注册的
`GET /static/<path:filename>`。Go `NewApp()` 注册 380 个 method+path，与 TSV 完全一致（0 缺、0 多）。

| 项 | Python 位置 | Go 位置 | 状态 |
|---|---|---|---|
| 380 个 API/MCP method+path | `backend/routes/**`、`backend/mcp_server/server.py` | `internal/server/server.go` 的 routePackages → `internal/api/*`、`internal/mcp` | ok |
| TSV 中 7 行 `todo`：`/api/custom-config/linux-gateway`（GET/POST、`/preview`、`/restore`、`/restore/preview`、`/rules/<path>`、`/source/<path>`） | `routes/linux_gateway.py` | `internal/api/linuxgateway/routes.go:23`（与已 done 的 profile 版本同一循环、同一 handler；有 `difftest/`） | ok — **TSV 已改为 done**；实时差分 GET 一致 |
| `GET /static/<path:filename>` | Flask 默认静态目录 `backend/static`（不存在，恒 404） | 前端静态由 `httpx.Edge` 提供 | 有意不做：Python 中无内容可服务；无前端调用 |
| `GET /api/config/<profile_id>/loon/<filename>` 的 endpoint 名 | `config.get_profile_loon_config` | `config.get_profile_loon_named_config` | ok：endpoint 只用于 blueprint 前缀（`config`），无可观察差异 |
| 占位/固定 503 | — | grep `internal/api` 全部 503：tasks/notifications「未接线 slot」（生产全部接线，实时运行返回 200）、rule library 创建结果不确定、节点测速、Web 端口不可用、安装链接上限、安装脚本不可用、onboarding setup 不可用、linux-gateway provider 出错 | ok：均为有条件分支，与 Python 对应；artifact 三条已是真实实现（无 503 占位） |

实时差分（新数据目录）：156 个 GET 中 0 个状态码或字段结构差异（200×89、400×9、401×3、404×46、405×2、409×4、500×3）。

## 2. 后台任务与启动钩子

Python 周期均为“轮后 sleep”；Go 用 `lifecycle.RegisterPeriodic`（FirstDelay + Interval）。数值逐个对照 Python 源码常量/`sleep` 参数核实。
Python 的 P1–P5 只在 `-m backend.app` 下启动；生产镜像经 `startup.py` `execve(python -m backend.app)`，所以生产中全部运行；Go 始终启动，等价。

| # | 项 | Python 位置 | Go 位置 | 周期 Python / Go | 状态 |
|---|---|---|---|---|---|
| P1 | scheduled-refresh | `utils/scheduled_refresh.py:319` | `internal/refresh/rows.go:159` | 首 30 s，每 30 s / 同 | ok |
| P2 | router-rule-sync | `routers/rule_sync.py:448` | `internal/routers/rule_sync_tasks.go:38` | 首 30 s，每 30 s / 同 | ok |
| P3 | domain-discovery-auto | `utils/domain_discovery_service.py:1043` | `internal/discovery/auto.go:215` | 首 120 s，每 600 s / 同 | ok |
| P4 | oxidns-ranking-history | `agents/oxidns_ranking_history.py:374` | `internal/agents/monitor/rankings.go:815` | 首 60 s，每 60 s / 同 | ok |
| P5 | metrics-history-flush（+启动 load_history、SIGTERM 保存） | `agents/metrics_flush.py:186` | `internal/agents/metrics/flush.go:383`、`wire.go:49,124-126` | tick 30 s / 同，含 Wake、两段关停保存 | ok |
| P6 | sub-store-idle | `utils/sub_store_runtime.py:214` | `internal/substore/runtime.go:619` | 0，每 5 s / 同 | ok |
| P7 | dns-follow | `agents/dns_follow.py:711`（init_agent_manager） | `internal/agents/dnsfollow/wire.go:39,53` | 0，每 5 s / 同（OnManagerInit） | ok |
| P8 | oxidns-ha-publish | `agents/oxidns_ha_publish.py:655` | `internal/agents/dnsha/hooks.go:39-41` | 0，每 2 s / 同 | ok |
| P9 | ipv6-verification-scheduler | `agents/ipv6_verification.py:595` | `internal/agents/ipv6/wire.go:185-197` | 0，每 5 s / 同 | ok |
| P10 | agent-rule-sync-loop（首个通过认证的请求后启动） | `app.py:112`、`agents/rule_sync.py:605` | `internal/agents/rulesync/wire.go:43`（`AfterFirstRequest`） | 0，每 5 s / 同 | ok |
| P11 | webdav-auto-backup | `utils/webdav_backup.py:186` | `internal/webdav/adapter.go:123,131` | 提交后 1 s 去抖 / 同 | ok |
| P12 | agent-address 跟随 | `agents/address_follow.py:64` | `internal/agents/upgrades/address.go:30` | 节流 120 s / 同 | ok |
| P13 | gateway-onboarding-<id> | `agents/gateway_installation.py:723` | `internal/gateway/installation/publish.go:97` | 按会话 | ok |
| P14 | router-rule-sync-now | `routers/rule_sync.py:442` | `internal/routers/rule_sync.go:587` | 手动 | ok |
| P15 | task-<key>（立即运行） | `routes/tasks.py:245`、`scheduled_refresh.py:312` | `internal/tasks/tasks.go`、`internal/refresh/scheduled.go`（忙碌消息相同） | 手动 | ok |
| P16 | domain-<kind>-<id> | `domain_discovery_service.py:908` | `internal/discovery/jobs.go` | 手动 | ok |
| P17 | sub-store 更新 | `utils/dependencies.py:349` | `internal/substore/dependencies.go`（`已有更新任务在进行中`） | 手动 | ok |
| P18 | configflow-queue-watch | `http_queue.py:35` | — | — | 有意不做（packages.md「Not ported」：http_queue.py；net/http 无固定线程池） |
| P19 | matcher 清理定时器 | `utils/domain_matcher_cache.py:180` | `internal/rules/matchercache.go:31-33` | 60 s / 空闲 1800 s，同 | ok |
| P20 | setup 检查带超时加载 | `common/setup_completion.py:89` | `internal/setup/completion.go` | 每次调用 | ok |
| P21 | 单次调用内扇出池（provider_response、content_sync、ros_clients、pending、client_discovery、mosdns_archive、node_latency、aggregations、deployment_bundle） | 见 o3 表 | `internal/provider`、`deploy/contentsync`、`routers/clients_ros.go:283`、`routers/pending.go:230`、`clients/discovery.go`、`mosdns/archive.go` 等 | — | ok |
| S1 | load_config（含 rule-proxy 令牌轮换） | `app.py:95` | `cmd/configflow/service.go:57` | 启动 | ok |
| S2 | Sub-Store monitor 启动 | `app.py:133` | `substore.RegisterStartup` | 启动 | ok |
| S3 | prune_orphan_caches | `app.py:137` | `internal/substore/wire.go:77` | 启动 | ok |
| S4 | init_agent_manager（含 runtime.sqlite3 一次导入） | `app.py:143` | `service.go:62`、`internal/agents/manager.go:132` | 启动 | ok |
| S5 | node_test_dns_cleanup.migrate | `app.py:147` | — | — | 有意不做（packages.md「Not ported」，#569 一次性清理） |
| S6 | metrics load_history + 信号处理 | `app.py:196-198` | `metrics/wire.go:49,125-126`、`service.go` 二次信号逻辑 | 启动/关停 | ok |
| S7 | 无认证横幅 | `common/auth.py` | `cmd/configflow/main.go:60` | 启动 | ok |

## 3. 前端调用

| 项 | Python 位置 | Go 位置 | 状态 |
|---|---|---|---|
| 299 个唯一 方法+URL（axios `api.*`，含辅助函数展开；`DomainDiscovery.vue` 的 `read(url)` 与 Mihomo 面板缓存资源手工展开） | Flask url_map | Go 路由表 | ok：0 缺（脚本未匹配的 10 条是模板字符串提取误差，逐条人工核对均存在：gateway-onboarding/preview、gateway-installations/<id>/{network,publish}、setup/<id>/<operation>、oxidns/cache/flush） |
| 6 个 URL 字面量（订阅链接 `/api/config/<id>/{mihomo,surge,loon/<name>.lcf,mosdns,oxidns}`、`/api/profiles/<id>/rule-library/content/<rule>`、`/api/agents/gateway-network-probe.sh`、`/api/auth/login`、`/api/agents/<id>/oxidns-ha`） | — | 均已注册 | ok |
| `/frontend-version.json`（`lib/pageRecovery.ts`） | nginx.conf | `internal/httpx/edge.go:83` | ok |
| 前端读写的 652 个 snake_case 字段名 | — | Go 非测试源码 | ok：0 缺（唯一命中 `gateway_forwarding_entry` 是注释里的 Python 函数名） |
| 响应字段结构 | — | 实时差分 156 GET + 19 写操作响应 | ok：0 差异 |

前端读取但 Go 不返回的字段：**无**（在静态字段名核对和实时差分覆盖范围内）。
范围说明：实时差分是新数据目录 + 合成写入，带真实设备/Agent 的响应体字段由各模块已有的 Flask↔Go difftest 覆盖，本审计未重跑。

## 4. 设置键与存储

| 项 | Python 位置 | Go 位置 | 状态 |
|---|---|---|---|
| SQLite schema（`sections`/`fields`/`meta`） | `common/config_store.py` | `internal/store` | ok：两边新库 schema 文本完全一致 |
| system/shared/profile 字段集合（SHARED_FIELDS、PROFILE_FIELDS、system_config/backup/agents） | `common/config_repository.py:199-201,389` | `internal/repo` | ok：新库及 19 个写操作后 `fields` 表解析后一致（仅随机 profile ID、令牌、时间戳不同） |
| 系统设置写入：server_domain、rule_fetch_proxy、subscription_aggregation_enabled、WebDAV（url/用户名/密码/路径/auto_backup/keep_latest/keep_daily）、config_token 增删、任务间隔、Sub-Store 模式、web 端口 | `routes/settings.py`、`routes/tasks.py` | `internal/api/settings`、`internal/api/tasks` | ok：状态码、响应键、落库一致 |
| 全局导出 `/api/config/export` | `config_repository.export_all` | `repo.ExportAllJSON` | ok：结构一致 |
| 全局导入（交叉：Flask 导出→Go 导入，Go 导出→Flask 导入） | `routes/config.py:253` | `internal/api/config` | ok：两边 `{"success": true}`，再次导出结构一致 |
| WebDAV 备份内容 | `utils/webdav_backup.py:208`（export_all JSON，`config_backup_<时间>_<微秒>.json`） | `internal/webdav/adapter.go:65`、`backup.go:97` | ok：同一导出 JSON、同名格式、同保留策略 |
| `meta` 表 `legacy_config`/`migration` 的内部形状 | `config_repository.py` | `internal/repo` | ok（内部元数据，只有当前后端读；Python 不再运行） |
| 仅 Python 出现的 80 个字面量 | 见下 | — | ok：全部为内部变量/缓存/kwargs/日志格式/第三方库参数，无对外字段或存储键缺失 |
| 遗留存储迁移（config.json、损坏重建 `corrupt-rebuild`/`recovered_from`、`last_legacy_import`、LEGACY_DIRS） | `config_repository.py:650-740`、`metrics_history` | Go 拒绝仅有遗留文件的数据目录 | 有意不做（packages.md「Not ported」） |

80 个仅 Python 字面量的分类（逐个查看上下文）：
- 内部批处理/缓存状态：`added_since_read`、`groups_stale`、`groups_written`、`address_list_*`、`nat_list_*`、`batch_kinds`、`batch_restorable`、`failed_at`、`cache_library`、`cache_updates`、`rule_cache`、`content_loader`、`*_manifest` 变量名等；
- Flask `g`/上下文名：`configflow_profile_*`、`configflow_resolved_profiles`、`mcp_profile_id`、`client_discovery_cache`、`generated_subscription_*`、`rule_duplicate_scan`、`configflow_local_http_depth`；
- 第三方库参数：`webdav_hostname`/`webdav_login`/`webdav_timeout`（webdavclient3）、`problem_mark`、`yaml_set_anchor`、`sqlite_errorcode`、`ipv4_mapped`；
- 只出现在 YAML 注释或摘要输入中、Go 用其他名字实现同样行为：`proxy_count`/`total_nodes`（订阅 provider 注释，Go `api/subscriptions/providers.go:63-65` 同文本）、`router_fingerprints`（IPv6 check_token 摘要，Go `agents/ipv6/service.go:265-273` 同样包含路由指纹）、`nat_decision`（Go `NATDecision`）、`lease_time`（Go `clients_ros.go` 读 `lease-time`）；
- `hours_24`：流量统计 period，Python 中等同 `total`，Go 未知 period 同样回落到全部数据 —— ok；
- 遗留迁移：`legacy-*`、`split-v2`、`config-v5`、`corrupt*`、`recovered_from`、`last_legacy_import` —— 有意不做。

## 5. 其他入口

| 项 | Python 位置 | Go 位置 | 状态 |
|---|---|---|---|
| 环境变量 ADMIN_USERNAME/ADMIN_PASSWORD/JWT_SECRET_KEY、DATA_DIR、LOG_LEVEL、LOG_FILE、CONFIGFLOW_BACKEND_HOST/PORT、CONFIGFLOW_LOCK_TIMEOUT_SECONDS、AGENTS_STATIC_DIR、SUB_STORE_URL/BUILTIN_DIR/BACKEND_API_PORT、代理变量 | `common/auth.py`、`app.py`、`http_server.py`、`agents/upgrades.py`、`utils/sub_store_*` | `internal/auth`、`internal/repo`、`internal/logx`、`internal/server/server.go:216`、`agents/upgrades/artifacts.go:41`、`internal/substore`、`http.ProxyFromEnvironment` | ok |
| CONFIGFLOW_HTTP_THREADS、CONFIGFLOW_MEMORY_TUNING、MALLOC_ARENA_MAX、MALLOC_TRIM_THRESHOLD_、WERKZEUG_RUN_MAIN | `http_server.py`、`startup.py`、`app.py` | Go 用 GOMEMLIMIT/CONFIGFLOW_MEMORY_LIMIT_RATIO（`main.go:71`） | 有意不做（packages.md「Not ported」：startup.py/http_server.py/http_queue.py） |
| CLI | `python -m backend.app`（无参数） | `configflow [-frontend DIR]`、`CONFIGFLOW_FRONTEND_DIR`、`CONFIGFLOW_SHUTDOWN_GRACE_SECONDS` | ok（Go 新增项仅为替代 nginx 与关停宽限） |
| 启动横幅、无认证提示 | `app.py:161-178`、auth | `service.go:printStartupBanner`、`main.go:60` | ok（Python `build_time` 从不存在，两边都打印 unknown） |
| 静态资源与 SPA（nginx.conf 全部 location、gzip_static、缓存头、`/health`、`/_configflow/listener-health`、上传上限） | `docker/nginx.conf` | `internal/httpx/edge.go:23-42` | ok |
| Web 端口监听切换（`web_listener.py prepare` + 设置页） | `utils/web_listener.py`、`configflow-start.sh` | `internal/weblistener`、`service.go` | ok |
| Agent 二进制下载 / 升级制品 | `agents/upgrades.py:30`（AGENTS_STATIC_DIR，默认 `/opt/configflow/static/agents`，回落源码目录） | `agents/upgrades/artifacts.go:32-41`、`api/agents/downloads.go:38` | ok |
| 安装/网关/HA 脚本 | `backend/agents/scripts/*`、`*_install_script.py` | `internal/installer/scripts/*`、`constants_gen.go` | ok（脚本内变量 CONFIGFLOW_LOG_CURSOR 等均在 Go 嵌入脚本中） |
| 独立测速组件 node-probe | `/opt/configflow/node-probe/` | `internal/health/runner.go:153-171` | ok |
| MCP 工具列表 | `mcp_server/tools.py` | `internal/mcp` | ok：53/53，名称、描述、inputSchema 完全一致（实时 `tools/list`） |
| 发布镜像 / entrypoint（Dockerfile 仍装 Python+nginx+supervisord 并运行 Flask；supervisord `[program:flask]`；`configflow-start.sh`） | `docker/Dockerfile`、`supervisord.conf`、`configflow-start.sh` | — | **缺失**（已登记 packages #40 todo） |
| 自动更新镜像（autoupdate tag：CONFIGFLOW_AUTO_UPDATE/UPDATE_BRANCH/UPDATE_REPO/GOPROXY，拉源码重建前端与 Agent） | `docker/auto_update.py`、Dockerfile stage 6 | — | **缺失**（属于 #40，需维护者决定） |

## 缺失/部分逐条

1. **#40 发布镜像/entrypoint（缺失，已登记）**
   影响：没有它无法切换——现有镜像仍运行 Flask。需要：Go 多架构构建、去掉 Python 与 nginx；supervisord 需保留（Go 的 substore runtime 与 Python 一样用
   `supervisorctl -c … start|stop sub-store` 按需启停，见 `internal/substore/runtime.go:308`），`[program:flask]` 换成 Go server、
   静态资源（前端 dist + gz、Agent 制品、node-probe）放到 Go 读取的路径、tzdata/CA、`DATA_DIR=/data`、信号/关停。规模：M（与 O14 一起）。
2. **auto-update 镜像（缺失，#40 子项）**：`docker/auto_update.py` 是 Python 脚本，在容器启动时拉分支源码、按需重建前端和 Agent、
   记录镜像树指纹。Go 镜像里若保留，需要改为重建 Go server（并带 Go 工具链），否则应在发布说明中宣布下线 autoupdate tag。规模：S–M，取决于决定。

## 需要维护者决定的项

1. autoupdate 镜像在 Go 版本中保留（改为拉源码重建 Go server + 前端 + Agent）还是下线？
2. （可选，非完整性问题）`GET /api/agents/<id>/logs`、`/logs/sources`、`/config/logging` 等对不存在的 Agent 返回
   500「Agent not found」（Python 原样，Go 已一致保留）。按“Python bug 在 Go 中修正”的规则可改为 404；前端未依赖 500。规模：S。

## 账目修正

- `parity/routes.tsv`：7 行 `/api/custom-config/linux-gateway*` 由 `todo`（空 package）改为 `internal/api/linuxgateway` / `done`。现 380 行全部 done，且全部 go package 目录存在。
- TSV 自身完整性：与 Flask url_map 逐行一致（仅缺 Flask 默认 `/static`，见第 1 节，有意不列）。
