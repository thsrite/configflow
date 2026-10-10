"""Agent 管理器"""
import secrets
import hashlib
import hmac
import re
import os
import fcntl
import threading
import time
from datetime import datetime, timedelta, timezone
from typing import Dict, Any, List, Optional
import requests
from .metrics_history import MetricsHistory


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


def _upgrade_busy(agent):
    upgrade = agent.get('latest_upgrade') or {}
    return bool(upgrade) and upgrade.get('status') not in ('succeeded', 'failed', 'rolled_back')


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

    def _agents(self) -> List[Dict[str, Any]]:
        return self.repository.get_system().get('agents', [])

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
                agent_id = existing_agent['id']
                token = existing_agent['token']
                updated_agent = {
                    'id': agent_id,
                    'name': agent_name,
                    'host': agent_host,
                    'port': agent_data.get('port', 8080),
                    'token': token,
                    'service_type': agent_data.get('service_type', 'mihomo'),
                    'profile_id': requested_profile_id or existing_agent.get('profile_id', 'default'),
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
                }
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
                'profile_id': requested_profile_id or 'default',
                'deployment_method': agent_data.get('deployment_method', 'unknown'),
                'status': 'online',
                'last_heartbeat': datetime.now().isoformat(),
                'version': agent_data.get('version', '1.0.0'),
                'config_version': '0',
                'enabled': True,
                'created_at': datetime.now().isoformat(),
            }
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
                if ('profile_id' in updates and updates['profile_id'] != agent.get('profile_id', 'default')
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

    def delete_agent(self, agent_id: str) -> bool:
        """删除 Agent"""
        def delete(agents):
            agent = next((item for item in agents if item['id'] == agent_id), None)
            if agent and _upgrade_busy(agent):
                raise AgentDeploymentConflict('Agent 正在更新或等待恢复')
            initial_len = len(agents)
            agents[:] = [agent for agent in agents if agent['id'] != agent_id]
            return len(agents) < initial_len

        return self._update_agents(delete)

    def update_heartbeat(self, agent_id: str, heartbeat_data: Dict[str, Any] = None) -> bool:
        """
        更新 Agent 心跳

        Args:
            agent_id: Agent ID
            heartbeat_data: 心跳数据，包含 version, service_status, config_version, system_metrics 等

        Returns:
            bool: 是否更新成功
        """
        def update(agents):
            for agent in agents:
                if agent['id'] != agent_id:
                    continue
                agent['last_heartbeat'] = datetime.now().isoformat()
                agent['status'] = 'online'  # Agent 在线状态

                # 更新其他信息
                if heartbeat_data:
                    if 'version' in heartbeat_data:
                        agent['version'] = heartbeat_data['version']
                    if 'service_status' in heartbeat_data:
                        agent['service_status'] = heartbeat_data['service_status']  # 服务运行状态
                    if 'config_version' in heartbeat_data:
                        agent['config_version'] = heartbeat_data['config_version']

                    # 处理系统监控数据
                    if 'system_metrics' in heartbeat_data:
                        system_metrics = heartbeat_data['system_metrics']
                        agent['system_metrics'] = system_metrics

                        # 同时保存到顶层字段以保持向后兼容
                        if 'cpu' in system_metrics:
                            agent['cpu'] = system_metrics['cpu']
                        if 'memory' in system_metrics:
                            agent['memory'] = system_metrics['memory']
                        if 'disk' in system_metrics:
                            agent['disk'] = system_metrics['disk']
                        if 'network' in system_metrics:
                            agent['network'] = system_metrics['network']

                        # 保存监控数据到历史记录
                        try:
                            self.metrics_history.add_metrics(agent_id, system_metrics)
                        except Exception as e:
                            print(f"Warning: Failed to save metrics history: {e}")

                    # 兼容旧版本的直接字段
                    elif 'cpu' in heartbeat_data:
                        agent['cpu'] = heartbeat_data['cpu']
                    elif 'memory' in heartbeat_data:
                        agent['memory'] = heartbeat_data['memory']

                return True
            return False

        return self._update_agents(update)

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
                      'profile_id': agent.get('profile_id', 'default'), 'created_at': datetime.now().isoformat()}
            handle = self._preparation_file(agent_id, deployment_id)
            try:
                fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                handle.close()
                return {'success': False, 'message': 'Deployment preparation already running', 'http_status': 409}
            self._preparation_files[(agent_id, deployment_id)] = handle
            deployments[deployment_id] = record
            agent['latest_deployment'] = record.copy()
            # Bound history, retaining failed records as well as the active task.
            for key in list(deployments)[:-30]:
                del deployments[key]
            return record.copy()
        with self._deployment_lock:
            try:
                result = self._update_agents(begin)
            except Exception:
                self.finish_deployment_preparation(agent_id, deployment_id)
                raise
            if result.get('success') and not result.get('existing'):
                self._preparing_deployments.add((agent_id, deployment_id))
            return result

    def record_deployment(self, agent_id, deployment_id, updates):
        """Only a confirmed successful activation changes the displayed version."""
        def record(agents):
            agent = next((item for item in agents if item['id'] == agent_id), None)
            if agent is None:
                return {'success': False, 'message': 'Agent not found'}
            state = agent.get('deployments', {}).get(deployment_id)
            if state is None:
                return {'success': False, 'message': 'Deployment not found', 'http_status': 404}
            if state.get('status') in ('succeeded', 'failed', 'rolled_back') and updates.get('status', state['status']) != state['status']:
                # An upload response or overlapping poll may arrive after a
                # newer terminal result. Never regress a confirmed transaction.
                return state.copy()
            if updates.get('status') and updates.get('status') != 'unknown' and not updates.get('http_status'):
                state.pop('http_status', None)
                if 'message' not in updates:
                    state.pop('message', None)
            # Never let an upstream response replace task ownership or identity.
            for key in ('status', 'message', 'error', 'failed_stage', 'rollback_error', 'config_version',
                        'config_revision', 'success', 'sha256', 'submitted', 'previous_running', 'health',
                        'http_status'):
                if key in updates:
                    state[key] = updates[key]
            state['updated_at'] = datetime.now().isoformat()
            if state.get('status') == 'succeeded':
                agent['config_version'] = state.get('config_version', agent.get('config_version', '0'))
                # 配置空间修订号：界面据此判断 Agent 上的配置是否落后于当前配置
                agent['pushed_at'] = datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')
                if state.get('config_revision') is not None:
                    agent['config_revision'] = state['config_revision']
            if (agent.get('latest_deployment') or {}).get('deployment_id') == deployment_id:
                agent['latest_deployment'] = state.copy()
            return state.copy()
        return self._update_agents(record)

    def _deployment_request(self, agent, method, suffix, **kwargs):
        url = f"http://{agent['host']}:{agent['port']}/api/{suffix}"
        headers = {'Authorization': f'Bearer {agent["token"]}'}
        headers.update(kwargs.pop('headers', {}))
        timeout = kwargs.pop('timeout', (5, 30))
        retry_busy = method == 'post' and (suffix == 'deployments' or
                     (suffix.startswith('deployments/') and suffix.endswith('/activate')))
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
        if state.get('profile_id') != agent.get('profile_id', 'default'):
            return {'success': False, 'message': 'Deployment belongs to a different profile', 'http_status': 409}
        if state.get('status') in ('succeeded', 'failed', 'rolled_back'):
            return state
        if not state.get('submitted'):
            if self._preparation_running(agent_id, deployment_id):
                return state
            return self.record_deployment(agent_id, deployment_id, {
                'success': False, 'status': 'failed', 'message': 'Preparation interrupted before upload; current service unchanged'})
        try:
            result = self._deployment_request(agent, 'get', f'deployments/{deployment_id}')
            if result.get('http_status') == 404:
                return self.record_deployment(agent_id, deployment_id, {
                    'success': False, 'status': 'failed', 'message': 'Agent has no record of the uploaded deployment'})
            if result.get('http_status') or not isinstance(result.get('status'), str):
                return {**state, 'message': 'Deployment status temporarily unavailable', 'pending': True}
            return self.record_deployment(agent_id, deployment_id, result)
        except requests.RequestException:
            return {**state, 'message': 'Deployment status temporarily unavailable', 'pending': True}

    def activate_deployment(self, agent_id, deployment_id):
        state = self.get_deployment(agent_id, deployment_id)
        if state.get('http_status') or state.get('status') != 'ready':
            return {**state, 'success': False, 'message': 'Deployment must be ready before activation', 'http_status': 409}
        agent = self.get_agent_by_id(agent_id)
        try:
            result = self._deployment_request(agent, 'post', f'deployments/{deployment_id}/activate')
            if result.get('http_status'):
                return result
            return self.record_deployment(agent_id, deployment_id, result)
        except requests.RequestException:
            return self.record_deployment(agent_id, deployment_id, {
                'success': True, 'status': 'unknown', 'message': 'Activation sent; awaiting Agent status'})

    def publish_deployment(self, agent_id, bundle, *, activate=True, config_revision=None):
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found', 'http_status': 404}
        deployment_id = bundle.manifest['deployment_id']
        if deployment_id not in agent.get('deployments', {}):
            result = self.begin_deployment(agent_id, deployment_id)
            if not result.get('success'):
                return result
        try:
            if agent.get('profile_id', 'default') != bundle.manifest['profile_id']:
                raise ValueError('Agent profile changed during preparation')
            capability = self._deployment_request(agent, 'get', 'capabilities', timeout=5)
            if 1 not in capability.get('deployment_protocols', []) or capability.get('service_type') != agent.get('service_type'):
                return self.record_deployment(agent_id, deployment_id, {
                    'success': False, 'status': 'failed', 'http_status': 409,
                    'message': 'Agent does not support transactional deployments; upgrade the Shell-installed or Docker Agent first'})
            from .deployment_bundle import retarget_config_path
            bundle = retarget_config_path(bundle, capability.get('config_path', 'config.yaml'))
            previous = self.get_agent_by_id(agent_id).get('deployments', {}).get(deployment_id, {})
            if previous.get('status') in ('failed', 'rolled_back') and not previous.get('submitted'):
                return previous
            if previous.get('submitted'):
                if previous.get('sha256') != bundle.sha256:
                    return {'success': False, 'deployment_id': deployment_id, 'http_status': 409,
                            'message': 'Deployment ID already belongs to different content'}
                return self.get_deployment(agent_id, deployment_id)
            # Persist before the request: a lost response can still be reconciled.
            self.record_deployment(agent_id, deployment_id, {
                'success': True, 'status': 'uploading', 'submitted': True,
                'sha256': bundle.sha256, 'config_version': bundle.config_version,
                'config_revision': config_revision})
            result = self._deployment_request(agent, 'post', 'deployments', data=bundle.archive, timeout=(10, 120), headers={
                'Content-Type': 'application/gzip', 'X-Deployment-ID': deployment_id,
                'X-Content-SHA256': bundle.sha256, 'X-Activate': 'true' if activate else 'false'})
            if result.get('http_status'):
                result.setdefault('status', 'failed')
            elif not isinstance(result.get('status'), str):
                result = {'success': True, 'status': 'unknown', 'message': 'Upload accepted; awaiting valid Agent status'}
            return self.record_deployment(agent_id, deployment_id, result)
        except requests.RequestException:
            agent = self.get_agent_by_id(agent_id)
            state = agent.get('deployments', {}).get(deployment_id, {})
            submitted = state.get('submitted', False)
            return self.record_deployment(agent_id, deployment_id, {
                'success': submitted, 'status': 'unknown' if submitted else 'failed',
                'message': 'Upload outcome unknown; query deployment status' if submitted else 'Unable to query Agent deployment capabilities'})
        except ValueError as exc:
            return self.record_deployment(agent_id, deployment_id, {'success': False, 'status': 'failed', 'message': str(exc)})
        finally:
            self.finish_deployment_preparation(agent_id, deployment_id)

    def push_config_to_agent(
        self,
        agent_id: str,
        config_content: str,
        extra_data: Optional[Dict[str, Any]] = None,
        config_revision: Optional[int] = None,
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

        if agent.get('service_type', 'mihomo') in ('mihomo', 'mosdns'):
            bundle = (extra_data or {}).get('deployment_bundle')
            if bundle is None:
                return {'success': False, 'message': 'A complete deployment bundle is required', 'http_status': 409}
            return self.publish_deployment(agent_id, bundle, activate=(extra_data or {}).get('activate', True),
                                           config_revision=config_revision)

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
                result = response.json()
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
                    return {'success': False, 'message': result.get('message', 'Unknown error')}
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

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
                return response.json()
            else:
                return {'success': False, 'message': f'HTTP {response.status_code}'}

        except requests.exceptions.RequestException as e:
            return {'success': False, 'message': f'Connection error: {str(e)}'}

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

    def get_agent_logs(self, agent_id: str, lines: int = 100, log_path: str = '') -> Dict[str, Any]:
        """
        获取 Agent 日志

        Args:
            agent_id: Agent ID
            lines: 日志行数
            log_path: 可选的自定义日志路径

        Returns:
            Dict: 日志内容
        """
        agent = self.get_agent_by_id(agent_id)
        if not agent:
            return {'success': False, 'message': 'Agent not found'}

        # 构建 URL,包含可选的日志路径参数
        agent_url = f"http://{agent['host']}:{agent['port']}/api/logs?lines={lines}"
        if log_path:
            # URL 编码日志路径
            from urllib.parse import quote
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

    def clear_agent_log(self, agent_id: str, log_path: str) -> Dict[str, Any]:
        """
        清空 Agent 指定日志文件

        Args:
            agent_id: Agent ID
            log_path: 日志文件路径

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
                json={'log_path': log_path},
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

        return self._update_agents(update)

    def probe_domains(self, agent: Dict[str, Any], payload: Dict[str, Any], timeout: int = 200) -> Dict[str, Any]:
        """让 Agent 经本机 Mihomo 探测一批域名；失败时抛出异常，消息可展示给用户。"""
        response = requests.post(
            f"http://{agent['host']}:{agent['port']}/api/domain-probe",
            json=payload,
            headers={'Authorization': f'Bearer {agent["token"]}'},
            timeout=timeout,
        )
        if response.status_code == 404:
            raise RuntimeError(f'Agent {agent.get("name") or agent["id"]} 不支持域名探测，请升级')
        try:
            body = response.json()
        except ValueError:
            body = {}
        if response.status_code != 200 or not body.get('success'):
            raise RuntimeError(body.get('message') or f'Agent 返回 HTTP {response.status_code}')
        return body
