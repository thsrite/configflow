"""配置管理路由"""
import json
import io
import logging
import re
from urllib.parse import quote
from flask import request, jsonify, send_file

from backend.routes import config_bp
from backend.common.auth import require_auth, validate_token_or_jwt
from backend.common.config import get_config, save_config, get_repository, reset_config_context, save_config_with_publish_guard
from backend.common.config_repository import ProfileInUse, ProfileValidationError
from backend.common.profile_context import resolve_profile_id
from backend.utils.logger import client_ip, get_logger
from backend.utils.strategy_references import StrategyReferenceError

logger = get_logger(__name__)
from backend.converters.mihomo import generate_mihomo_config
from backend.converters.surge import generate_surge_config
from backend.converters.loon import generate_loon_config
from backend.converters.mosdns import generate_mosdns_config
from backend.converters.oxidns import generate_oxidns_config


def _reject_invalid_config_auth(config_data):
    auth_result = validate_token_or_jwt(request, config_data)
    if auth_result.get('valid'):
        return None
    return jsonify({
        'success': False,
        'message': auth_result.get('message', 'Unauthorized'),
    }), 401


@config_bp.route('/mihomo', methods=['GET'])
def get_mihomo_config():
    """获取 Mihomo 配置内容（通过 URL 访问）

    支持两种授权方式：
    1. 前端请求：使用 Authorization header (Bearer token)
    2. 外部请求：使用 URL 参数 ?token=xxx
    """
    try:
        config_data = get_config(resolve_profile_id())

        auth_error = _reject_invalid_config_auth(config_data)
        if auth_error:
            return auth_error

        # 获取前端传递的 base_url（协议 + 主机 + 端口）
        # 优先从query参数获取，如果没有则尝试从JSON body获取（兼容POST请求）
        base_url = request.args.get('base_url', '')
        if not base_url:
            data = request.get_json(silent=True) or {}
            base_url = data.get('base_url', '')

        # 生成配置
        yaml_content = generate_mihomo_config(config_data, base_url=base_url)

        # 直接返回内容，不下载
        return yaml_content, 200, {
            'Content-Type': 'text/plain; charset=utf-8',
            'Content-Disposition': 'inline'
        }
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@config_bp.route('/surge', methods=['GET'])
def get_surge_config():
    """获取 Surge 配置内容（通过 URL 访问）

    支持两种授权方式：
    1. 前端请求：使用 Authorization header (Bearer token)
    2. 外部请求：使用 URL 参数 ?token=xxx
    """
    try:
        config_data = get_config(resolve_profile_id())

        auth_error = _reject_invalid_config_auth(config_data)
        if auth_error:
            return auth_error

        # 获取 base_url（从请求头构建）
        scheme = request.headers.get('X-Forwarded-Proto', request.scheme)
        host = request.headers.get('X-Forwarded-Host', request.host)
        base_url = f"{scheme}://{host}"

        # 生成配置
        config_content = generate_surge_config(config_data, base_url=base_url)

        # 直接返回内容，不下载
        return config_content, 200, {
            'Content-Type': 'text/plain; charset=utf-8',
            'Content-Disposition': 'inline'
        }
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


def loon_config_name(name, fallback=None):
    """配置空间名称去掉文件名非法字符后作为 Loon 配置名"""
    cleaned = re.sub(r'[\\/:*?"<>|\x00-\x1f]+', ' ', str(name or '')).strip().strip('.')
    return cleaned or fallback or 'loon'


@config_bp.route('/loon', methods=['GET'])
def get_loon_config():
    """获取 Loon 配置内容（通过 URL 访问，可用于 loon://import?sub= 导入）

    支持两种授权方式：
    1. 前端请求：使用 Authorization header (Bearer token)
    2. 外部请求：使用 URL 参数 ?token=xxx
    """
    try:
        config_data = get_config(resolve_profile_id())

        auth_error = _reject_invalid_config_auth(config_data)
        if auth_error:
            return auth_error

        scheme = request.headers.get('X-Forwarded-Proto', request.scheme)
        host = request.headers.get('X-Forwarded-Host', request.host)
        base_url = f"{scheme}://{host}"

        config_content = generate_loon_config(config_data, base_url=base_url)

        # Loon 以文件名作为导入后的配置名称，使用配置空间名称
        filename = quote(f"{loon_config_name(config_data.get('name'), config_data.get('profile_id'))}.lcf")
        return config_content, 200, {
            'Content-Type': 'text/plain; charset=utf-8',
            'Content-Disposition': f"inline; filename*=UTF-8''{filename}"
        }
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500

@config_bp.route('/mosdns', methods=['GET'])
def get_mosdns_config():
    """获取 MosDNS 配置内容（通过 URL 访问）

    支持两种授权方式：
    1. 前端请求：使用 Authorization header (Bearer token)
    2. 外部请求：使用 URL 参数 ?token=xxx
    """
    try:
        config_data = get_config(resolve_profile_id())

        auth_error = _reject_invalid_config_auth(config_data)
        if auth_error:
            return auth_error

        # 获取 base_url（从请求头构建）
        scheme = request.headers.get('X-Forwarded-Proto', request.scheme)
        host = request.headers.get('X-Forwarded-Host', request.host)
        base_url = f"{scheme}://{host}"

        # 生成配置
        yaml_content = generate_mosdns_config(config_data, base_url=base_url)

        # 直接返回内容，不下载
        return yaml_content, 200, {
            'Content-Type': 'text/plain; charset=utf-8',
            'Content-Disposition': 'inline'
        }
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@config_bp.route('/<profile_id>/mihomo', methods=['GET'])
def get_profile_mihomo_config(profile_id):
    return get_mihomo_config()


@config_bp.route('/oxidns', methods=['GET'])
@config_bp.route('/<profile_id>/oxidns', methods=['GET'])
def get_oxidns_config(profile_id=None):
    """Profile-scoped live OxiDNS configuration with the usual subscription auth."""
    config_data = get_config(resolve_profile_id(explicit=profile_id))
    auth_error = _reject_invalid_config_auth(config_data)
    if auth_error:
        return auth_error
    scheme = request.headers.get('X-Forwarded-Proto', request.scheme)
    host = request.headers.get('X-Forwarded-Host', request.host)
    try:
        content = generate_oxidns_config(config_data, base_url=f'{scheme}://{host}')
        return content, 200, {'Content-Type': 'text/plain; charset=utf-8',
                             'Content-Disposition': 'inline'}
    except ValueError as error:
        return jsonify({'success': False, 'message': str(error)}), 400


@config_bp.route('/<profile_id>/surge', methods=['GET'])
def get_profile_surge_config(profile_id):
    return get_surge_config()


@config_bp.route('/<profile_id>/loon', methods=['GET'])
@config_bp.route('/<profile_id>/loon/<filename>', methods=['GET'])
def get_profile_loon_config(profile_id, filename=None):
    # filename 仅用于让 Loon 按链接末段命名配置，内容只由 profile_id 决定
    return get_loon_config()

@config_bp.route('/<profile_id>/mosdns', methods=['GET'])
def get_profile_mosdns_config(profile_id):
    return get_mosdns_config()


_CONFIG_PULL_PATH = re.compile(
    r'^/api/config/(?:(?P<profile>[A-Za-z0-9][A-Za-z0-9_-]{0,63})/)?(?P<target>mihomo|surge|loon|mosdns|oxidns)(?:/[^/]+)?$')


def register_config_pull_log(app):
    """Client subscriptions are the only evidence a device actually fetched its config."""
    from backend.common.internal_call import is_internal_call

    @app.after_request
    def log_config_pull(response):
        # The query string carries the config token and is never logged.
        match = _CONFIG_PULL_PATH.match(request.path) if request.method == 'GET' else None
        if match and not is_internal_call():
            from backend.utils.logger import log_text
            logger.log(logging.INFO if response.status_code < 400 else logging.WARNING,
                       '配置被拉取：%s（配置空间 %s），HTTP %s，来源 %s，客户端 %s', match['target'],
                       match['profile'] or '默认', response.status_code, client_ip(),
                       log_text(request.headers.get('User-Agent') or '-', 80))
        return response


@config_bp.route('/export', methods=['GET'])
@require_auth
def export_config():
    """Download a complete, restorable global backup for the administrator."""
    desensitize = request.args.get('desensitize', 'false').lower() == 'true'
    export_data = get_repository().export_all(desensitize=desensitize)
    payload = json.dumps(export_data, ensure_ascii=False, indent=2).encode('utf-8')
    logger.info('已导出全部配置%s，来源 %s', '（脱敏）' if desensitize else '', client_ip())
    return send_file(
        io.BytesIO(payload),
        as_attachment=True,
        download_name='config_desensitized.json' if desensitize else 'config.json',
        mimetype='application/json',
    )


@config_bp.route('/import', methods=['POST'])
@require_auth
def import_config():
    try:
        if not isinstance(request.json, dict):
            return jsonify({'success': False, 'message': '请求数据必须是 JSON 对象'}), 400
        get_repository().import_all(request.json)
        reset_config_context()
        logger.info('已导入全部配置（覆盖现有配置），来源 %s', client_ip())
        return jsonify({'success': True})
    except ProfileInUse as e:
        return jsonify({'success': False, 'message': str(e)}), 409
    except ProfileValidationError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@config_bp.route('/reset', methods=['POST'])
@require_auth
def reset_config():
    """Reset the entire schema, including shared resources and all profiles."""
    try:
        get_repository().reset_all()
        reset_config_context()
        logger.warning('全部配置已重置为默认值，来源 %s', client_ip())

        return jsonify({'success': True, 'message': '配置已重置为默认值'})
    except ProfileInUse as e:
        return jsonify({'success': False, 'message': str(e)}), 409
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


# 自定义配置路由（使用独立蓝图）

from backend.routes import custom_config_bp


def _mihomo_agent_context():
    from backend.common.agent_manager import get_agent_manager
    from backend.common.mihomo_base_settings import agent_context
    return agent_context(get_repository(), get_agent_manager(), resolve_profile_id())


def _saved_mihomo_base(config_data):
    """The saved base document the form's changes are compared with; None when unreadable."""
    import yaml
    from backend.converters.mihomo import _default_mihomo_template
    text = (config_data.get('mihomo') or {}).get('custom_config') or ''
    if not isinstance(text, str) or not text.strip():
        return _default_mihomo_template()
    try:
        document = yaml.safe_load(text)
    except (yaml.YAMLError, RecursionError):
        return None
    return document if isinstance(document, dict) else None


@custom_config_bp.route('/mihomo', methods=['GET', 'POST'])
@require_auth
def handle_custom_mihomo_config():
    """获取或保存 Mihomo 自定义配置"""
    config_data = get_config()

    # 确保 mihomo 字段存在
    if 'mihomo' not in config_data:
        config_data['mihomo'] = {'custom_config': ''}

    if request.method == 'GET':
        # 获取自定义配置（从嵌套结构中读取）
        mihomo_config = config_data['mihomo'].get('custom_config', '')
        from backend.converters.mihomo import _default_mihomo_template
        from backend.common.mihomo_base_settings import takeover_mode
        import yaml
        reserved, hosts = _mihomo_agent_context()
        return jsonify({'config': mihomo_config,
                        'default_config': yaml.safe_dump(_default_mihomo_template(), allow_unicode=True, sort_keys=False),
                        'provider_update_interval': config_data['mihomo'].get('provider_update_interval', 3600),
                        # What the base settings form needs to check what the server checks on save.
                        'base_settings': {'takeover': takeover_mode(config_data),
                                          'reserved_ports': [{'port': port, 'owner': owner} for port, owner in sorted(reserved.items())],
                                          'local_addresses': hosts}})

    elif request.method == 'POST':
        # 保存自定义配置（保存到嵌套结构中）
        try:
            custom_config = request.json.get('config', '')
            if 'provider_update_interval' in request.json:
                from backend.utils.subscription_refresh import validate_refresh_interval
                try:
                    interval = validate_refresh_interval(request.json['provider_update_interval'], 'Mihomo 订阅同步间隔')
                except ValueError as exc:
                    return jsonify({'success': False, 'message': str(exc)}), 400
                config_data['mihomo']['provider_update_interval'] = interval
            # 预先做 YAML 校验：避免把坏的 custom_config 写进配置数据库，
            # 下次生成时反而更难定位问题（生成路径也会 fail-closed）。
            if custom_config and custom_config.strip():
                try:
                    import yaml as _yaml
                    document = _yaml.safe_load(custom_config)
                    from backend.converters.mihomo import validate_ipv6_flags, CustomConfigYAMLError
                    from backend.common.mihomo_base_settings import BaseSettingsError, validate_base_settings
                    try:
                        validate_ipv6_flags(document)
                        if isinstance(document, dict):
                            reserved, hosts = _mihomo_agent_context()
                            validate_base_settings(document, _saved_mihomo_base(config_data),
                                                   reserved_ports=reserved, local_addresses=hosts)
                    except (CustomConfigYAMLError, BaseSettingsError) as error:
                        return jsonify(success=False, message=str(error)), 400
                except _yaml.YAMLError as e:
                    return jsonify({
                        'success': False,
                        'message': f'YAML 语法错误，未保存: {e}',
                    }), 400
            config_data['mihomo']['custom_config'] = custom_config
            guard = save_config_with_publish_guard(config_data, 'mihomo')
            return jsonify({'success': True, 'publish_guard': guard})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@custom_config_bp.route('/surge', methods=['GET', 'POST'])
@require_auth
def handle_custom_surge_config():
    """获取或保存 Surge 自定义配置"""
    config_data = get_config()

    # 确保 surge 字段存在
    if 'surge' not in config_data:
        config_data['surge'] = {'custom_config': ''}

    if request.method == 'GET':
        # 获取自定义配置（从嵌套结构中读取）
        surge_config = config_data['surge']
        return jsonify({
            'config': surge_config.get('custom_config', ''),
            'smart_groups': surge_config.get('smart_groups', [])
        })

    elif request.method == 'POST':
        # 按字段合并更新（而非整体覆盖）
        try:
            data = request.json or {}
            if 'config' in data:
                config_data['surge']['custom_config'] = data['config']
            if 'smart_groups' in data:
                config_data['surge']['smart_groups'] = data['smart_groups']
            guard = save_config_with_publish_guard(config_data, 'surge')
            return jsonify({'success': True, 'publish_guard': guard})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@custom_config_bp.route('/loon', methods=['GET', 'POST'])
@require_auth
def handle_custom_loon_config():
    """获取或保存 Loon 自定义配置"""
    config_data = get_config()
    config_data.setdefault('loon', {'custom_config': ''})

    if request.method == 'GET':
        return jsonify({'config': config_data['loon'].get('custom_config', '')})

    try:
        data = request.json or {}
        if 'config' in data:
            config_data['loon']['custom_config'] = data['config']
        save_config(config_data)
        return jsonify({'success': True})
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500

@custom_config_bp.route('/mosdns', methods=['GET', 'POST'])
@require_auth
def handle_custom_mosdns_config():
    """获取或保存 MosDNS 自定义配置"""
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

    if request.method == 'GET':
        # 获取自定义配置（从嵌套结构中读取）
        mosdns_config = config_data['mosdns'].get('custom_config', '')
        return jsonify({'config': mosdns_config})

    elif request.method == 'POST':
        # 保存自定义配置（保存到嵌套结构中）
        try:
            custom_config = request.json.get('config', '')
            config_data['mosdns']['custom_config'] = custom_config
            guard = save_config_with_publish_guard(config_data, 'mosdns')
            return jsonify({'success': True, 'publish_guard': guard})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500


@custom_config_bp.route('/oxidns', methods=['GET', 'POST'])
@require_auth
def handle_custom_oxidns_config():
    """Keep the native extension YAML in the independent OxiDNS profile."""
    from backend.common.config import update_config_transaction
    from backend.common.oxidns_settings import normalize_oxidns_settings

    if request.method == 'GET':
        return jsonify({'config': get_config().get('oxidns', {}).get('custom_config', '')})
    data = request.get_json(silent=True)
    if not isinstance(data, dict) or not isinstance(data.get('config'), str):
        return jsonify({'success': False, 'message': 'config 必须是 YAML 文本'}), 400
    try:
        def update(config):
            settings = dict(config.get('oxidns', {}))
            settings['custom_config'] = data['config']
            config['oxidns'] = normalize_oxidns_settings(settings, validate_native_arguments=False)
        update_config_transaction(update)
    except ValueError as error:
        return jsonify({'success': False, 'message': str(error)}), 400
    return jsonify({'success': True})
