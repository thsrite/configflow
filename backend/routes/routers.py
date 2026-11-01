"""Upstream routers: CRUD, capability probe, drift report and staged releases."""
from datetime import datetime

import requests
from flask import jsonify, request

from backend.common.auth import require_auth
from backend.agents.operation_events import event_message
from backend.common.config import get_config, get_repository, reset_config_context, save_config
from backend.routers import dns_guard, ipv6_pd_guard
from backend.routers.base import RouterConflictError, RouterError, as_dict, retire_policy_routes
from backend.routers.desired import compute_desired
from backend.routers.dhcp_dns import build_dns_report, dns_agent_settings, dns_ha_settings
from backend.routers.ikuai import IKuaiDriver
from backend.routers.plan import build_plan
from backend.routers.release import AgentReturnRouting, execute
from backend.routers.report import build_report
from backend.routers.route_editor import (
    RouteEditValidationError, build_route_plan, execute_route_plan,
    normalize_request, public_route, read_routes, router_operation_lock,
)
from backend.routers.routeros import RouterOSDriver
from backend.routers.store import (
    RouterValidationError, find_router, guard_settings, normalize, public_view, routers_of, static_rule_sets,
)
from backend.routes import routers_bp
from backend.utils.logger import get_logger, log_text

DRIVERS = {'ikuai': IKuaiDriver, 'routeros': RouterOSDriver}
logger = get_logger(__name__)


def _label(router):
    return log_text(router.get('name') or router.get('id'))


def _manual_sync_lock(function):
    from functools import wraps
    @wraps(function)
    def wrapped(router_id):
        from backend.common.profile_context import resolve_profile_id
        from backend.routers.rule_sync_store import job_lock
        with job_lock(resolve_profile_id(), router_id, find_router(get_config(), router_id)) as acquired:
            if not acquired:
                return jsonify(success=False, message='此路由器正在发布或同步，请稍后重试'), 409
            return function(router_id)
    return wrapped


def _router_settings_lock(function):
    from functools import wraps
    locked = _manual_sync_lock(function)

    @wraps(function)
    def wrapped(router_id):
        # GET stays read-only; rule-sync POST acquires its own job lock.
        if request.method in ('PUT', 'DELETE'):
            return locked(router_id)
        return function(router_id)
    return wrapped


def _capture_sync(config, router, driver, desired):
    from backend.common.profile_context import resolve_profile_id
    from backend.routers.rule_sync import capture_manual
    try:
        if hasattr(driver, 'ipv6_fakeip_manifest'):
            router = {**router, 'ipv6_fakeip_state': driver.ipv6_fakeip_manifest}
        if hasattr(driver, 'ipv6_routes_manifest'):
            router = {**router, 'ipv6_routes_state': driver.ipv6_routes_manifest}
        capture_manual(resolve_profile_id(), config, router, driver, desired)
    except Exception:
        # A runtime receipt I/O problem must not turn a successful manual release
        # into a reported router failure. Automatic jobs still require a receipt.
        from backend.utils.logger import get_logger
        get_logger(__name__).exception('Could not record remote rule synchronization baseline')


def _driver(router):
    return DRIVERS[router['type']](router['endpoint'], router['username'], router.get('password') or '',
                                   verify_tls=router.get('verify_tls') is not False)


def _not_found():
    return jsonify({'success': False, 'message': '路由器不存在'}), 404


def _save_items(config, items):
    config['routers'] = {**(config.get('routers') if isinstance(config.get('routers'), dict) else {}), 'items': items}
    save_config(config)


def _agent(agent_id):
    if not agent_id:
        return None
    from backend.common.agent_manager import get_agent_manager
    return get_agent_manager().get_agent_by_id(agent_id)


@routers_bp.route('', methods=['GET', 'POST'])
@require_auth
def list_or_create_routers():
    config = get_config()
    if request.method == 'GET':
        return jsonify({'success': True, 'routers': [public_view(item) for item in routers_of(config)]})
    try:
        router = normalize(request.get_json(silent=True))
    except RouterValidationError as error:
        return jsonify({'success': False, 'message': str(error)}), 400
    _save_items(config, routers_of(config) + [router])
    logger.info('已添加主路由 %s（%s）', _label(router), router['type'])
    return jsonify({'success': True, 'router': public_view(router)})


def _proxy_gateways(config):
    """agent_id -> the routers sending proxied traffic to it, in router order."""
    linked = {}
    for item in routers_of(config):
        agent_id = item.get('proxy_gateway_agent_id')
        if agent_id and isinstance(agent_id, str):
            linked.setdefault(agent_id, []).append({'id': item.get('id'), 'name': item.get('name') or item.get('id')})
    return linked


def _no_store(response, status=200):
    response.headers['Cache-Control'] = 'no-store'
    return response, status


@routers_bp.route('/gateway-forwarding', methods=['GET', 'POST'])
@require_auth
def gateway_forwarding():
    """IPv4 forwarding on each router's proxy gateway; a router link in this profile authorises it."""
    from backend.agents import linux_gateway
    from backend.agents.oxidns_monitor import MonitorError
    from backend.common.agent_manager import get_agent_manager
    linked = _proxy_gateways(get_config())
    manager = get_agent_manager()
    if request.method == 'GET':
        return _no_store(jsonify({'success': True, 'gateways': [
            linux_gateway.gateway_forwarding_entry(manager, agent_id, refs) for agent_id, refs in linked.items()]}))
    body = request.get_json(silent=True)
    if not isinstance(body, dict) or set(body) != {'agent_id'} or not isinstance(body['agent_id'], str):
        return _no_store(jsonify({'success': False, 'code': 'INVALID_REQUEST', 'message': '请求只能包含 agent_id。'}), 400)
    agent_id = body['agent_id']
    if agent_id not in linked:
        return _no_store(jsonify({'success': False, 'code': 'NOT_LINKED',
                                  'message': '这台 Agent 不是当前配置空间中任何路由器的代理网关。'}), 404)
    try:
        data = linux_gateway.persist_gateway_forwarding(manager, agent_id, linked[agent_id])
    except MonitorError as error:
        return _no_store(jsonify({'success': False, 'code': error.code, 'message': error.message}), error.status)
    logger.info('已为代理网关 Agent %s 开启并持久化 IPv4 转发', log_text(agent_id))
    return _no_store(jsonify(data))


@routers_bp.route('/<router_id>', methods=['PUT', 'DELETE'])
@require_auth
@_router_settings_lock
def update_or_delete_router(router_id):
    from backend.common.profile_context import resolve_profile_id
    config = get_config()
    existing = find_router(config, router_id)
    if existing is None:
        return _not_found()
    with router_operation_lock(existing):
        reset_config_context()
        config = get_config()
        current = find_router(config, router_id)
        if current != existing:
            return jsonify(success=False, message='路由器设置已变化，请刷新后重试'), 409
        existing = current
        profile_id = resolve_profile_id()
        if request.method == 'DELETE':
            if existing.get('ros_policy_state'):
                return jsonify(success=False, message='请先清空策略路由选择并发布撤回，再删除路由器记录'), 409
            if existing.get('ipv6_fakeip_state'):
                return jsonify(success=False, message='请先关闭 IPv6 Fake-IP 并发布撤销，再删除路由器记录'), 409
            if existing.get('ipv6_routes_state'):
                return jsonify(success=False, message='请先关闭 Mihomo IPv6 或取消相关规则集，并发布撤回 IPv6 规则集路由，再删除路由器记录'), 409
            if existing.get('dns_guard_state'):
                return jsonify(success=False, message='请先关闭强制 DNS 并发布撤销，再删除路由器记录'), 409
            if existing.get('ipv6_pd_guard_state'):
                return jsonify(success=False, message='请先关闭 IPv6 前缀变化保护并发布撤回，再删除路由器记录'), 409
            get_repository().save_router_settings(profile_id, existing, None)
            reset_config_context()
            logger.info('已删除主路由记录 %s', _label(existing))
            return jsonify({'success': True})
        try:
            router = normalize(request.get_json(silent=True), existing)
        except RouterValidationError as error:
            return jsonify({'success': False, 'message': str(error)}), 400
        if any(router.get(key) != existing.get(key) for key in ('type', 'endpoint', 'username', 'password', 'verify_tls')):
            router.pop('last_probe', None)   # stale once the connection changes
        get_repository().save_router_settings(profile_id, existing, router)
        reset_config_context()
        changed = sorted(key for key in set(router) | set(existing)
                         if key not in ('last_probe', 'password') and router.get(key) != existing.get(key))
        if router.get('password') != existing.get('password'):
            changed.append('password')
        logger.info('已修改主路由 %s 的设置：%s', _label(router), '、'.join(changed) or '无变化')
        return jsonify({'success': True, 'router': public_view(router)})


@routers_bp.route('/<router_id>/probe', methods=['POST'])
@require_auth
@_manual_sync_lock
def probe_router(router_id):
    config = get_config()
    router = find_router(config, router_id)
    if router is None:
        return _not_found()
    driver = _driver(router)
    probe = {'at': datetime.now().isoformat(timespec='seconds')}
    try:
        info = driver.probe()
        probe.update(ok=True, **as_dict(info))
    except RouterError as error:
        probe.update(ok=False, message=str(error))
    finally:
        driver.close()
    items = routers_of(config)
    for item in items:
        if item.get('id') == router_id:
            item['last_probe'] = probe
    _save_items(config, items)
    return jsonify({'success': probe['ok'], 'probe': probe, 'message': probe.get('message', '')}), 200 if probe['ok'] else 502


def _dns_settings(dns_id):
    """DHCP DNS target: the selected OxiDNS, or its pair's virtual IP once DNS HA is enabled."""
    dns_agent = _agent(dns_id) if dns_id else None
    ha = (dns_agent or {}).get('oxidns_ha') or {}
    if ha.get('enabled') is True:
        return dns_ha_settings(dns_id, dns_agent, _agent(ha.get('peer_id')))
    return dns_agent_settings(dns_id, dns_agent)


def _inspect(config, router, driver, content_loader=None):
    """Desired state, router state, drift report and Agent return-routing state."""
    from backend.routers import ros_address_lists
    # One preview proves each address list once, however many readers need it.
    with ros_address_lists.reuse(driver):
        return _inspect_state(config, router, driver, content_loader)


def _inspect_state(config, router, driver, content_loader=None):
    gateway_agent = _agent(router.get('proxy_gateway_agent_id'))
    policy_sets = set(router.get('ros_policy_rule_sets') or []) if router['type'] == 'routeros' else set()
    static_sets = static_rule_sets(router)
    desired = dict(compute_desired(config, gateway_agent.get('host') if gateway_agent else None, static_sets,
                                   **({'content_loader': content_loader} if content_loader else {})))
    dns_id = router.get('dns_agent_id') or ''
    desired.update(_dns_settings(dns_id))
    if router['type'] == 'routeros' and (policy_sets or router.get('ros_policy_state')):
        driver.ros_policy_manifest = router.get('ros_policy_state')
    guard = dns_guard.relevant(router)
    if guard:
        driver.dns_guard_manifest = router.get('dns_guard_state')
    state = driver.read_state()
    if router.get('manage_return_routing') is not True:
        # Policy rules need managed return routing; without it, route by static routes.
        retire_policy_routes(state)
    if guard:
        desired['dns_guard'] = {'settings': guard_settings(router), 'previous': router.get('dns_guard_state')}
        state.dns_guard = dns_guard.snapshot(driver)
    if router['type'] == 'routeros' and (policy_sets or router.get('ros_policy_state')):
        from backend.routers.ros_policy import inspect_desired, snapshot, owned_rows, MANGLE, RULES, VRFS, ROUTES
        desired['ros_policy'] = inspect_desired(config, router, desired,
                                                **({'content_loader': content_loader} if content_loader else {}))
        state.ros_policy = snapshot(driver)
        # Only verified objects from our previous release may be disregarded
        # when assessing the upcoming path's NAT requirements.
        try:
            owned = owned_rows(router.get('ros_policy_state'), state.ros_policy)
        except RouterError:
            owned = []
        owned_mangle = {row['id'] for row in owned if row['path'] == MANGLE}
        # Live counters do not change routing behavior. Keep all matchers,
        # ordering and route state used to prove the egress in the fingerprint.
        state.nat_context.update(
            mangle=[{key: value for key, value in row.items() if key not in {'bytes', 'packets'}}
                    for row in state.ros_policy[MANGLE] if row.get('.id') not in owned_mangle],
            routing_rules=state.ros_policy[RULES], vrfs=state.ros_policy[VRFS], routes=state.ros_policy[ROUTES])
    if router['type'] == 'routeros':
        from backend.routers import ipv6_fakeip, ipv6_routes
        hosts = []

        def host_report():
            # One Agent request serves both IPv6 route features of this preview.
            if not hosts:
                hosts.append(_ipv6_host(config, gateway_agent))
            return hosts[0]
        if ipv6_fakeip.relevant(config, router):
            host = host_report() if ipv6_fakeip.settings(config)['enabled'] else {}
            driver.ipv6_fakeip_manifest = router.get('ipv6_fakeip_state')
            state.ipv6_fakeip_plan = ipv6_fakeip.inspect(config, router, gateway_agent, host, driver)
        if ipv6_routes.relevant(config, router, desired):
            host = host_report() if ipv6_routes.needs_host(config, router, desired) else {}
            state.ipv6_routes_plan = ipv6_routes.inspect(config, router, desired, gateway_agent, host, driver)
    elif desired.get('ipv6_cidrs'):
        desired.setdefault('warnings', []).append(f'所选规则集含 {len(desired["ipv6_cidrs"])} 个 IPv6 网段；'
                                   'iKuai 只发布 IPv4 路由，IPv6 网段未处理')
    if dns_id and not desired['dns_error']:
        try:
            state.dhcp_dns = driver.read_dhcp_dns()
        except RouterError as error:
            desired['dns_error'] = f'无法读取 DHCP DNS 配置：{error}'
    if router['type'] == 'routeros' and dns_id and not desired['dns_error']:
        dns = build_dns_report(desired, state.dhcp_dns)
        if dns['status'] in {'pending', 'synced'} and len(dns['items']) == 1:
            from backend.routers import ipv6_dns
            item = dns['items'][0]
            scope = {'id': item['id'], 'network': item['network'], 'gateway': desired['proxy_gateway'],
                     'dns': item['desired_dns']}
            state.ipv6_dns_plan = ipv6_dns.inspect({**desired, 'ipv6_dns_scope': scope}, driver)
    if ipv6_pd_guard.relevant(router):
        state.ipv6_pd_guard_plan = ipv6_pd_guard.inspect(router, (gateway_agent or {}).get('host') or '', driver)
    report = build_report(desired, state)
    from backend.routers import forward_hint
    # Read-only: never part of a release operation, check or fingerprint.
    hint = forward_hint.ipv4(router, desired, state, report, driver)
    if hint:
        report['routes']['forward_hint'] = hint
    if router.get('manage_return_routing') is True:
        report['return_routing'] = _return_routing(gateway_agent)
    else:
        report['return_routing'] = {
            'status': 'disabled',
            'message': '未启用自动配置回流；本次不写入回流规则。',
        }
        cleanup = _return_cleanup(router, gateway_agent)
        if cleanup:
            report['return_routing'].update(
                cleanup=cleanup, message='未启用自动配置回流；mihomo 上仍有 ConfigFlow 写入的回流规则，发布时最后移除。')
    return gateway_agent, desired, state, report


def _ipv6_host(config, agent):
    """The Mihomo gateway's IPv6 addresses, or the reason they are unknown."""
    from backend.common.profile_context import resolve_profile_id
    from backend.agents import ipv6_readiness
    from backend.agents.oxidns_monitor import MonitorError
    if not agent or agent.get('profile_id') != (config.get('profile_id') or resolve_profile_id()):
        return {'error': '代理网关不属于当前配置空间，请重新关联后预览。'}
    if agent.get('service_type') != 'mihomo' or agent.get('enabled') is False:
        return {'error': '代理网关不是可用的 Mihomo 设备，请核对关联。'}
    try:
        return ipv6_readiness._request(agent)
    except MonitorError as error:
        return {'error': str(error)}


def _return_cleanup(router, agent):
    """Return routing this Agent still manages after the switch was turned off, or None.

    An unreachable or old Agent leaves its rules alone, as before. Another router
    in any profile that still uses return routing on the same Agent keeps them.
    """
    if agent is None or agent.get('status') != 'online' or not agent.get('host') or not agent.get('port'):
        return None
    status = _return_routing(agent)
    managed = status.get('managed') or {}
    if status.get('status') != 'ok' or not managed.get('spec'):
        return None
    safety = status.get('safety') or {}
    # Removal keeps the state file inactive: only active or unsettled state needs attention.
    if managed.get('active') is not True and safety.get('ok') is True and not safety.get('blockers'):
        return None
    from backend.common.config import get_config, get_repository
    from backend.common.profile_context import resolve_profile_id
    current = (resolve_profile_id(), router.get('id'))
    shared = [item.get('name') or item.get('id')
              for profile in get_repository().list_profiles()
              for item in routers_of(get_config(profile['id']))
              if (profile['id'], item.get('id')) != current and item.get('manage_return_routing') is True
              and item.get('proxy_gateway_agent_id') == agent.get('id')]
    return {'spec': managed['spec'], 'complete': managed.get('table_present') is True,
            'safety': status.get('safety'), 'shared_with': shared}


def _now():
    return datetime.now().isoformat(timespec='seconds')


@routers_bp.route('/<router_id>/report', methods=['GET'])
@require_auth
def router_report(router_id):
    config = get_config()
    router = find_router(config, router_id)
    if router is None:
        return _not_found()
    driver = _driver(router)
    try:
        _, _, _, report = _inspect(config, router, driver)
    except RouterError as error:
        return jsonify({'success': False, 'message': str(error)}), 502
    finally:
        driver.close()
    from backend.common.profile_context import resolve_profile_id
    from backend.notifications import safely, record_router
    safely(record_router, resolve_profile_id(), config, router, report)
    return jsonify({'success': True, 'report': report, 'generated_at': _now()})


@routers_bp.route('/<router_id>/plan', methods=['POST'])
@require_auth
def router_plan(router_id):
    """Preview a release: nothing is written."""
    config = get_config()
    router = find_router(config, router_id)
    if router is None:
        return _not_found()
    driver = _driver(router)
    try:
        _, desired, state, report = _inspect(config, router, driver)
        plan = build_plan(router, desired, state, report, report['return_routing'], driver)
    except RouterError as error:
        return jsonify({'success': False, 'message': str(error)}), 502
    finally:
        driver.close()
    from backend.common.profile_context import resolve_profile_id
    from backend.notifications import safely, record_router
    safely(record_router, resolve_profile_id(), config, router, report, plan)
    if plan['empty'] and not plan['blockers']:
        from backend.common.profile_context import resolve_profile_id
        from backend.routers.rule_sync import needs_baseline
        # Nothing to write, but confirming still records the automatic sync
        # baseline, which is how a paused sync resumes.
        plan['sync_baseline'] = needs_baseline(resolve_profile_id(), router)
    return jsonify({'success': True, 'plan': plan, 'generated_at': _now()})


@routers_bp.route('/<router_id>/apply', methods=['POST'])
@require_auth
@_manual_sync_lock
def router_apply(router_id):
    """Run the previewed release. The plan is rebuilt and must match the preview's fingerprint."""
    from backend.common.profile_context import resolve_profile_id
    config = get_config()
    router = find_router(config, router_id)
    if router is None:
        return _not_found()
    expected = str((request.get_json(silent=True) or {}).get('fingerprint') or '')
    driver = None
    try:
        with router_operation_lock(router):
            reset_config_context()
            config = get_config()
            if find_router(config, router_id) != router:
                return jsonify({'success': False, 'message': '路由器设置在等待发布期间发生变化，请重新预览'}), 409
            router = find_router(config, router_id)
            driver = _driver(router)
            gateway_agent, desired, state, report = _inspect(config, router, driver)
            plan = build_plan(router, desired, state, report, report['return_routing'], driver)
            if plan['blockers']:
                logger.warning('主路由 %s 发布被阻止：%s', _label(router), '；'.join(plan['blockers']))
                return jsonify({'success': False, 'message': '；'.join(plan['blockers']), 'plan': plan}), 409
            if not expected or plan['fingerprint'] != expected:
                logger.warning('主路由 %s 发布被拒绝：预览后状态已变化，需要重新预览', _label(router))
                return jsonify({'success': False, 'message': '路由器、Agent 或期望状态在预览后发生了变化，请重新预览',
                                'plan': plan}), 409
            if plan['empty']:
                _capture_sync(config, router, driver, desired)
                get_repository().save_router_publication(resolve_profile_id(), router, last_release={
                    'at': _now(), 'ok': True, 'fingerprint': plan['fingerprint'], 'failed_stage': '', 'message': '',
                })
                reset_config_context()
                from backend.notifications import safely, refresh_tasks
                safely(refresh_tasks)
                logger.info('主路由 %s 发布：没有需要写入的变更', _label(router))
                return jsonify({'success': True, 'result': {'ok': True, 'log': [], 'rollback': []}, 'plan': plan})
            agent_client = None
            if router.get('manage_return_routing') is True or any(
                    op['kind'] == 'return_routing_remove' for stage in plan['stages'] for op in stage['ops']):
                from backend.common.agent_manager import get_agent_manager
                agent_client = AgentReturnRouting(get_agent_manager(), gateway_agent)
            published_config = config
            changes = sum(len(stage['ops']) for stage in plan['stages'])
            logger.info('开始发布到主路由 %s：%d 项变更', _label(router), changes)
            result = execute(plan, driver, agent_client)
            _log_release(router, changes, result)
            publication = {'last_release': {
                'at': _now(), 'ok': result['ok'], 'fingerprint': plan['fingerprint'],
                'failed_stage': result.get('failed_stage', ''),
                'message': event_message('；'.join(entry.get('summary', '') for entry in
                    result.get('log', []) + result.get('rollback', []) if entry.get('status') == 'failed'), (router.get('password'),)),
            }}
            if hasattr(driver, 'ros_policy_manifest'):
                publication['manifest'] = driver.ros_policy_manifest
            if hasattr(driver, 'dns_guard_manifest'):
                publication['dns_guard'] = driver.dns_guard_manifest
            if hasattr(driver, 'ipv6_fakeip_manifest'):
                publication['ipv6_fakeip'] = driver.ipv6_fakeip_manifest
            if hasattr(driver, 'ipv6_routes_manifest'):
                publication['ipv6_routes'] = driver.ipv6_routes_manifest
            if hasattr(driver, 'ipv6_pd_guard_manifest'):
                publication['ipv6_pd_guard'] = driver.ipv6_pd_guard_manifest
            get_repository().save_router_publication(resolve_profile_id(), router, **publication)
            reset_config_context()
            from backend.notifications import safely, refresh_tasks
            safely(refresh_tasks)
            if result['ok']:
                _capture_sync(published_config, router, driver, desired)
    except RouterError as error:
        logger.warning('主路由 %s 发布中止：%s', _label(router), error)
        return jsonify({'success': False, 'message': str(error)}), 502
    finally:
        if driver is not None:
            driver.close()

    message = ''
    if not result['ok']:
        rollback_failed = any(item['status'] == 'failed' for item in result['rollback'])
        detail = '回滚未完成，请检查发布记录' if rollback_failed else '已按逆序撤销已完成的变更'
        message = f'阶段「{result["failed_stage"]}」失败，{detail}'
    return jsonify({'success': result['ok'], 'result': result, 'plan': plan, 'message': message}), \
        200 if result['ok'] else 502


def _log_release(router, changes, result):
    label = _label(router)
    for entry in result['log']:
        if entry['status'] in ('warning', 'manual'):
            logger.warning('主路由 %s 发布提示（%s）：%s', label, entry['stage'], entry['summary'])
    if result['ok']:
        logger.info('主路由 %s 发布完成：%d 项变更已写入并通过回读校验', label, changes)
        return
    failure = next((entry['summary'] for entry in reversed(result['log']) if entry['status'] == 'failed'), '')
    logger.error('主路由 %s 发布失败：阶段「%s」%s', label, result['failed_stage'], failure)
    failed = [entry['summary'] for entry in result['rollback'] if entry['status'] == 'failed']
    if failed:
        logger.error('主路由 %s 回滚未完成：%s', label, '；'.join(failed))
    elif result['rollback']:
        logger.warning('主路由 %s 已逆序撤销 %d 项已完成的变更', label, len(result['rollback']))


@routers_bp.route('/<router_id>/routes', methods=['GET'])
@require_auth
def list_device_routes(router_id):
    router = find_router(get_config(), router_id)
    if router is None:
        return _not_found()
    driver = _driver(router)
    try:
        rows = read_routes(driver)
        return jsonify({'success': True, 'routes': [public_route(row) for row in rows]})
    except RouterError as error:
        return jsonify({'success': False, 'message': str(error)}), 502
    finally:
        driver.close()


@routers_bp.route('/<router_id>/policy', methods=['GET'])
@require_auth
def list_device_policy(router_id):
    from backend.common.agent_manager import get_agent_manager
    from backend.common.config import get_default_profile_id
    from backend.common.profile_context import resolve_profile_id
    from backend.routers.ros_policy_view import read_policy_objects

    router = find_router(get_config(), router_id)
    if router is None:
        return _not_found()
    if router['type'] != 'routeros':
        return jsonify({'success': False, 'message': '策略路由查看仅支持 RouterOS'}), 400
    driver = None
    try:
        with router_operation_lock(router):
            reset_config_context()
            current = find_router(get_config(), router_id)
            if current != router:
                raise RouterConflictError('路由器设置已变化，请刷新后重试')
            driver = _driver(current)
            objects = read_policy_objects(driver, router.get('ros_policy_state'))
        profile_id = resolve_profile_id()
        source_agents = [{'id': item['id'], 'name': item.get('name') or item['id']}
                         for item in get_agent_manager().get_all_agents()
                         if item.get('service_type') == 'oxidns'
                         and (item.get('profile_id') or get_default_profile_id()) == profile_id]
        response = jsonify({'success': True, 'policy': {'objects': objects, 'source_agents': source_agents},
                            'generated_at': _now()})
        response.headers['Cache-Control'] = 'no-store'
        return response
    except RouterConflictError as error:
        return jsonify({'success': False, 'message': str(error)}), 409
    except RouterError as error:
        return jsonify({'success': False, 'message': str(error)}), 502
    finally:
        if driver is not None:
            driver.close()


@routers_bp.route('/<router_id>/policy-settings', methods=['GET', 'PUT'])
@require_auth
@_router_settings_lock
def policy_settings(router_id):
    from backend.common.agent_manager import get_agent_manager
    from backend.common.config import get_default_profile_id, get_repository, update_config_transaction
    from backend.common.profile_context import resolve_profile_id
    from backend.common.oxidns_settings import OxidnsSettingsError
    from backend.common.config_repository import ProfileRepositoryError
    from backend.routers.policy_workspace import FIELDS, read_workspace, save_workspace

    profile_id = resolve_profile_id()
    router = find_router(get_config(profile_id), router_id)
    if router is None:
        return _not_found()
    if router.get('type') != 'routeros':
        return jsonify(success=False, message='策略路由设置仅支持 RouterOS'), 400
    try:
        if request.method == 'PUT':
            body = request.get_json(silent=True)
            # Sources are edited on the client devices page; older clients may still send them.
            if not isinstance(body, dict) or set(body) not in ({'revision', 'router', 'base_url'},
                                                               {'revision', 'names_revision', 'router', 'policies', 'base_url'}) or \
                    not isinstance(body['router'], dict) or set(body['router']) != set(FIELDS) or \
                    not isinstance(body['base_url'], str):
                return jsonify(success=False, message='策略路由设置格式无效'), 400
            with router_operation_lock(router):
                reset_config_context()
                if find_router(get_config(profile_id), router_id) != router:
                    raise RouterConflictError('路由器设置已变化，请刷新后重试')
                update_config_transaction(lambda config: save_workspace(
                    config, router_id, body, body['base_url']), profile_id)
        config = get_config(profile_id)
        router = find_router(config, router_id)
        if router is None:
            return _not_found()
        workspace = read_workspace(config, router, profile_id,
                                   get_repository().get_profile(profile_id)['name'],
                                   get_agent_manager().get_all_agents(), get_default_profile_id())
        # Read after the draft, so a newer write only makes the publish recheck stricter.
        workspace['config_revision'] = get_repository().config_revision(profile_id)
        response = jsonify(success=True, workspace=workspace)
        response.headers['Cache-Control'] = 'no-store'
        return response
    except RouterConflictError as error:
        return jsonify(success=False, message=str(error)), 409
    except (RouterValidationError, OxidnsSettingsError, ProfileRepositoryError) as error:
        return jsonify(success=False, message=str(error)), 400


@routers_bp.route('/<router_id>/policy-settings/preview', methods=['POST'])
@require_auth
def preview_policy_settings(router_id):
    from backend.common.agent_manager import get_agent_manager
    from backend.common.config import get_repository, get_default_profile_id
    from backend.common.profile_context import resolve_profile_id
    from backend.common.oxidns_device_policy import revision
    from backend.common.config_export import sanitize_external_payload
    from backend.common.oxidns_validation import require_valid_document
    from backend.converters.oxidns import generate_oxidns_config
    from backend.routers.policy_workspace import draft_revision
    import yaml

    body = request.get_json(silent=True)
    if not isinstance(body, dict) or not isinstance(body.get('targets'), list) or \
            any(not isinstance(value, str) for value in body['targets']) or \
            len(set(body['targets'])) != len(body['targets']) or not isinstance(body.get('base_url', ''), str):
        return jsonify(success=False, message='发布预览参数无效'), 400
    profile_id = resolve_profile_id()
    repository = get_repository()
    config, config_revision = repository.get_publish_snapshot(profile_id)
    router = find_router(config, router_id)
    if router is None:
        return _not_found()
    if router.get('type') != 'routeros':
        return jsonify(success=False, message='策略路由设置仅支持 RouterOS'), 400
    if body.get('revision') != draft_revision(config, router):
        return jsonify(success=False, message='配置已更新，请重新载入后预览。'), 409
    manager = get_agent_manager()
    peers = {a['id']: a for a in manager.get_all_agents() if a.get('service_type') == 'oxidns'
             and (a.get('profile_id') or get_default_profile_id()) == profile_id}
    if any(target not in peers for target in body['targets']):
        return jsonify(success=False, message='OxiDNS 发布目标或绑定已变化，请重新载入。'), 409
    driver = _driver(router)
    try:
        _, desired, state, report = _inspect(config, router, driver)
        plan = build_plan(router, desired, state, report, report['return_routing'], driver)
        content = ''
        if body['targets']:
            document = yaml.safe_load(generate_oxidns_config(config, base_url=body.get('base_url', '')))
            require_valid_document(document)
            content = yaml.safe_dump(sanitize_external_payload(document, config.get('system_config')), allow_unicode=True, sort_keys=False)
        if repository.config_revision(profile_id) != config_revision:
            return jsonify(success=False, message='生成预览期间配置已更新，请重新预览。'), 409
        response = jsonify(success=True, preview={
            'revision': body['revision'], 'profile_id': profile_id, 'config_revision': config_revision,
            'oxidns_revision': revision(config.get('oxidns') or {}), 'router_plan': plan,
            'dns_content': content, 'targets': [{'id': target, 'name': peers[target].get('name') or target,
                'host': peers[target].get('host') or '', 'status': peers[target].get('status') or 'offline'}
                for target in body['targets']]})
        response.headers['Cache-Control'] = 'no-store'
        return response
    except (RouterError, ValueError) as error:
        return jsonify(success=False, message=str(error)), 400
    finally:
        driver.close()


@routers_bp.route('/<router_id>/routes/plan', methods=['POST'])
@require_auth
def preview_device_route(router_id):
    return _device_route_change(router_id, apply=False)


@routers_bp.route('/<router_id>/routes/apply', methods=['POST'])
@require_auth
@_manual_sync_lock
def apply_device_route(router_id):
    return _device_route_change(router_id, apply=True)


def _device_route_change(router_id, *, apply):
    router = find_router(get_config(), router_id)
    if router is None:
        return _not_found()
    payload = request.get_json(silent=True)
    try:
        normalize_request(payload)
    except RouteEditValidationError as error:
        return jsonify({'success': False, 'message': str(error)}), 400
    driver = None
    try:
        with router_operation_lock(router):
            reset_config_context()
            current = find_router(get_config(), router_id)
            if current != router:
                raise RouterConflictError('路由器设置已变化，请刷新后重试')
            driver = _driver(current)
            rows = read_routes(driver)
            plan = build_route_plan(router, rows, payload)
            if not apply:
                return jsonify({'success': True, 'plan': plan})
            fingerprint = payload.get('fingerprint') if isinstance(payload, dict) else None
            if not isinstance(fingerprint, str) or fingerprint != plan['fingerprint']:
                return jsonify({'success': False, 'message': '操作内容或路由状态已变化，请重新预览后确认'}), 409
            result = execute_route_plan(driver, rows, plan)
            logger.info('主路由 %s：%s%s', _label(current), plan['summary'], '' if result['changed'] else '（没有变化）')
            return jsonify({'success': True, 'result': result,
                            'message': '路由变更已写入并通过回读校验' if result['changed'] else '路由没有变化'})
    except RouteEditValidationError as error:
        return jsonify({'success': False, 'message': str(error)}), 400
    except RouterConflictError as error:
        return jsonify({'success': False, 'message': str(error)}), 409
    except RouterError as error:
        if apply:
            logger.warning('主路由 %s 静态路由变更失败：%s', _label(router), error)
        return jsonify({'success': False, 'message': str(error)}), 502
    finally:
        if driver is not None:
            driver.close()

def _return_routing(agent):
    """Return-routing state reported by the proxy gateway's Agent (read-only)."""
    if agent is None:
        return {'status': 'unlinked', 'message': '未关联 mihomo Agent'}
    if agent.get('status') != 'online':
        return {'status': 'offline', 'message': f'Agent {agent.get("name") or agent.get("id")} 离线'}
    from backend.common.agent_manager import get_agent_manager
    try:
        body = get_agent_manager().return_routing_status(agent)
    except (RuntimeError, requests.RequestException) as error:
        return {'status': 'unavailable', 'message': str(error) or error.__class__.__name__}
    return {'status': 'ok', **{key: value for key, value in body.items() if key != 'success'}}


@routers_bp.route('/<router_id>/rule-sync', methods=['GET', 'PUT', 'POST'])
@require_auth
@_router_settings_lock
def router_rule_sync(router_id):
    from backend.common.profile_context import resolve_profile_id
    from backend.common.config import update_config_transaction
    from backend.routers import rule_sync_store as sync_store
    from backend.routers.rule_sync import start_job
    profile_id = resolve_profile_id()
    router = find_router(get_config(profile_id), router_id)
    if router is None:
        return _not_found()
    if router.get('type') != 'routeros':
        return jsonify(success=False, message='远程规则自动同步仅支持 RouterOS'), 400
    try:
        if request.method == 'PUT':
            body = request.get_json(silent=True)
            if not isinstance(body, dict) or set(body) != {'auto_rule_sync', 'rule_sync_interval'}:
                return jsonify(success=False, message='请提供同步开关和检查间隔'), 400
            with router_operation_lock(router):
                reset_config_context()
                if find_router(get_config(profile_id), router_id) != router:
                    raise RouterConflictError('路由器设置已变化，请刷新后重试')
                def mutate(config):
                    current = find_router(config, router_id)
                    if current is None:
                        raise RouterValidationError('路由器已不存在')
                    normalized = normalize(body, current)
                    current.update(auto_rule_sync=normalized['auto_rule_sync'],
                                   rule_sync_interval=normalized['rule_sync_interval'])
                update_config_transaction(mutate, profile_id)
            router = find_router(get_config(profile_id), router_id)
        elif request.method == 'POST':
            receipt = sync_store.read(profile_id, router_id)
            if not router.get('auto_rule_sync') or not receipt.get('baseline'):
                return jsonify(success=False, message='请开启自动同步，并先手动预览发布一次'), 409
            if receipt.get('status') == 'paused':
                from backend.routers.rule_sync import paused_message
                return jsonify(success=False, message=paused_message(receipt)), 409
            if receipt.get('status') in ('running', 'queued'):
                return jsonify(success=False, message=receipt.get('message') or '同步尚未就绪'), 409
            start_job(profile_id, router_id)
        response = jsonify(success=True, settings={
            'auto_rule_sync': router.get('auto_rule_sync') is True,
            'rule_sync_interval': router.get('rule_sync_interval', 60)},
            sync=sync_store.public(sync_store.read(profile_id, router_id), router.get('auto_rule_sync') is True))
        response.headers['Cache-Control'] = 'no-store'
        return response, 202 if request.method == 'POST' else 200
    except RouterValidationError as error:
        return jsonify(success=False, message=str(error)), 400
    except RouterError as error:
        return jsonify(success=False, message=str(error)), 409
