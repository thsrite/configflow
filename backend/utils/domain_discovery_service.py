"""域名发现业务逻辑：设置、默认规则集、写入 / 撤销、主动探测任务与自动化。

所有函数都显式接收 profile_id，路由与后台自动探测共用。
"""

import hashlib
import threading
import uuid
from datetime import datetime, timedelta
from typing import Any, Callable, Dict, Optional

from backend.agents.traffic_store import get_traffic_store
from backend.agents.version import compare_versions
from backend.common.agent_manager import get_agent_manager
from backend.common.config import get_config, update_config_transaction, update_shared_config_transaction
from backend.common.config_repository import ProfileValidationError
from backend.utils.domain_discovery import TARGETS, format_ruleset_line, summarize
from backend.utils.domain_probe import (
    CONTROL_TARGETS, get_probe_store, judge, latest_probes, path_healthy, probe_url,
)
from backend.utils.logger import get_logger
from backend.utils.rule_matcher import RuleConfigMatcher, is_valid_domain, is_valid_ip, match_domain, parse_rule_line
from backend.utils.rule_utils import save_rule_to_local
from backend.utils.url_utils import safe_exception_details

logger = get_logger(__name__)

# 可以作为写入目标的规则集：内容型，且格式能表达域名规则
WRITABLE_BEHAVIORS = ('classical', 'domain')
DEFAULT_RULESET_NAMES = {'direct': '域名发现-直连', 'proxy': '域名发现-代理'}
TARGET_LABELS = {'direct': '直连', 'proxy': '代理'}
# 支持流量上报 / 主动探测的最低 Agent 版本
MIN_AGENT_VERSION = '1.4.0-go'
MIN_PROBE_AGENT_VERSION = '1.5.0-go'
MIN_REGION_AGENT_VERSION = '1.6.0-go'
MAX_APPLY_ITEMS = 500
MAX_IGNORED = 2000
MAX_HISTORY = 1000
CONFIDENCE_CHOICES = (80, 90, 95)

PROBE_BATCH = 18            # 每批再加 2 个对照目标，正好是 Agent 的 20 个上限
PROBE_HOSTS_PER_DOMAIN = 3
PROBE_ATTEMPTS = 3
PROBE_TIMEOUT_MS = 5000
PROBE_MAX_VALUES = 100

AUTO_PROBE_BATCH = 20
AUTO_REPROBE_AFTER = timedelta(days=3)
RECHECK_AFTER = timedelta(days=30)
RECHECK_BATCH = 10

_SETTING_DEFAULTS = {
    'direct_ruleset': None,
    'proxy_ruleset': None,
    'ignored': [],
    'auto_probe': False,
    'auto_apply': False,
    'auto_apply_min_confidence': 90,
    'history': [],
    'region_check_enabled': False,
    'region_probe_port': 17999,
    'regional_rulesets': {},
}
POLICY_TARGET_PREFIX = 'policy:'


class ProbeError(Exception):
    """探测无法开始或整体失败，消息可直接展示给用户。"""


class ProbeBusy(ProbeError):
    def __init__(self, job_id):
        super().__init__('已有探测任务在运行')
        self.job_id = job_id


# ---------- 设置与规则集 ----------

def read_settings(config) -> Dict[str, Any]:
    raw = config.get('domain_discovery') or {}
    settings = {key: raw.get(key, default) for key, default in _SETTING_DEFAULTS.items()}
    settings['ignored'] = [value for value in settings['ignored'] or [] if isinstance(value, str)]
    settings['history'] = [entry for entry in settings['history'] or [] if isinstance(entry, dict)]
    settings['auto_probe'] = bool(settings['auto_probe'])
    settings['auto_apply'] = bool(settings['auto_apply'])
    if settings['auto_apply_min_confidence'] not in CONFIDENCE_CHOICES:
        settings['auto_apply_min_confidence'] = 90
    settings['region_check_enabled'] = bool(settings['region_check_enabled'])
    if not isinstance(settings['region_probe_port'], int) or not 1024 <= settings['region_probe_port'] <= 65535:
        settings['region_probe_port'] = 17999
    if not isinstance(settings['regional_rulesets'], dict):
        settings['regional_rulesets'] = {}
    return settings


def target_label(target):
    if target.startswith(POLICY_TARGET_PREFIX):
        return f'「{target[len(POLICY_TARGET_PREFIX):]}」'
    return TARGET_LABELS.get(target, target)


def _target_ruleset_id(settings, target):
    if target.startswith(POLICY_TARGET_PREFIX):
        return settings['regional_rulesets'].get(target[len(POLICY_TARGET_PREFIX):])
    return settings.get(f'{target}_ruleset')


def _mutate_settings(profile_id, mutate: Callable[[Dict[str, Any]], None]):
    def update(profile):
        settings = read_settings(profile)
        mutate(settings)
        profile['domain_discovery'] = settings

    update_config_transaction(update, profile_id)


def ruleset_problem(library_item):
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


def ruleset_info(config, library_id):
    if not library_id:
        return None
    library_item = next((item for item in config.get('rule_library', []) if item.get('id') == library_id), None)
    references = _references(config, library_id)
    return {
        'id': library_id,
        'name': library_item.get('name') if library_item else None,
        'behavior': library_item.get('behavior', 'classical') if library_item else None,
        'problem': ruleset_problem(library_item),
        'references': references,
        'active': any(reference['enabled'] and not reference['after_match'] for reference in references),
    }


def policy_names(config):
    return [group.get('name') for group in config.get('proxy_groups', []) if group.get('name')]


def settings_payload(profile_id):
    config = get_config(profile_id)
    settings = read_settings(config)
    return {
        'direct': ruleset_info(config, settings['direct_ruleset']),
        'proxy': ruleset_info(config, settings['proxy_ruleset']),
        'ignored': settings['ignored'],
        'auto_probe': settings['auto_probe'],
        'auto_apply': settings['auto_apply'],
        'auto_apply_min_confidence': settings['auto_apply_min_confidence'],
        'confidence_choices': list(CONFIDENCE_CHOICES),
        'probe_path': proxy_probe_path(config, settings),
        'region_check_enabled': settings['region_check_enabled'],
        'region_probe_port': settings['region_probe_port'],
        'candidates': [
            {'id': item.get('id'), 'name': item.get('name'), 'behavior': item.get('behavior', 'classical')}
            for item in config.get('rule_library', []) if ruleset_problem(item) is None
        ],
        'policies': policy_names(config),
    }


def update_settings(profile_id, payload):
    if not isinstance(payload, dict):
        raise ProfileValidationError('请求必须是 JSON 对象')
    config = get_config(profile_id)
    library = {item.get('id'): item for item in config.get('rule_library', [])}
    changes = {}
    for target in TARGETS:
        key = f'{target}_ruleset'
        if key not in payload:
            continue
        value = payload[key] or None
        if value is not None and ruleset_problem(library.get(value)) is not None:
            raise ProfileValidationError('默认规则集必须是内容型、classical 或 domain 格式的规则库条目')
        changes[key] = value
    for key in ('auto_probe', 'auto_apply'):
        if key in payload:
            if not isinstance(payload[key], bool):
                raise ProfileValidationError(f'{key} 必须是布尔值')
            changes[key] = payload[key]
    if 'auto_apply_min_confidence' in payload:
        if payload['auto_apply_min_confidence'] not in CONFIDENCE_CHOICES:
            raise ProfileValidationError('自动采纳阈值只能是 80、90 或 95')
        changes['auto_apply_min_confidence'] = payload['auto_apply_min_confidence']
    if 'region_check_enabled' in payload:
        if not isinstance(payload['region_check_enabled'], bool):
            raise ProfileValidationError('region_check_enabled 必须是布尔值')
        changes['region_check_enabled'] = payload['region_check_enabled']
    if 'region_probe_port' in payload:
        port = payload['region_probe_port']
        if not isinstance(port, int) or isinstance(port, bool) or not 1024 <= port <= 65535:
            raise ProfileValidationError('区域检测入口端口必须在 1024-65535 之间')
        if port in _custom_config_ports(config):
            raise ProfileValidationError(f'端口 {port} 已被 Mihomo 配置中的入站占用')
        changes['region_probe_port'] = port
    _mutate_settings(profile_id, lambda settings: settings.update(changes))
    return settings_payload(profile_id)


def _custom_config_ports(config):
    import yaml
    from backend.converters.mihomo import mihomo_used_ports
    try:
        parsed = yaml.safe_load((config.get('mihomo') or {}).get('custom_config') or '') or {}
    except yaml.YAMLError:
        return set()
    return mihomo_used_ports(parsed) if isinstance(parsed, dict) else set()


def _unique_name(existing, base):
    name, counter = base, 2
    while name in existing:
        name, counter = f'{base}-{counter}', counter + 1
    return name


def init_rulesets(profile_id, targets, proxy_policy):
    """一键创建默认的直连 / 代理规则集，并引用到兜底规则之前。"""
    targets = targets or list(TARGETS)
    if not isinstance(targets, list) or any(target not in TARGETS for target in targets):
        raise ProfileValidationError('targets 只能包含 direct / proxy')
    config = get_config(profile_id)
    if 'proxy' in targets and proxy_policy not in policy_names(config):
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
        rules[position:position] = [{
            'id': f'ruleset_{uuid.uuid4()}',
            'itemType': 'ruleset',
            'library_rule_id': created[target]['id'],
            'policy': policies[target],
            'enabled': True,
            'no_resolve': False,
        } for target in targets]
        settings = read_settings(profile)
        settings.update({f'{target}_ruleset': created[target]['id'] for target in targets})
        profile['domain_discovery'] = settings

    update_config_transaction(reference, profile_id)
    return settings_payload(profile_id)


def provider_revisions(profile_id):
    """默认规则集的内容摘要，Agent 发现变化后让 mihomo 立即刷新对应的 rule-provider。"""
    config = get_config(profile_id)
    settings = read_settings(config)
    library = {item.get('id'): item for item in config.get('rule_library', [])}
    result = {}
    ruleset_ids = [settings[f'{target}_ruleset'] for target in TARGETS] + list(settings['regional_rulesets'].values())
    for ruleset_id in ruleset_ids:
        info = ruleset_info(config, ruleset_id)
        if not info or info['problem'] or not info['active']:
            continue
        result[info['name']] = hashlib.md5(_served_rule_content(library[info['id']])).hexdigest()
    return result


def _served_rule_content(library_item) -> bytes:
    """/rules/local 实际下发给 Mihomo 的内容。

    摘要必须按缓存文件算：配置先提交、缓存文件后写入，按配置算会让 Agent 在文件更新前就刷新，
    Mihomo 拉到旧内容后 Agent 又以为已经刷新过，新规则要等 rule-provider 的 interval 才生效。
    """
    import os
    from backend.utils.rule_utils import get_rules_dir, sanitize_rule_name
    path = os.path.join(get_rules_dir(), f"{sanitize_rule_name(library_item.get('name', ''))}.list")
    try:
        with open(path, 'rb') as handle:
            return handle.read()
    except OSError:
        return (library_item.get('content') or '').encode('utf-8')


def proxy_probe_path(config, settings=None) -> Optional[str]:
    """探测代理路径用默认代理规则集实际指向的策略组。"""
    settings = settings or read_settings(config)
    info = ruleset_info(config, settings['proxy_ruleset'])
    if not info:
        return None
    for reference in info['references']:
        if reference['enabled'] and not reference['after_match'] and reference['policy'] not in ('DIRECT', 'REJECT'):
            return reference['policy']
    return None


# ---------- Agent ----------

def agent_supported(version, minimum=MIN_AGENT_VERSION):
    try:
        return bool(version) and compare_versions(version, minimum) >= 0
    except (TypeError, ValueError):
        return False


def profile_agents(profile_id):
    return [agent for agent in get_agent_manager().get_all_agents()
            if agent.get('profile_id', 'default') == profile_id and agent.get('service_type') == 'mihomo']


def _probe_agents(profile_id):
    agents = [agent for agent in profile_agents(profile_id)
              if agent.get('domain_discovery_enabled') and agent_supported(agent.get('version'), MIN_PROBE_AGENT_VERSION)]
    # 最近有心跳的优先
    return sorted(agents, key=lambda agent: agent.get('last_heartbeat') or '', reverse=True)


# ---------- 列表 ----------

def _probe_view(entry):
    if not entry:
        return None
    return {key: entry.get(key) for key in (
        'host', 'url', 'checked_at', 'verdict', 'target', 'confidence', 'reasons',
        'direct', 'proxy', 'proxy_path')}


def load_domains(profile_id, *, days=7, view='uncovered', agent_id=None, include_ignored=False):
    from backend.routes.rules import get_ruleset_content
    from backend.utils.domain_probe import domain_suggestion

    config = get_config(profile_id)
    settings = read_settings(config)
    agents = profile_agents(profile_id)
    store = get_traffic_store()
    reports = [(agent, store.load(agent['id'])) for agent in agents]
    matcher = RuleConfigMatcher(
        config.get('rule_configs', []), config.get('rule_library', []),
        lambda item, library_rule: get_ruleset_content(item, library_rule, allow_network=False),
    )
    result = summarize(reports, days=days, ignored=() if include_ignored else settings['ignored'],
                       match_rule=matcher.match, view=view, agent_id=agent_id)
    probes = latest_probes([agent['id'] for agent in agents])
    for item in result['items']:
        for host in item['hosts']:
            host['probe'] = _probe_view(probes.get(host['host']))
        item['suggestion'] = domain_suggestion(item['hosts'])
    by_id = {agent['id']: agent for agent in agents}
    for agent in result['agents']:
        source = by_id[agent['id']]
        agent['enabled'] = bool(source.get('domain_discovery_enabled'))
        agent['version'] = source.get('version', '')
        agent['supported'] = agent_supported(agent['version'])
        agent['probe_supported'] = agent_supported(agent['version'], MIN_PROBE_AGENT_VERSION)
    result['min_agent_version'] = MIN_AGENT_VERSION
    result['min_probe_agent_version'] = MIN_PROBE_AGENT_VERSION
    result['probe_job'] = public_job(running_job(profile_id))
    return result


# ---------- 写入、撤销、忽略 ----------

def clean_domain(value):
    if not isinstance(value, str):
        return None
    value = value.strip().lower().rstrip('.')
    if not value or is_valid_ip(value) or not is_valid_domain(value) or '.' not in value:
        return None
    return value


def parse_apply_items(items):
    from backend.utils.domain_discovery import APPLY_RULE_TYPES
    if not isinstance(items, list) or not items or len(items) > MAX_APPLY_ITEMS:
        raise ProfileValidationError(f'items 必须是 1-{MAX_APPLY_ITEMS} 条')
    entries = []
    for item in items:
        if not isinstance(item, dict):
            raise ProfileValidationError('items 中每一项必须是对象')
        value = clean_domain(item.get('value'))
        rule_type = item.get('rule_type', 'DOMAIN-SUFFIX')
        target = item.get('target')
        if value is None or rule_type not in APPLY_RULE_TYPES or target not in TARGETS:
            raise ProfileValidationError('每一项需要有效域名、DOMAIN-SUFFIX/DOMAIN 类型和 direct/proxy 目标')
        entries.append({'value': value, 'rule_type': rule_type, 'target': target})
    return entries


def _covered(existing, rule_type, value):
    return any(
        (existing_type, existing_value.lower()) == (rule_type, value)
        or (existing_type == 'DOMAIN-SUFFIX' and match_domain(value, existing_type, existing_value))
        for existing_type, existing_value in existing
    )


def apply_domains(profile_id, entries, *, source='manual', evidence=None, now=None):
    """把域名追加进默认规则集，并记录到历史。evidence: {value: 探测建议}。"""
    now = now or datetime.now()
    evidence = evidence or {}
    config = get_config(profile_id)
    settings = read_settings(config)
    infos = {}
    for target in {entry['target'] for entry in entries}:
        info = ruleset_info(config, _target_ruleset_id(settings, target))
        if info is None or info['problem']:
            raise ProfileValidationError(f'还没有设置可写入的{target_label(target)}规则集')
        infos[target] = info

    added, skipped, updated_items = [], [], []

    def append_lines(shared):
        library = {item.get('id'): item for item in shared.get('rule_library', [])}
        for target, info in infos.items():
            item = library.get(info['id'])
            if ruleset_problem(item) is not None:
                raise ProfileValidationError('默认规则集已被修改，请刷新后重试')
            content = item.get('content', '') or ''
            existing = [parsed for parsed in (parse_rule_line(line) for line in content.splitlines()) if parsed]
            new_lines = []
            for entry in (entry for entry in entries if entry['target'] == target):
                if _covered(existing, entry['rule_type'], entry['value']):
                    skipped.append({'value': entry['value'], 'target': target, 'reason': 'exists'})
                    continue
                existing.append((entry['rule_type'], entry['value']))
                new_lines.append(format_ruleset_line(item.get('behavior', 'classical'), entry['rule_type'], entry['value']))
                added.append({**entry, 'ruleset': item.get('name'), 'ruleset_id': item.get('id')})
            if new_lines:
                prefix = content if not content or content.endswith('\n') else content + '\n'
                item['content'] = prefix + '\n'.join(new_lines) + '\n'
                updated_items.append(item)

    update_shared_config_transaction(append_lines)
    for item in updated_items:
        save_rule_to_local(item)

    if added:
        timestamp = now.isoformat(timespec='seconds')

        def record(settings):
            keys = {(entry['value'], entry['target']) for entry in added}
            history = [entry for entry in settings['history'] if (entry.get('value'), entry.get('target')) not in keys]
            for entry in added:
                proof = evidence.get(entry['value']) or {}
                history.append({
                    'value': entry['value'],
                    'rule_type': entry['rule_type'],
                    'target': entry['target'],
                    'ruleset_id': entry['ruleset_id'],
                    'source': source,
                    'applied_at': timestamp,
                    'verdict': proof.get('verdict'),
                    'confidence': proof.get('confidence'),
                    'recheck': None,
                })
            settings['history'] = history[-MAX_HISTORY:]

        _mutate_settings(profile_id, record)

    warnings = [
        f'规则集「{info["name"]}」没有在当前配置中启用（或位于兜底规则之后），写入的域名不会生效'
        for info in infos.values() if not info['active']
    ]
    return {'added': added, 'skipped': skipped, 'warnings': warnings}


def undo_domain(profile_id, value, target):
    """从默认规则集中删除该域名对应的行，并删除历史记录。"""
    value = clean_domain(value)
    if value is None or not isinstance(target, str) or not (target in TARGETS or target.startswith(POLICY_TARGET_PREFIX)):
        raise ProfileValidationError('需要有效域名和规则集目标')
    config = get_config(profile_id)
    settings = read_settings(config)
    history = [entry for entry in settings['history'] if entry.get('value') == value and entry.get('target') == target]
    ruleset_id = (history[0].get('ruleset_id') if history else None) or _target_ruleset_id(settings, target)
    removed_lines, updated_items = [], []

    def remove_lines(shared):
        item = next((item for item in shared.get('rule_library', []) if item.get('id') == ruleset_id), None)
        if item is None or item.get('source_type') != 'content':
            return
        kept = []
        for line in (item.get('content') or '').splitlines():
            parsed = parse_rule_line(line)
            if parsed and parsed[0] in ('DOMAIN', 'DOMAIN-SUFFIX') and parsed[1].lower() == value:
                removed_lines.append(line.strip())
                continue
            kept.append(line)
        if removed_lines:
            item['content'] = '\n'.join(kept) + ('\n' if kept else '')
            updated_items.append(item)

    update_shared_config_transaction(remove_lines)
    for item in updated_items:
        save_rule_to_local(item)
    _mutate_settings(profile_id, lambda settings: settings.update(history=[
        entry for entry in settings['history']
        if not (entry.get('value') == value and entry.get('target') == target)]))
    return {'removed': removed_lines}


def update_ignored(profile_id, values, add):
    cleaned = [clean_domain(value) for value in values] if isinstance(values, list) else [None]
    if not cleaned or None in cleaned:
        raise ProfileValidationError('values 必须是有效域名列表')

    def mutate(settings):
        current = settings['ignored']
        if add:
            current.extend(value for value in cleaned if value not in current)
            if len(current) > MAX_IGNORED:
                raise ProfileValidationError(f'忽略列表最多 {MAX_IGNORED} 条')
        else:
            current[:] = [value for value in current if value not in cleaned]

    _mutate_settings(profile_id, mutate)
    return read_settings(get_config(profile_id))['ignored']


def history_payload(profile_id):
    config = get_config(profile_id)
    settings = read_settings(config)
    library = {item.get('id'): item.get('name') for item in config.get('rule_library', [])}
    entries = [{**entry, 'ruleset': library.get(entry.get('ruleset_id'))} for entry in settings['history']]
    entries.sort(key=lambda entry: entry.get('applied_at') or '', reverse=True)
    return entries


# ---------- 主动探测 ----------

def _probe_plan(profile_id, values):
    """把用户给的主域 / 子域展开成具体的探测 host，并为每个 host 选 Agent。"""
    agents = _probe_agents(profile_id)
    if not agents:
        raise ProbeError(f'没有可用于探测的 Agent：需要开启域名发现且 Agent 版本不低于 {MIN_PROBE_AGENT_VERSION}')
    store = get_traffic_store()
    summary = summarize([(agent, store.load(agent['id'])) for agent in profile_agents(profile_id)],
                        days=7, view='all', ignored=())
    domains = {item['domain']: item for item in summary['items']}
    hosts = {host['host']: host for item in summary['items'] for host in item['hosts']}
    agent_ids = {agent['id'] for agent in agents}
    plan = {}
    for value in values:
        if value in domains:
            candidates = domains[value]['hosts'][:PROBE_HOSTS_PER_DOMAIN]
        elif value in hosts:
            candidates = [hosts[value]]
        else:
            candidates = [{'host': value, 'port': 443, 'agent_ids': [], 'failing': False}]
        for host in candidates:
            agent_id = next((agent for agent in host.get('agent_ids', []) if agent in agent_ids), agents[0]['id'])
            plan.setdefault(host['host'], {
                'host': host['host'],
                'url': probe_url(host['host'], host.get('port')),
                'agent_id': agent_id,
                'failing': bool(host.get('failing')),
            })
    return {agent['id']: agent for agent in agents}, list(plan.values())


def run_probes(profile_id, values, progress: Optional[Callable[[int, int], None]] = None, now=None):
    """同步执行探测并保存结果，返回 {host: 结果}。"""
    config = get_config(profile_id)
    proxy_path = proxy_probe_path(config)
    if not proxy_path:
        raise ProbeError('请先设置默认代理规则集，并在当前配置中启用（探测会经它指向的策略组访问）')
    agents, plan = _probe_plan(profile_id, values)
    total, done = len(plan), 0
    if progress:
        progress(done, total)
    results, errors = {}, []
    store = get_probe_store()
    manager = get_agent_manager()
    for agent_id in {target['agent_id'] for target in plan}:
        targets = [target for target in plan if target['agent_id'] == agent_id]
        for start in range(0, len(targets), PROBE_BATCH):
            batch = targets[start:start + PROBE_BATCH]
            payload = {
                'targets': [CONTROL_TARGETS['direct'], CONTROL_TARGETS['proxy']]
                           + [{'host': target['host'], 'url': target['url']} for target in batch],
                'paths': ['DIRECT', proxy_path],
                'attempts': PROBE_ATTEMPTS,
                'timeout_ms': PROBE_TIMEOUT_MS,
            }
            try:
                response = manager.probe_domains(agents[agent_id], payload)
            except Exception as error:
                logger.warning('Domain probe failed on agent %s: %s', agent_id, safe_exception_details(error))
                errors.append({'agent_id': agent_id, 'message': str(error)})
                done += len(batch)
                if progress:
                    progress(done, total)
                continue
            rows = response.get('results') or []
            if len(rows) != len(payload['targets']):
                errors.append({'agent_id': agent_id, 'message': 'Agent 返回的结果数量不符'})
                done += len(batch)
                if progress:
                    progress(done, total)
                continue
            direct_healthy = path_healthy(rows[0]['paths'].get('DIRECT'))
            proxy_healthy = path_healthy(rows[1]['paths'].get(proxy_path))
            timestamp = (now or datetime.now()).isoformat(timespec='seconds')
            saved = {}
            for target, row in zip(batch, rows[2:]):
                direct, proxy = row['paths'].get('DIRECT'), row['paths'].get(proxy_path)
                verdict = judge(direct, proxy, passive_failing=target['failing'],
                                direct_healthy=direct_healthy, proxy_healthy=proxy_healthy)
                saved[target['host']] = {
                    'host': target['host'], 'url': target['url'], 'checked_at': timestamp,
                    'proxy_path': proxy_path, 'direct': direct, 'proxy': proxy, **verdict,
                }
            store.save(agent_id, saved)
            results.update(saved)
            done += len(batch)
            if progress:
                progress(done, total)
    return {'results': results, 'errors': errors, 'total': total}


_JOBS: Dict[str, Dict[str, Any]] = {}
_JOBS_LOCK = threading.Lock()
_MAX_FINISHED_JOBS = 20


def running_job(profile_id=None):
    with _JOBS_LOCK:
        return next((job for job in _JOBS.values() if job['status'] == 'running'
                     and (profile_id is None or job['profile_id'] == profile_id)), None)


def public_job(job):
    if not job:
        return None
    return {key: job.get(key) for key in ('id', 'kind', 'status', 'total', 'done', 'errors', 'message', 'started_at', 'finished_at')}


def get_job(job_id):
    with _JOBS_LOCK:
        return _JOBS.get(job_id)


def _clean_values(values):
    cleaned = [clean_domain(value) for value in values] if isinstance(values, list) else [None]
    if not cleaned or None in cleaned or len(cleaned) > PROBE_MAX_VALUES:
        raise ProfileValidationError(f'domains 必须是 1-{PROBE_MAX_VALUES} 个有效域名')
    return list(dict.fromkeys(cleaned))


def start_probe_job(profile_id, values, *, wait=False):
    """后台执行探测任务；同一时间只运行一个任务（探测会占用代理带宽）。"""
    values = _clean_values(values)
    return _start_job(profile_id, 'probe', lambda progress: run_probes(profile_id, values, progress), wait=wait)


def _start_job(profile_id, kind, run: Callable[[Callable[[int, int], None]], Dict[str, Any]], *, wait=False):
    """探测与区域检测共用：同一时间只运行一个任务。"""
    with _JOBS_LOCK:
        busy = next((job for job in _JOBS.values() if job['status'] == 'running'), None)
        if busy:
            raise ProbeBusy(busy['id'])
        job = {
            'id': uuid.uuid4().hex, 'profile_id': profile_id, 'kind': kind, 'status': 'running', 'total': 0, 'done': 0,
            'errors': [], 'message': '', 'started_at': datetime.now().isoformat(timespec='seconds'),
            'finished_at': None, 'results': {},
        }
        _JOBS[job['id']] = job
        finished = [key for key, item in _JOBS.items() if item['status'] != 'running']
        for key in finished[:-_MAX_FINISHED_JOBS]:
            del _JOBS[key]

    def progress(done, total):
        job['done'], job['total'] = done, total

    def work():
        from backend.common.config import reset_config_context
        reset_config_context()
        try:
            outcome = run(progress)
            job['errors'] = outcome.get('errors', [])
            job['results'] = outcome.get('results', {})
            job['status'] = 'done'
        except ProbeError as error:
            job['status'], job['message'] = 'failed', str(error)
        except Exception as error:  # 后台线程不能把异常吞掉而不留痕
            logger.error('Domain discovery %s job failed: %s', kind, safe_exception_details(error))
            job['status'], job['message'] = 'failed', '执行失败，请查看服务端日志'
        finally:
            job['finished_at'] = datetime.now().isoformat(timespec='seconds')

    if wait:
        work()
        if job['status'] == 'failed':
            raise ProbeError(job['message'])
    else:
        threading.Thread(target=work, name=f'domain-{kind}-{job["id"][:8]}', daemon=True).start()
    return job


# ---------- 自动探测、自动采纳与复检 ----------

def _recheck_outcome(entry, probe):
    """已加入的条目重新探测后，是否建议移除。"""
    verdict, target = probe.get('verdict'), probe.get('target')
    if verdict == 'unreachable':
        return True, '直连与代理都不通，站点可能已失效'
    if entry['target'] == 'proxy' and target == 'direct' and verdict in ('direct_only', 'both_ok'):
        return True, '直连已经可以正常访问'
    if entry['target'] == 'direct' and verdict == 'needs_proxy':
        return True, '直连已不通，建议改为代理'
    return False, ''


def auto_round(profile_id, now=None):
    """一轮自动化：探测新出现的未覆盖域名、按阈值自动采纳、复检已加入超过 30 天的条目。"""
    now = now or datetime.now()
    config = get_config(profile_id)
    settings = read_settings(config)
    if not settings['auto_probe'] or not proxy_probe_path(config, settings) or not _probe_agents(profile_id):
        return {'probed': 0, 'applied': 0, 'rechecked': 0}

    listing = load_domains(profile_id, days=7, view='uncovered')
    stale = (now - AUTO_REPROBE_AFTER).isoformat(timespec='seconds')
    candidates = [
        item for item in listing['items']
        if 'uncovered' in item['tags'] and any(
            not host.get('probe') or (host['probe'].get('checked_at') or '') < stale
            for host in item['hosts'][:PROBE_HOSTS_PER_DOMAIN])
    ][:AUTO_PROBE_BATCH]

    applied = 0
    if candidates:
        # 与手动探测共用同一把「同时只跑一个任务」的锁
        start_probe_job(profile_id, [item['domain'] for item in candidates], wait=True)
        if settings['auto_apply']:
            refreshed = {item['domain']: item for item in load_domains(profile_id, days=7, view='uncovered')['items']}
            entries, evidence = [], {}
            for item in candidates:
                suggestion = (refreshed.get(item['domain']) or {}).get('suggestion')
                if (suggestion and suggestion.get('target')
                        and suggestion['confidence'] >= settings['auto_apply_min_confidence']):
                    entries.append({'value': item['domain'], 'rule_type': 'DOMAIN-SUFFIX',
                                    'target': suggestion['target']})
                    evidence[item['domain']] = suggestion
            if entries:
                try:
                    applied = len(apply_domains(profile_id, entries, source='auto', evidence=evidence, now=now)['added'])
                except ProfileValidationError as error:
                    logger.warning('Auto apply skipped for profile %s: %s', profile_id, error)

    due = (now - RECHECK_AFTER).isoformat(timespec='seconds')
    history = read_settings(get_config(profile_id))['history']
    to_check = [entry for entry in history
                if entry.get('target') in TARGETS and (entry.get('applied_at') or '') < due
                and (not entry.get('recheck') or (entry['recheck'].get('at') or '') < due)][:RECHECK_BATCH]
    if to_check:
        results = start_probe_job(profile_id, [entry['value'] for entry in to_check], wait=True)['results']
        timestamp = now.isoformat(timespec='seconds')
        outcomes = {}
        for entry in to_check:
            probe = results.get(entry['value']) or next(
                (result for host, result in results.items() if host.endswith('.' + entry['value'])), None)
            if probe:
                suggest_remove, reason = _recheck_outcome(entry, probe)
                outcomes[(entry['value'], entry['target'])] = {
                    'at': timestamp, 'verdict': probe.get('verdict'),
                    'suggest_remove': suggest_remove, 'reason': reason,
                }

        def record(settings):
            for entry in settings['history']:
                outcome = outcomes.get((entry.get('value'), entry.get('target')))
                if outcome:
                    entry['recheck'] = outcome

        _mutate_settings(profile_id, record)
    return {'probed': len(candidates), 'applied': applied, 'rechecked': len(to_check)}


AUTO_INTERVAL_SECONDS = 600
AUTO_FIRST_DELAY_SECONDS = 120
_AUTO_STARTED = False


def start_auto_loop(lock_dir):
    """启动后台自动探测线程；用文件锁保证多进程部署时只有一个进程在跑。"""
    global _AUTO_STARTED
    if _AUTO_STARTED:
        return
    _AUTO_STARTED = True

    def loop():
        import fcntl
        import os
        import time
        from backend.common.config import get_repository, reset_config_context

        os.makedirs(lock_dir, exist_ok=True)
        handle = open(os.path.join(lock_dir, '.auto.lock'), 'w')
        try:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except OSError:
            handle.close()
            return
        time.sleep(AUTO_FIRST_DELAY_SECONDS)
        while True:
            for profile in get_repository().list_profiles():
                reset_config_context()
                try:
                    outcome = auto_round(profile['id'])
                    if any(outcome.values()):
                        logger.info('Domain discovery auto round for %s: %s', profile['id'], outcome)
                except ProbeBusy:
                    pass
                except Exception as error:
                    logger.error('Domain discovery auto round failed for %s: %s',
                                 profile['id'], safe_exception_details(error))
            time.sleep(AUTO_INTERVAL_SECONDS)

    threading.Thread(target=loop, name='domain-discovery-auto', daemon=True).start()



# ---------- 区域限制检测 ----------

def _region_agents(profile_id):
    agents = [agent for agent in profile_agents(profile_id)
              if agent.get('domain_discovery_enabled') and agent_supported(agent.get('version'), MIN_REGION_AGENT_VERSION)]
    return sorted(agents, key=lambda agent: agent.get('last_heartbeat') or '', reverse=True)


def _chunks(items, size):
    return [items[start:start + size] for start in range(0, len(items), size)]


def _region_domain_plan(profile_id, values):
    """每个主域取流量最多的一个子域；没有流量记录的按给定值访问。"""
    store = get_traffic_store()
    summary = summarize([(agent, store.load(agent['id'])) for agent in profile_agents(profile_id)],
                        days=7, view='all', ignored=())
    domains = {item['domain']: item for item in summary['items']}
    hosts = {host['host']: host for item in summary['items'] for host in item['hosts']}
    plan = []
    for value in values:
        host = domains[value]['hosts'][0] if value in domains else hosts.get(value, {'host': value, 'port': 443})
        if all(entry['host'] != host['host'] for entry in plan):
            plan.append({'host': host['host'], 'url': probe_url(host['host'], host.get('port'))})
    return plan


def run_region_check(profile_id, *, services=True, domains=(), progress=None, now=None):
    """经各目标（直连、策略组、地区节点）检测常见服务与指定域名，保存结果。"""
    from backend.utils import region_check as region

    config = get_config(profile_id)
    settings = read_settings(config)
    if not settings['region_check_enabled']:
        raise ProbeError('请先开启区域检测并部署一次配置')
    agents = _region_agents(profile_id)
    if not agents:
        raise ProbeError(f'没有可用于区域检测的 Agent：需要开启域名发现且 Agent 版本不低于 {MIN_REGION_AGENT_VERSION}')
    agent = agents[0]
    manager = get_agent_manager()
    try:
        available = manager.region_targets(agent)
    except Exception as error:
        raise ProbeError(str(error)) from error
    targets = region.pick_targets(available, proxy_probe_path(config, settings))
    domain_plan = _region_domain_plan(profile_id, list(domains))
    requests = ([*region.service_requests()] if services else [region.EXIT_REQUEST]) + [
        region.domain_request(f'domain-{index}', entry['url']) for index, entry in enumerate(domain_plan)]
    # Agent 每次最多 12 个请求：出口检测只放在第一批
    batches = _chunks(requests, 12)
    total = len(targets)
    if progress:
        progress(0, total)
    raw: Dict[str, List[Dict[str, Any]]] = {}
    errors = []
    for index, target in enumerate(targets):
        results = []
        try:
            for batch in batches:
                response = manager.region_check(agent, {'target': target['name'], 'timeout_ms': region.CHECK_TIMEOUT_MS,
                                                        'requests': batch})
                results += response.get('results') or []
        except Exception as error:
            logger.warning('Region check failed on %s via %s: %s', agent['id'], target['name'],
                           safe_exception_details(error))
            errors.append({'target': target['name'], 'message': str(error)})
        raw[target['name']] = results
        if progress:
            progress(index + 1, total)

    timestamp = (now or datetime.now()).isoformat(timespec='seconds')
    target_infos = []
    for target in targets:
        results = raw.get(target['name']) or []
        target_infos.append({**target, 'exit': region.exit_info(results) if results else None,
                             'error': next((item['message'] for item in errors if item['target'] == target['name']), None)})

    def save(data):
        if services:
            data['services'] = {
                'checked_at': timestamp,
                'targets': target_infos,
                'matrix': {name: region.evaluate_services(results) for name, results in raw.items() if results},
            }
        for index, entry in enumerate(domain_plan):
            per_target = {}
            for name, results in raw.items():
                if not results:
                    continue
                result = next((item for item in results if item['id'] == f'domain-{index}'), None)
                per_target[name] = region.classify_domain_result(result, entry['url'])
            data['domains'][entry['host']] = {
                'checked_at': timestamp, 'url': entry['url'], 'targets': per_target,
                'target_infos': target_infos, **region.summarize_domain(per_target),
            }

    region.get_region_store().update(agent['id'], save)
    return {'errors': errors, 'results': {}}


def start_region_job(profile_id, *, services=True, domains=None, wait=False):
    from backend.utils.region_check import MAX_DOMAINS_PER_CHECK
    values = []
    if domains:
        values = _clean_values(domains)
        if len(values) > MAX_DOMAINS_PER_CHECK:
            raise ProfileValidationError(f'一次最多检测 {MAX_DOMAINS_PER_CHECK} 个域名')
    if not services and not values:
        raise ProfileValidationError('请选择要检测的服务或域名')
    return _start_job(profile_id, 'region',
                      lambda progress: run_region_check(profile_id, services=services, domains=values,
                                                        progress=progress), wait=wait)


def region_payload(profile_id):
    from backend.utils import region_check as region

    config = get_config(profile_id)
    settings = read_settings(config)
    agents = profile_agents(profile_id)
    store = region.get_region_store()
    # 取最近有服务检测结果的 Agent
    data_by_agent = [(agent, store.load(agent['id'])) for agent in agents]
    with_services = [(agent, data) for agent, data in data_by_agent if data.get('services')]
    agent, data = max(with_services, key=lambda item: item[1]['services']['checked_at']) if with_services \
        else (data_by_agent[0] if data_by_agent else (None, {'services': None, 'domains': {}}))
    domains = {}
    for _, item in data_by_agent:
        for host, entry in item.get('domains', {}).items():
            if host not in domains or entry['checked_at'] > domains[host]['checked_at']:
                domains[host] = entry
    region_agents = _region_agents(profile_id)
    return {
        'enabled': settings['region_check_enabled'],
        'port': settings['region_probe_port'],
        'services': region.public_services(),
        'result': data.get('services'),
        'agent': {'id': agent['id'], 'name': agent.get('name')} if agent else None,
        'domains': domains,
        'policies': policy_names(config),
        'ready': bool(region_agents),
        'min_agent_version': MIN_REGION_AGENT_VERSION,
        'job': public_job(running_job(profile_id)),
        'regional_rulesets': {
            policy: ruleset_info(config, library_id) for policy, library_id in settings['regional_rulesets'].items()},
    }


def ensure_policy_ruleset(profile_id, policy):
    """为策略组准备「域名发现-<策略组>」规则集：放在默认代理规则集之前（没有时放在 MATCH 之前）。"""
    config = get_config(profile_id)
    if policy not in policy_names(config):
        raise ProfileValidationError('请选择当前配置中的策略组')
    settings = read_settings(config)
    info = ruleset_info(config, settings['regional_rulesets'].get(policy))
    if info and not info['problem'] and info['active']:
        return info['id']

    library_id = info['id'] if info and not info['problem'] else None
    if library_id is None:
        created = {}

        def create(shared):
            library = shared.setdefault('rule_library', [])
            item = {
                'id': f'lib_{uuid.uuid4().hex}',
                'name': _unique_name({entry.get('name') for entry in library}, f'域名发现-{policy}'),
                'source_type': 'content', 'content': '', 'behavior': 'classical', 'enabled': True,
            }
            library.append(item)
            created.update(item)

        update_shared_config_transaction(create)
        save_rule_to_local(created)
        library_id = created['id']

    def reference(profile):
        rules = profile.setdefault('rule_configs', [])
        if not any(item.get('itemType') == 'ruleset' and item.get('library_rule_id') == library_id
                   and item.get('enabled', True) for item in rules):
            # 放在默认代理规则集之前，否则同一域名会先被代理规则集截走
            proxy_ruleset = read_settings(profile)['proxy_ruleset']
            position = next((index for index, item in enumerate(rules)
                             if item.get('itemType') == 'ruleset' and proxy_ruleset
                             and item.get('library_rule_id') == proxy_ruleset), None)
            if position is None:
                position = next((index for index, item in enumerate(rules)
                                 if item.get('itemType') == 'rule' and item.get('rule_type') == 'MATCH'), len(rules))
            rules.insert(position, {
                'id': f'ruleset_{uuid.uuid4()}', 'itemType': 'ruleset', 'library_rule_id': library_id,
                'policy': policy, 'enabled': True, 'no_resolve': False,
            })
        settings = read_settings(profile)
        settings['regional_rulesets'][policy] = library_id
        profile['domain_discovery'] = settings

    update_config_transaction(reference, profile_id)
    return library_id


def route_to_policy(profile_id, kind, value, policy):
    """把服务（内置域名列表）或单个域名指定走某个策略组。"""
    from backend.utils.region_check import SERVICES_BY_ID

    if kind == 'service':
        service = SERVICES_BY_ID.get(value)
        if service is None:
            raise ProfileValidationError('未知的服务')
        values = service['domains']
    elif kind == 'domain':
        cleaned = clean_domain(value)
        if cleaned is None:
            raise ProfileValidationError('需要有效域名')
        values = [cleaned]
    else:
        raise ProfileValidationError('kind 只能是 service 或 domain')
    ensure_policy_ruleset(profile_id, policy)
    target = f'{POLICY_TARGET_PREFIX}{policy}'
    entries = [{'value': item, 'rule_type': 'DOMAIN-SUFFIX', 'target': target} for item in values]
    return apply_domains(profile_id, entries, source='region')
