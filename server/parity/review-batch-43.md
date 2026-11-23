# batch-43 复核（O17 切换前清理第一部分）

日期 2026-11-23。复核者 Opus 5.5。分支 `batch-43`（rebase 到 go@4c99e4495，即 batch-42/T1d 合入之后）。
被复核提交（rebase 后）：ab5b55c11（删 convert/ipaddr.go）、0657c7db1（Compose 漏判 err）、a64179a35（异常名）、
83303ae3d（重复小工具）、59449a566（fsx 原子写）、f66b57f62（HA 向导只收字符串）、b213460d2（「无 HTTP 响应」）、
76b308958（textx.SplitLines）、fbde43154（O17 进展）。
复核新增提交：3a74a2981（诊断文本 YAML 块密钥脱敏）、4a063e56d（Agent 版本长数字段）、本报告。

## 结论

中 1 个（已修）、低 1 个（已修）、其余为说明性低级事项，不处理。没有未列入 behaviour-changes 的功能差异。

## 发现

### 中（已修，3a74a2981）：断行改动让诊断文本的 YAML 块密钥脱敏变窄

`diag.hideBlockSecrets` 用 `^` 锚定的 `blockSecret` 识别 `password: |` 一类块头，然后吞掉缩进的子行。
改用 `textx.SplitLines` 后，U+0085/U+2028/U+2029 留在行内（`DiagnosticText` 先删掉了其它控制字符，`\v`/`\f`/`\x1c–\x1e` 到不了这里），
`"log: x password: |\n  s3cr3t\n"` 这一行不再以键开头，块头不匹配，子行 `s3cr3t` 原样留在诊断文本里。
Python 按 `str.splitlines` 会在 U+2028 处断开并隐藏。作者「行只会变长、范围不会变窄」的论证对 `redact.redactText`
（字面替换 + 整行判断；分隔符不是 `%`/十六进制字符，百分号解码不会跨过它，所以对子串成立的判断对整行也成立）是对的，对锚定行首的块头识别不成立。

修法：先用整行匹配，不匹配再依次从每个分隔符之后开始匹配，取第一个匹配；输出保留分隔符前的内容。
只看最后一个分隔符之后是不够的：录制的 Python 用例 `"token: | # comment\u0085 x\nnext"` 整行能匹配（注释吃掉了 U+0085），只看尾段反而漏掉
（第一次修时被 `TestReferenceFailureText` 抓到）。子行吞并照旧按编辑器行，行更长只会多吞。
表驱动测试 `internal/agents/diag/diag_test.go`。

### 低（已修，4a063e56d）：Agent 版本超过 64 位的数字段按文本比较

`version.CompareAgent` 把超过 uint64 的纯数字段当文本，`"1"+25个0+".0"` 与 `"2.0"` 比较得 -1。更新判断是
`CompareAgent(current, latest) < 0`，这种 current 会被判「有更新」；功能门槛 `CompareAgent(v, minimum) >= 0` 也可能误判不支持。
真实版本号不受影响，但这一类可以不靠 math/big 直接消掉：ASCII 数字段（任意长度）去前导零后先比长度再比字节。
符号、下划线、空白仍按文本（作者的裁定不变）；`ParseRelease`（Sub-Store 版本）保留 uint64，溢出即无效，`NewerRelease` 因此不会误报更新。
behaviour-changes 对应条目已改；`TestNativeVersionBusinessRules` 里 4301 位的用例改为期望 1。fuzz `FuzzCompareAgent` 60 s 通过。

### 低（不处理）：目录 fsync 失败时报错，但文件已经换好

`fsx.Commit` 在 rename 成功后 `SyncDir` 失败会返回错误；`jsonstore` 补的 `SyncDir` 同样。调用方（例如通知的事务、`jsonstore` 的签名校验
和 `afterReplace`）会把这次写当失败处理，而磁盘上已经是新内容。以前 `monitor`、`repo` 已是这种语义；目录 fsync 只在 EIO 一类磁盘故障时失败，
那时报错比静默更合适。记在这里，不改。

### 说明（无问题）

- **fsx 顺序与清理**：同目录临时文件（`O_EXCL`，随机名，模式无 `*` 时随机串接在末尾，与 `os.CreateTemp` 相同）→ 写 → fsync → close →
  （Commit）检查 ctx → rename → fsync 目录。写/fsync/close/chmod 失败和 ctx 取消、rename 失败都删除临时文件。EINVAL/ENOTSUP 的目录 fsync 视为成功。
- **18 处逐个对照**（权限位/模式/取消错误）：ipv6 readiness、metrics history、monitor state、deploy、gateway installation、notifications、
  routers 回执、rulefetch 备份、setup、Sub-Store 状态、taskstatus、repo writeTemp 原先都是 `os.CreateTemp`（0600），现在 fsx 默认 0600；
  installer capability 原 `os.WriteFile(…, 0600)`，autoupdate 清单原 `os.WriteFile(…, 0644)`，现在 `OpenFile` 同权限（同样受 umask）；
  weblistener 原 `CreateTemp`+`Chmod(mode)`，现在 `ExactPerm`。repo 的三处 `fsyncDir` → `fsx.SyncDir`。都不改属主。
  临时文件名只有两处变了：capability 从固定 `<id>.tmp` 变成 `<id>.<随机>.tmp`，autoupdate 清单从 `manifest.json.tmp` 变成
  `.manifest.json.<随机>.tmp`（autoupdate 的残留清理按 `.tmp` 后缀，仍覆盖）。没有代码按旧名找这些文件。
- **并发**：fsx 不加锁，随机临时名使并发写不会互相截断临时文件；同一目标的串行化仍在各 store 自己的锁里（与改前相同）。
  `jsonstore` 的固定临时名 `<id>.json.tmp` 是改前就有的，靠调用方（repo）串行化。
- **jsonstore**：保留自己的 rename（签名校验要读仍打开的句柄），补 `handle.Sync()` 在 `afterTempWrite` 之前、rename 之后 `SyncDir`，顺序正确。
- **断行的其它用处**：规则内容的行号、OxiDNS 多行列表、安装脚本渲染（模板只含 `\n`）、`redact.redactText`。规则里用 `\v`/`\f`/U+2028 等
  分隔的两条规则现在算一行；mihomo 等客户端按 `\n` 读规则文件，本来就会当一行，编辑器也显示为一行，所以这是更忠实而不是失效。已存储的旧值
  「未改动时照旧可读」的兼容路径不受影响；proddiff 覆盖生产数据（见下）。
- **installer IPv4 只收字符串**：新增 400「虚拟 IPv4 地址必须是文本」「高可用网关地址必须是文本」，常量在 `messages.go`。前端核对：
  `DockerHAInstallGuide.vue` 的 host 来自 `v-model` 输入框，初值 `ha_gateway: ''`、`container_name: ''`；`inspect` 只在迁移模式带
  `container_name` 字符串；`OxiDNSHAPairing.vue` 的 `verify` 只在 `vip` 非空时发保存的字符串 VIP。MCP 没有调用这个接口。
- **jsonresp.Value 日志**：只记 `%T` 和 `json.Marshal` 的错误。标准库错误不含值（`SyntaxError` 至多带一个出错字符，`UnsupportedValueError` 只对浮点 NaN/Inf）；
  检查了 226 个自定义 `MarshalJSON`，返回的错误都是固定文案，不拼接值。不会把凭据写进日志。
- **metrics.naiveNow**：不是 bug。写入（`sample.go`）与比较（窗口起点、`today`/`week` 起点）都用本地墙钟，存成不带时区的文本，Python 同样用
  `datetime.now()`。运行镜像装了 tzdata 且 `TZ=Asia/Shanghai`，Go 与 Python 读到同一本地时间；`time.UTC` 只是装墙钟的容器，比较两边一致。
  只有夏令时地区在回拨那一小时会有歧义，Python 相同。不改。
- **rebase 冲突**（与 T1d）：`health/check/probe.go`、`region.go`、`health/dns.go`、`health/latency.go` 取 T1d 的结构（T1d 删掉的检查不恢复），
  把其中的 `valueError` 改为 `invalidInput`；两个测试里 T1d 新增的 `check.ValueError` 改为 `check.ValidationError`；`substore/dependencies.go`
  的 import 两边合并；behaviour-changes 两节都保留。检查了 T1d 合入代码里没有新的 `ptr`/`raw`/`asciiDigits`/`isoformat`/`splitlines` 副本或 Python 异常类型。

## 验证（.64，固定 `RT_HOST=sunbroke@192.168.0.64`，`GOTMPDIR=~/.cache/rt-tmp/b43`）

在 rebase 后的 4a063e56d 上执行（本报告提交只加文档）。

- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-43 -p 6`（两种 tag 的 vet + 全量非 race）：`PREFLIGHT PASS`
- `GATE_ONLY=difftest scripts/gate.sh -p 6`：`DIFF PASS: 214 required entries ran and passed; 0 test skips` / `GATE STAGE PASS: difftest`
- `GATE_ONLY=proddiff scripts/gate.sh -p 6`：self-check 全 PASS，`baseline: new=0 stale=0` / `GATE STAGE PASS: proddiff`
- 本批改到的 49 个包 `go test -race -p 6`：47 个 ok（`internal/utilx` 已删除，setup failed 属预期）；`convert`、`oxidns` 在负载 10+ 时
  超过默认 10 分钟，`-timeout 40m` 单独重跑：`ok configflow/internal/convert 602.342s`、`ok configflow/internal/oxidns 664.713s`。
  `gofmt -l ./cmd ./internal` 无输出。
- rebase 前（.32）：`go test -race ./internal/agents/ ./internal/agents/diag/ ./internal/redact/ ./internal/version/` ok；
  `FuzzCompareAgent` 60 s，220 万次执行 PASS。
