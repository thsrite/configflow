# Go 生产快照结构对比

从仓库根目录运行（所有构建和测试均在测试服务器）：

```bash
rt -- server/scripts/proddiff.sh
rt -- server/scripts/proddiff.sh ~/cf-prod-snapshot/20261116/data
```

默认使用第二条命令的快照目录，Python 为 `~/cf-test/.venv/bin/python`。
可通过 `CONFIGFLOW_DIFF_PYTHON` 指定相同依赖环境的解释器。
退出码：0 表示没有新增差异和覆盖回退；1 表示发现回归；2 表示运行环境或框架失败。
基线差异消失时打印 `stale` 并令检查失败，必须删除过期条目后重跑；门禁要求 `new=0 stale=0`。

`rt -- server/scripts/proddiff.sh --a13-proof` 在同样的只读、断网命名空间中，使用真实 Python
准备函数产出的候选 bundle 验证 A13 的 24 条 A14 延期差异。先逐条证明原 20 项与新增 4 项
（list 三条 content_sync.checked_at、detail 一条）不接线时都能复现，再向
Go Checker 注入经过归档完整性校验的真实候选，比较同一批路由的完整响应。独立生成的时间和版本号
单独检查格式、一一对应和缓存稳定性；标准 proddiff 的比较规则保持不变。真实数据只留在服务器的
私有临时目录，不提交语料，不输出响应值。

仅在 Go `configflow_proddiff` 构建和 Flask 测试启动器中，Agent list/detail 的 content_sync
以及 push-diff 会等实际 Checker 不再返回 checking 后再交给比较器。等待上限 120 秒，
这些 GET 的 HTTP 客户端上限至少 150 秒；entries 不触发准备，保留普通截止时间。
超时以专用 harness header 标记为框架错误，比较器直接终止，不计算 new/stale；不会把
checking、超时或别的阶段伪造成一致结果。生产异步响应、TTL 和比较字段均不改变。
验收同时运行慢 Checker 的 Go/Flask 自检和超时不进入比较器的回归；gate.sh 内包含 tagged Go 自检。A14 接线后 **24 条全部要消失**。

## 隔离与认证

脚本先构建 Go，然后进入新的 user/net/mount/PID namespace。
命名空间内只启用 lo；程序用接口 API 再次确认只有 loopback。
文件系统 Unix socket 不受网络命名空间限制（例如 systemd-resolved 的 varlink socket 会让
nss-resolve 在宿主机上解析域名），因此脚本在命名空间内用 tmpfs 覆盖 `/run`，并把
`/etc/nsswitch.conf`、`resolv.conf` 固定为 `hosts: files dns` / `nameserver 127.0.0.1`。
`run.py` 启动时证明无出口：只有 lo、看不到宿主机 IPC socket、`example.com` 解析失败、
到 192.0.2.1 和 2001:db8::1 的 TCP 连接返回网络不可达。任一项不满足即框架失败。
快照以 bind mount 挂为只读，程序验证只读挂载标志，并在运行前后对文件内容做摘要检查。
拒绝含符号链接的快照，防止复制或访问越出快照目录。

Python、Go 和比较程序都在同一个断网命名空间。两端各加载快照的一份临时副本；
无论正常完成还是异常退出都清理副本。SIGINT/SIGTERM/SIGHUP 触发清理；
运行期间持有共享锁，第一个拿到独占锁的运行会删除被强制终止的旧运行遗留的副本；PID namespace
确保服务退出后不会遗留子进程。直接 SIGKILL/主机断电无法执行文件清理，遗留副本仅会位于私有目录。
没有外部设备假服务，也不会放开网络。真实设备不可达时保留并比较原始 HTTP 失败响应；
报告另外统计两端连接失败数。HTTP 客户端初始超时为 20 秒；只有一侧传输失败
（超时、连接被拒、连接被重置）时，只重试失败侧，每个用例每侧最多 3 次，
超时依次为 40、80、160 秒（包括 bracket 期间的失败，共用同一预算）。
重试成功后正常比较；耗尽后记录 `status transport-one-sided` 并令门禁失败。
两侧都无响应仍记录 `status transport-both`，不重试；收到 HTTP 响应（包括 500）
不会触发传输重试，真实状态码差异立即失败，也不能借 bracket 重问消除。

两端使用相同的 `ADMIN_USERNAME`、`ADMIN_PASSWORD`、`JWT_SECRET_KEY` 测试环境变量，
各自调用正常 `/api/auth/login` 获取 JWT。它们本来就支持环境变量账号；
不读取生产密码，不更改快照账号，也不添加认证后门。Agent 专用配置拉取使用副本里的 Agent token，
只在断网请求和服务器私有报告中使用。

Python 通过测试启动器在导入 app 之前禁用具名启动函数，正常加载持久历史。
HTTP 模式直接构建 `cmd/configflow`，只加 `-tags configflow_proddiff`，不生成 overlay，
不重写任何 Go 源码。`lifecycle/mode_proddiff.go` 将默认 Runtime 固定为 ModeManual，
同步执行已注册 Startup；main 的 build-tag helper 从实际 Router 导出路由清单。
Startup 顺序为 `load-config`、`subscription-cache-prune`、`agent-manager-init`、
`metrics-load-history`（对应 Python 导入时的一次性工作），五个周期循环不启动。Startup 完成后输出 `.loops`、`.startups` 和最后的 `.ready` 标记，
比较程序等到 ready 才发请求；Startup 清单来自注册表，没有具名工作白名单。
普通构建的 helper 是 noop，默认 Runtime 固定为 ModeRun，没有环境变量或参数能关闭循环。
`configflow_difftest` 是另一独立 tag：只向 `CONFIGFLOW_LIFECYCLE_EVENTS` 指定的私有 JSONL
文件追加 round/job start/end 事件，用于逐轮同步；它不会关闭循环。

当前 Python 禁用 10 项：DNS follow、OxiDNS HA publication、IPv6 verification、
Agent rule sync、metrics flush、OxiDNS ranking、domain discovery、scheduled refresh、
router rule sync、Sub-Store idle monitor。
当前 Go 禁用 5 个已注册循环：metrics-history-flush、oxidns-ranking-history、
sub-store-idle、scheduled-refresh、domain-discovery-auto；四项 Startup 按上文顺序同步执行一次。
Python 多出的 5 项（DNS follow、HA publication、IPv6 verification、Agent/router rule sync）在 Go 中尚无实现，
无需禁用。`flask_worker.py` 记录每个启动的线程（只写私有目录），被禁用的循环一旦启动即框架失败。
所有已知定时启动入口均已关闭；按请求执行的计算、连接尝试仍在隔离网络中运行。
Python 启动迁移和缓存整理仍正常执行，但只作用于临时副本。

## 覆盖与比较

实现位于 `server/tools/proddiff/`：这是离线验收工具，不是服务内部依赖。
采用 Python 是为了复用测试服务器已有的 SQLite、PyYAML 和 Flask 参考运行环境，
不引入 Go 业务依赖或新的依赖包。

- 从 `server/parity/routes.tsv` 选择所有 GET，不相信表中的 todo/done 状态。
  是否“Go 尚未实现”由测试构建实际注册的 method/template 决定；不会把业务 404 当作未实现。
  Go Router 和 Flask `url_map` 中的每条路由都必须出现在 routes.tsv，否则打印 `UNMAPPED-*-ROUTE` 并失败，
  防止模板写法不同的已实现路由被永久算作未实现。
- 每个配置空间均作为请求作用域，展开保存的 Agent、routers.items、订阅、聚合、规则库、
  部署记录、预览、安装会话、安装脚本 token。读取配置 SQLite 和快照元数据时只使用副本。
  对路径中没有 `<profile_id>` 的 GET，第一个配置空间的每个实体另发一次不带配置空间参数的请求，
  覆盖默认配置空间解析，以及拒绝任何查询参数的路由的正常分支。
- 请求分三段：GET 并行 → 只读 POST 并行 → `/api/logs/*` 最后串行（日志内容取决于之前所有请求）。
- 安装脚本和 Docker 命令按保存的 Agent 参数渲染；配置拉取包含 Mihomo、Surge、Loon、
  MosDNS、OxiDNS；生成、预览、导出、provider、Agent 配置均由各自公开入口覆盖。
- 非 GET 使用经过逐条选择的 `proddiff-readonly-post.json`。
  新增 POST 不会因为名字含 preview 或 generate 就自动执行。
  生成接口可在临时副本中保存生成产物，不能提交配置或应用设备变更。
  HA guide 只执行 `action=inspect`，避免签发新安装能力。
- JSON 深度比较，YAML 解析后比较；JSON 内的 content/config/compose 也解析配置内容。
  JSON/YAML 重复键视为解析失败（PyYAML/json 会静默保留最后一个，yaml.v3 消费方会拒绝）。
  相等的整数与浮点数单独记为 `int-float`。ZIP 按成员读取后做同样比较，并比较成员权限位和换行风格。
  文本逐行比较；换行风格（CRLF/CR）不同记为 `line-endings`，只忽略末尾换行数量。
  状态码、Content-Type、除传输层（Date、Server、Content-Length、Connection、Keep-Alive、
  Transfer-Encoding）之外的全部响应头独立比较；两端都无响应记为 `transport-both`。
  每个服务的临时数据目录路径在比较前替换为 `<DATA_DIR>`。差异只输出模板、键路径、类型。
- L1 D8 恢复 `/api/agents/i/<token>` 的原始纯文本404/503：正文（含末尾换行）、状态、
  Content-Type 必须与 Flask 完全一致，不再允许 JSON 包装。只有已批准的错误附件头移除
  单独声明：Python 必须恰为 configflow-install.sh 的 attachment，Go 必须没有该头，
  其余头仍比较。篡改消息、状态、类型或回到 JSON 都会失败。
- L1 D3 的 `/api/agents/install-script` 成功脚本仅反向应用固定的
  `internal/installer/installertest/l1-rollback-deltas.json` 源码差异，与在线差分共用
  同一份批准记录；完整模块必须精确匹配，其余正文、状态和响应头继续比较。
  修改无关脚本行或回滚模块任一字节的负向检验会失败。不新增允许差异条目。
- `proddiff-normalize.json` 逐项声明精确路由（必须在 routes.tsv 中）、精确路径和原因，不支持通配符；
  未知字段或多个模式同时出现即框架失败。路径可用 `name[]` 表示列表每一项，`header.<name>` 表示响应头。
  模式：默认（两端同类型标量才替换）；`pattern`（两端各自只替换匹配部分，其余文本照常比较）；
  `tolerance`（两端时间文本形状相同且相差不超过 N 秒）；`unordered`（按多重集合比较列表）；
  路由级 `bracket`（按墙钟时间窗口计算的 GET：出现差异时交替重问两端，
  只有 Go 等于紧邻其前或其后的 Python 回复才通过，最多 3 轮；
  单侧传输重试恢复后从最新回复的另一侧继续交替，不能跳过相邻性检查。
  未声明的路由不做窗口重问，只允许上述单侧传输重试）。
- 每次运行必做 23 项自检，全部交给同一个生产比较器，并要求报告准确的路径和类型：
  对照组（真实回复与自身、重新编码的真实 JSON、重新输出的真实 YAML 必须无差异）；
  真实 Go 回复的状态码、Content-Type、响应头值/缺失、双端无响应；
  合成 JSON 的深层叶子、int↔float、int↔string、重复键、列表顺序；
  最大的真实 Go JSON 回复的叶子、缺键、多键、null 与值、列表顺序；
  真实 Go YAML 的叶子、重复键；真实 Go 文本的改行、插行、CRLF。
  任一项未检出、或缺少可用的真实样本即失败；永远不报差异或永远报差异的比较器都过不了对照组。
  合成测试另覆盖响应头、规范化各模式、规则校验、bracket、分段顺序、数据目录替换和实体枚举，
  以及单侧传输恢复/耗尽、双方失败不重试、HTTP 500 不重试和恢复后的 bracket 相邻顺序。

没有保存实例的路径参数用合成 missing 参数请求错误分支，并在 coverage 中明确计数。
进程内探测 job、设备端 DNS query record 等无法从离线快照恢复，只覆盖错误分支。
现有预览也可能已过期。未接入 Go 的路由单独计数，不发出无意义的双端比较，
不把它们计为 HTTP 通过；已有公开 Go 生成函数的项目由下面的生成器模式先覆盖。
没有 Go 生成实现的项目仍明确标为未实现，不计入成功覆盖。
POST 草稿输入来自快照；需要额外用户意图的接口可能走校验失败分支。
这是一份真实保存状态的回归检查，不替代覆盖每个写操作和每种请求分支的领域验收。

## HTTP 接通前的生成器模式

同一条脚本会额外构建 `generator/main.go`，直接调用 Go 现有公开生成函数。
Python 对应 `python_generators.py`，调用参考实现的公开生成函数。
两端再各取一份只读源快照的临时副本；公共私有 manifest 仅含选择配置空间、订阅、Agent 的 ID，
不会提交或取回本机。Go 通过 `internal/repo` 加载配置，使用生产 `server.NewApp` 的公开 wiring，
不监听 HTTP、不调用 lifecycle.Start、不替换生成业务逻辑。
Python 通过 get_config 加载同一配置空间；无 app 初始化、无后台循环。

Mihomo 使用 `SkipProviderPreflight: true` / `preflight_providers=False`，避免生成主配置时先要求外部源可达。
订阅输出单独调用 PrepareSubscriptionProvider / prepare_subscription_provider，
通过公开 Fetch 参数注入相同的 offline 失败，让业务函数读取快照节点缓存；不提交暂存 cache_updates。
Surge/Loon 订阅文本继续调用两端公开的 provider 转换函数。
Agent 内容调用对应服务生成器；Mihomo 传 for_agent、region_probe 和副本里保存的核心版本。
完整设备端 final-preview 和部署 bundle 仍需要尚未实现的 Go 入口，单独列为未实现。

当前生成器覆盖（每项成功生成后再做结构或行比较，报错不算成功输出）：

| 生成器 | 输入数 | 成功输出数 |
|---|---:|---:|
| Mihomo / Surge / Loon / MosDNS / OxiDNS 主配置 | 各 2 | 各 2 |
| MosDNS 自定义文件 / 规则下载清单 | 各 2 | 各 2 |
| OxiDNS 自定义文件 / 规则下载清单 | 各 2 | 各 2 |
| Mihomo provider / 规则下载清单 | 各 2 | 各 2 |
| 订阅 Mihomo / Surge / Loon | 各 4 | 各 4 |
| Agent Mihomo | 1 | 1 |
| Agent OxiDNS | 2 | 2 |
| Agent MosDNS / Surge | 各 0（快照无实例） | 各 0 |

合计 37 个成功输出，新增生成器差异 0。没有用空配置或合成配置替换生产配置。
Go 尚无公开实现：路由器脚本导出（1 个输入）、完整 Agent 部署 bundle（3 个输入）、
aggregation-provider（当前快照 0 个输入）。这些缺口不能理解为已通过。

`generators.py` 的静态 inventory 记录生成器与 HTTP 模板的归属（包括当前尚未实现的三类）。
对应模板真实注册到 Go 后，改用 HTTP 已比较的输出记覆盖；订阅会请求三种 format，
Agent 按 service_type 归属。没有独立 HTTP 入口的附件元数据生成继续直接比较。
覆盖文件保留每种生成器的 cases、compared、success、双方/单方报错数及当前 owner。
所有成功输出消失会丢失成功标记并令门禁失败，不能用双方报错代替成功。
新生成器或新公开函数实现需更新 inventory/适配器，禁止用业务重实现补齐缺口。

## 基线和私有报告

`server/parity/proddiff-baseline.txt` 每行包含 method/template、键路径、差异类型、原因或 open。
出现基线之外的新项目即失败；基线项目消失则提示更新。
`proddiff-coverage.txt` 记录之前实际比较的路由和生成器、成功输出标记，以及
`<route> snapshot-parameter`（至少一个用例使用了快照中的真实参数）；只剩合成 missing 参数也算覆盖回退。
基线原因不能是 `open`：重录后必须逐项写明原因（Go 问题写 `proddiff-findings.md` 编号）。
`proddiff-generator-coverage.json` 记录全部生成器（包含零实例、未实现项）的覆盖数量；
普通运行要求每种生成器成功输出数不低于该基线，防止部分或全部转为报错后掩盖回退。
需要人工审阅后更新基线时执行：

```bash
rt --back server/parity/proddiff-baseline.txt \
   --back server/parity/proddiff-coverage.txt \
   --back server/parity/proddiff-generator-coverage.json \
   -- server/scripts/proddiff.sh --record-baseline
```

此命令是显式重录模式，普通门禁不能加 `--record-baseline`。
取回的文件仅有公开模板、生成器名称、键路径、类型和数量，不含真实数据值。

每次运行的报告位于测试服务器 `~/cf-prod-snapshot/runs/run-*/`，目录权限 700。
`summary.json`、`coverage.json`、`differences.tsv`、`report.md` 是脱敏结果；
summary 中的 `transport_retries` 分别累计 Python/Go 的实际传输重试请求数（不含初次请求），
`counts.bracket-rescued` 保留成功窗口重问的用例数；两者都是随请求调度变化的重试计数。
`loadavg.start` / `loadavg.end` 保存比较程序开始/结束时的 `/proc/loadavg` 原文；
`connection_failures` 只统计最终回复，因此成功恢复的临时失败只体现在重试计数中。
`responses.jsonl`、`generator-*.jsonl`、`generator-jobs.json` 和服务日志可能含真实请求参数、响应和凭据，只能留在该私有目录，
不能复制到仓库、交接回复或公开报告。框架错误详情也只写入私有 `error.txt`。
普通运行不会在项目中生成未跟踪文件。

当前基线 10 项，全部来自 Go 问题 F1（见 `server/parity/proddiff-findings.md`）：
`GET /api/agents/<agent_id>/oxidns/rules/update` 在默认配置空间下对 HA 成员返回 500（A18 未移植），
以及由此多出的两行异常日志造成的 `/api/logs/info`、`/api/logs/tail` 差异。
审查记录见 `server/parity/review-proddiff.md`。

## 第二阶段建议回放的写接口

需要同时检查 SQLite、文件和隔离假设备收到的请求，至少包括：

- `POST /api/profiles`、`POST /api/profiles/<profile_id>/clone`
- `POST /api/config/import`、`POST /api/subscriptions/<sub_id>/fetch`
- `POST /api/rules/compose`、`PUT /api/agents/<agent_id>`
- `POST /api/agents/<agent_id>/heartbeat`、`POST /api/agents/<agent_id>/push-config`
- `POST /api/routers/<router_id>/apply`、`POST /api/routers/<router_id>/routes/apply`
- 安装能力签发、安装登记、部署 activate/rollback/recover 和 settings 保存入口

上述建议也写在每轮私有 report.md 的末尾。第一版不执行这些写操作。

## 单侧传输重试验收（2026-11-17）

仅修改本工具、合成测试和本文档；未修改业务代码、baseline 或 normalize。
单元测试在 .32 上运行：

```bash
rt -- bash -lc '~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py'
# Ran 30 tests ... OK
```

前台 `rt -- python -c ...` 驱动 CPU 压力和顺序的三次
`subprocess.run(["server/scripts/proddiff.sh"])`，不使用 `--record-baseline`。
压力使用有超时的 `stress-ng --cpu-method int64`，所有自加压力进程均已清理。
共享服务器同时运行其他测试，实测一分钟负载如下：

| 轮次 | 开始 / 结束负载 | Python / Go 传输重试 | bracket-rescued | 门禁结果 |
|---|---|---|---|---|
| 1 | 46.81 / 73.10 | 1 / 0 | 3 | exit=0, new=0, stale=0 |
| 2 | 75.34 / 77.28 | 0 / 0 | 0 | exit=0, new=0, stale=0 |
| 3 | 79.58 / 85.95 | 0 / 0 | 0 | exit=0, new=0, stale=0 |

三轮均为 2831 个枚举用例、415 个 HTTP 比较、23 项运行自检通过，
最终 Python/Go 连接失败计数均为 30，覆盖回退和未映射路由均为 0。
仅去除 `transport_retries`、`counts.bracket-rescued`、`loadavg` 后，完整 summary
序列化为排序键的紧凑 JSON，三轮 SHA-256 均为
`fc0edcca151f6fb60c1fbb0387e17a536c125388207cd822c2fdc23c55ddcd5d`。
脱敏 summary 保存在 .32 的 `~/cf-prod-snapshot/runs/load-acceptance-129088.json`。

更早的一轮在 81.88 → 80.03 负载下发生 33 次 Python 传输重试，仍为
`new=0 stale=0`、最终连接失败 30/30；但窗口重问增加了线程数量，
那组三轮不满足非重试字段完全相同，未作为上述三轮验收结果。

O3-3 的实例锁、Startup 和关机接线见 [实现说明](../../design/o3-startup-shutdown.md)。

A11 bundle generator: `GENERATOR agent-deployment-bundle` now invokes the real Python
`build_deployment_bundle` and Go `deploy.Build` on each transactional Agent's captured profile.
Both materialize subscriptions from the snapshot with offline fetch and use isolated rule caches.
Comparison includes protocol/service/config path and ordered paths/roles/names. Both workers
first verify every manifest size/SHA256 against their own delivered bytes. Generated main and
provider YAML files are parsed and compared structurally; all other files retain exact
size/SHA256/base64 comparisons. This accepts native YAML formatting without hiding corrupt
protocol metadata or altered provider content. No content fingerprint is normalized: v1
fingerprint encoding is independently locked by the A11 Python corpus. This generator does
not attest target final-preview, core reload or rollback (A14/L2).

C31 removes the temporary aggregation freeze. Both workers now independently render every
provider with offline fetch and snapshot caches. The former partial Agent case is promoted
to successful end-to-end generator coverage; the standalone aggregation-provider generator
also compares real Python/Go output. No frozen fixture bytes are passed between workers.

## Provider 响应预算与负载隔离

普通服务的 Mihomo provider 保持 12 秒 last-good 降级：慢渲染返回旧 YAML 和
`X-ConfigFlow-Provider: last-good`，后台继续刷新；没有旧内容则一直等待。
这不是固定的生成结果：相同输入在两端渲染速度不同的情况下，一端可返回旧文件，另一端已返回新文件。

生产快照比较专门等待两端完成生成。`flask_worker.py` 的 `provider_policy.wait_for_fresh_provider`
把 Python future 的等待预算设为 `None`；Go 仅在 `configflow_proddiff` 构建的启动钩子中设置
`provider.SetResponseSeconds(-1)`。两端仍通过原 provider HTTP 入口生成、校验、提交并保存 last-good，
没有复制缓存给另一端，也没有更改 body、header、比较规则或 baseline。HTTP 传输重试和最终失败门禁仍然有效。
普通 Go 构建不调用这个钩子，12 秒生产行为不变。

`internal/provider/difftest/TestSlowProviderParity` 对两个 provider 路径比较真实 Python：
生产默认预算的慢渲染必须带 last-good header 并返回旧 body；比较模式必须等到新 body，且仍保存新产物。
生产测试使用同步屏障和真正的 12 秒预算，不用短超时模拟。
`cmd/configflow/TestProddiffWaitsForFreshProvider`（`-tags configflow_proddiff`）验证启动钩子；
比较器负向测试仍要求旧 header 和 proxies 长度差异报错。根因、负载复现和命令见
[复核记录](../../parity/fix-agg-provider-load.md)。
