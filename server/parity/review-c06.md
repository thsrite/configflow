# C06 独立复核

状态：**verified**，独立复核完成，**high 0 / medium 1 / low 0，已修复，0 个模块内未决发现**。复核起点
`go-port-c06-c11@cefe6f7e3`，分支 `go-review-c06-c11`，初始基于 `go@f90a156eb`。
复核者不是移植作者；未 push、未启动子 agent、未访问生产。新增输入全部为合成数据。

## 发现与修复

**F1 / medium：完整设置值泄露到日志，并可带入伪造日志行。**
`handleServerDomain` 将 newDomain 拼到 INFO 日志，`handleSubscriptionAggregation` 将
宽松 enabled 值经 jsonx.Str 拼到日志。两个字段都允许字符串，包含 URL userinfo 密码、
query token 和内部换行的合法设置可以原样写入日志；global JSON 脱敏不保护日志。
Python 原版同样存在。`TestReviewSettingsLogsDoNotEchoValues` 在 `.64` 修复前真实 HTTP
保存均返回 200，两个子例都检出 synthetic-log-secret、synthetic-query-secret、域名和
forged-log-entry 泄露，测试退出 1（0.588s）。

现在两个位置只记更新事件，不记录设置内容；继续保存、返回原值，没有改校验和 API。
回归同时断言事件仍存在、完整存储值不变和日志无凭据/伪造行。遍历其他 C06 写 handler，
proxy/token/backup/mode 已分别只记录固定事件、字段名、开关或已验证枚举，不存在另一个
拼接任意设置值的 INFO 位点。此安全差异已登记 behaviour-changes；修复提交 `8168bc648`。

## 审计结论

- 完整阅读 Python 的 app.py、routes/__init__.py、common/auth.py、routes/auth.py、
  routes/settings.py、conftest.py 及交接列出的五个测试文件；对照 HTTP shell、认证、
  settings DTO/handler/owner 和共享脱敏调用链。已读 AGENT-BRIEF、PORTING、WORKFLOW
  四常设项、C06/C11 HANDOFF、设计 Clarified、四份指定复核报告及 O3-6a 记录。
- 六组 GET/写路由均在绑定 body 前将 HEAD 分派到 GET。`TestSettingsHEADIsReadOnly`
  对每组有效写入 body、畸形 body、非对象 body 共 18 次检查 GET 同状态/全 headers、
  HEAD 空 body、全表 Fingerprint（包括 generation、字段 revision）及全部文件字节不变。
  web-port 只改 handler 分派，listener 的 Status/Update、持久化、锁和健康探测仍归 C07。
- 严格 reader 是 server-domain、config-token、subscription-aggregation 的 Bind；
  web-port、rule-fetch-proxy、backup/config、backup/test、backup/now、Sub-Store mode
  使用 BindSilent。update 不读 body。非对象/null、畸形 JSON、非 JSON MIME、大小写
  精确成员、NaN/Infinity/-Infinity 均有真实 Flask 对照；400/415 差异严格限定于已批准策略。
  RawMessage 保留成员存在性和数值拼写，宽松 aggregation 值仍原样保存。
- 新增 `TestReviewC06SettingsMemberTypes`，34 个独立 Flask 请求：null/bool/数字/数组/对象
  成员、整数与 1.0/1e0、保留份数边界、重复键最后成员替换、大小写不同键、
  aggregation 对象覆盖/null/字符串。响应和完整 meta/sections/fields 及文件状态全部相等。
  复用原有状态比较器，没有增加 normalization。提交：`4cbbb19f9`。
- `saveSettings`、`testBackupConnection`、`runManualBackup` 在 owner Detach 后检查取消；
  保存前直接检查，手动备份在 Capture 后、Run 前再次检查。handler 没有 WithoutCancel。
  原始 Scope/baseline 继续交给 repo，不另造提交/锁/回执 helper。
  `TestSettingsRuntimeWriteLifetime` 确实启动真实 Runtime，并由 BindRequest 绑定关机 parent；
  八类操作 × shutdown/disconnect 共 16 例。关机组检查 DB/files 和零 WebDAV 请求；
  断开组检查具体设置、revision +1、远端目录、完整上传和成功 taskstatus。
  这是「调用 owner 前已关机/已断开」的验证，不冒称覆盖共享层执行中最终提交窗口。
- `TestPythonTextRedactionAndStoredSecretFinder` 直接调用真实 Python helper，比较编码变体、
  八层编码、重叠 token、Unicode 码点范围、CRLF/其他换行和冻结快照；safe error
  type/status 另有真实 Python 对照。A13 的 registration-key finder 扩展已有独立测试，
  Python 相等性测试清空该环境变量，没有把这个安全扩展误记成原 Python 行为。
- 认证矩阵从 `server.NewApp().Router.Routes()` 动态枚举，每条注册展开方法及 HEAD/OPTIONS，
  对照真实 Flask 注册后的 global gate，再检查匿名/坏 JWT/有效 JWT。leaf stub 只证明
  global gate，不能证明自认证 leaf。C06 自身真实 leaf 另由 settings/auth tests 和 HTTP
  差分覆盖。SQLite 只读失败回归比较 DB/revision 并断言响应、日志不泄露代理凭据。

## 已知、已分派的共享窗口与外部入口

这些是既定 owner 边界，不计为本轮新发现，也不声称已关闭：

- **O3-6c / 6e5c7760**：repo updater 返回到最终 SQLite COMMIT 的窗口；taskstatus
  Context 入口/调用方及最终 rename；rulefetch cache touch/replace；共享 archive 链。
  特别是 `webdav.Run` 在 Upload 取消返回后仍调用无 ctx 的 `taskstatus.Record`，执行中
  关机仍可能写失败回执。C06 的入口检查及上述 16 例不能代替这项共享修复。
- **finish/MCP**：真实 `/mcp` bearer/query-token、坏 bearer、超长 query token、
  当前/退役内部 token 拒绝。共享 parser/JWT/global gate 不能替代 leaf 验证。
- **C34**：真实 config/profile-config 的 JWT/query-token 优先级、编码/超长 token、
  内部 token 优先拒绝，以及完整备份导出/恢复代理、脱敏导出；共享 repo/redact 契约已有，
  实际 HTTP 入口仍归 C34。
- **C33**：proxy-group CRUD 与 listener 原子联动；C11 另列精确缺项。
  N8-5 后续消费者迁移须保留未知存储字段、null/presence 和绑定策略；本轮未抢改 typed repo。

四常设项：测试没有改写其要验证的生产参数；C06 没有新增 HTTP/device client，备份继续走
C35 共享 WebDAV；未修改共享驱动/脱敏实现；真实 Runtime 的关机/断开验证如上。
无新增功能或操作入口，无需 doc 截图变更。本轮唯一新增差异是 F1 的日志安全修正；既有 HEAD、N8-6 与
C35 安全修正继续以设计/behaviour-changes 为准。

## 验证

以下全部在测试服务器前台通过 rt 执行。

1. `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/settings ./internal/auth ./internal/api/auth ./internal/redact ./internal/httpx/difftest ./internal/subs ./internal/subs/difftest ./internal/convert ./internal/refresh'`
   自动选 `.64`，退出 0：settings 12.154s、auth 1.197s、api/auth 0.155s、redact 0.160s、
   httpx/difftest 21.054s、subs 3.764s、subs/difftest 9.671s、convert 8.799s、refresh 39.684s。
2. `.64` 新增 C06 用例及扩展 C11 用例定向运行退出 0：httpx/difftest 10.797s、subs/difftest 0.127s。
3. `RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/settings ./internal/httpx/difftest ./internal/subs/difftest && go vet ./... && test -z "$(gofmt -l internal tools cmd)"'`
   退出 0：settings 42.567s、httpx/difftest 96.790s、subs/difftest 24.710s；vet 和 gofmt 无输出。

4. `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -count=1 -run "^TestRecordedPythonTests$" ./internal/provider && cd .. && ~/cf-test/.venv/bin/python -m pytest -q -p no:cacheprovider backend/test_bearer_auth.py backend/test_login_messages.py backend/test_request_exception_redaction.py backend/test_rule_fetch_proxy_settings.py backend/test_security_regressions.py backend/test_proxy_chains.py backend/test_mihomo_listener_lifecycle.py backend/test_proxy_server_dns.py'`
   退出 0：provider 199.603s；`296 passed in 58.51s`。

## 完整 gate 与最终基线

`.32:~/.cache/rt-tmp/review-c06-c11-fixed` 完整 gate **退出 0，GATE PASS**：

- gofmt 无输出；全仓 vet、双 tag build/vet/lifecycle 和 observer 自检通过。
- 全量 `-race -p=6` 全绿，95 个包输出 ok；settings 64.538s、auth 6.877s、redact 1.255s、subs 103.446s、substore 96.425s、convert 238.156s、refresh 162.831s。没有 data race。
- 离线 proddiff：`new=0 stale=0`；15 个启用 HTTP difftest 包全部通过，httpx/difftest 66.329s、subs/difftest 26.152s。
- 两个 fuzz 各 60s 通过，最终输出：

```text
baseline: new=0 stale=0
FuzzBindNativeJSON ok configflow/internal/httpx 61.069s
FuzzSubscriptionYAMLReaders ok configflow/internal/subs 61.046s
GATE PASS
```

此门禁在旧 `4754d18da` 快照执行，等价于 `go@1439e503d` 上的本分支产品/Go测试/gate源码。
此前两次因变基、补 F1 修复而主动中止的运行均不计通过。运行期间叠入的 configdiff 工具/文档
不在这次门禁范围内；以下比较当时退出 0，确认 Go 产品/测试与 gate 输入未变：
`git diff --exit-code 4754d18da HEAD -- backend server/internal server/cmd server/go.mod server/go.sum server/scripts server/tools/proddiff`。

随后 go 合入 C33 和 N7-3d 独立复核，最终变基到 **go@a9d6ce86c**。
冲突仅为行为变更文档的追加段落，保留双方；没有 C06/C11 产品代码冲突。
这次确有其他 owner 的产品变化，所以不把旧完整 gate 冒称为最终整棵树的 gate。
按新 WORKFLOW 的批量合入机制，协调者在最终 batch 树上统一验收。

最终基线定向命令，rt 自动选择 `.32`，退出 0：

```sh
rt -e GOMAXPROCS=4 -- bash -lc 'cd server && test -z "$(gofmt -l internal tools cmd)" && go vet ./... && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/settings ./internal/httpx/difftest ./internal/subs/difftest && go test -race -count=1 -run "^TestHostsChangeInvalidatesAgentContentChecksOnce$" ./internal/substore && go test -count=1 ./internal/subs ./internal/convert'
```

settings race 42.690s、httpx/difftest race 93.640s、subs/difftest race 24.668s、hosts invalidator
race 1.714s、subs 14.130s、convert 34.147s；最终格式及全仓 vet 通过。动态认证矩阵包含新
合入的全部 C33 注册。C33 真实入口已有其独立 `review-c33.md`，不由 leaf stub 冒认；
MCP/C34 和共享 O3-6c 的责任仍按上文保留。

正常的 owner 前读取也已追至 GetSystemWithSource → committedSource → readEntry/loadEntry：
健康读取没有先检查请求 ctx.Err 的闸门，冷读不会仅因客户端断开而直接拒绝；没有发现新增问题。
16 个 Runtime 回归仍明确只声称其入口边界，不代替共享 I/O 失败重试或最终提交窗口验收。

最终修复提交：**8168bc648**（F1 日志），边界增强 **4cbbb19f9**；分支 `go-review-c06-c11`。
0 个 C06 未决发现；未 push、未访问生产、未启动子 agent。

### 完整 gate 的精确前台 rt 启动命令

守卫只对未取得 slot 的排队设置 900 秒上限，取得 slot 后持续前台跟随至退出。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'python3 - <<'"'"'PY'"'"'
import os, selectors, signal, subprocess, sys, time
logdir = os.path.expanduser("~/.cache/rt-tmp/review-c06-c11-fixed")
os.makedirs(logdir, exist_ok=True)
os.environ["GATE_LOGS"] = logdir
print("C06/C11 fixed gate; queue cutoff 900s; logs " + logdir, flush=True)
p = subprocess.Popen(["server/scripts/gate.sh", "-p", "6", "FuzzBindNativeJSON:./internal/httpx", "FuzzSubscriptionYAMLReaders:./internal/subs"], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, start_new_session=True)
s = selectors.DefaultSelector(); s.register(p.stdout, selectors.EVENT_READ)
start, acquired, pending = time.monotonic(), False, ""
with open(os.path.join(logdir,"console.log"),"wb") as log:
    while True:
        if not acquired and time.monotonic()-start >= 900:
            print("QUEUE LIMIT: 900 seconds without slot; terminating queued gate",flush=True)
            os.killpg(p.pid,signal.SIGTERM); p.wait(timeout=30); sys.exit(75)
        for key,_ in s.select(timeout=5):
            chunk=os.read(key.fileobj.fileno(),65536)
            if not chunk: s.unregister(key.fileobj); continue
            log.write(chunk); log.flush()
            sys.stdout.buffer.write(chunk); sys.stdout.buffer.flush()
            pending+=chunk.decode("utf-8",errors="replace")
            while "\n" in pending:
                line,pending=pending.split("\n",1)
                if line.startswith("gate: slot "): acquired=True
        if p.poll() is not None and not s.get_map(): sys.exit(p.returncode)
PY'
```
