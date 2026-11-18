"""Untrusted redirect/body parsing over the actual requests HTTP boundary."""
import base64

from cases import Case
import probes


FALLBACK = ('iKuai 将请求转到了其他页面（HTTP 301）。请在浏览器打开管理页面，'
            '核对最终地址的协议和端口；若地址无误，请检查管理入口或反向代理的跳转设置。')
ONE_LOGIN = ({'match': {'peer': 'ikuai', 'path': '/Action/login'}, 'count': 1},)


def cases():
    yield probes.probe('redirect-parser-healthy', 'ikuai', ok=True, entries=('H0209', 'H0228', 'H0229'))
    for label, location in (('text-port', 'http://router.example:bad/private?synthetic=1'),
                            ('range-port', 'http://router.example:99999/private?synthetic=1')):
        yield probes.probe('redirect-parser-' + label, 'ikuai',
            (probes.fault('ikuai', '/Action/login', status=301, headers={'Location': location}),),
            message=FALLBACK, entries=('H0209',), expected_requests=ONE_LOGIN)
    yield probes.probe('invalid-gzip-body', 'ikuai',
        (probes.fault('ikuai', '/Action/login', status=200, headers={'Content-Encoding': 'gzip'},
                      raw=base64.b64encode(b'not a gzip frame').decode()),),
        message='与 iKuai 通信失败：请检查管理地址、端口和网络连接，稍后重试。',
        entries=('H0228', 'H0229'), expected_requests=ONE_LOGIN)
    yield Case('R017/ikuai/redirect-parser-invalid-bracket', 'R017', 'POST', '/api/routers/r-ik/probe', 500,
        ('R017-F12', 'X12'),
        prepare=(probes.fault('ikuai', '/Action/login', status=301,
                             headers={'Location': 'http://[invalid/private'}),),
        unchanged=False, effect='device-read', html_error=True,
        python_bug='IKUAI_PROBE_REDIRECT_PARSE_CRASH', expected_requests=ONE_LOGIN,
        console_patterns=(r'requests/sessions\.py', r'ValueError: Invalid IPv6 URL'))
    yield probes.probe('redirect-parser-recovery', 'ikuai', ok=True, entries=('X12',))
