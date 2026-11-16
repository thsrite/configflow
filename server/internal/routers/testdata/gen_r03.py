"""R03 reference corpus from the real Python entry points (run on the test server).

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/routers/testdata/gen_r03.py \
        server/internal/routers/testdata/r03_corpus.jsonl.gz

Rows {"fn", "in", "out"}. Synthetic values only (documentation/private ranges,
made-up names). Entry points: build_report, assess_nat, assess_bypass,
planned_bypass_conflicts, preserves_sources, inspect_rule, the iKuai group
readback, a GroupPlanner script (as plan.py drives it), verify, payload/
group_bytes, span, canon, digest, _networks.
"""
import copy
import gzip
import inspect
import ipaddress
import json
import random
import sys

from backend.routers import ikuai_groups as ig
from backend.routers.base import NatBypassEntry, PolicyRouteEntry, RouteEntry, RouterError, RouterState
from backend.routers.report import _networks, build_report
from backend.routers.routeros_nat import (
    assess_bypass, assess_nat, client_source_networks, planned_bypass_conflicts, preserves_sources,
)
from backend.routers.routeros_nat_list import inspect_rule

rng = random.Random(3)
rows = []

# Python bug fixed in Go (intended difference, see PROGRESS R03): when layout
# repacks a fragmented family, new CIDRs the first pass already put into an
# existing group were queued again. FIXED_LAYOUT is ikuai_groups.layout with
# exactly that line changed; ikuai_plan rows whose result differs under it
# carry the fixed result as "out_fixed" (Go must match it, not "out").
_LAYOUT_SRC = inspect.getsource(ig.layout)
_BUGGY = "sequence = [value for slot in slots for value in slot['values']] + pending\n"
assert _LAYOUT_SRC.count(_BUGGY) == 1
_fixed_ns = dict(vars(ig))
exec(_LAYOUT_SRC.replace(_BUGGY, "sequence = [value for slot in slots for value in slot['values']] + pending[position:]\n"),
     _fixed_ns)
FIXED_LAYOUT = _fixed_ns['layout']


def emit(fn, value, out, out_fixed=None):
    row = {'fn': fn, 'in': value, 'out': out}
    if out_fixed is not None:
        row['out_fixed'] = out_fixed
    rows.append(row)


def plan_rows(groups, nat, policy, steps):
    """run_planner with the real layout and with FIXED_LAYOUT (None when equal)."""
    out = run_planner(gstate_of(groups, nat, policy), steps)
    real = ig.layout
    ig.layout = FIXED_LAYOUT
    try:
        fixed = run_planner(gstate_of(groups, nat, policy), steps)
    finally:
        ig.layout = real
    same = json.dumps(out, ensure_ascii=False, default=str) == json.dumps(fixed, ensure_ascii=False, default=str)
    return out, None if same else fixed


def pick(*options):
    return rng.choice(options)


def maybe(p=0.5):
    return rng.random() < p


# ---- RouterOS evidence ------------------------------------------------------------------------
LAN, WAN = ['bridge', 'lan4', 'ether2', 'vlan10'], ['pppoe-out1', 'wan', 'ether1', 'lte1']
GATEWAYS = ['192.168.0.3', '192.168.88.2', '10.10.0.254', '172.16.5.1', '192.168.0.1', '192.168.0.254', '192.168.1.0']
CIDRS = ['198.18.0.0/16', '203.0.113.0/24', '198.51.100.0/24', '192.0.2.0/24', '198.18.0.0/15', '203.0.113.128/25',
         '100.64.0.0/10', '8.8.8.8/32', '1.1.1.0/24', '149.154.160.0/20', '91.108.4.0/22']
FLAG = ['true', 'false', 'yes', 'no', True, False, '', None, 'TRUE', '1', '0']


def flags(row, keys, p=0.3):
    for key in keys:
        if maybe(p):
            row[key] = pick(*FLAG)
    return row


def route_row(i, gateway, cidr=None):
    cidr = cidr or pick(*CIDRS, '0.0.0.0/0', gateway.rsplit('.', 1)[0] + '.0/24', 'bogus', '', '2001:db8::/32',
                        gateway + '/24', '192.168.0.0/16')
    row = {'.id': f'*{i:X}', 'dst-address': cidr}
    kind = rng.random()
    if kind < 0.4:
        row.update({'gateway': pick(gateway, gateway, '192.168.0.9', 'pppoe-out1'),
                    'immediate-gw': pick(f'{gateway}%{pick(*LAN)}', f'{gateway}%{pick(*WAN)}', gateway, '',
                                         f'x%{pick(*LAN)}', f'{gateway}%a,b', f'{gateway}%{pick(*LAN)}%z')})
    elif kind < 0.7:
        iface = pick(*LAN)
        row.update({'gateway': pick(iface, iface, '192.168.0.1', 'x,y'), 'connect': pick('true', 'true', 'false'),
                    'immediate-gw': pick(iface, '', pick(*LAN))})
        if not row['immediate-gw']:
            del row['immediate-gw']
    else:
        row.update({'gateway': pick('pppoe-out1', '100.64.0.1', gateway)})
    if maybe(0.15):
        row['routing-table'] = pick('main', 'backup', '', None)
    if maybe(0.4):
        row['distance'] = pick('1', '2', '0', 'x', ' 3 ', 1, 5, None)
    if maybe(0.85):
        row['active'] = pick('true', 'true', 'false', True)
    return flags(row, ['inactive', 'invalid', 'disabled', 'blackhole', 'ecmp', 'dynamic'], 0.08)


def nat_row(i):
    row = {'.id': f'*{100 + i:X}', 'chain': pick('srcnat', 'srcnat', 'srcnat', 'srcnat', 'srcnat', 'dstnat', 'custom',
                                                 '' if maybe(0.3) else 'srcnat', None if maybe(0.3) else 'srcnat'),
           'action': pick('masquerade', 'masquerade', 'src-nat', 'accept', 'accept', 'log', 'passthrough', 'return',
                          'jump', 'netmap', 'same', 'dst-nat', 'weird', None)}
    if row['chain'] is None and maybe(0.5):
        del row['chain']
    if row['action'] is None and maybe(0.5):
        del row['action']
    if maybe(0.5):
        row['out-interface'] = pick(*WAN, *LAN, '!' + pick(*WAN, *LAN), 'all', '!all', 'all-ethernet', '!all-ppp',
                                    '', '!', '!!x', 'a,b')
    if maybe(0.3):
        row['out-interface-list'] = pick('WAN', 'LAN', '!WAN', '!LAN', 'unknown-list', 'all', 'none', 'static',
                                         '!dynamic', 'INC', 'ALLM')
    if maybe(0.35):
        row['src-address'] = pick('192.168.0.0/24', '192.168.0.0/25', '192.168.0.128/26', '10.10.0.0/16',
                                  '0.0.0.0/0', 'bad', '192.168.88.0/24', '172.16.5.0/24')
    if maybe(0.35):
        row['dst-address'] = pick(*CIDRS, 'bad', '0.0.0.0/0')
    if maybe(0.25):
        row['src-address-list'] = pick('lan', 'clients', '!lan', 'missing', 'cf-mihomo-nat-src', 'broken')
    if maybe(0.25):
        row['dst-address-list'] = pick('proxy', 'fake', '!proxy', 'missing', 'cf-mihomo-nat-dst', 'broken')
    if maybe(0.3):
        row['comment'] = pick('cf-mihomo', 'cf-tg', 'manual', 'mihomo-nat', 'cf-mihomo-nat', '', 'CF', 'cf:x')
    if maybe(0.1):
        row[pick('protocol', 'connection-state', 'to-addresses', 'log-prefix')] = pick('tcp', 'new', '1.2.3.4', 'x')
    if maybe(0.2):
        row['bytes'], row['packets'] = '1234', '10'
    return flags(row, ['disabled', 'dynamic', 'invalid', 'log'], 0.12)


def mangle_rows():
    if maybe(0.15):
        return pick(None, [None][:0] or None)
    out = []
    for i in range(rng.randint(0, 3)):
        row = {'.id': f'*M{i}', 'chain': pick('prerouting', 'forward', 'custom', '', None),
               'action': pick('mark-connection', 'accept', 'change-mss', 'jump', 'mark-routing', 'route', 'weird', '',
                              None, 'fasttrack-connection')}
        if row['action'] == 'jump':
            row['jump-target'] = pick('custom', 'nowhere', None)
        out.append(flags(row, ['disabled'], 0.3))
    return out


def realistic_context(gateway, cidr):
    """A plausible RouterOS: proxy route via the gateway, connected LAN, WAN masquerade, maybe exemptions."""
    lan = pick(*LAN)
    lan_net = str(ipaddress.ip_network(f'{gateway}/24', strict=False)) if gateway not in ('', 'bad') else '192.168.0.0/24'
    routes = [{'.id': '*2', 'dst-address': lan_net, 'gateway': lan, 'immediate-gw': lan, 'active': 'true',
               'connect': 'true', 'dynamic': 'true', 'disabled': 'false'},
              {'.id': '*3', 'dst-address': '0.0.0.0/0', 'gateway': 'pppoe-out1', 'immediate-gw': 'pppoe-out1',
               'active': 'true', 'distance': '1'}]
    if maybe(0.7) and cidr:
        routes.insert(0, {'.id': '*1', 'dst-address': cidr, 'gateway': gateway,
                          'immediate-gw': f'{gateway}%{pick(lan, lan, lan, "wan")}', 'routing-table': 'main',
                          'active': pick('true', 'true', 'true', 'false'), 'disabled': 'false', 'distance': '1',
                          'comment': pick('cf-fake-ip', 'manual')})
    if maybe(0.15):
        routes.append(route_row(9, gateway, pick(cidr, '198.18.0.0/17') if cidr else None))
    rules = []
    for i in range(rng.randint(0, 3)):
        kind = rng.random()
        if kind < 0.45:
            rule = {'.id': f'*E{i}', 'chain': 'srcnat', 'action': 'accept',
                    'comment': pick('cf-mihomo', 'manual', 'cf-tg', ''), 'disabled': pick('false', 'false', 'true')}
            if maybe(0.7):
                rule['src-address'] = pick(lan_net, lan_net, '192.168.0.0/25', '0.0.0.0/0')
            elif maybe(0.5):
                rule['src-address-list'] = pick('lan', 'clients', 'cf-mihomo-nat-src')
            if maybe(0.8):
                rule['dst-address'] = pick(cidr or '198.18.0.0/16', cidr or '198.18.0.0/16', '198.18.0.0/15', '203.0.113.0/24')
            elif maybe(0.5):
                rule['dst-address-list'] = pick('proxy', 'fake', 'cf-mihomo-nat-dst')
            if maybe(0.2):
                rule['out-interface'] = pick(lan, 'wan', '!pppoe-out1')
            if maybe(0.1):
                rule['protocol'] = 'tcp'
            rules.append(rule)
        elif kind < 0.6:
            rules.append({'.id': f'*L{i}', 'chain': 'srcnat', 'action': 'accept', 'comment': 'cf-mihomo-nat',
                          'src-address-list': 'cf-mihomo-nat-src', 'dst-address-list': 'cf-mihomo-nat-dst',
                          **({'out-interface': lan} if maybe(0.3) else {})})
        else:
            rules.append(nat_row(i))
    rules.append({'.id': '*W', 'chain': 'srcnat', 'action': 'masquerade',
                  **pick({'out-interface': pick('pppoe-out1', 'wan', lan, '!' + lan)},
                         {'out-interface-list': pick('WAN', 'LAN', '!WAN', '!LAN', 'unknown-list')}, {}),
                  'disabled': pick('false', 'false', 'false', 'true')})
    return {'routes': routes, 'rules': rules, 'routing_rules': pick([], [], [], [{'disabled': 'true'}]),
            'mangle': pick([], [], [{'chain': 'forward', 'action': 'change-mss'}],
                           [{'chain': 'prerouting', 'action': 'jump', 'jump-target': 'c'}, {'chain': 'c', 'action': 'accept'}]),
            'vrfs': [], 'interface_lists': [{'name': 'WAN'}, {'name': 'LAN'}],
            'interface_members': [{'list': 'WAN', 'interface': 'pppoe-out1'}, {'list': 'WAN', 'interface': 'wan'},
                                  {'list': 'LAN', 'interface': lan}]}


def nat_context(gateway, cidr):
    if maybe(0.04):
        return None
    if maybe(0.6):
        return realistic_context(gateway, cidr)
    ctx = {'routes': [route_row(i, gateway, cidr if maybe(0.4) else None) for i in range(rng.randint(0, 5))],
           'rules': [nat_row(i) for i in range(rng.randint(0, 5))],
           'routing_rules': pick([], [], [{'.id': '*R1', 'disabled': pick('true', 'false')}], None),
           'mangle': mangle_rows(),
           'vrfs': pick([], [], [{'name': 'main'}], [{'name': 'vrf1', 'disabled': pick('true', 'false')}], None),
           'interface_lists': pick([{'.id': '*L1', 'name': 'WAN'}, {'.id': '*L2', 'name': 'LAN'},
                                    {'.id': '*L3', 'name': 'INC', 'include': 'WAN'},
                                    {'.id': '*L4', 'name': 'ALLM'}], None),
           'interface_members': pick([{'list': 'WAN', 'interface': 'pppoe-out1', 'disabled': 'false'},
                                      {'list': 'WAN', 'interface': 'wan', 'disabled': pick('false', 'true')},
                                      {'list': 'LAN', 'interface': pick(*LAN)},
                                      {'list': 'ALLM', 'interface': pick('all-ethernet', 'all', 'ether2')}],
                                     [{'list': 'WAN', 'interface': 'wan'}, {'list': '', 'interface': 'x'}], None)}
    if isinstance(ctx['interface_lists'], dict):
        ctx['interface_lists'] = [ctx['interface_lists']]
    if ctx['interface_lists'] is not None and not isinstance(ctx['interface_lists'], list):
        ctx['interface_lists'] = None
    for key in ('interface_lists', 'interface_members'):
        value = ctx[key]
        if isinstance(value, dict):
            ctx[key] = [value]
    # pick() over list literals: lists of dicts as alternatives
    if maybe(0.05):
        ctx['rules'] = None
    if maybe(0.03):
        ctx['routes'] = None
    if maybe(0.02):
        ctx['rules'].append({'.id': '*NC', 'action': 'masquerade'}) if ctx['rules'] is not None else None
    return ctx


def address_sets():
    sets = {}
    for name in ('lan', 'clients', 'proxy', 'fake', 'broken', 'cf-mihomo-nat-src', 'cf-mihomo-nat-dst'):
        if maybe(0.6):
            sets[name] = rng.sample(['192.168.0.0/24', '192.168.0.1-192.168.0.100', '192.168.0.0/25',
                                     '192.168.0.128/25', '10.10.0.0/16', '198.18.0.0/16', '203.0.113.0/24',
                                     '0.0.0.0/0', '172.16.5.0/24', '192.168.88.0/24', '198.51.100.7'],
                                    rng.randint(0, 4))
    if 'broken' in sets:
        sets['broken'].append(pick('bad', '1.2.3.9-1.2.3.1', '1.1.1.1-x'))
    return sets


def ros_state(gateway, cidr):
    state = RouterState(nat_context=nat_context(gateway, cidr), address_sets=address_sets())
    return state


def state_json(state):
    return {'nat_context': state.nat_context, 'address_sets': state.address_sets}


# ---- inspect_rule ------------------------------------------------------------------------------
def list_rule_rows():
    out = []
    for i in range(rng.randint(0, 4)):
        if maybe(0.5):
            row = {'.id': f'*{i}', 'chain': 'srcnat', 'action': 'accept', 'comment': 'cf-mihomo-nat',
                   'src-address-list': 'cf-mihomo-nat-src', 'dst-address-list': 'cf-mihomo-nat-dst'}
            for _ in range(rng.randint(0, 2)):
                key = pick('chain', 'action', 'src-address-list', 'dst-address-list', 'log', 'log-prefix',
                           'out-interface', 'disabled', 'dynamic', 'invalid', 'protocol', 'comment', 'zz', 'bytes')
                row[key] = pick('dstnat', 'masquerade', '!cf-mihomo-nat-src', 'x', 'true', 'yes', 'bridge',
                                '!bridge', '1.2.3.4', 'a,b', '', 'tcp', 'cf-mihomo-nat', 'cf-mihomo-nat-dst')
            out.append(row)
        else:
            out.append(nat_row(i))
    rng.shuffle(out)
    return out


# ---- iKuai groups ------------------------------------------------------------------------------
NOTES = ['', '', '', '公司', 'office vpn', '备注 with space', 'a%b', 'x"y', '\u0001ctl', 'emoji \U0001F600',
         'line\nbreak', ' padded ', "q'uote;$&|`\\", '长' * 40, 'tab\there', '　wide']


def cidrs(count, base='10.0.0.0/8', prefix=24):
    network = ipaddress.ip_network(base)
    out = []
    for subnet in network.subnets(new_prefix=prefix):
        if len(out) >= count:
            break
        out.append(str(subnet))
    return out


POOL = cidrs(9000, '10.0.0.0/8', 24)
BIG = [0]  # rows with more groups than one rule can reference


def value_spelling(cidr):
    return pick(cidr, cidr, cidr, cidr.replace('/32', ''), ' ' + cidr, cidr.split('/')[0] + '-' + cidr.split('/')[0])


def group_row(name, values, marker, gid, rid, refs=None, notes=None):
    entries = []
    for position, value in enumerate(values):
        comment = marker if position == 0 and marker is not None else (pick(*NOTES) if notes else '')
        entries.append({'ip': value, 'comment': comment})
    row = {'id': rid, 'group_id': gid, 'group_name': name, 'type': 0, 'group_value': entries}
    if refs is not None:
        row['ref_count'] = refs
    return row


def ikuai_rows():
    """route_object rows, NAT rows and 端口分流 rows of a synthetic iKuai."""
    groups, gid = [], 1
    roles = rng.sample(['pa', 'pm', 'nat'], rng.randint(1, 3))
    used = 0
    for role in roles:
        for index in range(1, rng.randint(1, pick(5, 5, 9))):
            size = 2000 if maybe(0.015) else 200 if maybe(0.04) else pick(0, 1, 3, 20, rng.randint(1, 60))
            values = [value_spelling(v) for v in POOL[used:used + size]]
            used += size
            marker = ig.ROLES[role] if maybe(0.85) else pick('edited', '', None)
            name = ig.group_name(role, pick(index, index, index + 3))
            groups.append(group_row(name, values, marker, f'IPGP{gid}', gid, pick(None, 0, 1, 2, 3), maybe(0.4)))
            gid += 1
    for _ in range(rng.randint(0, 2)):  # the user's groups, odd rows
        name = pick('office', 'cf_pa_99', 'cf_pa_1', 'cf_nat_2', 'cf_xx_1', 'cf_pa_01', '')
        groups.append(group_row(name, [value_spelling(v) for v in POOL[used:used + 3]], pick('mine', 'cf-mihomo', None),
                                f'IPGP{gid}', pick(gid, str(gid), None), pick(None, 1, 2)))
        used += 3
        gid += 1
    if maybe(0.1):
        groups.append({'id': 77, 'group_name': 'cf_pa_9', 'group_value': ['bad', {'comment': 'cf-mihomo'}]})
    names = [row.get('group_name') for row in groups]

    def refs():
        out = []
        for _ in range(rng.randint(0, 3)):
            target = pick(*groups) if groups else {}
            kind = rng.random()
            if kind < 0.6 and target:
                out.append({'type': 1, 'gid': target.get('group_id'), 'gp_name': target.get('group_name')})
            elif kind < 0.75:
                out.append({'type': 1, 'gp_name': pick(*names, 'cf_pa_42') if names else 'cf_pa_42'})
            elif kind < 0.85:
                out.append({'type': 1, 'gid': 'IPGP999', 'gp_name': pick('cf_nat_5', 'gone')})
            else:
                out.append(pick('junk', {'type': 1}))
        return out

    def address(custom_count):
        return {'custom': [pick(*POOL[8000:8100]) for _ in range(custom_count)] + ([5] if maybe(0.05) else []),
                'object': refs()}

    nat = []
    for i in range(rng.randint(0, 3)):
        nat.append({'id': 10 + i, 'action': pick('filter', 'filter', 'snat'), 'enabled': pick('yes', 'yes', 'no'),
                    'comment': pick('cf-mihomo', 'cf-tg', 'manual', '', 'cf-mihomo-nat'),
                    'src_addr': address(0), 'dst_addr': address(pick(0, 0, 3, 40)) if maybe(0.9) else {}})
    policy = []
    for i in range(rng.randint(0, 3)):
        policy.append({'id': 20 + i, 'enabled': pick('yes', 'yes', 'no'),
                       'comment': pick('cf-mihomo', 'cf-mihomo-manual', 'mine', ''),
                       'src_addr': address(0), 'dst_addr': address(pick(0, 0, 2, 30))})
    if maybe(0.1) and nat:
        nat.append(copy.deepcopy(nat[0]))
    return groups, nat, policy


def gstate_of(groups, nat, policy):
    parsed = [ig.group_from_row(row) for row in groups]
    ig.mark_owners(parsed)
    return {'groups': parsed, 'rules': ig.read_rules(parsed, nat, policy)}


def group_view(group):
    return {'id': group['id'], 'gid': group['gid'], 'name': group['name'], 'comment': group['comment'],
            'values': group['values'], 'entries': group['entries'], 'type': group['type'], 'refs': group['refs'],
            'role': group['role'] or '', 'index': group['index'] or 0, 'users': group['users'],
            'dup': bool(group.get('dup')), 'adopted': bool(group.get('adopted'))}


def rules_view(rules):
    return [{'key': key, 'func': rule['func'], 'id': rule['id'], 'comment': rule['comment'], 'enabled': rule['enabled'],
             'action': rule['action'] if isinstance(rule['action'], str) else '', 'custom': rule['custom'],
             'objects': rule['objects'], 'src_objects': rule['src_objects']} for key, rule in rules.items()]


def planner_script(gstate):
    keys = list(gstate['rules']) + [None, 'nat:404']
    steps = []
    for _ in range(rng.randint(1, 6)):
        op = pick('family', 'family', 'family', 'needs_rewrite', 'dropped', 'migration', 'deleted', 'rewritten',
                  'referenced', 'cleanup', 'check')
        if op == 'family':
            role = pick('pa', 'pm', 'nat')
            size = pick(1999, 2000, 2001, 4100, 33000) if maybe(0.04) else pick(0, 1, 5, 50, rng.randint(0, 300))
            start = pick(0, 0, 10, 300, 2500)
            want = [value_spelling(v) for v in POOL[start:start + size]]
            if size == 33000 and BIG[0] < 3:
                BIG[0] += 1
                want = cidrs(33000, '11.0.0.0/8', 30)
            if maybe(0.3):
                want += want[:5]
            if maybe(0.3):
                want = [*POOL[8000:8010], *want]
            rng.shuffle(want) if maybe(0.2) else None
            steps.append({'op': op, 'role': role, 'want': want, 'subject': pick(*keys)})
        elif op == 'needs_rewrite':
            steps.append({'op': op, 'key': pick(*keys), 'role': pick('pa', 'pm', 'nat'), 'keep': maybe()})
        elif op in ('dropped', 'deleted', 'referenced'):
            steps.append({'op': op, 'key': pick(*keys[:-2] or [None]) or 'nat:404'})
        elif op == 'migration':
            steps.append({'op': op, 'key': pick(*keys), 'kind': pick('端口分流', 'NAT 免转换'),
                          'rule': pick('cf-mihomo', 'cf-mihomo-manual', 'cf-tg')})
        elif op == 'rewritten':
            steps.append({'op': op, 'key': pick(*keys[:-2] or ['nat:404']),
                          'names': rng.sample(['cf_nat_1', 'cf_nat_2', 'cf_pa_1', 'cf_pm_1'], rng.randint(0, 2))})
        elif op == 'cleanup':
            steps.append({'op': op, 'roles': pick(['pa', 'pm'], ['nat'], ['pa'])})
        else:
            steps.append({'op': op, 'roles': pick(['pa', 'pm'], ['nat']),
                          'rules': [{'func': pick('stream_ipport', 'nat_rule'), 'comment': 'cf-mihomo',
                                     'groups': ['cf_pa_1'], 'keep_foreign': maybe()}],
                          'removed': pick([], ['cf_pa_3'])})
    if maybe(0.3):
        # A fragmented family: everything its groups hold is still wanted, plus a few more.
        role = pick('pa', 'pm', 'nat')
        held = [v for g in sorted(gstate['groups'], key=lambda g: g['index'] or 0) if g['role'] == role for v in g['values']]
        steps.insert(0, {'op': 'family', 'role': role, 'want': held + POOL[8500:8500 + pick(0, 3, 40)],
                         'subject': pick(*keys)})
    steps.append({'op': 'cleanup', 'roles': pick(['pa', 'pm'], ['nat'], ['pa', 'pm', 'nat'])})
    return steps


def run_planner(gstate, steps):
    warnings, blockers = ['earlier'], []
    planner = ig.GroupPlanner(gstate, warnings, blockers)
    results = []
    for step in steps:
        op = step['op']
        if op == 'family':
            final, ops = planner.family(step['role'], step['want'], step['subject'])
            results.append({'final': final, 'ops': ops, 'contents': planner.contents[step['role']]})
        elif op == 'needs_rewrite':
            results.append(planner.needs_rewrite(step['key'], step['role'], step['keep']))
        elif op == 'dropped':
            results.append(planner.dropped_foreign(step['key']))
        elif op == 'migration':
            results.append(planner.migration(step['key'], step['kind'], step['rule']))
        elif op == 'deleted':
            planner.deleted_rules.add(step['key'])
            results.append(None)
        elif op == 'rewritten':
            planner.rewritten[step['key']] = step['names']
            results.append(None)
        elif op == 'referenced':
            results.append(planner.referenced_values(step['key']))
        elif op == 'cleanup':
            results.append(planner.cleanup(tuple(step['roles'])))
        else:
            results.append(planner.check(tuple(step['roles']), step['rules'], step['removed']))
    return {'results': results, 'warnings': warnings, 'blockers': blockers,
            'migration_warning': planner.migration_warning()}


# ---- generation --------------------------------------------------------------------------------
def gen_ros():
    for _ in range(3000):
        gateway = pick(*GATEWAYS, '', 'bad')
        cidr = pick(*CIDRS, '198.18.0.1/16', 'bad')
        state = ros_state(gateway, cidr)
        value = {'cidr': cidr, 'gateway': gateway, 'state': state_json(state), 'policy_routing': maybe(0.2)}
        emit('assess_nat', value, assess_nat(cidr, gateway, state, policy_routing=value['policy_routing']))
        ids = [row.get('.id') for row in (state.nat_context or {}).get('rules') or [] if isinstance(row.get('.id'), str)]
        retiring = rng.sample(ids, min(len(ids), 1)) if maybe(0.2) else []
        if retiring:
            state.ros_policy_plan = {'ops': [{'owned': [{'id': rid, 'path': '/ip/firewall/nat'} for rid in retiring]}]}
        emit('assess_bypass', {**value, 'retiring': retiring}, assess_bypass(cidr, gateway, state))
    for _ in range(800):
        gateway = pick(*GATEWAYS)
        state = ros_state(gateway, pick(*CIDRS))
        if state.nat_context is None or not isinstance(state.nat_context.get('rules'), list) or not state.nat_context['rules']:
            continue
        ops = []
        for row in rng.sample(state.nat_context['rules'], rng.randint(1, len(state.nat_context['rules']))):
            if isinstance(row.get('.id'), str):
                ops.append({'kind': 'nat_update', 'id': row['.id'], 'comment': pick('cf-x', 'cf-mihomo'),
                            'add_dst': rng.sample(CIDRS, rng.randint(0, 3))})
        value = {'ops': ops, 'gateway': gateway, 'state': state_json(state)}
        emit('planned', value, planned_bypass_conflicts(ops, state, gateway))
    for _ in range(2000):
        gateway = pick(*GATEWAYS)
        ctx = nat_context(gateway, pick(*CIDRS)) or {'rules': []}
        rows_ = ctx.get('rules') or []
        sources = rng.sample(['192.168.0.0/25', '192.168.0.128/26', '192.168.0.4/30', '10.10.0.0/16', '0.0.0.0/0',
                              '192.168.88.0/24', '172.16.5.0/24'], rng.randint(1, 3))
        destinations = rng.sample(CIDRS, rng.randint(1, 3))
        iface = pick(None, *LAN, *WAN)
        context = {**ctx, 'address_sets': address_sets()}
        accepted = []
        result = preserves_sources(rows_, [ipaddress.IPv4Network(s) for s in sources],
                                   [ipaddress.IPv4Network(d) for d in destinations], iface, context,
                                   accepted_ids=accepted)
        emit('preserves', {'rows': rows_, 'sources': sources, 'destinations': destinations, 'interface': iface,
                           'context': ctx, 'address_sets': context['address_sets']},
             {'result': result, 'accepted': accepted})
    for _ in range(1500):
        rows_ = list_rule_rows()
        mangle = [(f'*M{i}', pick('cf-mihomo-nat-src', 'cf-mihomo-nat-dst')) for i in range(pick(0, 0, 0, 1))]
        firewall = [(pick('/ip/firewall/filter', '/ip/firewall/raw'), '*F1', 'cf-mihomo-nat-dst')
                    for _ in range(pick(0, 0, 0, 1))]
        found = inspect_rule(rows_, mangle, firewall)
        emit('inspect_rule', {'rows': rows_, 'mangle': mangle, 'firewall': firewall}, found)


def route_entries(gateway):
    out = []
    for i in range(rng.randint(0, 6)):
        dst = pick(*CIDRS, '198.18.0.0/17', '198.18.128.0/17', '10.0.0.0/8', 'bad')
        entry = RouteEntry(f'r{i}', dst, pick(gateway, gateway, '192.168.0.9', ''), maybe(0.8),
                           pick('cf-fake-ip', 'cf', 'manual', '', 'cf-ros-policy-x', 'CF-x'),
                           table=pick('main', 'main', 'main', 'backup'),
                           active=pick(None, True, False), distance=pick(None, 1, 2, 0))
        out.append(entry)
    return out


def policy_entries(gateway):
    out = []
    for i, comment in enumerate(rng.sample(['cf-mihomo', 'cf-mihomo-manual', 'mine'], rng.randint(0, 3))):
        dst = rng.sample(CIDRS + ['1.1.1.1', '8.8.8.8', '10.0.0.1-10.0.0.9'], rng.randint(0, 5))
        out.append(PolicyRouteEntry(f'{20 + i}', dst, pick(gateway, gateway, '192.168.0.9', ''), maybe(0.85), comment,
                                    src=pick([gateway], [gateway], ['10.0.0.1']), src_invert=maybe(0.85),
                                    dst_invert=maybe(0.1), protocol=pick('any', 'any', 'tcp'),
                                    priority=pick(0, 0, 1, None)))
    return out


def nat_entries():
    return [NatBypassEntry(f'{10 + i}', rng.sample(CIDRS + ['10.0.0.0/8', '1.1.1.1-1.1.1.20', 'bad'], rng.randint(0, 4)),
                           ['192.168.0.0/24'], maybe(0.8), pick('cf-mihomo', 'manual', '', 'cf-mihomo-nat'),
                           {'k': i})
            for i in range(rng.randint(0, 4))]


def as_json(entries):
    return [entry.__dict__ for entry in entries] if entries is not None else None


def gen_report():
    for n in range(2500):
        gateway = pick(*GATEWAYS, '')
        count = pick(1, 2, 3, 5, 0, 300 if maybe(0.01) else 4)
        cidrs_ = sorted(set(rng.sample(CIDRS, min(count, len(CIDRS))) + POOL[:count if count > 50 else 0]))
        desired = {'fake_ip_range': '198.18.0.0/16', 'proxy_gateway': gateway,
                   'proxy_cidrs': [{'cidr': c, 'sources': ['fake-ip' if c.startswith('198.18') else 'rules']}
                                   for c in cidrs_ if not c.startswith('100.64')],
                   'ipv6_cidrs': [], 'warnings': ['w'], 'blocking_warnings': []}
        if maybe(0.2):
            desired.update({'dns_agent_id': pick('dns-a', ''), 'dns_server': pick('192.168.0.53', ''),
                            'dns_error': pick('', '所选 DNS Agent 不存在，请重新选择')})
        vendor = pick('routeros', 'routeros', 'ikuai', 'legacy')
        state = RouterState(routes=route_entries(gateway), nat_bypass=nat_entries())
        ik = None
        if vendor == 'routeros':
            state.nat_context = nat_context(gateway, pick(*desired['proxy_cidrs'])['cidr'] if desired['proxy_cidrs'] else None)
            state.address_sets = address_sets()
        elif vendor == 'ikuai':
            if maybe(0.7):
                state.policy_routes = policy_entries(gateway)
            elif maybe(0.4):
                state.retired_policy_routes = policy_entries(gateway)
            if maybe(0.4):
                state.active_routes = []
            if maybe(0.75):
                ik = ikuai_rows()
                state.ikuai_groups = gstate_of(*ik)
        value = {'desired': desired, 'state': {
            'routes': as_json(state.routes), 'nat_bypass': as_json(state.nat_bypass),
            'address_sets': state.address_sets, 'nat_context': copy.deepcopy(state.nat_context),
            'policy_routes': as_json(state.policy_routes), 'retired_policy_routes': as_json(state.retired_policy_routes)},
            'ikuai': ik and {'groups': ik[0], 'nat': ik[1], 'policy': ik[2]}}
        emit('report', value, build_report(copy.deepcopy(desired), state))


def gen_ikuai():
    for _ in range(1500):
        groups, nat, policy = ikuai_rows()
        value = {'groups': groups, 'nat': nat, 'policy': policy}
        gstate = gstate_of(groups, nat, policy)
        small = sum(len(row.get('group_value') or []) for row in groups) < 300
        if small:
            emit('ikuai_read', value, {'groups': [group_view(g) for g in gstate['groups']],
                                       'rules': rules_view(gstate['rules'])})
        steps = planner_script(gstate)
        emit('ikuai_plan', {**value, 'steps': steps}, *plan_rows(groups, nat, policy, steps))
        # Read-back: a check planned on this state, against this or a tampered readback.
        planner = ig.GroupPlanner(gstate_of(groups, nat, policy), [], [])
        role = pick('pa', 'pm', 'nat')
        final, _ = planner.family(role, POOL[:rng.randint(0, 30)], None)
        check = planner.check((role,), [{'func': pick('stream_ipport', 'nat_rule'),
                                         'id': pick(None, '10', '20', '11'),
                                         'comment': pick('cf-mihomo', 'cf-tg', 'cf-mihomo-manual'),
                                         'groups': pick(final, [g['name'] for g in planner.owned(role)], []),
                                         'keep_foreign': maybe()} for _ in range(rng.randint(0, 2))],
                              pick([], [g['name'] for g in planner.owned(role)][:1], ['cf_pa_7']))
        if maybe(0.4):
            check['groups'] = {g['name']: ig.digest(g['values']) for g in planner.owned(role)}
        for rule in check['rules']:
            if rule['id'] is None:
                del rule['id']
        missing = maybe(0.05)
        try:
            ig.verify(check, RouterState(ikuai_groups=None if missing else gstate_of(groups, nat, policy)))
            out = None
        except RouterError as exc:
            out = str(exc)
        if small:
            emit('verify', {**value, 'check': check, 'missing': missing}, out)
    for n in range(300):
        # Fragmented families (repacked when they hold more than two groups too many).
        role, used, groups = pick('pa', 'pm', 'nat'), 0, []
        for index in range(1, rng.randint(2, 9)):
            size = (1990 if n % 25 == 0 else pick(1, 5, 30, 120)) if maybe(0.9) else 0
            groups.append(group_row(ig.group_name(role, index + pick(0, 0, 0, 2)), POOL[used:used + size],
                                    ig.ROLES[role], f'IPGP{index}', index, pick(None, None, None, 0, 1), maybe(0.3)))
            used += size
        groups = [g for g in groups if g['group_value'] or maybe(0.5)]
        held = [v for g in groups for v in (e['ip'] for e in g['group_value'])]
        want = rng.sample(held, int(len(held) * pick(1, 1, 0.9, 0.5))) + POOL[8500:8500 + (2500 if n % 30 == 1 else pick(0, 3, 40))]
        if maybe(0.5):
            want.sort(key=lambda v: POOL.index(v) if v in POOL else 0)
        nat = [{'id': 10, 'action': 'filter', 'enabled': 'yes', 'comment': 'cf-mihomo',
                'dst_addr': {'custom': [], 'object': [{'gid': g['group_id'], 'gp_name': g['group_name']}
                                                      for g in groups if maybe(0.8)]}}] if role == 'nat' else []
        steps = [{'op': 'family', 'role': role, 'want': want, 'subject': 'nat:10' if nat else None},
                 {'op': 'cleanup', 'roles': [role]}]
        emit('ikuai_plan', {'groups': groups, 'nat': nat, 'policy': [], 'steps': steps},
             *plan_rows(groups, nat, [], steps))
    for _ in range(600):
        role = pick('pa', 'pm', 'nat')
        values = [value_spelling(v) for v in rng.sample(POOL, rng.randint(0, 20))] + \
            [pick('1.2.3.4', ' 5.6.7.8 ', 'a b', 'x%y', '中文', '\U0001F600', 'q"\\', '\u0085')] * pick(0, 1)
        notes = {ig.canon(v): pick(*NOTES) for v in values if maybe(0.4)}
        name = pick(ig.group_name(role, rng.randint(1, 9999)), 'x y', '名字')
        row_id = pick(None, 7, 1234567890)
        emit('payload', {'name': name, 'role': role, 'values': values, 'notes': notes, 'id': row_id},
             {'payload': ig.payload(name, role, values, row_id, notes), 'bytes': ig.group_bytes(name, role, values, notes),
              'entry_payload': ig.entry_payload(name, [{'ip': v, 'comment': 'n'} for v in values], row_id)})
    for host in range(256):
        gateway = f'192.168.{host % 3}.{host}'
        emit('client_sources', gateway, [str(n) for n in client_source_networks(gateway)])
    for _ in range(400):
        names = [ig.group_name(pick('pa', 'pm', 'nat'), rng.randint(1, 5)) for _ in range(rng.randint(0, 4))]
        if maybe(0.5) and names:
            names = [ig.group_name('pa', i) for i in range(1, len(names) + 1)]
        emit('span', names, ig.span(names))
    for value in ['1.1.1.1', '1.1.1.1/32', ' 10.0.0.0/8 ', '10.0.0.1/8', '1.2.3.4-1.2.3.9', '1.2.3.4 - 1.2.3.9',
                  '1.2.3.9-1.2.3.4', 'x-y', 'bad', '', '2001:db8::1', '2001:db8::/32', '1.2.3.4/255.255.255.0',
                  '1.2.3.4-5-6', '01.2.3.4', '1.2.3.4/33', '-', '1.1.1.1-']:
        emit('canon', value, ig.canon(value))
        emit('networks', [value], [str(n) for n in _networks([value])])
    for _ in range(300):
        values = [value_spelling(v) for v in rng.sample(POOL[:50], rng.randint(0, 8))]
        emit('digest', values, ig.digest(values))
        emit('networks', values, [str(n) for n in _networks(values)])


gen_ros()
gen_report()
gen_ikuai()
with gzip.open(sys.argv[1], 'wt', encoding='utf-8') as out:
    for row in rows:
        out.write(json.dumps(row, ensure_ascii=False, default=str) + '\n')
print(len(rows), 'rows')
