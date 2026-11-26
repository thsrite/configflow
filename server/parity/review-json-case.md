# JSON 精确键名修复独立复核

复核者：Codex，未参与原实现。分支 `go-review-json-case`，复核基线 `go-audit-json-case@32120955f`。按协调者要求沿用指定 worktree，不另建 upstream/main 任务。仅在 `sunbroke@192.168.0.32` 测试，Go 命令使用 `-p 4`；未跑完整门禁、作者全套比较、生产快照或真实设备。

## 结论

原修复需要补正：发现两项 Medium、一项 Low，均已在本分支修复。未发现合法前端/Agent/设备键名因迁移被遗漏，未发现 Python 写入的旧凭据需要迁移。共享 facade 继续保留两个明确契约差别：结构体键精确匹配；普通 DTO 的重复成员取最后完整值。自定义 codec 继续拥有自身协议和重复键策略。

## 发现与修复

### M1：已有接口值可绕过精确字段匹配

原 `prepare` 只检查 `reflect.Type`。`any` 中已有非 nil 的 `*child` 时，native decoder 会沿指针更新该结构体，但 prepare 将 interface 当成动态 map 跳过。因此 `{"id":"ok","ID":"bad"}` 最终得到 `bad`。根接口、结构体 interface 成员、slice/array interface 元素均可复现；slice 长度为 0、容量内仍有旧元素时也一样。

修复：同时检查目标的实际值；只沿 native decoder 会复用的非 nil 接口指针解析，保留 nil/非指针接口、map 新元素和指针分配的原生行为。字段缓存增加嵌入字段索引，以定位实际目标；slice 同时考虑可复用容量。`TestReviewDynamicTargets` 的五个子项在原提交全部失败。

### M2：对象 map 重组改变解码顺序

原实现在 map 上迭代输出对象，改变无重复、精确键 JSON 的处理顺序。自定义 `UnmarshalJSON` 的调用顺序和遇错停止位置因而随机变化；不同 JSON 字符串映射到同一个 Go map key（例如 int key 的 `"01"` / `"1"`）时，最终值也可能错误。

修复：通过 native `Decoder.Token/Decode` 记录原成员顺序，仅移除普通 DTO 重复键的旧值；自定义 codec 仍收到完整原始子值。输入 span 引用原始字节，避免跨 Decode 借用 decoder 可复用缓冲区。未发生裁剪/去重/子值变更时直接交回原字节。`TestReviewCustomCodecOrder` 对成功和中途失败检查实际调用序列；`TestReviewMapKeyOrder` 检查映射后碰撞；另有超过 decoder 初始缓冲区大小的 opaque 原文测试。

### L1：合法 dash tag 被当作忽略字段

native JSON 只将完整 tag `json:"-"` 作为忽略标记；`json:"-,omitempty"` 声明合法字段名 `-`。原实现先切掉选项再判断 `-`，会丢失该字段。已改为先判断完整 tag。`TestReviewNativeTags` 在原提交失败；同时对照非法 tag 回退和 `string,omitempty`。现有生产 DTO 未发现使用此 dash tag。

## 协议和存储核对

| 范围 | 核对结果 |
|---|---|
| 迁移调用的目标字段 | 临时扩展作者的 go/ast + go/types 清单，在 .32 枚举默认构建的 1,222 个调用，递归检查普通结构体字段。迁移后的 jsondecode/devhttp/httpx 可达普通 DTO 只有内部 `rules.MatcherInput` 的 Rules/Library/CacheLibrary 缺省命名；其输入在同处由相同类型 Marshal 产生。扫描工具和中间清单已删除。 |
| frontend 请求体 | 核对 `frontend/src/api/*.ts` 的字段和对应 server DTO/自定义请求解码，特别是 settings/WebDAV、任务、通知、规则同步、网关、客户端设备和规则请求。`groupName`/`skipUnsupported` 等客户端参数在组包时变成 `group_name`/`skip_unsupported`；合法 `itemType` 有同名 tag，不要求所有键小写。 |
| Go Agent | 核对 `client.go` 注册/心跳和 `metrics.go` 的 tag：version/host/service_status/system_metrics 及 cpu/memory/disk/network 子字段与 server 声明一致；注册凭据/installation 字段走精确 model/请求读取。Agent 与 server 标签全集按 casefold 比较，唯一大小写集合差别是 Host/host；大写 Host 属于订阅传输 headers 的自定义 model，不是 Agent 心跳字段。 |
| RouterOS/iKuai | 实际 HTTP reader 解入 any/Row/map，不按 DTO tag 裁剪设备键。`TestReviewDeviceCorpusExactKeys` 从已有 r03 corpus 各取一组 RouterOS routes / iKuai groups，经 `devhttp.Response.JSON` 对照 native UseNumber；检查 dst-address / group_name 等原键和值完整保留。manifest/操作证明另检查显式 tag，包含作者补正的 priority/table/mark。 |
| 旧凭据 | Python `routes/settings.py` 写入 webdav_username/webdav_password；router/agent 持久化使用 username/password/token。新增测试读取这些历史格式的合成记录，验证凭据、旧字段缺省值、未知大小写扩展和原字节 round-trip。WebDAV Settings 自定义 codec 内部 alias 仍按准确历史名读取；Router/Agent/Backup 的 model codec 未被前置过滤。 |
| 重复键 | `TestReviewDuplicateDTOValues` 检查完整对象覆盖、被覆盖的旧错误类型、escaped 同名键；作者已有 last-complete-object / last-map-object 例覆盖嵌套 map。与 Python json.loads 的最后值覆盖规则一致。model 的严格重复键拒绝例仍保留。 |

## 差分测试与负控

`FuzzReviewNativeSemantics` 使用 native Unmarshal 和 native Decoder.UseNumber 作 oracle，同时比较结果、错误类别、type-error 的值类型/字段路径和出错后的部分目标值。输入 oracle 排除大小写别名及重复键这两个已约定差别。包含嵌入结构体、普通/多层容器指针、非零目标、map、固定数组、RawMessage、自定义 JSON codec、TextUnmarshaler（含 map key）、omitempty/string tag、接口的种子。嵌入歧义/带 tag 优先/浅层优先/未导出嵌入指针、畸形输入、自定义失败和 opaque 原文另做定向差分。错误字节偏移可能随必要的字段裁剪改变，不作为外部协议契约；不宣称有限 fuzz 等于所有可能 Go 类型的形式证明。

负控在远端临时恢复 `32120955f` 的 `jsondecode.go`，不改 DTO/断言，运行：

```sh
go test -p 4 -count=1 -run '^TestReview(NativeTags|DynamicTargets|CustomCodecOrder|MapKeyOrder)$' ./internal/jsondecode
```

四个顶层测试全部失败（DynamicTargets 五项失败），退出码 1；脚本同时断言失败码和顶层失败数量，然后恢复修复实现。恢复后的同名测试通过。作者原生 facade 负控的覆盖和恢复脚本亦已阅读，未重跑其整套负控。

## 最终验证

所有命令固定 `RT_HOST=sunbroke@192.168.0.32`。最终产品代码验证调用：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/jsondecode -- bash -lc 'cd server && gofmt -w internal/jsondecode/*.go && go vet -p 4 ./... && test -z "$(gofmt -l internal/jsondecode internal/routers/review_json_case_test.go internal/webdav/review_json_case_test.go)" && go test -p 4 -race -count=1 -run "^TestReview|^TestAuditBindingExactMembers$|^TestAuditDeviceReplyExactMembers$|^TestAuditStoredCredentialsExactMembers$" ./internal/jsondecode ./internal/httpx ./internal/devhttp ./internal/webdav ./internal/routers && go test -p 4 ./internal/jsondecode -run "^$" -fuzz "^FuzzReviewNativeSemantics$" -fuzztime=60s -parallel=4'
```

退出 0；gofmt/vet 无输出，测试末行：

```text
ok  configflow/internal/jsondecode  1.020s
ok  configflow/internal/httpx       1.301s
ok  configflow/internal/devhttp     1.009s
ok  configflow/internal/webdav      1.021s
ok  configflow/internal/routers     3.985s
fuzz: elapsed: 1m1s, execs: 3551419 (0/sec), new interesting: 40 (total: 580)
PASS
ok  configflow/internal/jsondecode  61.015s
```

随后仅增强顺序测试输入：加入被忽略字段，强制触发重组分支，避免只验证“原文不变”路径。没有再改产品代码或 fuzz 目标。追加验证：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/jsondecode/review_test.go -- bash -lc 'cd server && gofmt -w internal/jsondecode/review_test.go && go vet -p 4 ./internal/jsondecode && go test -p 4 -race -count=1 -run "^TestReview(CustomCodecOrder|MapKeyOrder|DuplicateDTOValues)$" ./internal/jsondecode'
```

退出 0，`ok configflow/internal/jsondecode 1.014s`。最终四例负控在原提交全部失败；恢复后执行 `go test -p 4 -race -count=1 -run "^TestReview(NativeTags|DynamicTargets|CustomCodecOrder|MapKeyOrder)$" ./internal/jsondecode` 再通过。

Python 参考也在 .32 通过 `rt -- python -c` 执行：对完整对象覆盖、旧值错误类型、escaped 同名键三份输入调用真实 `json.loads`，均断言结果为 `{"child":{"id":"last"}}`。退出 0，`Python json.loads: 3/3 last-complete-value cases passed`。

迭代期间新增嵌入歧义夹具曾触发 vet 的重复 tag 报警，已用 reflect.StructOf 构造有意的冲突类型；最终全仓 vet 已通过。前两次较窄 fuzz 也通过，但只以上述最终覆盖较广的一次作为交回证据。

## 交回

分支 `go-review-json-case`；实现、定向回归和本报告一并提交，提交号见协调者交回消息。没有推送或 PR；完整 batch gate 由协调者执行。没有 UI/操作入口变更，不涉及 doc/ 操作说明或截图更新。
