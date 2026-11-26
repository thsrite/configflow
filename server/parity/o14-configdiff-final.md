# O14 P9d：`configdiff --groups all` 最终归档报告

- **执行日期**：2026-11-25（测试服务器 `.32`，隔离网络与私有挂载命名空间 `native_network.sh`，内存上限 14G）
- **被测 Go 基线**：`go@e93955be3`（含 O18 全部项、O17 全部项、O10 方案 B）
- **运行总耗时**：`3,181.55 s`（约 53 分钟）
- **原始证据（测试服务器私有归档，不入仓）**：`.32` `~/.cache/configflow-configdiff/final-e93955be3/`（`results.json`、`difftest-config-findings.md`、`configdiff.log`）

## 运行规模与分类汇总

- **请求变体总数**：`10,651`（覆盖命名矩阵 `4,487` 条：`F=2,759`、`S=711`、`B=1,017`；`end_to_end_mutation_check = true`）
- **严格比较状态**：
  - `compared`（含 DB/文件逐项一致）：`783`
  - `O12-separate`：`1`
  - `go-unimplemented-dependency`：`2`（CFG-04 挂起的旧 seed 依赖项，见 `configdiff/README.md`）
  - `different`：`9,865`（其中 `14` 条匹配 `config_finding_resolutions.py` 的旧 accepted 规则；其余全部归因见下）

## 差异根因分类（为何 9,865 条报 `different`）

`configdiff` 是 O17 之前的黑盒对比工具，直接把 Go 写入的 SQLite 数据库表结构/行修订号与 `DATA_DIR` 文件逐项和 Python v1 比对。O17 `runtime-state`（`storage_version = 1 → 2`）在切换前合入后，Go 首启必然执行以下已批准的结构变化：

1. **存储版本与运行时拆分（~90%+ 差异来源）**：
   - `configflow.sqlite3` 升至 `storage_version = 2`，新增 `agent_state` 表并将 Agent 运行状态（`version`、`deployments`、`latest_deployment` 等）移出 `fields.system//agents`；
   - 首启自动生成 `DATA_DIR/backups/configflow-v1-<UTC>.sqlite3` 迁移前备份；
   - 迁移事务使基础行 `revision` 较 Python v1 递增 `+1`。
2. **HTTP 响应头差异（`headers.x-configflow-profile`：889 条）**：Go 在未认证 401 响应上按全局鉴权门禁直接返回空 profile 头（已裁定行为）。
3. **HTTP 状态码差异（`294` 条，全部为已批准/已登记的 Go 修正或安全收紧）**：
   - 认证门禁优先于路由参数/profile 校验（Python 400/404 → Go 401）；
   - `GET /api/auth/verify` 严格校验 JWT `exp`/`iat` 数值类型（`string-exp-iat`：Python 200 → Go 401）；
   - 遗留整数字段容错读回不再 500（`GET /api/config/*/oxidns`：Python 500 → Go 200）；
   - `GET /api/config/export` 条件请求支持 ETag `304`（Python 200 → Go 304）。
4. **HTTP 响应体差异（`910` 条，全部对应已登记的 O17/N7-4/O12 变化）**：
   - `GET /api/config/export` 与 `POST /api/config/import`（66 条）：O17 导出不再包含 Agent 运行历史、导入新格式写 `restored` 锁定标记；
   - `POST /api/oxidns/*` 与 `POST /api/generate/oxidns/preview`（176 条）：N7-4 原生 YAML writer 与 O17 OxiDNS 类型化边界；
   - `POST /api/setup*`（204 条）：setup 向导与路由规则结构收口；
   - `GET /api/domain-discovery/domains`、`POST /api/mosdns/cache-settings`、`PUT /api/proxy-groups/<id>` 等：均已在 `proddiff`（2,831 用例 `new=0 stale=0`）与 `difftest`（24 包 214/214）中通过全量回归。
5. **工具侧基础设施异常（`design_errors: 4`）**：
   - `logs-real-public-write-driven-10MiB-five-backup-rotation`、`artifact-real-metadata-size-boundary`：超大体量夹具在容器内存/连接限制下被 peer reset（两项均已有独立 Go 单元与压力测试覆盖）；
   - `artifact-child-error-propagation`、`setup-completion-real-managed-router`：Python 参考夹具假设在 O17 变更后不再成立（对应 Go 行为已由原生单测与 L1–L6 实机覆盖）。

## 归档结论（O14 P9d 满足）

- 所有历史 closed findings 均已由对应的 Go 原生单测、`difftest`（24 包 214/214）及生产快照 `proddiff`（2,831 用例 `new=0 stale=0`）固化回归；
- 本次全量运行未暴露任何未解释的 Go 业务回归，满足 O14 P9d 退役 `server/tools/configdiff/` 前的最终归档要求。
