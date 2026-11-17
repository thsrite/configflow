"""X07 real login/pull/DAV/subscription/import logs and level-filter polling."""
import copy
from urllib.parse import urlencode

from compare import differences
from fakes import WebDAV
from harness import Pair, json_body, send
from seed import PULL
from sources import Source
from subscriptions import configure, Exchange as SourceExchange, blocked_error_projection, capture_notice
from cases.helpers import profile_policy
from cases.pull_logging import events
from cases.subscriptions import SUB, require
from cases.webdav import Exchange as DAVExchange, diagnostic_policy

MARKERS = ('登录失败：', '配置被拉取：', 'WebDAV 手动备份失败：', '从URL获取订阅失败:', '已向配置空间 ')


def poll_value(reply, directory, marker):
    """Remove only validated log envelope identities and O12 line prefixes."""
    value = copy.deepcopy(reply.json())
    raw = (directory / 'app.log').read_text()
    expected_file = str(directory / 'app.log')
    errors = []
    if value.get('log_file') == expected_file:
        value['log_file'] = '<validated-owned-log>'
    else:
        errors.append('wrong-log-file')
    if type(value.get('total_lines')) is int and value['total_lines'] == len(raw.splitlines()):
        value['total_lines'] = '<validated-whole-file-line-count>'
    else:
        errors.append('wrong-total-lines')
    lines = value.get('logs')
    if not isinstance(lines, list) or any(not isinstance(line, str) for line in lines):
        errors.append('invalid-log-lines')
        return {'reply': value, 'violations': errors}
    parsed = events(('\n'.join(lines) + '\n').encode(), marker)
    if len(parsed) != len(lines) or value.get('filtered_lines') != len(lines):
        errors.append('lost-unparsed-line-or-wrong-filtered-count')
    value['logs'] = parsed
    return {'reply': value, 'violations': errors}


def sequence(binary, recorder):
    with Source(b'synthetic rejected subscription') as source, WebDAV() as dav:
        source.status = 403
        url = source.url + '/feed?token=synthetic-source-log-secret'
        backup = {'webdav_url': dav.url, 'webdav_username': dav.username, 'webdav_password': dav.password,
            'webdav_path': '/backup/', 'keep_latest': 1, 'keep_daily': 0, 'auto_backup': False}
        with Pair(binary, recorder, options={'config_token': PULL, 'backup': backup,
                'shared': {'subscriptions': [{**SUB, 'url': url}]}}) as pair:
            pair.policy = profile_policy(pair)
            configure(pair, url)
            variant = 'X07/real-fixed-request-sequence/'
            pair.request('R011-F02', 'POST /api/auth/login', auth='',
                body=json_body({'username': 'synthetic-log-user', 'password': 'synthetic-wrong-login-secret'}),
                expect=401, writes=True, variant=variant + 'failed-login')
            pair.request('R030-S02', 'GET /api/config/mihomo', '/api/config/mihomo?token=' + PULL,
                headers={'User-Agent': 'synthetic-log-sequence'}, auth='', expect=200, writes=True,
                variant=variant + 'token-pull')
            diagnostic_policy(pair, 'space', stored=True)
            pair.request('R016-F15', 'POST /api/backup/now', body=json_body(backup), expect=500, writes=True,
                interaction=DAVExchange(dav, pair, failures={'PUT': 507}, backup=True, expected_status=500, put_count=1),
                variant=variant + 'password-DAV-failure')
            pair.request('R218-F09', 'POST /api/subscriptions/<sub_id>/fetch', '/api/subscriptions/sub-a/fetch',
                body=json_body({'preview': True}), expect=500, writes=True,
                interaction=SourceExchange(pair, source), projection=blocked_error_projection, capture=capture_notice(pair),
                variant=variant + 'token-source-failure')
            pair.request('R137-S01', 'POST /api/profiles/<profile_id>/import', '/api/profiles/other/import',
                body=json_body({'config': {'rule_configs': []}}), expect=200, writes=True,
                variant=variant + 'profile-import')
            available = not pair.unimplemented_mutations
            observed = []
            for index, directory in enumerate(pair.dirs):
                if index and not available:
                    break
                raw = (directory / 'app.log').read_bytes()
                secrets = (PULL, dav.password, 'synthetic-wrong-login-secret', 'synthetic-source-log-secret', url, *pair.tokens)
                leaked = [secret for secret in secrets if secret and secret.encode() in raw]
                local = {'leaked_secret_count': len(leaked), 'polls': []}
                for marker in MARKERS:
                    expected = events(raw, marker)
                    if index == 0:
                        require(len(expected) == 1, 'fixed sequence emits exactly one named event: ' + marker)
                    level = expected[0]['level'] if expected else 'INFO'
                    path = '/api/logs/tail?' + urlencode({'lines': 0, 'search': marker, 'level': level})
                    reply = send(pair.bases[index], 'GET', path, headers={'Authorization': 'Bearer ' + pair.tokens[index]})
                    value = poll_value(reply, directory, marker)
                    value['status'] = reply.status
                    local['polls'].append(value)
                    if index == 0:
                        require(reply.status == 200 and not value['violations'] and value['reply']['success'] is True
                            and value['reply']['logs'] == expected,
                            'real tail level regex returns the complete exact named event and accurate envelope')
                if index == 0:
                    require(not leaked, 'actual sequence logs contain no credential, JWT or full subscription link')
                observed.append(local)
            # Only this finite English implementation diagnosis is covered by
            # the existing O6 exception policy. Chinese, status and all else stay exact.
            pair.policy.internal_error_values['logs.fixed-sequence.polls[2].reply.logs[0].message'] = (
                'WebDAV 手动备份失败：NotEnoughSpace', 'WebDAV 手动备份失败：HTTP 507')
            diffs = differences(*observed, 'logs.fixed-sequence', pair.policy) if available else []
            recorder.record('X07', 'real-five-event-sequence-secrecy-and-level-filter-polling', 'GET /api/logs/tail', 'X07',
                'go-unimplemented-dependency' if not available else 'different' if diffs else 'compared', diffs,
                python_status=200, go_status=200 if available else None, checked_events=len(MARKERS),
                blocked_by=sorted(pair.unimplemented_mutations) if not available else [])


GROUPS = {'logs-real-five-event-sequence-and-level-filter-polling': sequence}
