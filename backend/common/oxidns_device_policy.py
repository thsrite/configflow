"""Profile-owned device rules, patched into the native draft without regeneration."""
from copy import deepcopy
import hashlib
import ipaddress
import json
import re

import yaml

from backend.common.oxidns_settings import OxidnsSettingsError

POLICY_TAG = 'cf_device_policy'
DIRECT_TAG = 'cf_device_direct'
HOOK = {'exec': f'jump {POLICY_TAG}'}
DIRECT = {'tag': DIRECT_TAG, 'type': 'sequence', 'args': [
    {'exec': '$forward_local'}, {'matches': ['!has_resp'], 'exec': 'reject 3'},
    {'exec': 'accept'},
]}


def normalize_client_range(raw):
    """Canonicalize hosts/CIDRs, including IPv4-mapped peers and host prefixes."""
    try:
        if not isinstance(raw, str):
            raise ValueError()
        parts = raw.strip().split('/')
        if len(parts) > 2 or not re.fullmatch(r'[0-9a-fA-F:.]+', parts[0]):
            raise ValueError()
        address = ipaddress.ip_address(parts[0])
        prefix = address.max_prefixlen
        if len(parts) == 2:
            if not re.fullmatch(r'[0-9]{1,3}', parts[1]):
                raise ValueError()
            prefix = int(parts[1])
        if not 0 <= prefix <= address.max_prefixlen:
            raise ValueError()
        # OxiDNS sees IPv4-mapped peers as the same IPv4 client.
        if isinstance(address, ipaddress.IPv6Address) and address.ipv4_mapped:
            if prefix < 96:
                raise ValueError()
            address, prefix = address.ipv4_mapped, prefix - 96
        network = ipaddress.ip_network(f'{address}/{prefix}', strict=False)
        if network.is_multicast:
            raise ValueError()
        if prefix == address.max_prefixlen:
            if address.is_unspecified:
                raise ValueError()
            return str(address)
        return str(network)
    except ValueError:
        raise OxidnsSettingsError('请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。') from None


def normalize_policies(value):
    if not isinstance(value, list) or len(value) > 512:
        raise OxidnsSettingsError('设备策略必须为列表，最多 512 条策略。')
    result, seen = [], set()
    for row in value:
        if not isinstance(row, dict) or set(row) - {'ip', 'name', 'mode'}:
            raise OxidnsSettingsError('设备策略仅支持 IP / 网段、备注和策略。')
        ip = normalize_client_range(row.get('ip'))
        if ip in seen:
            raise OxidnsSettingsError(f'客户端 IP / 网段重复：{ip}')
        seen.add(ip)
        name, mode = row.get('name', ''), row.get('mode')
        if not isinstance(name, str) or len(name) > 80 or any(ord(c) < 32 for c in name):
            raise OxidnsSettingsError('设备备注最多 80 个字符，不能包含换行或控制字符。')
        if mode not in ('smart', 'direct'):
            raise OxidnsSettingsError('设备策略必须为智能分流或直连。')
        # Older versions accepted DEL in names; drop it so the row stays editable.
        result.append({'ip': ip, 'name': name.replace('\x7f', '').strip(), 'mode': mode})
    return result


def revision(settings):
    return hashlib.sha256(json.dumps(settings, sort_keys=True, ensure_ascii=False,
                                    separators=(',', ':')).encode()).hexdigest()


def _policy_plugin(rows):
    direct = [row['ip'] for row in rows if row['mode'] == 'direct']
    if not direct:
        return None
    if any('/' in row['ip'] for row in rows):
        # More specific ranges (including individual clients) override broader ones.
        # A smart exception must return to the caller before a broader direct rule.
        def specificity(row):
            network = ipaddress.ip_network(row['ip'])
            return (-network.prefixlen, network.version, int(network.network_address))

        args = [{'matches': ['client_ip ' + row['ip']],
                 'exec': f'goto {DIRECT_TAG}' if row['mode'] == 'direct' else 'return'}
                for row in sorted(rows, key=specificity)]
        return {'tag': POLICY_TAG, 'type': 'sequence', 'args': args + [{'exec': 'return'}]}
    # Preserve the original host-only shape so existing drafts stay editable.
    return {'tag': POLICY_TAG, 'type': 'sequence', 'args': [
        {'matches': ['client_ip ' + ' '.join(direct)], 'exec': f'goto {DIRECT_TAG}'},
        {'exec': 'return'},
    ]}


def _refuse(reason, fix='请先在 OxiDNS 完整参数中调整'):
    raise OxidnsSettingsError(f'{reason}，无法安全插入设备策略；{fix}。')


def _check_main(document, plugins):
    """The hook is only placed in a recognizable sequence_main with a local upstream."""
    if document.get('include'):
        _refuse('配置用 include 引入了其他文件，看不到完整的处理链')
    items = document.get('plugins')
    if not plugins and isinstance(items, list) and items:
        seen = set()
        for index, item in enumerate(items, 1):
            tag = item.get('tag') if isinstance(item, dict) else None
            if not isinstance(tag, str):
                _refuse(f'第 {index} 个插件没有 tag')
            if tag in seen:
                _refuse(f'插件 tag「{tag}」重复')
            seen.add(tag)
    main = plugins.get('sequence_main')
    if main is None:
        _refuse('找不到主处理链 sequence_main')
    if main.get('type') != 'sequence':
        _refuse(f'主处理链 sequence_main 的类型是 {main.get("type")}，不是 sequence')
    args = main.get('args')
    if not isinstance(args, list) or not args:
        _refuse('主处理链 sequence_main 没有处理步骤')
    for index, item in enumerate(args, 1):
        if not isinstance(item, dict) or not isinstance(item.get('exec'), str):
            _refuse(f'主处理链 sequence_main 第 {index} 步没有 exec 动作')
    if args[-1] not in ({'exec': 'goto china_dns'}, {'exec': 'goto proxy_dns_seq'}):
        last = args[-1]['exec'] + ('（带匹配条件）' if args[-1].get('matches') else '')
        _refuse(f'主处理链 sequence_main 的最后一步是「{last}」，'
                '设备策略要求最后一步是不带条件的 goto china_dns 或 goto proxy_dns_seq')
    forward = plugins.get('forward_local')
    if forward is None:
        _refuse('找不到本地上游 forward_local，直连设备没有可用的上游')
    if forward.get('type') != 'forward':
        _refuse(f'本地上游 forward_local 的类型是 {forward.get("type")}，不是 forward')
    if not isinstance(forward.get('args'), dict) or not forward['args'].get('upstreams'):
        _refuse('本地上游 forward_local 没有配置上游地址')


def _listen_port(listen):
    """The listening port, or None when the core resolves the address later."""
    if not isinstance(listen, str) or '${' in listen:
        return None
    port = listen.strip().rpartition(':')[2]
    return int(port) if port.isdecimal() and 0 < int(port) <= 65535 else None


def _server_entries(plugin):
    """Executors a DNS server hands queries to, or None when they cannot be read."""
    args = plugin.get('args')
    if not isinstance(args, dict):
        return None
    if plugin.get('type') == 'http_server':
        items = args.get('entries')
        if not isinstance(items, list) or not items or any(not isinstance(item, dict) for item in items):
            return None
        values = [item.get('exec') for item in items]
    else:
        values = [args.get('entry')]
    return values if all(isinstance(value, str) and value for value in values) else None


def _reach(document, plugins, start):
    """Plugins a chain can run from its entries: (tags, unresolved reason or '')."""
    from backend.common.oxidns_references import reference_locations

    tags = list(plugins)
    seen, queue = set(), list(start)
    while queue:
        tag = queue.pop()
        if tag in seen:
            continue
        if '${' in tag:
            return seen, '含未展开的环境变量'
        if tag not in plugins:
            return seen, f'用到的插件 {tag} 不存在'
        seen.add(tag)
        if tag == 'sequence_main':
            continue  # Reaching it is the answer; its own graph is irrelevant here.
        for edge in reference_locations({'plugins': [plugins[tag]]}, tags):
            if edge['tag'] is None:
                return seen, '含未展开的环境变量'
            queue.append(edge['tag'])
    return seen, ''


def _endpoint(listen):
    host, _, port = listen.strip().rpartition(':')
    return host.strip('[]'), port


def entry_context(config):
    """What ConfigFlow itself knows about node-test DNS listeners of this profile.

    Read from the profile's compatibility view only (no I/O): the managed
    listener records and the addresses node tests are currently configured to
    query. Missing context means nothing is recognized, never that it is trusted.
    """
    if not isinstance(config, dict):
        return {}
    profile_id = config.get('profile_id')
    agents = [a for a in config.get('agents') or [] if isinstance(a, dict)]
    system = config.get('system_config') if isinstance(config.get('system_config'), dict) else {}
    values = list(system.get('node_test_dns') or []) if isinstance(system.get('node_test_dns'), list) else []
    saved = system.get('node_test_dns_config')
    if isinstance(saved, dict) and saved.get('mode') == 'custom' and isinstance(saved.get('servers'), list):
        values += saved['servers']
    addresses = set()
    for value in values:
        if not isinstance(value, str) or '://' in value:
            continue
        host, separator, port = value.strip().partition(':')
        try:
            ip = str(ipaddress.IPv4Address(host))
        except ValueError:
            continue
        addresses.add((ip, port if separator else '53'))
    hosts = {a.get('host') for a in agents if a.get('service_type') == 'oxidns'
             and a.get('profile_id', 'default') == profile_id and isinstance(a.get('host'), str)}
    return {'profile_id': profile_id, 'agents': agents, 'addresses': addresses, 'hosts': hosts}


def _legacy_node_test(document, plugins, servers, reach, listen, context):
    """A hand-made listener that node tests currently query, on an isolated chain.

    Both must hold: the saved node-test DNS points at exactly this listener,
    and the chain is its own (servers -> one sequence used by nobody else ->
    forwards used only by that sequence), so it cannot touch client traffic.
    """
    from backend.common.oxidns_references import reference_locations

    host, port = _endpoint(listen)
    if not any(port == want and (host == ip or host in ('', '0.0.0.0', '::') and ip in context['hosts'])
               for ip, want in context.get('addresses', ())):
        return False
    entries = {entry for _, values in servers for entry in values}
    if len(entries) != 1 or any(p.get('type') not in ('udp_server', 'tcp_server') for p, _ in servers):
        return False
    entry = next(iter(entries))
    if plugins[entry].get('type') != 'sequence':
        return False
    if any(plugins[tag].get('type') in ('sequence', 'fallback') for tag in reach - {entry}):
        return False
    names = {p['tag'] for p, _ in servers}

    def owners(tag):
        # A reference outside plugins (or an unreadable path) counts as foreign.
        return {document['plugins'][edge['path'][1]].get('tag')
                if edge['path'][:1] == ['plugins'] and len(edge['path']) > 1 else None
                for edge in reference_locations(document, [tag], dynamic=False)}

    if owners(entry) - names:
        return False
    forwards = [tag for tag in reach if plugins[tag].get('type') == 'forward']
    return bool(forwards) and all(owners(tag) <= {entry} for tag in forwards)


def inspect_entries(document, plugins, config=None):
    """Check every DNS listener; return side entries that bypass sequence_main.

    Listeners on a client DNS port (53, or a port the main chain listens on)
    must enter sequence_main. Other listeners may run their own isolated chain:
    device policies do not apply to them, so they are reported, except
    ConfigFlow's own node-test listeners, which are expected to bypass it.
    """
    servers = [p for p in plugins.values() if str(p.get('type', '')).endswith('_server')]
    if not servers:
        _refuse('没有任何 DNS 监听入口（udp_server / tcp_server 等）')
    groups, main_ports, has_main = {}, {53}, False
    for plugin in servers:
        listen = plugin['args'].get('listen') if isinstance(plugin.get('args'), dict) else None
        name = f'监听入口 {plugin["tag"]}' + (f'（{listen}）' if isinstance(listen, str) else '')
        entries = _server_entries(plugin)
        if entries is None:
            _refuse(f'{name} 的 entry 无法读取')
        if all(entry == 'sequence_main' for entry in entries):
            has_main = True
            if _listen_port(listen) is not None:
                main_ports.add(_listen_port(listen))
        else:
            groups.setdefault((str(listen), tuple(entries)), []).append((plugin, entries))
    if not has_main:
        _refuse('没有任何监听入口进入主处理链 sequence_main')
    context = entry_context(config) if groups else {}
    managed = set()
    if context.get('agents'):
        from backend.agents.node_test_dns import recorded_listeners
        managed = recorded_listeners(plugins, context['agents'], context['profile_id'])
    side, node_test = [], []
    for (listen, entries), members in groups.items():
        tags = [plugin['tag'] for plugin, _ in members]
        item = {'tags': tags, 'listen': listen, 'entry': '、'.join(dict.fromkeys(entries))}
        if set(tags) <= managed:
            node_test.append({**item, 'kind': 'managed'})
            continue
        name = f'监听入口 {"、".join(tags)}（{listen}）'
        port = _listen_port(listen)
        if port is None or port in main_ports:
            port_text = '无法确认监听端口' if port is None else f'监听客户端 DNS 端口 {port}'
            _refuse(f'{name}{port_text}，但 entry 是 {item["entry"]}，没有进入主处理链 sequence_main'
                    '（经这里查询的客户端不受设备策略控制）',
                    '请在 OxiDNS 完整参数中把它的 entry 改为 sequence_main，或让它改用其他端口')
        reach, problem = _reach(document, plugins, entries)
        if problem:
            _refuse(f'{name} 的处理链{problem}，无法确认它是否经过主处理链')
        if 'sequence_main' in reach:
            _refuse(f'{name} 的处理链 {item["entry"]} 会间接调用主处理链 sequence_main，看不出设备策略是否生效',
                    '请在 OxiDNS 完整参数中把它的 entry 直接改为 sequence_main，或让它不再调用主处理链')
        if _legacy_node_test(document, plugins, members, reach, listen, context):
            node_test.append({**item, 'kind': 'legacy'})
        else:
            side.append(item)
    return {'side_entries': side, 'node_test_entries': node_test}


def _patch(document, previous, rows, config=None):
    from backend.common.oxidns_common_routing import _plugins, _prefix_end

    document = deepcopy(document)
    plugins = _plugins(document)
    _check_main(document, plugins)
    report = inspect_entries(document, plugins, config)
    args = plugins['sequence_main']['args']

    old = _policy_plugin(previous)
    owned = {tag: plugins[tag] for tag in (POLICY_TAG, DIRECT_TAG) if tag in plugins}
    hooks = [i for i, item in enumerate(args) if item == HOOK]
    if old:
        if owned != {POLICY_TAG: old, DIRECT_TAG: DIRECT} or len(hooks) != 1:
            raise OxidnsSettingsError('设备策略执行链已被手动修改，请先在完整参数中恢复，避免覆盖自定义内容。')
        index = hooks[0]
        args.pop(index)
        if index != _prefix_end(args):
            raise OxidnsSettingsError('设备策略的位置已被修改，请先恢复到分流规则之前。')
    elif owned or hooks:
        raise OxidnsSettingsError('完整参数中存在同名设备策略插件，请先处理命名冲突。')
    # Extra calls to owned plugins could turn a per-client rule into a global rule.
    remaining = [p for p in document['plugins'] if p.get('tag') not in (POLICY_TAG, DIRECT_TAG)]
    text = json.dumps(remaining)
    if POLICY_TAG in text or DIRECT_TAG in text:
        raise OxidnsSettingsError('设备策略插件存在额外引用，请先在完整参数中处理。')
    document['plugins'] = remaining
    new = _policy_plugin(rows)
    if new:
        args.insert(_prefix_end(args), deepcopy(HOOK))
        document['plugins'].extend([new, deepcopy(DIRECT)])
    return document, report


def patch_document(document, previous, rows, config=None):
    """Own exactly two plugins and one hook in sequence_main; refuse altered or ambiguous chains.

    Side listeners with their own chain are never modified.
    """
    return _patch(document, previous, rows, config)[0]


def read_policies(settings, config):
    from backend.common.oxidns_editor import prepare_editor
    from backend.common.oxidns_native import load_native_yaml

    from backend.common.device_notes import stored_names

    names = stored_names(config)
    rows = [{**row, 'name': names.get(row['ip'], row['name'])}
            for row in normalize_policies(settings.get('device_policies', []))]
    try:
        current = prepare_editor(settings, config)
        report = _patch(load_native_yaml(current['native_config']), rows, rows, config)[1]
        return {'policies': rows, 'editable': True, 'reason': '', 'side_entries': report['side_entries']}
    except OxidnsSettingsError as exc:
        return {'policies': rows, 'editable': False, 'reason': str(exc), 'side_entries': []}


def apply_policies(settings, config, rows, base_url=''):
    from backend.common.oxidns_editor import prepare_editor
    from backend.common.oxidns_native import load_native_yaml

    rows = normalize_policies(rows)
    current = prepare_editor(settings, config, base_url)
    previous = normalize_policies(current.get('device_policies', []))
    document = patch_document(load_native_yaml(current['native_config']), previous, rows, config)
    current['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    current['device_policies'] = rows
    if isinstance(current.get('template_settings'), dict):
        current['template_settings']['device_policies'] = deepcopy(rows)
    return current
