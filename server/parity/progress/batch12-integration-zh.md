# batch-12 集成

基底 origin/go@ae00b71f1；分支 batch-12，私有 worktree ~/program/configflow-go-batch-12。
按指定顺序叠入两个已复核分支，源分支未移动，未 push。

## 叠入与冲突

- monitor go-opus-review-n8-10-monitor@627763044：7 个提交，基底正好 ae00b71f1，rebase 无需改写，batch-12 快进到原 tip。包含 M1 superseded status 损坏拒绝修复及 null 按缺失的协调者裁定。
- installer go-opus-review-n8-10-installer@f8df41fb1：7 个提交，基于同一 ae00b71f1，逐个 rebase 到 monitor tip，结果 6714a866f。包含 // 根目录别名拒绝、原生 Docker 端口 shellWord、非 ASCII 空白 ss 证据 unknown。
- 唯一冲突在 installer 第一个提交追加 behaviour-changes.md 时：手工按 monitor 后 installer 的顺序保留双方完整章节，未删除内容。range-diff 只有这一处追加上下文变化，其余 6 个 installer 提交完全等价。
- 没有代码冲突。合并后 installer/API 路径逐树与原 installer tip 相同，monitor/dnsha 路径逐树与原 monitor tip 相同；未绕过 jsondecode 精确键名或损坏存储的拒绝/无写入边界。
- 没有重复、跳过的源提交，未改原始 corpus、baseline、分配阈值或依赖。

## 验证

按任务授权只在 .32 跑完整预检和 compare，core 由协调者在 .64 负责。
先 RT_HOST=<test-host> rt sync，再 ssh <test-host> 'bash /tmp/preflight.sh program__configflow-go-batch-12 -p 6'，GOMAXPROCS=6。
预检日志 ~/.cache/batch12-preflight.log、~/.cache/batch12-preflight-tests.log。
结果（集成者 b6fa1f80 在叠入与启动预检后被上游服务的内容过滤中断，协调者 2f0cdf04 接手后续步骤）：

- 预检 .32：PREFLIGHT PASS（vet 两套 tag + 完整非 race 套件），无过期 pyref。
- compare .32（setsid/nohup，~/.cache/batch12-compare-run）：baseline new=0 stale=0；全部 difftest 包 PASS；SYNTHETIC LIVE PASS 23/23，0 skip；GATE STAGE PASS: compare。
- core .64（GATE_FULL=1 GATE_ONLY=core，-p 6，8 个变更包 fuzz 目标，setsid/nohup）：GATE STAGE PASS: core。
- 两半门禁都在 6714a866f 上运行；之后只追加本记录。
