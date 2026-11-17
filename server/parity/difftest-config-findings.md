# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

## 既有差异复验状态

仅实际相比通过标为closed；未实现/依赖跳过标blocked，未重跑标pending。基线表示该条最后一次复验；未重跑条目不随本轮更新。

| 用例 / 变体 | 路由 | 状态 | 最后复验Go基线 | Python / Go HTTP |
|---|---|---|---|---|
| G13 / expired/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / expired/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / expired/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / expired/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / fresh/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / fresh/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / fresh/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / fresh/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / interval-zero/all/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / interval-zero/all/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / interval-zero/filtered/mihomo/sub-a | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / interval-zero/filtered/mihomo/sub-b | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| G13 / same-name-different-definition/merged | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| G13 / same-name-identical/merged | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R016-B02 /  | `POST /api/backup/now` | pending | 尚未复验 | — / — |
| R016-B03 /  | `POST /api/backup/now` | pending | 尚未复验 | — / — |
| R016-B90 /  | `POST /api/backup/now` | pending | 尚未复验 | — / — |
| R016-F15 /  | `POST /api/backup/now` | pending | 尚未复验 | — / — |
| R016-F16 /  | `POST /api/backup/now` | pending | 尚未复验 | — / — |
| R016-S01 /  | `POST /api/backup/now` | pending | 尚未复验 | — / — |
| R016-S02 /  | `POST /api/backup/now` | pending | 尚未复验 | — / — |
| R016-S03 /  | `POST /api/backup/now` | pending | 尚未复验 | — / — |
| R017-F18 /  | `POST /api/backup/test` | pending | 尚未复验 | — / — |
| R017-F19 /  | `POST /api/backup/test` | pending | 尚未复验 | — / — |
| R055-B01 / disabled/domain/example.com | `POST /api/domain-discovery/region/route` | pending | 尚未复验 | — / — |
| R055-S02 / new/domain/example.com | `POST /api/domain-discovery/region/route` | pending | 尚未复验 | — / — |
| R056-B01 / name-collision/0 | `POST /api/domain-discovery/rulesets/init` | pending | 尚未复验 | — / — |
| R056-B02 / before-MATCH/0 | `POST /api/domain-discovery/rulesets/init` | pending | 尚未复验 | — / — |
| R056-S01 / direct/0 | `POST /api/domain-discovery/rulesets/init` | pending | 尚未复验 | — / — |
| R056-S01 / duplicate-targets/0 | `POST /api/domain-discovery/rulesets/init` | pending | 尚未复验 | — / — |
| R056-S02 / proxy/0 | `POST /api/domain-discovery/rulesets/init` | pending | 尚未复验 | — / — |
| R056-S03 / both/0 | `POST /api/domain-discovery/rulesets/init` | pending | 尚未复验 | — / — |
| R056-S03 / empty-means-both/0 | `POST /api/domain-discovery/rulesets/init` | pending | 尚未复验 | — / — |
| R056-S04 / repeat/0 | `POST /api/domain-discovery/rulesets/init` | pending | 尚未复验 | — / — |
| R056-S04 / repeat/1 | `POST /api/domain-discovery/rulesets/init` | pending | 尚未复验 | — / — |
| R058-B03 / stored-legacy-extension | `PUT /api/domain-discovery/settings` | pending | 尚未复验 | — / — |
| R087-B04 / text/B04 | `GET /api/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R087-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R087-F13 / text/F13 | `GET /api/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R087-F16 / receipt-path-is-directory | `GET /api/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R087-S90 / declared-no-cache-download-failure | `GET /api/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R087-S91 /  | `GET /api/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R138-B04 / text/B04 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R138-B91 / absolute-query-dispatches-internal-content-endpoint | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R138-F13 / text/F13 | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R138-F16 / receipt-path-is-directory | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R138-S90 / declared-no-cache-download-failure | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R138-S91 /  | `GET /api/profiles/<profile_id>/mosdns/rule-proxy` | pending | 尚未复验 | — / — |
| R154-B03 / ?format=unknown | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R154-B04 / URL-over-query-header | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R154-B90 / Asia/Shanghai | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R154-B90 / UTC | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R154-B91 / HEAD/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R154-S01 / GET/first-download | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R154-S01 / default | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R154-S06 /  | `GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R217-S02 /  | `PUT /api/subscriptions/<sub_id>` | pending | 尚未复验 | — / — |
| R218-F09 /  | `POST /api/subscriptions/<sub_id>/fetch` | pending | 尚未复验 | — / — |
| R218-F12 /  | `POST /api/subscriptions/<sub_id>/fetch` | pending | 尚未复验 | — / — |
| R218-F13 /  | `POST /api/subscriptions/<sub_id>/fetch` | pending | 尚未复验 | — / — |
| R218-S03 /  | `POST /api/subscriptions/<sub_id>/fetch` | pending | 尚未复验 | — / — |
| R220-B03 / ?format=unknown | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R220-B90 / Asia/Shanghai | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R220-B90 / UTC | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R220-B91 / HEAD/first-download | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R220-S01 / GET/first-download | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R220-S01 / default | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R220-S06 /  | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| R222-B02 / none | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R222-B90 / Asia/Shanghai | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R222-B90 / UTC | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R222-F11 / invalid-source-no-fallback | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R222-S01 / different-definitions-no-chain | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R222-S01 / identical-crossfeed | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R222-S01 / two | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R222-S02 / disabled | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R222-S03 / http403-cache-fallback | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| R222-S04 / none | `GET /api/subscriptions/proxies` | pending | 尚未复验 | — / — |
| T06 / 12-second-last-good-and-background-finish | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| T06 / 20-clients-one-flight | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| T06 / next-poll-is-fresh | `GET /api/subscriptions/<sub_id>/proxies` | pending | 尚未复验 | — / — |
| T27 / real-tick/due-and-backoff | `startup` | pending | 尚未复验 | — / — |
| X04 / new_domain/literal/"\ud800" | `POST /api/server-domain` | pending | 尚未复验 | — / — |
| X04 / token/literal/"\ud800" | `POST /api/config-token` | pending | 尚未复验 | — / — |
| X08 / retired-node-test-dns-migration | `startup` | pending | 尚未复验 | — / — |

## R087-B04 / text/B04

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': '远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 "b\'\\\\xff\'"：内容不是有效 UTF-8 文本；请检查订阅链接、编码或是否误选了 MRS 格式。', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T02:51:00Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:51:00.554181+00:00', 'fir | None |

## R087-B91 / absolute-query-dispatches-internal-content-endpoint

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 200 | 502 |
| `headers.content-type` | value | 'text/plain; charset=utf-8' | 'application/json' |
| `body` | type | ['full:inline.example'] | {'code': 'RULE_SOURCE_UNAVAILABLE', 'message': '规则源暂时不可用，未返回旧缓存；请恢复规则源后重试。', 'success': False} |

## R087-F13 / text/F13

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': "远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 'DOMAIN,example.com'：Domain 不接受 Classical 规则行；精确域名请填写 example.com，域名及其子域名请填写 +.example.com。", 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T02:50:59Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '202 | None |

## R087-F16 / receipt-path-is-directory

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | value | 'Failed to process rule proxy request' | 'Invalid remote URL' |

## R087-S90 / declared-no-cache-download-failure

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-no-cache': {'source': 'task', 'title': '手写规则🧪 · 规则集更新', 'message': '上游返回 HTTP 503', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-no-cache', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T02:50:23Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-no-cache', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:50:23.605298+00:00', 'first_seen_at': '2026-11-17T02:50:23.605298+00:00', 'last_seen_at': '2026-11-17T0 | None |

## R087-S91 /

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests[0].host` | value | '' | 'raw.githubusercontent.com:443' |
| `subscription.source.requests[0].request_line` | value | 'CONNECT raw.githubusercontent.com:443 HTTP/1.0' | 'CONNECT raw.githubusercontent.com:443 HTTP/1.1' |
| `subscription.source.requests[0].user_agent` | value | '' | 'Go-http-client/1.1' |

## R138-B04 / text/B04

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': '远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 "b\'\\\\xff\'"：内容不是有效 UTF-8 文本；请检查订阅链接、编码或是否误选了 MRS 格式。', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T02:51:08Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:51:08.171316+00:00', 'fir | None |

## R138-B91 / absolute-query-dispatches-internal-content-endpoint

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 200 | 502 |
| `headers.content-type` | value | 'text/plain; charset=utf-8' | 'application/json' |
| `body` | type | ['full:inline.example'] | {'code': 'RULE_SOURCE_UNAVAILABLE', 'message': '规则源暂时不可用，未返回旧缓存；请恢复规则源后重试。', 'success': False} |

## R138-F13 / text/F13

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': "远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 'DOMAIN,example.com'：Domain 不接受 Classical 规则行；精确域名请填写 example.com，域名及其子域名请填写 +.example.com。", 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T02:51:06Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '202 | None |

## R138-F16 / receipt-path-is-directory

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 400 |
| `body.message` | value | 'Failed to process rule proxy request' | 'Invalid remote URL' |

## R138-S90 / declared-no-cache-download-failure

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-no-cache': {'source': 'task', 'title': '手写规则🧪 · 规则集更新', 'message': '上游返回 HTTP 503', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-no-cache', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T02:50:32Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-no-cache', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:50:33.674214+00:00', 'first_seen_at': '2026-11-17T02:50:33.674214+00:00', 'last_seen_at': '2026-11-17T0 | None |

## R138-S91 /

路由：`GET /api/profiles/<profile_id>/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests[0].host` | value | '' | 'raw.githubusercontent.com:443' |
| `subscription.source.requests[0].request_line` | value | 'CONNECT raw.githubusercontent.com:443 HTTP/1.0' | 'CONNECT raw.githubusercontent.com:443 HTTP/1.1' |
| `subscription.source.requests[0].user_agent` | value | '' | 'Go-http-client/1.1' |

## R154-B03 / ?format=unknown

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |

## R154-B04 / URL-over-query-header

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-B90 / Asia/Shanghai

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-B90 / UTC

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-B91 / HEAD/first-download

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from Sub-Store (cache updated)'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-F01 / open/provider-D1

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-F02 / open/provider-D2

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-F03 / open/provider-D3

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-F06 / open/provider-D6

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-F07 / open/provider-D7

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-F08 / open/named-provider-authorization-guard

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-S01 / GET/first-download

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from Sub-Store (cache updated)'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-S01 / default

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-S01 / login/authorized-provider

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-S01 / open/authorized-provider

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-S01 / token/authorized-provider

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R154-S06 /

路由：`GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / expired/all/mihomo/sub-a

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 3', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / expired/all/mihomo/sub-b

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源乙🧪', '# ID: sub-b', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 1', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / expired/filtered/mihomo/sub-a

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / expired/filtered/mihomo/sub-b

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源乙🧪', '# ID: sub-b', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 1', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / fresh/all/mihomo/sub-a

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 3', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / fresh/all/mihomo/sub-b

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源乙🧪', '# ID: sub-b', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 1', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / fresh/filtered/mihomo/sub-a

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / fresh/filtered/mihomo/sub-b

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源乙🧪', '# ID: sub-b', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 1', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / interval-zero/all/mihomo/sub-a

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 3', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / interval-zero/all/mihomo/sub-b

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源乙🧪', '# ID: sub-b', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 1', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / interval-zero/filtered/mihomo/sub-a

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / interval-zero/filtered/mihomo/sub-b

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 来源乙🧪', '# ID: sub-b', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 1', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-B03 / ?format=unknown

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |

## R220-B90 / Asia/Shanghai

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-B90 / UTC

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-B91 / HEAD/first-download

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from Sub-Store (cache updated)'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-F01 / open/provider-D1

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-F02 / open/provider-D2

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-F03 / open/provider-D3

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-F06 / open/provider-D6

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-F07 / open/provider-D7

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-F08 / open/named-provider-authorization-guard

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-S01 / GET/first-download

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from Sub-Store (cache updated)'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-S01 / default

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-S01 / login/authorized-provider

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-S01 / open/authorized-provider

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-S01 / token/authorized-provider

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## R220-S06 /

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## T06 / 12-second-last-good-and-background-finish

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.fallback.served_comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from Sub-Store (cache updated)'] | {'invalid': 'expected exactly one Generated comment', 'observed': []} |

## T06 / 20-clients-one-flight

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from Sub-Store (cache updated)'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## T06 / next-poll-is-fresh

路由：`GET /api/subscriptions/<sub_id>/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `provider.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `provider.comments` | length | 6 | 0 |
| `provider.served-file.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from local cache'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |
| `provider.served-file.bak.comments` | type | ['# ConfigFlow Subscription Proxies', '# Subscription: 订阅甲🧪', '# ID: sub-a', '# Generated: 0000-00-00T00:00:00.000000Z', '# Proxies: 2', '# Source: from Sub-Store (cache updated)'] | {'invalid': 'expected exactly one Generated comment', 'comments': []} |

## G13 / same-name-different-definition/merged

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 8 | 0 |

## G13 / same-name-identical/merged

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 8 | 0 |

## R222-B02 / none

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 6 | 0 |

## R222-B90 / Asia/Shanghai

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 7 | 0 |

## R222-B90 / UTC

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 7 | 0 |

## R222-F11 / invalid-source-no-fallback

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | '订阅「订阅甲🧪」准备失败：Provider 代理必须是含非空 name 的对象\n字段：proxies[0]\n内容来源：本次订阅返回的内容\n位置：第 1 行，第 11 列\n出错内容：proxies: [bad]' | 'Provider 代理必须是含非空 name 的对象\n字段：proxies[0]' |

## R222-S01 / different-definitions-no-chain

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 8 | 0 |

## R222-S01 / identical-crossfeed

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 8 | 0 |

## R222-S01 / two

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 8 | 0 |

## R222-S02 / disabled

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 7 | 0 |

## R222-S03 / http403-cache-fallback

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 7 | 0 |

## R222-S04 / none

路由：`GET /api/subscriptions/proxies`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `merged.metadata` | invalid-Go-metadata | '<valid>' | {'error': 'expected exactly one Generated comment', 'comments': []} |
| `merged.comments` | length | 6 | 0 |

## R016-B02 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-B03 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[10].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-B90 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[10].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-F15 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | '备份失败：NotEnoughSpace' | '备份失败：HTTP 507' |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:webdav_backup': {'source': 'task', 'title': 'WebDAV 手动备份', 'message': 'NotEnoughSpace', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'system', 'profile_name': None, 'target': {'path': '/system-settings', 'query': {'section': 'backup'}, 'label': '检查 WebDAV 设置'}, 'checked_at': '2026-11-17T02:49:36Z', 'failure_count': 1, 'id': 'task:webdav_backup', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:49:36.171321+00:00', 'first_seen_at': '2026-11-17T02:49:36.171321+00:00', 'last_seen_at': '2026-11-17T02:49:36Z', 'occurrences': 1, 'evidence': '1:2026-11-1 | None |
| `files.task-status.json.content.webdav_backup.last_error` | value | 'NotEnoughSpace' | 'HTTP 507' |

## R016-F16 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-S01 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-S02 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R016-S03 /

路由：`POST /api/backup/now`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.requests[4].body` | value | '' | '<?xml version="1.0"?><propfind xmlns="DAV:"><allprop/></propfind>' |

## R017-F18 /

路由：`POST /api/backup/test`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | '无法创建目录 /one/：ResponseErrorCode HTTP 403' | '无法创建目录 /one/：HTTP 403' |

## R017-F19 /

路由：`POST /api/backup/test`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `body.message` | value | '连接测试失败：NoConnection' | '连接测试失败：network request failed' |

## X04 / token/literal/"\ud800"

路由：`POST /api/config-token`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 200 |
| `body.config_token` | extra-in-go | None | '�' |
| `body.message` | missing-in-go | "'utf-8' codec can't encode character '\\ud800' in position 238: surrogates not allowed" | None |
| `body.success` | value | False | True |
| `db.fields.system//_revision.revision` | value | 4 | 5 |
| `db.fields.system//_revision.value` | value | 2 | 3 |
| `db.fields.system//system_config.revision` | value | 4 | 5 |
| `db.fields.system//system_config.value.config_token` | value | '' | '�' |
| `db.meta.generation` | value | 4 | 5 |

## R055-B01 / disabled/domain/example.com

路由：`POST /api/domain-discovery/region/route`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_e3dc304c-dbae-4bd7-98d0-47383c7adfdc' | 'ruleset_02177db8bf7ac495304d49cd1f26a331' |

## R055-S02 / new/domain/example.com

路由：`POST /api/domain-discovery/region/route`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.added[0].ruleset_id` | value | 'lib_14355839928c475da8c431fe218aa35a' | 'lib_2061a802324393f1f460fd6ab5aa4216' |
| `db.fields.profile/default/domain_discovery.value.history[0].ruleset_id` | value | 'lib_14355839928c475da8c431fe218aa35a' | 'lib_2061a802324393f1f460fd6ab5aa4216' |
| `db.fields.profile/default/domain_discovery.value.regional_rulesets.代理🧪` | value | 'lib_14355839928c475da8c431fe218aa35a' | 'lib_2061a802324393f1f460fd6ab5aa4216' |
| `db.fields.profile/default/rule_configs.value[1].id` | value | 'ruleset_9631c36f-0c5d-4070-bf96-6d687990d57d' | 'ruleset_f28e0c6131921b99e54d4c7fe6a7a8cf' |
| `db.fields.profile/default/rule_configs.value[1].library_rule_id` | value | 'lib_14355839928c475da8c431fe218aa35a' | 'lib_2061a802324393f1f460fd6ab5aa4216' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_14355839928c475da8c431fe218aa35a' | 'lib_2061a802324393f1f460fd6ab5aa4216' |
| `files.shared/rules/by-id/434b78ce44f8eaee71ae7b525741498a2722b49eef17c997516db1063414d718.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 26, 'bytes_sha256': '145f7fd7beed2e748e7cfadb10fa168d309267d703f0ec09358ede2e8081bc9a', 'bytes_prefix_hex': '444f4d41494e2d5355464649582c6578616d706c652e636f6d0a'}} | None |
| `files.shared/rules/by-id/434b78ce44f8eaee71ae7b525741498a2722b49eef17c997516db1063414d718.list.bak` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/c0e212e5ba7fb9c5247d13ed07e2c8326ddb3ee2482e9d43731882686d235f5f.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 26, 'bytes_sha256': '145f7fd7beed2e748e7cfadb10fa168d309267d703f0ec09358ede2e8081bc9a', 'bytes_prefix_hex': '444f4d41494e2d5355464649582c6578616d706c652e636f6d0a'}} |
| `files.shared/rules/by-id/c0e212e5ba7fb9c5247d13ed07e2c8326ddb3ee2482e9d43731882686d235f5f.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-B01 / name-collision/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_02dcbd56936344a0a208058294cec6eb' | 'lib_b110935847f39dd7cb0bedef42af1d8a' |
| `body.candidates[3].id` | value | 'lib_e8a8574dfab847aa9a4b3fe292564317' | 'lib_de66dd68509ae5943f0b83983484cf5e' |
| `body.direct.id` | value | 'lib_02dcbd56936344a0a208058294cec6eb' | 'lib_b110935847f39dd7cb0bedef42af1d8a' |
| `body.direct.references[0].id` | value | 'ruleset_e001ab17-4dd7-4727-96b0-89c5ad752562' | 'ruleset_bce50c10cbf9344258d866b6e31fcb4e' |
| `body.proxy.id` | value | 'lib_e8a8574dfab847aa9a4b3fe292564317' | 'lib_de66dd68509ae5943f0b83983484cf5e' |
| `body.proxy.references[0].id` | value | 'ruleset_d55fd29d-ae9a-4cbe-bd12-b552cedcbcdc' | 'ruleset_bffa1963c7f623143f76c297a57a50d9' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_02dcbd56936344a0a208058294cec6eb' | 'lib_b110935847f39dd7cb0bedef42af1d8a' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_e8a8574dfab847aa9a4b3fe292564317' | 'lib_de66dd68509ae5943f0b83983484cf5e' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_e001ab17-4dd7-4727-96b0-89c5ad752562' | 'ruleset_bce50c10cbf9344258d866b6e31fcb4e' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_02dcbd56936344a0a208058294cec6eb' | 'lib_b110935847f39dd7cb0bedef42af1d8a' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_d55fd29d-ae9a-4cbe-bd12-b552cedcbcdc' | 'ruleset_bffa1963c7f623143f76c297a57a50d9' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_e8a8574dfab847aa9a4b3fe292564317' | 'lib_de66dd68509ae5943f0b83983484cf5e' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_02dcbd56936344a0a208058294cec6eb' | 'lib_b110935847f39dd7cb0bedef42af1d8a' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_e8a8574dfab847aa9a4b3fe292564317' | 'lib_de66dd68509ae5943f0b83983484cf5e' |
| `files.shared/rules/by-id/15c16a903d85182d1f53648a68f91645bb29bbbfeffc1c041d96045e61fa679b.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/17b2c959ad3a6c6d88ea41dfbd1bebd9d6d4ddeb310c3b5af6670dce23c73d35.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/bf62e4f5ce2e8d9e088420696327bcd3f647daf4df66e09bec7bc10cac39b978.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/f79fcba31d1a4e6fe862b2838aead7e0c4b75c7ef19cba64b955697bbcffd59e.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-B02 / before-MATCH/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_570cf08aca854be88fb5bf1c384894d3' | 'lib_bbfba28c8374a3812bde65477fde6fe9' |
| `body.candidates[3].id` | value | 'lib_4e866bf627ac4b84a0ea011d04802e55' | 'lib_1bef591c57159858b936cfdd75b0f7ee' |
| `body.direct.id` | value | 'lib_570cf08aca854be88fb5bf1c384894d3' | 'lib_bbfba28c8374a3812bde65477fde6fe9' |
| `body.direct.references[0].id` | value | 'ruleset_2365e719-f074-45b4-9e44-9d82fac665f4' | 'ruleset_fbe52ee29a43d25585144cbc0d86f5f6' |
| `body.proxy.id` | value | 'lib_4e866bf627ac4b84a0ea011d04802e55' | 'lib_1bef591c57159858b936cfdd75b0f7ee' |
| `body.proxy.references[0].id` | value | 'ruleset_2fd28914-7478-471f-a5dd-742838c286bc' | 'ruleset_efbcd993ad7e17ef89ca23e5b2c90a6c' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_570cf08aca854be88fb5bf1c384894d3' | 'lib_bbfba28c8374a3812bde65477fde6fe9' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_4e866bf627ac4b84a0ea011d04802e55' | 'lib_1bef591c57159858b936cfdd75b0f7ee' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_2365e719-f074-45b4-9e44-9d82fac665f4' | 'ruleset_fbe52ee29a43d25585144cbc0d86f5f6' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_570cf08aca854be88fb5bf1c384894d3' | 'lib_bbfba28c8374a3812bde65477fde6fe9' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_2fd28914-7478-471f-a5dd-742838c286bc' | 'ruleset_efbcd993ad7e17ef89ca23e5b2c90a6c' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_4e866bf627ac4b84a0ea011d04802e55' | 'lib_1bef591c57159858b936cfdd75b0f7ee' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_570cf08aca854be88fb5bf1c384894d3' | 'lib_bbfba28c8374a3812bde65477fde6fe9' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_4e866bf627ac4b84a0ea011d04802e55' | 'lib_1bef591c57159858b936cfdd75b0f7ee' |
| `files.shared/rules/by-id/c0ea25224ca839f76cf0e8b9a8e8e78e2dd02e933b1ba4fec3a1027a043b3076.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/cdadf1efa3f74cc6611fd8df9b7d03c2b8582dbff49e40a1f4f229da5be7f3b3.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/ed63c6ba187acfb5e56c6d40fdd21f7ff9f76f83410d0d55eba8db767006559a.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/ee154bf3925fd521f7aba0a997d04b05a853d81d4ad046d7a2b203162d6fd664.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S01 / direct/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_65e918e60ea7485c83eb54536db39cf1' | 'lib_f3c8fdb8bcb12f475773004ac5d70551' |
| `body.direct.id` | value | 'lib_65e918e60ea7485c83eb54536db39cf1' | 'lib_f3c8fdb8bcb12f475773004ac5d70551' |
| `body.direct.references[0].id` | value | 'ruleset_701c1a96-fc53-4b5e-842b-14acfa073e57' | 'ruleset_47a9452bef59a550ac562f2ee9ed336b' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_65e918e60ea7485c83eb54536db39cf1' | 'lib_f3c8fdb8bcb12f475773004ac5d70551' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_701c1a96-fc53-4b5e-842b-14acfa073e57' | 'ruleset_47a9452bef59a550ac562f2ee9ed336b' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_65e918e60ea7485c83eb54536db39cf1' | 'lib_f3c8fdb8bcb12f475773004ac5d70551' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_65e918e60ea7485c83eb54536db39cf1' | 'lib_f3c8fdb8bcb12f475773004ac5d70551' |
| `files.shared/rules/by-id/82c540f56845976bf8aab95ccd119ce7e6af76d04f6af886a676d0777b5f1039.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/e7a611a4649573b8f93ea26dc1b4cf171e09bc671b7f21274f3844d25274c7fc.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S01 / duplicate-targets/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_4f429dfb435f4b07ac7888121c9c0ea8' | 'lib_76ce2b5eed1a63cf50cea0ff205efacf' |
| `body.candidates[3].id` | value | 'lib_2ddf4e5195f74df7b6bd63f543fe51e5' | 'lib_9c7e1d58363b464b85588940713eaa0e' |
| `body.direct.id` | value | 'lib_2ddf4e5195f74df7b6bd63f543fe51e5' | 'lib_9c7e1d58363b464b85588940713eaa0e' |
| `body.direct.references[0].id` | value | 'ruleset_cbfda791-b06a-471b-939b-9c95be37e209' | 'ruleset_e3f63e79db4e296886d58e78e50f6578' |
| `body.direct.references[1].id` | value | 'ruleset_a97a0538-731a-4fe8-b3a1-6a2e1e45749c' | 'ruleset_2a8c3fffb6e98a9a2df62c2f8c8b9717' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_2ddf4e5195f74df7b6bd63f543fe51e5' | 'lib_9c7e1d58363b464b85588940713eaa0e' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_cbfda791-b06a-471b-939b-9c95be37e209' | 'ruleset_e3f63e79db4e296886d58e78e50f6578' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_2ddf4e5195f74df7b6bd63f543fe51e5' | 'lib_9c7e1d58363b464b85588940713eaa0e' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_a97a0538-731a-4fe8-b3a1-6a2e1e45749c' | 'ruleset_2a8c3fffb6e98a9a2df62c2f8c8b9717' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_2ddf4e5195f74df7b6bd63f543fe51e5' | 'lib_9c7e1d58363b464b85588940713eaa0e' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_4f429dfb435f4b07ac7888121c9c0ea8' | 'lib_76ce2b5eed1a63cf50cea0ff205efacf' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_2ddf4e5195f74df7b6bd63f543fe51e5' | 'lib_9c7e1d58363b464b85588940713eaa0e' |
| `files.shared/rules/by-id/0d2257fc072a9385f1b9afbdfe3a8f7ea0ce438bbb2c3549bc90fd5d6e838003.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/bbc25944ff7525154acf9c5fd6a22f9e29d1e4f9aedfde10e4edccfbf5a1f5c9.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/bbc25944ff7525154acf9c5fd6a22f9e29d1e4f9aedfde10e4edccfbf5a1f5c9.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S02 / proxy/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_ccd60822d9e34e039341da5261461f72' | 'lib_2cdb830e75979950138e84fb35a858e9' |
| `body.proxy.id` | value | 'lib_ccd60822d9e34e039341da5261461f72' | 'lib_2cdb830e75979950138e84fb35a858e9' |
| `body.proxy.references[0].id` | value | 'ruleset_27500f86-1317-4b94-be44-0e09a7d9c96b' | 'ruleset_c0b1b2fb253a815a47b6e3d55e229151' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_ccd60822d9e34e039341da5261461f72' | 'lib_2cdb830e75979950138e84fb35a858e9' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_27500f86-1317-4b94-be44-0e09a7d9c96b' | 'ruleset_c0b1b2fb253a815a47b6e3d55e229151' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_ccd60822d9e34e039341da5261461f72' | 'lib_2cdb830e75979950138e84fb35a858e9' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_ccd60822d9e34e039341da5261461f72' | 'lib_2cdb830e75979950138e84fb35a858e9' |
| `files.shared/rules/by-id/066bef8533efcb9e13d95e7fde181bab12e18294a0f057fa4478a96b5ffb6c1d.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/7b1fe4310b2ab863a81946034c27c1a13d6d43374f0a7319f82c4c7ec3ab77f6.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S03 / both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_e1c68d2a144b44ee9de549111e7af409' | 'lib_b62ff56a6254a386c5ef10726d68538c' |
| `body.candidates[3].id` | value | 'lib_765cae69adde48c6ab49871fe8154205' | 'lib_54db11e89a09c2db1006812546af4598' |
| `body.direct.id` | value | 'lib_e1c68d2a144b44ee9de549111e7af409' | 'lib_b62ff56a6254a386c5ef10726d68538c' |
| `body.direct.references[0].id` | value | 'ruleset_a939896c-3707-4c9e-8a3b-a335d58e8507' | 'ruleset_b6a6a342e1a0991e901472e9d48e1694' |
| `body.proxy.id` | value | 'lib_765cae69adde48c6ab49871fe8154205' | 'lib_54db11e89a09c2db1006812546af4598' |
| `body.proxy.references[0].id` | value | 'ruleset_1cd90342-be47-4590-bc2f-11af545be45f' | 'ruleset_b921ec10fbaf06a4eb60e1ea3c49ea9b' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_e1c68d2a144b44ee9de549111e7af409' | 'lib_b62ff56a6254a386c5ef10726d68538c' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_765cae69adde48c6ab49871fe8154205' | 'lib_54db11e89a09c2db1006812546af4598' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_a939896c-3707-4c9e-8a3b-a335d58e8507' | 'ruleset_b6a6a342e1a0991e901472e9d48e1694' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_e1c68d2a144b44ee9de549111e7af409' | 'lib_b62ff56a6254a386c5ef10726d68538c' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_1cd90342-be47-4590-bc2f-11af545be45f' | 'ruleset_b921ec10fbaf06a4eb60e1ea3c49ea9b' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_765cae69adde48c6ab49871fe8154205' | 'lib_54db11e89a09c2db1006812546af4598' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_e1c68d2a144b44ee9de549111e7af409' | 'lib_b62ff56a6254a386c5ef10726d68538c' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_765cae69adde48c6ab49871fe8154205' | 'lib_54db11e89a09c2db1006812546af4598' |
| `files.shared/rules/by-id/0fe0368b1e11502b3b449dd6cb7c4d96980caad620a2f107285867c1f5961cbc.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/733be5c3960ed52030995e40f435f741db3023a6daed47b965f6305a18812344.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/887c111516719e1b8be59bc96a65460def24bc653b79c85db28046d99a0c0d0e.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/a1bd040f224d913a8ed23d7035d10fd43d89181b71ebeef045bec43fcd957226.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S03 / empty-means-both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_85bf1e1679b84a3ca6391de277be955b' | 'lib_6d956b81f7ff7a123332c8bd1a4a87a3' |
| `body.candidates[3].id` | value | 'lib_2f0d219bd5444769ac818e8b2b090921' | 'lib_e7f5d85dac88e4fac0dd880fe5f7065a' |
| `body.direct.id` | value | 'lib_85bf1e1679b84a3ca6391de277be955b' | 'lib_6d956b81f7ff7a123332c8bd1a4a87a3' |
| `body.direct.references[0].id` | value | 'ruleset_bf99b385-2804-4f1a-bf6a-d7f15e69c580' | 'ruleset_ae2a402f4a9cf9ad0f235c2cc2822112' |
| `body.proxy.id` | value | 'lib_2f0d219bd5444769ac818e8b2b090921' | 'lib_e7f5d85dac88e4fac0dd880fe5f7065a' |
| `body.proxy.references[0].id` | value | 'ruleset_ac05db80-91a7-4fb3-82bf-8768c75b3f5c' | 'ruleset_87dea9e3b3254858b00ba41b4bcac1a8' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_85bf1e1679b84a3ca6391de277be955b' | 'lib_6d956b81f7ff7a123332c8bd1a4a87a3' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_2f0d219bd5444769ac818e8b2b090921' | 'lib_e7f5d85dac88e4fac0dd880fe5f7065a' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_bf99b385-2804-4f1a-bf6a-d7f15e69c580' | 'ruleset_ae2a402f4a9cf9ad0f235c2cc2822112' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_85bf1e1679b84a3ca6391de277be955b' | 'lib_6d956b81f7ff7a123332c8bd1a4a87a3' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_ac05db80-91a7-4fb3-82bf-8768c75b3f5c' | 'ruleset_87dea9e3b3254858b00ba41b4bcac1a8' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_2f0d219bd5444769ac818e8b2b090921' | 'lib_e7f5d85dac88e4fac0dd880fe5f7065a' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_85bf1e1679b84a3ca6391de277be955b' | 'lib_6d956b81f7ff7a123332c8bd1a4a87a3' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_2f0d219bd5444769ac818e8b2b090921' | 'lib_e7f5d85dac88e4fac0dd880fe5f7065a' |
| `files.shared/rules/by-id/14eb790a43f7630a9c983bc10729af52a88e88cbdcb7166e4313e988409828a5.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/434932d04928040d0546d4613fbafb5ef123bd92e1d0e84855c34f2082250ac9.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/6dfc20112d0e5de858b185d794e961bee63f111d0898c25c18dc396f25f9206a.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/bf74c6e329ae44cf784523c726b0d53499b21aa7cfe97f20e1e99e75ae1e2c01.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S04 / repeat/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_384f6c1bda4f4ae2a3b985a50dda7932' | 'lib_b09a5848e98481c1359e884dc6fe6ab6' |
| `body.direct.id` | value | 'lib_384f6c1bda4f4ae2a3b985a50dda7932' | 'lib_b09a5848e98481c1359e884dc6fe6ab6' |
| `body.direct.references[0].id` | value | 'ruleset_6223b992-ad50-4164-b1f0-0e620e60ff1d' | 'ruleset_b17be4fe8ef30d8e9e976300d032fb8a' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_384f6c1bda4f4ae2a3b985a50dda7932' | 'lib_b09a5848e98481c1359e884dc6fe6ab6' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_6223b992-ad50-4164-b1f0-0e620e60ff1d' | 'ruleset_b17be4fe8ef30d8e9e976300d032fb8a' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_384f6c1bda4f4ae2a3b985a50dda7932' | 'lib_b09a5848e98481c1359e884dc6fe6ab6' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_384f6c1bda4f4ae2a3b985a50dda7932' | 'lib_b09a5848e98481c1359e884dc6fe6ab6' |
| `files.shared/rules/by-id/ae98ffe087274a2064d4dc3d21f7f751903d479de8b5788383afbe7e069f8d56.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/fceba6c959b1cfde808cdc45da4988f86851540c26623753a12bf45b9740a386.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S04 / repeat/1

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_384f6c1bda4f4ae2a3b985a50dda7932' | 'lib_b09a5848e98481c1359e884dc6fe6ab6' |
| `body.candidates[3].id` | value | 'lib_266b8314d7b547198e94208f50433e36' | 'lib_1b7c5e2df39c3c7e5dfa10b1b3281a6a' |
| `body.direct.id` | value | 'lib_266b8314d7b547198e94208f50433e36' | 'lib_1b7c5e2df39c3c7e5dfa10b1b3281a6a' |
| `body.direct.references[0].id` | value | 'ruleset_b924a4bd-7275-4722-963b-5ccaed9d8947' | 'ruleset_bcdacf9998972b71ea9ec0a73e7bf21a' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_266b8314d7b547198e94208f50433e36' | 'lib_1b7c5e2df39c3c7e5dfa10b1b3281a6a' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_6223b992-ad50-4164-b1f0-0e620e60ff1d' | 'ruleset_b17be4fe8ef30d8e9e976300d032fb8a' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_384f6c1bda4f4ae2a3b985a50dda7932' | 'lib_b09a5848e98481c1359e884dc6fe6ab6' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_b924a4bd-7275-4722-963b-5ccaed9d8947' | 'ruleset_bcdacf9998972b71ea9ec0a73e7bf21a' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_266b8314d7b547198e94208f50433e36' | 'lib_1b7c5e2df39c3c7e5dfa10b1b3281a6a' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_384f6c1bda4f4ae2a3b985a50dda7932' | 'lib_b09a5848e98481c1359e884dc6fe6ab6' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_266b8314d7b547198e94208f50433e36' | 'lib_1b7c5e2df39c3c7e5dfa10b1b3281a6a' |
| `files.shared/rules/by-id/55f26cfda829c6f482a241e6335469b2a9f073259944357638d7514a2cc39661.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/786f1f4905fc4cabf3aba940bf11e40bdc0dfbea4e27292d00b2eb165d635b38.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/ae98ffe087274a2064d4dc3d21f7f751903d479de8b5788383afbe7e069f8d56.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/fceba6c959b1cfde808cdc45da4988f86851540c26623753a12bf45b9740a386.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## X04 / new_domain/literal/"\ud800"

路由：`POST /api/server-domain`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 200 |
| `body.message` | missing-in-go | "'utf-8' codec can't encode character '\\ud800' in position 18: surrogates not allowed" | None |
| `body.server_domain` | extra-in-go | None | '�' |
| `body.success` | value | False | True |
| `db.fields.system//_revision.revision` | value | 4 | 5 |
| `db.fields.system//_revision.value` | value | 2 | 3 |
| `db.fields.system//system_config.revision` | value | 4 | 5 |
| `db.fields.system//system_config.value.server_domain` | value | 'http://panel.example' | '�' |
| `db.meta.generation` | value | 4 | 5 |

## R218-F09 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T03:03:20Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-17T03:03:20.606437+00:00', 'first_seen_at': '2026-11-17T03:03:20.606437+00:00', 'last_seen_at': '2026-11-17T03:03:20Z', 'occurre | None |

## R218-F12 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'application/json' | 'text/html; charset=utf-8' |
| `body` | type | {'message': '订阅操作失败', 'success': False} | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '仍使用上一次有效缓存（2026-01-01T00:00:00Z）', 'status_label': '执行失败', 'severity': 'warning', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T03:03:22Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-17T03:03:22.112375+00:00', 'first_seen_at': '2026-11-17T03:03:22.112375+00:00', 'last_seen_at': '202 | None |

## R218-F13 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'application/json' | 'text/html; charset=utf-8' |
| `body` | type | {'message': '订阅操作失败', 'success': False} | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] |

## R218-S03 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '仍使用上一次有效缓存（2026-01-01T00:00:00Z）', 'status_label': '执行失败', 'severity': 'warning', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T03:03:18Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-17T03:03:19.018167+00:00', 'first_seen_at': '2026-11-17T03:03:19.018167+00:00', 'last_seen_at': '202 | None |

## R058-B03 / stored-legacy-extension

路由：`PUT /api/domain-discovery/settings`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/domain_discovery.value.synthetic_legacy_extension` | extra-in-go | None | {'flag': False, 'values': []} |

## R217-S02 /

路由：`PUT /api/subscriptions/<sub_id>`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.shared//subscriptions.revision` | value | 2 | 5 |

## T27 / real-tick/due-and-backoff

路由：`startup`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:backoff': {'source': 'task', 'title': '合成-backoff · 订阅更新', 'message': '任务未完成，请查看任务详情。', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'backoff', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T02:45:39Z', 'failure_count': 3, 'id': 'task:subscription:backoff', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:54:28.096630+00:00', 'first_seen_at': '2026-11-17T02:54:28.096630+00:00', 'last_seen_at': '2026-11-17T02:45:39Z', ' | None |

## X08 / retired-node-test-dns-migration

路由：`startup`；状态：O12-separate

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `db.fields.profile/default/_revision.revision` | value | 5 | 2 |
| `db.fields.profile/default/_revision.value` | value | 2 | 1 |
| `db.fields.profile/default/updated_at.revision` | value | 5 | 1 |
| `db.fields.system//_revision.revision` | value | 6 | 4 |
| `db.fields.system//_revision.value` | value | 3 | 2 |
| `db.fields.system//agents.revision` | value | 6 | 4 |
| `db.fields.system//agents.value[0].node_test_dns_endpoints` | extra-in-go | None | {'legacy-synthetic': {'endpoint': '192.0.2.53:15353', 'profile_id': 'default', 'status': 'installed', 'plugins': [], 'installed': True}} |
| `db.fields.system//system_config.revision` | value | 6 | 4 |
| `db.fields.system//system_config.value.node_test_dns` | missing-in-go | ['https://dns.example/dns-query'] | None |
| `db.fields.system//system_config.value.node_test_dns_config.servers` | length | 1 | 2 |
| `db.fields.system//system_config.value.node_test_dns_config.servers[0]` | value | 'https://dns.example/dns-query' | '192.0.2.53:15353' |
| `db.meta.generation` | value | 6 | 4 |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'notice:node-test-dns-managed-removed': {'source': 'notice', 'scope': 'system', 'severity': 'warning', 'status_label': '已自动调整', 'title': '节点测速 DNS 已自动调整', 'message': '节点测速的「OxiDNS 托管」模式已移除。测速 DNS 已改为自定义：https://dns.example/dns-query。原专用入口 192.0.2.53:15353 已从 OxiDNS 配置中去掉，「合成 Agent 🧪」上的监听在下一次发布 OxiDNS 后关闭。', 'impact': '', 'checked_at': '2026-11-17T02:48:28.262249+00:00', 'target': {'path': '/nodes', 'query': {}, 'label': '查看测速设置'}, 'id': 'notice:node-test-dns-managed-removed', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:48:28.262293+00:00', 'first_seen_at': '2026-11-17T02:48:28.262293+00:00' | None |

## S91 CONNECT 传输观察（待兼容性审查）

仅上述精确字段组合的用例：R087-S91、R138-S91。

两端都先经fake代理尝试相同GitHub原站CONNECT，拒绝后再GET相同镜像URL；最终200、转换正文与存储一致。记录到的差别是Python使用HTTP/1.0且无Host/User-Agent，Go使用HTTP/1.1及上述标准字段。这里保留原始线级差异供审核，不据此要求Go模仿Python传输栈；没有归一化或吞掉字段。若目标、方法、认证、次数、正文或存储还存在其他差异，不会归入本说明。

## Oracle 限制：混合目标采纳的顺序

路由：`POST /api/domain-discovery/apply`；用例：R046-S03；类型：Python 集合遍历顺序不稳定。

未固定 hash seed 的首轮合成请求中，Python added.target 为 `[proxy, direct]`，Go 为 `[direct, proxy]`，同一批 history 顺序也不同；每个目标的内容一致。Python 来源为 `backend/utils/domain_discovery_service.py:567` 的 target 集合遍历。

当前独立用例固定 `PYTHONHASHSEED=0`，保留原始 HTTP 数组与数据库 history 顺序比较；该环境下实际比较通过。其他 Python hash 环境下的顺序不作通过保证，留作部分覆盖/审查项，没有对数组排序以吞掉差异。普通多次写入之间等待下一秒，避免秒级时间戳产生不同的排序平局。
