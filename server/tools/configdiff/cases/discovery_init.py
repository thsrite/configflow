"""Default discovery ruleset creation, repeat allocation and ordering."""
from compare import database
from discovery import Allocations
from harness import Pair, json_body
from cases.config_backup import message
from cases.domain_discovery import GROUP, options
from cases.helpers import profile_policy
from cases.subscriptions import require


def matrix(binary, recorder):
    vectors = [('S01', ['direct'], 'direct'), ('S02', ['proxy'], 'proxy'), ('S03', ['direct', 'proxy'], 'both'),
               ('S03', [], 'empty-means-both'), ('S01', ['direct', 'direct'], 'duplicate-targets'),
               ('S04', ['direct'], 'repeat'), ('B01', ['direct', 'proxy'], 'name-collision'), ('B02', ['direct', 'proxy'], 'before-MATCH')]
    for case, targets, variant in vectors:
        opts = options()
        if variant == 'name-collision':
            opts['shared']['rule_library'][0]['name'] = '域名发现-直连'
            opts['shared']['rule_library'][1]['name'] = '域名发现-代理'
        with Pair(binary, recorder, options=opts) as pair:
            pair.policy = profile_policy(pair)
            previous = None
            for round_number in range(2 if variant == 'repeat' else 1):
                count = len(targets) if targets else 2
                observer = Allocations(pair, count)
                def check(reply):
                    observer.reference(reply)
                    value = reply.json()
                    selected = targets or ['direct', 'proxy']
                    for target in set(selected):
                        require(value[target]['active'] is True and all(not item['after_match'] for item in value[target]['references']),
                                'new references are active before the final MATCH')
                    if previous is not None:
                        require(value['direct']['id'] != previous, 'repeated initialization creates another resource instead of reusing the previous allocation')
                    if variant == 'name-collision':
                        require(value['direct']['name'] == '域名发现-直连-2' and value['proxy']['name'] == '域名发现-代理-2',
                                'name collisions retain all existing libraries and choose deterministic unused suffixes')
                replies = pair.request('R056-' + case, 'POST /api/domain-discovery/rulesets/init',
                    body=json_body({'targets': targets, 'proxy_policy': GROUP['name']}), expect=200, writes=True,
                    check=check, capture=observer.capture, variant=variant + '/' + str(round_number))
                previous = replies[0].json()['direct']['id']


def guards(binary, recorder):
    for case, targets, policy, expected, helper in [
        ('F08', 'direct', GROUP['name'], 'targets 只能包含 direct / proxy', 'H052'),
        ('F08', ['invalid'], GROUP['name'], 'targets 只能包含 direct / proxy', 'H052'),
        ('F09', ['proxy'], 'missing-policy', '请选择代理规则集使用的策略组', 'H053')]:
        with Pair(binary, recorder, options=options()) as pair:
            before = database(pair.dirs[0])
            def check(reply):
                message(expected)(reply)
                require(database(pair.dirs[0]) == before, 'bad initialization leaves shared resources, local references and revisions unchanged')
            for ident in ('R056-' + case, helper):
                pair.request(ident, 'POST /api/domain-discovery/rulesets/init', body=json_body({'targets': targets, 'proxy_policy': policy}),
                             expect=400, writes=True, check=check, variant=repr(targets))


GROUPS = {'discovery-init': matrix, 'discovery-init-guards': guards}
