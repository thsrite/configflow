# C35 对抗性复核

状态：进行中。分支 `go-review-c35` 从本地 `go` 新建；没有子 agent、推送或生产设备访问。

## 发现与修复

- 高：`GET /api/backup/config` 原样返回 URL userinfo；目录错误的脱敏只覆盖独立用户名/密码，未覆盖 URL userinfo 和百分号编码凭据，导致手动/自动任务错误和 JSON 回执泄露。已剥除编辑响应中的 userinfo，错误路径统一覆盖两组凭据及编码变体；异常远端正文仍不读入错误。修复前 HTTP 和诊断回归测试均失败，修复后通过。
- 中：保存备份表单的空密码覆盖原密码。按协调者明确要求，空值与 `******` 都保留原密码，真实新密码仍可替换。这与 Python 的空值清空行为不同，已记入行为变更账本。
- 中：手动备份在应用有效的请求参数前，先解析了不使用的已存连接字段；已存 username/path 类型错误时 Python 成功、Go 500 且不上传/不记回执。拆开文档捕获和自动任务设置读取，手动备份只验证实际请求设置；新增完整 Flask/存储/DAV 差分修复前失败、修复后通过。
- 中：Surge WebSocket 的显式空 Host 被 obfs-host 覆盖，与 Python 不同，可能改变实际连接目标。按 map 键是否存在决定回退；新增 Python 原始结构语料修复前失败、修复后通过。
- 低：照搬 Python 的警告去重列表和 section 查找导致二次复杂度。改用 Go map 索引，警告只保存显示的前 8 项与精确去重计数，标准库 slices.Contains 替代私有 contains 副本；Python 警告顺序/去重/总数语料不变。
- 已核对禁用行为：repo.update 在调用提交后 hook 前过滤 auto_backup=false；禁用提交不会启动 worker 或重置 pending deadline。原实现没有这项缺陷，新增测试通过。

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

剩余：导入路径隔离、fuzz、最后分支复查，最终 .32 全量验收/proddiff/rebase。
