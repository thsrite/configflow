# HR 复核：O18 T1a（顶层记录改普通结构体 + 逐行读写）

分支 batch-30（= go-o18-t1a，go@b408224bf 之上 ea9138e5b…8fdf655fb），复核 2026-11-22，测试在 .64（锁问题压测在 .32）。
按 WORKFLOW「Leaner verification」：不重跑作者全套对照，只跑自己修复涉及的包、相关 difftest、真 Python CrossCheck 和 preflight，
可能空过的守卫各做一个负向对照。

## 发现

### 高

**H1 无关写入把损坏的 system_config 改写成空令牌的新对象（已修，f0833341b）**
`transactLocked` 每次提交都调用 `ensureSystemTokens`（`applySystemIn` 里也调用）。T1a 之后 `system_config` 永远编码成对象，
旧的「不是对象就跳过」判断失效：库里 `system_config` 为 `null`/数字/字符串/列表时，任意无关写入（改配置空间描述、Agent 写入等）
都会生成新的 `rule_proxy_token`，`keepUnchangedRows` 看到值变化，写入 `{"retired_rule_proxy_tokens":[],"rule_proxy_token":"…"}`。
损坏的配置令牌因此变成「未设置令牌」，未开登录时配置接口匿名可访问；`SystemSettingsDamaged` 也随之变为 false。
作者只拦了启动路径（`openLocked`）。复现：`repo.TestReviewO18T1aUnrelatedWriteKeepsDamagedSettings` 修复前 4/4 子用例失败。
修复：`keepDamagedSettings`——基线设置损坏、且本事务对设置除令牌维护的两个成员外没有改动时，恢复基线设置并跳过令牌维护；
用户显式保存设置（以及 setup 向导生成配置令牌）才会修复（`TestReviewO18T1aExplicitSettingsSaveRepairsDamagedSettings`）。
copy gate 加了带理由的豁免（caab298e2）。

**H2 自带快照的令牌校验入口不检查设置损坏（已修，003d32340）**
`auth.ValidateSystemTokenOrJWT` 只在 `config == nil` 时查 `SystemSettingsDamaged`。实时配置 `/api/config/...`（`api/config/live.go`）
和 Linux 网关 provider/source（`api/linuxgateway/routes.go`）传入自己的快照，损坏设置读成空设置 → 未开登录时直接放行生成的完整配置。
修复：两种调用都查仓库的损坏标记（另一代快照只可能是用户显式保存后的设置）。
负向对照：`auth.TestSystemTokenRefusesDamagedSettingsFromCallerSnapshot` 在旧代码上 4/4 返回 200。

### 中

**M1 严格解码的切片 cap 未裁剪（已修，1e4ec8390）**
`decodeSection`（`DecodeInput`、`DecodeDocument`、`Document.UnmarshalJSON`）直接放入 v2 解出的切片：3 个策略组 cap 4、9 个 cap 16；
只有 `decodeMember`（SetJSON）裁剪。现有 `repo.TestO10R2SnapshotSlicesHaveNoSpareCapacity` 只看存储路径（`decodeRecordList` 本来就精确分配），
把 `clipSlices` 改成空函数它仍通过——对 clip 本身是空过的。目前这些解码结果都是事务/导入的私有副本，未进借用快照，所以是中而非高；
T1b 迁移嵌套记录后存储路径也会经 v2，届时这条测试才有意义。新增 `model.TestReviewO18T1aInputListsHaveNoSpareCapacity`
（SetJSON、DecodeInput、DecodeDocument；关掉 clip 时失败）。

**M2 `TestPooledReadsRaceWithCommits` 的 database is locked：测试问题（已修测试，5565ea800）**
判定方法：.32 上 6 路 `dd conv=fsync` + 12 路 CPU 空转（负载 30–44），同一用例 `-race -count=20`：
连接池开启 3/20 失败、`maxIdleReaders=0`（每次新开连接，等同池化之前）5/20 失败，错误都在读端（readers_test.go:175）。
即读端在 2.5 s busy_timeout 内等不完写端 EXTRA fsync，与读连接复用无关；复用连接的 busy_timeout 与新连接相同
（`reader` 还校验 `c.timeout == timeout`），回滚日志模式下空闲连接不持锁。该用例只检查代数单调，改用 60 s 锁等待：同样压力下 0/40。
生产按 `LockTimeout` 等待，行为与池化前一致，不改 store。

### 低（未改，记录）

- L1 （已改，9ba989ad3）`input_errors.go` 与 `validateDocument` 共用的中文提示改为 `messages.go` 常量，文案不变。
- L2 （已补，0392a83d6）路由级测试：`api/config.TestImportRefusesWrongTypedMembers`（整份导入，`description`/`created_at`/
  `agent_registration_sessions` → 「X 类型不正确」）、`api/profiles.TestProfileImportRefusesWrongTypedMembers`（配置空间导入，
  Python 形状文案）；两者都确认未写入。负向对照：`decodeStrict` 换成宽松存储解码时 7/7 返回 200。
- L3 宽松读的列表（一个元素被拒 → 整列零值）在用户随后添加元素时整列被新值替换，原错值丢失。与 R5 原行为相同，已在行为变更里体现，不是回归。
- L4 未声明成员守卫 `TestPlainRecordsDeclareEveryStoredMember` 只能从 Python 参考文档里抓「Go 漏声明」（负向对照：把
  `client_fixed_ips` 标签改名后守卫在 `python reference.profiles[]` 报错）；「新库导出」那一半对漏声明天然空过（Go 不会写出未声明成员）。
  前端新增顶层成员不经过这条守卫：配置空间保存只合并 `ProfileFields` 列表里的成员（T1a 前后相同），Go 代码 `SetJSON` 未声明成员会直接报错。
  所以守卫的实际作用是「Python 写过的顶层成员都已声明」，名字可以照此理解。
- L5 `Update` 的文档注释仍写「untouched members keep their stored bytes … only Clear removes」，已按逐行写入更新（eb4899cbb）。

## 逐项结论

1. **存储兼容**：`keepUnchangedRows` 的基准是 `transactLocked` 在 `.config.lock` 内读到的 `base`（`base.Entry` 行与 `base.Model` 同一代解码），
   提交时 `CommitContext` 在 `BEGIN IMMEDIATE` 里比对 head，不一致即冲突返回，不会用旧快照写入。三方合并、导入（`ImportAllJSON` 也走 `transact`）、
   令牌维护都在同一事务里改工作副本，比较的是事务前后的类型化值。建库/迁移用 `createStore`（无基线，全量写）。
   store 的 `diff` 对目标里缺失的行发 DELETE，所以 omitzero 成员清空、未知成员确实落库删除；行 `revision` 列只在该行文本变化时更新为新代号，
   保留的行保留原 revision。真 Python：`TestCrossCheck*`（FreshInit/Operations/Interchange/Lock/WriterKilled/C04）、`TestStoredRoundTrip`、
   `TestReviewN85PythonFullStorageDifferential` 均 PASS。
2. **宽松读 vs 严格输入**：HTTP/MCP/导入写入口经 `SetJSON`/`DecodeInput`/`DecodeDocument`（严格）；记录的宽松 `UnmarshalJSON` 在非测试代码里
   只用于存储读取和 Go 自己产生的数据的往返（routers/clients/dnsfollow/clientdevices 里的 `json.Unmarshal(…, &model.Profile)` 都是对 Go 编码结果的往返，
   嵌套记录仍是 R5 记录）。错型数据读成零值后，无关写入的新值与基线编码相同，`keepUnchangedRows` 保留库里原文本，不会被改写成 0；
   只有用户把值改成别的值才写。显式 `enabled:null` 读作关有测试（`TestModelRowsUnknownMembersAndNullSwitch`）。
   例外正是 H1（令牌维护会改值），已修。
3. **system_config 安全读取点**（全部列出）：
   - `auth.ValidateSystemTokenOrJWT`：config_token / rule_proxy_token / retired 列表。调用方 `api/aggregations`、`api/subscriptions`（读仓库）、
     `api/config/live.go`、`api/linuxgateway/routes.go`（自带快照，H2 已修）。
   - `mcp/transport.authenticate`：已查损坏标记（View 快照与标记分两次读，两边都只会拒绝）。
   - `mosdns/ruleproxy.authorized`：只接受非空的 rule_proxy_token / config_token，损坏 → 空 → 拒绝（失败即关）。
   - `repo.RuleProxyTokensForSanitization`、`redact.ContainsInternalRuleProxyToken`、`importexport` 的令牌退役：损坏时集合为空，只影响脱敏/退役，不开放访问。
   - `setup/service` 的 `storedConfigToken`：损坏 → 生成新配置令牌（显式写入，结果有令牌）。
   - `api/settings` 配置令牌 GET/POST：GET 返回空，POST 为显式保存（修复）。
   - `gateway/adapter`、`convert/*` 把 config_token 拼进 URL：仅输出。
   - 登录（`auth.IsAuthEnabled`）读环境变量的管理员账号，不读 system_config；备份 WebDAV 凭据在 `system.backup`，损坏只会让备份失败。
4. **未知成员**：加载告警 `msgModelUnknownMember` 只给路径模式和个数（`TestModelIssuesLoggedOncePerRevision`）；守卫负向对照见 L4。
5. **旧 rules/rule_sets**：生成器、mosdns、oxidns、ruleutils、deploy 由 `Fields.Get` 改为 `Profile.LegacyRuleList`，存储里的 `null` 仍读作「存在」
   （`json.RawMessage("null")` 非零，omitzero 也照写），`SetJSON` 对 RawMessage 不拒 null；读取路径不变。
6. **借用约定**：见 M1。

## 提交（batch-30，8fdf655fb 之后）

- 003d32340 go(auth): system token check refuses damaged settings for caller snapshots too
- f0833341b go(repo): unrelated writes keep damaged settings
- 1e4ec8390 go(model): strict section decode clips slices too
- 5565ea800 go(store): pooled-read race test waits long enough for loaded fsyncs
- caab298e2 go(repo): copy-gate exemption for keepDamagedSettings
- 6bf39b174 go: pyref source metadata re-recorded for model/document.go（13 个文件只有 `metadata` 键变化，条目逐字节相同；check 模式 0 differ）
- eb4899cbb go: behaviour note for damaged settings; Update doc follows row-wise writes

## 验证（.64）

- `gofmt -l internal/ tools/ cmd/` 无输出；`go vet ./...` 通过。
- `go test -race -count=1 -p 6`：model、repo、auth、store、mcp、api/config、api/aggregations、api/subscriptions、setup 全部 ok
  （repo 180.6 s）。
- `CONFIGFLOW_DIFF=1` + 真 Python（环境变量同 gate-live.py）：repo、model、devicenotes/…、api/config/…、auth、mcp、oxidns、mosdns ok；
  CrossCheck/往返见上。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-30 -p 6`（.64，负载 4.4，树为 6bf39b174；之后只有注释/文档提交）：
  vet 两套 tag 通过，全量非 race `PREFLIGHT PASS`。
