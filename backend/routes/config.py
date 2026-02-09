"""配置管理路由"""
import os
import json
import copy
import tempfile
import jwt

from flask import request, jsonify, send_file

from backend.routes import config_bp
from backend.common.auth import require_auth, JWT_SECRET_KEY, JWT_ALGORITHM
from backend.common.config import get_config, save_config, CONFIG_FILE
from backend.converters.mihomo import generate_mihomo_config
from backend.converters.surge import generate_surge_config
from backend.converters.mosdns import generate_mosdns_config


@config_bp.route('/mihomo', methods=['GET'])
def get_mihomo_config():
    """获取 Mihomo 配置内容（通过 URL 访问）

    支持两种授权方式：
    1. 前端请求：使用 Authorization header (Bearer token)
    2. 外部请求：使用 URL 参数 ?token=xxx
    """
    try:
        config_data = get_config()

        # 检查是否有 Authorization header（前端请求）
        auth_header = request.headers.get('Authorization', '')
        has_valid_jwt = False

        if auth_header and auth_header.startswith('Bearer '):
            # 尝试验证 JWT token
            try:
                jwt_token = auth_header.split(' ')[1]
                jwt.decode(jwt_token, JWT_SECRET_KEY, algorithms=[JWT_ALGORITHM])
                has_valid_jwt = True
            except:
                pass

        # 如果没有有效的 JWT，检查配置令牌（外部请求）
        if not has_valid_jwt:
            config_token = config_data.get('system_config', {}).get('config_token', '')
            if config_token:
                # 从查询参数获取令牌
                request_token = request.args.get('token', '')
                if not request_token or request_token != config_token:
                    return jsonify({'success': False, 'message': 'Invalid or missing token'}), 401
            # 如果没有配置令牌，则允许访问（向后兼容）

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
        config_data = get_config()

        # 检查是否有 Authorization header（前端请求）
        auth_header = request.headers.get('Authorization', '')
        has_valid_jwt = False

        if auth_header and auth_header.startswith('Bearer '):
            # 尝试验证 JWT token
            try:
                jwt_token = auth_header.split(' ')[1]
                jwt.decode(jwt_token, JWT_SECRET_KEY, algorithms=[JWT_ALGORITHM])
                has_valid_jwt = True
            except:
                pass

        # 如果没有有效的 JWT，检查配置令牌（外部请求）
        if not has_valid_jwt:
            config_token = config_data.get('system_config', {}).get('config_token', '')
            if config_token:
                # 从查询参数获取令牌
                request_token = request.args.get('token', '')
                if not request_token or request_token != config_token:
                    return jsonify({'success': False, 'message': 'Invalid or missing token'}), 401
            # 如果没有配置令牌，则允许访问（向后兼容）

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
        config_data = get_config()

        # 检查是否有 Authorization header（前端请求）
        auth_header = request.headers.get('Authorization', '')
        has_valid_jwt = False

        if auth_header and auth_header.startswith('Bearer '):
            # 尝试验证 JWT token
            try:
                jwt_token = auth_header.split(' ')[1]
                jwt.decode(jwt_token, JWT_SECRET_KEY, algorithms=[JWT_ALGORITHM])
                has_valid_jwt = True
            except:
                pass

        # 如果没有有效的 JWT，检查配置令牌（外部请求）
        if not has_valid_jwt:
            config_token = config_data.get('system_config', {}).get('config_token', '')
            if config_token:
                # 从查询参数获取令牌
                request_token = request.args.get('token', '')
                if not request_token or request_token != config_token:
                    return jsonify({'success': False, 'message': 'Invalid or missing token'}), 401
            # 如果没有配置令牌，则允许访问（向后兼容）

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
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@config_bp.route('/export', methods=['GET'])
@require_auth
def export_config():
    """导出配置为 JSON"""
    config_data = get_config()

    # 检查是否是脱敏导出
    desensitize = request.args.get('desensitize', 'false').lower() == 'true'

    if desensitize:
        # 脱敏导出：移除敏感信息
        # 深拷贝配置数据，避免影响原始数据
        desensitized_data = copy.deepcopy(config_data)

        # 脱敏订阅的 URL
        for sub in desensitized_data.get('subscriptions', []):
            sub['url'] = '***已脱敏***'

        # 脱敏节点的 proxy_string
        for node in desensitized_data.get('nodes', []):
            if 'proxy_string' in node:
                node['proxy_string'] = '***已脱敏***'

        # 创建临时文件
        with tempfile.NamedTemporaryFile(mode='w', suffix='.json', delete=False, encoding='utf-8') as f:
            json.dump(desensitized_data, f, ensure_ascii=False, indent=2)
            temp_file = f.name

        try:
            return send_file(temp_file, as_attachment=True, download_name='config_desensitized.json')
        finally:
            # 清理临时文件
            try:
                os.unlink(temp_file)
            except:
                pass
    else:
        # 正常导出
        return send_file(CONFIG_FILE, as_attachment=True, download_name='config.json')


@config_bp.route('/import', methods=['POST'])
@require_auth
def import_config():
    """导入配置"""
    try:
        from backend.common.config import config_data as global_config
        global_config.clear()
        global_config.update(request.json)
        save_config()
        return jsonify({'success': True})
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@config_bp.route('/reset', methods=['POST'])
@require_auth
def reset_config():
    """重置配置为默认模板"""
    try:
        from backend.common.config import config_data as global_config

        # 读取模板配置
        template_path = os.path.join(os.path.dirname(os.path.dirname(__file__)), 'config_template.json')
        with open(template_path, 'r', encoding='utf-8') as f:
            template_data = json.load(f)

        # 清空当前配置并使用模板数据
        global_config.clear()
        global_config.update(template_data)
        save_config()

        return jsonify({'success': True, 'message': '配置已重置为默认值'})
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


# 自定义配置路由（使用独立蓝图）

from backend.routes import custom_config_bp


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
        return jsonify({'config': mihomo_config})

    elif request.method == 'POST':
        # 保存自定义配置（保存到嵌套结构中）
        try:
            custom_config = request.json.get('config', '')
            config_data['mihomo']['custom_config'] = custom_config
            save_config()
            return jsonify({'success': True})
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
            save_config()
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
            save_config()
            return jsonify({'success': True})
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500
