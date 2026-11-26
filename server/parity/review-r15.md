# R15 HR 复核（路由器 HTTP 入口 + 策略工作区 + 上游提醒）

复核者：新的 Opus 会话（未参与移植；R15 由 Codex 移植，提交 `f7931b2a4`、`c0a2de43a`）。分支 `go-opus-review-r15`，
从 `go-port-r15@c0a2de43a` 分出。只用 loopback fake 和合成数据，未接触生产、真实路由器、Agent、DNS 或 PVE；未 push。
按 Leaner verification：没有重跑作者的完整 Flask 对照套件，读了作者全部 R15 测试，只为自己的修复写定向测试（修复前失败、
修复后通过），再对改动包及共享 redaction 的直接使用方跑 `-race`。

范围：Python `backend/routes/routers.py`（R15 部分全部 handler、两个锁装饰器）、`routes/upstream_router_pending.py`、
`routers/policy_workspace.py`、`rule_sync_store.job_lock`、`route_editor.router_operation_lock`、`common/config_repository`
的异常层次、`oxidns_validation/oxidns_references` 的异常层次；Go `internal/api/routers/*.go`、`internal/routers/management*.go`、
`internal/routers/workspace.go`，以及它们调用的 `JobLock`、`RouterOperationLock`、`Publish`（入口与错误分流）、
`PendingCache`、`redact.SanitizeCredentials`、`diag.CredentialSpans/EventMessage`、`oxidns.IsSettingsError`。

## 结论

**0 条 high，2 条 medium（均已修），2 条 low（已修）。** 未达到「≥ 2 条 high」的回退门槛；PROGRESS 中 R15 改为 `verified`。
集成待办（O3-6b 唯一 RouterJobLock、A22 完整注册）不是发现，单列在下文清单。

## 发现

### 中

- **M1 — iKuai 探测失败文案里的管理地址被整段隐藏（Python 测试明确断言该地址可见，已修）。**
  `test_ikuai_connection_errors.py::test_probe_api_persists_explanation_for_router_list` 要求 probe 响应和 `last_probe.message`
  都包含「跳转到 https://<地址>」，这是告诉用户应改成哪个管理地址的唯一线索。Go 有两层把它抹掉：
  1. `ManagementService.Probe` 用日志用的 `safeLog` 生成要保存的 message，经 `diag.EventMessage` 把 URL 换成
     「[下载地址已隐藏]」并截断；Python 保存的是原样 `str(error)`。
  2. 共享 `diag.CredentialSpans`：`https://198.51.100.7:8443（HTTP 302）` 中 URL 后紧跟全角括号、没有空白，正则把
     `（HTTP` 一起吞进 URL，`url.Parse` 失败后按「无法确定边界」整段视为凭据，`SanitizeCredentials` 返回 `[REDACTED]`。
     这一层同样作用于所有 R15 公开响应（report/plan/apply 中出现同类设备文案时也会整段变成 `[REDACTED]`）。
  修复：probe 保存值只做凭据清洗（`safeText`），日志仍走 `EventMessage`；`urlCredentialSpans` 新增收窄的例外
  `proseAfterOrigin`：仅当 URL 是不带 userinfo/path/query/fragment 的纯 `scheme://host[:port]`、紧跟非 ASCII 标点、且该词余下
  部分不含 `@` 时才不隐藏，其余情况保持 fail-closed。
  证据：`api/routers.TestReviewR15ProbeRedirectKeepsManagementOrigin`（真实 Flask 对照：Go 响应与保存的 message 与 Python 逐字
  相同，且不含 Location 中的 token；修复前 Go 为 `[REDACTED]`）；`redact.TestReviewR15OriginBeforeChinesePunctuation`
  （两条可见 + userinfo、后续 `@`、query token、坏端口四条仍隐藏的负控）。

- **M2 — OxiDNS 校验/引用错误被当成 500（R05 错误类别，已修）。**
  Python `OxidnsValidationError`、`OxidnsReferenceError` 都是 `OxidnsSettingsError`（`ValueError`）子类，policy-settings PUT
  按 400 返回中文原因，preview 按 `ValueError` 400 返回。Go `failure()` 只认 `*oxidns.SettingsError`，`*oxidns.ValidationError`
  （native 草稿未通过校验，由 `Generate`/`ApplyPolicies` 返回）和 `*oxidns.ReferenceError` 落到通用 500，用户看不到原因。
  修复：`failure()` 改用已有的 `oxidns.IsSettingsError`（其他 API 包已这样做），消息取其 `Msg`。
  证据：`api/routers.TestReviewR15OxidnsAndRepositoryRejectionsAre400`（save/preview 两种覆盖状态、reference；普通 error 仍为
  500 的负控）；修复前前三项为 500。

### 低

- **L1 — policy-settings 未把 ProfileRepositoryError 映射为 400（已修）。** Python `policy_settings` 的 try 覆盖保存与读取，
  `ProfileRepositoryError`（含「配置数据库在事务外被修改…」与校验失败）返回 400 + 消息；Go 返回 500。修复：工作区 handler 在
  类型检查之后统一经 `repositoryFailure` 把 `*repo.Error` 映射为 400（仅此入口，与 Python 一致）。同一测试的 `repository` 子用例。
- **L2 — `internal/server/server.go` 未 gofmt（已修）。** 作者在远端 `gofmt -w` 后没有取回该文件，新增的两行是空格缩进、import
  未排序；`gofmt -l .` 非空。已格式化。

## 核对过无问题的项

- **Step B 三项**：HEAD collection 只读（`TestR15CRUDHeadAndAuth` 断言 revision 不变、无 body；`collection` 只有 POST 进入创建）；
  HEAD forwarding 只走 GET（Agent 无 POST，正控走真实 wire）；坏 verinfo / 损坏 Location 保存失败 probe、只改 last_probe、
  revision +1（`TestR15MalformedProbePersistsOnlyProbe`，含 Python 500 的实证）。
- **udp 关键字（C26）**：R15 没有自己的引用扫描。`ReadWorkspace` → `oxidns.ReadPolicies`，`SaveWorkspace` →
  `SavePolicyRows` → `ApplyPolicies`，正是 C26 修过并由 `TestStepBC26IndependentSidePolicyLifecycle` 覆盖的函数，R15 直接继承。
- **锁顺序**：PUT/DELETE、probe、apply、routes/apply、policy-settings PUT、rule-sync PUT 在 lookup/body 校验之前取 L1（忙 409
  优先，`TestR15JobLockPrecedesValidation`）；GET 与 rule-sync POST 不取 wrapper 锁（POST 由 `StartJob` 自取），与 Python 装饰器
  一致；L1 key 与 Python `job_lock` 相同（有记录时为 type+endpoint，否则 profile+id）。L2 后重读完整原始记录比对，409 文案一致。
- **第四条常设项**：Create / Save / Probe / SaveSyncSettings / SavePolicyWorkspace 都在 Detach 后检查 `ctx.Err()`，事务回调
  返回 `work.Err()`，`saveManagementSettings` 写前检查；作者用真实 Runtime 测了 probe/publish 的断连与关机。
- **错误分流**：report/plan RouterError（含 conflict）→ 502；preview → 400；routes plan/apply conflict → 409、其他 → 502；
  rule-sync PUT RouterError → 409、校验 → 400；apply 的 Busy/Blocked/Stale/RouterChanged → 409，设备错误 → 502，
  空计划响应不含 message，与 Python 一致。删除守卫用 `State()`（Python 真值语义）。
- **上游提醒**：`refresh` 只认 `1`/`true`，超时 504 + no-store，凭据种子覆盖全部配置空间（未登录仍 401）。
- **常设项 1/2**：没有新的设备 client；routerfake 未覆盖生产超时；forwarding 有原始 body/头对照。
- **常设项 3（共享路径）**：M1 改了共享 `diag.CredentialSpans`，已在合并前树上跑 redact、pushdiff、agents、api/profiles、
  api/rules、gateway/draft、gateway/host、setup、server 的全部 `-race` 测试。

## 集成清单（不是发现）

1. `internal/api/routers/handlers.go` 的 `job()` 和 `settingsJob()` 调用过渡期的 `core.JobLock`（R14 `syncJobs`，
   `lockx.Keyed` "router-job"）；O3-6b 合入时替换为唯一 `RouterJobLock`，保持「有记录用连接 key、否则 profile+id」与「锁先于校验」。
2. `RuleSyncService.StartJob`（rule-sync POST）内部同样用 `syncJobs`，需与 1 一起换成同一把锁。
3. R15 **没有**私有锁或私有 syncJobs：L2 用共享 `RouterOperationLock`（R06），`PendingCache` 只有只读 singleflight
   （`lockx.Flight` "upstream-pending"），不是路由器任务锁；每个 App 在 `Register` 中创建一份，Spawner 为
   `lifecycle.DefaultSpawner`，接入时确认它随 App Runtime 关机。
4. C03 批量入口（profile 导入、整体导入、重置、删除空间）对已有归属/首次写入窗口的拒绝，接入统一锁后需复跑
   `TestR15PublishHTTP*`。
5. A22 gateway-installations 的真实注册合入后，复跑 `TestR15FlaskRoutePriorities` / `TestR15RealFlaskRoutePriorities`
   和 `server.TestNewAppRegistersImplementedRoutes`。

## L5 实机项

- 真实 iKuai 在 HTTP→HTTPS / 改端口时的 302：probe 文案显示正确的管理地址，保存后路由器列表显示同一文案。
- 真实 RouterOS 7.22+ 的策略对象查看（`/policy`）在发布前后、外部改动后的 409/502 文案。
- 真实 OxiDNS native 草稿校验失败时，策略工作区保存与发布预览显示 400 原因（M2）。
- 手动规则同步 202 后的 queued/running/synced 阶段与实际设备结果。

## 命令

见 PROGRESS-routers 的 R15 复核段落。
