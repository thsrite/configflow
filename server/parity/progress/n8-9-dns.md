# N8-9 DNS 迁移

分支：`go-n8-9-dns`，初始基点 `go@03257978f`，已变基 `go@d4fab6441`。仅在 .32 使用 rt 做定向检查；按协调者补充要求执行 compare，不跑 core/完整门禁。

## 范围与边界

- MosDNS：typed model 设置、规则布局、生成器、设置 HTTP DTO 与 typed 仓储写入。
- OxiDNS：内部 native YAML + typed 仓储/metadata；移除 Python truthiness、私有 JSON/标量 codec。
- `dnsha` 和 Agent 业务不修改。协调者批准 `oxidns/legacy.go` 只保留转码，旧 `NewProfile(*jsonx.Object)`、`ToJSONX` 等待 N8-10 迁移 `agents/rulesync/adapter.go` 与 `api/agents/client_tools_adapter.go` 后删除；N8-11 仍须检查这两个调用方。
- 不把 model.Raw 重新编码后当作消费模型。原始 bytes/revision 由仓储 R1–R9 保留；消费使用共享 R5 typed 字段。

## 验证记录

MosDNS typed 生成、共享 R5 消费与源不变检查已通过；既有参考语料以 typed/R5 投影输入调用真实 Python 复验。没有改 baseline。

## 已完成的定向检查（实施阶段）

- `.32`：MosDNS typed generation / R5，OxiDNS whole-draft 事务和 If-Match，C27 非字符串展示名称，ZIP shutdown / disconnect；新 MosDNS owner 的真实 Runtime 断连、已关机、事务锁等待取消，定向 race 通过。
- ZIP 原录制 86 条全部通过，包含匹配缓存回退和文件副作用；未放宽断言。
- `yamlx.CopyGraph` 经协调者批准，直接复用 Edit 的 copyGraph；共享关系、别名预算、源不变、并发编辑与放大负控 race 通过，未改 Clone 架构门。
- CopyGraph 物理图基准（100 次）：深度 8 / 64 / 256 分别为 5,064 / 41,256 / 164,456 B/op，38 / 268 / 1,040 allocs/op，与物理图规模成比例。
- OxiDNS 冷缓存全链 `Generate → ippresence → rulefetch / RulesetFormat`（128 条 URL 来源，128 份真实验证缓存，native config，3 次）原窄 metadata bridge 仍为 1,034,492,202 B/op、12,736,634 allocs/op。typed presence + 已知 format 读取后降为 54,275,613 B/op、648,782 allocs/op；加 2 MiB 无关字段 54,293,730 B/op、648,774 allocs/op。每次清 memo，并断言至少 128 次 presence 调用；分配回归测试限制 80 MiB/900000 次分配及无关宽度增量。此前 5.9 MB 的 template/inline 样本未经过远程验证缓存，不作全链证据。
- `ippresence.TypedClassicalIPLines(ctx, model.RuleConfig, []model.RuleLibraryItem) (*bool, error)` 共用原 inline/cache/memo/UTF8/IP 判断，保留旧 any 入口。真实 Wire 切 typed hook；不访问网络。
- rulefetch 仅 `cache.go` 增加过渡入口：`ReadValidatedRuleCacheWithFormat(ctx, rule, config *jsonx.Object, format string) ([]byte,error)`；原入口委托 format=""。按协调者指定增加同文件最小 `SourceConfig` 和 `ReadCacheWithFormat(ctx, model.RuleConfig, *SourceConfig, string) ([]byte,error)`；已知 format 不再解码 library。batch-8 先叠 rules 后叠 DNS，这份过渡实现应取 rules 分支的同名 native 版本，不保留两份。

- Native Reader / R5 旧异常形状与极值语料已逐项核对；原生类型的批准差异另存精确输入与完整期望，保留其他字段严格比较。没有改 proddiff/configdiff baseline。

## 变基与新增的原生边界

已变基到 `go@d4fab6441`。合并冲突保留 O3 新 Runtime worker 跟踪、configgen 故障钩子；没有改 Agent/dnsha 文件。发现并修复 typed 投影漏掉退休 rule-proxy token 的问题：不再阻止经过证明的旧生成链接更新到新域名/当前 token。能力证据继续要求原始 JSON 字符串。

动态设置按协调者的严格请求/宽容存储裁定实施：新增 `Profile.FieldIssues()`，只记录路径与 IssueType；API 400 不提交；读取/生成做 owned projection，数字 token 文本和 0/1 保留明确语义，错误容器停用该项。冻结录制未改，99 条精确输入的新拒绝另存原生期望索引。真实 Python 对照覆盖 0/1、数字路由表、数字网关、数组路由表、对象网关；完整投影结果比较，原始存储不变。

原生诊断、能力、读取边界采用单独登记的新期望，既有录制语料不改。

原生边界收尾补充：新 reader 索引登记 108 个源 SHA，未知新文本仍失败且必须完整匹配共享 facade 图；52 条诊断、2 条 capability 独立新期望均保留其他完整输出。录制和随机差分发现并修掉 alias 序列子项漏校验、非文本 cert/file/key 被静默跳过、数值零检查丢掉 0.0、非空 form 漏做互斥校验等实际问题。正常模板中非字符串扩展键仍用完整 YAML 语义对比；不使用原生文档的安全字段名检查器替代生成结果比较。

## 最终验证记录（.32）

共同前缀：`RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && …'`。

1. `gofmt -l internal/mosdns internal/oxidns internal/ippresence internal/api/mosdns internal/api/oxidns internal/configgen internal/yamlx/copy.go internal/yamlx/copy_test.go internal/rulefetch/cache.go`：无输出。
2. `go vet ./...`：通过、无输出。
3. `go run ./tools/pyref -scopes server/internal/mosdns,server/internal/oxidns`：`pyref audit: 2 registered call sites, 2 scopes; no unregistered process constructors/live oracles`。
4. `PYREF_MODE=record PYREF_ACCEPT=1 go test -p 4 -count=1 -timeout 30m ./internal/mosdns ./internal/oxidns ./internal/ippresence ./internal/api/mosdns ./internal/api/oxidns ./internal/configgen`：全部通过。最后各包为 `83.603s / 112.152s / 2.199s / 0.676s / 0.608s / 1.375s`。只更新 pyref 来源指纹和真实 Python 的新投影输入；原冻结 `.jsonl.gz` 无修改。随后以 check 模式复验（见下）。

5. 首次 8 包完整定向 race 暴露 MosDNS archive hook race（401.254s）及 OxiDNS pyref 对象顺序问题（896.306s），未标为通过；其余 ippresence / api/mosdns / api/oxidns / configgen / yamlx / rulefetch 为 11.207s / 2.074s / 2.375s / 3.234s / 20.141s / 31.880s。问题修复后按失败范围补跑，不额外重复全量。
6. `PYREF_MODE=check go test -race -p 4 -count=1 -timeout 30m -run "TestStoredLegacySettingsAgainstPython|TestBoundaryRoutingCorpus|TestStoredSettingsProjectionKeepsStoredValidator|TestC29" ./internal/oxidns`：`ok ... 52.785s`。MosDNS 完整普通 check 为 `68.863s`（原生文本追加后再次复验见后）。
7. `CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout 30m -run "Test.*(Runtime|Shutdown|Disconnect|Archive|DNS|Mos|Oxi|ClientTools)|TestA24FlaskBoundaries|TestClientNoteAndPolicyCancelledOwners" ./internal/deploy ./internal/setup ./internal/agents/rulesync ./internal/api/agents`：全部通过，34.571s / 6.824s / 46.182s / 47.202s。
8. 三个目标各 `go test -p 4 -count=1 -run "^$" -fuzz "^NAME$" -fuzztime 60s -parallel 2 ./internal/PKG`：CopyGraph 2,115,735 exec / 60.095s；NativeYAMLRoundTrip 79,113 exec / 61.036s；MosDNSYAML 32,621 exec / 61.023s，全 PASS。追加原生文本后重跑 MosDNS fuzz，见后。
9. 最终基准 `go test -p 4 -count=1 -run TestMetadataChainAllocationBound -bench BenchmarkOxiDNSMetadataChain -benchtime 3x ./internal/oxidns`：PASS 4.589s。128 个真实离线规则，unused0：61,374,226 B/op、749,515 allocs/op、72.198 ms；unused2MiB：61,360,968 B/op、749,545 allocs/op、71.081 ms。最终版本约 61 MB，前面的 54 MB 是实施阶段记录；仍远低于原始 1.03 GB，且无关大字段不放大分配。80 MiB/900000 分配负控通过。

10. 原生文本追加后：`PYREF_MODE=record PYREF_ACCEPT=1 go test -p 4 -count=1 -run TestNativeRuleTextAgainstPython ./internal/mosdns`：15 个实际 Python 对照输入通过（0.333s）；完整 MosDNS record：69.758s；随后 `PYREF_MODE=check go test -race -p 4 -count=1 -timeout 30m -run "TestNativeRuleTextAgainstPython|TestArchive" ./internal/mosdns`：27.041s，通过。`go vet ./...` 和 pyref 两包 audit 再次通过。
11. 原生文本追加后 `FuzzMosDNSYAML` 再跑 60 秒：35,205 exec，34 个新 interesting，PASS，61.031s。最终 `gofmt -l` 改动目录/文件无输出。
12. 按协调者要求 `GATE_ONLY=compare server/scripts/gate.sh -p 4`，外层仅给排队加 900 秒上限（超限只停自身进程组；拿到 slot 后不限制执行时间）。.32 起始负载为 2.00 / 2.50 / 3.42，立即取得 slot。proddiff：`baseline: new=0 stale=0`、`self_check=PASS`，Go/Python transport_retries 均为 0；proddiff 起止 loadavg 为 2.96 / 2.70 / 3.47 → 4.97 / 3.64 / 3.69。精确匹配本次私有 summary 后，仅统计 go.log 的相关字段路径告警，命中 0 条；生产快照没有触发批准的旧动态类型兼容分支。没有拷贝快照内容或改 baseline。首次 compare 的 proddiff 通过，但 difftest 阶段失败，未执行 synthetic live：devicenotes/save 的 null、[]、bool 三项，以及 MosDNS 请求的正常字符串匹配项、已批准类型边界与 revision 级联差异。随后修正常功能与严格写入验证，将17个错型/引用向量移至各自独立快照的真实 Flask 对照，精确断言状态和 Go 无写入；原 GET 大整数案例已在独立向量中逐字段比较批准差异。第二次 compare 在 2c8ce947e 上完整通过，见末尾记录。

### R15 新接入点（协调者追加，最小改动）

batch-7 的 `routers/management_repo.go:saveWorkspacePolicies` 新增了第三处旧 OxiDNS 调用。按协调者分工，仅将此函数改为 `ProfileFromJSON(config)` + 共享 `model.Fields`/`json.RawMessage` 替换 OxiDNS，保留其他原始字段；JSON/Node 转码错误明确返回。路由器业务、事务、锁都不改。`CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout 30m -run "TestR15|TestReviewR15|Test.*Workspace" ./internal/routers ./internal/api/routers`：1.090s / 32.062s，全过，包含 TestR15FlaskHTTPAndStoredState 与 TestR15FlaskWorkspaceSourcesAndDNSPreview。新增名字更新保留 OxiDNS/MosDNS/扩展字段原始片段、重复保存 no-op 的回归。`TestClientNoteAndPolicyCancelledOwners` 单独 race 5.559s 通过。Agent 两处继续留到 N8-10。

## 问题与修复

以下为迁移实现检查发现并修复的问题。

- High：alias 指向序列时，schema 校验原先可能跳过子项。统一在 schema 入口解引用，既有错误配置仍被拒绝。
- High：资源路径字段类型错时可能静默跳过引用检查。现在拒绝存在但类型错误的 file/cert/key/ca/ssl 字段，不改变合法模板。
- High：超大指数数字可能进入任意精度转换；先做有限值及 schema 验证，避免资源放大。
- Medium：完整 OxiDNS 生成的逐规则 metadata 路径重复消费整份配置；改为 typed 元数据与已知 format 的离线 cache reader，全链分配从约 1.03 GB 降至约 61 MB（实施阶段约 54 MB，见最终基准）。
- Medium：ZIP 下载提前失败后，worker 重读可替换的 fetch hook，与录制测试恢复 hook 形成数据竞争。改为启动 worker 前捕获 collaborator，保留提前返回及 Runtime 跟踪语义。
- Medium：正常 custom_matches 字符串被误拒绝、旧无效备注原类型被 R5 默认值掩盖；补 typed union 与写入前原字段验证，相关 Flask 边界已复验。
- 迁移中修正：退休 rule-proxy token 必须保留以安全重写旧链接；重复规则 ID 的文件布局使用最终完整标签映射；无 ID 来源的缺失字段不人为补 null；设备备注仍单独提交，名字改动不重建原始 DNS 设置；兼容读取保留原始存储 ETag，允许用户修复旧类型，修复后旧 token 失效。

## 明确剩余边界

- oxidns/legacy.go 仍有旧接口转码；生产 NewProfile/ToJSONX 调用只余两处 Agent adapter，按裁定留给 N8-10，不能宣称 N8-11 删除门槛已满足。difftest 中的旧输入适配也保留，随调用方迁移处理。
- MosDNS 的外部 refresh/rulefetch/rules 等未迁移 collaborator 留在唯一 legacy 文件；无私有 YAML scalar/Python truthiness 实现。既有错误分类兼容仍复用共享 rulefetch/refresh，待相应 owner 的 typed API 合入再删转码。
- rulefetch/cache.go 的最小 SourceConfig/ReadCacheWithFormat 是跨分支过渡层；batch-8 合并以 rules 分支的同签名原生实现为准。
- 本分支没有改 Agent/dnsha 业务、原冻结语料或比较器/baseline。完整门禁由协调者在批次树执行。

参考复验曾有 29 个 live 输出只因 JSON 对象成员顺序而不同（解析后结构逐项相等），因此只在 n89_reference.py 的传输 json.dumps 增加 sort_keys=True，产品与列表顺序不改。收尾重新录制的 12 份 pyref（含 18033 个 MosDNS 输入和 60 个 routing 输入）输入 key 集、输入/输出解析结构均无变化；来源指纹和传输对象排序变化经过逐项审阅。

### MosDNS 原生文本处理（协调者追加）

content.go 消费规则类型关键字、经典规则 payload 和域名模式；不是密码/不透明 ID。已替换 pystr.Strip/Upper/Lower 为 strings.TrimSpace 与 x/text/cases。15 个真实 Python 正负控覆盖 ß、i/ı/İ、最终 sigma、NBSP、每个 U+001C–U+001F 的精确字节以及整份规则内容输出。正常 Unicode 输出保持；仅原生首尾空白不删除这些四个控制字符，具体差异登记 behaviour-changes。原共享 splitlines 的 U+001C–U+001E 换行语法不改。

两处 rulefetch.PythonClass 经协调者明确裁定本轮保留：archive.go:safeDetails、ruleproxy.go:isBadRequest。batch-8 集成时接 rules 组的 typed errors，由协调者列入集成清单；不复制错误分类，不宣称 N8-11 grep 已满足。

首次完整定向 race 的确切命令（非全仓）：`PYREF_MODE=check go test -race -p 4 -count=1 -timeout 30m ./internal/mosdns ./internal/oxidns ./internal/ippresence ./internal/api/mosdns ./internal/api/oxidns ./internal/configgen ./internal/yamlx ./internal/rulefetch`。修复后失败范围及直接调用方的复验命令、结果均在上文，不能把首次失败覆盖成全绿。

### 最后一轮 HTTP 边界收口

- 前端类型证据：frontend/src/views/Generate.vue 的数值输入使用 v-model.number，缓存加载使用 Number(...)，Switch 为 bool；saveMosdnsSettings 直接提交 draft。保留严格 ASCII 整数字符串接受，拒绝 float/bool/overflow/Python 专用整数文本的裁定已写 behaviour-changes。
- model.MosDNSMatch 补字符串/对象 union：保留原始字符串 JSON 和完整合法 mixed list；生成时继续忽略字符串项。原 custom-matches POST 与随后 GET 不再被误拒绝。
- DeviceNotes 严格写入验证只消费该字段的保真 JSON 和原始类型诊断，不重新编码整份 profile；读取/生成的 R5 默认不能掩盖非法旧备注。普通 DNS 生成不因这些备注中断。
- cache-settings 的 null 恢复默认200；缺失引用按裁定409/usages。错误用例没有合并到比较器中，也没有改变 baseline。

HTTP 收口定向结果：`CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout 30m ./internal/model ./internal/api/mosdns ./internal/devicenotes/difftest ./internal/mosdns/difftest` 全过：19.510s / 2.031s / 3.420s / 281.255s。随后补回大整数 GET 精确两侧完整响应，并细分“必须是对象/字段类型无效”中文常量；`-run "TestMosDNSNativeRequestDifferences/.*big.*|TestTypedSettings|TestCacheIntegers|TestMosDNSMatch"` 对 MosDNS difftest / api/mosdns / model 的 race 再过：15.174s / 1.916s / 1.020s。原调用链的语料和 R1–R9 检查继续保留，新增 scalar union 的 R1 字符串源码保真和有效 mixed list 回归。

共享 model union 和备注原类型验证落地后，重新录制 MosDNS/OxiDNS：78.128s / 107.262s，全过；已有 scalar union fuzz FuzzStoredTypes 扩展覆盖字符串变体，60 秒 882,711 exec（14 个新 interesting），PASS 61.016s。原对象变体的 R1–R9 编辑断言保留，标量变体独立核对源码保真和字符串修改值。

最后一次 source 指纹审阅：OxiDNS 各 transcript 原输入 key 集和完整解析输出不变。MosDNS Corpus 从18033变为18040个实际参考输入；195个旧 key 被202个新 key 替代。195组原有函数向量去掉 custom_matches 投影和 actual_yaml 后，其余输入逐项相等（未解释0）；新增7条是实际 custom_matches 对照。原因是共享 model 现在保留合法字符串/mixed list，过去整列表被误回退为空。共同输入的真实 Python 输出结构变化0。原 `.jsonl.gz` 冻结文件未改。

最终附加检查：`PYREF_MODE=check go test -race -p 4 -count=1 -timeout 30m -run "TestMalformedStoredNotesOnlyRejectNotesWrites|TestStoredSettingsProjectionKeepsStoredValidator|TestStoredLegacySettingsAgainstPython|TestBoundaryRoutingCorpus|TestC29" ./internal/oxidns`：51.017s。该检查证明坏备注只拒绝备注写入，无关生成仍成功、原始记录未变。R15/workspace 全范围定向 race 再次通过1.085s / 28.523s；全链分配负控再次通过1.702s。gofmt 无输出，先前 go vet 全仓和 pyref 两包 audit 已通过。

最终产品提交：`2c8ce947e`（在本报告记录提交之前）；分支 `go-n8-9-dns`。第二次 compare 在 .32 起始负载0.48 / 0.88 / 2.28，立即取得 slot，该树完整 compare 已通过，见末尾记录。没有运行 core/全仓门禁。

## 交回结果

- 完整 compare 的产品树为 `2c8ce947e`，.32：`GATE_ONLY=compare server/scripts/gate.sh -p 4`。proddiff `new=0 stale=0 self_check=PASS`，Go/Python 重试均0；起止 loadavg 为1.00 / 0.98 / 2.30 → 2.97 / 1.94 / 2.46。最终私有日志相关动态类型路径告警再次统计为0。只记录汇总，没有复制快照内容。
- 全部23个 difftest 包通过。先前两包失败已关闭：devicenotes/difftest 0.601s；mosdns/difftest 40.336s；api/agents/difftest 252.678s。synthetic live 使用真实 Python，23个必跑 entry 全过、0 skip，并验证 repo Python 子测试和 WebDAV Python exec。
- 最后几行：`SYNTHETIC LIVE PASS: 23 required entries ran and passed; 0 skips; repo Python subtest and WebDAV Python exec verified`；`GATE STAGE PASS: compare`。19:42:35 结束时负载1.38 / 1.82 / 2.31。
- compare 之后只收紧两条 MosDNS 写入口的缓存 bool 显式 null：它属于非 bool 请求，不能被指针解码当作缺省值；整个 cache-settings body 的 JSON null 仍重置200。新增3个真实 Flask 向量（两个 cache 字段、一个 whole editor），总计20个独立错误边界；Python200 / Go400，DB行/revision/业务文件均未写入。此小改动不改变 compare 的读取/生成路径。
- 末次补跑确切命令：`CONFIGFLOW_DIFF=1 go test -race -p 4 -count=1 -timeout 30m -run "TestMosDNSNativeRequestDifferences/.*null.*|TestDifferentialMosDNSRoutes|TestTypedSettings" ./internal/mosdns/difftest ./internal/api/mosdns`，77.628s / 1.845s，全过；包括正常路由流程、ASCII整数字符串、整体cache null默认重置和四个null边界。随后 `go vet ./...` 再过、gofmt无输出。最后产品提交为 `783e12839`。
- 不再重复完整 compare；完整批次 core/门禁由协调者执行。明确区分 compare 的2c8ce947e树与末次写请求守卫的定向复验。

跨分支集成清单：`rulefetch/cache.go` 取 rules 分支正式同签名实现，typed `ReadCacheWithFormat` 的非空 format 跳过推断；ippresence 的窄 cacheMetadata 转码在接 rules 原生缓存路径后收口。MosDNS/configgen 的旧 Render、deploy 的旧 snapshot 兼容调用仍经过唯一 legacy 转码层，核心生成已 typed，随外部 owner 迁移删除；没有复制外部业务。OxiDNS NewProfile/ToJSONX 生产使用只余两个已批准 Agent adapter，留N8-10；两处 PythonClass 留 batch-8 typed errors 集成。N8-11 删除门槛仍须检查这些残余。

最终执行 git rebase go，基点仍为 go@d4fab6441；工作区应为空。未 push。报告记录提交号在交回消息中给出。
