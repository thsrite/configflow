# O10-R10 HR 复核：SQLite 读连接复用（2026-11-22）

分支 batch-27（rebase 到 `go@4e478d651` 之后）：作者提交 eea36b689、fa81ff774、1112dd3ac；复核修复 f65a5f8e5。
读了 `internal/store` 全部源码、repo 对 store 的全部调用、`cmd/configflow` 关停钩子、`httpxtest`、o3-concurrency 关停阶段。

## 结论

设计成立：每次 `Reading` 都是新的 deferred 事务（`BEGIN` … `COMMIT`，`fn` 拿不到 exec，不能留长快照），
可见性由 SQLite 的 change counter / schema cookie 保证；文件替换靠 inode 检查兜住。发现 1 个中等问题（已修），其余为低风险说明。

## 发现

### 中（已修，f65a5f8e5）：WAL 模式下空闲连接让旧文件的 `-wal` 一直存在，rename 换进来的新文件被读成旧数据

- 场景：别的进程（迁移工具、手工操作）把库切成 WAL 并提交；之后恢复/导入用 rename 把新文件换到同一路径。
  空闲读连接（WAL 模式）一直开着，`<path>-wal` 不会在最后一个连接关闭时 checkpoint 并删除；下一个打开新文件的连接
  （写者、Check、另一个 Store/进程）看到同名 `-wal` 就按 WAL 打开，把旧文件的帧套到新文件上。
- 复现（修复前失败）：`TestRenameAfterForeignWALCommitReadsNewFile` —— 新文件被读成旧 store_id、generation 9。
  池化之前读连接用完即关，同样的步骤里最后一个连接会 checkpoint 并删掉 `-wal`，所以这是 R10 引入的。
- 修复：`release` 只保留处于 DELETE 日志模式的连接（`PRAGMA journal_mode`，无 I/O）；发现 WAL 的连接读完即关，
  行为回到池化之前。`TestWALConnectionIsNotKept`（修复前失败：kept 1 WAL connections）。
  `Check` 前的 `closeIdle` 保留（现在是双保险）。
- 正常运行库始终是 DELETE（启动 Check 会改回），所以只在外部改成 WAL 时触发；后果是静默读错/写坏，按中处理。

### 低（说明，不改）

1. **替换检测的竞态窗口**：`reader()` 先 stat 再借连接，到 `BEGIN` 之间文件被 rename 替换，这次读会读到旧文件的一致快照。
   与旧实现（open 到 BEGIN 之间被替换）等价，可线性化为“替换之前的读”；替换完成后开始的读一定 stat 到新 inode。
2. **inode 复用 / mtime 粒度**：空闲连接持有旧 inode 的 fd，旧 inode 号在同一文件系统上不会被新文件复用，
   所以同大小、同 mtime 的连续两次恢复仍能识别（`TestReplacementWithSameSizeAndMtime`，负向对照：池里那条连接直接读确实还是旧 head）。
   路径本身不允许是符号链接（openLocked 用 Lstat 拒绝）；数据目录是符号链接或 bind mount 时 stat 与 SQLite 都跟随同一目标。
   只有 inode 不稳定的网络文件系统（CIFS `noserverino` 之类）理论上可能误判，生产不是这种挂载。
3. **非 SQLite 的原地覆盖**（`cp` 覆盖正在使用的库、同 inode 同大小同 mtime）：只能靠文件头 change counter 等 16 字节区分，
   几乎不可能碰撞；这种操作本身就会损坏正在使用的库，不额外处理。
4. **Check 与并发归还**：`closeIdle` 之后，并发读的连接仍可能归还进池。Check 只在 `repo.Open` 的 openLocked 中调用，
   此时没有并发读；且 WAL 连接已不入池，不影响日志模式切换。
5. **其他需要独占的操作**：库里没有 VACUUM；Install/PreserveCorrupt 只在启动路径；BackupTo、SalvageMeta、写都用独立新连接，
   不受空闲读连接影响（DELETE 模式下空闲连接不持锁，`TestIdleReaderHoldsNoLockAcrossProcesses` 用 Python 进程验证 `BEGIN EXCLUSIVE`
   不等待；负向对照：读事务内同样的请求得到 `database is locked`）。
6. **schema 变化**：modernc 驱动不缓存语句，SQLite 每个事务检查 schema cookie；`TestReusedReaderSeesSchemaChange`（stat 不变，确实复用）。
7. **关停 / fd**：`store-close` 在 PhaseClose，注册在 `instance-release` 之前；关闭后读仍可用（每次开关）。读失败、`fn` 出错、COMMIT 失败、
   panic（defer）都走 `close`（含 rollback）。空闲连接的 fd 现在跨整个进程寿命，确认带 `O_CLOEXEC`，不会泄漏给子进程
   （`TestIdleReaderDescriptorIsCloseOnExec`，含 `Close` 后 fd 归零；负向对照：不带 CLOEXEC 的 fd 被识别）。
8. **临时 Store 泄漏**：非测试代码里 `store.New` 只有 repo 一处和 BuildTemporary（已 Close）；`tools/faults/fixture` 的 `repo.Open`
   不 Close，是短命进程，无影响。测试里未 Close 的 repository 每个最多留 2 个连接，preflight 通过。
9. busy_timeout 变化不复用；`statErr` 时清掉所有空闲连接后重开；均与代码一致。

## 验证（.64，rebase 前）

- `gofmt -l internal/store` 无输出；`go vet ./internal/store/ ./internal/repo/` 通过。
- `CONFIGFLOW_PY=~/cf-test/.venv/bin/python CONFIGFLOW_REPO=<repo> go test -race -count=1 ./internal/store/ ./internal/repo/` → ok / ok。
- CrossCheck/Raw/N85/TestWriteFromAnotherProcessIsSeen + 新增复核测试 `-v`：28 PASS，0 FAIL，0 SKIP。
- 修复前负向对照：`TestWALConnectionIsNotKept`、`TestRenameAfterForeignWALCommitReadsNewFile` 失败（见上）。

rebase 后的验证见交回消息（store/repo -race + `/tmp/preflight.sh`）。
