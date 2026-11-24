# Go 生产快照结构对比

`proddiff` 在同一离线快照上比较 Go 基线和 Go 候选树。脚本从两棵树分别构建带
`configflow_proddiff` 标签的服务端和生成器，再从候选树构建 Go 比较器。
`PRODDIFF_MODE` 和 `PRODDIFF_COMPARATOR` 均默认并仅接受 `go`。

在仓库根目录运行。将 `CF_PRODDIFF_BASE_TREE` 设为测试服务器上基线源码的绝对路径，
该目录应包含 `server/`。

```bash
rt -e "PRODDIFF_BASE_TREE=$CF_PRODDIFF_BASE_TREE" -- server/scripts/proddiff.sh
rt -e "GATE_ONLY=proddiff" -e "PRODDIFF_BASE_TREE=$CF_PRODDIFF_BASE_TREE" -- server/scripts/gate.sh
```

`proddiff.sh` 默认使用测试服务器上的 `~/cf-prod-snapshot/20261116/data`，
也接受快照目录作为第一个参数。退出码 0 表示基线和覆盖检查通过，1 表示发现新增差异、
过期基线或覆盖回退，2 表示框架失败。

## 隔离与启动

构建完成后，脚本在独立的 user、network、mount 和 PID 命名空间运行比较器及两个服务。
命名空间只启用 loopback，屏蔽宿主 IPC 和 DNS；原快照挂载为只读，两个服务各读独立临时副本。
比较器拒绝含符号链接的快照，并在运行前后校验快照摘要。退出或收到信号时清理临时副本。

两个服务按注册顺序执行启动任务，输出各自的路由清单并标记 ready 后才接收比较请求。
`configflow_proddiff` 构建关闭周期循环，保留由请求触发的行为，包括隔离环境中的设备连接失败响应。
两边从测试环境取得相同账号，通过正常的登录 API 获取凭据。快照里的 Agent 凭据只用于命名空间内
的请求，原快照账号保持原样。

## 覆盖与比较

- 路由清单以 `server/parity/routes.tsv` 为索引，并以两端实际注册的方法和模板核实覆盖。
  只比较两边都注册的路由。候选独有路由计入覆盖报告，曾被覆盖的路由消失会导致覆盖回退。
- 测试覆盖各配置空间，以及快照中保存的 Agent、路由器、订阅、聚合、规则库、部署和预览。
  快照没有实例时，以合成的缺失参数覆盖错误分支，并将其与快照参数的覆盖分开计数。
- 执行顺序为 GET、`proddiff-readonly-post.json` 明列的 POST，最后串行比较日志路由。
  生成操作可以在临时副本内写入生成产物。独立生成器使用各自的公开入口和副本中的缓存，
  覆盖主配置、provider、规则、Agent 内容与部署归档。
- 比较状态码、媒体类型、响应头和正文。JSON、YAML 与归档中可解析的成员比较结构，
  普通文本比较行内容，报告还区分换行格式、数字类型和传输失败。
- `proddiff-normalize.json` 按精确路由和键路径声明归一规则。仅对指定的 GET 允许在差异出现时
  交替重问两端，检查相邻回复是否一致。单侧传输失败有独立的重试预算。
- 快照比较等待两边完成 provider 新鲜内容生成，避免调度速度差导致一侧返回 last-good。
  特殊构建也等待 Agent 内容状态检查完成。超时按框架错误处理。
- 每轮将 23 项自检交给同一个比较器，覆盖状态码、响应头、深层字段、
  重复键、列表顺序、格式和归一规则等边界。

## 基线与报告

- `server/parity/proddiff-baseline-go.txt` 记录已接受的差异。新增差异或基线条目消失都会令门禁失败。
- `server/parity/proddiff-coverage.txt` 记录路由、生成器及快照参数的覆盖。
- `server/parity/proddiff-generator-coverage.json` 记录各生成器的成功输出数量下限，
  避免双方同时报错掩盖覆盖回退。

人工审阅后可以显式重录基线。

```bash
rt -e "PRODDIFF_BASE_TREE=$CF_PRODDIFF_BASE_TREE" \
   --back server/parity/proddiff-baseline-go.txt \
   --back server/parity/proddiff-coverage.txt \
   --back server/parity/proddiff-generator-coverage.json \
   -- server/scripts/proddiff.sh --record-baseline
```

每轮报告写入测试服务器上的 `~/cf-prod-snapshot/runs/run-*/`。
`summary.json`、`coverage.json`、`differences.tsv` 和 `report.md` 保存公开模板、键路径、
差异类别和计数。`transport-attempts.jsonl` 记录各侧的重试和时长。
响应原文、生成产物、请求参数、服务日志和 `error.txt` 保留在私有目录内。
