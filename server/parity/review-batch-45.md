# batch-45 复核（O17 小清理：Agent 原子写 + 服务端重复小工具）

日期 2026-11-23。复核者 Opus 5.5。分支 `batch-45`，**已 rebase 到 go@98bf6099c**（batch-43 合入之后；原基点 d8286168b 不含 batch-43 的
`fsx` 和 9f4726b98「重复小工具合一」，两批改的是同一批文件）。

被复核提交（rebase 后）：5dce7d9b7（Agent atomicfile）、8295688ad（weburl.HasControl）、abebcf082（pathx）、
30445de6a（slices.Contains）、398401d94（digest）、1653d3dac（httpx.BindObject/DecodeObject）。
复核新增提交：075428f4c（AIO 转发提交文案）、72f6b515a（漏掉的 3 份 sha256 摘要）、185520ace（atomicfile.SyncDir 与 fsx 一致）、
5b7a91671（monitor 的 contains）、本报告。

rebase 冲突只有 `installer/{aio,gateway,templates}.go`：go 侧已把 `valueError` 改成 `invalidInput`、`splitLines` 换成 `textx`，
本批把 `hasControl` 换成 `weburl.HasControl`；取两边（`invalidInput` + `weburl.HasControl`，删掉 `hasControl`）。

## 结论

中 2 个（已修）、低 3 个（1 个已修，2 个说明不改）。服务端 4 个提交逐项等价；`BindObject/DecodeObject` 用改前/改后对照测试证明响应逐字节一致。

## 发现

### 中（已修，075428f4c）：IPv4 转发持久化的三条中文报错合成了一条

`routes/aio_forwarding.go` 的 `aioCommitForwardingConfig` 改前分阶段报错，面板原样显示：
建临时文件失败「IPv4 转发持久化目录不可写：…」、写/改权限/fsync/close 失败「无法写入 IPv4 转发持久化文件：…」、
硬链接失败时**只要已有受管文件就算成功**（任何链接错误），否则「无法提交 IPv4 转发持久化配置：…」；链接后目录 fsync 失败返回原错误。
改后所有失败都成了「无法提交…」，受管文件判断收窄到 `EEXIST`，目录 fsync 错误也被包成「无法提交」。
修法：通过 `CreateTemp`/`BeforeCommit`/`SyncDir` 钩子记录走到哪一步，按阶段恢复原文案和原判断。
表驱动测试 `routes/aio_forwarding_commit_test.go`（新建、目录只读、已有受管文件、已有外来文件、目录 fsync 失败后文件已落地；都检查无临时文件残留）。

### 中（已修，185520ace）：两处新增目录 fsync，在不支持目录 fsync 的文件系统上会变成失败

`preflight/tun_prepare.go`（`/etc/modules-load.d`）和 `routes/return_routing_manage.go`（回程路由状态）改前不 fsync 目录，现在经 atomicfile 会做，
而 Agent 的 `atomicfile.SyncDir` 不像服务端 `fsx.SyncDir` 那样把 `EINVAL`/`ENOTSUP` 当成功。Agent 数据目录可能在 Docker 绑定挂载
（9p/virtiofs/CIFS 一类）上，那里目录 fsync 可能返回 EINVAL：文件已经换好却报失败。按 batch-43 对 fsx 的裁定统一：`EINVAL`/`ENOTSUP` 视为已同步。
这对原本就 fsync 目录的几处（Agent 配置、部署、升级迁移、dockerha、dockerimage）是放宽（以前这种文件系统上会报错），与服务端一致。
表驱动测试 `TestSyncUnsupportedOK`。

### 低（已修，72f6b515a、5b7a91671）：遗漏的重复实现

- sha256 十六进制摘要：`agents/dnsha/storage.go` 的 `digest`、`agents/rulesync/snapshot.go` 的 `digest`、`agents/dnsfollow/publish.go` 的 `hash`
  没有换成 `digest.SHA256Hex`（提交说明称已统一）。已换，输出相同。
- `agents/monitor` 的 `contains([]string, string)` 是 o17 3.5 清单里最后一份可直接替换的，已换 `slices.Contains`。

### 低（不改）：Agent 侧几处英文内部错误的包装文本变了

`dockerha` 的 `writeIdentity`/`writeJSON`/`writeEvidence`/`createHostIdentity`：建临时文件和写入失败以前不带或带 `write <临时名>:` 前缀，
现在统一带 `publish <文件>:`；`routes` 的 `syncDirectory` 现在带 `open directory:`/`sync directory:` 前缀。都是英文诊断文本（init 容器 stderr、
部署失败详情里的底层错误），没有代码按文本判断，`Failure` 类型的中文提示不经过这些路径。不改。

### 低（不改）：临时文件名变化

`dockerimage` 从固定 `<文件>.tmp` 变成 `.<文件>-<随机>.tmp`，回程状态从 `<state>.tmp` 变成 `.return-state-<随机>`，
companion 证据从 `.companion-<pid>` 变成 `.companion-<随机>`。没有代码按旧名查找或清理；旧版本崩溃留下的固定名临时文件不会被清理（以前也只在下次写时覆盖）。
新名字用 `O_EXCL` 创建，不会再像旧的 `O_TRUNC` 固定名那样跟随同名符号链接，写失败也会删掉临时文件（companion 以前会留下）。

## 逐项核对（无问题）

**Agent 原子写 11 处**（对照改前代码）：

| 位置 | 临时文件 | 权限/属主 | 发布 | 目录 fsync | 结论 |
|---|---|---|---|---|---|
| `config.go` Save | `.agent-config-<随机>`，同目录 | Chmod 0600 | rename | 有（同前） | 同 |
| `upgrade.AtomicWrite` | `.configflow-write-<随机>` | Chmod mode | rename | 有 | 同 |
| `routes.durableWrite` | `.write-<随机>`（`deployment.go` 的残留扫描按 `.write-` 前缀，仍匹配） | Chmod mode | rename | `syncDirectory` 变量（测试计数保留） | 同 |
| `routes.copyFileOwned` | `.configflow-copy-<随机>` | 先比属主再 Chown，失败即返回；Chmod mode | rename | 跳过（同前，调用方批量 fsync） | 同 |
| `routes.aioCommitForwardingConfig` | `.configflow-aio-forward.<随机>`，在 `aioSysctlDir`（= 目标所在目录） | 0644 | Link（独占） | 有 | 见中 1，已修 |
| `routes.saveReturnState` | 原固定 `.tmp` + `os.WriteFile`（不 fsync、受 umask） | 现精确 0600 | rename | 新增 | 更耐久；见中 2 |
| `preflight.persistTUNFile` | `.configflow-tun-<随机>` | mode 取首次 `check()` 读到的原文件权限（参数求值在首次 check 之后，正确） | 已存在 rename，否则 Link；`check` 作 `BeforeCommit` 在发布前重跑 | 新增 | 同；见中 2 |
| `dockerha.writeIdentity` | 自带 `createTemp`（`O_EXCL|O_NOFOLLOW`，属主前缀） | 0600 | Link | 有 | 同 |
| `dockerha.writeJSON` | 同上 | 0600 | `replace` 时 rename 否则 Link | 有 | 同 |
| `dockerha.createHostIdentity` | 同上 | 0600 | Link；`EEXIST`（别的项目已初始化）仍只 fsync 目录后成功 | 有 | 同 |
| `dockerha.writeEvidence` | `.companion-<随机>`（原 pid 固定名） | 0600 | rename | 有 | 同（命名见低） |
| `dockerimage.replaceFile` | `.<文件>-<随机>.tmp`（原 `<文件>.tmp`） | 0600 | rename | 有 | 同（命名见低） |

另外 `upgrade/recovery_cleanup.go` 两处、`upgrade/worker.go` 一处只是把内联目录 fsync 换成 `atomicfile.SyncDir`。

- 失败清理：建临时文件之后任何失败（Chown/Chmod/写/fsync/close/`BeforeCommit`/rename/link）都删临时文件；Link 成功后临时名也删掉，目标保留。
- 符号链接目标：rename 替换链接本身、Link 遇到已存在的链接返回 EEXIST，与改前相同。跨文件系统：临时文件始终在目标目录，不会 EXDEV。
- 目标已存在且权限不同：rename 后是新文件的权限（各处显式 Chmod），与改前相同；TUN 文件沿用原权限。
- 取消：Agent 侧这些函数改前改后都不带 ctx，没有取消语义变化。

**服务端**：

- `weburl.HasControl`：与 installer 原 `hasControl` 字符范围相同（`< 32` 或 `127`）。`oxidns.hasControl`（只 `< 32`）和
  `agents/monitor.hasControl`（含 C1 `127–159`）范围不同，正确地没有合并。
- `pathx`：原来三份吞错误的 `pathExists`（agents 下载、repo、repo 测试）换 `Accessible`，原来不吞错误的日志页那份换 `Exists`，
  两者实现与各自原函数逐行相同，**所有调用方行为不变**（日志页在权限错误时仍返回错误响应，下载和 repo 仍当不存在）。
  `agents/upgrades.pathExists` 把 `ENOTDIR`/`ELOOP`/`EINVAL` 也当不存在且检查 ctx，语义不同，保留。
- `slices.Contains`：类型都是 `[]string`，等价。
- `digest.SHA256Hex`：与原 `sha256.Sum256` + `hex.EncodeToString` 相同（小写十六进制）。
- `httpx.BindObject/DecodeObject`：`rulelibrary` 原来 `Unmarshal` 失败返回 `(部分结果, true, err)`，现在 `(零值, false, err)`；
  所有调用方先判断 `err`，所以没有差别。`aggregations`/`subscriptions` 原来直接调 `out.UnmarshalJSON`，现在经 `json.Unmarshal`
  走同一个（嵌入的 `model.Fields`）方法；原始字节已由 `Bind` 校验过。
  **对照测试**：把三处改前的 `readBody` 原样放进临时测试，与改后的 `readBody` 挂在两个路由上，对 28 种请求体 × 3 种 Content-Type
  （空体、空白、`null`、数组、标量、`{}`、重复成员、字段类型错误、截断 JSON、NaN/Infinity、多个值、BOM、`1e400`、孤立代理项转义、
  非 UTF-8 键、10001 层嵌套、4 MiB 字段、`text/plain`、无 Content-Type）比较状态码和响应体：三个包各 84 个请求逐字节一致。
  临时测试没有提交（含改前实现的副本）。
- `api/agents/installer.go` 删掉只转发的 `readBody`，直接用 `readBoundJSON`，等价。
- 提交说明里「api/rules、api/proxygroups」实际没有改动（它们已不是 `readBody` 形状），只是说明不准确。

## o17 前-3 清单剩余（与 batch-43 合并后）

- `ptr`：只剩 `deploy/pushdiff/types.go`（参数名叫 `new`，batch-43 有意保留）。
- `asciiDigits`、`isoformat`、`raw(v)`、版本比较、`utilx`：batch-43 已处理，无剩余。
- `hasControl`：剩 `oxidns`、`agents/monitor` 两份，范围不同，不合并。
- `contains`：`oxidns/node.go` 的是 YAML 节点版，不是同一个函数。
- 内联 `sha256.Sum256` 约 30 处（`routers`、`oxidns`、`subs`、`setup` 等）多数截断或取原始字节，不是 `digest` 函数的副本，不在本清单内。

## 验证（.64，`RT_HOST=sunbroke@192.168.0.64`，`GOTMPDIR=~/.cache/rt-tmp/b45`）

在 5b7a91671 上执行（本报告提交只加文档）。

- 服务端受影响 65 个包 `go test -race -count=1 -p 6 -timeout 40m`（httpx、pathx、digest、weburl、installer、api/...、repo、deploy/...、
  gateway/...、rules、testgolden、discovery、agents/...）：全部 ok；`agents/monitor` 改动后单独 `-race`：`ok configflow/internal/agents/monitor 525.957s`。
  `gofmt -l ./cmd ./internal` 无输出，`go vet ./internal/...` 通过。
- BindObject 对照测试（临时）：rulelibrary / aggregations / subscriptions 各 `compared 84 requests`，PASS。
- Agent 模块 `go test -race` atomicfile/routes/dockerha/dockerimage/preflight/upgrade/根包：全部 ok；`TestAIOCommitForwardingConfigStages` 5 个子用例 PASS。
- `GATE_ONLY=agent scripts/gate.sh -p 6`：`gate: agent module PASS (gofmt, vet, vet -tags halab nftlab, race)` / `GATE STAGE PASS: agent`
- `GATE_ONLY=difftest scripts/gate.sh -p 6`：`DIFF PASS: 214 required entries ran and passed; 0 test skips` / `GATE STAGE PASS: difftest`
- `GATE_ONLY=proddiff scripts/gate.sh -p 6`：self-check 全 PASS，`baseline: new=0 stale=0` / `GATE STAGE PASS: proddiff`
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-45 -p 6`：`PREFLIGHT PASS`
