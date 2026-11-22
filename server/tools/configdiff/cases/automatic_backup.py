"""T16/T17 real commit hooks, held uploads, coalescing, failure and recovery."""
import copy
from dataclasses import asdict
from datetime import datetime
import json
from pathlib import Path
import re
import threading
import time
from urllib.parse import unquote
from zoneinfo import ZoneInfo

from compare import database, database_diffs, file_diffs, differences, timestamp, Difference
from fakes import WebDAV
from harness import Pair, json_body, ROOT
from cases.discovery_reads import heartbeat
from cases.helpers import profile_policy
from cases.subscriptions import SUB, require
from cases.webdav import diagnostic_policy, receipt_policy
from cases.pull_logging import events

CONFIG = 'POST /api/backup/config'
DOMAIN = 'POST /api/server-domain'
NAME = re.compile(r'/backup/config_backup_([0-9]{8}_[0-9]{6}_[0-9]{6})\.json')


def document(directory):
    """Read-only SQL inversion through Python's pure storage-format reader."""
    import sys
    if str(ROOT) not in sys.path: sys.path.append(str(ROOT))
    from backend.common.config_store import build_document
    value = database(directory)
    return build_document(value['meta']['root_order'], {tuple(row[:2]): row[2] for row in value['sections']},
        {tuple(key.split('/', 2)): (row['position'], row['value']) for key, row in value['fields'].items()})


def puts(snapshot):
    return [row for row in snapshot['requests'] if row['method'] == 'PUT']


def receipt(directory):
    path = directory / 'task-status.json'
    return json.loads(path.read_text()).get('webdav_backup', {}) if path.exists() else {}


def wait_for(index, predicate, description):
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        if predicate(): return True
        time.sleep(.03)
    if index == 0: require(False, description)
    return False


def protocol_errors(snapshot, expected_documents, basic, policy):
    errors = []
    rows = puts(snapshot)
    paths = [row['path'] for row in rows]
    if len(set(paths)) != len(paths):
        errors.append(Difference('automatic-backup.filename', 'reused-filename', 'one unique filename per upload', paths))
    errors += differences(len(expected_documents), len(rows), 'automatic-backup.upload-count')
    for index, (row, expected) in enumerate(zip(rows, expected_documents)):
        path = unquote(row['path'])
        match = NAME.fullmatch(path)
        valid = False
        if match:
            try:
                stamp = datetime.strptime(match[1], '%Y%m%d_%H%M%S_%f').replace(tzinfo=ZoneInfo(policy.tz)).timestamp()
                valid = policy.start - 2 <= stamp <= policy.end + 2
            except (ValueError, OverflowError):
                pass
        if not valid:
            errors.append(Difference('automatic-backup.filename', 'invalid-current-filename', 'allocated dated backup', path))
        errors += differences(expected, row['body'], f'automatic-backup.snapshot[{index}]')
    for index, row in enumerate(snapshot['requests']):
        errors += differences(basic, row['authorization'], f'automatic-backup.auth[{index}]')
    return errors


def snapshots_diffs(pair, snapshots, expected_documents, fakes):
    """Keep complete wire/object data; validate identities before mapping them."""
    policy = pair.policy
    policy.end = time.time()
    errors = []
    proofs = []
    for index in range(2):
        violations = protocol_errors(snapshots[index], expected_documents[index], fakes[index].basic, policy)
        if index == 0:
            require(not violations, 'automatic backup sends complete exact SQL snapshots and authenticated bounded filenames: '
                + repr([(row.path, row.kind) for row in violations[:5]]))
        proofs.append([asdict(row) for row in violations])
    left, right = [puts(snapshot) for snapshot in snapshots]
    if len(left) == len(right):
        for a, b in zip(left, right):
            pa, pb = unquote(a['path']), unquote(b['path'])
            if NAME.fullmatch(pa) and NAME.fullmatch(pb):
                if (pa in policy.identities and policy.identities[pa] != pb
                        or pb in policy.reverse and policy.reverse[pb] != pa):
                    errors.append(Difference('automatic-backup.filename', 'non-injective-upload-path', pa, pb))
                    continue
                policy.bind(pa, pb, NAME.pattern, 'automatic-backup-upload-path')
                if (a['path'], b['path']) != (pa, pb):
                    policy.bind(a['path'], b['path'], r'/backup/config_backup_[0-9_]+\.json', 'automatic-backup-encoded-path')
    for i, row in enumerate(snapshots[0]['requests']):
        if row['method'] == 'PUT':
            policy.times.add(f'automatic-backup.wire.requests[{i}].body.profiles.default.updated_at')
    for path in snapshots[0]['objects']:
        policy.times.add('automatic-backup.wire.objects.' + path + '.profiles.default.updated_at')
    receipt_policy(pair, policy)
    errors += differences(*proofs, 'automatic-backup.complete-snapshot-proofs', policy)
    errors += differences(*snapshots, 'automatic-backup.wire', policy)
    return errors + database_diffs(*pair.dirs, policy) + file_diffs(*pair.dirs, policy)


def settings(fake):
    return {'webdav_url': fake.url, 'webdav_username': fake.username, 'webdav_password': fake.password,
        'webdav_path': '/backup/', 'auto_backup': True, 'keep_latest': 0, 'keep_daily': 0}


def prepare(pair, fakes):
    pair.policy = profile_policy(pair)
    pair.policy.bind(fakes[0].url, fakes[1].url, r'http://127\.0\.0\.1:[0-9]+', 'automatic-backup-DAV-endpoint')


def coalesced(binary, recorder):
    with WebDAV() as left, WebDAV() as right, Pair(binary, recorder, options={
            'shared': {'subscriptions': [{**SUB, 'enabled': False}]}, 'agent_updates': {'enabled': False}}) as pair:
        fakes = left, right
        prepare(pair, fakes)
        gates = [(threading.Event(), threading.Event()) for _ in fakes]
        gate_errors = [[], []]
        for index, fake in enumerate(fakes):
            def hold(method, path, selected=index):
                if method == 'PUT' and not gates[selected][0].is_set():
                    gates[selected][0].set()
                    if not gates[selected][1].wait(30): gate_errors[selected].append('held upload timed out')
            fake.before_reply = hold
        initial = []
        try:
            trigger = pair.request('T16', CONFIG, body='{}', bodies=[json_body(settings(fake)) for fake in fakes],
                expect=200, writes=True, files=False, variant='held-first-event-upload/enable')
            entered = [gate[0].wait(10) for gate in gates]
            require(entered[0], 'real Python automatic worker reached its first owned DAV upload')
            initial = [document(directory) for directory in pair.dirs]
            pair.request('T16', DOMAIN, body=json_body({'new_domain': 'http://pending-backup.example'}),
                expect=200, writes=True, files=False, variant='pending-upload/system-commit')
            pair.request('T16', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                body=json_body({**SUB, 'name': '待上传最终订阅🧪', 'enabled': False}), expect=200, writes=True, files=False,
                variant='pending-upload/shared-commit')
            previous = document(pair.dirs[0])['profiles']['default']['domain_discovery'].get('auto_apply_min_confidence')
            confidence = 80 if previous == 95 else 95
            pair.request('T16', 'PUT /api/domain-discovery/settings', body=json_body({'auto_apply_min_confidence': confidence}),
                expect=200, writes=True, files=False, variant='pending-upload/profile-commit')
            final = [document(directory) for directory in pair.dirs]
            require(final[0]['system']['system_config']['server_domain'] == 'http://pending-backup.example'
                and final[0]['shared']['subscriptions'][0]['name'] == '待上传最终订阅🧪'
                and final[0]['profiles']['default']['domain_discovery']['auto_apply_min_confidence'] == confidence,
                'three actual scope commits remain durable while the first upload is blocked')
            held_counts = [len(puts(fake.snapshot())) for fake in fakes]
            require(held_counts[0] == 1, 'busy Python worker queues a reference rather than spawning uploads per save')
        finally:
            for _entered, release in gates: release.set()
        completed = [wait_for(i, lambda fake=fake, directory=directory: len(puts(fake.snapshot())) >= 2
            and receipt(directory).get('run_count') == 2 and receipt(directory).get('last_status') == 'success',
            'first snapshot and exactly one final coalesced snapshot complete')
            for i, (fake, directory) in enumerate(zip(fakes, pair.dirs))]
        snapshots = [fake.snapshot() for fake in fakes]
        expected = [[initial[i], final[i]] for i in range(2)]
        diffs = snapshots_diffs(pair, snapshots, expected, fakes)
        diffs += differences(*gate_errors, 'automatic-backup.gate-errors')
        diffs += differences(*entered, 'automatic-backup.first-upload-entered')
        diffs += differences(*held_counts, 'automatic-backup.only-one-upload-while-held')
        diffs += differences(*completed, 'automatic-backup.completed-two-snapshots')
        recorder.record('T16', 'held-upload-coalesces-system-shared-profile-commits-to-final-snapshot', CONFIG, 'T16',
            'different' if diffs else 'compared', diffs, python_status=None, go_status=None,
            trigger_http_statuses=[reply.status for reply in trigger])
        before = [fake.snapshot() for fake in fakes]
        pair.request('T16', DOMAIN, body=json_body({'new_domain': 'http://pending-backup.example'}), expect=200,
            writes=True, variant='no-op-does-not-schedule')
        pair.request('T16', DOMAIN, body=json_body({'new_domain': ''}), expect=400, writes=True,
            variant='rejected-transaction-does-not-schedule')
        heartbeat(pair, version='1.9.3-go', variant='T16/runtime-only-must-not-schedule')
        time.sleep(3)
        quiet = [fake.snapshot() for fake in fakes]
        proof = [differences(a, b, 'automatic-backup.no-op-failure-runtime-only') for a, b in zip(before, quiet)]
        require(not proof[0], 'no-op, rejection and runtime-only Agent heartbeat do not schedule another automatic upload')
        diffs = proof[1] + database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        recorder.record('T16', 'no-op-rejected-write-and-runtime-heartbeat-create-no-upload', CONFIG, 'T16',
            'different' if diffs else 'compared', diffs, python_status=None, go_status=None)
        pair.request('T16', CONFIG, body=json_body({'auto_backup': False}), expect=200, writes=True,
            variant='disable-prevents-later-upload')
        pair.request('T16', DOMAIN, body=json_body({'new_domain': 'http://disabled-backup.example'}),
            expect=200, writes=True, variant='core-commit-while-disabled-creates-no-upload')
        time.sleep(3)
        disabled = [differences(a, fake.snapshot(), 'automatic-backup.disabled') for a, fake in zip(quiet, fakes)]
        require(not disabled[0] and document(pair.dirs[0])['system']['backup']['auto_backup'] is False,
            'disabling persists and schedules no further upload')
        diffs = disabled[1] + database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        recorder.record('T16', 'disabled-setting-schedules-no-upload', CONFIG, 'T16',
            'different' if diffs else 'compared', diffs, python_status=None, go_status=None)


def failed_then_recovered(binary, recorder):
    with WebDAV() as left, WebDAV() as right, Pair(binary, recorder) as pair:
        fakes = left, right
        prepare(pair, fakes)
        for fake in fakes: fake.reset(failures={'PUT': 507})
        trigger = pair.request('T17', CONFIG, body='{}', bodies=[json_body(settings(fake)) for fake in fakes],
            expect=200, writes=True, files=False, variant='upload-failure-never-rolls-back-successful-core-commit')
        expected_failed = [document(directory) for directory in pair.dirs]
        failed = [wait_for(i, lambda directory=directory: receipt(directory).get('last_status') == 'failed',
            'failed background uploads expose their actual failed receipt') for i, directory in enumerate(pair.dirs)]
        first = [fake.snapshot() for fake in fakes]
        diagnostic_policy(pair, 'space', stored=True)
        diffs = snapshots_diffs(pair, first, [[doc] for doc in expected_failed], fakes)
        require(expected_failed[0]['system']['backup']['auto_backup']
            and receipt(pair.dirs[0]).get('run_count') == 1 and receipt(pair.dirs[0]).get('last_trigger') == 'event',
            'backup failure preserves enabled settings and one event failure, without reporting a failed HTTP commit')
        time.sleep(3)
        stable = [fake.snapshot() for fake in fakes]
        no_retry = [differences(a, b, 'automatic-backup.failure-no-unbounded-retry') for a, b in zip(first, stable)]
        require(not no_retry[0], 'failure is not retried until a fresh real configuration commit')
        diffs += no_retry[1]
        diffs += differences(*failed, 'automatic-backup.actual-failure-receipt')
        warning = 'WebDAV 自动备份失败（'
        logs = [events((directory / 'app.log').read_bytes(), warning) for directory in pair.dirs]
        require(len(logs[0]) == 1 and logs[0][0]['level'] == 'WARNING'
            and all(fake.password.encode() not in (pair.dirs[0] / 'app.log').read_bytes() for fake in fakes),
            'actual automatic failure logs one warning without the DAV password')
        pair.policy.internal_error_values['automatic-backup.failure-logs[0].message'] = (
            'WebDAV 自动备份失败（NotEnoughSpace）；已保存的配置不受影响',
            'WebDAV 自动备份失败（HTTP 507）；已保存的配置不受影响')
        diffs += differences(*logs, 'automatic-backup.failure-logs', pair.policy)
        leaked = [fake.password.encode() in (directory / 'app.log').read_bytes() for fake, directory in zip(fakes, pair.dirs)]
        diffs += differences(*leaked, 'automatic-backup.password-leaked-to-log')
        recorder.record('T17', 'background-507-keeps-core-commit-and-does-not-retry', CONFIG, 'T17',
            'different' if diffs else 'compared', diffs, python_status=None, go_status=None,
            trigger_http_statuses=[reply.status for reply in trigger])
        for fake in fakes: fake.reset()
        pair.request('T17', DOMAIN, body=json_body({'new_domain': 'http://recovered-backup.example'}),
            expect=200, writes=True, files=False, variant='later-core-commit-restarts-automatic-worker')
        expected_success = [document(directory) for directory in pair.dirs]
        recovered = [wait_for(i, lambda directory=directory: receipt(directory).get('last_status') == 'success'
            and receipt(directory).get('run_count') == 2, 'later commit completes a new successful event upload')
            for i, directory in enumerate(pair.dirs)]
        diffs = snapshots_diffs(pair, [fake.snapshot() for fake in fakes], [[doc] for doc in expected_success], fakes)
        require(receipt(pair.dirs[0]).get('failure_count') == 1 and receipt(pair.dirs[0]).get('consecutive_failures') == 0,
            'success retains total failures while clearing consecutive failures')
        diffs += differences(*recovered, 'automatic-backup.actual-recovery-receipt')
        recorder.record('T17', 'fresh-commit-recovers-background-upload-with-exact-final-snapshot', DOMAIN, 'T17',
            'different' if diffs else 'compared', diffs, python_status=None, go_status=None)


GROUPS = {'automatic-backup-real-held-upload-coalescing-and-no-op': coalesced,
          'automatic-backup-real-failure-no-retry-and-new-commit-recovery': failed_then_recovered}
