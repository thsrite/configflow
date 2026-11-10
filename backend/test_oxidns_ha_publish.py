"""HA publication must install one snapshot, standby first, even across workers."""
from copy import deepcopy
from dataclasses import dataclass
import hashlib
import io
import json
import tarfile
from concurrent.futures import ThreadPoolExecutor
from threading import Event

import pytest
import requests

from backend.agents import oxidns_ha
from backend.agents.deployment_bundle import build_deployment_bundle
from backend.agents.manager import AgentDeploymentConflict, AgentManager
from backend.common import config as config_store
from backend.common.config_repository import ProfileRepository


PRIMARY = '192.0.2.20'
SECONDARY = '192.0.2.21'
VIP = '192.0.2.53'
CONFIG = '''plugins:
  - tag: shared_rules
    type: domain_set
    args:
      files: [rules/shared.txt]
  - tag: response
    type: sequence
    args:
      - exec: reject 3
  - tag: udp
    type: udp_server
    args:
      entry: response
      listen: 0.0.0.0:53
'''


def archive_files(raw):
    with tarfile.open(fileobj=io.BytesIO(raw), mode='r:gz') as archive:
        manifest = json.load(archive.extractfile('manifest.json'))
        files = {item['path']: archive.extractfile('files/' + item['path']).read()
                 for item in manifest['files']}
    return manifest, files


class PairDevices:
    """Protocol boundary only: preparation, receipts and persistence stay real."""

    def __init__(self):
        self.calls = []
        self.uploads = []
        self.journals = {}
        self.candidates = {}
        self.installed = {PRIMARY: {'config.yaml': b'primary original'},
                          SECONDARY: {'config.yaml': b'secondary original'}}
        self.upload_status = {}
        self.activation_status = {}
        self.unreachable_status = set()
        self.before_upload = None
        self.ha_applied = []
        self.vip_holder = PRIMARY
        self.vip_holders = None
        self.unhealthy = set()
        self.vip_unhealthy = set()
        self.handoff_protocols = {}
        self.handoffs = {}
        self.handoff_calls = []
        self.handoff_unreachable = set()
        self.handoff_lost_reply = set()
        self.auto_handoff = True

    def request(self, agent, method, path, **kwargs):
        host = agent['host']
        self.calls.append((host, method, path))
        if (method, path) == ('get', 'capabilities'):
            return {'success': True, 'service_type': 'oxidns', 'deployment_protocols': [1],
                    'oxidns_ha_protocols': [1],
                    'oxidns_ha_handoff_protocols': self.handoff_protocols.get(host, [1]),
                    'reload_only_protocols': [1], 'config_path': 'config.yaml'}
        if (method, path) == ('post', 'deployments'):
            manifest, files = archive_files(kwargs['data'])
            ident = manifest['deployment_id']
            assert manifest['agent_id'] == agent['id']
            assert kwargs['headers']['X-Content-SHA256'] == hashlib.sha256(kwargs['data']).hexdigest()
            self.uploads.append({'host': host, 'id': ident, 'files': files,
                                 'activate': kwargs['headers']['X-Activate'] == 'true'})
            self.candidates[host, ident] = files
            if self.before_upload:
                self.before_upload(host)
            status = self.upload_status.get(host, 'succeeded' if self.uploads[-1]['activate'] else 'ready')
            self.journals[host, ident] = {'success': status not in ('failed', 'rolled_back', 'rollback_failed'),
                                         'deployment_id': ident, 'status': status,
                                         'sha256': kwargs['headers']['X-Content-SHA256'],
                                         'config_version': hashlib.sha256(files['config.yaml']).hexdigest()[:8]}
            if status == 'succeeded':
                self.installed[host] = deepcopy(files)
            return deepcopy(self.journals[host, ident])
        assert path.startswith('deployments/'), (method, path)
        ident = path.split('/')[1]
        key = host, ident
        if method == 'get':
            if host in self.unreachable_status:
                raise requests.Timeout('status reply lost')
            return deepcopy(self.journals.get(key, {'success': False, 'http_status': 404}))
        if method == 'post' and path.endswith('/activate'):
            status = self.activation_status.get(host, 'succeeded')
            self.journals[key].update(status=status, success=status not in ('failed', 'rolled_back'))
            if status == 'succeeded':
                self.installed[host] = deepcopy(self.candidates[key])
            return deepcopy(self.journals[key])
        pytest.fail(f'unexpected deployment request: {host} {method} {path}')

    def status(self, agent):
        host = agent['host']
        master = host in self.vip_holders if self.vip_holders is not None else host == self.vip_holder
        return {'protocol': 1, 'managed': {'active': True, 'pending': False},
                'runtime': {'keepalived_running': True, 'role': 'master' if master else 'backup',
                            'vip_present': master, 'healthy': host not in self.unhealthy,
                            'vip_healthy': host not in self.vip_unhealthy},
                'handoff': deepcopy(self.handoffs.get(host)), 'error': ''}

    def handoff(self, agent, method, operation_id=None):
        host, method = agent['host'], method.lower()
        self.calls.append((host, method, 'handoff'))
        self.handoff_calls.append((host, method, operation_id))
        if (host, method) in self.handoff_unreachable:
            raise requests.Timeout('handoff request unavailable')
        active = self.handoffs.get(host)
        peer = SECONDARY if host == PRIMARY else PRIMARY
        if method == 'post':
            assert operation_id, 'handoff mutations require a durable operation identity'
            if active and active['operation_id'] != operation_id:
                raise ValueError('another handoff operation owns this device')
            self.handoffs[host] = {'operation_id': operation_id, 'active': True,
                'peer_healthy': peer not in self.unhealthy, 'yielding': peer not in self.unhealthy,
                'created_at': '2026-10-27T05:00:00Z'}
            if self.auto_handoff and peer not in self.unhealthy:
                self.vip_holder = peer
        elif method == 'delete':
            assert operation_id, 'cleanup must target the accepted handoff identity'
            if active and active['operation_id'] != operation_id:
                raise ValueError('another handoff operation owns this device')
            self.handoffs.pop(host, None)
            if self.auto_handoff:
                self.vip_holder = PRIMARY
        else:
            assert method == 'get'
        if (host, method) in self.handoff_lost_reply:
            self.handoff_lost_reply.remove((host, method))
            raise requests.Timeout('handoff applied, but reply lost')
        return self.status(agent)

    def preview(self, _agent, _spec):
        return {'ok': True, 'blockers': [], 'interface': 'eth0', 'cidr': '192.0.2.0/24'}

    def apply(self, agent, _spec):
        self.ha_applied.append(agent['host'])
        return {'protocol': 1}

    def mutations(self):
        return [(host, path) for host, method, path in self.calls if method == 'post']


@dataclass
class Harness:
    repository: ProfileRepository
    manager: AgentManager
    devices: PairDevices
    primary: dict
    secondary: dict
    config: dict

    def pair(self):
        def save(items):
            for item in items:
                if item['id'] not in (self.primary['id'], self.secondary['id']):
                    continue
                is_primary = item['id'] == self.primary['id']
                item['oxidns_ha'] = {'enabled': True, 'group_id': 'ha-fixture', 'vip': VIP, 'vrid': 53,
                                     'peer_id': self.secondary['id'] if is_primary else self.primary['id'],
                                     'role': 'primary' if is_primary else 'secondary'}
                item['oxidns_ha_state'] = {'status': 'succeeded', 'error': ''}
        self.manager._update_agents(save)

    def bundle(self, owner=None, ident='ha-publish', rule='domain:shared.example'):
        owner = owner or self.primary
        return build_deployment_bundle(owner, self.config, CONFIG, deployment_id=ident,
                                       custom_files=[{'path': 'rules/shared.txt', 'content': rule}],
                                       local_dns_watchdog=False)

    def drain(self, owner=None, manager=None, attempts=16):
        from backend.agents import oxidns_ha_publish
        for _ in range(attempts):
            oxidns_ha_publish.advance(manager or self.manager, (owner or self.primary)['id'])


@pytest.fixture
def ha_publish(tmp_path, monkeypatch):
    from backend.agents import oxidns_ha_publish
    previous = config_store._repository
    repository = ProfileRepository(tmp_path)
    config_store.set_repository(repository)
    manager = AgentManager(repository)
    devices = PairDevices()
    monkeypatch.setattr(oxidns_ha_publish, 'start_loop', lambda _manager: None)
    monkeypatch.setattr(oxidns_ha, 'SETTLE_SECONDS', 0)
    monkeypatch.setattr(AgentManager, '_deployment_request',
                        lambda _self, agent, method, path, **kwargs: devices.request(agent, method, path, **kwargs))
    for name in ('status', 'preview', 'apply'):
        monkeypatch.setattr(AgentManager, f'oxidns_ha_{name}',
                            lambda _self, *args, _name=name: getattr(devices, _name)(*args))
    monkeypatch.setattr(AgentManager, 'oxidns_ha_remove', lambda _self, _agent: {'removed': True})
    monkeypatch.setattr(AgentManager, 'oxidns_ha_handoff',
                        lambda _self, *args, **kwargs: devices.handoff(*args, **kwargs), raising=False)
    monkeypatch.setattr('requests.sessions.Session.request',
                        lambda *_args, **_kwargs: pytest.fail('unexpected external request'))
    primary = manager.register_agent({'name': 'DNS primary', 'host': PRIMARY, 'port': 8082,
                                      'profile_id': 'default', 'service_type': 'oxidns',
                                      'deployment_method': 'shell', 'version': '1.8.45-go'})
    secondary = manager.register_agent({'name': 'DNS secondary', 'host': SECONDARY, 'port': 8082,
                                        'profile_id': 'default', 'service_type': 'oxidns',
                                        'deployment_method': 'shell', 'version': '1.8.45-go'})
    for agent in (primary, secondary):
        manager.update_heartbeat(agent['id'], {'service_status': 'running'})
    primary, secondary = (manager.get_agent_by_id(agent['id']) for agent in (primary, secondary))
    config = {'profile_id': 'default', 'rule_library': [], 'system_config': {'server_domain': 'https://config.test'}}
    yield Harness(repository, manager, devices, primary, secondary, config)
    config_store.set_repository(previous)


@pytest.mark.parametrize('entry', ['primary', 'secondary'])
def test_publish_from_either_member_installs_identical_files_standby_first(ha_publish, entry):
    env = ha_publish
    env.pair()
    owner = getattr(env, entry)
    candidate = env.bundle(owner)
    expected = archive_files(candidate.archive)[1]
    checked = []

    def before_upload(host):
        if host == PRIMARY:
            assert env.devices.installed[SECONDARY] == expected
            checked.append(host)
    env.devices.before_upload = before_upload
    env.manager.publish_deployment(owner['id'], candidate)
    env.drain(owner)
    result = env.manager.get_deployment(owner['id'], candidate.manifest['deployment_id'])
    assert result['status'] == 'succeeded'
    assert [item['host'] for item in env.devices.uploads] == [SECONDARY, PRIMARY]
    assert checked == [PRIMARY]
    assert env.devices.installed[PRIMARY] == env.devices.installed[SECONDARY] == expected
    first = env.manager.get_agent_by_id(env.primary['id'])
    second = env.manager.get_agent_by_id(env.secondary['id'])
    assert first['applied_content']['fingerprint'] == second['applied_content']['fingerprint']


def test_initial_enable_materializes_once_and_synchronizes_different_configs_before_keepalived(ha_publish, monkeypatch):
    from backend.agents import rule_sync
    from backend.routes import agents as agent_routes
    env = ha_publish
    rule_sync.configure(env.manager, env.primary['id'], {'library': True, 'rules': False})
    env.repository.save_profile('default', {'oxidns': {
        'config_mode': 'native', 'native_config': CONFIG,
        'additional_files': [{'path': 'rules/shared.txt', 'content': 'domain:first-enable.example'}]}})
    prepare = agent_routes._prepare_transactional_content
    preparations = []

    def counted_prepare(*args, **kwargs):
        prepared = prepare(*args, **kwargs)
        preparations.append(prepared['bundle'])
        return prepared
    monkeypatch.setattr(agent_routes, '_prepare_transactional_content', counted_prepare)
    applied = env.devices.apply

    def apply_only_after_both_configs_match(agent, spec):
        assert env.devices.installed[PRIMARY] == env.devices.installed[SECONDARY]
        assert env.devices.installed[PRIMARY]['rules/shared.txt'] == b'domain:first-enable.example'
        return applied(agent, spec)
    monkeypatch.setattr(env.devices, 'apply', apply_only_after_both_configs_match)
    oxidns_ha.configure(env.manager, env.primary['id'], {'peer_id': env.secondary['id'], 'vip': VIP})
    # Enabling returns before network preparation; the persisted job is recoverable.
    assert preparations == [] and env.devices.uploads == []
    env.repository.save_profile('default', {'oxidns': {
        'config_mode': 'native', 'native_config': CONFIG,
        'additional_files': [{'path': 'rules/shared.txt', 'content': 'domain:newer-draft.example'}]}})
    env.drain()
    state = oxidns_ha.status(env.manager, env.primary['id'])
    assert state['state']['status'] == 'succeeded'
    assert len(preparations) == 1
    assert [item['host'] for item in env.devices.uploads] == [SECONDARY, PRIMARY]
    assert env.devices.ha_applied == [SECONDARY, PRIMARY]
    assert env.devices.installed[PRIMARY] == env.devices.installed[SECONDARY]
    for agent in (env.primary, env.secondary):
        assert rule_sync.settings_for(env.manager.get_agent_by_id(agent['id'])) == {'library': True, 'rules': False}


@pytest.mark.parametrize('standby_status', ['failed', 'rolled_back', 'rollback_failed'])
def test_standby_failure_never_uploads_or_activates_primary(ha_publish, standby_status):
    env = ha_publish
    env.pair()
    env.devices.upload_status[SECONDARY] = standby_status
    original = deepcopy(env.devices.installed[PRIMARY])
    candidate = env.bundle()
    env.manager.publish_deployment(env.primary['id'], candidate)
    env.drain()
    result = env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])
    assert result['status'] in ('failed', 'rolled_back', 'rollback_failed')
    assert all(host == SECONDARY for host, _ in env.devices.mutations())
    assert env.devices.installed[PRIMARY] == original
    assert 'applied_content' not in env.manager.get_agent_by_id(env.primary['id'])
    primary = next(item for item in result['ha_publication']['members'] if item['agent_id'] == env.primary['id'])
    assert primary['status'] == 'skipped'


def test_repeating_initial_enable_reuses_one_pending_publication(ha_publish):
    env = ha_publish
    env.repository.save_profile('default', {'oxidns': {
        'config_mode': 'native', 'native_config': CONFIG,
        'additional_files': [{'path': 'rules/shared.txt', 'content': 'domain:first-enable.example'}]}})
    request = {'peer_id': env.secondary['id'], 'vip': VIP}
    first = oxidns_ha.configure(env.manager, env.primary['id'], request)
    repeated = oxidns_ha.configure(env.manager, env.primary['id'], request)
    assert repeated['publication']['id'] == first['publication']['id']
    assert env.devices.uploads == []
    env.devices.upload_status[SECONDARY] = 'unknown'
    env.drain()
    repeated = oxidns_ha.configure(env.manager, env.primary['id'], request)
    assert repeated['publication']['id'] == first['publication']['id']
    env.drain()
    assert [item['host'] for item in env.devices.uploads] == [SECONDARY]


def test_unhealthy_standby_blocks_primary_despite_successful_deployment_receipt(ha_publish):
    env = ha_publish
    env.pair()
    env.devices.unhealthy.add(SECONDARY)
    candidate = env.bundle()
    env.manager.publish_deployment(env.primary['id'], candidate)
    env.drain()
    result = env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])
    assert result['status'] == 'failed'
    assert '健康检查' in result['message']
    assert [item['host'] for item in env.devices.uploads] == [SECONDARY]
    assert env.devices.installed[PRIMARY] == {'config.yaml': b'primary original'}


def test_unknown_health_after_standby_success_blocks_primary_until_confirmed(ha_publish, monkeypatch):
    env = ha_publish
    env.pair()
    real_status = env.devices.status
    unreachable = {'value': True}

    def status(agent):
        if agent['host'] == SECONDARY and env.devices.installed[SECONDARY] != {'config.yaml': b'secondary original'} and unreachable['value']:
            raise RuntimeError('health reply is temporarily unavailable')
        return real_status(agent)
    monkeypatch.setattr(env.devices, 'status', status)
    candidate = env.bundle()
    env.manager.publish_deployment(env.primary['id'], candidate)
    env.drain()
    result = env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])
    assert result['status'] == 'unknown'
    assert [item['host'] for item in env.devices.uploads] == [SECONDARY]
    unreachable['value'] = False
    env.drain()
    assert env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])['status'] == 'succeeded'
    assert [item['host'] for item in env.devices.uploads] == [SECONDARY, PRIMARY]


def test_primary_failure_is_not_hidden_by_successful_secondary_entry_record(ha_publish):
    env = ha_publish
    env.pair()
    env.devices.upload_status[PRIMARY] = 'failed'
    candidate = env.bundle(env.secondary)
    env.manager.publish_deployment(env.secondary['id'], candidate)
    env.drain(env.secondary)
    result = env.manager.get_deployment(env.secondary['id'], candidate.manifest['deployment_id'])
    assert result['status'] == 'failed' and not result['success']
    secondary = env.manager.get_agent_by_id(env.secondary['id'])
    assert secondary['deployments'][candidate.manifest['deployment_id']]['status'] == 'succeeded'
    assert env.devices.installed[PRIMARY] == {'config.yaml': b'primary original'}


@pytest.mark.parametrize('status_unavailable', [False, True], ids=['unknown', 'status-timeout'])
def test_unknown_or_unconfirmed_standby_blocks_primary_and_new_publications(ha_publish, status_unavailable):
    env = ha_publish
    env.pair()
    env.devices.upload_status[SECONDARY] = 'unknown' if not status_unavailable else 'verifying'
    if status_unavailable:
        env.devices.unreachable_status.add(SECONDARY)
    candidate = env.bundle()
    env.manager.publish_deployment(env.primary['id'], candidate)
    env.drain()
    result = env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])
    assert result['status'] not in ('succeeded', 'failed', 'ready')
    assert all(host == SECONDARY for host, _ in env.devices.mutations())
    competing = env.manager.begin_deployment(env.secondary['id'], 'competing')
    assert not competing['success'] and competing['http_status'] == 409


def test_confirmed_standby_result_resumes_after_lost_reply_without_reupload(ha_publish):
    env = ha_publish
    env.pair()
    env.devices.upload_status[SECONDARY] = 'unknown'
    candidate = env.bundle()
    env.manager.publish_deployment(env.primary['id'], candidate)
    env.drain()
    assert [item['host'] for item in env.devices.uploads] == [SECONDARY]
    secondary_id = env.devices.uploads[0]['id']
    # The Agent completed its durable task while the management server was away.
    env.devices.journals[SECONDARY, secondary_id].update(status='succeeded', success=True)
    env.devices.installed[SECONDARY] = deepcopy(env.devices.candidates[SECONDARY, secondary_id])
    restarted = AgentManager(env.repository)
    env.drain(manager=restarted)
    assert restarted.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])['status'] == 'succeeded'
    assert [item['host'] for item in env.devices.uploads] == [SECONDARY, PRIMARY]
    assert env.devices.installed[PRIMARY] == env.devices.installed[SECONDARY]


def test_current_vip_holder_is_published_last_after_failover(ha_publish):
    env = ha_publish
    env.pair()
    env.devices.vip_holder = SECONDARY
    candidate = env.bundle(env.secondary)
    env.manager.publish_deployment(env.secondary['id'], candidate)
    env.drain(env.secondary)
    result = env.manager.get_deployment(env.secondary['id'], candidate.manifest['deployment_id'])
    assert result['status'] == 'succeeded'
    assert [item['host'] for item in env.devices.uploads] == [PRIMARY, SECONDARY]


def test_completed_group_replay_does_not_reupload_either_member(ha_publish):
    env = ha_publish
    env.pair()
    candidate = env.bundle()
    env.manager.publish_deployment(env.primary['id'], candidate)
    env.drain()
    assert len(env.devices.uploads) == 2
    replay = env.manager.publish_deployment(env.primary['id'], candidate)
    env.drain()
    assert replay['status'] == 'succeeded'
    assert len(env.devices.uploads) == 2


def test_reusing_group_deployment_id_with_different_rule_bytes_is_rejected(ha_publish):
    env = ha_publish
    env.pair()
    first = env.bundle()
    env.manager.publish_deployment(env.primary['id'], first)
    env.drain()
    changed = env.bundle(rule='domain:different.example')
    try:
        result = env.manager.publish_deployment(env.primary['id'], changed)
    except ValueError:
        pass
    else:
        assert not result['success'] and result['http_status'] == 409
    assert len(env.devices.uploads) == 2


def test_staged_pair_resumes_after_manager_restart_and_activates_standby_before_primary(ha_publish):
    env = ha_publish
    env.pair()
    candidate = env.bundle()
    original = deepcopy(env.devices.installed)
    env.manager.publish_deployment(env.primary['id'], candidate, activate=False)
    env.drain()
    result = env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])
    assert result['status'] == 'ready'
    assert len(env.devices.uploads) == 2
    assert not any(item['activate'] for item in env.devices.uploads)
    assert env.devices.installed == original
    secondary_id = next(item['id'] for item in env.devices.uploads if item['host'] == SECONDARY)
    # A newer draft must not replace the already prepared pair candidate.
    env.repository.save_profile('default', {'oxidns': {'config_mode': 'native', 'native_config': 'plugins: []\n'}})
    restarted = AgentManager(env.repository)
    restarted.activate_deployment(env.secondary['id'], secondary_id)
    env.drain(env.secondary, restarted)
    result = restarted.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])
    assert result['status'] == 'succeeded'
    activations = [host for host, path in env.devices.mutations() if path.endswith('/activate')]
    assert activations == [SECONDARY, PRIMARY]
    assert env.devices.installed[PRIMARY] == env.devices.installed[SECONDARY] == archive_files(candidate.archive)[1]


def test_staged_activation_rechecks_vip_holder_instead_of_using_stale_order(ha_publish):
    env = ha_publish
    env.pair()
    candidate = env.bundle()
    env.manager.publish_deployment(env.primary['id'], candidate, activate=False)
    env.drain()
    assert env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])['status'] == 'ready'
    env.devices.vip_holder = SECONDARY
    env.manager.activate_deployment(env.primary['id'], candidate.manifest['deployment_id'])
    env.drain()
    assert [host for host, path in env.devices.mutations() if path.endswith('/activate')] == [PRIMARY, SECONDARY]


def test_failed_staging_releases_unactivated_peer_for_a_new_group(ha_publish):
    env = ha_publish
    env.pair()
    env.devices.upload_status[PRIMARY] = 'failed'
    first = env.bundle(ident='staged-failure')
    env.manager.publish_deployment(env.primary['id'], first, activate=False)
    env.drain()
    assert env.manager.get_deployment(env.primary['id'], first.manifest['deployment_id'])['status'] == 'failed'
    assert env.devices.installed == {PRIMARY: {'config.yaml': b'primary original'},
                                     SECONDARY: {'config.yaml': b'secondary original'}}
    env.devices.upload_status.clear()
    replacement = env.bundle(ident='retry-staging')
    env.manager.publish_deployment(env.primary['id'], replacement)
    env.drain()
    assert env.manager.get_deployment(env.primary['id'], replacement.manifest['deployment_id'])['status'] == 'succeeded'


def test_agent_side_rollback_recovery_is_reconciled_before_republishing_pair(ha_publish):
    env = ha_publish
    env.pair()
    env.devices.upload_status[SECONDARY] = 'rollback_failed'
    first = env.bundle(ident='needs-recovery')
    env.manager.publish_deployment(env.primary['id'], first)
    env.drain()
    assert env.manager.get_deployment(env.primary['id'], first.manifest['deployment_id'])['status'] == 'rollback_failed'
    secondary_id = env.devices.uploads[0]['id']
    # Explicit Agent-side recovery restored the old files; it is not a successful new publication.
    env.devices.journals[SECONDARY, secondary_id].update(status='rolled_back', success=False)
    for _ in range(3):
        result = env.manager.get_deployment(env.primary['id'], first.manifest['deployment_id'])
    assert result['status'] == 'failed'
    assert env.manager.get_agent_by_id(env.secondary['id'])['deployments'][secondary_id]['status'] == 'rolled_back'
    env.devices.upload_status.clear()
    replacement = env.bundle(ident='after-recovery')
    env.manager.publish_deployment(env.primary['id'], replacement)
    env.drain()
    assert env.manager.get_deployment(env.primary['id'], replacement.manifest['deployment_id'])['status'] == 'succeeded'


@pytest.mark.parametrize('operation', ['restart_agent_service', 'uninstall_agent'])
def test_group_reservation_blocks_lifecycle_on_a_locally_successful_member(ha_publish, monkeypatch, operation):
    env = ha_publish
    env.pair()
    env.devices.upload_status[PRIMARY] = 'verifying'
    env.manager.publish_deployment(env.primary['id'], env.bundle())
    env.drain()
    secondary = env.manager.get_agent_by_id(env.secondary['id'])
    assert secondary['latest_deployment']['status'] == 'succeeded'
    calls = []
    def post(*args, **kwargs):
        calls.append(args)
        response = requests.Response()
        response.status_code = 200
        response._content = b'{"success":true}'
        return response
    monkeypatch.setattr('backend.agents.manager.requests.post', post)
    result = getattr(env.manager, operation)(env.secondary['id'])
    assert not result['success']
    assert calls == []


def test_group_reservation_blocks_upgrade_after_local_child_succeeds(ha_publish, monkeypatch):
    from backend.agents import upgrades
    env = ha_publish
    env.pair()
    env.devices.upload_status[PRIMARY] = 'verifying'
    env.manager.publish_deployment(env.primary['id'], env.bundle())
    env.drain()
    calls = []
    def request(agent, method, path, **_kwargs):
        calls.append((method, path))
        response = requests.Response()
        response.status_code = 200
        response._content = json.dumps({'agent_id': agent['id'], 'deployment_method': 'shell',
                                        'architecture': 'linux-amd64'} if method == 'GET' else {}).encode()
        return response
    monkeypatch.setattr(upgrades, '_request', request)
    monkeypatch.setattr(upgrades, 'artifacts', lambda: {'linux-amd64': {'sha256': 'a' * 64, 'size': 100}})
    with pytest.raises(ValueError):
        upgrades.start(env.manager, env.secondary['id'])
    assert not any(method == 'POST' for method, _ in calls)


def test_pending_group_blocks_port_change_via_reregistration(ha_publish):
    env = ha_publish
    env.pair()
    env.devices.upload_status[SECONDARY] = 'unknown'
    env.manager.publish_deployment(env.primary['id'], env.bundle())
    primary = env.manager.get_agent_by_id(env.primary['id'])
    with pytest.raises(AgentDeploymentConflict):
        env.manager.register_agent({**primary, 'port': 8999}, existing_token=primary['token'])
    assert env.manager.get_agent_by_id(primary['id'])['port'] == 8082


def test_competing_publish_from_other_worker_cannot_reserve_peer_during_upload(ha_publish):
    env = ha_publish
    env.pair()
    entered = Event()
    release = Event()
    candidate = env.bundle()
    other = AgentManager(env.repository)

    def block_secondary(host):
        if host == SECONDARY:
            entered.set()
            assert release.wait(5), 'concurrency test did not release the first upload'
    env.devices.before_upload = block_secondary
    with ThreadPoolExecutor(max_workers=1) as pool:
        future = pool.submit(env.manager.publish_deployment, env.primary['id'], candidate)
        try:
            assert entered.wait(5), 'first publication did not reach the secondary'
            rejection = other.begin_deployment(env.secondary['id'], 'other-worker')
            assert rejection['http_status'] == 409 and not rejection['success']
        finally:
            release.set()
        future.result(timeout=5)
    env.drain()
    assert [item['host'] for item in env.devices.uploads] == [SECONDARY, PRIMARY]


def test_non_ha_oxidns_preserves_single_agent_publish_behavior(ha_publish):
    env = ha_publish
    candidate = env.bundle()
    result = env.manager.publish_deployment(env.primary['id'], candidate)
    assert result['status'] == 'succeeded'
    assert [item['host'] for item in env.devices.uploads] == [PRIMARY]
    assert env.devices.uploads[0]['activate'] is True
    assert env.devices.installed[SECONDARY] == {'config.yaml': b'secondary original'}
    assert 'oxidns_ha_publication' not in env.manager.get_agent_by_id(env.primary['id'])


@pytest.mark.parametrize('listen', ['127.0.0.1:53', '192.0.2.20:53', '0.0.0.0:5353'])
def test_candidate_without_wildcard_udp53_is_rejected_before_any_device_write(ha_publish, listen):
    env = ha_publish
    env.pair()
    candidate = build_deployment_bundle(env.primary, env.config, CONFIG.replace('0.0.0.0:53', listen),
        deployment_id='invalid-ha-listener',
        custom_files=[{'path': 'rules/shared.txt', 'content': 'domain:unchanged.example'}],
        local_dns_watchdog=False)
    env.manager.publish_deployment(env.primary['id'], candidate)
    env.drain()
    result = env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])
    assert result['status'] == 'failed'
    assert 'UDP 53' in result['message']
    assert env.devices.mutations() == []


def publish_generated_rule_baseline(env):
    from backend.converters.oxidns import generate_oxidns_config, get_oxidns_ruleset_downloads
    env.repository.update_shared_transaction(lambda shared: shared['rule_library'].append(
        {'id': 'ha-library', 'name': 'HA library', 'source_type': 'content',
         'behavior': 'domain', 'content': 'old.example'}))
    env.repository.update_profile_fields('default', {'rule_configs': [
        {'id': 'ha-rule', 'itemType': 'ruleset', 'library_rule_id': 'ha-library',
         'policy': 'DIRECT', 'enabled': True}]})
    env.repository.update_profile_transaction('default', lambda profile: profile['oxidns'].update(
        listen='0.0.0.0:53', direct_rulesets=['ha-rule'], proxy_rulesets=[]))
    source, _ = env.repository.get_publish_snapshot('default')
    bundle = build_deployment_bundle(env.primary, source,
        generate_oxidns_config(source, base_url='https://config.test'),
        ruleset_downloads=get_oxidns_ruleset_downloads(source, base_url='https://config.test'),
        base_url='https://config.test', deployment_id='ha-baseline')
    env.manager.publish_deployment(env.primary['id'], bundle, source_snapshot=source,
                                   base_url='https://config.test')
    env.drain()
    assert env.manager.get_deployment(env.primary['id'], 'ha-baseline')['status'] == 'succeeded'
    return source, bundle


def test_manual_rule_update_uses_committed_source_and_same_bundle_on_both_members(ha_publish, monkeypatch):
    from backend.agents import oxidns_rule_update, rule_sync, rule_sync_snapshot
    env = ha_publish
    env.pair()
    source, baseline = publish_generated_rule_baseline(env)
    monkeypatch.setattr(rule_sync_snapshot, 'fetch_snapshot', lambda _manager, _agent, saved: saved['bundle'])
    preparations = []
    prepare = rule_sync.prepare_candidate

    def counted_prepare(agent, selected, *args, **kwargs):
        assert selected['oxidns'] == source['oxidns']
        preparations.append(agent['id'])
        return prepare(agent, selected, *args, **kwargs)
    monkeypatch.setattr(rule_sync, 'prepare_candidate', counted_prepare)
    # The current editor draft is deliberately unsuitable for HA; updating
    # already-deployed rules must keep the published listener and upstreams.
    env.repository.update_profile_transaction('default', lambda profile: profile['oxidns'].update(
        listen='127.0.0.1:5353', local_dns='udp://192.0.2.99'))
    result, code = oxidns_rule_update.call(env.manager, env.secondary['id'], 'POST')
    assert code in (200, 202)
    env.drain(env.secondary)
    result, code = oxidns_rule_update.call(env.manager, env.primary['id'], 'GET')
    assert code == 200 and result['task']['status'] == 'succeeded'
    updates = env.devices.uploads[2:]
    assert [item['host'] for item in updates] == [SECONDARY, PRIMARY]
    assert len(preparations) == 1
    assert updates[0]['files'] == updates[1]['files']
    assert updates[0]['files'][baseline.manifest['config_path']] == archive_files(baseline.archive)[1][baseline.manifest['config_path']]


def test_automatic_rule_workers_on_both_members_create_only_one_pair_publication(ha_publish, monkeypatch):
    from backend.agents import rule_sync, rule_sync_store
    env = ha_publish
    env.pair()
    _, baseline = publish_generated_rule_baseline(env)
    monkeypatch.setattr(rule_sync, 'fetch_snapshot', lambda _manager, _agent, saved: saved['bundle'])
    for agent in (env.primary, env.secondary):
        rule_sync.configure(env.manager, agent['id'], {'library': True, 'rules': False})
    env.repository.update_shared_transaction(lambda shared: next(
        item for item in shared['rule_library'] if item['id'] == 'ha-library').update(content='new.example'))
    now = [100.0]
    preparations = []

    def prepare(*args, **kwargs):
        preparations.append(args[0]['id'])
        return rule_sync.prepare_candidate(*args, **kwargs)
    workers = [rule_sync.RuleSyncWorker(AgentManager(env.repository), clock=lambda: now[0], prepare=prepare)
               for _ in range(2)]
    with ThreadPoolExecutor(max_workers=2) as pool:
        for _ in range(12):
            futures = [pool.submit(worker.run, agent['id'])
                       for worker, agent in zip(workers, (env.primary, env.secondary))]
            for future in futures:
                future.result(timeout=10)
            now[0] += 5
    assert preparations == [env.primary['id']]
    updates = env.devices.uploads[2:]
    assert [item['host'] for item in updates] == [SECONDARY, PRIMARY]
    assert updates[0]['files'] == updates[1]['files']
    assert updates[0]['files'][baseline.manifest['config_path']] == archive_files(baseline.archive)[1][baseline.manifest['config_path']]
    assert any(b'new.example' in value for path, value in updates[0]['files'].items() if path.startswith('rules/'))
    for agent in (env.primary, env.secondary):
        saved = rule_sync_store.load(env.repository, env.manager.get_agent_by_id(agent['id']))
        assert next(item for item in saved['source']['rule_library'] if item['id'] == 'ha-library')['content'] == 'new.example'


@pytest.mark.parametrize('entry', ['primary', 'secondary'])
def test_rule_sync_scopes_are_shared_when_changed_from_either_member(ha_publish, entry):
    from backend.agents import rule_sync
    env = ha_publish
    env.pair()
    for scopes in ({'library': True, 'rules': False}, {'library': False, 'rules': True},
                   {'library': False, 'rules': False}):
        rule_sync.configure(env.manager, getattr(env, entry)['id'], scopes)
        for agent in (env.primary, env.secondary):
            assert rule_sync.settings_for(env.manager.get_agent_by_id(agent['id'])) == scopes


def test_reconciliation_rests_without_unfinished_pair_work_and_resumes_with_it(ha_publish, monkeypatch):
    from backend.agents import oxidns_ha_publish
    env = ha_publish
    env.pair()
    advanced = []
    real = oxidns_ha_publish.advance
    monkeypatch.setattr(oxidns_ha_publish, 'advance', lambda manager, agent_id: advanced.append(agent_id) or real(manager, agent_id))
    oxidns_ha_publish.reconcile(env.manager)
    assert advanced == []
    with monkeypatch.context() as patched:
        patched.setattr(env.manager, 'get_all_agents', lambda: pytest.fail('idle round read the Agents'))
        for _ in range(5):
            oxidns_ha_publish.reconcile(env.manager)
    # A staged pair publication is unfinished work: every round advances it once, for one member.
    candidate = env.bundle()
    env.manager.publish_deployment(env.primary['id'], candidate, activate=False)
    for _ in range(16):
        if env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])['status'] == 'ready':
            break
        before = len(advanced)
        oxidns_ha_publish.reconcile(env.manager)
        assert len(advanced) == before + 1
    assert env.manager.get_deployment(env.primary['id'], candidate.manifest['deployment_id'])['status'] == 'ready'
    # 'ready' waits for the user: rounds rest again after one look.
    del advanced[:]
    oxidns_ha_publish.reconcile(env.manager)
    monkeypatch.setattr(env.manager, 'get_all_agents', lambda: pytest.fail('idle round read the Agents'))
    oxidns_ha_publish.reconcile(env.manager)
    assert advanced == []


def test_failed_reconciliation_round_is_not_taken_for_an_idle_one(ha_publish, monkeypatch):
    from backend.agents import oxidns_ha_publish
    env = ha_publish
    env.pair()
    reads, real = [], env.manager.get_all_agents
    def failing():
        reads.append(1)
        raise RuntimeError('database busy')
    monkeypatch.setattr(env.manager, 'get_all_agents', failing)
    for _ in range(2):
        with pytest.raises(RuntimeError):
            oxidns_ha_publish.reconcile(env.manager)
    assert len(reads) == 2
    monkeypatch.setattr(env.manager, 'get_all_agents', lambda: reads.append(1) or real())
    oxidns_ha_publish.reconcile(env.manager)
    assert len(reads) == 3
