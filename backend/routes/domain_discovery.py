"""域名发现路由：查看 Agent 发现的未覆盖 / 直连失败域名，并写入默认规则集。"""

import hashlib
import uuid

from flask import Blueprint, jsonify, request

from backend.agents.traffic_store import get_traffic_store
from backend.agents.version import compare_versions
from backend.common.agent_manager import get_agent_manager
from backend.common.auth import require_auth
from backend.common.config import get_config, update_config_transaction, update_shared_config_transaction
from backend.common.config_repository import ProfileValidationError
from backend.common.profile_context import resolve_profile_id
from backend.utils.domain_discovery import (
    APPLY_RULE_TYPES, TARGETS, VIEWS, format_ruleset_line, summarize,
)
from backend.utils.rule_matcher import RuleConfigMatcher, is_valid_domain, is_valid_ip, match_domain, parse_rule_line
from backend.utils.rule_utils import save_rule_to_local

domain_discovery_bp = Blueprint('domain_discovery', __name__, url_prefix='/api/domain-discovery')

# 可以作为写入目标的规则集：内容型，且格式能表达域名规则
WRITABLE_BEHAVIORS = ('classical', 'domain')
DEFAULT_RULESET_NAMES = {'direct': '域名发现-直连', 'proxy': '域名发现-代理'}
# 支持域名发现上报的最低 Agent 版本
MIN_AGENT_VERSION = '1.4.0-go'
MAX_APPLY_ITEMS = 500
MAX_IGNORED = 2000


def _settings(config):
    raw = config.get('domain_discovery') or {}
    return {
        'direct': raw.get('direct_ruleset') or None,
        'proxy': raw.get('proxy_ruleset') or None,
        'ignored': [value for value in raw.get('ignored', []) if isinstance(value, str)],
    }


def _stored_settings(settings):
    return {
        'direct_ruleset': settings['direct'],
        'proxy_ruleset': settings['proxy'],
        'ignored': settings['ignored'],
    }


def _ruleset_problem(library_item):
    if library_item is None:
        return 'missing'
    if library_item.get('source_type', 'url') != 'content':
        return 'not_content'
    if library_item.get('behavior', 'classical') not in WRITABLE_BEHAVIORS:
        return 'unsupported_behavior'
    return None


def _references(config, library_id):
    """当前配置空间里引用该规则集的条目，以及它相对兜底规则的位置。"""
    result = []
    match_index = None
    for index, item in enumerate(config.get('rule_configs', [])):
        if item.get('itemType') == 'rule' and item.get('rule_type') == 'MATCH' and item.get('enabled', True):
            match_index = index if match_index is None else match_index
        if item.get('itemType') == 'ruleset' and item.get('library_rule_id') == library_id:
            result.append({'id': item.get('id'), 'policy': item.get('policy'),
                           'enabled': item.get('enabled', True), 'index': index})
    for reference in result:
        reference['after_match'] = match_index is not None and reference['index'] > match_index
    return result


def _ruleset_info(config, library_id):
    if not library_id:
        return None
    library_item = next((item for item in config.get('rule_library', []) if item.get('id') == library_id), None)
    references = _references(config, library_id)
    return {
        'id': library_id,
        'name': library_item.get('name') if library_item else None,
        'behavior': library_item.get('behavior', 'classical') if library_item else None,
        'problem': _ruleset_problem(library_item),
        'references': references,
        'active': any(reference['enabled'] and not reference['after_match'] for reference in references),
    }


def _policy_names(config):
    return [group.get('name') for group in config.get('proxy_groups', []) if group.get('name')]


def _profile_agents(profile_id):
    return [agent for agent in get_agent_manager().get_all_agents()
            if agent.get('profile_id', 'default') == profile_id and agent.get('service_type') == 'mihomo']


def discovery_provider_revisions(profile_id):
    """默认规则集的内容摘要，Agent 发现变化后让 mihomo 立即刷新对应的 rule-provider。"""
    config = get_config(profile_id)
    settings = _settings(config)
    library = {item.get('id'): item for item in config.get('rule_library', [])}
    result = {}
    for target in TARGETS:
        info = _ruleset_info(config, settings[target])
        if not info or info['problem'] or not info['active']:
            continue
        content = library[info['id']].get('content', '') or ''
        result[info['name']] = hashlib.md5(content.encode('utf-8')).hexdigest()
    return result


def _agent_supported(version):
    try:
        return bool(version) and compare_versions(version, MIN_AGENT_VERSION) >= 0
    except (TypeError, ValueError):
        return False


@domain_discovery_bp.route('/settings', methods=['GET'])
@require_auth
def get_settings():
    config = get_config()
    settings = _settings(config)
    candidates = [
        {'id': item.get('id'), 'name': item.get('name'), 'behavior': item.get('behavior', 'classical')}
        for item in config.get('rule_library', [])
        if _ruleset_problem(item) is None
    ]
    return jsonify({
        'success': True,
        'direct': _ruleset_info(config, settings['direct']),
        'proxy': _ruleset_info(config, settings['proxy']),
        'ignored': settings['ignored'],
        'candidates': candidates,
        'policies': _policy_names(config),
    })


@domain_discovery_bp.route('/settings', methods=['PUT'])
@require_auth
def update_settings():
    payload = request.get_json(silent=True)
    if not isinstance(payload, dict):
        raise ProfileValidationError('请求必须是 JSON 对象')
    config = get_config()
    library = {item.get('id'): item for item in config.get('rule_library', [])}
    changes = {}
    for target in TARGETS:
        key = f'{target}_ruleset'
        if key not in payload:
            continue
        value = payload[key] or None
        if value is not None and _ruleset_problem(library.get(value)) is not None:
            raise ProfileValidationError('默认规则集必须是内容型、classical 或 domain 格式的规则库条目')
        changes[target] = value

    def mutate(profile):
        settings = _settings(profile)
        settings.update(changes)
        profile['domain_discovery'] = _stored_settings(settings)

    update_config_transaction(mutate)
    return get_settings()


def _unique_name(existing, base):
    name, counter = base, 2
    while name in existing:
        name, counter = f'{base}-{counter}', counter + 1
    return name


@domain_discovery_bp.route('/rulesets/init', methods=['POST'])
@require_auth
def init_rulesets():
    """一键创建默认的直连 / 代理规则集，并引用到兜底规则之前。"""
    payload = request.get_json(silent=True) or {}
    targets = payload.get('targets') or list(TARGETS)
    if not isinstance(targets, list) or not targets or any(target not in TARGETS for target in targets):
        raise ProfileValidationError('targets 只能包含 direct / proxy')
    config = get_config()
    proxy_policy = payload.get('proxy_policy')
    if 'proxy' in targets and proxy_policy not in _policy_names(config):
        raise ProfileValidationError('请选择代理规则集使用的策略组')
    policies = {'direct': 'DIRECT', 'proxy': proxy_policy}

    created = {}

    def create_library(shared):
        library = shared.setdefault('rule_library', [])
        names = {item.get('name') for item in library}
        for target in targets:
            item = {
                'id': f'lib_{uuid.uuid4().hex}',
                'name': _unique_name(names, DEFAULT_RULESET_NAMES[target]),
                'source_type': 'content',
                'content': '',
                'behavior': 'classical',
                'enabled': True,
            }
            names.add(item['name'])
            library.append(item)
            created[target] = item

    update_shared_config_transaction(create_library)
    for item in created.values():
        save_rule_to_local(item)

    def reference(profile):
        rules = profile.setdefault('rule_configs', [])
        position = next((index for index, item in enumerate(rules)
                         if item.get('itemType') == 'rule' and item.get('rule_type') == 'MATCH'), len(rules))
        new_items = [{
            'id': f'ruleset_{uuid.uuid4()}',
            'itemType': 'ruleset',
            'library_rule_id': created[target]['id'],
            'policy': policies[target],
            'enabled': True,
            'no_resolve': False,
        } for target in targets]
        rules[position:position] = new_items
        settings = _settings(profile)
        settings.update({target: created[target]['id'] for target in targets})
        profile['domain_discovery'] = _stored_settings(settings)

    update_config_transaction(reference)
    return get_settings()


@domain_discovery_bp.route('/domains', methods=['GET'])
@require_auth
def list_domains():
    from backend.routes.rules import get_ruleset_content

    try:
        days = int(request.args.get('days', 7))
    except ValueError:
        days = 7
    days = 1 if days <= 1 else 7
    view = request.args.get('view', 'uncovered')
    if view not in VIEWS:
        view = 'uncovered'
    profile_id = resolve_profile_id()
    config = get_config()
    store = get_traffic_store()
    reports = [(agent, store.load(agent['id'])) for agent in _profile_agents(profile_id)]
    matcher = RuleConfigMatcher(
        config.get('rule_configs', []), config.get('rule_library', []),
        lambda item, library_rule: get_ruleset_content(item, library_rule, allow_network=False),
    )
    result = summarize(reports, days=days, ignored=_settings(config)['ignored'], match_rule=matcher.match,
                       view=view, agent_id=request.args.get('agent_id') or None)
    for agent in result['agents']:
        source = next(item for item in reports if item[0]['id'] == agent['id'])[0]
        agent['enabled'] = bool(source.get('domain_discovery_enabled'))
        agent['version'] = source.get('version', '')
        agent['supported'] = _agent_supported(agent['version'])
    result['min_agent_version'] = MIN_AGENT_VERSION
    return jsonify({'success': True, **result})


def _clean_domain(value):
    if not isinstance(value, str):
        return None
    value = value.strip().lower().rstrip('.')
    if not value or is_valid_ip(value) or not is_valid_domain(value) or '.' not in value:
        return None
    return value


@domain_discovery_bp.route('/apply', methods=['POST'])
@require_auth
def apply_rules():
    """把域名追加进默认直连 / 代理规则集。"""
    payload = request.get_json(silent=True) or {}
    items = payload.get('items')
    if not isinstance(items, list) or not items or len(items) > MAX_APPLY_ITEMS:
        raise ProfileValidationError(f'items 必须是 1-{MAX_APPLY_ITEMS} 条')
    requests_by_target = {target: [] for target in TARGETS}
    for item in items:
        if not isinstance(item, dict):
            raise ProfileValidationError('items 中每一项必须是对象')
        value = _clean_domain(item.get('value'))
        rule_type = item.get('rule_type', 'DOMAIN-SUFFIX')
        target = item.get('target')
        if value is None or rule_type not in APPLY_RULE_TYPES or target not in TARGETS:
            raise ProfileValidationError('每一项需要有效域名、DOMAIN-SUFFIX/DOMAIN 类型和 direct/proxy 目标')
        requests_by_target[target].append((rule_type, value))

    config = get_config()
    settings = _settings(config)
    infos = {}
    for target, entries in requests_by_target.items():
        if not entries:
            continue
        info = _ruleset_info(config, settings[target])
        if info is None or info['problem']:
            label = '直连' if target == 'direct' else '代理'
            raise ProfileValidationError(f'还没有设置可写入的默认{label}规则集')
        infos[target] = info

    added, skipped, updated_items = [], [], []

    def append_lines(shared):
        library = {item.get('id'): item for item in shared.get('rule_library', [])}
        for target, info in infos.items():
            item = library.get(info['id'])
            if _ruleset_problem(item) is not None:
                raise ProfileValidationError('默认规则集已被修改，请刷新后重试')
            content = item.get('content', '') or ''
            existing = [parsed for parsed in (parse_rule_line(line) for line in content.splitlines()) if parsed]
            new_lines = []
            for rule_type, value in requests_by_target[target]:
                covered = any(
                    (existing_type, existing_value.lower()) == (rule_type, value)
                    or (existing_type == 'DOMAIN-SUFFIX' and match_domain(value, existing_type, existing_value))
                    for existing_type, existing_value in existing
                )
                if covered:
                    skipped.append({'value': value, 'target': target, 'reason': 'exists'})
                    continue
                existing.append((rule_type, value))
                new_lines.append(format_ruleset_line(item.get('behavior', 'classical'), rule_type, value))
                added.append({'value': value, 'rule_type': rule_type, 'target': target, 'ruleset': item.get('name')})
            if new_lines:
                prefix = content if not content or content.endswith('\n') else content + '\n'
                item['content'] = prefix + '\n'.join(new_lines) + '\n'
                updated_items.append(item)

    update_shared_config_transaction(append_lines)
    for item in updated_items:
        save_rule_to_local(item)

    warnings = [
        f'规则集「{info["name"]}」没有在当前配置中启用（或位于兜底规则之后），写入的域名不会生效'
        for info in infos.values() if not info['active']
    ]
    return jsonify({'success': True, 'added': added, 'skipped': skipped, 'warnings': warnings})


def _update_ignored(values, add):
    cleaned = [_clean_domain(value) for value in values] if isinstance(values, list) else [None]
    if not cleaned or None in cleaned:
        raise ProfileValidationError('values 必须是有效域名列表')

    def mutate(profile):
        settings = _settings(profile)
        current = settings['ignored']
        if add:
            current.extend(value for value in cleaned if value not in current)
            if len(current) > MAX_IGNORED:
                raise ProfileValidationError(f'忽略列表最多 {MAX_IGNORED} 条')
        else:
            current[:] = [value for value in current if value not in cleaned]
        profile['domain_discovery'] = _stored_settings(settings)

    update_config_transaction(mutate)
    return jsonify({'success': True, 'ignored': _settings(get_config())['ignored']})


@domain_discovery_bp.route('/ignore', methods=['POST'])
@require_auth
def add_ignored():
    return _update_ignored((request.get_json(silent=True) or {}).get('values'), add=True)


@domain_discovery_bp.route('/ignore', methods=['DELETE'])
@require_auth
def remove_ignored():
    return _update_ignored((request.get_json(silent=True) or {}).get('values'), add=False)
