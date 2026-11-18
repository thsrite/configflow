# repo live cross-check / gate env 分诊

分支go-fix-repo-crosscheck，从最新go@215ab6b04建立。只用合成数据，不读快照、不修改gate、不push、不派子agent。
O3-6b等待R10/6c的正式合入通知，本任务不改它的公共锁或产品实现。

## 已定位

唯一已定位的首坏提交：53ccc09bb734d65c1dcd95de65cd40bbf82b5e25，
`go(lifecycle): O3-6 record repository order and replace resource locks`。
该提交将DeleteProfile/writeProfileFile的operation flock和共享/规则缓存叶flock改为进程锁；
O12已批准Python退休锁不再创建、已有不删除。crosscheck_test.go当时未更新。
不是N8引入的新DB/字段互通缺陷；当前失败是原布局把Python旧锁误当成必需的Go文件。

.64基线复现（无修改）：TestCrossCheckOperations26.31s、Interchange28.74s，包55.101s退出1；
都在layout line22：Go shared/ 对 Python profiles/.copy.operation.lock。

没有做整段历史二分；选择源码明确改变锁行为的提交，做相邻parent/child的无修改验证：
- parent7fd0dbf1850a122a96f3a9562a89efdb0b8d17bc：Operations -race PASS28.217s。
- 53ccc09bb：同命令FAIL26.974s（测试本体26.91s），同一layout line22旧operation锁差异。
- 该相邻提交未改crosscheck测试或Python参考。原有SQL/结果断言未放宽。

历史命令：分别在独立detached worktree /tmp/configflow-xc-before 与 /tmp/configflow-xc-after执行：
```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD") go test -race -count=1 -run "^TestCrossCheckOperations$" -timeout=10m ./internal/repo/'
```

## 测试修正

只在xcMaskedLayout排除精确退休文件：profiles/.<valid-id>.operation.lock；
shared/rules/(by-id|by-source)/<64 lowercase hex>.list.lock；以及C04语料实际访问的6个明确叶锁名字：
profiles/work/a/c.txt.lock、profiles/work/data/x.json.lock、profiles/work/generated/config.yaml.lock、
shared/cache/a.json.lock、shared/cache/missing.json.lock、shared/rules/x.list.lock。
没有排除整个目录、所有.lock、.config.lock、缓存正文/backup/invalid marker、migration备份。

xcOperationsFile在原始Go目录执行前后采集退休锁快照，拒绝Go新建、删除、改内容/inode/mtime/mode。
过滤无法掩盖新的锁回归。30条窄过滤子例和6个Guard负控验证上述边界。
C04扩查最初还出现profiles/work/a/c.txt.lock差异（281.79s），同属53ccc09bb的已批准叶锁退休；
已按精确fixture文件名补齐，不扩大通配目录，也不改任何结果、完整DB rows或互通断言。

## gate 覆盖审查

见[gate-env-coverage.md](../gate-env-coverage.md)。重点缺口除repo/store/model hard gate外，
还有普通包中的4个DIFF gated测试，以及WebDAV和repo roundtrip的静默Python条件分支。
父测试自行设置helper环境的场景已覆盖，不能误报为漏跑；16个difftest包已在其阶段启用DIFF。
这里只给方案，不修改gate.sh/cf-gate，不声称执行过私人快照或容器/实验网。

## 当前定向检查

首轮仅扩查C04因上面额外叶锁差异失败；主2条与窄过滤/Guard没有失败。
C04精确补齐后重验已退出0：repo346.929s（带真实Python和race，含全部30+6单元子例）。
最终需跑主2条live -race、相关cross-check调用、gofmt/vet；无新fuzz，无需完整gate。

C04重验命令：
```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/repo/crosscheck_layout_test.go -- bash -lc 'cd server && gofmt -w internal/repo/crosscheck_layout_test.go && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD") go test -race -count=1 -run "^TestCrossCheck(RetiredLock.*|C04Operations)$" -timeout=15m ./internal/repo/'
```
主序列与其余合成互通/锁等待/旧锁哨兵最后一轮检查正在前台运行；明确unset CONFIGFLOW_SNAPSHOT。
