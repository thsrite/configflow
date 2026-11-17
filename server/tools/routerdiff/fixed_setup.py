"""Fixed-IP setup guard with real session files and public setup HTTP reads.

The fixture is an archived synthetic storage record, not a mocked profile_sessions
function. Completion controls test the stored completed flag, not setup's device
installation or finish workflow (owned by the config domain).
"""
import base64
import copy
import json

from cases import Case, Ref
from clients import options as client_options, device_world, request, FOREIGN


SESSION = '0123456789abcdef0123456789abcdef'
PROFILE = 'setup_' + SESSION[:20]
FILE = 'setup-sessions/' + SESSION + '.json'
MESSAGE = '配置向导进行中，只允许命名；请完成向导后再固定或删除固定 IP。'


def session_record(state='active'):
    value = {'id': SESSION, 'profile_id': PROFILE, 'name': '合成配置向导🧪', 'goal': 'router',
             'step': 1, 'max_step': 1, 'created_at': '2026-01-01T00:00:00+00:00',
             'subscription_ids': [], 'node_ids': [], 'prepared': False,
             'profile_revision': 0, 'completed': False, 'updated_at': '2026-01-01T00:00:00+00:00'}
    if state == 'abandoned':
        value.update(abandoned=True, abandoned_at='2026-01-01T00:01:00+00:00')
    elif state == 'completed':
        value.update(completed=True, prepared=True, step=5, max_step=5,
                     router_mode='manual', router_verification='manual')
    elif state != 'active':
        raise ValueError('unknown synthetic setup state')
    return value


def options(state='active'):
    value = client_options()
    profile = {**copy.deepcopy(value['alpha']), 'name': '合成配置向导🧪'}
    value['additional_profiles'] = {PROFILE: profile}
    if state == 'foreign':
        # Its canonical session/profile ID remain paired; the selected profile
        # below is alpha. An active different profile must not block alpha.
        state = 'active'
    record = session_record(state)
    value['files'] = {FILE: base64.b64encode(json.dumps(record, ensure_ascii=False).encode()).decode()}
    value['file_modes'] = {FILE: 0o600}
    value['directory_modes'] = {'setup-sessions': 0o700}
    return value


def body(profile):
    return {**request('add', '10.44.0.21', '02:00:00:00:00:21', 'r-ros', '设备🧪'), 'profile_id': profile}


def worlds():
    for state in ('active', 'completed', 'abandoned', 'foreign'):
        profile = 'alpha' if state == 'foreign' else PROFILE
        def preview(state=state, profile=profile):
            active = state == 'active'
            yield Case('R006/setup-' + state, 'R006', 'POST',
                '/api/client-devices/fixed-ip/preview?profile=' + profile, 409 if active else 200,
                ('H0035', 'R006-F17'), profile=profile, document=body(profile),
                prepare=(device_world(),), unchanged=active, effect='' if active else 'device-read',
                message=MESSAGE if active else None,
                checks={} if active else {'success': True, 'actions/0/ip': '10.44.0.21'}, foreign=FOREIGN,
                expected_requests=({'match': {'peer': 'ros'}, 'count': 0},
                                   {'match': {'peer': 'ikuai'}, 'count': 0}) if active else ())
        yield 'fixed-setup-' + state, options(state), (preview,)
    yield 'fixed-setup-reopened', options('abandoned'), (reopened,)


def reopened():
    preview = 'R006/setup-before-reopen'
    yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview?profile=' + PROFILE, 200,
        ('H0035', 'R005-F14'), profile=PROFILE, document=body(PROFILE), prepare=(device_world(),),
        unchanged=False, effect='device-read', foreign=FOREIGN, checks={'success': True})
    yield Case('R005/setup-reopened-valid-token', 'R005', 'POST',
        '/api/client-devices/fixed-ip/apply?profile=' + PROFILE, 409, ('H0035', 'R005-F14'),
        profile=PROFILE, document={'profile_id': PROFILE, 'token': Ref(preview, '/body/token')},
        requires=('R006',), message=MESSAGE, foreign=FOREIGN,
        prepare=({'rpc': 'http', 'method': 'PATCH', 'path': '/api/setup/' + SESSION, 'document': {},
                  'expected': 200, 'checks': {'id': SESSION, 'profile_id': PROFILE, 'completed': False},
                  'absent': ('abandoned',)},),
        expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
