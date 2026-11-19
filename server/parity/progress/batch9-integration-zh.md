# batch-9 集成（进行中）

基底 go@bcbb6e573；工作树 configflow-go-batch-9，分支 batch-9，不 push。
按 subs、F1 jsonstore、routers 顺序 rebase/FF 叠入，原分支引用不动；无 patch-equivalent 重复提交。
最终需要指定主测试服务器 preflight PASS、完整 compare PASS 和三个分配基准无回升。
完整 core 由协调者跑。测试、构建、格式检查均远端执行。

## 叠入与冲突

| 来源 | 原范围 | 组合范围 |
| --- | --- | --- |
| subs | d4fab6441..9be6977cb，18提交 | bcbb6e573..4e7d9ca901 |
| F1 | d4fab6441..2fac52248，2提交 | 4e7d9ca901..061a0b8d7b |
| routers | bcbb6e573..83b99d13a，5提交 | 061a0b8d7b..9f0dff6862 |

- Markdown 冲突两边追加均保留。
- httpx/n8_1_test.go：保留 batch-8 rule cache native digest，同时合入 subs hosts native digest，旧摘要仍在注释。
- refresh：保留已合入的 native scheduled.go，subs 的空 SubscriptionFetchError 中文业务提示移到现有 typed FailureReason；不恢复旧 Python-emulation 实现。
- convert/generation_bytes_test.go：同时保留 batch-8 provider path 精确参考和 subs 的9条非法 Agent version 精确错误；原 byte manifest 不改。
- 三份 MosDNS pyref 二进制：保留 batch-8 参考。incoming 早期 transcript 仅2863项，而现有为18040项；不会用旧子集覆盖现有完整参考。最后 live Python check 后仅刷新实际依赖 metadata，并核对全部非 metadata payload。
- F1 discovery/legacy.go diff 只有 SaveContext/UpdateContext 两行替换；batch-8 rules 接线不变。
- rulefetch、oxidns/adapter.go、mosdns/archive.go/ruleproxy.go 对基底 diff 为空；既有64MiB逐层上限、PeekCacheWithFormat 和 typed failures 均保留。

## 分配基准方案

Mihomo 使用原 BenchmarkMihomoGenerationProfile/128，OxiDNS 使用原 BenchmarkOxiDNSMetadataChain。
routers 新增跨旧/新树同样可编译的 BenchmarkBatch9SyncSources，经真实 loadSyncSources，1/16/64条规则、0/1MiB无关配置。
三个基准均固定 GOMAXPROCS=4、benchtime=3x、count=3；在独立只读基底副本测旧树后再测最终树。
首次在叠入工作树启动同步时源文件被 rebase 移走，rsync退出23；未运行任何基准，不计有效结果。已改在独立 detached 基底树完成测量。

基底 bcbb6e573：
- Mihomo128：64,577,874–64,610,077 B/op，611045–611046 allocs/op。
- OxiDNS unused0：42,808,066–42,874,776 B/op，470875–470908；unused2MiB：42,823,008–42,853,706 B/op，470855–470916。
- routers1：69,757–69,906 B/op，964–965；+1MiB：5,315,928–5,338,688 B/op，985–997。
- routers16：6,880,928–6,900,338 B/op，96011–96027；+1MiB：90,820,802–90,867,410 B/op，96207–96220。
- routers64：102,759,754–102,937,888 B/op，1416010–1416091；+1MiB：437,283,234–437,392,914 B/op，1415920–1415984。

剩余：定向编译/来源审计/安全与owner回归、三组最终基准、preflight、完整compare。

## 来源审计与定向集成检查

来源审计首轮发现两组旧测试仍声明已删除的 chains/compat/pyconv/yaml_reader；
按实际传递调用给 M73补20个新 native helper，两个组各删4个已删除路径，不改变任何
product/helper分类或进程豁免。审计0.853s通过。已保存18份现有 transcript 共18491项
完整非metadata payload摘要，后续先真实Python check再判断是否仅指纹需要更新。

定向非race：substore4.719s、subs0.326s、jsonstore0.013s、discovery3.212s、health/check0.013s、
refresh4.488s、httpx0.215s、routers1.311s，均通过；包含64MiB各解压层、独立子树alias预算、
F1 Context/取消提交、refresh空cause中文、两个digest和只读cache peek回归。
另用准确名称单独补 TestDiscoveryJSONStoreOwnerLifetime，避免正则无匹配被误计为通过。

## 最终产品分配（8f906a8b8，之后仅参考metadata/文档）

与基底相同主机、GOMAXPROCS=4、benchtime=3x、count=3：
- Mihomo128：45,837,090–45,873,045 B/op，410335–410337 allocs/op，明显下降。
- OxiDNS unused0：42,828,408–42,853,690 B/op，470875–470878；unused2MiB：42,821,896–42,850,034 B/op，470843–470867。
  B/op处于基底原波动范围，allocs无增加，原生DNS消费代码未发生改变。
- routers1：56,653–57,085 B/op，706–708；+1MiB：57,005–57,240 B/op，706–708。
- routers16：904,480–907,160 B/op，10793–10811；+1MiB：890,680–896,525 B/op，10780–10804。
- routers64：3,615,677–3,635,600 B/op，43120–43142；+1MiB：3,582,672–3,628,442 B/op，43076–43214。
  所有规模均下降，1MiB无关字段不再放大逐规则分配。

基准命令均 `go test -p 4 -run "^$" -bench <如下正则> -benchmem -benchtime=3x -count=3 <包>`：
`^BenchmarkMihomoGenerationProfile/128$` / internal/convert；
`^BenchmarkOxiDNSMetadataChain$` / internal/oxidns；
`^BenchmarkBatch9SyncSources$` / internal/routers。
准确名称的 discovery owner 回归通过，0.898s。

真实Python首次check：MosDNS所有功能对照0差异（47072 corpus行和5436 recorded调用），
OxiDNS全部边界0差异；routersplit5.720s通过。MosDNS57.873s/OxiDNS49.111s仅因12份
源码metadata变化失败，各条entries数量相同。随后显式record+check，且核对18份全部非metadata摘要。

显式record与再次check均通过：record MosDNS58.183s/OxiDNS50.413s/routersplit5.241s；
check MosDNS55.108s/OxiDNS47.457s/routersplit5.179s。18份、18491项的全部非metadata
payload SHA 完全相同，没有修改输入、输出或条数；只更新12份实际源码指纹。
最终产品无额外改动，继续最终preflight和完整compare。

## 首轮完整 preflight（9c51cbbfd）发现及修正

两种 tag vet 通过，full nonrace 完成，FAIL 范围如下；其余包通过，未进入 compare：
1. convert 旧单测和一条冻结 replay 仍期待 alpha-abc 通过。按 subs 已批准守卫更新单测；
   新独立 Python 参考先验证录制行1144原始输出，再仅注入同一版本守卫；完整 mutation 与
   原记录相同，只存 archive/row 摘要和错误。原 archive 不改。
2. byte gate 20行失败。只读基底与整合树实际生成逐行对照，仅两种指数 scalar 拼写：
   15处 number 1e+20→原输入1.0e+20，5处 text 1000.0→原输入1e3。
   原 native JSON 桥规范化拼写，新 native callbacks 保留原 YAML 词法。两者在实际
   yamlx 消费者中均为同一 float tag/数值。Python1.1 对1e3的字符串解析属于既有N7差异，
   独立 native corpus oracle 继续检查完整生成结果。新参考逐一绑定20条原行SHA、
   原 byte manifest SHA 和旧输出 SHA，执行真实 Python 完整生成确认原输出。仅反向改
   一个精确 scalar 的词法，yamlx.Equal 检查类型/数值相等，其余每个字节仍受旧 SHA约束；
   不重录原 manifest，不放宽整份 YAML 格式。
3. refresh 两条回放的完整错误文字和 cache/DB/health/task 文件全相同，只缺 native
   SubscriptionFetchError 的测试类型映射；仅给 replay adapter 增加 errors.As。
4. draft 在 Generate 更早观察到 ctx 取消后，将错误包装为中文 validation，丢失
   errors.Is(Canceled)。保留业务中文路径，单独透传 owner ctx 取消及错误链中的
   Canceled/DeadlineExceeded。原真实 Runtime 回归仍严格检查 DB/文件状态不变。
5. subs/substore 14处 runtime Clone 触发 facade gate。改用物理图 CopyGraph，不扩展
   alias、不截成 null；保留所有原 root/独立子树 Encode/ToJSON 预算检查，无架构豁免。

第一组定向：新 Python 参考20条完整生成与原输出一致；convert（旧断言/byte/全部recorded）
31.071s、yamlx facade0.596s、draft0.195s、refresh完整3150回放38.459s、subs alias0.038s、
substore alias0.015s，全部 PASS。完整包补验：subs26.879s、substore24.163s、
convert corpus16.919s、draft0.134s、discovery owner0.481s、yamlx facade0.527s，全部 PASS。
临时 byte-dump 仅保存于服务器私有缓存；临时 Go 诊断测试已从本地移除，不提交。
跨包 live check 仍在运行；产品 CopyGraph 修改后的三个最终基准与 preflight/compare 待完成。

CopyGraph 集成已单独提交。draft 的取消错误链修正另作一个提交；已有真实 Runtime
ShutdownBeforeRepositoryCommit 回归从红变绿，DB/文件状态原断言不变。

旧测试接线修正单独提交：不可解析版本的单测/精确1144回放、20条数值词法参考、
refresh native 错误类型映射。原 archive、原 byte manifest、原 cache/DB/文件期待均未改。

## 修正后最终复测（a84687ba7）

跨包第一次补验命令漏设 PYREF_MODE=check，触发 reuse 指纹拒绝，不计 live 通过。
显式 check 后，MosDNS52.953s所有功能0差异，仅5份 Clone→CopyGraph 源metadata需刷新；
OxiDNS45.283s、routersplit4.864s live通过。继续record/check与18份payload核对。

最终三组同条件分配，全部不高于上一批：
- Mihomo128：45,882,408–45,906,280 B/op，410321–410339 allocs/op。
- OxiDNS unused0：42,803,109–42,832,981 B/op，470837–470858；unused2MiB：42,815,821–42,845,592 B/op，470867–470908。
- routers1：56,717–57,010 B/op，707–708；+1MiB：56,845–57,298 B/op，706–709。
- routers16：901,842–908,704 B/op，10784–10799；+1MiB：891,394–935,986 B/op，10774–10804。
- routers64：3,595,570–3,635,904 B/op，43090–43136；+1MiB：3,595,522–3,607,296 B/op，43069–43191。

a84687ba7 的最终 metadata record/check 完成：record MosDNS59.460s/OxiDNS50.793s/
routersplit5.056s；check MosDNS58.305s/OxiDNS50.077s/routersplit5.928s。
再次验证18份18491项全部非metadata payload SHA与批次开始时相同，仅5份MosDNS元数据变更。
当前产品及测试修正已完整提交，开始最后preflight与完整compare，不再计此前失败轮次为通过。

## 路由器记录投影补充基准

除逐规则 source pipeline 外，单独测作者的 BenchmarkRouterProjection（1/16/64台router）。
旧基底无 routerProfiles helper，临时 test-only adapter 原样使用旧 ruleSyncRows 的投影顺序：
ListProfiles → pendingProfile 解码 → GetProfile → routersOf(mustPolicyJSON(config))；不含后续task构建。
相同 projectionRepository fixture、相同 BenchmarkRouterProjection 循环；未修改旧产品实现。
最终树直接运行已有原始 benchmark。均固定主测试服务器、GOMAXPROCS=4、benchtime=3x、count=3。

| Router数 | 基底0 padding B/op | 最终0 padding B/op | 基底1MiB padding B/op | 最终1MiB padding B/op |
| --- | --- | --- | --- | --- |
| 1 | 314072–317538 | 20840–21320 | 26583960–26587821 | 20840–21320 |
| 16 | 663549–668922 | 252256–252736 | 26898008–26901570 | 252368–254370 |
| 64 | 1712410–1714613 | 997856–1002736 | 31346320–31352797 | 993141–993869 |

最终 allocs/op：1台263–264（两种padding相同）；16台2457–2458/2457–2461；
64台9478–9490/9468–9469。全部低于基底，未发生无关配置按router重复复制。
命令：`go test -p 4 -run "^$" -bench "^BenchmarkRouterProjection$" -benchmem -benchtime=3x -count=3 ./internal/routers`。
基底4.864s、最终4.691s，均PASS。准备baseline helper时一次读取了不存在的相对路径，
随后第一次bench无匹配不计结果；修正为明确源路径后才得到上述实际6组×3轮测量。

## 第二轮预检与首次 compare

最终预检 af8366c97 返回 PREFLIGHT PASS。完整compare进入proddiff，observer自检PASS，
但proddiff new=97/stale=4、Surge/Loon生成覆盖回退，尚未进入difftest/synthetic。
没有增加baseline。诊断只在服务器私有目录，报告不复制快照字段值。

根因1：生产 NativeEnv 的单节点转换器新接口会返回Sub-Store转换失败，旧接口返回nil节点。
Surge/Loon预检只是可选的节点探测，普通转换不可用不应中断整份生成。修正生产composition
adapter：先Detach保留Runtime，普通转换失败仍无节点；来源owner取消仍返回ctx错误。
保留typed Client的错误契约，其他消费者不变。loopback HTTP503新回归在修复前两个引擎
真实失败，修复后正常生成，原shutdown/cache回归也通过。不会把转换URL/凭据带进响应。
root作用域只修NativeEnv适配，不给任意生成错误加忽略规则。

根因2：dependencies.update.finished_at 的idle值经native float64→JSON→legacy Object变成
整数token 0，原HTTP为0.0。只在现有legacy HTTP codec显式保留float标记，native观察值仍为
float64；GetUpdateState/GetSubStoreStatus/ListDependencies共用同一窄边界处理。
新增真实HTTP raw JSON token断言修复前失败、修复后通过；不归一化时间，不加baseline。
整包api/settings12.264s、mihomoenv0.221s通过；精确GetSubStoreStatus0.284s、native失败/
shutdown回归0.247s、source audit0.810s通过，gofmt全树通过。
第一次复核命令中的TestGetUpdateState实际无匹配，不作为证据；HTTP raw token测试与
GetSubStoreStatus为实际运行的正控。继续先定位验证proddiff，再最终preflight+完整compare。
