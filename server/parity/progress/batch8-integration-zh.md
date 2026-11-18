# batch-8 集成交接（进行中）

基底 go@d4fab6441，工作树 configflow-go-batch-8，分支 batch-8；不 push。
指定 a–g 七个集成项已分别提交；协调者补充的 profile-local 旧缓存认领和九条精确
C28 D2 预期已在 23c83a970 关闭，当前参考指纹与 preflight 已通过，继续完整 compare 验收。core/race 由协调者跑。

## 叠入范围

所有源分支保持原引用不变，临时 staging 分支逐一 rebase 到 batch-8，
再 fast-forward；没有 patch-equivalent 重复提交。Markdown 冲突两边保留。

| 分支项 | 源范围（不含左端） | 组合树提交范围 |
| --- | --- | --- |
| rules | d4fab6441..9989da440 | d4fab64412..9989da440d |
| dns | d4fab6441..2afc61ece | 9989da440d..958c21eb90 |
| mcp | 95440b69e..5f5435423 | 958c21eb90..110690a1b5 |
| artifacts | 95440b69e..faa3b0dcb | 110690a1b5..4d63104c94 |
| o9 | d4fab6441..300e51b41 | 4d63104c94..3a20bdd7aa |
| o9-medium | d4fab6441..1ab78956d | 3a20bdd7aa..1d54b09101 |
| o9-h2 | d4fab6441..db993863e | 1d54b09101..0a3a63815e |

## 冲突处理

- DNS cache.go：保留 rules 正式 SourceConfig / ReadCacheWithFormat 和迁移/receipt/路径校验，
  不退回 DNS 过渡 JSON adapter；a 提交再移除 IP-presence 的剩余 JSON 选址桥。
- 三份 MosDNS pyref 二进制冲突保留 DNS 已复核的 typed 参考输入/输出；在整合树
  重新做只读真实 Python check，指纹按复核后的实际依赖刷新，不合并孤立旧输入或重写期望。
- configgen/generate.go：保留普通 Build 直接加载 typed repository 的路径；冻结 Capture
  使用同一次捕获快照的 build，二者共用 materialize，不重新读取当前配置。MosDNS 只在
  冻结 legacy 边界转一次 typed config，OxiDNS 冻结分支经既有兼容 archive 边界。
  已定向通过 GenerationOwnerRuntime 与三个 ManualArtifact 测试。
- baseline：删除已接线 artifact 的 deferred 行，同时保持 batch-7 已删除的 R14/A18
  deferred 不复活；D2 IPv6/通知精确 baseline 原样保留。
- proddiff run.py / test_proddiff.py：H1/H2 逻辑与测试两边都保留，e 提交固定 bootstrap
  在 expected_headers 前处理；组合正控与错误 header/body 负控均通过。

## 已完成的集成提交

| 项 | 提交 | 内容 |
| --- | --- | --- |
| a | b7185f81c | IP-presence 直接接正式 typed cache path/reader，验证旧缓存迁移 |
| b | ef915df6e | PeekCacheWithFormat 纯读入口，生产 Wire 接只读 generation resolver，真实 Runtime/锁/receipt/symlink/mtime 回归 |
| c | a23b3c539 | MosDNS archive/ruleproxy 以 typed failures 判状态和下载错误元数据 |
| d | 5c8a6cf5e | 删除已被 H1/M1/H2 正式回归取代的两个 tagged 文件；带 o9repro 的正式回归通过 |
| e | cf1377319 | bootstrap_installer_baseline 先于 expected_headers；四条 Python 组合规则测试通过 |
| f | bf6e40db1 | 统一解压后 64 MiB 边界；promote gzip bomb 为正式回归，cache 保留旧有效内容 |
| g | 65d696738 | current/config/retired credential 均走固定尺寸恒时摘要比较 |

## 独立正负控

- b：真实 Runtime 关机后，经 production Wire 的完整 OxiDNS native Generate 链读取旧缓存，
  DB 文档、revision、commit count、receipt、全部非 SQLite 文件内容/模式/mtime 保持不变。
  请求 disconnect 仍可生成；普通带来源 ctx 的 reader 可迁移，停止/锁等待关机不迁移。
  临时仅恢复旧 Wire reader，两个 generation case 都因文件/mtime 写入失败；finally 恢复。
- f：192 MiB 解压数据的小 gzip 响应在 eager/stream raw body 均被实际 64 MiB 边界拒绝，
  cache refresh 不替换上一份有效文件；精确边界/重复 Close 覆盖。临时仅移除 wrapper，
  两个下载和 cache 回归立即失败；finally 恢复。Sub-Store 专属代码未改。

## 协调者追加裁定

正式 reader 必须允许当前已保存的 profile-local URL 声明认领旧缓存，只读与迁移分开授权。
只存在 captured args / 拟议编辑、已经删除或不同 ID 的同名来源不能认领；迁移在写锁内
再次检查当前 document。当前写 API 已禁止新增这种独立来源，回归以 native 事务种入
历史 SQLite row（store API），不放松写验证。原冻结 C28 #72/#78 当前数据库无所有者，按批准 D2
精确预期为拒绝/Timeout；七条文件名/迁移效果也必须精确绑定，原档案不改。

## 基准基线

固定主测试服务器、GOMAXPROCS=4、benchtime=3x、count=3：
- go@d4fab6441 Mihomo 128 节点+512规则：75,947,720–76,008,229 B/op，710397–710428 allocs/op。
- 原 DNS@2afc61ece OxiDNS 冷缓存链，128 rules：unused0 61,374,738–61,389,080 B/op、
  749512–749517 allocs/op；unused2MiB 61,316,677–61,381,077 B/op、749479–749531 allocs/op。
  原分支只读运行，没有修改其文件或引用。最终树需按同一命令复测。

## 最终验收待完成

profile-local 认领回归、C28 精确 D2 对照、参考指纹、生产快照认领数量、两个最终基准、
PREFLIGHT PASS、完整 GATE_ONLY=compare（proddiff new/stale 0、difftest 与 synthetic live 全过）。
所有重负载均在固定主测试服务器，不运行完整 core/race。

### 补充验证记录

- H：当前存储 profile-local / 旧 rule_sets 可 Peek 并在真实 Runtime ctx（请求已断开）迁移；
  captured-only、删除、拟议 URL、更换 ID 同名、未解析 library 全部拒绝。迁移锁内重验所有者；
  receipt 与越界 symlink 继续 fail closed。`rulefetch 2.566s`、`ippresence 0.369s`。
- 临时只恢复 shared-library-only 选址，两个存储正控立即因读不到旧字节失败，finally 恢复。
- C28 原始 archive SHA 和每条 raw line SHA 绑定 2 个精确 refusal / 7 个精确地址转移，
  独立参考执行真实 Python rule_cache_filename，再独立计算既定 D2 JSON 摘要。
  `TestArchiveMatchesRecordedPythonBuilds` 全部86次构建检查通过，`mosdns 9.264s`。
- 组合 source audit 发现 H2 model/registration、rules 拆分 adapter、CopyGraph、yamlbridge
  是真实新增传递 helper，已人工核对并纳入对应组 sources；移除 rulesetfmt/legacy.go
  这个已删除源路径，不改变产品边界豁免。`TestAuditRepository` 通过，`tools/pyref 0.845s`。
- 初次只读 PYREF check 全部业务对照 0 differ，仅源码/工具链/依赖指纹变化；刷新前保存了
  16 个 transcript 的完整非 metadata payload 哈希，共18489个 exact entries；刷新后全部
  非 metadata 字段逐值哈希相同（14份文件需更新指纹，2份原样）。最终 live check：MosDNS
  53.523s / OxiDNS 30.533s / routersplit 1.949s 均通过，metadata record 前一次分别
  56.673s / 33.144s / 1.890s。没有改 oracle 输入、输出或条数。

### 最终产品树分配与快照认领统计

产品树为 dead8fcdb（随后仅回放测试和文档更新，不改变产品）。同主测试服务器、GOMAXPROCS=4、
benchtime=3x、count=3，完整生产链基准：
- Mihomo 128 nodes / 512 rules：64,577,634–64,687,274 B/op，611016–611042 allocs/op，
  相比基底约76MB/710k下降，没有回升。
- OxiDNS cold metadata chain：unused0 42,814,437–42,849,157 B/op，470843–470874 allocs/op；
  unused2MiB 42,797,861–42,843,274 B/op，470824–470852 allocs/op。
  相比独立 DNS 分支约61.3MB/749.5k下降，2MiB未消费字段没有放大。
- 实际命令：`GOMAXPROCS=4 go test -run "^$" -bench "^BenchmarkMihomoGenerationProfile/128$" -benchmem -benchtime=3x -count=3 ./internal/convert`；
  `GOMAXPROCS=4 go test -run "^$" -bench "^BenchmarkOxiDNSMetadataChain$" -benchmem -benchtime=3x -count=3 ./internal/oxidns`。

对和 proddiff 相同的离线生产快照建立私有临时副本，在无网络命名空间调用当前正式
SplitRules / CacheReadPath / PeekCacheWithFormat，统计已启用 profile-local URL rulesets。
结果：候选0条，旧路径认领0条，验证可用旧缓存0条、涉及profile0个。当前快照没有这类
历史记录，功能正负控由独立合成 SQLite 存储回归证明；没有把生产数据做成 fixture。
源快照前后完整树摘要相同，副本、统计临时 Go 工具及 binary 全部清理；仅保留聚合数字。

### 解码链复核补充

额外发现仅在最终 Body 计数仍允许中间解压层产生超大 padding 后被后续 gzip reader
丢弃。f 提交已合并逐层64MiB限额；两个独立回归构造 tiny 有效 gzip member 加超过64MiB
padding，外层分别 gzip / deflate，最终正文只有4字节。仅临时去掉逐层计数而保留最终
Body 限额时，两例都失败；恢复逐层限额后通过。超限保持 typed size refusal 并关闭来源。
第一次 preflight 在 full non-race 阶段被主动停止，以便补齐该发现；没有把部分结果当通过。
将按用户命令在最终产品树重跑完整 preflight。

### 整合预检发现的旧参考接线修正

完整预检第一次返回 tests FAIL（两种 tag vet 均通过）：四份 pyref 仍带旧模块指纹，
N8-1 rule digest 尚未激活已批准 D2，convert 字节/录制回放没有接既有 D2 provider
路径参考，IP-presence 冻结 world 的旧缓存缺当前所有者。已按根因补齐测试接线：
- 四份额外 live Python 对照功能全部0差异；18份 transcript 的全部非 metadata
  payload SHA 逐一相同，只刷新指纹。
- byte gate 只逆向替换独立 Python 已绑定 provider path，其余每个输出字节仍受原摘要校验；
  recorded replay 9条路径经真实 Python lowerer 捕获 preimage，另2条数字0筛选 hook
  经共享 R5 输入投影调用真实 Python render_members，精确为 `(0)|(0)`。
- presence 独立参考调用真实 Python declaration/cache filename，确认旧缓存存在但当前
  library 没有相同ID及来源声明，绑定原 archive 和 raw line SHA：recorded7、corpus237、
  edge30。后5个 edge 输入是数字/bool引用，原Python解析也不能提供当前存储归属；
  与协调者已批准 captured-only D2 相同，结果精确为 unknown，其余输出/错误不豁免。
- 定向检查：convert 35.005s / httpx 0.004s / ippresence 1.818s，均通过。

剩余：最终完整 preflight 与 compare。

### 最终预检

最终树 868703c80，按用户指定：
`RT_HOST=<固定主测试服务器> rt sync` 后执行
`ssh <固定主测试服务器> 'bash /tmp/preflight.sh program__configflow-go-batch-8 -p 6'`。
两种 tag vet 与全部非 race 测试完成，退出0，最终行 **PREFLIGHT PASS**。
随后执行 `RT_HOST=<固定主测试服务器> rt -e GATE_ONLY=compare -- bash -lc 'server/scripts/gate.sh -p 6'`，
等待 proddiff、difftest、全部 synthetic live 完成。未执行完整 core 门禁；compare 内置 synthetic live 自带的定向 race 按原脚本执行。

### 完整 compare 首轮与 MosDNS HTTP 存储断言

proddiff 已返回 `baseline: new=0 stale=0`，全部 self-check PASS。全部 difftest 中仅
MosDNS HTTP 的缓存文件名仍按旧 Python digest 比较：55个响应相同，两份完整缓存内容
相同，四条路径缺失/新增构成两对既定 D2 地址。测试现在调用真实 Python filename，
再独立计算 JSON digest，仅在这两个确切来源上重命名期望 key，不修剪内容/放宽文件集。
`CONFIGFLOW_DIFF=1 go test -count=1 -timeout=5m -run "^TestDifferentialMosDNSRoutes$" ./internal/mosdns/difftest`
通过，`mosdns/difftest 4.241s`。首轮 synthetic 尚未运行，不算完整通过；将整段 compare 重跑。

### synthetic live 中的 repository D2 布局接线

第二次完整 compare：proddiff new=0/stale=0、全部24个 difftest package 通过；synthetic live
在3条 repo crosscheck 的最终目录布局发现旧 cache digest。没有数据库/操作结果差异。
对两份原操作文件的特定 create_rule_library_item（index47/189）调用真实 Python filename，
独立计算 JSON digest，并绑定完整 source SHA。只修改 Python 期望中那个具体布局 entry；
新增完整两端缓存 byte equality，以及旧/新交叉地址均不存在的断言。
继续定向运行这3条真实 Python race 回归，然后整段 compare 重跑，不能把部分通过当完整通过。

三条定向真实 Python repo race 回归完成：`repo 233.027s`，PASS。
为使交回验证覆盖最终测试树，将依次重新执行用户指定 preflight 与完整 compare。
