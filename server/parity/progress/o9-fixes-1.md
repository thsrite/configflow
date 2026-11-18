# O9 第一轮安全修复（httpx / backend listener / auth）

## 范围与状态

任务基线 `go@d4fab6441`，分支 `go-fix-o9-medium`。修复 O9-M1、L1、L3（仅 httpx）、L5、L6，以及维护者追加批准的 H1、M3。O9-M2 和 mosdns token 比较不在本任务。测试只在 `.32` 经 `rt`；未运行 full gate。

全部指定修复与回归完成。定向 race、格式/vet、生产快照比较、全体 difftest（5 包修正旧期望后定向补跑）及 synthetic live 均已验证；compare 首次的失败与补跑记录如下。

## 实现与边界

- M1：`httpx.LimitRequestBody` 与 Edge 共用同一个 body limiter；backend listener 套该 wrapper，默认地址不改。通常 1 MiB，含 profile 的 OxiDNS settings/render 96 MiB。Content-Length 预检与 chunked/stream 超限均 413；即使 handler 忽略读取错误，也不会把超限响应降为 400/200。
- L1：Edge access log 的 RequestURI 和 Referer 经 `weburl.SafeForLog`；保留非敏感查询字段。
- L3：httpx 内部调用头用 `subtle.ConstantTimeCompare`。没有改 mosdns。
- L5：Edge 响应及 App 的 `text/*` 响应增加 `X-Content-Type-Options: nosniff`，覆盖静态错误、规则/规则库文本、配置订阅和 HEAD。
- L6：认证门禁从 profile hook 之后移到之前。匿名受保护请求统一 401、不带 profile 回显；已登录/公开/关闭认证时 profile 校验不变。冻结 Python corpus 不改，通过明确的新 401 期望核对；live HTTP 差分独立断言 Python 原 404 与 Go 新 401 的完整 JSON。
- H1：只向自身 scheme/host/port 的 Origin 添加 CORS；无 Origin、foreign、null、格式错误时没有 CORS 头。Edge 用进程内 context 保存改写前来源，不使用客户端 X-Forwarded-*。CORS 不拒绝请求、不改变状态码/正文；反代改写 Host/终止 TLS 的 POST/PUT 仍按原 handler 返回。
- M3：未设置、compose/安装文档占位值、少于 32 字节的 JWT_SECRET_KEY 均换为随机密钥并 WARNING（原因、正式密钥设置办法，不含密钥）。恰好 32 字节、Unicode 字节长度、正常密钥均覆盖；旧 token 对临时密钥重启失效，对固定正式密钥重启仍有效。
- `behaviour-changes.md` 已登记 O12；`doc/guide/install.md` 与 compose 注释说明随机正式密钥和重启登录影响。没有 UI 改动，无需新截图。

## 新增文件

| 文件 | 行数 |
|---|---:|
| `internal/httpx/bodylimit.go` | 54 |
| `internal/httpx/o9_fixes_test.go` | 236 |
| `internal/httpx/httpxtest/security.go` | 59 |
| `internal/httpx/httpxtest/security_test.go` | 45 |
| `internal/auth/o9_secret_test.go` | 66 |
| `cmd/configflow/o9_body_limit_test.go` | 49 |
| `tools/security_headers.py` | 22 |

本报告也是新增文件。Python 对照范围为 `backend/routes/auth.py`、`backend/common/auth.py`、`backend/common/profile_context.py`、`backend/app.py`；不重写 Python，认证/配置空间正常路径沿用原差分，已批准差异逐条列于 O12。`o9repro` 的后端体积与 CORS 两条用例都转为正常测试，未跳过本任务范围的回归。

## 差分规则

`httpxtest.O9ExpectedHeaders` 和 Python 共用 `tools/security_headers.py` 仅处理三个 Flask-CORS 头（ACAO/Allow-Headers/Allow-Methods）、Vary 的 Origin token（H1），以及 text 响应的精确 nosniff 新增（L5）。其余 Vary token、头、状态、正文、存储仍严格比较；错误 nosniff、额外 CORS、其他头/正文/状态的反例均验证。L6 的独立 helper 只对两端完整 JSON 同为精确登录/会话无效消息的 401，允许移除 profile 回显；status/消息/额外字段不符的反例均拒绝。规则适用于现有无 Origin/外域 Origin 比较夹具，同域正例由直接 HTTP/Edge 回归覆盖。

proddiff 与 configdiff 的共享合成 JWT fixture 改为至少 32 字节，继续两端验证同一固定正式密钥。未改 snapshot、baseline、golden 或 Python 产品代码。

## 修复前失败证据

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -count=1 -run "^TestO9" ./internal/httpx ./internal/auth ./cmd/configflow'
```

原始产品代码上退出 1：M1 的 8 MiB 已知长度/chunked 登录体得到 400（此真实服务 fixture 未开启登录），非 413；L1 日志泄露合成 token；L3 源码守卫找不到 ConstantTimeCompare；L5 静态/文本无 nosniff；L6 匿名响应泄露 profile；H1 外域/跨端口/跨协议/null 等都获得 ACAO；M3 占位值/短密钥原样复用且没有 warning。

M1 和 H1 从 `go-opus-o9-security` 的 o9repro 复现转为无 tag 正式回归，M1 改为真实 service listener。整合原 security 分支时请移除其两个已替代的 tagged 复现：H1 同名函数会重复定义；M1 原 App fixture 不代表实际 listener（本分支没有原 tagged 文件）。

## 验证命令和结果

已通过（均 `.32`）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'PYTHONPATH=server/tools/proddiff ~/cf-test/.venv/bin/python -m unittest test_proddiff.ComparatorTests.test_o9_security_header_delta_is_exact && PYTHONPATH=server/tools/configdiff ~/cf-test/.venv/bin/python -m unittest test_compare.Comparators.test_o9_security_header_delta_is_exact && cd server && go test -race -count=1 ./internal/httpx ./internal/httpx/httpxtest ./internal/auth ./internal/server ./internal/api/auth ./internal/api/rules ./internal/api/rulelibrary && go test -race -count=1 -run "^TestO9ReproBackendPortHasNoBodyLimit$" ./cmd/configflow'
```

两组 Python 规则测试均 `Ran 1 test ... OK`；race：`httpx 51.953s`、`httpxtest 1.020s`、`auth 3.043s`、`server 19.110s`、`api/auth 1.346s`、`api/rules 76.301s`、`api/rulelibrary 57.673s`、真实监听 `cmd/configflow 3.322s` 全部 `ok`。

最终 streaming 限长补充后执行了一次指定的 compare：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -count=1 -run "^TestO9" ./internal/httpx ./internal/httpx/httpxtest ./cmd/configflow && GATE_ONLY=compare GATE_LOGS="$HOME/.cache/o9-fixes-1-compare" scripts/gate.sh -p 6'
```

proddiff：`baseline: new=0 stale=0`，`self_check: PASS`。difftest 中 5 包因原有 L6 profile-header / 404 期望失败，其他包均通过（含 agents 258.243s、httpx 35.980s）；compare 因此在 synthetic live 之前退出。未重复 proddiff/已通过包，修正这 5 包的精确期望后定向重跑，并单独执行同一 compare 的 synthetic-live 收尾：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -count=1 ./internal/httpx/httpxtest && CONFIGFLOW_DIFF=1 go test -count=1 -p 6 ./internal/api/aggregations/difftest ./internal/api/oxidns/difftest ./internal/api/rulelibrary/difftest ./internal/api/rules/difftest ./internal/api/subscriptions/difftest && ~/cf-test/.venv/bin/python scripts/gate-live.py "$HOME/.cache/o9-fixes-1-compare" -p=6 && test -z "$(gofmt -l internal/ tools/ cmd/)" && go vet ./...'
```

已通过：helper race 1.014s；aggregations 60.386s、oxidns 14.318s、rulelibrary 41.921s、rules 98.769s、subscriptions 140.912s。`SYNTHETIC LIVE PASS: 23 required entries ran and passed; 0 skips; repo Python subtest and WebDAV Python exec verified`。最后 gofmt 无输出、`go vet ./...` 通过。

前半输出：`ok configflow/internal/httpx 4.686s`、`ok configflow/internal/httpx/httpxtest 1.015s`、`ok configflow/cmd/configflow 3.398s`。

`CONFIGFLOW_DIFF=1 go test -count=1 ./internal/httpx/difftest` 已 `ok ... 23.967s`。`test -z "$(gofmt -l internal/ tools/ cmd/)" && go vet ./...` 已无输出通过；auth 跨重启回归经再次定向 race：`ok configflow/internal/auth 1.015s`。

没有重复完整 compare 或完整 gate；一次 compare 的 proddiff 与所有原先通过的 difftest 结果保留，只补跑因 L6 旧期望失败的 5 包和尚未运行的 synthetic live。

## 交回

分支 `go-fix-o9-medium`；提交为本报告所在的 `go(httpx): harden O9 request and authentication boundaries`（最终回信提供完整提交 ID）。无新依赖、无未完成事项。协调者负责集成与推送。
