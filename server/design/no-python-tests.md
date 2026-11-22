# 测试去 Python 化方案（切换前置条件）

2026-11-22，基线 `go@44424c19f`。只读盘点加方案，没有改产品代码，没有跑测试。
数字来自静态扫描（grep/AST 名称）和 `parity/gate-env-coverage.md`（2026-11-20 运行时清单），
WP0 会用一次「隐藏 backend 的真实运行」把它们核实成准确清单。

## 结论（先看这里）

1. **影响面很大，但大部分有现成办法。** 依赖真实 Python 的东西分成 8 类，大约 **72 个 Go 包、约 145 个测试文件**，
   外加门禁的 difftest/live 两个阶段和 proddiff/configdiff/routerdiff 三个工具。backend/ 一删，
   门禁会出现三种情况：大面积失败（调用 Python 失败）、**静默变绿**（缺解释器就 skip 的测试），
   或者测试根本没被选中（compare 阶段整体起不来）。
2. **核心做法：用「双方都跑过一次」的结果冻结期望值。** 在 Python 还在的时候，用 record 模式跑一次差分：
   Python 和 Go 都跑、比较通过以后，才把**归一化后的 Go 观察结果**（响应、写入后的 DB 行和文件）
   写进仓库里的 golden 文件。之后正常测试只跑 Go，和 golden 比较。
   这样录下来的值经过了 Python 核对，也自动带上了已经批准的行为差异（录 Python 那一侧反而会把修好的 Python bug 带回来）。
   机制直接扩展现有的 `internal/testgolden`（`PYREF_MODE=record/check`、`PYREF_ACCEPT=1`、精确输入作键、要求每条录制都被用到），不另起一套。
3. **分类处理：**
   - (a) **已有 pyref golden（4 个包 17 个文件、7.9 MB）**、以及约 177 MB 已提交的 Python 生成语料：数据本身已经自包含，
     只要去掉 backend 源码和 venv 的指纹校验、录制入口和 `PYREF_INPUT_FINGERPRINT`。工作量 S。
   - (b) **Flask/Python 差分改成冻结回放**：24 个 difftest 包 197 个顶层测试；15 个普通包 42 个 live 测试；
     约 27 个包里直接 `exec` Python 的参考调用（routers 设备写入边界最重要）。这是主要工作量。
   - (c) **建议删除**：Python 与 Go 同时运行的锁和并发互测、Go 写入后让 Python 读的方向、PyYAML 读 Go 产出的 YAML、
     WebDAV TLS 的 Python 预言机、「Go 副本必须等于 backend 文件」的同步检查、源码指纹。切换后不会再有 Python 进程
     和 Go 共用数据，也不回滚，这些保护对象已经不存在了。（详见第 3 节）
   - (d) **proddiff 改成「Go 上一版 vs Go 候选版」**，在同一份生产快照上比较；Python 侧的响应**不能**提交进仓库
     （包含生产数据）。configdiff / routerdiff 在切换候选树上最后跑一次、归档结果，然后退役；
     它们发现过的问题逐条确认已有 Go 回归测试。
4. **必须趁 Python 还在时录制的东西**：difftest 回放 golden、live 测试的参考值、routers/clients 的边界参考、
   Python 写出的存储 fixture（SQLite + 数据目录，合成数据），以及 configdiff 的 seed 库。估计新增约 **40–70 MB（gzip 后）**。
   **最终录制安排在切换候选树上、切换前 1 周内**（和 O14 P9 的 proddiff 刷新同一时间）。
   O17/O18 还在有意改行为，录早了会反复重录。
5. **门禁改造后**：compare 只剩「proddiff（Go vs Go）+ 前端」；difftest 和 live 两个阶段、`gate-live.py`、strace 证据、
   `PYREF_INPUT_FINGERPRINT` 全部去掉，转成 Go 回放测试后并入 core 的 `-race` 全量运行。
   core 新增一条「无 Python 审计」：测试代码里出现任何 Python 进程调用、或带 Python 字样的 skip，都直接判失败。
   附带的好处：golden 都在模块内，Go 测试缓存能正常命中，门禁会更快。
6. **代价**：冻结以后就没有独立的参考实现了。今后有意改行为时，只能从 Go 重录 golden，靠人审 diff。
   对策见第 4 节：保留 `python-final` 标签方便和 Python 最后版本对比；重录必须带说明，并登记到 behaviour-changes.md；
   不允许在没有 Python 核对的情况下首次录制。
7. **拆成 16 个工作包**（S×6 / M×7 / L×3），大多数可以并行。建议**切换前至少 2 周完成转换**，再连续跑两次
   「隐藏 backend 的预演门禁」，最后在切换候选树上做最终录制。

## 1. 盘点

### 1.1 门禁阶段（`server/scripts/gate.sh`）

| 阶段 | 现在做什么 | Python 依赖 | 删掉 backend/ 后 |
|---|---|---|---|
| core: gofmt/vet/build tags | 静态检查 | 无 | 不变 |
| core: pyref 指纹 | `go run ./tools/pyref -fingerprint`：给 backend/ 源码和 venv METADATA 算哈希，导出 `PYREF_INPUT_FINGERPRINT` | 读 backend/ 和 venv 文件（不执行 Python） | 哈希变了，**所有 pyref golden 报「Python 参考已变化」** |
| core: `go test -race ./...` | 全量测试 | 约 27 个包的测试用固定路径 `~/cf-test/.venv/bin/python` 加 `PYTHONPATH=.` 调 backend 函数；routers 等包缺解释器就 skip | 解释器还在但 `import backend` 失败 → 大量失败；如果以后 venv 被删 → **静默 skip** |
| core: Agent 模块 | `backend/agents/go-agent` 的 gofmt/vet/race | 无（静态扫描没发现 Python 调用） | 切换计划保留 `backend/agents/go-agent`、`backend/node-probe`，路径不变 |
| compare: proddiff | Flask 和 Go 都在断网命名空间里加载生产快照副本，比较所有 GET 和生成器输出 | 启动 Flask（`flask_worker.py`）和 Python 生成器（`python_generators.py`），比较器 `run.py` 本身也是 Python | 起不来 |
| compare: difftest | `gate-live.py --difftest`：24 个 `/difftest` 包，非 race | 每个包自己 `python -m backend.app` 启动 Flask | 起不来；而且 `gate-live.py` 预检要求能 `import flask, requests, yaml, webdav3.client` |
| compare: synthetic live | `gate-live.py`：自动发现引用 `CONFIGFLOW_DIFF/PY/REPO/YAML_PYTHON` 的普通测试，带 race，并用 strace 证明 WebDAV 的 Python 预言机真的执行过 | 同上 | 起不来 |
| compare: frontend | vue-tsc + vitest | 无（前端里只有注释提到 backend/*.py） | 不变 |

门禁外但也会受影响的：`tools/configdiff`（自己的 `gate.sh` 包装，约 10 666 个 case）、`tools/routerdiff`（还在迭代，没有完整验收）、
`tools/o10perf --servers python`（性能基准对照，P4 验收完就不需要）、`.github/workflows/checks.yml` 的 backend pytest 分片
（切换后要换成 Go 任务，属于切换计划 #40 的范围，这里只提一句）。

### 1.2 测试分类

「保护什么」用这几个词：**API**＝HTTP 路由、状态码、JSON 字段和中文消息；**存储**＝SQLite 行和数据目录文件的兼容；
**生成**＝生成配置的语义；**设备**＝写到路由器或 Agent 的内容；**格式**＝逐字节对齐。

| 类 | 内容 | 规模 | 依赖方式 | 保护什么 | 处理 |
|---|---|---|---|---|---|
| A | pyref golden：`mosdns`(5)、`oxidns`(8)、`routersplit`(3)、`installer`(1) 的 `testdata/pyref/*.json[.gz]`；`tools/pyref/sources.json` 登记 23 组 | 17 个文件，7.9 MB（其中 mosdns `TestCorpus` 6.8 MB） | 正常运行不启动 Python，但元数据里存了 backend 源码哈希和 venv 依赖哈希，每次运行都会核对 | 生成、格式 | (a) 去指纹 |
| B | 其他已提交的 Python 生成语料：`*_recorded_test.go`、`corpus_test.go`、`*_reference_test.go` 读取的 `testdata/*.json(l).gz`；生成它们的 `testdata/*.py` 有 254 个 | testdata 共 544 个文件 177 MB；其中大部分是 Python 录制或语料 | 回放不需要 Python；少数测试顺带核对 backend 文件的 SHA（例如 `provider/native_reference_test.go` 核对 `backend/utils/provider_delivery.py`），生成脚本 import backend | 生成、API、格式 | (a) 删 SHA 核对，生成脚本归档 |
| C | difftest：24 个包 | 197 个顶层测试（其中 api/agents 29、subscriptions 24、rulelibrary 20、config 19、rules 19、aggregations 13、tasks 11、linuxgateway 10、webdav 9、httpx 8、oxidns 8，其余 13 个包共 27） | 每个包自己启动 Flask（`python -m backend.app`），部分包还用 `python -c "from backend… import …"` 调内部函数；一部分是不需要 Python 的 Go 负控 | API、存储（写入后 DB 行和文件）、生成 | (b) 冻结回放 |
| D | synthetic live 普通测试：15 个包 | 42 个顶层测试 + 2 个 Python 专用子例（`repo/store.TestStoredRoundTrip` 的 python 分支） | `CONFIGFLOW_DIFF=1` 时调用 Python；没设就只跑 Go 部分，或者 skip | 存储（repo 9、store 5、model 2）、Agent 协议（agents、dnsha、ipv6、rulesync）、API（api/agents、api/ipv6）、生成（gateway） | (b)，其中一部分 (c) |
| E | core 里固定解释器的参考调用：routers（R04–R17 边界、routerfake、addresslists、natlist、r07）、clients、api/routers、setup、model、store/raw、repo/reference、auth JWT、notifications、jsonstore、taskstatus、weblistener、agents/metrics、trafficstore、health(+check)、gateway dnsloop、configexport、configgen zip、deploy publish oracle、provider corpus、refresh corpus、tasks history、api/stats、api/clientdevices | 27 个包、58 个文件；routers 一个包约 26 个文件、按名称约 65 个测试 | `exec ~/cf-test/.venv/bin/python -c "from backend… import …"`，或运行 `testdata/*.py` 里 import backend 的 helper；**有一部分缺解释器就 `t.Skip`** | 设备（routers，最重要）、存储、API、生成 | (b)，少数 (c) |
| F | 读 backend/ 文件的同步检查和 fixture | 约 15 个文件：`installer/templates_test.go`（scripts 必须等于 `backend/agents/scripts/*.sh`，外加 Python 指纹）、`repo/crosscheck_test.go`（config_template.json 相同）、`agents/a02_tables_test.go`（version.py）、`api/agents/difftest/diff_test.go`（LATEST_AGENT_VERSION）、`installer/docker_ha_*`（compose/preflight.py 相同）、`installer/deltas_test.go` 和 `gateway_core_test.go`（`backend/testdata/*.sh`）、`oxidns/review_s_test.go`（`backend/schemas/oxidns`） | 读文件，不执行 | 「Go 副本没有跟 Python 漂移」；fixture | (c) 删除同步检查，fixture 搬进 server testdata |
| G | 只针对 PyYAML / Python 库行为的测试 | `yamlx` 3 个（PyYAML 能读 Go 写的 YAML）+ `yamlx/yamlxtest` 语料 28 MB + `tools/yaml*oracle`；`webdav.TestTLSVerificationMatchesPythonDefault`；`installer` 的 Python stub helper（用系统 python3） | 真实 PyYAML 或 webdav3 | 格式、Python 库默认行为 | (c) 或改成纯 Go 断言 |
| H | testgolden / pyref 工具自身 | `testgolden` 13 个测试（含假解释器负控）、`tools/pyref` 8 个、`tools/gatediscover` 4 个、`scripts/test-gate-*.py` | 自测 | 工具正确性 | 跟着改造 |

两个容易漏掉的点：

- **fixture 本身是 Python 造的。** configdiff 用 Python 的备份导入器建库；repo/store/setup 的部分测试用
  `ProfileRepository` 等 Python 类准备数据。这些 fixture 也要冻结，否则删掉 backend 后连准备步骤都做不了。
- **随机 fuzz 对 Flask**（`api/agents/difftest.TestInstallerFuzzAgainstFlask`）没法整体冻结，
  只能录一组固定种子，再加上不依赖 Python 的 Go 性质测试（比如输出必须是安全的 shell）。

## 2. 替代方案

### 2.1 统一机制：testgolden 的「冻结参考」

扩展 `internal/testgolden`，不另起框架：

- **键**：精确输入，也就是归一化后的请求（method、路径、查询、关键请求头、body）加上前置状态摘要。
  动态值只替换成占位符（端口、临时目录、时间、随机 ID），用现有的 `keyInput` 规则：只替换这个 fixture 自己分配的值，不做整树 mask。
- **值**：归一化后的 Go 观察结果：status、比较器原本就比较的响应头、完整 JSON/YAML/文本、写入后的 DB 行
  （meta/sections/fields）和数据目录文件。存 gzip JSON。存储状态记录相对前一步的变化，不每步存全量。
- **录制**（`PYREF_MODE=record PYREF_ACCEPT=1`）：Python 和 Go 都跑，现有比较器必须通过，然后写入 Go 那一侧。
  比较失败就不写（现有的「测试失败不写入」规则）。
- **检查**（`PYREF_MODE=check`）：Python、Go 都跑，再和 golden 比较。只读，用来证明 golden 和当前 Python 一致。
- **回放**（默认）：只跑 Go，和 golden 比较；每条录制都必须被用到（现有规则，防止某个分支悄悄不再比较）。
- **冻结**：删掉 backend 后，元数据里的源码和 venv 哈希不再核对，改成一个 `frozen: python-final` 标记。
  测试 harness 的源码哈希也不再核对，因为冻结以后无法再从 Python 重录，哈希一变测试就永远修不好了。
  保留的约束只有：精确输入作键，以及每条录制都被用到。

每个 difftest 包已经有自己的「调 Flask」函数（例如 `tasks/difftest.python(...)`、各包的 Flask 启动器），
改造方式就是在这一层包一个 `Reference.Call(key, run)`：Go 侧逻辑和比较器原样不动。

### 2.2 各类怎么改

**A 类（pyref golden）→ (a)**
`testgolden` 的 `Fingerprint()`、`cacheGuard()`、`dependencies()`，以及 `sourceMetadata` 里对 backend 的默认值，
改成在冻结状态下跳过；`tools/pyref -fingerprint`、`-test` 包装、`sources.json` 的源码登记和 `live-exceptions-*.json`
改成一个「零例外」审计：任何测试再调用 Python 都失败。golden 文件内容**不用重录**，只要一次性改写元数据。

**B 类（已提交语料）→ (a)**
删掉对 backend 文件 SHA 的核对（provider 的 `PythonSHA`、installer 的 `legacyInstallerFingerprint`、`pythonFingerprint`）。
254 个 `testdata/*.py` 生成脚本已经跑不了了：建议删除，每个 testdata 目录留一行来源说明
（「由 Python <commit> 的 <脚本> 生成，脚本在切换时删除，见 git 历史」）。语料原样保留。

**C 类（difftest 197 个）→ (b)**
按 2.1 包一层。比较器、归一化规则、批准差异的处理都不动。只读不写的负控（不需要 Python 的 Go-only 测试）照常跑。
`TestInstallerFuzzAgainstFlask` 录 200 个固定种子，外加 Go 性质 fuzz。
HEAD 只读、非有限数绑定这类「预期差异」测试本来就断言 Go 的行为，回放时直接和 golden 比。

**D 类（live 42 个）**
- 存储兼容（repo 9、store 5、model 2）：只留 **Python 写 → Go 读** 这一个方向，做法是冻结一组 Python 写出的
  SQLite 和数据目录 fixture（各种历史布局、旧版数据目录、python-written-rows、committed-python-fixture 已经有了）。
  这是切换后**最重要**的一项保护，因为真实用户带着 Python 写的数据升级。
  其余方向见 (c)。
- Agent 协议、API、生成（agents、dnsha、ipv6、rulesync、api/agents、api/ipv6、gateway、server、webdav 之外的）→ 2.1 回放。
- 当前有 2 个 live 测试是红的（`rulesync.TestProjectionAgainstPython`、`gateway/onboarding.TestPythonPreparation`，
  2026-11-20 的结果），必须先修绿才能录。

**E 类（core 参考调用）→ (b)**
大多数是「给输入，调一次 Python 函数，比较输出」，正好对应 `Reference.Call` / `NewLines`，改起来很机械。
routers 是重点：R04–R17 的边界语料（RouterOS/iKuai 计划、写入、回滚、NAT、地址列表、IPv6），保护的是写到设备上的内容，
必须录全；它们的 Python 侧 helper（`routers/testdata/*.py`，共 24 个）在录完后删除。
`setup` / `server` / `gatewayonboarding` 共用的 `owned_digest` 等小函数，各自录一份。

**F 类（同步检查）→ (c)**，fixture 搬家：
`backend/testdata/` 里测试用到的文件（`gateway_install_selective_0ef9e89.sh`、`go_aio_install_0ef9e89.sh`、
`docker_ha_preflight_synology_reconstructed.txt` 等，共 612 KB）搬到对应包的 `testdata/`；
`backend/schemas/oxidns` 如果 oxidns 仍需要 schema，确认 Go 已有内嵌副本，删掉「依赖 backend 文件」的断言。

**G 类 → (c) / 纯 Go**
- PyYAML 读 Go 的 YAML：切换后没有 Python 消费者（Agent、mihomo 都是 Go），删除；YAML 合法性已由 yaml.v3 往返和生成语义测试覆盖。
  `yamlxtest` 的 28 MB PyYAML 语料：读取方向（Go 读 Python 写的 YAML）属于存储兼容，保留成回放；写出方向删除。
- WebDAV TLS：改成纯 Go 断言「默认拒绝自签名证书」，不再需要 strace 证据。
- installer 的 Python stub：用系统 python3 模拟远端主机，不依赖 backend，可以暂时保留；之后改成 shell stub（S，不阻塞切换）。

**H 类**：跟着 WP1 和 WP11 改。

### 2.3 (c) 建议删除的项目和理由

| 项目 | 理由 |
|---|---|
| `repo.TestCrossCheckLockExcludesPythonAndGo`、`store.TestCrossCheckPythonConcurrentCommits`、`repo.TestCrossCheckPythonWriterKilledBeforeCommit` 等 Python 与 Go 同时操作同一个库的测试 | 切换计划规定不在同一 `DATA_DIR` 上同时跑两个后端（A.5「不做的事」、D10）。Go 自己的锁和崩溃一致性由 Go-Go 测试覆盖 |
| `repo.TestCrossCheckInterchange` 和 `model.TestTypesPythonRoundTrip` 里 **Go 写 → Python 读** 的方向 | 不回滚到 Python（基本原则 2026-11-16）。反方向保留成冻结 fixture |
| `yamlx` 的 3 个 PyYAML 读取测试，以及 `tools/yamlwriteroracle` | 没有 Python 消费者 |
| `webdav.TestTLSVerificationMatchesPythonDefault` 的 Python 分支 | 改成纯 Go 断言 |
| F 类全部「Go 副本 == backend 文件」检查 | 删掉 backend 后 Go 副本就是唯一来源 |
| testgolden / pyref 的源码和 venv 指纹、`PYREF_INPUT_FINGERPRINT` | 参考已冻结，不会再变 |
| 只为逐字节对齐、且**没有现成 golden** 的测试（例如 `configgen.TestZIPExternalAttributesMatchPython`） | 2026-11-16 起的功能对齐标准不再追字节；已有 golden 的（installer deflate）保留，成本为零 |

删除前逐条确认没有别的价值；如果某条测试还顺带断言了 Go 自身的行为，就把那部分留成纯 Go 测试。

### 2.4 (d) proddiff / configdiff / routerdiff

**proddiff**
- 切换前（O14 P9）：照常做最后一次 Python vs Go，用刷新后的快照，要求 new=0。
- 切换后改成 **Go 基线二进制 vs Go 候选二进制**：基线是上一次发布的 Go tag（或上一次通过门禁的 `go`）。
  断网命名空间、只读快照、比较器、覆盖文件（`proddiff-coverage.txt` 防止覆盖倒退）、`proddiff-baseline.txt`
  （有意改变时登记理由）都保留。要换掉的只有 Python 一侧：`flask_worker.py` 换成第二个 Go 进程，`python_generators.py` 换成 `generator/main.go`。
- Python 的响应**不提交、也不长期缓存**：里面是生产数据（地址、token、机场域名），只能留在测试服务器的私有目录；
  快照一刷新，旧缓存就没意义了。
- 比较器 `run.py` 暂时还是 Python 脚本（只用标准库和 PyYAML，不需要 Flask 和 backend）。在测试服务器上保留一个
  不依赖 backend 的最小工具 venv（新建 `server/tools/requirements-tools.txt`）。改写成 Go 放到切换以后做，可选。
- `--a13-proof` 依赖真实 Python 准备函数：A14 接线后那 24 条已经消失，切换时删除。

**configdiff**（约 10 666 个 case，Python harness，用 Python 备份导入器建 seed 库）
- 推荐：在切换候选树上最后跑一次 `configdiff all`，把 `testdata/results.json` 和 findings 作为验收证据留在仓库，然后退役。
- 退役前逐条核对 `finding_reviews` 里 closed 的发现：每一条都要有 Go 回归测试（difftest 或普通测试）。没有的补上（WP10）。
- 不推荐的备选：给 configdiff 加「只跑 Go、对比冻结结果」的回放模式（L）。理由是它和 24 个 difftest 包覆盖的路由大量重叠，
  长期维护一套 Python harness 也和「用 Go 原生」的方向冲突。

**routerdiff**：还没有完成验收（默认 gate 返回 2，`--allow-incomplete` 只用于迭代）。直接退役。
设备写入的保护由 E 类 routers 边界冻结回放、loopback fake 测试，以及 PVE 实验室的端到端测试承担。

### 2.5 需要趁 Python 还在时录制的东西

| 录制项 | 命令（WP1 落地后） | 体积估计（gzip） |
|---|---|---|
| difftest 回放（24 包） | `server/scripts/freeze-record.sh difftest`：`CONFIGFLOW_DIFF=1 PYREF_MODE=record PYREF_ACCEPT=1 go test -count=1 -timeout=60m <24 个包>`，环境与 `gate-live.py` 相同 | 25–45 MB（197 个测试，每个约 100–200 KB，存储状态按增量存） |
| live 普通测试（15 包） | `freeze-record.sh live`（同一份环境，`-run` 用 gatediscover 的清单） | 3–8 MB |
| core 参考调用（routers、clients、setup …） | `freeze-record.sh core`：`PYREF_MODE=record PYREF_ACCEPT=1 go test -count=1 ./internal/{routers,clients,api/routers,setup,model,store,repo,…}` | 8–15 MB（routers 占大头） |
| Python 写出的存储 fixture | `go test -run TestFreezePythonFixtures ./internal/repo ./internal/store`（在 record 模式下调用 Python writer 生成合成库） | 2–5 MB |
| configdiff seed 库（只在保留回放时需要） | 推荐方案下不需要 | 0 |
| 已有 pyref（A 类） | 不重录，只改元数据 | 0（现有 7.9 MB） |

合计新增约 **40–70 MB**。仓库已经有 177 MB testdata，可以接受，但要求：一律 gzip；相同响应去重；不放生产数据（只用合成数据，
和现有规则一致）。每次录制完都要再跑一次 `PYREF_MODE=check` 确认没有差异。

录制顺序：先修绿 D 类那 2 个红测试；所有包在同一棵树上录（切换候选树）；录完提交一个独立的 `go(golden): freeze Python references at <commit>` 提交，
打标签 `python-final-goldens`。

## 3. 风险，以及 golden 的更新流程

### 3.1 冻结后哪些保护会变弱

| 风险 | 说明 | 对策 |
|---|---|---|
| 没有独立参考了 | 今后 golden 的期望值只能来自 Go 自己，有意改动和误改都表现为「golden 变了」 | golden 改动必须单独提交，并写明理由；对应登记到 `parity/behaviour-changes.md`；审查时用工具输出可读的差异（哪个请求、哪个字段，旧值 → 新值），不让人去读 gzip |
| 把错误结果录成 golden | 例如录制时 Go 有 bug 而比较器放过了；或者切换后有人重录时顺手接受了回归 | 切换前：只有 Python 和 Go 比较通过才写入（2.1）。切换后：`PYREF_ACCEPT=1` 只能人工使用，门禁和 CI 里设置就直接失败（现有规则）；golden 文件的 diff 必须在 PR 里可见；保留 `python-final-goldens` 标签，随时可以对比当前 golden 和 Python 最终版本的差别 |
| 回放太脆 | 无关的重构改变了请求顺序或输入，导致「没有录制的答案」 | 键只取语义输入，动态值用占位符；有意的流程改动走更新流程；大量无关失败说明键设计有问题，要先修键，不是批量重录 |
| 静默 skip | 缺解释器就 skip 的测试会变成空跑 | core 新增审计：测试源码里出现 Python 进程调用，或者 skip 消息里带 Python/interpreter，都失败（扩展现有 `tools/pyref` 的 AST 审计，例外数为零）；`gatediscover` 改成：任何测试再引用 `CONFIGFLOW_DIFF/PY/REPO/YAML_PYTHON` 就失败 |
| 覆盖倒退 | 删 golden、删测试比修测试省事 | 每个 golden 必须被某个测试读到（现有的「录制必须被消费」规则）；加一个反向检查：每个 `testdata/pyref` 文件都要有测试在用，删测试要同时删 golden，并在提交里说明 |
| 改行为还没停 | O17/O18 还在把模型改成普通结构体、把轮询改成按变化驱动，会让 golden 频繁变化 | 转换工作现在就可以做，但**最终录制放在切换候选树上**；中间阶段录的 golden 允许重录（Python 还在，可以 check） |
| 仓库体积 | 新增约 40–70 MB | gzip + 去重；存储状态存增量 |

### 3.2 golden 更新流程（切换后）

1. 改代码，跑相关包的测试，回放失败并列出差异。
2. 判断差异是不是有意的。不是 → 修代码。是 → 继续。
3. 在 `parity/behaviour-changes.md` 登记（改了什么、为什么、影响哪些路由或设备）。
4. 在测试服务器上执行 `PYREF_MODE=record PYREF_ACCEPT=1 go test -count=1 ./internal/<包>`（切换后 record 只录 Go 一侧），
   再跑一次普通回放确认通过。
5. golden 改动单独提交 `go(golden): <理由>`，提交说明附上差异工具的摘要（请求、字段、旧值 → 新值）。
6. 审查人对照 behaviour-changes.md 逐条确认；涉及设备写入（routers、Agent 推送）的改动按 HR 审查。

切换前（Python 还在）的更新：同样的流程，但第 4 步是 Python 和 Go 都跑、比较通过才写入，之后再跑一次 `check`。

## 4. 工作包

S ≈ 半天到 1 天，M ≈ 2–4 天，L ≈ 1 周以上（一个 agent 的工作量）。

| WP | 内容 | 大小 | 依赖 | 完成时间 |
|---|---|---|---|---|
| WP0 | **运行时盘点**：在测试服务器的 rt 副本里把 backend/ 的 Python 文件挪走（保留 go-agent、node-probe），跑一次全量非 race `go test -json ./...`，用 strace 记录 Python exec；产出准确的失败、skip、exec 清单，替换本文的估计数 | S | 无 | 立即 |
| WP1 | testgolden 冻结模式：record 时 Python/Go 都跑、比较通过才写入 Go 侧；增量存储状态；`frozen` 元数据；可读差异工具；`scripts/freeze-record.sh`（difftest/live/core 三组）；负控（比较失败不写入、未消费录制失败、ACCEPT 在门禁里失败） | M | WP0 | 切换前 4 周 |
| WP2 | A/B 类去指纹：testgolden 指纹和 cacheGuard、pyref `-fingerprint`/`-test`、sources.json 登记、provider/installer 的 SHA 核对；生成脚本删除，留来源说明 | S | WP1（frozen 元数据格式） | 切换前 3 周 |
| WP3 | F 类：删同步检查，把 `backend/testdata` 等 fixture 搬进 server | S | 无 | 切换前 3 周 |
| WP4 | 存储兼容：Python 写出的 fixture 冻结（repo、store、model、setup）；删掉并发互测和 Go→Python 方向；修 D 类 2 个红测试中属于这里的部分 | M | WP1 | 切换前 3 周 |
| WP5 | routers / clients / api/routers 边界冻结回放（E 类重点，设备写入） | L | WP1 | 切换前 2 周 |
| WP6a | difftest：`api/agents`（29）+ `agents/dnsfollow`、`agents/dnsha`、`gateway/installation`、`api/gatewayonboarding`（含 fuzz 固定种子） | L | WP1 | 切换前 2 周 |
| WP6b | difftest：config、subscriptions、aggregations、rules、rulelibrary（95） | L | WP1 | 切换前 2 周 |
| WP6c | difftest：其余 14 个包（httpx、tasks、webdav、linuxgateway、oxidns、discovery、mosdns、refresh、substore、subs、health、provider、mcp、devicenotes，共 66） | M | WP1 | 切换前 2 周 |
| WP7 | D 类其余 live 测试 + E 类非 routers 的一次性参考调用（setup、model、auth、notifications、jsonstore、taskstatus、weblistener、metrics、trafficstore、health、gateway、configexport、deploy、provider、refresh、tasks、api/stats、api/clientdevices）；修 `rulesync.TestProjectionAgainstPython`、`gateway/onboarding.TestPythonPreparation` | M | WP1 | 切换前 2 周 |
| WP8 | G 类：yamlx 写出方向删除、读取方向冻结；WebDAV TLS 改纯 Go；installer stub 改 shell（可延后） | S | WP1 | 切换前 2 周 |
| WP9 | proddiff 改「Go 基线 vs Go 候选」：第二个 Go 进程代替 Flask，Go generator 代替 Python generator，工具 venv 和 `requirements-tools.txt`，删 `--a13-proof`；先在 Python 还在时同时跑两种模式，确认 Go-vs-Go 模式能抓到故意注入的差异 | M | 无 | 切换前 2 周 |
| WP10 | configdiff / routerdiff：在候选树上最后跑一次并归档；核对 configdiff 已关闭的发现都有 Go 回归测试，缺的补上；然后删除工具 | M | WP6 大部分完成 | 切换前 1 周 |
| WP11 | 门禁改造（见下）+ `test-gate-routing.py` 等控制测试；加「隐藏 backend」预演开关 `GATE_NOPY=1`，连续两次批次门禁用这个开关通过 | M | WP2–WP9 | 切换前 1–2 周 |
| WP12 | 文档：AGENT-BRIEF（去掉 venv 和 Python 参考说明）、根目录 AGENTS.md 测试节（删 `scripts/test-backend.sh`）、WORKFLOW（验收节改成冻结参考 + 更新流程）、PORTING、gate.md；O14 清单加本前置条件；CI `checks.yml` 交给 #40 | S | WP11 | 切换前 1 周 |
| WP13 | **最终录制**：在切换候选树上跑 `freeze-record.sh` 三组，再跑 `check`；提交 `go(golden): freeze …`，打标签 `python-final-goldens`；然后在候选树上删掉 backend 的 Python 文件，跑一次完整门禁 | S–M | 全部 | 切换前 1 周内（和 P9 同期） |

可以并行的：WP0 之后，WP3、WP9 不依赖 WP1，马上可以开始；WP1 完成后，WP2、WP4、WP5、WP6a/b/c、WP7、WP8 全部可以并行（按包分开，互不冲突）。
需要串行的：WP11 等转换完成；WP13 最后。

### 4.1 门禁改造后各阶段

| 阶段 | 改造后 |
|---|---|
| core: gofmt/vet/build tags | 不变 |
| core: pyref 指纹 | **删除**（不再导出 `PYREF_INPUT_FINGERPRINT`） |
| core: 无 Python 审计（新增） | `go run ./tools/goldenaudit`（由 `tools/pyref` 改名精简）：测试代码里的 Python 进程调用数必须为零；每个 golden 都有测试在用；`gatediscover` 发现任何 Python 环境变量引用就失败 |
| core: `go test -race ./...` | 原来的 difftest 和 live 测试变成 Go 回放后在这里一起跑（每个 difftest 包都会带 race，WP11 先测一次耗时；必要时 difftest 包单独用 `-p` 限制）。不再需要 `-count=1`，测试缓存可以正常命中 |
| core: Agent 模块、fuzz | 不变 |
| compare: proddiff | 「Go 基线 vs Go 候选」，其余规则不变（new=0、stale=0、覆盖不倒退） |
| compare: difftest、synthetic live | **删除**，连同 `gate-live.py`、`gate-test-exec.sh`、strace 依赖和 `test-gate-live.py` |
| compare: frontend | 不变 |
| 测试服务器 | `~/cf-test/.venv` 退役；只留 proddiff 比较器用的最小工具 venv（不含 Flask、不装 backend 依赖） |

`GATE_ONLY` 的取值去掉 `difftest`；`compare` 的含义变成「proddiff + frontend」。

### 4.2 时间线（以切换日 T 为准）

- **T−4 周**：WP0、WP1、WP3、WP9 开始。
- **T−3 周**：WP2、WP4 完成；WP5–WP8 并行进行。
- **T−2 周**：所有转换完成；WP11 合入；连续两次 `GATE_NOPY=1` 批次门禁通过。
- **T−1 周**：WP10、WP12；O14 P9 刷新快照，做最后一次 Python vs Go proddiff；WP13 最终录制并打标签。
- **T**：Go 合入 main 的同一个提交删除 backend/ 的 Python 源码；门禁已经是无 Python 版本，第一次门禁就应该直接通过。

如果切换日提前，可以先砍掉的：WP8 的 installer stub 改写、WP9 的比较器 Go 化（本来就是切换后的可选项）、WP10 里补测试的部分
（改成切换后补，先把 configdiff 结果归档）。**不能砍的**：WP4（用户数据兼容）、WP5（设备写入）、WP6（API 边界）、WP11 的无 Python 审计。
