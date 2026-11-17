"""Manual enrolment followed by real detached HTTP-triggered rule refreshes."""
import base64
import hashlib
import ipaddress

from addresses import network
from cases import Case, Ref
import releases


def content(label):
    return 'IP-CIDR,' + network('sync-' + label) + '\nDOMAIN-SUFFIX,' + label + '.example.com\n'


def options():
    value = releases.options()
    value['alpha']['routers']['items'][0].update(auto_rule_sync=True, ip_rule_sets=['remote-rule'])
    value['alpha']['rule_configs'] = [{'id': 'remote-rule', 'name': '合成同步规则🧪', 'itemType': 'ruleset',
                                      'library_rule_id': 'remote-library', 'enabled': True, 'policy': 'PROXY'}]
    value['shared'] = {'rule_library': [{'id': 'remote-library', 'name': 'SyntheticSync', 'source_type': 'url',
        'url': 'http://10.44.0.1:18084/rules', 'behavior': 'classical', 'enabled': True, 'interval': 0}]}
    # Publicly supported unambiguous legacy cache: first publication is offline.
    value['files'] = {'shared/rules/SyntheticSync.list': base64.b64encode(content('old').encode()).decode()}
    return value


def document(text):
    return {'action': 'seed', 'documents': {'/rules': {'status': 200, 'headers': {'Content-Type': 'text/plain'},
                                                    'body': base64.b64encode(text.encode()).decode()}}}


def manual_refresh():
    yield Case('R012/sync-enrol-preview', 'R012', 'POST', '/api/routers/r-ros/plan', 200,
               ('R012-S01',), unchanged=False, effect='device-read',
               checks={'plan/blockers': [], 'plan/empty': False}, formats={'plan/fingerprint': r'[0-9a-f]{16}'})
    yield Case('R011/sync-enrol-publish', 'R011', 'POST', '/api/routers/r-ros/apply', 200,
               ('R011-S01', 'T14'), document={'fingerprint': Ref('R012/sync-enrol-preview', '/body/plan/fingerprint')},
               requires=('R012',), unchanged=False, effect='profile-write-device', checks={'success': True},
               after_poll=({'path': '/api/routers/r-ros/rule-sync', 'expect': {'sync/status': 'synced', 'sync/ready': True}},))
    yield Case('R022/enrolled', 'R022', 'GET', '/api/routers/r-ros/rule-sync', 200,
               ('R022-S02',), checks={'sync/status': 'synced', 'sync/ready': True,
                    'sync/rules/0/ip_count': 1, 'sync/rules/0/domain_count': 1,
                    'sync/rules/0/version': hashlib.sha256(content('old').encode()).hexdigest()[:12],
                    'sync/rules/0/synced_version': hashlib.sha256(content('old').encode()).hexdigest()[:12]})
    for label, text in [('new', content('new')), ('unchanged', content('new')),
                        ('domain-only-edit', content('new').replace('new.example.com', 'changed.example.com'))]:
        yield Case('R023/refresh-' + label, 'R023', 'POST', '/api/routers/r-ros/rule-sync', 202,
                   ('R023-S01', 'T14'), prepare=(document(text),), unchanged=False, effect='device-only',
                   checks={'success': True, 'sync/ready': True},
                   after_poll=({'path': '/api/routers/r-ros/rule-sync', 'expect': {'sync/status': 'synced'}},),
                   expected_requests=({'match': {'peer': 'rules', 'method': 'GET', 'path': '/rules'}, 'count': 1},),
                   file_contents={r'shared/rules/by-source/[0-9a-f]{64}\.list': text})
        yield Case('R022/refreshed-' + label, 'R022', 'GET', '/api/routers/r-ros/rule-sync', 200,
                   ('R022-S02',), checks={'sync/status': 'synced', 'sync/ready': True,
                    'sync/added': 1 if label == 'new' else 0, 'sync/removed': 1 if label == 'new' else 0,
                    'sync/rules/0/version': hashlib.sha256(text.encode()).hexdigest()[:12],
                    'sync/rules/0/synced_version': hashlib.sha256(text.encode()).hexdigest()[:12]},
                   fake_lengths={'ros/~1ip~1route': 2},
                   fake_checks={'ros/~1ip~1route/1/dst-address': network('sync-new')})


def _enrol(label):
    preview = 'R012/enrol-' + label
    return [Case(preview, 'R012', 'POST', '/api/routers/r-ros/plan', 200, ('T14',),
                 unchanged=False, effect='device-read', checks={'plan/blockers': []}),
            Case('R011/enrol-' + label, 'R011', 'POST', '/api/routers/r-ros/apply', 200,
                 ('R023-B05',), document={'fingerprint': Ref(preview, '/body/plan/fingerprint')}, requires=('R012',),
                 unchanged=False, effect='profile-write-device', checks={'success': True},
                 after_poll=({'path': '/api/routers/r-ros/rule-sync', 'expect': {'sync/status': 'synced'}},))]


def download_failures():
    # Each paused job is recovered by a fresh public manual publish; no private
    # receipt constructor or direct sync() invocation creates the baseline.
    many4 = ipaddress.ip_network(network('sync-many4', prefix=16))
    definitions = [
        ('http-503', {'status': 503, 'body': b'synthetic upstream refusal'}, 'failed', r'远程规则下载失败：.+'),
        ('invalid-utf8', {'status': 200, 'body': b'\xff\xfe'}, 'failed', r'远程规则下载失败：.+'),
        ('html', {'status': 200, 'body': b'<html>synthetic login page</html>'}, 'failed', r'.+'),
        ('json', {'status': 200, 'body': b'["example.com"]'}, 'failed', r'.+'),
        ('yaml-broken', {'status': 200, 'body': b'payload: [unclosed'}, 'failed', r'.+'),
        ('comments-only', {'status': 200, 'body': b'# synthetic comment only\n'}, 'paused', r'规则集「SyntheticSync」：规则不包含任何有效条目'),
        ('domain-only', {'status': 200, 'body': b'DOMAIN,only.example.com\n'}, 'paused',
         r'规则集「SyntheticSync」的有效 IPv4 从 1 条变为 0，保留 ROS 原状态，请人工确认'),
        ('over-4mib', {'status': 200, 'body': b'#' * (4 * 1024 * 1024 + 1)}, 'paused', r'远程规则下载超过大小或时间限制'),
        ('over-100000-lines', {'status': 200, 'body': b'DOMAIN,a.example\n' * 100001}, 'paused',
         r'规则集「SyntheticSync」：规则条目超过 100000 条，停止自动同步'),
        ('over-20000-v4', {'status': 200, 'body': ('\n'.join('IP-CIDR,' + str(many4.network_address + index) + '/32'
            for index in range(1, 20002)) + '\n').encode()}, 'paused', r'规则集「SyntheticSync」：有效 IPv4 超过 20000 条，请使用手动发布')]
    for label, entry, terminal, message in definitions:
        yield from _enrol(label)
        seed = {'action': 'seed', 'documents': {'/rules': {'status': entry['status'],
                    'headers': {'Content-Type': 'text/plain'}, 'body': base64.b64encode(entry['body']).decode()}}}
        yield Case('R023/fetch-failure-' + label, 'R023', 'POST', '/api/routers/r-ros/rule-sync', 202,
                   ('R023-F16', 'T26') if terminal == 'failed' else ('T26',),
                   prepare=(seed,), unchanged=False, effect='device-read', checks={'success': True},
                   after_poll=({'path': '/api/routers/r-ros/rule-sync', 'expect': {'sync/status': terminal}},),
                   fake_lengths={'ros/~1ip~1route': 2},
                   file_contents={r'shared/rules/SyntheticSync\.list': content('old'),
                                  **({r'shared/rules/by-source/[0-9a-f]{64}\.list': content('old')}
                                     if label != 'http-503' else {})})
        yield Case('R022/fetch-failure-' + label, 'R022', 'GET', '/api/routers/r-ros/rule-sync', 200,
                   ('R022-S03',), checks={'sync/status': terminal, 'sync/ready': True}, formats={'sync/message': message})
        if label == 'http-503':
            yield Case('R023/retry-failed-download', 'R023', 'POST', '/api/routers/r-ros/rule-sync', 202,
                       ('R023-S02',), prepare=(document(content('old')),), unchanged=False, effect='device-read',
                       checks={'success': True},
                       after_poll=({'path': '/api/routers/r-ros/rule-sync', 'expect': {'sync/status': 'synced'}},))


def proxy_options():
    value = options()
    value['system']['system_config'] = {'rule_fetch_proxy':
        'http://synthetic-proxy:synthetic-proxy-password@10.44.0.1:18085'}
    return value


def proxy_refresh():
    yield from _enrol('proxy')
    authorization = 'Basic ' + base64.b64encode(b'synthetic-proxy:synthetic-proxy-password').decode()
    url = 'http://10.44.0.1:18084/rules'
    yield Case('R023/explicit-proxy', 'R023', 'POST', '/api/routers/r-ros/rule-sync', 202, ('T26',),
               prepare=({'action': 'seed', 'documents': {url: {'status': 200,
                         'headers': {'Content-Type': 'text/plain'},
                         'expected_headers': {'proxy-authorization': authorization},
                         'body': base64.b64encode(content('old').encode()).decode()}}},),
               unchanged=False, effect='device-read', checks={'success': True},
               after_poll=({'path': '/api/routers/r-ros/rule-sync', 'expect': {'sync/status': 'synced'}},),
               expected_requests=({'match': {'peer': 'rules'}, 'count': 0},
                                  {'match': {'peer': 'proxy', 'method': 'GET', 'path': url,
                                             '/headers/proxy-authorization': authorization}, 'count': 1}),
               file_contents={r'shared/rules/by-source/[0-9a-f]{64}\.list': content('old')})
