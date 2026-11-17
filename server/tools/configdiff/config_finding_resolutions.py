"""Repair attribution from config-findings-fixes.md and git commit subjects.

Repair provenance requires an actual successful recheck. Explicit approvals
require exact recorded differences. Raw case verdicts/evidence never change.
"""
import re

from compare import differences

DECISIONS = '[behaviour-changes.md · {item}](behaviour-changes.md#config-step-b--coordinator-decisions)'


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
        return ['abdf47ef', 'd01fdf32', 'f9963a7f'] + (['bc05a32b'] if item['case'] in ('R016-F15', 'R017-F18', 'R017-F19') else [])
    if (group == 'webdav-named-preflight-and-directory-failure' and item['case'] == 'R016-F15'
            and item['route'] == 'POST /api/backup/now' and item['variant'] == 'real-MKCOL-403-before-PUT'):
        return ['bc05a32b']
    if (group == 'settings-backup-validation' and item['case'] != 'R015-S04'
            and paths == {'db.fields.system//backup.value.webdav_password'}):
        return ['fd7f6f8f']
    if (group == 'aggregation-provider-refresh'
            and paths == {'files.shared/subscribes/sub-a.json.content.updated_at'} and (
            (item['case'] == 'R007-S02' and item['variant'] == '') or
            (item['case'] in ('R008-S05', 'R122-S05') and item['variant'] == 'failed-source-valid-cache'))):
        return ['fd7f6f8f']
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
        if item['status'] == 'closed' and 'bc05a32b' in item['fix_commits']:
            item['closure_note'] = 'bc05a32b为O6英文内部诊断比较器纠正，不是Go业务修复；中文/HTTP数字/副作用继续严格比较'
        if item['status'] == 'closed' and 'fd7f6f8f' in item['fix_commits']:
            item['closure_note'] = 'fd7f6f8f为夹具纠正（C35后公开恢复共同前提/固定缓存时间保持精确），不是Go业务修复'
    return records
