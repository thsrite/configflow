"""Profile management and explicit profile URL aliases."""

from flask import jsonify, request

from backend.common.auth import require_auth
from backend.common.config import get_repository
from backend.common.config_export import sanitize_config_for_output
from backend.routes import profiles_bp
from backend.utils.logger import client_ip, get_logger, log_text

logger = get_logger(__name__)


@profiles_bp.route('/<profile_id>/generate/<service>/artifact', methods=['POST'])
@require_auth
def profile_generate_artifact(profile_id, service):
    from backend.routes.generate import generate_artifact
    return generate_artifact(service)


def _profile_label(profile):
    return f"{log_text(profile.get('name') or profile.get('id'))}（{log_text(profile.get('id'), 64)}）"


@profiles_bp.route('', methods=['GET', 'POST'])
@require_auth
def handle_profiles():
    repository = get_repository()
    if request.method == 'GET':
        return jsonify(repository.list_profiles())

    data = request.get_json(silent=True) or {}
    clone_from = data.pop('clone_from', None)
    profile = repository.create_profile(data, clone_from=clone_from)
    logger.info('已创建配置空间 %s%s', _profile_label(profile),
                f'，复制自 {log_text(clone_from, 64)}' if clone_from else '')
    return jsonify(profile), 201


@profiles_bp.route('/import-client', methods=['POST'])
@require_auth
def import_client_profile():
    """导入 Mihomo / Surge / Loon / Shadowrocket 配置文件为新的配置空间。

    请求体：{client_type, content, name, id?, description?, default_subscription_ids?, dry_run?}
    dry_run 为 true 时只返回解析结果，不写入。
    """
    from backend.utils.config_importer import ConfigImportError, build_import, parse_client_config

    data = request.get_json(silent=True)
    if not isinstance(data, dict):
        return jsonify({'success': False, 'message': '请求数据必须是 JSON 对象'}), 400
    name = data.get('name')
    if not isinstance(name, str) or not name.strip():
        return jsonify({'success': False, 'message': '请填写配置名称'}), 400
    default_subscription_ids = data.get('default_subscription_ids') or []
    if not isinstance(default_subscription_ids, list):
        return jsonify({'success': False, 'message': 'default_subscription_ids 必须是数组'}), 400
    try:
        plan = parse_client_config(data.get('client_type'), data.get('content'))
    except ConfigImportError as error:
        return jsonify({'success': False, 'message': str(error)}), 400

    def build(shared):
        return build_import(plan, shared, default_subscription_ids)

    repository = get_repository()
    if data.get('dry_run'):
        result = build(repository.get_shared())
        return jsonify({'success': True, 'summary': result['summary'], 'warnings': result['warnings']})
    result = repository.import_client_profile(
        {'id': data.get('id'), 'name': name, 'description': data.get('description') or ''}, build)
    logger.info('已从 %s 客户端配置导入配置空间 %s', log_text(data.get('client_type'), 32), _profile_label(result['profile']))
    return jsonify({'success': True, 'profile': result['profile'], 'summary': result['summary'],
                    'warnings': result['warnings']}), 201


@profiles_bp.route('/<profile_id>', methods=['GET', 'PUT', 'DELETE'])
@require_auth
def handle_profile(profile_id):
    repository = get_repository()
    if request.method == 'GET':
        return jsonify(repository._profile_metadata(profile_id))
    if request.method == 'PUT':
        profile = repository.update_profile(profile_id, request.get_json(silent=True) or {})
        logger.info('已修改配置空间 %s', _profile_label(profile))
        return jsonify(profile)
    body = request.get_json(silent=True) or {}
    try:
        profile = repository._profile_metadata(profile_id)
    except Exception:
        profile = {'id': profile_id}
    repository.delete_profile(profile_id, request.args.get('new_default') or body.get('new_default'))
    logger.info('已删除配置空间 %s', _profile_label(profile))
    return '', 204


@profiles_bp.route('/<profile_id>/default', methods=['POST'])
@require_auth
def set_default_profile(profile_id):
    repository = get_repository()
    previous = repository.default_profile_id()
    if repository.set_default_profile(profile_id):
        logger.info('默认配置空间已从 %s 改为 %s', log_text(previous, 64),
                    _profile_label(repository._profile_metadata(profile_id)))
    return jsonify({'success': True, 'default_profile_id': profile_id})


@profiles_bp.route('/<profile_id>/clone', methods=['POST'])
@require_auth
def clone_profile(profile_id):
    data = request.get_json(silent=True) or {}
    profile = get_repository().clone_profile(profile_id, data)
    logger.info('已复制配置空间 %s → %s', log_text(profile_id, 64), _profile_label(profile))
    return jsonify(profile), 201


@profiles_bp.route('/<profile_id>/agents', methods=['GET'])
@require_auth
def profile_agent_bindings(profile_id):
    return jsonify(get_repository().get_profile_agent_bindings(profile_id))


@profiles_bp.route('/<profile_id>/agents/rebind', methods=['POST'])
@require_auth
def rebind_profile_agents(profile_id):
    from backend.common.agent_manager import get_agent_manager
    from backend.agents.manager import AgentDeploymentConflict

    data = request.get_json(silent=True)
    if not isinstance(data, dict):
        return jsonify({'success': False, 'message': '请求数据必须是 JSON 对象'}), 400
    try:
        result = get_agent_manager().rebind_profile_agents(
            profile_id, data.get('target_profile_id'), data.get('agent_ids'))
    except AgentDeploymentConflict as error:
        return jsonify({'success': False, 'message': str(error)}), 409
    logger.info('已将 %d 个 Agent 从配置空间 %s 改绑到 %s', len(result['agents']),
                log_text(profile_id, 64), log_text(data.get('target_profile_id'), 64))
    return jsonify({'success': True, **result})


@profiles_bp.route('/<profile_id>/export', methods=['GET'])
@require_auth
def export_profile(profile_id):
    response = jsonify(sanitize_config_for_output(get_repository().export_profile(profile_id)))
    logger.info('已导出配置空间 %s，来源 %s', log_text(profile_id, 64), client_ip())
    response.headers['Content-Disposition'] = f'attachment; filename={profile_id}.json'
    return response


@profiles_bp.route('/<profile_id>/import', methods=['POST'])
@require_auth
def import_profile(profile_id):
    data = request.get_json(silent=True)
    if not isinstance(data, dict):
        return jsonify({'success': False, 'message': '请求数据必须是 JSON 对象'}), 400
    get_repository().import_profile(profile_id, data)
    logger.info('已向配置空间 %s 导入配置，来源 %s', log_text(profile_id, 64), client_ip())
    return jsonify({'success': True, 'profile_id': profile_id})




@profiles_bp.route('/<profile_id>/subscriptions/<sub_id>/proxies', methods=['GET'])
def profile_subscription_proxies(profile_id, sub_id):
    from backend.routes.subscriptions import get_subscription_proxies
    return get_subscription_proxies(sub_id)




@profiles_bp.route('/<profile_id>/rules', methods=['GET', 'POST'])
@require_auth
def profile_rules(profile_id):
    from backend.routes.rules import handle_rules
    return handle_rules()


@profiles_bp.route('/<profile_id>/rules/local/<name>', methods=['GET'])
def profile_local_rule(profile_id, name):
    from backend.routes.rules import get_local_rule
    return get_local_rule(name)




@profiles_bp.route('/<profile_id>/rule-library/content/<rule_id>', methods=['GET'])
def profile_rule_library_content(profile_id, rule_id):
    from backend.routes.rule_library import get_rule_library_content
    return get_rule_library_content(rule_id)


@profiles_bp.route('/<profile_id>/proxy-groups', methods=['GET', 'POST'])
@require_auth
def profile_proxy_groups(profile_id):
    from backend.routes.proxy_groups import handle_proxy_groups
    return handle_proxy_groups()




@profiles_bp.route('/<profile_id>/aggregations/<agg_id>/provider', methods=['GET'])
def profile_aggregation_provider(profile_id, agg_id):
    from backend.routes.aggregations import get_aggregation_provider
    return get_aggregation_provider(agg_id)


@profiles_bp.route('/<profile_id>/mosdns/rule-proxy', methods=['GET'])
def profile_mosdns_rule_proxy(profile_id):
    from backend.routes.mosdns import mosdns_rule_proxy
    return mosdns_rule_proxy()


@profiles_bp.route('/<profile_id>/generate/mihomo', methods=['POST'])
@require_auth
def profile_generate_mihomo(profile_id):
    from backend.routes.generate import generate_mihomo
    return generate_mihomo()


@profiles_bp.route('/<profile_id>/generate/surge', methods=['POST'])
@require_auth
def profile_generate_surge(profile_id):
    from backend.routes.generate import generate_surge
    return generate_surge()


@profiles_bp.route('/<profile_id>/generate/loon', methods=['POST'])
@require_auth
def profile_generate_loon(profile_id):
    from backend.routes.generate import generate_loon
    return generate_loon()

@profiles_bp.route('/<profile_id>/generate/mosdns', methods=['POST'])
@require_auth
def profile_generate_mosdns(profile_id):
    from backend.routes.generate import generate_mosdns
    return generate_mosdns()


@profiles_bp.route('/<profile_id>/generate/mihomo/preview', methods=['POST'])
@require_auth
def profile_preview_mihomo(profile_id):
    from backend.routes.generate import preview_mihomo
    return preview_mihomo()


@profiles_bp.route('/<profile_id>/generate/surge/preview', methods=['POST'])
@require_auth
def profile_preview_surge(profile_id):
    from backend.routes.generate import preview_surge
    return preview_surge()


@profiles_bp.route('/<profile_id>/generate/loon/preview', methods=['POST'])
@require_auth
def profile_preview_loon(profile_id):
    from backend.routes.generate import preview_loon
    return preview_loon()

@profiles_bp.route('/<profile_id>/generate/mosdns/preview', methods=['POST'])
@require_auth
def profile_preview_mosdns(profile_id):
    from backend.routes.generate import preview_mosdns
    return preview_mosdns()


@profiles_bp.route('/<profile_id>/generate/oxidns', methods=['POST'])
@require_auth
def profile_generate_oxidns(profile_id):
    from backend.routes.generate import generate_oxidns
    return generate_oxidns()


@profiles_bp.route('/<profile_id>/generate/oxidns/preview', methods=['POST'])
@require_auth
def profile_preview_oxidns(profile_id):
    from backend.routes.generate import preview_oxidns
    return preview_oxidns()


@profiles_bp.route('/<profile_id>/oxidns/settings', methods=['GET', 'POST'])
@require_auth
def profile_oxidns_settings(profile_id):
    from backend.routes.oxidns import oxidns_settings
    return oxidns_settings()


@profiles_bp.route('/<profile_id>/oxidns/render', methods=['POST'])
@require_auth
def profile_render_oxidns_draft(profile_id):
    from backend.routes.oxidns import render_oxidns_draft
    return render_oxidns_draft()


@profiles_bp.route('/<profile_id>/oxidns/validate', methods=['POST'])
@require_auth
def profile_validate_oxidns_draft(profile_id):
    from backend.routes.oxidns import validate_oxidns_draft
    return validate_oxidns_draft()


@profiles_bp.route('/<profile_id>/oxidns/editor', methods=['POST'])
@require_auth
def profile_prepare_oxidns_editor(profile_id):
    from backend.routes.oxidns import prepare_oxidns_editor
    return prepare_oxidns_editor()


@profiles_bp.route('/<profile_id>/oxidns/template-preview', methods=['POST'])
@require_auth
def profile_preview_oxidns_template(profile_id):
    from backend.routes.oxidns import preview_oxidns_template
    return preview_oxidns_template()


@profiles_bp.route('/<profile_id>/oxidns/common-settings', methods=['POST'])
@require_auth
def profile_read_oxidns_common_settings(profile_id):
    from backend.routes.oxidns import read_oxidns_common_settings
    return read_oxidns_common_settings()


@profiles_bp.route('/<profile_id>/oxidns/common-settings/apply', methods=['POST'])
@require_auth
def profile_apply_oxidns_common_settings(profile_id):
    from backend.routes.oxidns import apply_oxidns_common_settings
    return apply_oxidns_common_settings()


@profiles_bp.route('/<profile_id>/oxidns/native-settings/apply', methods=['POST'])
@require_auth
def profile_apply_oxidns_native_settings(profile_id):
    from backend.routes.oxidns import apply_oxidns_native_settings
    return apply_oxidns_native_settings()
