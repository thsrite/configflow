# N8-4e/f HR 存储类型复核（Agent 日志/回执，Router ownership/基准）

状态：**verified**；N8-4e/f 未关闭问题 **0**。分支 `go-opus-review-n8-4ef`（从 `go-n8-4ef` 分出，最终 rebase 到
`go` 64815165）。复核人不是作者（作者为 Codex，`go-n8-4ef`：N8-4e `d3d51e4b`、N8-4f `c565ef17`，rebase 后为 `dd6986df`、`2b5c6af7`）。
只用合成数据；生产快照只读，只在测试服务器私有副本上读取，输出只含字段名/计数，跑完私有文件数为 0。

## 发现与修复

| 严重程度 | 发现 | 修复 / 证据 |
|---|---|---|
| high（HR 接口，N8-4f 约束 3） | 设计要求「R5 回退得到的空值不能当作有效 ownership 证明」，但 model 包外**没有任何办法**区分回退值和真实的空/缺失：`Raw` 是未导出成员，`valueIssues` 未导出，issue 只能经 `DecodeDocument` 以整份文档路径拿到，而 N8-5 的 `Profile()`/`View` 交给消费者的是单个记录。结果：`"ros_policy_state":"x"` 和缺失都读成 nil，`"resources":"x"` 和 `[]` 都读成空列表，`"id":7` 读成 `""`，R08–R12 只能把它们当成「没有自有对象」。作者的 `TestOwnershipFallbackIsDiagnosed` 用的是包内函数，证明不了消费者能做到 | 新增 `model.Issues(v)`（`v` 及其子树解码时记录的 issue，路径相对 `v`，只含 schema 名和下标；Go 中新建的记录没有 issue）和 `model.IssueUnder(issues, path)`（按整段匹配，`ipv6_pd_guard` 不会命中 `ipv6_pd_guard_state`）。`TestIssuesSeparateFallbackFromEmptyProof`（9 例 + 子树/新建记录/Agent Map 不泄露用户键）。设计 n7-n8 §2.3「Settled by the N8-4e/f review」与 r12-release「Clarified in N8-4f」写明：设备操作前对清单成员 `IssueUnder` 为真即拒绝 |
| low（字段完整性） | `IPv6Verification` 缺 `error_code`：`ipv6_verification._unavailable` 写入 `'error_code': error.code` | 新增 `ErrorCode *string`。`TestHRReviewMembers`；Python 双向和 fuzz 种子随 storedRecordTypes 自动覆盖 |
| low（字段完整性） | `DeploymentManifest` 缺 `final_config`：`generated_preview.finalize_bundle` 设置 `manifest['final_config'] = True`，该 manifest 会进入 Agent rule-sync 回执和 HA payload 的 bundle，`_publish_deployment` 按它分支 | 新增 `FinalConfig *bool`。同上 |
| low（presence） | `DNSGuardSpec.Exempt` 是 `[]string`，但 Python 按存在性分支：`spec['exempt'] if 'exempt' in spec else _exempt_hosts(spec['bypass'])`（旧清单没有该成员）。用 nil/空切片区分存在性很脆弱 | 改为 `*[]string`。`TestHRReviewMembers`：缺失 → nil，`[]` → 非 nil 空，均逐字节往返；Go 显式写 `&[]string{}` 输出 `"exempt":[]` |
| medium（N8-5 接口，按协调裁定本步不改 codec） | 写入意图缺「清除」：R2/R5 下指针置 nil 会保留存储的 `null`，也会保留错误类型的原字节（`unchanged = invalid && isZero`）。Python 的 `pop`（`save_router_publication` 传入假值、`record_deployment` 删除 `applied_content` 等）能删掉格式错误的成员；Go 不能，格式错误的清单会一直存在，而按上一条 R12 会一直拒绝发布 | 已写入 n7-n8 §2.3：N8-5 的显式 clear 必须也能删掉 null/错误类型的原值。N8-4 不加 API（与「Clarified in N8-4f」一致） |

其余检查无问题，见下。

## 逐项结论（任务重点 1–6）

1. **字段完整性**。逐个对照 Python 写入点（作者交回记录中列出的模块，外加 `generated_preview.py`、
   `diagnostics.dns_report_fields`、`operation_events.clean_events`、`upgrades._save/start/artifacts`、
   `oxidns_ha._record_error/_record_leftover/enable`、`oxidns_ha_handoff.prepare/request`、
   `dns_follow` 全部 `_state` 字段、`rule_sync._state` 全部字段、`routers/store.normalize`、
   `routes/routers.probe_router/publish`、五种 manifest 的所有赋值分支、`routers/rule_sync._capture/run`、
   `rule_sync_rules.classify/load_sources`、`desired.compute_desired`、`routeros_nat_list.nat_resources`）。
   除上表三项外字段齐全；`has_update` 是旧版本写下的派生成员，按作者决定留在 Fields。
   新增私有快照普查 `TestHRSnapshotSchema`（只输出字段名/计数）：遍历 Agent 3、Deployment 64、
   OperationEvent 887、DeploymentDNSCheck 308、IPv6VerificationCheck 301、DeploymentFile 303、
   外部回执 7/1/1 等全部 HR 记录：**R5 类型 issue 0；非指针成员中出现 null 0；未声明成员仅 `Agent.has_update`**。
   这项检查此前缺失（`assertStoredTree` 只校验字节往返，类型错误会被 R5 静默吞掉），现已作为门禁的一部分。
   指针/值的选择：Python 中所有 `'x' in record` 的存在性判断（`handoff_required`、`force_refresh`、
   `oxidns_ha_state`、`dns_guard`、`ipv6_pd_guard`、`exempt`）现在都对应指针；可写 null 的成员
   （`after_deployment_id`、`source_deployment_id`、`deployment_id`、`config_revision`、`client_fingerprint`、
   `active_agent_id`、`enable`、`comparison_config`、`policy`、`nat_list`、`failed_stage` 等）都是指针。
   该用 Dynamic/Raw 的没有被收窄：resource `config`、`candidate_settings`、`plugins` 用 Dynamic；
   `health`、`source`、`source_snapshot`、`resource_config` 用 RawMessage（面板不导航，只携带）。
2. **HR 状态机语义不被 codec 改变**：`_preparation_running` 无成员；`submitted` 为 `*bool`，codec 不推断；
   迟到回执的判定依赖的 `agent_updated_at`/`status` 原样保留；`AppliedContent` 与 `PreparedContent`
   为独立类型，model 里没有提升函数；所有状态字段为 `string`；`ROSPolicyState` 无 `version`、
   `PDGuardResource` 无 `config`（作者测试断言不会凭空写出）；resource `config` 为 Dynamic，复合值逐字节保留；
   `Router` 无 `has_password`；model 新文件中没有 sha/哈希/指纹计算（grep 无 `crypto`、`sha256.` 调用）；
   浮点 epoch（`started_at`/`updated_at`（upgrade）/`recovery_reported_at`/`started_epoch`/`noticed_at`/
   `retry_at`/`requested_at`/`released_at`）为 float64，HA 的 `int(time.time())` 为 Int，ISO 时间为 string。
3. **N8-4f 约束**：「读到的 nil 不是清除命令」有作者测试（缺失/null 再赋 nil 字节不变），设计已写入；
   「R5 回退不是 ownership 证明」此前只有包内测试，现在有导出的 `Issues/IssueUnder` 和外部视角测试，
   设计两处写明调用方式。
4. **字节保留与覆盖**：R1/R2 由每类型逐字段 no-op/单成员编辑测试、`FuzzStoredTypes`（64+ 个 HR 类型种子）
   和快照逐记录往返覆盖；R5 诊断不带值（Map 用下标，新测试断言用户键不出现在路径中）；
   Python↔Go 双向由 `TestTypesPythonRoundTrip`（所有声明类型，含本次新增成员）和 `TestHRPythonWriters`
   （真实 Python `append_event`/`_record_leftover`/`_resource`/`classify`）覆盖。快照中没有的类型
   （ROSPolicy、IPv6FakeIP、IPv6Routes、PDGuard、handoff、native task、DNSFollowJob）都在 `hr_interchange.py`
   中有合成样例。作者交回记录把真实覆盖和合成覆盖分开列出，核对属实。
5. **外部 envelope**：Agent rule-sync 回执（`json.dumps(ensure_ascii=False)`，默认分隔符）、HA payload（同）、
   router 回执（`separators=(',', ':')`）—— Go 未改动成员按原字节写回，快照中 7/1/1 个文件逐字节往返；
   Go 新写的值为紧凑 JSON、非 ASCII 原样输出，Python `json.loads` 可读（interchange audit 结构相等）。
   唯一的字节差异是新写入时 U+2028/2029 会被转义（N8-3 已记录），不影响读取。
6. **Python 模拟 / any**：HR 类型文件中没有 `any`、`interface{}`、`map[string]…`；没有复制 Python 的调用形状。
   新增的 `Issues(value any)` 与已有的 `Marshal(v any)` 同类，是对任意 model 值的 codec 查询，不对应 Python 函数。

## 对 N8-5（repo 类型化 API）的可用性

修完 high 项后可以直接使用，没有会导致 N8-5 返工的类型问题。需要 N8-5 注意：

- **medium**：显式写入意图（untouched/replace/clear）里的 clear 要能删除 null 和错误类型的原成员（见上表）。
  建议做成 repo/model 层的显式操作，而不是让调用方依赖「指针置 nil」。
- **low**：repo 向消费者交出单个记录时，R5 信息随记录的 Raw 一起走，消费者用 `model.Issues` 查询；
  N8-5 不需要另建 issue 通道，但 `View` 的只读快照文档要说明 `Issues` 反映的是存储字节、不是之后的赋值。
- **low**：`PreparedContent → AppliedContent` 的提升（N8-10）需要手工复制已知字段和 `Fields`；
  Python 是 `{**prepared, config_version, deployment_id}`，未知成员也会带过去。复制 `Fields` 即可保持语义，
  新记录的成员顺序会与 Python 不同（语义无差别）。这一步属于业务，model 不提供提升函数是正确的。
- `System.Agents []Agent`、`Profile.Routers Routers{Items []Router}`、`Agent.Deployments Map[Deployment]`
  的写法与 N8-4a–d 一致（Map 用 Get/Set，按值复制共享切片）；外部回执由 N8-9/N8-10 的文件 I/O 使用，
  与 repo 无关。

## 门禁

修复提交 `d82af92f`（rebase 后 `083f4c2b`） 先在 `.32` 单包验证（model 全部测试含 Python 双向与私有快照，`ok configflow/internal/model 8.551s`），
再在 `.64` 跑完整门禁（`/tmp/gate.S3uEen`，排队等待 2 个其他门禁后执行）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$PWD CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data; server/scripts/gate.sh -p 6 FuzzRecordRoundTrip:./internal/model FuzzDocumentRoundTrip:./internal/model FuzzReviewRecordEdits:./internal/model FuzzStoredTypes:./internal/model FuzzRawDocument:./internal/store'
```

- gofmt 无输出，go vet 通过，两个 build tag 通过。
- 全量 `-race -p=6`：86 个有测试的包全部 ok；model 32.113 s，repo 307.345 s。
- 12 个 difftest 包全部 ok。
- proddiff：2831 cases、462 comparisons、11 个既有 unique differences，new=0 stale=0，self_check PASS。
- 5 个 fuzz 目标各 60 秒通过（model 4 个、store 1 个；deploy/pushdiff 本次未改动，未重复跑）。
- 私有快照测试的临时文件由 `t.TempDir` 删除，`TestHRSnapshotSchema` 读取后立即删除行文件；未取回、未提交任何生产数据。

```text
FuzzRecordRoundTrip ok  	configflow/internal/model	61.015s
FuzzDocumentRoundTrip ok  	configflow/internal/model	61.016s
FuzzReviewRecordEdits ok  	configflow/internal/model	60.110s
FuzzStoredTypes ok  	configflow/internal/model	61.013s
FuzzRawDocument ok  	configflow/internal/store	61.016s
GATE PASS
```

门禁后 `go` 合入 N8-6（只改 httpx/proddiff 和 n7-n8.md 的 N8-6 一节，与本复核无重叠文件）。rebase 到 64815165 后在 `.32` 复验：
gofmt 无输出，model/repo vet 通过，全包测试编译通过，`go test -race ./internal/model`（含 Python 与私有快照）`ok 39.263s`。
