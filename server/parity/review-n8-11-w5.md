# N8-11 W5 配置/档案/设置 API 簇复核（fresh Opus）

复核对象：`go..HEAD` 的三个提交（3aaa4a4db 配置 HTTP 边界迁移、518aa77f2 R5 原始字段类型与 auth 去 Python 辅助、80f7502ef live/linuxgateway 鉴权传同快照 System）。
复核分支 `go-opus-review-n8-11-w5`。按计划 2.7，导入导出与令牌路径按 HR 标准抽查，其余按非 HR 抽查。对照的是 Python（`backend/routes/{profiles,settings,config}.py`、
`backend/common/{auth,config_repository}.py`、`backend/agents/manager.py`）和 `go` 上的旧 Go。协调者裁定（global 409 去 usages、live/linuxgateway 用 `ValidateSystemTokenOrJWT`
并传同快照 System、L1 形状校验放进 HTTP 绑定、description 只收字符串、profiles 非法 JSON/非有限数统一 400、R5 在 DTO 处读原始成员、auth 去 PyParseInt/PyTypeName）只核对实现，
没有重新讨论。按 Leaner verification，没有重跑作者整套差分；只跑修复相关的定向测试和少量抽查，测试都在 .64 上经 `rt` 运行，回归测试只用合成数据。

## 结论

high 0，medium 3（已修，各附 Flask 差分回归和负控），low 6（记录）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| M1 | medium | 已修 | **profile 缺引擎段时读自定义配置 500。** 位置 `api/config/custom.go` `readCustom`。新代码 `json.Unmarshal(fields[engine], &settings)`，成员缺失时输入为空，报 `unexpected end of JSON input`，返回 500。仓库校验并不要求 profile 带 `loon`（导入旧备份时可能没有），Python 的 handler 用 `setdefault('loon', {'custom_config': ''})`，返回 `{"config": ""}`；旧 Go 的 `GetObject` 也返回空。影响：这类 profile 打开 Loon 页面直接报错。修复：成员缺失时按空设置处理；成员存在但不是对象时仍报错，与 Python 的 500 一致。测试 `TestReviewN811W5MissingEngineSection`：两侧导入同一份去掉 `loon` 的文档，比较 GET、POST、再 GET 的状态、响应和存储。负控：换回复核前的 custom.go，Go 返回 500，Python 返回 200，测试失败。 |
| M2 | medium | 已修（新增登记） | **`DELETE /api/profiles/<id>` 被 profiles 的严格 JSON 绑定覆盖到了。** 位置 `api/profiles/profiles.go` `objectBody`。裁定和 ledger 只写了 create/PUT/clone，但 DELETE 也走 `objectBody(c, true)`。所以只要带 `Content-Type: application/json`，即使请求体为空也返回 400，profile 删不掉。Flask 的 `get_json(silent=True) or {}` 会照常删除。前端用 axios，无请求体时不发这个头；但通用 HTTP 客户端常对所有方法都带上它。修复：DELETE 在 JSON Content-Type 下请求体为空（含只有空白）时，保持静默回退为 `{}`；非法 JSON 和 NaN 仍按裁定返回 400，不删除。Flask 在这两种情况下分别是静默按 `{}` 删除和 500，与“非法 JSON/非有限数统一 400”同属一类，已在 behaviour-changes 的 W5 条目下补登。测试 `TestReviewN811W5DeleteProfileBody`：空体和 `" \n"` 两侧都返回 204，且两侧存储一致；`{` 和 `NaN` 分别断言 Python 的原状态、Go 的 400 完整错误体，以及存储不变。负控：换回复核前的 profiles.go，空体用例得到 400，测试失败。 |
| M3 | medium | 已修 | **改绑请求的校验顺序与 Python 不一致，中文消息不同。** 位置 `api/profiles/profiles.go` rebind。新代码在调用 `RebindProfiles` 之前先检查 `agent_ids`。Python（`manager.rebind_profile_agents`）的顺序是：校验来源 id → 校验目标 id → 检查是否同一配置 → 检查 `agent_ids`。旧 Go 的 legacy 入口也是这个顺序。两处都不合法的请求会得到另一条 400 消息：例如目标就是来源本身且 `agent_ids` 不是列表，Python 返回「请选择其他配置空间」，Go 返回「请提供已确认的 Agent ID 列表」；目标为 null 时，Python 返回 `Invalid profile id`。修复：形状不合法的 `agent_ids` 按“没有 ID”传给 `RebindProfiles`，由它按原顺序校验，最后报同一条消息；删掉 profiles 包里重复的 `msgReviewedIDs`。测试 `TestReviewN811W5RebindValidationOrder`，三种组合与 Flask 完全相等。负控：换回复核前的代码，消息不同，测试失败。 |
| L1 | low | 记录 | **settings 的 POST 失败日志被删掉了。** server-domain、config-token（POST/DELETE）和 subscription-aggregation 在输入或保存失败时返回 500 和 `err.Error()`，但不再记 `Failed to update server domain: …` 这类错误日志。Python 和旧 Go 都会记。响应体不变，只是少了运维诊断。建议下次改 settings 时补回（直接用 `appLogger.Error`，不要既记日志又把错误返回）。 |
| L2 | low | 记录 | **JWT 字符串数值声明不再接受下划线。** `parseClaimInt` 用 `big.Int.SetString(s, 10)`，`"1_700_000_000"` 现在报 `errClaimValue`；Python `int()` 和旧 `PyParseInt` 都接受下划线。ledger 只登记了“拒绝非 ASCII 数字”。这类声明只会出现在我们自己用密钥签发的令牌里，而签发方只写整数，实际没有影响。建议在 ledger 那一条补上“下划线分隔”。 |
| L3 | low | 记录 | **OxiDNS 导出脱敏出错时，从“失败即全部打码”变成整次导出 500。** 旧 hook 遇到任何错误都返回 `[REDACTED]` 字符串；新的 `RedactOxidnsJSON` 把错误往上返回，导致 desensitize 导出失败。两种做法都不会泄漏数据，而存储里的合法 JSON 实际不会触发这个错误。记录在这里，供以后决定统一采用哪种失败方式。 |
| L4 | low | 记录 | **备份配置 POST 后，backup 成员顺序变成按字母排序。** `backupConfig` 先把成员读进 `map[string]json.RawMessage`，再重新编码，存储里的 `backup` 键序因此变成字母序；值和未知成员都保留。这属于已获准的成员顺序许可，不影响读取。webdav 快照改用 `json.MarshalIndent`，会把 `<>&` 转义，值相同，与 W1 已登记的 fileJSON 同类。 |
| L5 | low | 记录（W9） | **live.go 和 linuxgateway/routes.go 各复制了一段相同的“jsonx `system_config` → `model.System`”转码。** 实现符合裁定（同一快照、不传 nil）。不过这两段和 `auth/legacy.go` 的转码是同一个形状，W7 把 live 换成 typed `Config` 后应该直接传 `config.System`，到时删掉这两段。 |
| L6 | low | 记录 | **`agentContext` 对同一个 id 会把所有同 id 的 Agent 都加入。** 旧实现按 id 建 map，后出现的覆盖前面的，每个绑定只取一条记录。只有存储里出现重复 Agent id 时才有区别，而 `AgentPorts` 对保留端口是先到者优先、主机去重，结果基本相同。 |

## 逐项核对

1. **导出脱敏（HR）。** `ExportAllJSON(desensitize)` 仍然清掉三个敏感设置，把 backup 设为 `{}`、agents 设为 `[]`、订阅 url 和节点五键改成 `[REDACTED]`、params 设为 `{}`，并处理 oxidns/routers/easytier 三个 hook、四个引擎的 custom_config 和 restore point；最后经过 `SanitizeExternalDocument`，交给共享的 `redact.SanitizeExternalPayload`。新的 configexport hook 把 `model.Document` 编码成 RawMessage 后再交给 sanitizer；sanitizer 对 RawMessage 用 `decodePublic` 逐层展开（保持键序，标量保留原文），所以不会绕过遍历，大整数也不会变成 float64。secrets 取自 repo 的规则代理令牌、Agent 令牌和传入的原始 settings。作者的负控（把 redactor 换成直接返回输入，`TestFullAndShareableBackupHTTP` 报 `desensitize bypassed external payload policy`）有效。`profiles` 的单 profile 导出也走同一个 `configexport.Sanitize`。
2. **导入/重置。** `global.go` 先要求请求体以 `{` 开头（Bind 之后已规范化），否则返回 400 `msgObject`；`ErrInUse` 映射成只带 success/message 的 409，与 Flask 一致。其余错误照常返回。`writeGlobal` 会 Detach、检查 `ctx.Err()`，成功后重置 Scope。repo 层的 batch、退役旧令牌和 revision 推进都没有改动。
3. **令牌/JWT（HR）。** `ValidateSystemTokenOrJWT` 与 Python `validate_token_or_jwt` 逐支对照：内部令牌集合只取 `rule_proxy_token` 和 `retired_rule_proxy_tokens` 中的非空字符串（`SanitizationTokens` 的第一个返回值，不包括 Agent 令牌）；bearer 和 url token 都用 `equalCredential`（固定长度摘要，常量时间）对每个令牌比较，没有短路；`not config_token` 的真值判断覆盖 `""`、null、false、0、0.0、-0、`[]`、`{}`（数字和容器先解析再判断）；非字符串的 config_token 永远不能通过 url token 认证（R5 投影成 `"42"` 也不行，有测试）；返回消息固定为 `Invalid or missing authentication`，不带令牌内容。live 和 linuxgateway 从处理器的同一个 `config` 快照解码 System，作者的轮换测试证明鉴权和渲染用的是同一个快照。JWT：数值和布尔分支没有改；字符串分支只有 L2 所说的下划线差异；类型错误仍是 `*ClaimTypeError`（500），消息改成原生文本，不含令牌。
4. **R5 原始类型。** settings 的 GET 用 `MemberJSON` 原样返回 `rule_fetch_proxy`、`server_domain`、`config_token` 和 `subscription_aggregation_enabled`；backup 的 GET 对 username、path、auto_backup 返回原始值，密码只按真值显示 `******`，webdav_url 仍走 `backupEditorURL`。我加了探针（没有提交）：`model.Backup` 为零值、缺失或 null 时编码为 `{}`，未知成员和非字符串成员原样保留，所以 POST 的 map 合并既不会因 nil map 崩溃，也不会多写成员。routers 的 workspacePeers 和 clientdevices 读 profile_id/host 时用 `MemberJSON` 加字符串解码，非字符串按空处理，与 Python `get(...)` 后比较字符串的结果一致。
5. **新增的 400 边界。** 抽查了 `n811_w5_test.go` 的 metadata（错型、多余键、空白名）、import 非对象、rebind 非列表、import-client 错型 IDs、description 三个入口、create/PUT/clone 的非法 JSON/空体/NaN，以及 router 确认的 NaN。每类都有一例 Flask 差分，并严格比较存储。缺的是 DELETE（见 M2）和 rebind 的复合错误顺序（见 M3），已补齐。
6. **设置备份读写（凭据）。** GET 不返回密码明文，POST 传 `******` 或空串时保留原密码；脱敏导出中 backup 整体是 `{}`；webdav 快照读的是 `document.System.Backup` 的原始 JSON。日志只记录变更的字段名，不记录值。没有发现泄漏。
7. **被删除的 facade。** `auth.pyParseInt/pyTypeName`、settings 的 `requests.go/legacyObject/silentRequestObject`、tasks 的 `legacy.go`、旧的 `ValidateTokenOrJWT` 函数体（现在转发到 typed）：grep 确认没有生产调用方遗留；`httpx.PyParseInt` 仍有别的使用者，留给 W9。
8. **ctx/Detach（第四条）。** settings 的读写、profiles 的 `mutate`、custom 的 `saveCustom`（OxiDNS 分支在回调内、提交前检查 `ctx.Err()`）、global 的 `writeGlobal`、clientdevices 的状态读取和设备写入，都先 Detach，并在提交点检查 `ctx.Err()`；作者在真实 Runtime 下的断开和停止测试保留。
9. **测试替身。** 没有发现替身覆盖生产参数；R12 的 return-routing 原始请求断言没有改。

## 复核运行（.64）

修复及负控（负控在服务器副本上把对应文件换回复核前版本，跑完后还原）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/api/config internal/api/profiles)" && echo GOFMT OK && go vet ./internal/api/config/... ./internal/api/profiles && CONFIGFLOW_DIFF=1 go test -count=1 -run "TestReviewN811W5" -v ./internal/api/config/difftest; <换回复核前 custom.go/profiles.go/messages.go> CONFIGFLOW_DIFF=1 go test -count=1 -run "TestReviewN811W5" -v ./internal/api/config/difftest; <还原>'
```

```text
--- PASS: TestReviewN811W5MissingEngineSection (1.11s)
--- PASS: TestReviewN811W5DeleteProfileBody (3.21s)
--- PASS: TestReviewN811W5RebindValidationOrder (0.78s)
ok  	configflow/internal/api/config/difftest	5.110s
== negative control
    review_n811_w5_test.go:40: status: $ Go=500 Python=200
    review_n811_w5_test.go:62: empty JSON body deletes phone: $ Go=400 Python=204
    review_n811_w5_test.go:67: body: $.message Go="请提供已确认的 Agent ID 列表" Python="请选择其他配置空间"
    review_n811_w5_test.go:67: body: $.message Go="请提供已确认的 Agent ID 列表" Python="Invalid profile id"
FAIL	configflow/internal/api/config/difftest
restored
```

改动包 `-race` 和相关差分组：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l .)" && echo GOFMT OK && go vet ./internal/api/... && go test -race -count=1 ./internal/api/config ./internal/api/profiles && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=20m -run "TestReviewN811W5|TestN811W5|TestC34ProfilesAndCustomConfiguration|TestC34ExplicitBindingDifferences|TestReviewC34" ./internal/api/config/difftest'
```

```text
GOFMT OK
ok  	configflow/internal/api/config	16.656s
ok  	configflow/internal/api/profiles	6.575s
ok  	configflow/internal/api/config/difftest	120.981s
```

完整预检结果见交回报告。
