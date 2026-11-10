"""Opt-in Mihomo -> OxiDNS publications, recovered independently of the browser.

Only confirmed device contents are a source. A child deployment is reserved and
persisted before submission; ambiguous responses are reconciled by that same ID.
The OxiDNS draft is updated only after success, with a compare-and-swap guard.
"""
from contextlib import contextmanager
from copy import deepcopy
from datetime import datetime, timezone
import fcntl
import hashlib
import ipaddress
import json
import logging
import re
import threading
import uuid
from urllib.parse import urlsplit

import requests
import yaml

from backend.agents.deployment_bundle import (
    deployment_content_digests, deployment_content_fingerprint, render_deployment_config,
)
from backend.agents.diagnostics import failure_text
from backend.agents.idle_rounds import IdleRounds
from backend.agents.operation_events import append_event
from backend.common.oxidns_editor import prepare_editor
from backend.common.oxidns_native import load_native_yaml

_TERMINAL = {'succeeded', 'failed', 'rolled_back'}
_RUNNING = {'preparing', 'publishing'}
_logger = logging.getLogger(__name__)
_loops = set()
_loop_guard = threading.Lock()


class FollowError(ValueError):
    pass


def _now():
    return datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')


def _digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False).encode()).hexdigest()


def settings_digest(config):
    """Identify the OxiDNS draft a publication was prepared from."""
    return _digest(config.get('oxidns', {}))


@contextmanager
def _job_lock(manager):
    # Includes binding edits, retry and the whole preparation/submission window.
    # flock also excludes separate Gunicorn workers and releases on process exit.
    path = manager.repository.data_dir / '.dns-follow.lock'
    with open(path, 'a+b') as handle:
        try:
            fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise FollowError('DNS 联动正在处理，请稍后重试') from None
        yield


def _profile(manager, agent):
    return agent.get('profile_id') or manager.repository.default_profile_id()


def _agent(manager, agent_id, service):
    agent = manager.get_agent_by_id(agent_id)
    if not agent or agent.get('service_type') != service or not agent.get('enabled', True):
        raise FollowError(f'{service} Agent 不存在、已停用或类型不符')
    return agent


def _document(config, base_url=''):
    settings = prepare_editor(config.get('oxidns', {}), config, base_url)
    return settings, load_native_yaml(settings['native_config'])


def _entries(document):
    plugins = document.get('plugins', [])
    tags = [item.get('tag') for item in plugins if isinstance(item, dict)]
    for plugin in plugins:
        if not isinstance(plugin, dict) or plugin.get('type') != 'forward':
            continue
        tag = plugin.get('tag')
        # Rule DNS helpers hold a copy pinned by a rule; following would rewrite it.
        if not isinstance(tag, str) or not tag or tags.count(tag) != 1 or tag.startswith('cf_dns_'):
            continue
        args = plugin.get('args') or {}
        if not isinstance(args, dict):
            continue
        upstreams = args.get('upstreams', [])
        if not isinstance(upstreams, list):
            continue
        for index, entry in enumerate(upstreams):
            if isinstance(entry, dict) and isinstance(entry.get('addr'), str):
                yield tag, index, entry


def _scheme(address):
    # Mihomo dns.listen supplies plain UDP/TCP DNS, not DoH/DoT credentials.
    parsed = urlsplit(address if '://' in address else 'udp://' + address)
    if (parsed.scheme not in ('udp', 'tcp') or not parsed.hostname or parsed.username or parsed.password
            or parsed.path not in ('', '/') or parsed.query or parsed.fragment):
        raise FollowError('仅普通 UDP / TCP DNS 上游可以跟随 Mihomo')
    return parsed.scheme


def _entry(document, binding):
    for tag, index, entry in _entries(document):
        if tag == binding['upstream_tag'] and index == binding['upstream_index']:
            return entry
    raise FollowError('绑定的 OxiDNS 上游已移除，请重新选择绑定')


def _pinned_by_rule(config, settings, address):
    """1-based position of a rule pinned to address, active or parked, else None."""
    pinned = set()
    for key in ('rule_actions', 'rule_dns_parked'):
        values = settings.get(key)
        for identity, action in values.items() if isinstance(values, dict) else ():
            upstream = action.get('upstream') if isinstance(action, dict) and action.get('action') == 'upstream' else None
            if isinstance(upstream, dict) and upstream.get('addr') == address:
                pinned.add(identity)
    return next((index for index, item in enumerate(config.get('rule_configs') or [], 1)
                 if isinstance(item, dict) and item.get('id') in pinned), None)


def _network_host(value):
    if not isinstance(value, str) or not value or len(value) > 253 or value != value.strip():
        raise FollowError('请填写 OxiDNS 可以访问的 Mihomo IP 或主机名，不含协议和端口')
    try:
        ip = ipaddress.ip_address(value)
    except ValueError:
        if (not re.fullmatch(r'(?=.{1,253}\Z)[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?', value)
                or any(not label or len(label) > 63 or label.startswith('-') or label.endswith('-') for label in value.split('.'))
                or value.lower() == 'localhost'):
            raise FollowError('Mihomo 地址格式无效；请填写 IP 或主机名，不含协议和端口') from None
    else:
        if ip.is_loopback or ip.is_unspecified or ip.is_multicast:
            raise FollowError('跨设备连接必须使用可访问的地址，不能使用回环或通配地址')
    return value


def _suggested_network_host(agent):
    # Management addresses are a starting point, not proof of DNS reachability.
    host = agent.get('host')
    try:
        if isinstance(host, str) and host.startswith('[') and host.endswith(']'):
            host = str(ipaddress.IPv6Address(host[1:-1]))
        return _network_host(host)
    except ValueError:
        return ''


def _request(manager, agent, path, **kwargs):
    try:
        result = manager._deployment_request(agent, 'get', path, **kwargs)
    except requests.RequestException:
        raise FollowError(f'{agent.get("name", "Agent")} 无法连接，请检查网络后重试') from None
    if not result.get('success') or result.get('http_status'):
        message = result.get('message') or '请升级 Agent 至 1.8.24-go 或更高版本后重试'
        if result.get('http_status') == 404:
            message = '请升级 Agent 至 1.8.24-go 或更高版本后重试'
        raise FollowError(f'{agent.get("name", "Agent")}：{message}')
    return result


def _same_namespace(manager, target, source):
    peer = _request(manager, target, 'dns/peer')
    if peer.get('peer_agent_id') != source['id'] or peer.get('service_type') != 'mihomo':
        raise FollowError('无法确认这两个 Agent 共用网络空间；请选择网络地址连接')


def options(manager, target_id):
    target = _agent(manager, target_id, 'oxidns')
    config = manager.repository.get_compat_config(_profile(manager, target))
    _, document = _document(config, (target.get('applied_content') or {}).get('base_url', ''))
    upstreams = []
    for tag, index, entry in _entries(document):
        try:
            _scheme(entry['addr'])
        except (ValueError, TypeError):
            continue
        upstreams.append({'tag': tag, 'index': index, 'address': failure_text(entry['addr'])})
    return {'success': True, 'binding': target.get('dns_follow'), 'state': target.get('dns_follow_state'),
            'sources': [{'id': agent['id'], 'name': agent.get('name', agent['id']),
                         'suggested_address': _suggested_network_host(agent)}
                        for agent in manager.get_all_agents()
                        if agent.get('service_type') == 'mihomo' and agent.get('enabled', True)],
            'upstreams': upstreams}


def _mutable(target):
    from .oxidns_ha_publish import active
    if active(target):
        raise FollowError('DNS 主备配置正在同步，请等待两台发布完成后修改联动')
    if (target.get('dns_follow_state') or {}).get('status') in _RUNNING:
        raise FollowError('OxiDNS 联动发布尚未确认结束，请等待结果后再修改绑定')


def configure(manager, target_id, payload):
    with _job_lock(manager):
        if not isinstance(payload, dict) or set(payload) - {'source_agent_id', 'upstream_tag', 'upstream_index', 'connection', 'address'}:
            raise FollowError('DNS 绑定参数无效')
        target = _agent(manager, target_id, 'oxidns')
        _mutable(target)
        source = _agent(manager, payload.get('source_agent_id'), 'mihomo')
        if (not isinstance(payload.get('upstream_tag'), str)
                or type(payload.get('upstream_index')) is not int or payload['upstream_index'] < 0
                or payload.get('connection') not in ('auto', 'network')):
            raise FollowError('请选择上游和连接方式')
        binding = {key: payload[key] for key in ('source_agent_id', 'upstream_tag', 'upstream_index', 'connection')}
        if (target.get('oxidns_ha') or {}).get('enabled') is True and binding['connection'] != 'network':
            raise FollowError('DNS 主备必须通过同一个 Mihomo 网络地址联动，请选择网络地址连接')
        if binding['connection'] == 'network':
            binding['address'] = _network_host(payload.get('address'))
        else:
            _same_namespace(manager, target, source)
        profile_id = _profile(manager, target)
        config = manager.repository.get_compat_config(profile_id)
        settings, document = _document(config, (target.get('applied_content') or {}).get('base_url', ''))
        entry = _entry(document, binding)
        _scheme(entry['addr'])
        # Following would leave the rule's pinned copy behind on the old address.
        index = _pinned_by_rule(config, settings, entry['addr'])
        if index is not None:
            raise FollowError(f'该上游已被策略规则 #{index} 固定为指定上游，请先改选后再绑定')
        binding.update(id=uuid.uuid4().hex, source_profile_id=_profile(manager, source), target_profile_id=profile_id,
                       upstream_signature=_digest(entry), after_deployment_id=(source.get('latest_deployment') or {}).get('deployment_id'))
        def save(agents):
            current = next(item for item in agents if item['id'] == target_id)
            _mutable(current)
            current_source = next((item for item in agents if item['id'] == source['id']), None)
            if (_profile(manager, current) != profile_id or not current_source
                    or _profile(manager, current_source) != binding['source_profile_id']):
                raise FollowError('Agent 配置绑定已改变，请刷新后重新设置')
            for other in agents:
                # One writer per OxiDNS profile. Shared profiles can be reused,
                # but two independent bindings must not fight over their draft.
                if other['id'] != target_id and (other.get('dns_follow') or {}).get('target_profile_id') == profile_id:
                    raise FollowError('此 OxiDNS 配置已有联动绑定，请使用独立配置或先解除另一绑定')
            current['dns_follow'] = binding
            current.pop('dns_follow_job', None)
            current['dns_follow_state'] = {'status': 'waiting', 'message': '等待 Mihomo 下一次成功发布', 'updated_at': _now(), 'events': []}
        manager._update_agents(save)
    return options(manager, target_id)


def disable(manager, target_id):
    with _job_lock(manager):
        _agent(manager, target_id, 'oxidns')
        def save(agents):
            target = next(item for item in agents if item['id'] == target_id)
            _mutable(target)
            for key in ('dns_follow', 'dns_follow_state', 'dns_follow_job'):
                target.pop(key, None)
        manager._update_agents(save)
    return {'success': True, 'binding': None, 'state': None}


_LOG_LEVELS = {'preparing': logging.INFO, 'succeeded': logging.INFO, 'failed': logging.WARNING}


class _Unchanged(Exception):
    """Leaves the save without a commit."""


def _state(manager, target_id, status, message, *, before_save=None, **fields):
    return _save_state(manager, target_id, status, message, before_save=before_save, **fields)[0]


def _save_state(manager, target_id, status, message, *, before_save=None, **fields):
    """Record the state; returns (state, whether anything was written).

    A round that finds what the previous one found, a binding that stays
    broken or a publication still waiting for its device, would only move the
    timestamp. That is not written: each write is a commit of every Agent
    record, and it would wake everything that rests until the configuration
    changes.
    """
    names, kept = [], []

    def save(agents):
        if before_save is not None:
            before_save(agents)
        target = next(item for item in agents if item['id'] == target_id)
        secrets = tuple(item.get('token') for item in agents)
        state = target.setdefault('dns_follow_state', {})
        message_safe = failure_text(message, secrets)
        events = append_event(state.get('events'), status, message_safe, secrets=secrets)
        if (before_save is None and (state.get('status'), state.get('message')) == (status, message_safe)
                and all(key in state and state[key] == value for key, value in fields.items())
                and events == state.get('events')):
            kept.append(deepcopy(state))
            raise _Unchanged()
        names.append((target.get('name') or target_id, (state.get('status'), state.get('message'))))
        state.update(fields, status=status, message=message_safe, updated_at=_now())
        state['events'] = events
        return deepcopy(state)
    try:
        result = manager._update_agents(save)
    except _Unchanged:
        return kept[0], False
    if names and status in ('succeeded', 'failed') and names[-1][1][0] != status:
        # One finished run for 系统设置 → 定时任务, not one per reconcile round.
        from backend.utils import task_status
        task_status.record(f'dns_follow:{target_id}', ok=status == 'succeeded', trigger='event',
                           error=result.get('message', ''))
    if names and names[-1][1] != (result.get('status'), result.get('message')):
        # A persistent error repeats every round; record only its first occurrence.
        from backend.utils.logger import log_text
        _logger.log(_LOG_LEVELS.get(status, logging.DEBUG), 'OxiDNS %s DNS 联动：%s',
                    log_text(names[-1][0]), log_text(result.get('message', ''), 300))
    return result, True


def retry(manager, target_id):
    with _job_lock(manager):
        target = _agent(manager, target_id, 'oxidns')
        state = target.get('dns_follow_state') or {}
        binding = target.get('dns_follow')
        if not binding or state.get('status') != 'failed':
            raise FollowError('只有已确认失败的 DNS 联动可以重试；执行中的任务会继续确认结果')
        child_id = state.get('target_deployment_id')
        child = target.get('deployments', {}).get(child_id)
        if child and child.get('status') not in _TERMINAL:
            raise FollowError('OxiDNS 发布结果尚未确认，请先恢复 Agent 连接')
        def reset(agents):
            current = next(item for item in agents if item['id'] == target_id)
            current.pop('dns_follow_job', None)
            current['dns_follow_state'] = {'status': 'waiting', 'source_deployment_id': state.get('source_deployment_id'),
                'retry_requested': True, 'attempt': state.get('attempt', 0) + 1, 'updated_at': _now(), 'events': []}
        manager._update_agents(reset)
        _state(manager, target_id, 'waiting', '已安排重试，将重新确认 Mihomo 当前 DNS 地址')
    return {'success': True, 'state': manager.get_agent_by_id(target_id)['dns_follow_state']}


def _prepare(agent, config, base_url, deployment_id):
    from backend.routes.agents import _prepare_transactional_content
    return _prepare_transactional_content(agent, config, base_url=base_url, deployment_id=deployment_id)


def _generated_main(config, baseline):
    from backend.converters.oxidns import generate_oxidns_config
    return yaml.safe_load(generate_oxidns_config(config, base_url=baseline.get('base_url', '')))


def _matches_applied_config(target, config, baseline):
    """Prove the draft upstream belongs to the confirmed live main file.

    A binding can be created from an unpublished draft. Its address alone is
    not evidence of the installed address. Only a full main-file digest, plus
    dns/active's device verification, permits skipping resource downloads.
    """
    version = baseline.get('config_version')
    if (not isinstance(version, str) or not re.fullmatch(r'[0-9a-f]{64}', version)
            or target.get('config_version') != version):
        return False
    main = _generated_main(config, baseline)
    # Existing publications may predate the local watchdog. Both renderings
    # retain the bound upstream; a no-op does not need to add the local probe.
    return any(hashlib.sha256(render_deployment_config(
        main, 'oxidns', local_dns_watchdog=watchdog)).hexdigest() == version
        for watchdog in (False, True))


def _matches_applied_bundle(bundle, config, baseline):
    if deployment_content_fingerprint(bundle.manifest) == baseline['fingerprint']:
        return True
    # A full publication adds the local probe, but the confirmed baseline may
    # predate it. Compare the original rendering with the SAME materialized
    # resources so a guard migration cannot hide draft edits or rule changes.
    legacy_main = render_deployment_config(_generated_main(config, baseline), 'oxidns', local_dns_watchdog=False)
    manifest = deepcopy(bundle.manifest)
    for item in manifest['files']:
        if item['path'] == manifest['config_path']:
            item.update(sha256=hashlib.sha256(legacy_main).hexdigest(), size=len(legacy_main))
    return deployment_content_fingerprint(manifest) == baseline['fingerprint']


UNPUBLISHED_CHANGES = 'OxiDNS 有其他未发布的配置或规则变化，请先手动发布这些修改，再重试联动'


def _drift_message(bundle, config, baseline):
    """Say why a regenerated bundle differs, as far as the baseline can prove.

    Only an unchanged OxiDNS draft rules out the user's own edits. Baselines
    recorded before per-file digests existed keep the original message.
    """
    recorded = baseline.get('files')
    if (not isinstance(recorded, dict) or not baseline.get('settings_digest')
            or baseline['settings_digest'] != settings_digest(config)):
        return UNPUBLISHED_CHANGES
    current = deployment_content_digests(bundle.manifest)
    legacy_main = render_deployment_config(_generated_main(config, baseline), 'oxidns', local_dns_watchdog=False)
    main_changed = recorded.get('config.yaml') not in (current['config.yaml'], hashlib.sha256(legacy_main).hexdigest())
    rules = sorted(path for path in set(recorded) | set(current)
                   if path != 'config.yaml' and recorded.get(path) != current.get(path))
    if not main_changed:
        example = f'，如 {rules[0]}' if rules else ''
        return (f'OxiDNS 设置没有修改，但 {len(rules)} 个规则文件与上次发布不同{example}：规则源内容有更新。'
                '请手动发布一次 OxiDNS；如已开启规则自动同步，等同步完成后重试联动即可')
    extra = f'，另有 {len(rules)} 个规则文件不同' if rules else ''
    return (f'OxiDNS 设置没有修改，但生成的主配置与上次发布不同{extra}：通常是 ConfigFlow 更新改变了生成方式，'
            '或引用的规则集、策略组有变化。请手动发布一次 OxiDNS 建立新的基准，再重试联动')


def _check_ownership(manager, agents, target, source, binding, source_id, baseline):
    """Reject stale decisions after Agent calls or resource preparation."""
    current_source = next((item for item in agents if item['id'] == source['id']), None)
    current_target = next((item for item in agents if item['id'] == target['id']), None)
    for current, previous, service in ((current_source, source, 'mihomo'), (current_target, target, 'oxidns')):
        if not current or current.get('service_type') != service or not current.get('enabled', True):
            raise FollowError(f'{service} Agent 不存在、已停用或类型不符')
        if any(current.get(key) != previous.get(key) for key in ('host', 'port', 'token')):
            raise FollowError('Agent 连接信息已改变，请重试 DNS 联动')
    if (_profile(manager, current_source) != binding['source_profile_id']
            or _profile(manager, current_target) != binding['target_profile_id']
            or current_target.get('dns_follow') != binding):
        raise FollowError('Agent 的配置绑定已改变，请重新设置 DNS 联动')
    latest = current_source.get('latest_deployment') or {}
    if (latest.get('deployment_id') != source_id or latest.get('status') != 'succeeded'
            or latest.get('manual_rollback')):
        raise FollowError('Mihomo 已开始新的发布或回滚，请等待最新结果后重试')
    latest_target = current_target.get('latest_deployment') or {}
    previous_target = target.get('latest_deployment') or {}
    if (current_target.get('applied_content') != baseline
            or current_target.get('config_version') != target.get('config_version')
            or any(latest_target.get(key) != previous_target.get(key)
                   for key in ('deployment_id', 'status', 'manual_rollback'))
            or (latest_target and latest_target.get('status') not in _TERMINAL)):
        raise FollowError('OxiDNS 发布基准已变化，请重试')


def _finish_unchanged(manager, target, source, binding, source_id, baseline, address):
    # Validate and record success under the same store transaction. Otherwise a
    # newer binding or deployment could arrive between the check and the write.
    _state(manager, target['id'], 'succeeded', 'Mihomo 已更新，OxiDNS 上游地址未变化，无需再次发布',
           before_save=lambda agents: _check_ownership(manager, agents, target, source, binding, source_id, baseline),
           target_deployment_id=None, address=address)


def _endpoint(manager, target, source, binding, deployment_id, entry):
    result = _request(manager, source, 'dns/listener', params={'deployment_id': deployment_id})
    if result.get('agent_id') != source['id'] or result.get('deployment_id') != deployment_id or result.get('enabled') is not True:
        raise FollowError('Mihomo DNS 尚未确认启用，请重新发布后重试')
    port, host = result.get('port'), result.get('host')
    if type(port) is not int or not 1 <= port <= 65535 or not isinstance(host, str):
        raise FollowError('Mihomo 返回的 DNS 监听地址无效')
    try:
        ip = ipaddress.ip_address(host)
    except ValueError:
        raise FollowError('Mihomo DNS 监听地址必须为明确的 IP 地址') from None
    if binding['connection'] == 'auto':
        _same_namespace(manager, target, source)
        if ip.is_unspecified:
            host = '::1' if ip.version == 6 else '127.0.0.1'
    else:
        if ip.is_loopback:
            raise FollowError('Mihomo 仅监听回环地址，跨设备 OxiDNS 无法访问；请修改 dns.listen 后重新发布')
        host = _network_host(binding.get('address'))
    return f'{_scheme(entry["addr"])}://{("[" + host + "]") if ":" in host else host}:{port}'


def _set_job(manager, target_id, job):
    def save(agents):
        next(item for item in agents if item['id'] == target_id)['dns_follow_job'] = deepcopy(job)
    manager._update_agents(save)


def _finalize(manager, target, state, job):
    # An interrupted finalization is repeatable. Never replace a newer draft.
    target = manager.get_agent_by_id(target['id'])
    if ((target.get('latest_deployment') or {}).get('deployment_id') != state['target_deployment_id']
            or (target.get('applied_content') or {}).get('deployment_id') != state['target_deployment_id']
            or _profile(manager, target) != job['profile_id']
            or (target.get('dns_follow') or {}).get('id') != job['binding_id']):
        raise FollowError('OxiDNS 当前发布或绑定已改变，未覆盖草稿，请核对设备状态后重试')
    binding = target['dns_follow']
    source = _agent(manager, binding['source_agent_id'], 'mihomo')
    source_state = source.get('latest_deployment') or {}
    if (_profile(manager, source) != binding['source_profile_id']
            or source_state.get('deployment_id') != state['source_deployment_id']
            or source_state.get('status') != 'succeeded' or source_state.get('manual_rollback')):
        raise FollowError('Mihomo 当前发布已改变或回滚，OxiDNS 联动未确认一致，请核对后重试')
    def save(config):
        if config.get('oxidns', {}) == job['candidate_settings']:
            return
        if _digest(config.get('oxidns', {})) != job['original_settings_digest']:
            raise FollowError('发布期间 OxiDNS 草稿有新修改，已保留草稿；请核对后再发布')
        config['oxidns'] = deepcopy(job['candidate_settings'])
    warning = ''
    try:
        manager.repository.update_profile_transaction(job['profile_id'], save)
    except ValueError as error:
        warning = str(error)
    if not warning:
        def bind(agents):
            current = next(item for item in agents if item['id'] == target['id'])
            if (current.get('dns_follow') or {}).get('id') == job['binding_id']:
                current['dns_follow']['upstream_signature'] = job['candidate_signature']
        manager._update_agents(bind)
    _state(manager, target['id'], 'succeeded', 'Mihomo 与 OxiDNS 均已发布成功' + ('；' + warning if warning else ''),
           draft_synced=not bool(warning))


def _resume(manager, target):
    state, job = target['dns_follow_state'], target.get('dns_follow_job')
    child_id = state.get('target_deployment_id')
    if not child_id or not job:
        raise FollowError('联动准备被中断，请重试')
    child = manager.get_deployment(target['id'], child_id)
    if child.get('status') == 'succeeded' and not child.get('manual_rollback'):
        _finalize(manager, target, state, job)
    elif child.get('status') in ('failed', 'rolled_back', 'rollback_failed') or child.get('http_status') == 404:
        if child.get('status') == 'rollback_failed':
            _state(manager, target['id'], 'publishing', 'OxiDNS 回滚失败，请先在发布记录中恢复服务并确认结果')
        else:
            raise FollowError(child.get('error_details') or child.get('error') or child.get('message') or 'OxiDNS 发布失败')
    else:
        _state(manager, target['id'], 'publishing', 'Mihomo 已更新；OxiDNS：' + (child.get('message') or '正在确认发布结果'))


def _run(manager, target, source, binding, source_state):
    target_id, source_id = target['id'], source_state['deployment_id']
    previous_state = target.get('dns_follow_state') or {}
    attempt = previous_state.get('attempt', 0) if previous_state.get('source_deployment_id') == source_id else 0
    child_id = 'dns-' + _digest([binding['id'], source_id, attempt])[:48]
    _state(manager, target_id, 'preparing', 'Mihomo 已更新，正在读取实际 DNS 监听地址',
           source_deployment_id=source_id, target_deployment_id=child_id, attempt=attempt, retry_requested=False)
    baseline = target.get('applied_content') or {}
    if (not baseline.get('fingerprint') or baseline.get('profile_id') != binding['target_profile_id']
            or baseline.get('service_type') != 'oxidns'):
        raise FollowError('请先手动成功发布一次 OxiDNS，建立当前配置基准后重试')
    base_url = baseline.get('base_url', '')
    config, revision = manager.repository.get_publish_snapshot(binding['target_profile_id'])
    settings, document = _document(config, base_url)
    entry = _entry(document, binding)
    if _digest(entry) != binding['upstream_signature']:
        raise FollowError('绑定的上游已被编辑或移动，请重新选择绑定，避免覆盖自定义内容')
    address = _endpoint(manager, target, source, binding, source_id, entry)
    active = _request(manager, target, 'dns/active', params={'deployment_id': baseline.get('deployment_id')})
    if active.get('agent_id') != target_id or active.get('deployment_id') != baseline.get('deployment_id'):
        raise FollowError('OxiDNS 当前配置与最后成功发布不一致，请先手动发布并确认')
    if entry['addr'] == address and _matches_applied_config(target, config, baseline):
        _finish_unchanged(manager, target, source, binding, source_id, baseline, address)
        return
    _state(manager, target_id, 'preparing', '正在核对 OxiDNS 配置与规则文件是否存在未发布修改')
    prepared = _prepare(target, config, base_url, child_id)
    if not _matches_applied_bundle(prepared['bundle'], config, baseline):
        raise FollowError(_drift_message(prepared['bundle'], config, baseline))
    scheme, endpoint = address.split('://', 1)
    _state(manager, target_id, 'preparing', f'正在同步 OxiDNS 上游为 {endpoint}（{scheme.upper()}）', address=address)
    if entry['addr'] == address:
        _finish_unchanged(manager, target, source, binding, source_id, baseline, address)
        return
    original_settings_digest = _digest(config.get('oxidns', {}))
    entry['addr'] = address
    settings['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    config['oxidns'] = settings
    candidate = _prepare(target, config, base_url, child_id)
    # A remote ruleset can change between materializations. This automatic
    # release is authorized to change one upstream, not refresh other files.
    def resources(bundle):
        return sorted((item['path'], item['sha256']) for item in bundle.manifest['files']
                      if item['path'] != bundle.manifest['config_path'])
    if resources(prepared['bundle']) != resources(candidate['bundle']):
        raise FollowError('OxiDNS 规则文件在准备期间发生变化，请先手动发布后重试')
    # No profile writes during preparation. Re-read ownership immediately before
    # reservation; latest source is also rechecked to avoid publishing stale jobs.
    _check_ownership(manager, manager.get_all_agents(), target, source, binding, source_id, baseline)
    result = manager.begin_deployment(target_id, child_id)
    if not result.get('success') or result.get('existing'):
        raise FollowError(result.get('message') or 'OxiDNS 还有其他发布任务，请等待结果后重试')
    try:
        job = {'binding_id': binding['id'], 'profile_id': binding['target_profile_id'],
               'original_settings_digest': original_settings_digest, 'candidate_settings': settings,
               'candidate_signature': _digest(entry)}
        _set_job(manager, target_id, job)
        _state(manager, target_id, 'publishing', 'Mihomo 已更新，正在校验并发布 OxiDNS')
        manager.publish_deployment(target_id, candidate['bundle'], activate=True,
                                   config_revision=revision, base_url=base_url, resource_config=config,
                                   source_snapshot=config)
    finally:
        manager.finish_deployment_preparation(target_id, child_id)
    _resume(manager, manager.get_agent_by_id(target_id))


WAITING_FOR_TARGET = 'Mihomo 已更新，等待 OxiDNS 当前发布完成后再同步'


def _target_release_in_flight(manager, target):
    """A release that is still activating moves the device off the baseline.

    Checking during that window compares the old baseline with the new active
    release and fails spuriously; once it ends the next round sees the result.
    """
    from .oxidns_ha_publish import active
    if active(target):
        return True
    latest = target.get('latest_deployment') or {}
    if not latest.get('deployment_id') or latest.get('status') in _TERMINAL:
        return False
    return manager.get_deployment(target['id'], latest['deployment_id']).get('status') not in _TERMINAL


_rounds = IdleRounds()


def tick(manager):
    """Run one bounded round. Source polling continues even when the UI closes."""
    from backend.common.config import reset_config_context
    reset_config_context()
    head = _rounds.due(manager.repository)
    if head is None:
        return
    _rounds.done(manager.repository, head, _round(manager))


def _listed(agents, agent_id, service):
    """_agent() against the Agents this round already read."""
    agent = agents.get(agent_id)
    if not agent or agent.get('service_type') != service or not agent.get('enabled', True):
        raise FollowError(f'{service} Agent 不存在、已停用或类型不符')
    return agent


def _round(manager):
    """One round; True when no binding had anything to do, poll or wait for."""
    idle = True
    try:
        with _job_lock(manager):
            agents = {item['id']: item for item in manager.get_all_agents()}
            targets = [item for item in agents.values() if item.get('dns_follow')]
            for target in targets:
                # Until the round asks a device or starts work, what it finds
                # follows from the stored configuration alone.
                stored_only = True
                try:
                    state, binding = target.get('dns_follow_state') or {}, target['dns_follow']
                    if state.get('status') in _RUNNING:
                        idle = stored_only = False
                        _resume(manager, target)
                        continue
                    _listed(agents, target['id'], 'oxidns')
                    source = _listed(agents, binding['source_agent_id'], 'mihomo')
                    if (_profile(manager, target) != binding['target_profile_id']
                            or _profile(manager, source) != binding['source_profile_id']):
                        raise FollowError('Agent 的配置绑定已改变，请重新设置 DNS 联动')
                    latest = source.get('latest_deployment') or {}
                    if latest.get('deployment_id') and latest.get('status') not in _TERMINAL:
                        idle = stored_only = False
                        latest = manager.get_deployment(source['id'], latest['deployment_id'])
                    source_id = latest.get('deployment_id')
                    if (not source_id or latest.get('status') != 'succeeded' or latest.get('manual_rollback')
                            or source_id == binding.get('after_deployment_id')):
                        continue
                    if source_id == state.get('source_deployment_id') and not state.get('retry_requested'):
                        continue
                    idle = stored_only = False
                    if _target_release_in_flight(manager, target):
                        # Keep the source unrecorded so the next round still runs.
                        if (state.get('status'), state.get('message')) != ('waiting', WAITING_FOR_TARGET):
                            _state(manager, target['id'], 'waiting', WAITING_FOR_TARGET)
                        continue
                    # The check may just have recorded the release's outcome. The
                    # source is read again as well: the work below takes the stored
                    # record, not this round's listing.
                    target = manager.get_agent_by_id(target['id'])
                    source = _agent(manager, binding['source_agent_id'], 'mihomo')
                    if (source.get('latest_deployment') or {}).get('deployment_id') != source_id:
                        continue
                    _run(manager, target, source, binding, latest)
                except Exception as error:
                    # Requests errors can contain tokens/config URLs. Do not log
                    # payloads or include opaque third-party exception strings.
                    message = str(error) if isinstance(error, ValueError) else '联动处理异常，请检查 Agent 连接后重试'
                    current = manager.get_agent_by_id(target['id'])
                    state = current.get('dns_follow_state') or {}
                    child_id = state.get('target_deployment_id')
                    child = current.get('deployments', {}).get(child_id)
                    if child and child.get('status') not in _TERMINAL and child.get('submitted'):
                        idle = False
                        _state(manager, target['id'], 'publishing', 'Mihomo 已更新，OxiDNS 发布结果待确认：' + message)
                    else:
                        if child and child.get('status') not in _TERMINAL:
                            idle = False
                            manager.record_deployment(target['id'], child_id, {'status': 'failed', 'success': False,
                                'message': 'DNS 联动准备失败', 'error_details': message}, only_unsubmitted=True)
                        prefix = 'Mihomo 已更新，OxiDNS 同步失败：' if state.get('source_deployment_id') else 'DNS 联动暂不可用：'
                        _, written = _save_state(manager, target['id'], 'failed', prefix + message)
                        # A binding broken in the stored configuration, and already
                        # recorded as such, stays broken until that configuration
                        # changes: the rounds may rest. Anything a device, this
                        # round's own writes or an unexpected error had a part in
                        # is tried again.
                        if written or not stored_only or not isinstance(error, FollowError):
                            idle = False
    except FollowError:
        return False  # Another worker owns this round; the next tick will reconcile it.
    return idle


def start_loop(manager):
    import os
    key = (os.getpid(), str(manager.repository.data_dir))
    with _loop_guard:
        if key in _loops:
            return
        _loops.add(key)
    def work():
        from backend.common import config as config_module
        try:
            while config_module._repository is manager.repository:
                try:
                    tick(manager)
                except Exception:
                    _logger.error('DNS follow worker could not complete a round')
                threading.Event().wait(5)
        finally:
            with _loop_guard:
                _loops.discard(key)
    threading.Thread(target=work, daemon=True, name='dns-follow').start()
