#!/usr/bin/env python3
"""Generate review.json (review C02): task_status.record on hand-edited counters and
parse_time's UTC offset. Synthetic values only. Run on the test server from the repo root:

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/taskstatus/testdata/gen_review.py
"""
import json
import sys
import tempfile
from pathlib import Path
from unittest import mock

assert sys.version_info[:2] == (3, 11)
from backend.utils import task_status  # noqa: E402

COUNTS = ['4', ' 7 ', '０３', '١٢', '1_0', '', '0', '-2', 'x', '1.5', '4.0', None, True, False, 0, 3, 2.9, -1.5, 1e20,
          12345678901234567890123, [], {}, [1], {'a': 1}, float('inf'), float('nan'), '　5　', '9' * 30]
TIMES = ['2026-11-14T12:00:00Z', '2026-11-14T12:00:00+00:00', '2026-11-14T12:00:00-00:00', '2026-11-14T20:00:00+08:00',
         '2026-11-14T06:30:00-05:30', '2026-11-14T12:00:00', '2026-11-14', '2026-11-14T12:00:00.5+05:30:15',
         '2026-11-14T12:00:00+05:30:15.250000', '2026-11-14T12:00:00+23:59']


def record_case(field, value, ok):
    with tempfile.TemporaryDirectory() as data_dir:
        path = Path(data_dir) / task_status.FILENAME
        path.write_text(json.dumps({'k': {field: value}}), encoding='utf-8')
        with mock.patch.object(task_status, '_path', lambda: path), \
                mock.patch('backend.notifications.safely', lambda *a, **k: None):
            try:
                entry = task_status.record('k', ok=ok, started_at='S', duration_ms=1)
            except Exception as exc:  # Python crashes: the file is left as it was
                return {'error': type(exc).__name__, 'file': json.loads(path.read_text())}
        return {'entry': {key: entry[key] for key in ('run_count', 'failure_count', 'consecutive_failures') if key in entry}}


def main():
    records = []
    for field in ('run_count', 'failure_count', 'consecutive_failures'):
        for value in COUNTS:
            for ok in (True, False):
                records.append({'field': field, 'value': value, 'ok': ok, 'out': record_case(field, value, ok)})
    times = []
    for text in TIMES:
        value = task_status.parse_time(text)
        times.append({'in': text, 'iso': value.isoformat(), 'offset': value.utcoffset().total_seconds(), 'tzname': value.tzname()})
    out = {'records': records, 'times': times}
    Path(__file__).with_name('review.json').write_text(json.dumps(out, ensure_ascii=True, allow_nan=True, indent=0), encoding='ascii')


main()
