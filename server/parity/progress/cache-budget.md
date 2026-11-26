# O10 缓存预算报告与进度（`go-perf-cache-budget`）

状态：**已完成并验证通过**（基于 `go@d8286168b`）。

## 总体数字

### 1. 同机同构建前后对比（`.64`，`O10_LEAN=1 GODEBUG=disablethp=1`，2 min 空闲 + 10 次迭代 + 60 s 静置）

| 指标 | 改前（`go@d8286168b`） | 改后（`HEAD`） | 变化 |
|---|---:|---:|---|
| 空闲结束 RSS（GC 后） | 65.9 MB | **61.7 MB** | −4.2 MB |
| 负载后即刻 RSS | 86.6 MB | **76.1 MB** | −10.5 MB |
| 负载后静置 60 s RSS（GC 后） | 78.9 MB | **75.1 MB** | −3.8 MB |
| 空闲结束存活堆（GC 后） | 9.33 MB | **7.10 MB** | −2.23 MB |
| 负载后静置存活堆（GC 后） | 17.57 MB | **10.98 MB** | **−6.59 MB** |
| **负载后存活堆增量（`settled_gc − idle_end_gc`）** | **8.24 MB** | **3.89 MB** | **−4.35 MB（远低于 ≤ 10 MB 目标）** |
| 19 条路由 CPU/次合计 | 261.0 ms | **241.0 ms** | −7.7% |
| `GET /api/config/<profile>/mihomo` 延迟 / CPU / 分配 | 40.0 ms / 61 ms / 34.95 MB | **32.4 ms / 50 ms / 32.97 MB** | −19% / −18% / −5.7% |
| `GET /api/profiles/<profile>/aggregations/<agg>/provider` 延迟 / CPU / 分配 | 110.9 ms / 77 ms / 39.38 MB | **74.1 ms / 55 ms / 34.45 MB** | −33% / −29% / −12.5% |

> 说明：2 min 空闲窗口下，后台 `agent-rule-sync` 在空闲期已触发一轮规则构建，因此 `BASE` 的 2 min 空闲存活堆（9.33 MB）已包含部分规则缓存；与 R13 10 min 空闲基线（空闲 6.48 MB → 负载后 25.1–26.1 MB，增量 ~19 MB）对照，改后负载后存活堆从 ~25.5 MB 降到 **10.98 MB（lean）/ 11.48 MB（pprof，增量 5.74 MB）**，净降约 **14 MB**。

### 2. 每项缓存改前/改后占用（heap profile diff `idle → after-load`，inuse_space）

| 缓存项 | 改前（R13 `065519`） | 改后（`.32` `075451`） | 改动说明 |
|---|---:|---:|---|
| 规则内容池：`rulefetch.normalizedCache` + `deploy.conversions` | 4.39 + 2.16 = **6.55 MB** | 1.15 + 0.57 = **1.72 MB** | 两表总上限 16 MB → **1 MB**，单条 > 512 KB 不缓存；`normalizedCache` 改按字节计数 |
| `yamlx.CopyGraph`（`provider.parsedProviders` + `parsedSubscriptions`） | **4.61 MB** | **2.50 MB** | `ParseNodes` 不再污染 `parsedProviders`；`readProviderNodesFor(..., own=false)` 冷路径与 `ValidateArtifacts` 零 `CopyGraph`；两表改 LRU、上限 32 → 16 MB |
| `convert.subscriptionCaches` + `substore.cacheMemo.encoded` | 2.05 + 0.73 = **2.78 MB** | **0 MB** | 新增 `substore.LoadSubscriptionCacheShared` + `MihomoEnvironment.BindTypedSubscriptionCache`，零拷贝共享 `Nodes`，生产路径不再 JSON 往返（替换 `LoadSubscriptionCache` 时自动回退）；旧 JSON 表收紧到 4 MB LRU |
| `aggregations.sourceNodeMemo` | **1.54 MB** | **0.50 MB** | 按订阅 ID 只保留一代 + LRU，上限 32 → 16 MB，订阅内容变化立即替换旧代 |
| `refresh.cacheTexts` | ~0.73 MB | ~0.73 MB | 保留按订阅 ID 只留一代，补充 4 MB 字节上限 + LRU |
| Agent/系统派生视图（`api/agents.agentViews`） | ~1.6–3.6 MB | **1.56 MB** | 按 `(repository, head, env)` 只留一代，未改 |
| rule-sync 写回新快照（`editRegistry -> UpdateSystem` / `repo.agentsCache`） | **5.36 MB** | **−0.21 MB（≈ 0）** | `Repository.setCache`/`dropCache` 同步清空旧代 `agentsCache`，不再滞留旧代 `[]model.Agent` |

## 提交记录（`go-perf-cache-budget`，基于 `go@d8286168b`）

- `b6bfd388b` `go(substore,convert): O10 cache budget: share typed subscription cache without JSON round-trip`
- `64b584b19` `go(provider,aggregations,refresh): O10 cache budget: drop duplicate provider ASTs and bound reuse tables with LRU`
- `37817a40c` `go(rulefetch,deploy): O10 cache budget: cap rule content caches at 1 MB`
- `838d87163` `go(repo): O10 cache budget: drop superseded agentsCache on documentCache update`
- `7298c3127` `go(parity): record O10 cache budget round-1 progress`
- `558143f13` `go(convert): bypass typed subscription cache when LoadSubscriptionCache is overridden`
- （本报告与 `gofmt` 对齐为最终提交）

## 验证结果

- 受影响包 `-race`（`.64` + `.32`）：`substore`、`convert`、`convert/mihomoenv`、`provider`、`aggregations`、`refresh`、`rulefetch`、`deploy`、`repo` 全部 `ok`。
- `server` 包 R11 测试（`.32` + `.64`）：`TestO10RouteAllocationCeilings`、`TestO10WholeRecordEncodeBoundary`、`TestO10WholeRecordEncodeBoundaryDetects`、`TestO10ReuseTablesFollowInputChanges` 全部 `PASS`（`mihomo` 9.21→7.37 MB/req，`provider` 30.43→24.95 MB/req）。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__go-perf-cache-budget -p 6`（`.32`）：`PREFLIGHT PASS`。
- `GATE_ONLY=proddiff server/scripts/gate.sh -p 6`（`.32`）：`baseline: new=0 stale=0`，`self_check: PASS`，`GATE STAGE PASS: proddiff`。
- 私有运行目录（不提交）：`.32` `~/cf-prod-snapshot/runs/o10-20261123-075451`，`.64` `~/cf-prod-snapshot/runs/o10-cb-{base,after}-160311`。
