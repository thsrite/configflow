# A24 对抗式复核

状态：复核进行中，两个发现已修复，最终门禁待完成。独立复核者未编写 A24 原实现；
分支 `go-review-a24`，起点 `go-port-a24@642e84db`。无子 agent、无 push、无生产访问。

## 发现

- **F1，高，已修复：公共 Agent/部署元数据仍可泄露凭据。** 原 helper 仅排除顶层
  token 和三个私有历史字段，部署/升级序列化也没有通用凭据字段防线。有效的 Agent
  host 可包含 Basic-auth userinfo；旧/导入记录的 registration key、密码、private key、
  API key，HA/部署 message 的 `password=...`，以及 token 在升级其他文本中的引用会透传。
  全局 JSON hook 只知道仓库登记的 token，不能保护这些密码或注册密钥。
  Python 原版也有此漏洞，安全修正登记在 `behaviour-changes.md`，未放宽差分比较。
  现在公共 Agent 和共享 `deploy.PublicDeployment` 调用共享 `redact.SanitizeCredentials`；
  后者复用已有树遍历、`diag.IsCredentialKey/CredentialSpans/HasCredentialText/FailureText` 和
  URL 编码 token 识别，收集旧凭据后再次遮盖其他字段中的引用。没有复制业务算法，
  没有改变全局 token hook 的策略。原记录不改，普通未来字段保留。
  补充回归确认无字段标签的环境 registration key 也会泄露，现将
  `AGENT_REGISTRATION_KEY` 纳入共享已知密钥快照；该项同样先复现失败，再修复。
  - 回归：`TestReviewA24PublicCredentials`（7 组，含裸 Bearer/Basic 和 PEM）、
    `TestReviewA24PublicHTTPDoesNotExposeLegacyCredentials`（两个真实 HTTP 出口）、
    `redact.TestPublicMetadataCredentials`（跨字段/编码引用、不变性、幂等、全局策略隔离）。
- **F2，高，已修复：Runtime 关机取消后仍能提交开关/清理。** `Detach` 保留了关机
  parent，但快速 owner 路径没有先检查 `ctx.Err()`；清理的叶子 store API 无 context，
  repo 的未竞争锁也不保证已取消 context 必定拒绝，因此切换成功、文件被删除。
  六个 A24 写入口 detach 后先检查关机取消，直接调用的 manager.SetDomainDiscovery 同样检查。
  handler 继续传原 context，没有 handler 级 WithoutCancel。
  - 回归：`TestReviewA24ShutdownRejectsWrites` 使用真正 Runtime.Start/BindRequest/Shutdown，
    对备注、策略、domain/IP 规则、开关、清理比较完整 DB/JSON/YAML 状态，并检查取消错误。
    既有 `TestClientNoteAndPolicyCancelledOwners` 继续证明请求断开后六类写入均可完成。

修复前在 **192.168.0.64** 运行新增回归，确切命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -count=1 ./internal/api/agents -run "TestReviewA24"'
```

```text
--- FAIL: TestReviewA24ShutdownRejectsWrites/toggle
    shutdown result: <nil>
    shutdown changed repository or discovery files
--- FAIL: TestReviewA24ShutdownRejectsWrites/clear
    shutdown result: <nil>
    shutdown changed repository or discovery files
--- FAIL: TestReviewA24PublicCredentials
    credentials escaped: {"id":"legacy","host":"operator:secret-password@192.0.2.9"}
    ... registration_key/password/private_key, upgrade token, deployment and HA messages also leaked
FAIL configflow/internal/api/agents
```

策略用例同轮返回 revision 冲突而非取消；修复后六个用例一致返回 context.Canceled，零写入。

## 已审计范围

- 已读 AGENT-BRIEF、PORTING 顶部/4b、WORKFLOW、O3 2.5 和全部 Clarified 段、
  A24 handoff、PROGRESS A24/A13/C37、review-c31、review-a11。
  初始分支没有 `review-a12.md`；rebase 到 `go@ae2e0f59` 后已补读该报告全文和新回归。
  A12 的反斜杠 URL 凭据识别修复自动由本复核的共享调用采用，未复制旧正则。
- 完整逐分支对照 `backend/routes/client_tools.py`，agents.py 的两个 discovery 和
  device-policies handler、`_public_agent/_public_deployment`，manager.set_domain_discovery；
  对照 shared device notes、connection rules、discovery 的 parse/apply/guard/query 调用链。
  8/8 路由均有真实 Flask 成功/失败/鉴权差分；每次响应后比较完整 DB 和 JSON/YAML 文件。
- 管理员鉴权先执行；Agent bearer 无效；Agent 绑定 profile 生效，查询参数中的 global UI
  选择即便不存在也不影响绑定。note previous_name、policy settings/notes revision 和绑定
  在同一事务内再次检查；两编辑者一成功、一冲突；纯改名保持 OxiDNS/native draft 和 settings revision。
- `publicAgent` 原 10 组 Python 参照扩到 **24 组**：HA 状态/字段、成功和失败部署、升级
  回滚、空 HA、字段缺失、null、非对象旧值、非字符串旧名称和未来字段；所有普通字段逐值比较，
  输入记录不变。安全修正单独断言，不改 oracle、不忽略敏感语义字段。
- C37 QueryRuleset 复用 info/reference/eligibility；BeforeWrite 在 shared transaction 内，
  library mutation 之前运行；拒绝时完整库/历史/cache 路径/字节/mtime 不变；通过与不带
  守卫的 response、库和历史相等。既有 C37 测试与 difftest 纳入最终门禁。
- A11 共享 PublicDeployment 非空 HA 才聚合；空 HA 保留 status/message/success/pending/
  http_status。A11/deploy、A12/pushdiff 测试、真实 Python 参照和 bundle proddiff 一同复验。
- 没有新增依赖、Python 模拟、后台 goroutine 或私有业务 helper 副本。JSONx 仅用于既有
  registry/repo/redact adapter；解析/规则校验/持久化/脱敏均复用共享 owner。
- 无用户功能/操作入口变化，不需新增 doc 截图。已登记唯一的 Python 功能差异是凭据安全修正。

## 最终门禁

迭代在两台测试服务器经 rt 执行；最终完整排队门禁固定 **192.168.0.64**，`-p 4`。
初始 `go@d3124353` 的定向 race 已通过：
API 138.472s、agents difftest 230.503s、registry 208.796s、discovery 10.852s、
discovery difftest 10.346s、deploy 24.984s、pushdiff 48.707s、redact 1.021s。
原独立全量非 race 日志也全绿，但旧批次因协调者合入 A24 原版、A12 复核和 N8 更新停止，
不把中断的 rt/SSH 连接计作最终门禁通过。新 AGENT-BRIEF 指定共享 gate.sh，
其中全仓 race 已执行全部测试，不再另跑全仓非 race。

另在 .64 直接调用真实 Python `_public_agent` 的四组独立参照确认 F1 为原版漏洞：

```text
{"id":"legacy","host":"operator:synthetic-password@192.0.2.9","registration_key":"synthetic-registration","password":"synthetic-password"}
{"id":"ha","oxidns_ha_publication":{"status":"failed","message":"password=synthetic-ha"}}
{"id":"upgrade","latest_upgrade":{"status":"failed","message":"old synthetic-token"}}
{"id":"deployment","latest_deployment":{"status":"ready","api_key":"synthetic-api","message":"password=synthetic-diagnostic"}}
```

旧版 `.32` 门禁在 loadavg 55（随后协调者观测到 130）时出现 aggregation provider
三条假差异；同一代码在 `.64` 隔离重跑已是 new=0/stale=0。按协调者新的全量排队规定，
停止未完成的 `.32` 全仓 race，不计作通过，也未修改任何 baseline。
此前 `.64` 的 12 个 enabled difftest、API Python 参照和 8×60 秒 fuzz 已全部通过。

最终完整门禁使用 `go@8c5d2835` + 复核实现 `3930e27d`，采用新增槽位限制的共享脚本：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -e GATE_LOGS=/tmp/review-a24-gate-queued -- bash -lc 'set -o pipefail; server/scripts/gate.sh -p 4 FuzzClientConnectionEntries:./internal/api/agents FuzzConvertMosDNSRules:./internal/deploy FuzzSourceExcerpt:./internal/deploy FuzzOpenArchive:./internal/deploy FuzzConfig:./internal/deploy/pushdiff FuzzRuleEntries:./internal/deploy/pushdiff FuzzNodeItems:./internal/deploy/pushdiff FuzzCredentialSpans:./internal/deploy/pushdiff 2>&1 | tee /tmp/review-a24-gate-queued-output.log; result=${PIPESTATUS[0]}; printf "%s\n" "$result" > /tmp/review-a24-gate-queued/exit-code; exit "$result"'
```

该脚本还覆盖 configflow_proddiff / configflow_difftest 两个 build tag 的全仓构建、vet 和
lifecycle 测试。当前已完成 gofmt/vet/tags 和 proddiff（new=0/stale=0）；全仓 race 仍在跑，
最终结果待补充。

环境注册密钥的最后一项补强只改共享 redactor 的已知密钥快照，没有改变任何 fuzz 解析目标。
先失败的回归 `legacy_error=old synthetic-env-registration` 已修复；`.32` 共享 redactor race
和 A24/公开 Agent 真实 Python 补验先通过（1.035s / 9.936s）。三个消费者包的最后补验：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=2 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/redact/redact.go internal/redact/credentials_test.go)"; go vet ./internal/redact ./internal/api/agents ./internal/deploy; CONFIGFLOW_DIFF=1 go test -race -p=2 -count=1 -timeout=30m ./internal/redact ./internal/api/agents ./internal/deploy'
```

该补验当前 redactor 1.067s 已通过，API/deploy 等待完成。
