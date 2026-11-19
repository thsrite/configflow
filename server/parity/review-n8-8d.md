# N8-8d 独立复核

复核者 Codex，非作者；对象 `go-n8-8d@d0f7cf462`（产品提交 `72ab32b6f`），
基线 `go@1fd89e1c1`，复核分支 `go-review-n8-8d`。
已读 AGENT-BRIEF、PORTING、WORKFLOW 的 Leaner verification／第四常设项、
n7-n8 的 N8-8，以及 review-n8-9-subs／rules。

结论：**0 high、2 medium，均已修复；N8-8d 主体未发现新增功能回归。**
两项是按第四常设项追查出的既有 deploy owner 缺陷，并非本次 typed provider 新引入。
所有执行均在 `.32`，`GOMAXPROCS=4`、`-p 4`；未跑作者整套、完整 gate、全仓 race、
生产访问、push、PR 或子 agent。新修的 deploy 公共存储涉及 HR 路径，交协调者安排
后续 fresh-Opus 复核及批次门禁；本报告不代替该风险流程。

## Medium M1 — preview 在真正发布前已屏蔽 shutdown

`Previews.Save` 在 L4 锁检查后立即 Uninterruptible，连额度／过期检查都不再感知
来源 Runtime。真实 Start/BindRequest/Shutdown 回归在第二次时钟读取（锁内、发布前）
关机，修复前仍返回新 Preview、写出 record.json，`err=nil`；disconnect 正控通过。

移除整个 Save 的取消屏蔽，保留原子文件叶层的 rename 前检查及失败目录清理。
锁准入不再被视为发布；关机保留原 preview 目录，断开仍完成新 preview。
未改 TTL、额度、计时和生产 I/O 参数。

## Medium M2 — receipt 捕获在两次文件替换前均可绕过 shutdown

`Receipts.Capture` 同样在锁准入后屏蔽了整个目录写入和修剪。在真实 Runtime 下，
分别于第一／第二次原子写入前关机，修复前两次都写出 archive＋receipt.json，返回 nil。

移除该屏蔽；archive 和 receipt.json 各自保留 ctx 至真实 temp/fsync/rename。
若 archive 已完成而关机阻止 enrollment，保留这个未引用 archive，不再继续写 receipt
或进入修剪。disconnect 两文件仍完成。测试只在 owner 的私有写入依赖处触发关机，
随后调用原 atomicWrite；没有假装写成功或覆盖生产超时／批大小。

以上两项均先取得真实失败，再修产品并通过回归。补充 o3-concurrency 的澄清，明确
第四常设项覆盖旧 provider batch 例外；repo 三种已标记 filesystem/DB 批次、提交后
durability／backup 和 R12 aborted-write receipt 的既有例外保持原含义。

## 原始证据与独立期望

- git 对照确认原 provider corpus 1581 行、recorded 1262 行、N88a 期望、convert
  corpus／generation manifests 均未修改；无新增 baseline 豁免。
- 五份 MosDNS pyref 解压后逐字段比较：只有 metadata 变化，全部 entries／输入／
  原输出不变。没有把答案重录为 Go 输出。
- 审读 n88d_reference.py：只对已批准的 R5 stored-group 读视图及显式 main 类型校验
  做投影，拓扑正常业务继续调用真实 Python DeliverySnapshot；未读取 Go 结果。
  85 条 snapshot＋4 条退休语言异常的替换均绑定 archive SHA、原行索引、完整输入、
  完整原结果、Python owner SHA 和 recorder SHA。不是按错误类通配跳过。
- 在 `.32` 用真实参考 venv 重录 89 条，压缩文件与提交版本**逐字节一致**，finally
  恢复原字节。新增测试只重放这 85 条可疑 snapshot，不重复整份 1581 行 corpus。
- 将第 262 行独立期望临时改为 synthetic-negative-control，实际定向测试失败，打印
  `want synthetic-negative-control / got *provider.InputError`；finally 恢复后通过。
  四条非 snapshot 是 native InputError／url.Error 的内部类别替换，未调整业务中文期待。

## 字节、状态、兼容性与生命周期抽检

- 在 `.32` 的临时目录覆盖回 `1fd89e1c1` 的原 Go 文件，删除作者新增 Go 文件，运行
  同一聚合 provider 样本，独立录得三种原始响应体 SHA；临时树随后删除。
  当前 typed 树逐字节匹配：Mihomo `47fac98b…c64619`，Surge `1d48f377…10c407c`，
  Loon `d8001c28…83691`。完整 SHA 固定在 review_n88d_test.go，三者均 HTTP 200。
  同一请求另调用真实 Flask，比较状态、输出、headers 和持久化副作用。
- Flask 抽检聚合空 name／非对象 provider 成员：400 与业务中文相同；订阅普通节点、
  dangling dialer、shape 错误，以及 Surge／Loon regex 过滤亦通过原差分。
  typed Error 的 cause 链仍支持 rules／dialer／listener 分类，未改变 route 分支。
- 新外部 provider 正控接受 quoted port、未知协议及 opaque 参数、数值扩展 key、
  merge/anchor 和不参与消费的复杂根 key；未把 stored-record R5 用来收紧外部 YAML。
  Base64／订阅输送及机场分组白名单仍由原 subs/substore owner 处理，没有另写解码器。
- 作者 provider 修复确实到达叶层：抽检真实 Runtime＋真实 repo 文件锁的 cache/artifact
  等锁取消，cache 成功前缀后 shutdown，render callback 返回后 shutdown；全部通过。
  另抽检 MosDNS 下载后关机／receipt 和 OxiDNS settings/archive 的真实 Runtime 正反控。
  未用普通 cancel context 冒充 shutdown，也未重启旧 generation。
- 剩余 Uninterruptible 站点逐一读过：repo 标记批次、提交后的 rule-cache durability、
  listener 回滚、R12 已发生设备写入后的 aborted receipt 有各自的已说明语义；未扩大豁免。

## 分配与负控

只使用作者已有的 bytes/op 门槛和 ReportAllocs，不引入墙钟门槛。恢复后的正控：
provider 0/1MiB 为 **1460266/1466649 B/op**，aggregation **2667134/2673393 B/op**。
临时在两个 typed 入口各加入一次 model.Marshal(config)，两条分配断言均真实失败：
provider **1606518/36252156**、aggregation **3100967/37735068 B/op**。
即使不是逐节点调用，整份大配置往返也会被拦住。finally 逐字节恢复源文件后正控通过。

1x ReportAllocs 观测（8/32/128）：provider **600864/4197752/46494680 B/op**、
**4511/29635/293569 allocs/op**；aggregation **679152/2671952/10345136 B/op**、
**5652/21304/84447 allocs/op**。provider 的完整拓扑复核仍随源数有重复成本；
未将“没有重复解大配置”夸大为整个 bundle 线性复杂度。

## 定向命令与结果

下列命令全部使用前缀 `RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc '…'`，
Go 命令在 `cd server` 后执行；gofmt 修改的文件用 rt --back 取回。

修复前两个回归分别执行，均退出 1：

```sh
go test -race -p 4 -count=1 -timeout=3m -run '^TestReviewN88dPreviewShutdownBeforePublication$' ./internal/deploy
go test -race -p 4 -count=1 -timeout=3m -run '^TestReviewN88dReceiptShutdownBeforeReplacement$' ./internal/deploy
```

最终产品／caller 定向验证，均退出 0：

```sh
go test -race -p 4 -count=1 -timeout=4m ./internal/deploy -run '^TestReviewN88d|^TestManualArtifactShutdownBeforePreviewCommit$|^TestShutdownRejectsEveryPublishCommit$|^TestPythonPrivateStoresLoad$|^TestReceipt|^TestPreview'
go vet -p 4 ./...
test -z "$(gofmt -l internal/deploy/previews.go internal/deploy/receipts.go internal/deploy/artifacts_test.go internal/deploy/review_n88d_test.go internal/provider/review_n88d_test.go internal/api/aggregations/difftest/review_n88d_test.go)"
go test -p 4 -run '^$' -bench '^BenchmarkN88d' -benchtime=1x -benchmem ./internal/provider ./internal/aggregations
go test -race -p 4 -count=1 -timeout=3m ./internal/convert ./internal/mosdns ./internal/oxidns -run '^TestN88dNativeDNSExports|^TestNativeRuleTextAgainstPython$|^TestO36cArchive(ClosedRuntime|ShutdownAfterFetch)$|^TestC29(RuntimeShutdownRejectsEveryOwner|RealRuntimeDisconnectCompletesEveryOwner)$'
```

尾行：deploy **23.729s**；vet／格式无输出；provider benchmark **0.075s**、aggregation
benchmark **0.028s**；convert **1.078s**、mosdns **8.126s**、oxidns **1.784s**。

其余已完成的抽检命令（均退出 0）：

```sh
go test -race -p 4 -count=1 -timeout=4m ./internal/provider -run '^TestReviewN88d|^TestN88d(RunFlightShutdownAfterCallback|ShutdownBetweenCacheAndArtifact)$'
CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout=4m -v -run '^TestReviewN88dProviderByteSamples$|^TestRejectedSourceDoesNotPublish$/[012]$' ./internal/api/aggregations/difftest
CONFIGFLOW_DIFF=1 go test -p 4 -count=1 -timeout=4m ./internal/api/subscriptions/difftest -run '^TestProviderDeliveryGraphs$/none/single/(valid|shape|dangling)$|^TestAlternateFormatFilters$'
go test -race -p 4 -count=1 -timeout=4m ./internal/repo ./internal/oxidns ./internal/mosdns -run '^TestN88dCommitRuntimeAndLeafLocks$|^TestC29(RuntimeShutdownRejectsEveryOwner|RealRuntimeDisconnectCompletesEveryOwner)$|^TestCFG09'
```

尾行：provider **2.535s**；aggregation difftest **6.766s**；subscription difftest **4.077s**；
repo **2.088s**、oxidns **1.895s**、mosdns **3.701s**。

期望负控实际命令为 `go test -p 4 -count=1 -timeout=3m -run '^TestReviewN88dReferenceBindings$/262$' ./internal/provider`。
分配负控及恢复后正控为 `go test -p 4 -count=1 -timeout=3m -run '^TestN88d(NoConfigDecodePerProvider|AggregationDoesNotDecodeConfigPerNode)$' ./internal/provider ./internal/aggregations`。
Python 参考命令为 `PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/provider/testdata/n88d_reference.py`。
临时控制脚本／baseline tar 未提交，源文件与参考文件均在 finally 恢复。

验证中还修正了两处测试准备：本次新测试曾误把只负责打印的 diffText 当比较器，改为
先 jsonx.Equal；原 TestManualArtifactShutdownBeforePreviewCommit 独立执行缺少
mihomoenv.Wire 初始化，补上与同文件其他测试相同的生产接线。两者不是产品发现，未改期望。

无新依赖、无新 fuzz target、无操作入口变更；未重复作者 fuzz。提交号由最终交回消息给出。

## Opus HR 补修复核

复核者 fresh Opus（未参与本部分代码），对象 `go-review-n8-8d@c127f642e` 中
`Previews.Save`／`Receipts.Capture` 去掉 Uninterruptible 的两项补修。已读 AGENT-BRIEF、
WORKFLOW 第四常设项、o3-concurrency、a14-publish。执行均在 `.32`，`GOMAXPROCS=4`、`-p 4`。

结论：**0 high、0 medium、2 low（1 项已补测试，1 项接受并记录）；补修本身正确。**

逐项结论：
1. **每个提交点前检查 ctx**：Save 在锁内准入、每次淘汰 RemoveAll 前、建新目录前检查；
   bundle.tar.gz／record.json 由 atomicWrite 在建临时文件前和 rename 前各检查一次。
   Capture 在锁内准入、archive／receipt.json 两次 atomicWrite、每次修剪删除前检查。
   两个 owner 入口都先 `lifecycle.Detach`，`Publish`／`PublishDirect` 外层还有 `RunDetached`，
   请求断开不会取消；真实 Runtime 正控证明断开后两个文件均写完。
2. **半状态**：preview 在 record.json 写入前被关机时，defer 删除整个新目录；进程崩溃留下的
   无 record.json 目录被 Load 判为不可用，按 mtime 计入额度并最终被淘汰。receipt 中
   archive 先写、receipt.json 后写，receipt.json 是登记点：同一 deployment 再次捕获
   （AdvanceContent 形态）时关机，旧 receipt.json 逐字节不变，它引用的旧 archive 未被修剪；
   修剪只在 receipt.json 写入之后。发布链中 Capture 被关机打断时 publishDirect 在上传前
   返回 ctx 错误，部署停在 preparing，与关机打断其它准备阶段（资源检查、OxiDNS 校验）相同，
   由既有启动恢复处理；设备未被写入，A14 链没有出现“已上传未登记”的半截。
3. **锁顺序**：未变。Capture 仍在取 L4 receipt 锁之前完成 repo 读取（retainedReceipts），
   锁内只做文件操作；Save 只持有 p.mu（L4）。没有新增锁或在 L4 内访问 repo。

### Low L1 — 原 preview 回归没有覆盖两个文件替换点（已补）

`TestReviewN88dPreviewShutdownBeforePublication` 在第二次时钟读取（淘汰判定）处关机，
命中的是锁内准入后的 ctx 检查，没有走到 bundle.tar.gz／record.json 两个提交点，也没有
验证中途关机后的目录清理（负控 NC3 去掉清理时它仍通过）。为 Previews 增加与 Receipts
相同的 `writeFile` 观察点（生产为 atomicWrite），新增
`TestReviewN88dOpusPreviewShutdownAtEachReplacement`：真实 Runtime 在每次替换前关机，
要求返回 Canceled、目录列表与之前完全相同、旧 preview 仍可 Load；断开时两文件按顺序写完且新
preview 可 Load。另增 `TestReviewN88dOpusRecaptureShutdownKeepsReferencedArchive` 覆盖上面第 2 点。

### Low L2 — 淘汰后关机会丢掉被淘汰的旧 preview（接受）

去掉 Uninterruptible 后，配额满或过期时先删旧 preview，若随即关机，新 preview 不写，旧的
已删。被删的只会是过期或超出配额、本次保存本来就要淘汰的项；持有它的用户得到既有的
“预览不可用”提示后重新预览即可，不产生不一致状态。改成“先写后删”会让磁盘额度临时超限，
不值得，保持现状。

### 负控

在服务器副本中逐项变异后只跑 `^TestReviewN88d`，随后恢复：

| 变异 | 失败的测试 |
|---|---|
| NC1 Save 锁内恢复 `context.WithoutCancel` | Opus preview 新测试、原 preview 回归 |
| NC2 Capture 锁内恢复 `context.WithoutCancel` | Opus recapture 新测试、原 receipt 回归 |
| NC3 去掉 preview 失败目录清理 | 仅 Opus preview 新测试 |
| NC4 receipt.json 先于 archive 写入 | Opus recapture 新测试、原 receipt 回归 |

命令（前缀 `RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && …'`）：

```sh
gofmt -l internal/deploy && go vet ./internal/deploy/
go test -race -p 4 -count=1 -timeout=4m ./internal/deploy -run '^TestReviewN88d|^TestManualArtifactShutdownBeforePreviewCommit$|^TestShutdownRejectsEveryPublishCommit$|^TestPythonPrivateStoresLoad$|^TestReceipt|^TestPreview'
go vet -p 4 ./...
go test -race -p 4 -count=1 -timeout=4m ./internal/lifecycle -run '^TestConcurrencyFacadeBoundary$'
go test -race -p 4 -count=1 -timeout=4m ./internal/deploy -run '^TestManualArtifact|^TestReviewN88d'
```

尾行：gofmt／vet 无输出；deploy 正控 `ok … 25.939s`，NC1–NC4 均 `exit=1`，恢复后
`ok … 25.971s`；lifecycle `ok … 2.566s`；deploy `ok … 18.517s`。负控脚本未提交。
