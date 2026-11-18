"""Actual Python converter and parser; no emulated YAML constructors.

Mapping comparison uses compose pairs so hashability and bool/int key collisions
cannot hide a changed/extra pair. Scalar values come from the actual safe loader.
"""
import json
import sys
import yaml
from backend.converters import mosdns as m


def semantic(text):
    def visit(node):
        if isinstance(node, yaml.ScalarNode):
            value = yaml.safe_load(yaml.serialize(node))
            if isinstance(value, float):
                value = value.hex()
            elif isinstance(value, int) and not isinstance(value, bool):
                value = str(value)
            return [node.tag, value]
        if isinstance(node, yaml.SequenceNode):
            return [node.tag, [visit(item) for item in node.value]]
        pairs = [[visit(key), visit(value)] for key, value in node.value]
        pairs.sort(key=lambda item: json.dumps(item[0], sort_keys=True))
        return [node.tag, pairs]
    return visit(yaml.compose(text))


for line in sys.stdin:
    request = json.loads(line)
    fn, args = request['fn'], request['args']
    try:
        if fn == 'generate':
            result = m.generate_mosdns_config(*args)
        elif fn == 'upstreams':
            result = {'dump': yaml.dump(m.parse_dns_upstreams(*args), Dumper=m.IndentDumper, allow_unicode=True, sort_keys=False, indent=2)}
        elif fn == 'convert':
            result = m.convert_rule_content_for_mosdns(*args)
        else:
            functions = {
                'domain_pattern': m.domain_pattern_for_mosdns,
                'parse_clash': m.parse_clash_rule_value,
                'downloads': m.get_mosdns_ruleset_downloads,
                'custom_files': m.get_mosdns_custom_files,
                'split': m.split_rules_and_rulesets,
                'proxy_url': m._build_rule_proxy_url,
                'ruleset_files': m._ruleset_files,
                'custom_matches': m._parse_custom_matches,
            }
            result = functions[fn](*args)
        answer = {'out': result}
        if 'actual_yaml' in request:
            expected = result if fn == 'generate' else result['dump']
            left, right = semantic(expected), semantic(request['actual_yaml'])
            answer['yaml_equal'] = left == right
            if left != right:
                def difference(a, b, path=''):
                    if type(a) != type(b): return f'{path}: {type(a).__name__}/{type(b).__name__}'
                    if isinstance(a, list):
                        if len(a) != len(b): return f'{path}: lengths {len(a)}/{len(b)}'
                        for i, (x, y) in enumerate(zip(a, b)):
                            if x != y: return difference(x, y, f'{path}/{i}')
                    return f'{path}: {a!r}/{b!r}'
                answer['yaml_difference'] = difference(left, right)
    except Exception as error:
        answer = {'err': [type(error).__name__, str(error)]}
    print(json.dumps(answer, ensure_ascii=False), flush=True)
