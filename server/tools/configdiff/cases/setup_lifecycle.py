"""Setup's private router draft and abandonment boundaries, without device writes."""
import re

from compare import database, Difference
from harness import Pair
from cases.config_backup import message
from cases.helpers import profile_capture
from cases.setup import CREATE, PID, PATH, ACTION, SID, configure, call, row, session, Stable
from cases.subscriptions import require, put_file
from seed import PULL

ROUTER = {'name': '主路由🧪', 'type': 'ikuai', 'endpoint': 'http://192.0.2.1', 'username': 'synthetic-user', 'password': 'synthetic-router-password'}


def options(*, goal='router', prepared=True, max_step=3, session_changes=None, profile=None):
    fixture = {**CREATE, 'goal': goal, 'session': {'prepared': prepared, 'step': min(max_step, 3), 'max_step': max_step,
                                                **(session_changes or {})}}
    if profile:
        fixture['profile'] = profile
    return {'config_token': PULL, 'setup_sessions': [fixture]}


class DraftOnly(Stable):
    def snapshot(self, index):
        stored = session(self.pair, index=index)
        allowed = {'router_draft', 'router_mode', 'updated_at', 'abandoned'}
        return {'database': database(self.pair.dirs[index]),
                'immutable_session': {k: v for k, v in stored.items() if k not in allowed}}


def capture_draft(pair):
    def capture(replies, policy):
        out = profile_capture(pair)(replies, policy)
        left = replies[0].json().get('router_draft', {}).get('id')
        if replies[1].status != 200:
            return out
        try:
            right = replies[1].json().get('router_draft', {}).get('id')
        except (ValueError, AttributeError):
            right = None
        if not isinstance(right, str) or not re.fullmatch(r'router_[0-9a-f]{12}', right):
            return out + [Difference('body.router_draft.id', 'not-source-generated-router-id-shape', left, right)]
        if left in policy.identities and policy.identities[left] != right:
            return out + [Difference('body.router_draft.id', 'unstable-Go-draft-id', policy.identities[left], right)]
        if right in policy.reverse and policy.reverse[right] != left:
            return out + [Difference('body.router_draft.id', 'reused-Go-draft-id', '<new private router identity>', right)]
        policy.bind(left, right, r'router_[0-9a-f]{12}', 'setup-private-router')
        return out
    return capture


def drafts(binary, recorder):
    for kind in ('ikuai', 'routeros'):
        with Pair(binary, recorder, options=options()) as pair:
            configure(pair)
            body = {**ROUTER, 'type': kind}
            def check(reply):
                value, private = reply.json()['router_draft'], session(pair)['router_draft']
                require(re.fullmatch(r'router_[0-9a-f]{12}', value['id']) and private['id'] == value['id']
                    and value['name'] == body['name'] and value['type'] == kind and value['has_password'] is True
                    and 'password' not in value and private['password'] == ROUTER['password'],
                    'draft has a new stable id, public password-presence flag and private raw credential')
                require(row(pair)['routers']['items'] == [] and reply.json()['router_mode'] == 'managed', 'connection draft stays outside managed profile routers until installation checks')
            call(pair, 'R211-S08', ACTION, PATH + '/router-draft', body={'router': body}, check=check, variant=kind + '/create',
                 interaction=DraftOnly(pair), capture=capture_draft(pair))
            previous = session(pair)['router_draft']['id']
            body['name'] = '更新名称中文🧪'
            body['password'] = ''
            call(pair, 'R211-S08', ACTION, PATH + '/router-draft', body={'router': body}, variant=kind + '/edit-blank-password-keeps-secret',
                 check=lambda r: (check(r), require(r.json()['router_draft']['id'] == previous, 'draft edit preserves allocated id')),
                 interaction=DraftOnly(pair), capture=capture_draft(pair))
            call(pair, 'R211-B03', ACTION, PATH + '/router-draft', body={'router': {'name': '特殊名 🧪 中文 "\\'}}, variant=kind + '/Unicode-name',
                 check=lambda r: require(r.json()['router_draft']['name'] == '特殊名 🧪 中文 "\\'
                    and session(pair)['router_draft']['password'] == ROUTER['password'], 'partial draft edit retains secret and exact Unicode/special characters'),
                 interaction=DraftOnly(pair), capture=capture_draft(pair))
            call(pair, 'R211-S09', ACTION, PATH + '/router-draft', body={'router': None}, variant=kind + '/withdraw',
                 check=lambda r: require('router_draft' not in r.json() and 'router_draft' not in session(pair)
                    and r.json()['router_mode'] == 'managed', 'withdraw clears only private draft; existing mode is not silently reset'), interaction=DraftOnly(pair))
            body = {**ROUTER, 'type': kind}
            call(pair, 'R211-S08', ACTION, PATH + '/router-draft', body={'router': body}, variant=kind + '/recreate-after-withdraw',
                 check=lambda r: (check(r), require(r.json()['router_draft']['id'] != previous, 'withdrawn connection draft is replaced by a fresh private identity')),
                 interaction=DraftOnly(pair), capture=capture_draft(pair))


def draft_guards(binary, recorder):
    for goal, prepared, maximum, label in [('gateway', True, 3, 'wrong-goal'), ('router', False, 3, 'not-prepared'),
                                           ('router', True, 4, 'already-checked')]:
        with Pair(binary, recorder, options=options(goal=goal, prepared=prepared, max_step=maximum)) as pair:
            configure(pair)
            for case in ('R211-F21', 'H1049'):
                call(pair, case, ACTION, PATH + '/router-draft', body={'router': ROUTER}, expect=409, readonly=True, variant=label,
                     check=message('请在安装等待期间填写主路由信息；安装完成后使用路由器管理'))
    vectors = [('H1050', {}, '主路由连接参数无效'), ('H1050', {'router': ROUTER, 'extra': True}, '主路由连接参数无效'),
               ('H1051', {'router': []}, '主路由连接参数无效'), ('H1051', {'router': {**ROUTER, 'id': 'cannot-force-id'}}, '主路由连接参数无效')]
    for field, value in [('proxy_gateway_agent_id', 'unrelated-agent'), ('dns_agent_id', 'unrelated-agent'), ('auto_rule_sync', True)]:
        vectors.append(('H1052', {'router': {**ROUTER, field: value}}, '安装完成后自动关联本次设备，暂不启用规则自动同步'))
    for patch, text in [({'name': ''}, '请填写名称'), ({'type': 'unknown'}, '路由器类型只支持 iKuai 4.x 或 RouterOS'),
                        ({'endpoint': 'file:///synthetic'}, '管理地址格式应为 http(s)://主机[:端口]'),
                        ({'username': ''}, '请填写用户名'), ({'password': ''}, '请填写密码'),
                        ({'rule_sync_interval': 61}, '请选择有效的规则检查间隔')]:
        vectors.append(('H1053', {'router': {**ROUTER, **patch}}, text))
    vectors.append(('H1054', {'router': {**ROUTER, 'type': 'routeros', 'ros_policy_rule_sets': ['synthetic-set']}},
                    '新配置空间还没有设为智能分流的客户端设备，暂不能使用策略路由；完成向导后先在「客户端设备」中设置，再到上游路由中启用'))
    with Pair(binary, recorder, options=options()) as pair:
        configure(pair)
        for index, (helper, body, text) in enumerate(vectors):
            for case in ('R211-F21', helper):
                call(pair, case, ACTION, PATH + '/router-draft', body=body, expect=400, readonly=True, check=message(text), variant=str(index))


GROUPS = {'setup-router-drafts': drafts, 'setup-router-draft-guards': draft_guards}


def assert_abandoned(pair, reply, *, kept):
    value = reply.json()
    fields = database(pair.dirs[0])['fields']
    require(value['delete_profile'] is not kept and fields['system//system_config']['value']['setup_suggestion_dismissed'] is True,
            'applied abandon persists dismissal and reports whether profile was kept')
    exists = any(key.startswith('profile/' + PID + '/') for key in fields)
    require(exists is kept, 'profile removal decision matches actual SQL rows')
    path = pair.dirs[0] / 'setup-sessions' / (SID + '.json')
    require(path.exists() is kept and (not kept or session(pair)['abandoned'] is True), 'kept profile retains abandoned record; deleted profile loses session file')


def continuation_read(pair, variant, *, kept, reopened=False):
    for name in ('created_at', 'updated_at', 'abandoned_at'):
        pair.policy.times.add('body.sessions[0].' + name)
    def check(reply):
        value = reply.json()
        require(value['suggested'] is False and [item['id'] for item in value['sessions']] == ([SID] if kept else []),
                'completed abandon suppresses suggestions and lists exactly the retained resumable setup')
        if kept:
            require(('abandoned' not in value['sessions'][0]) if reopened else value['sessions'][0]['abandoned'] is True,
                    'public list reflects the persisted abandon/reopen state')
    call(pair, 'T23', 'GET /api/setup', check=check, readonly=True, variant=variant)


def abandon(binary, recorder):
    from sources import Source
    from subscriptions import Exchange
    for condition in ('unused', 'default', 'bound-agent', 'router', 'installation'):
        fixture = options(prepared=False, max_step=1)
        if condition == 'router':
            fixture['setup_sessions'][0]['profile'] = {'routers': {'items': [{**ROUTER, 'id': 'kept-router',
                'enabled': False, 'auto_rule_sync': False}]}}
        with Pair(binary, recorder, options=fixture) as pair:
            configure(pair)
            if condition == 'default':
                call(pair, 'R124-S01', 'POST /api/profiles/<profile_id>/default', '/api/profiles/' + PID + '/default',
                     body={}, variant='setup-abandon-default-precondition')
            elif condition == 'bound-agent':
                pair.policy.times.add('db.fields.system//agents.value[0].updated_at')
                call(pair, 'R121-S01', 'POST /api/profiles/<profile_id>/agents/rebind', '/api/profiles/default/agents/rebind',
                     body={'target_profile_id': PID, 'agent_ids': ['agent-synthetic']}, variant='setup-abandon-binding-precondition')
            elif condition == 'installation':
                put_file(pair, 'gateway-installations/' + 'b' * 32 + '.json', {'id': 'b' * 32, 'profile_id': PID})
                for directory in pair.dirs:
                    (directory / 'gateway-installations' / ('b' * 32 + '.json')).chmod(0o600)
            kept = condition != 'unused'
            def plan(reply):
                value = reply.json()
                require(value['profile']['id'] == PID and value['delete_profile'] is not kept
                    and bool(value['kept_reason']) is kept and value['subscription'] is None,
                    'dry-run plan has exact ownership/removal decision without modifying any persistence')
                if kept:
                    phrase = {'default': '默认配置空间', 'bound-agent': '已有设备在使用它',
                              'router': '已添加主路由', 'installation': '设备安装已经开始'}[condition]
                    require(phrase in value['kept_reason'] and bool(value['guidance']), 'kept-profile plan explains the specific dependency')
            call(pair, 'R211-S13', ACTION, PATH + '/abandon', body={'dry_run': True}, readonly=True, check=plan, variant=condition)
            call(pair, 'R211-S14', ACTION, PATH + '/abandon', body={},
                 check=lambda r: (plan(r), assert_abandoned(pair, r, kept=kept)), variant=condition)
            if kept:
                call(pair, 'R209-S01', 'GET /api/setup/<session_id>', PATH, readonly=True,
                     check=lambda r: require(r.json()['abandoned'] is True and r.json()['completed'] is False,
                                             'abandoned kept setup remains resumable'), variant=condition + '/abandoned-read')
            continuation_read(pair, condition + '/after-abandon', kept=kept)
    with Source() as source, Pair(binary, recorder, options=options(prepared=False, max_step=1)) as pair:
        configure(pair)
        identity = 'setup_sub_' + SID
        call(pair, 'R211-S01', ACTION, PATH + '/source', body={'url': source.url + '/owned-subscription'},
             interaction=Exchange(pair, source, expected=0), variant='abandon-owned-source-precondition')
        cache_name = 'shared/subscribes/' + identity + '.json'
        put_file(pair, cache_name, {'nodes': [], 'count': 0, 'metadata': {'url': source.url + '/owned-subscription'}})
        put_file(pair, 'shared/subscription_health.json', {identity: {'history': []}})
        call(pair, 'R211-S13', ACTION, PATH + '/abandon', body={'dry_run': True}, readonly=True,
             check=lambda r: require(r.json()['subscription']['id'] == identity and r.json()['delete_profile'] is True,
                                     'dry run includes unused wizard-created source'), variant='owned-source')
        def removed(reply):
            assert_abandoned(pair, reply, kept=False)
            subscriptions = database(pair.dirs[0])['fields']['shared//subscriptions']['value']
            require(not any(s['id'] == identity for s in subscriptions) and not (pair.dirs[0] / cache_name).exists(),
                    'abandon removes owned unused subscription SQL row and original cache file')
        call(pair, 'R211-S14', ACTION, PATH + '/abandon', body={}, check=removed, variant='owned-source')
        continuation_read(pair, 'owned-source/after-abandon', kept=False)
    with Pair(binary, recorder, options=options(max_step=5, session_changes={'completed': True, 'step': 5})) as pair:
        configure(pair)
        for label, body in [('dry-run', {'dry_run': True}), ('apply', {})]:
            call(pair, 'H987', ACTION, PATH + '/abandon', body=body, expect=409, readonly=True,
                 check=message('向导已完成，无需放弃'), variant=label)


def abandon_shared_source(binary, recorder):
    identity = 'setup_sub_' + SID
    owned = {'id': identity, 'name': '向导共有订阅🧪', 'url': 'https://source.example/owned', 'enabled': True, 'interval': 0}
    fixture = options(prepared=False, max_step=2, session_changes={'subscription_ids': [identity]})
    fixture['shared'] = {'subscriptions': [owned]}
    fixture['other_profile'] = {'proxy_groups': [{'id': 'other-uses-owned', 'name': '其他空间仍使用🧪', 'type': 'select',
        'manual_nodes': ['DIRECT'], 'subscriptions': [identity]}]}
    with Pair(binary, recorder, options=fixture) as pair:
        configure(pair)
        cache_name = 'shared/subscribes/' + identity + '.json'
        put_file(pair, cache_name, {'nodes': [], 'count': 0, 'metadata': {'url': owned['url']}})
        other_before = {k: v for k, v in database(pair.dirs[0])['fields'].items() if k.startswith('profile/other/')}
        def check(reply):
            assert_abandoned(pair, reply, kept=False)
            fields = database(pair.dirs[0])['fields']
            require(reply.json()['subscription_kept'] is True and any(s['id'] == identity for s in fields['shared//subscriptions']['value'])
                and (pair.dirs[0] / cache_name).exists()
                and {k: v for k, v in fields.items() if k.startswith('profile/other/')} == other_before,
                'a subscription still referenced by another profile and its cache survive wizard cleanup')
        call(pair, 'R211-S14', ACTION, PATH + '/abandon', body={}, check=check, variant='source-used-by-other-profile')
        continuation_read(pair, 'shared-source/after-abandon', kept=False)


def abandon_after_new_binding(binary, recorder):
    with Pair(binary, recorder, options=options(prepared=False, max_step=1)) as pair:
        configure(pair)
        call(pair, 'R211-S13', ACTION, PATH + '/abandon', body={'dry_run': True}, readonly=True,
             check=lambda r: require(r.json()['delete_profile'] is True, 'initial dry run offers to delete an unused setup'), variant='binding-race/initial-plan')
        pair.policy.times.add('db.fields.system//agents.value[0].updated_at')
        call(pair, 'R121-S01', 'POST /api/profiles/<profile_id>/agents/rebind', '/api/profiles/default/agents/rebind',
             body={'target_profile_id': PID, 'agent_ids': ['agent-synthetic']}, variant='binding-between-abandon-plan-and-apply')
        call(pair, 'R211-S14', ACTION, PATH + '/abandon', body={}, variant='binding-race/apply-rechecks-usage',
             check=lambda r: (assert_abandoned(pair, r, kept=True), require('已有设备在使用它' in r.json()['kept_reason'],
                 'binding added after dry run prevents deletion when user confirms')))
        call(pair, 'R210-S05', 'PATCH /api/setup/<session_id>', PATH, body={}, variant='resume-after-guarded-abandon',
             check=lambda r: require('abandoned' not in r.json() and 'abandoned' not in session(pair), 'PATCH reopens the retained setup'))
        continuation_read(pair, 'binding-race/resumed', kept=True, reopened=True)


GROUPS.update({'setup-abandon': abandon, 'setup-abandon-shared-source': abandon_shared_source,
               'setup-abandon-binding-race': abandon_after_new_binding})
