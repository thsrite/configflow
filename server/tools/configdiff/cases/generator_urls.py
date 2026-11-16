"""G15: output URL origin, profile paths and capability-token selection."""
import copy
import re
import json
import sqlite3
import uuid
from urllib.parse import parse_qs, urlsplit

from harness import Pair, json_body
from seed import PULL
from sources import Source
from subscriptions import Exchange
from cases.rule_io import write_cache
from cases.generator_shapes import project
from cases.rule_library import DOMAIN
from cases.subscriptions import SUB, put_file, require


def output_urls(text):
    return [value for value in re.findall(r'https?://[^\s,\x27\x22<>]+|/api/(?:profiles|config)/[^\s,\x27\x22<>]+', text)
            if '/api/' in value]


def matrix(binary, recorder):
    with Source(b'+.example.com\n') as source:
        for stored in (False, True):
            for capability in (False, True):
                profile = {'proxy_groups': [{'id': 'url-group', 'name': '来源组🧪', 'type': 'select', 'subscriptions': ['sub-a'], 'manual_nodes': ['DIRECT']}],
                           'rule_configs': [{'id': 'url-rule', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT', 'enabled': True}],
                           'mosdns': {'direct_rulesets': ['url-rule']}, 'oxidns': {'direct_rulesets': ['url-rule']}}
                opts = {'shared': {'subscriptions': [{**SUB, 'url': ''}], 'rule_library': [DOMAIN]},
                        'profile': copy.deepcopy(profile), 'other_profile': copy.deepcopy(profile),
                        'system_config': {'server_domain': 'http://stored.example:18080/panel' if stored else ''},
                        'config_token': PULL if capability else ''}
                # A valid cached legacy no-URL source isolates address generation
                # from source refresh. Provider transport has dedicated G13/T06
                # cases; this matrix sends no traffic to generated public hosts.
                with Pair(binary, recorder, options=opts) as pair:
                    fixture_commit = uuid.uuid4().hex
                    if not stored:
                        # Native startup guesses a LAN origin for an empty value.
                        # Set the persisted empty-field fixture after startup so
                        # these requests exercise the converter fallback itself.
                        for directory in pair.dirs:
                            with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                                row = db.execute("SELECT value FROM fields WHERE scope='system' AND owner='' AND field='system_config'").fetchone()
                                value = json.loads(row[0])
                                value['server_domain'] = ''
                                db.execute("UPDATE fields SET value=? WHERE scope='system' AND owner='' AND field='system_config'", (json.dumps(value),))
                                generation = json.loads(db.execute("SELECT value FROM meta WHERE key='generation'").fetchone()[0])
                                db.execute("UPDATE meta SET value=? WHERE key='generation'", (json.dumps(generation + 1),))
                                db.execute("UPDATE meta SET value=? WHERE key='commit_id'", (json.dumps(fixture_commit),))
                    write_cache(pair, opts['shared']['rule_library'][0], b'+.example.com\n')
                    put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': '2026-01-01T00:00:00Z',
                        'nodes': [{'id': 'cached-a', 'name': '缓存节点🧪', 'type': 'socks5', 'server': '192.0.2.8', 'port': 1080}],
                        'count': 1, 'metadata': {'url': ''}})
                    for explicit in (False, True):
                        for forwarded in (False, True):
                            for scoped in (False, True):
                                for service in ('mihomo', 'surge', 'loon'):
                                    route = 'POST /api/' + ('profiles/<profile_id>/' if scoped else '') + 'generate/' + service + '/preview'
                                    path = route.split(' ', 1)[1].replace('<profile_id>', 'other')
                                    body = {'base_url': 'https://body.example:18443/base%20space'} if explicit else {}
                                    headers = {'Host': 'request.example:18080', 'X-ConfigFlow-Profile': 'default'}
                                    if forwarded:
                                        headers.update({'X-Forwarded-Proto': 'https', 'X-Forwarded-Host': 'forward.example:18443'})
                                    profile_id = 'other' if scoped else 'default'
                                    def check(reply):
                                        urls = output_urls(reply.json()['content'])
                                        require(bool(urls), 'generated output really contains API source addresses')
                                        require(any('/subscriptions/sub-a/proxies' in url for url in urls), 'client output includes actual subscription provider URL')
                                        require(any('/rules/local/' in url or '/rule-library/content/' in url for url in urls), 'client output includes actual local rule delivery URL')
                                        for url in urls:
                                            parsed = urlsplit(url)
                                            if '/api/config/' in parsed.path:
                                                require('/api/config/' + profile_id + '/' in parsed.path, 'managed client config URL uses selected profile')
                                            else:
                                                require('/api/profiles/' + profile_id + '/' in parsed.path, 'source URL uses selected profile, including scoped URL priority over header')
                                            if stored:
                                                require(parsed.netloc == 'stored.example:18080', 'stored origin takes precedence over request/base URL')
                                            elif explicit:
                                                require(parsed.netloc == 'body.example:18443', 'explicit base origin is preserved when no stored origin: ' + repr((service, url)))
                                            query = parse_qs(parsed.query)
                                            if '/subscriptions/' in parsed.path or '/api/config/' in parsed.path:
                                                require(query.get('token', []) == ([PULL] if capability else []), 'client delivery URL embeds configured pull token only when present')
                                            elif '/rules/local/' in parsed.path or '/rule-library/content/' in parsed.path:
                                                require('token' not in query, 'anonymous local rule URL contains neither capability nor administrator JWT')
                                    pair.request('G15', route, path, body=json_body(body), headers=headers, expect=200, writes=True,
                                                 projection=project(service), check=check, interaction=Exchange(pair, source, expected=0),
                                                 variant=f'stored-{stored}/token-{capability}/base-{explicit}/forward-{forwarded}/{profile_id}/{service}')

GROUPS = {'generator-url-capabilities': matrix}
