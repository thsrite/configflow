"""Agent 管理路由模块

提供 Agent 的注册、管理、配置推送等功能
"""
import hmac
import json
import math
import os
import sqlite3
import time
from flask import request, jsonify, send_file
from werkzeug.datastructures import MultiDict

from backend.agents.config_generator import generate_agent_config
from backend.agents.deployment_messages import public_deployment_messages
from backend.agents.manager import AgentDeploymentConflict
from backend.agents.traffic_store import REPORT_MAX_CONTENT_LENGTH, clean_report, get_traffic_store
from backend.agents.upgrade_messages import public_upgrade_state, upgrade_error_message
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
from backend.common.config_export import redact_secrets_in_text
from backend.common.auth import is_token_within_length, parse_bearer_token, require_auth
from backend.common.config import get_config, get_default_profile_id, get_repository
from backend.common.config_repository import ProfileRepositoryError
from backend.common.agent_manager import get_agent_manager
from backend.common.utils import str_to_bool
from backend.utils.logger import get_logger
from backend.agents.diagnostics import failure_text
from backend.utils.strategy_references import StrategyReferenceError
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
    """Hide credentials and private deployment comparison metadata."""
    public = {key: value for key, value in agent.items() if key not in ('token', 'applied_content', 'dns_follow_job')}
    if public.get('oxidns_ha_publication'):
        from backend.agents.oxidns_ha_publish import summary
        public['oxidns_ha_publication'] = summary(public['oxidns_ha_publication'])
    if 'deployments' in public:
        public['deployments'] = {key: _public_deployment(value)
                                 for key, value in public['deployments'].items()}
    if isinstance(public.get('latest_deployment'), dict):
        public['latest_deployment'] = _public_deployment(public['latest_deployment'])
    if isinstance(public.get('latest_upgrade'), dict):
        public['latest_upgrade'] = public_upgrade_state(public['latest_upgrade'])
    return public


@bp.route('/<agent_id>/dns-follow', methods=['GET', 'PUT', 'DELETE'])
@require_auth
def agent_dns_follow(agent_id):
    from backend.agents import dns_follow
    manager = get_agent_manager()
    try:
        if request.method == 'PUT':
            result = dns_follow.configure(manager, agent_id, request.get_json(silent=True))
        elif request.method == 'DELETE':
            result = dns_follow.disable(manager, agent_id)
        else:
            result = dns_follow.options(manager, agent_id)
        return jsonify(result)
    except ValueError as error:
        secrets = tuple(agent.get('token') for agent in manager.get_all_agents())
        return jsonify({'success': False, 'message': failure_text(str(error), secrets)}), 409


@bp.route('/<agent_id>/dns-follow/retry', methods=['POST'])
@require_auth
def retry_agent_dns_follow(agent_id):
    from backend.agents import dns_follow
    manager = get_agent_manager()
    try:
        return jsonify(dns_follow.retry(manager, agent_id)), 202
    except ValueError as error:
        secrets = tuple(agent.get('token') for agent in manager.get_all_agents())
        return jsonify({'success': False, 'message': failure_text(str(error), secrets)}), 409


@bp.route('/<agent_id>/oxidns-ha', methods=['GET', 'PUT', 'DELETE'])
@require_auth
def agent_oxidns_ha(agent_id):
    from backend.agents import oxidns_ha
    manager = get_agent_manager()
    try:
        if request.method == 'PUT':
            result = oxidns_ha.configure(manager, agent_id, request.get_json(silent=True),
                                         base_url=request.host_url.rstrip('/'))
        elif request.method == 'DELETE':
            result = oxidns_ha.disable(manager, agent_id, force=request.args.get('force', '').lower() in ('1', 'true'))
        else:
            result = oxidns_ha.status(manager, agent_id)
        publication = result.get('publication') or {}
        code = 202 if request.method == 'PUT' and publication.get('status') not in ('succeeded', 'failed', 'rollback_failed') else 200
        return jsonify(result), code
    except ValueError as error:
        secrets = tuple(agent.get('token') for agent in manager.get_all_agents())
        body = {'success': False, 'message': failure_text(str(error), secrets)}
        for key, value in getattr(error, 'details', {}).items():
            body[key] = [failure_text(item, secrets) for item in value] if isinstance(value, list) else value
        return jsonify(body), 409


def _public_deployment(record):
    if record.get('ha_publication'):
        from backend.agents.oxidns_ha_publish import _aggregate
        record = _aggregate(record, record['ha_publication'])
    return public_deployment_messages({key: value for key, value in record.items()
                                      if key not in ('prepared_content', 'oxidns_validation_receipt')})


def _agent_with_content_sync(agent, snapshots):
    from backend.agents.content_sync import get_content_sync
    public = _public_agent(agent)
    repository = get_repository()
    profile_id = (agent.get('profile_id') or get_default_profile_id())
    try:
        if profile_id not in snapshots:
            snapshots[profile_id] = repository.get_compat_config(profile_id)
        # Preserve the origin used by the last push when server_domain is unset.
        # Opening this UI through a different host must not change generated URLs.
        base_url = (agent.get('applied_content') or {}).get('base_url', '')
        public['content_sync'] = get_content_sync(
            agent, snapshots[profile_id], base_url, _prepare_content_bundle,
            scope=str(repository.data_dir), repository=repository)
    except ProfileRepositoryError:
        public['content_sync'] = {'status': 'unknown', 'reason': 'profile_unavailable'}
    return public


@bp.route('/core-install-script', methods=['GET'])
def get_core_install_script():
    """Download the standalone, fixed-layout Mihomo + OxiDNS installer."""
    try:
        from backend.agents.core_install_script import generate_core_install_script
        script = generate_core_install_script()
    except (OSError, ValueError):
        logger.exception("生成双核心安装脚本失败")
        return jsonify({'success': False, 'message': '核心安装脚本不可用，请更新 ConfigFlow 后重试。'}), 500
    return script, 200, {
        'Content-Type': 'text/plain; charset=utf-8',
        'Content-Disposition': 'attachment; filename="install-mihomo-oxidns.sh"',
        'Cache-Control': 'no-store',
        'X-Content-Type-Options': 'nosniff',
    }


@bp.route('/gateway-network-probe.sh', methods=['GET'])
def get_gateway_network_probe():
    """Download a fixed, public, read-only Linux network probe without credentials."""
    path = os.path.join(os.path.dirname(__file__), '..', 'agents', 'scripts', 'detect-gateway-network.sh')
    try:
        with open(path, encoding='utf-8') as source:
            script = source.read()
    except OSError:
        logger.exception('读取网关网络检测脚本失败')
        return jsonify({'success': False, 'message': '网络检测脚本暂时不可用，请更新 ConfigFlow 后重试。'}), 503
    return script, 200, {
        'Content-Type': 'text/plain; charset=utf-8',
        'Content-Disposition': 'attachment; filename="detect-gateway-network.sh"',
        'Cache-Control': 'no-store',
        'X-Content-Type-Options': 'nosniff',
    }


_INSTALL_COMMAND_FIELDS = frozenset({
    'name', 'type', 'port', 'agent_ip', 'config_path', 'restart_command', 'server_url',
    'agent_type', 'install_core', 'service_manager', 'service_unit', 'service_binary',
    'stop_command', 'start_command', 'status_command', 'health_url',
    'health_dns_address', 'health_dns_name', 'deployment_health_timeout',
    'mihomo_agent_port', 'oxidns_agent_port', 'mihomo_config_path', 'oxidns_config_path',
    'mihomo_service_unit', 'oxidns_service_unit', 'mihomo_service_binary', 'oxidns_service_binary',
    'ipv4_forwarding', 'prepare_dns_ha',
})
_INSTALL_COMMAND_BOOLEANS = frozenset({'install_core', 'ipv4_forwarding', 'prepare_dns_ha'})
_INSTALL_COMMAND_NUMBERS = frozenset({
    'port', 'mihomo_agent_port', 'oxidns_agent_port', 'deployment_health_timeout',
})


def _install_command_params(payload):
    if not isinstance(payload, dict) or set(payload) - _INSTALL_COMMAND_FIELDS:
        raise ValueError('安装参数无效，请重新生成命令。')
    params = MultiDict()
    for key, value in payload.items():
        if key in _INSTALL_COMMAND_BOOLEANS:
            if not isinstance(value, bool):
                raise ValueError(f'{key} 必须为 true 或 false')
            value = 'true' if value else 'false'
        elif key in _INSTALL_COMMAND_NUMBERS:
            if not isinstance(value, int) or isinstance(value, bool) or not 1 <= value <= 65535:
                raise ValueError(f'{key} 必须是 1 到 65535 之间的整数')
            value = str(value)
        elif not isinstance(value, str) or len(value) > 4096 or '\0' in value:
            raise ValueError(f'{key} 参数无效')
        params[key] = value
    if params.get('agent_type', 'go') not in ('go', 'shell'):
        raise ValueError('agent_type 必须为 go 或 shell')
    return params


def _save_install_command(script):
    from backend.agents.install_command_store import InstallCommandStoreFull, save_install_script
    try:
        token, expires_at = save_install_script(script)
    except InstallCommandStoreFull:
        return jsonify({'success': False, 'message': '可用安装链接已达上限，请使用已生成的命令或等待旧链接过期。'}), 503
    except (OSError, ValueError, sqlite3.Error):
        logger.exception('保存安装命令失败')
        return jsonify({'success': False, 'message': '无法保存安装命令，请检查数据目录后重试。'}), 500
    response = jsonify({
        'success': True, 'script': script,
        'download_path': f'/api/agents/i/{token}', 'expires_at': expires_at,
    })
    response.headers['Cache-Control'] = 'no-store'
    return response


@bp.route('/install-command', methods=['POST'])
@require_auth
def create_install_command():
    """Render once and keep the preview and downloaded script identical."""
    payload, error = _read_json_body(32 * 1024, '安装参数无效')
    if error:
        return error
    try:
        params = _install_command_params(payload)
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    rendered = _install_script_response(params)
    if rendered[1] != 200:
        return rendered
    return _save_install_command(rendered[0])


@bp.route('/core-install-command', methods=['POST'])
@require_auth
def create_core_install_command():
    payload, error = _read_json_body(1024, '核心安装参数无效')
    if error:
        return error
    if (not isinstance(payload, dict) or set(payload) != {'target'}
            or payload['target'] not in ('both', 'mihomo', 'oxidns')):
        return jsonify({'success': False, 'message': '请选择 both、mihomo 或 oxidns 核心。'}), 400
    rendered = get_core_install_script()
    if rendered[1] != 200:
        return rendered
    lines = rendered[0].splitlines(keepends=True)
    # Preserve the shebang and interface marker while fixing the chosen target.
    script = ''.join(lines[:2]) + f"set -- --core {payload['target']}\n" + ''.join(lines[2:])
    return _save_install_command(script)


@bp.route('/i/<token>', methods=['GET'])
def download_install_command(token):
    from backend.agents.install_command_store import load_install_script
    headers = {
        'Content-Type': 'text/plain; charset=utf-8',
        'Content-Disposition': 'attachment; filename="configflow-install.sh"',
        'Cache-Control': 'no-store',
        'X-Content-Type-Options': 'nosniff',
    }
    try:
        script = load_install_script(token)
    except (OSError, sqlite3.Error):
        logger.exception('读取安装命令失败')
        return '安装脚本暂时不可用，请稍后重试。\n', 503, headers
    if script is None:
        return '安装链接不存在或已过期，请在面板重新生成。\n', 404, headers
    return script, 200, headers


@bp.route('/install-script', methods=['GET'])
def get_install_script():
    return _install_script_response(request.args)


def _install_script_response(params):
    """生成原生 Agent 安装脚本，支持单服务和 Mihomo + OxiDNS 同机。"""
    try:
        import os
        from backend.agents.go_install_script import generate_go_agent_install_script, generate_go_aio_install_script
        from backend.agents.install_script import generate_lightweight_install_script

        # 获取参数
        agent_name = params.get('name', 'My-Agent')
        service_type = params.get('type', 'mihomo')
        agent_port = params.get('port', 8080, type=int)
        agent_ip = params.get('agent_ip', '').strip()  # 可选的 Agent IP
        config_path = params.get('config_path', f'/etc/{service_type}/config.yaml')
        restart_command = params.get('restart_command', f'systemctl restart {service_type}')

        # agent_type: 'go' (默认) 或 'shell'
        agent_type = params.get('agent_type', 'go').lower().strip()
        install_core_value = params.get('install_core', 'false')
        if install_core_value not in ('true', 'false'):
            raise ValueError('install_core 必须为 true 或 false')
        install_core = install_core_value == 'true'
        prepare_ha_value = params.get('prepare_dns_ha')
        if prepare_ha_value not in (None, 'true', 'false'):
            raise ValueError('prepare_dns_ha 必须为 true 或 false')
        if prepare_ha_value is not None and (service_type != 'oxidns' or agent_type != 'go'):
            raise ValueError('仅 OxiDNS 单服务 Go Agent 支持准备 DNS 高可用环境')
        prepare_dns_ha = prepare_ha_value == 'true'
        if install_core:
            if service_type not in ('mihomo', 'oxidns', 'aio') or agent_type != 'go':
                raise ValueError('仅 Mihomo、OxiDNS 或同机双服务 Go Agent 支持同时安装核心')
            # Preserve strict validation instead of Flask's integer converter
            # silently replacing an invalid explicit value with the default.
            agent_port = params.get('port', '8080')
        # Omitted: Mihomo single-service installs under systemd/OpenRC enable
        # and persist IPv4 forwarding; false opts out. Other installs never take it.
        forwarding_value = params.get('ipv4_forwarding')
        if forwarding_value not in (None, 'true', 'false'):
            raise ValueError('ipv4_forwarding 必须为 true 或 false')
        if forwarding_value is not None and (service_type != 'mihomo' or agent_type != 'go'):
            raise ValueError('仅 Mihomo 单独安装（Go Agent）可设置 IPv4 转发')
        manage_forwarding = None if forwarding_value is None else forwarding_value == 'true'

        logger.info(f"生成安装脚本请求 - 名称: {agent_name}, 类型: {service_type}, 端口: {agent_port}, Agent类型: {agent_type}")

        # 检查模板文件是否存在
        scripts_dir = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'agents', 'scripts')
        # logger.info(f"脚本模板目录: {scripts_dir}")
        # logger.info(f"模板目录是否存在: {os.path.exists(scripts_dir)}")
        # if os.path.exists(scripts_dir):
        #     logger.info(f"模板目录内容: {os.listdir(scripts_dir)}")

        # 获取服务器 URL
        # 优先使用前端传递的 server_url 参数（包含协议+IP/域名+端口）
        server_url = params.get('server_url', '').strip()

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
        if service_type == 'aio':
            if agent_type != 'go':
                raise ValueError('Mihomo + OxiDNS 同机安装需要 Go Agent')
            script = generate_go_aio_install_script(
                server_url=server_url,
                agent_name=agent_name,
                agent_ip=agent_ip,
                binary_download_url=f"{server_url}/api/agents/download",
                mihomo_agent_port=params.get('mihomo_agent_port', '8080'),
                oxidns_agent_port=params.get('oxidns_agent_port', '8082'),
                install_core=install_core,
                **{f'{service}_{field}': params.get(f'{service}_{field}', default).strip()
                   for service in ('mihomo', 'oxidns')
                   for field, default in (
                       ('config_path', f'/etc/{service}/config.yaml'),
                       ('service_unit', service),
                       ('service_binary', service),
                   )},
            )
        elif agent_type == 'shell':
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
                **{key: params.get(key, '').strip() for key in (
                    'service_manager', 'service_unit', 'service_binary', 'stop_command',
                    'start_command', 'status_command', 'health_url', 'health_dns_address', 'health_dns_name')},
                deployment_health_timeout=params.get('deployment_health_timeout', 30, type=int),
                install_core=install_core,
                manage_ipv4_forwarding=manage_forwarding if service_type == 'mihomo' else False,
                prepare_dns_ha=prepare_dns_ha,
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
        logger.error(f"请求参数: name={params.get('name')}, type={params.get('type')}, port={params.get('port')}, agent_type={params.get('agent_type')}")
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

        # A gateway installer gets a short-lived capability for exactly these
        # two service identities. Never reveal the global registration key.
        installation_id = agent_data.pop('installation_id', None)
        installation_token = agent_data.pop('installation_token', None)
        installation_profile = None
        if installation_id is not None or installation_token is not None:
            from backend.agents.gateway_installation import registration_profile, InstallationError
            try:
                installation_profile = registration_profile(get_repository(), installation_id, installation_token, agent_data,
                                                            fallback_host=request.remote_addr)
            except InstallationError as exc:
                return jsonify(success=False, message=str(exc)), exc.status
            agent_data['profile_id'] = installation_profile

        # 可选的注册密钥验证
        registration_key = os.environ.get('AGENT_REGISTRATION_KEY', '')
        if registration_key and installation_profile is None:
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
        if installation_profile is None and (not agent_host or is_docker_ip):
            agent_data['host'] = client_ip

        logger.info("Agent注册字段: %s", _registration_log_fields(agent_data))

        # 已有 Agent 的重注册必须使用其当前能力令牌。注册密钥仅控制首次
        # 注册资格，不能替代已有 Agent 的令牌认证。
        existing_token = parse_bearer_token(
            request.headers.get('Authorization', '')
        )

        # host 已按真实客户端 IP 归一化后，才允许 Manager 做 name/host 匹配。
        if installation_profile is not None:
            from backend.agents.gateway_installation import register as register_installation, InstallationError
            try:
                result = register_installation(get_repository(), agent_manager, installation_id,
                                               installation_token, agent_data, existing_token)
            except InstallationError as exc:
                return jsonify(success=False, message=str(exc)), exc.status
        else:
            result = agent_manager.register_agent(agent_data, existing_token=existing_token)

        response_data = {'success': True, **result}
        logged = _registration_log_fields(agent_data)
        logger.info('%s：%s（%s，%s）', '新 Agent 注册成功' if result.get('is_new') else 'Agent 重新注册',
                    logged.get('name') or result.get('id'), logged.get('service_type', 'mihomo'), logged.get('host', ''))

        return jsonify(response_data), 200

    except AgentDeploymentConflict as exc:
        return jsonify(success=False, message=str(exc)), 409

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
    report = clean_report(report)
    if report is None:
        return jsonify({'success': False, 'message': 'Invalid traffic report'}), 400
    get_traffic_store().add_report(agent_id, report)
    return jsonify({
        'success': True,
        'enabled': True,
        'rule_providers': discovery_provider_revisions((agent.get('profile_id') or get_default_profile_id())),
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
        profile_id = (agent.get('profile_id') or get_default_profile_id())
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
        if request.args.get('summary') == '1':
            # Device pickers and live polling need no content comparison, which
            # loads and hashes each profile's full configuration.
            return jsonify([_public_agent(agent) for agent in agent_manager.get_all_agents()]), 200
        snapshots = {}
        agents = [_agent_with_content_sync(agent, snapshots) for agent in agent_manager.get_all_agents()]
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
        except AgentDeploymentConflict as exc:
            return jsonify({'success': False, 'message': str(exc)}), 409
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
            return jsonify(_agent_with_content_sync(agent, {})), 200
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
        source = request.args.get('source', '')  # Agent 解析出的日志来源
        agent_manager = get_agent_manager()
        result = agent_manager.get_agent_logs(agent_id, lines, log_path=log_path, source=source)
        for key in ('logs', 'error_logs'):
            if key in result:
                result[key] = redact_secrets_in_text(result[key])
        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/logs/sources', methods=['GET'])
@require_auth
def get_agent_log_sources(agent_id):
    """获取 Agent 本机可读的日志来源"""
    try:
        agent_manager = get_agent_manager()
        result = agent_manager.get_agent_log_sources(agent_id)
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
        source = data.get('source', '')

        if not log_path and not source:
            return jsonify({'success': False, 'message': '日志路径不能为空'}), 400

        agent_manager = get_agent_manager()
        result = agent_manager.clear_agent_log(agent_id, log_path, source=source)
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
        return jsonify(public_upgrade_state(result)), 202 if result['status'] != 'failed' else 409
    except ValueError as exc:
        return jsonify({'success': False, 'message': upgrade_error_message(str(exc))}), 409


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
            return jsonify(public_upgrade_state(poll(manager, agent_id))), 200
        except ValueError as exc:
            return jsonify({'success': False, 'message': upgrade_error_message(str(exc))}), 404
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
        return jsonify({'success': False, 'message': 'Agent 上报的更新结果格式无效。'}), 400


# Docker 相关路由（安装脚本生成）

@bp.route('/<agent_id>/oxidns-ha-environment', methods=['GET'])
@require_auth
def agent_oxidns_ha_environment(agent_id):
    """Live, read-only proof for the installation guide, including unpaired nodes."""
    manager = get_agent_manager()
    agent = manager.get_agent_by_id(agent_id)
    if not agent or agent.get('service_type') != 'oxidns':
        return jsonify({'success': False, 'message': 'OxiDNS Agent not found'}), 404
    try:
        capability = manager._deployment_request(agent, 'get', 'capabilities', timeout=5)
    except Exception:
        return jsonify({'success': False, 'message': '无法连接 Agent，请先检查容器日志和网络'}), 502
    if not isinstance(capability, dict) or capability.get('success') is False:
        return jsonify({'success': False, 'message': 'Agent 能力检查失败，请确认 Agent 已上线'}), 502
    result = {'success': True, 'agent_id': agent_id, 'service_type': 'oxidns'}
    for field in ('oxidns_ha_protocols', 'oxidns_ha_container_protocols', 'oxidns_ha_handoff_protocols'):
        value = capability.get(field)
        result[field] = [v for v in value if type(v) is int and 0 <= v < 100] if isinstance(value, list) else []
    environment = capability.get('oxidns_ha_environment')
    environment = environment if isinstance(environment, dict) else {}
    result['oxidns_ha_environment'] = {
        'kind': environment.get('kind') if environment.get('kind') in ('native', 'container') else 'unknown',
        'ready': environment.get('ready') is True,
        'network': environment.get('network') if environment.get('network') in ('host', 'macvlan') else '',
        'host_id': environment.get('host_id') if isinstance(environment.get('host_id'), str)
            and len(environment['host_id']) == 64 and all(c in '0123456789abcdef' for c in environment['host_id']) else '',
        'host_id_source': environment.get('host_id_source', 'file')
            if environment.get('host_id_source', 'file') in ('file', 'docker-volume', 'installation') else 'unknown',
        'host_boot_id': environment.get('host_boot_id') if isinstance(environment.get('host_boot_id'), str)
            and len(environment['host_boot_id']) == 64
            and all(c in '0123456789abcdef' for c in environment['host_boot_id']) else '',
        'interface': str(environment.get('interface') or '')[:64],
        'blockers': [failure_text(v, (agent.get('token'),))[:500] for v in environment.get('blockers', [])[:20]
            if isinstance(v, str)] if isinstance(environment.get('blockers'), list) else [],
    }
    return jsonify(result)

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
            'mosdns_port': request.args.get('mosdns_port', 8081, type=int),
            'enable_oxidns': str_to_bool(request.args.get('enable_oxidns', 'false')),
            'oxidns_port': request.args.get('oxidns_port', 8082, type=int),
            'enable_dns_ha': str_to_bool(request.args.get('enable_dns_ha', 'false')),
            'ha_parent': request.args.get('ha_parent', '').strip(),
            'ha_subnet': request.args.get('ha_subnet', '').strip(),
            'ha_gateway': request.args.get('ha_gateway', '').strip(),
            'ha_host_id_path': request.args.get('ha_host_id_path', '/etc/machine-id').strip(),
        }

        script = generate_docker_agent_compose(**params)
        return script, 200, {'Content-Type': 'text/yaml; charset=utf-8'}

    except ValueError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
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
            'mosdns_port': request.args.get('mosdns_port', 8081, type=int),
            'enable_oxidns': str_to_bool(request.args.get('enable_oxidns', 'false')),
            'oxidns_port': request.args.get('oxidns_port', 8082, type=int),
            'enable_dns_ha': str_to_bool(request.args.get('enable_dns_ha', 'false')),
            'ha_parent': request.args.get('ha_parent', '').strip(),
            'ha_subnet': request.args.get('ha_subnet', '').strip(),
            'ha_gateway': request.args.get('ha_gateway', '').strip(),
            'ha_host_id_path': request.args.get('ha_host_id_path', '/etc/machine-id').strip(),
        }

        script = generate_docker_agent_run(**params)
        return script, 200, {'Content-Type': 'text/plain; charset=utf-8'}

    except ValueError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


def _profile_revision(profile_id):
    """推送时配置空间的修订号，仅供诊断。"""
    try:
        return get_repository().config_revision(profile_id)
    except ProfileRepositoryError:
        return None


def _generated_config_failure(exc, content, service):
    """Resolve generated-file coordinates only after an existing check fails."""
    import yaml
    from backend.agents.deployment_locations import preparation_context, source_error

    issues = getattr(exc, 'issues', None)
    mark = None
    if issues:
        first = next((issue for issue in issues if issue.get('severity', 'error') == 'error'), None)
        path = first.get('path') if first else None
        # A missing field has no line of its own. Keep its schema path in the
        # original issue instead of pointing at an unrelated parent/source line.
        if path:
            try:
                node = yaml.compose(content, Loader=yaml.SafeLoader)
                for part in path:
                    if isinstance(part, str) and isinstance(node, yaml.MappingNode):
                        node = next((value for key, value in reversed(node.value)
                                     if key.value == part), None)
                    elif isinstance(part, int) and isinstance(node, yaml.SequenceNode) and 0 <= part < len(node.value):
                        node = node.value[part]
                    else:
                        node = None
                    if node is None:
                        break
                if node is not None:
                    mark = node.start_mark
            except yaml.YAMLError:
                pass
    error = source_error('生成配置 YAML 格式错误' if isinstance(exc, yaml.YAMLError) else str(exc), content,
                         line=mark.line + 1 if mark else None,
                         column=mark.column + 1 if mark else None,
                         cause=exc if isinstance(exc, yaml.YAMLError) else None)
    error = preparation_context(error, path='config.yaml（生成配置）', kind=f'{service} 配置')
    if issues is not None:
        error.issues = issues
    return error


def _store_subscription_downloads(prepared_providers):
    """Save the subscription nodes a preparation downloaded; True when it saved any.

    The same validated nodes and hosts the publication would save afterwards,
    written the way a manual subscription update writes them.
    """
    from backend.utils.provider_delivery import commit_cache_updates
    updates = {}
    for item in prepared_providers:
        for update in item.get('cache_updates', []):
            updates[update[0]] = update
    if not updates:
        return False
    commit_cache_updates(updates.values())
    return True


def _saved_subscriptions_only(_sub_id, _url, **_kwargs):
    """Rendering again after saving never downloads: a subscription that failed
    to download keeps the fallback it had in the first rendering."""
    raise ConnectionError('本次发布已经尝试过下载这个订阅，不再重复下载')


def _prepare_transactional_content(agent, config_data, *, base_url='', deployment_id=None,
                                   prefer_cached=False, progress=None, detail_progress=None, phase_progress=None,
                                   store_downloads=False):
    """Materialize exactly the files used by a push, without publishing.

    Caches are saved only with ``store_downloads`` (a real publication): a
    subscription without a current node cache is downloaded, validated with the
    whole provider graph, saved like a manual update, and the Mihomo main file
    and providers are then rendered again from the saved nodes. Without that,
    the first push renders the downloaded body (original key order, no
    subscription hosts) while every later content check renders the saved
    nodes, so a just-published device would show 「有变更，待推送」.
    With a current cache nothing is downloaded or saved and nothing is rendered twice.
    """
    import yaml
    from backend.agents.deployment_bundle import build_deployment_bundle
    from backend.utils.provider_delivery import prepare_provider_bundle

    service_type = agent.get('service_type', 'mihomo')
    if service_type == 'mihomo' and (config_data.get('mihomo', {}).get('linux_gateway') or {}).get('enabled'):
        from backend.agents.linux_gateway import require_gateway_agent
        require_gateway_agent(agent)
    from backend.common.router_split import (
        GATEWAY_MARKER, gateway_was_used, is_full_tun, publish_legacy_source, read_marker, require_full_tun_generated,
    )
    if service_type == 'mihomo' and read_marker(config_data)[0] != 'absent':
        from backend.agents.linux_gateway import FORWARDING_MIN_AGENT_VERSION, supports_forwarding
        if agent.get('deployment_method') != 'docker' and not supports_forwarding(agent):
            raise ValueError(f'主路由分流（完整 TUN）需要 Mihomo Agent {FORWARDING_MIN_AGENT_VERSION} 或更新版本；'
                             '旧版本在移除旧网关记录时可能关闭 IPv4 转发。请先在 Agent 页更新。')
    if phase_progress is not None:
        service_name = {'mihomo': 'Mihomo', 'mosdns': 'MosDNS', 'oxidns': 'OxiDNS'}.get(service_type, service_type)
        phase_progress(f'正在生成 {service_name} config.yaml 并检查基础配置', force=True)
    provider_downloads, custom_files, prepared_providers = [], [], []
    if service_type == 'mihomo':
        from backend.converters.mihomo import node_conversions
        from backend.utils.proxy_server_dns import reported_core_version
        conversions = {}  # Sub-Store results of URI nodes, reused when rendering again

        def generate_main(reuse=False):
            with node_conversions(conversions, reuse=reuse):
                content = generate_mihomo_config(config_data, base_url=base_url,
                                                 sync_lan_hosts=True, preflight_providers=False,
                                                 region_probe=bool(agent.get('domain_discovery_enabled')),
                                                 mihomo_core_version=reported_core_version(agent))
            try:
                return content, yaml.safe_load(content)
            except yaml.YAMLError as exc:
                raise _generated_config_failure(exc, content, 'Mihomo') from exc

        config_content, main = generate_main()
        if is_full_tun(config_data):
            # Without TUN the main router's forwarded connections would loop.
            require_full_tun_generated(main)
            if agent.get('deployment_method') != 'docker':
                # The deployed full TUN makes the Agent remove an old gateway
                # record, which must not turn forwarding off with it.
                from backend.agents import linux_gateway
                linux_gateway.require_forwarding_kept(get_agent_manager(), agent)
        elif (agent.get('deployment_method') != 'docker'
              and not (isinstance(main, dict) and isinstance(main.get(GATEWAY_MARKER), dict)
                       and main[GATEWAY_MARKER].get('enabled') is True)):
            # An old router split with the gateway off: the Agent removes the
            # host's gateway record, and with it the IPv4 forwarding unless persisted.
            # Only a host that can hold one, for a profile that used the gateway.
            from backend.agents import linux_gateway
            source = (linux_gateway.supports_gateway(agent)
                      and publish_legacy_source(get_repository(), agent.get('profile_id'), config_data, agent.get('id')))
            if source and gateway_was_used(config_data, agent, source):
                linux_gateway.require_gateway_off_kept(get_agent_manager(), agent)
        provider_downloads = get_mihomo_provider_downloads(config_data, base_url=base_url, main_config=main)
        if provider_downloads:
            from functools import partial
            from backend.utils.sub_store_client import get_subscription_proxies_yaml
            subscription_fetch = (partial(get_subscription_proxies_yaml, prefer_cached=True)
                                  if prefer_cached else None)
            with node_conversions(conversions):
                prepared_providers = prepare_provider_bundle(config_data, main, render_all=True,
                                                             allow_transport_fallback=False,
                                                             subscription_fetch=subscription_fetch,
                                                             progress=phase_progress)
            if store_downloads and _store_subscription_downloads(prepared_providers):
                # Saved subscriptions add their hosts to the main file and are
                # rendered from the saved nodes: the same data and serialization
                # as the content check after this push. In the main file only
                # hosts-derived sections change; proxy-providers and
                # rule-providers stay the same.
                if phase_progress is not None:
                    phase_progress('已保存新下载的订阅节点，正在按保存的节点重新生成 config.yaml', force=True)
                config_content, main = generate_main(reuse=True)
                with node_conversions(conversions, reuse=True):
                    prepared_providers = prepare_provider_bundle(config_data, main, render_all=True,
                                                                 allow_transport_fallback=False,
                                                                 subscription_fetch=_saved_subscriptions_only)
            provider_downloads = [{key: item[key] for key in ('name', 'url', 'local_path', 'content')}
                                  for item in prepared_providers]
        from backend.common.linux_gateway import gateway_settings
        if gateway_settings(config_data)['enabled']:
            # Same list get_mihomo_ruleset_downloads builds from a second
            # generation; the LAN hosts and region probe options used here do
            # not touch rule-providers.
            ruleset_downloads = [{'name': name, 'url': provider['url'], 'local_path': provider['path']}
                                 for name, provider in main.get('rule-providers', {}).items()
                                 if provider.get('type') == 'http' and provider.get('url')]
        else:
            ruleset_downloads = get_mihomo_ruleset_downloads(config_data, base_url=base_url)
    elif service_type == 'mosdns':
        config_content = generate_mosdns_config(config_data, base_url=base_url)
        ruleset_downloads = get_mosdns_ruleset_downloads(config_data, base_url=base_url)
        custom_files = get_mosdns_custom_files(config_data)
    elif service_type == 'oxidns':
        from backend.converters.oxidns import generate_oxidns_config, get_oxidns_ruleset_downloads, get_oxidns_custom_files
        from backend.common.oxidns_validation import OxidnsValidationError, require_valid_document
        config_content = generate_oxidns_config(config_data, base_url=base_url)
        # This covers both legacy template output and native drafts, before
        # downloading or uploading any candidate deployment resources.
        try:
            require_valid_document(yaml.safe_load(config_content))
        except (yaml.YAMLError, OxidnsValidationError) as exc:
            raise _generated_config_failure(exc, config_content, 'OxiDNS') from exc
        ruleset_downloads = get_oxidns_ruleset_downloads(config_data, base_url=base_url)
        custom_files = get_oxidns_custom_files(config_data)
    else:
        raise ValueError('Unsupported service type')
    bundle = build_deployment_bundle(agent, config_data, config_content,
                                     provider_downloads=provider_downloads,
                                     ruleset_downloads=ruleset_downloads,
                                     custom_files=custom_files, base_url=base_url,
                                     deployment_id=deployment_id, progress=progress, detail_progress=detail_progress,
                                     phase_progress=(lambda message: phase_progress(message, force=True)) if phase_progress else None)
    return {'bundle': bundle, 'config_content': config_content, 'prepared_providers': prepared_providers,
            'provider_downloads': provider_downloads, 'ruleset_downloads': ruleset_downloads,
            'custom_files': custom_files}


def _prepare_content_bundle(agent, config_data, *, base_url='', deployment_id=None):
    return _prepare_transactional_content(agent, config_data, base_url=base_url,
                                          deployment_id=deployment_id, prefer_cached=True)['bundle']


@bp.route('/<agent_id>/push-diff', methods=['GET'])
@require_auth
def agent_push_diff(agent_id):
    """Read-only: what pushing now would change against the last applied bundle.

    Shares the background comparison (and its cache) with the list's sync
    status; answers ``checking`` until a worker has prepared the candidate.
    """
    from backend.agents.content_sync import get_push_diff
    from backend.agents.push_diff import without_stored
    from backend.common.config_export import stored_secret_finder
    agent = get_agent_manager().get_agent_by_id(agent_id)
    if not agent:
        return jsonify(success=False, message='找不到这台 Agent，请刷新设备列表。'), 404
    repository = get_repository()
    profile_id = agent.get('profile_id') or get_default_profile_id()
    try:
        config = repository.get_compat_config(profile_id)
    except ProfileRepositoryError:
        result = {'status': 'unknown', 'reason': 'profile_unavailable'}
    else:
        result = get_push_diff(agent, config, (agent.get('applied_content') or {}).get('base_url', ''),
                               _prepare_content_bundle, scope=str(repository.data_dir), repository=repository)
    result = dict(result)
    result['service_type'] = agent.get('service_type', 'mihomo')
    if agent.get('applied_content'):
        result['baseline'] = {'pushed_at': agent.get('pushed_at'), 'config_revision': agent.get('config_revision')}
    # Values are shown with credentials marked (the dialog hides them on
    # request). ConfigFlow's own stored tokens still never leave: they become
    # hidden "[REDACTED]" pieces instead of the response hook blanking whole texts.
    result = without_stored(result, stored_secret_finder())
    response = jsonify(result)
    response.headers['Cache-Control'] = 'no-store'
    return response


@bp.route('/<agent_id>/push-diff/entries', methods=['GET'])
@require_auth
def agent_push_diff_entries(agent_id):
    """Read-only: one page of a subscription's nodes or a rule set's rules from the shown change list.

    ``version`` is the change list's (from /push-diff); ``file`` its path; ``change``
    added / removed / modified; ``offset``, ``limit`` (default 50, at most 200) and an
    optional case-insensitive ``q``. Answers ``stale`` once that list is no longer the
    current one, so the dialog reloads instead of mixing two comparisons.
    """
    from backend.agents.content_sync import get_push_diff_entries
    from backend.agents.push_diff import MAX_PAGE_SIZE, MAX_QUERY_CHARS, PAGE_SIZE, search_query, without_stored
    from backend.common.config_export import stored_secret_finder

    def number(name, default, low, high):
        raw = request.args.get(name)
        if raw is None or raw == '':
            return default
        if not (raw.isascii() and raw.isdigit()) or not low <= int(raw) <= high:
            raise ValueError(name)
        return int(raw)

    version, path, change = (request.args.get(name, '') for name in ('version', 'file', 'change'))
    query = request.args.get('q', '')
    try:
        offset = number('offset', 0, 0, 10_000_000)
        limit = number('limit', PAGE_SIZE, 1, MAX_PAGE_SIZE)
        if not version or len(version) > 64 or not path or len(path) > 1024 or len(query) > MAX_QUERY_CHARS:
            raise ValueError('arguments')
        if change not in ('added', 'removed', 'modified'):
            raise ValueError('change')
    except ValueError:
        return jsonify(success=False, message='分页参数无效，请重新加载。'), 400
    agent = get_agent_manager().get_agent_by_id(agent_id)
    if not agent:
        return jsonify(success=False, message='找不到这台 Agent，请刷新设备列表。'), 404
    repository = get_repository()
    profile_id = agent.get('profile_id') or get_default_profile_id()
    try:
        config = repository.get_compat_config(profile_id)
    except ProfileRepositoryError:
        result = {'status': 'stale', 'reason': 'changed'}
    else:
        find = stored_secret_finder()
        try:
            result = get_push_diff_entries(
                agent, config, (agent.get('applied_content') or {}).get('base_url', ''), version=version, path=path,
                change=change, offset=offset, limit=limit, query=search_query(query), find=find,
                scope=str(repository.data_dir), repository=repository)
        except KeyError:
            return jsonify(success=False, message='这个文件没有可加载的明细，请重新加载。'), 404
        # Same as /push-diff: ConfigFlow's own stored tokens become hidden "[REDACTED]" pieces.
        result = without_stored(result, find)
    response = jsonify(result)
    response.headers['Cache-Control'] = 'no-store'
    return response


@bp.route('/<agent_id>/rule-sync', methods=['GET', 'PUT'])
@require_auth
def agent_rule_sync(agent_id):
    """Independent per-device rule scopes; never trust an active-profile header."""
    from backend.agents.rule_sync import configure, status
    manager = get_agent_manager()
    try:
        if request.method == 'PUT':
            if request.args or request.content_length and request.content_length > 1024:
                return jsonify(success=False, message='规则同步设置无效'), 400
            result = configure(manager, agent_id, request.get_json(silent=True))
        else:
            result = status(manager, agent_id)
        response = jsonify(result)
        response.headers['Cache-Control'] = 'no-store'
        return response
    except KeyError:
        return jsonify(success=False, message='未找到这台 Agent'), 404
    except ValueError as error:
        return jsonify(success=False, message=str(error)), 400


@bp.route('/<agent_id>/push-config', methods=['POST'])
@require_auth
def push_config_to_agent(agent_id):
    """Prepare one profile snapshot and publish a complete transactional archive."""
    from copy import deepcopy
    import uuid
    from backend.agents.deployment_bundle import DeploymentPreparationError
    from backend.utils.provider_delivery import commit_provider_bundle

    agent_manager = get_agent_manager()
    deployment_id = None
    owns_preparation = False
    agent = {}
    try:
        agent = agent_manager.get_agent_by_id(agent_id)
        if not agent:
            return jsonify({'success': False, 'message': '找不到这台 Agent，请刷新设备列表。'}), 404
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return jsonify({'success': False, 'message': '发布请求格式无效，请重新提交。'}), 400
        base_url = data.get('base_url', '').strip()
        if not base_url:
            scheme = request.headers.get('X-Forwarded-Proto', request.scheme)
            host = request.headers.get('X-Forwarded-Host', request.host)
            base_url = f'{scheme}://{host}'
        profile_id = (agent.get('profile_id') or get_default_profile_id())
        legacy_revision_guard = ('expected_config_revision' in data and
                                 'expected_profile_id' in data and
                                 'expected_oxidns_revision' in data and
                                 'expected_service_type' not in data)
        guarded = ('expected_service_type' in data or 'expected_config_revision' in data) and not legacy_revision_guard
        if legacy_revision_guard and (type(data['expected_config_revision']) is not int
                                      or data['expected_config_revision'] < 0):
            return jsonify(success=False, message='配置已改变，请重新预览后发布。'), 409
        if guarded and (not isinstance(data.get('expected_profile_id'), str)
                        or not isinstance(data.get('expected_service_type'), str)
                        or type(data.get('expected_config_revision')) is not int
                        or data['expected_config_revision'] < 0):
            return jsonify(success=False, message='保存后的发布信息不完整，请重新保存草稿后发布。'), 400
        try:
            if guarded or legacy_revision_guard:
                config_data, config_revision = get_repository().get_publish_snapshot(profile_id)
            else:
                config_data = deepcopy(get_config(profile_id))
                config_revision = _profile_revision(profile_id)
        except ProfileRepositoryError as exc:
            return jsonify({'success': False, 'message': f'Agent 绑定的配置空间不可用。原始错误：{exc}'}), 409
        if legacy_revision_guard and data['expected_config_revision'] != config_revision:
            return jsonify(success=False, message='配置已改变，请重新预览后发布。'), 409
        service_type = agent.get('service_type', 'mihomo')
        if guarded and (data['expected_profile_id'] != profile_id
                        or data['expected_service_type'] != service_type
                        or data['expected_config_revision'] != config_revision):
            return jsonify(success=False, message='草稿已保存，但配置或设备绑定已改变，请重新载入并保存后发布。'), 409
        if 'expected_oxidns_revision' in data or ('expected_profile_id' in data and not guarded):
            from backend.common.oxidns_device_policy import revision
            if service_type != 'oxidns' or data.get('expected_profile_id') != profile_id or \
                    data.get('expected_oxidns_revision') != revision(config_data.get('oxidns', {})):
                # The code tells multi-device publishers that every target would be rejected.
                return jsonify(success=False, code='draft_changed', message='配置或设备绑定已改变，请重新载入后发布。'), 409
        if service_type == 'surge':
            content = generate_surge_config(config_data, base_url=base_url)
            restart = data.get('restart', True)
            self_restart = _supports_self_restart(agent.get('version') or '')
            extra = {'restart_after_update': True} if restart and self_restart else None
            target_guard = ({'expected_profile_id': profile_id, 'expected_service_type': service_type}
                            if guarded else {})
            result = agent_manager.push_config_to_agent(agent_id, content, extra_data=extra,
                                                        config_revision=config_revision, **target_guard)
            if result.get('success'):
                result['profile_id'] = profile_id
                if restart:
                    result['restart'] = ({'success': True, 'message': '配置写入后由 Agent 执行重启。'}
                                         if self_restart else agent_manager.restart_agent_service(agent_id))
            return jsonify(_public_deployment(result)), 200 if result.get('success') else result.get('http_status', 500)
        if service_type not in ('mihomo', 'mosdns', 'oxidns'):
            return jsonify({'success': False, 'message': '当前服务类型不支持配置发布。'}), 400
        activate = data.get('restart', True)
        if not isinstance(activate, bool):
            return jsonify({'success': False, 'message': '发布请求中的重启选项无效，请重新提交。'}), 400
        deployment_id = data.get('deployment_id') or uuid.uuid4().hex
        reservation = agent_manager.begin_deployment(agent_id, deployment_id)
        if isinstance(reservation, dict):
            if not reservation.get('success'):
                return jsonify(_public_deployment(reservation)), reservation.get('http_status', 409)
            if reservation.get('existing'):
                return _deployment_response(agent_manager.get_deployment(agent_id, deployment_id))
        owns_preparation = True
        if guarded and isinstance(reservation, dict) and (
                reservation.get('profile_id') != profile_id or reservation.get('service_type') != service_type):
            result = agent_manager.record_deployment(agent_id, deployment_id, {
                'success': False, 'status': 'failed', 'http_status': 409,
                'message': '草稿已保存，但设备绑定已改变，请重新选择设备后发布。'})
            return jsonify(_public_deployment(result)), 409
        agent_manager.record_deployment(agent_id, deployment_id, {
            'status': 'preparing', 'message': '正在生成并检查配置'})
        last_progress_at = None
        from threading import Lock
        from backend.agents.operation_events import event_message
        progress_lock = Lock()
        last_detail_at = None
        rule_detail_started = False

        def preparation_detail(message, *, force=False):
            nonlocal last_detail_at
            # Rule workers run concurrently. Persist at most one object update
            # per second, plus real phase boundaries and final completion.
            with progress_lock:
                now = time.monotonic()
                if not force and last_detail_at is not None and now - last_detail_at < 1:
                    return
                agent_manager.record_deployment(agent_id, deployment_id, {
                    'status': 'preparing', 'message': event_message(message, (agent.get('token'),))})
                if not force:
                    last_detail_at = now

        def rule_detail(done, total, name, phase):
            nonlocal rule_detail_started
            if phase == 'preparing':
                with progress_lock:
                    first = not rule_detail_started
                    rule_detail_started = True
                preparation_detail(f'正在准备规则「{name}」（共 {total} 个文件）', force=first)
            else:
                preparation_detail(f'规则「{name}」已准备（{done}/{total}）', force=done == total)

        def rule_progress(done, total):
            nonlocal last_progress_at
            now = time.monotonic()
            # Persist only useful progress, not one config write per rule file.
            if done not in (0, total) and last_progress_at is not None and now - last_progress_at < 1:
                return
            message = (f'正在准备规则文件 {done}/{total}' if done < total
                       else '规则文件准备完成，正在打包')
            agent_manager.record_deployment(agent_id, deployment_id, {
                'status': 'preparing', 'message': message})
            last_progress_at = now

        prepared = _prepare_transactional_content(agent, config_data, base_url=base_url,
                                                   deployment_id=deployment_id, progress=rule_progress,
                                                   detail_progress=rule_detail, phase_progress=preparation_detail,
                                                   store_downloads=True)
        bundle = prepared['bundle']
        prepared_providers = prepared['prepared_providers']
        # Publish caches only after all files and the exact graph are ready.
        # (Subscriptions downloaded for this push were validated with that
        # graph and saved during preparation, so the files use the saved nodes.)
        if prepared_providers:
            commit_provider_bundle(profile_id, prepared_providers)
        agent_manager.record_deployment(agent_id, deployment_id, {
            'status': 'preparing', 'message': '文件已准备，正在校验并连接设备'})
        extra_data = {'deployment_bundle': bundle, 'activate': activate, 'resource_config': config_data,
                      'source_snapshot': config_data,
                      'base_url': base_url, **{key: prepared[key] for key in
                      ('provider_downloads', 'ruleset_downloads', 'custom_files')}}
        result = agent_manager.push_config_to_agent(agent_id, prepared['config_content'], extra_data=extra_data,
                                                    config_revision=config_revision)
        result['profile_id'] = profile_id
        result['file_count'] = len(bundle.manifest['files'])
        result['provider_count'] = len(prepared['provider_downloads'])
        result['rule_count'] = len(prepared['ruleset_downloads'])
        return _deployment_response(result)
    except (StrategyReferenceError, DeploymentPreparationError, ValueError) as exc:
        details = failure_text(str(exc), (agent.get('token'),))
        result = {'success': False, 'status': 'failed', 'failed_stage': 'preparing',
                  'message': details.splitlines()[0][:300] if details else 'Deployment preparation failed',
                  'error_details': details}
        if hasattr(exc, 'issues'):
            result['issues'] = exc.issues
        if deployment_id:
            recorded = agent_manager.record_deployment(agent_id, deployment_id, result)
            result['events'] = recorded.get('events', [])
            result['deployment_id'] = deployment_id
        return jsonify(_public_deployment(result)), 400
    except Exception as exc:
        logger.error('Agent deployment preparation failed (%s)', type(exc).__name__)
        result = {'success': False, 'status': 'failed', 'failed_stage': 'preparing',
                  'message': 'Deployment preparation failed',
                  'error_details': failure_text(f'{type(exc).__name__}: {exc}', (agent.get('token'),))}
        if deployment_id:
            recorded = agent_manager.record_deployment(agent_id, deployment_id, result)
            result['events'] = recorded.get('events', [])
            result['deployment_id'] = deployment_id
        return jsonify(_public_deployment(result)), 500
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
    return jsonify(_public_deployment(result)), code


@bp.route('/<agent_id>/deployments/<deployment_id>', methods=['GET'])
@require_auth
def get_agent_deployment(agent_id, deployment_id):
    result = get_agent_manager().get_deployment(agent_id, deployment_id)
    # A terminal failure is still a successfully retrieved task, not a failed poll.
    code = 200 if result.get('status') in ('succeeded', 'failed', 'rolled_back', 'rollback_failed') else result.get('http_status', 200)
    return jsonify(_public_deployment(result)), code


@bp.route('/<agent_id>/deployments/<deployment_id>/activate', methods=['POST'])
@require_auth
def activate_agent_deployment(agent_id, deployment_id):
    return _deployment_response(get_agent_manager().activate_deployment(agent_id, deployment_id))


@bp.route('/<agent_id>/deployments/<deployment_id>/rollback', methods=['POST'])
@require_auth
def rollback_agent_deployment(agent_id, deployment_id):
    return _deployment_response(get_agent_manager().rollback_deployment(agent_id, deployment_id))


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


# DNS monitoring and its local statistics baseline stay separate from deployment and lifecycle actions.
def _oxidns_monitor_response(operation, agent_id, *args):
    from backend.agents import oxidns_monitor
    try:
        result = getattr(oxidns_monitor, operation)(get_agent_manager(), agent_id, *args)
        response = jsonify(result)
        response.headers['Cache-Control'] = 'no-store'
        return response
    except oxidns_monitor.MonitorError as exc:
        response = jsonify(success=False, code=exc.code, message=exc.message)
        response.headers['Cache-Control'] = 'no-store'
        return response, exc.status


@bp.route('/<agent_id>/mihomo/<path:resource>', methods=['GET', 'PUT', 'PATCH', 'DELETE', 'POST'])
@require_auth
def mihomo_panel(agent_id, resource):
    """Mihomo panel requests; the resource/method whitelist lives in backend.agents.mihomo_panel."""
    from backend.agents import mihomo_panel as panel
    from backend.agents.oxidns_monitor import MonitorError
    try:
        body = request.get_json(silent=True) if request.method in ('PUT', 'PATCH') else None
        response = jsonify(panel.call(get_agent_manager(), agent_id, resource, request.method, request.args, body))
        response.headers['Cache-Control'] = 'no-store'
        return response
    except MonitorError as exc:
        response = jsonify(success=False, code=exc.code, message=exc.message)
        response.headers['Cache-Control'] = 'no-store'
        return response, exc.status


@bp.route('/<agent_id>/oxidns/overview', methods=['GET'])
@require_auth
def get_oxidns_overview(agent_id):
    return _oxidns_monitor_response('overview', agent_id, request.args)


@bp.route('/<agent_id>/oxidns/rules/update', methods=['GET', 'POST'])
@require_auth
def update_oxidns_rules(agent_id):
    from backend.agents import oxidns_rule_update
    from backend.agents.oxidns_monitor import MonitorError

    try:
        # Only the operation itself is exposed, never arbitrary core paths,
        # credentials, plugin tags or download URLs supplied by a browser.
        if request.args or (request.content_length or 0) > 1024:
            raise oxidns_rule_update._fail('INVALID_REQUEST', 400)
        raw = request.stream.read(1025)
        if len(raw) > 1024 or request.method == 'GET' and raw:
            raise oxidns_rule_update._fail('INVALID_REQUEST', 400)
        if raw:
            try:
                valid = request.is_json and json.loads(raw) == {}
            except (ValueError, UnicodeError, RecursionError):
                valid = False
            if not valid:
                raise oxidns_rule_update._fail('INVALID_REQUEST', 400)
        data, status = oxidns_rule_update.call(get_agent_manager(), agent_id, request.method)
    except MonitorError as exc:
        data, status = {'success': False, 'code': exc.code, 'message': exc.message}, exc.status
    response = jsonify(data)
    response.headers['Cache-Control'] = 'no-store'
    return response, status


@bp.route('/<agent_id>/oxidns/cache', methods=['GET'])
@bp.route('/<agent_id>/oxidns/cache/flush', methods=['POST'])
@require_auth
def oxidns_cache(agent_id):
    from backend.agents import oxidns_cache as caches
    from backend.agents.oxidns_monitor import MonitorError

    try:
        # The browser names only cache tags of this one Agent; the Agent
        # rechecks them against its own deployed configuration.
        if request.args or (request.content_length or 0) > 16 * 1024:
            raise caches._fail('INVALID_REQUEST', 400)
        raw = request.stream.read(16 * 1024 + 1)
        tags = None
        if request.method == 'GET':
            if raw:
                raise caches._fail('INVALID_REQUEST', 400)
        else:
            try:
                body = json.loads(raw) if request.is_json and len(raw) <= 16 * 1024 else None
            except (ValueError, UnicodeError, RecursionError):
                body = None
            if not isinstance(body, dict) or set(body) != {'tags'} or not caches.valid_request_tags(body['tags']):
                raise caches._fail('INVALID_REQUEST', 400)
            tags = body['tags']
        data, status = caches.call(get_agent_manager(), agent_id, request.method, tags), 200
    except MonitorError as exc:
        data, status = {'success': False, 'code': exc.code, 'message': exc.message}, exc.status
        if hasattr(exc, 'flushed'):
            data['flushed'] = exc.flushed
    response = jsonify(data)
    response.headers['Cache-Control'] = 'no-store'
    return response, status


@bp.route('/<agent_id>/oxidns/dashboard/reset', methods=['POST'])
@require_auth
def reset_oxidns_dashboard(agent_id):
    def invalid_request():
        response = jsonify(success=False, code='INVALID_QUERY', message='重置请求格式无效。')
        response.headers['Cache-Control'] = 'no-store'
        return response, 400

    if request.args or request.content_length is not None and request.content_length > 2048:
        return invalid_request()
    # Bound chunked requests too, rather than trusting Content-Length or reading
    # an arbitrary body into the JSON parser.
    raw = request.stream.read(2049)
    if len(raw) > 2048 or raw and not request.is_json:
        return invalid_request()
    try:
        body = json.loads(raw) if raw else {}
    except (ValueError, UnicodeError, RecursionError):
        return invalid_request()
    return _oxidns_monitor_response('reset_dashboard', agent_id, body)


@bp.route('/<agent_id>/oxidns/clients', methods=['GET'])
@require_auth
def get_oxidns_clients(agent_id):
    return _oxidns_monitor_response('clients', agent_id, request.args)


@bp.route('/<agent_id>/oxidns/queries', methods=['GET'])
@require_auth
def get_oxidns_queries(agent_id):
    return _oxidns_monitor_response('queries', agent_id, request.args)


@bp.route('/<agent_id>/oxidns/queries/<record_id>', methods=['GET'])
@require_auth
def get_oxidns_query(agent_id, record_id):
    return _oxidns_monitor_response('query', agent_id, record_id, request.args)


@bp.route('/<agent_id>/oxidns/device-policies', methods=['GET', 'PUT'])
@require_auth
def oxidns_device_policies(agent_id):
    """Edit only the bound profile, independently of the global UI selection."""
    from backend.common.config import update_config_transaction
    from backend.common.oxidns_device_policy import read_policies, revision
    from backend.common.device_notes import policy_names_revision, read_notes, save_policy_rows
    from backend.common.oxidns_settings import OxidnsSettingsError

    def response(data, status=200):
        result = jsonify(data)
        result.headers['Cache-Control'] = 'no-store'
        return result, status

    manager = get_agent_manager()
    agent = manager.get_agent_by_id(agent_id)
    if not agent:
        return response({'message': 'Agent 不存在。'}, 404)
    if agent.get('service_type') != 'oxidns':
        return response({'message': '设备策略仅支持 OxiDNS Agent。'}, 400)
    profile_id = agent.get('profile_id') or get_default_profile_id()
    try:
        if request.method == 'PUT':
            body = request.get_json(silent=True)
            if not isinstance(body, dict) or set(body) - {'profile_id', 'revision', 'notes_revision', 'policies', 'base_url'} or \
                    not isinstance(body.get('base_url', ''), str):
                return response({'message': '设备策略请求格式无效。'}, 400)
            if body.get('profile_id') != profile_id:
                return response({'message': 'Agent 绑定的配置空间已改变，请重新载入。'}, 409)

            def update(config):
                # Recheck binding under the repository transaction lock.
                latest = manager.get_agent_by_id(agent_id)
                if not latest or latest.get('service_type') != 'oxidns' or \
                        (latest.get('profile_id') or get_default_profile_id()) != profile_id or \
                        body.get('revision') != revision(config.get('oxidns', {})) or \
                        body.get('notes_revision') != policy_names_revision(config):
                    raise PolicyConflict('配置或设备绑定已更新，请重新载入后再保存。')
                config['oxidns'] = save_policy_rows(config, config.get('oxidns', {}), body.get('policies'),
                                                    body.get('base_url', ''))

            update_config_transaction(update, profile_id)
        config = get_config(profile_id)
        settings = config.get('oxidns', {})
        profile = get_repository().get_profile(profile_id)
        peers = [{'id': item['id'], 'name': item.get('name') or item['id']}
                 for item in manager.get_all_agents()
                 if item.get('service_type') == 'oxidns' and
                 (item.get('profile_id') or get_default_profile_id()) == profile_id]
        return response({'success': True, 'draft': True, 'profile_id': profile_id,
                         'profile_name': profile.get('name') or profile_id,
                         'revision': revision(settings), 'notes_revision': policy_names_revision(config),
                         'agents': peers, 'device_notes': read_notes(config),
                         'deployment': _public_deployment(agent['latest_deployment']) if agent.get('latest_deployment') else None,
                         **read_policies(settings, config)})
    except PolicyConflict as exc:
        return response({'message': str(exc)}, 409)
    except (OxidnsSettingsError, ProfileRepositoryError) as exc:
        return response({'message': str(exc)}, 400)


class PolicyConflict(ValueError):
    pass
