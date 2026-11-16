"""Notification body guards and version-aware read receipts at the file boundary."""
import copy
from datetime import datetime, timezone
import json

from harness import Pair, DesignError, json_body


def validation(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.policy.times.add('body.checked_at')
        pair.request('R104-S01', 'GET /api/notifications', expect=200, writes=True)
        for case, body in [('S01', {'versions': {}}), ('S03', {'versions': {'unknown-synthetic': 'unknown-version'}}),
                           ('B01', {'versions': {'id-' + str(i): 'v' for i in range(2000)}}),
                           ('B02', {'versions': {'x' * 1024: 'v'}}), ('B03', {'versions': {'id': 'v' * 100}})]:
            pair.request('R105-' + case, 'POST /api/notifications/read', body=json_body(body), expect=200, writes=True)
        for case, body in [('F08', {}), ('F09', {'versions': []}),
                           ('F10', {'versions': {'id-' + str(i): 'v' for i in range(2001)}}),
                           ('F11', {'versions': {'x' * 1025: 'v'}}), ('F12', {'versions': {'id': 1}}),
                           ('F13', {'versions': {'id': 'v' * 101}})]:
            pair.request('R105-' + case, 'POST /api/notifications/read', body=json_body(body), expect=400, writes=True)
    for case in ('F08', 'F09'):
        with Pair(binary, recorder) as pair:
            for directory in pair.dirs:
                if case == 'F08':
                    (directory / 'notifications.json').mkdir()
                else:
                    (directory / 'task-status.json').write_text(json.dumps({'webdav_backup': {'run_count': 'synthetic-invalid'}}))
            pair.request('R104-' + case, 'GET /api/notifications', expect=503, writes=True)


def read_versions(binary, recorder):
    with Pair(binary, recorder, options={'x01_location': 'notifications', 'x01_payload': '合成提醒 中文🧪'}) as pair:
        pair.policy.times.add('body.checked_at')
        result = pair.request('R104-S02', 'GET /api/notifications', expect=200, writes=True)
        initial = json.loads((pair.dirs[0] / 'notifications.json').read_text())
        key = 'notice:synthetic-x01'
        original = initial['items'][key]
        version = original['version']
        pair.request('R104-B02', 'GET /api/notifications', expect=200, writes=True)
        pair.request('R105-S02', 'POST /api/notifications/read',
                     body=json_body({'versions': {key: version}}), expect=200, writes=True)
        # A new stored incident version stands for the producer's next receipt;
        # the producer itself is covered by T27 rather than reimplemented here.
        stamp = datetime.now(timezone.utc).isoformat(timespec='microseconds')
        for directory in pair.dirs:
            path = directory / 'notifications.json'
            stored = json.loads(path.read_text()) if path.exists() else {'items': {}}
            stored['items'][key] = {**original, 'version': stamp, 'read': False, 'state': 'pending', 'message': '新版本🧪'}
            path.write_text(json.dumps(stored, ensure_ascii=False))
        pair.request('R104-B01', 'GET /api/notifications', expect=200, writes=True,
                     check=lambda r: require(any(v.get('version') == stamp for v in r.json()['items']), 'new incident missing'))
        pair.request('R105-S03', 'POST /api/notifications/read', body=json_body({'versions': {key: version}}),
                     variant='stale-known-version', expect=200, writes=True)
        pair.request('R104-B01', 'GET /api/notifications', expect=200, writes=True, variant='stale-read-does-not-dismiss',
                     check=lambda r: require(any(v.get('version') == stamp for v in r.json()['items']), 'stale read dismissed new version'))


def require(value, message):
    if not value:
        raise DesignError(message)


GROUPS = {'notifications-validation': validation, 'notifications-versions': read_versions}
