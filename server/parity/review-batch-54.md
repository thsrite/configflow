# batch-54 复核（O17 3.8 + 2.7：go-agent any、Go 版本、分级脱敏日志、响应结构体、部署测试等待）

日期 2026-11-24。复核者 Opus 5.5。分支 `batch-54`，已 rebase 到 go@81069ed30（无冲突；本批只改 `backend/agents/go-agent` 和
`docker/Dockerfile.agent`，没有 pyref 黄金文件变化，无需重录）。

被复核提交：c5b21871c（any）、431466f8d（go 1.27.0 / Dockerfile.agent）、abe06cc32（logx）、991263cf1（响应结构体）、
21ab81529（测试等待）。复核新增：5390fff1d（脱敏补漏）、本报告。

## 结论

中 1 个（已修），低 4 个（1 个随中项修，3 个说明不改）。面板↔Agent 协议未发现变化。

## 发现

### 中（已修，5390fff1d）：自由文本日志里注册密钥和 `private-key` 不脱敏

`logx.Redact` 的键名列表只有 `api_?key`/`private_?key`，`registration_key=…`、`{"registration_key":"…"}`、`AGENT_REGISTRATION_KEY=…`、
WireGuard/Mihomo 写法 `private-key = …`、`api-key: …` 原样输出（构造测试确认）。结构化 `Log(kv…)` 的键判断含 `key`，不受影响。
修法：以 `_key`/`-key` 结尾的名字都算凭据（`apikey`/`privatekey` 连写仍覆盖）；表驱动用例加进 `TestRedactSecrets`。

### 低（已修，同上）：`[REDACTED]]` 多一个括号

`Authorization: Bearer x` 先被 scheme 规则换成 `[REDACTED]`，再被 value 规则匹配到 `[REDACTED` 又换一次；URL 里 `token=[REDACTED]` 同理被
kv 规则二次替换。值的字符类排除 `[` 后不再重复替换，测试断言输出不含 `]]`。

### 低（不改）：原 `log.Printf` 全部映射为 INFO，`LOG_LEVEL` 是通用变量名

没有错误被降成 DEBUG（只有 `/health` 的每次请求日志从无级别改为 DEBUG，属降噪）。但原来的失败日志也都是 INFO，若设备环境里有
`LOG_LEVEL=WARNING/ERROR`（Agent 读的是通用名，不是 `CONFIGFLOW_AGENT_*`），这些失败行会被一起过滤。安装脚本和 compose 都不设该变量，
默认 INFO 不丢日志。以后若要按级别细分，建议把失败路径改成 Warn/Error，并考虑改用 Agent 专属变量名。

### 低（不改）：未覆盖的格式

裸 JWT（无 `Bearer`/`Authorization` 前缀）、`ss://<base64>@host` 这类节点 URI 的 userinfo 不脱敏；Agent 现有日志路径不打印这两类内容。
`_key` 规则也会把 `public-key`、`ssh-key=/path` 之类的值遮住（日志可读性的小代价）。

### 低（不改）：`waitDeploymentState` 超时诊断不带最后一次读取错误

60 s 上限（`t.Context()` 约束、10 ms 轮询）合理：原 3 s/8 s 在 -race 高负载下误报；真挂起时 60 s 后带最后状态失败。若 `loadDeployment`
一直出错，报错里只有 `last state: <nil>`，看不到错误本身。等待条件改成“到终态即返回”（含 failed）只会让失败更快暴露，不会掩盖挂起。

## 逐项核对（无问题）

- **协议**：逐个看了 46 个文件中所有 `map → 结构体` 的改动。条件出现的键（`error_details`、`manual_rollback`、`recovering`、capabilities 各协议、
  `config_directory`/`mihomo_resources`（指针保留 null）、网关 `pending`/`spec`/`applied_at`/`error`/`*_error`、回程 `allocation`、
  `retained_empty_table`）都用 `omitempty`/指针，与旧代码的 `if` 分支一致；旧代码总有的键（HA 的 `managed:null`/`handoff:null`/`error:""`、
  `tun_prepare`、`retention_days`）没有 `omitempty`。成员按字典序排列，与 map 编码相同。`JsonResponse`、状态码、响应头未变。
  作者的对照测试是“手写结构体 vs 手写旧 map”，证明编码相同但不证明处理器填值相同；后者靠上面的逐处阅读；difftest（214 项必跑）与 Agent 全量 -race 也通过。
- **URL 脱敏**：`routes` 和 `main` 两份 `redactURLForLog`/`SafeURLFailure` 与新 `logx.RedactURL`/`SafeURLFailure` 逐行相同，回给面板的错误文本不变。
- **日志格式**：旧格式 `2006/01/02 15:04:05 msg`，新格式与面板一致 `… - agent/routes - INFO - msg`；服务端/前端没有按旧格式解析 Agent 日志。
  控制字符换成空格防伪造行。剩余 `log` 只在 `clioutput` 里重定向（与 logx 同步切换）。
- **Go 版本**：Agent `go 1.27.0` + `toolchain go1.27.4`；`Dockerfile.agent` 与 `Dockerfile`（含面板内置 Agent 的 go-builder）都是
  `golang:1.27.4-alpine` + `GOTOOLCHAIN=local`；CI `go-version-file` 读 go.mod。四平台交叉编译在 agent 门禁里通过。
