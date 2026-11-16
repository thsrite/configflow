"""G06: every Python rule vocabulary member in each public client preview."""
import ast

import yaml

from harness import Pair, ROOT, DesignError, json_body
from cases.generator_shapes import fixture, node, project
from cases.subscriptions import require

VALUES = {
    'DOMAIN': '中文.example', 'DOMAIN-SUFFIX': 'example.com', 'DOMAIN-KEYWORD': 'keyword',
    'DOMAIN-REGEX': '^a,b[.]example$', 'DOMAIN-WILDCARD': '*.example.net',
    'GEOSITE': 'cn', 'GEOIP': 'CN', 'SRC-GEOIP': 'CN', 'IP-ASN': '64512', 'SRC-IP-ASN': '64513',
    'IP-CIDR': '192.0.2.0/24', 'IP-CIDR6': '2001:db8::/32', 'SRC-IP-CIDR': '198.51.100.0/24',
    'IP-SUFFIX': '192.0.2.0/24', 'SRC-IP-SUFFIX': '198.51.100.0/24',
    'SRC-PORT': '80/443', 'DST-PORT': '443/8000-8002', 'IN-PORT': '10080', 'DSCP': '10/20-30',
    'PROCESS-NAME': 'synthetic-process', 'PROCESS-PATH': '/usr/bin/synthetic-process',
    'PROCESS-NAME-REGEX': '^synthetic-.*$', 'PROCESS-PATH-REGEX': '^/usr/bin/synthetic-.*$',
    'PROCESS-NAME-WILDCARD': 'synthetic-*', 'PROCESS-PATH-WILDCARD': '/usr/bin/synthetic-*',
    'NETWORK': 'tcp', 'UID': '1000/2000-2002', 'IN-TYPE': 'mixed', 'IN-USER': 'synthetic-user',
    'IN-NAME': 'synthetic-listener', 'REMATCH-NAME': 'synthetic-rule', 'SUB-RULE': '(NETWORK,tcp)',
    'AND': '((DOMAIN,logic.example),(NETWORK,tcp))', 'OR': '((DOMAIN,or.example),(NETWORK,udp))',
    'NOT': '((DOMAIN,not.example))', 'RULE-SET': 'synthetic-provider', 'MATCH': '',
}


def shapes(binary, recorder):
    # Read only the explicitly allowed Python source vocabulary. A new Python
    # rule kind must get a deliberate recipe, never silently lose coverage.
    tree = ast.parse((ROOT / 'backend/utils/rule_syntax.py').read_text())
    vocabulary = next(ast.literal_eval(statement.value.args[0]) for statement in tree.body
                      if isinstance(statement, ast.Assign) and any(isinstance(target, ast.Name) and target.id == 'RULE_TYPES' for target in statement.targets))
    if set(VALUES) != vocabulary:
        raise DesignError('G06 recipes do not cover the exact Python RULE_TYPES set')
    for service in ('mihomo', 'surge', 'loon'):
        opts = fixture(node('socks5', {'username': 'synthetic', 'password': 'synthetic'}))
        opts['profile']['proxy_groups'].append({'id': 'child-group', 'name': 'child', 'type': 'select', 'manual_nodes': ['DIRECT']})
        opts['profile']['rule_configs'] = [{'id': 'vocabulary-' + str(index), 'itemType': 'rule', 'rule_type': kind,
            'value': value, 'policy': 'child' if kind == 'SUB-RULE' else 'DIRECT', 'enabled': True,
            **({'udp_fallback': 'REJECT', 'ntp_direct': True} if kind == 'MATCH' else {})}
            for index, (kind, value) in enumerate(VALUES.items())]
        opts['profile']['mihomo'] = {'custom_config': 'dns:\n  enable: true\nrule-providers:\n  synthetic-provider:\n    type: inline\n    behavior: domain\n    payload: [+.example.com]\nsub-rules:\n  child:\n    - DOMAIN,child.example,DIRECT\n'}
        with Pair(binary, recorder, options=opts) as pair:
            def check(reply):
                text = reply.json()['content']
                if service == 'mihomo':
                    document = yaml.safe_load(text)
                    emitted = {line.split(',', 1)[0] for line in document['rules']}
                    require(set(VALUES) <= emitted, 'Mihomo output retains every configured rule kind and extra MATCH UDP/NTP handling')
                else:
                    require('FINAL,DIRECT' in text and 'synthetic-provider' in text and 'logic.example' in text,
                            'client renderer includes FINAL, provider and logical rules with its unsupported-type comments retained')
            pair.request('G06', 'POST /api/generate/' + service + '/preview', body=json_body({'base_url': 'http://panel.example'}),
                         headers={'Host': 'panel.example'}, expect=200, writes=True, projection=project(service), check=check,
                         variant='all-' + str(len(VALUES)) + '-rule-types/' + service)


GROUPS = {'generator-rule-types': shapes}
