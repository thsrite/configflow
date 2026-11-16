"""G07/G09: declared provider formats and DNS conversion through public APIs."""
import struct
from urllib.parse import urlencode

import yaml
import zstandard

from downloads import capture_headers, validate_headers
from harness import Pair, json_body, DesignError
from sources import Source
from subscriptions import Exchange
from cases.generator_shapes import project
from cases.rule_io import write_cache
from cases.subscriptions import require


def dns_conversion(binary, recorder):
    classical = ['DOMAIN,exact.example,DIRECT', 'DOMAIN-SUFFIX,suffix.example,no-resolve', 'DOMAIN-KEYWORD,key,DIRECT',
                 'DOMAIN-REGEX,^a,b[.]example$,DIRECT,no-resolve', 'IP-CIDR,192.0.2.0/24,no-resolve',
                 'IP-CIDR6,2001:db8::/32,no-resolve', 'GEOIP,CN']
    domains = ['full:exact.example', 'domain:suffix.example', 'keyword:key', 'regexp:^a,b[.]example$,DIRECT,no-resolve']
    ips = ['192.0.2.0/24', '2001:db8::/32']
    list_text = 'example.com\n+.example.org\n.example.net\n*.example.edu\n192.0.2.1\n2001:db8::/32\n'
    list_domains = ['full:example.com', 'domain:example.org', r'regexp:.+\.example\.net$', r'regexp:^[^.]+\.example\.edu$']
    native = '# original comment\ndomain:native.example\nfull:exact.example\nkeyword:key\nregexp:^a,b[.]native$\nip:192.0.2.0/24\n2001:db8::/32\n'
    forms = [('clash-text', '\n'.join(classical), domains, ips, '\n'.join(domains)),
             ('clash-yaml', yaml.safe_dump({'payload': classical}, sort_keys=False), domains, ips, '\n'.join(domains)),
             ('list', list_text, list_domains, ['192.0.2.1', '2001:db8::/32'], '\n'.join(list_domains + ['192.0.2.1', '2001:db8::/32'])),
             ('native', native, ['domain:native.example', 'full:exact.example', 'keyword:key', 'regexp:^a,b[.]native$'], ips, native)]
    with Source() as source:
        with Pair(binary, recorder) as pair:
            for label, text, domain_lines, ip_lines, combined in forms:
                source.body = text.encode()
                for part, expected in [(None, combined), ('domain', '\n'.join(domain_lines)), ('ip', '\n'.join(ip_lines))]:
                    query = {'url': source.url + '/rules'}
                    if part:
                        query['part'] = part
                    pair.request('G09', 'GET /api/mosdns/rule-proxy', '/api/mosdns/rule-proxy?' + urlencode(query),
                                 expect=200, writes=True, interaction=Exchange(pair, source), variant=label + '/' + (part or 'mixed'),
                                 check=lambda r: require(r.body == expected.encode(), 'DNS rule conversion preserves exact prefixes, comma regex payload and part filtering'))
            source.body = b'full:only-domain.example\n'
            pair.request('G09', 'GET /api/mosdns/rule-proxy', '/api/mosdns/rule-proxy?' + urlencode({'url': source.url + '/rules', 'part': 'ip'}),
                         expect=200, writes=True, interaction=Exchange(pair, source), variant='empty-ip-part',
                         check=lambda r: require(r.body == b'# no ip rules', 'empty selected part uses explicit nonempty downloader-safe marker'))


def format_shapes(binary, recorder):
    for behavior in ('domain', 'ipcidr', 'classical'):
        for rule_format in ('text', 'yaml', 'mrs'):
            if behavior == 'classical' and rule_format == 'mrs':
                continue  # An invalid declaration is not a G07 success shape.
            payload = ['+.example.com'] if behavior == 'domain' else ['192.0.2.0/24'] if behavior == 'ipcidr' else ['DOMAIN,example.com', 'IP-CIDR,192.0.2.0/24,no-resolve']
            if rule_format == 'mrs':
                raw = zstandard.ZstdCompressor(write_checksum=True).compress(b'MRS\x01' + bytes([0 if behavior == 'domain' else 1]) + struct.pack('>qq', 0, 0) + b'\x00')
            else:
                raw = (yaml.safe_dump({'payload': payload}, sort_keys=False) if rule_format == 'yaml' else '\n'.join(payload) + '\n').encode()
            with Source(raw) as source:
                declaration = {'id': 'format-source', 'name': '格式' + behavior + '_' + rule_format, 'source_type': 'url',
                               'url': source.url + '/rules', 'behavior': behavior, 'format': rule_format, 'enabled': True, 'update_interval': 0}
                opts = {'shared': {'rule_library': [declaration]}, 'profile': {
                    'rule_configs': [{'id': 'format-reference', 'itemType': 'ruleset', 'library_rule_id': 'format-source', 'policy': 'DIRECT', 'enabled': True}],
                    'mosdns': {'direct_rulesets': ['format-reference']}, 'oxidns': {'direct_rulesets': ['format-reference']}}}
                with Pair(binary, recorder, options=opts) as pair:
                    write_cache(pair, declaration, raw)
                    for service in ('mihomo', 'surge', 'loon', 'mosdns', 'oxidns'):
                        pair.request('G07', 'POST /api/generate/' + service + '/preview', body=json_body({'base_url': 'http://panel.example'}),
                                     headers={'Host': 'panel.example'}, expect=200, writes=True, interaction=Exchange(pair, source, expected=0),
                                     projection=project(service), variant=behavior + '/' + rule_format + '/preview/' + service)
                    if rule_format == 'mrs':
                        pair.request('G07', 'GET /api/mosdns/rule-proxy',
                                     '/api/mosdns/rule-proxy?' + urlencode({'url': source.url + '/rules', 'source_id': 'format-source'}),
                                     expect=422, writes=True, interaction=Exchange(pair, source, expected=0), variant=behavior + '/mrs/reject-dns-delivery',
                                     check=lambda r: require(r.json()['code'] == 'RULE_CONTENT_INVALID' and '不支持 MRS' in r.json()['message'], 'MRS is rejected before DNS conversion'))
                    if rule_format != 'mrs' or behavior == 'domain':
                        for service in ('mosdns', 'oxidns'):
                            status = (500 if service == 'mosdns' else 400) if rule_format == 'mrs' else 200
                            def check(reply):
                                if status == 200:
                                    errors = validate_headers(reply, pair.policy.start, pair.policy.end)
                                    if errors:
                                        raise DesignError('Python ruleset bundle headers: ' + repr(errors))
                                else:
                                    require('不支持 MRS' in reply.json()['message'], 'DNS archive cannot silently accept an active MRS source')
                            pair.request('G07', 'POST /api/generate/' + service, body=json_body({'base_url': 'http://panel.example'}),
                                         headers={'Host': 'panel.example'}, expect=status, writes=True, interaction=Exchange(pair, source, expected=0),
                                         check=check, capture=capture_headers if status == 200 else None, filename=service + '-config.zip',
                                         variant=behavior + '/' + rule_format + '/archive/' + service)


GROUPS = {'generator-dns-conversion': dns_conversion, 'generator-ruleset-formats': format_shapes}
