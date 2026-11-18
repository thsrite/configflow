# N8-9 rules / rulefetch / refresh / rulesetfmt

状态：实施中；基线 go@95440b69e（只比03257978f多盘点文档，batch-7未合入）。
不依赖尚未落地的health/discovery typed API，不复制其他分支文件，不改convert。

## 第一段：规则索引和格式叶层

- Matcher / Hit、Cache / Cached / MatcherInput 使用 model.RuleConfig / RuleLibraryItem；
  旧 discovery 调用通过 matcher_legacy.go、cache_legacy.go 的窄边界衔接。
- 删除 rulesetfmt/legacy.go，唯一剩余生产调用方 rules.RulesetFormat 改走原生RuleFormat。
  保留原Python格式档案及逐字正常路径断言；不再以legacy输出当typed实现的参考。
- 签名采用D2原生JSON；保留invalid receipt、当前声明检查、旧identity/name冲突协议。
- typed policies / 泛型reorder已落地，旧调用方边界不复制核心规则。
- 两种节点规模（0 / 20000个无关节点）的窄投影分配均为188 allocs/op，
  14606 / 14580 B/op；另有AllocsPerRun负控约束无关配置大小不改变分配。

协调者确认：原始语料不动，R5/D2用逐行独立预期验证。
`testdata/n89_cache_reference.py` 调真实Python记录600条缓存名、7条namespace和114条policy；
先对typed输入调用原Python，D2只采用约定的compact JSON声明摘要。
Go测试逐行绑定原输入和原输出，不空跳过错误类别。
缓存原始档案SHA256固定；policy行数/原输入绑定。

定向远端检查（.32，GOMAXPROCS=4）已通过：
`go test -p 4 ./internal/rules ./internal/rulesetfmt`，末行2.272s / cached。
rulefetch / refresh尚未改时各自原包通过6.539s /44.192s。
尚未做最终race/vet/Flask/fuzz，不能作为交回结果。

## O12切换检查（待第二段闭环）

生成路径会使用规则缓存；只更换摘要而无兼容读取，在上游离线时会失去旧缓存兜底。
已增加旧fixed-schema摘要的只读寻址，限定当前存储声明认领；写入方不使用旧摘要。
后续必须在rulefetch对旧文件内容校验、声明复核后写入native名称，并补离线、改声明、
invalid receipt和真实Runtime关机/断开测试。未完成前不交回。

## 剩余工作

- rules组候选/连接DTO和验证，移除动态core。
- rulefetch内容、下载、缓存和原生HTML/error迁移；一次性缓存读取闭环。
- refresh仓储/DTO/任务行和ctx提交边界迁移（仅对未迁移owner保留薄adapter）。
- 增量race、跨包Corpus|Reference|Golden、相关Flask和fuzz60s；完整go vet与最终报告。
