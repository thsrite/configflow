# Batch-5 proddiff 分类（C34 与共享 owner）

分支 `go-fix-proddiff-c34`，从协调者指定 `batch-5@89a57e75` 建立。
只读查看测试服务器私有运行，未取回响应、日志、参数、文件内容或标识。
本文和代码/测试/baseline 仅含路由、字段路径、差异类型、类别及独立合成数据。

## 原始差异与分类

最新指定运行是 new=68、stale=0，共80个结构项；其中12项已在旧baseline。
新68项：14项 Go bug（10项 POST Range header + 4项 ZIP 元数据）、20项批准的
元数据/原生表示差异、34项 artifact owner 延期。旧12项均为 A18 延期及其日志后果。
每项只归入下列三类之一。没有使用 wildcard baseline；files.config.yaml 是生成器
代码固定的公开主文件名，不是从快照带出的私有路径。未知键仍隐藏。

- **PD-1（协调者，2026-11-18）**：独立进程生成文件时刻与路径不同造成的
  Last-Modified/ETag，框架固有差异；见 tools/proddiff/README.md。合成测试验证同文件
  ETag 稳定、内容变化失效、If-None-Match 的200/304，未改 ETag 产品算法。
  Last-Modified 按补充裁定是六条精确路由上的 HTTP-date/600秒归一规则，**不是baseline**；
  缺失、错误格式/类型、超窗、其他路由和其他 header 均有负控，不关闭 stale。
  因此最终静态 baseline 是61项（15项有意表示差异+46项deferred），原始5项时间
  差异由六条精确规则处理；同秒生成不会随机造成 new/stale。
- **D2/N7-4**：私有 Go 副本既有 baseline-format 证明迁移的摘要字段；完整管理员
  export 保留真实可恢复状态。证明只允许主配置字节标识与依赖摘要变化。
- **N7-4（D3）**：default_config 的原生 YAML 文本布局；已私下验证结构相等，
  不在报告记录模板文本。以上有意差异在 behaviour-changes 对应条目可查，PD-1
  按裁定只记 proddiff 说明，因为它不是行为变更。
- **C34 Clarified**：artifact 的固定503等待 frozen release owner；deferred → A14/A21。
- **A18 F1**：rules/update 5项、generate-config 1项、logs 6项原条目仍匹配，
  并非这次新报。JSON 500 的 Step B #3 表现未变化；logs owner 是 C06，后果随A18清除。

| 路由 | 字段路径 | 差异类型 | 类别 | owner | 理由/裁定 |
|---|---|---|---|---|---|
| `GET /api/agents/<agent_id>/oxidns/rules/update` | `body.message` | `extra-in-go` | 等待 owner 的 deferred | A18 | deferred → A18：既有 F1，HA rules owner 尚缺；保留 Step B #3 的 JSON 500，接线后必须 stale |
| `GET /api/agents/<agent_id>/oxidns/rules/update` | `body.success` | `value` | 等待 owner 的 deferred | A18 | deferred → A18：既有 F1，HA rules owner 尚缺；保留 Step B #3 的 JSON 500，接线后必须 stale |
| `GET /api/agents/<agent_id>/oxidns/rules/update` | `body.task` | `missing-in-go` | 等待 owner 的 deferred | A18 | deferred → A18：既有 F1，HA rules owner 尚缺；保留 Step B #3 的 JSON 500，接线后必须 stale |
| `GET /api/agents/<agent_id>/oxidns/rules/update` | `header.Cache-Control` | `missing-in-go` | 等待 owner 的 deferred | A18 | deferred → A18：既有 F1，HA rules owner 尚缺；保留 Step B #3 的 JSON 500，接线后必须 stale |
| `GET /api/agents/<agent_id>/oxidns/rules/update` | `status` | `value` | 等待 owner 的 deferred | A18 | deferred → A18：既有 F1，HA rules owner 尚缺；保留 Step B #3 的 JSON 500，接线后必须 stale |
| `GET /api/config/export` | `body.system.agents[0].applied_content.files.config.yaml` | `value` | 已批准的有意差异 | C34 | D2/N7-4：私有 baseline-format 证明迁移只改主配置字节标识及依赖摘要；完整管理员备份保留真实可恢复状态 |
| `GET /api/config/export` | `body.system.agents[0].applied_content.fingerprint` | `value` | 已批准的有意差异 | C34 | D2/N7-4：私有 baseline-format 证明迁移只改主配置字节标识及依赖摘要；完整管理员备份保留真实可恢复状态 |
| `GET /api/config/export` | `body.system.agents[1].applied_content.deployed_config_sha256` | `value` | 已批准的有意差异 | C34 | D2/N7-4：私有 baseline-format 证明迁移只改主配置字节标识及依赖摘要；完整管理员备份保留真实可恢复状态 |
| `GET /api/config/export` | `body.system.agents[1].applied_content.files.config.yaml` | `value` | 已批准的有意差异 | C34 | D2/N7-4：私有 baseline-format 证明迁移只改主配置字节标识及依赖摘要；完整管理员备份保留真实可恢复状态 |
| `GET /api/config/export` | `body.system.agents[1].applied_content.fingerprint` | `value` | 已批准的有意差异 | C34 | D2/N7-4：私有 baseline-format 证明迁移只改主配置字节标识及依赖摘要；完整管理员备份保留真实可恢复状态 |
| `GET /api/config/export` | `body.system.agents[2].applied_content.deployed_config_sha256` | `value` | 已批准的有意差异 | C34 | D2/N7-4：私有 baseline-format 证明迁移只改主配置字节标识及依赖摘要；完整管理员备份保留真实可恢复状态 |
| `GET /api/config/export` | `body.system.agents[2].applied_content.files.config.yaml` | `value` | 已批准的有意差异 | C34 | D2/N7-4：私有 baseline-format 证明迁移只改主配置字节标识及依赖摘要；完整管理员备份保留真实可恢复状态 |
| `GET /api/config/export` | `body.system.agents[2].applied_content.fingerprint` | `value` | 已批准的有意差异 | C34 | D2/N7-4：私有 baseline-format 证明迁移只改主配置字节标识及依赖摘要；完整管理员备份保留真实可恢复状态 |
| `GET /api/custom-config/mihomo` | `body.default_config` | `value` | 已批准的有意差异 | C34 | N7-4（D3）：默认模板由 Go 原生 YAML writer 输出；结构相等，文本布局不同 |
| `GET /api/generate/artifacts/<preview_id>/<action>` | `body.code` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `GET /api/generate/artifacts/<preview_id>/<action>` | `body.message` | `value` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `GET /api/generate/artifacts/<preview_id>/<action>` | `header.Cache-Control` | `extra-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `GET /api/generate/artifacts/<preview_id>/<action>` | `status` | `value` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `GET /api/logs/info` | `body.size` | `value` | 等待 owner 的 deferred | C06（后果归 A18） | deferred → A18：既有 F1 异常日志后果；路由 owner C06；A18 接线后必须 stale（proddiff-findings.md F1） |
| `GET /api/logs/tail` | `body.filtered_lines` | `value` | 等待 owner 的 deferred | C06（后果归 A18） | deferred → A18：既有 F1 异常日志后果；路由 owner C06；A18 接线后必须 stale（proddiff-findings.md F1） |
| `GET /api/logs/tail` | `body.logs` | `length` | 等待 owner 的 deferred | C06（后果归 A18） | deferred → A18：既有 F1 异常日志后果；路由 owner C06；A18 接线后必须 stale（proddiff-findings.md F1） |
| `GET /api/logs/tail` | `body.logs[0]` | `value` | 等待 owner 的 deferred | C06（后果归 A18） | deferred → A18：既有 F1 异常日志后果；路由 owner C06；A18 接线后必须 stale（proddiff-findings.md F1） |
| `GET /api/logs/tail` | `body.logs[1]` | `value` | 等待 owner 的 deferred | C06（后果归 A18） | deferred → A18：既有 F1 异常日志后果；路由 owner C06；A18 接线后必须 stale（proddiff-findings.md F1） |
| `GET /api/logs/tail` | `body.total_lines` | `value` | 等待 owner 的 deferred | C06（后果归 A18） | deferred → A18：既有 F1 异常日志后果；路由 owner C06；A18 接线后必须 stale（proddiff-findings.md F1） |
| `POST /api/agents/<agent_id>/generate-config` | `body.message` | `a18-ha-refusal` | 等待 owner 的 deferred | A18 | deferred → A18：HA 最终生成在 A18 接入前按设计拒绝，接入后必须消失 |
| `POST /api/generate/<service>/artifact` | `body.agent_id` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.config_path` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.content` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.deployment_id` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.expires_at` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.files` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.id` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.message` | `extra-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.profile_id` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.service_type` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.subscription_dialers` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.subscription_hosts` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.success` | `value` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `body.target_ids` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/<service>/artifact` | `status` | `value` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/generate/loon` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/generate/loon` | `header.ETag` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：独立生成文件的 mtime/路径元数据；同进程稳定性、内容变更与 304 已由合成测试约束 |
| `POST /api/generate/loon` | `header.Last-Modified` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：两个进程独立生成文件的运行时刻；精确标准 HTTP 字段；HTTP-date归一规则，非baseline |
| `POST /api/generate/mihomo` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/generate/mihomo` | `header.ETag` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：独立生成文件的 mtime/路径元数据；同进程稳定性、内容变更与 304 已由合成测试约束 |
| `POST /api/generate/mihomo` | `header.Last-Modified` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：两个进程独立生成文件的运行时刻；精确标准 HTTP 字段；HTTP-date归一规则，非baseline |
| `POST /api/generate/mosdns` | `body.<key>（ZIP 外部属性）` | `value` | Go 的 bug | C28（OxiDNS 复用） | 共享 ZIP writer 多加了 regular-file 类型位；合成 Python 对照证实并修复，保留私有权限与压缩方式 |
| `POST /api/generate/mosdns` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/generate/oxidns` | `body.<key>（ZIP 外部属性）` | `value` | Go 的 bug | C28（OxiDNS 复用） | 共享 ZIP writer 多加了 regular-file 类型位；合成 Python 对照证实并修复，保留私有权限与压缩方式 |
| `POST /api/generate/oxidns` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/generate/surge` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/generate/surge` | `header.ETag` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：独立生成文件的 mtime/路径元数据；同进程稳定性、内容变更与 304 已由合成测试约束 |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.agent_id` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.config_path` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.content` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.deployment_id` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.expires_at` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.files` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.id` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.message` | `extra-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.profile_id` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.service_type` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.subscription_dialers` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.subscription_hosts` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.success` | `value` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `body.target_ids` | `missing-in-go` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/<service>/artifact` | `status` | `value` | 等待 owner 的 deferred | A14/A21 | deferred → A14/A21：C34 Clarified 已批准 frozen artifact owner 暂缺时 503；正式接线后必须 stale |
| `POST /api/profiles/<profile_id>/generate/loon` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/profiles/<profile_id>/generate/loon` | `header.ETag` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：独立生成文件的 mtime/路径元数据；同进程稳定性、内容变更与 304 已由合成测试约束 |
| `POST /api/profiles/<profile_id>/generate/loon` | `header.Last-Modified` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：两个进程独立生成文件的运行时刻；精确标准 HTTP 字段；HTTP-date归一规则，非baseline |
| `POST /api/profiles/<profile_id>/generate/mihomo` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/profiles/<profile_id>/generate/mihomo` | `header.ETag` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：独立生成文件的 mtime/路径元数据；同进程稳定性、内容变更与 304 已由合成测试约束 |
| `POST /api/profiles/<profile_id>/generate/mihomo` | `header.Last-Modified` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：两个进程独立生成文件的运行时刻；精确标准 HTTP 字段；HTTP-date归一规则，非baseline |
| `POST /api/profiles/<profile_id>/generate/mosdns` | `body.<key>（ZIP 外部属性）` | `value` | Go 的 bug | C28（OxiDNS 复用） | 共享 ZIP writer 多加了 regular-file 类型位；合成 Python 对照证实并修复，保留私有权限与压缩方式 |
| `POST /api/profiles/<profile_id>/generate/mosdns` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/profiles/<profile_id>/generate/oxidns` | `body.<key>（ZIP 外部属性）` | `value` | Go 的 bug | C28（OxiDNS 复用） | 共享 ZIP writer 多加了 regular-file 类型位；合成 Python 对照证实并修复，保留私有权限与压缩方式 |
| `POST /api/profiles/<profile_id>/generate/oxidns` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/profiles/<profile_id>/generate/surge` | `header.Accept-Ranges` | `extra-in-go` | Go 的 bug | C05/httpx | 普通 POST 附件不应声明 Range；保留 GET/HEAD 的 200/206/304 行为，已修共享 writeFile |
| `POST /api/profiles/<profile_id>/generate/surge` | `header.ETag` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：独立生成文件的 mtime/路径元数据；同进程稳定性、内容变更与 304 已由合成测试约束 |
| `POST /api/profiles/<profile_id>/generate/surge` | `header.Last-Modified` | `value` | 已批准的有意差异 | C34 | PD-1（协调者 2026-11-18）：两个进程独立生成文件的运行时刻；精确标准 HTTP 字段；HTTP-date归一规则，非baseline |

## 产品修复与回归约束

- **low，C05/httpx**：共享附件响应仅对 GET/HEAD 声明和执行 Range。
  真实 Flask 的合成附件对照覆盖普通 GET/HEAD/POST、GET Range 和 POST Range；
  同一磁盘文件另测稳定 ETag、修改内容后失效及 If-None-Match 的200/304。
- **low，C28 共享 ZIP writer**：成员保留私有权限，移除多余的 regular-file 类型位。
  合成 Python zipfile 对照同时经过 MosDNS/OxiDNS 的公开构建入口，逐项比较
  ExternalAttrs、creator system 和压缩方式，没有登记 ZIP body 豁免。
- 比较器只公开标准 HTTP header 名和固定主文件名；其他未知键仍脱敏。
  新增 artifact 延期后，既有 A13 测试改按其 Agent 路由限定延期集合，仍要求原24项
  全部存在或全部由 owner 接线删除，未放松到任意 A14 条目。

## 定向验证

以下命令均固定 `RT_HOST=sunbroke@192.168.0.32`，没有运行完整 core gate。

```sh
rt -- bash -lc 'cd server && go test -race -count=1 -timeout=4m ./internal/httpx ./internal/mosdns ./internal/oxidns'
```

这次较宽检查中 httpx 通过（49.794s）；MosDNS 的4项 pyref 校验因临时把新增测试
放进其录制输入集合而失败，随后将公开构建入口的对照放回调用方 configgen，未改 golden；
OxiDNS 在无关的 `TestN73OxidnsReaderInputs` 超时，未据此声称全包通过。
实际改动相关的增量检查为：

```sh
rt -- bash -lc 'cd server && go vet ./internal/httpx ./internal/configgen ./internal/mosdns && go test -race -count=1 -timeout=3m ./internal/configgen ./internal/mosdns ./internal/oxidns -run "Test(ZIPExternalAttributesMatchPython|ArchiveMatchesRecordedPythonBuilds|ArchivePathsAndReferences|C29ArchivePythonBoundary)$"'
```

末行分别是 `ok configflow/internal/configgen 1.073s`、
`ok configflow/internal/mosdns 27.952s`、`ok configflow/internal/oxidns 1.309s`。

```sh
rt -- bash -lc '$HOME/cf-test/.venv/bin/python server/tools/proddiff/test_proddiff.py && GATE_ONLY=compare GATE_SLOTS=2 server/scripts/gate.sh'
```

52项合成比较器测试通过；PD-1 正控接受600秒边界，缺失、坏格式、非日期类型、
601秒超窗、其他路由和 ETag 变更均保留差异。首轮补充规则后的 compare：
`new=0, stale=0`，self-check 23项通过；全部19个 difftest 包通过；
`SYNTHETIC LIVE PASS: 23 required entries ran and passed; 0 skips`；
最终 `GATE STAGE PASS: compare`。

此前静态 Last-Modified baseline 的试跑为 `new=0, stale=3`：同秒生成导致3条
静态项不再出现。已先交协调者裁定，再改成 PD-1 的六条有界归一规则，未禁用 stale，
也未用通配符扩大其他 baseline。

第二轮复验命令：

```sh
rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/httpx/write.go internal/httpx/edge_test.go internal/httpx/file_parity_test.go internal/mosdns/archive.go internal/configgen/zip_metadata_test.go)" && go vet ./... && cd .. && GATE_ONLY=compare GATE_SLOTS=2 server/scripts/gate.sh'
```

gofmt 无输出、全仓 vet 通过；proddiff 再次 `new=0, stale=0`，self-check 23项通过，
全部19个 difftest 包再次通过。
末行为 `SYNTHETIC LIVE PASS: 23 required entries ran and passed; 0 skips` 和
`GATE STAGE PASS: compare`。两轮连续完整 compare 均退出0。

交回为本分支相对 `batch-5@89a57e75` 的单独提交；未 push。
