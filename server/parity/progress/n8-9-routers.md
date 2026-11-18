# N8-9 routers / routersplit S pass

状态：S pass本体完成，Opus HR **verified**，F1–F9全部关闭；作者本组定向race/HTTP、fuzz、gofmt/vet和审计均通过。**依赖 batch-8 集成，跨包 corpus 待 batch-8 合入后复验**。分支 `go-n8-9-routers`。
起点 `go@d4fab6441`；按协调者授权叠在 rules 最终复核基点 `9989da440`。
本组产品提交：`c8b4ab7cc`、`117cf39c0`、`434cacd69`，最终验收提交见分支 HEAD。
不 push、不跑完整门禁。作者所有测试均固定 `.32`。

## 交付与保留边界

- routersplit 删除 `pyhelpers.go`；Profile/Marker/Context/Session/SplitState 与 yaml.Node 为业务边界，删除 Python str/list/dict/hash/异常模拟。原 N7 YAML oracle 保留。`PythonClass` 只在测试中标注冻结答案；产品错误使用原生 ValidationError 或 RouterSplitError。
- routers 存储接共享 `model.Router`、`model.String`/`model.Int`；窄运行时投影保留原始成员及显式编辑意图。未知字段添加/修改用 Fields.Set，删除用 Fields.Delete/Clear；无关编辑保留错型、缺失成员、大整数及所有权证据，不物化运行时默认。
- 管理读写接 repo.Config/UpdateProfile，设置 CAS 接唯一 SaveRouterSettingsJSON；通知接 native Inputs/SaveRouter，task slot 接 SetRowBuilder。定时扫描只读路由器子树。repo.SaveRouterSettings 的旧签名仅做 codec。
- rules 正式接 SourceConfig/ValidateRemote/CacheReadPath/WriteCache/DownloadRequest，已删除 `legacy_rulefetch.go` 和 `syncTransportKey`。一次操作建立一次不可变配置环境，读取、下载及最终写缓存循环共用；transport 摘要只算一次，逐源缓存键只拼摘要和 URL。
- 生成缓存读取是有界、UTF-8 校验的只读 peek，不能迁移、触碰 mtime 或 DB/revision。授权基点尚无 PeekCacheWithFormat，故用 typed CacheReadPath 加只读文件读取实现相同语义；batch-8 提供正式入口后以 owner 为准。
- 生产 jsonx 只余 routersplit/legacy.go、routers/legacy_wire.go 的旧调用者 codec，以及协调者明确暂留的 management_repo.go:saveWorkspacePolicies OxiDNS 转码。后者对 `d4fab6441` 逐字不变；没有抢改 DNS owner 区段。

## 常设问题类

1. **超时**：未替换生产 driver/download 的真实 timeout 参数；下载 5s/10s/30s 合同由 R14 原回归验证。
2. **设备字节**：驱动编码未修改。R04–R17 原始 HTTP/Python 边界继续比较 body/header、GET/DELETE 无 body、批命令、回读与补偿；新增下载服务也验证 GET 和空原始 body。
3. **凭据**：日志、错误继续统一 internal/redact，没有第二套 scrub，也不把用户名当密码。导出脱敏仍按既有出口合同。
4. **锁及关机**：RouterJobLock 是唯一 L1，只负责 try-lock 与拿锁后的 shutdown 检查，busy 优先级不变。身份校验在实际打开设备/Inspect/写设备边界，坏存储仍能修复或删除。Detach 后各提交点保留 ctx/work.Err 检查，事务叶锁及锁顺序不变。
5. **外部输入**：RouterOS/iKuai 的 Row.text/flag/number 厂商字符串、数值兼容保留，函数注释说明外部设备输入理由；用户身份及选中规则 ID 列表严格检查原 token，数值不能制造有效身份。
6. **分配**：真实逐规则读/下载/写缓存路径和真实路由器投影均有分配保护，不以只测 helper 替代业务循环。见下文数字及负控。

## R5 / D2 与独立期望

原 r01/r02/r03 `.jsonl.gz` 输入及旧答案均未修改；普通行仍用原比较器，不放宽整个类别。
新 sidecar 由真实 Python 加明确的 typed-input 政策独立生成，不读 Go 输出；每条包含原输入 SHA256、完整答案，拒绝重复和过期期望。

- R01 固定10000行，725行独立期望。请求 interval 仍严格 JSON 整数，字符串及60.0均拒绝；存储 interval 用共享 model.Int。身份/凭据须文本，verify_tls 须 bool；name 共享 model.String。选择列表 false/0/空字符串从 Python 的空列表改为400，已经登记 behaviour-changes。
- R02 三份 routersplit 调用共10918条（recorded2595、corpus6000、edge2323），2553行独立期望。原 gz 总13641行另含 ippresence2723，分母没有减少。
- marker 只接受原始整数 token1；实际使用的非文本身份用 RouterSplitError/ROUTER_SPLIT_INVALID/409。非文本可选链接视为无链接，marker 不受无关拓扑影响。只校验最新未放弃且实际消费的 session ID；安装证明只接受非空文本。
- `completed=1` 的原测试输入和 Python 旧答案保留，另有 session fixture 同时绑定旧答案和 R5 bool-only 答案。Python `_router_ids` 空 ID/数字相等制造关系的缺陷已登记。
- pyref 三份快照仅刷新独立 recorder/source 元数据；原语料内容不改。routersplit n89 参考脚本/sidecar 已列 sources；静态 session fixture由测试直接逐值绑定。
- 本组不新增 D2 设备身份或缓存签名变化；通知沿 N8-7 native RouterDigest，规则控制摘要沿 R14 canonicalReleaseJSON。

## HR 独立复核

报告：`server/parity/review-n8-9-routers.md`。Opus5.5/high 最终只读确认产品树 `434cacd69` 无遗留发现，同意作者收尾检查通过后标为 verified。

| 问题类 | 修复及防线 |
| --- | --- |
| F1 坏记录锁死 | 锁不校验身份；设备边界拒绝，HTTP回归固定 PUT修复/DELETE200、probe/routes400。 |
| F2 范围外拓扑拒绝 | marker跳过旧拓扑；可选非文本链接无关联，只校验匹配且消费的身份；独立sidecar重新由Python生成。 |
| F3 坏列表被写空 | 未被payload修正的坏列表返回400，完整记录及revision不变；显式修正允许保存。 |
| F4 未知字段不能修改 | Fields.Set/Delete 与成员 Clear；回归检查增改删及原对象元数据不变。 |
| F5 请求整数被放宽 | 恢复 JSON整数请求类型，严格区分存储codec；sidecar保留真实Python请求结果。 |
| F6 逐规则重复解码 | 正式typed规则接口、一次环境/摘要、真实loadSyncSources及writeSyncSources分配预算，隐藏环境的负控必须超限。 |
| F7 数值伪身份 | session/installation/pending/returnCleanup/IPv6链接看原文本token；GET routes也先校验。 |
| F8 期望/元数据 | 完整class/code/status、独立session fixture、source登记和Python bug说明。 |
| F9 坏guard触发撤除 | 原enabled或所有权state存在时拒绝坏guard，不按关闭规划撤除；保守范围包含无关设备操作，已登记。 |

首轮 reviewer 自行在 `.64` 独立目录做过负控，违反本任务仅 `.32` 的限制；这些结果不计本组验收。
作者已将关键负控固化后在 `.32` 验证。此后复核均只读，没有重复跑测试或调用 rt。
低严重度已记录：坏存储多字段同时出错时首条消息顺序可与 Python 不同；设置环境单次分配约为大成员的14倍，但不随规则数重复。

## 分配与只读缓存证据（.32）

- cache模式，额外1MiB无关shared/config，1规则57365/56277 B/op、16规则898508/882945 B/op；不逐规则复制无关仓储配置。
- 真HTTP下载16规则、额外256KiB settings：native 1739188/5439882 B/op，增3700694B；隐藏环境负控6528080/69906437 B/op，增63378357B。相同6MiB预算正控通过、负控超限，证明能抓实际逐规则重建。
- 真实writeSyncSources循环race：16规则2118706/5813900 B/op，增3695194B，低于6MiB。
- legacy peek回归验证完整内容、原文件字节/mtime、DB revision均不变且native文件不存在；与R15真实Flask同步测试一起race通过（routers1.587s、api3.473s）。
- BenchmarkRouterProjection 与 BenchmarkSyncSourcePipeline 覆盖1/16/64项、0/1MiB padding，100次/项：

| 实际路径 | 项数 | 0 padding B/op | 1MiB padding B/op |
| --- | ---: | ---: | ---: |
| routerProfiles | 1 | 21366 | 20961 |
| routerProfiles | 16 | 253493 | 252682 |
| routerProfiles | 64 | 996770 | 993646 |
| loadSyncSources | 1 | 58078 | 56932 |
| loadSyncSources | 16 | 907850 | 895792 |
| loadSyncSources | 64 | 3647091 | 3595792 |

无关配置大小没有成为每台/每条循环的额外分配成本；settings的大成员另由上面的正负控守住。

## 最终定向验证（全部固定 .32）

公共前缀：`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 PYREF_MODE=check && <command>'`。
没有运行完整 gate、全库race或proddiff。

1. `go test -race -p 4 -count=1 -timeout=20m ./internal/routers ./internal/routersplit ./internal/api/routers -skip "Corpus|Reference|Golden|RecordedPython"`
   最后输出：routers **439.625s**、routersplit **9.467s**、api/routers **50.967s**，全部ok。覆盖R06–R17的HTTP/Python及原始设备请求、Runtime/关机回归；R04/R05由下一项跨包Corpus执行。
2. `go test -race -p 4 -count=1 -timeout=12m ./internal/clients ./internal/agents/ipv6 ./internal/repo ./internal/deploy ./internal/gateway/draft ./internal/setup ./internal/routersplit -run "TestN89|Test.*Router|Test.*Runtime|Test.*Readiness|Test.*Fixed|Test.*FullTun|Test.*Takeover|TestSaveRouter" -skip "Corpus|Reference|Golden|RecordedPython"`
   clients3.738s、agents/ipv6 5.413s、repo21.412s、deploy无匹配、gateway/draft4.410s、setup10.131s、routersplit1.088s，全部ok；包括最终新增的身份负控。
3. `go test -p 4 -count=1 -timeout=20m -run "Corpus|Reference|Golden" ./internal/...`：本组routers67.379s、routersplit1.786s通过（含R04/R05 HTTP）。跨包失败：ippresence的classical_ip_lines在rules@9989da440旧缓存路径237/2000 corpus、30/199 edge返回unknown；MosDNS47072行0 differ，但TestCorpus快照还记录旧ruleutils.go哈希（2ab905→18a96b）。协调者确认属于batch-8依赖问题，本组不修：23c83a970补stored profile缓存声明，ef915df6e新增PeekCache只读入口，dead8fcdb对齐参考依赖。**依赖 batch-8 集成，跨包 corpus 待 batch-8 合入后复验**；不会改原corpus期望掩盖失败。
4. `go test -p 4 -run "^$" -fuzz "^FuzzN89StoredRouter$" -fuzztime=60s -parallel=4 ./internal/routers`：PASS，912472次，61.016s。
   `go test -p 4 -run "^$" -fuzz "^FuzzRouterSplitYAML$" -fuzztime=60s -parallel=4 ./internal/routersplit`：PASS，42684次，61.031s。
5. `gofmt -l .` 无输出；`go vet -p 4 ./...` 通过；`go test -p 4 -count=1 ./tools/pyref -run "^TestAuditRepository$"` 最后输出 `ok configflow/tools/pyref 0.543s`。
6. `go test -p 4 -run "^$" -bench "BenchmarkRouterProjection|BenchmarkSyncSourcePipeline" -benchtime=100x -benchmem ./internal/routers`：PASS，最后 `ok configflow/internal/routers 6.637s`；B/op见上表。

## 迭代发现及集成责任

初次race暴露CAS显式默认、坏选择列表/guard和probe保真问题，均修产品代码，原断言保留；新peek fixture曾缺子目录或误把文档网段期望计作IP，修的是新测试自身，没有改冻结答案。
一次早期cross-corpus被排队rt同步改写远端oracle/metadata，属于无效执行不计PASS；最终按一条rt完成后再同步下一条执行。

本分支已按授权叠rules@9989da440；交回前已执行 `git rebase go`（up to date）。本地go仍为d4fab6441，协调者明确先交回本组，batch-8（尚无最终tip）合入go后再通知本组rebase及复验跨包corpus，重复rules patch自动跳过。
DNS的saveWorkspacePolicies区段以及新增PeekCacheWithFormat所有权以batch-8为准。
