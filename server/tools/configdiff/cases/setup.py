"""First-use setup sessions through public HTTP, SQL rows and session files."""
import copy
import json
import re

from compare import database, files as stored_files, timestamp, Difference, differences
from harness import Pair, json_body
from seed import PULL, CURRENT, RETIRED
from cases.config_backup import message
from cases.helpers import profile_policy, profile_capture
from cases.subscriptions import require

SID = '0123456789abcdef0123456789abcdef'
PID = 'setup_' + SID[:20]
NAME = '合成向导🧪'
CREATE = {'id': SID, 'name': NAME, 'goal': 'router'}
READ = 'GET /api/setup/<session_id>'
PATCH = 'PATCH /api/setup/<session_id>'
ACTION = 'POST /api/setup/<session_id>/<operation>'
PATH = '/api/setup/' + SID


def configure(pair, sid=SID):
    pair.policy = profile_policy(pair)
    pid = 'setup_' + sid[:20]
    for field in ('created_at', 'updated_at'):
        pair.policy.times.update({'body.' + field, 'db.fields.profile/' + pid + '/' + field + '.value',
                                  'files.setup-sessions/' + sid + '.json.content.' + field})
    for field in ('abandoned_at',):
        pair.policy.times.update({'body.' + field, 'files.setup-sessions/' + sid + '.json.content.' + field})


def row(pair, pid=PID, index=0):
    return {key.split('/')[-1]: value['value'] for key, value in database(pair.dirs[index])['fields'].items()
            if key.startswith('profile/' + pid + '/')}


def session(pair, sid=SID, index=0):
    return json.loads((pair.dirs[index] / 'setup-sessions' / (sid + '.json')).read_text())


def initial_session_errors(value, *, sid=SID, name=NAME, goal='router'):
    from compare import differences
    expected = {'id': sid, 'profile_id': 'setup_' + sid[:20], 'name': name, 'goal': goal,
                'step': 1, 'max_step': 1, 'subscription_ids': [], 'node_ids': [], 'prepared': False,
                'profile_revision': 0, 'completed': False}
    return differences(expected, {k: value.get(k) for k in expected}, 'setup.initial')


def bind_public_token(pair):
    def capture(replies, policy):
        values = [database(d)['fields']['system//system_config']['value']['config_token'] for d in pair.dirs]
        if not re.fullmatch(r'[A-Za-z0-9_-]{43}', values[1]) or values[1] in (PULL, CURRENT, RETIRED):
            return [Difference('db.config_token', 'not-new-public-capability', '<new token_urlsafe(32)>', values[1])]
        policy.bind(*values, r'[A-Za-z0-9_-]{43}', 'setup-public-capability')
        return profile_capture(pair)(replies, policy)
    return capture


def create(pair, case='S01', data=None, *, variant='', new_token=False, headers=None, raw=None):
    value = CREATE if data is None else data
    sid = value.get('id')
    configure(pair, sid)
    def reference(reply):
        payload = reply.json()
        require(not initial_session_errors(payload, sid=sid, name=value['name'].strip(), goal=value['goal']), 'new session has exact initial state')
        require(session(pair, sid) == payload and row(pair, 'setup_' + sid[:20])['name'] == value['name'].strip(),
                'response, private atomic session file and new profile SQL rows agree')
        require(all(timestamp(payload[k], pair.policy) for k in ('created_at', 'updated_at')), 'session dates belong to actual request lifetime')
        if new_token:
            token = database(pair.dirs[0])['fields']['system//system_config']['value']['config_token']
            require(bool(re.fullmatch(r'[A-Za-z0-9_-]{43}', token)) and token not in (PULL, CURRENT, RETIRED), 'first setup actually generates a fresh public pull token')
    return pair.request('R208-' + case, 'POST /api/setup', body=raw or json_body(value), headers=headers, expect=200, writes=True,
                        check=reference, capture=bind_public_token(pair) if new_token else profile_capture(pair), variant=variant)


class Stable:
    """Independently forbid changes on either server, including volatile fields."""
    def __init__(self, pair):
        self.pair = pair
        self.before_values, self.after_values = [None, None], [None, None]

    def snapshot(self, index):
        return {'database': database(self.pair.dirs[index]), 'files': stored_files(self.pair.dirs[index])}

    def before(self, index):
        self.before_values[index] = self.snapshot(index)

    def after(self, index, reply):
        self.after_values[index] = self.snapshot(index)
        if index == 0:
            require(self.before_values[0] == self.after_values[0], 'read/rejected request keeps every SQL field and persisted file exact')

    def compare(self, policy):
        # Intra-server values need no cross-server timestamp/identity allowance.
        return differences(self.before_values[1], self.after_values[1], 'setup.readonly-go')


def call(pair, case, route, path=None, *, body=None, expect=200, check=None, variant='', headers=None, readonly=False, interaction=None, capture=None):
    return pair.request(case, route, path, body=json_body(body) if isinstance(body, (dict, list)) else body, expect=expect,
                        writes=True, check=check, capture=capture or profile_capture(pair), variant=variant, headers=headers,
                        interaction=Stable(pair) if readonly else interaction)


def creates(binary, recorder):
    for goal, case in [('router', 'S01'), ('gateway', 'S02')]:
        with Pair(binary, recorder, options={'config_token': PULL}) as pair:
            data = {**CREATE, 'goal': goal}
            create(pair, case, data, variant='initial-' + goal)
            before = database(pair.dirs[0]), stored_files(pair.dirs[0])
            create(pair, 'S03', data, variant='same-session-retry-' + goal)
            require((database(pair.dirs[0]), stored_files(pair.dirs[0])) == before, 'retry does not rewrite session or create a second profile/commit')
            for changed, label in [({'goal': 'gateway' if goal == 'router' else 'router'}, 'goal'), ({'name': NAME + '更名'}, 'name')]:
                call(pair, 'R208-F14', 'POST /api/setup', body={**data, **changed}, expect=409,
                     check=message('该向导已创建，请继续原向导或新建一次配置'), variant=goal + '/' + label)
                call(pair, 'H1014', 'POST /api/setup', body={**data, **changed}, expect=409, readonly=True,
                     check=message('该向导已创建，请继续原向导或新建一次配置'), variant=goal + '/' + label)
    with Pair(binary, recorder) as pair:
        create(pair, 'B02', new_token=True)
    with Pair(binary, recorder, options={'config_token': PULL}) as pair:
        create(pair, 'B01', {**CREATE, 'name': '中' * 63 + '🧪'}, variant='64-Unicode-codepoints')
    with Pair(binary, recorder, options={'config_token': PULL}) as pair:
        configure(pair)
        before = database(pair.dirs[0]), stored_files(pair.dirs[0])
        call(pair, 'R208-B90', 'POST /api/setup', body=' ' * 32769 + json_body(CREATE),
             headers={'Transfer-Encoding': 'chunked'}, expect=413, check=message('向导请求过大'), variant='chunked-wire-waitress-decoded-length-over-32KiB')
        require((database(pair.dirs[0]), stored_files(pair.dirs[0])) == before, 'production WSGI length reconstruction rejects oversized chunked setup before any write')


def create_guards(binary, recorder):
    with Pair(binary, recorder, options={'config_token': PULL}) as pair:
        configure(pair)
        vectors = [('F08', {**CREATE, 'padding': 'x' * 32768}, 413, '向导请求过大'),
                   ('F09', [], 400, '请提交有效 JSON 对象'), ('F09', 'not-json', 400, '请提交有效 JSON 对象'),
                   ('F10', {**CREATE, 'goal': 'invalid'}, 400, '请选择使用方式'),
                   ('F11', {'id': SID, 'goal': 'router'}, 400, '配置名称需为 1–64 个字符'),
                   ('F11', {**CREATE, 'name': ''}, 400, '配置名称需为 1–64 个字符'),
                   ('F11', {**CREATE, 'name': '  '}, 400, '配置名称需为 1–64 个字符'),
                   ('F12', {**CREATE, 'name': '中' * 65}, 400, '配置名称需为 1–64 个字符'),
                   ('F13', {**CREATE, 'id': 'invalid'}, 404, '向导记录不存在'),
                   ('F13', {'name': NAME, 'goal': 'router'}, 404, '向导记录不存在')]
        for index, (case, value, status, text) in enumerate(vectors):
            before = database(pair.dirs[0]), stored_files(pair.dirs[0])
            call(pair, 'R208-' + case, 'POST /api/setup', body=value, expect=status, check=message(text), variant=str(index))
            require((database(pair.dirs[0]), stored_files(pair.dirs[0])) == before, 'invalid setup creation leaves every SQL row and file intact')
        for helper, value, status, text in [('H1012', {**CREATE, 'goal': None}, 400, '请选择使用方式'),
                ('H1013', {**CREATE, 'name': 1}, 400, '配置名称需为 1–64 个字符')]:
            call(pair, helper, 'POST /api/setup', body=value, expect=status, check=message(text))
    with Pair(binary, recorder, options={'config_token': PULL}) as pair:
        configure(pair)
        call(pair, 'R116-S01', 'POST /api/profiles', body={'id': PID, 'name': '占用标识🧪'}, expect=201, variant='setup-collision-precondition')
        for case in ('R208-F15', 'H1015'):
            call(pair, case, 'POST /api/setup', body=CREATE, expect=409, check=message('配置名称标识冲突，请重新开始'))


GROUPS = {'setup-create': creates, 'setup-create-guards': create_guards}


def lists(binary, recorder):
    for agents, case, suggested in [([], 'S01', True), (None, 'S04', False)]:
        opts = {'config_token': PULL}
        if agents is not None:
            opts['agents'] = agents
        with Pair(binary, recorder, options=opts) as pair:
            call(pair, 'R207-' + case, 'GET /api/setup', readonly=True,
                 check=lambda r: require(r.json()['suggested'] is suggested and r.json()['sessions'] == [], 'suggestion depends on persisted managed-device catalog, not online state'))
    with Pair(binary, recorder, options={'agents': [], 'config_token': PULL, 'system_config': {'setup_suggestion_dismissed': True}}) as pair:
        call(pair, 'R207-S03', 'GET /api/setup', readonly=True,
             check=lambda r: require(r.json()['suggested'] is False, 'explicit abandoned/dismissed setup does not prompt again'))
    node = {'id': 'setup-node', 'name': '节点中文🧪', 'type': 'ss', 'server': '192.0.2.1', 'port': 443,
            'cipher': 'aes-128-gcm', 'password': 'synthetic'}
    subs = [{'id': 'linked', 'name': '订阅中文🧪', 'url': 'https://source.example/sub', 'enabled': True, 'interval': 0},
            {'id': 'multiline', 'name': '多行链接', 'url': 'https://a.example/sub\n\nhttps://b.example/sub', 'enabled': True, 'interval': 0},
            {'id': 'disabled', 'name': '已禁用', 'url': 'https://source.example/disabled', 'enabled': False, 'interval': 0},
            {'id': 'inline', 'name': '内联不可选', 'url': 'proxies: []', 'enabled': True, 'interval': 0}]
    with Pair(binary, recorder, options={'shared': {'nodes': [node, {**node, 'id': 'disabled-node', 'enabled': False}], 'subscriptions': subs}}) as pair:
        def check(reply):
            require(reply.json()['subscriptions'] == [{'id': s['id'], 'name': s['name']} for s in subs[:2]]
                and reply.json()['nodes'] == [{'id': node['id'], 'name': node['name']}], 'only enabled nodes and all-link enabled subscriptions are selectable')
        for case in ('B01', 'B02'):
            call(pair, 'R207-' + case, 'GET /api/setup', check=check, readonly=True)
    with Pair(binary, recorder, options={'config_token': PULL, 'setup_sessions': [CREATE]}) as pair:
        configure(pair)
        for field in ('created_at', 'updated_at'):
            pair.policy.times.add('body.sessions[0].' + field)
        call(pair, 'R207-S02', 'GET /api/setup', readonly=True,
             check=lambda r: require(r.json()['sessions'] == [session(pair)] and r.json()['suggested'] is False, 'actual stored setup appears intact and suppresses a new prompt'))


def reads(binary, recorder):
    with Pair(binary, recorder, options={'config_token': PULL, 'setup_sessions': [CREATE]}) as pair:
        configure(pair)
        def check(reply):
            payload = reply.json()
            require({k: v for k, v in payload.items() if k != 'default_profile'} == session(pair)
                and payload['default_profile']['is_default'] is False
                and payload['default_profile']['current']['id'] == 'default', 'read returns exact unprepared state and current-default summary')
        for case in ('S01', 'B02'):
            call(pair, 'R209-' + case, READ, PATH, check=check, readonly=True)
    for case, path in [('F08', '/api/setup/' + 'f' * 32), ('F10', '/api/setup/invalid')]:
        with Pair(binary, recorder) as pair:
            call(pair, 'R209-' + case, READ, path, expect=404, readonly=True,
                 check=message('向导记录不存在，请重新开始' if case == 'F08' else '向导记录不存在'))
    with Pair(binary, recorder, options={'config_token': PULL, 'setup_sessions': [CREATE]}) as pair:
        configure(pair)
        call(pair, 'R117-S01', 'DELETE /api/profiles/<profile_id>', '/api/profiles/' + PID, expect=204, variant='setup-orphan-precondition')
        call(pair, 'R209-F09', READ, PATH, expect=404, readonly=True, check=message('该配置空间已删除，请重新开始向导'))
        call(pair, 'R210-F16', PATCH, PATH, body={}, expect=404, readonly=True, check=message('该配置空间已删除，请重新开始向导'), variant='profile-deleted')
        call(pair, 'R211-F11', ACTION, PATH + '/source', body={}, expect=404, readonly=True, check=message('该配置空间已删除，请重新开始向导'))
    for broken, label in [(b'{broken JSON', 'invalid-json'), (b'[]', 'non-object')]:
        with Pair(binary, recorder, options={'config_token': PULL, 'setup_sessions': [CREATE]}) as pair:
            configure(pair)
            for directory in pair.dirs:
                (directory / 'setup-sessions' / (SID + '.json')).write_bytes(broken)
            call(pair, 'R209-B01', READ, PATH, expect=500, readonly=True, variant=label)
    for legacy in (False, True):
        fixture = {**CREATE, 'session': {'prepared': True, 'step': 3, 'max_step': 3}}
        if legacy:
            fixture['profile'] = {'mihomo': {'linux_gateway': {'enabled': True, 'rule_set_ids': []}}}
        with Pair(binary, recorder, options={'config_token': PULL, 'setup_sessions': [fixture]}) as pair:
            configure(pair)
            call(pair, 'R209-S03' if legacy else 'R209-S02', READ, PATH, readonly=True,
                check=lambda r: require(r.json()['prepared'] is True and r.json()['legacy_takeover'] is legacy,
                                        'old prepared session gains read-only derived legacy takeover while stored file stays exact'),
                variant='compatibility-prepared-record')


def progress(binary, recorder):
    for goal in ('router', 'gateway'):
        with Pair(binary, recorder, options={'config_token': PULL, 'setup_sessions': [{**CREATE, 'goal': goal}]}) as pair:
            configure(pair)
            for case, body, expected in [('S01', {'step': 1}, {'step': 1}), ('B01', {'step': 0}, {'step': 0}),
                    ('B02', {}, {'step': 0}), ('S02', {'router_mode': 'manual'}, {'router_mode': 'manual'}),
                    ('S03', {'router_mode': 'managed'}, {'router_mode': 'managed'})]:
                if goal == 'gateway' and 'router_mode' in body:
                    call(pair, 'R210-F14', PATCH, PATH, body=body, expect=400, check=message('请选择有效的路由器配置方式'),
                         readonly=True, variant=body['router_mode'])
                    continue
                def check(reply):
                    value = reply.json()
                    require(all(value[k] == v for k, v in expected.items())
                        and {k: v for k, v in value.items() if k != 'default_profile'} == session(pair)
                        and database(pair.dirs[0]) == before, 'progress response matches atomic session file without committing configuration')
                before = database(pair.dirs[0])
                call(pair, 'R210-' + case, PATCH, PATH, body=body, check=check, variant=goal)
            for case, body, status, text in [('F08', {'padding': 'x' * 32768}, 413, '向导请求过大'),
                ('F09', [], 400, '请提交有效 JSON 对象'), ('F10', {'unexpected': True}, 400, '向导进度参数无效'),
                ('F11', {'step': 2}, 400, '请先完成前面的步骤'), ('F12', {'step': True}, 400, '请先完成前面的步骤'),
                ('F12', {'step': '1'}, 400, '请先完成前面的步骤'), ('F13', {'router_mode': 'invalid'}, 400, '请选择有效的路由器配置方式'),
                ('F15', {'router_id': 'missing'}, 409, '请选择当前配置空间中的路由器')]:
                call(pair, 'R210-' + case, PATCH, PATH, body=body, expect=status, check=message(text), readonly=True, variant=goal + '/' + repr(body)[:50])
            call(pair, 'R210-B90', PATCH, PATH, body=' ' * 32769 + '{}', headers={'Transfer-Encoding': 'chunked'},
                 expect=413, check=message('向导请求过大'), readonly=True, variant=goal + '/chunked-waitress-decoded-length')
    router = {'id': 'setup-router', 'name': '当前空间路由🧪', 'type': 'ikuai', 'endpoint': 'http://192.0.2.1',
              'username': 'synthetic', 'password': 'synthetic', 'enabled': False, 'auto_sync': False}
    fixture = {**CREATE, 'profile': {'routers': {'items': [router]}}, 'session': {'abandoned': True}}
    for case in ('S04', 'S05'):
        with Pair(binary, recorder, options={'config_token': PULL, 'setup_sessions': [fixture]}) as pair:
            configure(pair)
            require(session(pair)['abandoned'] is True, 'reopen case starts with a genuinely abandoned record')
            call(pair, 'R210-' + case, PATCH, PATH, body={'router_id': router['id']} if case == 'S04' else {},
                 check=lambda r: require('abandoned' not in r.json() and 'abandoned' not in session(pair)
                    and (case == 'S05' or r.json()['router_id'] == router['id']), 'selected current-profile router and continuing progress reopen abandoned setup'))
    for path in (PATH, '/api/setup/invalid'):
        with Pair(binary, recorder) as pair:
            call(pair, 'R210-F16', PATCH, path, body={}, expect=404, readonly=True, variant=path)


GROUPS.update({'setup-list': lists, 'setup-read': reads, 'setup-progress': progress})


def action_guards(binary, recorder):
    with Pair(binary, recorder, options={'config_token': PULL, 'setup_sessions': [CREATE]}) as pair:
        configure(pair)
        for case, operation, body, status, text, label in [
            ('F08', 'source', {'padding': 'x' * 32768}, 413, '向导请求过大', 'length'),
            ('F09', 'source', [], 400, '请提交有效 JSON 对象', 'array'),
            ('F09', 'source', 'not-json', 400, '请提交有效 JSON 对象', 'syntax'),
            ('F12', 'unknown', {}, 404, '未知向导操作', 'unknown-operation'),
            ('F13', 'abandon', {'dry_run': 1}, 400, '请提交有效的放弃请求', 'not-bool'),
            ('F13', 'abandon', {'extra': True}, 400, '请提交有效的放弃请求', 'extra'),
            ('F14', 'check', {}, 409, '请先生成配置方案', 'unprepared-check'),
            ('F15', 'finish', {}, 409, '请先生成配置方案', 'unprepared-finish')]:
            call(pair, 'R211-' + case, ACTION, PATH + '/' + operation, body=body, expect=status,
                 check=message(text), readonly=True, variant=label)
        call(pair, 'R211-B90', ACTION, PATH + '/source', body=' ' * 32769 + '{}', headers={'Transfer-Encoding': 'chunked'},
             expect=413, check=message('向导请求过大'), readonly=True, variant='chunked-waitress-decoded-length')
        for helper, body, status, text in [('H1045', {'extra': True}, 400, '向导进度参数无效'),
                ('H1046', {'step': -1}, 400, '请先完成前面的步骤'),
                ('H1047', {'router_mode': None}, 400, '请选择有效的路由器配置方式'),
                ('H1048', {'router_id': 'other-profile-router'}, 409, '请选择当前配置空间中的路由器')]:
            call(pair, helper, PATCH, PATH, body=body, expect=status, check=message(text), readonly=True)
    for case, ident, text in [('H1010', 'invalid', '向导记录不存在'), ('H1011', 'f' * 32, '向导记录不存在，请重新开始')]:
        with Pair(binary, recorder) as pair:
            for label, body in [('empty', {}), ('large-invalid-JSON', 'x' * 32769)]:
                call(pair, 'R211-F10', ACTION, '/api/setup/' + ident + '/source', body=body, expect=404,
                     check=message(text), readonly=True, variant=case + '/' + label)
            call(pair, case, ACTION, '/api/setup/' + ident + '/source', body={}, expect=404, check=message(text), readonly=True)
    with Pair(binary, recorder, options={'config_token': PULL, 'setup_sessions': [{**CREATE, 'session': {'completed': True}}]}) as pair:
        configure(pair)
        call(pair, 'R211-F16', ACTION, PATH + '/abandon', body={}, expect=409, readonly=True, check=message('向导已完成，无需放弃'))


GROUPS['setup-action-guards'] = action_guards


def concurrent_create(binary, recorder):
    from concurrent.futures import ThreadPoolExecutor
    import threading
    import time
    from compare import database_diffs, file_diffs, response_diffs
    from harness import send
    with Pair(binary, recorder, options={'config_token': PULL}) as pair:
        configure(pair)
        route = 'POST /api/setup'
        available = pair.route_available(route)
        results = []
        before = database(pair.dirs[0])
        for index, base in enumerate(pair.bases):
            if index and not available:
                break
            barrier = threading.Barrier(2)
            def request(_):
                barrier.wait(timeout=5)
                return send(base, 'POST', '/api/setup', json_body(CREATE),
                            {'Authorization': 'Bearer ' + pair.tokens[index], 'Content-Type': 'application/json'})
            with ThreadPoolExecutor(max_workers=2) as pool:
                results.append(list(pool.map(request, range(2))))
        require([r.status for r in results[0]] == [200, 200] and results[0][0].json() == results[0][1].json(),
                'simultaneous identical creates both return one identical reserved session')
        require(not initial_session_errors(results[0][0].json()) and session(pair) == results[0][0].json(),
                'concurrent creation persists exactly the returned initial session')
        after = database(pair.dirs[0])
        require(after['meta']['generation'] == before['meta']['generation'] + 1
            and sum(s[0] == 'profile' and s[1] == PID for s in after['sections']) == 1,
            'shared session lock causes exactly one profile commit and one profile section')
        pair.policy.end = time.time()
        diffs = []
        if available:
            # Both expected responses are identical; preserving request slots
            # exposes any rejected duplicate without sorting away its payload.
            for left, right in zip(*results):
                diffs += response_diffs(left, right, pair.policy, method='POST')
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        recorder.record('R208-B03', 'two-concurrent-identical-creates', route, 'B',
                        'go-unimplemented' if not available else 'different' if diffs else 'compared', diffs,
                        python_statuses=[r.status for r in results[0]], go_statuses=[r.status for r in results[1]] if available else None)


GROUPS['setup-concurrent-create'] = concurrent_create


NODE = {'id': 'setup-node', 'name': '节点中文🧪', 'type': 'ss', 'server': '192.0.2.1', 'port': 443,
        'cipher': 'aes-128-gcm', 'password': 'synthetic'}
SUB = {'id': 'setup-existing-sub', 'name': '已有订阅🧪', 'url': 'https://source.example/sub', 'interval': 0, 'enabled': True}


def source_options(session_changes=None):
    return {'config_token': PULL, 'setup_sessions': [{**CREATE, 'session': session_changes or {}}],
            'shared': {'nodes': [NODE, {**NODE, 'id': 'disabled-node', 'enabled': False}],
                       'subscriptions': [SUB, {**SUB, 'id': 'disabled-sub', 'enabled': False},
                                         {**SUB, 'id': 'placeholder', 'url': 'proxies: []'}]}}


def sources(binary, recorder):
    from sources import Source
    from subscriptions import Exchange
    for case, field, identity in [('S02', 'subscription_ids', SUB['id']), ('S03', 'node_ids', NODE['id'])]:
        with Pair(binary, recorder, options=source_options()) as pair:
            configure(pair)
            before = database(pair.dirs[0])
            def check(reply):
                value = reply.json()
                require(value[field] == [identity] and value['step'] == value['max_step'] == 2
                    and database(pair.dirs[0]) == before and session(pair)[field] == [identity],
                    'existing resources deduplicate stably and unlock step 2 without modifying configuration')
            call(pair, 'R211-' + case, ACTION, PATH + '/source', body={field: [identity, identity]}, check=check)
    with Source() as source, Pair(binary, recorder, options=source_options()) as pair:
        configure(pair)
        for variant, raw in [('trimmed-URL-wins-over-existing-selections', '  ' + source.url + '/first?synthetic=1  '),
                             ('repeat-one-owned-subscription', source.url + '/first?synthetic=1'),
                             ('replace-owned-URL-stable-id', source.url + '/second?synthetic=2')]:
            def check(reply):
                value = reply.json()
                identity = 'setup_sub_' + SID
                rows = database(pair.dirs[0])['fields']['shared//subscriptions']['value']
                owned = [s for s in rows if s['id'] == identity]
                require(len(owned) == 1 and owned[0]['url'] == raw.strip() and owned[0]['name'] == NAME + '订阅'
                    and owned[0]['enabled'] is True and owned[0]['interval'] == 86400
                    and value['subscription_ids'] == [identity] and value['node_ids'] == [] and value['step'] == value['max_step'] == 2,
                    'HTTP URL owns one stable shared source, overrides previous selected resources and keeps exact refresh interval')
            body = {'url': raw, 'subscription_ids': [SUB['id']], 'node_ids': [NODE['id']]}
            call(pair, 'R211-S01', ACTION, PATH + '/source', body=body, check=check,
                 interaction=Exchange(pair, source, expected=0), variant=variant)
        call(pair, 'R211-B03', ACTION, PATH + '/source', body={'node_ids': [NODE['id']]},
             check=lambda r: require(r.json()['name'] == NAME and session(pair)['name'] == NAME, 'Chinese and emoji session name remains exact during source changes'))


def source_guards(binary, recorder):
    vectors = []
    for field in ('subscription_ids', 'node_ids'):
        for value in (None, True, [1], ['missing'] * 101):
            vectors.append(('H1024', {field: value}, 400, '请选择有效的订阅或节点'))
    for value in (None, 1, 'x' * 8193):
        vectors.append(('H1025', {'url': value}, 400, '订阅链接格式不正确'))
    for value in ('file:///synthetic', 'https:///missing', 'https://source.example:65536/sub', 'https://[bad'):
        vectors.append(('H1026', {'url': value}, 400, '请输入 HTTP(S) 订阅链接'))
    for field, values in [('subscription_ids', ['missing', 'disabled-sub']), ('node_ids', ['missing', 'disabled-node'])]:
        for value in values:
            vectors.append(('H1027', {field: [value]}, 409, '所选来源已删除或停用，请刷新后重选'))
    vectors.extend([('H1028', {'subscription_ids': ['placeholder']}, 400, '所选订阅未填写订阅链接，请改用订阅链接添加'),
                    ('H1029', {}, 400, '请添加订阅链接或选择已有节点'),
                    ('H1029', {'url': '   '}, 400, '请添加订阅链接或选择已有节点')])
    with Pair(binary, recorder, options=source_options()) as pair:
        configure(pair)
        for index, (helper, body, status, text) in enumerate(vectors):
            for case in ('R211-F17', helper):
                call(pair, case, ACTION, PATH + '/source', body=body, expect=status, check=message(text), readonly=True, variant=str(index))
    with Pair(binary, recorder, options=source_options({'prepared': True})) as pair:
        configure(pair)
        for case in ('R211-F17', 'H1023'):
            call(pair, case, ACTION, PATH + '/source', body={'node_ids': [NODE['id']]}, expect=409, readonly=True,
                 check=message('方案已生成；修改来源请前往订阅来源或创建新的向导'), variant='prepared-session')


GROUPS.update({'setup-source': sources, 'setup-source-guards': source_guards})
