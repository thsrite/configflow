# R14 自动规则同步 — ported，待 HR 复核

范围：`rule_sync.py`、`rule_sync_rules.py`、`rule_sync_store.py`、`rule_sync_write.py` 和 C07 `_router_rows`。无新路由，R15 负责 rule-sync HTTP 三个 method。基线 `go@c5f5ff349`，分支 `go-port-r14`，首个实现提交 `2e8925789`；对照/边界修正为本文件所在提交。只做合成数据和 loopback routerfake；未 push、未运行 gate/全仓 race/proddiff，未操作实验设备或生产。

## 已实现

- `RuleSyncService`：`Capture`、`SyncControls`、`Sync`、`StartJob`、`RunRound`。`CaptureManual` 由 `Wire` 安装，持有发布者的 L1/L2 时直接调用，不再次取锁。
- 规则分类：4 MiB/100000 行、IPv4/IPv6 分别 20000 条、128 源、总 16 MiB；严格 YAML payload、域名不解析 IP、非公网前缀忽略、逐源清空保护、本地源变更保护。
- 下载：使用 C14 rulefetch（连接 5s、读取 10s、总 30s），每轮 16 MiB FIFO，URL+system_config 共享传输，但每个声明独立校验；每轮结束丢弃缓存。
- 受限增删：R12 Inspect/BuildPlan/Verify；R07/R08 归属守卫、Create/DeleteAddresses、ROSPolicyUndo；R10 IPv6RoutesApply/Restore。只更改已登记的目的地址/静态路由，不自动迁移旧 NAT、改变来源/网关、启用 IPv6 或切换下一跳。新增路由先于 NAT 成员，移除成员先于路由；policy manifest 保留依赖顺序和未知 spec 字段。
- 回执：原子临时文件/fsync/rename，叶锁后和替换前检查 ctx；保持旧记录的未知字段、规则展示顺序、profile 隔离。公开 projection 不包含凭据、内容、基准和设备 ID。
- C07 rows/runner、notifications.TaskStatus；30s 首延迟/周期，单 worker，disabled/未登记/paused/未到期时不下载，不创建每路由器 worker。running 重启恢复为 paused，孤立 queued 可由调度恢复。

## 并发与集成约定

本基线尚无 O3-6b `RouterJobLock`。暂由 `JobLock` + `syncJobs` 集中提供一个 `lockx.Keyed` L1，键为 type+去尾斜线 endpoint（不存在时 profile+id）；repo hook、Sync 和 R15 调用者必须共用它。**O3-6b 合入时应委托统一入口、删除临时锁，不能保留两把独立锁**。L2 使用原有 RouterOperationLock。没有 flock，没有新增依赖或后台裸 goroutine。

所有 write owner Detach，保留 Runtime 关机取消；commit 点前检查 ctx，设备 I/O 保持可取消。R14 没有扩大 R12 的 D-R12-3 不可中断特例：关机取消后停止设备/本地写入，已写 running 回执供重启后的人工确认。R04–R11 中设备写到一半时尚未进入内存 manifest 的 O14 跟进仍属原有问题。

## Python 对照与覆盖

`testdata/r14_reference.py` 运行真实 Python，设备双方使用同一个重置后的仓库 routerfake；比较结果、中文错误、设备最终表、写请求序列和 manifest。`r14_boundary_test.go`：20 个分类、11 个 IPv4/policy/NAT delta、4 个实际 service；`r14_ipv6_test.go`：5 个 IPv6 delta/回滚/跳过分支，共 40 个对照案例。只对 service 的三种时钟字段去值；签名按 D2 重新生成，其他业务字段不归一化。

- `test_router_rule_sync.py` → boundary/service/safety/runtime：来源、受限增删、未登记 cf 保留、无关草稿阻断、本地修改、逐源清空、no-op 不增修订、失败恢复、单 worker、暂停文案、公有回执。
- `test_routeros_nat_list_sync.py` → DeltaPython 的 nat/nat_reference/delete_failure：成员顺序、外部引用、失败补偿；旧 NAT 迁移限制复用 R07 NATMigration/NATMemberUpdate（R07/R12 定向回归）。
- `test_router_ipv6_routes_sync.py` → IPv6DeltaPython：正常目的增删、写失败、未登记、换下一跳、现场未知。
- `test_router_repository_safety.py::test_rule_sync_persists_rollback…` → ManifestRollbackPersistsLatestState。
- `test_scheduled_tasks.py` router row → TasksRows；`test_rule_cache_identity.py` router consumer → SourceSelectionAndCacheIdentity；`test_remote_rule_consumers.py` → DownloadsProductionParameters（同 URL 第二个不兼容声明必须拒绝）；profile clone receipt half → ReceiptCompatibilityAndRuleOrder。
- `test_router_ipv6_integration.py` sync 部分 → IPv6DeltaPython、R12 已验证的 captureRouter overlay（定向复跑）、Fake-IP/ND/PD pending guards。HTTP/auth/settings/body cases（原 test_router_rule_sync 的 API 用例）留 R15，不声称 HTTP 差分已完成。
- `r14_runtime_test.go`/`r14_delta_runtime_test.go`：真实 Runtime.Start/BindRequest/Shutdown 的设备读取消、断开后目的写完成、关机后停止设备和本地写、leaf 等待取消、单 worker/同连接克隆锁/任务移交/Spawn 拒绝释放；不覆盖生产超时参数。raw HTTP 验证真实 GET/DELETE 空 body、Basic auth、PUT 字节/Content-Type；下载 transport 仅旁观参数、不改写。
- Step B findings 派修表没有归 R14 的条目；X04/X07/T25 等模块相关锁/重启/回执行为由以上测试覆盖，R15 HTTP 边界仍待后续。

## HR checklist

- R04：沿用共享驱动读写、batch proof 和 UndoOf；没有新 console parser；任意回读错误均进入补偿。F-R12-3 反向对照明确证明 Python ReleaseError 留下目的更新，Go 撤销更新并保留外部编辑。
- R05：失败使用 RouterError/中文常量；不存在的 ID 不作为归属；完整稳定配置守卫，撤销前重新核对；读回建立索引，不逐项扫描整张静态路由表；所有失败类别补偿。
- A19：YAML 用 yamlx/yaml.v3 安全节点，不展开别名；缺内容/无法解析 fail closed，IP 处理复用 netipx，无 Python 仿真。
- R06：真实 production timeout，测试不缩短设备参数；真实 raw 请求校验；凭据用共享 routeSafeError/diag/weburl，用户名保留。
- R12：Capture 使用发布后的 IPv6 overlay；Capture 不重拿 L1；Go 原生 canonical 签名；manifest 比较 latest，回滚即使等于最初状态也写回；panic 只记 ID，不记录载荷。
- R13/X1：复用统一 routersOf/Router.UnmarshalJSON，不另写 router 容错；disabled/坏邻居不会阻断其他路由器；原始记录留作 repo CAS。

## 行为差异与限制

- D2：controls 使用 Go canonical JSON，可能与 Python 基准不同，切换后要求一次手动「预览发布」重建基准。回执 key 对合法 profile/router ID 仍兼容，无旧签名仿真。
- F-R12-3：修复 Python 回读 ReleaseError 不补偿的问题（已由设计批准）。
- 总 30s 下载预算从请求前开始，包含共享客户端的重试/响应读取；比 Python 收到响应后才开始计时更有界。
- 未增加用户功能或操作入口，现有 doc/ UI 文档无需新增截图。L5 实机尚未执行：CHR 上目的列表/NAT/IPv6 连续刷新、写失败/外部编辑、同步中关机与重启人工确认。

## 验证

仅指定测试服务器。报告以 `R14_TEST_HOST` 表示会话已固定的 SSH 目标（不把基础设施地址写入仓库）。`RT_HOST="$R14_TEST_HOST"` 固定会话指定服务器；未尝试其他主机。

```sh
RT_HOST="$R14_TEST_HOST" rt -- bash -lc 'cd server && gofmt -w internal/routers/rule_sync*.go internal/routers/r14*.go internal/routers/wire.go && test -z "$(gofmt -l internal/routers)" && go vet ./... && go test -race -count=1 ./internal/routers -run "TestR14" -timeout 10m && go test -race -count=1 ./internal/repo -run "Test.*Router|Test.*Publication|Test.*JobLock" -timeout 10m && go test -race -count=1 ./internal/tasks ./internal/rulefetch -timeout 10m && go test ./internal/routers -run "^$" -fuzz "^FuzzR14Classify$" -fuzztime=60s -parallel=2'
```

全部 exit 0；format 无输出、vet 无输出；routers 26.606s，repo 52.377s，tasks 1.595s，rulefetch 34.889s；fuzz `PASS`，287337 次执行，61.043s，无崩溃（237 个新增 interesting 输入在服务器 Go cache，不提交生成噪声）。最后仅补了 leaf owner 自己 Detach/对应断开回归和 R15 `NeedsBaseline` adapter；最终复跑如下（exit 0）：

```sh
RT_HOST="$R14_TEST_HOST" rt -- bash -lc 'cd server && gofmt -w internal/routers/r14_delta_runtime_test.go && go test -race -count=20 ./internal/routers -run "TestR14RuntimeDestinationWriteBoundaries" -timeout 3m && go test -race -count=1 ./internal/routers -run "TestR14" -timeout 10m && test -z "$(gofmt -l internal/routers)" && go vet ./...'
```

输出：`ok configflow/internal/routers 2.300s`（目的写入断开/关机各 20 次），`ok configflow/internal/routers 31.258s`（全部 R14）；gofmt/vet 无输出。最终测试内的停止回调以 channel 作同步屏障，避免客户端先收到取消后读取尚未记下的 call-count，原先偶发的测试断言窗口已关闭；未放宽设备请求断言。分类器与 60 秒 fuzz 时相同，不重复 fuzz。

## 既有 R13 问题（medium，交协调者/复核者）

`TestR13ReviewNoticeWaitingForLeafLock/shutdown` 间歇报 `r13_review_test.go:159: shutdown lost cancellation`；没有观察到关机后通知落盘。R14 尚未执行前的 R13 分组就会失败，且 R13 产品与测试未修改。

1. 共享路径定向回归（`go test -race -count=1 ./internal/routers -run "TestR14|TestR12|TestR13|TestR0[478]|TestRouterOS|TestOwnerLifetime|TestMissingJobLock" -timeout 25m`）：唯一失败为此用例，173.870s；R14/R12/R04/R07/R08 等未报失败。
2. 单独 `go test -race -count=3 ./internal/routers -run "TestR13ReviewNoticeWaitingForLeafLock" -timeout 5m`：同一失败，10.520s。
3. 测试服务器独立临时副本：复制 server/backend，删除全部新增 rule_sync*.go/r14*.go、恢复 `c5f5ff349` 原始 wire.go（其余原文件未变），再 `go test -race -count=5 ./internal/routers -run "^TestR13ReviewNoticeWaitingForLeafLock$" -timeout 5m`：5 次中 2 次同一失败，15.836s，`BASELINE_R13_EXIT=1`。临时副本已清理。

这是基线已有取消结果竞态，未将它改成通过，也未把共享回归称为全绿。建议由 R13/新 Opus 检查通知取消被 Safely 吞掉后 Flight/等待者完成与关机之间的结果竞争。R14 自己的真实 Runtime/leaf/目的写用例均通过。

## 新增文件行数

| 文件（internal/routers 下） | 行数 |
|---|---:|
| `rule_sync.go` | 653 |
| `rule_sync_adapter.go` | 243 |
| `rule_sync_controls.go` | 128 |
| `rule_sync_json.go` | 145 |
| `rule_sync_messages.go` | 92 |
| `rule_sync_rules.go` | 311 |
| `rule_sync_store.go` | 218 |
| `rule_sync_tasks.go` | 144 |
| `rule_sync_write.go` | 525 |
| `r14_boundary_test.go` | 284 |
| `r14_delta_runtime_test.go` | 82 |
| `r14_ipv6_test.go` | 106 |
| `r14_runtime_test.go` | 361 |
| `r14_safety_test.go` | 261 |
| `r14_service_test.go` | 275 |
| `testdata/r14_reference.py` | 102 |

## 完成状态

已实现四个 Python 模块全部业务功能；R15 HTTP 与 L5 实机仍按既定归属后续完成。没有未提交半写代码、调试 fixture、临时数据库或新依赖。待新 Opus HR 复核；协调者统一接 O3-6b 并运行批次 gate。
