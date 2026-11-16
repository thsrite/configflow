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

## R055-B01 / disabled/domain/example.com

路由：`POST /api/domain-discovery/region/route`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_7cf664bc-d60c-4ed1-92cb-dbe875a45b0e' | 'ruleset_2ceb3caaa3c0d4424e27476d19a6548d' |

## R055-S02 / new/domain/example.com

路由：`POST /api/domain-discovery/region/route`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.added[0].ruleset_id` | value | 'lib_52b75d6f15fc4cc5b2fa09d675318e37' | 'lib_95e5c54a38304179905b8c31e34edc48' |
| `db.fields.profile/default/domain_discovery.value.history[0].ruleset_id` | value | 'lib_52b75d6f15fc4cc5b2fa09d675318e37' | 'lib_95e5c54a38304179905b8c31e34edc48' |
| `db.fields.profile/default/domain_discovery.value.regional_rulesets.代理🧪` | value | 'lib_52b75d6f15fc4cc5b2fa09d675318e37' | 'lib_95e5c54a38304179905b8c31e34edc48' |
| `db.fields.profile/default/rule_configs.value[1].id` | value | 'ruleset_faf6a670-8d8f-4db9-85ca-365c87eef38a' | 'ruleset_3c6bf050289794dd656afc99ea52e084' |
| `db.fields.profile/default/rule_configs.value[1].library_rule_id` | value | 'lib_52b75d6f15fc4cc5b2fa09d675318e37' | 'lib_95e5c54a38304179905b8c31e34edc48' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_52b75d6f15fc4cc5b2fa09d675318e37' | 'lib_95e5c54a38304179905b8c31e34edc48' |
| `files.shared/rules/by-id/6f08e1cf57a4cdd68afc0029e57a5d8ffb0646d0205c2ee872f061cad4ba4813.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 26, 'bytes_sha256': '145f7fd7beed2e748e7cfadb10fa168d309267d703f0ec09358ede2e8081bc9a', 'bytes_prefix_hex': '444f4d41494e2d5355464649582c6578616d706c652e636f6d0a'}} | None |
| `files.shared/rules/by-id/6f08e1cf57a4cdd68afc0029e57a5d8ffb0646d0205c2ee872f061cad4ba4813.list.bak` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/d8eda47d636884475dce1ba4755ff11abdf97547ec293d9579cd625347943042.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 26, 'bytes_sha256': '145f7fd7beed2e748e7cfadb10fa168d309267d703f0ec09358ede2e8081bc9a', 'bytes_prefix_hex': '444f4d41494e2d5355464649582c6578616d706c652e636f6d0a'}} |
| `files.shared/rules/by-id/d8eda47d636884475dce1ba4755ff11abdf97547ec293d9579cd625347943042.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-B01 / name-collision/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_4b44ceb89b6e4de3ba048722edbaacd1' | 'lib_8ec278683639a7768bf5d591fa208e60' |
| `body.candidates[3].id` | value | 'lib_2c3c9df504c14667b074ec2a80ea12af' | 'lib_349c8bc05ed94c8cbfe7a1f706bb2527' |
| `body.direct.id` | value | 'lib_4b44ceb89b6e4de3ba048722edbaacd1' | 'lib_8ec278683639a7768bf5d591fa208e60' |
| `body.direct.references[0].id` | value | 'ruleset_c8a1c8aa-0740-4764-ac2c-f5f69304fead' | 'ruleset_8e90b0a49257287b586320c93db03caa' |
| `body.proxy.id` | value | 'lib_2c3c9df504c14667b074ec2a80ea12af' | 'lib_349c8bc05ed94c8cbfe7a1f706bb2527' |
| `body.proxy.references[0].id` | value | 'ruleset_8cdc9e63-753c-48fb-8d2c-a67b54e3091c' | 'ruleset_23f4bc76ac5915fcce27d0ec715ba707' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_4b44ceb89b6e4de3ba048722edbaacd1' | 'lib_8ec278683639a7768bf5d591fa208e60' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_2c3c9df504c14667b074ec2a80ea12af' | 'lib_349c8bc05ed94c8cbfe7a1f706bb2527' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_c8a1c8aa-0740-4764-ac2c-f5f69304fead' | 'ruleset_8e90b0a49257287b586320c93db03caa' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_4b44ceb89b6e4de3ba048722edbaacd1' | 'lib_8ec278683639a7768bf5d591fa208e60' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_8cdc9e63-753c-48fb-8d2c-a67b54e3091c' | 'ruleset_23f4bc76ac5915fcce27d0ec715ba707' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_2c3c9df504c14667b074ec2a80ea12af' | 'lib_349c8bc05ed94c8cbfe7a1f706bb2527' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_4b44ceb89b6e4de3ba048722edbaacd1' | 'lib_8ec278683639a7768bf5d591fa208e60' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_2c3c9df504c14667b074ec2a80ea12af' | 'lib_349c8bc05ed94c8cbfe7a1f706bb2527' |
| `files.shared/rules/by-id/173f373d387f9d7b1e62c80867c96a5efb2a62af991745bb41e2fae04532b33c.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/bc7f21e23dd863e88c7cf1d697c33228e6e20d37637986605db055b8189da82f.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/d4725338ac477ff6a5ba5e4064d94bd54273d0b40e45ed131a397cb0312fe633.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/f1a0620b589e03db8ca2948f4e14526ce99951a1e56189996902ac76fb19a346.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-B02 / before-MATCH/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_23ddef54f9244b5aafd92ec90c753838' | 'lib_030e0da737082710a6be9d144ac733c2' |
| `body.candidates[3].id` | value | 'lib_2db72f03c7864cceb6179c667147a044' | 'lib_6f1f6928087fff96d81bd4b825512ce6' |
| `body.direct.id` | value | 'lib_23ddef54f9244b5aafd92ec90c753838' | 'lib_030e0da737082710a6be9d144ac733c2' |
| `body.direct.references[0].id` | value | 'ruleset_5dbd3ceb-31da-458d-b9f6-9f04e553d8b8' | 'ruleset_67b5facbba9c94244392540e4cf0b99c' |
| `body.proxy.id` | value | 'lib_2db72f03c7864cceb6179c667147a044' | 'lib_6f1f6928087fff96d81bd4b825512ce6' |
| `body.proxy.references[0].id` | value | 'ruleset_b4264d83-11ac-4500-b3dc-5af44c1839dc' | 'ruleset_37bdc358133549daafd5a6e1af2c5a38' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_23ddef54f9244b5aafd92ec90c753838' | 'lib_030e0da737082710a6be9d144ac733c2' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_2db72f03c7864cceb6179c667147a044' | 'lib_6f1f6928087fff96d81bd4b825512ce6' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_5dbd3ceb-31da-458d-b9f6-9f04e553d8b8' | 'ruleset_67b5facbba9c94244392540e4cf0b99c' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_23ddef54f9244b5aafd92ec90c753838' | 'lib_030e0da737082710a6be9d144ac733c2' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_b4264d83-11ac-4500-b3dc-5af44c1839dc' | 'ruleset_37bdc358133549daafd5a6e1af2c5a38' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_2db72f03c7864cceb6179c667147a044' | 'lib_6f1f6928087fff96d81bd4b825512ce6' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_23ddef54f9244b5aafd92ec90c753838' | 'lib_030e0da737082710a6be9d144ac733c2' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_2db72f03c7864cceb6179c667147a044' | 'lib_6f1f6928087fff96d81bd4b825512ce6' |
| `files.shared/rules/by-id/2cb5e8f4993c3006df713046966d3b576ff5e579ae042cb441ffa4530b7545a5.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/82e48bdfe208fcd77cc4bb228e7545fcd083accaca971d31025a914cf4e93aeb.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/9888fcefb389aad534c71d1110e7335f7be6e2355d3f3823a7268372da4f25e6.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/a116be254d76a24174e30577c159e1898e23189905021fc51f1ee3d45e5f6907.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S01 / direct/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_393868e7ebff42a5b9f9c1d68e6db3ec' | 'lib_3c97d82ab60d489f2a9ea261ad8c6464' |
| `body.direct.id` | value | 'lib_393868e7ebff42a5b9f9c1d68e6db3ec' | 'lib_3c97d82ab60d489f2a9ea261ad8c6464' |
| `body.direct.references[0].id` | value | 'ruleset_1d61582c-c6bd-4d32-87d0-52f9457425ba' | 'ruleset_4b97d38c11eb4d0988978f4dda446f03' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_393868e7ebff42a5b9f9c1d68e6db3ec' | 'lib_3c97d82ab60d489f2a9ea261ad8c6464' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_1d61582c-c6bd-4d32-87d0-52f9457425ba' | 'ruleset_4b97d38c11eb4d0988978f4dda446f03' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_393868e7ebff42a5b9f9c1d68e6db3ec' | 'lib_3c97d82ab60d489f2a9ea261ad8c6464' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_393868e7ebff42a5b9f9c1d68e6db3ec' | 'lib_3c97d82ab60d489f2a9ea261ad8c6464' |
| `files.shared/rules/by-id/17530d5511f0649401fe26af46aa524ae46b7913e6ad6a34d2e279572582caf1.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/eb534899a432665209a9e97c49131a0887502196b50d7cfe4c12adfab8ecf4b9.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S01 / duplicate-targets/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_86016a98d6a8439bae160bcfba22093e' | 'lib_890a22b1fdfc466f85bd0fd8efa37d65' |
| `body.candidates[3].id` | value | 'lib_67b0c20da15648ed812b0ce5025bd38f' | 'lib_14342484029dfac60bbc74381bc35aa3' |
| `body.direct.id` | value | 'lib_67b0c20da15648ed812b0ce5025bd38f' | 'lib_14342484029dfac60bbc74381bc35aa3' |
| `body.direct.references[0].id` | value | 'ruleset_2621d32b-3b33-470c-b8b7-30f227fd6a19' | 'ruleset_9f4832012346499279abf5da5ea8fb3e' |
| `body.direct.references[1].id` | value | 'ruleset_e692b4c2-3074-4513-8c7b-5dd80e59749b' | 'ruleset_70c8a594cff57a8dc435eedf90ba403e' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_67b0c20da15648ed812b0ce5025bd38f' | 'lib_14342484029dfac60bbc74381bc35aa3' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_2621d32b-3b33-470c-b8b7-30f227fd6a19' | 'ruleset_9f4832012346499279abf5da5ea8fb3e' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_67b0c20da15648ed812b0ce5025bd38f' | 'lib_14342484029dfac60bbc74381bc35aa3' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_e692b4c2-3074-4513-8c7b-5dd80e59749b' | 'ruleset_70c8a594cff57a8dc435eedf90ba403e' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_67b0c20da15648ed812b0ce5025bd38f' | 'lib_14342484029dfac60bbc74381bc35aa3' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_86016a98d6a8439bae160bcfba22093e' | 'lib_890a22b1fdfc466f85bd0fd8efa37d65' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_67b0c20da15648ed812b0ce5025bd38f' | 'lib_14342484029dfac60bbc74381bc35aa3' |
| `files.shared/rules/by-id/403f9e7cb875aa59e74441df6d9b82c63e771ff49a2724b25812622df4aa7119.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/689913c60b5d133d5f2be262f2ab325e30d8ba3b4b1405dd9e7a852b5b7c46b5.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/689913c60b5d133d5f2be262f2ab325e30d8ba3b4b1405dd9e7a852b5b7c46b5.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S02 / proxy/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_f526f52f3ecf469c9a370cf8bc119967' | 'lib_47e39833bf2e119d504956fb5d9a44f5' |
| `body.proxy.id` | value | 'lib_f526f52f3ecf469c9a370cf8bc119967' | 'lib_47e39833bf2e119d504956fb5d9a44f5' |
| `body.proxy.references[0].id` | value | 'ruleset_a22f9353-1cb6-4aa5-95a0-78e749006d40' | 'ruleset_0367704c3c9b95f765825bec9ba860f1' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_f526f52f3ecf469c9a370cf8bc119967' | 'lib_47e39833bf2e119d504956fb5d9a44f5' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_a22f9353-1cb6-4aa5-95a0-78e749006d40' | 'ruleset_0367704c3c9b95f765825bec9ba860f1' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_f526f52f3ecf469c9a370cf8bc119967' | 'lib_47e39833bf2e119d504956fb5d9a44f5' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_f526f52f3ecf469c9a370cf8bc119967' | 'lib_47e39833bf2e119d504956fb5d9a44f5' |
| `files.shared/rules/by-id/c30223f9ec56f761b9ad6e5ba70e5c9a170b269fd4b218ff81ea86d78fe29c9b.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/cf6edf5f81623607783e4e503b40b3b71e69ed62e0d460de9a91394d06c16d7c.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S03 / both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_8a7c195e866d4849aaeb2254949cbd66' | 'lib_39c61bc837d1703646a4dcba1f75cb3d' |
| `body.candidates[3].id` | value | 'lib_75886159342349c7ad10b6956b2ec2d8' | 'lib_87c1774a0ec9c129482f2276c0968a90' |
| `body.direct.id` | value | 'lib_8a7c195e866d4849aaeb2254949cbd66' | 'lib_39c61bc837d1703646a4dcba1f75cb3d' |
| `body.direct.references[0].id` | value | 'ruleset_24a09805-a297-40a5-97d8-265454326367' | 'ruleset_dde066f6dc1d41c093d28728dc45e60c' |
| `body.proxy.id` | value | 'lib_75886159342349c7ad10b6956b2ec2d8' | 'lib_87c1774a0ec9c129482f2276c0968a90' |
| `body.proxy.references[0].id` | value | 'ruleset_44b8a3a2-a3fd-480f-994c-1813bf6765df' | 'ruleset_541869c14e6e4ca5bf1ed007f1c0f45e' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_8a7c195e866d4849aaeb2254949cbd66' | 'lib_39c61bc837d1703646a4dcba1f75cb3d' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_75886159342349c7ad10b6956b2ec2d8' | 'lib_87c1774a0ec9c129482f2276c0968a90' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_24a09805-a297-40a5-97d8-265454326367' | 'ruleset_dde066f6dc1d41c093d28728dc45e60c' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_8a7c195e866d4849aaeb2254949cbd66' | 'lib_39c61bc837d1703646a4dcba1f75cb3d' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_44b8a3a2-a3fd-480f-994c-1813bf6765df' | 'ruleset_541869c14e6e4ca5bf1ed007f1c0f45e' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_75886159342349c7ad10b6956b2ec2d8' | 'lib_87c1774a0ec9c129482f2276c0968a90' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_8a7c195e866d4849aaeb2254949cbd66' | 'lib_39c61bc837d1703646a4dcba1f75cb3d' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_75886159342349c7ad10b6956b2ec2d8' | 'lib_87c1774a0ec9c129482f2276c0968a90' |
| `files.shared/rules/by-id/1eebe5211f4319363312c14a0960bbbbce57ba4aa1a34393c264c4c54d34ed40.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/bb8a6af072ad45790998b5eb1d4a622b7b21b46f13e7c3aaa2f74554307d81d3.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/ec88ddd450423a871f056598cf6cf2e514c059c8cde055d76f00a387f393c3cf.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/f3a2d828cde37a2ab3944bf8238e58b7acfef1d668c893f4ca42215eb48913eb.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S03 / empty-means-both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_4c04f2e865f84e7aa1891494e802420b' | 'lib_06807196e599340b92f395970c706dfc' |
| `body.candidates[3].id` | value | 'lib_d5214a184d674a17a6779218975d04a5' | 'lib_d3cc09997a916a530250ac500955ca08' |
| `body.direct.id` | value | 'lib_4c04f2e865f84e7aa1891494e802420b' | 'lib_06807196e599340b92f395970c706dfc' |
| `body.direct.references[0].id` | value | 'ruleset_c21768e6-13ed-40ee-a6bd-6301ca10b52f' | 'ruleset_0b1e8f127f3d14ceb9211fd6fe680cd7' |
| `body.proxy.id` | value | 'lib_d5214a184d674a17a6779218975d04a5' | 'lib_d3cc09997a916a530250ac500955ca08' |
| `body.proxy.references[0].id` | value | 'ruleset_865842f0-8980-46f5-975a-a40fd095c874' | 'ruleset_c823482726bbf85ea00fd6d680d154c0' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_4c04f2e865f84e7aa1891494e802420b' | 'lib_06807196e599340b92f395970c706dfc' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_d5214a184d674a17a6779218975d04a5' | 'lib_d3cc09997a916a530250ac500955ca08' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_c21768e6-13ed-40ee-a6bd-6301ca10b52f' | 'ruleset_0b1e8f127f3d14ceb9211fd6fe680cd7' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_4c04f2e865f84e7aa1891494e802420b' | 'lib_06807196e599340b92f395970c706dfc' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_865842f0-8980-46f5-975a-a40fd095c874' | 'ruleset_c823482726bbf85ea00fd6d680d154c0' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_d5214a184d674a17a6779218975d04a5' | 'lib_d3cc09997a916a530250ac500955ca08' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_4c04f2e865f84e7aa1891494e802420b' | 'lib_06807196e599340b92f395970c706dfc' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_d5214a184d674a17a6779218975d04a5' | 'lib_d3cc09997a916a530250ac500955ca08' |
| `files.shared/rules/by-id/2c525c709c10def0b9f6c657d76505b7be2fcba85775682e221a364f7938e83f.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/3a3da226a53450bc74bbfe9d515bf30f68d36f505a2c965b79ff0075fc7a125a.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/457ac963528d7a04aaddcaeb6780107ae381c152dd9bec3fb7c11c0363943d8e.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/728a48d5c29337626713f44c76014a856470a94ef1ab9a5d18f287531b465074.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S04 / repeat/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_fcd17d387b57476bb9256c41ac3d0807' | 'lib_4bb7274e2b6c62d9745d6f4d1cc349f9' |
| `body.direct.id` | value | 'lib_fcd17d387b57476bb9256c41ac3d0807' | 'lib_4bb7274e2b6c62d9745d6f4d1cc349f9' |
| `body.direct.references[0].id` | value | 'ruleset_69ad8f5e-2641-4094-aec3-8e61b308748d' | 'ruleset_5c685f9cd148b8900d3a7077b7dae109' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_fcd17d387b57476bb9256c41ac3d0807' | 'lib_4bb7274e2b6c62d9745d6f4d1cc349f9' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_69ad8f5e-2641-4094-aec3-8e61b308748d' | 'ruleset_5c685f9cd148b8900d3a7077b7dae109' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_fcd17d387b57476bb9256c41ac3d0807' | 'lib_4bb7274e2b6c62d9745d6f4d1cc349f9' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_fcd17d387b57476bb9256c41ac3d0807' | 'lib_4bb7274e2b6c62d9745d6f4d1cc349f9' |
| `files.shared/rules/by-id/c2f6385b6f69d4957af1c1d2c22c1fc49d3ab4a2849f09a7c615c6206add8431.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/fa9f00f5b4738264769989ad4dbea90afe7130282783559e51005cb39338daf6.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S04 / repeat/1

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<resource-prefixed hex/UUID IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase hex/UUID shape' |
| `body.candidates[2].id` | value | 'lib_fcd17d387b57476bb9256c41ac3d0807' | 'lib_4bb7274e2b6c62d9745d6f4d1cc349f9' |
| `body.candidates[3].id` | value | 'lib_c50996cad8084245aa12e09f94f74722' | 'lib_b27e7df4b6f01d242c5188e9d8e9b8cc' |
| `body.direct.id` | value | 'lib_c50996cad8084245aa12e09f94f74722' | 'lib_b27e7df4b6f01d242c5188e9d8e9b8cc' |
| `body.direct.references[0].id` | value | 'ruleset_11beb6ce-16b3-46da-8e8c-15eee1e8436e' | 'ruleset_7e10bee88b764b4bf706f8ea5c122ee8' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_c50996cad8084245aa12e09f94f74722' | 'lib_b27e7df4b6f01d242c5188e9d8e9b8cc' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_69ad8f5e-2641-4094-aec3-8e61b308748d' | 'ruleset_5c685f9cd148b8900d3a7077b7dae109' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_fcd17d387b57476bb9256c41ac3d0807' | 'lib_4bb7274e2b6c62d9745d6f4d1cc349f9' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_11beb6ce-16b3-46da-8e8c-15eee1e8436e' | 'ruleset_7e10bee88b764b4bf706f8ea5c122ee8' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_c50996cad8084245aa12e09f94f74722' | 'lib_b27e7df4b6f01d242c5188e9d8e9b8cc' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_fcd17d387b57476bb9256c41ac3d0807' | 'lib_4bb7274e2b6c62d9745d6f4d1cc349f9' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_c50996cad8084245aa12e09f94f74722' | 'lib_b27e7df4b6f01d242c5188e9d8e9b8cc' |
| `files.shared/rules/by-id/0dc4601611f346fa76c4c26ad1fb6791365bf4c38aa77ef122e68495099081dd.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/37a8e402160173f8427e66098c562e055cae5659a6aa2fb7204a8917d66402a9.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/c2f6385b6f69d4957af1c1d2c22c1fc49d3ab4a2849f09a7c615c6206add8431.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/fa9f00f5b4738264769989ad4dbea90afe7130282783559e51005cb39338daf6.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

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
