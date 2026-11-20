# N8-11 batch-16 SQLite position 追加修复

状态：**完成**。实现、新增回归、三个完整 live difftest 包、repo 定向 race、两组 vet 和完整非 race 预检全部 PASS。分支 batch-16，基底 671f65db3。本机仅编辑/git/轻量读取，测试均在服务器；未 push。

## 范围与根因

协调者交办 compare 的九个 HTTP/SQLite 状态差异；按默认裁定修复 repo，不修改 difftest 比较。问题引入提交：4831dcd25（W1 的 import_conversion.go）。

实际不符行是 shared/_revision。convertImportJSON 新建 shared 时用 struct literal 赋四个非 nil 空列表，却只对 _revision=0 调 SetJSON。四个列表尚未进入 Raw.members，SetJSON 先登记 revision，EncodeRecord 先输出 Raw.members 再输出新 typed 字段，产生 `_revision,subscriptions,nodes,subscription_aggregations,rule_library`。position 来自 store.documentRows 对最终完整字段列表的零基枚举，因而 revision=0；Python 按 SharedFields 构建四个列表后再追加 revision，其 position=4。

Flask 差分的种子树从 Go 复制，因此初始错误位置两边相同；后续 Python _apply_shared_locked 重建 shared 为 SharedFields+revision，将其恢复为 4，而 Go 保留错误的 0，暴露状态差异。并非 SQLite 给单个新字段盲目分配 0：store 对每个 section 完整编号并更新所有变化行，正常写入 position 唯一，不会因本缺陷产生 position 重复或同位置排序不稳定；错误是初始化的确定性成员顺序。未改底层 store/model，也没有扩大或掩盖已有重复 position 数据的读入规则。

## 修复与回归

- 新建 shared 时按 SharedFields 依次 SetJSON([])，然后 SetJSON(_revision=0)，让所有成员通过同一个原生 model codec 建立正确顺序。适用于首次启动、旧单档案导入、schema-2 导入；当前 schema-5 导入保持原始成员顺序。
- 更新 behaviour-changes：撤销 SQLite position 可随 native 编码变化的宽泛例外；新成员追加，更新已有成员不移动位置。未改任何 difftest 比较、冻结语料、Python oracle、pyref 数据。
- 新增 batch16_position_test.go（153 行）：fresh / single-profile / schema-2 表驱动回归，直接查询 SQLite field+position，精确断言五成员顺序；连续增加两个未知成员必须追加且已有成员位置不变；改写第一个未知成员不移位；reload+no-op 不重写任何存储行。每次查询同时校验连续且无重复 position。
- 负控：在 .32 仅临时换回 HEAD 的旧 import_conversion.go，三种创建路径全部因 revision 在首位而失败；恢复修复文件后通过。临时 harness 诊断也已还原，无调试文件入库。

## 同轮修复的第二个 W1 存储问题

position 修正后，rules/rulelibrary 两个完整差分包已通过，但 C06 的两项测试露出下一处差异：state.tables.fields[11][5] Go=1 Python=2。取证确认是 profile/default/oxidns 的 SQLite 行 revision，不是业务 revision；两边解析后的值相同，position 同为 5。W1 defaults.go 将带缩进的 embedded oxidns_defaults.json 作为原始 Dynamic JSON 入库（3692 bytes）；Python 在任意成功保存时将它原生 compact 编码为 2669 bytes，触发一次额外行重写，Go 正确保留未变用户行，因此原有比较失败。引入同为 4831dcd25。

仅在内置默认值构造处用 encoding/json.Compact 去掉源文件格式空白；DefaultOxidnsSettings 和 defaultConfigJSON 共用此入口。不紧凑化任何已存储用户数据，不仿制 Python encoder。新增回归直接断言内置 OxiDNS 行为 compact JSON、系统写入不改此行的内容/行 revision，并另写入带格式的用户 OxiDNS JSON 后证明系统写入依然逐字节保留它与行 revision。C06 两项原测试与比较保持不变。

## 归因与初步证据

- .32 修前 TestRuleLibraryReadCreateHTTPParity 复现 Python=4 Go=0；临时日志只观察差异行，证实是 shared/_revision，测试比较未改变。
- .64 W1 前 e7aba2586 的独立基线（~/batch16-baselines/go）：CONFIGFLOW_DIFF=1 go test -count=1 -timeout 5m ./internal/api/rulelibrary/difftest -run '^TestRuleLibraryReadCreateHTTPParity$' → PASS，0.994s。
- .32 position 修后新回归 PASS，0.744s；同一 Flask 用例 PASS，1.290s。增加内置默认值修复后的两个新回归 PASS，0.926s；C06 两项原测试 PASS，16.663s。
- 本次改动与 tools/pyref/sources.json 相交为空，不需刷新任何 reference。

## 验证命令

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/repo/batch16_position_test.go -- bash -lc 'cd server && gofmt -w internal/repo/batch16_position_test.go && go test -count=1 -timeout 5m ./internal/repo -run "^TestSharedMemberPositionsSurviveImportAndTypedUpdates$" && CONFIGFLOW_DIFF=1 go test -count=1 -timeout 10m ./internal/api/rulelibrary/difftest -run "^TestRuleLibraryReadCreateHTTPParity$"'

RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l .)" && CONFIGFLOW_DIFF=1 go test -count=1 -timeout 20m ./internal/api/rules/difftest ./internal/api/rulelibrary/difftest ./internal/httpx/difftest && CONFIGFLOW_PY="$HOME/cf-test/.venv/bin/python" CONFIGFLOW_REPO="$(pwd)/.." go test -race -count=1 -timeout 15m ./internal/repo -run "TestSharedMemberPositionsSurviveImportAndTypedUpdates|TestBuiltinOxidnsRowsDoNotIntroduceFormattingRewrites|TestModelRowsByteRoundTripAndTypedEdits|TestRawStoreBridgePreservesUntouchedFields|TestReferenceConvertImport|TestReferenceLegacyDefaults|TestReferenceConfigDefaults|TestConfigTemplateMatchesBackend|TestBackupsOfEverySupportedSchemaImport|TestN811ImportExportOpaqueBytes|TestCrossCheckFreshInit"'
```

最终代码检查退出 0：全仓 gofmt -l 无输出；api/rules/difftest 70.472s、api/rulelibrary/difftest 34.701s、httpx/difftest 30.076s；repo 定向 race 21.359s。九个原始失败测试全部被对应完整包覆盖，不跳过、不修改比较。

完整预检命令：

```sh
RT_HOST=sunbroke@192.168.0.32 rt sync
ssh sunbroke@192.168.0.32 'source ~/.rt-env && bash /tmp/preflight.sh .paseo__worktrees__302rx1dg__go-batch-16 -p 8'
```

完整预检退出 0，最后输出：

```text
== vet
== full non-race tests (-p 8)
PREFLIGHT PASS
```

两项根因与修复均在本提交中。无待完成项；未 push。新增回归文件 153 行，以及本报告。没有新增依赖、修改 difftest 比较或冻结期望；后续 batch compare/core 门禁由协调者继续执行。
