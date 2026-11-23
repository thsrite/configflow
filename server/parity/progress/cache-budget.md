# O10 缓存预算进度（`go-perf-cache-budget`）

## 目标

将生产快照下负载后存活堆增量从约 19 MB（R13 后：空闲 6.4 MB → 负载后 25.1–25.4 MB，增量 ~18.8 MB）降到 **≤ 10 MB**，不改变任何输出、不增加 CPU、不降低命中率，且尽量不碰 `internal/oxidns`、`internal/routers`、`internal/agents` 与 `repo` 存储布局。

## 基线归因（R13 `o10-20261123-065519` heap profile diff）

1. **规则内容池（~6.55 MB）**：
   - `rulefetch.normalizedCache`（原上限 16 MB，按 rune 计数）：~4.39 MB（来自后台 `deploy.RunDetached` 标准化大规则集）
   - `deploy.conversions`（原上限 16 MB）：~2.16 MB（来自 `ConvertMosDNSRules` 缓存大规则集）
2. **订阅节点缓存与解析复用链（~9–10 MB，同一批订阅节点被重复缓存 6 份）**：
   - `substore.cacheMemo`：`*substore.Cache`（~1.5 MB）+ `encoded []byte`（~0.73 MB）
   - `convert.subscriptionCaches`（原上限 32 MB）：~2.05 MB（把 `LoadCacheJSON` 编出来的 JSON 又解码回 `*subs.SubscriptionCache`）
   - `provider.parsedProviders` + `provider.parsedSubscriptions`（原各 32 MB，满表全清）：`yamlx.CopyGraph` 合计 ~4.61 MB（其中 `ParseNodes` 冷路径重复污染 `parsedProviders` ~2.05 MB；`ValidateArtifacts` 用 `own=true` 多做 `CopyGraph`）
   - `aggregations.sourceNodeMemo`（原上限 32 MB，满表全清）：~1.54 MB
   - `refresh.cacheTexts`（按订阅 ID 只留一代，无字节上限）：~0.73 MB
3. **Repository 快照（~5.36 MB）**：
   - `Repository.setCache` 更新 `documentCache` 后未清空旧代 `agentsCache`，导致旧代 `agentSnapshot` 滞留。

## 第一轮改动（已提交，验证中）

- `fdfa05df2` `substore.LoadSubscriptionCacheShared` + `convert.MihomoEnvironment.LoadSubscriptionCacheTyped`：生产路径 `MergeHosts`/`DialerGroups` 直接共享只读 `cache.Nodes`，不再触发 `substore.LoadCacheJSON`（`cacheMemo.encoded` = 0）和 `convert.subscriptionCaches`（0）；同时将 `convert.subscriptionCaches` 上限收紧到 4 MB（LRU）。
- `4cc2bd4a5` `provider` / `aggregations` / `refresh`：`ParseNodes` 直接调用 `parseProviderNodes` 不再污染 `parsedProviders`；`readProviderNodesFor(..., own=false)` 冷路径不额外 `CopyGraph`；`ValidateArtifacts` 改用 `own=false`；两张表改为 LRU 逐条淘汰，上限收紧到 16 MB；`aggregations.sourceNodeMemo` 改为按订阅 ID 只留一代 + LRU（16 MB）；`refresh.cacheTexts` 补充 4 MB 字节上限 + LRU。
- `be6862f26` `rulefetch.normalizedCache` & `deploy.conversions`：总上限均收紧到 1 MB，单条超过 512 KB 不缓存；`normalizedCache` 改为按字节计数。
- `eb194e9bd` `repo.Repository.setCache` / `dropCache`：同步清空旧代 `agentsCache`。

## 验证进度

- `.64` `gofmt -l ./internal` 无输出，`go vet ./...` 通过。
- `.64` `go test -race -count=1 -p 6 ./internal/substore ./internal/convert/mihomoenv ./internal/provider ./internal/aggregations ./internal/refresh ./internal/rulefetch ./internal/deploy ./internal/repo`：全部 `ok`。
- `.32` 新预算单测 + `go test -count=1 -v -run TestO10 ./internal/server`：全部 `PASS`；其中 `GET /api/config/<profile>/mihomo` 分配从 `9.21 → 7.37 MB/req`，`GET /api/profiles/<profile>/aggregations/<agg>/provider` 从 `30.43 → 24.95 MB/req`。
- 进行中：`.32` 改后 `o10perf --mem-detail`（含 pprof）、`.64` `./internal/convert -race`。
- 待跑：`GATE_ONLY=proddiff server/scripts/gate.sh -p 6`、`/tmp/preflight.sh`、以及低负载下的同机同构建（`O10_LEAN=1`）改前/改后 `o10perf --mem-detail` 正式对比。
