"""C16 reference corpus from CPython (synthetic values only; no network, no real probe).

rt --back server/internal/health/testdata/corpus.json -- bash -lc \
  'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/health/testdata/gen.py'
"""
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime
from types import SimpleNamespace

from backend.utils import health_check as hc
from backend.utils import node_dns as dns
from backend.utils import node_latency as latency
from backend.utils import region_check as region
from backend.utils import domain_probe as probe

rows = []


def call(fn, name, *args, **kwargs):
    try:
        out = fn(*args, **kwargs)
        rows.append({'fn': name, 'args': list(args), 'kwargs': kwargs, 'out': out})
    except Exception as exc:  # noqa: BLE001
        rows.append({'fn': name, 'args': list(args), 'kwargs': kwargs,
                     'error': type(exc).__name__, 'message': str(exc)})


# ---------- validate_health ----------
values = [None, [], 'x', {}, {'enable': True}, {'enable': 1}, {'lazy': 'false'}, {'lazy': False},
          {'interval': 0}, {'interval': 1}, {'interval': 86400}, {'interval': 86401}, {'interval': True},
          {'interval': 60.0}, {'interval': 10 ** 30}, {'timeout': 600000}, {'timeout': 600001}, {'timeout': -1},
          {'tolerance': 0}, {'tolerance': -1}, {'tolerance': 600000}, {'unknown': 1},
          {'expected-status': '204'}, {'expected-status': '200-299/304'}, {'expected-status': '299-200'},
          {'expected-status': '099'}, {'expected-status': '600'}, {'expected-status': '204\n'},
          {'expected-status': '204/'}, {'expected-status': '२०४'}, {'expected-status': 204},
          {'expected-status': '100-599/200/300-301'}, {'expected-status': '204,abc'}]
urls = ['https://www.gstatic.com/generate_204', 'http://probe.test/x', 'ftp://probe.test', 'http://@example.test',
        'http://example.test:0', 'http://example.test:65535', 'http://example.test:65536', 'http://example.test:',
        'http://user:pw@example.test', 'http://example.test/#frag', 'http://example.test/#', 'https://',
        'http:///path', 'https://[::1]/x', 'https://[::1/x', 'https://exa mple.test', 'https://example.test\t',
        'https://ex ample.test', 'https://ex\x7fample.test', 'HTTPS://EXAMPLE.TEST', 'https://例子.测试/',
        'https://example.test:８０', 'https://ｅxample.test／x', 'https://[v1.x]/', 'http://a:b', 'http://:80',
        'https://example.test:00080', 'https://example.test:+80', 'https://xn--fsq.test', 'http://[fe80::1%25eth0]/']
for url in urls:
    values.append({'url': url})
    values.append({'url': url, 'interval': 60})
values.append({'url': 5})
values.append({'url': None})
for value in values:
    call(lambda v: hc.validate_health(v, provider=False), 'validate_health_group', value)
    call(lambda v: hc.validate_health(v, provider=True), 'validate_health_provider', value)

for source in [{}, {'health_check_url': 'http://old.test/check'}, {'health_check_url': ''}, {'health_check_url': 5},
               {'health_check': {}}, {'health_check': None}]:
    s = json.loads(json.dumps(source))
    hc.initialize_provider_health(s)
    rows.append({'fn': 'initialize_provider_health', 'args': [source], 'out': s})
for group in [{}, {'type': 'url-test'}, {'type': 'fallback', 'url': 'x', 'lazy': True}, {'type': 'select'},
              {'type': 'load-balance', 'tolerance': 3}, {'type': ['url-test']}, {'type': 'url-test', 'tolerance': None}]:
    g = json.loads(json.dumps(group))
    try:
        hc.initialize_group_health(g)
        rows.append({'fn': 'initialize_group_health', 'args': [group], 'out': g})
    except Exception as exc:
        rows.append({'fn': 'initialize_group_health', 'args': [group], 'error': type(exc).__name__, 'message': str(exc)})
for source in [{'health_check': {}}, {'health_check': {'enable': False, 'interval': 61}},
               {'health_check': {'x': 1}}, {}, {'health_check': {'lazy': 0}}]:
    call(hc.provider_health_check, 'provider_health_check', source)

# ---------- node_dns ----------
servers = ['192.0.2.53', ' 192.0.2.53 ', '192.0.2.53:53', '192.0.2.53:053', '192.0.2.53:5353', '192.0.2.53:0',
           '192.0.2.53:65535', '192.0.2.53:65536', '192.0.2.53:', '192.0.2.53:abc', '192.0.2.53:+53', '192.0.2.53:５３',
           '192.0.2.053', '0.0.0.0', '224.0.0.1', '239.255.255.255', '255.255.255.255', '127.0.0.1:5353', '::1',
           '1.2.3', '1.2.3.4.5', '１.2.3.4', 'dns.example', '', '   ', '　192.0.2.1　', None, 5, ['x'],
           'https://DNS.EXAMPLE', 'https://192.0.2.1:443/dns-query', 'tls://DNS.EXAMPLE:853', 'tls://192.0.2.1:8853',
           'https://user:password@dns.example/dns-query', 'https://dns.example/dns-query?', 'https://dns.example/dns-query#',
           'https://dns.example:0', 'https://dns.example:', 'https://dns.example:65536', 'https://dns.example/a\nb',
           'https://dns.example/dns%zz', 'https://dns.example/dns%2', 'https://dns.example/dns%2F', 'https://dns.example/%',
           'https://dns.example/dns\x7f', 'https://dns.example\\evil', 'tls://dns.example/', 'http://192.0.2.53',
           'udp://192.0.2.53', 'https://[::1]/dns-query', 'https://0.0.0.0/dns-query', 'https://224.0.0.1/dns-query',
           'tls://bad_host', 'tls://dns.example..', 'tls://dns.example.', 'https://1.2.3.4./x', 'https://例子.测试',
           'https://ＤＮＳ.example', 'https://straße.example', 'https://' + 'a' * 64 + '.example',
           'https://' + ('a' * 60 + '.') * 5 + 'com', 'https://-bad.example', 'https://dns.example:8443/q',
           'https://dns.example:443', 'tls://dns.example:853', 'tls://dns.example:00853', 'https://dns.example/',
           'https://dns.example' + '/x' * 1100, 'HTTPS://Dns.Example/Path', 'https://@dns.example',
           'https://dns.example:+443', 'https://xn--fsq.example', 'https://xn--zz.example', 'https://[fe80::1%25eth0]/',
           'tls://192.0.2.1', 'https://192.0.2.1', 'https://01.2.3.4/']
for item in servers:
    call(lambda v: dns.normalize_dns_servers([v]), 'normalize_dns_servers', item)
    call(lambda v: dns.normalize_dns_servers([v], bootstrap=True), 'normalize_bootstrap', item)
for value in [None, [], ['192.0.2.1'] * 5, ['192.0.2.1', '192.0.2.1:53', '192.0.2.2'], 'x', {'a': 1},
              ['192.0.2.1'] * 4, ['tls://dns.example', 'TLS://DNS.EXAMPLE:853']]:
    call(lambda v: dns.normalize_dns_servers(v), 'normalize_dns_servers_list', value)
for value in [None, {}, {'mode': 'system'}, {'mode': 'system', 'servers': ['bad']}, {'mode': 'managed'},
              {'mode': 'custom'}, {'mode': 'custom', 'servers': []}, {'mode': 'custom', 'servers': ['192.0.2.1']},
              {'mode': 'custom', 'servers': ['tls://dns.example']},
              {'mode': 'custom', 'servers': ['tls://dns.example'], 'bootstrap': ['192.0.2.53:53']},
              {'mode': 'custom', 'servers': ['tls://dns.example'], 'bootstrap': ['tls://dns.example']},
              {'mode': 'custom', 'servers': ['https://1.2.3.4./q'], 'bootstrap': []},
              {'mode': 'custom', 'servers': None}, {'mode': 'custom', 'servers': ['192.0.2.1'], 'bootstrap': None},
              {'mode': ['custom']}, [], 'system']:
    call(dns.normalize_config, 'normalize_config', value)

# ---------- check_settings / _run_probe output parsing (fake subprocess) ----------
latency._probe_binary = lambda: '/test/node-probe'


def fake_run(stdout, code=0, stderr=b''):
    def run(command, **kwargs):
        if isinstance(stdout, bytes) or isinstance(stderr, bytes):
            out = stdout if isinstance(stdout, bytes) else stdout.encode()
            err = stderr if isinstance(stderr, bytes) else stderr.encode()
            out.decode('utf-8')  # raise like text=True
            err.decode('utf-8')
            return SimpleNamespace(returncode=code, stdout=out.decode(), stderr=err.decode())
        return SimpleNamespace(returncode=code, stdout=stdout, stderr=stderr)
    return run


check_outputs = [
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['192.0.2.1', '2001:db8::1'], 'latency_ms': 12}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': []}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['198.19.0.1']}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['198.20.0.1', '::ffff:198.18.0.1']}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['invalid']}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': [True]}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['192.0.2.1'], 'latency_ms': True}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['192.0.2.1'], 'latency_ms': -1}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['192.0.2.1'], 'latency_ms': 0}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['192.0.2.1'], 'latency_ms': 1.0}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['192.0.2.1'], 'latency_ms': 10 ** 30}},
    {'version': 4, 'dns_check': {'success': False, 'addresses': [], 'error_code': 'dns_fake_ip', 'error': 'raw'}},
    {'version': 4, 'dns_check': {'success': False, 'addresses': [], 'error_code': 'unknown'}},
    {'version': 4, 'dns_check': {'success': False, 'addresses': [], 'error_code': None}},
    {'version': 4, 'dns_check': {'success': False, 'addresses': [], 'error_code': ['x']}},
    {'version': 4, 'dns_check': {'success': False, 'addresses': [], 'error_code': 5}},
    {'version': 4, 'dns_check': {'success': False, 'addresses': ['192.0.2.1']}},
    {'version': 4, 'dns_check': {'success': 1, 'addresses': []}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': '192.0.2.1'}},
    {'version': 4, 'dns_check': []},
    {'version': 4, 'dns_check': None},
    {'version': 4},
    {'version': 3, 'dns_check': {}},
    {'version': 4.0, 'dns_check': {'success': False, 'addresses': []}},
    {'version': True, 'dns_check': {}},
    {'dns_check': {}},
    [], 'x', 5, None,
    {'version': 4, 'dns_check': {'success': True, 'addresses': [
        '2001:DB8:0:0:1:0:0:1', '::ffff:1.2.3.4', '::1.2.3.4', '0:0:0:0:0:0:0:0', '1::', 'fe80::1%eth0',
        'fe80::1%', '2001:db8::0:1', '2001:0db8::1', '1:0:0:2:0:0:0:3', '1:2:3:4:5:6:7::', '::2:3:4:5:6:7:8',
        '192.0.2.1', '192.0.2.1', '01.2.3.4']}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['1:0:0:2:0:0:0:3', 'fe80::1%a%b']}},
    {'version': 4, 'dns_check': {'success': True, 'addresses': ['::ffff:1.2.3.4', '1:2:3:4:5:6:7:8', 'FE80::A']}},
]
raw_outputs = [('not json', 0), ('', 0), ('{"version": 4, "dns_check": {"success": true, "addresses": ["192.0.2.1"]}}', 1),
               ('{"version": 4, "dns_check": {"success": true, "addresses": ["192.0.2.1"]}}', 3),
               ('{"version": 4, "dns_check": {"success": true, "addresses": ["192.0.2.1"]}}', -9),
               ('﻿{"version": 4}', 0), ('{"version": NaN}', 0)]
for output in check_outputs:
    raw_outputs.append((json.dumps(output), 0))
for stdout, code in raw_outputs:
    dns.subprocess.run = fake_run(stdout, code)
    try:
        out = dns.check_settings({'mode': 'system'})
        rows.append({'fn': 'check_settings', 'stdout': stdout, 'code': code, 'out': out})
    except Exception as exc:
        rows.append({'fn': 'check_settings', 'stdout': stdout, 'code': code, 'error': type(exc).__name__,
                     'message': str(exc)})
for stdout, stderr in [(b'\xff', b''), (b'ok\xe4\xb8', b''), (b'\xe4\x41', b''), (b'\xe4\xb8\x41', b''),
                       (b'{}', b'\xed\xa0\x80'), (b'ab\xf0\x9f\x98', b''), (b'\xf4\x90\x80\x80', b''), (b'\xc0\xaf', b''),
                       (b'\xe0\x80', b''), (b'\xe0', b''), (b'x\xf0\x90', b'')]:
    dns.subprocess.run = fake_run(stdout, 0, stderr)
    try:
        dns.check_settings({'mode': 'system'})
        rows.append({'fn': 'decode', 'stdout_hex': stdout.hex(), 'stderr_hex': stderr.hex(), 'out': None})
    except Exception as exc:
        rows.append({'fn': 'decode', 'stdout_hex': stdout.hex(), 'stderr_hex': stderr.hex(),
                     'error': type(exc).__name__, 'message': str(exc)})

N = '["node","a"]'
M = '["node","b"]'
probe_outputs = [
    {'version': 4, 'results': [{'name': N, 'latency': 99, 'first_latency': 250}, {'name': M, 'latency': None, 'error': 'x'}]},
    {'version': 4, 'results': [{'name': N, 'latency': 1}, {'name': M, 'latency': 0, 'error': 'x'}]},
    {'version': 4, 'results': [{'name': N, 'latency': True}, {'name': M, 'latency': 2}]},
    {'version': 4, 'results': [{'name': N, 'latency': 2.0}, {'name': M, 'latency': 2}]},
    {'version': 4, 'results': [{'name': N, 'latency': None}, {'name': M, 'latency': 2}]},
    {'version': 4, 'results': [{'name': N, 'latency': None, 'error': ''}, {'name': M, 'latency': 2}]},
    {'version': 4, 'results': [{'name': N, 'latency': 3}]},
    {'version': 4, 'results': [{'name': N, 'latency': 3}, {'name': M, 'latency': 2}, {'name': 'x', 'latency': 2}]},
    {'version': 4, 'results': [{'name': N, 'latency': 3}, {'name': M, 'latency': 2}, {'name': N, 'latency': 5}]},
    {'version': 4, 'results': [{'name': N, 'latency': 3}, {'name': M}]},
    {'version': 4, 'results': [{'name': N, 'latency': 3}, {'name': [M], 'latency': 2}]},
    {'version': 4, 'results': [{'name': N, 'latency': 3}, {'name': 5, 'latency': 2}]},
    {'version': 4, 'results': [{'name': N, 'latency': 3}, 'x']},
    {'version': 4, 'results': [{'name': N, 'latency': 10 ** 20}, {'name': M, 'latency': -5, 'error': 'e'}]},
    {'version': 4, 'results': {}}, {'version': 4}, {'version': 2, 'results': []}, {'results': []}, [], None,
]
for output in probe_outputs:
    latency.subprocess.run = fake_run(json.dumps(output), 0)
    proxies = [{'name': N, 'type': 'http'}, {'name': M, 'type': 'http'}]
    try:
        out = latency._run_probe(proxies, {'url': 'u', 'timeout_ms': 5000, 'dns_servers': [], 'dns_bootstrap': [],
                                           'unified_delay': True})
        rows.append({'fn': 'run_probe', 'stdout': json.dumps(output), 'out': out})
    except Exception as exc:
        rows.append({'fn': 'run_probe', 'stdout': json.dumps(output), 'error': type(exc).__name__, 'message': str(exc)})
for code in [3, 1, 2, -9, 255]:
    latency.subprocess.run = fake_run('', code)
    try:
        latency._run_probe([{'name': N}], {'timeout_ms': 5000})
    except Exception as exc:
        rows.append({'fn': 'run_probe_code', 'code': code, 'error': type(exc).__name__, 'message': str(exc)})

# ---------- settings() env ----------
for env in [{}, {'CONFIGFLOW_NODE_TEST_URL': 'file:///etc/passwd'}, {'CONFIGFLOW_NODE_TEST_URL': 'https://user:secret@example.com'},
            {'CONFIGFLOW_NODE_TEST_URL': 'not-a-url'}, {'CONFIGFLOW_NODE_TEST_URL': '  https://t.example/x  '},
            {'CONFIGFLOW_NODE_TEST_URL': 'https://@t.example/'}, {'CONFIGFLOW_NODE_TEST_URL': 'https://:@t.example/'},
            {'CONFIGFLOW_NODE_TEST_URL': 'https://t.example:99999/'}, {'CONFIGFLOW_NODE_TEST_URL': 'https://[::1/'},
            {'CONFIGFLOW_NODE_TEST_TIMEOUT_MS': '499'}, {'CONFIGFLOW_NODE_TEST_TIMEOUT_MS': '500'},
            {'CONFIGFLOW_NODE_TEST_TIMEOUT_MS': '10000'}, {'CONFIGFLOW_NODE_TEST_TIMEOUT_MS': '10001'},
            {'CONFIGFLOW_NODE_TEST_TIMEOUT_MS': ' 5_000 '}, {'CONFIGFLOW_NODE_TEST_TIMEOUT_MS': '５０００'},
            {'CONFIGFLOW_NODE_TEST_TIMEOUT_MS': '5000.0'}, {'CONFIGFLOW_NODE_TEST_TIMEOUT_MS': '+800'},
            {'CONFIGFLOW_NODE_TEST_UNIFIED_DELAY': '0'}, {'CONFIGFLOW_NODE_TEST_UNIFIED_DELAY': 'FALSE'},
            {'CONFIGFLOW_NODE_TEST_UNIFIED_DELAY': 'no'}, {'CONFIGFLOW_NODE_TEST_UNIFIED_DELAY': ''},
            {'CONFIGFLOW_NODE_TEST_UNIFIED_DELAY': 'FALSİ'}]:
    saved = dict(os.environ)
    os.environ.update(env)
    latency.effective_settings = lambda: {'dns_servers': [], 'dns_bootstrap': []}
    try:
        rows.append({'fn': 'settings', 'env': env, 'out': latency.settings()})
    except Exception as exc:
        rows.append({'fn': 'settings', 'env': env, 'error': type(exc).__name__, 'message': str(exc)})
    os.environ.clear()
    os.environ.update(saved)

# ---------- region check ----------
def res(request_id, status=200, final_url=None, redirects=None, markers=None, error=''):
    return {'id': request_id, 'status': status, 'final_url': final_url or f'https://x.test/{request_id}',
            'redirects': redirects or [], 'markers': markers or {}, 'error': error, 'elapsed_ms': 10}


for name in ['🇺🇸 美国 01', 'HK-IEPL-02', 'RUSSIA 01', 'Tokyo 03', 'hk', 'xHK', 'HKx', 'hongkong', 'Hong Kong',
             'HongKong', 'UK London', 'TÜRKIYE', 'Türkiye', 'türkiye', 'CAN', 'ca1', '印度', 'India', 'malaysia',
             '', None, 5, ['HK'], 'ＨＫ', 'İN', 'ın', 'Kus', 'KR', 'kr', 'korea', 'america', 'AMERICA', 'Singapore',
             'sg-01', '新加坡', '洛杉矶', 'unitedstates', 'United  States', 'United\tStates', '_HK_', '1HK2', 'HKK']:
    call(region.region_of_name, 'region_of_name', name)
for code in ['HK', 'XX', '', None, 0, 'hk', ['HK']]:
    call(region.region_label, 'region_label', code)

eval_cases = {
    'openai': [
        [res('openai-compliance', markers={'consent': 'cookie_consent_required'}), res('openai-ios', 403, markers={'dc': '"type":"dc"'}), res('openai-trace', markers={'loc': 'US'})],
        [res('openai-compliance', markers={'unsupported': 'unsupported_country'}), res('openai-ios')],
        [res('openai-compliance', markers={'consent': 'x'}), res('openai-ios', markers={'vpn': 'VPN'})],
        [res('openai-compliance'), res('openai-ios')],
        [res('openai-compliance', 200.0, markers={'consent': ''})],
        [res('openai-compliance', 200, markers={'consent': None})],
        [res('openai-compliance', error='tls', markers={'consent': 'x'})],
        [], [{'id': 'openai-compliance', 'markers': None}], [{'id': 'openai-ios', 'markers': ['vpn']}],
        [{'id': 'openai-compliance'}], ['x'], [{'no': 'id'}], [{'id': ['x']}], [{'id': 5, 'status': 200}],
        [res('openai-compliance', markers={'b': 1, 'a': 2, '中': 3})],
        [{'id': 'openai-compliance', 'status': 200, 'markers': 'consent'}],
    ],
    'claude': [
        [res('claude-web', final_url='https://www.anthropic.com/app-unavailable-in-region', redirects=['https://claude.ai/']), res('claude-trace', markers={'loc': 'HK'})],
        [res('claude-web')], [res('claude-web', 403)], [res('claude-web', error='timeout')], [res('claude-web', 500)],
        [res('claude-web', 200, error='tls')], [res('claude-web', redirects=['https://x/app-unavailable-in-region'])],
        [{'id': 'claude-web', 'status': 200, 'redirects': 'app-unavailable-in-region'}],
        [{'id': 'claude-web', 'status': 200, 'redirects': None}], [{'id': 'claude-web', 'status': 200, 'redirects': [5]}],
        [{'id': 'claude-web', 'status': 200, 'final_url': 5}], [{'id': 'claude-web', 'status': 200, 'final_url': ['app-unavailable-in-region']}],
    ],
    'netflix': [
        [res('netflix-licensed-1', final_url='https://www.netflix.com/hk-en/title/81280792', redirects=['https://www.netflix.com/title/81280792']), res('netflix-licensed-2', 404), res('netflix-original')],
        [res('netflix-licensed-1', 404), res('netflix-licensed-2', 404), res('netflix-original', final_url='https://www.netflix.com/title/80018499')],
        [res('netflix-licensed-1', 403), res('netflix-licensed-2', 403), res('netflix-original', 403)],
        [res('netflix-licensed-1', error='timeout')], [res('netflix-licensed-1', 404), res('netflix-licensed-2', 403)],
        [res('netflix-licensed-1', 302, final_url='https://www.netflix.com/jp/title/1')],
        [res('netflix-licensed-1', 200, final_url='https://www.netflix.com/JP/title/1')],
        [res('netflix-licensed-1', 200, final_url='https://[bad/title/1')],
        [res('netflix-licensed-1', 200, final_url='https://www.netflix.com/title/1'), res('netflix-original', 200, final_url='https://www.netflix.com/us-es/title/1')],
        [{'id': 'netflix-licensed-1', 'status': 200, 'final_url': 7}],
    ],
    'youtube': [
        [res('youtube-premium', markers={'gl': 'US'})], [res('youtube-premium', markers={'blocked': 'x', 'gl': 'HK'})],
        [res('youtube-premium', markers={'gl': 'CN'})], [res('youtube-premium')], [res('youtube-premium', markers={'gl': 5})],
        [res('youtube-premium', error='e', markers={'gl': 'US'})],
    ],
}
for service_id, cases in eval_cases.items():
    for case in cases:
        def evaluate(c, sid=service_id):
            return region.SERVICES_BY_ID[sid]['evaluate']({item['id']: item for item in c})
        call(evaluate, 'evaluate_' + service_id, case)
        call(region.evaluate_services, 'evaluate_services', case)
        call(region.exit_info, 'exit_info', case)
call(region.exit_info, 'exit_info', [res('exit', markers={'loc': 'US', 'ip': '1.2.3.4'}), 'junk'])
call(region.exit_info, 'exit_info', [res('exit', error='down')])
rows.append({'fn': 'service_requests', 'out': region.service_requests()})
rows.append({'fn': 'public_services', 'out': region.public_services()})
rows.append({'fn': 'domain_request', 'out': region.domain_request('domain-1', 'https://a.test/')})

for result, url in [(res('d', 451), 'https://shop.test/'),
                    (res('d', 403, markers={'block': 'not available in your country'}), 'https://shop.test/'),
                    (res('d', 200, final_url='https://shop.test/unavailable-region'), 'https://shop.test/'),
                    (res('d', 200, final_url='https://shop.test/unavailable-region'), 'https://shop.test/region/'),
                    (res('d', 200, final_url='https://shop.test/'), 'https://shop.test/'),
                    (res('d', 403, final_url='https://shop.test/'), 'https://shop.test/'),
                    (res('d', error='timeout'), 'https://shop.test/'), (None, 'x'), ({}, 'x'),
                    (res('d', 500), 'x'), (res('d', 0), 'x'), (res('d', None), 'x'), (res('d', True), 'x'),
                    (res('d', 404.5), 'x'), (res('d', 'x'), 'x'), (res('d', 200, final_url='https://s/geoblock'), 'x'),
                    (res('d', 200, final_url='https://s/GEO-BLOCK'), 'x'), (res('d', 200, final_url='https://s/counKtry'), 'x'),
                    (res('d', 200, final_url='https://s/countrİ'), 'x'),
                    (res('d', 200, markers={'block': 'x'}), 'x'), (res('d', 451.0), 'x'), (res('d', 200, final_url='https://[x/region'), 'x'),
                    ({'id': 'd', 'status': 200, 'final_url': 5}, 'x'), ('str', 'x'), ({'error': 'e'}, 'x'),
                    (res('d', 200, final_url='https://s/region'), 'https://[bad/')]:
    call(region.classify_domain_result, 'classify_domain_result', result, url)
for states in [['restricted', 'ok'], ['restricted', 'restricted'], ['ok', 'denied'], ['error', 'error'], [],
               ['denied'], ['restricted', 'error'], ['ok']]:
    call(region.summarize_domain, 'summarize_domain', {f't{i}': {'state': s} for i, s in enumerate(states)})
targets = [
    {'name': 'DIRECT', 'type': 'Direct', 'alive': True}, {'name': 'GLOBAL', 'type': 'Selector', 'alive': True},
    {'name': 'ConfigFlow-Region-Probe', 'type': 'Selector', 'alive': True}, {'name': '自动选择', 'type': 'URLTest', 'alive': True},
    {'name': '🚀 节点选择', 'type': 'Selector', 'alive': True}, {'name': '🇭🇰 香港 01', 'type': 'Shadowsocks', 'alive': False},
    {'name': '🇭🇰 香港 02', 'type': 'Shadowsocks', 'alive': True}, {'name': '🇺🇸 美国 01', 'type': 'Vmess', 'alive': True},
    {'name': '🇺🇸 美国 02', 'type': 'Vmess', 'alive': True}, {'name': '未知节点', 'type': 'Trojan', 'alive': True},
]
for preferred in ['🚀 节点选择', None, '自动选择', 'missing']:
    call(region.pick_targets, 'pick_targets', targets, preferred)
many = [{'name': f'G{i} 香港', 'type': 'Selector'} for i in range(6)] + \
       [{'name': f'{rule[1]} {i}', 'type': 'Trojan'} for i, rule in enumerate(region.REGION_RULES)] + [{'name': 'x', 'type': 'Dns'}]
call(region.pick_targets, 'pick_targets', many, 'G5 香港')
call(region.pick_targets, 'pick_targets', list(reversed(many)), None)
for bad in [[{'type': 'Direct'}], [{'name': ['x']}], [{'name': 'a', 'type': ['Selector']}], ['x'],
            [{'name': 5, 'type': 'Trojan'}], [{'name': 'HK', 'alive': 0}], [{'name': 'HK', 'alive': None}],
            [{'name': None, 'type': 'Selector'}], [{'name': 'DIRECT'}]]:
    call(region.pick_targets, 'pick_targets', bad, None)

# ---------- domain probe ----------
def stats(ok, total, delays=None):
    return {'ok': ok, 'total': total, 'delays': delays if delays is not None else [100] * ok,
            'errors': {'timeout': total - ok} if total > ok else {}}


judge_cases = [
    (stats(0, 2), stats(2, 2), {}), (stats(0, 2), stats(2, 2), {'passive_failing': True}), (stats(0, 3), stats(2, 3), {}),
    (stats(0, 2), stats(2, 2), {'direct_healthy': False}), (stats(2, 2), stats(0, 2), {}),
    (stats(2, 2), stats(0, 2), {'proxy_healthy': False}), (stats(2, 2, [100, 120]), stats(2, 2, [300, 300]), {}),
    (stats(2, 2, [900, 900]), stats(2, 2, [200, 200]), {}), (stats(0, 2), stats(0, 2), {}),
    (stats(0, 2), stats(0, 2), {'proxy_healthy': False}), (stats(0, 2), stats(0, 2), {'direct_healthy': False}),
    (stats(1, 3), stats(1, 3), {}), (stats(1, 1), {'ok': 0, 'total': 0, 'delays': [], 'errors': {}}, {}),
    (stats(1, 1), {'ok': 0, 'total': 1, 'delays': [], 'errors': {'no_path': 1}}, {}), (None, None, {}),
    (stats(1, 2, [1, 2]), stats(2, 2, [1.5, 2.5]), {}), (stats(2, 2, [1, 4]), stats(2, 2, [3]), {}),
    (stats(2, 2, [2.5, 2.5]), stats(2, 2, [3.5]), {}), (stats(2, 2, []), stats(2, 2), {}),
    (stats(1, 1), stats(1, 1), {}), (stats(0, 1), stats(1, 1), {}), (stats(1, 1), stats(0, 1), {}),
    ({'ok': True, 'total': True}, {'ok': 1, 'total': 2.0}, {}), ({'total': 2}, stats(2, 2), {}),
    ({'ok': 'x', 'total': 2}, stats(2, 2), {}), ({'ok': 1, 'total': 'x'}, stats(2, 2), {}),
    (stats(2, 2), {'ok': 2, 'total': 2, 'errors': None}, {}), (stats(10 ** 20 - 1, 2 * 10 ** 20, [1]), stats(1, 1), {}),
    (stats(2 ** 60 - 1, 2 ** 61, [1]), stats(1, 1), {}), (stats(2, 2, [10 ** 18, 10 ** 18 + 1]), stats(2, 2, [1]), {}),
    (stats(3, 3, [5, 1, 3]), stats(3, 3, [2, 2, 2]), {}), (stats(2, 2, [float('nan'), 1]), stats(2, 2, [1]), {}),
    (stats(2, 2, ['a', 'b']), stats(2, 2, [1]), {}), ({'ok': 2, 'total': 2, 'delays': [1, 3]}, {'ok': 2, 'total': 2, 'delays': [1]}, {}),
    (stats(1, 2, [7]), stats(1, 2, [5]), {}), ({'ok': 0, 'total': 2}, {'ok': 2, 'total': 2}, {}),
    ({'ok': 2, 'total': True}, stats(2, 2), {}),
]
for direct, prox, kwargs in judge_cases:
    call(lambda d, p, **k: probe.judge(d, p, **k), 'judge', direct, prox, **kwargs)
for host, port in [('a.com', 443), ('a.com', 0), ('a.com', 80), ('a.com', 8443), ('a.com', None), ('a.com', 80.0),
                   ('a.com', 443.0), ('a.com', True), ('a.com', '8080')]:
    call(probe.probe_url, 'probe_url', host, port)
for hosts in [
    [{'probe': {'host': 'a', 'verdict': 'both_ok', 'target': 'direct', 'confidence': 60, 'checked_at': '2'}},
     {'probe': {'host': 'b', 'verdict': 'needs_proxy', 'target': 'proxy', 'confidence': 85, 'checked_at': '1'}}, {'probe': None}],
    [{'probe': {'host': 'c', 'verdict': 'unreachable', 'target': None, 'confidence': 0, 'checked_at': '3'}}],
    [{'probe': None}], [], [{'probe': {'host': 'a', 'target': 'x', 'confidence': 5}}, {'probe': {'host': 'b', 'target': 'y', 'confidence': 5}}],
    [{'probe': {'host': 'a', 'target': 'x'}}], [{'probe': {'host': 'a'}}, {'probe': {'host': 'b', 'checked_at': '1'}}],
    [{'probe': {'host': 'a', 'target': 'x', 'confidence': 5.5}}, {'probe': {'host': 'b', 'target': 'y', 'confidence': 6}}],
    [{'probe': {'host': 'a', 'target': 'x', 'confidence': 'a'}}, {'probe': {'host': 'b', 'target': 'y', 'confidence': 6}}],
    ['x'], [{'probe': 'p'}],
]:
    call(probe.domain_suggestion, 'domain_suggestion', hosts)
call(probe.path_healthy, 'path_healthy', stats(1, 2))
call(probe.path_healthy, 'path_healthy', stats(0, 2))
call(probe.path_healthy, 'path_healthy', None)

# ---------- stores (file bytes) ----------
with tempfile.TemporaryDirectory() as tmp:
    store = probe.ProbeStore(tmp)
    now = datetime(2026, 10, 10, 12, 0, 0)
    steps = [
        {'old.com': {'checked_at': '2026-08-01T00:00:00', 'verdict': 'unreachable'}},
        {'new.com': {'checked_at': '2026-10-10T00:00:00', 'verdict': 'needs_proxy'}, '中.com': {'checked_at': '2026-09-10T12:00:00'}},
        {'edge.com': {'checked_at': '2026-09-10T11:59:59'}, 'none.com': {}},
        {'x.com': {'checked_at': '2026-10-01T00:00:00', 'nested': {'a': [1, 2.50, None]}}},
    ]
    for step in steps:
        store.save('a/1', step, now=now)
        rows.append({'fn': 'probe_store_save', 'results': step, 'file': open(store._path('a/1'), encoding='utf-8').read(),
                     'load': store.load('a/1', now=now)})
    rstore = region.RegionStore(tmp)
    def mutate_a(data):
        data['services'] = {'x': 1}
        data['domains']['a.test'] = {'checked_at': '2026-10-01T00:00:00'}
        data['domains']['old.test'] = {'checked_at': '2026-09-10T11:59:59'}
        data['domains']['edge.test'] = {'checked_at': '2026-09-10T12:00:00'}
        data['websites']['w2'] = {'checked_at': '2026-10-02T00:00:00'}
        data['websites']['w1'] = {'checked_at': '2026-10-01T00:00:00'}
        data['websites']['w0'] = {'checked_at': '2026-10-02T00:00:00'}
        data['websites']['wx'] = {}
    rstore.update('r1', mutate_a, now=now)
    rows.append({'fn': 'region_store', 'step': 'a', 'file': open(rstore._path('r1'), encoding='utf-8').read(),
                 'load': rstore.load('r1')})
    with open(os.path.join(tmp, 'r2.json'), 'w') as f:
        f.write('{"domains": [], "websites": 5, "keep": "x"}')
    rows.append({'fn': 'region_store', 'step': 'load-odd', 'load': rstore.load('r2')})
    with open(os.path.join(tmp, 'p2.json'), 'w') as f:
        f.write('{"hosts": [], "other": 1}')
    rows.append({'fn': 'probe_store_load_odd', 'load': store.load('p2', now=now)})

out = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'corpus.json')
with open(out, 'w', encoding='utf-8') as f:
    json.dump({'python': sys.version.split()[0], 'rows': rows}, f, ensure_ascii=False, indent=1, default=repr)
print(len(rows), 'rows')
