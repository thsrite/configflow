# A21 HR 复核（Linux 网关接入 onboarding）

复核者：新的 Opus（非作者），2026-11-18。分支 `go-opus-review-a21`，起点 `go-port-a21@26a8a649`
（作者 Codex，基于 `go@c5f5ff34`，A20/C34/C36/O3-6c 均已在内）。无子 agent、无 push、未访问生产/路由器/PVE；
测试只经 `rt` 固定 `RT_HOST=sunbroke@192.168.0.32` 跑，数据全部合成。

**结论：high 0 条，medium 2 条，low 2 条，另 1 条测试覆盖补充；全部已修复并带测试，0 未决。**
未达到「≥2 条 high」回退门槛。A21 状态改为 `verified`（待协调者批次 gate）。

## 方法

- 读 Python：`backend/common/gateway_onboarding.py`、`backend/routes/gateway_onboarding.py`、`setup_flow.adopt_owned_write`、
  `router_split.is_full_tun/read_marker`、`linux_gateway.direct_overlap_warnings/_rule_networks`、
  `oxidns_common_routing._references`、`gateway_installation`（A22 对 `assert_gateway_onboarding_ready` 的用法）。
- 逐分支对照 Go：`internal/gateway/onboarding`（input/state/prepare/native/dns/config/adapter/service）、
  `internal/api/gatewayonboarding`、`server/setup.go` 接线、`setup.AdoptOwnedWrite/RegisterExternalOwners`、
  `oxidns.ReferenceLocationsWithOptions/ForwardUpstreams`、`redact.SanitizeWithSecrets/CollectCredentials`、`model.Int`。
- 抽查作者证据：Flask HTTP、adoption revision 条件（含 +2 反例）、Runtime 关机/断连、DNS 回环零写入；
  用真实 Python 探针确认可疑差异，每个发现先写测试并确认修复前失败。

## 发现

### F1，medium，已修复：`AssertReady` 的校验失败不是输入错误

- Python `assert_gateway_onboarding_ready` 把 OxiDNS 设置、YAML、ValueError 等全部包装成
  `GatewayOnboardingError`（ValueError），A22 的 `routes/gateway_installations.py` 统一回 400 + 原消息。
- Go `AssertReady` 原样返回 `*oxidns.SettingsError`、`*yamlx.SyntaxError` 等；对外只有 `IsInputError`（只认
  `*gateway.Error`），分类函数 `validationMessage` 未导出。A22 按 `IsInputError` 映射时，用户可修的已保存草稿错误
  （例如 remote_dns YAML 损坏、Mihomo 基础 YAML 损坏）会变成 500。
- 真实 Python：两例均为 `GatewayOnboardingError`（`DNS 上游 YAML 语法无效`；PyYAML 解析消息）。
- 修复：`AssertReady` 外层用同一 `validationMessage` 把校验类错误包成保留 Cause 的 `*gateway.Error`；
  ctx 取消与基础设施错误保持原链。测试 `TestAssertReadyClassifiesValidationErrors`（修复前两例均失败）。
  YAML 消息文本与 PyYAML 不同属既有 YAML 消息类别。

### F4，medium，已修复：短凭据会把向导自身的键和字段替换成 `[REDACTED]`

- Go 的 `sanitize` 从整个配置收集凭据（节点 password、uuid、short-id、psk、API 密码等）后对**整个**状态/报告做
  子串脱敏；共享 sanitizer 会把「包含该值」的任意键或值整串替换。节点密码为 `dns` 时，GET 状态里的
  `local_dns` 值和 `oxidns_mode` **键名**都变成 `[REDACTED]`（实测），前端读不到表单值，回填保存会被拒；
  Python 原样返回这些字段（全局钩子只脱敏规则代理/Agent token）。
- 修复：分两级。系统密钥（rule proxy/config/retired token、fetch proxy、Agent token、注册密钥）仍整份脱敏；
  从配置发现的用户凭据只用于诊断字段（`blockers`、`warnings`、`message`、`validation`、`field_warnings`）及错误消息。
  结构字段（设置、候选、策略、changes）与 Python 一致原样返回。作者的 retired token 测试保持通过。
- 测试 `TestShortCredentialDoesNotCorruptWizardFields`（修复前失败：`"[REDACTED]":"template"`）。

### F2，low，已登记：原生备份内容（Python bug，Go 已正确）

- 原生备用链已停用、`_prepare_native_gateway_fallback` 恢复成功，但 `apply_common_settings` 仍拒绝（例如监听插件缺失）
  且用户确认转换模板时，Python 在 `settings` 已被替换为「已恢复」投影后才写 `gateway_onboarding_native_backup`。
  真实 Python 探针：备份 `native_config != 原文`，多出 forward_fallback/fallback_dns/fallback。
- Go 的恢复结果只在 apply 成功时替换 settings，备份是原文。作者未登记，已补 `behaviour-changes.md` A21 条目，
  并用 `TestConversionBackupKeepsOriginalNative` 固定。

### F3，low，已修复：R5 null 投影

- Python `item.get('policy', '')`、`settings.get('config_mode', 'template')` 对**存在但为 null** 的值返回 null；
  Go 投影成 `""` / `"template"`，把无效存储伪装成有效默认（真实 Python 探针确认两处差异）。
- 修复：`DomainCandidate.Policy` 为 `*string`（存储 null → JSON null，缺失 → `""`）；`State.OxiDNSMode` 按存储原值投影，
  缺失才是 `"template"`。`testdata/reference.py` 新增 `null-policy`、`null-library-behavior`、`null-oxidns-values`、
  `null-config-mode` 四行进入 `TestPythonPreparation`（前两处修复前失败）。
- 未改：Python 对 `name: null` 直接抛 AttributeError（500），Go 返回 blocker，属 Python 崩溃修复类。

### F5，low（测试覆盖），已补：Flask HTTP 差分缺少冲突类代表

- `TestFlaskHTTP` 原无 409（stale revision）、schema 前的 revision 比较（字符串 `"7"` → 409）、保存被 blocker 拒绝
  （400 + 报告）、不存在的 profile（GET/POST 404）。按 Leaner verification「每类错误一例」补 5 例，全部与真实 Flask
  在状态码、body、headers、SQLite/文件上一致（无产品改动）。

## 核对通过的项目（无发现）

- **revision+1**：adoption 在 repo L3 释放后、`saved._revision == before._revision + 1` 且 ctx 未取消时调用；before/after
  是完整存储 profile。`_revision` 为 null/缺失时 `model.Int` 解码失败 → 400 且不写（Python 会在提交后 TypeError 500）。
  作者的真实 Flask 对照（重复保存 +1 仍 adoption；before 后插入 metadata 写入 +2 两侧都不 adoption）抽查有效。
- **C36 接线**：`SetAdoptOwnedWrite(setup.AdoptOwnedWrite)`；`RegisterExternalOwners` 只合并非 nil DNS owner，保留 C32；
  `PrepareMihomoDNS` 薄闭包传 nil 地址 + 共享 `CheckDNSLoop`（= `gateway.MihomoDNSLoop`），无第二份 DNS 解析。
- **锁顺序**：A21 不取 session 锁；takeover 读 session 无锁；adoption 在 repo 释放后由 C36 自取 `LevelSetupSession`；
  符合 setup-session → device → repo。
- **Runtime**：保存 Detach；入口、事务前、回调内、Set 前、adoption 前检查 ctx；repo 最终 COMMIT 闸门。作者的
  关机前/生成中/提交点关机零写入、断连完整提交测试抽查有效。
- **DNS 回环**：预览 200 ready=false、保存 400，文档/revision/文件不变。
- **HEAD**：走 GET 只读（已登记 HEAD 只读类别）。
- **共享入口复用**：A19 `ValidateSelection/ValidateRuleSetIDs/EditorState/DirectOverlapWarnings`、A20/routersplit
  `ReadMarker/RouteRuleSetIDs/RouterSplitState/LegacyContext/FullTunBaseYAML`、C18 `GenerateMihomoConfig/DefaultMihomoTemplate`、
  C22–C25 `NormalizeSettings/NativeDocument/LoadNativeYAML/ReadCommonSettings/ApplyCommonSettings/ValidateSettings/Generate/
  ReferenceLocations`；新增的 `ForwardUpstreams` 与 `ReferenceOptions.AdditionalFields` 是薄导出，没有复制业务逻辑。
  `_references`/动态 `${}` 引用语义与 Python 一致（含 `$${` 转义、plugins.tag/type 排除）。

## 观察（不是发现）

- 并发两次 A21 保存时，两个 adoption 在锁外执行顺序不定，可能让 session 证明停在较早的一次（之后 C36 把较新那次
  视为外部修改，需要用户确认）。Python 同样如此，窗口极窄，不改。
- 关机发生在 profile 已提交、adoption 之前时，保存已落盘但 adoption 跳过并返回取消错误；这是「关机后不再写」的
  预期结果，重启后 C36 会把该次保存当作外部修改。

## 需要 L4 实机验证

1. 保存后的两份草稿经 A22 依次发布到真实 Linux 主机：Mihomo（fake-IP、DNS 127.0.0.1:1053、选择性接管/完整 TUN）与
   OxiDNS（0.0.0.0:53 UDP/TCP、IPv4 优先、代理与备用上游 127.0.0.1:1053）核心自检通过，`core_check_required` 闭环。
2. 原生 OxiDNS：已停用备用链被恢复、管理 API 自动开启在 127.0.0.1:9199 后，DNS 面板、规则更新、热重载可用；
   确认转换模板后从配置导出恢复 `gateway_onboarding_native_backup` 的原生文档可用。
3. 完整 TUN（主路由分流）：主路由只转发 fake-IP 与所选规则集，Linux 侧 IPv4 转发持久化与 TUN 生效。
4. A22 安装时注入目标主机真实地址后，指向本机 53/1053 的 Mihomo 上游被 `AssertReady` 拒绝；正常远端 DoH 放行。
5. 首次使用向导嵌入保存后，C36 check/finish 认定为向导自身修改（owned_digest 与 without_base 两份证明）。

## 定向检查（.32）

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -l internal/ tools/ cmd/ && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout 10m ./internal/gateway/onboarding ./internal/api/gatewayonboarding/... ./internal/server ./internal/setup && go test -run "^$" -fuzz "^FuzzGatewayOnboarding$" -fuzztime 60s -parallel 2 ./internal/gateway/onboarding'
```

gofmt 无输出，`go vet ./...` 通过。输出尾部：

```text
ok  	configflow/internal/gateway/onboarding	29.433s
ok  	configflow/internal/api/gatewayonboarding	7.283s
ok  	configflow/internal/api/gatewayonboarding/difftest	9.399s
ok  	configflow/internal/server	9.230s
ok  	configflow/internal/setup	51.732s
PASS                                   (FuzzGatewayOnboarding 60 s)
ok  	configflow/internal/gateway/onboarding	61.017s
```

没有运行完整 gate / proddiff（按 2026-11-18 规则由协调者批次执行）。

修复提交：`26feab070`（产品代码、测试、文档同一提交）。
