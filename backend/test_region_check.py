"""域名发现 P2：服务解锁判定、域名地区差异、目标选择、配置注入、检测任务与指定走向。"""
import yaml
import pytest
from flask import Flask

from backend.agents.manager import AgentManager
from backend.common import config as config_module
from backend.common.agent_manager import init_agent_manager
from backend.common.config_repository import ProfileRepository
from backend.converters.mihomo import REGION_PROBE_GROUP, REGION_PROBE_LISTENER, generate_mihomo_config
from backend.routes import register_blueprints
from backend.utils import domain_discovery_service as service
from backend.utils import region_check as region


def res(request_id, status=200, final_url=None, redirects=None, markers=None, error=''):
    return {'id': request_id, 'status': status, 'final_url': final_url or f'https://x.test/{request_id}',
            'redirects': redirects or [], 'markers': markers or {}, 'error': error, 'elapsed_ms': 10}


def evaluate(service_id, *results):
    return region.SERVICES_BY_ID[service_id]['evaluate']({item['id']: item for item in results})


# ---------- 服务判定 ----------

def test_openai_observed_datacenter_response_is_available_with_note():
    # 49 实测：compliance 200 无 unsupported，ios 返回 403 {"type":"dc"}
    result = evaluate('openai',
                      res('openai-compliance', markers={'consent': 'cookie_consent_required'}),
                      res('openai-ios', 403, markers={'dc': '"type":"dc"'}),
                      res('openai-trace', markers={'loc': 'US'}))
    assert result['status'] == 'available' and result['region'] == 'US'
    assert '数据中心' in result['reason']


@pytest.mark.parametrize('compliance_markers, ios_markers, status, reason', [
    ({'unsupported': 'unsupported_country'}, {}, 'blocked', '所在地区不受支持'),
    ({'consent': 'x'}, {'vpn': 'VPN'}, 'blocked', '被识别为代理 / VPN'),
    ({}, {}, 'unknown', '响应不符合已知模式'),
])
def test_openai_blocked_and_unknown(compliance_markers, ios_markers, status, reason):
    result = evaluate('openai', res('openai-compliance', markers=compliance_markers), res('openai-ios', markers=ios_markers))
    assert (result['status'], result['reason']) == (status, reason)


def test_claude():
    blocked = evaluate('claude', res('claude-web', final_url='https://www.anthropic.com/app-unavailable-in-region',
                                     redirects=['https://claude.ai/']), res('claude-trace', markers={'loc': 'HK'}))
    assert blocked['status'] == 'blocked' and blocked['region'] == 'HK'
    assert evaluate('claude', res('claude-web'))['status'] == 'available'
    challenge = evaluate('claude', res('claude-web', 403))  # 49 实测：Cloudflare 质询
    assert challenge['status'] == 'unknown' and 'Cloudflare' in challenge['reason']
    assert evaluate('claude', res('claude-web', error='timeout'))['status'] == 'unknown'


def test_netflix():
    full = evaluate('netflix',
                    res('netflix-licensed-1', final_url='https://www.netflix.com/hk-en/title/81280792',
                        redirects=['https://www.netflix.com/title/81280792']),
                    res('netflix-licensed-2', 404), res('netflix-original'))
    assert (full['status'], full['region'], full['reason']) == ('available', 'HK', '完整解锁')
    originals = evaluate('netflix', res('netflix-licensed-1', 404), res('netflix-licensed-2', 404),
                         res('netflix-original', final_url='https://www.netflix.com/title/80018499'))
    assert (originals['status'], originals['region']) == ('partial', 'US')
    assert evaluate('netflix', res('netflix-licensed-1', 403), res('netflix-licensed-2', 403),
                    res('netflix-original', 403))['status'] == 'blocked'
    assert evaluate('netflix', res('netflix-licensed-1', error='timeout'))['status'] == 'unknown'
    # 出错的请求即使带着状态码也不能当成成功
    assert evaluate('claude', res('claude-web', 200, error='tls'))['status'] == 'unknown'


def test_youtube():
    assert evaluate('youtube', res('youtube-premium', markers={'gl': 'US'}))['region'] == 'US'
    assert evaluate('youtube', res('youtube-premium', markers={'blocked': 'x', 'gl': 'HK'}))['status'] == 'blocked'
    assert evaluate('youtube', res('youtube-premium', markers={'gl': 'CN'}))['status'] == 'blocked'
    assert evaluate('youtube', res('youtube-premium'))['status'] == 'unknown'


def test_service_requests_fit_agent_limits():
    requests = region.service_requests()
    assert requests[0]['id'] == 'exit' and len(requests) <= 12
    assert len({item['id'] for item in requests}) == len(requests)
    for item in requests:
        assert len(item.get('markers', {})) <= 8


# ---------- 域名地区差异 ----------

def test_classify_domain_result():
    url = 'https://shop.test/'
    assert region.classify_domain_result(res('d', 451), url)['state'] == 'restricted'
    assert region.classify_domain_result(res('d', 403, markers={'block': 'not available in your country'}), url)['state'] == 'restricted'
    assert region.classify_domain_result(res('d', 200, final_url='https://shop.test/unavailable-region'), url)['state'] == 'restricted'
    assert region.classify_domain_result(res('d', 200, final_url='https://shop.test/'), url)['state'] == 'ok'
    assert region.classify_domain_result(res('d', 403, final_url='https://shop.test/'), url)['state'] == 'denied'
    assert region.classify_domain_result(res('d', error='timeout'), url)['state'] == 'error'
    assert region.classify_domain_result(None, url)['state'] == 'error'


@pytest.mark.parametrize('states, verdict', [
    (['restricted', 'ok'], 'suspected'),
    (['restricted', 'restricted'], 'all_restricted'),
    (['ok', 'denied'], 'no_difference'),
    (['error', 'error'], 'unreachable'),
])
def test_summarize_domain(states, verdict):
    per_target = {f't{i}': {'state': state} for i, state in enumerate(states)}
    assert region.summarize_domain(per_target)['verdict'] == verdict


# ---------- 目标选择 ----------

def test_region_of_name():
    assert region.region_of_name('🇺🇸 美国 01') == 'US'
    assert region.region_of_name('HK-IEPL-02') == 'HK'
    assert region.region_of_name('RUSSIA 01') is None
    assert region.region_of_name('Tokyo 03') == 'JP'


def test_pick_targets():
    targets = [
        {'name': 'DIRECT', 'type': 'Direct', 'alive': True},
        {'name': 'GLOBAL', 'type': 'Selector', 'alive': True},
        {'name': 'ConfigFlow-Region-Probe', 'type': 'Selector', 'alive': True},
        {'name': '自动选择', 'type': 'URLTest', 'alive': True},
        {'name': '🚀 节点选择', 'type': 'Selector', 'alive': True},
        {'name': '🇭🇰 香港 01', 'type': 'Shadowsocks', 'alive': False},
        {'name': '🇭🇰 香港 02', 'type': 'Shadowsocks', 'alive': True},
        {'name': '🇺🇸 美国 01', 'type': 'Vmess', 'alive': True},
        {'name': '🇺🇸 美国 02', 'type': 'Vmess', 'alive': True},
        {'name': '未知节点', 'type': 'Trojan', 'alive': True},
    ]
    picked = region.pick_targets(targets, preferred_group='🚀 节点选择')
    assert [(item['name'], item['kind']) for item in picked] == [
        ('DIRECT', 'direct'), ('🚀 节点选择', 'group'), ('自动选择', 'group'),
        ('🇭🇰 香港 02', 'node'), ('🇺🇸 美国 01', 'node'),
    ]


# ---------- 配置注入 ----------

def _mihomo(discovery, custom=''):
    return yaml.safe_load(generate_mihomo_config({
        'proxy_groups': [{'id': 'g1', 'name': 'PROXY', 'type': 'select', 'proxies': ['DIRECT']}],
        'rule_configs': [{'id': 'm', 'itemType': 'rule', 'rule_type': 'MATCH', 'value': '', 'policy': 'DIRECT'}],
        'mihomo': {'custom_config': custom or 'mixed-port: 7890\nlisteners:\n  - {name: lan, type: socks, port: 7891}\n'},
        'domain_discovery': discovery,
    }, region_probe=True, preflight_providers=False))


def test_probe_entry_injected_only_when_enabled_for_agents():
    config = _mihomo({'region_check_enabled': True, 'region_probe_port': 18999})
    group = next(item for item in config['proxy-groups'] if item['name'] == REGION_PROBE_GROUP)
    assert group['type'] == 'select' and group['include-all'] and group['hidden']
    assert group['proxies'] == ['DIRECT', 'PROXY']
    assert [item['name'] for item in config['listeners']] == ['lan', REGION_PROBE_LISTENER]
    probe = config['listeners'][1]
    assert (probe['listen'], probe['port'], probe['proxy']) == ('127.0.0.1', 18999, REGION_PROBE_GROUP)

    assert 'listeners' not in _mihomo({}, custom='mixed-port: 7890\n')
    plain = yaml.safe_load(generate_mihomo_config({
        'proxy_groups': [], 'rule_configs': [], 'mihomo': {'custom_config': 'mixed-port: 7890\n'},
        'domain_discovery': {'region_check_enabled': True}}, preflight_providers=False))
    assert 'listeners' not in plain  # 订阅等非 Agent 配置不注入

    conflict = _mihomo({'region_check_enabled': True, 'region_probe_port': 7891})
    assert REGION_PROBE_LISTENER not in [item['name'] for item in conflict['listeners']]


# ---------- 检测任务与指定走向 ----------

class FakeRegionAgent:
    def __init__(self):
        self.calls = []

    def targets(self, agent):
        return [
            {'name': 'DIRECT', 'type': 'Direct', 'alive': True},
            {'name': 'PROXY', 'type': 'Selector', 'alive': True},
            {'name': '🇭🇰 香港 01', 'type': 'Socks5', 'alive': True},
            {'name': '🇺🇸 美国 01', 'type': 'Socks5', 'alive': True},
        ]

    def check(self, agent, payload):
        assert len(payload['requests']) <= 12
        self.calls.append(payload)
        target = payload['target']
        loc = {'DIRECT': 'CN', 'PROXY': 'HK', '🇭🇰 香港 01': 'HK', '🇺🇸 美国 01': 'US'}[target]
        results = []
        for item in payload['requests']:
            rid = item['id']
            if rid == 'exit':
                results.append(res(rid, markers={'loc': loc, 'ip': '1.2.3.4'}))
            elif rid == 'openai-compliance':
                markers = {'unsupported': 'unsupported_country'} if loc in ('CN', 'HK') else {'consent': 'x'}
                results.append(res(rid, markers=markers))
            elif rid == 'openai-trace':
                results.append(res(rid, markers={'loc': loc}))
            elif rid.startswith('domain-'):
                if loc == 'US':
                    results.append(res(rid, 200, final_url=item['url']))
                else:
                    results.append(res(rid, 403, final_url=item['url'], markers={'block': 'not available in your country'}))
            else:
                results.append(res(rid, error='timeout'))
        return {'success': True, 'results': results}


@pytest.fixture
def env(tmp_path, monkeypatch):
    repository = ProfileRepository(tmp_path)
    repository.save_profile('default', {
        'proxy_groups': [{'id': 'g1', 'name': 'PROXY', 'type': 'select'}, {'id': 'g2', 'name': '🇺🇸 美国', 'type': 'select'}],
        'rule_configs': [{'id': 'm', 'itemType': 'rule', 'rule_type': 'MATCH', 'value': '', 'policy': 'DIRECT',
                          'enabled': True}],
    })
    config_module.set_repository(repository)
    manager = init_agent_manager()
    agent = manager.register_agent({'name': 'gw', 'host': '127.0.0.1', 'service_type': 'mihomo'})
    app = Flask(__name__)
    register_blueprints(app)
    client = app.test_client()
    auth = {'Authorization': f"Bearer {agent['token']}"}
    client.post(f"/api/agents/{agent['id']}/heartbeat", json={'version': '1.6.0-go'}, headers=auth)
    client.put(f"/api/agents/{agent['id']}/domain-discovery", json={'enabled': True})
    fake = FakeRegionAgent()
    monkeypatch.setattr(AgentManager, 'region_targets', lambda self, agent: fake.targets(agent))
    monkeypatch.setattr(AgentManager, 'region_check', lambda self, agent, payload: fake.check(agent, payload))
    with service._JOBS_LOCK:
        service._JOBS.clear()
    return client, repository, agent, auth, fake


def test_region_settings_validate_port(env):
    client, repository, _, _, _ = env
    repository.update_profile_fields('default', {'mihomo': {'custom_config': 'mixed-port: 7890\n'}})
    assert client.put('/api/domain-discovery/settings', json={'region_probe_port': 80}).status_code == 400
    assert client.put('/api/domain-discovery/settings', json={'region_probe_port': 7890}).status_code == 400
    body = client.put('/api/domain-discovery/settings', json={'region_check_enabled': True, 'region_probe_port': 18000}).get_json()
    assert body['region_check_enabled'] is True and body['region_probe_port'] == 18000


def test_region_check_requires_enable(env):
    client, _, _, _, _ = env
    with pytest.raises(service.ProbeError, match='开启区域检测'):
        service.start_region_job('default', wait=True)


def test_region_check_builds_matrix_and_domain_verdicts(env):
    client, _, agent, auth, fake = env
    client.put('/api/domain-discovery/settings', json={'region_check_enabled': True})
    client.post(f"/api/agents/{agent['id']}/traffic-report", headers=auth, json={'schema': 1, 'status': 'running', 'items': [
        {'host': 'www.shop-demo.com', 'port': 443, 'rule': 'Match', 'policy': 'DIRECT', 'outlet': 'direct', 'conns': 3}]})

    response = client.post('/api/domain-discovery/region/check', json={'services': True, 'domains': ['shop-demo.com', 'a-demo.com', 'b-demo.com']})
    assert response.status_code == 202
    job_id = response.get_json()['job']['id']
    for _ in range(100):
        job = client.get(f'/api/domain-discovery/probe/{job_id}').get_json()['job']
        if job['status'] != 'running':
            break
    assert job['status'] == 'done' and job['kind'] == 'region' and job['total'] == 4

    # 服务 10 个请求 + 3 个域名请求，超过 12 个，每个目标拆成两批
    assert len(fake.calls) == 8
    body = client.get('/api/domain-discovery/region').get_json()
    result = body['result']
    assert [target['name'] for target in result['targets']] == ['DIRECT', 'PROXY', '🇭🇰 香港 01', '🇺🇸 美国 01']
    assert result['targets'][3]['exit']['loc'] == 'US'
    assert result['matrix']['🇺🇸 美国 01']['openai']['status'] == 'available'
    assert result['matrix']['🇭🇰 香港 01']['openai']['status'] == 'blocked'
    assert result['matrix']['DIRECT']['claude']['status'] == 'unknown'
    shop = body['domains']['www.shop-demo.com']
    assert shop['verdict'] == 'suspected' and shop['usable'] == ['🇺🇸 美国 01']
    assert shop['url'] == 'https://www.shop-demo.com/'
    assert set(body['domains']) == {'www.shop-demo.com', 'a-demo.com', 'b-demo.com'}


def test_route_service_to_policy_creates_ruleset_and_undo(env):
    client, repository, _, _, _ = env
    client.post('/api/domain-discovery/rulesets/init', json={'proxy_policy': 'PROXY'})
    assert client.post('/api/domain-discovery/region/route',
                       json={'kind': 'service', 'value': 'openai', 'policy': '不存在'}).status_code == 400

    body = client.post('/api/domain-discovery/region/route',
                       json={'kind': 'service', 'value': 'openai', 'policy': '🇺🇸 美国'}).get_json()
    assert [item['value'] for item in body['added']] == region.SERVICES_BY_ID['openai']['domains']
    library = {item['name']: item for item in repository.get_shared()['rule_library']}
    assert library['域名发现-🇺🇸 美国']['content'].startswith('DOMAIN-SUFFIX,openai.com\n')
    rules = repository.get_profile('default')['rule_configs']
    names = {item['id']: item['name'] for item in repository.get_shared()['rule_library']}
    order = [names.get(item.get('library_rule_id'), item.get('rule_type')) for item in rules]
    assert order == ['域名发现-直连', '域名发现-🇺🇸 美国', '域名发现-代理', 'MATCH']
    assert '域名发现-🇺🇸 美国' in service.provider_revisions('default')

    again = client.post('/api/domain-discovery/region/route',
                        json={'kind': 'domain', 'value': 'shop.test', 'policy': '🇺🇸 美国'}).get_json()
    assert again['added'][0]['value'] == 'shop.test'
    assert len(repository.get_profile('default')['rule_configs']) == 4  # 已有引用，不重复添加

    history = client.get('/api/domain-discovery/history').get_json()['items']
    entry = next(item for item in history if item['value'] == 'shop.test')
    assert entry['target'] == 'policy:🇺🇸 美国' and entry['source'] == 'region'
    undo = client.post('/api/domain-discovery/undo', json={'value': 'shop.test', 'target': 'policy:🇺🇸 美国'}).get_json()
    assert undo['removed'] == ['DOMAIN-SUFFIX,shop.test']


def test_route_places_ruleset_before_competing_user_rule(env):
    client, repository, _, _, _ = env
    client.post('/api/domain-discovery/rulesets/init', json={'proxy_policy': 'PROXY'})

    def add_user_rule(profile):
        profile['rule_configs'].insert(0, {'id': 'user-openai', 'itemType': 'rule', 'rule_type': 'DOMAIN-SUFFIX',
                                           'value': 'chatgpt.com', 'policy': 'PROXY', 'enabled': True})
    config_module.update_config_transaction(add_user_rule, 'default')

    body = client.post('/api/domain-discovery/region/route',
                       json={'kind': 'service', 'value': 'openai', 'policy': '🇺🇸 美国'}).get_json()
    assert body['warnings'] == []
    rules = repository.get_profile('default')['rule_configs']
    names = {item['id']: item['name'] for item in repository.get_shared()['rule_library']}
    order = [names.get(item.get('library_rule_id'), item.get('id')) for item in rules]
    assert order[:2] == ['域名发现-🇺🇸 美国', 'user-openai']


def test_apply_warns_when_an_earlier_rule_still_matches(env):
    client, _, _, _, _ = env
    client.post('/api/domain-discovery/rulesets/init', json={'proxy_policy': 'PROXY'})

    def add_user_rule(profile):
        profile['rule_configs'].insert(0, {'id': 'user-direct', 'itemType': 'rule', 'rule_type': 'DOMAIN-KEYWORD',
                                           'value': 'blocked', 'policy': 'DIRECT', 'enabled': True})
    config_module.update_config_transaction(add_user_rule, 'default')

    body = client.post('/api/domain-discovery/apply', json={'items': [
        {'value': 'blocked.example.com', 'target': 'proxy'}, {'value': 'fine.example.com', 'target': 'proxy'}]}).get_json()
    assert len(body['warnings']) == 1
    assert 'blocked.example.com → 第 1 条' in body['warnings'][0] and 'fine.example.com' not in body['warnings'][0]


@pytest.mark.parametrize('store_path, factory, empty', [
    ('backend.agents.traffic_store', 'TrafficStore', lambda store: store.load('a1')['days'] == {}),
    ('backend.utils.domain_probe', 'ProbeStore', lambda store: store.load('a1') == {}),
    ('backend.utils.region_check', 'RegionStore', lambda store: store.load('a1')['domains'] == {}),
])
def test_stores_treat_corrupt_files_as_empty(tmp_path, store_path, factory, empty):
    import importlib
    store = getattr(importlib.import_module(store_path), factory)(tmp_path)
    (tmp_path / 'a1.json').write_text('{not json', encoding='utf-8')
    assert empty(store)
    store.delete('a1')
    assert not (tmp_path / 'a1.json').exists()
