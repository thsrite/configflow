"""Converted nodes are remembered per bundled Sub-Store version.

Every content check and every aggregation a device fetches converts the same
URI nodes; in on-demand mode each question starts Sub-Store and keeps it for
minutes.
"""
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager

import pytest

from backend.utils import dependencies, sub_store_client as client

NODE = 'anytls://secret@node.example.test:443?sni=example.test#one'
OTHER = 'trojan://secret@other.example.test:443#two'


@pytest.fixture
def store(monkeypatch):
    """A bundled Sub-Store that answers, with every question recorded."""
    state = {'version': '2.42.3', 'builtin': True, 'asked': [], 'fail': set(), 'during': None}
    monkeypatch.setattr(dependencies, 'is_builtin_available', lambda: state['builtin'])
    monkeypatch.setattr(dependencies, 'active_bundle', lambda: ('/opt/sub-store', state['version']))

    def convert(proxy_string, target):
        state['asked'].append((proxy_string, target))
        if state['during']:
            state['during']()
        if isinstance(proxy_string, str) and proxy_string in state['fail']:
            return None
        return {'name': 'from-uri', 'type': 'anytls', 'server': str(proxy_string)[-12:], 'port': 443,
                'converted-by': state['version'], 'target': target, 'alpn': ['h2']}
    monkeypatch.setattr(client, '_convert_proxy_string', convert)
    monkeypatch.setattr(client, '_node_conversions', type(client._node_conversions)())
    return state


def test_the_same_node_is_converted_once_and_callers_get_their_own_copy(store):
    first = client.convert_proxy_string(NODE)
    assert store['asked'] == [(NODE, 'ClashMeta')]
    # convert_node_to_mihomo names the result and repairs its fields in place.
    first['name'] = 'renamed by the caller'
    first['alpn'].append('changed')
    second = client.convert_proxy_string(NODE)
    assert store['asked'] == [(NODE, 'ClashMeta')]
    assert second == {'name': 'from-uri', 'type': 'anytls', 'server': NODE[-12:], 'port': 443,
                      'converted-by': '2.42.3', 'target': 'ClashMeta', 'alpn': ['h2']}
    second['alpn'].append('changed again')
    assert client.convert_proxy_string(NODE)['alpn'] == ['h2']
    assert client.convert_proxy_string(OTHER)['server'] == OTHER[-12:]
    assert len(store['asked']) == 2


def test_a_failed_conversion_is_asked_again(store):
    store['fail'].add(NODE)
    assert client.convert_proxy_string(NODE) is None
    assert client.convert_proxy_string(NODE) is None
    assert len(store['asked']) == 2
    store['fail'].clear()
    assert client.convert_proxy_string(NODE)['type'] == 'anytls'
    assert client.convert_proxy_string(NODE)['type'] == 'anytls'
    assert len(store['asked']) == 3


def test_another_sub_store_version_converts_again(store):
    assert client.convert_proxy_string(NODE)['converted-by'] == '2.42.3'
    store['version'] = '2.43.0'
    assert client.convert_proxy_string(NODE)['converted-by'] == '2.43.0'
    assert client.convert_proxy_string(NODE)['converted-by'] == '2.43.0'
    assert len(store['asked']) == 2
    # Rolled back: the answers of that version are its own.
    store['version'] = '2.42.3'
    assert client.convert_proxy_string(NODE)['converted-by'] == '2.42.3'
    assert len(store['asked']) == 2


def test_an_update_during_the_conversion_keeps_nothing(store):
    """The version is read before the lock an update holds; whichever program
    answered, the result is only this call's."""
    store['during'] = lambda: store.update(version='2.43.0')
    assert client.convert_proxy_string(NODE) is not None
    store['during'] = None
    for version in ('2.42.3', '2.43.0'):
        store['version'] = version
        before = len(store['asked'])
        client.convert_proxy_string(NODE)
        assert len(store['asked']) == before + 1


@pytest.mark.parametrize('version', ['', None])
def test_nothing_is_kept_without_a_known_version(store, version):
    store['version'] = version
    client.convert_proxy_string(NODE)
    client.convert_proxy_string(NODE)
    assert len(store['asked']) == 2


def test_a_sub_store_outside_the_image_is_always_asked(store):
    """SUB_STORE_URL may point at anything, and it can change unnoticed."""
    store['builtin'] = False
    client.convert_proxy_string(NODE)
    client.convert_proxy_string(NODE)
    assert len(store['asked']) == 2
    store['builtin'] = True
    client.convert_proxy_string(NODE)
    client.convert_proxy_string(NODE)
    assert len(store['asked']) == 3


def test_each_target_format_has_its_own_result(store):
    assert client.convert_proxy_string(NODE)['target'] == 'ClashMeta'
    assert client.convert_proxy_string(NODE, target='Surge')['target'] == 'Surge'
    assert client.convert_proxy_string(NODE)['target'] == 'ClashMeta'
    assert client.convert_proxy_string(NODE, target='Surge')['target'] == 'Surge'
    assert len(store['asked']) == 2


def test_only_the_most_recently_used_nodes_are_kept(store, monkeypatch):
    monkeypatch.setattr(client, 'NODE_CONVERSION_LIMIT', 3)
    nodes = [f'ss://secret@node{number}.example.test:443#n' for number in range(4)]
    for node in nodes[:3]:
        client.convert_proxy_string(node)
    client.convert_proxy_string(nodes[0])  # used again: the oldest is now nodes[1]
    client.convert_proxy_string(nodes[3])
    assert len(store['asked']) == 4 and len(client._node_conversions) == 3
    client.convert_proxy_string(nodes[0])
    client.convert_proxy_string(nodes[2])
    client.convert_proxy_string(nodes[3])
    assert len(store['asked']) == 4
    client.convert_proxy_string(nodes[1])
    assert len(store['asked']) == 5


def test_a_value_that_is_not_text_is_passed_through(store):
    value = {'type': 'ss', 'server': 'node.example.test'}
    assert client.convert_proxy_string(value) is not None
    assert client.convert_proxy_string(value) is not None
    assert len(store['asked']) == 2 and not client._node_conversions


def test_a_version_file_that_cannot_be_read_only_costs_the_memory(store, monkeypatch):
    def broken():
        raise OSError('unreadable')
    monkeypatch.setattr(dependencies, 'active_bundle', broken)
    assert client.convert_proxy_string(NODE) is not None
    assert client.convert_proxy_string(NODE) is not None
    assert len(store['asked']) == 2


def test_concurrent_requests_for_one_node_agree(store):
    with ThreadPoolExecutor(8) as pool:
        results = list(pool.map(lambda _: client.convert_proxy_string(NODE), range(64)))
    assert all(result == results[0] for result in results)
    assert len({id(result) for result in results}) == 64
    before = len(store['asked'])
    client.convert_proxy_string(NODE)
    assert len(store['asked']) == before


def test_rendering_again_does_not_start_sub_store(store, monkeypatch):
    """Through the real client: the second conversion makes no request at all,
    so the on-demand runtime is never entered and Sub-Store stays asleep."""
    monkeypatch.undo()
    uses, requested = [], []

    @contextmanager
    def use():
        uses.append('start')
        yield
    monkeypatch.setattr(client.runtime, 'use', use)
    monkeypatch.setattr(dependencies, 'is_builtin_available', lambda: True)
    monkeypatch.setattr(dependencies, 'active_bundle', lambda: ('/opt/sub-store', '2.42.3'))
    monkeypatch.setattr(client, '_node_conversions', type(client._node_conversions)())
    monkeypatch.setattr(client, '_create_subscription', lambda *args, **kwargs: requested.append('create'))
    monkeypatch.setattr(client, '_delete_subscription', lambda *args, **kwargs: requested.append('delete'))

    class Response:
        text = 'proxies:\n  - {name: from-uri, type: anytls, server: node.example.test, port: 443, password: secret}\n'

        def raise_for_status(self):
            pass

    def get(url, **kwargs):
        requested.append('download')
        assert kwargs['params'] == {'content': NODE}
        return Response()
    monkeypatch.setattr(client.requests, 'get', get)

    from backend.converters.mihomo import convert_node_to_mihomo
    first = convert_node_to_mihomo({'name': 'Home', 'proxy_string': NODE})
    assert uses == ['start'] and requested == ['create', 'download', 'delete']
    second = convert_node_to_mihomo({'name': 'Home', 'proxy_string': NODE})
    third = convert_node_to_mihomo({'name': 'Away', 'proxy_string': NODE})
    assert uses == ['start'] and requested == ['create', 'download', 'delete']
    assert first == second and first['name'] == 'Home' and first['server'] == 'node.example.test'
    assert third == {**first, 'name': 'Away'}
