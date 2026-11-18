# R15 路由器 HTTP 与策略工作区（实现中）

分支 `go-port-r15`，从协调者指定 `go-opus-review-r14@a08a120b2` 新建。按要求待 batch-6 通知后再 rebase/接统一 RouterJobLock；目前沿用 R14 复核后的临时 JobLock。不 push；测试固定会话指定服务器，全部合成/loopback。

第一步：typed 策略工作区 revision/read/save，ManagementService 的配置 CRUD、仅 last_probe 的探测提交、手动路由编辑、原子工作区与规则同步设置；legacy C03/C22/C07 接缝集中在 adapters。API 五组共 21 method/path 已注册，接 R12 Publish/Inspect/BuildPlan、R13 PendingCache、R14 StartJob、A20 host.Bridge。最终公开投影用共享 redact；HEAD 显式走 GET，写入口先 L1 再验证，继而 L2/事务 guard。

当前基底的 httpx 已是 Flask 静态段优先/按 method 继续匹配的自定义 matcher，没有 ServeMux。三个 O6 优先级已有注册/endpoint 测试：安装 session 与 upstream 路由重叠、短链 i/status、POST mihomo/rule-targets fallback。A22 gateway-installations 尚未注册在本基底，测试显式注册它的真实 Flask pattern 作为匹配合同；另外两个使用已存在生产 API。batch 后需要用完整注册集补确认。

首轮 `RT_HOST="$R15_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=1 ./internal/api/routers -timeout 3m'`：`ok configflow/internal/api/routers 4.945s`。已通过 auth、CRUD、HEAD 零提交、L1 busy 优先于坏参数、无设备访问的 workspace read/save/stale/HEAD、三条 routing priority。格式在远端 gofmt 后逐一 --back 取回。

剩余：真实 Python/Flask 响应/DB/文件对照，失败 probe 的 KindInvalidResponse（坏 verinfo/Location）、健康网关不受坏报告影响、Runtime 关机/断连、发布和手动路由 HTTP、工作区原子变更/preview/target/revision、跨设备凭据；相关共享路径定向 race、全包 vet、新 fuzz 60 秒；完成 PROGRESS/行为差异/Step B 回填及交接。
