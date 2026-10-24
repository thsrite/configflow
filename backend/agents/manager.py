"""Agent 管理器"""
import secrets
import hashlib
import hmac
import re
import os
import fcntl
import logging
import threading
import time
from copy import deepcopy
from datetime import datetime, timedelta, timezone
from typing import Dict, Any, List, Optional
import requests
from . import runtime_state
from .metrics_history import MetricsHistory
from .diagnostics import dns_report_fields, failure_text, response_failure
from .operation_events import append_event, event_time

logger = logging.getLogger(__name__)
_DEPLOYMENT_RESULTS = {
    'succeeded': (logging.INFO, '配置发布成功'), 'failed': (logging.WARNING, '配置发布失败'),
    'rolled_back': (logging.WARNING, '配置发布未生效，已回滚到原配置'),
    'rollback_failed': (logging.ERROR, '配置发布失败，且回滚失败'),
}


def _agent_label(agent):
    from backend.utils.logger import log_text
    return log_text((agent or {}).get('name') or (agent or {}).get('id'))


def _constant_time_ascii_equal(provided: Any, expected: Any) -> bool:
    """Compare credentials without timing leaks; non-ASCII is simply invalid."""
    if not isinstance(provided, str) or not isinstance(expected, str):
        return False
    try:
        return hmac.compare_digest(provided.encode('ascii'), expected.encode('ascii'))
    except UnicodeEncodeError:
        return False


class AgentDeploymentConflict(ValueError):
    """A profile binding cannot change during an unfinished deployment."""


class _NothingToSave(Exception):
    """Aborts a configuration transaction that would change nothing."""


def _upgrade_busy(agent):
    upgrade = agent.get('latest_upgrade') or {}
    return bool(upgrade) and upgrade.get('status') not in ('succeeded', 'failed', 'rolled_back')


def _dns_follow_busy(agents, agent_id):
    return any((item.get('dns_follow_state') or {}).get('status') in ('preparing', 'publishing')
               and (item['id'] == agent_id or (item.get('dns_follow') or {}).get('source_agent_id') == agent_id)
               for item in agents)


class AgentManager:
    """Agent 管理器，负责 Agent 的注册、心跳、状态管理等"""

    def __init__(self, repository):
        """
        初始化 Agent 管理器

        Args:
            repository: system/profile 配置仓库
        """
        self.repository = repository
        self._preparing_deployments = set()
        self._preparation_files = {}
        self._deployment_lock = threading.RLock()

        # 初始化监控历史管理器
        self.metrics_history = MetricsHistory()
        # 心跳状态只放在内存，不再每次心跳重写整份配置；版本号变化时才写配置
        self.runtime = runtime_state.for_data_dir(repository.data_dir)
        runtime_state.import_legacy_database(repository, self.runtime, self._save_reported_fields)

    def _agents(self) -> List[Dict[str, Any]]:
        return self.runtime.overlay(self.repository.get_system().get('agents', []))

    def _save_reported_fields(self, changes):
        """Write {agent_id: {field: value}} reported by heartbeats into the configuration in one transaction.

        Returns the IDs written; when none of the Agents exists any more nothing is written.
        """
        saved = set()

        def update(agents):
            for item in agents:
                if item['id'] in changes:
                    item.update(changes[item['id']])
                    saved.add(item['id'])
            if not saved:
                raise _NothingToSave()
        try:
            self._update_agents(update)
        except _NothingToSave:
            pass
        return saved

    def _update_agents(self, updater, profile_id=None):
        result = {}

        def update(system):
            if profile_id:
                self.repository._profile_metadata(profile_id)
            result['value'] = updater(system.setdefault('agents', []))

        self.repository.update_system_transaction(update)
        return result.get('value')

    def register_agent(
        self,
        agent_data: Dict[str, Any],
        existing_token: Optional[str] = None,
        installation_id: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        注册新的 Agent

        如果已存在相同名称和地址的 Agent，则更新该记录（保留 ID 和 token）
        否则创建新记录

        Args:
            agent_data: Agent 信息，包含 name, host, port, service_type

        Returns:
            Dict: 新 Agent 包含一次性返回的 token；已有 Agent 只返回非敏感状态
        """
        agent_name = agent_data.get('name', 'Unnamed Agent')
        agent_host = agent_data.get('host', '')
        requested_profile_id = agent_data.get('profile_id')
        if requested_profile_id:
            from backend.common.config import get_repository
            get_repository().validate_profile_id(requested_profile_id)

        def register(agents):
            existing_agent = next(
                (agent for agent in agents if agent.get('name') == agent_name and agent.get('host') == agent_host),
                None,
            )
            if existing_agent:
                if not _constant_time_ascii_equal(existing_token, existing_agent.get('token')):
                    raise PermissionError('Unauthorized')
                if (installation_id and existing_agent.get('gateway_installation_id') == installation_id
                        and requested_profile_id != existing_agent.get('profile_id')):
                    raise AgentDeploymentConflict('Installation Agent profile changed; prepare a new installation')
                if (requested_profile_id and requested_profile_id != (existing_agent.get('profile_id') or self.repository.default_profile_id())
                        and (_upgrade_busy(existing_agent) or any(
                            task.get('status') not in ('succeeded', 'failed', 'rolled_back')
                            for task in existing_agent.get('deployments', {}).values()))):
                    raise AgentDeploymentConflict('Agent has an unfinished deployment or upgrade; recover it before changing profile')
                if _dns_follow_busy(agents, existing_agent['id']) and (
                        agent_data.get('service_type', 'mihomo') != existing_agent.get('service_type')
                        or (requested_profile_id and requested_profile_id != existing_agent.get('profile_id'))
                        or agent_data.get('port', 8080) != existing_agent.get('port')):
                    raise AgentDeploymentConflict('DNS 联动发布正在执行，请等待结果后再改变 Agent 注册信息')
                agent_id = existing_agent['id']
                token = existing_agent['token']
                updated_agent = {
                    'id': agent_id,
                    'name': agent_name,
                    'host': agent_host,
                    'port': agent_data.get('port', 8080),
                    'token': token,
                    'service_type': agent_data.get('service_type', 'mihomo'),
                    'profile_id': requested_profile_id or (existing_agent.get('profile_id') or self.repository.default_profile_id()),
                    'deployment_method': agent_data.get('deployment_method', existing_agent.get('deployment_method', 'unknown')),
                    'status': 'online',
                    'last_heartbeat': datetime.now().isoformat(),
                    'version': agent_data.get('version', '1.0.0'),
                    'config_version': existing_agent.get('config_version', '0'),
                    'enabled': True,
                    'created_at': existing_agent.get('created_at', datetime.now().isoformat()),
                    'updated_at': datetime.now().isoformat(),
                    'deployments': existing_agent.get('deployments', {}),
                    'latest_deployment': existing_agent.get('latest_deployment'),
                    'latest_upgrade': existing_agent.get('latest_upgrade'),
                    # 管理端设置的开关，重新注册不能把它清掉
                    'domain_discovery_enabled': existing_agent.get('domain_discovery_enabled', False),
                }
                for field in ('config_revision', 'pushed_at', 'applied_content', 'dns_follow', 'dns_follow_state', 'dns_follow_job', 'rule_sync'):
                    if field in existing_agent:
                        updated_agent[field] = deepcopy(existing_agent[field])
                if existing_agent.get('gateway_installation_id'):
                    updated_agent['gateway_installation_id'] = existing_agent['gateway_installation_id']
                agents[agents.index(existing_agent)] = updated_agent
                return {'id': agent_id, 'status': 'online', 'is_new': False}

            agent_id = f"agent_{int(datetime.now().timestamp() * 1000)}_{secrets.token_hex(3)}"
            token = secrets.token_urlsafe(24)
            agent = {
                'id': agent_id,
                'name': agent_name,
                'host': agent_host,
                'port': agent_data.get('port', 8080),
                'token': token,
                'service_type': agent_data.get('service_type', 'mihomo'),
                'profile_id': requested_profile_id or self.repository.default_profile_id(),
                'deployment_method': agent_data.get('deployment_method', 'unknown'),
                'status': 'online',
                'last_heartbeat': datetime.now().isoformat(),
                'version': agent_data.get('version', '1.0.0'),
                'config_version': '0',
                'enabled': True,
                'created_at': datetime.now().isoformat(),
            }
            if installation_id:
                # Trusted caller metadata, never read from registration JSON.
                # Persist with the identity so a lost response can be replayed
                # by that same short-lived installation capability only.
                agent['gateway_installation_id'] = installation_id
            agents.append(agent)
            return {
                'id': agent_id,
                'status': 'online',
                'is_new': True,
                'token': token,
            }

        return self._update_agents(register, requested_profile_id)

    def get_all_agents(self) -> List[Dict[str, Any]]:
        """获取所有 Agent 列表"""
        agents = self._agents()

        # 更新在线状态（超过 2 分钟未心跳视为离线）
        now = datetime.now()
        for agent in agents:
            try:
                last_heartbeat = datetime.fromisoformat(agent.get('last_heartbeat', ''))
                if now - last_heartbeat > timedelta(minutes=2):
                    agent['status'] = 'offline'
            except (ValueError, TypeError):
                agent['status'] = 'offline'

        return agents

    def get_agent_by_id(self, agent_id: str) -> Optional[Dict[str, Any]]:
        """根据 ID 获取 Agent"""
        agents = self._agents()
        return next((a for a in agents if a['id'] == agent_id), None)

    def get_agent_by_token(self, token: str) -> Optional[Dict[str, Any]]:
        """根据 token 获取 Agent（用于认证）"""
        agents = self._agents()
        return next((a for a in agents if a.get('token') == token), None)

    def update_agent(self, agent_id: str, updates: Dict[str, Any]) -> bool:
        """
        更新 Agent 信息

        Args:
            agent_id: Agent ID
            updates: 要更新的字段

        Returns:
            bool: 是否更新成功
        """
        if 'profile_id' in updates:
            self.repository.validate_profile_id(updates['profile_id'])

        def update(agents):
            for agent in agents:
                if agent['id'] != agent_id:
                    continue
                if _upgrade_busy(agent):
                    raise AgentDeploymentConflict('Agent update is in progress; wait for confirmation before editing it')
                if _dns_follow_busy(agents, agent_id) and any(
                        key in updates and updates[key] != agent.get(key)
                        for key in ('host', 'port', 'enabled', 'service_type', 'profile_id')):
                    raise AgentDeploymentConflict('DNS 联动发布正在执行，请等待结果后再修改 Agent')
                if ('profile_id' in updates and updates['profile_id'] != (agent.get('profile_id') or self.repository.default_profile_id())
                        and any(task.get('status') not in ('succeeded', 'failed', 'rolled_back')
                                for task in agent.get('deployments', {}).values())):
                    # Runs under the same repository transaction lock used by
                    # begin_deployment, so a concurrent preparation cannot race.
                    raise AgentDeploymentConflict('Agent has an unfinished deployment; finish or recover it before changing profile')
                # 更新允许的字段
                allowed_fields = ['name', 'host', 'port', 'enabled', 'service_type', 'profile_id']
                for field in allowed_fields:
                    if field in updates:
                        agent[field] = updates[field]

                agent['updated_at'] = datetime.now().isoformat()
                return True
            return False

        return self._update_agents(update, updates.get('profile_id'))

    def rebind_profile_agents(self, source_profile_id, target_profile_id, expected_agent_ids):
        """Move exactly the reviewed bindings together, under the deployment transaction lock."""
        from backend.common.config_repository import ProfileInUse, ProfileValidationError

        self.repository.validate_profile_id(source_profile_id)
        self.repository.validate_profile_id(target_profile_id)
        if source_profile_id == target_profile_id:
            raise ProfileValidationError('请选择其他配置空间')
        if (not isinstance(expected_agent_ids, list) or not expected_agent_ids
                or any(not isinstance(item, str) or not item for item in expected_agent_ids)
                or len(set(expected_agent_ids)) != len(expected_agent_ids)):
            raise ProfileValidationError('请提供已确认的 Agent ID 列表')

        def rebind(agents):
            source = self.repository._profile_metadata(source_profile_id)
            target = self.repository._profile_metadata(target_profile_id)
            default_id = self.repository.default_profile_id()
            bound = [agent for agent in agents if (agent.get('profile_id') or default_id) == source_profile_id]
            if {agent['id'] for agent in bound} != set(expected_agent_ids):
                raise ProfileInUse('Agent 绑定已变化，请刷新列表后重新确认')
            blocked = [agent.get('name') or agent['id'] for agent in bound
                       if _upgrade_busy(agent) or any(task.get('status') not in ('succeeded', 'failed', 'rolled_back')
                                                     for task in agent.get('deployments', {}).values())]
            if blocked:
                raise AgentDeploymentConflict(f'以下 Agent 正在发布、更新或等待恢复，请完成后再改绑：{"、".join(blocked)}')
            result = {'from_profile': source, 'to_profile': target,
                      'agents': [self.repository.agent_binding_summary(agent) for agent in bound]}
            for agent in bound:
                agent['profile_id'] = target_profile_id
                agent['updated_at'] = datetime.now().isoformat()
            return result

        return self._update_agents(rebind)

    def delete_agent(self, agent_id: str) -> bool:
        """删除 Agent"""
        def delete(agents):
            agent = next((item for item in agents if item['id'] == agent_id), None)
            if agent and _upgrade_busy(agent):
                raise AgentDeploymentConflict('Agent 正在更新或等待恢复')
            if _dns_follow_busy(agents, agent_id):
                raise AgentDeploymentConflict('DNS 联动发布正在执行，请等待结果后再删除 Agent')
            initial_len = len(agents)
            agents[:] = [agent for agent in agents if agent['id'] != agent_id]
            if len(agents) < initial_len:
                deleted.append(agent)
            return len(agents) < initial_len

        deleted = []
        result = self._update_agents(delete)
        if deleted:
            logger.info('已删除 Agent %s', _agent_label(deleted[0]))
            self.runtime.delete(agent_id)
        return result

    def update_heartbeat(self, agent_id: str, heartbeat_data: Dict[str, Any] = None) -> bool:
        """
        更新 Agent 心跳

        心跳状态只记在内存，不改动配置文件；只有 version 或 config_version 变化时才写配置。

        Args:
            agent_id: Agent ID
            heartbeat_data: 心跳数据，包含 version, service_status, config_version, system_metrics 等

        Returns:
            bool: 是否更新成功
        """
        heartbeat_data = heartbeat_data or {}
        mark = self.runtime.mark()
        agent = next((item for item in self.repository.get_system().get('agents', []) if item['id'] == agent_id), None)
        # 查到 Agent 之后它又被删除时，不能把心跳状态再记回去
        if agent is None or not self.runtime.record_heartbeat(agent, heartbeat_data, mark):
            return False

        # config_version 是发布基线；version 决定更新检查和版本门槛，重启后不能退回注册时的值。
        # 两者都很少变化，只在变化时写配置。
        changes = {key: heartbeat_data[key] for key in ('version', 'config_version')
                   if key in heartbeat_data and heartbeat_data[key] != agent.get(key)}
        if changes and not self._save_reported_fields({agent_id: changes}):
            return False

        if 'system_metrics' in heartbeat_data:
            # 保存监控数据到历史记录
            try:
                self.metrics_history.add_metrics(agent_id, heartbeat_data['system_metrics'])
            except Exception as e:
                logger.warning('Agent %s 的监控历史保存失败：%s', _agent_label(agent), e)
        return True

    def _preparation_file(self, agent_id, deployment_id):
        root = self.repository.data_dir / '.agent-deployment-locks'
        root.mkdir(mode=0o700, exist_ok=True)
        key = hashlib.sha256((agent_id + '/' + deployment_id).encode()).hexdigest()
        return open(root / key, 'a+b')

    def finish_deployment_preparation(self, agent_id, deployment_id):
        key = (agent_id, deployment_id)
        with self._deployment_lock:
            self._preparing_deployments.discard(key)
            handle = self._preparation_files.pop(key, None)
            if handle:
                handle.close()

    def _preparation_running(self, agent_id, deployment_id):
        if (agent_id, deployment_id) in self._preparing_deployments:
            return True
        # The preparation request and polling request may use different workers.
        # Process death releases this lock; an in-memory flag cannot prove that.
        with self._preparation_file(agent_id, deployment_id) as handle:
            try:
                fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                return True
        return False

    def begin_deployment(self, agent_id, deployment_id):
        """Persist a profile-bound task before preparing or sending its artifacts."""
        if not isinstance(deployment_id, str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,80}', deployment_id):
            raise ValueError('Invalid deployment ID')
        def begin(agents):
            agent = next((item for item in agents if item['id'] == agent_id), None)
            if agent is None:
                return {'success': False, 'message': 'Agent not found', 'http_status': 404}
            follow = agent.get('dns_follow_state') or {}
            if follow.get('status') in ('preparing', 'publishing') and follow.get('target_deployment_id') != deployment_id:
                return {'success': False, 'message': 'DNS 联动发布正在执行，请等待结果后再发布 OxiDNS', 'http_status': 409}
            if any((item.get('dns_follow') or {}).get('source_agent_id') == agent_id
                   and (item.get('dns_follow_state') or {}).get('status') in ('preparing', 'publishing') for item in agents):
                return {'success': False, 'message': '绑定的 OxiDNS 正在同步，请等待结果后再发布 Mihomo', 'http_status': 409}
            if _upgrade_busy(agent):
                return {'success': False, 'message': 'Agent update is in progress', 'http_status': 409}
            deployments = agent.setdefault('deployments', {})
            if deployment_id in deployments:
                return {**deployments[deployment_id], 'existing': True}
            active = next((item for item in deployments.values() if item.get('status') not in
                           ('succeeded', 'failed', 'rolled_back')), None)
            if active:
                return {'success': False, 'message': 'Agent already has an unfinished deployment',
                        'deployment_id': active['deployment_id'], 'status': active['status'], 'http_status': 409}
            record = {'success': True, 'deployment_id': deployment_id, 'status': 'preparing',
                      'profile_id': (agent.get('profile_id') or self.repository.default_profile_id()),
                      'service_type': agent.get('service_type', 'mihomo'), 'created_at': datetime.now().isoformat()}
            record['events'] = append_event([], 'preparing')
            handle = self._preparation_file(agent_id, deployment_id)
            try:
                fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                handle.close()
                return {'success': False, 'message': 'Deployment preparation already running', 'http_status': 409}
            self._preparation_files[(agent_id, deployment_id)] = handle
            deployments[deployment_id] = record
            agent['latest_deployment'] = record.copy()
            started.append(agent)
            # Bound history, retaining failed records as well as the active task.
            for key in list(deployments)[:-30]:
                del deployments[key]
            return record.copy()
        started = []
        with self._deployment_lock:
            try:
                result = self._update_agents(begin)
            except Exception:
                self.finish_deployment_preparation(agent_id, deployment_id)
                raise
            if result.get('success') and not result.get('existing'):
                self._preparing_deployments.add((agent_id, deployment_id))
                if started:
                    logger.info('开始向 Agent %s 发布配置（任务 %s）', _agent_label(started[0]), deployment_id[:12])
            return result

    def record_deployment(self, agent_id, deployment_id, updates, *, only_unsubmitted=False, upload_receipt=False, activation_receipt=False):
        """Only a confirmed successful activation changes the displayed version."""
        def record(agents):
            agent = next((item for item in agents if item['id'] == agent_id), None)
            if agent is None:
                return {'success': False, 'message': 'Agent not found'}
            state = agent.get('deployments', {}).get(deployment_id)
            if state is None:
                return {'success': False, 'message': 'Deployment not found', 'http_status': 404}
            if only_unsubmitted and state.get('submitted'):
                return state.copy()
            if (upload_receipt and updates.get('status') not in ('succeeded', 'failed', 'rolled_back', 'rollback_failed')
                    and state.get('status') not in ('preparing', 'uploading', 'receiving')):
                # A POST acceptance snapshot can arrive after a GET has already
                # observed validation/startup. It is not a fresh worker status.
                return state.copy()
            if (activation_receipt and updates.get('status') not in ('succeeded', 'failed', 'rolled_back', 'rollback_failed')
                    and state.get('status') not in ('ready', 'unknown')):
                return state.copy()
            if state.get('manual_rollback') and not updates.get('manual_rollback'):
                # An overlapping publication poll cannot undo a reserved rollback.
                return state.copy()
            if state.get('status') in ('succeeded', 'failed', 'rolled_back') and updates.get('status', state['status']) != state['status']:
                # An upload response or overlapping poll may arrive after a
                # newer terminal result. Never regress a confirmed transaction.
                return state.copy()
            reported_at = event_time(updates.get('updated_at')) if isinstance(updates.get('updated_at'), str) else None
            if reported_at and state.get('agent_updated_at') and reported_at < state['agent_updated_at']:
                return state.copy()
            before = state.get('status')
            if updates.get('status') and updates.get('status') != 'unknown' and not updates.get('http_status'):
                if state.get('status') == 'unknown':
                    # Transport diagnostics describe an unconfirmed outcome,
                    # not the next confirmed Agent stage or failure.
                    for field in ('error_details', 'rollback_error_details'):
                        if field not in updates:
                            state.pop(field, None)
                state.pop('http_status', None)
                if 'message' not in updates:
                    state.pop('message', None)
            for summary, details in (('error', 'error_details'), ('rollback_error', 'rollback_error_details')):
                if summary in updates and updates[summary] != state.get(summary) and details not in updates:
                    state.pop(details, None)
            # Never let an upstream response replace task ownership or identity.
            for key in ('status', 'message', 'error', 'error_details', 'failed_stage', 'rollback_error', 'rollback_error_details', 'config_version',
                        'config_revision', 'success', 'sha256', 'submitted', 'previous_running', 'health',
                        'http_status', 'manual_rollback', 'source_sha256'):
                if key in updates:
                    state[key] = (failure_text(updates[key], (agent.get('token'),))
                                  if key in ('message', 'error', 'error_details', 'rollback_error', 'rollback_error_details')
                                  else updates[key])
            # Reports describe the candidate checked before activation. Keep
            # them through ready/success/failure even when later replies omit
            # them, and leave old Agents' missing results genuinely unknown.
            state.update(dns_report_fields(updates, (agent.get('token'),)))
            if updates.get('status') in ('ready', 'succeeded'):
                for field in ('error', 'error_details', 'failed_stage', 'rollback_error', 'rollback_error_details'):
                    if field not in updates:
                        state.pop(field, None)
            if updates.get('status') == 'rolled_back' and not updates.get('rollback_error'):
                state.pop('rollback_error', None)
                state.pop('rollback_error_details', None)
            state['updated_at'] = datetime.now().isoformat()
            if reported_at:
                state['agent_updated_at'] = reported_at
            state['events'] = append_event(state.get('events'), state.get('status'),
                                           state.get('message') or state.get('error'),
                                           secrets=(agent.get('token'),))
            is_latest = (agent.get('latest_deployment') or {}).get('deployment_id') == deployment_id
            if (state.get('status') == 'succeeded' and is_latest
                    and state.get('profile_id') == (agent.get('profile_id') or self.repository.default_profile_id())
                    and state.get('service_type', agent.get('service_type')) == agent.get('service_type')):
                agent['config_version'] = state.get('config_version', agent.get('config_version', '0'))
                # Revisions are diagnostic; only server-prepared complete contents
                # may establish a baseline after confirmed activation.
                agent['pushed_at'] = datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')
                if state.get('config_revision') is not None:
                    agent['config_revision'] = state['config_revision']
                prepared = state.get('prepared_content')
                if prepared:
                    agent['applied_content'] = {**deepcopy(prepared),
                        'config_version': agent['config_version'], 'deployment_id': deployment_id}
                else:
                    agent.pop('applied_content', None)
            if state.get('manual_rollback') and state.get('status') == 'rolled_back' and is_latest:
                agent['config_version'] = state.get('config_version') or '0'
                # The restored pre-publication files may predate ConfigFlow.
                # Do not claim the current profile is installed after rollback.
                for field in ('applied_content', 'config_revision', 'pushed_at'):
                    agent.pop(field, None)
            if is_latest:
                agent['latest_deployment'] = state.copy()
            if state.get('status') != before and state.get('status') in _DEPLOYMENT_RESULTS:
                finished.append((_agent_label(agent), dict(state)))
            return state.copy()
        from backend.utils.logger import log_text
        finished = []
        result = self._update_agents(record)
        for label, state in finished[-1:]:
            level, text = _DEPLOYMENT_RESULTS[state['status']]
            detail = state.get('error') or state.get('rollback_error') or state.get('message') or ''
            logger.log(level, 'Agent %s %s（任务 %s）%s', label, text, deployment_id[:12],
                       '：' + log_text(detail, 300) if detail and state['status'] != 'succeeded' else '')
        return result

    def _deployment_request(self, agent, method, suffix, **kwargs):
        url = f"http://{agent['host']}:{agent['port']}/api/{suffix}"
        headers = {'Authorization': f'Bearer {agent["token"]}'}
        headers.update(kwargs.pop('headers', {}))
        timeout = kwargs.pop('timeout', (5, 30))
        retry_busy = method == 'post' and (suffix == 'deployments' or
                     (suffix.startswith('deployments/') and suffix.endswith(('/activate', '/rollback'))))
        deadline = time.monotonic() + 2
        for attempt in range(21):
            # Only this exact lock rejection proves no mutation was accepted.
            # Network errors and every other 409 retain their original outcome.
            response = getattr(requests, method)(url, headers=headers, timeout=timeout, **kwargs)
            try:
                result = response.json()
            except (ValueError, TypeError):
                result = {'success': False, 'message': f'Agent returned HTTP {response.status_code}'}
            if not isinstance(result, dict):
                result = {'success': False, 'message': 'Invalid Agent response'}
            if response.status_code >= 400:
                result['success'] = False
                result['http_status'] = response.status_code
            busy = response.status_code == 409 and result.get('message') == 'another service operation is in progress'
            # Rejected activation/rollback responses can return immediately
            # without record_deployment; sanitize those at the same boundary.
            for field in ('message', 'error', 'error_details', 'rollback_error', 'rollback_error_details', 'rollback_message', 'failed_stage'):
                if field in result:
                    result[field] = failure_text(result[field], (agent.get('token'),))
            report = dns_report_fields(result, (agent.get('token'),))
            for field in ('dns_checks', 'dns_checked_at'):
                result.pop(field, None)
            result.update(report)
            response.close()
            remaining = deadline - time.monotonic()
            if not retry_busy or not busy or attempt == 20 or remaining <= 0:
                return result
            time.sleep(min(.1, remaining))

    def get_deployment(self, agent_id, deployment_id):
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found', 'http_status': 404}
        state = agent.get('deployments', {}).get(deployment_id)
        if not state:
            return {'success': False, 'message': 'Deployment not found', 'http_status': 404}
        if state.get('profile_id') != (agent.get('profile_id') or self.repository.default_profile_id()):
            return {'success': False, 'message': 'Deployment belongs to a different profile', 'http_status': 409}
        if state.get('status') in ('succeeded', 'failed', 'rolled_back'):
            return state
        if state.get('service_type') != agent.get('service_type', 'mihomo'):
            return {**state, 'success': False, 'http_status': 409,
                    'message': 'Deployment belongs to a different service type'}
        if not state.get('submitted'):
            if self._preparation_running(agent_id, deployment_id):
                return state
            # The POST may finish preparation between our first read and the
            # lock check. Re-read before classifying an interrupted task.
            agent = self.get_agent_by_id(agent_id)
            state = agent.get('deployments', {}).get(deployment_id, state)
            if state.get('status') in ('succeeded', 'failed', 'rolled_back'):
                return state
            if not state.get('submitted'):
                if self._preparation_running(agent_id, deployment_id):
                    return state
                return self.record_deployment(agent_id, deployment_id, {
                    'success': False, 'status': 'failed', 'message': 'Preparation interrupted before upload; current service unchanged'},
                    only_unsubmitted=True)
        upload_in_flight = self._preparation_running(agent_id, deployment_id)
        try:
            result = self._deployment_request(agent, 'get', f'deployments/{deployment_id}')
            if state.get('manual_rollback') and result.get('status') == 'succeeded' and not result.get('manual_rollback'):
                if state.get('status') != 'unknown':
                    return {**state, 'message': 'Rollback submission awaiting Agent confirmation', 'pending': True}
                # A lost POST response is ambiguous. Reissuing the same guarded,
                # idempotent rollback is safe; a publication status alone is not.
                result = self._deployment_request(agent, 'post', f'deployments/{deployment_id}/rollback')
                busy = result.get('http_status') == 409 and result.get('message') == 'another service operation is in progress'
                if result.get('http_status') in (400, 404, 405, 409) and not busy:
                    return self._cancel_manual_rollback(agent_id, deployment_id, result)
            if result.get('http_status') == 404:
                if state.get('manual_rollback'):
                    # A missing journal cannot prove whether the rollback ran.
                    return {**state, 'message': 'Rollback journal unavailable; restore Agent records before retrying', 'pending': True}
                if upload_in_flight or self._preparation_running(agent_id, deployment_id):
                    # A concurrent GET can beat the upload to Agent journal
                    # creation. Even if the upload completed while GET was in
                    # flight, that earlier 404 cannot prove publication failed.
                    return {**state, 'message': '设备正在接收发布，等待确认', 'pending': True}
                return self.record_deployment(agent_id, deployment_id, {
                    'success': False, 'status': 'failed', 'message': 'Agent has no record of the uploaded deployment'})
            if result.get('http_status') or not isinstance(result.get('status'), str):
                return {**state, 'message': 'Deployment status temporarily unavailable', 'pending': True}
            return self.record_deployment(agent_id, deployment_id, result)
        except requests.RequestException:
            return {**state, 'message': 'Deployment status temporarily unavailable', 'pending': True}

    def activate_deployment(self, agent_id, deployment_id):
        state = self.get_deployment(agent_id, deployment_id)
        agent = self.get_agent_by_id(agent_id)
        if state.get('http_status'):
            return state
        if not agent:
            return {'success': False, 'message': 'Agent not found', 'http_status': 404}
        if (state.get('profile_id') != (agent.get('profile_id') or self.repository.default_profile_id())
                or state.get('service_type') != agent.get('service_type', 'mihomo')):
            return {**state, 'success': False, 'http_status': 409,
                    'message': 'Deployment profile or service type changed; activation refused'}
        if agent and agent.get('service_type') == 'oxidns':
            if state.get('status') == 'succeeded' and not state.get('http_status'):
                return state
            if state.get('status') == 'ready' and not state.get('http_status'):
                if state.get('pending'):
                    return {**state, 'success': False, 'http_status': 409,
                            'message': '尚未确认 Agent 候选状态，请稍后重试激活'}
                from .oxidns_validation_receipt import receipt_matches
                if not receipt_matches(agent, state):
                    # Leave the running service and remote candidate untouched.
                    # End this server task so a new checked push is possible.
                    def reject_unchecked(agents):
                        current = next(item for item in agents if item['id'] == agent_id)
                        task = current['deployments'][deployment_id]
                        # A simultaneous status poll may have observed progress.
                        # Never turn a transaction already applying into failure.
                        if task.get('status') != 'ready' or task.get('sha256') != state.get('sha256'):
                            return {**task, 'success': False, 'http_status': 409,
                                    'message': '候选状态已变化，请刷新后重试'}
                        task.update(success=False, status='failed', failed_stage='verifying', http_status=409,
                                    message='候选配置缺少当前规则的校验证明，已停止激活。请重新推送配置并完成校验。',
                                    updated_at=datetime.now().isoformat())
                        task['events'] = append_event(task.get('events'), task['status'], task['message'], secrets=(current.get('token'),))
                        if (current.get('latest_deployment') or {}).get('deployment_id') == deployment_id:
                            current['latest_deployment'] = task.copy()
                        return task.copy()
                    return self._update_agents(reject_unchecked)
        if state.get('http_status') or state.get('status') != 'ready':
            return {**state, 'success': False, 'message': 'Deployment must be ready before activation', 'http_status': 409}
        try:
            result = self._deployment_request(agent, 'post', f'deployments/{deployment_id}/activate')
            if result.get('http_status'):
                return result
            return self.record_deployment(agent_id, deployment_id, result, activation_receipt=True)
        except requests.RequestException:
            return self.record_deployment(agent_id, deployment_id, {
                'success': True, 'status': 'unknown', 'message': 'Activation sent; awaiting Agent status'}, activation_receipt=True)

    def rollback_deployment(self, agent_id, deployment_id):
        """Reserve a rollback before network I/O so a new push cannot race it."""
        def reserve(agents):
            agent = next((item for item in agents if item['id'] == agent_id), None)
            if agent is None:
                return {'success': False, 'message': 'Agent not found', 'http_status': 404}
            state = agent.get('deployments', {}).get(deployment_id)
            if not state:
                return {'success': False, 'message': 'Deployment not found', 'http_status': 404}
            if _upgrade_busy(agent):
                return {'success': False, 'message': 'Agent update is in progress', 'http_status': 409}
            if _dns_follow_busy(agents, agent_id):
                return {'success': False, 'message': 'DNS 联动发布尚未确认结束，请等待结果后再回滚', 'http_status': 409}
            if (state.get('profile_id') != (agent.get('profile_id') or self.repository.default_profile_id())
                    or state.get('service_type', agent.get('service_type')) != agent.get('service_type')
                    or (agent.get('latest_deployment') or {}).get('deployment_id') != deployment_id):
                return {'success': False, 'message': 'Only the latest deployment of this profile can be rolled back', 'http_status': 409}
            if state.get('manual_rollback'):
                return {**state, '_already_reserved': True}
            if state.get('status') != 'succeeded':
                return {'success': False, 'message': 'Only a successful deployment can be rolled back', 'http_status': 409}
            state.update(status='rolling_back', manual_rollback=True, success=True, submitted=True)
            state['events'] = append_event(state.get('events'), 'rolling_back', '正在向 Agent 提交回滚请求')
            state.pop('http_status', None)
            state.pop('rollback_rejected', None)
            state.pop('rollback_message', None)
            state.pop('rollback_error', None)
            state.pop('rollback_error_details', None)
            agent['latest_deployment'] = state.copy()
            return state.copy()

        state = self._update_agents(reserve)
        if state.get('http_status') or state.pop('_already_reserved', False):
            return state
        agent = self.get_agent_by_id(agent_id)
        try:
            result = self._deployment_request(agent, 'post', f'deployments/{deployment_id}/rollback')
            if result.get('http_status') in (400, 404, 405, 409):
                # Explicit rejection proves the Agent did not accept this action.
                self._cancel_manual_rollback(agent_id, deployment_id, result)
                return result
            if result.get('http_status'):
                return self.record_deployment(agent_id, deployment_id, {
                    'success': True, 'status': 'unknown', 'manual_rollback': True,
                    'message': 'Rollback outcome unknown; awaiting Agent status'})
            return self.record_deployment(agent_id, deployment_id, result)
        except requests.RequestException:
            return self.record_deployment(agent_id, deployment_id, {
                'success': True, 'status': 'unknown', 'manual_rollback': True,
                'message': 'Rollback outcome unknown; awaiting Agent status'})

    def _cancel_manual_rollback(self, agent_id, deployment_id, rejection):
        """A definite preflight rejection preserves the successful publication."""
        def cancel(agents):
            current = next(item for item in agents if item['id'] == agent_id)
            task = current['deployments'][deployment_id]
            if task.get('status') in ('rolling_back', 'unknown') and task.get('manual_rollback'):
                task.update(status='succeeded', success=True, rollback_rejected=True,
                            rollback_message=failure_text(rejection.get('message', 'Rollback was rejected by Agent'), (current.get('token'),)))
                task['updated_at'] = datetime.now().isoformat()
                task['events'] = append_event(task.get('events'), 'succeeded',
                                              '回滚请求被拒绝，回滚未执行，原配置继续运行', secrets=(current.get('token'),))
                details = rejection.get('rollback_error_details') or rejection.get('error_details')
                task.pop('rollback_error', None)
                if details:
                    task['rollback_error_details'] = failure_text(details, (current.get('token'),))
                else:
                    task.pop('rollback_error_details', None)
                for field in ('manual_rollback', 'http_status', 'message'):
                    task.pop(field, None)
                if (current.get('latest_deployment') or {}).get('deployment_id') == deployment_id:
                    current['latest_deployment'] = task.copy()
            return task.copy()
        return self._update_agents(cancel)

    def _remember_deployment_content(self, agent_id, bundle, base_url, validation_receipt=None, source=None):
        """Persist trusted preparation metadata, never fields from an Agent response."""
        from .deployment_bundle import deployment_content_digests, deployment_content_fingerprint
        manifest = bundle.manifest
        comparison_manifest = deepcopy(manifest)
        if bundle.comparison_config:
            comparison_manifest['files'] = [bundle.comparison_config if item['role'] == 'config' else item
                                             for item in manifest['files']]
        prepared = {'fingerprint': deployment_content_fingerprint(comparison_manifest),
                    'profile_id': manifest['profile_id'], 'service_type': manifest['service_type'],
                    'config_path': manifest['config_path'], 'base_url': base_url,
                    # Explain later drift: which files changed, and whether the draft did.
                    'files': deployment_content_digests(comparison_manifest)}
        if source is not None and manifest['service_type'] == 'oxidns':
            from .dns_follow import settings_digest
            prepared['settings_digest'] = settings_digest(source)
        def remember(agents):
            agent = next(item for item in agents if item['id'] == agent_id)
            state = agent['deployments'][manifest['deployment_id']]
            if (state['profile_id'] != prepared['profile_id']
                    or agent.get('service_type') != prepared['service_type']):
                raise ValueError('Deployment ownership changed during preparation')
            state['prepared_content'] = prepared
            if validation_receipt is not None:
                state['oxidns_validation_receipt'] = deepcopy(validation_receipt)
        self._update_agents(remember)

    def publish_deployment(self, agent_id, bundle, *, activate=True, config_revision=None, base_url='',
                           resource_config=None, source_snapshot=None):
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found', 'http_status': 404}
        deployment_id = bundle.manifest['deployment_id']
        if deployment_id not in agent.get('deployments', {}):
            result = self.begin_deployment(agent_id, deployment_id)
            if not result.get('success'):
                return result
        try:
            if (agent.get('profile_id') or self.repository.default_profile_id()) != bundle.manifest['profile_id']:
                raise ValueError('Agent profile changed during preparation')
            if agent.get('service_type', 'mihomo') != bundle.manifest['service_type']:
                raise ValueError('Agent service type changed during preparation')
            from .linux_gateway import require_gateway_bundle_agent
            require_gateway_bundle_agent(agent, bundle)
            capability = self._deployment_request(agent, 'get', 'capabilities', timeout=5)
            if 1 not in capability.get('deployment_protocols', []) or capability.get('service_type') != agent.get('service_type'):
                return self.record_deployment(agent_id, deployment_id, {
                    'success': False, 'status': 'failed', 'http_status': 409,
                    'message': 'Agent does not support transactional deployments; upgrade the Shell-installed or Docker Agent first'})
            if bundle.manifest.get('reload_only') and 1 not in capability.get('reload_only_protocols', []):
                return self.record_deployment(agent_id, deployment_id, {
                    'success': False, 'status': 'failed', 'http_status': 409,
                    'message': '请升级 Agent 到 1.8.30-go 或更新版本后使用规则自动热加载'})
            from .deployment_bundle import retarget_config_path
            bundle = retarget_config_path(bundle, capability.get('config_path', 'config.yaml'))
            previous = self.get_agent_by_id(agent_id).get('deployments', {}).get(deployment_id, {})
            if previous.get('status') in ('failed', 'rolled_back') and not previous.get('submitted'):
                return previous
            if previous.get('submitted'):
                if previous.get('source_sha256', previous.get('sha256')) != bundle.sha256:
                    return {'success': False, 'deployment_id': deployment_id, 'http_status': 409,
                            'message': 'Deployment ID already belongs to different content'}
                return self.get_deployment(agent_id, deployment_id)
            content_bundle = bundle
            if agent.get('service_type') == 'mihomo':
                from .mihomo_resources import prepare_mihomo_resources
                self.record_deployment(agent_id, deployment_id, {
                    'status': 'preparing', 'message': '正在检查并准备 Mihomo Geo 资源'})
                bundle = prepare_mihomo_resources(bundle, capability, resource_config or source_snapshot or self.repository.get_compat_config(bundle.manifest['profile_id']),
                    progress=lambda message: self.record_deployment(agent_id, deployment_id, {'status': 'preparing', 'message': message}))
            validation_receipt = None
            if agent.get('service_type') == 'oxidns':
                from .oxidns_validation_receipt import validate_bundle
                self.record_deployment(agent_id, deployment_id, {
                    'status': 'preparing', 'message': '正在校验 OxiDNS 配置与规则文件'})
                validation_receipt = validate_bundle(agent_id, bundle)
            self._remember_deployment_content(agent_id, content_bundle, base_url, validation_receipt,
                                              source_snapshot if source_snapshot is not None else resource_config)
            if source_snapshot is not None and agent.get('service_type') in ('mihomo', 'oxidns'):
                from .rule_sync_store import capture
                # Geo databases are Agent-carried resources, omitted by the
                # managed snapshot API and the profile content fingerprint.
                capture(self.repository, agent_id, content_bundle, source_snapshot, base_url,
                        comparison_config=content_bundle.comparison_config)
            # Persist before the request: a lost response can still be reconciled.
            self.record_deployment(agent_id, deployment_id, {
                'success': True, 'status': 'uploading', 'submitted': True,
                'message': f'正在上传发布包（{len(bundle.manifest["files"])} 个文件，{len(bundle.archive):,} 字节）',
                'sha256': bundle.sha256, 'source_sha256': content_bundle.sha256, 'config_version': bundle.config_version,
                'config_revision': config_revision})
            result = self._deployment_request(agent, 'post', 'deployments', data=bundle.archive, timeout=(10, 120), headers={
                'Content-Type': 'application/gzip', 'X-Deployment-ID': deployment_id,
                'X-Content-SHA256': bundle.sha256, 'X-Activate': 'true' if activate else 'false'})
            if result.get('http_status'):
                result.setdefault('status', 'failed')
            elif not isinstance(result.get('status'), str):
                result = {'success': True, 'status': 'unknown', 'message': 'Upload accepted; awaiting valid Agent status'}
            return self.record_deployment(agent_id, deployment_id, result, upload_receipt=True)
        except requests.RequestException as exc:
            agent = self.get_agent_by_id(agent_id)
            state = agent.get('deployments', {}).get(deployment_id, {})
            submitted = state.get('submitted', False)
            return self.record_deployment(agent_id, deployment_id, {
                'success': submitted, 'status': 'unknown' if submitted else 'failed',
                'message': 'Upload outcome unknown; query deployment status' if submitted else 'Unable to query Agent deployment capabilities',
                'error_details': failure_text(str(exc), (agent.get('token'),))}, upload_receipt=True)
        except ValueError as exc:
            details = failure_text(str(exc), (agent.get('token'),))
            return self.record_deployment(agent_id, deployment_id, {
                'success': False, 'status': 'failed', 'failed_stage': 'preparing',
                'message': details.splitlines()[0][:300] if details else '发布准备失败',
                'error_details': details})
        finally:
            self.finish_deployment_preparation(agent_id, deployment_id)

    def push_config_to_agent(
        self,
        agent_id: str,
        config_content: str,
        extra_data: Optional[Dict[str, Any]] = None,
        config_revision: Optional[int] = None,
        expected_profile_id: Optional[str] = None,
        expected_service_type: Optional[str] = None,
    ) -> Dict[str, Any]:
        """
        主动推送配置到 Agent

        Args:
            agent_id: Agent ID
            config_content: 配置文件内容
            extra_data: 额外数据（如 directories, ruleset_downloads）
            config_revision: 推送时配置空间的修订号，记录后用于判断 Agent 是否待更新

        Returns:
            Dict: 推送结果
        """
        import json

        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        if ((expected_profile_id is not None and
             (agent.get('profile_id') or self.repository.default_profile_id()) != expected_profile_id)
                or (expected_service_type is not None and agent.get('service_type', 'mihomo') != expected_service_type)):
            return {'success': False, 'message': '草稿已保存，但设备绑定已改变，请重新选择设备后发布。',
                    'http_status': 409}

        bundle = (extra_data or {}).get('deployment_bundle')
        if bundle is not None or agent.get('service_type', 'mihomo') in ('mihomo', 'mosdns', 'oxidns'):
            if bundle is None:
                return {'success': False, 'message': 'A complete deployment bundle is required', 'http_status': 409}
            return self.publish_deployment(agent_id, bundle, activate=(extra_data or {}).get('activate', True),
                                           config_revision=config_revision, base_url=(extra_data or {}).get('base_url', ''),
                                           resource_config=(extra_data or {}).get('resource_config'),
                                           source_snapshot=(extra_data or {}).get('source_snapshot'))

        # 构建 Agent 的 URL
        agent_url = f"http://{agent['host']}:{agent['port']}/api/config/update"

        # 计算配置 MD5
        config_md5 = hashlib.md5(config_content.encode('utf-8')).hexdigest()

        try:
            # 构建 payload
            payload_dict = {
                'config': config_content,
                'md5': config_md5
            }

            # 添加额外数据（如 directories, ruleset_downloads）
            if extra_data:
                payload_dict.update(extra_data)

            # 手动序列化JSON，确保中文不被转义
            payload = json.dumps(payload_dict, ensure_ascii=False)

            # 发送 POST 请求到 Agent
            response = requests.post(
                agent_url,
                data=payload.encode('utf-8'),
                headers={
                    'Authorization': f'Bearer {agent["token"]}',
                    'Content-Type': 'application/json; charset=utf-8'
                },
                timeout=10
            )

            if response.status_code == 200:
                try:
                    result = response.json()
                except (ValueError, TypeError):
                    result = None
                if not isinstance(result, dict):
                    return response_failure(response, fallback='Invalid Agent response', secrets=(agent.get('token'),))
                # 更新配置版本
                if result.get('success'):
                    def update_version(agents):
                        for item in agents:
                            if item['id'] == agent_id:
                                item['config_version'] = config_md5[:8]
                                item['pushed_at'] = datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')
                                if config_revision is not None:
                                    item['config_revision'] = config_revision
                                return True
                        return False

                    self._update_agents(update_version)
                    return {'success': True, 'message': 'Config pushed successfully'}
                else:
                    return response_failure(response, fallback='Config update failed', secrets=(agent.get('token'),))
            else:
                return response_failure(response, fallback=f'HTTP {response.status_code}', secrets=(agent.get('token'),))

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': 'Unable to connect to Agent',
                    'error_details': failure_text(str(e), (agent.get('token'),))}

    def restart_agent_service(self, agent_id: str) -> Dict[str, Any]:
        """
        触发 Agent 重启服务

        Args:
            agent_id: Agent ID

        Returns:
            Dict: 操作结果
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        if _upgrade_busy(agent):
            return {'success': False, 'message': 'Agent update is in progress'}

        agent_url = f"http://{agent['host']}:{agent['port']}/api/restart"

        try:
            response = requests.post(
                agent_url,
                headers={
                    'Authorization': f'Bearer {agent["token"]}'
                },
                timeout=10
            )

            if response.status_code == 200:
                try:
                    result = response.json()
                except (ValueError, TypeError):
                    result = None
                if not isinstance(result, dict):
                    return response_failure(response, fallback='Invalid Agent response', secrets=(agent.get('token'),))
                if result.get('success'):
                    return result
                return response_failure(response, fallback='Restart failed', secrets=(agent.get('token'),))
            else:
                return response_failure(response, fallback=f'HTTP {response.status_code}', secrets=(agent.get('token'),))

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': 'Unable to connect to Agent',
                    'error_details': failure_text(str(e), (agent.get('token'),))}

    def get_agent_status(self, agent_id: str) -> Dict[str, Any]:
        """
        获取 Agent 状态

        Args:
            agent_id: Agent ID

        Returns:
            Dict: 状态信息
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        agent_url = f"http://{agent['host']}:{agent['port']}/api/status"

        try:
            response = requests.get(
                agent_url,
                headers={
                    'Authorization': f'Bearer {agent["token"]}'
                },
                timeout=5
            )

            if response.status_code == 200:
                return response.json()
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

    def get_agent_log_sources(self, agent_id: str) -> Dict[str, Any]:
        """
        获取 Agent 本机解析出的日志来源

        旧版 Agent 没有该接口时返回 legacy=True，由前端退回自动检测。
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        agent_url = f"http://{agent['host']}:{agent['port']}/api/logs/sources"

        try:
            response = requests.get(
                agent_url,
                headers={
                    'Authorization': f'Bearer {agent["token"]}'
                },
                timeout=5
            )

            if response.status_code == 200:
                return response.json()
            if response.status_code == 404:
                return {'success': True, 'legacy': True, 'sources': []}
            return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

    def get_agent_logs(self, agent_id: str, lines: int = 100, log_path: str = '', source: str = '') -> Dict[str, Any]:
        """
        获取 Agent 日志

        Args:
            agent_id: Agent ID
            lines: 日志行数
            log_path: 可选的自定义日志路径
            source: 可选的日志来源 ID（来自 get_agent_log_sources）

        Returns:
            Dict: 日志内容
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        # 构建 URL,包含可选的日志来源或路径参数
        from urllib.parse import quote
        agent_url = f"http://{agent['host']}:{agent['port']}/api/logs?lines={lines}"
        if source:
            agent_url += f"&source={quote(source)}"
        elif log_path:
            agent_url += f"&log_path={quote(log_path)}"

        try:
            response = requests.get(
                agent_url,
                headers={
                    'Authorization': f'Bearer {agent["token"]}'
                },
                timeout=5
            )

            if response.status_code == 200:
                return response.json()
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

    def clear_agent_log(self, agent_id: str, log_path: str = '', source: str = '') -> Dict[str, Any]:
        """
        清空 Agent 指定日志文件

        Args:
            agent_id: Agent ID
            log_path: 日志文件路径
            source: 日志来源 ID，优先于 log_path

        Returns:
            Dict: 操作结果
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        agent_url = f"http://{agent['host']}:{agent['port']}/api/logs/clear"

        try:
            response = requests.post(
                agent_url,
                json={'source': source} if source else {'log_path': log_path},
                headers={
                    'Authorization': f'Bearer {agent["token"]}'
                },
                timeout=5
            )

            if response.status_code == 200:
                return response.json()
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

    def uninstall_agent(self, agent_id: str) -> Dict[str, Any]:
        """
        卸载远程 Agent

        Args:
            agent_id: Agent ID

        Returns:
            Dict: 卸载结果
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        if _upgrade_busy(agent):
            return {'success': False, 'message': 'Agent update is in progress'}

        agent_url = f"http://{agent['host']}:{agent['port']}/api/uninstall"

        try:
            response = requests.post(
                agent_url,
                headers={
                    'Authorization': f'Bearer {agent["token"]}'
                },
                timeout=10
            )

            if response.status_code == 200:
                result = response.json()
                return {
                    'success': True,
                    'message': 'Agent uninstall started. The agent will be removed in a few seconds.'
                }
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

    def update_agent_version(self, agent_id: str, new_version: str, binary_url: str) -> Dict[str, Any]:
        """
        触发 Agent 更新

        Args:
            agent_id: Agent ID
            new_version: 新版本号
            binary_url: 新版本二进制文件下载 URL

        Returns:
            Dict: 更新结果
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        agent_url = f"http://{agent['host']}:{agent['port']}/api/update"

        try:
            payload = {
                'version': new_version,
                'download_url': binary_url
            }

            response = requests.post(
                agent_url,
                json=payload,
                headers={
                    'Authorization': f'Bearer {agent["token"]}',
                    'Content-Type': 'application/json'
                },
                timeout=10
            )

            if response.status_code == 200:
                return {
                    'success': True,
                    'message': 'Agent update started'
                }
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

    def validate_agent_log_path(self, agent_id: str, log_path: str) -> Dict[str, Any]:
        """
        验证自定义日志路径是否有效

        Args:
            agent_id: Agent ID
            log_path: 日志文件路径

        Returns:
            Dict: 验证结果
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        agent_url = f"http://{agent['host']}:{agent['port']}/api/logs/validate"

        try:
            response = requests.post(
                agent_url,
                json={'path': log_path},
                headers={
                    'Authorization': f'Bearer {agent["token"]}',
                    'Content-Type': 'application/json'
                },
                timeout=5
            )

            if response.status_code == 200:
                return response.json()
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

    def get_logging_config(self, agent_id: str) -> Dict[str, Any]:
        """
        获取 Agent 日志配置状态

        Args:
            agent_id: Agent ID

        Returns:
            Dict: 日志配置信息
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        agent_url = f"http://{agent['host']}:{agent['port']}/api/config/logging"

        try:
            response = requests.get(
                agent_url,
                headers={
                    'Authorization': f'Bearer {agent["token"]}'
                },
                timeout=5
            )

            if response.status_code == 200:
                return response.json()
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

    def set_logging_config(self, agent_id: str, enabled: bool) -> Dict[str, Any]:
        """
        设置 Agent 日志启用/禁用

        Args:
            agent_id: Agent ID
            enabled: 是否启用日志

        Returns:
            Dict: 操作结果
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        agent_url = f"http://{agent['host']}:{agent['port']}/api/config/logging"

        try:
            response = requests.post(
                agent_url,
                json={'enabled': enabled},
                headers={
                    'Authorization': f'Bearer {agent["token"]}',
                    'Content-Type': 'application/json'
                },
                timeout=5
            )

            if response.status_code == 200:
                result = response.json()
                # 更新本地配置记录
                if result.get('success'):
                    def update_logging(agents):
                        for item in agents:
                            if item['id'] == agent_id:
                                item.setdefault('logging_config', {})['enabled'] = enabled
                                return True
                        return False

                    self._update_agents(update_logging)
                return result
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

    def set_domain_discovery(self, agent_id: str, enabled: bool) -> Optional[Dict[str, Any]]:
        """开关 Agent 的域名发现；只对 mihomo Agent 生效。返回更新后的 Agent，不存在时返回 None。"""
        def update(agents):
            for agent in agents:
                if agent['id'] != agent_id:
                    continue
                if enabled and agent.get('service_type') != 'mihomo':
                    raise ValueError('域名发现只支持 mihomo Agent')
                agent['domain_discovery_enabled'] = bool(enabled)
                agent['updated_at'] = datetime.now().isoformat()
                return dict(agent)
            return None

        result = self._update_agents(update)
        return self.runtime.overlay([result])[0] if result else None

    def probe_domains(self, agent: Dict[str, Any], payload: Dict[str, Any], timeout: int = 200) -> Dict[str, Any]:
        """让 Agent 经本机 Mihomo 探测一批域名；失败时抛出异常，消息可展示给用户。"""
        return self._agent_json_request(agent, 'post', '/api/domain-probe', timeout, '域名探测', json=payload)

    def _agent_json_request(self, agent: Dict[str, Any], method: str, path: str, timeout: int,
                            feature: str, **kwargs) -> Dict[str, Any]:
        """调用 Agent 的域名发现类接口；失败时抛出可展示给用户的异常。"""
        response = getattr(requests, method)(
            f"http://{agent['host']}:{agent['port']}{path}",
            headers={'Authorization': f'Bearer {agent["token"]}'},
            timeout=timeout,
            **kwargs,
        )
        if response.status_code == 404:
            raise RuntimeError(f'Agent {agent.get("name") or agent["id"]} 不支持{feature}，请升级')
        try:
            body = response.json()
        except ValueError:
            body = {}
        if response.status_code == 409 and path.startswith('/api/region-check'):
            raise RuntimeError('Agent 上的 Mihomo 还没有检测入口，请为该 Agent 开启域名发现后部署一次配置')
        if response.status_code != 200 or not body.get('success'):
            raise RuntimeError(body.get('message') or f'Agent 返回 HTTP {response.status_code}')
        return body

    def return_routing_status(self, agent: Dict[str, Any]) -> Dict[str, Any]:
        """读取回流规则现状及新版 Agent 的资源归属、占用安全检查。"""
        return self._agent_json_request(agent, 'get', '/api/return-routing', 15, '回流规则状态')

    def return_routing_apply(self, agent: Dict[str, Any], spec: Dict[str, Any]) -> Dict[str, Any]:
        """通过安全握手应用回流规则；占用阻止写入，不接管手工规则。"""
        return self._agent_json_request(agent, 'put', '/api/return-routing', 60, '回流规则管理', json=spec)

    def return_routing_remove(self, agent: Dict[str, Any], restore_legacy: bool) -> Dict[str, Any]:
        """仅撤销归属可验证的回流对象；新版 Agent 拒绝 legacy 恢复请求。"""
        return self._agent_json_request(agent, 'delete', '/api/return-routing', 60, '回流规则管理',
                                        params={'restore_legacy': '1'} if restore_legacy else None)

    def region_targets(self, agent: Dict[str, Any]) -> List[Dict[str, Any]]:
        """探测组可切换的目标（节点与策略组）。"""
        return self._agent_json_request(agent, 'get', '/api/region-check/targets', 15, '区域检测').get('targets') or []

    def region_check(self, agent: Dict[str, Any], payload: Dict[str, Any]) -> Dict[str, Any]:
        """经探测组的某个目标执行一组请求。"""
        return self._agent_json_request(agent, 'post', '/api/region-check', 120, '区域检测', json=payload)
