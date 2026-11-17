# CFG harness 工单修复

分支`go-fix-cfg-harness`，起点`go@1439e503d`。仅tools/测试证据及文档；业务代码不改，不推送，不派子agent。范围：CFG-05、10、04；CFG-08等待协调者通知C33合入后复验。

## 第一批：比较器及明确的未实现依赖

- CFG-05：fake DAV快照仅对两边均为纯allprop语义的PROPFIND正文作等价比较；其余快照完整比较。DTD/PI/注释、namespace、属性选择、额外文本、属性、非法XML/超长正文及方法/路径/认证/Depth/远端对象/请求数都保留负控。原始观测不被修改。
- CFG-10：所有CONNECT即使未开record_wire也记录request line/Host/代理认证。比较器先验证authority、准确目标、HTTP/1.0或1.1及Host一致性，再去除版本/派生Host表示的差异；仅消除已观察到的空UA→Go-http-client/1.1默认UA。未知UA、错Host/目标/版本/认证、正文、额外字段、顺序/次数与非CONNECT头仍报错。
- CFG-04：两条请求显式pending_dependency，仍跑Python catalog/guard及每端自身无写入前提；Go不发送请求，不把源端不完整binding引起的后台失败当作规则回归或有效DNS生命周期oracle。A17及完整fixture就绪后人工恢复；不关闭任何后台loop，不屏蔽agents/通知/SQL/files。blocked原因写入finding_reviews/Markdown，恢复后不会继承旧blocked理由。
- 新增三类正控/负控，首次.64自检153项10.831秒OK；随后补充“defer之后普通请求的SQL/files差异仍能检出”负控，下一轮自检与定向复验验证最终代码。尚未把任何旧finding标closed。

```text
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server/tools/configdiff && ~/cf-test/.venv/bin/python -m unittest -q test_compare'
Ran 153 tests in 10.831s
OK
```

## 第二批：真实定向复验与finding归因

- .64固定Go源码基线1439e503d，6组43个实际变体126.01秒，compared41 / go-unimplemented-dependency2 / different0，design_errors=[]，exit0。标准runner开头154项自检5.624秒OK，实际故意破坏Go body.authEnabled已被检出。
- CFG-05的10条与CFG-10的3条全部真实双端相比关闭；13项归因2896b8d9e，并明确是测试比较器修正，不是Go业务修复。原始历史差异保留；归因只匹配有限case/group/route/variant和原始差异路径，open/blocked、额外SQL或其他身份不会获得此归因，另有自检。
- CFG-04两项真实执行Python catalog/guard及自身无写入检查，Go未发送请求/未相比；记录go-unimplemented-dependency，finding保持blocked并显示具体A17/完整binding恢复前提。不是closed，也不算规则接口回归或Go通过。
- 当前全局10666变体：compared5767、Go未实现4476、原始different309（accepted257/open52）、未实现依赖113、O12单列1。reviews closed282/accepted257/open52/blocked2/separate1。未重跑的其他工单保持原基线与状态，不宣称被本修复关闭。
- CFG-08尚待协调者C33合入通知；2条absolute-query仍保留open及a08c4cc27原证据。本批没有重跑该组或将502批准为安全差异。接到通知后再rebase、实跑rule-proxy-local-cache并给最终判定。

```text
RT_HOST=sunbroke@192.168.0.64 rt --back server/tools/configdiff/testdata/results.json --back server/parity/difftest-config-findings.md -- bash -lc 'server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups settings-webdav,webdav-local-day-retention-boundary,webdav-named-preflight-and-directory-failure,rule-fetch-exhausted-candidates,rule-proxy-transport,rule-dns-extra-guard-branches --baseline 1439e503d'
Ran 154 tests in 5.624s
OK
MUTATION-CHECK detected deliberately corrupted Go body.authEnabled
R188-S01 real-DNS-follow-binding-catalog: go-unimplemented-dependency
H911 actual-public-catalog-key-is-bound-to-DNS-follow: go-unimplemented-dependency
groups6 / variants43 / compared41 / dependency2 / different0 / design_errors=[] / seconds126.01 / exit0
```

最终补充：CONNECT authority由标准urlsplit解析，保留精确目标字节，只把Host与目标做语义一致性检查；补了合法IPv6、带scope的IPv6及旧式下划线主机名正控，错误scope负控。避免手写DNS字符白名单把合法CONNECT当成错误。最终代码再跑全部154项自检及CONNECT两组3变体，compared3/different0、design_errors=[]、15.85秒exit0；全局finding状态未变。最终覆盖索引更新通过：228路由、10666变体、R/H5428、unaccounted/unknown/missing S-F均空，T observed22/partial-mapped8且无遗漏。所有命令均已结束，没有后台测试等待。

```text
RT_HOST=sunbroke@192.168.0.64 rt --back server/tools/configdiff/testdata/results.json --back server/parity/difftest-config-findings.md -- bash -lc 'server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-fetch-exhausted-candidates,rule-proxy-transport --baseline 1439e503d'
Ran 154 tests in 4.995s
OK
H123: compared, diffs0
R087-S91: compared, diffs0
R138-S91: compared, diffs0
current compared3 / different0 / design_errors=[] / seconds15.85 / exit0
```
