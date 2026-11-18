# batch-7 集成交接

任务：以 batch-6 f9f3105ee 为基底叠入已复核分支，完成五项集成，交协调者跑 core/race 门禁。
仅在新 worktree/branch batch-7 工作；原分支和 batch-6 worktree 未修改。

## 分支叠入

每支使用独立临时分支从指定 tip rebase --onto 当前 batch-7，再 fast-forward 回 batch-7；
未把各分支包含的旧基底重复叠入。下表是 source boundary..tip（右侧为当时叠入后的 tip）。

| 分支 | 取用区间 | 叠入后 tip |
|---|---|---|
| a22 | 663d271bb..9791bc8d | d91c219de5 |
| r14 | c5f5ff349..a08a120b | 1d55ae951d |
| r16 | c5f5ff349..1b28b662 | 214abefbf5 |
| r17 | 673851659..b78bbb38 | 3e7d5a4e3c |
| r15 | a08a120b..9df1a9a4 | 2fdedbcc1d |
| o37 | cdca79def..a1d3e1b7 | 6ecbea848b |
| o38 | a1d3e1b7..b8f6cbeb | da161d9088 |
| pyref | c5f5ff349..06a25922 | ed4a22b44a |
| 8b | 6e9d0b738..fbd9993c | d3f110ab75 |
| 8c | 7d27bfa11..b1efee615 | ff8547b6f1 |
| discovery | 9b72fc898..778442e4d | 478bb5022e |
| health | 824189f07..372840ba8 | e04a39de21 |
| a18 | e84063f9c..74935690 | 9fb89fc22b |
| a23 | 673851659..3ce4338d | 06f2e87376 |

- R14 264ffbb55 与 batch-6 8ec68fd4d 补丁等价，显式丢弃。
- R17/A23 从 673851659 后取用，不重复叠 R14 作者与已复核 R16 临时基底。
- N8-8b 从 6e9d0b738 后取用；8c 从其等价 8b tip 7d27bfa11 后取用。
  不重复引入 N8-8a/9b72fc898 的 batch-6 等价内容。health 仅取 824189f07 后四个提交。
- .md 冲突按要求保留双方文本；未随意采用整文件 ours/theirs。

## 代码及数据冲突处理

- A22/A18 的 agents/messages.go、api/agents/messages.go：独立消息常量同时保留。
- R14 wire.go：保留 O3-6b 的 repo JobLock hook，同时调用 wireRuleSync；之后在独立锁集成提交中删除 R14 私有 hook。
- R17/A23 同名 RouterOperationLocks 文本不冲突但编译重复：合并为 A23 排序去重的共享 L2 group，R17 显式传 write=true；移除旧副本，复跑双方锁/关停回归。
- R17 ikuai.go：保留新 writeForm 共用实现，Write 调用 lifecycle.Detach，保持 batch-6 的关停取消能力。
- A23 server.go：保留 dnsfollow/dnsha 及 ipv6 imports，路由注册同时保留 routers/ipv6。
- N8-8b/c 删除旧生成器时与 batch-6 validateGenerationRules 冲突：采用 native typed 生成器，
  把相同 validateRuleIdentities 检查移到 MihomoDocument/clientPreflight，保持 R5 discriminator 拒绝。
- pyref 的五份冲突 golden：解压解析后确认 metadata 外输出完全相同，采用新 narrowed provenance；
  最终合成树的来源指纹还需按 pyref 工具刷新，绝不修改 reference 输出以消除产品差异。
- A18 baseline 冲突暂保留 batch-6 文件，将 R14/A18 deferred 删除集中到独立集成提交。

## 集成项

1. 唯一路由器任务锁：实现及定向 race 验证通过。R14/R17/R15/A23 全部调用 RouterJobLock，
   删除 syncJobs 与 R14 hook；KindConflict 保持各入口原中文提示，关停/取消错误原样传播。
   有记录按连接、无记录按 profile+id；HTTP 继续先取锁再执行 404/请求体校验。
2. A22 完整注册：待真实路由优先级及 NewApp 验证。
3. baseline：已删除 11 条 deferred→R14 和 12 条 deferred→A18；其余分类保留。最终 compare 检验 new/stale。
4. O3-8：待跑 16 个 scenarios，验证已包含 R14 M1 的关停次序。
5. A14 节流：集成树初次独立 race count=3 通过（14.714s）；为消除慢磁盘/负载造成的真实时间依赖，
   仅测试接入既有生命周期 fake Clock，验证 999ms 不写、1000ms 写及强制阶段写，不修改生产阈值。

## 验证及剩余工作

定向检查、预检与 compare 证据将在每项完成时更新。尚未跑完整 core/race 门禁；由协调者执行。
batch-6 若追加日志修复，需要在最终预检前再 rebase 到其最新 tip。

### 集成 1 验证

主测试服务器 `rt`（固定用户指定 RT_HOST）：
`go test -race -count=1 -timeout=20m ./internal/routers ./internal/clients ./internal/api/routers ./internal/agents/ipv6 -run "TestR14ReviewRepositoryHookSharesJobLock|TestReviewR17RepositoryHookSharesJobLock|TestR14JobAndScheduler|TestR12StartPublish|TestR15PublishHTTP|TestR15JobLockPrecedesValidation|TestReviewR17ShutdownWhileWaitingForDeviceLock|TestR17BatchLocksAndRollback|TestR17DistinctDeviceLocksAndMixedVendors|TestR17Runtime|TestRouterOperationGroup|TestOperationKeys"`

末行：routers 3.747s，clients 5.479s，api/routers 6.169s 均 ok；ipv6 编译通过但本正则无测试。
首次编译发现 R17/A23 重复 RouterOperationLocks，统一后上述实际守卫与多设备锁/取消用例全部通过。
本提交包含合并冲突代码的 gofmt 收尾；A14 fake Clock 和 baseline 删除各留下一项独立提交。

### 集成 3 baseline 清理

按 baseline 原理由精确删除 23 行，没有扩大正则或增加新豁免；A14/A21/C34 的其他 deferred
条目仍保留，必须由最终 compare 判断是否还有效。A18 原分支的删除在 rebase 冲突中暂缓，
现在与 R14 一起集中记录，避免漏删旧 logs 路由的 A18 后果行。
