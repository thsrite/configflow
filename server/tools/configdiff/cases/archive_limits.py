"""Archive path and real 4096-file limits through complete MosDNS downloads."""
import io
import zipfile
from harness import Pair
from sources import Source
from cases.config_backup import message
from cases.pull_routes import SourceStable
from cases.rule_library import DOMAIN
from cases.subscriptions import require

TARGETS = [('POST /api/generate/mosdns', '/api/generate/mosdns'),
           ('POST /api/profiles/<profile_id>/generate/mosdns', '/api/profiles/default/generate/mosdns')]


def settings(name):
    return {'shared': {'rule_library': [{**DOMAIN, 'name': name}]},
            'profile': {'rule_configs': [{'id': 'archive-reference', 'itemType': 'ruleset',
                'library_rule_id': DOMAIN['id'], 'policy': 'DIRECT', 'enabled': True}],
                'mosdns': {'direct_rulesets': ['archive-reference']}}}


def paths(binary, recorder):
    vectors = [('H001', 'bad\\name', 'bad\\name', '规则文件路径无效'),
               ('H001', 'bad\nname', 'badname', '规则文件路径无效'),
               ('H002', '../escape', '../escape', '规则文件必须使用安全的相对路径'),
               ('H002', '/absolute', '/absolute', '规则文件必须使用安全的相对路径')]
    for case, name, label, text in vectors:
        with Source(b'unsafe archive paths are rejected before any download') as source:
            with Pair(binary, recorder, options=settings(name)) as pair:
                for route, path in TARGETS:
                    pair.request(case, route, path, body='{}', expect=500, writes=True,
                        check=message('规则“' + label + '”准备失败：' + text), interaction=SourceStable(pair, source),
                        variant='real-library-name-path/' + repr(name))
    opts = settings('custom_hosts')
    opts['profile']['mosdns']['custom_hosts'] = 'custom.example 192.0.2.53\n'
    with Source(b'inline rules need no remote source') as source, Pair(binary, recorder, options=opts) as pair:
        for route, path in TARGETS:
            def check(reply):
                with zipfile.ZipFile(io.BytesIO(reply.body)) as archive:
                    require(archive.read('rules/custom_hosts.txt') == b'custom.example 192.0.2.53'
                        and archive.read('rules/custom_hosts_2.txt') == b'domain:example.com',
                        'real builtin tag reservation prevents the alleged archive path collision')
            pair.request('G10', route, path, body='{}', expect=200, writes=True, filename='mosdns-config.zip',
                check=check, interaction=SourceStable(pair, source),
                variant='reserved-custom-hosts-name-produces-two-disjoint-valid-files')


def file_count(binary, recorder):
    library = [{**DOMAIN, 'id': 'limit-' + str(i), 'name': 'limit-' + str(i), 'behavior': 'classical',
                'content': 'DOMAIN,example.com\nIP-CIDR,192.0.2.0/24\n'} for i in range(2048)]
    references = [{'id': 'ref-' + str(i), 'itemType': 'ruleset', 'library_rule_id': rule['id'],
                   'policy': 'DIRECT', 'enabled': True} for i, rule in enumerate(library)]
    opts = {'shared': {'rule_library': library}, 'profile': {'rule_configs': references,
            'mosdns': {'direct_rulesets': [rule['id'] for rule in references]}}}
    with Source(b'file-order limit is checked before downloads') as source, Pair(binary, recorder, options=opts) as pair:
        for route, path in TARGETS:
            pair.request('H013', route, path, body='{}', expect=500, writes=True,
                check=message('生成配置超过打包文件数量限制'), interaction=SourceStable(pair, source),
                variant='real-2048-classical-sources-create-4096-download-members-plus-config')


def inline_count(binary, recorder):
    rules = [{'id': 'inline-' + str(i), 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'example.com',
              'policy': 'DIRECT', 'enabled': True} for i in range(4096)]
    opts = {'profile': {'rule_configs': rules, 'mosdns': {
        'direct_rules': [rule['id'] for rule in rules[::2]], 'proxy_rules': [rule['id'] for rule in rules[1::2]]}}}
    with Source(b'inline rules never download') as source, Pair(binary, recorder, options=opts) as pair:
        for route, path in TARGETS:
            pair.request('H011', route, path, body='{}', expect=500, writes=True,
                check=message('生成配置超过打包大小限制'), interaction=SourceStable(pair, source),
                variant='real-4096-adjacent-routing-groups-plus-main-file-exceed-add-limit')


GROUPS = {'archive-real-name-path-guards': paths,
          'archive-real-download-file-order-limit': file_count,
          'archive-real-inline-file-add-limit': inline_count}
