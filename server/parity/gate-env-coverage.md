# gate 环境变量覆盖审查（go@215ab6b04）

范围：全仓 Go 测试的 `os.Getenv/LookupEnv("CONFIGFLOW_…")`、`t.Skip`，并扩查 return/条件分支及
INSTALLER_*；核对 server/scripts/gate.sh、proddiff.sh 和本机 cf-gate 包装。只审查脚本，不修改 gate。
下表“未覆盖”指脚本默认环境未显式启用，不能据此断言历史人工定向验收从未执行过。

## 脚本实际行为

- race 是不附加环境的 `go test -race … ./...`。
- difftest 阶段仅给该次 Go 进程 `CONFIGFLOW_DIFF=1`，包来自 `go list ./... | grep difftest`。
  文件名含 diff 的普通包不会被这条包筛选选中。
- 未设置 CONFIGFLOW_PY、CONFIGFLOW_REPO、CONFIGFLOW_SNAPSHOT；cf-gate 只传 GATE_SLOTS 和 -p/fuzz，未补这些值。
- proddiff.sh 默认使用独立 run.py；不等价于跑 Go 的 live interchange 测试，也没有默认启用 --a13-proof。
- 默认目录在 server，不能跨 Go module 覆盖 backend/agents/go-agent、backend/node-probe。

## 覆盖表

| 环境 / 测试与位置 | 默认 gate 是否执行 | 纳入方式 / 说明 |
|---|---|---|
| CONFIGFLOW_PY + CONFIGFLOW_REPO：repo 的 TestCrossCheckFreshInit、Operations、Interchange、C04Operations、LockExcludesPythonAndGo、PythonWriterKilledBeforeCommit；review_c03 的 TestWriteFromAnotherProcessIsSeen | **未覆盖，skip**；当前布局问题就是这一组被漏跑 | 在 synthetic live 阶段显式设解释器/项目根，选这些测试，建议带 race |
| CONFIGFLOW_REPO：repo.TestConfigTemplateMatchesBackend | **未覆盖，skip** | 同上，仅需根目录，不依赖私人快照 |
| CONFIGFLOW_PY + CONFIGFLOW_REPO：store.TestCrossCheckPython、PythonErrors、PythonConcurrentCommits、PythonOddDocuments、TestStoredRoundTrip | **未覆盖，skip** | 同一 synthetic live 阶段；StoredRoundTrip 未设置 SNAPSHOT 时只跑合成数据 |
| CONFIGFLOW_PY + CONFIGFLOW_REPO：repo.TestStoredRoundTrip 的 Python 部分（model_roundtrip_test.go） | **Go 部分执行，Python 部分静默不执行**，并非整个测试skip | 同上；不能以这个测试的默认绿色证明完成 Python 互通 |
| CONFIGFLOW_PY：model.TestTypesPythonRoundTrip、TestHRPythonWriters | **未覆盖，skip** | synthetic live 阶段设 PY 即可 |
| CONFIGFLOW_PY/REPO/SNAPSHOT：repo.TestN82ReviewPrivateRepositoryInterchange；model.TestTypesPrivateSnapshot、TestHRSnapshotSchema；repo/store TestStoredRoundTrip 的 private-snapshot 子例 | **私人部分未覆盖**；普通 proddiff 不会自动调用它们 | 单列 snapshot live 阶段，用既有只读离线快照和临时副本，沿用抑制内容输出；此任务没有读取或运行快照 |
| CONFIGFLOW_DIFF：16个 */difftest 包（清单见下） | **执行**，在 difftest 阶段；race 阶段这些live部分通常skip | 保持现有阶段，显式设 DIFF_PYTHON 可统一解释器；Fuzz函数这里只执行种子，变异还需传目标 |
| CONFIGFLOW_DIFF：api/agents.TestPublicAgentPython；TestA24FlaskBoundaries；review_a24.TestReviewA24TargetsIgnoreUnrelatedLegacySettings；agents.TestProxyDifferentialAgainstRequests | **未覆盖，skip**，因为不在 */difftest 包 | 把这些普通包中的精确测试名单加入 synthetic live 阶段；不是整份 client_tools_diff_test.go 都skip，其余Go-only测试已执行 |
| CONFIGFLOW_DIFF：webdav.TestTLSVerificationMatchesPythonDefault 的 Python 对照 | **Go 校验执行，Python 分支未执行**（return，不是Skip） | synthetic live 阶段补该测试及 DIFF=1 |
| CONFIGFLOW_A13_PROOF：api/agents.TestRealPreparationProof | **未覆盖，skip** | 专用 `server/scripts/proddiff.sh --a13-proof` 已有隔离namespace/fixture准备；不能单纯设一个空目录冒充执行 |
| CONFIGFLOW_L1_CONTAINERS：installer 的 TestL1CompactMountsWithRealCompose、HAStartupWithRealSocketsAndSupervisor、RollbackWithRealInitManagers、ReviewFailureInjectionWithRealInitManagers、HAVerificationWithRealDNSClients | **未覆盖，skip** | 独立具备Docker/Compose/init条件的L1作业；不要把缺容器前提记成通过，亦不建议在所有race全局强开 |
| CONFIGFLOW_WRITE_ARCHIVES：deploy.TestWriteServerArchives；CONFIGFLOW_SERVER_ARCHIVES：go-agent/routes.TestServerArchive | **生产端导出skip，消费端不属server module** | 单列合成归档conformance链：先导出，再cd go-agent模块给SERVER_ARCHIVES跑消费端；导出成功不等于提取器校验成功 |
| INSTALLER_SAMPLE_DIR：installer.TestWriteShellcheckSample | **未覆盖，skip** | 这是shellcheck样本产物入口，接既有Python样本/shellcheck作业；不要把只导出文件当成完成shellcheck |
| CONFIGFLOW_*_HELPER、CONFIGFLOW_REVIEW_N73D_*_CHILD、CONFIGFLOW_C37_RESTART_DIR/JOB | **执行**，父测试自行给子进程设置 | lifecycle、lockx、substore、repo、discovery、rules、contentsync、pushdiff中已有exec.Env接线；不要在gate全局设置这些helper开关 |
| CONFIGFLOW_DIFF_PYTHON、CONFIGFLOW_YAML_PYTHON | **不是必需门开关**；多数真实Python引用有 ~/cf-test/.venv/bin/python 回退 | 测试服务器有解释器时普通race已跑，如routers、routersplit、yamlx等；缺解释器才会skip，建议启动预检fail而非静默跳过 |
| CONFIGFLOW_ROOT（oxidns） | **执行**，TestMain填入默认根路径 | 没有因该变量为空跳过测试 |
| CONFIGFLOW_R07_MUTATE、INSTALLER_MATRIX_FULL | **正常用例执行**，前者只选可选故意破坏，后者只增强失败诊断 | 不应误列成正常覆盖缺口；mutation负控若要常跑应另列，不能全局破坏普通stage |
| backend/node-probe、go-agent startup 的 CONFIGFLOW_*CHILD/CA/DNS 类 | **不在server gate模块范围** | 各自模块的父子进程/系统DNS测试另行验收；系统环境opt-in不要混作普通server单测开关 |

16个difftest包：api/agents、api/aggregations、api/oxidns、api/rulelibrary、api/rules、api/subscriptions、
devicenotes、discovery、health、httpx、mosdns、provider、refresh、subs、substore、webdav（均带 /difftest 后缀）。

helper明细：CONFIGFLOW_LIFECYCLE_HELPER、LOCKX_ORDER_HELPER、LOCKX_FLIGHT_HELPER、SUBSTORE_ORDER_HELPER、
REPO_ORDER_HELPER、REVIEW_N73D_MATCHER_CHILD、REVIEW_N73D_CONTENT_CHILD、REVIEW_N73D_LISTING_CHILD，
以及C37_RESTART_DIR/JOB。源码已核对父测试的exec.Env，不是只按环境变量名字猜测。

## 建议给协调者的 gate 改法（本分支不修改脚本）

1. 保留普通race和现有16包difftest；在difftest附近增加一个**合成 live** 命令，避免把整个race阶段
   全局打开DIFF而令所有live HTTP差分重复执行，也避免只给 */difftest 包设置PY后误以为repo/model已跑。
2. 明确项目根/解释器，预检解释器可执行及需要的依赖；缺前提直接失败。默认该合成命令unset SNAPSHOT。
3. 通过 `-json` 保存逐测试结果；维护必跑live测试名单并断言这些测试的run/pass存在、没有skip。
   仅设环境变量不足以防止以后 helper 又增加静默return 或错误根目录导致绕过。
4. 私人快照互通、A13 proof、归档提取器conformance、L1容器、shellcheck是不同前提的明确作业，
   在批次矩阵登记实际PASS或尚未执行。普通proddiff覆盖不能替代它们。

合成live命令示例（由协调者落实，执行目录server；沿用pflag，按负载选-p）：

```sh
root="$(cd .. && pwd)"
python="${CONFIGFLOW_PY:-${CONFIGFLOW_DIFF_PYTHON:-$HOME/cf-test/.venv/bin/python}}"
test -x "$python" || exit 1
unset CONFIGFLOW_SNAPSHOT
CONFIGFLOW_PY="$python" CONFIGFLOW_REPO="$root" CONFIGFLOW_DIFF_PYTHON="$python" CONFIGFLOW_DIFF=1 \
PYTHONDONTWRITEBYTECODE=1 go test -race -count=1 -timeout=40m -run \
'^(TestCrossCheck.*|TestConfigTemplateMatchesBackend|TestWriteFromAnotherProcessIsSeen|TestStoredRoundTrip|TestTypesPythonRoundTrip|TestHRPythonWriters|TestPublicAgentPython|TestA24FlaskBoundaries|TestReviewA24TargetsIgnoreUnrelatedLegacySettings|TestProxyDifferentialAgainstRequests|TestTLSVerificationMatchesPythonDefault)$' \
./internal/repo ./internal/store ./internal/model ./internal/api/agents ./internal/agents ./internal/webdav
```

此命令是覆盖方案，不宣称本任务已经运行全部普通包外置live差分；本任务的定向结果在
[fix-repo-crosscheck.md](progress/fix-repo-crosscheck.md)。
