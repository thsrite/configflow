"""订阅聚合路由模块

负责订阅聚合相关的业务逻辑和路由，包括：
- 聚合的增删改查
- 生成聚合 provider 文件
- 清理策略组中的聚合引用
"""
import os
import re
import uuid
import yaml
from typing import Dict, Any
from datetime import datetime
from flask import request, jsonify, send_file, current_app, Response

from backend.common.config import config_data, save_config, DATA_DIR
from backend.common.auth import validate_token_or_jwt, require_auth
from backend.routes import subscription_aggregations_bp as bp
from backend.converters.mihomo import convert_node_to_mihomo
from backend.utils.subscription_cache import load_subscription_cache, save_subscription_nodes
from backend.utils.sub_store_client import (
    get_subscription_proxies_yaml,
    parse_proxies_from_yaml,
    proxies_to_nodes,
)
from backend.utils.logger import get_logger

logger = get_logger(__name__)

# 聚合 provider 文件存储目录
AGGREGATION_PROVIDERS_DIR = os.path.join(DATA_DIR, 'providers')

# 确保目录存在
if not os.path.exists(AGGREGATION_PROVIDERS_DIR):
    os.makedirs(AGGREGATION_PROVIDERS_DIR)


# ============================================================================
# 业务逻辑函数
# ============================================================================

def generate_aggregation_provider(aggregation: Dict[str, Any]) -> Dict[str, Any]:
    """生成订阅聚合的 provider YAML 文件

    Args:
        aggregation: 聚合配置，包含 subscriptions、nodes、regex_filter 等字段

    Returns:
        Dict[str, Any]: 包含文件路径和节点统计数据
        {
            'file_path': str,  # 生成的 YAML 文件路径
            'subscription_node_counts': dict,  # 各订阅的节点数统计
            'total_count': int  # 总节点数
        }

    处理流程：
    1. 从选择的订阅中获取节点（优先从URL获取并更新缓存，失败则读取本地缓存）
    2. 添加手动选择的节点（避免重复）
    3. 应用正则过滤
    4. 转换为 mihomo 格式
    5. 生成并保存 YAML 文件
    """
    agg_id = aggregation['id']
    agg_name = aggregation['name']

    # 收集所有节点
    all_nodes = []

    # 记录各订阅的节点数统计（不保存到配置文件，仅用于返回）
    subscription_node_counts = {}

    # 1. 从选择的订阅中获取节点 - 优先通过 Sub-Store 获取
    # sub_proxies_map: sub_id -> proxies list（mihomo 格式，用于最终输出）
    sub_proxies_map = {}
    subscription_ids = aggregation.get('subscriptions', [])
    if subscription_ids:
        subscriptions = config_data.get('subscriptions', [])
        for sub_id in subscription_ids:
            sub = next((s for s in subscriptions if s['id'] == sub_id and s.get('enabled', True)), None)
            if sub:
                nodes_list = None

                # 优先通过 Sub-Store 获取
                try:
                    logger.info(f"尝试通过 Sub-Store 获取订阅最新数据: '{sub['name']}'")
                    yaml_text = get_subscription_proxies_yaml(sub_id, sub['url'])
                    proxies = parse_proxies_from_yaml(yaml_text)
                    sub_proxies_map[sub_id] = proxies

                    # 转换为 node 格式用于缓存和过滤
                    nodes_list = proxies_to_nodes(proxies)
                    for node in nodes_list:
                        node['subscription_id'] = sub_id
                        node['subscription_name'] = sub['name']
                        if 'id' not in node:
                            node['id'] = f"node_{uuid.uuid4().hex[:8]}"

                    # 保存到本地缓存
                    save_subscription_nodes(
                        sub_id,
                        nodes_list,
                        {
                            'subscription_name': sub['name'],
                            'url': sub.get('url')
                        }
                    )
                    logger.info(f"成功通过 Sub-Store 获取并更新缓存: '{sub['name']}', 节点数: {len(nodes_list)}")
                except Exception as e:
                    logger.warning(f"通过 Sub-Store 获取订阅 '{sub['name']}' 失败: {e}, 尝试读取本地缓存")

                # 如果从 Sub-Store 获取失败，从本地缓存读取
                if not nodes_list:
                    cache = load_subscription_cache(sub_id)
                    if cache:
                        nodes_list = cache.get('nodes', [])
                        logger.info(f"从本地缓存读取订阅 '{sub['name']}', 节点数: {len(nodes_list)}")
                    else:
                        logger.error(f"订阅 '{sub['name']}' 既无法从 Sub-Store 获取也没有本地缓存")
                        nodes_list = []

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
            logger.error(f"聚合 '{agg_name}' 的正则表达式无效: {e}")

    # 4. 转换为 mihomo 格式
    # 构建 sub-store proxies 按名称索引（用于快速查找）
    sub_store_proxy_by_name = {}
    for proxies_list in sub_proxies_map.values():
        for p in proxies_list:
            sub_store_proxy_by_name[p.get('name', '')] = p

    proxies = []
    for node in all_nodes:
        node_name = node.get('name', '')
        # 优先使用 Sub-Store 返回的原始 proxy（已经是 mihomo 格式）
        if node_name in sub_store_proxy_by_name:
            proxies.append(sub_store_proxy_by_name[node_name])
        else:
            # 手动节点或缓存降级节点，使用 convert_node_to_mihomo
            proxy = convert_node_to_mihomo(node)
            if proxy:
                proxies.append(proxy)

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

    # 6. 保存到文件
    file_path = os.path.join(AGGREGATION_PROVIDERS_DIR, f"{agg_id}.yaml")
    with open(file_path, 'w', encoding='utf-8') as f:
        f.write(yaml_content)

    logger.info(f"生成聚合 provider: {agg_name}, {len(proxies)} 个节点")

    # 7. 返回文件路径和统计数据（不保存到配置文件）
    return {
        'file_path': file_path,
        'subscription_node_counts': subscription_node_counts,
        'total_count': len(proxies)
    }


def clean_proxy_groups_aggregation(agg_id: str) -> bool:
    """从所有策略组中移除指定的聚合引用

    Args:
        agg_id: 聚合 ID

    Returns:
        bool: 是否有策略组被修改
    """
    proxy_groups = config_data.get('proxy_groups', [])
    modified = False

    for group in proxy_groups:
        aggregations = group.get('aggregations', [])
        if agg_id in aggregations:
            group['aggregations'] = [a for a in aggregations if a != agg_id]
            modified = True
            logger.info(f"从策略组 '{group.get('name')}' 中移除聚合 {agg_id}")

    return modified


def clean_invalid_proxy_group_aggregations() -> bool:
    """清理所有策略组中对已删除或已禁用聚合的引用

    Returns:
        bool: 是否有策略组被修改
    """
    proxy_groups = config_data.get('proxy_groups', [])
    if not proxy_groups:
        return False

    # 获取所有启用的聚合 ID
    enabled_aggregation_ids = {
        agg['id'] for agg in config_data.get('subscription_aggregations', [])
        if agg.get('enabled', True)
    }

    modified = False
    for group in proxy_groups:
        old_aggs = group.get('aggregations', [])
        new_aggs = [a for a in old_aggs if a in enabled_aggregation_ids]
        if len(new_aggs) != len(old_aggs):
            group['aggregations'] = new_aggs
            modified = True
            logger.info(f"清理策略组 '{group.get('name')}' 中的无效聚合引用")

    return modified


# ============================================================================
# 路由函数
# ============================================================================


@bp.route('', methods=['GET', 'POST'])
@require_auth
def handle_subscription_aggregations():
    """订阅聚合列表"""
    if request.method == 'GET':
        # 获取所有聚合（快速返回，不计算节点数）
        return jsonify(config_data.get('subscription_aggregations', []))

    elif request.method == 'POST':
        # 创建新聚合
        aggregation = request.json
        aggregation['id'] = f"agg_{uuid.uuid4().hex[:8]}"
        aggregation['created_at'] = datetime.now().isoformat()
        aggregation['updated_at'] = datetime.now().isoformat()

        # 移除不应该保存的统计字段
        aggregation.pop('node_count', None)
        aggregation.pop('total_node_count', None)
        aggregation.pop('subscription_node_counts', None)
        aggregation.pop('loading_count', None)

        if 'subscription_aggregations' not in config_data:
            config_data['subscription_aggregations'] = []

        config_data['subscription_aggregations'].append(aggregation)
        save_config()

        return jsonify({'success': True, 'data': aggregation})


@bp.route('/<agg_id>', methods=['GET', 'PUT', 'DELETE'])
@require_auth
def handle_subscription_aggregation_item(agg_id):
    """单个订阅聚合操作"""
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
                    old_enabled = a.get('enabled', True)
                    updated_aggregation = request.json
                    updated_aggregation['id'] = agg_id  # 确保ID不变
                    new_enabled = updated_aggregation.get('enabled', True)

                    # 更新修改时间
                    updated_aggregation['updated_at'] = datetime.now().isoformat()

                    # 移除不应该保存的统计字段
                    updated_aggregation.pop('node_count', None)
                    updated_aggregation.pop('total_node_count', None)
                    updated_aggregation.pop('subscription_node_counts', None)
                    updated_aggregation.pop('loading_count', None)  # 前端的加载状态也不应该保存

                    config_data['subscription_aggregations'][i] = updated_aggregation

                    # 如果聚合被禁用，从所有策略组中移除
                    if old_enabled and not new_enabled:
                        clean_proxy_groups_aggregation(agg_id)

                    save_config()
                    return jsonify({'success': True, 'data': updated_aggregation})

            return jsonify({'success': False, 'message': 'Aggregation not found'}), 404
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500

    elif request.method == 'DELETE':
        # 删除聚合
        config_data['subscription_aggregations'] = [a for a in aggregations if a['id'] != agg_id]
        # 从所有策略组中移除此聚合
        clean_proxy_groups_aggregation(agg_id)
        save_config()
        return jsonify({'success': True})


@bp.route('/<agg_id>/count', methods=['GET'])
@require_auth
def get_aggregation_node_count(agg_id):
    """获取聚合的节点数量（仅从本地缓存读取，不触发更新）"""
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
            for sub_id in subscription_ids:
                cache = load_subscription_cache(sub_id)
                if cache:
                    node_count = cache.get('count', 0)
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
        logger.error(f"获取聚合节点数量失败: {str(e)}")
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agg_id>/preview', methods=['GET'])
@require_auth
def preview_aggregation_nodes(agg_id):
    """预览聚合的节点列表"""
    try:
        # 查找聚合
        aggregations = config_data.get('subscription_aggregations', [])
        agg_index = next((i for i, a in enumerate(aggregations) if a['id'] == agg_id), None)

        if agg_index is None:
            return jsonify({'success': False, 'message': 'Aggregation not found'}), 404

        aggregation = aggregations[agg_index]

        # 生成 provider 文件，获取统计数据
        result = generate_aggregation_provider(aggregation)
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

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


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

        # 生成 provider 文件（优先从URL获取订阅新数据并更新缓存，失败则使用本地缓存）
        result = generate_aggregation_provider(aggregation)
        file_path = result['file_path']

        # 如果请求 Surge 格式，转换为 Surge 纯文本
        if request.args.get('format') == 'surge':
            with open(file_path, 'r', encoding='utf-8') as f:
                provider_data = yaml.safe_load(f)
            proxies = provider_data.get('proxies', [])
            from backend.converters.surge import convert_proxies_to_surge_text
            surge_text = convert_proxies_to_surge_text(proxies)
            return Response(surge_text, mimetype='text/plain')

        # 返回 YAML 文件
        return send_file(
            file_path,
            mimetype='text/yaml',
            as_attachment=False,
            download_name=f"{aggregation['name']}.yaml"
        )

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500
