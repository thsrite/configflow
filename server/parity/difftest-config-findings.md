# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

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

## T16 / enable-immediate-final

路由：`POST /api/backup/config`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `dav.objects./backup/config_backup_20261116_182225_821400.json` | missing-in-go | {'schema_version': 5, 'system': {'system_config': {'server_domain': 'http://panel.example', 'github_proxy_domain': '', 'rule_fetch_proxy': '', 'retired_rule_proxy_tokens': ['synthetic-retired-proxy/token+9876543210'], 'rule_proxy_token': 'synthetic-current-proxy/token+0123456789', 'config_token': ''}, 'backup': {'auto_backup': True, 'webdav_url': 'http://127.0.0.1:43336', 'webdav_username': 'synthetic-dav-user', 'webdav_password': 'synthetic-dav-password', 'webdav_path': '/backup/', 'keep_latest': 0, 'keep_daily': 0}, 'agents': [{'id': 'agent-synthetic', 'name': '合成 Agent 🧪', 'token': 'synthetic-agent/token+01234567890123456789', 'profile_id' | None |
| `dav.requests` | length | 3 | 0 |
| `files.task-status.json` | missing-in-go | {'mode': 384, 'content': {'webdav_backup': {'last_run_at': '2026-11-16T18:22:25Z', 'last_finished_at': '2026-11-16T18:22:25Z', 'last_status': 'success', 'last_duration_ms': 12, 'last_trigger': 'event', 'last_error': '', 'run_count': 1, 'last_success_at': '2026-11-16T18:22:25Z', 'consecutive_failures': 0}}} | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '没有可用缓存', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-16T20:00:33Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-16T20:00:33.408234+00:00', 'first_seen_at': '2026-11-16T20:00:33.408234+00:00', 'last_seen_at': '2026-11-16T20:00:33Z', 'occurre | None |

## R218-F11 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `subscription.source.requests` | length | 0 | 1 |
| `status` | value | 500 | 200 |
| `headers.content-type` | value | 'text/html; charset=utf-8' | 'application/json' |
| `body` | type | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] | {'added': 2, 'cached_count': 2, 'cached_updated_at': '2026-11-16T20:00:36.335498+00:00', 'count': 2, 'fetch_error': None, 'from_cache': False, 'nodes': [{'id': 'node_8b611873', 'name': '甲🧪', 'params': {}, 'port': 1080, 'server': '192.0.2.1', 'subscription_id': 'sub-a', 'subscription_name': '订阅甲🧪', 'type': 'socks5'}, {'id': 'node_69421a7e', 'name': '乙', 'params': {}, 'port': 1081, 'server': '192.0.2.2', 'subscription_id': 'sub-a', 'subscription_name': '订阅甲🧪', 'type': 'socks5'}], 'success': True, 'total_count': 2} |
| `db.meta.commit_id` | unstable-Go-commit | 'bf0f3d5bba1b4138b54f4c2681e86b16' | 'f0bcfd2b2e5147e98c04e9211db29ade' |
| `db.fields.shared//_revision.revision` | value | 2 | 5 |
| `db.fields.shared//_revision.value` | value | 1 | 2 |
| `db.fields.shared//nodes.revision` | value | 1 | 5 |
| `db.fields.shared//nodes.value` | length | 0 | 2 |
| `db.meta.commit_id` | value | 'bf0f3d5bba1b4138b54f4c2681e86b16' | 'f0bcfd2b2e5147e98c04e9211db29ade' |
| `db.meta.generation` | value | 4 | 5 |
| `files.shared/subscribes/sub-a.json` | extra-in-go | None | {'mode': 384, 'content': {'subscription_id': 'sub-a', 'updated_at': '2026-11-16T20:00:36.335498+00:00', 'count': 2, 'nodes': [{'name': '甲🧪', 'type': 'socks5', 'server': '192.0.2.1', 'port': 1080, 'params': {}, 'subscription_id': 'sub-a', 'subscription_name': '订阅甲🧪', 'id': 'node_8b611873'}, {'name': '乙', 'type': 'socks5', 'server': '192.0.2.2', 'port': 1081, 'params': {}, 'subscription_id': 'sub-a', 'subscription_name': '订阅甲🧪', 'id': 'node_69421a7e'}], 'metadata': {'subscription_name': '订阅甲🧪', 'url': 'http://127.0.0.1:44232/feed', 'fetched_at': '2026-11-16T20:00:36.335498+00:00'}}} |
| `files.shared/subscription-downloads/5352ec5879f2278d3776fd605c8aaea7065863f9c849f0370ca96f0c254bf31a.json` | extra-in-go | None | {'mode': 384, 'content': {'text': 'proxies:\n  - {name: 甲🧪, type: socks5, server: 192.0.2.1, port: 1080}\n  - {name: 乙, type: socks5, server: 192.0.2.2, port: 1081}\n', 'fetched_at': '2026-11-16T20:00:36.335498+00:00', 'traffic': None}} |
| `files.shared/subscription_health.json` | extra-in-go | None | {'mode': 384, 'content': {'sub-a': {'history': [{'at': '2026-11-16T20:00:36.357250Z', 'status': 'ok', 'count': 2}]}}} |
| `files.task-status.json` | extra-in-go | None | {'mode': 384, 'content': {'subscription:sub-a': {'last_run_at': '2026-11-16T20:00:36Z', 'last_finished_at': '2026-11-16T20:00:36Z', 'last_status': 'success', 'last_duration_ms': 32, 'last_trigger': 'manual', 'last_error': '', 'run_count': 1, 'last_success_at': '2026-11-16T20:00:36Z', 'consecutive_failures': 0, 'cache_updated_at': '2026-11-16T20:00:36.335498+00:00'}}} |

## R218-F12 /

路由：`POST /api/subscriptions/<sub_id>/fetch`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `headers.content-type` | value | 'application/json' | 'text/html; charset=utf-8' |
| `body` | type | {'message': '订阅操作失败', 'success': False} | ['<!doctype html>', '<html lang=en>', '<title>500 Internal Server Error</title>', '<h1>Internal Server Error</h1>', '<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>'] |
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '仍使用上一次有效缓存（2026-01-01T00:00:00Z）', 'status_label': '执行失败', 'severity': 'warning', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-16T20:00:34Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-16T20:00:34.877185+00:00', 'first_seen_at': '2026-11-16T20:00:34.877185+00:00', 'last_seen_at': '202 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:sub-a': {'source': 'task', 'title': '订阅甲🧪 · 订阅更新', 'message': '上游返回 HTTP 403', 'impact': '仍使用上一次有效缓存（2026-01-01T00:00:00Z）', 'status_label': '执行失败', 'severity': 'warning', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'sub-a', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-16T20:00:31Z', 'failure_count': 1, 'id': 'task:subscription:sub-a', 'state': 'pending', 'read': False, 'version': '2026-11-16T20:00:31.940542+00:00', 'first_seen_at': '2026-11-16T20:00:31.940542+00:00', 'last_seen_at': '202 | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'task:subscription:backoff': {'source': 'task', 'title': '合成-backoff · 订阅更新', 'message': '任务未完成，请查看任务详情。', 'impact': '', 'status_label': '执行失败', 'severity': 'error', 'profile_id': None, 'scope': 'shared', 'profile_name': None, 'target': {'path': '/subscriptions', 'query': {'subscription': 'backoff', 'section': 'settings'}, 'label': '检查订阅设置'}, 'checked_at': '2026-11-16T18:30:49Z', 'failure_count': 3, 'id': 'task:subscription:backoff', 'state': 'pending', 'read': False, 'version': '2026-11-16T18:39:38.869388+00:00', 'first_seen_at': '2026-11-16T18:39:38.869388+00:00', 'last_seen_at': '2026-11-16T18:30:49Z', ' | None |

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
| `files.notifications.json` | missing-in-go | {'mode': 384, 'content': {'items': {'notice:node-test-dns-managed-removed': {'source': 'notice', 'scope': 'system', 'severity': 'warning', 'status_label': '已自动调整', 'title': '节点测速 DNS 已自动调整', 'message': '节点测速的「OxiDNS 托管」模式已移除。测速 DNS 已改为自定义：https://dns.example/dns-query。原专用入口 192.0.2.53:15353 已从 OxiDNS 配置中去掉，「合成 Agent 🧪」上的监听在下一次发布 OxiDNS 后关闭。', 'impact': '', 'checked_at': '2026-11-16T18:21:12.517498+00:00', 'target': {'path': '/nodes', 'query': {}, 'label': '查看测速设置'}, 'id': 'notice:node-test-dns-managed-removed', 'state': 'pending', 'read': False, 'version': '2026-11-16T18:21:12.517541+00:00', 'first_seen_at': '2026-11-16T18:21:12.517541+00:00' | None |

## Oracle 限制：混合目标采纳的顺序

路由：`POST /api/domain-discovery/apply`；用例：R046-S03；类型：Python 集合遍历顺序不稳定。

未固定 hash seed 的首轮合成请求中，Python added.target 为 `[proxy, direct]`，Go 为 `[direct, proxy]`，同一批 history 顺序也不同；每个目标的内容一致。Python 来源为 `backend/utils/domain_discovery_service.py:567` 的 target 集合遍历。

当前独立用例固定 `PYTHONHASHSEED=0`，保留原始 HTTP 数组与数据库 history 顺序比较；该环境下实际比较通过。其他 Python hash 环境下的顺序不作通过保证，留作部分覆盖/审查项，没有对数组排序以吞掉差异。普通多次写入之间等待下一秒，避免秒级时间戳产生不同的排序平局。
