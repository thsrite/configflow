# JSON 字段大小写全仓审计

基线：`12150a402`，分支 `go-audit-json-case`。范围：`server/internal`、`server/cmd` 的非测试运行时代码，另列 fake/夹具辅助包。先登记本清单，再修改实现。类型清单由 Go AST + go/types（.32）生成；不把 map/标量调用误判成结构体。完整原始数据见 `json-case-inventory.jsonl`，生成器 `tools/jsonaudit`。

## 现行契约与发现（修复前）

- 以 YAML 入口读取 JSON 语法文本的路径仍走 yamlx.Parse/Decode：按 YAML D1 拒绝重复键，yaml tag 无 encoding/json 的大小写折叠。本清单保留 JSON decoder/Bind/model 边界，未将 20 个 YAML Decode 调用误判成 JSON DTO。
- N7/N8 D1 仅要求 YAML 重复键一律拒绝；未改 YAML。model 的 `validJSON` 另有严格存储契约，递归拒绝 JSON 重复键，`DecodeRecord` 用 `schema.byName` 精确匹配，并保留未知字段/原始字节，已安全。
- `httpx.Bind`/`BindSilent` 已将完全同名 JSON 重复成员规范为最后一个完整值（嵌套对象不合并），但最后解入 DTO 仍大小写折叠。须在共享层解决，未知字段继续忽略。
- 普通结构体、结构体数组/map、自定义 UnmarshalJSON 内的 alias，以及接收 `any`/泛型的桥接都有折叠风险。最高风险是操作/manifest 的安全版本、身份/资源证明、凭据引用、管理开关；恶意 Agent 或可编辑存储内容可让非精确字段参与决策。认证入口的 loginRequest、JWT Claims 已用精确 map 读凭据，无直接新增认证绕过证据。
- RouterOS/iKuai 的动态设备行通常解入 `any`/map，安全；设备 manifest/操作日志和 Agent 结构体回包仍需共享保护。订阅 vmessWire、Sub-Store 版本回包、规则/配置投影同样存在风险。
- 自定义 codec 一律按内部实际调用继续追踪，不在外层提前裁剪原始 JSON，避免丢弃 model 的未知字段或改变重复键拒绝。
- 可信内部 round-trip（如 Marshal 后立即解入视图）本身不会产生混合大小写/重复键；若编码来源含用户保留的 Raw/扩展字段仍视作非可信。为避免未来调用源变化，普通 DTO 桥接也使用同一 facade。fake/固定测试数据明确豁免。

## 风险结论

| 严重度 | 类别 | 实证与影响 |
|---|---|---|
| Medium | Agent/设备协议与 manifest/操作证据 | 原生负控让 TABLE/SAFETY_VERSION 覆盖精确值，并把大小写变体当有效路由证明；影响协议解释、安全版本和资源身份判断 |
| Medium | 存储设置/custom codec alias | WebDAV 负控中大写用户名/密码覆盖精确字段，AUTO_BACKUP 开启开关；需先有设置/存储写入能力，未发现新增未认证绕过 |
| Medium | 订阅/第三方回包 | VMess 负控把 ADD/PORT/PS 当真实服务器、端口、名称，Sub-Store 大写 Version 改变版本判断 |
| Medium | HTTP/MCP DTO | Bind 嵌套 ENABLED 被接受；MCP METHOD 可以执行 ping、ID 将通知变成有回复请求；认证层仍独立生效 |
| Low | proddiff 专用观察器 | 默认构建外的 Status 字段依赖折叠；已补 status tag 和共享 facade，标签负控/定向 race 通过；只影响测试等待判定 |
| 无缺陷 | model、JWT Claims、loginRequest、map/标量、可信固定结构 | 精确 map/schema 或无结构体匹配，已列全调用清单和可信理由 |

## 完整调用清单（基线行号）

“自定义”条目的内部 alias、map、model.DecodeRecord 调用均另有行；标量、map、动态值仍列出，用于证明扫描覆盖。来源列描述该边界的上游类别，桥接可能既接外部数据也接内部值；不以内部 Marshal 作为自动可信证明。

| 文件:行 | 调用/目标类型 | 输入来源 | 修复前结论/处置 |
|---|---|---|---|
| `internal/jsonresp/canonical.go:17` | `json.Decode` → `*any` | 已存配置/导入文档/JSON 桥接 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/jsonresp/stored.go:18` | `json.Unmarshal` → `any` | 已存配置/导入文档/JSON 桥接 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/yamlx/json_keys.go:64` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/model/config_decode.go:13` | `json.Unmarshal` → `*model.Fields` | 存储/导入/请求成员；model codec | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/model/config_decode.go:23` | `json.Unmarshal` → `*model.String` | 存储/导入/请求成员；model codec | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/model/config_decode.go:53` | `json.Unmarshal` → `*model.Profile` | 存储/导入/请求成员；model codec | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/model/config_decode.go:56` | `json.Unmarshal` → `*model.Shared` | 存储/导入/请求成员；model codec | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/model/config_decode.go:59` | `json.Unmarshal` → `*model.System` | 存储/导入/请求成员；model codec | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/model/int.go:21` | `json.Unmarshal` → `*string` | 存储/导入/请求成员；model codec | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/model/json.go:123` | `json.Decode` → `*json.RawMessage` | 存储/导入/请求成员；model codec | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/model/record.go:516` | `json.Unmarshal` → `*[]json.RawMessage` | 存储/导入/请求成员；model codec | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/model/record.go:537` | `json.Unmarshal` → `any` | 存储/导入/请求成员；model codec | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/model/settings.go:234` | `json.Unmarshal` → `*string` | 存储/导入/请求成员；model codec | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/model/string.go:23` | `json.Unmarshal` → `*json.Number` | 存储/导入/请求成员；model codec | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/model/string.go:30` | `json.Unmarshal` → `*string` | 存储/导入/请求成员；model codec | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/model/textlines.go:32` | `json.Unmarshal` → `*string` | 存储/导入/请求成员；model codec | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/model/textlines.go:37` | `json.Unmarshal` → `*[]string` | 存储/导入/请求成员；model codec | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rulesetfmt/rulesetfmt.go:86` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rulesetfmt/rulesetfmt.go:164` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/format_legacy.go:38` | `json.Unmarshal` → `any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/rules/groups.go:291` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/groups.go:324` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/groups.go:562` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/groups.go:569` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/groups.go:597` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/groups.go:602` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/groups.go:638` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/groups.go:643` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/groups.go:647` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/groups.go:647` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/groups.go:699` | `json.Unmarshal` → `*model.ProxyGroup` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/groups.go:707` | `json.Unmarshal` → `*[]string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/groups.go:746` | `json.Unmarshal` → `*model.ProxyGroup` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/matchercache.go:573` | `json.Unmarshal` → `*rules.MatcherInput` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：受控 typed/内置模板 round-trip（详见明确例外）；防御性统一 facade |
| `internal/rules/order_request.go:15` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/order_request.go:20` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/order_request.go:25` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/order_request.go:31` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/ruleutils.go:62` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/ruleutils.go:126` | `json.Unmarshal` → `*[]model.RuleConfig` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/ruleutils.go:250` | `json.Unmarshal` → `*model.RuleConfig` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/ruleutils.go:271` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/ruleutils.go:280` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/ruleutils.go:288` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/ruleutils.go:339` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/ruleutils.go:364` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rules/ruleutils.go:374` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rules/ruleutils.go:374` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/health/check/healthcheck.go:109` | `json.Unmarshal` → `*check.StrictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/healthcheck.go:155` | `json.Unmarshal` → `*check.Text` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/healthcheck.go:188` | `json.Unmarshal` → `*check.rawJSONObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/healthcheck.go:250` | `json.Unmarshal` → `*check.rawJSONObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/healthcheck.go:258` | `json.Unmarshal` → `*check.rawJSONObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/healthcheck.go:305` | `json.Unmarshal` → `*check.StrictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/healthcheck.go:318` | `json.Unmarshal` → `*check.StrictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/healthcheck.go:359` | `json.Unmarshal` → `*model.ProviderHealthCheck` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/legacy.go:30` | `json.Unmarshal` → `*T` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/health/check/probe.go:90` | `model.DecodeRecord` → `*check.PathStats` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/health/check/probe.go:260` | `model.DecodeRecord` → `*check.Suggestion` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/health/check/probe.go:296` | `model.DecodeRecord` → `*check.HostProbe` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/health/check/probe.go:314` | `json.Unmarshal` → `*check.rawJSONObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/probe.go:318` | `json.Unmarshal` → `*check.Suggestion` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/probe.go:375` | `model.DecodeRecord` → `*check.TimedRecord` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/health/check/probe.go:390` | `model.DecodeRecord` → `*check.probeFile` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/health/check/probe.go:429` | `json.Unmarshal` → `*check.probeFile` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/probe.go:441` | `json.Unmarshal` → `*model.Map[check.TimedRecord]` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/probe.go:458` | `json.Unmarshal` → `*check.rawJSONObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/probe.go:467` | `json.Unmarshal` → `*check.StrictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:171` | `json.Unmarshal` → `*check.rawJSONObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:185` | `json.Unmarshal` → `*model.Int` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:191` | `json.Unmarshal` → `*check.Text` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:197` | `json.Unmarshal` → `*[]check.Text` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:206` | `json.Unmarshal` → `*model.Map[check.NullableText]` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:212` | `json.Unmarshal` → `*check.StrictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:732` | `model.DecodeRecord` → `*check.TargetState` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/health/check/region.go:790` | `json.Unmarshal` → `*check.rawJSONObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:804` | `json.Unmarshal` → `*check.StrictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:911` | `model.DecodeRecord` → `*check.RegionData` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/health/check/region.go:953` | `json.Unmarshal` → `*check.RegionData` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:980` | `json.Unmarshal` → `*check.rawJSONObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/region.go:1015` | `json.Unmarshal` → `*check.RegionData` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/values.go:58` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/health/check/values.go:75` | `json.Unmarshal` → `*model.String` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/check/values.go:120` | `model.DecodeRecord` → `*check.rawJSONObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/chain_lower.go:76` | `json.Unmarshal` → `**model.ResourceRef` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/chain_lower.go:184` | `json.Unmarshal` → `*struct{ID json.RawMessage "json:\"id\""; Name json.RawMessage "json:\"name\""; Enabled json.RawMessage "json:\"enabled\""}` | 订阅内容、缓存、已存配置、Agent 状态 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/subs/chain_lower.go:368` | `json.Unmarshal` → `*model.ProxyGroup` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/chain_providers.go:20` | `model.DecodeRecord` → `*subs.chainOriginal` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/chain_providers.go:32` | `model.DecodeRecord` → `*subs.chainFilters` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/chain_providers.go:45` | `json.Unmarshal` → `*subs.chainFilters` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/chain_providers.go:173` | `json.Unmarshal` → `*model.Map[subs.chainOriginal]` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/chain_providers.go:223` | `json.Unmarshal` → `*model.String` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/core_status.go:22` | `model.DecodeRecord` → `*subs.CoreAgent` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/core_status.go:32` | `model.DecodeRecord` → `*subs.CoreStatus` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/core_status.go:77` | `json.Unmarshal` → `*subs.CoreStatus` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/dialer_extract.go:41` | `json.Unmarshal` → `*model.String` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/dialer_extract.go:141` | `json.Unmarshal` → `*model.String` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/dialer_extract.go:168` | `json.Unmarshal` → `*subs.DialersData` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/dialer_groups.go:60` | `json.Unmarshal` → `*struct{Dialers json.RawMessage "json:\"dialers\""}` | 订阅内容、缓存、已存配置、Agent 状态 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/subs/filter.go:102` | `json.Unmarshal` → `*model.String` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/host_extract.go:171` | `json.Unmarshal` → `*subs.HostsData` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/host_index.go:20` | `model.DecodeRecord` → `*subs.SourceMetadata` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/host_index.go:38` | `model.DecodeRecord` → `*subs.sourceCachePayload` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/host_index.go:45` | `json.Unmarshal` → `*subs.sourceCachePayload` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/host_index.go:50` | `json.Unmarshal` → `*[]json.RawMessage` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/host_index.go:55` | `json.Unmarshal` → `*model.Node` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/host_index.go:88` | `json.Unmarshal` → `*struct{Entries json.RawMessage "json:\"entries\""}` | 订阅内容、缓存、已存配置、Agent 状态 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/subs/host_merge.go:212` | `json.Unmarshal` → `*model.String` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/legacy.go:18` | `json.Unmarshal` → `*T` | 订阅内容、缓存、已存配置、Agent 状态 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/subs/parser.go:123` | `json.Unmarshal` → `*model.Node` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/parser.go:157` | `json.Unmarshal` → `*model.Node` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/parser.go:303` | `model.DecodeRecord` → `*subs.vmessFields` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/parser.go:322` | `json.Unmarshal` → `*subs.vmessWire` | 订阅内容、缓存、已存配置、Agent 状态 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/subs/parser.go:336` | `json.Unmarshal` → `*subs.vmessFields` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/parser.go:415` | `json.Unmarshal` → `*model.Node` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/proxyserverdns.go:345` | `model.DecodeRecord` → `*subs.dnsSettings` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/proxyserverdns.go:373` | `json.Unmarshal` → `*subs.dnsSettings` | 订阅内容、缓存、已存配置、Agent 状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/subs/records.go:16` | `model.DecodeRecord` → `*subs.HostSkip` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/records.go:32` | `model.DecodeRecord` → `*subs.HostsData` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/records.go:54` | `model.DecodeRecord` → `*subs.DialerTarget` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/subs/records.go:68` | `model.DecodeRecord` → `*subs.DialersData` | 订阅内容、缓存、已存配置、Agent 状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/convert/client_nodes.go:122` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/client_values.go:29` | `json.Unmarshal` → `*[]*model.String` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/client_values.go:43` | `json.Unmarshal` → `*model.String` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/client_values.go:72` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/convert/client_values.go:74` | `json.Unmarshal` → `*[]json.Number` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/convert/client_values.go:157` | `model.DecodeRecord` → `*convert.clientOptions` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/convert/client_values.go:169` | `model.DecodeRecord` → `*convert.clientTransportOptions` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/convert/client_values.go:179` | `model.DecodeRecord` → `*convert.clientHeaders` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/convert/client_values.go:192` | `model.DecodeRecord` → `*convert.clientPluginOptions` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/convert/client_values.go:203` | `model.DecodeRecord` → `*convert.clientReality` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/convert/client_values.go:215` | `json.Unmarshal` → `*convert.clientOptions` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/client_values.go:219` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/client_values.go:253` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/convert/client_values.go:257` | `json.Unmarshal` → `*[]*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/convert/common.go:81` | `json.Unmarshal` → `*model.RuleConfig` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/common.go:108` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/dialers.go:120` | `json.Unmarshal` → `**string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/convert/dialers.go:137` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/dialers.go:149` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/dialers.go:155` | `json.Unmarshal` → `**model.String` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/dialers.go:166` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/dialers.go:184` | `json.Unmarshal` → `*[]*model.String` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/dialers.go:196` | `json.Unmarshal` → `*[]map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/dialers.go:210` | `json.Unmarshal` → `**model.String` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/dialers.go:326` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/dialers.go:462` | `json.Unmarshal` → `*struct{Entry json.RawMessage "json:\"entry\""; Exit json.RawMessage "json:\"exit\""}` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/convert/dialers_api.go:112` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/convert/easytier.go:82` | `json.Unmarshal` → `*model.EasyTierSettings` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/easytier.go:206` | `json.Unmarshal` → `*model.Int` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/easytier.go:349` | `json.Unmarshal` → `*model.Int` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/easytier.go:411` | `json.Unmarshal` → `*model.EasyTierSettings` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/easytier.go:463` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/convert/easytier.go:590` | `json.Unmarshal` → `*model.EasyTierSettings` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/legacy.go:30` | `json.Unmarshal` → `*T` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/convert/legacy.go:483` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/mihomo_generate.go:210` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/mihomo_generate.go:215` | `json.Unmarshal` → `*model.RuleConfig` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/mihomo_generate.go:241` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/mihomo_generate.go:301` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/convert/mihomo_generate.go:331` | `json.Unmarshal` → `*model.ResourceRef` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/mihomo_generate.go:782` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/convert/mihomo_nodes.go:435` | `json.Unmarshal` → `*struct{Type json.RawMessage "json:\"type\""}` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/convert/subscription_environment.go:40` | `json.Unmarshal` → `*subs.SubscriptionCache` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/convert/subscription_environment.go:79` | `json.Unmarshal` → `*subs.HostReport` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/convert/subscription_environment.go:96` | `json.Unmarshal` → `*subs.SubscriptionCache` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/devicenotes/notes.go:59` | `json.Decode` → `*json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/devicenotes/notes.go:73` | `json.Decode` → `*json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/devicenotes/notes.go:86` | `json.Unmarshal` → `**string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/devicenotes/policies.go:23` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/devicenotes/policies.go:30` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/devicenotes/policies.go:39` | `json.Unmarshal` → `**string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/devicenotes/policies.go:53` | `json.Unmarshal` → `**string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/devicenotes/policies.go:63` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routerdoc/bindings.go:22` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routerdoc/bindings.go:27` | `json.Unmarshal` → `*map[string]*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/store/conn.go:187` | `json.Unmarshal` → `*int64` | 已存配置/导入文档/JSON 桥接 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/store/conn.go:199` | `json.Unmarshal` → `*string` | 已存配置/导入文档/JSON 桥接 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/store/conn.go:217` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/store/errors.go:138` | `json.Unmarshal` → `*json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/importexport.go:233` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/legacy.go:263` | `json.Unmarshal` → `*model.Node` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/legacy.go:382` | `json.Unmarshal` → `any` | 已存配置/导入文档/JSON 桥接 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/repo/load.go:27` | `json.Unmarshal` → `*model.Int` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/profiles.go:76` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/profiles.go:400` | `json.Unmarshal` → `*model.Profile` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/profiles.go:969` | `json.Unmarshal` → `*jsonx.Object` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/read.go:98` | `json.Unmarshal` → `*T` | 已存配置/导入文档/JSON 桥接 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/repo/resolve.go:388` | `json.Unmarshal` → `*T` | 已存配置/导入文档/JSON 桥接 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/repo/resolve.go:449` | `json.Unmarshal` → `*model.RuleConfig` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/router_settings.go:51` | `json.Unmarshal` → `*model.Router` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/shared.go:135` | `json.Unmarshal` → `*model.RuleLibraryItem` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/system.go:67` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:66` | `json.Unmarshal` → `*string` | 已存配置/导入文档/JSON 桥接 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/repo/validation.go:88` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:92` | `json.Unmarshal` → `*map[string]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:115` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:144` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:178` | `json.Unmarshal` → `*json.Number` | 已存配置/导入文档/JSON 桥接 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/repo/validation.go:211` | `json.Unmarshal` → `*model.Int` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:475` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:496` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:618` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:652` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/repo/validation.go:724` | `json.Unmarshal` → `*[]json.RawMessage` | 已存配置/导入文档/JSON 桥接 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/taskstatus/count.go:28` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/taskstatus/count.go:41` | `json.Unmarshal` → `*json.Number` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/taskstatus/legacy.go:24` | `json.Unmarshal` → `*jsonx.Object` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/taskstatus/legacy.go:39` | `json.Unmarshal` → `*jsonx.Object` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/taskstatus/taskstatus.go:119` | `model.DecodeRecord` → `*taskstatus.Receipt` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/taskstatus/taskstatus.go:127` | `json.Unmarshal` → `*taskstatus.Receipt` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/tasks/legacy.go:26` | `json.Unmarshal` → `*jsonx.Object` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/tasks/legacy.go:65` | `json.Unmarshal` → `*tasks.Task` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/tasks/legacy.go:99` | `json.Unmarshal` → `**int64` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/tasks/legacy.go:123` | `json.Unmarshal` → `*taskstatus.Receipt` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/tasks/legacy.go:150` | `json.Unmarshal` → `*tasks.Task` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/tasks/row.go:37` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/tasks/row.go:46` | `json.Unmarshal` → `*tasks.HistoryData` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/tasks/row.go:92` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/tasks/row.go:99` | `json.Unmarshal` → `*tasks.taskWire` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/tasks/row.go:103` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/tasks/row.go:111` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/tasks/row.go:129` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/legacy.go:22` | `json.Unmarshal` → `*T` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/notifications/store.go:57` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/store.go:229` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/notifications/store.go:240` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/store.go:256` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/store.go:266` | `json.Unmarshal` → `*notifications.Item` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/store.go:287` | `json.Unmarshal` → `*notifications.Item` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/types.go:23` | `model.DecodeRecord` → `*notifications.Navigation` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/notifications/types.go:51` | `json.Unmarshal` → `*model.Int` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/types.go:96` | `model.DecodeRecord` → `*notifications.Item` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/notifications/types.go:107` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/types.go:118` | `json.Unmarshal` → `*notifications.Item` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/types.go:138` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/types.go:145` | `json.Unmarshal` → `*notifications.Items` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/types.go:148` | `model.DecodeRecord` → `*notifications.storeDocument` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/notifications/types.go:193` | `model.DecodeRecord` → `*notifications.RouterCheck` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/notifications/types.go:206` | `model.DecodeRecord` → `*notifications.RouteHints` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/notifications/types.go:227` | `model.DecodeRecord` → `*notifications.RouterReport` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/notifications/types.go:231` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/notifications/types.go:251` | `model.DecodeRecord` → `*notifications.RouterSummary` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/redact/fields.go:81` | `json.Unmarshal` → `*any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/redact/fields.go:93` | `json.Decode` → `*json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/redact/fields.go:102` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/redact/fields.go:114` | `json.Decode` → `*any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ha_model.go:83` | `json.Unmarshal` → `*struct{GroupID string "json:\"group_id\""; PeerID string "json:\"peer_id\""}` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/ha_model.go:99` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/prepared.go:23` | `json.Unmarshal` → `*struct{ProfileID string "json:\"profile_id\""; ServiceType string "json:\"service_type\""}` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/prepared.go:27` | `json.Unmarshal` → `*model.PreparedContent` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/prepared.go:36` | `json.Unmarshal` → `*model.OxiDNSValidationReceipt` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/prepared.go:89` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/prepared.go:107` | `json.Unmarshal` → `*struct{ProfileID json.RawMessage "json:\"profile_id\""}` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/public_metadata_legacy.go:16` | `json.Unmarshal` → `**jsonx.Object` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/registry.go:80` | `json.Unmarshal` → `*model.Agent` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/registry.go:285` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/registry.go:315` | `json.Unmarshal` → `*struct{Name string "json:\"name\""; Error string "json:\"error\""}` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/runtime_legacy.go:26` | `json.Unmarshal` → `*model.Agent` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/runtime_state.go:190` | `json.Unmarshal` → `*model.Fields` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/agentfake/agentfake.go:120` | `json.Unmarshal` → `*[]agentfake.Call` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/agents/agentfake/agentfake.go:236` | `json.Unmarshal` → `*struct{Content string "json:\"content\""}` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/agents/agentfake/agentfake.go:446` | `json.Unmarshal` → `*map[string]json.RawMessage` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/agents/agentfake/agentfake.go:454` | `json.Unmarshal` → `*agentfake.Manifest` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/agents/agentfake/ha.go:88` | `json.Unmarshal` → `*map[string]json.RawMessage` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/agents/agentfake/ha.go:99` | `json.Unmarshal` → `*struct{Priority int "json:\"priority\""}` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/agents/agentfake/ha.go:119` | `json.Unmarshal` → `*struct{ID string "json:\"operation_id\""}` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/agents/agentfake/ha.go:206` | `json.Unmarshal` → `*[]agentfake.HACall` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/agents/agentfake/cmd/agentfake/main.go:24` | `json.Unmarshal` → `*agentfake.Options` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/httpx/bind.go:89` | `json.Decode` → `any` | HTTP 请求/响应 JSON | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/httpx/number.go:69` | `json.Unmarshal` → `*string` | HTTP 请求/响应 JSON | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/httpx/sanitize.go:78` | `json.Decode` → `*any` | HTTP 请求/响应 JSON | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/httpx/sanitize.go:130` | `json.Decode` → `*map[string]any` | HTTP 请求/响应 JSON | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/auth/auth.go:189` | `json.Unmarshal` → `*any` | JWT/认证请求/配置 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/auth/claims.go:23` | `json.Unmarshal` → `*map[string]json.RawMessage` | JWT/认证请求/配置 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/auth/claims.go:54` | `json.Unmarshal` → `*string` | JWT/认证请求/配置 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/auth/jwt.go:78` | `json.Unmarshal` → `*map[string]json.RawMessage` | JWT/认证请求/配置 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/auth/jwt.go:109` | `json.Unmarshal` → `**auth.Claims` | JWT/认证请求/配置 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/auth/jwt.go:185` | `json.Decode` → `*any` | JWT/认证请求/配置 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/auth/jwt.go:246` | `json.Decode` → `*any` | JWT/认证请求/配置 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rulefetch/cache_legacy.go:19` | `json.Unmarshal` → `any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/rulefetch/content.go:582` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rulefetch/content.go:589` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rulefetch/content.go:611` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rulefetch/content.go:635` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rulefetch/content.go:636` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rulefetch/publication.go:409` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rulefetch/publication.go:409` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/rulefetch/website.go:138` | `json.Unmarshal` → `*float64` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rulefetch/website.go:142` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rulefetch/website.go:156` | `json.Unmarshal` → `*bool` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rulefetch/website.go:157` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/rulefetch/website.go:164` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/substore/cache.go:339` | `json.Unmarshal` → `*struct{Nodes json.RawMessage "json:\"nodes\""; Count json.RawMessage "json:\"count\""}` | Sub-Store 回包、已存缓存/配置/状态 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/substore/cache.go:341` | `json.Unmarshal` → `*[]json.RawMessage` | Sub-Store 回包、已存缓存/配置/状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/substore/dependencies.go:172` | `json.Unmarshal` → `*substore.releasePayload` | Sub-Store 回包、已存缓存/配置/状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/substore/dependency_records.go:70` | `model.DecodeRecord` → `*substore.releasePayload` | Sub-Store 回包、已存缓存/配置/状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/substore/dependency_records.go:81` | `model.DecodeRecord` → `*substore.releaseAsset` | Sub-Store 回包、已存缓存/配置/状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/substore/env.go:280` | `json.Unmarshal` → `*struct{Data json.RawMessage "json:\"data\""; Version model.String "json:\"version\""}` | Sub-Store 回包、已存缓存/配置/状态 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/substore/env.go:287` | `json.Unmarshal` → `*struct{Version model.String "json:\"version\""}` | Sub-Store 回包、已存缓存/配置/状态 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/substore/httpclient.go:235` | `json.Unmarshal` → `*json.RawMessage` | Sub-Store 回包、已存缓存/配置/状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/substore/legacy.go:19` | `json.Unmarshal` → `*T` | Sub-Store 回包、已存缓存/配置/状态 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/substore/legacy.go:49` | `json.Unmarshal` → `*struct{Nodes []json.RawMessage "json:\"nodes\""}` | Sub-Store 回包、已存缓存/配置/状态 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/substore/proxies.go:89` | `json.Unmarshal` → `*model.Node` | Sub-Store 回包、已存缓存/配置/状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/substore/proxies.go:139` | `json.Unmarshal` → `*model.Node` | Sub-Store 回包、已存缓存/配置/状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/substore/records.go:20` | `model.DecodeRecord` → `*substore.Traffic` | Sub-Store 回包、已存缓存/配置/状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/substore/records.go:39` | `model.DecodeRecord` → `*substore.CacheMetadata` | Sub-Store 回包、已存缓存/配置/状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/substore/records.go:56` | `model.DecodeRecord` → `*substore.Cache` | Sub-Store 回包、已存缓存/配置/状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/substore/runtime_state.go:36` | `model.DecodeRecord` → `*substore.runtimeState` | Sub-Store 回包、已存缓存/配置/状态 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/substore/runtime_state.go:79` | `json.Unmarshal` → `*substore.runtimeState` | Sub-Store 回包、已存缓存/配置/状态 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/substore/runtime_state.go:85` | `json.Unmarshal` → `*struct{LastUsed json.RawMessage "json:\"last_used\""}` | Sub-Store 回包、已存缓存/配置/状态 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/refresh/health.go:82` | `model.DecodeRecord` → `*refresh.FetchEvent` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/refresh/health.go:94` | `model.DecodeRecord` → `*refresh.HealthEntry` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/refresh/health.go:103` | `json.Unmarshal` → `*refresh.HealthEntry` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/refresh/health.go:123` | `json.Unmarshal` → `*refresh.HealthSnapshot` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/refresh/interval.go:52` | `json.Unmarshal` → `*int64` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/refresh/owners_legacy.go:25` | `json.Unmarshal` → `any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/refresh/source.go:34` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/refresh/source.go:51` | `json.Unmarshal` → `*refresh.SourceMetadata` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/refresh/source.go:168` | `model.DecodeRecord` → `*refresh.TransportReceipt` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/refresh/source.go:199` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/refresh/source.go:267` | `json.Unmarshal` → `*refresh.TransportReceipt` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/refresh/types.go:22` | `model.DecodeRecord` → `*refresh.Traffic` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/refresh/types.go:43` | `model.DecodeRecord` → `*refresh.SourceMetadata` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/refresh/types.go:56` | `model.DecodeRecord` → `*refresh.NodeCache` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/mosdns/archive.go:157` | `json.Unmarshal` → `*model.Fields` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/archive.go:168` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mosdns/archive.go:179` | `json.Unmarshal` → `*bool` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mosdns/credentials.go:22` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/credentials.go:25` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mosdns/legacy.go:22` | `json.Unmarshal` → `*model.Fields` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/legacy.go:38` | `json.Unmarshal` → `*model.Profile` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/legacy.go:52` | `json.Unmarshal` → `*model.System` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/legacy.go:67` | `json.Unmarshal` → `*mosdns.profileIdentity` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/legacy.go:82` | `json.Unmarshal` → `*model.Shared` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/legacy.go:226` | `json.Unmarshal` → `*model.MosDNSSettings` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/legacy.go:258` | `json.Unmarshal` → `*model.RuleConfig` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/legacy.go:306` | `json.Unmarshal` → `any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/mosdns/legacy.go:377` | `json.Unmarshal` → `*model.RuleLibraryItem` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/legacy.go:480` | `json.Unmarshal` → `*model.Fields` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mosdns/legacy.go:514` | `model.DecodeRecord` → `*mosdns.profileIdentity` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/mosdns/native_layout.go:38` | `model.DecodeRecord` → `*mosdns.ruleLists` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/mosdns/native_layout.go:89` | `json.Unmarshal` → `*mosdns.ruleLists` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/oxidns/adapter.go:202` | `json.Unmarshal` → `*model.Fields` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/oxidns/adapter.go:218` | `json.Unmarshal` → `*model.Profile` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/oxidns/adapter.go:232` | `json.Unmarshal` → `*oxidns.profileIdentity` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/oxidns/adapter.go:247` | `json.Unmarshal` → `*model.System` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/oxidns/adapter.go:262` | `json.Unmarshal` → `*model.Shared` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/oxidns/adapter.go:277` | `json.Unmarshal` → `*model.System` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/oxidns/adapter.go:297` | `json.Unmarshal` → `*model.Shared` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/oxidns/adapter.go:367` | `json.Unmarshal` → `*model.MosDNSSettings` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/oxidns/adapter.go:449` | `model.DecodeRecord` → `*oxidns.profileIdentity` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/oxidns/devicenotes.go:144` | `json.Unmarshal` → `*[]devicenotes.Policy` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/oxidns/errors.go:107` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/oxidns/errors.go:110` | `json.Unmarshal` → `*int` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/oxidns/node.go:326` | `json.Decode` → `*any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/oxidns/validation.go:63` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/oxidns/validation.go:67` | `json.Unmarshal` → `*[]string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/oxidns/validation.go:87` | `json.Unmarshal` → `*oxidns.Schema` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/oxidns/validation.go:123` | `json.Unmarshal` → `any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/configgen/generate.go:224` | `json.Unmarshal` → `*[]mosdns.Download` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：受控 typed/内置模板 round-trip（详见明确例外）；防御性统一 facade |
| `internal/configgen/request.go:21` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/configgen/request.go:33` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/configgen/request.go:44` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/configgen/request.go:50` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/configgen/request.go:55` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/configgen/request.go:60` | `json.Unmarshal` → `*json.Number` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routersplit/legacy.go:22` | `json.Unmarshal` → `*jsonx.Object` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/legacy.go:35` | `json.Unmarshal` → `*model.Profile` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/legacy.go:42` | `json.Unmarshal` → `*routersplit.Context` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/legacy.go:67` | `json.Unmarshal` → `*jsonx.Object` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/legacy.go:80` | `json.Unmarshal` → `*jsonx.Object` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/legacy.go:199` | `json.Unmarshal` → `*struct{InstallationID *string "json:\"gateway_installation_id\""}` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routersplit/legacy.go:251` | `json.Unmarshal` → `*[]routersplit.Session` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/legacy.go:268` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/legacy.go:272` | `json.Unmarshal` → `*[]map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/legacy.go:280` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routersplit/legacy.go:283` | `json.Unmarshal` → `**string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routersplit/legacy.go:300` | `model.DecodeRecord` → `*struct{Agents []model.Agent "json:\"agents\""; Raw model.Raw}` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/routersplit/legacy.go:331` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routersplit/routersplit.go:90` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/routersplit.go:90` | `json.Unmarshal` → `*routersplit.Marker` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routersplit/routersplit.go:121` | `model.DecodeRecord` → `*struct{Enabled bool "json:\"enabled\""; Raw model.Raw}` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/routersplit/routersplit.go:200` | `model.DecodeRecord` → `*routersplit.Context` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/routersplit/routersplit.go:204` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/routersplit.go:209` | `json.Unmarshal` → `*[]*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routersplit/routersplit.go:223` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routersplit/routersplit.go:243` | `model.DecodeRecord` → `*routersplit.Session` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/routersplit/routersplit.go:247` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/routersplit.go:250` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routersplit/routersplit.go:386` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routersplit/routersplit.go:390` | `json.Unmarshal` → `**string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routersplit/routersplit.go:393` | `json.Unmarshal` → `**string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/settings.go:44` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/settings.go:48` | `json.Unmarshal` → `*bool` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/settings.go:75` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/settings.go:82` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/provider/delivery.go:324` | `json.Unmarshal` → `*subs.SourceMetadata` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/provider/delivery.go:342` | `json.Unmarshal` → `*struct{Text string "json:\"text\""}` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/provider/delivery.go:443` | `json.Unmarshal` → `*substore.CacheMetadata` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/provider/delivery.go:519` | `json.Unmarshal` → `*model.Fields` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/provider/delivery.go:1017` | `json.Unmarshal` → `*model.Fields` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/provider/delivery.go:1042` | `json.Unmarshal` → `*model.Node` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/provider/hooks.go:163` | `json.Unmarshal` → `*refresh.Traffic` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/provider/legacy.go:25` | `json.Unmarshal` → `any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/setupflow/read.go:96` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setupflow/read.go:101` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setupflow/read.go:113` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setupflow/read.go:121` | `json.Unmarshal` → `*bool` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setupflow/read.go:127` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/adapter.go:234` | `json.Decode` → `*any` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/adapter.go:265` | `json.Unmarshal` → `*deploy.receiptAgentView` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/adapter.go:461` | `json.Unmarshal` → `*deploy.Capabilities` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/adapter.go:853` | `json.Unmarshal` → `*deploy.taskView` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/bundle.go:329` | `json.Unmarshal` → `*deploy.Manifest` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/capabilities.go:9` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/capabilities.go:15` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/capabilities.go:21` | `json.Unmarshal` → `*int` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/capabilities.go:30` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/capabilities.go:31` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/capabilities.go:32` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/capabilities.go:34` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/capabilities.go:40` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/content_adapter.go:145` | `json.Unmarshal` → `*struct{ConfigVersion json.RawMessage "json:\"config_version\""; ConfigRevision json.RawMessage "json:\"config_revision\""; Applied json.RawMessage "json:\"applied_content\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/content_adapter.go:150` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/content_adapter.go:153` | `json.Unmarshal` → `*deploy.AppliedContent` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/orchestrate.go:237` | `json.Unmarshal` → `*bool` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/orchestrate.go:278` | `json.Unmarshal` → `*bool` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/orchestrate.go:470` | `json.Unmarshal` → `*map[string]any` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/orchestrate.go:492` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/orchestrate.go:513` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/previews.go:81` | `json.Decode` → `*json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/previews.go:309` | `json.Unmarshal` → `*deploy.Preview` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/receipts.go:160` | `json.Unmarshal` → `*deploy.Receipt` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/types.go:208` | `json.Unmarshal` → `*struct{Path *string "json:\"path\""; Size *int64 "json:\"size\""; SHA256 *string "json:\"sha256\""; Role *deploy.Role "json:\"role\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/types.go:214` | `json.Unmarshal` → `*deploy.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/types.go:221` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/types.go:230` | `json.Unmarshal` → `*deploy.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/dnsfollow/adapter.go:37` | `json.Unmarshal` → `*model.Agent` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/adapter.go:174` | `json.Unmarshal` → `*model.Deployment` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/adapter.go:220` | `json.Unmarshal` → `*map[string]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/adapter.go:225` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsfollow/adapter.go:233` | `json.Unmarshal` → `*int` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsfollow/adapter.go:235` | `json.Unmarshal` → `*bool` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsfollow/adapter.go:237` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsfollow/adapter.go:262` | `json.Unmarshal` → `*dnsfollow.deviceReply` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/adapter.go:351` | `json.Unmarshal` → `*dnsfollow.OptionsView` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/dnsfollow/adapter.go:369` | `json.Unmarshal` → `*[]model.OperationEvent` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/document.go:85` | `json.Unmarshal` → `*struct{Tag *string "json:\"upstream_tag\""; Index *int64 "json:\"upstream_index\""}` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/dnsfollow/document.go:160` | `json.Unmarshal` → `*[]struct{ID string "json:\"id\""}` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/dnsfollow/document.go:177` | `json.Unmarshal` → `*model.Fields` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/document.go:192` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsfollow/document.go:200` | `json.Unmarshal` → `*int64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsfollow/document.go:200` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsfollow/document.go:209` | `json.Unmarshal` → `*model.DNSFollowBinding` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/publish.go:50` | `json.Unmarshal` → `*int` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsfollow/service.go:236` | `json.Unmarshal` → `*model.DNSFollowState` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/service.go:259` | `json.Unmarshal` → `*model.DNSFollowState` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/service.go:269` | `json.Unmarshal` → `*model.Fields` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsfollow/service.go:311` | `json.Unmarshal` → `*model.DNSFollowState` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/rulesync/adapter.go:31` | `json.Unmarshal` → `*model.Agent` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/rulesync/adapter.go:48` | `json.Unmarshal` → `*model.RuleSyncSettings` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/rulesync/adapter.go:103` | `json.Unmarshal` → `*model.RuleSyncState` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/rulesync/adapter.go:286` | `json.Unmarshal` → `*struct{Task rulesync.nativeTask "json:\"task\""}` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/rulesync/adapter.go:320` | `json.Unmarshal` → `*model.Deployment` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/rulesync/service.go:142` | `json.Unmarshal` → `*map[string]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/rulesync/service.go:152` | `json.Unmarshal` → `*rulesync.Settings` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/rulesync/service.go:356` | `json.Unmarshal` → `*map[string]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/rulesync/snapshot.go:116` | `json.Unmarshal` → `*rulesync.snapshotManifest` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/rulesync/snapshot.go:155` | `json.Unmarshal` → `*[]rulesync.snapshotFile` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/rulesync/snapshot.go:175` | `json.Unmarshal` → `**deploy.Role` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/rulesync/snapshot.go:176` | `json.Unmarshal` → `**int64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/rulesync/snapshot.go:177` | `json.Unmarshal` → `**string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsha/adapter.go:28` | `json.Unmarshal` → `*model.Agent` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/adapter.go:60` | `json.Unmarshal` → `*model.HAIdentity` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/adapter.go:207` | `json.Unmarshal` → `*map[string]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/adapter.go:216` | `json.Unmarshal` → `*model.HAPublicationSummary` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/adapter.go:223` | `json.Unmarshal` → `*model.Deployment` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/adapter.go:316` | `json.Unmarshal` → `*model.Profile` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/client.go:17` | `json.Unmarshal` → `*dnsha.object` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/client.go:22` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsha/client.go:28` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/client.go:35` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsha/client.go:46` | `json.Unmarshal` → `*dnsha.object` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/client.go:57` | `json.Unmarshal` → `*float64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsha/hooks.go:221` | `json.Unmarshal` → `*model.Deployment` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/payload.go:12` | `json.Unmarshal` → `*dnsha.plain` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/dnsha/payload.go:16` | `json.Unmarshal` → `*map[string]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/payload.go:36` | `json.Unmarshal` → `*map[string]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/status.go:177` | `json.Unmarshal` → `*int` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsha/status.go:228` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsha/storage.go:113` | `json.Unmarshal` → `*dnsha.payload` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/storage.go:188` | `json.Unmarshal` → `*model.HAJournal` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/storage.go:211` | `json.Unmarshal` → `*model.HAJournal` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/storage.go:312` | `json.Unmarshal` → `*int64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/dnsha/validation.go:30` | `json.Unmarshal` → `*dnsha.object` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/dnsha/validation.go:79` | `json.Unmarshal` → `*int` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/devhttp/devhttp.go:134` | `json.Decode` → `any` | Agent / RouterOS / iKuai HTTP 回包、设备 manifest | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/routers/addresslists.go:260` | `json.Unmarshal` → `*routers.rosManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/agent_return.go:43` | `json.Unmarshal` → `*model.Agent` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/agent_return.go:46` | `json.Unmarshal` → `*routers.DNSAgent` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/agent_return.go:78` | `json.Unmarshal` → `*routers.ReturnStatus` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/agent_return.go:88` | `json.Unmarshal` → `*agents.Record` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/desired.go:179` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/desired.go:179` | `json.Unmarshal` → `*[]string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/desired.go:197` | `json.Unmarshal` → `*routers.desiredProfile` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/desired.go:221` | `json.Unmarshal` → `*routers.LibraryRule` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/desired.go:228` | `json.Unmarshal` → `*routers.RuleSet` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/dns_guard_model.go:121` | `json.Unmarshal` → `**routers.DNSGuardManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/dns_guard_model.go:128` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/dns_guard_write.go:191` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/dns_guard_write.go:195` | `json.Unmarshal` → `*[]map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/dns_guard_write.go:412` | `json.Unmarshal` → `*routers.DNSGuardReplace` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/dns_guard_write.go:424` | `json.Unmarshal` → `*routers.DNSGuardUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/fixed_ip_ikuai_write.go:122` | `json.Unmarshal` → `*routers.fixedIKScope` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/fixed_ip_model.go:62` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/fixed_ip_read.go:250` | `json.Unmarshal` → `*routers.fixedROSScope` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/fixed_ip_read.go:450` | `json.Unmarshal` → `*routers.fixedIKScope` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ikuai.go:168` | `devhttp.JSON` → `*any` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ikuai.go:776` | `devhttp.DecodeJSON` → `*any` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ikuai_apply.go:27` | `devhttp.DecodeJSON` → `*any` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ikuai_apply.go:67` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ikuai_apply.go:533` | `devhttp.DecodeJSON` → `*routers.ikuaiOp` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ikuai_apply.go:960` | `devhttp.DecodeJSON` → `*routers.Row` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ikuai_apply.go:1181` | `devhttp.DecodeJSON` → `*routers.ikuaiOp` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ikuai_dhcp.go:255` | `devhttp.DecodeJSON` → `*any` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ikuai_dhcp.go:326` | `json.Unmarshal` → `*routers.ikuaiDHCPScope` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ikuai_dhcp.go:340` | `json.Unmarshal` → `*[]any` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/inspect.go:54` | `json.Unmarshal` → `*model.Profile` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/inspect.go:64` | `json.Unmarshal` → `*model.Router` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/inspect.go:268` | `json.Unmarshal` → `**routers.DNSAgent` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/inspect.go:289` | `json.Unmarshal` → `*struct{ProfileID string "json:\"profile_id\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/inspect.go:336` | `json.Unmarshal` → `*bool` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/inspect.go:337` | `json.Unmarshal` → `*[]string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/inspect.go:377` | `json.Unmarshal` → `*struct{Routers json.RawMessage "json:\"routers\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/inspect.go:377` | `json.Unmarshal` → `*struct{Items json.RawMessage "json:\"items\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/inspect.go:377` | `json.Unmarshal` → `*[]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/inspect.go:383` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/inspect.go:387` | `json.Unmarshal` → `*routers.Router` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_pd_model.go:463` | `json.Unmarshal` → `*routers.IPv6PDManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_pd_model.go:476` | `json.Unmarshal` → `*struct{Resources []struct{Config json.RawMessage "json:\"config\""} "json:\"resources\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_pd_plan.go:165` | `json.Unmarshal` → `*routers.IPv6PDManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_pd_plan.go:258` | `json.Unmarshal` → `*routers.IPv6PDManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_pd_write.go:82` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_pd_write.go:82` | `json.Unmarshal` → `*[]map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_pd_write.go:87` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_pd_write.go:88` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_pd_write.go:109` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_pd_write.go:114` | `json.Unmarshal` → `*[]map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_pd_write.go:120` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_pd_write.go:121` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_pd_write.go:122` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_pd_write.go:143` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_pd_write.go:149` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_pd_write.go:162` | `json.Unmarshal` → `*routers.IPv6PDManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_pd_write.go:237` | `json.Unmarshal` → `*routers.Row` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_pd_write.go:338` | `json.Unmarshal` → `*routers.IPv6PDManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_pd_write.go:420` | `json.Unmarshal` → `*routers.IPv6DNSUpdate` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_pd_write.go:431` | `json.Unmarshal` → `*routers.IPv6PDGuardSync` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_pd_write.go:442` | `json.Unmarshal` → `*routers.IPv6PDGuardUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_plan.go:104` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_plan.go:112` | `json.Unmarshal` → `*int` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_plan.go:112` | `devhttp.DecodeJSON` → `*[]routers.IPv6Resource` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_plan.go:117` | `json.Unmarshal` → `**routers.IPv6RouteSpec` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_plan.go:122` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_plan.go:313` | `json.Unmarshal` → `*routers.IPv6FakeIPManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_plan.go:584` | `json.Unmarshal` → `*routers.IPv6RoutesManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:121` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_write.go:121` | `json.Unmarshal` → `*[]map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_write.go:126` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_write.go:150` | `devhttp.DecodeJSON` → `*routers.ROSUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:315` | `json.Unmarshal` → `*routers.rosOperation` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:333` | `json.Unmarshal` → `*routers.rosOperation` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:578` | `json.Unmarshal` → `*routers.IPv6FakeIPManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:600` | `json.Unmarshal` → `*routers.IPv6RoutesManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:636` | `devhttp.DecodeJSON` → `*routers.IPv6FakeIPReplace` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:647` | `devhttp.DecodeJSON` → `*routers.IPv6FakeIPUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:657` | `devhttp.DecodeJSON` → `*routers.IPv6RoutesReplace` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:668` | `devhttp.DecodeJSON` → `*routers.IPv6RoutesUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/ipv6_write.go:707` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_write.go:707` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_write.go:707` | `json.Unmarshal` → `*[]map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_write.go:707` | `json.Unmarshal` → `*[]map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/ipv6_write.go:713` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_write.go:718` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/ipv6_write.go:753` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management.go:78` | `json.Unmarshal` → `*model.Profile` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management.go:91` | `json.Unmarshal` → `*model.Router` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management.go:179` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management.go:180` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management.go:257` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management.go:270` | `json.Unmarshal` → `*model.Profile` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management.go:283` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management.go:323` | `json.Unmarshal` → `*struct{Fingerprint string "json:\"fingerprint\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/management.go:376` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management.go:441` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/management_repo.go:29` | `json.Unmarshal` → `*model.Profile` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/management_repo.go:92` | `json.Unmarshal` → `*model.Fields` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/natlist_write.go:27` | `json.Unmarshal` → `*routers.NATListSync` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/natlist_write.go:38` | `json.Unmarshal` → `*routers.NATListUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/natlist_write.go:433` | `json.Unmarshal` → `*routers.OpHead` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/natlist_write.go:436` | `json.Unmarshal` → `*routers.NATListUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/pending_summary.go:126` | `json.Unmarshal` → `*routers.pendingReport` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/pending_summary.go:129` | `json.Unmarshal` → `*routers.pendingPlan` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan.go:40` | `json.Decode` → `*any` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/plan.go:66` | `json.Unmarshal` → `*routers.ReturnSpec` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan.go:107` | `json.Unmarshal` → `*routers.ReturnSpec` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan.go:210` | `json.Unmarshal` → `*routers.ROSPolicyManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan.go:383` | `json.Unmarshal` → `*routers.ReturnSpec` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan_nat.go:168` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/plan_return.go:66` | `json.Unmarshal` → `*routers.plain` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan_return.go:70` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/plan_return.go:85` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/plan_return.go:125` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/plan_return.go:134` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/plan_return.go:143` | `json.Unmarshal` → `*routers.ReturnAllocation` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan_return.go:157` | `json.Unmarshal` → `*[]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/plan_return.go:163` | `json.Unmarshal` → `*json.Number` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/plan_return.go:178` | `json.Unmarshal` → `*int` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/plan_return.go:183` | `json.Unmarshal` → `*bool` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/plan_return.go:183` | `json.Unmarshal` → `*[]string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/plan_return.go:227` | `json.Unmarshal` → `*routers.ReturnSpec` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan_return.go:263` | `json.Unmarshal` → `*struct{Priority json.RawMessage; Table json.RawMessage; Mark string}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan_return.go:269` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/plan_return.go:310` | `json.Unmarshal` → `*routers.plain` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/plan_return.go:314` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/plan_return.go:325` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/publish.go:399` | `json.Unmarshal` → `*routers.Router` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/release.go:72` | `json.Unmarshal` → `*routers.IPv6PDGuardCheck` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/release.go:117` | `json.Unmarshal` → `*routers.ReturnSpec` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/release.go:222` | `json.Unmarshal` → `*struct{Kind string "json:\"kind\""; Spec json.RawMessage "json:\"spec\""; Previous json.RawMessage "json:\"previous\""; RestoreLegacy bool "json:\"restore_legacy\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/release.go:250` | `json.Unmarshal` → `*routers.ReturnSpec` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/release.go:250` | `json.Unmarshal` → `*struct{AdoptLegacy bool "json:\"adopt_legacy\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/release.go:292` | `json.Unmarshal` → `*routers.ReturnSpec` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/release.go:427` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/release.go:594` | `json.Unmarshal` → `*struct{Enabled bool "json:\"enabled\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/release.go:631` | `json.Unmarshal` → `*struct{ProxyDst []string "json:\"proxy_dst\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/release.go:632` | `json.Unmarshal` → `*struct{ProxyDst []string "json:\"proxy_dst\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/release.go:637` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/release.go:637` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/release_model.go:171` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/release_model.go:204` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/release_model.go:214` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/report.go:203` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rospolicy_model.go:156` | `devhttp.DecodeJSON` → `*any` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/rospolicy_plan.go:256` | `json.Unmarshal` → `*struct{Spec json.RawMessage "json:\"spec\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rospolicy_plan.go:274` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rospolicy_plan.go:370` | `devhttp.DecodeJSON` → `*routers.Row` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/rospolicy_plan.go:370` | `devhttp.DecodeJSON` → `*routers.Row` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/rospolicy_read.go:24` | `devhttp.DecodeJSON` → `*routers.ROSPolicyManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rospolicy_write.go:45` | `devhttp.DecodeJSON` → `*routers.policyWrite` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rospolicy_write.go:56` | `devhttp.DecodeJSON` → `*routers.ROSPolicyUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rospolicy_write.go:77` | `devhttp.DecodeJSON` → `*routers.policyWrite` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rospolicy_write.go:468` | `devhttp.DecodeJSON` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/route_editor.go:152` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/route_editor.go:161` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/route_editor.go:165` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/route_editor.go:168` | `json.Unmarshal` → `*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/route_editor.go:188` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/route_editor.go:211` | `json.Unmarshal` → `*bool` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/router.go:87` | `json.Unmarshal` → `*model.Router` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:101` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:109` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:118` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:217` | `json.Unmarshal` → `*model.String` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:224` | `json.Unmarshal` → `**string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/router.go:233` | `json.Unmarshal` → `*[]*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/router.go:263` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:278` | `json.Unmarshal` → `*[]*string` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/router.go:308` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:333` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:342` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:347` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:395` | `json.Unmarshal` → `**int` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/router.go:465` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:468` | `json.Unmarshal` → `*[]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/router.go:472` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/routeros.go:170` | `devhttp.DecodeJSON` → `*routers.Row` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/routeros.go:189` | `devhttp.JSON` → `*any` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/routeros.go:242` | `devhttp.JSON` → `*any` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/routeros.go:832` | `json.Unmarshal` → `*routers.rosManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros.go:891` | `devhttp.DecodeJSON` → `*routers.rosManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros.go:1000` | `json.Unmarshal` → `*struct{Kind string "json:\"kind\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros.go:1013` | `json.Unmarshal` → `*routers.rosOperation` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros.go:1200` | `json.Unmarshal` → `*routers.Row` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/routeros.go:1212` | `json.Unmarshal` → `*routers.Row` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/routeros_batch.go:68` | `json.Unmarshal` → `*routers.rosOperation` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros_batch.go:107` | `json.Unmarshal` → `*routers.rosOperation` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros_batch.go:135` | `json.Unmarshal` → `*routers.ROSUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros_batch.go:170` | `json.Unmarshal` → `*routers.ROSUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros_batch.go:202` | `json.Unmarshal` → `*routers.ROSUndo` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros_batch.go:535` | `json.Unmarshal` → `*routers.rosOperation` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros_batch.go:647` | `json.Unmarshal` → `*routers.rosOperation` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/routeros_batch.go:844` | `json.Unmarshal` → `*routers.Row` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/rule_sync_adapter.go:47` | `json.Unmarshal` → `*struct{URL string "json:\"url\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_controls.go:13` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_controls.go:69` | `json.Unmarshal` → `*struct{Mihomo struct{CustomConfig string "json:\"custom_config\""} "json:\"mihomo\""; Oxidns json.RawMessage "json:\"oxidns\""; Groups json.RawMessage "json:\"proxy_groups\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_controls.go:83` | `json.Unmarshal` → `*model.Profile` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_controls.go:101` | `json.Unmarshal` → `*struct{HA struct{Enabled bool "json:\"enabled\""; GroupID json.RawMessage "json:\"group_id\""; VIP json.RawMessage "json:\"vip\""; PeerID string "json:\"peer_id\""} "json:\"oxidns_ha\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_json.go:13` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_json.go:27` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_json.go:38` | `json.Unmarshal` → `*routers.plain` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_json.go:49` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_json.go:53` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_json.go:75` | `json.Unmarshal` → `*routers.plain` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_json.go:101` | `json.Unmarshal` → `*routers.plain` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_json.go:139` | `json.Decode` → `*json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_rules.go:198` | `json.Unmarshal` → `*struct{Rules []json.RawMessage "json:\"rule_configs\""; Library []json.RawMessage "json:\"rule_library\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_rules.go:204` | `json.Unmarshal` → `*routers.RuleSet` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_rules.go:211` | `json.Unmarshal` → `*routers.LibraryRule` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_rules.go:244` | `json.Unmarshal` → `*struct{ID string "json:\"id\""; Name string "json:\"name\""; SourceType string "json:\"source_type\""; URL string "json:\"url\""; Behavior string "json:\"behavior\""; Content string "json:\"content\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_rules.go:253` | `json.Unmarshal` → `*struct{Behavior string "json:\"behavior\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_sources.go:32` | `json.Unmarshal` → `*struct{System json.RawMessage "json:\"system_config\""; Library []model.RuleLibraryItem "json:\"rule_library\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_sources.go:37` | `json.Unmarshal` → `*model.SystemConfig` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_sources.go:53` | `json.Unmarshal` → `*model.RuleConfig` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_store.go:142` | `json.Unmarshal` → `*routers.RuleSyncReceipt` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_write.go:228` | `json.Unmarshal` → `*routers.ROSPolicyManifest` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/rule_sync_write.go:436` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/rule_sync_write.go:438` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/stored_model.go:53` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/stored_model.go:120` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/stored_model.go:173` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/stored_model.go:177` | `json.Unmarshal` → `*struct{Enabled bool "json:\"enabled\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/routers/stored_model.go:204` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/wire.go:33` | `json.Unmarshal` → `*notifications.RouterSummary` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/wire.go:49` | `json.Unmarshal` → `*model.Router` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/wire.go:57` | `json.Unmarshal` → `*notifications.RouterReport` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/workspace.go:79` | `json.Unmarshal` → `*map[string]json.RawMessage` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/routers/workspace.go:131` | `json.Unmarshal` → `*struct{Rules []struct{routers.WorkspaceRule; ItemType string "json:\"itemType\""} "json:\"rule_configs\""}` | 路由器/Agent 回包、配置、已存 manifest/操作日志 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/clients/cache.go:137` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/devices.go:86` | `json.Unmarshal` → `*[]clients.Device` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/clients/devices.go:97` | `json.Unmarshal` → `*struct{Notes json.RawMessage "json:\"device_notes\""; Fixed json.RawMessage "json:\"client_fixed_ips\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/clients/devices.go:117` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/devices.go:141` | `json.Decode` → `*json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/devices.go:177` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/devices.go:193` | `json.Unmarshal` → `*struct{Policies json.RawMessage "json:\"device_policies\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/clients/devices.go:235` | `json.Unmarshal` → `*struct{Policies json.RawMessage "json:\"device_policies\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/clients/devices.go:254` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/discovery.go:235` | `json.Unmarshal` → `*clients.Discovery` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/clients/fixed.go:40` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/fixed.go:45` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/clients/fixed.go:45` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/fixed.go:53` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/fixed.go:63` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/clients/fixed.go:76` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/clients/fixed.go:183` | `json.Unmarshal` → `*struct{Bindings map[string]*routers.FixedIPBinding "json:\"client_fixed_ips\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/clients/fixed.go:371` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/fixed.go:372` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/fixed.go:377` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/clients/fixed.go:408` | `json.Unmarshal` → `*clients.fixedIPApproval` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/clients/fixed_repo.go:25` | `json.Unmarshal` → `*jsonx.Object` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/pushdiff/entries.go:108` | `json.Unmarshal` → `*pushdiff.Entry` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/pushdiff/json.go:83` | `json.Unmarshal` → `*pushdiff.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/pushdiff/json.go:87` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/pushdiff/text.go:140` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/pushdiff/text.go:147` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/deploy/pushdiff/text.go:153` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/pushdiff/text.go:161` | `json.Unmarshal` → `*struct{Secret *string "json:\"secret\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/deploy/pushdiff/text.go:264` | `json.Unmarshal` → `*any` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/deploy/contentsync/details.go:20` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/adapter.go:98` | `json.Unmarshal` → `*int64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/config.go:49` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/config.go:115` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/config.go:142` | `json.Unmarshal` → `*int` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/evidence.go:125` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/evidence.go:136` | `json.Unmarshal` → `*any` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/evidence.go:261` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/evidence.go:282` | `json.Unmarshal` → `*[]string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/execute.go:21` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/execute.go:46` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/execute.go:129` | `json.Unmarshal` → `**int64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/jobs.go:41` | `json.Unmarshal` → `*int64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/jobs.go:105` | `json.Unmarshal` → `*int64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/jobs.go:289` | `json.Unmarshal` → `*[]ipv6.Check` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/ipv6/public.go:49` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/readiness.go:62` | `json.Unmarshal` → `*ipv6.lastCheck` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/ipv6/readiness.go:272` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/readiness.go:326` | `json.Unmarshal` → `*ipv6.ProbeResult` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/ipv6/service.go:130` | `json.Unmarshal` → `*ipv6.Report` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/ipv6/service.go:144` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/service.go:210` | `json.Unmarshal` → `*[]ipv6.Document` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/service.go:217` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/service.go:235` | `json.Unmarshal` → `*model.Agent` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/service.go:246` | `json.Unmarshal` → `*routers.IPv6Host` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/ipv6/service.go:328` | `json.Unmarshal` → `*routers.IPv6Host` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/ipv6/types.go:79` | `json.Unmarshal` → `*ipv6.Document` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/types.go:80` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/types.go:112` | `json.Unmarshal` → `*routers.IPv6Readiness` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/ipv6/verification.go:69` | `json.Unmarshal` → `*model.Map[json.RawMessage]` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/verification.go:77` | `json.Unmarshal` → `*float64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/verification.go:129` | `json.Unmarshal` → `*float64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/ipv6/verification.go:161` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/verification.go:229` | `json.Unmarshal` → `*ipv6.Settings` | Agent 回包/上报、已存配置/任务/状态文件 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/agents/ipv6/wire.go:38` | `json.Unmarshal` → `*model.Map[json.RawMessage]` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/ipv6/wire.go:61` | `json.Unmarshal` → `**int64` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/health/conversion.go:22` | `json.Unmarshal` → `*model.Node` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/conversion.go:26` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/conversion.go:53` | `json.Unmarshal` → `*model.Fields` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/dns.go:210` | `json.Unmarshal` → `*[]health.strictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/dns.go:286` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/dns.go:294` | `json.Unmarshal` → `*health.strictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/dns.go:363` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/dns.go:614` | `json.Unmarshal` → `*model.Int` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/dns.go:622` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/dns.go:633` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/dns.go:651` | `json.Unmarshal` → `*[]health.strictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/dns.go:694` | `json.Unmarshal` → `*health.strictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:178` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:226` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:247` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:254` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:270` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:297` | `json.Unmarshal` → `*health.strictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:352` | `model.DecodeRecord` → `*health.LatencyEntry` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/health/latency.go:373` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:446` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/health/latency.go:458` | `json.Unmarshal` → `*health.strictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:472` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:483` | `json.Unmarshal` → `*[]health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:492` | `json.Unmarshal` → `*health.strictString` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:500` | `json.Unmarshal` → `*health.LatencyEntry` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:520` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:554` | `json.Unmarshal` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:559` | `json.Unmarshal` → `*health.LatencyEntry` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/latency.go:704` | `json.Unmarshal` → `*model.Fields` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/legacy.go:486` | `json.Unmarshal` → `*model.Fields` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/health/legacy.go:496` | `json.Unmarshal` → `*health.ProbeSettings` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/health/values.go:18` | `model.DecodeRecord` → `*health.rawObject` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/health/values.go:33` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/health/values.go:49` | `json.Unmarshal` → `*model.String` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/legacy.go:24` | `json.Unmarshal` → `any` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/discovery/model.go:28` | `json.Unmarshal` → `*model.DomainDiscovery` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:55` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:59` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:62` | `json.Unmarshal` → `*model.String` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:66` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:69` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:73` | `json.Unmarshal` → `*discovery.HistoryEntry` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:130` | `json.Unmarshal` → `*model.DiscoveryHistory` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:149` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:163` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:166` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:199` | `json.Unmarshal` → `*model.RuleLibraryItem` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:214` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:245` | `json.Unmarshal` → `*model.RuleConfig` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:265` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:279` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/model.go:416` | `json.Unmarshal` → `*discovery.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/query.go:13` | `json.Unmarshal` → `*struct{Library []discovery.libraryItem "json:\"rule_library\""; Rules []discovery.ruleConfig "json:\"rule_configs\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/region.go:43` | `json.Unmarshal` → `*discovery.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/region.go:70` | `json.Unmarshal` → `*discovery.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/region.go:338` | `json.Unmarshal` → `*struct{At string "json:\"checked_at\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/repository.go:34` | `json.Unmarshal` → `*discovery.Settings` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/repository.go:195` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/ruleowners.go:22` | `json.Unmarshal` → `*model.RuleLibraryItem` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/ruleowners.go:119` | `json.Unmarshal` → `*discovery.Match` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/ruleowners.go:144` | `json.Unmarshal` → `*discovery.WebsitePage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/ruleowners.go:154` | `json.Unmarshal` → `*model.RuleLibraryItem` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/settings.go:40` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/settings.go:55` | `json.Unmarshal` → `**string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/discovery/settings.go:77` | `json.Unmarshal` → `*bool` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/discovery/settings.go:91` | `json.Unmarshal` → `*float64` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/discovery/settings.go:98` | `json.Unmarshal` → `*int` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/discovery/settings.go:306` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/settings.go:311` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/discovery/settings.go:311` | `json.Unmarshal` → `**string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/discovery/summary.go:82` | `json.Decode` → `*json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/summary.go:94` | `json.Unmarshal` → `*discovery.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/summary.go:101` | `json.Unmarshal` → `*struct{Days json.RawMessage "json:\"days\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/summary.go:112` | `json.Unmarshal` → `*discovery.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/summary.go:119` | `json.Unmarshal` → `*struct{Hosts json.RawMessage "json:\"hosts\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/summary.go:130` | `json.Unmarshal` → `*discovery.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/summary.go:138` | `json.Unmarshal` → `*struct{Ports json.RawMessage "json:\"ports\""; Routes json.RawMessage "json:\"by_route\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/summary.go:239` | `json.Unmarshal` → `*discovery.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/write.go:48` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/write.go:54` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/discovery/write.go:61` | `json.Unmarshal` → `*discovery.ApplyEntry` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/discovery/write.go:446` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/metrics/history.go:333` | `json.Unmarshal` → `*string` | Agent 回包/上报、已存配置/任务/状态文件 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/agents/metrics/history.go:427` | `json.Unmarshal` → `*map[string]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/metrics/history.go:435` | `json.Unmarshal` → `*[]json.RawMessage` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/metrics/history.go:649` | `json.Unmarshal` → `**jsonx.Object` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/agents/metrics/sample.go:21` | `json.Unmarshal` → `*model.Fields` | Agent 回包/上报、已存配置/任务/状态文件 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/aggregations/adapter.go:46` | `json.Unmarshal` → `any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/aggregations/native_delivery.go:68` | `json.Unmarshal` → `*aggregations.Proxy` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/aggregations/native_delivery.go:142` | `json.Unmarshal` → `*substore.CacheMetadata` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/aggregations/native_delivery.go:197` | `json.Unmarshal` → `*refresh.Traffic` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/aggregations/native_delivery.go:221` | `json.Unmarshal` → `*model.Node` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/aggregations/render.go:21` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/aggregations/render.go:161` | `json.Unmarshal` → `**string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/host/filter.go:111` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/host/filter.go:116` | `json.Unmarshal` → `*[]string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/host/filter.go:141` | `json.Unmarshal` → `*struct{State string "json:\"state\""; Sources []struct{Kind string "json:\"kind\""} "json:\"sources\""; Remedies []struct{Source string "json:\"source\""} "json:\"remedies\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/gateway/host/host.go:51` | `json.Unmarshal` → `*host.Reply` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/host/host.go:52` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/host/host.go:85` | `json.Unmarshal` → `*model.Agent` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/host/host.go:253` | `json.Unmarshal` → `*host.Spec` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/gateway/onboarding/adapter.go:167` | `json.Unmarshal` → `*model.System` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/onboarding/adapter.go:177` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/onboarding/config.go:21` | `json.Unmarshal` → `*onboarding.Document` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/onboarding/config.go:31` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/onboarding/config.go:61` | `json.Unmarshal` → `*model.Profile` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/onboarding/config.go:64` | `json.Unmarshal` → `*model.Shared` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/onboarding/config.go:98` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/onboarding/input.go:48` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/onboarding/input.go:62` | `json.Unmarshal` → `*[]string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/onboarding/input.go:111` | `json.Unmarshal` → `**string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/onboarding/input.go:120` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/onboarding/prepare.go:85` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/onboarding/service.go:112` | `json.Unmarshal` → `*model.Int` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/onboarding/service.go:168` | `json.Unmarshal` → `*onboarding.Report` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/gateway/onboarding/service.go:193` | `json.Unmarshal` → `*model.Int` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/installation/adapter.go:88` | `json.Unmarshal` → `*float64` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/installation/adapter.go:185` | `json.Unmarshal` → `*onboarding.Document` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/installation/adapter.go:298` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/installation/adapter.go:298` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/installation/network.go:192` | `json.Unmarshal` → `*struct{Interfaces []struct{Name string "json:\"name\""; Addresses []string "json:\"addresses\""} "json:\"interfaces\""; Routes []struct{Interface string "json:\"interface\""; Gateway string "json:\"gateway\""} "json:\"default_routes\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/gateway/installation/record.go:57` | `json.Unmarshal` → `*installation.plain` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/gateway/installation/record.go:110` | `json.Unmarshal` → `*installation.task` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/gateway/installation/record.go:120` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/installation/record.go:123` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/installation/record.go:124` | `json.Unmarshal` → `*bool` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/installation/service.go:132` | `json.Unmarshal` → `*int64` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/installation/service.go:151` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/installation/service.go:176` | `json.Unmarshal` → `*int` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/installation/store.go:74` | `json.Unmarshal` → `*installation.Record` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/agents/client_tools.go:81` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/agents/client_tools.go:84` | `json.Unmarshal` → `**string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/agents/client_tools_adapter.go:51` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/agents/client_tools_adapter.go:94` | `json.Unmarshal` → `*struct{Settings discovery.Settings "json:\"domain_discovery\""}` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/api/agents/client_tools_adapter.go:242` | `json.Unmarshal` → `*model.RuleLibraryItem` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/agents/content_routes_adapter.go:173` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/agents/dnsfollow.go:28` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/agents/dnsha.go:30` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/agents/gateway.go:83` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/agents/gateway.go:122` | `httpx.BindSilent` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/agents/gateway.go:126` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/agents/gateway_installation.go:49` | `httpx.BindSilent` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/aggregations/adapter.go:35` | `json.Unmarshal` → `any` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/api/aggregations/adapter.go:59` | `json.Unmarshal` → `*aggregations.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/aggregations/adapter.go:245` | `json.Unmarshal` → `*json.Number` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/aggregations/adapter.go:282` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/aggregations/adapter.go:322` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/aggregations/routes.go:22` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/aggregations/routes.go:33` | `json.Unmarshal` → `*[]aggregations.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/auth/auth.go:32` | `httpx.Bind` → `*auth.loginRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/auth/login_request.go:16` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/auth/login_request.go:28` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/auth/login_request.go:29` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/clientdevices/handlers.go:74` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/clientdevices/handlers.go:89` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/clientdevices/handlers.go:252` | `json.Unmarshal` → `*jsonx.Object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/clientdevices/handlers.go:304` | `json.Unmarshal` → `*struct{Completed bool "json:\"completed\""; Abandoned bool "json:\"abandoned\""}` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/api/config/custom.go:34` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/config/custom.go:66` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/config/custom.go:70` | `httpx.Bind` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/config/custom.go:85` | `json.Unmarshal` → `*config.customRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/config/custom.go:181` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/config/custom.go:209` | `json.Unmarshal` → `*jsonx.Object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/config/custom.go:240` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/config/global.go:44` | `httpx.Bind` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/config/global.go:48` | `json.Unmarshal` → `**jsonx.Object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/config/live.go:53` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/domaindiscovery/routes.go:28` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/domaindiscovery/routes.go:39` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/domaindiscovery/routes.go:54` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/domaindiscovery/routes.go:64` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/domaindiscovery/routes.go:69` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/domaindiscovery/routes.go:84` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/domaindiscovery/routes.go:109` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/gatewayonboarding/routes.go:52` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/generate/artifacts.go:32` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/generate/artifacts.go:35` | `httpx.Bind` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/generate/generate.go:52` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/generate/generate.go:56` | `httpx.Bind` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/ipv6/routes.go:64` | `httpx.BindSilent` → `*ipv6.Document` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/ipv6/routes.go:77` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/draft/adapter.go:194` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/draft/draft.go:36` | `json.Unmarshal` → `*draft.document` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/draft/draft.go:38` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/draft/draft.go:73` | `json.Unmarshal` → `*model.Int` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/draft/draft.go:79` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/draft/draft.go:82` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/gateway/draft/draft.go:85` | `json.Unmarshal` → `*draft.document` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/draft/draft.go:85` | `json.Unmarshal` → `*draft.document` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/gateway/draft/draft.go:181` | `json.Unmarshal` → `*httpx.Number` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/linuxgateway/routes.go:68` | `httpx.BindSilent` → `*draft.Request` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/mosdns/mosdns.go:122` | `json.Unmarshal` → `*model.Fields` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/mosdns/mosdns.go:146` | `json.Unmarshal` → `*model.MosDNSSettings` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/nodes/adapter.go:23` | `json.Unmarshal` → `any` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/api/nodes/crud.go:22` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/nodes/crud.go:27` | `json.Unmarshal` → `*nodes.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/nodes/crud.go:32` | `json.Unmarshal` → `*[]nodes.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/nodes/crud.go:37` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/nodes/crud.go:62` | `json.Unmarshal` → `*nodes.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/nodes/crud.go:168` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/nodes/crud.go:173` | `json.Unmarshal` → `*[]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/nodes/deletion.go:79` | `json.Unmarshal` → `*nodes.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/nodes/deletion.go:136` | `json.Unmarshal` → `*struct{Type string "json:\"type\""; ID string "json:\"id\""}` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/api/nodes/deletion.go:161` | `json.Unmarshal` → `*any` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/nodes/deletion.go:162` | `json.Unmarshal` → `*any` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/nodes/deletion.go:167` | `json.Unmarshal` → `*nodes.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/nodes/deletion.go:236` | `json.Decode` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/nodes/deletion.go:250` | `json.Decode` → `*any` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/notifications/notifications.go:80` | `httpx.BindSilent` → `*notifications.readRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/notifications/read_request.go:10` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/notifications/read_request.go:15` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/notifications/read_request.go:24` | `json.Unmarshal` → `**string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/oxidns/oxidns.go:36` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/oxidns/oxidns.go:116` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/oxidns/oxidns.go:144` | `httpx.BindSilent` → `*oxidns.draftRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/configexport/export.go:36` | `json.Unmarshal` → `*jsonx.Object` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/importer/build.go:32` | `json.Decode` → `*any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/importer/types.go:81` | `json.Unmarshal` → `*importer.value` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/importer/types.go:85` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/importer/types.go:147` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/profiles/profiles.go:184` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/profiles/profiles.go:197` | `json.Unmarshal` → `*json.Number` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/profiles/profiles.go:204` | `json.Unmarshal` → `**jsonx.Object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/profiles/profiles.go:268` | `json.Unmarshal` → `*importer.Shared` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/api/profiles/profiles.go:280` | `json.Unmarshal` → `*jsonx.Object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/proxygroups/adapter.go:29` | `json.Unmarshal` → `*proxygroups.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/proxygroups/adapter.go:50` | `httpx.BindSilent` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/proxygroups/adapter.go:54` | `httpx.Bind` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/proxygroups/adapter.go:62` | `json.Unmarshal` → `*proxygroups.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/proxygroups/model.go:17` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/proxygroups/model.go:26` | `json.Unmarshal` → `*[]proxygroups.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/proxygroups/model.go:40` | `json.Unmarshal` → `*T` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/api/proxygroups/model.go:61` | `json.Unmarshal` → `*proxygroups.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/proxygroups/preview.go:43` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/proxygroups/service.go:103` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/routers/adapters.go:32` | `json.Unmarshal` → `*jsonx.Object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/routers/handlers.go:55` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/routers/handlers.go:73` | `json.Unmarshal` → `**string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/routers/handlers.go:321` | `json.Unmarshal` → `*struct{Fingerprint string "json:\"fingerprint\""}` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/api/routers/workspace.go:233` | `json.Unmarshal` → `**[]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rulelibrary/adapter.go:33` | `json.Unmarshal` → `*rulelibrary.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rulelibrary/adapter.go:53` | `httpx.Bind` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rulelibrary/adapter.go:60` | `json.Unmarshal` → `*rulelibrary.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rulelibrary/cache.go:31` | `json.Unmarshal` → `*model.RuleLibraryItem` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rulelibrary/checks.go:199` | `json.Unmarshal` → `*[]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rulelibrary/checks.go:205` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rulelibrary/draft.go:28` | `json.Decode` → `*map[string]any` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rulelibrary/model.go:17` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rulelibrary/model.go:40` | `json.Unmarshal` → `*[]rulelibrary.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rulelibrary/model.go:48` | `json.Unmarshal` → `*rulelibrary.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rulelibrary/model.go:65` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rulelibrary/model.go:77` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rulelibrary/model.go:81` | `json.Unmarshal` → `*[]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/adapter.go:29` | `json.Unmarshal` → `*rules.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/adapter.go:123` | `json.Decode` → `*any` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rules/adapter.go:151` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rules/adapter.go:156` | `json.Unmarshal` → `*any` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rules/adapter.go:277` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rules/adapter.go:343` | `json.Unmarshal` → `*rules.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/compose.go:211` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rules/compose_adapter.go:19` | `json.Unmarshal` → `*rules.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/dns.go:32` | `json.Decode` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/model.go:16` | `json.Unmarshal` → `**string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rules/model.go:37` | `json.Unmarshal` → `*rules.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/model.go:47` | `json.Unmarshal` → `*[]rules.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/model.go:99` | `json.Unmarshal` → `*[]rules.record` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/model.go:112` | `json.Unmarshal` → `*[]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/read_adapter.go:243` | `json.Unmarshal` → `*struct{Name string "json:\"name\""}` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/api/rules/service.go:198` | `json.Unmarshal` → `*[]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/service.go:219` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/rules/service.go:227` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rules/service.go:278` | `json.Unmarshal` → `*[]string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/rules/service.go:440` | `json.Unmarshal` → `*[]*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/webdav/adapter.go:58` | `json.Unmarshal` → `*webdav.Settings` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/webdav/changes.go:31` | `json.Decode` → `*any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/webdav/changes.go:42` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/webdav/changes.go:46` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/webdav/changes.go:58` | `json.Unmarshal` → `*webdav.commitDocument` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/webdav/changes.go:61` | `json.Unmarshal` → `*webdav.commitDocument` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/webdav/settings.go:49` | `json.Unmarshal` → `*webdav.textFields` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/webdav/settings.go:62` | `json.Unmarshal` → `*int` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/weblistener/legacy.go:20` | `json.Unmarshal` → `*jsonx.Object` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/weblistener/legacy.go:29` | `json.Unmarshal` → `*int` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/weblistener/weblistener.go:196` | `json.Unmarshal` → `*int` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/settings/backup_adapter.go:40` | `json.Unmarshal` → `*webdav.Settings` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/settings/dependencies.go:38` | `httpx.BindSilent` → `*settings.settingsRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/settings/dependencies.go:40` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/settings/requests.go:36` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/settings/requests.go:69` | `httpx.BindSilent` → `*settings.settingsRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/settings/settings.go:175` | `httpx.Bind` → `*settings.settingsRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/settings/settings.go:299` | `httpx.BindSilent` → `*settings.settingsRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/action.go:107` | `json.Unmarshal` → `*routers.Router` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/action.go:111` | `json.Unmarshal` → `*setup.checkedInstallation` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/action.go:136` | `json.Unmarshal` → `*struct{Routers struct{Items []json.RawMessage "json:\"items\""} "json:\"routers\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/action.go:148` | `json.Unmarshal` → `*struct{ID string "json:\"id\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/action.go:167` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/base.go:187` | `json.Unmarshal` → `*jsonx.Object` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/completion.go:97` | `json.Unmarshal` → `*setup.checkedInstallation` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/completion.go:238` | `json.Unmarshal` → `*setup.routeInstruction` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/completion.go:257` | `json.Unmarshal` → `*setup.routeInstruction` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/completion.go:282` | `json.Unmarshal` → `*setup.checkedInstallation` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/completion.go:335` | `json.Unmarshal` → `*setup.checkedInstallation` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/completion.go:364` | `json.Unmarshal` → `*struct{Routers struct{Items []routers.Router "json:\"items\""} "json:\"routers\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/digest.go:18` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/digest.go:30` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/digest.go:55` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/generation_adapter.go:26` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/generation_adapter.go:32` | `json.Unmarshal` → `*jsonx.Object` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/generation_adapter.go:79` | `json.Unmarshal` → `*model.Node` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/prepare.go:33` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/prepare.go:42` | `json.Unmarshal` → `*bool` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/prepare.go:51` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/prepare.go:60` | `json.Unmarshal` → `*jsonx.Object` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/prepare.go:336` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/prepare.go:349` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/prepare.go:456` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/prepare.go:486` | `json.Unmarshal` → `*model.Int` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/presets.go:63` | `json.Unmarshal` → `*setup.template` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：受控 typed/内置模板 round-trip（详见明确例外）；防御性统一 facade |
| `internal/setup/record.go:57` | `model.DecodeRecord` → `*setup.Record` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/setup/record.go:81` | `model.DecodeRecord` → `*setup.Selections` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/setup/record.go:111` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/record.go:121` | `json.Unmarshal` → `*setup.Record` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/repository.go:23` | `json.Unmarshal` → `*model.Shared` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/repository.go:41` | `json.Unmarshal` → `*model.Shared` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/repository.go:52` | `json.Unmarshal` → `*jsonx.Object` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/repository.go:75` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/service.go:51` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/service.go:78` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/service.go:79` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/service.go:80` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/service.go:225` | `json.Unmarshal` → `*setup.Index` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/service.go:279` | `json.Unmarshal` → `*struct{Message string "json:\"message\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/service.go:294` | `json.Unmarshal` → `*jsonx.Object` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/service.go:301` | `json.Unmarshal` → `*routers.Router` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/service.go:306` | `json.Unmarshal` → `*jsonx.Object` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/service.go:315` | `json.Unmarshal` → `*routers.Router` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/service.go:381` | `json.Unmarshal` → `*int64` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/service.go:388` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/service.go:395` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/service.go:427` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/source.go:52` | `json.Unmarshal` → `*[]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/source.go:58` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/source.go:93` | `json.Unmarshal` → `*string` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/setup/source.go:213` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/source.go:221` | `json.Unmarshal` → `*struct{Proxy string "json:\"proxy_gateway_agent_id\""; DNS string "json:\"dns_agent_id\""; Auto bool "json:\"auto_rule_sync\""}` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/setup/source.go:227` | `json.Unmarshal` → `*routers.Router` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/setup/store.go:76` | `json.Unmarshal` → `*setup.Record` | 配置/任务/缓存/文件、Agent 或外部服务结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/stats/stats.go:55` | `model.DecodeRecord` → `*stats.historyDocument` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：schema.byName 精确匹配；重复键拒绝 |
| `internal/api/stats/stats.go:121` | `json.Unmarshal` → `*stats.sample` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/api/stats/stats.go:165` | `json.Unmarshal` → `*stats.sample` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/api/stats/stats.go:264` | `json.Unmarshal` → `*json.Number` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/subscriptions/adapter.go:51` | `json.Unmarshal` → `any` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 泛型/接口目标可达结构体；共享解码层须保护 |
| `internal/api/subscriptions/models.go:16` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/subscriptions/models.go:42` | `json.Unmarshal` → `*subscriptions.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/subscriptions/providers.go:254` | `json.Unmarshal` → `*int` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/subscriptions/routes.go:395` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/tasks/interval_request.go:21` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/tasks/interval_request.go:28` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/tasks/interval_request.go:31` | `json.Unmarshal` → `*httpx.Number` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/tasks/run_request.go:9` | `json.Unmarshal` → `*map[string]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/tasks/run_request.go:14` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/api/tasks/tasks.go:45` | `httpx.BindSilent` → `*tasks.runRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/api/tasks/tasks.go:78` | `httpx.BindSilent` → `*tasks.intervalRequest` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/httpx/httpxtest/errors.go:42` | `json.Unmarshal` → `*map[string]json.RawMessage` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/httpx/httpxtest/errors.go:46` | `json.Unmarshal` → `*string` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/httpx/httpxtest/security.go:44` | `json.Unmarshal` → `*map[string]json.RawMessage` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/httpx/httpxtest/security.go:48` | `json.Unmarshal` → `*string` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/deltas.go:37` | `json.Unmarshal` → `*installertest.deltaSet` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:27` | `json.Unmarshal` → `*installertest.haDeltas` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:34` | `json.Unmarshal` → `*installertest.haDeltas` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:50` | `json.Unmarshal` → `*map[string]json.RawMessage` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:54` | `json.Unmarshal` → `*installertest.haDeltas` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:58` | `json.Unmarshal` → `*string` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:70` | `json.Unmarshal` → `*string` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:83` | `json.Unmarshal` → `*string` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:100` | `json.Unmarshal` → `*[]map[string]json.RawMessage` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:105` | `json.Unmarshal` → `*string` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/installer/installertest/ha.go:109` | `json.Unmarshal` → `*string` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/ippresence/ippresence.go:213` | `json.Unmarshal` → `*model.RuleConfig` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/ippresence/ippresence.go:221` | `json.Unmarshal` → `*[]model.RuleLibraryItem` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mcp/crud.go:83` | `json.Unmarshal` → `*[]mcp.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mcp/crud.go:127` | `json.Unmarshal` → `*[]mcp.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mcp/crud.go:131` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mcp/invoker.go:49` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mcp/invoker.go:54` | `json.Unmarshal` → `*mcp.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mcp/invoker.go:69` | `json.Unmarshal` → `*model.String` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mcp/invoker.go:144` | `json.Unmarshal` → `*mcp.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mcp/invoker.go:149` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mcp/invoker.go:153` | `json.Unmarshal` → `*mcp.arguments` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/mcp/invoker.go:153` | `json.Unmarshal` → `*mcp.object` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mcp/invoker.go:215` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mcp/tools.go:19` | `json.Unmarshal` → `*[]mcp.Definition` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：受控 typed/内置模板 round-trip（详见明确例外）；防御性统一 facade |
| `internal/mcp/tools.go:244` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mcp/transport.go:126` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mcp/transport.go:155` | `httpx.Bind` → `*json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mcp/transport.go:161` | `json.Unmarshal` → `*[]json.RawMessage` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/mcp/transport.go:191` | `json.Unmarshal` → `*mcp.message` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/mcp/transport.go:205` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mcp/transport.go:205` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mcp/transport.go:221` | `json.Unmarshal` → `*struct{ProtocolVersion string "json:\"protocolVersion\""}` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/mcp/transport.go:271` | `json.Unmarshal` → `*mcp.callParams` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/mcp/transport.go:275` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/mcp/transport.go:341` | `json.Unmarshal` → `*string` | HTTP/MCP 请求、存储配置投影或内部调用结果 | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/routers/routerfake/fake.go:152` | `json.Decode` → `*any` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/routers/routerfake/fake.go:204` | `json.Decode` → `*routerfake.ExternalWrite` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/routers/routerfake/fake.go:212` | `json.Decode` → `*routerfake.State` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/routers/routerfake/ikuai.go:184` | `json.Decode` → `*routerfake.IkuaiState` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/routers/routerfake/ikuai.go:216` | `json.Decode` → `*any` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/routers/routerfake/ikuai.go:610` | `json.Decode` → `*routerfake.IkuaiState` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/server/setup.go:82` | `json.Unmarshal` → `*struct{Session json.RawMessage "json:\"session\""}` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/server/setup.go:121` | `json.Unmarshal` → `*struct{ID string "json:\"id\""; Phase string "json:\"phase\""; ConfigChanged bool "json:\"config_changed\""; TargetChanged bool "json:\"target_changed\""; Agents map[string]json.RawMessage "json:\"agents\""}` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 须修：普通结构体大小写折叠；统一 jsondecode，重复键最后完整值替换 |
| `internal/server/setup.go:129` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/server/setup.go:132` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/server/setup.go:152` | `json.Unmarshal` → `*map[string]json.RawMessage` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 自定义 codec/RawMessage；本层不折叠，继续审计内部解码（见同清单） |
| `internal/server/setup.go:157` | `json.Unmarshal` → `*any` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/server/setup.go:183` | `json.Unmarshal` → `*string` | 配置/内容/文件/内部 JSON 桥接（按下述 codec 分类） | 安全：map 键/标量/动态值无结构体字段匹配；保留现行重复键行为 |
| `internal/testgolden/golden.go:216` | `json.Unmarshal` → `*testgolden.document` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/testgolden/sources.go:43` | `json.Unmarshal` → `*[]testgolden.sourceSet` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |
| `internal/yamlx/yamlxtest/history.go:51` | `json.Decode` → `any` | 测试 fake/固定夹具（非生产输入） | 可信测试协议/夹具；不改生产行为 |

### 非默认构建与生成工具补扫（先登记，再修复）

默认 GoFiles 的 1,215 项之外，rg 补扫还登记以下 5 个调用；生产 cmd/configflow 没有 JSON 解码点。OS 专用文件没有额外 JSON 解码。

| 文件:基线行 | 输入来源/目标 | 结论 |
|---|---|---|
| `internal/api/agents/content_observer_proddiff.go:68` | proddiff 标签观察本服务 HTTP 列表，`[]item` | 仅测试观察器；Status 无 tag、依赖折叠，统一 facade 并补 status tag，避免扩展大小写键影响等待判定 |
| `internal/api/agents/content_observer_proddiff.go:78` | 同上，单条 `item` | 同类修复；不影响生产构建 |
| `internal/yamlx/testdata/readers/main.go:85` | yamlx 生成的 JSON 标量 → `any` | 安全：生成器只读本层标量，不解入结构体 |
| `internal/yamlx/testdata/readers/main.go:108` | 固定 N7 迁移输入 sources manifest → source/mode rows | 安全：仅非生产录制生成器读取维护者准备的固定迁移清单，字段由生成脚本约定；不读取服务配置或设备输入 |
| `internal/yamlx/yamlxtest/testdata/frozenindex/main.go:83` | 固定 N7 录制 archive → archive row（含 physicalNode） | 安全：非生产生成工具，仅读经 revision、source hash 校验的固定录制；字段名由录制器维护 |

另有仅使用 Token 的 reader：`discovery/options.go:16`（只读开头类型判断开关）、`jsonx/json.go:29,60`（精确 Object 键/数字）、`yamlx/native_json.go:19`（精确 Node 键，JSON 重复最后值）；这些没有结构体字段匹配。

## 修复与验证

实现与验证已完成。原生解码负控已确认：9 个测试包及 1 个 proddiff 标签包的代表回归均失败（普通 DTO、Bind、设备、路由证明/manifest、存储凭据、订阅、Sub-Store、MCP），切回共享实现后原有专项通过。

- 新共享 `internal/jsondecode`：原生 JSON 语法/标量解码，普通结构体按完整 JSON tag 名匹配；递归覆盖指针、数组、slice、map 值及匿名嵌入字段。未知字段忽略，普通 DTO 重复字段取最后完整值。Unicode 折叠别名（如 long-s）也不再匹配。
- HTTP Bind、设备 DecodeJSON/Response.JSON、DecodeStored 和所有可达普通结构体的原生 Unmarshal/泛型桥接统一使用 facade。map、标量、RawMessage 不增加不必要的裁剪；自定义 codec 接收原文，内部 alias 使用 facade，model 严格存储规则不变。
- `routers/plan_return.go` 的 priority/table/mark 读协议时原无 tag，依赖大小写折叠，已补小写 tag；精确合法证明继续有效，大小写别名不再构成证明。
- 暂存 JSON 子值通过原生 UnmarshalJSON 获得输入片段，避免给未消费的大配置分配整份副本；片段仅在解码准备阶段借用，输出的 RawMessage/model 仍由各自 codec 持有副本。

### 可信结构体/内部 round-trip 的明确例外

| 基线位置 | 可信理由 | 处置 |
|---|---|---|
| `internal/mcp/tools.go:19` | `catalog` 是 go:embed 的仓库 catalog.json，键由源码维护 | 无外部输入缺陷；防御性统一使用 facade |
| `internal/setup/presets.go:63` | `repo.GetInitialConfig()` 由内置模板构造，固定根键 | 无外部输入缺陷；防御性统一使用 facade |
| `internal/configgen/generate.go:224` | OxiDNS RulesetDownloads 的 Go 类型写出、再转成 MosDNS Download；字段名由类型控制 | 无外部输入缺陷；防御性统一使用 facade |
| `internal/rules/matchercache.go:573` | MatcherInput 已是 typed 列表，JSON 只用于深复制；子记录自带精确 model codec | 无新的字段身份来源；防御性统一使用 facade |
| `internal/routers/rospolicy_plan.go:274` | 本地 typed requestCopy 经 Marshal 读成 map，只有值来自请求 | 安全，map 本身精确；无需改 |
| `internal/routers/wire.go:33,49,57` | typed report/summary/router 桥接，目标均为 model 自定义 codec | 安全，codec 精确且保留原始证据；无需改 |
| 清单中 agentfake/routerfake/httpxtest/installertest/testgolden/yamlxtest 全部调用 | 仅测试 fake 控制口、固定录制/测试夹具，不链接生产 ConfigFlow 请求路径 | 原生测试协议保留，不将其当生产漏洞 |

### 回归和负控

代表类别：`jsondecode.TestExactMembers`（普通/嵌套/list/map/escaped key/Unicode alias/整值重复）；`TestCustomModelPolicyAndProvenance`（model 精确键、重复拒绝和原字节）；`TestEmbeddedAndDeclaredNames`（嵌入字段、歧义、默认名和 string tag）；`httpx.TestAuditBindingExactMembers`（Bind 与 BindSilent）；`devhttp.TestAuditDeviceReplyExactMembers`；`routers.TestAuditReturnProofExactMembers` / `TestAuditManifestExactMembers`；`deploy.TestAuditManifestFileMetadataExactNames`（required metadata 校验和最终文件路径一致）；`webdav.TestAuditStoredCredentialsExactMembers`（自定义 codec 的 alias/凭据）；`subs.TestAuditVMessExactMembers`；`substore.TestAuditVersionReplyExactMembers`；`mcp.TestAuditRPCExactMembers`。另将 METHOD/ID/JSONRPC 大写和混合键加入 MCP 的真实 Python HTTP differential。

首轮定向测试发现并修复了嵌套自定义 codec 分派和无关配置复制的实现问题；首次 routers 运行在已知失败后中止，不能视为通过。原生负控已完成且 9 个默认包 + 1 个标签包的代表回归全部失败；修正后专项通过。较大范围的包测试在磁盘临时目录下 fsync 过慢，采样时 agents 线程停在 notifications.store 的 File.Sync，诊断运行人工中止，不能视为通过；最终运行通过 `go test -exec "env TMPDIR=… GOTMPDIR=…"` 将测试进程的数据放入 .32 `/dev/shm` 临时目录、编译进程的 GOTMPDIR 留磁盘（Go 1.26 testing.TempDir 优先读 GOTMPDIR，单改 TMPDIR 无效），扩大的全包状态机测试另已中止，按本任务限定收敛为 JSON/协议相关的定向 race；结果见下方验证记录。


### 修复后扫描复核

`go run ./tools/jsonaudit`（.32）在默认构建修复树记录 1,221 个 JSON 调用，其中 221 个使用共享 jsondecode；补上 configflow_proddiff 标签与两个 testdata 工具后为 1,226 个，其中 223 个使用共享 jsondecode。剩余原生普通结构体/泛型入口仅有：jsondecode 最终原生调用；model/record.go:537（所有路径先经过 schema/storedType，结构体必须实现自定义 codec）；以及明确豁免的 fake/夹具包。没有遗漏的生产 DTO 原生解码入口。`model.Map.UnmarshalJSON` 同样先用 storedType 限制泛型值类型。

未更改 UI、操作入口或功能文档；此次是 JSON 接受语义/协议一致性修复，报告记录完整输入与安全影响。


## 最终验证记录

主机固定为 `sunbroke@192.168.0.32`。未跑 gate/proddiff/全仓 race，未另开 agent。最终 race 仅选择 JSON 与协议相关测试；HTTP、设备、WebDAV、订阅、Sub-Store、MCP 的全包 race 在前面的专项运行亦已通过，但扩大到旧状态机的全包尝试已人工中止，不算通过。

最终运行命令（测试进程临时文件放 tmpfs，编译工作目录仍在磁盘）：

```bash
set -euo pipefail
cd server
gofmt -w internal/jsondecode/jsondecode_test.go internal/httpx/json_case_test.go internal/subs/json_case_test.go internal/mcp/difftest/diff_test.go tools/jsonaudit/main.go internal/mcp/json_case_test.go internal/deploy/json_case_test.go
test -z "$(gofmt -l internal cmd tools/jsonaudit)"
go vet ./...
go run ./tools/jsonaudit > parity/progress/json-case-current.jsonl
audit_tmp=$(mktemp -d /dev/shm/configflow-json-case.XXXXXX)
trap 'rm -rf -- "$audit_tmp"' EXIT
export TMPDIR="$audit_tmp" GOTMPDIR="$HOME/.cache/rt-tmp"
go test -exec "env TMPDIR=$audit_tmp GOTMPDIR=$audit_tmp" -race -p 4 -count=1 -run 'TestAudit|TestExactMembers|TestCustomModelPolicyAndProvenance|TestDecodeNumberAndValidation|TestEmbedded|TestReviewB10HeartbeatMembersMatchExactNames|TestReviewB10PublicHeartbeatBytesMatchModelPath|TestRawClientRequestsAndMalformedReplies|TestMemberIdentityComparisonAndUnknownFields|TestPayloadResumeKeepsPythonPresenceAndUnknownFields|TestDNSRequestsCarryExactDeploymentQuery|TestMalformedListenerReplyKeepsSpecificRefusal|TestDeviceAndSourceNamesFollowStoredRecord|TestN89SyncPipelineAllocations' ./internal/jsondecode ./internal/httpx ./internal/devhttp ./internal/webdav ./internal/subs ./internal/substore ./internal/mcp ./internal/deploy ./internal/routers ./internal/agents ./internal/agents/dnsfollow ./internal/agents/dnsha 2>&1 | tee parity/progress/json-case-targeted.log
CONFIGFLOW_DIFF=1 CONFIGFLOW_DIFF_PYTHON="$HOME/cf-test/.venv/bin/python" go test -exec "env TMPDIR=$audit_tmp GOTMPDIR=$audit_tmp" -p 4 -count=1 -timeout=40m ./internal/agents/dnsfollow/difftest ./internal/agents/dnsha/difftest ./internal/api/agents/difftest ./internal/api/aggregations/difftest ./internal/api/config/difftest ./internal/api/gatewayonboarding/difftest ./internal/api/linuxgateway/difftest ./internal/api/oxidns/difftest ./internal/api/rulelibrary/difftest ./internal/api/rules/difftest ./internal/api/subscriptions/difftest ./internal/devicenotes/difftest ./internal/discovery/difftest ./internal/gateway/installation/difftest ./internal/health/difftest ./internal/httpx/difftest ./internal/mcp/difftest ./internal/mosdns/difftest ./internal/provider/difftest ./internal/refresh/difftest ./internal/subs/difftest ./internal/substore/difftest ./internal/tasks/difftest ./internal/webdav/difftest 2>&1 | tee parity/progress/json-case-difftest.log
```

上面的临时脚本执行命令是 `RT_HOST=sunbroke@192.168.0.32 rt -- bash server/parity/progress/json-case-run.sh`。临时脚本已清理，上文保留完整内容供复现。未在 Mac 执行测试或构建。

结果：格式检查无输出，`go vet ./...` 通过；12 个包的定向 race 全通过，24 个相关 difftest 包全通过（CONFIGFLOW_DIFF=1，含 MCP 新增的真实 Python 大小写用例）。最后输出如下：

```text
ok  	configflow/internal/jsondecode	1.012s
ok  	configflow/internal/httpx	1.224s
ok  	configflow/internal/devhttp	1.014s
ok  	configflow/internal/webdav	1.027s
ok  	configflow/internal/subs	1.049s
ok  	configflow/internal/substore	1.071s
ok  	configflow/internal/mcp	1.189s
ok  	configflow/internal/deploy	1.094s
ok  	configflow/internal/routers	18.326s
ok  	configflow/internal/agents	9.275s
ok  	configflow/internal/agents/dnsfollow	216.186s
ok  	configflow/internal/agents/dnsha	24.537s
ok  	configflow/internal/agents/dnsfollow/difftest	21.687s
ok  	configflow/internal/agents/dnsha/difftest	12.724s
ok  	configflow/internal/api/agents/difftest	162.264s
ok  	configflow/internal/api/aggregations/difftest	18.112s
ok  	configflow/internal/api/config/difftest	31.099s
ok  	configflow/internal/api/gatewayonboarding/difftest	2.447s
ok  	configflow/internal/api/linuxgateway/difftest	8.724s
ok  	configflow/internal/api/oxidns/difftest	7.153s
ok  	configflow/internal/api/rulelibrary/difftest	15.377s
ok  	configflow/internal/api/rules/difftest	35.253s
ok  	configflow/internal/api/subscriptions/difftest	46.728s
ok  	configflow/internal/devicenotes/difftest	0.641s
ok  	configflow/internal/discovery/difftest	4.839s
ok  	configflow/internal/gateway/installation/difftest	5.520s
ok  	configflow/internal/health/difftest	13.736s
ok  	configflow/internal/httpx/difftest	8.065s
ok  	configflow/internal/mcp/difftest	0.792s
ok  	configflow/internal/mosdns/difftest	33.247s
ok  	configflow/internal/provider/difftest	49.829s
ok  	configflow/internal/refresh/difftest	4.142s
ok  	configflow/internal/subs/difftest	1.929s
ok  	configflow/internal/substore/difftest	5.816s
ok  	configflow/internal/tasks/difftest	4.422s
ok  	configflow/internal/webdav/difftest	11.490s
```

补扫 proddiff 标签观察器只跑新增单测：`go test -tags configflow_proddiff -race -count=1 -run "^TestAuditProddiffObservationExactNames$" ./internal/api/agents`，输出 `ok configflow/internal/api/agents 1.072s`；没有跑完整 proddiff。

最终全范围类型清单：`GOFLAGS=-tags=configflow_proddiff go run ./tools/jsonaudit ./internal/yamlx/testdata/readers ./internal/yamlx/yamlxtest/testdata/frozenindex > parity/progress/json-case-current.jsonl`。原始基线 JSONL 为默认构建的 1,215 项，非默认 5 项在上面补扫表逐项登记。

### 修复前行为负控与恢复验证

临时脚本命令为 `RT_HOST=sunbroke@192.168.0.32 rt -- bash server/parity/progress/json-case-negative.sh`（已清理；完整内容如下）。它临时把共享 facade 换回原生 Unmarshal/Decoder，恢复迁移前各入口的匹配行为，不改业务 DTO 或断言。9 个默认包和 1 个标签包均出现预期回归失败；恢复共享实现后相同回归全部通过。脚本以 0 退出（用失败作为负控成功条件），无临时 stub 留在分支。

```bash
set -euo pipefail
cd server
gofmt -w internal/api/agents/content_observer_proddiff.go internal/api/agents/content_observer_proddiff_test.go internal/jsondecode/jsondecode_test.go internal/httpx/json_case_test.go internal/subs/json_case_test.go internal/mcp/difftest/diff_test.go tools/jsonaudit/main.go internal/mcp/json_case_test.go internal/deploy/json_case_test.go
audit_tmp=$(mktemp -d /dev/shm/configflow-json-negative.XXXXXX)
export TMPDIR="$audit_tmp" GOTMPDIR="$HOME/.cache/rt-tmp"
cp internal/jsondecode/jsondecode.go "$audit_tmp/original.go"
trap 'cp "$audit_tmp/original.go" internal/jsondecode/jsondecode.go; rm -rf -- "$audit_tmp"' EXIT
cat > internal/jsondecode/jsondecode.go <<'NATIVE'
package jsondecode
import("encoding/json";"bytes")
func Unmarshal(data []byte,dst any)error{return json.Unmarshal(data,dst)}
func Decode(data []byte,dst any)error{d:=json.NewDecoder(bytes.NewReader(data));d.UseNumber();return d.Decode(dst)}
NATIVE
set +e
go test -exec "env TMPDIR=$audit_tmp GOTMPDIR=$audit_tmp" -race -count=1 -run 'TestAudit|TestExactMembers|TestEmbedded|TestDecodeNumberAndValidation' ./internal/jsondecode ./internal/httpx ./internal/devhttp ./internal/webdav ./internal/subs ./internal/substore ./internal/mcp ./internal/deploy ./internal/routers > parity/progress/json-case-negative.log 2>&1
negative_status=$?
go test -exec "env TMPDIR=$audit_tmp GOTMPDIR=$audit_tmp" -tags configflow_proddiff -race -count=1 -run "^TestAuditProddiffObservationExactNames$" ./internal/api/agents > parity/progress/json-case-tag-negative.log 2>&1
tag_negative_status=$?
set -e
cp "$audit_tmp/original.go" internal/jsondecode/jsondecode.go
test "$negative_status" -ne 0
test "$tag_negative_status" -ne 0
test "$(rg '^FAIL\s+configflow/internal/' parity/progress/json-case-negative.log | wc -l)" -eq 9
rg '^--- FAIL: Test|^FAIL\s+configflow/internal/' parity/progress/json-case-negative.log
test -z "$(gofmt -l internal cmd tools/jsonaudit)"
go test -exec "env TMPDIR=$audit_tmp GOTMPDIR=$audit_tmp" -race -count=1 -run 'TestAudit|TestExactMembers|TestEmbedded|TestDecodeNumberAndValidation|TestCustomModelPolicyAndProvenance' ./internal/jsondecode ./internal/httpx ./internal/devhttp ./internal/webdav ./internal/subs ./internal/substore ./internal/mcp ./internal/deploy ./internal/routers > parity/progress/json-case-restored.log 2>&1
cat parity/progress/json-case-restored.log
go test -exec "env TMPDIR=$audit_tmp GOTMPDIR=$audit_tmp" -tags configflow_proddiff -race -count=1 -run "^TestAuditProddiffObservationExactNames$" ./internal/api/agents
GOFLAGS=-tags=configflow_proddiff go run ./tools/jsonaudit ./internal/yamlx/testdata/readers ./internal/yamlx/yamlxtest/testdata/frozenindex > parity/progress/json-case-current.jsonl

```

默认包的负控失败：jsondecode、httpx、devhttp、webdav、subs、substore、mcp、deploy、routers。标签负控失败为 `TestAuditProddiffObservationExactNames` 的 case-only/mixed/nested。恢复后的输出：

```text
ok  	configflow/internal/jsondecode	1.014s
ok  	configflow/internal/httpx	1.232s
ok  	configflow/internal/devhttp	1.012s
ok  	configflow/internal/webdav	1.025s
ok  	configflow/internal/subs	1.032s
ok  	configflow/internal/substore	1.039s
ok  	configflow/internal/mcp	1.205s
ok  	configflow/internal/deploy	1.089s
ok  	configflow/internal/routers	1.099s
ok configflow/internal/api/agents 1.072s
```

## 交回

分支 `go-audit-json-case`，基线 `12150a402`；本地 go 仍在该基线，无待解决冲突。未推送，未跑完整门禁，未使用复核 agent。没有遗留实现项；最终追加检查：.32 上 `test -z "$(gofmt -l internal cmd tools/jsonaudit)" && go vet ./... && go vet -tags configflow_proddiff ./internal/api/agents` 退出 0、无输出。

功能变化限于不再把 JSON 大小写别名当作已声明字段（恢复精确键语义），普通 DTO 的重复成员按最后完整值处理，model 的原文/未知成员和重复键拒绝规则保持原契约。没有新增依赖。
