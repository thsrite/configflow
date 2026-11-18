# MCP #39 移植交回（待 Opus HR）

分支 `go-port-mcp`；最初基于 `go@03257978f`，完成后 rebase 到本地
`go@95440b69e`（新增 remaining-work 盘点，产品代码相同）。产品提交 `205c116b0`，凭据容器补强 `02a39c5a8`。
状态 **ported，未 verified，等待独立 Opus HR 复核**。本次没有子 agent、push、生产访问或完整门禁。

## 范围与实现

完整读过 Python `backend/mcp_server/{server,auth,invoker,tools}.py` 和
`backend/test_mcp_profiles.py`、`backend/test_mcp_tool_coverage.py`。

- `internal/mcp/transport.go`：POST `/mcp`、`/mcp/`；GET/DELETE 两种路径均返回显式
  JSON-RPC -32601 / HTTP405 无状态说明。框架仍处理 OPTIONS/HEAD。
  initialize 三个版本协商、ping、tools/list、tools/call、批量、通知202、空批400、非对象400、
  parse400、未知方法/工具、业务 isError、内部错误兜底全部接通。通知不执行工具。
- 53个工具均实现。`catalog.json` 从真实 Python `list_tools()` 生成描述/schema，
  不是手工删减的工具子集。`tools.go`/`crud.go`/`settings.go` 只实现 MCP 自有的
  路由选择、必填检查、ID分配和部分更新适配；业务验证、事务、文件/设备写入全由既有 REST owner 执行。
- `invoker.go` 使用 `App.Invoke` 在进程内调用，固定 localhost、本进程内部能力 header、
  每工具选定 profile header；从不把外部任意 header、method、URL当作调用权限。
  GET/DELETE 的 JSON interface 确实为 nil，原始 body 和 Content-Type 均为空。
  支持 JSON、文本、空响应、非法UTF-8二进制摘要和 >=400 消息提取；不自动跟随重定向。
- 原生 DTO、encoding/json、json.RawMessage 开放资源字段、共享 model.String 文本读取；
  核心没有 jsonx/pystr/PythonClass、Python异常文本、私有 scalar coercion 或动态 Python 参数形状。
  所有 I/O 方法 ctx 在首参，没有把 ctx 存入结构体。
- `server.go` 注册及 `routes.tsv` 六行完成；不动认证公共入口白名单（已有 MCP 例外）。
  用户说明在 `doc/manual/mcp.md` 补充协议与无状态响应，功能范围保持一致。

## batch-7 依赖核对

Python 本次53工具没有 router 工具，也没有调用 batch-7 新增的
`oxidns-ha`、`oxidns-ha-environment`、`gateway-installations`、`ipv6-readiness`、
`routers`、`client-devices` 或 `upstream-routers` 路由，所以本次没有暂缓接线的工具。
Agent 工具实际使用的既有入口如下，基线已接通，94条适配参考包含全部 action：

- agents 列表、单条 GET/PUT/DELETE、status；绑定profile通过既有单条PUT。
- restart、push-config、uninstall、update。
- logs、logs/clear、logs/validate、config/logging GET/POST。
- metrics、metrics/summary、metrics/history、traffic/stats、traffic/trend。
- install-script、docker-agent-compose、docker-agent-run、latest-version。

没有为了覆盖新路由私自扩充 Python 的工具集合；后续若新增路由器工具，应另开功能任务。

## 认证、profile 和事务

1. 读取同一 repository 的系统配置，按严格 Bearer parser 和8192字符上限处理；
   配置令牌支持 bearer/query，开启登录才接受JWT；两者都未启用时放行，符合既有选择。
2. **当前和退休 rule-proxy token 优先拒绝**，即使另一种凭证正确，或旧库令牌与config_token相等。
   不把规则下载能力升级为MCP管理能力。配置令牌比较使用SHA256固定长度常量时间比较。
3. 存储凭据不能使用R5零值/数字文本作权限证据。补过失败回归：损坏token=true/对象/数组
   会被R5读为空，初版会匿名放行；数字123会被当作新字符串token。现从model保留的原始JSON
   严格读取配置凭据（包括system_config容器本身），非字符串损坏值拒绝配置令牌认证，仍允许开启登录时的有效JWT。
   回归还包含system_config非对象、config_token等于内部token的旧库状态，直接在合成SQLite字段导入，未绕业务writer伪造成功。
4. 工具profile来自 `arguments.profile_id`，省略/空字符串取当前默认空间；外部浏览器
   `X-ConfigFlow-Profile` 不参与工具选择。每次内部请求携带同一选择，profile专属URL仍由owner判定。
   顶层对象结果仅在缺失时补profile_id；列表/文本/二进制不伪造包装。全局资源不因profile参数变局部。
5. CRUD默认保留当前字段，health_check嵌套合并保留false；显式已有空health_check不误回退legacyURL。
   proxy-group update直接提交partial到事务owner，compose_rule/set_rule_dns保留单次事务。
   reorder只发ids/position，不把列表展示字段回写。其它read-modify-write及create后bottom-reorder
   仍是Python的多请求边界，不另套全局事务，不跨HTTP回调持有仓储锁；第二步失败不伪报全体成功。
6. 每个工具入口Detach一次，随后每次内部API调用前检查原owner ctx；API owner继续在最终提交点检查。
   没有后台job或新Runtime generation。真实Runtime.Start/BindRequest/Shutdown回归在subscription GET
   完成后暂停：关机后不再PUT，整个shared含revision/字段字节不变；断开请求后实际PUT落盘成功。
7. `_text_content` 复用shared redact和同repository的实时系统配置。保留既有管理员配置令牌读取合同，
   不把config_token当作隐藏字段；rule-proxy当前/退休token、Agent内部token仍被删除/脱敏。
   用户名不是新秘密。内部异常不返回panic/堆栈/底层敏感文本，日志只用固定消息。

## review-r04/r05/a19/r06 问题类逐条核对

| 历史发现 | 本次防护或边界 |
| --- | --- |
| R04 F1 额外批量ID导致panic/漏记已写对象 | MCP不解析设备批量回执；由原设备owner处理。批量RPC逐条安全分发，无ID下标配对。 |
| R04 F2 回滚中文/错误类型错 | 保留REST的HTTP状态和message/error/msg，转业务isError；不创建新的回滚错误类型或自拼设备回滚文本。 |
| R04 F3 断开中止已提交批次/补偿 | 单工具Detach保留关机generation；真实Runtime读后写前屏障覆盖关机与断开正控。设备补偿留原owner。 |
| R04 F4 清理回滚N次往返 | MCP无自行回滚循环；管理动作单次调用既有owner，不替换其批量恢复。 |
| R04 F5 动态NAT错误类别 | 无NAT操作重写或动态行判断，保留owner业务错误。 |
| R04 F6 已删除对象被误记skipped | 不把未完成调用标成功；任何内部HTTP失败都传isError，多步适配不伪造设备阶段状态。 |
| R04 F7 数组/对象比较panic或坍缩 | 开放值保留RawMessage；不对动态interface做==，业务对象merge不转成空串；测试比较用native JSON树。 |
| R04 F8 恢复脚本无32KiB预算 | MCP不生成路由器脚本；重用拥有脚本预算的原owner，未缩短/覆盖它的参数。 |
| R04 F9 缺预览快照仍写 | MCP不提供绕过plan/expected的设备入口，未新增batch-7路由工具；已有Agent push走完整deploy owner。 |
| R04 F10 重复网络/证明helper | 未复制路由/网络/设备证明业务；只保存MCP特有的partial/reorder适配。 |
| R05 F1 URL userinfo/Basic丢失 | 没有修改共享设备HTTP driver；MCP内部调用无需外部Basic，Agent现有owner继续携带设备凭据。 |
| R05 F2 英文普通错误跳过rollback | HTTP业务失败统一apiError并保留中文；不捕获设备错误后绕过原owner补偿。 |
| R05 F3 无tag归属仍add/undo | MCP不做设备add/undo，输入ID限定单路由段，当前记录ID严格字符串匹配；设备归属仍由owner证明。 |
| R05 F4 按复用ID盲目恢复旧值 | 不实现或触发额外恢复，不拿工具返回ID充当设备对象归属证明。 |
| R05 F5 二次方read-back | 无设备read-back复制；CRUD查找一次线性遍历，生成ID一次索引taken集合，适配参考锁定请求序列。 |
| A19 F1 alias放大/循环失控 | MCP不解析/展开YAML，所有YAML交给共享facade/转换owner；自有Oxi JSON深merge有128层边界，输入原生JSON无循环。 |
| A19 F2 document wrapper/非mapping静默成功 | 不接收YAML Node API；外层RPC/arguments必须为对应JSON shape，错误不会变成空的成功transport。 |
| A19 F3 Unicode空白误判清空capture | 无规则正文检测/剪枝复制，规则请求交C32/C33及共享reader；RawMessage保留业务正文。 |
| R06 F1 写和补偿无超时/fake篡改参数 | 不新建设备客户端、不覆盖原owner超时；测试只替换REST回执或使用实际路由，Runtime测试未替换工作参数。 |
| R06 F2 把用户名当秘密 | 使用shared ExternalPayload redactor，用户名operator正控可见，内部token负控不可见；不增加私有用户名黑名单。 |
| R06 F3 GET/DELETE JSON null | 显式在body非nil时才赋InvokeRequest.JSON，94条回放逐个读取原始body和Content-Type断言为空。 |
| R06 F4 字符串化错误类型后接受 | 请求DTO严格类型、profile/token/资源ID权限判据明确检查原token，错类型不会由model.String变成有效身份。 |
| R06 F5 map模拟typed业务 | envelope/content/arguments/schema是DTO；自由配置仅保留RawMessage扩展，业务plan/rollback不复制到map。 |

## 原生边界差异（明确列出）

正常请求、业务中文、权限和profile合同无新增差异。以下只涉及非法/损坏输入：

- 非字符串config_token一律拒绝该凭据，不因R5成为匿名/数字token。Python对某些假值如0/false/空容器
  会当未配置；Go保守拒绝，不能从坏配置推导管理授权。
- arguments/params及工具primitive字段用DTO验证；false/0/空数组不模拟Python `or {}`。
  非对象arguments用原中文-32602，字段错类型返回清楚的工具400，而不是仿真Python内部TypeError/ValueError文本。
  unknown name/method、正常必填业务错误仍走原分支。
- 原生JSON拒绝NaN/Infinity，内部异常文本采用固定中文；ID中路径分隔符/`.`/`..`不能改写目标REST路由；
  Oxi嵌套对象超128层拒绝，避免递归放大。无有效已支持输入因此失去功能。
- JSON text content编码采用原生JSON；比较完整解析值，不追空白/转义/对象字段顺序。

## 验证和测试映射

所有检查固定 `RT_HOST=sunbroke@192.168.0.32`，GOMAXPROCS=4，Go `-p 4`。

- **94条真实Python工具适配参考，53/53实际执行**：含所有CRUD分支、health false保留、创建后排序、
  备份、全部Agent action、日志/metrics/query、DNS/MosDNS/Oxi设置。比较实际method/path/body/query和结果；
  不把mock REST比对说成设备成功验收。Python `tools.call_tool` 的default profile/setdefault实际运行。
- **35条真实Flask HTTP + 实际仓储状态**：initialize版本、ping、全部catalog、订阅partial/health、
  reorder、策略组事务partial、profile创建和隔离、profile备份、未知tool/method、参数非对象、必填缺失、
  非法RPC、混合batch、通知不执行reset、空batch、非对象body、坏JSON、认证失败、config bearer/query、
  内部token、405四入口、匿名、登录未带JWT/有效JWT。每条都比较default的订阅和策略组存储。
- Python profiles测试映射：profile创建/列表空间选择走live HTTP；bind_agent_profile走53工具回放，
  Agent PUT字段语义沿用已验证api/agents。Python coverage测试中的reorder/partial/健康/DNS/链/compose
  均映射到本层适配回放+既有owner测试；本次没有重跑全部依赖的HTTP/设备矩阵冒充MCP覆盖。
- 新Runtime回归、坏存储凭据导入、共享脱敏不改原payload、明确空health对象正负控；新transport fuzz60s。
- 一个实际参考索引负控：临时改第0行out.profile_id，真实回放必须失败，finally恢复原文件；不跑mutation矩阵。

初期fixture缺动态必填参数/查询文本类型、oracle根目录和进程cleanup顺序
均已纠正；不是对产品差异增加mask。对照只规范化created_at/updated_at和嵌入JSON文本格式，存储字段不mask。
没有生产快照/fixture或全门禁。


## 最终验证命令与结果

Python适配参考生成（原生Python业务，94例/53工具）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/mcp/testdata/adapters.json -- bash -lc 'set -e; PYTHONPATH=. $HOME/cf-test/.venv/bin/python server/internal/mcp/testdata/record.py "$TMPDIR/mcp-adapter-reference"; cd server; go test -p 4 -race -count=1 -run TestPythonToolAdapterContracts ./internal/mcp'
```

```text
Python adapter cases: 94 tools: 53
ok configflow/internal/mcp 2.422s
```

合并到当前go基线后的主检查（退出0，gofmt/vet无输出）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/mcp internal/server/server.go)"; go vet -p 4 ./...; go test -p 4 -race -count=1 -timeout=5m ./internal/mcp/... ./internal/server; go test -p 4 -count=1 -run "^$" -fuzz "^FuzzTransportJSON$" -fuzztime=60s -parallel=2 ./internal/mcp'
```

```text
ok configflow/internal/mcp          5.951s
ok configflow/internal/mcp/difftest 6.027s
ok configflow/internal/server       9.243s
PASS
ok configflow/internal/mcp         61.125s (21412 fuzz executions)
```

命令首部实际先 `gofmt -w internal/server/server.go` 并用 `--back` 取回，修复注册行的import排序。
其后的参考索引负控代码如下（同一rt命令里执行，退出0）：

```python
import json,pathlib,subprocess
p=pathlib.Path("internal/mcp/testdata/adapters.json");original=p.read_bytes()
try:
 rows=json.loads(original);rows[0]["out"]["profile_id"]="synthetic-negative-profile"
 p.write_text(json.dumps(rows))
 run=subprocess.run(["go","test","-p","4","-count=1","-run","^TestPythonToolAdapterContracts$","./internal/mcp"],text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
 assert run.returncode!=0 and "synthetic-negative-profile" in run.stdout and "out got" in run.stdout,run.stdout
 print("Python adapter indexed negative control: rejected changed profile_id")
finally:p.write_bytes(original)
```

```text
Python adapter indexed negative control: rejected changed profile_id
```

之后补了system_config整个容器损坏的检查与三例回归；最终产品树再次通过以下必要增量检查，
没有重复与该存储种子无关的fuzz：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 --back server/internal/mcp/transport.go --back server/internal/mcp/security_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/mcp/transport.go internal/mcp/security_test.go; test -z "$(gofmt -l internal/mcp internal/server/server.go)"; go vet -p 4 ./...; go test -p 4 -race -count=1 -timeout=5m ./internal/mcp/... ./internal/server'
```

```text
ok configflow/internal/mcp          6.198s
ok configflow/internal/mcp/difftest 5.376s
ok configflow/internal/server       8.225s
```

移植过程中坏token的失败回归确实先失败：true/对象/数组匿名200，数字123以Bearer123得到200；
修正后全表401。Runtime关机的固定错误日志是预期取消，不包含输入或凭据。
未做真实设备执行、L2–L6、完整门禁；独立HR和batch集成门禁由协调者继续安排。
