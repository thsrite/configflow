"""First-use setup sessions through public HTTP, SQL rows and session files."""
import copy
import json
import re

from compare import database, files as stored_files, timestamp, Difference
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


def call(pair, case, route, path=None, *, body=None, expect=200, check=None, variant='', headers=None):
    return pair.request(case, route, path, body=json_body(body) if isinstance(body, (dict, list)) else body, expect=expect,
                        writes=True, check=check, capture=profile_capture(pair), variant=variant, headers=headers)


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
