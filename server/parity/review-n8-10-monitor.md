# N8-10 agents/monitor S pass HR 复核（fresh Opus）

复核对象：`go-n8-10-monitor@e72917233`（基于 go@ae00b71f1，5 个提交），作者 Codex 1cc7e926。
复核分支 `go-opus-review-n8-10-monitor`。对照 Python（`backend/agents/oxidns_publication_stats.py` 等）
和迁移前 Go（ae00b71f1）阅读代码。定向测试只在 .64 经 `rt` 运行（GOMAXPROCS=4，-p 4）。
没有重跑作者的整套测试，也没有跑完整门禁。

## 结论

high 0，medium 1（已修），low 2（记录）。修复随本分支叠批后，monitor 可以 verified。
需要协调者裁定 1 项（L1，见文末）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| M1 | medium | 已修 | **判断“检查点已被取代”时，损坏的 status 仍被当成丢弃检查点的依据（违反裁定 2）。** `statePublication` 计算 `superseded` 时用 `originalText` 读取 pending 部署的 `status` 和 `latest_deployment.status`。非字符串会投影成 `""`，于是只要 `latest_deployment.deployment_id` 指向另一次部署，就判定为已取代并返回 nil，AdvanceState 随后清掉 pending，或开启新周期并写入统计文件。作者只在**未被取代**的分支（AdvanceState 中）校验 status 类型，所以作者的 `status` 回归（latest 仍指向 pending 部署）覆盖不到这条路径。Python 在这里会因 `[...] in (...)` 为 False 而丢弃检查点，这正是裁定 2 要求收紧的“零值当丢弃证明”。修复：新增 `storedStatus`，null 或缺失按缺失处理，其他非字符串返回 `errStatusDamaged`（StateError → DASHBOARD_STATE_ERROR/500）。pending 部署的 status 总是校验；latest.status 只在确实被参考时校验（latest 指向别的部署，且 pending 状态为 succeeded/rolled_back），避免无关的 latest 记录损坏也阻塞面板。为不改变 a06 中 8 条原有绑定行（采样早于 checkpoint 时 Python 先抛 PublicationPending），AdvanceState 先做时间判断，再报告 status 损坏。回归测试 `TestReviewN810SupersededCheckDoesNotTrustDamagedStatus`（task-status / latest-status 两个子测试）检查 500、检查点文件字节不变，以及统计重置可以恢复。修复前两个子测试都失败（`got <nil>, want DASHBOARD_STATE_ERROR 500`），修复后通过。behaviour-changes 的 pending 一节已补登记。 |
| L1 | low | 待裁定 | **status 为 JSON null 按缺失处理。** 作者在 AdvanceState 中把 null 视同缺失：没有状态，在 reason 为空时清除 pending。M1 修复沿用了这个约定。Python 中 None 和缺失是同一路径，所以这不算 R5 投影；但裁定原文是“status 非 string → StateError”，严格来说 null 也属于非 string。正常部署记录总会写入字符串 status，null 只会出现在损坏数据里。是否把 null 也改成 StateError，请协调者裁定（改动只需在 `storedStatus` 和 AdvanceState 的 null 分支去掉对 null 的特殊处理，并补一条 a06 绑定）。 |
| L2 | low | 记录 | **legacy 适配基本是机械转码，但有两处小逻辑。** (a) `publication_hooks_legacy.go` 的 `Complete` 遇到容器值会返回 `msgPublicationDamaged`，而生产注册的 `completeHook`（`wire.go`）把非字符串直接当成 `""`，结果是“非终态，不写”，同样 fail closed。`Complete` 只给测试或旧调用方用，两者都不会写入，所以没有安全影响。(b) `cache_legacy.go` 的 `ValidRequestTags` 对嵌套容器返回英文 error，这是 ae00b71f1 已有的 API 层合同（`api/agents/panel.go:153`），本轮只是原样搬迁。其余 7 个文件只做 jsonx ↔ typed 转换和签名转发，没有业务分支。延后清单与报告一致：9 个文件共 1004 行；生产主体（非 `_legacy`、非测试）没有 jsonx 引用，也没有 `json.Unmarshal`。 |

## 逐项核对

1. **裁定 1（非 object HA publication）**：`ruleupdate.go` 的 POST 路径在 `oxidns_ha_publication` 存在、不是 null，且 `jsondecode.Unmarshal` 解不成 `model.Fields` 时（包括空串、0、字符串、数组），或 `status` 是容器时，在 HA busy 判断、pair 转发和设备请求之前就返回 `msgHAPublicationDamaged`（集中定义在 messages.go），不发请求，也不写入。GET 只读，不受影响，和 behaviour-changes 写的“规则更新 POST”一致。`dnsha/monitor_repair_test.go` 覆盖 `""`/`0`/`"damaged"`/`[]`，依次验证：拒绝且设备调用数不变 → 现有 `Disable` 清掉该字段 → 规则更新 202 成功。负控（临时去掉 journal 检查，已还原）：`TestReviewRuleUpdateHAActiveFollowsPython` 和 `TestMonitorDamagedPublicationCanBeDisabledAndRulesResume` 都失败。
2. **裁定 2（pending checkpoint）**：deployments、latest_deployment 和 entry 不是 object 时（`storedObject`/`EntryValid`），AdvanceState 中未被取代的 status 和 manual_rollback 类型错误时，都返回 StateError。profile/service/deployment_id 只认原始 JSON string，大小写别名不能覆盖。这些作者已实现，回归齐全（四类损坏、五类原始证明，都验证文件字节不变和统计重置可以恢复）。漏掉的 superseded 路径见 M1，已修。首屏没有 UI 恢复入口，按协调者批准记为后续；`doc/manual/oxidns-panel.md` 已写明 reset API。
3. **裁定 3（core version）**：`CoreUpdateCheck` 用 `fieldText`（只认原始 string）取出 current，再用与 version.txt 相同的 `coreVersionPattern = ^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$` 校验（Go 的 `$` 不会匹配尾随换行）。不符合时返回 `panelFail("INVALID_RESPONSE", 0)`（502），并且是在 `cachedLatest` 之前返回，不会 fetch，也不碰缓存。`TestReviewCoreUpdateDigitLimitFollowsPython` 断言 `latest.count()==0` 且缓存指针不变。`releaseInts` 用共享的 `taskstatus.Count` 做任意精度比较，`pynum.go`（CPython 4300 位仿真）已删除。
4. **来自 Agent 的 JSON 精确键名**：生产代码中没有 `encoding/json` 的 struct 解码。Agent 回包走 `protocolAnswer` → `model.Fields`（拒绝重复键），再按字段 `jsondecode` 解码，`answerBool` 严格要求 boolean。legacy 中的 `metricJSON` 和 `legacyRegistry.ReadAgent` 也走 `jsondecode.Unmarshal`。
5. **存储侧身份 / ctx / 脱敏**：存储身份只经 `originalString` 读取原始 string。`CallRuleUpdate` POST、`CoreUpdateCheck` 和 publication hooks 都先 `lifecycle.Detach`，在请求前后检查 `ctx.Err()`。后台 panic 只在 `rankings.go` 顶层 recover，日志不带载荷。本轮 diff 没有私有的脱敏实现。
6. **期望 / 语料 / 阈值**：既有 testdata（a06/a07 archive、pyref）没有修改，只新增了 `n810_*` 绑定 fixture。绑定方式是 archive SHA 加单行 canonical 完全相等，只对列出的行生效，其余行仍比较原期望（抽查 `nativeHARefusal`、`nativeVersionExpected`）。修改过的既有测试只是在绑定行上走 native 分支，或者跟随 `rulefetch.DownloadRequest`、`model.SystemConfig` 的类型改名；原 `exception` 分支保留。本包没有分配阈值测试。behaviour-changes 已登记三条裁定以及 duplicate、非有限值、指标整数和存储摘要，本复核补了 M1。

## 提交与测试

提交：本分支 `go(monitor): keep pending checkpoint on damaged superseding status`（M1 修复、回归测试、behaviour-changes 补登记、本报告）。

rt 命令（均为 `RT_HOST=sunbroke@192.168.0.64`，在 `server/` 下执行）：

1. 修复前（临时还原 publication.go）：`go test -p 4 -count=1 -run "ReviewN810Superseded" ./internal/agents/monitor` → 两个子测试都是 `got <nil> … want DASHBOARD_STATE_ERROR 500`，`FAIL`。
2. 初版修复把 status 校验放在时间判断之前，a06 的 8 条 advance 绑定行（`want PublicationPending`）因此失败；调整顺序后通过。
3. 修复后完整 monitor 包：`gofmt -l internal/agents/monitor`（无输出）；`go vet ./internal/agents/monitor`；`go test -race -p 4 -count=1 -timeout=30m ./internal/agents/monitor` → `ok 289.679s`。
4. 直接调用方定向：`go test -race -p 4 -count=1 -run "Monitor|Publication|Dashboard|Overview" ./internal/agents/dnsha ./internal/api/agents` → `ok dnsha 31.415s`；`ok api/agents 3.671s`。
5. 改用 `errors.Is` 后：gofmt 无输出、vet 通过；`go test -race -run "ReviewN810|Publication|Pending|Checkpoint|Damaged|A06Reference|A06Review|State" ./internal/agents/monitor` → `ok 13.133s`。
6. HA 守卫负控（临时改动，已还原）：`go test -run "TestReviewRuleUpdateHAActiveFollowsPython|TestMonitorDamagedPublication" ./internal/agents/monitor ./internal/agents/dnsha` → 两个包都 `FAIL`。

## 需要协调者裁定

- L1：pending 部署（或被参考的 latest）的 `status` 为 JSON null 时，是继续按缺失处理（现状，与 Python 的 None 一致），还是按裁定 2 的字面意思当作非 string，返回 StateError？
