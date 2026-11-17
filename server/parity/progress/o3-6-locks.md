# O3-6a — 进程内资源锁与 owner 取消边界

分支 `go-o3-6`，当前基线 `go@28e6e0c1`（含 L1 复核、C29、R09）。无冲突变基。
本轮 test-only 提交 `49b95db20`；工作区交回前核对干净，不 push、不派子 agent、不在本机测试。
完整合入门禁仍由协调者重跑。

## L1 / O12 集成跟进（2026-11-18）

协调者在变基后的 `14cf7ee8` 上复验：gofmt、vet、两种 tag、全量 race、proddiff
（new=0/stale=0）均通过，只有 L1 compact-colon 的差分失败。
原因已确认：共享 `installationFiles` 按 O12 排除了 `.issue.lock`，L1 新测试却仍断言
快照有四项且包含这个锁。没有产品代码故障，也没有改生产实现。

修正仅在 `l1_installer_diff_test.go`：
- Python 快照必须恰有目录、一个 session JSON、一个 index，不再包含退休锁。
- 在原始文件系统单独断言 Python 的 `.issue.lock` 是已存在的普通文件。
- 在原始文件系统单独断言 Go 没有创建 `.issue.lock`，原有 Go 拒绝后无文件断言也保留。
- 目录权限严格为 0700，session/index 均严格为 0600；响应、Compose 和 Agent 注册断言未改。

以 `git diff --name-only dda8973d 28e6e0c1 -- server` 限定 L1/C29/R09 合入范围，
用 rg 检查其中 58 个测试及 testdata 文件的退休锁、flock、锁路径与文件快照引用。
除本次 L1 外，没有另一处要求退休锁存在的断言；另一处 `.issue.lock` 命中是
`installer/docker_ha_recorded_test.go` 已有的 O12 排除处理。`.config.lock` 和目标机锁未改。

本轮在 .64 前台执行：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/api/agents/difftest/l1_installer_diff_test.go)" && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/agents/difftest/ && go vet ./...'
```

退出 0；gofmt 无输出，difftest 输出 `ok configflow/internal/api/agents/difftest 200.487s`，
随后全仓 vet 无输出并通过。依协调者要求，本轮不重跑完整 gate。

以下保留首次交回（基于 dda8973d）的实现范围及验证历史，不把旧结果视为当前基线的最终全绿。

## 已交付

- `ctxx` 仅依赖 context，共享 cancelParent/Detach，lifecycle 保留转发。
  `Uninterruptible` 为本地原子批处理屏蔽父取消，下游再次 Detach 也不能恢复它。
  repo 写入口先 Detach；provider/repo/deploy 的本地批处理显式使用该边界。
- `lockx.NewLeafKeyed`：有上限、可取消、无用条目清理，不记录 L4、不掩盖祖先层级。
  `LevelDeviceControl` 表达 Sub-Store work→control 的 L2 子顺序，反向仍记违规。
- repo：`.config.lock` 保留 flock 并实际记录 L3；profile operation 改为按物理目录/profile ID
  的 L2 Keyed；缓存和生成文件改用有界叶锁。多份 Repository 对象共享物理资源互斥。
  `WithReadSnapshot` 在 L3 内提供一致读取，供核验→叶锁 CAS；禁止回调内写入及设备/网络 I/O，
  回调结束后快照失效。缺少 JobLock hook 时默认拒绝。
- taskstatus/notifications：进程内叶锁和原子写，不再创建或打开旧 flock。notifications 在锁外
  收集观察、锁内按版本合并；新的未变化快照和已提交通知操作都能阻止旧观察覆盖，包括 MarkRead。
- monitor：StateStore 阻塞 API 接收 ctx；每物理文件叶锁上限 5 秒。真实 repo 上保持
  L3 读取并核验 target/publication → L4 CAS，全程没有释放 L3 的核验窗口。
- weblistener：按目录共享 L1 try admission；Start/Status/Update 接收 ctx 并在 owner Detach。
  保留原校验/409 优先级，健康检查 5 秒、探测间隔 300ms，Clock 等待能收到关机取消。
- agents deployments：按物理目录/Agent/deployment 共享 L1，preparationRunning 使用 Held。
  内存锁不跨 repo I/O；Finish、提交失败、panic 展开及关机在提交完成后释放自身租约，
  失败申请者不释放其他任务的租约。
- installer：仅替换服务端 `.issue.lock`，IssueEnrollment 接收 ctx；目标 bootstrap.py 的
  `.compose-enrollment.lock` 原样保留。并发签发仍只产生一个 installation identity。
- Sub-Store：按物理 root 共享 work RWMutex(L2)、control Mutex(L2 子顺序)，沿用 45/90/45 秒。
  Tick/SetMode 显式 TryLock。Use/Maintenance 的回调接收 held ctx；嵌套 Use 复用有效读租约，
  不在等写者时嵌套 RLock，不允许读升写。启动等待使用 Clock，生产 20 秒/200ms 值有测试钉住；
  持久化 Monotonic 不改；资源条目引用包含等待者和持有者。
- 非 routers handler 传原始请求 ctx；discovery/health/MosDNS/WebDAV/Sub-Store/repo/adapter
  owner 负责 Detach。metrics 历史保存横跨 I/O，按 review-o3 改为可取消的 L2 operation 锁。

## JobLock 未接线的实际影响

生产 routers.Wire 暂未注册 JobLock。通用 repo 事务修改或删除**已有 router** 的连接字段
（type/endpoint/username/password/verify_tls）或 manifest 字段时，返回 ErrInUse，HTTP 409：
`路由器正在发布或同步，不能替换连接或删除记录，请稍后重试`，usages=[]，不提交。
已有 manifest ownership 检查可能更早拒绝。受影响的操作也包括改变这些记录的 import/overwrite、
profile 删除和 reset；新建 router 或只改非保护显示字段，不会仅因这个 hook 被拒绝。
`SaveRouterSettings`/`SaveRouterPublication` 保留“调用方已经持有 job lock”的契约，不重复取锁。
真实 routers Keyed 的接线留给 O3-6b，未放宽 fail-closed。
非测试 Go 文件本轮新增 878 行、移除 498 行，净增 380 行。

首次交回时 router HTTP 路由仍有 todo；当时 proddiff 排除设备 apply/route-apply 写入和 config import，
router-script generator 也明确尚未实现。因此绿色 proddiff 不代表这些写路径已覆盖。
已跑过的 proddiff 和差分未发现 JobLock 引起的失败；但最终基线的全部启用 difftest 尚未重跑，
不能从排除范围推断覆盖，协调者的完整 gate 仍是合入门禁。

## Python 遗留锁与差分

旧锁文件内容、inode、mtime 不变，不再新建；缓存删除/清理也不删除旧锁。
O12 已逐项列明 task-status、notifications、listener、deployment、installer、dashboard、规则缓存、
profile operation 和 Sub-Store 文件名；只保留实例锁（含 Python lifetime 兼容锁）、`.config.lock`
和目标机 bootstrap 的跨进程锁。真实跨进程服务边界仍由 O3-3 instance 测试保证。

三个 Flask 文件快照只排除精确 `.issue.lock`、`dns-monitor/<64hex>.lock` 和
`shared/rules/by-source/<64hex>.list.lock`，每处另断言 Go 没有创建对应锁。
其他文件、全部响应和实际 JSON/cache/config 数据继续逐项比较，没有排除整个目录。
installer corpus 的 review/issue/197 原本只验证打开遗留锁目录，已明确接受这个退休边界，
并由成功签发且旧目录原样保留的回归替代。资源互斥测试改为独立 Store/Runtime 对象竞争。

## 验证与门禁经历

所有测试通过 rt 在 .32/.64。相关包迭代 race、取消/超时、CAS 和旧文件哨兵已通过。
repo/Sub-Store 正常测试断言 Violations=0；故意反序用子进程隔离。替换短时限或自动假钟的
测试另钉生产值，叶锁/profile 用例覆盖串行、并行、删除/写入竞争、取消和条目清理。

- 两次 .64 gate（基于 64815165/d8f50b00）在排队时按协调者通知终止，退出 143，没有执行检查。
- .32 `/tmp/o36a`：proddiff 2831 cases、23 self-checks PASS，new=0/stale=0；race 仅旧 C27
  控制用例失败。它把已取消的请求交给新 repo owner，仍期待立即取消，导致等待自身未释放的写锁。
  测试改为负控使用已取消工作父级、正控使用原始已取消请求，保留真实竞争、Scope（含 updater）、
  精确 revision、合并及回滚断言；三个受影响测试 race count=3 通过（13.340s）。未改产品默认或探针上限。
- .32 `/tmp/o36a-2`：两种 tag、全量 race、proddiff new=0/stale=0 全通过；启用的差分仅上述
  三份退休锁文件痕迹不同。DockerHA 239、OxiDNS 69、refresh 31 次响应/设备比较及实际数据相等。
  协调者批准精确排除并补无新锁断言后，真实 Flask 定向复核通过：agents 13.651s、refresh 7.666s；
  gofmt/vet 通过。没有 JobLock 导致的现有差分失败。
- 最终命令（协调者指定 .32，无 -p）：

  `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'GATE_LOGS=/tmp/o36a-3 server/scripts/gate.sh'`

  覆盖 gofmt、vet、两种 tag（build/vet/lifecycle 测试）、全量 race、proddiff、全部启用 difftest。
  第三轮仍通过 gofmt/vet、两种 tag、proddiff（2831 cases，new=0/stale=0，23 项自检通过，
  coverage_regressions=0、unmapped_routes=0）。race 返回两项失败：Latin1 fixture 时序，以及
  C27 释放锁后的测试 5 秒完成断言；因此 gate 退出 1，没有进入启用 difftest。
  其他 race 包全部通过，未出现 data race。日志留在 .32 `/tmp/o36a-3`。

### 第三轮失败的定向复核与修正

Latin1：captureAuthorization 启动 1.5 秒 Accept deadline 后，原测试才创建 repo/Agent fixture。
负载下 fixture 耗尽时限，实际请求尚未发出，不能抓到 header。协调者批准将 newUpgradeEnv 提前；
1.5 秒 Accept、3 秒读取及所有 header/错误断言均未改，产品代码未改。独立 test-only 提交。
`.64 gofmt/vet` 通过；`go test -race -count=3 -run '^TestReviewLatin1Tokens$' ./internal/agents/upgrades`
在 .64 通过 59.212s，在 .32 通过 75.418s。

C27：本轮不同于第一轮的旧取消契约，是最新 go 加入 t.Parallel 后，在整包 CPU 重载下的
完成等待断言。没有直接归为测试问题；依协调者要求在同一 .32 同时启动基线 go@895f5e21、
分支（修正前）和 `go test -race -count=10 ./internal/rules` 负载，三进程启动相差不足 3ms。
两边都执行 `go test -json -race -run TestC27Review -count=5 ./internal/oxidns/`，全部通过：

| 项目 | 基线 go@895f5e21 | O3-6a 分支 |
|---|---|---|
| 整包耗时 | 272.678s | 272.149s |
| 关键事务五轮耗时（秒） | 5.02 / 4.83 / 4.95 / 4.84 / 6.49 | 4.66 / 5.22 / 4.47 / 4.57 / 5.13 |
| 关键事务中位数 | 4.95s | 4.66s |
| 失败 | 0 | 0 |

表内是整条测试的耗时，含准备工作；内部每个事务的 5 秒完成断言仍通过。
基线使用原有取消契约；分支使用已批准的 owner Detach/工作父级契约，正式对照时都保留 t.Parallel。
负载 rules count=10 通过 212.435s。未见分支明显变慢或丢失唤醒的证据。
原始记录：.32 `/tmp/o36a-c27-compare/{baseline,branch}-concurrent.json`、
`summary-concurrent.json`、`load-concurrent.log`，调度脚本 `/tmp/o36a-c27-compare.py`。
预检因 rt 项目锁而未严格同时启动，没有用它代替正式对照。
满足协调者条件后，仅删除这一条测试的 t.Parallel，另作 test-only 提交；两个真实竞争事务、
5s/50ms、Scope/revision/merge 断言全保留。修改后单条测试 race count=5 通过 8.091s。

协调者最新要求：Latin1 count3 与上述对比完成后，不再跑完整 gate，直接交回，由其 cf-gate。
完整 gate 如果排队超过 15 分钟也交回；本次没有再启动排队。
最终已变基到 dda8973d（A13 合入），无冲突。变基后在 .32 执行：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal tools cmd)" && go vet ./... && go build -tags configflow_proddiff ./... && go build -tags configflow_difftest ./... && go test -race -count=1 -run "^(TestC27ReviewCanceledTransactionWaitsAndCommits|TestReviewLatin1Tokens|TestMissingJobLockFailsClosed|TestOwnerLifetimeAndLocalBatch|TestReadSnapshotSerializesWriterAndExpires|TestProfileOperationsSerializePerProfileAndAllowParallelProfiles|TestDeleteWaitsForProfileWrite|TestResourceLeafWaitAndLegacyFiles)$" ./internal/oxidns ./internal/agents/upgrades ./internal/repo'
```

退出 0：gofmt 无输出、vet 和两种 tag build 通过；OxiDNS 3.492s、upgrades 25.840s、repo 8.650s。
没有在 A13 新基线上重跑完整 race/proddiff/启用的 difftest，不能把旧基线结果写成最终全绿。
对照用的本机 detached worktree 已删除；远端对照源码与日志保留。

## 后续边界

**O3-6b**：R09 已随 28e6e0c1 合入；后续按协调者安排另开分支，接 routers 按物理连接共享的 L1 Keyed → repo.JobLock，
审计 R04–R07/R09 写入、读回、回滚 owner Detach。R08 已迁移多数 RouterOS 设备 I/O；
剩余当前点包含 route_editor ExecuteRoutePlan/RouterOperationLock(write)、routeros_routes、
iKuai write/confirm。保留只读操作取消与写入 10 分钟上限，补真实共享 key/busy/CAS 边界测试。

**O3-6c**：6a 合入后处理 shared commit points honour shutdown。A14 已复现真实 Runtime
在 Store.BeforeCommit 期间关机仍能 SQLite COMMIT，这次没有声称已修复。
以 `git show go-port-a14:server/parity/progress/a14-shared-commit-window.patch` 为起点，保留复现测试；
增加 taskstatus RecordContext/ForgetContext（锁内写前检查，旧入口保留）、repo 最终提交 ctx 检查、
rulefetch touch/rename 前检查，并处理 BuildMosDNSZip 若仍有 WithoutCancel。沿用本次锁分层。

## 当前基线上的提交

23908f728 设计；9089fcc05 叶锁/子顺序；55db73bec ctxx/本地批处理；7fd0dbf18 taskstatus；
53ccc09bb repo；a9d62a492 notifications；49343cbd8 monitor；98ea91c60 listener；5c4b6ab77 installer；
5a20d9fd3 Sub-Store；c65154ee0 deployment；bef2fbec0 owners；6ebabfc35 拆分范围；
b8c1ddf50 C27 取消契约；614bab184 退休锁差分；fe18846d0 Latin1 test-only；358911a19 C27 调度 test-only；
b8be9043a 首次交回记录；49b95db20 L1/O12 test-only。最新验证记录提交见本文件的 git log。
