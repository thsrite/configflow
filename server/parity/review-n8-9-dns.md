# N8-9 DNS 组独立复核

复核者 Codex（非作者），作者 `0d31fb93`；复核对象
`go-n8-9-dns@12549f544`，工作分支 `go-review-n8-9-dns`。
按 AGENT-BRIEF、WORKFLOW 的 Leaner verification／第四常设项及 n7-n8
的 N8-9 约束复核，并参照 rules 组独立报告。

本分支未发现需要修改产品代码的独立缺陷；发现 **1 项 medium 的跨分支集成风险**，
协调者已裁定由 batch-8 叠批者关闭，详见 M1。不能把当前定向通过当作组合树已通过。
只在 `.32` 运行定向验证，`GOMAXPROCS=4`、`-p 4`；未重跑作者整套、完整 gate、
未 push、未启动子 agent。本次提交只增加复核报告。

## Medium M1 — 生成链丢失 Runtime，而组合后的缓存读取会写入迁移结果

- `oxidns/adapter.go:classicalWithoutIP` 调用元数据 resolver 时使用
  `context.Background()`；`ippresence.TypedClassicalIPLines` 再调用
  `rulefetch.ReadCacheWithFormat`。旧实现也使用 Background，本 DNS 分支的过渡
  reader 尚不迁移，因此本树不能重现关机后迁移写入。
- rules 分支的正式 `ReadCacheWithFormat` 在验证旧缓存后调用 `migrateCache`，
  由 repo transaction／文件替换写入新地址。直接组合会把这项写入挂到没有来源
  generation 的 Background 上，破坏第四常设项，也可能绕过调用者的只读快照。
  这是两条真实生产路径的组合风险，不将单独 cancel context 当作关机证据。
- **协调者裁定（2026-11-19）**：纯生成路径的规则元数据读取只读、从不触发迁移；
  旧缓存迁移仅在带真实 Runtime ctx 的刷新／请求等路径发生。正式 reader 在 rules
  分支，故由 batch-8 叠批者在组合树实现，不在本分支复制其业务。
- 建议入口：
  `rulefetch.PeekCacheWithFormat(ctx context.Context, rule model.RuleConfig, config *rulefetch.SourceConfig, format string) ([]byte, error)`。
  它复用相同选址、invalid receipt、内容验证和已知 format 快路径，但不迁移、不取写锁、
  不改 DB、文件、mtime 或 task receipt。`classicalWithoutIP` 经 typed presence hook
  改走该只读入口；`ReadCacheWithFormat` 的正常迁移合同保留。
- **批次必需回归**：真实 Runtime Start／BindRequest／Shutdown 后，经完整
  `Generate → classicalWithoutIP → ippresence` 读取可用的旧缓存，断言没有新缓存、
  暂存文件、DB/revision/receipt/mtime 变化；请求断开仍能读取；同一旧缓存经带来源 ctx
  的正常读取会迁移，而等待锁或提交前 Shutdown 不迁移。两个入口均应拒绝 invalid
  receipt、越界符号链接及错误格式；保留旧缓存离线可读的正控。

## CopyGraph、别名与所有权

- `CopyGraph` 直接复用 Edit 的 memoized `copyGraph`；先登记副本再遍历 Content／Alias，
  保留锚名、共享、环和节点元数据，不展开或截断。所有权复制的工作量随物理节点／边数增长。
  检查其新增调用点：OxiDNS clone／storedProjection／deliveryNode／dump，以及 MosDNS
  fallback upstream 副本，均为所有权隔离；没有拿它代替需要展开的 JSON 边界。
- `deliveryNode` 仅展开有效 merge 对，保留 alias 图；最终 yamlx 写边界仍校验预算。
  MosDNS 的 upstream alias 定义及 fallback 副本由 MaterializeAliases 重新安置／改名。
  Parse、Decode、ToJSON、Marshal 的校验未被 CopyGraph 绕过。
- 抽检物理图深度 8／64／256、共享双边、源不变、环与并发编辑；另外抽检两组公开
  读取入口的别名放大／递归拒绝、移走原定义仍保留 alias 内容。均通过。
- 阅读 FuzzCopyGraph：覆盖 Content／Alias 交叉边、共享和环，双向映射检查既不保留
  源指针也不合并无关节点。作者已跑60s，本次未重复该 fuzz，也未新增 fuzz target。

## 独立期望、冻结输入与 Python

- `git diff go...12549f544 --name-only -- '*jsonl.gz' '*baseline*'` 无输出。
  MosDNS／OxiDNS 的原冻结 archive 和 baseline 未动；新增原生期望与重新录制的 pyref
  transcript 分开。未修改比较器／冻结语料来适配本次复核。
- R5：MosDNS 的投影从原行 args 的副本出发，只取共享 model 已声明字段；OxiDNS 只投影
  指定入口的 rule_configs／rule_library 成员。真实 Python 计算业务结果，Go 生成结果
  不参与构造期望；testgolden 以精确 fn／args 输入绑定 transcript。YAML 输入投影使用
  共享 facade，仍比较完整输出，不把批准的 R5 当作任意差异豁免。
- 对全部 OxiDNS 冻结 archive 做独立指纹检查：请求期望 **99/99**、诊断期望
  **52/52**、能力期望 **2/2** 均能绑定原行的函数和精确 args（请求索引只在 normalize
  使用）；没有孤立索引。指纹是各 raw argument 的 compact JSON 加 NUL 后的 SHA256。
- D2：`review_c27.jsonl.gz` 的每条原行同时携带完整 input／旧 output／stage／旧新 key／
  必要的 delivered。catalog／read 仅替换该行唯一一个旧 key 并完整比较其余结果；
  select／rebuild 精确检查拒绝及 usages／警告；生成交付和输入不变分别断言。
  没有本次新增的通配 D2 分类器；这些逻辑与原冻结输入未被改动。
- 独立负控：仅将 native-request 索引的 enabled 消息临时改成
  `synthetic-negative-control`，真实 `TestNativeRequestExpectationControls` 立即失败：
  `registered native rejection missing`。恢复原字节后正控通过；没有提交临时 mutation runner。
- 真 Python check 抽检旧 enabled=0/1、数字路由表／网关、数组路由表、对象网关，
  **13 个精确输入**全部 equal。既断言与原 Python 输出的差异类别，也与独立手工投影后
  的真实 Python 完整生成结果比较；原存储字节不变，FieldIssue 路径和 type 精确匹配。

## HTTP、Unicode、性能及路由器

- OxiDNS 写请求先执行原类型验证，再 Normalize／保存；旧存储先做 owned projection，
  0/1 → bool、数字 token → 文本、非法容器停用 RouterOS 功能。路径告警只含字段路径和
  IssueType，不含密码、token 或原值。API 400 无写入抽检通过。
- MosDNS 读取 Generate.vue 的实际保存链：三个 Input 为 type=number／v-model.number，
  初始化 Number(...)，draft 经过 JSON snapshot 后直接放入请求；Switch 绑定 bool。
  Input.vue 透传 input 属性并用 v-model／useVModel 传值，未添加其他字符串转换。
  在 `.32` 用实际 Input.vue 加 Vue test-utils／jsdom 挂载相同的三个绑定，输入
  `128`／`60`／`20` 后经过相同 JSON draft，三个字段均为 number（非仅从模板推断）。
  严格带引号整数仍由共享 model.Int 接受。空输入等无法形成合法整数时由 API 拒绝。
- cache-settings 的整体 null 是200默认重置，字段 bool:null 是400；类型错误400、
  缺失引用409且带 usages 的独立 Flask 对照保留。字符串／对象 mixed custom_matches
  由共享 union 存储，字符串生成时忽略；原字符串的源码保真及 R1–R9 断言仍在。
- Unicode 的 **15 个真实 Python 精确输入** check equal；覆盖 ß、i/ı/İ、最终 sigma、
  NBSP 和 U+001C–U+001F。普通 Unicode 结果保持；四个控制字符的 TrimSpace 差异已有
  精确 Go／Python 期望和 behaviour-changes 登记，splitlines 的既有分隔语义保留。
- 冷缓存全链实测：unused=0 **63,993,080 bytes／755,408 allocs**；unused=2MiB
  **64,212,720 bytes／755,592 allocs**（race 定向 run）。每次清 memo、至少128次真实
  presence callback、真实离线验证缓存，非只测模板或 mock 空 reader。
- **分配负控**：只在 `ReadCacheWithFormat` 开头临时将 format 置空，恢复逐规则完整
  library 转码／格式推断；未改测试。真实 `TestMetadataChainAllocationBound` 失败：
  **689,850,496 bytes／8,240,472 allocs**，命中80MiB／900000阈值。
  恢复原文件后正控通过，能拦住作者修掉的这类回退。
- `routers/management_repo.go` 的 diff 只涉及 `saveWorkspacePolicies` 及 imports。
  ProfileFromJSON／model.Fields／yamlx 替换旧转码；显式保留独立 device_notes 写计划，
  避免失去旧 NewProfile 回调的名字副作用。management transaction、路由器业务、锁及
  写入顺序没有变；名字修改保留原 OxiDNS／MosDNS／扩展字段和重复保存 no-op 回归通过。

## 关机与验证记录

- 检查 MosDNS save、OxiDNS SaveSettings、configgen Build、两种 ZIP owner：Detach 后
  立即检查来源 ctx，在 repo callback、最终赋值、生成文件和下载／打包交付前继续检查。
  archive worker 使用来源 Spawner，提前失败后仍受 Runtime 跟踪，collaborator 在启动前捕获。
- 抽检真实 Runtime 的已关机、请求断开、事务锁等待、HTTP 下载中关机／断开和生成 owner；
  检查 DB／revision／文件或 archive 副作用。没有用单独 cancel 代替 Runtime 证明，
  没有让 fake 修改生产超时。M1 的组合树叶层写入是明确未关闭的例外。

共同命令前缀：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc '…'
```

第一组定向检查（退出0）：

```sh
cd server
PYREF_MODE=check go test -race -p 4 -count=1 -timeout=5m -v -run '^TestCopyGraph|^TestMetadataChainAllocationBound$|^TestStoredLegacySettingsAgainstPython$|^TestTypedSettings|^TestNativeRuleTextAgainstPython$|^TestR15WorkspacePolicy' ./internal/yamlx ./internal/oxidns ./internal/api/mosdns ./internal/mosdns ./internal/routers
```

```text
ok configflow/internal/yamlx       1.035s
ok configflow/internal/oxidns      5.439s
ok configflow/internal/api/mosdns  1.869s
ok configflow/internal/mosdns      1.436s
ok configflow/internal/routers     1.135s
PYREF equal: 13 exact stored-settings inputs; 15 exact Unicode inputs
```

两个负控在 rt 内由 Python finally 保存／恢复原文件，实际 Go 命令如下，均如期退出1：

```sh
go test -p 4 -count=1 -timeout=3m -run '^TestMetadataChainAllocationBound$' ./internal/oxidns
go test -p 4 -count=1 -timeout=3m -run '^TestNativeRequestExpectationControls$' ./internal/oxidns
```

索引审计使用 `.32` 的 `~/cf-test/.venv/bin/python` 读取全部冻结 gzip，未运行产品业务：
`native-request 99/99; native-validation 52/52; native-capability 2/2; FROZEN INPUT BINDINGS PASS`。

关机／错误边界等补充抽检（同一前缀，退出0）：

```sh
set -e
cd server
go test -race -p 4 -count=1 -timeout=5m -run '^TestC29RuntimeShutdownRejectsEveryOwner$|^TestC29RealRuntimeDisconnectCompletesEveryOwner$|^TestN73OxidnsReaderSafety$|^TestNativeRequestExpectationControls$|^TestNativeReaderIndexRejectsUnregisteredInput$|^TestO36cArchive|^TestN73YAMLReadGraphs$|^TestN73RemovedSectionsKeepSharedValues$|^TestGenerationOwnerRuntime/(mosdns|oxidns)/' ./internal/oxidns ./internal/mosdns ./internal/configgen
CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout=5m -run '^TestMosDNSNativeRequestDifferences/(post_rulesets_unknown|fractional_integer|boolean_integer|null_cache_boolean)$' ./internal/mosdns/difftest
go test -race -p 4 -count=1 -timeout=3m -run '^TestNativeRequestTypesReturn400WithoutWrites$|^TestTypedPresenceMatchesOfflineLegacy$|^TestR15WorkspacePolicyNamesRetainUnchangedFields$' ./internal/api/oxidns ./internal/ippresence ./internal/routers
```

```text
ok configflow/internal/oxidns          1.868s
ok configflow/internal/mosdns         14.718s
ok configflow/internal/configgen       1.790s
ok configflow/internal/mosdns/difftest 51.899s
ok configflow/internal/api/oxidns      1.395s
ok configflow/internal/ippresence       1.348s
ok configflow/internal/routers         1.123s
```

末组（同一前缀，退出0）：

```sh
set -e
cd server
PYREF_MODE=check go test -race -p 4 -count=1 -timeout=3m -run '^TestN73SharedUpstreamMetadataMatchesPython$|^TestR5ReferenceProjectionStaysNarrow$' ./internal/mosdns
go vet -p 4 ./...
test -z "$(gofmt -l internal/yamlx/copy.go internal/yamlx/copy_test.go internal/mosdns internal/oxidns internal/ippresence internal/api/mosdns internal/api/oxidns internal/configgen internal/routers/management_repo.go)"
```

MosDNS `ok 5.227s`，vet／gofmt 无输出。随后临时测试仅读取原 C27 行
433／434／437／438，调用真实 RuleDNS 入口并执行完整 c27AssertD2；
`go test -race -p 4 -count=1 -timeout=3m -v -run '^TestReviewN89D2Spot$' ./internal/oxidns`
四个阶段 catalog／select／read／rebuild 均通过，`ok 3.123s`。
临时测试由 finally 删除，未提交或修改原期望。

前端组件类型验证：`RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc '…'`
内使用已有 lockfile 对应的 `~/cf-test/node_modules/<hash>/node_modules`，运行
`node <cache>/vitest/vitest.mjs run --config <rt-root>/server/parity/review-types.config.mts`。
临时 config 只启用 Vue 插件、jsdom 和该一条测试，1 worker，未运行前端全量或 dev server。
测试挂载真实 Input.vue，分别 setValue 后 nextTick，再比较 JSON draft：

```text
ACTUAL INPUT JSON: {"cache_size":128,"cache_lazy_ttl":60,"cache_dump_interval":20}
Test Files 1 passed (1); Tests 1 passed (1); tests 42ms; Duration 897ms
```

临时 config／test／依赖链接均由 finally 清理，没有修改前端源文件。

批次全门禁及 M1 的实现／组合回归由协调者执行；本次提交号见交回消息。
