"""Successful native web-port changes versus a recorded Python Nginx peer."""
from contextlib import ExitStack
import copy
import json
import os
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


class Rollback(Switch):
    def before(self, index):
        folder = self.pair.dirs[index]
        (folder / 'web-listener.lock').touch(mode=0o600, exist_ok=True)
        super().before(index)
        self.before_config = (ROOT / 'nginx.conf').read_bytes()
        folder.chmod(0o500)
        self.active = not os.access(folder, os.W_OK)
        if index == 0:
            require(self.active, 'real data directory prevents creating an atomic listener state file')

    def after(self, index, reply):
        folder = self.pair.dirs[index]
        try:
            retained = folder.stat().st_mode & 0o777 == 0o500
            same_files = files(folder) == self.before_files
            same_db = database(folder) == self.before_db
        finally:
            folder.chmod(0o700)
        old = health(self.old[index])
        events = self.peer.events[self.offset:]
        self.observed[index] = {'real_fault_active': self.active, 'readonly_mode_retained': retained,
            'database_unchanged': same_db, 'all_files_unchanged': same_files,
            'old_health_and_capability_unchanged': old == self.before_health and old[0] == 200,
            'old_dual_stack_same_capability': health(self.old[index], '[::1]') == old,
            'new_port_closed': health(self.target[index])[0] == 0 and health(self.target[index], '[::1]')[0] == 0,
            'controller_rollback_valid': events == ([{'action': 'check', 'valid': True},
                {'action': 'reload', 'valid': True}, {'action': 'reload', 'valid': True}] if index == 0 else []),
            'python_config_restored': (ROOT / 'nginx.conf').read_bytes() == self.before_config,
            'clear_Chinese_failure': reply.json().get('success') is False
                and bool(re.search('[\u4e00-\u9fff]', reply.json().get('message', '')))}
        if index == 0:
            require(all(self.observed[0].values()), 'failed final state save restores the original complete deployment: ' + repr(self.observed[0]))


def success(binary, recorder):
    switch(binary, recorder)


def save_failure(binary, recorder):
    switch(binary, recorder, readonly=True)


def switch(binary, recorder, *, readonly=False, targets=None):
    require_listener_mount()
    with Peer() as peer:
        go_port = free_port()
        target = targets or (free_port(), free_port())
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
                pair.policy.allocated_ports['files.web-listener.json.content.port'] = old if readonly else target
                if readonly:
                    pair.request('R206-F20', 'POST ' + ROUTE, body='{}', bodies=[json_body({'port': port}) for port in target],
                        expect=503, writes=True, interaction=Rollback(pair, peer, old, target),
                        variant='real-state-save-EACCES-restores-old-listener-and-all-storage',
                        check=lambda reply: require(reply.json() == {'success': False, 'message': '保存 Web 端口失败，已恢复原配置'},
                            'actual final state save failure has the precise public rollback message'))
                else:
                    pair.request('R206-S01', 'POST ' + ROUTE, body='{}', bodies=[json_body({'port': port}) for port in target],
                        expect=200, writes=True, interaction=Switch(pair, peer, old, target),
                        variant='native-frontend-and-recorded-nginx-peer-successful-switch' + ('/boundary-' + '-'.join(map(str, target)) if targets else ''),
                        check=lambda reply: require(reply.json() == {'success': True, 'supported': True, 'port': target[0],
                            'editable': True, 'ipv6': True, 'source': 'saved'}, 'public switch response agrees with saved new listener'))
                    if targets:
                        from cases.web_listener import Readonly
                        pair.request('R205-B01', 'GET ' + ROUTE, expect=200, writes=True,
                            interaction=Readonly(pair, target),
                            check=lambda reply: require(reply.json() == {'supported': True, 'port': target[0],
                                'editable': True, 'ipv6': True, 'source': 'saved'},
                                'actual live listener reports exact boundary port after a successful public switch'),
                            variant='complete-frontend-actual-boundary-port/' + '-'.join(map(str, target)))
        finally:
            for directory in pair.dirs:
                if directory.exists():
                    directory.chmod(0o700)
            (ROOT / 'configflow-nginx.conf.template').unlink(missing_ok=True)
            (ROOT / 'nginx.conf').unlink(missing_ok=True)


GROUPS = {'web-listener-complete-switch': success, 'web-listener-real-save-failure': save_failure}


def boundary_ports(binary, recorder):
    import os
    import subprocess
    require_listener_mount()
    require(os.geteuid() != 0, 'public servers remain unprivileged')
    key = 'net.ipv4.ip_unprivileged_port_start'
    original = subprocess.run(['sysctl', '-n', key], check=True, capture_output=True, text=True).stdout.strip()
    require(original.isascii() and original.isdecimal(), 'private-netns low-port setting can be restored')
    try:
        subprocess.run(['sudo', 'sysctl', '-w', key + '=0'], check=True, capture_output=True)
        for target in ((1, 65535), (65535, 1)):
            switch(binary, recorder, targets=target)
    finally:
        subprocess.run(['sudo', 'sysctl', '-w', key + '=' + original], check=True, capture_output=True)


GROUPS['web-listener-real-live-port-one-and-65535-status'] = boundary_ports
