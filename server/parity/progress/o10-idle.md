# O10「空闲 CPU 收尾」（2026-11-22）

分支 `go-perf-idle`，已 rebase 到 `go@77c4c4538`（batch-28，含 R6）。

## 状态

两处 Go 多做的空闲工作已修（见「改动」）；测量见「前后」。没有改 `internal/model`、repo 读写编解码、GOGC/GOMEMLIMIT、
任何循环周期。

## 方法

- `server/tools/o10perf`，`--servers go,python --idle-only --idle-minutes 12`，主机 .64。新增 `--idle-profile-start N`
  （默认 120 s，与旧行为一致；设 0 时 profile 覆盖开机后的首轮刷新/首轮排行等）和 `idlestats.py`
  （10 s 窗口中位 / p10 / p90 / 5 min 后均值 / 分配中位）。
- 断网命名空间：规则源/订阅刷新全部失败，在 ≈30 s 与 ≈330 s 各重试一轮（31 个资源），两边相同。
- profile 按后台循环归类（按调用栈根函数：refresh、rulesync、discovery、monitor 排行、dnsha、dnsfollow、ipv6、
  webdav、GC…）。私有输出在 .64 `~/cf-prod-snapshot/runs/idle-*`，不提交。

## 空闲开销清单（改前，go@1ada5e9db，.64，12 min，profile 0–540 s 共 5.73 s CPU）

| 循环 / 来源 | 周期 | Go CPU（540 s 内） | 性质 | Python 同名任务 | 判定 |
|---|---|---:|---|---|---|
| scheduled-refresh：旧缓存名迁移 `rulefetch.migrateCache` | 首轮刷新时每个旧名缓存一次 | 1.42 s（31 次 `repo.Update`，每次整文档解码 ≈45 ms） | **一次性**（迁移后读新名；测量每轮都用新快照副本，所以每轮都出现） | 不迁移，每次直接读旧名 | Go 独有的数据迁移；单价来自写事务整文档解码（S3/T1a 范围）。未改，见「待裁定」 |
| scheduled-refresh：资源失败 → 状态 + 合并通知刷新 | 30 s 检查，失败 300 s 重试 | 0.16 + 0.09 s | 断网重试 | 同样逐条失败 + 写状态 | 正常（第 2 轮 330 s 只 0.13 s，Python 1.48 s） |
| scheduled-refresh：`due()`/缓存时间 | 30 s | 0.09 s（≈3–5 ms/轮） | 稳态 | 同 | 正常 |
| domain-discovery-auto | 120 s 首次，600 s | 0.57 s | 稳态（每 10 min） | ≈1.1 s（120–140 s 窗口） | Go 更省 |
| oxidns-ranking-history：`MaintainContext`/`mergeDay` | 60 s，合并每小时一次 | 0.52 s | 每小时一次 SQLite 合并 | 同样的 SQL 合并 | 正常（纯 Go SQLite 比 C 慢） |
| agent-rule-sync worker：首次写「离线等待」状态 | 每个 Agent 状态变化时 | 0.42 s（写事务整文档解码） | 一次性（状态不变不写） | 同样写一次 | 单价属 repo 写路径（S3/T1a） |
| **agent-rule-sync Round：每轮编码全部 Agent 收集凭据 + 脱敏所有显示值** | **5 s** | 0.18 s（稳态窗口 120–540 s 里 0.20 s / 420 s，占稳态 26%，≈2.4 ms/轮） | **稳态** | 只比较状态，只在上下线切换时脱敏那一行 | **Go 多做** → 已修 |
| webdav 自动备份：提交钩子 + 导出 | 每次非运行时字段提交 | 0.20 s | 由上面的写触发 | 同样触发（快照里 auto_backup 开） | 正常；但钩子在 auto 关时也整文档编码比较 → 已修（Python 先看开关） |
| oxidns-ha-publish（IdleRounds） | 2 s | 0.12 s | 稳态 | 同 | 正常 |
| dns-follow / ipv6-verification | 5 s / 5 s | 0.06 / 0.04 s | 稳态 | 同 | 正常 |
| metrics flush / router-rule-sync | 30 s / 30 s | ≈0 | 稳态 | 同 | 正常 |
| GC | — | 1.51 s（26%） | 随分配 | — | 随上面下降；不调 GOGC（R12） |

分段 CPU（改前 base 轮，Go / Python，秒）：0–30 s 0.69 / 0.27，30–60 s 2.48 / 1.86（首轮刷新；Go 含迁移 1.42），
60–120 s 0.96 / 0.61（首轮排行合并），120–140 s 0.36 / 1.12（域名发现），140–300 s 0.20 / 0.56，
300–360 s 0.13 / 1.48（第二轮失败重试），360 s–结束 0.42 / 1.13。**稳态（>5 min）Go 0.14% vs Python 0.63%。**
Go 平均值里的超出部分全部在开机后 2 min 内（一次性迁移 + 首轮写），不是稳态。

## 改动

1. `030737e1d go(agents)`：presence 扫描只比较状态，凭据只在要写上下线日志时收集。
   - `ObservePresenceRecords(records, PresenceSecrets)`：`PresenceSecrets` 是惰性函数，每次观察最多调用一次，且只在有切换要记日志时调用；
     失败时该行名字/心跳为 `[REDACTED]`（与原来的 hidden 分支输出相同）。
   - 进程内 presence 状态只保留 status（原来还保存脱敏后的名字/心跳，但从未读取）。
   - `RuleSyncAgents` 改用 `BorrowAgents`，持有不可变代的记录指针，`Credentials()` 按需编码；在写事务内（working copy）仍立即收集。
   - 日志文本、切换判定、重复 ID 取最后一条的规则不变。
2. `adef2e150 go(webdav)`：提交钩子先看 `auto_backup`，明确为 `false` 时不再编码比较两份整文档（Python 先判断开关）。
   缺失/null/类型不对仍走原路径（worker 会重新读设置），所以行为不变。
3. `172ba78c5 go(perf)`：o10perf `--idle-profile-start`、`idlestats.py`。

测试：`TestPresenceSecretsOnlyForLoggedTransitions`（空闲扫描 0 次取凭据、切换时 1 次、失败时隐藏）、
`TestAutoBackupOffSkipsOnlyAnExplicitFalse`；原 `TestTypedPresenceRetainsOnlySanitizedMetadata` 改为断言只保留状态。

## 前后（.64，go@77c4c4538 vs 本分支，同时运行，各带一个 Python，12 min）

loadavg 3–8（别的 agent 在 .64 上跑测试；四个进程同时承受同样的负载）。私有输出 `idle-b28-{base,fix}`。
CPU 采样分辨率 0.1%（10 s 窗口，100 Hz tick）。

| 空闲 12 min | go@77c4c4538 | 本分支 | Python（两轮） |
|---|---:|---:|---:|
| CPU 平均 | 0.82% | **0.74%** | 0.98% / 1.00% |
| 10 s 窗口中位 | 0.10% | **0.10%** | 0.30% / 0.30% |
| 窗口 p10 | 0.00% | **0.00%** | 0.20% / 0.20% |
| 窗口 p90 | 0.40% | 0.30% | 1.6% / 1.1% |
| 5 min 后平均 | 0.13% | **0.09%** | 0.59% / 0.59% |
| CPU 秒：0–140 s / 140 s–结束 / 合计 | 5.12 / 0.71 / 5.83 | 4.77 / **0.51** / 5.28 | 4.02–4.19 / 2.92 / 6.94–7.12 |
| 分配：5 min 后窗口中位 / 5 min 后总量 | 0.32 MB/s / 185 MB | **0.05 MB/s / 76 MB** | — |
| GC 次数 | 170 | 156 | — |

profile（0–540 s）按循环：agent-rule-sync Round 190 → **40 ms**（−79%）；webdav 310 → 180 ms（这一项随写入次数波动，
快照 auto_backup 开，修改 2 在这里不生效）；其余在噪声内。**三项都 ≤ 同机 Python**（改前就已满足，改后稳态再降 28%、分配降 84%）。
Go 剩下比 Python 多的只在开机 140 s 内：一次性旧缓存迁移 1.56 s（Python 无此步）；去掉它 Go 0–140 s ≈ 3.2 s < Python 4.0–4.2 s。

协调者提到的「测试服务器 2.2–2.4% vs Python 1.8%」应是 5 min 左右的短窗口（如 R4 的 5 min 测量），开机段（首轮刷新 + 迁移）
占比大；12 min 窗口下 Go 已低于 Python。PVE 正式机（2 核 E 核）未测（本任务按测试服务器验收）。

## 待裁定 / 留给后续

- **旧缓存名迁移的单价**：每个旧名缓存文件迁移一次，占一个 `repo.Update`（整文档解码 ≈45 ms），快照里 31 个 → 首轮刷新多 1.4 s CPU。
  只在切换到 Go 后第一次读到旧名时发生，生产里是一次性的；但 PVE 验收每轮用新快照副本，30 min 平均里约占 0.1–0.15%。
  可选：(a) 不动，测量时在报告里注明；(b) 一轮刷新的迁移合并成一个事务；(c) 等 S3/T1a 降低写事务解码成本后自然下降。需要改 repo 写路径的 (b) 未做。
- 首次写 rule-sync 状态、webdav 导出的单价同属写事务/整文档编解码（S3/T1a）。
- 周期与 Python 一致，未发现需要问的周期差异。

## 验证（.32，本分支 = go@77c4c4538 + 3 个提交）

```
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && echo "gofmt: [$(gofmt -l .)]" && go test -race -count=1 ./internal/agents ./internal/agents/rulesync ./internal/webdav; CONFIGFLOW_DIFF=1 go test -count=1 -tags configflow_difftest ./internal/api/agents/difftest ./internal/webdav/difftest; /tmp/preflight.sh .paseo__worktrees__302rx1dg__go-perf-idle'
gofmt: []
ok  	configflow/internal/agents	478.817s
ok  	configflow/internal/agents/rulesync	89.741s
ok  	configflow/internal/webdav	5.453s
ok  	configflow/internal/api/agents/difftest	268.464s
ok  	configflow/internal/webdav/difftest	24.745s
== vet
== full non-race tests (-p 8)
PREFLIGHT PASS
```
