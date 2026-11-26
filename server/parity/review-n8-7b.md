# N8-7 第 2 批独立复核

作者 ref `go-n8-7b@802c81c9`，基线 `go@fbbe382ec`；复核分支 `go-review-n8-7b`。
复核者未参与作者实现。范围为 taskstatus、tasks、notifications/store、weblistener、
api/{logs,stats,tasks,notifications}，共享 jsonresp.Canonical / diag.EventText，以及 D2 和 O14 说明。

结论：**0 high，1 medium、2 low，均已修；0 open。** 容器错误映射和 nullable codec 为本次重构回归；
occurrences 的上溢/大整数丢失在旧 Go 路径也存在，本次按数字精度问题一并修复，不归因于作者新引入。
未发现 O3-6c/C07 提交点、票据或 HEAD 只读保证被削弱。按 Leaner 未重跑作者全套测试/fuzz/完整 gate。

## 发现及修复

### F1 · low · 无效通知容器从 503 变成 500（已修）

原 store 在根或 items 不是 object 时返回 ErrInvalidStore，GET/HEAD 将它映射为 503。
新 storeDocument/Items 的 json.Unmarshal 在收到数组时先返回 UnmarshalTypeError；API 并不把这个
类型当成不可用存储，结果掉入通用 500，违反作者 ledger 中「非法 store 容器仍是 503」的承诺。

修复前，新增 `TestN87ReviewStoreContainerErrorsRetainUnavailable` 的 `[]` 和 `{"items":[]}`
两条代表均 FAIL：GET 实际 500，期望 503。修复只在两个容器解码边界把 native 解码原因与
ErrInvalidStore 合并，保留 errors.Is/errors.As 链。单项非 object 的 errInvalidItem 不变，
没有把整类类型错误都改成 503。修复后 GET/HEAD 均 503，原文件逐字节不变，无 tmp 残留。

### F2 · medium · occurrences 有限整数上溢或重置（已修；旧 Go 亦有）

typed Item 原来声明 `*model.Int`，ObserveItem 直接 `count++`。
9223372036854775807 会被保存成 -9223372036854775808；更大的整数在 R5 解码后为 nil，递增变成 1。
旧 Go 的 jsonx.AsInt + int64 加法也有该范围缺陷；Python 的 `occurrences += 1` 对整数没有这个限制。
这不是 jsonresp.Canonical 的问题，独立检查确认共享编码器没有把数字转 float64。

新增 `TestN87ReviewOccurrenceCounterDoesNotOverflow` 修复前分别观察到负值和 1；60.0 正对照通过。
修复采用专用 native OccurrenceCount：普通 JSON 整数使用 math/big.Int 解码、复制后精确 +1；
R8 的整数字面量浮点写法仍复用既有 model.Int 校验，范围/错误类型规则不另做一套。
Raw provenance 保持未编辑的 60.0 原文，字符串/布尔/小数等坏类型仍 R5，不恢复 Python 动态加法。
测试锁定两个 int64 边界、R8 60.0，以及未知根成员的大整数和同项 sibling:1e400 不变；
额外负控验证 `true`、`"7"`、`60.5` 仍是 typed nil 且无关修改保留其原文。
OccurrenceCount 不参与 fingerprint；本修复不扩大 D2 身份变化。

### F3 · low · 兼容入口在转码阶段拒绝 null Agent（已修）

旧 RecordAgent 对缺失 Agent 是 no-op，RecordRegistrationRejection 对缺失数据生成默认未命名注册通知。
新 legacy codec 把 null 解成非可空 model.Agent，在 native owner 之前返回 model 类型错误；
导致原本可处理的空输入变成后台错误/漏掉拒绝通知。native SaveAgent/RejectRegistration 仍保留了正确的 nil 分支。

`TestN87ReviewNullableLegacyAgentCodecs` 修复前两项均 FAIL（model: incompatible JSON type）。
修复仅将这两条旧 API 改成可空 model.Agent 指针转码，再调用原 native owner；不在 legacy.go
复制业务判定。修复后部署 no-op 不需要 repository，注册拒绝产生且仅产生一条 registration incident。

## 重点核对

### 提交点逐项对照（迁移前 fbbe382ec → 作者 ref）

| owner | 前后保持的顺序 / 拒绝点 |
|---|---|
| taskstatus RecordContext → SaveResult / ForgetContext | ctxx.Detach → 入口检查 → 同一 process-wide leaf mutex → 锁后 ctx.Err → 私有快照修改 → 编码/CreateTemp/Write/Sync/Close → ctx.Err → Rename；defer 删除 tmp，OnRecord 在锁释放后调用 |
| notifications TransactionContext → UpdateItems | lifecycle.Detach → 同一通知 leaf key/5s → 锁后 ctx.Err → DecodeStored/迁移/回调 → ctx.Err → 编码/Write/Sync/Close → ctx.Err → Rename；异常/取消清理 tmp，无提交则不更新 appliedVersions |
| notification snapshot/refresh | 票据在 collect 开始前分配；收集不持 leaf；叶内 admission 比较 appliedVersions；成功 no-op 或 Rename 后才更新票据；过时收集不能覆盖后来的 Dismiss/Record；partial 未就绪保留未观察项 |
| notification HEAD | PreviewItems 的 persist=false，不建目录/文件、不迁移落盘、不替换 inode；普通 GET SnapshotItems 才使用 detached owner |
| listener Start/Update → Listen/ChangePort | Detach/L1 try-lock → listen/serve/health → ctx.Err → 保存原选择 → atomicWrite close/check/rename → mutex 内 ctx.Err → active 切换；失败关新 socket/恢复 token；文件已写而关机时仍用既定 Uninterruptible 恢复原选择 |
| task interval owner | Detach/入口与 setter 前 ctx.Err → typed UpdateShared callback 检查 → repo 事务提交门；已成功提交后的呈现失败只返回 fallback，不谎报保存失败；任务 busy key/Runtime Spawn admission/释放顺序保留 |
| stats sampling | GET detached，HEAD 不 persist；WriteProfileText 的 profile-operation/原子替换 owner 未改；调用前后均检查取消，optional I/O 失败策略不变 |
| logs clear | Detach → 入口检查 → 取路径/存在检查 → 最后 ctx.Err → 清文件；读和 HEAD 路径不调用写 owner |

Model.Raw 明确是不可变 provenance；ClearMember 给副本创建新 member list。SnapshotItems 对 Item
浅拷贝后清 signature/evidence 不会误改持久化原记录。没有引入第二套锁、flock、裸后台 goroutine 或扩大不可中断区。
真实 Runtime 的等锁、回调内关机、请求断开、listener 两个提交点和 owner 取消代表用例已抽查通过。

### 旧 API 与 core 边界

手工检查四包 legacy.go、task/API 的旧 hook codecs。业务只有一份 native 实现，旧入口仅做序列化、
字段打包和旧 callback 类型适配；类型验证委托同一个 native validator。F3 修复仍保持这个界线。
`TestNativeConsumerBoundaries` 校验八个 core 无 jsonx import / any；没有把未迁调用方也声称删除。
interval 的精确键/字段数/整数字面量判定与 BindSilent 修复 guard 保留；请求从不走 DecodeStored。

### D2 独立调用图

仓库 grep（只计非 test Go 文件）及读调用点得到：

- RouterSignature 只有 notifications/legacy.go 的定义，它只调用 RouterDigest。
- RouterDigest 仅由 SaveRouter 和 collect 调用；前者写 router-check.signature，后者产生
  collectedObservations.routers，在 apply 中只比较并设置 pending incident 的 stale。
- IncidentFingerprint 只由 dismissedItem / ObserveItem / legacy Fingerprint 使用，服务于确认标记、版本和去重。
- notifications.json / router-check 的生产读写集中在 notifications；上述调用没有通向
  routers rule-sync baseline、StartPublish 或其他任务状态提交。

因此作者「不会因为签名变化新增同步/发布 pending/waiting，也无需手动重新发布」属实。
这里的 pending 是通知自身状态，不等于任务 pending。O14 的一次通知重现、旧检查可能 stale、
重新确认后稳定及 Agent 规则缓存预热的独立关系表述正确。跨进程稳定和一轮重新确认的代表测试通过。

### 非有限数、stats 差异与共享入口

- task-status/notifications/stats/listener 读取仅在持久化边界调用 DecodeStored。
  它只在字符串之外把裸 NaN/±Infinity 换 null；有效数字和未知成员走 RawMessage/model.Raw。
  非对象 store 拒绝不覆盖原文件；taskstatus/optional stats 的损坏读默认策略保留。
  有限整数兄弟字段不应被 R5 错当溢出计数后写坏，F2 已处理。
- stats 的 500→200 已登记 behaviour-changes.md 的 second batch D2/file readers，以及作者交接。
  `n8_7_stats_test.go` 仅在独立 overflow case 中要求真实 Flask 的第二个响应为 infinity 错误 500，
  然后构造完整成功期望，文件只改那一个 nodes 样本 Infinity→原始 1e400；正常 decoder/comparator 未改。
  D2 的 `withNativeDismissal` 也只改一个固定 incident fingerprint，先断言 Python 原 digest 和独立 native digest。
  没有全局忽略 status、JSON 字段、文件或其他数字。
- jsonresp.Canonical 使用 Decoder.UseNumber + 原生 Encode，保持大整数/指数原文；只排序对象键，
  无 Python 编码器，不是请求修复入口。diag.EventText 仅把 string secrets 转交已有 EventMessage visitor，
  规则、截断、UTF-8 和用户名保留策略未变。原 C07 组合脱敏真实 Flask case 与精确 Canonical 样本通过。
- O14 的 Python 紧急回滚 null 算术风险仍准确：taskstatus/stats 用 int(value or 0) 能读；手工坏通知
  occurrences=null 的 Python +=1 仍可能 TypeError，不能宣称自动恢复。F2 没有改变这个已登记风险。

## 定向验证（全部 .32、前台）

修复前：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/api/notifications/review_n87_test.go --back server/internal/notifications/review_n87_test.go -- bash -lc 'cd server && gofmt -w internal/api/notifications/review_n87_test.go internal/notifications/review_n87_test.go && go test -count=1 ./internal/notifications ./internal/api/notifications -run "^TestN87Review" -timeout 3m'
# FAIL notifications 0.035s：max→负数；max+1→1
# FAIL api/notifications 0.178s：根/items 数组返回 500，期望 503

RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/notifications/review_n87_test.go -- bash -lc 'cd server && gofmt -w internal/notifications/review_n87_test.go && go test -count=1 ./internal/notifications -run "^TestN87ReviewNullableLegacyAgentCodecs$" -timeout 2m'
# FAIL notifications 0.162s：两条可空输入在转码时被拒绝
```

修复后和风险代表：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/notifications/types.go --back server/internal/notifications/store.go -- bash -lc 'cd server && gofmt -w internal/notifications/types.go internal/notifications/store.go && go test -race -count=1 ./internal/notifications ./internal/api/notifications -run "TestN87Review" -timeout 3m'
# ok notifications 1.061s；api/notifications 1.468s

RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/notifications/legacy.go --back server/internal/notifications/review_n87_test.go -- bash -lc 'cd server && gofmt -w internal/notifications/legacy.go internal/notifications/review_n87_test.go && test -z "$(gofmt -l internal/notifications/{store,types,legacy,review_n87_test}.go internal/api/notifications/review_n87_test.go)" && go vet ./... && go test -race -count=1 ./internal/taskstatus ./internal/tasks ./internal/notifications ./internal/api/notifications ./internal/weblistener ./internal/api/logs ./internal/api/tasks ./internal/api/stats ./internal/jsonresp -run "^(TestN87Review.*|TestO36cStatusLockWaitRuntimeBoundary|TestO36cStatusShutdownDuringRecord|TestRecordContextPreservesNativeStorageErrors|TestLeafCommitChecksOriginalRuntimeAfterCallback|TestWaitingLeafTransactionHonoursShutdown|TestObservationCollectionDoesNotHoldLeafAndCannotOverwriteNewerState|TestNativeD2DismissalReappearsOnceAndRouterStateIsPreserved|TestNativeSignaturesAcrossProcessRestarts|TestHeadProjectsWithoutMigratingOrWritingReceipts|TestInvalidSavedHistoryReturnsUnavailableWithoutReplacingReceipts|TestBackgroundIOReceivesRuntimeCancellation|TestNativeConsumerBoundaries|TestHealthyProbeCannotCommitAfterRuntimeShutdown|TestReviewShutdownBetweenSavedStateAndActiveSwitchRestoresSelection|TestPartialServeAdmissionClosesAllSockets|TestClearOwnerRuntimeAndFinalCommitCheck|TestIntervalOwnerKeepsDisconnectButRejectsStoppedGeneration|TestIntervalBindingRetainsLiteralValidation|TestHEADHasGETHeadersAndNeverCreatesOrReplacesHistory|TestSamplingOwnerRuntimeAndCancellationBeforeCommit|TestNativeHistoryInterchangeAndExactCounters|TestCanonicalIdentityEncoding)$" -timeout 5m && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/tasks/difftest -run "^(TestN87OverviewOverflowAgainstFlask|TestC07NotificationReadAndCombinedRedaction)$" -timeout 3m'
# gofmt clean；go vet ./... 通过
# taskstatus 1.364s；tasks 1.060s；notifications 5.107s；api/notifications 2.181s
# weblistener 1.066s；api/logs 1.771s；api/tasks 2.806s；api/stats 2.440s；jsonresp 1.010s
# tasks/difftest 2.026s（仅两条明确选中的真实 Flask case）
```

只用现有合成夹具、t.TempDir 和 loopback；未访问生产或真实设备，未 push、未开子 agent。
复核提交即本文件所在提交。完整 batch gate 仍由协调者运行。
