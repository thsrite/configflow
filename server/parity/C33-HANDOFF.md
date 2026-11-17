# C33 — 规则库和策略组路由交回

当前状态：实现与针对性边界检查完成，最终 gate 正在 .32 跑全量 race；proddiff 已通过，后续 live difftest/fuzz 等 gate 顺序执行。结束后补齐最终结果。
分支 `go-port-c33`，已含 verified C32，当前变基到 `go=895f5e21`（门禁临时文件使用磁盘 TMPDIR）。不 push，不改已交回的 C32 分支。

## 范围

全部 25 方法已接入 `server.NewApp`，`routes.tsv` 标为 done，逐条成功/失败和 Python 测试归属见 [difftest-c33.md](difftest-c33.md)。没有剩余未实现的 C33 路由。

- `backend/routes/rule_library.py`：共享库 CRUD、draft-content、check-content、cache、proxy-domains、reorder、test、test-single，以及全局/配置空间两个公开内容入口。
- `backend/routes/proxy_groups.py`：全局/配置空间集合、PUT/DELETE、batch-health-url、health-defaults、preview-regex、reorder。
- Python `test_policy_editors.py` 的内容编辑半边在本模块；RouterOS manifest/authoring 半边继续由 R08/R09/R10 负责。没有新增 RouterOS 写操作。

实现用 native `model.Fields` 保留未知 JSON 成员，具体 DTO 描述响应和工作结果；仅两个生产 adapter.go 接 legacy repo/引擎。复用 C09 candidates/reorder/regex、C14 rulefetch、C15 refresh/taskstatus、C16 health/check、C11 listeners/filter、substore cache、C27 DNS apply/follow 和 C26 native source rebase，没有私有算法副本或新 repo 更新原语。YAML 走 yamlx，native download rebase 前拒绝 D1/alias policy，只有 ErrSyntax 保留旧回退。

## 结果与发现（按严重程度）

1. **High，已修：C33 自有提交点关机后仍可能写入。** 每个 owner Detach 后检查 ctx，进入共享 owner 前及自有事务 callback 首尾检查；暂存、receipt、fsync、删除、rename 显式带 ctx。真实 Runtime.Start/BindRequest/Shutdown 覆盖 15 类入口；停止后每类重复 8 次无落地/网络，断开请求仍完成。另覆盖 DB 已提交但 rename 前关机，阻止之后的缓存发布；失效标记保留，下一代 owner 修复缓存和该源暂存文件。
2. **High，明确延期到 O3-6c：共享提交点/锁等待窗口。** 按协调者分工，C33 本轮不改共享提交实现，仅在调用前检查。`taskstatus.Store.Forget` 及 refresh 内的 Record 无 ctx 锁内提交检查；repo 最终 commitStore 和 rulefetch cache touch/rename 也由共享 owner 补。责任人 `6e5c7760` 在 O3-6a 后统一添加 ForgetContext/RecordContext、repo/cache 提交检查。新入口合入后 C33 切换并补共享锁等待期间关机测试。这是已知剩余窗口，不能据本交回宣称共享层已修或设为 verified。
3. **High，已修 Python 可靠性 bug：新 DB + 旧稳定缓存。** draft/内容 PUT 先暂存并 fsync，提交前留下持久失效标记，提交后原子 rename、同步目录后清标记。写入/rename 失败返回 500；未知提交故障保留标记，稳定和 legacy 缓存均不能复活。交付从当前 DB/inline 重建或直接使用，手动缓存可修复标记。明确校验拒绝不修改 DB。N8-5 的正式事务 API 落地后重写本协议。
4. **Medium，已修 Python 并发 bug：批量测试保存旧快照。** HEAD 后只在新事务中关闭 ID+完整声明仍匹配的失败源；测试期间的另一个 PUT 不会被覆盖。复用 PUT 的 native profile rebuild/parking，同一事务提交源 gate 和 DNS 状态。
5. **Medium，已修 Python HEAD 副作用。** draft 的授权 HEAD 带合法 body 在真实 Flask 中会写入；Go 只把 PUT 当写入。集合 HEAD 走读取，避免 Python 未返回响应导致 500。
6. **Low，已修 Python 选择验证 bug。** 非数组 rule_ids 会被 Python 当字符串子串/对象键筛选；真实 Flask 证明 `manual` 会选中 `a` 和 `manual`。Go 400 拒绝并保持状态不变，调用方传字符串数组；前端原本即如此。数组内未知值仍按旧语义忽略，完全无匹配 404。

## 有意差异

除上述 bug 修正及已批准的 D1/D2 外，没有遗漏有效请求功能。中文消息、状态、响应值和持久化结果通过真实 Flask 对比。内部英文诊断保持 Go 原生，不复制异常类/错误文本；仅具体畸形请求按已观察 rejection/status 处理，其余字段、中文和存储严格比较。

D2：draft revision 用 native encoding/json 递归排序后 SHA-256，不复刻 ASCII 转义/数字拼写。旧页面可能一次 409，刷新即可；不持久化。真实 Flask/Go 各自验证稳定、变更失效、并发同 token 只成功一条。生产 normalize 仅精确 draft 路由顶层 revision、`^[0-9a-f]{64}$`，格式/类型/缺失字段/其他字段/其他路由有负控；baseline 未改。

## 边界验收

- 每请求比较状态、JSON、Content-Type/Cache-Control/配置空间 header，完整 SQLite sections/fields/位置、export、JSON/YAML/list 文件。
- native template/follow/nested groups、locked-native rollback、停用/恢复/retype parking、inline→URL→新 URL 更新真实 Python 一致，另断言 native draft 地址确实更新。
- 监听入口 rename/disable 比较真实 Python，另断言用户注释保留；C11/N7-2 relink 差分重跑通过。
- 回环 forward proxy 比较真实 GET URI、空 body、headers，Python/Go 共 4 个代理请求、0 个直接 origin 请求；证明全局下载设置传入。下载失败保留原配置和 raw cache。
- C28 两个延期回调已经去掉 `deferTo("C33")`，正常严格比较通过。
- FuzzEditableContent 覆盖 editable YAML/文本及递归/重复键种子。注册时同时装配 rules、aggregations 和整个 server，ServeMux 不冲突。

## 已完成的针对性检查

`.64`：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go vet ./internal/api/rulelibrary/... ./internal/api/proxygroups/... && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/rulelibrary/... ./internal/api/proxygroups/... ./internal/api/rules/... ./internal/rules/... ./internal/rulefetch/... ./internal/mosdns/difftest ./internal/subs/difftest ./internal/server/...'
```

相关全绿记录：C33 API7.861s/diff43.144s/proxy7.696s，C32 API35.714s/diff160.883s，rules16.191s，rulefetch34.617s，C28diff22.046s，C11diff13.633s，server1.868s。其后新增 Runtime 守卫检查 API22.474s/diff33.371s/proxy4.259s/server1.435s 全绿。最后附加畸形输入/源地址/代理路径的 C33 live race 分别58.314s、26.351s、30.245s通过。

`.32` harness：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc '~/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py && ~/cf-test/.venv/bin/python -m unittest discover -s server/tools/proddiff -p test_proddiff.py'
```

两种入口分别 `Ran 36 tests ... OK`；正负控均执行。没有用新 main block 跳过旧测试。

## 最终 gate（进行中）

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GATE_LOGS=/home/sunbroke/.cache/rt-tmp/c33-gate-20261118-final -- bash -lc 'test -n "$TMPDIR" && cat /proc/loadavg && server/scripts/gate.sh -p 6 FuzzEditableContent:./internal/api/rulelibrary && cat /proc/loadavg'
```

开始：`13.76 23.34 36.29`，.32 前台 session9396，slot1。gofmt、全仓 vet、双tag均通过，race/proddiff并行；proddiff已完成：89 GET模板/91 method-path、1061实际比较、coverage_regressions=0、new=0、stale=0，23项自检PASS，aggregation-provider2/2、agent-deployment-bundle3/3。其load开始15.10/22.61/35.51、结束32.58/26.23/35.35；race中间观察41.89/31.57/33.75。

.64 的两次排队均在检查开始前取消：session30829没有后来新增的磁盘TMPDIR；session79393一直等slot，而 .32 有空slot，故转机且不增加并发轮次。.32第一轮gofmt发现自动合并的server.go导入块，纯格式修正后启动上述正式轮次。没有绕过gate限流、调大timeout或人工施压；业务逻辑与 comparator/normalize 冻结。

## 文件与提交

核心新增：`internal/api/proxygroups/`、`internal/api/rulelibrary/`；注册 `internal/server/server.go`。共享读侧仅为 cache invalid receipt 接入 `rules.RuleCacheReadPath` 和 rulefetch read path；没有擅改共享 write/commit 关机语义。`api/rules/read_adapter.go` 的 inline 缓存失效后从 DB 交付，保留 cache→inline 既有顺序（正常 cache 不受影响）。

新增测试及记录：C33 两包的 unit/race/runtime/fuzz、rulelibrary/difftest（真实 Flask worker）、本报告、difftest-c33.md；更新 PROGRESS、routes.tsv、behaviour-changes、proddiff normalize/README/自检。新增 proxygroups 核心6文件1268行、native测试1文件178行；rulelibrary核心9文件2019行、native测试5文件910行、live差分及Python共8文件1224行。最终 tip 和最后 git rebase/干净检查在 gate 后补。
