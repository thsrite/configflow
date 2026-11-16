"""Hand-picked edge inputs for routers R02 (router_split, rule_ip_presence),
recorded by real Python through the r02_record plugin's dispatcher, in the
row format of r02_recorded.jsonl.gz (review R02; replayed by TestEdgeCorpus
in routersplit and ippresence).

    rt --back server/internal/routersplit/testdata/r02_edge.jsonl.gz -- bash -lc \
        'PYTHONPATH=.:server/internal/routersplit/testdata ~/cf-test/.venv/bin/python server/internal/routersplit/testdata/gen_r02_edge.py'

Synthetic values only (*.test hosts, documentation addresses).
"""
import gzip
import json
import os
import shutil
import sys
import tempfile

_DATA = tempfile.mkdtemp(prefix='r02-edge-')
os.environ['DATA_DIR'] = _DATA
os.environ['LOG_FILE'] = os.path.join(_DATA, 'app.log')

import r02_record  # noqa: E402

M = {'version': 1, 'takeover': 'full_tun', 'route_rule_set_ids': ['a'], 'origin': 'setup'}
FULL_TUN = 'tun: {enable: true, auto-route: true, auto-redirect: true, dns-hijack: [any:53]}\n'


def marker(**changes):
    m = dict(M)
    m.update(changes)
    return m


def cfg(marker_value=None, gateway=None, custom=None, routers=None, **extra):
    mihomo = {}
    if marker_value is not None:
        mihomo['router_split'] = marker_value
    if gateway is not None:
        mihomo['linux_gateway'] = gateway
    if custom is not None:
        mihomo['custom_config'] = custom
    mihomo.update(extra)
    c = {'mihomo': mihomo}
    if routers is not None:
        c['routers'] = routers
    return c


MARKERS = [
    marker(version=True), marker(version=1.0), marker(version='1'), marker(version='１'), marker(version=10 ** 20),
    marker(version=-1), marker(version=0), marker(takeover='full_tun '), marker(origin='Setup'),
    marker(route_rule_set_ids=['😀' * 256]), marker(route_rule_set_ids=['😀' * 257]),
    marker(route_rule_set_ids=['规' * 256, '则']), marker(route_rule_set_ids=['a\x1f']),
    marker(route_rule_set_ids=['a\x7f']), marker(route_rule_set_ids=['a\x80']), marker(route_rule_set_ids=['a\x85']),
    marker(route_rule_set_ids=['a\u2028']), marker(route_rule_set_ids=['\u3000']), marker(route_rule_set_ids=['１２３']),
    marker(route_rule_set_ids=['b', 'a', 'b', 'a', 'c']), marker(route_rule_set_ids=[f'r{i}' for i in range(1024)]),
    marker(route_rule_set_ids=[f'r{i}' for i in range(1025)]), marker(route_rule_set_ids=[]),
    marker(route_rule_set_ids=['']), marker(route_rule_set_ids=[' ']), marker(route_rule_set_ids=['\x00']),
    marker(route_rule_set_ids={'a': 1}), marker(route_rule_set_ids='a'),
    {**M, 'extra': None}, {k: v for k, v in M.items() if k != 'origin'}, None, [], 'full_tun', 1, {},
]

YAML_TEXTS = [
    '\ufeffmode: rule\n', '\u3000', '\x85', '\u2028\n', '\x00', 'a: "\\x00"\n', 'a: \x07\n',
    'a: &x []\nb: *x\n' + FULL_TUN, 'a: &x {}\nb: *x\n', 'a: &t 2001-01-01\nb: *t\n',
    'tun: &t\n  enable: false\n  stack: gvisor\nother: *t\n', 'tun: ~\nmode: rule\n', 'mode: rule\ntun: &n ~\nx: *n\n',
    'tun: &h {dns-hijack: &d [udp://any:53]}\nhijack: *d\n',
    'd: 2026-13-45\n', 'd: 2026-02-30\n', 'n: 123456789012345678901234567890\n', 'x: 1e400\ny: .nan\nz: -.inf\nw: .inf\n',
    'a: 0o17\nb: 0b101\nc: 1_000\nd: +12\ne: 0x_1F\nf: 1:30\ng: 1.5e3\nh: 017\ni: 1_0.5\n',
    '😀: 规则\n"\\U0001F600": x\nk: "\\u2028"\n', 'long: ' + 'x' * 200 + '\n', 'long: ' + 'word ' * 60 + '\n',
    '---\na: 1\n---\nb: 2\n', 'a: 1\n...\n', '--- \nmode: rule\n',
    'a: &a {x: 1}\nb: &b {y: 2}\nc: {<<: [*a, *b], z: 3}\n' + FULL_TUN,
    'tun: {include-interface: [], exclude-interface: [eth0]}\n', 'tun: {include-uid: 0, exclude-uid: [0]}\n',
    'tun: {include-package: "", exclude-package: " "}\n', 'tun: {route-address: [], stack: ~}\n',
    'tun: {stack: "", enable: false, dns-hijack: []}\n', '? [a, b]\n: c\n', '? {a: 1}\n: c\n',
    '1.0: x\nnull: y\ntrue: z\n' + FULL_TUN, '0: a\n1: b\nyes: c\n',
    'a: !!str 5\nb: !!int "5"\nc: !!float 5\n', 'b: !!binary aGVsbG8=\n',
    'x: !!timestamp 2001-12-14t21:59:43.10-05:00\ny: 2001-12-14 21:59:43.10\nz: 2001-12-14\n',
    'a: !!python/tuple [1]\n', 'a: !custom 1\n', 'a: b: c\n', '[1, 2]\n', '"just text"\n', '~\n', '# 注释\n',
    'deep: ' + '[' * 300 + ']' * 300 + '\n', 'deep: ' + '[' * 600 + ']' * 600 + '\n',
    'tun:\n  enable: true\n  auto-route: true\n  auto-redirect: true\n  dns-hijack: [any:53]\n  device: 😀\n',
    'tun: [enable]\n', 'tun: 5\n', 'tun: ""\n', 'mode: "\\t規則"\nname: "a\\nb"\n', 'k: "  lead"\nm: "trail  "\n',
    'k: \'single\'\'quote\'\n', 'k: |\n  line1\n  line2\n', 'k: >\n  folded\n  text\n',
    'x-configflow-linux-gateway: {enabled: true}\n' + FULL_TUN, 'a: !!null ""\n', 'a: !!bool "yes"\n',
]

ROUTERS = [
    {'items': [{'id': 'r1', 'proxy_gateway_agent_id': 1}, {'id': 'r2', 'proxy_gateway_agent_id': True},
               {'id': 'r3', 'proxy_gateway_agent_id': 1.0}]},
    {'items': [{'id': 'r1', 'proxy_gateway_agent_id': '规则'}, {'id': None, 'proxy_gateway_agent_id': '😀'}]},
    {'items': [{'id': 'r1', 'proxy_gateway_agent_id': ['m']}]}, {'items': [{'id': 'r1', 'proxy_gateway_agent_id': None}]},
    {'items': 0}, {'items': ''}, {'items': 'ab'}, {'items': {'r': {}}}, [], 'x', 0,
]

CONTEXTS = [
    {'mihomo_agent_ids': [1], 'session_goal': None}, {'mihomo_agent_ids': [True]}, {'mihomo_agent_ids': [1.0]},
    {'mihomo_agent_ids': ['规则', '😀']}, {'mihomo_agent_ids': [None]}, {'mihomo_agent_ids': [['m']]},
    {'mihomo_agent_ids': [{'a': 1}]}, {'mihomo_agent_ids': '😀规'}, {'mihomo_agent_ids': {'规则': 1}},
    {'mihomo_agent_ids': 1.5}, {'mihomo_agent_ids': True}, {'mihomo_agent_ids': 0}, {'mihomo_agent_ids': ''},
    {'session_goal': 'router', 'mihomo_agent_ids': 5}, {'session_goal': 'Router'}, {'has_gateway_session': 0.0},
    {'has_gateway_session': []}, {'has_gateway_session': [0]}, {'session_id': {'a': 1}}, {}, None, [], 'x',
]


def router_split_cases():
    from backend.common import router_split as rs
    from backend.common import setup_flow
    cases = []
    for m in MARKERS:
        for c in (cfg(m), cfg(m, gateway={'enabled': True}), cfg(m, custom=FULL_TUN)):
            cases.append(('read_marker', lambda c=c: rs.read_marker(c)))
            cases.append(('route_rule_set_ids', lambda c=c: rs.route_rule_set_ids(c)))
            cases.append(('check_generation', lambda c=c: rs.check_generation(c)))
            cases.append(('mode_from_context', lambda c=c: rs.mode_from_context(c, {})))
    for c in ({'mihomo': None}, {'mihomo': []}, {'mihomo': 'router_split'}, {'mihomo': {'router_split': None}},
              None, [], 'x', 5, {}, {'mihomo': {'linux_gateway': {'enabled': 1}}, 'routers': None},
              {'mihomo': {'linux_gateway': []}}, {'mihomo': {'linux_gateway': 'x'}}, {'mihomo': 0},
              {'mihomo': {'linux_gateway': {'enabled': True}}}):
        for name in ('read_marker', 'check_generation', 'needs_context', 'base_is_full_tun'):
            cases.append((name, lambda c=c, name=name: getattr(rs, name)(c)))
        cases.append(('mode_from_context', lambda c=c: rs.mode_from_context(c, {'session_goal': 'router'})))
    for value in (0, '', [], {}, [0], ' ', '0', False, True, None, 0.0, [None], {'a': None}):
        for key in ('include-interface', 'exclude-package', 'include-android-user'):
            tun = {'enable': True, key: value}
            cases.append(('tun_filters', lambda tun=tun: rs.tun_filters(tun)))
            cases.append(('full_tun_document', lambda tun=tun: rs.full_tun_document({'tun': tun})))
    for doc in ({'tun': None, 'mode': 'rule'}, {'mode': 'rule'}, {'tun': {'stack': None}}, {'tun': {'stack': ''}},
                {'tun': {'dns-hijack': [], 'enable': False, 'route-address': ['192.0.2.0/24'], 'device': 'utun'}},
                {'tun': {'auto-route': 'yes', 'mtu': 9000, 'route-exclude-address-set': []}}, {'tun': []},
                {'tun': 'on'}, {'tun': 0}, {'tun': False}, {'tun': {}}, 'x', [], None, 5, {},
                {'x-configflow-linux-gateway': {}, 'tun': {}}, {'tun': {'规则': '😀'}}):
        cases.append(('full_tun_document', lambda doc=doc: rs.full_tun_document(doc)))
        cases.append(('is_full_tun_document', lambda doc=doc: rs.is_full_tun_document(doc)))
        cases.append(('require_full_tun_generated', lambda doc=doc: rs.require_full_tun_generated(doc)))
    for tun in ({'enable': True, 'auto-route': True, 'auto-redirect': True, 'dns-hijack': ['any:53'],
                 'include-uid': [0], 'exclude-package': ['x'], 'include-interface': ['eth0']},
                {'enable': 1, 'auto-route': True, 'auto-redirect': True, 'dns-hijack': ['any:53']},
                {'enable': True, 'auto-route': True, 'auto-redirect': True, 'dns-hijack': 'any:53'},
                {'enable': True, 'auto-route': True, 'auto-redirect': True, 'dns-hijack': ['ANY:53', 'any:53 ']},
                {'enable': True, 'auto-route': True, 'auto-redirect': True, 'dns-hijack': ['any:53'],
                 'inet6-route-address': None}):
        cases.append(('require_full_tun_generated', lambda tun=tun: rs.require_full_tun_generated({'tun': tun})))
        cases.append(('is_full_tun_document', lambda tun=tun: rs.is_full_tun_document({'tun': tun})))
    for text in YAML_TEXTS + [None, 1, 1.5, True, ['a'], {'a': 1}, b'mode: rule' and 'mode: rule']:
        cases.append(('full_tun_base_yaml', lambda text=text: rs.full_tun_base_yaml(text)))
        c = cfg(None, gateway={'enabled': True}, custom=text)
        cases.append(('base_is_full_tun', lambda c=c: rs.base_is_full_tun(c)))
        cases.append(('mode_from_context', lambda c=c: rs.mode_from_context(c, {'session_goal': 'router'})))
        cm = cfg(M, custom=text)
        cases.append(('router_split_state', lambda cm=cm: rs.router_split_state(None, 'home', cm, {})))
    # Base YAML jsonx cannot hold: only the base-document readers (full_tun_base_yaml
    # cannot write these in Go; documented limit).
    for text in ('a: &a [1, *a]\n' + FULL_TUN, 'tun: &t {enable: true, auto-route: true, auto-redirect: true, '
                 'dns-hijack: &d [any:53, *d], self: *t}\n', 's: !!set {a, b}\n' + FULL_TUN,
                 'tun: {enable: true, auto-route: true, auto-redirect: true, dns-hijack: [any:53], '
                 'include-interface: !!set {eth0}}\n', 'tun: !!set {enable}\n', '!!set {a}\n',
                 'o: !!omap [{a: 1}]\n' + FULL_TUN, 'p: !!pairs [{a: 1}]\n', 'tun: !!omap [{enable: true}]\n',
                 'tun: {enable: true, auto-route: true, auto-redirect: true, dns-hijack: !!set {any:53}}\n'):
        c = cfg(None, gateway={'enabled': True}, custom=text)
        cases.append(('base_is_full_tun', lambda c=c: rs.base_is_full_tun(c)))
        cases.append(('mode_from_context', lambda c=c: rs.mode_from_context(c, {'session_goal': 'router'})))
        cm = cfg(M, custom=text)
        cases.append(('router_split_state', lambda cm=cm: rs.router_split_state(None, 'home', cm, {})))
    for custom in (0, 0.0, False, '', [], {}, 1, ['tun'], {'tun': 1}, True):
        c = cfg(M, custom=custom)
        cases.append(('router_split_state', lambda c=c: rs.router_split_state(None, 'home', c, {})))
        cases.append(('base_is_full_tun', lambda c=c: rs.base_is_full_tun(c)))
    for routers in ROUTERS:
        for context in CONTEXTS:
            c = cfg(None, gateway={'enabled': True}, routers=routers)
            cases.append(('legacy_source', lambda c=c, x=context: rs.legacy_source(c, x)))
            cases.append(('legacy_gateway_agent_ids', lambda c=c, x=context: rs.legacy_gateway_agent_ids(c, x)))
            cases.append(('mode_from_context', lambda c=c, x=context: rs.mode_from_context(c, x)))
    for mihomo in ({'linux_gateway_restore_point': None}, 'x linux_gateway_restore_point y', ['linux_gateway_restore_point'],
                   ['x'], 'x', 0, 1, 1.5, True, False, None, [], {}, {'规则': 1}):
        for agent in ({'gateway_installation_id': '规'}, {'gateway_installation_id': 0}, {}, None, [], 'x'):
            for source in ('setup', 'topology', None, 1, 'Setup'):
                c = {'mihomo': mihomo}
                cases.append(('gateway_was_used', lambda c=c, a=agent, s=source: rs.gateway_was_used(c, a, s)))
    for c in (None, [], 'x'):
        cases.append(('gateway_was_used', lambda c=c: rs.gateway_was_used(c, {}, None)))
        cases.append(('gateway_was_used', lambda c=c: rs.gateway_was_used(c, {}, 'setup')))

    class Repo:
        def __init__(self, bindings, profile=None):
            self.bindings, self.profile = bindings, profile

        def get_profile_agent_bindings(self, profile_id):
            return json.loads(json.dumps(self.bindings))

        def get_profile(self, profile_id):
            return json.loads(json.dumps(self.profile))

    session_sets = [
        [], [{}], [{'abandoned': True, 'goal': 'router', 'id': 'x'}, {'goal': 'gateway', 'id': 'y', 'completed': 1}],
        [{'abandoned': 0, 'goal': 'router', 'completed': '', 'id': '规'}], [{'abandoned': [], 'goal': None}],
        [{'abandoned': 'x', 'goal': 'router'}, {'goal': 'gateway'}], [{'goal': 'router', 'completed': [0]}],
        [{'goal': 'other'}, {'goal': 'gateway', 'abandoned': None}],
    ]
    binding_sets = [
        {'agents': []}, {'agents': [{'id': 'm', 'service_type': 'mihomo'}, {'id': 'o', 'service_type': 'oxidns'}]},
        {'agents': [{'service_type': 'mihomo'}]}, {'agents': [{'id': 1, 'service_type': 'mihomo'}, 'x']},
        {'agents': ['x']}, {'agents': 'ab'}, {'agents': None}, {'agents': {'a': 1}}, {}, {'agents': [{'id': 'm'}]},
    ]
    for sessions in session_sets:
        for bindings in binding_sets:
            def run(sessions=sessions, bindings=bindings):
                setup_flow.profile_sessions = lambda repository, profile_id: json.loads(json.dumps(sessions))
                return rs.legacy_context(Repo(bindings), 'home')
            cases.append(('legacy_context', run))

        for profile_id in ('home', '', None):
            for config in (cfg(None, gateway={'enabled': True}, routers=ROUTERS[0]), cfg(M), None, 'x'):
                def run(sessions=sessions, profile_id=profile_id, config=config):
                    setup_flow.profile_sessions = lambda repository, pid: json.loads(json.dumps(sessions))
                    return rs.publish_legacy_source(Repo({}), profile_id, config, 1)
                cases.append(('publish_legacy_source', run))

        for config in (None, cfg(None, gateway={'enabled': True}, routers=ROUTERS[1])):
            def run(sessions=sessions, config=config):
                setup_flow.profile_sessions = lambda repository, pid: json.loads(json.dumps(sessions))
                profile = cfg(None, gateway={'enabled': True}, routers=ROUTERS[1])
                return rs.router_split_state(Repo(binding_sets[1], profile), 'home', config, None)
            cases.append(('router_split_state', run))
    return cases


# ---- classical_ip_lines ----

def entry(i, **changes):
    e = {'id': f'edge-{i}', 'name': f'Edge {i}', 'source_type': 'url', 'behavior': 'classical', 'format': 'text',
         'url': f'https://rules.test/edge-{i}.list'}
    e.update(changes)
    return {k: v for k, v in e.items() if v is not ...}


def classical_cases():
    from backend.common import rule_ip_presence
    from backend.common.config import get_repository
    from backend.utils import rule_utils
    default_library = get_repository().get_shared()['rule_library']

    def setup(stored, files):
        get_repository().update_shared_transaction(
            lambda shared: shared.update(rule_library=json.loads(json.dumps(default_library)) + stored))
        root = rule_utils.get_rules_dir()
        shutil.rmtree(root, ignore_errors=True)
        os.makedirs(root, exist_ok=True)
        for name, data in files.items():
            path = os.path.join(root, name)
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, 'wb') as fh:
                fh.write(data if isinstance(data, bytes) else data.encode())

    def legacy(e):
        return rule_utils.sanitize_rule_name(e['name']) + '.list'

    def source(e):
        return rule_utils.rule_cache_filename(e)

    cases = []

    def add(item, config, stored=(), files=None):
        def run():
            setup(list(stored), files or {})
            return rule_ip_presence.classical_ip_lines(item, config)
        cases.append(('classical_ip_lines', run))

    e0 = entry(0)
    ref = {'id': 'item', 'itemType': 'ruleset', 'library_rule_id': 'edge-0', 'behavior': 'classical', 'name': 'x'}
    # Legacy-name path: the stored declaration matches the config's, no new or by-id cache.
    # (Library entries that are not dicts or have a non-string name are left
    # out: the repository refuses them — 共享规则仓库名称必须非空且唯一 / 每一项必须有 ID —
    # so no stored or composed config holds them; Python raises on them here.)
    for library in ([e0], [e0, entry(1, name='Edge 0')], [e0, entry(1, name='Edge/0')],
                    [e0, entry(1, name=' Edge 0. ')], [e0, entry(1, name='edge 0')], [e0, entry(1, name='Edge 0 ')]):
        for content in ('192.0.2.0/24\n', 'DOMAIN,a.test\n'):
            add(ref, {'rule_library': library}, [e0], {legacy(e0): content})
            add(e0, {'rule_library': library}, [e0], {legacy(e0): content})
    # (A declaration with a non-string name that also matches a stored entry
    # by id is left out for the same reason: only library entries share ids
    # with the stored library, and their names are validated strings.)
    add(entry(4, name=['x']), {'rule_library': []}, [entry(4)], {})
    for name in ('规则/抖音', '😀', 'a' * 250, ' . ', 'TikTok ', '１２３'):
        e = entry(5, name=name)
        add(e, {'rule_library': [e]}, [e], {legacy(e): '2001:db8::/32\n'})
        add(e, {'rule_library': [e, entry(6, name=name + 'x')]}, [e], {legacy(e): '2001:db8::/32\n'})
    # by-id cache of the stored declaration
    for identity in ('edge-7', 7, True):
        e = entry(7, id=identity)
        import hashlib
        by_id = os.path.join('by-id', hashlib.sha256(str(identity).encode()).hexdigest() + '.list')
        add(e, {'rule_library': [e]}, [entry(7)] if identity != 'edge-7' else [e], {by_id: 'IP-CIDR,192.0.2.0/24\n'})
    # library_rule_id shapes
    lib = [entry(8, id=0), entry(9, id=1), entry(10, id='')]
    for library_id in (0, False, 1, True, 1.0, '', None, [1], {'a': 1}, '1', 'edge-x'):
        item = {'itemType': 'ruleset', 'library_rule_id': library_id, 'behavior': 'classical', 'name': 'Edge 9',
                'source_type': 'content', 'content': '198.51.100.0/24'}
        add(item, {'rule_library': lib}, [], {})
        add(dict(item, source_type='url', url='https://rules.test/x.list'), {'rule_library': lib}, [],
            {source(lib[1]): '192.0.2.1\n', source(lib[0]): 'DOMAIN,a.test\n'})
    # url and source_type shapes
    for url in (5, True, ['x'], {'a': 1}, 1.5, ' ', 'https://rules.test/a.MRS', 'https://rules.test/a.yaml?x=.mrs',
                'rules.test/a.mrs#frag', '规则.mrs'):
        e = entry(11, url=url, format=...)
        add(e, {'rule_library': [e]}, [], {})
        add(entry(11, url=url), {'rule_library': [e]}, [], {})
    for st in (None, 'URL', 'content ', 'file', 1, ...):
        add(entry(12, source_type=st, content='192.0.2.1'), {'rule_library': []}, [], {})
    # contents
    for content in ('１９２.０.２.１', 'IP-CIDR,１９２.０.２.０/２４', '规则.test\n192.0.2.1', '😀\n2001:db8::1',
                    'a.test\u2028192.0.2.1', 'a.test\u2029192.0.2.1', 'a.test\x0c192.0.2.1', 'a.test\x1c192.0.2.1',
                    'a.test\x1e192.0.2.1', 'a.test\x0b192.0.2.1', '\ufeff\ufeff192.0.2.1', '\ufeffpayload:\n  - 192.0.2.1\n',
                    'payload:\n  - &a 192.0.2.1\n  - *a\n', 'payload: !!set {a}\n', 'payload: [1, 2]\n', 'payload: {a: 1}\n',
                    'payload:\n  - IP-CIDR,192.0.2.0/24\n  - 1\n', '{payload: [192.0.2.1]}', '  {a', 'payload: [',
                    '# only\n#192.0.2.1', ' #x\n\t192.0.2.1', '\u3000192.0.2.1', '\x85192.0.2.1', 'GEOIP,CN,no-resolve',
                    'IP-ASN,13335', 'SRC-IP-CIDR,192.0.2.0/24', 'IP-CIDR6,2001:db8::/32,no-resolve', '0.0.0.0/0',
                    '::/0', '192.0.2.1/33', '300.0.0.1', 'IP-CIDR,2001:db8::/32', 'DOMAIN,192.0.2.1', ''):
        add({'source_type': 'content', 'content': content, 'behavior': 'classical'}, None)
        e = entry(13, format='text')
        add(e, {'rule_library': [e]}, [], {source(e): content.encode('utf-8')})
    for raw in (b'\xef\xbb\xbf192.0.2.1', b'\xef\xbb\xbf\xef\xbb\xbf192.0.2.1', b'\xed\xa0\x80', b'\xc0\x80',
                b'\xf4\x90\x80\x80', b'192.0.2.1\xff', b'\x00192.0.2.1', b'\xe2\x80\xa8192.0.2.1'):
        e = entry(14)
        add(e, {'rule_library': [e]}, [], {source(e): raw})
    # config / item shapes
    for config in (None, [], 'x', 5, {'rule_library': None}, {'rule_library': 0}, {'rule_library': ''},
                   {'rule_library': {}}, {'rule_library': 'ab'}, {'rule_library': {'a': 1}}, {'rule_library': ['x']},
                   {'rule_library': [None, entry(15)]}, {'rule_library': [entry(15), None]}, {}):
        add(entry(15), config, [], {source(entry(15)): '192.0.2.1'})
        add(dict(ref, library_rule_id='edge-15'), config, [], {source(entry(15)): '192.0.2.1'})
    for item in (None, [], 'x', 5, {}):
        add(item, {'rule_library': []})
    return cases


def main():
    from backend.common import setup_flow
    r02_record._install()
    original_sessions = setup_flow.profile_sessions
    cases = router_split_cases()
    setup_flow.profile_sessions = original_sessions
    cases += classical_cases()
    for i, (name, run) in enumerate(cases):
        r02_record._TEST[0] = f'edge::{name}::{i}'
        try:
            run()
        except Exception:  # noqa: BLE001 - recorded
            pass
        finally:
            setup_flow.profile_sessions = original_sessions
    rows = [e for e in r02_record._RECORDS if 'unsupported' not in e]
    unsupported = {}
    for e in r02_record._RECORDS:
        if 'unsupported' in e:
            key = f"{e['fn']}: {e['unsupported']}"
            unsupported[key] = unsupported.get(key, 0) + 1
    output = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'r02_edge.jsonl.gz')
    data = '\n'.join(json.dumps(e, ensure_ascii=False) for e in rows) + '\n'
    with open(output, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    per_fn, errors = {}, 0
    for e in rows:
        per_fn[e['fn']] = per_fn.get(e['fn'], 0) + 1
        errors += 'err' in e
    print(f'{len(rows)} rows ({errors} raising) of {len(cases)} cases -> {output}')
    print('per function:', json.dumps(per_fn))
    print('unsupported (not written):', json.dumps(unsupported, ensure_ascii=False))
    shutil.rmtree(_DATA, ignore_errors=True)


if __name__ == '__main__':
    main()
