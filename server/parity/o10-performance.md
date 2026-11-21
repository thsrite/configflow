# O10 性能与内存审计（2026-11-21）

审计者：Opus（fresh），分支 `go-audit-o10`（基于 `go` 2f47d56bb）。
24 小时浸泡留给实验网 L6，本文只覆盖测试服务器上的 30 分钟空闲 + 合成负载。

## 结论（先看这里）

**Go 版本当前达不到重写目标，而且在多数路由上明显比 Python 慢、占内存多。** 根因不是生成器算法，
而是几条“为读一小块数据而整份拷贝/重编码”的路径，在每个请求、每个后台循环里重复执行：

1. `repo.Config` / `repo.System` 用 JSON 往返（`model.Marshal` + `Unmarshal`）深拷贝**全部 Agent 及其部署/IPv6 校验历史**
   （快照里 3 个 Agent 共 312 KB），而大多数调用方只要 `system_config`、`backup` 或一个 profile。
2. `model.Marshal` 在**每一层嵌套记录**上都重新做 UTF-8 + `json.Valid` + 逐 token 重复键检查（`validJSON`），
   一份 Agent 列表被校验 5–6 遍；它单独占负载期全部分配的 39%。
3. 通知快照（`notifications.collect` → `tasks.Available`）为每一行任务重新拷贝 System / 编码整条 Agent 记录；
   而每次任务状态写入（定时刷新每个资源一次）都会触发一次完整通知快照。

本分支已做一个小修复（见“已做的修复”）：四个只读设置的热点调用点改用新的窄读取
`Repository.SystemConfig` / `Repository.Backup`。空闲 CPU 底线从约 9% 降到约 2.3%，规则同步路由分配降 91%，
通知路由分配降一半。其余问题拆成下文的 T1–T11。

## 方法

- 工具：`server/tools/o10perf/`（本分支新增，只用于测量）。`run.sh` 构建普通 Go 二进制，用 `go build -overlay`
  临时加入 pprof + `runtime.MemStats` 监听（源码树里没有这个文件，产品构建不受影响），然后在与 proddiff 相同的
  user/net/mount/pid 命名空间里运行：只有 `lo`，`/run` 被 tmpfs 覆盖，DNS 固定到 127.0.0.1，快照只读挂载；
  `perf.py` 启动前再次证明无出口（只有 lo、宿主 IPC socket 不可见、`example.com` 解析失败、
  到 192.0.2.1 / 2001:db8::1 的 TCP 返回不可达）。两端各用快照的私有临时副本，结束即删除。
  Sub-Store 两端都禁用（`SUB_STORE_BUILTIN_DIR=/nonexistent`），所以下面的 RSS 只是后端进程本身。
- 所有后台循环在两端都**正常开启**（与 proddiff 不同），以测真实空闲开销。所有路由器、Agent、WebDAV 备份、
  规则源/订阅出站都因断网立即失败——这会让“刷新失败 → 300 s 后重试”比生产频繁，空闲尖峰因此偏高（见注）。
- 采样：每 10 s 读 `/proc/<pid>`（RSS、线程、fd、utime+stime）与 Go `MemStats`；Go 在空闲第 2 分钟起抓
  CPU profile，空闲结束抓 heap/goroutine/allocs；负载阶段每个用例 1 次预热 + 10 次计时，同时抓 120 s CPU
  profile 和负载前后 allocs 差分。
- 合成负载：快照里的 2 个配置空间 × 5 种生成（Mihomo/Surge/Loon/MosDNS/OxiDNS）、聚合 provider、
  `/api/agents`（全量 / `summary=1` / `history=0`）、每个 Agent 的详情、`rule-sync`、心跳（合成的合法 metrics），
  `/api/notifications`、`/api/profiles`。另跑了 `internal/convert` 现有的 128 节点 + 512 规则 Mihomo 基准。
- 主机：.32（12 核，空闲时测）；Go-only 空闲 profile 在 .64。Python 用 `~/cf-test/.venv`（3.11），
  启动方式与生产相同（`python -m backend.startup` → waitress）。
- 生产快照只在测试服务器上使用；本文只有汇总数字。私有输出（profile、日志）在测试服务器
  `~/cf-prod-snapshot/runs/o10-*`，不提交。

## 数字

### 启动与空闲 30 分钟（修复前，Go 与 Python 同时运行，.32）

| 指标 | Go（修复前） | Go（本分支修复后，12 min） | Python |
|---|---:|---:|---:|
| 启动到可服务 | 1.45 s | 1.41 s | 1.33 s |
| 启动后 RSS | 103 MB | 84 MB | 75 MB |
| 启动后 Go 堆 | 48 MB（启动期累计分配 ≈1.1 GB） | 33 MB | — |
| 空闲 RSS（结束 / 最大） | 289 / 320 MB | 232 / 233 MB | 117 / 118 MB |
| GC 后存活堆 | 17 MB | 17 MB | — |
| 空闲 CPU 平均（单核 %） | **34.9%** | 24.3% | **0.9%** |
| 空闲 CPU 稳态底线（无刷新重试时） | ≈9%，分配 ≈36 MB/s | **≈2.3%，分配 ≈10 MB/s** | ≈0.4% |
| 空闲 CPU 尖峰（10 s 窗口） | 425% | 405% | 23% |
| 空闲分配速率 | 9.3 GB/min | 7.1 GB/min | — |
| goroutine（开始/结束/最大） | 15 / 15 / 34 | 15 / 15 / 22 | 线程 21–22 |
| fd（开始/结束/最大） | 14 / 14 / 17 | 14 / 14 / 17 | 16 / 12 / 17 |

注：

- 生产 Python 常驻约 146 MB（含导入 72 MB）是带 Sub-Store 客户端与真实设备的数字；同条件下本测 Python 117 MB。
- Go 的 RSS 几乎全是**分配抖动**：存活堆只有 17 MB，`heap_sys` ≈ 294 MB。降分配后 RSS 会随之下降；
  在那之前调 GOGC/GOMEMLIMIT 只是掩盖（T9）。
- 30 分钟内 goroutine 和 fd 没有增长趋势（未见泄漏）；24 h 趋势留给 L6。
- 空闲尖峰来自定时刷新：断网下 46 个规则源/订阅每 300 s 全部失败一次，**每次任务状态写入触发一次完整通知快照**
  （修复前每次约 2.5 s CPU / 1.9 GB 分配）。生产里刷新成功、按各自间隔（默认 1 天）运行，频率低得多，
  但每次的单价相同；浏览器每 30 s 轮询 `/api/notifications` 的单价也相同。

### 每路由（修复前，.32；Python 同快照）

延迟为 10 次中位数（受后台刷新尖峰干扰，Go 侧偏高，只作量级参考）；分配按 handler 从负载期 allocs 差分归因，
不受后台干扰。

| 路由 | Go 延迟 | Go CPU/次 | Go 分配/次 | Python 延迟 | Python CPU/次 | Go/Python |
|---|---:|---:|---:|---:|---:|---:|
| GET /api/notifications | 2.5 s | 4.3 s | **1905 MB** | 41 ms | 42 ms | ×60 |
| GET /api/agents（全量） | 0.91 s | 1.6 s | 688 MB¹ | 35 ms | 35 ms | ×26 |
| GET /api/agents?history=0 | 0.85 s | 1.5 s | 686 MB¹ | 44 ms | 151 ms | ×19 |
| GET /api/agents?summary=1 | 89 ms | 160 ms | 54 MB¹ | 4 ms | 4 ms | ×22 |
| GET /api/agents/<id> | 0.78 s | 2.0 s | 386 MB | 25 ms | 25 ms | ×31 |
| GET /api/agents/<id>/rule-sync | 0.22 s | 0.8 s | 130 MB | 10 ms | 11 ms | ×22 |
| GET /api/config/<p>/surge·loon·mosdns | 0.17–0.20 s | 0.33–0.39 s | 137 MB² | 13–19 ms | 14–19 ms | ×10–15 |
| GET /api/config/<p>/oxidns | 0.23 s | 0.45 s | 137 MB² | 37 ms | 40 ms | ×6 |
| GET /api/config/<p>/mihomo | 0.67 s | 1.1 s | 137 MB² | 0.63 s | 0.64 s | ×1.1 |
| GET …/aggregations/<agg>/provider | 1.0 s | 1.9 s | 390 MB | 0.74 s | 0.71 s | ×1.4 |
| GET /api/profiles | 41 ms | 93 ms | 31 MB | 2 ms | 1 ms | ×20 |
| POST /api/agents/<id>/heartbeat | 7 ms | 23 ms | 2.6 MB | 2 ms | 2 ms | ×3 |

¹ 三种 `/api/agents` 变体同一 handler（`createAgent` → `listContentAgents`），handler 归因只能给出 33 次平均（447 MB）；
表中是各变体自己的 `MemStats` 差值（含少量后台分配）。
² `/api/config/*` 五种引擎同一 handler（`config.live`），110 次请求平均；74% 在 `repo.Config`（拷贝 Agent）。
Mihomo 与 provider 两端都被 Sub-Store 断连拖慢（3 个 URI 节点每次尝试转换），比较时看比例即可。

合成规模（`BenchmarkMihomoGenerationProfile/128`，128 节点 + 512 规则，GOMAXPROCS=4，.64）：
**21–22 ms，33.6 MB，246k 次分配**（batch-7 时 64–70 ms / 76 MB；batch-6 前 typed 化之前的 Go 9.8 MB；Python 95 ms / 119 MB）。
生成器本身已经比 Python 快；生产路由慢是因为生成前的 `repo.Config` 拷贝和每层校验。

### 本分支修复前后（Go，.32，同一工具）

| 项 | 修复前 | 修复后 |
|---|---:|---:|
| 空闲稳态 CPU / 分配 | ≈9% / 36 MB/s | ≈2.3% / 10 MB/s |
| GET /api/agents/<id>/rule-sync 分配 / CPU | 130 MB / 0.8 s | 12 MB / 0.12 s |
| GET /api/notifications 分配 | 1905 MB | 944 MB |
| provider 路由中 `authorize` CPU（负载期合计） | 3.59 s | 0.02 s |
| provider 路由分配 | 390 MB | 270 MB |
| `/api/agents`、`/api/agents/<id>`、`/api/config/*` | — | 不变（T1、T4） |

## 热点（profile 证据）

负载期 allocs 差分（修复前，合计 103 GB）：

| 函数（累计） | 占比 | 说明 |
|---|---:|---|
| `model.Marshal` | 71% | 记录编码；内部每层都调 `validJSON` |
| `model.DecodeRecord` | 66% | 记录解码（含 `parseObject` → `validJSON`） |
| `model.validJSON` | **39%** | 每层嵌套重复 UTF-8 / `json.Valid` / token 遍历；`json.NewDecoder` 本身 12 GB |
| `repo.(*Repository).View` 内的拷贝 | 49% | `resolveConfig` 26 GB（其中 **拷贝 Agent 列表 22.5 GB = 85%**）、`System` 拷贝 23 GB |
| `rulesync.systemSecrets`（`System` 拷贝） | 16% | 每个 Agent 行、每个规则同步轮次都拷贝整份 System 只为读 `system_config` 的密钥 |
| `notifications.collect` | 24% | `tasks.Available` 80%：`agents.agentRows` 13 GB（rulesync 行 10.2 GB、dnsfollow 行 1.6 GB）、`discovery.domainRows` 2.9 GB、`backupRows` 1.5 GB、`metrics.BuildRows` 1.5 GB |
| `api/agents.loadContentSource` | 14% | 每个 Agent：`repo.Config` → `model.Marshal` → `deploy.SourceFromJSON`（**每个 Agent 把整份配置再解码一遍**） |
| `api/agents.publicAgentModel` / `deploy.NewTypedTarget` | 4.7% / 3% | 公开投影与目标构造仍整条编码 Agent |

空闲 CPU profile（Go-only，.64，360 s）：

- 57%：`refresh.recordStatus` → `taskstatus.SaveResult` → `notifications.RefreshTasks` → 完整快照（见上）。
- 11%：周期循环本身，主要是 `rulesync.(*Service).Round` → `observePresence` → `systemSecrets`（每 5 s 一轮，
  每轮整份拷贝 System），其次 `ViewAgents` → `agents.copyAgent`（每轮拷贝含历史的 Agent）。
- 其余为 GC（`gcBgMarkWorker` ≈ 21%）。

128 节点 Mihomo 基准的分配（20 次合计 2.16 GB）：`model.EncodeRecord` 45%，其中 `RuleConfig.MarshalJSON` 31%
来自 `convert.validateRuleIdentities` 与 `rules.ValidatePolicies → rulePolicy`——**为读一个成员把每条规则整条编码再
`json.Unmarshal` 成 map**；`yamlx.Marshal` 21%（每个值新建一个 yaml.v3 encoder，`yaml_emitter_initialize` 8%）；
`yamlx.copyGraph` 6%。

### 重复解码路径清单（WORKFLOW O10 要求）

| 路径 | 每次重复的范围 | 触发频率 |
|---|---|---|
| `repo.Config` → `copyValue(document.System.Agents)` | 全部 Agent + 历史 | 每次生成、聚合 provider、域名发现、通知快照、每个 Agent 的内容源 |
| `repo.System` → `copyRecord(&document.System)` | 全部 System | 规则同步每轮、每个 Agent 任务行、metrics、认证、备份行（4 处已修，见下） |
| `api/agents.loadContentSource` | 整份配置：Config 拷贝 → 编码 → `deploy.SourceFromJSON` 再解码 | 列表与详情里**每个 Agent 一次** |
| `rulesync.safeText` → `model.Marshal(record)` | 整条 Agent（含历史） | 每个 Agent 任务行、每条规则同步日志 |
| `model.Marshal` → 每层 `validJSON` | 子树重复校验 depth 次 | 所有记录编码 |
| `rules.rulePolicy`、`convert.validateRuleIdentities` | 每条规则整条编码 + 解码为 map | 每次生成 × 规则数 |
| `oxidns` 适配器 `model.Marshal(agent)` → `yamlx.FromJSON` | 整条 Agent 转 YAML 行 | 每次 OxiDNS 生成 × Agent 数 |

`internal/` 下（不含 `model`）共有 241 处 `model.Marshal(...)` / `.MarshalJSON()` 调用，T7 逐一检查只读一个成员的用法。

## 已做的修复（本分支）

- `repo.(*Repository).SystemConfig(ctx)`、`repo.(*Repository).Backup(ctx)`：只拷贝对应子记录，语义与
  `System(ctx)` 返回的同名字段相同（调用方拥有副本）。
- 改用窄读取：`agents/rulesync.systemSecrets`、`agents/metrics.currentIntervalSeconds`、`tasks.backupRows`、
  `auth.ValidateSystemTokenOrJWT`（`config == nil` 分支）。行为不变，只是不再拷贝 Agent 列表。
- 测试 `internal/repo/o10_system_settings_test.go`：两个读取与 `System()` 的对应字段编码相同；修改返回值不影响仓库；
  500 条部署历史下分配不超过 1 条历史时 + 64 KB；负向对照证明 `System()` 在同样数据下多分配 ≥512 KB
  （否则界限无意义）。
- 检查（.32）：`gofmt -l` 无输出；`go vet ./...` 通过；`go test -race` 通过：repo（456 s，需 `-timeout 40m`，
  与其他包并行时默认 10 min 超时）、auth、tasks、agents/metrics、agents/rulesync、notifications、api/config、
  api/subscriptions、api/aggregations；`TestConcurrencyFacadeBoundary` 通过。未跑全量 gate（按 AGENT-BRIEF）。

## 任务清单（按收益排序，可直接派给执行者）

每个任务都应附：前后 `server/tools/o10perf/run.sh` 数字（或对应基准）、一个“分配不随 Agent 历史/规则数增长”的
回归测试（参照 `o10_system_settings_test.go` / `review_n811_test.go` 的写法，带负向对照），以及受影响包的 difftest。

**T1 `repo.Config` 不再拷贝 Agent（最高收益，中）**
`resolveConfig(..., agents bool)` 目前 `Config()` 恒为 true。改为默认不带 Agent；只有确实读 Agent 的调用方
（OxiDNS 适配器 `source.HasAgents` 路径）显式请求，且只取它用到的成员（id、名称、类型、规则 DNS 相关字段），
不带 `deployments` / `ipv6_verifications`。核对 `model.Config.MarshalJSON` 的使用者（Python 兼容视图）是否有
输出依赖 `agents`。预期：`/api/config/*` 137 MB → ≈25 MB/次，Surge/Loon/MosDNS 延迟降到 Python 同量级；
聚合 provider、域名发现、通知、Agent 内容源同时受益（`resolveConfig` 占全部分配 25%）。

**T2 `model.Marshal` 只在最外层校验一次（最高收益，中，需 Opus 设计复核）**
`EncodeRecord` 对每个成员调 `Marshal`，`Marshal` 每次都 `acyclic` + `validJSON`，嵌套 depth 层就重复 depth 次。
改为内部编码路径不校验，只有对外的 `Marshal` 在最终字节上做一次；`EncodeRecord` 的 `seen` 与“opaque 不得遮蔽
已声明成员”检查已保证不产生重复键，原始成员字节在解码时已校验。`validJSON` 本身改为单次扫描
（`jsontext.Decoder` 读 token 而不是 `encoding/json.Decoder.Token` + 每层新建 decoder）。同样检查
`DecodeRecord` → `parseObject` → `validJSON` 的逐层重复。预期：所有路由与循环分配降 30–40%。
风险：model 是核心包，必须保留“任何输出都无重复键、合法 UTF-8”的保证——加一个 fuzz 目标对照旧实现。

**T3 通知快照降本（高，中）**
`notifications.collect` → `tasks.Available` 修复后仍 ≈944 MB/次：
(a) `rulesync.agentTaskRow` → `safeText` 每行 `model.Marshal(record)` 收集凭据——改为按成员读取凭据
（参照 N8-11 W1 的 `sanitizationTokensModel`），系统密钥每次快照算一次；(b) `dnsfollow.agentTaskRow`、
`agents.ListAgents`、`metrics.BuildRows`、`discovery.domainRows` 同类检查；(c) 一次快照内共享一次 Agent 读取
（`ViewAgents`），不要每个行构建器各拷贝一次。预期：`/api/notifications` 降到 < 50 ms / < 30 MB，
空闲时每次任务状态写入的 2.5 s CPU 尖峰随之消失。

**T4 Agent 列表/详情：内容源只建一次，不经 JSON（高，中）**
`api/agents.loadContentSource` 对每个 Agent 做 `repo.Config` → `model.Marshal` → `deploy.SourceFromJSON`。
按配置空间缓存一次请求内的内容源，并让 `deploy` 直接接受 typed `*model.Config`（去掉 JSON 往返）；
`publicAgentModel`、`deploy.NewTypedTarget` 改为读需要的成员。预期：`/api/agents` ≈690 MB → < 30 MB，
延迟从 0.9 s 降到 Python 同量级（35 ms）；Agents 页 10 s 轮询与 `summary=1` 每 5 s 轮询直接受益。

**T5 定时刷新：一轮只刷新一次通知（中，小）**
`refresh.RunRound` 每个资源 `recordStatus` → `SaveResult` → `OnRecord` → 完整通知快照（Python 同样逐条刷新，
但单价低 60 倍）。在一轮内合并为结束时一次刷新（单个手动运行仍立即刷新）；先确认持久化 observation 的
“首次出现时间”不因合并而改变用户可见结果。T3 做完后收益变小，可排在 T3 之后。

**T6 其余 `repo.System` 调用点（中，小）**
剩余整份拷贝 System 的调用：`health/dns.go`、`setup/service.go`（2 处）、`agents/metrics/flush.go`（303、430 行）、
`agents/ipv6/adapter.go`、`agents/rulesync`（若 T3 后仍有）、`mcp/transport.go`（2 处）、`api/settings/owner.go`、
`api/config/custom.go`、`api/agents/traffic.go`、`api/rulelibrary/adapter.go`、`gateway/installation/adapter.go`、
`repo/scope.go`（启动一次，可不改）。只读 settings/backup 的改用本分支的窄读取；真要 Agent 的用 `ViewAgents`。

**T7 “整条编码只为读一个成员”清扫（中，小–中）**
`rules.rulePolicy`、`convert.validateRuleIdentities` 改用 `RuleConfig.MemberJSON`；按同样方式检查 241 处
`model.Marshal` / `.MarshalJSON()` 调用（`oxidns` 适配器把整条 Agent 转 YAML 行一并处理，与 T1 协调）。
预期：128 节点基准 33.6 MB → ≈20 MB；规则多的配置空间收益更大。

**T8 YAML 编码复用（中低，小）**
`yamlx.Marshal` / `prepareStrings` 每个值新建 yaml.v3 encoder（`yaml_emitter_initialize` 占基准 8%），
`yamlx.copyGraph` 6%。改为整份文档一次编码、避免逐值往返。

**T9 内存回收策略（在 T1–T4 之后，小）**
存活堆 17 MB 而 RSS 230–290 MB。降分配后重测；若 RSS 仍明显高于 Python（117 MB），再评估默认 GOGC 或在
无 cgroup 限制时设一个保守的软上限（`tuneMemory` 目前只在 cgroup 有限制时设置）。不要先于 T1–T4 做。

**T10 规则同步轮次不拷贝历史（低–中，小）**
`rulesync.(*Service).Round` 每 5 s `ViewAgents` → `agents.copyAgent`（含部署历史）；只取比较所需成员，
或在 head 未变时沿用上轮结果（Python #535 的“空闲轮”做法）。

**T11 性能回归防线（低，小）**
把 `tools/o10perf` 的负载部分做成可选的 gate 步骤或周期任务：对 `/api/notifications`、`/api/agents`、
`/api/config/*` 设每请求分配上限（例如 Python 同路由耗时的 3 倍、分配 50 MB），超限失败。避免 batch-6 那样的
回退再次出现。

## T2 结果（2026-11-21，分支 `go-perf-t2`）

设计：`server/design/o10-t2-marshal-validation.md`。完整校验只在对外 `Marshal` 的最终字节上做一次（`jsontext` 单次
扫描）；内部编码单元只校验外来 `Marshaler` 字节；原校验点保留零分配的深度计数（10000 层上限不满足组合性）；
解码不再经 `encoding/json` 转手。对外行为由旧实现录制的 9249 条参照语料、两个新旧对照 fuzz 与输出合法性 fuzz 固定。

o10perf（Go-only，.64，空闲 2 min + 每用例 10 次；.64 当时有其他负载，延迟只作量级参考，分配可靠）：

| 项 | 前 | 后 |
|---|---:|---:|
| 负载期全部分配 | 86.3 GB | 43.5 GB |
| 其中 `model.validJSON` | 32.4 GB（37%） | 0.25 GB（0.6%） |
| 负载期 CPU（120 s profile） | 127.8 s | 51.8 s |
| 空闲分配速率 | 22.7 GB/min | 10.2 GB/min |
| GET /api/agents 分配 / CPU | 681 MB / 974 ms | 319 MB / 368 ms |
| GET /api/agents/<id> | 510 MB / 733 ms | 246 MB / 277 ms |
| GET /api/config/<p>/surge·loon·mosdns | 121–252 MB / 173–357 ms | 54–58 MB / 60–69 ms |
| GET /api/config/<p>/oxidns | 161 MB / 231 ms | 76 MB / 99 ms |
| GET /api/config/<p>/mihomo | 662 MB / 883 ms | 354 MB / 390 ms |
| GET /api/agents/<id>/rule-sync | 61 MB / 91 ms | 7.8 MB / 14 ms |
| GET /api/notifications | 1003 MB / 1394 ms | 460 MB / 504 ms |

基准 `BenchmarkO10T2AgentList`（3 个 Agent × 60 条部署，.64）：编码 10.0 ms / 12.9 MB / 180k 次分配 → 2.9 ms / 5.8 MB / 27k；
解码 41 ms / 45 MB / 573k → 16.6 ms / 22 MB / 143k；单次校验 0.68 ms / 347 KB → 0.13 ms / 0 B。

剩余热点：`DecodeRecord` 每层按字段算 R2 基线（编码整棵子树）仍是解码的主要开销；与 T1/T4（不再整份拷贝 Agent）
叠加后收益更大，暂不单独处理。

## 未覆盖 / 留给后续

- 24 h 浸泡（RSS/goroutine/fd 趋势）：实验网 L6。本测 30 min 内无增长趋势。
- Sub-Store 进程内存（生产约 100 MB）：两端都禁用，未测；Go 版仍按需启动，策略未变。
- 真实设备在线时的规则同步/DNS 跟随/HA 发布开销：断网下这些循环都在失败分支；L6 用实验网设备复测。
- 每路由延迟受后台刷新尖峰干扰；T3/T5 之后重测会更干净。

## 复现

```bash
# 完整对比（Go + Python，30 分钟空闲 + 负载），输出在测试服务器私有目录
RT_HOST=sunbroke@192.168.0.32 rt -- server/tools/o10perf/run.sh --idle-minutes 30 --iterations 10
# 只测 Go
rt -- server/tools/o10perf/run.sh --servers go --idle-minutes 12
# 只要空闲 profile
rt -- server/tools/o10perf/run.sh --servers go --idle-minutes 11 --idle-only
# 合成规模生成基准
rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -run XXX -bench BenchmarkMihomoGenerationProfile/128 -benchmem ./internal/convert'
```

分析：`go tool pprof -sample_index=alloc_space -base go-before-load-allocs.pprof go-after-load-allocs.pprof`
（负载期分配），`go tool pprof go-idle-cpu.pprof`（空闲 CPU）。
