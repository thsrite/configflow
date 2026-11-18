# batch-7：Mihomo merge 标量 fuzz 回归修复

基于 batch-7@3b2948e1b（go@95440b69e 已是祖先），独立修复，不 push。

## 根因与修复

取回原始 `FuzzNativeMihomoGraph/e8afede0babd2821`，内容为 Go fuzz v1 的
`string("<<")`，实际 YAML 输入是裸 `<<`，不是包含双引号的 YAML 字符串。
原报错 `round trip "\"<<\"\n": <nil>` 中的双引号来自写回结果；写回仍是字符串。

根因在 `convert.projectReadGraph`：`ProjectJSONKeys` 已经展开所有 merge pair，
读取投影却仍跳过所有 `!!merge` 标量，遗留根、值、序列项及 alias target 上的标签。
`outputGraph` 按既有 ToJSON 策略将它变成字符串，导致与投影后的输入标签不同。
现在键投影之后统一处理每个标量；真实 merge pair 仍先展开，alias 仍保留物理共享。
没有改 yamlx emitter，没有放松 Equal、fuzz 断言或任何 baseline。

## 回归与特殊标量检查

- 原始 crasher 按原字节纳入 testdata。
- 新增读取/写回回归：裸 `<<` 的根、值、序列项、别名目标；真实 merge key 合并。
- 带引号的 `<<`、`~`、`null`、`true`、`0x1`、`1e3`、`=` 等 20 个字符串，
  分别覆盖根、键和值，并保留原生 null/bool/int/float 的区别。
- yamlx 层独立覆盖上述七个特殊字符串的根/键/值；原有 N72 穷举字符串与不同布局、
  style 的测试也通过，未发现 emitter 引号丢失问题。
- 为 Mihomo fuzz 再加入包含双引号的 `<<` 根、键和值种子。

## 验证

全部执行在固定主测试服务器；本机仅编辑、git 与轻量文件检查。
以下 `BATCH_TEST_HOST` 指本任务固定测试主机，不将实际主机写入仓库。

初始定向回归验证精确复现四个读取投影错误和原 crasher；修复后：
`go test -count=1 ./internal/convert ./internal/yamlx -run "^(TestNativeMihomoScalarProjection|TestMarshalQuotedSpecialScalars|TestReviewN72StringsSurviveMarshal|FuzzNativeMihomoGraph)$"`
→ `ok configflow/internal/convert 0.008s`、`ok configflow/internal/yamlx 0.423s`。

最终包级 race、格式与 vet：
```sh
RT_HOST="$BATCH_TEST_HOST" rt -- bash -lc 'cd server && gofmt -l internal/convert/proxyyaml.go internal/convert/mihomo_native_test.go internal/convert/mihomo_scalar_test.go internal/yamlx/native_roundtrip_test.go && go vet ./internal/convert ./internal/yamlx && go test -race -count=1 -timeout=20m ./internal/convert ./internal/yamlx'
```
`gofmt -l` 无输出，vet 退出 0；race 最终输出：
```
ok configflow/internal/convert 1133.558s
ok configflow/internal/yamlx 17.059s
```

六个 fuzz 均使用以下命令形式，每目标 `60s`，每批最多三个目标：
```sh
RT_HOST="$BATCH_TEST_HOST" rt --no-sync -- bash -lc 'cd server && GOMAXPROCS=2 go test -run "^$" -fuzz "^TARGET$" -fuzztime=60s -parallel=2 -timeout=4m PACKAGE'
```
实际目标与最终输出（全部 `PASS`）：

| 目标 | 包 | 用时 | execs |
| --- | --- | --- | --- |
| FuzzNativeMihomoGraph | internal/convert | 61.012s | 176956 |
| FuzzNativeRoundTrip | internal/yamlx | 61.029s | 73208 |
| FuzzNativeNoPanic | internal/yamlx | 61.037s | 71211 |
| FuzzNativeNodeGraph | internal/yamlx | 60.089s | 787388 |
| FuzzEditScalar | internal/yamlx | 61.035s | 22148 |
| FuzzEditDeletePair | internal/yamlx | 61.024s | 114552 |

没有新 crasher，未运行完整门禁。

## 下游参考指纹

`tools/pyref/sources.json` 中仅三个 MosDNS 参考组包含 `proxyyaml.go`。
先用只读 `PYREF_MODE=check` 校验参考：TestCorpus 47072 行、3706 个实时 Python
native/structural comparisons，另有 TestRecordedPythonTests 的 5436 次记录调用，均 `0 differ`。
初次检查仅报告该源文件的哈希变化，entry 数量未变（2863、0、5）。
随后显式刷新这三个参考文件的元数据，再做一次只读 check：
```sh
RT_HOST="$BATCH_TEST_HOST" rt --no-sync -- bash -lc 'cd server && PYREF_MODE=record PYREF_ACCEPT=1 go test -count=1 -timeout=20m ./internal/mosdns -run "^(TestCorpus|TestRecordedPythonTests|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython)$" && PYREF_MODE=check go test -count=1 -timeout=20m ./internal/mosdns -run "^(TestCorpus|TestRecordedPythonTests|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython)$"'
```
最终输出：`ok configflow/internal/mosdns 49.029s`（record）、
`ok configflow/internal/mosdns 49.553s`（check）。实际命令还用 `--back` 取回三份参考文件。

刷新前保存了所有 entries，刷新后比较解压后的完整 JSON：三份文件唯一变化均为
`metadata.sources.server/internal/convert/proxyyaml.go`；其余全部字段及 2868 个
exact input/output entries 完全相同。无 oracle 输出、输入、数量或比较规则变化。

完整 core 留给协调者在其测试服务器重跑，本轮无遗留修复项。


