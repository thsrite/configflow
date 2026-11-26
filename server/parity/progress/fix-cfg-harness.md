# CFG harness 工单修复

CFG-05/10/04分支`go-fix-cfg-harness`，起点`go@1439e503d`；后续CFG-08分支`go-fix-cfg-08`，起点`go@9c88c4390`。仅tools/测试证据及文档；业务代码不改，不推送，不派子agent。CFG-05/10已实跑关闭13条；CFG-04保持A17依赖未实现；CFG-08现已在C33合入后实跑关闭2条。以下保留各批次当时的等待状态。

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

## 第三批：C33合入后CFG-08最终复验

- 接协调者C33已合入通知，从最新go9c88c4390新建go-fix-cfg-08。标准runner在.64真实启动Flask及公开Go入口，原有fixture/比较器均未改。完整rule-proxy-local-cache组20变体140.10秒全部compared，different0、design_errors=[]、exit0；开头154项自检7.578秒OK，并检出故意破坏的Go响应。
- R087-B91与R138-B91的absolute-query变体均Python200/Go200，正文严格为`full:inline.example`；完整稳定headers/SQLite/files无差异，external fake请求数两端均0。相邻的跨profile相对URL、带query的相对URL和禁用library引用仍按原预期拒绝，未为本次复验放宽安全边界。
- **最终判定：CFG-08 closed。** 先前Go502是C33公共content路由未接通的依赖前提问题；现在正常派发成功，不是“Go拒绝Python安全漏洞”，不需要新增Go更正确/行为变化批准。
- 关闭归因4786d7f86（C33 shared-source/content路由及server接线），实跑集成基线9c88c4390。只补有限两条identity、原差异路径及实际200/200/closed的自动归因，避免后续refresh丢失修复来源；没有改比较器、请求fixture或业务实现。归因负控覆盖open/blocked、错误route/group/variant/status及额外SQL路径。
- 本批报告reviews closed284/accepted257/open50/blocked2/separate1；仅CFG-08两条从open关闭，其他工单不改判定。canonical保留未重跑组的旧基线，历史Go未实现计数不代表C33合入后所有路由仍未接通；本次不宣称完整config全量复验。

| 用例 / variant | 路由 | Python / Go | 正文 / 外部fake / 存储 | 判定 |
|---|---|---|---|---|
| R087-B91 / absolute-query-dispatches-internal-content-endpoint | GET /api/mosdns/rule-proxy | 200 / 200 | full:inline.example；0 / 0；无差异 | closed，4786d7f86 |
| R138-B91 / absolute-query-dispatches-internal-content-endpoint | GET /api/profiles/\<profile_id\>/mosdns/rule-proxy | 200 / 200 | full:inline.example；0 / 0；无差异 | closed，4786d7f86 |

```text
RT_HOST=sunbroke@192.168.0.64 rt --back server/tools/configdiff/testdata/results.json --back server/parity/difftest-config-findings.md -- bash -lc 'server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-proxy-local-cache --baseline 9c88c4390'
R087-B91 absolute-query-dispatches-internal-content-endpoint: compared, diffs0
R138-B91 absolute-query-dispatches-internal-content-endpoint: compared, diffs0
groups1 / variants20 / compared20 / different0 / design_errors=[] / seconds140.10 / exit0
```

关闭归因补齐后，.64全部155项自检10.909秒OK；refresh确认两项closed均带4786d7f86，原始10666个实际case verdict未由注释刷新修改。canonical覆盖索引再次通过，228路由、R/H5428、未知编号/遗漏/缺S-F/运行夹具错误均空。只更新本次两条关闭证据与自动归因，不更改任何HTTP、协议、SQL/files比较条件。
