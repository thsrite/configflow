"""Operation logs: one readable line per event, never secrets or forged lines."""
import logging

from backend.agents import dns_follow, presence_log
from backend.agents import rule_sync as agent_rule_sync
from backend.agents.manager import AgentManager
from backend.common.config_repository import ProfileRepository
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401  (pytest fixture)
from backend.test_router_release import (
    DESIRED, HOME_NATS, HOME_ROUTES, SAFE_RETURN_STATUS, FakeAgent, StatefulIKuai, _driver,
)
from backend.test_webdav_auto_backup import backup_api, dav  # noqa: F401  (pytest fixtures)
from backend.utils.logger import log_text


def _messages(caplog, level=None):
    return [record.getMessage() for record in caplog.records if level is None or record.levelno == level]


def test_log_text_neutralizes_line_breaks_and_bounds_length():
    assert log_text('a\r\nb c\td') == 'a??b?c?d'
    assert log_text('x' * 200, 10) == 'x' * 10 + '…'
    assert log_text(None) == 'None'


def test_login_success_and_failure_are_logged_without_password(authenticated_app, caplog):  # noqa: F811
    client = authenticated_app[0]
    with caplog.at_level(logging.INFO):
        client.post('/api/auth/login', json={'username': 'upgrade-admin', 'password': 'wrong-secret-pw'})
        client.post('/api/auth/login', json={'username': 'upgrade-admin', 'password': 'upgrade-test-password'})
    failures = _messages(caplog, logging.WARNING)
    assert any(message.startswith('登录失败') and 'upgrade-admin' in message for message in failures)
    assert any(message.startswith('登录成功') for message in _messages(caplog, logging.INFO))
    assert 'wrong-secret-pw' not in caplog.text and 'upgrade-test-password' not in caplog.text


def test_client_config_pull_is_logged_without_query_token(authenticated_app, caplog):  # noqa: F811
    client = authenticated_app[0]
    with caplog.at_level(logging.INFO):
        client.get('/api/config/mihomo?token=pull-secret-token', headers={'User-Agent': 'clash.meta\tv1'})
        client.get('/api/config/default/oxidns?token=pull-secret-token')
        client.get('/api/config/export')   # an administrator download, not a client pull
    pulls = [message for message in _messages(caplog) if message.startswith('配置被拉取')]
    assert len(pulls) == 2
    assert 'mihomo（配置空间 默认）' in pulls[0] and 'clash.meta?v1' in pulls[0]
    assert 'oxidns（配置空间 default）' in pulls[1]
    assert 'pull-secret-token' not in caplog.text


def _router_app(authenticated_app, monkeypatch, session):  # noqa: F811
    client, _, agents, admin, _ = authenticated_app
    router_id = client.post('/api/routers', headers=admin, json={
        'name': '主路由', 'type': 'ikuai', 'endpoint': 'http://10.0.0.1', 'username': 'admin',
        'password': 'router-secret-pw', 'proxy_gateway_agent_id': agents[0]['id'],
        'manage_return_routing': True}).get_json()['router']['id']
    from backend.routes import routers as routes_module
    agent = FakeAgent(SAFE_RETURN_STATUS)
    monkeypatch.setitem(routes_module.DRIVERS, 'ikuai', lambda *args, **kwargs: _driver(session))
    monkeypatch.setattr(routes_module, 'compute_desired', lambda *args, **kwargs: DESIRED)
    monkeypatch.setattr(routes_module, '_return_routing', lambda record: {**agent.status(), 'status': 'ok'})
    monkeypatch.setattr(routes_module, 'AgentReturnRouting', lambda manager, record: agent)
    fingerprint = client.post(f'/api/routers/{router_id}/plan', headers=admin).get_json()['plan']['fingerprint']
    return client, admin, router_id, fingerprint


def test_router_release_logs_start_and_verified_completion(authenticated_app, monkeypatch, caplog):  # noqa: F811
    session = StatefulIKuai(HOME_ROUTES, HOME_NATS)
    client, admin, router_id, fingerprint = _router_app(authenticated_app, monkeypatch, session)
    with caplog.at_level(logging.INFO):
        stale = client.post(f'/api/routers/{router_id}/apply', headers=admin, json={'fingerprint': 'stale'})
        applied = client.post(f'/api/routers/{router_id}/apply', headers=admin, json={'fingerprint': fingerprint})
    assert stale.status_code == 409 and applied.status_code == 200
    messages = _messages(caplog)
    assert any('主路由 主路由 发布被拒绝' in message for message in messages)
    assert any(message.startswith('开始发布到主路由 主路由') for message in messages)
    assert any('发布完成' in message and '通过回读校验' in message for message in messages)
    assert 'router-secret-pw' not in caplog.text


def test_router_release_failure_logs_stage_and_rollback(authenticated_app, monkeypatch, caplog):  # noqa: F811
    session = StatefulIKuai(HOME_ROUTES, HOME_NATS, fail=('nat_rule', 'edit'))
    client, admin, router_id, fingerprint = _router_app(authenticated_app, monkeypatch, session)
    with caplog.at_level(logging.INFO):
        result = client.post(f'/api/routers/{router_id}/apply', headers=admin, json={'fingerprint': fingerprint})
    assert result.status_code == 502
    errors = _messages(caplog, logging.ERROR)
    assert any('发布失败：阶段「nat_bypass」' in message for message in errors)
    assert any('已逆序撤销' in message for message in _messages(caplog, logging.WARNING))


def test_presence_logs_transitions_but_not_the_startup_baseline(monkeypatch, caplog):
    monkeypatch.setattr(presence_log, '_known', {})
    online = {'id': 'a1', 'name': 'gw\nfake', 'status': 'online', 'last_heartbeat': '2026-10-20T10:00:00'}
    offline = {**online, 'status': 'offline'}
    with caplog.at_level(logging.INFO, logger='backend.agents.presence_log'):
        presence_log.observe([offline])           # after restart, not heard from yet: silent
        presence_log.observe([offline])
        presence_log.observe([online])            # first heartbeat since start: baseline, silent
        presence_log.observe([online])
        presence_log.observe([offline])
        presence_log.observe([online])
        presence_log.observe([])                  # deleted: silent
        presence_log.observe([offline])           # back but not heard from: silent
    assert _messages(caplog) == ['Agent gw?fake 已离线：超过 2 分钟没有心跳（最后心跳 2026-10-20T10:00:00）',
                                 'Agent gw?fake 已恢复在线']


def test_agent_rule_sync_logs_each_state_change_once(monkeypatch, caplog):
    agent = {'id': 'a1', 'name': 'edge', 'rule_sync': {'state': {}}}

    class Manager:
        def get_agent_by_id(self, agent_id):
            return agent

    def update(manager, agent_id, state):
        agent['rule_sync']['state'] = state
    monkeypatch.setattr(agent_rule_sync, '_update', update)
    worker = agent_rule_sync.RuleSyncWorker(Manager(), clock=lambda: 0)
    with caplog.at_level(logging.DEBUG, logger='backend.agents.rule_sync'):
        worker._state('a1', 'failed', '下载失败', retry_at=60)
        worker._state('a1', 'failed', '下载失败', retry_at=120)   # retry with the same error
        worker._state('a1', 'succeeded', '已热加载')
    assert [(record.levelno, record.getMessage()) for record in caplog.records] == [
        (logging.WARNING, 'Agent edge 规则自动同步：下载失败'),
        (logging.INFO, 'Agent edge 规则自动同步：已热加载'),
    ]


def test_dns_follow_repeated_error_is_logged_once(caplog):
    agents = [{'id': 'ox', 'name': 'oxidns-1', 'token': 'agent-secret-token'}]

    class Manager:
        def _update_agents(self, updater):
            return updater(agents)
    with caplog.at_level(logging.DEBUG, logger='backend.agents.dns_follow'):
        for _ in range(3):
            dns_follow._state(Manager(), 'ox', 'failed', 'DNS 联动暂不可用：agent-secret-token 离线')
        dns_follow._state(Manager(), 'ox', 'preparing', '正在准备')
    messages = _messages(caplog)
    assert len(messages) == 2
    assert messages[0].startswith('OxiDNS oxidns-1 DNS 联动：DNS 联动暂不可用')
    assert 'agent-secret-token' not in caplog.text


def test_deployment_result_is_logged_once(tmp_path, caplog):
    manager = AgentManager(ProfileRepository(tmp_path))
    agent_id = manager.register_agent({'name': 'edge', 'host': '10.0.0.8', 'service_type': 'mihomo'})['id']
    with caplog.at_level(logging.INFO, logger='backend.agents.manager'):
        assert manager.begin_deployment(agent_id, 'dep-1')['success']
        manager.finish_deployment_preparation(agent_id, 'dep-1')
        manager.record_deployment(agent_id, 'dep-1', {'status': 'failed', 'error': '配置校验失败\n第 3 行'})
        manager.record_deployment(agent_id, 'dep-1', {'status': 'failed', 'error': '配置校验失败'})
        manager.delete_agent(agent_id)
    assert _messages(caplog) == [
        '开始向 Agent edge 发布配置（任务 dep-1）',
        'Agent edge 配置发布失败（任务 dep-1）：配置校验失败?第 3 行',
        '已删除 Agent edge',
    ]


def test_manual_webdav_backup_logs_file_and_saved_fields_only(backup_api, dav, caplog):  # noqa: F811
    with caplog.at_level(logging.INFO):
        saved = backup_api.post('/api/backup/config', json={'webdav_path': '/other/', 'webdav_password': 'new-dav-pw'})
        result = backup_api.post('/api/backup/now', json={'webdav_url': 'https://dav.test', 'webdav_username': 'user',
                                                          'webdav_password': '******', 'webdav_path': '/backups/'})
    assert saved.status_code == 200 and result.status_code == 200
    messages = _messages(caplog)
    assert any(message.startswith('WebDAV 备份配置已保存：webdav_password、webdav_path') for message in messages)
    assert any(message.startswith('WebDAV 手动备份完成：/backups/config_backup_') and 'KB' in message
               for message in messages)
    assert 'new-dav-pw' not in caplog.text and 'secret' not in caplog.text


def test_rule_download_retry_and_mirror_fallback_are_logged_with_safe_urls(monkeypatch, caplog):
    import requests
    from backend.test_rule_fetch_retries import response, tls_eof
    from backend.utils.rule_fetch import request_rule
    source = 'https://user:dl-secret@raw.githubusercontent.com/owner/repo/main/rules.list?token=dl-secret'
    calls = []

    def fetch(url, **kwargs):
        calls.append(url)
        if len(calls) <= 3:
            raise tls_eof()
        return response()
    monkeypatch.setattr('backend.utils.rule_fetch.time.sleep', lambda _: None)
    monkeypatch.setattr(requests, 'get', fetch)
    config = {'system_config': {'github_proxy_domain': 'https://mirror.test/'}}
    with caplog.at_level(logging.INFO, logger='backend.utils.rule_fetch'):
        request_rule(source, config_data=config)
    messages = _messages(caplog)
    assert len(messages) == 3
    assert '1 秒后重试第 1 次' in messages[0] and '2 秒后重试第 2 次' in messages[1]
    assert '改用GitHub 代理地址' in messages[2] and 'raw.githubusercontent.com' in messages[2]
    assert 'dl-secret' not in caplog.text


def test_subscription_node_count_change_is_logged(tmp_path, monkeypatch, caplog):
    from backend.common import config as config_module
    from backend.utils import subscription_cache
    monkeypatch.setattr(config_module, '_repository', ProfileRepository(tmp_path))
    with caplog.at_level(logging.INFO, logger='backend.utils.subscription_cache'):
        subscription_cache.save_subscription_nodes('sub1', [{}, {}], {'subscription_name': '机场 A'})
        subscription_cache.save_subscription_nodes('sub1', [{}, {}], {'subscription_name': '机场 A'})
        subscription_cache.save_subscription_nodes('sub1', [{}], {'subscription_name': '机场 A'})
    assert _messages(caplog) == ['订阅 机场 A 节点数变化：2 → 1']
