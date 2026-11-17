# N7-3d 独立复核

复核者未参与作者实现；起点 `go@a08c4cc27`，范围 `f90a156e..a08c4cc2`。
分支 `go-review-n7-3d`。仅用 rt 测试；新增用例为合成数据；无生产访问、push 或子 agent。
状态：实现复核完成，6项发现全部修复；high0 / medium3 / low3，未决0。最终完整 gate 正在 .64 前台排队，结果待补；不计通过。

## 发现

- **F1 / medium：provider 和 gateway 返回错误丢失原始 reader 原因链。**
  providerSourceError 把原始错误仅格式化为字符串，providerContext 再次重建 Error；
  gateway.BaseDocument/yamlPayloadStrings 直接替换成固定错误。七种合成拒绝
  （syntax、D1、tag shape、merge、explicit scalar、递归、depth40 alias bomb）
  全部拒绝，但 `errors.Is` 无法辨别原始原因。修复前两个新增回归共14个场景均失败。
  修复保留原因链，仍能匹配原验证错误类；中文展示文本不变，完整原生诊断、原生行列
  通过 `errors.As(*yamlx.SyntaxError)` 保留。gateway 不新增显示诊断，以遵守原有中文合同。
  `TestReviewN73DProviderReaderCauses` / `TestReviewN73DGatewayReaderCauses` 通过；
  原235条 provider 完整显示参照也通过（0位置差）。修复提交：`117f785f0`。
- **F2 / medium：AST 门漏扫 cmd/tools 对 yamlxtest 的运行时导入，并放过 yamlx 任意子包。**
  原门仅限制 internal 下运行时 helper 导入，且 WalkDir 跳过整个 yamlx 目录。
  合成违规文件在 cmd 导入 yamlxtest、在 yamlx/bypass 直接引用 yaml.Unmarshal，旧门均退出0；
  修复门均退出1。原有 raw Unmarshal、runtime Clone、internal helper 负控旧/新均退出1。
  现扫描全部子包，仅 yamlx 本包和 yamlxtest 本包可直接读 YAML；两个固定 oracle 工具和
  testdata recorder 可以导入测试 helper，其余 cmd/tools/internal 的非测试文件均禁止。
  11个永久正/负控覆盖 import alias、函数引用、dot import、Clone、helper、子包，全部通过。
  修复提交：`02afae340`。
- **F3 / low：冻结档案缺少独立全来源执行门。**
  原有 Classify 按调用者选中的来源运行；convert 的3837来源只跑注入值/Parse audit，
  不调用 Classify。档案条数/SHA固定不能保证全部21343来源均在每次测试实际执行。
  新增 `TestAllFrozenReaderSources` 对每条来源调用 Classify，逐条校验当前完整图或完整错误，
  不恢复旧实现；执行21343，unknown=0，类别计数与作者报告一致。修复提交：`02afae340`。

- **F4 / low：deploy.SourceError 把原生未知坐标显示为第0行/第0列。**
  NUL 控制字符的原生解析错误没有位置；未闭合 flow list 只有行号。原实现无条件把0
  转为位置指针，向用户显示不存在的行列。新增三种真实 Parse 合成控制，修复前前两种
  失败；现在仅附加原生确有的正行号/列号，保留原始原因和诊断。provider hook 的历史
  展示合同不变，235条冻结完整显示参照继续通过。修复提交：`2d027e716`。

- **F5 / medium（A12）：规则文件明细错误地按 YAML 结构解释展示行。**
  Python `push_diff.rule_entries` 逐行扫描；Go RuleEntries 却调用 Parse 并解 alias/去注释。
  因而正常 block 中的注释展示、flow 计数、非法缩进、`payload: [`、alias、BOM 和
  首200行探测都与 Python 不同。独立14组 Python 控制中原实现9组失败。
  按协调者明确要求恢复逐行扫描，不经过 YAML reader；invalid UTF-8 仍显示字节大小。
  本基线不存在指定的 rules/ruleset_lines.go；复用现有 rules/text.go 的行切分算法，
  增加 RuleFileLines 入口支持 Unicode 分隔、空行和无限长行，不维护第二份切行算法。
  新 oracle 逐项比较原始条目、计数和完整展示；14/14通过，含1 MiB行、空文件、有效
  depth40 alias bomb（42个字面条目）。contentsync 公共入口也确认42项，循环flow为0项，
  没有 graph 构造/引用展开。修复提交：`cf23b3fd7`。
- **F6 / low：matcher 内部 reader 没有保留错误原因。**
  外层 Match 无匹配合同与 Python 相同，但 yamlPayloadLines 把 policy/alias/syntax
  全部变成 nil。现内部 helper 返回原始 error；公共迭代器只在拒绝来源边界转为空迭代，
  不重试纯文本，不生成配置。新增子进程10秒硬截止测试检查原因/行诊断、depth40/self
  和公共无匹配结果；实际约10毫秒完成。修复提交：`cf23b3fd7`。

## 验证（迭代）

所有下列命令由 `RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server; …'` 执行。

- 修复前：`go test -count=1 -run "^TestReviewN73D.*ReaderCauses$" ./internal/gateway ./internal/provider` → exit1，14个场景丢失 cause。
- 修复后：`go test -count=1 -run "^TestReviewN73D.*ReaderCauses$|^TestN73NativeDiagnosticInterface$" ./internal/gateway ./internal/provider` → gateway0.009s/provider0.043s PASS；随后两个包 vet 通过。
- `go test -count=1 -v -run "^TestYAMLFacade|^TestAllFrozenReaderSources$" ./internal/yamlx ./internal/yamlx/yamlxtest` → yamlx0.572s/yamlxtest4.948s PASS；21343条真实冻结来源实际执行。



补验：`go test -count=1 -run "^TestReviewN73DSourceErrorUnknownCoordinates$|^TestN73NativeDiagnosticInterface$" ./internal/deploy ./internal/provider` → deploy0.022s/provider0.078s PASS。
`go test -p=4 -count=1 ./internal/rules ./internal/deploy/...` → rules3.607s/deploy24.485s/contentsync0.642s/pushdiff7.947s PASS；随后相关包 vet 通过（规则明细恢复前）。


## 逐调用点审计

已读 AGENT-BRIEF、PORTING、WORKFLOW、N7设计全部 Clarified、作者 n7-3d 报告、
review-n7-3b / review-c29 / review-a19 / review-a12。review-n7-3a.md 和 review-n7-3c.md
在基线中不存在，补读对应 progress（含 n7-3c-gate）。复核9个提交，不复制其他分支实现。

| 调用点 | 错误/回退与表示边界 |
|---|---|
| convert.loadCustomConfig | Parse → RejectNonStringKeys → yamlbridge.Value；所有失败返回，CustomConfigYAMLError 保留 cause 和原中文上下文，不默认配置 |
| convert.GenerateMihomoDocument | 先生成验证过的 YAML，再经同一原生读取/键投影；任何失败无 document |
| convert.loadProxyYAML / cache / parseStructured | cache 仅收有界无共享树，错误不入 cache，共享值绕过 DeepCopy；只有 ErrSyntax 才能尝试其他格式；policy/alias 经 ProxyYAMLError 返回 |
| importer.Parse / parseMihomo | 完整图先 Parse，失败 `%w` 包装；选中 JSON 子树 ToJSON、保留 custom 子树 Encode/Marshal 再验预算；失败无 Plan |
| rules.yamlPayloadLines / Match | helper 返回原始错误；外层 lookup 拒绝整来源为无匹配，不尝试按纯文本匹配；与 Python 解析异常合同一致，另有批准的 D1/alias 拒绝 |
| gateway.BaseDocument / yamlPayloadStrings / ExtractCIDRs | 全图 Parse 后读 shape；错误不生成 capture/default，F1保留类型/原生行列；展示仍用既有固定中文上下文 |
| provider.loadProviderProxies / errors / hooks | Parse → 选 proxies → StringifyScalarKeys → ToJSON；任何策略/alias错误作为验证错误返回，不走 transport/cache fallback；F1保留全 cause 链 |
| deploy.Locations.SourceError / SourceError / ConvertMosDNSRules / Build | hook 保持既有一基展示；直接 SourceError 修复未知0坐标；构建/转换拒绝保留 cause，无空 provider/默认规则产物 |
| aggregations.providerYAML | typed JSON → FromJSON → Marshal；不再把 JSON 当 YAML 读，Marshal/JSON错误直接返回；emoji、字符串、任意精度整数 tag 测试仍有效 |
| tools/proddiff/generator provider reload | Parse → RejectNonStringKeys → 物理 Value；所有失败直接返回，未加比较归一化或 baseline 豁免 |
| contentsync / pushdiff provider/main | nodeItems 原生 Parse/cause；仅展示层把不可解析节点文件降为字节大小；main 保持 unparsable，准备失败仍 unknown/check_failed，不伪装 in-sync |
| contentsync / pushdiff RuleEntries | F5恢复 Python 字面逐行展示，没有 YAML graph、alias 展开或 parse fallback；生成/验证仍走原生 facade |

补充条件逐条确认：
1. 真实 Python `_iter_ruleset_lines` 对 syntax、nested payload、self、depth40 payload 均无匹配；
   `_node_change` 对 malformed/self provider 为 size；`diff_config` 对 malformed 为 unparsable。
   规则展示的历史差异已按要求修复（F5），没有把它登记成接受差异。
2. matcher 和 provider helper 仍先验整图的 alias 预算/递归；内部 errors.Is/As 检查覆盖
   depth40、self、policy、syntax。10秒子进程硬截止回归实际约10/15毫秒完成。
   contentsync 规则明细的有效 depth40 graph 只扫描42个字面条目，公共边界约39毫秒完成，
   没有构造 graph，因此无法按引用次数放大。1 MiB行和空文件均与 Python 完整展示一致。
3. matcher helper、nodeItems 保留原始 cause；公共 API 签名未改。新设计 Clarified 明确
   无匹配/size/unparsable 是只读拒绝或展示，不能成为生成/验证成功；rule listing 是独立的纯文本展示。

## 分类、诊断、删除和冻结证据

convert 独立核对保存的原始 source/tag/位置/物理 value graph，再检查278行分类和完整旧结果、
调用参数。oracle 只替换真实 Python 的 YAML loader/writer，不调用 Go 业务函数生成预期；
未改原 golden、proddiff baseline、业务差分 normalization。3837来源全部存在于冻结历史中。

| 类别 | 行数 | 抽样（mihomo_corpus 零基索引）与实际变化 |
|---|---:|---|
| YAML1.1/timestamp |182|344/345：yes 从 bool 变 string，走原 ipv6 中文验证；成功生成输出另见原生数字/时间类型变化 |
| native-diagnostics |34|550/977：旧最终 problem mark 第8行/第1列，原生报第7行，仍同类 CustomConfigYAMLError/同中文上下文 |
| nonfinite-scalar-text |31|439/494：`.number` 的 float .inf → 原文 string；全部成功生成变化只在 `.number` |
| tagged-collections |31|686/860：本批实际是 binary 标量，`.bin` bytes → 原文 base64 string；不是额外的 collection 业务变更 |

对所有成功生成的 changed 行用独立 Python loader 比较完整输出：nonfinite只有 `.number`
float→str31处；binary只有 `.bin` bytes→str31处。typing 子集有 `.redir-port` int→str46处、
`.mixed-port` str→int46处、`.text` str→float10处、`.date` date→str33处。
**端口字段确实有原生隐式类型变化**，属于设计已批准的 YAML1.1/native 解析变化；没有把它
描述为“只有 metadata 改变”。timestamp/binary/nonfinite 的本批来源才只影响对应 metadata。
YAML1.1 布尔变字符串而拒绝的输入，仍使用原验证消息，无默认/空值代替。

34条 diagnostic 逐条重算 `n73d_diagnostic_positions.json`：32个主行号变化、34个不可用列被移除，
全部保留 `mihomo.custom_config YAML 解析失败: YAML 语法错误` 前缀和原生诊断。按 §5.7
允许使用原生实际位置，不能补造不存在的列。provider 235个完整显示文本（193拒绝）逐字相同，
位置差0；F4只修直接 deploy formatter 的两种合成未知坐标，未改这235条。

A19四条 oracle：111/130/209/210，实际3个来源，递归1、隐式schema1、Tab1来源影响2行；
changed=classified=4/unknown0。旧 fn/args/result/corpus SHA 校验保留，其他7490行继续严格比较。
importer14来源 equal12/diagnostic1/位置1；matcher981来源 equal940/diagnostic41；unknown均0。

实际 Go import/调用中 Load/SafeLoad/LoadWith/LoadCSafeFirst、旧 Timestamp/Mapping、pyyaml、yamltest
引用为0（AST门自身的禁止列表字符串不计）。旧5个loader文件和pyyaml目录均不存在。
yamlbridge 仍是唯一 jsonx writer 过渡层；不支持的[]byte等会明确报错，未静默换类型。
Node时间戳/二进制/非有限数到jsonx的原文字符串化是批准的显式边界，Value回归检查类型与共享；
没有 runtime Clone、每个alias occurrence展开、截断或null填充。

共享校验先检查循环/标准tag形状/merge/明确标量内容/D1，再按yaml.v3 v3.0.1预算检查alias：
>100 alias操作、>1000总操作且超过比例时拒绝；允许比例从400k前99%降到4m时10%。普通节点无此大小上限。
Parse/Decode/Marshal及选中子树边界共用检查；现有深度/子树稀释回归保留。

21343条完整历史来源现由独立测试全部执行（不是只读取档案的条数/SHA）：equal17094、typing574、
diagnostic1641、tagged256、policy383、D1 16、native grammar1256、composite10、unknown-tag92、
unsafe-alias14、deep2、explicit-native5，unclassified0。每行比对当前完整原生图或完整诊断，
source SHA/revision/archive SHA均严格固定；旧图/拒绝是历史数据，没有恢复旧解析器。

## 定向验收与复现

所有命令在 .64，经 rt 执行。报告只引用测试合成数据；完整gate如读取快照，仅记录汇总。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; go vet ./internal/gateway/... ./internal/provider ./internal/yamlx/...; go test -p=4 -count=1 ./internal/gateway/... ./internal/provider ./internal/routersplit ./internal/yamlx/...'
```

结果：gateway8.500s/provider411.365s/routersplit9.378s/yamlx5.381s/yamlxtest7.032s PASS；
vet silent。provider时服务器load25–28，未跳过失败或修改超时。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/deploy/pushdiff/testdata/gen_review_n73d.py; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/deploy/pushdiff/testdata/check_review_n73d_contracts.py; cd server; go vet ./internal/rules ./internal/deploy/pushdiff ./internal/deploy/contentsync; go test -p=4 -count=1 ./internal/rules ./internal/deploy/pushdiff ./internal/deploy/contentsync'
```

分次执行的最终输出：oracle14 cases；Python matcher四类无匹配、provider两类size、main unparsable；
rules2.946s/pushdiff6.663s/contentsync0.399s PASS，vet silent。原子完整展示JSON及原始条目逐条比较。
最后测试清理后 `go test -count=1 -run "^TestReviewN73D"` 同三个包再次 PASS（0.021s/0.084s/0.032s）。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server; go test -count=1 -v -run "^TestN73(DocumentReaderInjectionValues|GatewayLoaderOracleProvenance|NativeDiagnosticInterface|ImporterReaderMigration|MatcherReaderMigration)$" ./internal/convert ./internal/gateway ./internal/provider ./internal/importer ./internal/rules'
```

convert1.742s/gateway0.057s/provider0.084s/importer0.013s/rules0.260s PASS；日志分别报告
3837/278、4/4、235/位置0、14、981、unknown0。AST真实违规负控旧/新退出码见F2；永久11组正负控也通过。

新增Go回归行数：gateway59、provider54、deploy41、rules60、pushdiff64+47、contentsync68、
yamlxtest27；Python检查16行、oracle生成31行，14行gzip合成语料3718 bytes。
无新依赖、无新功能/操作入口、不改生产配置或快照，不需要doc操作截图变化。

## 最终完整门禁

`cf23b3fd7` 启动 .64 前台 gate，`GOMAXPROCS=4`、`-p 6`，包含
FuzzMatcherYAML / FuzzRuleEntries / FuzzNodeItems / FuzzCustomConfigYAML（各60秒）。
使用900秒无slot截止包装器；获得slot后不设900秒测试运行截止，不会误杀已开始的gate。
日志 `.ci-results/review-n73d-gate/gate.out`。当前排队，尚未运行任何gate阶段；最终结果待补。
