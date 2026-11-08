"""Durable onboarding sessions for the opt-in native Linux gateway installer.

An installation capability can enroll exactly one pair of Agents. Publishing
requires the administrator, exact bound identities and the saved profile revision.
It never applies host forwarding: that remains an explicit preview/apply action.
"""
from contextlib import contextmanager
from copy import deepcopy
from datetime import datetime, timezone
import fcntl
import ipaddress
import json
import logging
import os
from pathlib import Path
import re
import secrets
import shlex
import tempfile
import threading
import time
from urllib.parse import urlsplit, urlencode
import uuid

from backend.agents.linux_gateway import _spec
from backend.agents.oxidns_monitor import MonitorError
from backend.agents.manager import _constant_time_ascii_equal, _upgrade_busy, _dns_follow_busy, is_usable, online_status

logger = logging.getLogger(__name__)
SERVICES = ('mihomo', 'oxidns')
SESSION_TTL = 24 * 60 * 60
PUBLISH_TIMEOUT = 300


class InstallationError(ValueError):
    def __init__(self, message, status=400, code=None):
        super().__init__(message)
        self.status = status
        self.code = code


def _iso(timestamp=None):
    return datetime.fromtimestamp(timestamp or time.time(), timezone.utc).isoformat()


def _root(repository):
    root = Path(repository.data_dir) / 'gateway-installations'
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    return root


def _path(repository, session_id):
    if not isinstance(session_id, str) or not re.fullmatch(r'[a-f0-9]{32}', session_id):
        raise InstallationError('安装会话不存在。', 404)
    return _root(repository) / (session_id + '.json')


def _read(repository, session_id):
    try:
        value = json.loads(_path(repository, session_id).read_text())
    except FileNotFoundError:
        raise InstallationError('安装会话不存在，请重新创建安装。', 404) from None
    if not isinstance(value, dict) or value.get('id') != session_id:
        raise InstallationError('安装记录无法读取，请保留现场并检查服务端数据。', 409)
    return value


def _write(repository, session):
    path = _path(repository, session['id'])
    fd, tmp = tempfile.mkstemp(prefix='.gateway-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as output:
            os.fchmod(output.fileno(), 0o600)
            json.dump(session, output, ensure_ascii=False)
            output.flush()
            os.fsync(output.fileno())
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


@contextmanager
def _locked(repository, session_id):
    path = _path(repository, session_id)
    with open(path.with_suffix('.lock'), 'a+b') as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock.fileno(), fcntl.LOCK_EX)
        try:
            yield
        finally:
            fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def _authorized(session, token):
    if (not token or not _constant_time_ascii_equal(token, session['token'])
            or time.time() >= session['expires']):
        raise InstallationError('安装凭据无效或已过期，请在向导创建新的安装命令。', 401)


def _server_url(value):
    if not isinstance(value, str) or not value or len(value) > 2048 or any(ord(c) < 33 for c in value):
        raise InstallationError('请填写 Linux 能访问的 ConfigFlow HTTP(S) 服务地址。')
    try:
        parsed = urlsplit(value)
        port = parsed.port
    except ValueError:
        raise InstallationError('ConfigFlow 服务地址无效。') from None
    if parsed.scheme not in ('http', 'https') or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        raise InstallationError('服务地址必须是无账号、查询参数或片段的 HTTP(S) 地址。')
    if parsed.hostname.lower() in ('localhost', '0.0.0.0', '::', '::1') or parsed.hostname.startswith('127.'):
        raise InstallationError('Linux 安装不能使用 localhost / 回环服务地址，请填写 ConfigFlow 的局域网地址或域名。')
    return value.rstrip('/')


def _takeover(config):
    from backend.common.router_split import is_full_tun
    return 'full_tun' if is_full_tun(config) else 'selective'


def _saved_snapshot(repository, session):
    config, revision = repository.get_publish_snapshot(session['profile_id'])
    if revision != session['config_revision']:
        raise InstallationError('配置已在其他页面修改，请重新准备安装并预览保存；已安装的设备会继续沿用。', 409)
    if session.get('takeover') and session['takeover'] != _takeover(config):
        raise InstallationError('接管方式已变化，请重新准备安装；已安装的设备会继续沿用。', 409)
    from backend.common.gateway_onboarding import assert_gateway_onboarding_ready
    address = session['network'].get('address')
    assert_gateway_onboarding_ready(config, base_url=session['server_url'],
                                    local_addresses=(address,) if address else ())
    configured_url = config.get('system_config', {}).get('server_domain', '').strip().rstrip('/')
    if configured_url and configured_url != session['server_url']:
        raise InstallationError(f'系统设置中的服务地址是 {configured_url}，与这里填写的 {session["server_url"]} 不一致。'
                                '核心会使用系统服务地址持续下载规则，两者需相同。可将本次地址设为系统服务地址后继续，'
                                '或在系统设置中修改。', 409, code='server_url_mismatch')
    return config


def _check_mihomo_ports(repository, profile_id, ports):
    """Both Agents run beside Mihomo, which keeps the ports of its base YAML."""
    import yaml
    from backend.converters.mihomo import _default_mihomo_template, mihomo_listen_ports
    text = (repository.get_profile(profile_id).get('mihomo') or {}).get('custom_config') or ''
    try:
        document = yaml.safe_load(text) if isinstance(text, str) and text.strip() else _default_mihomo_template()
    except (yaml.YAMLError, RecursionError):
        return  # Reported by the onboarding check with its own message.
    owners = mihomo_listen_ports(document) if isinstance(document, dict) else {}
    for service, port in ports.items():
        if port in owners:
            raise InstallationError(f'{service} Agent 端口 {port} 与 Mihomo 基础配置的 {owners[port]} 冲突，'
                                    '请换一个 Agent 端口，或先修改 Mihomo 基础配置。')


def create(repository, manager, body):
    allowed = {'profile_id', 'name', 'server_url', 'network', 'config_revision', 'mihomo_agent_port', 'oxidns_agent_port', 'auto_network', 'agent_ip',
               'use_as_server_domain'}
    if not isinstance(body, dict) or set(body) - allowed:
        raise InstallationError('安装参数无效。')
    profile_id = body.get('profile_id')
    repository.validate_profile_id(profile_id)
    if type(body.get('config_revision')) is not int or body['config_revision'] < 0:
        raise InstallationError('请先预览并保存网关配置。')
    name = body.get('name', '')
    if not isinstance(name, str) or not re.fullmatch(r'[A-Za-z0-9_-]{1,48}', name):
        raise InstallationError('设备名称使用 1–48 个字母、数字、短横线或下划线。')
    auto_network = body.get('auto_network', False)
    if type(auto_network) is not bool:
        raise InstallationError('auto_network 必须为布尔值。')
    if auto_network:
        if 'network' in body:
            raise InstallationError('自动识别网络时只需填写可选的 Agent IP。')
        requested_ip = body.get('agent_ip', '')
        if not isinstance(requested_ip, str):
            raise InstallationError('Agent IP 必须是 IPv4 地址或留空。')
        requested_ip = _host_ipv4(requested_ip.strip()) if requested_ip.strip() else ''
        network = {'address': requested_ip, 'interface': '', 'lan_cidr': '', 'upstream_gateway': ''}
    else:
        if 'agent_ip' in body:
            raise InstallationError('手动网络模式请使用完整网络参数。')
        try:
            network = _spec(body.get('network'))
        except MonitorError as exc:
            raise InstallationError(exc.message) from None
    ports = {service: body.get(service + '_agent_port', default) for service, default in zip(SERVICES, (8080, 8082))}
    if any(type(port) is not int or not 1024 <= port <= 65535 or port == 1053 for port in ports.values()) or len(set(ports.values())) != 2:
        raise InstallationError('两个 Agent 需要不同的 1024–65535 端口，不能占用 Mihomo DNS 1053。')
    _check_mihomo_ports(repository, profile_id, ports)
    now = time.time()
    session = {'id': uuid.uuid4().hex, 'profile_id': profile_id, 'config_revision': body['config_revision'],
               'name': name, 'server_url': _server_url(body.get('server_url')), 'network': network, 'ports': ports,
               'token': secrets.token_urlsafe(32), 'created_at': _iso(now), 'expires': now + SESSION_TTL,
               'phase': 'waiting', 'agent_ids': {}, 'registered': {}, 'deployments': {},
               'message': '安装命令已准备，请在目标 Linux 执行后回到这里。'}
    if auto_network:
        session.update(auto_network=True, requested_ip=requested_ip)
    if type(body.get('use_as_server_domain', False)) is not bool:
        raise InstallationError('use_as_server_domain 必须为布尔值。')
    if body.get('use_as_server_domain'):
        # Explicitly confirmed by the administrator: generated links and rule
        # downloads of every profile use this address from now on.
        repository.update_system_transaction(
            lambda system: system['system_config'].update(server_domain=session['server_url']))
        logger.info('已将系统服务地址设为 %s（网关安装）', session['server_url'])
    # The installer itself differs by takeover: full TUN persists forwarding.
    session['takeover'] = _takeover(_saved_snapshot(repository, session))
    # Resolve all immutable, checksum-verified downloads before handing a command
    # to the user. Generating this script has no host-side effects.
    # A lost HTTP response must not create a second capability for the same
    # installation. Serialize creation across application worker processes.
    with open(_root(repository) / '.create.lock', 'a+b') as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX)
        current = latest(repository, manager, profile_id)['session']
        if current and not current['expired'] and not current['target_changed']:
            previous = _read(repository, current['id'])
            keys = ('profile_id', 'config_revision', 'name', 'server_url', 'ports', 'takeover')
            # Sessions from before the takeover field always used the gateway.
            previous.setdefault('takeover', 'selective')
            # After enrollment the wizard shows the detected address in the
            # optional IP field, so re-preparing then names the same host.
            same_target = (bool(previous.get('auto_network')) == auto_network and
                (requested_ip in (previous.get('requested_ip'), previous['network'].get('address'))
                 if auto_network else previous.get('network') == network))
            if same_target and all(previous.get(key) == session[key] for key in keys):
                if _ensure_short_download(previous):
                    _write(repository, previous)
                return public(repository, manager, previous)
            if same_target and all(previous.get(key) == session[key] for key in keys if key != 'config_revision'):
                return _reuse_for_revision(repository, manager, previous['id'], session['config_revision'])
        session['script'] = _script(session)
        _ensure_short_download(session)
        _write(repository, session)
    logger.info('已创建网关安装 %s（目标 %s）', session['name'], session['network']['address'])
    return public(repository, manager, session)


def _reuse_for_revision(repository, manager, session_id, revision):
    """Bind a newly reviewed configuration to the same installation target.

    The install script and enrollment never depend on the configuration, so
    the installed devices stay; only publishing starts over with this revision.
    """
    with _locked(repository, session_id):
        session = _read(repository, session_id)
        if session['phase'] == 'publishing':
            raise InstallationError('发布进行中，请等待完成后再更新配置。', 409)
        enrolled = set(session['agent_ids']) == set(SERVICES)
        session.update(config_revision=revision, deployments={}, phase='enrolled' if enrolled else 'waiting',
                       message='配置已更新，继续沿用已安装的设备，请重新发布。' if enrolled
                       else '配置已更新，安装命令不变，请在目标 Linux 执行后回到这里。')
        _ensure_short_download(session)
        _write(repository, session)
    logger.info('网关安装 %s：沿用已安装的设备，配置更新为版本 %s', session['name'], revision)
    return public(repository, manager, session)


def _script(session):
    from backend.agents.gateway_install_script import generate_gateway_install_script
    return generate_gateway_install_script(
        server_url=session['server_url'], agent_name=session['name'], agent_ip=session['network']['address'],
        binary_download_url=session['server_url'] + '/api/agents/download',
        mihomo_agent_port=session['ports']['mihomo'], oxidns_agent_port=session['ports']['oxidns'],
        enrollment_url=session['server_url'] + '/api/agents/gateway-installations/' + session['id'] + '/enroll',
        enrollment_token=session['token'], installation_id=session['id'],
        full_tun=session.get('takeover') == 'full_tun',
    )


def _ensure_short_download(session):
    """Reuse the ordinary AIO install-link store and its bounded lifetime."""
    if session.get('download_path'):
        return False
    from backend.agents.install_command_store import save_install_script, InstallCommandStoreFull
    from backend.agents.install_output import with_root_privileges
    try:
        token, _ = save_install_script(with_root_privileges(session['script']), expires_at=int(session['expires']))
    except InstallCommandStoreFull:
        raise InstallationError('可用安装链接已达上限，请使用已生成的命令或等待旧链接过期。', 503) from None
    session['download_path'] = '/api/agents/i/' + token
    return True


def script(repository, session_id, token):
    session = _read(repository, session_id)
    _authorized(session, token)
    from backend.agents.install_output import with_root_privileges
    return with_root_privileges(session['script'])


def _match_agent(session, agent, service, *, require_profile=True):
    if (not agent or agent.get('service_type') != service or agent.get('deployment_method') != 'shell'
            or agent.get('host') != session['network']['address'] or agent.get('port') != session['ports'][service]
            or agent.get('name') != session['name'] + '-' + service
            or (require_profile and agent.get('profile_id') != session['profile_id'])):
        raise InstallationError('设备身份、地址、服务类型或配置空间已改变，请检查本次安装目标。', 409)


def _host_ipv4(value):
    try:
        address = ipaddress.IPv4Address(value)
        if address.is_loopback or address.is_multicast or address.is_unspecified or address.is_link_local or str(address) == '255.255.255.255':
            raise ValueError
    except (ValueError, TypeError):
        raise InstallationError('安装主机需要可访问的 LAN IPv4 地址。') from None
    return str(address)


def _registration_target(session, data, existing_agents=()):
    if session.get('auto_network') and not session['network']['address']:
        owned_hosts = {agent.get('host') for agent in existing_agents
                       if agent.get('gateway_installation_id') == session['id']}
        if len(owned_hosts) > 1:
            raise InstallationError('本次安装的两个 Agent 地址不一致，请检查安装主机。', 409)
        host = _host_ipv4(data.get('host'))
        if owned_hosts and host not in owned_hosts:
            raise InstallationError('本次安装已在另一台主机登记，不能更换地址。', 409)
        session['network']['address'] = host


def registration_profile(repository, session_id, token, data, fallback_host=None):
    session = _read(repository, session_id)
    _authorized(session, token)
    service = data.get('service_type')
    if service not in SERVICES:
        raise InstallationError('安装凭据只能注册本次指定的 Mihomo / OxiDNS。', 403)
    if session.get('auto_network') and not data.get('host'):
        data['host'] = _host_ipv4(fallback_host)
    _registration_target(session, data)
    _match_agent(session, data, service, require_profile=False)
    return session['profile_id']


def register(repository, manager, session_id, token, data, existing_token=None):
    """Replay only credentials created by this exact installation capability.

    The ownership marker is committed atomically with the Agent. It also covers
    a process crash before writing the session's registration receipt. A prior
    independently installed Agent still requires its existing bearer token.
    """
    with _locked(repository, session_id):
        session = _read(repository, session_id)
        registration_profile(repository, session_id, token, data)
        # Pin the first reported address under the installation lock. Retain
        # the ownership marker as proof even after a lost registration receipt.
        _registration_target(session, data, manager._agents())
        service = data['service_type']
        existing = next((agent for agent in manager._agents()
                         if agent.get('name') == data['name'] and agent.get('host') == data['host']), None)
        owned = existing and existing.get('gateway_installation_id') == session_id
        if owned:
            _match_agent(session, existing, service)
            if session['agent_ids'].get(service, existing['id']) != existing['id']:
                raise InstallationError('本次安装已关联其他设备，不能替换。', 409)
            existing_token = existing['token']
        elif session['agent_ids'].get(service) and (not existing or existing['id'] != session['agent_ids'][service]):
            raise InstallationError('本次安装关联的设备已改变，请创建新的安装。', 409)
        result = manager.register_agent({**data, 'profile_id': session['profile_id']},
            existing_token=existing_token, installation_id=session_id)
        if owned:
            result['token'] = existing['token']
        session['registered'][service] = result['id']
        _write(repository, session)
        return result


def enroll(repository, manager, session_id, token, body):
    if not isinstance(body, dict) or set(body) != {'agents'} or not isinstance(body['agents'], dict) or set(body['agents']) != set(SERVICES):
        raise InstallationError('请提交本次安装的两个 Agent 身份。')
    with _locked(repository, session_id):
        session = _read(repository, session_id)
        _authorized(session, token)
        if not session['network']['address']:
            raise InstallationError('请先等待两个 Agent 通过本次安装命令登记。', 409)
        def bind(agents):
            found = {}
            # Verify both proofs and all busy guards under one repository lock;
            # invalid second credentials cannot partially rebind the first core.
            for service in SERVICES:
                proof = body['agents'][service]
                if not isinstance(proof, dict) or set(proof) != {'id', 'token'} or not all(isinstance(v, str) for v in proof.values()):
                    raise InstallationError('Agent 身份凭据无效。', 401)
                agent = next((item for item in agents if item['id'] == proof['id']), None)
                if not agent or not _constant_time_ascii_equal(proof['token'], agent.get('token')):
                    raise InstallationError('无法验证本次安装的 Agent 身份。', 401)
                if agent.get('service_type') != service or agent.get('deployment_method') != 'shell':
                    raise InstallationError('本次安装只能关联对应的 Shell 核心 Agent。', 409)
                if session['agent_ids'].get(service, agent['id']) != agent['id']:
                    raise InstallationError('本次安装已关联其他设备，不能替换。', 409)
                if session['registered'].get(service, agent['id']) != agent['id']:
                    raise InstallationError('Agent 与本次安装的登记记录不一致。', 409)
                # Re-running the AIO installer retains proven Agent IDs/tokens
                # but changes local name/address/port. Bind those exact IDs to
                # the explicitly supplied install target, never infer by name.
                expected = {'name': session['name'] + '-' + service,
                            'host': session['network']['address'], 'port': session['ports'][service],
                            'profile_id': session['profile_id']}
                changed = any(agent.get(key) != value for key, value in expected.items())
                if session['agent_ids'].get(service) and changed:
                    raise InstallationError('已关联设备的信息发生变化，请重新准备安装。', 409)
                if any(other['id'] != agent['id'] and other.get('name') == expected['name']
                       and other.get('host') == expected['host'] for other in agents):
                    raise InstallationError('目标名称和地址已被另一台 Agent 使用，请更换设备名称。', 409)
                if changed and (_upgrade_busy(agent) or _dns_follow_busy(agents, agent['id']) or any(
                        task.get('status') not in ('succeeded', 'failed', 'rolled_back')
                        for task in agent.get('deployments', {}).values())):
                    raise InstallationError('设备仍有未完成的发布或升级，请先完成或恢复后再关联。', 409)
                found[service] = (agent, expected)
            for agent, expected in found.values():
                agent.update(expected)
            return {service: agent['id'] for service, (agent, _) in found.items()}
        found = manager._update_agents(bind, session['profile_id'])
        session['agent_ids'] = found
        if session['phase'] == 'waiting':
            session.update(phase='enrolled', message='两个核心和 Agent 已登记。继续按顺序发布配置。')
            logger.info('网关安装 %s：Mihomo 和 OxiDNS Agent 已登记', session['name'])
        _write(repository, session)
    return {'success': True, 'message': '安装登记完成，请回到 ConfigFlow 向导继续发布。'}


def public(repository, manager, session):
    result = {key: deepcopy(session[key]) for key in ('id', 'profile_id', 'config_revision', 'name', 'network', 'phase', 'created_at', 'deployments', 'message')}
    result['success'] = True
    result['remedy'] = deepcopy(session.get('remedy'))
    result['server_url'] = session['server_url']
    result['auto_network'] = session.get('auto_network', False)
    result['takeover'] = session.get('takeover', 'selective')
    result['expires_at'] = _iso(session['expires'])
    result['expired'] = time.time() >= session['expires']
    result['agents'] = {}
    result['config_changed'] = repository.config_revision(session['profile_id']) != session['config_revision']
    result['target_changed'] = False
    for service, agent_id in session['agent_ids'].items():
        agent = manager.get_agent_by_id(agent_id)
        if agent:
            result['agents'][service] = {key: agent.get(key) for key in ('id', 'name', 'host', 'port', 'profile_id', 'service_type', 'enabled', 'version')}
            # Same rule as publishing (_pair) and the setup check that reads this.
            result['agents'][service]['status'] = online_status(agent)
        try:
            _match_agent(session, agent, service)
            if session['phase'] == 'published':
                expected = session['deployments'].get(service, {}).get('deployment_id')
                task = agent.get('latest_deployment') or {}
                if task.get('deployment_id') != expected or task.get('status') != 'succeeded':
                    raise InstallationError('设备配置在安装完成后已更改。', 409)
        except InstallationError:
            result['target_changed'] = True
    if not result['expired']:
        url = session['server_url'] + '/api/agents/gateway-installations/' + session['id'] + '/script?' + urlencode({'token': session['token']})
        result['download_url'] = session['server_url'] + session['download_path'] if session.get('download_path') else url
        # Download to a private temp file; a failed transfer never executes a
        # partial script. sudo is conditional so Alpine/root works as well, and
        # BusyBox wget is used where curl is absent (native Alpine). The wizard
        # builds its curl/wget choice from download_url; this is the fallback.
        result['command'] = "sh -c " + shlex.quote('set -eu; f=$(mktemp); trap \'rm -f "$f"\' EXIT; '
            + 'if command -v curl >/dev/null 2>&1; then curl -fSL ' + shlex.quote(url) + ' -o "$f"; '
            + 'else wget -O "$f" ' + shlex.quote(url) + '; fi; if [ "$(id -u)" -eq 0 ]; then sh "$f"; else sudo sh "$f"; fi')
    return result


def get(repository, manager, session_id):
    return public(repository, manager, _read(repository, session_id))


def has_installation(repository, profile_id):
    """Whether any installation was created for this profile."""
    for path in _root(repository).glob('*.json'):
        try:
            if json.loads(path.read_text()).get('profile_id') == profile_id:
                return True
        except (OSError, ValueError):
            continue
    return False


def latest(repository, manager, profile_id):
    repository.validate_profile_id(profile_id)
    sessions = []
    for path in _root(repository).glob('*.json'):
        try:
            session = json.loads(path.read_text())
            if session.get('profile_id') == profile_id:
                sessions.append(session)
        except (OSError, ValueError):
            continue
    sessions.sort(key=lambda item: item.get('created_at', ''), reverse=True)
    return {'success': True, 'session': public(repository, manager, sessions[0]) if sessions else None}


def _pair(repository, manager, session):
    if repository.config_revision(session['profile_id']) != session['config_revision']:
        raise InstallationError('配置已修改：请重新准备安装，预览并保存后会继续沿用已安装的设备，无需重新运行安装命令。', 409)
    if set(session['agent_ids']) != set(SERVICES):
        raise InstallationError('请先完成两个核心和 Agent 的安装登记。', 409)
    agents = {}
    for service, agent_id in session['agent_ids'].items():
        agent = manager.get_agent_by_id(agent_id)
        _match_agent(session, agent, service)
        if not is_usable(agent):
            raise InstallationError('安装设备离线或已禁用，请检查连接后继续。', 409)
        deployment_id = session['deployments'].get(service, {}).get('deployment_id')
        if (deployment_id in agent.get('deployments', {})
                and (agent.get('latest_deployment') or {}).get('deployment_id') != deployment_id):
            raise InstallationError('设备已开始另一项发布，请重新准备本次安装。', 409)
        agents[service] = agent
    return agents


def _resolve_auto_network(repository, manager, session):
    """Read the exact registered host; never run a separate user-side probe."""
    if not session.get('auto_network') or session['network'].get('interface'):
        return
    from backend.agents import linux_gateway
    agents = _pair(repository, manager, session)
    try:
        status = linux_gateway.call(manager, agents['mihomo']['id'], 'status', 'GET', session['profile_id'])
        if not status.get('success') or status.get('supported') is not True or status.get('detection_error'):
            raise InstallationError(status.get('detection_error') or status.get('unsupported_reason') or '暂时无法自动读取安装主机的网络，请检查 Agent 后重试。', 409)
    except MonitorError as exc:
        raise InstallationError(exc.message, exc.status) from None
    try:
        detection = status.get('detection')
        network = _spec(detection.get('suggested') if isinstance(detection, dict) else None)
    except MonitorError:
        raise InstallationError('无法自动确认主机的 LAN 网络或唯一默认路由，请检查主机网络后重试。', 409) from None
    if network['address'] != session['network']['address']:
        raise InstallationError('自动识别的 LAN 地址与 Agent 登记地址不同，请检查多网卡或默认路由后重试。', 409)
    _pair(repository, manager, session)
    candidate = {**session, 'network': network}
    _saved_snapshot(repository, candidate)
    session['network'] = network


def update_network(repository, manager, session_id, body):
    """Read/confirm the enrolled host's network without changing host networking."""
    automatic = isinstance(body, dict) and set(body) == {'auto_network'} and body['auto_network'] is True
    if not automatic and (not isinstance(body, dict) or set(body) != {'network'}):
        raise InstallationError('请选择自动检测，或提交完整网络参数。')
    from backend.agents import linux_gateway
    import ipaddress
    with _locked(repository, session_id):
        session = _read(repository, session_id)
        if session['phase'] not in ('enrolled', 'publish_failed', 'published'):
            raise InstallationError('请等待设备登记完成，发布过程中不能修改网络参数。', 409)
        agents = _pair(repository, manager, session)
        try:
            status = linux_gateway.call(manager, agents['mihomo']['id'], 'status', 'GET', session['profile_id'])
            if not status.get('success') or status.get('supported') is not True or status.get('detection_error'):
                raise InstallationError(status.get('detection_error') or status.get('unsupported_reason') or '无法读取主机网络。', 409)
            detection = status.get('detection')
            if not isinstance(detection, dict):
                raise InstallationError('Agent 未返回有效网络检测结果，请重试。', 409)
            network = _spec(detection.get('suggested') if automatic else body['network'])
        except MonitorError as exc:
            raise InstallationError('自动检测无法确认网络，请在高级选项填写实际网络参数。' if automatic else exc.message, 409 if automatic else exc.status) from None
        if network['address'] != session['network']['address']:
            raise InstallationError('Linux IP 必须与本次已登记设备一致，不能切换目标主机。', 409)
        routes = detection.get('default_routes') or []
        try:
            matching_address = any(
                interface.get('name') == network['interface'] and any(
                    str(ipaddress.ip_interface(address).ip) == network['address'] and
                    str(ipaddress.ip_interface(address).network) == network['lan_cidr']
                    for address in interface.get('addresses', []))
                for interface in detection.get('interfaces', []))
            matching_route = isinstance(routes, list) and len(routes) == 1 and routes[0].get('interface') == network['interface'] and routes[0].get('gateway') == network['upstream_gateway']
        except (ValueError, TypeError, AttributeError):
            matching_address = matching_route = False
        if not matching_address or not matching_route:
            raise InstallationError('参数与主机实际网卡、地址或唯一默认路由不一致；向导不会修改主机 IP 或默认路由。', 409)
        managed = status.get('managed') or {}
        if (managed.get('active') or managed.get('pending')) and network != session['network']:
            try:
                live = _spec(managed.get('spec'))
            except MonitorError:
                live = None
            # A new installation on a host that already forwards starts with
            # an unresolved network; confirming the live parameters is no change.
            if network != live:
                raise InstallationError('请先恢复客户端设置并撤销网关转发，再修改网络参数。', 409)
        _pair(repository, manager, session)
        _saved_snapshot(repository, {**session, 'network': network})
        session.update(network=network, auto_network=automatic)
        _write(repository, session)
        return public(repository, manager, session)


def _publish_service(repository, manager, session, service):
    from backend.routes.agents import _prepare_transactional_content
    from backend.utils.provider_delivery import commit_provider_bundle
    agents = _pair(repository, manager, session)
    agent = agents[service]
    deployment_id = session['deployments'][service]['deployment_id']
    reservation = manager.begin_deployment(agent['id'], deployment_id)
    if reservation.get('existing'):
        return manager.get_deployment(agent['id'], deployment_id)
    if not reservation.get('success'):
        raise InstallationError(reservation.get('message') or '设备正在执行其他任务。', 409)
    try:
        config = _saved_snapshot(repository, session)
        prepared = _prepare_transactional_content(agent, config, base_url=session['server_url'], deployment_id=deployment_id,
                                                  store_downloads=True)
        _pair(repository, manager, session)
        if prepared['prepared_providers']:
            commit_provider_bundle(session['profile_id'], prepared['prepared_providers'])
        return manager.push_config_to_agent(agent['id'], prepared['config_content'],
            extra_data={'deployment_bundle': prepared['bundle'], 'activate': True, 'base_url': session['server_url'],
                        **{key: prepared[key] for key in ('provider_downloads', 'ruleset_downloads', 'custom_files')}},
            config_revision=session['config_revision'], expected_profile_id=session['profile_id'], expected_service_type=service)
    except Exception:
        task = manager.record_deployment(agent['id'], deployment_id, {'success': False, 'status': 'failed', 'message': '安装向导配置发布准备失败，请检查错误后重试。'})
        # Mirror the terminal record into the session; a stale preparing status
        # would make the next retry reuse this ID and only read back the failure.
        _progress(repository, session['id'], service, task)
        raise
    finally:
        manager.finish_deployment_preparation(agent['id'], deployment_id)


def _progress(repository, session_id, service, task):
    safe = {key: task[key] for key in ('deployment_id', 'status', 'message', 'error', 'rollback_error', 'success', 'pending') if key in task}
    with _locked(repository, session_id):
        session = _read(repository, session_id)
        session['deployments'][service].update(safe)
        session['message'] = f'正在发布 {service}：' + str(task.get('message') or task.get('status') or '等待设备')
        _write(repository, session)


def run_publication(repository, manager, session_id, *, pause=time.sleep):
    """Resume recorded IDs; never start OxiDNS before Mihomo health succeeds."""
    try:
        session = _read(repository, session_id)
        for service in SERVICES:
            _pair(repository, manager, session)
            task = _publish_service(repository, manager, session, service)
            deadline = time.monotonic() + PUBLISH_TIMEOUT
            while True:
                _progress(repository, session_id, service, task)
                if task.get('status') == 'succeeded' and not task.get('http_status'):
                    break
                if task.get('status') in ('failed', 'rolled_back', 'rollback_failed'):
                    raise InstallationError(f'{service} 发布未成功：' + str(task.get('error') or task.get('message') or task['status']), 409)
                if task.get('http_status') in (400, 401, 403, 404, 409, 422) and not task.get('pending'):
                    raise InstallationError(task.get('message') or '设备拒绝发布，请检查后继续。', 409)
                if time.monotonic() >= deadline:
                    raise InstallationError('等待发布结果超时，任务编号已保存；可点击继续发布确认结果，不会重复创建任务。', 409)
                if task.get('status') == 'ready' and not task.get('http_status'):
                    # An upload after uncertain transport can be ready. This
                    # wizard explicitly requested activation of these exact IDs.
                    _pair(repository, manager, session)
                    task = manager.activate_deployment(session['agent_ids'][service], task['deployment_id'])
                    if task.get('status') == 'ready':
                        pause(2)
                    continue
                pause(2)
                _pair(repository, manager, session)
                task = manager.get_deployment(session['agent_ids'][service], session['deployments'][service]['deployment_id'])
        _pair(repository, manager, session)
        with _locked(repository, session_id):
            final = _read(repository, session_id)
            final.update(phase='published', message='Mihomo 和 OxiDNS 均已发布并通过健康检查，现在检查 IPv4 转发与 TUN。'
                         if final.get('takeover') == 'full_tun' else
                         'Mihomo 和 OxiDNS 均已发布并通过健康检查，现在可以预览网关转发。')
            _write(repository, final)
        logger.info('网关安装 %s：Mihomo 和 OxiDNS 均已发布并通过健康检查', session['name'])
    except Exception as exc:
        with _locked(repository, session_id):
            final = _read(repository, session_id)
            message = str(exc) if isinstance(exc, (InstallationError, ValueError)) else '发布暂未完成，请检查设备状态后继续；已保存任务编号。'
            # remedy lets the wizard offer the GitHub mirror and download proxy inline.
            final.update(phase='publish_failed', message=message[:1200], remedy=getattr(exc, 'remedy', None))
            _write(repository, final)
        from backend.utils.logger import log_text
        logger.warning('网关安装 %s 发布未完成：%s', final.get('name', session_id[:8]), log_text(message, 300))


def publish(repository, manager, session_id, body, app):
    if not isinstance(body, dict) or set(body) - {'retry_failed'} or type(body.get('retry_failed', False)) is not bool:
        raise InstallationError('发布参数无效。')
    # Keep a separate process lock for the entire worker lifetime. Process death
    # releases it, allowing the same recorded deployment IDs to resume safely.
    lease = open(_path(repository, session_id).with_suffix('.publish.lock'), 'a+b')
    os.chmod(lease.name, 0o600)
    try:
        fcntl.flock(lease, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        lease.close()
        return get(repository, manager, session_id)
    try:
        with _locked(repository, session_id):
            session = _read(repository, session_id)
            _pair(repository, manager, session)
            _resolve_auto_network(repository, manager, session)
            if session['phase'] == 'published':
                lease.close()
                return public(repository, manager, session)
            failed = any(task.get('status') in ('failed', 'rolled_back', 'rollback_failed') for task in session['deployments'].values())
            if failed and not body.get('retry_failed'):
                raise InstallationError('上次发布失败，请确认错误已解决后选择重试发布。', 409)
            if failed or not session['deployments']:
                # A later core can fail after the first one is already healthy.
                # Retain successful/uncertain task IDs so continuation only polls
                # those exact tasks; creating fresh IDs would redeploy Mihomo.
                session['deployments'] = {
                    service: ({'deployment_id': uuid.uuid4().hex, 'status': 'preparing'}
                              if not session['deployments'].get(service)
                              or session['deployments'][service].get('status') in ('failed', 'rolled_back', 'rollback_failed')
                              else session['deployments'][service])
                    for service in SERVICES}
            session.update(phase='publishing', remedy=None, message='开始按顺序发布 Mihomo 和 OxiDNS；任一步失败都会停止后续操作。')
            _write(repository, session)
            logger.info('网关安装 %s：开始按顺序发布 Mihomo 和 OxiDNS', session['name'])
    except Exception:
        lease.close()
        raise
    def worker():
        try:
            with app.app_context():
                run_publication(repository, manager, session_id)
        finally:
            lease.close()
    try:
        threading.Thread(target=worker, name='gateway-onboarding-' + session_id[:8], daemon=True).start()
    except Exception:
        lease.close()
        raise
    return public(repository, manager, session)
