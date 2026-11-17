"""Manual enrolment followed by real detached HTTP-triggered rule refreshes."""
import base64
import hashlib

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
