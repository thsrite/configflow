# O10 第一批（T1/T2/T3 + T5/T6/T10）HR 复核与整合（2026-11-21）

复核者：Opus 5.5，分支 `batch-21`（已 rebase 到 `go` bfd2daed3）。
范围：O10 审计小修复、T2（`model.Marshal` 单次校验）、T1/T6/T10（repo 窄读取 + 协调裁定）、
T3/T5（通知快照降本 + 一轮只刷新一次），以及本次整合与复核修复。

## 结论

- 正确性：未发现阻断问题。T3 与 T1 的冲突已按两条语义整合（见“整合”）；复核中自己 grep 了全部
  `repo.Config` 调用方、全部凭据收集点和所有缓存的事务/快照分支，结论见下。
- 性能：相对 `go` 所有被测路由都明显改善，**没有退步的路由**。空闲 CPU 34.9%（审计时）/50%（本次 go 基线）→ 2.0%。
  仍未达到目标：空闲 CPU 平均 2.0%（目标 ≤ 1%），空闲 RSS 169 MB（目标 ≤ 117 MB），
  `/api/agents*`、`/api/agents/<id>`、`/api/profiles`、心跳仍慢于 Python（属于 T4/T7/T9 范围）。
- 复核修复 1 个提交：两个空闲热点（HA 配对读取拷贝全部 Agent；探测结果合并 O(n²)）。修复后空闲 CPU 2.02% → 1.78%，
  空闲分配 730 → 616 MB/min，空闲窗口总分配 −28%（`LatestProbeEntries` 1047 → 180 MB，`ReadHAPair` 1213 → 862 MB）。

## 整合（提交 `go(perf): O10 wave A — integrate T3 with T1 …`）

1. `notifications.routerSignatures`（T3，按 store head 缓存）改读 `ConfigWithOxiDNSAgents`（T1）：
   缓存保留，摘要语义与 T1 一致——OxiDNS Agent 地址变化让检查过期，部署历史/token 变化不过期。
   T1 的表驱动摘要测试与 T3 的“编辑后立即过期”测试都通过；T3 测试里录检查用的配置同步改为投影视图。
2. 协调裁定：Agent 规则同步行、DNS 跟随行的脱敏集合与改前一致包含部署历史。两种行都改用
   `redact.AgentRecordCredentials`（仓库按 store head 缓存），一次任务行投影只读一次；去掉了 T3 的
   “无历史逐条收集”（`RowAgentCredentials`、`CollectAllAgentCredentials`）。规则同步行由“本条记录凭据”
   变为“全部记录凭据”，是改前集合的超集（只会多遮盖，不会少）。
   测试：T3 快照测试在部署历史里放一个只存在于历史的 password，并让两种行的消息含它，断言快照不泄漏；
   负向对照证明无历史收集找不到它（把实现临时换回无历史收集时该测试失败，已实测）。
3. rebase 到 `go`：`o10perf` 工具以 `go` 版本为准；`o10-performance.md` 以 batch-21 内容为基础保留 `go` 新增；
   T1 发布说明编号因 `go` 已占用 U16 改为 **U17**，第 5 步“按需重做”合并两边文字。

## 复核发现（按严重度）

### 中：空闲热点（已修复，复核修复提交）

- **M1 `agents.ReadHAPair` 为取两条记录拷贝全部 Agent。** 规则同步 worker 每次运行都经 `leader → AgentPair →
  ReadHAPair → ReadAgents`，对每个 Agent 做一次 `Marshal + Unmarshal`。本批空闲 CPU profile 中 `rulesync worker.run`
  占 25%、`ReadHAPair` 16%。修复：在存储视图上做配对检查（检查只读 `oxidns_ha`/`profile_id`，
  心跳 overlay 只改运行时字段），只拷贝两条成员，再套 overlay。所有 id 类别（成对、半对、非 HA、不存在）结果与旧实现逐字节相同；
  分配不随其他 Agent 数增长（负向对照：全量拷贝随之增长）。
- **M2 `health/check.LatestProbeEntries` / `ProbeStore.SaveEntries` 合并为 O(n²)。** 每个主机一次 `model.Map.Set`，
  而 `Set` 每次都整份 clone（copy-on-write）。域名发现自动轮次经 `LoadDomains → legacyProbes` 调用，
  占本批空闲分配 20%（12 分钟约 0.9 GB）。修复：新增 `model.Map.SetAll`（与逐个 `Set` 等价，只拷贝一次），
  合并先用 Go map 记最新条目再一次写入。顺序、已有键位置、未改字节与逐个 `Set` 相同（表驱动测试）；
  分配线性（250→2500 主机，负向对照：逐个 `Set` 超线性）。

### 低：记录但未改

- **L1 `routerSignatures` 缓存键与读取不在同一代。** 先读 head 再逐个 profile 读配置，期间若有提交，缓存会以旧 head
  为键存新内容。head 只前进，之后的读取都会因 head 不同而重算，不会给出旧值；held snapshot 内 head 与读取同代，
  事务内不走缓存。与 T3 之前逐 profile 读取的行为相同，不改。
- **L2 `RuleSyncAgents` 每 5 s 对每个 Agent 编码一次（无历史）收集凭据。** 量小（本批空闲 profile < 2%），
  可在 T4/T10 后续按 store head 缓存。
- **L3 剩余空闲开销**（修复后 profile）：`agents.copyAgent` 的 JSON 往返占空闲 CPU 27%，其中 62% 仍是 `ReadHAPair` 拷贝
  两条配对成员（生产快照里配对记录不含历史也较大；离线环境下 worker 每轮重试），其余为 `ReadAgent`/`ReadAgents`；
  域名发现自动轮次 20%（`LoadDomains`、`startJob`、`editSettings`）；GC 约 23%。让 `leader()` 只读不拷贝、
  或按 store head 缓存拷贝，属于 T4/T10 后续。

### 已核对、无问题

- **T2 单次校验不放宽：** `validJSON` = `utf8.Valid` + `jsontext.Value.IsValid`（语法、深度、每层重名，名字按反转义比较，
  孤立代理项与旧实现同样接受）；失败分类沿用旧实现。内部编码单元只信任本包编解码输出（入口均已校验：解码原文、`Fields.Set`、
  `SetMemberJSON`、`Dynamic`、`MosDNSMatch` 原文、`encoding/json` 标量），外来 `json.Marshaler` 仍在原检查点整体校验；
  `Marshal` 对最终字节完整校验一次；10000 层上限用 `tooDeep` 保持。逐个看了 model 包 97 个 `MarshalJSON`，
  都走 `EncodeRecord`/已校验原文。`decodeInto` 直调本包 `UnmarshalJSON` 前输入已被外层整体校验（含嵌套重名）。
  9249 条旧实现参照语料 + 两个新旧对照 fuzz 固定外部行为。
- **T1 调用方：** 自己 grep 了全部 `repo.Config`/`ConfigFrom` 调用（29 处）与所有读 `agents` 的代码。OxiDNS 包内只有
  `ProfileAgents`、`NewEntryContext` 读 Agent；其调用链（规则 DNS 读/写、设备策略、路由器、客户端设备、规则页、
  `withheld`）都已改用 `ConfigWithOxiDNSAgents` 或事务内 `agentCopyOxiDNS`。生成器 `PatchDocument(…, nil)` 不读 Agent；
  部署源 `deploy.SourceFromJSON` 本来就删除 `agents`；需要整条记录的（IPv6、网关安装/引导/草稿）用 `PublishConfig(…, true)`。
- **脱敏集合：** 逐个核对 `CollectCredentials` 调用点。原先从带 Agent 的配置收集凭据的路由器、路由器工作区、管理诊断、
  客户端设备响应已由 T1 裁定改用 `AgentRecordCredentials`；客户端设备发现的中间结果最终也经同一 `sanitize`。
  T10 规则同步轮次：改前 `AgentSnapshots(ctx, false)` 本就不含历史，集合未变。T3 行已按裁定恢复含历史。
- **缓存：** `AgentRecordCredentials`、sanitization tokens 在事务内读工作副本、held snapshot 内读快照、都不写缓存；
  缓存以实际读到的代的 head 存储，写后/回滚后 head 不同即重算。T3 快照内 Agent 列表缓存只在一次 `collect` 的 ctx 内共享。
- **T5 合并刷新：** `RunRound` 正常结束、`Stopping` 中途退出（`stop` 先 `wg.Wait()` 再刷新）都会刷新一次；
  上下文已 `Detach`，`RefreshTasks` 再次 `Detach`，关停时仍执行（与逐条钩子相同）；某个资源写回执失败只影响它自己，
  其他已写回执仍触发刷新；没有回执的轮次不刷新；手动/直接刷新仍逐条刷新。T5 测试固定 first_seen_at、occurrences、
  version、忽略状态与轮次中途的浏览器读取。
- **T6 设置保存：** `SaveSystemConfig/SaveBackup` 对单个成员做三方合并，与原 `SaveSystemRecord`（每次都带 baseline）
  对其他成员的合并结果相同（原合并对未改成员得到现值）。

## 性能（.32，`server/tools/o10perf`，Go-only，空闲 12 min + 每用例 10 次，同一快照）

测量时 .32 负载 < 2（开始前 1 分钟均值 0.06–1.8）；Python 数字取自 `o10-performance.md`（同快照、同机、审计时测）。
延迟为 10 次中位数，分配/CPU 为每请求。

### 空闲

| 指标 | go（49b2270e8，未含本批） | batch-21（复核修复前） | batch-21（复核修复后） | Python | 目标 |
|---|---:|---:|---:|---:|---:|
| 空闲 CPU 平均 | 50.2% | 2.02% | 1.78% | 0.9% | ≤ 1% |
| 空闲 CPU 峰值（10 s 窗口） | 419% | 32% | 33% | 23% | — |
| RSS 结束 / 最大 | 294 / 295 MB | 169 / 179 MB | 167 / 171 MB | 117 / 118 MB | ≤ 117 MB |
| 空闲分配速率 | 14.8 GB/min | 0.73 GB/min | 0.62 GB/min | — | — |
| GC 后存活堆 | 31 MB | 18 MB | 18 MB | — | — |

### 每路由（中位延迟 / 分配 / CPU）

“batch-21”列是复核修复前；修复后（同机、同工具）各路由在 ±10% 内，`mihomo` 分配 321 → 232 MB、
`rule-sync` 11.6 → 8.7 ms、`mosdns` 16.5 → 14.5 ms（后台干扰减少）。修复后新增的路由器路由（`go` 版工具）：
`/api/routers` 16 ms / 15 MB，`/policy` 68 ms / 71 MB，`/policy-settings` 79 ms / 76 MB，`/rule-sync` 17 ms / 15 MB，
`/api/agents/<id>/upstream-routers` 79 ms / 104 MB（审计表无 Python 对照，留给 PVE 测量 CT 补测）。

| 路由 | go | batch-21 | Python 延迟 | batch-21 ≤ Python？ |
|---|---:|---:|---:|---|
| GET /api/notifications | 2418 ms / 2072 MB / 3932 ms | 26 ms / 21 MB / 40 ms | 41 ms | 是 |
| GET /api/config/<p>/mihomo | 342 ms / 349 MB / 701 ms | 151 ms / 321 MB / 454 ms | 630 ms | 是（分配仍高，T7） |
| GET /api/config/<p>/surge | 147 ms / 184 MB / 410 ms | 13.3 ms / 12 MB / 20 ms | 13–19 ms | 是 |
| GET /api/config/<p>/loon | 145 ms / 155 MB / 340 ms | 13.8 ms / 12 MB / 20 ms | 13–19 ms | 是 |
| GET /api/config/<p>/mosdns | 166 ms / 338 MB / 604 ms | 16.5 ms / 23 MB / 39 ms | 13–19 ms | 持平 |
| GET /api/config/<p>/oxidns | 207 ms / 311 MB / 627 ms | 26.5 ms / 21 MB / 38 ms | 37 ms | 是 |
| GET …/aggregations/<agg>/provider | 957 ms / 854 MB / 1685 ms | 261 ms / 205 MB / 331 ms | 740 ms | 是 |
| GET /api/agents | 846 ms / 723 MB / 1392 ms | 291 ms / 219 MB / 388 ms | 35 ms | **否 ×8**（T4） |
| GET /api/agents?history=0 | 844 ms / 701 MB / 1342 ms | 272 ms / 225 MB / 392 ms | 44 ms | **否 ×6**（T4） |
| GET /api/agents?summary=1 | 78 ms / 57 MB / 121 ms | 47 ms / 33 MB / 64 ms | 4 ms | **否 ×12**（T4） |
| GET /api/agents/<id> | 645 ms / 785 MB / 1625 ms | 184 ms / 138 MB / 247 ms | 25 ms | **否 ×7**（T4） |
| GET /api/agents/<id>/rule-sync | 175 ms / 191 MB / 397 ms | 11.6 ms / 8 MB / 16 ms | 10 ms | 否（×1.2） |
| GET /api/profiles | 42 ms / 52 MB / 108 ms | 13.7 ms / 11 MB / 20 ms | 2 ms | **否 ×7** |
| POST /api/agents/<id>/heartbeat | 5.8 ms / 7.6 MB / 15 ms | 3.7 ms / 1.8 MB / 6 ms | 2 ms | 否（×1.9） |

没有比 go 慢的路由。启动后首个采样 RSS（batch-21 124 MB vs go 97 MB）不是退步：batch-21 0.3 s 即就绪，采样时还没 GC
（go 1.3 s 就绪、已 GC 85 次）；batch-21 10 s 后 98 MB，启动期累计分配 433 MB（go 1168 MB）。

差距：空闲 CPU 仍为 Python 的约 2 倍、RSS 高 52 MB（存活堆只有 18 MB，RSS 来自分配抖动，T9 在 T4/T7 之后再看）；
Agent 列表/详情类路由仍是 Python 的 6–12 倍（`loadContentSource` 每个 Agent 一次配置 JSON 往返与 `copyAgent`，T4）。

## 验证（rebase 到 go bfd2daed3 之后的最终树）

- .64：`gofmt -l .` 无输出；`go vet ./...` 与 `go vet -tags configflow_proddiff,configflow_difftest ./...` 通过。
- .64：`go test -race -count=1 -p 4 -timeout 60m` model、repo、redact、notifications、agents、agents/rulesync、
  agents/dnsfollow、agents/dnsha、refresh、tasks、discovery、substore、health/...、routers、api/routers、
  api/clientdevices、api/settings、api/agents、api/rules、api/notifications、oxidns：23 个包全部 ok（EXIT=0）。
- .64：pyref 重录后 `PYREF_MODE=check` oxidns、mosdns、routersplit、installer、api/agents 全部 ok；
  重录的 13 个 golden 只有源哈希变化（条目数与内容逐个比较相同）。
- .32：`CONFIGFLOW_DIFF=1` difftest：tasks、refresh、discovery、health、substore、agents/dnsfollow、agents/dnsha、
  api/agents、api/config、api/oxidns、api/rules、api/rulelibrary、api/aggregations 全部 ok，0 SKIP。
- .32：`/tmp/preflight.sh`（两套 vet + 完整非 race `go test ./...`）PREFLIGHT PASS。
