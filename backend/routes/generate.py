"""配置生成路由"""
import logging
from flask import g, request, jsonify, send_file

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


def _subscription_hosts_report(config_data, report):
    hosts = report.get('subscription_hosts')
    dns = (hosts or {}).get('dns')
    if dns and dns.get('applied'):
        from backend.utils.proxy_server_dns import outdated_agents
        try:
            dns['outdated_agents'] = outdated_agents(config_data.get('profile_id'))
        except Exception:
            logger.debug('读取 Agent 上报的 Mihomo 版本失败', exc_info=True)
    return hosts


@generate_bp.route('/<service>/artifact', methods=['POST'])
@require_auth
def generate_artifact(service):
    """Manual export also previews/downloads one materialized artifact."""
    import hashlib
    import io
    import uuid
    import zipfile
    from backend.agents import generated_preview as previews
    from backend.agents.deployment_bundle import _pack
    generators = {'mihomo': generate_mihomo, 'surge': generate_surge, 'loon': generate_loon,
                  'mosdns': generate_mosdns, 'oxidns': generate_oxidns}
    if service not in generators:
        return jsonify(success=False, message='不支持的配置类型。'), 400
    response = generators[service]()
    if isinstance(response, tuple) or response.status_code != 200:
        return response
    response.direct_passthrough = False
    try:
        value = response.get_data()
    finally:
        response.close()
    main = {'surge': 'surge.conf', 'loon': 'loon.lcf'}.get(service, 'config.yaml')
    if service in ('mosdns', 'oxidns'):
        with zipfile.ZipFile(io.BytesIO(value)) as archive:
            files = {name: (archive.read(name), 'config' if name == main else 'custom')
                     for name in archive.namelist() if not name.endswith('/')}
    else:
        files = {main: (value, 'config')}
    config = get_config()
    repository = get_repository()
    manifest = {'protocol_version': 1, 'agent_id': 'manual', 'deployment_id': uuid.uuid4().hex,
                'service_type': service, 'profile_id': config['profile_id'], 'config_path': main,
                'files': [{'path': path, 'role': role, 'size': len(body), 'sha256': hashlib.sha256(body).hexdigest()}
                          for path, (body, role) in files.items()]}
    bundle = _pack(manifest, files, hashlib.sha256(files[main][0]).hexdigest()[:8])
    record = previews.save(repository, {'id': 'manual', 'service_type': service}, config,
        repository.config_revision(config['profile_id']), '',
        {'bundle': bundle, 'config_content': files[main][0].decode('utf-8')}, members=[])
    result = jsonify(success=True, **previews.public(record), subscription_hosts=getattr(g, 'generated_subscription_hosts', None))
    result.headers['Cache-Control'] = 'no-store'
    return result


@generate_bp.route('/artifacts/<preview_id>/<action>', methods=['GET'])
@require_auth
def read_artifact(preview_id, action):
    from backend.routes.agents import download_final_agent_config, read_final_agent_file
    if action == 'download':
        return download_final_agent_config('manual', preview_id)
    if action == 'file':
        return read_final_agent_file('manual', preview_id)
    return jsonify(success=False, message='未知操作。'), 404


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
        report = {}
        yaml_content = generate_mihomo_config(config_data, base_url=base_url, report=report)
        g.generated_subscription_hosts = _subscription_hosts_report(config_data, report)

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
        report = {}
        yaml_content = generate_mihomo_config(config_data, base_url=base_url, report=report)
        hosts_report = _subscription_hosts_report(config_data, report)
        return jsonify({'content': yaml_content, 'subscription_hosts': hosts_report})
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


@generate_bp.route('/oxidns', methods=['POST'])
@require_auth
def generate_oxidns():
    """Download a complete OxiDNS configuration and all managed rule files."""
    from backend.converters.oxidns import (generate_oxidns_config,
        get_oxidns_ruleset_downloads, get_oxidns_custom_files)
    from backend.common.oxidns_settings import OxidnsSettingsError
    from backend.utils.oxidns_archive import build_oxidns_zip
    try:
        config = get_config()
        data = request.get_json(silent=True) or {}
        base_url = (data.get('base_url') or request.host_url).rstrip('/')
        content = generate_oxidns_config(config, base_url=base_url)
        archive = build_oxidns_zip(config, content,
            get_oxidns_ruleset_downloads(config, base_url=base_url),
            get_oxidns_custom_files(config), base_url=base_url)
        return send_file(archive, as_attachment=True, download_name='oxidns-config.zip',
                         mimetype='application/zip')
    except (OxidnsSettingsError, ValueError) as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    except Exception as exc:
        detail = safe_exception_details(exc)
        logger.warning('OxiDNS ZIP generation failed: %s', detail)
        return jsonify({'success': False, 'message': '生成 OxiDNS 配置失败：' + detail}), 500


@generate_bp.route('/oxidns/preview', methods=['POST'])
@require_auth
def preview_oxidns():
    from backend.converters.oxidns import generate_oxidns_config
    try:
        import yaml
        from backend.common.config_export import sanitize_external_payload
        data = request.get_json(silent=True) or {}
        config = get_config()
        content = generate_oxidns_config(config, base_url=data.get('base_url', ''))
        # Native rule updates contain private conversion capabilities. Scrub the
        # parsed YAML so one protected URL does not erase the entire preview.
        document = sanitize_external_payload(yaml.safe_load(content), config.get('system_config'))
        return jsonify({'content': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)})
    except ValueError as exc:
        return jsonify({'success': False, 'message': str(exc)}), 400
    except Exception as exc:
        return jsonify({'success': False, 'message': safe_exception_details(exc)}), 500
