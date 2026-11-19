# N8-10 installer S pass

状态：实现完成，最终定向验证进行中；待 fresh Opus HR，未宣称 verified。
分支 `go-n8-10-installer`，已 rebase 到 core-2 HR tip `70cea1eb6`。
实现提交 `4dd8a10bb`、`c63d2e6b5`；后续收尾提交见分支 log。未 push。

## 完成边界

- 单 Agent、同机 AIO、Docker Compose/Run、网关安装脚本改用 native options 和配置 DTO；
  HA script/bundle/direct/compact 与迁移命令改用 typed plan、Compose service/mount/
  environment/network DTO。YAML 使用现有原生 writer，保持既有 L1 修复和 shell 业务。
- HA HTTP guide/enroll 已接 `GenerateGuideJSON` / `IssueCapability` / `EnrollIdentity`，
  registry 使用 core-2 `EditAgents` typed transaction。Agent 身份只认原始 JSON string；
  port 用 big.Rat 精确比较等值 JSON 数值，不让 bool/string 或浮点舍入成为证明。exact-key model.Fields
  处理外部协议、重复键拒绝，未知 JSON 成员保留，不用 struct 大小写折叠解码 Agent。
- preflight 使用 typed report/check 和精确 probe DTO；网卡、ss、版本复用原生文本规则。
  删除私有 pyint/pyip、pystr/PythonClass 和 CPython zip/deflate 仿真。ZIP 改为 archive/zip；
  五个实际安装成员的内容、顺序、Unix mode 仍严格检查，压缩流不是安装契约。
- capability 和 install-command Store 脱离浏览器断线、继续跟随 Runtime shutdown；
  入场、锁后、随机数后、文件替换和 SQLite/registry commit 前检查 ctx。随机数失败
  返回错误，不在请求路径 panic。停机/旧 generation 不写，断线后合法操作仍完成。
- 仅改 installer、必要 API 接线与对应比较工具/文档，没有改 monitor/core 公共 API、
  go.mod、依赖或 Dockerfile；没有跑完整门禁。

## 明确保留与 N8-11

三个 `*_legacy.go` 和 `options_legacy.go` 只为未迁消费方保留旧参数/结果 codec；
业务只在 native 实现。API 通用 binder、gateway/installation 等旧输入消费方不属于
本步骤。没有把这些 facade 宣称已删除；installer 生产 jsonx import 仅剩三个 legacy
codec，pystr/PythonClass 为零。旧 PyInt/PyStr 导出入口已删除，API 数字入口直接复用
`taskstatus.Count`。人工 numeric IPv4 renderer 兼容限于 legacy codec，生产 guide
拒绝 numeric/bool 身份；该兼容随 N8-11 删除。

`identityJSON` 是**保留的 D2 身份编码，非 legacy facade**：只编码已发布 spec index、
Compose owner/project，保持 sorted-key ASCII JSON 与原空格，无 jsonx/pystr、无业务。
Unicode/DEL/emoji、浮点形式、超大整数和负零有 byte golden。以后统一 D2 owner 时迁，
不可在删 facade 时重发身份。已在 remaining-work §3.4 登记。普通配置/协议/存储使用
原生 JSON DTO；结构比较只解析精确嵌入的 JSON，不对 shell 作宽泛归一化。

## 裁定与兼容性证据

1. 会话 index/session.id 均须 32 位小写 hex 且相同，损坏 issue409/enroll401、无写入。
   Python 唯一合法创建入口 `backend/agents/docker_ha_enrollment.py:58` 是 uuid4().hex。
   .32 离线快照仅计数：会话 0，非规范 ID 0；没有复制任何内容。
2. 机器网卡序号/ss 计数/版本只认 ASCII，文本采用 strings.Fields/TrimSpace；异常
   unknown 不能令 preflight 通过。脚本尾空白用 TrimRightFunc(unicode.IsSpace)，
   不保留 C0 1c–1f 私有 Python whitespace 谓词；真实模板固定 LF。
3. 整数复用 taskstatus.Count，保留 ASCII sign/underscore 十进制、拒绝非 ASCII 数字，
   不模拟 4300 位限制。Agent port 1..65535，HA 1024..65535。timeout 选择明确拒绝：
   超出 int64 或 time.Duration 秒数承载范围（±9223372036）均返回 ValidationError；
   两类溢出各有回归，不发生回绕。
4. 坏 registry 非对象成员沿 core repo fail-closed500，无写入；不复刻 Python 找到
   前方 owned Agent 后忽略坏尾部的短路。IssueCapability 在 mkdir 前拒绝错型身份，
   三条旧空目录副作用行及一条旧 helper 成功行精确绑定。
5. 危险目录比较与身份/迁移目录重叠比较前 path.Clean；只归一化比较值。覆盖 `//`、
   `//etc`、`//opt`、`/etc/`、`/./etc`、`/opt/../etc` 以及有效 `//srv/native`；
   有效请求的原始路径和原 D2 project 保留。grep 后同类 bundle/direct identity 与
   migration bind overlap 一并修复。全部测试只用合成路径，无真实系统目录 I/O。
6. HTTP guide 在创建凭据前拒绝 numeric/bool agent_ip，使用现有 IPv4 中文400，无目录/
   Agent 写入。前端只发 string。四种旧 int-ip HTTP 请求按完整 body 精确对应原200/
   新400，检查 Python 原 session 确实存 numeric host；清理仅这些合成旧侧文件后继续
   比较其他持久状态。另保留原 macvlan numeric 拒绝类。不存在新的生产 numeric 身份。
7. expires=NaN 的非标准存储 JSON fail closed，enroll401/issue409，无写入，不保留
   NaN parser。旧 Python 永不失效/成功行完整保留。所有批准变化登记 behaviour-changes。

新独立参考均由服务器上的真实 Python或字面政策生成，不调用 Go：

- n810_preflight_native：19 行，保留完整 Python report/error 与完整 native report。
- n810_session_native：70 行，保留原输入/结果/文件副作用/registry 副作用。
- n810_scalar_native：600 行（40 integer/timeout + 560 人工尾空白），完整新脚本或错误。

每行绑定原 archive SHA 与完整原行 SHA，原语料不动，有伪造 binding 负控；
unknown preflight、非法会话无写入、JSON 值改变与 shell 前后任意字节改变均有负控。
冻结 shell SHA 仅还原既有批准的 L1 字面 source delta，并解析三个固定 JSON heredoc；
bootstrap 只解析固定 settings=json.loads 的 JSON literal。其余 shell 字节不放宽。
ZIP enrollment.json 只比较解析后的 JSON，其他文件 bytes/mode/name 严格保留。

ZIP pyref 先 check 确认 233 个 deflate 输入和 60 个 ZIP 的 payload/mode 全相等，仅
source metadata 不同，再显式 record metadata，随后 check PASS。旧 input_sha256、venv_dependencies_sha256、entries 完全不变，
没有重录原输入/输出来迁就实现。source manifest 只登记真实测试依赖与新 composeArchive
产品边界，不豁免 helper。

## 对外接口（供 upgrades 接续）

- `GenerateGuideJSON(data json.RawMessage, enrollment *Capability) (*GuideResult, error)`：
  exact-key 请求；`DecodeGuideRequest` 可先得到 typed request；生成错误为 ValidationError
  （HTTP400）或 RenderError（坏内部形态），EnrollmentError 带既有中文和 HTTP status。
- `IssueCapability(ctx context.Context, dataDir string, request json.RawMessage, profileID string)
  (*Capability, error)`：返回 URL、Token、ExpiresAt(json.Number)。24h、spec index、重试复用
  和过期重发不变。请求身份须原始 string；目录创建前准入，关机不提交。
- `EnrollIdentity(ctx context.Context, dataDir string, manager AgentEditor, sessionID, token string,
  present bool) (AgentIdentity, error)`：返回 AgentID/Token。AgentEditor 接口为
  `EditAgents(context.Context, string, func(context.Context, *[]model.Agent) error) error`；
  core Manager 直接满足。registry owner 负责事务与关机前提交检查；重放必须匹配原始
  marker/name/host/profile/service/method/port/id/token，同事务检查冲突、创建或返回旧身份。
- `AgentInstallOptions` / `RenderAgentInstall`，`AIOInstallOptions` / `DefaultAIOOptions` /
  `RenderAIOInstall`，`DockerInstallOptions` / `DefaultDockerOptions` / `RenderDockerCompose` /
  `RenderDockerRun`，`GatewayInstallOptions` / `RenderGatewayInstall` 是 native renderer。
  Agent timeout 为 *big.Int（nil默认30），AIO/Gateway ports为int，Docker ports为机器字符串。
- `EvaluatePreflight(output, mode string) (*PreflightReport, error)`，`CollectorScript()`。
- Store 的 SaveInstallScript/LoadInstallScript 都接 ctx；继承 originating Runtime，
  调用者不能用新的 Runtime 或 Background 替换原 shutdown 来源。

## 定向验证

所有命令通过 rt 在 .32/.64 执行，本机只编辑/git/轻量检查。
最终包级 race 与完整 HTTP fuzz 结果待本轮完成后补入。

- .32：`GOMAXPROCS=4 go test -p 4 -count=1 -run "N810|FuzzCorpus|Matrix|DockerHAReview"
  ./internal/installer ./internal/api/agents ./internal/api/agents/difftest`：PASS，
  installer 12.515s、API 7.387s、比较负控 0.023s。
- .64：`GOMAXPROCS=4 go vet -p 4 ./...`：exit0，静默。
- .64：`GOMAXPROCS=4 go test -p 4 -count=1 -run "^$" -fuzz "^FuzzInstallerNativeProtocols$"
  -fuzztime=60s -parallel=2 ./internal/installer`：PASS 61.010s，601670 executions。
- .64 ZIP pyref metadata：check→显式record→check，最后两次分别 1.252s / 1.237s PASS；
  本地只检查 JSON 差异：唯一变化 source metadata，input/venv/entries 不变。
- .32：`GOMAXPROCS=4 go test -race -p 4 -count=1 -run "N810|InstallationIdentityJSONGolden"
  ./internal/installer ./internal/api/agents ./internal/api/agents/difftest`：PASS，1.220s /
  30.842s / 1.154s（含最后的精确 port proof 与 SQLite commit ctx 检查）。
- .32：`GOMAXPROCS=4 CONFIGFLOW_DIFF=1 CONFIGFLOW_INSTALLER_FUZZ_ITERATIONS=40 go test
  -p 4 -count=1 -timeout=20m -run "InstallerDifferential|InstallerFuzz|DockerHADifferential|L1CompactColon"
  ./internal/api/agents/difftest`：PASS 36.286s，固定正常路径/错误类和每 phase 40 条 HTTP fuzz。
- .32：`GOMAXPROCS=4 go run ./tools/pyref -scopes server/internal/installer`：PASS，
  10 registered call sites、1 scope，无未登记 process/live oracle。

集成点：fresh HR 安全复核及协调者 batch gate；upgrades 改接上述 typed 协议；邻居消费方
完成后删 legacy codec。此基底的仓库级 pyref 审计缺 core zoneinfo live oracle 登记，
协调者确认 batch-11 `dd5041faa` 已修；无需本分支改公共文件，合入新 go 后复验。
