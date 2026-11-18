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

## 第二段：内容/下载/缓存核心（实施中）

- rulefetch Content明确区分文本与二进制；Inspection/Problem/Change、SourceConfig、
  typed CacheOptions/DownloadRequest/ResolveSource全部用原生类型。
  API/deploy/substore旧调用只经过窄adapter，不再由core创建jsonx config；不改convert。
- 默认重试等待使用ctx选择；写owner在Detach后和下载完成后的实际缓存提交前检查同一Runtime。
- O12已补一次性读取闭环：旧摘要只用于读，校验内容后在repo事务中再次核对声明/receipt，
  以native名称保存，旧文件保留便于回滚。事务以内部sentinel退出，不增加DB revision。
  合成测试覆盖离线上游、迁移后去掉旧文件仍可读、坏内容、改URL/behavior、旧/新invalid receipt。
- 真实Runtime.Start/BindRequest/Shutdown覆盖请求断开与关机；在下载回调结束、提交前触发关机
  不写文件，浏览器断开仍完成已接管的写入。没有用普通cancel代替关机。
- 内容DTO测试只改访问方法，不改原始期望。手写enum/content的null/数字仍由写入口严格拒绝；
  曾发现typed回退把两个null编辑变成默认，以及数字content被转文本，已修产品验证。
- 删除pystr.go中的JSON/bytes repr和Unicode异常仿真；规则文本仍保留已有分隔符集合。
  无效UTF-8仍保留原中文消息、行号和有限字节证据；72849+13832+103734条原参考继续精确对照。
- 仅一条requests内部英文异常改为原生ErrRedirectEncoding；原档案的完整UnicodeDecodeError
  仍固定断言，同时逐字段断言原生拒绝与完整消息，网络请求transcript照常比较（R8）。

远端原包最后通过：`go test -p 4 ./internal/rulefetch`，7.902s；
新增迁移/Runtime/download owner子集通过2.176s。最终race/Flask/fuzz仍待整组完成。

## 第三段：候选/连接核心（实施中）

- GroupCatalog/GroupSelection/GroupMember/Provider/RenderedMembers与IPDeps/IPTarget/
  RulesetInfo/IPResult均为原生DTO。rules运行时只有明确的*_legacy.go边界引用jsonx。
- CheckCandidates、CheckFilters、MergeGroup检查原始成员证据；源ID数组及排序项不能依赖
  R5将非法写请求变成有效引用。显式健康字段删除保留未知成员和原始数值字节。
- LowerProviderFilters在独占YAML Node上工作；必须复制的provider走yamlx.Marshal/Parse预算API，
  不用Clone。provider私有path采用D2原生三字段JSON摘要。
- `n89_group_reference.py` 调真实Python记录1500条typed reader输入、600条lower及88条复核lower。
  D2仅拦截Python函数自身的tuple摘要输入并按约定native JSON编码，候选选择/过滤/命名/文档
  全部仍由真实Python执行；逐行绑定原输入，原档案和未知期望不改。
  原始受控写验证和merge期望继续原样比较，不将错误消息先后顺序变更登记为R5。
- 定向包最后通过 `go test -p 4 ./internal/rules` 3.745s。
