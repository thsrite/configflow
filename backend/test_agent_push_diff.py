"""What a push would change: file digests, YAML key paths, bounded lists, marked credentials, speed."""
import difflib
import json
import time

import pytest
import yaml

from backend.agents import push_diff
from backend.agents.deployment_bundle import build_deployment_bundle, deployment_content_digests
from backend.agents.push_diff import (build_push_diff, diff_config, rule_entries, secret_spans, unpack, without_secrets,
                                      without_stored)

SECRETS = {
    'ss-password': 'Pa55-ss-node-0001',
    'vmess-uuid': '3f1d2c4b-1111-4a2b-9c3d-abcdefabcdef',
    'wg-key': 'cGF5bG9hZC1wcml2YXRlLWtleS13aXJlZ3VhcmQ9',
    'controller': 'controller-secret-0002',
    'snell-psk': 'snell-psk-value-0003',
    'hy2-obfs': 'obfs-password-0004',
    'sub-token': 'abcDEF0123456789subscriptionTOKEN',
    'url-pass': 'urlPassw0rd0005',
    'header': 'Bearer header-token-0006',
    'auth-user': 'admin:auth-pass-0007',
    'path-token': 'Q2xhc2hTdWJzY3JpcHRpb25QYXRoVG9rZW4wMDg',
}


def dump(document):
    return yaml.safe_dump(document, allow_unicode=True, sort_keys=False).encode()


def base_config():
    return {
        'mixed-port': 7890,
        'log-level': 'info',
        'external-controller': '127.0.0.1:9090',
        'secret': 'old-controller-secret',
        'hosts': {'nas.lan': '192.168.1.10'},
        'dns': {
            'enable': True,
            'fake-ip-filter': ['+.lan', 'time.example.com'],
            'proxy-server-nameserver-policy': {'old.example.com': '192.0.2.53'},
        },
        'proxies': [
            {'name': 'HK 01', 'type': 'ss', 'server': 'hk.example.com', 'port': 443, 'cipher': 'aes-128-gcm',
             'password': 'old-ss-password-9999'},
            {'name': 'JP 01', 'type': 'vmess', 'server': '198.51.100.7', 'port': 8443, 'uuid': 'old-uuid-0000-1111'},
        ],
        'proxy-groups': [{'name': 'Proxy', 'type': 'select', 'proxies': ['HK 01', 'JP 01', 'DIRECT']}],
        'rules': ['DOMAIN-SUFFIX,example.com,Proxy', 'MATCH,DIRECT'],
        'proxy-providers': {'Feed': {
            'type': 'http', 'path': './providers/Feed.yaml',
            'url': 'https://sub.example.com:8443/api/v1/client/subscribe?token=OLDtoken0123456789abcdefXYZ&flag=clash'}},
    }


def changed_config():
    document = base_config()
    document['log-level'] = 'debug'
    document['secret'] = SECRETS['controller']
    document['hosts']['printer.lan'] = '192.168.1.20'
    document['dns']['fake-ip-filter'].append('+.stun.example.org')
    document['dns']['proxy-server-nameserver-policy']['new.example.com'] = '192.0.2.54'
    document['proxies'] = [item for item in document['proxies'] if item['name'] != 'JP 01']
    document['proxies'][0]['password'] = SECRETS['ss-password']
    document['proxies'][0]['port'] = 8388
    document['proxies'].append({'name': 'US 01', 'type': 'snell', 'server': 'us.example.net', 'port': 9443,
                                'psk': SECRETS['snell-psk']})
    document['proxies'].append({'name': 'WG', 'type': 'wireguard', 'server': '203.0.113.9', 'port': 51820,
                                'private-key': SECRETS['wg-key']})
    document['proxies'].append({'name': 'HY2', 'type': 'hysteria2', 'server': 'hy.example.org', 'port': 443,
                                'obfs-password': SECRETS['hy2-obfs'], 'password': SECRETS['url-pass']})
    document['proxy-groups'][0]['proxies'] = ['HK 01', 'US 01', 'DIRECT']
    document['rules'].insert(1, 'DOMAIN,api.example.org,Proxy')
    document['proxy-providers']['Feed'].update(
        url=f'https://sub.example.com:8443/api/v1/client/subscribe?token={SECRETS["sub-token"]}&flag=clash',
        header={'Authorization': [SECRETS['header']]})
    document['rule-providers'] = {
        'Private': {'type': 'http', 'behavior': 'domain', 'path': './ruleset/Private.list',
                    'url': f'https://user:{SECRETS["url-pass"]}@rules.example.com/{SECRETS["path-token"]}/private.list'}}
    document['authentication'] = [SECRETS['auth-user']]
    return document


def items_by_key(result):
    return {item['key']: item for item in result['items']}


def raw_lines(old, new):
    """The unified diff of the documents as yaml.safe_dump writes them, nothing replaced."""
    return list(difflib.unified_diff(yaml.safe_dump(old, allow_unicode=True, sort_keys=False).splitlines(),
                                     yaml.safe_dump(new, allow_unicode=True, sort_keys=False).splitlines(),
                                     'a/config.yaml', 'b/config.yaml', lineterm=''))


def secrets_of(value):
    """Every credential piece of a response or display text."""
    if isinstance(value, dict):
        if set(value) == {'secret'}:
            return [value['secret']]
        return [piece for item in value.values() for piece in secrets_of(item)]
    if isinstance(value, list):
        return [piece for item in value for piece in secrets_of(item)]
    return []


def test_main_yaml_is_compared_by_key_path_with_old_and_new_values():
    result = diff_config(dump(base_config()), dump(changed_config()))
    items = items_by_key(result)
    assert result['status'] == 'compared'
    assert items['log-level'] == {'key': 'log-level', 'change': 'modified', 'sensitive': False,
                                  'old': 'info', 'new': 'debug'}
    assert items['hosts.printer.lan'] == {'key': 'hosts.printer.lan', 'change': 'added', 'sensitive': False,
                                          'old': None, 'new': '192.168.1.20'}
    policy = items['dns.proxy-server-nameserver-policy.new.example.com']
    assert (policy['change'], policy['new']) == ('added', '192.0.2.54')
    assert 'dns.proxy-server-nameserver-policy.old.example.com' not in items
    fake = items['dns.fake-ip-filter']['list']
    assert (fake['before'], fake['after'], fake['added'], fake['removed']) == (2, 3, 1, 0)
    assert fake['entries'] == [{'change': 'added', 'text': '+.stun.example.org'}]
    rules = items['rules']['list']
    assert (rules['before'], rules['after'], rules['added'], rules['removed'], rules['reordered']) == (2, 3, 1, 0, False)
    assert rules['entries'] == [{'change': 'added', 'text': 'DOMAIN,api.example.org,Proxy'}]
    assert 'rules.0' not in items and 'mixed-port' not in items  # unchanged keys and list indexes are not rows


def test_named_lists_match_entries_by_name_and_list_member_changes():
    items = items_by_key(diff_config(dump(base_config()), dump(changed_config())))
    proxies = items['proxies']['list']
    assert (proxies['before'], proxies['after'], proxies['added'], proxies['removed'], proxies['modified']) == (2, 4, 3, 1, 1)
    entries = {(entry['change'], entry['text']): entry for entry in proxies['entries']}
    assert {('added', 'US 01'), ('added', 'WG'), ('added', 'HY2'), ('removed', 'JP 01'), ('modified', 'HK 01')} == set(entries)
    fields = {field['key']: field for field in entries['modified', 'HK 01']['fields']}
    assert fields['port'] == {'key': 'port', 'change': 'modified', 'sensitive': False, 'old': '443', 'new': '8388'}
    assert fields['password'] == {'key': 'password', 'change': 'modified', 'sensitive': True,
                                  'old': [{'secret': 'old-ss-password-9999'}], 'new': [{'secret': SECRETS['ss-password']}]}
    group = items['proxy-groups']['list']['entries'][0]
    assert (group['change'], group['text']) == ('modified', 'Proxy')
    members = group['fields'][0]
    assert members['key'] == 'proxies'
    assert members['list']['added_items'] == ['US 01'] and members['list']['removed_items'] == ['JP 01']


def test_reordered_rules_are_reported_even_without_additions():
    old = base_config()
    new = base_config()
    new['rules'] = list(reversed(new['rules']))
    rules = items_by_key(diff_config(dump(old), dump(new)))['rules']['list']
    assert (rules['added'], rules['removed'], rules['reordered']) == (0, 0, True)


def test_long_lists_are_counted_exactly_but_truncated():
    old = {'rules': [f'DOMAIN,keep-{index}.example.com,DIRECT' for index in range(100)]}
    new = {'rules': old['rules'][50:] + [f'DOMAIN,new-{index}.example.com,Proxy' for index in range(500)]}
    rules = items_by_key(diff_config(dump(old), dump(new)))['rules']['list']
    assert (rules['before'], rules['after'], rules['added'], rules['removed']) == (100, 550, 500, 50)
    assert len(rules['entries']) == push_diff.MAX_LIST_ENTRIES
    assert rules['omitted'] == 550 - push_diff.MAX_LIST_ENTRIES
    assert [entry['change'] for entry in rules['entries']] == ['added'] * push_diff.MAX_LIST_ENTRIES


def test_rows_values_and_raw_diff_are_bounded():
    old = {'hosts': {}}
    new = {'long': 'x' * 5000,
           'hosts': {f'host-{index}.lan': f'192.168.{index // 250}.{index % 250}' for index in range(1000)}}
    result = diff_config(dump(old), dump(new))
    assert len(result['items']) == push_diff.MAX_CONFIG_ROWS
    assert result['omitted'] == 1001 - push_diff.MAX_CONFIG_ROWS and result['total'] == 1001
    assert [unpack(line)[0] for line in result['raw']['lines']] == raw_lines(old, new)
    big = {'hosts': {f'host-{index}.lan': 'x' * 100 for index in range(3000)}}
    raw = diff_config(dump(old), dump(big))['raw']
    assert sum(len(unpack(line)[0].encode()) + 1 for line in raw['lines']) <= push_diff.MAX_RAW_BYTES
    assert len(raw['lines']) <= push_diff.MAX_RAW_LINES and raw['truncated']
    assert raw['omitted_lines'] == len(raw_lines(old, big)) - len(raw['lines'])
    assert [unpack(line)[0] for line in raw['lines']] == raw_lines(old, big)[:len(raw['lines'])]  # the first part
    huge = {'rules': [f'DOMAIN,{index}.example.com,DIRECT' for index in range(push_diff.MAX_RAW_INPUT_LINES)]}
    assert diff_config(dump({'rules': []}), dump(huge))['raw'] == {
        'lines': None, 'reason': 'too_large', 'total_lines': push_diff.MAX_RAW_INPUT_LINES + 2}
    long_row = items_by_key(result)['long']
    assert len(long_row['new']) < 300 and long_row['new'].endswith('（共 5000 字符）')


def test_values_are_returned_with_every_credential_marked():
    old, new = base_config(), changed_config()
    result = diff_config(dump(old), dump(new))
    encoded = json.dumps(result, ensure_ascii=False)
    hidden = json.dumps(without_secrets(result), ensure_ascii=False)
    shown = [*(value for name, value in SECRETS.items() if name != 'vmess-uuid'), 'old-controller-secret', 'old-ss-password-9999', 'old-uuid-0000-1111',
             'OLDtoken0123456789abcdefXYZ']
    for value in shown:
        assert value in encoded, value  # the dialog shows values as they are by default
        assert value not in hidden, value  # ... and hides every one of them on request
    for value in ('auth-pass-0007', 'user:urlPassw0rd0005', 'urlPassw0rd0005'):
        assert value not in hidden, value
    items = items_by_key(result)
    assert items['secret'] == {'key': 'secret', 'change': 'modified', 'sensitive': True,
                               'old': [{'secret': 'old-controller-secret'}], 'new': [{'secret': SECRETS['controller']}]}
    assert items['authentication'] == {'key': 'authentication', 'change': 'added', 'sensitive': True, 'old': None,
                                       'new': [{'secret': '["admin:auth-pass-0007"]'}]}
    assert items['proxy-providers.Feed.header.Authorization'] == {
        'key': 'proxy-providers.Feed.header.Authorization', 'change': 'added', 'sensitive': True, 'old': None,
        'new': [{'secret': '["Bearer header-token-0006"]'}]}
    # Host, port, path and harmless parameters are plain; only the token is a credential piece.
    feed = items['proxy-providers.Feed.url']
    assert feed['old'] == ['https://sub.example.com:8443/api/v1/client/subscribe?token=',
                           {'secret': 'OLDtoken0123456789abcdefXYZ'}, '&flag=clash']
    assert feed['new'][1] == {'secret': SECRETS['sub-token']} and feed['sensitive'] is False
    # A whole new provider is one row; the credentials inside its JSON are marked.
    private = items['rule-providers.Private']
    assert private['change'] == 'added' and private['old'] is None
    text, _ = unpack(private['new'])
    assert json.loads(text)['url'] == f'https://user:{SECRETS["url-pass"]}@rules.example.com/{SECRETS["path-token"]}/private.list'
    assert secrets_of(private['new']) == [f'user:{SECRETS["url-pass"]}', SECRETS['path-token']]
    assert not any(key.startswith('rule-providers.Private.') for key in items)
    # Domains, IP addresses and ports are never marked.
    for visible in ('hk.example.com', '203.0.113.9', '51820', 'us.example.net', '192.168.1.20', 'rules.example.com'):
        assert visible in hidden
    # The raw diff is the real unified diff, line for line, with the same credentials marked.
    raw = result['raw']['lines']
    assert [unpack(line)[0] for line in raw] == raw_lines(old, new)
    assert ['-secret: ', {'secret': 'old-controller-secret'}] in raw
    assert ['+secret: ', {'secret': SECRETS['controller']}] in raw
    assert ['+  password: ', {'secret': SECRETS['ss-password']}] in raw
    assert '+  printer.lan: 192.168.1.20' in raw


def test_raw_diff_marks_credentials_exactly_where_yaml_writes_them():
    pem = '-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmU\n-----END OPENSSH PRIVATE KEY-----\n'
    long_auth = ' '.join(f'word{index}secret' for index in range(30))  # folded over several lines
    new = {
        'proxies': [
            {'name': 'quote', 'type': 'ss', 'password': "it's-a-secret"},
            {'name': 'digits', 'type': 'ss', 'password': '123456'},
            {'name': 'number', 'type': 'ss', 'password': 987654},
            {'name': 'ssh', 'type': 'ssh', 'private-key': pem},
            {'name': 'nested', 'type': 'http', 'auth': {'user': 'nested-user', 'pass': 'nested-pass'}},
            {'name': 'socks', 'type': 'socks5', 'dialer': "relay user1:p'ass1@10.0.0.9:1080"},
            {'name': 'tab', 'type': 'http', 'note': 'tab\there https://u2:tabpass2@h.example/x'},
        ],
        'auth-str': long_auth,
        'rule-providers': {'R': {'url': 'https://h.example/q?a=1&key=quoted-key-1: tail'}},
    }
    secrets = ["it's-a-secret", '123456', '987654', 'b3BlbnNzaC1rZXktdjEAAAAABG5vbmU', 'BEGIN OPENSSH',
               'nested-user', 'nested-pass', "p'ass1", 'tabpass2', 'quoted-key-1', 'word0secret', 'word29secret']
    result = diff_config(dump({'proxies': []}), dump(new))
    lines = result['raw']['lines']
    assert [unpack(line)[0] for line in lines] == raw_lines({'proxies': []}, new)
    text = '\n'.join(unpack(line)[0] for line in lines)
    hidden = '\n'.join(without_secrets({'lines': lines})['lines'])
    for value in secrets:
        assert value in text and value not in hidden, value
    # Hosts stay readable where a credential was found inside the string.
    assert '@10.0.0.9:1080' in hidden and 'relay ' in hidden and 'h.example/q?a=1&key=' in hidden
    assert "password: '已隐藏" not in hidden  # the quotes belong to the hidden value
    assert '+  password: 已隐藏' in hidden


def test_display_texts_escape_and_mark_credentials_inside_json():
    old = {'hosts': {}, 'odd': 'scalar'}
    new = {'hosts': {}, 'proxy-providers': {'P': {'url': 'https://h.example/s', 'header': {'Cookie': ['a"b\\c']}}},
           'odd': {1: 'x', None: 'y', 2.5: 'z'}}
    items = items_by_key(diff_config(dump(old), dump(new)))
    provider = items['proxy-providers.P']['new']
    assert json.loads(unpack(provider)[0]) == new['proxy-providers']['P']
    assert secrets_of(provider) == ['["a\\"b\\\\c"]']
    assert items['odd']['new'] == '{"1": "x", "null": "y", "2.5": "z"}'  # json.dumps would raise on mixed keys


def test_stored_tokens_become_hidden_redactions_inside_display_texts():
    def find(text):
        index = text.find('stored-token-1')
        return [(index, index + len('stored-token-1'))] if index >= 0 else []

    result = {'files': [{'path': 'p', 'name': 'feed stored-token-1'}],
              'config': {'items': [{'key': 'url', 'old': ['https://h/?token=', {'secret': 'stored-token-1'}], 'new': 'x'}],
                         'raw': {'lines': ['+a: stored-token-1 tail', '-b']}}}
    cleaned = without_stored(result, find)
    assert cleaned['files'][0]['name'] == ['feed ', {'secret': '[REDACTED]'}]
    assert cleaned['config']['items'][0]['old'] == ['https://h/?token=', {'secret': '[REDACTED]'}]
    assert cleaned['config']['raw']['lines'] == [['+a: ', {'secret': '[REDACTED]'}, ' tail'], '-b']
    assert 'stored-token-1' not in json.dumps(cleaned)
    assert without_stored(result, lambda text: []) == result


def test_secret_detection_keeps_domains_and_data_keys():
    assert push_diff.is_secret_key('obfs-password') and push_diff.is_secret_key('PASSWORD')
    assert push_diff.is_secret_key('private_key') and push_diff.is_secret_key('client-secret')
    assert not push_diff.is_secret_key('token.example.com')
    assert not push_diff.is_secret_key('public-key') and not push_diff.is_secret_key('server')
    old = {'hosts': {}, 'rule-providers': {}}
    new = {'hosts': {'token.example.com': '192.0.2.1'},
           'rule-providers': {'secret': {'type': 'http', 'behavior': 'domain', 'url': 'https://rules.example.com/a.list'}}}
    items = items_by_key(diff_config(dump(old), dump(new)))
    assert items['hosts.token.example.com']['new'] == '192.0.2.1'
    assert json.loads(items['rule-providers.secret']['new'])['url'] == 'https://rules.example.com/a.list'


def pieces(text):
    return [text[start:end] for start, end in secret_spans(text)]


def test_credential_spans_inside_text():
    assert secret_spans('https://dns.example.com/dns-query') == []
    assert pieces('socks5 user:pa55word@10.0.0.1:1080 then https://x.example.com/?key=abc&v=1') == ['user:pa55word', 'abc']
    assert secret_spans('alice:wonder@192.0.2.10:1080') == [(0, 12)]
    assert pieces('https://h.example/?Q2xhc2hTdWJzY3JpcHRpb25QYXRoVG9rZW4wMDg') == ['Q2xhc2hTdWJzY3JpcHRpb25QYXRoVG9rZW4wMDg']
    assert pieces('https://h.example/a?api_key=k%2B1&x=2#frag') == ['k%2B1']
    assert pieces('see http://[::1/broken') == ['http://[::1/broken']  # unparsable: the whole URL
    assert pieces('mail me: someone@example.com') == []


def test_formatting_only_changes_are_not_reported_as_items():
    old = b'a: 1\nb: [x, y]\n'
    new = b'b:\n  - x\n  - y\na: 1\n'
    result = diff_config(old, new)
    assert result['status'] == 'formatting' and result['items'] == []


def test_unparsable_yaml_falls_back_to_file_level():
    assert diff_config(b'a: [', b'a: 1\n')['status'] == 'unparsable'


def test_rule_entries_read_text_and_yaml_payloads():
    assert rule_entries(b'# comment\n+.a.example\n\nDOMAIN,b.example\n') == ['+.a.example', 'DOMAIN,b.example']
    assert rule_entries(b"payload:\n  - '+.a.example'\n  - \"b.example\"\n  - c.example # tail\n")[:2] == [
        '+.a.example', 'b.example']


def bundle(config_document, *, providers=None, rules=None, custom=None, service='mihomo', deployment='d1'):
    agent = {'id': 'diff-agent', 'profile_id': 'default', 'service_type': service}
    pinned = [{'path': path, 'content': content, 'role': 'rule'} for path, content in (rules or {}).items()]
    # Remote rule providers would be downloaded; these bundles pin their rule bytes instead.
    dropped = {'rule-providers'} | (set() if providers else {'proxy-providers'})
    config_document = {key: value for key, value in config_document.items() if key not in dropped}
    return build_deployment_bundle(
        agent, {'profile_id': 'default'}, yaml.safe_dump(config_document, allow_unicode=True, sort_keys=False),
        local_dns_watchdog=service == 'mihomo',
        provider_downloads=[{'name': name, 'local_path': f'providers/{name}.yaml', 'content': content}
                            for name, content in (providers or {}).items()],
        custom_files=[{'path': path, 'content': content} for path, content in (custom or {}).items()],
        pinned_files=pinned, deployment_id=deployment)


def provider(nodes):
    return yaml.safe_dump({'proxies': nodes}, allow_unicode=True)


def receipt_for(old):
    return {'manifest': old.manifest, 'bundle': old, 'comparison_config': None}


def agent_for(old, service='mihomo'):
    return {'id': 'diff-agent', 'service_type': service, 'applied_content': {'files': deployment_content_digests(old.manifest)}}


NODE = {'name': 'Node A', 'type': 'ss', 'server': 'a.example.com', 'port': 443, 'cipher': 'aes-128-gcm',
        'password': 'node-a-password'}


def test_files_are_listed_by_digest_with_role_summaries():
    old = bundle(base_config(),
                 providers={'Feed': provider([NODE, {**NODE, 'name': 'Node B'}, {**NODE, 'name': 'Node C'}])},
                 rules={'ruleset/Ads.list': '+.ads.example\n+.track.example\n', 'ruleset/Same.list': '+.same.example\n'},
                 custom={'extra.txt': 'abc'})
    new = bundle(changed_config(),
                 providers={'Feed': provider([{**NODE, 'password': 'rotated-password'}, {**NODE, 'name': 'Node B'},
                                              {**NODE, 'name': 'Node D'}, {**NODE, 'name': 'Node E'}]),
                            'Second': provider([NODE])},
                 rules={'ruleset/Ads.list': '+.ads.example\n+.new.example\n+.more.example\n',
                        'ruleset/Same.list': '+.same.example\n'},
                 deployment='d2')
    result = build_push_diff(agent_for(old), new, None, receipt=receipt_for(old))
    assert result['status'] == 'changed' and result['content'] == 'full'
    assert result['counts'] == {'added': 1, 'removed': 1, 'modified': 3, 'unchanged': 1}
    files = {item['path']: item for item in result['files']}
    assert files['config.yaml']['role'] == 'config' and files['config.yaml']['summary']['kind'] == 'config'
    assert files['config.yaml']['summary']['changes'] == result['config']['total'] > 0
    assert files['providers/Feed.yaml']['name'] == 'Feed'
    assert files['providers/Feed.yaml']['summary'] == {'kind': 'nodes', 'before': 3, 'after': 4, 'added': 2,
                                                       'removed': 1, 'modified': 1}
    assert files['providers/Second.yaml']['status'] == 'added'
    assert files['providers/Second.yaml']['summary'] == {'kind': 'nodes', 'before': None, 'after': 1, 'added': 1,
                                                         'removed': 0, 'modified': 0}
    assert files['ruleset/Ads.list']['summary'] == {'kind': 'rules', 'before': 2, 'after': 3, 'added': 2, 'removed': 1}
    assert files['extra.txt']['status'] == 'removed' and files['extra.txt']['summary'] == {
        'kind': 'size', 'before': 3, 'after': None}
    assert [item['path'] for item in result['unchanged']] == ['ruleset/Same.list']
    # Order: main configuration, subscriptions, rule sets, other files.
    assert [item['role'] for item in result['files']] == ['config', 'provider', 'provider', 'rule', 'custom']
    encoded = json.dumps(result, ensure_ascii=False)
    assert 'rotated-password' not in encoded and 'node-a-password' not in encoded


def test_status_follows_comparison_digest_when_live_main_file_differs():
    """After an automatic rule refresh the receipt keeps live bytes but compares the original digest."""
    old = bundle(base_config())
    new = bundle(base_config(), deployment='d2')
    live = bundle({**base_config(), 'log-level': 'warning'}, deployment='live')
    receipt = {'manifest': live.manifest, 'bundle': live,
               'comparison_config': next(item for item in old.manifest['files'] if item['role'] == 'config')}
    result = build_push_diff({'id': 'diff-agent', 'service_type': 'mihomo', 'applied_content': {}}, new, None,
                             receipt=receipt)
    assert result['counts']['modified'] == 0 and result['config'] is None
    changed = bundle(changed_config(), deployment='d3')
    result = build_push_diff({'id': 'diff-agent', 'service_type': 'mihomo', 'applied_content': {}}, changed, None,
                             receipt=receipt)
    assert items_by_key(result['config'])['log-level']['old'] == 'warning'  # what the device actually runs


def test_without_saved_bundle_only_digests_are_compared():
    old = bundle(base_config(), providers={'Feed': provider([NODE])}, service='mosdns')
    new = bundle(changed_config(), providers={'Feed': provider([NODE, {**NODE, 'name': 'Node B'}])},
                 rules={'rules/new.txt': 'example.com\n'}, service='mosdns', deployment='d2')
    result = build_push_diff(agent_for(old, 'mosdns'), new, None, receipt=None)
    assert result['content'] == 'digests' and result['content_reason'] == 'service_without_bundle'
    files = {item['path']: item for item in result['files']}
    assert 'summary' not in files['providers/Feed.yaml']  # previous bytes were not kept
    assert files['rules/new.txt']['summary'] == {'kind': 'rules', 'before': None, 'after': 1, 'added': 1, 'removed': 0}
    assert result['config'] == {'format': 'yaml', 'status': 'unavailable', 'reason': 'service_without_bundle',
                                'items': [], 'omitted': 0}
    mihomo = build_push_diff(agent_for(old, 'mihomo'), new, None, receipt=None)
    assert mihomo['content_reason'] == 'bundle_missing'
    legacy = build_push_diff({'id': 'x', 'service_type': 'mihomo', 'applied_content': {'fingerprint': 'v1:' + '0' * 64}},
                             new, None, receipt=None)
    assert legacy == {'status': 'changed', 'files': None, 'reason': 'baseline_files_missing'}


def test_large_rule_sets_are_counted_quickly_and_keep_the_response_small():
    lines = [f'+.domain-{index}.example.com' for index in range(60000)]
    old_rules = '\n'.join(lines) + '\n'
    new_rules = '\n'.join(lines[1500:] + [f'+.fresh-{index}.example.org' for index in range(2500)]) + '\n'
    yaml_old = 'payload:\n' + ''.join(f"  - '{line}'\n" for line in lines)
    yaml_new = 'payload:\n' + ''.join(f"  - '{line}'\n" for line in lines[:-700])
    old = bundle(base_config(), rules={'ruleset/Big.list': old_rules, 'ruleset/Big.yaml': yaml_old})
    new = bundle(base_config(), rules={'ruleset/Big.list': new_rules, 'ruleset/Big.yaml': yaml_new}, deployment='d2')
    started = time.perf_counter()
    result = build_push_diff(agent_for(old), new, None, receipt=receipt_for(old))
    elapsed = time.perf_counter() - started
    files = {item['path']: item['summary'] for item in result['files']}
    assert files['ruleset/Big.list'] == {'kind': 'rules', 'before': 60000, 'after': 61000, 'added': 2500, 'removed': 1500}
    assert files['ruleset/Big.yaml'] == {'kind': 'rules', 'before': 60000, 'after': 59300, 'added': 0, 'removed': 700}
    assert elapsed < 3, elapsed
    assert len(json.dumps(result, ensure_ascii=False)) < 4000


def test_large_inline_rules_in_the_main_file_stay_bounded():
    old = {'rules': [f'DOMAIN-SUFFIX,site-{index}.example.com,DIRECT' for index in range(30000)]}
    new = {'rules': old['rules'][1000:] + [f'DOMAIN-SUFFIX,new-{index}.example.com,Proxy' for index in range(3000)]}
    started = time.perf_counter()
    result = diff_config(dump(old), dump(new))
    elapsed = time.perf_counter() - started
    rules = items_by_key(result)['rules']['list']
    assert (rules['added'], rules['removed']) == (3000, 1000)
    assert elapsed < 6, elapsed
    assert len(json.dumps(result, ensure_ascii=False).encode()) < push_diff.MAX_RAW_BYTES + 32 * 1024


@pytest.mark.parametrize('value,shown', [('plain', 'plain'), (7, '7'), (1.5, '1.5'), (True, 'true'), (None, 'null'),
                                         ({'a': [1]}, '{"a": [1]}')])
def test_scalar_display(value, shown):
    result = diff_config(dump({'k': 'before'}), dump({'k': value}))
    assert result['items'][0]['new'] == shown


from backend.test_agent_content_sync import setup  # noqa: E402,F401  (checker fixture with a deferred executor)

BASE_URL = 'https://config.example.test'


def push_diff_of(setup):
    checker, _, _, agent, config, prepare, _ = setup
    return checker.get_push_diff(agent, config, BASE_URL, prepare)


def test_change_list_shares_the_status_worker_and_cache(setup, monkeypatch):
    checker, executor, now, agent, config, prepare, calls = setup
    built = []
    monkeypatch.setattr('backend.agents.push_diff.build_push_diff',
                        lambda item, candidate, repository: built.append(candidate) or {'status': 'changed', 'files': []})
    config['test_rule'] = 'DOMAIN,changed.example.test\n'
    for _ in range(5):
        assert push_diff_of(setup) == {'status': 'checking'}
    assert len(executor.jobs) == 1 and calls == []
    executor.run()
    result = push_diff_of(setup)
    assert result == {'status': 'changed', 'files': [], 'checked_at': result['checked_at']}
    # The list's status came from the same preparation, and polling reuses the list.
    assert checker.get_content_sync(agent, config, BASE_URL, prepare)['status'] == 'changed'
    assert push_diff_of(setup) == result
    assert len(calls) == 1 and len(built) == 1 and not executor.jobs
    # A later status-only refresh does not rebuild a list nobody asked for.
    checker.invalidate()
    assert checker.get_content_sync(agent, config, BASE_URL, prepare) == {'status': 'checking'}
    executor.run()
    assert len(built) == 1
    # Asking again prepares once more and lists the changes.
    assert push_diff_of(setup) == {'status': 'checking'}
    executor.run()
    assert push_diff_of(setup)['status'] == 'changed' and len(built) == 2


def test_synced_and_unavailable_answers_skip_the_change_list(setup, monkeypatch):
    checker, executor, _, agent, config, prepare, _ = setup
    monkeypatch.setattr('backend.agents.push_diff.build_push_diff', lambda *_: pytest.fail('nothing to list'))
    assert push_diff_of(setup) == {'status': 'checking'}
    executor.run()
    assert push_diff_of(setup)['status'] == 'synced'
    pending = {**agent, 'latest_deployment': {'status': 'uploading'}}
    assert checker.get_push_diff(pending, config, BASE_URL, prepare) == {'status': 'unknown', 'reason': 'deployment_pending'}
    fresh = {key: value for key, value in agent.items() if key not in ('applied_content', 'config_version')}
    assert checker.get_push_diff(fresh, config, BASE_URL, prepare) == {'status': 'never-pushed'}


def test_failed_listing_keeps_the_status_and_retries_soon(setup, monkeypatch):
    checker, executor, now, agent, config, prepare, _ = setup
    attempts = []

    def flaky(*_):
        attempts.append(1)
        raise OSError('receipt unreadable')

    monkeypatch.setattr('backend.agents.push_diff.build_push_diff', flaky)
    config['test_rule'] = 'DOMAIN,changed.example.test\n'
    push_diff_of(setup)
    executor.run()
    failed = push_diff_of(setup)
    assert (failed['status'], failed['files'], failed['reason']) == ('changed', None, 'diff_failed')
    assert checker.get_content_sync(agent, config, BASE_URL, prepare)['status'] == 'changed'
    now[0] += 31  # past the error interval, still within the status interval
    assert push_diff_of(setup) == {'status': 'checking'}
    executor.run()
    assert len(attempts) == 2


def test_only_a_few_change_lists_are_kept(setup, monkeypatch):
    checker, executor, _, agent, config, prepare, _ = setup
    monkeypatch.setattr('backend.agents.push_diff.build_push_diff', lambda *_: {'status': 'changed', 'files': []})
    config['test_rule'] = 'DOMAIN,changed.example.test\n'
    checker._max_details = 2
    for index in range(4):
        item = {**agent, 'id': f'agent-{index}'}
        checker.get_push_diff(item, config, BASE_URL, prepare)
        executor.run()
    assert len(checker._details) == 2
