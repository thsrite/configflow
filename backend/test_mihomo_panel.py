"""The Mihomo panel only forwards whitelisted controller calls through the Agent."""
import copy
import json
from urllib.parse import quote

import pytest
import requests
from werkzeug.datastructures import MultiDict

from backend.agents import mihomo_panel as panel
from backend.agents.oxidns_monitor import MonitorError
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401  (fixture)

AGENT = {'id': 'mihomo', 'name': 'Gateway', 'host': '127.0.0.1', 'port': 8080,
         'service_type': 'mihomo', 'version': '1.8.15-go', 'status': 'online', 'token': 'agent-private-token'}


class Manager:
    def __init__(self, agent=None):
        self.agent = copy.deepcopy(AGENT if agent is None else agent)

    def get_agent_by_id(self, aid, history=True):
        return self.agent if aid == 'mihomo' else None


class Response:
    def __init__(self, data=None, status=200, raw=None):
        self.status_code = status
        self.body = raw if raw is not None else json.dumps(data).encode()

    def __enter__(self): return self
    def __exit__(self, *args): pass

    def iter_content(self, size):
        for start in range(0, len(self.body), size):
            yield self.body[start:start + size]


class Calls(list):
    replies: list


@pytest.fixture
def calls(monkeypatch):
    seen = Calls()
    replies = []

    def request(self, method, url, **kwargs):
        seen.append({'method': method, 'url': url, **kwargs})
        return replies.pop(0) if replies else Response({'ok': True})

    monkeypatch.setattr(requests.Session, 'request', request)
    seen.replies = replies
    return seen


def test_forwards_named_resources_as_single_encoded_segment(calls):
    result = panel.call(Manager(), 'mihomo', 'proxy', 'PUT', MultiDict({'name': '节点/香港 01'}), {'name': 'HK'})
    assert result == {'success': True, 'data': {'ok': True}}
    call = calls[0]
    assert call['method'] == 'PUT'
    assert call['url'] == 'http://127.0.0.1:8080/api/mihomo/proxies/%E8%8A%82%E7%82%B9%2F%E9%A6%99%E6%B8%AF%2001'
    assert json.loads(call['data']) == {'name': 'HK'}
    assert call['headers']['Authorization'] == 'Bearer agent-private-token'
    assert call['allow_redirects'] is False


def test_delay_query_is_validated_and_defaulted(calls):
    panel.call(Manager(), 'mihomo', 'group/delay', 'GET', MultiDict({'name': 'Auto', 'extra': 'x'}))
    assert calls[0]['params'] == {'url': panel.DEFAULT_TEST_URL, 'timeout': '5000'}
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager(), 'mihomo', 'group/delay', 'GET', MultiDict({'name': 'Auto', 'url': 'file:///etc/passwd'}))
    assert exc.value.status == 400


@pytest.mark.parametrize('resource,method,body', [
    ('configs', 'PUT', {'mode': 'rule'}),
    ('restart', 'POST', None),
    ('configs', 'PATCH', {'mode': 'rule', 'tun': {'enable': False}}),
    ('configs', 'PATCH', {'external-controller': '0.0.0.0:9090'}),
    ('proxy', 'PUT', {'name': ''}),
])
def test_rejects_operations_outside_the_panel(calls, resource, method, body):
    with pytest.raises(MonitorError):
        panel.call(Manager(), 'mihomo', resource, method, MultiDict({'name': 'G'}), body)
    assert calls == []


def test_mode_switch_only_sends_mode(calls):
    panel.call(Manager(), 'mihomo', 'configs', 'PATCH', MultiDict(), {'mode': 'global'})
    assert json.loads(calls[0]['data']) == {'mode': 'global'}


def test_requires_new_mihomo_agent(calls):
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'version': '1.8.14-go'}), 'mihomo', 'version', 'GET', MultiDict())
    assert exc.value.code == 'UPGRADE_REQUIRED'
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'service_type': 'oxidns'}), 'mihomo', 'version', 'GET', MultiDict())
    assert exc.value.code == 'WRONG_SERVICE'
    assert calls == []


def test_empty_reply_and_core_errors(calls):
    calls.replies.extend([Response(raw=b'', status=204), Response({'message': 'Timeout'}, status=504),
                          Response({'success': False, 'code': 'CONTROLLER_UNAVAILABLE'}, status=409)])
    assert panel.call(Manager(), 'mihomo', 'connection', 'DELETE', MultiDict({'name': 'abc'})) == {'success': True, 'data': None}
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager(), 'mihomo', 'proxy/delay', 'GET', MultiDict({'name': 'HK'}))
    assert (exc.value.status, exc.value.message) == (504, 'Timeout')
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager(), 'mihomo', 'version', 'GET', MultiDict())
    assert exc.value.code == 'CONTROLLER_UNAVAILABLE'


def test_route_requires_admin_auth_and_forwards_name(authenticated_app, calls):
    client, manager, _, admin, _ = authenticated_app
    registered = manager.register_agent({k: v for k, v in AGENT.items() if k not in ('id', 'token')})
    aid = registered['id']
    url = f'/api/agents/{aid}/mihomo/proxy?name=%E8%8A%82%E7%82%B9%2FA'
    assert client.put(url, json={'name': 'HK'}).status_code == 401
    assert client.put(url, json={'name': 'HK'}, headers={'Authorization': 'Bearer ' + registered['token']}).status_code == 401
    assert calls == []
    response = client.put(url, json={'name': 'HK'}, headers=admin)
    assert response.status_code == 200 and response.headers['Cache-Control'] == 'no-store'
    assert calls[0]['url'].endswith('/api/mihomo/proxies/%E8%8A%82%E7%82%B9%2FA')
    assert client.get(f'/api/agents/{aid}/mihomo/restart', headers=admin).status_code == 404


def test_reload_and_upgrade_need_newer_agent_and_send_no_body(calls):
    old = Manager()  # 1.8.15-go
    for resource, method in (('configs/reload', 'PUT'), ('upgrade', 'POST')):
        with pytest.raises(MonitorError) as exc:
            panel.call(old, 'mihomo', resource, method, MultiDict(), {'path': '/tmp/evil.yaml'})
        assert exc.value.code == 'UPGRADE_REQUIRED' and '1.8.16-go' in exc.value.message
    assert calls == []
    new = Manager({**AGENT, 'version': '1.8.16-go'})
    panel.call(new, 'mihomo', 'configs/reload', 'PUT', MultiDict(), {'path': '/tmp/evil.yaml', 'payload': 'x'})
    panel.call(new, 'mihomo', 'upgrade', 'POST', MultiDict({'channel': 'alpha'}))
    assert [(c['method'], c['url'].rsplit('/api/mihomo/', 1)[1], c['data'], c['params']) for c in calls] == [
        ('PUT', 'configs', None, {}), ('POST', 'upgrade', None, {})]
    assert calls[1]['timeout'] == (3, 200)


def test_provider_node_delay_encodes_both_names_and_keeps_test_options(calls):
    provider = '订阅 / A%?#'
    name = '🇯🇵 JP-05 · AWS2 / 50%?#'
    url = 'http://127.0.0.1:18080/generate_204?value=a&other=b'
    calls.replies.append(Response({'delay': 123}))
    result = panel.call(Manager({**AGENT, 'version': '1.8.18-go'}), 'mihomo',
                        'providers/proxy/delay', 'GET', MultiDict({
                            'provider': provider, 'name': name, 'url': url,
                            'timeout': '7300', 'unrelated': 'discard'}))
    assert result == {'success': True, 'data': {'delay': 123}}
    forwarded = calls[0]
    assert forwarded['url'].endswith(
        '/api/mihomo/providers/proxies/' + quote(provider, safe='') + '/' + quote(name, safe='') + '/healthcheck')
    assert forwarded['params'] == {'url': url, 'timeout': '7300'}
    assert forwarded['method'] == 'GET' and forwarded['data'] is None
    assert forwarded['timeout'] == (3, 40)


def test_ordinary_node_still_works_on_older_agent_and_provider_requires_update(calls):
    calls.replies.append(Response({'delay': 81}))
    result = panel.call(Manager(), 'mihomo', 'proxy/delay', 'GET', MultiDict({'name': '普通节点'}))
    assert result['data']['delay'] == 81
    assert '/proxies/' in calls[0]['url'] and '/providers/' not in calls[0]['url']
    assert calls[0]['params'] == {'url': panel.DEFAULT_TEST_URL, 'timeout': '5000'}
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'version': '1.8.17-go'}), 'mihomo',
                   'providers/proxy/delay', 'GET', MultiDict({'provider': 'Airport', 'name': 'HK'}))
    assert exc.value.code == 'UPGRADE_REQUIRED'
    assert '1.8.18-go' in exc.value.message and '订阅节点测速' in exc.value.message
    assert len(calls) == 1


@pytest.mark.parametrize('params', [
    {'name': 'HK'}, {'provider': 'Airport'}, {'provider': '', 'name': 'HK'},
    {'provider': 'bad\nname', 'name': 'HK'}, {'provider': 'Airport', 'name': 'bad\0name'},
    {'provider': 'x' * 513, 'name': 'HK'},
    {'provider': 'Airport', 'name': 'HK', 'url': 'file:///tmp/probe'},
    {'provider': 'Airport', 'name': 'HK', 'timeout': 'invalid'},
    {'provider': 'Airport', 'name': 'HK', 'timeout': '30001'},
    {'provider': 'Airport', 'name': 'HK', 'timeout': '499'},
])
def test_provider_node_uses_existing_name_and_delay_validation(calls, params):
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'version': '1.8.18-go'}), 'mihomo',
                   'providers/proxy/delay', 'GET', MultiDict(params))
    assert exc.value.code == 'INVALID_REQUEST' and exc.value.status == 400
    assert calls == []


@pytest.mark.parametrize('status,message,expected_status', [
    (404, 'Resource not found', 400), (504, 'Timeout', 504),
    (503, 'An error occurred in the delay test', 502),
])
def test_provider_node_core_failure_is_reported_without_testing_another_node(calls, status, message, expected_status):
    calls.replies.append(Response({'message': message}, status=status))
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'version': '1.8.18-go'}), 'mihomo',
                   'providers/proxy/delay', 'GET', MultiDict({'provider': 'Airport', 'name': 'HK'}))
    assert (exc.value.code, exc.value.status, exc.value.message) == ('CORE_ERROR', expected_status, message)
    assert len(calls) == 1


def test_provider_node_route_requires_admin_and_keeps_provider_healthcheck(authenticated_app, calls):
    client, manager, _, admin, _ = authenticated_app
    registered = manager.register_agent({**{k: v for k, v in AGENT.items() if k not in ('id', 'token')}, 'version': '1.8.18-go'})
    url = f'/api/agents/{registered["id"]}/mihomo/providers/proxy/delay'
    params = {'provider': '订阅/A', 'name': '节点/B'}
    assert client.get(url, query_string=params).status_code == 401
    assert calls == []
    calls.replies.append(Response({'delay': 19}))
    result = client.get(url, query_string=params, headers=admin)
    assert result.status_code == 200 and result.json['data'] == {'delay': 19}
    assert result.headers['Cache-Control'] == 'no-store'
    assert client.post(url, query_string=params, headers=admin).status_code == 404
    assert len(calls) == 1
    panel.call(Manager(), 'mihomo', 'providers/proxy/healthcheck', 'GET', MultiDict({'name': 'Airport'}))
    assert calls[-1]['url'].endswith('/providers/proxies/Airport/healthcheck')
    assert calls[-1]['params'] == {}


@pytest.mark.parametrize('message', [
    'download failed: dial tcp: i/o timeout',
    'Agent download connection failed',
])
def test_core_upgrade_errors_keep_original_text(calls, message):
    calls.replies.append(Response({'message': message}, status=500))
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'version': '1.8.16-go'}), 'mihomo', 'upgrade', 'POST', MultiDict())
    assert exc.value.code == 'CORE_ERROR'
    assert exc.value.message == message


def test_live_flow_needs_newer_agent_and_forwards_without_params(calls):
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'version': '1.8.25-go'}), 'mihomo', 'flow', 'GET', MultiDict())
    assert exc.value.code == 'UPGRADE_REQUIRED'
    assert '1.8.26-go' in exc.value.message and '实时流量' in exc.value.message
    assert calls == []

    calls.replies.append(Response({'interval': 2.0, 'paths': []}))
    result = panel.call(Manager({**AGENT, 'version': '1.8.26-go'}), 'mihomo', 'flow', 'GET',
                        MultiDict({'url': 'http://ignored'}))
    assert result['data'] == {'interval': 2.0, 'paths': []}
    assert calls[0]['method'] == 'GET'
    assert calls[0]['url'].endswith('/api/mihomo/flow')
    assert calls[0]['params'] == {} and calls[0]['data'] is None


def test_live_logs_need_newer_agent_and_forward_only_level_and_cursor(calls):
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'version': '1.8.55-go'}), 'mihomo', 'logs', 'GET', MultiDict())
    assert exc.value.code == 'UPGRADE_REQUIRED'
    assert '1.8.56-go' in exc.value.message and '实时日志' in exc.value.message
    assert calls == []

    agent = Manager({**AGENT, 'version': '1.8.56-go'})
    calls.replies.append(Response({'stream': 'debug', 'entries': [], 'next': 7}))
    result = panel.call(agent, 'mihomo', 'logs', 'GET', MultiDict({'level': 'debug', 'after': '7', 'path': '/etc'}))
    assert result['data'] == {'stream': 'debug', 'entries': [], 'next': 7}
    assert calls[0]['url'].endswith('/api/mihomo/logs')
    assert calls[0]['params'] == {'level': 'debug', 'after': '7'}

    panel.call(agent, 'mihomo', 'logs', 'GET', MultiDict())
    assert calls[1]['params'] == {'level': 'info', 'after': '0'}


@pytest.mark.parametrize('args', [{'level': 'trace'}, {'level': 'silent'}, {'after': '-1'}, {'after': '1e3'},
                                  {'after': '9' * 19}])
def test_live_logs_reject_invalid_level_or_cursor(calls, args):
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'version': '1.8.56-go'}), 'mihomo', 'logs', 'GET', MultiDict(args))
    assert exc.value.code == 'INVALID_REQUEST'
    assert calls == []


def _node(name, **extra):
    return {'name': name, 'type': 'Trojan', 'udp': True, 'xudp': True, 'tfo': False, 'mptcp': False, 'smux': False,
            'interface': '', 'routing-mark': 0, 'dialer-proxy': '', 'provider-name': 'sub', 'id': 'x' * 36, 'alive': True,
            'history': [{'time': 't1', 'delay': 300}, {'time': 't2', 'delay': 120}],
            'extra': {'https://cp.cloudflare.com/generate_204': {'alive': False, 'history': [{'time': 't1', 'delay': 90}, {'time': 't2', 'delay': 0}]}},
            **extra}


def test_node_listings_keep_only_panel_fields_and_the_newest_delay(calls):
    group = _node('Proxy', type='Selector', now='HK', all=['HK', 'JP'], testUrl='https://cp.cloudflare.com/generate_204', hidden=False, icon='i', fixed='')
    calls.replies.append(Response({'proxies': {'Proxy': group, 'HK': _node('HK'), 'odd': 'not a dict'}}))
    proxies = panel.call(Manager(), 'mihomo', 'proxies', 'GET', MultiDict())['data']['proxies']
    assert proxies['HK'] == {'name': 'HK', 'type': 'Trojan', 'udp': True, 'alive': True,
                             'history': [{'time': 't2', 'delay': 120}],
                             'extra': {'https://cp.cloudflare.com/generate_204': {'alive': False, 'history': [{'time': 't2', 'delay': 0}]}}}
    assert {key: proxies['Proxy'][key] for key in ('now', 'all', 'testUrl', 'hidden', 'icon', 'fixed')} == {
        'now': 'HK', 'all': ['HK', 'JP'], 'testUrl': 'https://cp.cloudflare.com/generate_204', 'hidden': False, 'icon': 'i', 'fixed': ''}
    assert proxies['odd'] == 'not a dict'


def test_provider_listing_slims_nodes_and_keeps_only_names_of_compatible_members(calls):
    subscription = {'name': 'sub', 'type': 'Proxy', 'vehicleType': 'HTTP', 'updatedAt': 'now', 'testUrl': 'u',
                    'subscriptionInfo': {'Total': 1}, 'proxies': [_node('HK'), _node('JP', history=[])]}
    compatible = {'name': 'Proxy', 'type': 'Proxy', 'vehicleType': 'Compatible',
                  'proxies': [_node('DIRECT', type='Direct'), _node('Auto', type='URLTest', all=['HK'] * 500, now='HK')]}
    calls.replies.append(Response({'providers': {'sub': subscription, 'Proxy': compatible, 'empty': {'name': 'empty', 'vehicleType': 'File'}}}))
    providers = panel.call(Manager(), 'mihomo', 'providers/proxies', 'GET', MultiDict())['data']['providers']
    assert {key: value for key, value in providers['sub'].items() if key != 'proxies'} == {
        key: value for key, value in subscription.items() if key != 'proxies'}
    assert providers['sub']['proxies'][0]['history'] == [{'time': 't2', 'delay': 120}]
    assert 'xudp' not in providers['sub']['proxies'][0] and providers['sub']['proxies'][1]['history'] == []
    assert providers['Proxy']['proxies'] == [{'name': 'DIRECT'}, {'name': 'Auto'}]
    assert providers['empty'] == {'name': 'empty', 'vehicleType': 'File'}


def test_other_replies_pass_through_unchanged(calls):
    reply = {'proxies': {'HK': _node('HK')}}
    calls.replies.append(Response(reply))
    assert panel.call(Manager(), 'mihomo', 'proxy', 'PUT', MultiDict({'name': 'Proxy'}), {'name': 'HK'})['data'] == reply
    calls.replies.append(Response(['unexpected']))
    assert panel.call(Manager(), 'mihomo', 'proxies', 'GET', MultiDict())['data'] == ['unexpected']


class VersionText:
    def __init__(self, text, status=200):
        self.content = text.encode()
        self.status_code = status

    def __enter__(self): return self
    def __exit__(self, *args): pass

    def raise_for_status(self):
        if self.status_code >= 400:
            raise requests.HTTPError(response=self)


@pytest.fixture
def latest(monkeypatch):
    seen = Calls()
    replies = []

    def request_rule(url, **kwargs):
        seen.append((url, kwargs))
        reply = replies.pop(0)
        if isinstance(reply, Exception):
            raise reply
        return reply

    class Repository:
        def get_system_settings(self):
            return dict(settings)

    settings = {'rule_fetch_proxy': '', 'github_proxy_domain': '', 'backup': {'large': 'not needed'}}
    seen.settings = settings

    monkeypatch.setattr('backend.utils.rule_fetch.request_rule', request_rule)
    monkeypatch.setattr('backend.common.config.get_repository', Repository)
    monkeypatch.setattr(panel, '_core_latest', {})
    seen.replies = replies
    return seen


@pytest.mark.parametrize('current, upstream, channel, has_update', [
    ('v1.19.32', 'v1.19.33\n', 'release', True),
    ('v1.19.32', 'v1.19.32\n', 'release', False),
    ('alpha-0a1b2c3', 'alpha-4d5e6f7', 'alpha', True),
    ('alpha-0a1b2c3', 'alpha-0a1b2c3', 'alpha', False),
])
def test_core_update_check_follows_the_cores_own_channel_and_comparison(calls, latest, current, upstream, channel,
                                                                          has_update):
    calls.replies.append(Response({'meta': True, 'version': current}))
    latest.replies.append(VersionText(upstream))
    result = panel.call(Manager({**AGENT, 'version': '1.8.16-go'}), 'mihomo', 'core/update', 'GET', MultiDict())
    data = result['data']
    assert (data['current'], data['latest'], data['channel'], data['has_update'], data['error']) == (
        current, upstream.strip(), channel, has_update, '')
    assert latest[0][0] == panel.CORE_VERSION_URLS[channel]
    # Bounded streaming read, with only the download settings rather than the whole system section.
    assert latest[0][1]['stream'] is True and latest[0][1]['max_bytes'] == 256
    assert set(latest[0][1]['config_data']) == {'system_config'}
    assert 'backup' not in latest[0][1]['config_data']['system_config']
    assert [(c['method'], c['url'].rsplit('/api/mihomo/', 1)[1]) for c in calls] == [('GET', 'version')]


def test_core_update_check_needs_the_upgrade_agent_version(calls, latest):
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager(), 'mihomo', 'core/update', 'GET', MultiDict())
    assert exc.value.code == 'UPGRADE_REQUIRED' and '1.8.16-go' in exc.value.message
    assert calls == [] and latest == []


@pytest.mark.parametrize('failure, reason', [
    (requests.ConnectTimeout('https://user:secret@proxy.invalid'), '连接 GitHub 超时'),
    (requests.HTTPError(response=VersionText('', 404)), 'GitHub 返回 HTTP 404'),
    (requests.ConnectionError('https://user:secret@proxy.invalid'), '无法连接 GitHub'),
    (VersionText('<html>blocked</html>'), 'version.txt 内容不是版本号'),
])
def test_core_update_check_failure_still_reports_the_running_version(calls, latest, failure, reason):
    calls.replies.append(Response({'meta': True, 'version': 'v1.19.32'}))
    latest.replies.append(failure)
    data = panel.call(Manager({**AGENT, 'version': '1.8.16-go'}), 'mihomo', 'core/update', 'GET', MultiDict())['data']
    assert data['current'] == 'v1.19.32' and data['latest'] == '' and data['has_update'] is False
    assert reason in data['error'] and 'secret' not in data['error']


def test_core_update_check_reports_an_unreachable_core(calls, latest):
    calls.replies.append(Response({'success': False, 'code': 'CORE_UNREACHABLE'}, status=502))
    with pytest.raises(MonitorError) as exc:
        panel.call(Manager({**AGENT, 'version': '1.8.16-go'}), 'mihomo', 'core/update', 'GET', MultiDict())
    assert exc.value.code == 'CORE_UNREACHABLE'
    assert latest == []


def test_core_update_check_caches_github_and_a_manual_check_refreshes(calls, latest, monkeypatch):
    clock = [1000.0]
    monkeypatch.setattr(panel.time, 'monotonic', lambda: clock[0])
    manager = Manager({**AGENT, 'version': '1.8.16-go'})

    def check(force=False):
        calls.replies.append(Response({'meta': True, 'version': 'v1.19.32'}))
        return panel.call(manager, 'mihomo', 'core/update', 'GET', MultiDict({'force': '1'} if force else {}))['data']

    latest.replies.append(requests.ConnectTimeout())
    assert check()['error']
    clock[0] += panel.CORE_CHECK_FAILURE_TTL - 1
    assert check()['error'] and len(latest) == 1  # a failure is not retried on every open
    clock[0] += 2
    latest.replies.append(VersionText('v1.19.32'))
    assert check()['has_update'] is False and len(latest) == 2
    clock[0] += panel.CORE_CHECK_TTL - 1
    assert check()['latest'] == 'v1.19.32' and len(latest) == 2
    latest.replies.append(VersionText('v1.19.33'))
    data = check(force=True)
    assert data['has_update'] is True and data['latest'] == 'v1.19.33' and len(latest) == 3
    assert data['release_url'].endswith('/releases/tag/v1.19.33')


def test_a_core_newer_than_the_release_is_not_offered_a_downgrade(calls, latest, monkeypatch):
    clock = [1000.0]
    monkeypatch.setattr(panel.time, 'monotonic', lambda: clock[0])
    manager = Manager({**AGENT, 'version': '1.8.16-go'})

    def check(current):
        calls.replies.append(Response({'meta': True, 'version': current}))
        return panel.call(manager, 'mihomo', 'core/update', 'GET', MultiDict())['data']

    latest.replies.append(VersionText('v1.19.33'))
    assert check('v1.19.32')['has_update'] is True
    # Upgraded elsewhere (or a dev build) past the cached release: no "update" to an older version,
    # and the cache is looked at again only after the short TTL, not on every open.
    data = check('v1.19.34')
    assert data['has_update'] is False and data['latest'] == 'v1.19.33' and len(latest) == 1
    clock[0] += panel.CORE_CHECK_FAILURE_TTL - 1
    assert check('v1.19.34')['has_update'] is False and len(latest) == 1
    clock[0] += 2
    latest.replies.append(VersionText('v1.19.34'))
    data = check('v1.19.34')
    assert data['has_update'] is False and data['latest'] == 'v1.19.34' and len(latest) == 2
    # Equal versions use the long TTL again.
    clock[0] += panel.CORE_CHECK_FAILURE_TTL + 1
    assert check('v1.19.34')['latest'] == 'v1.19.34' and len(latest) == 2


def test_core_update_check_fetches_github_once_for_concurrent_requests(latest, monkeypatch):
    import threading
    release, fetches = threading.Event(), []

    def slow(channel, settings):
        fetches.append(channel)
        release.wait(5)
        return 'v1.19.33'

    monkeypatch.setattr(panel, '_latest_core_version', slow)
    results = []
    threads = [threading.Thread(target=lambda: results.append(panel._cached_latest('release', 'v1.19.32', False)))
               for _ in range(6)]
    for thread in threads:
        thread.start()
    while not fetches:
        threading.Event().wait(0.01)
    release.set()
    for thread in threads:
        thread.join(5)
    assert fetches == ['release']
    assert results == [('v1.19.33', '')] * 6
    # A manual check started after that fetch finished asks GitHub again.
    assert panel._cached_latest('release', 'v1.19.32', True) == ('v1.19.33', '') and len(fetches) == 2


def test_an_expired_cache_answers_at_once_while_another_request_refreshes(latest, monkeypatch):
    monkeypatch.setattr(panel, '_core_latest', {'release': (0.0, 'v1.19.33', '', panel._download_settings())})
    monkeypatch.setattr(panel.time, 'monotonic', lambda: panel.CORE_CHECK_TTL + 10.0)
    monkeypatch.setattr(panel, '_latest_core_version', lambda channel, settings: pytest.fail('must not fetch'))
    lock = panel._core_locks['release']
    assert lock.acquire(blocking=False)
    try:
        assert panel._cached_latest('release', 'v1.19.32', False) == ('v1.19.33', '')
    finally:
        lock.release()


def test_failure_cache_counts_from_when_the_fetch_ended(latest, monkeypatch):
    clock = [1000.0]
    monkeypatch.setattr(panel.time, 'monotonic', lambda: clock[0])

    def slow_failure(channel, settings):
        clock[0] += 30
        raise requests.ConnectTimeout()

    monkeypatch.setattr(panel, '_latest_core_version', slow_failure)
    assert panel._cached_latest('release', 'v1.19.32', False)[1]
    assert panel._core_latest['release'][0] == 1030.0


def test_changing_the_download_proxy_retries_a_cached_failure_at_once(calls, latest):
    manager = Manager({**AGENT, 'version': '1.8.16-go'})

    def check():
        calls.replies.append(Response({'meta': True, 'version': 'v1.19.32'}))
        return panel.call(manager, 'mihomo', 'core/update', 'GET', MultiDict())['data']

    latest.replies.append(requests.ConnectionError())
    assert check()['error'] and check()['error'] and len(latest) == 1
    latest.settings['rule_fetch_proxy'] = 'http://127.0.0.1:7890'
    latest.replies.append(VersionText('v1.19.33'))
    data = check()
    assert data['error'] == '' and data['has_update'] is True and len(latest) == 2
    assert latest[1][1]['config_data']['system_config']['rule_fetch_proxy'] == 'http://127.0.0.1:7890'
