"""REBIND helper guards through the public profile transaction endpoint."""
from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.helpers import profile_policy
from cases.pull_routes import SourceStable

ROUTE = 'POST /api/profiles/<profile_id>/agents/rebind'
PATH = '/api/profiles/default/agents/rebind'
GOOD = {'target_profile_id': 'other', 'agent_ids': ['agent-synthetic']}


def guards(binary, recorder):
    with Source(b'rebinding must not push configuration') as source:
        with Pair(binary, recorder, options={'agent_updates': {'port': source.server.server_port}}) as pair:
            pair.policy = profile_policy(pair)
            rows = [('H742', {**GOOD, 'target_profile_id': 'default'}, 400, '请选择其他配置空间', 'same-profile')]
            for label, value in [('null', None), ('empty', []), ('string', 'agent-synthetic'),
                                 ('object', {}), ('number-item', [1]), ('bool-item', [True]),
                                 ('null-item', [None]), ('empty-item', ['']),
                                 ('duplicate', ['agent-synthetic', 'agent-synthetic'])]:
                rows.append(('H743', {**GOOD, 'agent_ids': value}, 400, '请提供已确认的 Agent ID 列表', label))
            rows.append(('H743', {'target_profile_id': 'other'}, 400, '请提供已确认的 Agent ID 列表', 'missing'))
            for label, value in [('changed', ['unexpected']), ('extra', ['agent-synthetic', 'unexpected'])]:
                rows.append(('H744', {**GOOD, 'agent_ids': value}, 409, 'Agent 绑定已变化，请刷新列表后重新确认', label))
            for case, body, status, text, label in rows:
                pair.request(case, ROUTE, PATH, body=json_body(body), expect=status, writes=True,
                    check=message(text), interaction=SourceStable(pair, source), variant='rebind-guard/' + label)
        for field, states in [('latest_upgrade', ['pending', 'running', 'unknown', None]),
                              ('oxidns_ha_publication', ['preparing', 'publishing', 'rolled_back', None]),
                              ('deployments', ['pending', 'preparing', 'rollback_failed', None])]:
            for state in states:
                job = {'status': state, 'synthetic_fixture': True}
                updates = {'port': source.server.server_port, field: {'synthetic': job} if field == 'deployments' else job}
                with Pair(binary, recorder, options={'agent_updates': updates}) as pair:
                    pair.policy = profile_policy(pair)
                    for case in (['H745', 'R121-F17'] if field == 'oxidns_ha_publication' else ['H745']):
                        pair.request(case, ROUTE, PATH, body=json_body(GOOD), expect=409, writes=True,
                            check=message('以下 Agent 正在发布、更新或等待恢复，请完成后再改绑：合成 Agent 🧪'),
                            interaction=SourceStable(pair, source), variant='rebind-busy/' + field + '/' + str(state))


GROUPS = {'profile-rebind-helper-guards': guards}
