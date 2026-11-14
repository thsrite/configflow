"""Manifest-owned IPv6 Fake-IP routes in the ordinary RouterOS release workflow.

IPv6 destinations never enter the IPv4 NAT/return-routing planners. Existing
hand-written or legacy readiness routes are borrowed, not claimed by a comment.
"""
import copy
import ipaddress
from typing import Any, Dict

import yaml

from backend.routers.base import RouterError, RouterRollbackError
from backend.routers.ipv6_readiness import _gateway, _lan, _network, _same_gateway, enabled, yes

ROUTES = '/ipv6/route'
ADDRESSES = '/ipv6/address'
IPV4_ADDRESSES = '/ip/address'
FILTER = '/ipv6/firewall/filter'
ADDRESS_LISTS = '/ipv6/firewall/address-list'
PREFIX = 'cf-fakeip6:'
VERSION = 1
# 'valid' and 'preferred' are the remaining lifetimes of an address taken from
# a DHCPv6-PD pool; since RouterOS 7.25.1 they count down every second, so they
# would make every preview stale before it could be published.
RUNTIME = {'active', 'inactive', 'immediate-gw', 'belongs-to', 'hw-offloaded', 'debug.fwp-ptr',
           'actual-interface', 'bytes', 'packets', 'uptime', 'last-updated', 'expires-after', 'expires',
           'valid-lifetime', 'preferred-lifetime', 'valid', 'preferred'}


def settings(config):
    """Whether the saved Mihomo configuration has an IPv6 Fake-IP range to route.

    Like the IPv4 Fake-IP range, the route follows Mihomo's use of IPv6 and
    Fake-IP (top-level ``ipv6``, default on, and DNS ``fake-ip`` with a
    ``fake-ip-range6``), not ``dns.ipv6``: both IPv6 modes publish it, so a
    mode switch never needs the router again. ``dns.ipv6`` only decides whether
    clients receive these addresses; only then must the range be present and
    TUN be on to receive the connections. The keys stay as they were, since
    automatic rule sync fingerprints this result.
    """
    result = {'enabled': False, 'cidr': '', 'tun_enabled': False, 'error': ''}
    try:
        raw = (config.get('mihomo') or {}).get('custom_config') or ''
        document = yaml.safe_load(raw) if raw.strip() else {}
        if not isinstance(document, dict):
            raise ValueError('基础 YAML 必须是对象')
        dns = document.get('dns', {})
        if not isinstance(dns, dict):
            raise ValueError('dns 必须是对象')
        for name, value in [('ipv6', document.get('ipv6', True)), ('dns.ipv6', dns.get('ipv6', False))]:
            if type(value) is not bool:
                raise ValueError(name + ' 必须填写 true 或 false')
        clients = dns.get('ipv6') is True
        result['enabled'] = (document.get('ipv6', True) is True and dns.get('enable') is True
                             and dns.get('enhanced-mode') == 'fake-ip'
                             and (clients or dns.get('fake-ip-range6') not in (None, '')))
        if not result['enabled']:
            return result
        network = ipaddress.IPv6Network(dns.get('fake-ip-range6'), strict=False)
        if not network.subnet_of(ipaddress.IPv6Network('fc00::/7')):
            raise ValueError('IPv6 Fake-IP 网段必须是 ULA 地址，不能覆盖公网或链路本地地址')
        result['cidr'] = str(network)
        tun = document.get('tun') or {}
        result['tun_enabled'] = isinstance(tun, dict) and tun.get('enable') is True
        if clients and not result['tun_enabled']:
            raise ValueError('IPv6 Fake-IP 需要 Mihomo 开启 TUN；请先检查设备配置')
    except (yaml.YAMLError, ValueError, TypeError, AttributeError) as error:
        result['error'] = ('IPv6 Fake-IP 配置无法确认，请检查 YAML、fake-ip-range6 和 TUN 设置：' + str(error))
    return result


def relevant(config, router):
    return router.get('type') == 'routeros' and (settings(config)['enabled'] or bool(router.get('ipv6_fakeip_state')))


def stable(row):
    from backend.routers.routeros import _comparable
    values = {key: _comparable(key, value) for key, value in row.items()
              if not key.startswith('.') and key not in RUNTIME}
    # RouterOS' generic address-list comparer normalizes to a network. Interface
    # addresses must retain their host bits so an address change invalidates a preview.
    if 'address' in row:
        try:
            values['address'] = str(ipaddress.ip_interface(row['address']))
        except (TypeError, ValueError):
            values['address'] = str(row['address'])
        # _lan() uses the resolved interface of an IPv4 address. A bridge or
        # interface change must invalidate the transaction's pre-write proof.
        if 'actual-interface' in row:
            values['actual-interface'] = _comparable('actual-interface', row['actual-interface'])
    return values


def snapshot(driver, include_network=True):
    result = {'routes': driver._list(ROUTES)}
    if include_network:
        result.update(addresses=driver._list(ADDRESSES), ipv4_addresses=driver._list(IPV4_ADDRESSES))
    return result


def evidence(live):
    # Lease lifetimes and counters are not configuration changes. Route activity
    # is relevant, however: an inactive borrowed route must not become green.
    return {key: [{'.id': str(row.get('.id') or ''), **stable(row),
                   **({'active': yes(row.get('active'))} if key == 'routes' else {})}
                  for row in sorted(rows, key=lambda item: str(item.get('.id') or ''))]
            for key, rows in live.items()}


def owned_entries(previous, live):
    if not previous:
        return []
    if (not isinstance(previous, dict) or previous.get('version') != VERSION
            or not isinstance(previous.get('resources'), list)
            or len(previous['resources']) > 1 or not isinstance(previous.get('spec'), dict)):
        raise RouterError('IPv6 Fake-IP 管理记录无效，保留现有路由，请核对归属')
    owned = []
    for entry in previous['resources']:
        if (not isinstance(entry, dict) or not isinstance(entry.get('id'), str)
                or not isinstance(entry.get('config'), dict) or entry.get('path') != ROUTES):
            raise RouterError('IPv6 Fake-IP 管理记录缺少完整对象信息，保留现有路由')
        rows = [row for row in live['routes'] if str(row.get('.id')) == entry['id']]
        if len(rows) > 1:
            raise RouterError('IPv6 Fake-IP 路由对象 ID 不唯一，停止发布')
        if not rows:
            continue
        spec = previous['spec']
        if (str(_network(entry['config'].get('dst-address'))) != spec.get('cidr')
                or entry['config'].get('gateway') != spec.get('gateway')):
            raise RouterError('IPv6 Fake-IP 管理记录与记录中的路由配置不一致，保留现场，请核对')
        if stable(rows[0]) != entry['config']:
            raise RouterError('IPv6 Fake-IP 受管路由被外部修改，保留现场，请先核对')
        if yes(rows[0].get('dynamic')) or yes(rows[0].get('connect')):
            raise RouterError('IPv6 Fake-IP 管理记录指向动态路由，停止发布')
        owned.append(entry)
    return owned


def _target_rows(live, cidr, ignore=()):
    target = ipaddress.IPv6Network(cidr)
    return [row for row in live['routes'] if str(row.get('.id')) not in ignore
            and row.get('routing-table', 'main') == 'main'
            and (network := _network(row.get('dst-address'))) and network.version == 6
            and (network == target or (enabled(row) and network.prefixlen > target.prefixlen and network.overlaps(target)))]


def _correct(row, spec):
    return (_network(row.get('dst-address')) == ipaddress.IPv6Network(spec['cidr'])
            and row.get('routing-table', 'main') == 'main' and enabled(row) and yes(row.get('active'))
            and not any(yes(row.get(key)) for key in ('blackhole', 'unreachable', 'prohibit', 'invalid'))
            and _same_gateway(row, spec['gateway']))


def hairpin(driver, lan, cidr):
    """Warn when the forward chain drops the hairpin's later packets as invalid.

    Shared by IPv6 Fake-IP and IPv6 rule-set routes: both send LAN clients to
    Mihomo through the router's LAN port, so one same-port accept covers both.
    ICMPv6 redirects (sent only for an LLA next hop, about one per second,
    ignored by forwarding Linux clients) ease but do not remove the stall, so
    the next hop never silences this. The text is ``forward_hint``'s, shared
    with IPv4. Read-only: returns (warning, command).
    """
    from backend.routers.forward_hint import IPV6_DROP, check
    result = check(driver, 6, lan, [cidr])
    if result['status'] == 'unknown':
        return '未能读取 RouterOS IPv6 防火墙，无法确认转给 Mihomo 的连接是否会被 forward 链丢弃', ''
    if result['status'] != 'drop':
        return '', ''
    return IPV6_DROP, result['command']


def inspect(config, router, agent, host, driver):
    setting = settings(config)
    previous = router.get('ipv6_fakeip_state')
    driver.ipv6_fakeip_manifest = copy.deepcopy(previous)
    plan = {'enabled': setting['enabled'], 'status': 'disabled', 'items': [], 'blockers': [],
            'warnings': [], 'ops': [], 'check': {'spec': None}, 'evidence': {}, 'fix_command': ''}
    if setting['error']:
        plan['blockers'].append(setting['error'])
    try:
        live = snapshot(driver, setting['enabled'])
        plan['evidence'] = evidence(live)
        owned = owned_entries(previous, live)
    except RouterError as error:
        plan['blockers'].append(str(error))
        plan['status'] = 'unknown'
        return plan
    if plan['blockers']:
        plan['status'] = 'unknown'
        return plan
    spec = None
    borrowed = None
    if setting['enabled']:
        if not agent or agent.get('service_type') != 'mihomo' or agent.get('enabled') is False:
            plan['blockers'].append('请关联本配置空间中启用的 Mihomo 代理网关')
        elif host.get('error'):
            plan['blockers'].append('无法读取 Mihomo 的 IPv6 地址：' + str(host['error']))
        lan = _lan(live, (agent or {}).get('host', ''))
        existing = [row for row in live['routes']
                    if _network(row.get('dst-address')) == ipaddress.IPv6Network(setting['cidr'])]
        gateway = _gateway(host, live, lan, existing)
        if not gateway:
            plan['blockers'].append('未确认 Mihomo 唯一、稳定的同网段 IPv6 下一跳，请先检查设备 IPv6 网络')
        else:
            # Read-only hint, as for rule-set routes; the firewall stays user-owned.
            warning, plan['fix_command'] = hairpin(driver, lan, setting['cidr'])
            plan['warnings'] += [warning] if warning else []
        target = ipaddress.IPv6Network(setting['cidr'])
        if any((network := _network(row.get('address'))) and network.version == 6 and network.overlaps(target)
               for row in live['addresses']):
            plan['blockers'].append('IPv6 Fake-IP 与 ROS 的内网实际地址重叠，请先调整网段')
        spec = {'cidr': setting['cidr'], 'gateway': gateway or ''}
        candidates = _target_rows(live, spec['cidr'], {entry['id'] for entry in owned})
        if candidates:
            if len(candidates) == 1 and gateway and _correct(candidates[0], spec):
                borrowed = { 'id': str(candidates[0].get('.id') or ''), 'config': stable(candidates[0]) }
                if not borrowed['id'] or yes(candidates[0].get('dynamic')) or yes(candidates[0].get('connect')):
                    plan['blockers'].append('现有 IPv6 Fake-IP 路由不能确认为普通静态路由，请先核对')
            else:
                plan['blockers'].append('已有 IPv6 Fake-IP 路由错误、停用或与其他路由冲突；不会自动覆盖')
        good_owned = len(owned) == 1 and _correct(next(row for row in live['routes'] if str(row.get('.id')) == owned[0]['id']), spec)
        if owned and borrowed:
            plan['blockers'].append('受管路由与其他 IPv6 Fake-IP 路由重复，请先核对')
        if plan['blockers']:
            status, detail = 'conflict', '；'.join(plan['blockers'])
        elif good_owned:
            status, detail = 'synced', '对应路由已生效，由 ConfigFlow 管理'
        elif borrowed:
            status, detail = 'borrowed', '沿用已有正确路由，保留原归属；关闭开关时不会删除此路由'
        else:
            status, detail = 'missing', '发布时添加对应 IPv6 Fake-IP 路由，并登记为 ConfigFlow 受管对象'
        plan['items'].append({**spec, 'status': status, 'detail': detail})
        plan['status'] = status
        plan['check'] = {'spec': spec, 'borrowed': borrowed, 'previous': copy.deepcopy(previous)}
    else:
        good_owned = False
        plan['check'] = {'spec': None, 'removed': [entry['id'] for entry in owned]}
        if previous:
            plan['status'] = 'pending'
            plan['items'] = [{**(previous.get('spec') or {}), 'status': 'pending',
                              'detail': 'IPv6 Fake-IP 已关闭，发布时仅撤回本功能已核实归属的路由'}]
    if not plan['blockers'] and (previous or spec) and not good_owned and not borrowed:
        values = ({'dst-address': spec['cidr'], 'gateway': spec['gateway'], 'routing-table': 'main',
                   'distance': '1', 'disabled': 'false', 'comment': PREFIX + router['id']} if spec else None)
        plan['ops'] = [{'kind': 'ipv6_fakeip_replace', 'previous': previous, 'owned': owned,
                        'spec': spec, 'values': values, 'expected': plan['evidence'],
                        'summary': (f'补齐 IPv6 Fake-IP 路由 {spec["cidr"]} → {spec["gateway"]}' if spec
                                    else '撤回 ConfigFlow 受管 IPv6 Fake-IP 路由')}]
    return plan


def report(plan):
    return {key: copy.deepcopy(plan[key])
            for key in ('enabled', 'status', 'items', 'blockers', 'warnings', 'fix_command')}


def _rollback(driver, changes):
    remap = {}
    failures = []
    for change in changes:
        try:
            check = driver._perform(change, [], restoring=True)
            driver._verify_change(check)
            if change['method'] == 'RESTORE':
                remap[str(change['id'])] = check['id']
        except RouterError as error:
            failures.append(str(error))
    if failures:
        raise RouterRollbackError('IPv6 Fake-IP 路由回滚未完成：' + '；'.join(failures))
    return remap


def _remapped(previous, remap, driver):
    previous = copy.deepcopy(previous)
    if previous:
        for entry in previous['resources']:
            if entry['id'] in remap:
                entry['id'] = remap[entry['id']]
                entry['config'] = stable(driver._row(ROUTES, entry['id']))
    return previous


def apply(driver, op):
    live = snapshot(driver, bool(op.get('spec')))
    if evidence(live) != op.get('expected'):
        raise RouterError('IPv6 路由或内网地址在预览后变化，请重新预览')
    previous = op.get('previous')
    owned = owned_entries(previous, live)
    if owned != op.get('owned'):
        raise RouterError('IPv6 Fake-IP 对象归属已变化，请重新预览')
    undo = []
    resources = []
    try:
        for entry in owned:
            row = driver._row(ROUTES, entry['id'])
            if stable(row) != entry['config']:
                raise RouterError('IPv6 Fake-IP 受管路由在操作期间变化，保留现场')
            check = driver._perform({'method': 'DELETE', 'path': ROUTES, 'id': entry['id'],
                                     'expected': driver._ownership_snapshot(ROUTES, row)}, undo)
            driver._verify_change(check)
        if op.get('spec'):
            # A second writer may have appeared after deletion. Never create a
            # duplicate just because the preview used to be clear.
            if _target_rows({'routes': driver._list(ROUTES)}, op['spec']['cidr']):
                raise RouterError('IPv6 Fake-IP 目标已出现其他路由，停止添加')
            check = driver._perform({'method': 'PUT', 'path': ROUTES, 'values': op['values']}, undo)
            driver._verify_change(check)
            row = driver._row(ROUTES, check['id'])
            if not _correct(row, op['spec']):
                raise RouterError('新建 IPv6 Fake-IP 路由尚未生效，已停止发布')
            resources = [{'path': ROUTES, 'id': check['id'], 'config': stable(row)}]
        driver.ipv6_fakeip_manifest = ({'version': VERSION, 'spec': op['spec'], 'resources': resources}
                                       if op.get('spec') else None)
    except RouterError as error:
        try:
            remap = _rollback(driver, list(reversed(undo)))
        except RouterError as rollback_error:
            raise RouterRollbackError(f'{error}；{rollback_error}') from error
        driver.ipv6_fakeip_manifest = _remapped(previous, remap, driver)
        raise
    return {'kind': 'ipv6_fakeip_restore', 'changes': list(reversed(undo)), 'previous': previous}


def restore(driver, op):
    remap = _rollback(driver, op['changes'])
    driver.ipv6_fakeip_manifest = _remapped(op.get('previous'), remap, driver)
    return {}


def verify(driver, check):
    live = snapshot(driver, False)
    manifest = getattr(driver, 'ipv6_fakeip_manifest', None)
    owned = owned_entries(manifest, live)
    spec = check.get('spec')
    if not spec:
        if manifest or owned or any(str(row.get('.id')) in check.get('removed', []) for row in live['routes']):
            raise RouterError('回读校验失败：IPv6 Fake-IP 受管路由未按计划撤回')
        return
    rows = _target_rows(live, spec['cidr'])
    if len(rows) != 1 or not _correct(rows[0], spec):
        raise RouterError('回读校验失败：IPv6 Fake-IP 路由未生效或存在冲突')
    borrowed = check.get('borrowed')
    if borrowed:
        if str(rows[0].get('.id')) != borrowed['id'] or stable(rows[0]) != borrowed['config']:
            raise RouterError('回读校验失败：沿用的 IPv6 Fake-IP 路由已变化')
    elif len(owned) != 1 or str(rows[0].get('.id')) != owned[0]['id'] or (manifest or {}).get('spec') != spec:
        raise RouterError('回读校验失败：IPv6 Fake-IP 路由缺少匹配的 ConfigFlow 管理记录')
