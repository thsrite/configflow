# R17 固定 IP / 客户端设备 HTTP（实现中）

分支 `go-port-r17`，协调者临时基底 `go-r17-base-v2@673851659`（go + R14 + 已复核 R16），已按要求 --onto 替换旧 R16 基底。未 push；仅测试服务器定向检查，合成数据/loopback。R13 失败已由协调者接走，不在本任务处理。

已完成第一步：routers 的 typed FixedIPDriver/Binding/Operation/Snapshot/State；RouterOS/iKuai DHCP 读取、资格判断、预览、写入、删除、补偿及删除恢复。使用现有 driver/原生 JSON/netipx、KindRejected/UncertainWrite，固定 IP 的归属守卫不放宽为只读行容错。routerfake 增加真实 make-static 动作，不覆盖生产参数。

编译和 `TestR17ROSReservationLifecycle` 的新增/动态转静态→删除→恢复→撤销定向 race 通过：`ok configflow/internal/routers 1.065s`。

下一步：客户端服务的精确请求验证、跨路由器 L1/L2、其余事务与反向补偿、600 秒可互验 preview token；C36 guard、A13 projection；正式 HTTP 五路由（HEAD 只读）、R16 enrichment/公开完整凭据脱敏；Python/Flask 边界和 Runtime 回归；定向 vet/race/fuzz；按协调者后续通知 rebase go。

R16 复核规则：坏记录只影响自身；公开列表/发现/错误不能引用其他设备凭据；完整响应用共享 redact 和 repository token hook，私有乐观更新快照不修改。UDP 协议引用误判属 C26 已修，R17 HTTP 补回归。

命令：`RT_HOST="$R17_TEST_HOST" rt -- bash -lc 'cd server && gofmt -w internal/routers/fixed_ip*.go internal/routers/r17_fixed_test.go internal/routers/routerfake/fake.go && go test -race -count=1 ./internal/routers -run "TestR17" -timeout 3m'`（R17_TEST_HOST 是会话固定的测试服务器 SSH 目标，不把基础设施地址写入仓库）。无未编译的半写文件。

第二步：clients FixedIPService（C36 fail-closed guard、重建计划、跨设备 L1、共用 R06 的批量 L2、事务/补偿/删除恢复 ID 保存、发现 enrichment）与正式 api/clientdevices 五路由完成。使用 A13 deploy.PublicDeployment、C36 ProfileSessions、auth.Current SecretKey；HEAD 走 GET；完整公开投影沿用共享 redact 和 repository tokens。preview token 的签名按 itsdangerous 原协议、JSON 原生编码，Python 与 Go 双向互验（含压缩/Unicode/600 秒/未来时间）通过。批量 L2 只 try 一组，任何失败先释放整组再等待单锁，不绕开 checker 或另建锁。

定向 race：routers R17 1.084s；正式 HTTP 3.546s；clients R17 5.045s。下一步真实 Flask HTTP 差分、两厂商 Python 边界错误/回滚语义、真实 Runtime 设备 I/O/写后关机、跨设备/克隆/恢复/超限回归、UDP protocol collision HTTP 入口回归，以及最终定向检查。第一步提交 `8f05e4f0e`；第二步为本文件所在提交。
