# N8-9 subs/substore

分支 `go-n8-9-subs`，初始基于 `go@03257978f`，现已 rebase 到合入 batch-7 的 `go@d4fab6441`。仅在主测试服务器通过 rt 定向验证，不 push、不起子 agent、不跑完整门禁。

## 范围与设计

- subs：URI/开放 YAML 解析、筛选、hosts、dialer 元数据、代理链、监听器、节点 DNS；固定配置消费 model.Config / model.Node，开放引擎图保留 yamlx.Node。
- substore：类型化协议/响应、缓存、runtime 状态、依赖更新及其仓储接线；旧入口仅在 legacy.go 转码。
- 解决 subs→convert 校验私有副本：由 convert 接线 typed collaborator，调用现有共享实现，不复制业务；必要 convert 文件单列在最终清单中。
- 配置只在请求/旧 API 边界投影一次；节点/订阅循环直接访问 typed 记录。新增分配量基准及回归约束。
- 共享 String/Int codec、严格身份字段、Name 数字文本兼容；不加私有标量规则，不改已有 corpus 迎合实现。
- 所有写 owner Detach 后、锁后、提交点前检查取消；真实 Runtime 关机/请求断开测试。已有叶锁、observation、原子替换及清理行为保持。

## 状态

产品迁移已经完成，当前进行最终定向验证。subs/substore 核心只用 model/DTO/yamlx；
旧入口收在 legacy*.go 且只转码。私有 compat/pyconv/pystr/yaml_reader 已删除，
convert 的订阅协作者直接调用 typed owner。下面各“阶段/剩余”描述是相应提交时的历史记录，
最终检查结果与交回清单以文末为准。

固定 RT_HOST=sunbroke@192.168.0.32，GOMAXPROCS=4，-p 4。不跑完整门禁。

### 缓存阶段验证

全部固定主测试服务器、GOMAXPROCS=4、-p 4：

- `PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/substore/testdata/n89_cache_reference.py`
  → `native cache reference: 1 exact projected Python case`。
- `cd server && go test -race -p 4 -count=1 -timeout=5m ./internal/substore -run "TestN89|TestPruneInvalid|TestLoadSubscriptionCacheLegacyAndBroken|TestSavedCacheBytes|TestStartupPrunes|TestDeletingA|TestPruningDeletes|TestHostsChange|TestNodeCount"`
  → `ok configflow/internal/substore 7.142s`。
- 同一远端副本 finally 恢复精确 fixture 的负控：只在 python.removed 加一个合成文件名，
  实际 `TestN89NumericCacheIDReference` 必须报 `native cache reference mismatch`；已通过此负控。
- `go test -p 4 -run "^$" -bench "^BenchmarkN89CacheRecord$" -benchtime=1x -benchmem ./internal/substore`
  → 32/256/2048 节点分别 787768/6007016/47830936 B/op、10090/78970/631010 allocs/op；
  节点量每增8倍，分配约增8倍，没有每节点重解整个缓存的平方增长；32→128 的分配上限回归通过。
  完整配置协作者的公开路径基准待后续 subs 迁移完成后补，不冒充已经验证。
- `go test -race -p 4 -count=1 -timeout=5m ./internal/repo -run "^TestN89NativeSharedDeleteCommitLifetime$"`
  → `ok configflow/internal/repo 1.529s`。

本阶段未跑全量 corpus/HTTP/fuzz，留给全部迁移后的最终定向验证。

缓存阶段受影响三包 `go vet -p 4 ./internal/substore ./internal/subs ./internal/repo` 和本阶段 gofmt 检查退出0、无输出。


## runtime 阶段（进行中）

- 状态读取/原子写入/Snapshot/saveMode/GetMode 改为 typed；工作/控制/跨副本叶锁原结构不变。
  GetMode 只在 repo.View 中读取 mode，不创建整个 system 的旧 JSON 视图。
- 原无ctx私有 read/write 和旧 Status 留在 legacy.go，仅转码旧测试/调用方；dependencies 核心
  仍调用 Status，下一阶段改 Snapshot。旧错误工具/路径和时钟工具仍待客户端/dependencies一起清理。
- 私有状态文件 framing 和 CPython内部错误按协调者同类裁定处理；正常字段/值/顺序/
  supervisorctl 请求与效果不放宽。已核查部署读取方，旧 Python 原样字节读测试通过，详见 ledger。
- Python引用：`PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/substore/testdata/n89_runtime_reference.py`
  → `native runtime reference: 128 exact real-Python cases`；原 review_reference.json/生成器/比较器不改。
- 定向 race（主服务器，GOMAXPROCS=4，-p4）：
  `go test -race -p 4 -count=1 -timeout=5m ./internal/substore -run "TestReviewRuntimeMalformedState|TestN89Runtime|TestStateFileBytes|TestPolling|TestConcurrentConversions|TestUpdateWaits|TestStartFailure|TestStartTimesOut"`
  → `ok configflow/internal/substore 9.556s`。
  包含写完/关闭真实临时文件后 Runtime 关机/断开，主文件不变/正常提交、未知成员原字节保留、无临时文件残留。
- 实际 reference 负控：仅改变128条中row1的返回 mode，原测试必须失败，已通过；finally恢复文件。
- 同一公开 BenchmarkN89RuntimeMode，0/64KiB/1MiB 无关设置：
  - 迁移前：6400/6400/5920 B/op，121/121/120 allocs/op。
  - 迁移后：5320/5800/5800 B/op，115/116/116 allocs/op。
  模式读取没有随无关配置增大；缓存后测试回归预算为大配置不能多出64KiB/op。
  这不代替后续完整订阅/代理链生成路径的配置分配基准。


## 客户端阶段

- 新增 Fetch/FetchResult、ConvertProxy/ConvertTarget（实现共享 convert.NodeConverter）、
  ParseProxyNodes/ProxyNodes；native core 不再导入 jsonx 或 any。旧下载/转换/批次入口
  仅留 legacy.go 转码；临时 YAML→jsonx 中转文件已删除。wire 新增 NativeConverter。
- 默认 fake 的有效业务/请求/日志断言不改。共享结果指针身份测试改为直接调用 Fetch；
  fake callback 只换 typed 类型，原请求次数、版本LRU、深拷贝、流量和文件期待不改。
- 节点零值输出保留完整 name/type/server/port/params，开放参数保留 tag；严格 type、共享
  数字文本和带引号整数回归通过。完整 proxies 子树先做 yamlx 预算检查，避免 alias 解开后
  克隆静默变 null。原三条 alias 放大/子树预算测试保持。
- 精确 native_client fixture 绑定原 corpus/review SHA，只列批准的9处边界；原 corpus、
  N7 oracle、比较器不改。固定中文业务前缀保留。详见 ledger。
- 主服务器 `GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=5m ./internal/substore -run
  "TestN89NativeProxyRecords|TestN89ClientOwnerLifetime|TestN89TemporarySubscriptionRawRequests|TestN89FlightWaiter|TestPythonCorpus|TestReviewParseUserinfo|TestProxyYAML|TestConversionFailure"`
  → `ok configflow/internal/substore 2.890s`；同次定向 vet 退出0。
- 另一次完整客户端正常复用/退避/版本转换/日志/alias 定向 race → 9.392s。
  env/http 响应与 dependencies 的旧动态消费、subs 核心仍待迁移；不能据此交回完成。


## 环境、HTTP 与 dependencies 阶段

- release/asset/UpdateState/DependencyStatus、Latest/Snapshot/List/StartUpdate 均 typed。
  内部只用原生入口；legacy.go 旧接口仅转码。全包核心 jsonx 仅残留 legacy.go，泛型缓存的
  `V any` 是类型参数，不是动态业务参数。删除319行 pystr.go；旧语料的错误标签仅在
  legacy_errors_test.go 中作为测试数据，不参与产品路径。
- repo.View 分别取 proxy 与 github 设置，不构造旧 system/config JSON。JSONData 只用原生
  encoding/json，不调用 DecodeStored（远端 HTTP 不是持久化资料）。默认 HTTP UA/有效
  headers/timeout 保持现有已验证协议，内部类别保留 native RequestError。
- 真实更新测试使用生产 job lock、Maintenance 叶锁、Runtime.Spawn/BindRequest/Shutdown。
  三提交点（下载文件写、bundle replace、VERSION write）× 关机/断开均通过；入口关机
  不改 state、不建目录。关机无重启/补偿、tmp 清理、backup 保留、最终 L1 可重取。
- 主服务器完整 substore race 48.605s 只发现2组旧边界断言（U+001C端口、review畸形节点）；
  不把 FAIL 写成全包通过。修成逐项精确 native 期待后的定向 race：
  `go test -race -p 4 -count=1 -timeout=5m ./internal/substore -run
  "TestBuiltinPortForms|TestN89BuiltinPort|TestReviewParseProxiesFromYAML|TestUpdateRestoresPreviousBundleWhenRestartRaises|TestReviewUpdatePanic"`
  → `ok configflow/internal/substore 3.935s`。三包定向 vet 同次退出0。
  最终整体阶段还会重新跑受影响包，不能据此声称任务完成。
- 版本及环境 native fixture 继续绑定原 archive SHA，逐项扩展；不修改原 corpus/N7 oracle
  或任何比较器。4301位分量明确走 native 文本验证，不放开其他语料。
- 此时 substore 已迁移，subs（parser/filter/hosts/dialers/chains/listeners/proxy DNS）尚未迁移；
  下一阶段清除 subs 的私有 convert copy，保留业务功能和链路正常输出。


## batch-7 基线与 subs 首批

- 已在干净节点 rebase 到 go@d4fab6441。四条 substore 提交改为
  8164958d7 / 43be7e5ec / 7102e685c / 6b13526e8；仅 ledger 追加位置冲突，双方内容保留。
  已读取8b/8c实际 typed collaborator API；将直接接入，不复制生成器实现。
- listener 核心已 typed，旧接口只转码，原注释/alias保护不改；filter 使用 model 和 yamlx，
  数字文本/R5消费按已定规则。9条 malformed 改变由真实 Python消费投影得出精确选择，
  原始记录及字段不重写。schema 与每条 input SHA 都有校验。
- Base64协调者明确不批准strict。已按裁定用清洗+encoding/base64改写，保留完整接受范围；
  原2500 helper+3714实际URI的6235条 parser corpus 未改且通过。此时 parser 其余动态
  字段和 pyconv 的数字/IP等还未迁移，不能声称 subs 已完成。
- 主服务器 GOMAXPROCS=4、-p4：parser+listener正常/安全编辑/别名的定向 race 2.431s；
  filter+新R5原始记录保留回归定向 race 1.169s。下一阶段清理剩余 parser、hosts、dialers、
  chains、proxy DNS 及 compat 私有副本，之后再做跨包/HTTP/fuzz/完整配置分配基准。


## typed parser 及8c接线阶段

- ParseList/ParseObject/ParsedProxy、协议DTO与共享codec已接入。legacy旧解析入口只转码；
  pyconv 中剩余 getOr/isString 等仍供未迁移 hosts/chains/dialers/DNS 使用，待一起删除。
- 必要 convert 改动只有新增 subscription_nodes.go 的 typed 接线和 legacyClientCallbacks
  的 ParseNodes 回调替换；没有改 Surge/Loon/Mihomo 生成业务。后续 chain/hosts等也用现有
  typed environment 回调接线，避免复制业务或每node重新转整份配置。
- 自检修复两条实际回归：VMess字符串重建丢server类型证据、开放UUID/NULL被强制规范化；
  以及先前substore.ProxyNodes重建丢相同证据。新增 subs provenance 和 convert owner/error
  测试在修复前真实失败（输出空server却nil error），修复后通过。8c原 `skip-failing-node`
  reference曾失败，修复后原期待/索引恢复，无改动。
- 新parser reference在真实Python既有业务上注入消费规则，复用原N7 loader值；795条精确
  新边界（含21条私有整数helper换成真实URI整数协议）都有source/row SHA和field/class。
  原6235条parser corpus、C10 reference及比较器不改；实际更改parser/17完整期待为[]的
  负控被拒绝（该负控在本阶段早期987条临时参考上执行，最终795条会再核对一次）。
- 最新主服务器 GOMAXPROCS=4、-p4：
  `go test -race -p 4 -count=1 -timeout=5m ./internal/subs ./internal/substore ./internal/convert -run
  "TestN89|TestParserCorpus|TestReviewC10References|TestTypedClient|TestClientReviewBoundaryReferences|TestReviewParseProxiesFromYAML"`
  → subs5.420s / substore17.882s / convert4.731s，全绿。
- 另一次误带整份SurgeLoonCorpus的race在8分钟超时；当时也捕获上述类型证据回归。它不是
  PASS，不拿它作为验收。后续固定只跑明确typed边界race，最终按要求跑跨包非race语料。
- 本阶段未完成全部subs；剩余 hosts/dialers/chains/proxy DNS/私有compat副本及完整配置
  分配基准、跨包语料/相关HTTP/fuzz。没有提前交回“完成”。

- 最终795条参考的实际负控再次完成：把 parser/22 的完整非空输出改成[]，实际
  TestParserCorpus在同一row失败，finally恢复原始gzip字节。该条不是沿用旧987条的证据。
- 对解析error传播的小整理之后，subs/convert相关定向race再次通过3.712s/4.130s；
  同次三包vet退出0。


## DNS/hosts 基础（进行中）

- 新 addresses.go 使用 netip 与 x/text，删除原手写 IPv6压缩/IPv4-mapped formatter。
  原pyIPAddress暂为未迁移调用方的薄视图，解析/文本/属性均调用native owner；映射地址的
  可用性判断使用Unmap，zone仅保留在地址文本、不影响地址属性。
- 新 HostResolver 对只读YAML图构建一次索引，exact为O(1)、fallback只扫描wildcard键；
  用于后续ApplyDNS复用，避免每个映射域名重建整份hosts映射。当前尚未接线，不提前声称
  已解决该生成路径的分配问题。
- 主服务器旧HostsCorpus3186条、DNSCorpus4769条、映射地址专例及新resolver的定向race
  → `ok configflow/internal/subs 3.347s`；旧期待未改。

## DNS typed 阶段

- ApplyDNS / DNSReport / DNSChoice / DNSSkip / CoreVersion / DNSListener 原生入口；只投影五个
  固定标量字段，开放 hosts/policy/filter 直接在 yamlx 上计算。HostResolver 每生成一次索引，
  修复迁移中 null 链端点被误当无目标的问题，原4769条 DNS 语料现通过（仅两条已批准的
  ASCII 数字边界用逐行 SHA 独立期待）。没有改原 archive 或比较函数。
- 通过 shared alias 的 DNS/filter 写入改成只克隆拥有子树，其他 alias 不变且最终 YAML 可重读。
  malformed rule filter false/0/空串/映射保持原文、返回固定业务警告，未知不等于空集合。
- CoreAgent / CoreStatus / CoreReport 和 typed status loader 取代动态状态消费，旧调用只在
  legacy.go 转码。convert 仅调整 legacy environment 的 NodeDNS 接线，未改生成器。
- 主测试服务器，GOMAXPROCS=4、-p4：
  `go test -race -p 4 -count=1 -timeout 5m ./internal/subs ./internal/convert -run
  "TestN89DNS|TestN89NativeHostResolver|TestN89HostChain|TestProxyServerDNSCorpus|TestReviewC10References|TestTypedClient"`
  → subs 3.695s、convert 1.086s；随后 subs/substore/convert vet 通过。
- BenchmarkN89DNSHostIndex，32/256/2048 个域名：69368/281368/2328064 B/op，
  308/902/6340 allocs/op。输入增8倍，分配近线性增长；64→256 的分配回归上限为6倍，
  不允许重新出现每个映射重建整个 hosts 索引的平方增长。完整生成管线基准仍待后续阶段。
- 剩余：hosts/dialers/chains/compat 核心及私有 pyconv 清除、完整管线基准和最终跨包/HTTP/fuzz。

## hosts typed 阶段

- ExtractHosts / HostDigest / CompareHosts / SourceMetadata / SubscriptionCache / HostIndex /
  MergeHosts / HostReport / LogHostReport 已成为唯一原生业务入口。原旧入口在 legacy.go 转码，
  不再有 ExtractSubscriptionHosts/HostsDigest/DescribeChange 的旧算法。hosts.go 核心不引用
  jsonx/any/私有数字解码；原生合并不序列化配置，不通过节点循环调用旧 Config adapter。
- 只读 cache DTO 对坏 node sibling 单独跳过，保留有效兄弟；entries 缺失/null/错型不伪造
  空索引。加入这些真实消费回归、共享 alias 不外溢、缺 owner 失败不改图、regex全排除仍有
  正确 ignored preview 的回归。三种持久化拥有者的未知字段由原 store/model 继续保留。
- 生成的元数据先用普通 DTO 写齐必需0字段，再进入 model；不把 R3 的 omitted zero 当作
  对外协议。正常四字段、空 domain、条目顺序及原 cache 侧效果保持。
- 88条独立边界：19个 CPython 私有 splitlines 输入由真实Python业务+原生区块选择生成完整
  期待，69个 D2 digest 用标准紧凑UTF-8 canonical JSON；所有 source/row SHA 验证。冻结
  archive、N7原生资料和比较函数不改。共享 httpx 的一条身份断言明确更新，旧hash保留注释。
- 主测试服务器 GOMAXPROCS=4、-p4：
  `go test -race -p 4 -count=1 -timeout 5m ./internal/subs ./internal/substore ./internal/httpx -run
  "TestN89Host|TestN89TypedHost|TestHostsCorpus|TestReviewC10References|TestExtract|TestHostsDigest|TestMerges|TestN8Persisted|TestHostsChange"`
  → subs21.738s / substore1.598s / httpx1.041s；随后四包 subs/substore/convert/httpx vet通过。
- BenchmarkN89HostGeneration（配置另带1MiB无关数据），32/256/1024节点：
  66304/526016/1957720 B/op，348/2188/8399 allocs/op；分配随节点量近线性。
  TestN89HostGenerationDoesNotDecodeUnrelatedConfig 测试64节点下0与1MiB无关配置的实际B/op，
  增量不得超过32KiB，每次生成 callback必须恰好1次。均通过；这是 hosts 管线，完整Mihomo
  管线在代理链/dialer迁移后再测，不提前声称整体完成。
- convert 只换 owned MergeHosts/LogHosts callbacks 为 typed，生成器代码未动。最终将统一从
  legacy.go 移出这些已原生的 collaborator 接线。legacy_pending.go 的 listField/objectsByID
  仍是尚未迁移的 dialers/chains/compat 所用临时旧逻辑，必须在下一阶段删除。
- 接入 native hosts/DNS 后，主服务器非race Mihomo原始语料+原输入不变门：
  `go test -p 4 -count=1 -timeout 8m ./internal/convert -run
  "^(TestMihomoCorpus|TestNativeMihomoOriginalInputsUnchanged)$"`
  → `ok configflow/internal/convert 32.770s`。2353正常生成仍走原exact-count字节门。

## dialer 元数据及交付阶段（尚未全部完成）

- ExtractDialers / AdaptProxies 使用 model 与 YAML图；旧两个入口只转码。外部机场 type 的
  白名单/default-select 按协调者裁定有意保留，函数 airportGroupType 注释写明原因，不扩大
  到用户存储的 ProxyGroup.Type/Node.Type；真实生产共享校验器的 strict 回归同时通过。
- 11条既有 malformed-array 边界用真实 Python 业务+N7独立loader值精确投影，source/row/
  loader SHA绑定，类型/错误消息逐项核对。正常四种组类型、开放 binary/timestamp标量字段、
  原 metadata 字段/值/顺序与交付副作用保持。原 corpus 和比较函数不改。
- 修复重复子组 DAG 展开复杂度：每个 target 每组只访问一次，首次发现顺序、去重及原上限
  不变。新30层重复 DAG 回归不会进行原先的指数叶展开。
- 已建立 SharedDialers typed协作者与 convert 唯一实现接线，外部 test package 导入实际
  注册。尚未将所有链降级/组生成迁到这些接口；compat.go/legacy_pending.go/pyconv.go 的
  旧私有副本仍要随下一阶段删除，当前不能交回完成。
- 在主服务器，GOMAXPROCS=4、-p4：
  `go test -race -p 4 -count=1 -timeout 5m ./internal/subs ./internal/convert -run
  "TestN89Dialer|TestN89Airport|TestN89Adapt|TestDialersCorpus|TestReviewC10References|TestN89DNS|TestN89HostMerge"`
  → subs7.518s / convert1.043s（convert该pattern无测试）；subs/substore/convert vet同次退出0。
- 克隆前验证改用 yamlx.Encode（只验证当前拥有子树），保留开放 YAML 合法非字符串键；
  未修改共享 yamlx 产品代码。新增交付开放键/原节点不变、原生错误精确消息、实际共享
  identity校验及 bounded DAG 回归。剩余：AddDialerGroups、LowerProxyChains、LowerChainProviders、
  删除私有copy与 Python模拟helper、最终完整管线分配基准及跨包Corpus/HTTP/fuzz。

## typed 中转组生成阶段

- AddGroups / DialerInputs / DialerReport / SourceDialers 已替换旧 AddDialerGroups 与 DialerRecord
  算法，旧入口只转码各自记录。dialers.go 不再引用 jsonx/any/Python iteration；删除已无用的
  iterate/isScalarSetting/notIterable/nodeDialer 和私有 re.escape，使用 regexp.QuoteMeta 加
  显式 portable punctuation 的既有输出规则。旧 chains 对转义函数的单次调用改名，没有改
  convert 生成器。Native env 的 DialerGroups callback 不再编码整个配置或文档。
- 原 dialer corpus 全通过；Mihomo原始语料+输入不变/2353字节门非race通过29.145s。
  新回归固定显式null/坏record不得fallback、每源恰好1次cache读取、config不变、冲突保留已
  生成前缀、无确认成员不能伪造空组。真实Runtime关机发生在cache返回之前，零转换且图不变。
- 主服务器，GOMAXPROCS=4、-p4：
  `go test -race -p 4 -count=1 -timeout 5m ./internal/subs ./internal/convert ./internal/substore -run
  "TestN89AddGroups|TestN89SourceDialer|TestN89DialerGeneration|TestDialersCorpus|TestN89DialerNative|TestSubscription|TestN89Airport"`
  → subs13.110s / convert1.047s（该pattern无测试）/ substore1.285s；三包vet退出0。
- BenchmarkN89DialerGeneration，8/32/128源、配置另有1MiB无关字段：254344/920632/3647664
  B/op，2098/8173/32367 allocs/op，输入四倍时分配近四倍。16源的0/1MiB无关配置增量
  不得超过32KiB的实际B/op回归通过，不允许每个源重新编码完整配置。
- 剩余核心仅链降级/链provider和compat/pyconv/yaml_reader旧模拟删除；随后统一原生
  collaborator接线文件、完整Mihomo管线分配基准、最终跨包Corpus|Reference|Golden、HTTP、fuzz。

## typed 链降级与私有模拟清除阶段

- LowerChains/ChainProviders 已成为原生 owner；配置只在旧 API 边界投影一次，节点/源循环内
  无完整配置转码。删除 chains.go/compat.go/pyconv.go/yaml_reader.go/legacy_pending.go，旧入口
  与历史测试 helper 分离。新增核心 AST 断言：subs/substore 非 legacy 产品不引 jsonx、不用
  动态 any（omap 的泛型约束除外），不保留两套 dialer 校验。shared owner 缺失明确报错。
- 656条已有输入的精确 Python+R5 reference，source/row/N7 SHA 绑定；19类边界逐项 ledger。
  所有冻结 archive 与比较函数未改。1800链语料、原共享convert漂移及repo golden同次通过。
- 新模型成员入口 ProxyGroup.SetJSON 复用共享 codec，保证显式null/空值、成员追加顺序和COW。
  链克隆只投影单个拥有的group，保留原私有成员位置；节点/组/源 ID 和生成字节不变。新增
  COW/失败成功前缀/源provider不变/坏alias key不panic及核心边界回归。
- 原生订阅协作者已移到 convert/subscription_environment.go；legacy.go 仍保留其他 owner
  （rules/check/provider）薄桥，不在本任务复制它们。ValidateListeners 直接使用原生入口。
- 主服务器 `GOMAXPROCS=4 go test -p 4 -count=1 -timeout=6m ./internal/subs ./internal/model
  ./internal/convert -run "TestN89Core|TestN89HostsRejects|TestN89LowerChains|TestN89Chains|TestN89ChainProvider|TestProxyGroupSetJSON|TestNativeMihomoMissingCollaborators|TestChainsCorpus|TestCompatDoesNotDriftFromConvertCorpus|TestCompatAgainstRepoGolden"`
  → subs10.132s/model0.003s/convert0.010s。
- Mihomo正常2353字节门、原输入不变与完整管线无关配置分配回归同次非race通过21.970s。
  BenchmarkN89ChainGeneration 8/32/128节点：435840/1231176/4559128 B/op，
  5245/15316/56151 allocs/op。BenchmarkN89MihomoSubscriptionPipeline 32/128节点、512规则、
  1MiB无关字段：34418752/80579208 B/op，370726/861075 allocs/op；四倍节点量约2.34倍分配。
  两个真实B/op回归分别约束链owner/完整typed生成16节点时，无关配置从0→1MiB的分配增量
  ≤32KiB/（基线2%+64KiB），均通过。完整管线含buffer pool，在race下有分配采样噪声；
  该预算仍小于单次1MiB配置解码，更远低于16节点逐节点解码的16MiB。未声称其他未迁移rules/provider薄桥已被删除。
- 剩余：最终受影响包/直接调用方race，跨包Corpus|Reference|Golden、相关HTTP difftest、60s fuzz及vet。

### 最终验证记录（进行中）

- 第一轮 `-race`：subs17.892s、substore44.979s、model12.912s 通过；convert误带全套生成
  字节测试，295.920s时由作者停止，不记通过。该轮暴露新分配断言的固定64KiB预算对race
  buffer-pool噪声过紧（35.86→36.12MB/op），已改为基线2%+64KiB，仍小于一次1MiB配置
  解码，远小于逐节点16MiB。算法/正常字节/原语料期待没有改变。后续convert race跳过整套
  Corpus/Reference/Golden/RecordedPython/GenerationBytes，这些由非race跨包/字节门覆盖。
