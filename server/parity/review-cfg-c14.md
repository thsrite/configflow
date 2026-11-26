# CFG-01 / CFG-03 / CFG-09 修复

分支go-fix-cfg-c14，起点go1439e503d。仅rt测试、合成数据；不push、不启动子agent。
每张工单独立提交。Paseo技能用于协调者要求的O3-6c接口确认及C32分工确认。

## CFG-01 / P1 / C14

根因：internalRuleBase在识别API前先Prepare，消除/api/../后把请求重分类为external，
redirect检查因此失效。修复保留原始API分类；一旦进入内部API，origin/prefix边界在整个
链中保持，不能因下一次normalize丢失。规范化后的请求、每个redirect目标均经同一
prepareRuleTarget校验，在任何local callback/transport调用之前拒绝越界。

仅使用原生weburl/net/url/path：对已解码路径做Clean并核对API前缀，反斜杠拒绝；
嵌套转义最多8层原生PathUnescape，每层都验前缀，仍有未证明安全的编码则拒绝。
没有私写URL parser、没有全局公共IP限制或改变正常外部来源。合法内部redirect和编码
名称可用；第3/5跳才跨origin也在目标请求前拒绝。首次normalize越界会更早400，
不再先访问源：H120明确验Python源请求1/Go0、双方禁止目标0，其他响应/SQL/files仍严格。

20组真实Python request_rule+真实loopback HTTP对照：dot、%2e/%2E、双重编码、反斜杠、
跨origin、同origin非API、redirect中的歧义和N跳、合法内外部控制。修复前4组失败，
修复后20组通过；full rulefetch15.246s PASS，全树go vet ./...通过（.64）。

新增对照揭示Python也接受编码API前缀的初始dot路径及三种歧义redirect目标（encoded-dot、double-encoded-dot、
backslash），Go按本票统一规范化边界要求拒绝；测试逐名固定这四例，不泛化为parity
豁免。编码API前缀例实际访问跨origin目标；两个编码dot目标例loopback fake记录到landing路径，反斜杠例记录原始编码路径；不把后者
夸大为已发生外部访问。此安全强化需在后续独立安全复核中一并检查。

复现：
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_cfg01.py'`
`RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server; go test -count=1 ./internal/rulefetch; go vet ./internal/rulefetch'`
最终20对照+local callback前置保护回归0.028s PASS；H120公开接口4/4 compared、open0、design_errors0（.64，22.49s），150个比较器自检和真实body篡改负控通过。
首轮只因旧自检仍期望Go源请求1而停止，未跑业务；更新为Py1/Go0的精确观测后通过，仍拦截多余目标/路径/存储写入。
原始results/triage未改，报告在.ci-results/cfg01/；运行命令：
`RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-fetch-real-internal-redirect-escape-after-dot-normalization --baseline b52f1b79b+observer-selfcheck --output "$PWD/.ci-results/cfg01/results.json" --findings "$PWD/.ci-results/cfg01/findings.md"'`
该基线为amend前等价Go代码；CFG-01完成后交协调者另行安全复核，当前不标verified。

## CFG-03 / P2 / C14

用yamlx.Source复用editor的line/column定位和quoted scalar lexer，按原文token判断物理跨行，
而非只看fold后的Value。行索引支持CRLF、BOM、CJK和YAML Unicode换行；每行列索引只建一次，
避免大flow列表每项重复扫描行首形成平方复杂度。editor与delivery共用quote扫描，不复制PyYAML。

payload跨行或flow mapping中的跨行value会原样返回全部字节+已有line0 warning；普通单行
flow仍可展开，native reader的折叠值/后续规则匹配不变。补设计Clarified和用户文档说明。
真实Python normalize_mihomo_yaml初始14组中旧Go失败10组；最终16组全部通过，覆盖双/单引号、
escaped续行/引号、plain折叠、nested value、tag、BOM/CJK、CRLF及Unicode换行正控。
20,000个同一物理行的Unicode标量位置检查在.64约52.6毫秒完成（隔离15秒截止）。

完整rulefetch12.689s/yamlx4.782s通过，随后全树vet通过。更新原N7正常化测试只要求本delivery
入口保留原文+warning；ReadRulePayload/InspectDomainContent的原生折叠/匹配断言继续严格。
CFG-02由0d31fb93负责route消费warning/每请求一次日志；本票不改api/rules/read_adapter.go。
CFG02 owner以本票d8e6cacff为base，仅cherry-pick其9fe6a851得到临时联合树
59238b255549e04d82b0249f118331027113a515，.64真实rule-delivery-yaml-helpers
复验22 compared、0different、0open、design_errors[]，135.88s；H133两个前缀均diffs0。
原文字节、各一次line0 warning和DB/files均严格参与比较。联合提交未进双方工单分支，
日志由owner保留在.64 ~/.cache/rt-tmp/cfg-c32-cfg0203-joint/。A12展示扫描不计本票覆盖。

复现：
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_cfg03.py'`
`RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server; go test -count=1 ./internal/rulefetch ./internal/yamlx; go vet ./...'`

## CFG-09 / P2 / C28

根因：os.Rename返回*os.LinkError，C28未识别，被legacy PythonClass的ValueError兜底
误归为非法URL400。先判native LinkError及context取消/超时链为服务器失败，再做现有
URL判定；PathError仍保持原有IO分支。保留原始错误供errors.As/Is，不创建异常兼容类。
日志只使用weburl.SafeErrorDetails暴露native类，不泄漏文件路径/URL/凭据。

已先向6e5c7760确认O3-6c接口：RecordContext/ForgetContext在最终rename前检查ctx，
仍直接返回native LinkError/PathError，refresh不改错误类型；本票不复制生命周期实现。
基线上的真实receipt目录rename失败和权限失败、成功URL200、非法URL400/no-fetch控制通过；
私有错误链和公共响应分别验证，确已下载/写cache后才receipt失败，原cause保留且无临时文件。
修复前rename及取消链控制失败，修复后mosdns定向3.861s+vet通过（.64）。

默认Runtime联验用例在基线无RecordContext时明确skip；没有将基线skip计作通过。
隔离联合树4d3dfa7a2 = 本票ddc6b3218（go9c88基线）+ 稳定O3-6c7c9f2a9bc，
真实Runtime关机不落receipt、errors.Is(Canceled)不误归URL、请求断开仍完成、
native Link/Path链四子例每轮均执行无skip。与taskstatus锁等待/中途关机/native causes
共同-race -count=3通过：mosdns23.276s、taskstatus2.237s，vet通过（.64）。
O3 owner确认后续5cefd0b2e/c2dfb6050仅重放于d43cca6e4及注释/progress，产品等价。
联合实现不混入本工单分支。该联合树公开F16两个前缀亦2/2compared、0open、design_errors[]，72.24s；154比较器自检及真实body负控通过。

公开configdiff rule-proxy-processing-failure：2/2compared、0open、design_errors[]，
73.05s；150比较器自检及故意篡改Go body的真实负控通过，HTTP/SQL/files严格比较。
运行基线名d8e6cacff+cfg09-wip，产品代码等价于CFG09提交，未改中央历史results/triage。
`RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-proxy-processing-failure --baseline d8e6cacff+cfg09-wip --output "$PWD/.ci-results/cfg09/results.json" --findings "$PWD/.ci-results/cfg09/findings.md"'`
联合native复现（在独立联合worktree执行）：
`RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server; go test -race -count=3 -v -run "^TestCFG09|^TestO36cStatus|^TestRecordContextPreservesNativeStorageErrors$" ./internal/mosdns ./internal/taskstatus; go vet ./internal/mosdns ./internal/taskstatus'`

## 最终交付验证

已重放最新本地go@d43cca6e4；三票代码互不混入其他owner实现。
当前CFG01 0d543f53e（原1d0a791a8）、CFG03 47881419c（原d8e6cacff），
CFG09最终hash见本报告最后提交。重放后的三个完整包与全树vet通过：
mosdns221.145s、rulefetch17.816s、yamlx9.977s（.64，高负载）；gofmt-l无输出。
`RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/mosdns/ internal/rulefetch/ internal/yamlx/)"; go test -count=1 ./internal/mosdns ./internal/rulefetch ./internal/yamlx; go vet ./...'`
最终gate按最新默认单slot前台排队，.64 07:47:04—08:02:04，独立900s watchdog
到限停止，退出124：`QUEUE LIMIT: stopped after 900 seconds without a gate slot; no gate stages ran.`
没有执行任何gate阶段，不标完整gate通过；按AGENT-BRIEF交协调者在合并树补跑。
实际gate调用：`RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back .ci-results/cfg-c14-gate -- python3 -c '<foreground 900s process-group watchdog>'`，
watchdog子命令为`bash server/scripts/gate.sh -p 6 FuzzRuleYAMLBoundary:./internal/rulefetch FuzzEditScalar:./internal/yamlx`。
日志在本worktree `.ci-results/cfg-c14-gate/gate.out`；CFG09单独和联合日志分别保留于
`.ci-results/cfg09/`、`.ci-results/cfg09-joint/`、`.ci-results/cfg09-joint-http/`。
联合临时worktree在取回日志并确认git干净后清理。

交回边界：三张工单的修复、回归与定向公开Python对照均完成；完整gate因队列未执行，
CFG01独立安全复核由协调者另派。CFG01四个实名Python边界缺陷/歧义仍须纳入该复核；
没有据此放宽其他比较、引入生产访问、push或启动子agent。
