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

## 首次预检失败与独立修复

首次 full preflight 退出 1，三处失败：api/agents TestReviewConditionalMatchesWerkzeug、installer TestReviewDeflateAndZipMatchPython 是此前未覆盖的两份嵌套 pyref 指纹过期（a/b 改源码、c 改 go.mod 的组合元数据变化，待真实 record/check）；routers TestN89SyncPipelineAllocations 为 a 分支 jsondecode.objectMembers 的 Decoder 缓冲复制未知大值造成的真实回退。未进入 compare。

经协调者裁定，新增共享窄接口 jsondecode.Select：用原生 json.Unmarshal + 已有 borrowedJSON 扫描 root map，按精确名字只复制被选值，返回全 owned RawMessage；普通重复键取最后完整值；null 为空结果，其他非对象/畸形根沿用原生 type/syntax 类。不改 Unmarshal/Decode 行为，不暴露借用，不写自制 parser。
routers 的 withSyncEnvironment 和 selectedSyncSources 改用 Select，再解选中字段；Agent codec 与其 exact-name 规则未放宽。

验证（.32、Go1.27.4、GOMAXPROCS=4）：

- Select exact-input 11 组、所有权断言 PASS；0/1MiB 未选中值 856/860 B、8/8 allocs，原分配阈值检查 B/op 与 allocs/op。
- routers 原 TestN89SyncPipelineAllocations PASS 11.835s：cache rules1=66,261/65,287 B；rules16=1,016,853/997,262 B；下载正路径 delta=3,692,384 B（<原6MiB），隐藏 environment 负控 delta=63,318,532 B（>原6MiB）；原64KiB/6MiB阈值全部保持。
- FuzzSelectExactMembers 60.064s PASS，3,064,367 execs；对照原生 exact map，检查重复值、错误、输入不变与 owned 结果。
- 真实负控仅临时在 Select 入口 bytes.Clone 整个 root，未改断言；0/1MiB 888/1,058,491 B、9/10 allocs，明确因 Select copied unselected payload 失败；trap 恢复后 targeted race 的 Select/Review 全通过 3.400s，相关 vet 静默。

本修复按要求单独提交，交协调者安排 be408111 快速独立复核。其间继续补指纹与重新预检，复核认可后才进 compare。

## Select 独立复核与最终参考

协调者确认 be408111 已独立复核 `487095608`，认可、无阻断。唯一记录：逐字段解码可能改变内部 UnmarshalTypeError 的字段路径/顺序；类别和拒绝行为不变，协调者接受。此结论已放行复核条件，仍需最终预检干净后再启动 compare。

补齐全部18份 pyref（含两个嵌套 .json）真实 record/check，18,491条完整 version/entries/input/venv 全不变，仅来源元数据变化。
record/check 末行：MosDNS59.696s/54.131s、OxiDNS49.390s/47.198s、routersplit5.970s/5.381s、api/agents0.461s/0.403s、installer3.258s/3.362s，全部退出0。
Select 最终产品树再跑 govulncheck ./...：退出0，No vulnerabilities found.，日志 `~/.cache/batch11-govulncheck-final.log`。

再次完整预检中；通过后启动 compare 时告知协调者准确 tip，从启动到 compare 结束冻结 batch-11 提交，方便 .64 并行 core。
