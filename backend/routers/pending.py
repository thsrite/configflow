"""Upstream routers with changes to publish, for one Mihomo proxy gateway.

Pages that edit Mihomo only remind and link; the router itself is published from
the 上游路由 page after its preview. Each router that uses the Agent as its proxy
gateway, in any profile, is read live with the same drift report and release
preview as that page (``_inspect`` and ``build_plan``); nothing is written.

Reads run concurrently under one deadline and are reused for as long as the
cache key is unchanged. The key covers each linked router record, its profile's
configuration revision and the Agent record, so a saved draft or a release is
never answered from an older read; saving, publishing and the user's
「重新检查」 also force a new read (``refresh``), since edits made directly on a
router are not seen. A read with a router that could not be read is not reused:
``unknown`` is never kept as a result, nor turned into ``synced``.
"""
import copy
import hashlib
import json
import threading
from collections import OrderedDict
from concurrent.futures import Future, ThreadPoolExecutor, wait
from datetime import datetime
from typing import Any, Dict, List, Optional, Tuple

from backend.routers.base import RouterError
from backend.routers.store import routers_of
from backend.utils.logger import get_logger

ROUTER_TIMEOUT = 25      # seconds for all routers of one Agent, read in parallel
MAX_WORKERS = 8
MAX_AGENTS = 64          # cached Agents
AGENT_FIELDS = ('id', 'host', 'port', 'profile_id', 'service_type', 'enabled', 'status', 'version')
GENERIC_BLOCKED = '差异报告中有暂时无法发布的项目，请打开差异报告查看原因。'
SECTION_WORDS = {'missing': '待添加', 'pending': '待更新', 'conflict': '冲突', 'unknown': '待确认'}
logger = get_logger(__name__)


def _change_word(ops: List[Dict[str, Any]]) -> str:
    kinds = {str(op.get('kind') or '') for op in ops}
    if kinds == {'route_comment'}:  # an existing hand-made route is tagged, not changed
        return '待接管'
    if kinds and all(kind == 'add' or kind.endswith('_add') for kind in kinds):
        return '待添加'
    if kinds and all(kind == 'delete' or kind.endswith(('_delete', '_remove')) for kind in kinds):
        return '待移除'
    return '待更新'


def _stage_word(stage: Dict[str, Any], report: Dict[str, Any]) -> str:
    """Short status for one release stage, worded like the report section it comes from."""
    key = stage.get('key')
    section = report.get(key) if isinstance(report.get(key), dict) else {}
    if key in ('ipv6_fakeip', 'ipv6_routes', 'dns_guard', 'ipv6_pd_guard') and section.get('enabled') is False:
        return '待撤回'
    if key == 'ipv6_fakeip' and section.get('status') in SECTION_WORDS:
        return SECTION_WORDS[section['status']]
    if key == 'ipv6_routes':
        statuses = {item.get('status') for item in section.get('items') or []} - {'synced', 'borrowed'}
        return '待添加' if statuses == {'missing'} else '待更新'
    if key == 'ipv6_dns':
        return '待关闭'
    if key == 'ipv6_pd_guard':
        statuses = {item.get('status') for item in section.get('items') or []} - {'synced', 'borrowed'}
        return '待安装' if statuses == {'missing'} else '待接管' if statuses == {'adopt'} else '待更新'
    if key == 'return_cleanup':
        return '待移除'
    return _change_word(stage.get('ops') or [])


def _blocked_reasons(report: Dict[str, Any]) -> List[str]:
    """Why an unsynchronised report has nothing to publish, in the report's own words."""
    reasons = list((report.get('ipv6_routes') or {}).get('skip') or [])
    reasons += list((report.get('ipv6_dns') or {}).get('blockers') or [])
    reasons += list((report.get('ipv6_pd_guard') or {}).get('blockers') or [])
    for section in ((report.get('routes') or {}).get('items') or [], (report.get('nat_bypass') or {}).get('items') or []):
        reasons += [f'{item.get("cidr")}：{item.get("detail")}' if item.get('detail') else f'{item.get("cidr")} 存在冲突'
                    for item in section if item.get('status') == 'conflict']
    return [reason for reason in reasons if isinstance(reason, str) and reason.strip()]


def _first(reasons: List[str]) -> str:
    return reasons[0] + (f'（另有 {len(reasons) - 1} 项）' if len(reasons) > 1 else '')


def summarize(report: Dict[str, Any], plan: Dict[str, Any]) -> Dict[str, Any]:
    """State, pending count and short item list of one router's report and release preview.

    ``pending`` counts the changes the preview lists in its enabled stages, the
    same numbers the 预览发布 dialog shows. Plan blockers, or differences that
    no stage can publish (a skipped stage, a route conflict), are ``blocked``.
    """
    items = []
    skipped = False
    for stage in plan.get('stages') or []:
        if stage.get('enabled') is False:
            continue
        label = stage.get('title') or stage.get('key') or ''
        if stage.get('skipped'):
            skipped = True
            items.append({'key': stage.get('key') or '', 'label': label, 'status': '本次跳过', 'count': 0})
        elif stage.get('ops'):
            items.append({'key': stage.get('key') or '', 'label': label, 'status': _stage_word(stage, report),
                          'count': len(stage['ops'])})
    pending = sum(item['count'] for item in items)
    blockers = [item for item in plan.get('blockers') or [] if isinstance(item, str) and item.strip()]
    if blockers:
        state, message = 'blocked', _first(blockers)
    elif pending:
        state, message = 'pending', ''
    elif skipped or report.get('in_sync') is not True:
        state, message = 'blocked', _first(_blocked_reasons(report) or [GENERIC_BLOCKED])
    else:
        state, message = 'synced', ''
    return {'state': state, 'pending': pending, 'items': items, 'message': message}


def _read_router(config: Dict[str, Any], router: Dict[str, Any]) -> Dict[str, Any]:
    """One live, read-only report and release preview, exactly as the 上游路由 page builds them."""
    from backend.routers.plan import build_plan
    from backend.routes.routers import _driver, _inspect
    driver = _driver(router)
    try:
        _, desired, state, report = _inspect(config, router, driver)
        plan = build_plan(router, desired, state, report, report['return_routing'], driver)
    finally:
        driver.close()
    return {**summarize(report, plan), '_notification_report': report, '_notification_plan': plan}


def _unknown(message: str) -> Dict[str, Any]:
    return {'state': 'unknown', 'pending': 0, 'items': [], 'message': message}


def _read_all(targets: List[Tuple[Dict[str, Any], Dict[str, Any], Dict[str, Any]]],
              timeout: float) -> List[Dict[str, Any]]:
    """Router entries in target order; a slow or failing router never hides the others."""
    pool = ThreadPoolExecutor(max_workers=min(MAX_WORKERS, len(targets)), thread_name_prefix='upstream-pending')
    futures = [pool.submit(_read_router, copy.deepcopy(config), copy.deepcopy(router))
               for _, config, router in targets]
    wait(futures, timeout=timeout)
    pool.shutdown(wait=False, cancel_futures=True)
    entries = []
    for (profile, config, router), future in zip(targets, futures):
        if not future.done() or future.cancelled():
            result = _unknown(f'读取超时（{timeout:g} 秒），请检查路由器负载和网络后重试。')
        else:
            try:
                result = future.result()
            except RouterError as error:
                result = _unknown(str(error) or '读取路由器失败')
            except Exception as error:  # a parsing bug must not hide the other routers
                logger.exception('Upstream router status failed for router %s', router.get('id'))
                result = _unknown(f'读取失败：{error.__class__.__name__}')
        report = result.pop('_notification_report', None)
        plan = result.pop('_notification_plan', None)
        if report is not None:
            from backend.notifications import safely, record_router
            safely(record_router, profile['id'], config, router, report, plan)
        entries.append({'id': router.get('id') or '', 'name': router.get('name') or router.get('id') or '',
                        'type': router.get('type') or '', 'profile_id': profile['id'],
                        'profile_name': profile.get('name') or profile['id'], **result})
    return entries


def _targets(agent_id: str):
    """Routers in any profile that use this Agent as proxy gateway, the Agent's profile and a cache key."""
    from backend.common.agent_manager import get_agent_manager
    from backend.common.config import get_config, get_repository
    repository = get_repository()
    agent = get_agent_manager().get_agent_by_id(agent_id)
    targets, key = [], []
    for profile in repository.list_profiles():
        config = get_config(profile['id'])
        linked = [router for router in routers_of(config) if router.get('proxy_gateway_agent_id') == agent_id]
        if linked:
            targets += [(profile, config, router) for router in linked]
            key.append([profile['id'], profile.get('name'), profile.get('revision'), linked])
    agent_profile = (agent or {}).get('profile_id') or repository.default_profile_id()
    key.append({field: (agent or {}).get(field) for field in AGENT_FIELDS} if agent else None)
    # Router records include credentials: only their digest is kept.
    signature = hashlib.sha256(json.dumps(key, sort_keys=True, default=str).encode()).hexdigest()
    return targets, agent_profile, signature


class _Entry:
    __slots__ = ('signature', 'routers', 'checked_at', 'pending')

    def __init__(self, signature: str):
        self.signature = signature
        self.routers: Optional[List[Dict[str, Any]]] = None
        self.checked_at = ''
        self.pending: Optional[Future] = None


class PendingCache:
    """Per-Agent results kept while their key holds; simultaneous requests share one read."""

    def __init__(self):
        self._lock = threading.Lock()
        self._entries: 'OrderedDict[str, _Entry]' = OrderedDict()

    def clear(self):
        with self._lock:
            self._entries.clear()

    def read(self, agent_id: str, signature: str, load, refresh: bool = False):
        """(routers, checked_at, cached) for this signature, reading at most once at a time."""
        with self._lock:
            entry = self._entries.get(agent_id)
            if entry is None or entry.signature != signature:
                entry = _Entry(signature)
                self._entries[agent_id] = entry
            self._entries.move_to_end(agent_id)
            while len(self._entries) > MAX_AGENTS:
                self._entries.popitem(last=False)
            if (entry.routers is not None and not refresh and entry.pending is None
                    and all(router.get('state') != 'unknown' for router in entry.routers)):
                return copy.deepcopy(entry.routers), entry.checked_at, True
            owner = entry.pending is None
            if owner:
                entry.pending = Future()
            pending = entry.pending
        if not owner:
            # A read already in progress is at least as fresh as a new one.
            routers, checked_at = pending.result(timeout=ROUTER_TIMEOUT + 10)
            return copy.deepcopy(routers), checked_at, False
        try:
            checked_at = datetime.now().astimezone().isoformat(timespec='seconds')
            routers = load()
        except BaseException as failure:
            with self._lock:
                entry.pending = None
            pending.set_exception(failure)
            raise
        with self._lock:
            entry.routers, entry.checked_at, entry.pending = routers, checked_at, None
        pending.set_result((routers, checked_at))
        return copy.deepcopy(routers), checked_at, False


_cache = PendingCache()


def agent_routers(agent_id: str, refresh: bool = False, *, cache: Optional[PendingCache] = None,
                  timeout: Optional[float] = None) -> Dict[str, Any]:
    """Payload of GET /api/agents/<agent_id>/upstream-routers."""
    timeout = timeout or ROUTER_TIMEOUT
    targets, agent_profile, signature = _targets(agent_id)
    result = {'agent_id': agent_id, 'agent_profile_id': agent_profile, 'routers': [], 'cached': False,
              'checked_at': datetime.now().astimezone().isoformat(timespec='seconds')}
    if not targets:
        return result
    routers, checked_at, cached = (cache or _cache).read(agent_id, signature, lambda: _read_all(targets, timeout),
                                                         refresh)
    result.update(routers=routers, checked_at=checked_at, cached=cached)
    return result
