"""Strict emitted graph guards on downloaded, selected provider payloads."""
import copy
import hashlib

from compare import database, files, timestamp
from harness import Pair, json_body
from sources import Source
from subscriptions import Exchange
from cases.groups import NODES, CHAIN
from cases.subscriptions import SUB, require


def rejected(binary, recorder):
    basic = {'name': 'ordinary', 'type': 'socks5', 'server': '192.0.2.8', 'port': 1080}
    import yaml
    vectors = [
        ('H210', 'duplicate-original-names', [basic, {**basic, 'dialer-proxy': 'DIRECT'}], {}, 'Provider 节点名称重复'),
        ('H212', 'builtin-original-name', [{**basic, 'name': 'DIRECT'}, {**basic, 'dialer-proxy': 'DIRECT'}], {},
         'Provider 节点名称与代理拓扑冲突'),
        ('H213', 'unresolved-original-dialer', [{**basic, 'dialer-proxy': 'missing-synthetic-policy'}], {},
         '的 dialer-proxy「missing-synthetic-policy」引用了不存在的代理或策略组'),
        ('H211', 'chain-exit-empty-provider', [], {'chain': True}, '拨号拓扑 Provider 过滤后不能为空'),
        ('H206', 'raw-name-reserved-order-marker', [{**basic, 'name': '排序_合成'}],
         {'chain': True, 'regex': '.*`x'}, 'Provider 原始节点名称与代理链私有排序标记冲突'),
        ('H218', 'same-static-and-provider-candidate', [basic], {'overlap': True}, '策略组候选名称重复'),
        ('H1065', 'chain-source-fails-without-any-cache', [], {'chain': True, 'status': 403}, '订阅没有可用缓存或远程代理'),
    ]
    for case, variant, proxies, flags, diagnostic in vectors:
        raw = yaml.safe_dump({'proxies': proxies}, allow_unicode=True).encode()
        with Source(raw) as source:
            source.status = flags.get('status', 200)
            sub = {**SUB, 'url': source.url + '/feed'}
            group = {'id': 'emitted-provider-root', 'name': '真实Provider来源', 'type': 'select', 'subscriptions': ['sub-a']}
            groups = [group]
            nodes = copy.deepcopy(NODES)
            if flags.get('overlap'):
                nodes[0].update(name='ordinary', params={'dialer-proxy': 'DIRECT'})
                group['manual_nodes'] = ['n-a']
            if flags.get('chain'):
                if 'regex' in flags:
                    group['regex'] = flags['regex']
                groups.append({'id': 'emitted-chain', 'name': 'Provider落地链', 'type': 'chain',
                    'chain': {**copy.deepcopy(CHAIN), 'exit': {'type': 'group', 'id': group['id']}}})
            with Pair(binary, recorder, options={'shared': {'nodes': nodes, 'subscriptions': [sub]},
                    'profile': {'proxy_groups': groups}}) as pair:
                pair.request('R217-S01', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                    body=json_body({**sub, 'interval': 60}), expect=200, writes=True, variant='emitted/' + variant + '/enable')
                before_db, before_files = database(pair.dirs[0]), files(pair.dirs[0])
                transport = 'shared/subscription-downloads/' + hashlib.sha256(sub['url'].encode()).hexdigest() + '.json'
                pair.policy.times.add('files.' + transport + '.content.fetched_at')
                def check(reply):
                    require(reply.json().get('success') is False and diagnostic in reply.json().get('message', ''),
                        'downloaded selected provider reaches named emitted guard: ' + diagnostic + ': ' + repr(reply.json()))
                    require(database(pair.dirs[0]) == before_db, 'graph rejection never changes SQL')
                    current = files(pair.dirs[0])
                    if source.status != 200:
                        require(current == before_files, 'failed upstream produces neither transport, node nor published cache')
                        return
                    value = current.pop(transport, {})
                    content = value.get('content', {})
                    require(value.get('mode') == 0o600 and set(content) == {'text', 'fetched_at', 'traffic'}
                        and content['text'] == raw.decode() and content['traffic'] is None
                        and timestamp(content['fetched_at'], pair.policy), 'original transport is complete and current')
                    require(current == before_files, 'no published or node cache is installed on graph failure')
                pair.request(case, 'GET /api/subscriptions/<sub_id>/proxies', '/api/subscriptions/sub-a/proxies',
                    expect=400, writes=True, interaction=Exchange(pair, source), check=check, variant=variant)


GROUPS = {'provider-real-downloaded-emitted-graph-guards': rejected}


def late_activation(binary, recorder):
    import yaml
    node = {'name': 'duplicate', 'type': 'socks5', 'server': '192.0.2.8', 'port': 1080}
    duplicate = yaml.safe_dump({'proxies': [node, node]}).encode()
    activation = yaml.safe_dump({'proxies': [{**node, 'name': 'activate', 'dialer-proxy': 'DIRECT'}]}).encode()
    with Source(activation) as source:
        source.responses['/duplicate'] = (200, duplicate, {'Content-Type': 'text/yaml'})
        subs = [{**SUB, 'name': '先前非拓扑来源', 'url': source.url + '/duplicate'},
            {**SUB, 'id': 'sub-b', 'name': '后置启用来源', 'url': source.url + '/activation'}]
        with Pair(binary, recorder, options={'shared': {'subscriptions': subs}, 'profile': {'proxy_groups': [
                {'id': 'late-activation-root', 'name': '完整双来源', 'type': 'select', 'subscriptions': ['sub-a', 'sub-b']}]}}) as pair:
            for sub in subs:
                pair.request('R217-S01', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/' + sub['id'],
                    body=json_body({**sub, 'interval': 60}), expect=200, writes=True, variant='late-activation/enable/' + sub['id'])
            baseline_db, baseline_files = database(pair.dirs[0]), files(pair.dirs[0])
            expected = {}
            for sub, raw in zip(subs, (duplicate, activation)):
                path = 'shared/subscription-downloads/' + hashlib.sha256(sub['url'].encode()).hexdigest() + '.json'
                expected[path] = raw.decode()
                pair.policy.times.add('files.' + path + '.content.fetched_at')
            def check(reply):
                require('Provider 节点名称重复' in reply.json().get('message', '')
                    and '先前非拓扑来源' in reply.json()['message'], 'later source activates strict validation of the prior plain duplicate collection')
                require(database(pair.dirs[0]) == baseline_db, 'late activation rejection leaves all SQL unchanged')
                current = files(pair.dirs[0])
                for path, text in expected.items():
                    value = current.pop(path, {})
                    content = value.get('content', {})
                    require(value.get('mode') == 0o600 and set(content) == {'text', 'traffic', 'fetched_at'}
                        and content['text'] == text and content['traffic'] is None and timestamp(content['fetched_at'], pair.policy),
                        'each raw source transport remains exact on rejected graph')
                require(current == baseline_files, 'late validation never commits either staged node cache or any served graph')
            pair.request('H1067', 'GET /api/subscriptions/<sub_id>/proxies', '/api/subscriptions/sub-b/proxies',
                expect=400, writes=True, check=check, interaction=Exchange(pair, source, expected=2),
                variant='plain-duplicate-source-revalidated-after-second-source-activates-dialer')


GROUPS['provider-real-late-topology-activation-revalidates-all-sources'] = late_activation
