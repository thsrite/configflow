"""Reference results of backend/agents/manager.py for the A01 review (CPython 3.11, requests 2.31).

Run on the test server from the repository root:

  PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/testdata/gen_a01_reference.py \
      > server/internal/agents/testdata/a01_reference.json

Every Agent is a loopback listener started here; DNS is disabled (only IP
literals, glibc's numeric forms and "localhost" resolve), so no request leaves
the machine. The Go side (a01_reference_test.go) rebuilds the same cases.
"""
import json
import os
import socket
import sys
import tempfile
import threading
import time
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import requests

from backend.agents import manager as manager_module
from backend.agents.manager import AgentManager, heartbeat_fresh
from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository

_real_getaddrinfo = socket.getaddrinfo


def _no_dns(host, port, family=0, type=0, proto=0, flags=0):
    try:
        return _real_getaddrinfo(host, port, family, type, proto, flags | socket.AI_NUMERICHOST)
    except socket.gaierror:
        if isinstance(host, str) and host.lower() == 'localhost':
            return _real_getaddrinfo('127.0.0.1', port, family, type, proto, flags)
        raise socket.gaierror(socket.EAI_NONAME, 'Name or service not known (DNS disabled)')


socket.getaddrinfo = _no_dns
seen = []


class Handler(BaseHTTPRequestHandler):
    def _answer(self):
        length = int(self.headers.get('Content-Length') or 0)
        body = self.rfile.read(length) if length else b''
        seen.append({'method': self.command, 'path': self.path, 'host': self.headers.get('Host'),
                     'authorization': self.headers.get('Authorization'), 'body': body.decode('latin-1'),
                     'content_type': self.headers.get('Content-Type')})
        data = b'{"success": true, "seen": true}'
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    do_GET = do_POST = do_PUT = do_DELETE = _answer

    def log_message(self, *args):
        pass


class Server6(ThreadingHTTPServer):
    address_family = socket.AF_INET6


def serve(server_class, address):
    server = server_class(address, Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    return server


v4 = serve(ThreadingHTTPServer, ('127.0.0.1', 0))
PORT = v4.server_address[1]
try:
    v6 = serve(Server6, ('::1', PORT))
except OSError:
    v6 = None

repo = ProfileRepository(tempfile.mkdtemp())
config_module.set_repository(repo)
m = AgentManager(repo)
out = {'port_placeholder': 'PORT'}

FULLWIDTH = str.maketrans('0123456789', '０１２３４５６７８９')
PORTS = {
    'int': lambda: PORT, 'str': lambda: str(PORT), 'float': lambda: float(PORT), 'space_before': lambda: ' %d' % PORT,
    'space_after': lambda: '%d ' % PORT, 'plus': lambda: '+%d' % PORT, 'zero_pad': lambda: '0%d' % PORT,
    'fullwidth': lambda: str(PORT).translate(FULLWIDTH), 'slash': lambda: '%d/x' % PORT, 'true': lambda: True,
    'none': lambda: None, 'empty': lambda: '', 'big': lambda: PORT + 65536, 'underscore': lambda: '%s_%s' % (str(PORT)[:2], str(PORT)[2:]),
}
HOSTS = ['127.0.0.1', 'localhost', 'LOCALHOST', '127.0.0.1.', '::1', '[::1]', '127.1', '0x7f.0.0.1', '2130706433',
         '127.0.0.1/x', '127.0.0.1?q=1', '127.0.0.1#frag', 'user@127.0.0.1', 'user:p%40ss@127.0.0.1',
         '127.0.0.1 ', ' 127.0.0.1', '127.0.0.1\t', '127.0.0.1\n', '', 'local host', '１２７.0.0.1', '127.0.0.1:1',
         '%31%32%37.0.0.1', 'localhost.', 'xn--', '127.0.0.256', '[127.0.0.1]', 'Localhost', 'host_name']
URL_CASES = [(h, 'int') for h in HOSTS] + [('127.0.0.1', p) for p in PORTS if p != 'int'] + [('::1', 'str'), ('localhost', 'float')]

url_results = []
for host, port_kind in URL_CASES:
    port = PORTS[port_kind]()
    repo.update_system_transaction(lambda s: s.update(agents=[]))
    aid = m.register_agent({'name': 'u', 'host': host, 'port': port})['id']
    seen.clear()
    entry = {'host': host, 'port': port_kind}
    try:
        result = m.get_agent_status(aid)
        message = result.get('message', '') if isinstance(result, dict) else ''
        if isinstance(message, str) and message.startswith('Connection error: '):
            entry['outcome'] = 'connection_error'
        else:
            entry['outcome'] = 'ok'
    except Exception as exc:  # not a RequestException: the route answers 500
        entry['outcome'] = 'exception'
        entry['error'] = type(exc).__name__ + ': ' + str(exc).replace(str(PORT), 'PORT')
    if seen:
        request = dict(seen[0])
        request['host'] = (request['host'] or '').replace(str(PORT), 'PORT')
        entry['request'] = {k: request[k] for k in ('method', 'path', 'host', 'authorization')}
    url_results.append(entry)
out['urls'] = url_results

# requests' json= refuses NaN/Infinity (InvalidJSONError, a RequestException).
repo.update_system_transaction(lambda s: s.update(agents=[]))
aid = m.register_agent({'name': 'j', 'host': '127.0.0.1', 'port': PORT})['id']
agent = m.get_agent_by_id(aid)
json_results = {}
for name, payload in (('nan', {'x': float('nan')}), ('inf', [float('inf')]), ('ok', {'x': 1.5, 'y': '中'})):
    seen.clear()
    try:
        json_results[name] = {'result': m.probe_domains(agent, payload)}
    except requests.RequestException as exc:
        json_results[name] = {'request_exception': type(exc).__name__, 'message': str(exc)}
    except Exception as exc:
        json_results[name] = {'exception': type(exc).__name__ + ': ' + str(exc)}
    if seen:
        json_results[name]['body'] = seen[0]['body']
        json_results[name]['content_type'] = seen[0]['content_type']
out['json_bodies'] = json_results

# record_deployment with values an Agent may answer.
record_results = []
STATUSES = [['x'], {'a': 1}, 5, None, True, 'succeeded', 'weird']
for stored_status in ('uploading', 'ready'):
    for options in ({}, {'activation_receipt': True}, {'upload_receipt': True}):
        for status in STATUSES:
            def setup(system):
                system['agents'] = [{
                    'id': 'a', 'name': 'a', 'token': 'tok', 'host': '127.0.0.1', 'port': 9, 'service_type': 'mihomo',
                    'profile_id': 'default', 'deployments': {'d': {'deployment_id': 'd', 'status': stored_status,
                                                                    'profile_id': 'default', 'service_type': 'mihomo'}},
                    'latest_deployment': {'deployment_id': 'd', 'status': stored_status}}]
            repo.update_system_transaction(setup)
            entry = {'stored': stored_status, 'options': options, 'status': status}
            try:
                result = m.record_deployment('a', 'd', {'status': status, 'message': 'm'}, **options)
                entry['result_status'] = result.get('status')
            except Exception as exc:
                entry['error'] = type(exc).__name__ + ': ' + str(exc)
            entry['saved_status'] = repo.get_system()['agents'][0]['deployments']['d'].get('status')
            record_results.append(entry)
out['record_statuses'] = record_results

# heartbeat_fresh: naive heartbeats compare wall clocks, across DST in New York.
os.environ['TZ'] = 'America/New_York'
time.tzset()
fresh = []
for heartbeat, now in (
        ('2026-03-08T01:59:00', '2026-03-08T03:00:30'),  # 61 s real, 3630 s wall
        ('2026-11-01T01:59:00', '2026-11-01T01:00:30'),  # wall clock went back
        ('2026-11-01T01:30:00', '2026-11-01T01:31:59.999999'),
        ('2026-11-01T01:30:00', '2026-11-01T01:32:00.000001'),
        ('2026-11-01T01:30:00', '2026-11-01T01:32:00'),
        ('2026-11-01T01:30:00+00:00', '2026-11-01T01:31:00'),  # aware vs naive: TypeError → stale
        ('2026-11-01T01:30:00Z', '2026-11-01T01:31:00'),
        ('2026-11-01', '2026-11-01T00:01:00'),
        ('2026-11-01T01:30:00.5', '2026-11-01T01:32:00.5'),
        ('0001-01-01T00:00:00', '0001-01-01T00:02:00'),
        ('9999-12-31T23:59:59', '0001-01-01T00:00:00')):
    fresh.append({'heartbeat': heartbeat, 'now': now,
                  'fresh': heartbeat_fresh({'last_heartbeat': heartbeat}, datetime.fromisoformat(now))})
out['heartbeat_fresh'] = fresh

json.dump(out, sys.stdout, ensure_ascii=False, indent=1, sort_keys=True)
print()
v4.shutdown()
if v6:
    v6.shutdown()
