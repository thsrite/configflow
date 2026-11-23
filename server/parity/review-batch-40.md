# batch-40 复核：O18 T8 metrics/traffic、monitor/rankings 去掉模拟的 sqlite3 事务与 Python 异常（C11）

复核者：Opus 5.5，2026-11-23。分支 batch-40（go 之上 c458f0006），复核修正见本文件所在提交。

## 结论

事务改写正确，存储兼容。修了两处低风险缺口（回滚失败无日志、建连中途 panic 会遗留打开的写事务），补登 behaviour-changes。

## 逐项核对

1. **事务语义**
   - 流量：`inTransaction` = `BeginTx` → body → 出错 `Rollback` / 成功 `Commit`。存槽（save：写入+整理+裁剪）、迁移、删除、保留
     都走它；所有调用方 `defer c.close()`，close 对未结束的事务再回滚一次，body panic 时也能回滚。ctx 取消：database/sql 在 ctx
     结束时自己回滚（随后 `Rollback` 为 `ErrTxDone`），提交前检查 ctx 则主动回滚。
   - 排行：`withContext` 在 `BeginTx` 之后 `defer tx.Rollback()`，覆盖出错、ctx 取消、panic；提交后回滚为 `ErrTxDone`。
   - 加锁：两处都用 DSN `_txlock=immediate`（modernc v1.62.0 支持；`file:` URI 的查询参数 SQLite 会忽略未知项），`BEGIN IMMEDIATE`
     在 `busy_timeout`（流量 10 s、排行 30 s，建连时 PRAGMA 设置）内排队。流量与原来一致；排行从「首个写语句前隐式 DEFERRED BEGIN」变为
     事务一开始就 IMMEDIATE——只会更早拿写锁（不会出现读锁升级失败），WAL 下读者不受阻；进程内本来就有 `s.mu` 串行。
   - COMMIT 失败（如 rollback-journal 下等读者 SHARED 锁超时）：modernc 的 `tx.Commit` 在仍处于事务时自动 ROLLBACK，连接随后关闭。
   - 连接：每次操作独立 `sql.DB`（MaxOpenConns 1）+ `sql.Conn`，用完即关；与 store 包的配置库连接池（R10 读连接复用）无共享，不会互相阻塞。
2. **回滚失败顶替原错误已删除**：现在返回原错误。但回滚失败**没有日志**（被 `_ =` 丢弃）——**已修**：流量 `logRollback`、排行的 defer
   在非 `ErrTxDone` 时记 Warning（文案在各自 `messages.go`）。
3. **哨兵错误**：`ErrHoursOutOfRange`（仍 500）、`ErrTrafficSettingsShape`、`ErrSavedMetricsShape`（`errors.Is`，文案不变）、
   `errCounterInvalid/Range`。生产代码里已无 metrics 的 `TypeError/AttributeError/OverflowError/SQLiteError/OSError`。
   `cutoff` 新算法（|hours| ≤ 1 万年再按日/时相减）对负数与以前的 floor divmod 结果相同，越界判定都落到 500。
   流量失败原因文本（定时任务记录里）随之变为 Go 错误文本——作者未登记，**已补登** behaviour-changes「O18 T8」。
4. **半偶舍入**：仅剩 `metrics.isoUTC`（流量查询 `start`）与 `monitor.fromTimestampMS`（看板 `collected_at`），用的是 Go 的
   `math.RoundToEven`，不是模拟。`fromTimestampMS` 理由成立（同一输入输出可复现，保持远期毫秒值文本不变）；`isoUTC` 的 `start` 依赖当前时间，
   「保持以前返回的值」说法较弱，但只在精确平局时有别、无害，不改。metrics/monitor 中无其他 Python 风格舍入。
5. **存储兼容抽查**：在 .32 上用快照私有副本，改前（a2a92eca2）与改后分别运行同一临时探针（未提交）：流量全部聚合查询哈希、
   +8 天 Flush（整理 23514 行）后两表行数与内容哈希；排行全部查询哈希、+8 天 Maintain、存槽+重复存槽、再查询哈希、三表行数/哈希。
   两侧完全一致，`integrity_check` ok。（快照只有一个流量 Agent，保留/删除步骤未覆盖。）

## 发现

### 低

1. 回滚失败无日志（任务要求「回滚失败也有日志」）。已修。
2. `connectContext` 在迁移阶段 panic 时不关闭连接：打开的 `BEGIN IMMEDIATE` 会一直持有写锁，之后所有流量写入等 10 s 后失败。
   旧代码同样如此，只在程序缺陷时触发。已修：未交给调用方的连接一律在 defer 中关闭。
3. behaviour-changes 未登记 T8 的对外文本变化（OverflowError → 中文、定时任务记录里的流量失败原因）。已补。

新增测试 `monitor.TestReviewBatch40StoreSlotFailureRollsBackEverything`：用触发器让存槽最后一条语句失败，确认槽、时段累计、
行都回滚，重试可正常写入（不被遗留事务阻塞）。
