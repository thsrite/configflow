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
