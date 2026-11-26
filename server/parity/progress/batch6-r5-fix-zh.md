# batch-6 R5 共享标量门禁失败修复

分支 `go-fix-batch6-r5`，基线 `824189f07`。只处理 convert、notifications、repo 的 R5 失败；
不触碰 gateway/onboarding、pyref 审计、proddiff。未运行完整门禁，未 push。

## 判断与改动

| 失败 | 分类与最终行为 |
| --- | --- |
| Common UDP fallback RuleComment | 旧断言过时：纯格式化得到 MATCH,5,；生成入口另外拒绝数字 value |
| EasyTier mtu:"1500" | 旧断言过时：有效整数 1500；增加正断言，以 "1500.0" 保留错误断言 |
| Node name:5、转换日志 | 协调者确认 Name 非严格身份字段；输出、日志均使用 "5" |
| Node.Type:5（追加裁定） | 实际使用存储节点时拒绝，保留原 JSON；proxy_string 覆盖路径不读取它 |
| Node port:"80" | 真实生成缺陷：不再回取字符串 tag，输出整数 80；存储仍原样保留 |
| Rule library URL:0 | 旧断言过时：读取 "0"，格式 text、路径 Rules.list；其他无效类型不放宽 |
| notifications occurrences:"7" | 旧断言过时：读取 7，修改 stale 后保留原始带引号字节 |
| repo string revision、numeric name | 读取无类型问题且分别为 60、"7"；原始写入校验不变，修复 revision 为 61 |

协调者明确批准 RuleConfig.Value 的严格身份检查放到生成入口，RuleComment 不改签名。
同属已定身份字段的 rule_type 使用相同原值检查。协调者另批准 Node.Type 实际使用时严格校验；
其他字段不扩大范围。三个生成入口复用 common 的 typed 校验，
legacy 只负责转码；复用现有 identityString 和中文模板，避免另造模型规则。

## 当前状态

已应用 Node.Type 追加裁定，最终定向 race、受影响三包 vet、gofmt 检查全部通过。不再扩大字段范围。
4548 字节样本静态审计：实际使用的存储 Node.Type 没有非字符串。
Node.Type 的 5、false、[] 三例修复前均失败（0.016s）：数字被输出为字符串类型，bool/数组
静默得到 nil；proxy_string 覆盖正对照保留。修复重用现有节点序列化结果和 identityString。

## 证据与验证

- 基线上逐条复现用户列出的全部失败（race，三个包，定向 -run）。
- 新的 TestBatch6R5GenerationRejectsRuleIdentityTypes 和修订的 TestMihomoQuotedPort
  在产品修复前失败：三种生成入口未拒绝原值，port 为字符串 tag。
- 正对照覆盖字符串规则值有效、原始字段往返未变、带前导零 port 生成整数、数值 80.0 的
  旧 R2 测试不变，开放 Params/password 测试不变。

完整三包 race 首轮使用默认 10m 超时：notifications 17.783s、repo 394.988s 通过；
convert 600.223s 在 TestN88GenerationBytes（当时已运行 7m2s）超时，不能据此宣称 convert 通过。
只重跑 convert，显式 -timeout 30m：1800.074s 超时，停在
TestMihomoRecordedPythonTests（该项已跑 10m5s），当时执行序列已越过 TestN88GenerationBytes。
未出现新的断言失败输出，但**整包 convert race 未完成，不能宣称通过**。
按 Leaner 收口，不第三次重复作者整套生成对照；最终定向命令及结果见下。
新增入口只转码 rule_configs，避免为类型检查再解码一遍完整共享快照。
固定 RT_HOST=sunbroke@192.168.0.32，-p 4。


## 命令

所有命令固定 `RT_HOST=sunbroke@192.168.0.32`，不跑完整门禁。

```sh
# 整包首轮：两个包通过，convert 默认 10m 超时
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -p 4 -count=1 ./internal/convert ./internal/notifications ./internal/repo'
# 仅 convert 延长上限：30m 超时于完整 Python 生成对照，整包未完成
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -p 4 -count=1 -timeout 30m ./internal/convert'
# Node.Type 修复前负对照：FAIL 0.016s
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -p 4 -count=1 -timeout 5m ./internal/convert -run "^TestBatch6R5(StoredNodeType|UnusedStoredNodeType)$"'
# 最终定向 race（所有列出的失败、原值诊断、覆盖路径和相关 native/typed 测试）
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -p 4 -count=1 -timeout 10m ./internal/convert ./internal/notifications ./internal/repo -run "^(TestCommon|TestTyped|TestNode|TestNativeNode|TestNativeStructured|TestNativeNonfinite|TestMihomoNumeric|TestMihomoQuotedPort|TestBatch6|TestConversionLogLines|TestMihomoReviewEmptyLibraryURLUsesRuleFormat|TestN87ReviewOccurrence|TestModelR5StoredValues)" && go vet -p 4 ./internal/convert ./internal/notifications ./internal/repo && test -z "$(gofmt -l internal/convert internal/notifications/review_n87_test.go internal/repo/model_rows_test.go)"'
```

最终输出（退出 0；vet 和 gofmt 检查无输出）：

```text
ok  configflow/internal/convert        1.191s
ok  configflow/internal/notifications  1.076s
ok  configflow/internal/repo           16.358s
```

完整 convert 对照/最终批次门禁由协调者运行，本报告不把整包超时记作通过。
无待完成产品改动；没有修改其他 Codex 的文件，没有改冻结字节清单或通用比较器。

