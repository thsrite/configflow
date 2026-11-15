#!/usr/bin/env python3
"""C09 adversarial review: Python reference results for edge inputs.

Run from the repository root (CPython 3.11, production modules):

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rules/testdata/review_c09_gen.py \
        server/internal/rules/testdata/review_c09.json.gz

Synthetic values only. Sections: filters (filter_candidates), validate
(validate_filters), lower (lower_group_provider_filters with odd provider
names: the clone digest is sha256(repr(key))), reorder (resolve_new_order),
policies (validate_rule_policies), destination (_destination_network) and
apply (connection_rules.apply_ip with the repository/discovery stubbed).
"""
import copy
import gzip
import json
import sys

from backend.utils import connection_rules
from backend.utils import group_candidates as gc
from backend.utils import reorder
from backend.utils import strategy_references

assert sys.version_info[:2] == (3, 11), sys.version


def error_of(exc):
    return {'exception': type(exc).__name__, 'message': str(exc)}


NAMES = ['İstanbul', 'istanbul', 'ISTANBUL', 'ıI', 'i̇', '１２３', '123', '٣', '香港01', '香港 IPLC', '🇺🇸 US-01',
         'US-01', '', 'a\nb', 'ǅ', 'ß', 'SS', 'ﬃ', 'K', 'k', 'ſ', 's', 'Ω', 'ω', 'é', 'é', '　US', 'x' * 300,
         'US`old', 'HK\\x', '日本-東京', 'Ⅻ', '²', 'a_b', 'a-b']

PATTERNS = [None, '', '   ', '　', ' US ', ' us ', '(?i)us', '(?i)İ', '(?i)ı', '(?i)i', '(?i)ß', '(?i)ss',
            '(?i)ﬃ', '(?i)k', '(?i)s', '(?i)ω', r'\d', r'^\d+$', r'[０-９]', r'\w+$', r'^\W', r'\s', r'^$', '.',
            r'(?<=香)港', r'(?<!香)港', r'(\w)\1', r'\bUS\b', r'(?i)\bus\b', '🇺🇸', r'[一-鿿]', r'^.{300}$',
            r'(?a)\d', r'(?a)\w+$', r'(?u)\w', r'(?s)a.b', r'(?m)^b$', r'a$', r'(?x) U S ', r'é', r'é',
            r'[[:alpha:]]', r'\N{LATIN SMALL LETTER A}', r'(?P<x>U)(?P=x)', r'(?i:us)-01', r'US|HK', '`', r'\`',
            '[', '(?<=a+)b', 'a{2,1}', '*', r'\p{L}', 5, True, [], {}]


def filter_cases():
    items = [{'name': name, 'extra': index} for index, name in enumerate(NAMES)]
    out = []
    for include in PATTERNS:
        for exclude in (None, '', 'US', '(?i)us'):
            try:
                kept = [item['extra'] for item in gc.filter_candidates(items, include, exclude)]
                result = {'kept': kept}
            except gc.CandidateValidationError as exc:
                result = {'error': str(exc)}
            except Exception as exc:  # noqa: BLE001
                result = error_of(exc)
            out.append({'include': include, 'exclude': exclude, 'result': result})
    for exclude in PATTERNS:
        try:
            kept = [item['extra'] for item in gc.filter_candidates(items, '', exclude)]
            result = {'kept': kept}
        except gc.CandidateValidationError as exc:
            result = {'error': str(exc)}
        except Exception as exc:  # noqa: BLE001
            result = error_of(exc)
        out.append({'include': '', 'exclude': exclude, 'result': result})
    return {'names': NAMES, 'cases': out}


def validate_cases():
    out = []
    values = PATTERNS + [1.0, 1, False, None, '(?i)us']
    for field in gc.FILTER_FIELDS:
        for value in values:
            for original in (None, {}, {field: value}, {field: copy.deepcopy(value) if not isinstance(value, bool) else int(value)},
                             {field: 1.0 if value == 1 and not isinstance(value, float) else value}):
                group = {field: value}
                try:
                    gc.validate_filters(group, original)
                    result = ''
                except gc.CandidateValidationError as exc:
                    result = str(exc)
                except Exception as exc:  # noqa: BLE001
                    result = error_of(exc)
                out.append({'group': group, 'original': original, 'result': result})
    return out


ODD = ["it's", 'say "hi"', 'both \'"', 'back\\slash', 'new\nline', 'tab\there', 'del\x7f', 'nul\x00', 'wide　space',
       'zw​sp', 'emoji 🇭🇰', 'combining é', 'ls ', 'cjk 香港', 'bidi ‮', 'nbsp\xa0', 'latin1 \xe9',
       'astral \U0001F600', 'private ', 'unassigned ͸', 'surrogate-free \U00010000', 'ctrl \x1b[0m']


def lower_cases():
    out = []
    for index, name in enumerate(ODD):
        other = ODD[(index + 1) % len(ODD)]
        shared = {
            'subscriptions': [{'id': 's1', 'name': name}, {'id': 's2', 'name': other + ' 2'}],
            'subscription_aggregations': [{'id': 'a1', 'name': 'agg ' + name, 'subscriptions': []}],
            'nodes': [],
        }
        excludes = [('过期', name), ('`' + name, ''), ('', 'x`y'), ('a\\`b', '(?i)' + name)]
        for exclude, agg_exclude in excludes:
            group = {'id': 'g1', 'name': 'G ' + name, 'type': 'select', 'subscriptions': ['s1', 's2'],
                     'aggregations': ['a1'], 'regex': name if index % 2 else '', 'exclude_regex': exclude,
                     'aggregation_regex': '', 'aggregation_exclude_regex': agg_exclude}
            follower = {'id': 'g2', 'name': 'F ' + name, 'type': 'select', 'follow_group': 'g1'}
            config = {**shared, 'proxy_groups': [group, follower]}
            providers = {item['name']: {'type': 'http', 'url': 'https://sub.invalid/' + item['id'], 'path': './p/' + item['id']}
                         for item in shared['subscriptions'] + shared['subscription_aggregations']}
            main = {'proxies': [{'name': 'G ' + name + '_' + name + '_筛选'}], 'proxy-providers': providers,
                    'proxy-groups': [{'name': 'G ' + name, 'type': 'select', 'use': list(providers)},
                                     {'name': 'F ' + name, 'type': 'select', 'use': list(providers)[:2]}]}
            before = copy.deepcopy(main)
            try:
                gc.lower_group_provider_filters(copy.deepcopy(config), main)
                result = main
            except Exception as exc:  # noqa: BLE001
                result = error_of(exc)
            out.append({'config': config, 'main': before, 'result': result})
    return out


def reorder_cases():
    items = [{'id': 'a'}, {'id': 'é'}, {'id': 'é'}, {'id': None}, 'raw', {'id': 'İ'}, {'name': 'no id'}, {'id': 'a'},
             {'id': '１'}, {'id': '1'}]
    bodies = [
        {'ids': ['é', 'a']}, {'ids': ['é']}, {'ids': ['1', '１'], 'position': 'bottom'}, {'ids': ['İ'], 'position': None},
        {'ids': ['a'], 'position': 'TOP'}, {'ids': ['a'], 'position': 1}, {'ids': []}, {'ids': [], 'position': 'bottom'},
        {'ids': ['a', 'a']}, {'ids': ['x']}, {'ids': ['x', 'y', 'a']}, {'ids': [None]}, {'ids': 'a'}, {'ids': {}},
        {'ids': None, 'rules': [{'id': 'é'}, {'id': 'a'}]}, {'rules': [{'id': 'é'}, 'x']}, {'rules': 'x'},
        {'rules': []}, {'rules': [{'name': 'no id'}]}, {'rules': [{'id': 'a'}, {'id': 'a'}]}, {},
        {'ids': ['a', 'é', 'é', 'İ', '１', '1']}, {'ids': ['ı']},
    ]
    out = []
    for body in bodies:
        try:
            ordered, missing = reorder.resolve_new_order(copy.deepcopy(items), copy.deepcopy(body), 'rules')
            result = {'ordered': ordered, 'missing': missing}
        except Exception as exc:  # noqa: BLE001
            result = error_of(exc)
        out.append({'items': items, 'body': body, 'result': result})
    return out


def policy_cases():
    rules = [
        {'id': 'r1', 'itemType': 'rule', 'policy': 'DIRECT'},
        {'id': 1.5, 'itemType': 'rule', 'policy': 'Ghost'},
        {'name': '名字', 'itemType': 'ruleset'},
        {'id': None, 'name': 'n', 'itemType': 'rule'},
        {'itemType': 'rule', 'policy': None},
        {'id': True, 'itemType': 'ruleset', 'policy': 'proxy'},
        {'id': 'x', 'itemType': 'ruleset', 'policy': '香港 🇭🇰'},
        {'id': 'off', 'itemType': 'rule', 'policy': 'Ghost', 'enabled': False},
        {'id': 'off2', 'itemType': 'rule', 'policy': 'Ghost', 'library_enabled': 0},
        {'id': 'off3', 'itemType': 'rule', 'policy': 'Ghost', 'enabled': ''},
        {'id': 'other', 'itemType': 'RULE', 'policy': 'Ghost'},
        {'id': 'num', 'itemType': 'rule', 'policy': 5},
        {'id': {'a': 1}, 'itemType': 'rule', 'policy': 'Nope'},
        {'id': 'f', 'itemType': 'rule', 'policy': 'CELLULAR'},
        {'id': 'g', 'itemType': 'rule', 'policy': 'REJECT-IMG'},
        {'id': 'h', 'itemType': 'rule', 'policy': 'COMPATIBLE'},
        {'id': 'i', 'itemType': 'rule', 'policy': 'direct'},
        {'id': 'j', 'itemType': 'rule', 'policy': 'Proxy'},
        {'id': 'k', 'itemType': 'rule', 'policy': 'PROXY'},
    ]
    out = []
    for fmt in ('mihomo', 'surge', 'loon'):
        for rule in rules:
            config = {'rule_configs': [rule]}
            for names in ([], ['Ghost', '香港 🇭🇰', 'proxy']):
                try:
                    strategy_references.validate_rule_policies(config, names, fmt)
                    result = ''
                except strategy_references.StrategyReferenceError as exc:
                    result = str(exc)
                except Exception as exc:  # noqa: BLE001
                    result = error_of(exc)
                out.append({'config': config, 'names': names, 'format': fmt, 'result': result})
    return out


VALUES = ['192.0.2.8', ' 192.0.2.8 ', '　192.0.2.8　', '192.0.2.8\n', '１９２.0.2.8', '192.0.2.8/024',
          '192.0.2.8/ 24', '192.0.2.8/24 ', '192.0.2.8/0024', '192.0.2.8/٢٤', '192.0.2.8/', '/24', '', '192.0.2.8/32/',
          '192.0.2.8/255.255.255.0', '0x7f.0.0.1', '127.1', '1.2.3.04', '1.2.3.4.', '::ffff:192.0.2.8/120',
          '::ffff:192.0.2.8/95', '::ffff:192.0.2.8/96', '::ffff:0.0.0.0', '::ffff:224.0.0.1', '1:2:3:4:5:6:7::',
          '::1:2:3:4:5:6:7', '::', '::/0', '0.0.0.0/0', '0.0.0.0/32', '224.0.0.0/3', '224.0.0.0/4', '239.255.255.255/32',
          'ff00::/8', 'ff00::/7', 'fe80::1%eth0', 'FE80::ABCD', '2001:DB8::/32', '2001:db8::1/129', '2001:db8::1/128',
          '00001::', '::ffff:01.2.3.4', '1::2::3', '255.255.255.255', '255.255.255.255/31', '10.0.0.0/8', '10.1.2.3/8',
          '::ffff:10.1.2.3/104', '64:ff9b::192.0.2.8', '2001:db8::192.0.2.8/120', 'a' * 50, 5, True, None, ['192.0.2.8']]


def destination_cases():
    out = []
    for value in VALUES:
        for rule_type in ('IP-CIDR', 'IP-CIDR6', 'IP', 'ip-cidr'):
            try:
                result = {'network': str(connection_rules._destination_network(value, rule_type))}
            except Exception as exc:  # noqa: BLE001
                result = error_of(exc)
            out.append({'value': value, 'rule_type': rule_type, 'result': result})
    return out


CONTENTS = [
    '', 'IP-CIDR,192.0.2.0/24,no-resolve', 'IP-CIDR,192.0.2.0/255.255.255.0', 'IP-CIDR,192.0.2.0/0.0.0.255',
    'IP-CIDR6,fe80::%eth0/64', 'IP-CIDR6,fe80::1%eth0/64', 'IP-CIDR6,fe80::/64', 'IP,192.0.2.8', 'IP,192.0.2.9',
    'IP-CIDR,0.0.0.0/0', 'IP-CIDR6,::/0', 'IP-CIDR,::ffff:192.0.2.0/120', 'IP-CIDR6,::ffff:192.0.2.0/120',
    'DOMAIN,example.test\nIP-CIDR,bad', '# c\n192.0.2.0/24\n', '192.0.2.8', 'IP-CIDR,192.0.2.8/32,no-resolve,extra',
    'payload:\n  - IP-CIDR,192.0.2.0/24\n', 'payload: [broken', 'ip-cidr,192.0.2.0/24', ' IP-CIDR , 192.0.2.0/24 ',
    'IP-CIDR,192.0.2.0/024', 'IP-CIDR6,2001:db8::/32\r\nIP-CIDR,10.0.0.0/8', 'IP-ASN,1234', 'IP-CIDR,192.0.2.008/32',
]


def apply_cases():
    out = []
    for behavior in ('classical', 'ipcidr'):
        for content in CONTENTS:
            for value, rule_type in (('192.0.2.8', 'IP-CIDR'), ('fe80::/64', 'IP-CIDR6'), ('fe80::1', 'IP-CIDR6'),
                                     ('::ffff:192.0.2.8', 'IP-CIDR'), ('192.0.2.0/25', 'IP-CIDR')):
                for policy in ('DIRECT', 'PROXY', 'REJECT', None):
                    shared = {'rule_library': [{'id': 'lib', 'name': '规则 lib', 'source_type': 'content',
                                                'behavior': behavior, 'content': content}]}
                    references = [] if policy is None else [{'enabled': True, 'after_match': False, 'policy': policy}]

                    class Repo:
                        def get_compat_config(self, profile_id):
                            return {'rule_configs': []}

                    def ruleset_info(config, ident, references=references):
                        item = next(item for item in config['rule_library'] if item['id'] == ident)
                        return {'id': ident, 'name': item['name'], 'behavior': item.get('behavior', 'classical'),
                                'references': references}

                    def transaction(updater, shared=shared):
                        updater(shared)

                    connection_rules.get_repository = Repo
                    connection_rules.discovery = type('D', (), {'ruleset_info': staticmethod(ruleset_info)})
                    connection_rules.update_shared_config_transaction = transaction
                    connection_rules.save_rule_to_local = lambda item: None
                    target = 'direct' if policy in ('DIRECT', None) else 'proxy'
                    try:
                        result = connection_rules.apply_ip('p', 'lib', target, value, rule_type, lambda: None)
                        result = {'result': result, 'content': shared['rule_library'][0]['content']}
                    except Exception as exc:  # noqa: BLE001
                        result = error_of(exc)
                    out.append({'behavior': behavior, 'content': content, 'value': value, 'rule_type': rule_type,
                                'policy': policy, 'target': target, 'result': result})
    return out


def main():
    data = {
        'filters': filter_cases(),
        'validate': validate_cases(),
        'lower': lower_cases(),
        'reorder': reorder_cases(),
        'policies': policy_cases(),
        'destination': destination_cases(),
        'apply': apply_cases(),
    }
    text = json.dumps(data, ensure_ascii=True, indent=0, sort_keys=False) + '\n'
    with gzip.GzipFile(sys.argv[1], 'wb', mtime=0) as handle:
        handle.write(text.encode())


if __name__ == '__main__':
    main()
