# N8-9 rules 组独立复核

复核者 Codex（非作者），作者 `89c495b3`；复核对象
`go-n8-9-rules@0c58b6e0e`，工作分支 `go-review-n8-9-rules`。
按 AGENT-BRIEF、WORKFLOW 的 Leaner verification／第四常设项、n7-n8 的 N8-9
约束复核，并参照 health 独立复核和 batch6 的 R5／difftest 报告。

结论：**0 high、1 medium、1 low，均已修复**。迁移主体、独立期望、alias 预算和
窄读取设计在本次范围通过。只在 `.32` 跑定向验证，`GOMAXPROCS=4`、`-p 4`；
没有重跑作者整套、完整 gate、push 或子 agent。批次全门禁仍由协调者执行。

## Medium M1 — invalid receipt 的回退路径未全部 fail-closed

- `rules.CacheReadPath` 给旧摘要文件检查 receipt，却漏了旧 `by-id`／名称文件。
  两条实际读取入口回归在修复前都返回 `payload: [example.com]`，而且会迁移。
- 原有拒绝值 `<cache>.invalid/unavailable` 只是普通路径。把 receipt 换成目录，
  在其中放同名有效 YAML，真实读取入口会返回 `payload: [receipt.example]`。
  新增负控修复前失败，证明不是仅检查辅助函数。
- `os.Stat` 还把悬空 receipt 符号链接当成不存在。独立回归修复前返回旧有效字节，
  未阻止迁移。现在所有本组 receipt 检查改用 `Lstat`，判断目录项是否存在。
- 修复：四种缓存位置都检查 receipt；拒绝时统一返回空路径，discovery 的取消／
  仓储读取失败路径也使用相同结果。无 receipt 时保留原读取优先级和离线兜底。
  `writeCurrentInline` 的两处 receipt 检查同样使用 Lstat。
- 回归：`rulefetch/review_n89_test.go` 的三种旧地址各有拒绝和去掉 receipt 后成功迁移
  正控，另覆盖可读 sentinel、悬空 receipt；discovery 现有“不打开 View”测试增加悬空
  receipt。其拒绝期望由 sentinel 改成空路径，是上述实际失败的修复结果；未改 corpus。

## Low L1 — 旧缓存符号链接可以读取并固化规则目录外的字节

- 文件名本身使用摘要／净化名称，没有字符串穿越；但旧缓存或命名空间目录可为符号链接。
  修复前，真实 `ReadValidatedRuleCacheWithFormat` 把目录外有效 YAML 读出并迁移为新缓存。
  这是本机缓存目录的隔离缺口，不宣称新增了远程任意文件访问入口。
- 修复：`rules.CacheReadPath` 对选中的现存路径和规则根目录解析符号链接，再以
  `filepath.Rel/IsLocal` 检查包含关系，越界或解析错误拒绝读取；缺失文件仍走原缺失路径。
  迁移目标原有 `repo.SharedPath` 写入边界保留。
- 回归：真实迁移入口拒绝越界文件链接、不生成新缓存、不改外部文件；纯路径入口覆盖
  文件／命名空间链接 × 目录内／目录外四例。目录内有效链接继续可读，带 `../` 的 ID
  仍只生成摘要文件名。这里验证的是路径解析边界，未模拟恶意本机进程并发替换符号链接。

## 原始证据与 R5／D2 期望

- 对照 `go` 的 git blob：rules 的 corpus／review、rulefetch 的原 testdata、refresh 的
  corpus、Mihomo corpus 未变；没有调整原 archives 来适配实现。
- 三个修改过的 mosdns gzip 导出 `go` 版本后在 `.32` 逐项比较，除了 metadata／sources
  外整个文档相同；entries 分别 **2863／0／5**，与基线完全相同。
- 审查参考生成器：调用真实 Python 的候选、lower、policy、调度、history、task 业务。
  投影只定义共享 model 的已批准 R5／R8 及 D2 摘要输入，未读取 Go 输出计算期望。
  cache 逐行保留 input／original；group 把原始整行（含结果）作为 input；refresh 绑定
  index／input／original，scenario 文件输入还由整份 corpus SHA 保护；convert 以原
  archive SHA、行号、函数名、精确旧 path 绑定，并在录制时确认其他完整输出没有变化。
- 在 `.32` 重录 cache、group、alias、migration、refresh 五份独立参考，与提交版本
  **逐字节一致**：600 cache＋7 namespace＋114 policy；1500 group＋600 lower＋88 review
  lower；1 alias；6 历史名称；33 refresh（31 R5、2 R8）。录制后 finally 恢复原字节。
- 精确期望负控：仅把 cache 第一条 D2 行的 native 改为 `synthetic-negative-control`，
  真实 `TestCorpusCacheNames` 在该行失败并打印完整 got／want；finally 恢复后正向通过。
  没有分类器通配放过实际结果，也没有新增 baseline 豁免。

## 迁移、性能、alias 与关机的其他检查

- 迁移：作者的实际离线刷新回归仍通过；旧内容需符合声明和格式。读取路径先以当前
  repo.View 验证声明，迁移在 UpdateShared 内重查声明、receipt、旧内容；以专用错误
  回滚文档事务，不变更 DB／revision。目标文件锁中重新检查存在性，暂存文件带旧 mtime，
  使用已带 ctx 的 `writeAtomicUsing`，不覆盖新目标、不延长 freshness。
- 新增真实 Runtime＋真实文件锁回归：等锁期间请求断开仍安装完整 80 KiB 旧内容；
  Shutdown 返回 Canceled、没有目标或临时文件；已持锁写方发布较新目标后，等待的迁移
  不覆盖、不改 mtime。另抽检原有 staging 后 Shutdown／disconnect／主 rename 后 shutdown
  的原子写回归，证明提交前取消会清理暂存、已经提交的原子批次会完成备份。
- 第四常设项：检查 refresh 的 transport receipt、node cache、health、taskstatus 到 repo／
  taskstatus 叶层的 ctx 传递；各 owner 在 Detach 后、回调后、提交前检查来源 generation。
  抽检 `TestNativeSourceOwnerKeepsGenerationAtEveryBoundary`、缓存写入及下载回调后关机用例，
  全部使用真实 Runtime。关机不继续写 health／task receipt，断开仍完成，旧 generation
  不能重启。不把单独 cancel context 当成关机证明；没有改生产超时和调度参数。
- 外部 anchor provider：`yamlx.Encode` 复制物理图并验证共享 alias 预算，随后
  MaterializeAliases 修复完整文档定义；不使用会截断的 Clone、不展开逻辑树。
  新增 4 层正控（物理节点不放大、Marshal 闭环）和20层负控（必须是 ErrAliasBudget）。
  临时只去掉 Encode 的 validateNode 后，实际 LowerProviderFilters 用例失败：
  `expected actual alias budget refusal, got <nil>`。恢复后通过，作者的外部 anchor／
  修改副本不污染原节点／递归拒绝用例也抽检通过。
- 已知 format：legacy adapter 的 library 是惰性的 ID／name 投影；稳定缓存及 invalid
  receipt 在 View 前处理。格式验证仍执行；没有为已知格式重新 decode 整份 library。
  旧名称首次迁移有意访问仓储以证明所有权，不声称该慢路径也不读声明。
  正常 race 实测 small／large **509／506 allocs、63745／63466 bytes/read**。
  临时在已知格式路径加入同次数分配的整块内容复制，alloc 阈值仍通过（481／483），
  bytes 阈值失败：**43960／1102278 bytes/read**，能拦住同分配次数的大块复制回退。
  首次用 strings.Clone 注入时已在 alloc 阈值失败；第二次刻意等次数分配，单独证实 bytes
  阈值确实生效。所有临时源文件和参考修改都由 finally 恢复，未提交 mutation runner。
- convert：`git diff go...0c58b6e0e -- internal/convert/legacy.go` 确认生产仅四个回调改动。
  四条 Mihomo generate、两条 provider_downloads 用原 archive 的完整文档／report／输入
  mutation 比较，只有已绑定 D2 path 投影；再以原 Surge／Loon 各一条带过滤的正常
  profile 比较完整生成字节和 mutation。**八条全部通过**。没有修改生成算法或原期望。

## 定向命令与输出

全部在任务 worktree 使用下面前缀，实际运行主机均为 `.32`：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc '…'
```

修复前各新增产品回归的实际 Go 命令（通过上述 rt 执行，均退出1）：

```sh
cd server
go test -p 4 -race -count=1 -timeout=3m -run "^TestReviewN89" ./internal/rulefetch
go test -p 4 -race -count=1 -timeout=3m -run "^TestReviewN89ReceiptCannotBecomeReadableSentinel$" ./internal/rulefetch
go test -p 4 -race -count=1 -timeout=3m -run "^TestReviewN89DanglingReceiptStillBlocksCache$" ./internal/rulefetch
```

M1／L1 的初始失败分别见上；不存在编译错误冒充产品失败。

主定向验证（退出0，gofmt／vet 无输出）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/rules internal/rulefetch/review_n89_test.go internal/repo/review_n89_test.go internal/discovery/ruleowners.go internal/discovery/ruleowners_test.go internal/convert/review_n89_test.go)"; go vet -p 4 ./...; go test -p 4 -race -count=1 -timeout=3m -v -run "^TestKnownFormatReadAllocationsIgnoreUnrelatedLibrary$" ./internal/rulefetch; go test -p 4 -race -count=1 -timeout=5m -run "^TestReviewN89|^TestRuleCacheReadPath|^TestNativeCacheMigration|^TestLegacyCacheMigration|^TestKnownFormatNever|^TestProviderCopy|^TestHistoricalMigrationFilenameReference$|^TestNativeSourceOwnerKeepsGenerationAtEveryBoundary$|^TestCacheWriteUsesOriginatingRuntime|^TestRefreshCommitChecksRuntime|^TestO36cAtomicFileRuntimeBoundary$|^TestStableMatcher|^TestMatcherLegacy|^TestTypedEntryExpectedBytes$|^TestMalformedMembersUseTypedValidation$|^TestLibraryIndexStopsAfterFirstMatch$|^TestReviewC09(FilterCandidates|ValidateRulePolicies)$" ./internal/rules ./internal/rulefetch ./internal/repo ./internal/discovery ./internal/refresh ./internal/convert ./internal/rulesetfmt'
```

```text
ok configflow/internal/rulefetch 3.493s   # bytes/read 正控
ok configflow/internal/rules      1.687s
ok configflow/internal/rulefetch  8.713s
ok configflow/internal/repo       3.449s
ok configflow/internal/discovery  1.113s
ok configflow/internal/refresh    5.174s
ok configflow/internal/convert    6.626s
ok configflow/internal/rulesetfmt 1.026s
```

补充生成与 API／恢复期望抽检（退出0）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/convert/review_n89_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/convert/review_n89_test.go; go test -p 4 -race -count=1 -timeout=3m -v -run "^TestReviewN89ClientFilterGenerationSamples$" ./internal/convert; go test -p 4 -race -count=1 -timeout=3m -run "^TestCorpusCacheNames$|^TestNativeReferenceEvidence$|^TestUnknownCommitFailureRetainsDurableInvalidation$" ./internal/rules ./internal/refresh ./internal/api/rulelibrary; go vet -p 4 ./internal/convert; test -z "$(gofmt -l internal/convert/review_n89_test.go)"'
```

```text
ok configflow/internal/convert          11.838s
ok configflow/internal/rules             2.210s
ok configflow/internal/refresh           1.048s
ok configflow/internal/api/rulelibrary   2.397s
```

悬空 receipt 修复后再跑全仓 vet／格式和五包增量 race（退出0）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/rules internal/rulefetch/cache.go internal/rulefetch/review_n89_test.go internal/repo/review_n89_test.go internal/discovery/ruleowners.go internal/discovery/ruleowners_test.go internal/convert/review_n89_test.go)"; go vet -p 4 ./...; go test -p 4 -race -count=1 -timeout=5m -run "^TestReviewN89|^TestNativeCacheMigration|^TestLegacyCacheMigration|^TestCacheWriteUsesOriginatingRuntime|^TestRefreshCommitChecksRuntime|^TestStableMatcher|^TestMatcherLegacy|^TestUnknownCommitFailureRetainsDurableInvalidation$" ./internal/rules ./internal/rulefetch ./internal/repo ./internal/discovery ./internal/api/rulelibrary'
```

```text
ok configflow/internal/rules             1.123s
ok configflow/internal/rulefetch         8.441s
ok configflow/internal/repo              2.076s
ok configflow/internal/discovery         1.041s
ok configflow/internal/api/rulelibrary   3.242s
```

三个负控由 rt 中的 Python finally 包装临时修改并执行以下实际命令，均如期退出1：

```sh
go test -p 4 -count=1 -timeout=3m -run '^TestReviewN89ProviderCopyEnforcesAliasBudget$' ./internal/rules
go test -p 4 -count=1 -timeout=3m -run '^TestKnownFormatReadAllocationsIgnoreUnrelatedLibrary$' ./internal/rulefetch
go test -p 4 -count=1 -timeout=3m -run '^TestCorpusCacheNames$' ./internal/rules
```

独立参考实际以 `PYTHONPATH=<仓库根目录>` 和 `~/cf-test/.venv/bin/python` 运行
`server/internal/rules/testdata/n89_{cache,group,alias,migration}_reference.py`、
`server/internal/refresh/testdata/n89_reference.py`，每份先保存字节，重录后比较并 finally
恢复；输出计数如上。只读基线导出的临时 tar 已删除。未新增 fuzz target，未重复作者60s fuzz。

无未提交的产品残项；行为变化只修复上述缓存拒绝／隔离缺陷。N9／N10 和其他 owner
接口收口仍按原计划，不把本次抽检冒称全部生产切换或全局关机门禁。
