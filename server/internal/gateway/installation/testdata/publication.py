"""A22 public worker against real Python owners and loopback Agent receivers."""
import contextlib
import io
import json
import sys
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit
import requests
from backend.agents.manager import AgentManager
from backend.agents import gateway_installation, ipv6_verification
from backend.common import config
from backend.common.config_repository import ProfileRepository

request = json.load(sys.stdin)
# A23's external follow-up is outside A22, matching the Go unregistered owner.
ipv6_verification.enroll = lambda *args, **kwargs: None
original = requests.sessions.Session.request

def loopback(self, method, url, **kwargs):
    parsed = urlsplit(url)
    destination = request['destinations'].get(parsed.netloc)
    if not destination:
        raise AssertionError('unregistered network origin forbidden')
    target = urlsplit(destination)
    assert target.hostname in ('127.0.0.1', '::1', 'localhost')
    redirected = urlunsplit((target.scheme, target.netloc, parsed.path, parsed.query, parsed.fragment))
    return original(self, method, redirected, **kwargs)

requests.sessions.Session.request = loopback
with contextlib.redirect_stdout(io.StringIO()):
    repository = ProfileRepository(request['directory'])
    config.set_repository(repository)
    manager = AgentManager(repository)
    session = gateway_installation._read(repository, request['session_id'])
    for agent_id in session['agent_ids'].values():
        manager.update_heartbeat(agent_id, {'version': '1.8.60-go'})
    gateway_installation.run_publication(repository, manager, session['id'], pause=lambda _: None)
    final = gateway_installation._read(repository, session['id'])
    public = gateway_installation.get(repository, manager, session['id'])
print(json.dumps({'session': final, 'public': public}, ensure_ascii=False))
