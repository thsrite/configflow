"""Monitoring authenticates both hops and never exposes core configuration."""
import copy
import json

import pytest
import requests
from werkzeug.datastructures import MultiDict

from backend.agents import oxidns_monitor as monitor
from backend.test_agent_upgrade_auth import authenticated_app

AGENT = {'id': 'oxi', 'name': 'DNS', 'host': '127.0.0.1', 'port': 8082,
         'profile_id': 'default', 'service_type': 'oxidns', 'version': '1.7.0-go',
         'status': 'online', 'token': 'agent-private-token'}
ROW = {'id': 42, 'created_at_ms': 1792100000123, 'elapsed_ms': 0,
       'client_ip': '192.0.2.1', 'questions_json': [{'name': 'example.test.', 'qtype': 'A', 'qclass': 'IN'}],
       'error': None, 'has_response': True, 'rcode': 'NOERROR', 'answer_count': 1,
       'answers_json': [{'name': 'example.test.', 'rr_type': 'A', 'ttl': 60, 'payload_text': '192.0.2.2'}]}
OVERVIEW = {'success': True, 'health': {'version': '1.6.1', 'status': 'ok', 'uptime_ms': 1234,
            'instance_id': 'instance'}, 'capabilities': {'query_recorders': ['recorder'], 'caches': ['cache']},
            'metrics': 'server_request_total{plugin_tag="udp",protocol="udp"} 5\n',
            'config': 'private configuration must not be forwarded'}


class Manager:
    def __init__(self, agent=None):
        self.agent = copy.deepcopy(AGENT if agent is None else agent)
    def get_agent_by_id(self, aid, history=True):
        return self.agent if aid == 'oxi' else None


class Response:
    def __init__(self, data=None, status=200, raw=None):
        self.status_code = status
        self.body = raw if raw is not None else json.dumps(data).encode()
    def __enter__(self): return self
    def __exit__(self, *args): pass
    def iter_content(self, size):
        for start in range(0, len(self.body), size):
            yield self.body[start:start + size]


class Session:
    def __init__(self, response, calls):
        self.response, self.calls = response, calls
        self.trust_env = True
    def __enter__(self): return self
    def __exit__(self, *args): pass
    def get(self, url, **kwargs):
        self.calls.append((url, kwargs, self.trust_env))
        if isinstance(self.response, Exception): raise self.response
        return self.response


def install_transport(monkeypatch, response):
    calls = []
    monkeypatch.setattr(monitor.requests, 'Session', lambda: Session(response, calls))
    return calls


def test_overview_uses_registered_agent_and_excludes_credentials(monkeypatch):
    calls = install_transport(monkeypatch, Response(OVERVIEW))
    result = monitor.overview(Manager(), 'oxi')
    assert result['metrics']['requests_total'] == 5
    assert result['metrics']['avg_latency_ms'] is None
    assert result['recorders'] == ['recorder']
    assert result['core']['instance_id'] == 'instance'
    serialized = json.dumps(result)
    assert 'agent-private-token' not in serialized and 'private configuration' not in serialized
    url, kwargs, trust_env = calls[0]
    assert url == 'http://127.0.0.1:8082/api/oxidns/overview'
    assert kwargs['headers']['Authorization'] == 'Bearer agent-private-token'
    assert kwargs['allow_redirects'] is False and kwargs['stream'] is True and trust_env is False


@pytest.mark.parametrize('data,status,raw,code', [
    ({'message': 'core-password'}, 302, None, 'INVALID_RESPONSE'),
    ({'message': 'core-password'}, 401, None, 'AGENT_AUTH_FAILED'),
    ({'message': 'core-password'}, 403, None, 'AGENT_AUTH_FAILED'),
    (None, 404, b'404 page not found', 'UPGRADE_REQUIRED'),
    ({'success': False, 'code': 'CORE_AUTH_FAILED', 'message': 'core-password'}, 502, None, 'CORE_AUTH_FAILED'),
    ({'success': False, 'code': 'RECORD_NOT_FOUND', 'message': 'private trace'}, 404, None, 'RECORD_NOT_FOUND'),
    ({'success': False, 'code': 'TLS_UNSUPPORTED', 'message': 'private config'}, 409, None, 'TLS_UNSUPPORTED'),
    (None, 200, b'not json with password', 'INVALID_RESPONSE'),
    ([], 200, None, 'INVALID_RESPONSE'),
    (None, 200, b'x' * (monitor.MAX_RESPONSE_BYTES + 1), 'INVALID_RESPONSE'),
])
def test_upstream_errors_are_bounded_and_sanitized(monkeypatch, data, status, raw, code):
    install_transport(monkeypatch, Response(data, status, raw))
    with pytest.raises(monitor.MonitorError) as error:
        monitor.overview(Manager(), 'oxi')
    assert error.value.code == code
    assert all(secret not in str(error.value) for secret in ('core-password', 'private trace', 'private config'))


def test_transport_exception_does_not_leak_credentials(monkeypatch):
    install_transport(monkeypatch, requests.Timeout('http://user:secret@example.test'))
    with pytest.raises(monitor.MonitorError) as exc:
        monitor.overview(Manager(), 'oxi')
    assert exc.value.code == 'AGENT_UNREACHABLE' and 'secret' not in str(exc.value)


@pytest.mark.parametrize('change,code', [({'version': '1.6.1-go'}, 'UPGRADE_REQUIRED'),
    ({'service_type': 'mihomo'}, 'WRONG_SERVICE'), ({'host': 'localhost/private'}, 'AGENT_UNREACHABLE'),
    ({'host': 'user@localhost'}, 'AGENT_UNREACHABLE'), ({'port': 0}, 'AGENT_UNREACHABLE')])
def test_agent_guards_precede_network(monkeypatch, change, code):
    calls = install_transport(monkeypatch, Response(OVERVIEW))
    manager = Manager(); manager.agent.update(change)
    with pytest.raises(monitor.MonitorError) as exc: monitor.overview(manager, 'oxi')
    assert exc.value.code == code and not calls


@pytest.mark.parametrize('params', [
    [('url', 'http://elsewhere')], [('recorder', '../config')], [('limit', '0')], [('limit', '101')],
    [('qname', 'ok'), ('qname', 'different')], [('cursor', 'page2')], [('since_ms', '-1')],
    [('since_ms', '100'), ('until_ms', '99')], [('status', 'delete')], [('qtype', 'A&flush=true')],
    [('qname', 'a\nb')], [('qname', 'x' * 1025)], [('cursor', '9999999999999999:1')],
])
def test_query_validation_rejects_injection_before_agent_call(monkeypatch, params):
    calls = install_transport(monkeypatch, Response({}))
    with pytest.raises(monitor.MonitorError) as exc:
        monitor.queries(Manager(), 'oxi', MultiDict(params))
    assert exc.value.code == 'INVALID_QUERY' and not calls


def test_queries_preserve_cursor_filter_and_zero_latency(monkeypatch):
    calls = install_transport(monkeypatch, Response({'ok': True, 'records': [ROW], 'next_cursor': '1792100000123:42'}))
    result = monitor.queries(Manager(), 'oxi', MultiDict({'recorder': 'recorder', 'qname': 'example', 'qtype': 'aaaa', 'limit': '50'}))
    assert result['records'][0]['elapsed_ms'] == 0
    assert result['next_cursor'] == '1792100000123:42'
    assert 'total' not in result
    assert calls[0][1]['params']['qtype'] == 'AAAA'


def test_query_detail_is_flat_and_read_only(monkeypatch):
    row = {**ROW, 'steps': [{'event_index': 0, 'sequence_tag': 'main', 'node_index': 0,
                           'kind': 'executor', 'tag': 'cache', 'outcome': 'continued'}], 'private_field': 'omit'}
    calls = install_transport(monkeypatch, Response({'ok': True, 'record': row}))
    result = monitor.query(Manager(), 'oxi', '42', MultiDict({'recorder': 'recorder'}))
    assert result['record']['id'] == 42 and result['record']['steps'][0]['tag'] == 'cache'
    assert 'private_field' not in result['record']
    assert calls[0][0].endswith('/queries/42')


@pytest.mark.parametrize('source', ['local', 'remote', 'fallback', 'pinned', 'cache', 'hosts', 'blocked',
                                    'rejected', 'unknown', 'unrecorded', 'error', 'no_response', 'ipv4_preferred'])
def test_list_preserves_agent_source_summary_without_trace_requests(monkeypatch, source):
    calls = install_transport(monkeypatch, Response({'ok': True, 'records': [
        {**ROW, 'resolution_source': source, 'source_credentials': 'never forward'}], 'next_cursor': None}))
    result = monitor.queries(Manager(), 'oxi', MultiDict({'recorder': 'recorder'}))
    assert result['records'][0]['resolution_source'] == source
    assert 'source_credentials' not in result['records'][0]
    assert len(calls) == 1 and calls[0][0].endswith('/queries')
    assert calls[0][1]['headers']['X-ConfigFlow-Query-Sources'] == '1'



def test_pinned_resolution_source_accepted():
    # Agents name a rule's pinned upstream (cf_dns_up_*) as its own source.
    assert monitor._record({**ROW, 'resolution_source': 'pinned'})['resolution_source'] == 'pinned'

@pytest.mark.parametrize('source', ['private://credentials', 'constructor', {}, [], None])
def test_invalid_source_summary_is_ignored_for_legacy_detail_fallback(source):
    assert 'resolution_source' not in monitor._record({**ROW, 'resolution_source': source})


def test_routes_require_admin_auth_and_never_accept_agent_token(authenticated_app, monkeypatch):
    client, manager, _, admin, _ = authenticated_app
    registered = manager.register_agent({k: v for k, v in AGENT.items() if k not in ('id', 'token')})
    aid = registered['id']
    calls = install_transport(monkeypatch, Response(OVERVIEW))
    for suffix in ('overview', 'queries', 'queries/42'):
        url = f'/api/agents/{aid}/oxidns/{suffix}'
        assert client.get(url).status_code == 401
        assert client.get(url, headers={'Authorization': 'Bearer ' + registered['token']}).status_code == 401
    assert not calls
    response = client.get(f'/api/agents/{aid}/oxidns/overview', headers=admin)
    assert response.status_code == 200 and response.headers['Cache-Control'] == 'no-store'
    assert registered['token'] not in response.get_data(as_text=True)
    calls.clear()
    assert client.post(f'/api/agents/{aid}/oxidns/overview', headers=admin).status_code == 405
    assert not calls


@pytest.mark.parametrize('uptime', [10 ** 400, monitor.MAX_INTEGER + 1])
def test_overview_treats_unrepresentable_uptime_as_unknown(monkeypatch, uptime):
    data = copy.deepcopy(OVERVIEW)
    data['health']['uptime_ms'] = uptime
    install_transport(monkeypatch, Response(data))
    result = monitor.overview(Manager(), 'oxi')
    assert result['core']['uptime_ms'] is None


@pytest.mark.parametrize('field', ['created_at_ms', 'elapsed_ms'])
@pytest.mark.parametrize('value', [10 ** 400, monitor.MAX_INTEGER + 1])
def test_record_rejects_unrepresentable_times_without_overflow(monkeypatch, field, value):
    row = copy.deepcopy(ROW)
    row[field] = value
    install_transport(monkeypatch, Response({'ok': True, 'records': [row], 'next_cursor': None}))
    with pytest.raises(monitor.MonitorError) as error:
        monitor.queries(Manager(), 'oxi', MultiDict({'recorder': 'recorder'}))
    assert error.value.code == 'INVALID_RESPONSE' and error.value.status == 502


@pytest.mark.parametrize('rcode,expected', [
    ('NOERROR', 'No Error'), ('noerror', 'No Error'), ('FormErr', 'Form Error'),
    ('SERVFAIL', 'Server Failure'), ('NXDOMAIN', 'Non-Existent Domain'),
    ('NOTIMP', 'Not Implemented'), ('REFUSED', 'Query Refused'),
    ('YXDOMAIN', 'Name should not exist'), ('YXRRSET', 'RR Set should not exist'),
    ('NXRRSET', 'RR Set does not exist'), ('NOTAUTH', 'Not authorized'),
    ('NOTZONE', 'Name not in zone'), ('BADVERS', 'Bad OPT Version'),
    ('BADSIG', 'TSIG Signature Failure'), ('BADKEY', 'Key not recognized'),
    ('BADTIME', 'Signature out of time window'), ('BADMODE', 'Bad TKEY mode'),
    ('BADNAME', 'Duplicate key name'), ('BADALG', 'Algorithm not supported'),
    ('BADTRUNC', 'Bad truncation'), ('BADCOOKIE', 'Bad server cookie'),
    ('No Error', 'No Error'), ('  Non-Existent Domain  ', 'Non-Existent Domain'),
    ('server failure', 'server failure'), ('RCODE15', 'RCODE15'),
    ('Future Response', 'Future Response'),
])
def test_rcode_filter_maps_mnemonics_and_preserves_official_text(monkeypatch, rcode, expected):
    row = {**ROW, 'rcode': 'No Error'}
    calls = install_transport(monkeypatch, Response({'ok': True, 'records': [row], 'next_cursor': None}))
    result = monitor.queries(Manager(), 'oxi', MultiDict({'recorder': 'recorder', 'rcode': rcode}))
    assert calls[0][1]['params']['rcode'] == expected
    assert result['records'][0]['rcode'] == 'No Error'


@pytest.mark.parametrize('rcode', ['No Error&flush=true', '../config', 'No Error?',
                                    'No Error#', 'No Error\x7f', 'No Error\n',
                                    'No\tError', '拒绝', 'x' * 65])
def test_rcode_filter_rejects_invalid_characters_before_network(monkeypatch, rcode):
    calls = install_transport(monkeypatch, Response({}))
    with pytest.raises(monitor.MonitorError) as error:
        monitor.queries(Manager(), 'oxi', MultiDict({'rcode': rcode}))
    assert error.value.code == 'INVALID_QUERY' and error.value.status == 400
    assert not calls


def clients_manager():
    manager = Manager()
    manager.agent['version'] = '1.7.1-go'
    return manager


CLIENTS = {'ok': True, 'recorder': 'recorder', 'sample_size': 3,
           'rows': [{'key': '192.168.0.2', 'count': 2, 'share': 2 / 3},
                    {'key': '2001:db8::2', 'count': 1, 'share': 1 / 3}],
           'config': 'private configuration', 'token': 'core-private-token'}


def test_clients_use_independent_search_and_return_only_safe_dto(monkeypatch):
    calls = install_transport(monkeypatch, Response(CLIENTS))
    result = monitor.clients(clients_manager(), 'oxi', MultiDict({
        'recorder': 'recorder', 'since_ms': '100', 'until_ms': '200', 'search': '192.168.'}))
    assert result == {'success': True, 'recorder': 'recorder', 'sample_size': 3, 'sample_limited': False,
                      'clients': [{'ip': '192.168.0.2', 'count': 2}, {'ip': '2001:db8::2', 'count': 1}]}
    url, kwargs, trust_env = calls[0]
    assert url == 'http://127.0.0.1:8082/api/oxidns/clients'
    assert kwargs['params'] == {'recorder': 'recorder', 'since_ms': '100', 'until_ms': '200', 'search': '192.168.'}
    assert kwargs['headers']['Authorization'] == 'Bearer agent-private-token'
    assert kwargs['allow_redirects'] is False and trust_env is False
    assert not any(secret in json.dumps(result) for secret in ('agent-private-token', 'core-private-token', 'private configuration'))


@pytest.mark.parametrize('sample,rows,limited', [
    (0, [], False), (10000, [{'key': '192.0.2.1', 'count': 10000, 'share': 1}], True),
])
def test_clients_sample_boundary_and_resolved_recorder(monkeypatch, sample, rows, limited):
    install_transport(monkeypatch, Response({'ok': True, 'recorder': 'auto-records', 'sample_size': sample, 'rows': rows}))
    result = monitor.clients(clients_manager(), 'oxi', MultiDict())
    assert result['sample_limited'] is limited and result['sample_size'] == sample
    assert result['recorder'] == 'auto-records'


@pytest.mark.parametrize('params', [
    [('qname', 'example')], [('qtype', 'A')], [('client_ip', '192.0.2.1')], [('limit', '20')],
    [('cursor', '100:1')], [('status', 'all')], [('search', 'a'), ('search', 'b')],
    [('recorder', '../config')], [('recorder', 'a..b')], [('search', 'a\nb')],
    [('search', '\x7f')], [('search', '\x85')], [('search', 'x' * 65)], [('search', '测' * 22)],
    [('since_ms', '-1')], [('since_ms', '101'), ('until_ms', '100')], [('agent_id', 'another')],
])
def test_clients_reject_unrelated_filters_and_bad_search_before_network(monkeypatch, params):
    calls = install_transport(monkeypatch, Response(CLIENTS))
    with pytest.raises(monitor.MonitorError) as exc:
        monitor.clients(clients_manager(), 'oxi', MultiDict(params))
    assert exc.value.code == 'INVALID_QUERY' and exc.value.status == 400 and not calls


def test_clients_accept_bounded_utf8_search(monkeypatch):
    calls = install_transport(monkeypatch, Response(CLIENTS))
    monitor.clients(clients_manager(), 'oxi', MultiDict({'search': '测试'}))
    assert calls[0][1]['params'] == {'search': '测试'}


@pytest.mark.parametrize('change', [
    {'sample_size': True}, {'sample_size': -1}, {'sample_size': 10001}, {'sample_size': 10 ** 400},
    {'recorder': 'other'}, {'recorder': '../secret'}, {'rows': None},
    {'rows': [{'key': 'core-password', 'count': 3}]},
    {'rows': [{'key': '192.0.2.1', 'count': True}]},
    {'rows': [{'key': '192.0.2.1', 'count': 0}]},
    {'rows': [{'key': '192.0.2.1', 'count': 4}]},
    {'rows': [{'key': '192.0.2.1', 'count': 2}, {'key': '192.0.2.1', 'count': 1}]},
    {'rows': [{'key': '192.0.2.1', 'count': 2}]},
    {'rows': [{'key': '192.0.2.1', 'count': 1}] * 10001},
])
def test_clients_validate_core_response_without_leaking(monkeypatch, change):
    install_transport(monkeypatch, Response({**CLIENTS, **change}))
    with pytest.raises(monitor.MonitorError) as exc:
        monitor.clients(clients_manager(), 'oxi', MultiDict({'recorder': 'recorder'}))
    assert exc.value.code == 'INVALID_RESPONSE' and exc.value.status == 502
    assert 'core-password' not in str(exc.value)


def test_clients_and_exact_ip_require_new_agent_but_old_queries_keep_working(monkeypatch):
    calls = install_transport(monkeypatch, Response({'ok': True, 'records': [], 'next_cursor': None}))
    assert monitor.queries(Manager(), 'oxi', MultiDict())['records'] == []
    calls.clear()
    for operation, params in ((monitor.clients, {}), (monitor.queries, {'client_ip': '192.0.2.1'})):
        with pytest.raises(monitor.MonitorError) as exc:
            operation(Manager(), 'oxi', MultiDict(params))
        assert exc.value.code == 'UPGRADE_REQUIRED' and '1.7.1-go' in str(exc.value)
        assert not calls
    monitor.queries(clients_manager(), 'oxi', MultiDict({'client_ip': '2001:0DB8:0:0:0:0:0:2'}))
    assert calls[0][1]['params']['client_ip'] == '2001:db8::2'


@pytest.mark.parametrize('ip', ['192.168.', '192.0.2.1:53', 'localhost', 'fe80::2%eth0'])
def test_exact_client_filter_requires_a_complete_ip(monkeypatch, ip):
    calls = install_transport(monkeypatch, Response({}))
    with pytest.raises(monitor.MonitorError) as exc:
        monitor.queries(clients_manager(), 'oxi', MultiDict({'client_ip': ip}))
    assert exc.value.code == 'INVALID_QUERY' and not calls


@pytest.mark.parametrize('data,raw', [(None, b'404 page not found'), ({'code': 'NOT_FOUND'}, None)])
def test_missing_clients_endpoint_reports_correct_upgrade(monkeypatch, data, raw):
    install_transport(monkeypatch, Response(data, 404, raw))
    with pytest.raises(monitor.MonitorError) as exc:
        monitor.clients(clients_manager(), 'oxi', MultiDict())
    assert exc.value.code == 'UPGRADE_REQUIRED' and '1.7.1-go' in str(exc.value)


def test_clients_route_auth_no_store_and_agent_isolation(authenticated_app, monkeypatch):
    client, manager, _, admin, _ = authenticated_app
    registered = manager.register_agent({**{k: v for k, v in AGENT.items() if k not in ('id', 'token')}, 'version': '1.7.1-go'})
    aid = registered['id']
    calls = install_transport(monkeypatch, Response(CLIENTS))
    endpoint = f'/api/agents/{aid}/oxidns/clients'
    assert client.get(endpoint).status_code == 401
    assert client.get(endpoint, headers={'Authorization': 'Bearer ' + registered['token']}).status_code == 401
    assert client.post(endpoint, headers=admin).status_code == 405
    assert not calls
    response = client.get(endpoint, headers=admin)
    assert response.status_code == 200 and response.headers['Cache-Control'] == 'no-store'
    assert registered['token'] not in response.get_data(as_text=True)
    calls.clear()
    assert client.get('/api/agents/missing/oxidns/clients', headers=admin).status_code == 404
    response = client.get(endpoint + '?agent_id=elsewhere', headers=admin)
    assert response.status_code == 400 and response.headers['Cache-Control'] == 'no-store'
    assert not calls
