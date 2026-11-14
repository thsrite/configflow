"""Opt-in RouterOS DNS guard: send LAN clients' plain DNS to the selected OxiDNS.

Clients with hard-coded resolvers ignore DHCP DNS and get poisoned answers. A
dstnat pair sends their port-53 queries to OxiDNS and a hairpin masquerade
returns the replies through the router. Optional filter rules reject DoT/DoQ,
and a Netwatch pauses the hijack while OxiDNS stops answering on TCP 53.

Objects are owned only through ``dns_guard_state``: device IDs plus the exact
configuration read back after writing. ``cf-dnsguard:`` comments help people in
WinBox and are never treated as proof of ownership. IPv4 only, like the other
router stages.
"""
import copy
import ipaddress
from typing import Any, Dict, List, Optional, Tuple

from backend.routers.base import RouterError, RouterRollbackError

ADDRESSES = '/ip/firewall/address-list'
NAT = '/ip/firewall/nat'
FILTER = '/ip/firewall/filter'
NETWATCH = '/tool/netwatch'
PATHS = (ADDRESSES, NAT, FILTER, NETWATCH)
PREFIX = 'cf-dnsguard:'
BYPASS_LIST = 'cf-dnsguard-bypass'
NETWATCH_NAME = 'cf-dnsguard'
HIJACK_COMMENT = PREFIX + 'hijack'
MANIFEST_VERSION = 1
MAX_BYPASS = 64
DEFAULTS = {'enabled': False, 'block_dot': False, 'failover': True, 'bypass': []}
# Netwatch replies carry probe counters and timestamps; only these are configuration.
NETWATCH_FIELDS = ('name', 'host', 'port', 'type', 'interval', 'timeout', 'startup-delay', 'up-script',
                   'down-script', 'comment', 'disabled')
# Pause only after the outage survives this long (at least two 10 s probes).
PAUSE_CONFIRM = '15s'
# Bypass networks this small are expanded into addresses for the resume flush.
SMALL_EXEMPT_PREFIX = 30
MAX_EXEMPT_HOSTS = 64
RUNTIME_FIELDS = {'dynamic', 'invalid', 'bytes', 'packets', 'creation-time'}
STAGE_TITLE = 'DNS 劫持'
PATH_NAMES = {ADDRESSES: '放行地址', NAT: 'NAT', FILTER: '防火墙', NETWATCH: 'Netwatch'}


class SettingsError(ValueError):
    pass


def normalize_settings(value: Any, router_type: str, dns_agent_id: str) -> Dict[str, Any]:
    """Validate the saved switch; absent means the feature was never enabled."""
    if value is None:
        return copy.deepcopy(DEFAULTS)
    if not isinstance(value, dict) or set(value) - set(DEFAULTS):
        raise SettingsError('强制 DNS 设置格式无效')
    settings = {**copy.deepcopy(DEFAULTS), **value}
    for key in ('enabled', 'block_dot', 'failover'):
        if type(settings[key]) is not bool:
            raise SettingsError('强制 DNS 的开关必须为布尔值')
    bypass = settings['bypass']
    if not isinstance(bypass, list) or any(not isinstance(item, str) for item in bypass):
        raise SettingsError('放行设备必须是 IPv4 地址或网段列表')
    networks = []
    for item in bypass:
        try:
            networks.append(ipaddress.IPv4Network(item.strip(), strict=False))
        except ValueError:
            raise SettingsError(f'放行设备「{item}」不是有效的 IPv4 地址或网段') from None
    if len(networks) > MAX_BYPASS:
        raise SettingsError(f'放行设备最多 {MAX_BYPASS} 项')
    settings['bypass'] = list(dict.fromkeys(str(network) for network in networks))
    if settings['enabled']:
        if router_type != 'routeros':
            raise SettingsError('强制局域网 DNS 走 OxiDNS 仅支持 RouterOS')
        if not dns_agent_id:
            raise SettingsError('请先选择 DHCP DNS 服务器（OxiDNS Agent），强制 DNS 使用同一台设备')
    return settings


def relevant(router: Dict[str, Any]) -> bool:
    """Only routers that enable the guard or still own objects get the stage."""
    settings = router.get('dns_guard') or {}
    return router.get('type') == 'routeros' and (settings.get('enabled') is True or bool(router.get('dns_guard_state')))


def snapshot(driver) -> Dict[str, Optional[List[Dict[str, Any]]]]:
    from backend.routers.ros_address_lists import filtered, locate
    live = {}
    for path in PATHS:
        try:
            # Only the bypass list is ever owned or checked for name collisions.
            live[path] = filtered(driver, BYPASS_LIST) if path == ADDRESSES else driver._list(path)
            if path == ADDRESSES:
                # A managed member moved out of the bypass list must still look edited.
                present = {row.get('.id') for row in live[path]}
                manifest = getattr(driver, 'dns_guard_manifest', None) or {}
                live[path] += locate(driver, [item['id'] for item in manifest.get('resources', [])
                                             if item.get('path') == ADDRESSES and item['id'] not in present])
        except RouterError:
            if path != NETWATCH:
                raise
            live[path] = None  # older firmware or a read-restricted account
    return live


def stable(path: str, row: Dict[str, Any]) -> Dict[str, str]:
    from backend.routers.routeros import _comparable
    if path == NETWATCH:
        values = {key: row[key] for key in NETWATCH_FIELDS if key in row}
    else:
        values = {key: value for key, value in row.items()
                  if not key.startswith('.') and key not in RUNTIME_FIELDS}
    return {key: _comparable(key, value) for key, value in values.items()}


def _paused_field(role: str, key: str, failover: bool) -> bool:
    # The guard's own Netwatch toggles the hijack pair; that is not tampering.
    return failover and role.startswith('hijack') and key == 'disabled'


def _comparable_config(role: str, config: Dict[str, str], failover: bool) -> Dict[str, str]:
    return {key: value for key, value in config.items() if not _paused_field(role, key, failover)}


def owned_entries(previous: Optional[Dict[str, Any]], live) -> List[Dict[str, Any]]:
    """Manifest entries still present, in creation order; raise on outside edits."""
    owned = []
    failover = bool(((previous or {}).get('spec') or {}).get('failover'))
    for entry in (previous or {}).get('resources', []):
        rows = live.get(entry['path'])
        if rows is None:
            raise RouterError('无法读取受管的 Netwatch，不能确认 DNS 劫持对象的归属')
        matches = [row for row in rows if str(row.get('.id')) == entry['id']]
        if len(matches) > 1:
            raise RouterError('DNS 劫持对象 ID 不唯一，停止发布')
        if not matches:
            continue  # removed by hand; recreated by the next release
        if (_comparable_config(entry['role'], stable(entry['path'], matches[0]), failover)
                != _comparable_config(entry['role'], entry['config'], failover)):
            raise RouterError(f'DNS 劫持的受管对象被外部修改，保留现场，请先核对：{entry["path"]}/{entry["id"]}')
        owned.append(entry)
    return owned


def _connected(context: Optional[Dict[str, Any]], address: ipaddress.IPv4Address) -> Tuple[str, str]:
    """The unique active connected route reaching the OxiDNS host."""
    from backend.routers.routeros_nat import _interface, _truthy
    rows = (context or {}).get('routes')
    if not isinstance(rows, list):
        raise RouterError('无法读取路由表，不能确定 OxiDNS 所在的局域网接口')
    candidates = []
    for row in rows:
        if (row.get('routing-table') or 'main') != 'main' or not _truthy(row.get('connect')):
            continue
        if not _truthy(row.get('active')) or _truthy(row.get('disabled')):
            continue
        try:
            network = ipaddress.IPv4Network(str(row.get('dst-address') or ''), strict=False)
        except ValueError:
            continue
        if address in network:
            candidates.append((network, row))
    if not candidates:
        raise RouterError('OxiDNS 不在路由器的直连网段内，不能确定劫持所用的局域网接口')
    longest = max(network.prefixlen for network, _ in candidates)
    selected = [(network, row) for network, row in candidates if network.prefixlen == longest]
    if len(selected) != 1:
        raise RouterError('OxiDNS 所在网段存在多条直连路由，不能确定唯一的局域网接口')
    network, row = selected[0]
    interface = _interface(row.get('gateway'))
    immediate = _interface(row.get('immediate-gw')) if row.get('immediate-gw') else interface
    if not interface or immediate != interface:
        raise RouterError('OxiDNS 所在网段的直连路由没有给出唯一接口')
    return interface, str(network)


def _ha_desired_spec(settings: Dict[str, Any], desired: Dict[str, Any], state) -> Dict[str, Any]:
    """Spec for an OxiDNS pair behind a virtual IP (VIP), with real hosts R1 and R2.

    The hijack and hairpin target the VIP. Both real hosts are exempt: their
    upstream queries leave from the real address, and whichever holds the VIP
    after a failover must not be hijacked back to itself. ``exempt`` keeps the
    single addresses before collapsing, so the resume flush can spare them even
    when they merge into a network larger than /30.
    """
    ha = desired['dns_ha']
    try:
        vip = ipaddress.IPv4Address(ha['vip'])
        hosts = [ipaddress.IPv4Address(host) for host in ha['hosts']]
    except (KeyError, TypeError, ValueError):
        raise RouterError('无法确定 OxiDNS 高可用的虚拟 IP 和主机地址') from None
    interface, network = _connected(state.nat_context, vip)
    for host in hosts:
        try:
            same = _connected(state.nat_context, host) == (interface, network)
        except RouterError:
            same = False
        if not same:
            raise RouterError(f'OxiDNS 高可用主机 {host} 必须与虚拟 IP {vip} 在同一直连网段 {network}')
    targets = [entry['cidr'] for entry in desired.get('proxy_cidrs') or []]
    targets += [entry['cidr'] for entry in (desired.get('ros_policy') or {}).get('cidrs') or []]
    for address in [vip, *hosts]:
        if any(address in ipaddress.IPv4Network(cidr, strict=False) for cidr in targets):
            raise RouterError(f'OxiDNS 地址 {address} 落在代理目的网段内，劫持后的查询会被送往 Mihomo，请检查网段设置')
    singles = [vip, *hosts]
    try:
        singles.append(ipaddress.IPv4Address(desired.get('proxy_gateway') or ''))
    except ValueError:
        pass  # no linked gateway: nothing else on the LAN needs upstream DNS
    bypass = [ipaddress.IPv4Network(str(address)) for address in singles]
    bypass += [ipaddress.IPv4Network(item) for item in settings['bypass']]
    exempt = [str(address) for address in singles]
    for item in settings['bypass']:
        small = ipaddress.IPv4Network(item)
        if small.prefixlen >= SMALL_EXEMPT_PREFIX:
            exempt.extend(str(address) for address in small)
    return {'agent': str(vip), 'interface': interface, 'network': network,
            'bypass': [str(item) for item in ipaddress.collapse_addresses(bypass)],
            'block_dot': settings['block_dot'], 'failover': settings['failover'],
            'exempt': list(dict.fromkeys(exempt))[:MAX_EXEMPT_HOSTS]}


def desired_spec(settings: Dict[str, Any], desired: Dict[str, Any], state) -> Dict[str, Any]:
    if desired.get('dns_error'):
        raise RouterError('强制 DNS 依赖 DHCP DNS 设置：' + desired['dns_error'])
    if desired.get('dns_ha'):
        return _ha_desired_spec(settings, desired, state)
    try:
        agent = ipaddress.IPv4Address(desired.get('dns_server') or '')
    except ValueError:
        raise RouterError('无法确定 OxiDNS Agent 的 IPv4 地址') from None
    interface, network = _connected(state.nat_context, agent)
    targets = [entry['cidr'] for entry in desired.get('proxy_cidrs') or []]
    targets += [entry['cidr'] for entry in (desired.get('ros_policy') or {}).get('cidrs') or []]
    if any(agent in ipaddress.IPv4Network(cidr, strict=False) for cidr in targets):
        raise RouterError('OxiDNS 地址落在代理目的网段内，劫持后的查询会被送往 Mihomo，请检查网段设置')
    bypass = [ipaddress.IPv4Network(str(agent))]
    try:
        bypass.append(ipaddress.IPv4Network(str(ipaddress.IPv4Address(desired.get('proxy_gateway') or ''))))
    except ValueError:
        pass  # no linked gateway: nothing else on the LAN needs upstream DNS
    bypass += [ipaddress.IPv4Network(item) for item in settings['bypass']]
    return {'agent': str(agent), 'interface': interface, 'network': network,
            'bypass': [str(item) for item in ipaddress.collapse_addresses(bypass)],
            'block_dot': settings['block_dot'], 'failover': settings['failover']}


def resources(spec: Dict[str, Any]) -> List[Dict[str, Any]]:
    """Creation order: exemptions and hairpin before the hijack can take effect."""
    items = [{'role': f'bypass:{cidr}', 'path': ADDRESSES,
              'values': {'list': BYPASS_LIST, 'address': cidr, 'comment': PREFIX + 'bypass', 'disabled': 'false'}}
             for cidr in spec['bypass']]
    for protocol in ('udp', 'tcp'):
        items.append({'role': f'hairpin-{protocol}', 'path': NAT, 'values': {
            'chain': 'srcnat', 'action': 'masquerade', 'out-interface': spec['interface'],
            'src-address': spec['network'], 'dst-address': spec['agent'], 'protocol': protocol,
            'dst-port': '53', 'comment': PREFIX + 'hairpin', 'disabled': 'false'}})
    for protocol in ('udp', 'tcp'):
        # Top of the NAT list: an earlier 53 dst-nat must not take over the pair.
        items.append({'role': f'hijack-{protocol}', 'path': NAT, 'top': True, 'values': {
            'chain': 'dstnat', 'action': 'dst-nat', 'in-interface': spec['interface'],
            'src-address-list': '!' + BYPASS_LIST, 'dst-address': '!' + spec['agent'],
            'dst-address-type': '!local', 'protocol': protocol, 'dst-port': '53',
            'to-addresses': spec['agent'], 'to-ports': '53', 'comment': HIJACK_COMMENT, 'disabled': 'false'}})
    if spec['block_dot']:
        for protocol, action in (('tcp', {'action': 'reject', 'reject-with': 'tcp-reset'}), ('udp', {'action': 'drop'})):
            items.append({'role': f'dot-{protocol}', 'path': FILTER, 'top': True, 'values': {
                'chain': 'forward', 'in-interface': spec['interface'], 'protocol': protocol, 'dst-port': '853',
                'src-address-list': '!' + BYPASS_LIST, **action, 'comment': PREFIX + 'dot', 'disabled': 'false'}})
    if spec['failover']:
        items.append({'role': 'netwatch', 'path': NETWATCH, 'values': {
            'name': NETWATCH_NAME, 'host': spec['agent'], 'port': '53', 'type': 'tcp-conn', 'interval': '10s',
            # RouterOS waits 5 minutes after boot by default; OxiDNS may share the host.
            'timeout': '2s', 'startup-delay': '30s',
            'up-script': _netwatch_script(spec, 'up'), 'down-script': _netwatch_script(spec, 'down'),
            'comment': PREFIX + 'netwatch', 'disabled': 'false'}})
    return items


def _exempt_hosts(bypass: List[str]) -> List[str]:
    """Single addresses the resume flush must keep, from the collapsed list.

    collapse_addresses merges adjacent hosts (OxiDNS .2 and mihomo .3 become
    .2/31), so expand small networks back into addresses. Larger networks
    cannot be matched by a plain address comparison and stay unprotected.
    """
    hosts = []
    for cidr in bypass:
        network = ipaddress.IPv4Network(cidr)
        if network.prefixlen >= SMALL_EXEMPT_PREFIX:
            hosts.extend(str(address) for address in network)
    return hosts[:MAX_EXEMPT_HOSTS]


def _netwatch_script(spec: Dict[str, Any], event: str) -> str:
    """Toggle the hijack pair and drop port-53 flows still bound to the old path.

    Conntrack keeps a client's NAT decision for as long as the same source port
    keeps talking (up to the 3 min UDP stream timeout). Without the flush such
    clients keep querying a dead OxiDNS after a pause, or keep bypassing it
    after a resume. Each removal tolerates flows that expired meanwhile.
    """
    agent = spec['agent']
    if event == 'down':
        toggle = f'/ip firewall nat disable [find comment="{HIJACK_COMMENT}"]'
        stale = f'dst-port=53 and dstnat and reply-src-address={agent}'
    else:
        toggle = f'/ip firewall nat enable [find comment="{HIJACK_COMMENT}"]'
        stale = ' and '.join(['dst-port=53 and !dstnat', f'dst-address!={agent}']
                             + [f'src-address!={host}' for host in
                                (spec['exempt'] if 'exempt' in spec else _exempt_hosts(spec['bypass']))])
    flush = (f':foreach c in=[/ip firewall connection find where {stale}] do={{'
             ':do {/ip firewall connection remove $c} on-error={}}')
    if event == 'down':
        return (f':delay {PAUSE_CONFIRM}; :if ([/tool netwatch get [find name="{NETWATCH_NAME}"] status] = "down") '
                f'do={{{toggle}; {flush}}}')
    return f'{toggle}; {flush}'


def _matches(item: Dict[str, Any], row: Dict[str, Any], failover: bool) -> bool:
    from backend.routers.routeros import _comparable
    return all(_comparable(key, row.get(key, '')) == _comparable(key, value)
               for key, value in item['values'].items() if not _paused_field(item['role'], key, failover))


def _hijack_on_top(row: Dict[str, Any], nat: List[Dict[str, Any]], owned_ids: set) -> bool:
    """No other enabled port-53 dst-nat precedes this owned hijack (verify() requires it)."""
    for other in nat:
        if str(other.get('.id')) == str(row.get('.id')):
            return True
        if (other.get('chain') == 'dstnat' and str(other.get('dst-port', '')) == '53'
                and str(other.get('disabled')).lower() not in ('true', 'yes')
                and (NAT, str(other.get('.id'))) not in owned_ids):
            return False
    return False


def _kept_roles(items: List[Dict[str, Any]], owned: List[Dict[str, Any]], live, failover: bool) -> set:
    """Owned objects that already are exactly what the new spec asks for stay untouched.

    Re-creating them would briefly drop the hijack and give the rules new IDs for
    no reason; only the objects whose content changes are deleted and created.
    """
    by_role = {entry['role']: entry for entry in owned}
    rows = {(path, str(row.get('.id'))): row for path in PATHS for row in live[path] or []}
    owned_ids = {(entry['path'], entry['id']) for entry in owned}
    kept = set()
    for item in items:
        entry = by_role.get(item['role'])
        row = rows.get((item['path'], entry['id'])) if entry and entry['path'] == item['path'] else None
        if row is None or not _matches(item, row, failover):
            continue
        if item['role'].startswith('hijack') and not _hijack_on_top(row, live[NAT] or [], owned_ids):
            continue
        kept.add(item['role'])
    return kept


def _summary(spec: Optional[Dict[str, Any]], previous: Optional[Dict[str, Any]]) -> str:
    if spec is None:
        return '撤销 DNS 劫持：删除已核实归属的受管对象'
    extras = ['拦截 853 加密 DNS' if spec['block_dot'] else '', 'OxiDNS 故障时自动暂停' if spec['failover'] else '']
    detail = '，'.join(item for item in extras if item)
    verb = '更新' if previous else '启用'
    return (f'{verb} DNS 劫持：{spec["interface"]} 上的 53 端口查询转给 {spec["agent"]}，'
            f'放行 {", ".join(spec["bypass"])}' + (f'；{detail}' if detail else ''))


def build(desired: Dict[str, Any], state) -> Dict[str, Any]:
    """Plan ops, report items, blockers and warnings from one device read."""
    request = desired['dns_guard']
    settings, previous = request['settings'], request.get('previous')
    live = state.dns_guard
    result = {'enabled': settings['enabled'], 'status': 'disabled', 'message': '', 'spec': None,
              'items': [], 'ops': [], 'blockers': [], 'warnings': [], 'roles': []}
    try:
        if live is None:
            raise RouterError('无法读取强制 DNS 相关的防火墙配置')
        owned = owned_entries(previous, live)
        spec = desired_spec(settings, desired, state) if settings['enabled'] else None
    except RouterError as error:
        result.update(status='unknown', message=str(error), blockers=[str(error)])
        return result
    owned_ids = {(entry['path'], entry['id']) for entry in owned}
    items = resources(spec) if spec else []
    result['spec'], result['roles'] = spec, [item['role'] for item in items]
    if spec and spec['failover'] and live[NETWATCH] is None:
        result['blockers'].append('路由器不支持 Netwatch 或账号无法读取，请关闭「OxiDNS 故障时自动暂停」')
    for path, key, name, label in ((ADDRESSES, 'list', BYPASS_LIST, '地址列表'), (NETWATCH, 'name', NETWATCH_NAME, 'Netwatch')):
        foreign = [row for row in live[path] or [] if row.get(key) == name and (path, str(row.get('.id'))) not in owned_ids]
        if foreign and spec:
            result['blockers'].append(f'{label}「{name}」已被其他配置使用，请先改名或删除这些对象')
    if spec:
        others = [row for row in live[NAT] if row.get('chain') == 'dstnat' and str(row.get('dst-port', '')) == '53'
                  and row.get('action') in ('dst-nat', 'redirect') and str(row.get('disabled')) not in ('true', 'yes')
                  and (NAT, str(row.get('.id'))) not in owned_ids]
        if others:
            names = '、'.join(str(row.get('comment') or row.get('.id')) for row in others)
            result['warnings'].append(f'路由器上已有其他 53 端口的 dst-nat / redirect 规则（{names}）。'
                                      '本功能的劫持规则放在 NAT 列表最前并优先生效；确认无误后可手动删除这些旧规则。')
    by_role = {entry['role']: entry for entry in owned}
    rows = {(path, str(row.get('.id'))): row for path in PATHS for row in live[path] or []}
    failover = bool(spec and spec['failover'])
    kept = _kept_roles(items, owned, live, failover)
    removed = [entry for entry in owned if entry['role'] not in kept]
    synced = (bool(previous) and previous.get('spec') == spec and len(kept) == len(items)
              and [entry['role'] for entry in owned] == result['roles'])
    for item in items:
        entry = by_role.get(item['role'])
        row = rows.get((item['path'], entry['id'])) if entry else None
        # Each row says what publishing does to it: nothing, replace, or create.
        # A first enable keeps its original all-pending report.
        status = 'synced' if item['role'] in kept else ('pending' if row is not None or not owned else 'add')
        result['items'].append({'role': item['role'], 'object': PATH_NAMES[item['path']],
                                'detail': _describe(item), 'status': status,
                                'paused': bool(item['role'].startswith('hijack') and row is not None
                                               and str(row.get('disabled')).lower() in ('true', 'yes'))})
    roles = set(result['roles'])
    for entry in removed:
        if entry['role'] not in roles:
            result['items'].append({'role': entry['role'], 'object': PATH_NAMES[entry['path']],
                                    'detail': _describe({'role': entry['role'], 'values': entry['config']}),
                                    'status': 'remove', 'paused': False})
    if not spec and not owned:
        result['status'] = 'disabled'
        result['message'] = '未开启强制 DNS；已发布的受管对象均已不存在。' if previous else '未开启强制 DNS。'
        if previous:
            result['ops'].append({'kind': 'dns_guard_replace', 'spec': None, 'previous': previous, 'owned': [],
                                  'create': [], 'summary': '清除已不存在对象的强制 DNS 发布记录'})
        return result
    if synced:
        result['status'] = 'synced'
        paused = any(item['paused'] for item in result['items'])
        result['message'] = ('OxiDNS 暂时没有应答，Netwatch 已自动暂停劫持；恢复后自动重新启用。' if paused
                             else '强制 DNS 规则与设置一致。')
        return result
    result['status'] = 'pending'
    result['message'] = '发布后撤销强制 DNS。' if spec is None else '发布后按以下对象维护强制 DNS。'
    op = {'kind': 'dns_guard_replace', 'spec': spec, 'previous': previous,
          'owned': [{key: entry[key] for key in ('role', 'path', 'id', 'config')} for entry in owned],
          'create': [item for item in items if item['role'] not in kept], 'summary': _summary(spec, previous)}
    if owned:
        # Without owned objects (a first enable) everything is created, exactly as before.
        op.update(keep=[role for role in result['roles'] if role in kept],
                  delete=[{key: entry[key] for key in ('role', 'path', 'id', 'config')} for entry in removed],
                  roles=result['roles'])
        op = {key: op[key] for key in ('kind', 'spec', 'previous', 'owned', 'keep', 'delete', 'create', 'roles', 'summary')}
    result['ops'].append(op)
    return result


def _describe(item: Dict[str, Any]) -> str:
    values, role = item['values'], item['role']
    if role.startswith('bypass'):
        return f'不劫持 {values["address"]}'
    if role.startswith('hairpin'):
        return f'{values["protocol"].upper()} 回包经路由器返回（{values["src-address"]} → {values["dst-address"]}:53）'
    if role.startswith('hijack'):
        return f'{values["protocol"].upper()} 53 端口查询转给 {values["to-addresses"]}（入口 {values["in-interface"]}）'
    if role.startswith('dot'):
        return f'{values["protocol"].upper()} 853 端口{"拒绝" if values["action"] == "reject" else "丢弃"}'
    return f'每 10 秒检测 {values["host"]}:53，持续 {PAUSE_CONFIRM} 无应答时暂停劫持，切换时清除旧的 53 端口连接'


def report(plan: Dict[str, Any]) -> Dict[str, Any]:
    return {key: plan[key] for key in ('enabled', 'status', 'message', 'items', 'blockers', 'warnings')}


def _rollback(driver, changes: List[Dict[str, Any]]) -> Dict[str, str]:
    """Undo journal entries; return re-created object IDs keyed by the deleted ID."""
    failures, remap = [], {}
    for change in changes:
        values = change.get('values') or {}
        if change['method'] == 'RESTORE' and values.get('place-before') in remap:
            # Adjacent pairs were deleted together; the neighbour came back first
            # under a new ID and is still the correct position.
            change = {**change, 'values': {**values, 'place-before': remap[values['place-before']]}}
        try:
            check = driver._perform(change, [], restoring=True)
            driver._verify_change(check)
            if change['method'] == 'RESTORE':
                remap[str(change['id'])] = check['id']
        except RouterError as error:
            failures.append(str(error))
    if failures:
        raise RouterRollbackError('强制 DNS 回滚未完成：' + '；'.join(failures))
    return remap


def _remapped(manifest: Optional[Dict[str, Any]], remap: Dict[str, str], driver) -> Optional[Dict[str, Any]]:
    """Restored objects get new IDs; keep owning exactly those objects."""
    if not manifest:
        return manifest
    result = copy.deepcopy(manifest)
    for entry in result['resources']:
        if entry['id'] in remap:
            entry['id'] = remap[entry['id']]
            entry['config'] = stable(entry['path'], driver._row(entry['path'], entry['id']))
    return result


def apply(driver, op: Dict[str, Any]) -> Dict[str, Any]:
    """Delete the changed owned objects, then create their replacements; undo on any failure.

    Owned objects the preview found already correct are left in place.
    """
    previous = op.get('previous')
    live = snapshot(driver)
    owned = owned_entries(previous, live)
    if [entry['id'] for entry in owned] != [entry['id'] for entry in op.get('owned') or []]:
        raise RouterError('强制 DNS 的受管对象在预览后发生变化，请重新预览')
    keep = set(op.get('keep') or [])
    by_role = {entry['role']: entry for entry in owned}
    failover = bool((op.get('spec') or {}).get('failover'))
    for item in resources(op['spec']) if op.get('spec') else []:
        if item['role'] in keep:
            entry = by_role.get(item['role'])
            row = next((row for row in live[item['path']] or [] if entry and str(row.get('.id')) == entry['id']), None)
            if row is None or not _matches(item, row, failover):
                raise RouterError('强制 DNS 的受管对象在预览后发生变化，请重新预览')
    undo: List[Dict[str, Any]] = []
    created = []
    try:
        for entry in reversed([entry for entry in owned if entry['role'] not in keep]):
            row = driver._row(entry['path'], entry['id'])
            check = driver._perform({'method': 'DELETE', 'path': entry['path'], 'id': entry['id'],
                                     'expected': driver._ownership_snapshot(entry['path'], row)}, undo)
            driver._verify_change(check)
        for item in op.get('create') or []:
            values = dict(item['values'])
            if item.get('top'):
                rows = driver._list(item['path'])
                if rows:
                    values['place-before'] = rows[0]['.id']
            check = driver._perform({'method': 'PUT', 'path': item['path'], 'values': values}, undo)
            driver._verify_change(check)
            created.append({'role': item['role'], 'path': item['path'], 'id': check['id']})
        ids = {entry['role']: entry for entry in created}
        ids.update({role: {key: by_role[role][key] for key in ('role', 'path', 'id')} for role in keep})
        resources_ = [{**ids[role], 'config': stable(ids[role]['path'], driver._row(ids[role]['path'], ids[role]['id']))}
                      for role in op.get('roles') or [entry['role'] for entry in created]]
    except RouterError as error:
        try:
            remap = _rollback(driver, list(reversed(undo)))
        except RouterRollbackError as rollback_error:
            raise RouterRollbackError(f'{error}；{rollback_error}') from error
        driver.dns_guard_manifest = _remapped(previous, remap, driver)
        raise
    driver.dns_guard_manifest = ({'version': MANIFEST_VERSION, 'spec': op['spec'], 'resources': resources_}
                                 if op.get('spec') else None)
    return {'kind': 'dns_guard_restore', 'changes': list(reversed(undo)), 'previous': previous}


def restore(driver, op: Dict[str, Any]) -> Dict[str, Any]:
    remap = _rollback(driver, op['changes'])
    driver.dns_guard_manifest = _remapped(op.get('previous'), remap, driver)
    return {}


def verify(driver, check: Dict[str, Any]) -> None:
    manifest = getattr(driver, 'dns_guard_manifest', None)
    live = snapshot(driver)
    owned = owned_entries(manifest, live)
    roles = [entry['role'] for entry in owned]
    if roles != check['roles'] or (not check['roles'] and manifest):
        raise RouterError('回读校验失败：强制 DNS 的受管对象与计划不一致')
    hijacks = [entry for entry in owned if entry['role'].startswith('hijack')]
    nat = live[NAT]
    for entry in hijacks:
        index = next(i for i, row in enumerate(nat) if str(row.get('.id')) == entry['id'])
        earlier = [row for row in nat[:index] if row.get('chain') == 'dstnat' and str(row.get('dst-port', '')) == '53'
                   and str(row.get('disabled')).lower() not in ('true', 'yes')
                   and str(row.get('.id')) not in {item['id'] for item in hijacks}]
        if earlier:
            raise RouterError('回读校验失败：强制 DNS 的劫持规则前面还有其他 53 端口规则')


def owned_hairpins(manifest: Optional[Dict[str, Any]], rules: List[Dict[str, Any]]) -> set:
    """Hairpin rows proven to be ours: they only translate queries to OxiDNS:53.

    Their single-host destination is outside every proxy CIDR (checked when the
    spec is built), so proxy-path NAT assessment may disregard them.
    """
    ids = set()
    for entry in (manifest or {}).get('resources', []):
        if not entry['role'].startswith('hairpin'):
            continue
        row = next((row for row in rules if str(row.get('.id')) == entry['id']), None)
        if row is not None and stable(NAT, row) == entry['config']:
            ids.add(entry['id'])
    return ids
