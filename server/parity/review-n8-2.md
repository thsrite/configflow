# N8-2 / A07 代理差分对抗性复核

状态：**verified**；N8-2 未关闭问题 **0**。分支 `go-review-n8-2`。作者之外独立复核。

## 发现与修复

| 严重程度 | 发现 | 修复 / 边界证据 |
|---|---|---|
| medium | store.Commit 提交完全相同的 raw metadata 仍执行写入，generation 的空白格式也会被重写 | 过滤 byte-identical metadata；meta-only 写入不改无关 generation。新 no-op 回归先复现失败，再修复 |
| medium | decodeError 模拟 sqlite3 异常并回显整个非 UTF-8 行，可泄露 token、密码等任意字段 | 删除模拟；报告表名/键/列、一基字节偏移、错误类别。非 UTF-8 统一 ErrCorrupt；坏 JSON 使用 encoding/json SyntaxError.Offset；不打印 value |
| medium | 坏行启动失败缺少完整原路径/可执行恢复说明，错误只到 stderr | repo 原文件路径及停止服务/保留原文件/备份恢复/最后可读版本导出提示；最终 startup 边界 logx 同时输出日志和 stderr |
| low | storage_version 的错误诊断回显值，超大数用 big.Int 解析并整段输出 | 原生 strconv.ParseInt；错误类型提示无 value，超大正数给有界的 unsupported 提示 |
| medium/security | N2 简化环境选择器遗失 CGI 的 httpoxy 防护；原 differential 28 次请求有 5 处失败 | 改共享 httpproxy，CGI 不发请求。loopback 直连和其余原生差异经协调者明确接受；保留 ALL_PROXY 用户功能 |

## 范围与接受差异

- 设计 §2.4 的原始行、schema/version、row positions、revision、SQLite 原子事务保留。
- **语法损坏** JSON / 非 UTF-8：依 C01/C03 决定拒绝启动并保全原库，不自动重建。
  合法 JSON 的错误类型 R5 保留/读零/单次 warning 属 N8-3，不在临时桥里另造模型。
- **repo.update 入口 detach（PORTING 4b）→ N8-5/O3-6**。协调者明确本轮不提前迁移。
  store API 没有请求 ctx，底层事务用 background；新测试在 BeforeCommit 阻塞时取消
  发起请求，随后放行，确认整次提交成功。外层等锁取消行为仍由后续步骤迁移。
- 指针身份、Python 内部异常文本不追；原始行字节、未知成员、顺序、no-op head 与
  revision 必须保持。语法损坏 UTF-8 改统一 ErrCorrupt，是安全诊断分类变更。
- 代理决定及恢复配置方式见 behaviour-changes.md 的 N2 记录。旧 A07 决定 5 失效；
  httpproxy 使用已有 x/net 依赖。ALL_PROXY 仅作 Config 的空 scheme 回退，其余选择、
  CGI、NO_PROXY、解析均交原生库，未保留自写 matcher 或 Python 代理模拟。

## 覆盖与验收

已逐分支对照 backend/common/config_store.py（document_rows/build_document、读快照、
metadata/head、diff、commit/apply、build/install/check/salvage/backup），Go store 与
临时 repo/store_rows.go 桥；读取作者的 N8-2 记录及现有跨语言测试。

- 保留作者 TestStoredRoundTrip（12+12+私有库43行）、真实 Python ConfigStore 交叉检查、
  跨进程并发/冲突、失败回滚、backup、field order/revision 测试。
- 新增 store/review_n82_test.go：完全相同 metadata/no-op；7 种坏行（含 secret、截断、
  非 UTF-8、metadata、超过 JSON 最大深度、10 MiB 损坏行）；约9 MiB 合法 opaque 行
  原样读写与8-writer单赢家；取消后完成；100k-digit version 有界诊断。
- repo/review_n82_test.go + testdata/review_n82_interchange.py：私有只读备份、真实
  Python ProfileRepository 和 Go Repository 双向 profile 写入、无改动提交；未知
  坏行启动的定位/恢复/不泄密，以及原 DB 文件逐字节不变。
- agents/proxy_diff_test.go：真实 requests / Go、loopback proxy/Agent +仅 DNS 映射的
  synthetic routable host。14环境×3目标，18 个明确原生结果差异、24 个相同结果，分别断言完整 target、Host、代理认证和
  Authorization；CGI 为错误且0请求。无 mask/跳过差异。
- Agent / Sub-Store 新测试：ALL fallback、scheme 优先、小写优先、CGI/ALL 不绕过；
  旧 loopback 回归改为直连，Sub-Store 显式代理、proxy→direct redirect 仍实测。

`backend/test_config_store.py` 的 11 项全部保留：

| Python 行为 | Go 边界 |
|---|---|
| rows round-trip、额外键、Unicode、顺序 | store.TestRowsRoundTripPreservesOrderUnicodeAndExtraKeys / TestStoredRoundTrip |
| repository export after reopen | repo.TestRepositoryExportIsByteIdenticalAfterReopening / 新私有仓库往返 |
| pragmas/file mode、WAL reset | store.TestPragmasAndFileMode / TestWalModeIsResetToDeleteByCheck；repo 对应测试 |
| 只写变化行、删除 profile cascade、profile reorder | store.TestCommitWritesOnlyChangedRows / TestDeletingAProfileCascadesItsFieldsAndReorders / TestReorderingProfilesIsAChange |
| 空 diff、meta-only、generation | store.TestEmptyDiffAndMetaOnlyCommitDoNotBumpGeneration / 新 no-op metadata 回归 |
| 外部 head/revision 冲突 | store.TestHeadMovedOutsideTheLockRaisesConflict / Go-Python 跨进程并发；repo SaveProfile/SaveShared 三方合并与 stale revision 原有交叉检查 |
| 非 JSON 在写入前失败 | store.TestRawRowsRefuseInvalidEnvelopesBeforeWriting；旧任意 Go 值改为 raw JSON 验证；repo.TestRawStoreBridgeRejectsNonJSONWithoutWriting |
| Fingerprint 读稳定、写变化 | store.TestFingerprintIsStableAcrossReadsAndChangesOnWrite；实际行字节/field revision 断言，非 Python 哈希格式 |

额外检查：raw 源码未引入 jsonx/pystr 等 emulation；错误不携带 raw value；临时
repo 桥是既定 N8-3 交接层；排序比较保持原始位置，复合 row key 是 struct；SQL 的
BEGIN IMMEDIATE + head 核对保证并发单赢家；提交失败 rollback、Entry 返回值不
共享调用者的可变字节由作者测试和新增并发大行测试共同覆盖。无功能省略。

迭代用 .63；真实代理 differential 和私有快照验证均在 .32。最终命令/尾部输出见下。

已通过迭代：.63 `go vet ./...`；真实 Python 环境下 store 12.910s、repo 49.691s、
weburl 0.003s、agents 50.050s、substore 8.168s。`.32` 原始 store 往返 2.015s，
新私有 repository 双向写入 1.560s。一次新测试回调误把 hosts 当对象而 panic，已改用
mihomo 中的合成未知成员后通过；这是测试构造错误，无生产代码修复或私有内容输出。

代理最终迭代：.63 vet/agents 41.321s、substore 6.481s、weburl 0.003s；.32 42请求
差分 **18 个显式结果差异 / 24 相同，4.511s**。3种合成坏库的真实CLI启动：退出1，
日志/stderr均有安全恢复提示，原数据库逐字节不变；即使 LOG_LEVEL=CRITICAL 也可见。

最终源码再检查发现 ParseInt 对很长的浮点字面量可能先报告 range 而不是 suffix
语法错误；已在范围判断前排除 `.eE`，保证坏版本仍分类为 ErrCorrupt，不误报为新版库。
三条回归（长小数、长指数、长负整数）；.63 store 全包 live-Python race **16.131s**。
此补充发生在 .32 组合验收进行中；最终 store 源码已在 .32 补跑全包 live-Python/private race **12.510s** 与全仓 vet，通过。

A15 consumer 补验：旧 TestUpgradesProxyDifferentialAgainstFlask 的6条路由中，4条
loopback trace 因新决定出现预期差异，已实测复现（响应/Agent调用无差异）。现对
status/logs/restart/address 四条命名路径逐一精确断言 Python proxy trace 与 Go 空
trace；升级 trust_env-off 保持两端0次代理、可路由 follow 保持两端1次代理，并严格
比较完整 trace、HTTP响应和实际Agent请求。没有整行skip或通用mask。


## 最终命令与尾部输出

主验收在基点 `0efddadf`，退出0；包并发 `-p=2` 仅降低共享服务器负载，不抽样、不
改断言。五包 race 开启真实 Python / 私有库环境；普通全量不额外打开 opt-in differential。

```sh
rt -- bash -lc 'set -euo pipefail
cd server
format_paths=$(gofmt -l internal/ tools/ cmd/)
if [[ -n "$format_paths" ]]; then printf "%s\n" "$format_paths"; exit 1; fi
go vet ./...
CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD") CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data go test -race -p=2 -count=1 -timeout=30m ./internal/store ./internal/repo ./internal/agents ./internal/substore ./internal/weburl
CONFIGFLOW_DIFF=1 go test -count=1 -v -run TestProxyDifferentialAgainstRequests ./internal/agents
go test -p=2 -count=1 ./...
cd ..
binary=$(mktemp /tmp/cf-n82-startup.XXXXXX)
trap '"'"'rm -f "$binary"'"'"' EXIT
cd server
go build -o "$binary" ./cmd/configflow
cd ..
PYTHONPATH=$PWD ~/cf-test/.venv/bin/python server/scripts/review-n8-2-startup.py "$binary"
server/scripts/proddiff.sh'
```

- gofmt/vet：无输出，退出0。
- race：store **18.087s**、repo **217.966s**、agents **43.141s**、substore **6.174s**、
  weburl **1.014s**，全部通过（不是全仓 race）。
- requests：`42 recorded requests compared; 18 explicit native N2 outcomes`；
  `ok configflow/internal/agents 5.510s`。
- 全量所有包通过：installer **160.758s**、oxidns **88.397s**，尾部：

```text
ok configflow/internal/weburl 0.003s
ok configflow/internal/yamlx 0.922s
?  configflow/tools/proddiff/generator [no test files]
3 synthetic corrupt startup cases: exit=1, safe log+stderr, original database bytes unchanged
baseline: new=0 stale=0
```

Proddiff：23项自检PASS、2831cases/415comparisons、65 GET/66 method-path；11条既有
baseline，`new=0 stale=0 coverage_regressions=0 unmapped_routes=0`。Python/Go各30次
对称 transport failure 单列，未算成功；可用的生成器/订阅输出全部成功。没有改baseline。

末次版本边界修复后的 .32 补验（store race/vet 退出0）：

```sh
rt -- bash -lc 'set -euo pipefail; cd server; test -z "$(gofmt -l internal/ tools/ cmd/)"; go vet ./...; CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD") CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data go test -race -count=1 ./internal/store'
```

`ok configflow/internal/store 12.510s`，包含私有读写往返和真实Python交叉检查。

最终 rebase 无冲突，含 O3-1/O3-2、R05；随后 .32 再检查格式/vet 和 A15消费边界：

```sh
rt -- bash -lc 'set -euo pipefail; cd server; test -z "$(gofmt -l internal/ tools/ cmd/)"; go vet ./...; CONFIGFLOW_DIFF=1 go test -race -count=1 -v -run TestUpgradesProxyDifferentialAgainstFlask ./internal/api/agents/difftest; cd ..; find "$HOME/cf-prod-snapshot/runs" -maxdepth 1 -type d \( -name "n8-2-roundtrip-*" -o -name "review-n82-*" \) | wc -l'
```

```text
match proxy status           → 200 (Python proxy=1, Go proxy=0, Agent=1)
match proxy logs             → 200 (Python proxy=1, Go proxy=0, Agent=1)
match proxy restart          → 200 (Python proxy=1, Go proxy=0, Agent=1)
match proxy address describe → 200 (Python proxy=1, Go proxy=0, Agent=1)
match proxy update (trust_env off) → 202 (Python proxy=0, Go proxy=0, Agent=2)
match proxy follow           → 200 (Python proxy=1, Go proxy=1, Agent=1)
6 proxy cases, 4 explicit native N2 outcomes, 0 unexpected differences
ok configflow/internal/api/agents/difftest 5.360s
0
```

变基后的二进制也重跑 startup 脚本，3/3通过，原库字节不变。可复用以上主命令中
mktemp/build/Python片段；临时二进制用trap删除。最新变基只增加 A14 设计与 A19 复核文档。
协调者明确不冻结 go；合并前会在集成分支上再跑全套，本报告不宣称最新 O3/R05
代码又跑过一次全量。未推送、未开PR，无依赖新增。

## 交接与文件

提交：`dcab48dc`（store/no-op/安全诊断）、`1a331eb0`（原生代理）、`9744bef9`
（CLI边界脚本）、`8f065c7a`（版本分类）、`17f7d48e`（A15路由断言），另有最终验收
文档提交。最终基点 `045f2485`。详细差异决定已同步 WORKFLOW、behaviour-changes、
PROGRESS-agents 和用户手册 doc/manual/system-settings.md（环境配置说明，无UI截图变更）。

新增文件及行数：store/review_n82_test.go **194**；repo/review_n82_test.go **147**；
repo/testdata/review_n82_interchange.py **80**；agents/review_n82_proxy_test.go **48**；
substore/review_n82_proxy_test.go **42**；scripts/review-n8-2-startup.py **53**；本报告。
无私有数据、真实域名/IP/凭据或临时debug文件提交。
