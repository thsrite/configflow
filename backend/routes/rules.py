"""规则路由模块"""
import copy
import os
import requests
from flask import g, request, jsonify, make_response
from backend.routes import rules_bp as bp, rule_sets_bp as rule_sets_bp
from backend.common.auth import require_auth
from backend.common.config import (
    update_config_transaction,
    get_config,
    get_repository,
)
from backend.common.profile_context import resolve_profile_id
from backend.common.config_repository import ProfileRepositoryError, ProfileValidationError
from backend.utils.rule_matcher import RuleConfigMatcher, parse_rule_line, is_valid_domain, is_valid_ip
from backend.utils.reorder import reorder_by_ids
from backend.utils.rule_utils import get_rules_dir, sanitize_rule_name
from backend.utils.logger import get_logger
from backend.utils.url_utils import safe_exception_details, safe_url_for_log
from backend.utils.rule_fetch import request_rule

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


def normalize_rule_config(rule_item: dict) -> None:
    """Persist composition only; shared library owns every source field."""
    from backend.common.config_repository import ProfileValidationError
    if not isinstance(rule_item, dict):
        raise ProfileValidationError('规则请求必须是 JSON 对象')
    rule_item.setdefault('itemType', 'rule' if 'rule_type' in rule_item else 'ruleset')
    if rule_item['itemType'] != 'ruleset':
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


def _mutate_rule(rule_id, item_type=None):
    from werkzeug.exceptions import NotFound
    payload = request.get_json() if request.method == 'PUT' else None
    if request.method == 'PUT' and not isinstance(payload, dict):
        raise ProfileValidationError('规则请求必须是 JSON 对象')
    result = {}
    def mutate(profile):
        items = profile.setdefault('rule_configs', [])
        original = next((item for item in items if item.get('id') == rule_id
                         and (item_type is None or item.get('itemType') == item_type)), None)
        if original is None:
            raise NotFound('Rule not found')
        if request.method == 'DELETE':
            items.remove(original)
            return
        updated = {**original, **payload, 'id': rule_id}
        if item_type:
            updated['itemType'] = item_type
        normalize_rule_config(updated)
        items[items.index(original)] = updated
        result.update(updated)
    update_config_transaction(mutate)
    return jsonify({'success': True, **({'data': result} if result else {})})


@bp.route('/<rule_id>', methods=['DELETE', 'PUT'])
@require_auth
def handle_rule(rule_id):
    return _mutate_rule(rule_id)


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
    order = []
    def mutate(profile):
        items, missing = reorder_by_ids(profile.get('rule_configs', []), ids, body.get('position', 'top'))
        if missing:
            raise NotFound(f'以下规则 id 不存在: {missing}')
        profile['rule_configs'] = items
        order.extend(item['id'] for item in items)
    update_config_transaction(mutate)
    return jsonify({'success': True, 'order': order})


@bp.route('/batch', methods=['POST'])
@require_auth
def batch_add_rules():
    """批量添加规则"""
    data = request.json
    rule_type = data.get('rule_type')
    domains = data.get('domains', [])  # 域名列表
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

    这个端点用于 Mihomo 配置中的 rule-providers，
    提供本地缓存的规则文件，避免外部网络请求。

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
        from backend.utils.rule_utils import sanitize_rule_name, get_rules_dir, save_rule_to_local

        profile_id = resolve_profile_id()
        repository = get_repository()
        filename = f"{sanitize_rule_name(name)}.list"
        filepath = os.path.join(get_rules_dir(), filename)

        logger.info(f"Requesting local rule: {name}, filepath: {filepath}")

        # 查找规则库中的规则（通过名称）
        rule_library = config_data.get('rule_library', [])
        rule = next((r for r in rule_library if r.get('name') == name), None)

        # 如果规则仓库中没有找到该规则
        if not rule:
            # 检查本地缓存是否存在（可能是旧数据）
            if os.path.exists(filepath):
                logger.warning(f"Rule '{name}' not found in library, but cache exists, returning cache")
                with open(filepath, 'rb') as f:
                    content = f.read()
                resp = make_response(content, 200)
                resp.headers['Content-Type'] = 'text/plain; charset=utf-8'
                resp.headers['Content-Length'] = str(len(content))
                return resp
            else:
                logger.error(f"Rule '{name}' not found in rule library and no cache exists")
                return jsonify({
                    'success': False,
                    'message': f'Rule not found in rule library: {name}. Please add it to the rule library first.'
                }), 404

        # 如果找到规则且是 URL 类型，尝试实时拉取最新数据
        if rule.get('source_type') == 'url':
            import requests
            url = rule.get('url', '')
            logger.info(f"Rule '{name}' is URL type, attempting to fetch from: {safe_url_for_log(url)}")

            try:
                # 尝试在 2 秒内拉取最新数据
                response = request_rule(url, timeout=2, config_data=config_data)
                if response.status_code == 200:
                    logger.info(f"Successfully fetched latest data for rule '{name}'")
                    # 更新本地缓存
                    repository.write_shared_text(os.path.join('rules', filename), response.text)
                    # 返回响应并添加 Content-Length 头
                    content = response.text.encode('utf-8')
                    resp = make_response(content, 200)
                    resp.headers['Content-Type'] = 'text/plain; charset=utf-8'
                    resp.headers['Content-Length'] = str(len(content))
                    return resp
                else:
                    logger.warning(f"Failed to fetch rule '{name}', status code: {response.status_code}")
            except requests.Timeout:
                logger.warning(f"Timeout fetching rule '{name}', falling back to cache")
            except Exception as e:
                logger.error("Error fetching rule '%s', falling back to cache: %s", name, safe_exception_details(e))

        # 如果是 content 类型或拉取失败，使用本地缓存
        if os.path.exists(filepath):
            logger.info(f"Returning cached rule file: {filepath}")
            with open(filepath, 'rb') as f:
                content = f.read()
            resp = make_response(content, 200)
            resp.headers['Content-Type'] = 'text/plain; charset=utf-8'
            resp.headers['Content-Length'] = str(len(content))
            return resp
        else:
            # 缓存文件不存在，尝试重新生成
            logger.warning(f"Cache file not found for rule '{name}', regenerating...")
            try:
                save_rule_to_local(rule)
                if os.path.exists(filepath):
                    logger.info(f"Successfully regenerated cache for rule '{name}'")
                    with open(filepath, 'rb') as f:
                        content = f.read()
                    resp = make_response(content, 200)
                    resp.headers['Content-Type'] = 'text/plain; charset=utf-8'
                    resp.headers['Content-Length'] = str(len(content))
                    return resp
                else:
                    logger.error(f"Failed to regenerate cache for rule '{name}'")
                    return jsonify({
                        'success': False,
                        'message': f'Failed to generate cache for rule: {name}'
                    }), 500
            except Exception as cache_error:
                logger.error("Error regenerating cache for rule '%s': %s", name, safe_exception_details(cache_error))
                return jsonify({
                    'success': False,
                    'message': 'Error generating rule cache'
                }), 500

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
                        config_data: dict = None) -> str:
    """获取规则集内容（优先使用本地缓存，从URL获取后会自动缓存）

    Args:
        rule_item: 规则集配置项
        library_rule: 规则仓库中的规则（可选）
        allow_network: 为 False 时只读本地缓存和内容型规则，不发起网络请求
        config_data: 本次请求的配置快照，下载远程内容时使用（可选）

    Returns:
        规则集内容字符串，获取失败返回空字符串
    """
    import os

    rule_content = ''
    rule_name = ''
    filepath = ''
    repository = get_repository()

    # 获取规则名称和缓存路径
    if library_rule:
        rule_name = library_rule.get('name', '')
    if not rule_name:
        rule_name = rule_item.get('name', '')

    if rule_name:
        filename = f"{sanitize_rule_name(rule_name)}.list"
        filepath = str(os.path.join(get_rules_dir(), filename))

    # 1. 优先尝试从本地缓存读取
    if filepath and os.path.exists(filepath):
        try:
            with open(filepath, 'r', encoding='utf-8') as f:
                rule_content = f.read()
            logger.info(f"Loaded rule content from cache: {filepath}")
            return rule_content
        except Exception as e:
            logger.warning("Failed to read cached rule file %s: %s", filepath, safe_exception_details(e))

    # 2. 如果缓存不存在或读取失败，从规则仓库获取内容
    if library_rule and not rule_content:
        source_type = library_rule.get('source_type', 'url')
        if source_type == 'content':
            # 直接使用内容（content类型在规则仓库保存时已经缓存）
            rule_content = library_rule.get('content', '')
            logger.info(f"Using rule content from library config")
            return rule_content
        elif allow_network:
            # 从 URL 获取
            url = library_rule.get('url', '')
            if url:
                if config_data is None:
                    config_data = get_config()
                try:
                    response = request_rule(url, timeout=30, config_data=config_data)
                    if response.status_code == 200:
                        rule_content = response.text
                        logger.info(f"Fetched rule content from library URL: {safe_url_for_log(url)}")

                        # 保存到本地缓存
                        if filepath:
                            try:
                                repository.write_shared_text(os.path.join('rules', filename), rule_content)
                                logger.info(f"Cached rule content to: {filepath}")
                            except Exception as cache_error:
                                logger.warning(f"Failed to cache rule content to {filepath}: {cache_error}")

                        return rule_content
                except Exception as e:
                    logger.warning("Failed to fetch rule from library URL %s: %s", safe_url_for_log(url), safe_exception_details(e))

    # 3. 如果规则仓库没有，尝试从规则集的 url 字段获取
    if not rule_content and allow_network:
        url = rule_item.get('url', '')
        if url:
            if config_data is None:
                config_data = get_config()
            # 如果是相对路径，拼接 server_domain
            if url.startswith('/'):
                server_domain = config_data.get('system_config', {}).get('server_domain', '')
                if server_domain:
                    url = f"{server_domain}{url}"

            try:
                response = request_rule(url, timeout=30, config_data=config_data)
                if response.status_code == 200:
                    rule_content = response.text
                    logger.info(f"Fetched rule content from item URL: {safe_url_for_log(url)}")

                    # 保存到本地缓存
                    if filepath:
                        try:
                            repository.write_shared_text(os.path.join('rules', filename), rule_content)
                            logger.info(f"Cached rule content to: {filepath}")
                        except Exception as cache_error:
                            logger.warning(f"Failed to cache rule content to {filepath}: {cache_error}")

                    return rule_content
            except Exception as e:
                logger.warning("Failed to fetch rule from item URL %s: %s", safe_url_for_log(url), safe_exception_details(e))

    return rule_content


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

        # 验证输入格式
        if not is_valid_domain(query) and not is_valid_ip(query):
            return jsonify({'success': False, 'message': '请输入有效的域名或IP地址'}), 400

        matcher = RuleConfigMatcher(
            config_data.get('rule_configs', []), config_data.get('rule_library', []),
            lambda item, library_rule: get_ruleset_content(item, library_rule, config_data=config_data))
        matched = matcher.match(query)
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

        # key: (规则类型, 归一化规则值) -> {'value': 首次出现的原始值, 'items': 出现位置列表}
        occurrences = {}
        rules_checked = 0
        rulesets_checked = 0
        failed_rulesets = []

        # 正则类规则值大小写敏感，不做小写归一
        case_sensitive_types = {'REGEX', 'REGEXP'}

        def add_occurrence(rule_type, rule_value, item):
            normalized = rule_value if rule_type in case_sensitive_types else rule_value.lower()
            entry = occurrences.setdefault((rule_type, normalized), {'value': rule_value, 'items': []})
            entry['items'].append(item)

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
                add_occurrence(rule_type, rule_value, {
                    'source_type': 'rule',
                    'source': '直接配置的规则',
                    'rule_id': rule_item.get('id', ''),
                    'policy': rule_item.get('policy', 'DIRECT'),
                    'priority': index,
                    'line': f'{rule_type},{rule_value}'
                })

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
                    add_occurrence(rule_type, rule_value, {
                        'source_type': 'ruleset',
                        'source': rule_set_name,
                        'rule_id': rule_item.get('id', ''),
                        'policy': policy,
                        'priority': index,
                        'line': line.strip(),
                        'line_no': line_no
                    })
                logger.info('Duplicate scan %s scanned ruleset "%s": %d entries in %.1f ms',
                            scan_id, rule_set_name, source_entries, (time.perf_counter() - source_started) * 1000)

        # 同一规则集内部与跨来源的重复都算重复
        duplicates = []
        for (rule_type, _), entry in occurrences.items():
            items = entry['items']
            if len(items) < 2:
                continue
            duplicates.append({
                'rule_type': rule_type,
                'value': entry['value'],  # 首次出现的原始值，保留大小写用于展示
                'count': len(items),
                'policy_conflict': len({i['policy'] for i in items}) > 1,
                'occurrences': items
            })

        # 策略冲突的排前面，其余按出现次数降序、优先级升序
        duplicates.sort(key=lambda d: (not d['policy_conflict'], -d['count'], d['occurrences'][0]['priority']))

        elapsed_time = time.perf_counter() - start_time
        logger.info('Duplicate scan %s matched: %d duplicate groups in %.1f ms; preparing response',
                    scan_id, len(duplicates), elapsed_time * 1000)
        return jsonify({
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
