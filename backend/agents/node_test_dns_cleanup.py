"""One-time cleanup of the removed "OxiDNS 托管" node-test DNS mode.

Earlier versions published a dedicated listener (plugins tagged
``cf_node_test_<id>_*``) to an OxiDNS device and recorded it on the Agent.
Leaving that behind would keep an unexplained port open on the device, so at
startup the listener is taken out of the profile's OxiDNS configuration, node
tests move to an IP-form DoH (which a gateway's DNS hijack does not touch) and
the user is told. Nothing is published here: the device drops the listener
with its next ordinary OxiDNS publication.
"""
from copy import deepcopy
import logging

import yaml

from backend.common.dns_watchdog import WATCHDOG_RULE
from backend.common.oxidns_references import reference_locations

FIELD = 'node_test_dns_endpoints'
NOTICE_KEY = 'notice:node-test-dns-managed-removed'
_log = logging.getLogger(__name__)


def _normalized(plugin):
    result = deepcopy(plugin)
    if result.get('type') == 'sequence' and isinstance(result.get('args'), list):
        # Publication adds this health check to every entry sequence.
        result['args'] = [rule for rule in result['args'] if rule != WATCHDOG_RULE]
    return result


def _without_listener(document, record):
    """The document without this record's plugins, or None when they must stay.

    Only plugins identical to what ConfigFlow published are removed; a
    hand-edited copy, or one something else still refers to, is the user's.
    """
    expected = {p.get('tag'): p for p in record.get('plugins') or [] if isinstance(p, dict)}
    found = [p for p in document['plugins'] if p.get('tag') in expected]
    if not expected or not found:
        return document
    if len(found) != len(expected) or {p['tag']: _normalized(p) for p in found} != expected:
        return None
    result = deepcopy(document)
    result['plugins'] = [p for p in result['plugins'] if p.get('tag') not in expected]
    return None if reference_locations(result, set(expected)) else result


def _clean_profile(repository, profile_id, records):
    """Remove intact listeners from one profile; return the endpoints that are gone."""
    gone = set()

    def update(profile):
        settings = profile.get('oxidns')
        try:
            document = yaml.safe_load(settings.get('native_config', '')) if isinstance(settings, dict) else None
        except yaml.YAMLError:
            document = None
        if (not isinstance(document, dict) or not isinstance(document.get('plugins'), list)
                or not all(isinstance(plugin, dict) for plugin in document['plugins'])):
            # No readable native configuration: nothing of ours can be in it.
            gone.update(record.get('endpoint') for record in records)
            return
        changed = False
        for record in records:
            cleaned = _without_listener(document, record)
            if cleaned is None:
                continue
            gone.add(record.get('endpoint'))
            if cleaned is not document:
                document, changed = cleaned, True
        if changed:
            settings['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)

    try:
        repository.update_profile_transaction(profile_id, update)
    except Exception:
        _log.exception('Could not remove managed node-test DNS listeners from profile %s', profile_id)
        return set()
    return {endpoint for endpoint in gone if endpoint}


def migrate(repository):
    """Idempotent; safe to run from every web worker at startup."""
    from backend.utils.node_dns import CONFIG_KEY, DIRECT_DOH
    system = repository.get_system()
    agents = [agent for agent in system.get('agents', []) if isinstance(agent.get(FIELD), dict)]
    saved = system.get('system_config', {}).get(CONFIG_KEY)
    managed = isinstance(saved, dict) and saved.get('mode') == 'managed'
    if not agents and not managed:
        return False
    by_profile, endpoints, devices = {}, {}, []
    for agent in agents:
        for endpoint_id, record in agent[FIELD].items():
            if not isinstance(record, dict):
                continue
            endpoints[endpoint_id] = record.get('endpoint')
            if record.get('status') != 'removed' and isinstance(record.get('plugins'), list):
                by_profile.setdefault(record.get('profile_id') or agent.get('profile_id', 'default'), []).append(record)
                if record.get('installed') and agent.get('name') not in devices:
                    devices.append(agent.get('name') or agent['id'])
    gone = set()
    for profile_id, records in by_profile.items():
        gone |= _clean_profile(repository, profile_id, records)
    outcome = {}

    def update(current):
        for agent in current.get('agents', []):
            agent.pop(FIELD, None)
        target = current.setdefault('system_config', {})
        config = target.get(CONFIG_KEY)
        servers = None
        if isinstance(config, dict) and config.get('mode') == 'managed':
            binding = config.get('managed') if isinstance(config.get('managed'), dict) else {}
            endpoint = endpoints.get(binding.get('endpoint_id'))
            # A listener the user edited stays theirs and keeps serving node tests.
            servers = [endpoint] if endpoint and endpoint not in gone else list(DIRECT_DOH)
        elif isinstance(config, dict) and config.get('mode') == 'custom' and isinstance(config.get('servers'), list):
            kept = [server for server in config['servers'] if server not in gone]
            if kept != config['servers']:
                servers = kept or list(DIRECT_DOH)
        if servers is not None:
            target[CONFIG_KEY] = {'mode': 'custom', 'servers': servers,
                                  'bootstrap': config.get('bootstrap', []) if config.get('mode') == 'custom' else []}
            target['node_test_dns'] = servers
            outcome['servers'] = servers

    repository.update_system_transaction(update)
    from backend.common.config import reset_config_context
    reset_config_context()
    if outcome or gone:
        _notify(repository, outcome.get('servers'), sorted(gone), devices)
    _log.info('Removed managed node-test DNS: listeners %s, node-test DNS now %s',
              sorted(gone) or 'none', outcome.get('servers') or 'unchanged')
    return True


def _notify(repository, servers, gone, devices):
    from backend import notifications
    parts = ['节点测速的「OxiDNS 托管」模式已移除。']
    if servers:
        parts.append('测速 DNS 已改为自定义：' + '、'.join(servers) + '。')
    if gone:
        where = '「' + '」「'.join(devices) + '」' if devices else '设备'
        parts.append(f'原专用入口 {"、".join(gone)} 已从 OxiDNS 配置中去掉，{where}上的监听在下一次发布 OxiDNS 后关闭。')
    notifications.safely(notifications.record_notice, repository, NOTICE_KEY, '节点测速 DNS 已自动调整', ''.join(parts),
                         notifications.target('/nodes', '查看测速设置'))
