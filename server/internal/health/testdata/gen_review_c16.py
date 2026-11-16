"""Review corpus of C16 (adversarial review, 2026-11-16): edge inputs the
Python suite does not reach, called through the c16_record recorder so the
rows have the recorded form replayed by recorded_test.go (TestReviewCorpus).

Run on the test server from the repository root:
  PYTHONPATH=.:server/internal/health/testdata ~/cf-test/.venv/bin/python server/internal/health/testdata/gen_review_c16.py
Writes server/internal/health/testdata/review_c16.jsonl.gz. Synthetic values only; no network.
"""
import datetime as dt
import gzip
import json
import os
import subprocess
import sys
import tempfile
from types import SimpleNamespace

import c16_record

c16_record._install()

from backend.common import config as config_module  # noqa: E402
from backend.common.config_repository import ProfileRepository  # noqa: E402
from backend.utils import domain_probe, health_check, node_dns, node_latency, region_check  # noqa: E402

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'review_c16.jsonl.gz')
TEST = ['']


def call(label, function, *args, **kwargs):
    c16_record._TEST[0] = 'review::' + label
    try:
        function(*args, **kwargs)
    except Exception:  # noqa: BLE001 - recorded
        pass


tmp = tempfile.mkdtemp()
repo = ProfileRepository(os.path.join(tmp, 'data'))
config_module.set_repository(repo)
config_module.reset_config_context()

# ---- validate_health: URL forms, numbers, statuses
urls = ['https://[github.com]/', 'https://[::1]/', 'http://[v1.x]/', 'http://[v1.x', 'https://[::1]:0/', 'https://h:65535/',
        'https://h:65536/', 'https://h:０/', 'https://h:+80/', 'https://ｈ.test/', 'https://h／x/', 'HTTPS://H/',
        'https://u@h/', 'https://:p@h/', 'https://@h/', 'https://h/#', 'https://h/#x', 'https://h?#', 'ftp://h/',
        'https:///x', 'https://h:/', 'https://[fe80::1%25eth0]/', 'https://[fe80::1%]/', 'https://1.2.3.4.5/', '//h/',
        'https://h\u00a0/', 'https://h\u2028/', 'https://h/\x7f', 'https://例子.测试/']
for url in urls:
    for provider in (False, True):
        call('vh url', health_check.validate_health, {'url': url}, provider=provider)
for key in ('interval', 'timeout', 'tolerance'):
    for value in (0, 1, -1, 86400, 86401, 600000, 600001, 2 ** 63, 2 ** 64 + 1, -2 ** 63 - 1, 1.0, True, '5', None):
        call('vh int', health_check.validate_health, {key: value})
for value in ('204', '200-299/304', '599-100', '099', '600', '200-', '2000', '204\n', '\uff12\uff10\uff14', '204/', '/204',
              '100-599/100-599/204', '204-204', '٢٠٤', 204, None):
    call('vh status', health_check.validate_health, {'expected-status': value}, provider=True)
for values in ([], 'x', None, {'enable': 1}, {'lazy': None}, {'x': 1}, {'enable': True, 'x': 1}):
    call('vh shape', health_check.validate_health, values, provider=True)
    call('vh shape', health_check.validate_health, values)
for source in ({}, {'health_check': None}, {'health_check': {'url': 'https://h/'}}, {'health_check': {'interval': 0}}):
    call('phc', health_check.provider_health_check, source)
for group in ({'type': 'url-test', 'interval': 5}, {'type': 'fallback'}, {'type': 'select'}, {'type': None}, {'type': ['url-test']}):
    call('igh', health_check.initialize_group_health, group)
for source in ({'health_check_url': 'https://x/'}, {'health_check_url': ''}, {'health_check': None}, {'health_check_url': 5}):
    call('iph', health_check.initialize_provider_health, source)

# ---- node DNS server forms
servers = ['1.2.3.4', '1.2.3.4:53', '1.2.3.4:053', '1.2.3.4:00000', '1.2.3.4:', ':53', '1.2.3.4 :53', ' 1.2.3.4 ',
           '\u30001.2.3.4', '１.2.3.4', '01.2.3.4', '0.0.0.0', '224.0.0.1', '239.255.255.255', '255.255.255.255',
           '127.0.0.1:65535', '1.2.3.4:65536', '1.2.3.4:٥٣', '1.2.3.4:+53', '1.2.3.4:5:3', '1.2.3', '1.2.3.4.5',
           'https://dns.test', 'https://dns.test/', 'https://DNS.Test/dns-query', 'https://dns.test:443/x',
           'https://dns.test:8443/x', 'https://dns.test:/x', 'https://dns.test:0/x', 'https://dns.test:65536/',
           'tls://dns.test', 'tls://dns.test:853', 'tls://dns.test:8853', 'tls://dns.test/x', 'tls://1.2.3.4',
           'https://[github.com]/', 'https://[::1]/', 'https://[v1.x]/', 'https://1.2.3.4.5/', 'https://01.2.3.4/',
           'https://0.0.0.0/', 'https://224.0.0.1/', 'https://例子.测试/dns-query', 'https://xn--fsqu00a.xn--0zwm56d/',
           'https://Ｅxample.com/', 'https://ex_ample.com/', 'https://-a.test/', 'https://a-.test/', 'https://a.test./',
           'https://a..test/', 'https://' + 'a' * 63 + '.test/', 'https://' + 'a' * 64 + '.test/',
           'https://' + '.'.join(['a' * 61] * 4) + '/', 'https://' + '.'.join(['a' * 62] * 4) + '/',
           'https://dns.test/%2', 'https://dns.test/%zz', 'https://dns.test/%41', 'https://dns.test/é',
           'https://dns.test/?x', 'https://dns.test/#x', 'https://dns.test\\x', 'https://u@dns.test/',
           'https://:p@dns.test/', 'https://@dns.test/', 'http://dns.test/', 'quic://dns.test', 'https:///x',
           'https://dns.test\u00a0/', 'https://dns.test/\x7f', 'https://' + 'a' * 2050, 'https://ß.test/',
           'https://faß.de/', 'https://☃.test/', 'https://xn--n3h.test/', 'https://İ.test/', 'https://K.test/',
           'https://1.2.3.4:443', 'https://[fe80::1%25eth0]/', '', '   ', None, 5, ['1.2.3.4']]
for server in servers:
    call('server', node_dns._server, server)
    call('ipv4', node_dns._ipv4_server, server, 'msg')
for value in (servers[:4], servers[:5], ['1.2.3.4', '1.2.3.4:53', ' 1.2.3.4'], 'x', None, {'a': 1}, []):
    call('nds', node_dns.normalize_dns_servers, value)
    call('nds', node_dns.normalize_dns_servers, value, bootstrap=True)
for value in ({'mode': 'custom', 'servers': ['https://dns.test/'], 'bootstrap': []},
              {'mode': 'custom', 'servers': ['https://dns.test/'], 'bootstrap': ['1.2.3.4']},
              {'mode': 'custom', 'servers': ['https://1.2.3.4/'], 'bootstrap': None},
              {'mode': 'custom', 'servers': []}, {'mode': 'custom'}, {'mode': 'system', 'servers': 5},
              {'mode': 'managed'}, {'mode': True}, {}, None, [], {'mode': 'custom', 'servers': ['tls://dns.test'],
                                                                  'bootstrap': ['1.2.3.4:5353']}):
    call('nc', node_dns.normalize_config, value)

# ---- settings() over environment values
envs = [{'CONFIGFLOW_NODE_TEST_URL': u} for u in ('https://[github.com]/', 'http://[::1]:80/', 'http://[v1.x]/',
                                                  'https://u@h/', 'https://:p@h/', 'https://@h/', ' https://h/ ',
                                                  '\u3000https://h/\u3000', 'https://h:0/', 'https://h:65536/',
                                                  'https://h:８０/', 'https://ｈ/', 'ftp://h/', '')]
envs += [{'CONFIGFLOW_NODE_TEST_TIMEOUT_MS': v} for v in (' 5000 ', '5_000', '٥٠٠٠', '+600', '500.0', '10000', '10001',
                                                          '499', '', '0x200')]
envs += [{'CONFIGFLOW_NODE_TEST_UNIFIED_DELAY': v} for v in ('FALSE', 'False', ' 0', '0', 'no', 'İ', '')]
for env in envs:
    saved = {key: os.environ.pop(key, None) for key in c16_record.ENV_KEYS}
    os.environ.update(env)
    try:
        call('settings', node_latency.settings)
    finally:
        for key in c16_record.ENV_KEYS:
            os.environ.pop(key, None)
            if saved[key] is not None:
                os.environ[key] = saved[key]

# ---- worker outputs: _run_probe and check_settings
node_latency_probe = node_latency._probe_binary
node_latency._probe_binary = lambda: '/review/node-probe'


def fake_run(stdout, returncode=0, stderr=''):
    def run(argv, **kwargs):
        return SimpleNamespace(returncode=returncode, stdout=stdout, stderr=stderr)
    return run


options = {'url': 'https://www.gstatic.com/generate_204', 'timeout_ms': 5000, 'dns_servers': [], 'dns_bootstrap': [],
           'unified_delay': True}
proxies = [{'name': '["node","a"]', 'type': 'ss', 'server': '例子.test', 'port': 1, 'password': 'p😀'},
           {'name': '["node","b"]', 'type': 'http', 'server': 'b.test', 'port': 2}]
outputs = [
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': 1}, {'name': '["node","b"]', 'latency': None, 'error': 'x'}]},
    {'version': 4.0, 'results': [{'name': '["node","a"]', 'latency': 2 ** 64}, {'name': '["node","b"]', 'latency': 3}]},
    {'version': True, 'results': []}, {'version': '4', 'results': []}, {'version': 5}, {'results': []}, [], 'x', None, 4,
    {'version': 4, 'results': {}}, {'version': 4, 'results': None},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': 1.0}, {'name': '["node","b"]', 'latency': 1}]},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': True}, {'name': '["node","b"]', 'latency': 1}]},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': 0}, {'name': '["node","b"]', 'latency': 1}]},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': '1'}, {'name': '["node","b"]', 'latency': 1}]},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': None, 'error': ''}, {'name': '["node","b"]', 'latency': 1}]},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': None}, {'name': '["node","b"]', 'latency': 1}]},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': 1}, {'name': '["node","a"]', 'latency': 2},
                               {'name': '["node","b"]', 'latency': 3}]},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': 1}]},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': 1}, {'name': '["node","b"]', 'latency': 1},
                               {'name': 'extra', 'latency': 1}]},
    {'version': 4, 'results': [{'name': 5, 'latency': 1}, {'name': '["node","b"]', 'latency': 1}]},
    {'version': 4, 'results': [{'name': ['x'], 'latency': 1}]},
    {'version': 4, 'results': [{'latency': 1}]}, {'version': 4, 'results': [5]},
    {'version': 4, 'results': [{'name': '["node","a"]'}, {'name': '["node","b"]', 'latency': 1}]},
    {'version': 4, 'results': [{'name': '["node","a"]', 'latency': 1, 'error_code': 'x', 'name2': '😀'},
                               {'name': '["node","b"]', 'latency': 99999999999999999999999}]},
]
texts = [json.dumps(o, ensure_ascii=False) for o in outputs] + ['', 'not json', '{"version": 4, "results": [], }', 'NaN',
                                                               '{"version": NaN}', '\ufeff{"version": 4}', '-0']
for text in texts:
    subprocess.run, real_run = fake_run(text), subprocess.run
    try:
        call('run_probe', node_latency._run_probe, proxies, options)
    finally:
        subprocess.run = real_run
for code in (3, 1, -9, 2):
    subprocess.run, real_run = fake_run(texts[0], returncode=code), subprocess.run
    try:
        call('run_probe rc', node_latency._run_probe, proxies, options)
    finally:
        subprocess.run = real_run

checks = [
    {'success': True, 'addresses': ['1.2.3.4'], 'latency_ms': 0},
    {'success': True, 'addresses': ['1.2.3.4'], 'latency_ms': -0},
    {'success': True, 'addresses': ['1.2.3.4'], 'latency_ms': 1.0},
    {'success': True, 'addresses': ['1.2.3.4'], 'latency_ms': True},
    {'success': True, 'addresses': ['1.2.3.4'], 'latency_ms': 2 ** 70},
    {'success': True, 'addresses': ['1.2.3.4'], 'latency_ms': -1},
    {'success': True, 'addresses': ['fe80::1%eth0', '::ffff:198.18.0.1', '::0001', '::1']},
    {'success': True, 'addresses': ['198.19.255.255']}, {'success': True, 'addresses': ['198.20.0.0', '198.17.255.255']},
    {'success': True, 'addresses': ['1.2.3.4 ']}, {'success': True, 'addresses': ['01.2.3.4']},
    {'success': True, 'addresses': []}, {'success': False, 'addresses': [], 'error_code': 5},
    {'success': False, 'addresses': [], 'error_code': None}, {'success': False, 'addresses': [], 'error_code': []},
    {'success': False, 'addresses': [], 'error_code': 'TLS'}, {'success': False, 'addresses': [], 'error_code': 'tls'},
    {'success': False, 'addresses': ['1.2.3.4', '1.2.3.4'], 'error_code': 'timeout', 'latency_ms': 3},
    {'success': 1, 'addresses': []}, {'success': True, 'addresses': 'x'}, {'success': True, 'addresses': [1]},
    {'success': True, 'addresses': ['2001:db8::1', '2001:DB8:0:0::1', '::ffff:1.2.3.4']},
    {'success': True, 'addresses': ['1.2.3.4'], 'latency_ms': None, 'error_code': 'dns_fake_ip'},
]
check_texts = [json.dumps({'version': 4, 'dns_check': c}) for c in checks]
check_texts += [json.dumps({'version': 4, 'dns_check': []}), json.dumps({'version': 4, 'dns_check': 'x'}),
                json.dumps({'version': 4}), json.dumps({'version': 3, 'dns_check': {}}), '[]', 'garbage']
for text in check_texts:
    subprocess.run, real_run = fake_run(text), subprocess.run
    try:
        call('check', node_dns.check_settings, {'mode': 'system'})
    finally:
        subprocess.run = real_run
for code in (3, 1):
    subprocess.run, real_run = fake_run(check_texts[0], returncode=code), subprocess.run
    try:
        call('check rc', node_dns.check_settings, {'mode': 'custom', 'servers': ['https://dns.test/'],
                                                    'bootstrap': ['1.2.3.4']})
    finally:
        subprocess.run = real_run
node_latency._probe_binary = node_latency_probe

# ---- region check
names = ['ｈｋ', 'Hong\u00a0Kong', 'HongKong', 'hk1', '1HK', 'xHK', 'ÜS', 'TÜRKIYE', 'türkiye', 'İndia', 'ſingapore',
         'Singapore', 'K', 'UK', 'uk', 'Britain', '港', '🇭🇰', 'America', 'AMERICA', 'us-west', 'US_1', 'HKG', 'JPN',
         '', None, 0, 5, ['HK'], 'ca', 'CAN', 'Ca', 'ar', 'in', 'IN', 'my', 'tH', 'Hong\tKong', 'Hong\u3000Kong']
for name in names:
    call('region', region_check.region_of_name, name)
for code in ('HK', 'hk', 'XX', '', None, 0, ['HK']):
    call('label', region_check.region_label, code)
statuses = [200, 200.0, True, '200', None, 0, 451, 451.0, 403, 499, 500, -1, 1e3, 2 ** 70]
for status in statuses:
    call('classify', region_check.classify_domain_result, {'id': 'd', 'status': status}, 'https://x/')
    call('classify', region_check.classify_domain_result, {'id': 'd', 'status': status, 'markers': {'block': 'not available in your country'}}, 'https://x/')
for final in ('https://x/geo-block', 'https://x/geoblock', 'https://x/Region', 'https://[::1/region', 'region', '',
              'https://x/?region=1', 'https://x/#country', 5):
    call('classify url', region_check.classify_domain_result, {'id': 'd', 'status': 200, 'final_url': final}, 'https://x/')
    call('classify url', region_check.classify_domain_result, {'id': 'd', 'status': 200, 'final_url': final}, 'https://x/region')
for result in (None, {}, {'error': ''}, {'error': 'boom'}, {'error': 0, 'status': 200}, [], 'x'):
    call('classify shape', region_check.classify_domain_result, result, 'https://x/')
targets = [{'name': 'DIRECT', 'type': 'Direct'}, {'name': 'GLOBAL', 'type': 'Selector'},
           {'name': 'ConfigFlow-Region-Probe', 'type': 'Selector'}, {'name': 'B 香港', 'type': 'URLTest'},
           {'name': 'A', 'type': 'Selector'}, {'name': 'C', 'type': 'Fallback'}, {'name': 'D', 'type': 'LoadBalance'},
           {'name': 'E', 'type': 'Relay'}, {'name': 'HK 1', 'type': 'Shadowsocks', 'alive': False},
           {'name': 'HK 2', 'type': 'Shadowsocks'}, {'name': 'BR', 'type': 'Vmess'}, {'name': 'JP', 'alive': 0},
           {'name': 'JP 2'}, {'name': 'US'}, {'name': 'SG'}, {'name': 'TW'}, {'name': 'KR'}, {'name': 'DE'}, {'name': 'GB'},
           {'name': 'FR'}, {'name': 'NL'}]
for preferred in (None, 'C', 'E', 'missing', 'GLOBAL'):
    call('pick', region_check.pick_targets, targets, preferred)
for bad in ([{'name': ['x']}], [{'type': 'Selector'}], [{'name': 'A', 'type': ['Selector']}], ['x'], [{'name': 5, 'type': 'Selector'}],
            [{'name': 'HK', 'type': None}, {'name': 'HK2', 'alive': None}]):
    call('pick bad', region_check.pick_targets, bad)
for per_target in ({}, {'a': {'state': 'ok'}}, {'a': {'state': 'restricted'}}, {'a': {'state': 'restricted'}, 'b': {'state': 'error'}},
                   {'a': {'state': 'denied'}}, {'a': {}}, {'a': {'state': 'ok'}, 'b': {'state': 'restricted'}}):
    call('summarize', region_check.summarize_domain, per_target)
results_sets = [
    [{'id': 'exit', 'markers': {'loc': 'HK', 'ip': '1.2.3.4'}}, {'id': 'openai-compliance', 'status': 200, 'markers': {'consent': ''}},
     {'id': 'openai-ios', 'status': 200, 'markers': {'dc': 'x'}}, {'id': 'claude-web', 'status': 200, 'final_url': 'https://claude.ai/',
                                                                      'redirects': ['https://claude.ai/app-unavailable-in-region']},
     {'id': 'netflix-licensed-1', 'status': 404, 'final_url': 'https://www.netflix.com/hk-en/title/1'},
     {'id': 'netflix-original', 'status': 200.0, 'final_url': 'https://www.netflix.com/title/2'},
     {'id': 'youtube-premium', 'status': 200, 'markers': {'gl': 'CN'}}],
    [{'id': 'exit', 'error': 'timeout'}, {'id': 'claude-web', 'status': 403, 'final_url': '', 'redirects': []},
     {'id': 'netflix-licensed-1', 'status': 403}, {'id': 'netflix-licensed-2', 'status': 403}, {'id': 'netflix-original', 'status': 403},
     {'id': 'youtube-premium', 'status': 200, 'markers': {'blocked': 'x', 'gl': 'US'}}],
    [{'id': 'claude-web', 'status': 200, 'redirects': 'app-unavailable-in-region'}, {'id': True, 'status': 1},
     {'id': 1, 'status': 2}, {'id': 'netflix-licensed-2', 'status': 200, 'final_url': 'https://www.netflix.com/title/3'}],
    [{'id': 'claude-web', 'status': 200, 'final_url': ['app-unavailable-in-region']}],
    [{'id': 'claude-web', 'status': 200, 'redirects': None}], [{'id': ['x']}], [{'status': 200}], ['x'], [],
    [{'id': 'openai-compliance', 'error': 'x', 'markers': {'unsupported': 'y'}}, {'id': 'openai-ios', 'markers': {'vpn': 'v'}}],
    [{'id': 'youtube-premium', 'status': 200, 'markers': None}],
    [{'id': 'openai-compliance', 'status': 200, 'markers': {'consent': None, 'b': 1, 'a': 2}}],
    [{'id': 'openai-compliance', 'status': 200, 'markers': ['z', 'a']}],
    [{'id': 'openai-compliance', 'status': 200, 'markers': 'zx'}],
]
for results in results_sets:
    call('evaluate', region_check.evaluate_services, results)
    call('exit', region_check.exit_info, results)

# ---- domain probe
stats = [None, {}, {'total': 0}, {'ok': 1, 'total': 2}, {'ok': 1, 'total': 2.0}, {'total': 3}, {'ok': True, 'total': 1},
         {'ok': '1', 'total': 2}, {'ok': 1, 'total': '2'}, {'ok': 10 ** 400, 'total': 1}, {'ok': 1, 'total': 10 ** 400},
         {'ok': 1, 'total': 1e-320}, {'ok': 0.5, 'total': 1}, {'ok': 2, 'total': 1}, {'ok': -1, 'total': 1}]
for s in stats:
    call('healthy', domain_probe.path_healthy, s)
pairs = [
    ({'ok': 2, 'total': 2, 'delays': [2.5, 3.5]}, {'ok': 2, 'total': 2, 'delays': [1.5, 2.5]}),
    ({'ok': 2, 'total': 2, 'delays': [150]}, {'ok': 2, 'total': 2, 'delays': [100]}),
    ({'ok': 2, 'total': 2, 'delays': [151]}, {'ok': 2, 'total': 2, 'delays': [100]}),
    ({'ok': 2, 'total': 2, 'delays': [10 ** 30]}, {'ok': 2, 'total': 2, 'delays': [10 ** 30]}),
    ({'ok': 2, 'total': 2, 'delays': [10 ** 400]}, {'ok': 2, 'total': 2, 'delays': [1]}),
    ({'ok': 2, 'total': 2, 'delays': [1, 2, 3, 4]}, {'ok': 2, 'total': 2, 'delays': [5, 1, 3]}),
    ({'ok': 2, 'total': 2, 'delays': [True, False]}, {'ok': 2, 'total': 2, 'delays': [1]}),
    ({'ok': 2, 'total': 2, 'delays': []}, {'ok': 2, 'total': 2, 'delays': [1]}),
    ({'ok': 0, 'total': 1}, {'ok': 1, 'total': 1}), ({'ok': 0, 'total': 2}, {'ok': 2, 'total': 2}),
    ({'ok': 0, 'total': 2.0}, {'ok': 2, 'total': 2}), ({'ok': 2, 'total': 2}, {'ok': 0, 'total': 2}),
    ({'ok': 0, 'total': 2}, {'ok': 0, 'total': 2}), ({'ok': 1, 'total': 3}, {'ok': 1, 'total': 3}),
    ({'ok': 1, 'total': 2}, {'ok': 1, 'total': 2}), ({'total': 2}, {'ok': 2, 'total': 2}),
    ({'ok': 0, 'total': 2}, {'ok': 1, 'total': 2, 'errors': {'no_path': 1}}),
    ({'ok': 0, 'total': 2}, {'ok': 1, 'total': 2, 'errors': None}), (None, {'ok': 1, 'total': 1}),
    ({'ok': 2, 'total': 2, 'delays': [0.5]}, {'ok': 2, 'total': 2, 'delays': [0.3333333333333333]}),
]
for direct, proxy in pairs:
    for kwargs in ({}, {'passive_failing': True}, {'direct_healthy': False}, {'proxy_healthy': False},
                   {'direct_healthy': False, 'proxy_healthy': False}):
        call('judge', domain_probe.judge, direct, proxy, **kwargs)
for host, port in (('a.test', 80), ('a.test', 443), ('a.test', None), ('a.test', 0), ('a.test', 8443), ('a.test', 80.0),
                   ('a.test', True), ('例子.test', 65535), ('a.test', '8080')):
    call('probe_url', domain_probe.probe_url, host, port)
for hosts in ([], [{}], [{'probe': None}], [{'probe': {'target': 'proxy', 'confidence': 50}}, {'probe': {'target': 'direct', 'confidence': 50}}],
              [{'probe': {'target': 'proxy', 'confidence': 50}}, {'probe': {'target': 'direct', 'confidence': 60.5}}],
              [{'probe': {'checked_at': '2026-01-02'}}, {'probe': {'checked_at': '2026-01-03', 'host': 'b'}}],
              [{'probe': {'checked_at': '2026-01-02'}}, {'probe': {}}],
              [{'probe': {'target': 'x', 'confidence': '5'}}, {'probe': {'target': 'y', 'confidence': 5}}],
              [{'probe': {'target': 'x'}}], ['x']):
    call('suggestion', domain_probe.domain_suggestion, hosts)

# ---- stores (fixed now; boundary at exactly 30 days, ties beyond 5000 hosts)
now = dt.datetime(2026, 3, 29, 2, 30, 0)
store_dir = os.path.join(tmp, 'probes')
probe_store = domain_probe.ProbeStore(store_dir)
region_store = region_check.RegionStore(os.path.join(tmp, 'regions'))
bound = (now - dt.timedelta(days=30)).isoformat(timespec='seconds')
hosts = {f'h{i}.test': {'checked_at': bound if i % 3 else '2026-03-28T00:00:00'} for i in range(5003)}
hosts['old.test'] = {'checked_at': '2026-02-27T02:29:59'}
hosts['none.test'] = {}
call('probe save', probe_store.save, 'agent/1', hosts, now=now)
call('probe load', probe_store.load, 'agent/1', now=now)
call('probe save bad', probe_store.save, 'agent-2', {'x': {'checked_at': 5}}, now=now)
call('probe save bad', probe_store.save, 'agent-3', {'x': 'y'}, now=now)
with open(os.path.join(store_dir, 'agent-4.json'), 'w') as handle:
    handle.write('{"hosts": [1]}')
call('probe load list', probe_store.load, 'agent-4', now=now)
with open(os.path.join(store_dir, 'agent-5.json'), 'w') as handle:
    handle.write('{"hosts": {"a": {"checked_at": "2026-03-01T00:00:00"}}, "extra": "😀"}')
call('probe save keep', probe_store.save, 'agent-5', {'b': {'checked_at': '2026-03-29T00:00:00'}}, now=now)


def mutate_sites(data):
    data['websites'].update({f'w{i}.test': {'checked_at': f'2026-03-{1 + i % 28:02d}T00:00:00'} for i in range(1010)})
    data['domains'].update({'d.test': {'checked_at': bound}, 'e.test': {'checked_at': '2026-02-01T00:00:00'}})


call('region update', region_store.update, 'agent-1', mutate_sites, now=now)
call('region load', region_store.load, 'agent-1')
call('region update bad', region_store.update, 'agent-2', lambda data: data['domains'].update({'x': {'checked_at': None}}), now=now)
call('region update bad', region_store.update, 'agent-3', lambda data: data.update({'domains': []}), now=now)
call('region update services', region_store.update, 'agent-4', lambda data: data.update({'services': {'a': 1}}), now=now)

with gzip.open(OUT, 'wt', encoding='utf-8') as handle:
    for entry in c16_record._RECORDS:
        handle.write(json.dumps(entry, ensure_ascii=False) + '\n')
print(len(c16_record._RECORDS), 'rows', file=sys.stderr)
