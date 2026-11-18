"""Read errors versus uncertain writes through public RouterOS workflows."""
import base64
from dataclasses import replace

from cases import Case, Ref
import clients
import probes


PUT = {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/dhcp-server/lease'}
DELETE = {'peer': 'ros', 'method': 'DELETE'}
READ_LABELS = ('http-401', 'http-403', 'http-404', 'http-detail-400', 'http-detail-500',
               'permissions-detail', 'not-json-200', 'not-json-500', 'connection-closed',
               'real-read-timeout', 'version-success-0')
HELPERS = tuple('H' + str(number).zfill(4) for number in range(826, 835))


def reads():
    selected = {'R017/routeros/' + label for label in READ_LABELS}
    for case in probes.routeros():
        if case.ident in selected:
            yield replace(case, entries=case.entries + HELPERS) if case.expected == 200 else case


def normal():
    # Reuse the existing full public fixed-IP workflow's independent new lease
    # control. Its initial seed must be explicit when selecting only this pair.
    for case in clients.fixed_ip():
        if case.ident in ('R006/fixed-add-new', 'R005/fixed-add-new'):
            yield replace(case, prepare=(clients.device_world(),) if case.route == 'R006' else case.prepare,
                          entries=case.entries + HELPERS)


def vectors():
    raw = base64.b64encode(b'<html>synthetic device response</html>').decode()
    yield 'not-json-success', {'status': 200, 'raw': raw, 'apply': True}, 'RouterOS 返回的不是 JSON', ('H0833', 'H0834')
    yield 'not-json-server-error', {'status': 500, 'raw': raw, 'apply': True}, 'RouterOS PUT /ip/dhcp-server/lease 返回 HTTP 500', ('H0831', 'H0832')
    yield 'client-error-array', {'status': 400, 'body': []}, 'RouterOS PUT /ip/dhcp-server/lease 返回 HTTP 400', ('H0832',)
    yield 'server-error-detail', {'status': 503, 'body': {'message': '合成写入失败', 'detail': '详情🧪'}, 'apply': True}, 'RouterOS PUT /ip/dhcp-server/lease 返回 HTTP 503（合成写入失败: 详情🧪）', ('H0832',)
    yield 'permission-detail', {'status': 500, 'body': {'detail': 'not enough permissions (9)'}, 'apply': True}, (
        'RouterOS 账号权限不足。' + probes.PERMISSION + ' 原始错误：RouterOS PUT /ip/dhcp-server/lease 返回 HTTP 500（not enough permissions (9)）'), ('H0832',)
    yield 'unauthorized', {'status': 401}, ('RouterOS 认证失败，请核对 ConfigFlow 中的用户名、密码和账号允许的来源地址；'
        '在 WinBox → System → Users / Groups 检查用户组的 api、rest-api 权限'), ('H0828',)
    yield 'forbidden', {'status': 403}, 'RouterOS 拒绝访问，请先确认所用 HTTP/HTTPS 服务允许 REST API。' + probes.PERMISSION, ('H0829',)
    yield 'missing-write', {'status': 404}, 'RouterOS PUT /ip/dhcp-server/lease 返回 HTTP 404（synthetic device refusal）', ('H0832',)
    yield 'closed-after-write', {'disconnect': True, 'apply': True}, '无法连接 RouterOS：ConnectionError', ('H0826', 'H0827')


def writes():
    for label, fault, message, entries in vectors():
        preview = 'R006/ros-http-' + label
        yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200,
            document=clients.request('add', '10.44.0.22', '02:00:00:00:00:22', 'r-ros', '写入边界🧪'),
            prepare=(clients.device_world(),), unchanged=False, effect='device-read', foreign=clients.FOREIGN,
            checks={'success': True, 'actions/0/ip': '10.44.0.22'})
        yield Case('R005/ros-http-' + label, 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 502, entries,
            document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
            prepare=({'action': 'fault', 'match': PUT, **fault},), unchanged=False, effect='device-only',
            message=message, checks={'success': False}, stored={'client_fixed_ips': {}}, foreign=clients.FOREIGN,
            fake_lengths={'ros/~1ip~1dhcp-server~1lease': 2, 'ikuai/dhcp_static': 1},
            expected_requests=({'match': PUT, 'count': 1}, {'match': DELETE, 'count': int(fault.get('apply', False))}),
            request_order=(PUT, DELETE) if fault.get('apply') else (PUT,))
