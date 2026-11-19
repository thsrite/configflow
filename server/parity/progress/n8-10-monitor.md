# N8-10 monitor S pass

状态：进行中。尚未完整完成，不可视为 verified 或全部去 jsonx。

初始基点 core-2@a75dd4a1d；待本次可构建检查点提交后 rebase 到 fresh HR tip 70cea1eb6。

已迁移：cache/ruleupdate/Mihomo panel 主体使用 typed Agent、原始身份 member、精确键名
model.Fields 和 DTO；生产通过 ReadAgent，不经旧 GetAgentByID 的动态往返。旧公开签名
仅在 *_legacy.go 转码。Agent 请求复用 jsonresp.EncodeDevice，保留 body/header 原字节、
超时、NoProxy/NoRedirects、响应上限。写操作 Detach 后检查 shutdown，设备回包后也检查。
下载版本缓存只保留实际消费的三个设置，使用 native rulefetch.Download/localhttp 接线。

协调者已批准损坏 HA publication 的 fail-closed，见 behaviour-changes；完整原 archive +
六条原始 row SHA 固定。未改原 Python 答案、corpus 或 pyref entries。现有 HA Disable
可以清除坏字段；新增直接调用方回归证明清除后规则更新成功。

指标 typed observation / summary / baseline arithmetic 正在原语料验证。
剩余：dashboard 状态/CAS/发布检查点，overview/query/client/ranking 协议和缓存，collector 接线；
私有旧 numeric adapter 仍供这些 consumer 使用，不能据本阶段声称全面完成。

## 已完成定向验证（.64）

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "NativeMonitor|Cache|RuleUpdate|Panel|CoreUpdate|NewerRelease|TargetedPaths|UnknownTarget|TransportFailure|MonitorDamaged" ./internal/agents/monitor ./internal/agents/dnsha ./internal/api/agents ./internal/agents/rulesync'

Exit 0：monitor 39.292s、dnsha 3.069s、api/agents 23.507s、rulesync 无匹配测试。
完整新增 protocol_native_test 包含真实 Runtime 断线/回包时 shutdown/已停止 generation、原始
类型身份、case alias/escaped duplicate/nested duplicate 等回归；fuzz 60s 与最终 vet 尚待。

## typed 状态/查询/排行/采集器检查点

DashboardState/Observations/MetricSummary/Snapshot/Rankings/RuleTask/CacheResult 及发布统计
钩子主体均已 typed。monitor.go、metrics/state/publication/publication_hooks/rankings/
history/panel/cache/ruleupdate 均不再导入 jsonx；旧签名隔离在 *_legacy.go 转码。
移除旧对象 truthiness、AttributeError/TypeError 和 PythonClass 业务仿真。未重写的
Unicode 数字/文本输入兼容 helper 属 N10，机器版本入口先验证 ASCII 并 fail closed。

SQLite 排行 StoreSlot/MaintainContext/FetchedContext/QueryContext/HasAnyContext 接受调用方
ctx，连接、语句、commit 贯穿 ctx；旧签名只供仍未迁移调用方作 Background codec。原生
collector 和 overview 不走这些无 ctx 入口。maintenance watermark 仅在提交成功后推进。
统计文件同样在创建目录/temp 和 replace 前检查 ctx；owner Detach 保留 Runtime shutdown。

已复跑原 metrics/baseline/reference：PASS（包括 700 组 review baseline；旧字段缺失与
显式 null 的差别完整保留）。typed 状态、CAS、统计恢复定向 PASS；原所有 monitor +
三组直接调用方 race 在第一检查点全 PASS：monitor 268.401s、dnsha 391.209s、rulesync
127.418s、API 467.824s；该 run 的 vet ./... 静默通过。后续状态/collector 变更需再做
最终 race。当前正在跑 reference + flows + native boundaries，尚不宣称最终全绿。

## 主体迁移定向通过（.64）

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -p 4 -count=1 -timeout=10m -run "Reference|ReviewOverviewFlows|AfterATimeout|ATimeoutWithout|CollectRound|NativeMonitor|MonitorDamaged|Publication" ./internal/agents/monitor ./internal/agents/dnsha'

Exit 0：monitor 20.728s、dnsha 5.695s。覆盖原全部 Reference、400 flows/2846 steps、
中文原文、body/header、typed cache/rule/panel、两种损坏记录恢复动作。没有重录原期望。
历史 shape-free public_upstreams 私有 helper fixture 留为 test-only adapter（复用生产
identity/display primitives），生产仅接 typed MetricSummary；不向产品恢复不可达的动态形状。

下一步：提交/rebase 后补 native SQL/state shutdown、真实 HTTP 字节与缓存副本回归，最终
race、Corpus|Reference|Golden/agents difftest、全部新 fuzz 各 60s、gofmt/vet，并收尾报告。
