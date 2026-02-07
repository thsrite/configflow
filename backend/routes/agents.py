"""Agent 管理路由模块

提供 Agent 的注册、管理、配置推送等功能
"""
import os
from flask import request, jsonify, send_file

from backend.agents.config_generator import generate_agent_config
from backend.agents.version import get_latest_version, has_update
from backend.converters.mihomo import generate_mihomo_config, get_mihomo_provider_downloads, get_mihomo_ruleset_downloads
from backend.converters.mosdns import generate_mosdns_config, get_mosdns_ruleset_downloads, get_mosdns_custom_files
from backend.converters.surge import generate_surge_config
from backend.routes import agents_bp as bp
from backend.common.auth import require_auth
from backend.common.config import config_data, save_config
from backend.common.agent_manager import get_agent_manager
from backend.utils.logger import get_logger

logger = get_logger(__name__)


@bp.route('/install-script', methods=['GET'])
def get_install_script():
    """生成 Agent 安装脚本（默认 Go 版本，可选 Shell 版本）"""
    try:
        import os
        from backend.agents.go_install_script import generate_go_agent_install_script
        from backend.agents.install_script import generate_lightweight_install_script

        # 获取参数
        agent_name = request.args.get('name', 'My Agent')
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
                binary_download_url=binary_download_url
            )

        logger.info(f"安装脚本生成成功，长度: {len(script)} 字符")
        return script, 200, {'Content-Type': 'text/plain; charset=utf-8'}

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

        # 记录请求信息以便调试
        logger.info("收到Agent注册请求")
        logger.info(f"Content-Type: {request.content_type}")

        # 获取 JSON 数据
        agent_data = request.get_json(force=True, silent=False)

        if agent_data is None:
            logger.error("Failed to parse JSON data")
            return jsonify({'success': False, 'message': 'Invalid JSON data'}), 400

        logger.info(f"Agent数据: {agent_data}")

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
            logger.info(f"Agent提供的host为Docker容器IP或为空({agent_host})，使用客户端IP: {client_ip}")
            agent_data['host'] = client_ip
        else:
            logger.info(f"保留Agent提供的host: {agent_host}")

        # 注册 Agent
        result = agent_manager.register_agent(agent_data)
        save_config()

        response_data = {'success': True, **result}
        logger.info(f"注册成功: {result.get('id')}")

        return jsonify(response_data), 200

    except Exception as e:
        import traceback
        error_detail = traceback.format_exc()
        logger.error(f"Agent注册失败: {e}")
        logger.error(f"错误详情: {error_detail}")
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/heartbeat', methods=['POST'])
def agent_heartbeat(agent_id):
    """Agent 心跳"""
    try:
        agent_manager = get_agent_manager()
        heartbeat_data = request.json or {}
        result = agent_manager.update_heartbeat(agent_id, heartbeat_data)
        if result:
            # 心跳信息是临时状态，不需要持久化到配置文件
            # 只在内存中更新即可
            return jsonify({'success': True}), 200
        else:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/config', methods=['GET'])
def get_agent_config(agent_id):
    """获取 Agent 的配置文件"""
    try:
        # 从查询参数获取 token
        token = request.args.get('token')
        if not token:
            return jsonify({'success': False, 'message': 'Token required'}), 401

        agent_manager = get_agent_manager()
        # 验证 token
        agent = agent_manager.get_agent_by_token(token)
        if not agent or agent['id'] != agent_id:
            return jsonify({'success': False, 'message': 'Invalid token'}), 401

        # 生成配置
        config_result = generate_agent_config(config_data, agent)

        return jsonify({
            'success': True,
            'content': config_result['content'],
            'md5': config_result['md5'],
            'version': config_result['version']
        }), 200

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
        agents = agent_manager.get_all_agents()
        # 为每个 agent 添加 has_update 字段
        for agent in agents:
            current_version = agent.get('version', '0.0.0')
            agent['has_update'] = has_update(current_version)
        return jsonify(agents), 200

    elif request.method == 'POST':
        # 手动添加 Agent（非 Agent 自注册）
        try:
            agent_data = request.json
            result = agent_manager.register_agent(agent_data)
            save_config()
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
            return jsonify(agent), 200
        else:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404

    elif request.method == 'PUT':
        try:
            agent_data = request.json
            result = agent_manager.update_agent(agent_id, agent_data)
            if result:
                save_config()
                return jsonify({'success': True, 'data': result}), 200
            else:
                return jsonify({'success': False, 'message': 'Agent not found'}), 404
        except Exception as e:
            return jsonify({'success': False, 'message': str(e)}), 500

    elif request.method == 'DELETE':
        try:
            result = agent_manager.delete_agent(agent_id)
            if result:
                save_config()
                return jsonify({'success': True}), 200
            else:
                return jsonify({'success': False, 'message': 'Agent not found'}), 404
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

        if result.get('success'):
            save_config()

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
            save_config()
        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/<agent_id>/update', methods=['POST'])
@require_auth
def update_agent_version(agent_id):
    """触发 Agent 更新"""
    try:
        agent_manager = get_agent_manager()
        agent = agent_manager.get_agent_by_id(agent_id)
        if not agent:
            return jsonify({'success': False, 'message': 'Agent not found'}), 404

        # 获取最新版本
        latest_version = get_latest_version()

        # 构建二进制下载 URL
        # 根据架构确定文件名（简化处理，默认使用 amd64）
        # 实际应用中可能需要 agent 报告其架构
        arch = request.json.get('arch', 'linux-amd64')
        binary_filename = f'configflow-agent-{arch}'

        # 构建完整的下载 URL
        server_url = request.host_url.rstrip('/')
        binary_url = f"{server_url}/api/agents/download/{binary_filename}"

        # 触发更新
        result = agent_manager.update_agent_version(agent_id, latest_version, binary_url)
        return jsonify(result), 200 if result.get('success') else 500
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


# Docker 相关路由（安装脚本生成）

@bp.route('/docker-mihomo-compose', methods=['GET'])
@require_auth
def get_docker_mihomo_compose():
    """生成 Mihomo Docker Compose 配置"""
    from backend.agents.install_script import generate_docker_mihomo_compose

    try:
        params = {
            'server_url': request.args.get('server_url', ''),
            'agent_token': request.args.get('agent_token', ''),
            'agent_name': request.args.get('agent_name', 'mihomo-agent'),
            'data_dir': request.args.get('data_dir', './mihomo_data')
        }

        script = generate_docker_mihomo_compose(**params)
        return script, 200, {'Content-Type': 'text/yaml; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/docker-mihomo-run', methods=['GET'])
@require_auth
def get_docker_mihomo_run():
    """生成 Mihomo Docker Run 命令"""
    from backend.agents.install_script import generate_docker_mihomo_run

    try:
        params = {
            'server_url': request.args.get('server_url', ''),
            'agent_token': request.args.get('agent_token', ''),
            'agent_name': request.args.get('agent_name', 'mihomo-agent'),
            'data_dir': request.args.get('data_dir', './mihomo_data')
        }

        script = generate_docker_mihomo_run(**params)
        return script, 200, {'Content-Type': 'text/plain; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/docker-mosdns-compose', methods=['GET'])
@require_auth
def get_docker_mosdns_compose():
    """生成 MosDNS Docker Compose 配置"""
    from backend.agents.install_script import generate_docker_mosdns_compose

    try:
        params = {
            'server_url': request.args.get('server_url', ''),
            'agent_token': request.args.get('agent_token', ''),
            'agent_name': request.args.get('agent_name', 'mosdns-agent'),
            'data_dir': request.args.get('data_dir', './mosdns_data')
        }

        script = generate_docker_mosdns_compose(**params)
        return script, 200, {'Content-Type': 'text/yaml; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/docker-mosdns-run', methods=['GET'])
@require_auth
def get_docker_mosdns_run():
    """生成 MosDNS Docker Run 命令"""
    from backend.agents.install_script import generate_docker_mosdns_run

    try:
        params = {
            'server_url': request.args.get('server_url', ''),
            'agent_token': request.args.get('agent_token', ''),
            'agent_name': request.args.get('agent_name', 'mosdns-agent'),
            'data_dir': request.args.get('data_dir', './mosdns_data')
        }

        script = generate_docker_mosdns_run(**params)
        return script, 200, {'Content-Type': 'text/plain; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/docker-aio-compose', methods=['GET'])
@require_auth
def get_docker_aio_compose():
    """生成 All-in-One Docker Compose 配置（Mihomo + MosDNS）"""
    from backend.agents.install_script import generate_docker_aio_compose

    try:
        params = {
            'server_url': request.args.get('server_url', ''),
            'agent_token': request.args.get('agent_token', ''),
            'agent_name': request.args.get('agent_name', 'aio-agent'),
            'data_dir': request.args.get('data_dir', './aio_data')
        }

        script = generate_docker_aio_compose(**params)
        return script, 200, {'Content-Type': 'text/yaml; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/docker-aio-run', methods=['GET'])
@require_auth
def get_docker_aio_run():
    """生成 All-in-One Docker Run 命令（Mihomo + MosDNS）"""
    from backend.agents.install_script import generate_docker_aio_run

    try:
        params = {
            'server_url': request.args.get('server_url', ''),
            'agent_token': request.args.get('agent_token', ''),
            'agent_name': request.args.get('agent_name', 'aio-agent'),
            'data_dir': request.args.get('data_dir', './aio_data')
        }

        script = generate_docker_aio_run(**params)
        return script, 200, {'Content-Type': 'text/plain; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/docker-compose', methods=['GET'])
@require_auth
def get_docker_compose():
    """生成通用 Docker Compose 配置（Agent 容器）"""
    try:
        # 获取参数
        name = request.args.get('name', 'configflow-agent')
        service_type = request.args.get('type', 'mihomo')
        port = request.args.get('port', '8080')
        agent_ip = request.args.get('agent_ip', '')
        docker_image = request.args.get('docker_image', '')
        container_name = request.args.get('container_name', 'configflow-agent')
        service_container_name = request.args.get('service_container_name', service_type)
        network_mode = request.args.get('network_mode', 'bridge')
        server_url = request.args.get('server_url', '')

        # 使用默认镜像如果未指定
        if not docker_image:
            docker_image = 'thsrite/config-flow-agent:latest'

        # 根据服务类型设置默认配置路径和重启命令
        if service_type == 'mihomo':
            config_path = '/etc/mihomo/config.yaml'
            restart_command = 'systemctl restart mihomo'
        elif service_type == 'mosdns':
            config_path = '/etc/mosdns/config.yaml'
            restart_command = 'systemctl restart mosdns'
        else:
            config_path = f'/etc/{service_type}/config.yaml'
            restart_command = f'systemctl restart {service_type}'

        # 生成 Docker Compose 配置
        compose_template = """version: '3.8'

services:
  agent:
    image: {docker_image}
    container_name: {container_name}
    restart: unless-stopped
    network_mode: {network_mode}
    ports:
      - "{port}:{port}"
    environment:
      - SERVER_URL={server_url}
      - AGENT_NAME={name}
      - SERVICE_TYPE={service_type}
      - CONFIG_PATH={config_path}
      - RESTART_COMMAND={restart_command}
      - SERVICE_CONTAINER_NAME={service_container_name}
      - AGENT_PORT={port}
{agent_ip_env}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
"""

        # 如果指定了 agent_ip，添加到环境变量
        agent_ip_env = f"      - AGENT_IP={agent_ip}" if agent_ip else ""

        compose_content = compose_template.format(
            docker_image=docker_image,
            container_name=container_name,
            network_mode=network_mode,
            port=port,
            server_url=server_url,
            name=name,
            service_type=service_type,
            config_path=config_path,
            restart_command=restart_command,
            service_container_name=service_container_name,
            agent_ip_env=agent_ip_env
        )

        return compose_content, 200, {'Content-Type': 'text/yaml; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@bp.route('/docker-run', methods=['GET'])
@require_auth
def get_docker_run():
    """生成通用 Docker Run 命令（Agent 容器）"""
    try:
        # 获取参数
        name = request.args.get('name', 'configflow-agent')
        service_type = request.args.get('type', 'mihomo')
        port = request.args.get('port', '8080')
        agent_ip = request.args.get('agent_ip', '')
        docker_image = request.args.get('docker_image', '')
        container_name = request.args.get('container_name', 'configflow-agent')
        service_container_name = request.args.get('service_container_name', service_type)
        network_mode = request.args.get('network_mode', 'bridge')
        server_url = request.args.get('server_url', '')

        # 使用默认镜像如果未指定
        if not docker_image:
            docker_image = 'thsrite/config-flow-agent:latest'

        # 根据服务类型设置默认配置路径和重启命令
        if service_type == 'mihomo':
            config_path = '/etc/mihomo/config.yaml'
            restart_command = 'systemctl restart mihomo'
        elif service_type == 'mosdns':
            config_path = '/etc/mosdns/config.yaml'
            restart_command = 'systemctl restart mosdns'
        else:
            config_path = f'/etc/{service_type}/config.yaml'
            restart_command = f'systemctl restart {service_type}'

        # 构建环境变量
        env_vars = [
            f'-e SERVER_URL="{server_url}"',
            f'-e AGENT_NAME="{name}"',
            f'-e SERVICE_TYPE="{service_type}"',
            f'-e CONFIG_PATH="{config_path}"',
            f'-e RESTART_COMMAND="{restart_command}"',
            f'-e SERVICE_CONTAINER_NAME="{service_container_name}"',
            f'-e AGENT_PORT="{port}"'
        ]

        if agent_ip:
            env_vars.append(f'-e AGENT_IP="{agent_ip}"')

        # 构建环境变量字符串（先构建，避免在f-string中使用反斜杠）
        backslash = ' \\'
        env_vars_str = backslash.join([f'\n  {env}' for env in env_vars])

        # 生成 Docker Run 命令
        run_command = f"""docker run -d \\
  --name {container_name} \\
  --restart unless-stopped \\
  --network {network_mode} \\
  -p {port}:{port} {env_vars_str} \\
  -v /var/run/docker.sock:/var/run/docker.sock \\
  --log-driver json-file \\
  --log-opt max-size=10m \\
  --log-opt max-file=3 \\
  {docker_image}"""

        return run_command, 200, {'Content-Type': 'text/plain; charset=utf-8'}

    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


def _prefetch_download_contents(downloads, base_url):
    """预获取所有下载项的文件内容，写入 item['content']

    Args:
        downloads: provider_downloads + ruleset_downloads 列表
        base_url: 服务器基础 URL（server_domain 或前端传递的 base_url）
    """
    if not downloads:
        return

    import requests
    from concurrent.futures import ThreadPoolExecutor, as_completed

    def fetch_one(item):
        url = item.get('url', '')
        if not url:
            return
        try:
            # Backend 自身 URL：替换为内部地址避免外部网络绕行
            fetch_url = url
            if base_url and url.startswith(base_url):
                fetch_url = url.replace(base_url, 'http://127.0.0.1:5001', 1)

            resp = requests.get(fetch_url, timeout=30)
            resp.raise_for_status()
            item['content'] = resp.text
            logger.info(f"预获取成功: {item.get('name', url)} ({len(resp.text)} 字符)")
        except Exception as e:
            logger.warning(f"预获取失败: {item.get('name', url)}, 错误: {e}, Agent 将 fallback 到 URL 下载")
            item['content'] = ''

    with ThreadPoolExecutor(max_workers=8) as executor:
        futures = {executor.submit(fetch_one, item): item for item in downloads}
        for future in as_completed(futures):
            future.result()  # 触发异常日志（已在 fetch_one 内处理）


@bp.route('/<agent_id>/push-config', methods=['POST'])
@require_auth
def push_config_to_agent(agent_id):
    """主动推送配置到 Agent"""
    try:
        logger.info(f"开始推送配置到 Agent: {agent_id}")

        agent_manager = get_agent_manager()
        agent = agent_manager.get_agent_by_id(agent_id)
        if not agent:
            logger.error(f"Agent not found: {agent_id}")
            return jsonify({'success': False, 'message': 'Agent not found'}), 404

        # 获取 base_url（优先使用前端传递的，否则从请求头构建）
        data = request.get_json() or {}
        base_url = data.get('base_url', '').strip()

        if not base_url:
            # 如果前端没有传递，则从请求头构建
            scheme = request.headers.get('X-Forwarded-Proto', request.scheme)
            host = request.headers.get('X-Forwarded-Host', request.host)
            base_url = f"{scheme}://{host}"

        logger.info(f"Agent: {agent.get('name')}, Service Type: {agent.get('service_type')}, Base URL: {base_url}")

        # 根据 service_type 生成配置
        service_type = agent.get('service_type', 'mihomo')
        provider_downloads = []  # Provider 下载信息（Mihomo 需要）
        ruleset_downloads = []  # 规则集下载信息（Mihomo 和 MosDNS 需要）
        custom_files = []  # 自定义文件列表（仅 MosDNS 需要）

        try:
            if service_type == 'mihomo':
                logger.info("生成 Mihomo 配置...")
                config_content = generate_mihomo_config(config_data, base_url=base_url)

                # 获取 provider 下载信息
                provider_downloads = get_mihomo_provider_downloads(config_data, base_url=base_url)
                logger.info(f"需要下载 {len(provider_downloads)} 个 provider 文件")

                # 获取 ruleset 下载信息
                ruleset_downloads = get_mihomo_ruleset_downloads(config_data, base_url=base_url)
                logger.info(f"需要下载 {len(ruleset_downloads)} 个 ruleset 文件")
            elif service_type == 'mosdns':
                logger.info("生成 MosDNS 配置...")
                config_content = generate_mosdns_config(config_data, base_url=base_url)

                # 获取规则集下载信息
                ruleset_downloads = get_mosdns_ruleset_downloads(config_data, base_url=base_url)
                logger.info(f"需要下载 {len(ruleset_downloads)} 个规则集文件")

                # 获取自定义文件列表（hosts 和单个规则）
                custom_files = get_mosdns_custom_files(config_data)
                logger.info(f"需要写入 {len(custom_files)} 个自定义文件")
            elif service_type == 'surge':
                logger.info("生成 Surge 配置...")
                config_content = generate_surge_config(config_data, base_url=base_url)
            else:
                logger.error(f"Unsupported service type: {service_type}")
                return jsonify({'success': False, 'message': f'Unsupported service type: {service_type}'}), 400

            logger.info(f"配置生成成功，长度: {len(config_content)} 字符")
        except Exception as gen_error:
            import traceback
            error_detail = traceback.format_exc()
            logger.error(f"生成配置失败: {gen_error}")
            logger.error(f"错误详情: {error_detail}")
            return jsonify({'success': False, 'message': f'配置生成失败: {str(gen_error)}'}), 500

        # 预获取所有文件内容，随配置一起推送给 Agent（避免 Agent 逐个下载）
        if provider_downloads or ruleset_downloads:
            server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()
            effective_base_url = server_domain or base_url
            all_downloads = provider_downloads + ruleset_downloads
            logger.info(f"预获取 {len(all_downloads)} 个文件内容...")
            _prefetch_download_contents(all_downloads, effective_base_url)
            prefetched_count = sum(1 for d in all_downloads if d.get('content'))
            logger.info(f"预获取完成: {prefetched_count}/{len(all_downloads)} 个文件成功")

        # 推送到 Agent
        logger.info(f"推送配置到 Agent: {agent.get('host')}:{agent.get('port')}")

        # 准备额外数据
        extra_data = None
        if service_type == 'mihomo':
            # Mihomo 需要下载 providers 和 rulesets
            if provider_downloads or ruleset_downloads:
                extra_data = {
                    'directories': ['providers', 'ruleset']  # Agent 需要创建 providers 和 ruleset 目录
                }
                if provider_downloads:
                    extra_data['provider_downloads'] = provider_downloads
                if ruleset_downloads:
                    extra_data['ruleset_downloads'] = ruleset_downloads

                log_parts = []
                if provider_downloads:
                    log_parts.append(f"{len(provider_downloads)} 个 provider 下载")
                if ruleset_downloads:
                    log_parts.append(f"{len(ruleset_downloads)} 个 ruleset 下载")
                log_parts.append("目录创建指令")

                logger.info(f"准备推送配置，包含 {', '.join(log_parts)}")
        elif service_type == 'mosdns':
            # MosDNS 需要下载 rulesets 和写入自定义文件
            extra_data = {
                'directories': ['rules']  # Agent 需要在配置文件同级目录创建 rules 文件夹
            }
            if ruleset_downloads:
                extra_data['ruleset_downloads'] = ruleset_downloads
            if custom_files:
                extra_data['custom_files'] = custom_files

            log_parts = []
            if ruleset_downloads:
                log_parts.append(f"{len(ruleset_downloads)} 个规则集下载")
            if custom_files:
                log_parts.append(f"{len(custom_files)} 个自定义文件")
            log_parts.append("目录创建指令")

            logger.info(f"准备推送配置，包含 {', '.join(log_parts)}")

        result = agent_manager.push_config_to_agent(agent_id, config_content, extra_data=extra_data)

        # 处理推送结果
        if result['success']:
            logger.info(f"配置推送成功: {agent_id}")
            if service_type == 'mihomo':
                # 在返回结果中包含下载信息（用于前端显示）
                if provider_downloads or ruleset_downloads:
                    result['directories'] = ['providers', 'ruleset']
                if provider_downloads:
                    result['provider_downloads'] = provider_downloads
                if ruleset_downloads:
                    result['ruleset_downloads'] = ruleset_downloads
            elif service_type == 'mosdns':
                # 在返回结果中包含规则集下载信息和目录创建信息（用于前端显示）
                result['directories'] = ['rules']
                if ruleset_downloads:
                    result['ruleset_downloads'] = ruleset_downloads
            save_config()
        else:
            logger.error(f"配置推送失败: {result.get('message')}")

        return jsonify(result), 200 if result['success'] else 500

    except Exception as e:
        import traceback
        error_detail = traceback.format_exc()
        logger.error(f"推送配置异常: {e}")
        logger.error(f"错误详情: {error_detail}")
        return jsonify({'success': False, 'message': str(e)}), 500


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
