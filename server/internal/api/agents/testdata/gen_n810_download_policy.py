"""Bind complete old Python rows to the approved native HTTP date policy.

Uses only archived Python evidence, datetime and Werkzeug; never invokes Go.
"""
import base64
import hashlib
import json
import re
import sys
from datetime import datetime, timezone
from pathlib import Path
from werkzeug.http import parse_date as old_parse_date
from werkzeug.test import EnvironBuilder
from werkzeug.wrappers import Response
import werkzeug.sansio.http
import werkzeug.wrappers.response

root = Path('server/internal/api/agents')
archive = root / 'testdata/pyref/TestReviewConditionalMatchesWerkzeug.json'
data = json.loads(archive.read_bytes())
rows = []

def canonical(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':')).encode()

def native_date(value):
    patterns = [
        (r'[A-Z][a-z]{2}, [0-9]{2} [A-Z][a-z]{2} [0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2} GMT', '%a, %d %b %Y %H:%M:%S GMT'),
        (r'[A-Z][a-z]+, [0-9]{2}-[A-Z][a-z]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} GMT', '%A, %d-%b-%y %H:%M:%S GMT'),
        (r'[A-Z][a-z]{2} [A-Z][a-z]{2} [ 0-9][0-9] [0-9]{2}:[0-9]{2}:[0-9]{2} [0-9]{4}', '%a %b %d %H:%M:%S %Y'),
    ]
    for pattern, layout in patterns:
        if re.fullmatch(pattern, value or ''):
            try:
                return datetime.strptime(value, layout).replace(tzinfo=timezone.utc)
            except ValueError:
                pass
    return None

def date_result(value, parser):
    try:
        date = parser(value)
        return 'none' if date is None else 'ok:' + str(int(date.timestamp()))
    except Exception as exc:
        return 'raise:' + type(exc).__name__ + ':' + str(exc)

def request_result(case, native):
    # Patch only the date policy, retaining Werkzeug's independent conditional
    # processing. C0 separators outside native Unicode whitespace are invalid
    # Range syntax; replacing them with a nonspace character preserves refusal.
    import werkzeug.http
    parser = native_date if native else old_parse_date
    werkzeug.http.parse_date = parser
    werkzeug.sansio.http.parse_date = parser
    werkzeug.wrappers.response.parse_date = parser
    environ = EnvironBuilder(method=case['method'], path='/x').get_environ()
    for name, value in case['headers'].items():
        if native and name.lower() == 'range':
            value = re.sub(r'[\x1c-\x1f]', '\ufffd', value)
        environ['HTTP_' + name.upper().replace('-', '_')] = value
    response = Response(b'x' * case['size'], mimetype='application/octet-stream')
    if case['etag']:
        response.headers['ETag'] = case['etag']
    if case['last_modified']:
        response.headers['Last-Modified'] = case['last_modified']
    try:
        response.make_conditional(environ, accept_ranges=True, complete_length=case['size'])
        return ''
    except Exception as exc:
        return 'raise:' + type(exc).__name__ + ':' + str(exc)

def add(kind, value, old, native):
    if old == native:
        return
    original = {'kind': kind, 'input': value, 'python': old}
    rows.append({'original': original, 'sha256': hashlib.sha256(canonical(original)).hexdigest(), 'native': native})

for entry in data['entries'].values():
    inputs = json.loads(base64.b64decode(entry['input']))
    outputs = json.loads(base64.b64decode(entry['output']))
    for value, old in zip(inputs['dates'], outputs['dates'], strict=True):
        add('date', value, old, date_result(value, native_date))
    for value, old in zip(inputs['requests'], outputs['requests'], strict=True):
        add('request', value, old, request_result(value, True))

source = (root / 'a10_review_internal_test.go').read_text()
fixed_dates = re.findall(r'\{"((?:\\.|[^"\\])*)", "((?:\\.|[^"\\])*)"\}', source)
for value, old in fixed_dates:
    value, old = json.loads('"' + value + '"'), json.loads('"' + old + '"')
    add('date', value, old, date_result(value, native_date))

etag = '"1793536215.1234567-70000-12345"'
last = 'Sun, 01 Nov 2026 12:30:15 GMT'
fixed_headers = [
 {'Range':'bytes=0-99999999999999999999'}, {'Range':'bytes=0-9223372036854775807'},
 {'Range':'bytes=99999999999999999999-'}, {'Range':'bytes=0-9\xa0'}, {'Range':'\x85bytes=0-9'},
 {'Range':'bytes=80000-', 'If-Range':'01 Nov 2026 12:30:15 +0000'},
 {'Range':'bytes=0-1', 'If-Range':'01 Nov 99999999999 12:30:15 GMT'},
 {'If-Modified-Since':'01 Nov 99999999999 12:30:15 GMT'},
 {'Range':'bytes=0-1','If-Modified-Since':'01 Nov 99999999999 12:30:15 GMT'},
 {'If-Range':'x','If-Modified-Since':'01 Nov 99999999999 12:30:15 GMT'},
 {'Range':'bytes=80000-','If-Range':'x','If-None-Match':'"a"\xa0,\xa0'+etag},
]
for headers in fixed_headers:
    value = dict(method='GET',size=70000,etag=etag,last_modified=last,headers=headers)
    add('request',value,request_result(value,False),request_result(value,True))
unique = {canonical(row['original']): row for row in rows}
output = {'archive_sha256': 'f48d9d4fe53965be138f860382176ba71d21fc1feb4df4b79f0b568ce8bcc557', 'entries_sha256': hashlib.sha256(canonical(data['entries'])).hexdigest(), 'rows': list(unique.values())}
(root / 'testdata/n810_download_policy.json').write_text(json.dumps(output, ensure_ascii=True, indent=2) + '\n')
print(f"{len(unique)} complete bound native HTTP policy rows")
