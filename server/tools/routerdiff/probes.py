"""Actual device HTTP failure/capability recipes; failed probes still commit."""
import base64

from cases import Case
from seed import DEVICE_PASSWORD, CURRENT_PROXY
from seed import router


PERMISSION = ('请用管理员登录 WinBox，在 System → Users 找到 ConfigFlow 使用的账号并确认 Group；'
              '在 Groups 中为该账号的专用用户组启用 read、write、api、rest-api，重点检查 write。'
              '保存后重新生成发布预览；连接测试成功不代表已有写入权限。')


def fault(peer, path, **values):
    return {'action': 'fault', 'match': {'peer': peer, 'path': path}, **values}


def probe(label, vendor, controls=(), message='', ok=False, checks=None, entries=(), stored_message=None,
          finalize=(), elapsed_min=0, expected_requests=()):
    ident, index = ('r-ros', 0) if vendor == 'routeros' else ('r-ik', 1)
    stored = {f'routers/items/{index}/last_probe/ok': ok}
    if not ok:
        stored[f'routers/items/{index}/last_probe/message'] = message if stored_message is None else stored_message
    return Case('R017/' + vendor + '/' + label, 'R017', 'POST', '/api/routers/' + ident + '/probe',
                200 if ok else 502, (('R017-S01' if vendor == 'routeros' else 'R017-S02',) if ok else ('R017-F12',)) + entries,
                prepare=controls, finalize=finalize, elapsed_min=elapsed_min, expected_requests=expected_requests,
                message=message, unchanged=False, effect='profile-write', stored=stored,
                checks={'success': ok, 'probe/ok': ok, **(checks or {})}, times=('probe/at',))


def tls_options():
    return {'fake_tls': True,
            'alpha': {'routers': {'items': [router(), router('r-ik', 'ikuai')]}},
            'beta': {'routers': {'items': [router()]}}}


def tls():
    for vendor, ident, port in [('routeros', 'r-ros', 18443), ('ikuai', 'r-ik', 18444)]:
        message = ('无法连接 RouterOS：SSLError' if vendor == 'routeros' else
                   'iKuai HTTPS 连接失败：请检查证书是否可信、是否过期，以及管理地址和 HTTPS 端口是否正确。')
        for index, verify in enumerate((True, False, True)):
            # Public settings edit clears stale last_probe and increments its
            # own revision before the actual probe's independent write baseline.
            label = 'self-signed-' + ('verified' if verify else 'unverified')
            preparation = ({'rpc': 'http', 'method': 'PUT', 'path': '/api/routers/' + ident,
                'document': {'endpoint': 'https://10.44.0.1:' + str(port), 'verify_tls': verify}, 'expected': 200},)
            yield probe(label + '-' + str(index), vendor, preparation,
                        message=message if verify else '', ok=not verify,
                        checks={'probe/version': '7.24.5 (stable)' if vendor == 'routeros' else '4.0.314'} if not verify else {},
                        entries=('H0220', 'H0221') if verify and vendor == 'ikuai' else
                                ('H0826', 'H0827') if verify else (),
                        expected_requests=({'match': {'peer': 'ros' if vendor == 'routeros' else 'ikuai'}, 'count': 0},)
                                          if verify else ())


def ipv6_options():
    return {**tls_options(), 'fake_ipv6': True}


def ipv6():
    for vendor, ident, http_port, tls_port in [('routeros', 'r-ros', 18081, 18443), ('ikuai', 'r-ik', 18082, 18444)]:
        for scheme, port in [('http', http_port), ('https', tls_port)]:
            preparation = ({'rpc': 'http', 'method': 'PUT', 'path': '/api/routers/' + ident,
                'document': {'endpoint': scheme + '://[fd42:cf::1]:' + str(port), 'verify_tls': False}, 'expected': 200},)
            yield probe('ipv6-' + scheme, vendor, preparation, ok=True,
                        checks={'probe/version': '7.24.5 (stable)' if vendor == 'routeros' else '4.0.314'})


def auth_disabled():
    for vendor in ('routeros', 'ikuai'):
        yield probe('authentication-disabled', vendor, ok=True)


def routeros():
    resource = '/rest/system/resource'
    for status, message, helpers in (
        (401, 'RouterOS 认证失败，请核对 ConfigFlow 中的用户名、密码和账号允许的来源地址；'
              '在 WinBox → System → Users / Groups 检查用户组的 api、rest-api 权限', ('H0828',)),
        (403, 'RouterOS 拒绝访问，请先确认所用 HTTP/HTTPS 服务允许 REST API。' + PERMISSION, ('H0829',)),
        (404, 'RouterOS 未提供 REST API（HTTPS 需要 7.1+ 并启用 www-ssl；HTTP 需要 7.9+ 并启用 www）', ('H0830',))):
        yield probe('http-' + str(status), 'routeros', (fault('ros', resource, status=status),), message, entries=helpers)
    for status in (300, 400, 405, 429, 500, 502, 503):
        yield probe('http-detail-' + str(status), 'routeros',
            (fault('ros', resource, status=status, body={'message': '合成拒绝', 'detail': '详情🧪'}),),
            f'RouterOS GET /system/resource 返回 HTTP {status}（合成拒绝: 详情🧪）', entries=('H0832',))
    yield probe('permissions-detail', 'routeros',
        (fault('ros', resource, status=500, body={'detail': 'not enough permissions (9)'}),),
        'RouterOS 账号权限不足。' + PERMISSION +
        ' 原始错误：RouterOS GET /system/resource 返回 HTTP 500（not enough permissions (9)）', entries=('H0832',))
    for status in (200, 500):
        yield probe('not-json-' + str(status), 'routeros',
            (fault('ros', resource, status=status, raw=base64.b64encode(b'<html>synthetic</html>').decode()),),
            'RouterOS 返回的不是 JSON' if status == 200 else 'RouterOS GET /system/resource 返回 HTTP 500',
            entries=('H0833', 'H0834') if status == 200 else ('H0831', 'H0832'))
    for index, value in enumerate(([], ['row'], None, False, 123, 'text')):
        yield probe('resource-shape-' + str(index), 'routeros', (fault('ros', resource, status=200, body=value),),
                    'RouterOS 系统信息格式无法识别', entries=('H0841',))
    for index, (value, shown) in enumerate((('', '未知'), (None, '未知'), (0, '未知'),
        ('bad', 'bad'), ('6.49', '6.49'), ('7.0.9', '7.0.9'), ('7', '7'), ('v7.24', 'v7.24'))):
        yield probe('old-version-' + str(index), 'routeros',
            (fault('ros', resource, status=200, body={'version': value}),),
            '需要 RouterOS 7.1+，当前版本 ' + shown, entries=('H0842',))
    yield probe('204-is-empty-resource', 'routeros', (fault('ros', resource, status=204, raw=''),),
                '需要 RouterOS 7.1+，当前版本 未知', entries=('H0842',))
    for index, version in enumerate(('7.1', '7.24.5（合成）', '8.0')):
        yield probe('version-success-' + str(index), 'routeros',
            (fault('ros', resource, status=200, body={'version': version, 'board-name': '合成型号🧪'}),), ok=True,
            checks={'probe/version': version, 'probe/model': '合成型号🧪',
                    'probe/capabilities': ['routes', 'nat_bypass', 'address_sets']})
    capabilities = [('/rest/ip/route', 'routes'), ('/rest/ip/firewall/nat', 'nat_bypass'),
                    ('/rest/ip/firewall/address-list?list=cf-probe', 'address_sets')]
    for path, absent in capabilities:
        for label, values in (('refused', {'status': 403}), ('bad-shape', {'status': 200, 'body': {}}),
                              ('bad-row', {'status': 200, 'body': [None]})):
            yield probe('capability-' + absent + '-' + label, 'routeros', (fault('ros', path, **values),), ok=True,
                checks={'probe/capabilities': [name for _, name in capabilities if name != absent]}, entries=('H0843',))
    yield probe('connection-closed', 'routeros', (fault('ros', resource, disconnect=True),),
                '无法连接 RouterOS：ConnectionError', entries=('H0826', 'H0827'))
    raw_message = f'RouterOS GET /system/resource 返回 HTTP 500（{DEVICE_PASSWORD}）'
    yield probe('device-password-echo-is-not-global-token', 'routeros',
                (fault('ros', resource, status=500, body={'detail': DEVICE_PASSWORD}),), raw_message, entries=('X01',))
    raw_message = f'RouterOS GET /system/resource 返回 HTTP 500（{CURRENT_PROXY}）'
    yield probe('global-token-whole-string-redacted', 'routeros',
                (fault('ros', resource, status=500, body={'detail': CURRENT_PROXY}),), '[REDACTED]',
                entries=('X01',), stored_message=raw_message)
    yield probe('real-read-timeout', 'routeros',
                (fault('ros', resource, gate='probe-read-timeout', status=200, body={'version': '7.24'}),),
                '无法连接 RouterOS：ReadTimeout', entries=('H0826', 'H0827'), elapsed_min=9,
                finalize=({'action': 'release', 'gate': 'probe-read-timeout'},))


def ikuai():
    login = '/Action/login'
    reasons = {401: '身份验证未通过，请检查用户名、密码，以及管理入口是否另有登录验证',
        403: '拒绝访问，请检查账号的管理员权限和管理页面的访问限制',
        404: '未找到管理接口，请检查管理地址和端口，并确认使用的是 iKuai 4.x',
        405: '管理接口不接受此请求，请检查管理地址，以及反向代理是否允许接口请求',
        408: '等待请求超时，请检查网络连接后重试', 429: '请求过于频繁，请稍后再试',
        502: '管理入口未能连接后端服务，请检查 iKuai 管理服务或反向代理',
        503: '管理服务暂时不可用，请稍后再试', 504: '管理入口等待后端响应超时，请检查 iKuai 管理服务或反向代理',
        418: '请求未成功，请检查管理地址、端口和管理入口设置',
        500: '管理服务发生错误，请确认浏览器能否正常打开管理页面，稍后重试'}
    for status, reason in reasons.items():
        yield probe('http-' + str(status), 'ikuai', (fault('ikuai', login, status=status),),
                    f'iKuai {reason}（HTTP {status}）。', entries=('H0230',))
    for status in (301, 302, 303, 307, 308):
        for label, location, message in (
            ('relative', '/Action/login?secret=synthetic', f'iKuai 将请求转到了其他页面（HTTP {status}）。'
             '请在浏览器打开管理页面，核对最终地址的协议和端口；若地址无误，请检查管理入口或反向代理的跳转设置。'),
            ('other-origin', 'https://user:synthetic@router.example:8443/private?secret=synthetic',
             f'iKuai 要求跳转到 https://router.example:8443（HTTP {status}）。'
             '请核对浏览器打开的管理地址，在「编辑路由器」中修改地址后重试。')):
            yield probe('redirect-' + str(status) + '-' + label, 'ikuai',
                (fault('ikuai', login, status=status, headers={'Location': location}),), message, entries=('H0230',),
                expected_requests=({'match': {'peer': 'ikuai', 'path': login}, 'count': 1},))
    for index, raw in enumerate((b'', b' \n\t', b'<html>synthetic</html>', b'{')):
        empty = not raw.strip()
        yield probe('not-json-' + str(index), 'ikuai',
            (fault('ikuai', login, status=200, raw=base64.b64encode(raw).decode()),),
            'iKuai 没有返回任何内容：请求可能过大或被 iKuai 拒绝' if empty else 'iKuai 返回的不是 JSON，请确认地址指向管理页面',
            entries=('H0231', 'H0232' if empty else 'H0233'))
    for index, value in enumerate(([], None, True, 1, 'text')):
        yield probe('login-shape-' + str(index), 'ikuai', (fault('ikuai', login, status=200, body=value),),
                    'iKuai 返回格式无法识别', entries=('H0234',))
    yield probe('result-success-code-refusal', 'ikuai',
        (fault('ikuai', login, status=200, body={'Result': 10000, 'code': 1005, 'message': '合成拒绝🧪'}),),
        'iKuai 登录失败：合成拒绝🧪（错误码 1005）', entries=('H0235',))
    yield probe('login-missing-code-old-result-refused', 'ikuai',
        (fault('ikuai', login, status=200, body={'Result': 10001}),), 'iKuai 登录失败：账号或密码错误', entries=('H0235',))
    sysstat = {'peer': 'ikuai', 'path': '/Action/call', '/body/func_name': 'sysstat'}
    for key in ('Data', 'Result'):
        yield probe('old-call-' + key, 'ikuai',
            ({'action': 'fault', 'match': sysstat, 'status': 200, 'body': {key: {}}},),
            '检测到 iKuai 3.x 的响应格式，只支持 iKuai 4.x', entries=('H0236',))
    for index, (code, label, detail, message) in enumerate((
        (1001, '合成功能', '', 'iKuai 账号没有「合成功能」（sysstat）的访问权限（错误码 1001）。请在 iKuai 的用户组中授予读取权限，或改用管理员账号。'),
        (1002, '', '', 'iKuai 账号没有 sysstat 的执行权限（错误码 1002）。请在 iKuai 的用户组中授予修改权限，或改用管理员账号。'),
        (1003, '', '合成不支持', 'iKuai 不提供 sysstat 接口（错误码 1003：合成不支持）。当前固件可能不支持这项功能，请确认 iKuai 版本。'),
        (3001, '', '合成参数错误', 'iKuai 调用 sysstat 失败：合成参数错误（错误码 3001）'))):
        yield probe('call-code-' + str(index), 'ikuai',
            ({'action': 'fault', 'match': sysstat, 'status': 200,
              'body': {'Result': 10000, 'code': code, 'Label': label, 'message': detail}},), message, entries=('H0237',))
    expired = {'action': 'fault', 'match': sysstat, 'status': 200, 'body': {'Result': 10000, 'code': 1008}}
    yield probe('session-relogin-once', 'ikuai', (expired,), ok=True,
                checks={'probe/capabilities': ['routes', 'nat_bypass', 'address_sets', 'policy_routes']},
                expected_requests=({'match': {'peer': 'ikuai', 'path': login}, 'count': 2},))
    yield probe('session-retry-still-expired', 'ikuai', (expired, expired),
                'iKuai 登录会话已失效（错误码 1008），重新登录后仍未恢复，请稍后重试。', entries=('H0237',),
                expected_requests=({'match': {'peer': 'ikuai', 'path': login}, 'count': 2},))
    for index, value in enumerate(('', None, '3.7.16', '5.0')):
        shown = value or '未知'
        yield probe('old-version-' + str(index), 'ikuai',
            ({'action': 'fault', 'match': sysstat, 'status': 200,
              'body': {'Result': 10000, 'code': 0, 'results': {'verinfo': {'version': value}}}},),
            '只支持 iKuai 4.x，当前版本 ' + shown, entries=('H0241',))
    capabilities = [('static_rt', 'routes'), ('nat_rule', 'nat_bypass'), ('route_object', 'address_sets'),
                    ('stream_ipport', 'policy_routes')]
    for func, absent in capabilities:
        yield probe('capability-' + func, 'ikuai',
            ({'action': 'fault', 'match': {'peer': 'ikuai', 'path': '/Action/call', '/body/func_name': func},
              'status': 200, 'body': {'Result': 10000, 'code': 1001}},), ok=True,
            checks={'probe/capabilities': [name for _, name in capabilities if name != absent]}, entries=('H0242',))
    yield probe('connection-closed', 'ikuai', (fault('ikuai', login, disconnect=True),),
                '无法连接 iKuai 管理服务：请检查地址、端口和域名解析，确认管理服务已开启，并允许 ConfigFlow 所在主机访问。',
                entries=('H0226', 'H0227'))
    yield probe('real-read-timeout', 'ikuai', (fault('ikuai', login, gate='ikuai-read-timeout'),),
                '等待 iKuai 响应超时（10 秒）：请检查网络连接和路由器负载，稍后重试。', entries=('H0224', 'H0225'),
                elapsed_min=9, finalize=({'action': 'release', 'gate': 'ikuai-read-timeout'},))
