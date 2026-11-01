"""Upstream routers awaiting a release, for the reminder on pages that edit Mihomo."""
import threading
import time

import pytest

from backend.routers import pending
from backend.routers.base import NatBypassEntry, RouteEntry, RouterDriver, RouterError, RouterState
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401  (pytest fixture)

PATH = '/api/agents/{}/upstream-routers'


def stage(key, title, ops=(), **extra):
    return {'key': key, 'title': title, 'ops': [{'kind': kind, 'summary': kind} for kind in ops], **extra}


def plan(*stages, blockers=()):
    return {'stages': list(stages), 'blockers': list(blockers), 'warnings': [], 'empty': not any(s['ops'] for s in stages)}


# ---------------------------------------------------------------- summarize


def test_synced_needs_an_empty_preview_and_an_in_sync_report():
    result = pending.summarize({'in_sync': True}, plan(stage('routes', '主路由代理路由'), stage('nat_bypass', 'NAT')))
    assert result == {'state': 'synced', 'pending': 0, 'items': [], 'message': ''}


def test_pending_counts_the_preview_changes_and_words_ipv6_fakeip_like_the_report():
    report = {'in_sync': False, 'ipv6_fakeip': {'enabled': True, 'status': 'missing', 'items': []}}
    result = pending.summarize(report, plan(
        stage('return_routing', 'mihomo 回流规则', enabled=False),
        stage('routes', '主路由代理路由', ['route_add', 'route_add']),
        stage('ipv6_fakeip', 'IPv6 Fake-IP 分流路由', ['ipv6_fakeip_replace']),
        stage('nat_bypass', '主路由 NAT 免转换'),
        stage('dhcp_dns', 'DHCP DNS', ['dhcp_dns_update'], enabled=True)))
    assert result['state'] == 'pending' and result['pending'] == 4 and result['message'] == ''
    assert result['items'] == [
        {'key': 'routes', 'label': '主路由代理路由', 'status': '待添加', 'count': 2},
        {'key': 'ipv6_fakeip', 'label': 'IPv6 Fake-IP 分流路由', 'status': '待添加', 'count': 1},
        {'key': 'dhcp_dns', 'label': 'DHCP DNS', 'status': '待更新', 'count': 1}]


@pytest.mark.parametrize('section,key,ops,word', [
    ({'enabled': False, 'status': 'pending'}, 'ipv6_fakeip', ['ipv6_fakeip_replace'], '待撤回'),
    ({'enabled': True, 'status': 'pending'}, 'ipv6_fakeip', ['ipv6_fakeip_replace'], '待更新'),
    ({'enabled': True, 'status': 'synced'}, 'ipv6_fakeip', ['ipv6_fakeip_replace'], '待更新'),
    ({'enabled': True, 'items': [{'status': 'missing'}, {'status': 'borrowed'}]}, 'ipv6_routes',
     ['ipv6_routes_replace'], '待添加'),
    ({'enabled': True, 'items': [{'status': 'missing'}, {'status': 'pending'}]}, 'ipv6_routes',
     ['ipv6_routes_replace'], '待更新'),
    ({'enabled': False, 'items': []}, 'ipv6_routes', ['ipv6_routes_replace'], '待撤回'),
    ({'enabled': False}, 'dns_guard', ['dns_guard_replace'], '待撤回'),
    ({'enabled': True}, 'ipv6_dns', ['ipv6_dns_update'], '待关闭'),
    ({'enabled': True, 'items': [{'status': 'missing'}, {'status': 'missing'}, {'status': 'synced'}]},
     'ipv6_pd_guard', ['ipv6_pd_guard_sync'], '待安装'),
    ({'enabled': True, 'items': [{'status': 'adopt'}, {'status': 'adopt'}]}, 'ipv6_pd_guard',
     ['ipv6_pd_guard_sync'], '待接管'),
    ({'enabled': True, 'items': [{'status': 'missing'}, {'status': 'update'}]}, 'ipv6_pd_guard',
     ['ipv6_pd_guard_sync'], '待更新'),
    ({'enabled': False, 'items': [{'status': 'remove'}]}, 'ipv6_pd_guard', ['ipv6_pd_guard_restore'], '待撤回'),
    ({}, 'routes', ['route_delete', 'policy_route_delete'], '待移除'),
    ({}, 'routes', ['route_add', 'route_delete'], '待更新'),
    ({}, 'routes', ['route_comment'], '待接管'),
    ({}, 'return_cleanup', ['return_routing_remove'], '待移除'),
])
def test_item_status_words(section, key, ops, word):
    result = pending.summarize({'in_sync': False, key: section}, plan(stage(key, key, ops)))
    assert result['items'] == [{'key': key, 'label': key, 'status': word, 'count': len(ops)}]
    assert result['state'] == 'pending'


def test_preview_blockers_make_the_router_blocked_even_with_changes():
    result = pending.summarize({'in_sync': False}, plan(
        stage('routes', '主路由代理路由', ['route_add']),
        blockers=['IPv6 Fake-IP：代理网关不属于当前配置空间', '另一项']))
    assert result['state'] == 'blocked' and result['pending'] == 1
    assert result['message'] == 'IPv6 Fake-IP：代理网关不属于当前配置空间（另有 1 项）'


def test_a_skipped_stage_is_never_reported_as_synced():
    report = {'in_sync': False, 'ipv6_routes': {'enabled': True, 'skip': ['没有可用的 IPv6 下一跳'], 'items': []}}
    result = pending.summarize(report, plan(stage('routes', '主路由代理路由'),
                                            stage('ipv6_routes', 'IPv6 规则集路由', skipped=True)))
    assert result['state'] == 'blocked' and result['pending'] == 0
    assert result['message'] == '没有可用的 IPv6 下一跳'
    assert result['items'] == [{'key': 'ipv6_routes', 'label': 'IPv6 规则集路由', 'status': '本次跳过', 'count': 0}]
    # Other changes are still published: the router has items waiting.
    busy = pending.summarize(report, plan(stage('routes', '主路由代理路由', ['route_add']),
                                          stage('ipv6_routes', 'IPv6 规则集路由', skipped=True)))
    assert busy['state'] == 'pending' and busy['pending'] == 1 and len(busy['items']) == 2


def test_an_unsynchronised_report_without_changes_explains_itself():
    conflict = {'in_sync': False, 'routes': {'items': [
        {'cidr': '91.108.4.0/22', 'status': 'conflict', 'detail': '同网段路由下一跳为 192.0.2.9'}]}}
    result = pending.summarize(conflict, plan(stage('routes', '主路由代理路由')))
    assert result == {'state': 'blocked', 'pending': 0, 'items': [],
                      'message': '91.108.4.0/22：同网段路由下一跳为 192.0.2.9'}
    pd_guard = {'in_sync': False, 'ipv6_pd_guard': {'enabled': True, 'blockers': ['找到多个 IPv6 DHCP 客户端'], 'items': []}}
    skipped = pending.summarize(pd_guard, plan(stage('ipv6_pd_guard', 'IPv6 前缀变化保护', skipped=True)))
    assert skipped['state'] == 'blocked' and skipped['message'] == '找到多个 IPv6 DHCP 客户端'
    assert pending.summarize({'in_sync': False}, plan())['message'] == pending.GENERIC_BLOCKED
    assert pending.summarize({}, plan())['state'] == 'blocked'


# ---------------------------------------------------------------- API


@pytest.fixture
def lab(authenticated_app, monkeypatch):
    """Two profiles with routers on fake drivers; the cache starts empty."""
    client, manager, _, admin, _ = authenticated_app
    repository = manager.repository
    repository.create_profile({'id': 'home', 'name': 'Home'})
    repository.create_profile({'id': 'other', 'name': 'Other'})
    pending._cache.clear()
    monkeypatch.setattr(pending, 'ROUTER_TIMEOUT', 2)
    from backend.routes import routers as routes_module
    monkeypatch.setattr(routes_module, '_return_routing', lambda agent: {'status': 'ok', 'return_tables': []})
    states, reads = {}, []

    class Driver(RouterDriver):
        vendor = 'ikuai'
        supports_write = True

        def __init__(self, endpoint, username, password, verify_tls=True):
            self.endpoint = endpoint

        def read_state(self):
            reads.append(self.endpoint)
            value = states[self.endpoint]
            if isinstance(value, Exception):
                raise value
            if callable(value):
                return value()
            return value

    monkeypatch.setitem(routes_module.DRIVERS, 'ikuai', Driver)

    def agent(name='gateway', profile='home', host='192.0.2.4'):
        return manager.register_agent({'name': name, 'host': host, 'port': 7890, 'service_type': 'mihomo',
                                       'deployment_method': 'shell', 'version': '1.8.50-go', 'profile_id': profile})['id']

    def routers(profile, *items):
        repository.save_profile(profile, {'routers': {'items': [
            {'id': f'{profile}-{index}', 'name': name, 'type': 'ikuai', 'endpoint': endpoint, 'username': 'admin',
             'password': 'secret', 'proxy_gateway_agent_id': link}
            for index, (name, endpoint, link) in enumerate(items, 1)]}})

    def get(agent_id, **query):
        response = client.get(PATH.format(agent_id), headers=admin, query_string=query)
        assert response.headers['Cache-Control'] == 'no-store'
        assert response.status_code == 200, response.get_json()
        return response.get_json()

    return client, admin, agent, routers, states, reads, get


def synced_state(gateway='192.0.2.4'):
    return RouterState(routes=[RouteEntry('1', '198.18.0.0/16', gateway, True, 'cf:fake-ip')],
                       nat_bypass=[NatBypassEntry('n1', ['198.18.0.0/16'], [], True, 'cf:mihomo')])


def test_lists_linked_routers_in_every_profile_with_their_live_state(lab):
    client, admin, agent, routers, states, reads, get = lab
    gateway, unrelated = agent(), agent('other', host='192.0.2.5')
    routers('home', ('主路由', 'http://synced', gateway), ('无关', 'http://unrelated', unrelated))
    routers('other', ('分店', 'http://pending', gateway))
    states.update({'http://synced': synced_state(), 'http://pending': RouterState()})
    data = get(gateway)
    assert data['success'] is True and data['agent_id'] == gateway and data['agent_profile_id'] == 'home'
    assert data['cached'] is False and data['checked_at']
    by_id = {item['id']: item for item in data['routers']}
    assert [item['id'] for item in data['routers']] == ['home-1', 'other-1']
    assert by_id['home-1']['state'] == 'synced' and by_id['home-1']['pending'] == 0
    assert {key: by_id['home-1'][key] for key in ('name', 'type', 'profile_id', 'profile_name')} == {
        'name': '主路由', 'type': 'ikuai', 'profile_id': 'home', 'profile_name': 'Home'}
    other = by_id['other-1']
    assert other['state'] == 'pending' and other['pending'] >= 1 and other['profile_name'] == 'Other'
    assert {'key': 'routes', 'label': '主路由代理路由', 'status': '待添加', 'count': 1} in other['items']
    assert sorted(reads) == ['http://pending', 'http://synced']
    assert 'secret' not in client.get(PATH.format(gateway), headers=admin).get_data(as_text=True)


def test_explicit_profile_reads_only_its_router_even_with_a_cloned_id(lab):
    from backend.common.config import get_repository
    client, admin, agent, routers, states, reads, get = lab
    gateway = agent()
    routers('home', ('ROS', 'http://home', gateway))
    routers('other', ('ROS', 'http://other', gateway))
    get_repository().update_profile_transaction('other', lambda profile: profile['routers']['items'][0].update(id='home-1'))
    states.update({'http://home': synced_state(), 'http://other': RouterState()})

    home = get(gateway, profile='home')
    assert [(item['id'], item['profile_id'], item['state']) for item in home['routers']] == [('home-1', 'home', 'synced')]
    assert reads == ['http://home']  # Exclude foreign routers before contacting devices.
    assert get(gateway, profile='home')['cached'] is True
    other = get(gateway, profile='other')
    assert [(item['id'], item['profile_id'], item['state']) for item in other['routers']] == [('home-1', 'other', 'pending')]
    assert reads == ['http://home', 'http://other']
    assert get(gateway, profile='home')['routers'] == home['routers']


def test_scoped_request_does_not_reuse_an_unscoped_result(lab):
    client, admin, agent, routers, states, reads, get = lab
    gateway = agent()
    routers('home', ('主路由', 'http://home', gateway))
    routers('other', ('测试路由', 'http://other', gateway))
    states.update({'http://home': synced_state(), 'http://other': RouterState()})
    assert len(get(gateway)['routers']) == 2
    reads.clear()
    scoped = get(gateway, profile='home')
    assert scoped['cached'] is False
    assert [item['profile_id'] for item in scoped['routers']] == ['home']
    assert reads == ['http://home']
    assert len(get(gateway)['routers']) == 2


def test_empty_or_missing_profile_never_falls_back_to_another_profile(lab):
    client, admin, agent, routers, states, reads, get = lab
    gateway = agent()
    routers('other', ('测试路由', 'http://other', gateway))
    assert get(gateway, profile='home')['routers'] == []
    for profile, status in (('deleted', 404), ('', 400)):
        response = client.get(PATH.format(gateway), headers=admin, query_string={'profile': profile})
        assert response.status_code == status
        assert response.get_json()['success'] is False
    assert reads == []


def test_an_agent_without_linked_routers_reads_nothing(lab):
    client, admin, agent, routers, states, reads, get = lab
    gateway = agent()
    routers('home', ('主路由', 'http://synced', ''))
    assert get(gateway)['routers'] == [] and reads == []
    assert get('missing-agent')['routers'] == []


def test_read_failures_and_timeouts_are_unknown_not_synced(lab):
    client, admin, agent, routers, states, reads, get = lab
    gateway = agent()
    release = threading.Event()

    def slow():
        release.wait(5)
        return synced_state()
    routers('home', ('坏', 'http://broken', gateway), ('慢', 'http://slow', gateway), ('好', 'http://synced', gateway))
    states.update({'http://broken': RouterError('无法连接路由器：连接被拒绝'), 'http://slow': slow,
                   'http://synced': synced_state()})
    started = time.monotonic()
    data = get(gateway)
    release.set()
    assert time.monotonic() - started < 4.5
    states_by_name = {item['name']: (item['state'], item['message']) for item in data['routers']}
    assert states_by_name['坏'] == ('unknown', '无法连接路由器：连接被拒绝')
    assert states_by_name['慢'][0] == 'unknown' and '读取超时（2 秒）' in states_by_name['慢'][1]
    assert states_by_name['好'] == ('synced', '')


def test_an_unexpected_error_is_unknown_with_its_kind(lab):
    client, admin, agent, routers, states, reads, get = lab
    gateway = agent()
    routers('home', ('主路由', 'http://bug', gateway))
    states['http://bug'] = KeyError('dst')
    [entry] = get(gateway)['routers']
    assert entry['state'] == 'unknown' and entry['message'] == '读取失败：KeyError'


def test_results_are_reused_until_the_key_changes_and_any_relevant_change_reads_again(lab):
    client, admin, agent, routers, states, reads, get = lab
    gateway = agent()
    routers('home', ('主路由', 'http://pending', gateway))
    states['http://pending'] = RouterState()
    assert get(gateway)['routers'][0]['state'] == 'pending'
    states['http://pending'] = synced_state()
    again = get(gateway)
    assert again['cached'] is True and again['routers'][0]['state'] == 'pending' and len(reads) == 1
    # No fixed expiry: the same key keeps answering from the last read, with its time.
    assert not hasattr(pending, 'CACHE_TTL')
    later = get(gateway)
    assert later['cached'] is True and later['checked_at'] == again['checked_at'] and len(reads) == 1
    # The reminder refreshes after a save or a check.
    fresh = get(gateway, refresh=1)
    assert fresh['cached'] is False and fresh['routers'][0]['state'] == 'synced' and len(reads) == 2
    # A saved draft changes the profile revision: never answered from the older read.
    states['http://pending'] = RouterState()
    client.post('/api/custom-config/mihomo', headers={**admin, 'X-ConfigFlow-Profile': 'home'},
                json={'config': 'dns:\n  enhanced-mode: fake-ip\n', 'provider_update_interval': 3600})
    changed = get(gateway)
    assert changed['cached'] is False and changed['routers'][0]['state'] == 'pending' and len(reads) == 3


def test_simultaneous_requests_share_one_read():
    """A reader arriving during a read, even with refresh, joins it instead of reading the router again."""
    cache = pending.PendingCache()
    calls, results = [], []
    entered, gate = threading.Event(), threading.Event()

    def load():
        calls.append(1)
        entered.set()
        gate.wait(5)
        return [{'id': 'r', 'state': 'synced'}]
    first = threading.Thread(target=lambda: results.append(cache.read('a', 'sig', load)))
    first.start()
    assert entered.wait(5)
    second = threading.Thread(target=lambda: results.append(cache.read('a', 'sig', load, refresh=True)))
    second.start()
    time.sleep(0.05)
    gate.set()
    first.join(5)
    second.join(5)
    assert calls == [1]
    assert [routers for routers, _, _ in results] == [[{'id': 'r', 'state': 'synced'}]] * 2
    # While the signature holds the result is served from memory; another signature reads again.
    assert cache.read('a', 'sig', load)[2] is True and calls == [1]
    assert cache.read('a', 'changed', load)[2] is False and calls == [1, 1]


def test_a_read_with_an_unreadable_router_is_tried_again():
    """unknown is not a result to keep: the next request reads again, a known result is then kept."""
    cache = pending.PendingCache()
    answers = [[{'id': 'r', 'state': 'unknown'}, {'id': 's', 'state': 'synced'}], [{'id': 'r', 'state': 'pending'}]]
    load = lambda: answers.pop(0)
    assert cache.read('a', 'sig', load)[0][0]['state'] == 'unknown'
    routers, _, cached = cache.read('a', 'sig', load)
    assert routers[0]['state'] == 'pending' and cached is False and not answers
    assert cache.read('a', 'sig', load)[2] is True


def test_requires_authentication(lab):
    client, admin, agent, routers, states, reads, get = lab
    assert client.get(PATH.format(agent())).status_code == 401
