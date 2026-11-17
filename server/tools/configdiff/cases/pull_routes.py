"""Named config.py live-pull forms, authorization priority and render failures."""
import copy
import json
import sqlite3
import uuid
from urllib.parse import urlencode
import yaml

from compare import database
from harness import Pair, json_body
from sources import Source
from subscriptions import Exchange
from cases.crosscutting import PULL_ROUTES, pull_path, pull_filename
from cases.generator_urls import output_urls
from cases.helpers import profile_policy
from cases.rule_library import DOMAIN
from cases.rule_io import write_cache
from cases.setup import Stable
from cases.subscriptions import SUB, put_file, require


def options(rule_url=None):
    profile = {'proxy_groups': [{'id': 'pull-group', 'name': '来源中文🧪', 'type': 'select', 'subscriptions': ['sub-a'], 'manual_nodes': ['DIRECT']}],
               'rule_configs': [{'id': 'pull-rule', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT', 'enabled': True}],
               'mosdns': {'direct_rulesets': ['pull-rule']}, 'oxidns': {'direct_rulesets': ['pull-rule']}}
    library = copy.deepcopy(DOMAIN)
    if rule_url:
        library.update(source_type='url', url=rule_url)
        library.pop('content', None)
    return {'shared': {'subscriptions': [{**SUB, 'url': ''}], 'rule_library': [library]},
            'profile': copy.deepcopy(profile), 'other_profile': copy.deepcopy(profile)}


class SourceStable(Stable):
    def __init__(self, pair, source):
        super().__init__(pair)
        self.exchange = Exchange(pair, source, expected=0)

    def before(self, index):
        super().before(index)
        self.exchange.before(index)

    def after(self, index, reply):
        super().after(index, reply)
        self.exchange.after(index, reply)

    def compare(self, policy):
        return super().compare(policy) + self.exchange.compare(policy)


def empty_origin(pair):
    # Empty values are guessed at process startup and cannot be saved by the
    # public form. This is an explicit persisted compatibility fixture, as in
    # G15: update the external SQLite boundary and its cache generation on both
    # sides, using one identical valid commit identity, never a Go cache API.
    commit = uuid.uuid4().hex
    for directory in pair.dirs:
        with sqlite3.connect(directory / 'configflow.sqlite3') as connection:
            stored = json.loads(connection.execute("SELECT value FROM fields WHERE scope='system' AND owner='' AND field='system_config'").fetchone()[0])
            stored['server_domain'] = ''
            connection.execute("UPDATE fields SET value=? WHERE scope='system' AND owner='' AND field='system_config'", (json.dumps(stored),))
            generation = json.loads(connection.execute("SELECT value FROM meta WHERE key='generation'").fetchone()[0])
            connection.execute("UPDATE meta SET value=? WHERE key='generation'", (json.dumps(generation + 1),))
            connection.execute("UPDATE meta SET value=? WHERE key='commit_id'", (json.dumps(commit),))


def install_cache(pair):
    put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': '2026-01-01T00:00:00Z',
        'nodes': [{'id': 'cached-a', 'name': '缓存中文🧪', 'type': 'socks5', 'server': '192.0.2.8', 'port': 1080}],
        'count': 1, 'metadata': {'url': ''}})


def forms(binary, recorder):
    with Source(b'+.example.com\n') as source, Pair(binary, recorder, options=options(source.url + '/rules')) as pair:
        pair.policy = profile_policy(pair)
        empty_origin(pair)
        install_cache(pair)
        write_cache(pair, pair.options['shared']['rule_library'][0], b'+.example.com\n')
        for route, rid in PULL_ROUTES:
            mihomo = '/mihomo' in route
            base_path = pull_path(route)
            vectors = [('S04' if '/oxidns' not in route else 'S05', 'forwarded',
                {'Host': 'request.example:18080', 'X-Forwarded-Proto': 'https', 'X-Forwarded-Host': 'forward.example:18443'},
                '', None, '' if mihomo else 'https://forward.example:18443')]
            if '/oxidns' not in route:
                vectors.append(('S05', 'query-base' if mihomo else 'request-host', {'Host': 'request.example:18080'},
                    '?base_url=https%3A%2F%2Fquery.example%3A18443%2Fbase%2520space' if mihomo else '', None,
                    'https://query.example:18443/base%20space' if mihomo else 'http://request.example:18080'))
            if mihomo:
                vectors.append(('S06', 'GET-JSON-body-base', {'Host': 'request.example:18080'}, '',
                    json_body({'base_url': 'https://body.example:18443/base%20space'}), 'https://body.example:18443/base%20space'))
            # A B91 has multiple subforms: this group covers raw forwarded
            # comma values and IPv6 Host. HTTP/1.0 with no Host is separate work.
            vectors.append(('B91', 'IPv6-Host', {'Host': '[::1]:18080'}, '', None, '' if mihomo else 'http://[::1]:18080'))
            for case, variant, headers, query, body, origin in vectors:
                def check(reply):
                    if '/mosdns' in route:
                        document = yaml.safe_load(reply.body)
                        sets = [item for item in document['plugins'] if item.get('type') == 'domain_set']
                        require(bool(sets) and all(item.get('args', {}).get('files') for item in sets)
                            and not output_urls(reply.body.decode()),
                            'MosDNS live YAML exposes local rule paths, not download URLs; Host selection is unobservable here and remains partial')
                        return
                    urls = output_urls(reply.body.decode())
                    require(bool(urls), 'live pull includes actual provider/rule delivery URLs, making origin precedence observable')
                    require(all(url.startswith(origin + '/api/') for url in urls),
                        'every generated API URL uses the known query/body/forwarded/request origin; Mihomo ignores Host headers')
                pair.request(rid + '-' + case, route, base_path + query, body=body,
                    headers={'Content-Type': 'application/json', **headers}, expect=200, writes=True,
                    filename=pull_filename(route), interaction=SourceStable(pair, source), check=check, variant='named/' + variant)
    with Pair(binary, recorder, options={'profile': {'name': '配置中文🧪'}}) as pair:
        pair.policy = profile_policy(pair)
        for route, rid in PULL_ROUTES:
            for case in ('B01', 'B02'):
                pair.request(rid + '-' + case, route, pull_path(route), headers={'Host': 'panel.example'}, expect=200,
                    writes=True, filename=pull_filename(route), interaction=Stable(pair), variant='named/Unicode-profile-empty-resources')


def guards(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        for route, rid in PULL_ROUTES:
            path = pull_path(route)
            pair.request(rid + '-F08', route, path + '?token=wrong-synthetic', auth='', expect=401,
                headers={'Host': 'panel.example'}, writes=True, interaction=Stable(pair), variant='named/live-render-auth')
            for identity, expected in [('missing-synthetic', 404), ('bad!id', 400)]:
                for selector in ('profile', 'profile_id', 'header'):
                    if '<profile_id>' in route:
                        target, headers = pull_path(route, identity), {'Host': 'panel.example'}
                        if selector != 'profile':
                            continue
                    else:
                        target = path + ('?' + urlencode({selector: identity}) if selector != 'header' else '')
                        headers = {'Host': 'panel.example', **({'X-ConfigFlow-Profile': identity} if selector == 'header' else {})}
                    pair.request(rid + '-F90', route, target, auth='', headers=headers, expect=expected,
                        writes=True, interaction=Stable(pair), variant='named/anonymous-' + selector + '/' + identity)


def failures(binary, recorder):
    profile = {'rule_configs': [{'id': 'missing-policy-rule', 'itemType': 'rule', 'rule_type': 'DOMAIN',
                'value': 'example.com', 'policy': 'synthetic-missing-policy', 'enabled': True}]}
    with Pair(binary, recorder, options={'profile': profile}) as pair:
        pair.policy = profile_policy(pair)
        for route, rid in PULL_ROUTES:
            if not any('/' + service in route for service in ('mihomo', 'surge', 'loon')):
                continue
            pair.request(rid + '-F09', route, pull_path(route), expect=400, writes=True,
                interaction=Stable(pair), variant='named/StrategyReferenceError',
                check=lambda r: require(r.json()['message'] == "规则 missing-policy-rule 引用了不存在或未启用的策略 'synthetic-missing-policy'，请修改策略引用",
                                         'actual enabled rule reaches the converter strategy-reference branch'))
    with Pair(binary, recorder, options={'profile': {'oxidns': {'config_mode': 'native', 'native_config': 'plugins: [\n'}}}) as pair:
        for route, rid in PULL_ROUTES:
            if '/oxidns' not in route:
                continue
            pair.request(rid + '-F09', route, pull_path(route), expect=400, writes=True,
                interaction=Stable(pair), variant='named/invalid-native-YAML')
    with Pair(binary, recorder, options={'profile': {'mihomo': {'custom_config': '[\n'}}}) as pair:
        for route, rid in PULL_ROUTES:
            if '/mihomo' not in route:
                continue
            pair.policy.internal_errors['body.message'] = ''
            pair.request(rid + '-F10', route, pull_path(route), expect=500, writes=True,
                interaction=Stable(pair), variant='named/unhandled-base-YAML-error')


GROUPS = {'config-pull-named-forms': forms, 'config-pull-named-guards': guards,
          'config-pull-named-failures': failures}
