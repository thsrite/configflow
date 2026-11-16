"""X04 literal JSON spellings and Content-Type behavior of known public validators."""
import json

from compare import Policy
from harness import Pair, USERNAME, PASSWORD, json_body
from cases.auth import bind_jwt


def decoder(binary, recorder):
    with Pair(binary, recorder) as pair:
        credentials = json.dumps({'username': USERNAME, 'password': PASSWORD})
        for name, body, content_type, expected in [
            ('json-charset', credentials, 'application/json; charset=utf-8', 200),
            ('merge-patch', credentials, 'application/merge-patch+json', 200),
            ('bom', b'\xef\xbb\xbf' + credentials.encode(), 'application/json', 200),
            ('empty', '', 'application/json', 500), ('plain', credentials, 'text/plain', 500),
            ('null', 'null', 'application/json', 500),
            ('duplicate-username', '{"username":"wrong","username":"' + USERNAME + '","password":"' + PASSWORD + '"}', 'application/json', 200)]:
            policy = Policy(start=pair.policy.start, internal_errors={'body.message': '登录失败。原始错误：'}) if expected == 500 else Policy(start=pair.policy.start)
            pair.request('X04', 'POST /api/auth/login', body=body, auth='', headers={'Content-Type': content_type},
                         expect=expected, variant='login/' + name, policy=policy,
                         capture=bind_jwt if expected == 200 else None)
        for literal in ('NaN', 'Infinity', '-Infinity', '9223372036854775808', '9007199254740993', '"\\ud800"'):
            body = credentials[:-1] + ',"ignored_extra":' + literal + '}'
            pair.request('X04', 'POST /api/auth/login', body=body, auth='', expect=200,
                         variant='login/ignored-extra/' + literal,
                         policy=Policy(start=pair.policy.start), capture=bind_jwt)


def settings_and_intervals(binary, recorder):
    vectors = []
    for field, route in [('new_domain', 'POST /api/server-domain'), ('token', 'POST /api/config-token')]:
        for literal in ('NaN', 'Infinity', '-Infinity', '9223372036854775808', '9007199254740993', '60.0', '6e1', 'true', 'null', '"\\ud800"'):
            vectors.append((route, '{"' + field + '":' + literal + '}', 'application/json', 500, field + '/literal/' + literal))
        value = 'http://second.example' if field == 'new_domain' else 'second-synthetic-token'
        vectors.append((route, '{"' + field + '":"first","' + field + '":"' + value + '"}',
                        'application/json', 200, field + '/duplicate-key-last-wins'))
        valid = json_body({field: value})
        for name, body, ct, expected in [('charset', valid, 'application/json; charset=utf-8', 200),
                                        ('merge', valid, 'application/merge-patch+json', 200),
                                        ('bom', b'\xef\xbb\xbf' + valid.encode(), 'application/json', 200),
                                        ('empty', '', 'application/json', 500), ('plain', valid, 'text/plain', 500)]:
            vectors.append((route, body, ct, expected, field + '/content-type/' + name))
    # Each spelling gets a fresh clone. A Go-only surrogate replacement commit
    # must not contaminate the generations of later otherwise valid scenarios.
    for route, body, ct, expected, variant in vectors:
        with Pair(binary, recorder) as pair:
            pair.request('X04', route, body=body, headers={'Content-Type': ct}, expect=expected,
                         variant=variant, writes=True,
                         policy=Policy(start=pair.policy.start, internal_errors={'body.message': ''}) if expected == 500 else pair.policy)
    with Pair(binary, recorder) as pair:
        for endpoint in ('config', 'now', 'test'):
            for literal in ('60.0', '6e1', 'true', '"60"', '9223372036854775808', 'NaN', 'Infinity', '-Infinity'):
                pair.request('X04', 'POST /api/backup/' + endpoint, body='{"keep_latest":' + literal + '}', expect=400,
                             variant='backup/' + endpoint + '/strict-int/' + literal, writes=True)
        for key in ('subscription:missing-synthetic', 'rule_source:missing-synthetic', 'metrics_history_flush'):
            for literal in ('60.0', '6e1', 'true', '"60"', '9223372036854775808', 'NaN'):
                pair.request('X04', 'PUT /api/tasks/interval', body='{"key":"' + key + '","interval_seconds":' + literal + '}',
                             expect=400, variant='interval/' + key + '/' + literal, writes=True)


def silent_content_types(binary, recorder):
    rows = [('POST /api/backup/config', {'keep_latest': 30}),
            ('POST /api/nodes/parse-lines', {'lines': []}),
            ('POST /api/settings/rule-fetch-proxy', {'rule_fetch_proxy': ''})]
    with Pair(binary, recorder) as pair:
        for route, value in rows:
            valid = json_body(value)
            for name, body, ct, expected in [('charset', valid, 'application/json; charset=utf-8', 200),
                                            ('merge', valid, 'application/merge-patch+json', 200),
                                            ('bom', b'\xef\xbb\xbf' + valid.encode(), 'application/json', 200),
                                            ('empty', '', 'application/json', 400), ('plain', valid, 'text/plain', 400)]:
                pair.request('X04', route, body=body, headers={'Content-Type': ct}, expect=expected,
                             variant=route + '/content-type/' + name, writes=True)


GROUPS = {'json-decoder': decoder, 'json-settings-intervals': settings_and_intervals,
          'json-silent-content-types': silent_content_types}
