# batch-9 集成交接

开工基底 go@bcbb6e573；工作树 configflow-go-batch-9，分支 batch-9。
收尾按brief变基到go@08d8e6ff1，后者只新增O12文档。实际验收树b66306f28与
变基后产品树3e3e7c64c3的产品、测试、门禁脚本逐字相同；交回tip仅再更新本报告。
未push，完整core留给协调者。
本报告中的主测试服务器参数使用占位符，实际全部固定到任务指定的主测试服务器。

## 叠入范围

逐个临时分支 rebase 后 fast-forward 到 batch-9，原分支引用未修改；25个来源提交全部保留，
没有 patch-equivalent 重复提交。Markdown 冲突双方均保留。

| 来源 | 原范围（不含左端） | 组合树范围 |
| --- | --- | --- |
| go-review-n8-9-subs | d4fab6441..9be6977cb，18提交 | 08d8e6ff1..e41430b1d0 |
| go-review-jsonstore-ctx | d4fab6441..2fac52248，2提交 | e41430b1d0..e71167a299 |
| go-n8-9-routers | bcbb6e573..83b99d13a，5提交 | e71167a299..a423592cee |

## 冲突与交叉接线

- httpx/n8_1_test.go 同时保留 batch-8 rule cache native digest 和 subs hosts native digest，
  原摘要仍作为注释证据。
- refresh 保留 batch-8 的 native scheduled 实现。subs 的空 SubscriptionFetchError 中文
  提示接到现有 typed FailureReason，不恢复旧异常模拟代码。
- generation_bytes_test.go 同时保留 batch-8 provider path 精确参考、subs 9条非法Agent版本
  精确错误；原 byte manifest 不变。
- 三份 MosDNS 二进制 pyref 保留 batch-8 完整参考，不用旧分支的临时子集覆盖；随后真实
  Python check、显式 metadata record、再次 check，并验证完整 payload 不变。
- F1 discovery/legacy.go 只有两处 SaveContext/UpdateContext 替换，保留原 rules 接线。
- rulefetch、oxidns/adapter.go、mosdns/archive.go/ruleproxy.go 对基底产品 diff 为零：
  64MiB逐层限制、PeekCacheWithFormat、typed failures、当前存储声明的旧缓存归属均保留。

## 集成提交

| 提交 | 内容 |
| --- | --- |
| 9276baa9d | 新 subs helper 的真实来源登记、跨批次 source pipeline 分配基准 |
| 3f2d6b924 | 组合树源码指纹更新；原 transcript payload 不变 |
| 3549329c4 | 14处 runtime Clone 改为物理图 CopyGraph，保留独立子树预算检查 |
| 96dada57a | gateway draft 保留 owner 取消错误链 |
| a0a0219f5 | 旧单测/精确回放/数值词法的对照接线，原 archive 和 byte manifest 不变 |
| 2c7c794ce | CopyGraph 后真实 Python 参考指纹复核 |
| cacf64043 | 生产 NativeEnv 恢复可选单节点转换失败返回无节点，Runtime取消仍传播 |
| 3e3e7c64c | legacy HTTP边界保留 finished_at 的浮点JSON token，含0.0 |

## 发现与修复证据

1. **来源审计**：两组旧测试仍声明已删除的 subs 文件。根据实际传递调用给M73补20个
   native helper，两组各删4个失效路径；未改变 product/helper 分类或进程豁免。
   最终审计0.810s通过。
2. **YAML facade**：新 subs/substore 14处旧 Clone 被全树 gate 抓到。CopyGraph 保留物理
   alias/anchor，不扩展或截成null；原 Encode/ToJSON 的 root及独立子树预算仍严格执行。
   完整 subs26.879s、substore24.163s、convert corpus16.919s通过；facade及alias回归通过。
3. **取消错误链**：更早观察到取消后，draft 把它包装成业务validation，破坏 errors.Is。
   单独透传 owner ctx取消与 Canceled/DeadlineExceeded，其他业务中文路径不变。
   原真实Runtime回归从红变绿，仍检查DB/文件状态完全不变。
4. **既有批准行为的旧断言**：非法非空Agent版本仍有旧单测和录制行1144期待放行。
   新独立Python参考先验证原始输出，再仅注入已批准版本守卫；mutation必须与原记录相同。
   refresh两条回放仅缺 native SubscriptionFetchError 测试类型映射，完整文字及文件状态相同。
5. **20条数值词法**：旧JSON桥规范化指数，新native回调保留原输入词法。15处number
   1e+20/1.0e+20、5处text 1000.0/1e3，在实际yamlx消费者中均为同一float tag/数值。
   Python1.1对1e3的字符串解析属于既有N7差异，完整native corpus仍严格检查。
   参考逐条绑定原行、原manifest与原输出摘要，并执行真实Python完整生成确认原输出。
   只反向改一个精确scalar的词法，yamlx.Equal验证数值和类型，其余每个字节仍受旧SHA约束。
   原archive、原byte manifest没有重录，首轮20条红灯在修正后全部通过。
6. **生产可选转换**：proddiff抓到Surge/Loon生成、预览、artifact失败。新接口把可选的
   Sub-Store单节点转换失败传播为整份生成错误；旧契约是无节点。仅在生产composition
   adapter恢复该契约，typed Client的错误接口不变。真实loopback HTTP503回归修复前
   两个引擎失败、修复后生成成功；原Runtime shutdown/cache回归通过，敏感转换内容不出现在输出。
7. **浮点响应token**：idle finished_at 经标准JSON桥变成0，旧HTTP为0.0。仅在既有legacy
   HTTP边界保留float标记，native观察值仍为float64。真实HTTP raw token断言修复前失败、
   修复后通过。没有时间归一化或baseline豁免。

定向补验：convert旧断言/byte/全部recorded31.071s、refresh完整3150回放38.459s通过；
整包api/settings12.264s、mihomoenv0.221s通过。原 archive/manifest不改；18份transcript、
18491项的全部非metadata payload SHA与批次开始时完全相同，多轮仅更新实际源码metadata。
实际live复核：MosDNS58.305s、OxiDNS50.077s、routersplit5.928s通过。

早期一次跨包命令漏设check走了reuse拒绝，不计live通过；已用显式check补跑。
首次preflight和首次compare各自发现以上问题后修复，最终验收只计下方完整执行。
没有新增baseline、时间通配规则、架构豁免或未移植项。现有批准的D2/R5及O12登记保留。

## 分配基准

全部在同一主测试服务器，GOMAXPROCS=4、benchtime=3x、count=3，最终树b66306f28。
比较B/op和allocs/op，不设墙钟阈值。Mihomo128节点同时包含512规则；OxiDNS包含
0/2MiB未消费字段；routers两条路径均包含0/1MiB无关配置。

| 路径 | 基底B/op观测区间 | 最终B/op观测区间 |
| --- | --- | --- |
| Mihomo128+512 | 64577874–64610077 | 45866152–45894250 |
| OxiDNS冷缓存（两种padding合计） | 42808066–42874776 | 42837117–42873466 |
| routerProfiles 1台 | 314072–317538 | 20840 |
| routerProfiles 16台 | 663549–668922 | 252256–254802 |
| routerProfiles 64台 | 1712410–1714613 | 995554–1000378 |
| loadSyncSources 1规则 | 69757–69906 | 56909–57133 |
| loadSyncSources 16规则 | 6880928–6900338 | 900400–906328 |
| loadSyncSources 64规则 | 102759754–102937888 | 3617877–3631594 |

Mihomo allocs由约611k降至410314–410353；OxiDNS约470.9k稳定。最终routerProfiles
allocs为263、2457–2464、9473–9486；source pipeline为707、10786–10811、43116–43131。
三组无实际回退。OxiDNS仅有微小运行时波动，原产品消费路径未变，区间重叠且计数未增加。

1MiB padding的旧/新routerProfiles B/op约为26.59MB/20.84KB、26.90MB/252.3KB、
31.35MB/0.995MB。source pipeline64规则约437.3MB/3.60MB；无关配置没有按router/规则重复解码。
基底没有routerProfiles helper，用test-only adapter暴露旧ruleSyncRows的原投影顺序：
ListProfiles→pendingProfile解码→GetProfile→routersOf，不含后续task构建；fixture和循环
与新分支BenchmarkRouterProjection相同，旧产品不改。source pipeline基准直接在两树编译相同文件。

基准均 `go test -p 4 -run '^$' -bench '<正则>' -benchmem -benchtime=3x -count=3 <包>`：
- `^BenchmarkMihomoGenerationProfile/128$`，`./internal/convert`
- `^BenchmarkOxiDNSMetadataChain$`，`./internal/oxidns`
- `^BenchmarkRouterProjection$|^BenchmarkBatch9SyncSources$`，`./internal/routers`

最终完整原始分配日志为服务器私有 `.cache/batch9-final-bench.log`。临时诊断测试不提交。

## 最终验收

实际验收执行于b66306f28。收尾变基仅带入O12文档；与交回tip的完整文件树差异
严格限于O12和本报告两份Markdown，产品、测试和门禁脚本不变。固定主测试服务器：

```sh
RT_HOST=<主测试服务器> rt sync
ssh <主测试服务器> 'bash /tmp/preflight.sh program__configflow-go-batch-9 -p 6'
RT_HOST=<主测试服务器> rt -e GATE_ONLY=compare -- bash -lc 'server/scripts/gate.sh -p 6'
```

```text
PREFLIGHT PASS
baseline: new=0 stale=0
SYNTHETIC LIVE PASS: 23 required entries ran and passed; 0 skips; repo Python subtest and WebDAV Python exec verified
GATE STAGE PASS: compare
```

最终日志组 `gate.swtR7f`：proddiff observer/self-check全部PASS，205条路由、2831例，
new=0/stale=0、无覆盖回退；全部24个difftest包通过。
Synthetic live全部23项实际执行，无skip；repo真实Python子测试和WebDAV真实Python exec证据已验证。
没有剩余集成项或未解决finding。
完整core未执行，由协调者独立运行。没有push。
