# C29 对抗式复核

状态：复核修复及定向验证完成，最终门禁待运行；C29 自身发现均已关闭。
分支 `go-review-c29`，起点 `go-port-c29@6640783c`（工作区已确认干净），复核者未参与移植。
只通过 rt 在测试服务器执行测试、gofmt、vet；无子 agent、无 push、无生产访问，测试仅用合成数据。

## 发现及修复

- **F1，中，已修复：草稿 DTO 误把大小写不同的 JSON 成员当作编辑输入。**
  `encoding/json` 默认不区分 struct 字段名大小写；`Settings` 可替代必需的 `settings`，
  `BASE_URL`、`CHANGES` 等无关成员也会进入校验或修改编辑意图，与 Flask/N8-6 exact-case 合同不符。
  `draftRequest.UnmarshalJSON` 现在只通过原生 JSON map 取精确名称，保留 RawMessage 与原有绑定/重复成员合同。
  没有新增解析器或更改共享 Bind。
  `TestReviewC29ExactRequestKeys` 在两个前缀逐个覆盖七个草稿方法，包含必需字段、全部可选字段、
  native_config 以及组合干扰字段；逐请求比较真实 Flask 的状态、headers、完整 JSON 和 DB/files。
- **F2，中，已修复：混合新增的更新器错误定位到无关插件。**
  同时新增 lazy_cache 和无订阅来源的 rules_download 时，Go 用全部新增 tag 查找错误位置，
  Python 只用新增 updater；导致 references 指向 plugins[0].args/lazy_cache，而非
  plugins[1].args/rules_download，中文消息中的路径也不同。现在目标限制到 updater 交集，
  继续复用公共 reference/资源引擎。`TestReviewC29NativeReferenceTargets` 对两个 HTTP 前缀
  严格比较真实 Flask 的完整错误及完整存储，没有修改 oracle 或 normalization。
- **F3，中，已修复：template reader 调用者吞掉 policy/alias cause 和行诊断。**
  safeLoad 已返回共享 policy/alias 错误，但 templateDocument、forwardUpstreams、fallbackUpstreams、
  prepareFallback 和 patchField 先看 ok=false，把它改成普通语法错误；fallback 分支还重复进入 forward。
  现在先返回 err，仅真正的 ErrSyntax 保留原语法合同。没有改业务 builder 或恢复宽松 reader。
  `TestReviewC29ReaderCausesReachConsumers` 覆盖 5 类拒绝 × 4 个 template 字段的 Generate，及
  local/remote/fallback 的 15 个 ApplyCommonSettings 公共边界，检查 errors.Is 和无产物。
  `TestReviewC29ReaderPolicyHTTP` 在两个前缀的四个字段逐个比较真实 Python 接受 D1 的对照，
  明确断言 Go 400、共享完整行诊断及双方 DB/files 不变。差异属于已登记的 §5.7/D1 类别，未扩大豁免。

三项均先在修复前运行新增回归而失败；代表性输出：

```text
TestReviewC29ExactRequestKeys: status/body differs (Settings / BASE_URL)
TestReviewC29NativeReferenceTargets:
  Go:     规则定时更新需要当前配置引用至少一个可下载订阅（plugins[0].args）, tag=lazy_cache
  Python: 规则定时更新需要当前配置引用至少一个可下载订阅（plugins[1].args）, tag=rules_download
TestReviewC29ReaderCausesReachConsumers:
  Generate lost reader cause: DNS 上游 YAML 语法无效
  Generate lost reader cause: OxiDNS 高级 YAML 语法无效
```

未修复命令在 .64 执行（前两项 CONFIGFLOW_DIFF=1）：
`go test -count=1 -run TestReviewC29ExactRequestKeys ./internal/api/oxidns/difftest`；
`go test -count=1 -run TestReviewC29NativeReferenceTargets ./internal/api/oxidns/difftest`；
`go test -count=1 -run TestReviewC29ReaderCausesReachConsumers ./internal/oxidns`。
均通过 `RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && …'`，退出 1。

## 已完成审计

- 逐函数/分支对照 backend/common/oxidns_native_apply.py、utils/oxidns_archive.py、routes/oxidns.py
  和 profiles.py 的九个 wrapper；追读 settings、native resource validator、common lifecycle、
  editor、routing/withdrawal/reference 和共享 MosDNS materializer。核对作者 HANDOFF、PROGRESS C29、
  AGENT-BRIEF、PORTING 顶部/4b、WORKFLOW 四常设项、O3 2.5/Clarified、N7/N8 §5.4/5.7、
  review-c31/c32/a24 问题类别。未复制 C22–C27 或 MosDNS 逻辑，无 Python 模拟、无新依赖。
- Native 明确/唯一自动重命名、同批参数编辑、禁止覆盖/重复/空 tag、环境/外部引用、标准新增/恢复、
  空 fallback、删除依赖、cron、cache dump、Hosts 来源撤回和受管下载协调均保留；
  既有 222 条真实 Python 公共参照和完整 OxiDNS 包均复验。原输入 post-state 仍严格比较。
- 18 条路由的成功/失败、JSON/中文、no-store、ETag、强/弱/星号 If-Match、profile scope 和落盘有
  实时 Flask 差分；注册唯一性由完整 server.NewApp 测试覆盖。新增管理边界测试验证全部 18 对
  method/path 均拒绝 config/rule-proxy query token + Bearer（共 36 请求），并对照无鉴权时的
  missing profile 解析顺序。有效 JWT 的普通读写流程和未鉴权失败继续由作者矩阵覆盖。
- If-Match 对 fresh stored OxiDNS 和 InvalidateFollow 在同一 UpdateProfileTransaction callback 内执行，
  只替换 OxiDNS 字段；SaveSettings 返回本次提交的 ETag 和 config revision，不二次读取。
  同一 validator 竞争仅一次成功、一次409、不增加失败请求 revision；断开请求排队保存仍保留
  并发 MosDNS 字段，既有真实 Runtime 回归继续通过。
- ZIP 调用共享 ArchivePath/BuildMosDNSZip；资源校验、include/state 排除、二进制附件、重复来源获取一次、
  同路径去重/冲突、权限及顺序均有 18 组真实 Python ZIP 对照。
  新增 TestReviewC29ArchiveLimits 经 BuildZIP 测试相对路径穿越/绝对路径/空段/反斜杠/控制字符、
  ././state、custom/download 两种路径、canonical/config.yaml 冲突、文件数/字节预算和恰好达到预算，
  拒绝前 source I/O 为零；沿用共享限制 seam，不另写资源预算或 materializer。
- SaveSettings：Detach 后、repo 事务前、锁内 callback 入口及交回候选前检查 ctx.Err。
  BuildZIP：Detach 后、shared archive 调用前、返回产物前检查；取消不交付 ZIP。
  handlers 传原 ctx，无 handler WithoutCancel。其余 draft 操作为纯 caller-owned 变换，不持久化。
  TestC29RuntimeShutdownRejectsEveryOwner 使用真实 Start/BindRequest/Shutdown 验证两个 owner 返回
  context.Canceled、DB/files/commit count/revision 不变；TestC29RealRuntimeDisconnectCompletesEveryOwner
  验证请求断开后保存与完整 ZIP 均完成。
- HEAD 只读、非有限 JSON 拒绝、§5.7 reader 差异均已登记 behaviour-changes.md。
  HEAD 既有测试明确复现 Python 写入 bug，Go 沿用 GET 全 headers/空 body/存储不变；
  16 个 POST × NaN/Infinity/-Infinity 的 48 个真实 Flask 控制继续执行。
  reader 仅更换读取层，D1/tag-shape/merge/alias 预算由 yamlx 统一；冻结旧 reader 只在 test 中，
  输入迁移门不读取 Python 预期输出，unknown=0，计数见下。没有吞掉未知差异或修改 baseline。
- 新增文件：internal/oxidns/review_c29_test.go、internal/api/oxidns/difftest/review_test.go。
  无新增用户操作入口或功能变更，无需 doc 截图变更。

## 已知、已分派的共享层窗口（不是 C29 未决发现）

协调者 2026-11-18 明确分派给 **6e5c7760 / O3-6c**，在 O3-6a 合入后完成；
C29 不等待或复制共享修复，O3-6c 合入后由协调者单独复核这条链。

- internal/mosdns/archive.go：BuildMosDNSZip 入口 context.WithoutCancel（当前399行）丢失 Runtime
  parent；fetchSource → refresh.ReadRuleSource/RefreshRuleSource 在途下载及 cache/status 工作继续。
  C29 的最终 ctx 检查能拦截产物交付，不能替代共享 owner 的 I/O/提交取消。
- internal/repo/repository.go：writeLocked 在 updater 返回后继续验证，再调用 commitStore；
  internal/repo/store_rows.go 的 commitStore → store.Commit 无 ctx，最终 SQLite 提交前没有取消闸门。
  C29 callback 退出后的此窗口属于共享 repo 提交 owner。
- internal/rulefetch/cache.go：WriteValidatedRuleCache 的 TouchUnchangedCache/WriteSharedText；
  internal/rules/ruleutils.go：TouchUnchangedCache 最后的 os.Chtimes；
  internal/repo/files.go：WriteSharedText 锁内调用无 ctx 的 writeAtomic，后者 os.Rename 主文件及 .bak。
  下载后到这些最终 cache touch/replace 点的取消闸门由 O3-6c 补齐。
- 同链状态记录：internal/refresh/scheduled.go 的 recordStatus 调用 Store.Record 无 ctx；
  internal/taskstatus/taskstatus.go 的 write 最终 os.Rename，同属既定 O3-6c context 化任务。

## rt 验证与输出

本次复核所有命令固定 **192.168.0.64**。以下均在前台执行并等待退出。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -run TestReviewC29 ./internal/api/oxidns/difftest && go test -count=1 -run "^TestN73OxidnsReaderInputs$" -v -timeout=15m ./internal/oxidns'
```

退出0：
```text
ok configflow/internal/api/oxidns/difftest 2.377s
native:   equal=52768 D1=547 policy=177 alias=96 unknown=0 (53588)
template: equal=325967 D1=132 alias=133 unknown=0 (326232)
PASS
ok configflow/internal/oxidns 88.241s
```

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/oxidns/review_c29_test.go --back server/internal/api/oxidns/oxidns.go --back server/internal/api/oxidns/difftest/review_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/oxidns/review_c29_test.go internal/api/oxidns/oxidns.go internal/api/oxidns/difftest/review_test.go; go test -race -count=1 -run "^TestReviewC29" ./internal/oxidns; CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 -timeout=15m ./internal/api/oxidns/...'
```

退出0：oxidns 3.134s、api/oxidns 2.342s、api/oxidns/difftest 25.822s。

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/oxidns/difftest/review_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/api/oxidns/difftest/review_test.go; CONFIGFLOW_DIFF=1 go test -count=1 -timeout=20m ./internal/oxidns ./internal/api/oxidns/... > /tmp/review-c29-scoped.log 2>&1; tail -12 /tmp/review-c29-scoped.log'
```

退出0：oxidns 111.325s、api/oxidns 0.376s、difftest 9.274s。完整核心参照包括迁移门和所有既有 C22–C29 测试。
随后补充 D1 HTTP 和全部管理 token 矩阵：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/oxidns/difftest/review_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/api/oxidns/difftest/review_test.go; CONFIGFLOW_DIFF=1 go test -race -count=1 -run "^TestReviewC29" -v ./internal/api/oxidns/difftest > /tmp/review-c29-http.log 2>&1; tail -15 /tmp/review-c29-http.log'
```

退出0：TestReviewC29ReaderPolicyHTTP PASS、TestReviewC29AdminBoundary PASS；difftest 12.114s。
最后收紧预算失败的具体错误及成功边界，再执行：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/oxidns/review_c29_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/oxidns/review_c29_test.go; test -z "$(gofmt -l internal/ tools/ cmd/)"; go vet ./internal/oxidns ./internal/api/oxidns/...; CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 -timeout=15m -run "^(TestReviewC29.*|TestC29.*|TestN73OxidnsReaderSafety|TestN73OxidnsNativeBusinessBuilderPreserved)$" ./internal/oxidns ./internal/api/oxidns/...; printf "C29_SCOPED_REVIEW_PASS\n"'
```

退出0，gofmt/vet 无输出：
```text
ok configflow/internal/oxidns 42.633s
ok configflow/internal/api/oxidns 2.226s
ok configflow/internal/api/oxidns/difftest 45.016s
C29_SCOPED_REVIEW_PASS
```

完整 gate/fuzz 与最终 rebase 结果待下一节补齐。若 .64 gate 排队超过15分钟，
按协调者明确指示停止排队并交回，由协调者使用 cf-gate 跑完整验收，不把排队当通过。

协调同步使用 [Paseo 技能](/Users/sunbroke/.agents/skills/paseo/SKILL.md)，只向本任务协调者发送复核进度/共享依赖说明。
