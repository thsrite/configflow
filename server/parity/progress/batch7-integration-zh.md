# batch-7 集成交接

任务：以 batch-6 f9f3105ee 为基底叠入已复核分支，完成五项集成，交协调者跑 core/race 门禁。
仅在新 worktree/branch batch-7 工作；原分支和 batch-6 worktree 未修改。

## 分支叠入

每支使用独立临时分支从指定 tip rebase --onto 当前 batch-7，再 fast-forward 回 batch-7；
未把各分支包含的旧基底重复叠入。下表是 source boundary..tip（右侧为当时叠入后的 tip）。

| 分支 | 取用区间 | 叠入后 tip |
|---|---|---|
| go-opus-review-a22 | 663d271bb..9791bc8d | d91c219de5 |
| go-opus-review-r14 | c5f5ff349..a08a120b | 1d55ae951d |
| go-review-r16 | c5f5ff349..1b28b662 | 214abefbf5 |
| go-opus-review-r17 | 673851659..b78bbb38 | 3e7d5a4e3c |
| go-opus-review-r15 | a08a120b..9df1a9a4 | 2fdedbcc1d |
| go-o3-7 | cdca79def..a1d3e1b7 | 6ecbea848b |
| go-o3-8 | a1d3e1b7..b8f6cbeb | da161d9088 |
| go-pyref-narrow | c5f5ff349..06a25922 | ed4a22b44a |
| go-opus-review-n8-8b | 6e9d0b738..fbd9993c | d3f110ab75 |
| go-n8-8c | 7d27bfa11..b1efee615 | ff8547b6f1 |
| go-n8-9-discovery | 9b72fc898..778442e4d | 478bb5022e |
| go-review-n8-9-health | 824189f07..372840ba8 | e04a39de21 |
| go-opus-review-a18 | e84063f9c..74935690 | 9fb89fc22b |
| go-opus-review-a23 | 673851659..3ce4338d | 06f2e87376 |

- R14 264ffbb55 与 batch-6 8ec68fd4d 补丁等价，显式丢弃。
- R17/A23 从 673851659 后取用，不重复叠 R14 作者与已复核 R16 临时基底。
- N8-8b 从 6e9d0b738 后取用；8c 从其等价 8b tip 7d27bfa11 后取用。
  不重复引入 N8-8a/9b72fc898 的 batch-6 等价内容。health 仅取 824189f07 后四个提交。
- .md 冲突按要求保留双方文本；未随意采用整文件 ours/theirs。

## 代码及数据冲突处理

- A22/A18 的 agents/messages.go、api/agents/messages.go：独立消息常量同时保留。
- R14 wire.go：保留 O3-6b 的 repo JobLock hook，同时调用 wireRuleSync；之后在独立锁集成提交中删除 R14 私有 hook。
- R17/A23 同名 RouterOperationLocks 文本不冲突但编译重复：合并为 A23 排序去重的共享 L2 group，R17 显式传 write=true；移除旧副本，复跑双方锁/关停回归。
- R17 ikuai.go：保留新 writeForm 共用实现，Write 调用 lifecycle.Detach，保持 batch-6 的关停取消能力。
- A23 server.go：保留 dnsfollow/dnsha 及 ipv6 imports，路由注册同时保留 routers/ipv6。
- N8-8b/c 删除旧生成器时与 batch-6 validateGenerationRules 冲突：采用 native typed 生成器，
  把相同 validateRuleIdentities 检查移到 MihomoDocument/clientPreflight，保持 R5 discriminator 拒绝。
- pyref 的五份冲突 golden：解压解析后确认 metadata 外输出完全相同，采用新 narrowed provenance；
  最终合成树的来源指纹还需按 pyref 工具刷新，绝不修改 reference 输出以消除产品差异。
- A18 baseline 冲突暂保留 batch-6 文件，将 R14/A18 deferred 删除集中到独立集成提交。

## 集成项

1. 唯一路由器任务锁：实现及定向 race 验证通过。R14/R17/R15/A23 全部调用 RouterJobLock，
   删除 syncJobs 与 R14 hook；KindConflict 保持各入口原中文提示，关停/取消错误原样传播。
   有记录按连接、无记录按 profile+id；HTTP 继续先取锁再执行 404/请求体校验。
2. A22 完整注册：真实 apiagents 注册替换 R15 过渡 shim，Flask/Go 路由优先级和 NewApp 注册验证通过。
3. baseline：已删除 11 条 deferred→R14 和 12 条 deferred→A18；其余分类保留。最终 compare 检验 new/stale。
4. O3-8：16/16 scenarios 全部通过；包括 R14 M1 的 drain→stopJobs→取消 detached I/O 次序。
5. A14 节流：集成树初次独立 race count=3 通过（14.714s）；为消除慢磁盘/负载造成的真实时间依赖，
   仅测试接入既有生命周期 fake Clock（race count=3 验证通过，17.059s），验证 999ms 不写、1000ms 写及强制阶段写，不修改生产阈值。

## 验证及剩余工作

定向检查、预检与 compare 证据将在每项完成时更新。尚未跑完整 core/race 门禁；由协调者执行。
batch-6 若追加日志修复，需要在最终预检前再 rebase 到其最新 tip。

### 集成 1 验证

主测试服务器 `rt`（固定用户指定 RT_HOST）：
`go test -race -count=1 -timeout=20m ./internal/routers ./internal/clients ./internal/api/routers ./internal/agents/ipv6 -run "TestR14ReviewRepositoryHookSharesJobLock|TestReviewR17RepositoryHookSharesJobLock|TestR14JobAndScheduler|TestR12StartPublish|TestR15PublishHTTP|TestR15JobLockPrecedesValidation|TestReviewR17ShutdownWhileWaitingForDeviceLock|TestR17BatchLocksAndRollback|TestR17DistinctDeviceLocksAndMixedVendors|TestR17Runtime|TestRouterOperationGroup|TestOperationKeys"`

末行：routers 3.747s，clients 5.479s，api/routers 6.169s 均 ok；ipv6 编译通过但本正则无测试。
首次编译发现 R17/A23 重复 RouterOperationLocks，统一后上述实际守卫与多设备锁/取消用例全部通过。
本提交包含合并冲突代码的 gofmt 收尾；A14 fake Clock 和 baseline 删除各留下一项独立提交。

### 集成 3 baseline 清理

按 baseline 原理由精确删除 23 行，没有扩大正则或增加新豁免；A14/A21/C34 的其他 deferred
条目仍保留，必须由最终 compare 判断是否还有效。A18 原分支的删除在 rebase 冲突中暂缓，
现在与 R14 一起集中记录，避免漏删旧 logs 路由的 A18 后果行。

### 集成 2 验证

移除 R15 优先级测试中的假 A22 route，使用 apiagents.Register 的真实安装处理器；
NewApp endpoint 清单增加全部六个 A22 endpoint（index 同时承载 GET/POST）。
`go test -count=1 -timeout=15m ./internal/api/routers ./internal/server -run "^TestR15(FlaskRoutePriorities|RealFlaskRoutePriorities)$|^TestNewAppRegistersImplementedRoutes$"`
末行 api/routers 0.566s、server 0.013s 均 ok。
同时回归 batch-6 R5：`go test -count=1 ./internal/convert -run "TestBatch6|TestMihomo.*Identit"` → ok 0.011s。

### 集成 5 验证

未在三次独立 race 重放中复现原真实时间误判，但该断言原本依赖数据库落盘速度；
测试改用 publishProgress 已支持的 lifecycle.ClockFrom，严格检查 999ms/1000ms 边界及同一刻 forced phase。
`go test -race -count=3 ./internal/deploy -run "^TestReviewA14UnforcedProgressIsThrottled$"` → ok 17.059s。
没有修改生产节流逻辑、持续时间、阈值或放宽断言。

### 集成 4 验证

`RT_HOST="$BATCH_TEST_HOST" rt -- bash -lc 'server/tools/faults/run.sh'`，主测试服务器，退出码 0。
runner 当前登记恰好 16 项，实际输出 `FAULTS PASS: 16/16`，没有运行额外 HR 场景。
`sigterm-cancel-deadline` 证明 detached I/O 持续经过 drain，在 stopJobs 后约 6.006s 取消；
取消下载无配置变更，SQLite/已提交前缀可读。1000 请求（100 断连）后 goroutine 14→14。
production-isolation、SIGTERM/SIGKILL、锁等待、写故障和所有断连收敛对照均通过。
报告位于本次 runner 打印的测试服务器私有 cache 目录；未复制设备/生产数据到仓库。

### pyref 叠入后的来源声明更新（已完成）

只读 live check 确认 MosDNS 47072 corpus 行、5436 recorded 调用和 router-split 全部边界 0 differ；
失败仅为来源 metadata 变化（各 golden exact input 数不变）。三个未受影响的包检查已通过。
依据 -suggest-sources 的 AST 依赖建议新增 typed convert/model/health 和 writer 图源文件，
删除实际已移除的 clients.go、health/check/num.go、health/check/pyerr.go；产品边界豁免未变。
显式 record 后再次只读 check：mosdns 51.046s、routersplit 1.391s 均 ok。
本机仅作 blob 哈希查看，确认 10 个 transcript、2916 个 exact input/output 字节全不变，改动仅为 metadata。

元数据更新命令（主测试服务器）：
`PYREF_MODE=record PYREF_ACCEPT=1 go test -count=1 -p 6 -timeout=20m ./internal/mosdns ./internal/routersplit -run "^(TestCorpus|TestRecordedPythonTests|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython|TestN73SharedUpstreamMetadataMatchesPython|TestEdgeCorpus)$"`
随后同参数 `PYREF_MODE=check`。源声明更新前后 product_symbols/imports/members 均未放宽。

### 首次 preflight 集成缺陷与修复

首次预检两组 vet 通过，完整非 race 暴露以下叠入问题，未进入 core/race：

- N8-8b 的 standalone 客户端转换缺少默认解析回调，Snell JSON 导入失败；默认只注册既有
  legacyClientCallbacks，其他必需回调继续由正式 Wire 安装，空环境仍按原测试拒绝。
- batch-6 R5 的严格 rule identity 校验保留，并让带 ID 的错误沿用 N8-8b 可定位诊断。
  数字 value 仍能无损读写，但生成时拒绝；没有回退成宽松 discriminator。
- 共享 model.Int/String 已批准接受 quoted 整数、数字文本。旧参考中 provider interval、health URL
  的这两类差异明确分类；新增 typed 边界证明 interval 的范围验证不变、health URL 输出文本。
  probe-18001 删除旧测试补丁，恢复直接对比原 Python 18001 输出；无 reference blob 改写。
- 实际生成发现 follow_group 缺少 interval 时，本组 quoted override 被转成数值。修复只保留
  该组字段的存储标量类型；所有原始 corpus 组 interval 对照继续执行，没有加入跳过类。
  YAML 失败诊断新增结构路径，避免首条文本引号差异掩盖真正字段类型问题。
- yamlx 审计：客户端边界测试改用 yamlx.Parse/Decode；未增加 raw YAML parser 豁免。
- 生命周期审计：按准确 file:function 更新 health joined worker 名称，增加 IPv6 最多四个
  RPC 且 WaitGroup 收敛的精确说明，删去已同步执行命令的旧 Runner 豁免。
- A23 故意制造 L2 违规的负向控制移入同测试二进制子进程，保留其检测能力，避免全局
  Violations 计数污染 R12 的零违规断言；没有重置计数或放宽 R12。
- pyref live audit 精确登记 A18/A23 已有 CONFIGFLOW_DIFF=1 保护的 Python 调用和上述
  固定 self-process，未增加包级/通配豁免。

首次定向修复回归：lifecycle 0.751s、yamlx 1.168s、clients 8.925s、routers 0.829s、
importer 0.005s、convert 原 R5/身份校验相关 0.032s 均 ok。后续整树 preflight/compare 仍待完成。

后续定向结果：Mihomo 13908 行 corpus / review boundary / shared scalar / follow override / YAML
比较器全部通过（39.296s），pyref repository audit 通过（0.554s）。
修复 follow override 后 live check：MosDNS 47072 corpus、5436 recorded 仍 0 differ，
失败仅为三个来源指纹；router-split 只读 check 通过 1.039s。显式 record 后三个 goldens
仅 metadata 更新；全十份 2916 exact input/output 的哈希再次证明完全不变。
`go test -race -count=1 -timeout=15m ./internal/convert ./internal/importer -run "TestBatch6|TestN88bReview|TestMihomoFollowKeepsLocalQuotedInterval|TestSharedScalarMihomoOutputBoundaries|TestTypedMihomoProviderInterval|TestSnell"`
→ convert 1.292s、importer 1.023s 均 ok。

### 修复后的完整预检

`RT_HOST="$BATCH_TEST_HOST" rt sync`，随后在用户指定主测试服务器执行：
`bash /tmp/preflight.sh program__configflow-go-batch-7 -p 6`

最终输出：`== vet`、`== full non-race tests (-p 6)`、`PREFLIGHT PASS`（退出码 0）。
本次产品树为 ae3ad5077。batch-6 后续日志/性能修复尚待其最终 tip，rebase 后需再确认预检与 compare。

### batch-6 追加修复 rebase

第一次 compare 在槽位队列中尚未执行，主动终止自己排队的 gate（exit 143），没有读取或修改
其他任务。为避免对旧基底重复比较，预先 rebase 到 batch-6 修复支线已提交的 f2666fd23：
包含 3aa7c917b（客户端取消日志）、d74eb1dc2（schema-only 传输诊断）、af7315893/f2666fd23
（旧生成器复用 typed render view）。最终 batch-6 正式 tip 到位后仍核对祖先关系。

仅 N8-8b 替换旧生成器时发生两个代码冲突：mihomo_generate.go、mihomo_downloads.go。
保留已迁移的 native typed 实现；它直接调用 ApplyNodeDialer/RuleFormat/RulesForOutput，
不再逐节点/逐规则集解码整份配置。追加修复的 legacy codec 入口、精确调用审计和 128 节点
公共生成路径不变性回归同时保留。与 rebase 前比较，这两个原生产品文件完全未变；
总差异仅为追加日志/诊断、legacy helper 和测试，不重新引入旧生成器。

追加修复叠入后：定向 race convert 3.066s、httpx 1.967s、api/subscriptions 1.025s 均 ok。
只读 live check 再次确认 MosDNS 47072 / recorded 5436 零差异、router-split ok 1.316s，
仅 legacy.go 来源指纹变化；显式 record 53.397s 通过。十份记录2916 exact entries 哈希未变。
