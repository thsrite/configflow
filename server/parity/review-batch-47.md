# batch-47 复核：O10 缓存预算（`go-perf-cache-budget`）

复核者：Claude Opus 5.5，2026-11-23。分支 `batch-47` 已 `git rebase go`（`go@ffd33e872`），无冲突。
正确性验证在 `.64`；性能结论以 PVE 正式测量 CT 240 为准（第 4 节），`.64` 的数字只作参考。

## 结论

改动正确，可以合并。没有发现高/中级问题；修了两处低级问题，补了三组只读/失效测试。
内存下降站得住（CT 240：负载后存活堆增量 11.4 → 5.8 MB，静置 RSS 92.9 → 78.2 MB）；CPU 与改前持平（19 条路由合计 +3%，来自一次后台任务撞进 `/api/routers` 窗口，去掉路由器族 +1.8%）。作者报告中的「CPU −7.7%」「provider 延迟 −33%」没有复现。

## 1. 零拷贝共享（`LoadSubscriptionCacheShared` / `BindTypedSubscriptionCache` / own=false）

- **语义等价**：JSON 路径是 `model.Marshal(*substore.Cache)` → `subs.SubscriptionCache.UnmarshalJSON`。
  逐项对照：`Metadata.Dialers` 是 `omitzero`，所以 JSON 路径的 `DialersStored` 恰好等于 `Dialers != nil`，与共享视图一致；
  `Hosts.Entries`（`OrderedMap`）编码总是 `{…}`，JSON 路径不会出现 `hostsDamaged`，共享视图同样不设；
  `url: null` 两边都是 `""`；`Nodes` 为 nil/空两边长度都是 0。
- **调用方只读**：`subs.NewHostIndex` 只读 `Hosts`/`Nodes`（`Skipped` 复制）；`DialerGroups` 把节点拷到新切片，
  `FilterNodes` 返回新切片，`SourceDialers` 返回共享 `*DialersData` 但后续只 `Get`/遍历，成员用 `append([]string{}, …)` 复制；
  `NodeProxy` 读 `Params`（`Dynamic` 是字节，`Node()` 每次新建图）和 `ProtocolParams`（字节，约定不可变）。没有排序、补字段、删成员或向共享底层数组 append。
- **provider own=false**：`ValidateArtifacts`、`PrepareBundle` 的链式探测只读；`DeliverySnapshot.Validate` 注释即「without mutation」。
  `own=true` 的读者仍拿 `copyNodes` 副本。
- **证明**（新增测试，`-race` 通过）：
  - `server/TestO10SharedSubscriptionCacheStaysReadOnly`：订阅缓存带 hosts + dialers（`dialer-proxy` 中转组），
    串行 2 次 + 并发 8 次生成 `/api/config/default/mihomo`：输出完全相同、确实用到缓存（含 `Feed A/Transit` 与 hosts 目标）、
    `LoadCacheShared` 指针未换、共享记录的 `model.Marshal` 前后字节相同；重写缓存后下一次生成立即使用新 hosts。
  - `provider/TestO10SharedProviderParseStaysReadOnly`：含锚点/别名与 `dialer-proxy` 的 provider 文本，并发 `ValidateArtifacts`
    + 并发改写 `own=true` 副本，记忆中的共享解析结果不变、未被替换。
- 只读防护（view guard）未加：现有调用方全部只读，上面两条测试作为回归防线；以后新增调用方若要改节点，应改用 `LoadCache`/`own=true`。

## 2. 缓存失效

- 规则内容池（`rulefetch.normalizedCache`、`deploy.conversions`）按内容摘要做键，内容一变即未命中；单条 > 512 KB 不进缓存只影响命中率。
- `provider.parsedProviders/parsedSubscriptions`、`convert.subscriptionCaches` 以全文为键，LRU 只影响保留量。
- `aggregations.sourceNodeMemo` 键为订阅 ID，但命中还要求 `entry.text == text`，旧文本不会被返回。
- `refresh.cacheTexts` 命中要求 `kept.cache == cache`（substore memo 换代即换指针）。
- 典型路由回归：`TestO10ReuseTablesFollowInputChanges` 通过；新增测试覆盖 mihomo 路由的 typed 路径。

## 3. repo `setCache`/`dropCache` 清旧代 `agentsCache`

- 只把指针置 nil，借出的 `[]model.Agent` 仍被读者引用，GC 不会回收，读者安全。
- 超出 `cacheMaxBytes` 的文档 `setCache` 提前返回，不清 `agentsCache`——这正是 `agentsCache` 唯一不可替代的场景，未受影响。
- 竞态情形（旧 head 的 `setCache` 晚于新 head 的 `agentsCache` 写入）只会多一次解码，不会返回错数据（命中要求 head 相等）。
- 新增 `repo/TestDroppedAgentsCacheKeepsBorrowedRecords`：4 个读者在借用期间反复 `dropCache`，同时 20 次 `UpdateSystem`，`-race` 通过，借出记录不变，最后可见最新一代。

## 4. 性能

### 4.1 PVE 正式测量机 CT 240（结论以此为准）

`rt -- server/tools/o10perf/pve.sh run --servers go`（改后 = 本分支）与 `pve.sh run --src ~/o10src/b47-base --servers go`
（改前 = `git archive go@ffd33e872`），参数相同（默认：空闲 30 min + 每路由 1 次预热 + 10 次计时 + 静置 120 s），
串行独占 CT：改后 10:33–11:09 UTC，改前 11:09–11:46 UTC；开跑前 `pct status 240` = stopped、两台测试服务器都没有其他 o10perf；
宿主 load 1–2。私有输出：`.32` `~/cf-prod-snapshot/runs/pve-20261123-103316`（改后）、`pve-20261123-110944`（改前）。

| 指标 | 改前 `ffd33e872` | 改后 batch-47 | 变化 | 对照 `o10-pve-baseline.md` batch-21 |
|---|---:|---:|---|---:|
| 空闲 CPU 平均 | 0.52% | 0.55% | 持平 | 1.91–1.97%（Python 1.45–1.57%） |
| 空闲 10 s 窗口中位 / p10 / p90 | 0.20 / 0.10 / 0.40% | 0.30 / 0.10 / 0.40% | 持平（0.1% 是一个 tick） | 1.1 / 0.9–1.0 / 1.3–1.4% |
| 空闲尖峰 | 12.4% | 12.2% | 持平 | 41% |
| 空闲 RSS 结束 / 最大 | 60.8 / 70.6 MB | **57.8** / 71.9 MB | −3.0 MB | 91–93 / 125–142 MB |
| 空闲 GC 后存活堆（MemStats） | 4.83 MB | 4.72 MB | 持平 | 16.7 MB |
| 存活堆 空闲 → 负载后（heap profile inuse） | 6.46 → 17.84 MB | 7.20 → 12.99 MB | | |
| **负载后存活堆增量** | **+11.4 MB** | **+5.8 MB** | **−5.6 MB** | （batch-21 负载后 GC 存活堆 35–42 MB） |
| 负载后即刻 RSS | 97.2 MB | 105.3 MB | 取决于采样时刻与 GC 相位 | 132–140 MB |
| 静置 120 s 后 RSS / 静置期最低 heap_alloc | 92.9 MB / 18.8 MB | **78.2 MB / 12.1 MB** | −14.7 MB / −6.7 MB | 141–155 MB |
| 19 条路由 CPU/次合计 | 394 ms | 406 ms | +3.0% | |
| mihomo 延迟 / CPU / 分配 | 65 ms / 87 ms / 36.4 MB | 66 ms / 87 ms / 34.5 MB | 分配 −5% | 236–274 / 346–359 ms / 171 MB |
| provider 延迟 / CPU / 分配 | 78 ms / 94 ms / 38.0 MB | 76 ms / 94 ms / 36.0 MB | 分配 −5% | 313–334 / 440 ms / 206 MB |

19 条路由 CPU 的 +12 ms 几乎全在路由器族：`GET /api/routers` 7 → 22 ms（分配 2.8 → 9.4 MB），而 `policy` 12 → 8、
`policy-settings` 31 → 24 ms 反向变化；去掉路由器族 4 条后合计 342 → 348 ms（+1.8%），其余路由逐条差 ≤ 2 ms。
`.64` 上 12 轮（改前改后各 6 轮）`/api/routers` 都是 0.4 MB/次，policy 族的偶发尖峰（13.4 MB、8.9 MB）两边都出现过，
说明这是单轮里后台任务（断网下的规则源/订阅重试、rule-sync）撞进该路由计时窗口，与 `o10-pve-baseline.md` 记录的
「`GET /api/routers` 第 2 轮分配 25 MB（第 1 轮 15 MB）」是同一类波动，不是本批引入的回退。所有请求 200。

结论：内存目标成立（负载后存活堆增量从 11.4 MB 降到 5.8 MB，低于 ≤ 10 MB 目标；静置 RSS −15 MB）；空闲与路由 CPU 没有回退；
作者写的 CPU −7.7%、provider 延迟 −33% 在正式测量机上没有出现（CPU、延迟都持平，只有分配 −5%）。

### 4.2 `.64` 参考数字（门禁同时在跑，只看内存方向）

内存（`O10_LEAN=1 GODEBUG=disablethp=1 --idle-minutes 2 --iterations 10 --mem-detail --settle-seconds 60`，各 2 轮）：
静置后存活堆 18.23 / 18.28 MB → 11.36 / 11.98 MB，负载后存活堆增量 +8.6 / +7.8 → +3.8 / +5.2 MB，与 CT 240 方向一致。
CPU 数字受负载影响（改后两轮期间 load 达 109/40），不作结论。
私有输出：`.64` `~/cf-prod-snapshot/runs/o10-b47rev-*`。

## 发现

| 级别 | 内容 | 处理 |
|---|---|---|
| 低 | `refresh.cacheTexts.store`：新文本超过 `limitBytes/2` 时直接返回，同一订阅的旧一代（旧记录指针 + 旧文本）留在表里直到被 LRU 挤出。不影响正确性（命中要求指针相同），但违背「只留一代」 | 已修 `b45802350`：先删旧一代再判断大小；测试补断言 |
| 低 | `aggregations.sourceNodes`：同上，超限文本不缓存时旧一代滞留 | 已修 `b45802350`，测试补断言 |
| 低 | `activeTypedSubscriptionCache` 用 `reflect.Value.Pointer()` 比较函数值：同一函数字面量生成的不同闭包代码指针相同，若测试用同一字面量的闭包替换 `LoadSubscriptionCache` 不会触发回退。目前生产与测试都没有这种用法 | 记录，不改 |
| 信息 | 作者报告的 CPU −7.7%、provider 延迟 −33% 在 CT 240 上未复现（持平）；内存下降复现 | 见第 4.1 节 |
| 信息 | 第一次 `GATE_ONLY=difftest` 有 `substore/difftest TestDifferentialReviewMalformedState` 一项不同：Sub-Store「检查更新」的网络异常类型（Python `ConnectionError` vs Go `ProxyError`），取决于当时的外网/代理环境，与本批无关；单独重跑两次、整阶段重跑一次均通过 | 无需处理 |

功能差异：无。

## 提交

- `b45802350` go(refresh,aggregations): drop the superseded generation when the new text is too large to keep
- `9fbf90ffa` go(server,provider,repo): batch-47 review tests for shared caches staying read-only
- 本报告单独提交。

## 验证（`.64`）

```
rt -- bash -lc 'cd server && gofmt -l . && go vet ./... && go test -race -count=1 -p 6 ./internal/substore ./internal/convert/... \
  ./internal/provider ./internal/aggregations ./internal/refresh ./internal/rulefetch ./internal/deploy ./internal/repo ./internal/subs \
  && go test -race -count=1 -run TestO10 ./internal/server/'
GOFMT-OK / VET-OK；上述 10 个包与 internal/server 全部 ok
CONFIGFLOW_DIFF=1 go test -tags configflow_difftest（substore/difftest、convert、provider、aggregations、refresh、repo、deploy）×2：全部 ok
GATE_ONLY=difftest scripts/gate.sh -p 6：DIFF PASS: 214 required entries ran and passed; 0 test skips / GATE STAGE PASS: difftest
GATE_ONLY=proddiff scripts/gate.sh -p 6：baseline: new=0 stale=0，self_check PASS，GATE STAGE PASS: proddiff
/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-47 -p 6：PREFLIGHT PASS
```
