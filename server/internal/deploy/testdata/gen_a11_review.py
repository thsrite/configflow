"""A11 HR review oracle: real Python outputs for the review findings (synthetic data only)."""
import base64, copy, hashlib, itertools, json
from pathlib import Path
import yaml
from backend.agents.deployment_bundle import convert_mosdns_rules, deployment_content_fingerprint
from backend.agents.deployment_locations import source_excerpt
from backend.agents.mihomo_resources import required_geo_resources

root = Path(__file__).parent

# Rule sources that Python reads as YAML documents: quoted payload keys and flow/JSON mappings.
texts = ['{"payload": ["DOMAIN,example.com", "IP-CIDR,192.0.2.0/24", "DOMAIN-SUFFIX,example.org"]}',
         '"payload":\n  - DOMAIN,example.com\n  - IP-CIDR,192.0.2.0/24\n',
         "'payload': [DOMAIN-SUFFIX,example.org, 'IP-CIDR6,2001:db8::/32']\n",
         '  {payload: [example.com, 192.0.2.1]}\n',
         '{"version": 1, "rules": [{"domain_suffix": ["example.com"], "ip_cidr": ["192.0.2.0/24"]}]}',
         '\ufeff{"payload": ["DOMAIN,example.net"]}']
conversions = []
for text, kind, mixed in itertools.product(texts, ['domain_set', 'ip_set'], [False, True]):
    row = dict(text=text, kind=kind, mixed=mixed)
    try:
        row['result'] = base64.b64encode(convert_mosdns_rules(text.encode(), kind, mixed=mixed)).decode()
    except ValueError as exc:
        row['error'] = str(exc)
    conversions.append(row)

# Credentials whose value continues on the excerpted line.
excerpt_texts = ['authorization: Bearer\nsynthetic-bearer-value\nnext: x\n',
                 'headers: {Authorization: Basic\n  c3ludGhldGljOnZhbHVl}\nrest: 1\n',
                 'token\n: synthetic-token-value\nother: 1\n',
                 'uuid\n= 00000000-0000-4000-8000-000000000000\n',
                 'proxies:\n  - {name: a, password: Bearer\n     synthetic-pass}\n']
excerpts = []
for text in excerpt_texts:
    for line in range(1, len(text.splitlines()) + 2):
        excerpts.append(dict(text=text, line=line, result=source_excerpt(text, line)))

# Geo selection from real YAML text (YAML 1.2 core booleans/numbers parse the same in both loaders).
geo_cases = [
    ('rules: ["GEOIP,CN,DIRECT"]\ngeodata-mode: True\n', {}),
    ('rules: ["GEOIP,CN,DIRECT"]\ngeodata-mode: TRUE\n', {}),
    ('dns: {fallback: [192.0.2.1], fallback-filter: {geoip: False}}\n', {}),
    ('dns: {fallback: False}\n', {}),
    ('dns: {fallback: 0.0}\n', {}),
    ('dns: {fallback: 0x0}\n', {}),
    ('dns: {fallback: [192.0.2.1], fallback-filter: {geoip: true, geosite: 0}}\n', {}),
    ('sniffer: {skip-dst-address: ["geoip:lan:extra"]}\n', {}),
    ('sub-rules: {"IP-ASN,1": ["MATCH,DIRECT"]}\n', {}),
    ('rule-providers: {Set: {behavior: classical, format: text, path: ./rules/set.list}}\n',
     {'rules/set.list': '\ufeffGEOSITE,cn\n'}),
]
geo = []
for text, files in geo_cases:
    document = yaml.safe_load(text)
    packed = {path: (value.encode('utf-8'), 'rule') for path, value in files.items()}
    geo.append(dict(text=text, files=files, required=required_geo_resources(document, packed)))

# Persisted v1 fingerprints for paths JSON escapes differently by default.
base = dict(protocol_version=1, deployment_id='a11-review', agent_id='agent-example', profile_id='profile-example',
            service_type='mosdns', config_path='config.yaml')
fingerprints = []
for path in ['rules/a b.txt', 'rules/a b.txt', 'rules/<a>&"b\'.txt', 'rules/é\U0001F600.txt']:
    manifest = copy.deepcopy(base)
    manifest['files'] = [dict(path='config.yaml', size=3, sha256=hashlib.sha256(b'a:1').hexdigest(), role='config'),
                         dict(path=path, size=1, sha256=hashlib.sha256(b'x').hexdigest(), role='rule')]
    fingerprints.append(dict(manifest=manifest, fingerprint=deployment_content_fingerprint(manifest)))

# Build failures and JSON rule sources through a loopback rule server (placeholder @RULE_SERVER@).
import tempfile, threading
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
from backend.agents.deployment_bundle import build_deployment_bundle
from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
payloads = {'/ips': '192.0.2.0/24\n', '/json': '{"payload": ["DOMAIN,example.com", "IP-CIDR,192.0.2.0/24"]}',
            '/singbox': '{"version": 1, "rules": [{"domain_suffix": ["example.com"]}]}'}


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200 if self.path in payloads else 404)
        self.end_headers()
        self.wfile.write(payloads.get(self.path, '').encode())

    def log_message(self, *args):
        pass


server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
threading.Thread(target=server.serve_forever, daemon=True).start()
base = f'http://127.0.0.1:{server.server_port}'
tmp = tempfile.TemporaryDirectory()
config_module._repository = ProfileRepository(tmp.name)
config_module.reset_config_context()
source = {'profile_id': 'alpha', 'system_config': {'server_domain': 'https://panel.example'},
          'rule_library': [{'id': 'lib', 'name': 'Library', 'source_type': 'url', 'url': base + '/missing',
                            'behavior': 'domain', 'format': 'text'}]}
dns_main = {'plugins': [{'tag': 'domains', 'type': 'domain_set', 'args': {'files': ['./rules/m']}},
                        {'tag': 'ips', 'type': 'ip_set', 'args': {'files': ['./rules/i']}}]}
builds = []


def build(name, service, main, downloads):
    row = dict(name=name, service=service, main=yaml.safe_dump(main, allow_unicode=True, sort_keys=False),
               downloads=downloads)
    try:
        bundle = build_deployment_bundle({'id': 'agent-example', 'service_type': service, 'profile_id': 'alpha'},
                                         copy.deepcopy(source), row['main'], ruleset_downloads=downloads,
                                         base_url='https://panel.example', deployment_id='a11-review',
                                         local_dns_watchdog=False)
        row['files'] = {p: base64.b64encode(v).decode() for p, v in bundle.files().items() if p != 'config.yaml'}
    except ValueError as exc:
        row['error'] = str(exc)
        row['remedy'] = getattr(exc, 'remedy', None)
    builds.append(row)


for behavior in ('domain', None):
    item = {'name': '缺失', 'url': base + '/missing', 'local_path': './rules/m'}
    if behavior:
        item['behavior'] = behavior
    build('missing-' + str(behavior), 'mosdns', dns_main,
          [item, {'name': 'IPs', 'url': base + '/ips', 'local_path': './rules/i', 'behavior': 'ipcidr'}])
build('library-missing', 'mosdns', dns_main,
      [{'name': 'Library', 'url': 'https://panel.example/api/profiles/alpha/rules/local/Library', 'local_path': './rules/m',
        'behavior': 'domain'},
       {'name': 'IPs', 'url': base + '/ips', 'local_path': './rules/i', 'behavior': 'ipcidr'}])
build('refused', 'oxidns', dns_main,
      [{'name': 'Refused', 'url': 'http://127.0.0.1:1/rules', 'local_path': './rules/m', 'behavior': 'domain'},
       {'name': 'IPs', 'url': base + '/ips', 'local_path': './rules/i', 'behavior': 'ipcidr'}])
build('mihomo-missing', 'mihomo', {'rule-providers': {'Set': {'type': 'http', 'url': base + '/missing',
                                                              'path': './rules/set', 'behavior': 'domain', 'format': 'text'}}}, [])
for kind in ('json', 'singbox'):
    build('json-' + kind, 'mosdns', dns_main,
          [{'name': 'Domains', 'url': base + '/' + kind, 'local_path': './rules/m', 'behavior': 'classical', 'format': 'yaml'},
           {'name': 'IPs', 'url': base + ('/json' if kind == 'json' else '/ips'), 'local_path': './rules/i',
            'behavior': 'classical' if kind == 'json' else 'ipcidr', 'format': 'yaml' if kind == 'json' else 'text'}])
    build('json-undeclared-' + kind, 'mosdns', dns_main,
          [{'name': 'Domains', 'url': base + '/' + kind, 'local_path': './rules/m'},
           {'name': 'IPs', 'url': base + '/ips', 'local_path': './rules/i', 'behavior': 'ipcidr'}])
server.shutdown()

(root / 'review.json').write_text(json.dumps(dict(conversions=conversions, excerpts=excerpts, geo=geo,
                                                  fingerprints=fingerprints, builds=builds, payloads=payloads),
                                             ensure_ascii=False).replace(base, '@RULE_SERVER@'))
print(f'A11 review: {len(conversions)} conversions, {len(excerpts)} excerpts, {len(geo)} Geo, '
      f'{len(fingerprints)} fingerprints, {len(builds)} builds')
