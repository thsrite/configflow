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
