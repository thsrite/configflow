# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

## 既有差异复验状态

仅实际相比通过标为closed；accepted要求全部原始差异精确符合链接中的批准决定，仍保留严格比较证据。未实现/依赖跳过标blocked，未重跑标pending。基线表示该条最后一次复验；未重跑条目不随本轮更新。

| 用例 / 变体 | 路由 | 状态 | 修复提交 / 接受依据 | 最后复验Go基线 | Python / Go HTTP |
|---|---|---|---|---|---|
| H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/cross-origin | `GET /api/mosdns/rule-proxy` | open | — | 4d8a665a | 400 / 200 |
| H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/same-origin-outside-api | `GET /api/mosdns/rule-proxy` | open | — | 4d8a665a | 400 / 200 |
| H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/cross-origin | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 4d8a665a | 400 / 200 |
| H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/same-origin-outside-api | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | open | — | 4d8a665a | 400 / 200 |

## H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/cross-origin

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.internal-redirect-boundary.destination_requests` | length | 0 | 1 |
| `status` | value | 400 | 200 |
| `headers.content-type` | value | 'application/json' | 'text/plain; charset=utf-8' |
| `headers.cache-control` | value | '' | 'no-store' |
| `body` | type | {'message': 'Invalid remote URL', 'success': False} | [] |

## H120 / real-dot-segment-network-redirect//api/mosdns/rule-proxy/same-origin-outside-api

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests` | length | 1 | 2 |
| `rule.internal-redirect-boundary.source_paths` | length | 1 | 2 |
| `status` | value | 400 | 200 |
| `headers.content-type` | value | 'application/json' | 'text/plain; charset=utf-8' |
| `headers.cache-control` | value | '' | 'no-store' |
| `body` | type | {'message': 'Invalid remote URL', 'success': False} | [] |

## H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/cross-origin

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `rule.internal-redirect-boundary.destination_requests` | length | 0 | 1 |
| `status` | value | 400 | 200 |
| `headers.content-type` | value | 'application/json' | 'text/plain; charset=utf-8' |
| `headers.cache-control` | value | '' | 'no-store' |
| `body` | type | {'message': 'Invalid remote URL', 'success': False} | [] |

## H120 / real-dot-segment-network-redirect//api/profiles/default/mosdns/rule-proxy/same-origin-outside-api

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests` | length | 1 | 2 |
| `rule.internal-redirect-boundary.source_paths` | length | 1 | 2 |
| `status` | value | 400 | 200 |
| `headers.content-type` | value | 'application/json' | 'text/plain; charset=utf-8' |
| `headers.cache-control` | value | '' | 'no-store' |
| `body` | type | {'message': 'Invalid remote URL', 'success': False} | [] |
