"""CPython references for the A07 review (a07_review_test.go). Synthetic values, no network.

    rt --back server/internal/agents/monitor/testdata/a07_review.json -- bash -lc \
      'cd server/internal/agents/monitor && PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a07_review.py \
       > testdata/a07_review.json'
"""
import copy
import json
from unittest import mock

from backend.agents import mihomo_panel as panel
from backend.agents import oxidns_cache as caches
from backend.agents import oxidns_monitor as monitor
from backend.agents import oxidns_rule_update as updates

OUT = {}


def outcome(fn):
    try:
        return {'result': fn()}
    except monitor.MonitorError as exc:
        return {'error': exc.code, 'status': exc.status, 'message': exc.message}
    except Exception as exc:  # noqa: BLE001 - the class is the reference
        return {'exception': type(exc).__name__}


class Manager:
    def __init__(self, agent):
        self.agent = agent

    def get_agent_by_id(self, agent_id, history=True):
        return copy.deepcopy(self.agent) if agent_id == self.agent['id'] else None


big = '1' * 4301
OUT['newer_release'] = [
    [a, b, outcome(lambda a=a, b=b: panel._newer_release(a, b))]
    for a, b in [('1.2.' + big, 'v1.2.3'), ('1.2.' + '1' * 4300, 'v1.2.3'), ('v1.2.3', '1.2.' + big),
                 ('x1.2.' + big, 'v1.2.3'), ('1.2.' + big, 'alpha-1'), ('v' + big + '.0.0', 'v' + big + '.0.0')]
]


def core_update(current, cached, force):
    """check_core_update with a fake Agent version answer and version.txt fetch: answer and fetch count."""
    panel._core_latest.clear()
    settings = {'rule_fetch_proxy': '', 'github_proxy_domain': '', 'server_domain': ''}
    if cached:
        panel._core_latest['release'] = (panel.time.monotonic(), 'v1.2.3', '', settings)
    fetched = []

    def fetch(channel, used):
        fetched.append(channel)
        return 'v1.2.3'

    agent = {'id': 'm', 'service_type': 'mihomo', 'version': '1.8.60-go', 'host': '127.0.0.1', 'port': 1, 'token': 't'}
    with mock.patch.object(panel, 'call', lambda *a, **k: {'success': True, 'data': {'version': current}}), \
            mock.patch.object(panel, '_download_settings', lambda: dict(settings)), \
            mock.patch.object(panel, '_latest_core_version', fetch):
        result = outcome(lambda: panel.check_core_update(Manager(agent), 'm', force=force))
    result['fetched'] = len(fetched)
    result['cached_after'] = list(panel._core_latest['release'][1:3]) if 'release' in panel._core_latest else None
    return result


OUT['core_update'] = [
    [current, cached, force, core_update(current, cached, force)]
    for current in ('1.2.' + big, '1.2.' + '1' * 4300, 'v1.2.4')
    for cached in (False, True) for force in (False, True)
]

cache_agent = {'id': 'dns', 'service_type': 'oxidns', 'version': '1.8.36-go', 'host': 'bad/host', 'port': 8082,
               'token': '令牌'}
OUT['cache_bad_host_token'] = outcome(lambda: caches.call(Manager(cache_agent), 'dns', 'GET'))

rule_agent = {'id': 'dns', 'service_type': 'oxidns', 'version': '1.8.36-go', 'host': '127.0.0.1', 'port': 8082,
              'token': 't'}
OUT['rule_update_ha'] = []
for field in ['x', ['x'], {'status': ['a']}, {'status': {'a': 1}}, {'status': 1}, {'status': None}, {'status': 'failed'},
              {}, '', 0, {'kind': 'rules'}]:
    agent = dict(rule_agent, oxidns_ha_publication=field)
    with mock.patch.object(updates.requests, 'Session', side_effect=RuntimeError('no network')):
        result = outcome(lambda: updates.call(Manager(agent), 'dns', 'POST'))
    OUT['rule_update_ha'].append([field, result])

print(json.dumps(OUT, ensure_ascii=False, sort_keys=True, indent=1))
