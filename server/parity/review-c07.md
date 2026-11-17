# C07 独立复核

分支 `go-review-c07`，起点 `go-port-c07@edde72d2b`，初始集成基底 `go@765c04edb`。
复核者不是作者。已完整读取 C07 的七个 Python 源文件，再核对实现和映射。
未启动子 agent、未 push、未接触生产；新增输入全部合成，所有测试/格式/vet 经 rt。
当前发现 **high 0 / medium 1 / low 2**，三项均已修复并有修复前失败证据；最终验收进行中。

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
  修复前两条 GET 和三组直接计数均失败；修复后定向测试通过。修复提交待填写。
- **F2 / low：假 slot 及 runner 泄漏到后续测试。**
  metrics、refresh 及其 live harness、C35 live harness 的空 rows 未注册清理。
  C07 live harness 调用 `refresh.Wire` 后再覆盖 runner，cleanup 实际恢复成 Wire 的 runner，
  丢失进入 fixture 前的注册。顺序运行可让缺失 slot 看似就绪，污染 readiness 验收。
  所有假 rows 现使用 `t.Cleanup(SetRows(...))`；C07 fixture 直接作用域安装真实 rows，
  不调用有全局注册副作用的 Wire。C35 Sources 也恢复先前值而非清空整个注册。
  两个 `TestReviewTaskFixtureRestoresMissingSlots` 和
  `TestReviewC07FixtureRestoresRunner` 修复前分别观察 missing 列表变空、原 runner 被替换，
  修复后均通过。生产注册未改，无生产空 rows。修复提交待填写。
- **F3 / low：已成功回滚的 listener 状态读取故障返回错误的恢复提示。**
  新端口真实 token 健康检查成功后，状态目录失去遍历权限；Update 为保存回滚快照而读旧文件，
  直接交出 PathError。现有 C06 handler 会将它变成503“无法访问 Web 监听配置”，
  而不是原合同的503“保存 Web 端口失败，已恢复原配置”。这不是原先监听丢失或500。
  现在该失败仍先关闭新 sockets、恢复旧 token，再返回统一恢复提示；保留关机取消优先。
  `TestReviewStateReadFailureAfterHealthRollsBackWithSafeError` 用真实无权限目录、实际HTTP探测，
  检查旧 socket/token/文件字节与新端口关闭，修复前检出 PathError，修复后通过。
  没有改持久化协议或用假写入替代故障。修复提交待填写。

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
- logs 成功响应继续经过中央 stored-token 脱敏；通知文本使用共享 diag.EventMessage，
  没有私有脱敏器。新增 `TestReviewLogFailuresRedactStoredTokens` 对 tail/info/clear500 的
  原始/一层/两层编码 token，及 `TestReviewRunFailuresUseCentralRedaction` 对四类409/404
  失败逐项验证中央出口；作者组合用例同时核对通知 API 和私有通知文件的 diag 文本。
- 已读 C29/C32/A24 和 O3-6a 问题类别；当前树缺 C06/C33 报告，从各自复核分支补读，
  不将未合入的 C06 web-port HEAD/Bind 改动复制到本分支。

## CFG-07 证据方向

原分诊行225为 **Python500 / Go200**，原因是 Go 把 Stat EACCES 当成不存在；
原 Python logs.py:27 的 Path.exists 会传播权限错误，info 的 except 返回500。
作者修复5b6dcd1f3只改 Go 的 native fs.ErrNotExist 分流；本轮非root目录权限回归再次通过。
`log_stat_fault.py` 仍要求 expect500、真实不可遍历目录、数据库可读及指定路径的权限错误；
没有改 Python，也没有反向修成200或改变比较方式。作者记录的500/500与原方向一致。

## 已分派共享边界

以下是既定 O3-6c（6e5c7760）任务，不把 C07 的入口测试说成已关闭这些窗口：
repo callback 之后的最终 SQLite COMMIT、共享 WriteProfileJSON 最终替换、无 ctx 的
taskstatus.Record/Forget 最终 rename。尤其 C35 Run 在 Upload 因关机返回后仍可能写失败回执。
后合入方须接 RecordContext/ForgetContext 并用真实 Runtime 补验；不在 C07 复制共享 store。
R14/A16/A17 的真实 rows/runner、routers confirmed-empty apply、C06 handler仍归相应owner。

## 验证

待最终命令和门禁结果补齐；不会把作者门禁排队退出75或本轮中止的诊断运行计为通过。
