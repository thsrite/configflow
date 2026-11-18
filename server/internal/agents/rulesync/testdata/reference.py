"""Real Python projection oracle; synthetic sources and no network."""
import network_guard  # noqa: F401
import copy
import json
from backend.test_rule_sync_projection import fixture, native, add_set
from backend.agents.rule_sync_projection import project_rule_changes, rule_changes

cases = []
for service in ('mihomo', 'oxidns'):
    for mode in ('template', 'native') if service == 'oxidns' else ('template',):
        for selected in ('library', 'rules', 'both', 'off'):
            for change in ('content', 'inline', 'new-set', 'unused', 'missing-source'):
                base = fixture()
                if mode == 'native':
                    base = native(base)
                current = copy.deepcopy(base)
                current['nodes'][0]['server'] = 'draft.example.com'
                current['oxidns']['local_dns'] = 'udp://192.0.2.9'
                if change == 'content':
                    current['rule_library'][0]['content'] = 'new.example.com'
                elif change == 'inline':
                    current['rule_configs'][1]['value'] = 'inline.example.com'
                elif change == 'new-set':
                    add_set(current)
                    current['oxidns']['proxy_rulesets'] = ['set-b']
                elif change == 'unused':
                    current['rule_library'].append({'id': 'unused', 'source_type':'content', 'content':'unused.example.com'})
                else:
                    current['rule_configs'][0]['library_rule_id'] = 'missing'
                settings = {'library': selected in ('library','both'), 'rules': selected in ('rules','both')}
                case = dict(name=f'{service}/{mode}/{selected}/{change}',base=base,current=current,settings=settings,service=service)
                try:
                    kind, projected, withheld = rule_changes(base,current,service,settings)
                    case.update(kind=kind,projected=projected,withheld=sorted(withheld))
                except ValueError as error:
                    case['error'] = str(error)
                cases.append(case)
print(json.dumps(cases, ensure_ascii=False))
