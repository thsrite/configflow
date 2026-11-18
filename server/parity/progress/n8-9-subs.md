# N8-9 subs/substore 交回

状态：移植完成，定向验证全绿，待独立复核及批次门禁。进入 batch-9，不在 batch-8 内。

- worktree：`~/.paseo/worktrees/302rx1dg/go-n8-9-subs`
- 分支：`go-n8-9-subs`
- 产品提交截止：`910e9f57340c1952863278344b680ff4d75ba6a9`
- 初始基于 go@03257978f，已 rebase 到 batch-7 合入后的 go@d4fab6441；交回前再次
  `git rebase go`，结果 up to date。报告收尾提交仅改本文档。
- 全部测试/构建/静态检查均通过 rt 在 `sunbroke@192.168.0.32` 执行；GOMAXPROCS=4、-p4。
  不跑完整门禁、不用.64、不碰生产/真实设备、不push、不起子agent。

## 改动与边界

1. subs 的 URI/YAML解析、过滤、监听器、hosts、DNS、外部dialer元数据、链降级和provider
   复制迁到 model/DTO/yamlx。删除私有 compat.go、pyconv.go、旧chains.go、yaml_reader.go。
   依赖校验/图/引用/过滤使用 convert 唯一 typed owner，经 SharedDialers 接线，不保留第二套业务。
2. substore 的 Cache/CacheMetadata/Traffic、FetchResult、runtime状态、release/update状态、
   HTTP协议与依赖更新均typed。删除pystr.go；旧API只在legacy*.go转码。持久化使用typed
   repo边界，新增ReadSharedBytes/RemoveSharedFile；原叶锁、原子替换、tmp清理和错误链保留。
3. 字符串/整数/布尔读法使用共享model规则；Node.Name数字文本兼容，实际使用的身份字段严格。
   开放Params保留原值/tag；错误token及FieldIssue不因重建记录丢失。未知存储字段和大整数保留。
4. 配置只在旧入口边界投影一次，不在节点/订阅循环重解完整配置。hosts索引、源缓存与正则
   分批构建/读取；链降级COW，不修改输入。ProxyGroup.SetJSON只复用共享record codec，
   保留显式null/空列表/新增字段顺序，不动未知字段。
5. 写owner均保留Detach后的关机取消；入口/拿锁后/提交点前检查ctx.Err。真实Runtime回归覆盖
   cache保存/删除/清理、实际repo叶锁、state的close/rename、bundle/VERSION/重启、请求断开与关机。
   没有新增Uninterruptible例外，没有关机后补偿写。
6. 私有state.json使用原生JSON framing；已审计只有Go/Python面板按JSON解析读取该hashed
   scratch文件，Sub-Store运行时与安装脚本不按字节读它。真实Python原样旧格式输入读取测试
   包含空格/ASCII Unicode转义/未知大整数，读取不改写文件。

convert产品文件仅为接线：legacy.go、subscription_nodes.go、subscription_dialers.go、
subscription_environment.go、mihomoenv/{legacy,native}.go；另按协调者裁定修改easytier.go与
messages.go的Agent版本门禁。没有修改Mihomo/Surge/Loon生成算法。生产Converter和CacheReader
直接接typed substore，Runtime关机错误不再经旧无error签名丢失。其他owner的rules/check/
provider/refresh旧回调仍为其原实现薄codec，不在本任务复制或提前迁移其业务。

## 保留的兼容与获批变化

详见 [behaviour ledger](../behaviour-changes.md) 的 N8-9 各节。

- 外部订阅Base64：decodeSubscriptionBase64忽略既有ASCII垃圾/空白，按原padding规则停止，
  非ASCII及不完整末尾仍失败。用encoding/base64加命名清楚的预处理；2500原语料期待不改。
- 外部机场group type：airportGroupType只收select/url-test/fallback/load-balance，其他值
  包括数字均fallback select。用户存储的组type与Node.Type仍严格。
- 已批准R5/原生边界按原输入逐行固定期待：filter 9、parser 795、hosts 88、dialers 11、
  DNS 2、chains/shared helpers 656；原archive、N7原资料及比较函数保留。真实Python负责
  业务答案，只注入已批准的读视图/规则；source/row/相关loader SHA绑定。未扩展输入矩阵。
- 缓存数字ID有真实Python完整精确参照；曾修改一个期待删除项运行原测试，确实失败，随后恢复。
  runtime有128条真实Python参照及实际输出负控；parser也做过完整输出负控。
- 已批准删除CPython内部报错/私有framing；固定中文业务前缀、有效字段/原始存储/顺序/
  命令/副作用不放宽。坏引用fail-closed；未知entries/dialers不制造可用空集合。
- Python bug修复：重复子组DAG每target每组只访问一次，保留首次叶顺序/上限；未知或空的
  relay成员不生成伪成功空组；DNS坏filter不被当成空白名单覆盖。
- Agent版本新裁定：仅Agent路径，非空但无法识别的机器版本明确拒绝，提示升级/重启Agent；
  空/null/未上报保持unknown路径。低层4条版本、22条私有type错误和完整生成13条均精确绑定。
  完整生成期待由真实Python生成器仅注入该守卫，比较完整err/report/profile状态。
- 原4548项字节manifest未改：9项alpha机器版本逐项断言原SHA、完整错误及空输出；其余4539项
  仍逐字节比较。覆盖计数仍Mihomo2353（2344字节+9明确拒绝）、Loon1064、Surge1131；
  8c原例外不变，没有整体比较器豁免或用Go失败输出重录期待。
- 原生临时目录边界已获协调者批准：按TMPDIR/TEMP/TMP首个非空路径选定绝对路径，否则
  os.TempDir；不探测写文件、不静默回退。首次使用校验存在/目录/权限/ACL/只读挂载，
  RuntimeDirectoryError包含环境变量、路径和中文原因并保留IO cause；启动Wire发告警。
  六条不存在/不可写×三个变量回归检查Snapshot/Use、告警、零命令/零文件写入。

## 边界检查找到并修复的问题

| 问题 | 处理与证据 |
|---|---|
| 重建节点丢失server/port错型证据 | 保留原token/FieldIssue，8c原306条边界与分类索引原样通过 |
| 投影整个provider根，误拒无关复杂键 | 仅投影消费的proxies子树；原provider语料3条及新回归通过 |
| 无cause的SubscriptionFetchError丢业务名 | 保持“更新失败（SubscriptionFetchError）”；原refresh语料与新回归通过 |
| 旧filter codec复制节点，聚合缓存少enabled | 映射native选择回原对象，保留重复引用/顺序/未知字段；原HTTP状态/副作用断言不改，修复前失败、修复后通过 |
| 源清单指向被删除的subs文件 | canonical建议/审计更新三个mosdns依赖组；真实Python重录并check，2863/0/5个exact entries及输入/venv hash完全不变，只刷新源码metadata |

后三项是迁移引入的遗漏，已修复，未将它们登记为Python缺陷。版本与临时目录政策为协调者
明确裁定，均在ledger中列出；没有待决定的行为边界或已知功能缺口。

## 分配量与输入不变

所有基准均远端执行，ReportAllocs，非墙钟阈值。各管线另带1MiB无关配置。

| 基准 | 规模 | B/op | allocs/op |
|---|---|---|---|
| CacheRecord | 32/256/2048节点 | 787768/6007016/47830936 | 10090/78970/631010 |
| DNSHostIndex | 32/256/2048域名 | 69368/281368/2328064 | 308/902/6340 |
| HostGeneration | 32/256/1024节点 | 66304/526016/1957720 | 348/2188/8399 |
| DialerGeneration | 8/32/128源 | 254344/920632/3647664 | 2098/8173/32367 |
| ChainGeneration | 8/32/128节点 | 435840/1231176/4559128 | 5245/15316/56151 |
| MihomoSubscriptionPipeline（512规则） | 32/128节点 | 34418752/80579208 | 370726/861075 |

输入四倍时完整生成分配约2.34倍。实际B/op回归分别限制hosts/dialer/chain引入1MiB无关
配置的增量≤32KiB；完整生成16节点的预算为基线2%+64KiB（缓冲池/race采样有噪声），
仍小于一次1MiB解码，远低于逐节点16MiB。Runtime模式读取0/64KiB/1MiB无关配置为
5320/5800/5800 B/op。原输入不变、源缓存不变、alias不外溢和失败保留成功前缀均有回归。

## 最终验证

以下均使用前缀：
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 && …'`。
所有长命令前台跟到完成；没有gate或全仓race。

1. 受影响包race：subs17.892s、substore44.979s、model12.912s；convert跳过重复大语料矩阵后
   51.289s。最终runtime/环境变更后的完整substore非语料race55.463s；新增生成参照race6.533s、
   环境目录race2.547s、生产接线race2.037s、filter身份race1.199s。
   - `go test -race -p 4 -count=1 -timeout=8m ./internal/subs ./internal/substore ./internal/model ./internal/convert -skip "Corpus|OriginalInputsUnchanged|Reference|Golden"`
     前三包通过；convert误带全套生成测试后作者停止，不计通过。
   - `go test -race -p 4 -count=1 -timeout=8m ./internal/convert -skip "Corpus|OriginalInputsUnchanged|Reference|Golden|RecordedPython|GenerationBytes"`
   - `go test -race -p 4 -count=1 -timeout=8m ./internal/substore -skip "Corpus|Reference|Golden"`
   - 最后fixture/facade/接线改动各自的定向race均通过，未重复整套参考矩阵。
2. 直接调用方race：
   `go test -race -p 4 -count=1 -timeout=12m ./internal/configgen ./internal/provider ./internal/refresh ./internal/convert/mihomoenv ./internal/api/subscriptions ./internal/api/aggregations ./internal/aggregations ./internal/api/proxygroups ./internal/api/settings ./internal/api/nodes ./internal/health ./internal/discovery ./internal/setup ./internal/deploy ./internal/repo ./internal/httpx -run "N89|SubStore|Subscription|Source|Cache|Mihomo|Chain|Dialer|Runtime|Wire|Provider|NativeSharedDelete" -skip "Corpus|OriginalInputsUnchanged|Reference|Golden|RecordedPython|GenerationBytes"`
   全绿；个别包无匹配单测只编译，其HTTP路径另由下面对照覆盖。关键尾行：provider12.027s、
   refresh9.454s、configgen3.373s、deploy36.649s、repo22.686s、httpx3.575s。
3. 最终全跨包：
   `go test -p 4 -count=1 -timeout=20m -run "Corpus|Reference|Golden" ./internal/...`
   全绿。尾行摘要：convert82.400s、subs14.636s、substore0.242s、provider23.414s、
   refresh2.493s、mosdns6.767s、routers63.408s，其余全部ok/无匹配测试。
4. 最终真实HTTP对照：
   `CONFIGFLOW_DIFF=1 go test -p 4 -count=1 -timeout=20m ./internal/provider/difftest ./internal/api/subscriptions/difftest ./internal/api/aggregations/difftest ./internal/subs/difftest ./internal/substore/difftest ./internal/refresh/difftest`
   六组全绿：51.318s / 101.043s / 42.306s / 8.853s / 8.667s / 5.054s。
   原聚合缓存enabled副作用断言、原始请求字节、文件与有效命令期待保留。
5. 最终fuzz（逐项60s、parallel4）：
   - `go test -p 4 -run "^$" -fuzz "^FuzzN89CacheRecord$" -fuzztime=60s -parallel=4 ./internal/substore`
     → PASS，61.010s，1294328 execs。
   - `go test -p 4 -run "^$" -fuzz "^FuzzProxyYAMLReaders$" -fuzztime=60s -parallel=4 ./internal/substore`
     → PASS，61.019s，233693 execs。
   - `go test -p 4 -run "^$" -fuzz "^FuzzSubscriptionYAMLReaders$" -fuzztime=60s -parallel=4 ./internal/subs`
     → PASS，61.018s，125767 execs。
6. `go vet -p 4 ./...`、`go vet -p 4 -tags routersplit ./...`、
   `test -z "$(gofmt -l internal tools)"` 均退出0、无输出。
   `go test -p 4 -count=1 ./tools/pyref -run "^TestAuditRepository$"` →0.540s。
7. MosDNS源码metadata刷新：同三个测试组先`PYREF_MODE=record PYREF_ACCEPT=1`再
   `PYREF_MODE=check`，最终48.621s/48.329s。与git原文件逐项比较，entries、version、
   declared input、venv依赖完全相同；未改Python业务答案。0条既有占位组不算新增覆盖。

最终检查在产品树910e9f573的全部内容上执行（提交前同一working tree）；本报告随后只做
文档收尾。完整批次门禁、独立复核由协调者安排。
