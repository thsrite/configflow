# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

## R087-B04 / text/B04

路由：`GET /api/mosdns/rule-proxy`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': '远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 "b\'\\\\xff\'"：内容不是有效 UTF-8 文本；请检查订阅链接、编码或是否误选了 MRS 格式。', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T00:37:19Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '2026-11-17T00:37:19.743187+00:00', 'fir | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': "远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 'DOMAIN,example.com'：Domain 不接受 Classical 规则行；精确域名请填写 example.com，域名及其子域名请填写 +.example.com。", 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T00:37:18Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '202 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-no-cache': {'source': 'task', 'title': '手写规则🧪 · 规则集更新', 'message': '上游返回 HTTP 503', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-no-cache', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T00:40:53Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-no-cache', 'state': 'pending', 'read': False, 'version': '2026-11-17T00:40:53.051292+00:00', 'first_seen_at': '2026-11-17T00:40:53.051292+00:00', 'last_seen_at': '2026-11-17T0 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': '远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 "b\'\\\\xff\'"：内容不是有效 UTF-8 文本；请检查订阅链接、编码或是否误选了 MRS 格式。', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T00:37:25Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '2026-11-17T00:37:25.651702+00:00', 'fir | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-invalid': {'source': 'task', 'title': '声明无效内容 · 规则集更新', 'message': "远端规则集「声明无效内容」声明为 domain/text，第 1 行，原文 'DOMAIN,example.com'：Domain 不接受 Classical 规则行；精确域名请填写 example.com，域名及其子域名请填写 +.example.com。", 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-invalid', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T00:37:24Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-invalid', 'state': 'pending', 'read': False, 'version': '202 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:rule_source:proxy-no-cache': {'source': 'task', 'title': '手写规则🧪 · 规则集更新', 'message': '上游返回 HTTP 503', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/rule-library', 'query': {'rule': 'proxy-no-cache', 'section': 'settings'}, 'label': '检查规则集设置'}, 'checked_at': '2026-11-17T00:40:59Z', 'failure_count': 1, 'id': 'task:rule_source:proxy-no-cache', 'state': 'pending', 'read': False, 'version': '2026-11-17T00:40:59.988589+00:00', 'first_seen_at': '2026-11-17T00:40:59.988589+00:00', 'last_seen_at': '2026-11-17T0 | None |

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
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_50e25591-2bc1-439b-ae02-14f18a03ce9f' | 'ruleset_965d4a9374dd9ed7010949a9ab05af1d' |

## R055-S02 / new/domain/example.com

路由：`POST /api/domain-discovery/region/route`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.added[0].ruleset_id` | value | 'lib_a6ff4e682f4f4525a850abbbed210951' | 'lib_95e71a8fc00ee559ee2f019f447af47a' |
| `db.fields.profile/default/domain_discovery.value.history[0].ruleset_id` | value | 'lib_a6ff4e682f4f4525a850abbbed210951' | 'lib_95e71a8fc00ee559ee2f019f447af47a' |
| `db.fields.profile/default/domain_discovery.value.regional_rulesets.代理🧪` | value | 'lib_a6ff4e682f4f4525a850abbbed210951' | 'lib_95e71a8fc00ee559ee2f019f447af47a' |
| `db.fields.profile/default/rule_configs.value[1].id` | value | 'ruleset_151276f8-60d0-4404-bfd9-6ed9eeb7c7e4' | 'ruleset_abadd710bd227b0a0f39be5cb69d6eb6' |
| `db.fields.profile/default/rule_configs.value[1].library_rule_id` | value | 'lib_a6ff4e682f4f4525a850abbbed210951' | 'lib_95e71a8fc00ee559ee2f019f447af47a' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_a6ff4e682f4f4525a850abbbed210951' | 'lib_95e71a8fc00ee559ee2f019f447af47a' |
| `files.shared/rules/by-id/0f3fd2ffdd09ad4d4b36d06e885ec3a34d666c211acada70a160d4b25811262f.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 26, 'bytes_sha256': '145f7fd7beed2e748e7cfadb10fa168d309267d703f0ec09358ede2e8081bc9a', 'bytes_prefix_hex': '444f4d41494e2d5355464649582c6578616d706c652e636f6d0a'}} | None |
| `files.shared/rules/by-id/0f3fd2ffdd09ad4d4b36d06e885ec3a34d666c211acada70a160d4b25811262f.list.bak` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/ab46fef61a3b1f90bc38fa13b2a2490fd55ac3afa2b5823659c35784858634de.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 26, 'bytes_sha256': '145f7fd7beed2e748e7cfadb10fa168d309267d703f0ec09358ede2e8081bc9a', 'bytes_prefix_hex': '444f4d41494e2d5355464649582c6578616d706c652e636f6d0a'}} |
| `files.shared/rules/by-id/ab46fef61a3b1f90bc38fa13b2a2490fd55ac3afa2b5823659c35784858634de.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-B01 / name-collision/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_55af523c2fbc453495f0619c950c0956' | 'lib_bb865c92fcac77fac8cbf5cfd476228d' |
| `body.candidates[3].id` | value | 'lib_c6e1c2cc54ca4a4ebd3a1e24267c7d69' | 'lib_6c9c4c63df338f7b6a23afcf8e3ff8de' |
| `body.direct.id` | value | 'lib_55af523c2fbc453495f0619c950c0956' | 'lib_bb865c92fcac77fac8cbf5cfd476228d' |
| `body.direct.references[0].id` | value | 'ruleset_939e7af7-7a41-47ce-9891-a43efcc10c6d' | 'ruleset_9b16b913322fe6bb9870c7e2794b7674' |
| `body.proxy.id` | value | 'lib_c6e1c2cc54ca4a4ebd3a1e24267c7d69' | 'lib_6c9c4c63df338f7b6a23afcf8e3ff8de' |
| `body.proxy.references[0].id` | value | 'ruleset_b662511e-1a41-4e7a-90ab-0305531d6a24' | 'ruleset_5673cfa211bbafcdfd5f1f21235e2ac4' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_55af523c2fbc453495f0619c950c0956' | 'lib_bb865c92fcac77fac8cbf5cfd476228d' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_c6e1c2cc54ca4a4ebd3a1e24267c7d69' | 'lib_6c9c4c63df338f7b6a23afcf8e3ff8de' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_939e7af7-7a41-47ce-9891-a43efcc10c6d' | 'ruleset_9b16b913322fe6bb9870c7e2794b7674' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_55af523c2fbc453495f0619c950c0956' | 'lib_bb865c92fcac77fac8cbf5cfd476228d' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_b662511e-1a41-4e7a-90ab-0305531d6a24' | 'ruleset_5673cfa211bbafcdfd5f1f21235e2ac4' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_c6e1c2cc54ca4a4ebd3a1e24267c7d69' | 'lib_6c9c4c63df338f7b6a23afcf8e3ff8de' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_55af523c2fbc453495f0619c950c0956' | 'lib_bb865c92fcac77fac8cbf5cfd476228d' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_c6e1c2cc54ca4a4ebd3a1e24267c7d69' | 'lib_6c9c4c63df338f7b6a23afcf8e3ff8de' |
| `files.shared/rules/by-id/230efccdac3f4d5dcb9b11fb1ca4865f01fc3c6dd8cf768450cba0d317c1bc11.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/2e15176794787847004426c53d13f357369c9de12cfc5c23195886f6fb0ff701.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/78a7870bfedf90fc4e33319910a4c17c1fd83fb7fdd422f2983c0b256b003d10.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/d1d421331f0d610f1d0f2ce98e5d6db6fb0354fe60771e1057d1bf06917c596d.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-B02 / before-MATCH/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_f58bf67ca57643229bfbb45ab7653f59' | 'lib_cb3bc950c06439694fb59d8d890bfdff' |
| `body.candidates[3].id` | value | 'lib_5e2e6d10073c4dad85b7f6168c221522' | 'lib_1c1228910be45e92aadc9f88c6af50a0' |
| `body.direct.id` | value | 'lib_f58bf67ca57643229bfbb45ab7653f59' | 'lib_cb3bc950c06439694fb59d8d890bfdff' |
| `body.direct.references[0].id` | value | 'ruleset_8beff804-0ad0-4df9-81b8-682a036a0d41' | 'ruleset_1e1f187bc2b4b3b602ff0217e3e2c98f' |
| `body.proxy.id` | value | 'lib_5e2e6d10073c4dad85b7f6168c221522' | 'lib_1c1228910be45e92aadc9f88c6af50a0' |
| `body.proxy.references[0].id` | value | 'ruleset_f3b070ff-b69a-4bdf-bb1d-f2a242ea925e' | 'ruleset_a80dd8620e1617780fcffc52d96c1690' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_f58bf67ca57643229bfbb45ab7653f59' | 'lib_cb3bc950c06439694fb59d8d890bfdff' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_5e2e6d10073c4dad85b7f6168c221522' | 'lib_1c1228910be45e92aadc9f88c6af50a0' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_8beff804-0ad0-4df9-81b8-682a036a0d41' | 'ruleset_1e1f187bc2b4b3b602ff0217e3e2c98f' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_f58bf67ca57643229bfbb45ab7653f59' | 'lib_cb3bc950c06439694fb59d8d890bfdff' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_f3b070ff-b69a-4bdf-bb1d-f2a242ea925e' | 'ruleset_a80dd8620e1617780fcffc52d96c1690' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_5e2e6d10073c4dad85b7f6168c221522' | 'lib_1c1228910be45e92aadc9f88c6af50a0' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_f58bf67ca57643229bfbb45ab7653f59' | 'lib_cb3bc950c06439694fb59d8d890bfdff' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_5e2e6d10073c4dad85b7f6168c221522' | 'lib_1c1228910be45e92aadc9f88c6af50a0' |
| `files.shared/rules/by-id/3dbe33fd8d758ef78f58ada95f007e1fe7247e5b2b85a36599ac7e42bd2f6fe8.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/6d703b32998776c799a98a8e89a722072943dc5f09ba75063837458bff25daa3.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/c9bf4438b3e6bc80731f1033933cf91295361f8ed2ef9d47814ba8c047bec6ea.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/d84c6d8797a7e86b2e53bcbf9e17f87e479b2994715c06ac20fdcbdecdae2378.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S01 / direct/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_5cdb167ae6b44c208f2731371f7bfc49' | 'lib_544170718438d233e0cfbbabcc171b47' |
| `body.direct.id` | value | 'lib_5cdb167ae6b44c208f2731371f7bfc49' | 'lib_544170718438d233e0cfbbabcc171b47' |
| `body.direct.references[0].id` | value | 'ruleset_85b6f8cc-0a50-426c-9f7b-a2873e6e5cbf' | 'ruleset_bb2dea0b5c85d05c6d3de2a82bbc2d6a' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_5cdb167ae6b44c208f2731371f7bfc49' | 'lib_544170718438d233e0cfbbabcc171b47' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_85b6f8cc-0a50-426c-9f7b-a2873e6e5cbf' | 'ruleset_bb2dea0b5c85d05c6d3de2a82bbc2d6a' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_5cdb167ae6b44c208f2731371f7bfc49' | 'lib_544170718438d233e0cfbbabcc171b47' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_5cdb167ae6b44c208f2731371f7bfc49' | 'lib_544170718438d233e0cfbbabcc171b47' |
| `files.shared/rules/by-id/80d31aed2d62558d2c7b7a2bcca848b72fdf324eb617c2d6d52c30ff436088aa.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/e09dc3a4fdab80f211c288167c78945fe668b2fc1ca60838e2c651759c0cef12.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S01 / duplicate-targets/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_b0e60965ff064d7fb221307c7467b350' | 'lib_3ed7ec13dbb03e339516ef39703deaa4' |
| `body.candidates[3].id` | value | 'lib_9a73b055bc5d441ebe9ffbbc31a8f767' | 'lib_8cb038a82c4a8d5d9cae9b6c851fe1b7' |
| `body.direct.id` | value | 'lib_9a73b055bc5d441ebe9ffbbc31a8f767' | 'lib_8cb038a82c4a8d5d9cae9b6c851fe1b7' |
| `body.direct.references[0].id` | value | 'ruleset_3554ad4f-8dff-4f39-b8e6-a7ae1800bd7e' | 'ruleset_0d40d05d713b76a13c59d7bf081ead69' |
| `body.direct.references[1].id` | value | 'ruleset_955769a0-ed8e-48e2-abca-4915d44c8c03' | 'ruleset_010d807b3eb4fbe9fff0081de76fdb2e' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_9a73b055bc5d441ebe9ffbbc31a8f767' | 'lib_8cb038a82c4a8d5d9cae9b6c851fe1b7' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_3554ad4f-8dff-4f39-b8e6-a7ae1800bd7e' | 'ruleset_0d40d05d713b76a13c59d7bf081ead69' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_9a73b055bc5d441ebe9ffbbc31a8f767' | 'lib_8cb038a82c4a8d5d9cae9b6c851fe1b7' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_955769a0-ed8e-48e2-abca-4915d44c8c03' | 'ruleset_010d807b3eb4fbe9fff0081de76fdb2e' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_9a73b055bc5d441ebe9ffbbc31a8f767' | 'lib_8cb038a82c4a8d5d9cae9b6c851fe1b7' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_b0e60965ff064d7fb221307c7467b350' | 'lib_3ed7ec13dbb03e339516ef39703deaa4' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_9a73b055bc5d441ebe9ffbbc31a8f767' | 'lib_8cb038a82c4a8d5d9cae9b6c851fe1b7' |
| `files.shared/rules/by-id/2593bc5400448b757a0c83752f0ddde1850afb47e77b43802ecf769d2fc10f08.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/5bc825d936d00dd0c3a12752ebc7caf4bb1e467e45fa0213bf87fc8e93022db8.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/5bc825d936d00dd0c3a12752ebc7caf4bb1e467e45fa0213bf87fc8e93022db8.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S02 / proxy/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_961a18eb71f944d18673a80b9e9c6bd3' | 'lib_470e6958f10a8a9a01132bc07dbcf0b0' |
| `body.proxy.id` | value | 'lib_961a18eb71f944d18673a80b9e9c6bd3' | 'lib_470e6958f10a8a9a01132bc07dbcf0b0' |
| `body.proxy.references[0].id` | value | 'ruleset_8395c407-d1d6-4657-8d2b-ad6bd03fcbad' | 'ruleset_d76cc8e089773bb98983717fb1af00bb' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_961a18eb71f944d18673a80b9e9c6bd3' | 'lib_470e6958f10a8a9a01132bc07dbcf0b0' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_8395c407-d1d6-4657-8d2b-ad6bd03fcbad' | 'ruleset_d76cc8e089773bb98983717fb1af00bb' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_961a18eb71f944d18673a80b9e9c6bd3' | 'lib_470e6958f10a8a9a01132bc07dbcf0b0' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_961a18eb71f944d18673a80b9e9c6bd3' | 'lib_470e6958f10a8a9a01132bc07dbcf0b0' |
| `files.shared/rules/by-id/447193db2037faacd53b3d7aed5e8af0e837370f882ba0e3c0a33a8b364ca163.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/79cda24be7fc43f1450b0eb7c71ff401413512dafd4bd094fbf838001c077e2d.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S03 / both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_fe1f818b8edc47a89e126c2f587b78d0' | 'lib_681ae1b42f84a3d88fd6cc69d504ef27' |
| `body.candidates[3].id` | value | 'lib_31b7ed5518154c73b1007b171f0d3038' | 'lib_0c2f0a84dee8b8653e1e7ed25e98bf23' |
| `body.direct.id` | value | 'lib_fe1f818b8edc47a89e126c2f587b78d0' | 'lib_681ae1b42f84a3d88fd6cc69d504ef27' |
| `body.direct.references[0].id` | value | 'ruleset_852de108-a20b-4216-9bab-46bce404c215' | 'ruleset_8d07d704cf0327c86f8027459bfdfe57' |
| `body.proxy.id` | value | 'lib_31b7ed5518154c73b1007b171f0d3038' | 'lib_0c2f0a84dee8b8653e1e7ed25e98bf23' |
| `body.proxy.references[0].id` | value | 'ruleset_5d8bfc8d-ff8c-48f6-bb72-3315edf21278' | 'ruleset_f75f93717de0d175a06204f8078f46ec' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_fe1f818b8edc47a89e126c2f587b78d0' | 'lib_681ae1b42f84a3d88fd6cc69d504ef27' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_31b7ed5518154c73b1007b171f0d3038' | 'lib_0c2f0a84dee8b8653e1e7ed25e98bf23' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_852de108-a20b-4216-9bab-46bce404c215' | 'ruleset_8d07d704cf0327c86f8027459bfdfe57' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_fe1f818b8edc47a89e126c2f587b78d0' | 'lib_681ae1b42f84a3d88fd6cc69d504ef27' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_5d8bfc8d-ff8c-48f6-bb72-3315edf21278' | 'ruleset_f75f93717de0d175a06204f8078f46ec' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_31b7ed5518154c73b1007b171f0d3038' | 'lib_0c2f0a84dee8b8653e1e7ed25e98bf23' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_fe1f818b8edc47a89e126c2f587b78d0' | 'lib_681ae1b42f84a3d88fd6cc69d504ef27' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_31b7ed5518154c73b1007b171f0d3038' | 'lib_0c2f0a84dee8b8653e1e7ed25e98bf23' |
| `files.shared/rules/by-id/1a416f3146cfb673d0934d9db77efdfa987ff3c0d152dd072c05b5b8d4f43517.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/7781ca73b0e9290a24eaf6dbf93fb2a041530a2372982c44bc354ac466369dac.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/aef7b268060b3018428921726b849ff29560a3a20a189394dcc25cd83172c4d1.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/dfcfff3f77cb7150c8db5722ea50ba0b946654cce2cb6d220f2b91b0013faa32.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S03 / empty-means-both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_508a12b92a234ae1989cd7e4301cac2d' | 'lib_43408e537f8781b6b915ef84f1c5388e' |
| `body.candidates[3].id` | value | 'lib_971216d370094ba8b21de727d65ffbbc' | 'lib_f5ada77f6acd9626969dedd6bc1bae96' |
| `body.direct.id` | value | 'lib_508a12b92a234ae1989cd7e4301cac2d' | 'lib_43408e537f8781b6b915ef84f1c5388e' |
| `body.direct.references[0].id` | value | 'ruleset_dbf0182d-7751-411c-878b-d399be035d9a' | 'ruleset_285b606c64e82446ee6f50d1219a21dc' |
| `body.proxy.id` | value | 'lib_971216d370094ba8b21de727d65ffbbc' | 'lib_f5ada77f6acd9626969dedd6bc1bae96' |
| `body.proxy.references[0].id` | value | 'ruleset_f3fcfd8c-d44c-4b33-a9ab-5f1759ff26db' | 'ruleset_ab3847e9ec90696158cb9259d1959a9f' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_508a12b92a234ae1989cd7e4301cac2d' | 'lib_43408e537f8781b6b915ef84f1c5388e' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_971216d370094ba8b21de727d65ffbbc' | 'lib_f5ada77f6acd9626969dedd6bc1bae96' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_dbf0182d-7751-411c-878b-d399be035d9a' | 'ruleset_285b606c64e82446ee6f50d1219a21dc' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_508a12b92a234ae1989cd7e4301cac2d' | 'lib_43408e537f8781b6b915ef84f1c5388e' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_f3fcfd8c-d44c-4b33-a9ab-5f1759ff26db' | 'ruleset_ab3847e9ec90696158cb9259d1959a9f' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_971216d370094ba8b21de727d65ffbbc' | 'lib_f5ada77f6acd9626969dedd6bc1bae96' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_508a12b92a234ae1989cd7e4301cac2d' | 'lib_43408e537f8781b6b915ef84f1c5388e' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_971216d370094ba8b21de727d65ffbbc' | 'lib_f5ada77f6acd9626969dedd6bc1bae96' |
| `files.shared/rules/by-id/0920bb317062d91407531942aea863aa9db3bb35497836b4de40cfb342a2fd34.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/452b48975bf9471c31645f215af2633f6da8ceadce5d1bafc7bff7e5d2c226ff.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/687887f9be03b86d44b2b887233990c0a60bf78cef3dd8c8664af30a3741cb95.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/6de9dba9986a86b5bf68bb6a093f5a42f0b0036c50ddfbbad477094801cc7320.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S04 / repeat/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_c28718330be149bbb23c82f4d5745e80' | 'lib_173098d2be55bcf27f6f3635f4ab051c' |
| `body.direct.id` | value | 'lib_c28718330be149bbb23c82f4d5745e80' | 'lib_173098d2be55bcf27f6f3635f4ab051c' |
| `body.direct.references[0].id` | value | 'ruleset_3f31dc30-04c0-4f89-846b-6d98bb5973fb' | 'ruleset_b5c0ae8f568735155502373b806c0e1f' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_c28718330be149bbb23c82f4d5745e80' | 'lib_173098d2be55bcf27f6f3635f4ab051c' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_3f31dc30-04c0-4f89-846b-6d98bb5973fb' | 'ruleset_b5c0ae8f568735155502373b806c0e1f' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_c28718330be149bbb23c82f4d5745e80' | 'lib_173098d2be55bcf27f6f3635f4ab051c' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_c28718330be149bbb23c82f4d5745e80' | 'lib_173098d2be55bcf27f6f3635f4ab051c' |
| `files.shared/rules/by-id/a2ce710c2beea4d9a514e292798392e646dacf537c5df5dcbf74517d87dc28eb.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/f2b20de09e7ccd0809f83b696c3d51f5d4719a211bf5f8c153788bb2df770692.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S04 / repeat/1

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_c28718330be149bbb23c82f4d5745e80' | 'lib_173098d2be55bcf27f6f3635f4ab051c' |
| `body.candidates[3].id` | value | 'lib_d8ee003c0e6c47fbb1ed4ab3b5dad7b9' | 'lib_0429c03129f3e725a52c542c149d6bf4' |
| `body.direct.id` | value | 'lib_d8ee003c0e6c47fbb1ed4ab3b5dad7b9' | 'lib_0429c03129f3e725a52c542c149d6bf4' |
| `body.direct.references[0].id` | value | 'ruleset_bfadb262-6f10-461d-b894-455e9b1fdbb7' | 'ruleset_a8b2b1244acad44206e929dd06cca59d' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_d8ee003c0e6c47fbb1ed4ab3b5dad7b9' | 'lib_0429c03129f3e725a52c542c149d6bf4' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_3f31dc30-04c0-4f89-846b-6d98bb5973fb' | 'ruleset_b5c0ae8f568735155502373b806c0e1f' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_c28718330be149bbb23c82f4d5745e80' | 'lib_173098d2be55bcf27f6f3635f4ab051c' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_bfadb262-6f10-461d-b894-455e9b1fdbb7' | 'ruleset_a8b2b1244acad44206e929dd06cca59d' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_d8ee003c0e6c47fbb1ed4ab3b5dad7b9' | 'lib_0429c03129f3e725a52c542c149d6bf4' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_c28718330be149bbb23c82f4d5745e80' | 'lib_173098d2be55bcf27f6f3635f4ab051c' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_d8ee003c0e6c47fbb1ed4ab3b5dad7b9' | 'lib_0429c03129f3e725a52c542c149d6bf4' |
| `files.shared/rules/by-id/108578b3f5436ba9c67f406801f4a28a8ffd34abef4191c461ed6dcc29da7a53.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/a2ce710c2beea4d9a514e292798392e646dacf537c5df5dcbf74517d87dc28eb.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/a33317b99863f424f57ddceab07ec9ba40a32fa0bd8fbb7f4ebd944b51555432.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/f2b20de09e7ccd0809f83b696c3d51f5d4719a211bf5f8c153788bb2df770692.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## X04 / POST /api/nodes/parse-lines/content-type/bom

路由：`POST /api/nodes/parse-lines`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 200 | 400 |
| `body.message` | extra-in-go | None | 'lines 必须是字符串数组' |
| `body.results` | missing-in-go | [] | None |
| `body.success` | value | True | False |

## R103-F14 /

路由：`POST /api/nodes/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 404 |
| `body.message` | non-ascii-internal-error-text | "'list' object has no attribute 'get'" | "以下节点 id 不存在: ['nodes 必须是对象数组']" |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T00:17:25Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-17T00:17:25.649815+00:00', 'first_seen_at': '2026-11-17T00:17:25.649815+00:00', 'last_seen_at': '2026-11-17T00:17:25Z', 'occurre | None |

## R218-F11 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests` | length | 0 | 1 |
| `status` | value | 500 | 200 |
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'added': 2, 'cached_count': 2, 'cached_updated_at': '2026-11-17T00:17:28.557454+00:00', 'count': 2, 'fetch_error': None, 'from_cache': False, 'nodes': [{'id': 'node_3ca16126', 'name': '甲🧪', 'params': {}, 'port': 1080, 'server': '192.0.2.1', 'subscription_id': 'sub-a', 'subscription_name': '订阅甲🧪', 'type': 'socks5'}, {'id': 'node_859c03b7', 'name': '乙', 'params': {}, 'port': 1081, 'server': '192.0.2.2', 'subscription_id': 'sub-a', 'subscription_name': '订阅甲🧪', 'type': 'socks5'}], 'success': True, 'total_count': 2} |
| `db.meta.commit_id` | unstable-Go-commit | 'b249363c8df44983bf11c5133443c3e9' | 'f962adc18a4546ba923e3c6c4c41e4c0' |
| `db.fields.shared//_revision.revision` | value | 2 | 5 |
| `db.fields.shared//_revision.value` | value | 1 | 2 |
| `db.fields.shared//nodes.revision` | value | 1 | 5 |
| `db.fields.shared//nodes.value` | length | 0 | 2 |
| `db.meta.commit_id` | value | 'b249363c8df44983bf11c5133443c3e9' | 'f962adc18a4546ba923e3c6c4c41e4c0' |
| `db.meta.generation` | value | 4 | 5 |
| `files.shared/subscribes/sub-a.json` | extra-in-go | None | {'mode': 384, 'content': {'subscription_id': 'sub-a', 'updated_at': '2026-11-17T00:17:28.557454+00:00', 'count': 2, 'nodes': [{'name': '甲🧪', 'type': 'socks5', 'server': '192.0.2.1', 'port': 1080, 'params': {}, 'subscription_id': 'sub-a', 'subscription_name': '订阅甲🧪', 'id': 'node_3ca16126'}, {'name': '乙', 'type': 'socks5', 'server': '192.0.2.2', 'port': 1081, 'params': {}, 'subscription_id': 'sub-a', 'subscription_name': '订阅甲🧪', 'id': 'node_859c03b7'}], 'metadata': {'subscription_name': '订阅甲🧪', 'url': 'http://127.0.0.1:41395/feed', 'fetched_at': '2026-11-17T00:17:28.557454+00:00'}}} |
| `files.shared/subscription-downloads/ab069242ce287e2f4f2669d407f42618630eb2f74f63222e114df4dd52ff9b56.json` | extra-in-go | None | {'mode': 384, 'content': {'text': 'proxies:\n  - {name: 甲🧪, type: socks5, server: 192.0.2.1, port: 1080}\n  - {name: 乙, type: socks5, server: 192.0.2.2, port: 1081}\n', 'fetched_at': '2026-11-17T00:17:28.557454+00:00', 'traffic': None}} |
| `files.shared/subscription_health.json` | extra-in-go | None | {'mode': 384, 'content': {'sub-a': {'history': [{'at': '2026-11-17T00:17:28.557771Z', 'status': 'ok', 'count': 2}]}}} |
| `files.task-status.json` | extra-in-go | None | {'mode': 384, 'content': {'subscription:sub-a': {'last_run_at': '2026-11-17T00:17:28Z', 'last_finished_at': '2026-11-17T00:17:28Z', 'last_status': 'success', 'last_duration_ms': 0, 'last_trigger': 'manual', 'last_error': '', 'run_count': 1, 'last_success_at': '2026-11-17T00:17:28Z', 'consecutive_failures': 0, 'cache_updated_at': '2026-11-17T00:17:28.557454+00:00'}}} |

## R218-F12 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'application/json' | 'text/html; charset=utf-8' |
| `body` | type | {'message': '订阅操作失败', 'success': False} | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '仍使用上一次有效缓存（2026-01-01T00:00:00Z）', 'status_label': '执行失败', 'severity': 'warning', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T00:17:27Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-17T00:17:27.104661+00:00', 'first_seen_at': '2026-11-17T00:17:27.104661+00:00', 'last_seen_at': '202 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '仍使用上一次有效缓存（2026-01-01T00:00:00Z）', 'status_label': '执行失败', 'severity': 'warning', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T00:17:24Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-17T00:17:24.208391+00:00', 'first_seen_at': '2026-11-17T00:17:24.208391+00:00', 'last_seen_at': '202 | None |

## R223-F12 /

路由：`POST /api/subscriptions/reorder`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `status` | value | 500 | 404 |
| `body.message` | value | '订阅操作失败' | "以下订阅 id 不存在: ['subscriptions 必须是对象数组']" |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:backoff': {'source': 'task', 'title': '合成-backoff · 订阅更新', 'message': '任务未完成，请查看任务详情。', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'backoff', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-17T00:03:09Z', 'failure_count': 3, 'id': 'task:subscription:backoff', 'state': 'pending', 'read': False, 'version': '2026-11-17T00:11:59.650273+00:00', 'first_seen_at': '2026-11-17T00:11:59.650273+00:00', 'last_seen_at': '2026-11-17T00:03:09Z', ' | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'notice:node-test-dns-managed-removed': {'source': 'notice', 'scope': 'system', 'severity': 'warning', 'status_label': '已自动调整', 'title': '节点测速 DNS 已自动调整', 'message': '节点测速的「OxiDNS 托管」模式已移除。测速 DNS 已改为自定义：https://dns.example/dns-query。原专用入口 192.0.2.53:15353 已从 OxiDNS 配置中去掉，「合成 Agent 🧪」上的监听在下一次发布 OxiDNS 后关闭。', 'impact': '', 'checked_at': '2026-11-17T00:09:26.537479+00:00', 'target': {'path': '/nodes', 'query': {}, 'label': '查看测速设置'}, 'id': 'notice:node-test-dns-managed-removed', 'state': 'pending', 'read': False, 'version': '2026-11-17T00:09:26.537530+00:00', 'first_seen_at': '2026-11-17T00:09:26.537530+00:00' | None |

## S91 CONNECT 传输观察（待兼容性审查）

仅上述精确字段组合的用例：R087-S91、R138-S91。

两端都先经fake代理尝试相同GitHub原站CONNECT，拒绝后再GET相同镜像URL；最终200、转换正文与存储一致。记录到的差别是Python使用HTTP/1.0且无Host/User-Agent，Go使用HTTP/1.1及上述标准字段。这里保留原始线级差异供审核，不据此要求Go模仿Python传输栈；没有归一化或吞掉字段。若目标、方法、认证、次数、正文或存储还存在其他差异，不会归入本说明。

## Oracle 限制：混合目标采纳的顺序

路由：`POST /api/domain-discovery/apply`；用例：R046-S03；类型：Python 集合遍历顺序不稳定。

未固定 hash seed 的首轮合成请求中，Python added.target 为 `[proxy, direct]`，Go 为 `[direct, proxy]`，同一批 history 顺序也不同；每个目标的内容一致。Python 来源为 `backend/utils/domain_discovery_service.py:567` 的 target 集合遍历。

当前独立用例固定 `PYTHONHASHSEED=0`，保留原始 HTTP 数组与数据库 history 顺序比较；该环境下实际比较通过。其他 Python hash 环境下的顺序不作通过保证，留作部分覆盖/审查项，没有对数组排序以吞掉差异。普通多次写入之间等待下一秒，避免秒级时间戳产生不同的排序平局。
