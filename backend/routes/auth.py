"""认证相关路由"""
import re
from flask import request, jsonify
from backend.routes import auth_bp
from backend.common.internal_call import is_internal_call
from backend.common.auth import is_auth_enabled, generate_token, parse_bearer_token, verify_token, require_auth, ADMIN_USERNAME, ADMIN_PASSWORD, JWT_EXPIRATION_HOURS
from backend.utils.logger import client_ip, get_logger, log_text

logger = get_logger(__name__)

@auth_bp.route('/status', methods=['GET'])
def auth_status():
    return jsonify({'authEnabled': is_auth_enabled()})

@auth_bp.route('/login', methods=['POST'])
def login():
    try:
        data = request.json
        username, password = data.get('username'), data.get('password')
        if not is_auth_enabled():
            return jsonify({'success': False, 'message': '当前未开启登录验证。'}), 400
        if username == ADMIN_USERNAME and password == ADMIN_PASSWORD:
            logger.info('登录成功：用户 %s，来源 %s', log_text(username), client_ip())
            return jsonify({'success': True, 'token': generate_token(username), 'username': username, 'expiresIn': JWT_EXPIRATION_HOURS * 3600})
        logger.warning('登录失败：用户名或密码错误，用户 %s，来源 %s', log_text(username), client_ip())
        return jsonify({'success': False, 'message': '用户名或密码错误，请重新输入。'}), 401
    except Exception as e:
        return jsonify({'success': False, 'message': f'登录失败。原始错误：{e}'}), 500

@auth_bp.route('/verify', methods=['GET'])
@require_auth
def verify():
    return jsonify({'success': True, 'message': '登录状态有效。'})

def setup_before_request(app):
    @app.before_request
    def before_request_auth():
        if is_internal_call() or not is_auth_enabled():
            return None
        public_exact_paths = {
            '/api/auth/status', '/api/auth/login', '/api/config/mihomo', '/api/config/surge', '/api/config/loon', '/api/config/mosdns', '/api/config/oxidns',
            '/api/mosdns/rule-proxy',
            '/api/agents/register', '/api/agents/install-script', '/api/agents/core-install-script', '/api/agents/docker-compose', '/api/agents/docker-run',
            '/api/agents/gateway-network-probe.sh',
            '/api/agents/docker-mihomo-compose', '/api/agents/docker-mihomo-run', '/api/agents/docker-mosdns-compose',
            '/api/agents/docker-mosdns-run', '/api/version', '/', '/mcp',
        }
        public_prefixes = ('/api/rule-library/content/', '/api/rules/local/', '/mcp/', '/api/static/agents/', '/assets/', '/static/')
        # Installers fetch allowlisted binaries and expiring, random-token script
        # snapshots without a panel JWT. Snapshot creation still requires login.
        if request.endpoint in {'agents.download_agent_binary', 'agents.download_install_command'}:
            return None
        # The gateway installer authenticates a short-lived session capability.
        if request.endpoint in {'agents.gateway_installation_script', 'agents.gateway_installation_enroll',
                                'agents.docker_oxidns_ha_enroll'}:
            return None
        # These routes validate their own Agent credentials (or, for upgrade
        # status, an admin JWT). Do not require a JWT before their checks run.
        if request.endpoint in {
            'agents.agent_heartbeat', 'agents.agent_traffic_report', 'agents.agent_traffic_history_report',
            'agents.get_agent_config',
            'agents.get_agent_upgrade', 'agents.report_agent_upgrade',
            'custom_config.linux_gateway_rule_provider', 'profiles.linux_gateway_rule_provider',
            'custom_config.linux_gateway_source_provider', 'profiles.linux_gateway_source_provider',
        }:
            return None
        profile_public_patterns = (
            r'^/api/config/[A-Za-z0-9][A-Za-z0-9_-]{0,63}/(?:mihomo|surge|loon|mosdns|oxidns)$',
            r'^/api/config/[A-Za-z0-9][A-Za-z0-9_-]{0,63}/loon/[^/]+$',
            r'^/api/profiles/[A-Za-z0-9][A-Za-z0-9_-]{0,63}/mosdns/rule-proxy$',
            r'^/api/profiles/[A-Za-z0-9][A-Za-z0-9_-]{0,63}/(?:subscriptions/[^/]+/proxies|aggregations/[^/]+/provider|rules/local/[^/]+|rule-library/content/[^/]+)$',
        )
        is_public = request.path in public_exact_paths or any(request.path.startswith(path) for path in public_prefixes)
        is_public = is_public or any(re.match(pattern, request.path) for pattern in profile_public_patterns)
        if is_public:
            return None
        token = parse_bearer_token(request.headers.get('Authorization'))
        if token is None:
            return jsonify({'success': False, 'message': '请先登录后再操作。'}), 401
        payload = verify_token(token)
        if not payload or (isinstance(payload, dict) and 'error' in payload):
            return jsonify({'success': False, 'message': '登录已失效，请重新登录。'}), 401
        return None
