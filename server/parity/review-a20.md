# A20 HR 复核（Agent gateway bridge、gateway draft、gateway routes）

复核者：新的 Opus（非 A20 作者），2026-11-18。分支 `go-opus-review-a20`，起点 `go-port-a20@4dbf75cf`
（基于 `go@dda8973d`）。无子 agent、无 push、未访问生产/路由器/PVE；测试只经 `rt` 在测试服务器跑，
设备一律用 loopback fake 和合成数据。

**结论：high 0 条，medium 1 条，low 3 条，均已修复并带测试；0 未决。** 未触发「≥2 条 high」回退。

## 方法

- 完整读 Python：`backend/agents/linux_gateway.py`、`backend/routes/linux_gateway_agents.py`、
  `backend/common/linux_gateway_draft.py`、`backend/routes/linux_gateway.py`，以及被调用的
  `router_split.legacy_context/_session_context/legacy_source/legacy_gateway_agent_ids`、
  `setup_flow.profile_sessions`、`routers.store.routers_of`、`linux_gateway.validate_gateway_selection/
  gateway_settings/direct_overlap_warnings`。
- 逐分支对照 Go：`internal/gateway/host`（host/filter/forwarding/bundle）、`internal/gateway/draft`
  （draft/adapter）、`internal/api/linuxgateway`、`internal/api/agents/gateway.go`、`internal/setupflow`、
  server 注册；读了 `redact`/`diag` 的凭据规则、`httpx` 的 Bind/BindSilent/HEAD 路由语义、`yamlx` 的
  alias 预算与 `ValueHash` memo、`agents.Client` 的 MaxBody。
- 每个发现先写测试，在 .64 上确认修复前失败，并用真实 Python 确认原版行为；修复后把四个修复都加入
  `scripts/a20-mutation-gate.py`，12/12 能被检出。

## 发现

### M1，medium，已修复：HEAD `/api/agents/gateway-forwarding` 会写设备

- 场景：httpx 与 Werkzeug 一样用 GET 路由服务 HEAD。`gatewayForwarding` 只把 `GET` 当读取分支，
  其他方法都进入 POST 分支；带 `{"agent_id":"…"}` JSON body 的授权 HEAD 会调用
  `PersistForwarding`，向 Agent 发 `POST /api/linux-gateway/forwarding`，改变主机的 IPv4 转发持久化。
  Python 原版同样如此（Flask HEAD → `request.method == 'HEAD'` → `get_json` → 持久化），与 C32
  的 HEAD DNS 写入同类。
- 证据：修复前 `TestReviewA20HeadGatewayForwardingIsReadOnly` 失败：
  `HEAD wrote to the Agent: [POST /api/linux-gateway/forwarding]`。真实 Flask
  `TestA20PythonHEADForwardingWriteIsFixed` 断言 Python 确实发出 forwarding POST、Go 只做读取、
  完整存储快照不变。
- 修复：HEAD 走 GET 分支（`api/agents/gateway.go`）。其余 A20 路由已核对：编辑器路由按
  `POST` 判断写入，HEAD 为读；`/api/agents/<id>/linux-gateway/<op>` 的 HEAD 在 `allowed` 白名单
  前返回 `NOT_ALLOWED`，无设备请求。登记于 `behaviour-changes.md`（Python bug 修正）。
- 提交：`7c4ad2bb`（修复与测试）、`be8ec9c7`（变异门禁 head-read-only）、`8a3725df`（behaviour-changes 登记）。

### L1，low，已修复：预览字段遮盖比 Python 窄

- 场景：Python `_safe` 对键名**包含** `password/secret/token/authorization/private-key/private_key/uuid`
  的值显示 `[已隐藏]`。移植改为只用共享 `diag.IsCredentialKey`（精确名 + 少数后缀），
  因此 `secret_key`、`api_tokens`、`vmess-uuid`、`X-Authorization`、`PasswordHint` 等键的值在预览
  diff 中以明文显示，比原版弱。常见 Mihomo 字段（password/uuid/private-key/psk/auth-str…）两边都隐藏，
  所以实际影响限于非常规字段名，定为 low。
- 证据：修复前 `TestReviewA20DisplayHidesPythonCredentialKeys` 六个子用例全失败；.64 上真实 Python
  `_safe` 对这六个键全部输出 `[已隐藏]`、`username` 保持可见。
- 修复：`draft/adapter.go` 的 `hiddenKey` = 共享 `IsCredentialKey` ∪ Python 的子串词表，只用于
  显示遮盖（共享 redactor 未改，没有新增凭据 grammar；跨字段引用的种子收集仍用共享规则）。
- 提交：`79c36b35`。

### L2，low，已修复：恢复快照字段大小写不敏感

- 场景：`point()` 用 `json.Unmarshal` 到结构体，encoding/json 的字段匹配大小写不敏感。存储中的
  `{"ID":"…","Mihomo":{…}}` 被当作有效快照（Python `dict.get('id')` 判无效）；而
  `{"id":"p1","ID":""}` 或 `"mihomo":{},"Mihomo":null` 中后出现的变体会覆盖有效值，使 Python 认为
  有效的快照被判无效，进而在保存时报「原有网关恢复快照无效」。违反移植者自述的 exact-case 原则。
- 证据：修复前 `TestReviewA20RestorePointMembersAreExactCase` 四个用例失败；真实 Python 对两类输入分别
  返回 `None` 与有效。
- 修复：`id/created_at/mihomo/oxidns` 全部从原始对象按精确键读取。只影响手改/损坏的存储，
  ConfigFlow 自己写的快照不受影响。
- 提交：`79c36b35`。

### L3，low，已修复：forwarding 回复缺 `managed` 时误报「有网关记录」

- 场景：`PersistForwarding` 用 `Reply{"managed": data["managed"]}` 构造状态，回复没有 `managed`
  时键存在但值为空；`HasRecord` 只把缺键或 `null` 当无记录，空值走到「格式错误 = 不可读记录」，
  返回的 `gateway.gateway_record` 为 `true`。Python 读到 `None`，判无记录。只影响 Agent 页卡片的
  显示字段，不影响拒绝/放行判断（`state` 不依赖它）。
- 证据：修复前 `TestReviewA20PersistEntryWithoutManagedHasNoRecord/absent` 失败；真实 Python
  `_entry_from_status(..., {'managed': None})['gateway_record']` 为 `False`。
- 修复：`HasRecord` 把空值与缺键同等对待；格式错误的 `managed`（字符串等）仍算不可读记录
  （fail-closed 不变，测试覆盖）。
- 提交：`46dc9c06`。

### 杂项（无行为变化）

- `server.go` 的 wiring 注释被错位（`gateway.Wire` 注释落到 `setupflow.Wire` 上），已更正：`dc60b1ae`。

## 逐项检查（无发现）

1. **凭据脱敏**：Agent 回复与 GATEWAY_ERROR 文本统一过 `redact.SanitizeCredentials`（含 Agent token
   与环境 registration key、二次遍历跨字段引用）；forward_filter 在脱敏后再按白名单收窄；
   `msgOffUnknown`/发布拒绝里的设备消息来自已脱敏的 `monitor.Error`。预览：先从完整
   current/candidate 和两份生成文档收集种子，再投影；URL 去 userinfo/fragment、query 变 `…`；
   字符串经共享 sanitizer（含 opaque path token 与编码变体）；字典键与 change path 经种子再扫；
   生成错误文本同样清洗；恢复摘要只露 id/time。日志只记 agent id（`logx.LogText`），A20 包内无其他日志。
   编辑器 GET 状态与 overlap 警告属 A19，内容不含凭据，与 Python 一致。
2. **草稿生命周期**：preview token = 去掉 agents/backup 的完整输入 + revision + action + base URL，
   在事务内对当前数据重算并常数时间比较；`TestBackupRuntimeDoesNotInvalidate…` 证明心跳/备份不使预览
   失效，两个并发保存一成一冲突（`repo.ErrInUse`）；恢复在第一次 guard 放行后才读快照，恢复后快照随
   旧 mihomo 一起消失，与 Python 相同。未加引导保存保持旧接口语义（last writer wins，与 Python 同）。
3. **写 owner**：`Bridge.Call`（非 GET）、`Request`（非 GET）、`PersistForwarding`、`draft.Execute`
   （保存/恢复）都在 owner 内 Detach，随后 `ctx.Err()`；设备写在 `Do` 前、repo 写在事务前与回调首尾
   再检查。真实 `Runtime.Start/BindRequest/Shutdown` 测试覆盖 apply/remove/forwarding/persist/raw、
   保存与恢复的「关机零副作用 / 请求断开仍完成」，以及「预检与 POST 之间关机」「事务内生成后关机」。
   预检 Agent 调用在 L3 外；预检里的非用户错误被吞后由事务内 `msgConcurrent` 拒绝（与 Python
   不同的只是消息，零写入）。
4. **setupflow reader**：读目录失败、打开失败、>16 MiB、非 UTF-8/非法 JSON、`profile_id`/`goal`/
   `updated_at`/`abandoned`/`completed` 错类型都 fail-closed（`ErrReadSession` → 中文 500）；文件消失、
   非记录文件名、非对象 JSON、他人 profile 按 Python 跳过。接口与 A14 `go-port-a14` 的
   `setupflow.ProfileSessions(ctx,dataDir,profile)` 同签名，A14 的 `IsPreparationFailure` 依赖的
   `ErrInvalidSession` 仍在链上。**合入 A14 时**：删掉 `deploy.Wire` 里重复的 `SetProfileSessions`，
   只留 `setupflow.Wire`；`deploy.SetGatewayGuards` 的两个回调用
   `(&host.Bridge{Repository: m.Repository(), Manager: m}).RequireForwardingKept(ctx, t.ID, t.ProfileID)`
   与 `RequireGatewayOffKept`（`*agents.Manager` 已满足 `host.Manager`），注册点放在 server 的
   wirePackages（host 不能反向依赖 deploy）。当前基线没有 A14，故未注册是预期状态。
5. **路由**：编辑器四组路径 + 两种根都 `auth.Require`；provider 路由在 `auth.go` 公共白名单内，handler
   内做 token/JWT；HEAD 见 M1。请求绑定：编辑器 `BindSilent` 到 map（= `get_json(silent=True)` +
   dict 检查）；设备操作先 4096 字节上界，再强制 JSON 媒体类型 `BindSilent` 到 RawMessage
   （= `json.loads(raw)`，空 body 为 `{}`）——Bind 会重新编码，转发给 Agent 的 body 是规范 JSON，
   不会原样透传用户字节；gateway-forwarding POST `BindSilent` + 恰好一个字符串 `agent_id`。注册冲突由
   `TestGatewayRegistrationAndPrecedence` 在完整应用上验证。
6. **YAML/JSON**：生成文档、bundle 主文件都走 `yamlx.Parse`（重复键、环、yaml.v3 alias 预算）；
   `changes` 的 `ValueHash` 带 memo，递归只进入不同的映射，开销受 alias 预算 × 深度约束；展示用
   `ToJSON`（同一预算）。没有回退到第二套解析器：生成失败按 Python 的两条分支（目标 400、当前→警告）。
   请求/存储 JSON 用 encoding/json 与 `model.Int`/`httpx.Number`。
7. **登记差异**：Agent 回复脱敏、错类型 managed 视为不可读记录、错类型向导字段拒绝、错类型
   mihomo/oxidns 段拒绝、非有限 JSON 400、preview token 原生编码、快照 version 用 model.Int——都是
   fail-closed 或临时身份，合理。新增 M1 一条。

## 其他观察（不改）

- `ParseSpec` 只接受 JSON 字符串字段并用 strict IPv4；`emptyBody` 依赖 Bind 规范化后的 `{}`，路由外
  若有调用方直接传 `{ }` 会被视为非空，目前没有这样的调用方。
- 保存时 mihomo 顶层键在存储 blob 内按字母序重写（Python 保留插入顺序）；SQLite 字段位置和结构化值
  不变，API 输出 Flask 本就排序，无功能影响。

## 命令与结果（.64）

修复前复现：

```sh
rt -- bash -lc 'cd server && go test -count=1 -run "TestReviewA20" ./internal/api/agents ./internal/gateway/draft'
# HEAD wrote to the Agent: [POST /api/linux-gateway/forwarding]
# TestReviewA20DisplayHidesPythonCredentialKeys: 6/6 FAIL; TestReviewA20RestorePointMembersAreExactCase: 4 FAIL
rt -- bash -lc 'cd server && go test -count=1 -run TestReviewA20 ./internal/gateway/host'
# TestReviewA20PersistEntryWithoutManagedHasNoRecord/absent: GatewayRecord:true
```

修复后定向：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go vet ./internal/api/agents ./internal/gateway/... ./internal/api/linuxgateway/... ./internal/server ./internal/setupflow && go test -race -count=1 ./internal/api/agents ./internal/gateway/... ./internal/setupflow ./internal/server && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/linuxgateway/difftest'
```

```text
ok  configflow/internal/api/agents              265.227s
ok  configflow/internal/gateway                  13.503s
ok  configflow/internal/gateway/draft             4.816s
ok  configflow/internal/gateway/host              8.735s
ok  configflow/internal/setupflow                 1.027s
ok  configflow/internal/server                    1.855s
ok  configflow/internal/api/linuxgateway/difftest 33.799s
```

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && python3 scripts/a20-mutation-gate.py'
# … head-read-only / preview-mask-words / restore-exact-case / absent-managed: DETECTED
# A20 mutation gate: 12/12 detected; sources restored
```

完整门禁（代码 `8a3725df`，其后只有本报告/PROGRESS 文档提交；.64 槽位 1，未排队，04:51–05:13）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'server/scripts/gate.sh -p 6 FuzzGatewayDraft:./internal/gateway/draft FuzzGatewaySpec:./internal/gateway/host FuzzGatewayForwardFilter:./internal/gateway/host FuzzGatewayBundle:./internal/gateway/host FuzzSetupSession:./internal/setupflow FuzzExtractCIDRs:./internal/gateway'
```

```text
== gofmt / vet / build tags / proddiff observer self-checks
== race + proddiff (parallel)
baseline: new=0 stale=0
== difftest   （15 个包全部 ok，含 api/linuxgateway/difftest 16.898s、api/agents/difftest 216.600s）
== fuzz (6 targets, 60s each)  FuzzGatewayDraft/Spec/ForwardFilter/Bundle/SetupSession/ExtractCIDRs 全部 ok
GATE PASS
```
