"""Repair attribution from config-findings-fixes.md and git commit subjects.

Repair provenance requires an actual successful recheck. Explicit approvals
require exact recorded differences. Raw case verdicts/evidence never change.
"""
import re
import json
from pathlib import Path
from functools import lru_cache

from compare import differences

DECISIONS = '[behaviour-changes.md · {item}](behaviour-changes.md#config-step-b--coordinator-decisions)'


@lru_cache(maxsize=1)
def c29_vectors():
    """Read finite observed identities, not replacement response goldens."""
    return json.loads((Path(__file__).parent / 'testdata/c29-approved-vectors.json').read_text())


def c29_message_basis(item, raw, statuses):
    if statuses != (400, 400) or len(raw) != 1:
        return ''
    for vector in c29_vectors()['messages']:
        if all(item[key] == vector[key] for key in ('case', 'group', 'route', 'variant')):
            expected = [{'path': 'body.message', 'kind': 'value', 'python': vector['python'], 'go': vector['go']}]
            if not differences(expected, raw):
                anchor = {'N7-3d': 'n7-3d-share--oxidns-readers',
                    'N7-3': 'n7-3--native-yaml-readers-subssubstore',
                    'N8-6': 'n8-6--native-request-binding-and-correct-bad-request-statuses'}[vector['decision']]
                label = vector.get('label', 'C29 / ' + vector['decision'])
                return '[behaviour-changes.md · ' + label + '](behaviour-changes.md#' + anchor + ')'
    return ''


def native_yaml_layout_basis(item, raw, statuses):
    """Accept N7-4 layout only, with exact typed Nodes and duplicate rejection."""
    vectors = {('rule-dns-choices', case, variant): code for case, variant, code in [
        ('R189-B02', 'native', 200), ('R189-S04', 'native', 200),
        ('R189-S02', 'native/follow', 200), ('R189-S03', 'native/default', 200),
        *[('R189-S01', 'native/' + choice, 200) for choice in ('blackhole', 'direct', 'nodata', 'nxdomain', 'proxy', 'refused')],
        *[(case, 'native/', 400) for case in ('R189-F08', 'R189-F09', 'R189-F10', 'R189-F11', 'R189-F13', 'R189-F15')],
        ('R189-F12', "native/'unknown'", 400), ('R189-F12', 'native/1', 400)]}
    vectors['rule-dns-selectors-types', 'R189-S01', 'cleanup-prepare/True'] = 200
    vectors['rule-dns-explicit-and-reorder', 'R195-B03', 'real-recognized-native-provider-order-rebuild'] = 200
    code = vectors.get((item['group'], item['case'], item['variant']))
    route = 'POST /api/rules/reorder' if item['case'] == 'R195-B03' else 'PUT /api/rules/dns'
    allowed_paths = {'db.fields.profile/default/oxidns.value.native_config'}
    if code is None or item['route'] != route:
        vector = next((vector for vector in c29_vectors()['layouts']
            if all(item[key] == vector[key] for key in ('case', 'group', 'route', 'variant'))), None)
        if vector is None: return ''
        code, allowed_paths = vector['code'], set(vector['paths'])
    if statuses != (code, code) or len(raw) != len(allowed_paths) or {row['path'] for row in raw} != allowed_paths:
        return ''
    if any(row['kind'] != 'value' for row in raw): return ''
    if not all(yaml_row_equal(row) for row in raw): return ''
    return '[behaviour-changes.md · N7-4](behaviour-changes.md#n7-4--原生-yaml-writer-与-python-类型输出-bug-修正)'


def yaml_row_equal(row):
    """Preserve every typed node, sequence order and string value."""
    if any(type(row[side]) is not str or len(row[side]) > 262144 for side in ('python', 'go')): return ''
    if '\r' in row['go'] or '\t' in row['go'] or any(
            (len(line) - len(line.lstrip(' '))) % 2 for line in row['go'].split('\n') if line.strip()): return ''
    import yaml
    def graph(value):
        from yaml.tokens import ScalarToken
        spans = [(token.start_mark.index, token.end_mark.index) for token in yaml.scan(value) if isinstance(token, ScalarToken)]
        if any(char == '#' and not any(start <= index < end for start, end in spans)
                for index, char in enumerate(value)):
            raise ValueError('unreviewed generated comment')
        seen, budget = set(), [10000]
        def visit(node):
            budget[0] -= 1
            if budget[0] < 0 or node is None or id(node) in seen: raise ValueError('unexpected alias or oversized native document')
            seen.add(id(node))
            if isinstance(node, yaml.ScalarNode):
                if node.tag not in {'tag:yaml.org,2002:' + tag for tag in ('str', 'int', 'bool', 'null', 'float')}:
                    raise ValueError('unexpected scalar tag')
                return node.tag, node.value
            if isinstance(node, yaml.SequenceNode) and node.tag == 'tag:yaml.org,2002:seq':
                return node.tag, tuple(visit(child) for child in node.value)
            if isinstance(node, yaml.MappingNode) and node.tag == 'tag:yaml.org,2002:map':
                keys, fields = set(), []
                for key, child in node.value:
                    if not isinstance(key, yaml.ScalarNode) or key.tag != 'tag:yaml.org,2002:str' or key.value in keys:
                        raise ValueError('duplicate or non-string mapping key')
                    keys.add(key.value)
                    fields.append((key.value, visit(child)))
                return node.tag, tuple(sorted(fields))
            raise ValueError('unexpected node')
        return visit(yaml.compose(value, Loader=yaml.SafeLoader))
    try:
        if graph(row['python']) != graph(row['go']): return ''
    except (ValueError, RecursionError, yaml.YAMLError):
        return ''
    return True


def rules_boundary_basis(item, raw, statuses):
    """Approve finite observed C32 HEAD and Step B #3 error-envelope vectors."""
    identity = item['case'], item['group'], item['route'], item['variant']
    if identity == ('X06', 'http-head', 'GET /api/oxidns/settings', 'head//api/oxidns/settings'):
        from cases.http_shell import OXIDNS_HEAD_ETAG
        expected = [{'path': 'status', 'kind': 'value', 'python': 400, 'go': 200},
            {'path': 'headers.etag', 'kind': 'value', 'python': '', 'go': OXIDNS_HEAD_ETAG}]
        if statuses == (400, 200) and not differences(expected, raw):
            return '[behaviour-changes.md · C29 HEAD只读](behaviour-changes.md#head-只读c29--c31--c32)'
        return ''
    if identity in {('X06', 'http-head', 'GET ' + path, 'head/' + path)
            for path in ('/api/rules', '/api/rule-sets')}:
        expected = [{'path': 'status', 'kind': 'value', 'python': 500, 'go': 200},
            {'path': 'headers.content-type', 'kind': 'value', 'python': 'text/html; charset=utf-8', 'go': 'application/json'}]
        if statuses == (500, 200) and not differences(expected, raw):
            return '[behaviour-changes.md · C32](behaviour-changes.md#c32--head-规则接口只读python-bug-修正)'
        return ''
    internal = ('The server encountered an internal error and was unable to complete your request. '
        'Either the server is overloaded or there is an error in the application.')
    missing = ('The requested URL was not found on the server. If you entered the URL manually '
        'please check your spelling and try again.')
    vectors = {
        ('R179-F08', 'rules-sets', 'POST /api/rule-sets', ''): (500, internal),
        ('R181-F09', 'rules-sets', 'PUT /api/rule-sets/<rule_set_id>', ''): (404, 'Rule not found'),
        ('R185-F16', 'rules-update', 'PUT /api/rules/<rule_id>', ''): (404, 'Rule not found'),
        ('R195-F11', 'rules-reorder', 'POST /api/rules/reorder', ''): (404, "以下规则 id 不存在: ['missing']"),
        ('R195-F13', 'rules-reorder', 'POST /api/rules/reorder', "{'ids': [1]}"): (404, '以下规则 id 不存在: [1]'),
        ('R195-F13', 'rules-reorder', 'POST /api/rules/reorder', "{'ids': [[1]]}"): (500, internal),
        **{('X06', 'http-options-routing', 'GET ' + path, 'encoded-slash/GET ' + path): (404, missing)
            for path in ('/api/rules/local/<name>', '/api/profiles/<profile_id>/rules/local/<name>')},
    }
    if identity not in vectors: return ''
    code, message = vectors[identity]
    title = {404: 'Not Found', 500: 'Internal Server Error'}[code]
    html = ['<!doctype html>', '<html lang=en>', f'<title>{code} {title}</title>', f'<h1>{title}</h1>',
        '<p>' + message.replace("'", '&#39;') + '</p>']
    expected = [{'path': 'headers.content-type', 'kind': 'value', 'python': 'text/html; charset=utf-8', 'go': 'application/json'},
        {'path': 'body', 'kind': 'type', 'python': html, 'go': {'message': message, 'success': False}}]
    if statuses == (code, code) and not differences(expected, raw): return DECISIONS.format(item='Step B #3')
    return ''


def native_binding_basis(item):
    """Match only the nine actually observed, approved N8-6 login transitions."""
    if item.get('status') != 'open' or item.get('route') != 'POST /api/auth/login':
        return ''
    bad = 'The browser (or proxy) sent a request that this server could not understand.'
    media = "Did not attempt to load JSON data because the request Content-Type was not 'application/json'."
    variants = {
        ('R011-F06', 'auth-routes', ''): ("登录失败。原始错误：'list' object has no attribute 'get'", 400, bad),
        ('R011-F07', 'auth-routes', ''): ('登录失败。原始错误：400 Bad Request: ' + bad, 400, bad),
        ('R011-F08', 'auth-routes', ''): ('登录失败。原始错误：415 Unsupported Media Type: ' + media, 415, media),
        ('X04', 'json-decoder', 'login/empty'): ('登录失败。原始错误：400 Bad Request: ' + bad, 400, bad),
        ('X04', 'json-decoder', 'login/null'): ("登录失败。原始错误：'NoneType' object has no attribute 'get'", 400, bad),
        ('X04', 'json-decoder', 'login/plain'): ('登录失败。原始错误：415 Unsupported Media Type: ' + media, 415, media),
    }
    raw = item.get('remaining_differences', [])
    identity = item['case'], item['group'], item['variant']
    def entry(path, kind, python, go):
        return {'path': path, 'kind': kind, 'python': python, 'go': go}
    if identity in variants:
        message, code, target = variants[identity]
        if (item.get('python_status'), item.get('go_status')) != (500, code): return ''
        expected = [entry('status', 'value', 500, code), entry('body.message', 'internal-error-prefix', message, target)]
    elif (item['case'] == 'X04' and item['group'] == 'json-decoder'
            and item['variant'] in ('login/ignored-extra/NaN', 'login/ignored-extra/Infinity', 'login/ignored-extra/-Infinity')
            and (item.get('python_status'), item.get('go_status')) == (200, 400)):
        import jwt
        from harness import SECRET, USERNAME
        tokens = [row['python'] for row in raw if row['path'] == 'body.token' and row['kind'] == 'missing-in-go']
        if len(tokens) != 1: return ''
        token = tokens[0]
        try:
            # The live capture already checked the request-time window. Review
            # may occur after expiry, so recheck signature/shape/duration here.
            claims = jwt.decode(token, SECRET, algorithms=['HS256'], options={'verify_exp': False, 'verify_iat': False})
            if (set(claims) != {'username', 'iat', 'exp'} or claims['username'] != USERNAME
                    or type(claims['iat']) is not int or type(claims['exp']) is not int
                    or claims['exp'] - claims['iat'] != 86400): return ''
        except (jwt.PyJWTError, ValueError, TypeError):
            return ''
        expected = [entry('body.token', 'invalid-Go-JWT', '<valid-HS256>', '<invalid>'),
            entry('status', 'value', 200, 400), entry('body.expiresIn', 'missing-in-go', 86400, None),
            entry('body.message', 'extra-in-go', None, bad), entry('body.success', 'value', True, False),
            entry('body.token', 'missing-in-go', token, None), entry('body.username', 'missing-in-go', USERNAME, None)]
    else:
        return ''
    key = lambda row: (row['path'], row['kind'])
    if differences(sorted(expected, key=key), sorted(raw, key=key)): return ''
    return '[behaviour-changes.md · N8-6](behaviour-changes.md#n8-6--native-request-binding-and-correct-bad-request-statuses)'


def accepted_basis(item):
    if item['status'] != 'open':
        return ''
    native = native_binding_basis(item)
    if native:
        return native
    raw = item.get('remaining_differences', [])
    values = {entry['path']: entry for entry in raw}
    if len(values) != len(raw):
        return ''
    statuses = item.get('python_status'), item.get('go_status')
    if any(type(value) is not int for value in statuses):
        return ''
    boundary = rules_boundary_basis(item, raw, statuses)
    if boundary:
        return boundary
    message = c29_message_basis(item, raw, statuses)
    if message:
        return message
    layout = native_yaml_layout_basis(item, raw, statuses)
    if layout:
        return layout
    c35_path = 'db.fields.system//backup.value.webdav_password'
    c35_revision = 'db.fields.system//backup.revision'
    if (item['case'] == 'R015-S04' and item['group'] == 'settings-backup-validation'
            and item['route'] == 'POST /api/backup/config' and item['variant'] == '' and statuses == (200, 200)
            and set(values) == {c35_path, c35_revision} and values[c35_path]['kind'] == 'value'
            and values[c35_path]['python'] == '' and values[c35_path]['go'] == 'synthetic-pass'
            and (values[c35_revision]['python'], values[c35_revision]['go']) == (8, 6)):
        return '[behaviour-changes.md · C35空密码保留](behaviour-changes.md#c35webdav-凭据安全与表单密码保留)'
    reordered = {
        'R103-F08': (404, "以下节点 id 不存在: ['ids 必须是数组']", 'ids 必须是数组'),
        'R103-F09': (404, "以下节点 id 不存在: ['id 必须是互不重复的字符串']", 'id 必须是互不重复的字符串'),
        'R103-F10': (404, "以下节点 id 不存在: ['id 必须是互不重复的字符串']", 'id 必须是互不重复的字符串'),
        'R103-F11': (404, "以下节点 id 不存在: ['nodes 必须是对象数组']", 'nodes 必须是对象数组'),
        'R103-F14': (500, "'list' object has no attribute 'get'", '请求体必须是 JSON 对象'),
        'R223-F08': (404, "以下订阅 id 不存在: ['id 必须是互不重复的字符串']", 'id 必须是互不重复的字符串'),
        'R223-F09': (404, "以下订阅 id 不存在: ['id 必须是互不重复的字符串']", 'id 必须是互不重复的字符串'),
        'R223-F10': (404, "以下订阅 id 不存在: ['ids 必须是数组']", 'ids 必须是数组'),
        'R223-F12': (500, '订阅操作失败', '请求体必须是 JSON 对象'),
    }
    if item['case'] in reordered:
        py_status, py_message, go_message = reordered[item['case']]
        nodes = item['case'].startswith('R103')
        if (item['group'] == ('nodes-reorder' if nodes else 'subscriptions-reorder') and item['variant'] == ''
                and item['route'] == ('POST /api/nodes/reorder' if nodes else 'POST /api/subscriptions/reorder')
                and statuses == (py_status, 400) and set(values) == {'status', 'body.message'}
                and (values['status']['python'], values['status']['go']) == statuses
                and (values['body.message']['python'], values['body.message']['go']) == (py_message, go_message)):
            return DECISIONS.format(item='Step B #1')
    head_routes = ('/api/aggregations', '/api/mosdns/api-settings', '/api/nodes', '/api/subscriptions')
    if (item['case'] == 'X06' and item['group'] == 'http-head' and statuses == (500, 500)
            and any(item['route'] == 'GET ' + route and item['variant'] == 'head/' + route for route in head_routes)
            and set(values) == {'headers.content-type'}
            and (values['headers.content-type']['python'], values['headers.content-type']['go']) ==
                ('text/html; charset=utf-8', 'application/json')):
        return DECISIONS.format(item='Step B #3')
    if (item['case'] == 'X04' and item['group'] == 'json-settings-intervals' and statuses == (500, 400)
            and item['variant'] in ('token/literal/"\\ud800"', 'new_domain/literal/"\\ud800"')
            and item['route'] == ('POST /api/config-token' if item['variant'].startswith('token/') else 'POST /api/server-domain')
            and set(values) == {'status', 'body.message'}
            and values['status']['python'] == 500 and values['status']['go'] == 400
            and values['body.message']['go'] == '请求内容包含无效的 Unicode 字符'
            and isinstance(values['body.message']['python'], str)
            and re.fullmatch(r"'utf-8' codec can't encode character '\\ud800' in position [0-9]+: surrogates not allowed",
                             values['body.message']['python'])):
        return DECISIONS.format(item='Step B #2')
    legacy = 'db.fields.profile/default/domain_discovery.value.synthetic_legacy_extension'
    if (item['case'] == 'R058-B03' and item['variant'] == 'stored-legacy-extension' and statuses == (200, 200)
            and item['route'] == 'PUT /api/domain-discovery/settings' and item['group'] == 'discovery-settings'
            and set(values) == {legacy} and values[legacy]['kind'] == 'extra-in-go'
            and values[legacy]['python'] is None and not differences({'flag': False, 'values': []}, values[legacy]['go'])):
        return DECISIONS.format(item='R058-B03')
    if (statuses == (200, 200) and item['route'] in ('GET /api/domain-discovery/agents', 'GET /api/domain-discovery/domains')
            and raw and all(
                row['kind'] == 'extra-in-go' and row['python'] is None and (
                    (re.fullmatch(r'body\.agents\[\d+\]\.store_state', row['path']) and row['go'] == 'ok')
                    or (re.fullmatch(r'body\.agents\[\d+\]\.store_message', row['path']) and row['go'] == ''))
                for row in raw)):
        return '[behaviour-changes.md · M18](behaviour-changes.md#m18--域名发现流量文件损坏状态与界面恢复入口)'
    version_method = (item['variant'] == '' and statuses == (405, 405) and (item['case'], item['group'], item['route']) in {
        ('R228-F01', 'settings-sync', 'GET /api/version'), ('R012-F01', 'auth-routes', 'GET /api/auth/status')})
    unknown_api = (item['case'] == 'X06' and item['group'] == 'http-options-routing' and statuses == (404, 404)
        and item['route'] == 'GET /api/subscriptions' and item['variant'] in {
            'login/trailing-slash', 'login/unknown-admin', 'open/trailing-slash', 'open/unknown-admin', 'open/unknown-anonymous'})
    mosdns_storage = (item['route'] == 'POST /api/mosdns/settings' and item['case'] == 'R090-F10'
                     and item['group'] == 'mosdns-whole-editor-failures'
                     and item['variant'] == 'SQLite-trigger-rejects-real-editor-commit' and statuses == (500, 500))
    provider_storage = (item['group'] == 'provider-named-graph-and-file-failures'
                        and item['variant'] == 'last-good-provider-output-path-is-directory' and statuses == (500, 500)
                        and (item['case'], item['route']) in {
                            ('R220-F17', 'GET /api/subscriptions/<sub_id>/proxies'),
                            ('R154-F17', 'GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies')})
    if not (version_method or unknown_api or mosdns_storage or provider_storage) and (item['route'] != 'POST /api/subscriptions/<sub_id>/fetch' or item['variant'] != ''):
        return ''
    if version_method:
        status, title = 405, 'Method Not Allowed'
        description = 'The method is not allowed for the requested URL.'
        expected = {'message': description, 'success': False}
        fields, decision = {'headers.content-type', 'body'}, 'Step B #3'
    elif unknown_api:
        status, title = 404, 'Not Found'
        description = ('The requested URL was not found on the server. If you entered the URL manually '
                       'please check your spelling and try again.')
        expected = {'message': description, 'success': False}
        fields, decision = {'headers.content-type', 'body'}, 'Step B #3'
    elif mosdns_storage or provider_storage:
        status, title = 500, 'Internal Server Error'
        description = ('The server encountered an internal error and was unable to complete your request. '
                       'Either the server is overloaded or there is an error in the application.')
        expected = {'message': description, 'success': False}
        fields, decision = {'headers.content-type', 'body'}, 'Step B #3'
    elif item['case'] == 'R218-F10' and statuses == (400, 400):
        status, title = 400, 'Bad Request'
        description = 'The browser (or proxy) sent a request that this server could not understand.'
        expected = {'message': description, 'success': False}
        fields, decision = {'headers.content-type', 'body'}, 'Step B #3'
    elif item['case'] == 'R218-F11' and statuses == (500, 400):
        status, title = 500, 'Internal Server Error'
        description = ('The server encountered an internal error and was unable to complete your request. '
                       'Either the server is overloaded or there is an error in the application.')
        expected = {'message': '请求体必须是 JSON 对象', 'success': False}
        fields, decision = {'status', 'headers.content-type', 'body'}, 'Step B #1'
    else:
        return ''
    html = ['<!doctype html>', '<html lang=en>', '<title>' + str(status) + ' ' + title + '</title>',
            '<h1>' + title + '</h1>', '<p>' + description + '</p>']
    if (set(values) == fields and values['headers.content-type']['python'] == 'text/html; charset=utf-8'
            and values['headers.content-type']['go'] == 'application/json'
            and not differences(html, values['body']['python']) and not differences(expected, values['body']['go'])
            and ('status' not in values or (values['status']['python'], values['status']['go']) == (500, 400))):
        return DECISIONS.format(item=decision)
    return ''

METADATA_GROUPS = {
    'delivery-graph', 'delivery-graph-collisions', 'merged-provider-data', 'merged-provider-timezone',
    'subscription-provider-formats', 'subscription-provider-head', 'subscription-provider-selector',
    'subscription-provider-timezone', 'subscription-provider-validation', 'provider-concurrent-clients',
    'provider-last-good-lifecycle', 'provider-auth-environments',
}


def repair_commits(item):
    if item['status'] != 'closed':
        return []
    group = item['group']
    paths = {difference['path'] for difference in item['differences']}
    if (group == 'rule-proxy-local-cache'
            and item.get('variant') == 'absolute-query-dispatches-internal-content-endpoint'
            and (item.get('case'), item.get('route')) in {
                ('R087-B91', 'GET /api/mosdns/rule-proxy'),
                ('R138-B91', 'GET /api/profiles/<profile_id>/mosdns/rule-proxy')}
            and (item.get('python_status'), item.get('go_status')) == (200, 200)
            and paths == {'status', 'headers.content-type', 'body'}):
        return ['4786d7f86']
    for vector in json.loads((Path(__file__).parent / 'testdata/cfg-harness-repairs.json').read_text()):
        if (all(item.get(key) == vector[key] for key in ('case', 'group', 'variant', 'route'))
                and sorted(paths) == vector['original_paths']):
            return ['2896b8d9e']
    if ((group == 'rule-DNS-real-new-native-validation-issue'
            and paths == {'db.fields.profile/default/updated_at.value'})
            or (group == 'oxidns-control-output' and item['case'] == 'G11'
                and item['variant'] in ('log/off/False/draft', 'log/off/True/draft')
                and paths == {'body.config.log.level'})):
        return ['b93269d6d']
    timestamp_groups = {'rules-create', 'rules-sets', 'rules-update', 'rule-dns-selectors-types',
        'rules-batch', 'rules-concurrent-batch', 'rules-compose-success', 'rules-compose-remote',
        'rule-dns-choices', 'rules-labels-policy', 'rule-dns-native-locked',
        'rule-dns-explicit-and-reorder', 'rules-reorder'}
    if group in timestamp_groups and paths and paths <= {
            'db.fields.profile/default/updated_at.value', 'db.fields.profile/other/updated_at.value'}:
        return ['fd6355ce0']
    if paths and paths <= {'files.notifications.json'}:
        return ['8f3d36ad']
    if group in METADATA_GROUPS:
        return ['7b904725']
    if group == 'merged-provider-remote':
        return ['cb0cc9fa'] if item['case'] == 'R222-F11' else ['7b904725']
    if group in ('discovery-init', 'discovery-region-route'):
        return ['041ab5a6']
    if group == 'subscriptions-cleanup':
        return ['e9187b9f']
    if group == 'subscription-fetch-failures':
        return ['0313e086', '8f3d36ad'] if item['case'] == 'R218-F12' else ['0313e086', '3ab22d36']
    if group == 'settings-webdav':
        return ['abdf47ef', 'd01fdf32', 'f9963a7f'] + (['0b760d9cb'] if item['case'] in ('R016-F15', 'R017-F18', 'R017-F19') else [])
    if (group == 'webdav-named-preflight-and-directory-failure' and item['case'] == 'R016-F15'
            and item['route'] == 'POST /api/backup/now' and item['variant'] == 'real-MKCOL-403-before-PUT'):
        return ['0b760d9cb']
    if (group == 'settings-backup-validation' and item['case'] != 'R015-S04'
            and paths == {'db.fields.system//backup.value.webdav_password'}):
        return ['e4b3fb39f']
    if (group == 'aggregation-provider-refresh'
            and paths == {'files.shared/subscribes/sub-a.json.content.updated_at'} and (
            (item['case'] == 'R007-S02' and item['variant'] == '') or
            (item['case'] in ('R008-S05', 'R122-S05') and item['variant'] == 'failed-source-valid-cache'))):
        return ['e4b3fb39f']
    return []


def annotate(records):
    for item in records:
        # Re-evaluate on each actual verdict. A later unrelated difference must
        # reopen this case instead of inheriting a previous approval.
        if item['status'] == 'accepted':
            item['status'] = 'open'
        item['accepted_basis'] = accepted_basis(item)
        if item['accepted_basis']:
            item['status'] = 'accepted'
        item['fix_commits'] = repair_commits(item)
        item.pop('closure_note', None)
        if item['status'] == 'closed' and '4786d7f86' in item['fix_commits']:
            item['closure_note'] = 'CFG-08：C33公共content路由接线；Go9c88c4390实跑两端200及完整内容/存储相比通过，旧502为依赖未接通，不是安全拒绝'
        if item['status'] == 'closed' and '2896b8d9e' in item['fix_commits']:
            item['closure_note'] = '2896b8d9e为CFG-05/10窄协议比较器修正，不是Go业务修复；已实际重跑，目标/认证/顺序/次数及完整响应/SQL/files继续严格比'
        if item['status'] == 'closed' and 'b93269d6d' in item['fix_commits']:
            item['closure_note'] = 'b93269d6d为测试夹具的限定时间窗/生成log.level文本读取纠正，不是Go修复；已实际重跑，其他字段严格比'
        if item['status'] == 'closed' and 'fd6355ce0' in item['fix_commits']:
            item['closure_note'] = 'fd6355ce0为夹具时间窗纠正，不是Go业务修复；已实际重跑，revision/值/文件仍严格比'
        if item['status'] == 'closed' and '0b760d9cb' in item['fix_commits']:
            item['closure_note'] = '0b760d9cb为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较'
        if item['status'] == 'closed' and 'e4b3fb39f' in item['fix_commits']:
            item['closure_note'] = 'e4b3fb39f为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复'
    return records
