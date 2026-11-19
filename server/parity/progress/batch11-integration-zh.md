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
core 留给协调者。各项实际结果见下文，未完成的检查不计通过。

## 工具链结果

已在 .32 实际使用 go1.27.4；`go install golang.org/x/vuln/cmd/govulncheck@latest` 安装 scanner v1.10.0。
`govulncheck ./...` 在 server 目录退出 0，输出 `No vulnerabilities found.`；DB https://vuln.go.dev，更新时间 2026-11-11 02:51:11 UTC。
日志在服务器 `~/.cache/batch11-govulncheck{,-version}.log`。
未运行 tidy，未修改其他依赖版本、go.sum、Agent 模块或 Dockerfile.agent。

首次来源审计发现 JSON-case 分支引入的共享 jsondecode.go 尚未声明到 pyref 来源组；这是 a 分支接入缺口，下一提交补精确来源声明并验证真实 Python transcript，仅更新来源元数据，不重造答案。其后定向测试因 set -e 尚未执行，不计通过。

来源审计继续发现 core-2 的 TestLocalWallZoneTransitionsAgainstPython 缺少 live oracle 注册；补一条精确 file:function:callee/count=1 的 CONFIGFLOW_DIFF guard 记录，不放宽 scope 或生产过程边界。

## 来源与定向结果

- sources.json 仅在 17 个实际依赖组追加同一个 jsondecode.go，未改既有分类或删除来源。
- TestAuditRepository PASS 0.789s；jsondecode 0.006s、agents 26.220s、dnsha 5.461s、rulesync 0.013s、deploy 6.509s，选择器为 `^TestReview|^TestAudit.*Exact|Protocol|Ownership|Prepared|Preparation|Identity`，均 PASS。
- CONFIGFLOW_DIFF=1 的真实 Python zoneinfo 七组边界实际执行，PASS 0.027s（不是 skip）。
- Go1.27.4 最终树 Python record/check：MosDNS 53.804s/52.888s，OxiDNS 44.837s/45.088s，routersplit 4.984s/4.793s，均 PASS。
- 独立核验全部 16 份 transcript 的完整 version/entries/input/venv SHA 不变，只改 Go 依赖来源指纹；原 byte manifest、录制 archive、proddiff baseline 未改。

下一步在此产品/测试树跑完整 preflight，随后在服务器 setsid/nohup 启动完整 compare。不同于先前批次，compare 不依赖 rt 会话。
