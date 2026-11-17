"""Retained duplicate YAML keys and real generated fixed-listener validation."""
from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.groups import options, BASE
from cases.pull_routes import SourceStable


def guarded(binary, recorder):
    fixture = options()
    name = BASE[0]['name']
    fixture['profile']['mihomo'] = {'custom_config':
        'listeners:\n  - {name: synthetic-inbound, type: socks, port: 1088, proxy: "' + name + '", proxy: "' + name + '"}\n'}
    with Source(b'listener guard never fetches sources') as source, Pair(binary, recorder, options=fixture) as pair:
        pair.request('H254', 'PUT /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a',
            body=json_body({'name': '真实改名但必须拒绝🧪'}), expect=400, writes=True,
            interaction=SourceStable(pair, source), check=message('无法安全保留 Mihomo 基础配置的注释与引用，原配置已保留'),
            variant='PyYAML-accepts-retained-duplicate-key-roundtrip-edit-rejects')
    fixture = options()
    fixture['profile']['mihomo'] = {'custom_config':
        'listeners: [{name: synthetic-inbound, type: socks, port: 1088, proxy: 不存在的入口策略🧪}]\n'}
    with Source(b'generated listener guard never fetches sources') as source, Pair(binary, recorder, options=fixture) as pair:
        for route, path in [('POST /api/generate/mihomo/preview', '/api/generate/mihomo/preview'),
                ('POST /api/profiles/<profile_id>/generate/mihomo/preview', '/api/profiles/default/generate/mihomo/preview')]:
            pair.request('H255', route, path, body='{}', expect=400, writes=True, interaction=SourceStable(pair, source),
                check=message('Mihomo listeners[0].proxy 引用了不存在或未启用的策略「不存在的入口策略🧪」'),
                variant='fixed-listener-target-is-not-an-emitted-policy')


GROUPS = {'groups-real-retained-and-generated-listener-guards': guarded}
