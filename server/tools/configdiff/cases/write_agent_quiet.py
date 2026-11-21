"""T25 actual online Agents record every request during saves and preview."""
import copy

from agent_fake import Agent, NoRequests
from compare import database, structure
from harness import json_body
from cases.discovery_reads import heartbeat
from cases.helpers import profile_pair
from cases.rules import RULE, options
from cases.rule_library import DOMAIN
from cases.subscriptions import require


def preview(value, index):
    value = copy.deepcopy(value)
    if isinstance(value, dict) and isinstance(value.get('content'), str):
        value['content'] = structure(value['content'].encode(), filename='config.yaml')
    return value


def quiet(binary, recorder):
    group = {'id': 'spy-group', 'name': '仅保存策略组🧪', 'type': 'select', 'manual_nodes': ['DIRECT']}
    library = {**DOMAIN, 'id': 'spy-library', 'name': '仅保存规则库🧪'}
    flows = {
        'rules': [
            ('POST /api/rules', None, {**RULE, 'id': 'spy-rule', 'value': 'saved.example'}),
            ('PUT /api/rules/<rule_id>', '/api/rules/spy-rule', {'remark': '修改后不发布🧪'}),
            ('PUT /api/rules/dns', None, {'routes': {'spy-rule': 'direct'}, 'base_url': 'http://panel.example'}),
            ('POST /api/rules/reorder', None, {'ids': ['spy-rule'], 'position': 'bottom'}),
            ('DELETE /api/rules/<rule_id>', '/api/rules/spy-rule', None)],
        'groups': [
            ('POST /api/proxy-groups', None, group),
            ('PUT /api/proxy-groups/<group_id>', '/api/proxy-groups/spy-group', {**group, 'name': '改名仍不发布🧪'}),
            ('DELETE /api/proxy-groups/<group_id>', '/api/proxy-groups/spy-group', None)],
        'library': [
            ('POST /api/rule-library', None, library),
            ('PUT /api/rule-library/<rule_id>', '/api/rule-library/spy-library', {**library, 'content': '+.changed.example\n'}),
            ('DELETE /api/rule-library/<rule_id>', '/api/rule-library/spy-library', None)],
        'preview': [('POST /api/generate/mihomo/preview', None, {'base_url': 'http://panel.example'})],
    }
    for name, operations in flows.items():
        reply = lambda request, index: (200, {'success': True})
        with Agent(reply, gate_index=-1) as left, Agent(reply, gate_index=-1) as right:
            fixture = options()
            fixture['agent_port_pairs'] = {'agent-synthetic': [left.port, right.port]}
            with profile_pair(binary, recorder, options=fixture) as pair:
                heartbeat(pair, version='1.9.0-go', variant='T25/' + name + '/online-before-write')
                for index, (route, path, body) in enumerate(operations):
                    last = index == len(operations) - 1
                    def check(response):
                        if name == 'preview':
                            value = structure(response.json()['content'].encode(), filename='config.yaml')
                            require(isinstance(value, dict) and bool(value.get('rules')),
                                'preview really renders the seeded rule composition before the no-push observation')
                            return
                        fields = database(pair.dirs[0])['fields']
                        key = {'rules': 'profile/default/rule_configs', 'groups': 'profile/default/proxy_groups',
                            'library': 'shared//rule_library'}[name]
                        ident = {'rules': 'spy-rule', 'groups': 'spy-group', 'library': 'spy-library'}[name]
                        rows = fields[key]['value']
                        if last:
                            require(all(row['id'] != ident for row in rows),
                                'the real CRUD sequence has completed before observing the entire Agent wire')
                        else:
                            saved = next(row for row in rows if row['id'] == ident)
                            require(all(saved.get(field) == body[field] for field in (
                                'value', 'remark', 'name', 'content') if field in body),
                                'the source actually persists the intended edit, rather than a no-op')
                            if name == 'rules' and index == 2:
                                require(response.json()['rules'][ident]['route'] == 'direct', 'the real DNS choice is stored')
                            if name == 'rules' and index == 3:
                                require(rows[-1]['id'] == ident, 'the saved rule is actually moved to the bottom')
                    observer = NoRequests([left, right]) if last else None
                    pair.request('T25', route, path, body=json_body(body) if body is not None else None,
                        expect=200, writes=True, check=check, variant=name + '/operation-' + str(index + 1),
                        interaction=observer,
                        projection=preview if name == 'preview' else None)
                    if last:
                        recorder.results[-1]['agent_observations'] = copy.deepcopy(observer.observed)
                        recorder.results[-1]['agent_observation_seconds'] = observer.wait_seconds


GROUPS = {'writes-real-online-Agent-no-push-or-activate': quiet}
