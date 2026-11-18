# N8-9 rules / rulefetch / refresh / rulesetfmt

独立复核已由非作者 Codex 完成，分支 `go-review-n8-9-rules`；新增 1 medium（旧地址／
异常 invalid receipt 未完全 fail-closed）、1 low（符号链接越出规则目录），均有修复前
失败证据和回归，已修复。独立参考重录、alias／分配阈值负控及八条生成样本通过；
测试仅 `.32`、`-p 4` 定向，未跑完整 gate／push。详见
[review-n8-9-rules.md](../review-n8-9-rules.md)。完整批次验证由协调者执行。

以下为作者交回记录。分支 `go-n8-9-rules`，已变基到 `go@d4fab6441`。
产品主体收口 `fcc671d9d`；共享字段证据/迁移文件 API 为 `55f13acb7`。
最终提交另补稳定matcher路径不打开View及已知format的bytes/op自动阈值。
前置分段提交：`378791c18`、`64415feec`、`64bf4f22a`、`f343883b8`。
测试全部固定 `.32`，没有本机测试、完整 gate、push 或子 agent。

## 实现

- rules 的 Matcher/Hit、Cache/Cached/MatcherInput、GroupCatalog/GroupSelection、
  Provider/RenderedMembers、IPDeps/IPTarget/IPResult、policy验证和泛型reorder使用native DTO。
  jsonx只留在明确的legacy签名边界，核心不再绕回这些边界。
- 删除rulesetfmt/legacy.go；其剩余生产调用方转RuleFormat。convert.RulesetFormat继续沿用
  batch-6的窄rule_library codec，没有重新解码整份Config。
- rulefetch的Content明确区分文本/bytes，SourceConfig、DownloadRequest、CacheOptions、
  Inspection/Problem/Change、ResolveSource及下载/缓存/内容处理采用native类型。
  删除被替换的JSON/bytes repr、Unicode异常仿真及已无调用的isValueError/safeExceptionDetails。
- refresh采用RefreshEnv/SourceOwners、SourceResult/SourceMetadata/NodeCache、health/traffic/
  receipt DTO、native仓储和task row builder。不再仿真Python history字符串/字典迭代。
  所有写owner在Detach后、外部callback返回后及实际提交前检查同一Runtime generation；
  重试保留batch-7的Clock timer和ctx等待，不以旧Sleep覆盖。
- 已接batch-7 discovery的native规则保存、校验、缓存读取、Matcher/Cache、服务目录与page DTO。
  CacheOptions.ReadPath在repo.View内校验当前声明，仅返回路径，不保留借用记录、不复制library。
  signature和lazy load都有拒绝调用StoredLibrary的负控。没有每个ruleset重新解码Config/library。
- 必要convert产品改动仅legacy.go四个callback：RenderMembers→RenderGroupDocument，
  FilterGroups→LowerProviderFilters，ValidateRules→ValidatePolicies，FilterNodes→Filter。
  不改其签名、生成算法或默认超时。额外convert测试/录制器只处理已批准的D2私有provider path。
- 外部subs/substore、deploy、API与repo共享JSON文件接口仍有窄adapter；没有复制owner业务。
  既有HTML parser/entity及requests异常分类依design/n7-n8.md §1属于N9/N10，本轮未重写，
  不冒称已经删除。website核心现为PageProbe/PageInfo/ServiceIdentity原生DTO。

## 独立参考与期望保护

原始archives不改；正常行继续原期望。R5/D2另录逐行、绑定原输入/输出的独立预期：

- 缓存名600条、namespace 7条、policy 114条；候选读取1500条、lower 600+88条。
- refresh原corpus1210条、recorded 3150条：另录31条R5、2条R8。实际调度/history/task逻辑
  由真实Python执行，Go测试不照实现计算答案。原history异常证据也单独保存。
- 私有provider path的49条Mihomo参考由真实Python generator生成；只拦截其摘要输入。
  录制器要求除精确path外整个文档/下载列表不变。当前39条正常生成/下载行直接对照；
  原有N7/R5错误分支继续完整比较错误，不应用路径改写。
- 新增6条真实Python旧缓存编码参考（显式空ID、转义字符串、UTF-8分隔符等）和1条外部anchor
  provider复制参考。未知成员和未变化值仍保留原始JSON证据。
- D2文件比较只放过已迁移owner JSON的成员顺序/空白；负控拒绝URL、ID、数字类型、缺失字段、
  非owner文件的变化。Flask缓存路径投影只改已知cache文件key及results[].local_path；
  未知namespace、额外文件、正文、错误、状态码、SQLite及其他字段均继续比较。
- pyref人工审核suggest-sources结果：3个mosdns集合删已删除的legacy文件，补7个实际可达helper，
  不扩大product白名单。三个gzip仅更新metadata；与go基线逐项比较2863+5份entries不变，
  Recorded原有0份仍为0；N73共享upstream参考文件不变。真实Pythonrecord/check再次通过。

## 已修风险

本次实现没有未修复的high/medium产品问题。以下均已修复并有回归（收口提交fcc671d9d；
共享支持55f13acb7）：

- **medium**：只换D2摘要会失去首次启动时的离线兜底。旧文件须在当前声明、内容及invalid
  receipt校验后迁移；目标file锁内只安装缺失文件，不覆盖较新文件，不提交DB/revision。
  保留旧mtime，不能因迁移延长freshness；旧文件留作回滚。readonly snapshot或迁移写失败
  仍返回通过校验的旧bytes，下次可写读取再迁移；真正的关机取消继续传播。
- **medium**：R5文本回退不能成为URL/时间戳ownership证明。新增Raw.MemberJSON，只读单个
  有效原始字段，不编码整个cache。数字URL、文本/日期token及可解析数字日期有负控。
- **medium**：callback结束后T0不能继续保存health/task receipt。真实Runtime覆盖
  entry/fetch/metadata/save × 请求断开/关机，断开仍完成已接管写入，关机停止后续提交；
  配置/revision不变，旧generation不能重新启动。
- **medium**：Marshal/Parse复制私有provider会拒绝指向文档外anchor的有效YAML。
  改用带预算的yamlx.Encode图复制，随后MaterializeAliases修复定义和唯一名称；不用Clone，
  不展开alias。真实Python对照、修改副本不影响原节点、alias共享、comments及Marshal/Parse
  闭环均验证；实际Lower入口拒绝手工40层放大链和自引用。
- **low**：暂存实现曾提前拒绝空URL注入回调、漏写count=0、丢失超大interval调度短路、
  错拆聚合下载错误/暴露Dialer详细错误。修产品代码，未将这些回归登记成R5。

O12的离线兼容和mtime说明已写behaviour-changes.md及progress/o14-switch.md。

## 分配约束与DNS交接

- 窄matcher输入：0 / 20000个无关nodes均188 allocs/op，约14.6KB/op。
- 已知format读取：空 / 1MiB无关library均409 allocs/op，约35.4KB/op；不可JSON编码的无关
  transient成员用例另行保留；bytes/op阈值负控也防止“分配次数不变但复制整块payload”的回归。
  错误format仍拒绝缓存。
- 字段原始证据读取：空 / 1MiB无关payload均16 allocs/op、1344 B/op。
- DNS可直接用：
  `ReadValidatedRuleCacheWithFormat(ctx, rule, config *jsonx.Object, format string) ([]byte,error)`；
  native为`ReadCacheWithFormat(ctx, model.RuleConfig, *SourceConfig, string)`。
  旧入口委托format=""。显式format跳过inference，stable路径不读library；旧名称回退仅为
  名称消歧懒投影ID/name，ownership始终由当前存储声明证明。batch-8先rules后dns。

## 定向验证

命令均在任务worktree运行，统一前缀：
`RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; …'`。
长命令在前台等待完成；没有使用完整gate。

1. `gofmt -l`覆盖全部修改Go文件/目录，无输出；`go vet ./...`通过。
   `go test -count=1 ./tools/pyref -run TestAuditRepository`最终0.600s通过。
2. 完整定向race：
   `go test -race -p 4 -count=1 -timeout=20m ./internal/rules ./internal/rulefetch ./internal/refresh ./internal/rulesetfmt ./internal/model ./internal/discovery ./internal/api/rules ./internal/api/rulelibrary ./internal/api/proxygroups ./internal/api/tasks ./internal/convert/mihomoenv ./internal/server`
   末行分别：39.474s /44.251s /184.220s /3.090s /13.822s /23.910s /88.576s /
   65.907s /7.919s /7.125s /1.015s /20.610s，全部ok。
3. 修复后的增量race：
   `go test -race -p 3 ./internal/rules ./internal/convert/mihomoenv ./internal/server`
   44.118s /1.020s /21.579s全部ok。新迁移API的repo/rulefetch子集race1.601s/4.664s；
   新Runtime/字段证据/已知format/路径负控增量model、refresh、rulefetch、refresh/difftest
   race1.015s/3.758s/1.587s/1.305s均ok。
4. 跨包参考：
   `go test -p 4 -count=1 -timeout=20m -run "Corpus|Reference|Golden" ./internal/convert ./internal/provider ./internal/subs ./internal/substore ./internal/health/... ./internal/mosdns ./internal/oxidns`
   provider21.917s、subs4.677s、substore0.092s、health2.370s、oxidns18.180s通过；
   D2/metadata收口后convert70.811s、mosdns4.889s通过。最后alias修复增量
   `go test -p 3 -count=1 ./internal/convert -run "TestMihomoCorpus|TestD2Provider"`27.665s通过。
5. 真实Pythonmetadata核对：
   `PYREF_MODE=record PYREF_ACCEPT=1 go test -count=1 ./internal/mosdns -run "TestCorpus$|TestRecordedPythonTests$|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython$"`
   随后同选择器`PYREF_MODE=check`，最后59.324s /57.854s均ok。原entries逐项不变检查通过。
6. 真实Flask：
   `CONFIGFLOW_DIFF=1 go test -p 2 -count=1 -timeout=15m ./internal/refresh/difftest ./internal/discovery/difftest ./internal/api/rules/difftest ./internal/api/rulelibrary/difftest`
   refresh5.306s、discovery11.301s；D2路径投影后的API增量rules67.041s、rulelibrary30.136s
   均ok。refresh的31个HTTP请求状态/正文一致，缓存正文和备份继续比较。
7. 每条均`go test -run "^$" -fuzz "^名称$" -fuzztime=60s -parallel=2 ./对应包`：

   | 目标 | 包 | 末行 | executions |
   |---|---|---|---:|
   | FuzzTypedGroupCandidates | internal/rules | PASS /61.009s | 764377 |
   | FuzzTypedRuleContent | internal/rulefetch | PASS /61.012s | 239917 |
   | FuzzMemberJSONEvidence | internal/model | PASS /61.006s | 2137917 |
   | FuzzSourceYAMLPolicy | internal/refresh | PASS /61.019s | 176532 |
   | FuzzRuleFormat | internal/rulesetfmt | PASS /61.006s | 2355675 |
   | FuzzProviderAliasCopy | internal/rules | PASS /61.013s | 508392 |

8. `PYTHONPATH=. $HOME/cf-test/.venv/bin/python -m unittest server.internal.rules.testdata.test_d2_http_cache`：
   2 tests /OK。独立参考录制器也全部只在`.32`执行。`git diff --check`无输出。

最终scope：类型迁移和必要callbacks完成，原archives保留，R5/D2/R8与O12均登记。
N9/N10及未迁移外部owner的接口收口按原计划继续；本报告不宣称整个N8或生产切换完成。

最后性能约束增量：稳定matcher cache与invalid receipt在访问repo.View之前处理，避免超大
配置因超出repository decoded-cache预算而被每个ruleset重复解码。注入read callback负控
要求稳定路径不打开View；旧名称回退读取失败仍fail-closed。discovery完整race19.533s通过。
已知format除了allocs/read，新增bytes/read自动阈值（无关1MiB payload增量不得超过64KiB），
防止“buffer次数相同但复制整个payload”。其race实测小/大437/438 allocs、56278/56197
bytes/read，3.594s通过；原普通benchmark409 allocs、约35.4KB的口径保持不变。
增量后全仓go vet与pyref审计通过（后者0.568s）。
