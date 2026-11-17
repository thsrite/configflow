"""Probe → persisted/list/workspace readback using each side's own HTTP data."""
from cases import Case, Ref
from probes import probe

QUIET = ({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0})


def roundtrip():
    initial = 'R014/probe-clock-workspace-before'
    yield Case(initial, 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200, ('R015-B04',))
    result = probe('clock-roundtrip', 'routeros', ok=True)
    yield result
    stamp = Ref(result.ident, '/body/probe/at')
    yield Case('R007/probe-clock-public-list', 'R007', 'GET', '/api/routers', 200, ('R017-S01',),
        requires=('R017',), checks={'routers/0/last_probe/at': stamp, 'routers/0/last_probe/ok': True},
        expected_requests=QUIET)
    yield Case('R014/probe-clock-does-not-change-editor-revision', 'R014', 'GET',
        '/api/routers/r-ros/policy-settings', 200, ('R015-B04',), requires=('R017',),
        checks={'workspace/router/last_probe/at': stamp, 'workspace/revision': Ref(initial, '/body/workspace/revision'),
                'workspace/names_revision': Ref(initial, '/body/workspace/names_revision')}, expected_requests=QUIET)
    yield Case('R010/probe-clock-name-edit-retains-last-probe', 'R010', 'PUT', '/api/routers/r-ros', 200,
        ('R010-B03',), document={'name': '探测后改名🧪'}, requires=('R017',), unchanged=False, effect='profile-write',
        checks={'router/last_probe/at': stamp, 'router/name': '探测后改名🧪'}, expected_requests=QUIET,
        retained_fields=('device_notes', 'client_fixed_ips', 'oxidns'))
    draft = {'revision': Ref(initial, '/body/workspace/revision'),
             'router': {'ip_rule_sets': [], 'ros_policy_rule_sets': [], 'proxy_gateway_agent_id': ''}, 'base_url': ''}
    yield Case('R015/probe-clock-name-edit-invalidates-old-editor', 'R015', 'PUT',
        '/api/routers/r-ros/policy-settings', 409, ('R015-F15',), document=draft, requires=('R014', 'R010', 'R017'),
        message='配置已在其他页面更新，请重新载入后再保存。', expected_requests=QUIET)
    refreshed = 'R014/probe-clock-workspace-after-name-edit'
    yield Case(refreshed, 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200, ('R014-S01',),
        requires=('R010', 'R017'), checks={'workspace/router/last_probe/at': stamp}, expected_requests=QUIET)
    yield Case('R015/probe-clock-modern-editor-retains-last-probe', 'R015', 'PUT',
        '/api/routers/r-ros/policy-settings', 200, ('R015-S01',), requires=('R014', 'R017'),
        document={**draft, 'revision': Ref(refreshed, '/body/workspace/revision')},
        unchanged=False, effect='profile-write', retained_fields=('oxidns', 'device_notes', 'client_fixed_ips'),
        checks={'workspace/router/last_probe/at': stamp}, expected_requests=QUIET)
    ikuai = probe('clock-roundtrip', 'ikuai', ok=True)
    yield ikuai
    yield Case('R007/probe-clock-both-vendors-public-list', 'R007', 'GET', '/api/routers', 200,
        ('R017-S01', 'R017-S02'), requires=('R017',),
        checks={'routers/0/last_probe/at': stamp, 'routers/1/last_probe/at': Ref(ikuai.ident, '/body/probe/at'),
                'routers/0/last_probe/ok': True, 'routers/1/last_probe/ok': True}, expected_requests=QUIET)


def invalidation_worlds(options):
    """Each edit starts from a real fresh probe, never a fabricated cached row."""
    changes = (('type', {'type': 'ikuai'}), ('endpoint', {'endpoint': 'http://10.44.0.1:18089'}),
               ('username', {'username': 'changed-admin'}), ('password', {'password': 'synthetic-changed-secret-0123456789'}),
               ('verify-tls', {'verify_tls': False}))
    retained = (('name', {'name': '保留连接的改名🧪'}), ('same-type', {'type': 'routeros'}),
                ('same-username', {'username': '  test-admin  '}),
                ('same-endpoint', {'endpoint': ' http://10.44.0.1:18081/ '}),
                ('same-tls', {'verify_tls': 0}), ('noop', {}),
                ('password-empty', {'password': ''}), ('password-null', {'password': None}),
                ('password-false', {'password': False}), ('password-zero', {'password': 0}),
                ('password-array', {'password': []}), ('password-object', {'password': {}}))
    for clear, variants in ((True, changes), (False, retained)):
        for label, body in variants:
            kind = 'clear-' if clear else 'retain-'
            def sequence(clear=clear, label=label, body=body, kind=kind):
                result = probe('edit-' + kind + label, 'routeros', ok=True, entries=('R010-B03',))
                yield result
                stamp = Ref(result.ident, '/body/probe/at')
                yield Case('R010/probe-' + kind + label, 'R010', 'PUT', '/api/routers/r-ros', 200,
                    ('R010-B03',), document=body, requires=('R017',), unchanged=False, effect='profile-write',
                    checks={'router/last_probe': None} if clear else {'router/last_probe/at': stamp},
                    stored_absent=('routers/items/0/last_probe',) if clear else (),
                    stored={} if clear else {'routers/items/0/last_probe/at': stamp},
                    retained_fields=('oxidns', 'device_notes', 'client_fixed_ips'), expected_requests=QUIET,
                    absent=('router/password',))
            yield 'probe-edit-' + kind + label, options, (sequence,)
