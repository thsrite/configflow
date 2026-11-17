# C35 对抗性复核

状态：`verified`（2026-11-17），本轮 C35 无未关闭问题。分支 `go-review-c35` 从本地 `go` 新建；没有子 agent、推送或生产设备访问。C34 的导入 HTTP 路由归属不变。

## 发现与修复

- 高：`GET /api/backup/config` 原样返回 URL userinfo；目录错误的脱敏只覆盖独立用户名/密码，未覆盖 URL userinfo 和百分号编码凭据，导致手动/自动任务错误和 JSON 回执泄露。已剥除编辑响应中的 userinfo，错误路径统一覆盖两组凭据及编码变体；异常远端正文仍不读入错误。修复前 HTTP 和诊断回归测试均失败，修复后通过。
- 中：保存备份表单的空密码覆盖原密码。按协调者明确要求，空值与 `******` 都保留原密码，真实新密码仍可替换。这与 Python 的空值清空行为不同，已记入行为变更账本。
- 中：手动备份在应用有效的请求参数前，先解析了不使用的已存连接字段；已存 username/path 类型错误时 Python 成功、Go 500 且不上传/不记回执。拆开文档捕获和自动任务设置读取，手动备份只验证实际请求设置；新增完整 Flask/存储/DAV 差分修复前失败、修复后通过。
- 中：Surge WebSocket 的显式空 Host 被 obfs-host 覆盖，与 Python 不同，可能改变实际连接目标。按 map 键是否存在决定回退；新增 Python 原始结构语料修复前失败、修复后通过。
- 中：重复斜杠未归一导致上传位置和清理目录偏离 Python。请求和 PROPFIND 根目录统一使用标准库 path.Clean；新增六组真实 Flask/DAV 路径差分。点路径段按协调者裁定在保存、测试、手动/自动执行前拦截，不模拟 Python 的意外目录展开；48 个 HTTP 拒绝用例及旧设置自动任务用例证明零网络请求，错误提示清楚、配置不回滚。安全修正及用户迁移说明已入账。
- 中：默认 HTTP 客户端没有 cookie jar，不能连接依赖会话 cookie 的 DAV（Python 每次操作使用 requests.Session）。使用标准库 cookiejar，并为每次操作克隆 http.Client；同一次操作保留 cookie，不同操作不串会话，保留原 transport、TLS 校验和重定向策略。
- 低：照搬 Python 的警告去重列表和 section 查找导致二次复杂度。改用 Go map 索引，警告只保存显示的前 8 项与精确去重计数，标准库 slices.Contains 替代私有 contains 副本；Python 警告顺序/去重/总数语料不变。
- 已核对禁用行为：repo.update 在调用提交后 hook 前过滤 auto_backup=false；禁用提交不会启动 worker 或重置 pending deadline。原实现没有这项缺陷，新增测试通过。

## 源码逐分支核对与边界

| Python 分支 | Go 边界与复核证据 |
| --- | --- |
| `config_importer.py` 客户端选择、section/CSV/options、各协议/TLS/WS、provider/group/rule、复用与冲突、警告、自定义设置 | `importer.Parse/Build`；97 条真实 Python 结构语料、真实 repository 提交/失败回滚、Snell 导入→导出往返；补显式空 Host、警告去重/计数、非法输入及资源探测 |
| `webdav_backup.py` root/check、递归目录、PUT、文件名、PROPFIND/保留/删除 | 真实 Flask↔Go HTTP 差分并比较完整导出、sidecars、远端目录/文件；只用 loopback fake DAV；成功上传后的清理失败仍视为成功，PUT 失败不清理 |
| TLS、认证、异常处理、日志与凭据 | Go 和真实 Python 均拒绝自签名证书；独立 Basic 字段参与认证，URL userinfo 不参与；对原文/百分号/多层编码/加号变体扫描日志、错误、任务及通知 JSON；请求错误使用安全描述，响应不转发远端正文/头 |
| `settings.py` GET/POST config、test、now、JWT、掩码与空值、非法类型/保留值 | 所有四条路由经真实 HTTP 边界；仅编辑接口按已裁定合同显示用户名/地址；安全差异逐字段单独断言 Python 原行为，未泛化归一化 |
| post-commit hook、auto_backup、1 秒合并、上传中再次提交、异常/禁用/取消/退出 | worker 时序和真实 repo/Agent 写入测试；hook 在锁外，备份失败不回滚提交；WaitIdle 取消不取消上传，Close 丢弃 pending、当前 I/O 自带超时；失效后新提交可重启，空闲 goroutine 退出，弱引用证明 Source/快照释放 |

风格复核：生产 importer 不依赖 jsonx；WebDAV 的 jsonx 限于存储 `adapter.go`。传输使用 net/http/encoding/xml/cookiejar、错误链和标准库路径处理；没有移植 requests/Python 异常类。原有 worker 是 O3 迁移范围，本次没有新增后台 goroutine；已核对新并发设计的过渡规则，原有 recover/logx 和有界 I/O 保留。完整生命周期统一迁移归 O3-5，非 C35 新增延期问题。

C34 持有 `/api/profiles/import-client` HTTP 导入入口；C35 本轮验证纯 Parse/Build 和真实 repository 写入边界，不把该 HTTP 集成标为完成。生产差分使用隔离的只读生产快照；未操作真实 WebDAV 或设备。

## 已有证据

`.32`（loopback fake DAV）：

```sh
rt --back server/internal/api/settings/review_c35_test.go --back server/internal/webdav/review_c35_test.go --back server/internal/webdav/client.go --back server/internal/api/settings/backup.go -- bash -lc 'cd server && gofmt -w internal/api/settings/review_c35_test.go internal/webdav/review_c35_test.go internal/webdav/client.go internal/api/settings/backup.go && go vet ./... && go test -race -count=1 ./internal/api/settings/... ./internal/webdav/...'
```

退出 0；vet 无输出：settings 4.997s、webdav 1.821s、webdav/difftest 1.050s（该次未启用 Flask 差分）。

补充证据：

- .32 `CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/webdav/... ./internal/api/settings/...` 退出 0：webdav 1.808s、真实 Flask 差分 21.402s、settings 3.477s。包含 Go/Python 均拒绝自签名 TLS、默认 transport 拒绝跨 origin 重定向、弱引用证明 idle worker 不保留已完成 Source/8 MiB snapshot、完整凭据路径错误/任务/通知扫描。
- .63 importer 97 条 Python parse/build 结构语料通过；普通测试 0.876s，race 11.998s。独立子进程探测 2 万层嵌套、递归 alias、merge 扩展、2 万键 mapping、4 万警告及 32 MiB 文本；普通运行 1.691s，峰值 RSS 约 108 MiB。race 的 shadow memory/访问检查开销单独放宽测试限额，不作为生产内存测量；没有把 instrumentation 超限当作实现缺陷。
- 明确的 Python 安全修正只对两个已知字段调整预期，并先断言 Python 原行为；其余响应、完整 SQLite 导出、JSON sidecars 和远端 DAV 文件仍完整比较。未增加通用归一化掩盖差异。

- .63 `TestImportedNamesCannotEscapeRepository` 通过：恶意资源名只作数据，缓存路径不越界；非法 profile ID 不改变数据库，目录外哨兵保留。
- .63 `go test -run=^$ -fuzz=FuzzClientImport -fuzztime=60s -parallel=4 ./internal/importer` 通过：96 个支持客户端的语料种子，1,201,454 次执行，61.037s；最大 1 MiB 与 HTTP edge 限额一致，另有 32 MiB 直接库调用资源探测。
- .32 Agent O6 第 7 条真实 repo→worker→DAV 覆盖：创建以及 name/host/enabled/logging_config/rule_sync_state/dns_follow/oxidns_ha 变化触发，version/status/applied_content 运行字段不触发；race 通过（1.774s）。

## 最终 .32 验收

修复提交：`6ca39f71`（凭据与编辑密码）、`762cdc56`（手动快照/导入器边界与性能）、`801ed02e`（路径拦截/会话/Agent/fuzz）。已变基到 `go` 的 `5535931d`；最终 `git rebase go` 无待变基提交；工作区干净。

以下在 .32 同一次 `rt -- bash -lc` 顺序执行，全部退出 0；所有 DAV 仅 loopback fake，未改 proddiff baseline/coverage/normalization：

```sh
cd server
gofmt -l internal/ tools/                  # 无输出
go vet ./...                             # 无输出
go test -race -count=1 -timeout=20m ./internal/importer/... ./internal/webdav/... ./internal/api/settings/... ./internal/server/...
go test -count=1 -timeout=20m ./...
CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=20m ./internal/webdav/... ./internal/api/settings/...
CONFIGFLOW_DIFF=1 go test -count=1 -timeout=20m ./internal/httpx/difftest/
cd ..
server/scripts/proddiff.sh
```

关键原始输出（完整日志在 .32 `/tmp/c35-final-{gofmt,vet,race,full,diff,httpx,proddiff}.log`）：

```text
# scoped race
ok configflow/internal/importer         26.674s
ok configflow/internal/webdav           5.019s
ok configflow/internal/webdav/difftest   1.121s  # 此轮未启用；下方启用运行
ok configflow/internal/api/settings     7.423s
ok configflow/internal/server           1.056s
# whole tree: 64 个有测试的包全部通过；包括
ok configflow/internal/importer          3.193s
ok configflow/internal/installer       298.375s
ok configflow/internal/server            0.039s
# real Python differential + TLS probe enabled under race
ok configflow/internal/webdav            4.144s
ok configflow/internal/webdav/difftest   39.009s
ok configflow/internal/api/settings      8.592s
ok configflow/internal/httpx/difftest     1.845s
# proddiff (summary 摘要)
self_check=PASS GET_routes_compared=65 routes_compared=66 cases=2831
coverage_regressions=0 unmapped_routes=0 unique_differences=10 new=0 stale=0
baseline: new=0 stale=0
```

proddiff 摘要仍有两端各 30 条响应正文中的 connection diagnostic 计数（隔离网络下的既有错误响应）；没有 status=0 的单边/双边 transport failure，未作为成功响应吞掉。所有 self-check 通过，10 个既有差异保留，无新增或过期 baseline 项。服务器高负载下全量 installer 用时约 298s，不改变测试断言。

## 后续：资源探测在全量并行 race 下的误超时

协调者复验发现 `wide` 单跑正常（普通 0.72s、race 6.8s），但全量包并行时可能超过原 30s 墙钟限制；这不是解析器资源失控。修复分支 `go-fix-importer-bounds` 从 `go` 的 `aeff9d75` 创建，仅改测试：

- 子进程启用 `RLIMIT_CPU` 硬上限，普通 3 CPU 秒、race 30 CPU 秒，按实际 user+system CPU 耗用计数，以原墙钟上限作为实际 CPU 预算，不把墙钟放宽转成计算预算放宽。
- 墙钟只作死锁/I/O 兜底，普通 30s、race 5min（均为原上限 10 倍），允许全量包并行时等待调度。
- 全部输入和 RSS 限额保持原规模：wide 2 万键、warnings 4 万条、large 32 MiB、deep 2 万层、递归 alias 和指数 merge；96 MiB Go 软内存限制、32 MiB 栈限制、普通/race 峰值 RSS 256/768 MiB 断言保留。CPU 硬限可终止持续展开或内存压力导致的持续 GC，成功返回仍检查实际峰值 RSS。
- 增加故意无限计算的子进程控制，断言被 CPU 硬限以 SIGKILL 终止且不是墙钟兜底杀死；Getrusage 失败现在明确报错，日志同时输出 CPU 用量。

.63 定向普通/race 均通过（1.520s / 12.520s）；wide 分别耗 440ms / 3.949s CPU，峰值约 61 / 188 MiB；无限计算控制普通/race 均在约 1 CPU 秒被终止。命令：

```sh
RT_HOST=sunbroke@192.168.0.63 rt -- bash -lc 'cd server && go test -count=1 -v ./internal/importer -run "TestAdversarialImporterResourceBounds|TestImportProbeCPULimit" && go test -race -count=1 -v ./internal/importer -run "TestAdversarialImporterResourceBounds|TestImportProbeCPULimit"'
```

.32 最终连续两次全量 `go test -race -count=1 ./...` 验证进行中，结果待补。
