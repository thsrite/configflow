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

## R056-B01 / name-collision/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<UUID4 resource IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase UUID4' |
| `body.candidates[2].id` | value | 'lib_5aa352d1d67940aa936f36c92b2c25ca' | 'lib_49bf05dbd0fa2e6f699dd85e5590f06d' |
| `body.candidates[3].id` | value | 'lib_436a13afa6464312b440ba5573b1b50c' | 'lib_41f2ccd3a9f56d706bfde1d18587683a' |
| `body.direct.id` | value | 'lib_5aa352d1d67940aa936f36c92b2c25ca' | 'lib_49bf05dbd0fa2e6f699dd85e5590f06d' |
| `body.direct.references[0].id` | value | 'ruleset_e57997e6-357b-421a-b77b-5fae817ad03b' | 'ruleset_9851b81c012173de6b7d627e3080b326' |
| `body.proxy.id` | value | 'lib_436a13afa6464312b440ba5573b1b50c' | 'lib_41f2ccd3a9f56d706bfde1d18587683a' |
| `body.proxy.references[0].id` | value | 'ruleset_50fcc790-dcd0-4b45-9d0a-d3e03aee673d' | 'ruleset_2160a868260049b9a20a932d18ded1b4' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_5aa352d1d67940aa936f36c92b2c25ca' | 'lib_49bf05dbd0fa2e6f699dd85e5590f06d' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_436a13afa6464312b440ba5573b1b50c' | 'lib_41f2ccd3a9f56d706bfde1d18587683a' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_e57997e6-357b-421a-b77b-5fae817ad03b' | 'ruleset_9851b81c012173de6b7d627e3080b326' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_5aa352d1d67940aa936f36c92b2c25ca' | 'lib_49bf05dbd0fa2e6f699dd85e5590f06d' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_50fcc790-dcd0-4b45-9d0a-d3e03aee673d' | 'ruleset_2160a868260049b9a20a932d18ded1b4' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_436a13afa6464312b440ba5573b1b50c' | 'lib_41f2ccd3a9f56d706bfde1d18587683a' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_5aa352d1d67940aa936f36c92b2c25ca' | 'lib_49bf05dbd0fa2e6f699dd85e5590f06d' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_436a13afa6464312b440ba5573b1b50c' | 'lib_41f2ccd3a9f56d706bfde1d18587683a' |
| `files.shared/rules/by-id/3bc5dfdd2e4e865e32bfece26231275bf3ca506563ea9da71c5abcc5743118cf.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/7b29183fc516a7d3929f2f476bf4e384319d136ed62bf8daeb550ea32b42d888.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/c19dd82aa4d127189806fd875244b1cc3c44e795f5d9d9fe772e08bb396984a6.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/e58f99001adf902c2d445a7c92daffa31eb9802e57002a2985ec4abc2ba6fa10.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-B02 / before-MATCH/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<UUID4 resource IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase UUID4' |
| `body.candidates[2].id` | value | 'lib_e334621084264d18b3978f657b8f607e' | 'lib_6a2c2856998d96c8f915da7b2b98c5a5' |
| `body.candidates[3].id` | value | 'lib_56ab7636cd48425f81479eb5fa336f74' | 'lib_6baa35a4e291e8748fcff9a5fecd5916' |
| `body.direct.id` | value | 'lib_e334621084264d18b3978f657b8f607e' | 'lib_6a2c2856998d96c8f915da7b2b98c5a5' |
| `body.direct.references[0].id` | value | 'ruleset_508da043-7044-44c2-9e37-639fd20def78' | 'ruleset_c9df200c2a37a5c3bac9904a44acacda' |
| `body.proxy.id` | value | 'lib_56ab7636cd48425f81479eb5fa336f74' | 'lib_6baa35a4e291e8748fcff9a5fecd5916' |
| `body.proxy.references[0].id` | value | 'ruleset_7c2dd642-20b6-4ba5-91d3-a35a1fc42edd' | 'ruleset_290756db8968440d9cd90f34dba52530' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_e334621084264d18b3978f657b8f607e' | 'lib_6a2c2856998d96c8f915da7b2b98c5a5' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_56ab7636cd48425f81479eb5fa336f74' | 'lib_6baa35a4e291e8748fcff9a5fecd5916' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_508da043-7044-44c2-9e37-639fd20def78' | 'ruleset_c9df200c2a37a5c3bac9904a44acacda' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_e334621084264d18b3978f657b8f607e' | 'lib_6a2c2856998d96c8f915da7b2b98c5a5' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_7c2dd642-20b6-4ba5-91d3-a35a1fc42edd' | 'ruleset_290756db8968440d9cd90f34dba52530' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_56ab7636cd48425f81479eb5fa336f74' | 'lib_6baa35a4e291e8748fcff9a5fecd5916' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_e334621084264d18b3978f657b8f607e' | 'lib_6a2c2856998d96c8f915da7b2b98c5a5' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_56ab7636cd48425f81479eb5fa336f74' | 'lib_6baa35a4e291e8748fcff9a5fecd5916' |
| `files.shared/rules/by-id/1be768fa23474dcb1032e4d563cfb1d14a1194071c32e377b8cbc5bffb74543c.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/53c422707e3af0d2ca08f2a239d9b5fc9645a3b2327bc08f744e176773039371.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/c57c2c26b194c7f0b44c201a4ab85a89f5a53e095f33d0295ecf04f9a239f1a8.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/e38ad2d89819b5df55884239ef02c6f4da0e81e061e70342e2ac27cb08cb7618.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S01 / direct/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<UUID4 resource IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase UUID4' |
| `body.candidates[2].id` | value | 'lib_dc768a12c7b4459db65182d1be89e218' | 'lib_e358e6f353f4a643304be31bfd345c75' |
| `body.direct.id` | value | 'lib_dc768a12c7b4459db65182d1be89e218' | 'lib_e358e6f353f4a643304be31bfd345c75' |
| `body.direct.references[0].id` | value | 'ruleset_f885866f-655e-4464-ad86-6e965e4dfeb0' | 'ruleset_1d2e42a7ad1c6419b8e1a26c41733b67' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_dc768a12c7b4459db65182d1be89e218' | 'lib_e358e6f353f4a643304be31bfd345c75' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_f885866f-655e-4464-ad86-6e965e4dfeb0' | 'ruleset_1d2e42a7ad1c6419b8e1a26c41733b67' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_dc768a12c7b4459db65182d1be89e218' | 'lib_e358e6f353f4a643304be31bfd345c75' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_dc768a12c7b4459db65182d1be89e218' | 'lib_e358e6f353f4a643304be31bfd345c75' |
| `files.shared/rules/by-id/4422093ea2a905025720cede5a2ccf6fded3f01d0545f9e8de83cf5a0db9f783.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/9ded588dc6191bc5c64b7a34ee1178592cc171106ec945d377ca35cb28e63abc.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S01 / duplicate-targets/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<UUID4 resource IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase UUID4' |
| `body.candidates[2].id` | value | 'lib_5f3c4e6146f448aa946f058a5a70efc5' | 'lib_bbababd60216bbbb84466da5afe48b87' |
| `body.candidates[3].id` | value | 'lib_1cb47eaa54f547f488712472d13e9d84' | 'lib_9d07f8c60ba0ab8c65c98ef48c51558a' |
| `body.direct.id` | value | 'lib_1cb47eaa54f547f488712472d13e9d84' | 'lib_9d07f8c60ba0ab8c65c98ef48c51558a' |
| `body.direct.references[0].id` | value | 'ruleset_489a50eb-0989-45eb-a7dc-a2740bfdcc43' | 'ruleset_879f2809ea50963abf0e1487c595eec7' |
| `body.direct.references[1].id` | value | 'ruleset_91e63e3d-5fd6-4533-b249-9a5fb548e64f' | 'ruleset_5a2785f8f4454fe6643f3a76f7b6eca5' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_1cb47eaa54f547f488712472d13e9d84' | 'lib_9d07f8c60ba0ab8c65c98ef48c51558a' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_489a50eb-0989-45eb-a7dc-a2740bfdcc43' | 'ruleset_879f2809ea50963abf0e1487c595eec7' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_1cb47eaa54f547f488712472d13e9d84' | 'lib_9d07f8c60ba0ab8c65c98ef48c51558a' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_91e63e3d-5fd6-4533-b249-9a5fb548e64f' | 'ruleset_5a2785f8f4454fe6643f3a76f7b6eca5' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_1cb47eaa54f547f488712472d13e9d84' | 'lib_9d07f8c60ba0ab8c65c98ef48c51558a' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_5f3c4e6146f448aa946f058a5a70efc5' | 'lib_bbababd60216bbbb84466da5afe48b87' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_1cb47eaa54f547f488712472d13e9d84' | 'lib_9d07f8c60ba0ab8c65c98ef48c51558a' |
| `files.shared/rules/by-id/46102e10bf37e43166e32a5257484a97646ba95bcec3fab834f33298460c7648.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/46102e10bf37e43166e32a5257484a97646ba95bcec3fab834f33298460c7648.list.bak` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/d3214c44c9ed5c8149a56e781c97e3c9e156f7cee409fdc0db7f8cc1bd00d30b.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S02 / proxy/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<UUID4 resource IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase UUID4' |
| `body.candidates[2].id` | value | 'lib_662fa7c642aa4b04b677882ddfca5f96' | 'lib_3086b06e562a3f23f585957dcd15b149' |
| `body.proxy.id` | value | 'lib_662fa7c642aa4b04b677882ddfca5f96' | 'lib_3086b06e562a3f23f585957dcd15b149' |
| `body.proxy.references[0].id` | value | 'ruleset_92b829c3-2734-4e21-a217-2152a0020a64' | 'ruleset_1a77518fe86399a9d2f074462f730669' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_662fa7c642aa4b04b677882ddfca5f96' | 'lib_3086b06e562a3f23f585957dcd15b149' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_92b829c3-2734-4e21-a217-2152a0020a64' | 'ruleset_1a77518fe86399a9d2f074462f730669' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_662fa7c642aa4b04b677882ddfca5f96' | 'lib_3086b06e562a3f23f585957dcd15b149' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_662fa7c642aa4b04b677882ddfca5f96' | 'lib_3086b06e562a3f23f585957dcd15b149' |
| `files.shared/rules/by-id/6f0aee8a67aa7ddd7f0149da6e49d32bdbc848fb0d02765166308cff01efda4d.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/87d624d2168afc5db6f7640697562d7e57fb987f178a839941782ccb155f2373.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S03 / both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<UUID4 resource IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase UUID4' |
| `body.candidates[2].id` | value | 'lib_1d9f8dcf5e2541e88dcb39cbb23a5440' | 'lib_ccdc6df30cf3eac1b19b96c9482a30da' |
| `body.candidates[3].id` | value | 'lib_a9812dc5ff3e481bb8c4583a523c2e7f' | 'lib_0840745fcf752750083a6083f7b0d662' |
| `body.direct.id` | value | 'lib_1d9f8dcf5e2541e88dcb39cbb23a5440' | 'lib_ccdc6df30cf3eac1b19b96c9482a30da' |
| `body.direct.references[0].id` | value | 'ruleset_95b173e6-7a71-46e4-a9e2-81644df59eb3' | 'ruleset_0fe410780cc5347ee0cd9339928035e0' |
| `body.proxy.id` | value | 'lib_a9812dc5ff3e481bb8c4583a523c2e7f' | 'lib_0840745fcf752750083a6083f7b0d662' |
| `body.proxy.references[0].id` | value | 'ruleset_74b38ae5-5dce-4a2a-b091-ba1b44b68543' | 'ruleset_4d7c1bd4d736dd06d602f7a35189a93a' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_1d9f8dcf5e2541e88dcb39cbb23a5440' | 'lib_ccdc6df30cf3eac1b19b96c9482a30da' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_a9812dc5ff3e481bb8c4583a523c2e7f' | 'lib_0840745fcf752750083a6083f7b0d662' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_95b173e6-7a71-46e4-a9e2-81644df59eb3' | 'ruleset_0fe410780cc5347ee0cd9339928035e0' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_1d9f8dcf5e2541e88dcb39cbb23a5440' | 'lib_ccdc6df30cf3eac1b19b96c9482a30da' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_74b38ae5-5dce-4a2a-b091-ba1b44b68543' | 'ruleset_4d7c1bd4d736dd06d602f7a35189a93a' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_a9812dc5ff3e481bb8c4583a523c2e7f' | 'lib_0840745fcf752750083a6083f7b0d662' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_1d9f8dcf5e2541e88dcb39cbb23a5440' | 'lib_ccdc6df30cf3eac1b19b96c9482a30da' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_a9812dc5ff3e481bb8c4583a523c2e7f' | 'lib_0840745fcf752750083a6083f7b0d662' |
| `files.shared/rules/by-id/23fcd1045c4256dbc1620bc1ebd5f593175c3e17606df9e5fd51b24a55317b56.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/39dbe170cd649e6d7074ef1f95ff29c9b2e962cbc98b3065b55c7498cf683bf3.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/9c11db3ab5448124a132a3118722471568098a89914ffbf09c17ac70966a0a84.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/adfcd6afc4c556063fc08d1864b97cfd7bec5ee74dea411a4edc47c58f09c1c5.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S03 / empty-means-both/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<UUID4 resource IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase UUID4' |
| `body.candidates[2].id` | value | 'lib_5ca37213da604b679e9fc45df10942f1' | 'lib_4383b1046441e3c40fd23741e137a5f2' |
| `body.candidates[3].id` | value | 'lib_cf03c715b64743638eb393d15a793a49' | 'lib_baba8eed504048afe4a708455c360711' |
| `body.direct.id` | value | 'lib_5ca37213da604b679e9fc45df10942f1' | 'lib_4383b1046441e3c40fd23741e137a5f2' |
| `body.direct.references[0].id` | value | 'ruleset_ec32517d-bae8-4ebb-9705-e873d2a0a174' | 'ruleset_8d0699fc5c69b2107ba5c2fcaebe1266' |
| `body.proxy.id` | value | 'lib_cf03c715b64743638eb393d15a793a49' | 'lib_baba8eed504048afe4a708455c360711' |
| `body.proxy.references[0].id` | value | 'ruleset_8bd8030f-190e-4046-8a2f-2115b0c25b65' | 'ruleset_30f487c61cfc00633e55b1c526ccba30' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_5ca37213da604b679e9fc45df10942f1' | 'lib_4383b1046441e3c40fd23741e137a5f2' |
| `db.fields.profile/default/domain_discovery.value.proxy_ruleset` | value | 'lib_cf03c715b64743638eb393d15a793a49' | 'lib_baba8eed504048afe4a708455c360711' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_ec32517d-bae8-4ebb-9705-e873d2a0a174' | 'ruleset_8d0699fc5c69b2107ba5c2fcaebe1266' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_5ca37213da604b679e9fc45df10942f1' | 'lib_4383b1046441e3c40fd23741e137a5f2' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_8bd8030f-190e-4046-8a2f-2115b0c25b65' | 'ruleset_30f487c61cfc00633e55b1c526ccba30' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_cf03c715b64743638eb393d15a793a49' | 'lib_baba8eed504048afe4a708455c360711' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_5ca37213da604b679e9fc45df10942f1' | 'lib_4383b1046441e3c40fd23741e137a5f2' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_cf03c715b64743638eb393d15a793a49' | 'lib_baba8eed504048afe4a708455c360711' |
| `files.shared/rules/by-id/14f237422c1dc386bb0aeeea1b1857d416af192afff73f916a099c35a856d496.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/8fdf6f7b710a71597771a1170cbe2798da0a02731f9e353934569c18c7d4c951.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/ca3896807218aae4b3a2559354660197042e365b269e2e5da9dd2694c1c49dd6.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/daef94a3dfd5ac61cb43c512de046965f29fecf23292a5528274fdef90680ba8.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

## R056-S04 / repeat/0

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<UUID4 resource IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase UUID4' |
| `body.candidates[2].id` | value | 'lib_826960dd38bf445fa24cba3be25b9110' | 'lib_16e19e9374a46039371cf29d04af8455' |
| `body.direct.id` | value | 'lib_826960dd38bf445fa24cba3be25b9110' | 'lib_16e19e9374a46039371cf29d04af8455' |
| `body.direct.references[0].id` | value | 'ruleset_7dec4325-fe55-4f3e-87d8-2cb538a40d9f' | 'ruleset_b14b46d3b83e8c4f27b78167805c3b5f' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_826960dd38bf445fa24cba3be25b9110' | 'lib_16e19e9374a46039371cf29d04af8455' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_7dec4325-fe55-4f3e-87d8-2cb538a40d9f' | 'ruleset_b14b46d3b83e8c4f27b78167805c3b5f' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_826960dd38bf445fa24cba3be25b9110' | 'lib_16e19e9374a46039371cf29d04af8455' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_826960dd38bf445fa24cba3be25b9110' | 'lib_16e19e9374a46039371cf29d04af8455' |
| `files.shared/rules/by-id/0dd742e1c8a34d8e37ef59966a5d63f9c3b7f48ece2fd772bfdf47fed7c7513c.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/a3f8794faea4d5e2e9e04de6f3e7e4d0c3a01bb5f14840fe3c9d21ea3dc1cbc8.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |

## R056-S04 / repeat/1

路由：`POST /api/domain-discovery/rulesets/init`；状态：different

| 路径 | 类型 | Python | Go |
|---|---|---|---|
| `discovery.allocations` | invalid-Go-allocation | '<UUID4 resource IDs and derived paths>' | 'discovery allocation needs its resource prefix and lowercase UUID4' |
| `body.candidates[2].id` | value | 'lib_826960dd38bf445fa24cba3be25b9110' | 'lib_16e19e9374a46039371cf29d04af8455' |
| `body.candidates[3].id` | value | 'lib_06aa4c23040f4213b657635ef9b99370' | 'lib_168cf097989e2190d21f6dc6dca51709' |
| `body.direct.id` | value | 'lib_06aa4c23040f4213b657635ef9b99370' | 'lib_168cf097989e2190d21f6dc6dca51709' |
| `body.direct.references[0].id` | value | 'ruleset_9e6c3d07-6503-4ffb-b271-2d161e762e24' | 'ruleset_5915ce9c750bbdb2375f443491424671' |
| `db.fields.profile/default/domain_discovery.value.direct_ruleset` | value | 'lib_06aa4c23040f4213b657635ef9b99370' | 'lib_168cf097989e2190d21f6dc6dca51709' |
| `db.fields.profile/default/rule_configs.value[2].id` | value | 'ruleset_7dec4325-fe55-4f3e-87d8-2cb538a40d9f' | 'ruleset_b14b46d3b83e8c4f27b78167805c3b5f' |
| `db.fields.profile/default/rule_configs.value[2].library_rule_id` | value | 'lib_826960dd38bf445fa24cba3be25b9110' | 'lib_16e19e9374a46039371cf29d04af8455' |
| `db.fields.profile/default/rule_configs.value[3].id` | value | 'ruleset_9e6c3d07-6503-4ffb-b271-2d161e762e24' | 'ruleset_5915ce9c750bbdb2375f443491424671' |
| `db.fields.profile/default/rule_configs.value[3].library_rule_id` | value | 'lib_06aa4c23040f4213b657635ef9b99370' | 'lib_168cf097989e2190d21f6dc6dca51709' |
| `db.fields.shared//rule_library.value[4].id` | value | 'lib_826960dd38bf445fa24cba3be25b9110' | 'lib_16e19e9374a46039371cf29d04af8455' |
| `db.fields.shared//rule_library.value[5].id` | value | 'lib_06aa4c23040f4213b657635ef9b99370' | 'lib_168cf097989e2190d21f6dc6dca51709' |
| `files.shared/rules/by-id/0dd742e1c8a34d8e37ef59966a5d63f9c3b7f48ece2fd772bfdf47fed7c7513c.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |
| `files.shared/rules/by-id/391ead2ddfb37b44efdd972a50ec9627a438743896363e423b3a202ea46276e6.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/a3f8794faea4d5e2e9e04de6f3e7e4d0c3a01bb5f14840fe3c9d21ea3dc1cbc8.list` | missing-in-go | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} | None |
| `files.shared/rules/by-id/fbd449be309f3cf3c3ef9e560151bd28d777b1d2e0daba2a5498f83f623f08d7.list` | extra-in-go | None | {'mode': 384, 'content': {'bytes_length': 0, 'bytes_sha256': 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', 'bytes_prefix_hex': ''}} |

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
