"""Region-to-policy adoption with stable reuse and independently checked allocations."""
import copy

from compare import database
from composition import cache_name
from discovery import Allocations, allocation_ids
from harness import Pair, json_body
from region_fake import protocol
from cases.config_backup import message
from cases.domain_discovery import DIRECT, GROUP, options, stored
from cases.discovery_writes import history_times
from cases.helpers import profile_policy
from cases.rule_io import write_cache
from cases.subscriptions import require

REGIONAL = {**DIRECT, 'id': 'discovery-regional', 'name': '域名发现-代理🧪', 'content': '# regional preserved\n'}
REFERENCE = {'id': 'discovery-regional-ref', 'itemType': 'ruleset', 'library_rule_id': REGIONAL['id'], 'policy': GROUP['name'], 'enabled': True}


def fixtures(shape):
    opts = options()
    if shape != 'new':
        opts['shared']['rule_library'].append(copy.deepcopy(REGIONAL))
        opts['profile']['domain_discovery']['regional_rulesets'] = {GROUP['name']: REGIONAL['id']}
        if shape == 'reuse':
            opts['profile']['rule_configs'].insert(0, copy.deepcopy(REFERENCE))
        elif shape == 'disabled':
            opts['profile']['rule_configs'].insert(0, {**REFERENCE, 'enabled': False})
        elif shape == 'after-match':
            opts['profile']['rule_configs'].append(copy.deepcopy(REFERENCE))
    return opts


def writes(binary, recorder):
    vectors = [('R055-S01', 'reuse', 'service', service, protocol.SERVICES_BY_ID[service]['domains'])
               for service in ('openai', 'claude', 'netflix', 'youtube')]
    vectors += [('R055-S02', 'reuse', 'domain', 'EXAMPLE.COM.', ['example.com']),
                ('R055-B02', 'reuse', 'domain', 'xn--fiqs8s.example', ['xn--fiqs8s.example']),
                ('R055-S02', 'new', 'domain', 'example.com', ['example.com']),
                ('R055-B01', 'disabled', 'domain', 'example.com', ['example.com']),
                ('R055-B01', 'after-match', 'domain', 'example.com', ['example.com'])]
    for case, shape, kind, value, expected in vectors:
        with Pair(binary, recorder, options=fixtures(shape)) as pair:
            pair.policy = profile_policy(pair)
            initial = database(pair.dirs[0])
            other = {k: v for k, v in initial['fields'].items() if k.startswith('profile/other/')}
            for library in initial['fields']['shared//rule_library']['value']:
                if library.get('source_type') == 'content':
                    write_cache(pair, library, library.get('content', '').encode())
            observer = Allocations(pair, 0)
            def check(reply):
                added = reply.json()['added']
                require({item['value'] for item in added} == set(expected) and
                        all(item['target'] == 'policy:' + GROUP['name'] for item in added), 'exact requested service/domain values adopted into Chinese policy')
                new = observer.added(0)
                require([len(part) for part in new] == ([1, 1] if shape == 'new' else [0, 1] if shape == 'disabled' else [0, 0]),
                        'fresh creates library+reference; disabled adds reference; active/after-match reuse allocation counts follow Python')
                allocation_ids(*new)
                libs = database(pair.dirs[0])['fields']['shared//rule_library']['value']
                selected_id = stored(pair)['regional_rulesets'][GROUP['name']]
                selected = next(lib for lib in libs if lib['id'] == selected_id)
                require((pair.dirs[0] / cache_name(selected)).read_bytes() == selected['content'].encode(), 'regional cache equals exact committed source bytes')
                history = stored(pair)['history']
                require({item['value'] for item in history} == set(expected) and all(item['source'] == 'region' for item in history), 'region source and exact target values persisted in history')
                require({k: v for k, v in database(pair.dirs[0])['fields'].items() if k.startswith('profile/other/')} == other,
                        'region adoption leaves other profile rows and revisions unchanged')
                if shape == 'new':
                    rules = database(pair.dirs[0])['fields']['profile/default/rule_configs']['value']
                    pos = next(i for i, rule in enumerate(rules) if rule.get('library_rule_id') == selected_id)
                    require(pos < next(i for i, rule in enumerate(rules) if rule['id'] == 'discovery-proxy-ref'), 'new regional reference precedes proxy catch-all')
                history_times(pair)
            pair.request(case, 'POST /api/domain-discovery/region/route', body=json_body({'kind': kind, 'value': value, 'policy': GROUP['name']}),
                         expect=200, writes=True, check=check, capture=observer.capture, variant=shape + '/' + kind + '/' + value)
            if shape == 'reuse':
                snapshot = database(pair.dirs[0])
                def repeated(reply):
                    actual = database(pair.dirs[0])
                    fields = lambda db: {k: v for k, v in db['fields'].items() if k != 'shared//_revision'}
                    require(reply.json()['added'] == [] and len(reply.json()['skipped']) == len(expected)
                            and fields(actual) == fields(snapshot) and actual['sections'] == snapshot['sections']
                            and actual['meta']['generation'] == snapshot['meta']['generation'] + 1
                            and actual['fields']['shared//_revision']['value'] == snapshot['fields']['shared//_revision']['value'] + 1,
                            'repeat keeps content/references/history but Python shared transaction still advances revision/generation once')
                pair.request('R055-B01', 'POST /api/domain-discovery/region/route', body=json_body({'kind': kind, 'value': value, 'policy': GROUP['name']}),
                             expect=200, writes=True, check=repeated,
                             variant='repeat/' + kind + '/' + value)


def guards(binary, recorder):
    for case, helper, body, text in [
            ('F08', 'H081', {}, 'kind 只能是 service 或 domain'),
            ('F08', 'H081', {'kind': 'other'}, 'kind 只能是 service 或 domain'),
            ('F09', 'H079', {'kind': 'service', 'value': 'unknown'}, '未知的服务'),
            ('F10', 'H080', {'kind': 'domain', 'value': '192.0.2.1'}, '需要有效域名'),
            ('F10', 'H080', {'kind': 'domain', 'value': '中国.example'}, '需要有效域名'),
            ('F11', 'H078', {'kind': 'domain', 'value': 'example.com', 'policy': 'missing'}, '请选择当前配置中的策略组')]:
        with Pair(binary, recorder, options=options()) as pair:
            for ident in ('R055-' + case, helper):
                before = database(pair.dirs[0])
                def check(reply):
                    message(text)(reply)
                    require(database(pair.dirs[0]) == before, 'invalid region routing causes no partial library/profile commit')
                pair.request(ident, 'POST /api/domain-discovery/region/route', body=json_body(body), expect=400, writes=True, check=check, variant=repr(body))


GROUPS = {'discovery-region-route': writes, 'discovery-region-route-guards': guards}
