"""Profile-local Mihomo dialer metadata. Names are resolved only at emission."""
from backend.utils.strategy_references import StrategyReferenceError


class DialerReferenceError(StrategyReferenceError):
    pass


def validate_shapes(profile, *, require_ids=True):
    """Validate metadata before attribute access, hashing or string parsing."""
    for key in ('nodes', 'proxy_groups'):
        items = profile.get(key, [])
        if not isinstance(items, list):
            raise DialerReferenceError(f'{key} 必须是数组')
        for obj in items:
            if not isinstance(obj, dict):
                raise DialerReferenceError('资源必须是 JSON 对象')
            for field in ('id', 'name'):
                # Legacy subscription caches may omit generated resource IDs.
                if field == 'id' and not require_ids and field not in obj:
                    continue
                value = obj.get(field)
                if not isinstance(value, str) or not value.strip():
                    raise DialerReferenceError(f'资源 {field} 必须是非空字符串')
            if key == 'nodes':
                if not isinstance(obj.get('params', {}), dict):
                    raise DialerReferenceError('节点 params 必须是对象，不能为 null')
                if obj.get('proxy_string') is not None and not isinstance(obj['proxy_string'], str):
                    raise DialerReferenceError('节点 proxy_string 必须是字符串')
            else:
                for field in ('manual_nodes', 'include_groups', 'proxies', 'subscriptions', 'aggregations', 'use'):
                    if field in obj and (not isinstance(obj[field], list) or
                            any(not isinstance(v, str) or not v.strip() for v in obj[field])):
                        raise DialerReferenceError(f'策略组 {field} 必须是字符串数组')
                order = obj.get('proxies_order', [])
                if not isinstance(order, list) or any(not isinstance(i, dict) or
                        not isinstance(i.get('type'), str) or not isinstance(i.get('id'), str)
                        for i in order):
                    raise DialerReferenceError('策略组 proxies_order 必须是含 type/id 字符串的对象数组')


def validate_dialers(profile):
    validate_shapes(profile)
    from backend.utils.proxy_chains import validate_proxy_chains
    validate_proxy_chains(profile)
    nodes = profile.get('nodes', [])
    if (any(g.get('type') == 'chain' for g in profile.get('proxy_groups', [])) or
            any(n.get('dialer_ref') is not None or raw_dialer(n) is not None for n in nodes)):
        names = []
        for items in (nodes, profile.get('proxy_groups', [])):
            ids = [obj.get('id') for obj in items]
            if len(set(ids)) != len(ids):
                raise DialerReferenceError('拨号引用要求资源 ID 唯一')
            names.extend(obj.get('name') for obj in items if obj.get('enabled', True))
        if (any(not isinstance(name, str) or not name.strip() for name in names) or
                len(set(names)) != len(names) or
                any(name in {'DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'PASS-RULE', 'COMPATIBLE'} for name in names)):
            raise DialerReferenceError('拨号代理要求代理/策略组名称非空、唯一且不与内置策略冲突')
    for node in nodes:
        ref = node.get('dialer_ref')
        if ref is None:
            continue
        if (not isinstance(ref, dict) or set(ref) != {'type', 'id'} or
                ref.get('type') not in ('node', 'group') or
                not isinstance(ref.get('id'), str) or not ref['id'].strip()):
            raise DialerReferenceError('dialer_ref 必须为 {type: node|group, id: 非空字符串}')
        targets = profile.get('nodes' if ref['type'] == 'node' else 'proxy_groups', [])
        target = next((t for t in targets if t.get('id') == ref['id']), None)
        if target is None or not target.get('enabled', True):
            raise DialerReferenceError('拨号引用不存在、未启用或不在当前配置空间')
        if ref['type'] == 'node' and target.get('id') == node.get('id'):
            raise DialerReferenceError('节点不能引用自身作为拨号代理')


    graph = dependency_graph(profile)
    # Managed references have a manual-only contract. Legacy raw name references
    # remain a separate compatibility mode and are not rewritten into managed refs.
    objects = {('node', n['id']): n for n in nodes}
    objects.update({('group', g['id']): g for g in profile.get('proxy_groups', [])})
    objects.update({('subscription', s['id']): s for s in profile.get('subscriptions', [])})
    objects.update({('aggregation', a['id']): a for a in profile.get('subscription_aggregations', [])})
    pending = [('node', n['id']) for n in nodes if n.get('dialer_ref') is not None]
    pending.extend(('group', g['id']) for g in profile.get('proxy_groups', [])
                   if g.get('type') == 'chain')
    visited = set()
    roots = set(pending)
    while pending:
        key = pending.pop()
        if key in visited or key[0] == 'builtin':
            continue
        visited.add(key)
        obj = objects.get(key)
        if obj is None:
            raise DialerReferenceError('拨号依赖不存在')
        if key[0] == 'node' and obj.get('subscription_id'):
            raise DialerReferenceError('稳定拨号引用仅支持手动节点，不能使用订阅节点')
        if not obj.get('enabled', True) and key not in roots:
            raise DialerReferenceError('拨号依赖不存在或未启用')
        if key in graph:
            pending.extend(graph[key])
        elif key[0] == 'node':
            # Disabled managed sources still retain the manual-only metadata
            # contract, although they are not roots of the emitted topology.
            target = dialer_target(profile, obj)
            if target:
                pending.append(target)
        elif obj.get('type') == 'chain':
            ref = obj['chain']['entry']
            pending.append((ref['type'], ref['id']))
        elif key[0] == 'group':
            pending.extend(group_dependencies(profile, obj))


def group_members(group):
    if group.get('proxies_order'):
        members = [(('node' if i.get('type') == 'node' else 'group'), i.get('id'))
                   for i in group['proxies_order'] if i.get('type') in ('node', 'strategy')]
    else:
        members = [('node', id) for id in group.get('manual_nodes', [])]
        members += [('group', id) for id in group.get('include_groups', [])]
        if not members and group.get('source') in ('node', 'strategy'):
            members = [('node' if group['source'] == 'node' else 'group', id) for id in group.get('proxies', [])]
    return [('builtin' if kind == 'node' and id in ('DIRECT', 'REJECT') else kind, id)
            for kind, id in members]


def group_dependencies(profile, group):
    """Logical sources include providers; actual remote edges are checked at delivery."""
    if group.get('include_all') or group.get('include-all'):
        raise DialerReferenceError('拨号拓扑不支持 include-all 隐式全局来源，请显式选择来源')
    if group.get('follow_group'):
        return [('group', group['follow_group'])]
    children = group_members(group)
    subscriptions = set(group.get('subscriptions', []))
    aggregations = set(group.get('aggregations', []))
    aggregations.update(i['id'] for i in group.get('proxies_order', []) if i['type'] == 'aggregation')
    subscriptions.update(i['id'] for i in group.get('proxies_order', []) if i['type'] == 'subscription')
    for name in group.get('use', []):
        matches = [(kind, obj['id']) for kind, key in
                   (('subscription', 'subscriptions'), ('aggregation', 'subscription_aggregations'))
                   for obj in profile.get(key, []) if obj.get('name') == name and obj.get('enabled', True)]
        if len(matches) != 1:
            raise DialerReferenceError('策略组 use 引用不存在或名称不唯一')
        (subscriptions if matches[0][0] == 'subscription' else aggregations).add(matches[0][1])
    for kind, ids, key in (('subscription', subscriptions, 'subscriptions'),
                           ('aggregation', aggregations, 'subscription_aggregations')):
        catalog = {o['id']: o for o in profile.get(key, [])}
        for id in sorted(ids):
            if id not in catalog or not catalog[id].get('enabled', True):
                raise DialerReferenceError('拨号策略组来源不存在或未启用')
            children.append((kind, id))
    if not children:
        raise DialerReferenceError('拨号策略组不能为空')
    return children


def raw_dialer(node):
    # Never contact Sub-Store while validating a write. URI results are checked
    # again after conversion, before artifacts or agent requests are produced.
    from backend.converters.mihomo import _parse_structured_proxy_string
    if node.get('proxy_string'):
        parsed = _parse_structured_proxy_string(node['proxy_string'])
        return parsed.get('dialer-proxy') if parsed else None
    return node.get('params', {}).get('dialer-proxy')


def dialer_target(profile, node):
    ref = node.get('dialer_ref')
    if ref is not None:
        return (ref['type'], ref['id'])
    name = raw_dialer(node)
    if name is None:
        return None
    if not isinstance(name, str) or not name.strip():
        raise DialerReferenceError('原始 dialer-proxy 必须是非空名称')
    if name in ('DIRECT', 'REJECT'):
        return ('builtin', name)
    matches = [(kind, obj['id']) for kind, items in
               (('node', profile.get('nodes', [])), ('group', profile.get('proxy_groups', [])))
               for obj in items if obj.get('name') == name and obj.get('enabled', True)]
    if len(matches) != 1:
        raise DialerReferenceError(f"原始 dialer-proxy '{name}' 不存在、未启用或名称不唯一，请改用稳定引用")
    return matches[0]


def chain_exit_dependencies(profile, ref, objects):
    """Exit candidate dialers are replaced; only nested chain structure is shared."""
    graph, dependencies = {}, []
    pending = [(ref['type'], ref['id'])]
    while pending:
        key = pending.pop()
        if key in graph:
            continue
        if key[0] == 'builtin':
            raise DialerReferenceError('代理链落地组不能包含 DIRECT/REJECT 等无法施加前置代理的候选')
        obj = objects.get(key)
        if not obj or not obj.get('enabled', True):
            raise DialerReferenceError('代理链落地候选不存在或未启用')
        if key[0] == 'node':
            if obj.get('subscription_id'):
                raise DialerReferenceError('订阅节点请通过策略组 Provider 使用')
            graph[key] = []
        elif key[0] == 'group' and obj.get('type') == 'chain':
            dependencies.append(key)
            graph[key] = []
        elif key[0] == 'group':
            graph[key] = group_dependencies(profile, obj)
        elif key[0] == 'aggregation':
            graph[key] = [('node', id) for id in obj.get('nodes', [])] + [('subscription', id) for id in obj.get('subscriptions', [])]
            if not graph[key]:
                raise DialerReferenceError('代理链落地聚合不能为空')
        else:
            graph[key] = []
        pending.extend(graph[key])
    check_cycles(graph)
    return dependencies


def dependency_graph(profile):
    validate_shapes(profile)
    from backend.utils.proxy_chains import validate_proxy_chains
    validate_proxy_chains(profile)
    objects = {('node', n['id']): n for n in profile.get('nodes', [])}
    objects.update({('group', g['id']): g for g in profile.get('proxy_groups', [])})
    objects.update({('subscription', s['id']): s for s in profile.get('subscriptions', [])})
    objects.update({('aggregation', a['id']): a for a in profile.get('subscription_aggregations', [])})
    graph = {}
    pending = [key for key, obj in objects.items() if obj.get('enabled', True) and
               ((key[0] == 'node' and (obj.get('dialer_ref') is not None or raw_dialer(obj) is not None)) or
                (key[0] == 'group' and obj.get('type') == 'chain'))]
    while pending:
        key = pending.pop()
        if key in graph or key[0] == 'builtin':
            continue
        obj = objects.get(key)
        if not obj or not obj.get('enabled', True):
            raise DialerReferenceError('拨号依赖不存在或未启用')
        if key[0] == 'group' and obj.get('type') == 'chain':
            entry = obj['chain']['entry']
            children = [(entry['type'], entry['id'])]
            children += chain_exit_dependencies(profile, obj['chain']['exit'], objects)
        elif key[0] == 'group':
            children = group_dependencies(profile, obj)
        elif key[0] == 'subscription':
            children = []
        elif key[0] == 'aggregation':
            children = [('node', id) for id in obj.get('nodes', [])]
            children += [('subscription', id) for id in obj.get('subscriptions', [])]
            if not children:
                raise DialerReferenceError('拨号策略组聚合来源不能为空')
        else:
            target = dialer_target(profile, obj)
            children = [target] if target else []
        graph[key] = children
        pending.extend(children)
    check_cycles(graph)
    return graph


def check_cycles(graph):
    """Iterative DFS with explicit enter/exit frames; no process recursion limit."""
    visiting, done = set(), set()
    for root in graph:
        pending = [(root, False)]
        while pending:
            key, exiting = pending.pop()
            if exiting:
                visiting.remove(key)
                done.add(key)
                continue
            if key in visiting:
                raise DialerReferenceError('拨号代理依赖存在循环')
            if key in done:
                continue
            visiting.add(key)
            pending.append((key, True))
            pending.extend((child, False) for child in reversed(graph.get(key, [])))


def incoming_dialers(profile, type, id):
    """Return logical dependents, including inactive metadata retained by users."""
    target = (type, id)
    direct = [('node', n['id']) for n in profile.get('nodes', [])
              if n.get('dialer_ref') == {'type': type, 'id': id}]
    for group in profile.get('proxy_groups', []):
        if group.get('type') == 'chain':
            chain = group['chain']
            matches = any(ref == {'type': type, 'id': id} for ref in chain.values())
        else:
            matches = (target in group_members(group) or
                       (type == 'group' and (id in group.get('include_groups', []) or
                                            group.get('follow_group') == id)))
        if matches:
            direct.append(('group', group['id']))
    graph = dependency_graph(profile)
    return list(dict.fromkeys(direct + [key for key, children in graph.items() if target in children]))


def resolve_dialer(profile, node):
    ref = node.get('dialer_ref')
    if ref is None:
        return None
    targets = profile.get('nodes' if ref['type'] == 'node' else 'proxy_groups', [])
    return next(t['name'] for t in targets if t.get('id') == ref['id'])


def overlay_dialer(profile, node, proxy):
    if not proxy and ('node', node.get('id')) in dependency_graph(profile):
        raise DialerReferenceError('拨号拓扑参与节点转换失败，不能省略节点')
    if proxy and node.get('dialer_ref') is not None:
        if node.get('_proxy_chain_id'):
            proxy['name'] = node['name']
        proxy['dialer-proxy'] = resolve_dialer(profile, node)
    return proxy


def filter_provider_candidates(proxies, include='', exclude='', exclude_type=''):
    """Match original names, preserving Mihomo's backtick filter ordering."""
    import re
    try:
        exclusions = [re.compile(value) for value in exclude.split('`') if value]
        filters = [re.compile(value) for value in include.split('`')]
    except re.error as exc:
        raise DialerReferenceError('Provider 正则过滤无效或不受支持') from exc
    selected, seen = [], set()
    excluded_types = {value.lower() for value in exclude_type.split('|') if value}
    for pattern in filters:
        for proxy in proxies:
            name = proxy['name']
            if proxy.get('type', '').lower() in excluded_types:
                continue
            if name not in seen and pattern.search(name) and not any(p.search(name) for p in exclusions):
                selected.append(proxy)
                seen.add(name)
    return selected


def provider_proxy_name(proxy, override):
    """Project the native name overrides emitted by the chain compiler."""
    import re
    name = proxy['name']
    rules = override.get('proxy-name', [])
    if rules and name.startswith(rules[0]['target'].rsplit('_', 2)[0] + '_'):
        raise DialerReferenceError('Provider 原始节点名称与代理链私有排序标记冲突')
    try:
        for rule in override.get('proxy-name', []):
            def replacement(match):
                return re.sub(r'\$(\d+)', lambda part: match.group(int(part[1])) or '', rule['target'])
            name = re.sub(rule['pattern'], replacement, name)
    except (re.error, IndexError) as exc:
        raise DialerReferenceError('Provider 名称转换规则无效') from exc
    return override.get('additional-prefix', '') + name + override.get('additional-suffix', '')


def validate_emitted(config, *, force_chains=False, provider_proxies=None, require_providers=False):
    objects = config.get('proxies', []) + config.get('proxy-groups', [])
    definitions = config.get('proxy-providers', {})
    payloads = provider_proxies or {}
    chains_active = (force_chains or any(obj.get('dialer-proxy') is not None for obj in objects) or
                     any(p.get('override', {}).get('dialer-proxy') for p in definitions.values()) or
                     any(p.get('dialer-proxy') is not None for ps in payloads.values() for p in ps))
    if not chains_active:
        return
    names = [obj.get('name') for obj in objects]
    builtins = {'DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'PASS-RULE', 'COMPATIBLE'}
    if len(set(names)) != len(names) or any(not n or n in builtins for n in names):
        raise DialerReferenceError('生成的代理/策略组名称重复或与内置策略冲突')
    available = set(names) | builtins
    graph = {}
    candidates = {}
    for name, definition in definitions.items():
        if name not in payloads:
            if require_providers:
                raise DialerReferenceError('拨号拓扑缺少已验证的 Provider 内容')
            continue
        proxies = payloads[name]
        raw_names = [p['name'] for p in proxies]
        if len(set(raw_names)) != len(raw_names):
            raise DialerReferenceError('Provider 节点名称重复')
        proxies = filter_provider_candidates(proxies, definition.get('filter', ''),
                                             definition.get('exclude-filter', ''), definition.get('exclude-type', ''))
        if not proxies:
            raise DialerReferenceError('拨号拓扑 Provider 过滤后不能为空')
        override = definition.get('override', {})
        candidates[name] = []
        for proxy in proxies:
            emitted_name = provider_proxy_name(proxy, override)
            key = ('provider', name, emitted_name)
            if emitted_name in builtins or (emitted_name in available and override.get('additional-prefix')):
                raise DialerReferenceError('Provider 节点名称与代理拓扑冲突')
            target = override.get('dialer-proxy', definition.get('dialer-proxy', proxy.get('dialer-proxy')))
            if target is not None and (not isinstance(target, str) or target not in available):
                raise DialerReferenceError(
                    f'Provider 节点「{emitted_name}」的 dialer-proxy「{target}」引用了不存在的代理或策略组；'
                    '若它是机场订阅自带的中转分组，在「订阅来源」刷新该订阅后 ConfigFlow 会自动补上')
            graph[key] = [target] if target is not None else []
            candidates[name].append({'name': emitted_name, 'key': key, 'type': proxy.get('type', '')})
    groups = {g['name']: g for g in config.get('proxy-groups', [])}
    by_name = {o['name']: o for o in objects}
    for obj in objects:
        children = obj.get('proxies', [])[:]
        if len(children) != len(set(children)):
            raise DialerReferenceError('策略组候选名称重复')
        if any(n not in available for n in children):
            raise DialerReferenceError('拨号拓扑中的策略组引用了未生成的代理或策略组')
        if obj['name'] in groups:
            static = [by_name.get(name, {'name': name, 'type': name}) for name in children]
            children = [p['name'] for p in filter_provider_candidates(static,
                exclude=obj.get('exclude-filter', ''), exclude_type=obj.get('exclude-type', ''))]
        candidate_names = set(children)
        target = obj.get('dialer-proxy')
        if target is not None:
            if not isinstance(target, str) or target not in available:
                raise DialerReferenceError('生成的 dialer-proxy 引用了不存在的代理或策略组')
            children.append(target)
        for provider in obj.get('use', []):
            if provider not in definitions:
                raise DialerReferenceError('策略组引用了未生成的 Provider')
            selected = filter_provider_candidates(candidates.get(provider, []), obj.get('filter', ''),
                                                  obj.get('exclude-filter', ''), obj.get('exclude-type', ''))
            for candidate in selected:
                if candidate['name'] in candidate_names:
                    raise DialerReferenceError('策略组候选名称重复')
                candidate_names.add(candidate['name'])
                children.append(candidate['key'])
        graph[obj['name']] = children
    check_cycles(graph)
    # Empty group auto-DIRECT must never be a dialer or an independently lowered exit.
    roots = [o['dialer-proxy'] for o in objects if o.get('dialer-proxy')]
    roots += [p['override']['dialer-proxy'] for p in definitions.values() if p.get('override', {}).get('dialer-proxy')]
    roots += list(groups) if require_providers else []
    pending, visited = roots, set()
    while pending:
        key = pending.pop()
        if key in visited:
            continue
        visited.add(key)
        if key in groups and not graph[key] and (require_providers or not groups[key].get('use')):
            raise DialerReferenceError('拨号策略组过滤后没有可用候选')
        pending.extend(graph.get(key, []))


def reject_surge_dialers(profile, client='Surge'):
    if any(g.get('type') == 'chain' and g.get('enabled', True) for g in profile.get('proxy_groups', [])):
        raise DialerReferenceError(f'{client} 暂不支持代理链，请使用 Mihomo 或禁用代理链')
    from backend.converters.mihomo import convert_node_to_mihomo
    for node in profile.get('nodes', []):
        if not node.get('enabled', True):
            continue
        if node.get('dialer_ref') is not None or raw_dialer(node) is not None:
            raise DialerReferenceError(f'{client} 暂不支持 Mihomo 拨号代理拓扑，请使用 Mihomo 或显式移除拨号配置')
        if node.get('proxy_string'):
            proxy = convert_node_to_mihomo(node)
            if proxy and proxy.get('dialer-proxy') is not None:
                raise DialerReferenceError(f'{client} 暂不支持转换结果中的 dialer-proxy')


def dependency_node_ids(profile, selected, include_roots=True):
    """Resolve node/group dependency closure from manual node roots."""
    graph = dependency_graph(profile)
    roots = [('node', id) for id in selected]
    pending = roots if include_roots else [child for key in roots for child in graph.get(key, [])]
    selected = set(selected) if include_roots else set()
    visited = set()
    while pending:
        key = pending.pop()
        if key in visited:
            continue
        visited.add(key)
        if key[0] == 'node':
            selected.add(key[1])
        pending.extend(graph.get(key, []))
    return selected
