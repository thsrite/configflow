# batch-47 复核：O10 缓存预算（`go-perf-cache-budget`）

复核者：Claude Opus 5.5，2026-11-23。分支 `batch-47` 已 `git rebase go`（`go@ffd33e872`），无冲突。
测试服务器：`.64`（`.32` 当时 load 25–30，未用于计时）。

## 结论

改动正确，可以合并。没有发现高/中级问题；修了两处低级问题，补了三组只读/失效测试。
内存下降站得住；CPU 与改前持平，作者报告中的「CPU −7.7%」「provider 延迟 −33%」在本次重跑中没有复现（落在噪声内）。

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

## 4. 性能（`.64`，`O10_LEAN=1 GODEBUG=disablethp=1`，改前 = `go@ffd33e872`）

内存（`--idle-minutes 2 --iterations 10 --mem-detail --settle-seconds 60`，改前/改后各 2 轮，交替顺序）：

| 指标 | 改前 base1 / base2 | 改后 after1 / after2 |
|---|---:|---:|
| 空闲结束存活堆（GC 后） | 9.64 / 10.46 MB | 7.57 / 6.80 MB |
| 负载后即刻 RSS | 94.4 / 91.6 MB | 73.3 / 78.9 MB |
| 静置后存活堆（GC 后） | 18.23 / 18.28 MB | **11.36 / 11.98 MB** |
| 负载后存活堆增量 | +8.59 / +7.82 MB | **+3.79 / +5.18 MB** |
| 每次请求分配：mihomo / provider | 36.5 / 38.1–41.3 MB | 34.6 / 36.1 MB |

静置存活堆净降约 6.5 MB，负载后 RSS 降 13–21 MB，与作者数字一致。

CPU：after 两轮跑时 `.64` 被其他任务推到 load 109/40，CPU 不可比，另补两组安静时的对比（load ≤ 4）：

| 运行 | 19 条路由 CPU/次合计 | mihomo CPU | provider CPU |
|---|---:|---:|---:|
| iter10 改前 B1 / B2 | 206 / 211 ms | 55 / 56 | 53 / 54 |
| iter10 改后 A1（load 4→13 衰减中）/ A2 | 241 / 216 ms | 66 / 54 | 61 / 53 |
| iter40 改前 B1 / B2（全部行合计） | 340 / 341 ms | 48–50 | 53–54 |
| iter40 改后 A1（load 8）/ A2 | 424 / 352 ms | 48–60 | 52–64 |

安静条件下改后比改前高 0–3%，在噪声内，**没有 CPU 回退**，但也看不出作者写的 −7.7%。provider 路由延迟中位数
两边都在 72–152 ms 之间跳动（每个点只有 10/40 次），不能支持「−33%」的结论；CPU/次两边相同。

私有运行目录（不提交）：`.64` `~/cf-prod-snapshot/runs/o10-b47rev-{base1,base2,after1,after2}-165710`、
`o10-b47rev-{cpuA1,cpuA2,cpuB1,cpuB2}-171512`、`o10-b47rev-i40{A1,A2,B1,B2}-172318`。

## 发现

| 级别 | 内容 | 处理 |
|---|---|---|
| 低 | `refresh.cacheTexts.store`：新文本超过 `limitBytes/2` 时直接返回，同一订阅的旧一代（旧记录指针 + 旧文本）留在表里直到被 LRU 挤出。不影响正确性（命中要求指针相同），但违背「只留一代」 | 已修 `b45802350`：先删旧一代再判断大小；测试补断言 |
| 低 | `aggregations.sourceNodes`：同上，超限文本不缓存时旧一代滞留 | 已修 `b45802350`，测试补断言 |
| 低 | `activeTypedSubscriptionCache` 用 `reflect.Value.Pointer()` 比较函数值：同一函数字面量生成的不同闭包代码指针相同，若测试用同一字面量的闭包替换 `LoadSubscriptionCache` 不会触发回退。目前生产与测试都没有这种用法 | 记录，不改 |
| 信息 | 作者报告的 CPU −7.7%、provider 延迟 −33% 未复现；内存数字复现 | 见第 4 节 |
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
