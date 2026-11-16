"""Anonymous local-rule delivery, matching and duplicate scan observations."""
import copy
from urllib.parse import quote

from compare import differences
from composition import cache_name
from harness import Pair, json_body
from sources import Source
from subscriptions import Exchange, capture_notice
from task_lifecycle import configure
from cases.rule_library import DOMAIN
from cases.rules import RULE, options
from cases.subscriptions import require, put_file


def check_elapsed(pair, reply):
    value = reply.json().get('elapsed_time')
    require(not differences(value, value, 'body.elapsed_time', pair.policy), 'elapsed milliseconds are finite, bounded and rounded to two decimals')


def write_cache(pair, declaration, content):
    for directory in pair.dirs:
        path = directory / cache_name(declaration)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(content)


def local_content(binary, recorder):
    for rid, prefix in [(192, '/api/rules/local/'), (153, '/api/profiles/default/rules/local/')]:
        route = 'GET ' + ('/api/rules/local/<name>' if rid == 192 else '/api/profiles/<profile_id>/rules/local/<name>')
        with Pair(binary, recorder, options=options()) as pair:
            path = prefix + quote(DOMAIN['name'], safe='')
            for case in ('S02', 'S01', 'B01'):
                pair.request(f'R{rid:03}-' + case, route, path, auth='', expect=200, writes=True,
                             check=lambda r: require(r.body == DOMAIN['content'].encode() and (pair.dirs[0] / cache_name(DOMAIN)).read_bytes() == r.body,
                                                     'handwritten source creates then serves its exact raw cached bytes'))
            for directory in pair.dirs:
                old = directory / 'shared/rules/legacy.list'
                old.parent.mkdir(parents=True, exist_ok=True)
                old.write_bytes(b'# old source\nlegacy.example\n')
            pair.request(f'R{rid:03}-S05', route, prefix + 'legacy', auth='', expect=200, writes=True,
                         check=lambda r: require(r.body == b'# old source\nlegacy.example\n', 'unambiguous legacy cache is served even without catalog entry'))
            pair.request(f'R{rid:03}-F01', route, prefix + 'missing', auth='', expect=404, writes=True)
            pair.request(f'R{rid:03}-B02', route, prefix + quote('..\\outside', safe=''), auth='', expect=404, writes=True)
        for case, original, expected, behavior in [('S06', b'example.com\n192.0.2.1\n', b'DOMAIN,example.com', 'classical'),
                                                    ('B03', b'\xff\n', b'\xff\n', 'classical'), ('B04', b'', b'', 'domain')]:
            declaration = {**DOMAIN, 'content': '' if case == 'B04' else DOMAIN['content'], 'behavior': behavior}
            with Pair(binary, recorder, options={'shared': {'rule_library': [declaration]}}) as pair:
                write_cache(pair, declaration, original)
                def check(reply):
                    require(expected in reply.body if case == 'S06' else reply.body == expected, 'target normalization or invalid UTF-8 passthrough matches intended branch')
                    require((pair.dirs[0] / cache_name(declaration)).read_bytes() == original, 'delivery transformation never overwrites raw cache')
                pair.request(f'R{rid:03}-' + case, route, prefix + quote(DOMAIN['name'], safe='') + '?target=mihomo',
                             auth='', expect=200, writes=True, check=check)
        with Pair(binary, recorder, options=options()) as pair:
            for directory in pair.dirs:
                (directory / cache_name(DOMAIN)).mkdir(parents=True)
            pair.request(f'R{rid:03}-F04', route, prefix + quote(DOMAIN['name'], safe=''), auth='', expect=500, writes=True)


def local_remote(binary, recorder):
    for rid, prefix in [(192, '/api/rules/local/'), (153, '/api/profiles/default/rules/local/')]:
        route = 'GET ' + ('/api/rules/local/<name>' if rid == 192 else '/api/profiles/<profile_id>/rules/local/<name>')
        for case, scenario in [('S03', 'cached'), ('S04', 'stale'), ('S03', 'first-download'), ('F02', 'invalid'), ('F03', 'http-failure')]:
            with Source(b'+.remote.example\n') as source:
                declaration = {**DOMAIN, 'id': 'lib-remote', 'name': '远端🧪', 'source_type': 'url', 'url': source.url + '/rules'}
                declaration.pop('content')
                source.status = 403 if scenario in ('stale', 'http-failure') else 200
                if scenario == 'invalid':
                    source.body = b'not a domain\n'
                with Pair(binary, recorder, options={'shared': {'rule_library': [declaration]}}) as pair:
                    key = 'rule_source:lib-remote'
                    if scenario in ('cached', 'stale'):
                        write_cache(pair, declaration, b'+.cached.example\n')
                    if scenario == 'stale':
                        put_file(pair, 'task-status.json', {key: {'last_status': 'failed', 'last_error': '上游返回 HTTP 403', 'run_count': 7}})
                    else:
                        configure(pair, key)
                    def check(reply):
                        if scenario in ('cached', 'stale'):
                            require(reply.body == b'+.cached.example\n', 'valid cache is served without downloading again')
                        if scenario == 'stale':
                            require(reply.headers.get('x-configflow-cache') == 'stale' and '110 ConfigFlow' in reply.headers.get('warning', ''), 'previous failed task marks cache stale')
                    pair.request(f'R{rid:03}-' + case, route, prefix + quote(declaration['name'], safe=''), auth='',
                                 expect=422 if scenario == 'invalid' else 502 if scenario == 'http-failure' else 200, writes=True,
                                 interaction=Exchange(pair, source, expected=0 if scenario in ('cached', 'stale') else 1), check=check,
                                 capture=capture_notice(pair, key), variant=scenario)


def matching(binary, recorder):
    opts = options()
    opts['profile']['rule_configs'] += [{**RULE, 'id': 'ipv6', 'rule_type': 'IP-CIDR6', 'value': '2001:db8::/32'},
                                      {**RULE, 'id': 'unicode', 'value': '中文.example'}]
    with Pair(binary, recorder, options=opts) as pair:
        pair.policy.elapsed_ms.add('body.elapsed_time')
        for case, query, ident in [('S01', 'example.com', 'r-a'), ('S02', '192.0.2.5', 'r-ip'),
                                    ('S03', '2001:db8::1', 'ipv6'), ('S04', 'unmatched.invalid', None),
                                    ('B01', 'example.com.', 'r-a')]:
            def check(reply):
                check_elapsed(pair, reply)
                require(reply.json()['matched'] is (ident is not None) and reply.json().get('rule_id') == ident, 'matcher reports exact intended first rule')
            pair.request('R193-' + case, 'POST /api/rules/match-test', body=json_body({'query': query}), expect=200, writes=True,
                         check=check)
        pair.request('R193-B02', 'POST /api/rules/match-test', body=json_body({'query': '中文.example'}), expect=400, writes=True,
                     variant='raw-unicode-rejected')
        pair.request('R193-B02', 'POST /api/rules/match-test', body=json_body({'query': '中文.example'.encode('idna').decode()}),
                     expect=200, writes=True, variant='ascii-idna-input',
                     check=lambda r: require(r.json()['matched'] is False, 'ASCII query is accepted without silently converting stored Unicode rule'))
        for case, data, status in [('F08', {}, 400), ('F09', {'query': '   '}, 400), ('F10', {'query': 'bad domain'}, 400),
                                   ('F12', [1], 500), ('F12', {'query': 1}, 500)]:
            pair.request('R193-' + case, 'POST /api/rules/match-test', body=json_body(data), expect=status, writes=True,
                         variant=repr(data) if case == 'F12' else '')
    opts = options()
    opts['profile']['rule_configs'] = [{**RULE, 'enabled': False}]
    with Pair(binary, recorder, options=opts) as pair:
        pair.policy.elapsed_ms.add('body.elapsed_time')
        pair.request('R193-B03', 'POST /api/rules/match-test', body=json_body({'query': 'example.com'}), expect=200, writes=True,
                     check=lambda r: require(r.json()['matched'] is False, 'disabled ordinary rule is skipped'))


def duplicates(binary, recorder):
    for case in ('S01', 'S02', 'S03', 'B01', 'B02', 'B03'):
        opts = options()
        if case == 'S01':
            opts['profile']['rule_configs'] = [copy.deepcopy(RULE)]
        elif case == 'S02':
            opts['profile']['rule_configs'] = [copy.deepcopy(opts['profile']['rule_configs'][2])]
            opts['shared']['rule_library'][0].update(behavior='classical', content='DOMAIN,inside.example\nDOMAIN,inside.example\n')
        elif case == 'S03':
            opts['profile']['rule_configs'][2]['policy'] = 'REJECT'
            opts['shared']['rule_library'][0].update(behavior='classical', content='DOMAIN,example.com\n')
        elif case in ('B01', 'B02'):
            opts['profile']['rule_configs'] = [{**RULE, 'id': 'case-upper', 'rule_type': 'REGEX' if case == 'B01' else 'DOMAIN', 'value': 'EXAMPLE.COM'},
                                              {**RULE, 'id': 'case-lower', 'rule_type': 'REGEX' if case == 'B01' else 'DOMAIN', 'value': 'example.com'}]
        else:
            opts['profile']['rule_configs'] = [{**RULE, 'id': 'tie-' + str(i), 'value': ('a.example' if i % 2 == 0 else 'b.example')} for i in range(4)]
        with Pair(binary, recorder, options=opts) as pair:
            pair.policy.elapsed_ms.add('body.elapsed_time')
            def check(reply):
                check_elapsed(pair, reply)
                rows = reply.json()['duplicates']
                require(len(rows) == (0 if case in ('S01', 'B01') else 2 if case == 'B03' else 1), 'duplicate group count demonstrates chosen normalization/tie branch')
                if case == 'S03':
                    require(rows[0]['policy_conflict'] is True, 'cross-source duplicate preserves conflicting policies')
                if case == 'B03':
                    require([row['value'] for row in rows] == ['a.example', 'b.example'], 'tie order follows earliest configured priority')
            pair.request('R190-' + case, 'POST /api/rules/find-duplicates', body='{}', expect=200, writes=True, check=check)


def invalid_remote(binary, recorder):
    for scenario in ('invalid', 'failure'):
        with Source(b'not a domain\n') as source:
            if scenario == 'failure':
                source.status = 403
            opts = options()
            opts['shared']['rule_library'][0].update(source_type='url', url=source.url + '/rules')
            opts['shared']['rule_library'][0].pop('content')
            opts['profile']['rule_configs'] = [copy.deepcopy(opts['profile']['rule_configs'][2])]
            with Pair(binary, recorder, options=opts) as pair:
                pair.policy.elapsed_ms.add('body.elapsed_time')
                if scenario == 'invalid':
                    pair.request('R193-F11', 'POST /api/rules/match-test', body=json_body({'query': 'example.com'}), expect=422,
                                 writes=True, interaction=Exchange(pair, source))
                pair.request('R190-F08' if scenario == 'invalid' else 'R190-F09', 'POST /api/rules/find-duplicates', body='{}',
                             expect=500 if scenario == 'invalid' else 200, writes=True, interaction=Exchange(pair, source),
                             check=(lambda r: require(r.json()['stats']['failed_rulesets'] == ['域名🧪'], 'unavailable ruleset is listed rather than failing whole scan')) if scenario == 'failure' else None)


GROUPS = {'rule-local-content': local_content, 'rule-local-remote': local_remote, 'rule-match': matching,
          'rule-duplicates': duplicates, 'rule-index-remote-errors': invalid_remote}
