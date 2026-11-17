# A14 交回记录

状态：`ported`，待新的 Opus HR 复核和 L2；分支 `go-port-a14`，已经 rebase 到 `go@28e6e0c12`。没有 push，没有子 agent，没有操作生产或真实 Agent。以下验收只使用测试服务器、loopback fake 和合成语料；proddiff 快照只留在服务器私有目录。

## 完成范围

- A14-1 `687fe17a2`：类型化 prepare、provider 下载保存后重渲染、生成错误位置、配置拉取、真实 PrepareFunc 接线。
- A14-2 `1c35c4994`：submitted 先持久化后 POST；upload/activate、回执合并、unknown、仅当前目标确认成功推进 applied_content；Keyed 准备锁、回滚和恢复、旧协议兼容。
- A14-3 `736af4a90`：九条 HTTP 路由、guard、冻结 generation、TTL/no-store 产物、进度节流。
- A14-4 `637165642` 及后续提交：完整 HTTP/DB/文件差分、真实进程崩溃/关机、原始 Agent 请求、首推缓存、真实 go-agent 归档兼容、变异和 fuzz、共享提交窗口 patch。
- 消费 A13 的状态稳定观察 harness；全部 24 条 A14 延期 baseline 已删除，保留且只新增获准的那一条 A18 精确 message baseline。A18 删除任务已登记。
- C36 只读 reader 位于共享 `internal/setupflow`，PROGRESS-config 已登记复用；未实现 C36 写入流程。
- `RulesFingerprint` 保持规则 schema/build 证明语义，内容 Fingerprint 排除资源身份；nil read 不清除证明。准备状态通过 Keyed/Held 判断，不持久化 `_preparation_running`。

各复核问题类别逐条说明见 [PROGRESS-agents 的 A14 节](PROGRESS-agents.md#a14--publishing-transactional-content-push-config-deployments-receipts)，覆盖 A11/A12/A13/A19/A24、R04–R08 和四条常设检查项。Python 测试文件到边界测试的映射及 L2 清单也在该节。

## 已知问题和差异

- **high，已知、已分派给 O3-6c**：真实 Runtime 在 Store.BeforeCommit 期间关机后，当前共享 Store 仍可能 COMMIT。A14 自身入口及提交前检查已完成；按协调者决定不等待共享修复。完整回归 `TestShutdownAtSQLiteCommitKeepsWholeDocument` 暂时显式 `t.Skip("等 O3-6c…")`。O3-6c 合入时必须删除 skip 并通过测试。[最小原型 format-patch](progress/a14-shared-commit-window.patch) 和[复现说明](progress/a14-shared-commit-window.md)已提交，未应用到 A14 运行源码。
- **high，已修复，Python 同样存在**：先删除私有 proof 再收集凭据会让 message/error_details 中的裸密码/token 引用泄漏。Go 在投影前调用共享 redact 收集，保留 name/username，原记录不变；真实 Python 参照确认旧行为。
- **medium，已修复**：嵌套 RunDetached 再次 Detach 会丢失已跟踪 owner 的取消。Go 保留 tracked ctx；真实全局 Runtime 在入口前和执行中取消的回归均先失败后修复。
- **medium，已修复，Python bug**：final_config 带冻结 Geo 的相同 deployment_id 重试，将含资源归档与不含资源 source_sha256 比较，误拒绝同一内容。Go 两次均使用资源外的内容身份，只上传一次。
- 原生 YAML/JSON/归档格式不追逐 Python 字节，生成内容语义相同。按设计 §7.1，原 Python baseline 在切换后必须显示一次 changed，O14 已登记全量推送。测试覆盖原字节 changed 和 formatting-only push-diff。
- **测试副本内的基线格式迁移**只作用于隔离 proddiff 的 Go 副本：逐一以 yamlx.Equal 证明主文件语义相同，非主文件逐字节相同，并独立重开归档；证明失败直接报错。前后文件、证明结果仅留远端私有目录。它用于比较后续完整状态，不新增 baseline，不改生产状态。实际 proddiff 已通过逐项证明，原始文件与证明仍只留远端私有目录。
- `/config` 结构比较只限这个路由的 YAML content，MD5/version 对各自原字节验证；修改值和删除键均被变异自检检出。其他路径没有借此放宽。
- A18 未接入的 HA 最终生成按设计拒绝；已确认 status=400、success=false、其他字段及比较 headers 一致，只有获准的精确 message baseline。畸形非布尔 Surge restart 返回类型化 400；正常布尔和 null 与 Python 一致。
- 日志 tail 的旧差异定位到历史 HA 状态读取误报 ErrNotPorted，已经修复历史终态的纯读取；未规范化日志正文，后续实际 proddiff 已确认日志 tail 没有新增差异。

## 已通过验证

- 真实 Python prepare 10 个案例、发布 transcript 11 种情形；Flask↔Go 30 次 HTTP，每次写后比较完整 DB 和整个 DATA_DIR，并独立验证归档，比较 fake Agent 原始 body/headers。
- 六个实际 SIGKILL/SIGTERM 子进程故障；七种首推/缓存场景；6/6 语义变异被检出且源码逐字节恢复。
- 变基后的 Flask/API、Runtime 边界及脱敏专项 race 通过；Python harness 44 tests OK；其他共享驱动模块在合并后的树上重测通过，详见 PROGRESS。

最新 Runtime/投影回归命令（.64）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && test -z "$(gofmt -l internal/deploy/runtime_boundary_test.go)" && go test -race -count=1 -v -run "^(TestShutdownAtSQLiteCommitKeepsWholeDocument|TestShutdownRejectsEveryPublishCommit|TestShutdownAfterAgentAcceptanceRetainsUncertainSubmission|TestDisconnectedBoundRequestCompletesPublicationAndArtifacts|TestNestedOwnerKeepsTrackedCancellation|TestDeploymentCollectsCredentialsBeforePrivateProjection)$" ./internal/deploy'
```

```text
--- SKIP: TestShutdownAtSQLiteCommitKeepsWholeDocument (0.00s)
--- PASS: TestShutdownAfterAgentAcceptanceRetainsUncertainSubmission (8.99s)
--- PASS: TestDisconnectedBoundRequestCompletesPublicationAndArtifacts (2.51s)
PASS
ok configflow/internal/deploy 14.213s
```

真实 go-agent 归档兼容命令（.32）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd backend/agents/go-agent && CONFIGFLOW_SERVER_ARCHIVES=$PWD/../../../server/internal/deploy/testdata/archives go test -count=1 -v -run "^TestServerArchive$" ./routes/'
```

```text
15 subtests PASS: mihomo/mosdns/oxidns × 4096/final/normal/reload/resources
PASS
ok agent/routes 232.559s
```

解析入口 fuzz 命令（.32）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && go test -run "^$" -fuzz "^FuzzPublishRequest$" -fuzztime=60s -parallel=2 ./internal/deploy'
```

```text
339222 executions, 60s fuzz
PASS
ok configflow/internal/deploy 61.038s
```

## 完整门禁状态

最新命令（.64）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'GATE_LOGS=$HOME/.cache/rt-tmp/a14-c90af974 server/scripts/gate.sh -p 6 FuzzPublishRequest:./internal/deploy FuzzOpenArchive:./internal/deploy FuzzConvertMosDNSRules:./internal/deploy FuzzSourceExcerpt:./internal/deploy'
```

这轮 gate 已结束：格式/vet、两种 tag 和观察器检查通过，完整 race **96 个包全部通过**，没有 FAIL/data-race 标记。受当时汇总调用遗漏 identity tracker 影响，proddiff `new=6 stale=0`，gate 因此退出 1，未进入 difftest/fuzz；六项均为 content_sync/push-diff 的 checked_at/version。不是一次完整的 GATE PASS。

修复最终汇总调用后，新增实际 main() 回归先失败再通过；内容变异仍会失败。修正后私有原始响应回放恰好消除六项，`new=0 stale=0`。再经过 L1 合并，harness 46 tests OK，A14/L1/HTTP 专项 race 在合并树上通过：deploy 20.993s、api/agents 4.515s、真实 Flask 差分 52.140s、httpx 1.628s；共享 API 错误边界专项另通过 1.363s。

使用新合入 gate 的单阶段入口重新执行真实 proddiff（不是回放），没有重复完整 race：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'GATE_ONLY=proddiff GATE_LOGS=$HOME/.cache/rt-tmp/a14-merged-proddiff server/scripts/gate.sh -p 6'
```

```text
baseline: new=0 stale=0
GATE STAGE PASS: proddiff
```

这个成功的 proddiff 基于 `go@6e37489ff` + A14；其后最终 rebase 到含 C29/R09 的 `go@28e6e0c12`。最终合并树完整门禁仍由协调者 cf-gate 复验；不把分阶段结果写成一次完整 GATE PASS。真实 120s upload-read 超时测试已随完整 deploy race 执行通过。Opus HR 和 L2 尚未执行。

C29 合入后的全 difftest 阶段命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'GATE_ONLY=difftest GATE_LOGS=$HOME/.cache/rt-tmp/a14-c29-difftest server/scripts/gate.sh -p 6'
```

14 个包通过；agents 差分只有 TestContentChangesAgainstFlask 失败。已单独复现：A13 夹具在 app 启动前装替身，被 A14 正常接线覆盖。只将其移到启动后，保留全部比较与 shutdown 后 cleanup；未改生产参数。修正后最终 R09-base 下验证：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e CONFIGFLOW_DIFF=1 -- bash -lc 'cd server && test -z "$(gofmt -l internal/api/agents/difftest/content_diff_test.go)" && go test -race -count=1 -run "^(TestContentChangesAgainstFlask|TestPublishHTTPAgainstFlask|TestPublishLegacyProxyAgainstFlask)$" ./internal/api/agents/difftest && go test -race -count=1 -run "^TestPublishedContentUsesRealAsyncPreparation$" ./internal/api/agents'
```

```text
ok configflow/internal/api/agents/difftest 20.257s
ok configflow/internal/api/agents 3.856s
```

最终树另通过 gofmt/vet、Python prepare/transcript/首推缓存/资源重试的 race（deploy 124.704s）和真实发布 Flask race（16.207s）。该命令的末尾 Python 检查曾因 cwd 留在 server/ 而未执行；已从仓库根目录单独重跑成功：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'PYTHONPATH=server/tools/proddiff ~/cf-test/.venv/bin/python -m unittest test_proddiff'
```

```text
Ran 46 tests in 0.908s
OK
```

补跑的四个 fuzz（.32，L1-base；后续 C29/R09 未改这些目标源码），每个均为 `go test -run '^$' -fuzz="^${name}$" -fuzztime=60s -parallel=2 ./internal/deploy`，通过 rt、GOMAXPROCS=4 并发执行，日志留在 `$TMPDIR/a14-merged-fuzz/`：

```text
FuzzPublishRequest: ok configflow/internal/deploy 60.930s
FuzzOpenArchive: ok configflow/internal/deploy 61.042s
FuzzConvertMosDNSRules: ok configflow/internal/deploy 61.078s
FuzzSourceExcerpt: ok configflow/internal/deploy 61.075s
```

最后只提交交回文档；A14 四步和后续修复均在分支独立提交，代码修复截止 `3c7e5dfdd`。完整合并树 cf-gate、独立 Opus HR、L2 仍由协调者继续。
