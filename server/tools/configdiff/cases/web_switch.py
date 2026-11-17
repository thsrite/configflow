"""Successful native web-port changes versus a recorded Python Nginx peer."""
from contextlib import ExitStack
import copy
import json
from pathlib import Path
import re
from urllib.parse import urlsplit

from compare import database, differences, files
from harness import Pair, free_port, json_body, send
from sources import Source
from cases.subscriptions import require
from cases.web_listener import ROOT, TOKEN, ROUTE, require_listener_mount

HTML = b'synthetic ConfigFlow frontend\n'
TEMPLATE = ('events {}\nhttp { upstream configflow_backend { server 127.0.0.1:__CONFIGFLOW_BACKEND_PORT__; } '
    'server { __CONFIGFLOW_LISTEN__ location /_configflow/listener-health { '
    'return 200 "__CONFIGFLOW_LISTENER_TOKEN__"; } } }\n')


def rendered(port, token, backend):
    return TEMPLATE.replace('__CONFIGFLOW_BACKEND_PORT__', str(backend)).replace('__CONFIGFLOW_LISTENER_TOKEN__', token).replace(
        '__CONFIGFLOW_LISTEN__', 'listen 0.0.0.0:' + str(port) + ';\n        listen [::]:' + str(port) + ' ipv6only=on;')


class Peer:
    def __init__(self):
        self.stack, self.servers, self.events = ExitStack(), [], []
        self.backend = None

    def start(self, port, token):
        new = []
        for host in ('127.0.0.1', '::1'):
            source = Source(HTML, host=host, port=port)
            source.responses['/_configflow/listener-health'] = (200, token.encode(), {'Content-Type': 'text/plain'})
            self.stack.enter_context(source)
            new.append(source)
        old, self.servers = self.servers, new
        for source in old:
            source.__exit__()

    def __enter__(self):
        try:
            self.controller = self.stack.enter_context(Source(b'{"success":false}'))
            self.controller.record_body = True
            self.controller.before_reply = self.control
            self.initial_port = free_port()
            self.start(self.initial_port, TOKEN)
            return self
        except BaseException:
            self.stack.close()
            raise

    def control(self, path):
        row = self.controller.requests[-1]
        valid = path == '/_configdiff/nginx' and row['method'] == 'POST' and row['authorization'] == 'Bearer synthetic-nginx-peer'
        try:
            data = json.loads(bytes.fromhex(row['body_hex']))
            active = data['active']
            valid = (valid and set(data) == {'action', 'active', 'body'} and data['action'] in ('check', 'reload')
                and set(active) == {'port', 'ipv6', 'token'} and type(active['port']) is int and 1 <= active['port'] <= 65535
                and active['ipv6'] is True and bool(re.fullmatch('[0-9a-f]{32}', active['token']))
                and data['body'] == rendered(active['port'], active['token'], self.backend))
        except (ValueError, KeyError, TypeError):
            valid, data = False, {'action': 'invalid'}
        self.events.append({'action': data['action'], 'valid': bool(valid)})
        if valid and data['action'] == 'reload':
            self.start(active['port'], active['token'])
        self.controller.body = json.dumps({'success': bool(valid)}).encode()

    def __exit__(self, *_args):
        self.stack.close()


def health(port, host='127.0.0.1'):
    try:
        reply = send('http://' + host + ':' + str(port), 'GET', '/_configflow/listener-health', timeout=1)
        return reply.status, reply.body.strip()
    except OSError:
        return 0, b''


class Switch:
    def __init__(self, pair, peer, old, target):
        self.pair, self.peer, self.old, self.target = pair, peer, old, target
        self.observed = [None, None]

    def before(self, index):
        self.before_db, self.before_files = database(self.pair.dirs[index]), files(self.pair.dirs[index])
        self.before_health = health(self.old[index])
        self.offset = len(self.peer.events)

    def after(self, index, reply):
        current = health(self.target[index])
        ipv6 = health(self.target[index], '[::1]')
        try:
            html = send('http://127.0.0.1:' + str(self.target[index]), 'GET', '/', timeout=1)
            valid_html = html.status == 200 and html.body == HTML
        except OSError:
            valid_html = False
        events = self.peer.events[self.offset:]
        self.observed[index] = {'database_unchanged': database(self.pair.dirs[index]) == self.before_db,
            'new_health_200': current[0] == 200,
            'new_token_valid': bool(re.fullmatch(b'[0-9a-f]{32}', current[1])),
            'new_token_rotated': current[1] != self.before_health[1],
            'dual_stack_same_token': current == ipv6, 'html_exact': valid_html,
            'old_port_closed': health(self.old[index])[0] == 0,
            'controller_sequence_valid': events == ([{'action': 'check', 'valid': True}, {'action': 'reload', 'valid': True}] if index == 0 else [])}
        if index == 0:
            require(all(self.observed[0].values()), 'successful switch persists exact state, rotates health token, serves both stacks and releases old port: '
                    + repr(self.observed[0]))
            expected = copy.deepcopy(self.before_files)
            expected['web-listener.json'] = {'mode': 0o600, 'content': {'port': self.target[0]}}
            require(not differences(expected, files(self.pair.dirs[0])), 'only deployment listener state changes; all other files stay exact')
            first, body = (ROOT / 'nginx.conf').read_text().split('\n', 1)
            active = json.loads(first.removeprefix('# configflow-web-listener '))
            require(active == {'port': self.target[0], 'ipv6': True, 'token': current[1].decode()}
                    and body == rendered(self.target[0], current[1].decode(), self.peer.backend),
                    'Python-generated config corresponds exactly to requested port, assigned upstream and served new health capability')

    def compare(self, policy):
        return differences(*self.observed, 'listener.switch', policy)


def success(binary, recorder):
    require_listener_mount()
    with Peer() as peer:
        go_port = free_port()
        target = free_port(), free_port()
        old = peer.initial_port, go_port
        pair = Pair(binary, recorder, options={'web_frontend': True, 'web_listener_ports': old,
            'environment': {'CONFIGDIFF_NGINX_CONTROL': peer.controller.url + '/_configdiff/nginx'}})
        pair.port_leases.extend((peer.initial_port, go_port, *target))
        try:
            with pair:
                peer.backend = urlsplit(pair.bases[0]).port
                (ROOT / 'configflow-nginx.conf.template').write_text(TEMPLATE)
                (ROOT / 'nginx.conf').write_text('# configflow-web-listener ' + json.dumps({
                    'port': old[0], 'ipv6': True, 'token': TOKEN}) + '\n' + rendered(old[0], TOKEN, peer.backend))
                pair.policy.allocated_ports['body.port'] = target
                pair.policy.allocated_ports['files.web-listener.json.content.port'] = target
                pair.request('R206-S01', 'POST ' + ROUTE, body='{}', bodies=[json_body({'port': port}) for port in target],
                    expect=200, writes=True, interaction=Switch(pair, peer, old, target),
                    variant='native-frontend-and-recorded-nginx-peer-successful-switch',
                    check=lambda reply: require(reply.json() == {'success': True, 'supported': True, 'port': target[0],
                        'editable': True, 'ipv6': True, 'source': 'saved'}, 'public switch response agrees with saved new listener'))
        finally:
            (ROOT / 'configflow-nginx.conf.template').unlink(missing_ok=True)
            (ROOT / 'nginx.conf').unlink(missing_ok=True)


GROUPS = {'web-listener-complete-switch': success}
