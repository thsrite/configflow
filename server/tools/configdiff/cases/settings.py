"""settings.py: concrete sync cases; DAV/auto-backup are separate fake groups."""
from urllib.parse import quote, urlsplit
import re

from compare import Difference
from harness import Pair, DesignError, json_body
from seed import CURRENT, RETIRED
from cases.helpers import deny_writes, exception_policy


def sync_settings(binary, recorder):
    with Pair(binary, recorder) as pair:
        def call(rid, suffix, method, path, value=None, **kwargs):
            return pair.request(f'R{rid:03}-{suffix}', method + ' /api/' + path,
                                body=None if value is None else json_body(value),
                                writes=method in ('POST', 'DELETE'), **kwargs)
        call(196, 'S01', 'GET', 'server-domain', expect=200)
        call(197, 'S01', 'POST', 'server-domain', {'new_domain': ' http://example.com:18080/ '}, expect=200)
        call(196, 'S02', 'GET', 'server-domain', expect=200)
        for suffix, body in [('F08', {}), ('F09', {'new_domain': ' \t '}),
                             ('F10', {'new_domain': 123}), ('B01', {'new_domain': 'http://example.com/中文🧪'}),
                             ('B02', {'new_domain': 'x' * 8193})]:
            call(197, suffix, 'POST', 'server-domain', body, expect=500 if suffix == 'F10' else 400 if suffix.startswith('F') else 200,
                 policy=exception_policy(pair) if suffix == 'F10' else pair.policy)
        pair.request('R197-F11', 'POST /api/server-domain', body='{"new_domain":', expect=500,
                     policy=exception_policy(pair), writes=True)
        call(201, 'S01', 'GET', 'settings/rule-fetch-proxy', expect=200)
        proxies = [('S01', 'http://127.0.0.1:18081'), ('S02', 'https://example.com:443'),
                   ('S03', ''), ('S04', ' http://proxy.example:7890/ '),
                   ('B01', 'http://%E7%94%A8%E6%88%B7:p%40ss%25@proxy.example:7890'),
                   ('B02', 'http://' + 'u' * 8193 + ':p@proxy.example:7890')]
        for suffix, proxy in proxies:
            call(202, suffix, 'POST', 'settings/rule-fetch-proxy', {'rule_fetch_proxy': proxy}, expect=200)
        call(201, 'S02', 'GET', 'settings/rule-fetch-proxy', expect=200)
        call(201, 'B01', 'GET', 'settings/rule-fetch-proxy', expect=200, variant='encoded-credentials')
        for suffix, body in [('F08', {}), ('F09', []), ('F10', {'rule_fetch_proxy': 'socks5://proxy.example:1080'}),
                             ('F11', {'rule_fetch_proxy': 'http://proxy.example:bad'}),
                             ('F12', {'rule_fetch_proxy': 'https:///missing'})]:
            call(202, suffix, 'POST', 'settings/rule-fetch-proxy', body, expect=400)
        call(203, 'S01', 'GET', 'settings/subscription-aggregation', expect=200)
        for suffix, body in [('S01', {'enabled': True}), ('S02', {'enabled': False}), ('S03', {}),
                             ('S04', {'enabled': []}), ('B01', {'enabled': None}), ('B02', {'enabled': '长🧪' * 4097})]:
            call(204, suffix, 'POST', 'settings/subscription-aggregation', body, expect=200)
            if suffix == 'S01':
                call(203, 'S02', 'GET', 'settings/subscription-aggregation', expect=200)
        for suffix, body in [('F08', '[]'), ('F09', '{"enabled":')]:
            pair.request('R204-' + suffix, 'POST /api/settings/subscription-aggregation', body=body,
                         expect=500, policy=exception_policy(pair), writes=True)
        for rid, suffix, path, body, prefix in [
            (197, 'F12', 'server-domain', {'new_domain': 'http://failed.example'}, ''),
            (202, 'F13', 'settings/rule-fetch-proxy', {'rule_fetch_proxy': ''}, None),
            (204, 'F10', 'settings/subscription-aggregation', {'enabled': True}, '')]:
            with deny_writes(pair):
                call(rid, suffix, 'POST', path, body, expect=500,
                     policy=pair.policy if prefix is None else exception_policy(pair, prefix))
        call(205, 'S02', 'GET', 'settings/web-port', expect=200)
        for suffix, body, status in [('F08', {}, 400), ('F09', {'port': '18080'}, 400),
                                     ('F10', {'port': True}, 400), ('F11', {'port': 0}, 400),
                                     ('F12', {'port': 65536}, 400), ('F13', {'port': 3001}, 409),
                                     ('F16', {'port': 18080}, 503), ('B01', {'port': 1}, 503),
                                     ('B02', {'port': 65535}, 503)]:
            call(206, suffix, 'POST', 'settings/web-port', body, expect=status)
        for suffix, body, status in [('F08', {}, 400), ('F09', {'mode': 'invalid'}, 400), ('F10', [], 400),
                                     ('F11', {'mode': 'always'}, 409)]:
            call(199, suffix, 'POST', 'settings/dependencies/sub-store/mode', body, expect=status)
        call(200, 'F11', 'POST', 'settings/dependencies/sub-store/update', {}, expect=409)
        call(228, 'S01', 'GET', 'version', expect=200)
        from cases.setup import Stable
        pair.request('R228-F01', 'GET /api/version', method='POST', expect=405,
                     writes=True, interaction=Stable(pair))
        pair.request('R228-B01', 'GET /api/version', auth='invalid-synthetic-jwt', expect=200)
    # Old imported values must be observed, not normalized by our fixture.
    with Pair(binary, recorder, options={'system_config': {'server_domain': '',
                    'subscription_aggregation_enabled': 'legacy-string'}}) as pair:
        def bind_origin(replies, policy):
            values = [r.json().get('server_domain', '') for r in replies]
            for index, value in enumerate(values):
                parsed = urlsplit(value)
                valid = (parsed.scheme == 'http' and parsed.port == urlsplit(pair.bases[index]).port
                         and not parsed.path and parsed.hostname == urlsplit(values[0]).hostname)
                if not valid:
                    if index == 0:
                        raise DesignError('Python automatic server origin does not describe its listener')
                    return [Difference('body.server_domain', 'wrong-auto-origin', '<corresponding listener>', '<invalid>')]
            policy.bind(*values, r'http://[0-9.]+:[0-9]+', 'automatic-server-origin')
            return []
        pair.request('R196-B01', 'GET /api/server-domain', variant='empty-auto-origin', expect=200, capture=bind_origin)
        pair.request('R203-B01', 'GET /api/settings/subscription-aggregation', expect=200)


def config_token(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.request('R019-S01', 'GET /api/config-token', expect=200)
        pair.request('R020-S01', 'POST /api/config-token', body=json_body({'token': ' synthetic-pull '}), expect=200, writes=True)
        pair.request('R019-S02', 'GET /api/config-token', expect=200)
        for suffix, body in [('S02', {'generate': True}), ('S03', {'token': 'manual-loses', 'generate': True})]:
            def capture(replies, policy):
                tokens = [r.json().get('config_token', '') for r in replies]
                if not re.fullmatch(r'[A-Za-z0-9]{32}', tokens[0]):
                    raise DesignError('Python generated token format changed')
                if not re.fullmatch(r'[A-Za-z0-9]{32}', tokens[1]):
                    return [Difference('body.config_token', 'token-format', '<32-alphanumeric>', tokens[1])]
                policy.bind(*tokens, r'[A-Za-z0-9]{32}', 'generated-config-token')
                if any(v == 'manual-loses' for v in tokens):
                    return [Difference('body.config_token', 'not-generated', tokens[0], tokens[1])]
                return []
            pair.request('R020-' + suffix, 'POST /api/config-token', body=json_body(body),
                         expect=200, capture=capture, writes=True)
        for suffix, body in [('F08', {}), ('F09', {'token': ' \t '}),
                             ('F10', {'token': 'prefix-' + CURRENT}),
                             ('F11', {'token': quote(RETIRED, safe='')}), ('F12', {'token': 1})]:
            pair.request('R020-' + suffix, 'POST /api/config-token', body=json_body(body),
                         expect=500 if suffix == 'F12' else 400, writes=True,
                         policy=exception_policy(pair) if suffix == 'F12' else pair.policy)
        for suffix, token, variant in [('B01', '中文🧪-% +/?', 'unicode'),
                                      ('B02', 'x' * 8192, '8192'), ('B02', 'x' * 8193, '8193')]:
            pair.request('R020-' + suffix, 'POST /api/config-token', body=json_body({'token': token}),
                         variant=variant, expect=200, writes=True)
            pair.request('R020-' + suffix, 'POST /api/config-token', '/api/config-token?token=' + quote(token, safe=''),
                         body=json_body({'token': token}), auth='', variant=variant + '/management-auth', expect=401, writes=True)
        with deny_writes(pair):
            pair.request('R020-F13', 'POST /api/config-token', body=json_body({'token': 'write-must-fail'}),
                         expect=500, writes=True, policy=exception_policy(pair))
            pair.request('R018-F08', 'DELETE /api/config-token', expect=500, writes=True, policy=exception_policy(pair))
        for suffix in ('S01', 'S02'):
            pair.request('R018-' + suffix, 'DELETE /api/config-token', expect=200, writes=True)
        pair.request('R018-B01', 'DELETE /api/config-token', auth='', expect=401, writes=True)
    with Pair(binary, recorder, options={'config_token': 'prefix-' + CURRENT}) as pair:
        pair.request('R019-B01', 'GET /api/config-token', expect=200,
                     check=lambda r: require('[REDACTED]' in r.json()['config_token'], 'old token redaction'))


def require(value, label):
    if not value:
        raise DesignError('Python fixture precondition: ' + label)


def backup_validation(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.request('R014-S01', 'GET /api/backup/config', expect=200)
        good = {'webdav_url': 'http://dav.example', 'webdav_username': 'synthetic-user',
                'webdav_password': 'synthetic-pass', 'webdav_path': '/backup/', 'auto_backup': False,
                'keep_latest': 30, 'keep_daily': 30}
        for suffix, data in [('S01', good), ('S02', {'keep_latest': 2}),
                             ('S03', {'webdav_password': '******'}), ('S04', {'webdav_password': ''}),
                             ('B01', {'keep_latest': 0}), ('B02', {'keep_latest': 1000}),
                             ('B03', {'keep_daily': 3650}), ('B04', {'webdav_path': '/中文🧪/%空 格/'})]:
            pair.request('R015-' + suffix, 'POST /api/backup/config', body=json_body(data), expect=200, writes=True)
            if suffix == 'S03':
                pair.request('R014-S02', 'GET /api/backup/config', expect=200,
                             check=lambda r: require(r.json()['webdav_password'] == '******', 'backup password mask'))
        invalid = [('empty', {}), ('array', []), ('unknown', {'not_setting': True}),
                   ('text-type', {'webdav_url': 1}), ('auto-type', {'auto_backup': 1}),
                   ('keep-negative', {'keep_latest': -1}), ('keep-large', {'keep_latest': 1001}),
                   ('daily-large', {'keep_daily': 3651}), ('keep-bool', {'keep_latest': True})]
        for rid, path in [(15, 'config'), (16, 'now'), (17, 'test')]:
            ids = [8, 9, 10, 11, 12, 13, 14, 15, 16] if rid == 15 else [9, 8, 10, 11, 12, 13, 13, 13, 13]
            for (label, data), number in zip(invalid, ids):
                pair.request(f'R{rid:03}-F{number:02}', 'POST /api/backup/' + path,
                             body=json_body(data), variant=label, expect=400, writes=True)
        for missing, suffix in [('webdav_url', 'F14'), ('webdav_username', 'F15'), ('webdav_password', 'F16')]:
            data = {**good, missing: ''}
            pair.request('R017-' + suffix, 'POST /api/backup/test', body=json_body(data), expect=400, writes=True)
        # upload_backup records a task failure even though validation rejects upload.
        # That persistent async task record is compared in the DAV/task groups.
    for legacy in ({}, {'keep_latest': True, 'keep_daily': -1}):
        with Pair(binary, recorder, options={'backup': legacy}) as pair:
            pair.request('R014-B01', 'GET /api/backup/config', variant='legacy-' + str(bool(legacy)), expect=200)


GROUPS = {'settings-sync': sync_settings, 'settings-token': config_token,
          'settings-backup-validation': backup_validation}
