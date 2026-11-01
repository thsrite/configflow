"""Project existing receipts into actionable notifications; never contact a device here."""
from copy import deepcopy
import hashlib
import json

from backend.agents.operation_events import event_message
from backend.utils import notification_store as store
from backend.utils.logger import get_logger

logger = get_logger(__name__)


def safely(function, *args, **kwargs):
    # A notification disk error must not turn a successful publication into a failure.
    try:
        return function(*args, **kwargs)
    except Exception:
        logger.exception('Could not update notification receipts')


def target(path, label, **query):
    return {'path': path, 'query': {key: str(value) for key, value in query.items() if value is not None}, 'label': label}


def task_observation(task, agents, profiles):
    category = task['category']
    resource = task['resource_id']
    profile = task.get('profile_id')
    agent = agents.get(resource, {}) if category in ('agent_rule_sync', 'dns_follow') else {}
    profile = profile or agent.get('profile_id')
    link = target('/system-settings', '查看对应任务', tab='tasks', task=task['key'])
    if category == 'subscription':
        link = target('/subscriptions', '检查订阅设置', subscription=resource, section='settings')
    elif category == 'rule_source':
        link = target('/rule-library', '检查规则集设置', rule=resource, section='settings')
    elif category == 'router_rule_sync':
        link = target('/routers', '查看同步设置', router=resource, profile=profile, section='rule-sync')
    elif category in ('agent_rule_sync', 'dns_follow'):
        link = target('/agents', '查看同步设置', agent=resource, profile=profile,
                      section='rule-sync' if category == 'agent_rule_sync' else 'dns-follow')
    elif category == 'webdav_backup':
        link = target('/system-settings', '检查 WebDAV 设置', section='backup')
    enabled = task.get('notifications_enabled', task['enabled']) and agent.get('enabled') is not False
    status = task.get('last_status')
    manual = category == 'router_rule_sync' and bool(task.get('state_message')) and not task.get('running')
    problem = status == 'failed' or (status == 'skipped' and bool(task.get('last_error'))) or manual
    state = 'inactive' if not enabled else ('unknown' if task.get('running') else
            'pending' if problem else 'resolved' if status == 'success' else 'unknown')
    labels = {'subscription': '订阅更新', 'rule_source': '规则集更新', 'router_rule_sync': '路由规则同步',
              'agent_rule_sync': '规则同步', 'dns_follow': 'DNS 联动', 'webdav_backup': 'WebDAV 备份',
              'domain_discovery': '域名探测', 'metrics_history': '监控历史保存'}
    message = task.get('last_error') or task.get('state_message') or '任务未完成，请查看任务详情。'
    cache = task.get('cache_retained')
    cache_time = task.get('cache_updated_at')
    impact = ('仍使用上一次有效缓存' + (f'（{cache_time}）' if cache_time else '') if cache else
              '没有可用缓存' if cache is False else '')
    title = task['name'] if category in ('webdav_backup', 'domain_discovery', 'metrics_history') else f'{task["name"]} · {labels.get(category, "后台任务")}'
    if category == 'webdav_backup' and task.get('last_trigger') == 'manual':
        title = 'WebDAV 手动备份'
    return {'state': state, 'source': 'task', 'title': title,
            'message': event_message(message), 'impact': impact,
            'status_label': '需要手动处理' if manual or status == 'skipped' else '执行失败',
            'severity': 'warning' if manual or status == 'skipped' or cache else 'error',
            'profile_id': profile, 'scope': 'profile' if profile else
            'shared' if category in ('subscription', 'rule_source') else 'system',
            'profile_name': profiles.get(profile, profile), 'target': link,
            'checked_at': task.get('last_run_at'),
            'failure_count': task.get('consecutive_failures') or 0,
            'evidence': f'{task.get("run_count", 0)}:{task.get("last_run_at") or ""}:{status}'}


def agent_observation(agent, repository):
    record = agent.get('latest_deployment') or {}
    status = record.get('status')
    profile = record.get('profile_id') or agent.get('profile_id') or repository.default_profile_id()
    labels = {'failed': '发布失败', 'rolled_back': '发布失败，已回滚', 'rollback_failed': '回滚失败',
              'ready': '等待激活', 'recovery_pending': '等待恢复检查', 'unknown': '结果待确认'}
    state = 'pending' if status in labels else 'unknown'
    if status == 'succeeded' or status == 'rolled_back' and record.get('manual_rollback'):
        state = 'resolved'
    if agent.get('enabled') is False or profile != (agent.get('profile_id') or repository.default_profile_id()):
        state = 'inactive'
    return {'state': state, 'source': 'deployment', 'profile_id': profile, 'scope': 'profile',
            'title': f'{agent.get("name") or agent["id"]} · {record.get("service_type") or agent.get("service_type", "")} 配置发布',
            'message': event_message(record.get('rollback_error') or record.get('error') or record.get('message')
                                     or labels.get(status, ''), (agent.get('token'),)),
            'impact': '已恢复发布前的配置' if status == 'rolled_back' else '',
            'status_label': labels.get(status, ''),
            'severity': 'error' if status in ('failed', 'rolled_back', 'rollback_failed') else 'warning',
            'checked_at': record.get('updated_at') or record.get('created_at'),
            'evidence': str(record.get('deployment_id')) + str(status),
            'target': target('/agents', '查看失败详情' if status in ('failed', 'rolled_back', 'rollback_failed') else '查看发布状态',
                             agent=agent['id'], profile=profile, section='deployment', deployment=record.get('deployment_id'))}


def record_agent(agent, repository):
    if agent:
        with store.transaction(repository.data_dir) as items:
            store.observe(items, 'deployment:' + agent['id'], agent_observation(agent, repository))


def router_signature(config, router):
    # Digest only: credentials and configuration contents never enter a notification.
    return hashlib.sha256(json.dumps([config, router], sort_keys=True, default=str).encode()).hexdigest()


def record_router(profile_id, config, router, report, plan=None, *, checked_at=None):
    from backend.common.config import get_repository
    from backend.routers.pending import summarize
    repository = get_repository()
    prefix = f'router:{profile_id}:{router["id"]}:'
    stamp = checked_at or store.now()
    common = {'source': 'router-check', 'scope': 'profile', 'profile_id': profile_id,
              'checked_at': stamp, 'evidence': stamp, 'signature': router_signature(config, router)}
    checks = []
    ipv4 = (report.get('routes') or {}).get('forward_hint') or {}
    checks.append(('firewall', 'IPv4 同口转发', ipv4, ipv4.get('warning') if ipv4.get('fix_command') else '',
                   'ipv4-forward-hint'))
    for section, title, anchor in [('ipv6_fakeip', 'IPv6 Fake-IP 分流路由', 'ipv6-fakeip'),
                                    ('ipv6_routes', 'IPv6 规则集路由', 'ipv6-routes'),
                                    ('ipv6_dns', 'IPv6 DNS 下发', 'ipv6-dns'),
                                    ('ipv6_pd_guard', 'IPv6 前缀防护', 'ipv6-pd-guard')]:
        data = report.get(section) or {}
        reason = '；'.join(data.get('blockers') or data.get('skip') or [])
        if data.get('fix_command'):
            reason = data.get('warning') or reason or data.get('message') or '需要在路由器终端手动处理。'
        checks.append((section, title, data, reason, anchor))
    with store.transaction(repository.data_dir) as items:
        for key, title, data, reason, section in checks:
            state = ('pending' if reason else 'unknown' if not data or data.get('status') in ('unknown', 'error')
                     else 'inactive' if data.get('enabled') is False else 'resolved')
            if not data and 'desired' in report and 'routes' in report:
                # A complete report omits sections that no longer apply to this configuration.
                state = 'inactive'
            store.observe(items, prefix + key, {**common, 'state': state, 'severity': 'warning',
                'title': f'{router.get("name") or router["id"]} · {title}', 'status_label': '需要手动处理',
                'message': event_message(reason, (router.get('password'),)),
                'target': target('/routers', '前往对应设置', router=router['id'], profile=profile_id, section=section)})
        if plan is not None:
            summary = summarize(report, plan)
            state = summary['state']
            store.observe(items, prefix + 'changes', {**common,
                'state': 'resolved' if state == 'synced' else 'pending' if state in ('pending', 'blocked') else 'unknown',
                'title': f'{router.get("name") or router["id"]} · 上游路由发布',
                'status_label': '待发布' if state == 'pending' else '需要手动处理',
                'severity': 'neutral' if state == 'pending' else 'warning',
                'message': event_message(summary['message'] or f'有 {summary["pending"]} 项配置差异待发布。', (router.get('password'),)),
                'target': target('/routers', '查看并发布' if state == 'pending' else '查看阻断原因',
                                 router=router['id'], profile=profile_id, section='report', preview='1')})
        elif report.get('in_sync') is True:
            store.observe(items, prefix + 'changes', {'state': 'resolved'})
        elif report.get('in_sync') is False:
            # The report is already evidence of drift; a preview refines which changes can be applied.
            store.observe(items, prefix + 'changes', {**common, 'state': 'pending', 'severity': 'neutral',
                'title': f'{router.get("name") or router["id"]} · 上游路由发布', 'status_label': '待确认差异',
                'message': '检查发现配置差异，请预览发布确认可应用的改动。',
                'target': target('/routers', '查看并发布', router=router['id'], profile=profile_id, section='report', preview='1')})


def _refresh(items, repository, completed_task=None):
    from backend.common.agent_manager import get_agent_manager
    from backend.routes.tasks import list_tasks
    from backend.routers.store import routers_of
    profiles = {item['id']: item['name'] for item in repository.list_profiles()}
    agents = {item['id']: item for item in get_agent_manager().get_all_agents()}
    keys = set()
    for task in list_tasks():
        if task['key'] == completed_task:
            # The receipt is final even if its worker has not yet released its running flag.
            task = {**task, 'running': False}
        key = 'task:' + task['key']
        keys.add(key)
        store.observe(items, key, task_observation(task, agents, profiles))
    for agent in agents.values():
        key = 'deployment:' + agent['id']
        keys.add(key)
        store.observe(items, key, agent_observation(agent, repository))
    routers = {}
    for profile_id in profiles:
        config = repository.get_compat_config(profile_id)
        for router in routers_of(config):
            prefix = f'router:{profile_id}:{router["id"]}:'
            routers[prefix] = router_signature(config, router)
            release = router.get('last_release') or {}
            key = prefix + 'release'
            keys.add(key)
            store.observe(items, key, {'state': 'pending' if release.get('ok') is False else
                'resolved' if release.get('ok') is True else 'unknown', 'source': 'router-release',
                'scope': 'profile', 'profile_id': profile_id, 'severity': 'error', 'status_label': '发布失败',
                'title': f'{router.get("name") or router["id"]} · 路由器发布',
                'message': event_message(release.get('message') or f'阶段「{release.get("failed_stage") or "未知"}」失败，请重新检查差异。', (router.get('password'),)),
                'checked_at': release.get('at'), 'evidence': release.get('at'),
                'target': target('/routers', '查看发布结果', router=router['id'], profile=profile_id, section='report')})
    for key, item in items.items():
        if item.get('source') == 'router-check':
            prefix = key.rsplit(':', 1)[0] + ':'
            if prefix in routers:
                keys.add(key)
                item['stale'] = item.get('signature') != routers[prefix]
        if key not in keys:
            store.observe(items, key, {'state': 'inactive'})
        if item.get('profile_id') in profiles:
            item['profile_name'] = profiles[item['profile_id']]


def snapshot(completed_task=None):
    from backend.common.config import get_repository
    repository = get_repository()
    with store.transaction(repository.data_dir) as items:
        _refresh(items, repository, completed_task)
        result = [{key: value for key, value in item.items() if key not in ('signature', 'evidence')}
                  for item in items.values()]
    priority = {'error': 0, 'warning': 1, 'neutral': 2}
    result.sort(key=lambda item: item.get('last_seen_at') or '', reverse=True)
    result.sort(key=lambda item: (item['state'] != 'pending', priority.get(item.get('severity'), 2)))
    return deepcopy(result)


def refresh_tasks(completed_task=None):
    # Called after the task receipt lock has been released, including without an open browser.
    snapshot(completed_task)
