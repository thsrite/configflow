"""Public setup state machine with synthetic publication receipts and Agent HTTP.

No installation, forwarding, TUN or device command is executed. Publication
receipts are boundary fixtures, not claims that an actual release succeeded.
"""
from contextlib import contextmanager
from concurrent.futures import ThreadPoolExecutor
import copy
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import threading

from compare import database, differences, response_diffs
from harness import Pair, json_body, send
from seed import AGENT, PULL
from sources import Source
from subscriptions import Exchange
from cases.config_backup import message
from cases.setup import (CREATE, PID, PATH, ACTION, SID, NODE, configure, create,
                         call, row, session, Stable)
from cases.setup_prepare import PREPARE, check_plan
from cases.subscriptions import require, put_file

INSTALL = 'b' * 32
INSTALL_NAME = '合成安装🧪'
IDS = ('agent-synthetic', 'setup-oxidns')
TOKENS = (AGENT, AGENT + '-oxidns')
SERVICES = ('mihomo', 'oxidns')
INSTALL_FILE = 'gateway-installations/' + INSTALL + '.json'


def healthy_status(goal='router'):
    return {'success': True, 'supported': True, 'enabled_in_config': False,
            'ipv4_forwarding': {'current': '1', 'persisted': True},
            'tun': {'configured': True, 'present': True, 'gateway_marker': False, 'device': 'Meta'},
            'managed': {'active': goal == 'gateway', 'pending': False, 'healthy': True}}


class GatewayAgent:
    """Only the recorded status protocol; deliberately has no OS capabilities."""
    def __init__(self, value=None, status=200, *, bind_host='127.0.0.1'):
        self.value = healthy_status() if value is None else value
        self.status, self.calls, self.errors = status, [], []
        self.responses = {}
        self.before_reply = None
        self.expected_disconnect = False
        self.lock = threading.Lock()
        owner = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def request(self):
                raw = self.rfile.read(int(self.headers.get('Content-Length', 0)))
                with owner.lock:
                    owner.calls.append({'method': self.command, 'path': self.path,
                        'body_hex': raw.hex(), 'authorization': self.headers.get('Authorization', ''),
                        'accept': self.headers.get('Accept', ''), 'content_type': self.headers.get('Content-Type', '')})
                if owner.before_reply:
                    owner.before_reply()
                value = owner.responses.get(self.path, owner.value)
                content = value if isinstance(value, bytes) else json.dumps(value, ensure_ascii=False).encode()
                self.send_response(owner.status)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(content)))
                self.end_headers()
                try:
                    self.wfile.write(content)
                except (BrokenPipeError, ConnectionResetError):
                    if not owner.expected_disconnect:
                        owner.errors.append('panel disconnected before status response')

            do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = do_HEAD = request

        self.server = ThreadingHTTPServer((bind_host, 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.port = self.server.server_port

    def snapshot(self):
        with self.lock:
            return copy.deepcopy(self.calls)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()


class StatusExchange:
    def __init__(self, pair, agents, *, expected=1, readonly=False):
        self.pair, self.agents, self.expected = pair, agents, expected
        self.observed = [None, None]
        self.stable = Stable(pair) if readonly else None

    def before(self, index):
        self.offsets = [len(agent.snapshot()) for agent in self.agents]
        if self.stable:
            self.stable.before(index)

    def after(self, index, reply):
        self.observed[index] = [{'requests': agent.snapshot()[offset:], 'errors': list(agent.errors)}
                                for agent, offset in zip(self.agents, self.offsets)]
        if index == 0:
            expected = [{'method': 'GET', 'path': '/api/linux-gateway/status', 'body_hex': '',
                'authorization': 'Bearer ' + TOKENS[0], 'accept': 'application/json', 'content_type': ''}] * self.expected
            require(self.observed[0] == [{'requests': expected, 'errors': []}, {'requests': [], 'errors': []}],
                    'setup sends exactly the intended authenticated read-only status request to Mihomo and never to OxiDNS')
        if self.stable:
            self.stable.after(index, reply)

    def compare(self, policy):
        out = differences(*self.observed, 'setup.agent-status', policy)
        if self.stable:
            out += self.stable.compare(policy)
        return out


def refresh_heartbeats(pair, variant, version='1.8.60-go'):
    for index, ident in enumerate(IDS):
        pair.request('X08', 'POST /api/agents/<agent_id>/heartbeat', '/api/agents/' + ident + '/heartbeat',
            body=json_body({'version': version, 'service_status': 'running'}), auth=TOKENS[index],
            expect=200, writes=True, variant=variant + '/' + ident)


def publication(pair, agents, **changes):
    fields = database(pair.dirs[0])['fields']
    revision = fields['profile/' + PID + '/_revision']['value'] + fields['shared//_revision']['value']
    value = {'id': INSTALL, 'profile_id': PID, 'config_revision': revision, 'name': INSTALL_NAME,
        'network': {'address': '127.0.0.1'}, 'phase': 'published', 'created_at': '2026-01-01T00:00:00Z',
        'deployments': {service: {'deployment_id': 'synthetic-published-' + service, 'status': 'succeeded'} for service in SERVICES},
        'message': '合成已发布记录；不执行安装', 'server_url': 'http://panel.example', 'expires': 4102444800,
        'token': 'synthetic-installation-token', 'agent_ids': dict(zip(SERVICES, IDS)),
        'ports': {service: agent.port for service, agent in zip(SERVICES, agents)}, **changes}
    put_file(pair, INSTALL_FILE, value)
    for directory in pair.dirs:
        (directory / INSTALL_FILE).chmod(0o600)
    return value


@contextmanager
def prepared(binary, recorder, goal, variant, *, heartbeat=True, version='1.8.60-go', disabled=False, quiet_library=False,
             service_host='127.0.0.1'):
    bind = '127.0.0.1' if service_host == '127.0.0.1' else '0.0.0.0'
    with GatewayAgent(healthy_status(goal), bind_host=bind) as mihomo, GatewayAgent(bind_host=bind) as oxidns, Source(b'synthetic blocked upstream') as outbound:
        outbound.status = 503
        agents = (mihomo, oxidns)
        fixture = {'config_token': PULL, 'shared': {'nodes': [NODE]},
                   'setup_shadow_builtin_names': quiet_library,
                   'system_config': {'rule_fetch_proxy': outbound.url},
                   'agents': [{'id': ident, 'name': INSTALL_NAME + '-' + service, 'service_type': service,
                       'token': TOKENS[index], 'profile_id': 'default', 'host': service_host, 'port': agent.port,
                       'enabled': not (disabled and index == 0), 'version': version, 'deployment_method': 'shell',
                       'latest_deployment': {'deployment_id': 'synthetic-published-' + service, 'status': 'succeeded'}}
                       for index, (ident, service, agent) in enumerate(zip(IDS, SERVICES, agents))]}
        with Pair(binary, recorder, options=fixture) as pair:
            create(pair, 'S01' if goal == 'router' else 'S02', {**CREATE, 'goal': goal}, variant=variant + '/create')
            call(pair, 'R211-S03', ACTION, PATH + '/source', body={'node_ids': [NODE['id']]}, variant=variant + '/source')
            call(pair, 'R211-S04', ACTION, PATH + '/prepare', body=PREPARE, variant=variant + '/prepare',
                 check=lambda r: check_plan(pair, r, 'mihomo', 'proxy', full_tun=goal == 'router'),
                 interaction=Exchange(pair, outbound, expected=0))
            for index in range(2):
                pair.policy.times.add('db.fields.system//agents.value[' + str(index) + '].updated_at')
            call(pair, 'R121-S01', 'POST /api/profiles/<profile_id>/agents/rebind', '/api/profiles/default/agents/rebind',
                 body={'target_profile_id': PID, 'agent_ids': list(IDS)}, variant=variant + '/bind')
            publication(pair, agents, network={'address': service_host})
            if heartbeat:
                refresh_heartbeats(pair, variant, version)
            yield pair, agents, outbound


def checked(pair, reply, *, abandoned=False):
    value = reply.json()
    require({k: v for k, v in value.items() if k != 'default_profile'} == session(pair)
        and value['step'] == value['max_step'] == 4 and value['completed'] is False,
        'successful check persists the exact response and unlocks finish, without completing it')
    require(value['installation']['id'] == INSTALL
        and [value['installation'][service]['id'] for service in SERVICES] == list(IDS),
        'installation receipt binds both actual profile-scoped Agents')
    instructions = value['route_instructions']
    require(instructions == {'gateway': '127.0.0.1', 'dns': '127.0.0.1',
        'routes': [{'cidr': '198.18.0.0/16', 'sources': ['fake-ip'], 'requires_source_exclusion': False}],
        'warnings': [], 'incomplete': False}, 'check returns only the synthetic fake-ip route, with exact exclusion flag and no unobserved downloads')
    require(value.get('abandoned', False) is abandoned, 'check does not reopen an abandoned session')


def lifecycle(binary, recorder):
    for goal in ('router', 'gateway'):
        variant = 'T22/' + goal
        with prepared(binary, recorder, goal, variant) as (pair, agents, outbound):
            baseline = database(pair.dirs[0])
            for case, suffix in [('R211-S10', 'check'), ('T22', 'check-retry')]:
                call(pair, case, ACTION, PATH + '/check', body={}, variant=variant + '/' + suffix,
                     check=lambda r: (checked(pair, r), require(database(pair.dirs[0]) == baseline, 'installation status check does not recommit configuration')),
                     interaction=StatusExchange(pair, agents))
            body = {'ack_routes': True, 'ack_dns': True} if goal == 'router' else {}
            def complete(reply):
                value = reply.json()
                require(value['completed'] is True and value['step'] == value['max_step'] == 5
                    and value['router_verification'] == ('manual' if goal == 'router' else 'client_pending')
                    and value['default_profile']['is_default'] is False and database(pair.dirs[0]) == baseline,
                    'manual/gateway finish persists completion and leaves default/profile SQL unchanged')
            call(pair, 'R211-S11' if goal == 'router' else 'T22', ACTION, PATH + '/finish', body=body,
                 variant=variant + '/finish', check=complete, interaction=StatusExchange(pair, agents))
            call(pair, 'T22', ACTION, PATH + '/finish', body=body, variant=variant + '/finish-retry',
                 check=complete, interaction=StatusExchange(pair, agents))


def status_guards(binary, recorder):
    vectors = [('H988', {}, '请将 Mihomo Agent 更新到 1.8.40-go 或更新版本后再检查'),
        ('H989', {'ipv4_forwarding': {'current': '0', 'persisted': True}}, 'Linux IPv4 转发未开启，请在安装与检查中点击「开启并持久化 IPv4 转发」'),
        ('H990', {'ipv4_forwarding': {'current': '1', 'persisted': False}}, 'IPv4 转发尚未持久化，重启后可能失效；请在安装与检查中点击「开启并持久化 IPv4 转发」'),
        ('H991', {'managed': {'active': True}}, '设备上仍有 Linux 网关的转发记录，请先在配置生成页的 Linux 网关中撤销后再检查'),
        ('H991', {'managed': {'pending': True}}, '设备上仍有 Linux 网关的转发记录，请先在配置生成页的 Linux 网关中撤销后再检查'),
        ('H991', {'managed': {'error': 'synthetic'}}, '设备上仍有 Linux 网关的转发记录，请先在配置生成页的 Linux 网关中撤销后再检查'),
        ('H992', {'enabled_in_config': True}, '已发布的 Mihomo 仍是 Linux 网关配置，请重新发布'),
        ('H992', {'tun': {'gateway_marker': True}}, '已发布的 Mihomo 仍是 Linux 网关配置，请重新发布'),
        ('H993', {'tun': {'configured': False}}, '已发布的 Mihomo 未启用完整 TUN，请检查基础 YAML 后重新发布'),
        ('H994', {'tun': {'present': False, 'device': '合成TUN🧪'}}, 'Mihomo TUN（合成TUN🧪）未运行，请检查 Mihomo 日志')]
    with prepared(binary, recorder, 'router', 'full-tun-guards') as (pair, agents, outbound):
        for index, (helper, patch, text) in enumerate(vectors):
            value = healthy_status()
            if not patch:
                value = {}
            else:
                for key, item in patch.items():
                    if isinstance(item, dict):
                        value[key].update(item)
                    else:
                        value[key] = item
            agents[0].value = value
            for case in ('R211-F22', helper):
                call(pair, case, ACTION, PATH + '/check', body={}, expect=409, variant='status/' + str(index),
                     check=message(text), interaction=StatusExchange(pair, agents, readonly=True))
        agents[0].value = healthy_status()
        # check alone is explicitly read-only with respect to abandon/reopen.
        for directory in pair.dirs:
            path = directory / 'setup-sessions' / (SID + '.json')
            if not path.exists():
                # An unwired create has deliberately not run on Go. Do not
                # manufacture its missing result or unblock the dependency.
                continue
            stored = json.loads(path.read_text())
            stored['abandoned'] = True
            path.write_text(json.dumps(stored, ensure_ascii=False))
        call(pair, 'R211-B91', ACTION, PATH + '/check', body={}, variant='check-keeps-abandoned',
             check=lambda r: checked(pair, r, abandoned=True), interaction=StatusExchange(pair, agents))
    with prepared(binary, recorder, 'gateway', 'gateway-health-guards') as (pair, agents, outbound):
        for index, managed in enumerate([{}, {'active': True, 'pending': True, 'healthy': True},
                                        {'active': True, 'pending': False, 'healthy': False}]):
            agents[0].value = {**healthy_status('gateway'), 'managed': managed}
            for case in ('R211-F22', 'H999'):
                call(pair, case, ACTION, PATH + '/check', body={}, expect=409, variant='gateway-health/' + str(index),
                     check=message('Linux 转发尚未启用或健康状态未确认，请继续安装向导'),
                     interaction=StatusExchange(pair, agents, readonly=True))


GROUPS = {'setup-completion-lifecycle': lifecycle, 'setup-completion-status-guards': status_guards}


def publication_guards(binary, recorder):
    text = '请先在安装向导完成 Mihomo、OxiDNS 的发布与检查；配置变化后需重新发布'
    with prepared(binary, recorder, 'router', 'publication-guards') as (pair, agents, outbound):
        for index, patch in enumerate([None, {'phase': 'enrolled'}, {'name': 'different-target'},
                                      {'config_revision': -1}, {'deployments': {}}]):
            if patch is None:
                for directory in pair.dirs:
                    (directory / INSTALL_FILE).unlink()
            else:
                publication(pair, agents, **patch)
            for case in ('R211-F22', 'H995'):
                call(pair, case, ACTION, PATH + '/check', body={}, expect=409, variant='publication/' + str(index),
                     check=message(text), interaction=StatusExchange(pair, agents, expected=0, readonly=True))
        publication(pair, agents, agent_ids={'mihomo': IDS[0]})
        for case in ('R211-F22', 'H996'):
            call(pair, case, ACTION, PATH + '/check', body={}, expect=409, variant='publication/missing-core',
                 check=message('两个核心的设备尚未确认，请继续安装向导'),
                 interaction=StatusExchange(pair, agents, expected=0, readonly=True))
    for variant, kwargs, helper, text in [
        ('offline', {'heartbeat': False}, 'H997', '安装设备离线或已禁用，请检查连接后再检查'),
        ('disabled', {'disabled': True}, 'H997', '安装设备离线或已禁用，请检查连接后再检查'),
        ('old-agent', {'version': '1.8.24-go'}, 'H998', '请先将 Mihomo Agent 更新到 1.8.25-go 或更新版本。')]:
        with prepared(binary, recorder, 'router', 'publication/' + variant, **kwargs) as (pair, agents, outbound):
            for case in ('R211-F22', helper):
                call(pair, case, ACTION, PATH + '/check', body={}, expect=409, variant='publication/' + variant,
                     check=message(text), interaction=StatusExchange(pair, agents, expected=0, readonly=True))


def protocol_guards(binary, recorder):
    vectors = [(404, b'not-json', 409, '设备尚未提供网关接口，请更新到包含本功能的 1.8.25-go 或更新版本。'),
               (401, b'not-json', 502, 'Agent 认证失败，请检查设备注册状态。'),
               (403, b'not-json', 502, 'Agent 认证失败，请检查设备注册状态。'),
               (200, b'<invalid synthetic JSON>', 502, 'Agent 返回的网关数据无法解析，请刷新设备状态。'),
               (200, [], 502, 'Agent 返回的网关数据格式异常。'),
               (200, b'x' * (512 * 1024 + 1), 502, 'Agent 返回的网关数据过大。'),
               (500, {'message': '合成网关拒绝🧪'}, 502, '合成网关拒绝🧪'),
               (422, {'error': '合成参数错误🧪'}, 422, '合成参数错误🧪'),
               (409, {}, 409, '网关操作未完成，请刷新状态后查看检查结果。'),
               (400, {'message': '中' * 1001}, 400, '中' * 1000)]
    with prepared(binary, recorder, 'router', 'protocol-guards') as (pair, agents, outbound):
        for index, (status, value, expected, text) in enumerate(vectors):
            agents[0].status, agents[0].value = status, value
            for case in ('R211-F22', 'H998'):
                call(pair, case, ACTION, PATH + '/check', body={}, expect=expected, variant='protocol/' + str(index),
                     check=message(text), interaction=StatusExchange(pair, agents, readonly=True))


def finish_guards(binary, recorder):
    with prepared(binary, recorder, 'router', 'finish-guards') as (pair, agents, outbound):
        call(pair, 'R211-S10', ACTION, PATH + '/check', body={}, variant='finish-guards/checked',
             check=lambda r: checked(pair, r), interaction=StatusExchange(pair, agents))
        for index, body in enumerate([{}, {'ack_routes': True}, {'ack_dns': True}, {'ack_routes': 'true', 'ack_dns': True}]):
            for case in ('R211-F23', 'H1003'):
                call(pair, case, ACTION, PATH + '/finish', body=body, expect=409, variant='finish/ack/' + str(index),
                     check=message('请先按说明配置主路由的静态路由与 DNS，再确认已完成'),
                     interaction=StatusExchange(pair, agents, readonly=True))
        for index, value in enumerate([None, 1, 'true', []]):
            for case in ('R211-F23', 'H1000'):
                call(pair, case, ACTION, PATH + '/finish', body={'make_default': value}, expect=400,
                     variant='finish/make-default-type/' + str(index), check=message('请选择是否设为默认配置空间'),
                     interaction=StatusExchange(pair, agents, expected=0, readonly=True))
        for index, value in enumerate([None, '', 'invalid']):
            for case in ('R211-F23', 'H1008'):
                call(pair, case, ACTION, PATH + '/finish', body={'router_mode': value}, expect=400,
                     variant='finish/router-mode/' + str(index), check=message('请选择路由器配置方式'),
                     interaction=StatusExchange(pair, agents, readonly=True))
        for index, body in enumerate([{'router_mode': 'managed'}, {'router_mode': 'managed', 'router_id': 'missing-router'}]):
            for case in ('R211-F23', 'H1004'):
                call(pair, case, ACTION, PATH + '/finish', body=body, expect=409, variant='finish/missing-router/' + str(index),
                     check=message('请关联本次安装的 Mihomo 和 OxiDNS 设备，再预览并发布路由器配置'),
                     interaction=StatusExchange(pair, agents, readonly=True))
        before = database(pair.dirs[0])
        call(pair, 'R211-S11', ACTION, PATH + '/finish', body={'ack_routes': True, 'ack_dns': True, 'make_default': True},
             variant='finish/make-default', interaction=StatusExchange(pair, agents),
             check=lambda r: require(r.json()['completed'] is True and r.json()['previous_default'] == 'default'
                and r.json()['default_profile']['is_default'] is True
                and database(pair.dirs[0])['fields']['system//system_config']['value']['default_profile_id'] == PID
                and row(pair) == {key.split('/')[-1]: item['value'] for key, item in before['fields'].items() if key.startswith('profile/' + PID + '/')},
                'explicit make-default changes only default selection and setup receipt, leaving prepared profile fields exact'))


GROUPS.update({'setup-completion-publication-guards': publication_guards,
               'setup-completion-protocol-guards': protocol_guards,
               'setup-completion-finish-guards': finish_guards})


class EditDuringStatus(StatusExchange):
    """A second real HTTP request changes core config while check is in flight."""
    def __init__(self, pair, agents):
        super().__init__(pair, agents)
        self.edits = [None, None]

    def before(self, index):
        super().before(index)
        self.entered, self.release = threading.Event(), threading.Event()
        self.saved_session = session(self.pair, index=index)
        def gate():
            self.entered.set()
            if not self.release.wait(8):
                self.agents[0].errors.append('concurrent settings edit did not release Agent gate')
        self.agents[0].before_reply = gate
        value = copy.deepcopy(row(self.pair, index=index)['oxidns'])
        value['query_recorder']['enabled'] = not value['query_recorder']['enabled']
        def edit():
            try:
                if not self.entered.wait(8):
                    return None
                return send(self.pair.bases[index], 'POST', '/api/profiles/' + PID + '/oxidns/settings', json_body(value),
                    {'Authorization': 'Bearer ' + self.pair.tokens[index], 'Content-Type': 'application/json'})
            finally:
                self.release.set()
        self.pool = ThreadPoolExecutor(max_workers=1)
        self.pending = self.pool.submit(edit)

    def after(self, index, reply):
        self.edits[index] = self.pending.result(timeout=12)
        self.pool.shutdown(wait=True)
        self.agents[0].before_reply = None
        if index == 0:
            require(self.edits[0] is not None and self.edits[0].status == 200,
                    'public core edit completes while the original Agent status request is still held')
            require(session(self.pair) == self.saved_session,
                    'failed finish does not persist its in-memory completion or installation changes')
        super().after(index, reply)

    def compare(self, policy):
        out = super().compare(policy)
        if self.edits[1] is None:
            return out + differences(True, False, 'setup.concurrent-core-edit.completed', policy)
        return out + response_diffs(*self.edits, policy, method='POST')


def concurrent_core_edit(binary, recorder):
    for case in ('R211-F23', 'H1009'):
        with prepared(binary, recorder, 'router', 'core-edit/' + case) as (pair, agents, outbound):
            call(pair, 'R211-S10', ACTION, PATH + '/check', body={}, variant='core-edit/' + case + '/checked',
                 check=lambda r: checked(pair, r), interaction=StatusExchange(pair, agents))
            call(pair, case, ACTION, PATH + '/finish', body={'ack_routes': True, 'ack_dns': True}, expect=409,
                 variant='core-edit/while-Agent-status-held', check=message('核心配置在检查期间已变化，请重新发布后检查'),
                 interaction=EditDuringStatus(pair, agents))
            call(pair, 'H995', ACTION, PATH + '/check', body={}, expect=409, variant='core-edit/' + case + '/retry-needs-republish',
                 check=message('请先在安装向导完成 Mihomo、OxiDNS 的发布与检查；配置变化后需重新发布'),
                 interaction=StatusExchange(pair, agents, expected=0, readonly=True))


GROUPS['setup-completion-concurrent-core-edit'] = concurrent_core_edit


def attach_router_draft(binary, recorder):
    from cases.setup_lifecycle import ROUTER, capture_draft
    with prepared(binary, recorder, 'router', 'attach-draft') as (pair, agents, outbound):
        baseline = database(pair.dirs[0])
        # This endpoint is a sentinel: attaching the draft is a config write,
        # not permission to log into or publish to a router.
        router = {**ROUTER, 'endpoint': 'http://127.0.0.1:' + str(agents[1].port)}
        call(pair, 'R211-S08', ACTION, PATH + '/router-draft', body={'router': router},
             variant='attach-draft/private', capture=capture_draft(pair),
             check=lambda r: require(database(pair.dirs[0]) == baseline, 'draft stays private until actual fake-Agent check'),
             interaction=StatusExchange(pair, agents, expected=0))
        ident = session(pair)['router_draft']['id']
        def attached(reply):
            value, saved = reply.json(), row(pair)
            require(value['router_id'] == ident and value['router_mode'] == 'managed'
                and 'router_draft' not in value and 'router_draft' not in session(pair),
                'checked draft becomes one managed profile router and leaves no pending secret draft')
            actual = saved['routers']['items']
            require(len(actual) == 1 and actual[0]['id'] == ident
                and actual[0]['password'] == ROUTER['password']
                and actual[0]['proxy_gateway_agent_id'] == IDS[0] and actual[0]['dns_agent_id'] == IDS[1]
                and actual[0]['auto_rule_sync'] is False and saved['_revision'] == 2,
                'exactly one router attaches actual checked Agents, retains credential, leaves auto-sync off and advances one profile revision')
        call(pair, 'R211-S10', ACTION, PATH + '/check', body={}, variant='attach-draft/checked',
             check=attached, interaction=StatusExchange(pair, agents))
        baseline = database(pair.dirs[0])
        call(pair, 'R211-B04', ACTION, PATH + '/check', body={}, variant='router-only-revision-does-not-require-republish',
             check=lambda r: (attached(r), require(database(pair.dirs[0]) == baseline,
                  'receipt fingerprint permits router-only revision and retry never duplicates router or recommits profile')),
             interaction=StatusExchange(pair, agents))
        call(pair, 'R211-S11', ACTION, PATH + '/finish', body={'router_mode': 'manual', 'ack_routes': True, 'ack_dns': True},
             variant='attach-draft/manual-confirm-after-router-revision', interaction=StatusExchange(pair, agents),
             check=lambda r: require(r.json()['completed'] is True and r.json()['router_verification'] == 'manual'
                and database(pair.dirs[0]) == baseline, 'explicit manual confirmation finishes after router-only revision without device login'))


GROUPS['setup-completion-attach-router'] = attach_router_draft
