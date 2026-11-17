"""Actual downloaded provider shape failures, without falling back to good nodes."""
import hashlib

from compare import database, files, timestamp
from harness import Pair, json_body
from sources import Source
from subscriptions import Exchange
from cases.subscription_providers import ROUTES
from cases.subscriptions import SUB, CACHE_NODES, STAMP, put_file, require
from cases.pull_routes import SourceStable
from cases.config_backup import message

VECTORS = [
    ('H1057', 'null-proxies', b'proxies: null\n', 'Provider proxies 必须是数组'),
    ('H1057', 'object-proxies', b'proxies: {}\n', 'Provider proxies 必须是数组'),
    ('H1057', 'string-proxies', b'proxies: text\n', 'Provider proxies 必须是数组'),
    ('H1058', 'scalar-proxy', b'proxies: [1]\n', 'Provider 代理必须是含非空 name 的对象'),
    ('H1058', 'null-proxy', b'proxies: [null]\n', 'Provider 代理必须是含非空 name 的对象'),
    ('H1058', 'missing-name', b'proxies: [{}]\n', 'Provider 代理必须是含非空 name 的对象'),
    ('H1058', 'empty-name', b'proxies: [{name: " "}]\n', 'Provider 代理必须是含非空 name 的对象'),
    ('H1059', 'references-not-array', b'proxies: [{name: synthetic, proxies: text}]\n', 'Provider proxies 引用必须是字符串数组'),
    ('H1059', 'references-not-string', b'proxies: [{name: synthetic, proxies: [1]}]\n', 'Provider proxies 引用必须是字符串数组'),
    ('H1060', 'type-not-string', b'proxies: [{name: synthetic, type: 1}]\n', 'Provider type 必须是字符串'),
    ('H1061', 'actual-YAML-syntax-error', b'proxies: [\n', 'YAML'),
    ('H1062', 'sequence-root', b'# proxies:\n[1]\n', 'Provider 必须是 YAML 对象'),
]


def shapes(binary, recorder):
    targets = ROUTES + [('GET /api/subscriptions/proxies', '/api/subscriptions/proxies', 'R222')]
    for case, variant, raw, diagnostic in VECTORS:
        for route, path, rid in targets:
            with Source(raw) as source:
                sub = {**SUB, 'url': source.url + '/feed'}
                with Pair(binary, recorder, options={'shared': {'subscriptions': [sub]}}) as pair:
                    pair.request('R217-S01', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                        body=json_body({**sub, 'interval': 60}), expect=200, writes=True,
                        variant='provider-shape/' + rid + '/' + variant + '/enable-first-fetch')
                    # Legacy node cache is valid but cannot prove current URL.
                    # A bad current response must be rejected instead of served
                    # successfully from these old nodes.
                    put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': STAMP,
                        'count': len(CACHE_NODES), 'nodes': CACHE_NODES})
                    before_db, before_files = database(pair.dirs[0]), files(pair.dirs[0])
                    transport = 'shared/subscription-downloads/' + hashlib.sha256(sub['url'].encode()).hexdigest() + '.json'
                    pair.policy.times.add('files.' + transport + '.content.fetched_at')
                    def check(reply):
                        data = reply.json()
                        require(data.get('success') is False and diagnostic in data.get('message', ''),
                                'source reaches the exact downloaded provider guard: ' + diagnostic + ': ' + repr(data))
                        require(database(pair.dirs[0]) == before_db, 'invalid downloaded provider leaves every SQL row and revision exact')
                        current = files(pair.dirs[0])
                        value = current.pop(transport, {})
                        content = value.get('content', {})
                        require(value.get('mode') == 0o600 and set(content) == {'text', 'fetched_at', 'traffic'}
                            and content['text'] == raw.decode() and content['traffic'] is None
                            and timestamp(content['fetched_at'], pair.policy),
                            'only a precisely validated original transport download is persisted')
                        require(current == before_files, 'good node cache, served files and all unrelated files remain byte-identical')
                    pair.request(case, route, path, expect=400, writes=True, check=check,
                                 interaction=Exchange(pair, source), variant=variant)
                    if case in ('H1057', 'H1058', 'H1059', 'H1060', 'H1062'):
                        pair.recorder.results[-1]['helper_cases'] = ['H1063']


def disabled_without_cache(binary, recorder):
    for route, path, rid in ROUTES + [('GET /api/subscriptions/proxies', '/api/subscriptions/proxies', 'R222')]:
        with Source(b'interval-zero must not download implicitly') as source:
            sub = {**SUB, 'url': source.url + '/feed'}
            with Pair(binary, recorder, options={'shared': {'subscriptions': [sub]}}) as pair:
                text = ('订阅「' + sub['name'] + '」已关闭自动更新，且没有当前订阅地址的可用缓存。'
                        '请先在「订阅来源」中手动更新，再重新发布或同步。')
                pair.request('H1087', route, path, expect=400, writes=True, check=message(text),
                             interaction=SourceStable(pair, source), variant='interval-zero-no-current-node-cache')


GROUPS = {'provider-downloaded-shape-guards': shapes, 'provider-manual-mode-missing-cache-guard': disabled_without_cache}
