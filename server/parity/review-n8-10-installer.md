# N8-10 installer S pass HR 复核（fresh Opus，安全相关）

复核对象：`go-n8-10-installer@6d84df68c`（代码 tip `bae7b9314`，基于 go@ae00b71f1），作者 Codex 7e69d031。
复核分支 `go-opus-review-n8-10-installer`。对照 Python（`backend/agents/docker_ha_*.py`）和迁移前 Go（ae00b71f1）
阅读代码。定向测试只在 .64 经 `rt` 运行（GOMAXPROCS=4，-p 4）。没有重跑作者整套比较，也没有跑完整门禁。
全部用例只用合成路径/数据，不碰真实系统目录。

## 结论

high 0，medium 1（已修），low 5（3 已修、2 记录）。L1 已按协调者裁定修复。修复随本分支叠批后，installer 可以 verified。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| R1 | medium | 已修 | **根目录别名 `//` 能绕过 guide 路径规则（裁定 6 的同类路径）。** `guidePath` 用原文 `text == "/"` 拒绝根目录，而 `canonicalInstallPath("//")` 按 POSIX 保留双斜线，结果 `//` 被当成规范路径。所有经 `guidePath` 的输入都受影响：脚本和安装包模式的 `data_dir`（数据写到 `/agent`、`/oxidns`、`/ha`）、`ha_host_id_path`、迁移的 `ha_data_dir`/`backup_dir`/bind 挂载源。安装包的身份文件重叠检查用 `dataCheck+"/"+part`，`dataCheck` 为 `/` 时变成 `//agent`，永远不匹配，所以 `data_dir="//"` 加 `ha_host_id_path="//agent/host-id"` 也能生成。精简/直接 Compose 碰巧被系统目录检查挡住，其他模式没有。作者的别名修复只覆盖了系统目录检查和重叠比较，漏了这条根目录规则。修复：`guidePath` 改为 `path.Clean(text) == "/"`，`//` 与 `/` 一样返回该路径既有的中文 400，并且发生在凭据、目录或输出之前。回归 `TestReviewN810RootAliasIsNotAnInstallPath` 覆盖 6 类（script/bundle/compact 数据目录、身份文件、迁移备份目录、迁移挂载），另有正控：`//srv/native` 仍然有效。修复前 6 个子测试全部失败（script/bundle/身份文件三项直接生成成功），修复后通过。作者测试 `TestN810LinuxPathAliasesCannotBypassInstallerGuards` 的 `//` 子项原先期望系统目录提示，现在先命中路径规则，所以只改了这一项的期望消息（这是本 pass 新增的测试，不是冻结语料；其余别名期望不变）。installer 全包 race（含全部 Python 语料和 pyref）通过，说明没有任何原 Python 行依赖 `//` 被接受。behaviour-changes 危险目录一节已补登记。 |
| R2 | low | 已修 | **原生 `RenderDockerRun` 把端口字符串直接拼进 shell。** S pass 把 `DockerInstallOptions` 的端口改成“机器字符串”，并作为 upgrades 接续用的导出 API；`-p <port>:<port>` 和 `-e AGENT_*_PORT=<port>` 都没有转义（HA 路径另有 `checkHAPort` 校验，不受影响）。生产 HTTP 路径经 `options_legacy` 把端口转成整数文本，目前不可利用；但直接调用原生 API 时传入 `8080;touch …` 会生成可执行的注入。修复：这 6 处改用已有的 `shellWord(…, true)`。整数、负数和 `True`/`False` 都在允许字符集内，字节完全不变（installer 全包语料、api/agents Docker 测试、difftest 均通过）。回归 `TestReviewN810NativeDockerRunQuotesPortText`：恶意文本必须是单个引号词，整数端口字节不变。修复前失败（输出含裸 `;touch`），修复后通过。Compose YAML 由 YAML writer 负责转义，不受影响。 |
| L1 | low | 已裁定并修复 | **ss 端口证据中只含非 ASCII 空白的行被当成空行丢弃，preflight 判为空闲/通过。** `preflightPortCheck` 的 ss 分支用 `strings.TrimSpace(line) != ""` 过滤空行，NBSP、U+3000、U+0085、`\v` 组成的行都会被丢掉。如果全部是这种行，就判定“未发现监听”，结果 pass（复核时用临时探针确认 udp53=pass，整体也是 pass）。Python 的 `line.strip()` 行为相同，真实 `ss` 也不会输出这种行，所以只影响人工改写的粘贴文本。但裁定 3 要求“不符合 → unknown，unknown 绝不能判为空闲”，裁定 9 又要求机器文本空白按 native（Unicode）处理，两条在这里冲突。建议：空行只认 ASCII 空白（`strings.Trim(line, " \t") == ""`），其余行必须解析成 5 列，否则 unknown。可能需要为 preflight 语料补绑定行，所以先提交协调者裁定。**裁定：裁定 3 优先。** 已修：ss 分支只把 ASCII 空格/制表符组成的行视为空行，其余行必须解析为 5 列，否则 unknown。回归 `TestReviewN810SSUnicodeBlankLineIsUnknown` 覆盖 NBSP/U+3000/U+0085/`\v`/`\f`（udp53、tcp53 和汇总都必须是 unknown），另有 ASCII 空行仍然 pass 的正控。修复前 NBSP 用例失败（`udp53=pass, overall pass`），修复后通过。installer 全包语料没有受影响的行，所以不需要新增绑定；behaviour-changes 已登记。 |
| L2 | low | 记录 | **生产 renderer 仍留有只供测试 codec 使用的兼容分支。** `GuideOptions.addressNode/addressDisplay/imageRaw`、`GuideRequest.imageRaw` 及其在 `guideComposeNative`、`generateCompactNative`、`planHash`、`imageNode` 中的分支，只有 `guide_legacy_test.go` 会赋值（用于回放 Python 的 numeric IPv4 和非字符串 image）。这些字段未导出，生产路径到不了，所以没有安全影响；但作者报告里“纯 renderer 的人工 numeric IPv4 兼容现在只在测试 codec”并不完全准确，生产代码里仍有对应分支。同类：`options_legacy.go` 的 `compatIntegerText` 对 bool 返回 `True`/`False`（Python `str(bool)`），`api/agents/installer.go` 把 `installer.PyStrip` 换成了共享的 `pystr.Strip`。这些都已登记为 N8-11 删除项，建议删 options codec 时一起清掉。另外 `decodeIPv4Value` 在生产中仍接受 `vip` 和 `ha_gateway` 的 numeric/bool 形式（与 Python 一致，输出前都经 IPv4 校验并转回文本，不属于身份字段，不在裁定 7 范围内）。 |
| L3 | low | 记录 | **原生 Gateway/AIO API 把端口 0 当成“使用默认值”。** `RenderGatewayInstall` 用 `o.MihomoAgentPort != 0` 判断是否覆盖默认端口，直接调用原生 API 时显式传 0 会静默得到 8080/8082，而不是 `Invalid Agent port`。生产路径经 `compatPort` 把 0 转成 `portError` 并向下传递，仍然拒绝。只影响以后的原生调用方（upgrades），记录供接续时注意。 |
| L4 | low | 已修 | **behaviour-changes 重复登记。** “protocol IPv4 and non-JSON stored expiry”和“string IPv4 identities and finite session JSON”两节内容基本相同（numeric agent_ip 400、NaN expires）。不影响行为。按协调者要求已删除前一节；后一节内容完整，保留。 |

另外两点只作说明，不算发现：(a) 安装会话文件同时保存明文 token 和 token_hash（0600）。这是 Python 设计：有效期内重复生成命令要返回同一凭据；enroll 只比较 hash（`subtle.ConstantTimeCompare`）。(b) 安装命令 snapshot 以明文 22 字符 token 作为主键，有效期内可以重复读取，与 Python 一致，不是一次性凭据。

## 逐项核对

1. **裁定 1（会话 ID）**：`isSessionID` 要求 32 位小写 hex。IssueCapability 先校验 index 字节（无效 UTF-8、`../`、带换行都 409），再要求 `session.id` 是原始 JSON string、通过同一校验并且与 index 相同；EnrollIdentity 先校验路径参数，再要求文件里的 id 与参数一致。由存储值拼出的路径只有 `<index>.json`、`<id>.json/.tmp`，都在校验之后。enroll 中的 `profile_id` 经 `repo.ValidateProfileID` 后才用于 registry。作者的 7 类损坏用例（null/父目录/绝对路径/不一致/数字/大写/index 父目录）都检查了无写入和 registry 未调用。会话文件内的无效 UTF-8 由 `model.Fields`（`utf8.Valid`）拒绝；复核新增 `TestReviewN810SessionInvalidUTF8NeverWrites`，覆盖 name 字节、未知成员字节和过期续发三种情况，issue 409、enroll 401、无写入（当前实现已通过，作为覆盖固定下来）。
2. **裁定 2（identityJSON）**：只被 `installationSpecDigest` 和 `planHash` 调用；不 import jsonx/pystr；non-compact 用 `", "`/`": "`，与 Python `json.dumps(sort_keys=True)` 一致。复核用临时探针，在 lone surrogate、控制字符/DEL/U+2028、`<>&`、`1e15`/`1e16`/`5e-324`/`1.7976931348623157e308`/`1E+2`/`-0.0`/`1e-5`、大整数、嵌套空容器、null/bool 上，逐字节对比 `identityJSON` 与旧 `jsonx.Encode(EnsureASCII, SortKeys[, Compact])`，两种分隔符全部相同。作者 golden 和 `//srv/native` 的 project 名断言也锁住了已发布身份。探针已删除。
3. **裁定 3（机器计数）**：网卡序号（`matchAddressLine`）、ss 的 Recv-Q/Send-Q（`asciiDigits`）、版本分量、协议行返回码都只认 ASCII 数字；格式不符时各项给 unknown，汇总按 fail > unknown > warning 排序，unknown 不会变成 pass。唯一例外见 L1（只含 Unicode 空白的行被丢弃）。
4. **裁定 4（端口/timeout）**：API 层 `rejectNonASCIIMachineNumber` 拒绝非 ASCII 数字；`parseInstallInteger` 经 `taskstatus.Count` 只认 ASCII 十进制。Agent 端口 1..65535，HA 端口 1024..65535。timeout 在 `RenderAgentInstall` 中按 `IsInt64` 和 ±`MaxInt64/1e9` 拒绝，不会回绕。首尾 Unicode 空白（如 NBSP）由 `TrimSpace` 去掉后接受，与 Python `int()` 一致，渲染时输出规范整数，没有注入面。
5. **裁定 5（registry / 早准入）**：enroll 的 registry 编辑走 core `EditAgents` → `repo.UpdateSystem`，非对象成员在 `model.System` 解码时 fail closed（session 参考中的 `model-type`/500 行）。IssueCapability 在 `MkdirAll` 之前检查 name/host（原始 string）和 port（uint16 且 ≥1024）。HTTP guide 先执行 `GenerateGuideJSON(raw, nil)` 完成全部校验，再签发凭据。
6. **裁定 6（危险目录）**：`composeDataDirRefused` 先 `path.Clean`；`installPathsOverlap`、bundle/direct 的身份文件重叠检查、迁移 bind 重叠检查都比较 Clean 后的值；AIO 的 config path 用 `insideAgentDirectory`/`purePosixStr` 归一化。漏掉的 `guidePath` 根目录规则见 R1，已修。其余原文比较（`install_core` 的默认路径相等判断）属于“非默认即拒绝”，别名只会被拒绝，不能绕过。
7. **裁定 7（numeric agent_ip）**：`DecodeGuideRequest` 要求 `agent_ip` 是原始 string，否则返回 msgFixedIPv4（400），且在 IssueCapability 之前；IssueCapability 自身也要求 string。`TestN810InstallerNumericIdentityNeverIssues` 覆盖了 HTTP 层。
8. **裁定 8（NaN）**：`NaN`/`Infinity` 不是合法 JSON，`model.Fields` 解码失败 → issue 409 / enroll 401，无写入（探针确认 enroll 返回 `EnrollmentError` 安装凭据无效）。`1e999` 这类合法大数按 big.Rat 比较，等同于永不过期，与 Python（inf）一致。
9. **裁定 9（空白）**：`trimScriptTail` = `TrimRightFunc(unicode.IsSpace)`；采集行只按 LF/CRLF 分行，base64 用标准库，并拒绝内嵌 CR/LF。shell 比较由作者的 `TestN810ArtifactComparisonKeepsShellBytesAndJSONValues` 负控锁住，只解析固定 JSON heredoc。
10. **shell 注入**：guide 的 docker run 参数统一经 `haRun`/`joinQuoted`/`q`（shlex quote），唯一的 raw 参数是固定的 `"$cf_cid_path"`。prepare/new/migration 中的容器名、网络名、路径、owner/project、镜像都经过 `q`；bootstrap heredoc 使用带引号的 `'CF_DOCKER_HA_ENROLL'`，设置用 JSON 嵌入 Python 字符串字面量，URL 和 token 由 `inlineSettingsJSON` 转义，并且 compact 路径另外校验 session/token 格式。输入侧：agent 名、单元名、parent、容器名都用 ASCII 字符类校验；路径禁止 `,`、`:`、NUL、CR/LF；server_url 拒绝凭据、query 和 fragment。Docker Agent run 的端口见 R2，已修。
11. **凭据 / 日志 / 事务**：enroll 只比较 token 的 SHA-256（常量时间）；Agent 的冲突检查、重放和创建在同一个 `EditAgents` 事务中完成，提交前检查 `tx.Err()`。installer 生产代码没有日志调用；API 只记录名称、类型、端口和 Agent 类型，没有 token 或 URL。随机数失败时返回错误，不 panic。
12. **ctx**：IssueCapability、EnrollIdentity 和 Store 的 Save/Load 都先 `lifecycle.Detach`，在锁后、随机数后、写文件前、rename 前、SQLite COMMIT 前检查 `ctx.Err()`；关机后的 COMMIT 不执行，连接关闭时回滚。
13. **期望 / 语料 / 登记**：原 archive 和 pyref entries 没有修改（作者用 SHA 绑定新增行，复核未改语料）。复核只改了作者新测试中 `//` 一项的期望消息（见 R1）。behaviour-changes 已补 R1；L4 是重复登记。延后清单（options codec、identityJSON 保留、legacy codec 只在测试中）与代码一致，补充说明见 L2。

## 提交与测试

提交：本分支 `go(installer): refuse root path alias and quote native Docker port words`（R1、R2 修复，回归测试，behaviour-changes 补登记，本报告）。

rt 命令（均为 `RT_HOST=sunbroke@192.168.0.64`，在 `server/` 下执行）：

1. 临时探针（已删除）：`go test -run ZZReview -v ./internal/installer`：identityJSON 与 jsonx 在全部样例上相同；jsondecode 拒绝 `NaN`/`Infinity`，enroll 返回 401；ss 中只含 NBSP/U+3000/U+0085/`\v` 的行 → udp53=pass（L1）。
2. 修复前：`go test -run ReviewN810RootAlias ./internal/installer` → 6 个子测试 FAIL（script/bundle/身份文件三项 `<nil>`，即生成成功）。
3. 修复前（临时还原 docker.go，已恢复）：`go test -run ReviewN810NativeDockerRun ./internal/installer` → FAIL（`port text is not one quoted shell word`，输出含裸 `;touch`）。
4. 修复后：`gofmt -l internal/installer`（无输出）；`go vet ./internal/installer ./internal/api/agents/...`（通过）。
5. 修复后：`go test -race -p 4 -count=1 -timeout=40m ./internal/installer` → `ok 299.352s`（全包，含 Python 语料、pyref、golden）。
6. 修复后：`go test -race -p 4 -count=1 -run "Docker|Install|N810|L1" ./internal/api/agents` → `ok 44.405s`；`CONFIGFLOW_DIFF=1 go test -p 4 -count=1 -run "InstallerDifferential|DockerHADifferential|L1CompactColon|N810" ./internal/api/agents/difftest` → `ok 16.366s`。
7. `go test -run ReviewN810 -v ./internal/installer` → 三个复核测试都 PASS。

## 协调者裁定

- L1：采用复核建议，裁定 3 优先：unknown 绝不能放行。已在本分支修复（见 L1）。

L1 修复的验证（.64）：修复前（临时还原 docker_ha_preflight.go，已恢复）`go test -run ReviewN810SSUnicode ./internal/installer` → FAIL；修复后 `go test -p 4 -count=1 ./internal/installer` → `ok 152.307s`；`go test -race -p 4 -count=1 -run "Preflight|ReviewN810|N810|DockerHA|Corpus|Reference|Golden" ./internal/installer` → `ok 106.716s`；gofmt 无输出，vet 通过。
