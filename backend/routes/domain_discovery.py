"""域名发现路由：查看 Agent 发现的域名、主动探测，并写入默认规则集。业务逻辑在 domain_discovery_service。"""

from flask import Blueprint, jsonify, request

from backend.common.auth import require_auth
from backend.common.profile_context import resolve_profile_id
from backend.utils import domain_discovery_service as service
from backend.utils.domain_discovery import VIEWS

domain_discovery_bp = Blueprint('domain_discovery', __name__, url_prefix='/api/domain-discovery')


def _payload():
    return request.get_json(silent=True) or {}


def _busy_response(busy, profile_id):
    """同一时间只运行一个任务（跨配置空间）。别的配置空间的任务不回传，避免前端去轮询看不到的任务。"""
    job = service.get_job(busy.job_id)
    if job is None or job['profile_id'] != profile_id:
        return jsonify({'success': False, 'message': '另一个配置空间的探测任务正在运行，请稍后再试', 'job': None}), 409
    return jsonify({'success': False, 'message': str(busy), 'job': service.public_job(job)}), 409


@domain_discovery_bp.route('/settings', methods=['GET'])
@require_auth
def get_settings():
    return jsonify({'success': True, **service.settings_payload(resolve_profile_id())})


@domain_discovery_bp.route('/settings', methods=['PUT'])
@require_auth
def update_settings():
    return jsonify({'success': True, **service.update_settings(resolve_profile_id(), request.get_json(silent=True))})


@domain_discovery_bp.route('/rulesets/init', methods=['POST'])
@require_auth
def init_rulesets():
    """一键创建默认的直连 / 代理规则集，并引用到兜底规则之前。"""
    payload = _payload()
    result = service.init_rulesets(resolve_profile_id(), payload.get('targets'), payload.get('proxy_policy'))
    return jsonify({'success': True, **result})


@domain_discovery_bp.route('/domains', methods=['GET'])
@require_auth
def list_domains():
    try:
        days = int(request.args.get('days', 7))
    except ValueError:
        days = 7
    view = request.args.get('view', 'uncovered')
    result = service.load_domains(
        resolve_profile_id(),
        days=1 if days <= 1 else 7,
        view=view if view in VIEWS else 'uncovered',
        agent_id=request.args.get('agent_id') or None,
    )
    return jsonify({'success': True, **result})


@domain_discovery_bp.route('/apply', methods=['POST'])
@require_auth
def apply_rules():
    """把域名追加进默认直连 / 代理规则集。"""
    profile_id = resolve_profile_id()
    entries = service.parse_apply_items(_payload().get('items'))
    # 手动采纳时，把当时的探测建议一起记进历史
    evidence = service.probe_evidence(profile_id, [entry['value'] for entry in entries])
    result = service.apply_domains(profile_id, entries, source='manual', evidence=evidence)
    return jsonify({'success': True, **result})


@domain_discovery_bp.route('/undo', methods=['POST'])
@require_auth
def undo():
    payload = _payload()
    result = service.undo_domain(resolve_profile_id(), payload.get('value'), payload.get('target'))
    return jsonify({'success': True, **result})


@domain_discovery_bp.route('/history', methods=['GET'])
@require_auth
def history():
    return jsonify({'success': True, 'items': service.history_payload(resolve_profile_id())})


@domain_discovery_bp.route('/ignore', methods=['POST'])
@require_auth
def add_ignored():
    ignored = service.update_ignored(resolve_profile_id(), _payload().get('values'), add=True)
    return jsonify({'success': True, 'ignored': ignored})


@domain_discovery_bp.route('/ignore', methods=['DELETE'])
@require_auth
def remove_ignored():
    ignored = service.update_ignored(resolve_profile_id(), _payload().get('values'), add=False)
    return jsonify({'success': True, 'ignored': ignored})


@domain_discovery_bp.route('/probe', methods=['POST'])
@require_auth
def start_probe():
    profile_id = resolve_profile_id()
    try:
        job = service.start_probe_job(profile_id, _payload().get('domains'))
    except service.ProbeBusy as busy:
        return _busy_response(busy, profile_id)
    return jsonify({'success': True, 'job': service.public_job(job)}), 202


@domain_discovery_bp.route('/probe/<job_id>', methods=['GET'])
@require_auth
def get_probe(job_id):
    job = service.get_job(job_id)
    if job is None or job['profile_id'] != resolve_profile_id():
        return jsonify({'success': False, 'message': 'Probe job not found'}), 404
    return jsonify({'success': True, 'job': service.public_job(job)})


@domain_discovery_bp.route('/region', methods=['GET'])
@require_auth
def region_status():
    return jsonify({'success': True, **service.region_payload(resolve_profile_id())})


@domain_discovery_bp.route('/region/check', methods=['POST'])
@require_auth
def start_region_check():
    payload = _payload()
    profile_id = resolve_profile_id()
    try:
        job = service.start_region_job(profile_id, services=bool(payload.get('services', True)),
                                       domains=payload.get('domains') or None)
    except service.ProbeBusy as busy:
        return jsonify({'success': False, 'message': str(busy), 'job': service.public_job(service.get_job(busy.job_id))}), 409
    return jsonify({'success': True, 'job': service.public_job(job)}), 202


@domain_discovery_bp.route('/region/route', methods=['POST'])
@require_auth
def route_region():
    payload = _payload()
    result = service.route_to_policy(resolve_profile_id(), payload.get('kind'), payload.get('value'), payload.get('policy'))
    return jsonify({'success': True, **result})
