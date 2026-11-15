import json, sys, tempfile
import requests
from backend.agents.manager import AgentManager
from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository

def resp(body, status=200, ctype=None):
    r = requests.Response(); r.status_code = status; r._content = body; r._content_consumed = True
    if ctype: r.headers['Content-Type'] = ctype
    return r

repo = ProfileRepository(tempfile.mkdtemp())
config_module.set_repository(repo)
m = AgentManager(repo)
reg = m.register_agent({'name': 't', 'host': 'agent.test', 'port': 8080})
aid = reg['id']
agent = m.get_agent_by_id(aid)
out = {}
def run(name, fn, getter=None, poster=None):
    if getter: requests.get = getter
    if poster: requests.post = poster
    try:
        out[name] = {'result': fn()}
    except Exception as e:
        out[name] = {'error': type(e).__name__ + ': ' + str(e)}
seen = []
run('status_non_json', lambda: m.get_agent_status(aid), getter=lambda *a, **k: resp(b'not json'))
run('status_404', lambda: m.get_agent_status(aid), getter=lambda *a, **k: resp(b'{}', 404))
run('status_list', lambda: m.get_agent_status(aid), getter=lambda *a, **k: resp(b'[1]'))
run('sources_404', lambda: m.get_agent_log_sources(aid), getter=lambda *a, **k: resp(b'x', 404))
def g(url, **k):
    seen.append(url); return resp(b'{"success": true}')
run('logs_url', lambda: m.get_agent_logs(aid, 50, '/var/log/a b?.log', ''), getter=g)
run('logs_source', lambda: m.get_agent_logs(aid, 50, '/x', 'mihomo/日志 a'), getter=g)
out['urls'] = seen
run('logging_list', lambda: m.set_logging_config(aid, True), poster=lambda *a, **k: resp(b'[1]'))
run('logging_null', lambda: m.set_logging_config(aid, True), poster=lambda *a, **k: resp(b'null'))
run('logging_ok', lambda: m.set_logging_config(aid, True), poster=lambda *a, **k: resp(b'{"success": true, "x": 1}'))
out['logging_saved'] = repo.get_system()['agents'][0].get('logging_config')
repo.update_system_transaction(lambda s: s['agents'][0].update(logging_config=None))
run('logging_existing_none', lambda: m.set_logging_config(aid, False), poster=lambda *a, **k: resp(b'{"success": true}'))
repo.update_system_transaction(lambda s: s['agents'][0].update(logging_config=[1]))
run('logging_existing_list', lambda: m.set_logging_config(aid, False), poster=lambda *a, **k: resp(b'{"success": true}'))
run('logging_fail', lambda: m.set_logging_config(aid, False), poster=lambda *a, **k: resp(b'{"success": false}'))
run('uninstall_non_json', lambda: m.uninstall_agent(aid), poster=lambda *a, **k: resp(b'oops'))
run('update_500', lambda: m.update_agent_version(aid, '1', 'u'), poster=lambda *a, **k: resp(b'', 500))
run('json_404', lambda: m.probe_domains(agent, {}), poster=lambda *a, **k: resp(b'', 404))
run('json_list', lambda: m.probe_domains(agent, {}), poster=lambda *a, **k: resp(b'[1]'))
run('json_null', lambda: m.probe_domains(agent, {}), poster=lambda *a, **k: resp(b'null'))
run('json_fail_msg', lambda: m.probe_domains(agent, {}), poster=lambda *a, **k: resp(b'{"success": false, "message": "busy"}', 500))
run('json_fail_nomsg', lambda: m.probe_domains(agent, {}), poster=lambda *a, **k: resp(b'not json', 502))
run('region_409', lambda: m.region_check(agent, {}), poster=lambda *a, **k: resp(b'[]', 409))
run('region_targets', lambda: m.region_targets(agent), getter=lambda *a, **k: resp(b'{"success": true, "targets": null}'))
run('ha_404', lambda: m.oxidns_ha_status(agent), getter=lambda *a, **k: resp(b'', 404))
run('ha_list', lambda: m.oxidns_ha_status(agent), getter=lambda *a, **k: resp(b'[1]'))
run('ha_false', lambda: m.oxidns_ha_status(agent), getter=lambda *a, **k: resp(b'{"success": false, "error": "bad ' + agent['token'].encode() + b'"}'))
run('ha_handoff_bad', lambda: m.oxidns_ha_handoff(agent, 'put', 'x'))
print(json.dumps(out, ensure_ascii=False, indent=1).replace(agent['token'], '<TOKEN>'))
