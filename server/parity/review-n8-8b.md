# N8-8(b) 抽查复核（Opus O7）

复核分支 `go-opus-review-n8-8b`，从作者 `go-n8-8b@c17fb2dd` 建立（叠在 8a 复核 tip 6e9d0b73 上）。
复核者未参与本次移植。按 Leaner：不重跑作者整套 fuzz / 全量 race / HTTP difftest / gate，
只跑复核修复相关的定向检查和风险抽样。8a 遗留的 provider TestCorpus 4 行、api/subscriptions
difftest 4 个子测试由 694df9d0 处理，不在本次范围。

结论：**0 high，1 medium（含同类 7 个字段），1 low（登记缺口），均已修复。** 正常输入的 Mihomo /
Loon / Surge 输出字节不变。

## F1 — medium，错型的输出键/规则文本字段被 R5 读成空串后静默生成坏配置（已修）

typed 生成器把以下存储字段直接用作 provider 键、策略组类型或规则行文本。字段错型时 R5 读成
`""`/缺省，生成照常成功：

| 输入（合成） | 迁移前 Go（≈Python） | 8b typed |
|---|---|---|
| 订阅 / 订阅聚合 `name: 7` | 明确报错 `provider name 7 is not a string` | `proxy-providers: {"": …}`，`path: ./providers/.yaml`；策略组 `use` 仍按原值渲染为 `7`，引用对不上；两个错型名称互相覆盖 |
| 规则集 `name: 7` | 同上报错 | `rule-providers: {"": …}` + `RULE-SET,,G`；下载入口返回 `Name:""` |
| 规则集 `behavior: 7` | 输出 `behavior: 7` | 静默改成 `behavior: domain` |
| 规则 `value: 443` | `DOMAIN,443,G`（有效规则） | `DOMAIN,,G` |
| 规则 `rule_type: 7` | `7,a.example.com,G` | `,a.example.com,G` |
| 策略组 `type: 7` | `type: 7` | `type: ""` |
| 策略组 `follow_group: 7`（有 type） | 找不到跟随目标，跳过该组 | 当普通组输出 |

这正是协调者点名的类别：真实的业务校验/身份被吞成空值，且错误里没有出错的值。provider 名称一项
在迁移前已有明确拒绝，属于回归；其余是未登记的 R5 输出变化，且产出 Mihomo 无法加载或语义不同的配置。

修复：`mihomo_generate.go` 新增 `storedText`（泛型约束为四种存储记录，不引入 `any`）。只有
`model.Issues` 在该成员上记录了 R5 类型问题时才拒绝，错误为
`msgMihomoStoredText`「<资源> <id> 的 <字段> 必须是字符串，当前值：<存储 JSON>」，资源名常量集中在
`messages.go`。检查点：订阅/聚合 provider、规则集 provider（生成与 `MihomoRulesetDownloads` 两处）、
规则行（ruleset 的 name；rule 的 rule_type，非 MATCH 的 value）、策略组（自身 type/follow_group，
以及被跟随组的 type）。正确类型的空串和缺失字段不受影响，因此不改变任何既有正常输出，也没有在
convert 里复制 rules/subs 的业务校验。已登记于 `behaviour-changes.md`「N8-8b 复核」。

测试 `review_n88b_test.go` `TestN88bReviewWrongTypedOutputIdentities`：一个有效正对照（含跟随组）
加 8 个错型子测试，断言完整错误文本（引用同一常量），规则集两项同时断言下载入口。
修复前：正对照 PASS，8 个子测试全部 FAIL（`got <nil>`，前 6 个与后 2 个分两轮加入，每轮都先见 FAIL
再修）；修复后 9/9 PASS。

## F2 — low，8b 已有的若干 R5/显式差异未逐项登记（已补登记）

复核探测中另外三处与迁移前不同、但作者登记里没有逐项列出：`config_token` 错型时 provider URL
不带 token；策略组引擎选项（`timeout`、`lazy` 等）错型时不写入；`region_probe_port` 为超出
1–65535 的整数时报「区域检测端口必须为 1–65535 的整数」（Python 原样注入）。行为本身符合 R5 /
显式校验，保留不改，只补登记到 `behaviour-changes.md` 同一节（纯文档，无测试）。

## 五项抽查结论

1. **typed 路径与旧路径等价。** 逐段对照 6e9d0b73 的 `generateMihomoDocument` 与新 `MihomoDocument`：
   节点选择（manual/proxies_order/跟随组/聚合根/仅聚合排除/依赖闭包）、provider URL 与 token、
   proxy-groups 字段顺序和跟随组默认值、rule-providers、gateway 时机、规则行、EasyTier、
   FilterGroups→ChainProviders→MergeHosts→NodeDNS→LogHosts→DialerGroups→ValidateRules→拓扑→
   Preflight→区域探测→监听校验的顺序一致。`orderedRoots` 改按节点表顺序只影响遍历顺序，
   `RequiredNodes` 对未知根无副作用。抽样跑了 Python 录制回放（含代理链、dialer、provider 过滤、
   DNS、规则策略校验）：1566 calls / 0 differ。
2. **16 个 typed callback。** Converter + 15 个函数字段，每个在 `legacy.go` 只有一个转码实现，
   只做 model/jsonx/yamlx 的转换和错误类别适配；需要的 12 个在生成前检查，缺失返回
   `HookMissingError`（作者测试逐个置空验证）；Converter/Cache/DownloadedText/Preflight 在用到时
   明确失败（旧版 DownloadedText 缺失时静默返回 false，现改为显式失败，更严格）。
   `TestProductionWireRegistersEveryCallback` 覆盖生产 `mihomoenv.Wire` 的全部字段。
   观察（不算本次发现）：`MihomoIntegrations.DNSLoop` 生产没有注册方，但其唯一调用方
   `ValidateBaseSettings` 目前没有生产调用，8a 已如此，归 N8-10。`BuildProviderHealthCheck`
   读取旧的 `CurrentMihomoEnv()`，生产中为空，但只用到无条件注册的 ProviderHealth，结果正确；
   该入口只剩测试调用，随 N8-11 删除。
3. **字节门。** `validateGenerationByteCoverage` 仍要求精确的 generate=2353 / loon=1064 /
   surge=1131、行唯一和 SHA 格式（8a F2 的负控仍有效）；8b 另加了 typed 入口与旧入口逐字节相同的断言。
   复核修复后重跑：4548/4548。
4. **畸形值分类。** 作者表中的 R5/R8 分类计数与本次复跑一致；本次补上 F1 的 7 个字段和 F2 的 3 处登记。
   其余检查（interval 字符串、provider_update_interval、health_check 非对象）在探测中与登记一致：
   provider_update_interval 错型仍把原值交给 refresh 校验并返回中文错误，没有被吞成默认值。
5. **业务逻辑复制。** convert 只保留 Mihomo 生成器自身的决策（选择、URL、默认值、区域探测）；
   成员渲染、过滤、链 provider、hosts、节点 DNS、dialer 组、策略/监听校验都经回调调用原 owner。
   F1 的修复只检查存储类型，不复制 rules/subs 的校验。

## 定向验证

全部固定 `RT_HOST=sunbroke@192.168.0.32`，前台完成。

```sh
# 修复前（只加测试和消息常量；产品为作者版本）
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -count=1 ./internal/convert -run TestN88bReviewWrongTypedOutputIdentities'
# 第一轮：subscription_name/aggregation_name/group_type/rule_set_name/rule_type/rule_value FAIL，got <nil>；valid PASS
# 第二轮（加 follow_group、rule_set_behavior 后）：这两项 FAIL，got <nil>

# 修复后
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && echo "gofmt:[$(gofmt -l internal/ tools/)]" && go vet ./... && go test -race -count=1 ./internal/convert/... -run "^(TestN88bReview.*|TestNativeMihomo.*|TestNativeBase.*|TestCommonCore.*|TestMihomoReview.*|TestProductionWire.*|TestGenerate.*|TestLinuxGateway.*|TestInvalidChain.*|TestProviderPreflight.*)$" && go test -race -count=1 ./internal/gateway/... ./internal/api/aggregations ./internal/api/subscriptions && go test -race -count=1 ./internal/provider -run "^Test(DefaultHooksFailClosedAndPassThrough|ReviewBundleAndCommitFinishWhenTheRequestIsCancelled|FinalBundleErrorIdentifiesActualProviderFile|PreparedProviderFailureKeepsAliasAndRealOutputPath|ValidProviderOutputAndSuccessPathParsingUnchanged|TopologyErrorHasSourceButNoGuessedLine)$" && go test -count=1 -timeout=20m -v ./internal/convert -run "^(TestMihomoCorpus|TestN88GenerationBytes|TestMihomoRecordedPythonTests|TestNativeMihomoOriginalInputsUnchanged)$"'
# gofmt:[]，vet 通过
# ok convert 26.454s；ok convert/mihomoenv 1.027s
# ok gateway 49.980s；gateway/draft 16.650s；gateway/host 25.680s；api/aggregations 12.700s；api/subscriptions 8.796s
# ok provider 3.200s（窄边界）
# original Go byte comparisons: map[generate:2353 loon:1064 surge:1131]；PASS TestN88GenerationBytes 82.81s
# TestMihomoCorpus 13908 行 0 differ PASS 50.34s；TestNativeMihomoOriginalInputsUnchanged PASS
# 1566 recorded calls / 1115 Python tests，14 skipped，0 differ PASS 45.09s
# ok configflow/internal/convert 180.287s
```

另外一次（第一轮修复后）跑了 `TestSurgeLoonCorpus`：34697 行 0 differ，PASS 77.13s（本次修复不涉及
Surge/Loon 路径，第二轮未重跑）。

探测用的临时测试和 6e9d0b73 的对照 worktree 已删除，不在提交中。没有访问生产/真实设备，输入均为
合成值；未 push，未开子 agent。完整 gate / proddiff 由协调者在批次树上执行。
