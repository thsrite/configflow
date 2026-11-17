"""Public schema formats, typed diagnostics and included-global origins."""
from collections import Counter

import yaml

from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.oxidns_plugin_helpers import check_issue
from cases.pull_routes import SourceStable
from cases.subscriptions import require

ROUTE = 'POST /api/oxidns/validate'
UNOBSERVABLE = {'H704': 'format名称固定在Python schema目录内；HTTP草稿不能定义schema，未知插件不能触发Unsupported OxiDNS schema format。'}


def plugin(kind, args):
    if kind == 'http_request':
        args = {'method': 'GET', **args}
    return {'plugins': [{'tag': 'subject', 'type': kind, 'args': args}]}


def vectors(origin):
    prefix = ['plugins', 0, 'args']
    rows = []
    def add(number, document, path, text, severity='error'):
        rows.append(('H' + str(number), document, path, text, severity))
    def arg(number, kind, args, path, text, severity='error'):
        add(number, plugin(kind, args), prefix + path, text, severity)
    arg(699, 'forward', {'upstreams': [{'addr': 'udp://192.0.2.53', 'dial_addr': '192.0.2.1%synthetic'}]},
        ['upstreams', 0, 'dial_addr'], '必须为有效 IP 地址')
    url_error = 'URL/上游地址无效或协议不受支持'
    for number, value in [(700, origin + '/has space'), (701, 'file:///synthetic'),
                          (701, 'https:///missing-host'), (702, 'https://example.com:bad')]:
        arg(number, 'forward', {'upstreams': [{'addr': value}]}, ['upstreams', 0, 'addr'], url_error)
    arg(703, 'time', {'timezone': 'Synthetic/Unknown', 'periods': [{'start': '12:00', 'end': '13:00'}]},
        ['timezone'], '时区须为有效 IANA 时区，例如 Asia/Shanghai')
    add(705, {'plugins': [], 'log': {'level': 123}}, ['log', 'level'], '参数类型无效，需要 string')
    deferred = '含动态占位符，实际值需由目标环境解析；静态校验无法确认展开后的参数'
    add(706, {'plugins': [], 'runtime': {'worker_threads': '${SYNTHETIC_THREADS}'}}, ['runtime', 'worker_threads'], deferred, 'warning')
    add(707, {'plugins': [], 'log': {'level': True}}, ['log', 'level'], '参数类型无效，需要 string')
    arg(708, 'forward', {'upstreams': [{'addr': 'udp://192.0.2.53'}], 'response_selection': 'invalid'}, ['response_selection'], '不在该参数允许的选项中')
    arg(709, 'cache', {'size': 0}, ['size'], '不能小于 1')
    arg(709, 'rate_limiter', {'qps': 0}, ['qps'], '必须大于 0')
    arg(709, 'forward', {'upstreams': [{'addr': 'udp://192.0.2.53', 'max_conns': 4097}]}, ['upstreams', 0, 'max_conns'], '不能大于 4096')
    arg(710, 'script', {'command': ''}, ['command'], '文本长度不足')
    arg(711, 'forward', {'upstreams': []}, ['upstreams'], '至少需要 1 个条目')
    add(711, plugin('sequence', []), prefix, '至少需要 1 个条目')
    arg(712, 'time', {'periods': [{'start': '12:00', 'end': '13:00'} for _ in range(65)]}, ['periods'], '条目数量超过 64')
    arg(713, 'forward', {}, ['upstreams'], '缺少必填参数')
    arg(714, 'ip_selector', {'synthetic_extension': True}, ['synthetic_extension'], '官方核心不接受此对象中的未知字段')
    arg(715, 'cache', {'synthetic_extension': 'preserved'}, ['synthetic_extension'], '扩展字段已保留，不在当前版本的校验目录中', 'warning')
    arg(716, 'http_request', {'url': origin + '/static-only', 'method': 'bad method'}, ['method'], 'HTTP 方法须为有效的 token')
    add(717, {'plugins': [{'tag': 'subject', 'type': 'synthetic_unknown'}]}, ['plugins', 0, 'type'], '插件未在当前校验目录中，发布时需由目标核心确认支持', 'warning')
    add(718, {'plugins': [{'tag': 'subject', 'type': 'udp_server'}]}, prefix, '缺少插件必填参数')
    # Repeat type/range predicates with each meaningful JSON/YAML shape; none
    # of the accepted cases is obtained by coercing a bool/float into an int.
    for value in (True, 1.0, '1'):
        arg(707, 'cache', {'size': value}, ['size'], '参数类型无效，需要 integer / null')
    arg(699, 'forward', {'upstreams': [{'addr': 'udp://192.0.2.53', 'dial_addr': 'fe80::1%synthetic'}]},
        ['upstreams', 0, 'dial_addr'], '必须为有效 IP 地址')
    arg(702, 'forward', {'upstreams': [{'addr': 'https://[broken'}]}, ['upstreams', 0, 'addr'], url_error)
    arg(706, 'http_request', {'url': '${SYNTHETIC_URL}'}, ['url'], deferred, 'warning')
    arg(708, 'forward', {'upstreams': [{'addr': 'udp://192.0.2.53', 'bootstrap_version': 5}]}, ['upstreams', 0, 'bootstrap_version'], '不在该参数允许的选项中')
    for kind, args, path in [('http_request', {'url': ''}, ['url']), ('http_request', {'url': origin + '/', 'body': ''}, ['body']),
        ('http_request', {'url': origin + '/', 'content_type': ''}, ['content_type']), ('query_recorder', {'path': ''}, ['path'])]:
        arg(710, kind, args, path, '文本长度不足')
    arg(711, 'cron', {'jobs': []}, ['jobs'], '至少需要 1 个条目')
    arg(711, 'download', {'downloads': []}, ['downloads'], '至少需要 1 个条目')
    arg(714, 'ip_selector', {'cache': {'synthetic_extension': True}}, ['cache', 'synthetic_extension'], '官方核心不接受此对象中的未知字段')
    return rows


def constraints(binary, recorder):
    with Source(b'schema validation must not connect') as source, Pair(binary, recorder, options={'isolated_cwd': True}) as pair:
        counters = Counter()
        for case, document, path, text, severity in vectors(source.url):
            settings = {'config_mode': 'native', 'native_config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)}
            pair.request(case, ROUTE, body=json_body({'settings': settings}), expect=200, writes=True,
                check=lambda r: check_issue(r, path, text, severity), interaction=SourceStable(pair, source),
                variant='schema-format/' + str(counters[case]))
            counters[case] += 1
        # Included globals are validated, although they cannot override the
        # parent's effective globals. The origin must remain the exact file.
        included = 'plugins: []\nlog: {level: 123}\n'
        settings = {'config_mode': 'native', 'native_config': 'include: [./included.yaml]\nplugins: []\n',
                    'additional_files': [{'path': 'included.yaml', 'encoding': 'text', 'content': included}]}
        pair.request('H720', ROUTE, body=json_body({'settings': settings}), expect=200, writes=True,
            check=lambda r: check_issue(r, ['log', 'level'], '参数类型无效，需要 string', 'error', source='included.yaml'),
            interaction=SourceStable(pair, source), variant='included-global-origin')
        # The strict public renderer rejects diagnostics that the read-only
        # validator exposes as HTTP200. native_document may reject first; this
        # is a partial observation of the later H719 defensive gate.
        settings = {'config_mode': 'native', 'native_config': yaml.safe_dump(plugin('cache', {'size': 0}))}
        pair.request('H719', 'POST /api/oxidns/render', body=json_body({'settings': settings}), expect=400, writes=True,
            check=message('OxiDNS 配置校验未通过：plugins[0].args.size 不能小于 1'),
            interaction=SourceStable(pair, source), variant='strict-render-gate/earlier-native-validation')
        require(len(counters) == 19, '19 schema helper IDs plus H719/H720; H704 has no public format selector')


GROUPS = {'oxidns-schema-helper-constraints': constraints}
