"""节点管理路由"""
from flask import request, jsonify
import uuid

from backend.routes import nodes_bp
from backend.common.auth import require_auth
from backend.common.config import get_shared_config, update_shared_config_transaction, get_repository, reset_config_context
from backend.common.config_repository import ProfileRepositoryError, NodeDeletionChanged
from backend.utils.reorder import resolve_new_order


@nodes_bp.route('', methods=['GET', 'POST'])
@require_auth
def handle_nodes():
    """节点管理"""
    config_data = get_shared_config()

    if request.method == 'GET':
        return jsonify(config_data['nodes'])

    elif request.method == 'POST':
        node = request.json
        if not isinstance(node, dict):
            return jsonify({'success': False, 'message': '节点请求必须是 JSON 对象'}), 400
        # 如果没有 ID，生成一个唯一 ID
        if 'id' not in node:
            node['id'] = f"node_{uuid.uuid4().hex[:8]}"
        if 'dialer_ref' in node:
            return jsonify({'success': False, 'message': '共享节点不保存拨号引用，请在策略组中创建代理链'}), 400
        update_shared_config_transaction(
            lambda shared: shared.setdefault('nodes', []).append(node)
        )
        return jsonify({'success': True, 'data': node})


@nodes_bp.route('/parse-lines', methods=['POST'])
@require_auth
def parse_node_lines():
    """把 Surge / Loon / QX 等客户端的节点行（snell 等没有通用 URI 的协议）通过内置
    Sub-Store 转成 mihomo 节点，供批量添加保存为结构化 YAML。"""
    from backend.utils.sub_store_client import convert_proxy_string
    data = request.get_json(silent=True)
    lines = data.get('lines') if isinstance(data, dict) else None
    if not isinstance(lines, list) or not all(isinstance(line, str) for line in lines):
        return jsonify({'success': False, 'message': 'lines 必须是字符串数组'}), 400
    if len(lines) > 500:
        return jsonify({'success': False, 'message': '单次最多解析 500 行'}), 400
    results = []
    for line in lines:
        proxy = convert_proxy_string(line.strip()) if line.strip() else None
        results.append({'proxy': proxy} if proxy else {'error': '无法识别的节点格式'})
    return jsonify({'success': True, 'results': results})


@nodes_bp.route('/delete-preview', methods=['POST'])
@nodes_bp.route('/delete', methods=['POST'])
@require_auth
def delete_nodes_with_references():
    """Preview, then atomically detach ordinary references and delete selected nodes."""
    from werkzeug.exceptions import NotFound
    data = request.get_json(silent=True)
    if not isinstance(data, dict):
        return jsonify({'success': False, 'message': '节点请求必须是 JSON 对象'}), 400
    repository = get_repository()
    try:
        if request.path.endswith('/delete-preview'):
            return jsonify(repository.preview_node_deletion(data.get('ids')))
        result = repository.delete_nodes_with_references(data.get('ids'), data.get('confirmation'))
        reset_config_context()
        return jsonify({'success': True, 'deleted_ids': [node['id'] for node in result['nodes']]})
    except NodeDeletionChanged as error:
        return jsonify({'success': False, 'code': 'node_deletion_changed', 'message': str(error)}), 409
    except NotFound as error:
        return jsonify({'success': False, 'message': error.description}), 404


@nodes_bp.route('/<node_id>', methods=['DELETE', 'PUT'])
@require_auth
def handle_node(node_id):
    """单个节点操作"""
    from werkzeug.exceptions import Conflict, NotFound
    from backend.utils.dialer_references import DialerReferenceError
    new_data = request.get_json() if request.method == 'PUT' else None
    deleting = request.method == 'DELETE'
    if not deleting and not isinstance(new_data, dict):
        return jsonify({'success': False, 'message': '节点请求必须为非 null JSON 对象'}), 400
    if isinstance(new_data, dict) and 'dialer_ref' in new_data:
        return jsonify({'success': False, 'message': '共享节点不保存拨号引用，请在策略组中创建代理链'}), 400
    def mutate(profile):
        nodes = profile.setdefault('nodes', [])
        original = next((n for n in nodes if n.get('id') == node_id), None)
        if original is None:
            raise NotFound('Node not found')
        if not deleting:
            if new_data.get('id', node_id) != node_id:
                raise DialerReferenceError('节点 ID 不可修改')
            new_data['id'] = node_id
        if deleting:
            nodes.remove(original)
        else:
            nodes[nodes.index(original)] = new_data
    try:
        update_shared_config_transaction(mutate)
    except DialerReferenceError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    except (Conflict, NotFound) as exc:
        return jsonify({'success': False, 'message': exc.description}), exc.code
    return jsonify({'success': True, **({'data': new_data} if new_data is not None else {})})


@nodes_bp.route('/reorder', methods=['POST'])
@require_auth
def reorder_nodes():
    """批量更新节点顺序"""
    from backend.utils.dialer_references import DialerReferenceError
    from werkzeug.exceptions import NotFound
    order = []
    body = request.get_json() or {}
    def mutate(profile):
        new_order, missing = resolve_new_order(profile.get('nodes', []), body, 'nodes')
        if missing:
            raise NotFound(f'以下节点 id 不存在: {missing}')
        profile['nodes'] = new_order
        order.extend(item.get('id') for item in new_order)
    try:
        update_shared_config_transaction(mutate)
        return jsonify({'success': True, 'order': order})
    except DialerReferenceError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    except NotFound as exc:
        return jsonify({'success': False, 'message': exc.description}), 404
    except ProfileRepositoryError:
        raise
    except Exception as exc:
        return jsonify({'success': False, 'message': str(exc)}), 500


@nodes_bp.route('/latency', methods=['GET', 'POST'])
@require_auth
def node_latency():
    """代理延迟：GET 取最近结果，POST 分批测试完整节点配置。"""
    from backend.utils.node_latency import load_results, run_tests, ProbeBusy, ProbeUnavailable

    if request.method == 'GET':
        return jsonify({'success': True, 'results': load_results()})

    payload = request.get_json(silent=True)
    if payload is None:
        payload = {}
    if not isinstance(payload, dict):
        return jsonify({'success': False, 'message': '请求必须是 JSON 对象'}), 400
    names = payload.get('names')
    keys = payload.get('keys')
    if names is not None and (
        not isinstance(names, list) or not all(isinstance(n, str) for n in names)
    ):
        return jsonify({'success': False, 'message': 'names 必须是字符串数组'}), 400
    if keys is not None and (
        not isinstance(keys, list) or not all(isinstance(key, str) for key in keys)
    ):
        return jsonify({'success': False, 'message': 'keys 必须是字符串数组'}), 400
    if names is not None and keys is not None:
        return jsonify({'success': False, 'message': 'names 与 keys 不能同时指定'}), 400

    try:
        outcome = run_tests(names, keys=keys)
    except ProbeBusy as exc:
        return jsonify({'success': False, 'message': str(exc)}), 409
    except ProbeUnavailable as exc:
        return jsonify({'success': False, 'message': str(exc)}), 503
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    return jsonify({'success': True, **outcome})


@nodes_bp.route('/latency/settings', methods=['GET', 'POST'])
@require_auth
def node_latency_settings():
    """Global DNS selection; creating a managed endpoint is a separate operation."""
    from backend.utils.node_dns import describe_settings, normalize_dns_servers, save_config

    try:
        if request.method == 'GET':
            return jsonify({'success': True, **describe_settings()})
        payload = request.get_json(silent=True)
        if not isinstance(payload, dict):
            raise ValueError('请提供测速 DNS 设置')
        if 'dns' in payload:
            value = payload['dns']
        elif 'dns_servers' in payload:
            servers = normalize_dns_servers(payload['dns_servers'])
            value = {'mode': 'custom' if servers else 'system', 'servers': servers}
        else:
            raise ValueError('请提供测速 DNS 设置')
        return jsonify({'success': True, **save_config(value)})
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400


@nodes_bp.route('/latency/dns/check', methods=['POST'])
@require_auth
def node_dns_check():
    from backend.utils.node_dns import check_settings
    from backend.utils.node_latency import ProbeUnavailable
    payload = request.get_json(silent=True)
    try:
        if not isinstance(payload, dict) or 'dns' not in payload:
            raise ValueError('请提供待检测的 DNS 设置')
        return jsonify(check_settings(payload['dns']))
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    except ProbeUnavailable as exc:
        return jsonify({'success': False, 'message': str(exc)}), 503


def _verify_managed_dns(endpoint):
    from backend.utils.node_dns import check_settings
    return check_settings({'mode': 'custom', 'servers': [endpoint], 'bootstrap': []})


@nodes_bp.route('/latency/dns/managed', methods=['GET', 'POST'])
@require_auth
def node_dns_managed():
    from backend.agents import node_test_dns
    from backend.common.agent_manager import get_agent_manager
    manager = get_agent_manager()
    try:
        if request.method == 'GET':
            result = node_test_dns.options(manager)
        else:
            payload = request.get_json(silent=True)
            if not isinstance(payload, dict):
                raise ValueError('请提供 OxiDNS 托管接口设置')
            result = node_test_dns.create(manager, payload, verify=_verify_managed_dns)
        return jsonify({'success': True, **result})
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400


@nodes_bp.route('/latency/dns/managed/<agent_id>/<endpoint_id>', methods=['GET', 'DELETE'])
@require_auth
def node_dns_managed_endpoint(agent_id, endpoint_id):
    from backend.agents import node_test_dns
    from backend.common.agent_manager import get_agent_manager
    manager = get_agent_manager()
    try:
        if request.method == 'DELETE':
            result = node_test_dns.remove(manager, agent_id, endpoint_id)
        else:
            result = node_test_dns.status(manager, agent_id, endpoint_id, verify=_verify_managed_dns)
        return jsonify({'success': True, **result})
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
