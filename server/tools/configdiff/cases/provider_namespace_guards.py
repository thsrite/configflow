"""Provider names collide with real panel chains or generated airport groups."""
from harness import Pair
from sources import Source
from cases.config_backup import message
from cases.groups import NODES, CHAIN
from cases.pull_routes import SourceStable
from cases.subscriptions import SUB, STAMP, put_file


def names(binary, recorder):
    for shape in ('panel-chain', 'airport-hidden-group'):
        sub = {**SUB, 'name': '合成来源', 'url': ''}
        root = {'id': 'provider-root', 'name': '提供者根', 'type': 'select', 'subscriptions': ['sub-a']}
        groups = [root]
        metadata = {'url': ''}
        if shape == 'panel-chain':
            groups.append({'id': 'namespace-chain', 'name': '链出口', 'type': 'chain', 'chain': CHAIN})
            cache = [{'id': 'provider-collision', 'name': '链出口', 'type': 'socks5', 'server': '192.0.2.4', 'port': 1080}]
        else:
            groups.append({'id': 'hidden-name-collision', 'name': '合成来源/Relay', 'type': 'select', 'manual_nodes': ['DIRECT']})
            cache = [
                {'id': 'airport-exit', 'name': 'Exit', 'type': 'socks5', 'server': '192.0.2.4', 'port': 1080,
                 'params': {'dialer-proxy': 'Relay'}},
                {'id': 'airport-relay', 'name': 'Relay-A', 'type': 'socks5', 'server': '192.0.2.5', 'port': 1080}]
            metadata['dialers'] = {'targets': {'Relay': {'type': 'select', 'members': ['Relay-A']}}, 'unresolved': []}
        options = {'shared': {'nodes': NODES, 'subscriptions': [sub]}, 'profile': {'proxy_groups': groups}}
        with Source(b'current stored provider names must not cause a download') as source, Pair(binary, recorder, options=options) as pair:
            put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': STAMP,
                'nodes': cache, 'count': len(cache), 'metadata': metadata})
            if shape == 'panel-chain':
                pair.request('H1056', 'GET /api/subscriptions/<sub_id>/proxies', '/api/subscriptions/sub-a/proxies',
                    expect=400, writes=True, interaction=SourceStable(pair, source),
                    check=message('订阅「合成来源」准备失败：Provider 节点名称与代理链出口冲突\n文件：./providers/合成来源.yaml'), variant=shape)
            else:
                for route, path in [('POST /api/generate/mihomo/preview', '/api/generate/mihomo/preview'),
                        ('POST /api/profiles/<profile_id>/generate/mihomo/preview', '/api/profiles/default/generate/mihomo/preview')]:
                    pair.request('H1081', route, path, body='{}', expect=400, writes=True,
                        interaction=SourceStable(pair, source),
                        check=message('名称「合成来源/Relay」已被使用，与订阅「合成来源」自动生成的中转分组冲突，请改名后重新生成'), variant=shape)


GROUPS = {'provider-real-chain-and-airport-name-collisions': names}
