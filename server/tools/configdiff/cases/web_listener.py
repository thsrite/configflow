"""Full native Go frontend versus Flask listener API with a synthetic web peer.

Real Nginx overlay is O9/O14; these reads/no-op/pre-bind failures execute no CLI.
"""
from contextlib import contextmanager
import json
import os
from pathlib import Path
import re
import subprocess

from compare import differences
from dependency_tls import require_native_network
from harness import Pair, DesignError, free_port, json_body, send
from sources import Source
from cases.setup import Stable
from cases.subscriptions import require

ROOT = Path('/etc/nginx')
TOKEN = '1' * 32
ROUTE = '/api/settings/web-port'


class Readonly(Stable):
    def __init__(self, pair, ports, occupied=None):
        super().__init__(pair)
        self.ports, self.occupied = ports, occupied
        self.observed = [None, None]

    def health(self, index):
        response = send('http://127.0.0.1:' + str(self.ports[index]), 'GET', '/_configflow/listener-health', timeout=1)
        return response.status, response.body.strip()

    def before(self, index):
        super().before(index)
        self.initial_health = self.health(index)
        self.offset = len(self.occupied.requests) if self.occupied else 0
        self.config = (ROOT / 'nginx.conf').read_bytes()

    def after(self, index, reply):
        super().after(index, reply)
        try:
            status, token = self.health(index)
            health = {'status': status, 'valid_capability': bool(re.fullmatch(b'[0-9a-f]{32}', token)),
                      'same_capability': (status, token) == self.initial_health}
        except OSError as error:
            health = {'error': type(error).__name__}
        self.observed[index] = {'old_listener': health,
            'python_config_unchanged': (ROOT / 'nginx.conf').read_bytes() == self.config,
            'occupied_requests': self.occupied.requests[self.offset:] if self.occupied else []}
        if index == 0:
            require(self.observed[0] == {'old_listener': {'status': 200, 'valid_capability': True, 'same_capability': True},
                'python_config_unchanged': True, 'occupied_requests': []},
                'read/no-op/rejection preserves configuration and reachable original frontend')

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'listener.readonly', policy)


def require_listener_mount():
    require_native_network()
    source = os.environ.get('CONFIGDIFF_LISTENER_SOURCE', '')
    mounted = any(len(parts) > 4 and parts[4] == '/etc/nginx'
        for parts in (line.split() for line in Path('/proc/self/mountinfo').read_text().splitlines()))
    if (os.environ.get('CONFIGDIFF_ISOLATED_LISTENER') != '1' or not mounted
            or not re.fullmatch('/tmp/configdiff-listener[.][A-Za-z0-9]{6}', source) or not ROOT.samefile(source)):
        raise DesignError('listener API fixture requires private /etc/nginx from native_network.sh')


@contextmanager
def deployment(binary, recorder, *, environment=False):
    require_listener_mount()
    with Source(TOKEN.encode()) as frontend:
        port = free_port()
        ports = (frontend.server.server_port, port)
        pair = Pair(binary, recorder, options={'web_frontend': True, 'web_listener_ports': ports,
            'web_port_environment': environment})
        pair.port_leases.append(port)
        try:
            with pair:
                (ROOT / 'configflow-nginx.conf.template').write_text('synthetic template; no Nginx command is invoked\n')
                (ROOT / 'nginx.conf').write_text('# configflow-web-listener ' + json.dumps({
                    'port': ports[0], 'ipv6': True, 'token': TOKEN}) + '\n')
                pair.policy.allocated_ports['body.port'] = ports
                pair.policy.allocated_ports['files.web-listener.json.content.port'] = ports
                yield pair, ports
        finally:
            (ROOT / 'configflow-nginx.conf.template').unlink(missing_ok=True)
            (ROOT / 'nginx.conf').unlink(missing_ok=True)


def supported(binary, recorder):
    with deployment(binary, recorder) as (pair, ports):
        def check(reply):
            require(reply.json() == {'supported': True, 'port': ports[0], 'editable': True,
                    'ipv6': True, 'source': 'saved'}, 'supported saved deployment exposes complete exact status')
        pair.request('R205-S01', 'GET ' + ROUTE, expect=200, writes=True, check=check,
            interaction=Readonly(pair, ports), variant='complete-native-frontend-with-synthetic-Flask-peer')
        pair.request('R206-S02', 'POST ' + ROUTE, body='{}', bodies=[json_body({'port': port}) for port in ports],
            expect=200, writes=True, interaction=Readonly(pair, ports), variant='complete-frontend-same-port-noop',
            check=lambda reply: require(reply.json() == {'success': True, 'supported': True, 'port': ports[0],
                'editable': True, 'ipv6': True, 'source': 'saved'}, 'same port keeps capabilities and configuration without reload'))


def occupied(binary, recorder):
    with Source(b'synthetic occupied service') as occupied, deployment(binary, recorder) as (pair, ports):
        port = occupied.server.server_port
        pair.request('R206-F14', 'POST ' + ROUTE, body=json_body({'port': port}), expect=409, writes=True,
            interaction=Readonly(pair, ports, occupied), variant='complete-frontend-occupied-port-preserves-old-listener',
            check=lambda reply: require(reply.json() == {'success': False,
                'message': '端口 ' + str(port) + ' 已被占用，请选择其他端口'}, 'occupied bind gives exact public conflict'))


GROUPS = {'web-listener-complete-readonly': supported, 'web-listener-complete-occupied': occupied}


def environment_locked(binary, recorder):
    with deployment(binary, recorder, environment=True) as (pair, ports):
        expected = '端口由 CONFIGFLOW_WEB_PORT 指定；移除该环境变量并重建容器后可在此修改'
        def status(reply):
            require(reply.json() == {'supported': True, 'port': ports[0], 'editable': False,
                'ipv6': True, 'source': 'environment', 'message': expected},
                'real full frontend honors environment precedence and advertises the exact immutable setting')
        pair.request('R205-S01', 'GET ' + ROUTE, expect=200, writes=True, check=status,
            interaction=Readonly(pair, ports), variant='complete-frontend-environment-port-is-readonly')
        target = free_port()
        pair.port_leases.append(target)
        pair.request('H1110', 'POST ' + ROUTE, body=json_body({'port': target}), expect=409, writes=True,
            interaction=Readonly(pair, ports),
            check=lambda reply: require(reply.json() == {'success': False, 'message': expected},
                'changing environment-owned port fails clearly and preserves old live listener'),
            variant='complete-native-frontend-refuses-environment-owned-port-change')


GROUPS['web-listener-real-environment-port-cannot-change'] = environment_locked


def privileged_bind_denied(binary, recorder):
    # ip_unprivileged_port_start is per-netns. Refuse to change it unless the
    # existing native wrapper has proved this is our isolated loopback namespace.
    require_listener_mount()
    require(os.geteuid() != 0, 'public servers run as the original unprivileged test user')
    key = 'net.ipv4.ip_unprivileged_port_start'
    original = subprocess.run(['sysctl', '-n', key], check=True, capture_output=True, text=True).stdout.strip()
    require(original.isascii() and original.isdecimal(), 'original private-netns sysctl is a restorable numeric value')
    try:
        subprocess.run(['sudo', 'sysctl', '-w', key + '=1024'], check=True, capture_output=True)
        with deployment(binary, recorder) as (pair, ports):
            require(subprocess.run(['sysctl', '-n', key], check=True, capture_output=True, text=True).stdout.strip() == '1024',
                'the real private kernel enforces privileged low ports')
            pair.request('R206-B01', 'POST ' + ROUTE, body=json_body({'port': 1}), expect=409, writes=True,
                interaction=Readonly(pair, ports),
                check=lambda reply: require(reply.json() == {'success': False,
                    'message': '无法监听端口 1，请检查权限和网络配置'}, 'actual kernel EACCES keeps the exact public Chinese bind error'),
                variant='complete-frontend-real-low-port-EACCES')
    finally:
        subprocess.run(['sudo', 'sysctl', '-w', key + '=' + original], check=True, capture_output=True)


GROUPS['web-listener-real-unprivileged-low-port-bind-failure'] = privileged_bind_denied
