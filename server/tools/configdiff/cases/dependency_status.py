"""Dependency discovery/cache through trusted local HTTPS and HTTP protocol fakes."""
import json

from compare import differences
from dependency_tls import GitHub, release_request, require_native_network
from harness import Pair
from sources import Source
from cases.setup import Stable
from cases.subscriptions import require


class DependencyRead(Stable):
    def __init__(self, pair, health, github, refresh, health_count=1):
        super().__init__(pair)
        self.health, self.github, self.refresh = health, github, refresh
        self.health_count = health_count
        self.observed = [None, None]

    def before(self, index):
        super().before(index)
        self.offsets = len(self.health.requests), len(self.github.requests)

    def after(self, index, reply):
        super().after(index, reply)
        # Health's default transport User-Agent has no declared API value.
        # Keep every method/path/auth/body and compare explicit GitHub headers.
        health = [{key: row[key] for key in ('method', 'path', 'authorization', 'body_hex', 'transfer_encoding')}
                  for row in self.health.requests[self.offsets[0]:]]
        self.observed[index] = {'health': health, 'github': self.github.requests[self.offsets[1]:]}
        expected = {'health': [{'method': 'GET', 'path': '/api/utils/env', 'authorization': '', 'body_hex': '', 'transfer_encoding': ''}] * self.health_count,
                    'github': release_request() if self.refresh else []}
        if index == 0:
            require(not differences(expected, self.observed[0]), 'dependency status uses exact local version GET and expected cached/forced TLS release query: '
                    + repr(self.observed[0]))

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'dependency.protocol', policy)


def status(binary, recorder):
    require_native_network()
    with Source(json.dumps({'data': {'version': '2.40.0'}}).encode()) as health, GitHub() as github:
        health.headers = {'Content-Type': 'application/json'}
        health.record_body = True
        environment = {'SUB_STORE_URL': health.url, 'REQUESTS_CA_BUNDLE': str(github.cert), 'SSL_CERT_FILE': str(github.cert),
                       'HTTP_PROXY': github.url, 'HTTPS_PROXY': github.url, 'NO_PROXY': '127.0.0.1,::1'}
        with Pair(binary, recorder, options={'environment': environment, 'system_config': {'rule_fetch_proxy': github.url}}) as pair:
            vectors = [('R198-S01', '', True), ('R198-S02', '?refresh=1', True),
                       ('R198-S03', '?refresh=true', True), ('R198-B02', '?refresh=TRUE', False),
                       ('R198-B01', '?refresh=1', True), ('R198-F08', '?refresh=1', True)]
            for case, query, refresh in vectors:
                if case == 'R198-B01':
                    github.release['tag_name'] = 'v2.40.0'
                if case == 'R198-F08':
                    github.status = 503
                def check(reply):
                    rows = reply.json()['dependencies']
                    require(len(rows) == 1 and rows[0]['key'] == 'sub-store', 'exact dependency identity is exposed')
                    value = rows[0]
                    require(value['current_version'] == '2.40.0' and value['running'] is True and value['updatable'] is False
                            and value['lifecycle']['managed'] is False and value['lifecycle']['mode'] == 'external',
                            'external service version and lifecycle cannot be confused with an installed local bundle')
                    require(value['latest_version'] == ('2.40.0' if case in ('R198-B01', 'R198-F08') else '2.42.1')
                            and value['has_update'] is (case not in ('R198-B01', 'R198-F08')), 'release cache and current/latest comparison stay exact')
                    require(bool(value['check_error']) is (case == 'R198-F08'), 'failed forced query retains prior successful release and exposes safe error')
                pair.request(case, 'GET /api/settings/dependencies', '/api/settings/dependencies' + query,
                    expect=200, writes=True, check=check, interaction=DependencyRead(pair, health, github, refresh),
                    variant='isolated-trusted-HTTPS-release-cache')


def builtin_status(binary, recorder):
    from dependency_control import managed
    for shape in ('offline-builtin-version', 'unreadable-version-file'):
        with GitHub() as github, managed(binary, recorder, github=github,
                health_status=503 if shape == 'offline-builtin-version' else 200,
                controller_status='STOPPED' if shape == 'offline-builtin-version' else 'RUNNING',
                unreadable_version=shape == 'unreadable-version-file') as (pair, health):
            def check(reply):
                value = reply.json()['dependencies'][0]
                require(value['current_version'] == '2.40.0' and value['updatable'] is True
                        and value['builtin_version'] == ('' if shape == 'unreadable-version-file' else '2.40.0'),
                        'builtin VERSION read errors fall back safely; offline service still reports installed bundle version')
                require(value['running'] is (shape == 'unreadable-version-file') and value['lifecycle']['managed'] is True
                        and value['lifecycle']['mode'] == 'on_demand' and value['runtime'].startswith('v'),
                        'managed lifecycle and actual Node version stay distinct from server readiness')
            pair.request('R198-S01' if shape == 'offline-builtin-version' else 'R198-F09', 'GET /api/settings/dependencies',
                expect=200, writes=True, check=check, interaction=DependencyRead(pair, health, github, True), variant=shape)


GROUPS = {'dependency-status-protocol': status, 'dependency-builtin-status': builtin_status}
