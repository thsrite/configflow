# batch-11 集成

基底 origin/go@7941ae6f1；分支 batch-11，私有 worktree ~/program/configflow-go-batch-11。
按指定次序 rebase JSON 精确键名 37a1a4840（2 提交）、core-2 70cea1eb6（22 提交）；源分支未移动，未 push。

## 冲突处理

- ha_model.go：移除重复的普通 identity struct，保留 core-2 从 model.HASettings.MemberJSON 严格读取 group_id/peer_id；数字 R5 投影不能成为身份。
- prepared.go：使用 model.PreparedContent 自定义 exact codec，再用 MemberJSON 验证原始 profile/service token；使用 Deployment.MemberJSON 校验 submitted 原始 bool，删除已被替代的 struct projection。
- dnsha/payload.go：保留 core-2 model.Fields 逐个准确名字读取 envelope/bundle，并保留未知成员；不用 plain struct 解码。
- rulesync/adapter.go：Agent 返回 envelope 的 task 只按 model.Fields.Get("task") 读取，nativeTask 自定义精确 codec 解析其成员。
- deploy/types.go：File/Manifest 采用 model.Fields 精确读取各声明字段、required/null/bool 校验，删除 type plain 默认解码。
- dnsfollow/adapter.go：保留 core-2 typed registry 和共享脱敏逻辑；publicOptions 属于本服务自行 Marshal 的 DTO，脱敏后仍交 jsondecode facade，保留先叠分支的精确键约束。
- 无 Markdown/二进制冲突；24 个源提交全部保留。

## 工具链与验证计划

仅更新 server go 1.27.0 / toolchain go1.27.4、现有 x/text v0.43.0 的直接依赖标记，以及主 Dockerfile 三处 Go builder tag。
不改 Agent 模块、Dockerfile.agent 或依赖版本；不运行可能改动其他依赖的 tidy。
在测试服务器 .32 安装最新版 govulncheck，server 目录扫描；预检双 tag vet + 完整 non-race；预检后用 setsid/nohup 在服务器启动完整 compare。
core 留给协调者。当前尚未提前声明任何检查通过。

## 工具链结果

已在 .32 实际使用 go1.27.4；`go install golang.org/x/vuln/cmd/govulncheck@latest` 安装 scanner v1.10.0。
`govulncheck ./...` 在 server 目录退出 0，输出 `No vulnerabilities found.`；DB https://vuln.go.dev，更新时间 2026-11-11 02:51:11 UTC。
日志在服务器 `~/.cache/batch11-govulncheck{,-version}.log`。
未运行 tidy，未修改其他依赖版本、go.sum、Agent 模块或 Dockerfile.agent。

首次来源审计发现 JSON-case 分支引入的共享 jsondecode.go 尚未声明到 pyref 来源组；这是 a 分支接入缺口，下一提交补精确来源声明并验证真实 Python transcript，仅更新来源元数据，不重造答案。其后定向测试因 set -e 尚未执行，不计通过。
