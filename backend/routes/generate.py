"""配置生成路由"""
import logging
from flask import request, jsonify, send_file

from backend.routes import generate_bp
from backend.common.auth import require_auth
from backend.common.config import get_config, get_repository
from backend.utils.strategy_references import StrategyReferenceError
from backend.utils.mosdns_archive import MosdnsArchiveError, build_mosdns_zip
from backend.utils.url_utils import safe_exception_details
from backend.converters.mihomo import generate_mihomo_config
from backend.converters.surge import generate_surge_config
from backend.converters.loon import generate_loon_config
from backend.converters.mosdns import (
    generate_mosdns_config,
    get_mosdns_ruleset_downloads,
    get_mosdns_custom_files,
)


logger = logging.getLogger(__name__)


@generate_bp.route('/mihomo', methods=['POST'])
@require_auth
def generate_mihomo():
    """生成 Mihomo 配置"""
    try:
        config_data = get_config()
        # 获取前端传递的 base_url（协议 + 主机 + 端口）
        data = request.get_json() or {}
        base_url = data.get('base_url', '')

        # 传递 base_url 给生成器
        yaml_content = generate_mihomo_config(config_data, base_url=base_url)

        # 保存到数据目录
        output_file = get_repository().write_generated(
            config_data['profile_id'], 'config.yaml', yaml_content
        )

        return send_file(output_file, as_attachment=True, download_name='mihomo.yaml')
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@generate_bp.route('/surge', methods=['POST'])
@require_auth
def generate_surge():
    """生成 Surge 配置"""
    try:
        config_data = get_config()
        # 获取前端传递的 base_url（协议 + 主机 + 端口）
        data = request.get_json() or {}
        base_url = data.get('base_url', '')

        # 传递 base_url 给生成器
        config_content = generate_surge_config(config_data, base_url=base_url)

        # 保存到数据目录
        output_file = get_repository().write_generated(
            config_data['profile_id'], 'config.conf', config_content
        )

        return send_file(output_file, as_attachment=True, download_name='surge.conf')
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@generate_bp.route('/loon', methods=['POST'])
@require_auth
def generate_loon():
    """生成 Loon 配置"""
    try:
        config_data = get_config()
        data = request.get_json() or {}
        base_url = data.get('base_url', '')

        config_content = generate_loon_config(config_data, base_url=base_url)

        output_file = get_repository().write_generated(
            config_data['profile_id'], 'loon.lcf', config_content
        )

        return send_file(output_file, as_attachment=True, download_name='loon.lcf')
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@generate_bp.route('/mosdns', methods=['POST'])
@require_auth
def generate_mosdns():
    """生成 MosDNS 配置"""
    try:
        config_data = get_config()
        # 获取前端传递的 base_url（协议 + 主机 + 端口）
        data = request.get_json() or {}
        base_url = (data.get('base_url', '') or '').strip()
        if not base_url:
            scheme = request.headers.get('X-Forwarded-Proto', request.scheme)
            host = request.headers.get('X-Forwarded-Host', request.host)
            base_url = f"{scheme}://{host}"

        # 传递 base_url 给生成器
        yaml_content = generate_mosdns_config(config_data, base_url=base_url)

        # 准备规则及自定义文件
        ruleset_downloads = get_mosdns_ruleset_downloads(config_data, base_url=base_url)
        custom_files = get_mosdns_custom_files(config_data)

        zip_buffer = build_mosdns_zip(
            config_data, yaml_content, ruleset_downloads, custom_files, base_url=base_url,
        )
        return send_file(zip_buffer, as_attachment=True, download_name='mosdns-config.zip',
                         mimetype='application/zip')
    except MosdnsArchiveError as e:
        logger.warning('MosDNS ZIP 生成失败：%s', e)
        return jsonify({'success': False, 'message': str(e)}), 500
    except Exception as e:
        detail = safe_exception_details(e)
        logger.error('MosDNS ZIP 生成失败：%s', detail)
        return jsonify({'success': False, 'message': f'生成 MosDNS 配置失败：{detail}'}), 500


@generate_bp.route('/mihomo/preview', methods=['POST'])
@require_auth
def preview_mihomo():
    """预览 Mihomo 配置"""
    try:
        config_data = get_config()
        # 获取前端传递的 base_url（协议 + 主机 + 端口）
        data = request.get_json() or {}
        base_url = data.get('base_url', '')

        # 传递 base_url 给生成器
        yaml_content = generate_mihomo_config(config_data, base_url=base_url)
        return jsonify({'content': yaml_content})
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@generate_bp.route('/surge/preview', methods=['POST'])
@require_auth
def preview_surge():
    """预览 Surge 配置"""
    try:
        config_data = get_config()
        # 获取前端传递的 base_url（协议 + 主机 + 端口）
        data = request.get_json() or {}
        base_url = data.get('base_url', '')

        # 传递 base_url 给生成器
        config_content = generate_surge_config(config_data, base_url=base_url)
        return jsonify({'content': config_content})
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@generate_bp.route('/loon/preview', methods=['POST'])
@require_auth
def preview_loon():
    """预览 Loon 配置"""
    try:
        config_data = get_config()
        data = request.get_json() or {}
        base_url = data.get('base_url', '')

        config_content = generate_loon_config(config_data, base_url=base_url)
        return jsonify({'content': config_content})
    except StrategyReferenceError as e:
        return jsonify({'success': False, 'message': str(e)}), 400
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500


@generate_bp.route('/mosdns/preview', methods=['POST'])
@require_auth
def preview_mosdns():
    """预览 MosDNS 配置"""
    try:
        config_data = get_config()
        # 获取前端传递的 base_url（协议 + 主机 + 端口）
        data = request.get_json() or {}
        base_url = data.get('base_url', '')

        # 传递 base_url 给生成器
        yaml_content = generate_mosdns_config(config_data, base_url=base_url)
        return jsonify({'content': yaml_content})
    except Exception as e:
        return jsonify({'success': False, 'message': str(e)}), 500
