# C36 独立复核

结论：C36 自有实现通过复核；high 0、medium 1（已修复）、low 0，未解决发现 0。
本分支不宣称尚缺的 A21/A22/router 外部 owner 已完成正式接线。

- 复核分支：`go-review-c36`，基底 `go-port-c36@736e52827`，依赖 C34 `9a5929ee2`、go `fbbe382ec`。
- 复核者不是作者；未开子 agent、未 push、未触碰生产、未跑完整门禁。
- 读过 AGENT-BRIEF、WORKFLOW 最后三节、两份 C36 交接、PROGRESS/FILES 对应规格，
  以及 Python 五个完整源文件：setup_flow、setup_presets、setup_completion、setup_abandon、routes/setup。
- 只用合成数据；检查固定在 `RT_HOST=sunbroke@192.168.0.32`，按 Leaner verification 抽查，未重复作者整套测试或 fuzz。

## M1 / medium / 已修复：投影之外的元数据和公开错误泄漏私有会话凭据

场景：私有 `router_draft.password` 在默认配置名称、共享来源名称或放弃计划的配置名称中有字符串引用。
`publicRecord` 已清洗会话及其 router draft，但 `described` 在清洗后追加 `default_profile`；
集合里的来源/规则选项、abandon 的计划也独立构造，因此这些字段没有使用私有会话的密码种子。
HTTP 的通用出口只加载 Agent/rule-proxy 等全局 token，不会从这些私有文件补齐 router draft 密码。
公开的 owner/校验错误也独立返回到 HTTP `message`，未经过会话凭据种子的清洗。

证据：新增 `TestReviewC36ResponsesRedactPrivateSessionCredentials`，通过公开 Service 返回值验证
GET、PATCH、集合、abandon dry-run 四条边界。修复前四项均失败，典型内容为
`default_profile.current.name = "Visible <合成私有密码>"`、`subscriptions[0].name` 或 `profile.name` 原样带出同一密码。
普通 router `username=operator`、`has_password=true` 仍需保留；私有文件必须保留恢复凭据，三个只读分支文件字节不变。
另新增 `TestReviewC36PublicOwnerFailureRedactsSessionCredential`：外部 owner 的公开 409 诊断引用
私有 router draft 密码时，修复前 `public.Message` 原样泄漏；修复后消息清洗，仍为 409，
`errors.Is(err, original)` 成立，失败请求不改私有文件。

修复：完整组装后，用原始会话（集合为全部列出的会话）的凭据种子调用共享 `redact` 做最终字符串引用清洗。
不重复定义凭据语法，不把 `has_password` 布尔值重新当作凭据字段，不修改存储，也不修改 Python/比较器。
修复后四项通过，并断言配置/来源 ID 与元数据仍存在，只有秘密引用被遮盖。
会话操作返回时还对公开 `setup.Error` 和 repo validation/in-use 消息执行同样清洗；原错误作为 Cause 保留。
固定的「等待首次使用向导模块」不含外部数据，不因弱密码恰巧等于这段固定文字而改变精确 503 合同。
已登记 `behaviour-changes.md` 的 C36 项。

修复提交：`1fe46a78f`（成功响应）、`3defa2895`（公开失败及错误链）。

## 重点检查结论

| 检查 | 结论与证据 |
|---|---|
| Python、状态、revision、存储 | 逐段核对五个源文件及 native 编排。真实 Flask 的认证、创建/重试、source、progress、HEAD、body/缺失记录错误和 DB/files 对照通过。只抽查一条 router prepare 的真实 Python owned sections 正常路径及 foreign-edit 409，不重复两种目标的整套材料。新增 `TestReviewC36PrepareRevisionAndStoredOwnership` 明确验证首次 profile revision+1、第二次无增量，session 的 profile/config revision 和 full/without-base digest 均对应已保存值。 |
| A21/A22/router 缺失 | 新增 `TestReviewC36MissingOwnersReturn503WithoutCommit`，分别到达 DNS、installation、managed router 缺省 callback，逐个断言 `*Error.Status==503`、精确中文「等待首次使用向导模块」、DB 指纹和会话字节不变。server 注册抽查证明 DNS callback 注册后从 503 进入真实校验 400，未用假 published/empty plan。 |
| abandon 锁顺序和持有期 | `Action` 整个 abandon 分支在 `recordStore.locked` 回调内，release 用 defer；plan、dismiss、DeleteProfile、共享来源/health/cache 清理、最终 session remove/write 全部结束后才释放。唯一锁为 LevelSetupSession，严格早于 Device/profile-operation 和 repo；既有子进程顺序正/反例抽查通过。 |
| ReadSession 三道限制 | `setupflow.ReadSession` 是两方共用的无操作锁 leaf：LimitReader(Max+1)，16 MiB 上限、UTF-8 和 JSON.Valid，错误链保留 ErrReadSession/ErrInvalidSession；无目录创建。抽查 `TestReadSessionSharesProfileReaderBoundary`，并检查 typed ownership 验证没有被抽取 reader 时丢掉。 |
| shutdown 与断连 | 写 owner 先 Detach 并检查原 Runtime generation；锁后及 session rename 前再次检查。新增真实 Runtime 用例在 DNS pure owner/安装占用读取返回前 shutdown，确认 prepare/abandon 不越过后续 DB/session 提交点。抽查已有 disconnect 的所有权接管测试、route worker shutdown/预算测试；没有 Background 新起 generation。 |
| HEAD | API 只有显式 POST/PATCH 才写；GET/HEAD 进入 Index/Get，锁不创建目录；新响应清洗不写私有文件。真实 Flask HTTP/全存储快照抽查通过。 |
| 脱敏 | M1 已修。router draft 的公开 View、未知凭据字段和正常用户名/has_password 保留策略均检查。外部 callback 按约定返回保留 Cause 的公开 `setup.Error`；C36 再遮盖公开消息里的私有会话凭据引用，保留状态和原错误链。纯 DNS 校验包装为 400，未注册必需 owner 为固定 503。内部非公开错误继续走统一 JSON 错误出口，未增加原始日志。 |
| C32 ReadRulesetContent | 保持单一 sourceContent；旧调用方 sourceOptions 的零超时仍为 30s/30s，fetch 的 boundDeadline=false 未变。新入口只转换参数，boundDeadline=true，每次 fetch 按剩余 deadline 收紧 connect/read；routeLoader 另有总预算和原 generation tracked worker。抽查离线零网络、connect 参数计时、真实 loopback stalled read、原 mirror/default30s、route warm-up 总预算与 shutdown，均通过。 |

外部接线边界仍按原批准清单保留：A21 最后合入者负责真实 DNS callbacks 与 AdoptOwnedWrite 注册，
并验证其真实 embedded-save 的 revision+1 前置条件；A22 负责真实 latest/has-installation/gateway status；
路由器负责 Driver/Inspect/BuildPlan/Close。C36 不复制这些 owner、不以当前测试代替正式集成。

## 检查命令与结果

修复前（预期 FAIL，四项泄漏均命中）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/setup/review_c36_test.go -- bash -lc 'cd server && gofmt -w internal/setup/review_c36_test.go && go test -count=1 -run TestReviewC36 ./internal/setup'
```

末行：`FAIL configflow/internal/setup 1.267s`。修复后同一公开响应回归通过。

新增回归最终完整运行（响应四项、revision/ownership、真实 Runtime、精确 owner 503）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/setup/review_c36_test.go -- bash -lc 'cd server && gofmt -w internal/setup/review_c36_test.go && go test -race -count=1 -timeout=2m -run ^TestReviewC36 ./internal/setup && test -z "$(gofmt -l internal/setup/service.go internal/setup/action.go internal/setup/review_c36_test.go)" && go vet ./...'
```

末行：`ok configflow/internal/setup 12.130s`；gofmt 无输出、vet exit0。
之后只调整 owner 测试 fixture 的隔离（显式清掉目标 callback，避免依赖全局已注册状态），再定向运行：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/setup/review_c36_test.go -- bash -lc 'cd server && gofmt -w internal/setup/review_c36_test.go && go test -race -count=1 -timeout=2m -run "^TestReviewC36MissingOwnersReturn503WithoutCommit$|^TestSetupServerOwnerRegistrationRemovesUnavailable$" ./internal/setup ./internal/server'
```

末行：setup `4.302s`、server `1.730s`，均 ok。

正常 prepare 的真实 Python 对照单独只选 router 分支：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/setup/review_c36_test.go -- bash -lc 'cd server && gofmt -w internal/setup/review_c36_test.go && go test -race -count=1 -timeout=2m -run "^TestReviewC36" ./internal/setup && go test -race -count=1 -timeout=2m -run "^TestPrepareFieldsMatchRealPythonAndRetainOwnership$/router$" ./internal/setup'
```

末行：setup `8.117s`、prepare Python 抽查 `4.194s`，均 ok。

以下是同一次 rt 前台检查链中已执行通过的其余独立阶段（都在 server 目录）：

```sh
go test -race -count=1 -run "TestSetupSessionSuborder|TestReadSessionSharesProfileReaderBoundary" ./internal/lockx ./internal/setupflow
go test -race -count=1 -timeout=3m -run "TestPublicRulesetContentTimeoutsAndOffline|TestRouterOfflineSourceOrderAndZeroNetwork|TestReviewSourceTimeoutAndMirror" ./internal/api/rules
CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=2m -run ^TestSetupFlaskHTTPStateAndAuthentication$ ./internal/api/config/difftest
```

末行：lockx `2.016s`、setupflow `1.299s`、api/rules `4.213s`、Flask difftest `5.640s`，均 ok。

另抽查已有 Runtime 的断连/预算：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/setup/service.go internal/setup/action.go internal/setup/review_c36_test.go)" && go test -race -count=1 -timeout=2m -run "^TestRouteWarmupBudgetAndOriginShutdown$|^TestSessionOwnerRuntimeCancellation$" ./internal/setup && go vet ./...'
```

末行：`ok configflow/internal/setup 1.866s`，vet exit0。无新 parser/fuzz target，不重复作者已通过的 60s fuzz。
完整 batch 门禁仍由协调者执行。

公开失败回归修复前（预期 FAIL）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/setup/review_c36_test.go -- bash -lc 'cd server && gofmt -w internal/setup/review_c36_test.go && go test -count=1 -run ^TestReviewC36PublicOwnerFailureRedactsSessionCredential$ ./internal/setup'
```

末行：`FAIL configflow/internal/setup 0.534s`，`public owner error leaked a credential or changed status`。

公开失败修复后（全部新增回归、Flask 旧状态/消息及全仓编译检查）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/setup/service.go --back server/internal/setup/action.go --back server/internal/setup/source.go -- bash -lc 'cd server && gofmt -w internal/setup/service.go internal/setup/action.go internal/setup/source.go && go test -race -count=1 -timeout=2m -run ^TestReviewC36 ./internal/setup && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=2m -run ^TestSetupFlaskHTTPStateAndAuthentication$ ./internal/api/config/difftest && go vet ./...'
```

末行：setup `13.459s`、Flask difftest `5.710s`，均 ok；vet exit0。
随后为精确 readiness 文案增加“密码恰等于固定文案”的合同保护，定向验证受影响的两个用例：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/setup/review_c36_test.go -- bash -lc 'cd server && gofmt -w internal/setup/review_c36_test.go && go test -race -count=1 -timeout=2m -run "^TestReviewC36MissingOwnersReturn503WithoutCommit$|^TestReviewC36PublicOwnerFailureRedactsSessionCredential$" ./internal/setup && test -z "$(gofmt -l internal/setup/service.go internal/setup/action.go internal/setup/source.go internal/setup/review_c36_test.go)" && go vet ./internal/setup ./internal/api/setup'
```

末行：`ok configflow/internal/setup 8.959s`；gofmt 无输出，targeted vet exit0。
