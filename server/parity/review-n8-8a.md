# N8-8(a) 独立复核

复核分支 `go-review-n8-8a`，从作者 `go-n8-8a@3f328827` 建立；作者基线为
`go@fbbe382ec`。复核者未参与本次 convert 移植。范围含六个 convert 核心、legacy/codecs、
节点缓存、dialer/Provider/topology、EasyTier、rulesetfmt typed 入口 `a633babfe`，以及
共享 yamlx 提交 `2aee129ab` / `e11becd23`。

结论：**0 high，1 medium、1 low，均已修复。** medium 是 MaterializeAliases 的一个合法编辑
回归；low 是字节门的覆盖清单可缩减而不失败。未发现正常转换输出的字节差异，未发现六个核心
保留动态业务逻辑或旧异常。按 Leaner，只检查作者测试的实现、跑风险代表及复核新增用例，
没有重跑 4548 份输出或作者完整 corpus/fuzz/全量 race，没有跑完整 gate。

## F1 — medium，移动而非删除锚点定义时，合法编辑被拒绝（已修）

`yamlx.MaterializeAliases` 会把首次提前出现的 alias 改为定义，并把后续 alias 指向该新定义。
但如果原定义仍在树上，只是位置被移到第一次使用之后，旧指针已在 emitted 中，遍历直接返回，
旧定义继续带同名 Anchor。writer 实际再次定义该名字，后面的 alias 却指向第一份定义。
新增静态检查因此报告「YAML 输出引用必须指向此前定义且名称一致的锚点」。

最小源文档：

```yaml
definition: &value {key: example}
first: *value
last: *value
```

把 `first` 移到 `definition` 之前，语义仍可通过 materialization 保持。
修复前 `TestN88ReviewMaterializeMovedDefinition/moved_later` FAIL，原定义已删除的正对照 PASS。
修复：遇到已被更早消费者接管、但仍物理存在的原定义时，清掉其旧 Anchor，保留普通值及其注释；
后续引用继续指向真正输出的第一份定义。没有放宽 writer 的名称/指针核验，没有运行时 Parse/Equal，
没有展开 alias。测试覆盖直接 MaterializeAliases→Marshal→Parse 和公开 Edit，两种情形均比较完整语义。

## F2 — low，字节门可以丢掉整种生成器而通过（已修）

实际冻结夹具确实包含 Mihomo=2353、Loon=1064、Surge=1131，共 4548 个唯一 source/row。
但 TestN88GenerationBytes 原来只要求 cases >=10，随后仅校验尚存的各行。
一个负控把临时远端夹具改为前 10 条（全部 Mihomo），原字节门仍 PASS：
`original Go byte comparisons: map[generate:10]`，`ok .../convert 3.485s`。
文件在 EXIT trap 恢复，旧 golden 和本地夹具未改。这是验证覆盖缺陷，不是已证实的生成结果变化。

修复：生成前验证固定三种引擎及各自精确数量、source/row 不重复、row 范围和 SHA-256 格式。
`TestN88ReviewByteGateCoverage` 对完整夹具通过，对「10 条 Mihomo」和重复行明确拒绝；
完整 TestN88GenerationBytes 使用同一检查。未重新录制哈希或改 source archives。

## 六项重点核对

1. **正常生成字节。** 阅读字节门全部路径：Mihomo 调公开 GenerateMihomoConfig，Loon 和 Surge
   分别调对应公开入口，源归档 SHA 固定，比较原始输出 SHA-256，不进行 YAML/INI 语义归一化。
   复核自行增加 TestN88ReviewByteSamples：每种引擎抽首/中/末各一行（共 9 行）。
   在当前树和真正的迁移前 `2277ce047d4abb78ffed436442c19b31152ba85d` 隔离 worktree 上跑同一测试，
   九份哈希均与冻结清单一致。隔离树只复制采样测试和清单，不复制产品实现；已删除。
   抽样 source/row：generate 300/4090/7785，loon 1/6313/11999，surge 0/6312/11998。
   不把作者的 4548/4548 结果冒称为本次复跑；本次独立证明了三条输出路径、样本来源和覆盖防退化。
2. **旧入口。** legacy.go 的 common/node/EasyTier/dialer/Provider 接口只做 JSON/Node 转码、
   原有可变返回值的拷回和错误类别适配。resource-array 解码的循环只验证对象/数组类型，
   不重复过滤、引用解析、图遍历或格式推断。rulesetfmt.Format 只解码并调用 RuleFormat，
   FormatSkippingInvalidEntries 保留 matcher 无 error 返回的旧签名。旧 helpers/异常仍只供未迁的
   (b)/(c)/(d)/N8-9 调用方使用，未把整个 convert 包冒称已经去掉 jsonx。
3. **六个核心。** common.go、mihomo_nodes.go、proxyyaml.go、easytier.go、dialers.go、
   dialers_api.go 的导入/标识符检查及手工读取一致：没有 jsonx/any、动态异常，也不反调 legacy。
   NodeConverter 是 typed interface，不是 interface{}。图、过滤、命名和规范化业务只有 typed 实现。
   缓存只保留不可变序列化结果，返回重新解析的私有图；alias 图不进入 proxy cache。
4. **畸形值。** 按 design R5/R8 核对 common 默认、Node/Params、EasyTier、资源/引用、URL 等边界。
   没有恢复 Python truthiness、动态 ID 相等或内部异常。显式校验利用 model 保留的原成员证据，
   类型读取及存储原文保留仍由 model 负责。旧 malformed oracle 的跳过只由字段类型/形状分类，
   不按转换结果是否失败决定；common/node/EasyTier/dialer 的分类计数在作者交接中登记，
   behaviour-changes.md N8-8a 各节对应 R5、R8、URL、端口、Unicode、输入副作用等差异。
   非 ASCII ordinal、U+001C/U+001D 等已批准的原生边界有独立期望；错误类型 URL 四种空假值仍明确拒绝。
5. **yamlx。** Marshal 的静态绑定检查按实际输出顺序核对先前定义的名字及目标指针；
   重定义后指向最新定义的合法 alias 仍接受，零 Kind/悬空/前向/错误绑定拒绝。
   tagged scalar policy 在准备输出后检查，不改 FromJSON→ToJSON 的原数字文本；普通有限值及下溢有正对照。
   修复 F1 后，合法的 anchor 移位编辑也能闭合。没有增加生产路径的再 Parse/Equal。
6. **31 行浅拷贝副作用。** 夹具固定 source 与 loader oracle SHA、31 个不重复 row，路径只能是
   `/nodes/<index>/params/reality-opts/short-id`。应用前要求源缺字段、Python mutated 有且为 `""`；
   删除这一项后对全部 siblings 精确比较。最终仍使用原 runMihomoRow 比较完整 mutated profile，
   其他字段没有 wildcard/忽略。独立 producer 只查看冻结输入和 Python expected，所调用的
   test-only ReplayIssue 仅作类型分类，不调用转换来生产期望；原 golden 未改。

## 定向验证

全部固定 `RT_HOST=sunbroke@192.168.0.32`，前台完成；完整命令及最后输出如下。

```sh
# F1 修复前（只增加回归测试，产品为作者版本）
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/yamlx/review_n88_test.go -- bash -lc 'cd server && gofmt -w internal/yamlx/review_n88_test.go && go test -count=1 ./internal/yamlx -run TestN88ReviewMaterializeMovedDefinition -timeout 2m'
# FAIL moved_later: YAML 输出引用必须指向此前定义且名称一致的锚点（第 3 行）
# FAIL configflow/internal/yamlx 0.002s

# F2 修复前负控：临时清单只保留前 10 条；结束自动恢复文件
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; task_n88_saved=$(mktemp); cp internal/convert/testdata/n88_generation_bytes.json "$task_n88_saved"; trap '\''cp "$task_n88_saved" internal/convert/testdata/n88_generation_bytes.json; rm -f "$task_n88_saved"'\'' EXIT; python - <<'\''PY'\''
import json
from pathlib import Path
p=Path("internal/convert/testdata/n88_generation_bytes.json")
f=json.loads(p.read_text())
f["cases"]=f["cases"][:10]
p.write_text(json.dumps(f))
PY
go test -count=1 -v ./internal/convert -run "^TestN88GenerationBytes$" -timeout 3m'
# PASS original Go byte comparisons: map[generate:10]; convert 3.485s

# 修复后检查和独立 9 样本（本次新增测试，不是完整字节门）
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/yamlx/edit.go --back server/internal/yamlx/review_n88_test.go --back server/internal/convert/generation_bytes_test.go --back server/internal/convert/review_n88_test.go --back server/internal/convert/review_n88_gate_test.go -- bash -lc 'cd server && gofmt -w internal/yamlx/edit.go internal/yamlx/review_n88_test.go internal/convert/generation_bytes_test.go internal/convert/review_n88*test.go && go test -race -count=1 ./internal/yamlx -run "TestN88Review|TestMarshalManualNodeBindings|Test.*Materializ" -timeout 3m && go test -race -count=1 -v ./internal/convert -run "TestN88Review" -timeout 3m'
# ok configflow/internal/yamlx 1.015s
# PASS TestN88ReviewByteGateCoverage、三种引擎各3条；convert 37.358s

# 真正旧基线独立抽样，隔离树现已清理
RT_HOST=sunbroke@192.168.0.32 rt -C /Users/sunbroke/.paseo/worktrees/302rx1dg/n88-review-byte-baseline -- bash -lc 'cd server && go test -count=1 -v ./internal/convert -run "^TestN88ReviewByteSamples$" -timeout 3m'
# PASS 9/9；ok configflow/internal/convert 1.538s
```

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/yamlx/review_n88_test.go -- bash -lc 'cd server && gofmt -w internal/yamlx/review_n88_test.go && test -z "$(gofmt -l internal/convert/generation_bytes_test.go internal/convert/review_n88*test.go internal/yamlx/edit.go internal/yamlx/review_n88_test.go)" && go vet ./... && go test -race -count=1 ./internal/yamlx -run "TestN88Review|TestMarshalManualNodeBindings|TestMarshalRejectsUnparseableTaggedScalars|TestEditRetainsCommentOnlyInputAndDisplacedAliases|TestEditRewriteKeepsCommentsAndSemantics|TestEditFailureRetainsOriginal" -timeout 3m && go test -race -count=1 ./internal/convert -run "^(TestCommonCoreHasNoLegacyDependency|TestCommonLegacyOnlyCallsTypedEntries|TestCommonTypedDefaultsAndMalformedStorage|TestNativeNodeR5AndParamsValidation|TestNestedResultsAndConversionNamesDoNotPolluteCache|TestNodeConversionsReuse|TestTypedDialerCore|TestTypedDialerMalformedValues|TestTypedNameOverrideExactKeys|TestTypedEasyTierValidation|TestTypedEasyTierUsesAliasedSections|TestMagicDNSRoutesTheZoneAndKeepsAUserPolicy|TestStaticProviderDialerAndBuiltinAllowed|TestInputPatchesEvidence)$" -timeout 4m && go test -race -count=1 ./internal/rulesetfmt -run "TestMalformedMembersUseTypedValidation|TestTypedEntryMatchesLegacyBytes" -timeout 3m'
# gofmt 无输出，go vet ./... 通过
# ok configflow/internal/yamlx 1.030s
# ok configflow/internal/convert 5.194s
# ok configflow/internal/rulesetfmt 1.013s
```

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -count=1 ./internal/convert -run "^TestNativeNodeFixPreservesInputParams$" -timeout 2m'
# ok configflow/internal/convert 1.021s
```

未访问生产/真实设备，所有输入为现有合成语料和最小合成 YAML；未 push，未开子 agent。
提交号见本报告所在的复核提交。正式合并树的完整 gate 由协调者执行。

## 合并树调用方补查

原复核漏跑了 provider 及订阅 HTTP 的调用方对照，未发现畸形 Provider 的误成功和
畸形 dialer-proxy 值的空诊断。本次补上产品校验、逐行逐项 native 期望及回归；
数字节点字段也按协调者的新裁定补齐。判定、修复前失败证据和最终跨包检查见
[Provider 调用方补漏](progress/n8-8a-provider-fix-zh.md)。原始 Python corpus 和通用比较器未放宽。
