"""MosDNS 配置路由模块"""
import logging
import os
from urllib.parse import urlparse

from flask import request, jsonify

from backend.converters.mihomo import apply_github_proxy_domain
from backend.converters.mosdns import RULE_PARTS, convert_rule_content_for_mosdns
from backend.routes import mosdns_bp as bp
from backend.common.auth import require_auth
from backend.common.config import get_config, save_config
from backend.utils.rule_utils import get_rules_dir, sanitize_rule_name
from backend.utils.rule_fetch import request_rule, validate_rule_url
from backend.utils.url_utils import safe_exception_details, safe_url_for_log

logger = logging.getLogger(__name__)


def _load_cached_rule_content_for_url(original_url: str, config_data=None) -> str:
    """尝试从本地规则缓存中读取与 URL 对应的规则内容。"""
    if config_data is None:
        config_data = get_config()
    if not original_url:
        return ''

    candidate_names = []

    # 1. 优先从 rule_configs / rule_library 中按 URL 反查规则名称
    for rule_item in config_data.get('rule_configs', []):
        if rule_item.get('itemType') == 'ruleset' and rule_item.get('url') == original_url:
            name = rule_item.get('name', '')
            if name:
                candidate_names.append(name)

    for library_item in config_data.get('rule_library', []):
        if library_item.get('url') == original_url:
            name = library_item.get('name', '')
            if name:
                candidate_names.append(name)

    # 2. 如果是 rule-library/content/<id> 形式，按 id 找规则库名称
    parsed = urlparse(original_url)
    path = parsed.path or ''
    marker = '/rule-library/content/'
    if marker in path:
        rule_id = path.split(marker, 1)[1].strip('/').split('/', 1)[0]
        if rule_id:
            library_item = next(
                (r for r in config_data.get('rule_library', []) if r.get('id') == rule_id),
                None
            )
            if library_item and library_item.get('name'):
                candidate_names.append(library_item['name'])

    # 去重并尝试读取缓存文件
    seen = set()
    for name in candidate_names:
        if not name or name in seen:
            continue
        seen.add(name)

        filename = f"{sanitize_rule_name(name)}.list"
        filepath = os.path.join(get_rules_dir(), filename)
        if os.path.exists(filepath):
            try:
                with open(filepath, 'r', encoding='utf-8') as f:
                    cached_content = f.read()
                logger.info(f"使用本地规则缓存兜底: {filepath}")
                return cached_content
            except Exception as e:
                logger.warning(f"读取本地规则缓存失败 {filepath}: {e}")

    return ''


@bp.route('/rulesets', methods=['GET', 'POST'])
@require_auth
def handle_mosdns_rulesets():
    """MosDNS 规则集管理"""
    config_data = get_config()
    # 确保 mosdns 字段存在
    if 'mosdns' not in config_data:
        config_data['mosdns'] = {
            'direct_rulesets': [],
            'proxy_rulesets': [],
            'direct_rules': [],
            'proxy_rules': [],
            'local_dns': '',
            'remote_dns': '',
            'fallback_dns': '',
            'default_forward': 'forward_remote',
            'custom_hosts': '',
            'custom_config': ''
        }

    mosdns_config = config_data['mosdns']

    if request.method == 'GET':
        return jsonify({
            'direct_rulesets': mosdns_config.get('direct_rulesets', []),
            'proxy_rulesets': mosdns_config.get('proxy_rulesets', []),
            'direct_rules': mosdns_config.get('direct_rules', []),
            'proxy_rules': mosdns_config.get('proxy_rules', [])
        })

    elif request.method == 'POST':
        try:
            data = request.json
            mosdns_config['direct_rulesets'] = data.get('direct_rulesets', [])
            mosdns_config['proxy_rulesets'] = data.get('proxy_rulesets', [])
            mosdns_config['direct_rules'] = data.get('direct_rules', [])
            mosdns_config['proxy_rules'] = data.get('proxy_rules', [])
            save_config(config_data)
            return jsonify({'success': True})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/custom-matches', methods=['GET', 'POST'])
@require_auth
def handle_mosdns_custom_matches():
    """MosDNS 自定义匹配规则管理"""
    config_data = get_config()
    # 确保 mosdns 字段存在
    if 'mosdns' not in config_data:
        config_data['mosdns'] = {
            'direct_rulesets': [],
            'proxy_rulesets': [],
            'direct_rules': [],
            'proxy_rules': [],
            'local_dns': '',
            'remote_dns': '',
            'fallback_dns': '',
            'default_forward': 'forward_remote',
            'custom_hosts': '',
            'custom_config': '',
            'custom_matches': [],
            'custom_match_position': 'tail'
        }

    mosdns_config = config_data['mosdns']

    if request.method == 'GET':
        return jsonify({
            'custom_matches': mosdns_config.get('custom_matches', []),
            'position': mosdns_config.get('custom_match_position', 'tail')
        })

    elif request.method == 'POST':
        try:
            data = request.json
            mosdns_config['custom_matches'] = data.get('custom_matches', [])
            mosdns_config['custom_match_position'] = data.get('position', 'tail')
            save_config(config_data)
            return jsonify({'success': True})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/dns-servers', methods=['GET', 'POST'])
@require_auth
def handle_mosdns_dns_servers():
    """MosDNS DNS 服务器配置"""
    config_data = get_config()
    # 确保 mosdns 字段存在
    if 'mosdns' not in config_data:
        config_data['mosdns'] = {
            'direct_rulesets': [],
            'proxy_rulesets': [],
            'direct_rules': [],
            'proxy_rules': [],
            'local_dns': '',
            'remote_dns': '',
            'fallback_dns': '',
            'default_forward': 'forward_remote',
            'custom_hosts': '',
            'custom_config': ''
        }

    mosdns_config = config_data['mosdns']

    if request.method == 'GET':
        return jsonify({
            'local_dns': mosdns_config.get('local_dns', ''),
            'remote_dns': mosdns_config.get('remote_dns', ''),
            'fallback_dns': mosdns_config.get('fallback_dns', ''),
            'default_forward': mosdns_config.get('default_forward', 'forward_remote'),
            'custom_hosts': mosdns_config.get('custom_hosts', '')
        })

    elif request.method == 'POST':
        try:
            data = request.json
            mosdns_config['local_dns'] = data.get('local_dns', '')
            mosdns_config['remote_dns'] = data.get('remote_dns', '')
            mosdns_config['fallback_dns'] = data.get('fallback_dns', '')
            mosdns_config['default_forward'] = data.get('default_forward', 'forward_remote')
            mosdns_config['custom_hosts'] = data.get('custom_hosts', '')
            save_config(config_data)
            return jsonify({'success': True})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/log-settings', methods=['GET', 'POST'])
@require_auth
def handle_mosdns_log_settings():
    """MosDNS 日志设置"""
    config_data = get_config()
    # 确保 mosdns 字段存在
    if 'mosdns' not in config_data:
        config_data['mosdns'] = {
            'direct_rulesets': [],
            'proxy_rulesets': [],
            'direct_rules': [],
            'proxy_rules': [],
            'local_dns': '',
            'remote_dns': '',
            'fallback_dns': '',
            'default_forward': 'forward_remote',
            'custom_hosts': '',
            'custom_config': '',
            'log_enabled': True,
            'log_level': 'info',
            'log_file': ''
        }

    mosdns_config = config_data['mosdns']

    if request.method == 'GET':
        return jsonify({
            'log_enabled': mosdns_config.get('log_enabled', True),
            'log_level': mosdns_config.get('log_level', 'info'),
            'log_file': mosdns_config.get('log_file', '')
        })

    elif request.method == 'POST':
        try:
            data = request.json
            mosdns_config['log_enabled'] = data.get('log_enabled', True)
            mosdns_config['log_level'] = data.get('log_level', 'info')
            mosdns_config['log_file'] = data.get('log_file', '')
            save_config(config_data)
            return jsonify({'success': True})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/api-settings', methods=['GET', 'POST'])
@require_auth
def handle_mosdns_api_settings():
    """MosDNS API 设置"""
    config_data = get_config()
    # 确保 mosdns 字段存在
    if 'mosdns' not in config_data:
        config_data['mosdns'] = {
            'direct_rulesets': [],
            'proxy_rulesets': [],
            'direct_rules': [],
            'proxy_rules': [],
            'local_dns': '',
            'remote_dns': '',
            'fallback_dns': '',
            'default_forward': 'forward_remote',
            'custom_hosts': '',
            'custom_config': '',
            'api_enabled': True,
            'api_address': ':8080'
        }

    mosdns_config = config_data['mosdns']

    if request.method == 'GET':
        return jsonify({
            'api_enabled': mosdns_config.get('api_enabled', True),
            'api_address': mosdns_config.get('api_address', mosdns_config.get('api_addr', ':8080'))  # 兼容旧字段
        })

    elif request.method == 'POST':
        try:
            data = request.json
            mosdns_config['api_enabled'] = data.get('api_enabled', True)
            mosdns_config['api_address'] = data.get('api_address', ':8080')
            # 清理旧字段
            if 'api_addr' in mosdns_config:
                del mosdns_config['api_addr']
            save_config(config_data)
            return jsonify({'success': True})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/cache-settings', methods=['GET', 'POST'])
@require_auth
def handle_mosdns_cache_settings():
    """MosDNS 缓存设置"""
    config_data = get_config()
    # 确保 mosdns 字段存在
    if 'mosdns' not in config_data:
        config_data['mosdns'] = {
            'direct_rulesets': [],
            'proxy_rulesets': [],
            'direct_rules': [],
            'proxy_rules': [],
            'local_dns': '',
            'remote_dns': '',
            'fallback_dns': '',
            'default_forward': 'forward_remote',
            'custom_hosts': '',
            'custom_config': '',
            'cache_enabled': True,
            'cache_size': 10240,
            'cache_lazy_ttl': 21600,
            'cache_dump_enabled': True,
            'cache_dump_file': './cache.dump',
            'cache_dump_interval': 300
        }

    mosdns_config = config_data['mosdns']

    # 确保字段存在（兼容老配置）
    if 'cache_enabled' not in mosdns_config:
        mosdns_config['cache_enabled'] = True
    if 'cache_size' not in mosdns_config:
        mosdns_config['cache_size'] = 10240
    if 'cache_lazy_ttl' not in mosdns_config:
        mosdns_config['cache_lazy_ttl'] = 21600
    if 'cache_dump_enabled' not in mosdns_config:
        mosdns_config['cache_dump_enabled'] = True
    if 'cache_dump_file' not in mosdns_config:
        mosdns_config['cache_dump_file'] = './cache.dump'
    if 'cache_dump_interval' not in mosdns_config:
        mosdns_config['cache_dump_interval'] = 300

    if request.method == 'GET':
        return jsonify({
            'cache_enabled': mosdns_config.get('cache_enabled', True),
            'cache_size': mosdns_config.get('cache_size', 10240),
            'cache_lazy_ttl': mosdns_config.get('cache_lazy_ttl', 21600),
            'cache_dump_enabled': mosdns_config.get('cache_dump_enabled', True),
            'cache_dump_file': mosdns_config.get('cache_dump_file', './cache.dump'),
            'cache_dump_interval': mosdns_config.get('cache_dump_interval', 300)
        })

    elif request.method == 'POST':
        try:
            data = request.json or {}

            mosdns_config['cache_enabled'] = bool(data.get('cache_enabled', True))
            mosdns_config['cache_dump_enabled'] = bool(data.get('cache_dump_enabled', True))

            # 数字字段做简单容错
            def _to_int(value, default: int) -> int:
                try:
                    return int(value)
                except Exception:
                    return default

            mosdns_config['cache_size'] = _to_int(data.get('cache_size', 10240), 10240)
            mosdns_config['cache_lazy_ttl'] = _to_int(data.get('cache_lazy_ttl', 21600), 21600)
            dump_interval = _to_int(data.get('cache_dump_interval', 300), 300)
            mosdns_config['cache_dump_interval'] = dump_interval if dump_interval > 0 else 300

            dump_file = data.get('cache_dump_file', './cache.dump')
            mosdns_config['cache_dump_file'] = str(dump_file) if dump_file is not None else './cache.dump'

            save_config(config_data)
            return jsonify({'success': True})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


_MAX_RULE_PROXY_BYTES = 5 * 1024 * 1024
_MAX_RULE_PROXY_REDIRECTS = 3


def _validate_remote_url(url: str) -> str:
    validate_rule_url(url)
    return url


def _fetch_remote_content(url: str, config_data=None) -> str:
    import requests

    response = request_rule(
        url, timeout=(3, 10), stream=True,
        max_redirects=_MAX_RULE_PROXY_REDIRECTS,
        config_data=config_data,
    )
    try:
        if not 200 <= response.status_code < 300:
            raise requests.exceptions.HTTPError(
                f'Remote server returned HTTP {response.status_code}', response=response,
            )
        content_length = response.headers.get('Content-Length')
        if content_length and int(content_length) > _MAX_RULE_PROXY_BYTES:
            raise ValueError('Remote response exceeds size limit')
        chunks, total = [], 0
        for chunk in response.iter_content(64 * 1024):
            total += len(chunk)
            if total > _MAX_RULE_PROXY_BYTES:
                raise ValueError('Remote response exceeds size limit')
            chunks.append(chunk)
        return b''.join(chunks).decode('utf-8', errors='replace')
    finally:
        response.close()


def _require_rule_proxy_auth():
    from backend.common.auth import is_token_within_length, parse_bearer_token, verify_token
    from backend.common.config import get_repository
    from backend.common.internal_call import is_internal_call

    # MCP 层发起的进程内调用，认证已在 /mcp 入口完成（与 validate_token_or_jwt 一致）
    if is_internal_call():
        return True
    system_config = get_config().get('system_config', {})
    config_token = system_config.get('config_token', '')
    rule_proxy_token = system_config.get('rule_proxy_token', '')
    header = request.headers.get('Authorization', '')
    bearer = parse_bearer_token(header) or ''
    internal_tokens = get_repository().rule_proxy_tokens_for_sanitization()
    retired_tokens = internal_tokens - {rule_proxy_token}
    if bearer in retired_tokens:
        return False
    if bearer:
        payload = verify_token(bearer)
        if payload and not (isinstance(payload, dict) and 'error' in payload):
            return True
    url_token = request.args.get('token', '')
    if not is_token_within_length(url_token):
        url_token = ''
    if url_token in retired_tokens:
        return False
    return bool(
        (rule_proxy_token and url_token == rule_proxy_token)
        or (config_token and url_token == config_token)
    )


def _local_content_rule(original_url, config):
    """Resolve only this profile's inline library sources, without self-HTTP."""
    from backend.common.profile_context import profile_api_path

    parsed = urlparse(original_url)
    base = config.get('system_config', {}).get('server_domain', '').strip() or request.host_url.rstrip('/')
    origin = urlparse(base)
    if parsed.scheme or parsed.netloc:
        if (parsed.scheme, parsed.netloc) != (origin.scheme, origin.netloc):
            return None
    if parsed.query or parsed.fragment:
        return None
    selected = {rule.get('library_rule_id') for rule in config.get('rule_configs', [])
                if rule.get('enabled', True) and rule.get('library_enabled', True)}
    for rule in config.get('rule_library', []):
        if rule.get('id') not in selected or rule.get('source_type') != 'content':
            continue
        path = profile_api_path(config, f'/rule-library/content/{rule["id"]}')
        if parsed.path in {path, origin.path.rstrip('/') + path}:
            return rule.get('content', '')
    return None


@bp.route('/rule-proxy', methods=['GET'])
def mosdns_rule_proxy():
    """
    MosDNS 规则代理接口
    拉取原始规则文件，转换格式后返回

    支持的格式转换：
    1. Clash 格式:
       - DOMAIN-SUFFIX,example.com → domain:example.com
       - DOMAIN,example.com → full:example.com
       - DOMAIN-KEYWORD,example → keyword:example
       - DOMAIN-REGEX,^.*example.*$ → regexp:^.*example.*$

    2. List 格式:
       - +.example.com → domain:example.com (匹配域名及所有子域名)
       - .example.com → regexp:.+\\.example\\.com$ (仅匹配子域名)
       - *.example.com → regexp:^[^.]+\\.example\\.com$ (仅匹配直接子域名)
       - example.com → full:example.com (精确匹配)
       - ip -> ip

    3. part 参数（可选）：
       - 不传：兼容旧行为，域名规则 + 裸 IP 行
       - domain：只返回域名规则（domain_set）
       - ip：只返回 IP/CIDR，含 Clash 的 IP-CIDR/IP-CIDR6（ip_set）

    注意：如果内容已经是 mosdns 格式，则只按 part 过滤，不进行转换
    """
    try:
        import requests

        config_data = get_config()
        if not _require_rule_proxy_auth():
            return jsonify({'success': False, 'message': 'Unauthorized'}), 401

        # 获取原始 URL
        original_url = request.args.get('url')
        if not original_url:
            return jsonify({'success': False, 'message': 'URL parameter is required'}), 400
        part = request.args.get('part') or None
        if part is not None and part not in RULE_PARTS:
            return jsonify({'success': False, 'message': 'Invalid part parameter'}), 400

        original_content = _local_content_rule(original_url, config_data)
        if original_content is None:
            # Use normal hostname routing, including Fake-IP networks and the download proxy.
            fetch_url = apply_github_proxy_domain(original_url, config_data)
            _validate_remote_url(fetch_url)
            try:
                original_content = _fetch_remote_content(fetch_url)
            except requests.exceptions.RequestException as e:
                logger.warning(
                    "远程拉取规则失败，尝试使用本地缓存兜底: %s %s",
                    safe_url_for_log(original_url),
                    safe_exception_details(e),
                )
                original_content = _load_cached_rule_content_for_url(original_url)
                if not original_content:
                    return jsonify({'success': False, 'message': 'Failed to fetch original URL'}), 500

        # 格式转换（Clash/List/YAML -> mosdns），按 part 拆分域名 / IP
        converted_content = convert_rule_content_for_mosdns(original_content, part)
        return converted_content, 200, {'Content-Type': 'text/plain; charset=utf-8'}

    except ValueError as e:
        logger.warning("MosDNS rule-proxy rejected request: %s", safe_exception_details(e))
        return jsonify({'success': False, 'message': 'Invalid remote URL'}), 400
    except Exception as e:
        logger.error("MosDNS rule-proxy request failed: %s", safe_exception_details(e))
        return jsonify({'success': False, 'message': 'Failed to process rule proxy request'}), 500
