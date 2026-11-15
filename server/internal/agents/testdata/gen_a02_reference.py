"""Reference results of the A02 Python functions for the Go review tests.

Run from the repository root on the test server:
  rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/testdata/gen_a02_reference.py \
      > server/internal/agents/testdata/a02_reference.json'
then copy the file back with rt --back server/internal/agents/testdata/a02_reference.json.
"""
import json
import logging
import os
import sqlite3
import subprocess
import sys
import tempfile
from pathlib import Path

from backend.agents import deployment_messages, diagnostics, operation_events, runtime_state, upgrade_messages
from backend.agents.version import compare_versions

PEM = '-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----'
LONG_CJK = '错误' * 3000
LONG_EMOJI = 'x' + '😀' * 5000

FAILURE_TEXTS = [
    None, 5, '', ' ', '\u3000\x1c ok \x1d\x85',
    'plain error', '\x1b[31mred\x1b[0m', 'a\x00b\x07c\x7fd\x85e\x9ff', 'tab\tnew\nline\r\nend\r',
    # Unicode word boundaries before a secret key or Bearer/Basic
    '密码password=abc', 'ätoken: x', 'éBearer abc', '_token=1', '1token=2', '-token=3', '(password=x)',
    '密码 password=abc', '中Basic dXNlcg==', 'Ωsecret: s', '٣token=5', 'Ⅻtoken=5', '①token=6', '\u0301token=7',
    'tokenpassword=x', 'xtoken=1 token=2', 'a"token": "v"', "'password': 'it''s'", '"password": "a\\"b" tail',
    # case folding
    'PASSWORD=x', 'paſſword=abc', 'toKen=abc', 'İtoken=abc', 'APİ_KEY=1', 'apı_key=1', 'BASİC abc', 'prıvate_key: x', 'ınvalid', 'tokın=1', 'Authorization: Bearer abc.def',
    'authorization=basic Zm9v', 'api-key = x;y', 'private_key: v}w', 'access-key=a&b',
    # Unicode whitespace in \s
    'password:\u3000abc', 'Bearer\u00a0xyz', 'token =\x1cabc', 'token\u2028= v', 'Basic\u2003abc',
    # PEM
    PEM, 'before\n' + PEM + '\nafter', '-----BEGIN PRIVATE KEY-----\nabc\n', '-----BEGIN EC PRIVATE KEY-----abc',
    # YAML blocks
    '  password: |\n    private-first\n  error: bad proxy name\n',
    '- token: >-\n  a\n  b\nnext: 1', 'secret: |  # c\n\tx\n  y\nz', 'Password : |+\n a\n\n b\nc',
    '"api_key": |\n  k\n', 'token: |\u2028 x\u2028y', 'token: |\r\n  x\r\nnext', 'credential: >2\n  x\n',
    'token: |\x0b x\nnext', 'token: | # comment\x85 x\nnext', '  - secret: |\n      a\n    b\n  c',
    # URLs
    'http://user:pass@host/x?token=1', 'socks5://user:pass@host:1080', 'HTTPS://h/p?a=1', 'tcp://1.2.3.4:53',
    'ss://YWVzOnBhc3M=@h:1#n', 'see http://例子.com/路径?password=1 end', 'x https://h/a<b> "https://h/c"',
    'h3://a.b/c', 'quic://u@h', 'mailto:x@y', 'git+ssh://h/r', '1http://a', 'ftp://u:p@h/f\\x',
    # secrets interplay
    'secret value here', 'token=abc; Basic abc',
    LONG_CJK, LONG_EMOJI, 'é' * 9000, 'a' * 16384, 'a' * 16385, ' ' + 'a' * 16384 + ' ',
]

SECRETS = [
    [], ['value'], ['abc'], ['a b', 'a'], ['密 码'], ['a+b/c'], ['', None, 5, 'xyz'], ['ab', 'abc', 'b'],
]

LIMITS = [None, 128, 1024, 2048, 28, 27]


def failure_text_cases():
    out = []
    for value in FAILURE_TEXTS:
        for limit in (None, 128):
            out.append({'value': value, 'secrets': [], 'limit': limit})
    for value in ['secret value here a b 密 码 a+b/c a%2Bb%2Fc a%20b xyz abc', 'password=a b', 'pw: a+b']:
        for secrets in SECRETS:
            out.append({'value': value, 'secrets': secrets, 'limit': None})
    for value in [LONG_CJK, LONG_EMOJI, 'é' * 300]:
        for limit in LIMITS:
            out.append({'value': value, 'secrets': [], 'limit': limit})
    for case in out:
        kwargs = {} if case['limit'] is None else {'limit': case['limit']}
        case['want'] = diagnostics.failure_text(case['value'], case['secrets'], **kwargs)
    return out


def event_message_cases():
    values = [None, 5, '', 'download https://h/a?token=1 failed', 'socks5://u:p@h x', 'token=abc http://h',
              'x' * 2000, '错' * 400, 'a' * 1023 + '错']
    return [{'value': v, 'secrets': s, 'want': operation_events.event_message(v, s)}
            for v in values for s in ([], ['abc'])]


EVENT_TIMES = [
    None, 5, '', 'x', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00+08:00', '2026-01-01T00:00:00', '2026-01-01',
    '2026-01-01T10', '2026-01-01T10:30', '20260101T100000Z', '2026-W01-1T10:00Z', '2026-01-01 10:00:00+00:00',
    '2026-01-01X10:00:00+00:00', '2026-01-01t10:00:00z', '0001-01-01T00:00:00+01:00', '0001-01-01T00:00:00-01:00',
    '9999-12-31T23:59:59-01:00', '9999-12-31T23:59:59+01:00', '2026-01-01T00:00:00.1234567Z',
    '2026-01-01T00:00:00,5Z', '2026-01-01T24:00:00Z', '2026-01-01T23:59:60Z', '2026-01-01T00:00:00+24:00',
    '2026-01-01T00:00:00+05:30:15.5', '2026-01-01T00:00:00.123+0530', '２０２６-01-01T00:00:00Z',
    '٢٠٢٦-01-01T00:00:00Z', '2026-01-01T00:00:00Z' + ' ' * 44, '2026-01-01T00:00:00Z' + ' ' * 45,
    '2026-02-29T00:00:00Z', '2024-02-29T00:00:00Z', '2026-01-01T00:00:00ZZ', '2026-01-01T00:00:00+00:00Z',
    '2026-01-01T00:00:00.Z', '2026-01-01T00:00:00.000000001+00:00', '+2026-01-01T00:00:00Z', '2026-1-1T00:00:00Z',
    '2026-001T00:00Z', '2026001T00Z', '2026-01-01T00Z', '2026-01-01T0000Z', '2026-01-01T00:00:00+00',
    '2026-01-01T00:00:00-00:00', '2026-01-01T00:00:00 +00:00', '2026-01-01T00:00:00\u3000Z',
    '2026-01-01\u5e7400:00:00Z', '2026-01-01T00:00:00+0000', '2026-01-01T01:02:03.4Z',
]


def event_time_cases():
    return [{'value': v, 'want': operation_events.event_time(v) if v is not None else 'NOW'}
            for v in EVENT_TIMES]


def event_list_cases():
    at = '2026-01-01T00:00:00Z'
    lists = [
        None, 'x', [], [5, None, 'x'],
        [{'at': at, 'status': 'ok'}, {'at': at, 'status': 'ok'}, {'at': at, 'status': 'ok', 'message': ''}],
        [{'at': at, 'status': 'Bad'}, {'at': at, 'status': 'a' * 64}, {'at': at, 'status': 'a' * 65},
         {'at': at, 'status': 'ok\n'}, {'at': at, 'status': '9a'}, {'at': at, 'status': 'a-b'}],
        [{'at': '2026-01-01T00:00:00', 'status': 'naive'}, {'at': 5, 'status': 'num'},
         {'at': at, 'status': 'msg', 'message': 'token=abc http://h/x'}, {'at': at, 'status': 'msg', 'message': 5}],
        [{'at': '2026-01-01T00:00:0%dZ' % (i % 10), 'status': 's%d' % (i % 3)} for i in range(100)],
        [{'at': at, 'status': 'a', 'message': 'x'}, {'at': at, 'status': 'a', 'message': 'y'},
         {'at': at, 'status': 'a', 'message': 'x'}],
    ]
    clean = []
    for value in lists:
        for statuses in (None, ['ok', 'msg', 'a']):
            clean.append({'value': value, 'statuses': statuses,
                          'want': operation_events.clean_events(value, ['abc'],
                                                                set(statuses) if statuses else None)})
    t1, t2, t3 = '2026-01-01T00:00:01Z', '2026-01-01T00:00:02.5Z', '2026-01-01T00:00:02Z'
    merges = [
        ([], [{'at': t1, 'status': 'start'}]),
        ([{'at': t2, 'status': 'start'}], []),
        ([{'at': t2, 'status': 'start'}, {'at': t1, 'status': 'uncertain', 'message': 'conn'}],
         [{'at': t3, 'status': 'start'}, {'at': t1, 'status': 'uncertain'}]),
        ([{'at': t1, 'status': 'a'}, {'at': t3, 'status': 'b'}], [{'at': t1, 'status': 'a'}, {'at': t2, 'status': 'c'}]),
        ([{'at': t3, 'status': 'a', 'message': 'x'}, {'at': t1, 'status': 'b'}],
         [{'at': t3, 'status': 'a', 'message': 'x'}, {'at': t1, 'status': 'a'}]),
        ([{'at': '2026-01-01T08:00:00+08:00', 'status': 'z'}], [{'at': '2026-01-01T00:00:00Z', 'status': 'z'}]),
    ]
    merge = [{'events': e, 'incoming': i, 'statuses': s,
              'want': operation_events.merge_events(e, i, secrets=['abc'], statuses=set(s) if s else None)}
             for e, i in merges for s in (None, ['a', 'start'])]
    appends = [
        (None, 'start', None, t1), ([{'at': t1, 'status': 'start'}], 'start', None, t2),
        ([{'at': t1, 'status': 'start'}], 'start', 'm', t2), ([], 'Bad', None, t1), ([], 'ok', 'token=abc', t1),
        ([{'at': t1, 'status': 's%d' % i} for i in range(80)], 'last', None, t2), ([], 'ok', None, 'garbage'),
    ]
    append = [{'events': e, 'status': s, 'message': m, 'at': a,
               'want': operation_events.append_event(e, s, m, at=a, secrets=['abc'])} for e, s, m, a in appends]
    return clean, merge, append


def dns_cases():
    good = {'name': 'listen', 'level': 'pass', 'message': 'ok'}
    payloads = [
        {}, {'dns_checks': None}, {'dns_checks': 'x'}, {'dns_checks': []}, {'dns_checks': [good]},
        {'dns_checks': [good, 5, {'name': 'n', 'level': 'info', 'message': 'm'}]},
        {'dns_checks': [{'name': ' ', 'level': 'warn', 'message': 'm'}]},
        {'dns_checks': [{'name': 'n', 'level': 'error', 'message': 'm token=abc', 'fix': '  '}]},
        {'dns_checks': [{'name': 'n', 'level': 'error', 'message': 'm', 'fix': 'run x', 'extra': 1}]},
        {'dns_checks': [dict(good, name='n%d' % i) for i in range(33)]},
        {'dns_checks': [dict(good, name='n%d' % i) for i in range(32)]},
        {'dns_checks': [dict(good, name='n%d' % i) for i in range(31)] + [5]},
        {'dns_checks': [dict(good, name='名' * 100, message='错' * 1000)]},
        {'dns_checked_at': '2026-01-01T00:00:00Z'}, {'dns_checked_at': '2026-01-01T00:00:00'},
        {'dns_checked_at': '2026-01-01'}, {'dns_checked_at': '2026-01-01T00:00:00+08:00'},
        {'dns_checked_at': '2026-01-01T00:00:00Z' + ' ' * 44}, {'dns_checked_at': '2026-01-01T00:00:00Z' + ' ' * 45},
        {'dns_checked_at': 'T'}, {'dns_checked_at': 5}, {'dns_checked_at': '2026-01-01t00:00:00z'},
        {'dns_checked_at': '2026-01-01X10:00:00+00:00T'}, {'dns_checked_at': '20260101T000000Z'},
    ]
    return [{'payload': p, 'want': diagnostics.dns_report_fields(p, ['abc'])} for p in payloads]


VERSIONS = [
    ('1.8.9-go', '1.8.10-go'), ('1.8.10-go', '1.8.9-go'), ('1.8', '1.8.0'), ('1.8.0.1', '1.8'), ('1.a', '1.10'),
    ('1.a', '1.B'), (' 1', '1'), ('\x1c1', '0'), ('1\x1f', '2'), ('１.2', '1.10'), ('𝟘', '1'), ('𝟙', '1'),
    ('𝟡', '8'), ('𝟢', '1'), ('𝟬', '3'), ('𝟵', '8'), ('٣', '4'), ('৯', '10'), ('1_0', '10'), ('1__0', '10'),
    ('_1', '1'), ('1_', '1'), ('+1', '1'), ('-1', '0'), ('', '0'), ('', ''), ('1' * 4300, '2'), ('1' * 4301, '2'),
    ('1' + '_1' * 4299, '2'), ('0' * 4301 + '1', '2'), ('99999999999999999999999', '1'), ('1.2-rc1-go', '1.2'),
    ('1.8.61-go', '1.8.61'), ('1..2', '1.0.2'), ('1.\u3000', '1.0'), ('1.\u00a02', '1.2'), ('1.0x5', '1.5'),
    ('1.5\x00', '1.5'), ('1.٣_٣', '1.33'), ('1.+-2', '1.0'), ('1.- 2', '1.0'), ('a', 'B'), ('é', 'z'),
    ('1.İ', '1.i'), ('1.8.61-go', '1.8.61-go'), ('\x1c１', '0'), ('１\x1f', '2'), ('\x851', '0'), ('\xa01', '0'),
    ('1\x85', '2'), ('\x0b1\x0c', '0'), ('\u30001', '0'), ('١\u2028', '0'),
]


def version_cases():
    return [{'a': a, 'b': b, 'want': compare_versions(a, b)} for a, b in VERSIONS]


MESSAGES = [
    None, 5, '', '   ', 'Agent not found', 'Agent not found ', 'manifest checksum mismatch for a', 'Agent returned HTTP 404',
    'Agent returned HTTP ٤٠٤', 'Agent returned HTTP 4040', 'Rule x: y', 'Rule x:\ny', 'Rule : y', 'Rule x: ',
    'unknown', 'timezone data for UTC missing', 'Agent returned HTTP 404\n',
]
UPGRADE = MESSAGES + [
    'İ/o timeout', 'I/O TIMEOUT here', '\x1cpermission denied\x1c', 'x; rollback: y', 'migration failed; rollback: ',
    'automatic migration failed: no space left on device', 'Agent download HTTP 500', 'Agent download HTTP ٥٠٠',
    'deployment abc requires recovery', 'deployment abc requires rollback recovery', 'deployment a\u3000b requires recovery',
    'systemctl operation failed: x', ' systemctl operation failed: x', '中文错误', 'mixed 中 text', 'refusing nonregular upgrade target: /x',
    '\u3000migration failed\u3000', 'interrupted migration restore failed: migration failed; rollback: unknown',
    'CONTEXT DEADLINE EXCEEDED', 'Kontext', 'no such host\u0130', '\x1cmigration failed\x1c', '\x1fAgent not found',
    '\x1c', '\x85', 'deployment a\x1cb requires recovery',
]


def message_cases():
    deploy = [{'value': v, 'want': deployment_messages.deployment_message(v)} for v in MESSAGES]
    upgrade = [{'value': v, 'want': upgrade_messages.upgrade_error_message(v)} for v in UPGRADE]
    t1 = '2026-01-01T00:00:01Z'
    records = [
        {'message': 'Agent not found', 'error': 5, 'rollback_error': None, 'other': 'Agent not found',
         'events': [{'at': t1, 'status': 'failed', 'message': 'Invalid deployment ID'}, {'at': t1, 'status': 'ok'}]},
        {'events': 'x'}, {},
    ]
    states = [
        {'error': 'İ/o timeout token=abc', 'message': '\x1b[1mhi\x1b[0m', 'events': [
            {'at': t1, 'status': 'failed', 'message': 'permission denied'},
            {'at': t1, 'status': 'running', 'message': 'permission denied'}]},
        {'error': 5, 'message': 0}, {'error': '', 'events': None},
    ]
    pub_deploy = [{'value': r, 'want': deployment_messages.public_deployment_messages(r)} for r in records]
    pub_upgrade = [{'value': s, 'want': upgrade_messages.public_upgrade_state(s)} for s in states]
    return deploy, upgrade, pub_deploy, pub_upgrade


TIMESTAMP_TEXTS = [
    '2026-03-08T02:30:00', '2026-03-08T01:59:59.999999', '2026-03-08T03:00:00', '2026-11-01T01:30:00',
    '2026-11-01T00:59:59', '2026-11-01T02:00:00', '2026-06-01T12:00:00.5', '1970-01-01T00:00:00',
    '1969-12-31T23:59:59.000001', '2026-01-01', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00.000001+05:45',
    '2038-01-19T03:14:08', '9999-12-31T23:59:59.999999+14:00', '0001-01-01T00:00:00+00:00',
    '2026-01-01T00:00:00.123456-03:30', '1900-01-01T00:00:00',
]
NANOS = [1_700_000_000_000_000_000, 1_700_000_000_123_456_789, 1_700_000_000_999_999_500, 1_700_000_000_000_000_500,
         1_772_953_200_000_001_000, 1_700_000_000_000_000_001, 1_785_000_000_987_654_321]


def timestamp_cases():
    """Run per time zone in a child process (TZ is read at start-up)."""
    result = {}
    for zone in ('America/New_York', 'Asia/Shanghai', 'UTC'):
        code = ('import json, sys\nfrom datetime import datetime\n'
                'texts, nanos = json.loads(sys.argv[1])\nout = {"timestamp": [], "fromtimestamp": []}\n'
                'for t in texts:\n'
                '    try:\n        out["timestamp"].append(repr(datetime.fromisoformat(t).timestamp()))\n'
                '    except Exception as e:\n        out["timestamp"].append(type(e).__name__)\n'
                'for n in nanos:\n'
                '    d = float(n // 10**9) if n % 10**9 == 0 else float(n) / 1e9\n'
                '    out["fromtimestamp"].append([repr(d), datetime.fromtimestamp(d).isoformat()])\n'
                'print(json.dumps(out))\n')
        env = dict(os.environ, TZ=zone)
        output = subprocess.run([sys.executable, '-c', code, json.dumps([TIMESTAMP_TEXTS, NANOS])],
                                env=env, check=True, capture_output=True, text=True).stdout
        data = json.loads(output)
        result[zone] = {
            'timestamp': [{'value': t, 'want': w} for t, w in zip(TIMESTAMP_TEXTS, data['timestamp'])],
            'fromtimestamp': [{'ns': n, 'float': f, 'want': w} for n, (f, w) in zip(NANOS, data['fromtimestamp'])],
        }
    return result


CREATE = ('CREATE TABLE agent_runtime (agent_id TEXT PRIMARY KEY, heartbeat_at REAL NOT NULL, '
          'last_heartbeat TEXT NOT NULL, fields TEXT NOT NULL)')
LOOSE = 'CREATE TABLE agent_runtime (agent_id, heartbeat_at, last_heartbeat, fields)'
LEGACY = [
    {'name': 'normal', 'sql': [CREATE, "INSERT INTO agent_runtime VALUES ('a', 1700000000.5, '2023-11-14T22:13:20.5', '{\"version\": \"1.0\"}')",
                               "INSERT INTO agent_runtime VALUES ('b', 1700000000, 'x', '{\"cpu\": 1.50, \"n\": 1e400}')"]},
    {'name': 'shapes', 'sql': [LOOSE,
                               "INSERT INTO agent_runtime VALUES (5, 1.0, 'x', '{}')",
                               "INSERT INTO agent_runtime VALUES ('int', 7, 'x', '{}')",
                               "INSERT INTO agent_runtime VALUES ('textnum', '7', 'x', '{}')",
                               "INSERT INTO agent_runtime VALUES ('null', NULL, 'x', '{}')",
                               "INSERT INTO agent_runtime VALUES ('lastnum', 1, 5, '{}')",
                               "INSERT INTO agent_runtime VALUES ('list', 1, 'x', '[]')",
                               "INSERT INTO agent_runtime VALUES ('badjson', 1, 'x', '{bad')",
                               "INSERT INTO agent_runtime VALUES ('nulljson', 1, 'x', NULL)",
                               "INSERT INTO agent_runtime VALUES ('intjson', 1, 'x', 5)",
                               "INSERT INTO agent_runtime VALUES ('textbom', 1, 'x', char(65279) || '{}')",
                               "INSERT INTO agent_runtime VALUES ('blob', 1, 'x', X'7B2261223A317D')",
                               "INSERT INTO agent_runtime VALUES ('blobbom', 1, 'x', X'EFBBBF7B7D')",
                               "INSERT INTO agent_runtime VALUES ('blob16', 1, 'x', X'FFFE7B007D00')",
                               "INSERT INTO agent_runtime VALUES ('blob16be', 1, 'x', X'007B007D')",
                               "INSERT INTO agent_runtime VALUES ('blob32', 1, 'x', X'7B0000007D000000')",
                               "INSERT INTO agent_runtime VALUES ('blobbad', 1, 'x', X'7B22FF227D')",
                               "INSERT INTO agent_runtime VALUES (X'61', 1, 'x', '{}')",
                               "INSERT INTO agent_runtime VALUES ('lastblob', 1, X'61', '{}')",
                               "INSERT INTO agent_runtime VALUES ('dup', 1, 'first', '{}')",
                               "INSERT INTO agent_runtime VALUES ('dup', 2, 'second', '{}')",
                               "INSERT INTO agent_runtime VALUES ('inf', 9e999, 'x', '{}')",
                               "INSERT INTO agent_runtime VALUES ('big', 9223372036854775807, 'x', '{}')",
                               "INSERT INTO agent_runtime VALUES ('dupkeys', 1, 'x', '{\"a\": 1, \"b\": 2, \"a\": 3}')"]},
    {'name': 'badtext', 'sql': [LOOSE, "INSERT INTO agent_runtime VALUES ('ok', 1, 'x', '{}')",
                                "INSERT INTO agent_runtime VALUES ('t', 1, CAST(X'61FF62' AS TEXT), '{}')"]},
    {'name': 'badtextmulti', 'sql': [LOOSE, "INSERT INTO agent_runtime VALUES ('t', 1, CAST(X'61E4B862F09F98' AS TEXT), '{}')"]},
    {'name': 'badtextid', 'sql': [LOOSE, "INSERT INTO agent_runtime VALUES (CAST(X'C3' AS TEXT), 1, CAST(X'FF' AS TEXT), '{}')"]},
    {'name': 'notable', 'sql': ['CREATE TABLE other (x)']},
    {'name': 'missingcolumn', 'sql': ['CREATE TABLE agent_runtime (agent_id, heartbeat_at, last_heartbeat)']},
    {'name': 'notadb', 'raw': 'not a database' * 100},
    {'name': 'empty', 'raw': ''},
]


def legacy_cases():
    messages = []

    class Capture(logging.Handler):
        def emit(self, record):
            messages.append(record.getMessage())

    logger = logging.getLogger('backend.agents.runtime_state')
    logger.addHandler(Capture())
    logger.setLevel(logging.DEBUG)
    out = []
    for case in LEGACY:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / 'runtime.sqlite3'
            if 'raw' in case:
                path.write_text(case['raw'])
            else:
                connection = sqlite3.connect(path)
                for statement in case['sql']:
                    connection.execute(statement)
                connection.commit()
                connection.close()
            messages.clear()
            rows = runtime_state._legacy_rows(path)
            out.append(dict(case, rows=[[k, repr(v[0]), v[1], v[2]] for k, v in rows.items()],
                            logs=list(messages)))
    return out


def remove_cases():
    messages = []

    class Capture(logging.Handler):
        def emit(self, record):
            messages.append(record.getMessage())

    logging.getLogger('backend.agents.runtime_state').addHandler(Capture())
    with tempfile.TemporaryDirectory() as tmp:
        wal = Path(tmp) / 'runtime.sqlite3-wal'
        (wal / 'inner').mkdir(parents=True)
        empty = Path(tmp) / 'runtime.sqlite3-shm'
        empty.mkdir()
        runtime_state._remove([Path(tmp) / 'runtime.sqlite3', wal, empty])
        return [m.replace(tmp, '<DIR>') for m in messages]


RESPONSES = [
    ('application/json', b'{"message": "ok token=abc", "error": "e"}', 400),
    ('application/json', b'\xef\xbb\xbf{"message": "bom"}', 200),
    ('application/json', b'{"message": "bad \xff byte"}', 200),
    ('application/json; charset=utf-8', b'{"message": "", "error": "only error"}', 500),
    ('application/json', b'{"message": "", "error": ""}', 500),
    ('application/json', b'[1, 2]', 502),
    ('application/json', b'not json', 502),
    (None, b'\xef\xbb\xbf{"message": "bom without type"}', 200),
    (None, '{"message": "utf16 \u4e2d"}'.encode('utf-16'), 200),
    (None, '{"message": "utf16le"}'.encode('utf-16-le'), 200),
    (None, b'{}', 200),
    (None, b'', 404),
    ('text/plain', '中文 错误 token=abc'.encode('utf-8'), 500),
    ('text/plain; charset=utf-8', '中文 错误 token=abc'.encode('utf-8'), 500),
    ('text/plain; charset="UTF-8"', '中文 \xff'.encode('utf-8') + b'\xff\xe4\xb8', 500),
    ('text/plain; charset=latin-1', b'caf\xe9', 500),
    ('text/plain;charset=ascii', b'caf\xe9', 500),
    ('text/plain; charset=nonexistent', '中'.encode('utf-8'), 500),
    ('text/plain', b'   ', 500),
    ('text/plain', b'{"message": "json as text"}', 200),
    ('Text/Plain', b'case', 500),
    (' text/plain', b'space', 500),
    ('text/html', b'<html>bad gateway</html>', 502),
    ('text/plain', b'{"message": 5}', 503),
    ('application/json', b'{"message": 5, "error": ["x"], "failed_stage": "s", "rollback_error": "r", "rollback_error_details": "d", "error_details": "x"}', 409),
]


def response_cases():
    from requests.models import Response
    from requests.structures import CaseInsensitiveDict
    from requests.utils import get_encoding_from_headers
    out = []
    for content_type, body, status in RESPONSES:
        response = Response()
        response.status_code = status
        response.headers = CaseInsensitiveDict({} if content_type is None else {'Content-Type': content_type})
        response._content = body
        response.encoding = get_encoding_from_headers(response.headers)
        try:
            payload = response.json()
            json_result = {'ok': payload}
        except ValueError as error:
            json_result = {'error': type(error).__name__}
        out.append({'content_type': content_type, 'body': list(body), 'status': status,
                    'text': response.text, 'json': json_result,
                    'want': diagnostics.response_failure(response, fallback='fallback', secrets=['abc'])})
    return out


# A symlink loop makes Path.resolve() raise RuntimeError in 3.11; such a data
# directory cannot be opened by the repository either, so it is not covered.
RESOLVE_LINKS = [('link', 'real'), ('real/sub/rel', '../../real'),
                 ('abs', '<DIR>/real/sub'), ('dangling', 'nowhere/deeper')]
RESOLVE_QUERIES = ['link/missing/../x', 'link/sub/..', 'link/./a//b', 'missing/../link', 'real/sub/rel/y',
                   'link/..', 'abs/../..', 'dangling/x', 'dangling/../..', 'real/sub/rel/sub/rel', 'link/runtime.sqlite3']


def resolve_cases():
    with tempfile.TemporaryDirectory() as tmp:
        tmp = os.path.realpath(tmp)
        (Path(tmp) / 'real' / 'sub').mkdir(parents=True)
        for name, target in RESOLVE_LINKS:
            os.symlink(target.replace('<DIR>', tmp), Path(tmp) / name)
        return {'links': RESOLVE_LINKS,
                'cases': [{'query': q, 'want': str(Path(tmp, q).resolve()).replace(tmp, '<DIR>')}
                          for q in RESOLVE_QUERIES]}


def main():
    clean, merge, append = event_list_cases()
    deploy, upgrade, pub_deploy, pub_upgrade = message_cases()
    reference = {
        'failure_text': failure_text_cases(),
        'event_message': event_message_cases(),
        'event_time': event_time_cases(),
        'clean_events': clean,
        'merge_events': merge,
        'append_event': append,
        'dns_report_fields': dns_cases(),
        'compare_versions': version_cases(),
        'deployment_message': deploy,
        'upgrade_error_message': upgrade,
        'public_deployment_messages': pub_deploy,
        'public_upgrade_state': pub_upgrade,
        'time': timestamp_cases(),
        'legacy_rows': legacy_cases(),
        'remove_logs': remove_cases(),
        'resolve': resolve_cases(),
        'responses': response_cases(),
    }
    json.dump(reference, sys.stdout, ensure_ascii=False, indent=1)
    sys.stdout.write('\n')


if __name__ == '__main__':
    main()
