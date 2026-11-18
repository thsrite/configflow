# CFG-01 独立安全复核（附 CFG-03、CFG-09）

复核者：Claude Opus 5.5（非作者）。分支 `go-review-cfg-c14`（基于 go@d43cca6e，含作者提交 CFG-01 0d543f53、
CFG-03 47881419、CFG-09 185d8a6f）。只用 rt（.32）、合成数据和 loopback；没有 push，没有开子 agent，没有访问生产。

## 结论

- CFG-01 的修复方向正确：内部分类改用原始写法，边界在整条重定向链中保持，每跳在 I/O 之前校验。但还有 **1 个同类绕过（high）**
  和 **1 个主机名大小写差异（medium）** 是 Python 拒绝、Go 放行；另外作者「路径含反斜杠一律拒绝」误伤了合法内部地址（medium）。三处都已修复并补了测试。
- CFG-03、CFG-09 未发现问题。CFG-03 补了 20 个真实 Python 对照向量。
- 实际影响：rule-proxy 需要 token，调用方本来就能直接传任意外部 URL（Python 注释写明这不是公网 IP 闸门）。
  内部边界实际只决定「是否绕过下载代理、是否进程内分发」。所以下面的绕过不会让攻击者访问到更多目标，但它是 P1 工单要求对齐的同一条边界，仍按 high 处理。

## 方法

新增真实 Python 对照（`server/internal/rulefetch/testdata/gen_cfg01_review.py` → `cfg01_review.json`）：

- **network**：64 组，真实 `request_rule` 对两个 loopback fake（源/外部，IPv6 用 `::1`）。路径以 `/landing` 结尾的请求算作禁止目标。
  Go 侧 `TestCFG01ReviewNetworkAgainstPython` 用同样的 fake 跑真实 `HTTPTransport`：Python 拒绝的，Go 必须拒绝、禁止目标 0 次，而且错误类必须是 ValueError（路由返回 400 `Invalid remote URL`）；
  Python 正常放行的，Go 的状态码和 body 必须一致；例外逐个列名（见下文「Go 更严」「Python 也不防」）。
- **classify**：20 组主机写法（IDN、尾点、数字 IPv4、IPv6 等）。这些写法不经 DNS 就连不上，所以用真实 `is_internal_rule_url` 作基准；
  Go 侧 `TestCFG01ReviewHostClassification` 用 fake transport 断言：凡 Python 判为内部的，Go 都在发出任何请求之前拒绝。

覆盖面：`/./`、`/../`、`%2e`/`%2E`/混合大小写、`.%2e`、`%252e`、三重编码、`%2f`、`\` 与 `%5c`、`//`、`///`、`;`、userinfo、
端口前导 0、显式默认端口、IPv6 字面量、十进制/八进制/十六进制/短写 IPv4、`0.0.0.0`、`localhost` 大小写及别名、IDN 大写/punycode/全角、尾点 `host.`、
主机中的百分号编码、fragment、query 中嵌套 URL、第 3/4 跳才逃逸、301/303/307/308、`file:`/`data:`/`gopher:`/`ftp:`、相对 Location（`ok`、`../landing`、`http:ok`、`http:/landing`）、
scheme-relative（`//host`、`\\host`、`/\host`）、Location 里带 tab、`server_domain` 带路径前缀 `/cf`。

## 发现

### HIGH-1 外部源用绝对 Location 跳进 `/api/../`，下一跳逃出 API

| | |
|---|---|
| payload | 外部源 `GET /source` → `302 Location: http://<panel>/api/../redirect`（或 `//<panel>/api/../redirect`）→ 面板 `/redirect` → `302 Location: http://<external>/landing` |
| Python | `urljoin` 对带主机的 Location 原样返回，按写法 `/api/` 判为内部 → 拒绝离开 API 的下一跳：ValueError，400，禁止目标 0 次 |
| Go（0d543f53） | `joinURL` 先 `weburl.Prepare` 消除了 dot segment，拿 `/redirect` 分类成外部，于是跟随到 `/landing`：200，禁止目标 1 次 |
| 修复 | `12925a10b`：`joinURL` 额外返回 Location 原始写法（仅当它带主机时），`prepareRuleTarget` 依次用「原始写法、请求 URL、规范化 URL」分类，任一命中即进入边界。修复后两例都在请求 `/redirect` 之前 400 |
| 测试 | `ext-into-api-dot`、`ext-into-api-scheme-relative`（修复前失败，修复后通过）；`ext-into-api-encoded`、`ext-into-api-ok` 作对照 |

作者的用例都是「链从内部开始」，没有覆盖「链中途从外部进入内部」。

### MEDIUM-1 非 ASCII 主机名大小写不同，被判为外部

| | |
|---|---|
| payload | `server_domain=http://bücher.example`，URL `http://BÜCHER.example/api/../redirect` |
| Python | `urlsplit().hostname` 用 `str.lower()`，Unicode 也转小写 → 判为内部 → 边界生效 |
| Go（0d543f53） | `weburl.Hostname` 只转 ASCII 小写 → `bÜcher.example` ≠ `bücher.example` → 外部 → 跟随重定向 |
| 修复 | `12925a10b`：`originOf` 比较主机名时，非 ASCII 名称先转成 `idna.Lookup` 形式，和 net/http 实际拨号用的形式一致（不是模拟 `str.lower`）；IDNA 拒绝的名称仍按原文比较 |
| 测试 | classify `idn-upper`（修复前失败）；同时让 `idn-punycode`、`idn-fullwidth`（`ｐａｎｅｌ.example`）也被判为内部，比 Python 严格（这两种写法 requests 也会拨到同一台主机，Python 不防） |

### MEDIUM-2 作者的反斜杠规则误伤合法内部地址

| | |
|---|---|
| payload | `http://<panel>/api/rules/local/a\b`、`…/a%5Cb`、`…/api/a\..\b`（规则名是任意显示名，可以含 `\`） |
| Python | 判为内部，进程内分发，200 |
| Go（0d543f53） | `internalBoundary.validate` 看到 `\` 就拒绝 → 400 |
| 修复 | `9aff9ee27`：路径必须**两种读法都**留在 API 前缀内：`\` 当名称字符读一次，当分隔符读一次（`\`→`/` 后 `path.Clean`）。`/api/..\landing`、`/api/x\..\..\landing` 仍然拒绝 |
| 测试 | `legit-backslash-name`、`legit-encoded-backslash-name`、`legit-backslash-dot-inside` 通过；`redir-backslash-traversal`、`redir-encoded-backslash` 仍拒绝 |

### LOW-1 主机别名不被识别为面板（Python 也不防，不修）

`localhost` 与 `127.0.0.1` 互为别名、IPv6 展开写法 `[0:0:0:0:0:0:0:1]`、尾点 `panel.example.`、`2130706433`、`0177.0.0.1`、`0x7f.0.0.1`、`127.1`、
`0.0.0.0`、`[::ffff:127.0.0.1]`：Python 和 Go 都判为外部，`/api/../` 之后的重定向不受限（network 用例 `alias-ip-for-localhost`、`v6-expanded`：两端都是 200，禁止目标都是 1 次）。
差异：Python（glibc）能把十进制/八进制 IPv4 解析到 loopback，Go 的解析器不能，Go 反而连不上。
语法层面的 origin 比较本来就做不到穷尽。
**建议**：如果以后要把内部边界当成真正的 SSRF 控制，应该解析后按 IP 比较面板自身地址，并在 `net.Dialer.Control` 中再次校验，防 DNS rebinding。这是新功能，不在本票范围。

### LOW-2 DNS 解析后的 IP 不校验 / DNS rebinding（Python 也不防，不修）

Python 和 Go 都不检查解析后的 IP（`rule_fetch.py` 文件头和 Go `fetch.go` 包注释都写明这不是公网 IP 闸门，Fake-IP 网络依赖正常解析）。
内部请求在生产中走进程内 `localhttp` 分发，不解析 DNS，所以分发目标不存在 rebinding 问题；外部请求按设计可以到达任意主机。建议同 LOW-1。

### LOW-3 Go 更严：Python 接受、Go 拒绝的输入

逐条判断，均不影响正常规则 URL：

| 用例 | Python | Go | 判断 |
|---|---|---|---|
| 作者登记的 `encoded-api-dot` `/%61pi/../redirect` | 原文路径不以 `/api/` 开头 → 当外部处理，跟到跨 origin 目标 | 按解码后的路径判内部 → 400 | 正确。`%61` 是非保留字符，RFC 3986 §6.2.2.2 规定它等价于 `a`。合法的 `/%61pi/start` 仍可用（改成进程内分发，不走下载代理） |
| 作者登记的 `redirect-encoded-dot` `/api/%2e%2e/landing`，以及本次的 `redir-encoded-mixed` | 发出字面 `/api/../landing`，真实服务器会规范化成 `/landing` | 400 | 正确 |
| 作者登记的 `redirect-double-encoded-dot` `/api/%252e%252e/landing` | 发出原文 | 400 | 纵深防御。生产中内部跳转都是进程内分发，只解码一次，所以经面板本身无法利用。只有名称解码两次后正好构成穿越时才会误伤，实际不存在 |
| 作者登记的 `redirect-backslash` `/api/..\landing`，以及本次的 `redir-encoded-backslash`、`redir-backslash-traversal` | 发出 `%5C` 原文 | 400 | 正确；误伤已由 MEDIUM-2 修掉 |
| `redir-encoded-slash` `/api/..%2flanding` | 发出 `%2F` 原文 | 400 | 正确（会先解码 `%2F` 的后端会逃逸） |
| `redir-backslash-scheme-relative` `\\<ext>/landing` | 当路径发出（`/api/%5C%5C…`） | net/url 拒绝 → 400 | 正确（浏览器把它当 scheme-relative） |
| `redir-http-colon-relative` `Location: http:ok` | `urljoin` 当相对路径 → `/api/ok`，200 | 原生 net/url 当绝对 URL，没有主机 → 400 | 可接受：这是畸形 Location，RFC 3986 §5.2.2 的 strict 解析器也按绝对 URL 处理 |
| `prefix-dot-out-no-redirect` `/cf/api/../../api/ok`（server_domain 带 `/cf`） | 走网络直连，200 | 在任何 I/O 之前 400 | 可接受，作者已在 behaviour-changes 登记（Python 先请求源，再拒重定向） |

正常地址回归对照全部一致：带 query（含编码嵌套 URL）、`%2F` 名称、`%25` 名称、反斜杠名称、`/cf` 前缀、
外部带 token query、外部→外部重定向、外部→内部合法重定向、userinfo 同源、端口前导 0 同源、绝对 Location 经 `/api/../api/ok` 回到 API、
fragment、query 嵌套 `…/landing`。

### INFO

- **校验时机 / TOCTOU**：首跳和每个重定向目标在发出请求之前都经过同一个 `prepareRuleTarget`；校验的是 `weburl.Prepare` 后的 URL，发出的也是它的 `String()`。
  transport 再做一次 Prepare（幂等），用 `RoundTrip`，不会自动跟随重定向；`localhttp.Do` 分发前还会按解码路径复查前缀。边界一旦进入就保持到链结束。没有发现校验对象和实际请求对象不一致的情况。
- **重定向方法**：只允许 GET/HEAD，没有 body；301/302/303/307/308 两端都保持原方法；四种状态码的逃逸都拒绝。
- **其他 scheme**：`file:`、`data:`、`gopher:`、`ftp:` 两端都返回 ValueError `Invalid HTTP/HTTPS URL` → 400。
- **`;` 参数**：`/api/..;/x` 两端都不当 dot segment，仍在 API 内。进程内分发没有 Tomcat 式的 `;` 语义。
- **query 嵌套**：`/api/mosdns/rule-proxy?url=…` 会形成嵌套回调（深度上限 8），内层要自己的 token，能到达的范围和直接传外部 URL 相同，与 Python 一致。
- **错误信息**：路由只返回固定的 `Invalid remote URL`；日志用 `safeDetails`，只记录 `exception_type`；`ErrInternalRedirect` 的文本不含 URL。
  `localError` 的 FetchError 文本含 `repr(URL)`，但只在 localhttp 解析失败时出现，而这种 URL 已经过 `ValidateRuleURL`，到不了这里，与 Python requests 的 InvalidURL 一致。

## CFG-03 复核

- 方法：yaml.v3 的 Line/Column 定位 + 共享的引号扫描，判断标量是否物理跨行。没有复制 PyYAML。行索引现在也把 NEL/LS/PS 当换行，与 yaml.v3 计行一致（editor 的定位也一起变准）；
  非 ASCII 行的列索引只建一次。
- 新增 `gen_cfg03_review.py` → `cfg03_review.json` 共 20 个真实 Python 向量（`TestCFG03ReviewPhysicalLines`）：块风格 payload 的双引号/单引号/plain 跨行、块根 + flow payload 内跨行、
  块根 flow payload 旁边的多行值（两端都展开）、NEL/LS/PS 换行、单独 CR、嵌套序列、CJK、plain/verbatim tag 跨行、`"a\\"` 结尾、`'it''s'`、
  多行排版但每个值都单行、注释里带引号。全部与 Python 一致：拒绝时原文逐字节保留，只有 1 条 line=0 的 warning，文案相同；接受时展开后 YAML 结构相等。
- `multiline-key`（`"multi\n key": v` 作隐式键）两端都在解析阶段失败、保留原文、给 line=0 warning，但错误文本分别是 PyYAML 和 yaml.v3 的解析器报错。这不属于 CFG-03，只比较了位置和原文。

## CFG-09 复核

- `isBadRequest` 先识别 `*os.LinkError`、`context.Canceled`、`context.DeadlineExceeded`（都用 errors.As/Is，所以包装后也能认出）并归为 500，然后才做 URL 判定；
  `*fs.PathError` 保持原来的 IO 分支。错误链没有改写，原生 LinkError/PathError 都能通过 `errors.As` 取到。
- 状态码与 Python 一致：receipt 目录冲突、权限失败 → 500 `Failed to process rule proxy request`；非法 URL → 400；下载失败 → 502。下载超时在 `ruleProxy` 内就先走 502，不会被新加的 DeadlineExceeded 分支截走。
- 日志 `safeDetails` 对 LinkError 输出 `exception_type=os.LinkError`，不含路径、URL 或凭据。小建议（不影响行为）：取消时输出的是 `errors.errorString`，诊断价值有限。
- `TestCFG09*` 在 -race 下通过。

## 修复提交

| 提交 | 内容 |
|---|---|
| `12925a10b` | HIGH-1 + MEDIUM-1：Location 按原始写法分类；非 ASCII 主机名按 IDNA lookup 形式比较；新增 64+20 组 Python 对照 |
| `9aff9ee27` | MEDIUM-2：反斜杠按两种读法都必须在 API 内，合法名称恢复可用 |
| `818f8340c` | CFG-03 补充 20 个 Python 对照向量（无代码改动） |

## 验证（.32）

- Python 基准：`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_cfg01_review.py'`、
  `… gen_cfg03_review.py`。
- 修复前 `TestCFG01Review*` 失败 5 个（`ext-into-api-dot`、`ext-into-api-scheme-relative`、`idn-upper`、`idn-punycode`、`idn-fullwidth`）；修复后全部通过。
  第二轮修复前，反斜杠名称用例按 Python 为 200，Go 为 400。
- `go test -race -count=1 ./internal/rulefetch ./internal/yamlx` → `ok rulefetch 45.520s`、`ok yamlx 25.855s`；`./internal/mosdns` 全包在 -race 下因 .32 负载 load≈25 超过 10 分钟默认超时，
  改用非 race 跑 `go test -count=1 -timeout 40m ./internal/mosdns` → `ok 122.334s`；`go test -race -run '^TestCFG09' ./internal/mosdns` → `ok 4.751s`；`go vet ./...` 通过；gofmt 无输出。
- 公开 configdiff：`rt -e GOMAXPROCS=4 -- bash -lc 'server/tools/configdiff/native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups rule-fetch-real-internal-redirect-escape-after-dot-normalization --baseline review-cfg-c14-security …'`
  → H120 4/4 compared，diffs 0，open 0，design_errors []，155 个比较器自检通过，篡改 body 的负控能检出（71.27s）。
- gate：`RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=6 --back .ci-results/review-gate -- bash -lc '<900 s 前台 watchdog> bash server/scripts/gate.sh -p 6 FuzzRuleYAMLBoundary:./internal/rulefetch FuzzEditScalar:./internal/yamlx'`。
  .64 08:32:25 开始排队（load≈26，已有 1 个 gate 在跑），900 s 内没有拿到 slot，按 AGENT-BRIEF 停下：
  `QUEUE LIMIT: stopped after 900 seconds without a gate slot; no gate stages ran.` 没有执行任何 gate 阶段，**不算完整 gate 通过**，
  请协调者在合并树上补跑 `cf-gate`。日志：`.ci-results/review-gate/gate.out`。确认没有残留进程。
