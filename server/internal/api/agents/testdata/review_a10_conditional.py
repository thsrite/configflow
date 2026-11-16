"""A10 review: Werkzeug references for download_agent_binary's conditional
handling (TestReviewConditionalMatchesWerkzeug).

Usage: python review_a10_conditional.py CASES.json

CASES.json: {"dates": [str, ...], "requests": [{"method", "size", "etag",
"last_modified", "headers": {name: latin-1 str}}, ...]}.
Prints one JSON object: {"dates": [result, ...], "requests": [result, ...]}
- date result: "none" | "ok:<UTC epoch seconds>" | "raise:<Class>:<str(e)>"
  of werkzeug.http.parse_date(value)
- request result: "" (make_conditional raised nothing) | "raise:<Class>:<str(e)>"
  of Response.make_conditional(environ, accept_ranges=True,
  complete_length=size) on a response carrying the given ETag/Last-Modified,
  i.e. what flask.send_file lets reach the view's `except Exception`.
"""
import json
import sys

from werkzeug.http import parse_date
from werkzeug.test import EnvironBuilder
from werkzeug.wrappers import Response


def date_result(value):
    try:
        dt = parse_date(value)
    except Exception as exc:  # noqa: BLE001 - recorded
        return 'raise:' + type(exc).__name__ + ':' + str(exc)
    if dt is None:
        return 'none'
    return 'ok:' + str(int(dt.timestamp()))


def request_result(case):
    builder = EnvironBuilder(method=case['method'], path='/x')
    environ = builder.get_environ()
    for name, value in case['headers'].items():
        environ['HTTP_' + name.upper().replace('-', '_')] = value
    response = Response(b'x' * case['size'], mimetype='application/octet-stream')
    if case['etag']:
        response.headers['ETag'] = case['etag']
    if case['last_modified']:
        response.headers['Last-Modified'] = case['last_modified']
    try:
        response.make_conditional(environ, accept_ranges=True, complete_length=case['size'])
    except Exception as exc:  # noqa: BLE001 - recorded
        return 'raise:' + type(exc).__name__ + ':' + str(exc)
    return ''


cases = json.load(open(sys.argv[1], encoding='utf-8'))
print(json.dumps({'dates': [date_result(v) for v in cases['dates']],
                  'requests': [request_result(c) for c in cases['requests']]}))
