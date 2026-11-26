# batch-7 集成交接

## 最终交回

**batch-7 已完成集成，可交协调者执行完整 core/race 门禁。**
最终基底为 `go@55c9794a0`，已确认是本分支祖先；源码、测试、文档均提交，未 push。
最初基底为 batch-6 f9f3105ee；下方分支区间表保留原始叠入边界和去重依据，后续均 rebase。

| 最终检查 | 结果 |
|---|---|
| 全树 gofmt | 无输出 |
| 指定 preflight（两组 vet、全量非 race） | `PREFLIGHT PASS` |
| 完整 compare | `GATE STAGE PASS: compare` |
| proddiff | `baseline: new=0 stale=0`，self_check PASS，Python/Go retries 0/0 |
| difftest | 全部23包通过 |
| synthetic live | 23 required entries全部执行通过，0 skips，Python子测试/WebDAV exec已验证 |
| 最终树 O3-8 关机故障 | `FAULTS PASS: 16/16` |
| 128节点＋512规则公开生成基准 | 约76.0–76.1 MB/op，低于batch-6约77.2 MB/op |
| pyref | 10份transcript的2916条exact输入/输出字节未变，仅来源metadata更新 |

检查固定运行在用户指定的主测试服务器，未运行完整core/race门禁。
预检/故障/基准的产品源码树为bb6cb1b00；之后只有测试oracle、harness和文档收尾，产品代码未变。
完整compare通过树为6c6b46ff5；最终文档提交不影响任何已验证产品或测试代码。

五项指定集成分别提交：

| 集成项 | 提交 |
|---|---|
| 1 唯一RouterJobLock、删除过渡锁/钩子 | a65fbccf4 |
| 2 A22真实注册与路由优先级/NewApp | 524ceec4a |
| 3 删除全部R14/A18 deferred baseline | feb6e94da |
| 4 O3-8 16场景 | 749f8257c；最终基底重跑 bb6cb1b00 |
| 5 A14无真实时间依赖的节流回归 | 3b44e7687 |

协调者批准的D2后果：切换后需重新做一次IPv6验收；已忽略通知会重新出现一次。
已记入behaviour-changes并标注O12发布说明；仅4个精确baseline字段，无宽豁免。
另有5条精确、格式/120秒受限的请求或采集时钟规则；缓存标志和全部业务证据仍严格比较。
本批指定功能与集成项均已完成，未增加依赖。完整门禁由协调者在此分支继续执行。

### 集成收尾提交（最终 rebase 后，不含本交接文档提交）

```
a65fbccf4 go(routers): integrate one job lock across sync and publication owners
feb6e94da go(proddiff): retire integrated R14 and A18 deferred differences
524ceec4a go(server): verify A22 route priorities through real registration
3b44e7687 go(deploy): make progress throttle regression use an injected clock
749f8257c go(lifecycle): verify integrated shutdown ordering with sixteen fault scenarios
203e73891 go(pyref): bind stacked native dependencies without changing oracle outputs
3c88b9846 go(integration): resolve native conversion and audit preflight failures
bddef4062 go(pyref): refresh conversion provenance after preflight repairs
860e68666 go(parity): record clean batch-7 non-race preflight
b78a2a32b go(integration): preserve native generators over batch-6 performance repairs
7319e4569 go(parity): record clean preflight after batch-6 repair rebase
959d26895 go(integration): close live compare differences at new route boundaries
268c43cdf go(integration): format the remaining stacked source files
d1b2aa3cc go(proddiff): require exact wire formats for request clocks
6f81db009 go(convert): keep stacked native collaborator codecs narrow
d80e7a24a go(integration): compare setup abandonment after full A22 wiring
bb6cb1b00 go(integration): verify final-base allocations and sixteen shutdown scenarios
43a791799 go(integration): enable real A23 enrollment in the publication oracle
6c6b46ff5 go(proddiff): align discovery cold-cache comparison ordering
```

## 叠入与过程证据

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

## 验证范围

定向检查、最终基底预检、完整compare及16场景均已完成，证据见上方最终结论和下方各轮记录。
完整core/race门禁由协调者执行。

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

rebase 的 range-diff 核对：原75提交全部保留，74个补丁相同；唯一改变是 N8-8b 替换已优化旧生成器
时的上述冲突补丁。没有额外删除/重复源分支提交，原始 source boundary 表继续适用。

### 追加修复后的预检

同一主测试服务器，按原命令先 `rt sync` 再执行
`bash /tmp/preflight.sh program__configflow-go-batch-7 -p 6`，两组 vet 和全量非 race 全通过，
最终输出 `PREFLIGHT PASS`（退出码 0）。验证产品树 1f610a0ea，包含 f2666fd23 的全部已提交修复。
没有运行完整 core/race 门禁。开始重新执行 `GATE_ONLY=compare server/scripts/gate.sh -p 6`。

### compare 首轮归因与协调者决定

compare（产品树 1f610a0ea）在 proddiff 阶段返回 `baseline: new=9 stale=0`、self_check PASS；
Go 有18次重试，本轮结束负载高，尚未执行后续 difftest/live。未把传输差异或重试加入 baseline。

- 4 项为新入口请求时刻：A23 refresh_error.at、R16 upstream-routers checked_at、
  notifications checked_at、R15 policy generated_at。每项精确 route/path、120秒、同类型/格式
  归一化，并保留缺失/错误格式/超窗/其他字段的负向控制；A23 时间格式修成带明确本地偏移
  的秒级 ISO 时间，与真实 Python 一致，不靠规则掩盖格式差异。
- 1 项为 A23 候选缺失：Go 额外用心跳新鲜度排除了 stored status=online 的客户端，
  Python `_client` 使用 GetAgentByID 的持久状态。修为相同状态判断；增加过期心跳的
  真 Flask↔Go HTTP fixture，继续拒绝 stored status=preparing/offline。
- 3 项为旧 IPv6 验收报告 D2 digest 失效的 message/details（长度、首项）。
  协调者明确确认按 D2 接受，只登记三个精确字段，新增新 native 报告有效、旧报告陈旧
  且 GET 不改写存储的回归，保留原历史报告。
- 1 项为 notifications items 长度。私有证明同一历史 subscription 通知原已 dismissed，
  Go native fingerprint 使其一次性重现；不是本轮产生的新失败。协调者明确确认按 D2
  登记这一个精确字段；新增 subscription 类 synthetic dismissed→pending→native dismissal
  不再重现、真正新事件重新出现的回归。所有已有项字段仍逐值比较。

两个 D2 后果均已进入 behaviour-changes，明确标记 O12 发布说明：切换后需重新做一次 IPv6
验收；切换后已忽略的通知会重新出现一次。没有恢复 Python hash 编码。

本轮定向结果：`CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=15m ./internal/agents/ipv6 ./internal/api/ipv6 ./internal/notifications -run "TestClient|TestD2VerificationReportMigration|TestRoutesAgainstFlask|TestNativeD2"`
→ ipv6 6.463s、api/ipv6 4.693s、notifications 1.047s 均 ok。
新增 old-heartbeat 客户端通过真实 Flask HTTP 对照；旧报告与新报告检查分别通过。
pyref audit 0.571s 通过；proddiff 全部54项 synthetic 自测 `Ran 54 tests ... OK`，
包括4条时间规则的类型、格式、缺失、超120秒、其他字段和其他路由负向控制。

### 等待最终基底期间的限定验证

协调者明确要求：batch-6 代码 tip 尚未确定，等待它交回前暂停全量 preflight/compare，只继续
冲突、定向验证和128节点基准。第三轮过渡树 preflight 两组 vet 已过，按此指示主动终止本树
的进程及子进程（旧产品树9733ee0a5；exit 255），不把它记作 PASS 或产品失败；未动其他任务。

预先 rebase e5cbeb6be 的窄 codec 优化。N8-8b 的完整原生生成器替换旧生成器，保留其直接
ApplyNodeDialer/RuleFormat/RulesForOutput 路径；legacyMembers、rules/rule-library/github/dialer/
EasyTier 的窄投影全部保留。configFromLegacy 只恢复在公共生成/interop入口按调用一次的全量
转换，未把任何每节点/每规则集路径改回全量解码。common_native AST 禁用表保留双方并集。
本次两个冲突：mihomo_generate.go、common_native_test.go；没有丢弃窄 codec 的回归或基准。

e5 定向验证发现的真实集成问题：shapeConfigFromLegacy 收窄后，旧 Surge/Loon 全量生成入口
仍调用它，会丢失规则/客户端设置。抽出同一原数组形状校验，仅这两个完整生成入口走
clientConfigFromLegacy；元数据入口继续窄解码。严格 R5 拒绝恢复，未放宽校验。

首次128节点基准约156.9 MB，profile 指向 N8-8b 各协作者适配器反复编码完整配置。
按实际 owner 输入收窄：group/chain/hosts/provider 协作者只传完整四类目录；规则校验只传
rule_configs；lowering 结果仅替换原生目录和 proxy_groups，保留其他 typed profile/settings/
rules/unknown 原样。完整公开入口按 profile/shared/system 各自拥有的字段解码，避免三份
record 把同一完整 view 都当 unknown fields 保留。没有复制 owner 业务逻辑、绕过回调、
移除校验、跨请求缓存，或在每节点/每规则集恢复全配置解码。

主测试服务器、GOMAXPROCS=4、同一公开 BenchmarkMihomoGenerationProfile、128节点+512规则：

| 树 | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| batch-6 e5 作者结果（同基准） | 72,510,587 | 77,159,944 | 763,421 |
| batch-7 第1次 | 64,733,090 | 75,989,080 | 710,420 |
| batch-7 第2次 | 66,089,995 | 75,851,712 | 710,374 |
| batch-7 第3次 | 70,195,770 | 75,989,688 | 710,452 |

分配没有回升。RulesetFormat 面对0/1/16 MiB无关历史，约29–44 KB/op（冷缓存采样差异），
不随历史大小增长。新增同一公开128节点回归明确验证全部512规则的数量/顺序/内容，避免
用漏输出来降低分配。定向 race convert 7.294s 通过，54项 proddiff synthetic 自测0.284s通过。

窄适配器改动后，定向执行所有受影响生成器的历史边界：
`go test -count=1 -timeout=20m ./internal/convert -run "^(TestCorpus|TestMihomoCorpus|TestMihomoRecordedPythonTests|TestSurgeLoonCorpus|TestSurgeLoonRecordedPythonTests|TestGenerationRetainsAllNodesAndSource)$"`
→ `ok configflow/internal/convert 74.484s`，包含完整512规则输出回归。
最终来源指纹更新留到协调者交付固定基底后，避免对不断变化的 legacy.go 重复录制；所有
原始 oracle 输入/输出继续保持，下一次只允许 metadata 变化且须先确认 live 输出0差异。

### batch-6 difftest 修复叠入

预先 rebase 到259efd4da（含a16a9fc99文档及两项测试修复，产品基底仍e5）。
.md冲突继续同时保留两边。upgrades_diff_test.go 的 notificationItems 与A18旧修复冲突：
保留最新 notificationStoreComparison（结构比较对象、保留可见列表顺序及字段），同时保留
A18的真实配对集成，未回退到旧字符串/对象排序实现。

A22 已在本批完整安装，新的setup差分测试仍硬编码abandon缺owner的503。集成树明确隔离
SetExternalOwners(empty)验证503且DB/文件不变，恢复真实A22后再与Flask做正常abandon并
完整比较DB/文件，避免通过删除依赖守卫断言来适应完整注册。

`CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=20m ./internal/api/config/difftest ./internal/api/agents/difftest ./internal/gateway/onboarding -run "^(TestSetupFlaskHTTPStateAndAuthentication|TestNotificationStoreComparisonPreservesDisplayAndData|TestUpgradesDifferentialAgainstFlask|TestSetupReadinessContract)$"`
→ config/difftest 8.217s、agents/difftest 30.781s、gateway/onboarding 6.931s 均 ok。
本次没有重跑全量 preflight 或 compare；仍等待协调者确认最终代码基底。

### 最终基底确认

协调者确认 batch-6 已合入并推送，指定 `go@55c9794a0`。batch-7 已执行
`git rebase --onto 55c9794a0 259efd4da batch-7`，82个提交全部无冲突重放；该基底是当前
分支真实祖先。与上一次定向验证树相比只有WORKFLOW及batch-6报告变化，产品文件相同。
按最新要求，最终树重跑128节点基准、O3-8恰好16个关机场景，然后只跑一次preflight和完整compare。

最终只读live check：MosDNS 47072 corpus、5436 recorded 调用仍0 differ，router-split ok 1.007s；
仅三个golden的legacy.go来源指纹过期，未出现输出变化。显式record之后继续核对2916 entries。

最终树检查（主测试服务器，GOMAXPROCS=4）：全树gofmt -l无输出；来源metadata显式record
`ok configflow/internal/mosdns 48.903s`。十份transcript、2916 exact输入/输出再次哈希完全不变。
最终基准128节点/512规则三次：
- 67,310,973 ns/op，75,977,992 B/op，710,390 allocs/op。
- 64,800,863 ns/op，76,036,272 B/op，710,434 allocs/op。
- 63,907,876 ns/op，76,097,896 B/op，710,407 allocs/op。

最终产品树 d80e7a24a，`server/tools/faults/run.sh`（仅当前16场景）退出0：
`FAULTS PASS: 16/16`。sigterm-cancel-deadline 的 scheduled refresh首轮等待约30.076s；
detached I/O贯穿drain、stopJobs后6.004s取消；取消下载无配置变更。1000请求/100断连后
14→14 goroutines，所有SQLite/JSON/metrics已提交前缀可读且重启后可再次工作。
未增加或跳过场景，报告保留在runner打印的私有cache路径中。

### 最终 preflight

最终基底 `go@55c9794a0`，验证树bb6cb1b00。严格按指定顺序：
`RT_HOST="$BATCH_TEST_HOST" rt sync`
然后在同一主测试服务器运行：
`bash /tmp/preflight.sh program__configflow-go-batch-7 -p 6`

退出0，最终输出：
```
== vet
== full non-race tests (-p 6)
PREFLIGHT PASS
```
两组tag的vet和全量非race通过；未执行完整core/race。现在仅执行完整`GATE_ONLY=compare`。

### 最终 compare 首轮的测试接线收尾

同一产品树首轮完整compare：proddiff `baseline: new=0 stale=0`、self_check PASS，
Python/Go retries均0。23个difftest包中22个通过；agents/difftest的A14发布用例在stage及完整
DB对照发现ipv6_verification_requested字段不一致，synthetic live尚未执行。
根因是旧publish_app.py显式把Python A23 enrollment设为no-op（当时A23未移植），集成树
的Go已经接入真实A23 hook。移除该临时禁用，使用完整真实Flask；另明确断言双方stage响应
必须有true标记，原DB/文件和Agent请求对照保留，没有忽略该字段或禁用Go功能。

修复只涉及测试/oracle，产品源码相对已通过预检、16场景、基准的bb6cb1b00未变。
`CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=10m ./internal/api/agents/difftest -run "^TestPublishHTTPAgainstFlask$"`
→ `ok configflow/internal/api/agents/difftest 8.634s`。现在重新执行完整compare全部阶段。

### discovery 冷缓存比较顺序收尾

下一轮compare在proddiff停下：new=2/stale=0，双方retries仍0，两个字段仅为
GET /api/client-devices/discovery 的cached和checked_at。代码核对表明60秒只是stale标记，
已有快照不因TTL自动重读；并非产品缓存过期错误。根因是harness并发发出同一profile的
显式/default查询，两边独立调度，可能把冷读取和已命中读取互相比较。

仅把此精确GET路由的case按原序串行，先对齐冷/命中转换，其余GET/POST仍按原阶段并行，
日志最后串行。cached/stale/refresh_error/devices继续严格比较，没有加baseline或忽略字段。
checked_at确由首次采集请求的时钟产生，命中时保留该采集时刻；为该精确route/path增加
相同120秒及秒级偏移格式约束，解释采集请求时间，非任意持久数据时间。synthetic测试
实际执行同一调度函数，验证冷→命中、日志最后，并证明缓存标志/错误/设备变化仍被检测。
`python -m unittest discover -s server/tools/proddiff -p test_proddiff.py` →55 tests，0.324s，OK。
本轮修复仍只有harness/规则/测试，已验证的产品树bb6cb1b00没有变化。


### 完整 compare 最终结果

固定主测试服务器：
`RT_HOST="$BATCH_TEST_HOST" rt -e GATE_ONLY=compare -- bash -lc 'server/scripts/gate.sh -p 6'`

最终运行退出0，完整执行proddiff、23个difftest包和synthetic live，最终输出：
```
baseline: new=0 stale=0
SYNTHETIC LIVE PASS: 23 required entries ran and passed; 0 skips; repo Python subtest and WebDAV Python exec verified
GATE STAGE PASS: compare
```
proddiff self_check PASS，Python/Go transport_retries均0；coverage_regressions=0、unmapped_routes=0。
全部23个difftest包均ok，其中api/agents/difftest 283.736s；没有跳过A23 enrollment字段。
本次gate的私有日志目录后缀为gate.TAy2Wm，数据未复制入仓库。
最终提交前确认go@55c9794a0为祖先、无R14/A18 deferred残留，git差异空白检查通过。

### core fuzz 后续修复

协调者 rebase 到 go@95440b69e 后发现 Mihomo graph fuzz 的裸 `<<` 标量。
根因修复、crasher、特殊字符串根/键/值回归和定向 race / 六目标 60s fuzz
见 [batch7-fuzz-merge-zh.md](batch7-fuzz-merge-zh.md)。
