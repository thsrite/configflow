"""Synthetic adversarial boundary references. Run only on the test server."""
import copy
import gzip
import json
import os
import tempfile
from pathlib import Path

temporary = tempfile.TemporaryDirectory(prefix='oxidns-review-')
os.environ['DATA_DIR'] = temporary.name
from backend.common.oxidns_plugin_constraints import _zone_records_error
from backend.common.oxidns_validation import format_error
from backend.converters.oxidns import generate_oxidns_config, get_oxidns_custom_files

rows = []
def record(fn, args, call):
    row = {'fn': fn, 'args': copy.deepcopy(args)}
    try:
        row['out'] = call()
    except Exception as e:
        row['err'] = [type(e).__name__, str(e)]
    rows.append(row)

names = ['家用.example.', '打印机.例子.', 'bücher.example.', 'BÜCHER.example.',
         'faß.example.', '电视。例子。', '日本語.example.', 'δοκιμή.example.',
         'пример.example.', 'مثال.example.', 'שלום.example.', '☃.example.',
         '😀.example.', '_服务._tcp.example.', '*.例子.', 'XN--bcher-kva.example.']
for name in names:
    records = [f'{name} 300 IN A 192.0.2.1', f'{name} AAAA 2001:db8::1',
               f'{name} CNAME 目标.例子.', f'{name} MX 10 邮件.例子.',
               f'{name} TXT "中文说明"', f'$ORIGIN {name}\n主机 A 192.0.2.2\n别名 CNAME 主机',
               f'{name} SRV 0 5 443 服务.例子.', f'{name} SOA ns.example. hostmaster.example. 1 2h 3d 4w 5',
               f'{name} CAA 0 issue "ca.example"', f'{name} A 192.0.2.999']
    for text in records:
        for single in (False, True):
            record('zone_records_error', [text, single], lambda: _zone_records_error(text, single))

for value in ['https://dns.example/dns-query', 'https://例子.example/dns-query',
              'https://dns.example/dns-query?name=家庭', 'tls://dns.example:853',
              'udp://[2001:db8::53]:53', '${UPSTREAM}', '$${X}', 'https://user:密码@dns.example/q']:
    record('format_error', ['upstream-address', value], lambda: format_error('upstream-address', value))

for i, patch in enumerate([
    {}, {'prefer_ipv4': True, 'cache_enabled': False},
    {'local_dns': '- addr: tls://dns.example:853\n  bootstrap: 192.0.2.53:53',
     'remote_dns': '- addr: https://resolver.example/dns-query\n  dial_addr: 192.0.2.54:443'},
    {'custom_hosts': '打印机.example 192.0.2.1\n192.0.2.1 printer.example alias.example\n电视.example 2001:db8::1\n'},
    {'custom_config': 'extra: &defaults\n  note: 中文\n  value: 12\ncopy:\n  <<: *defaults\n  value: 13\n'},
    {'direct_rules': ['direct'], 'proxy_rules': ['proxy'], 'aggregate_rules': False},
    {'direct_rules': ['direct'], 'proxy_rules': ['proxy'], 'aggregate_rules': True},
    {'adguard': {'enabled': True, 'urls': ['https://filters.example/dns.txt'], 'content': '||ads.example^'}},
    {'fallback_dns': '- addr: udp://192.0.2.54', 'fallback_threshold': 500},
    {'cache_dump_enabled': True, 'query_recorder': {'enabled': True}},
    {'dynamic_domain_set': {'enabled': True, 'learn': True, 'route': 'direct'}, 'ip_selector': {'enabled': True}},
    {'rule_actions': {'proxy': {'action': 'upstream', 'source': 'remote', 'upstream': {'addr': 'https://dns.example/q?name=例子', 'insecure_skip_verify': False, 'max_conns': 4}}}},
    {'proxy_rulesets': ['subscription'], 'rule_update': {'enabled': True}},
]):
    profile = {'profile_id': 'synthetic', 'system_config': {'server_domain': 'https://config.example', 'rule_proxy_token': 'synthetic-token'},
               'rule_configs': [{'id': 'direct', 'itemType': 'rule', 'rule_type': 'DOMAIN-SUFFIX', 'value': '例子.example', 'enabled': True},
                                {'id': 'proxy', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'proxy.example', 'enabled': True},
                                {'id': 'subscription', 'itemType': 'ruleset', 'url': 'https://lists.example/rules.txt', 'name': '规则订阅', 'behavior': 'classical', 'enabled': True}],
               'oxidns': {'local_dns': 'udp://192.0.2.53', 'remote_dns': 'https://dns.example/dns-query', **patch}}
    record('generate', [profile, 'https://config.example'], lambda: generate_oxidns_config(profile, 'https://config.example'))
    record('custom_files', [profile], lambda: get_oxidns_custom_files(profile))

path = Path(__file__).with_name('review_s.jsonl.gz')
with gzip.open(path, 'wt') as out:
    for row in rows: out.write(json.dumps(row, ensure_ascii=False) + '\n')
print(f'{len(rows)} references -> {path}')

temporary.cleanup()
