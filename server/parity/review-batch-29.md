# batch-29 独立复核（O10 空闲收尾）

范围：go@77c4c4538 之上 `172ba78c5`（o10perf 工具）、`030737e1d`（rule-sync presence）、`adef2e150`（webdav 提交钩子）、`a0900ab74`（PROGRESS）。
结论：**无需修改**，未发现缺陷。

## 1. `030737e1d` presence 只比较状态、凭据按需收集

**脱敏集合与改前相同。**
- 改前：`systemSecrets` + 每条记录的 `storedCredentials`（`ViewAgents` 回调内从借用记录立即收集）→ 一个集合，用于所有行。
- 改后：同一闭包（`systemSecrets` + 遍历全部 `records[i].Credentials()`），只是延迟到第一条要写的日志时调用一次，且每次观察最多一次（`loaded`）。
  遍历的仍是全部记录（含无效 ID），不只是切换的那条，所以集合不缩小。
- 同代：`RuleSyncAgents` 改用 `BorrowAgents`；`shared=true` 时保存的是不可变已发布代的记录指针，`Credentials()` 稍后对**同一条记录**编码，
  与 `Presence` 投影同代，不存在 R4 M1 那种记录/凭据跨代。`storedCredentials` 只做浅拷贝 + `Clear`（借用约定允许，copy-on-write）+ 编码，不改代。
  `shared=false`（写事务内工作副本）仍在 `RuleSyncAgents` 内立即收集。
- token 与嵌套密码：`storedCredentials` 编码整条记录（只清历史字段），`CollectCredentials` 递归收集，token 等私有成员都在，与改前同一函数。
  `ExternalPayloadSecrets` 另外带全部 Agent / 规则代理 token（读当前 head），与改前一样是单独一次读取 —— 只会多遮，不会少遮。
- 失败：`systemSecrets` 或任一记录编码失败 → 所有要写的行 name/heartbeat 为 `[REDACTED]`；渲染为 `presenceDisplay(id, Redacted, Redacted, nil)`，
  与改前 hidden 分支（Name/LastHeartbeat=Redacted，secrets=nil，再经 name 空回退 + `DiagnosticText`）逐字相同。ctx 取消同样落入此分支，与改前一致。
- 渲染顺序：`registryDisplay` → 空名回退 ID → `DiagnosticText(…,120)` / `(…,32)` → `LogText`，与改前相同。

**上下线判定一致。** 旧 `presence{status,name,heartbeat}` 中只有 `status` 被读过（`previous.status == item.status`）。
- 非 online 一律视为 offline；未知且非 online 的记录不跟踪（冷启动离线不记）；首次出现 online 静默跟踪；删除的 ID 从 `presenceKnown` 消失，重新出现按首次处理（重新注册同理）——逻辑逐行未变。
- 重复 ID：改前 `current[id]` 被最后一条（通过过滤的）覆盖；改后 `latest[id]=i` 同样取最后一条，渲染用该下标，下标与 `records` 对齐（两个入口都按原切片下标）。

**低（不改）**：惰性回调在持有 `presenceMu` 时做存储读取（`r.View`、token 读取）。只有 rule-sync 一个调用方会这样做，`presenceMu`
不被任何持有存储锁的路径获取（`rulesync.observePresence`/`ObserveRegistryPresence` 生产代码无调用者，且其 secrets 预先算好），不构成锁环；只在有切换的轮次发生。

## 2. `adef2e150` webdav 钩子先看 auto_backup

- 只有 `AutoBackup != nil && !*AutoBackup` 才跳过；缺失、null、错型（R5 回退为 nil）、true 都走原比较路径。测试覆盖五种形态 + nil 文档。
- 钩子看的是**提交后**的 `current`。false→true 那次提交：current 为 true → 比较，`System.Backup` 有变化 → 调度，与改前相同。
- true→false 那次提交：改前比较出变化后 `Schedule`（刷新 deadline），worker 到点读设置 `Auto=false` 不上传；改后直接返回，已挂起的那次仍在旧 deadline
  由 worker 读设置后跳过。结果相同（都不上传），且与 Python（`auto_backup and _configuration_changed`）一致。
- `Schedule` 在没有挂起项时只会启动一轮读设置；跳过它没有可观察副作用。

## 3. `172ba78c5` 工具（抽查）

`--idle-profile-start` 默认 120，profile 时长 `idle-120-180 = idle-300`，与原行为一致。`idlestats.py` 只读 `idle-series.json`。
极端参数（start 大于 idle-180）会得到非正时长，仅测量工具，不处理。

## 验证（.32）



```
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && echo "gofmt: [$(gofmt -l .)]" && go test -race -count=1 ./internal/agents ./internal/agents/rulesync ./internal/webdav; CONFIGFLOW_DIFF=1 go test -count=1 -tags configflow_difftest ./internal/api/agents/difftest ./internal/webdav/difftest; /tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-29'
gofmt: []
ok  	configflow/internal/agents	476.259s
ok  	configflow/internal/agents/rulesync	91.128s
ok  	configflow/internal/webdav	6.276s
ok  	configflow/internal/api/agents/difftest	267.314s
ok  	configflow/internal/webdav/difftest	24.253s
== vet
== full non-race tests (-p 8)
PREFLIGHT PASS
```
