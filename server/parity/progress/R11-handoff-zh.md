# R11 移植交接（进行中）

分支 go-port-r11，从 go-opus-review-r10@f368fa1b9 分出。仅合成 loopback；不 push、不派子 agent，不接触生产或设备。
范围：IPv6 PD guard、IPv6 DNS、RouterOS readiness；R12/R14 阶段调度和 A23 HTTP/缓存仍归原模块。
已完整阅读三个 Python 源文件、驱动与五份对应测试、R10 F1–F5/L5-D、R09、O3-6a 与更新后的 gate 限制。

已实现：
- PD 模板逐行带入（恢复当前前缀、10×1s 事件钩子、30s poll），派生、手动安装沿用/更新接管、安装/撤回二次清理、回读。
- PD 新 manifest 保存完整 stable 证明并读旧 ID-only 格式；新建名冲突、ID 复用/外改、table-qualified 重映射。
- 补偿 DELETE 失败时新 GET 仅以 404 证明已删除（R10 F1）；关机错误不被屏蔽。
- DNS 先证明 DHCP，预览允许 pending；执行须已应用；只 PATCH advertise-dns，all 提供手动 override 命令。
- readiness 导出只读入口、只读取引用的地址列表，复用快照的 forward hint，完整策略检查与旧 additive route receipt API。
- RouterState、report parts、Manifests、Apply handler 已接线。

验证（.32，全部 rt）：
`RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/routers/r11_boundary_test.go -- bash -lc 'cd server && gofmt -w internal/routers/r11_boundary_test.go && go test -count=1 -run "^TestR11PythonBoundary$" ./internal/routers'`
末行 `ok configflow/internal/routers 21.609s`。真实 Python 比较计划、写入序列、设备最终表。
内部快照的列表字符串改用 native JSON（Python repr 不再仿真），PD resources 多出 config，fingerprint 用 native stable JSON；这些精确例外会在最终行为台账说明。

下一步：加强 Runtime/原始字节/部分回滚/归属/脱敏/存储测试；检查共享路径；变异测试、fuzz；R10 合并后 rebase go；定向 race/vet，再按新版单 agent 槽位跑前台 gate，排队超过15分钟停止交回。

第二步（待记录提交 ID）：已加原始请求体/headers/超时透传、3 组真实 Python 模板字节、18 个真实 Runtime owner 边界、partial restore/new birth proof、legacy/unknown metadata、ND 最后事务外改、readiness snapshot/receipt、SQLite publication 与全写入口保护、public report copy 和脱敏测试。
.32 最新定向：`go test -count=1 -run "^TestR11" ./internal/routers ./internal/repo` → routers 24.695s、repo 9.443s；两包 vet clean。
.32 `python3 scripts/r11-mutation-gate.py` → result/journal/request/device-table/recovery 五类均 DETECTED（编译失败不计入），5/5，源文件恢复。
共享包 race + 全仓 gofmt/vet + 两个新 fuzz 正在 .32 前台跑（没有启动完整 race/proddiff）。与 O3-6b 已约定底层设备方法不重入 L1，由 R12/A23 上层取得统一 RouterJobLock→L2，held ctx 下传。当前 go 尚未包含 R10 review 分支，已通知协调者等合入再 rebase。

第三步（待记录提交 ID）：Python 矩阵扩至 80 个序列（含 runtime state + 跨表相同 ID 的撤回/恢复），模板识别/权限/定时差异/CLI 引号补齐。PD runtime 地址列表改用已验证的 single-row/scoped driver 模式，避免清理 cf-pd-last 时全读 GeoIP。撤回会发现 sweep 之后的新 runtime ID。旧 proof 的 null/empty/config/spec 不能降级为 legacy；ND 最后事务拒绝 dynamic/invalid="1"。部分 stage undo 仅保留尚未成功补偿的当前资源；恢复 PUT 的可信 ID 即使随后 GET 失败也用原证明登记。

.32 共享路径（214ed3bc4 的运行时代码）：全仓 gofmt 无输出、vet clean；race routers 990.855s、routersplit 56.088s、devhttp 3.948s、repo 448.124s、model 48.181s，全 PASS。两个新 fuzz 各 60s：PD 306469 executions / 61.088s；DNS 260409 executions / 60.117s，PASS。
.64 最新产品定向：`go test -race -p 6 -count=1 -run "^TestR11|^FuzzR11" ./internal/routers ./internal/repo` → routers 27.887s、repo 25.853s，vet clean。
.64 恢复修正后的 mutation 再跑 5/5 DETECTED；`TestR11PDRestoredReplySurvivesFailedReadback` 已包含在最新定向。测试文件与跨模块归属详见 `server/parity/r11-test-map.md`。
下一步仅最终门禁（.64，单agent槽位，前台，15分钟队列自动终止）、合入通知后 rebase 和交回；不再添加产品功能。
