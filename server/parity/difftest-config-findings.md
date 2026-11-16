# Config Step B 差异发现

仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。

Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。

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
