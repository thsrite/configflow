"""Real filesystem failure effects through ordinary public requests."""
from cases import Case, Ref
from fixtures import receipt_file
import releases


def readonly_store():
    body = {'profile_id': 'alpha', 'ip': '10.44.0.21', 'name': '只读库不得保存🧪', 'mode': '',
            'previous_name': '', 'previous_mode': '', 'base_url': ''}
    yield Case('R003/read-only-DATA_DIR', 'R003', 'PUT', '/api/client-devices', 500, ('T21',),
        document=body, capture_before_prepare=True, html_error=True,
        prepare=({'rpc': 'filesystem_fault', 'action': 'readonly', 'enabled': True},),
        finalize=({'rpc': 'filesystem_fault', 'action': 'readonly', 'enabled': False},),
        expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
    yield Case('R002/read-only-write-not-persisted', 'R002', 'GET', '/api/client-devices', 200,
        ('T21',), checks={'devices': []})
    yield Case('R003/writable-restored', 'R003', 'PUT', '/api/client-devices', 200, ('T21',), document=body,
        unchanged=False, effect='profile-write', checks={'devices': [{'ip': '10.44.0.21', 'name': body['name'], 'mode': ''}]})


def receipt_rename_failure():
    preview = 'R012/receipt-rename-failure'
    yield Case(preview, 'R012', 'POST', '/api/routers/r-ros/plan', 200, ('T13',),
        unchanged=False, effect='device-read', checks={'plan/blockers': [], 'plan/empty': False})
    relative = receipt_file('r-ros')
    yield Case('R011/receipt-rename-failure', 'R011', 'POST', '/api/routers/r-ros/apply', 200,
        ('R011-F21', 'T13', 'T21'), requires=('R012',), document={'fingerprint': Ref(preview, '/body/plan/fingerprint')},
        prepare=({'rpc': 'filesystem_fault', 'action': 'receipt-directory', 'path': relative},),
        finalize=({'rpc': 'filesystem_fault', 'action': 'remove-receipt-directory', 'path': relative},),
        unchanged=False, capture_before_prepare=True, effect='profile-write-device',
        checks={'success': True, 'result/ok': True, 'result/rollback': []},
        stored={'routers/items/0/last_release/ok': True}, fake_lengths={'ros/~1ip~1route': 1},
        console_patterns=(r'Could not record remote rule synchronization baseline', r'IsADirectoryError'))
    yield Case('R022/failed-receipt-is-not-a-baseline', 'R022', 'GET', '/api/routers/r-ros/rule-sync', 200,
        ('R011-F21', 'T21'), checks={'sync/status': 'waiting', 'sync/ready': False})


def busy_options():
    value = releases.options()
    # A documented application setting, equal on both normal public processes.
    # This exercises SQLite's real busy timeout without a five-minute fixture wait.
    value['environment'] = {'CONFIGFLOW_LOCK_TIMEOUT_SECONDS': '0.25'}
    return value


def sqlite_busy():
    body = {'profile_id': 'alpha', 'ip': '10.44.0.21', 'name': 'SQLite writer lock test', 'mode': '',
            'previous_name': '', 'previous_mode': '', 'base_url': ''}
    yield Case('R003/SQLite-external-writer-busy', 'R003', 'PUT', '/api/client-devices', 500,
        ('T21',), document=body, capture_before_prepare=True, html_error=True,
        prepare=({'rpc': 'filesystem_fault', 'action': 'sqlite-busy', 'enabled': True},),
        finalize=({'rpc': 'filesystem_fault', 'action': 'sqlite-busy', 'enabled': False},),
        console_patterns=(r'sqlite3.OperationalError: database is locked',))
    yield Case('R002/SQLite-busy-draft-not-persisted', 'R002', 'GET', '/api/client-devices', 200,
        ('T21',), checks={'devices': []})
    yield Case('R003/SQLite-writer-released', 'R003', 'PUT', '/api/client-devices', 200,
        ('T21',), document=body, unchanged=False, effect='profile-write',
        checks={'devices': [{'ip': '10.44.0.21', 'name': body['name'], 'mode': ''}]})


def sqlite_publication_busy():
    preview = 'R012/before-SQLite-publication-busy'
    yield Case(preview, 'R012', 'POST', '/api/routers/r-ros/plan', 200, ('T21',),
        unchanged=False, effect='device-read', checks={'plan/blockers': [], 'plan/empty': False})
    yield Case('R011/device-writes-before-SQLite-publication-failure', 'R011', 'POST', '/api/routers/r-ros/apply', 500,
        ('T13', 'T21'), requires=('R012',), document={'fingerprint': Ref(preview, '/body/plan/fingerprint')},
        prepare=({'rpc': 'filesystem_fault', 'action': 'sqlite-busy', 'enabled': True},),
        finalize=({'rpc': 'filesystem_fault', 'action': 'sqlite-busy', 'enabled': False},),
        unchanged=False, capture_before_prepare=True, html_error=True, effect='device-only',
        fake_lengths={'ros/~1ip~1route': 1}, stored_absent=('routers/items/0/last_release',),
        console_patterns=(r'sqlite3.OperationalError: database is locked',))
    yield Case('R022/SQLite-save-failed-no-sync-baseline', 'R022', 'GET', '/api/routers/r-ros/rule-sync', 200,
        ('T21',), checks={'sync/ready': False})
