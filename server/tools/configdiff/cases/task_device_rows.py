"""Inactive device task listing and retry guards without starting HR work."""
from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.pull_routes import SourceStable
from cases.subscriptions import put_file, require


def rows(binary, recorder):
    with Source(b'listing and rejection must not call a device') as device:
        options = {'agent_updates': {'service_type': 'oxidns', 'port': device.server.server_port,
                    'dns_follow': None, 'dns_follow_state': {'status': 'failed', 'message': '合成联动错误'},
                    'rule_sync': {'settings': {'library': False, 'rules': False}, 'state': {'status': 'failed', 'message': '合成设备错误'}}},
                   'profile': {'routers': {'items': [{'id': 'router-status', 'name': '合成路由器🧪', 'type': 'routeros',
                    'endpoint': device.url, 'username': 'synthetic-router-user', 'password': 'synthetic-router-password',
                    'auto_rule_sync': False, 'rule_sync_interval': 2}]}}}
        with Pair(binary, recorder, options=options) as pair:
            put_file(pair, 'task-status.json', {key: {'last_status': 'failed', 'last_error': '合成历史错误', 'run_count': 4,
                'failure_count': 2, 'consecutive_failures': 2, 'last_run_at': '2026-01-01T00:00:00Z'}
                for key in ('agent_rule_sync:agent-synthetic', 'dns_follow:agent-synthetic')})
            def check(reply):
                tasks = {item['key']: item for item in reply.json()['tasks']}
                agent, follow, router = [tasks[key] for key in ('agent_rule_sync:agent-synthetic', 'dns_follow:agent-synthetic',
                                                              'router_rule_sync:default:router-status')]
                require(agent['category'] == 'agent_rule_sync' and agent['enabled'] is False
                    and agent['can_run'] is False and agent['running'] is False
                    and agent['last_status'] == 'failed' and agent['last_error'] == '合成历史错误',
                    'disabled rule sync still exposes its complete durable task history')
                require(follow['category'] == 'dns_follow' and follow['enabled'] is False and follow['can_run'] is False
                    and follow['last_status'] == 'failed' and follow['last_error'] == '合成联动错误',
                    'DNS task distinguishes actual device follow-state from the durable generic receipt')
                require(router['category'] == 'router_rule_sync' and router['interval_seconds'] == 120
                    and router['enabled'] is False and router['can_run'] is False and router['running'] is False
                    and router['run_hint'] == '自动同步未开启' and router['state_message'] == '等待首次手动发布建立同步基准',
                    'router minutes convert to seconds and lack of baseline remains visible without a device call')
            pair.request('R225-S03', 'GET /api/tasks', expect=200, writes=True, interaction=SourceStable(pair, device),
                         check=check, variant='loopback-Agent-router-inactive-history-and-readiness')
            pair.request('R227-F14', 'POST /api/tasks/run', body=json_body({'key': 'router_rule_sync:default:router-status'}),
                         expect=409, writes=True, interaction=SourceStable(pair, device),
                         check=message('请开启自动同步，并先手动预览发布一次'), variant='real-router-retry-without-enabled-baseline')
            pair.request('R227-F15', 'POST /api/tasks/run', body=json_body({'key': 'dns_follow:agent-synthetic'}),
                         expect=409, writes=True, interaction=SourceStable(pair, device),
                         check=message('只有已确认失败的 DNS 联动可以重试；执行中的任务会继续确认结果'),
                         variant='real-existing-OxiDNS-Agent-without-follow-binding')


GROUPS = {'tasks-device-status-and-retry-guards': rows}
