# N8-9 subs/substore 独立复核

复核者 Codex（非作者），作者 `694df9d0`；对象
`go-n8-9-subs@5bd721950`，分支 `go-review-n8-9-subs`。
遵循 AGENT-BRIEF、WORKFLOW 的 Leaner verification／第四常设项和 n7-n8 的 N8-9 约束。

结论：**0 high、2 medium，均已修复**。复核只在 `.32`、`GOMAXPROCS=4`、`-p 4`
跑定向验证；没有重跑作者整套、完整 gate、push 或子 agent。批次门禁交协调者。

## Medium M1 — 网络响应及解压层没有大小上限（O9-M2 的 substore 部分）

- `RequestsClient.Do` 对网络响应直接 `io.ReadAll`；`decodeContent` 对每一层
  gzip／zlib deflate／raw deflate 同样无限读取。恶意订阅或 Sub-Store 响应可持续分配，
  压缩体很小也不能阻止内存耗尽。响应头和超时不提供解压后的字节预算。
- 新增真实 loopback HTTP＋生产 RequestsClient 回归，identity、gzip、zlib deflate、
  raw deflate、gzip+deflate 五种输入各输出 64 MiB+1。修复前五项全部失败：
  `accepted body above 64 MiB: bytes=67108865 err=<nil>`；退出1，0.918s。
- 修复：统一读取至多 64 MiB+1，原始响应和每一层解压均检查；超限返回 nil response，
  `ContentDecodingError` 保留 `ErrResponseTooLarge` 错误链和集中中文消息。
  解压器均关闭；读取沿用 ctx，解压期间也能响应 Runtime shutdown。
- 正控接受恰好 64 MiB 的 gzip 正文；另一个两层 gzip 例在中间层放巨大头部、最终正文
  为空，证明不能只检查最后输出或 Content-Length。所有输入流式合成，没有提交巨大 fixture。
- 这是显式安全上限；正常响应、查询／headers／请求 JSON、代理、fallback 和生成算法不变。

## Medium M2 — 从预算合格的大图复制子树，可能静默把 alias 内容截成 null

- `ParseProxyNodes` 只验证 proxies 整个列表，随后逐项 `yamlx.Clone(item)`。
  `ApplyDNS` 验证整个 dns，却单独 Clone policy 和重建后的 fake-ip-filter。
  共享预算按普通节点／alias 操作比例计算，父图的普通前缀能让父图合格，却不能证明子图合格。
  Clone 对超限边缘用 null；因此这三条路径把损坏内容当成成功，而非明确拒绝。
- 新增合成 alias 图：200 个普通代理在前，最后一个代理带12层共享引用；另在 DNS 内放
  2000 个普通节点，分别隔离 policy／filter。fixture 自检证明父图通过、子图确实命中
  `ErrAliasBudget`。修复前代理返回201项／nil，两个 ApplyDNS 都返回nil错误，三个测试失败。
- 修复：在每个将被单独展开的子树上调用共享 `yamlx.Encode` 校验，再 Clone；
  直接返回原 `ErrAliasBudget`，不增加第二套预算、不截断或替换原输入。
  DNS 两种失败回归同时断言原文档的完整 JSON 不变。
- 其余 Clone 调用按实际展开单元审计：parser 每 item 的 ToJSON、ProxyNodes 每 proxy 的
  ToJSON、dialer 的 base／definition、链 provider／owned sequence、hosts 均已有同单元
  校验；ConvertTarget 的生产来源经 ParseProxyNodes。没有更改 yamlx 的全局政策。

## 原始证据、逐行期望与协调者裁定

- `git diff go...5bd721950 --diff-filter=M --name-only` 在 subs／substore／convert 的 testdata
  目录无输出；原 manifest、原 parser/filter/hosts/chains/dialers/DNS corpora、N7 reader
  archive 没有改。N89 仅新增独立参照。MosDNS 三个 metadata 更新另已由 rules 组复核，
  不在本次重录。原 manifest 仍 **4548项**，原来源摘要受测试检查。
- 阅读新参考生成器与消费点：真实 Python 业务产生结果，不从 Go 失败输出录期望。
  filter／hosts／DNS 以整档 SHA＋精确行 SHA 绑定；parser／dialers／chain additionally
  绑定相关 N7 loader SHA。R5 是消费视图，原始 params／未知字段保留；D2 是明确 canonical
  JSON 摘要变更。已有测试修改对应获批规则、原生异常或 API codec，未见通配豁免。
- 外部 Base64 由命名明确的 `decodeSubscriptionBase64` 预处理 ASCII 垃圾／padding，
  标准库执行实际解码；2500条原输入和原输出未改。复核不重跑作者全部2500条。
- 机场 group type 使用 select/url-test/fallback/load-balance 白名单，未知回退 select；
  stored group type 保持严格。抽检现有实际边界测试。
- runtime state 的唯一面板读方分别是 Go loadState／Python json.loads；检索运行时／安装
  路径没有外部按字节比较该 hashed scratch file 的消费者。原 Python 字节回归覆盖空格、
  Unicode转义、未知大整数、读取不重写；原成员／顺序和业务中文仍受约束。
- Agent 非空不可解析版本仅在 EasyTier 适用的 Agent 路径拒绝并给中文；空／null 仍 unknown。
  13条完整生成参照绑定原行、完整 err/report/profile；9条原字节manifest例外逐项绑定旧SHA，
  其余字节比较不变。抽检这13条和低层caller证据。
- 临时目录依次读 TMPDIR/TEMP/TMP，再 os.TempDir；没有写探测或静默回退。
  首次使用通过 Stat／Access 返回环境变量、路径和中文原因，Wire 启动告警；抽检原回归。

## enabled 回写、性能、恶意输入和关机

- `FilterSubscriptionItems` 把 native 选择按原序映射回原 jsonx 对象，重复引用也保留。
  聚合 adapter 先把原 nodes 放 CacheUpdate，再给选择出的原对象设置 enabled/subscription_id，
  因而真正持久化同批数据。检查其全部生产调用方、FilterNodes／FilterProxies 和其他 legacy
  adapter：native FilterNodes 是值记录选择，未被当成原对象回写接口；AdaptProxies 的新对象
  用于输出且改 dialer 时有意 COW；监听器／hosts／DNS 的 codec 显式复制回拥有字段。
  未发现另一处筛选复制导致缓存丢写。原身份与未知字段回归抽检通过。
- 配置解码位于 legacy 入口；native hosts／dialer／chain 直接接收 model.Config。
  既有 B/op 基准另加1MiB无关字段，阈值32KiB；完整生成也有 B/op 上限，能够发现
  分配次数不变但复制整份配置的回退。本次另做一个临时整份配置解码负控并恢复，结果见下。
- YAML 递归／40层alias放大走 yamlx 的共享拒绝；深度上限由 shared yaml.v3／JSON reader
  承担。正则通过共享 rules owner，普通表达式用 RE2，backtracking 引擎每次匹配1秒上限；
  没有复制私有正则实现或放宽超时。这是每次匹配预算，不声称存在整个订阅的总 CPU 预算。
- 按第四常设项逐层查 Detach、回调返回、拿锁后和提交前的 ctx.Err：Cache save/delete/prune
  → repo文件叶锁、runtime state暂存/rename、bundle下载/替换、VERSION写入和重启均保留来源
  Runtime。抽检真实 Start/BindRequest/Shutdown 的 cache/state/update/client/graph 回归；
  断开仍完成，shutdown 不落新写，不做关机后补偿写。没有新增 Uninterruptible 例外。
- convert 产品 diff 主要为 native callback／parser接线，另有明确裁定的 Agent版本守卫；
  本复核不改生成器算法。独立抽检冻结manifest里带订阅的 Mihomo300、Surge0、Loon1
  完整输出字节，不以结构比较放宽这三个样本。

## 定向命令与结果

全部前缀为 `RT_HOST=sunbroke@192.168.0.32 rt`，工作目录均为本任务 worktree；
远端 Go 命令均 `GOMAXPROCS=4`、`-p 4`。没有新增 fuzz target，没有重复作者60秒fuzz。

修复前（均退出1，实际产品失败，非编译失败）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 && go test -p 4 -count=1 -timeout=3m ./internal/substore -run "^TestReviewN89ResponseBodyLimit$"'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 && go test -p 4 -count=1 -timeout=3m ./internal/substore -run "^TestReviewN89ProxySubtreeBudgetNeverTruncates$"'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 && go test -p 4 -count=1 -timeout=3m ./internal/subs -run "^TestReviewN89DNSSubtreeBudgetNeverTruncates$"'
```

三组分别0.918s／0.025s／0.060s；失败原文见M1、M2。

首轮抽检使用下面实际命令。substore38.635s、subs1.453s通过；convert24.747s中，
原Agent／caller测试通过，新抽样的Surge／Loon因测试遗漏安装corpus环境回调失败。
补上与原冻结字节测试相同的SetMihomoEnv初始化后，按下面第二组只重跑相关包。
这不是产品缺陷，也未改期望。首轮全仓vet／格式已通过。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; export GOMAXPROCS=4; test -z "$(gofmt -l internal/subs/proxyserverdns.go internal/subs/review_n89_alias_test.go internal/substore/body_limit.go internal/substore/httpclient.go internal/substore/messages.go internal/substore/proxies.go internal/substore/review_n89_alias_test.go internal/substore/review_n89_body_limit_test.go internal/convert/review_n89_subs_test.go)"; go vet -p 4 ./...; go test -race -p 4 -count=1 -timeout=5m ./internal/substore ./internal/subs ./internal/convert ./internal/convert/mihomoenv ./internal/repo -run "^TestReviewN89|^TestN89(RuntimeReadsOriginalPythonBytes|RuntimeStateCommitLifetimeAndRawMembers|CacheWriteOwnerLifetime|UpdateCommitLifetime|UpdateRejectedAfterShutdown|ClientOwnerLifetime|TemporarySubscriptionRawRequests|FlightWaiterShutdownDoesNotWaitForOwner|RuntimeDirectoryExplainsEnvironmentAndDoesNotWrite|RuntimeTempRootDoesNotProbeOrFallback|LegacyFilterRetainsSourceIdentityForCacheUpdates|NativeFilterRetainsRawItems|AirportTypeCompatibilityAndNumericNames|DNSOwnsOnlyGeneratedSubtree|DNSMalformedFilterIsUntouched|CallerReferenceProvenance|AgentGenerationReferences|ProxyReaderIgnoresUnconsumedComplexKeys|AddGroupsRuntimeStopsBeforeGraphCommit)$|^TestProxyYAML|^TestSubscriptionYAMLEntrypoints|^TestTrackedUpdateIgnoresDisconnectAndCancelsHTTPOnShutdown$|^TestTheSameNodeIsConvertedOnceAndCallersGetTheirOwnCopy$"'
```

最后产品变更（解压读取传ctx）及测试初始化后的验证，退出0：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/substore/body_limit.go --back server/internal/substore/httpclient.go --back server/internal/convert/review_n89_subs_test.go -- bash -lc 'set -e; cd server; export GOMAXPROCS=4; gofmt -w internal/substore/body_limit.go internal/substore/httpclient.go internal/convert/review_n89_subs_test.go; go test -race -p 4 -count=1 -timeout=4m ./internal/substore -run "^TestReviewN89|^TestN89(ClientOwnerLifetime|TemporarySubscriptionRawRequests|UpdateCommitLifetime)$"; go test -race -p 4 -count=1 -timeout=3m -v ./internal/convert -run "^TestReviewN89SubscriptionGenerationSamples$|^TestN89CallerReferenceProvenance$|^TestN89AgentGenerationReferences$"; go test -race -p 4 -count=1 -timeout=3m ./internal/convert/mihomoenv ./internal/repo ./internal/rules ./internal/yamlx -run "^TestN89NativeEnvironmentPreservesShutdownAndCacheRecords$|^TestNativeSharedDelete|^TestReviewBacktrackingMatchTimesOut$|^TestNativeSyntaxDiagnostics$"; go vet -p 4 ./...; test -z "$(gofmt -l internal/subs/proxyserverdns.go internal/subs/review_n89_alias_test.go internal/substore/body_limit.go internal/substore/httpclient.go internal/substore/messages.go internal/substore/proxies.go internal/substore/review_n89_alias_test.go internal/substore/review_n89_body_limit_test.go internal/convert/review_n89_subs_test.go)"'
```

```text
ok configflow/internal/substore             31.254s
ok configflow/internal/convert              22.932s
  AgentGenerationReferences                 5.25s
  CallerReferenceProvenance                  4.05s
  SubscriptionGenerationSamples             12.48s (Mihomo/Surge/Loon全部PASS)
ok configflow/internal/convert/mihomoenv    2.106s
ok configflow/internal/rules                1.243s
repo/yamlx 在此正则下无匹配，随后用准确名称补跑如下。
```

分配负控：rt中的Python try/finally仅临时在AddGroups加入一次
`model.Marshal(config)`＋`json.Unmarshal(..., &model.Config{})`，执行
`go test -p 4 -count=1 -timeout=3m ./internal/subs -run "^TestN89DialerGenerationDoesNotDecodeConfigPerSource$"`。
真实退出1，报 **empty=675495、1MiB=35323481 B/op**；2.525s。finally逐字节恢复源码，
未提交注入器。恢复后的正控、ReportAllocs基准和余下叶锁／深度抽检均退出0，实际命令：

```sh
# 与负控相同的 rt 前缀，cd server，export GOMAXPROCS=4
go test -p 4 -count=1 -timeout=3m ./internal/subs -run "^TestN89(DialerGenerationDoesNotDecodeConfigPerSource|ChainsDoNotDecodeUnrelatedConfig|HostGenerationDoesNotDecodeUnrelatedConfig)$"
go test -p 4 -run "^$" -bench "^BenchmarkN89(DialerGeneration|ChainGeneration)$" -benchtime=1x -benchmem ./internal/subs
go test -race -p 4 -count=1 -timeout=3m ./internal/substore ./internal/repo ./internal/yamlx -run "^TestN89NativeProxyRecords$|^TestN89NativeSharedDeleteCommitLifetime$|^TestSyntaxDiagnosticsAreChinese$"
```

```text
ok configflow/internal/subs       7.558s  # 1MiB无关字段预算正控
dialer 8/32/128: 222968/881816/3531776 B/op, 2078/8069/31965 allocs/op
chain  8/32/128: 423088/1230824/4558600 B/op, 5200/15314/56181 allocs/op
PASS; ok configflow/internal/subs 0.068s  # 1x为分配观测，不设墙钟门禁
ok configflow/internal/substore   1.042s
ok configflow/internal/repo       1.434s
ok configflow/internal/yamlx      1.068s
```

没有未提交的产品残项。新增文件：body_limit.go22行、body测试112行、proxy alias测试39行、
DNS alias测试56行、完整生成抽样55行，外加本报告。原Python业务／原Python测试映射仍见
作者n8-9-subs交回报告；本次仅上述两类安全修复，没有其他未移植项或新增依赖。
提交号由最终交回消息给出，报告随修复一并提交。
