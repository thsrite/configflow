# N8-9 subs/substore

分支 `go-n8-9-subs`，基于 `go@03257978f`。仅在主测试服务器通过 rt 定向验证，不 push、不起子 agent、不跑完整门禁。

## 范围与设计

- subs：URI/开放 YAML 解析、筛选、hosts、dialer 元数据、代理链、监听器、节点 DNS；固定配置消费 model.Config / model.Node，开放引擎图保留 yamlx.Node。
- substore：类型化协议/响应、缓存、runtime 状态、依赖更新及其仓储接线；旧入口仅在 legacy.go 转码。
- 解决 subs→convert 校验私有副本：由 convert 接线 typed collaborator，调用现有共享实现，不复制业务；必要 convert 文件单列在最终清单中。
- 配置只在请求/旧 API 边界投影一次；节点/订阅循环直接访问 typed 记录。新增分配量基准及回归约束。
- 共享 String/Int codec、严格身份字段、Name 数字文本兼容；不加私有标量规则，不改已有 corpus 迎合实现。
- 所有写 owner Detach 后、锁后、提交点前检查取消；真实 Runtime 关机/请求断开测试。已有叶锁、observation、原子替换及清理行为保持。

## 状态

已读 AGENT-BRIEF、PORTING/WORKFLOW、N8-9 与 R5/R8、discovery 交接、health 独立复核。
缓存及 DTO 已迁移；六条有效缓存原测试、原始文件字节/hosts/日志断言保留。
已补真实 Runtime 的保存/删除/清理 × 关机/断开六条，以及 repo 实际叶锁后取消两条。
数字 ID 真实 Python 精确参照已生成，改变一个完整期待删除项的实际测试负控确实失败。
客户端/runtime/dependencies、subs 核心尚未迁移；legacy.go 中 iterate/pyMakedirs 是旧 dependency
owner 暂时仍调用的原有 helper，后续必须一起删除，不能据此声称 legacy 已纯转码。
后续完成这些核心及分配基准、所有定向检查。

## 验证（待填写）

固定 RT_HOST=sunbroke@192.168.0.32，GOMAXPROCS=4，-p 4。
受影响包 race，跨包 Corpus|Reference|Golden，相关 HTTP difftest，60s fuzz；不跑完整门禁。


### 缓存阶段验证

全部固定主测试服务器、GOMAXPROCS=4、-p 4：

- `PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/substore/testdata/n89_cache_reference.py`
  → `native cache reference: 1 exact projected Python case`。
- `cd server && go test -race -p 4 -count=1 -timeout=5m ./internal/substore -run "TestN89|TestPruneInvalid|TestLoadSubscriptionCacheLegacyAndBroken|TestSavedCacheBytes|TestStartupPrunes|TestDeletingA|TestPruningDeletes|TestHostsChange|TestNodeCount"`
  → `ok configflow/internal/substore 7.142s`。
- 同一远端副本 finally 恢复精确 fixture 的负控：只在 python.removed 加一个合成文件名，
  实际 `TestN89NumericCacheIDReference` 必须报 `native cache reference mismatch`；已通过此负控。
- `go test -p 4 -run "^$" -bench "^BenchmarkN89CacheRecord$" -benchtime=1x -benchmem ./internal/substore`
  → 32/256/2048 节点分别 787768/6007016/47830936 B/op、10090/78970/631010 allocs/op；
  节点量每增8倍，分配约增8倍，没有每节点重解整个缓存的平方增长；32→128 的分配上限回归通过。
  完整配置协作者的公开路径基准待后续 subs 迁移完成后补，不冒充已经验证。
- `go test -race -p 4 -count=1 -timeout=5m ./internal/repo -run "^TestN89NativeSharedDeleteCommitLifetime$"`
  → `ok configflow/internal/repo 1.529s`。

本阶段未跑全量 corpus/HTTP/fuzz，留给全部迁移后的最终定向验证。

缓存阶段受影响三包 `go vet -p 4 ./internal/substore ./internal/subs ./internal/repo` 和本阶段 gofmt 检查退出0、无输出。


## runtime 阶段（进行中）

- 状态读取/原子写入/Snapshot/saveMode/GetMode 改为 typed；工作/控制/跨副本叶锁原结构不变。
  GetMode 只在 repo.View 中读取 mode，不创建整个 system 的旧 JSON 视图。
- 原无ctx私有 read/write 和旧 Status 留在 legacy.go，仅转码旧测试/调用方；dependencies 核心
  仍调用 Status，下一阶段改 Snapshot。旧错误工具/路径和时钟工具仍待客户端/dependencies一起清理。
- 私有状态文件 framing 和 CPython内部错误按协调者同类裁定处理；正常字段/值/顺序/
  supervisorctl 请求与效果不放宽。已核查部署读取方，旧 Python 原样字节读测试通过，详见 ledger。
- Python引用：`PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/substore/testdata/n89_runtime_reference.py`
  → `native runtime reference: 128 exact real-Python cases`；原 review_reference.json/生成器/比较器不改。
- 定向 race（主服务器，GOMAXPROCS=4，-p4）：
  `go test -race -p 4 -count=1 -timeout=5m ./internal/substore -run "TestReviewRuntimeMalformedState|TestN89Runtime|TestStateFileBytes|TestPolling|TestConcurrentConversions|TestUpdateWaits|TestStartFailure|TestStartTimesOut"`
  → `ok configflow/internal/substore 9.556s`。
  包含写完/关闭真实临时文件后 Runtime 关机/断开，主文件不变/正常提交、未知成员原字节保留、无临时文件残留。
- 实际 reference 负控：仅改变128条中row1的返回 mode，原测试必须失败，已通过；finally恢复文件。
- 同一公开 BenchmarkN89RuntimeMode，0/64KiB/1MiB 无关设置：
  - 迁移前：6400/6400/5920 B/op，121/121/120 allocs/op。
  - 迁移后：5320/5800/5800 B/op，115/116/116 allocs/op。
  模式读取没有随无关配置增大；缓存后测试回归预算为大配置不能多出64KiB/op。
  这不代替后续完整订阅/代理链生成路径的配置分配基准。


## 客户端阶段

- 新增 Fetch/FetchResult、ConvertProxy/ConvertTarget（实现共享 convert.NodeConverter）、
  ParseProxyNodes/ProxyNodes；native core 不再导入 jsonx 或 any。旧下载/转换/批次入口
  仅留 legacy.go 转码；临时 YAML→jsonx 中转文件已删除。wire 新增 NativeConverter。
- 默认 fake 的有效业务/请求/日志断言不改。共享结果指针身份测试改为直接调用 Fetch；
  fake callback 只换 typed 类型，原请求次数、版本LRU、深拷贝、流量和文件期待不改。
- 节点零值输出保留完整 name/type/server/port/params，开放参数保留 tag；严格 type、共享
  数字文本和带引号整数回归通过。完整 proxies 子树先做 yamlx 预算检查，避免 alias 解开后
  克隆静默变 null。原三条 alias 放大/子树预算测试保持。
- 精确 native_client fixture 绑定原 corpus/review SHA，只列批准的9处边界；原 corpus、
  N7 oracle、比较器不改。固定中文业务前缀保留。详见 ledger。
- 主服务器 `GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=5m ./internal/substore -run
  "TestN89NativeProxyRecords|TestN89ClientOwnerLifetime|TestN89TemporarySubscriptionRawRequests|TestN89FlightWaiter|TestPythonCorpus|TestReviewParseUserinfo|TestProxyYAML|TestConversionFailure"`
  → `ok configflow/internal/substore 2.890s`；同次定向 vet 退出0。
- 另一次完整客户端正常复用/退避/版本转换/日志/alias 定向 race → 9.392s。
  env/http 响应与 dependencies 的旧动态消费、subs 核心仍待迁移；不能据此交回完成。
