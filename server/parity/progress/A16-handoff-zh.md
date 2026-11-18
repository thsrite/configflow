# A16：Agent 规则同步、投影与设备快照交回

状态：ported，待独立 Opus HR 复核。分支 `go-port-a16`，按任务从 `go-opus-review-a14@b6ce4ac29` 起步，随后变基到该依赖分支的 `53d43b577`。没有子 agent、push、生产或真实设备访问；所有测试通过 rt 在 .64 上执行，新增网络服务只监听 loopback。

## 完成范围

- GET、PUT `/api/agents/<agent_id>/rule-sync`：JWT、独立的设备绑定、查询/长度守卫、精确布尔设置、HA 配对事务、中文错误、只公开状态三字段与 no-store。路由注册通过完整 server 注册测试。
- `internal/agents/rulesync`：SettingsFor、源投影/水合、有效规则签名、内容/结构分类、上游 withheld、来源 fingerprint、唯一规则文件复用、Mihomo/OxiDNS 双层隔离、PrepareCandidate、FetchSnapshot、worker、两槽轮询与请求后启动。
- 快照固定使用 AgentOrigin；Bearer、GET 空 body、Accept gzip、(3,90)、NoProxy/NoRedirects；压缩、全部解压字节、tar 扩展头、成员数、单文件、清单、路径归属、角色、摘要、设备身份及已确认主配置版本逐层验证，不解压到磁盘。
- worker 保持 5 秒防抖、60 秒重试、600 秒比较重查，持久化 native task 后重启续接，不重复 POST；快照未验证或并发编辑时不推进基线；结构同步只发布 reload_only 包。
- C07：真实 `SetRuleSyncRow`，终态转换一次记录，使用 `notifications.TaskStatus` 的共享通知回调。A17 的 DNSFollowRow/runner 没有伪造或替代，agent slot 仍由双方一起满足。
- C32：Wire 注册 `agents.Hooks.PinnedWithheld`，按实际默认配置空间解析各设备 receipt、复用同一投影，恢复该字段。没有改 C32 写 adapter、repo/store 或复制字段 patch；合并后必须保留 CFG-11 的“只写真实变化字段”。
- C27：调用既有 routing、action、catalog、hydration、generator、resource helpers，只补薄导出；原有只读 SyncSettings fallback 仍与 SettingsFor 严格布尔语义相同。
- A18 接口：Baseline、PrepareFunc/PrepareCandidate、FetchSnapshot、SettingsFor、ProjectRuleChanges/RuleChanges/RuleSignatures、RuleSourceFingerprint/UnchangedRulePaths、MihomoRuleEntries、ReadBundleFiles、PublishedUpstreams、IsolateRuleBundle、SetPairAdvance。HAEnabled 和 Manager.HAPair 在 agents 共享包内，compat.go 保留；A17 已确认无需互调设备 snapshot。

## 安全、取消与可靠性

写入口经共享 `deploy.RunDetached`（内部 Detach；tracked 调用复用已有 owner），并在每个自有提交/设备调用前检查 ctx.Err。真实 Runtime.Start/BindRequest/Stop 测试证明：入口前关机零写入；设备 I/O 中关机不推进 source receipt/任务状态；浏览器断开后完整完成。使用 `lockx` 的进程内 L1 worker key，按实际 DATA_DIR 路径解析符号链接，不创建 worker.lock；没有裸后台 goroutine。

比较记忆按 Service/Agent 隔离，删除设备即回收；两槽 pending 直到下一轮才回收，保持 Python 每轮最多接纳两个及轮转顺序。Runtime 排空后没有仍在运行的 worker；空闲轮不再提交或刷新。取设置、凭据快照用只读 settings/轻量 agent 接口，presence 只投影日志字段，不复制历史。

状态、日志、任务及 presence 文本使用共享 redact/diagnostics/weburl；URI userinfo、token、编码凭据、控制字符不外泄。presence 复用原有 ObservePresence 状态机，没有另写一份。正常名称保留；URL 内 userinfo 按共享日志策略移除。错误日志只记错误类型或固定 panic 文本。

### 已修的 Python 问题与批准差异

1. **高，Python 可靠性 bug**：`_publish` 在任意异常时无条件将部署写成 failed，即使已经 submitted。Go 仅用 A01 `OnlyUnsubmitted` 记录准备失败；已提交任务交给原有轮询，不把可能已上机的任务误标终态。
2. **中，Python 日志安全问题**：规则状态及 presence 的设备名/文本可能含凭据。A16 的日志边界先用共享凭据快照脱敏，原注册记录不变。
3. D2：持久化 state.key 使用既定 Go 原生 CanonicalDigest，不新增 Python JSON 模拟。切换时旧 debounce key 会重建；已受理 native task 先查询，完成后若 key 变化则再检查/刷新，不接受未验证内容。YAML/tar/gzip 的格式按既定 D1/D2 结构合同比较。
4. O12：不创建 `.agent-rule-sync/<hash>/worker.lock`，只保留真正 receipt/归档目录。A18 尚未注册的 SetPairAdvance 按设计为无设备作用的 no-op；配对发布仍经 A14 的 fail-closed publisher。

### 已知集成阻塞（不是声称已修）

- **高，共享层已知窗口，归 O3-6c**：本依赖树仍没有最终 SQLite CommitContext 和 taskstatus.RecordContext。A14 的 BeforeCommit 关机复现仍为显式 SKIP，见 review-a14.md。A16 已在调用之前检查 ctx，但不能保证旧叶锁等待期间关机后不提交；未复制临时 patch 或自行改共享锁。
- A14 尚未合入当前本地 go（最终检查时 go=e0e9d9d45）。遵照“待 A14 合入后再 rebase go”，本次只变基到最新 A14 复核依赖分支。已向新协调者 f8801d39 说明：A14/O3-6c 落地后需 rebase go、切换共享带 ctx 的记录入口、补锁等待/最终提交取消回归，并在合并树确认 CFG-11 行 revision 保真。
- A18 pair advance/publisher 的实现、A17 DNS-follow runner、真实设备/PVE L3 属各自模块及实验批次，本任务不模拟它们。

## 边界验收与 Python 测试映射

按 Leaner verification，不逐形状穷举、不跑完整 gate/proddiff；以下都是实际执行的边界。

| Python 来源 | Go 覆盖 |
|---|---|
| test_rule_sync_projection.py | TestProjectionAgainstPython：真实 Python，作用域、内容/结构/无变更、原生路由、缺共享源拒绝；TestIsolationAgainstPython：两个服务的有效隔离和插件/执行链/顺序拒绝；预览与实际归档逐文件/角色/字节比较；深层和循环 alias 拒绝 |
| test_rule_sync_snapshot.py | TestSnapshotAgainstPython：正常 live bytes 与各类代表篡改，含 wrong-size-type 的同一条中文摘要错误；TestSnapshotIntegrity；FuzzSnapshotArchive；真实 loopback GET 的原始 headers/body/生产 timeout/transport 断言 |
| test_agent_rule_sync.py | TestRuleSyncHTTPAgainstFlask：两个 method 的正常/失败，逐次比较完整 DB/文件；TestWorkerStatesAgainstPython：真实 Python worker 的防抖/刷新/空闲状态及 provider 调用；其他 worker 测试覆盖失败后基线不变、重试、accepted native 重启不重发、并发编辑、reload-only、HA pair CAS、两槽轮转、Runtime、任务行和 withheld |
| test_agent_light_reads.py（A16 延期） | history=false 读取、按 head/receipt/cache/settings 记忆比较；连续 idle 后 DB 字节和刷新次数不变；删除比较记忆、排空 pending 测试。未复制指标历史 |
| test_udp_fallback.py、test_oxidns_domain_only_native.py | 复用现有 C09/C25/C27 生成/路由语义，签名保留 udp_fallback/ntp_direct；投影 native/template 结构对照和隔离；真实设备热加载仍由 L3 证明 |
| test_operation_logs.py、test_scheduled_tasks.py agent_rule_sync 半行 | 状态转换与日志级别、终态一次记录、任务 History/blocked 状态及不泄露凭据；A17 的半行留给 A17 |

HTTP fixture 仅屏蔽规则同步定时驱动，两端一致；worker 本身由独立测试直接驱动，并使用真实 Runtime/loopback native APIs。没有调整任何待验证的生产请求参数。仅 timestamp/原生 YAML 格式按既定规则比较，不屏蔽配置、凭据、DB 行或文件。

## 实际命令与结果

主机固定 192.168.0.64。最终一组命令的开始 load=12.08/10.79/10.64，结束=13.61/12.77/11.48。验收点为最新 A14 依赖分支上的 A16 `20de1bf23`，之后的 presence/通知接线又补跑完整 rulesync race（见下）。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; mkdir -p "$TMPDIR/a16-targeted"; cat /proc/loadavg; cd server; test -z "$(gofmt -l internal/agents/rulesync internal/agents/ha.go internal/api/agents/rulesync.go internal/api/agents/messages.go internal/api/agents/agents.go internal/api/agents/difftest/rulesync_diff_test.go internal/oxidns/rulesync.go internal/deploy/adapter.go internal/server/server.go)"; go vet ./...; CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout=10m ./internal/agents/rulesync > "$TMPDIR/a16-targeted/rulesync.log" 2>&1; tail -3 "$TMPDIR/a16-targeted/rulesync.log"; go test -race -p 4 -count=1 -timeout=10m -run "^(TestHistoricalHAPairReadDoesNotReconcile|TestBuildSharedSourcesAndProgress|TestPairGateUsesAuthoritativeActiveJournal|TestOxiDNSPublicationAttestsBeforeActivation|TestPinnedWithheldHookIsBestEffort|TestFirstRegistrationReturnsTokenAndPersistsIt|TestAuthenticatedDuplicateRegistrationNeverReturnsToken)$" ./internal/agents ./internal/deploy ./internal/api/agents ./internal/api/rules; go test -race -count=1 ./internal/server; CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=10m -run "^TestRuleSyncHTTPAgainstFlask$" ./internal/api/agents/difftest; go test -run "^$" -fuzz "^FuzzSnapshotArchive$" -fuzztime=60s -parallel=2 ./internal/agents/rulesync > "$TMPDIR/a16-targeted/fuzz.log" 2>&1; tail -5 "$TMPDIR/a16-targeted/fuzz.log"; cat /proc/loadavg'
```

退出 0；gofmt 无输出、全树 vet 通过。末尾：

```text
ok configflow/internal/agents/rulesync 61.424s
ok configflow/internal/agents 15.646s
ok configflow/internal/deploy 9.841s
ok configflow/internal/api/agents 4.175s
ok configflow/internal/api/rules 7.165s
ok configflow/internal/server 1.743s
ok configflow/internal/api/agents/difftest 8.600s
fuzz: elapsed: 1m0s, execs: 599521, new interesting: 73 (total:75)
PASS
ok configflow/internal/agents/rulesync 61.028s
```

presence/通知接线后增量：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back server/internal/agents/rulesync/adapter.go --back server/internal/agents/rulesync/service.go --back server/internal/agents/rulesync/service_test.go -- bash -lc 'cd server && gofmt -w internal/agents/rulesync/adapter.go internal/agents/rulesync/service.go internal/agents/rulesync/service_test.go && go vet ./internal/agents/rulesync && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=10m ./internal/agents/rulesync'
```

退出 0：`ok configflow/internal/agents/rulesync 60.240s`。解析/fuzz 代码在这次增量中不变。

早期新增 fake 未读完整 PUT body，导致关机后 fake 的 HTTP/1 连接未退出、httptest.Close 等待；已仅对本轮 PID 发 SIGQUIT 取栈并停止。修复 fake 消费完整 body、有界释放 gate，未修改生产 HTTP 参数；对应 Runtime/并发/任务行/真实 Python worker 组合 race21.324s通过，其后上述整包 race 均通过。这不是一次被忽略的产品测试失败。

HTTP fixture 的最终稳态/错误服务补充检查及最终提交 hash 见文末追加。

## 收尾自查与最终增量

- **高，Python 同样存在，已修**：OxiDNS GET 更新状态期间发生配置绑定变化，旧流程仍接着 POST 旧来源的下载。现在在首次 POST 和 paths-unknown 回退 POST 前再次使用同一身份/设置守卫。真实 loopback GET 回调并发重绑定；修复前明确失败 `posted a download after profile changed`，修复后请求数为0，没有推进 receipt。
- **中，Python 同样存在，已修**：HA enable journal 可在 configure 的初次检查后才提交，旧流程仍覆盖双机设置。单机和配对事务内都重新检查 enable；真实 SQLite 事务的拒绝前后字节不变。新增回归修复前报 `concurrent enable guard: <nil>`。
- **中，移植自查，已修**：worker 必须使用心跳推导的离线状态，不能直接用 GetAgentByID 的原存储 status；stale-heartbeat 负控已复现并修正。执行视图还归一有效 enabled 与缺省绑定，写回不改变原记录。
- **中，移植自查，已修**：withheld 的 profile helper 现在直接复用 C27 ProfileAgents，不保留私有筛选副本；明确区分字面 `default` 和未指定配置空间，排除 disabled agent。测试覆盖修改默认配置空间后两个实际空间及空参数。
- **低，移植自查，已修**：布尔 size 的快照错误原先被 deploy.File 的严格 decoder 提前吞成通用错误。独立 typed wire 按验证顺序处理，真实 Python 12 类代表拒绝均一致。
- 设置只读 settings member；其他 typed agent 读取先去掉历史字段及不可读 sentinel。故意带会报错的 MarshalJSON 历史对象仍能读取设置/执行视图，证明不靠序列化发布历史取数据。Python reference 进程统一安装拒绝网络的 guard，只有 snapshot 的显式内存 fake 可覆盖它。

最终产品代码截止 `c241f29388d92187b1551f4e876dab08fa167469`。以下最后一轮在上述并发修复/轻量视图之后执行，解析代码仍与已完成60秒fuzz相同：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back server/internal/agents/rulesync/adapter.go --back server/internal/agents/rulesync/worker.go -- bash -lc 'cd server && gofmt -w internal/agents/rulesync/adapter.go internal/agents/rulesync/worker.go && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=10m ./internal/agents/rulesync && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=10m -run "^TestRuleSyncHTTPAgainstFlask$" ./internal/api/agents/difftest && go test -race -count=1 ./internal/server'
```

退出0：

```text
ok configflow/internal/agents/rulesync 69.567s
ok configflow/internal/api/agents/difftest 5.690s
ok configflow/internal/server 1.337s
```

之前的累计增量（不是一次新的完整门禁）：规则同步整包race51.506s/63.813s、HA/关机/轮转组合11.609s、扩展后的15项HTTP差分14.078s，均通过。最终只将共享HA纯谓词去掉jsonx导入、改用原生记录身份比较；源码格式和相关vet通过，补跑：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back server/internal/agents/ha.go -- bash -lc 'cd server && gofmt -w internal/agents/ha.go && go vet ./internal/agents ./internal/agents/rulesync ./internal/api/agents/... ./internal/server && go test -race -count=1 -timeout=10m -run "^(TestHASettingsPairAtomicity|TestHistoricalHAPairReadDoesNotReconcile|TestProfileFilteringKeepsLiteralDefaultAndDisabledAgents)$" ./internal/agents/rulesync ./internal/agents && test -z "$(gofmt -l internal/agents/rulesync internal/agents/ha.go internal/api/agents/rulesync.go internal/api/agents/difftest/rulesync_diff_test.go internal/oxidns/rulesync.go internal/deploy/adapter.go internal/server/server.go)"'
```

退出0：`rulesync 4.743s`、`agents 8.930s`，gofmt无输出。没有再跑完整 gate 或 proddiff，没有更改比较器/基线；完整合并门禁和新 Opus HR 由协调者执行。

业务提交（已跟到最新A14依赖分支）：

```text
fd55a2ec7 verify bounded authenticated live snapshots
0debce0a9 project opted-in scopes and isolate rule-only bundles
85dc0d840 reconcile device rules with tracked owners and task hooks
f5df78b98 verify HTTP persistence and native reconciliation boundaries
6f374bf3e align snapshot validation classes with Python
20de1bf23 retain two-worker rounds and prove preview isolation
0ebbc3acb guard concurrent settings and preserve lightweight device scope
4dac363ef stop native downloads after target rebinding
3790f8fe7 compare effective bindings in lightweight agent views
c241f2938 keep HA pair predicates on native record identities
```

最终报告提交的tip以交回消息为准。工作区交回前确认干净；不push。A14未合入go时不擅自重放整条依赖链到go；待通知后再做指定的rebase和共享提交接口增量。


## batch-6 集成增量（2026-11-18）

按协调者指定，从 `go-opus-review-a16@6464cd7c8` 新建 `go-a16-integrate`，执行：

```sh
git rebase --onto 0d43e3f1 53d43b577
```

`53d43b577` 是原 A16 的 A14 parent；仅重放 A16 作者与独立 HR 的 16 个提交，旧 A14 提交没有重复带入。新基点是 `batch-5@0d43e3f15`，保留其 `configexport.Wire` 并增加 `rulesync.Wire`。behaviour-changes 的两侧新增条目均保留。

本次生产改动：终态任务记录使用 `notifications.TaskStatus(owner ctx, dataDir).RecordContext(owner ctx, ...)`，沿用共享 O3-6c 在 leaf 锁内、替换前的取消检查，不复制锁或提交逻辑。适配 batch-5 的 `tasks.History` 双返回值并传播错误；共享脱敏返回值已改为 native JSON，复用现有 `agents.SanitizePublicMetadata` 桥接历史非字符串 presence 字段，转换失败时隐藏该字段。字符串字段的处理不变。

新增 `TestStateTaskStatusLockWaitRuntimeBoundary`：succeeded/failed × shutdown/disconnect 四个用例。用另一 Store 的现有 Now seam 持有真实 process-wide leaf mutex；通过 goroutine 栈确认实际 `saveState → lockedContext → Mutex.Lock` 等待后，才取消。真实 Runtime.Start/BindRequest/Spawn/Shutdown：关机后解除锁不得写 task-status.json/notifications.json、不得继续改 DB；请求断连后写入一次终态记录。DB 的终态更新在等待任务状态锁之前已经提交，测试不要求关机回滚之前的提交。另补历史对象型 presence 名称的脱敏与原记录不变断言。

负控：保留旧 `Record` 的定向 race 在 .32 运行，11.445s，两个 shutdown 子例均失败 `shutdown task-status write returned <nil>`，两个 disconnect 子例通过。这证明回归实际命中旧共享窗口。初次集成编译遇到上述两个已变更的共享签名，适配后再取得该负控结果。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/agents/rulesync/adapter.go --back server/internal/agents/rulesync/service_commit_test.go -- bash -lc 'cd server && gofmt -w internal/agents/rulesync/adapter.go internal/agents/rulesync/service_commit_test.go && go test -race -count=1 -timeout=5m -run "^TestStateTaskStatusLockWaitRuntimeBoundary$" ./internal/agents/rulesync'
```

本轮仅定向验证；完整门禁交由协调者在 batch-6 上执行。无 push、无子 agent、只用合成数据与回环 fake。

### .32 合并树验收

第一轮（固定 .32；开始 `/proc/loadavg`: `4.14 4.03 5.16 4/364 3217602`）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 --back server/internal/agents/rulesync/adapter.go --back server/internal/agents/rulesync/service.go --back server/internal/agents/rulesync/service_test.go --back server/internal/agents/rulesync/service_commit_test.go -- bash -lc 'set -e; cat /proc/loadavg; cd server; gofmt -w internal/agents/rulesync/adapter.go internal/agents/rulesync/service.go internal/agents/rulesync/service_test.go internal/agents/rulesync/service_commit_test.go; go vet ./internal/agents/rulesync ./internal/api/agents/... ./internal/server; go test -race -count=1 -timeout=20m ./internal/agents/rulesync; go test -race -count=1 -timeout=10m -run "^TestRuleSyncHTTPAgainstFlask$" ./internal/api/agents/difftest; go test -race -count=1 -timeout=10m -run "^(TestO36cStatusLockWaitRuntimeBoundary|TestO36cStatusShutdownDuringRecord|TestO36cRepoBeforeCommitRuntimeBoundary|TestRuleWritesPreserveUntouchedDNSRows)$" ./internal/taskstatus ./internal/repo ./internal/api/rules; go test -race -count=1 -timeout=10m ./internal/server; test -z "$(gofmt -l internal/agents/rulesync internal/api/agents/rulesync.go)"; cat /proc/loadavg'
```

```text
ok  configflow/internal/agents/rulesync 172.908s
FAIL configflow/internal/api/agents/difftest 14.158s
```

本包已包含新增锁等待 shutdown/disconnect、原有结构发布关机 T3、M1、真实 Python projection/isolation/snapshot/worker 对照；TestMain 的锁序断言通过。该轮在 HTTP 差分处停止，后面的包没有执行。HTTP 响应与完整 DB 均未报差异；文件差异仅为 A04 旧 `agent-busy_metrics.json` 启动迁移产生的 JSONL 排版：相同字段/值的成员次序与 `speed_recv: 2.5` / `2.50`。A16 专用夹具在复制到两侧之前将这一个旧文件换成当前 JSONL 格式，保留非空历史点以及其他监控文件，完整逐文件比较继续执行。A04 原有迁移用例、生产迁移实现、共享比较器和 baseline 均未修改。

只补跑受夹具修正影响的差分及前轮未执行的定向检查，不重复已通过的 rulesync 全包：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 --back server/internal/api/agents/difftest/rulesync_diff_test.go -- bash -lc 'set -e; cat /proc/loadavg; cd server; gofmt -w internal/api/agents/difftest/rulesync_diff_test.go; go vet ./internal/agents/rulesync ./internal/api/agents/... ./internal/server; go test -race -count=1 -timeout=10m -run "^TestRuleSyncHTTPAgainstFlask$" ./internal/api/agents/difftest; go test -race -count=1 -timeout=10m -run "^(TestO36cStatusLockWaitRuntimeBoundary|TestO36cStatusShutdownDuringRecord|TestO36cRepoBeforeCommitRuntimeBoundary|TestRuleWritesPreserveUntouchedDNSRows)$" ./internal/taskstatus ./internal/repo ./internal/api/rules; go test -race -count=1 -timeout=10m ./internal/server; test -z "$(gofmt -l internal/agents/rulesync internal/api/agents/rulesync.go internal/api/agents/difftest/rulesync_diff_test.go)"; cat /proc/loadavg'
```

退出 0；vet 通过、gofmt 检查无输出：

```text
3.12 3.89 4.88 4/371 3221591
ok  configflow/internal/api/agents/difftest 16.187s
ok  configflow/internal/taskstatus 1.266s
ok  configflow/internal/repo 1.669s
ok  configflow/internal/api/rules 6.907s
ok  configflow/internal/server 4.324s
3.36 3.87 4.83 3/354 3222142
```

HTTP 的 20 场景逐次比较完整 DB 和 DATA_DIR 文件；CFG-11 检查确认未改动 DNS 行的原始值和 revision 不变。A16 已按协调者要求标 `verified`；原报告中的 A14/O3-6c 阻塞由本节关闭，独立 HR 的 M1/L1 修复完整保留。没有新增未关闭的 A16 问题；A18 配对发布依赖与 L3 实机验证仍按原计划在对应批次执行。分支 `go-a16-integrate`，最终提交号随交回消息提供，进入 batch-6；未跑完整门禁、未 push。
