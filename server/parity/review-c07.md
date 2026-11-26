# C07 独立复核

分支 `go-review-c07`，起点 `go-port-c07@edde72d2b`，初始集成基底 `go@765c04edb`。
复核者不是作者。已完整读取 C07 的七个 Python 源文件，再核对实现和映射。
未启动子 agent、未 push、未接触生产；新增输入全部合成，所有测试/格式/vet 经 rt。
状态：**C07 独立复核 verified，high 0 / medium 1 / low 3，四项全部修复，0个模块内未决发现**。
最终完整 gate 排队达到900秒后按约定停止（exit75，无阶段开始）；协调者仍须在合并树运行完整门禁。

## 发现及修复

- **F1 / medium：合法旧计数字符串使整个任务列表不可用。**
  `run_count="1_000"`、`failure_count=" +01_002 "` 在真实 Python `_history` 输出整数
  1000/1002；Go 的 `big.Int.SetString(..., 10)` 拒绝数字分隔符，导致 `/api/tasks`
  Python200/Go500、`/api/notifications` Python200/Go503。
  现用标准 regexp 验证有符号十进制数字及数字间单个分隔符，再交 `big.Int` 保留精度；
  不接受 `1__0`、前后下划线或 0x/0b 前缀。没有扩展 CPython Unicode 模拟。
  `TestHistoryCountersAgainstPython` 从17扩到25组，包含超 int64 的有符号分隔符数值；
  `TestReviewC07LegacyCountersAndSilentBinding` 通过真实 Flask 比较两条 GET 及六条绑定边界，
  每次核对完整 JSON、headers 和 DB/JSON/log 后状态；不改 oracle 或 normalize。
  修复前两条 GET 和三组直接计数均失败；修复后定向测试通过。修复提交 `bf8003685`。
- **F2 / low：假 slot 及 runner 泄漏到后续测试。**
  metrics、refresh 及其 live harness、C35 live harness 的空 rows 未注册清理。
  C07 live harness 调用 `refresh.Wire` 后再覆盖 runner，cleanup 实际恢复成 Wire 的 runner，
  丢失进入 fixture 前的注册。顺序运行可让缺失 slot 看似就绪，污染 readiness 验收。
  所有假 rows 现使用 `t.Cleanup(SetRows(...))`；C07 fixture 直接作用域安装真实 rows，
  不调用有全局注册副作用的 Wire。C35 Sources 也恢复先前值而非清空整个注册。
  两个 `TestReviewTaskFixtureRestoresMissingSlots` 和
  `TestReviewC07FixtureRestoresRunner` 修复前分别观察 missing 列表变空、原 runner 被替换，
  修复后均通过。生产注册未改，无生产空 rows。修复提交 `bf8003685`。
- **F3 / low：已成功回滚的 listener 状态读取故障返回错误的恢复提示。**
  新端口真实 token 健康检查成功后，状态目录失去遍历权限；Update 为保存回滚快照而读旧文件，
  直接交出 PathError。现有 C06 handler 会将它变成503“无法访问 Web 监听配置”，
  而不是原合同的503“保存 Web 端口失败，已恢复原配置”。这不是原先监听丢失或500。
  现在该失败仍先关闭新 sockets、恢复旧 token，再返回统一恢复提示；保留关机取消优先。
  `TestReviewStateReadFailureAfterHealthRollsBackWithSafeError` 用真实无权限目录、实际HTTP探测，
  检查旧 socket/token/文件字节与新端口关闭，修复前检出 PathError，修复后通过。
  没有改持久化协议或用假写入替代故障。修复提交 `bf8003685`。
- **F4 / low：无效单键任务被误判为已知但尚未就绪。**
  metrics slot 缺失时，`PUT /api/tasks/interval` 接收
  `metrics_history_flush:unknown`，先按分类查询 readiness 而返回503；该 key 从来不属于
  metrics 的唯一合法任务，真实 Python 返回400，Go run 分派原来也返回400。
  interval 现在先拒绝这个非法 exact key，再对合法 key 查询缺失 owner。
  `TestReviewUnknownExactKeyDoesNotBecomeUnavailable` 修复前实测503/期望400，另保持
  合法 exact key 的 run/interval503 正控；`TestReviewC07UnknownExactKeyAgainstFlask`
  比较真实 Python 两条400、完整响应及 DB/files 不变。修复提交 `ae816cb07`。

## 审计结论

- 两个 GET 缺 slot 明确503并列出缺项。AvailableTasks 仅供部分后台观察；MissingTasks
  使未观察到的旧 task/deployment/router 通知保持原状，不将其视为恢复。
  ready run/interval 不依赖其他 slot；支持的已知缺失 owner503、未知操作400；interval
  先校验和提交，最后只查询自己的 rows，展示失败仍返回成功。生产没有安装假空 provider。
- WebDAV runner 使用 C35 的完整 stored Snapshot 和 Run/Upload，任务 admission 和后台工作
  Detach，Runtime 跟踪；既有回归确实使用 Start/BindRequest/Stop、回环DAV及真实回执，
  验证断开仍完整上传、调用前关机零上传零回执。未复制上传实现。
- logs clear、stats sampling、interval 写入 owner 在 Detach 后及本层提交前检查取消；
  stats HEAD 不采样；通知 HEAD 不迁移、不建文件、不替换 inode。notification leaf 观察在锁外、
  合并在叶锁内，等待可取消，callback 后和最终 rename 前均检查 ctx；MarkRead 传入工作 ctx。
  observation ticket 防止旧观察覆盖新 snapshot/notice/read。没有新 flock 或裸后台工作。
- C37 taskbridge 保留工作 values 和 Runtime generation，Runtime Spawn 创建新 repo Scope；
  `TestReviewManualAutoTaskKeepsWorkValuesWithFreshScope` 检查实际 Probe ctx、断开后工作继续、
  旧 generation 关机后取消且不会被重启复活。不是只检查传参 helper。
- BindSilent 负责 silent JSON/media 错误；run/read 的自定义 DTO 保留精确成员名、拒绝 null
  值错误形状；共享 binder 保留重复成员最后值替换、清空失败目标和原生 finite JSON 策略。
  新增真实 Flask 控制覆盖大小写、null、重复键、trailing document，未修改共享 binder。
- listener 的18项保留语义及3项废弃分界成立：Go 自己服务 HTTP/SPA，不存在 nginx -t、
  nginx→backend 网络跳或旧镜像 nginx template 替换。其余健康失败旧端口继续可用、真实不可写
  目录回滚、内部端口保留/不能共享、IPv4-only、认证和新端口设置读取均有实际对应测试。
  健康检查后/保存前、rename 前、active 切换前都有取消检查；两次提交间取消会恢复保存状态。
  新增 `TestReviewShutdownBetweenSavedStateAndActiveSwitchRestoresSelection` 在真实健康成功后
  只持有 active-pointer mutex，观察新状态文件实际提交，再关闭真实 Runtime 并放行切换；
  断言取消、旧文件恢复及旧 token/socket 可用，补验确切的两次提交间窗口（.32 race通过）。
- logs 成功响应继续经过中央 stored-token 脱敏；通知文本使用共享 diag.EventMessage，
  没有私有脱敏器。新增 `TestReviewLogFailuresRedactStoredTokens` 对 tail/info/clear500 的
  原始/一层/两层编码 token，及 `TestReviewRunFailuresUseCentralRedaction` 对四类409/404
  失败逐项验证中央出口；作者组合用例同时核对通知 API 和私有通知文件的 diag 文本。
- 已读 C29/C32/A24 和 O3-6a 问题类别；初始基底缺 C06/C33 报告，从各自复核分支补读，
  不将未合入的 C06 web-port HEAD/Bind 改动复制到本分支。

## CFG-07 证据方向

原分诊行225为 **Python500 / Go200**，原因是 Go 把 Stat EACCES 当成不存在；
原 Python logs.py:27 的 Path.exists 会传播权限错误，info 的 except 返回500。
作者修复5b6dcd1f3只改 Go 的 native fs.ErrNotExist 分流；本轮非root目录权限回归再次通过。
`log_stat_fault.py` 仍要求 expect500、真实不可遍历目录、数据库可读及指定路径的权限错误；
没有改 Python，也没有反向修成200或改变比较方式。作者记录的500/500与原方向一致。
本轮在两台服务器独立重新运行 R075-F08，均 compared1/diffs0/open0，155个比较器自检和
端到端 mutation check 通过；最终 .32 报告直接读取确认 python_status=500、go_status=500。
最终报告标注精确代码提交 ae816cb07，使用独立临时报告，没有改写全局 configdiff 结果。

## 已分派共享边界

以下是既定 O3-6c（6e5c7760）任务，不把 C07 的入口测试说成已关闭这些窗口：
repo callback 之后的最终 SQLite COMMIT、共享 WriteProfileJSON 最终替换、无 ctx 的
taskstatus.Record/Forget 最终 rename。尤其 C35 Run 在 Upload 因关机返回后仍可能写失败回执。
后合入方须接 RecordContext/ForgetContext 并用真实 Runtime 补验；不在 C07 复制共享 store。
R14/A16/A17 的真实 rows/runner、routers confirmed-empty apply、C06 handler仍归相应owner。

## 验证

最终基底 `go@d43cca6e4`（含C33及独立复核），代码修复 `bf8003685` / `ae816cb07`；
提交间窗口补验 `e32998805`。变基仅两个文档追加冲突，双方原文均保留，没有 Go 冲突。
全树 gofmt-l 无输出，go vet ./... 通过。没有新增依赖或用户功能/入口，无新增 doc 截图要求。

主要完整定向命令（.32，前台，退出0；末次F4增量另列）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/weblistener/portable_test.go -- bash -lc 'cd server && gofmt -w internal/weblistener/portable_test.go && test -z "$(gofmt -l internal tools cmd)" && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -p=4 -count=1 -timeout=10m ./internal/tasks/... ./internal/api/tasks ./internal/weblistener ./internal/notifications ./internal/api/notifications ./internal/api/logs ./internal/api/stats ./internal/webdav/... ./internal/refresh/... ./internal/agents/metrics ./internal/server && go test -race -count=3 -timeout=5m -run "^TestReviewManualAutoTaskKeepsWorkValuesWithFreshScope$" ./internal/discovery'
```

```text
ok configflow/internal/tasks                 2.607s
ok configflow/internal/tasks/difftest        30.919s
ok configflow/internal/api/tasks            12.476s
ok configflow/internal/weblistener           2.155s
ok configflow/internal/notifications        24.194s
ok configflow/internal/api/notifications     3.840s
ok configflow/internal/api/logs             12.187s
ok configflow/internal/api/stats            10.949s
ok configflow/internal/webdav               13.674s
ok configflow/internal/webdav/difftest       80.405s
ok configflow/internal/refresh             259.837s
ok configflow/internal/refresh/difftest      25.301s
ok configflow/internal/agents/metrics       27.128s
ok configflow/internal/server               2.315s
ok configflow/internal/discovery            6.249s
```

F4最后仅改 interval 三行判断；`.32` 启用真实 Flask 的完整 tasks/difftest race
**27.432s**、完整 api/tasks race **10.443s**，全树格式/vet仍通过。
`.64` 完整 api/tasks race/vet **5.433s**；`.32` exact-key400和合法缺失key503的正负控制
race count3 **2.981s**。对应命令：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/tasks/difftest/review_test.go -- bash -lc 'cd server && gofmt -w internal/tasks/difftest/review_test.go && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=5m ./internal/tasks/difftest && go test -race -count=1 -timeout=5m ./internal/api/tasks && test -z "$(gofmt -l internal tools cmd)" && go vet ./...'
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && go test -run "^$" -fuzz "^FuzzTaskHistoryCounters$" -fuzztime=60s -parallel=2 ./internal/tasks'
```

第二条是实际组合命令内的 fuzz 子命令：**686577 execs / 60.076s / PASS**，其后 parser 未变。
CFG-07 最后在 .32 执行（同一条 rt 先跑上述 exact-key count3和格式检查）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && test -z "$(gofmt -l internal tools cmd)" && go test -race -count=3 -timeout=5m -run "^TestReviewUnknownExactKeyDoesNotBecomeUnavailable$" ./internal/api/tasks && cd .. && mkdir -p "$HOME/.cache/rt-tmp/review-c07-cfg07-final" && server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups logs-real-stat-permission --baseline ae816cb07 --output "$HOME/.cache/rt-tmp/review-c07-cfg07-final/results.json" --findings "$HOME/.cache/rt-tmp/review-c07-cfg07-final/findings.md" > "$HOME/.cache/rt-tmp/review-c07-cfg07-final/console.log" 2>&1 && tail -10 "$HOME/.cache/rt-tmp/review-c07-cfg07-final/console.log"'
```

退出0，155自检/OK，MUTATION-CHECK 检出人为破坏；R075-F08 compared/diffs0，
open0、design_errors=[]、35.35s。`.64` 的独立同组复验为16.18s/相同结论。

诊断运行不计为通过：初始 .64 的 discovery 整包在
`TestN73CustomPortReaderMigrationGate` 长时间执行，先对本 worktree 的进程取 SIGQUIT 栈后停止，
再用 `go test -race -v -count=1 -timeout=90s ./internal/discovery` 定位，90s也停在该用例。
该用例校验 N7 冻结 YAML 语料，不是 C07 taskbridge；没有修改或跳过它、未据此断言产品缺陷。
C37手动任务专用三轮回归在两台分别通过8.544s/6.249s。未声称 discovery整包通过。

## 最终 gate

`.64` 前台运行 `server/scripts/gate.sh -p 6 FuzzTaskHistoryCounters:./internal/tasks`，
由 Python 子进程守卫读取实时 stdout；单机 agent slot上限保持1，没有绕过锁或在gate外跑全量race/proddiff。
07:48:41进入队列；07:57:17为补F4停下仍在排队的第一次尝试（exit241，无阶段开始）；
08:00:09在ae816cb07上恢复，保留原 **08:03:41（00:03:41UTC）** 绝对截止，暂停时间也计入900秒。
日志目录：`.64:~/.cache/rt-tmp/review-c07-final-resumed-20261118`。
最终 **08:03:41** 守卫按原截止停止排队，退出 **75**，末行：

```text
C07_QUEUE_LIMIT: original 900-second deadline reached without slot; stopping queued gate
```

没有取得slot，gofmt/vet/双tag/full-race/proddiff/difftest/fuzz等gate阶段全部未开始，
不计GATE PASS，本轮无proddiff new/stale/self-check汇总。上文的格式/vet、相关包race/live、
单独fuzz及CFG-07是确实执行完成的独立证据。完整门禁交协调者cf-gate，不重启或延长队列。
最后仅补两条已通过count3的正控及本文档收尾，无产品代码增量；工作区交回时干净。
