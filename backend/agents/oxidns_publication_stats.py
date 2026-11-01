"""Event-driven dashboard checkpoints, stored only on the ConfigFlow server.

No polling thread and no Agent changes: capture immediately before submitting
an activation, then reconcile its confirmed result or the next overview GET.
"""
import logging
import re
import secrets

from . import oxidns_metrics as metrics
from .oxidns_dashboard_state import DashboardStateStore, DashboardStateError, DashboardStateConflict

logger = logging.getLogger(__name__)
TERMINAL = {'succeeded', 'failed', 'rolled_back', 'rollback_failed'}


class PublicationPending(Exception):
    """Do not discard a checkpoint while the activation outcome is unknown."""


def _new_state(snapshot, core, observations, target, reason):
    return {'schema': 1, 'generation': secrets.token_hex(16),
            'started_at_ms': snapshot['collected_at_ms'], 'reason': reason, 'target': target,
            'instance_id': core['instance_id'], 'source_revision': snapshot['source_revision'],
            'runtime_revision': snapshot['runtime_revision'], 'baseline': observations}


def _task(agent, pending):
    agent = agent or {}
    ident = pending['deployment_id']
    task = (agent.get('deployments') or {}).get(ident) or {}
    latest = agent.get('latest_deployment') or {}
    # A delayed post-publication read may be recovered while the next release
    # is still preparing. Its new checkpoint will be saved before activation.
    superseded = (latest.get('deployment_id') != ident and not (
        task.get('status') in ('succeeded', 'rolled_back')
        and latest.get('status') in ('preparing', 'uploading', 'ready')))
    if (superseded
            or task.get('service_type') != 'oxidns'
            or task.get('profile_id') != (agent.get('profile_id') or 'default')):
        return {}
    return task


def advance(previous, snapshot, core, observations, target, agent, *, manual=False):
    """Pure transition; the caller commits with generation compare-and-set."""
    reason = ('manual' if manual else 'initial' if previous is None else
              'target_changed' if previous['target'] != target else
              'core_changed' if previous['instance_id'] != core['instance_id'] else
              'configuration_changed' if previous['source_revision'] != snapshot['source_revision'] else
              'core_changed' if previous['runtime_revision'] != snapshot['runtime_revision'] else
              'counters_changed' if metrics.metrics_counters_restarted(observations, previous['baseline']) else None)
    pending = previous.get('pending') if previous else None
    if pending and not manual and previous['target'] == target:
        task = _task(agent, pending)
        status = task.get('status')
        if snapshot['collected_at_ms'] < pending['collected_at_ms']:
            raise PublicationPending()
        if status and status not in TERMINAL:
            if reason:
                raise PublicationPending()
            return previous
        expected = (pending['expected_revision'] if status == 'succeeded' else
                    previous['source_revision'] if status == 'rolled_back' else None)
        if reason and expected == snapshot['source_revision'] and not task.get('manual_rollback'):
            # Counts observed since the new runtime started belong to the same
            # dashboard period. Do not subtract the first post-reload snapshot.
            return {**previous, 'generation': secrets.token_hex(16), 'reason': 'publication',
                    'instance_id': core['instance_id'], 'source_revision': snapshot['source_revision'],
                    'runtime_revision': snapshot['runtime_revision'],
                    'baseline': metrics.zero_baseline(observations), 'carried': pending['totals'],
                    'carried_partial': pending['partial'], 'pending': None,
                    'publication_count': previous['publication_count'] + 1}
        if not reason:
            # Rejection before mutation, or a publication which did not reload.
            # Keep the original baseline; adding the checkpoint would double count.
            return {**previous, 'generation': secrets.token_hex(16), 'pending': None}
    if reason:
        return _new_state(snapshot, core, observations, target, reason)
    return previous


def totals(state, observations):
    adjusted, partial = metrics.apply_metrics_baseline(observations, state['baseline'])
    if state.get('schema') == 2:
        adjusted = metrics.add_observations(adjusted, state['carried'])
        partial = partial or state['carried_partial']
    return adjusted, partial


def checkpoint(manager, agent_id, deployment_id, expected_revision):
    """Best-effort snapshot: monitoring failure must never block publication."""
    from . import oxidns_monitor as monitor
    agent = manager.get_agent_by_id(agent_id)
    if not agent or agent.get('service_type') != 'oxidns':
        return
    try:
        agent = monitor._agent(manager, agent_id, monitor.MIN_DASHBOARD_AGENT_VERSION)
        if not isinstance(expected_revision, str) or not re.fullmatch(r'[0-9a-f]{64}', expected_revision):
            return
        store = DashboardStateStore(manager.repository.data_dir)
        previous = store.read(agent_id)
        # No dashboard period has been started for this device, so there is
        # nothing to preserve. Do not enable monitoring as a side effect of push.
        if previous is None:
            return
        if (((previous or {}).get('pending') or {}).get('deployment_id') == deployment_id
                and (agent.get('deployments') or {}).get(deployment_id, {}).get('status') != 'ready'):
            return
        snapshot, core, _, observations, _ = monitor._overview_snapshot(agent, True)
        target = monitor._target(agent)
        monitor._verify_target(manager, agent_id, target)
        state = advance(previous, snapshot, core, observations, target, agent)
        if snapshot['collected_at_ms'] < state['started_at_ms']:
            raise PublicationPending()
        if state.get('pending') and state['pending']['deployment_id'] != deployment_id:
            raise PublicationPending()
        adjusted, partial = totals(state, observations)
        pending = {'deployment_id': deployment_id, 'expected_revision': expected_revision,
                   'collected_at_ms': snapshot['collected_at_ms'], 'totals': adjusted, 'partial': partial}
        if not _task(agent, pending):
            return
        state = {**state, 'schema': 2, 'generation': secrets.token_hex(16), 'pending': pending,
                 'carried': state.get('carried', {'servers': [], 'caches': [], 'upstreams': []}),
                 'carried_partial': state.get('carried_partial', False),
                 'publication_count': state.get('publication_count', 0)}
        def verify():
            monitor._verify_target(manager, agent_id, target)
            if _task(manager.get_agent_by_id(agent_id), pending).get('status') not in ('uploading', 'ready'):
                raise DashboardStateConflict()
        store.compare_and_set(agent_id, previous['generation'] if previous else None, state, verify=verify)
    except (monitor.MonitorError, DashboardStateError, DashboardStateConflict, PublicationPending):
        # Never include upstream exceptions: they can contain credentials.
        logger.warning('OxiDNS 发布前统计快照不可用，本次发布的统计可能重新开始')


def complete(manager, agent_id, deployment_id, status):
    """Reconcile once a transaction settles; retries are idempotent after CAS."""
    if status not in TERMINAL:
        return
    from . import oxidns_monitor as monitor
    agent = manager.get_agent_by_id(agent_id)
    if not agent or agent.get('service_type') != 'oxidns':
        return
    try:
        store = DashboardStateStore(manager.repository.data_dir)
        previous = store.read(agent_id)
        pending = (previous or {}).get('pending')
        if not pending or pending['deployment_id'] != deployment_id:
            return
        snapshot, core, _, observations, _ = monitor._overview_snapshot(agent, True)
        target = monitor._target(agent)
        state = advance(previous, snapshot, core, observations, target, agent)
        store.compare_and_set(agent_id, previous['generation'], state if state != previous else None,
                              verify=lambda: monitor._verify_target(manager, agent_id, target))
    except (monitor.MonitorError, DashboardStateError, DashboardStateConflict, PublicationPending):
        logger.warning('OxiDNS 发布后统计暂未接续，将在下次查看时重试')
