# N8-8a 调用方与通用标量修复交接

分支：`go-fix-n8-8a-provider`；当前基线：`batch-6@8ec68fd4d`。
原基线为 `go-review-n8-8a@6e9d0b738`，现已按 `--onto batch-6 6e9d0b73` 换基。
提交按模型入口、拓扑/投递、检查服务/参考夹具分开；提交号见交回消息。
固定测试机 `sunbroke@192.168.0.32`，所有测试、格式和 vet 都通过 rt 前台运行。
无完整 gate、无 push、无子 agent、无真实设备/生产访问。

## 修复与边界

1. **Provider 错把坏类型当成功**：`yamlx.Get` 在非 mapping 上返回 nil，typed 路径因而
   静默忽略非对象 definition/override。增加明确 mapping 校验，未使用的 Provider 在
   原 reachability 阶段校验，避免遮住先发生的链出口冲突、缺失目标、重名等业务错误。
   四行 snapshot 本来仍是完整列表，失败摘要的 `got null` 并不代表整份 outcome 消失。
2. **原值诊断**：Provider 的非字符串 dialer-proxy 诊断改用无损 JSON，列表不再显示为空；
   字符串原消息不变。四个 HTTP single/malformed 用例分别固定完整 Python/native body，
   连同订阅准备上下文、文件名、400、headers 和持久化效果一起检查。
3. **dialer_ref 业务拒绝**：模型把错误形状记成类型问题时，不得读作 nil 后取消代理链。
   使用前检查模型证据并报告完整原存储 JSON，例如：
   `dialer_ref 必须是 {type: node|group, id: 非空字符串}，当前值："b"`。
   同时覆盖 ConfigDialers、NodeDialer、NodeDialerTarget、ResolveNodeDialer；保留原业务错误类型。
4. **ippresence 三态**：显式 null/无法解析的规则库上下文和原生类型错误保留 unknown，
   不借 R5 零值继续读缓存。空字符串/空对象仍是 legacy 空库；首个匹配声明之后的无关
   损坏尾项不抹掉已验证的前缀。公共 `rulesetfmt.LibraryIndex` 负责统一选择规则，
   RuleFormat 与检查服务共用，不复制格式推断。已选 MRS 格式仍不能按文本缓存判 true。

## 给 N8-8c 的共享入口

- `model.String.UnmarshalJSON`：文本和有效 JSON 数值 token；无 float64 舍入。
  123 → "123"，123.0 → "123.0"，1e3 → "1e3"，123.50 → "123.50"。
  协调者已确认保留拼法，不统一数值等价写法；null 读作缺失文本，bool/容器不读作文本。
- `model.Int.UnmarshalJSON`：原 R8 整数数值写法，加严格 ASCII 十进制整数字符串。
  可有前导负号/零；拒绝内部空格、加号、小数点、指数、Unicode 数字及 int64 溢出。
- `model.DecodeRecord`、`model.SetMemberJSON` 的普通 string/Int 字段、指针、列表和
  `Map` 值使用同一规则。瞬时 protocol struct 可直接声明 `model.String` / `model.Int`。
  `json.Number` 仍严格拒绝引号数字。没有 Node 专用转换标记。
- 改变的读取类别：节点 server/name/port；协议 password 等文本；策略组 interval 等
  整数参数；资源/引用 ID、profile/状态文本；URL/系统文本设置。原始存储字节、未知字段
  与未改成员继续按 R2 保留。请求绑定和后续业务/归属检查仍有自己的约束。
- 开放 Mihomo Params 和 proxy_string YAML 保留值及 tag；数字 password 两条来源均仍
  输出 `password: 123`。quoted port 的 typed 值为 443，生成时保留原引号，正常字节不变。
  routers/agents 的归属、manifest、证据回归已单独跑 race，确保兼容读取不代替归属验证。

## 精确夹具与登记

原始 Python gzip/golden 未改；没有通用 normalizer 豁免。

- provider 四行 366/369/375/378：完整输入及全部 608 个 native outcome；仅 300 个
  AttributeError → 明确中文类型错误。300 个业务拒绝和 8 个头部结果不变；固定变更项数。
- ippresence：22 个已批准的 AttributeError → unknown，逐项固定原行 SHA-256、Python
  错误和 native null；其余结果保持原比较。空库和匹配后的尾项没有加入豁免，修产品恢复。
- repo：compat 逐行固定输入摘要、完整 native 值/错误；reference 逐项固定完整 Python
  record 和 native 结果/错误类型。包括 CPython U+001C、严格 bool、无关来源、params、
  数值 structured type，以及通用标量读取后仍由 legacy 结构/来源归属检查拒绝的情况。
- convert corpus 2021/2103：GitHub 代理域名 5 读成 "5" 后启用前缀；固定完整输入、
  原输出和完整 native 输出的摘要，不复制归档 URL。

所有类别与具体行号已写入 `behaviour-changes.md`；通用规则同步 `design/n7-n8.md` R5/R8。
原复核遗漏 provider/订阅调用方的问题已在 `review-n8-8a.md` 补记。

## 修复前证据

- provider `TestCorpus`：FAIL 227.689s，恰好四行，首个差异 outcome 2 为 AttributeError → 成功。
- `TestTypedTopologyMalformedProvider`：非对象 definition/override × forced false/true 均错误成功。
- 订阅四个 malformed HTTP 子例：FAIL 4.465s，原值列表显示成空；状态仍为 400。
- 数字 server/通用 router name：新断言分别显示为空；quoted 整数原 Int 测试明确拒绝。
- `TestTypedMalformedStoredReference`：FAIL 0.004s，坏引用静默成功。
- `TestNativeFormatErrorsRemainUnknown`：null/非空字符串库读成 true，原生 ID/URL 错误未捕获。
- 首轮跨包检查另发现 ippresence 28+15 项及 repo 调用方差异；已逐类修复/登记。
  随后跨包又发现空库/损坏尾项 9 项被过早拦截，已修产品而非放宽夹具。

## 原基线验证（历史证据）

以下命令 body 均通过 `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc '…'` 执行。
格式检查覆盖所有修改 Go 文件，`go vet ./...` 已通过；不运行完整 gate。

```sh
cd server &&
go test -race -count=1 -timeout=5m ./internal/model ./internal/rulesetfmt &&
go test -race -count=1 -timeout=5m -run 'TestTypedDialer|TestTypedNumericResource|TestTypedMalformedStoredReference|TestMihomoNumeric|TestMihomoQuotedPort|TestTypedTopology|TestN88ReviewByteGateCoverage' ./internal/convert &&
go test -race -count=1 -timeout=5m -run 'TestCompatGolden|TestReferenceValidateDocument' ./internal/repo &&
go test -race -count=1 -timeout=5m -run '^TestNativeFormatErrorsRemainUnknown$' ./internal/ippresence
```

PASS：model 12.823s、rulesetfmt 3.153s、convert 14.597s、repo 7.350s、ippresence 1.246s。
另有 provider corpus race PASS 286.231s；9 个三引擎字节样本与字节门清单覆盖断言通过。
没有重跑作者整套 4548 个字节对照。

```sh
cd server &&
go test -race -count=1 -timeout=10m -run 'Ownership|Manifest|Evidence' ./internal/routers ./internal/agents &&
CONFIGFLOW_DIFF=1 go test -p 4 -count=1 -timeout=20m ./internal/api/aggregations/difftest ./internal/api/rulelibrary/difftest ./internal/api/rules/difftest ./internal/api/subscriptions/difftest ./internal/provider/difftest
```

PASS：routers 1.883s、agents 28.788s；HTTP difftest aggregations 49.509s、rulelibrary 35.671s、
rules 75.464s、subscriptions 114.057s、provider 51.483s。

最终修复的定向 race：

```sh
cd server &&
go test -race -count=1 -timeout=5m -run 'TestNativeFormatErrorsRemainUnknown|TestLibraryIndexStopsAfterFirstMatch|TestGenericScalarReferenceRows|TestScalarTextBoundariesAndPreservation' ./internal/ippresence ./internal/rulesetfmt ./internal/convert ./internal/model
```

PASS：ippresence 1.386s、rulesetfmt 1.013s、convert 3.475s、model 1.016s。

最终树已对全部修改包执行 gofmt 无输出，并再次通过 `go vet ./...`；随后执行：

```sh
cd server && go test -p 6 -count=1 -timeout=30m -run 'Corpus|Reference|Golden' ./internal/...
```

**全部通过，退出码 0。** 关键末行：

```text
ok configflow/internal/convert      196.829s
ok configflow/internal/ippresence   519.903s
ok configflow/internal/provider    221.809s
ok configflow/internal/repo          3.443s
ok configflow/internal/routers     116.313s
ok configflow/internal/rulesetfmt    1.131s
ok configflow/internal/yamlx         0.485s
ok configflow/internal/yamlx/yamlxtest 0.037s
```

原基线检查全部完成。当前合并基线的验证见下；完整批次 gate 仍由协调者执行。

## batch-6 换基与冲突处理

- 基线固定为 `8ec68fd4d`，未改动 batch-6 分支。
- 保持 N8-5(c) 删除 compat_dialers.go/native_replay_test.go 的决定；没有恢复旧业务/异常桥。
  repo `nodeRawDialer` 直接使用 typed `convert.NodeDialer`，legacy import 入口只 JSON 转码；
  `checkDialers` 直接使用 typed `convert.ConfigDialers`，保留 ErrInUse 和 YAML 错误原有映射。
  `TestTypedDialerNumericProxyString` 同时检查 typed 节点与旧格式 import 入口接受数值 proxy_string。
- provider 保留 `corpusRepositoryFactory` 和 `nativeSnapshotExpectations`。
  ippresence 也同时保留 batch-6 的 replayRepository/reset 隔离守卫与逐项 native 期望。
- N8-5(c) 已删除 compat golden 测试，因此不再改动其归档 native YAML 期望。
  reference 的精确期望独立放在 reference_native_scalars_test.go；保留 N8-5(c) 的 typed
  validateDocument 调用及五条名称显示期望。
- schema str 现在按通用 model.Int 规则接受；subscription_id:0 读成 "0" 后，由 typed
  resolver 拒绝不存在的 subscriptions/0（ProfileInUse）。夹具固定完整 Python record 和
  新 typed 消息/错误类型，不放宽其他 reference 行。

换基后的检查固定 `.32`，同一条前台命令顺序运行：gofmt、全仓 vet、下列 race 与 difftest，最后全 internal 语料。

- `go test -race -count=1 -timeout=10m ./internal/model`：PASS 13.092s。
- convert 定向 race（typed dialer/数值文本/拓扑/密码/端口/精确 native 参考/字节门覆盖）：PASS 15.440s。
- repo 定向 race（`TestTypedDialerNumericProxyString|Corpus|Reference|Golden`）：PASS 21.990s。
- ippresence 定向 race（unknown、fixture 隔离）：PASS 1.717s。
- 5 组 HTTP difftest：PASS aggregations 62.513s、rulelibrary 46.212s、rules 106.485s、
  subscriptions 165.303s、provider 53.047s。

合并树全 internal `go test -p 6 -count=1 -timeout=30m -run 'Corpus|Reference|Golden' ./internal/...`
**全部通过，退出码 0**：convert 128.425s、provider 47.748s、ippresence 2.609s、repo 7.854s、
routers 78.184s、yamlx 0.224s，最后一行 `ok configflow/internal/yamlx/yamlxtest 0.013s`。

最终工作树只包含本次修复和集成适配；无待解决冲突、无失败/跳过的必跑项目。未 push。
