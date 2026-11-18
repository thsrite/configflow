# O3-6b — routers JobLock 与工作取消

状态：implementing。分支go-o3-6b，从协调者指定的go-opus-review-r10@f368fa1b9建立；
不push、不派子agent、本机不测试。最终需在R10及O3-6c正式合入后rebase go。

## 范围与契约

- routers拥有唯一L1 Keyed[string]，Wire接入repo.Hooks.JobLock，保持非阻塞、无repo回调。
  存在的连接按type+换行+去尾斜线endpoint；缺记录按profileID+换行+routerID。
- 当前repo对未接线JobLock默认fail-closed不放宽。接线后空闲连接可修改，发布期间仍409。
  SaveRouterSettings/SaveRouterPublication继续由调用方已持锁，不能在repo重复尝试同一L1。
- 剩余write/rollback的WithoutCancel迁到owner的lifecycle.Detach，保留请求断开完成，
  Runtime关机能取消；入口和最终设备写/本地状态提交前检查ctx，原生产I/O timeout不变。
- 不移植尚未落地的R12–R17，也不在底层driver方法重复取L1。

## R11 协调（开工前已联系694df9d0）

R11确认无临时Keyed/JobLock，无签名偏好。统一RouterJobLock(ctx,profileID,routerID,*Router)
返回held ctx/release/error，内部Detach与立即尝试；repo hook与该入口共用锁表。
R11底层PD/DNS/readiness方法已经ctxx.Detach+ctx.Err，每次设备I/O及manifest前检查；
它们没有profile/router上下文，不自行拿L1，由R12 Publish/StartPublish或A23上层取L1→L2并传ctx。
R11对RouterOS.Apply另加入口取消检查，后续rebase保留；不改R11私有产品文件。

## 步骤

1. 公共JobLock与Wire：类型/endpoint等价、缺记录fallback、冲突立即返回、释放后可用、
   L3下try无违规、旧锁文件哨兵、真实Runtime取消/断开；repo集成验证已持锁豁免与并发保护。
2. R04–R10现存write/rollback owner清除WithoutCancel，补真实Runtime回归；
   复用回环fake和生产参数，验证停止不写/不改proof、断开仍完成、L2等待可取消。
3. R10/6c合入后rebase，验证repo的真实BeforeCommit取消及并发发布互斥，保留R11集成约定。
4. 按更新后的AGENT-BRIEF（go@215ab6b04）：.32或.64只跑gofmt、全仓vet、改动包及直接调用方race、相关difftest；新增fuzz才跑60秒。
   agent不跑完整gate，定向通过即交回，由协调者按批次跑cf-gate。

## 当前进度

已实现RouterJobLock与Wire的JobLock适配，所有入口共享唯一L1表。未改repo的已持锁豁免。
测试证明：未Wire仍fail-closed；Wire后空闲连接修改、初次manifest、删除可提交；
同设备不同profile/router别名互斥，释放后可提交；持L1→L2调用SaveRouterSettings/Publication/withdraw/delete不重复取锁。
L3内部try不报反序；fallback键独立；Runtime停止拒绝/客户端断开可取；真实repo DATA_DIR里的旧锁哨兵内容/inode/mtime不变且不新增锁文件。

.32命令（含远端gofmt，取回格式化后的测试文件）：
```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers/joblock_test.go --back server/internal/routers/joblock_repo_test.go -- bash -lc 'cd server && gofmt -w internal/routers/joblock_test.go internal/routers/joblock_repo_test.go && go vet ./internal/routers/ && go test -race -count=3 -run "^TestO36b" ./internal/routers/'
```
退出0：`ok configflow/internal/routers 24.301s`。更早纯JobLock组race3通过1.150s。
下一步迁移旧driver的WithoutCancel并加真实Runtime I/O/rollback/lock-wait回归。

## 旧写入/回滚取消边界

已移除routers剩余WithoutCancel（iKuai Write/Apply/Batch、两vendor手工route编辑、ExecuteRoutePlan、
RouterOperationLock写模式），入口Detach并检查ctx。iKuai每次post前与返回后保留原生取消，设备timeout未变。
R07地址/NAT入口在清理read evidence或设置touched之前检查；RouterOS.Apply补入口检查，与R11同一修复后续保留一次。
R08 apply与restore在最终SetPolicyManifest前检查，取消不得提交proof。

协调者批准Clarified in O3-6b并登记behaviour-changes：ApplyMany批次间原ctx改work ctx，
旧review测试有意改为断开后150/150，符合O3 §2.5 / PORTING4b；新增真实Runtime第100个已证明记录回调后
Shutdown仍为100/100、后续批次不写。没有改batchMin/routeBatch或网络timeout。

真实Runtime回归TestO36bLegacyWriteRuntimeBoundary覆盖两vendor apply/restore/edit/plan/empty-plan及iKuai Write/Batch，
停止后零I/O/零device变化（空batch不执行callback），断开后全部完成。
TestO36bPolicyRestoreCommitRuntimeBoundary在最后snapshot响应后关机，旧proof不变；断开仍能完成manifest-only恢复。

- 红（产品修复前，.32）：`go test -count=1 -run "^TestO36b(LegacyWrite|ApplyMany|PolicyRestore)" ./internal/routers/`
  退出1，0.161s。10个旧owner在停止后仍成功执行/发请求；ApplyMany断开后停批次；R08取消后manifest=null。
- 初轮编译抓到一处范围过宽的ctx替换和删除旧断言后的unused import，已修正；不记为红证据。
- 绿（.32）：
```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers -- bash -lc 'cd server && gofmt -w internal/routers/route_editor.go internal/routers/ikuai_apply.go internal/routers/ikuai.go internal/routers/ikuai_routes.go internal/routers/routeros_routes.go internal/routers/routeros_batch.go internal/routers/rospolicy_write.go internal/routers/r04_review_test.go internal/routers/natlist_write.go internal/routers/addresswrite.go internal/routers/routeros.go && go vet ./internal/routers/ && go test -race -count=3 -run "TestO36b|TestApplyManyFinishesAfterRequestCancellation|TestApplyFinishesNATBatchAfterCancellation|TestRestoreManyIgnoresCancellation|TestR06ConnectionLock|TestR09RuntimeShutdownAndRequestDisconnect" ./internal/routers/'
```
  退出0，`ok configflow/internal/routers 30.676s`。
下一步：真实I/O中途取消、L2等待、repo caller-held提交与并发发布压力；R10/6c合入后rebase及最终门禁。

## 实际I/O、等待和并发集成

- TestO36bIkuaiWriteInFlightRuntimeBoundary：真实devhttp与回环HTTP；设备收到delete后阻塞但未处理。
  Runtime关机取消抵达服务端request，返回原生context.Canceled，device不变；只断开客户端时I/O仍等待，放行回复后完整删除。
  未缩短生产10秒timeout。TestO36bOperationWaitRuntimeBoundary：实际L2持锁等待，停止不等释放就取消，
  断开后仍等待并在释放后完成，不将context.Canceled误映射成busy。
- TestO36bConcurrentPublicationsShareJobLock：真实Runtime中首个owner持L1→L2，设备Apply证明后在DB BeforeCommit阻塞；
  24个其他profile/router别名同时try，全部立即冲突、零额外设备I/O；首个提交后锁释放，下一owner可完成device补偿与DB撤回。
  结果channel在持锁函数的所有defer执行后发送，避免把测试唤醒与解锁顺序做成竞态。
- TestO36bNATAndEmptyOwnerRuntimeBoundary：R07地址/NAT更新与恢复、R04空restore/ApplyMany，真实Runtime停止后
  零I/O/零journal/零touched；断开后更新+恢复完整完成。正常路径明确断言lockx.Violations()==0。
- .32 新I/O/等待/并发组race3通过7.164s、vet通过；随后全部TestO36b组合race3通过33.644s、vet通过。
```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers/joblock_test.go --back server/internal/routers/joblock_repo_test.go --back server/internal/routers/o36b_io_test.go --back server/internal/routers/o36b_fastpath_test.go -- bash -lc 'cd server && gofmt -w internal/routers/joblock_test.go internal/routers/joblock_repo_test.go internal/routers/o36b_io_test.go internal/routers/o36b_fastpath_test.go && go vet ./internal/routers/ && go test -race -count=3 -run "^TestO36b" ./internal/routers/'
```
更新PROGRESS-routers中已过时的R14 flock/JobLock默认放行、旧签名及handler WithoutCancel指引，防止后续重复造锁。

既定关机边界仍保留（review-r09 F6）：已经发送到设备的写入可能留下部分状态，关机后补偿也是写入，不能绕过取消；
后续R12恢复/人工核验负责该状态，不把本步取消闸门宣称为完整发布事务原子性。
接下来跑当前合并树routers/devhttp/routersplit完整相关包race，等待R10与6c正式合入通知后rebase。

## 长任务检查点

产品/测试提交：b8f34ccc7（共享JobLock）、c2b3e9991（owner取消与已批准旧测试语义）、
a80e49197（实际I/O/等待/并发/空路径）。当前完整相关包命令已前台执行完成（退出0）：
```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD"); test -z "$(gofmt -l internal tools cmd)" && go vet ./... && go test -race -p=4 -count=1 -timeout=60m ./internal/routers/ ./internal/devhttp/ ./internal/routersplit/'
```
前台exec session75973已退出0：gofmt全目录无输出、全仓vet通过；
`ok configflow/internal/routers 845.104s`、`ok configflow/internal/devhttp 4.092s`、
`ok configflow/internal/routersplit 36.519s`。包括当前R04–R10的共享驱动测试与真实Python参照；
基线仍为指定R10复核分支f368fa1b9，不冒称已经过R10/6c合入后的最终gate。

`/tmp/o36b-repo-runtime-draft.go`是后续集成测试草稿，尚未放进正式树：
TestO36bRepositoryRuntimeCommitBoundary对generic-password/manifest/delete、caller-held settings/settings-delete/publication/withdrawal，
分别在实际L3等待或BeforeCommit回调关机/断开（28子例），核对完整SQLite Fingerprint及非DB文件清单。
待O3-6c正式合入再应用为internal/routers/o36b_repo_runtime_test.go并远端gofmt、race3/vet；
缺6c的旧树已知仍在普通BeforeCommit后COMMIT，不能把依赖未接通当成本步产品回归，也不提交失败测试。
R10与6c未收到正式合入通知前不rebase；go新添的a9d6ce86c仅WORKFLOW批次合并/风险分层说明已读，未误当成R10合入。

### R11 最终共享路径提醒（d214fb515）

已只读核对go-port-r11相对R10的共享routeros.go差异，尚未合并进本分支：
Apply入口ctx.Err与本步相同，保留一次；RESTORE递归perform须继续转发beforeCreate；
IPv6 PD address-list加入rosSingleRowPaths，scoped/inScope按list限定，与IPv4一致；
ND最终payload沿用R11的ipv6DNSFlag严格拒绝未知/null及dynamic/invalid标志，不回退rosTruthy宽松判断。
这些改动依赖R11私有pdLists/ipv6DNSFlag等定义，后续整分支集成，不能只复制共享文件补丁。
R11 owner回报完整共享包race/Python/mutation已通过；此处仅登记其报告和差异审阅，不冒称本分支已跑R11。

### 流程更新（协调者，go@215ab6b04）

已读更新后的AGENT-BRIEF。后续不再发送中途进度消息；只有需裁定、交回或阻塞时联系协调者。
长测试以一条阻塞前台命令等待结果，不进行模型回合中的日志轮询。
O3-6b仍等待R10和O3-6c正式合入通知；之后rebase go、应用28子例草稿，跑定向验证即交回，
不运行gate.sh/proddiff/全仓race。当前尚无新fuzz目标，不为本步凭空增加解析器fuzz。
以上覆盖本文前面旧的最终gate/15分钟排队计划；已完成的历史定向结果保留。

### R12 开工接口确认

R12 owner694df9d0从go-opus-review-r11建立go-port-r12；已再次确认RouterJobLock现签名稳定，
不需要提前改基到未集成6b，也不复制公共文件或另new Keyed。其设计中的typed lock collaborator
默认fail-closed过渡，6b正式合入后rebase直接接唯一公共入口，并移除临时默认stub。
Publish先Detach/检查，读取快照选key但在404/参数错误返回前try L1（缺记录传nil fallback），
再取L2并下传held ctx；driver/ReturnAgent/rollback不重入L1，SaveRouterPublication沿用caller-held约定。
忙为KindConflict/messageEditBusy，Runtime取消保留原生ctx.Err，不能改成busy或继续补偿写。
6c的合并复验旧preparation测试已在独立分支go-o3-6c@426f6edd0 test-only修正交回；不把它误当成正式go合入。
