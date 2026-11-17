# Config Step B 修复进度

分支 `go-fix-config-findings`；证据读取自 `go-difftest-config-b:server/parity/difftest-config-findings.md`。逐项在 .63 复现，独立提交；最终验收在 .32。

| 项 | 状态/证据 |
|---|---|
| 1 非对象/排序字段 | 已修。`.63 go test -run TestConfigFindingsRejectInvalidRequestShapesWithoutEffects ./internal/api/subscriptions/`：修前 `[1]` 500；修后三个接口及非法 legacy/ids 字段均400 JSON，状态不变、零上游调用。nodes/subscriptions/rules 包测试通过。 |
| 2 孤立代理项 | 已修。`.63 TestConfigFindingsRejectUnpairedSurrogatesWithoutCommit` 修前两端点均200并写入U+FFFD；修后高/低/连续孤立项400且存储不变；合法代理对、U+FFFD、转义反斜杠仍200。settings race通过8.953s。 |
| 3 JSON 错误出口 | 待复现 |
| 4 失败/退避通知 | 待复现 |
| 5 同值 PUT revision | 待复现 |
| 6 provider 注释 | 待复现 |
| 7 准备失败上下文 | 待复现 |
| 8 ruleset UUID4 | 待复现 |
| 9 auto_backup | 待确认 C35 |
| 10 BOM | 待确认 C30 复核 |
| 11 未知字段保留 | 待验证并登记接受差异 |
| 12 Docker HA 测试 | 待复现 |

最终：gofmt/vet/改动包 race/全量/全部 CONFIGFLOW_DIFF=1 difftest/proddiff、rebase go、干净、不推送。
