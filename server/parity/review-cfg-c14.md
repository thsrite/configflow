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
对方CFG02提交9fe6a8512817231ba066dc3014cb8bfe7806cf47已实跑20compared+2H133不同，
正是尚缺本票shared判断。将提供本提交hash后由对方临时联合分支跑rule-delivery-yaml-helpers，
H133必须两票同时验原文与日志后才关闭；A12展示扫描不计本票覆盖。

复现：
`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_cfg03.py'`
`RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server; go test -count=1 ./internal/rulefetch ./internal/yamlx; go vet ./...'`

## CFG-09 / P2 / C28（待做）

已先向6e5c7760确认：O3-6c的RecordContext/ForgetContext在最终rename前检查ctx，
仍直接返回native *os.LinkError/*os.PathError，refresh不改错误类型。可在go1439上
只改分类/HTTP，不复制生命周期实现；待6c合入后联验关机不写receipt、取消不误归URL、
请求断开仍完成。6c在途cf14fe3c2，接口提交7a9b43e98（可能随rebase变）。
