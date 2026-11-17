# CFG-11 / CFG-02 / CFG-06 修复交回

分支 `go-fix-cfg-c32`。三张工单分别提交；从 `go@1439e503d` 开始，完整门禁在包含 C33 独立复核的 `go@d43cca6e4` 上通过，之后变基到 `go@a9d6ce86c`，只追加受新 matcher/text 影响的 C32 增量检查。不改比较器、原始 findings 或 baseline，不 push。

## 修复及接口协作

| 工单 | 严重度 | 最终业务提交 | 结果 |
|---|---|---|---|
| CFG-11 / C32 | P2 | `7f693bc4d` | adapter 仅写真实变化的顶层字段；未变行的原始 value、position、revision 保留 |
| CFG-02 / C32 | P2 | `065031306` | 消费交付归一化 warnings，按 Python 原文记录数量及前十个行号，每次交付一条 |
| CFG-06 / C37 | P3 | `ec5e13044` | 单次调用内 targets 按首次出现顺序去重，每个目标只分配一套资源 |

CFG-11 开工前与 O3-6c 作者 `6e5c7760` 对齐：公共 Updater/UpdateProfileTransaction 签名不变；本工单不改 repo/store、全局 revision 或提交语义。保留既有事务和 owner 取消检查，字段比较结束后再次检查事务 ctx；共享最终提交点仍由 O3-6c 负责。N8-5b 作者 `24d62bfc` 确认其 repo 内部 typed 迁移可叠加，不改 C32 adapter 或 legacy 入口合同。未来 N8-8/9 迁移 api/rules 时由迁移方删除 temporary sameJSONValue，同时保留“仅写变化字段”的语义和本工单20个目标向量。

CFG-02 与 CFG-03 作者 `a5567935` 分工：本分支只改 route/交付 owner；物理跨行识别、保留原文和 line0 warning 的产出归 CFG-03。没有复制共享 reader 或 YAML 词法逻辑。名称先经过共享 redact 再 LogText，日志不带内容、token、URL 凭据或伪造换行；脱敏读取失败时以固定占位名继续记录数量。非法 UTF-8 同样保留 Python 的交付 warning。普通 target/正常内容不多记警告，memo 命中也不丢警告，源缓存不变。

CFG-06 按协调者裁定只去重一次调用内的 targets，额外分配空间固定为两个有效目标。不同调用仍和 Python 一样新建库并追加名称后缀。行为台账已登记；C37 PROGRESS 另列跨调用复用的后续方案，需先明确重命名、删除和手工改引用后的有效绑定合同。本轮没有实现该后续项。

## CFG-11 首个误改与真实存储证明

新增 HTTP 回归逐请求读取 SQLite 的 DNS 行原始 value/position/revision，覆盖创建、编辑、策略、重排、删除、rule-set 创建/删除、两个配置空间及不存在 ID。

修复前的第一个误改出现在无 DNS 引用的 DELETE：`other/mosdns` 的首键从 `direct_rulesets` 变为排序后的 `cache_dump_enabled`，解析值不变，position 均4，revision 从1变为10；前面的创建/编辑请求没有触发本例 DNS 行变化。由此确认是删除清理把未变嵌套对象重新编码，而非仅凭最终404推断其写入。修复后，每步未变行的三个属性全部相等；不存在 ID 的404前后完整 store fingerprint 相同；真实 MosDNS 引用清理会修改对应行且推进 revision，其余行保持原样。

adapter 在原锁内工作视图上逐字段提交，不再整配置转回 legacy。只有被 owner 替换的 RawMessage 才进一步作 native JSON 结构比较；数字用 UseNumber 保留 token，布尔/数字、null/空数组、数组顺序、大整数及1/1.0仍区分。此判断局限于 C32，store 的字节差异和 revision 合同不变。

首轮六组 configdiff（实际冻结提交 `ca536ed0f948eaea2f0a6929fc592beb55dae3f8`，.64）：134变体，97 compared、9 different、28 go-unimplemented，design_errors=[]，218.93s。**所有20个目标变体已重跑，DNS revision 差异为0**。该轮在 C33 合入前，未实现计数如实保留。

严格余差逐项核对：

- R180-B01/F08/F09、R184-F08：原已批准的 HTML→JSON 错误壳。
- R179-F08 两端500、R181-F09/R185-F16 两端404，仅 content-type/body 错误壳不同，其他响应及 DB/files 无差异，属于同一个已批准类别。协调者明确同意按该类别处理，不改比较器。
- R184-S02 `dns-cleanup/True`、R189-S01 `cleanup-prepare/True`：**结构相等的布局差异**。真实 Python `yaml.safe_load` 两侧 native_config 后相等；解析结果以排序 JSON 编码的 SHA256，两侧分别同为 `9df49348e70f28008c5e4c26bb01a758726be7f96de0bf53720fcbfebcaf8b4f`、`c94d1f9cab546d1401ef932702f72d8ef499c407a6852394c2ba852c6ca75265`。仅 N7-4 序列缩进变化，不屏蔽字段 revision。

## CFG-02 / CFG-03 联合边界验收

本分支单独的22向量复验：20 compared、2 H133 different、design_errors=[]，92.53s。两条 mixed finding 当时仍同时有原文和 warning 差异，没有提前宣布关闭。

临时 detached worktree 以 CFG-03 `d8e6cacffb3efb0e6a1d492c16a9c139c3373c28` 为基底，仅 cherry-pick 原 CFG-02 `9fe6a8512817231ba066dc3014cb8bfe7806cf47`，得到联合树 `59238b255549e04d82b0249f118331027113a515`。该树完整复验 **22 compared、0 different、0 open、design_errors=[]，135.88s，exit0**。H133 两个前缀都逐字保留原文、各记录一次 line0 warning，且 DB/files 严格比较。联合提交没有进入工单分支；临时 worktree 及远端源码副本已清理，日志留存。

联合运行的确切命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -C /private/tmp/configflow-cfg02-03-joint -e CFG_FIX_BASELINE=59238b255549e04d82b0249f118331027113a515 -- bash -lc 'mkdir -p /home/sunbroke/.cache/rt-tmp/cfg-c32-cfg0203-joint && server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-delivery-yaml-helpers --baseline "$CFG_FIX_BASELINE" --output /home/sunbroke/.cache/rt-tmp/cfg-c32-cfg0203-joint/results.json --findings /home/sunbroke/.cache/rt-tmp/cfg-c32-cfg0203-joint/findings.md > /home/sunbroke/.cache/rt-tmp/cfg-c32-cfg0203-joint/console.log 2>&1; result=$?; tail -7 /home/sunbroke/.cache/rt-tmp/cfg-c32-cfg0203-joint/console.log; exit "$result"'
```

CFG-03 最终重放提交为 `47881419c`（作者确认产品代码与实际联合验证使用的 `d8e6cacff` 相同，重放到 `go@d43cca6e4`）。本分支没有 CFG-03，合并时必须保留其 producer 修复；不能仅凭消费方提交关闭 H133。

## CFG-06 与 Python 的明确差异

`TestCFG06DuplicateTargetDifference` 通过真实 Flask/Go HTTP，分别对 direct 重复和非相邻混合重复目标执行两次调用。显式断言 Python 每轮按原始数组分配孤立库项/重复引用，Go 按首见目标分配，无孤立库项；两侧引用都在 MATCH 前，缓存数按唯一目标增长且不产生备份。第二次调用仍分配新 ID。另有 API 回归检查输入验证拒绝无写入、空 targets 默认顺序、资源名后缀、文件内容和每个引用的库/策略关系。

原始 `discovery-init` 复验（冻结 `f256be7ca25214732e1245fc405444b0fdf816b1`，.64）：9变体，8 compared、1 different，design_errors=[]，80.52s。唯一不同是批准修正的 `R056-S01 / duplicate-targets/0`：Python 新增库/引用数为2/2，Go为1/1。原 observer 在数量不同时不建立 ID 配对，因此余下21条严格差异包括派生 ID、名称后缀、数组位置和缓存路径；不把它们冒充21个独立业务问题，也不吞掉原始比较证据。新差异断言验证每一侧的正确预期；普通8条，包括跨调用两次新建，全部严格一致。

## 针对性检查与已知测试耗时

均在 .64 运行：

- CFG-11：`go vet ./internal/api/rules/... && go test -race -count=1 ./internal/api/rules/...` → API128.206s、difftest1.188s PASS（此处未启用 live）。
- CFG-02 新增 warning 回归：`go test -race -count=1 -run '^TestLocalDelivery' ./internal/api/rules` → 7.931s PASS，vet通过。
- C37：`CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/discovery/... ./internal/api/domaindiscovery/...` → live33.234s、API11.712s通过；core 在既有 N7-3d 归档迁移测试上撞默认10分钟，见下。
- 固定容量去重后的 API 回归12.965s PASS。
- C33 合入后的变基增量：gofmt/vet通过；C32 SQL/type/warning/关机与断连回归70.394s、C37 API10.499s、真实 Flask 差异断言11.717s，均 PASS。

变基增量确切命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/rules/read_adapter.go -- bash -lc 'cd server && gofmt -w internal/api/rules/read_adapter.go && go vet ./internal/api/rules/... ./internal/discovery/... ./internal/api/domaindiscovery/... && go test -race -count=1 -timeout=30m -run "^(TestRuleWritesPreserveUntouchedDNSRows|TestRuleDeleteChangesOnlyReferencedDNSRow|TestFieldComparisonKeepsJSONTypesAndNumbers|TestLocalDelivery.*|TestReviewWriteCancellation|TestInitTargetsDeduplicateInFirstOccurrenceOrder)$" ./internal/api/rules ./internal/api/domaindiscovery && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=30m -run "^TestCFG06DuplicateTargetDifference$" ./internal/discovery/difftest'
```

既有 core 测试超时开头：

```text
panic: test timed out after 10m0s
    running tests:
        TestN73CustomPortReaderMigrationGate (9m47s)
goroutine 353 [running]:
    goroutine running on other thread; stack unavailable
FAIL configflow/internal/discovery 600.369s
```

该测试首次解码21343条冻结语料；当时 .64 load46.56/38.56/31.96、RSS约1.2GB。协调者确认与 CFG-06 无关，耗时交 N7-3d 作者另行处理；手动整包 race 应使用30m，gate已有60m。已按其明确要求停止刚开始的 .32 重复验证，没有缩减语料或改 timeout/test。此处不宣称 core 整包 default-timeout race 全绿。

## 最终定向与门禁

最终八组源码冻结在 `37ecb8229`，该点含本轮三张工单和 C33 修复；随后仅给 CFG-11 callback 增加末尾 tx.Err 检查并 squash 回对应工单，`TestReviewWriteCancellation` 再次 race20.696s通过。门禁时业务 tip `c85db0fec`，最终变基后业务 tip `ec5e13044`；三张工单仍各自一个提交。

八组最终复验：165变体，150 compared、14 different、1 go-unimplemented，design_errors=[]，353.13s；原始 runner exit1，未改其判定。开始load26.64/36.19/34.50，结束17.69/25.60/30.49。20个CFG-11目标依然无DNS revision差异；14条严格差异分解为7条批准错误壳、2条结构相等的布局差异、2条待同时合入CFG-03的H133、1条批准CFG-06、2条C33备份差异。

**P2，新观察、已分派给 `go-fix-c33-bak`**：C33合入后新接通的T25 `library/operation-2`、`library/operation-3` 都是200；Python保留14-byte/mode0600的by-id `.list.bak`，Go没有，响应、DB和主缓存其余相同。协调者已交C33复核者 `2a90a68e` 核对修复。本分支不改C33，不批准该差异；它与本票T25 `rules/operation-5` 的DNS revision回归分开记录。

八组确切命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e CFG_FIX_BASELINE=37ecb8229 -- bash -lc 'mkdir -p /home/sunbroke/.cache/rt-tmp/cfg-c32-final-targeted && cat /proc/loadavg && server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rules-sets,profile-rule-group-write-selectors,rule-dns-selectors-types,rule-dns-native-locked,rules-update,writes-real-online-Agent-no-push-or-activate,rule-delivery-yaml-helpers,discovery-init --baseline "$CFG_FIX_BASELINE" --output /home/sunbroke/.cache/rt-tmp/cfg-c32-final-targeted/results.json --findings /home/sunbroke/.cache/rt-tmp/cfg-c32-final-targeted/findings.md > /home/sunbroke/.cache/rt-tmp/cfg-c32-final-targeted/console.log 2>&1; result=$?; tail -8 /home/sunbroke/.cache/rt-tmp/cfg-c32-final-targeted/console.log; cat /proc/loadavg; exit "$result"'
```

末尾摘要：`current={compared:150,different:14,go-unimplemented:1}`、`request_variants=165`、`design_errors=[]`、`seconds=353.13`，末行load `17.69 25.60 30.49`。所有类型仍保留原始比较结果；本分支自身没有CFG-03，但独立联合树的22/22证明两个修复组合后H133归零。

最终 gate：.64 前台session93152，先做末尾ctx检查后的取消回归，再执行 `server/scripts/gate.sh -p 6`。约20秒排队后拿到slot1/1，未触及15分钟上限；开始load16.81/24.23/29.79。完整门禁 `GATE PASS`、exit0：gofmt、全仓vet、双tag构建/检查、observer自检、98个包全量race、16个live difftest全部通过。proddiff `new=0 stale=0`、self-check PASS。结束load21.89/21.41/21.82；race观察最高59.07/33.77/28.70，race结束附近28.53/20.55/21.65。

本轮主要race耗时：C32 API56.838s、C37 API7.094s、C37 core586.669s，agents434.164s、installer1621.042s、ippresence1293.373s、mosdns978.700s、oxidns1583.189s、provider1743.696s、routers1779.465s、routersplit1285.661s、yamlxtest1161.295s。均在脚本原有上限内，没有缩减语料或修改gate超时。live difftest：C32 94.522s、C37 11.313s、C33 41.653s，其余13包也全部通过。

门禁最后输出：

```text
ok  configflow/internal/api/rules/difftest 94.522s
ok  configflow/internal/discovery/difftest 11.313s
ok  configflow/internal/webdav/difftest 32.335s
08:51:22 up 15:09,  7 users,  load average: 21.89, 21.41, 21.82
GATE PASS
21.89 21.41 21.82 15/664 4045153
```

完整门禁日志：`.64 /home/sunbroke/.cache/rt-tmp/cfg-c32-final-gate/`（race.log、diff.log、proddiff.log及tag/observer日志）。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GATE_LOGS=/home/sunbroke/.cache/rt-tmp/cfg-c32-final-gate -- bash -lc 'set -e; cd server; go test -race -count=1 -timeout=30m -run "^TestReviewWriteCancellation$" ./internal/api/rules; cd ..; date -Ins; cat /proc/loadavg; server/scripts/gate.sh -p 6; cat /proc/loadavg'
```

远端证据均在 .64 的 `/home/sunbroke/.cache/rt-tmp/`：`cfg-c32-cfg11`、`cfg-c32-cfg02`、`cfg-c32-cfg0203-joint`、`cfg-c32-cfg06`、`cfg-c32-final-targeted`。仅合成夹具；报告未抄入快照或凭据。


## 最后变基后的增量

最终 `git rebase go` 到 `a9d6ce86c`。相对已通过完整门禁的基底，该树增加 N7-3d 复核的 matcher/text 改动；规则写入adapter、discovery、repo、rulefetch没有产品接口变化。本分支的字段patch、warning、targets去重语义保持。按新 WORKFLOW 批次合并规则，已完成的完整门禁保留其确切验收树，最终合并由协调者选入batch。

变基后增量命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cat /proc/loadavg; cd server && gofmt -l internal/api/rules internal/discovery internal/api/domaindiscovery && go vet ./internal/api/rules/... && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=30m ./internal/api/rules/...; result=$?; cat /proc/loadavg; exit "$result"'
```

增量结果：gofmt无输出、vet通过，C32 API63.206s / enabled live difftest160.419s均PASS，exit0；load16.38/20.43/21.48→17.46/18.10/20.35。最后输出：

```text
ok  configflow/internal/api/rules 63.206s
ok  configflow/internal/api/rules/difftest 160.419s
17.46 18.10 20.35 11/558 4104458
```

新流程 `go@215ab6b04` 到达时，本轮完整gate已经结束并通过，最后增量也已完成；没有再启动整轮。仅AGENT-BRIEF变化，最终再变基到215ab6b04，产品代码与增量验收点相同。按协调者安排三张票进入batch-2，后续完整门禁由协调者在批次树运行。最终文档tip见交回消息；代码无后续变更，提交后确认工作区干净，不push。
