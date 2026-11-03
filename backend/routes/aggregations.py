"""订阅聚合路由模块

负责订阅聚合相关的业务逻辑和路由，包括：
- 聚合的增删改查
- 生成聚合 provider 文件
- 清理策略组中的聚合引用
"""
import json
import os
import re
import uuid
import yaml
from typing import Dict, Any
from datetime import datetime
from flask import request, jsonify, current_app, Response, make_response

from backend.common.config import (
    get_default_profile_id,
    get_shared_config,
    save_shared_config,
    update_shared_config_transaction,
    DATA_DIR,
    get_repository,
    get_resource_config,
)
from backend.common.profile_context import resolve_profile_id
from backend.common.config_repository import ProfileRepositoryError
from backend.common.auth import validate_token_or_jwt, require_auth
from backend.routes import subscription_aggregations_bp as bp
from backend.converters.mihomo import convert_node_to_mihomo
from backend.utils.subscription_cache import load_subscription_cache
from backend.utils.subscription_filter import filter_subscription_items
from backend.utils.subscription_refresh import fetch_subscription_source, source_cache_metadata
from backend.utils.sub_store_client import (
    get_subscription_proxies_yaml,
    parse_proxies_from_yaml,
    proxies_to_nodes,
)
from backend.utils.strategy_references import StrategyReferenceError
from backend.utils.logger import get_logger
from backend.utils.url_utils import safe_exception_details
from backend.utils.reorder import resolve_new_order

logger = get_logger(__name__)

# 聚合 provider 文件存储目录
AGGREGATION_PROVIDERS_DIR = os.path.join(DATA_DIR, 'providers')



@bp.route('/<resource_id>/health-impact', methods=['GET'])
@require_auth
def health_impact(resource_id):
    from backend.common.config import get_repository
    return jsonify(get_repository().get_resource_usage('subscription_aggregations', resource_id))


# ============================================================================
# 业务逻辑函数
# ============================================================================

def generate_aggregation_provider(aggregation: Dict[str, Any], *, config=None,
                                  main_config=None, persist=True, subscription_fetch=None,
                                  source_prepared=False) -> Dict[str, Any]:
    """生成订阅聚合的 provider YAML 文件

    Args:
        aggregation: 聚合配置，包含 subscriptions、nodes、regex_filter 等字段
        source_prepared: 调用方已按刷新策略固定本次发布的数据，直接复用其结果

    Returns:
        Dict[str, Any]: 包含文件路径和节点统计数据
        {
            'file_path': str,  # 生成的 YAML 文件路径
            'subscription_node_counts': dict,  # 各订阅的节点数统计
            'total_count': int  # 总节点数
        }

    处理流程：
    1. 按订阅源更新间隔获取节点；关闭更新或尚未到期时复用缓存
    2. 添加手动选择的节点（避免重复）
    3. 应用正则过滤
    4. 转换为 mihomo 格式
    5. 生成并保存 YAML 文件
    """
    # Dry rendering accepts explicit profile-scoped snapshots without requiring
    # that a pure converter fixture has already been persisted in a repository.
    profile_id = (config.get('profile_id') or get_default_profile_id()) if config is not None and not persist else resolve_profile_id(
        config.get('profile_id') if config is not None else None)
    config_data = config if config is not None else get_resource_config('subscription_aggregations', aggregation['id'], profile_id)
    from backend.utils.dialer_references import validate_dialers
    # Known stored graph errors must fail before fetch or cache mutation.
    validate_dialers(config_data)
    # Standalone delivery must discover opaque main-node conversion results too.
    # Capture once before fetching; a supplied main is already materialized by
    # main preflight/Agent delivery. Disable provider preflight to avoid recursion
    # and unrelated provider fetches or cache/artifact publication.
    from backend.converters.mihomo import generate_mihomo_config
    from backend.utils.provider_delivery import DeliverySnapshot
    main = main_config if main_config is not None else yaml.safe_load(
        generate_mihomo_config(config_data, preflight_providers=False))
    snapshot = DeliverySnapshot.capture(profile_id, main, profile=config_data)
    if persist:
        from backend.utils.provider_delivery import prepare_provider_bundle, commit_provider_bundle
        bundle = prepare_provider_bundle(config_data, main, requested=('aggregation', aggregation),
                                         subscription_fetch=subscription_fetch)
        rendered = next(item for item in bundle if item['name'] == aggregation['name'])
        commit_provider_bundle(profile_id, bundle)
        rendered['file_path'] = str(get_repository().profile_path(profile_id, f"providers/{aggregation['id']}.yaml"))
        return rendered
    agg_name = aggregation['name']

    # 收集所有节点
    all_nodes = []

    # 记录各订阅的节点数统计（不保存到配置文件，仅用于返回）
    subscription_node_counts = {}

    # 1. 从选择的订阅中获取节点 - 优先通过 Sub-Store 获取
    # sub_proxies_map: sub_id -> proxies list（mihomo 格式，用于最终输出）
    sub_proxies_map = {}
    pending_cache_updates = []
    subscription_ids = aggregation.get('subscriptions', [])
    if subscription_ids:
        subscriptions = config_data.get('subscriptions', [])

        def fetch_one_subscription(sub_id, sub):
            """返回订阅内容、节点与需要提交的下载时间信息。

            单个订阅的失败被限制在本函数内：调用方只会看到 nodes_list 为
            空，不会中断其他订阅。
            """
            nodes_list = None
            proxies = None

            # 优先通过 Sub-Store 获取
            try:
                logger.info(f"尝试通过 Sub-Store 获取订阅最新数据: '{sub['name']}'")
                fetch = subscription_fetch or get_subscription_proxies_yaml
                result = fetch(sub_id, sub['url']) if source_prepared else fetch_subscription_source(sub, fetch=fetch)
                yaml_text, source = result
                from backend.utils.provider_delivery import parse_provider_proxies
                proxies = parse_provider_proxies(yaml_text, name=sub['name'])

                # 转换为 node 格式用于缓存和过滤
                nodes_list = proxies_to_nodes(proxies)
                for node in nodes_list:
                    node['subscription_id'] = sub_id
                    node['subscription_name'] = sub['name']
                    if 'id' not in node:
                        node['id'] = f"node_{uuid.uuid4().hex[:8]}"

                # Cache updates are deferred until the entire provider passes
                # final topology validation; dry preflight never commits them.
                if source == 'cache':
                    logger.info("使用订阅 '%s' 的已有节点缓存", sub['name'])
                elif source == 'source_cache':
                    logger.info("复用订阅 '%s' 在更新间隔内的下载结果", sub['name'])
                elif source == 'rendered_yaml':
                    logger.info(f"成功直接复用订阅 URL 返回的 Sub-Store YAML 并更新缓存: '{sub['name']}', 节点数: {len(nodes_list)}")
                elif source == 'sub_store':
                    logger.info(f"成功通过 Sub-Store 转换订阅并更新缓存: '{sub['name']}', 节点数: {len(nodes_list)}")
                elif source == 'direct_url_fallback':
                    logger.info(f"Sub-Store 获取失败后，成功直接拉取原始订阅并更新缓存: '{sub['name']}', 节点数: {len(nodes_list)}")
                else:
                    logger.info(f"成功获取订阅并更新缓存: '{sub['name']}', 节点数: {len(nodes_list)}")
            except StrategyReferenceError:
                raise
            except Exception as e:
                proxies = None
                nodes_list = None
                logger.warning("通过 Sub-Store 获取订阅 '%s' 失败, 尝试读取本地缓存: %s", sub['name'], safe_exception_details(e))

            # 如果从 Sub-Store 获取失败，从本地缓存读取
            if nodes_list is None:
                cache = load_subscription_cache(sub_id)
                if cache:
                    nodes_list = cache.get('nodes', [])
                    logger.info(f"从本地缓存读取订阅 '{sub['name']}', 节点数: {len(nodes_list)}")
                else:
                    logger.error(f"订阅 '{sub['name']}' 既无法从 Sub-Store 获取也没有本地缓存")
                    nodes_list = []

            metadata = source_cache_metadata(sub, result) if proxies is not None and source not in ('cache', 'prepared_yaml') else None
            return sub_id, proxies, nodes_list, metadata

        # 并发拉取各订阅：单个订阅经 Sub-Store 解析最长可耗数十秒，
        # 串行时一个慢/失败的订阅会拖长整个聚合，导致客户端（如 Mihomo
        # 拉取 provider）先行超时，表现为"整个聚合更新失败"
        pending = [
            (sub_id, sub)
            for sub_id in subscription_ids
            for sub in [next((s for s in subscriptions
                              if s['id'] == sub_id and s.get('enabled', True)), None)]
            if sub
        ]

        results = {}
        if pending:
            from concurrent.futures import ThreadPoolExecutor, as_completed
            with ThreadPoolExecutor(max_workers=min(8, len(pending))) as executor:
                futures = [executor.submit(fetch_one_subscription, sub_id, sub)
                           for sub_id, sub in pending]
                for future in as_completed(futures):
                    try:
                        fetched_id, proxies, nodes_list, fetched = future.result()
                        results[fetched_id] = (proxies, nodes_list, fetched)
                    except StrategyReferenceError:
                        raise
                    except Exception as e:
                        # 兜底：单个订阅的任何未预期异常都不影响其他订阅
                        logger.error("订阅拉取任务异常: %s", safe_exception_details(e))

        # 按聚合中配置的订阅顺序汇总，保证输出稳定
        for sub_id, _sub in pending:
            proxies, nodes_list, fetched = results.get(sub_id, (None, [], False))
            if proxies is not None:
                sub_proxies_map[sub_id] = filter_subscription_items(_sub, proxies)
                # 缓存保留订阅原始节点，订阅级筛选只作用于聚合输出
                if fetched:
                    pending_cache_updates.append((sub_id, nodes_list, fetched))
            nodes_list = filter_subscription_items(_sub, nodes_list)

            # Validate cache/remote metadata before hashing names or converting.
            from backend.utils.dialer_references import validate_shapes
            validate_shapes({'nodes': nodes_list}, require_ids=False)
            # 记录该订阅的节点数（在过滤前）
            subscription_node_counts[sub_id] = len(nodes_list)

            # 添加到节点列表（用于正则过滤）
            for node in nodes_list:
                node['subscription_id'] = sub_id
                node['enabled'] = True
                all_nodes.append(node)

    # 2. 添加手动选择的节点
    node_ids = aggregation.get('nodes', [])
    if node_ids:
        config_nodes = config_data.get('nodes', [])
        # 收集已有的节点名称，避免重复
        existing_node_names = {n.get('name') for n in all_nodes if n.get('name')}
        for node in config_nodes:
            if node.get('id') in node_ids and node.get('enabled', True):
                # 避免重复添加（按节点名称判断）
                if node.get('name') not in existing_node_names:
                    all_nodes.append(node)
                    existing_node_names.add(node.get('name'))

    # 3. 应用正则过滤
    regex_filter = aggregation.get('regex_filter', '').strip()
    if regex_filter:
        try:
            regex = re.compile(regex_filter)
            all_nodes = [node for node in all_nodes if regex.search(node.get('name', ''))]
        except re.error as e:
            from backend.utils.dialer_references import DialerReferenceError
            raise DialerReferenceError('聚合正则过滤无效，不能生成 Provider') from e

    # 4. 转换为 mihomo 格式
    # 构建 sub-store proxies 按名称索引（用于快速查找）
    sub_store_proxy_by_name = {}
    for proxies_list in sub_proxies_map.values():
        for p in proxies_list:
            sub_store_proxy_by_name[p.get('name', '')] = p

    from backend.utils.dialer_references import validate_dialers, overlay_dialer
    from backend.utils.subscription_dialers import adapt_subscription_proxies, dialer_record
    from flask import has_request_context
    validate_dialers(config_data)
    # Subscription nodes' own chained targets follow the main configuration;
    # Surge and Loon have no dialer-proxy, so those nodes are left out there.
    main_document = json.loads(snapshot.main_json)
    subscription_names = {item.get('id'): item.get('name', '') for item in config_data.get('subscriptions', [])}
    records = {item['id']: dialer_record(item) for item in config_data.get('subscriptions', [])
               if item.get('id') in set(aggregation.get('subscriptions', []))}
    chains = not (has_request_context() and request.args.get('format') in ('surge', 'loon'))
    proxies = []
    for node in all_nodes:
        node_name = node.get('name', '')
        # 优先使用 Sub-Store 返回的原始 proxy（已经是 mihomo 格式）
        if node_name in sub_store_proxy_by_name:
            proxy = sub_store_proxy_by_name[node_name]
        else:
            # 手动节点或缓存降级节点，使用 convert_node_to_mihomo
            proxy = overlay_dialer(config_data, node, convert_node_to_mihomo(node))
        if proxy and node.get('subscription_id') in subscription_names:
            sub_id = node['subscription_id']
            proxy = next(iter(adapt_subscription_proxies(subscription_names[sub_id], [proxy], main_document,
                                                         records.get(sub_id), chains=chains)), None)
        if proxy:
            proxies.append(proxy)

    # Activation belongs to the actual immutable main, not stored metadata.
    # Validate every collection before replacing any last-known-good artifact.
    snapshot.validate(proxies, provider_name=aggregation['name'])
    if snapshot.has_chains or any(p.get('dialer-proxy') is not None for p in proxies):
        from backend.utils.dialer_references import DialerReferenceError
        from flask import has_request_context
        if has_request_context() and request.args.get('format') in ('surge', 'loon'):
            client = 'Loon' if request.args.get('format') == 'loon' else 'Surge'
            raise DialerReferenceError(f'{client} 暂不支持 dialer-proxy，请使用 Mihomo')
    # 5. 生成 YAML 内容（使用 IndentDumper 确保正确的缩进）
    from backend.converters.mihomo import IndentDumper
    provider_data = {'proxies': proxies}
    yaml_content = yaml.dump(
        provider_data,
        Dumper=IndentDumper,
        allow_unicode=True,
        default_flow_style=False,
        sort_keys=False,
        indent=2
    )

    logger.info(f"生成聚合 provider: {agg_name}, {len(proxies)} 个节点")

    # 7. 返回文件路径和统计数据（不保存到配置文件）
    return {
        'name': aggregation['name'],
        'file_path': None,
        'content': yaml_content,
        'cache_updates': pending_cache_updates,
        'subscription_node_counts': subscription_node_counts,
        'total_count': len(proxies)
    }




# ============================================================================
# 路由函数
# ============================================================================


@bp.route('', methods=['GET', 'POST'])
@require_auth
def handle_subscription_aggregations():
    """订阅聚合列表"""
    config_data = get_shared_config()
    if request.method == 'GET':
        # 获取所有聚合（快速返回，不计算节点数）
        return jsonify(config_data.get('subscription_aggregations', []))

    elif request.method == 'POST':
        # 创建新聚合
        aggregation = request.json
        if not isinstance(aggregation, dict):
            return jsonify({'success': False, 'message': '聚合必须是对象'}), 400
        from backend.utils.health_check import initialize_provider_health
        initialize_provider_health(aggregation)
        aggregation['id'] = f"agg_{uuid.uuid4().hex[:8]}"
        aggregation['created_at'] = datetime.now().isoformat()
        aggregation['updated_at'] = datetime.now().isoformat()

        # 移除不应该保存的统计字段
        aggregation.pop('node_count', None)
        aggregation.pop('total_node_count', None)
        aggregation.pop('subscription_node_counts', None)
        aggregation.pop('loading_count', None)

        update_shared_config_transaction(
            lambda profile: profile.setdefault('subscription_aggregations', []).append(aggregation)
        )

        return jsonify({'success': True, 'data': aggregation})


@bp.route('/<agg_id>', methods=['GET', 'PUT', 'DELETE'])
@require_auth
def handle_subscription_aggregation_item(agg_id):
    """单个订阅聚合操作"""
    config_data = get_shared_config()
    aggregations = config_data.get('subscription_aggregations', [])

    if request.method == 'GET':
        # 获取单个聚合
        aggregation = next((a for a in aggregations if a['id'] == agg_id), None)
        if aggregation:
            # 返回时也过滤掉统计字段
            agg_copy = dict(aggregation)
            agg_copy.pop('node_count', None)
            agg_copy.pop('total_node_count', None)
            agg_copy.pop('subscription_node_counts', None)
            return jsonify(agg_copy)
        else:
            return jsonify({'success': False, 'message': 'Aggregation not found'}), 404

    elif request.method == 'PUT':
        # 更新聚合
        try:
            for i, a in enumerate(aggregations):
                if a['id'] == agg_id:
                    updated_aggregation = request.json
                    if not isinstance(updated_aggregation, dict):
                        return jsonify({'success': False, 'message': '聚合必须是对象'}), 400
                    updated_aggregation['id'] = agg_id  # 确保ID不变

                    # 更新修改时间
                    updated_aggregation['updated_at'] = datetime.now().isoformat()

                    # 移除不应该保存的统计字段
                    updated_aggregation.pop('node_count', None)
                    updated_aggregation.pop('total_node_count', None)
                    updated_aggregation.pop('subscription_node_counts', None)
                    updated_aggregation.pop('loading_count', None)  # 前端的加载状态也不应该保存

                    config_data['subscription_aggregations'][i] = updated_aggregation


                    save_shared_config(config_data)
                    return jsonify({'success': True, 'data': updated_aggregation})

            return jsonify({'success': False, 'message': 'Aggregation not found'}), 404
        except ProfileRepositoryError:
            raise
        except Exception as e:
            logger.error("聚合操作失败: %s", safe_exception_details(e))
            return jsonify({'success': False, 'message': '聚合操作失败'}), 500

    elif request.method == 'DELETE':
        # 删除聚合
        config_data['subscription_aggregations'] = [a for a in aggregations if a['id'] != agg_id]
        save_shared_config(config_data)
        return jsonify({'success': True})


@bp.route('/<agg_id>/count', methods=['GET'])
@require_auth
def get_aggregation_node_count(agg_id):
    """获取聚合的节点数量（仅从本地缓存读取，不触发更新）"""
    config_data = get_shared_config()
    try:
        # 查找聚合
        aggregations = config_data.get('subscription_aggregations', [])
        aggregation = next((a for a in aggregations if a['id'] == agg_id), None)

        if not aggregation:
            return jsonify({'success': False, 'message': 'Aggregation not found'}), 404

        total_count = 0
        subscription_counts = {}

        # 从选择的订阅中统计节点数（仅从缓存读取）
        subscription_ids = aggregation.get('subscriptions', [])
        if subscription_ids:
            subs_by_id = {s.get('id'): s for s in config_data.get('subscriptions', [])}
            for sub_id in subscription_ids:
                cache = load_subscription_cache(sub_id)
                if cache:
                    cached_nodes = cache.get('nodes')
                    node_count = (len(filter_subscription_items(subs_by_id.get(sub_id), cached_nodes))
                                  if isinstance(cached_nodes, list) else cache.get('count', 0))
                    subscription_counts[sub_id] = node_count
                    total_count += node_count

        # 添加手动选择的节点数量
        node_ids = aggregation.get('nodes', [])
        if node_ids:
            config_nodes = config_data.get('nodes', [])
            manual_nodes = [n for n in config_nodes if n.get('id') in node_ids and n.get('enabled', True)]
            total_count += len(manual_nodes)

        return jsonify({
            'success': True,
            'total_count': total_count,
            'subscription_counts': subscription_counts
        })

    except Exception as e:
        logger.error("获取聚合节点数量失败: %s", safe_exception_details(e))
        return jsonify({'success': False, 'message': '获取聚合节点数量失败'}), 500


@bp.route('/<agg_id>/preview', methods=['GET'])
@require_auth
def preview_aggregation_nodes(agg_id):
    """预览聚合的节点列表"""
    config_data = get_resource_config('subscription_aggregations', agg_id)
    try:
        # 查找聚合
        aggregations = config_data.get('subscription_aggregations', [])
        agg_index = next((i for i, a in enumerate(aggregations) if a['id'] == agg_id), None)

        if agg_index is None:
            return jsonify({'success': False, 'message': 'Aggregation not found'}), 404

        aggregation = aggregations[agg_index]

        # 生成 provider 文件，获取统计数据
        result = generate_aggregation_provider(aggregation, config=config_data)
        file_path = result['file_path']
        subscription_node_counts = result['subscription_node_counts']
        total_count = result['total_count']

        # 读取生成的 YAML 文件
        with open(file_path, 'r', encoding='utf-8') as f:
            provider_data = yaml.safe_load(f)

        proxies = provider_data.get('proxies', [])

        return jsonify({
            'success': True,
            'count': len(proxies),
            'nodes': proxies,
            'subscription_node_counts': subscription_node_counts
        })

    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        logger.error("聚合操作失败: %s", safe_exception_details(e))
        return jsonify({'success': False, 'message': '聚合操作失败'}), 500


@bp.route('/<agg_id>/provider', methods=['GET'])
def get_aggregation_provider(agg_id):
    """获取聚合 provider YAML 文件

    用于为 Mihomo/Surge 等客户端提供聚合 provider 文件

    支持两种认证方式：
    1. JWT token（前端使用）
    2. URL query token（外部客户端使用，如 Mihomo/Surge）

    Args:
        agg_id: 聚合 ID

    Query params:
        token: 可选，URL token（用于外部客户端认证）

    Returns:
        YAML 文件内容
    """
    # 双重认证：JWT（前端） 或 URL token（外部客户端）
    config_data = get_resource_config('subscription_aggregations', agg_id)
    auth_result = validate_token_or_jwt(request)
    if not auth_result['valid']:
        return jsonify({'success': False, 'message': auth_result.get('message', 'Unauthorized')}), 401

    try:
        # 查找聚合
        aggregations = config_data.get('subscription_aggregations', [])
        aggregation = next((a for a in aggregations if a['id'] == agg_id), None)

        if not aggregation:
            return jsonify({'success': False, 'message': 'Aggregation not found'}), 404

        if not aggregation.get('enabled', True):
            return jsonify({'success': False, 'message': 'Aggregation is disabled'}), 403
    except Exception as e:
        logger.error("聚合操作失败: %s", safe_exception_details(e))
        return jsonify({'success': False, 'message': '聚合操作失败'}), 500

    from backend.utils.group_candidates import CandidateValidationError, filter_candidates
    from backend.utils.provider_response import requested_filters
    try:
        filters = requested_filters()
    except CandidateValidationError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400

    def render():
        try:
            # 生成 provider 文件（优先从URL获取订阅新数据并更新缓存，失败则使用本地缓存）
            result = generate_aggregation_provider(aggregation, config=config_data)

            # 如果请求 Surge 格式，转换为 Surge 纯文本
            if request.args.get('format') == 'surge':
                provider_data = yaml.safe_load(result['content'])
                proxies = provider_data.get('proxies', [])
                from backend.converters.surge import convert_proxies_to_surge_text
                surge_text = convert_proxies_to_surge_text(
                    filter_candidates(proxies, *filters) if filters else proxies)
                return Response(surge_text, mimetype='text/plain')

            if request.args.get('format') == 'loon':
                provider_data = yaml.safe_load(result['content'])
                from backend.converters.loon import convert_proxies_to_loon_text
                proxies = provider_data.get('proxies', [])
                loon_text = convert_proxies_to_loon_text(
                    filter_candidates(proxies, *filters) if filters else proxies)
                return Response(loon_text, mimetype='text/plain')

            # Serve the exact validated snapshot, not a concurrently replaced artifact.
            return Response(result['content'], mimetype='text/yaml')

        except StrategyReferenceError as e:
            return jsonify({'success': False, 'message': str(e)}), 400
        except Exception as e:
            logger.error("聚合操作失败: %s", safe_exception_details(e))
            return jsonify({'success': False, 'message': '聚合操作失败'}), 500

    from backend.utils.provider_response import serve_provider
    return serve_provider('aggregation', agg_id, resolve_profile_id(config_data.get('profile_id')),
                          config_data, lambda: make_response(render()))


@bp.route('/reorder', methods=['POST'])
@require_auth
def reorder_aggregations():
    """批量更新聚合顺序"""
    try:
        config_data = get_shared_config()
        body = request.json or {}
        new_order, missing = resolve_new_order(
            config_data.get('subscription_aggregations', []), body, 'aggregations'
        )
        if missing:
            return jsonify({'success': False, 'message': f'以下聚合 id 不存在: {missing}'}), 404
        config_data['subscription_aggregations'] = new_order
        save_shared_config(config_data)
        return jsonify({'success': True, 'order': [a.get('id') for a in new_order]})
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except ProfileRepositoryError:
        raise
    except Exception as e:
        logger.error("聚合操作失败: %s", safe_exception_details(e))
        return jsonify({'success': False, 'message': '聚合操作失败'}), 500
