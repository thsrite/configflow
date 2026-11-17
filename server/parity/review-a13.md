# A13 对抗式复核

独立 Codex 复核，非原作者；分支 `go-review-a13`，起点 `go-port-a13@0de43b03`。
范围：content sync Checker、缓存与失效广播、四个 GET 路由；非 HR。
无子 agent、无 push/PR、无生产访问，测试经 rt 在测试服务器执行。

状态：**复核中**。已 rebase 到 `go@8d716ceb`（A24 复核、N8-6、R08、provider 负载修复）。
F1/F3 已修复并经 race/HTTP 回归；F2 元数据兜底已修复，环境 finder 快照待正式整理。
临时 `go-review-a13-a24-prep` 上的预验已完成，结束前清理其 worktree/branch 与 rt 副本。

## 发现

- **F1，中，已修复：真实 Spawn 丢弃 owner 取消，关闭后任务继续准备。**
  `Checker.Close` 取消 reservation context，但 `Runtime.Spawn` 明确使用 WithoutCancel，
  回调收到的任务 context 不再由该 reservation 取消。正在准备的任务继续 I/O，
  已准入但尚未开始的任务甚至在 Manager 关闭后才调用 Prepare。
  原延迟替身直接转交 context，掩盖生产差异；cache/busy 清理成功不等于任务真正停止。
  回调现将 reservation 的取消重新连接到 Runtime 提供的 task context，保留新 scope、
  ClockFrom 和 shutdown cancellation；启动前已取消时同步拒绝准备。
  `TestReviewCloseCancelsRealRuntimeWorkers` 使用真实 Runtime，分别覆盖运行中及启动前关闭，
  同时断言 WaitIdle、槽位、Keyed reservation、状态及详情全部清理。
  修复前两场景均失败；修复后 contentsync race 通过。
- **F2，高，待修复：环境注册密钥在 diff/entries 及搜索中泄露。**
  `StoredSecretFinder` 只读取仓库 tokens，遗漏 `AGENT_REGISTRATION_KEY`。
  明文、URL 编码及跨 CJK 的局部文本能从 push-diff、entries 返回并被 q 命中。
  真实 Python 同样复现，属于安全修正；协调者已确认在共享 finder 纳入环境密钥，
  保持全局 JSON hook 和用户可 reveal 的配置凭据策略不变。
  更强回归发现仅更新 finder 仍不足：配置 `hosts` 字典键、row.key/path、文件路径等
  不走显示文本转换，原来依赖全局 token hook 兜底。已在 A12 唯一的 mapTexts 遍历器
  加入使用同一 finder 的元数据兜底，不新增私有算法；显示文本仍局部替换，其余字符串
  和字典键含已知密钥则整项遮盖。排序确保遮盖后键冲突顺序确定。
  `TestReviewStoredCredentialMetadataFallback` 先失败，随后 pushdiff race 通过（60.471s）；
  普通上下文、用户可 reveal 的 password、隐藏视图和原对象不变均有断言。
- **F3，高，已修复：A24 publicAgent 投影顺序丢失密钥知识，跨字段引用泄露。**
  `deploy.PublicDeployment` 先把 history 中的 api_key 清成 `[REDACTED]`，随后
  外层 SanitizeCredentials 才收集字段，已经看不到原 key。顶层 journal 和 HA.message
  引用该密钥时均透传。隔离的 detached checkout `c106af0d` 两组回归经 rt 均先失败，
  真实 Python 也确认同类泄露。协调者批准新增共享 CollectCredentials(raw, seeds)，
  复用 sanitizer 唯一 visit/depth/cycle 限制；publicAgent 投影前收集，最后对公共记录
  统一清洗，保持 deploy.PublicDeployment 局部处理不变。合并树再次复现后修复。
  HTTP 回归另发现 summary=1/history=0 在调用 helper 之前已去掉历史，故还需保留更早的快照。
  history=0 先从原 record Collect，再投影；summary 使用 repo.GetAgentsInspect / Manager 透传，
  在同一 read view 中、轻读投影前 Collect。callback 不返回/保留原历史；collector 使用唯一
  visitor 的 collect-only 模式，不构造脱敏历史副本。runtime overlay/online 逻辑未变。
  所有公共 Agent 输出仍走唯一 publicAgentWithCredentials projector；A24 的 SetDomainDiscovery
  返回完整记录，原 publicAgent wrapper 可直接 Collect，其他 client-tools DTO 不走轻读 publicAgent。
  四种读取路径均有 journal→旧 deployment.api_key 的真实 HTTP 回归；另验 HA 引用、原记录不变、
  四类来源、嵌套 credential 对象、cycle/depth，以及错误和 panic 响应/应用日志不含密钥。
  repo 并发提交回归证明 seed 与返回 light record 同代、inspection 不阻塞 writer；Manager 回归
  证明 live overlay 与在线状态不变。**N8-5 迁移时需保留此 inspection 能力**；O3-6a 若先合入，
  保留其锁结构后把 callback 接回相同 read view。已按协调者要求独立提交。

## 补充检查

完整逐分支对照 `backend/agents/content_sync.py`、agents.py 四条 GET 路由、
Go contentsync 全部实现、content adapter、composition wiring、A24 publicAgent 和共享 redactor。
已读 AGENT-BRIEF/PORTING/WORKFLOW、A14 设计 A13/Clarified、O3 全文及各个 Clarified、
PROGRESS A13、review-a12，以及 A24 分支上的 review-a24；合入后会再核对共享源码。

| 规则 | 复核证据 |
|---|---|
| precondition：pending 优先、无基线不猜测、profile/service/version/path/fingerprint | ContentView/input 与 Python 每个提前返回对照；既有 precondition 单测及真实 Python histories |
| 输入键 | 恰好移除七个顶层元数据；profile_name、嵌套同名字段、完整 applied_content 和 base_url 均保留；preview 的不同策略共用 projection |
| 900/30 秒及详情短重试 | 假钟严格检查到期前/等于到期，失败详情不覆盖 changed 状态，返回版本稳定且重建后替换 |
| 512 状态、8 详情、48 MiB | 状态 LRU 跳过 running，详情按插入先后淘汰；过期详情清理、单份最新详情可独自超限的例外与 Python 相同 |
| 并发与取消 | entry 对象身份丢弃旧 worker（包括 A→B→A）；generation 阻止同步比较失效后回填；拒绝/panic/shutdown/Close 释放 slot/Keyed 一次 |
| 分页 | expired 优先于 changed，changed 优先于 replaced；缺 file/group 为404；entries 不触发准备，锁外读取不可变 FileEntries |
| 路由 | 既有 endpoint 的 GET 分支、四路管理员鉴权、summary/history 轻读、profile 绑定、原发布 origin、不存私有 baseline、no-store 与分页验证顺序 |
| 共享与隔离 | 一个 Manager 一个 Checker，替换/关闭注销；Sub-Store 广播给全部 live Checker；注销等待正在执行的广播 |

Python 的 repository 对象切换由 Go 的 Manager/Checker 生命周期承接；typed immutable Source
和 Target 快照承接 deepcopy。没有新增 Python 模拟或私抄 A12/A24 脱敏/比较逻辑，无新增依赖。
Checker 的读取只使用 manifest/Files，retarget 和 detail Build 不访问 Archive/SHA256，不触发归档压缩。
正常 YAML/JSON 显示排版继续沿用 A12 已接受的 Go 原生差异；本复核未添加差分忽略规则。

`TestReviewProductionCacheSpec` 直接钉住 New(Options{}) 的生产 spec：2 worker、900/30 秒、
512 状态、8 详情、48 MiB；未把这些默认值替换成缩小的测试参数。
实际创建 514 个状态和 9 份详情，并检查 48 MiB 包含边界及超限后最旧详情淘汰。
`TestReviewUnregisterWaitsForBroadcast` 在真实重叠的广播与注销间检查同步和后续隔离。

## 当前命令与证据

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -count=1 -run TestReview ./internal/deploy/contentsync'
```

修复前：

```text
before-start=false: Close did not cancel real Runtime preparation
before-start=false: closed Checker left Runtime busy
before-start=true: closed Checker left Runtime busy
before-start=true: closed Checker started preparation after admission
FAIL configflow/internal/deploy/contentsync
```

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/deploy/contentsync/review_test.go -- bash -lc 'cd server && gofmt -w internal/deploy/contentsync/review_test.go && go test -race -count=1 ./internal/deploy/contentsync'
```

```text
ok configflow/internal/deploy/contentsync 5.240s
```

后续追加生产边界/并发广播后，同包 race：`ok configflow/internal/deploy/contentsync 2.531s`。
元数据兜底：`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -race -count=1 ./internal/deploy/pushdiff'`
→ `ok configflow/internal/deploy/pushdiff 60.471s`。
F2 真实 Python oracle 由 `testdata/review_a13_reference.py` 调用完整准备与 Flask 路由：
`1 passed in 1.56s`，固定输出 `Python reference: registration key plaintext/encoded exposed in diff, entries and search; confirmed`。
Go HTTP 相反断言先失败，确认明文和编码形式都能泄露/搜索。
原版因果证明复验：`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'server/scripts/proddiff.sh --a13-proof'`
→ `A13 real preparation: 21 HTTP cases, 3 actual bundles; all 20 deferred fields reproduced without preparation and resolved with it; PASS`。
基线退出路径测试直接调用 proddiff.main，保留真实 baseline/report/exit 流程，隔离进程/网络边界：
matching=0、stale-only=1、new-only=1、explicit-record=0；验证 summary.new/stale 和实际 baseline 更新。
另验 A13 baseline 只能是获批的精确 20 条，或 A14 接线后全部移除，无通配符。
`PYTHONPATH=server/tools/proddiff ~/cf-test/.venv/bin/python -m unittest discover -s server/tools/proddiff -p test_proddiff.py`
经 rt .64：`Ran 36 tests in 0.040s — OK`。未改任何生产 comparator、归一化或 baseline 行。
原 Python 6 个相关测试文件完整执行：`101 passed in 12.31s`；新增三组安全参照 `3 passed in 1.38s`。

临时 A24 基线上的预验（正式合并树仍需复验）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go vet ./internal/redact ./internal/api/agents && CONFIGFLOW_DIFF=1 go test -race -count=1 -run "TestReviewCollect|TestReviewA13Projection|TestReviewA24Public|TestPublicAgentPython|TestPublicMetadata" ./internal/redact ./internal/api/agents'
```

redactor 1.043s / API 2.439s；再加 raw collector 的日志/错误回归后，redactor 全包 race 1.025s、
API 对应回归和原真实 Python 边界 race 2.359s。新 `TestReviewRegistrationFinderSnapshot`
先复现三种编码、Unicode 局部区间及环境快照遗漏，补 finder 后 redactor 全包 race 1.163s；
同时明确断言全局 JSON hook 的 ExternalPayloadSecrets 策略未改变。

正式合并树 F3 命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go vet ./internal/redact ./internal/api/agents ./internal/agents ./internal/repo && CONFIGFLOW_DIFF=1 go test -race -count=1 -run "TestReviewCollect|TestReviewAgentInspection|TestReviewInspection|TestReviewA13Projection|TestReviewA13PublicReads|TestReviewA13CollectedSecrets|TestReviewA24Public|TestPublicAgentPython|TestPublicMetadata" ./internal/redact ./internal/api/agents ./internal/agents ./internal/repo'
```

redact 1.066s、API 8.507s、Manager 3.839s；初始 repo fixture 缺 profile binding，补上正确的
默认 profile 后 repo 1.235s，再加入并发提交回归后 1.543s。无 API 或 redactor 未决失败。

剩余：整理 F2 环境 finder、完成共享脱敏/分页与 20 条 baseline 因果证明复验，
`git rebase go` 后运行 `server/scripts/gate.sh -p 6` 和相关 fuzz；零未决后改 verified。
