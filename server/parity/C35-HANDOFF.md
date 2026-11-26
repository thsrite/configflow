# C35 交回记录

分支：`go-port-c35`；运行时实现提交截至 `632bb763`，补充导入器断言 `3c946518`。仅本地提交，未推送。

完成：四种客户端配置的纯 Go 导入器（Mihomo/Clash、Surge、Loon、Shadowrocket）；WebDAV 手动备份、连接测试、保留策略和 `repo.Hooks.ConfigurationCommitted` 自动备份。新增 `POST /api/backup/now`、`POST /api/backup/test` 两条路由；C06 路由达到 22/22。WebDAV 采用标准库 net/http、encoding/xml，无新依赖。

自动备份在提交后、锁外调度，1 秒 debounce；只保存一个待处理仓库引用，等待后读最新快照，串行上传。上传期间的多次保存合并为后续一次；禁用取消待处理项；失败不重试、不回滚。断连或取消等待不取消已提交备份，关闭服务在给定期限内等待当前上传。

安全验证全部使用合成凭据与 loopback fake。验证 JWT 门禁、密码遮罩复用、MKCOL/PUT/PROPFIND/DELETE 错误；远端错误正文故意包含合成用户名、密码和 URL。错误响应、任务失败记录和日志只保留安全错误类别/HTTP 状态。默认客户端拒绝跨 origin 重定向；不会把远端响应正文写入错误。完整备份按既有协议包含完整配置，因此上传 JSON 与原始完整导出结构相等。

## Python 覆盖及差异

- `test_config_import.py`：92 组合成语料经 Python 的公开 parse/build 生成预期，Go 作结构比较；四种客户端实际走 repo.ImportClientProfile 提交，保留已有未知字段，非法导入验证完全不写入。`test_import_route_dry_run_and_commit` 的 HTTP 部分仍归 **C34**；接口用法已写 PORTING.md，C34 进度也有接入提醒。
- `test_snell_shadow_tls.py`：补齐 C19 延后的三种导入语料，以及实际 Go 导入→Surge 导出往返测试。
- `test_webdav_auto_backup.py`：Flask↔Go HTTP difftest 比较 config/test/now 的成功、验证失败、JWT 失败及远端失败，逐次比较完整响应、存储、JSON sidecar、任务回执/通知和 DAV 文件。包含自动失败后仍保存、coalescing、待处理禁用、保留和非法已存保留值回退。worker 单元测试补覆盖慢上传、快照失败不重试/后续恢复、读/失败事务不调度、Agent 运行态忽略/配置增删改、锁外运行及关闭/取消。
- 原生实现差异：不模拟 Python 异常名称和 YAML 内部错误；保留中文前缀和 HTTP 状态。yaml.v3 拒绝重复键；一个非法布尔节点名的警告使用 `false` 而不是 `False`。配置 YAML/JSON 格式、时间和独立 fake URL 仅作结构或局部归一化。正常导入结果及存储无已知功能差异。
- 不再创建 Python 临时文件：JSON 在内存编码，非法 JSON 在 PUT 前拒绝。CPython thread.start 失败注入无 Go 对应机制；验证原生 lazy worker 的失败恢复/重启。
- 审查修复：节点复用签名保留 JSON 数字精度，避免把大于 2^53 的相邻整数当作同一个节点；语料包含复用与不复用两个对照。自动任务回执/通知明确绑定发起备份的仓库。

## 验收

下列命令均通过 rt 在 .32 执行（同一批次内依次运行；只在 .63 迭代，未在 Mac 测试）：

```sh
rt -- bash -lc 'cd server && gofmt -l internal/ tools/ && go vet ./...'
rt -- bash -lc 'cd server && go test -race -count=1 ./internal/importer/... ./internal/webdav/... ./internal/api/settings/... ./internal/server/...'
rt -- bash -lc 'cd server && go test -count=1 ./...'
rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/webdav/difftest/'
rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/httpx/difftest/'
rt -- server/scripts/proddiff.sh
```

格式检查和 vet 无输出，全部退出码为 0。race 输出：

```text
ok  configflow/internal/importer          1.488s
ok  configflow/internal/webdav            1.357s
ok  configflow/internal/webdav/difftest   1.025s  # 此次未启用差异测试
ok  configflow/internal/api/settings      3.186s
ok  configflow/internal/server            1.043s
```

全量测试末尾：

```text
ok  configflow/internal/webdav          0.225s
ok  configflow/internal/webdav/difftest 0.005s
ok  configflow/internal/weblistener     0.005s
ok  configflow/internal/weburl          0.003s
ok  configflow/internal/yamlx           0.480s
?   configflow/tools/proddiff/generator [no test files]
```

全量中的 installer 141.490s、server 0.015s。单独启用差异测试的输出：

```text
ok  configflow/internal/webdav/difftest 14.075s
ok  configflow/internal/httpx/difftest   0.876s
```

最后加强 YAML 错误中文前缀断言后，又在 .32 跑了全仓格式/vet 和 importer race：

```text
ok  configflow/internal/importer 1.416s
```

proddiff 末尾（JSON 长行只摘取计数）：

```text
self-check: status=PASS json-leaf=PASS yaml-leaf=PASS text-line=PASS
GET_routes_compared=65 cases=2060 coverage_regressions=0 unique_differences=5 new=0 stale=0
baseline: new=0 stale=0
```

5 项均为既有 baseline；两条新增 POST 路由不新增 GET coverage，未改 baseline/coverage 文件。

重建合成导入语料：

```sh
RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/importer/testdata/corpus.jsonl -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/importer/testdata/generate.py'
# 92 synthetic parse/build cases
```

完整提交：`ed13691f`、`45a044f8`、`decafb67`、`cb09668e`、`90866647`、`cb0c641e`、`531b88b2`、`632bb763`、`3c946518`；后续提交只写交回文档。

初次全量测试仅暴露 `server.TestNewAppRegistersImplementedRoutes` 的旧“备份路由应不存在”断言；现已改成必须存在，完整复跑通过。

## 新增文件行数

| 文件（相对 server/） | 行数 |
|---|---:|
| `internal/api/settings/backup.go` | 58 |
| `internal/api/settings/backup_adapter.go` | 49 |
| `internal/api/settings/backup_messages.go` | 12 |
| `internal/api/settings/backup_test.go` | 120 |
| `internal/importer/build.go` | 356 |
| `internal/importer/corpus_test.go` | 176 |
| `internal/importer/loon.go` | 179 |
| `internal/importer/messages.go` | 46 |
| `internal/importer/parse.go` | 423 |
| `internal/importer/proxies.go` | 312 |
| `internal/importer/sections.go` | 252 |
| `internal/importer/storage_test.go` | 146 |
| `internal/importer/testdata/corpus.jsonl` | 92 |
| `internal/importer/testdata/generate.py` | 205 |
| `internal/importer/text.go` | 287 |
| `internal/importer/types.go` | 216 |
| `internal/webdav/adapter.go` | 149 |
| `internal/webdav/backup.go` | 124 |
| `internal/webdav/backup_test.go` | 180 |
| `internal/webdav/changes.go` | 81 |
| `internal/webdav/client.go` | 246 |
| `internal/webdav/difftest/cases_test.go` | 99 |
| `internal/webdav/difftest/diff_test.go` | 449 |
| `internal/webdav/difftest/testdata/reference.py` | 67 |
| `internal/webdav/lifecycle_test.go` | 151 |
| `internal/webdav/messages.go` | 15 |
| `internal/webdav/settings.go` | 128 |
| `internal/webdav/worker.go` | 128 |
| `internal/webdav/worker_test.go` | 182 |

另修改 api/settings 的原有注册/retention 适配、server 的生产 wiring/装配测试、parity/routes.tsv、PORTING.md 和 PROGRESS-config.md。生产 jsonx 接口仅留在存储适配文件；导入器与 WebDAV 核心使用原生类型。
