"""Random corpus for routers R02 (router_split, rule_ip_presence), recorded by
real Python through the r02_record plugin's dispatcher, in the same row format
as r02_recorded.jsonl.gz (routersplit/corpus_test.go, ippresence/corpus_test.go).

    rt --back server/internal/routersplit/testdata/r02_corpus.jsonl.gz -- bash -lc \
        'PYTHONPATH=.:server/internal/routersplit/testdata ~/cf-test/.venv/bin/python server/internal/routersplit/testdata/gen_r02.py 4000'

Synthetic values only (documentation addresses, *.test hosts).
"""
import gzip
import hashlib
import json
import os
import random
import shutil
import sys
import tempfile

_DATA = tempfile.mkdtemp(prefix='r02-corpus-')
os.environ['DATA_DIR'] = _DATA
os.environ['LOG_FILE'] = os.path.join(_DATA, 'app.log')

import r02_record  # noqa: E402

R = random.Random(20261116)
FILTERS = ['include-interface', 'exclude-interface', 'include-uid', 'include-uid-range', 'exclude-uid',
           'exclude-uid-range', 'include-mac-address', 'exclude-mac-address', 'include-android-user',
           'include-package', 'exclude-package']
ROUTE_KEYS = ['route-address', 'inet4-route-address', 'inet6-route-address', 'route-address-set',
              'route-exclude-address-set']
FULL = {'enable': True, 'stack': 'mixed', 'auto-route': True, 'auto-redirect': True,
        'auto-detect-interface': True, 'dns-hijack': ['any:53', 'tcp://any:53']}
STRINGS = ['', 'a', 'mihomo-1', 'mihomo-2', 'oxidns-1', '规则', '😀', ' x ', 'x\n', 'x\x7f', 'r' * 256, 'r' * 257,
           'ip', 'a b', '\x00']
AGENTS = ['mihomo-1', 'mihomo-2', 'oxidns-1', 'gone']


def scalar():
    return R.choice([None, True, False, 0, 1, -1, 1.0, 0.0, 2.5, 10 ** 20, '', 'x', 'true', 'any:53', '1', '规则'])


def value(depth=0):
    roll = R.random()
    if depth > 2 or roll < 0.6:
        return scalar()
    if roll < 0.8:
        return [value(depth + 1) for _ in range(R.randint(0, 3))]
    return {R.choice(STRINGS[:6] + ['enabled', 'id']): value(depth + 1) for _ in range(R.randint(0, 3))}


def ids():
    roll = R.random()
    if roll < 0.6:
        return [R.choice(['a', 'b', 'c', 'ip', '规则']) for _ in range(R.randint(0, 5))]
    if roll < 0.7:
        return ['r'] * R.choice([1024, 1025])
    if roll < 0.9:
        return [R.choice(STRINGS + [1, None, ['a']]) for _ in range(R.randint(1, 3))]
    return R.choice(['a', None, {}, 1])


def marker():
    base = {'version': 1, 'takeover': 'full_tun', 'route_rule_set_ids': ids(), 'origin': 'setup'}
    roll = R.random()
    if roll < 0.45:
        return base
    if roll < 0.7:
        key = R.choice(list(base))
        base[key] = R.choice([True, 2, '1', 1.0, 0, 'selective', 'manual', None, 'full_tun', 'setup', 1])
        return base
    if roll < 0.8:
        base.pop(R.choice(list(base)))
        return base
    if roll < 0.9:
        base[R.choice(['extra', 'migration_id', 'migrated_at'])] = value()
        return base
    return R.choice([None, [], 'x', 1, {}])


def tun():
    roll = R.random()
    if roll < 0.1:
        return R.choice([None, 'on', [], ['enable'], 1, {}])
    t = dict(FULL) if roll < 0.7 else {}
    for _ in range(R.randint(0, 4)):
        kind = R.random()
        if kind < 0.3:
            t[R.choice(FILTERS)] = R.choice([['eth0'], [], None, [1000], 'x', 0, ['00:11:22:33:44:55']])
        elif kind < 0.5:
            t[R.choice(ROUTE_KEYS + ['route-exclude-address'])] = R.choice([['192.0.2.0/24'], [], None])
        elif kind < 0.8:
            t[R.choice(['enable', 'auto-route', 'auto-redirect', 'auto-detect-interface'])] = R.choice(
                [True, False, 'true', 1, None])
        elif kind < 0.9:
            t['dns-hijack'] = R.choice([['any:53'], ['tcp://any:53'], 'any:53', [], ['any:53', 'x'], None, [1]])
        else:
            t[R.choice(['stack', 'device', 'mtu'])] = R.choice(['gvisor', 'system', 'utun', 9000, None])
    return t


def document():
    doc = {} if R.random() < 0.3 else {'mixed-port': 7890, 'mode': R.choice(['rule', 'global'])}
    if R.random() < 0.85:
        doc['tun'] = tun()
    if R.random() < 0.1:
        doc['x-configflow-linux-gateway'] = {'enabled': R.choice([True, False])}
    return doc


def yaml_text():
    import yaml
    roll = R.random()
    if roll < 0.08:
        return R.choice(['', '   \n', '　', '# 名称\n'])
    if roll < 0.16:
        return R.choice(['a: [', '[1, 2]', '- x\n', 'just text', '{a: 1', 'a: b: c', '!!python/name:os.system', '"\\x"'])
    if roll < 0.22:
        return R.choice([
            'tun: &t {enable: true, auto-route: true, auto-redirect: true, dns-hijack: [any:53]}\nother: *t\n',
            'base: &b [1, 2]\nlist: *b\ntun: {enable: true}\n',
            '1: x\ntun: {enable: true, auto-route: true, auto-redirect: true, dns-hijack: [any:53]}\n',
            'tun: {2: y, enable: true, auto-route: true, auto-redirect: true, dns-hijack: [any:53]}\n',
            'when: 2026-11-16\ntun: {enable: true}\n',
            'mode: rule\nname: 名称 😀\ntun: {exclude-interface: []}\n',
            'tun: {enable: yes, auto-route: on, auto-redirect: true, dns-hijack: [any:53]}\n',
            'blob: !!binary aGVsbG8=\n',
            'a: 1\nb: 1.50\nc: 0x1f\nd: ~\n',
            'tun: null\n', 'tun: []\n', 'tun: x\n',
            'm: &m {a: 1}\nn: {<<: *m, b: 2}\n',
            'deep: ' + '[' * 120 + ']' * 120 + '\n',
        ])
    if roll < 0.25:
        return R.choice([None, 1, ['a'], {'a': 1}, True])
    return yaml.safe_dump(document(), allow_unicode=R.random() < 0.5, sort_keys=R.random() < 0.5)


def mihomo_section():
    m = {}
    if R.random() < 0.6:
        m['router_split'] = marker()
    if R.random() < 0.6:
        m['linux_gateway'] = R.choice([{'enabled': True, 'rule_set_ids': ['r']}, {'enabled': False},
                                       {'enabled': 'true'}, {'enabled': 1}, None, [], 'x', {}])
    if R.random() < 0.8:
        m['custom_config'] = yaml_text()
    if R.random() < 0.15:
        m['linux_gateway_restore_point'] = {'mihomo': {}}
    return m


def routers():
    roll = R.random()
    if roll < 0.15:
        return R.choice([None, [], 'x', 1, {'items': 'ab'}, {'items': 5}, {'items': {'r': 1}}, {'items': None}, True])
    items = []
    for i in range(R.randint(0, 3)):
        if R.random() < 0.1:
            items.append(R.choice(['x', 1, None]))
            continue
        router = {'id': R.choice([f'router-{i}', None, 1])}
        if R.random() < 0.8:
            router['proxy_gateway_agent_id'] = R.choice(AGENTS + [None, 1, ['mihomo-1'], {'a': 1}])
        items.append(router)
    return {'items': items}


def config():
    roll = R.random()
    if roll < 0.05:
        return R.choice([None, [], 'x', 1])
    c = {}
    if roll < 0.12:
        c['mihomo'] = R.choice([None, [], 'x', ['linux_gateway_restore_point'], 1, True, {}])
    else:
        c['mihomo'] = mihomo_section()
    if R.random() < 0.7:
        c['routers'] = routers()
    return c


def context():
    roll = R.random()
    if roll < 0.05:
        return R.choice([None, [], 'x'])
    c = {}
    for key, choices in (('session_goal', ['router', 'gateway', None, 'other', 1]),
                         ('has_gateway_session', [True, False, 1, 0, 'x', '', None]),
                         ('session_id', ['0' * 32, None, 1]),
                         ('mihomo_agent_ids', [list(R.sample(AGENTS, R.randint(0, 3))), 'mihomo-1', None, [],
                                               {'mihomo-1': 1}, 5, [['x']], [1, 1.0, True]])):
        if R.random() < 0.8:
            c[key] = R.choice(choices)
    return c


def sessions():
    out = []
    for i in range(R.randint(0, 4)):
        s = {'id': f'{R.randint(0, 99):032x}', 'goal': R.choice(['router', 'gateway', 'other', None])}
        for key in ('abandoned', 'completed'):
            if R.random() < 0.4:
                s[key] = R.choice([True, False, 1, 0, 'x', '', None])
        out.append(s)
    return out


def bindings():
    roll = R.random()
    if roll < 0.05:
        return {}
    agents = []
    for _ in range(R.randint(0, 3)):
        a = {'id': R.choice(AGENTS + [1])}
        if R.random() < 0.9:
            a['service_type'] = R.choice(['mihomo', 'oxidns', None])
        if R.random() < 0.05:
            a.pop('id')
        agents.append(a)
    if R.random() < 0.05:
        agents = R.choice(['x', 5, ['x']])
    return {'agents': agents, 'default_profile_id': 'home'}


class FakeRepository:
    """Answers get_profile_agent_bindings / get_profile; the recorder wraps it."""

    def __init__(self):
        self.bindings = bindings()
        self.profile = config()
        self.missing = R.random() < 0.05

    def get_profile_agent_bindings(self, profile_id):
        if self.missing:
            from backend.common.config_repository import ProfileNotFound
            raise ProfileNotFound(profile_id)
        return json.loads(json.dumps(self.bindings))

    def get_profile(self, profile_id):
        return json.loads(json.dumps(self.profile)) if isinstance(self.profile, dict) else {}


def install_sessions():
    from backend.common import setup_flow
    answer = sessions()
    failing = R.random() < 0.03

    def profile_sessions(repository, profile_id):
        if failing:
            raise OSError('synthetic read failure')
        return json.loads(json.dumps(answer))
    setup_flow.profile_sessions = profile_sessions


# ---- classical_ip_lines ----

LINES = ['DOMAIN-SUFFIX,tiktok.test', 'DOMAIN,ip.test', 'IP-CIDR,203.0.113.0/24,no-resolve', 'IP-CIDR6,2001:db8::/32',
         '# comment', '', '   ', 'IP-ASN,13335', 'GEOIP,CN', '192.0.2.1', '198.51.100.0/24', '  # x', 'ip.test',
         'PROCESS-NAME,x', 'DOMAIN-KEYWORD,musical', 'full:a.test', '+.b.test', '2001:db8::1', 'IP-CIDR,bad',
         'AND,((DOMAIN,a.test),(IP-CIDR,192.0.2.0/24))', 'SRC-IP-CIDR,192.0.2.0/24']


def rule_text():
    roll = R.random()
    if roll < 0.15:
        items = [R.choice(LINES) for _ in range(R.randint(0, 4))]
        return 'payload:\n' + ''.join(f"  - '{line}'\n" for line in items if line.strip())
    if roll < 0.2:
        return R.choice(['payload: 5\n', 'payload:\n', 'payload: []\n', 'payload: x\n'])
    sep = R.choice(['\n', '\r\n', ' ', '\x85'])
    text = sep.join(R.choice(LINES) for _ in range(R.randint(0, 6)))
    if R.random() < 0.1:
        text = '﻿' + text
    return text


def library_entry(i):
    if R.random() < 0.5:
        # Well-formed classical URL source, so the cache decides true/false.
        return {'id': f'lib-{i}', 'name': R.choice(['TikTok', f'list-{i}', '抖音']), 'source_type': 'url',
                'behavior': 'classical', 'format': 'text', 'url': f'https://rules.test/{i}.list'}
    entry = {'id': f'lib-{i}', 'name': R.choice(['TikTok', '抖音', 'A B', 'x/y', f'list-{i}', 'TikTok ']),
             'source_type': R.choice(['url', 'url', 'content', 'file', None]),
             'behavior': R.choice(['classical', 'classical', 'domain', 'ipcidr']),
             'url': R.choice([f'https://rules.test/{i}.list', '', None, f'https://rules.test/{i}.mrs'])}
    if R.random() < 0.7:
        entry['format'] = R.choice(['text', 'yaml', 'mrs', 'bogus'])
    if entry['source_type'] == 'content' or R.random() < 0.1:
        entry['content'] = R.choice([rule_text(), None, 5])
    return {k: v for k, v in entry.items() if v is not None or R.random() < 0.3}


def classical_case():
    from backend.common.config import get_repository
    from backend.utils import rule_utils
    library = [library_entry(i) for i in range(R.randint(0, 3))]
    stored = library if R.random() < 0.7 else [library_entry(9)]
    try:
        get_repository().update_shared_transaction(lambda shared: shared.update(rule_library=stored))
    except Exception:  # noqa: BLE001 - keep whatever is stored
        stored = None
    root = rule_utils.get_rules_dir()
    shutil.rmtree(root, ignore_errors=True)
    os.makedirs(root, exist_ok=True)
    if library and R.random() < 0.5:
        item = {'id': 'item', 'itemType': 'ruleset', 'library_rule_id': R.choice(library + [{'id': 'gone'}])['id'],
                'behavior': 'classical', 'name': R.choice(['TikTok', 'x'])}
    else:
        item = library_entry(7)
        item['itemType'] = 'ruleset'
    if R.random() < 0.05:
        item['library_rule_id'] = R.choice([None, 1])
    cfg = {'rule_library': library}
    if R.random() < 0.08:
        cfg['rule_library'] = R.choice([None, 5, '', {}, 'ab'])
    if R.random() < 0.03:
        cfg = R.choice([None, [], 'x'])
    for entry in [item] + library:
        if R.random() < 0.6:
            try:
                name = rule_utils.rule_cache_filename(entry)
            except Exception:  # noqa: BLE001
                continue
            if R.random() < 0.15:
                name = rule_utils.sanitize_rule_name(entry.get('name', '')) + '.list'
            elif R.random() < 0.1:
                identity = str(entry.get('library_rule_id') or entry.get('id'))
                name = os.path.join('by-id', hashlib.sha256(identity.encode()).hexdigest() + '.list')
            path = os.path.join(root, name)
            os.makedirs(os.path.dirname(path), exist_ok=True)
            data = (rule_text() if R.random() < 0.5 else '\n'.join(
                R.choice(LINES[:4] + LINES[6:9] + LINES[12:17]) for _ in range(R.randint(1, 5)))).encode('utf-8')
            if R.random() < 0.05:
                data += b'\xff\xfe'
            with open(path, 'wb') as fh:
                fh.write(data)
    return item, cfg


def main(count):
    from backend.common import router_split, rule_ip_presence, setup_flow
    r02_record._install()
    original_sessions = setup_flow.profile_sessions
    calls = {
        'read_marker': lambda: router_split.read_marker(config()),
        'is_full_tun': lambda: router_split.is_full_tun(config()),
        'route_rule_set_ids': lambda: router_split.route_rule_set_ids(config()),
        'tun_filters': lambda: router_split.tun_filters(tun()),
        'full_tun_document': lambda: router_split.full_tun_document(R.choice([document(), document(), {}, {'tun': None}])),
        'full_tun_base_yaml': lambda: router_split.full_tun_base_yaml(yaml_text()),
        'is_full_tun_document': lambda: router_split.is_full_tun_document(R.choice([document(), None, [], 'x'])),
        'check_generation': lambda: router_split.check_generation(config()),
        'require_full_tun_generated': lambda: router_split.require_full_tun_generated(document()),
        'legacy_context': lambda: router_split.legacy_context(FakeRepository(), 'home'),
        'needs_context': lambda: router_split.needs_context(config()),
        'base_is_full_tun': lambda: router_split.base_is_full_tun(config()),
        'legacy_source': lambda: router_split.legacy_source(config(), context()),
        'publish_legacy_source': lambda: router_split.publish_legacy_source(
            FakeRepository(), R.choice(['home', '', None]), config(), R.choice(AGENTS + [None, 1])),
        'legacy_gateway_agent_ids': lambda: router_split.legacy_gateway_agent_ids(config(), context()),
        'gateway_was_used': lambda: router_split.gateway_was_used(
            config(), R.choice([{}, {'gateway_installation_id': R.choice(['x', '', None, 1])}, None]),
            R.choice(['setup', 'topology', None, 1])),
        'mode_from_context': lambda: router_split.mode_from_context(config(), context()),
        'router_split_state': lambda: router_split.router_split_state(
            FakeRepository(), 'home', R.choice([config(), None]), R.choice([context(), None])),
    }
    names = list(calls)
    for i in range(count):
        install_sessions()
        name = names[i % len(names)]
        r02_record._TEST[0] = f'corpus::{name}::{i}'
        try:
            calls[name]()
        except Exception:  # noqa: BLE001 - recorded
            pass
    setup_flow.profile_sessions = original_sessions
    for i in range(count // 3):
        item, cfg = classical_case()
        r02_record._TEST[0] = f'corpus::classical_ip_lines::{i}'
        try:
            rule_ip_presence.classical_ip_lines(item, cfg)
        except Exception:  # noqa: BLE001 - recorded
            pass
    rows = [e for e in r02_record._RECORDS if 'unsupported' not in e]
    unsupported = {}
    for e in r02_record._RECORDS:
        if 'unsupported' in e:
            key = f"{e['fn']}: {e['unsupported']}"
            unsupported[key] = unsupported.get(key, 0) + 1
    output = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'r02_corpus.jsonl.gz')
    data = '\n'.join(json.dumps(e, ensure_ascii=False) for e in rows) + '\n'
    with open(output, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    per_fn, errors = {}, 0
    for e in rows:
        per_fn[e['fn']] = per_fn.get(e['fn'], 0) + 1
        errors += 'err' in e
    print(f'{len(rows)} rows ({errors} raising) -> {output}')
    print('per function:', json.dumps(per_fn))
    print('unsupported (not written):', json.dumps(unsupported, ensure_ascii=False))
    shutil.rmtree(_DATA, ignore_errors=True)


if __name__ == '__main__':
    main(int(sys.argv[1]) if len(sys.argv) > 1 else 4000)
