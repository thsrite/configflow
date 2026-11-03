"""Chain targets that subscription nodes bring from the airport's own configuration.

An airport may publish a full Mihomo configuration in which a node says
``dialer-proxy: Relay`` and ``Relay`` is one of the airport's own, often hidden,
groups. ConfigFlow keeps only the nodes of a subscription, so that name would
not exist in the generated configuration and Mihomo looks dialer names up among
top-level proxies and groups only.

* ``extract_subscription_dialers`` records, from a downloaded body, the airport
  groups such references name and the airport proxies each finally uses. It is
  stored with the subscription cache next to its hosts and replaced on every
  download, so renamed relays follow the airport. ``dialer_record`` reads it,
  or extracts it from the last downloaded body for caches made before.
* Generation (``add_dialer_groups``) adds each needed group as a hidden group
  named ``<subscription>/<group>``. It picks the relays from the subscription's
  provider by exact name; when the subscription filter removed one of them, or
  the nodes are delivered only through an aggregation, the relays are copied
  as ``<subscription>/<relay>`` proxies instead.
* Every delivery of the subscription's nodes (``adapt_subscription_proxies``)
  points those ``dialer-proxy`` values at that group. A node whose airport group
  cannot be provided is left out, and generation reports it, instead of failing
  the whole publication: the airport's configuration is not the user's to fix.

Only references to the airport's own groups are handled here. A name that is
a proxy (also of another subscription) or that the airport does not define is
left as it is for the regular dialer validation, which accepts or rejects it.
"""
import logging
import re

import yaml

logger = logging.getLogger(__name__)

MAX_TARGETS = 50
MAX_MEMBERS = 100
MAX_REPORTED = 20
BUILTINS = frozenset({'DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'PASS-RULE', 'COMPATIBLE'})
_GROUP_TYPES = ('select', 'url-test', 'fallback', 'load-balance')
# Health-check and selection settings worth keeping from the airport's group.
_GROUP_KEYS = ('url', 'interval', 'tolerance', 'lazy', 'timeout', 'expected-status', 'strategy', 'max-failed-times')


def group_name(subscription, target):
    return f'{subscription}/{target}'


def _load(text):
    loader = getattr(yaml, 'CSafeLoader', None)
    if loader is not None:
        try:
            return yaml.load(text, Loader=loader)
        except Exception:  # libyaml differs on edge cases; PyYAML decides
            pass
    return yaml.safe_load(text)


def extract_subscription_dialers(text):
    """The airport groups the body's own ``dialer-proxy`` values name, or None without any.

    Returns ``{'targets': {group: {'type', 'members', ...settings}}, 'unresolved': [group]}``.
    Members are the airport proxies a group finally uses, nested groups expanded.
    A group is unresolved when it selects its proxies only dynamically
    (``use``, ``include-all``, ``filter``).
    """
    if not isinstance(text, str) or 'dialer-proxy' not in text:
        return None
    try:
        document = _load(text)
    except (yaml.YAMLError, RecursionError):
        return None
    if not isinstance(document, dict):
        return None
    listed = [item for item in document.get('proxies') or [] if isinstance(item, dict)]
    proxies = {item['name'] for item in listed if isinstance(item.get('name'), str)}
    groups = {item['name']: item for item in document.get('proxy-groups') or []
              if isinstance(item, dict) and isinstance(item.get('name'), str) and item['name'] not in proxies}
    # Malformed values are left to the regular validation, which rejects them.
    wanted = sorted({item['dialer-proxy'] for item in listed
                     if isinstance(item.get('dialer-proxy'), str) and item['dialer-proxy'] in groups})
    if not wanted:
        return None

    def leaves(name, seen):
        if name in proxies:
            return [name]
        group = groups.get(name)
        if group is None or name in seen:
            return []
        found = []
        for member in group.get('proxies') or []:
            if isinstance(member, str):
                found.extend(leaves(member, seen | {name}))
        return found

    targets, unresolved = {}, list(wanted[MAX_TARGETS:])
    for target in wanted[:MAX_TARGETS]:
        members = list(dict.fromkeys(leaves(target, frozenset())))[:MAX_MEMBERS]
        if not members:
            unresolved.append(target)
            continue
        source = groups[target]
        entry = {'type': source.get('type') if source.get('type') in _GROUP_TYPES else 'select', 'members': members}
        entry.update({key: source[key] for key in _GROUP_KEYS
                      if isinstance(source.get(key), (str, int, float, bool))})
        targets[target] = entry
    return {'targets': targets, 'unresolved': unresolved}


def dialer_record(sub, cache=None):
    """The recorded airport groups of ``sub``, or None when it has no chained nodes or no record yet."""
    from backend.utils.subscription_cache import load_subscription_cache
    cache = load_subscription_cache(sub['id']) if cache is None else cache
    metadata = (cache or {}).get('metadata') or {}
    if 'dialers' in metadata:
        return metadata['dialers']
    if not sub.get('url'):
        return None
    # A cache from before this record: the last downloaded body still has the groups.
    from backend.common.config import get_repository
    from backend.utils.subscription_refresh import _download_cache_path
    try:
        downloaded = get_repository().read_shared_json(_download_cache_path(sub['url']))
    except Exception:
        return None
    text = downloaded.get('text') if isinstance(downloaded, dict) else None
    return extract_subscription_dialers(text)


def _airport_groups(record):
    return set((record or {}).get('targets') or {}) | set((record or {}).get('unresolved') or [])


def _name_pattern(name):
    # Backticks separate Mihomo filter alternatives; keep a literal one literal.
    return re.escape(name).replace('`', r'\x60')


def _node_dialer(node):
    from backend.utils.dialer_references import raw_dialer
    try:
        target = raw_dialer(node)
    except Exception:
        return None
    return target if isinstance(target, str) and target not in BUILTINS else None


def add_dialer_groups(main, config):
    """Add the groups the emitted subscriptions' chained nodes need; return a report or None.

    Raises ``DialerReferenceError`` when a name ConfigFlow must create is already used,
    because a delivery would otherwise point those nodes at the user's object.
    """
    from backend.converters.mihomo import convert_node_to_mihomo
    from backend.utils.dialer_references import DialerReferenceError
    from backend.utils.subscription_cache import load_subscription_cache
    from backend.utils.subscription_filter import filter_subscription_items
    from backend.utils.subscription_hosts import _provider_sources

    subscriptions = {item['id']: item for item in config.get('subscriptions', []) if item.get('id')}
    aggregations = {item['id']: item for item in config.get('subscription_aggregations', []) if item.get('id')}
    direct, emitted = set(), []
    for kind, ident, _query in _provider_sources(main, config):
        members = [ident] if kind == 'subscription' else aggregations[ident].get('subscriptions', [])
        if kind == 'subscription':
            direct.add(ident)
        emitted.extend(member for member in members if member in subscriptions and member not in emitted)
    caches = {sub_id: load_subscription_cache(sub_id) or {} for sub_id in emitted
              if subscriptions[sub_id].get('enabled', True)}
    if not caches:
        return None
    # Node names any emitted subscription defines: a reference to one is not the airport's group.
    # Malformed nodes are left to the regular shape validation of the delivery.
    node_names = {node['name'] for cache in caches.values() for node in cache.get('nodes') or []
                  if isinstance(node, dict) and isinstance(node.get('name'), str)}

    providers = main.get('proxy-providers') or {}
    proxies = main.setdefault('proxies', [])
    groups = main.setdefault('proxy-groups', [])
    taken = {item['name'] for item in proxies} | {item['name'] for item in groups}
    own = set()  # names this function created; anything else in ``taken`` is the user's
    created, skipped = [], []

    for sub_id, cache in caches.items():
        sub = subscriptions[sub_id]
        nodes = [node for node in cache.get('nodes') or []
                 if isinstance(node, dict) and isinstance(node.get('name'), str) and node['name']]
        visible_nodes = filter_subscription_items(sub, nodes)
        chained = [(node, target) for node in visible_nodes
                   if 'dialer-proxy' in str(node.get('proxy_string', '')) + str(node.get('params', ''))
                   and (target := _node_dialer(node)) and target not in taken]
        if not chained:
            continue
        record = dialer_record(sub, cache)
        known = (record or {}).get('targets') or {}
        airport = _airport_groups(record)
        by_name = {node['name']: node for node in nodes}
        visible = {node['name'] for node in visible_nodes}
        name = sub['name']
        resolved = {}

        def claim(value):
            if value in taken:
                raise DialerReferenceError(
                    f'名称「{value}」已被使用，与订阅「{name}」自动生成的中转分组冲突，请改名后重新生成')
            taken.add(value)
            own.add(value)

        def resolve(target, path=()):
            """The emitted name for an airport group, creating it; None when it cannot be provided."""
            if target in resolved:
                return resolved[target]
            resolved[target] = None  # a cycle stays unresolved
            entry = known.get(target)
            if not entry or target in path or any(member not in by_name for member in entry['members']):
                return None
            members = entry['members']
            group = {'name': group_name(name, target), 'type': entry['type'], 'hidden': True,
                     **{key: entry[key] for key in _GROUP_KEYS if key in entry}}
            if sub_id in direct and name in providers and all(member in visible for member in members):
                # The relays are already in the subscription's provider: select them by exact name.
                group.update({'use': [name], 'filter': '^(?:' + '|'.join(map(_name_pattern, members)) + ')$'})
            else:
                # Prepare every copy first so a failure leaves nothing half added.
                pending = []
                for member in members:
                    copy_name = group_name(name, member)
                    if copy_name in own:
                        continue
                    proxy = convert_node_to_mihomo(by_name[member])
                    if not proxy:
                        return None
                    proxy = {**proxy, 'name': copy_name}
                    nested = proxy.get('dialer-proxy')
                    if isinstance(nested, str) and nested in airport:
                        emitted_nested = resolve(nested, path + (target,))
                        if emitted_nested is None:
                            return None
                        proxy['dialer-proxy'] = emitted_nested
                    pending.append(proxy)
                for proxy in pending:
                    claim(proxy['name'])
                    proxies.append(proxy)
                group['proxies'] = [group_name(name, member) for member in members]
            claim(group['name'])
            groups.append(group)
            created.append({'subscription': name, 'target': target, 'group': group['name'], 'members': members,
                            'copied': 'proxies' in group})
            resolved[target] = group['name']
            return group['name']

        for node, target in chained:
            if target in airport:
                if resolve(target) is None:
                    skipped.append({'subscription': name, 'node': node['name'], 'target': target, 'reason': 'missing'})
            elif record is None and target not in node_names:
                # Without a record this may be an airport group: a refresh records it. Until then
                # delivery keeps the node and the regular validation reports the reference.
                skipped.append({'subscription': name, 'node': node['name'], 'target': target, 'reason': 'refresh'})

    if not created and not skipped:
        return None
    report = {'groups': created, 'skipped': skipped[:MAX_REPORTED], 'skipped_count': len(skipped)}
    if skipped:
        logger.warning('订阅链式节点：%d 个节点的中转分组未能补上（%s）', len(skipped),
                       '；'.join(f"{item['subscription']}「{item['node']}」→ {item['target']}" for item in skipped[:5]))
    return report


def adapt_subscription_proxies(subscription, proxies, main, record, *, chains=True):
    """The subscription's proxies as delivered with ``main``.

    A node whose ``dialer-proxy`` names one of the airport's own groups (``record``)
    is re-pointed at the generated ``<subscription>/<group>`` when ``main`` has it,
    keeps the name when ``main`` defines it (a group the user made by hand), and is
    left out otherwise; ``chains=False`` (Surge, Loon) leaves it out too. Every
    other proxy is returned as it is. Applying it to its own output changes nothing.
    """
    airport = _airport_groups(record)
    if not airport:
        return proxies
    names = {item.get('name') for item in (main or {}).get('proxies') or []} | {
        item.get('name') for item in (main or {}).get('proxy-groups') or []}
    adapted, dropped = [], []
    for proxy in proxies:
        target = proxy.get('dialer-proxy') if isinstance(proxy, dict) else None
        if not isinstance(target, str) or target not in airport:
            adapted.append(proxy)
        elif not chains:
            dropped.append(proxy.get('name'))
        elif group_name(subscription, target) in names:
            adapted.append({**proxy, 'dialer-proxy': group_name(subscription, target)})
        elif target in names:
            adapted.append(proxy)
        else:
            dropped.append(proxy.get('name'))
    if dropped:
        logger.info('订阅「%s」跳过 %d 个链式节点：%s', subscription, len(dropped), '、'.join(map(str, dropped[:5])))
    return adapted
