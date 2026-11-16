"""Five independent custom editors, field merge semantics and Mihomo guards."""
import copy
import json

from compare import database
from harness import Pair, DesignError, json_body
from publish import check as guard_check, capture as guard_capture
from cases.helpers import profile_capture, profile_policy, deny_writes, exception_policy
from cases.config_backup import message

IDS = {'loon': (35, 36), 'mihomo': (37, 38), 'mosdns': (39, 40), 'oxidns': (41, 42), 'surge': (43, 44)}
TEXTS = {'loon': '# 中文🧪\n[General]\nloglevel = notify\n', 'mihomo': '# 中文🧪\nipv6: true\ndns:\n  ipv6: false\n',
         'mosdns': '# 中文🧪\nplugins: []\n', 'oxidns': '# 中文🧪\nplugins: []\n', 'surge': '# 中文🧪\n[General]\nloglevel = notify\n'}


def setup(pair):
    pair.policy = profile_policy(pair)
    return profile_capture(pair)


def save(pair, service, case, data, *, path=None, headers=None, variant='', profile='default', expect=200, policy=None, check=None):
    route = 'POST /api/custom-config/' + service
    before = database(pair.dirs[0])['fields']
    def reference(reply):
        if check:
            check(reply)
        if expect == 200:
            if service in ('mihomo', 'surge', 'mosdns'):
                guard_check(pair, profile, service)(reply)
            rows = database(pair.dirs[0])['fields']
            for key in before:
                if key.startswith('profile/' + ('other' if profile == 'default' else 'default') + '/') and rows.get(key) != before[key]:
                    raise DesignError('Python custom save changed unselected profile')
            if isinstance(data, dict) and 'config' in data:
                actual = rows['profile/' + profile + '/' + service]['value']['custom_config']
                if actual != data['config']:
                    raise DesignError('Python custom editor did not save exact config field')
    capture = guard_capture(pair, profile, service) if expect == 200 and service in ('mihomo', 'surge', 'mosdns') else profile_capture(pair)
    return pair.request(f'R{IDS[service][1]:03}-' + case, route, path,
                        body=json_body(data), headers=headers, variant=variant, expect=expect,
                        writes=True, policy=policy, check=reference, capture=capture)


def basic(binary, recorder):
    for service, (read_id, write_id) in IDS.items():
        with Pair(binary, recorder) as pair:
            capture = setup(pair)
            route = 'GET /api/custom-config/' + service
            pair.request(f'R{read_id:03}-S01', route, expect=200, writes=True, capture=capture)
            save(pair, service, 'S01', {'config': TEXTS[service]})
            pair.request(f'R{read_id:03}-S02', route, expect=200, writes=True, capture=capture)
            save(pair, service, 'B01' if service in ('loon', 'mihomo', 'surge') else 'S02',
                 {'smart_groups': []} if service == 'surge' else {'config': ''}, variant='clear')
            pair.request(f'R{read_id:03}-B01', route, expect=200, writes=True, capture=capture)
            if service == 'loon':
                save(pair, service, 'S02', {})
                save(pair, service, 'B02', {'config': TEXTS[service]})
            elif service == 'mihomo':
                save(pair, service, 'S02', {'config': ''})
                save(pair, service, 'S03', {'config': '', 'easytier': {'enabled': True,
                    'options': {'network-name': 'synthetic', 'network-secret': 'synthetic-secret', 'no-listener': False}}})
                save(pair, service, 'S04', {'config': '', 'provider_update_interval': 60})
                save(pair, service, 'B02', {'config': '# comment only\n'})
                save(pair, service, 'B03', {'config': '# 中文🧪\nlog-level: info\n'})
            elif service == 'mosdns':
                save(pair, service, 'S02', {})
                save(pair, service, 'B01', {'config': 'plugins: [this is intentionally invalid YAML'})
            elif service == 'oxidns':
                save(pair, service, 'S02', {'config': ''})
                save(pair, service, 'B01', {'config': TEXTS[service]})
            elif service == 'surge':
                save(pair, service, 'S02', {'smart_groups': []})
                save(pair, service, 'S03', {'config': TEXTS[service], 'smart_groups': []})
                save(pair, service, 'S04', {})
                save(pair, service, 'B02', {'config': None})
            # Long text is not assigned an invented limit. Generation is a separate step.
            save(pair, service, 'S01', {'config': '# 中文🧪' + 'x' * 65536 + '\n'}, variant='64KiB')


def selectors(binary, recorder):
    for service, (read_id, write_id) in IDS.items():
        with Pair(binary, recorder, options={'profile': {service: {'custom_config': '# default\n'}},
                                           'other_profile': {service: {'custom_config': '# other\n'}}}) as pair:
            setup(pair)
            first = 6 if service == 'mihomo' else 3 if service == 'mosdns' else 4
            for offset, (query, headers, profile) in enumerate([
                ('?profile=default&profile_id=other', {'X-ConfigFlow-Profile': 'other'}, 'default'),
                ('', {'X-ConfigFlow-Profile': 'other'}, 'other'), ('', {'X-ConfigFlow-Profile': None}, 'default')]):
                base = '/api/custom-config/' + service
                def check(reply):
                    if reply.json()['config'] != database(pair.dirs[0])['fields']['profile/' + profile + '/' + service]['value']['custom_config']:
                        raise DesignError('Python custom selector resolved another profile')
                pair.request(f'R{read_id:03}-B{3 + offset:02}', 'GET ' + base, base + query,
                             expect=200, headers=headers, check=check, writes=True)
                save(pair, service, f'B{first + offset:02}', {'config': '# selector ' + profile + '\n'},
                     path=base + query, headers=headers, profile=profile, variant='P' + str(offset + 4))


def wrappers_and_failures(binary, recorder):
    for service, (_, write_id) in IDS.items():
        with Pair(binary, recorder) as pair:
            setup(pair)
            route = 'POST /api/custom-config/' + service
            if service == 'oxidns':
                for case, data in [('F08', []), ('F09', {}), ('F10', {'config': 3})]:
                    save(pair, service, case, data, expect=400, check=message('config 必须是 YAML 文本'))
            else:
                bad_json = {'loon': 'F08', 'mihomo': 'F13', 'mosdns': 'F09', 'surge': 'F08'}[service]
                pair.request(f'R{write_id:03}-' + bad_json, route, body='{bad', expect=500,
                             writes=True, policy=exception_policy(pair))
                if service in ('mihomo', 'mosdns'):
                    save(pair, service, 'F13' if service == 'mihomo' else 'F08', [1], expect=500, policy=exception_policy(pair), variant='non-object')
                save_failure = {'loon': 'F09', 'mihomo': 'F14', 'mosdns': 'F10', 'surge': 'F09'}[service]
                with deny_writes(pair):
                    save(pair, service, save_failure, {'config': TEXTS[service]}, expect=500, policy=exception_policy(pair))
    with Pair(binary, recorder, options={'x01_native': True}) as pair:
        setup(pair)
        before = database(pair.dirs[0])['fields']['profile/default/oxidns']['value']['native_config']
        def check(reply):
            if database(pair.dirs[0])['fields']['profile/default/oxidns']['value']['native_config'] != before:
                raise DesignError('Python extension editor overwrote native_config')
        save(pair, 'oxidns', 'B02', {'config': '# independent extension\n'}, check=check)
    with Pair(binary, recorder, options={'profile': {'oxidns': {'api': {'enabled': 'legacy-invalid'}}}}) as pair:
        save(pair, 'oxidns', 'F11', {'config': ''}, expect=400)
    with Pair(binary, recorder, options={'profile': {'mihomo': {'custom_config': 'mixed-port: "invalid-legacy"\n'}}}) as pair:
        save(pair, 'mihomo', 'B04', {'config': 'mixed-port: "invalid-legacy"\n# unrelated comment\n'})


GROUPS = {'custom-config-basic': basic, 'custom-config-selectors': selectors,
          'custom-config-failures': wrappers_and_failures}
