"""Every node and rule of a push diff kept compressed, read back page by page and searched."""
import json
import re
import time
import tracemalloc

import pytest

from backend.agents import push_diff
from backend.agents.content_sync import ContentSyncChecker
from backend.agents.push_diff import build_push_diff, search_query, without_secrets, without_stored
from backend.test_agent_push_diff import (BASE_URL, agent_for, base_config, bundle, provider, provider_bytes,
                                          receipt_for)
from backend.test_agent_content_sync import setup  # noqa: F401  (checker fixture with a deferred executor)


def kept_rules(old_lines, new_lines, budget=None):
    keep = push_diff._Keep()
    old = ('\n'.join(old_lines) + '\n').encode()
    new = ('\n'.join(new_lines) + '\n').encode()
    summary, details = push_diff._rule_change(old, new, budget or push_diff._DetailBudget(), keep)
    return details, keep.claim()


def kept_nodes(old_nodes, new_nodes):
    keep = push_diff._Keep()
    summary, details = push_diff._node_change(provider_bytes(old_nodes), provider_bytes(new_nodes),
                                              push_diff._DetailBudget(), keep)
    return details, keep.claim()


def read_all(entries, change, query=None, find=None, limit=push_diff.PAGE_SIZE):
    """Every page from the start, as the dialog's 加载更多 does."""
    pages, offset = [], 0
    while offset is not None:
        page = entries.page(change, offset, limit, query, find)
        pages.append(page)
        offset = page['next_offset']
    return [entry for page in pages for entry in page['entries']], pages


def texts(entries):
    return [push_diff.unpack(entry['text'])[0] for entry in entries]


def groups_of(details):
    return {group['change']: group for group in details['groups']}


def test_the_first_page_is_listed_and_every_rule_kept_in_order():
    old = [f'+.site-{index:04d}.example' for index in range(1200)]
    new = old[150:] + [f'+.fresh-{index:03d}.example' for index in range(300)]
    details, entries = kept_rules(old, new)
    groups = groups_of(details)
    assert details['paging'] is True
    assert [(change, groups[change]['total'], len(groups[change]['entries']), groups[change]['available'])
            for change in ('added', 'removed')] == [('added', 300, 20, 300), ('removed', 150, 20, 150)]
    added, pages = read_all(entries, 'added')
    assert texts(added) == [f'+.fresh-{index:03d}.example' for index in range(300)]
    # The response's first page is the start of the kept list; 加载更多 continues after it.
    assert added[:20] == groups['added']['entries']
    assert [(page['offset'], len(page['entries']), page['next_offset']) for page in pages] == [
        (0, 50, 50), (50, 50, 100), (100, 50, 150), (150, 50, 200), (200, 50, 250), (250, 50, None)]
    second = entries.page('added', 20, 50)
    assert (second['total'], second['offset'], second['next_offset']) == (300, 20, 70)
    assert texts(second['entries']) == [f'+.fresh-{index:03d}.example' for index in range(20, 70)]
    removed, _ = read_all(entries, 'removed', limit=200)
    assert texts(removed) == old[:150]
    assert entries.size < 8 * 1024  # compressed


def test_page_bounds_offsets_past_the_end_and_the_byte_limit(monkeypatch):
    details, entries = kept_rules(['+.keep.example'], ['+.keep.example'] + [f'+.n{index}.example' for index in range(55)])
    last = entries.page('added', 50, 50)
    assert (len(last['entries']), last['next_offset'], last['total']) == (5, None, 55)
    assert entries.page('added', 55, 50) == {'total': 55, 'offset': 55, 'entries': [], 'next_offset': None}
    assert entries.page('added', 10_000, 50)['entries'] == []
    assert len(entries.page('added', 0, 200)['entries']) == 55
    with pytest.raises(KeyError):
        entries.page('removed', 0, 50)  # nothing removed: no such group
    # A page stops at its byte limit (but always carries one entry) and says where to go on.
    monkeypatch.setattr(push_diff, 'MAX_PAGE_BYTES', 120)
    page = entries.page('added', 0, 50)
    assert 1 < len(page['entries']) < 50 and page['next_offset'] == len(page['entries'])
    monkeypatch.setattr(push_diff, 'MAX_PAGE_BYTES', 1)
    assert len(entries.page('added', 3, 50)['entries']) == 1


HK = {'name': 'HK 香港 01', 'type': 'ss', 'server': 'edge-hk.example', 'port': 443, 'cipher': 'aes-128-gcm',
      'password': 'needle-pass-before'}


def test_nodes_are_searched_by_name_info_and_field_but_never_by_credentials():
    old = [HK, {'name': 'Gone JP', 'type': 'socks5', 'server': 'gone.example', 'port': 1080}]
    new = [{**HK, 'port': 8443, 'password': 'needle-pass-after'},
           {'name': 'Fresh SG', 'type': 'trojan', 'server': 'sg-edge.example', 'port': 443, 'password': 'trojan-pw-1'},
           {'name': 'Relay', 'type': 'http', 'server': 'relay.example', 'port': 80,
            'username': 'u', 'headers': {'X-Token': 'opaque'}}]
    details, entries = kept_nodes(old, new)

    def found(change, text, find=None):
        return texts(entries.page(change, 0, 50, search_query(text), find)['entries'])

    assert found('added', 'fresh') == ['Fresh SG'] and found('added', 'FRESH sg') == ['Fresh SG']  # case-insensitive
    assert found('added', 'sg-edge.example:443') == ['Fresh SG']  # a new node's type and server:port
    assert found('added', 'trojan') == ['Fresh SG']
    assert found('removed', 'gone') == ['Gone JP']
    assert found('removed', 'socks5') == ['Gone JP'] and found('removed', 'gone.example:1080') == ['Gone JP']  # its info too
    assert found('modified', '香港') == ['HK 香港 01']
    assert found('modified', 'port') == ['HK 香港 01'] and found('modified', '8443') == ['HK 香港 01']
    assert found('modified', 'password') == ['HK 香港 01']  # the field's name, not its value
    for secret in ('needle', 'pass-before', 'needle-pass-after', 'trojan-pw'):
        assert found('modified', secret) == [] and found('added', secret) == []
    # Nothing found: an empty page, still a definite answer.
    assert entries.page('modified', 0, 50, search_query('nothing-like-this'), None) == {
        'total': 0, 'offset': 0, 'entries': [], 'next_offset': None}
    assert search_query('  \n ') is None and search_query(None) is None


def test_rules_are_searched_without_credential_pieces_or_stored_tokens():
    old = ['DOMAIN-SUFFIX,keep.example']
    new = old + ['DOMAIN-SUFFIX,Mixed-Case.Example', 'URL-REGEX,https://feed.example/sub?token=abcdefTOKEN1234567890',
                 'DOMAIN,stored-tok-1.relay.example', 'DOMAIN,other.example']
    details, entries = kept_rules(old, new)

    def found(text, find=None):
        return texts(entries.page('added', 0, 50, search_query(text), find)['entries'])

    assert found('mixed-case.example') == ['DOMAIN-SUFFIX,Mixed-Case.Example']
    assert found('feed.example/sub?token=') == ['URL-REGEX,https://feed.example/sub?token=abcdefTOKEN1234567890']
    assert found('abcdefTOKEN') == [] and found('token=abcdef') == []  # a match never reaches into a credential
    assert found('other') == ['DOMAIN,other.example']
    # ConfigFlow's stored tokens are replaced only when a page is sent: search leaves them out as well.
    def find(text):
        return [(match.start(), match.end()) for match in re.finditer('stored-tok-1', text)]
    assert found('stored-tok', find) == [] and found('tok-1.relay', find) == []
    assert found('relay.example', find) == ['DOMAIN,stored-tok-1.relay.example']
    page = without_stored(entries.page('added', 0, 50, search_query('relay'), find), find)
    assert page['entries'][0]['text'] == ['DOMAIN,', {'secret': '[REDACTED]'}, '.relay.example']
    # Pages keep credential marks like the first one, so 隐藏敏感信息 hides them in loaded entries too.
    url = entries.page('added', 0, 50, search_query('feed'))['entries'][0]['text']
    assert url[-1] == {'secret': 'abcdefTOKEN1234567890'}
    assert 'abcdefTOKEN' not in json.dumps(without_secrets(entries.page('added', 0, 50)), ensure_ascii=False)


def test_search_results_page_like_the_list():
    new = [f'DOMAIN,match-{index}.example' if index % 3 == 0 else f'DOMAIN,other-{index}.example' for index in range(400)]
    details, entries = kept_rules([], new)
    matched, pages = read_all(entries, 'added', search_query('MATCH-'))
    assert texts(matched) == [f'DOMAIN,match-{index}.example' for index in range(0, 400, 3)]
    assert pages[0]['total'] == 134 and [page['next_offset'] for page in pages] == [50, 100, None]


def test_kept_entries_are_bounded_per_file_and_per_comparison(monkeypatch):
    monkeypatch.setattr(push_diff, 'MAX_KEPT_ENTRIES', 100)
    old = [f'+.old-{index}.example' for index in range(30)]
    new = [f'+.new-{index}.example' for index in range(500)]
    details, entries = kept_rules(old, new)
    groups = groups_of(details)
    # Shared fairly: the small group is kept in full, the rest goes to the large one.
    assert (groups['added']['available'], groups['removed']['available']) == (70, 30)
    assert groups['added']['total'] == 500
    assert texts(read_all(entries, 'added')[0]) == new[:70]  # a contiguous start
    monkeypatch.setattr(push_diff, 'MAX_KEPT_ENTRIES', 100_000)
    # Bytes per file: the kept part stops early, the first page is still listed.
    monkeypatch.setattr(push_diff, 'MAX_KEPT_FILE_BYTES', 3000)
    details, entries = kept_rules([], new)
    added = groups_of(details)['added']
    assert push_diff.FIRST_PAGE <= added['available'] < 100 and len(added['entries']) == push_diff.FIRST_PAGE
    assert len(read_all(entries, 'added')[0]) == added['available']
    monkeypatch.setattr(push_diff, 'MAX_KEPT_FILE_BYTES', 16 * 1024 * 1024)
    # Bytes per comparison: once spent, later files are listed without paging.
    monkeypatch.setattr(push_diff, 'MAX_KEPT_BYTES', 6000)
    rules = {f'ruleset/R{index}.list': ''.join(f'+.r{index}-{line}.example\n' for line in range(200)) for index in range(3)}
    old_bundle = bundle(base_config())
    new_bundle = bundle(base_config(), rules=rules, deployment='d2')
    lists = {}
    result = build_push_diff(agent_for(old_bundle), new_bundle, None, receipt=receipt_for(old_bundle), lists=lists)
    files = {item['path']: item['details'] for item in result['files'] if item.get('details')}
    assert files['ruleset/R0.list']['paging'] is True and 'ruleset/R0.list' in lists
    assert 'paging' not in files['ruleset/R2.list'] and 'ruleset/R2.list' not in lists
    assert len(files['ruleset/R2.list']['groups'][0]['entries']) == push_diff.FIRST_PAGE


def test_large_lists_are_kept_compact_and_read_back_quickly():
    """6 万条规则全部替换（超过每个文件 10 万条的上限）和 1 万个节点全部修改：耗时与内存。"""
    def node(index, port=443):
        return {'name': f'🇭🇰 节点 {index:05d}', 'type': 'vmess', 'server': f's{index}.example.com', 'port': port,
                'uuid': f'uuid-{index:05d}-aaaa-bbbb-cccc', 'network': 'ws',
                'ws-opts': {'path': '/ray', 'headers': {'Host': f'cdn{index}.example.com'}}}
    old_nodes = [node(index) for index in range(10000)]
    new_nodes = [{**node(index, 8443), 'uuid': f'uuid-{index:05d}-dddd-eeee-ffff'} for index in range(10000)]
    old_rules = [f'DOMAIN-SUFFIX,domain-{index}.example.com' for index in range(60000)]
    new_rules = [f'DOMAIN-SUFFIX,fresh-{index}.example.org' for index in range(60000)]
    old = bundle(base_config(), providers={'Feed': provider_bytes(old_nodes).decode()},
                 rules={'ruleset/Big.list': '\n'.join(old_rules) + '\n'})
    new = bundle(base_config(), providers={'Feed': provider_bytes(new_nodes).decode()},
                 rules={'ruleset/Big.list': '\n'.join(new_rules) + '\n'}, deployment='d2')
    lists = {}
    started = time.perf_counter()
    result = build_push_diff(agent_for(old), new, None, receipt=receipt_for(old), lists=lists)
    built = time.perf_counter() - started
    files = {item['path']: item for item in result['files']}
    rules = groups_of(files['ruleset/Big.list']['details'])
    # 120,000 changes: 100,000 kept, shared between the groups; the response says how many.
    assert (rules['added']['total'], rules['added']['available']) == (60000, 50000)
    assert (rules['removed']['total'], rules['removed']['available']) == (60000, 50000)
    nodes = groups_of(files['providers/Feed.yaml']['details'])
    assert (nodes['modified']['total'], nodes['modified']['available']) == (10000, 10000)
    assert len(json.dumps(result, ensure_ascii=False).encode()) < 64 * 1024  # the response stays small
    stored = sum(item.size for item in lists.values())
    raw = sum(len(push_diff.zlib.decompress(group.entries)) + len(push_diff.zlib.decompress(group.search))
              for item in lists.values() for group in item.groups.values())

    started = time.perf_counter()
    deep = lists['ruleset/Big.list'].page('added', 49950, 50)
    deep_node = lists['providers/Feed.yaml'].page('modified', 9950, 50)
    paged = time.perf_counter() - started
    assert texts(deep['entries'])[-1] == 'DOMAIN-SUFFIX,fresh-49999.example.org' and deep['next_offset'] is None
    assert deep_node['entries'][0]['fields'][0] == {'key': 'port', 'change': 'modified', 'sensitive': False,
                                                    'old': '443', 'new': '8443'}
    tracemalloc.start()
    started = time.perf_counter()
    found = lists['ruleset/Big.list'].page('added', 0, 50, search_query('FRESH-4999'))
    found_nodes = lists['providers/Feed.yaml'].page('modified', 0, 50, search_query('节点 0999'))
    everything = lists['ruleset/Big.list'].page('removed', 0, 50, search_query('domain'))
    searched = time.perf_counter() - started
    _, peak = tracemalloc.get_traced_memory()
    tracemalloc.stop()
    assert found['total'] == 11 and found_nodes['total'] == 10 and everything['total'] == 50000
    feed = lists['providers/Feed.yaml']
    assert feed.page('modified', 0, 50, search_query('UUID'))['total'] == 10000  # the field's name
    for secret in ('aaaa-bbbb', 'dddd-eeee', 'uuid-00001'):  # never its value
        assert feed.page('modified', 0, 50, search_query(secret))['total'] == 0
    print(f'\nbuild {built:.2f}s, kept {stored / 1048576:.2f} MiB compressed of {raw / 1048576:.1f} MiB, '
          f'two deep pages {paged * 1000:.0f} ms, three searches {searched * 1000:.0f} ms '
          f'(peak {peak / 1048576:.1f} MiB while searching)')
    assert stored < 6 * 1024 * 1024, stored
    assert built < 20, built
    assert paged < 1, paged
    assert searched < 3, searched


# The checker keeps every entry with the change list, under its version.

def fake_build(lists_by_path):
    """A build_push_diff that answers one changed rule set and keeps its entries."""
    def build(agent, bundle, repository, *, lists=None, **_):
        details = {}
        for path, (old, new) in lists_by_path.items():
            details[path], entries = kept_rules(old, new)
            if lists is not None:
                lists[path] = entries
        return {'status': 'changed', 'files': [{'path': path, 'role': 'rule', 'status': 'modified', 'details': value}
                                               for path, value in details.items()]}
    return build


RULES = {'ruleset/Big.list': ([], [f'+.n{index}.example' for index in range(120)])}


def entries_of(setup, version, **kwargs):
    checker, _, _, agent, config, _, _ = setup
    options = {'path': 'ruleset/Big.list', 'change': 'added', 'offset': 20, 'limit': 50, **kwargs}
    return checker.get_push_diff_entries(agent, config, BASE_URL, version=version, **options)


def settle(setup):
    checker, executor, _, agent, config, prepare, _ = setup
    assert checker.get_push_diff(agent, config, BASE_URL, prepare)['status'] == 'checking'
    executor.run()
    return checker.get_push_diff(agent, config, BASE_URL, prepare)


def test_pages_come_from_the_shown_change_list_and_expire_with_it(setup, monkeypatch):
    checker, executor, now, agent, config, prepare, _ = setup
    monkeypatch.setattr('backend.agents.push_diff.build_push_diff', fake_build(RULES))
    config['test_rule'] = 'DOMAIN,changed.example.test\n'
    shown = settle(setup)
    version = shown['version']
    assert shown['files'][0]['details']['paging'] is True and 'lists' not in shown
    page = entries_of(setup, version)
    assert (page['status'], page['version'], page['total'], page['offset'], page['next_offset']) == (
        'ok', version, 120, 20, 70)
    assert texts(page['entries'])[0] == '+.n20.example'
    searched = entries_of(setup, version, offset=0, query=search_query('N11'))
    assert searched['total'] == 11 and searched['next_offset'] is None
    assert entries_of(setup, 'not-this-one') == {'status': 'stale', 'reason': 'replaced'}
    with pytest.raises(KeyError):
        entries_of(setup, version, path='ruleset/Other.list')
    with pytest.raises(KeyError):
        entries_of(setup, version, change='removed')
    # The configuration changed since: the list no longer describes the next push.
    changed = {**config, 'test_rule': 'DOMAIN,again.example.test\n'}
    assert checker.get_push_diff_entries(agent, changed, BASE_URL, version=version, path='ruleset/Big.list',
                                         change='added') == {'status': 'stale', 'reason': 'changed'}
    pending = {**agent, 'latest_deployment': {'status': 'uploading'}}
    assert checker.get_push_diff_entries(pending, config, BASE_URL, version=version, path='ruleset/Big.list',
                                         change='added') == {'status': 'stale', 'reason': 'changed'}
    # Past the result's interval the list goes with it.
    now[0] += 901
    assert entries_of(setup, version) == {'status': 'stale', 'reason': 'expired'}
    # A new comparison is another version: the old one's pages are not mixed in.
    fresh = settle(setup)
    assert fresh['version'] != version
    assert entries_of(setup, version) == {'status': 'stale', 'reason': 'replaced'}
    assert entries_of(setup, fresh['version'])['status'] == 'ok'
    # Subscription caches changed (invalidate): the list is gone.
    checker.invalidate()
    assert entries_of(setup, fresh['version']) == {'status': 'stale', 'reason': 'expired'}


def test_kept_lists_are_bounded_in_bytes_across_agents(setup, monkeypatch):
    checker, executor, _, agent, config, prepare, _ = setup
    many = {'ruleset/Big.list': ([], [f'+.{index}-{index * 7919 % 10007}.example' for index in range(3000)])}
    monkeypatch.setattr('backend.agents.push_diff.build_push_diff', fake_build(many))
    config['test_rule'] = 'DOMAIN,changed.example.test\n'
    probe = ContentSyncChecker(executor=executor)
    size = None
    for index in range(4):
        item = {**agent, 'id': f'agent-{index}'}
        checker.get_push_diff(item, config, BASE_URL, prepare)
        executor.run()
        size = size or checker._details[('', item['id'])].size
    assert size > 0
    # Room for two lists: the oldest ones go first, the newest always stays.
    checker._max_list_bytes = 2 * size + 1
    item = {**agent, 'id': 'agent-4'}
    checker.get_push_diff(item, config, BASE_URL, prepare)
    executor.run()
    assert [identity[1] for identity in checker._details] == ['agent-3', 'agent-4']
    checker._max_list_bytes = 1
    item = {**agent, 'id': 'agent-5'}
    checker.get_push_diff(item, config, BASE_URL, prepare)
    executor.run()
    assert [identity[1] for identity in checker._details] == ['agent-5']
    assert probe._max_list_bytes == 48 * 1024 * 1024
