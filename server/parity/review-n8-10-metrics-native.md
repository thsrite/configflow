# N8-10 metrics / trafficstore native HR 复核（fresh Opus）

复核对象：`go-n8-10-metrics-native@1ce382482`，只看 `c8208b60f..1ce382482` 的 7 个提交（e5333b76e native 服务、
6bc803ebd 存储生命周期与批量更新、df2ab27a4 SQLite 坏计数器、2c353f574 api/agents 接线与差异绑定、71a67cea8 文档、
b7db27f14 REAL 聚合 NaN、1ce382482 交回文档）。复核分支 `go-opus-review-n8-10-metrics-native`。对照 Python
（`backend/agents/traffic_store.py`、`traffic_history.py`、`metrics_history.py`、`backend/routes/agents.py`）和迁移前 Go。
协调者已批准的裁定（A04 跳过坏条目、A05 只拒写坏 Agent/日 bucket、native 写 API 带 ctx、service_type guard、REAL NaN）
只核对实现与测试，没有重新讨论。定向测试只在 .64 经 `rt` 运行，没有重跑作者整套差分，也没有跑完整门禁。回归只用合成数据。

## 结论

high 0，medium 0，low 8（1 已修（补测试）、2 已补登、5 记录）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| R1 | low | 已修（补测试） | **`metrics.SetTrafficRetention` 没有真实 Runtime 的关机/断线测试。** 位置 `metrics/traffic_native.go`。WORKFLOW 第四条常驻项要求每个写 owner 在 Detach 后、提交前检查 `ctx.Err()`，并用真实 `Runtime.Start/BindRequest/Shutdown` 测试。代码本身正确（Detach → 检查 → `UpdateSystem` 回调内再检查；repo 再 Detach 并在事务内检查），但作者的 Runtime 测试只覆盖了 AddTraffic、TrafficHistory/History.FlushContext、AddSample、trafficstore.Add，保留天数这个 repo 写入没有覆盖。补测试 `TestReviewN810TrafficRetentionShutdownAndDisconnect`（表驱动）：关机后返回 `context.Canceled`，存储仍为默认 90；请求断开后写入完成，存储为 180。负控：去掉 `lifecycle.Detach` 后 disconnect 用例失败（`context canceled`）；改成 `context.Background()` 并去掉检查后 shutdown 用例失败（写入落地）。还原后通过。 |
| R2 | low | 已补登 | **`traffic/stats` 的 `data_points` 只计可用样本。** 位置 `metrics/stats.go` `trafficSamples`/`TrafficStatistics`。Python 的 `total`/`hours_24` 只读首尾两个样本，中间样本的 `network` 为 null、计数器为字符串、时间戳损坏都不影响，返回 200，`data_points` 计入这些样本；Go 跳过这些样本，返回 200，但 `data_points` 少计，首尾也可能落到相邻的好样本。只有人工损坏的历史才会出现，属于已批准的“坏条目跳过”。但 ledger 写的“`data_points` 原始点数语义保留”只对 summary 成立，所以在 behaviour-changes 的 N8-10 段补了一句说明 stats 按可用样本计数。 |
| R3 | low | 已补登 | **HTTP 浮点文本对 ≥1e6 的值改用指数形式。** 位置 `metrics/stats.go` `finiteJSON`（`strconv.FormatFloat(f, 'g', -1, 64)` + 补 `.0`）。shortest `'g'` 在指数 ≥ 6 时就用 e 记法：summary 的 avg、traffic stats 的 avg_speed、trend 的 speed、traffic-history 的 REAL 计数器，例如 1234567.5 会输出 `1.2345675e+06`，而 Python/旧 Go 输出 `1234567.5`。网速平均值经常超过 1e6 B/s，所以正常数据也会遇到。数值相等，前端 `JSON.parse` 结果相同，差分按结构比较，所以没有报差异；但这是 HTTP 字节变化，ledger 原来只写了“存储 JSON 的等值浮点格式”。已补登。没有改实现：改动会牵动作者已经 SHA 绑定的 native 期望（`n810_metrics_native.json`），而且属于已许可的 native 序列化。如果协调者希望和 `encoding/json` 一致（1e-6 ≤ \|x\| < 1e21 用定点），可以作为 N8-11 的小任务，需要同时更新绑定。 |
| L1 | low | 记录 | **trafficstore 的 REAL 计数器写回文本会变。** 位置 `trafficstore/counter.go` `storedCounter.raw`。只有旧文件里人工写入的浮点计数器才会走这条路径：`3.0 + 0` 写回 `3`（重读后变成整数），`123456789.0` 写回 `1.23456789e+08`。值不变，不丢字段；Agent 上报只有整数，正常数据不受影响。在“等值浮点格式”许可范围内。 |
| L2 | low | 记录（需要时请协调者裁定） | **整个文件不是合法 JSON 时，下一次上报仍会覆盖它。** 位置 `trafficstore/store.go` `read`（`jsonstore.Read` 对解析失败返回空 → `emptyDocument`）。Python 和迁移前 Go 也是这样，裁定只覆盖 present 错型的 `days` 和 bucket。因为写入是 tmp+rename 原子替换，正常情况下不会产生截断文件。如果希望 fail-closed，需要协调者裁定；本轮没有改。 |
| L3 | low | 记录 | **traffic-report / traffic-history 上报的 >4300 位整数字面量：500 → 400。** 位置 `api/agents/traffic.go` `readTrafficBody`。旧 Go 专门复刻了 Python 的 `Exceeds the limit` 500，现在合法 JSON 进入 owner 解码，按计数范围判为无效，返回 400 `Invalid traffic report`/`Invalid traffic history report`。与 ledger 已有的“不模拟 CPython 4300 位上限”属于同一类（第 2267/2283 行），只是没有逐路由列出。 |
| L4 | low | 记录 | **进度文件写“生产 metrics 不再导入 jsonx/jsonresp/pyvalue”，与实际不完全一致。** `metrics/history.go` 仍然调用 `jsonresp.DecodeStored/Encode/NormalizeNonFinite` 处理 JSONL 文件内容（N8-7 的条目）。jsonresp 不属于 N8-11 要删除的 facade，jsonx 和 pyvalue 确实已经没有了，只是文字不准确。N8-11 剩余 facade 清单已逐个核对：`discovery/legacy.go`（Load/LoadStatus）、`configgen/generate.go`、`deploy/adapter.go` 两处、`tools/proddiff/generator/main.go`（StatusOf）、`api/agents/client_tools_adapter.go`（DeleteContext），共 6 处调用，与清单一致；metrics 没有生产 legacy 调用方（旧名字只出现在 `_test.go`，构建通过即可证明）。 |
| L5 | low | 记录 | **哨兵命名。** `metrics.RetentionValueError` 是 `*ValueError` 哨兵，按 AGENTS.md 应该命名为 `ErrXxx`。这是从旧代码平移过来的，API 用 `errors.As(*ValueError)` 判断。可以在 N8-11 或 metrics S 轮次改名。 |

## 逐项核对

1. **存储兼容（A05 文件）。** `Document` 保留顶层未知成员；只解析当日 bucket，其他日期原样保留（坏的旧日期随七天过期清理）；host/route/port 用 `records` 保留原顺序，新增成员追加在后面。新建 host 和默认 stats 的键序和 Python 不同（`ports` 在 `by_route` 之前；`fail_kinds` 在最后），只影响字节，已批准为第五项结构比较，`samePolicyJSON` 仍然严格比较数组顺序和数值（有负控）。`emptyDocument` 的成员顺序是 `status, days`，和 Python 一致。prune 和 Python 的 `timedelta(days=6)` 等价。旧 Go 的 status cache 签名/LRU 语义不变。
2. **fail-closed，不覆盖原文件。** present 错型 `days` → `read` 报错；当日 bucket、`hosts`、被访问的 host/route/counter、status 成员错型 → `Add` 报错；在 `writeJSON` 之前返回，原文件不动（`TestNativeStoreIsolatesBucketsAndRecoversThroughExistingClear`、差分 `agent-edit.json` 逐字节保留）。拒写告警按 path+date 每 5 分钟最多一条，限流表最多 4096 项，经 `redact.EventText`。status 成员错型 → 500 已在 ledger 登记（“坏 status 被新版本字段绕过以及数字 mihomo_version”）。读取（`LoadDocument`）隔离坏 bucket/host/route/port，不写文件。恢复路径：现有 clear（DeleteContext）和七天过期，均有测试。
3. **A04 跳过。** summary/stats/trend 遇坏组件、坏数值、坏 section 时跳过并限流告警（按 Agent+字段），HTTP 200；summary 的 `data_points` 仍是原始点数。avg 用精确整数和（big.Rat）后再转换，混合浮点时按 Python 顺序提升；max/min 取第一个极值，按精确值比较；bool 按 1/0 参与，原样输出。
4. **SQLite 事务与部分失败。** upsert 的 `ON CONFLICT … DO UPDATE … WHERE` 防护 + `changes()==1` 校验，坏行冲突时返回 TypeError → 整个事务回滚，pending 恢复；compaction 在原有 GROUP BY 扫描里用 `MIN(typeof/范围)` 校验，Inf 由 `tripleFromSQL` 拒绝，SQLite 整数 SUM 溢出本来就会报错；`sqlValue` 拒绝绑定非有限值。查询逐行隔离（Scan 失败或计数器坏 → 告警、跳过），不写库。schema 不变。
5. **REAL 聚合 NaN（作者新加）——合理。** 只有人工写入的、符号相反的 ±1e308 级 REAL 行才会在聚合中得到 +Inf/−Inf，进而 `up+down = NaN`。`counter.cmp` 把 NaN 作为最小的一类（NaN 之间相等，再按 key 排序），仍然是严格弱序，`sort.Slice` 结果确定；`exact()` 不会收到 NaN（Inf 可以安全进入 big.Float）。输出 `null`，`queryMissingCounter` 触发一条限流告警；查询不写库；trimDay 走同一个 `cmp`，最终由 `sqlValue` 拒绝写入，事务回滚。测试 `TestNativeTrafficQueryFiniteREALOverflowDoesNotPanic` 覆盖排序（healthy 在前、manual 在后）、null、告警数为 1、原行数不变、`json.Marshal` 成功。
6. **并发与锁（o3）。** 没有新增 flock；trafficstore 继续用包级 `mu`（Python `_LOCK`）；没有新增裸 `go`；`time.Now` 只用于告警限流（门禁允许）。trafficstore 的 `now`/`SetNow` 是原有测试钩子。
7. **取消与关机（第四条）。** `trafficstore.Add/DeleteContext`、`AddTraffic`、`TrafficHistory.FlushContext`、`History.FlushContext/LoadContext/RetainMetricsAgents`、`AddSample`、`SetTrafficRetention` 都是先 Detach、检查，取锁后再检查，提交前（`WriteContext`、`appendLines` 每次写之前、`replaceContext` rename 之前、SQL commit）还会检查。关机 flush hook 收到 Runtime 的 `overall` 上下文（只受 grace 约束），所以 `metrics-history-flush-first/last` 不会被已取消的 job 上下文挡住。作者已有真实 Runtime 测试（`TestNativeServicesShutdownAndDisconnect`、`TestNativeStoreRuntimeLifetime`、`TestNativeStoreShutdownAfterOwnerLockWait`、`TestNativeTrafficShutdownWhileWaitingForOwnerLock`），R1 补上了保留天数。
8. **HTTP 边界。** traffic-report 的顺序和 Python 一致：鉴权 401 → 读 body（413/400）→ 409 已关闭 → 解码 400 → 写入；`status`/`outlet` 为 list/dict 时返回 500，与 Python 的 unhashable TypeError 一致。traffic-history PUT：非 JSON、非对象、`enabled` 非 bool、`clear` 为 null 时返回 400 `enabled 必须是布尔值`；retention：`30.0`、`true`、`"90"`、`null`、`[]` 都返回 400 `保留时长只能是 30、90、180 或 365 天`（`type(days) is int`）。`system_config` 为 falsy 时用默认值；为 truthy 非对象时，GET 返回 500，PUT 的 null/非对象也返回 500（Python AttributeError）。**R2 guard 保留**：`service_type` 缺失按 mihomo 处理，null 或错型返回 400。响应经 `httpx.JSONStatus` 排序键，struct 的字段顺序不影响输出。中文消息与 Python 逐字节相同。
9. **差分绑定（5 行）。** `n810_metrics_http_native.jsonl` 四行的 original 原文都做了 SHA256，SHA 不符就失败；按名字匹配后，再用 canonical 比较完整请求；Python 的 status/body 和三个响应头必须与绑定一致；native 的 status/body 也必须与审定值一致。`n810CompareAPI` 的 jsonl 里没有这些名字，不会覆盖结果。seed 用 `checkMetricsSeed`/`editedSeed` 逐字节检查，只替换动态日期。拒写文件逐字节比较。负控齐全：请求改变、无关 native 错误、原文 SHA、数组顺序/计数器变化都能检出；请求从 `requests()` 删除时 binding 测试失败。第五项 `agent-traffic.json` 只放宽对象顺序。不存在空过。
10. **被移到测试的 adapter。** `metrics` 的 `history/stats/flush/traffic_legacy_test.go`、trafficstore 的 `CleanReport/AddReport/...` 只在 `_test.go`，生产代码没有调用方。`addMetrics` 旧 hook 只做 `json.Marshal` → `AddSample` 委托；heartbeat 走 `AddMetricsJSON(ctx)`。
11. **性能。** 心跳路径（`AddSample` 用 `jsondecode.Select` 精确取 4 个成员）没有变重；作者原样运行的 `TestBatch10HeartbeatAllocationCeilings` 通过，阈值没有放宽。traffic-report（每 5 分钟一次，不在心跳路径）的 `DecodeReport` 先 `map[string]any` 解码再重新编码，以得到 last-member 语义，2000 items 约 30–50 MB 分配/次（作者 bench），可以接受；N8-11 或 O10 可以改成流式。
12. **风格。** 新增中文消息在 `messages.go`；日志用 `logx` + `redact`；错误类型平移（L5）。

## 复核运行（.64）

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/api/agents/review_n810_metrics_test.go)" && go vet ./internal/api/agents && go test -race -p 4 -count=1 -timeout=20m -run "ReviewN810TrafficRetention|TestOldAgentIsReportedUnsupportedAndRetentionIsValidated|TestSwitchReportsAndHeartbeat" ./internal/api/agents && go test -race -count=1 -run "Native|Context|Cancellation" ./internal/agents/metrics ./internal/agents/trafficstore && go test -count=1 -run "^TestN810MetricsHTTPPolicyBinding$" ./internal/api/agents/difftest'
```

```text
ok  	configflow/internal/api/agents	2.422s
ok  	configflow/internal/agents/metrics	5.358s
ok  	configflow/internal/agents/trafficstore	1.056s
ok  	configflow/internal/api/agents/difftest	0.013s
```

负控（在服务器副本上临时修改 `traffic_native.go`，跑完还原）：

```text
== control 1: no Detach
    --- FAIL: TestReviewN810TrafficRetentionShutdownAndDisconnect/disconnect (SetTrafficRetention error = context canceled, want <nil>)
== control 2: ignore shutdown
    --- FAIL: TestReviewN810TrafficRetentionShutdownAndDisconnect/shutdown (SetTrafficRetention error = <nil>, want context canceled)
== restored
--- PASS: TestReviewN810TrafficRetentionShutdownAndDisconnect (0.22s)
ok  	configflow/internal/api/agents	0.230s
```
