"""Source-derived Mihomo form/EasyTier validation, one changed field per request."""
import copy
import sys

from compare import database, files
from harness import Pair, DesignError, json_body, ROOT
from cases.config_backup import message
from cases.custom_config import setup


def run_guard(pair, case, body, expected, variant=''):
    before = (database(pair.dirs[0]), files(pair.dirs[0]))
    def check(reply):
        message(expected)(reply)
        if before != (database(pair.dirs[0]), files(pair.dirs[0])):
            raise DesignError('Python rejected Mihomo form persisted a partial change')
    pair.request(case, 'POST /api/custom-config/mihomo', body=json_body(body), variant=variant,
                 expect=400, writes=True, check=check)


def nested(path, value):
    out = value
    for key in reversed(path):
        out = {key: out}
    return out


def base(binary, recorder):
    sys.path.insert(0, str(ROOT))
    import yaml
    # Only Python specification constants are used to enumerate form fields;
    # validation itself is always exercised through the actual HTTP server.
    from backend.common.mihomo_base_settings import BOOLEANS, INTEGERS, ENUMS, STRING_LISTS, TEXTS, REMOVED_FINGERPRINT, SNIFF_PORTS_HINT
    with Pair(binary, recorder) as pair:
        setup(pair)
        def call(hid, document, expected, variant=''):
            run_guard(pair, hid, {'config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)}, expected, variant)
        for path, label in BOOLEANS.items():
            for bad in ('true', 1, None, []):
                call('H283', nested(path, bad), f'{label} 只能填写 true 或 false，不要加引号', '.'.join(path) + '/' + repr(bad))
        for path, (label, low, high) in INTEGERS.items():
            for bad in (low - 1, high + 1, True, float(low), str(low)):
                call('H284', nested(path, bad), f'{label} 需为 {low}–{high} 的整数', '.'.join(path) + '/' + repr(bad))
        call('H285', {'global-client-fingerprint': 'chrome'}, REMOVED_FINGERPRINT)
        for path, (label, choices) in ENUMS.items():
            for bad in ('invalid-synthetic', 123, None):
                call('H286', nested(path, bad), f'{label} 只能是 {" / ".join(choices)} 之一', '.'.join(path) + '/' + repr(bad))
        for path, label in STRING_LISTS.items():
            for bad in ('x', [None], [' '], [123]):
                call('H287', nested(path, bad), f'{label} 需为文本列表，每行一项，不能有空项', '.'.join(path) + '/' + repr(bad))
        for path, label in TEXTS.items():
            for bad in (None, '', ' ', 1):
                call('H288', nested(path, bad), f'{label} 需为非空文本；不需要时删除该项', '.'.join(path) + '/' + repr(bad))
        for bad in (1, 'bad', '127.0.0.1/16', 'ff00::/8', '198.18.0.1/31'):
            call('H290', {'dns': {'fake-ip-range': bad}},
                 'dns.fake-ip-range 需为 IPv4 网段（/8 至 /30），例如 198.18.0.1/16，不能是回环、多播或链路本地地址', repr(bad))
        call('H291', {'sniffer': {'sniff': []}}, 'sniffer.sniff 需为对象，按协议 HTTP / TLS / QUIC 分别填写')
        call('H292', {'sniffer': {'sniff': {'SMTP': {}}}}, 'sniffer.sniff 只支持 HTTP / TLS / QUIC，「SMTP」无法识别，Mihomo 会拒绝加载')
        for proto in ('HTTP', 'TLS', 'QUIC'):
            for bad in ([], ['bad'], [0], [65536], [True], [1.0], ['900-80']):
                call('H293', {'sniffer': {'sniff': {proto: {'ports': bad}}}}, f'sniffer.sniff.{proto}.ports：{SNIFF_PORTS_HINT}', proto + '/' + repr(bad))
            for bad in ('true', 1, None):
                call('H294', {'sniffer': {'sniff': {proto: {'ports': [443], 'override-destination': bad}}}},
                     f'sniffer.sniff.{proto}.override-destination 只能填写 true 或 false，不要加引号', proto + '/' + repr(bad))
        for bad in (1, 'noport', '127.0.0.1:0', '127.0.0.1:65536'):
            call('H295', {'external-controller': bad}, 'external-controller 需写成「地址:端口」，例如 0.0.0.0:9090 或 127.0.0.1:9090，端口 1–65535', repr(bad))
        call('H296', {'external-controller': '192.0.2.1:9090'},
             'external-controller 只能监听 0.0.0.0 / ::（局域网可访问）或 127.0.0.1 / ::1（仅本机）：Agent 经本机回环地址读取连接、流量并热重载配置，监听其他地址时这些功能不可用')
        for bad in (123, []):
            call('H297', {'secret': bad}, '控制面板密码（secret）必须是文本，纯数字等请加引号', repr(bad))
        call('H298', {'external-controller': '0.0.0.0:9090'}, '控制面板对局域网开放（external-controller 监听所有地址）时必须设置控制面板密码（secret）')
        for hid, exposed in [('H299', True), ('H300', False)]:
            for secret in ('123456', 'short', 'with space', '含中文', 'x' * 65):
                document = {'secret': secret, 'external-controller': '0.0.0.0:9090' if exposed else '127.0.0.1:9090'}
                expected = '不能使用初始模板的默认密码 123456，请自己设置一个' if secret == '123456' else '控制面板密码需为 8–64 个字母、数字或符号，不含空格'
                call(hid, document, expected, repr(secret))
        for bad in ('ftp://example.com/ui', None, 'http://[bad'):
            call('H301', {'external-ui-url': bad}, 'external-ui-url 需为 http(s) 下载地址，例如 https://github.com/…/gh-pages.zip', repr(bad))
        for bad in (None, 'bad'):
            call('H302', {'bind-address': bad}, 'bind-address 只能填写 *（所有地址）或本机的一个 IP 地址', repr(bad))
        call('H303', {'mixed-port': 18080, 'socks-port': 18080}, '端口 18080 同时被 mixed-port 和 socks-port 使用，请改用不同端口')
        call('H306', {'dns': {'enable': True, 'nameserver': []}}, 'dns.nameserver 至少填写一个上游')
        call('H307', {'dns': {'respect-rules': True, 'proxy-server-nameserver': []}}, '开启 dns.respect-rules 时必须填写 dns.proxy-server-nameserver，否则 Mihomo 无法启动')
        for key, label in [('ipv6', 'ipv6'), ('dns.ipv6', 'dns 下的 ipv6')]:
            for bad in ('true', 1, None):
                call('R038-F10', nested(key.split('.'), bad), label + ' 必须为 true 或 false，不能是字符串或拼写错误的值', key + '/' + repr(bad))
        from cases.helpers import exception_policy
        def yaml_error(reply):
            if not reply.json().get('message', '').startswith('YAML 语法错误，未保存: '):
                raise DesignError('Python YAML error reached another branch')
        pair.request('R038-F09', 'POST /api/custom-config/mihomo', body=json_body({'config': 'dns: [bad'}),
                     expect=400, writes=True, policy=exception_policy(pair, 'YAML 语法错误，未保存: '),
                     check=yaml_error)
        for bad in (-1, 59, 604801, True, 60.0, '60'):
            run_guard(pair, 'R038-F08', {'config': '', 'provider_update_interval': bad},
                      'Mihomo 订阅同步间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数', repr(bad))
        call('R038-F11', {'mixed-port': 18080, 'socks-port': 18080}, '端口 18080 同时被 mixed-port 和 socks-port 使用，请改用不同端口')
        call('H304', {'mixed-port': 9}, 'mixed-port 使用端口 9，与Mihomo Agent「合成 Agent 🧪」的端口冲突，请改用其他端口')
        call('H305', {'dns': {'nameserver': ['127.0.0.1']}}, 'dns.nameserver 中的 127.0.0.1 指向本机或系统 DNS，会形成解析循环；请改用外部 DNS，例如 https://223.5.5.5/dns-query')


def easytier(binary, recorder):
    sys.path.insert(0, str(ROOT))
    from backend.common.mihomo_easytier import FIELDS, IP_VERSIONS, _PEER_SCHEMES
    with Pair(binary, recorder) as pair:
        setup(pair)
        def call(hid, value, expected, variant=''):
            run_guard(pair, hid, {'config': '', 'easytier': value}, 'EasyTier 设置未保存：' + expected, variant)
        call('H325', [], 'EasyTier 设置必须是对象')
        call('H326', {'enabled': 1}, 'enabled 必须是布尔值')
        call('H308', {'name': 1}, '节点名称 必须是文本', 'name')
        call('H309', {'name': ''}, '请填写节点名称')
        for bad in ('x' * 65, 'a\nb'):
            call('H310', {'name': bad}, '节点名称 过长或包含控制字符', 'name/' + repr(bad))
        call('H327', {'name': 'a,b'}, '节点名称不能包含英文逗号，否则规则无法引用')
        call('H328', {'apply_to': 'invalid'}, '下发范围只能是 subscription、agent 或 all')
        call('H329', {'magic_dns': 1}, 'magic_dns 必须是布尔值')
        call('H330', {'keepalive': [1]}, 'keepalive 必须是对象')
        call('H331', {'keepalive': {'enabled': 1}}, 'keepalive.enabled 必须是布尔值')
        for bad in (9, 86401, True, 60.0, '60'):
            call('H332', {'keepalive': {'interval': bad}}, '保活检测间隔需为 10 ~ 86400 秒的整数', repr(bad))
        call('H333', {'keepalive': {'url': 'ftp://example.com'}}, '保活检测地址应为 http:// 或 https:// 开头的网址')
        call('H334', {'keepalive': {'enabled': True}}, '开启保活时请填写检测地址，例如虚拟网内某台设备上的网页')
        call('H335', {'options': []}, 'options 必须是对象')
        call('H336', {'options': {'unknown-synthetic': 1}}, '不认识的 EasyTier 参数：unknown-synthetic')
        for field, kind in FIELDS:
            if kind in ('text', 'secret'):
                call('H308', {'options': {field: 1}}, field + ' 必须是文本', field)
                limit = 4096 if kind == 'secret' else 512
                call('H310', {'options': {field: 'x' * (limit + 1)}}, field + ' 过长或包含控制字符', field)
            elif kind == 'bool':
                for bad in (1, 'true'):
                    call('H316', {'options': {field: bad}}, field + ' 必须是开或关', field + '/' + repr(bad))
            elif kind == 'int':
                low, high = (576, 9000) if field == 'mtu' else (0, 2 ** 32 - 1)
                for bad in (True, 600.0, '600'):
                    call('H317', {'options': {field: bad}}, field + ' 必须是整数', field + '/' + repr(bad))
                for bad in (low - 1, high + 1):
                    call('H318', {'options': {field: bad}}, f'{field} 需在 {low} ~ {high} 之间', field + '/' + repr(bad))
            elif kind == 'list':
                call('H311', {'options': {field: 'bad'}}, f'{field} 必须是列表，最多 64 项', field + '/type')
                call('H311', {'options': {field: ['x'] * 65}}, f'{field} 必须是列表，最多 64 项', field + '/length')
        label = '走 EasyTier 的网段'
        call('H311', {'routes': [1] * 65}, label + ' 必须是列表，最多 64 项', 'routes')
        for hid, bad, expected in [('H312', '192.0.2.0/24->198.51.100.0/24', f'{label}「192.0.2.0/24->198.51.100.0/24」：Mihomo 的 EasyTier 不支持「->」网段映射，只能原样填写网段'),
                                   ('H313', 'bad', f'{label}「bad」不是有效的网段'),
                                   ('H314', '2001:db8::/32', f'{label}「2001:db8::/32」：EasyTier 虚拟网只支持 IPv4')]:
            call(hid, {'routes': [bad]}, expected)
        for field in ('peers', 'listeners', 'mapped-listeners'):
            call('H315', {'options': {field: ['bad']}}, f'{field}「bad」格式不对，应类似 tcp://203.0.113.10:11010（支持 {", ".join(_PEER_SCHEMES[:7])} 等）', field)
        for bad in ('2001:db8::1', 'bad'):
            call('H320', {'options': {'exit-nodes': [bad]}}, f'exit-nodes「{bad}」应填写出口节点的虚拟 IPv4 地址', bad)
        call('H321', {'options': {'ip-version': 'bad'}}, 'ip-version 只能是 ' + ' / '.join(IP_VERSIONS))
        call('H322', {'options': {'ipv4': 'bad'}}, 'ipv4「bad」应类似 10.144.144.10 或 10.144.144.10/24')
        call('H323', {'options': {'ipv4': '2001:db8::1'}}, 'ipv4 只能填写 IPv4 地址')
        call('H324', {'options': {'hostname': 'bad.name'}}, 'hostname 只能包含字母、数字和连字符，不超过 63 个字符')
        call('R038-F12', {'enabled': True}, '请填写 network-name（EasyTier 网络名）')
        call('H337', {'enabled': True}, '请填写 network-name（EasyTier 网络名）')
        options = {'network-name': 'synthetic', 'no-listener': True, 'listeners': ['tcp://127.0.0.1:11010']}
        call('H338', {'enabled': True, 'options': options}, '填写了 listeners 时不能同时开启 no-listener')
        call('H339', {'enabled': True, 'options': {'network-name': 'synthetic'}}, '不监听端口时至少要填写一个 peers（入网节点）')
        call('H340', {'enabled': True, 'options': {'network-name': 'synthetic', 'no-listener': False, 'local-public-key': 'synthetic-key'}},
             'local-public-key 需要和 local-private-key 一起填写')


def accepted(binary, recorder):
    import yaml
    sys.path.insert(0, str(ROOT))
    from backend.common.mihomo_base_settings import BOOLEANS, INTEGERS, ENUMS, STRING_LISTS
    from cases.custom_config import save
    with Pair(binary, recorder) as pair:
        setup(pair)
        def call(path, value, label):
            document = nested(path, value)
            if path == ('dns', 'respect-rules') and value is True:
                document['dns']['proxy-server-nameserver'] = ['https://192.0.2.53/dns-query']
            text = yaml.safe_dump(document, sort_keys=False)
            save(pair, 'mihomo', 'S01', {'config': text}, variant='accepted/' + label + '/' + '.'.join(path) + '/' + repr(value))
        for path in BOOLEANS:
            for value in (True, False):
                call(path, value, 'boolean')
        for path, (_, low, high) in INTEGERS.items():
            for value in (low, high):
                call(path, value, 'integer-boundary')
        for path, (_, choices) in ENUMS.items():
            for value in choices:
                call(path, value, 'enum')
        for value in (True, False):
            call(('find-process-mode',), value, 'YAML-on-off-compatibility')
        for path in STRING_LISTS:
            for value in ([], None):
                call(path, value, 'empty-list-or-null')
        for value in (None, ''):
            call(('secret',), value, 'controller-secret-local-empty')


GROUPS = {'mihomo-base-guards': base, 'mihomo-easytier-guards': easytier}

GROUPS['mihomo-base-accepted'] = accepted
