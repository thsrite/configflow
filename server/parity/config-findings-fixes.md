# Config Step B 修复进度

分支 `go-fix-config-findings`；证据读取自 `go-difftest-config-b:server/parity/difftest-config-findings.md`。逐项在 .63 复现，独立提交；最终验收在 .32。

| 项 | 状态/证据 |
|---|---|
| 1 非对象/排序字段 | 已修。`.63 go test -run TestConfigFindingsRejectInvalidRequestShapesWithoutEffects ./internal/api/subscriptions/`：修前 `[1]` 500；修后三个接口及非法 legacy/ids 字段均400 JSON，状态不变、零上游调用。nodes/subscriptions/rules 包测试通过。 |
| 2 孤立代理项 | 已修。`.63 TestConfigFindingsRejectUnpairedSurrogatesWithoutCommit` 修前两端点均200并写入U+FFFD；修后高/低/连续孤立项400且存储不变；合法代理对、U+FFFD、转义反斜杠仍200。settings race通过8.953s。 |
| 3 JSON 错误出口 | 已修。`.63 TestConfigFindingsEveryAPIErrorUsesJSON` 修前HTML400/404/405/500，修后统一JSON；文件迟发错误与编码失败也走JSON。R218非法缓存回归断言「订阅操作失败」。httpx全包race与真实Flask差异通过；差异仅显式验证HTML→JSON的同status/准确描述，其他头/数据继续比较。 |
| 4 失败/退避通知 | 已修。修前完整NewApp的任务回执投影因缺任务slot返回ErrIncomplete，notifications.json不存在。生产通知改读AvailableTasks（只收集真实已注册模块），任务页面ListTasks仍要求完整。预存backoff三次失败+due完成的回归验证backoff通知pending/failure_count=3，无需浏览器GET。`.63 server race` 1.289s，notifications race 5.049s。refresh整包回放在WSL fsync滞留，已停止诊断，最终在.32复验。 |
| 5 同值 PUT revision | 已修。修前SQL subscriptions行revision 2→3（连续相同PUT）；修后保留原始有序存储对象，同一行value/revision均不变，包含嵌套未知字段。`.63 subscriptions race` 1.880s。注意证据针对字段revision；Python _apply_shared_locked仍每次推进全局generation/shared._revision，Go保持该事务语义。 |
| 6 provider 注释 | 待复现 |
| 7 准备失败上下文 | 待复现 |
| 8 ruleset UUID4 | 待复现 |
| 9 auto_backup | 待确认 C35 |
| 10 BOM | 待确认 C30 复核 |
| 11 未知字段保留 | 待验证并登记接受差异 |
| 12 Docker HA 测试 | 待复现 |

最终：gofmt/vet/改动包 race/全量/全部 CONFIGFLOW_DIFF=1 difftest/proddiff、rebase go、干净、不推送。
