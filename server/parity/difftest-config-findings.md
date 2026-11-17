# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

## R087-B04 / text/B04

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': '远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 "b\'\\\\xff\'"：内容不是有效 UTF-8 文本；请检查订阅链接、编码或是否误选了 MRS 格式。', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T01:56:11Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '2026-11-17T01:56:11.315314+00:00', 'fir | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': "远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 'DOMAIN,example.com'：Domain 不接受 Classical 规则行；精确域名请填写 example.com，域名及其子域名请填写 +.example.com。", 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T01:56:09Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '202 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-no-cache': {'source': 'task', 'title': '手写规则🧪 · 规则集更新', 'message': '上游返回 HTTP 503', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-no-cache', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T01:55:41Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-no-cache', 'state': 'pending', 'read': False, 'version': '2026-11-17T01:55:41.366099+00:00', 'first_seen_at': '2026-11-17T01:55:41.366099+00:00', 'last_seen_at': '2026-11-17T0 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': '远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 "b\'\\\\xff\'"：内容不是有效 UTF-8 文本；请检查订阅链接、编码或是否误选了 MRS 格式。', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T01:56:18Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '2026-11-17T01:56:18.872537+00:00', 'fir | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': "远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 'DOMAIN,example.com'：Domain 不接受 Classical 规则行；精确域名请填写 example.com，域名及其子域名请填写 +.example.com。", 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T01:56:16Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '202 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-no-cache': {'source': 'task', 'title': '手写规则🧪 · 规则集更新', 'message': '上游返回 HTTP 503', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-no-cache', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T01:55:48Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-no-cache', 'state': 'pending', 'read': False, 'version': '2026-11-17T01:55:48.974891+00:00', 'first_seen_at': '2026-11-17T01:55:48.974891+00:00', 'last_seen_at': '2026-11-17T0 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:webdav_backup': {'source': 'task', 'title': 'WebDAV 手动备份', 'message': 'NotEnoughSpace', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'system', 'profile_name': None, 'target': {'path': '/system-settings', 'query': {'section': 'backup'}, 'label': '检查 WebDAV 设置'}, 'checked_at': '2026-11-17T01:54:56Z', 'failure_count': 1, 'id': 'task:webdav_backup', 'state': 'pending', 'read': False, 'version': '2026-11-17T01:54:56.268572+00:00', 'first_seen_at': '2026-11-17T01:54:56.268572+00:00', 'last_seen_at': '2026-11-17T01:54:56Z', 'occurrences': 1, 'evidence': '1:2026-11-1 | None |
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
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_e6b3e9eb-7e40-4157-b922-7a6d5e046e07' | 'ruleset_33cbeda23973a1c6808559092e4167b4' |

## R055-S02 / new/domain/example.com

路由：`POST /api/domain-discovery/region/route`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.added[0].ruleset_id` | value | 'lib_bccf44fcc5684e639c4e71c4db92ff86' | 'lib_462eb64eb8f472dfc28bea6cefa2b8cc' |
| `db.fields.profile/default/domain_discovery.value.history[0].ruleset_id` | value | 'lib_bccf44fcc5684e639c4e71c4db92ff86' | 'lib_462eb64eb8f472dfc28bea6cefa2b8cc' |
| `db.fields.profile/default/domain_discovery.value.regional_rulesets.代理🧪` | value | 'lib_bccf44fcc5684e639c4e71c4db92ff86' | 'lib_462eb64eb8f472dfc28bea6cefa2b8cc' |
| `db.fields.profile/default/rule_configs.value[1].id` | value | 'ruleset_20f6de3e-41ae-4fbc-a8fe-d3e88e67e9c8' | 'ruleset_388e02a09a82cd52168696b17a800215' |
| `db.fields.profile/default/rule_configs.value[1].library_rule_id` | value | 'lib_bccf44fcc5684e639c4e71c4db92ff86' | 'lib_462eb64eb8f472dfc28bea6cefa2b8cc' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_bccf44fcc5684e639c4e71c4db92ff86' | 'lib_462eb64eb8f472dfc28bea6cefa2b8cc' |
| `files.shared/rules/by-id/407e3ef8705c7db0758604dcba53ed45b5af9e5d29caa3310c66b04236fab037.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 26, 'bytes_sha256': '145f7fd7beed2e748e7cfadb10fa168d309267d703f0ec09358ede2e8081bc9a', 'bytes_prefix_hex': '444f4d41494e2d5355464649582c6578616d706c652e636f6d0a'}} |
| `files.shared/rules/by-id/407e3ef8705c7db0758604dcba53ed45b5af9e5d29caa3310c66b04236fab037.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/4f26e1bc483f94e68e40d29ff0e6f3d0052f23f8be7affa6c235f38e4657ab24.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 26, 'bytes_sha256': '145f7fd7beed2e748e7cfadb10fa168d309267d703f0ec09358ede2e8081bc9a', 'bytes_prefix_hex': '444f4d41494e2d5355464649582c6578616d706c652e636f6d0a'}} | None |
| `files.shared/rules/by-id/4f26e1bc483f94e68e40d29ff0e6f3d0052f23f8be7affa6c235f38e4657ab24.list.bak` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-B01 / name-collision/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_0533dea6356f4ba6b391ac224477872f' | 'lib_8674d38b4dcb58a776b2a43140af7f33' |
| `body.candidates[3].id` | value | 'lib_7e7ffe2431904d1bbe0686e9ab10f91b' | 'lib_b4882d7b6e107442b073bc69f3437af3' |
| `body.direct.id` | value | 'lib_0533dea6356f4ba6b391ac224477872f' | 'lib_8674d38b4dcb58a776b2a43140af7f33' |
| `body.direct.references[0].id` | value | 'ruleset_55bd6b7a-d33f-4849-9da6-e82d762f3912' | 'ruleset_90a81f6961f1323c1f9dc0fe9a01b8f7' |
| `body.proxy.id` | value | 'lib_7e7ffe2431904d1bbe0686e9ab10f91b' | 'lib_b4882d7b6e107442b073bc69f3437af3' |
| `body.proxy.references[0].id` | value | 'ruleset_73fde686-984e-45fa-816c-8b10840cb5e3' | 'ruleset_ff65a34c436fc893a83ec63de5e57dbe' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_0533dea6356f4ba6b391ac224477872f' | 'lib_8674d38b4dcb58a776b2a43140af7f33' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_7e7ffe2431904d1bbe0686e9ab10f91b' | 'lib_b4882d7b6e107442b073bc69f3437af3' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_55bd6b7a-d33f-4849-9da6-e82d762f3912' | 'ruleset_90a81f6961f1323c1f9dc0fe9a01b8f7' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_0533dea6356f4ba6b391ac224477872f' | 'lib_8674d38b4dcb58a776b2a43140af7f33' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_73fde686-984e-45fa-816c-8b10840cb5e3' | 'ruleset_ff65a34c436fc893a83ec63de5e57dbe' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_7e7ffe2431904d1bbe0686e9ab10f91b' | 'lib_b4882d7b6e107442b073bc69f3437af3' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_0533dea6356f4ba6b391ac224477872f' | 'lib_8674d38b4dcb58a776b2a43140af7f33' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_7e7ffe2431904d1bbe0686e9ab10f91b' | 'lib_b4882d7b6e107442b073bc69f3437af3' |
| `files.shared/rules/by-id/47a17fe3ce08add096ebfcbcd8654cd796614258cd5ba83a94cb30f342e59948.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/b17b7006469fabf6f9c258e6256d4bbb41a3f4760ae3a10812782b5ccb136a9f.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/b842e564c5d8e7c397da955e17ce322f1c0a10b751860983e3f42f30d21319a7.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/d8763e2b3ab9db2cb0f390d0a2f6f00ef60fbaf26ab2f48f747b40bd970dbc10.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-B02 / before-MATCH/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_3b540ffe22f8462a9ec1b21b9d75326b' | 'lib_49c183cca6ccccf84b4f5a9a783ab970' |
| `body.candidates[3].id` | value | 'lib_a19c04bee0bd40e188dd544cdd30ab3e' | 'lib_49150da5306ff72d67e3f182225cc7c4' |
| `body.direct.id` | value | 'lib_3b540ffe22f8462a9ec1b21b9d75326b' | 'lib_49c183cca6ccccf84b4f5a9a783ab970' |
| `body.direct.references[0].id` | value | 'ruleset_54dcf690-7765-4f80-8ff8-c943aeb94f49' | 'ruleset_7d6a451e55189df2af926d6b79f58003' |
| `body.proxy.id` | value | 'lib_a19c04bee0bd40e188dd544cdd30ab3e' | 'lib_49150da5306ff72d67e3f182225cc7c4' |
| `body.proxy.references[0].id` | value | 'ruleset_7b6ae81c-2e96-4b2f-8e65-db1af07831b1' | 'ruleset_b88c232d43d1303c0c6d3b8f46753851' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_3b540ffe22f8462a9ec1b21b9d75326b' | 'lib_49c183cca6ccccf84b4f5a9a783ab970' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_a19c04bee0bd40e188dd544cdd30ab3e' | 'lib_49150da5306ff72d67e3f182225cc7c4' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_54dcf690-7765-4f80-8ff8-c943aeb94f49' | 'ruleset_7d6a451e55189df2af926d6b79f58003' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_3b540ffe22f8462a9ec1b21b9d75326b' | 'lib_49c183cca6ccccf84b4f5a9a783ab970' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_7b6ae81c-2e96-4b2f-8e65-db1af07831b1' | 'ruleset_b88c232d43d1303c0c6d3b8f46753851' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_a19c04bee0bd40e188dd544cdd30ab3e' | 'lib_49150da5306ff72d67e3f182225cc7c4' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_3b540ffe22f8462a9ec1b21b9d75326b' | 'lib_49c183cca6ccccf84b4f5a9a783ab970' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_a19c04bee0bd40e188dd544cdd30ab3e' | 'lib_49150da5306ff72d67e3f182225cc7c4' |
| `files.shared/rules/by-id/01aba351d934962938e907d83a0abd501cad88caccc64d5d913aa783a7ac4ae4.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/791170dc1aae3fd4c4bded3e3e34b573b47e4709bff6c41d142736fb24521559.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/efa128c14ca53dc66408c1c2258a6daf763997032b4cddf73ee3e4f507d0c024.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/f43be47d0af165167d85dc5124df89ea74b882e6d43f2b6a90cd6a3453bc1977.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S01 / direct/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_78c5a27cdffe4d5db2846be34e4a14d7' | 'lib_6a825e1622ff44bf99488b0b5c6fcb27' |
| `body.direct.id` | value | 'lib_78c5a27cdffe4d5db2846be34e4a14d7' | 'lib_6a825e1622ff44bf99488b0b5c6fcb27' |
| `body.direct.references[0].id` | value | 'ruleset_ed9aa212-a5aa-4f30-90ec-501c62429a19' | 'ruleset_11f0ee51dfa9ffc323ffee8d0031d3f2' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_78c5a27cdffe4d5db2846be34e4a14d7' | 'lib_6a825e1622ff44bf99488b0b5c6fcb27' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_ed9aa212-a5aa-4f30-90ec-501c62429a19' | 'ruleset_11f0ee51dfa9ffc323ffee8d0031d3f2' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_78c5a27cdffe4d5db2846be34e4a14d7' | 'lib_6a825e1622ff44bf99488b0b5c6fcb27' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_78c5a27cdffe4d5db2846be34e4a14d7' | 'lib_6a825e1622ff44bf99488b0b5c6fcb27' |
| `files.shared/rules/by-id/15180be51f1633de552f0fef2d2d36b5b2704f7f04baa1536c7e1b01d22b0305.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/9838079de193e707e87ae96c3f14532cfa2635fe2fdc4a612a69e08bf0b0c7fc.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S01 / duplicate-targets/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_4e63e33232cc4cc9b35c4a5d125caf6a' | 'lib_9be07069cdaeb02cd4997e0d2cac250d' |
| `body.candidates[3].id` | value | 'lib_444363c052854b20b653498bfc9ea1e8' | 'lib_0aa8e83296fa16ced1a5082ed67e9809' |
| `body.direct.id` | value | 'lib_444363c052854b20b653498bfc9ea1e8' | 'lib_0aa8e83296fa16ced1a5082ed67e9809' |
| `body.direct.references[0].id` | value | 'ruleset_03358175-e994-4b62-84d8-c0c15f7a2cb6' | 'ruleset_a208e50a7e84c4d99be4490e450ca48c' |
| `body.direct.references[1].id` | value | 'ruleset_f1251732-84c5-4f0b-902e-6fbb8817a1df' | 'ruleset_a977720fe494655032605b3222a46035' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_444363c052854b20b653498bfc9ea1e8' | 'lib_0aa8e83296fa16ced1a5082ed67e9809' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_03358175-e994-4b62-84d8-c0c15f7a2cb6' | 'ruleset_a208e50a7e84c4d99be4490e450ca48c' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_444363c052854b20b653498bfc9ea1e8' | 'lib_0aa8e83296fa16ced1a5082ed67e9809' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_f1251732-84c5-4f0b-902e-6fbb8817a1df' | 'ruleset_a977720fe494655032605b3222a46035' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_444363c052854b20b653498bfc9ea1e8' | 'lib_0aa8e83296fa16ced1a5082ed67e9809' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_4e63e33232cc4cc9b35c4a5d125caf6a' | 'lib_9be07069cdaeb02cd4997e0d2cac250d' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_444363c052854b20b653498bfc9ea1e8' | 'lib_0aa8e83296fa16ced1a5082ed67e9809' |
| `files.shared/rules/by-id/04b9a9ded3e1ad4322c6e898773e7f3e8e8b15bc48ff45607854623c75501e2c.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/c7dbd4936511e2c374ff56624df32c7438f2a1f0267cc18cbf449d7ad833d686.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/c7dbd4936511e2c374ff56624df32c7438f2a1f0267cc18cbf449d7ad833d686.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S02 / proxy/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_4545cc343cb4405eb4cedc32286cc538' | 'lib_95f61101303071c7b985d55ad3cd34fb' |
| `body.proxy.id` | value | 'lib_4545cc343cb4405eb4cedc32286cc538' | 'lib_95f61101303071c7b985d55ad3cd34fb' |
| `body.proxy.references[0].id` | value | 'ruleset_7e505219-365d-487d-97ce-e374749683fa' | 'ruleset_4db87ca1c0f91581ed6d1895996c4ee0' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_4545cc343cb4405eb4cedc32286cc538' | 'lib_95f61101303071c7b985d55ad3cd34fb' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_7e505219-365d-487d-97ce-e374749683fa' | 'ruleset_4db87ca1c0f91581ed6d1895996c4ee0' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_4545cc343cb4405eb4cedc32286cc538' | 'lib_95f61101303071c7b985d55ad3cd34fb' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_4545cc343cb4405eb4cedc32286cc538' | 'lib_95f61101303071c7b985d55ad3cd34fb' |
| `files.shared/rules/by-id/719cf873de7f134a27f7f9ba9efb2fd3de36ffa8c0d36218601cabd940f24338.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/cde2a4c07ec214b93bda7045f579cc2de4248d2618ce633b9239a0407acd6de8.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S03 / both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_c182b42da87e426b8ae9322681a86c51' | 'lib_e142743fb299694d4f99a7be1607dc54' |
| `body.candidates[3].id` | value | 'lib_d80a01acd48b476d9382edcab253ade9' | 'lib_50228b1e2035fb7a4499ded8f17cf674' |
| `body.direct.id` | value | 'lib_c182b42da87e426b8ae9322681a86c51' | 'lib_e142743fb299694d4f99a7be1607dc54' |
| `body.direct.references[0].id` | value | 'ruleset_09977719-4b55-4c25-9aca-bf018739d942' | 'ruleset_01350effe768feea780269f63c1d3107' |
| `body.proxy.id` | value | 'lib_d80a01acd48b476d9382edcab253ade9' | 'lib_50228b1e2035fb7a4499ded8f17cf674' |
| `body.proxy.references[0].id` | value | 'ruleset_ece9980a-1967-4c4f-8982-f3f61607a791' | 'ruleset_1c8d7fd218043ba44c44baf562131f11' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_c182b42da87e426b8ae9322681a86c51' | 'lib_e142743fb299694d4f99a7be1607dc54' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_d80a01acd48b476d9382edcab253ade9' | 'lib_50228b1e2035fb7a4499ded8f17cf674' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_09977719-4b55-4c25-9aca-bf018739d942' | 'ruleset_01350effe768feea780269f63c1d3107' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_c182b42da87e426b8ae9322681a86c51' | 'lib_e142743fb299694d4f99a7be1607dc54' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_ece9980a-1967-4c4f-8982-f3f61607a791' | 'ruleset_1c8d7fd218043ba44c44baf562131f11' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_d80a01acd48b476d9382edcab253ade9' | 'lib_50228b1e2035fb7a4499ded8f17cf674' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_c182b42da87e426b8ae9322681a86c51' | 'lib_e142743fb299694d4f99a7be1607dc54' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_d80a01acd48b476d9382edcab253ade9' | 'lib_50228b1e2035fb7a4499ded8f17cf674' |
| `files.shared/rules/by-id/1d3d07875532da8ec1fd13d025ca7d8fdd0ba9cd79dde7a2be3fa582008b248e.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/39b51038a7af97dc1464c4b5d9b5e49f6ae8262bded03fc107d5f9a7fff90164.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/c2ae329ec84fb1e9dd8b3aed89034b659bd0d81abe4d985dc2bb10fe7186fd39.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/d05faca9b3041a2ab892ea5d2b8d9dc50905b2ffef20570925799a64db21ab78.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S03 / empty-means-both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_3c1897e9e15342a6b8b697e84809aecf' | 'lib_c8a2fafb14f97c0a92df8ebe100a270a' |
| `body.candidates[3].id` | value | 'lib_c27b5442c39b430cbbd4a3fc504c5f6e' | 'lib_a2fb6035d38e440be95fbf95b6018c55' |
| `body.direct.id` | value | 'lib_3c1897e9e15342a6b8b697e84809aecf' | 'lib_c8a2fafb14f97c0a92df8ebe100a270a' |
| `body.direct.references[0].id` | value | 'ruleset_6091b6db-70cf-411b-b1de-27a12bd05cdd' | 'ruleset_50562bd3c77654c94c84b474d39ffbb5' |
| `body.proxy.id` | value | 'lib_c27b5442c39b430cbbd4a3fc504c5f6e' | 'lib_a2fb6035d38e440be95fbf95b6018c55' |
| `body.proxy.references[0].id` | value | 'ruleset_b108b8bf-a4db-4d0b-b39d-ed8813d37793' | 'ruleset_1432665c6a9683ce06baa52d3bfe0b15' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_3c1897e9e15342a6b8b697e84809aecf' | 'lib_c8a2fafb14f97c0a92df8ebe100a270a' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_c27b5442c39b430cbbd4a3fc504c5f6e' | 'lib_a2fb6035d38e440be95fbf95b6018c55' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_6091b6db-70cf-411b-b1de-27a12bd05cdd' | 'ruleset_50562bd3c77654c94c84b474d39ffbb5' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_3c1897e9e15342a6b8b697e84809aecf' | 'lib_c8a2fafb14f97c0a92df8ebe100a270a' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_b108b8bf-a4db-4d0b-b39d-ed8813d37793' | 'ruleset_1432665c6a9683ce06baa52d3bfe0b15' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_c27b5442c39b430cbbd4a3fc504c5f6e' | 'lib_a2fb6035d38e440be95fbf95b6018c55' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_3c1897e9e15342a6b8b697e84809aecf' | 'lib_c8a2fafb14f97c0a92df8ebe100a270a' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_c27b5442c39b430cbbd4a3fc504c5f6e' | 'lib_a2fb6035d38e440be95fbf95b6018c55' |
| `files.shared/rules/by-id/27932e23f9b5445f49c7dcaba4c694c3df79f9e156ac280a3ad8a44ac2f548f5.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/35571134b4ad666aed650e10375092901648861308c7bd43309fc96a7c1e7503.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/7388e2d1fddd30797e4d33b38e843be205eac699903149aeeac88d741e3c497b.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/7516acfe7c5f453c3bdfdb4ba6b15772d28aa46971a81a620f740e68a98e7fbb.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S04 / repeat/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_9f57701f3b584f869b5e5944aa436d56' | 'lib_a035e985e59115f37faec5d71fa0eae6' |
| `body.direct.id` | value | 'lib_9f57701f3b584f869b5e5944aa436d56' | 'lib_a035e985e59115f37faec5d71fa0eae6' |
| `body.direct.references[0].id` | value | 'ruleset_a1be7e24-f678-4f0c-8450-885e1413c1c1' | 'ruleset_e27ff2bfbb28df25b75531b78c93ddb7' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_9f57701f3b584f869b5e5944aa436d56' | 'lib_a035e985e59115f37faec5d71fa0eae6' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_a1be7e24-f678-4f0c-8450-885e1413c1c1' | 'ruleset_e27ff2bfbb28df25b75531b78c93ddb7' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_9f57701f3b584f869b5e5944aa436d56' | 'lib_a035e985e59115f37faec5d71fa0eae6' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_9f57701f3b584f869b5e5944aa436d56' | 'lib_a035e985e59115f37faec5d71fa0eae6' |
| `files.shared/rules/by-id/3dcfb7af81da07c973a1fa21e18b6aa3c7ecffcf40d9d8e388e9931edfe68966.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/57690cfbdfc15c07ff9d154f3e2e99d5a6968f17ae498f024386443fb2e9d85f.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S04 / repeat/1

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_9f57701f3b584f869b5e5944aa436d56' | 'lib_a035e985e59115f37faec5d71fa0eae6' |
| `body.candidates[3].id` | value | 'lib_75f899ad963d43eaaa5051ebdff6e15d' | 'lib_896dc8649067c1decee251d63eb757da' |
| `body.direct.id` | value | 'lib_75f899ad963d43eaaa5051ebdff6e15d' | 'lib_896dc8649067c1decee251d63eb757da' |
| `body.direct.references[0].id` | value | 'ruleset_78983d51-46e8-4309-882e-67a3e151ee55' | 'ruleset_78d2cac3b385f545ad8c03ed5221b862' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_75f899ad963d43eaaa5051ebdff6e15d' | 'lib_896dc8649067c1decee251d63eb757da' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_a1be7e24-f678-4f0c-8450-885e1413c1c1' | 'ruleset_e27ff2bfbb28df25b75531b78c93ddb7' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_9f57701f3b584f869b5e5944aa436d56' | 'lib_a035e985e59115f37faec5d71fa0eae6' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_78983d51-46e8-4309-882e-67a3e151ee55' | 'ruleset_78d2cac3b385f545ad8c03ed5221b862' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_75f899ad963d43eaaa5051ebdff6e15d' | 'lib_896dc8649067c1decee251d63eb757da' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_9f57701f3b584f869b5e5944aa436d56' | 'lib_a035e985e59115f37faec5d71fa0eae6' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_75f899ad963d43eaaa5051ebdff6e15d' | 'lib_896dc8649067c1decee251d63eb757da' |
| `files.shared/rules/by-id/1268e4540df8cd7cdfc40db21125d1a2b00a20c3254ef7d813ba396b0d05c793.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/3dcfb7af81da07c973a1fa21e18b6aa3c7ecffcf40d9d8e388e9931edfe68966.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/57690cfbdfc15c07ff9d154f3e2e99d5a6968f17ae498f024386443fb2e9d85f.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/f20ce93264552f6a64cc641f6bd34a01ac785bfa16aaf3de756fe075f7bdc41d.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T02:06:47Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:06:47.124360+00:00', 'first_seen_at': '2026-11-17T02:06:47.124360+00:00', 'last_seen_at': '2026-11-17T02:06:47Z', 'occurre | None |

## R218-F12 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'application/json' | 'text/html; charset=utf-8' |
| `body` | type | {'message': '订阅操作失败', 'success': False} | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '仍使用上一次有效缓存（2026-01-01T00:00:00Z）', 'status_label': '执行失败', 'severity': 'warning', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T02:06:48Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:06:48.718542+00:00', 'first_seen_at': '2026-11-17T02:06:48.718542+00:00', 'last_seen_at': '202 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '仍使用上一次有效缓存（2026-01-01T00:00:00Z）', 'status_label': '执行失败', 'severity': 'warning', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T02:06:45Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-17T02:06:45.564271+00:00', 'first_seen_at': '2026-11-17T02:06:45.564271+00:00', 'last_seen_at': '202 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:backoff': {'source': 'task', 'title': '合成-backoff · 订阅更新', 'message': '任务未完成，请查看任务详情。', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'backoff', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T01:49:32Z', 'failure_count': 3, 'id': 'task:subscription:backoff', 'state': 'pending', 'read': False, 'version': '2026-11-17T01:58:21.558846+00:00', 'first_seen_at': '2026-11-17T01:58:21.558846+00:00', 'last_seen_at': '2026-11-17T01:49:32Z', ' | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'notice:node-test-dns-managed-removed': {'source': 'notice', 'scope': 'system', 'severity': 'warning', 'status_label': '已自动调整', 'title': '节点测速 DNS 已自动调整', 'message': '节点测速的「OxiDNS 托管」模式已移除。测速 DNS 已改为自定义：https://dns.example/dns-query。原专用入口 192.0.2.53:15353 已从 OxiDNS 配置中去掉，「合成 Agent 🧪」上的监听在下一次发布 OxiDNS 后关闭。', 'impact': '', 'checked_at': '2026-11-17T01:53:50.068230+00:00', 'target': {'path': '/nodes', 'query': {}, 'label': '查看测速设置'}, 'id': 'notice:node-test-dns-managed-removed', 'state': 'pending', 'read': False, 'version': '2026-11-17T01:53:50.068281+00:00', 'first_seen_at': '2026-11-17T01:53:50.068281+00:00' | None |

## S91 CONNECT 传输观察（待兼容性审查）

仅上述精确字段组合的用例：R087-S91、R138-S91。

两端都先经fake代理尝试相同GitHub原站CONNECT，拒绝后再GET相同镜像URL；最终200、转换正文与存储一致。记录到的差别是Python使用HTTP/1.0且无Host/User-Agent，Go使用HTTP/1.1及上述标准字段。这里保留原始线级差异供审核，不据此要求Go模仿Python传输栈；没有归一化或吞掉字段。若目标、方法、认证、次数、正文或存储还存在其他差异，不会归入本说明。

## Oracle 限制：混合目标采纳的顺序

路由：`POST /api/domain-discovery/apply`；用例：R046-S03；类型：Python 集合遍历顺序不稳定。

未固定 hash seed 的首轮合成请求中，Python added.target 为 `[proxy, direct]`，Go 为 `[direct, proxy]`，同一批 history 顺序也不同；每个目标的内容一致。Python 来源为 `backend/utils/domain_discovery_service.py:567` 的 target 集合遍历。

当前独立用例固定 `PYTHONHASHSEED=0`，保留原始 HTTP 数组与数据库 history 顺序比较；该环境下实际比较通过。其他 Python hash 环境下的顺序不作通过保证，留作部分覆盖/审查项，没有对数组排序以吞掉差异。普通多次写入之间等待下一秒，避免秒级时间戳产生不同的排序平局。
