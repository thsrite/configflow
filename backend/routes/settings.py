"""设置相关路由"""
import os
import json
import tempfile
from datetime import datetime
from flask import request, jsonify, current_app

from backend.routes import settings_bp
from backend.common.auth import require_auth
from backend.common.utils import generate_random_token
from backend.common.config import get_config, save_config
from backend.version import get_version_info


@settings_bp.route('/server-domain', methods=['GET', 'POST'])
@require_auth
def handle_server_domain():
    """服务域名管理"""
    config_data = get_config()

    if request.method == 'GET':
        # 获取当前服务域名
        server_domain = config_data.get('system_config', {}).get('server_domain', '')
        return jsonify({'server_domain': server_domain})

    elif request.method == 'POST':
        # 更新服务域名
        try:
            data = request.json
            new_domain = data.get('new_domain', '').strip()

            if not new_domain:
                return jsonify({'success': False, 'message': 'New domain is required'}), 400

            # 确保 system_config 存在
            if 'system_config' not in config_data:
                config_data['system_config'] = {}

            # 更新配置中的服务域名
            config_data['system_config']['server_domain'] = new_domain

            current_app.logger.info(f"Updated server domain to {new_domain}")

            save_config()
            return jsonify({
                'success': True,
                'server_domain': new_domain
            })
        except Exception as e:
            current_app.logger.error(f"Failed to update server domain: {e}")
            return jsonify({'success': False, 'message': str(e)}), 500


@settings_bp.route('/config-token', methods=['GET', 'POST', 'DELETE'])
@require_auth
def handle_config_token():
    """配置令牌管理"""
    config_data = get_config()

    if request.method == 'GET':
        # 获取当前配置令牌
        config_token = config_data.get('system_config', {}).get('config_token', '')
        return jsonify({'config_token': config_token})

    elif request.method == 'POST':
        # 更新或生成配置令牌
        try:
            data = request.json
            new_token = data.get('token', '').strip()

            # 如果提供了 generate=true，生成随机令牌
            if data.get('generate', False):
                new_token = generate_random_token()

            # 如果没有提供令牌且不生成，返回错误
            if not new_token:
                return jsonify({'success': False, 'message': 'Token is required or set generate=true'}), 400

            # 确保 system_config 存在
            if 'system_config' not in config_data:
                config_data['system_config'] = {}

            # 更新配置中的令牌
            config_data['system_config']['config_token'] = new_token

            current_app.logger.info(f"Updated config token")

            save_config()
            return jsonify({
                'success': True,
                'config_token': new_token
            })
        except Exception as e:
            current_app.logger.error(f"Failed to update config token: {e}")
            return jsonify({'success': False, 'message': str(e)}), 500

    elif request.method == 'DELETE':
        # 删除配置令牌（禁用令牌验证）
        try:
            if 'system_config' not in config_data:
                config_data['system_config'] = {}
            config_data['system_config']['config_token'] = ''
            save_config()
            current_app.logger.info("Deleted config token")
            return jsonify({'success': True})
        except Exception as e:
            current_app.logger.error(f"Failed to delete config token: {e}")
            return jsonify({'success': False, 'message': str(e)}), 500


@settings_bp.route('/version', methods=['GET'])
def get_version():
    """获取系统版本信息"""
    return jsonify(get_version_info())


@settings_bp.route('/backup/config', methods=['GET', 'POST'])
@require_auth
def backup_config():
    """获取或保存备份配置"""
    config_data = get_config()

    if request.method == 'GET':
        # 返回备份配置（密码脱敏）
        backup_data = config_data.get('backup', {})
        return jsonify({
            'webdav_url': backup_data.get('webdav_url', ''),
            'webdav_username': backup_data.get('webdav_username', ''),
            'webdav_password': '******' if backup_data.get('webdav_password') else '',
            'webdav_path': backup_data.get('webdav_path', '/config-flow-backup/'),
            'auto_backup': backup_data.get('auto_backup', False)
        })

    elif request.method == 'POST':
        # 保存备份配置
        data = request.json
        if 'backup' not in config_data:
            config_data['backup'] = {}

        config_data['backup']['webdav_url'] = data.get('webdav_url', '')
        config_data['backup']['webdav_username'] = data.get('webdav_username', '')
        # 只在密码不是脱敏字符时更新
        if data.get('webdav_password') != '******':
            config_data['backup']['webdav_password'] = data.get('webdav_password', '')
        config_data['backup']['webdav_path'] = data.get('webdav_path', '/config-flow-backup/')
        config_data['backup']['auto_backup'] = data.get('auto_backup', False)

        save_config()
        return jsonify({'success': True, 'message': '备份配置已保存'})


@settings_bp.route('/backup/test', methods=['POST'])
@require_auth
def test_backup():
    """测试 WebDAV 连接"""
    try:
        from webdav3.client import Client

        config_data = get_config()
        data = request.json
        webdav_url = data.get('webdav_url', '').rstrip('/')
        webdav_username = data.get('webdav_username', '')
        webdav_password = data.get('webdav_password', '')
        # 如果密码是掩码，从配置中读取真实密码
        if webdav_password == '******':
            webdav_password = config_data.get('backup', {}).get('webdav_password', '')
        webdav_path = data.get('webdav_path', '/config-flow-backup/')

        if not webdav_url or not webdav_username or not webdav_password:
            return jsonify({'success': False, 'message': '请填写完整的 WebDAV 配置'}), 400

        # 创建 WebDAV 客户端
        options = {
            'webdav_hostname': webdav_url,
            'webdav_login': webdav_username,
            'webdav_password': webdav_password,
            'webdav_timeout': 30
        }

        client = Client(options)

        # 测试连接 - 检查根目录
        if not client.check('/'):
            return jsonify({'success': False, 'message': '无法连接到 WebDAV 服务器'}), 400

        # 检查或创建备份目录
        if not webdav_path.startswith('/'):
            webdav_path = '/' + webdav_path
        if not webdav_path.endswith('/'):
            webdav_path = webdav_path + '/'

        # 递归创建目录（包括所有父目录）
        if not client.check(webdav_path):
            # 分解路径，逐级创建目录
            path_parts = [p for p in webdav_path.strip('/').split('/') if p]
            current_path = '/'
            for part in path_parts:
                current_path = current_path + part + '/'
                if not client.check(current_path):
                    try:
                        client.mkdir(current_path)
                    except Exception as mkdir_error:
                        return jsonify({'success': False, 'message': f'无法创建目录 {current_path}：{str(mkdir_error)}'}), 400

        return jsonify({'success': True, 'message': 'WebDAV 连接测试成功'})

    except Exception as e:
        return jsonify({'success': False, 'message': f'连接测试失败：{str(e)}'}), 500


@settings_bp.route('/backup/now', methods=['POST'])
@require_auth
def backup_now():
    """立即备份配置到 WebDAV"""
    try:
        from webdav3.client import Client

        config_data = get_config()
        data = request.json
        webdav_url = data.get('webdav_url', '').rstrip('/')
        webdav_username = data.get('webdav_username', '')
        webdav_password = data.get('webdav_password', '')
        # 如果密码是掩码，从配置中读取真实密码
        if webdav_password == '******':
            webdav_password = config_data.get('backup', {}).get('webdav_password', '')
        webdav_path = data.get('webdav_path', '/config-flow-backup/')

        if not webdav_url or not webdav_username or not webdav_password:
            return jsonify({'success': False, 'message': '请填写完整的 WebDAV 配置'}), 400

        # 创建 WebDAV 客户端
        options = {
            'webdav_hostname': webdav_url,
            'webdav_login': webdav_username,
            'webdav_password': webdav_password,
            'webdav_timeout': 30
        }

        client = Client(options)

        # 确保备份目录存在
        if not webdav_path.startswith('/'):
            webdav_path = '/' + webdav_path
        if not webdav_path.endswith('/'):
            webdav_path = webdav_path + '/'

        # 递归创建目录（包括所有父目录）
        if not client.check(webdav_path):
            # 分解路径，逐级创建目录
            path_parts = [p for p in webdav_path.strip('/').split('/') if p]
            current_path = '/'
            for part in path_parts:
                current_path = current_path + part + '/'
                if not client.check(current_path):
                    try:
                        client.mkdir(current_path)
                    except Exception as mkdir_error:
                        return jsonify({'success': False, 'message': f'无法创建目录 {current_path}：{str(mkdir_error)}'}), 400

        # 生成备份文件名（带时间戳）
        timestamp = datetime.now().strftime('%Y%m%d_%H%M%S')
        filename = f'config_backup_{timestamp}.json'
        remote_path = webdav_path + filename

        # 创建临时文件保存配置
        with tempfile.NamedTemporaryFile(mode='w', suffix='.json', delete=False) as tmp_file:
            # 复制配置并脱敏
            import copy
            backup_data = copy.deepcopy(config_data)
            if 'backup' in backup_data and 'webdav_password' in backup_data['backup']:
                backup_data['backup']['webdav_password'] = '******'

            json.dump(backup_data, tmp_file, ensure_ascii=False, indent=2)
            tmp_file_path = tmp_file.name

        try:
            # 上传到 WebDAV
            client.upload_sync(remote_path=remote_path, local_path=tmp_file_path)
            return jsonify({'success': True, 'message': f'备份成功：{filename}'})
        finally:
            # 清理临时文件
            if os.path.exists(tmp_file_path):
                os.remove(tmp_file_path)

    except Exception as e:
        return jsonify({'success': False, 'message': f'备份失败：{str(e)}。请检查父级目录是否存在。'}), 500


@settings_bp.route('/settings/subscription-aggregation', methods=['GET', 'POST'])
@require_auth
def handle_subscription_aggregation():
    """订阅聚合开关管理"""
    config_data = get_config()

    if request.method == 'GET':
        # 获取当前订阅聚合开关状态
        enabled = config_data.get('system_config', {}).get('subscription_aggregation_enabled', False)
        return jsonify({'enabled': enabled})

    elif request.method == 'POST':
        # 更新订阅聚合开关状态
        try:
            data = request.json
            enabled = data.get('enabled', False)

            # 确保 system_config 存在
            if 'system_config' not in config_data:
                config_data['system_config'] = {}

            # 更新配置中的订阅聚合开关
            config_data['system_config']['subscription_aggregation_enabled'] = enabled

            current_app.logger.info(f"Updated subscription aggregation enabled to {enabled}")

            save_config()
            return jsonify({
                'success': True,
                'enabled': enabled
            })
        except Exception as e:
            current_app.logger.error(f"Failed to update subscription aggregation: {e}")
            return jsonify({'success': False, 'message': str(e)}), 500
