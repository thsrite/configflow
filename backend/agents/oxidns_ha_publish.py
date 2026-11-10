"""Two-member OxiDNS publication, using the existing per-device transactions.

The journal on both Agent records reserves the pair across web workers and
process restarts. A private immutable bundle is materialized once. Only the
standby's confirmed activation and DNS health permit touching the other core.
"""
from contextlib import contextmanager, ExitStack
from copy import deepcopy
import fcntl
import hashlib
import ipaddress
import json
import logging
import os
from pathlib import Path
import threading
import time
import uuid

from .deployment_bundle import DeploymentBundle, _pack, deployment_content_fingerprint
from .diagnostics import failure_text
from .idle_rounds import IdleRounds
from . import rule_sync_store as store
from . import oxidns_ha_handoff as handoffs

FIELD = 'oxidns_ha_publication'
TERMINAL = {'succeeded', 'failed', 'rollback_failed'}
CHILD_TERMINAL = {'succeeded', 'failed', 'rolled_back', 'rollback_failed'}
_log = logging.getLogger(__name__)
_loops = set()
_loop_guard = threading.Lock()


class HAStatusUnavailable(RuntimeError):
    pass


def active(agent):
    job = (agent or {}).get(FIELD) or {}
    return bool(job) and job.get('status') not in TERMINAL


def conflict(agent, deployment_id=None):
    job = (agent or {}).get(FIELD) or {}
    if active(agent) and job.get('children', {}).get(agent.get('id')) != deployment_id:
        return {'success': False, 'http_status': 409, 'message': 'DNS 主备正在同步，请等待两台发布完成',
                'ha_publication': summary(job)}
    return None


def service_operation(manager, agent_id, operation, *, uninstall=False):
    with _lock(manager, {'children': {agent_id: ''}}) as acquired:
        if not acquired:
            return {'success': False, 'http_status': 409, 'message': 'DNS 主备正在处理，请等待两台确认完成'}
        agent = manager.get_agent_by_id(agent_id) or {}
        rejection = conflict(agent)
        if rejection:
            return rejection
        if uninstall and (agent.get('oxidns_ha') or {}).get('enabled') is True:
            return {'success': False, 'http_status': 409, 'message': '请先停用 DNS 高可用，再卸载成员'}
        return operation()


def pair(manager, agent_id):
    agent = manager.get_agent_by_id(agent_id)
    ha = (agent or {}).get('oxidns_ha') or {}
    if ha.get('enabled') is not True:
        return None
    peer = manager.get_agent_by_id(ha.get('peer_id'))
    other = (peer or {}).get('oxidns_ha') or {}
    if (not peer or other.get('enabled') is not True or not ha.get('group_id')
            or other.get('group_id') != ha['group_id'] or other.get('peer_id') != agent_id
            or peer.get('profile_id') != agent.get('profile_id')):
        raise ValueError('DNS 高可用配对不完整，请先修复主备关系')
    return [agent, peer]


def summary(job):
    return {key: deepcopy(job[key]) for key in ('id', 'status', 'phase', 'message', 'owner_agent_id',
        'owner_deployment_id', 'active_agent_id', 'members', 'updated_at', 'warnings', 'routers') if key in job}


def _root(manager, key):
    root = Path(manager.repository.data_dir) / '.oxidns-ha-publications' / hashlib.sha256(key.encode()).hexdigest()
    root.mkdir(parents=True, mode=0o700, exist_ok=True)
    return root


@contextmanager
def _lock(manager, job):
    # Member locks also exclude a restart that began before a first HA enable
    # reserved the pair. Stable ordering prevents cross-pair lock inversion.
    keys = ['pair:' + '\n'.join(sorted(job['children']))] + ['member:' + key for key in sorted(job['children'])]
    with ExitStack() as stack:
        for key in keys:
            handle = stack.enter_context((_root(manager, key) / 'lock').open('a+b'))
            try:
                fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                yield False
                return
        yield True


def _folder(manager, job):
    return _root(manager, '\n'.join(sorted(job['children'])))


def _write_payload(manager, job, payload, bundle=None):
    folder = _folder(manager, job)
    if bundle is not None:
        store._write(folder / 'bundle.tar.gz', bundle.archive)
        payload = {**payload, 'bundle': {'manifest': bundle.manifest, 'sha256': bundle.sha256,
            'config_version': bundle.config_version, 'comparison_config': bundle.comparison_config}}
    store._write(folder / 'payload.json', json.dumps({**payload, 'id': job['id']}, ensure_ascii=False).encode())
    return payload


def _read_payload(manager, job):
    folder = _folder(manager, job)
    payload = json.loads((folder / 'payload.json').read_bytes())
    if payload.get('id') != job['id']:
        raise ValueError('主备发布资料与当前任务不匹配，请重新发布')
    bundle = None
    if payload.get('bundle'):
        meta = payload['bundle']
        archive = (folder / 'bundle.tar.gz').read_bytes()
        if hashlib.sha256(archive).hexdigest() != meta['sha256']:
            raise ValueError('主备发布资料校验失败，请重新发布')
        bundle = DeploymentBundle(meta['manifest'], archive, meta['sha256'], meta['config_version'],
                                  meta.get('comparison_config'))
    return payload, bundle


def _save(manager, job):
    job = deepcopy(job)
    job['updated_at'] = int(time.time())
    def save(agents):
        by_id = {item['id']: item for item in agents}
        members = []
        order = job.get('order')
        if not order:
            if job.get('enable'):
                order = [job['enable']['peer_id'], job['owner_agent_id']]
            else:
                order = sorted(job['children'], key=lambda key:
                    (by_id.get(key, {}).get('oxidns_ha') or {}).get('role') != 'secondary')
        for agent_id in order:
            deployment_id = job['children'][agent_id]
            agent = by_id.get(agent_id)
            if not agent or (agent.get(FIELD) or {}).get('id') != job['id']:
                raise ValueError('主备发布记录已经改变，已停止继续操作')
            state = agent.get('deployments', {}).get(deployment_id) or {}
            member_status = state.get('status', 'preparing')
            if not state.get('submitted') and agent_id != job.get('active_agent_id'):
                member_status = 'skipped' if member_status == 'failed' else 'waiting'
            members.append({'agent_id': agent_id, 'name': agent.get('name') or agent_id,
                            'deployment_id': deployment_id, 'status': member_status})
        job['members'] = members
        for agent_id, deployment_id in job['children'].items():
            agent = by_id[agent_id]
            agent[FIELD] = deepcopy(job)
            state = agent.get('deployments', {}).get(deployment_id)
            if state is not None:
                state['ha_publication'] = summary(job)
                if (agent.get('latest_deployment') or {}).get('deployment_id') == deployment_id:
                    agent['latest_deployment'] = deepcopy(state)
    manager._update_agents(save)
    return job


def _identity(agent):
    return {key: agent.get(key) for key in ('host', 'port', 'profile_id', 'service_type', 'enabled')}


def _reserve(manager, members, owner_id, deployment_id, *, activate, enable=None):
    children = {member['id']: (deployment_id if member['id'] == owner_id else
        'ha-' + hashlib.sha256((owner_id + '/' + deployment_id + '/' + member['id']).encode()).hexdigest()[:48])
        for member in members}
    job = {'id': uuid.uuid4().hex, 'owner_agent_id': owner_id, 'owner_deployment_id': deployment_id,
           'children': children, 'identities': {item['id']: _identity(item) for item in members},
           'status': 'preparing', 'phase': 'preparing', 'message': '正在准备同一份主备配置与规则',
           'active_agent_id': None, 'activate': activate, 'order': [], 'enable': enable,
           'updated_at': int(time.time()), 'handoff_protocol': 1}
    def reserve(agents):
        by_id = {item['id']: item for item in agents}
        for member in members:
            current = by_id.get(member['id'])
            if not current or _identity(current) != _identity(member) or current.get('oxidns_ha') != member.get('oxidns_ha'):
                raise ValueError('主备设备信息已改变，请刷新后重试')
            if active(current):
                raise ValueError('DNS 主备已有发布正在执行，请等待两台完成')
            if current.get('enabled') is False:
                raise ValueError('主备设备已停用，请先启用后发布')
            upgrade = current.get('latest_upgrade') or {}
            if upgrade and upgrade.get('status') not in CHILD_TERMINAL:
                raise ValueError('主备设备正在升级，请等待完成')
            for state in current.get('deployments', {}).values():
                if state.get('status') not in CHILD_TERMINAL and not (
                        current['id'] == owner_id and state.get('deployment_id') == deployment_id):
                    raise ValueError('主备设备已有未完成的发布，请先处理')
        for member in members:
            by_id[member['id']][FIELD] = deepcopy(job)
    manager._update_agents(reserve)
    return job


def _begin_children(manager, job):
    for agent_id, deployment_id in job['children'].items():
        result = manager.begin_deployment(agent_id, deployment_id)
        if not result.get('success'):
            raise ValueError(result.get('message') or '无法预留主备发布')
        # The durable pair journal owns unsent work; a process-local preparation
        # lock must not be mistaken for a crashed single-device request.
        manager.finish_deployment_preparation(agent_id, deployment_id)


def start_enable(manager, agent, peer, request, *, base_url='', warnings=None, routers=None):
    existing = agent.get(FIELD) or {}
    if active(agent):
        if existing.get('enable') != request or set(existing.get('children', {})) != {agent['id'], peer['id']}:
            raise ValueError('DNS 主备已有发布正在执行，请先等待完成')
        start_loop(manager)
        return summary(existing)
    source, revision = manager.repository.get_publish_snapshot(agent.get('profile_id') or manager.repository.default_profile_id())
    temporary = {'children': {agent['id']: '', peer['id']: ''}}
    with _lock(manager, temporary) as acquired:
        if not acquired:
            raise ValueError('DNS 主备正在处理，请稍后重试')
        job = _reserve(manager, [agent, peer], agent['id'], 'ha-' + uuid.uuid4().hex,
                       activate=True, enable=deepcopy(request))
        from .rule_sync import settings_for
        job.update(warnings=warnings or [], routers=routers or [], rule_sync_settings=settings_for(agent))
        try:
            _write_payload(manager, job, {'source_snapshot': source, 'resource_config': source,
                                        'config_revision': revision, 'base_url': base_url})
            _begin_children(manager, job)
            job = _save(manager, job)
        except Exception:
            _fail(manager, job, '主备同步准备失败，请重新开启')
            raise
    start_loop(manager)
    return summary(job)


def publish(manager, agent_id, bundle, *, activate=True, config_revision=None, base_url='',
            resource_config=None, source_snapshot=None):
    members = pair(manager, agent_id)
    if members is None:
        return manager._publish_deployment(agent_id, bundle, activate=activate, config_revision=config_revision,
            base_url=base_url, resource_config=resource_config, source_snapshot=source_snapshot)
    previous = (manager.get_agent_by_id(agent_id) or {}).get(FIELD) or {}
    if previous.get('children', {}).get(agent_id) == bundle.manifest['deployment_id']:
        payload, original = _read_payload(manager, previous)
        if original is None or deployment_content_fingerprint(original.manifest) != deployment_content_fingerprint(bundle.manifest):
            raise ValueError('发布任务 ID 已用于其他内容，请使用新的发布任务')
        advance(manager, agent_id)
        return deployment(manager, agent_id, bundle.manifest['deployment_id'])
    if previous.get('status') == 'rollback_failed':
        advance(manager, agent_id)
    temporary = {'children': {item['id']: '' for item in members}}
    with _lock(manager, temporary) as acquired:
        if not acquired:
            raise ValueError('DNS 主备正在处理，请稍后重试')
        # Automatic rule-only publications may not copy one device's unrelated
        # settings over a different peer baseline.
        follow = (manager.get_agent_by_id(agent_id) or {}).get('dns_follow_state') or {}
        if bundle.manifest.get('reload_only') or (follow.get('status') == 'publishing'
                and follow.get('target_deployment_id') == bundle.manifest['deployment_id']):
            fingerprints = {(item.get('applied_content') or {}).get('fingerprint') for item in members}
            if len(fingerprints) != 1 or not next(iter(fingerprints), None):
                raise ValueError('主备配置基线不同，请先完整发布一次主备配置，再自动同步规则')
        job = _reserve(manager, members, agent_id, bundle.manifest['deployment_id'], activate=activate)
        try:
            _write_payload(manager, job, {'config_revision': config_revision, 'base_url': base_url,
                'resource_config': resource_config, 'source_snapshot': source_snapshot}, bundle)
            _begin_children(manager, job)
            _save(manager, job)
        except Exception:
            _fail(manager, job, '主备同步准备失败，请重新发布')
            raise
    start_loop(manager)
    advance(manager, agent_id)
    return deployment(manager, agent_id, bundle.manifest['deployment_id'], reconcile=False)


def _fail(manager, job, message, *, status='failed'):
    pending = any((state := (manager.get_agent_by_id(agent_id) or {}).get('deployments', {}).get(deployment_id, {})).get('submitted')
                  and state.get('status') not in CHILD_TERMINAL | {'ready'}
                  for agent_id, deployment_id in job['children'].items())
    if pending:
        job.update(status='unknown', message=message + '；已提交的设备结果仍待确认，未继续另一台')
        return _save(manager, job)
    if job.get('handoff'):
        job['handoff_failure'] = {'message': message, 'status': status}
        _save(manager, job)
        try:
            handoffs.release(manager, job)
            handoffs.settle_failure(manager, job)
        except handoffs.HandoffPending as error:
            job.update(status='unknown', phase='handoff', message=message + '；' + str(error))
            return _save(manager, job)
    # Never rewrite a submitted child's outcome; its own transaction remains
    # authoritative and can still be reconciled or recover its automatic rollback.
    for agent_id, deployment_id in job['children'].items():
        agent = manager.get_agent_by_id(agent_id) or {}
        state = agent.get('deployments', {}).get(deployment_id)
        if state and state.get('status') not in CHILD_TERMINAL and (not state.get('submitted') or state.get('status') == 'ready'):
            # A staged, never activated candidate can be retired locally. The
            # Agent permits a new transaction; old activation stays group-guarded.
            manager.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'failed',
                'message': '主备发布未继续：' + message}, only_unsubmitted=not state.get('submitted'))
        manager.finish_deployment_preparation(agent_id, deployment_id)
    job.update(status=status, message=message)
    return _save(manager, job)


def _members(manager, job):
    members = {key: manager.get_agent_by_id(key) for key in job['children']}
    if any(not item or _identity(item) != job['identities'][key] for key, item in members.items()):
        raise ValueError('主备设备信息在发布期间改变，已停止继续操作')
    return members


def _runtime(manager, agent):
    try:
        result = manager.oxidns_ha_status(agent)
    except Exception as error:
        raise HAStatusUnavailable('无法确认主备的 DNS 健康和虚拟 IP 状态') from error
    runtime = result.get('runtime')
    if not isinstance(runtime, dict) or result.get('error'):
        raise HAStatusUnavailable('无法确认主备的 DNS 健康和虚拟 IP 状态')
    return runtime


def _order(manager, job, members):
    if job.get('enable') and not all((item.get('oxidns_ha') or {}).get('enabled') is True for item in members.values()):
        return [job['enable']['peer_id'], job['owner_agent_id']]
    runtimes = {key: _runtime(manager, member) for key, member in members.items()}
    holders = [key for key, runtime in runtimes.items() if runtime.get('vip_present') is True]
    if len(holders) != 1:
        raise ValueError('无法确认唯一持有虚拟 IP 的主机，未更新任何核心')
    return [key for key in members if key != holders[0]] + holders


def _require_listener(bundle):
    """Mirror the Agent's HA listener preflight against the new immutable file."""
    import yaml
    document = yaml.safe_load(store.bundle_files(bundle)[bundle.manifest['config_path']][0])
    for plugin in document.get('plugins', []):
        if not isinstance(plugin, dict) or plugin.get('type') != 'udp_server':
            continue
        args = plugin.get('args') or {}
        listen = args.get('listen') if isinstance(args, dict) else None
        if not isinstance(listen, str):
            continue
        host, separator, port = listen.strip().rpartition(':')
        if not separator or port != '53':
            continue
        if host in ('', '*'):
            return
        try:
            if ipaddress.ip_address(host.strip('[]')).is_unspecified:
                return
        except ValueError:
            pass
    raise ValueError('主备候选配置必须保留通配地址的 UDP 53 监听（如 0.0.0.0:53），未更新任何核心')


def _child_bundle(bundle, agent_id, deployment_id):
    manifest = deepcopy(bundle.manifest)
    manifest.update(agent_id=agent_id, deployment_id=deployment_id)
    return _pack(manifest, store.bundle_files(bundle), bundle.config_version,
                 comparison_config=bundle.comparison_config)


def _require_container_environment(manager, members):
    # Re-check at mutation boundaries, including rule-only and resumed/staged
    # transactions. A saved handoff decision is not current Docker readiness.
    if any(member.get('deployment_method') == 'docker' for member in members.values()):
        from .oxidns_ha import _require_pair_protocols
        _require_pair_protocols(manager, list(members.values()))


def _step(manager, job):
    if job.get('handoff_failure'):
        failure = job['handoff_failure']
        return _fail(manager, job, failure['message'], status=failure['status'])
    members = _members(manager, job)
    payload, bundle = _read_payload(manager, job)
    if bundle is None and job.get('kind') == 'rules':
        from .rule_sync import prepare_candidate
        from .rule_sync_snapshot import fetch_snapshot
        owner = members[job['owner_agent_id']]
        baseline = store.load(manager.repository, owner)
        if baseline is None or baseline['manifest']['deployment_id'] != payload.get('baseline_deployment_id'):
            raise ValueError('主备规则更新基线已改变，请重新发布配置')
        baseline = {**baseline, 'bundle': fetch_snapshot(manager, owner, baseline)}
        bundle = prepare_candidate(owner, payload['source_snapshot'], baseline, {'library': True, 'rules': False},
                                   job['owner_deployment_id'], force_refresh=True)
        payload = _write_payload(manager, job, payload, bundle)
    if bundle is None:
        # Enable captures a profile once, then uses the same normal materializer
        # as a manual publication. No core is contacted before it succeeds.
        from backend.routes.agents import _prepare_transactional_content
        owner = members[job['owner_agent_id']]
        prepared = _prepare_transactional_content(owner, payload['source_snapshot'],
            base_url=payload['base_url'], deployment_id=job['owner_deployment_id'])
        bundle = prepared['bundle']
        payload = _write_payload(manager, job, payload, bundle)
    _require_listener(bundle)
    handoffs.prepare(manager, job, members, bundle)
    if not job['order']:
        job['order'] = _order(manager, job, members)
        _save(manager, job)
    # With restart=false both immutable candidates may be staged safely. Their
    # activation still runs one at a time when either member is activated.
    for index, agent_id in enumerate(job['order']):
        deployment_id = job['children'][agent_id]
        agent = members[agent_id]
        state = agent.get('deployments', {}).get(deployment_id) or {}
        job.update(active_agent_id=agent_id, phase='standby' if index == 0 else 'primary')
        if index and job['activate']:
            first_id = job['order'][0]
            first = manager._get_deployment(first_id, job['children'][first_id])
            if first.get('status') != 'succeeded' or first.get('pending'):
                job.update(status='unknown' if first.get('pending') else 'applying', message='等待备机确认发布成功')
                return _save(manager, job)
            if _runtime(manager, members[first_id]).get('healthy') is not True:
                return _fail(manager, job, '备机发布后的 DNS 健康检查未通过，主机未更新')
        if not state.get('submitted'):
            if state.get('status') in CHILD_TERMINAL:
                return _fail(manager, job, state.get('message') or '主备发布已中止')
            _require_container_environment(manager, members)
            if index and job['activate']:
                handoffs.ensure(manager, job, members)
            job.update(active_agent_id=agent_id, phase='standby' if index == 0 else 'primary')
            job.update(status='applying' if job['activate'] else 'preparing', message=(
                ('正在更新未持有虚拟 IP 的设备：' if index == 0 else '正在准备原持有虚拟 IP 的设备：')
                + (agent.get('name') or agent_id)) if job.get('handoff_required') else
                ('正在发布备机' if index == 0 else '正在发布主机'))
            _save(manager, job)
            # Upload/validation can outlast the last VIP probe. Stage the old
            # holder, then verify handoff again immediately before activation.
            activate_now = job['activate'] and not (index and job.get('handoff_required'))
            result = manager._publish_deployment(agent_id, _child_bundle(bundle, agent_id, deployment_id),
                activate=activate_now, config_revision=payload.get('config_revision'),
                base_url=payload.get('base_url', ''), resource_config=payload.get('resource_config'),
                source_snapshot=payload.get('source_snapshot'))
        else:
            result = manager._get_deployment(agent_id, deployment_id)
        if result.get('pending') or result.get('status') == 'unknown':
            job.update(status='unknown', message='正在确认' + ('备机' if index == 0 else '主机') + '发布结果，暂不继续')
            return _save(manager, job)
        if result.get('status') in ('failed', 'rolled_back', 'rollback_failed'):
            return _fail(manager, job, ('备机' if index == 0 else '主机') + '发布失败：' +
                failure_text(result.get('message') or result.get('error') or '请查看设备发布记录',
                             tuple(item.get('token') for item in members.values())),
                status='rollback_failed' if result.get('status') == 'rollback_failed' else 'failed')
        if result.get('status') == 'ready' and job['activate']:
            _require_container_environment(manager, members)
            if index:
                handoffs.ensure(manager, job, members)
            job.update(active_agent_id=agent_id, phase='standby' if index == 0 else 'primary')
            if index and job.get('handoff_required'):
                job['message'] = '虚拟 IP 解析已确认，正在更新原持有设备：' + (agent.get('name') or agent_id)
            _save(manager, job)
            result = manager._activate_deployment(agent_id, deployment_id)
            if result.get('http_status') in (400, 404, 405):
                return _fail(manager, job, result.get('message') or '主备候选激活被拒绝')
            # Do not activate the next member from a mere accepted response.
            if result.get('status') != 'succeeded' or result.get('pending'):
                job.update(status='unknown' if result.get('status') == 'unknown' else 'applying',
                           message='等待' + ('备机' if index == 0 else '主机') + '激活及 DNS 健康检查')
                return _save(manager, job)
        elif result.get('status') not in ('succeeded', 'ready'):
            job.update(status='applying' if job['activate'] else 'preparing',
                       message='等待' + ('备机' if index == 0 else '主机') + '发布完成')
            return _save(manager, job)
        if job['activate'] and result.get('status') != 'succeeded':
            return _save(manager, job)
    if not job['activate']:
        job.update(status='ready', message='两台候选配置已准备，激活时将先备机后主机', active_agent_id=None)
        return _save(manager, job)
    # Succeeded means both transactions and a fresh DNS check on both devices.
    for member in members.values():
        if _runtime(manager, member).get('healthy') is not True:
            return _fail(manager, job, '主备配置已应用，但 DNS 健康检查未通过，请查看设备状态')
    handoffs.finish(manager, job, members)
    if job.get('enable'):
        job.update(phase='keepalived', status='applying', message='配置已同步，正在启用主备虚拟 IP')
        _save(manager, job)
        from .oxidns_ha import _configure_keepalived
        _configure_keepalived(manager, job['owner_agent_id'], job['enable'], rule_sync_settings=job.get('rule_sync_settings'))
    job.update(status='succeeded', phase='done', active_agent_id=None, message='主备配置与规则均已发布成功')
    return _save(manager, job)


def rules(manager, agent_id, method):
    """The existing rules endpoint represents a pair's pinned rule-only release."""
    members = pair(manager, agent_id)
    if members is None:
        raise ValueError('主备关系不可用')
    agent = manager.get_agent_by_id(agent_id)
    job = agent.get(FIELD) or {}
    if method == 'POST' and not (active(agent) and job.get('kind') == 'rules'):
        if active(agent):
            raise ValueError('DNS 主备正在发布，请等待两台完成后更新规则')
        baseline = store.load(manager.repository, agent)
        fingerprints = {(item.get('applied_content') or {}).get('fingerprint') for item in members}
        if baseline is None or len(fingerprints) != 1 or not next(iter(fingerprints), None):
            raise ValueError('请先完整发布相同的主备配置，再更新规则')
        with _lock(manager, {'children': {item['id']: '' for item in members}}) as acquired:
            if not acquired:
                raise ValueError('DNS 主备正在处理，请稍后重试')
            job = _reserve(manager, members, agent_id, 'rules-' + uuid.uuid4().hex, activate=True)
            job['kind'] = 'rules'
            try:
                _write_payload(manager, job, {'source_snapshot': baseline['source'],
                    'resource_config': baseline['source'], 'base_url': baseline.get('base_url', ''),
                    'baseline_deployment_id': baseline['manifest']['deployment_id']})
                _begin_children(manager, job)
                _save(manager, job)
            except Exception:
                _fail(manager, job, '主备规则更新准备失败，请重新发布配置')
                raise
    if job.get('kind') != 'rules':
        return {'success': True, 'task': {'id': '', 'status': 'idle', 'phase': 'idle', 'downloaded': 0,
                        'download_total': 0, 'reloaded': 0, 'reload_total': 0}}, 200
    start_loop(manager)
    advance(manager, agent_id)
    job = (manager.get_agent_by_id(agent_id) or {}).get(FIELD) or job
    state = 'succeeded' if job['status'] == 'succeeded' else 'failed' if job['status'] in TERMINAL else 'running'
    task = {'id': 'ha-' + job['id'], 'status': state,
            'phase': 'done' if state != 'running' else 'downloading' if job['phase'] == 'preparing' else 'reloading',
            'downloaded': 0, 'download_total': 0, 'reloaded': 0, 'reload_total': 0}
    if state == 'failed':
        task.update(code='RULE_UPDATE_FAILED', message=job['message'])
    return {'success': True, 'task': task, 'ha_publication': summary(job)}, 202 if state == 'running' else 200


def _reconcile_recovery(manager, job):
    unresolved = False
    for agent_id, deployment_id in job['children'].items():
        state = (manager.get_agent_by_id(agent_id) or {}).get('deployments', {}).get(deployment_id) or {}
        if state.get('status') == 'rollback_failed':
            state = manager._get_deployment(agent_id, deployment_id)
        if state.get('status') not in ('succeeded', 'failed', 'rolled_back') or state.get('pending'):
            unresolved = True
    if not unresolved:
        job.update(status='failed', message='设备回滚恢复已确认；原主备发布未完成，可重新发布')
    return _save(manager, job)


def advance(manager, agent_id):
    agent = manager.get_agent_by_id(agent_id) or {}
    job = agent.get(FIELD) or {}
    if not job or job.get('status') in {'succeeded', 'failed', 'ready'}:
        return summary(job) if job else None
    with _lock(manager, job) as acquired:
        if not acquired:
            return summary(job)
        job = deepcopy((manager.get_agent_by_id(agent_id) or {}).get(FIELD) or {})
        if not job or job.get('status') in {'succeeded', 'failed', 'ready'}:
            return summary(job) if job else None
        try:
            job = _reconcile_recovery(manager, job) if job.get('status') == 'rollback_failed' else _step(manager, job)
        except handoffs.HandoffPending as error:
            job.update(status='unknown' if error.unknown else 'checking', phase='handoff', message=str(error))
            job = _save(manager, job)
        except Exception as error:
            # A transport error while reading live health can be retried without
            # publishing the next member. Preparation errors are definite.
            import requests
            if isinstance(error, (requests.RequestException, HAStatusUnavailable)):
                job.update(status='unknown', message='主备设备连接暂不可用，正在等待确认，未继续更新下一台')
                job = _save(manager, job)
            else:
                message = failure_text(str(error), tuple((manager.get_agent_by_id(key) or {}).get('token')
                                                        for key in job['children'])) if isinstance(error, ValueError) else '主备发布处理失败，请查看配置与设备状态后重试'
                _log.warning('OxiDNS pair publication stopped (%s)', type(error).__name__)
                job = _fail(manager, job, message)
        return summary(job)


def deployment(manager, agent_id, deployment_id, *, reconcile=True):
    agent = manager.get_agent_by_id(agent_id) or {}
    job = agent.get(FIELD) or {}
    if job.get('children', {}).get(agent_id) != deployment_id:
        record = manager._get_deployment(agent_id, deployment_id)
        public = record.get('ha_publication')
        if public:
            return _aggregate(record, public)
        return record
    if reconcile:
        advance(manager, agent_id)
        agent = manager.get_agent_by_id(agent_id) or {}
        job = agent.get(FIELD) or {}
    record = agent.get('deployments', {}).get(deployment_id) or {'deployment_id': deployment_id}
    return _aggregate(record, summary(job))


def _aggregate(record, public):
    result = {**record, 'ha_publication': public, 'status': public['status'], 'message': public['message'],
              'success': public['status'] not in ('failed', 'rollback_failed')}
    result.pop('http_status', None)
    result.pop('pending', None)
    return result


def activate(manager, agent_id, deployment_id):
    agent = manager.get_agent_by_id(agent_id) or {}
    job = deepcopy(agent.get(FIELD) or {})
    if job.get('children', {}).get(agent_id) != deployment_id:
        rejection = conflict(agent)
        return rejection or manager._activate_deployment(agent_id, deployment_id)
    with _lock(manager, job) as acquired:
        if not acquired:
            return {'success': False, 'http_status': 409, 'message': '主备发布正在处理，请稍后确认'}
        job = deepcopy((manager.get_agent_by_id(agent_id) or {}).get(FIELD) or {})
        if job.get('status') == 'succeeded':
            return deployment(manager, agent_id, deployment_id, reconcile=False)
        if job.get('status') != 'ready':
            return {'success': False, 'http_status': 409, 'message': '两台候选配置尚未就绪，请等待主备准备完成'}
        try:
            job['order'] = _order(manager, job, _members(manager, job))
        except (ValueError, HAStatusUnavailable):
            return {'success': False, 'http_status': 409, 'message': '无法确认当前主备角色，未激活任何候选配置'}
        job.update(activate=True, status='applying', message='正在依次激活备机和主机')
        _save(manager, job)
    start_loop(manager)
    advance(manager, agent_id)
    return deployment(manager, agent_id, deployment_id, reconcile=False)


_rounds = IdleRounds()


def reconcile(manager):
    """Advance every unfinished pair publication once."""
    head = _rounds.due(manager.repository)
    if head is None:
        return
    seen = set()
    for agent in manager.get_all_agents():
        job = agent.get(FIELD) or {}
        if (active(agent) or job.get('status') == 'rollback_failed') and job.get('status') != 'ready' and job.get('id') not in seen:
            seen.add(job['id'])
            _rounds.done(manager.repository, head, False)
            advance(manager, agent['id'])
    # A job is stored with its devices, so none can appear without a commit.
    _rounds.done(manager.repository, head, not seen)


def start_loop(manager):
    """Resume durable pair work independently of an open browser tab."""
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
                    reconcile(manager)
                except Exception as error:
                    _log.warning('OxiDNS pair publication reconciliation failed (%s)', type(error).__name__)
                finally:
                    config_module.reset_config_context()
                time.sleep(2)
        finally:
            with _loop_guard:
                _loops.discard(key)
    threading.Thread(target=work, daemon=True, name='oxidns-ha-publish').start()
