# Config Step B 修复进度

分支 `go-fix-config-findings`；证据读取自 `go-difftest-config-b:server/parity/difftest-config-findings.md`。逐项在 .63 复现，独立提交；最终验收在 .32。

| 项 | 状态/证据 |
|---|---|
| 1 非对象/排序字段 | 已修。`.63 go test -run TestConfigFindingsRejectInvalidRequestShapesWithoutEffects ./internal/api/subscriptions/`：修前 `[1]` 500；修后三个接口及非法 legacy/ids 字段均400 JSON，状态不变、零上游调用。包含null、空数组、false、0、空字符串；fetch在资源查找前验证对象。新回归race1.273s，BOM合法对象差异0.861s。旧C30比较器显式断言第1项400决策，仍逐次比较存储，篡改成功字段/状态/消息会被拒绝；定向Flask差异50.061s。 |
| 2 孤立代理项 | 已修。`.63 TestConfigFindingsRejectUnpairedSurrogatesWithoutCommit` 修前两端点均200并写入U+FFFD；修后高/低/连续孤立项400且存储不变；合法代理对、U+FFFD、转义反斜杠仍200。settings race通过8.953s。 |
| 3 JSON 错误出口 | 已修。`.63 TestConfigFindingsEveryAPIErrorUsesJSON` 修前HTML400/404/405/500，修后统一JSON；文件迟发错误与编码失败也走JSON。R218非法缓存回归断言「订阅操作失败」。显式公开的text/plain错误（包括安装存储503）保留中文消息再转JSON，并经过统一脱敏；不公开任意HTML500正文。httpx race15.500s，安装链接完整差异5.793s，存储503定向API/httpx race1.140s/1.156s；R218-F13另按SQLite trigger拒绝提交补精确HTTP回归，.63 race1.327s；.32 subscriptions整包race1.786s，断言500、准确消息和配置不变；比较器只接受同status/准确公开描述的文本→JSON迁移，其他头/数据继续比较。 |
| 4 失败/退避通知 | 已修。修前完整NewApp的任务回执投影因缺任务slot返回ErrIncomplete，notifications.json不存在。生产通知改读AvailableTasks（只收集真实已注册模块），任务页面ListTasks仍要求完整。预存backoff三次失败+due完成的回归验证backoff通知pending/failure_count=3，无需浏览器GET。`.63 server race` 1.289s，notifications race 5.049s。refresh整包回放在WSL fsync滞留，已停止诊断，最终在.32复验。 |
| 5 同值 PUT revision | 已修。修前SQL subscriptions行revision 2→3（连续相同PUT）；修后保留原始有序存储对象，同一行value/revision均不变，包含嵌套未知字段。`.63 subscriptions race` 1.880s。注意证据针对字段revision；Python _apply_shared_locked仍每次推进全局generation/shared._revision，Go保持该事务语义。 |
| 6 provider 注释 | 已修。修前global/scoped/merged及served文件无注释；修后单源六行、合并源数量/明细、Generated本地微秒时间+Z、Source均恢复。`.63 targeted metadata race` 1.251s；完整C30差异新增逐行注释比较，元数据无差异；第1项已决定的4xx由独立策略断言，存储继续严格比较。 |
| 7 准备失败上下文 | 已修。`.63 TestConfigFindingsPreparationContextWithoutA11Positions` 修前缺订阅前缀和来源，修后默认provider上下文保留「订阅…准备失败」及内容来源。provider整包race通过169.005s。C30差异仅延后文件/坐标/摘录，前缀和来源严格比较；A11记录已缩小范围。 |
| 8 ruleset UUID4 | 已修。`.63`真实POST rulesets/init修前ruleset_32hex，修后存储/返回规则引用采用带横线UUID4并验证版本/variant；lib_32hex保持。使用既有google/uuid模块的NewRandom（返回错误、不panic），未增加依赖版本。discovery/API race 5.962s/1.761s。 |
| 9 auto_backup | C35已解决，无业务修改。`.63 CONFIGFLOW_DIFF=1 go test -count=1 -run ^TestAutomaticChangesCoalesceAndPendingDisableCancels/false$ ./internal/webdav/difftest/` 通过1.797s：启用auto_backup和连续提交经1s窗口只上传一次，Flask/Go文件及回执一致。实际注册方法为POST /api/backup/config（Python也是POST，PUT未注册），对应R015-S90。 |
| 10 BOM | C30复核已解决，无BOM业务修改。`.63 TestUTF8BOMRequestBodies` 首轮1.091s，强化第1项后0.861s；含parse-lines的合法BOM对象与Flask响应/存储一致。BOM null按第1项400规则单独断言Go无副作用。 |
| 11 未知字段保留 | 接受，无业务修改。`.63 TestConfigFindingsPreserveLegacyDiscoveryExtension` race通过1.199s；R058-B03的false/空数组未知遗留扩展在PUT后完整保留，符合N8。Python删除已登记O12。 |
| 12 agents 差异测试 | (a) .63复现非法IPv6的urllib/net/url英文诊断差异；只对该命名用例断言400、success=false并允许原生诊断不同，其他字段/头仍严格比较。末尾换行继续明确断言Python200/Go400。(b) .63复现fuzz 92项非法UTF-8 query差异；仅将非法字节规范成U+FFFD，再由独立Flask生成完整预期，脚本不mask；bash -n、字面JSON heredoc和隔离赋值验证引号/命令替换安全。a/b/safety通过86.991s。(c) .32原始启动器-count=3复现两次48/0；按维护者要求保留原进程内startGo，不调等待。Stop成功等待循环及hooks后复位started/cancel/baseCtx，超时不复位；.63重启单元修前第2代失败，修后race1.009s。.63重启回归-count=3也通过race1.010s（测试清理注册表，无跨次残留）；.32整个agents/difftest-count=2首轮通过283.486s，合入N8后最终双跑通过229.828s。 |

最终验收在 `.32` 执行，全部命令通过同一个前台 `rt -- bash -lc '…'` 运行；日志为 `/tmp/config-findings-final3-*`，agents 双跑为 `/tmp/config-findings-12-agents-twice-final3.log`：

```bash
cd server
gofmt -l internal/ tools/ # 必须无输出
go vet ./...
cd ..
~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py
cd server
go test -race -count=1 -timeout=20m \
  ./internal/api/agents/ ./internal/api/nodes/ ./internal/api/subscriptions/ \
  ./internal/api/settings/ ./internal/api/domaindiscovery/ ./internal/httpx/... \
  ./internal/rules/ ./internal/provider/ ./internal/tasks/ ./internal/server/ \
  ./internal/lifecycle/ ./internal/discovery/
go test -count=1 -timeout=20m ./...
config_diff_packages=$(go list ./... | rg '/difftest$' | rg -v 'internal/api/agents/difftest$')
CONFIGFLOW_DIFF=1 go test -count=1 -timeout=20m $config_diff_packages
CONFIGFLOW_DIFF=1 go test -count=2 -timeout=30m ./internal/api/agents/difftest/
cd ..
server/scripts/proddiff.sh
```

已完成：gofmt/vet无输出、比较器21项OK、改动包race全部通过（httpx77.167s、provider137.069s、lifecycle1.118s）。全量65个测试包通过（installer162.090s、refresh20.664s）；其余8个差异包全部通过（subscriptions77.699s、webdav14.351s、discovery2.621s、substore6.987s）。agents整包-count=2通过229.828s。proddiff共2831用例、415次比较、65个GET模板，23项自检通过，coverage_regressions=0、unmapped_routes=0、new=0、stale=0。

proddiff 的变更只处理已批准的 JSON 错误表示：短链错误按精确路由/同状态/准确公开消息/两个字段断言，负向回归保证错误success、消息、状态、额外字段、重复键和头仍失败；既有 A18/F1 的 Python成功/Go500不被掩盖，只将两条HTML结构基线改成三个JSON字段基线，旧状态/Cache-Control/日志差异保留。


最终输出节选（`.32`）：

```text
Ran 21 tests in 0.027s
OK
ok  configflow/internal/httpx     77.167s
ok  configflow/internal/provider  137.069s
ok  configflow/internal/lifecycle 1.118s
ok  configflow/internal/installer 162.090s
ok  configflow/internal/refresh   20.664s
ok  configflow/internal/api/subscriptions/difftest 77.699s
ok  configflow/internal/substore/difftest 6.987s
ok  configflow/internal/webdav/difftest 14.351s
ok  configflow/internal/api/agents/difftest 229.828s
baseline: new=0 stale=0
```

最后新增的F13仅补测试，业务代码不变；在`.32`另跑一次gofmt/vet、subscriptions整包race与全量测试（`/tmp/config-findings-final4-*`），全部通过：

```bash
rt -- bash -lc 'set -e; cd server; gofmt -l internal/ tools/ > /tmp/config-findings-final4-format.log; test ! -s /tmp/config-findings-final4-format.log; go vet ./... > /tmp/config-findings-final4-vet.log 2>&1; go test -race -count=1 -timeout=20m ./internal/api/subscriptions/ > /tmp/config-findings-final4-race.log 2>&1; cat /tmp/config-findings-final4-race.log; go test -count=1 -timeout=20m ./... > /tmp/config-findings-final4-full.log 2>&1; tail -8 /tmp/config-findings-final4-full.log'
```

```text
ok  configflow/internal/api/subscriptions 1.786s
ok  configflow/internal/installer 149.271s
ok  configflow/internal/weblistener 0.006s
ok  configflow/internal/weburl 0.007s
ok  configflow/internal/yamlx 1.527s
?   configflow/tools/proddiff/generator [no test files]
```

最终基于`go`的`5535931d`（含N8-1），保留已有N8非有限数差异与比较器。提交后执行`git rebase go`，工作区干净；未推送。
