"""Reference results of metrics_history.py for the A04 review tests.

Run from the repository root on the test server:
  rt --back server/internal/agents/metrics/testdata/a04_reference.json -- bash -lc 'PYTHONPATH=. \
      ~/cf-test/.venv/bin/python server/internal/agents/metrics/testdata/gen_a04_reference.py \
      > server/internal/agents/metrics/testdata/a04_reference.json'

Sections:
- read_file: bytes of a <agent>_metrics.jsonl file → _read_file(agent, cutoff)
  (points as [isoformat of the parsed time, stored line], line count)
- legacy: bytes of a <agent>_metrics.json file → _read_legacy(agent, cutoff)
- summary / traffic_stats / trend: points held in memory (timestamp text and
  the JSON line) with a fixed datetime.now() → the result JSON (sorted keys,
  ASCII, compact) or "<Exception>: <text>".
"""
import base64
import json
import sys
import tempfile
from collections import deque
from datetime import datetime

import backend.agents.metrics_history as mh

CUTOFF = '2026-11-14T12:00:00'
NOW = '2026-11-15T12:00:00'  # a Sunday


def enc(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'))


def error_text(error):
    return f'{type(error).__name__}: {error}'


def line(timestamp, **sections):
    point = {'timestamp': timestamp}
    point.update(sections)
    return json.dumps(point, ensure_ascii=False, separators=(',', ':'))


T = '2026-11-15T10:00:00'
GOOD = line(T, cpu={'usage_percent': 1.5}).encode()

READ_FILES = {
    'plain': GOOD + b'\n',
    'duplicate timestamp text': GOOD + b'\n' + line(T, cpu={'usage_percent': 2}).encode() + b'\n',
    'same time other text': GOOD + b'\n' + line(T + '.000000').encode() + b'\n' + line('2026-11-15 10:00:00').encode() + b'\n',
    'older than cutoff and at cutoff': line('2026-11-14T11:59:59.999999').encode() + b'\n' + line(CUTOFF).encode() + b'\n',
    'aware times': b'\n'.join(line(t).encode() for t in (
        '2026-11-15T10:00:00+08:00', '2026-11-15T10:00:00Z', '2026-11-15T10:00:00+00:00', '2026-11-15T10:00:00-00:30')) + b'\n',
    'iso variants': b'\n'.join(line(t).encode() for t in (
        '2026-11-15', '2026-11-15T10', '2026-11-15T10:01', '2026-11-15T10:02:03.5', '2026-11-15T10:02:04.1234567',
        '2026-11-15T10:02:05,25', '20261115T100206', '20261115', '2026-W46-7T10:02:07', '2026-W46-7', '2026-W46',
        '2026-11-15T24:00:00', '2026-11-15t10:02:08', '2026-11-15X10:02:09', '2026-11-15T10:02:10.123',
        '２０２６-11-15T10:02:11', '2026-11-15T10:02:12 ', ' 2026-11-15T10:02:13', '2026-11-15T10:02:14+08',
        '2026-11-15T10:02:15.000001Z', '9999-12-31T23:59:59.999999', '2026-02-29T10:00:00', '2026-11-15T10:61:00',
        '2026-11-15T10:02:16.', '2026-1115T10:02:17', '202611-15T10:02:18', '2026-11-15T1002', '2026-11-15T100219')) + b'\n',
    'bad timestamp types': b'\n'.join(x.encode() for x in (
        '{"timestamp":5}', '{"timestamp":null}', '{"timestamp":["2026-11-15T10:00:00"]}', '{"cpu":1}',
        '["2026-11-15T10:00:00"]', '"2026-11-15T10:00:00"', '5', 'null', '{}', '{"timestamp":""}')) + b'\n',
    'blank and space lines': b'\n'.join(x.encode() for x in (
        '', ' ', '\t', '\x1c', '\x1d\x1e\x1f', '　', '\x85', ' ', ' ', '​', '᠎', '\x0b\x0c',
        ' ' + line('2026-11-15T10:03:00'), '\t' + line('2026-11-15T10:03:01') + ' \t',
        '　' + line('2026-11-15T10:03:02'), line('2026-11-15T10:03:03') + '\x1c')) + b'\n',
    'universal newlines': line('2026-11-15T10:04:00').encode() + b'\r\n' + line('2026-11-15T10:04:01').encode() + b'\r'
        + line('2026-11-15T10:04:02').encode() + b'\r\r\n\n' + line('2026-11-15T10:04:03').encode(),
    'bom': b'\xef\xbb\xbf' + line('2026-11-15T10:05:00').encode() + b'\n' + line('2026-11-15T10:05:01').encode() + b'\n',
    'invalid utf-8 in strings': b'\n'.join([
        b'{"timestamp":"2026-11-15T10:06:00","n":"a\xff\xfeb"}',
        b'{"timestamp":"2026-11-15T10:06:01","n":"\xed\xa0\x80"}',
        b'{"timestamp":"2026-11-15T10:06:02","n":"\xe4\xb8"}',
        b'{"timestamp":"2026-11-15T10:06:03","n":"\xf0\x9f\x98x"}',
        b'{"timestamp":"2026-11-15T10:06:04","n":"\xc0\xaf\xe0\x80\xbf\xf5\x80"}',
        b'{"timestamp":"2026-11-15T10:06:05","n":"\xf4\x90\x80\x80|\xc2"}',
        b'{"timestamp":"2026-11-15T10:06:06\xff"}',
    ]) + b'\n\xe4\xb8',
    'numbers': b'\n'.join(x.encode() for x in (
        '{"timestamp":"2026-11-15T10:07:00","v":[NaN,Infinity,-Infinity,-0.0,1e400,-1e400,1E5,1.0,1.50,0.1,1e-400,12345678901234567890123]}',
        '{"timestamp":"2026-11-15T10:07:01","v":' + '9' * 4300 + '}',
        '{"timestamp":"2026-11-15T10:07:02","v":' + '9' * 4301 + '}',
        '{"timestamp":"2026-11-15T10:07:03","v":-0}',
        '{"timestamp":"2026-11-15T10:07:04","v":1.7976931348623157e308,"w":5e-324,"x":2.5e-308}',
        '{"timestamp":"2026-11-15T10:07:05","v":nan}',
        '{"timestamp":"2026-11-15T10:07:06","v":01}',
    )) + b'\n',
    'strings and keys': b'\n'.join(x.encode() for x in (
        '{"timestamp":"2026-11-15T10:08:00","timestamp":"2026-11-15T10:08:01","a":1,"a":2}',
        '{"a":1,"timestamp":"2026-11-15T10:08:02"}',
        '{"timestamp":"2026-11-15T10:08:03","n":"\\u00e9\\ud83d\\ude00中文😀\\u2028\\u0000\\"\\\\/"}',
        '{"timestamp":"2026-11-15T10:08:04","n":"\x7f\u0085"}',
        '{"timestamp":"2026-11-15T10:08:05","n":"tab\there"}',
        '{"timestamp":"2026-11-15T10:08:06"} trailing',
        '{"timestamp":"2026-11-15T10:08:07"}{"x":1}',
        '{"timestamp":"2026-11-15T10:08:08",}',
        '{"timestamp": "2026-11-15T10:08:09" , "cpu" : { } }',
        '{"timestamp":"2026-11-15T10:08:10","\\":1}',
    )) + b'\n',
    'truncated last line': GOOD + b'\n' + b'{"timestamp":"2026-11-15T10:09:00","cpu":{"usage',
    'empty file': b'',
    'only newlines': b'\n\n\r\n',
}

LEGACY_FILES = {
    'metrics list': json.dumps({'metrics': [{'timestamp': '2026-11-15T10:00:00', 'cpu': {'usage_percent': 1}},
                                            5, 'x', None, [1], {'timestamp': '2026-11-15T10:00:00+08:00'},
                                            {'timestamp': '2026-11-15T10:00:01', 'n': '中'},
                                            {'timestamp': '2026-11-15T10:00:00'}]}, ensure_ascii=False).encode(),
    'metrics null': b'{"metrics": null}',
    'metrics int': b'{"metrics": 5}',
    'metrics float': b'{"metrics": 1.5}',
    'metrics bool': b'{"metrics": true}',
    'metrics string': b'{"metrics": "abc"}',
    'metrics dict': b'{"metrics": {"timestamp": "2026-11-15T10:00:00"}}',
    'no metrics': b'{"other": []}',
    'top list': b'[{"timestamp": "2026-11-15T10:00:00"}]',
    'top string': b'"x"',
    'bom': b'\xef\xbb\xbf{"metrics": [{"timestamp": "2026-11-15T10:00:00"}]}',
    'utf-16': '{"metrics": [{"timestamp": "2026-11-15T10:00:00"}]}'.encode('utf-16'),
    'utf-16le no bom': '{"metrics": []}'.encode('utf-16-le'),
    'invalid utf-8': b'{"metrics": [{"timestamp": "2026-11-15T10:00:00", "n": "\xff"}]}',
    'empty': b'',
    'syntax error': b'{"metrics": [',
    'nan values': b'{"metrics": [{"timestamp": "2026-11-15T10:00:00", "v": NaN}]}',
    'crlf': b'{"metrics":\r\n [{"timestamp": "2026-11-15T10:00:00"}]}\r\n',
    'old points only': b'{"metrics": [{"timestamp": "2026-11-13T10:00:00"}]}',
}


class FixedNow(datetime):
    current = None

    @classmethod
    def now(cls, tz=None):
        return cls.current


def history_with(points, now_text):
    FixedNow.current = FixedNow.fromisoformat(now_text)
    mh.datetime = FixedNow
    directory = tempfile.mkdtemp()
    history = mh.MetricsHistory(directory)
    history._points['a'] = deque(((FixedNow.fromisoformat(json.loads(text)['timestamp']), text) for text in points),
                                 maxlen=history.max_points)
    return history


def point(minute, network=None, second=0, micro=0, day=15, hour=11, **sections):
    timestamp = datetime(2026, 11, day, hour, minute, second, micro).isoformat()
    if network is not None:
        sections['network'] = network
    return line(timestamp, **sections)


def net(sent, recv, **extra):
    value = {'bytes_sent': sent, 'bytes_recv': recv}
    value.update(extra)
    return value


SUMMARY = {
    'empty': ([], 1),
    'ints and floats': ([point(1, cpu={'usage_percent': 10}, memory={'used_percent': 20.5}, disk={'used_percent': 3},
                               network={'speed_sent': 1, 'speed_recv': 2}),
                         point(2, cpu={'usage_percent': 11.25}, memory={'used_percent': 21}, disk={},
                               network={'speed_sent': 3})], 1),
    'missing sections': ([point(3), point(4, cpu={})], 1),
    'bools': ([point(5, cpu={'usage_percent': True}), point(6, cpu={'usage_percent': 1}),
               point(7, cpu={'usage_percent': False})], 1),
    'only bools': ([point(5, cpu={'usage_percent': True})], 1),
    'string value': ([point(8, cpu={'usage_percent': 'x'})], 1),
    'string after float': ([point(8, cpu={'usage_percent': 1.5}), point(9, cpu={'usage_percent': 'x'})], 1),
    'none value': ([point(8, memory={'used_percent': None})], 1),
    'list value': ([point(8, disk={'used_percent': [1]})], 1),
    'dict value': ([point(8, disk={'used_percent': {}})], 1),
    'cpu list with key': ([point(8, cpu=['usage_percent'])], 1),
    'cpu list without key': ([point(8, cpu=['x', 1])], 1),
    'cpu string with key': ([point(8, cpu='xusage_percentx')], 1),
    'cpu string without key': ([point(8, cpu='abc')], 1),
    'cpu null': ([point(8, cpu=None)], 1),
    'cpu int': ([point(8, cpu=5)], 1),
    'cpu float': ([point(8, cpu=1.5)], 1),
    'cpu bool': ([point(8, cpu=True)], 1),
    'memory bad before disk': ([point(8, memory=5, disk=None)], 1),
    'huge ints': ([point(9, cpu={'usage_percent': 10 ** 308}), point(10, cpu={'usage_percent': 10 ** 308})], 1),
    'huge int mean overflow': ([point(9, cpu={'usage_percent': 10 ** 309}), point(10, cpu={'usage_percent': 10 ** 309})], 1),
    'huge int then float': ([point(9, cpu={'usage_percent': 10 ** 400}), point(10, cpu={'usage_percent': 1.5})], 1),
    'float then huge int': ([point(9, cpu={'usage_percent': 1.5}), point(10, cpu={'usage_percent': 10 ** 400})], 1),
    'long overflow ints': ([point(9, cpu={'usage_percent': 2 ** 63 - 1}), point(10, cpu={'usage_percent': 2 ** 63 - 1}),
                            point(11, cpu={'usage_percent': 0.5})], 1),
    'rounding int then float': ([point(9, cpu={'usage_percent': 2 ** 53 + 1}), point(10, cpu={'usage_percent': 0.5})], 1),
    'rounding float then int': ([point(9, cpu={'usage_percent': 0.5}), point(10, cpu={'usage_percent': 2 ** 53 + 1})], 1),
    'float overflow': ([point(9, cpu={'usage_percent': 1e308}), point(10, cpu={'usage_percent': 1e308})], 1),
    'negative zero': ([point(9, cpu={'usage_percent': -0.0}), point(10, cpu={'usage_percent': 0})], 1),
    'only negative zero': ([point(9, cpu={'usage_percent': -0.0})], 1),
    'mean exact third': ([point(9, cpu={'usage_percent': 1}), point(10, cpu={'usage_percent': 1}),
                          point(11, cpu={'usage_percent': 2})], 1),
    'int float equal max': ([point(9, cpu={'usage_percent': 1}), point(10, cpu={'usage_percent': 1.0}),
                             point(11, cpu={'usage_percent': True})], 1),
    'big int vs float compare': ([point(9, cpu={'usage_percent': 2 ** 53 + 1}), point(10, cpu={'usage_percent': 2.0 ** 53})], 1),
    'hours window': ([point(1, hour=9), point(2, hour=11, cpu={'usage_percent': 5})], 1),
    'hours zero': ([point(1, hour=11)], 0),
    'hours negative': ([point(1, hour=11)], -1),
    'hours big': ([point(1, hour=11)], 10 ** 6),
    'future point': ([point(1, hour=13, cpu={'usage_percent': 7})], 1),
}

# NaN/Infinity only come from saved files (heartbeats refuse them); they are
# JSON lines as json.dumps writes them.
SUMMARY_RAW = {
    'network null': ['{"timestamp":"2026-11-15T11:01:00","cpu":{"usage_percent":1},"network":null}'],
    'cpu null network null': ['{"timestamp":"2026-11-15T11:01:00","cpu":null,"network":null}'],
    'nan first': ['{"timestamp":"2026-11-15T11:01:00","cpu":{"usage_percent":NaN}}',
                  '{"timestamp":"2026-11-15T11:02:00","cpu":{"usage_percent":1}}',
                  '{"timestamp":"2026-11-15T11:03:00","cpu":{"usage_percent":2}}'],
    'nan middle': ['{"timestamp":"2026-11-15T11:01:00","cpu":{"usage_percent":1}}',
                   '{"timestamp":"2026-11-15T11:02:00","cpu":{"usage_percent":NaN}}',
                   '{"timestamp":"2026-11-15T11:03:00","cpu":{"usage_percent":2}}'],
    'infinities': ['{"timestamp":"2026-11-15T11:01:00","cpu":{"usage_percent":Infinity}}',
                   '{"timestamp":"2026-11-15T11:02:00","cpu":{"usage_percent":-Infinity}}'],
    'infinity and big int': ['{"timestamp":"2026-11-15T11:01:00","cpu":{"usage_percent":Infinity}}',
                             '{"timestamp":"2026-11-15T11:02:00","cpu":{"usage_percent":' + str(10 ** 400) + '}}'],
}

TRAFFIC = {
    'empty': ([], 'total', NOW),
    'single point': ([point(1, net(100, 200))], 'total', NOW),
    'two points': ([point(1, net(100, 200)), point(2, net(400, 260), second=30)], 'hours_24', NOW),
    'counter reset': ([point(1, net(1000, 50)), point(3, net(10, 70))], 'total', NOW),
    'no network': ([point(1), point(2)], 'total', NOW),
    'network list': ([point(1, network=[1]), point(2, net(1, 2))], 'total', NOW),
    'latest network list': ([point(1, net(1, 2)), point(2, network=[1])], 'total', NOW),
    'strings': ([point(1, net('a', 'b')), point(2, net('c', 'd'))], 'total', NOW),
    'string minus int': ([point(1, net(1, 2)), point(2, net('c', 2))], 'total', NOW),
    'none values': ([point(1, net(None, 2)), point(2, net(None, 2))], 'total', NOW),
    'bools': ([point(1, net(True, True)), point(2, net(False, True), second=10)], 'total', NOW),
    'floats': ([point(1, net(1.5, 2.25)), point(2, net(3.0, 1.0), micro=500)], 'total', NOW),
    'huge ints': ([point(1, net(0, 0)), point(2, net(10 ** 400, 10 ** 20))], 'total', NOW),
    'huge equal ints': ([point(1, net(10 ** 400, 5)), point(2, net(10 ** 400, 5))], 'total', NOW),
    'today': ([point(1, net(1, 1), day=14, hour=23), point(2, net(5, 9), hour=0), point(3, net(9, 10), hour=1)], 'today', NOW),
    'today none': ([point(1, net(1, 1), day=14, hour=23)], 'today', NOW),
    'week sunday': ([point(1, net(1, 1), day=14, hour=13), point(2, net(5, 9), day=15, hour=1)], 'week', NOW),
    'week monday': ([point(59, net(1, 1), day=15, hour=23), point(10, net(5, 9), day=16, hour=0)], 'week', '2026-11-16T00:30:00'),
    'week none': ([point(59, net(1, 1), day=15, hour=23)], 'week', '2026-11-16T00:30:00'),
    'other period': ([point(1, net(1, 2)), point(2, net(3, 4))], 'xyz', NOW),
    'future end': ([point(1, net(1, 2)), point(2, net(10, 20), day=16)], 'total', NOW),
    'backwards time': ([point(5, net(1, 2)), point(1, net(10, 20))], 'total', NOW),
    'micro duration': ([point(1, net(0, 0)), point(1, net(3, 1), micro=7)], 'total', NOW),
}

TRAFFIC_RAW = {
    'network null': (['{"timestamp":"2026-11-15T11:01:00","network":{"bytes_sent":1,"bytes_recv":1}}',
                      '{"timestamp":"2026-11-15T11:02:00","network":null}'], 'total', NOW),
    'nan': (['{"timestamp":"2026-11-15T11:01:00","network":{"bytes_sent":NaN,"bytes_recv":1}}',
             '{"timestamp":"2026-11-15T11:02:00","network":{"bytes_sent":1,"bytes_recv":NaN}}'], 'total', NOW),
    'infinity': (['{"timestamp":"2026-11-15T11:01:00","network":{"bytes_sent":Infinity,"bytes_recv":1}}',
                  '{"timestamp":"2026-11-15T11:02:00","network":{"bytes_sent":Infinity,"bytes_recv":-Infinity}}'], 'total', NOW),
    'year 9999 end': (['{"timestamp":"2026-11-15T11:01:00","network":{"bytes_sent":0,"bytes_recv":0}}',
                       '{"timestamp":"9999-12-31T23:59:59.999999","network":{"bytes_sent":123456789,"bytes_recv":7}}'], 'total', NOW),
    'year 2400 end': (['{"timestamp":"2026-11-15T11:01:00","network":{"bytes_sent":0,"bytes_recv":0}}',
                       '{"timestamp":"2400-01-01T00:00:00.000001","network":{"bytes_sent":1,"bytes_recv":3}}'], 'week', NOW),
}

TREND = {
    'empty': ([], 24, 5),
    'single': ([point(1, net(1, 2, speed_sent=3, speed_recv=4))], 24, 5),
    'five minute groups': ([point(m, net(m * 100, m * 10, speed_sent=m)) for m in (0, 2, 5, 6, 11, 12, 20)], 24, 5),
    'reset': ([point(0, net(100, 100)), point(5, net(10, 300)), point(10, net(20, 5))], 24, 5),
    'interval zero': ([point(0, net(1, 1)), point(0, net(2, 2), micro=1), point(1, net(4, 4))], 24, 0),
    'interval negative': ([point(0, net(1, 1)), point(1, net(2, 2))], 24, -1),
    'interval huge': ([point(0, net(1, 1)), point(1, net(2, 2))], 24, 10 ** 30),
    'equal times': ([point(0, net(1, 1)), point(0, net(5, 5)), point(0, net(7, 7))], 24, 0),
    'backwards': ([point(10, net(1, 1)), point(0, net(5, 5)), point(20, net(9, 9))], 24, 5),
    'no network': ([point(0), point(6)], 24, 5),
    'network list': ([point(0, net(1, 1)), point(6, network=[1])], 24, 5),
    'prev network list': ([point(0, network=[1]), point(6, net(1, 1))], 24, 5),
    'prev network list not due': ([point(0, network=[1]), point(1, net(1, 1))], 24, 5),
    'last network list': ([point(0, net(1, 1)), point(1, network=[1])], 24, 5),
    'strings': ([point(0, net('a', 'b')), point(6, net('c', 'd'))], 24, 5),
    'bools': ([point(0, net(True, False)), point(6, net(False, True))], 24, 5),
    'floats': ([point(0, net(1.5, 0.25)), point(6, net(2.5, 0.5), micro=3)], 24, 5),
    'huge': ([point(0, net(0, 0)), point(6, net(10 ** 400, 1))], 24, 5),
    'hours window': ([point(0, net(0, 0), hour=9), point(6, net(1, 1)), point(16, net(2, 2))], 1, 5),
    'hours negative': ([point(0, net(0, 0))], -1, 5),
}

TREND_RAW = {
    'network null': (['{"timestamp":"2026-11-15T11:00:00","network":null}',
                      '{"timestamp":"2026-11-15T11:01:00","network":{"bytes_sent":1,"bytes_recv":1}}'], 24, 5),
    'nan': (['{"timestamp":"2026-11-15T11:00:00","network":{"bytes_sent":1,"bytes_recv":1}}',
             '{"timestamp":"2026-11-15T11:06:00","network":{"bytes_sent":NaN,"bytes_recv":Infinity,"speed_sent":NaN}}'], 24, 5),
    'year 9999': (['{"timestamp":"2026-11-15T11:00:00","network":{"bytes_sent":1,"bytes_recv":1}}',
                   '{"timestamp":"9999-12-31T23:59:59.999999","network":{"bytes_sent":5,"bytes_recv":9}}'], 24, 5),
}


def capture(call):
    try:
        return enc(call())
    except Exception as error:  # noqa: BLE001 - recorded as the reference
        return error_text(error)


def main():
    out = {'cutoff': CUTOFF, 'now': NOW, 'python': sys.version, 'read_file': [], 'legacy': [],
           'summary': [], 'traffic_stats': [], 'trend': []}
    cutoff = datetime.fromisoformat(CUTOFF)
    directory = tempfile.mkdtemp()
    history = mh.MetricsHistory(directory)
    for name, data in READ_FILES.items():
        with open(history._get_agent_file('a'), 'wb') as handle:
            handle.write(data)
        points, count = history._read_file('a', cutoff)
        out['read_file'].append({'name': name, 'data': base64.b64encode(data).decode(),
                                 'points': [[timestamp.isoformat(), text] for timestamp, text in points], 'count': count})
    for name, data in LEGACY_FILES.items():
        with open(history._get_legacy_file('a'), 'wb') as handle:
            handle.write(data)
        case = {'name': name, 'data': base64.b64encode(data).decode()}
        try:
            case['points'] = [[timestamp.isoformat(), text] for timestamp, text in history._read_legacy('a', cutoff)]
        except ValueError as error:
            case['error'] = 'format'
            case['detail'] = error_text(error)
        except Exception as error:  # noqa: BLE001
            case['error'] = error_text(error)
        out['legacy'].append(case)

    for name, (points, hours) in SUMMARY.items():
        out['summary'].append({'name': name, 'points': points, 'hours': hours, 'now': NOW,
                               'result': capture(lambda: history_with(points, NOW).get_metrics_summary('a', hours=hours))})
    for name, points in SUMMARY_RAW.items():
        out['summary'].append({'name': name, 'points': points, 'hours': 1, 'now': NOW,
                               'result': capture(lambda: history_with(points, NOW).get_metrics_summary('a', hours=1))})
    for name, (points, period, now) in list(TRAFFIC.items()) + list(TRAFFIC_RAW.items()):
        out['traffic_stats'].append({'name': name, 'points': points, 'period': period, 'now': now,
                                     'result': capture(lambda: history_with(points, now).get_traffic_stats('a', period=period))})
    for name, (points, hours, interval) in list(TREND.items()) + list(TREND_RAW.items()):
        out['trend'].append({'name': name, 'points': points, 'hours': hours, 'interval': interval, 'now': NOW,
                             'result': capture(lambda: history_with(points, NOW).get_traffic_trend(
                                 'a', hours=hours, interval_minutes=interval))})
    json.dump(out, sys.stdout, ensure_ascii=False, indent=1)
    sys.stdout.write('\n')


if __name__ == '__main__':
    main()
