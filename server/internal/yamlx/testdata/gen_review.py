#!/usr/bin/env python3
"""Generate review.jsonl (review C02): PyYAML 6.0.1 on texts aimed at the constructor
(explicit tags on every node kind, int()/float() edge values, base64, timestamps,
"=" and merge keys, generator order of errors, Python dict key equality) and at the
known yaml.v3 gaps. Synthetic values only. Run from the repository root:

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/yamlx/testdata/gen_review.py

Each line: {"text", "safe", "native", "csafe"}; an outcome is {"value": json} or
{"error": class, "message": str} or {"special": ...}. Mappings with a key that is not
a str are written as {"$pairs": [[key, value], ...]}; datetimes as {"$ts"}, bytes as
{"$bytes"}. "csafe" is subscription_dialers._load (CSafeLoader, else safe_load).
"""
import datetime
import json
import math
import sys
from pathlib import Path

import yaml

assert sys.version_info[:2] == (3, 11), sys.version
from backend.common.oxidns_native import NativeLoader  # noqa: E402
from backend.utils.subscription_dialers import _load as csafe_load  # noqa: E402


def enc(value, active=()):
    if isinstance(value, (dict, list, set, tuple)) and id(value) in active:
        raise RecursionError('recursive')
    active = active + (id(value),)
    if isinstance(value, dict):
        if all(isinstance(key, str) for key in value):
            return {key: enc(item, active) for key, item in value.items()}
        return {'$pairs': [[enc(key, active), enc(item, active)] for key, item in value.items()]}
    if isinstance(value, list):
        return [enc(item, active) for item in value]
    if isinstance(value, datetime.datetime):
        return {'$ts': value.isoformat(' ')}
    if isinstance(value, datetime.date):
        return {'$ts': value.isoformat()}
    if isinstance(value, bytes):
        return {'$bytes': value.hex()}
    if isinstance(value, (set, tuple)):
        raise TypeError('unsupported')
    if isinstance(value, float) and math.isnan(value):
        return 'NaN-float'
    return value


def outcome(load, text):
    try:
        value = load(text)
    except Exception as exc:
        return {'error': type(exc).__name__, 'message': str(exc)}
    try:
        return {'value': json.dumps(enc(value), ensure_ascii=True, separators=(',', ':'))}
    except (TypeError, ValueError, RecursionError) as exc:
        return {'special': str(exc)[:60]}


TAGS = ['!!null', '!!bool', '!!int', '!!float', '!!binary', '!!timestamp', '!!str', '!!seq', '!!map', '!!set', '!!omap',
        '!!pairs', '!!merge', '!!value', '!!python/tuple', '!foo', '!']
BODIES = ['', 'x', '5', 'yes', '[a]', '[]', '{a: 1}', '{}', '{=: 7}', '{=: {=: yes}}', '[{a: 1}]', '[{a: 1, b: 2}]', '"q"', '|\n  a\n']
TEXTS = []
for tag in TAGS:
    for body in BODIES:
        TEXTS.append(f'{tag} {body}\n')
        TEXTS.append(f'k: {tag} {body}\n')
SCALARS = {
    '!!bool': ['maybe', 'YES', 'İ', 'Off', 'ON '],
    '!!int': ['""', '"-"', '"+"', '"0x-1F"', '"0x+1F"', '"0x 1f"', '"١٢"', '"０x1"', '"0b١"', '"0o17"', '"0_"', '"1:2:3"', '"1: 2"',
              '":"', '"-0"', '"00"', '"09"', '"0xzz"', '"1e3"', '" 12 "', '"1_2_3"', '"' + '9' * 4301 + '"', '"0x' + 'f' * 5000 + '"',
              '"' + 'x' * 250 + '"', '"12\\x0034"'],
    '!!float': ['""', '"-"', 'abc', 'INF', '"-.NaN"', '"nan"', '"-nan"', '"infinity"', '"1:30"', '"1:-30"', '"١.٥"', '"0x1p3"', '"1e400"',
                '"--1"', '" 1.5 "', '"İ"', '"1_000.5"', '"1__0"'],
    '!!binary': ['aGk=', 'aGk=aGk=', 'aGk', 'a', 'aG', '"a===="', '"aGk==x"', '"é"', '"éé"', '"aéb€"', '"\U0001F600"', '"a b\\tc"',
                 '"!!"', '"aGVsbG8"', '"=aGk="', '"a=Gk="', '"aG=k="'],
    '!!timestamp': ['2001-13-01', '2001-02-29', '2000-02-29', '0000-01-01', '2001-01-01 25:00:00', '2001-01-01 10:60:00',
                    '2001-01-01 10:00:60', '2001-13-01 10:00:00 +25:00', '2001-01-01 10:00:00 +24:00', '2001-01-01 10:00:00 -23:59',
                    '2001-01-01 10:00:00.1234567', '2001-1-1t1:02:03Z', 'foo', '"2001-01-01\\n"', '{=: 2001-01-01}'],
    '!!null': ['x', '[1]', '{=: 1}'],
}
for tag, values in SCALARS.items():
    for value in values:
        TEXTS.append(f'{tag} {value}\n')
        TEXTS.append(f'- {tag} {value}\n')
TEXTS += [
    # generator order: a deeper error loses to a shallower one
    'a: [!!int x]\nb: !!int y\n', 'a: [[!!int x]]\nb: [!!int y]\n', 'a: {b: !!bool x}\nc: !!int z\n', 'a: !!seq x\nb: !!int y\n',
    'a: !!map [1]\nb: !!int y\n', '- [!!int x]\n- !!float y\n', '- !!omap {a: 1}\n- !!int y\n',
    # "=" retagging is visible through aliases, in construction order
    'a: &m {=: 1}\nb: !!str {=: *m}\n', 'b: !!str {=: *m}\na: &m {=: 1}\n', 'x: {&k =: 1, b: *k}\n', '{b: &k =, x: {*k : 2}}\n',
    '!!str &a {=: *a}\n', '- &e =\n- {*e : 1}\n- *e\n',
    # merge keys mutate shared nodes
    'a: &a {x: 1, <<: {y: 2}}\nb: {<<: *a, z: 3}\nc: *a\n', 'a: &a {<<: *a}\n', 'd: {<<: [&x {a: 1}, *x], b: 2}\n',
    'base: &b {k: 1}\nd: {<<: *b, <<: {j: 2}}\n', 'd: {<<: {<<: {a: 1}, b: 2}, c: 3}\n', 'd: {<<: [{a: 1}, 5]}\n', 'd: {<<: !!map {a: 1}}\n',
    'd: {<<: !!str {a: 1}}\n', '<<: {a: 1}\n', '- <<\n- {<<: x}\n',
    # dict key equality and non-string keys
    '1: a\n1.0: b\ntrue: c\n"1": d\n', '0: a\n-0.0: b\nfalse: c\nno: d\n', '.nan: a\n.nan: b\n', '!!float nan: a\n!!float nan: b\n',
    '-.nan: a\n.NaN: b\n', '~: a\nnull: b\n"": c\n', '2001-01-01: a\n2001-01-01 00:00:00: b\n2001-01-01 00:00:00Z: c\n2001-01-01 08:00:00 +8: d\n',
    '!!binary aGk=: a\nhi: b\n', '0x10: a\n16: b\n0o20: c\n', '1e3: a\n1000.0: b\n', '100000000000000000000.0: a\n1e20: b\n1_0_0_0: c\n',
    'hosts:\n  123: 1.2.3.4\n  b.example.com: 1.2.3.4\n', '? [a]\n: 1\n', '? {a: 1}\n: 1\n', '? !!set {a}\n: 1\n', '? !!seq x\n: 1\n',
    '&a [*a]\n', 'a: &x\n  b: *x\n', '&a {k: *a}\n', '- &l [1]\n- *l\n', 'a: &s !!set {x}\nb: *s\n', '!!omap [a: 1, a: 2]\n',
    '!!pairs [a: 1, b: [*x]]\n', '!!set {a, b: 1, [c]}\n', '!!set [a]\n', '!!set a\n',
    # syntax yaml.v3 read differently
    '[1,000]\n', '{a: 1,000}\n', 'a: [1,000, 2]\n', 'name:\tx\n', 'a: b\t\n', 'a:\t1\n', '- \ta\n', 'k: !!int "0x-1F"\n', 'k: !!int "-0x1F"\n',
    'a: "\\/"\n', '%YAML 1.2\n---\na: 1\n', '%YAML 2.0\n---\na: 1\n', '\ufeff\ufeffa: 1\n', 'a: 1\n...\n', '--- |\n  x\n', 'a: !<!> x\n',
    "a: 'x\n\n  y'\n", 'a: "x\\\n  y"\n', '? a\n? b\n', '- a\n-b\n', 'a: &x 1\nb: &x 2\nc: *x\n', 'a: *u\n',
    '[' * 600 + ']' * 600, '{a: ' * 30 + '1' + '}' * 30,
    # NativeLoader specifics
    '__proto__: 1\n', 'a: 1\na: 2\n', '1: a\n', '? [a]\n: b\n', '<<: {a: 1}\n', '=: 1\n', '!!map ""\n', '!!map x\n', '!!map [a]\n',
    '!!map []\n', '!!set {a: 1, a: 2}\n', 'k: 0x1F\n', 'k: 0X1F\n', 'k: --0x1\n', 'k: -0x1f\n', 'k: 1_0\n', 'k: _0x1\n', 'k: 0o_7\n',
    'k: 1e5\n', 'k: .5\n', 'k: 1.\n', 'k: on\n', 'k: 2001-01-01\n', 'k: !!timestamp 2001-01-01\n', 'k: !!int "0X1F"\n',
    'k: !!int " 0x1f"\n', 'k: !!bool yes\n',
]


def main():
    seen, lines = set(), []
    for text in TEXTS:
        if text in seen:
            continue
        seen.add(text)
        lines.append({'text': text,
                      'safe': outcome(lambda t: yaml.load(t, Loader=yaml.SafeLoader), text),
                      'native': outcome(lambda t: yaml.load(t, Loader=NativeLoader), text),
                      'csafe': outcome(csafe_load, text)})
    path = Path(__file__).with_name('review.jsonl')
    with open(path, 'w', encoding='ascii', newline='\n') as handle:
        for line in lines:
            handle.write(json.dumps(line, ensure_ascii=True, allow_nan=True, separators=(',', ':')) + '\n')
    print(len(lines))


main()
