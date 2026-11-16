# ConfigFlow 协作指引

## 文档路由

按任务读对应文档，不要整仓扫描：

| 任务 | 先读 |
|---|---|
| 改前端界面、样式、组件、交互、动效，或新增页面 | [DESIGN.md](DESIGN.md) |

## 文档同步

- 功能或操作入口变更时，必须同步更新 `doc/` 中对应的说明和截图。

## 测试

- 后端全量：`scripts/test-backend.sh`（pytest-xdist 并行；单个文件或调试仍可直接 `python -m pytest backend/test_x.py`）。
- 前端全量：`cd frontend && npm test`。

## Go 后端（`server/`）代码风格

适用于 `server/` 下新写或改动的 Go 代码（旧代码在 S 轮次中顺带改）。移植规则见 `server/PORTING.md`，agent 共用说明见 `server/AGENT-BRIEF.md`。

- **命名**：遵循 Go 惯例。包名短、全小写、不加下划线；导出名不重复包名（`repo.Store`，而不是 `repo.RepoStore`）；缩写保持大小写一致（`ID`、`URL`、`HTTP`、`DNS`）；接收者名用一两个字母。
- **错误**：用 `fmt.Errorf("做什么: %w", err)` 包装，保留错误链，判断时用 `errors.Is`/`errors.As`；哨兵错误命名为 `ErrXxx`。错误不要既记日志又返回，二选一。
- **不用 panic**：请求路径和后台任务里都不 panic。只有程序启动阶段的编程错误（例如 `regexp.MustCompile` 编译常量）可以 panic。后台 goroutine 顶层要 recover，并通过 `logx` 记录。
- **context**：可能阻塞或做 I/O 的函数第一个参数是 `ctx context.Context`；不要把 ctx 存进结构体。外部请求、子进程、设备连接都要带超时。
- **文档注释**：每个导出标识符都有以名字开头的注释；每个包有 `// Package xxx ...` 注释，说明它负责什么、不负责什么。
- **测试**：优先用表驱动测试（`tests := []struct{...}` + `t.Run`），辅助函数调用 `t.Helper()`；用 `t.TempDir()`；测试之间不共享可变全局状态。
- **中文提示**：返回给前端或 Agent 的中文消息定义成包级常量或 `fmt` 模板常量，集中放在 `messages.go`，不要在逻辑中散写字符串字面量；测试引用同一个常量。
- **日志**：统一用 `internal/logx`，不用 `log`、`fmt.Print*` 或直接写 stderr。日志里不出现密码、token、订阅链接（用 `weburl` 的脱敏函数）。
- **检查**：提交前在测试服务器上执行 `gofmt -l` 无输出、`go vet ./...` 通过；不要提交调试文件（`zz_*`、临时 testdata）。
