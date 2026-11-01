"""Plan explicit node deletion without publishing or rewriting dialer dependencies."""

import copy
import hashlib
import json


def plan_node_deletion(document, node_ids):
    from werkzeug.exceptions import NotFound
    from backend.common.config_repository import (
        BUILTIN_POLICIES, RESOURCE_FIELDS, ProfileValidationError,
        _collect_resource_refs, _items_by_id,
    )
    from backend.utils.dialer_references import raw_dialer

    if (not isinstance(node_ids, list) or not node_ids
            or any(not isinstance(value, str) or not value.strip() for value in node_ids)):
        raise ProfileValidationError('请选择要删除的节点')
    ids = set(node_ids)
    shared = document['shared']
    nodes = [node for node in shared['nodes'] if node['id'] in ids]
    if len(nodes) != len(ids):
        raise NotFound('部分节点已不存在，请刷新节点列表')
    names = {node['name'] for node in nodes} - BUILTIN_POLICIES
    usages, groups, blockers, empty_sources = [], [], [], []
    originals = []
    candidate = copy.deepcopy(document)

    for aggregation in candidate['shared']['subscription_aggregations']:
        if not ids.intersection(aggregation.get('nodes', [])):
            continue
        originals.append(copy.deepcopy(aggregation))
        usages.append({'kind': 'aggregation', 'id': aggregation['id'], 'name': aggregation['name']})
        aggregation['nodes'] = [value for value in aggregation['nodes'] if value not in ids]
        if not aggregation['nodes'] and not aggregation.get('subscriptions'):
            empty_sources.append(f'聚合「{aggregation["name"]}」')

    catalogs = {field: _items_by_id(shared[field], field) for field in RESOURCE_FIELDS}
    for profile_id, profile in document['profiles'].items():
        if ids.intersection(_collect_resource_refs(profile, catalogs)['nodes']):
            usages.append({'kind': 'profile', 'id': profile_id, 'name': profile['name']})
        for group in candidate['profiles'][profile_id]['proxy_groups']:
            label = f'配置「{profile["name"]}」的策略组「{group["name"]}」'
            if group.get('type') == 'chain':
                if any(ref.get('type') == 'node' and ref.get('id') in ids
                       for ref in group.get('chain', {}).values()):
                    blockers.append(f'{label}是代理链，请先更换或移除对应的前置／落地节点。')
                continue
            before = copy.deepcopy(group)
            for field in ('manual_nodes', 'proxies'):
                if field in group and (field == 'manual_nodes' or group.get('source') == 'node'):
                    group[field] = [value for value in group[field] if value not in ids]
            if 'proxies_order' in group:
                group['proxies_order'] = [item for item in group['proxies_order']
                                          if not (item.get('type') == 'node' and item.get('id') in ids)]
            if group != before:
                originals.append({'profile_id': profile_id, 'group': before})
                groups.append({'id': group['id'], 'name': group['name'],
                               'profile_id': profile_id, 'profile_name': profile['name']})
                if not any(group.get(field) for field in (
                    'manual_nodes', 'proxies', 'subscriptions', 'aggregations', 'include_groups', 'follow_group',
                )):
                    empty_sources.append(label)
        # A rule naming a node cannot safely be redirected to DIRECT or another node.
        for rule in profile.get('rule_configs', []):
            if rule.get('policy') in names:
                blockers.append(f'配置「{profile["name"]}」的规则「{rule.get("name") or rule["id"]}」'
                                f'直接使用节点「{rule["policy"]}」，请先更换规则策略。')

    for node in shared['nodes']:
        target = raw_dialer(node)
        if node['id'] not in ids and isinstance(target, str) and target in names:
            blockers.append(f'节点「{node["name"]}」的 dialer-proxy 使用了待删除节点，请先修改拨号代理。')
    candidate['shared']['nodes'] = [node for node in shared['nodes'] if node['id'] not in ids]
    preview = {
        'nodes': [{'id': node['id'], 'name': node['name']} for node in nodes],
        'usages': usages, 'groups': groups, 'blockers': blockers, 'empty_sources': empty_sources,
    }
    # Confirm exactly the nodes and affected references shown to the user. Unrelated
    # writes do not invalidate the preview; changed targets/references always do.
    signature = {'nodes': nodes, 'originals': originals, 'preview': preview}
    preview['confirmation'] = hashlib.sha256(
        json.dumps(signature, ensure_ascii=False, sort_keys=True).encode('utf-8')
    ).hexdigest()
    return preview, candidate
