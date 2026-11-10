"""Scoped, read-only checks after a confirmed Mihomo publication.

Reports are persisted separately from deployment success. A reachable HTTP
endpoint does not, by itself, prove the client's route or IPv6 proxy transport.
"""
from concurrent.futures import ThreadPoolExecutor
from copy import deepcopy
from datetime import datetime, timezone
import logging
import math
import re
import threading
import time
import uuid
from urllib.parse import urlsplit

from backend.agents import ipv6_readiness as readiness
from backend.agents.idle_rounds import IdleRounds
from backend.agents.oxidns_monitor import MonitorError
from backend.utils.health_check import validate_health, GROUP_DEFAULTS

logger = logging.getLogger(__name__)
_executor = ThreadPoolExecutor(max_workers=2, thread_name_prefix='ipv6-verification')
_slots = threading.BoundedSemaphore(8)
_loops = set()
_loop_lock = threading.Lock()
_TERMINAL = {'succeeded', 'failed', 'rolled_back', 'rollback_failed'}
NOT_APPLICABLE = '未启用 IPv6，无需验收'


def _now():
    return datetime.now(timezone.utc).isoformat()


def _check(key, label, status, message, details=None):
    return {'id': key, 'label': label, 'status': status, 'message': message,
            **({'details': details} if details else {})}


def _identity(agent):
    return (*tuple(agent.get(key) for key in readiness.AGENT_FIELDS),
            agent.get('enabled') is not False, agent.get('gateway_installation_id'))


def _applied(agent):
    latest = agent.get('latest_deployment') or {}
    return {'latest': {key: latest.get(key) for key in
                       ('deployment_id', 'status', 'manual_rollback', 'config_version', 'config_revision')},
            'config_revision': agent.get('config_revision'), 'config_version': agent.get('config_version'),
            'applied_content': agent.get('applied_content') or {}}


def _client_fingerprint(manager, source, agents=None):
    ident = _settings(source)['client_agent_id']
    client = (next((a for a in agents if a.get('id') == ident), None) if agents is not None
              else manager.get_agent_by_id(ident)) if ident else None
    return readiness._digest({'identity': _identity(client), 'applied': _applied(client)}) if client else None


def _findings(manager, source, report, agents=None):
    """Compare durable inputs, never volatile heartbeats or this report itself."""
    changed = []
    if report.get('settings_fingerprint') != readiness._digest(_settings(source)):
        changed.append('验收设置已变化')
    if report.get('target_fingerprint') != readiness._digest(_identity(source)):
        changed.append('目标设备已变化')
    if report.get('applied_fingerprint') != readiness._digest(_applied(source)):
        changed.append('设备发布版本已变化')
    if report.get('client_fingerprint') != _client_fingerprint(manager, source, agents):
        changed.append('检测客户端已变化')
    if not report.get('deployment_id'):
        try:
            if report.get('config_revision') != manager.repository.config_revision(report['profile_id']):
                changed.append('已保存配置已变化')
        except (KeyError, ValueError):
            changed.append('配置空间已变化')
    return changed


def _expired(report):
    if report.get('status') != 'running':
        return False
    try:
        started = float(report.get('started_epoch', 0))
        return not math.isfinite(started) or time.time() - started > 300 or started > time.time() + 30
    except (ValueError, TypeError):
        return True


def _interrupt(report, message='上次验收超时或服务中断，请重新验收'):
    report.update(status='error', checked_at=_now(), message=message,
                  checks=[_check('interrupted', '验收未完成', 'error', '未收到完整结果，不能判断连接成功')])


def _profile(manager, agent):
    return agent.get('profile_id') or manager.repository.default_profile_id()


def _settings(agent):
    saved = agent.get('ipv6_verification_settings') or {}
    return {'client_agent_id': saved.get('client_agent_id') or '', 'test_url': saved.get('test_url') or ''}


def _capabilities(manager, agent):
    try:
        return manager._deployment_request(agent, 'get', 'capabilities', timeout=(2, 3))
    except Exception:
        return {}


def _client(manager, source, ident, *, capabilities=False):
    client = manager.get_agent_by_id(ident)
    if (not client or client.get('id') == source['id']
            or _profile(manager, client) != _profile(manager, source)
            or str(client.get('host', '')).lower() == str(source.get('host', '')).lower()
            or client.get('deployment_method') != 'shell' or client.get('service_type') == 'surge'):
        raise MonitorError('CLIENT_UNAVAILABLE', 409, '检测客户端必须是同一配置空间中另一台独立的 Linux 主机')
    if client.get('enabled') is False or client.get('status') != 'online':
        raise MonitorError('CLIENT_UNAVAILABLE', 409, '原检测客户端已离线或禁用，请恢复后重新验收')
    if capabilities:
        cap = _capabilities(manager, client)
        if cap.get('platform') != 'linux' or 1 not in cap.get('ipv6_client_probe_protocols', []):
            raise MonitorError('CLIENT_UPGRADE_REQUIRED', 409, '请更新检测客户端 Agent，使其支持客户端链路验收')
    return client


def settings(manager, agent_id, profile_id, values=None):
    source = readiness._agent(manager, agent_id, profile_id)
    if values is not None:
        if set(values) != {'client_agent_id', 'test_url'} or any(not isinstance(v, str) for v in values.values()):
            raise MonitorError('INVALID_REQUEST', 400, '请提供检测客户端和测试网址')
        values = {k: v.strip() for k, v in values.items()}
        if len(values['client_agent_id']) > 200 or len(values['test_url']) > 2048:
            raise MonitorError('INVALID_REQUEST', 400, '验收设置过长')
        if values['test_url']:
            try:
                validate_health({'url': values['test_url']})
            except ValueError as error:
                raise MonitorError('INVALID_REQUEST', 400, str(error)) from None
        if values['client_agent_id']:
            _client(manager, source, values['client_agent_id'], capabilities=True)
        def save(agents):
            current = next((a for a in agents if a['id'] == agent_id), None)
            if not current or _identity(current) != _identity(source):
                raise MonitorError('PROFILE_CHANGED', 409, '设备信息已变化，请重新打开设置')
            if any(r.get('status') == 'running' for r in (current.get('ipv6_verifications') or {}).values()):
                raise MonitorError('BUSY', 409, '验收正在执行，请结束后再修改验收设置')
            current['ipv6_verification_settings'] = deepcopy(values)
        manager._update_agents(save)
        source = readiness._agent(manager, agent_id, profile_id)
    candidates = []
    for item in manager.get_all_agents():
        try:
            candidate = _client(manager, source, item['id'])
        except MonitorError:
            continue
        candidates.append(candidate)
        if len(candidates) >= 32:
            break
    # This read is only done when opening settings, never on each heartbeat.
    with ThreadPoolExecutor(max_workers=4) as pool:
        caps = list(pool.map(lambda a: _capabilities(manager, a), candidates))
    return {'success': True, 'agent_id': agent_id, 'profile_id': profile_id, 'settings': _settings(source),
            'candidates': [{'id': a['id'], 'name': a.get('name') or a['id'], 'host': a.get('host'),
                            'status': a.get('status'), 'enabled': a.get('enabled') is not False,
                            'platform': cap.get('platform'),
                            'supported': cap.get('platform') == 'linux' and 1 in cap.get('ipv6_client_probe_protocols', [])}
                           for a, cap in zip(candidates, caps)]}


def _url(config, saved):
    if saved['test_url']:
        return saved['test_url']
    _, document = readiness.configuration(config)
    for group in document.get('proxy-groups') or []:
        if isinstance(group, dict) and group.get('type') in ('url-test', 'fallback', 'load-balance'):
            candidate = group.get('url')
            try:
                validate_health({'url': candidate})
                return candidate
            except ValueError:
                pass
    return GROUP_DEFAULTS['url']


def _source(manager, agent, deployment_id, expected_revision):
    config, revision = manager.repository.get_publish_snapshot(_profile(manager, agent))
    digest = None
    if deployment_id:
        task = (agent.get('deployments') or {}).get(deployment_id) or {}
        if (task.get('status') != 'succeeded' or task.get('manual_rollback')
                or task.get('profile_id') != _profile(manager, agent) or task.get('service_type') != 'mihomo'
                or (agent.get('latest_deployment') or {}).get('deployment_id') != deployment_id):
            raise MonitorError('DEPLOYMENT_CHANGED', 409, '本次发布尚未成功，或已被其他发布替代')
        revision = task.get('config_revision')
        from backend.agents.rule_sync_store import load
        receipt = load(manager.repository, agent, with_bundle=False)
        if not receipt or receipt['manifest'].get('deployment_id') != deployment_id:
            raise MonitorError('SNAPSHOT_UNAVAILABLE', 409, '缺少本次发布的配置快照，无法确认验收对象')
        config = receipt['source']
        # The Agent adapts the uploaded YAML to this installation before
        # publishing. Its confirmed final SHA identifies the file we verify;
        # the upload manifest only binds the immutable source snapshot.
        digest = _sha256(task.get('config_version'))
    # A manual check describes a captured saved revision; whether this Mihomo
    # runs it is settled in the worker (see _published).
    if expected_revision is not None and (type(expected_revision) is not int or expected_revision != revision):
        raise MonitorError('CONFIG_CHANGED', 409, '验收对应的配置版本已变化，请重新检查')
    return config, revision, digest


def _sha256(value):
    return value.lower() if isinstance(value, str) and re.fullmatch(r'[0-9a-fA-F]{64}', value) else None


def _published(manager, agent, config, revision):
    """Whether this Mihomo runs the saved configuration: ``(status, digest)``.

    Judged by what Mihomo receives (``readiness.publication``), not by the
    revision alone, which a router publication or another service's settings
    also move. Runs in the worker, so it may wait for the comparison. With
    ``synced``, ``digest`` is the running file's confirmed SHA for the Agent
    to check; ``changed`` or ``unknown`` give no digest.
    """
    from backend.agents.rule_sync_store import load
    applied = agent.get('applied_content') or {}
    receipt = load(manager.repository, agent, with_bundle=False)
    if not receipt or applied.get('deployment_id') != receipt['manifest'].get('deployment_id'):
        return 'unknown', None
    status = readiness.publication(manager, agent, config, revision, wait=True)
    if status != 'synced':
        return ('changed' if status == 'changed' else 'unknown'), None
    digest = _sha256(agent.get('config_version'))
    return ('synced' if digest else 'unknown'), digest


def _ipv6_enabled(config):
    return readiness.configuration(config)[0].get('ipv6') is True


def _not_applicable(agent_id, profile_id):
    """Without IPv6 in the verified configuration there is nothing to verify."""
    return {'success': True, 'agent_id': agent_id, 'profile_id': profile_id, 'verification': None,
            'applicable': False, 'message': NOT_APPLICABLE}


def result(manager, agent_id, profile_id, deployment_id=None):
    agent = readiness._agent(manager, agent_id, profile_id)
    if not deployment_id and not _ipv6_enabled(manager.repository.get_publish_snapshot(profile_id)[0]):
        # An earlier run while IPv6 was on says nothing about this configuration.
        return _not_applicable(agent_id, profile_id)
    runs = agent.get('ipv6_verifications') or {}
    report = deepcopy(runs.get(deployment_id) if deployment_id else max(
        (r for r in runs.values() if r.get('profile_id') == profile_id),
        key=lambda r: r.get('started_epoch', 0), default=None))
    if report and report.get('profile_id') != profile_id:
        report = None
    if report:
        changed = _findings(manager, agent, report)
        if changed:
            report.update(status='warning', stale=True, message='、'.join(changed) + '，请重新验收',
                          checks=[_check('result_stale', '旧验收结果', 'unknown',
                                         '不能将旧结果用于当前配置、设备或检测客户端', changed)])
        elif _expired(report):
            _interrupt(report)
    return {'success': True, 'agent_id': agent_id, 'profile_id': profile_id, 'verification': report}


def _safe_checks(data, prefix=''):
    checks = []
    values = data.get('checks') if isinstance(data, dict) else None
    for value in values[:80] if isinstance(values, list) else []:
        if not isinstance(value, dict):
            continue
        status = value.get('status')
        if status not in ('ok', 'warning', 'error', 'unknown'):
            status = 'unknown'
        details = value.get('details')
        checks.append(_check(prefix + str(value.get('id', 'unknown'))[:100],
                             str(value.get('label', '验收结果'))[:200], status,
                             str(value.get('message', '未提供检查证据'))[:2000],
                             [str(v)[:1000] for v in details[:16]] if isinstance(details, list) else []))
        if isinstance(value.get('scope'), str):
            checks[-1]['scope'] = value['scope'][:100]
    return checks


def _runtime_checks(runtime, digest, publication='unknown'):
    checks = [c for c in _safe_checks(runtime, 'runtime_') if c['id'] != 'runtime_client_path']
    if not digest:
        # Even an old Agent's incomplete response cannot establish which saved
        # configuration was tested without a confirmed publication receipt.
        checks.append(_check('runtime_expected_config', '验收配置版本', 'unknown',
                             '已保存的配置还没有发布到这台 Mihomo，请先保存并发布再验收' if publication == 'changed'
                             else '缺少与当前已保存配置一致的发布记录，尚不能确认验收的是这一版配置'))
    required = {'deployed_config', 'runtime_core', 'runtime_ipv6', 'runtime_tun',
                'proxy_http', 'node_ipv6_transport', 'dns_a', 'dns_aaaa'}
    present = {c['id'].removeprefix('runtime_') for c in checks}
    missing = sorted(required - present)
    if missing:
        checks.append(_check('runtime_evidence', '主机验收证据', 'unknown',
                             '设备未返回全部检查结果，不能仅凭网站可访问判定通过', missing))
    return checks


def _client_checks(manager, source, config, saved, test_url, run_id):
    from backend.agents.ipv6_client_evidence import evaluate, observe
    client = _client(manager, source, saved['client_agent_id'], capabilities=True)
    settings_value, document = readiness.configuration(config)
    dns = document.get('dns') or {}
    payload = {'test_url': test_url, 'request_id': run_id}
    if dns.get('enhanced-mode') == 'fake-ip' and dns.get('fake-ip-range'):
        payload['fake_ipv4_range'] = dns['fake-ip-range']
    if settings_value['fake_ipv6_enabled']:
        payload['fake_ipv6_range'] = settings_value['fake_ipv6_range']
    # Watch first, then send a new client request. The watcher excludes IDs
    # already present before it starts; tuple correlation is checked below.
    stop, ready = threading.Event(), threading.Event()
    with ThreadPoolExecutor(max_workers=2) as pool:
        router_watch = pool.submit(observe, config, source, test_url, stop, ready)
        ready.wait(3)
        watch = pool.submit(readiness._request, source, 'verify-connections', {
            'host': urlsplit(test_url).hostname, 'request_id': run_id})
        time.sleep(.25)
        try:
            report = readiness._request(client, 'verify-client', payload)
            try:
                trace = watch.result()
            except MonitorError:
                trace = {}
        finally:
            stop.set()
        try:
            observed = router_watch.result()
        except Exception:
            observed = {}
    if _identity(manager.get_agent_by_id(client['id']) or {}) != _identity(client):
        raise MonitorError('CLIENT_CHANGED', 409, '检测客户端在验收期间发生变化，结果已作废')
    checks = _safe_checks(report)
    report = report if isinstance(report, dict) else {}
    trace = trace if isinstance(trace, dict) else {}
    if report.get('request_id') != run_id or trace.get('request_id') != run_id:
        checks.append(_check('client_request_identity', '本次客户端请求', 'unknown',
                             '客户端与 Mihomo 未返回一致的本次请求标识，不能把其他请求算作验收通过'))
        report, trace = {}, {}
    # Only the correlated evidence helper may establish the client's complete
    # path; a separate watcher-wide socket observation is insufficient.
    checks.extend(evaluate(config, source, report, trace, test_url, observed))
    return checks


def _execute(manager, source, config, saved, digest, report, key):
    checks = []
    try:
        if _identity(manager.get_agent_by_id(source['id']) or {}) != _identity(source):
            raise MonitorError('AGENT_CHANGED', 409, '设备绑定在验收前已变化')
        inspection = readiness._inspect(manager, source, report['profile_id'], config)
        checks.extend(_safe_checks(inspection))
        for router in inspection.get('routers', []):
            for check in _safe_checks(router, 'router_' + str(router['id']) + '_'):
                check['label'] = str(router.get('name') or 'ROS') + ' · ' + check['label']
                checks.append(check)
        test_url = _url(config, saved)
        publication = 'synced' if digest else 'unknown'
        if not report.get('deployment_id'):
            publication, digest = _published(manager, source, config, report['config_revision'])
        payload = {'test_url': test_url}
        if digest:
            payload['expected_config_sha256'] = digest
        runtime = readiness._request(source, 'verify', payload)
        checks.extend(_runtime_checks(runtime, digest, publication))
        if saved['client_agent_id']:
            checks.extend(_client_checks(manager, source, config, saved, test_url, report['run_id']))
        else:
            checks.append(_check('client_not_configured', '客户端整条链路', 'unknown',
                                 '尚未配置检测客户端；本次只验收 Mihomo 主机，不代表手机、电脑已通过'))
    except MonitorError as error:
        checks.append(_check('verification_failed', '验收未完成', 'error', error.message))
    except Exception:
        logger.error('IPv6 verification failed for a captured target')
        checks.append(_check('verification_failed', '验收未完成', 'error', '验收未完成，请检查设备在线状态后重试'))
    def finish(agents):
        current = next((a for a in agents if a['id'] == source['id']), None)
        stored = ((current or {}).get('ipv6_verifications') or {}).get(key)
        if not stored or stored.get('run_id') != report['run_id'] or stored.get('status') != 'running':
            return
        changed = _findings(manager, current, report, agents)
        if changed:
            checks[:] = [_check('target_changed', '验收对象已变化', 'unknown',
                                '设备、客户端、配置或发布版本已变化，本次结果已作废', changed)]
        required = [c for c in checks if c['id'] != 'client_not_configured']
        status = 'error' if any(c['status'] == 'error' for c in required) else 'warning' if any(
            c['status'] != 'ok' for c in required) or not required else 'ok'
        stored.update(status=status, checks=checks, checked_at=_now(),
                      message='主机自检完成' if not saved['client_agent_id'] else '客户端与主机验收完成')
    manager._update_agents(finish)


def _fail_run(manager, agent_id, key, run_id):
    def fail(agents):
        current = next((a for a in agents if a['id'] == agent_id), None)
        stored = ((current or {}).get('ipv6_verifications') or {}).get(key)
        if stored and stored.get('run_id') == run_id and stored.get('status') == 'running':
            _interrupt(stored, '验收任务未完成，请重新验收')
    manager._update_agents(fail)


def _reusable(manager, source, report, automatic, deployment_id, agents=None):
    if not report:
        return False
    if report.get('status') == 'running' and not _expired(report):
        return True
    # Background scheduling is once per publication. Manual retry may replace
    # a failed, incomplete or stale run without retrying forever every tick.
    return bool(automatic or (deployment_id and report.get('status') == 'ok'
                             and not _findings(manager, source, report, agents)))


def start(manager, agent_id, profile_id, body, *, automatic=False):
    if set(body) - {'deployment_id', 'expected_config_revision'}:
        raise MonitorError('INVALID_REQUEST', 400, '验收只使用已保存的目标与测试设置')
    deployment_id = body.get('deployment_id')
    if deployment_id is not None and (not isinstance(deployment_id, str) or not 1 <= len(deployment_id) <= 128):
        raise MonitorError('INVALID_REQUEST', 400, '发布标识无效')
    if 'expected_config_revision' in body and (type(body['expected_config_revision']) is not int
                                              or body['expected_config_revision'] < 0):
        raise MonitorError('INVALID_REQUEST', 400, '配置版本无效')
    source = readiness._agent(manager, agent_id, profile_id)
    key = deployment_id or 'manual'
    old = (source.get('ipv6_verifications') or {}).get(key)
    if automatic and old:
        return result(manager, agent_id, profile_id, deployment_id)
    config, revision, digest = _source(manager, source, deployment_id, body.get('expected_config_revision'))
    if not _ipv6_enabled(config):
        # The published snapshot for a publication, else the saved configuration.
        return _not_applicable(agent_id, profile_id)
    if _reusable(manager, source, old, automatic, deployment_id):
        return result(manager, agent_id, profile_id, deployment_id)
    saved = _settings(source)
    report = {'status': 'running', 'scope': 'client_path' if saved['client_agent_id'] else 'mihomo_runtime',
              'checks': [], 'started_at': _now(), 'started_epoch': time.time(), 'run_id': uuid.uuid4().hex,
              'profile_id': profile_id, 'config_revision': revision, 'deployment_id': deployment_id,
              'settings_fingerprint': readiness._digest(saved), 'target_fingerprint': readiness._digest(_identity(source)),
              'applied_fingerprint': readiness._digest(_applied(source)),
              'client_fingerprint': _client_fingerprint(manager, source),
              'message': '正在自动验收发布结果' if deployment_id else '正在验收连接与分流'}
    if not _slots.acquire(blocking=False):
        raise MonitorError('BUSY', 409, '验收队列繁忙，请稍后重试')
    released = False
    release_lock = threading.Lock()
    def release():
        nonlocal released
        with release_lock:
            if not released:
                _slots.release()
                released = True
    reserved = False
    try:
        def reserve(agents):
            nonlocal reserved
            current = next((a for a in agents if a['id'] == agent_id), None)
            if not current or _findings(manager, current, report, agents):
                raise MonitorError('AGENT_CHANGED', 409, '设备或验收设置已变化，请重新检查')
            runs = current.setdefault('ipv6_verifications', {})
            for previous in runs.values():
                if _expired(previous):
                    _interrupt(previous)
            if _reusable(manager, current, runs.get(key), automatic, deployment_id, agents):
                return
            if any(r.get('status') == 'running' for r in runs.values()):
                raise MonitorError('BUSY', 409, '这台设备正在验收，请等待当前结果')
            runs[key] = deepcopy(report)
            for old_key in list(runs)[:-7]:
                if old_key != key:
                    runs.pop(old_key)
            reserved = True
        manager._update_agents(reserve)
        if reserved:
            def run():
                from backend.common.config import reset_config_context
                try:
                    reset_config_context()
                    with readiness._lock(agent_id):
                        _execute(manager, source, config, saved, digest, report, key)
                except Exception:
                    logger.error('IPv6 verification worker did not complete')
                    _fail_run(manager, agent_id, key, report['run_id'])
                finally:
                    release()
            _executor.submit(run)
        else:
            release()
    except Exception:
        release()
        if reserved:
            _fail_run(manager, agent_id, key, report['run_id'])
        raise
    return result(manager, agent_id, profile_id, deployment_id)


def enroll(manager, agent_id, deployment_id, config):
    """Enroll only newly prepared IPv6 publications, never historical tasks."""
    settings_value, _ = readiness.configuration(config)
    if settings_value.get('ipv6') is not True or settings_value.get('errors'):
        return
    def update(agents):
        agent = next((a for a in agents if a['id'] == agent_id), None)
        task = ((agent or {}).get('deployments') or {}).get(deployment_id)
        if task is not None:
            task['ipv6_verification_requested'] = True
            if (agent.get('latest_deployment') or {}).get('deployment_id') == deployment_id:
                agent['latest_deployment'] = task.copy()
    manager._update_agents(update)


def _unavailable(manager, source, deployment_id, error):
    """A definite preflight failure is a visible result, not an endless retry."""
    saved = _settings(source)
    report = {'status': 'error', 'scope': 'client_path' if saved['client_agent_id'] else 'mihomo_runtime',
              'checks': [_check('verification_unavailable', '验收未能开始', 'error', error.message)],
              'started_at': _now(), 'checked_at': _now(), 'started_epoch': time.time(), 'run_id': uuid.uuid4().hex,
              'profile_id': _profile(manager, source),
              'config_revision': (source.get('latest_deployment') or {}).get('config_revision'),
              'deployment_id': deployment_id, 'error_code': error.code,
              'settings_fingerprint': readiness._digest(saved), 'target_fingerprint': readiness._digest(_identity(source)),
              'applied_fingerprint': readiness._digest(_applied(source)),
              'client_fingerprint': _client_fingerprint(manager, source), 'message': error.message}
    def store(agents):
        current = next((a for a in agents if a.get('id') == source['id']), None)
        latest = (current or {}).get('latest_deployment') or {}
        if (not current or _findings(manager, current, report, agents)
                or latest.get('deployment_id') != deployment_id or latest.get('status') != 'succeeded'
                or latest.get('manual_rollback')):
            return
        runs = current.setdefault('ipv6_verifications', {})
        if deployment_id not in runs:
            runs[deployment_id] = deepcopy(report)
    manager._update_agents(store)


_rounds = IdleRounds()


def tick(manager):
    from backend.common.config import reset_config_context
    reset_config_context()
    head = _rounds.due(manager.repository)
    if head is None:
        return
    _rounds.done(manager.repository, head, _round(manager))


def _round(manager):
    """One scheduling round; True when it found nothing to start, poll or wait for."""
    def expire(agents):
        for agent in agents:
            for report in (agent.get('ipv6_verifications') or {}).values():
                if _expired(report):
                    _interrupt(report)
    agents = manager.get_all_agents()
    reports = [r for a in agents for r in (a.get('ipv6_verifications') or {}).values()]
    # A running report expires by the clock, not by a configuration change.
    idle = not any(r.get('status') == 'running' for r in reports)
    if any(_expired(r) for r in reports):
        manager._update_agents(expire)
        agents = manager.get_all_agents()
    for agent in agents:
        if agent.get('service_type') != 'mihomo' or agent.get('enabled') is False:
            continue
        latest = agent.get('latest_deployment') or {}
        ident = latest.get('deployment_id')
        if not ident or not latest.get('ipv6_verification_requested'):
            continue
        try:
            if latest.get('status') not in _TERMINAL:
                idle = False
                latest = manager.get_deployment(agent['id'], ident)
            if latest.get('status') == 'succeeded' and not latest.get('manual_rollback'):
                if ident in (agent.get('ipv6_verifications') or {}):
                    # Scheduled once per publication: start() would only read that run back.
                    continue
                idle = False
                start(manager, agent['id'], _profile(manager, agent), {'deployment_id': ident}, automatic=True)
        except MonitorError as error:
            if error.code in {'SNAPSHOT_UNAVAILABLE', 'CLIENT_UNAVAILABLE', 'CLIENT_UPGRADE_REQUIRED',
                              'CONFIG_CHANGED', 'INVALID_REQUEST', 'UPGRADE_REQUIRED', 'AGENT_AUTH_FAILED'}:
                _unavailable(manager, agent, ident, error)
            # Busy and changed/unconfirmed targets may be resolved by a later
            # tick. Definite missing prerequisites remain visible until retry.
        except Exception:
            logger.warning('IPv6 automatic verification could not be scheduled')
    return idle


def start_loop(manager):
    key = str(manager.repository.data_dir)
    with _loop_lock:
        if key in _loops:
            return
        _loops.add(key)
    def work():
        from backend.common import config as module
        try:
            while module._repository is manager.repository:
                try:
                    tick(manager)
                except Exception:
                    logger.warning('IPv6 verification scheduler will retry after an interrupted tick')
                threading.Event().wait(5)
        finally:
            with _loop_lock:
                _loops.discard(key)
    threading.Thread(target=work, daemon=True, name='ipv6-verification-scheduler').start()
