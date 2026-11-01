"""Explicit, transactional OxiDNS listeners owned by node-test DNS settings.

Settings reads never create listeners. A separate create/remove operation pins
the device's current rule bytes and edits only the last confirmed publication.
The journal and deployment ID survive web-worker restarts; unknown uploads are
polled, never assumed successful or silently submitted a second time.
"""
from concurrent.futures import ThreadPoolExecutor
from copy import deepcopy
import hashlib
import ipaddress
import json
import logging
import re
import threading
import uuid

import yaml

from . import rule_sync_store as store
from .deployment_bundle import build_deployment_bundle
from .oxidns_ha import _lock, enabled as ha_enabled
from .oxidns_ha_publish import active as ha_active
from .rule_sync_snapshot import fetch_snapshot
from backend.common.dns_watchdog import WATCHDOG_RULE
from backend.common.oxidns_references import reference_locations

FIELD = 'node_test_dns_endpoints'
TERMINAL = {'ready', 'failed', 'removed'}
_pool = ThreadPoolExecutor(max_workers=2, thread_name_prefix='node-dns')
_jobs, _jobs_lock = set(), threading.Lock()
_log = logging.getLogger(__name__)
_FORWARD_FIELDS = {'upstreams', 'concurrent', 'response_selection', 'short_circuit'}
_UPSTREAM_FIELDS = {'tag', 'addr', 'outbound', 'dial_addr', 'port', 'bootstrap', 'bootstrap_version',
    'socks5', 'idle_timeout', 'max_conns', 'min_conns', 'insecure_skip_verify', 'timeout',
    'enable_pipeline', 'enable_http3', 'so_mark', 'bind_to_device'}


def _enqueue(key, callback):
    with _jobs_lock:
        if key in _jobs:
            return
        _jobs.add(key)
    def run():
        try:
            callback()
        finally:
            with _jobs_lock:
                _jobs.discard(key)
    try:
        _pool.submit(run)
    except RuntimeError:
        with _jobs_lock:
            _jobs.discard(key)


def _agent(system, agent_id):
    agent = next((a for a in system.get('agents', []) if a['id'] == agent_id), None)
    if not agent:
        raise ValueError('OxiDNS Agent 不存在')
    return agent


def _check_agent(agent):
    if agent.get('service_type') != 'oxidns' or not agent.get('enabled', True):
        raise ValueError('请选择已启用的 OxiDNS Agent')
    if ha_enabled(agent) or ha_active(agent):
        raise ValueError('暂不支持在高可用 OxiDNS 上创建或管理测速接口')


def _exclusive(system, agent):
    peers = [a for a in system.get('agents', []) if a.get('service_type') == 'oxidns'
             and a.get('profile_id', 'default') == agent.get('profile_id', 'default')]
    if len(peers) != 1:
        raise ValueError('托管接口需要独立配置空间，请先将此 OxiDNS 与其他 OxiDNS Agent 分开绑定')


def _record(agent, endpoint_id):
    record = agent.get(FIELD, {}).get(endpoint_id)
    if not record:
        raise ValueError('托管 DNS 接口不存在')
    if record['profile_id'] != agent.get('profile_id', 'default') or record['agent_host'] != agent.get('host'):
        raise ValueError('Agent 绑定已改变，托管 DNS 接口需要重新创建')
    return record


def _public(record):
    return {key: deepcopy(record[key]) for key in ('agent_id', 'endpoint_id', 'endpoint',
        'listen_address', 'port', 'upstream', 'bootstrap', 'status', 'message', 'deployment_id') if key in record}


def _ipv4(value):
    try:
        if not isinstance(value, str):
            raise ValueError
        ip = ipaddress.IPv4Address(value)
        if ip.is_unspecified or ip.is_loopback or ip.is_multicast or int(ip) == 0xffffffff:
            raise ValueError
        return str(ip)
    except (ValueError, TypeError):
        raise ValueError('监听地址必须是设备可访问的 IPv4 地址') from None


def _spec(payload):
    from backend.utils.node_dns import normalize_config
    if not isinstance(payload, dict):
        raise ValueError('托管 DNS 参数无效')
    address = _ipv4(payload.get('listen_address'))
    port = payload.get('port', 15353)
    if type(port) is not int or not 1 <= port <= 65535:
        raise ValueError('监听端口必须在 1–65535 之间')
    value = payload.get('upstream', '')
    if not isinstance(value, str) or not value.strip():
        raise ValueError('请填写现有返回真实 IP 的上游 DNS')
    value = value.strip()
    bootstrap = payload.get('bootstrap', [])
    if not isinstance(bootstrap, list) or len(bootstrap) > 1:
        raise ValueError('托管接口最多支持一个 IPv4 引导 DNS')
    config = normalize_config({'mode': 'custom', 'servers': [value], 'bootstrap': bootstrap})
    upstream = config['servers'][0]
    if '://' not in upstream:
        upstream = 'udp://' + upstream
    return {'listen_address': address, 'port': port, 'endpoint': f'{address}:{port}',
            'upstream': upstream, 'bootstrap': config['bootstrap']}


def _upstreams(agent, baseline):
    """Return public candidates and private copied executor arguments.

    Eligibility means representable without losing semantics, not a successful
    DNS test. Never guess real/Fake-IP from a plugin name or an upstream address.
    """
    from backend.utils.node_dns import normalize_dns_servers
    document = _document(baseline['source'].get('oxidns'))
    network = document.get('network') or {}
    outbound = network.get('outbound') or {} if isinstance(network, dict) else {}
    outbound_digest = hashlib.sha256(json.dumps(outbound, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
    owned = {p['tag'] for record in agent.get(FIELD, {}).values() for p in record.get('plugins', [])}
    public, definitions = [], {}
    for index, plugin in enumerate(document['plugins']):
        if plugin.get('type') != 'forward' or plugin.get('tag') in owned:
            continue
        args = plugin.get('args') or {}
        entries = args.get('upstreams', []) if isinstance(args, dict) else []
        if not isinstance(entries, list):
            continue
        tag = plugin.get('tag', '')
        label = tag if isinstance(tag, str) and re.fullmatch(r'[\w.-]{1,80}', tag) else f'转发器 {index + 1}'
        if tag in ('forward_local', 'forward_remote', 'forward_fallback'):
            label = {'forward_local': '本地上游', 'forward_remote': '远程上游',
                     'forward_fallback': '备用上游'}[tag] + f'（{tag}）'
        arguments_digest = hashlib.sha256(json.dumps(args, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
        for offset, entry in enumerate(entries):
            fingerprint = {'agent_id': agent['id'], 'profile_id': agent.get('profile_id', 'default'),
                'position': [index, offset], 'tag': tag, 'args_digest': arguments_digest,
                'outbound_digest': outbound_digest}
            ident = hashlib.sha256(json.dumps(fingerprint, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
            address, bootstrap, reason = '', [], ''
            try:
                if not isinstance(entry, dict) or set(args) - _FORWARD_FIELDS or set(entry) - _UPSTREAM_FIELDS:
                    raise ValueError('包含暂不支持复用的扩展参数；不会自动丢弃这些参数')
                raw = entry.get('addr')
                if not isinstance(raw, str) or '${' in json.dumps(entry):
                    raise ValueError('包含无法在管理端确认的动态参数')
                if raw.startswith(('udp://', 'tcp://')):
                    scheme, plain = raw.split('://', 1)
                    address = scheme + '://' + normalize_dns_servers([plain])[0]
                else:
                    address = normalize_dns_servers([raw])[0]
                    if '://' not in address:
                        address = 'udp://' + address
                if entry.get('bootstrap'):
                    bootstrap = normalize_dns_servers([entry['bootstrap']], bootstrap=True)
                if entry.get('dial_addr'):
                    normalize_dns_servers([entry['dial_addr']], bootstrap=True)
                if entry.get('bootstrap_version') == 6:
                    raise ValueError('此上游依赖 IPv6 引导解析，暂不支持复用')
                if entry.get('outbound') or entry.get('socks5'):
                    raise ValueError('此上游依赖代理出站；请选择直接上游或手动填写')
                # An omitted per-upstream outbound inherits the global default.
                # Do not infer direct transport from a profile's name or drop
                # its resolver/proxy policy when copying an executor.
                if not isinstance(outbound, dict) or outbound.get('default'):
                    raise ValueError('此上游继承全局默认出站，暂不支持安全复用；请手动填写并检测完整路径')
                if entry.get('bind_to_device') and not re.fullmatch(r'[\w.:-]{1,64}', str(entry['bind_to_device'])):
                    raise ValueError('无法安全复用设备绑定参数')
            except (ValueError, TypeError) as error:
                # Address parsers use static messages; never echo credentials,
                # query strings or the full upstream mapping in an API error.
                reason = str(error) if isinstance(error, ValueError) else '此上游包含无法安全复用的参数类型'
            candidate = {'id': ident, 'label': f'{label} · 上游 {offset + 1}' + (f' · {address}' if address else ''),
                'address': address, 'bootstrap': bootstrap, 'eligible': not reason,
                'reason': reason, 'validation': 'unverified'}
            public.append(candidate)
            if not reason:
                # Keep the entire supported entry and forward-level settings.
                # In particular dial_addr, bootstrap and TLS flags retain their
                # original values, even when the display address is canonical.
                definitions[ident] = {**{key: deepcopy(value) for key, value in args.items() if key != 'upstreams'},
                                     'upstreams': [deepcopy(entry)]}
    return public, definitions


def _selected_spec(payload, agent, baseline):
    if any(key in payload for key in ('upstream', 'bootstrap')):
        raise ValueError('选择已有上游时只提交上游 ID，不能同时覆盖地址或引导 DNS')
    ident = payload.get('upstream_id')
    if not isinstance(ident, str) or not re.fullmatch('[0-9a-f]{64}', ident):
        raise ValueError('请选择已发布配置中的可复用上游')
    candidates, definitions = _upstreams(agent, baseline)
    selected = next((c for c in candidates if c['id'] == ident), None)
    if selected is None:
        raise ValueError('已发布上游已变更，请刷新列表后重新选择')
    if not selected['eligible']:
        raise ValueError(selected['reason'])
    address = _ipv4(payload.get('listen_address'))
    port = payload.get('port', 15353)
    if type(port) is not int or not 1 <= port <= 65535:
        raise ValueError('监听端口必须在 1–65535 之间')
    return {'listen_address': address, 'port': port, 'endpoint': f'{address}:{port}',
        'upstream': selected['address'], 'bootstrap': selected['bootstrap'], 'forward_args': definitions[ident]}


def _document(settings):
    if not isinstance(settings, dict) or settings.get('config_mode') != 'native':
        raise ValueError('托管接口需要已发布和当前草稿都使用 OxiDNS 原生配置')
    try:
        document = yaml.safe_load(settings.get('native_config', ''))
    except yaml.YAMLError:
        raise ValueError('OxiDNS 原生配置无法解析') from None
    if (not isinstance(document, dict) or not isinstance(document.get('plugins'), list)
            or not all(isinstance(plugin, dict) for plugin in document['plugins'])):
        raise ValueError('OxiDNS 原生配置缺少 plugins')
    return document


def _normalize_plugin(plugin):
    result = deepcopy(plugin)
    if result.get('type') == 'sequence' and isinstance(result.get('args'), list):
        # Publication adds this health check to every entry sequence. Only
        # this exact generated rule is excluded from the ownership digest.
        result['args'] = [rule for rule in result['args'] if rule != WATCHDOG_RULE]
    return result


def _owned(document, record, *, absent=False):
    tags = {p['tag']: p for p in record['plugins']}
    found = [p for p in document['plugins'] if isinstance(p, dict) and p.get('tag') in tags]
    if absent and not found:
        return False
    if len(found) != len(tags) or {p['tag']: _normalize_plugin(p) for p in found} != tags:
        raise ValueError('托管 DNS 插件已被手工修改或缺失，请先恢复原配置')
    return True


def recorded_listeners(plugins, agents, profile_id):
    """Server tags of intact managed listeners that this profile's records own.

    Only an exact match with the stored plugin definitions counts; a tag that
    merely looks like ours, or a hand-edited copy, is not trusted.
    """
    result = set()
    for agent in agents or []:
        if not isinstance(agent, dict) or not isinstance(agent.get(FIELD), dict):
            continue
        for record in agent[FIELD].values():
            if not isinstance(record, dict) or record.get('status') == 'removed' or \
                    record.get('profile_id') != profile_id or not isinstance(record.get('plugins'), list):
                continue
            expected = {p.get('tag'): p for p in record['plugins'] if isinstance(p, dict)}
            if expected and len(expected) == len(record['plugins']) and all(
                    isinstance(plugins.get(tag), dict) and _normalize_plugin(plugins[tag]) == plugin
                    for tag, plugin in expected.items()):
                result.update(tag for tag, plugin in expected.items()
                              if str(plugin.get('type', '')).endswith('_server'))
    return result


def _collision(document, record):
    for plugin in document['plugins']:
        if not isinstance(plugin, dict) or plugin.get('type') not in ('udp_server', 'tcp_server'):
            continue
        listen = (plugin.get('args') or {}).get('listen', '')
        if not isinstance(listen, str):
            raise ValueError('无法确认 OxiDNS 现有监听地址')
        host, sep, port = listen.rpartition(':')
        if sep and port == str(record['port']) and host in ('', '0.0.0.0', '[::]', record['listen_address']):
            raise ValueError('监听端口已被 OxiDNS 使用，请选择其他端口')


def _patch(document, record, operation):
    document = deepcopy(document)
    if operation == 'create':
        if _owned(document, record, absent=True):
            return document
        _collision(document, record)
        document['plugins'].extend(deepcopy(record['plugins']))
    else:
        if not _owned(document, record, absent=True):
            return document
        tags = {p['tag'] for p in record['plugins']}
        document['plugins'] = [p for p in document['plugins'] if p.get('tag') not in tags]
        if reference_locations(document, tags):
            raise ValueError('其他执行链仍引用托管 DNS 插件，不能自动删除')
    return document


def _plugins(endpoint_id, spec):
    prefix = 'cf_node_test_' + endpoint_id
    upstream = {'addr': spec['upstream']}
    if spec['bootstrap']:
        upstream['bootstrap'] = spec['bootstrap'][0]
    return [
        {'tag': prefix + '_forward', 'type': 'forward', 'args': deepcopy(spec.get('forward_args', {'upstreams': [upstream]}))},
        {'tag': prefix + '_sequence', 'type': 'sequence', 'args': [
            {'exec': '$' + prefix + '_forward'}, {'matches': ['!has_resp'], 'exec': 'reject 2'}, {'exec': 'accept'}]},
        *[{'tag': prefix + '_' + protocol, 'type': protocol + '_server', 'args': {
            'listen': spec['endpoint'], 'entry': prefix + '_sequence'}} for protocol in ('udp', 'tcp')],
    ]


def _baseline(manager, agent):
    baseline = store.load(manager.repository, agent)
    if not baseline:
        raise ValueError('请先成功发布一次基础 OxiDNS 配置，再创建托管接口')
    _document(baseline['source'].get('oxidns'))
    _document(manager.repository.get_profile(agent.get('profile_id', 'default')).get('oxidns'))
    return baseline


def _update(manager, agent_id, endpoint_id, **changes):
    def update(system):
        record = _record(_agent(system, agent_id), endpoint_id)
        record.update(deepcopy(changes))
        return None
    manager.repository.update_system_transaction(update)


def _referenced(system, agent_id, endpoint_id):
    settings = system.get('system_config', {}).get('node_test_dns_config', {})
    return settings.get('mode') == 'managed' and settings.get('managed') == {
        'agent_id': agent_id, 'endpoint_id': endpoint_id}


def _binding_record(system, binding):
    if not isinstance(binding, dict):
        raise ValueError('请选择已就绪的 OxiDNS 托管接口')
    agent = _agent(system, binding.get('agent_id'))
    _check_agent(agent)
    _exclusive(system, agent)
    record = _record(agent, binding.get('endpoint_id'))
    if record['status'] != 'ready':
        raise ValueError('托管 DNS 接口尚未就绪，请先完成创建和检查')
    return agent, record


def _publication(agent):
    applied = agent.get('applied_content') or {}
    return [applied.get('deployment_id'), applied.get('config_version'), agent.get('config_version')]


def validate_binding(system, binding):
    """No I/O: call inside the same system transaction that saves the binding."""
    agent, record = _binding_record(system, binding)
    if record.get('verified_publication') != _publication(agent):
        raise ValueError('托管 DNS 配置已变化，请刷新后重新检查')
    return record['endpoint']


def resolve(manager, binding):
    system = manager.repository.get_system()
    agent, record = _binding_record(system, binding)
    baseline = _baseline(manager, agent)
    _owned(_document(baseline['source'].get('oxidns')), record)
    _owned(_document(manager.repository.get_profile(record['profile_id']).get('oxidns')), record)
    observed = _publication(agent)
    if record.get('verified_publication') != observed:
        # A normal publication retaining these plugins is valid. Refresh only
        # after examining its receipt, never write on unchanged settings reads.
        def acknowledge(current):
            latest, endpoint = _binding_record(current, binding)
            if _publication(latest) != observed:
                raise ValueError('托管 DNS 配置已变化，请刷新后重新检查')
            endpoint['verified_publication'] = observed
        manager.repository.update_system_transaction(acknowledge)
    return record['endpoint']


def options(manager):
    names = {p['id']: p['name'] for p in manager.repository.list_profiles()}
    result = []
    system = manager.repository.get_system()
    for agent in system.get('agents', []):
        if agent.get('service_type') != 'oxidns':
            continue
        reason = ''
        upstreams = []
        try:
            _check_agent(agent)
            _exclusive(system, agent)
            baseline = _baseline(manager, agent)
            upstreams, _ = _upstreams(agent, baseline)
        except ValueError as exc:
            reason = str(exc)
        try:
            address = _ipv4(agent.get('host'))
        except ValueError:
            address = ''
        profile_id = agent.get('profile_id', 'default')
        result.append({'id': agent['id'], 'name': agent.get('name', agent['id']), 'profile_id': profile_id,
            'profile_name': names.get(profile_id, profile_id), 'address': address, 'eligible': not reason,
            'upstreams': upstreams, 'upstreams_reason': '' if any(u['eligible'] for u in upstreams) else
                '已发布配置没有可复用的直接上游，可查看原因或手动填写',
            'reason': reason, 'endpoints': [_public(r) for r in agent.get(FIELD, {}).values() if r['status'] != 'removed']})
    return {'agents': result, 'default_port': 15353}


def create(manager, payload, *, verify=None):
    if not isinstance(payload, dict):
        raise ValueError('托管 DNS 参数无效')
    agent_id = payload.get('agent_id')
    with _lock(manager):
        system = manager.repository.get_system()
        agent = _agent(system, agent_id)
        _check_agent(agent)
        _exclusive(system, agent)
        baseline = _baseline(manager, agent)
        spec = _selected_spec(payload, agent, baseline) if 'upstream_id' in payload else _spec(payload)
        for record in agent.get(FIELD, {}).values():
            if record['status'] != 'removed' and all(record.get(k) == v for k, v in spec.items()):
                if record['status'] == 'failed':
                    raise ValueError('相同托管接口创建失败，请先删除该接口记录后重试')
                if record['status'] == 'ready':
                    resolve(manager, {'agent_id': agent_id, 'endpoint_id': record['endpoint_id']})
                result = _public(record)
                break
        else:
            if any(r['status'] not in TERMINAL for r in agent.get(FIELD, {}).values()):
                raise ValueError('该 Agent 的托管 DNS 操作正在执行，请稍后重试')
            endpoint_id = uuid.uuid4().hex[:16]
            plugins = _plugins(endpoint_id, spec)
            record = {**spec, 'agent_id': agent_id, 'endpoint_id': endpoint_id,
                'profile_id': agent.get('profile_id', 'default'), 'agent_host': agent['host'],
                'plugins': plugins, 'fingerprint': hashlib.sha256(json.dumps(plugins, sort_keys=True).encode()).hexdigest(),
                'status': 'preparing', 'operation': 'create', 'message': '正在准备专用 DNS 接口',
                'deployment_id': 'node-dns-' + uuid.uuid4().hex, 'installed': False}
            if 'upstream_id' in payload:
                record['source_upstream_id'] = payload['upstream_id']
            def add(system):
                current = _agent(system, agent_id)
                _check_agent(current)
                _exclusive(system, current)
                if current.get('profile_id', 'default') != record['profile_id'] or current.get('host') != record['agent_host']:
                    raise ValueError('Agent 绑定已变化，请刷新后重试')
                if _publication(current) != _publication(agent):
                    raise ValueError('已发布配置已变化，请刷新后重新选择上游')
                current.setdefault(FIELD, {})[endpoint_id] = deepcopy(record)
            manager.repository.update_system_transaction(add)
            result = _public(record)
    if result['status'] not in TERMINAL:
        _schedule(manager, agent_id, result['endpoint_id'], verify)
    return result


def status(manager, agent_id, endpoint_id, *, verify=None):
    agent = _agent(manager.repository.get_system(), agent_id)
    record = _record(agent, endpoint_id)
    result = _public(record)
    if record['status'] not in TERMINAL:
        _schedule(manager, agent_id, endpoint_id, verify)
    elif record['status'] == 'ready':
        try:
            resolve(manager, {'agent_id': agent_id, 'endpoint_id': endpoint_id})
        except ValueError as exc:
            result.update(status='failed', message=str(exc))
    return result


def remove(manager, agent_id, endpoint_id):
    with _lock(manager):
        current_agent = _agent(manager.repository.get_system(), agent_id)
        current_record = _record(current_agent, endpoint_id)
        forget = _can_forget(manager, current_agent, current_record)
        def start(system):
            agent = _agent(system, agent_id)
            _check_agent(agent)
            record = _record(agent, endpoint_id)
            if _referenced(system, agent_id, endpoint_id):
                raise ValueError('节点测速仍引用此接口，请先保存其他 DNS 模式')
            if record['status'] in ('removed', 'removing'):
                return None
            if record['status'] not in ('ready', 'failed'):
                raise ValueError('托管接口仍在发布或检查，请等待结果后再删除')
            if forget and record == current_record:
                record.update(status='removed', message='未安装的接口记录已删除')
                return None
            record.update(status='removing', operation='remove', message='正在删除专用 DNS 接口',
                          deployment_id='node-dns-' + uuid.uuid4().hex)
        manager.repository.update_system_transaction(start)
        record = _record(_agent(manager.repository.get_system(), agent_id), endpoint_id)
        result = _public(record)
    if record['status'] != 'removed':
        _schedule(manager, agent_id, endpoint_id, None)
    return result


def _can_forget(manager, agent, record):
    """Only a proven, unsubmitted failure can be removed while offline."""
    if record['status'] != 'failed' or record.get('installed') or record.get('operation') != 'create':
        return False
    deployment = agent.get('deployments', {}).get(record['deployment_id'])
    if deployment and (deployment.get('submitted') or deployment.get('status') != 'failed'):
        return False
    baseline = store.load(manager.repository, agent, with_bundle=False)
    if not baseline:
        return False
    try:
        for settings in (baseline['source'].get('oxidns'),
                         manager.repository.get_profile(record['profile_id']).get('oxidns')):
            if _owned(_document(settings), record, absent=True):
                return False
    except ValueError:
        return False
    return True


def _schedule(manager, agent_id, endpoint_id, verify):
    key = (str(manager.repository.data_dir), agent_id, endpoint_id)
    _enqueue(key, lambda: _work(manager, agent_id, endpoint_id, verify))


def _merge_draft(manager, record):
    def merge(profile):
        settings = profile['oxidns']
        document = _patch(_document(settings), record, record['operation'])
        settings['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    manager.repository.update_profile_transaction(record['profile_id'], merge)


def _complete(manager, agent_id, record, verify):
    # A successful child may have been superseded while an HTTP response was
    # lost. Never call that older operation ready against a different graph.
    agent = _agent(manager.repository.get_system(), agent_id)
    baseline = _baseline(manager, agent)
    document = _document(baseline['source'].get('oxidns'))
    if record['operation'] == 'remove':
        if _owned(document, record, absent=True):
            raise ValueError('当前已发布配置仍包含托管接口，删除结果需要重新确认')
        _merge_draft(manager, record)
        _update(manager, agent_id, record['endpoint_id'], status='removed', installed=False, message='专用 DNS 接口已删除')
        return
    _owned(document, record)
    _update(manager, agent_id, record['endpoint_id'], installed=True, status='verifying', message='正在检查真实 DNS 应答')
    _merge_draft(manager, record)
    if verify is None:
        return
    result = verify(record['endpoint'])
    if not isinstance(result, dict) or result.get('success') is not True:
        _update(manager, agent_id, record['endpoint_id'], status='failed', message='专用接口检查失败，请确认上游能返回真实 IP，删除后重试')
        return
    _update(manager, agent_id, record['endpoint_id'], status='ready', verified_publication=_publication(agent),
            message='专用 DNS 接口已发布并通过检查')


def _work(manager, agent_id, endpoint_id, verify):
    reserved = None
    try:
        # Shared with HA setup, and held across the remote transaction. Other
        # web workers can poll without acquiring it or blocking an API request.
        with _lock(manager):
            agent = _agent(manager.repository.get_system(), agent_id)
            _check_agent(agent)
            record = deepcopy(_record(agent, endpoint_id))
            if record['status'] in TERMINAL:
                return
            ident = record['deployment_id']
            existing = agent.get('deployments', {}).get(ident)
            if existing:
                outcome = manager.get_deployment(agent_id, ident)
            else:
                baseline = _baseline(manager, agent)
                if record['operation'] == 'create' and record.get('source_upstream_id'):
                    _, definitions = _upstreams(agent, baseline)
                    if definitions.get(record['source_upstream_id']) != record.get('forward_args'):
                        raise ValueError('已发布上游已变更，请刷新列表后重新创建接口')
                # Validate draft ownership before any remote mutation, but do
                # not publish this draft. A second CAS merge happens afterward.
                _patch(_document(manager.repository.get_profile(record['profile_id']).get('oxidns')), record, record['operation'])
                begun = manager.begin_deployment(agent_id, ident)
                if not begun.get('success') or begun.get('existing'):
                    raise ValueError('Agent 正在发布其他配置，请等待完成后重试')
                reserved = ident
                live = fetch_snapshot(manager, agent, baseline)
                files = store.bundle_files(live)
                config_bytes = next(data for data, role in files.values() if role == 'config')
                live_doc = yaml.safe_load(config_bytes)
                updated = _patch(live_doc, record, record['operation'])
                source = deepcopy(baseline['source'])
                source['oxidns']['native_config'] = yaml.safe_dump(
                    _patch(_document(source['oxidns']), record, record['operation']), allow_unicode=True, sort_keys=False)
                if record['operation'] == 'remove' and updated == live_doc and not record.get('installed'):
                    # A preparation/validation failure installed nothing.
                    manager.record_deployment(agent_id, ident, {'status': 'failed', 'success': False, 'message': '无需部署：接口未创建'})
                    _merge_draft(manager, record)
                    _update(manager, agent_id, endpoint_id, status='removed', message='未安装的接口记录已删除')
                    return
                pinned = [{'path': path, 'content': data, 'role': role} for path, (data, role) in files.items() if role != 'config']
                bundle = build_deployment_bundle(agent, source, yaml.safe_dump(updated, allow_unicode=True, sort_keys=False),
                    pinned_files=pinned, base_url=baseline.get('base_url', ''), deployment_id=ident)
                _update(manager, agent_id, endpoint_id, status='removing' if record['operation'] == 'remove' else 'publishing', message='正在发布专用 DNS 配置')
                outcome = manager.publish_deployment(agent_id, bundle, source_snapshot=source, base_url=baseline.get('base_url', ''))
            if outcome.get('status') == 'succeeded':
                _complete(manager, agent_id, record, verify)
            elif outcome.get('status') in ('failed', 'rolled_back', 'rollback_failed'):
                _update(manager, agent_id, endpoint_id, status='failed', message='专用 DNS 配置发布失败，请查看 Agent 发布记录')
            else:
                _update(manager, agent_id, endpoint_id, status='removing' if record['operation'] == 'remove' else 'publishing', message='发布结果待确认，请刷新状态')
    except ValueError as exc:
        # The lock can be held by another worker which owns this exact job;
        # do not mark that live operation failed because a second poll lost it.
        if str(exc) == 'DNS 高可用正在处理，请稍后重试':
            return
        if reserved:
            manager.record_deployment(agent_id, reserved, {'status': 'failed', 'success': False, 'message': str(exc)}, only_unsubmitted=True)
        _update(manager, agent_id, endpoint_id, status='failed', message=str(exc))
    except Exception:
        _log.exception('Managed node-test DNS operation needs confirmation')
        # A transport/server error may occur after submission. Keep the same
        # deployment ID so the next poll reconciles it instead of duplicating.
        _update(manager, agent_id, endpoint_id, message='操作结果待确认，请刷新状态或查看 Agent 发布记录')
    finally:
        if reserved:
            manager.finish_deployment_preparation(agent_id, reserved)


def guard_agent_change(agent, updates=None):
    if not agent or not any(r.get('status') != 'removed' for r in agent.get(FIELD, {}).values()):
        return
    if updates is None or any(key in updates and updates[key] != agent.get(key)
                              for key in ('host', 'port', 'enabled', 'service_type', 'profile_id')):
        raise ValueError('请先删除此 Agent 的托管测速 DNS 接口，再删除或修改 Agent 绑定')


def deployment_conflict(agent, deployment_id):
    return any(record.get('status') in ('preparing', 'publishing', 'verifying', 'removing')
               and record.get('deployment_id') != deployment_id for record in agent.get(FIELD, {}).values())


def guard_system_change(before, after):
    """Protect ownership against all Agent mutation paths under the DB lock."""
    current = {a['id']: a for a in after.get('agents', [])}
    for agent in before.get('agents', []):
        replacement = current.get(agent['id'])
        guard_agent_change(agent, replacement)
        for endpoint_id, record in agent.get(FIELD, {}).items():
            if record.get('status') != 'removed' and endpoint_id not in (replacement or {}).get(FIELD, {}):
                raise ValueError('请通过托管 DNS 删除操作移除接口记录')
    for agent in current.values():
        if any(r.get('status') != 'removed' for r in agent.get(FIELD, {}).values()):
            _exclusive(after, agent)
            _check_agent(agent)


def guard_profile_change(system, profile_id, before, after):
    for agent in system.get('agents', []):
        if agent.get('profile_id', 'default') != profile_id:
            continue
        for record in agent.get(FIELD, {}).values():
            if record['status'] in ('removed', 'removing'):
                continue
            document = _document(after.get('oxidns'))
            if record.get('installed'):
                _owned(document, record)


def strip_owned_for_clone(system, profile_id, profile):
    """A cloned space must not inherit another device's private listener IP."""
    for agent in system.get('agents', []):
        if agent.get('profile_id', 'default') != profile_id:
            continue
        for record in agent.get(FIELD, {}).values():
            if record.get('status') == 'removed':
                continue
            settings = profile['oxidns']
            document = _patch(_document(settings), record, 'remove')
            settings['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
