# O9 修复复核（fresh Opus）

复核对象：`go-fix-o9-h2@cc69da0c1`（H2）与 `go-fix-o9-medium@70e08ff99`（H1/M3/M1/L1/L3/L5/L6），均基于 `go@d4fab6441`。
复核分支 `go-opus-review-o9fix`（基于 H2 tip）。只读代码对照 Python 与报告，定向测试只在测试服务器经 `rt` 运行，未跑完整门禁。

## 结论

两条分支的主修复成立，可以叠批。H2 没有发现可绕过首次注册授权的路径；发现 1 个注册路由的存在性探测（已修，属 H2），
1 个容量可被正常使用耗尽的 medium（未修，需决策），其余为 low/文档。medium 分支未发现阻断问题，有 2 个 low 建议。
`TestInstallerFuzzAgainstFlask` 在 race 下超时由作者跟进，本复核未处理、未复验。

## 发现

| # | 级别 | 分支 | 状态 | 内容 |
|---|---|---|---|---|
| R1 | low | H2 | 已修 | 公开的 `/api/agents/register` 可探测 name/host 是否已注册：开启登录（或设了注册密钥）时，匿名请求遇到已有身份返回 401 归属提示，未知身份返回 403 `RegistrationLocked`；随便带一个错误 Bearer 或垃圾 `registration_token` 也一样。现在没有任何首次注册证明（JWT、正确密钥、与该身份匹配的有效凭据）时统一返回 403 `RegistrationLocked`；有证明时保留 401 可操作提示，且探测不消费凭据。未开登录且未设密钥时不变。设了密钥、未开登录时恢复为与原 Python 一致的“先拒密钥”顺序。 |
| R2 | medium | H2 | 未修，交协调者 | 凭据上限可被正常操作耗尽：前端 Docker 生成同时请求 `docker-agent-compose` 和 `docker-agent-run`，两次请求各自发号；Docker AIO 每点一次“生成”占 4 个名额，约 25 次就达到 100，此后 Shell 和 Docker 生成全部 503，最长 24 小时，且没有管理员清理入口。只有已登录管理员能触发，不构成匿名 DoS。建议二选一：Docker 改成一次请求返回 compose 和 run（同一组凭据）；或增加“清除未使用注册凭据”入口。不建议按相同身份替换旧凭据，因为 compose/run 并发会让先返回的那份失效。 |
| R3 | low | H2 | 未修，文档 | 未开登录、已设 `AGENT_REGISTRATION_KEY` 的部署并非“完全不变”：已有 Agent 凭自身 token 重注册不再需要密钥（原来需要，安全上等价，因为仍需 token）；错误密钥的文案从 `Invalid registration key` 改为 `RegistrationLocked`；新 Agent 现在可以在配置里带 `registration_key` 注册（原来普通安装做不到）。`doc/guide/agent-install.md` “未开启登录时维持原有注册方式”应补一句这三点。 |
| R4 | low | H2 | 记录 | “只存 hash”只对主库成立：`install-command` 生成的脚本快照（installer store，24h）保存含明文凭据的完整脚本，`/api/agents/i/<id>` 下载链接就是该凭据的持有者链接；Docker 方式的凭据在宿主机 `docker run` 参数和 `docker inspect` 环境变量里可见。二者寿命与凭据一致，且首次注册后作废，可接受；behaviour-changes 已写“下载脚本含明文凭据”，建议报告措辞同步。 |
| R5 | low | medium | 交协调者 | M3 警告在未开登录时也会打印：`service.go` 无条件调用 `auth.FromEnv()`，未开登录的部署每次启动都会看到“JWT_SECRET_KEY 未设置……重启后需重新登录”，有误导。建议只在设置了 `ADMIN_USERNAME` 和 `ADMIN_PASSWORD` 时警告（仍生成随机密钥）。 |
| R6 | low | medium | 交协调者 | JWT 密钥也给客户端固定 IP 令牌签名（`clients/fixed.go` 的 `FixedIPToken{Secret: auth.Current().SecretKey}`）。原来用短密钥或示例占位值的用户升级后，每次重启除了要重新登录，这类签名也会失效。`doc/guide/install.md` 只写了“重启后需重新登录”，建议补一句。 |

## 逐项核对（无问题）

- H2 并发/TOCTOU：发号用 `repo.Update`，消费 + 建 Agent 用 `repo.UpdateSystem`（内部就是 `Update`），二者走同一个 `.config.lock` 写事务，不会丢写，也不会让已消费的会话复活。`SaveSystem` 只写 `system_config/backup/agents`，设置页保存不会覆盖会话；导出、导入都会丢弃会话。
- 计时：比较的是固定长度的 SHA-256 hex，用的是 `subtle`；注册密钥先做 sha256 再恒时比较。
- 重试/崩溃：消费与建 Agent 同事务；失败的事务不消费；Agent 客户端保存失败会恢复 bootstrap 字段。如果 Agent 收到 token 后、落盘前崩溃，再用同一凭据会得到归属拒绝，需要删记录重装，这和 Python 原有的“同名同地址无 token”行为一致。
- 泄露：脚本配置写 `0600`；Docker entrypoint 用 `0600` 临时文件；`cf_redact` 覆盖两个新字段；注册日志按白名单逐字段脱敏；生成响应 `no-store`。凭据不出现在 Shell 脚本的命令行参数里。
- 匿名入口：`install-script` 只在 JWT 有效时发号；`docker-agent-*` 由 `auth.Require` 保护；旧的公开 `docker-compose/docker-run/docker-mihomo-*` 只产出模板，不发号。
- H1：不拒绝请求，也不改状态码和正文；来源取自 Edge 进程内 context 或请求本身的 Host/TLS，不信任 `X-Forwarded-*`。TLS 在反代处终止时，同源请求只是拿不到 ACAO（同源本来不需要）。同源预检正常回 Allow-*，外域预检 200 但不带 CORS 头。代码里没有自行设置 ACAO 的路由，所以“删除 Access-Control-*”不会误伤。
- M3：警告里不带密钥（测试已断言）；占位值列表与 `docker/docker-compose.yml`、`doc/guide/install.md` 一致；Python 的 `your-secret-key` 不足 32 字节，由短密钥规则覆盖。
- L6：门禁放到 profile 解析之前，未登录的受保护入口统一 401；公开注册入口的存在性问题见 R1。
- 叠合：在临时 worktree 中，把 `70e08ff99` 合并到 `cc69da0c1`，冲突只有 `behaviour-changes.md`、`tools/proddiff/run.py`、`tools/proddiff/test_proddiff.py` 三个文本冲突，全部两边保留即可。`run.py` 里先调用 H2 的 `bootstrap_installer_baseline`，再调用 H1 的 `expected_headers`。叠合树的 proddiff o9 规则、`go vet ./...` 和四个包的 O9 race 全部通过（见下）。R1 修复不在这次叠合树内，叠批时一并带上。

## 提交与测试

本分支提交：R1 的修复、回归测试、差分精确例外，以及 behaviour-changes 和本报告（见 git log）。

差分例外：`api/agents/difftest/diff_test.go` 中 4 个具名的“register duplicate …”样例（wrong token、malformed、non-ascii、oversized bearer）由 `noOracleDelta` 精确断言 Python 401、Go 403 完整 JSON `RegistrationLocked`；其他样例照旧严格比较。

rt 命令（均为 `.64`）：

1. 修复前（还原 manager.go）：`go test -race ./internal/api/agents -run '^TestO9AnonymousRefusalDoesNotRevealExistingIdentity$'` → `status 401, want 403`，`FAIL configflow/internal/api/agents`（证明测试在修复前失败）。
2. `gofmt -l` 空 + `go vet ./...` + `go test -race ./internal/agents ./internal/api/agents -run "TestO9|TestUnreadHistoryIsNeverSaved|TestRegistrationLogsOnlyBounded|TestRegistrationKey|TestReregister|Register"` → `ok configflow/internal/agents 5.486s`；`ok configflow/internal/api/agents 70.000s`。
3. `CONFIGFLOW_DIFF=1 go test ./internal/api/agents/difftest -run '^(TestDifferentialAgainstFlask|TestDockerHADifferentialAgainstFlask)$' -timeout=30m`（非 race）→ `ok configflow/internal/api/agents/difftest 12.048s`。
4. 叠合树：`python -m unittest server/tools/proddiff/test_proddiff.py -k o9` → `Ran 3 tests ... OK`。
5. 叠合树：`go vet ./... && go test -race ./internal/api/agents ./internal/httpx ./internal/auth ./cmd/configflow -run TestO9` → `ok` 48.408s / 3.951s / 1.018s / 3.189s。

未运行：完整门禁、`TestInstallerFuzzAgainstFlask`（作者正在排查 race 超时）。
