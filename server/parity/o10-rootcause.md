# O10 性能根因严查（2026-11-21）

分支 `go-perf-rootcause` = `go-perf-t1@da5f07df4` + T2（3 个提交，cherry-pick 无冲突；任务说“4 个”，`go-perf-t2` 上
相对 T1 基点只有 3 个新提交）+ 本任务的工具/实验提交。主测量（run3）在 rebase 之后跑；run1/run2 在 rebase 前
（`59d240aab` + T2）跑，两者只差 da5f07df4（路由器脱敏凭据缓存），非路由器数字几乎一致。

## 结论（先看这里）

**协调者裁定（2026-11-21）：**
1. 采用 S1 + S2 + S3，按下文 R1–R12 的顺序推进。
2. 生成器路由（Mihomo/provider）的分配目标先定为 **≤ 30 MB/次**，不加生成结果缓存：延迟已优于 Python，
   不值得承担缓存失效的风险。R9 之后若仍超标再议。
3. 生产主机内核不提供 THP（没有 `/sys/kernel/mm/transparent_hugepage`，协调者只读确认），
   所以 R12 不考虑 THP，只看 GC。测试服务器上 THP 造成的 RSS 峰值不计入验收。

**按现在的数据模型，第一批 + 第二批（T1–T11）全部做完也达不到验收目标。** 能达到的是：空闲 CPU 平均 ≤ 1%
（靠 T3+T5 消掉刷新尖峰）、空闲结束 RSS ≤ 117 MB（现在就是 106 MB）、Mihomo/provider 延迟（现在就比 Python 快
3–4 倍）。达不到的是：

1. **稳态底线 ≤ 0.5%**：现在 0.7%（Python 0.3%）。剩下的主要是 5 s 一轮的 Agent 规则同步 worker 每轮解码含历史
   的 Agent 记录，不在 T1–T11 里。
2. **每路由中位延迟 ≤ Python**：计划里的 T3/T4 只覆盖通知和 Agent 列表/详情。**路由器 5 条读路由、`/api/profiles`、
   `summary=1`、心跳、Agent rule-sync** 都没有任务，而且是同一种模式：路由器 policy 71 ms（Python 13 ms），
   policy-settings 76 ms（16 ms），upstream-routers 85 ms（34 ms），`/api/profiles` 15 ms（1.3 ms），
   `summary=1` 38 ms（3.5 ms）。
3. **读路由分配 ≤ 几 MB**：点修做不到。即使所有拷贝都去掉（实验，见下），`/api/agents` 仍是 175 MB，
   通知 113 MB。根因有三层，点修只碰到第一层：
   - **(a) 读方要拿“自己的副本”**：repo/agents 的读 API（`Config`、`Profile`、`Shared`、`ReadAgents`、`ReadAgent`、
     `ListAgents`）用 JSON 往返深拷贝。快照里 Agent 占文档 442 KB 中的 304 KB（部署/IPv6 校验历史），所以
     碰一次 Agent 就要拷 300 KB。
   - **(b) 消费方的投影都是“整条编码 → 通用解码 → 再处理”**：公开视图、凭据收集（`redact.CollectCredentials`）、
     任务行（`rulesync.safeText`）、`recordMembers(System)` 都对整条记录重编码再解码成 map。
   - **(c) model 编解码本身放大极高**：基准里解码 **9.5 MB/s、每字节输入分配 147 B**，编码 53 MB/s、38 B/B。
     原因是 `DecodeRecord` 对每一层的每个成员都立即 `encodeUnit` 算 baseline（`internal/model/record.go:180`），
     嵌套 d 层的子树被重复编码 d 次。因此“少拷一次 300 KB”能省 40 MB，“多拷一次”也就多 40 MB。
     Python `json.loads` 同样数据约 2–3 ms。

**推荐方案（结构性，三步）：**

- **S1 读方共享不可变快照，写方才拷贝。** repo 已经有按 store head 缓存的类型化快照（`View`、`ViewAgents`），
  写事务也已经从行数据解码出自己的工作副本（`transaction.go:128`）。所以不需要重做存储，只要把读 API 的默认语义从
  “返回副本”改成“借用只读视图”，删掉读路径上的 `copyRecord`/`copyValue`/`copyAgent`。实验证明这一步单独就能把空闲底线降到
  0.3%（= Python），分配降 20–60%。
- **S2 按输入版本缓存派生结果**：键为 (repo head, 任务状态版本, 心跳/在线版本, 缓存文件 mtime)。缓存对象包括
  Agent 公开投影、凭据集合（T1 已对路由器做了一个）、内容源、通知快照、订阅节点缓存解析结果、工作区 peers。
  命中时路由只做拼接，分配降到 KB–MB 级。
- **S3 编解码降放大**：baseline 改成惰性计算，或者用廉价指纹代替立即重编码；编码改为流式写入同一缓冲
  （`MarshalJSONTo`），不再逐层返回 `[]byte`。这一步决定写入、冷加载以及所有“必须编码整条记录”的响应
  （`/api/agents` 全量 329 KB、详情）能否到几 MB。

点修 vs 结构：点修每条路由都要重新判断“它到底需要哪些成员”，拷贝 API 仍是默认，新代码会继续用它（batch-6 的回退就是这么来的）。
S1 改的是默认值，便宜的路自然成为默认。建议 **S1 先行（替代 T4/T10 后续及新增的路由器任务里“去拷贝”那部分），
T3/T5 照常做但基于 S1，S2/S3 紧随**。按收益排序的任务清单见末节。

## 测量

- 工具：`server/tools/o10perf/`。本任务新增 `--route-profile-seconds N`：每个路由标签循环请求 N 秒，期间抓 CPU profile，
  前后各抓一次 allocs 做差分；`--only`；路由器读路由（列表、policy、policy-settings = workspace、rule-sync、
  `/api/agents/<id>/upstream-routers` = pending）；采样 AnonHugePages。
- 主机 .64（16 核）。**负载提示**：run3 期间 loadavg 8–10（其他 agent 在跑测试），run5 时一度很高。
  所以延迟只作量级参考，分配不受负载影响，是硬数据。Go 与 Python 同时运行、同一快照、各自私有副本，断网命名空间。
- 运行：run1 Go+Py 12 min 空闲 + 负载 + 每路由 20 s（rebase 前）；run2 Go 实验（浅拷贝）；
  **run3 Go+Py（rebase 后，主数据）**；run4 Go 空闲 `GODEBUG=disablethp=1`；run5 Go 实验，只跑路由器/Agent/通知。
  私有输出在 .64 `~/cf-prod-snapshot/runs/o10-20261121-{163203,165830,171819,174018,175139}`。

### 当前底数（run3，Go = T1@da5f07df4 + T2）

| 指标 | Go | Python | 目标 |
|---|---:|---:|---|
| 空闲 CPU 平均（11.8 min） | **4.82%** | 1.07% | ≤ 1% |
| 空闲 CPU 稳态（10 s 窗口中位 / p10） | **0.7% / 0.5%** | 0.3% / 0.2% | ≤ 0.5% |
| 空闲尖峰（10 s 窗口，每 300 s 一次） | 121% | 21% | — |
| 空闲分配（平均 / 稳态中位） | 35 MB/s / 4.2 MB/s | — | — |
| 空闲 RSS 结束 / 最大 | **107 / 190 MB** | 123 / 123 MB | ≤ 117 |
| 负载后 RSS（即刻 / 120 s 后） | 230 / 187 MB | 149 / 145 MB | — |
| GC 后存活堆（空闲 / 负载后） | 18 / 34 MB | — | — |

| 路由（run3 中位，10 次） | Go 延迟 | Go CPU/次 | Go 分配/次 | Py 延迟 | Py CPU/次 | 响应 |
|---|---:|---:|---:|---:|---:|---:|
| GET /api/notifications | 256 ms | 365 ms | 263 MB | 38 ms | 38 ms | 23 KB |
| GET /api/agents | 239 ms | 321 ms | 221 MB | 35 ms | 38 ms | 329 KB |
| GET /api/agents?history=0 | 238 ms | 331 ms | 224 MB | 67 ms | 157 ms | 22 KB |
| GET /api/agents?summary=1 | 38 ms | 53 ms | 33 MB | 3.5 ms | 3 ms | 22 KB |
| GET /api/agents/<id>（最大者） | 130 ms | 181 ms | 138 MB | 23 ms | 26 ms | 238 KB |
| GET /api/agents/<id>/rule-sync | 11 ms | 24 ms | 15 MB | 10 ms | 10 ms | <1 KB |
| POST …/heartbeat | 3.0 ms | 8 ms | 2–6 MB | 2.1 ms | 16 ms¹ | <1 KB |
| GET /api/profiles | 15 ms | 22 ms | 11 MB | 1.3 ms | 1 ms | <1 KB |
| GET /api/config/<p>/surge·loon | 13.6 ms | 18 ms | 12 MB | 11.2 ms | 11–15 ms | 13 KB |
| GET /api/config/<p>/mosdns | 13.0 ms | 19 ms | 14 MB | 15.6 ms | 16 ms | 13 KB |
| GET /api/config/<p>/oxidns | 26 ms | 36 ms | 21 MB | 33 ms | 35 ms | 18 KB |
| GET /api/config/<p>/mihomo | 149 ms | 453 ms | 170–358 MB² | 544 ms | 541 ms | 22 KB |
| GET …/aggregations/<agg>/provider | 221 ms | 289 ms | 206 MB | 868 ms | 596 ms | 79 KB |
| GET /api/routers | 16.5 ms | 22 ms | 15 MB | 6.2 ms | 6 ms | <1 KB |
| GET /api/routers/<id>/policy | 71 ms | 103 ms | 70 MB | 12.8 ms | 14 ms | <1 KB |
| GET /api/routers/<id>/policy-settings | 76 ms | 109 ms | 76 MB | 16.1 ms | 19 ms | 6 KB |
| GET /api/routers/<id>/rule-sync | 16.6 ms | 23 ms | 15 MB | 6.5 ms | 7 ms | <1 KB |
| GET /api/agents/<id>/upstream-routers | 56–85 ms | 120–143 ms | 85–102 MB | 13–34 ms | 12–36 ms | <1 KB |

¹ Python 心跳 CPU 是 10 次差值，含后台线程，偏高。² 两个配置空间差两倍，取每路由 profile 的平均值 169 MB。
快照默认配置空间只有 1 个路由器，所以路由器各项都是单个路由器的数。

### 实验：去掉读路径深拷贝（只在 `go-perf-rootcause` 的 EXPERIMENT 提交里，`O10_EXPERIMENT_SHALLOW=1` 才生效）

`copyRecord`/`copyValue`/`copyAgent` 改成共享缓存快照（浅拷贝结构体），模拟 S1。这样做不安全，只用于测量。

| 项 | 现状 | 浅拷贝 | 变化 |
|---|---:|---:|---|
| 空闲稳态 CPU（窗口中位） | 0.6–0.7% | **0.3%** | = Python |
| 空闲稳态分配 | 3.8–4.2 MB/s | **0.5 MB/s** | −88% |
| 空闲尖峰 | 111–121% | 66% | 通知单价降了一半 |
| /api/notifications 分配 | 263 MB | 113 MB | −57% |
| /api/agents 分配 | 221 MB | 175 MB | −21%（其余是投影，见预算） |
| /api/agents/<id> 分配 | 86 MB（平均） | 66 MB | −23% |
| surge 分配 / 延迟 | 12 MB / 11 ms | 6.5 MB / 6.2 ms | −46% |
| 路由器 policy / policy-settings | 70 / 76 MB | 34 / 29 MB | −52% / −62% |
| 路由器 rule-sync / upstream | 15 / 101 MB | 9 / 77 MB | −40% / −24% |
| 心跳分配 | 1.9 MB | 0.5 MB | −74% |
| mihomo / provider | 169 / 204 MB | 164 / 189 MB | 基本不变（成本在别处） |

异常一处：浅拷贝下 `GET /api/routers` 的分配反而从 15 MB 升到 29 MB（run5，单次），未追查。猜测是浅拷贝让凭据缓存键发生变化，
但这只是猜测。实验结论不依赖它。

### model 编解码基准（`BenchmarkO10T2AgentList`，3 个 Agent × 60 次部署，150 KB，.64）

| | 速度 | 分配/次 | B/B | 对象/次 |
|---|---:|---:|---:|---:|
| 编码 `model.Marshal` | 2.8 ms（53 MB/s） | 5.75 MB | 38 | 27k |
| 解码 `json.Unmarshal → DecodeRecord` | **15.8 ms（9.5 MB/s）** | **22 MB** | **147** | 143k |
| 校验 `validJSON`（T2 后） | 0.12 ms（1.2 GB/s） | 0 | 0 | 0 |

一次“深拷贝 300 KB Agent 列表” ≈ 37 ms CPU + 57 MB。这就是各路由里 `copyAgent` 每出现一次就要 40–60 MB 的原因。

## 1. 空闲：后台循环逐项对照

空闲 CPU profile（run3，420 s，总样本 16.2 s = 3.85%；run1 同形）。断网下规则源/订阅每 300 s 全部失败重试一次。
生产中它们按各自间隔运行（默认 1 天），成功时同样写任务状态，所以“每次写状态的单价”在生产里也存在，只是频率低得多。

| 循环 | 周期 | Go 每轮（CPU） | Go 在 420 s 内 | Python 同名任务实际工作 | 判定 |
|---|---|---:|---:|---|---|
| scheduled-refresh：`due()` | 30 s | ≈11 ms | 0.16 s | 同样读 shared + 任务状态 + 缓存时间 | 量级可接受；`SourceCacheAt` 为读一个时间戳解码整份订阅缓存（见 T-R9） |
| scheduled-refresh：失败 → `recordStatus` → **完整通知快照** | 每资源每次刷新 | ≈170 ms × 46 | **8.0 s（50%）** | 同样逐条刷新，单价 ≈40 ms | **Go 干活多**：单价 ×4–6；T3 降单价，T5 每轮合并成 1 次 |
| agent-rule-sync `Round` | 5 s | ≈5 ms | 0.41 s | 读 dict | Go 多：T10 已收窄，仍有 `RuleSyncAgents` 拷凭据 |
| agent-rule-sync **worker**（每个合格 Agent） | 5 s | ≈14 ms | **1.16 s** | 离线时只比较状态 | **Go 干活多**：`workerAgent`/`ReadAgent`/`AgentPair`/`ReadHAPair` 每轮深拷贝含历史的 Agent，不在 T1–T11 里 |
| domain-discovery-auto | 600 s（首次 120 s） | ≈3 s（窗口差） | 0.70 s | 同样逐配置空间跑，≈1.4 s | Go 约 2 倍，包含一次 `UpdateProfile` 写（整文档解码） |
| oxidns-ha-publish（IdleRounds） | 2 s | <1 ms | 0.12 s | 同 | 正常 |
| dns-follow（IdleRounds） | 5 s | ≈1 ms | 0.09 s（run1） | 同 | 正常 |
| ipv6-verification-scheduler | 5 s | ≈1 ms | 0.12 s | 同 | 正常 |
| router-rule-sync / metrics flush / rankings | 30/30/60 s | 可忽略 | <0.05 s | 同 | 正常 |
| 每次 repo 读打开新 SQLite 连接（`store.connect`） | 每次读 | — | 0.44 s | Python 也按调用连接 | 小；心跳路由里占 11% CPU（T-R10） |
| GC（`gcBgMarkWorker`）+ 清扫 | — | — | 3.9 + 0.4 s（27%） | — | 随分配下降 |

浏览器开着时（不在上面的“空闲”里，但生产常见）：通知中心每 30 s 拉 `/api/notifications`（Go 365 ms CPU，持续占
1.2%；Python 0.13%）。Agents 页每 10 s 刷新（`history=0` 331 ms CPU，持续占 3.3%；Python 1.6%）。
这两项也必须按路由目标修，否则“打开页面后空闲”比 Python 差 3–10 倍。

估算（假设 T3 把通知降到 ≤ 40 ms，T5 每轮只刷 1 次）：尖峰从 12 CPU-s/300 s 降到 < 0.2 s，空闲平均 ≈ 稳态 + 0.1%。
**稳态 0.7% 要到 ≤ 0.5%，还需要 rule-sync worker 不再拷贝**（实验显示做完是 0.3%）。

## 2. 读路径：不可变快照方案评估（S1）

**现状。** `Repository` 缓存一份已校验的类型化文档（按 store head，`repository.go:79`）。`View`/`ViewAgents` 直接借给读方，
文档注释已规定“只读、不保留”。写事务从行数据解码自己的工作副本，提交后换缓存指针。
也就是说，copy-on-write 的写方和共享的不可变快照**已经存在**。问题在于上层读 API 为了“调用方可以随便改”而逐次深拷贝：

| API（非测试调用点：文件 / 次） | 拷贝内容 |
|---|---|
| `agents.Manager.ReadAgent` 30 / 59，`ReadAgents` 12 / 13，`ListAgents` 4 / 5，`AgentPair`/`ReadHAPair` 3 / 3，`ReadAgentByToken` 1 / 1 | `copyAgent`：整条 Agent 编码 + 解码（`history=true` 时含历史） |
| `repo.Config` 24 / 26，`ConfigWithOxiDNSAgents` 9 / 9，`PublishConfig` 14 / 16，`ConfigFrom` 2 / 2 | `resolveConfig`：`copyRecord(profile)` 整个配置空间 + `copyValue` SystemConfig/Backup/RuleLibrary + 每个选中资源 |
| `repo.Shared` 15 / 20，`Profile` 13 / 16，`SystemConfig` 10 / 10，`Backup` 2 / 2 | `copyRecord` |
| 合计约 **95 个文件、≈180 个调用点**；另有 `internal/` 下 246 处 `model.Marshal`/`.MarshalJSON()`（T7 的清扫对象，S2/S3 后多数自然消失） | |

**改法。**
1. 读 API 改成借用：`ViewConfig(ctx, id, func(*model.Config) error)`、`ViewAgent(ctx, id, fn)`，或者返回带“只读”约定的值。
   `resolveConfig` 构造的 `Config` 引用快照里的记录，只有被水合的 ruleset 用新切片。
2. 真要修改的调用方（例如把离线 Agent 的 `status` 改成 offline、心跳 overlay、生成器内部改写）对**结构体做浅拷贝**再改。
   `Raw`/`Fields`/`Map`/`Dynamic` 本来就是 copy-on-write（`record.go:21` 注释），所以 `SetJSON`/`Clear` 是安全的。
   不能做的是往借来的切片里 `append`/按下标赋值：需要时 `slices.Clone`，代码评审逐个看。
3. `PublishConfig` 等确实要交给写事务或长期保留的路径，继续用显式的 `Clone*`（名字写明代价）。

**并发安全怎么保证。**
- 快照在生命周期内不可变：提交只换指针，不改旧快照（现有保证）。
- **违约检测已有现成钩子**：`Repository.viewGuard` 会在 `View`/`ViewAgents` 前后各 `model.Marshal` 一次并比较字节
  （`read.go:56`、`agents_view.go:50`）。把它在所有包的测试里默认打开（测试 helper 注入），借用 API 也走同一个 guard，
  任何改了共享快照的测试都会失败。
- `-race`：每个迁移过的读 API 加“N 个并发读者 + 1 个写者循环提交”的测试。读者在借用期间遍历全部成员，
  写者在每次提交后断言旧快照编码不变。用 `go test -race -count=20` 跑受影响包。
- 门禁：`TestConcurrencyFacadeBoundary` 那类源码检查，禁止 `internal/api/**` 和后台循环直接调用 `copyAgent`/`copyRecord`。

**风险。** 违约的后果是静默的：改了共享快照，所有读者都看到，直到下一次提交才恢复，而且不会写回磁盘。
需要靠上面的 guard 和 race 测试把关。生成器内部原地改写（例如 `resolveConfig` 的 ruleset 水合、`convert` 里改节点）
最容易漏，实验里就碰到一次（已用 `slices.Clone` 规避）。工作量：Agent 侧约 90 个调用点，L（3–4 个执行者日）；
Config/Shared/Profile 侧约 75 个调用点，M（2–3 日）；guard、race 和门禁 S。可以分包推进，每包独立验收。

**与逐路由点修比较。** 点修（T4 那种“按成员读”）在单条路由上能做到同样的效果，但要覆盖目前已知的 19 条读路由和 5 个循环，
工作量不比 S1 小，而且不能防止新代码再用拷贝 API。S1 之后剩下的点修只有 (b) 类投影（S2）。**建议走 S1。**

## 3. 预算拆解（目标：延迟 ≤ Python，分配几 MB）

分配占比来自 run3 每路由 allocs 差分（cum，同一函数可能被父项包含，所以加和可以超过 100%）。
CPU 中 GC 约占 25–28%，随分配同比下降。

**GET /api/agents（全量，3 个 Agent，响应 329 KB）**：现 239 ms / 221 MB → 目标 ≤ 30 ms / ≤ 3 MB（命中缓存 ≈ 1 ms / < 1 MB）

| 段 | 现分配 | 证据 | 目标 |
|---|---:|---|---|
| `publicAgentWithCredentialsModel`：整条编码 → `CollectCredentials` 通用解码 → 解成 map → 每个部署再解码成 `model.Deployment` → `publicDeploymentModel` 再编码 | 42%（≈92 MB） | `public_agent_adapter.go:18–58` | 类型化投影一次写进响应缓冲；按 (head, agent, overlay) 缓存（S2）；≈1 MB / 3 ms |
| `deploy.NewTypedTarget`（整条编码） | 19% | `content_routes_adapter.go:83` | 读成员（T4）；≈0 |
| `copyAgent`（`ReadAgents`） | 19% | registry.go:31 | S1：0 |
| `PublicDeploymentMessagesModel` | 11% | 消息脱敏逐条整编码 | 投影缓存内；0 |
| `contentsync.Status` / 内容源（每个 Agent 解一遍配置） | 10% | `loadContentSource` | 按 (head, profile) 缓存一次（T4）；0 |
| `CollectCredentials` 独立部分 | 5% | | 凭据集合按 head 缓存（T1 做法扩展）；0 |

**GET /api/notifications（23 KB）**：现 256 ms / 263 MB → 目标 ≤ 30 ms / ≤ 3 MB（快照按输入版本缓存后 ≈ 1 ms）

| 段 | 现分配 | 目标 |
|---|---:|---|
| `copyAgent` 合计（`ListAgents` 31% + `NotificationAgents` 15% + 行构建器各自读） | **47%** | S1：一次借用，0 |
| `dnsfollow.agentTaskRow` | 24% | 读成员；≈0 |
| `rulesync.safeText`（整条 Agent 编码收集凭据） | 17% | 按 head 缓存凭据集合；0 |
| `redact.CollectCredentials`（合计） | 15% | 同上 |
| `resolveConfig`（发现/规则行） | 11% | S1：0 |
| `discovery.domainRows` | 7% | S1 后复查 |
| `refresh.SubscriptionTasks` + `substore.LoadCache`（为一个时间戳解码整份订阅缓存） | 6% | 按 (路径, mtime, size) 缓存时间戳；0 |
| `notifications.transaction`（持久化回执） | 5% | 无变化时不重写；≤ 0.5 MB |

**GET /api/config/<p>/surge·loon·mosdns（13 KB）**：现 13.6 ms / 12 MB → 目标 ≤ 8 ms / ≤ 3 MB

| 段 | 现分配 | 目标 |
|---|---:|---|
| `resolveConfig`（`copyRecord(profile)` 等） | 63% | S1：0（实验：6.5 MB / 6.2 ms） |
| `convert.SurgeConfig` 自身 | 36% | 其中 `clientPreflight` 13%（逐规则整编码，T7）；≈2 MB |

**GET /api/config/<p>/mihomo（22 KB）**：现 149 ms / 169 MB（延迟已 < Python 544 ms）→ 目标 延迟维持，分配 ≤ 30 MB；按输入版本缓存结果后命中 ≈ 1 ms

| 段 | 现分配 | 目标 |
|---|---:|---|
| `provider.PrepareBundle` → `refresh.FetchSource` → `substore.LoadCache`（订阅节点缓存每次用 model 编解码器整份解码）+ `readProviderNodes` | 33%（LoadCache 11%、readProviderNodes 11%） | 节点缓存按 (路径, mtime, size) 缓存类型化结果（新 T-R9）；≈0 |
| `yamlx.Marshal`（`prepareStrings` 16%、`preserveScalarTag` 11%、`copyGraph` 7%、`checkGraph` 7%） | 31% | T8 单次编码，不逐值往返；≈5 MB |
| `aggregations.providerYAML` | 15% | 同 T8 |
| model 编解码（`Node` 解码 12%） | — | S3 |
| `resolveConfig` | 5% | S1 |

**GET …/aggregations/<agg>/provider（79 KB）**：现 221 ms / 206 MB（Python 868 ms）→ 分配 ≤ 30 MB。构成与 mihomo 同：
`PrepareBundle` 28%、`yamlx.Marshal` 23%、`providerYAML` 13%、`readProviderNodes` 12%、`LoadCache` 9%、`resolveConfig` 4%。

**路由器读路由**（T1 交回的 `/api/routers` 28 MB → 现 15 MB，da5f07df4 起作用；policy 157 → 70 MB）：

| 路由 | 现 → 目标 | 主要段（分配占比） |
|---|---|---|
| GET /api/routers | 16.5 ms / 15 MB → ≤ 6 ms / ≤ 1 MB | `ManagementService.Records` 76%（其中 `ConfigWithOxiDNSAgents → resolveConfig` 52%）；`public` 的 `CollectCredentials` 21% |
| GET …/policy | 71 ms / 70 MB → ≤ 12 ms / ≤ 2 MB | **`workspacePeers → ListAgents → copyAgent` 59%（含历史！）**；`Records`/`resolveConfig` 32% |
| GET …/policy-settings | 76 ms / 76 MB → ≤ 15 ms / ≤ 2 MB | `workspacePeers` 53%；`ReadWorkspace` 25%（`oxidns.ProfileFromJSON` 20%，JSON 往返）；`Records` 15% |
| GET …/rule-sync | 16.6 ms / 15 MB → ≤ 6 ms / ≤ 1 MB | 同 `/api/routers`（`Records`） |
| GET /api/agents/<id>/upstream-routers | 56–85 ms / 85–102 MB → ≤ 13 ms / ≤ 2 MB | `allProfileSecrets` 55%（每个配置空间 `Config` + `recordMembers`）；`pendingTargets` 36%；`agentBindingRecords` 16% |

**其他同模式路由**：`/api/profiles`（11 MB，**98% 在 `repo.agentBindingRecords` → `recordMembers` 整个 System 编码**，只为读每个
Agent 的 profile_id）→ ≤ 1 ms / < 0.2 MB。`summary=1`（33 MB；`CollectCredentials` 42%：summary 模式也对含历史的原记录整编码收集凭据，
`content_routes_adapter.go:101`）→ ≤ 3 ms / < 1 MB。心跳（`ReadAgent` 深拷贝 + `store.connect` 11% CPU）→ ≤ 2 ms / < 0.3 MB。

## 4. RSS

- 空闲结束 RSS **106–107 MB < Python 123 MB**（run1/3/4 一致）。超标的是**峰值和负载后**：尖峰时 190 MB，
  负载后 230 MB，120 s 后仍 187 MB（Python 负载后 145–149 MB 且不回落）。
- 构成（run4，THP 关）：RSS − (heap_sys − heap_released) ≈ **57 MB 非堆**（二进制 text/data、运行时元数据、栈）。
  保留堆 ≈ 48 MB，其中存活 18 MB；heap_inuse 39 MB 对 heap_alloc 18 MB，span 内空洞约 20 MB；未归还的空闲堆约 9 MB。
  所以碎片不是主因，**主因是分配速率**：尖峰期间并发标记跟不上，堆目标被冲高，scavenger 归还又有滞后。
- **THP**：两台测试服务器都是 `transparent_hugepage=always`、`max_ptes_none=511`。khugepaged 会把 scavenger 已归还的稀疏区域重新合并成大页。
  run3 的 AnonHugePages 在尖峰后最高 117 MB；run4 用 `GODEBUG=disablethp=1` 后峰值 RSS 从 190 降到 165 MB，结束值不变。
  这是测试机的环境因素：生产主机内核不提供 THP（已裁定），所以 THP 关闭的 run4 更接近生产。
  不调 GOGC 掩盖。
- 判断：存活堆 18–34 MB，按 GOGC=100 的稳态上限 ≈ 57 + 2×34 + 少量 ≈ 125–130 MB（负载后），空闲 ≈ 57 + 2×18 ≈ 95–110 MB。
  **降分配（S1+S2+T3/T5）后空闲 RSS 可以稳定 ≤ 117 MB，峰值也会落进来**。负载后能否 ≤ 117 取决于存活堆：负载后的
  34 MB 里有 provider last-good 缓存等，S2 引入的缓存要计入预算（建议派生缓存总量 ≤ 10 MB，并按 head 只保留一代）。
  做完再评估 T9。

## 5. 任务清单（按收益排序；每项的“依据”是本报告的测量）

| # | 任务 | 预计收益 | 依据 | 量 |
|---|---|---|---|---|
| R1 | **S1-Agent：Agent 读 API 借用快照**（`ReadAgent`/`ReadAgents`/`ListAgents`/`AgentPair`/`ReadHAPair`/token 读；rule-sync worker、通知、路由器 workspacePeers、心跳随之受益）+ guard/race/门禁 | 空闲稳态 0.7→0.3%、稳态分配 −88%；通知 −57%；policy/settings −52/−62%；心跳 −74% | 实验 run2/run5 | L |
| R2 | **S1-Config：`resolveConfig`/`Shared`/`Profile` 借用**（只有水合 ruleset 用新切片；`PublishConfig` 保留显式 Clone） | surge/loon/mosdns −46%、≈6 ms（< Python）；`/api/routers`、router rule-sync、upstream −40–55% | 实验；route01/14/18 profile | M |
| R3 | **T3+T5 基于 R1**：行构建器按成员读；凭据集合按 head 缓存；一轮刷新只出一次快照；快照按 (head, 任务状态版本, 在线版本) 缓存 | 空闲平均 4.8→≈0.5–0.8%；通知 256 ms→≤ 30 ms；浏览器常驻开销 1.2%→< 0.1% | 空闲 profile 50% 在此；route09 | M |
| R4 | **T4 扩展为 Agent 公开投影 S2**：类型化投影直接写入响应，按 (head, agent, overlay) 缓存；`summary=1` 不再整编码收集凭据；`NewTypedTarget`/内容源按需 | `/api/agents` 221→≤ 3 MB、≤ 30 ms；`history=0` 同；`summary=1` 33→< 1 MB；详情同类 | route06/08/11 | M |
| R5 | **S3 编解码**：`DecodeRecord` 惰性 baseline（`record.go:180`）；流式编码 `MarshalJSONTo`；对照旧实现做 fuzz | 解码目标 ≥ 50 MB/s、≤ 10 B/B（现 9.5 MB/s、147 B/B）；编码 ≤ 3 B/B（现 38）；写入/冷加载/大响应分配降一个数量级 | 基准；model.* 占各路由 40–60% | L，需设计复核 |
| R6 | **路由器读路由**：`workspacePeers` 借用且只取所需成员；`allProfileSecrets`、`pendingTargets` 按 head 缓存；`ReadWorkspace`/`oxidns.ProfileFromJSON` 去 JSON 往返 | 5 条路由到 Python 同量级、≤ 2 MB | route14–18 | M（R1/R2 后剩余部分 S） |
| R7 | **`/api/profiles` 与 profile bindings**：`agentBindingRecords` 不再 `recordMembers(System)`，直接读各 Agent 的 profile_id | 15 ms/11 MB → ≤ 1 ms；upstream-routers −16% | route10 98% | S |
| R8 | **订阅节点缓存与时间戳**：`substore.LoadCache` 结果按 (路径, mtime, size) 缓存；`SourceCacheAt` 只取时间戳 | mihomo/provider −20%；通知 −6%；`due()` 每轮更省 | route00/05/09 | S |
| R9 | **T8 YAML 单次编码 + T7 规则整编码清扫** | mihomo/provider 再 −30–40%，到 ≤ 30 MB | route00/05 yamlx 23–31% | S–M |
| R10 | **SQLite 读连接复用**：常驻只读连接 + `PRAGMA data_version` 判断，不再每次读都 `sql.Open` + 3 个 PRAGMA | 心跳 CPU −11%；全局小幅 | route13；空闲 0.44 s/420 s | S |
| R11 | **T11 防线**：每路由分配上限（本报告的目标列）+ viewGuard 默认开 + 门禁禁止读路径直接拷贝 | 防回退 | batch-6 前例 | S |
| R12 | **T9 GC**：R1–R5 后重测（生产无 THP，只看 GC） | 视降分配后的 RSS 而定 | run4 | S |
| 可选 | Agent 历史冷热分离（部署/IPv6 校验历史单独存行） | 只在 R1–R5 后 `/api/agents` 全量仍不达标时考虑；涉及存储格式，需要裁定 | 历史占文档 69% | L |

依赖：R1、R2 互相独立，可以并行；R3/R4/R6 建在 R1 上；R5 可与 R1/R2 并行（model 包，冲突面小）。
原计划 T4 → 并入 R4；T10 的后续 → 并入 R1；T7/T8 → R9；T9 → R12；T11 → R11。

**用 R1–R4 + R7 + R8 估算达标情况**（R5、R9 是分配目标的保险）：空闲平均 ≤ 1% ✓，稳态 ≈ 0.3% ✓，空闲 RSS ≤ 117 ✓；
表中所有路由延迟 ≤ Python ✓（Agent 全量/详情命中缓存时）；分配：读路由 ≤ 3 MB ✓，Mihomo/provider 要 R9 才能到 ≤ 30 MB。
“几 MB”对生成器路由不现实，已裁定为 ≤ 30 MB（不加结果缓存）。

## 6. 裁定记录

见“结论”开头的三条裁定（S1+S2+S3 按 R1–R12 推进；生成器 ≤ 30 MB、不加结果缓存；生产无 THP，R12 只看 GC）。

## 本任务提交

- 本分支 `go-o10-tools`（基于 `go`）只含测量工具和文档：`server/tools/o10perf/`（每路由 CPU profile 与 allocs 差分
  `--route-profile-seconds`、路由器读路由、`--only`、AnonHugePages 采样）、本报告、`o10-performance.md`（原审计）。
- 浅拷贝实验（`O10_EXPERIMENT_SHALLOW=1`）的两个 EXPERIMENT 提交只留在 `go-perf-rootcause`，不合并。

复现：

```bash
RT_HOST=sunbroke@192.168.0.64 rt -- server/tools/o10perf/run.sh --idle-minutes 12 --iterations 10 --route-profile-seconds 15
RT_HOST=sunbroke@192.168.0.64 rt -e GODEBUG=disablethp=1 -- server/tools/o10perf/run.sh --servers go --idle-minutes 11 --idle-only
# 浅拷贝实验只能在 go-perf-rootcause 分支上复现：rt -e O10_EXPERIMENT_SHALLOW=1 -- server/tools/o10perf/run.sh --servers go …
# 基准在 T2 分支（go-perf-t2）上：rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -run XXX -bench BenchmarkO10T2AgentList -benchmem ./internal/model'
# 分析：go tool pprof -sample_index=alloc_space -top -cum -base routeNN-allocs-before.pprof routeNN-allocs-after.pprof
```
