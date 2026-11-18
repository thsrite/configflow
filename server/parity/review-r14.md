# R14 HR 复核（automatic rule sync：Capture / Sync / ApplyRuleSyncDelta / StartJob / RunRound）

复核者：新的 Opus 会话（未参与移植；R14 由 Codex 移植，提交 `2e8925789`、`05f09a5f8`）。分支 `go-opus-review-r14`，
从 `go-port-r14@05f09a5f8` 分出（基于 go `c5f5ff349`）。只用 loopback fake 和合成数据，未接触生产、真实路由器、Agent、
DNS 或 PVE；未 push，未开子 agent；只在指定测试服务器（`$R14_TEST_HOST`）跑定向检查，没有跑 gate、proddiff 或全仓 race。作者的对照用例逐个读过；
修复后跑了一次 `TestR14|TestR13|TestR12Review`（包含作者全部 R14 用例与 40 组 Python 对照）确认没有回归。

范围：完整读了 Python `backend/routers/rule_sync.py`（474）、`rule_sync_rules.py`（214）、`rule_sync_store.py`（90）、
`rule_sync_write.py`（265）和 `routeros_nat_list.references/member_update/migration`，对照 Go 的 `rule_sync*.go`
（9 个文件）、`wire.go`，以及它们依赖的 `lockx.Keyed`、`lifecycle.Runtime`（workContext/Spawn/Shutdown/BindRequest）、
`ctxx.Detach`、`rulefetch.Client.RequestRule` 的重试、`agents.ManagerAgentSource`、batch-6 的 `routers/joblock.go`
与 `jobLockHook`。

## 结论

**0 条 high，2 条 medium（均已修），3 条 low（已修），4 条 low 记录在案（不改代码）；另有 1 项待 batch-6 合入后复验。**
未达到「≥ 2 条 high」的回退门槛。

签名（D2 canonical JSON，字段覆盖与 Python `controls` 逐项一致，含仅在启用时加入的 dns_guard / ipv6_pd_guard / dns_ha、
IPv6 Fake-IP 状态）、基准捕获与发布后 IPv6 overlay（R12 F1 的同类问题未复现）、受限增删（只写已登记目的、NAT 只动
目的成员、新增路由先于 NAT 成员、移除成员先于路由、外部引用拒绝、策略 manifest 保留未知字段与依赖顺序）、
所有错误类别的补偿（含 F-R12-3）、回执原子写与叶锁、坏回执按 paused 隔离、单 worker、30 s 首延迟/周期、任务移交与
Spawn 拒绝回滚、公开投影不含凭据与基准，均已核对。问题集中在「下载失败的分类」和「关机时的取消可见性」两处。

修复提交：`264ffbb55`（M1，lifecycle）、`3c492b520`（M2、L1–L3 及 O3-6b 集成守卫）、本报告所在提交（文档）。

## 发现

### 中

- **M1 — R13 关机用例间歇失败的根因：lifecycle 的 job context 与 `Detach(job)` 看到关机的时刻不同（产品竞态，已修）。**
  作者报告的 `TestR13ReviewNoticeWaitingForLeafLock/shutdown`（`shutdown lost cancellation`）不是测试问题。
  `Runtime.workContext` 用 `context.AfterFunc(g.jobs, cancel)` 把关机转发给 job context（另起 goroutine，异步），而
  `withGeneration(work, g, g.jobs)` 让 `Detach(job)` 直接以 `g.jobs` 为取消父节点。关机时，owner 里 Detach 后的叶写入
  （通知、回执、repo）立刻收到 `Canceled`，但 owner 紧接着检查自己的 `job.Err()` 仍可能是 nil：R13 的 owner 因此把读取结果
  当作成功返回；同样的窗口也存在于 R14 `Sync` 的「`ctx.Err()` 非空则不再写失败回执」判断和 AGENTS 第四条要求的每个
  「提交前检查 ctx」。即使把 job 改成 `g.jobs` 的直接子 context 也不够：标准库 `cancelCtx.cancel` 先关闭父节点的 Done，
  再逐个取消子节点，观察者仍能抢在子节点之前醒来（实测 200 次中 1–3 次）。
  证据：`internal/lifecycle/r14_review_test.go` `TestR14ReviewShutdownReachesJobAndDetachTogether`（Spawn 与 periodic
  round 两个子用例，各 200 次：job 内等待 `Detach(job).Done()` 后读 `job.Err()`），修复前 `141/200`、`139/200` 次读到 nil；
  R13 用例在原代码上 `-count=10` 失败 3 次。
  修复：generation 新增 `detached` context，作为 `Detach`/`BindRequest`/`Interval` 保留的取消父节点；detached job 与
  round 改为 `g.jobs` 的直接子 context（不再经 AfterFunc）；`Shutdown` 在 `stopJobs()`（同步取消全部直接子节点后才返回）
  之后再取消 `detached`。于是任何 Detach 后的操作看到关机时，job 自己的 `Err()` 必定已非空。非 detach 的 startup / loop
  driver 仍按原方式转发（它们需要保留调用方 ctx 的截止时间，且不是提交 owner）。既有约定不变：job 结束后逃逸出去的
  `Detach(job)` 仍不被取消（`TestBindRequestKeepsShutdownCancellation`）。
  修复后：新用例 `-count=5`（每次 2×200）全过；R13 用例 `-count=20` 全过；lifecycle 全包 `-race` 通过。
  这是 O3 共享代码的改动，单独成一个提交，协调者可按批次单独评估。

- **M2 — 下载在收到响应前用完 30 s 总预算（含共享客户端的重试）会被当作「超过大小或时间限制」，自动同步暂停到下次手动发布
  （Go 引入，已修）。** 作者把 Python「收到响应后才开始」的 30 s 总预算改为从请求前开始（已登记 behaviour-changes），但没有
  注意分类后果：`Download` 中只要 `work` 截止就返回 `messageSyncDownloadLimit`（普通 `RouterError` → `paused`）。
  Python 的连接超时 5 s、读超时 10 s 都是 requests 异常 → `DownloadError` → `failed`，下个间隔自动重试。rulefetch 对瞬时错误
  重试 3 次（间隔 1 s、2 s），带 GitHub 镜像时还有多个候选：上游规则服务器挂起不回响应头时 3×10 s + 3 s 就超过 30 s。
  结果：一次常见的上游短时故障就让所有使用该规则源的路由器停在「需要手动预览发布」，自动同步失去意义。
  证据：`TestR14ReviewPreResponseTimeoutIsTransient`：只接受连接、不回响应的 loopback 服务器 + 较早截止的调用方 ctx（与 30 s
  预算走同一分支，生产参数不变），修复前 `Download` 返回 `远程规则下载超过大小或时间限制`，`Sync` 结果为 `paused`。
  修复：只有已经收到响应、在读响应体时超时（Python 的逐块 deadline）或超过 4 MiB 才是限制错误；收到响应前预算用完是
  `syncDownloadError`（`远程规则下载失败：…`，脱敏），`Sync` 记为 `failed`，按间隔重试，不接触设备（用例断言无设备请求）。
  behaviour-changes 的 30 s 条目已补充这一点。

### 低（已修）

- **L1 — 「立即同步」遇到忙碌路由器时的文案与 Python 不同（Go 引入）。** Python `start_job` 是 `此路由器正在发布或同步，请稍后重试`，
  只有 `sync` 自己用 `…请稍后再试`；Go 两处都用了后者，经 C07 任务页 409 显示给用户。
  证据：`TestR14ReviewStartJobBusyMessage` 修复前 `start job busy message 此路由器正在发布或同步，请稍后再试`。
  修复：`StartJob` 用 `messageEditBusy`（同文，也是 batch-6 `RouterJobLock` 的冲突文案）；`Sync` 保持 `messageSyncBusy`。

- **L2 — 纯 IP 规则集里的 `domain:`/`full:` 等域名写法，值恰好是 IP 时被当作地址写进路由（Go 引入）。** Python 一旦判定为域名
  写法，`network` 就是 None：在 `ipcidr` 规则集里这行是无效条目，整次同步以「纯 IP 规则集含有域名或无效条目」停止。Go 对所有
  行都先解析 IP，域名写法在 `ipcidr` 下没有计入域名，随后被当作 IP 接受，`domain:198.51.100.0/24` 会成为一条静态路由。
  证据：加入作者的真实 Python 对照 `TestR14ClassifyPython`：`domain:198.51.100.0/24`、`full:203.0.113.1`（ipcidr）修复前
  `error <nil> expected "纯 IP 规则集含有域名或无效条目：1 条（第 1 行）"`。修复：域名写法的行不再参与 IP 判定。

- **L3 — 只用 CR 换行的规则文件被整体当作一行（Go 引入）。** Python `splitlines()` 和 YAML 都把单独的 `\r` 当作换行；Go 只拆 `\n`，
  `198.51.100.0/24\r203.0.113.0/24` 成为一条无效条目，同步暂停。证据：同一 Python 对照新增两例（纯文本与 `payload:` YAML），
  修复前纯文本例报 `纯 IP 规则集含有域名或无效条目：1 条（第 1 行）`。修复：分类前把 CRLF 和单独 CR 统一为 LF（版本号仍按
  原始内容计算）。Python 的其他 `splitlines` 分隔符（`\v`、`\f`、`\x1c`–`\x1e`、`\x85`、U+2028/2029）未跟随：规则文件里
  几乎不出现，且 Go 的方向是拒绝（fail closed），不会写错路由。

### 低（记录，不改代码）

- **L4 — 本轮跳过与「立即同步」未执行时少两条日志。** Python `run_round` 对 `RouterError` 记 info
  `主路由 %s 本轮远程规则同步已跳过：%s`，`start_job` 的线程记 warning `…手动触发的远程规则同步未执行`；Go 静默忽略
  （回执与任务状态照常写入）。只影响排障日志，不影响边界行为；建议 R15/S 轮补上（文案放 `rule_sync_messages.go`，用路由器
  名、经 `diag.EventMessage`）。
- **L5 — 多个规则集同时触发「有效 IP 变为 0」等检查时，报告哪一个的顺序不同。** Python 按规则集选择顺序，Go 按规则集 ID 排序；
  只影响多个问题同时存在时消息里先出现的名字。
- **L6 — 一个配置空间读取失败会中止整轮（与 Python 相同）。** `RunRound` 中 `GetProfile` / profile 解码出错直接返回，后面配置空间的
  路由器本轮不同步（Python `run_round` 同样只有 `finally`）。仓库层已对单条记录容错，实际很难触发；如需隔离，建议记录配置空间 ID
  后继续下一个。
- **L7 — 关机打断「已写 running、尚未写设备」的同步，重启后仍要求人工确认。** 按设计关机后不做本地提交，`running` 回执重启后
  转为 `paused`（与 Python 进程被杀时一致）。若想区分「从未写设备」，需要一次关机后的不可中断写，超出 D-R12-3 的范围，不改。

## 待 batch-6 合入后复验（O3-6b RouterJobLock）

本分支的 `JobLock`/`syncJobs` 是临时 L1。我对照了 batch-6 的 `routers/joblock.go` 与 `wire.go`：

| 项 | R14 临时实现 | batch-6 `RouterJobLock` | 集成要求 |
|---|---|---|---|
| 键 | `type\n去尾斜线 endpoint`，无记录时 `profile\nid` | 相同（`routerJobKey`） | 一致 |
| 返回 | `(ctx, release, ok bool)` | `(ctx, release, error)`；先查 `ctx.Err()`，忙为 `KindConflict` + `messageEditBusy` | `StartJob` 可直接返回其错误（文案一致，见 L1）；`Sync` 必须把 `KindConflict` 映射为 `messageSyncBusy`（「再试」），取消错误原样返回 |
| repo hook | `wireRuleSync` 注册，整条 `Router` 解码失败时返回忙（fail closed） | `jobLockHook` 只取 `type`/`endpoint` 字符串 | 删除 R14 的 hook 注册，保留 batch-6 的 |
| 锁表 | `syncJobs`（名为 `router-job`） | `routerJobs`（同名） | **必须删除 `syncJobs`**：两张同名表互不排斥 |

风险：两边改的是 `wire.go` 的不同行，合并可能**没有文本冲突**；`Wire()` 先调 `wireRuleSync()` 再注册 batch-6 的 hook，
结果 repo 提交守卫与 R12 发布用 `routerJobs`，规则同步用 `syncJobs`，发布与同步可以同时改同一台设备。为此加了集成守卫
`TestR14ReviewRepositoryHookSharesJobLock`：经 `Wire()` 注册的 repo hook 持锁时，`Sync` 与 `StartJob` 必须报忙。本分支通过；
若合并后两张表并存，它会失败。合入 batch-6 后请复跑此用例和 `TestR14JobAndScheduler`、`TestR12StartPublish*`。

## HR 清单逐项

1. **触发与节流。** 30 s 首延迟、30 s 周期（`lifecycle.RegisterPeriodic`，在 round 中 recover）；单 worker（`syncWorkers` 以
   DataDir 为键，round 与 StartJob 共用，忙则本轮跳过/409）；disabled、未登记、paused、未到期均不下载；`running`/`queued`
   不看间隔；`checked_at` 无法解析视为到期；`rule_sync_interval` 缺失按 60，类型不符经 X1 容错为 60（Python 的 None 会使整轮
   崩溃）。每轮一个下载缓存（16 MiB FIFO），按声明重新校验。
2. **签名与基准。** `SyncControls` 字段与 Python 一致；路由器字段取存储记录原始字节的投影，凭据只以摘要落盘；canonical
   编码排序键、`UseNumber`。Capture 用 R12 的 IPv6 overlay 后路由器（R12 F1）比较发布配置与最新配置的签名、pin 的
   proxy_cidrs、策略目的集合并 `VerifyROSPolicy`。D2 切换后旧基准全部 paused，需手动发布一次（已登记）。
3. **复用 R12。** `Inspect`/`BuildPlan`/`Verify` 原样调用；`Verify` 的任何错误都补偿（F-R12-3，作者有 Python 反向对照）；
   不改 R12 的 L1/L2 约定：Capture 在发布者锁下运行，不再取锁；Sync 取 L1（try）后取 L2（等待）。
4. **与 Python 行为对齐。** 读了作者对照中的 `nat`、`nat_reference`、`delete_failure`、`verify_failure`、IPv6 `next_hop`
   与 service 的 paused/failed 分类；读码核对了 `restrict_plan`、`_apply_delta` 的每个分支、外部引用（与 Python
   `references` 相同：两个列表名、带 `!` 取反）、写入顺序与日志文案、`_save_manifest` 的比较。上面 M2、L1–L3 是找到的差异。
5. **真实 Runtime 下的关机与断连。** 作者用例覆盖读设备中关机、写目的中断开/关机、叶锁等待取消；M1 修复后 owner 在关机后的
   `ctx.Err()` 判断可靠。请求断开不影响（Sync/StartJob 全程 Detach）。
6. **脱敏。** 回执消息：`RouterError` 经 `routeSafeError`（endpoint、密码），下载错误经 `weburl.SafeErrorDetails`，日志经
   `diag.EventMessage`；用户名保留；公开投影只有计数、版本前 12 位与状态。
7. **坏记录隔离。** 回执无法解析/基准缺字段 → `paused`（Python 对缺字段会在之后 KeyError 变成反复 failed，Go 更安全）；
   路由器记录经 X1 容错；一台路由器失败不影响同轮其他路由器（L6 是配置空间级别的例外）。

## 需要 L5 实机验证

1. RouterOS CHR：规则源改为不回响应的地址（防火墙丢弃或挂起的 HTTP 服务），确认同步记为「失败，将按间隔重试」而不是暂停；
   恢复后下一个间隔自动成功（M2）。
2. CHR 上连续两次规则刷新：IPv4 目的增删、策略地址列表成员、NAT 免转换目的成员按顺序增删，`added/removed` 与日志正确。
3. 同步写入中途（第一条新增路由之后）向面板发 SIGTERM：关机后无设备写入；重启后回执为「上次同步未正常结束」，手动预览
   发布能接上归属（M1 修复后的 Go 行为）。
4. 同步进行中在设备上手工改动一条受管路由 / 添加引用 `cf-mihomo-nat-dst` 的防火墙规则，确认回读失败后只撤销自己的写入、
   保留外部编辑，并暂停。
5. 一次改变 IPv6 Fake-IP 或 IPv6 规则集路由归属的手动发布之后，回执为 synced（R12 F1 与 R14 Capture 的联动）。
6. batch-6 合入后：手动发布与「立即同步」同时触发同一台设备，后到者得到 409（单一 L1）。

## 验证命令与输出

```sh
# 反向对照：新测试放到 05f09a5f8 的产品代码上（只恢复 rule_sync.go / rule_sync_rules.go / rule_sync_adapter.go）
RT_HOST="$R14_TEST_HOST" rt -- bash -lc 'cd server && go test -count=1 ./internal/routers -run "TestR14Review|TestR14ClassifyPython"'
# FAIL：ClassifyPython 3 个新例（domain:/full:/CR）、PreResponseTimeoutIsTransient（远程规则下载超过大小或时间限制）、
#       StartJobBusyMessage（…请稍后再试）
RT_HOST="$R14_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=1 ./internal/lifecycle -run TestR14Review'
# 修复前 FAIL：141/200（spawn）、139/200（round）
RT_HOST="$R14_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=10 ./internal/routers -run "^TestR13ReviewNoticeWaitingForLeafLock$"'
# 修复前 FAIL 3/10：shutdown lost cancellation

# 修复后
RT_HOST="$R14_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=5 ./internal/lifecycle -run TestR14Review && go test -race -count=20 ./internal/routers -run "^TestR13ReviewNoticeWaitingForLeafLock$"'
# ok configflow/internal/lifecycle 1.165s；ok configflow/internal/routers 40.651s
RT_HOST="$R14_TEST_HOST" rt -- bash -lc 'cd server && go vet ./internal/routers ./internal/lifecycle && go test -race -count=1 ./internal/routers -run "TestR14|TestR13|TestR12Review" -timeout 20m'
# ok configflow/internal/routers 39.144s
```

lifecycle 直接使用者的定向 race 与 `go vet ./...` 结果见文末「最终定向检查」。未跑完整 gate、proddiff、全仓 race。

## 提交

- `264ffbb55` go(lifecycle): order job cancellation before the detached parent at shutdown（M1）
- `3c492b520` go(routers): R14 review fixes for download timeouts, busy text and classifier lines（M2、L1–L3、集成守卫）
- 本报告所在提交：本报告、behaviour-changes、PROGRESS R14 状态、交接文件结论。

## 最终定向检查（指定测试服务器，`3c492b520` 树）

```sh
RT_HOST="$R14_TEST_HOST" rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/lifecycle internal/routers)" && echo gofmt-clean && go vet ./... && echo vet-ok && go test -race -count=1 ./internal/lifecycle/... ./internal/lockx ./internal/ctxx ./internal/notifications ./internal/tasks ./internal/httpx ./internal/rulefetch ./internal/agents ./internal/server -timeout 20m'
```

```
gofmt-clean
vet-ok
ok  configflow/internal/lifecycle 3.497s
ok  configflow/internal/lifecycle/lifecycletest 1.022s
ok  configflow/internal/lockx 6.066s
ok  configflow/internal/ctxx 1.012s
ok  configflow/internal/notifications 26.533s
ok  configflow/internal/tasks 1.908s
ok  configflow/internal/httpx 64.750s
ok  configflow/internal/rulefetch 36.435s
ok  configflow/internal/agents 775.059s
ok  configflow/internal/server 8.688s
```

M1 改的是 lifecycle（所有 detached job 与 round 的取消路径），批次 gate 需要跑全仓 `-race`；其他包未在本分支跑。
