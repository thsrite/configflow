# C33 独立复核

状态：代码复核完成，7项发现（3 high / 2 medium / 2 low）已修复；最终门禁待执行。
分支 `go-review-c33`，独立于作者，从 `go-port-c33@a4445071` 分出，先 rebase 到本地
`go@dda8973d`（A13），再到 `6e37489ff`（L1）和 `cb5732ec5`（C29及其复核）。
最终验收基底为 `go@28e6e0c12`（R09）。先前冲突为 behaviour-changes.md 和 proddiff 自检入口；
后续 C29 合并涉及 server.go import、routes.tsv 和差异登记，保留双方注册/条目/测试，main 入口置于文件末尾。
无子 agent、无 push；自写用例及 fixture 只用合成数据。所有测试/格式/vet 经 rt 在服务器前台运行。

## 发现与修复

- **F1，high，已修：失效标记检查遇权限错误时复活 legacy 缓存。**
  场景：DB/缓存编辑已有 `.invalid`，by-id 目录不能搜索，而上级 legacy 文件仍可读。
  `fileExists` 将 EACCES 当成不存在，两个读路径最终返回旧 legacy。
  修复为仅 `os.ErrNotExist` 允许回退，其他 marker 检查错误全部 fail closed；不改下载/写入协议。
  `TestReviewUnreadableReceiptCannotReviveLegacyCache` 修复前返回 `+.stale-legacy.example`，
  修复后 shared cache 读 nil，HTTP 交付 DB inline 字节。修复提交：`e27c9c50c`。
- **F2，high，已修：交付读请求无锁补写旧快照，能越过三步提交协议。**
  场景：请求已取得旧 compat 快照；draft 提交新 DB 并 rename；标记尚未清除时请求 fallback 到旧 inline，
  再经 SaveRuleToLocal 覆盖新稳定文件；draft 随后清 marker，后续请求交付旧缓存。
  `TestReviewOldInlineReaderCannotOverwriteCommittedDraft` 在真实 rename 后执行已有快照 reader，
  不替换存储、超时、HTTP 参数。另 `TestReviewInlineFallbackDoesNotPublishOutsideEditLease` 修复前
  直接观察失效期间的 GET 重写稳定文件。
  修复：标记存在或无法确认时只交付 inline；普通冷 miss 保留补缓存，先重新读取当前声明。
  一致性守卫收敛到现有 rulefetch.WriteValidatedRuleCache：所有 inline 调用方在现有 repo 事务锁内核对
  当前 content 声明和原始字节，过期/已删除/已变 remote 的旧调用不发布；有 marker 同样不发布。
  内部 sentinel 中止未改变的 DB 候选，既不提交 DB，也不触发 hooks，不增加 API。
  调用方扫描包含 discovery、Agent 工具、compose；新增 TestReviewOtherInlineWritersCannotOverwriteCommittedDraft
  直接调用它们共用的真实 SaveRuleToLocal，验证编辑完成后迟到旧写不覆盖。
  `TestReviewColdInlineReaderPopulatesCurrentDeclaration` 验证旧请求可完成，补写字节来自新 DB，revision 不变。
  C32 原逐路由和冷交付差分保持完整文件比较，不豁免缺失缓存。修复提交：`e27c9c50c`。
- **F3，medium，已修：两个 reorder 入口把真值非对象丢成空对象，错误返回404。**
  `[1]`、`"bad"`、42、true 在真实 Flask 为500，Go 为404；空列表/null/false/0/空字符串原来均按空对象处理。
  已保留严格 JSON/media 边界和 falsey 默认，仅真值非对象提前返回 native500。
  `TestReviewReorderMalformedHTTPParity` 对两路由逐请求比较状态、envelope、DB/files。
  Python 内部 AttributeError 文字按既有标准不复制；只归一这两路由、合法非对象请求的已知内部诊断，
  其他字段及存储完整比较。修复提交：`d442f420a`。
- **F4，low，已修：live 差分的 D2 形状检查接受大写 hex。**
  生产 proddiff 配置已有精确路由/顶层 `revision`/`^[0-9a-f]{64}$`，但 C33 live harness
  的 hex.DecodeString 同样接受大写，弱于生产规范。改为同样的完整小写 regex；
  `TestReviewDraftRevisionShape` 有合法 token、长短、大写、非字符串、缺值及换行负控。
  生产比较器的字段/路由/嵌套值正负控未放宽。修复提交：`d442f420a`。
- **F5，medium，已修：严格请求的旧解析→原生转换把非有限 JSON 变成500。**
  旧 c.JSON 接受 NaN/Infinity/-Infinity，随后 json.Marshal(jsonx.Object) 拒绝它们，导致原本应在
  绑定阶段拒绝的内容走内部500。请求 adapter 改用现有 Bind/BindSilent + RawMessage，精确成员/未知值
  继续由 model.Fields 持有；不新增解析器。严格入口返回 malformed400，batch silent返回原有400；
  四个明确捕获 JSON 异常的入口仍按其既有 malformed500合同处理。
  `TestReviewNonFiniteBindingUsesMalformedBoundary` 对14个入口×3字面量，使用真实 Flask malformed
  请求提供每个边界的完整响应控制，状态、消息、headers及存储不归一。
  `TestReviewPythonNonFiniteAcceptanceIsExplicit` 独立证明真实 Python check-content 接受 unused:NaN
  而 Go400，双方均无写入；归入已批准的 N8 原生 JSON 差异。修复提交：`1edf24f54`。
- **F6，low，已修：预览的来源选择未保留宽松默认/未知ID语义。**
  `subscriptions/aggregations` 的 false/0/空字符串/空对象在 Python 按空选择处理；混合数组里的
  非字符串成员无法命中来源，仍会处理其余字符串ID。Go 的 []string 解码却直接500。
  现在 adapter 保留空值默认，数组忽略无法命中来源的成员；同时将真值非数组明确400拒绝，修复 Python
  偶然迭代字符串字符/对象键的输入验证漏洞（与作者已登记的非数组 rule_ids 同类），合法字符串数组不变。
  `TestReviewPreviewSelectionShapes` 对两个来源比较16组宽松输入的真实Flask完整响应/存储；
  另独立证明4组非数组 Python200/Go400，双方无写入。修复提交：`808d39374`。
- **F7，high，已修：三步协议的发布/撤销没有与其他 owner 的 DB 提交串行化。**
  C33 已提交声明A、尚未rename时，C37/A24等owner可提交新声明B；C33随后发布A并清marker，交付就会
  落到“新DB B + 旧缓存A”。同样，C33事务明确拒绝后盲目清marker，会覆盖另一owner在此期间提交的新来源
  所需的失效状态。只给C33入口加L2锁不能挡住其他owner的L3事务。
  现在stage记下原声明和拟提交声明；发布在现有repo L3内核对拟提交的五个来源字段，再完成rename/Sync/
  清marker；已过期stage拒绝发布并保留marker。撤销只在L3内确认原声明仍相同才清自己新建的marker；
  原有marker绝不清。内部staging失败沿用已持有的事务上下文，避免重入锁。两种文件副作用都中止未改变的
  DB候选，不推进revision/hook，不新增repo接口。来源以外的名称、interval等独立编辑不会阻止发布；
  原有prepared raw cache与cache→inline优先级保留。
  Create的实际提交也加入同ID编辑/清理lease，避免新一代来源在旧Delete清理完成前落地；下载仍在lease前，
  保留慢URL下载与inline创建竞争的Python行为。
  `TestReviewStageRejectsNewerCommittedDeclaration` 用真实SQLite依次提交A/B再执行暂存发布，验证拒绝、
  DB/revision不变、DB B交付及手动缓存修复；`TestReviewAbortClearsOnlyItsUnchangedBaseline` 有来源改动
  与仅metadata两侧控制；`TestReviewStageAllowsMetadataAndPreservesPreparedBytes` 防止过宽守卫。
  rename交错测试验证真实repo事务上下文；`TestReviewCreateUsesTheSourceCleanupLease` 在真实BeforeCommit
  观察同一lease；`TestReviewCreateDoesNotHoldCleanupLeaseDuringDownload` 用真实GET在回环服务端完成
  同ID inline创建，再让迟到下载409，核对赢家DB/cache，未改生产超时。修复提交：协议串行化提交（ID文末）。

## 缓存崩溃/故障核对

| 中断/故障点 | 安全结果及恢复 |
|---|---|
| temp 写/Sync、marker 创建/Sync、目录 Sync 失败 | DB 尚未提交；保留原声明；不清除原有失效标记；已完成 temp/marker 清理尽力执行 |
| stage 完成、DB 前 kill | DB 为旧声明，marker 拒绝所有旧文件；inline 仍可交付；下次编辑/手动缓存修复 |
| DB 未知提交错误 | failClosed 保留 durable marker，不能据内存错误误判回滚；真实 BeforeCommit 故障回归继续执行 |
| DB 后、rename 前 kill/关机 | 新 DB、旧文件被 marker 屏蔽；关机后不 rename/清理；新 generation 可修复 |
| rename 失败 | 返回500，保留 marker；移除文件尽力执行，失败也不能读旧 stable/legacy |
| rename 后、marker 前 kill/目录 Sync 失败 | 文件不作为已验证缓存交付；DB inline 可用；手动缓存重新 stage/Sync/rename/清 marker |
| marker 移除及目录 Sync 后 kill | 新文件已先持久化；重启读新缓存；若移除未持久化而 marker 重现，仍安全回退 DB |
| DB后有另一owner提交新来源 | L3内来源字段守卫拒绝旧stage，marker保留；交付新DB，手动缓存/新编辑可修复 |
| 明确回滚后有另一owner提交新来源 | L3内baseline守卫禁止清marker，绝不因旧请求撤销而恢复旧文件 |

新增 `TestReviewCacheCrashRecovery` 真实重执行测试二进制，分别在 staged/committed/renamed/complete
四个位置 SIGKILL，自另一个进程重新打开 SQLite，验证三种 HTTP 交付、失效缓存拒绝和手动修复后的
缓存字节、marker/temp 清理。没有把正常函数返回当成崩溃。磁盘满不填共享盘，按同一写/Sync错误分支审计；
实际故障注入沿用作者的写权限失败、rename失败、未知提交错误及真实 Runtime 取消测试。

## 审计范围与验收

已读 Python 两路由全文、三项主测试、policy editor 内容半边、DNS follow 和 scheduled rule source 相关分支；
已核对作者交接、25方法矩阵、WORKFLOW四常设项、O3/N7/N8设计及 C29/C32/A24/N8-4ef 问题类别。
当前树无 review-c29.md，已通过 `git show a2df30f6:server/parity/review-c29.md` 完整读取独立报告。

共享 taskstatus/repo/rulefetch 的最终提交/锁等待取消窗口按明确分工仍归 O3-6c，**不是 C33 发现**；
本轮缓存 reader 的声明一致性及 marker 判定不等同于该关机窗口。不得把 C33 回归当成共享层已完成。

**另需协调者分派的跨模块后续（不等同于O3-6c）：** C37 `discovery/write.go` 与 A24
`api/agents/client_tools_adapter.go:applyClientIP` 自己仍有“先提交DB，再SaveRuleToLocal”的来源修改路径。
本轮已阻止它们的迟到旧cache覆盖C33新声明，也阻止C33旧stage覆盖它们的新声明；但它们独立操作若在
自身DB提交后、首次补缓存前崩溃，仍需由相应owner复用持久失效/发布协议解决。不能把共享writer守卫或
O3-6c的关机取消检查宣称为这些owner整条DB/cache原子性已经完成；本报告的verified范围仅为C33。

`TestReviewAuthenticationAllowlist` 覆盖全部23个管理 method/path 的缺省/config/rule-proxy/retired/wrong
query token 与 Bearer，均真实 Flask/Go 401；两个匿名内容出口对这些 token 均200，比较每次完整存储。
有效JWT成功矩阵继续由作者25方法用例覆盖。`TestReviewStrictAndSilentBinding` 对14个写入口比较
JSON/text/plain/+json 与畸形/有效body；保留严格400/415、捕获异常500和 batch silent400的区别。
oracle 不再强制覆盖显式 Content-Type，实际 Go 请求和 Flask 收到同一个 media 参数。

`TestReviewConnectivityWorkerBoundAndRawRequest` 对27个真实回环 HEAD 验证峰值10、全部收敛、结果ID/status
对应，原始 body为空、没有 Content-Type/Authorization。没有覆盖或缩短生产 timeout。现有回环 forward
proxy 用例继续核对 GET URI/body/headers。所有 native DNS/live路径结束检查 lockx.Violations()==0。

响应继续经过中央 JSON/redact 出口，错误调用共享 SafeErrorDetails/DescribeFailure，不回显地址或请求体；
preview只投影固定显示成员，不返回节点密码等原记录。编辑器中的管理员可编辑用户来源配置继续遵循原有
可见策略，不套用A24公共Agent投影；没有改中央脱敏策略、凭据字段或用户名语义。相关rulefetch/refresh和
合并后的Agent工具测试一起执行。无新依赖、私有业务算法副本或新增用户操作入口，不需新增doc截图。

15类 Runtime 测试逐项核对：library create/put/delete/draft/reorder、proxy-domain、cache、批量连通性、
single probe、group全局/指定profile create、put/delete/health/reorder。每个owner在Detach后检查ctx，
事务callback入口和交回候选前检查ctx；stage/marker/Sync/rename/remove均在实际动作前检查。
已有Runtime八次停止重试和请求断开完成对照继续运行；rename阶段关机保留marker并由新generation修复。

普通CRUD、DNS native parking/rebase、监听入口、interval/history、公开内容和合法输入无未登记功能差异。
有意差异仅作者已登记的可靠性/HEAD/非数组rule_ids纠错、D1/D2，以及本轮inline守卫、N8非有限输入和
非数组预览选择纠错；全部有独立控制，不改生产baseline或扩大生产normalize。

## 定向验证（均通过rt）

合入C29（cb5732ec5）后，.32前台命令：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/ tools/ cmd/)"; go vet ./...; go test -race -count=1 -run "TestReviewC29|TestC29|TestN73OxidnsReaderSafety|TestN73OxidnsNativeBusinessBuilderPreserved" ./internal/oxidns; CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 ./internal/api/rulelibrary/... ./internal/api/proxygroups/... ./internal/api/rules/... ./internal/api/oxidns/... ./internal/rules ./internal/rulefetch ./internal/mosdns/difftest ./internal/subs/difftest ./internal/server'
```

退出0：gofmt/vet无输出；oxidns42.127s、C33 API111.073s/live140.268s、代理14.034s、C32 API120.945s/live254.575s、
C29 API4.038s/live83.430s、rules22.663s、rulefetch54.203s、C28live19.904s、C11live25.970s、server2.037s。
该轮是C29共享reader的完整增量检查；之后C33请求绑定/预览选择另按下面命令复验。

Bind修复后的.64合并树：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/ tools/ cmd/)"; CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 ./internal/api/rulelibrary/... ./internal/api/proxygroups/... ./internal/api/oxidns/... ./internal/server'
```

退出0：C33 API38.783s/live58.310s、代理5.887s、C29 API1.981s/live25.788s、server1.717s。

最后预览修复（基底28e6e0c12）的完整C33 race/live：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/api/proxygroups/messages.go -- bash -lc 'set -e; cd server; gofmt -w internal/api/proxygroups/messages.go; CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 ./internal/api/rulelibrary/... ./internal/api/proxygroups/... ./internal/server'
```

退出0：`ok configflow/internal/api/rulelibrary 92.565s`；difftest144.693s、proxygroups11.583s、server2.832s。
C33普通/错误/权限/HEAD/修复差异全部启用；没有用skip或替身绕过生产参数。

共享writer修改后也跑了Agent工具、discovery、rulefetch、refresh及其差分：Agent API250.222s、discovery13.831s/
live14.153s、rulefetch17.630s、refresh119.854s/live12.927s通过。那轮旧offline fixture用受保护writer创建未登记
来源而失败；已改为直接模拟既有物理缓存并保留原有cache优先级/零网络断言，.32补跑API rules43.457s/
library12.118s通过。未把旧轮组合退出1计作通过。最终全量门禁还会重新执行全部这些包。

独立fuzz及harness（.64；其时尚未合入C29/R09，fuzz目标逻辑不变）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/ tools/ cmd/)"; go vet ./...; go test -run "^$" -fuzz="^FuzzEditableContent$" -fuzztime=60s -parallel=2 ./internal/api/rulelibrary; cd ..; ~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py; ~/cf-test/.venv/bin/python -m unittest discover -s server/tools/proddiff -p test_proddiff.py'
```

退出0：fuzz39412次、93个新增interesting、`PASS`/61.048s；direct/discover各`Ran 42 tests ... OK`。

## 最终完整门禁

验收树：`go@28e6e0c12` + C33作者提交 + 四个复核修复，冻结tip `808d39374`。所有自写fixture合成；
仅proddiff按明确授权读取测试服务器现有离线快照，不连接生产；报告只记录new/stale/self-check。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -e GATE_LOGS=/home/sunbroke/.cache/rt-tmp/review-c33-complete-final-20261118 -- bash -lc 'mkdir -p "$GATE_LOGS"; exec setsid bash -c '\''echo $$ > "$GATE_LOGS/gate.pid"; date -Is; exec server/scripts/gate.sh -p 6 FuzzEditableContent:./internal/api/rulelibrary'\'''
```

05:15:20（Asia/Shanghai）取得slot1，进入检查，无排队等待。结果待本轮前台命令退出后补齐。
完整日志保留在上述GATE_LOGS；不引用作者历史GATE PASS作为本轮验收。

此前三轮分别因协调者要求新go/C29变基、原生绑定修复、预览选择修复而终止；均明确核对该worktree的
PID/PGID/cwd后只终止自己的进程组。前两轮排队未执行；第三轮通过格式/vet/双tag/observer和proddiff
`new=0/stale=0/self-check=PASS`后中止race，**不计完整通过**，没有修改门禁脚本或槽位上限。

新增独立测试文件：review_cache_test.go421行、review_crash_test.go124行、review_workers_test.go86行、
difftest/review_test.go203行。提交ID见各发现；最终报告提交后工作区必须干净，不push。
