"""Archive callback resolution through real MosDNS/OxiDNS download APIs."""
from urllib.parse import quote

from harness import Pair
from sources import Source
from cases.config_backup import message
from cases.pull_routes import SourceStable
from cases.rule_library import DOMAIN

NAME = '回调规则🧪'
PREFIX = '规则“' + NAME + '”准备失败：'


def options(url, proxy, format='text'):
    library = {**DOMAIN, 'id': 'callback-library', 'name': NAME, 'source_type': 'url', 'url': url,
               'format': format, 'update_interval': 0}
    library.pop('content', None)
    return {'shared': {'rule_library': [library]}, 'system_config': {'rule_fetch_proxy': proxy},
            'profile': {'rule_configs': [{'id': 'callback-reference', 'itemType': 'ruleset',
                                         'library_rule_id': library['id'], 'policy': 'DIRECT', 'enabled': True}],
                        'mosdns': {'direct_rulesets': ['callback-reference']},
                        'oxidns': {'direct_rulesets': ['callback-reference']}}}


def callbacks(binary, recorder):
    rows = [('H003', 'http://panel.example/api/profiles/other/rule-library/content/callback-library', '规则回调引用了其他配置'),
            ('H004', 'http://panel.example/api/rule-library/content/callback-library', '规则回调存在循环引用'),
            ('H005', 'http://panel.example/api/mosdns/rule-proxy?part=domain', '规则缺少下载地址'),
            ('H006', 'http://panel.example/api/mosdns/rule-proxy?part=unknown&url=' + quote('https://source.example/rules', safe=''), '规则转换类型无效'),
            ('H007', 'http://panel.example/api/unknown-callback', '无法识别规则回调'),
            ('H008', 'http://panel.example/api/rule-library/content/missing-synthetic', '规则不在当前配置快照中')]
    with Source(b'archive callback resolution must not open HTTP') as source:
        source.status = 502
        for case, url, text in rows:
            with Pair(binary, recorder, options=options(url, source.url)) as pair:
                for service, ident, status in [('mosdns', case, 500), ('oxidns', 'H024', 400)]:
                    pair.request(ident, 'POST /api/generate/' + service, body='{}', expect=status, writes=True,
                        check=message(PREFIX + text), interaction=SourceStable(pair, source),
                        variant='archive-callback/' + case + '/' + service)
        with Pair(binary, recorder, options=options(source.url + '/never-request-mrs', source.url, 'mrs')) as pair:
            text = '规则“' + NAME + '”下载失败：OxiDNS/MosDNS 不支持 MRS 二进制规则，请使用该订阅的 Text 或 YAML 版本。'
            for case, template, path in [('R068-F08', 'POST /api/generate/mosdns', '/api/generate/mosdns'),
                    ('R131-F08', 'POST /api/profiles/<profile_id>/generate/mosdns', '/api/profiles/default/generate/mosdns')]:
                pair.request(case, template, path, body='{}', expect=500, writes=True,
                    check=message(text), interaction=SourceStable(pair, source), variant='declared-MRS-rejected-before-HTTP')
    # These cases also prove the outer archive preparation-error wrapper H012;
    # the helper label is attached to the same request, without duplicate HTTP.


GROUPS = {'archive-callback-helper-guards': callbacks}


def retained_conversion(binary, recorder):
    from cases.archive_limits import settings, TARGETS
    opts = settings('保留错误内容')
    opts['shared']['rule_library'][0]['content'] = 1
    with Source(b'inline conversion must not contact a network') as source, Pair(binary, recorder, options=opts) as pair:
        for route, path in TARGETS:
            pair.request('H016', route, path, body='{}', expect=500, writes=True,
                interaction=SourceStable(pair, source),
                check=message('规则“保留错误内容”转换失败：exception_type=AttributeError'),
                variant='retained-inline-content-integer-reaches-real-conversion')


GROUPS['archive-real-retained-inline-conversion-failure'] = retained_conversion
