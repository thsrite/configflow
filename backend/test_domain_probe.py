"""域名发现 P1：主动探测判定、探测任务、历史与撤销、自动探测 / 采纳 / 复检。"""
import time
from datetime import datetime, timedelta

import pytest
from flask import Flask

from backend.agents.manager import AgentManager
from backend.common import config as config_module
from backend.common.agent_manager import init_agent_manager
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints
from backend.utils import domain_discovery_service as service
from backend.utils.domain_probe import ProbeStore, domain_suggestion, judge, probe_url


def stats(ok, total, delays=None):
    return {'ok': ok, 'total': total, 'delays': delays if delays is not None else [100] * ok,
            'errors': {'timeout': total - ok} if total > ok else {}}


# ---------- 判定 ----------

@pytest.mark.parametrize('direct, proxy, kwargs, verdict, target, confidence', [
    (stats(0, 2), stats(2, 2), {}, 'needs_proxy', 'proxy', 85),
    (stats(0, 2), stats(2, 2), {'passive_failing': True}, 'needs_proxy', 'proxy', 99),
    (stats(0, 3), stats(2, 3), {}, 'needs_proxy', 'proxy', 70),
    (stats(0, 2), stats(2, 2), {'direct_healthy': False}, 'direct_down', None, 0),
    (stats(2, 2), stats(0, 2), {}, 'direct_only', 'direct', 85),
    (stats(2, 2), stats(0, 2), {'proxy_healthy': False}, 'proxy_down', None, 0),
    (stats(2, 2, [100, 120]), stats(2, 2, [300, 300]), {}, 'both_ok', 'direct', 60),
    (stats(2, 2, [900, 900]), stats(2, 2, [200, 200]), {}, 'both_ok', 'proxy', 50),
    (stats(0, 2), stats(0, 2), {}, 'unreachable', None, 0),
    (stats(0, 2), stats(0, 2), {'proxy_healthy': False}, 'proxy_down', None, 0),
    (stats(1, 3), stats(1, 3), {}, 'flaky', None, 0),
    (stats(1, 1), {'ok': 0, 'total': 0, 'delays': [], 'errors': {}}, {}, 'incomplete', None, 0),
])
def test_judge(direct, proxy, kwargs, verdict, target, confidence):
    result = judge(direct, proxy, **kwargs)
    assert (result['verdict'], result['target'], result['confidence']) == (verdict, target, confidence)
    assert result['reasons']


def test_judge_explains_missing_proxy_path():
    result = judge(stats(1, 1), {'ok': 0, 'total': 1, 'delays': [], 'errors': {'no_path': 1}})
    assert result['verdict'] == 'incomplete' and '部署配置' in result['reasons'][0]


def test_probe_url_follows_port():
    assert probe_url('a.com', 443) == 'https://a.com/'
    assert probe_url('a.com', 0) == 'https://a.com/'
    assert probe_url('a.com', 80) == 'http://a.com/'
    assert probe_url('a.com', 8443) == 'https://a.com:8443/'


def test_probe_store_keeps_latest_and_expires(tmp_path):
    store = ProbeStore(tmp_path)
    old = datetime(2026, 8, 1)
    store.save('a1', {'old.com': {'checked_at': old.isoformat(), 'verdict': 'unreachable'}}, now=old)
    now = datetime(2026, 10, 10)
    store.save('a1', {'new.com': {'checked_at': now.isoformat(), 'verdict': 'needs_proxy'}}, now=now)
    assert set(store.load('a1', now=now)) == {'new.com'}


def test_domain_suggestion_prefers_highest_confidence():
    hosts = [
        {'probe': {'host': 'a', 'verdict': 'both_ok', 'target': 'direct', 'confidence': 60, 'checked_at': '2'}},
        {'probe': {'host': 'b', 'verdict': 'needs_proxy', 'target': 'proxy', 'confidence': 85, 'checked_at': '1'}},
        {'probe': None},
    ]
    assert domain_suggestion(hosts)['host'] == 'b'
    only_failed = [{'probe': {'host': 'c', 'verdict': 'unreachable', 'target': None, 'confidence': 0, 'checked_at': '3'}}]
    assert domain_suggestion(only_failed)['verdict'] == 'unreachable'
    assert domain_suggestion([{'probe': None}]) is None


# ---------- 端到端（假 Agent） ----------

def _report(*items):
    return {"schema": 1, "status": "running", "items": list(items)}


def _item(host, **overrides):
    item = {"host": host, "port": 443, "network": "tcp", "rule": "Match", "rule_payload": "",
            "policy": "DIRECT", "outlet": "direct", "conns": 1, "fails": 0, "fail_kinds": {},
            "zero_dl": 0, "up": 0, "down": 0}
    item.update(overrides)
    return item


class FakeAgent:
    """按域名关键字模拟探测结果；记录每次调用的请求。"""

    def __init__(self):
        self.calls = []
        self.proxy_down = False

    def __call__(self, agent, payload, timeout=200):
        self.calls.append((agent['id'], payload))
        proxy_path = payload['paths'][1]
        results = []
        for target in payload['targets']:
            host = target['host']
            if host == 'www.gstatic.com':
                direct, proxy = stats(2, 2), (stats(0, 2) if self.proxy_down else stats(2, 2))
            elif 'blocked' in host:
                direct, proxy = stats(0, 2), (stats(0, 2) if self.proxy_down else stats(2, 2))
            elif 'cnonly' in host:
                direct, proxy = stats(2, 2), stats(0, 2)
            elif 'dead' in host:
                direct, proxy = stats(0, 2), stats(0, 2)
            else:
                direct, proxy = stats(2, 2, [50, 50]), (stats(0, 2) if self.proxy_down else stats(2, 2, [300, 300]))
            results.append({'host': host, 'url': target['url'], 'paths': {'DIRECT': direct, proxy_path: proxy}})
        return {'success': True, 'results': results}


@pytest.fixture
def env(tmp_path, monkeypatch):
    repository = ProfileRepository(tmp_path)
    repository.save_profile("default", {
        "proxy_groups": [{"id": "g1", "name": "🚀 节点选择", "type": "select"}],
        "rule_configs": [{"id": "m", "itemType": "rule", "rule_type": "MATCH", "value": "", "policy": "DIRECT",
                          "enabled": True}],
    })
    config_module.set_repository(repository)
    manager = init_agent_manager()
    agent = manager.register_agent({"name": "gw", "host": "127.0.0.1", "service_type": "mihomo"})
    app = Flask(__name__)
    register_blueprints(app)
    client = app.test_client()
    auth = {"Authorization": f"Bearer {agent['token']}"}
    client.post(f"/api/agents/{agent['id']}/heartbeat", json={"version": "1.5.0-go"}, headers=auth)
    client.put(f"/api/agents/{agent['id']}/domain-discovery", json={"enabled": True})
    fake = FakeAgent()
    monkeypatch.setattr(AgentManager, 'probe_domains', fake)
    with service._JOBS_LOCK:
        service._JOBS.clear()
    return client, repository, agent, auth, fake


def _init(client):
    client.post("/api/domain-discovery/rulesets/init", json={"proxy_policy": "🚀 节点选择"})


def _send(client, agent, auth, *items):
    response = client.post(f"/api/agents/{agent['id']}/traffic-report", json=_report(*items), headers=auth)
    assert response.status_code == 200


def test_probe_requires_default_proxy_ruleset_and_capable_agent(env):
    client, _, agent, auth, _ = env
    _send(client, agent, auth, _item("www.blocked.com", conns=0, fails=5))
    with pytest.raises(service.ProbeError, match='默认代理规则集'):
        service.start_probe_job('default', ['blocked.com'], wait=True)

    _init(client)
    client.post(f"/api/agents/{agent['id']}/heartbeat", json={"version": "1.4.0-go"}, headers=auth)
    with pytest.raises(service.ProbeError, match='1.5.0-go'):
        service.start_probe_job('default', ['blocked.com'], wait=True)


def test_probe_job_builds_batches_with_controls_and_saves_verdicts(env):
    client, _, agent, auth, fake = env
    _init(client)
    _send(client, agent, auth,
          _item("www.blocked.com", conns=0, fails=5), _item("api.blocked.com", port=80),
          _item("x.cnonly.cn"), _item("fine.example.org"))

    response = client.post("/api/domain-discovery/probe",
                           json={"domains": ["blocked.com", "cnonly.cn", "fine.example.org", "never-seen.net"]})
    assert response.status_code == 202
    job_id = response.get_json()["job"]["id"]
    for _ in range(100):
        job = client.get(f"/api/domain-discovery/probe/{job_id}").get_json()["job"]
        if job["status"] != "running":
            break
        time.sleep(0.02)
    assert job["status"] == "done" and job["total"] == 5 and job["done"] == 5

    agent_id, payload = fake.calls[0]
    assert agent_id == agent['id']
    assert payload['paths'] == ['DIRECT', '🚀 节点选择']
    assert [target['host'] for target in payload['targets'][:2]] == ['www.baidu.com', 'www.gstatic.com']
    urls = {target['host']: target['url'] for target in payload['targets']}
    assert urls['api.blocked.com'] == 'http://api.blocked.com/'
    assert urls['never-seen.net'] == 'https://never-seen.net/'

    items = {item['domain']: item for item in client.get("/api/domain-discovery/domains?view=all").get_json()["items"]}
    blocked = items['blocked.com']['suggestion']
    assert blocked['verdict'] == 'needs_proxy' and blocked['target'] == 'proxy' and blocked['confidence'] == 99
    assert items['cnonly.cn']['suggestion']['target'] == 'direct'
    assert items['example.org']['suggestion']['verdict'] == 'both_ok'


def test_probe_job_rejects_concurrent_jobs(env):
    client, _, _, _, _ = env
    with service._JOBS_LOCK:
        service._JOBS['busy'] = {'id': 'busy', 'profile_id': 'default', 'status': 'running', 'total': 1, 'done': 0,
                                 'errors': [], 'message': '', 'started_at': '', 'finished_at': None, 'results': {}}
    response = client.post("/api/domain-discovery/probe", json={"domains": ["a.com"]})
    assert response.status_code == 409 and response.get_json()["job"]["id"] == "busy"
    assert client.post("/api/domain-discovery/probe", json={"domains": ["bad host"]}).status_code == 400


def test_proxy_down_control_prevents_direct_only_verdict(env):
    client, _, agent, auth, fake = env
    _init(client)
    _send(client, agent, auth, _item("fine.example.org"))
    fake.proxy_down = True
    service.start_probe_job('default', ['fine.example.org'], wait=True)
    item = client.get("/api/domain-discovery/domains?view=all").get_json()["items"][0]
    assert item['suggestion']['verdict'] == 'proxy_down' and item['suggestion']['target'] is None


def test_apply_records_history_with_evidence_and_undo_removes_line(env):
    client, repository, agent, auth, _ = env
    _init(client)
    _send(client, agent, auth, _item("www.blocked.com", conns=0, fails=5))
    service.start_probe_job('default', ['blocked.com'], wait=True)

    client.post("/api/domain-discovery/apply", json={"items": [{"value": "blocked.com", "target": "proxy"}]})
    history = client.get("/api/domain-discovery/history").get_json()["items"]
    assert len(history) == 1
    assert history[0]['source'] == 'manual' and history[0]['verdict'] == 'needs_proxy'
    assert history[0]['ruleset'] == '域名发现-代理'

    response = client.post("/api/domain-discovery/undo", json={"value": "blocked.com", "target": "proxy"}).get_json()
    assert response['removed'] == ['DOMAIN-SUFFIX,blocked.com']
    library = {item['name']: item for item in repository.get_shared()['rule_library']}
    assert library['域名发现-代理']['content'] == ''
    assert client.get("/api/domain-discovery/history").get_json()["items"] == []


def test_settings_validate_auto_fields_and_keep_other_settings(env):
    client, repository, _, _, _ = env
    _init(client)
    client.post("/api/domain-discovery/ignore", json={"values": ["noise.dev"]})
    assert client.put("/api/domain-discovery/settings", json={"auto_apply_min_confidence": 70}).status_code == 400
    assert client.put("/api/domain-discovery/settings", json={"auto_probe": "yes"}).status_code == 400
    body = client.put("/api/domain-discovery/settings",
                      json={"auto_probe": True, "auto_apply": True, "auto_apply_min_confidence": 95}).get_json()
    assert body['auto_probe'] is True and body['auto_apply_min_confidence'] == 95
    assert body['probe_path'] == '🚀 节点选择'
    stored = repository.get_profile("default")["domain_discovery"]
    assert stored['ignored'] == ['noise.dev'] and stored['proxy_ruleset']


def test_auto_round_probes_applies_above_threshold_and_skips_recent(env):
    client, repository, agent, auth, fake = env
    _init(client)
    _send(client, agent, auth, _item("www.blocked.com", conns=0, fails=5), _item("fine.example.org"))
    assert service.auto_round('default') == {'probed': 0, 'applied': 0, 'rechecked': 0}

    client.put("/api/domain-discovery/settings", json={"auto_probe": True, "auto_apply": True,
                                                       "auto_apply_min_confidence": 90})
    outcome = service.auto_round('default')
    assert outcome['probed'] == 2 and outcome['applied'] == 1
    library = {item['name']: item for item in repository.get_shared()['rule_library']}
    assert library['域名发现-代理']['content'] == 'DOMAIN-SUFFIX,blocked.com\n'
    assert library['域名发现-直连']['content'] == ''  # both_ok 置信度 60，低于阈值
    history = client.get("/api/domain-discovery/history").get_json()["items"]
    assert history[0]['source'] == 'auto' and history[0]['confidence'] == 99

    calls = len(fake.calls)
    assert service.auto_round('default')['probed'] == 0  # 3 天内探测过的不再探测
    assert len(fake.calls) == calls


def test_auto_round_rechecks_old_entries(env):
    client, repository, agent, auth, _ = env
    _init(client)
    client.put("/api/domain-discovery/settings", json={"auto_probe": True})
    client.post("/api/domain-discovery/apply", json={"items": [
        {"value": "fine.example.org", "target": "proxy"},
        {"value": "blocked.net", "target": "proxy"},
    ]})
    old = (datetime.now() - timedelta(days=40)).isoformat(timespec='seconds')

    def age(profile):
        for entry in profile['domain_discovery']['history']:
            entry['applied_at'] = old
    config_module.update_config_transaction(age, 'default')

    outcome = service.auto_round('default')
    assert outcome['rechecked'] == 2
    history = {entry['value']: entry for entry in client.get("/api/domain-discovery/history").get_json()["items"]}
    assert history['fine.example.org']['recheck']['suggest_remove'] is True
    assert history['fine.example.org']['recheck']['reason'] == '直连已经可以正常访问'
    assert history['blocked.net']['recheck']['suggest_remove'] is False


def test_provider_revision_follows_served_file_not_config(env):
    """配置已提交但缓存文件还没写时，摘要不能变，否则 Agent 会让 Mihomo 拉到旧内容。"""
    import os
    from backend.utils.rule_utils import get_rules_dir
    client, repository, _, _, _ = env
    _init(client)
    before = service.provider_revisions('default')

    def add_line(shared):
        for item in shared['rule_library']:
            if item['name'] == '域名发现-代理':
                item['content'] = 'DOMAIN-SUFFIX,late.com\n'
    config_module.update_shared_config_transaction(add_line)
    assert service.provider_revisions('default') == before

    with open(os.path.join(get_rules_dir(), '域名发现-代理.list'), 'w', encoding='utf-8') as handle:
        handle.write('DOMAIN-SUFFIX,late.com\n')
    assert service.provider_revisions('default')['域名发现-代理'] != before['域名发现-代理']


def test_busy_job_from_another_profile_is_not_exposed(env):
    client, repository, _, _, _ = env
    repository.create_profile({'id': 'other', 'name': 'Other'})
    with service._JOBS_LOCK:
        service._JOBS['busy'] = {'id': 'busy', 'profile_id': 'default', 'kind': 'probe', 'status': 'running',
                                 'total': 1, 'done': 0, 'errors': [], 'message': '', 'started_at': '',
                                 'finished_at': None, 'results': {}}
    response = client.post("/api/domain-discovery/probe", json={"domains": ["a.com"]},
                           headers={"X-ConfigFlow-Profile": "other"})
    body = response.get_json()
    assert response.status_code == 409 and body["job"] is None and "另一个配置空间" in body["message"]


def test_recheck_without_result_is_recorded_once(env, monkeypatch):
    client, _, _, _, fake = env
    _init(client)
    client.put("/api/domain-discovery/settings", json={"auto_probe": True})
    client.post("/api/domain-discovery/apply", json={"items": [{"value": "blocked.net", "target": "proxy"}]})
    old = (datetime.now() - timedelta(days=40)).isoformat(timespec='seconds')

    def age(profile):
        for entry in profile['domain_discovery']['history']:
            entry['applied_at'] = old
    config_module.update_config_transaction(age, 'default')

    # Agent 对这批目标什么都没返回：结果数量不符
    monkeypatch.setattr(AgentManager, 'probe_domains', lambda self, agent, payload, timeout=200: {'success': True, 'results': []})
    assert service.auto_round('default')['rechecked'] == 1
    entry = client.get("/api/domain-discovery/history").get_json()["items"][0]
    assert entry['recheck']['reason'] == '复检没有得到探测结果' and entry['recheck']['suggest_remove'] is False

    revision = config_module.get_repository().get_profile('default')['_revision']
    assert service.auto_round('default')['rechecked'] == 0
    assert config_module.get_repository().get_profile('default')['_revision'] == revision
