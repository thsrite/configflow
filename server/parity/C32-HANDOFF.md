# C32 规则路由移植（门禁前检查点）

分支 `go-port-c32`，从 go `d3124353` 建立；不修改已交回的 A24 分支。不派子 agent、不 push。

## 完成

全部21个method实现，`routes.tsv`已标done；列表/CRUD、batch/reorder/group/policy、compose、DNS、两个公开provider、match-test、find-duplicates。`server.NewApp` 全应用注册无冲突。

复用 C09 syntax/reorder/lazy matcher、C14 source validation/fetch/cache/delivery、refresh task owner、C27 DNS/native routing。核心为 native DTO/RawMessage，jsonx仅在legacy adapter边界。写入/缓存owner lifecycle.Detach；无handler WithoutCancel、无新后台goroutine。SQLite事务负责共享库+profile+DNS原子性；缓存失败只警告，不回滚已提交配置。

提交：`ed7f5f26` A16 hook；`847da438` 16方法；`02693e11` compose；`a851172c` provider/match/duplicates；`6656e51e` real Flask+SQLite/files differential及共享C27历史名称修正。当前边界补充即将提交。

协调者批准的延期：agents.Hooks.PinnedWithheld由A16注册。无符合条件的auto-sync OxiDNS确定false；需要投影但hook nil/error，单条脱敏warning、省略字段、保存不回滚。所有分支有测试。PROGRESS A16和C32已注明，不跳过其他字段。

## 发现及修正

- 中：C27 Agent read-model将历史数字/数组名称输出null；Name改独立json.RawMessage，数字/数组/null实测和Python一致，返回字节隔离。协调者已批准；C27 PROGRESS已说明，TestRuleDNSAgentLegacyNames及真实HTTP回归。
- 中（Python bug）：授权HEAD /api/rules/dns带body会写DNS。Go仅PUT写入；HEAD列表也修复Python500。真实Python旧写入与Go全状态不变均有断言，记入behaviour-changes。
- C32重排不能套其他路由严格OrderIDs：保留Python旧对象、非字符串/重复/未知ID和position结果，共享排序算法不复制。
- C14接受的11种文本行边界在C32matcher统一经共享RuleLines适配；只改index输入，不改缓存或通用C09语义。

## 验收

详见 [difftest-c32.md](difftest-c32.md)：全21方法成功/失败；真实Flask状态/响应/标头；每请求后SQLite sections/fields全行、export、所有JSON/YAML/list文件。native_config与标记YAML交付比结构，raw来源/缓存保留原文；随机ID一一配对并验证派生文件/tag和引用，计时用确定时钟。没有baseline改动。

7个Python测试文件逐组映射在该清单；C33/C18/C28/A11非C32路由明确归属，不复制。包括template/native/custom、DNS follow/park/actions/typeguard/deletion、250条policy组、remote compose 200/400/502、行分隔/通配符查询、代理原始GET/空body/Proxy-Authorization、2万条重复无截断、中央凭据脱敏、取消owner与并发重复检查。FuzzPastedRules待最终60秒门禁。

最近命令（固定.64，均通过）：
- `RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -p 6 -count=1 ./internal/api/rules'`：1.700s；随后1.148s。
- `RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -p 6 -count=1 ./internal/api/rules/...'`：API1.148s，diff39.194s。
- remote compose增加后：`CONFIGFLOW_DIFF=1 go test -p 6 -count=1 ./internal/api/rules/difftest -run ComposeRemoteLibraryAtomicValidation`：1.619s。
- shared C27 RuleDNS/concurrent snapshot1.709s；新增LegacyNames0.008s。

## 剩余

1. 提交可编译边界补充；git rebase go，解决仅本任务相关冲突，保留所有integration更新。
2. 先跑改动包测试/差分，随后最终必须用最新 `server/scripts/gate.sh -p 6 FuzzPastedRules:./internal/api/rules`（协调者最新：.64内存20GB+swap，禁止自行并行全量/proddiff；gate自动最多两轮排队）。包括gofmt/vet/tag builds、全race、proddiff、全部difftest和fuzz；记录host/load与末尾输出。已知aggregation provider3条差异由别的agent处理，不擅自baseline。
3. 若用户明确要求普通全量额外跑，必须同样受gate的slots约束；不要绕队列。
4. 最终更新PROGRESS为ported（不verified），重写本报告为最终结果，提交后git rebase go、干净、无push，报告hash及所有延期。

变基到go9a208c6c完成，文档追加冲突保留双方。启动接线A19现有SetRulesetContent：api/rules.Wire复用sourceContent和传入publish快照，取消调用仍由owner收尾，server/gateway集成测试通过1.944s/0.057s/1.604s。即将启动gate.sh -p 6。
