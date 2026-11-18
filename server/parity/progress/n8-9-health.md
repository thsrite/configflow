# N8-9 health/check 接手报告

分支 `go-n8-9-health`；原始未验证草稿已按要求先原样提交
`1ff063d34`（`wip(health): N8-9 Gemini trial, unverified`）。
协调者随后指定依赖 `batch-6@824189f07`，变基后的等价 WIP 是 `33fbaabe0`。
没有复制 discovery 文件或使用其旧基线；参照阅读了 `778442e4d` 的报告。
所有测试仅在 `.32` 通过 `RT_HOST=sunbroke@192.168.0.32 rt` 执行；不 push、不开子 agent、
不跑完整门禁、不读取生产数据。

## 对 Gemini 草稿的更正清单

1. **high：修复正常服务矩阵的编码失败。** 草稿把没有 model record codec 的
   `ServiceVerdict` 放入 `model.Map`，导致 `evaluate_services` 普通成功输入返回编码错误。
   加入 response DTO 的原生 MarshalJSON，并测试实际有序 map 编码。
2. **medium：删除残留 Python 仿真。** `pystr` 的 strip/lower/int/isspace 改为标准库；
   删除 CPython UTF-8 异常跨度/文本生成和手写 ipaddress 格式器，使用 utf8.Valid、netip。
   端口读取用 strconv；ErrTimeout 改为原生错误。不再拼装 Python codec 异常文本。
   为保留旧 Go 调用方编译，缺失 health_check 的旧导出类型暂留 legacy.go，错误消息已改为
   中文常量，Unwrap 返回原生 ErrMissingHealthCheck；无 Python repr 或 PythonClass。
3. **medium：共享 scalar codec，而非两份私有 coercion。** 字符串/数字文本统一用
   `model.String`；存储整数沿用该批 model.Int（包括已批准的严格十进制字符串）。
   不绕过共享解码。请求的健康检查整数 token 校验、worker version/latency 协议校验仍严格，
   不把存储 scalar 宽容规则误用为外部协议就绪证明。
4. **medium：补全 typed latency 记录与 worker DTO。** `LatencyEntry` 从全字段 model.Fields
   改为显式的 latency/error/method/history/settings 等成员，Fields 只保留扩展；worker 入参
   直接用 ProbeSettings 与 typed payload，去掉 timeout_ms 的动态 float64 读取。
   model.Raw 保留未改的数字文本、null、扩展及顺序；额外测试副本修改不会污染来源。
5. **medium：修复标识和构造值。** 共享 model 接受 numeric text 后，ID 的业务约束仍按
   原 token 单独检查；名称按共享解码接受无损数字文本。原生 Go 构造的 RegionResult
   不再因私有 hasID 位为 false 被拒绝。异常身份的测试改为从存储行导入，而不是先通过
   业务验证 writer 强行写入非法记录；保留仓储默认其他 section，避免草稿的无效 seed。
6. **medium：修复存储读写的 R5 边界。** 坏 checked_at 不再让读取丢掉整个 hosts map 的有效
   兄弟；写入前检查当前编码后的 record/map，拒绝显式非字符串时间戳、非对象 section，
   并允许 typed 非零值修复旧 R5 字段。数字时间戳不能因共享 text codec 而成为有效时间证明。
7. **medium：修复执行器与转换队列。** ExecRunner 用 CommandContext 同时受命令预算和
   原 Runtime generation 约束，保持断开请求后继续、关机取消并回收子进程；没有起新 generation。
   转换 panic 改为逐项恢复，避免所有转换 worker 退出后发送队列死锁；日志只写固定消息，
   不包含 panic 值。真实子进程、20 项全部 panic 的批处理均有回归。
8. **medium：重写草稿的 corpus 豁免。** 原草稿对 malformed_shape_r5 和
   integral_or_range_number 不做任何结果断言，对 numeric_as_text 仅断言无错，然后 continue。
   删除这一分类器。原始3个 corpus 不改字节；新参考文件对每个 typed 差异指定精确结果或
   错误类别/中文消息，并校验原文件 SHA256、索引函数名和实际覆盖数量。
   参考生成器仅将合成输入投影到 R5/R8 schema，调用真实 Python 业务函数计算 verdict、
   target、confidence 等，**不读取 Go 输出**。结果中的兄弟字段和数值都比较；另有故意
   改 verdict、confidence、删除字段的负控。原 Python suite 的2280条不需要 typed 豁免。
9. **low：恢复被无故删掉的测试背景注释。** recorded/corpus/review 文件仍保留原录制来源和
   重录命令。review_c16 的目录删除断言改为 errors.Is(EISDIR) 有依据：迁移到 N8-7 原生
   jsonstore 后不再伪造 Python OSError 文本；空/非空目录均必须保留，缺失文件仍成功。
   请求断开回归只有格式改动，断言没有放松。
10. **low：分清存储验证和请求验证。** 新的 typed Provider/Group validator 检查解码值和
    原字段 presence；R8 的60.0/6e1/十进制字符串不会被重新当作原始请求拒绝；wrong bool、
    null/坏值仍报原中文业务校验。旧 Raw/HTTP 调用方保留严格请求 token 验证。

11. **medium：业务不藏在 legacy 回调里。** 节点转换分支、结构化解析、完整原始协议字段
    保留都在 native conversion.go，直接用 N8-8a 的 StructuredProxy/NodeProxy/FixProxyNode；
    legacy 仅将尚未迁移的 Sub-Store 回调结果转成有预算的 yamlx 图。AST 检查允许这些 typed
    API，但拒绝 convert 的旧 jsonx API。默认回调完整安装有单独测试。

## 测试期望审计

- `review_c16_test.go`：目录异常采用 native errno；目录不被删除和请求取消后完成两个
  原行为约束保留。不是为了让 os.Remove 误删目录过关。
- `recorded_test.go`：worker stdin 只允许原生 JSON 重新编码，argv、预算、环境变量、文件、
  状态与字段不变；原2280条严格比较。原 review1017条中的 typed 差异有独立精确参考。
- `corpus_test.go`：原790条不重录；原 Turkish-I/N2 URL 的既有裁定保留，未扩成通配。
  新参考仅改变有 schema 依据的行，其他行继续对原 Python 结果严格比较。
- `api/nodes/latency_test.go`：只把一处 DNS stdin 的 Python 空白/转义字节断言改成
  完整 JSON 对象相等（D2）；所有字段、单次调用、系统配置和历史不变断言保留。
- health difftest 的 UTF-8 样本分别断言 Python/Go 的状态与完整错误体；未泛化 message mask。
- `check_test.go`：已逐项审查 typed fixture 改写；普通服务、目标选择、存储边界和评分
  断言仍对应原 Python 的有效输入；新增原生构造对象覆盖私有 hasID 位的问题。

## 迁移边界与交接

- health 仓储读取使用 Repository.Shared/System，DNS 写入使用 UpdateSystem。
- ProbeStore/RegionStore 使用 N8-7 的原生 jsonstore；字段和用户键用 model record/Map。
- C16 没有 task rows 或 taskstatus owner，未新增虚构任务；Agent 删除的两个 hook 及顺序保留。
- jsonx 只在两个 legacy.go 中；旧 api/nodes、discovery、convert 和 repo 的接口在此转码。
  后续 owner 改接 typed entry 后删除对应适配；不能由 core 回调旧接口。
- 延迟文件的 ReadSharedJSON/WriteSharedJSON、substore 缓存/转换和 subs.FilterSubscriptionItems
  暂仍是相邻 owner 的旧接口；适配只留 legacy.go，不能私自替换仓储文件锁/rename 协议。

## 明确的边界差异

- 非法 worker UTF-8：保留失败状态，改成固定中文消息，不复刻 CPython 字节偏移错误。
- 环境超时参数使用 strconv，拒绝 Python 的下划线/Unicode 数字语法。
- 存储 R5/R8 使用共享模型：无损 numeric text，整数按共享 Int、wrong bool/容器按 schema
  fallback；非法身份和写入时间戳明确验证，不把零值当作有效身份/时间证明。
- delay 的存储为 float64：极大整数超范围按 R5，不保留 Python 任意精度数值运算。
  原 worker 正整数 latency 仍用 json.Number 保留精度，未截断为 int64。
- worker 失败 error 必须为非空文本，不接受 Python truthy 容器；大整数 latency 的精度另有正控。
- JSON 原生编码（D2）允许 worker 输入的空白/Unicode 转义不同；实际 JSON 值仍逐项比较。

## 验证

主干定向检查已完成（固定 `.32`）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; gofmt -w internal/health/*.go internal/health/check/*.go internal/health/difftest/*.go internal/api/nodes/latency_test.go; test -z "$(gofmt -l internal/health/ internal/api/nodes/latency_test.go)"; go vet ./...; go test -race -count=1 -timeout=15m ./internal/health/... ./internal/api/nodes ./internal/discovery; CONFIGFLOW_DIFF=1 go test -count=1 -timeout=15m ./internal/health/difftest'
```

命令的这部分全部通过；gofmt/vet 无输出，末行：

```text
ok configflow/internal/health          34.875s
ok configflow/internal/health/check    20.020s
ok configflow/internal/health/difftest   1.049s
ok configflow/internal/api/nodes       36.448s
ok configflow/internal/discovery       22.332s
ok configflow/internal/health/difftest  14.385s
```

当时在同一命令后额外加了跨包 `-race -run "Corpus|Reference|Golden"`；范围比所需的
普通跨包参考检查重。convert 持续高 CPU 运算871.716秒后由我停止（`signal: interrupt`），
不能记为通过或产品断言失败。其余已返回 provider race88.772s、rules7.774s、
rulefetch23.068s；两个 discovery 包无该名称匹配。随后单独执行普通跨包参考检查和 fuzz，
不重跑已通过的完整包 race，也没有运行完整 gate。

最初的 api/nodes 检查还识别了一处依赖 Python JSON 空白的字节断言；已按 D2 改为完整
JSON 对象相等，之后上述 api/nodes race 通过。health 的正式 Flask 对照也已通过；新增的
UTF-8 边界分别要求双方完整响应，JSON stdin 另有值变化/字段删除/非法 JSON 负控。

后续增量命令（退出0）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; gofmt -w internal/health/difftest/diff_test.go internal/health/takeover_test.go; test -z "$(gofmt -l internal/health/)"; go vet ./internal/health/...; go test -race -count=1 -run "TestWorkerFailureRequiresTextAndRetainsIntegerPrecision|TestWorkerJSONComparisonRejectsChangedValues" ./internal/health/...; go test -p 4 -count=1 -timeout=20m -run "Corpus|Reference|Golden" ./internal/convert ./internal/provider ./internal/rules ./internal/rulefetch ./internal/health/... ./internal/discovery/...; CONFIGFLOW_DIFF=1 go test -count=1 -timeout=15m ./internal/health/difftest; go test -count=1 -run "^$" -fuzz "^FuzzHealthWorkerOutputs$" -fuzztime=60s -parallel=2 ./internal/health; go test -count=1 -run "^$" -fuzz "^FuzzCheckTypedProjections$" -fuzztime=60s -parallel=2 ./internal/health/check'
```

关键末行：

```text
ok configflow/internal/health          1.038s  (新增协议/负控 race)
ok configflow/internal/health/difftest 1.072s  (JSON stdin 负控 race)
ok configflow/internal/convert      151.149s
ok configflow/internal/provider      32.098s
ok configflow/internal/rules          2.078s
ok configflow/internal/rulefetch      6.940s
ok configflow/internal/health         4.083s  (Corpus/Reference/Golden)
ok configflow/internal/health/difftest 15.508s (真实 Flask)
PASS
ok configflow/internal/health        60.113s  (427991 fuzz executions)
PASS
ok configflow/internal/health/check  61.016s  (781056 fuzz executions)
```

跨包名称过滤中 health/check、health/difftest、discovery 与 discovery/difftest 无匹配测试；
其完整包 race 或启用的相关 Flask 在之前的定向命令已运行，未把 no-tests 记作覆盖。
Flask 的44次请求要求全部原行为一致或明确断言的1个 UTF-8 原生诊断，延迟文件与 system_config DNS 持久化状态仍比较。
162条 typed 参考分布为 corpus66条、review96条；原2280条 Python suite 无 typed 豁免。

独立参考的重录命令（本次执行输出 `typed reference expectations: 162`）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/health/testdata/n89_expected.json -- bash -lc 'PYTHONPATH=. $HOME/cf-test/.venv/bin/python server/internal/health/testdata/n89_reference.py'
```

全部改动均在测试服务器 gofmt，完整 go vet 和修改测试的增量 vet 通过，git diff --check
无输出。没有完整 gate、生产快照或 baseline 变更。待协调者通知 batch-6 合入后再变基到 go，
当前交回按指定依赖基线824189f07，进入独立复核。

初始原样草稿 race 已证实普通服务矩阵无法编码；未把这个错误登记成
有意差异。中间检查揭示的 shared model 依赖、无效 seed、原生 root 校验问题均修代码，
没有重写原 corpus 或放宽未列出的结果。

## 独立复核（Codex，2026-11-18）

`go-review-n8-9-health` 从本报告的 `d6cd8df63` 复核；修复提交 `0f301dc67`。
0 high，health 内新增1 medium和1 low已修，真实 Runtime worker/文件提交检查、
定向 race 与全仓 vet通过；真实Python重录162条参考逐字节一致，实际row745索引负控被拒。
不重跑作者整套，不跑完整gate。完整发现、命令和后续API要求：
[review-n8-9-health.md](../review-n8-9-health.md)。

协调者明确把共享 jsonstore 的最终rename/unlink Context API及 discovery两个adapter
拆到 batch-7 后另派；本次只补health自己的Context入口和提交前检查，
未把整个跨模块关机协议标成零遗留。
