"""策略组管理路由"""
import re
from copy import deepcopy

from flask import request, jsonify

from backend.routes import proxy_groups_bp
from backend.common.auth import require_auth
from backend.common.config import get_config, get_shared_config, update_config_transaction
from backend.common.config_repository import ProfileRepositoryError
from backend.utils.reorder import resolve_new_order
from backend.utils.subscription_cache import load_subscription_cache
from backend.utils.subscription_filter import filter_subscription_items
from backend.utils.group_candidates import CandidateValidationError, filter_candidates


@proxy_groups_bp.route('/health-defaults', methods=['GET'])
@require_auth
def health_defaults():
    from backend.utils.health_check import GROUP_DEFAULTS, PROVIDER_DEFAULTS
    return jsonify({'group': {**GROUP_DEFAULTS, 'tolerance': 50}, 'provider': PROVIDER_DEFAULTS})


def _get_subscription_nodes(config_data, sub_id):
    """Get nodes for a subscription from cache first, then imported nodes."""
    nodes = []
    cache = load_subscription_cache(sub_id)
    if cache and isinstance(cache.get('nodes'), list):
        nodes.extend(cache.get('nodes', []))

    existing_names = {node.get('name') for node in nodes if node.get('name')}
    for node in config_data.get('nodes', []):
        if node.get('subscription_id') == sub_id and node.get('enabled', True):
            if node.get('name') not in existing_names:
                nodes.append(node)
                existing_names.add(node.get('name'))

    sub = next((s for s in config_data.get('subscriptions', []) if s.get('id') == sub_id), None)
    return filter_subscription_items(sub, nodes)


def _get_manual_nodes(config_data, node_ids):
    """Resolve manually selected node ids to node objects."""
    result = []
    for node_id in node_ids:
        if node_id in ['DIRECT', 'REJECT']:
            result.append({
                'id': node_id,
                'name': node_id,
                'type': 'builtin',
                'source_type': 'manual'
            })
            continue

        node = next(
            (n for n in config_data.get('nodes', []) if n.get('id') == node_id and n.get('enabled', True)),
            None
        )
        if node:
            result.append(node)

    return result


def _add_source_meta(nodes, source_type, source_id, source_name):
    """Return shallow node copies with display source metadata."""
    enriched = []
    for node in nodes:
        item = dict(node)
        item['source_type'] = source_type
        item['source_id'] = source_id
        item['source_name'] = source_name
        enriched.append(item)
    return enriched


def _clean_group_fields(group):
    from backend.utils.health_check import GROUP_HEALTH_TYPES
    if group.get('type') not in GROUP_HEALTH_TYPES:
        for key in ('url', 'interval', 'timeout', 'lazy', 'expected-status', 'tolerance'):
            group.pop(key, None)
    if group.get('type') != 'url-test':
        group.pop('tolerance', None)
    if group.get('type') != 'load-balance':
        group.pop('strategy', None)
    if group.get('type') == 'chain':
        for field in ('subscriptions', 'regex', 'exclude_regex', 'manual_nodes',
                      'aggregations', 'aggregation_regex', 'aggregation_exclude_regex',
                      'include_groups', 'follow_group', 'proxies', 'source', 'source_type',
                      'proxies_order', 'url', 'interval', 'strategy', 'lazy', 'use',
                      'include_all', 'include-all'):
            group.pop(field, None)
    else:
        group.pop('chain', None)


def _sync_follow_dns(before, after):
    """Keep opted-in DNS routes with changed group defaults in this transaction.

    A custom native draft only blocks a group edit when a following rule would
    actually move. Explicit DNS choices and unrelated group edits stay intact.
    """
    from backend.common.rule_dns import apply_rule_dns, follow_route, policy_dns_routes
    settings = after.get('oxidns') or {}
    following = set(settings.get('rule_dns_follow') or [])
    if settings.get('config_mode') == 'native':
        following &= {rule['id'] for rule in after.get('rule_configs', [])
                      if rule.get('enabled', True) and rule.get('library_enabled', True)}
    if not following:
        return []
    # The transaction snapshot hydrates only the old group's shared references.
    # A newly selected node/provider must be visible while resolving its default.
    shared = get_shared_config()
    before = {**before, **shared}
    current = {**after, **shared}
    previous = {rule['id']: rule for rule in before.get('rule_configs', [])}
    before_routes, after_routes = policy_dns_routes(before), policy_dns_routes(current)
    changed = any(rule['id'] in following and rule['id'] in previous
                  and follow_route(previous[rule['id']], routes=before_routes) != follow_route(rule, routes=after_routes)
                  for rule in after.get('rule_configs', []))
    if not changed:
        return []
    warnings = apply_rule_dns(before, current, {}, base_url=request.url_root.rstrip('/'), strict=True)
    after['oxidns'] = current['oxidns']
    return warnings


@proxy_groups_bp.route('', methods=['GET', 'POST'])
@require_auth
def handle_proxy_groups():
    """策略组管理"""
    config_data = get_config()

    if request.method == 'GET':
        return jsonify(config_data['proxy_groups'])

    elif request.method == 'POST':
        group = request.json
        if not isinstance(group, dict):
            return jsonify({'success': False, 'message': '策略组请求必须是 JSON 对象'}), 400
        if 'delete_fields' in group:
            return jsonify({'success': False, 'message': 'delete_fields 仅适用于更新'}), 400
        _clean_group_fields(group)
        from backend.utils.health_check import initialize_group_health
        from backend.utils.group_candidates import validate_candidates
        initialize_group_health(group)
        warnings = []
        def create(profile):
            before = deepcopy(profile)
            validate_candidates(group, profile, get_shared_config())
            profile.setdefault('proxy_groups', []).append(group)
            warnings.extend(_sync_follow_dns(before, profile))
        try:
            update_config_transaction(create)
        except CandidateValidationError as exc:
            return jsonify({'success': False, 'message': str(exc)}), 400
        return jsonify({'success': True, 'data': group, **({'warnings': warnings} if warnings else {})})


@proxy_groups_bp.route('/batch-health-url', methods=['PUT'])
@require_auth
def update_batch_health_url():
    """Change only selected ordinary groups' probe URLs in one transaction."""
    from werkzeug.exceptions import BadRequest, NotFound
    from backend.utils.health_check import GROUP_HEALTH_TYPES, validate_health

    payload = request.get_json(silent=True)
    if not isinstance(payload, dict):
        return jsonify({'success': False, 'message': '批量测速地址请求必须是 JSON 对象'}), 400
    group_ids = payload.get('ids')
    if (not isinstance(group_ids, list) or not group_ids
            or any(not isinstance(ident, str) or not ident.strip() for ident in group_ids)
            or len(set(group_ids)) != len(group_ids)):
        return jsonify({'success': False, 'message': 'ids 必须是非空且不重复的策略组 ID 字符串数组'}), 400
    url = payload.get('url')
    try:
        validate_health({'url': url})
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400

    def mutate(profile):
        groups = {group.get('id'): group for group in profile.get('proxy_groups', [])}
        selected = []
        for group_id in group_ids:
            group = groups.get(group_id)
            if group is None:
                raise NotFound(f'策略组不存在：{group_id}')
            if group.get('follow_group'):
                raise BadRequest(f'策略组「{group.get("name", group_id)}」跟随其他组，请修改其来源组的测速地址')
            if group.get('type') not in GROUP_HEALTH_TYPES:
                raise BadRequest(f'策略组「{group.get("name", group_id)}」不支持节点测速')
            selected.append(group)
        for group in selected:
            group['url'] = url

    try:
        saved = update_config_transaction(mutate)
    except (BadRequest, NotFound) as exc:
        return jsonify({'success': False, 'message': exc.description}), exc.code
    groups = {group['id']: group for group in saved['proxy_groups']}
    return jsonify({'success': True, 'data': [groups[group_id] for group_id in group_ids]})


@proxy_groups_bp.route('/<group_id>', methods=['DELETE', 'PUT'])
@require_auth
def handle_proxy_group(group_id):
    """Single-profile transaction: validate references before any write."""
    from werkzeug.exceptions import Conflict, NotFound
    from backend.utils.dialer_references import incoming_dialers, DialerReferenceError
    from backend.utils.mihomo_listeners import relink_group_listeners, ListenerReferenceError

    group_data = request.get_json() if request.method == 'PUT' else None
    if request.method == 'PUT' and not isinstance(group_data, dict):
        return jsonify({'success': False, 'message': '策略组请求必须是 JSON 对象'}), 400
    listener_changes = []
    warnings = []

    def mutate(profile):
        before = deepcopy(profile)
        groups = profile.setdefault('proxy_groups', [])
        original = next((g for g in groups if g.get('id') == group_id), None)
        if original is None:
            raise NotFound('Group not found')
        if group_data is not None:
            if group_data.get('id', group_id) != group_id:
                raise Conflict('策略组 ID 不可修改，请保留原 ID')
            from backend.utils.group_candidates import merge_group_update
            from backend.utils.health_check import GROUP_HEALTH_TYPES, initialize_group_health
            base = dict(original)
            new_type = group_data.get('type', original.get('type'))
            if original.get('type') not in GROUP_HEALTH_TYPES and new_type in GROUP_HEALTH_TYPES:
                base['type'] = new_type
                initialize_group_health(base)
            merged = merge_group_update(base, group_data)
            group_data.clear()
            group_data.update(merged)
            group_data['id'] = group_id
            _clean_group_fields(group_data)
            from backend.utils.group_candidates import validate_candidates
            shared = get_shared_config()
            final_profile = {**profile, 'proxy_groups': [
                group_data if g is original else g for g in groups]}
            validate_candidates(group_data, final_profile, shared, original=original)
            if any(g.get('id') != group_id and g.get('name') == group_data.get('name') for g in groups):
                raise Conflict('策略组名称已存在，请使用其他名称')
        removing = request.method == 'DELETE' or not group_data.get('enabled', True)
        if removing:
            rules = [r for r in profile.get('rule_configs', [])
                     if r.get('enabled', True) and r.get('policy') == original.get('name')]
            dependents = [g for g in groups if g.get('id') != group_id and
                          (group_id in g.get('include_groups', []) or g.get('follow_group') == group_id)]
            if rules or dependents or incoming_dialers(profile, 'group', group_id):
                raise Conflict('策略组仍被规则或其他策略组引用，请先修改引用后再删除或禁用')
        if removing or original.get('name') != group_data.get('name'):
            listener_changes[:] = relink_group_listeners(
                profile, original.get('name'), None if removing else group_data.get('name'))
        if request.method == 'DELETE':
            groups.remove(original)
        else:
            for follower in groups:
                if follower.get('follow_group') == group_id:
                    validate_candidates(follower, final_profile, shared, original=follower)
            # Names are rule-policy keys; group-to-group references use stable IDs.
            old_name = original.get('name')
            new_name = group_data.get('name')
            if old_name != new_name:
                for rule in profile.get('rule_configs', []):
                    if rule.get('policy') == old_name:
                        rule['policy'] = new_name
            groups[groups.index(original)] = group_data
        warnings.extend(_sync_follow_dns(before, profile))

    try:
        update_config_transaction(mutate)
    except (DialerReferenceError, CandidateValidationError, ListenerReferenceError) as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    except (Conflict, NotFound) as exc:
        return jsonify({'success': False, 'message': exc.description}), exc.code
    result = {'success': True, **({'data': group_data} if group_data is not None else {}),
              **({'warnings': warnings} if warnings else {})}
    if listener_changes:
        removing = request.method == 'DELETE' or not group_data.get('enabled', True)
        result['listener_changes'] = listener_changes
        result['message'] = (f'已保存；{len(listener_changes)} 个 Mihomo 入口已解除固定策略，恢复各自的规则路由。'
                             if removing else f'已保存；已同步更新 {len(listener_changes)} 个 Mihomo 入口的固定策略名称。')
    return jsonify(result)


@proxy_groups_bp.route('/preview-regex', methods=['POST'])
@require_auth
def preview_proxy_group_regex():
    """Preview retained nodes after inclusion and exclusion, without saving config."""
    try:
        config_data = get_shared_config()
        payload = request.get_json() or {}
        if not isinstance(payload, dict):
            return jsonify({'success': False, 'message': '预览请求必须是 JSON 对象'}), 400
        source = payload.get('source')
        patterns = {}
        for field, label in (('regex', '正则表达式'), ('exclude_regex', '排除正则')):
            value = payload.get(field)
            if value is not None and not isinstance(value, str):
                return jsonify({'success': False, 'message': f'{label}必须是字符串'}), 400
            patterns[field] = (value or '').strip()
        regex_text = patterns['regex']
        exclude_text = patterns['exclude_regex']

        if source not in ['subscription', 'aggregation']:
            return jsonify({'success': False, 'message': 'Invalid preview source'}), 400

        if not regex_text and not exclude_text:
            return jsonify({'success': False, 'message': '请输入正则表达式'}), 400

        # The inclusion becomes the group's Mihomo filter, which Mihomo splits on
        # backticks and orders by part. The exclusion renders backticks literally.
        try:
            buckets = [re.compile(part) for part in regex_text.split('`')] if regex_text else [None]
        except re.error as exc:
            return jsonify({'success': False, 'message': f'正则表达式无效: {exc}'}), 400
        try:
            exclude_regex = re.compile(exclude_text) if exclude_text else None
        except re.error as exc:
            return jsonify({'success': False, 'message': f'排除正则无效: {exc}'}), 400

        candidates = []
        # 节点库按来源展示时保留不同订阅中的同名节点；策略组预览仍默认按名去重。
        deduplicate_by_source = payload.get('deduplicate_by_source') is True
        imported_node_ids = []

        if source == 'subscription':
            subscription_ids = payload.get('subscriptions') or []
            subscriptions = config_data.get('subscriptions', [])
            for sub_id in subscription_ids:
                sub = next((s for s in subscriptions if s.get('id') == sub_id and s.get('enabled', True)), None)
                if not sub:
                    continue
                nodes = _get_subscription_nodes(config_data, sub_id)
                candidates.extend(_add_source_meta(nodes, 'subscription', sub_id, sub.get('name', sub_id)))
                if deduplicate_by_source:
                    imported = [n for n in config_data.get('nodes', []) if n.get('subscription_id') == sub_id]
                    imported_node_ids.extend(
                        n['id'] for n in filter_subscription_items(sub, imported) if n.get('id')
                    )

        if source == 'aggregation':
            aggregation_ids = payload.get('aggregations') or []
            aggregations = config_data.get('subscription_aggregations', [])
            subscriptions = config_data.get('subscriptions', [])

            for agg_id in aggregation_ids:
                agg = next((a for a in aggregations if a.get('id') == agg_id and a.get('enabled', True)), None)
                if not agg:
                    continue

                agg_name = agg.get('name', agg_id)
                aggregation_nodes = []
                for sub_id in agg.get('subscriptions', []):
                    sub = next((s for s in subscriptions if s.get('id') == sub_id and s.get('enabled', True)), None)
                    if not sub:
                        continue
                    nodes = _get_subscription_nodes(config_data, sub_id)
                    aggregation_nodes.extend(_add_source_meta(nodes, 'aggregation', agg_id, agg_name))

                manual_nodes = _get_manual_nodes(config_data, agg.get('nodes', []))
                aggregation_nodes.extend(_add_source_meta(manual_nodes, 'aggregation', agg_id, agg_name))
                candidates.extend(filter_candidates(aggregation_nodes, agg.get('regex_filter') or ''))

        matched = []
        seen_names = set()
        for regex, node in ((regex, node) for regex in buckets for node in candidates):
            name = node.get('name', '')
            if not name or (regex and not regex.search(name)):
                continue
            if exclude_regex and exclude_regex.search(name):
                continue
            key = (node.get('source_id'), name) if deduplicate_by_source else name
            if key in seen_names:
                continue
            seen_names.add(key)
            matched.append({
                'id': node.get('id', name),
                'name': name,
                'type': node.get('type') or node.get('protocol') or 'unknown',
                'subscription_id': node.get('subscription_id'),
                'subscription_name': node.get('subscription_name'),
                'source_type': node.get('source_type'),
                'source_id': node.get('source_id'),
                'source_name': node.get('source_name')
            })

        response = {
            'success': True,
            'count': len(matched),
            'total_candidates': len(candidates),
            'nodes': matched
        }
        if deduplicate_by_source and source == 'subscription':
            response['imported_node_ids'] = imported_node_ids
        return jsonify(response)

    except CandidateValidationError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    except Exception as exc:
        return jsonify({'success': False, 'message': str(exc)}), 500


@proxy_groups_bp.route('/reorder', methods=['POST'])
@require_auth
def reorder_proxy_groups():
    """批量更新策略组顺序

    按 id 排序时传 {'ids': [...], 'position': 'top'|'bottom'}；
    传完整对象数组的旧格式仍然兼容。
    """
    from backend.utils.dialer_references import validate_dialers, DialerReferenceError
    from werkzeug.exceptions import NotFound
    order = []
    body = request.get_json() or {}
    def mutate(profile):
        new_order, missing = resolve_new_order(profile.get('proxy_groups', []), body, 'groups')
        if missing:
            raise NotFound(f'以下策略组 id 不存在: {missing}')
        profile['proxy_groups'] = new_order
        validate_dialers(profile)
        order.extend(item.get('id') for item in new_order)
    try:
        update_config_transaction(mutate)
        return jsonify({'success': True, 'order': order})
    except (DialerReferenceError, CandidateValidationError) as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    except NotFound as exc:
        return jsonify({'success': False, 'message': exc.description}), 404
    except ProfileRepositoryError:
        raise
    except Exception as exc:
        return jsonify({'success': False, 'message': str(exc)}), 500
