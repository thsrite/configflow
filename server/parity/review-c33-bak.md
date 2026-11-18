# C33 缓存备份合同修复

分支 `go-fix-c33-bak`，从本地 `go@d43cca6e4` 新建，最终变基到 `go@a9d6ce86c`（c31fbf306的N7-3d独立复核，加一条仅修改WORKFLOW的批次门禁说明）。只用合成数据和回环服务，不 push、不创建子 agent，不改比较器。开工前已通过 [paseo 技能](/Users/sunbroke/.agents/skills/paseo/SKILL.md) 通知 O3-6c（6e5c7760）修改 `api/rulelibrary/cache.go` 及本包测试；不改它正在验收的 repo/taskstatus/rulefetch。O3-6c 已确认边界及并行修改范围。

## Python 合同与用途

- `backend/utils/rule_utils.py:write_validated_rule_cache` 调用 `repository.write_shared_text`，后者委托 `ProfileRepository._write_atomic`。
- `_write_atomic` 先尝试读取旧主文件。新内容经 temp/write/fsync 后 replace 主文件并 fsync 目录；成功后，才用另一个 temp 写入**旧主文件的完整字节**，fsync 后 replace `.bak` 并同步目录。不是把主文件先移走，也不是将新内容复制成备份。旧主文件读取失败时不更新备份；空文件是有效的上一版。
- 备份 temp 来自 `tempfile.mkstemp`，权限0600。备份失败不改变主写成功结果，也不截断已有备份。
- `.list.bak` 没有自动恢复主缓存的读取方；JSON 的 `.bak` 恢复入口只处理 JSON。规则缓存的实际消费者是 `_touch_unchanged_cache`（主文件和备份都匹配才只更新时间）和 `create_rule_library_item`（只有孤儿 `.bak` 也拒绝同ID创建，保护旧内容）。`backend/test_agent_publish_caches.py:test_unchanged_rule_cache_is_touched_not_rewritten` 明确验证这份上一版备份。
- 协调者已明确裁定：上述两个消费者及 O3-6c 的 WriteSharedTextIfChanged 依赖备份，缺失属于功能差异，必须补齐。没有登记为可忽略差异。

## 发现及修复

**C33-bak / medium，已修：暂存发布绕过共享 writer，丢失上一版备份。** T25 `library/operation-2` 与 `library/operation-3` 原来双方200，DB、主缓存和响应相同，但 Python 保留14字节/0600的 `.list.bak`，Go缺失。这样会改变 unchanged-cache 与孤儿ID保护行为。

修复在既有L3来源声明守卫通过后读取旧主文件；最后一次owner取消检查置于主rename前。主rename成功即进入不可取消的本地收尾：同步主文件目录、尽力原子刷新0600备份、清除失效marker并再次同步目录。备份失败不返回主写错误；旧备份保留。备份临时文件使用既有 `.rule-edit-<cache>-*.tmp` 命名范围，崩溃残留由下一次同ID编辑清理。没有新repo API、锁、goroutine或后台恢复流程。

F7过期stage在调用publish前即拒绝，因此不会读取/轮转备份。没有改变DB声明检查、revision/hook或缓存交付优先级。

## 回归

- `TestCachePublicationRetainsPreviousPrimary`：真实PUT轮转旧内容及空内容，备份0600，不生成 `.bak.bak`。
- `TestCacheBackupParticipatesInUnchangedCheck`：新主/旧备份不能touch；真实手动缓存更新备份后，主/备份同内容才能touch，inode不变、mtime更新。
- `TestCacheBackupProtectsOrphanIdentity`：真实PUT→DELETE留下备份；同ID再创建409，DB和备份不变。
- `TestReviewStageRejectsNewerCommittedDeclaration`：真实SQLite A/B交错后拒绝旧stage，备份字节/权限/inode/mtime均不变。
- `TestCacheBackupFailureDoesNotFailPrimary`：仅备份rename遇真实文件系统失败，主写200、marker已清、临时文件清理。
- `TestCacheBackupFinishesAfterRenameShutdown`：真实Runtime在主rename后关停，备份及marker收尾仍完成；原主rename前关机测试继续拒写。
- `TestReviewCacheCrashRecovery`：真实SIGKILL/reopen的 staged/committed/renamed/complete 四点均核对备份；重启不会交付失效主缓存，手动缓存修复后备份等于修复前实际主文件，不凭旧备份回滚新DB。

## 验证

定向回归，.64，全部退出0：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back server/internal/api/rulelibrary/backup_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/api/rulelibrary/backup_test.go; go test -race -count=1 -run "TestCache|TestReviewStageRejectsNewerCommittedDeclaration|TestReviewCacheCrash|TestShutdownBetweenDBCommitAndCacheRenameIsFailClosed" ./internal/api/rulelibrary'
```

`ok configflow/internal/api/rulelibrary 9.198s`。新增PUT fixture最初未提供完整替换声明，分别在来源校验及名称校验被400拒绝；已补齐合法完整body，未放宽生产校验，未将失败轮次计作通过。

T25命令（无tag生产二进制，原比较器）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; review_dir="$HOME/.cache/rt-tmp/c33-bak-t25"; mkdir -p "$review_dir"; server/tools/configdiff/native_network.sh "$HOME/cf-test/.venv/bin/python" server/tools/configdiff/run.py --groups writes-real-online-Agent-no-push-or-activate --baseline 0ddafc602 --output "$review_dir/results.json" --findings "$review_dir/findings.md" > "$review_dir/run.log" 2>&1; tail -n 6 "$review_dir/run.log"'
```

首轮代码验收提交为0ddafc602；后续仅补文档并变基，C33的Go代码未再改动。变基后的复验代码提交为a0bbc3d6a。两目标均 `state=compared, python_status=200, go_status=200, differences=[]`：

- `T25 / library/operation-2` PUT：DB、响应、全部文件及权限精确比较通过。
- `T25 / library/operation-3` DELETE：同上，6秒双端Agent观察均无请求/错误。

**整组退出1，不能称整组通过。** 16变体中compared14/different1/go-unimplemented1，design_errors为空，故意篡改Go响应的mutation-check正确检出。其他差异：`rules/operation-5` 的 `profile/default/mosdns.revision` Python1/Go10；preview/operation-1未实现。它们没有被本票修复或忽略；本分支仅叠在go上，未复制协调者工作树中的CFG三票。日志保留在 `.64:~/.cache/rt-tmp/c33-bak-t25/`。

重叠包race/live及全仓格式/vet，.32，退出0：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/ tools/ cmd/)"; go vet ./...; CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 ./internal/api/rulelibrary/... ./internal/api/rules/... ./internal/rulefetch ./internal/rules ./internal/server'
```

格式/vet无输出。library131.993s/live212.345s、rules API140.396s/live365.702s、rulefetch61.656s、rules29.558s、server2.565s均通过。

最终gate首轮：.64于08:05:45入队，日志 `~/.cache/rt-tmp/c33-bak-final-gate/`；尚未取得槽位时因最新go合入N7-3d独立复核而主动停止，退出143，未开始检查，不计通过。变基后a0bbc3d6a再跑同一完整T25组（.64，命令如下）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'review_dir="$HOME/.cache/rt-tmp/c33-bak-t25-rebased"; mkdir -p "$review_dir"; server/tools/configdiff/native_network.sh env GOMAXPROCS=4 TMPDIR="$HOME/.cache/rt-tmp" GOTMPDIR="$HOME/.cache/rt-tmp" "$HOME/cf-test/.venv/bin/python" server/tools/configdiff/run.py --groups writes-real-online-Agent-no-push-or-activate --baseline a0bbc3d6a --output "$review_dir/results.json" --findings "$review_dir/findings.md" > "$review_dir/run.log" 2>&1; review_status=$?; tail -n 6 "$review_dir/run.log"; exit "$review_status"'
```

两个library目标仍双方200、compared、differences=[]；整组compared14/different1/go-unimplemented1，退出1，额外差异仍仅为上述MosDNS revision，preview仍未实现，mutation-check检出、design_errors=[]。

变基后.32补验命令，退出0：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/ tools/ cmd/)"; go vet ./...; go test -count=1 ./internal/yamlx -run TestYAMLFacadeBoundary; CONFIGFLOW_DIFF=1 go test -race -p 6 -count=1 ./internal/api/rulelibrary/... ./internal/rules ./internal/server'
```

格式/vet无输出；AST门1.117s；library92.060s/live145.000s、rules19.379s、server2.451s均通过。

最终gate命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -e GATE_LOGS=/home/sunbroke/.cache/rt-tmp/c33-bak-rebased-final-gate -- bash -lc 'mkdir -p "$GATE_LOGS"; exec setsid bash -c '\''echo $$ > "$GATE_LOGS/gate.pid"; date -Is; exec server/scripts/gate.sh -p 6 FuzzEditableContent:./internal/api/rulelibrary'\'''
```

.64于08:15:16入队，至08:30:21仍未取得槽位（约15分05秒）。核对本worktree的PID/PGID/cwd后仅终止自己的进程组，命令退出143（Terminated），并确认进程已退出。日志目录只有gate.pid，没有检查开始记录。因此本轮gofmt/vet/build/race/proddiff/difftest/fuzz门禁阶段均未执行，不计GATE PASS，也未产生proddiff汇总。日志 `~/.cache/rt-tmp/c33-bak-rebased-final-gate/`。完整门禁按WORKFLOW批次约定交协调者在合并树运行cf-gate。

指定定向验证均已完成且如上记录；两项目标T25差异已消除，整组的其他差异/未实现项未豁免。仅proddiff被授权读取测试服务器离线快照，不连接生产，本报告未引用任何快照内容。

最终纳入a9d6ce86c仅有WORKFLOW文档差异，不改变c31验收源码；交回版本相对a0bbc3d6a的server/internal完全相同。文档收尾并入同一个修复提交，不新增第二个修复提交；工作区干净，未push。
