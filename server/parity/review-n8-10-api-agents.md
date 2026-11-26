# N8-10 api/agents S pass HR 复核（fresh Opus）

复核对象：`go-n8-10-api-agents@f8b387f7f`，只看 `52dd0b884..f8b387f7f`（29d0a6115 installer 请求/渲染选项、
b3cbaef4b registry 与公开 client 边界、f8b387f7f monitor 绑定与边界证据）。复核分支 `go-opus-review-n8-10-api-agents`。
对照 Python（`backend/routes/agents.py`、`backend/agents/manager.py`、`oxidns_ha_publish.py`）和迁移前 Go。
upgrades 线（review-n8-10-upgrades.md）没有重审；traffic.go 仍走旧 metrics/trafficstore 属已批准范围。
定向测试只在 .64 经 `rt` 运行（GOMAXPROCS=4，-p 4），没有重跑作者整套差分，也没有跑完整门禁。回归只用合成数据
（192.0.2.0/24、假 token）。

## 结论

high 0，medium 0，low 8（3 已修、1 已补登、4 记录）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| R1 | low | 已修 | **`GET /api/agents/<id>/status`：存储的 `status` 为 null 时返回 `"unknown"`。** 位置 `upgrades.go` `getAgentStatus`。Python 是 `agent.get('status', 'unknown')`，只有缺失才用默认值，显式 null 原样返回 null；作者用 `rawAgentMember`（缺失和 null 都返回 `null`）后再把 null 换成 unknown，两种情况混在一起了。影响：只有导入/损坏的记录会出现，HTTP 类别不变。修复：用 `MemberJSON` 的 present 区分缺失和 null。回归 `TestReviewN810StoredMemberDefaults`（缺失→unknown，null→null，字符串原样）。 |
| R2 | low | 已修 | **流量记录路由：缺少 `service_type` 的 Agent 返回 400“流量记录只支持 mihomo Agent”。** 位置 `traffic.go` `mihomoAgentOrError`（GET/PUT/DELETE traffic-history、stats、trend 共用）。Python `_mihomo_agent_or_error` 是 `agent.get('service_type', 'mihomo') != 'mihomo'`，缺失按 mihomo 处理；迁移前 Go 用 `GetOr(..., "mihomo")`，与 Python 一致。typed 改写成 `agent.ServiceType != "mihomo"` 后，缺失也被拒绝。修复：缺失时放行；null 和错型（R5 回落为空串）仍然 400，与 Python 一致。回归同 R1 测试（缺失→200，null→400，oxidns→400）。 |
| R3 | low | 已修（补测试） | **新入口 `Manager.RunServiceOperation` 没有真实 Runtime 的关机/断线测试。** WORKFLOW 第四条常驻项要求：设备写入 owner 在 Detach 之后检查 `ctx.Err()`，并用真实 `Runtime.Start/BindRequest/Shutdown` 测试。代码本身正确（先 Detach、再检查 `ctx.Err()`；HA 路径在 lease 内还会检查 `held.Err()`），但作者的 dnsha 测试只覆盖了 lease 409。补测试 `TestReviewN810ServiceOperationShutdownAndDisconnect`（restart/uninstall × 关机/断线，表驱动）：关机后返回 `context.Canceled`，设备收不到请求；请求断开后仍然把 `POST /api/restart`、`/api/uninstall` 发到设备，并返回 success。负控：临时去掉 `RunServiceOperation` 的 `lifecycle.Detach`，两条断线用例失败（`context canceled`），还原后通过。 |
| R4 | low | 已补登 | **Agent 二进制下载的 OS 错误文本改了，但没有登记。** 位置 `downloads.go` `pyOSErrorText`，现在直接返回 `err.Error()`。500 的 message 从 `[Errno 2] No such file or directory: '<path>'` 变成 `stat <path>: no such file or directory`，is a directory、permission denied 两类也一样变了；状态码和字段不变。difftest `nativeFileDiagnostic` 只按错误类别比较。只在服务器上二进制缺失或权限错误时出现。已在 behaviour-changes 的 N8-10 api/agents 段补一行。`pyOSErrorText` 现在只是直通函数，可以在 N8-11 改名或删除。 |
| L1 | low | 记录（N8-11） | **旧 hook 只剩注册入口，已经没有生产调用点。** heartbeat 只调用 `AddressFollowTyped`/`AddressConfirmedTyped`；注册只调用 `InstallationProfileTyped`/`InstallationRegisterTyped`。旧的 `AddressFollow`/`AddressConfirmed`/`InstallationProfile`/`InstallationRegister` 现在只出现在 `hooks.go` 的字段、`RegisterHooks` 和测试里，测试里注册它们是为了断言“不会被调用”。`HAServiceOperation` 旧 hook 仍然是 `RunServiceOperation`/`serviceOperation` 的回落路径，但生产环境只注册 typed（`dnsha.installHooks`）。所以如果有人只注册旧 hook，heartbeat 和注册会**静默不调用**，而不是报错。grep 确认没有隐藏的生产调用方。与上一份复核的 L2 同类，N8-11 删除旧 hook 时一并处理，`agents/doc.go` 的 hook 清单也要同步补上 typed 变体。 |
| L2 | low | 记录 | **注册时 host 的“空值”按字面字节判断。** 位置 `agents.go` `doRegister` 的 `hostEmpty`，只认 `null`、`""`、`false`、`0`、`[]`、`{}`。Python 里同样为假的 `0.0`、`-0`、`0e0` 会被换成客户端 IP；Go 会报 500“registration host must be a string”。只有人工构造的非字符串 host 才会遇到，而且 Python 对为真的非字符串 host 本来就返回 500，属于同一个错误类别。Docker 地址识别改用 netip 已在 behaviour-changes 登记。 |
| L3 | low | 记录 | **安装器有英文字面量提示。** 位置 `installer.go` 的 `"Invalid installer integer"`、`"Invalid Agent port"`。这两段在 52dd0b884 之前就有，本轮只是从 installer 包挪到了 API 层，HTTP 400 不变。installer S 轮次时收进 `messages.go`。 |
| L4 | low | 记录 | **列表里心跳新鲜但缺少 `status` 的记录不带 `status` 键。** 位置 `content_routes_adapter.go` `listContentAgents`。Python `online_status` 返回 `agent.get('status')`，会写出 `"status": null`；Go 只在心跳过期时写入 `"offline"`，心跳新鲜时保持原记录，缺失就没有这个键。注册总会写 `status`，只有导入的旧记录会出现。 |

只作说明，不算发现：

- 卸载成功后的删除判断用 `success == true` 字面量，`resultResponse` 用真值判断，两处写法不一致。但 `RunDirectOperation` 的卸载结果只可能是 `remoteSuccess`（字面量 true）或 `success:false`，HA 拒绝也是 `false`，不存在 `1` 这类值，所以没有改。
- dnsha 旧 `ServiceOperation` 改成 JSON 编解码包装后，拒绝体的键序从插入序变成了 map 排序。Flask 的 jsonify 本来就排序键，而且 `rawObject(nil)` 和 `summary(nil)` 都编码为 `null`，`ha_publication` 不变。

## 逐项核对

1. **鉴权顺序**：heartbeat、traffic-report、traffic-history 上报、upgrade report 都是 `ReadAgent` → `agentTokenMatches`（原始 JSON string token、常量时间比较）→ 读 body → 校验 → 写入；鉴权失败时 401，不读 body。管理端路由都套了 `auth.Require`。注册：`RegisterIdentity` 先按 name+host 找到已有记录，再比较 token（`refreshRegistration`：token 不符时，匿名请求在未匹配 claim 时返回 403，否则 PermissionError）。新记录要先过 `admitRegistrationClaim`：匿名、无 proof、`AllowNew=false` 时直接 403，不做版本校验，所以不存在“版本先于鉴权”的探测口。proof 匹配之后才执行 `validate`，然后才从 working copy 删除 session；校验失败时事务回滚，proof 不会被消费。`AllowNew`（JWT/无鉴权/注册 key）也走 `authorized()` → validate。没有 Authorization 的入口（手动创建、安装器回放）统一做 `normalizeRegistrationVersion`。作者测试 `TestN810RegistrationVersionValidationFollowsAuthorization`、`TestN810ManualRegistrationUsesSharedVersionValidation` 覆盖了 403/400、proof 保留、null→1.0.0。
2. **typed hooks**：`InstallationProfileTyped(ctx, data, installationID, installationToken, fallbackHost)`、`InstallationRegisterTyped(ctx, m, id, token, data, existingToken)` 都是命名参数，`gateway/installation.Wire` 不做位置对调；`Register` 改为直接调用 `DecodeRegistrationInput` + `RegisterIdentity`，选项（ExistingToken、InstallationID、ReplayInstallation）与旧 `RegisterInstallationAgent` 一致。原始 JSON 只在 owner 边界解码。`TestN810InstallationTypedHooksKeepArgumentOrderAndRunOnce` 用 A/B/C 三个不同值验证顺序，并断言旧 hook 不被调用。`HAServiceOperationTyped` → `ServiceOperationJSON` 沿用原 `deploy.RunDetached` + lease + `active`/`haEnabled` + `held.Err()`，每次操作只触发一次 hook。
3. **If-Modified-Since / Range**（fork 复核）：只用 `http.ParseTime`；无效日期不作为条件，返回 200；If-Range 不是日期时按 ETag 处理（Werkzeug 一致）；416 规则不变。
4. **cache flush tags**：`[{}]`、字符串、数字等无法解码为 `[]string` 的值，以及 `null`（POST 时 `validTags(nil)` 为 false），都在请求 Agent 之前 400 + NoStore；不会退化成“清空全部缓存”。
5. **profile 绑定**：`clientAgent` 和 `loadContentSource` 统一走 `ResolveAgentProfile`；坏绑定返回 `clientBindingError` → `RepositoryErrorHandler` 400 + NoStore。traffic-report 在 `AddReport` 之前先解析绑定。policy peers 遇到坏条目跳过，并按 Agent 每 10 分钟限流告警（经 `redact.DiagnosticText`），节流表会清理过期项。note/policies 改为 `UpdateProfile`，只写 `device_notes`/`oxidns`，与旧 `notesChanged` 回调写的成员一致。A24 的关机测试（note/policy/domain/ip/toggle/clear）和断线完成测试仍然通过。
6. **公开投影**：`publicAgentWithCredentialsModel` 在删除 `token`/`applied_content`/`dns_follow_job` 之前，先对整条原始记录执行 `CollectCredentials`，删除之后再统一执行 `SanitizeCredentials`。summary 路径在读取历史之前收集凭据。列表的 offline 覆盖与 `OnlineStatus` 一致（L4 例外）。
7. **测试替身 / 原始字节**：没有发现替身改写被测生产参数（fork 复核了 installer fuzz，只推进请求 runtime 的时钟）。原始字节比对已有：`TestN810MalformedLoggingReplyIsStructured502WithoutWrite` 比对设备请求的 method/path/Authorization/Content-Type/body 字节（`{"enabled": true}`），restart 用例断言 body 为空、没有 Content-Type；installer 差分对脚本、headers、body 做字节比较。作者冻结的 SHA 绑定只替换按名字定位的行，并校验原输入和旧结果，原随机矩阵和 pyref entries 没有改动。
8. **风格**：新增的中文消息 `msgResetRequest`、`msgPolicyCorruptPeer` 在 `messages.go`；日志统一用 `logx`；注册日志字段先脱敏再截断。

## 复核运行（.64）

- 修复后：`gofmt -l internal/api/agents internal/agents` 无输出；`go vet ./internal/api/agents` 通过。
- `go test -race -p 4 -count=1 -timeout=15m -run "ReviewN810|N810Registration|N810Client|N810PolicyPeers|N810Cache|N810Installation|N810Traffic|N810MalformedRestart|TrafficHistory|ReviewA24ShutdownRejectsWrites|ClientNoteAndPolicyCancelledOwners" ./internal/api/agents`：`ok configflow/internal/api/agents 30.726s`。
- `go test -race -count=1 -run "N810NativeServiceOperation" ./internal/agents/dnsha`：`ok configflow/internal/agents/dnsha 1.382s`。
- 负控：临时恢复 HEAD 的 `upgrades.go`/`traffic.go`，并去掉 `RunServiceOperation` 的 Detach 后，
  `go test -p 4 -count=1 -run "ReviewN810StoredMemberDefaults|ReviewN810ServiceOperationShutdownAndDisconnect" ./internal/api/agents` FAIL：两条 disconnect 用例报 `context canceled`；`null status stays null` 得到 `"unknown"`。还原后通过。
