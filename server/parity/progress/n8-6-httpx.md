# N8-6 — native HTTP request binding

状态：实现与示范调用方完成，最终门禁待跑（ported，待独立复核）。分支 go-n8-6。
开工确认 go-n7-3a=c597cbf3 干净且不再改该分支；从 go=6b50151c 新建 go-n8-6。
已读 AGENT-BRIEF、AGENTS/PORTING/WORKFLOW、设计2.6/5.6/N8-6行及O6 X04。
无 push，无子 agent。

## 设计对应及接口

- `Ctx.Bind(dst any) error`：原生 encoding/json，非JSON媒体415，空/畸形/DTO错型400；
  返回同一 API 错误出口的完整错误体。Data缓存、decodeJSONBytes（BOM/UTF16/UTF32）共用。
- `Ctx.BindSilent(dst any) bool`：失败清零且false；合法JSON（包括null）成功true。
  先解入新值，整次成功才赋给调用者，防止 native UnmarshalTypeError 留部分旧字段。
- `httpx.Number`：原始JSON字面量/形态；IsIntegerLiteral/IsBool/IsString/IsNull/IsContainer/
  IsSet/Text/Raw/Int64。区别60、60.0、6e1、true、字符串、null/缺失、容器；不做int()转换。
  Int64用strconv原生范围，越界字面量仍在Raw里精确保留。字符串语法留N3，未动PyParseInt。
- Native Decoder.Token + InputOffset取得scalar RawMessage片段；容器只收集一次，最后一次
  编码再类型化解码。每层重复成员整体后值覆盖（包含object/array/scalar），不触发Go原生
  struct重复对象字段的“合并旧字段”。不反复逐层重编码。json.Valid先统一校验整个文档及
  native最大深度，避免Token接口绕过整体语法/深度检查。没有私有JSON词法表/解析器。
- 默认忽略未知字段；示范DTO用map[string]json.RawMessage保留精确key大小写及存在性，
  R226额外字段拒绝。Number/RawMessage保留错型值，由业务层返回原来的中文错误。
- N8-1唯一响应出口未变，非有限Go响应数字仍为null。

局部切换（协调者裁定，已写Clarified及N8-7行）：只新Bind/BindSilent及本步两个调用方
拒绝NaN/Infinity/-Infinity；旧JSON/JSONSilent/Force接口这一步不改。之后每迁移一个请求
调用方，同时在该步将其非有限请求改400，并在HTTP差分中明确写该项预期变化。

## 示范调用方（仅两个）

| 调用方 | DTO / 验证 | 保持与批准变化 |
|---|---|---|
| R226 PUT /api/tasks/interval | intervalRequest：精确key及两字段计数，Number.IsIntegerLiteral+Int64；暂以jsonx.Int接现有refresh/metrics validator，后续消费者迁移移除适配 | 60通过，60.0/6e1/bool/string/越界/container/null仍原中文400；额外字段/大小写/missing仍原中文400；写入规则及metrics setter不动 |
| POST /api/auth/login（strict示范） | loginRequest精确用户名/密码字段，RawMessage后字符串凭据比较 | 有/无charset及+json成功；正确登录/错误凭据/错型字段原中文401严格比较；畸形/非对象JSON由Python捕获的500改正确400，错误媒体500改415 |

其他§5.6调用方R015–17、R210/H511、R076/R047、R080/R090未迁移；绑定能力测试覆盖形态，
它们继续由原有HTTP差分覆盖，后续N8-7+迁移。

## HTTP验收与oracle

- httpx/difftest 实际Flask与Go服务器，新增登录frontend JSON两种charset、+json、
  UTF8-BOM/UTF16、错型字段、精确key；新增R226 int/fraction/exponent/bool/string/huge/null/
  container及重复/额外/key大小写输入。全部完整状态、头和JSON结构比较，共118个向量，17项已批准变化分别校验。
  复用已合入的yamlxtest.EqualJSON测试工具（无生产依赖，无复制实现），取代Python式bool/
  number相等；新增反例验证wrong status/message/type、missing/extra字段及Python oracle漂移都失败。
- 17个明确变化向量由 `testdata/n8_6_bind_gen.py` 记录真实Python login/R226响应；
  Go完整预期来自真实Flask strict JSON/畸形body控制请求。两端各自的完整状态和错误体
  都须匹配，不是统一normalize。成功/凭据错误/错型字段不进入变化分支。
- httpx/difftest测试壳不启动所有task rows，因此条件RegisterAlways挂真实R226 handler，
  仅用验证和资源不存在向量，不fake provider、不改生产wiring。**不是宣称生产注册依赖已完成**。
  成功200、实际保存、content规则源仍由已有refresh/difftest完整场景验证（真实handler与repo）。
- 独立单元测试覆盖嵌套重复对象整值替换、数组/标量替换、null/missing、silent清零、
  UTF16/BOM、精确保留scalar词形、任意大数字、非JSON媒体、非有限输入拒绝/响应null。
- FuzzBindNativeJSON 与原生encoding/json UseNumber的完整结果比较；错误/尾随/UTF编码/深度
  都走原生约束，合法值必须一致；新边界不走jsonx解码器。

## Agent ingestion后续清单（本步不改）

已同步PROGRESS-agents A04/A05：
1. heartbeat legacy reader → ValidateSystemMetrics/Manager.UpdateHeartbeat → AddMetrics hook →
   History.AddMetrics → pending JSONL/flush：入内存/文件前拒绝或丢弃非有限点，含转换溢出。
2. JSONL/readLegacy与seedMetrics/oddMetrics的历史NaN数据：请求绑定不会清理这些点；
   metrics/history/summary的N8-1显式NaN/Infinity→null差分保留。
3. POST traffic-report/traffic-history 的legacy ReadJSONBody（256KiB/4MiB）→history/store/
   aggregation：A05核查数值finite gate，点级拒绝/丢弃策略由该步决定。
4. 相应调用方迁移时，单独断言非有限请求→400，不能顺手全局改变其他未迁移路由。

## 已提交的可编译步骤

- e6fcfff4（原763843a0）：绑定核心/Number/原生性质fuzz。
- a0a86f26（原044fd3b6）：R226与login示范DTO及HTTP单元回归。
- f18ee230（原ca0e31bb）：HTTP差分/设计说明及局部非有限请求切换约束。
- 严格JSON比较与最终验收提交待补。

测试全在rt远端（.64）：
```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go vet ./... && go test -count=1 ./internal/httpx/... ./internal/api/auth ./internal/api/tasks && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/httpx/difftest ./internal/refresh/difftest'
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/httpx/testdata/n8_6_bind_gen.py'
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test ./internal/httpx -run=^$ -fuzz=^FuzzBindNativeJSON$ -fuzztime=60s -parallel=4'
```
最近阶段结果：gofmt/vet静默；httpx1.631s、auth0.015s、tasks0.083s；启用diff
httpx1.084s、refresh2.988s；17向量fixture录制成功；fuzz752514 exec、PASS61.017s。

发现并修正的测试配置问题：auth内部测试引入httpxtest造成cycle，改外部测试并通过harness
Options.Auth配置凭据；当前示范提交已修订为可编译。R226在httpx壳中因缺slots返回404，
现按上文挂实际handler，未掩盖路由缺口。没有修改baseline或使用宽泛normalize。

已在 .64 完成 rebase 到最新 go（含N7-3a/C31）；只发生文档追加冲突，双方记录均保留。
严格比较下最新httpx差分：118 requests，0 unexpected differences，PASS1.470s。
最终全门禁随后在同一台 .64 执行；不将阶段结果当最终全量结果。

最终门禁首轮 .64：gofmt/vet静默，race httpx35.426s、api/auth1.237s、api/tasks2.267s全过；
全量发现internal/auth/http_test.go仍要求登录畸形请求500，这是本步已批准的旧预期。
该测试改为400/415及精确错误体，.32 auth0.096s、api/auth0.019s通过（无产品变更）。
新版本最终完整门禁改固定 .32 运行；不将旧预期失败的 .64 全量当作通过。
