"""iKuai HTTP/authentication diagnostics, using actual public probes."""
from dataclasses import replace

import probes


HTTP_STATUSES = (401, 403, 404, 405, 408, 429, 502, 503, 504, 418, 500)
LABELS = tuple('http-' + str(status) for status in HTTP_STATUSES) + (
    'redirect-301-relative', 'redirect-301-other-origin', 'not-json-0', 'not-json-2',
    'login-shape-0', 'login-shape-1', 'result-success-code-refusal',
    'login-missing-code-old-result-refused', 'old-call-Data', 'old-call-Result',
    'call-code-0', 'call-code-1', 'call-code-2', 'call-code-3',
    'session-relogin-once', 'session-retry-still-expired', 'connection-closed', 'real-read-timeout')
HELPERS = tuple('H' + str(number).zfill(4) for number in (
    211, 212, 213, 214, 215, 216, 217, 218, 219, 224, 225, 226, 227, 230, 231, 232, 233, 234, 235, 236, 237))
CALL = {'peer': 'ikuai', 'path': '/Action/call', '/body/func_name': 'sysstat'}
EXTRA = {
    'missing-code': ({}, 'iKuai 调用 sysstat 返回格式无法识别', ('H0218', 'H0237')),
    'null-code': ({'code': None}, 'iKuai 调用 sysstat 返回格式无法识别', ('H0218', 'H0237')),
    'missing-message': ({'code': 3001}, 'iKuai 调用 sysstat 失败：未知错误（错误码 3001）', ('H0219', 'H0237')),
    'wrong-results': ({'code': 0, 'results': []}, 'iKuai 调用 sysstat 失败：未知错误（错误码 0）', ('H0219', 'H0237')),
    'unsupported-no-message': ({'code': 1003}, 'iKuai 不提供 sysstat 接口（错误码 1003）。当前固件可能不支持这项功能，请确认 iKuai 版本。', ('H0216', 'H0237')),
}


def reads():
    yield probes.probe('http-guard-healthy', 'ikuai', ok=True, entries=HELPERS,
        checks={'probe/version': '4.0.314', 'probe/capabilities': ['routes', 'nat_bypass', 'address_sets', 'policy_routes']})
    selected = {'R017/ikuai/' + label for label in LABELS}
    code_helpers = {'call-code-0': 'H0214', 'call-code-1': 'H0215', 'call-code-2': 'H0216',
                    'call-code-3': 'H0219', 'session-retry-still-expired': 'H0217'}
    for case in probes.ikuai():
        if case.ident not in selected:
            continue
        label = case.ident.removeprefix('R017/ikuai/')
        helpers = []
        if label in code_helpers:
            helpers.append(code_helpers[label])
        if label.startswith('http-'):
            helpers.append('H0213')
        if label.startswith('redirect-301-'):
            helpers.append('H0211' if label.endswith('other-origin') else 'H0212')
        if case.expected == 200:
            helpers += list(HELPERS)
        yield replace(case, entries=case.entries + tuple(helpers))
    for label, (body, message, helpers) in EXTRA.items():
        yield probes.probe('http-guard-' + label, 'ikuai',
            ({'action': 'fault', 'match': CALL, 'status': 200, 'body': body},), message=message, entries=helpers)
    yield probes.probe('http-guard-login-errmsg', 'ikuai',
        (probes.fault('ikuai', '/Action/login', status=200, body={'Result': 10001, 'ErrMsg': '合成旧式拒绝🧪'}),),
        message='iKuai 登录失败：合成旧式拒绝🧪', entries=('H0235',))
    yield probes.probe('http-guard-created-status-refused', 'ikuai',
        (probes.fault('ikuai', '/Action/login', status=201),),
        message='iKuai 请求未成功，请检查管理地址、端口和管理入口设置（HTTP 201）。', entries=('H0213', 'H0230'))


def tls():
    for case in probes.tls():
        if '/ikuai/' in case.ident:
            yield replace(case, entries=case.entries + (('H0220', 'H0221') if case.expected == 200 else ()))
