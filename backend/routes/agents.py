"""Agent 管理路由模块

提供 Agent 的注册、管理、配置推送等功能
"""
import hmac
import json
import math
import os
from urllib.parse import urlsplit, urlunsplit
from flask import request, jsonify, send_file

from backend.agents.config_generator import generate_agent_config
from backend.agents.manager import AgentDeploymentConflict
from backend.agents.traffic_store import REPORT_MAX_CONTENT_LENGTH, get_traffic_store, validate_report
from backend.agents.version import (
    LATEST_AGENT_VERSION,
    compare_versions,
    get_latest_version,
    has_update,
)
from backend.converters.mihomo import generate_mihomo_config, get_mihomo_provider_downloads, get_mihomo_ruleset_downloads
from backend.converters.mosdns import generate_mosdns_config, get_mosdns_ruleset_downloads, get_mosdns_custom_files
from backend.converters.surge import generate_surge_config
from backend.routes import agents_bp as bp
from backend.common.auth import is_token_within_length, parse_bearer_token, require_auth
from backend.common.config import get_config, get_repository
from backend.common.config_repository import ProfileRepositoryError
from backend.common.agent_manager import get_agent_manager
from backend.common.utils import str_to_bool
from backend.utils.logger import get_logger
from backend.utils.url_utils import safe_url_for_log
from backend.utils.strategy_references import StrategyReferenceError
from backend.utils.rule_fetch import is_internal_rule_url, request_rule
from backend.utils.domain_discovery_service import provider_revisions as discovery_provider_revisions
from backend.utils.domain_probe import get_probe_store
from backend.utils.region_check import get_region_store

logger = get_logger(__name__)

# 支持「配置落盘后由 Agent 自行重启」的最低 Agent 版本。
# 更早的版本不认识 restart_after_update 字段，只能由服务端触发重启。
_SELF_RESTART_MIN_VERSION = "1.1.0-go"
_HEARTBEAT_MAX_CONTENT_LENGTH = 32 * 1024
_HEARTBEAT_FIELDS = frozenset({'version', 'config_version', 'service_status', 'system_metrics'})
_METRIC_FIELDS = {
    'cpu': frozenset({'usage_percent', 'core_count'}),
    'memory': frozenset({'total', 'used', 'available', 'used_percent'}),
    'disk': frozenset({'total', 'used', 'free', 'used_percent'}),
    'network': frozenset({'bytes_sent', 'bytes_recv', 'speed_sent', 'speed_recv'}),
}
_PERCENT_FIELDS = frozenset({'usage_percent', 'used_percent'})
_REGISTRATION_LOG_FIELDS = ('name', 'host', 'port', 'service_type', 'version')
_REGISTRATION_LOG_VALUE_MAX_LENGTH = 128


def _bounded_log_value(value):
    """Return a bounded scalar safe for a single-line registration log."""
    if isinstance(value, bool) or not isinstance(value, (str, int, float)):
        return None
    text = str(value)
    control_index = next(
        (index for index, char in enumerate(text) if not char.isprintable()),
        len(text),
    )
    return text[:control_index][:_REGISTRATION_LOG_VALUE_MAX_LENGTH]


def _registration_log_fields(agent_data):
    """Select only explicitly public, scalar registration fields for logging."""
    safe_fields = {}
    for field in _REGISTRATION_LOG_FIELDS:
        if field not in agent_data:
            continue
        safe_value = _bounded_log_value(agent_data[field])
        if safe_value is not None:
            safe_fields[field] = safe_value
    return safe_fields


def _valid_metric_number(field, value):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value):
        return False
    if field in _PERCENT_FIELDS:
        return 0 <= value <= 100
    if not isinstance(value, int) or value < 0:
        return False
    return value <= (65536 if field == 'core_count' else (2 ** 64 - 1))


def _validate_system_metrics(metrics):
    if not isinstance(metrics, dict) or set(metrics) - (set(_METRIC_FIELDS) | {'collected_at'}):
        return False
    if len(json.dumps(metrics, ensure_ascii=False).encode('utf-8')) > 16 * 1024:
        return False
    collected_at = metrics.get('collected_at')
    if collected_at is not None and (not isinstance(collected_at, str) or len(collected_at) > 64):
        return False
    for section, allowed_fields in _METRIC_FIELDS.items():
        if section not in metrics:
            continue
        values = metrics[section]
        if not isinstance(values, dict) or set(values) - allowed_fields:
            return False
        if any(not _valid_metric_number(field, value) for field, value in values.items()):
            return False
    return True


def _validate_heartbeat_payload(payload):
    if not isinstance(payload, dict) or set(payload) - _HEARTBEAT_FIELDS:
        return False
    for field, max_length in (('version', 128), ('config_version', 128), ('service_status', 64)):
        if field in payload:
            value = payload[field]
            if not isinstance(value, str) or len(value) > max_length:
                return False
    return 'system_metrics' not in payload or _validate_system_metrics(payload['system_metrics'])


def _constant_time_ascii_equal(provided, expected):
    if not isinstance(provided, str) or not isinstance(expected, str):
        return False
    try:
        return hmac.compare_digest(provided.encode('ascii'), expected.encode('ascii'))
    except UnicodeEncodeError:
        return False


def _supports_self_restart(agent_version: str) -> bool:
    """判断 Agent 是否支持配置落盘后自重启"""
    if not agent_version:
        return False
    return compare_versions(agent_version, _SELF_RESTART_MIN_VERSION) >= 0


def _public_agent(agent):
    """Return an API-safe copy of an Agent without its capability token."""
    return {key: value for key, value in agent.items() if key != 'token'}


@bp.route('/install-script', methods=['GET'])
def get_install_script():
    """生成 Agent 安装脚本（默认 Go 版本，可选 Shell 版本）"""
    try:
        import os
        from backend.agents.go_install_script import generate_go_agent_install_script
        from backend.agents.install_script import generate_lightweight_install_script

        # 获取参数
        agent_name = request.args.get('name', 'My-Agent')
        service_type = request.args.get('type', 'mihomo')
        agent_port = request.args.get('port', 8080, type=int)
        agent_ip = request.args.get('agent_ip', '').strip()  # 可选的 Agent IP
        config_path = request.args.get('config_path', f'/etc/{service_type}/config.yaml')
        restart_command = request.args.get('restart_command', f'systemctl restart {service_type}')

        # agent_type: 'go' (默认) 或 'shell'
        agent_type = request.args.get('agent_type', 'go').lower().strip()

        logger.info(f"生成安装脚本请求 - 名称: {agent_name}, 类型: {service_type}, 端口: {agent_port}, Agent类型: {agent_type}")

        # 检查模板文件是否存在
        scripts_dir = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'agents', 'scripts')
        # logger.info(f"脚本模板目录: {scripts_dir}")
        # logger.info(f"模板目录是否存在: {os.path.exists(scripts_dir)}")
        # if os.path.exists(scripts_dir):
        #     logger.info(f"模板目录内容: {os.listdir(scripts_dir)}")

        # 获取服务器 URL
        # 优先使用前端传递的 server_url 参数（包含协议+IP/域名+端口）
        server_url = request.args.get('server_url', '').strip()

        # 如果前端没有传递，则自动获取
        if not server_url:
            # 使用 request.url_root 会自动包含 scheme://host:port
            server_url = request.url_root.rstrip('/')

            # 如果有反向代理，尝试从请求头获取
            forwarded_proto = request.headers.get('X-Forwarded-Proto')
            forwarded_host = request.headers.get('X-Forwarded-Host')

            if forwarded_proto and forwarded_host:
                server_url = f"{forwarded_proto}://{forwarded_host}"

        # logger.info(f"服务器URL: {server_url}")

        # 根据 agent_type 生成对应的脚本
        if agent_type == 'shell':
            # Shell 版本（兼容性更好）
            logger.info("生成 Shell 版本安装脚本")
            script = generate_lightweight_install_script(
                server_url=server_url,
                agent_name=agent_name,
                service_type=service_type,
                agent_port=agent_port,
                agent_ip=agent_ip,
                config_path=config_path,
                restart_command=restart_command
            )
        else:
            # Go 版本（默认，性能更好）
            logger.info("生成 Go 版本安装脚本")
            binary_download_url = f"{server_url}/api/agents/download"
            script = generate_go_agent_install_script(
                server_url=server_url,
                agent_name=agent_name,
                service_type=service_type,
                agent_port=agent_port,
                agent_ip=agent_ip,
                config_path=config_path,
                restart_command=restart_command,
                binary_download_url=binary_download_url,
                **{key: request.args.get(key, '').strip() for key in (
                    'service_manager', 'service_unit', 'service_binary', 'stop_command',
                    'start_command', 'status_command', 'health_url', 'health_dns_address', 'health_dns_name')},
                deployment_health_timeout=request.args.get('deployment_health_timeout', 30, type=int),
            )

        logger.info(f"安装脚本生成成功，长度: {len(script)} 字符")
        return script, 200, {'Content-Type': 'text/plain; charset=utf-8'}

    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400

    except FileNotFoundError as e:
        import traceback
        error_detail = traceback.format_exc()
        logger.error(f"模板文件未找到: {str(e)}")
        logger.error(f"错误详情:\n{error_detail}")

        # 提供更友好的错误信息
        error_msg = f"Agent安装脚本模板文件缺失。请确保Docker镜像构建时包含了 backend/agents/scripts/ 目录下的所有 .sh 文件。错误: {str(e)}"
        return jsonify({'success': False, 'message': error_msg}), 500

    except Exception as e:
        import traceback
        error_detail = traceback.format_exc()
        logger.error(f"生成安装脚本失败: {str(e)}")
        logger.error(f"错误详情:\n{error_detail}")
        logger.error(f"请求参数: name={request.args.get('name')}, type={request.args.get('type')}, port={request.args.get('port')}, agent_type={request.args.get('agent_type')}")
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/register', methods=['POST'])
def register_agent():
    """Agent 注册"""
    try:
        agent_manager = get_agent_manager()

        # 获取 JSON 数据
        agent_data = request.get_json(force=True, silent=False)

        if agent_data is None:
            logger.error("Failed to parse JSON data")
            return jsonify({'success': False, 'message': 'Invalid JSON data'}), 400

        # 可选的注册密钥验证
        registration_key = os.environ.get('AGENT_REGISTRATION_KEY', '')
        if registration_key:
            provided_key = agent_data.get('registration_key', '')
            if provided_key != registration_key:
                logger.warning("Agent 注册被拒绝：注册密钥不匹配")
                return jsonify({'success': False, 'message': 'Invalid registration key'}), 403

        # 移除注册密钥（不存入配置，并先于日志记录以防止泄露）
        agent_data.pop('registration_key', None)

        # 获取客户端真实 IP（用于 Docker 容器等场景）
        agent_host = agent_data.get('host', '')
        client_ip = request.headers.get('X-Forwarded-For', request.remote_addr)
        if client_ip and ',' in client_ip:
            client_ip = client_ip.split(',')[0].strip()

        # 检查是否是 Docker 容器内网 IP 或回环地址
        is_docker_ip = False
        if agent_host:
            parts = agent_host.split('.')
            if len(parts) == 4 and parts[0] == '172':
                try:
                    second_octet = int(parts[1])
                    if 17 <= second_octet <= 31:
                        is_docker_ip = True
                except ValueError:
                    pass
            if agent_host.startswith('127.') or agent_host == 'localhost':
                is_docker_ip = True

        # 如果是 Docker 容器 IP 或没有提供 host，使用客户端 IP
        if not agent_host or is_docker_ip:
            agent_data['host'] = client_ip

        logger.info("Agent注册字段: %s", _registration_log_fields(agent_data))

        # 已有 Agent 的重注册必须使用其当前能力令牌。注册密钥仅控制首次
        # 注册资格，不能替代已有 Agent 的令牌认证。
        existing_token = parse_bearer_token(
            request.headers.get('Authorization', '')
        )

        # host 已按真实客户端 IP 归一化后，才允许 Manager 做 name/host 匹配。
        result = agent_manager.register_agent(agent_data, existing_token=existing_token)

        response_data = {'success': True, **result}
        logger.info(f"注册成功: {result.get('id')}")

        return jsonify(response_data), 200

    except PermissionError:
        return jsonify({'success': False, 'message': 'Unauthorized'}), 401

    except Exception as e:
        import traceback
        error_detail = traceback.format_exc()
        logger.error(f"Agent注册失败: {e}")
        logger.error(f"错误详情: {error_detail}")
        return jsonify({'success': False, 'message': str(e)}), 500


def _authenticated_agent(agent_manager, agent_id):
    """按 Bearer token 认证 Agent 自身发起的请求，失败返回 None。"""
    agent = agent_manager.get_agent_by_id(agent_id)
    provided_token = parse_bearer_token(request.headers.get('Authorization', ''))
    expected_token = agent.get('token', '') if agent else ''
    if (
        provided_token is None
        or not isinstance(expected_token, str)
        or not expected_token
        or not _constant_time_ascii_equal(provided_token, expected_token)
    ):
        return None
    return agent


def _read_json_body(max_length, invalid_message):
    """读取有大小上限的 JSON 请求体，返回 (数据, 错误响应)。"""
    if request.content_length is not None and request.content_length > max_length:
        return None, (jsonify({'success': False, 'message': 'Request body too large'}), 413)
    raw_body = request.stream.read(max_length + 1)
    if len(raw_body) > max_length:
        return None, (jsonify({'success': False, 'message': 'Request body too large'}), 413)
    if not request.is_json:
        return None, (jsonify({'success': False, 'message': invalid_message}), 400)
    try:
        return json.loads(raw_body), None
    except (json.JSONDecodeError, UnicodeDecodeError):
        return None, (jsonify({'success': False, 'message': invalid_message}), 400)


def _domain_discovery_enabled(agent):
    return bool(agent.get('domain_discovery_enabled')) and agent.get('service_type') == 'mihomo'


@bp.route('/<agent_id>/heartbeat', methods=['POST'])
def agent_heartbeat(agent_id):
    """Agent 心跳"""
    try:
        agent_manager = get_agent_manager()
        agent = _authenticated_agent(agent_manager, agent_id)
        if agent is None:
            return jsonify({'success': False, 'message': 'Unauthorized'}), 401

        heartbeat_data, error = _read_json_body(_HEARTBEAT_MAX_CONTENT_LENGTH, 'Invalid heartbeat data')
        if error:
            return error
        if not _validate_heartbeat_payload(heartbeat_data):
            return jsonify({'success': False, 'message': 'Invalid heartbeat data'}), 400

        result = agent_manager.update_heartbeat(agent_id, heartbeat_data)
        if result:
            # 旧 Agent 会忽略多出的字段；新 Agent 据此启停域名发现
            return jsonify({'success': True, 'domain_discovery_enabled': _domain_discovery_enabled(agent)}), 200
        return jsonify({'success': False, 'message': 'Agent not found'}), 404
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/traffic-report', methods=['POST'])
def agent_traffic_report(agent_id):
    """Agent 上报 mihomo 流量汇总（域名发现）"""
    agent_manager = get_agent_manager()
    agent = _authenticated_agent(agent_manager, agent_id)
    if agent is None:
        return jsonify({'success': False, 'message': 'Unauthorized'}), 401
    report, error = _read_json_body(REPORT_MAX_CONTENT_LENGTH, 'Invalid traffic report')
    if error:
        return error
    if not _domain_discovery_enabled(agent):
        return jsonify({'success': False, 'enabled': False, 'message': 'Domain discovery disabled'}), 409
    if not validate_report(report):
        return jsonify({'success': False, 'message': 'Invalid traffic report'}), 400
    get_traffic_store().add_report(agent_id, report)
    return jsonify({
        'success': True,
        'enabled': True,
        'rule_providers': discovery_provider_revisions(agent.get('profile_id', 'default')),
    }), 200


@bp.route('/<agent_id>/domain-discovery', methods=['PUT'])
@require_auth
def set_agent_domain_discovery(agent_id):
    """开关 Agent 的域名发现"""
    payload = request.get_json(silent=True)
    if not isinstance(payload, dict) or not isinstance(payload.get('enabled'), bool):
        return jsonify({'success': False, 'message': 'enabled 必须是布尔值'}), 400
    try:
        agent = get_agent_manager().set_domain_discovery(agent_id, payload['enabled'])
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    except AgentDeploymentConflict as exc:
        return jsonify({'success': False, 'message': str(exc)}), 409
    if agent is None:
        return jsonify({'success': False, 'message': 'Agent not found'}), 404
    return jsonify({'success': True, 'data': _public_agent(agent)}), 200


@bp.route('/<agent_id>/domain-discovery/clear', methods=['POST'])
@require_auth
def clear_agent_domain_discovery(agent_id):
    """清除 Agent 已采集的域名发现数据"""
    if not get_agent_manager().get_agent_by_id(agent_id):
        return jsonify({'success': False, 'message': 'Agent not found'}), 404
    get_traffic_store().delete(agent_id)
    get_probe_store().delete(agent_id)
    get_region_store().delete(agent_id)
    return jsonify({'success': True}), 200


@bp.route('/<agent_id>/config', methods=['GET'])
def get_agent_config(agent_id):
    """获取 Agent 的配置文件"""
    try:
        # 从查询参数获取 token
        token = request.args.get('token')
        if not is_token_within_length(token):
            return jsonify({'success': False, 'message': 'Token required'}), 401

        agent_manager = get_agent_manager()
        # 验证 token
        agent = agent_manager.get_agent_by_token(token)
        if not agent or agent['id'] != agent_id:
            return jsonify({'success': False, 'message': 'Invalid token'}), 401

        # 生成配置
        profile_id = agent.get('profile_id', 'default')
        config_result = generate_agent_config(
            get_config(profile_id), agent, base_url=request.url_root.rstrip('/'))

        return jsonify({
            'success': True,
            'profile_id': profile_id,
            'content': config_result['content'],
            'md5': config_result['md5'],
            'version': config_result['version']
        }), 200

    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except ProfileRepositoryError as e:
        return jsonify({'success': False, 'message': f'Agent profile unavailable: {e}'}), 409
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/download/<filename>')
def download_agent_binary(filename):
    """提供 Go Agent 二进制文件下载"""
    try:
        # 优先使用 /opt/configflow/static/agents（Docker 部署）
        # 否则使用相对路径（开发环境）
        agents_dir = os.getenv('AGENTS_STATIC_DIR', '/opt/configflow/static/agents')
        if not os.path.exists(agents_dir):
            agents_dir = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'static', 'agents')

        # 安全检查：只允许下载特定的文件
        allowed_files = [
            'configflow-agent-linux-amd64',
            'configflow-agent-linux-arm64',
            'configflow-agent-linux-armv7'
        ]

        if filename not in allowed_files:
            return jsonify({'success': False, 'message': 'Invalid filename'}), 404

        filepath = os.path.join(agents_dir, filename)
        if not os.path.exists(filepath):
            logger.error(f"Agent binary not found: {filepath}")
            return jsonify({'success': False, 'message': 'File not found'}), 404

        return send_file(filepath, as_attachment=True, download_name=filename, mimetype='application/octet-stream')

    except Exception as e:
        logger.error(f"Error downloading agent binary: {str(e)}")
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('', methods=['GET', 'POST'])
@require_auth
def handle_agents():
    """Agent 列表管理"""
    agent_manager = get_agent_manager()

    if request.method == 'GET':
        agents = [_public_agent(agent) for agent in agent_manager.get_all_agents()]
        from backend.agents.upgrades import artifacts
        upgrade_files_ready = bool(artifacts())
        # 为每个 agent 添加 has_update 字段
        for agent in agents:
            current_version = agent.get('version', '0.0.0')
            agent['has_update'] = has_update(current_version)
            agent['upgrade_available'] = upgrade_files_ready and agent.get('deployment_method') == 'shell'
        return jsonify(agents), 200

    elif request.method == 'POST':
        # 手动添加 Agent（非 Agent 自注册）
        try:
            agent_data = request.json
            result = agent_manager.register_agent(agent_data)
            return jsonify({'success': True, 'data': result}), 200
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>', methods=['GET', 'PUT', 'DELETE'])
@require_auth
def handle_agent_item(agent_id):
    """单个 Agent 操作"""
    agent_manager = get_agent_manager()

    if request.method == 'GET':
        agent = agent_manager.get_agent_by_id(agent_id)
        if agent:
            return jsonify(_public_agent(agent)), 200
        else:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404

    elif request.method == 'PUT':
        try:
            agent_data = request.json
            result = agent_manager.update_agent(agent_id, agent_data)
            if result:
                return jsonify({'success': True, 'data': result}), 200
            else:
                return jsonify({'success': False, 'message': 'Agent not found'}), 404
        except AgentDeploymentConflict as exc:
            return jsonify({'success': False, 'message': str(exc)}), 409
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500

    elif request.method == 'DELETE':
        try:
            result = agent_manager.delete_agent(agent_id)
            if result:
                get_traffic_store().delete(agent_id)
                get_probe_store().delete(agent_id)
                get_region_store().delete(agent_id)
                return jsonify({'success': True}), 200
            else:
                return jsonify({'success': False, 'message': 'Agent not found'}), 404
        except AgentDeploymentConflict as exc:
            return jsonify({'success': False, 'message': str(exc)}), 409
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/restart', methods=['POST'])
@require_auth
def restart_agent(agent_id):
    """重启 Agent 服务"""
    try:
        agent_manager = get_agent_manager()
        result = agent_manager.restart_agent_service(agent_id)
        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/status', methods=['GET'])
@require_auth
def get_agent_status(agent_id):
    """获取 Agent 状态"""
    agent_manager = get_agent_manager()
    agent = agent_manager.get_agent_by_id(agent_id)

    if agent:
        return jsonify({'success': True, 'status': agent.get('status', 'unknown')}), 200
    else:
        return jsonify({'success': False, 'message': 'Agent not found'}), 404


@bp.route('/<agent_id>/logs', methods=['GET'])
@require_auth
def get_agent_logs(agent_id):
    """获取 Agent 日志"""
    try:
        lines = request.args.get('lines', 100, type=int)
        log_path = request.args.get('log_path', '')  # 可选的自定义日志路径
        agent_manager = get_agent_manager()
        result = agent_manager.get_agent_logs(agent_id, lines, log_path=log_path)
        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/logs/clear', methods=['POST'])
@require_auth
def clear_agent_log(agent_id):
    """清空 Agent 指定日志文件"""
    try:
        data = request.get_json() or {}
        log_path = data.get('log_path', '')

        if not log_path:
            return jsonify({'success': False, 'message': '日志路径不能为空'}), 400

        agent_manager = get_agent_manager()
        result = agent_manager.clear_agent_log(agent_id, log_path)
        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/logs/validate', methods=['POST'])
@require_auth
def validate_log_path(agent_id):
    """验证自定义日志路径是否有效"""
    try:
        data = request.get_json() or {}
        log_path = data.get('path', '')

        if not log_path:
            return jsonify({'success': False, 'message': '日志路径不能为空'}), 400

        agent_manager = get_agent_manager()
        result = agent_manager.validate_agent_log_path(agent_id, log_path)
        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/config/logging', methods=['GET'])
@require_auth
def get_logging_config(agent_id):
    """获取 Agent 日志配置状态"""
    try:
        agent_manager = get_agent_manager()
        result = agent_manager.get_logging_config(agent_id)
        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/config/logging', methods=['POST'])
@require_auth
def set_logging_config(agent_id):
    """设置 Agent 日志启用/禁用"""
    try:
        data = request.get_json() or {}
        enabled = data.get('enabled', True)

        agent_manager = get_agent_manager()
        result = agent_manager.set_logging_config(agent_id, enabled)

        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/uninstall', methods=['POST'])
@require_auth
def uninstall_agent(agent_id):
    """卸载 Agent"""
    try:
        agent_manager = get_agent_manager()
        result = agent_manager.uninstall_agent(agent_id)
        if result.get('success'):
            # 卸载成功后从数据库删除
            agent_manager.delete_agent(agent_id)
        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/update', methods=['POST'])
@require_auth
def update_agent_version(agent_id):
    """Create a durable upgrade; acceptance is not completion."""
    from backend.agents.upgrades import start
    try:
        result = start(get_agent_manager(), agent_id)
        return jsonify(result), 202 if result['status'] != 'failed' else 409
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 409


@bp.route('/<agent_id>/upgrade', methods=['GET'])
def get_agent_upgrade(agent_id):
    from backend.agents.upgrades import poll
    manager = get_agent_manager()
    agent = manager.get_agent_by_id(agent_id)
    provided = parse_bearer_token(request.headers.get('Authorization', ''))
    if agent and provided and _constant_time_ascii_equal(provided, agent.get('token', '')):
        # Legacy bootstrap reads its own pending plan; no recursive Agent call.
        return jsonify(agent.get('latest_upgrade') or {}), 200
    @require_auth
    def for_admin():
        try:
            return jsonify(poll(manager, agent_id)), 200
        except ValueError as exc:
            return jsonify({'success': False, 'message': str(exc)}), 404
    return for_admin()


@bp.route('/<agent_id>/upgrade/report', methods=['POST'])
def report_agent_upgrade(agent_id):
    from backend.agents.upgrades import report_legacy
    manager = get_agent_manager()
    agent = manager.get_agent_by_id(agent_id)
    provided = parse_bearer_token(request.headers.get('Authorization', ''))
    if not agent or not provided or not _constant_time_ascii_equal(provided, agent.get('token', '')):
        return jsonify({'success': False}), 401
    if request.content_length is None or request.content_length > 4096:
        return jsonify({'success': False}), 413
    try:
        return jsonify(report_legacy(manager, agent_id, request.get_json())), 200
    except (ValueError, TypeError, AttributeError):
        return jsonify({'success': False, 'message': 'Invalid update report'}), 400


# Docker 相关路由（安装脚本生成）

@bp.route('/docker-agent-compose', methods=['GET'])
@require_auth
def get_docker_agent_compose():
    """生成 Docker Agent Compose 配置 (统一接口)"""
    from backend.agents.install_script import generate_docker_agent_compose

    try:
        params = {
            'server_url': request.args.get('server_url', ''),
            'agent_name': request.args.get('agent_name', 'agent'),
            'agent_ip': request.args.get('agent_ip', ''),
            'data_dir': request.args.get('data_dir', './agent_data'),
            'network_mode': request.args.get('network_mode', 'host'),
            'enable_mihomo': str_to_bool(request.args.get('enable_mihomo', 'true')),
            'enable_mosdns': str_to_bool(request.args.get('enable_mosdns', 'false')),
            'mihomo_port': request.args.get('mihomo_port', 8080, type=int),
            'mosdns_port': request.args.get('mosdns_port', 8081, type=int)
        }

        script = generate_docker_agent_compose(**params)
        return script, 200, {'Content-Type': 'text/yaml; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/docker-agent-run', methods=['GET'])
@require_auth
def get_docker_agent_run():
    """生成 Docker Agent Run 命令 (统一接口)"""
    from backend.agents.install_script import generate_docker_agent_run

    try:
        params = {
            'server_url': request.args.get('server_url', ''),
            'agent_name': request.args.get('agent_name', 'agent'),
            'agent_ip': request.args.get('agent_ip', ''),
            'data_dir': request.args.get('data_dir', './agent_data'),
            'network_mode': request.args.get('network_mode', 'host'),
            'enable_mihomo': str_to_bool(request.args.get('enable_mihomo', 'true')),
            'enable_mosdns': str_to_bool(request.args.get('enable_mosdns', 'false')),
            'mihomo_port': request.args.get('mihomo_port', 8080, type=int),
            'mosdns_port': request.args.get('mosdns_port', 8081, type=int)
        }

        script = generate_docker_agent_run(**params)
        return script, 200, {'Content-Type': 'text/plain; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


def _prefetch_download_contents(downloads, base_url, *, validation_urls=(), config_data=None):
    """预获取所有下载项的文件内容，写入 item['content']

    Args:
        downloads: provider_downloads + ruleset_downloads 列表
        base_url: 服务器基础 URL（server_domain 或前端传递的 base_url）
    """
    if not downloads:
        return

    import requests
    from concurrent.futures import ThreadPoolExecutor, as_completed
    if config_data is None:
        from backend.common.config import get_system_config
        config_data = get_system_config()
    # Capture the callback origin before entering workers, which have no Flask
    # request context. Only known API callbacks receive the loopback rewrite.
    callback_settings = dict(config_data.get('system_config', {}))
    if base_url:
        callback_settings['server_domain'] = base_url
    callback_config = {'system_config': callback_settings}
    callback_base = callback_settings.get('server_domain', '')

    def fetch_one(item):
        url = item.get('url', '')
        if not url:
            return
        try:
            if is_internal_rule_url(url, callback_config):
                parsed = urlsplit(url)
                prefix = urlsplit(callback_base).path.rstrip('/')
                fetch_url = urlunsplit((
                    'http', '127.0.0.1:5001', parsed.path[len(prefix):], parsed.query, '',
                ))
                internal_config = {'system_config': {
                    **callback_settings, 'server_domain': 'http://127.0.0.1:5001',
                }}
                # This recognized callback is direct even when environment
                # proxies are set; request_rule also contains its redirects.
                resp = request_rule(fetch_url, timeout=30, config_data=internal_config)
            elif url in validation_urls:
                # Legacy provider prefetch keeps its existing transport; the
                # rule-only setting must not affect subscription providers.
                resp = requests.get(url, timeout=30)
            else:
                resp = request_rule(url, timeout=30, config_data=config_data)
            if resp.status_code == 400 and url in validation_urls:
                raise StrategyReferenceError('Provider validation failed (HTTP 400)')
            resp.raise_for_status()
            item['content'] = resp.text
            logger.info(f"预获取成功: {item.get('name') or safe_url_for_log(url)} ({len(resp.text)} 字符)")
        except StrategyReferenceError:
            raise
        except Exception as e:
            logger.warning(f"预获取失败: {item.get('name') or safe_url_for_log(url)}, Agent 将 fallback 到 URL 下载")
            item['content'] = ''

    with ThreadPoolExecutor(max_workers=8) as executor:
        futures = {executor.submit(fetch_one, item): item for item in downloads}
        for future in as_completed(futures):
            future.result()  # 触发异常日志（已在 fetch_one 内处理）


def _profile_revision(profile_id):
    """推送时配置空间的修订号，界面据此标记 Agent「待更新」。"""
    try:
        return get_repository().config_revision(profile_id)
    except ProfileRepositoryError:
        return None


@bp.route('/<agent_id>/push-config', methods=['POST'])
@require_auth
def push_config_to_agent(agent_id):
    """Prepare one profile snapshot and publish a complete transactional archive."""
    from copy import deepcopy
    import uuid
    import yaml
    from backend.agents.deployment_bundle import build_deployment_bundle, DeploymentPreparationError
    from backend.utils.provider_delivery import prepare_provider_bundle, commit_provider_bundle

    agent_manager = get_agent_manager()
    deployment_id = None
    owns_preparation = False
    try:
        agent = agent_manager.get_agent_by_id(agent_id)
        if not agent:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return jsonify({'success': False, 'message': 'Expected a JSON object'}), 400
        base_url = data.get('base_url', '').strip()
        if not base_url:
            scheme = request.headers.get('X-Forwarded-Proto', request.scheme)
            host = request.headers.get('X-Forwarded-Host', request.host)
            base_url = f'{scheme}://{host}'
        profile_id = agent.get('profile_id', 'default')
        try:
            config_data = deepcopy(get_config(profile_id))
        except ProfileRepositoryError as exc:
            return jsonify({'success': False, 'message': f'Agent profile unavailable: {exc}'}), 409
        service_type = agent.get('service_type', 'mihomo')
        if service_type == 'surge':
            content = generate_surge_config(config_data, base_url=base_url)
            restart = data.get('restart', True)
            self_restart = _supports_self_restart(agent.get('version') or '')
            extra = {'restart_after_update': True} if restart and self_restart else None
            result = agent_manager.push_config_to_agent(agent_id, content, extra_data=extra,
                                                        config_revision=_profile_revision(profile_id))
            if result.get('success'):
                result['profile_id'] = profile_id
                if restart:
                    result['restart'] = ({'success': True, 'message': 'Restart delegated to agent after config is written'}
                                         if self_restart else agent_manager.restart_agent_service(agent_id))
            return jsonify(result), 200 if result.get('success') else 500
        if service_type not in ('mihomo', 'mosdns'):
            return jsonify({'success': False, 'message': 'Unsupported service type'}), 400
        activate = data.get('restart', True)
        if not isinstance(activate, bool):
            return jsonify({'success': False, 'message': 'restart must be a boolean'}), 400
        deployment_id = data.get('deployment_id') or uuid.uuid4().hex
        reservation = agent_manager.begin_deployment(agent_id, deployment_id)
        if isinstance(reservation, dict):
            if not reservation.get('success'):
                return jsonify(reservation), reservation.get('http_status', 409)
            if reservation.get('existing'):
                return _deployment_response(agent_manager.get_deployment(agent_id, deployment_id))
        owns_preparation = True
        provider_downloads, custom_files, prepared_providers = [], [], []
        if service_type == 'mihomo':
            config_content = generate_mihomo_config(config_data, base_url=base_url,
                                                    sync_lan_hosts=True, preflight_providers=False,
                                                    region_probe=True)
            main = yaml.safe_load(config_content)
            provider_downloads = get_mihomo_provider_downloads(config_data, base_url=base_url, main_config=main)
            ruleset_downloads = get_mihomo_ruleset_downloads(config_data, base_url=base_url)
            if provider_downloads:
                prepared_providers = prepare_provider_bundle(config_data, main, render_all=True,
                                                               allow_transport_fallback=False)
                provider_downloads = [{key: item[key] for key in ('name', 'url', 'local_path', 'content')}
                                      for item in prepared_providers]
        else:
            config_content = generate_mosdns_config(config_data, base_url=base_url)
            ruleset_downloads = get_mosdns_ruleset_downloads(config_data, base_url=base_url)
            custom_files = get_mosdns_custom_files(config_data)
        bundle = build_deployment_bundle(agent, config_data, config_content,
                                         provider_downloads=provider_downloads,
                                         ruleset_downloads=ruleset_downloads,
                                         custom_files=custom_files, base_url=base_url,
                                         deployment_id=deployment_id)
        # Publish caches only after all files and the exact graph are ready.
        if prepared_providers:
            commit_provider_bundle(profile_id, prepared_providers)
        extra_data = {'deployment_bundle': bundle, 'activate': activate,
                      'provider_downloads': provider_downloads, 'ruleset_downloads': ruleset_downloads,
                      'custom_files': custom_files}
        result = agent_manager.push_config_to_agent(agent_id, config_content, extra_data=extra_data,
                                                    config_revision=_profile_revision(profile_id))
        result['profile_id'] = profile_id
        result['file_count'] = len(bundle.manifest['files'])
        result['provider_count'] = len(provider_downloads)
        result['rule_count'] = len(ruleset_downloads)
        return _deployment_response(result)
    except (StrategyReferenceError, DeploymentPreparationError, ValueError) as exc:
        result = {'success': False, 'status': 'failed', 'message': str(exc)}
        if deployment_id:
            agent_manager.record_deployment(agent_id, deployment_id, result)
            result['deployment_id'] = deployment_id
        return jsonify(result), 400
    except Exception as exc:
        # Do not expose transport exceptions containing subscription credentials.
        logger.error('Agent deployment preparation failed (%s)', type(exc).__name__)
        result = {'success': False, 'status': 'failed', 'message': 'Deployment preparation failed'}
        if deployment_id:
            agent_manager.record_deployment(agent_id, deployment_id, result)
            result['deployment_id'] = deployment_id
        return jsonify(result), 500
    finally:
        if owns_preparation:
            agent_manager.finish_deployment_preparation(agent_id, deployment_id)


def _deployment_response(result):
    status = result.get('status')
    code = result.get('http_status')
    if not code:
        if not result.get('success'):
            code = 409 if status in ('rolled_back', 'rollback_failed') else 500
        elif status and status not in ('succeeded', 'failed', 'rolled_back', 'rollback_failed'):
            code = 202
        else:
            code = 200
    return jsonify(result), code


@bp.route('/<agent_id>/deployments/<deployment_id>', methods=['GET'])
@require_auth
def get_agent_deployment(agent_id, deployment_id):
    result = get_agent_manager().get_deployment(agent_id, deployment_id)
    # A terminal failure is still a successfully retrieved task, not a failed poll.
    code = 200 if result.get('status') in ('succeeded', 'failed', 'rolled_back', 'rollback_failed') else result.get('http_status', 200)
    return jsonify(result), code


@bp.route('/<agent_id>/deployments/<deployment_id>/activate', methods=['POST'])
@require_auth
def activate_agent_deployment(agent_id, deployment_id):
    return _deployment_response(get_agent_manager().activate_deployment(agent_id, deployment_id))


@bp.route('/<agent_id>/metrics', methods=['GET'])
@require_auth
def get_agent_metrics(agent_id):
    """获取 Agent 最新监控数据"""
    try:
        agent_manager = get_agent_manager()

        # 从 Agent 记录中获取最新监控数据
        agent = agent_manager.get_agent_by_id(agent_id)
        if not agent:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404

        # 返回监控数据
        metrics = agent.get('system_metrics', {})
        return jsonify({
            'success': True,
            'data': {
                'agent_id': agent_id,
                'metrics': metrics,
                'collected_at': metrics.get('collected_at', None)
            }
        }), 200

    except Exception as e:
        logger.error(f"获取Agent监控数据失败: {e}")
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/metrics/history', methods=['GET'])
@require_auth
def get_agent_metrics_history(agent_id):
    """获取 Agent 监控历史数据"""
    try:
        agent_manager = get_agent_manager()

        # 检查 Agent 是否存在
        agent = agent_manager.get_agent_by_id(agent_id)
        if not agent:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404

        # 获取时间范围参数（默认 24 小时）
        hours = request.args.get('hours', type=int, default=24)

        # 获取历史数据
        history = agent_manager.metrics_history.get_metrics(agent_id, hours=hours)

        return jsonify({
            'success': True,
            'data': {
                'agent_id': agent_id,
                'history': history,
                'hours': hours,
                'data_points': len(history)
            }
        }), 200

    except Exception as e:
        logger.error(f"获取Agent监控历史数据失败: {e}")
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/metrics/summary', methods=['GET'])
@require_auth
def get_agent_metrics_summary(agent_id):
    """获取 Agent 监控数据统计摘要"""
    try:
        agent_manager = get_agent_manager()

        # 检查 Agent 是否存在
        agent = agent_manager.get_agent_by_id(agent_id)
        if not agent:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404

        # 获取时间范围参数（默认 1 小时）
        hours = request.args.get('hours', type=int, default=1)

        # 获取统计摘要
        summary = agent_manager.metrics_history.get_metrics_summary(agent_id, hours=hours)

        return jsonify({
            'success': True,
            'data': {
                'agent_id': agent_id,
                'summary': summary,
                'hours': hours
            }
        }), 200

    except Exception as e:
        logger.error(f"获取Agent监控统计摘要失败: {e}")
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/traffic/stats', methods=['GET'])
@require_auth
def get_agent_traffic_stats(agent_id):
    """获取 Agent 流量统计数据"""
    try:
        agent_manager = get_agent_manager()

        # 检查 Agent 是否存在
        agent = agent_manager.get_agent_by_id(agent_id)
        if not agent:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404

        # 获取统计周期参数（默认 total）
        # 可选值: 'total', 'today', 'week', 'hours_24'
        period = request.args.get('period', type=str, default='total')

        # 获取流量统计数据
        stats = agent_manager.metrics_history.get_traffic_stats(agent_id, period=period)

        return jsonify({
            'success': True,
            'data': {
                'agent_id': agent_id,
                'stats': stats
            }
        }), 200

    except Exception as e:
        logger.error(f"获取Agent流量统计失败: {e}")
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/traffic/trend', methods=['GET'])
@require_auth
def get_agent_traffic_trend(agent_id):
    """获取 Agent 流量趋势数据（用于图表）"""
    try:
        agent_manager = get_agent_manager()

        # 检查 Agent 是否存在
        agent = agent_manager.get_agent_by_id(agent_id)
        if not agent:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404

        # 获取参数
        hours = request.args.get('hours', type=int, default=24)
        interval_minutes = request.args.get('interval', type=int, default=5)

        # 获取流量趋势数据
        trend = agent_manager.metrics_history.get_traffic_trend(
            agent_id,
            hours=hours,
            interval_minutes=interval_minutes
        )

        return jsonify({
            'success': True,
            'data': {
                'agent_id': agent_id,
                'trend': trend,
                'hours': hours,
                'interval_minutes': interval_minutes,
                'data_points': len(trend)
            }
        }), 200

    except Exception as e:
        logger.error(f"获取Agent流量趋势失败: {e}")
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/latest-version', methods=['GET'])
@require_auth
def get_latest_agent_version():
    """获取最新的 Agent 版本号"""
    try:
        latest_version = get_latest_version()
        return jsonify({
            'success': True,
            'version': latest_version
        }), 200
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500
