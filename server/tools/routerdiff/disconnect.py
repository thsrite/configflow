"""Close real panel TCP clients only after the backend reaches a device gate."""
from cases import Case, Ref
import clients


def controls(ident, method, path, document, match):
    return ({'action': 'fault', 'match': match, 'gate': ident, 'apply': True, 'passthrough': True},
            {'rpc': 'open_request', 'ident': ident, 'method': method, 'path': path, 'document': document},
            {'action': 'wait', 'gate': ident}, {'rpc': 'abandon', 'ident': ident},
            {'rpc': 'assert_http', 'method': 'PUT', 'path': '/api/routers/r-ros', 'document': [], 'expected': 409},
            {'action': 'release', 'gate': ident}, {'rpc': 'wait_guard', 'path': '/api/routers/r-ros'})


def release():
    preview = 'R012/before-client-abandon'
    yield Case(preview, 'R012', 'POST', '/api/routers/r-ros/plan', 200, ('X10',),
               unchanged=False, effect='device-read', checks={'plan/blockers': []}, formats={'plan/fingerprint': r'[0-9a-f]{16}'})
    yield Case('R011/abandoned-client-still-saves-publication', 'R007', 'GET', '/api/routers', 200, ('X10', 'T09'),
               requires=('R010', 'R011', 'R012', 'R022'), unchanged=False, effect='profile-write-device', capture_before_prepare=True,
               prepare=controls('release-write', 'POST', '/api/routers/r-ros/apply',
                    {'fingerprint': Ref(preview, '/body/plan/fingerprint')},
                    {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/route'}),
               wait_until={'routers/0/last_release/ok': True},
               after_poll=({'path': '/api/routers/r-ros/rule-sync', 'expect': {
                   'sync/message': '本次发布未建立自动同步基准：没有选择需要同步的 IP 规则集'}},),
               checks={'routers/0/last_release/ok': True}, stored={'routers/items/0/last_release/ok': True})


def fixed():
    preview = 'R006/before-client-abandon'
    yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200, ('R005-B91',),
               document=clients.request('add', '10.44.0.22', '02:00:00:00:00:22', 'r-ros', '断连后仍保存🧪'),
               prepare=(clients.device_world(),), unchanged=False, effect='device-read')
    yield Case('R005/abandoned-client-still-saves-binding', 'R002', 'GET', '/api/client-devices', 200,
               ('R005-B91', 'X10'), requires=('R005', 'R006', 'R010'), unchanged=False, effect='profile-write-device',
               capture_before_prepare=True,
               prepare=controls('fixed-write', 'POST', '/api/client-devices/fixed-ip/apply',
                    {'profile_id': 'alpha', 'token': Ref(preview, '/body/token')},
                    {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/dhcp-server/lease'}),
               wait_until={'devices/1/fixed_ip/mac': '02:00:00:00:00:22'},
               stored={'client_fixed_ips/10.44.0.22/mac': '02:00:00:00:00:22'},
               fake_lengths={'ros/~1ip~1dhcp-server~1lease': 3}, foreign=clients.FOREIGN)


def manual():
    values = {'cidr': '10.55.0.0/24', 'gateway': '10.44.0.2', 'enabled': True, 'comment': '断连手动路由🧪'}
    preview = 'R021/before-client-abandon'
    yield Case(preview, 'R021', 'POST', '/api/routers/r-ros/routes/plan', 200, ('X10',),
               document={'action': 'add', 'values': values}, unchanged=False, effect='device-read',
               checks={'plan/empty': False}, formats={'plan/fingerprint': r'[0-9a-f]{64}'})
    yield Case('R020/abandoned-client-still-adds-route', 'R019', 'GET', '/api/routers/r-ros/routes', 200, ('X10',),
               requires=('R010', 'R013', 'R020', 'R021'), unchanged=False, effect='device-only', capture_before_prepare=True,
               prepare=controls('manual-write', 'POST', '/api/routers/r-ros/routes/apply',
                    {'action': 'add', 'values': values, 'fingerprint': Ref(preview, '/body/plan/fingerprint')},
                    {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/route'}),
               wait_until={'routes/0/cidr': values['cidr']}, checks={'routes/0/comment': values['comment']},
               after_poll=({'path': '/api/routers/r-ros/policy', 'expect': {'success': True}},),
               fake_lengths={'ros/~1ip~1route': 1})
