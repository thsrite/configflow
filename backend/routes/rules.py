"""规则路由模块"""
import copy
import os
import re
import requests
from flask import g, request, jsonify, make_response
from backend.common.json_response import jsonify_external
from backend.routes import rules_bp as bp, rule_sets_bp as rule_sets_bp
from backend.common.auth import require_auth
from backend.common.config import (
    update_config_transaction,
    get_config,
    get_repository,
)
from backend.common.profile_context import resolve_profile_id
from backend.common.config_repository import ProfileRepositoryError, ProfileValidationError
from backend.utils.rule_matcher import RuleConfigMatcher, parse_rule_line, is_valid_domain, is_valid_ip, normalize_match_query
from backend.utils.reorder import reorder_by_ids
from backend.utils.logger import get_logger
from backend.utils.url_utils import safe_exception_details, safe_url_for_log
from backend.utils.rule_fetch import request_rule
from backend.utils.rule_syntax import COMMA_PAYLOAD_TYPES, rule_value_error
from backend.converters.common import UDP_FALLBACK_POLICIES

logger = get_logger(__name__)


def attach_full_url_to_rules(rules: list) -> list:
    """为规则集的相对路径 URL 拼接完整的 server_domain

    Args:
        rules: 规则列表（会被深拷贝，不影响原始数据）

    Returns:
        带有完整 URL 的规则列表副本
    """
    # 深拷贝，避免修改原始数据
    config_data = get_config()
    rules_copy = copy.deepcopy(rules)
    server_domain = config_data.get('system_config', {}).get('server_domain', '').strip()

    # 如果 server_domain 为空，不进行拼接
    if not server_domain:
        return rules_copy

    # 遍历所有规则集，拼接相对路径
    for rule in rules_copy:
        if rule.get('itemType') == 'ruleset' and 'url' in rule:
            url = rule['url']
            # 如果是相对路径（以 / 开头），则拼接 server_domain
            if url and url.startswith('/'):
                rule['url'] = f"{server_domain}{url}"

    return rules_copy


def normalize_rule_config(rule_item: dict, previous=None) -> None:
    """Persist composition only; shared library owns every source field."""
    from backend.common.config_repository import ProfileValidationError
    if not isinstance(rule_item, dict):
        raise ProfileValidationError('规则请求必须是 JSON 对象')
    rule_item.setdefault('itemType', 'rule' if 'rule_type' in rule_item else 'ruleset')
    # 「节点不支持 UDP 时」与「对时始终直连」只属于 MATCH：空值跟随各客户端默认，对时默认关闭
    fallback = rule_item.pop('udp_fallback', None) or ''
    ntp = rule_item.pop('ntp_direct', False)
    if rule_item['itemType'] != 'ruleset' and rule_item.get('rule_type') == 'MATCH':
        if fallback not in ('', *UDP_FALLBACK_POLICIES):
            raise ProfileValidationError('节点不支持 UDP 时只能选择跟随客户端默认、直连或拒绝')
        if not isinstance(ntp, bool):
            raise ProfileValidationError('对时始终直连只能是开启或关闭')
        if fallback:
            rule_item['udp_fallback'] = fallback
        if ntp:
            rule_item['ntp_direct'] = True
    if rule_item['itemType'] != 'ruleset':
        # Keep unrelated edits to legacy rules possible; validate every new or
        # changed type/value before it reaches the saved profile.
        if not previous or any(rule_item.get(key) != previous.get(key) for key in ('itemType', 'rule_type', 'value')):
            error = rule_value_error(rule_item.get('rule_type'), rule_item.get('value', ''))
            if error:
                raise ProfileValidationError(error)
        return
    library_id = rule_item.get('library_rule_id')
    if not library_id or not any(item.get('id') == library_id
                                 for item in get_config().get('rule_library', [])):
        raise ProfileValidationError('规则集必须引用存在的共享规则库条目')
    for field in ('name', 'url', 'content', 'source_type', 'behavior', 'format', 'base_url', 'library_enabled'):
        rule_item.pop(field, None)


@bp.route('', methods=['GET', 'POST'])
@require_auth
def handle_rules():
    """规则管理（包含规则和规则集）"""
    config = get_config()

    if request.method == 'GET':
        # 获取规则并拼接完整 URL（用于前端显示）
        rule_configs = config.get('rule_configs', [])
        rules_with_full_url = attach_full_url_to_rules(rule_configs)
        return jsonify(rules_with_full_url)

    elif request.method == 'POST':
        rule = request.json
        normalize_rule_config(rule)
        update_config_transaction(
            lambda profile: profile.setdefault('rule_configs', []).insert(0, rule)
        )
        return jsonify({'success': True, 'data': rule})


_MISSING = object()


def _base_url(body=None):
    """Download base for native OxiDNS writes; the request root when not given."""
    value = body.get('base_url') if isinstance(body, dict) else None
    return value.rstrip('/') if isinstance(value, str) and value.strip() else request.url_root.rstrip('/')


def _native_oxidns_without(config, rule_id, base_url):
    """Native OxiDNS settings without a rule about to be deleted, or None if unchanged.

    Native YAML routes OxiDNS, so removing the stored selector alone would leave
    providers and downloads that reference files no publication can produce.
    """
    from backend.common.oxidns_common_routing import remove_rule
    from backend.common.oxidns_native import load_native_yaml
    from backend.common.oxidns_settings import OxidnsSettingsError, normalize_oxidns_settings
    settings = config.get('oxidns') if isinstance(config.get('oxidns'), dict) else {}
    if settings.get('config_mode') != 'native' or not settings.get('native_config'):
        return None
    try:
        load_native_yaml(settings['native_config'])
    except ValueError:
        return None  # An unreadable draft cannot be published; it is fixed in the editor.
    try:
        result = remove_rule(settings, config, rule_id, base_url)
    except OxidnsSettingsError as exc:
        raise ProfileValidationError(str(exc)) from None
    if result is None:
        return None
    # Stored exactly as the editor saves it.
    return normalize_oxidns_settings(result, validate_native_arguments=False)


def _ensure_dns_rule_type(config, rule, dns):
    """A type change must not strand a DNS route the generator cannot express.

    Read from the transaction snapshot; a native OxiDNS draft routes by its
    YAML. Follow rules simply lose their route, and sending dns: default with
    the change clears an explicit OxiDNS one.
    """
    from backend.common.rule_dns import ROUTE_LABELS, convertible, explicit_route
    if convertible(rule):
        return
    kind = rule.get('rule_type')
    from backend.converters.mosdns import DNS_RULE_TYPES as MOSDNS_RULE_TYPES
    mosdns = config.get('mosdns') if isinstance(config.get('mosdns'), dict) else {}
    for key, group in (('direct_rules', '直连规则'), ('proxy_rules', '代理规则')):
        if kind not in MOSDNS_RULE_TYPES and isinstance(mosdns.get(key), list) and rule['id'] in mosdns[key]:
            raise ProfileValidationError(
                f'此规则已用于 MosDNS {group}，{kind} 类型暂不能转换为 MosDNS 规则。'
                '请先在 MosDNS 设置中取消选择，或新建一条规则。')
    route = explicit_route(config, rule['id'])
    if route is not None and dns != 'default':
        raise ProfileValidationError(
            f'此规则已用于 OxiDNS DNS 处理「{ROUTE_LABELS[route.split(":", 1)[0]]}」，{kind} 类型不参与 DNS 分流。'
            '请同时把 DNS 设为默认上游，或新建一条规则')


def _pinned_withheld(rule_ids):
    """Response field: an auto-syncing OxiDNS device withholds a newly pinned upstream
    of these rules, so it must be published manually. Omitted when it cannot be
    computed; the save has already been committed."""
    from backend.agents.rule_sync import pinned_withheld
    try:
        return {'pinned_withheld': pinned_withheld(get_repository(), resolve_profile_id(), rule_ids)}
    except Exception as error:
        logger.warning('Checking withheld pinned upstreams failed: %s', safe_exception_details(error))
        return {}


def _hydrated(config, item):
    """item with its shared source, as the transaction snapshot holds rulesets."""
    from backend.common.rule_dns import hydrate_ruleset
    if item.get('itemType') != 'ruleset':
        return item
    library = next(entry for entry in config.get('rule_library', []) if entry.get('id') == item['library_rule_id'])
    return hydrate_ruleset(copy.deepcopy(item), library, config.get('profile_id'))


def _mutate_rule(rule_id, item_type=None):
    from werkzeug.exceptions import NotFound
    from backend.common.rule_dns import RULE_DNS_FIELDS, apply_rule_dns
    payload = request.get_json() if request.method == 'PUT' else None
    if request.method == 'PUT' and not isinstance(payload, dict):
        raise ProfileValidationError('规则请求必须是 JSON 对象')
    # DNS is per-profile OxiDNS state, never a field of the rule itself.
    dns = payload.pop('dns', _MISSING) if payload is not None else _MISSING
    base_url = _base_url()
    result, warnings = {}, []
    def mutate(profile):
        before = copy.deepcopy(profile)
        items = profile.setdefault('rule_configs', [])
        original = next((item for item in items if item.get('id') == rule_id
                         and (item_type is None or item.get('itemType') == item_type)), None)
        if original is None:
            raise NotFound('Rule not found')
        if request.method == 'DELETE':
            native = _native_oxidns_without(before, rule_id, base_url)
            if native is not None:
                profile['oxidns'] = native
            items.remove(original)
            # Remove target selections in the same transaction as the rule.
            # A later browser read/modify/write can fail or overwrite new edits.
            for engine in ('mosdns', 'oxidns'):
                settings = profile.get(engine, {})
                for key in ('direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules'):
                    if isinstance(settings.get(key), list):
                        settings[key] = [value for value in settings[key] if value != rule_id]
            oxidns = profile.get('oxidns')
            if isinstance(oxidns, dict):
                for key in RULE_DNS_FIELDS:
                    value = oxidns.get(key)
                    if isinstance(value, dict):
                        value.pop(rule_id, None)
                    elif isinstance(value, list):
                        oxidns[key] = [identity for identity in value if identity != rule_id]
            return
        updated = {**original, **payload, 'id': rule_id}
        if item_type:
            updated['itemType'] = item_type
        normalize_rule_config(updated, original)
        result.update(updated)
        updated = _hydrated(profile, updated)
        # Only a type change is checked, so other edits to older selections still save.
        if updated['itemType'] == 'rule' and updated.get('rule_type') != original.get('rule_type'):
            _ensure_dns_rule_type(before, updated, dns)
        items[items.index(original)] = updated
        # Policy, enablement and value edits move follow rules and rebuild native
        # routing; only a request naming dns fails on a custom native draft.
        warnings.extend(apply_rule_dns(before, profile, {} if dns is _MISSING else {rule_id: dns},
                                       base_url=base_url, strict=dns is not _MISSING))
    update_config_transaction(mutate)
    return jsonify({'success': True, **({'data': result} if result else {}),
                    **({'warnings': warnings} if warnings else {}),
                    **(_pinned_withheld([rule_id]) if request.method == 'PUT' else {})})


@bp.route('/<rule_id>', methods=['DELETE', 'PUT'])
@require_auth
def handle_rule(rule_id):
    return _mutate_rule(rule_id)


_DNS_BATCH_LIMIT = 200
_PASTE_LIMIT = 200
_LIBRARY_LIMIT = 50
_BUILTIN_POLICIES = ('DIRECT', 'REJECT', 'REJECT-DROP', 'PASS')


@bp.route('/dns', methods=['GET', 'PUT'])
@require_auth
def handle_rule_dns():
    """Per-rule OxiDNS DNS handling of the current profile.

    PUT {routes: {id: dns}, skip_unsupported, base_url} switches up to 200 rules
    in one transaction; rules whose type cannot be routed are skipped instead
    of refused when skip_unsupported is true.
    """
    from backend.common.rule_dns import apply_rule_dns, check_choice, convertible, rule_dns_entries, rule_dns_state
    if request.method == 'GET':
        return jsonify({'success': True, **rule_dns_state(get_config(), _base_url(request.args))})
    body = request.get_json(silent=True)
    routes = body.get('routes') if isinstance(body, dict) else None
    if not isinstance(routes, dict) or not routes:
        raise ProfileValidationError('请提供要修改 DNS 处理的规则')
    if len(routes) > _DNS_BATCH_LIMIT:
        raise ProfileValidationError(f'一次最多修改 {_DNS_BATCH_LIMIT} 条规则的 DNS 处理')
    for value in routes.values():
        check_choice(value)
    skip = body.get('skip_unsupported') is True
    base_url = _base_url(body)
    skipped, warnings = [], []

    def mutate(profile):
        before = copy.deepcopy(profile)
        items = {item.get('id'): item for item in profile.get('rule_configs', [])}
        assignments = {}
        for identity, choice in routes.items():
            item = items.get(identity)
            if skip and item is not None and choice != 'default' and not convertible(item):
                skipped.append(identity)
                continue
            assignments[identity] = choice
        warnings.extend(apply_rule_dns(before, profile, assignments, base_url=base_url, strict=True))

    update_config_transaction(mutate)
    changed = [identity for identity in routes if identity not in skipped]
    return jsonify({'success': True, 'rules': rule_dns_entries(get_config(), changed, base_url),
                    'skipped': skipped, 'warnings': warnings, **_pinned_withheld(changed)})


def _paste_rows(text, rule_type):
    """Rows of a pasted rule list: (rows, warnings, field errors).

    One rule per line; blank lines and # comments are ignored. ``TYPE,value``
    lines keep a trailing no-resolve and ignore other trailing fields such as a
    policy; ``+.x`` and ``*.x`` are DOMAIN-SUFFIX, ``full:x`` is DOMAIN, and
    other lines use rule_type. Repeated lines are added once.
    """
    lines = [(number, raw.strip()) for number, raw in enumerate(text.splitlines(), 1)]
    lines = [(number, line) for number, line in lines if line and not line.startswith(('#', '//'))]
    if len(lines) > _PASTE_LIMIT:
        raise ProfileValidationError(f'一次最多添加 {_PASTE_LIMIT} 条，较多条目请改为新建规则集')
    rows, warnings, errors, seen = [], [], [], set()
    for number, line in lines:
        no_resolve = False
        if ',' in line:
            fields = [field.strip() for field in line.split(',')]
            kind, value = fields[0].upper(), fields[1]
            if kind in COMMA_PAYLOAD_TYPES:
                value = line.split(',', 1)[1].strip()
            elif kind == 'MATCH':
                value = ''
            extras = [] if kind in COMMA_PAYLOAD_TYPES else fields[1:] if kind == 'MATCH' else fields[2:]
            for extra in extras:
                if extra.lower() == 'no-resolve':
                    no_resolve = True
                elif extra:
                    warnings.append(f'忽略第 {number} 行行尾策略 {extra}')
        elif line.startswith('+.'):
            kind, value = 'DOMAIN-SUFFIX', line[2:].strip()
        elif line.startswith('*.'):
            kind, value = 'DOMAIN-SUFFIX', line[2:].strip()
            warnings.append(f'{line} 将按 DOMAIN-SUFFIX 添加，也包含 {value} 本身')
        elif line.lower().startswith('full:'):
            kind, value = 'DOMAIN', line[5:].strip()
        else:
            kind, value = rule_type, line
        error = rule_value_error(kind, value)
        if error:
            errors.append({'line': number, 'message': error})
            continue
        if (kind, value) in seen:
            continue
        seen.add((kind, value))
        rows.append({'line': number, 'rule_type': kind, 'value': value, 'no_resolve': no_resolve})
    return rows, warnings, errors


def _new_library(value):
    from backend.utils.ruleset_content import validate_domain_content
    if not isinstance(value, dict):
        raise ProfileValidationError('请提供新规则集的来源')
    library = {key: copy.deepcopy(item) for key, item in value.items() if key not in ('id', 'base_url', 'has_ip_part')}
    if not isinstance(library.get('name'), str) or not library['name'].strip():
        raise ProfileValidationError('请填写规则集名称')
    library['name'] = library['name'].strip()
    # Stored entries carry an explicit flag; the rule library page reads a
    # missing one as disabled while generation treats it as enabled.
    library['enabled'] = library.get('enabled', True) is not False
    validate_domain_content(library)
    library.pop('content' if library.get('source_type', 'url') == 'url' else 'url', None)
    return library


def _new_id(prefix, used):
    import uuid
    while True:
        value = f'{prefix}_{uuid.uuid4().hex[:12]}'
        if value not in used:
            used.add(value)
            return value


@bp.route('/compose', methods=['POST'])
@require_auth
def compose_rules():
    """Create rules or ruleset references with their OxiDNS DNS atomically.

    Shared rule-library additions, the profile's rules, its OxiDNS choices
    and (native drafts) the YAML are written in one repository commit; any
    failure writes nothing. Ids are generated here.
    """
    from werkzeug.exceptions import NotFound
    from backend.common.config import update_shared_and_config_transaction
    from backend.common.config_repository import ProfileInUse
    from backend.common.rule_dns import IP_RULE_TYPES, apply_rule_dns, check_choice, convertible, rule_dns_entries
    body = request.get_json(silent=True)
    if not isinstance(body, dict):
        raise ProfileValidationError('请求必须是 JSON 对象')
    source = body.get('source') if isinstance(body.get('source'), dict) else {}
    rule = body.get('rule') if isinstance(body.get('rule'), dict) else {}
    kind = source.get('kind')
    dns = check_choice(body.get('dns', 'default'))
    dns_ip = check_choice(body['dns_ip']) if body.get('dns_ip') is not None else dns
    base_url = _base_url(body)
    policy = rule.get('policy')
    if not isinstance(policy, str) or not policy.strip():
        raise ProfileValidationError('请选择策略')
    position = rule.get('position', 'top')
    if position not in ('top', 'bottom'):
        raise ProfileValidationError('位置只能是 top 或 bottom')
    common = {'policy': policy, 'enabled': rule.get('enabled', True) is not False,
              'remark': rule.get('remark') if isinstance(rule.get('remark'), str) else '',
              'group_name': rule.get('group_name') if isinstance(rule.get('group_name'), str) else ''}
    warnings, rows, library, library_ids = [], [], None, []
    if kind == 'values':
        text = source.get('text')
        if not isinstance(text, str):
            raise ProfileValidationError('请输入规则')
        rule_type = source.get('rule_type') if isinstance(source.get('rule_type'), str) else ''
        rows, warnings, errors = _paste_rows(text, rule_type.strip().upper())
        if errors:
            first = errors[0]
            return jsonify({'success': False, 'message': f'第 {first["line"]} 行：{first["message"]}',
                            'field_errors': errors}), 400
        if not rows:
            raise ProfileValidationError('没有可添加的规则')
        for row in rows:
            choice = dns_ip if row['rule_type'] in IP_RULE_TYPES else dns
            if choice != 'default' and not convertible({'itemType': 'rule', 'rule_type': row['rule_type']}):
                raise ProfileValidationError(
                    f'第 {row["line"]} 行 {row["rule_type"]} 规则不参与 DNS 分流，请把 DNS 设为默认上游')
    elif kind == 'library':
        library_ids = source.get('library_rule_ids')
        if (not isinstance(library_ids, list) or not 1 <= len(library_ids) <= _LIBRARY_LIMIT or
                any(not isinstance(value, str) or not value for value in library_ids) or
                len(set(library_ids)) != len(library_ids)):
            raise ProfileValidationError(f'请选择 1 到 {_LIBRARY_LIMIT} 个规则集')
    elif kind == 'new_library':
        library = _new_library(source.get('library'))
    else:
        raise ProfileValidationError('请选择规则来源：values、library 或 new_library')
    prepared_remote = None
    if library is not None and library.get('source_type', 'url') == 'url':
        from backend.utils.rule_utils import read_rule_source_content
        import requests
        try:
            prepared_remote = read_rule_source_content(library)
        except requests.RequestException:
            return jsonify({'success': False, 'message': '规则源下载失败，未添加规则，请检查来源后重试'}), 502
        if prepared_remote is None:
            raise ProfileValidationError('请提供有效的规则来源和订阅地址')
    created, skipped = [], []

    def add_library(shared):
        entries = shared.setdefault('rule_library', [])
        if any(entry.get('name') == library['name'] for entry in entries):
            raise ProfileValidationError(f'规则库已有同名规则集「{library["name"]}」')
        library['id'] = _new_id('lib', {entry.get('id') for entry in entries})
        entries.append(copy.deepcopy(library))

    def compose(before, profile):
        groups = {group.get('name') for group in profile.get('proxy_groups', [])}
        if policy not in _BUILTIN_POLICIES and policy not in groups:
            raise ProfileValidationError(f'策略不存在：{policy}')
        items = profile.setdefault('rule_configs', [])
        used = {item.get('id') for item in items}
        additions, assignments = [], {}
        if kind == 'values':
            existing = {(item.get('rule_type'), item.get('value')): (index, item)
                        for index, item in reversed(list(enumerate(items, 1))) if item.get('itemType') != 'ruleset'}
            for row in rows:
                found = existing.get((row['rule_type'], row['value']))
                if found:
                    index, item = found
                    skipped.append({'line': row['line'], 'value': row['value'], 'existing_id': item['id'],
                                    'index': index, 'enabled': item.get('enabled', True) is not False})
                    continue
                ip = row['rule_type'] in IP_RULE_TYPES
                item = {'id': _new_id('rule', used), 'itemType': 'rule', 'rule_type': row['rule_type'],
                        'value': row['value'], **common,
                        'no_resolve': row['no_resolve'] or (ip and rule.get('no_resolve') is True)}
                additions.append(item)
                assignments[item['id']] = dns_ip if ip else dns
        else:
            library_by_id = {entry.get('id'): entry for entry in profile.get('rule_library', [])}
            for library_id in library_ids if kind == 'library' else [library['id']]:
                if library_id not in library_by_id:
                    raise NotFound('规则集必须引用存在的共享规则库条目')
                index = next((index for index, item in enumerate(items, 1)
                              if item.get('itemType') == 'ruleset' and item.get('library_rule_id') == library_id), None)
                if index is not None:
                    # Two references to one ruleset make native OxiDNS routing ambiguous.
                    raise ProfileInUse(f'该规则集已在策略规则 #{index}', [{
                        'kind': 'rule', 'code': 'ruleset_already_used', 'id': items[index - 1]['id'], 'index': index}])
                item = {'id': _new_id('ruleset', used), 'itemType': 'ruleset', 'library_rule_id': library_id,
                        **common, 'no_resolve': rule.get('no_resolve') is True}
                additions.append(item)
                assignments[item['id']] = dns
        created.extend(copy.deepcopy(additions))
        hydrated = [_hydrated(profile, item) for item in additions]
        if position == 'top':
            items[0:0] = hydrated
        else:
            items.extend(hydrated)
        dns_capable = {item['id'] for item in hydrated if convertible(item)}
        assignments = {identity: choice for identity, choice in assignments.items()
                       if choice != 'default' and identity in dns_capable}
        if assignments:
            warnings.extend(apply_rule_dns(before, profile, assignments, base_url=base_url, strict=True))

    try:
        if library is not None:
            update_shared_and_config_transaction(add_library, lambda _, before, after: compose(before, after))
        else:
            update_config_transaction(lambda profile: compose(copy.deepcopy(profile), profile))
    except NotFound as error:
        return jsonify({'success': False, 'message': error.description}), 404
    if library is not None:
        from backend.utils.rule_utils import save_rule_to_local
        try:
            if prepared_remote is not None:
                from backend.utils.rule_utils import write_validated_rule_cache
                write_validated_rule_cache(library, prepared_remote)
            else:
                save_rule_to_local(library)
        except Exception as error:  # The cache is refilled on demand; the commit stands.
            logger.warning('Caching composed rule library failed: %s', safe_exception_details(error))
            warnings.append('规则集已保存，但本地缓存失败，发布时会重新获取')
    return jsonify({'success': True, 'data': {'rules': created, 'library': library},
                    'dns': rule_dns_entries(get_config(), [item['id'] for item in created], base_url),
                    'skipped': skipped, 'warnings': warnings, 'draft': True,
                    **_pinned_withheld([item['id'] for item in created])})


@bp.route('/group-name', methods=['POST'])
@require_auth
def rename_rule_group():
    """Update the selected labels together without replacing stale rule fields."""
    from werkzeug.exceptions import NotFound
    body = request.get_json(silent=True)
    if not isinstance(body, dict) or not isinstance(body.get('group_name'), str):
        raise ProfileValidationError('请提供规则 ID 列表和组名称')
    ids = body.get('ids')
    if (not isinstance(ids, list) or not ids
            or any(not isinstance(value, str) or not value for value in ids)
            or len(set(ids)) != len(ids)):
        raise ProfileValidationError('规则 ID 列表不能为空或包含重复项')
    name = body['group_name'].strip()

    def mutate(profile):
        items = {item['id']: item for item in profile.get('rule_configs', [])}
        if any(value not in items for value in ids):
            raise NotFound('部分规则已被删除，请刷新列表后再重命名')
        for value in ids:
            items[value]['group_name'] = name

    try:
        update_config_transaction(mutate)
    except NotFound as error:
        return jsonify({'success': False, 'message': error.description}), 404
    return jsonify({'success': True})


@bp.route('/policy', methods=['PUT'])
@require_auth
def set_rule_policy():
    """Move rules to one policy in a single write; follow DNS moves with it.

    PUT {ids, policy, group_name?, base_url}. A group header changes every
    member and keeps its label; a row leaving its group sends group_name: ''
    as the editor does. Explicit DNS choices never change with the policy.
    """
    from werkzeug.exceptions import NotFound
    from backend.common.rule_dns import apply_rule_dns
    body = request.get_json(silent=True)
    if not isinstance(body, dict):
        raise ProfileValidationError('请提供规则 ID 列表和策略')
    ids = body.get('ids')
    if (not isinstance(ids, list) or not ids
            or any(not isinstance(value, str) or not value for value in ids)
            or len(set(ids)) != len(ids)):
        raise ProfileValidationError('规则 ID 列表不能为空或包含重复项')
    # No batch cap: a group of any size is one field change and one routing rebuild.
    policy = body.get('policy')
    if not isinstance(policy, str) or not policy:
        raise ProfileValidationError('请选择策略')
    group_name = body.get('group_name', _MISSING)
    if group_name is not _MISSING and not isinstance(group_name, str):
        raise ProfileValidationError('组名称必须是文本')
    base_url = _base_url(body)
    warnings = []

    def mutate(profile):
        groups = {group.get('name') for group in profile.get('proxy_groups', [])}
        if policy not in _BUILTIN_POLICIES and policy not in groups:
            raise ProfileValidationError(f'策略不存在：{policy}')
        before = copy.deepcopy(profile)
        items = {item.get('id'): item for item in profile.get('rule_configs', []) if isinstance(item, dict)}
        if any(value not in items for value in ids):
            raise NotFound('部分规则已被删除，请刷新列表后再修改')
        for value in ids:
            items[value]['policy'] = policy
            if group_name is not _MISSING:
                items[value]['group_name'] = group_name.strip()
        # Follow rules move with the policy and native routing is rebuilt; a
        # custom native draft keeps its YAML and the save returns a warning.
        warnings.extend(apply_rule_dns(before, profile, {}, base_url=base_url, strict=False))

    try:
        update_config_transaction(mutate)
    except NotFound as error:
        return jsonify({'success': False, 'message': error.description}), 404
    return jsonify({'success': True, **({'warnings': warnings} if warnings else {}), **_pinned_withheld(ids)})


@bp.route('/reorder', methods=['POST'])
@require_auth
def reorder_rules():
    """批量更新规则和规则集顺序

    按 id 排序时传 {'ids': [...], 'position': 'top'|'bottom'}；
    传完整 rule_configs 数组的旧格式仍然兼容。
    """
    from werkzeug.exceptions import NotFound
    body = request.get_json()
    if not isinstance(body, dict) or not body:
        raise ProfileValidationError('No request body provided')
    ids = body.get('ids')
    if ids is None:
        items = body.get('rule_configs')
        if not isinstance(items, list) or any(not isinstance(item, dict) for item in items):
            raise ProfileValidationError('rule_configs must be a list')
        ids = [item.get('id') for item in items]
    if not isinstance(ids, list):
        raise ProfileValidationError('ids must be a list')
    from backend.common.rule_dns import apply_rule_dns
    order, warnings = [], []
    base_url = _base_url(body)
    def mutate(profile):
        before = copy.deepcopy(profile)
        items, missing = reorder_by_ids(profile.get('rule_configs', []), ids, body.get('position', 'top'))
        if missing:
            raise NotFound(f'以下规则 id 不存在: {missing}')
        profile['rule_configs'] = items
        order.extend(item['id'] for item in items)
        # Native OxiDNS routes in rule order; rebuild it with the new order.
        warnings.extend(apply_rule_dns(before, profile, {}, base_url=base_url, strict=False))
    update_config_transaction(mutate)
    return jsonify({'success': True, 'order': order, **({'warnings': warnings} if warnings else {})})


@bp.route('/batch', methods=['POST'])
@require_auth
def batch_add_rules():
    """批量添加规则"""
    data = request.get_json()
    if not isinstance(data, dict):
        raise ProfileValidationError('规则请求必须是 JSON 对象')
    rule_type = data.get('rule_type')
    domains = data.get('domains', [])  # 域名列表
    if not isinstance(domains, list) or not domains or not all(isinstance(value, str) for value in domains):
        raise ProfileValidationError('请提供规则值列表')
    for number, value in enumerate(domains, 1):
        error = rule_value_error(rule_type, value.strip())
        if error:
            raise ProfileValidationError(f'第 {number} 行：{error}')
    policy = data.get('policy')

    new_rules = []

    def add_rules(profile):
        # Allocate under the repository transaction lock; never renumber stored IDs.
        rules = profile.setdefault('rule_configs', [])
        used_ids = {r.get('id') for r in rules}
        next_id = 1
        for domain in domains:
            while f'rule_{next_id}' in used_ids:
                next_id += 1
            rule_id = f'rule_{next_id}'
            used_ids.add(rule_id)
            new_rules.append({
                'id': rule_id,
                'rule_type': rule_type,
                'value': domain.strip(),
                'policy': policy,
                'enabled': True,
                'itemType': 'rule',
            })
        rules[0:0] = new_rules

    update_config_transaction(add_rules)
    return jsonify({'success': True, 'count': len(new_rules), 'rules': new_rules})


@bp.route('/local/<name>')
def get_local_rule(name):
    """获取本地缓存的规则文件（通过规则名称）

    Mihomo 的文本 provider 通过 target=mihomo 请求编码和格式兼容内容；
    无标记请求继续返回原文，供其他客户端及规则转换器使用。

    注意：此接口不需要权限校验，因为会被 Mihomo 等客户端直接访问。

    对于 URL 类型的规则，会尝试在 2 秒内拉取最新数据，
    如果超时则返回本地缓存版本。
    """
    import os
    from flask import send_file
    from backend.common.config import get_repository
    from backend.common.profile_context import resolve_profile_id
    from backend.utils.logger import get_logger

    logger = get_logger(__name__)
    config_data = get_config()

    try:
        # 对规则名称进行清理，确保与文件名匹配
        from backend.utils.rule_utils import rule_cache_filename, rule_cache_read_path, save_rule_to_local

        profile_id = resolve_profile_id()
        repository = get_repository()
        # 查找规则库中的规则（通过名称）
        rule_library = config_data.get('rule_library', [])
        rule = next((r for r in rule_library if r.get('name') == name), None)
        filename = rule_cache_filename(rule or {'name': name})
        filepath = rule_cache_read_path(rule or {'name': name}, rule_library)

        def content_response(content):
            # Transform only the delivered bytes, never the shared source/cache.
            if rule and request.args.get('target') == 'mihomo':
                from backend.converters.common import ruleset_format
                from backend.utils.ruleset_content import normalize_mihomo_ruleset
                try:
                    normalized, warnings = normalize_mihomo_ruleset(
                        content.decode('utf-8'), rule.get('behavior', 'domain'), ruleset_format(rule, config_data))
                except UnicodeDecodeError:
                    logger.warning("Rule '%s' is not UTF-8; leaving rule content unchanged", name)
                else:
                    content = normalized.encode('utf-8')
                    if warnings:
                        logger.warning("Rule '%s': %s entries need manual review (lines %s)",
                                       name, len(warnings), [item['line'] for item in warnings[:10]])
            resp = make_response(content, 200)
            resp.headers['Content-Type'] = 'text/plain; charset=utf-8'
            resp.headers['Content-Length'] = str(len(content))
            return resp

        logger.info(f"Requesting local rule: {name}, filepath: {filepath}")

        # 如果规则仓库中没有找到该规则
        if not rule:
            # 检查本地缓存是否存在（可能是旧数据）
            if os.path.exists(filepath):
                logger.warning(f"Rule '{name}' not found in library, but cache exists, returning cache")
                with open(filepath, 'rb') as f:
                    content = f.read()
                return content_response(content)
            else:
                logger.error(f"Rule '{name}' not found in rule library and no cache exists")
                return jsonify({
                    'success': False,
                    'message': f'Rule not found in rule library: {name}. Please add it to the rule library first.'
                }), 404

        from backend.utils.scheduled_refresh import read_rule_source
        from backend.utils.ruleset_content import RemoteRuleValidationError
        if rule.get('source_type', 'url') == 'url':
            try:
                def fetch(url):
                    response = request_rule(url, timeout=2, config_data=config_data)
                    response.raise_for_status()
                    return response.content
                # Library sources are served from the cache the scheduled task keeps fresh.
                content, error = read_rule_source(rule, config_data, fetch=fetch)
            except RemoteRuleValidationError as error:
                return jsonify({'success': False, 'message': str(error), 'errors': error.errors,
                                'cache_retained': False}), 422
            except Exception as error:
                return jsonify({'success': False, 'message': safe_exception_details(error),
                                'cache_retained': False}), 502
            response = content_response(content)
            if error is not None:
                response.headers['X-ConfigFlow-Cache'] = 'stale'
                response.headers['Warning'] = '110 ConfigFlow "Rule refresh failed; using validated cache"'
            return response

        if os.path.exists(filepath):
            with open(filepath, 'rb') as handle:
                return content_response(handle.read())
        save_rule_to_local(rule)
        with open(rule_cache_read_path(rule, rule_library), 'rb') as handle:
            return content_response(handle.read())

    except Exception as e:
        logger.error("Error getting local rule '%s': %s", name, safe_exception_details(e))
        return jsonify({'success': False, 'message': 'Error getting local rule'}), 500


# ==================== /api/rule-sets 路由（向后兼容）====================
# 注意：规则集现在统一存储在 rule_configs 数组中，通过 itemType='ruleset' 区分
# 这些接口是为了向后兼容旧版前端代码

@rule_sets_bp.route('', methods=['GET', 'POST'])
@require_auth
def handle_rule_sets():
    """规则集管理（使用 rule_configs 数组）"""
    config_data = get_config()
    if request.method == 'GET':
        # 从 rule_configs 中筛选出规则集
        rule_configs = config_data.get('rule_configs', [])
        rule_sets = [r for r in rule_configs if r.get('itemType') == 'ruleset']
        # 拼接完整 URL（用于前端显示）
        rule_sets_with_full_url = attach_full_url_to_rules(rule_sets)
        return jsonify(rule_sets_with_full_url)

    elif request.method == 'POST':
        rule_set = request.json
        # 确保有 itemType 字段
        rule_set['itemType'] = 'ruleset'
        normalize_rule_config(rule_set)
        update_config_transaction(
            lambda profile: profile.setdefault('rule_configs', []).insert(0, rule_set)
        )
        return jsonify({'success': True, 'data': rule_set})


@rule_sets_bp.route('/<rule_set_id>', methods=['DELETE', 'PUT'])
@require_auth
def handle_rule_set(rule_set_id):
    return _mutate_rule(rule_set_id, 'ruleset')




def get_ruleset_content(rule_item: dict, library_rule: dict = None, allow_network: bool = True, *,
                        config_data: dict = None, timeout=30) -> str:
    """获取规则集内容（优先使用本地缓存，从URL获取后会自动缓存）

    Args:
        rule_item: 规则集配置项
        library_rule: 规则仓库中的规则（可选）
        allow_network: 为 False 时只读本地缓存和内容型规则，不发起网络请求
        config_data: 本次请求的配置快照，下载远程内容时使用（可选）
        timeout: 每次下载请求的超时（秒，或 requests 的 (连接, 读取) 元组）

    Returns:
        规则集内容字符串，获取失败返回空字符串
    """
    from backend.utils.rule_utils import read_validated_rule_cache, refresh_remote_rule
    from backend.utils.ruleset_content import RemoteRuleValidationError
    source = library_rule or rule_item
    cached = read_validated_rule_cache(source, config_data)
    if cached is not None:
        try:
            return cached.decode('utf-8-sig')
        except UnicodeError:
            return ''  # The page matcher cannot interpret MRS binaries.
    if source.get('source_type') == 'content':
        return source.get('content', '')
    if not allow_network:
        return ''
    url = source.get('url', '')
    if not url:
        return ''
    if config_data is None:
        config_data = get_config()
    if url.startswith('/'):
        url = config_data.get('system_config', {}).get('server_domain', '').rstrip('/') + url
    def fetch(_):
        response = request_rule(url, timeout=timeout, config_data=config_data)
        response.raise_for_status()
        return response.content
    try:
        content, _ = refresh_remote_rule(source, config_data, fetch=fetch)
        return content.decode('utf-8-sig')
    except RemoteRuleValidationError:
        raise
    except Exception as error:
        logger.warning('Failed to fetch ruleset %s: %s', source.get('name', ''), safe_exception_details(error))
        alternate = rule_item.get('url')
        if library_rule and alternate and alternate != library_rule.get('url'):
            # Retain the legacy mirror fallback, but bind and validate those
            # bytes under the actual alternate source rather than the failed URL.
            from backend.converters.common import ruleset_format
            mirror = {**library_rule, 'url': alternate, 'format': ruleset_format(library_rule, config_data)}
            return get_ruleset_content(mirror, config_data=config_data, timeout=timeout)
        return ''


@bp.route('/match-test', methods=['POST'])
@require_auth
def match_test_rule():
    """规则索引 - 测试域名/IP匹配哪条规则（专业版功能）"""
    import time
    config_data = get_config()

    # 记录开始时间
    start_time = time.time()

    try:
        data = request.json
        query = data.get('query', '').strip()

        if not query:
            return jsonify({'success': False, 'message': '请输入域名或IP地址'}), 400

        # DNS 根点与内核规范化一致；只放宽规则测试，不影响域名发现校验。
        query = normalize_match_query(query)
        if not is_valid_domain(query, allow_dns_labels=True) and not is_valid_ip(query):
            return jsonify({'success': False, 'message': '请输入有效的域名或IP地址'}), 400

        from backend.utils.ruleset_content import RemoteRuleValidationError
        invalid_sources = []
        def load_source(item, library_rule):
            try:
                return get_ruleset_content(item, library_rule, config_data=config_data)
            except RemoteRuleValidationError as error:
                invalid_sources.append(error)
                return ''
        matcher = RuleConfigMatcher(
            config_data.get('rule_configs', []), config_data.get('rule_library', []), load_source)
        matched = matcher.match(query)
        if invalid_sources:
            error = invalid_sources[0]
            return jsonify({'success': False, 'message': str(error), 'errors': error.errors}), 422
        if matched:
            elapsed_time = time.time() - start_time
            return jsonify({
                'success': True,
                'matched': True,
                **matched,
                'elapsed_time': round(elapsed_time * 1000, 2)  # 转换为毫秒，保留2位小数
            })

        # 没有匹配到任何规则
        elapsed_time = time.time() - start_time
        return jsonify({
            'success': True,
            'matched': False,
            'message': '该域名/IP未匹配任何规则，将使用默认策略',
            'elapsed_time': round(elapsed_time * 1000, 2)  # 转换为毫秒，保留2位小数
        })

    except Exception as e:
        logger.error('Rule match test failed: %s', safe_exception_details(e))
        return jsonify({'success': False, 'message': '查询失败'}), 500


class _DuplicateOccurrences:
    """Keep single occurrences compact until a rule actually repeats."""

    __slots__ = ('value', 'first', 'additional')

    def __init__(self, value, first):
        self.value = value
        self.first = first
        self.additional = None

    def add(self, position):
        if self.additional is None:
            self.additional = []
        self.additional.append(position)

    def result(self, rule_type):
        """Consume positions once, reusing their list for the response records."""
        if self.additional is None:
            return None
        items = self.additional
        items.insert(0, self.first)
        self.first = None
        self.additional = None
        # Release each compact tuple as its final record is built, including
        # when one duplicate group contains nearly every rule in the scan.
        for index, position in enumerate(items):
            source_type, source, rule_id, policy, priority, line, line_no = position
            item = {
                'source_type': source_type, 'source': source, 'rule_id': rule_id,
                'policy': policy, 'priority': priority, 'line': line,
            }
            if line_no is not None:
                item['line_no'] = line_no
            items[index] = item
        return {
            'rule_type': rule_type, 'value': self.value, 'count': len(items),
            'policy_conflict': len({item['policy'] for item in items}) > 1,
            'occurrences': items,
        }


@bp.route('/find-duplicates', methods=['POST'])
@require_auth
def find_duplicate_rules():
    """查找重复规则 - 检查直接规则与规则集内容中的重复条目"""
    import time
    import uuid
    config_data = get_config()

    start_time = time.perf_counter()
    scan_id = uuid.uuid4().hex[:12]
    g.rule_duplicate_scan = (scan_id, start_time)

    try:
        rule_configs = config_data.get('rule_configs', [])
        rule_library = config_data.get('rule_library', [])
        logger.info('Duplicate scan %s started: %d configured items', scan_id, len(rule_configs))

        # Keep first-appearance order for stable ties in the final sort.
        occurrences = {}
        rules_checked = 0
        rulesets_checked = 0
        failed_rulesets = []

        # 正则类规则值大小写敏感，不做小写归一
        case_sensitive_types = {'REGEX', 'REGEXP'}

        def add_occurrence(rule_type, rule_value, position):
            normalized = rule_value if rule_type in case_sensitive_types else rule_value.lower()
            key = (rule_type, normalized)
            entry = occurrences.get(key)
            if entry is None:
                # Share an equal normalized value instead of retaining both strings.
                value = normalized if rule_value == normalized else rule_value
                occurrences[key] = _DuplicateOccurrences(value, position)
            else:
                entry.add(position)

        for index, rule_item in enumerate(rule_configs, start=1):
            # 跳过禁用的规则（与生成配置、规则索引行为一致）
            if not rule_item.get('enabled', True) or not rule_item.get('library_enabled', True):
                continue

            item_type = rule_item.get('itemType', 'rule')

            if item_type == 'rule':
                rule_type = (rule_item.get('rule_type') or '').strip().upper()
                rule_value = (rule_item.get('value') or '').strip()
                if not rule_type or not rule_value:
                    continue

                rules_checked += 1
                add_occurrence(rule_type, rule_value, (
                    'rule', '直接配置的规则', rule_item.get('id', ''),
                    rule_item.get('policy', 'DIRECT'), index,
                    f'{rule_type},{rule_value}', None,
                ))

            elif item_type == 'ruleset':
                rule_set_name = rule_item.get('name', '规则集')
                policy = rule_item.get('policy', 'DIRECT')
                library_rule_id = rule_item.get('library_rule_id', '')

                library_rule = None
                if library_rule_id:
                    library_rule = next((r for r in rule_library if r.get('id') == library_rule_id), None)

                source_started = time.perf_counter()
                rule_content = get_ruleset_content(rule_item, library_rule, config_data=config_data)
                if not rule_content:
                    logger.warning('Duplicate scan %s skipped ruleset "%s": unable to fetch content', scan_id, rule_set_name)
                    failed_rulesets.append(rule_set_name)
                    continue

                rulesets_checked += 1
                source_entries = 0
                for line_no, line in enumerate(rule_content.splitlines(), start=1):
                    parsed = parse_rule_line(line)
                    if not parsed:
                        continue

                    rule_type, rule_value = parsed
                    if not rule_value:
                        continue

                    source_entries += 1
                    add_occurrence(rule_type, rule_value, (
                        'ruleset', rule_set_name, rule_item.get('id', ''),
                        policy, index, line.strip(), line_no,
                    ))
                logger.info('Duplicate scan %s scanned ruleset "%s": %d entries in %.1f ms',
                            scan_id, rule_set_name, source_entries, (time.perf_counter() - source_started) * 1000)

        # 同一规则集内部与跨来源的重复都算重复
        duplicates = []
        for (rule_type, _), entry in occurrences.items():
            duplicate = entry.result(rule_type)
            if duplicate is not None:
                duplicates.append(duplicate)
        # The scan index is no longer needed while the response is encoded.
        occurrences.clear()
        entry = None

        # 策略冲突的排前面，其余按出现次数降序、优先级升序
        duplicates.sort(key=lambda d: (not d['policy_conflict'], -d['count'], d['occurrences'][0]['priority']))

        elapsed_time = time.perf_counter() - start_time
        logger.info('Duplicate scan %s matched: %d duplicate groups in %.1f ms; preparing response',
                    scan_id, len(duplicates), elapsed_time * 1000)
        return jsonify_external({
            'success': True,
            'duplicates': duplicates,
            'stats': {
                'rules_checked': rules_checked,
                'rulesets_checked': rulesets_checked,
                'failed_rulesets': failed_rulesets,
                'duplicate_groups': len(duplicates)
            },
            'elapsed_time': round(elapsed_time * 1000, 2)  # 毫秒
        })

    except Exception as e:
        logger.error('Find duplicate rules failed: %s', safe_exception_details(e))
        return jsonify({'success': False, 'message': '查重失败'}), 500
