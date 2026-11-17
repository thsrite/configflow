# C11 独立复核

状态：复核完成，**high 0 / medium 1 / low 1，均已修正，0 未决**。
两项均为验收证据/覆盖问题，没有发现需修改产品实现的功能缺陷。
分支 `go-review-c06-c11`，起点 `cefe6f7e3`，初始基于 `go@f90a156eb`。
复核者未编写移植；未 push、未启动子 agent、未访问生产。新增数据全部合成。

## 发现与修正

| 编号/级别 | 场景与证据 | 修正、回归和提交 |
|---|---|---|
| F1 / medium（验收覆盖） | 交接把 `test_only_hosts_changes_invalidate_agent_content_checks` 的四条 C15 回放当作失效副作用覆盖。逐条读取录制文件及 replay switch，四条全部是 `source_cache_metadata`，不会调用 SaveSubscriptionNodes 或 invalidator。另一个 provider-shadow 场景只列 C18 的生成调用，也不能证明之后的 snapshot.Validate。 | 明确 C15 只证明元数据，实际失效由 C13 `TestHostsChangeInvalidatesAgentContentChecksOnce` 验证；扩展它为真实 parse → nodes → SaveSubscriptionNodes 落盘，增加无 hosts 时仅节点地址变化不失效、相同 hosts 映射新增节点不失效，保留 hosts 新增/修改才失效。`9116717ae`。provider-shadow 映射补上 C17 `TestRecordedPythonTests` 中同源 1 capture + 2 validate（冲突拒绝/原始出口接受），另有 1 bundle；`95f0bb80d`。 |
| F2 / low（文档计数） | c11.md 声称旧 18 函数已过时、当前 19；PROGRESS-config 也写 19。实际 AST 和逐行 def 清单只有 18，映射表也恰好 18 行，没有漏掉第 19 个测试。 | 修正为 18，与参数化执行次数、录制调用次数明确区分。机械核验 18 chain + 22 DNS + 10 listener 共 50 个源函数全部有映射；所有写明的 C18/C19/C15 调用数匹配录制文件，无未注明的 patched 行。`95f0bb80d`。 |

## 审计结论

- 完整阅读 Python proxy_chains.py、commented_mihomo.py、mihomo_listeners.py、
  proxy_server_dns.py 及三个源测试文件；逐分支对照 Go chains/listeners/proxyserverdns、
  provider sorting 与已复核 N7-2 editor 的公共调用。共享 dialer compat 的既有结构未扩张，
  未新建私有 regex/parser/锁/后台 owner；新实现复用 `rules.Regex.Groups()`。
- 捕获组数只在 provider 实际消费 backtick 桶时编译，且每组只编译一次。未消费的无效
  filter 仍不报错，`TestChainProviderFilterWithoutSources` 验证这一 Python 边界；
  实际消费的非法正则返回编译错误，不再靠括号计数猜测。
- 扩展 `TestChainProviderCaptureGroups` 为 13 个真实 Python 向量：数值/命名条件的
  两个分支、verbose 中伪括号/伪命名组、命名捕获、转义括号、字符类括号、
  `(?#...)` 注释、12 个捕获的 `$13`、第二桶命中和多桶同时匹配时首桶优先。
  比较完整 provider/group/override 输出，再按生成 target 中实际 `$N` 数字执行全部
  排序替换；不从 Groups 重建待验证 target。与 Python 最终节点名相等。
  `95f0bb80d`，定向及 race 均通过。此处验证输出契约，不冒称启动了真实 Mihomo 内核。
- 50 条源函数逐项核验：原始文件确有该函数；每个引用的 Go 测试存在；录制调用实际存在、
  没有以函数名相同但跳过的行冒充覆盖；手写逻辑图/入边、禁用链、深拷贝、
  注释/alias/merge/CRLF 和事务 rollback 测试检查相应断言。C18/C19 生成及 C15 refresh
  replay 已复跑；original-catalog 另核对 `TestProviderPreflightGetsLogicalProfileAndFinishedDocument`
  确实断言实际 preflight 收到逻辑 profile，而非只回放生成输出；F1 的实际 shared invalidator 和 C17 Capture/Validate 另行复验。
  C06 映射另核验 58 个源函数无遗漏，本文不把外部 leaf 算作 C11 完成。
- DNS 的可用 listener、hosts 最终目标/IP/循环、registrable-domain 压缩、Fake-IP
  blacklist/whitelist/rule、用户 policy 优先级、旧/未知内核、discovery 版本来源及
  OutdatedAgents callback 保留；完整 DNS corpus 和生成消费者通过。无新增功能差异。
- N7-2 已独立复核的 editor 保持不变；listener helper 成功后才替换 profile 字段，
  解析/变量/不确定编辑拒绝时原文不改。测试 HTTP shell 是公共 helper/repo 事务契约，
  不是 proxy-group 真实 CRUD 的替代。

## 外部边界

- **C33**：真实 PUT/DELETE `/api/proxy-groups/<id>` 的 rename/disable/delete、
  listener_changes/中文 message、其他入边 409 保护、保存失败原子 rollback。
  这里已有 helper/editor/事务契约；真实路由验收仍由 C33 负责。
- **C34**：真实 POST `/api/generate/mihomo/preview` 将 OutdatedAgents 写入
  `subscription_hosts.dns.outdated_agents`；生成 DNS 和 callback 单测不能替代此响应。
- **A14**：传入内核版本后的生成结果有 C18 回放；实际 Agent preparation/publication 选择并传递
  discovery 版本及其状态机仍归 A14，不能用 C11 的生成测试代替实际 owner 链验证。
- **C13/C15**：C15 的四条元数据调用与 C13 的缓存失效副作用已拆开列证；没有借此
  宣称覆盖 Agent 内容状态机。C06 的共享最终提交窗口见 review-c06，属于 O3-6c。

无新写 owner、HTTP/device client、共享驱动变更或用户操作入口；WORKFLOW 四常设项中
不涉及的 client/raw request 项不以本模块测试冒充。无需新增 doc 截图。

## 验证

- `.64` 完整 subs、subs/difftest、convert、refresh 及 C06 HTTP 消费者全部通过，精确命令
  见 [C06 报告](review-c06.md)。最终扩展后 scoped race：settings 42.567s、
  httpx/difftest 96.790s、subs/difftest 24.710s；全仓 vet、格式检查通过。
- `RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -race -count=1 -run "^TestHostsChangeInvalidatesAgentContentChecksOnce$" ./internal/substore'`
  退出 0：`ok configflow/internal/substore 1.599s`。
- `.32`：`go test -count=1 -run '^TestRecordedPythonTests$' ./internal/provider`，
  退出 0：`ok configflow/internal/provider 199.603s`，包含 F1 所列 capture/validate 回放。

八个原始 Python 文件在 `.32` 退出 0：`296 passed in 58.51s`。精确 rt 命令见 C06 报告。

变基到本地最新 `go@a08c4cc27`（N7-3d），唯一冲突仅合并 behaviour-changes 两段追加文字。
修正提交变为 `95f0bb80d`、`9116717ae`。变基前 gate 主动中止（exit 241），不计通过；
变基后第二次 gate 因 C06 新日志修复主动停止（exit 241）；最终 gate 在 `86edb0737`
上运行，日志目录 `.32:~/.cache/rt-tmp/review-c06-c11-fixed`，队列守卫及命令见 C06 报告。最终结果待追加。


最终又变基到 `go@1439e503d`（只新增 configdiff 工具/文档）；与 gate 启动时的
backend/internal/cmd/go.mod/go.sum/scripts/tools/proddiff 完全无差异，核验命令见 C06 报告。
因此本次 gate 继续运行，测试源树未改变。最终 C11 修正提交：`95f0bb80d`、`9116717ae`。
