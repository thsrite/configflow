"""系统设置 → 定时任务：每个后台任务一行，带最近一次结果与下一次运行时间。

Subscriptions and library rule sources are refreshed by the server-side
scheduler (utils/scheduled_refresh). The other rows describe jobs that already
run on their own (router rule sync, Agent rule sync, DNS follow, domain
discovery, WebDAV backup); their own state stays the source of truth and
task_status adds the run history. Agent monitoring history is saved by its own
per-process loop (agents/metrics_flush) and shown as one system-level row.
"""
from datetime import timedelta
import threading
from urllib.parse import urlsplit

from flask import Blueprint, jsonify, request

from backend.common.auth import require_auth
from backend.utils import task_status
from backend.utils import scheduled_refresh as refresh

tasks_bp = Blueprint('tasks', __name__, url_prefix='/api/tasks')

DOMAIN_DISCOVERY_SECONDS = 600
_background = set()
_background_guard = threading.Lock()


def _iso(value):
    return value.isoformat(timespec='seconds').replace('+00:00', 'Z') if value else None


def _history(entry):
    keys = ('last_run_at', 'last_status', 'last_error', 'last_duration_ms', 'last_trigger',
            'last_success_at', 'run_count', 'failure_count', 'consecutive_failures', 'cache_retained', 'cache_updated_at')
    row = {key: entry.get(key) for key in keys}
    row['run_count'] = int(row['run_count'] or 0)
    row['failure_count'] = int(row['failure_count'] or 0)
    return row


def _row(key, category, name, **fields):
    row = {'key': key, 'category': category, 'name': name, 'detail': '', 'resource_id': '',
           'profile_id': None, 'profile_name': None, 'enabled': True, 'interval_seconds': None,
           'schedule_text': '', 'editable': False, 'can_run': False, 'run_hint': '', 'running': False,
           'next_run_at': None, 'state_message': '', 'link': None}
    row.update(fields)
    return row


def _host(url):
    try:
        return urlsplit(url or '').hostname or ''
    except ValueError:
        return ''


def _subscription_rows(shared, statuses):
    rows = []
    for sub in shared.get('subscriptions', []):
        key = refresh.subscription_key(sub['id'])
        entry = statuses.get(key) or {}
        interval = refresh.subscription_interval(sub)
        enabled = sub.get('enabled', True) is not False and bool(sub.get('url'))
        due = refresh.next_run_at(entry, interval, refresh.subscription_cache_time(sub)) if enabled else None
        rows.append(_row(key, 'subscription', sub.get('name') or sub['id'], resource_id=sub['id'],
            enabled=enabled and bool(interval), interval_seconds=interval, editable=bool(sub.get('url')),
            notifications_enabled=enabled,
            can_run=bool(sub.get('url')), run_hint='' if sub.get('url') else '没有订阅地址',
            running=refresh.is_running(key), next_run_at=_iso(due),
            state_message='' if sub.get('enabled', True) is not False else '订阅已停用',
            link='/subscriptions', **_history(entry)))
    return rows


def _rule_rows(shared, statuses):
    rows = []
    for rule in shared.get('rule_library', []):
        if rule.get('source_type', 'url') != 'url' or not rule.get('url'):
            continue
        key = refresh.rule_source_key(rule['id'])
        entry = statuses.get(key) or {}
        interval = refresh.rule_interval(rule)
        enabled = rule.get('enabled', True) is not False
        due = refresh.next_run_at(entry, interval, refresh.rule_cache_time(rule)) if enabled else None
        history = _history(entry)
        if not history['cache_updated_at']:
            history['cache_updated_at'] = refresh.rule_cache_time(rule)
        rows.append(_row(key, 'rule_source', rule.get('name') or rule['id'], resource_id=rule['id'],
            detail=_host(rule.get('url')), enabled=enabled and bool(interval), interval_seconds=interval,
            notifications_enabled=enabled,
            editable=True, can_run=True, running=refresh.is_running(key), next_run_at=_iso(due),
            state_message='' if enabled else '规则集已停用', link='/rule-library', **history))
    return rows


def _router_rows(repository, statuses):
    from backend.routers import rule_sync_store as sync_store
    from backend.routers.store import routers_of
    rows = []
    for profile in repository.list_profiles():
        config = repository.get_profile(profile['id'])
        for router in routers_of(config):
            if router.get('type') != 'routeros':
                continue
            key = f"router_rule_sync:{profile['id']}:{router['id']}"
            receipt = sync_store.read(profile['id'], router['id'])
            enabled = router.get('auto_rule_sync') is True
            interval = int(router.get('rule_sync_interval') or 60) * 60
            status = receipt.get('status', 'waiting')
            ready = bool(receipt.get('baseline'))
            history = _history(statuses.get(key) or {})
            history['last_run_at'] = receipt.get('checked_at') or history['last_run_at']
            history['last_status'] = {'synced': 'success', 'failed': 'failed', 'paused': 'failed'}.get(
                status, history['last_status'] if status in ('running', 'queued') else None)
            history['last_error'] = receipt.get('message', '') if status in ('failed', 'paused') else ''
            checked = task_status.parse_time(receipt.get('checked_at'))
            due = None
            if enabled and ready and status != 'paused':
                due = checked + timedelta(seconds=interval) if checked else None
            hint = ''
            if not enabled:
                hint = '自动同步未开启'
            elif not ready:
                hint = '请先在上游路由手动预览并发布一次'
            elif status in ('running', 'queued', 'paused'):
                hint = '同步尚未完成或需要人工确认'
            message = ''
            if not ready:
                message = '等待首次手动发布建立同步基准'
            elif status == 'paused':
                message = '已暂停，需要在上游路由手动预览发布'
            rows.append(_row(key, 'router_rule_sync', router.get('name') or router['id'], resource_id=router['id'],
                profile_id=profile['id'], profile_name=profile.get('name') or profile['id'],
                enabled=enabled, interval_seconds=interval, editable=True, can_run=not hint, run_hint=hint,
                running=status in ('running', 'queued'), next_run_at=_iso(due), state_message=message,
                link='/routers', **history))
    return rows


def _agent_rows(manager, statuses):
    from backend.agents.rule_sync import settings_for
    rows = []
    for agent in manager.get_all_agents():
        name = agent.get('name') or agent['id']
        if agent.get('service_type') in ('mihomo', 'oxidns'):
            key = f"agent_rule_sync:{agent['id']}"
            enabled = any(settings_for(agent).values())
            state = (agent.get('rule_sync') or {}).get('state') or {}
            if enabled or key in statuses:
                history = _history(statuses.get(key) or {})
                status = state.get('status')
                if enabled and status == 'blocked':
                    history['last_status'], history['last_error'] = 'skipped', state.get('message', '')
                elif enabled and status in ('succeeded', 'failed'):
                    history['last_status'] = 'success' if status == 'succeeded' else 'failed'
                    history['last_error'] = state.get('message', '') if status == 'failed' else ''
                rows.append(_row(key, 'agent_rule_sync', name, resource_id=agent['id'], enabled=enabled,
                    schedule_text='规则变化后自动同步', run_hint='在 Agent 卡片上点「更新规则」',
                    running=enabled and status == 'running',
                    state_message=state.get('message', '') if enabled and status == 'pending' else '',
                    link='/agents', **history))
        if agent.get('service_type') == 'oxidns' and (agent.get('dns_follow') or f"dns_follow:{agent['id']}" in statuses):
            key = f"dns_follow:{agent['id']}"
            state = agent.get('dns_follow_state') or {}
            status = state.get('status')
            history = _history(statuses.get(key) or {})
            if status in ('succeeded', 'failed'):
                history['last_status'] = 'success' if status == 'succeeded' else 'failed'
                history['last_error'] = state.get('message', '') if status == 'failed' else ''
            enabled = bool(agent.get('dns_follow'))
            rows.append(_row(key, 'dns_follow', name, resource_id=agent['id'], enabled=enabled,
                schedule_text='Mihomo 发布成功后同步', can_run=enabled and status == 'failed',
                run_hint='' if status == 'failed' else '只有失败后可以重试',
                running=status in ('preparing', 'publishing'), link='/agents',
                state_message=state.get('message', '') if status in ('waiting', 'preparing', 'publishing') else '',
                **history))
    return rows


def _domain_rows(repository, statuses):
    from backend.utils.domain_discovery_service import auto_task_key, read_settings
    rows = []
    for profile in repository.list_profiles():
        key = auto_task_key(profile['id'])
        settings = read_settings(repository.get_profile(profile['id']))
        if not settings['auto_probe'] and key not in statuses:
            continue
        entry = statuses.get(key) or {}
        last = task_status.parse_time(entry.get('last_run_at'))
        due = last + timedelta(seconds=DOMAIN_DISCOVERY_SECONDS) if settings['auto_probe'] and last else None
        rows.append(_row(key, 'domain_discovery', '域名发现自动探测', resource_id=profile['id'],
            profile_id=profile['id'], profile_name=profile.get('name') or profile['id'],
            enabled=settings['auto_probe'], interval_seconds=DOMAIN_DISCOVERY_SECONDS,
            can_run=settings['auto_probe'], run_hint='' if settings['auto_probe'] else '自动探测未开启',
            running=_is_background(key), next_run_at=_iso(due), link='/domain-discovery', **_history(entry)))
    return rows


def _backup_row(repository, statuses):
    from backend.utils.webdav_backup import BACKUP_TASK_KEY
    backup = repository.get_system().get('backup') or {}
    configured = all(backup.get(key) for key in ('webdav_url', 'webdav_username', 'webdav_password'))
    if not configured and BACKUP_TASK_KEY not in statuses:
        return []
    return [_row(BACKUP_TASK_KEY, 'webdav_backup', 'WebDAV 自动备份', enabled=bool(backup.get('auto_backup')),
        schedule_text='配置变更后自动备份', can_run=configured, run_hint='' if configured else '请先填写 WebDAV 配置',
        running=_is_background(BACKUP_TASK_KEY), link='/system-settings', **_history(statuses.get(BACKUP_TASK_KEY) or {}))]


def _metrics_row(statuses):
    from backend.agents import metrics_flush
    entry = statuses.get(metrics_flush.TASK_KEY) or {}
    interval = metrics_flush.interval_seconds()
    # 一个任务保存两样数据，写明各属于哪个软件
    return [_row(metrics_flush.TASK_KEY, 'metrics_history', '监控历史保存', interval_seconds=interval,
        detail='Agent 系统监控（CPU、内存、网速） · Mihomo 流量记录',
        editable=True, can_run=True, running=_is_background(metrics_flush.TASK_KEY) or metrics_flush.is_running(),
        next_run_at=_iso(metrics_flush.next_run_at(entry, interval)), link='/agents', **_history(entry))]


def _is_background(key):
    with _background_guard:
        return key in _background


def _start_background(key, work):
    with _background_guard:
        if key in _background:
            raise refresh.TaskBusy('这个任务正在运行，请稍后查看结果')
        _background.add(key)

    def run():
        from backend.common.config import reset_config_context
        try:
            work()
        except Exception:
            pass  # recorded in task_status by the task itself
        finally:
            reset_config_context()
            with _background_guard:
                _background.discard(key)

    try:
        threading.Thread(target=run, name=f'task-{key[:40]}', daemon=True).start()
    except Exception:
        with _background_guard:
            _background.discard(key)
        raise


def list_tasks():
    from backend.common.agent_manager import get_agent_manager
    from backend.common.config import get_repository
    repository = get_repository()
    statuses = task_status.load()
    shared = repository.get_shared()
    return (_subscription_rows(shared, statuses) + _rule_rows(shared, statuses)
            + _router_rows(repository, statuses) + _agent_rows(get_agent_manager(), statuses)
            + _domain_rows(repository, statuses) + _backup_row(repository, statuses) + _metrics_row(statuses))


@tasks_bp.route('', methods=['GET'])
@require_auth
def get_tasks():
    response = jsonify({'success': True, 'tasks': list_tasks()})
    response.headers['Cache-Control'] = 'no-store'
    return response


def _body_key():
    body = request.get_json(silent=True)
    if not isinstance(body, dict) or not isinstance(body.get('key'), str) or not body['key']:
        return None, body
    return body['key'], body


@tasks_bp.route('/run', methods=['POST'])
@require_auth
def run_task():
    key, _ = _body_key()
    if key is None:
        return jsonify(success=False, message='请指定任务'), 400
    category, _, resource = key.partition(':')
    try:
        if category in ('subscription', 'rule_source'):
            refresh.run_now(category, resource)
        elif category == 'router_rule_sync':
            from backend.routers.base import RouterError
            from backend.routers.rule_sync import start_job
            profile_id, _, router_id = resource.partition(':')
            try:
                start_job(profile_id, router_id)
            except RouterError as error:
                return jsonify(success=False, message=str(error)), 409
        elif category == 'dns_follow':
            from backend.agents import dns_follow
            from backend.common.agent_manager import get_agent_manager
            dns_follow.retry(get_agent_manager(), resource)
        elif category == 'domain_discovery':
            from backend.common.config import get_repository
            from backend.utils.domain_discovery_service import read_settings, run_auto_task
            if not read_settings(get_repository().get_profile(resource))['auto_probe']:
                return jsonify(success=False, message='自动探测未开启'), 409
            _start_background(key, lambda: run_auto_task(resource, 'manual'))
        elif category == 'webdav_backup':
            from backend.common.config import get_repository
            from backend.utils.webdav_backup import upload_backup

            def backup():
                repository = get_repository()
                snapshot = repository.export_all()
                upload_backup(snapshot, snapshot['system'].get('backup', {}))
            _start_background(key, backup)
        elif key == 'metrics_history_flush':
            from backend.agents import metrics_flush
            _start_background(key, lambda: metrics_flush.run('manual'))
        else:
            return jsonify(success=False, message='这个任务不能在这里立即运行'), 400
    except refresh.TaskBusy as error:
        return jsonify(success=False, message=str(error)), 409
    except LookupError as error:
        return jsonify(success=False, message=str(error).strip("'")), 404
    except ValueError as error:
        return jsonify(success=False, message=str(error)), 409
    return jsonify(success=True, message='已开始运行，结果会显示在列表中'), 202


@tasks_bp.route('/interval', methods=['PUT'])
@require_auth
def set_interval():
    """Subscriptions, rule sources and monitoring history; routers keep their own settings API."""
    from backend.common.config import update_shared_config_transaction
    from backend.utils.subscription_refresh import validate_refresh_interval
    key, body = _body_key()
    if key is None or set(body) != {'key', 'interval_seconds'}:
        return jsonify(success=False, message='请指定任务和更新间隔'), 400
    if key == 'metrics_history_flush':
        from backend.agents import metrics_flush
        try:
            metrics_flush.set_interval(body['interval_seconds'])
        except ValueError as error:
            return jsonify(success=False, message=str(error)), 400
        return jsonify(success=True, task=next((row for row in list_tasks() if row['key'] == key), None))
    category, _, resource_id = key.partition(':')
    if category not in ('subscription', 'rule_source'):
        return jsonify(success=False, message='这个任务的间隔不能在这里修改'), 400
    try:
        interval = validate_refresh_interval(body['interval_seconds'], '更新间隔')
    except ValueError as error:
        return jsonify(success=False, message=str(error)), 400
    field = 'interval' if category == 'subscription' else 'update_interval'

    def mutate(shared):
        items = shared.get('subscriptions' if category == 'subscription' else 'rule_library', [])
        item = next((item for item in items if item.get('id') == resource_id), None)
        if item is None:
            raise LookupError('任务对应的来源已不存在，请刷新后重试')
        item[field] = interval

    try:
        update_shared_config_transaction(mutate)
    except LookupError as error:
        return jsonify(success=False, message=str(error).strip("'")), 404
    return jsonify(success=True, task=next((row for row in list_tasks() if row['key'] == key), None))
