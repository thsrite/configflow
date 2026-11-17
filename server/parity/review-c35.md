# C35 对抗性复核

状态：进行中。分支 `go-review-c35` 从本地 `go` 新建；没有子 agent、推送或生产设备访问。

## 发现与修复

- 高：`GET /api/backup/config` 原样返回 URL userinfo；目录错误的脱敏只覆盖独立用户名/密码，未覆盖 URL userinfo 和百分号编码凭据，导致手动/自动任务错误和 JSON 回执泄露。已剥除编辑响应中的 userinfo，错误路径统一覆盖两组凭据及编码变体；异常远端正文仍不读入错误。修复前 HTTP 和诊断回归测试均失败，修复后通过。
- 中：保存备份表单的空密码覆盖原密码。按协调者明确要求，空值与 `******` 都保留原密码，真实新密码仍可替换。这与 Python 的空值清空行为不同，已记入行为变更账本。
- 已核对禁用行为：repo.update 在调用提交后 hook 前过滤 auto_backup=false；禁用提交不会启动 worker 或重置 pending deadline。原实现没有这项缺陷，新增测试通过。

## 已有证据

`.32`（loopback fake DAV）：

```sh
rt --back server/internal/api/settings/review_c35_test.go --back server/internal/webdav/review_c35_test.go --back server/internal/webdav/client.go --back server/internal/api/settings/backup.go -- bash -lc 'cd server && gofmt -w internal/api/settings/review_c35_test.go internal/webdav/review_c35_test.go internal/webdav/client.go internal/api/settings/backup.go && go vet ./... && go test -race -count=1 ./internal/api/settings/... ./internal/webdav/...'
```

退出 0；vet 无输出：settings 4.997s、webdav 1.821s、webdav/difftest 1.050s（该次未启用 Flask 差分）。

剩余：导入器非法/超大/深层/恶意 YAML 和 Python 结构比较，TLS/重定向，自动备份并发/取消/worker 释放，完整差分，最终 .32 全量验收/proddiff/rebase。
