"""Synthetic client files through Python's public parse/build entry points."""
import ast
import copy
import json
from pathlib import Path
import sys

root = Path(__file__).resolve().parents[4]
sys.path.insert(0, str(root))
from backend.utils import config_importer as reference

source = ast.parse((root / 'backend/test_config_import.py').read_text())
fixtures = {}
for statement in source.body:
    if isinstance(statement, ast.Assign) and isinstance(statement.value, ast.Constant):
        name = statement.targets[0]
        if isinstance(name, ast.Name) and name.id.endswith('_CONFIG'):
            fixtures[name.id] = statement.value.value

def synthetic(text):
    replacements = {'223.5.5.5': '192.0.2.53', '10.0.0.0/8': '192.0.2.0/24',
                    '10.0.0.2': '192.0.2.2', 'google.com': 'search.example.com',
                    'apple.com': 'vendor.example.com', 'netflix.com': 'media.example.com'}
    for before, after in replacements.items():
        text = text.replace(before, after)
    return text

shared = {
    'subscriptions': [{'id': 'existing-sub', 'name': 'Existing', 'url': 'https://sub.test/clash', 'type': 'universal', 'enabled': True}],
    'nodes': [{'id': 'existing-hk', 'name': 'HK 01', 'type': 'ss', 'server': 'other.test', 'port': 1,
               'params': {'cipher': 'aes-128-gcm', 'password': 'x'}, 'enabled': True}],
    'rule_library': [{'id': 'existing-lib', 'name': 'google', 'url': 'https://rules.test/other.yaml', 'behavior': 'domain', 'enabled': True}],
}
rows = []
def add(name, client, content, catalog=None, defaults=None):
    rows.append({'name': name, 'client': client, 'content': synthetic(content),
                 'shared': copy.deepcopy(shared if catalog is None else catalog), 'defaults': defaults or []})
for client in ('mihomo', 'surge', 'loon', 'shadowrocket'):
    add('original-' + client, client, fixtures[client.upper() + '_CONFIG'], defaults=['existing-sub', 'missing'])

surge = [
 'ss, example.com, 8388, password="p,a", method=aes-256-gcm, udp-relay=true, tfo=true, obfs=http, obfs-host=obfs.example.com',
 'ss, example.com, 8388, method=aes-128-gcm, password=p, obfs=websocket, tls=true, path=/ws',
 'ss, example.com, 8388, password=p, obfs=tls, shadow-tls-password=shadow, shadow-tls-sni=tls.example.com, shadow-tls-version=2',
 'vmess, example.com, 443, username=id, tls=true, ws=true, ws-headers="Host:cdn.example.com|X-Test:ok", ws-path=/p, alpn="h2,h3", allowinsecure=true',
 'vless, example.com, 443, password=id, publickey=public, shortid=short, flow=xtls-rprx-vision, obfs=grpc, path=service',
 'trojan, example.com, 443, password=p, peer=tls.example.com, obfs=ws, obfs-host=cdn.example.com',
 'http, example.com, 80, user, pass, udp=true', 'https, example.com, 443, username=u, password=p, tls-name=s.example.com',
 'socks5, example.com, 1080, user, pass', 'socks5-tls, example.com, 1080, sni=s.example.com, skip-cert-verify=false',
 'snell, example.com, 443, psk=p, version=4, reuse=true, obfs=tls, obfs-host=cdn.example.com',
 'snell, example.com, 443, psk=p, shadow-tls-password=shadow, shadow-tls-sni=tls.example.com, shadow-tls-version=3',
 'hy2, example.com, 443, password=p, download-bandwidth=100, port-hopping="443;8443", salamander-password=obfs, alpn=h3',
 'tuic, example.com, 443, uuid=id, password=p, token=t', 'tuic-v5, example.com, 443, uuid=id, alpn=h3',
 'anytls, example.com, 443, password=p, sni=s.example.com',
]
loon = [
 'Shadowsocks,example.com,8388,aes-128-gcm,"p,a",udp=true,fast-open=true,obfs-name=tls,obfs-host=cdn.example.com,udp-over-tcp=true',
 'Shadowsocks,example.com,8388,aes-256-gcm,p,shadow-tls-password=s,shadow-tls-sni=t.example.com,shadow-tls-version=2',
 'ShadowsocksR,example.com,8388,cipher,p,protocol=auth_sha1_v4,protocol-param=param,obfs=tls1.2_ticket_auth,obfs-param=value',
 'VMess,example.com,443,auto,id,transport=ws,path=/ws,host=cdn.example.com,over-tls=true,sni=tls.example.com,skip-cert-verify=true,alpn="h2,h3"',
 'VMess,example.com,80,auto,id,transport=http,path=/h,host=cdn.example.com',
 'VLESS,example.com,443,id,public-key=public,short-id=short,flow=vision,transport=tcp',
 'Trojan,example.com,443,p,transport=ws,sni=t.example.com',
 'Hysteria2,example.com,443,p,salamander-password=obfs,server-ports="443,8443",download-bandwidth=50',
 'AnyTLS,example.com,443,p,sni=t.example.com',
 'HTTP,example.com,80,"",p', 'HTTPS,example.com,443,user,p,tls-name=t.example.com',
 'SOCKS5,example.com,1080,user,p,over-tls=true,sni=t.example.com',
 'WireGuard,private-key=private,interface-ip=192.0.2.2,interface-ipv6=2001:db8::2,dns=192.0.2.53,dnsv6=2001:db8::53,mtu=1420,keepalive=25,peers=[{public-key=public,preshared-key=psk,endpoint=[2001:db8::1]:51820,allowed-ips="0.0.0.0/0,::/0",reserved=[1,2,3]}]',
]
for client, protocols in (('surge', surge), ('loon', loon), ('shadowrocket', surge)):
    for index, protocol in enumerate(protocols):
        add(f'{client}-protocol-{index}', client, '[Proxy]\nNode = ' + protocol + '\n[Proxy Group]\nPool = select,Node,DIRECT\n[Rule]\nFINAL,Pool\n', catalog={})
    for bad in ('http,example.com,0', 'http,example.com,65536', 'http,example.com,abc', 'http,example.com',
                'unknown,example.com,80', 'wireguard,section-name=Missing', 'vmess,example.com,443,auto,id,transport=grpc'):
        add(f'{client}-bad-{bad}', client, '[Proxy]\nBad = ' + bad + '\n[Proxy Group]\nPool = select,DIRECT\n[Rule]\nFINAL,Pool')

for client, content in (('mihomo','not: [valid'), ('mihomo','[]'), ('mihomo','null'), ('mihomo','{}'),
                        ('surge','without sections'), ('loon','  '), ('unknown','[General]'),
                        ('mihomo','a: 1\n---\nb: 2'), ('mihomo','\ufeff')):
    add('invalid-' + repr(content), client, content, catalog={})

add('duplicates-and-references','mihomo','''
proxies:
- {name: Same, type: http, server: example.com, port: 80}
- {name: Same, type: http, server: example.com, port: 81}
- {name: Builtin, type: reject}
- {name: Direct, type: direct}
- {name: DNS, type: dns}
- {name: Existing, type: http, server: example.com, port: 80, extra-option: {future: true}}
proxy-providers:
  First: {type: http, url: https://feed.example/sub}
  Second: {type: http, url: https://feed.example/sub}
  Bad: {type: file, path: ignored}
proxy-groups:
- {name: Pool, type: select, proxies: [Same, Same, Builtin, Direct, DNS, Missing], use: [First, Second, Missing]}
- {name: Pool, type: select, proxies: [DIRECT]}
- {name: DIRECT, type: select, proxies: [DIRECT]}
- {name: Empty, type: fallback}
- {name: All, type: load-balance, include-all-proxies: true, filter: '[', tolerance: 4, hidden: true}
rule-providers:
  unused: {type: http, url: https://rules.example/unused.list}
rules:
- DOMAIN,example.com,Existing
- MATCH,Pool
''', catalog={'nodes': [{'id':'reuse','name':'Existing','type':'http','server':'example.com','port':80,
                       'params':{'extra-option':{'future':True}},'enabled':False}]})
add('inline-and-library-reuse','mihomo','''
rule-providers:
  Inline: {type: inline, payload: ['DOMAIN,ad.example.com'], behavior: classical}
  Reused: {type: http, url: https://rules.example/reuse.list}
  Missing: {type: unsupported}
rules:
- RULE-SET,Inline,DIRECT
- RULE-SET,Inline,REJECT
- RULE-SET,Reused,DIRECT
- RULE-SET,Missing,DIRECT
''', catalog={'rule_library':[{'id':'existing','name':'Other','url':'https://rules.example/reuse.list'}]})
add('warning-summary', 'surge', '[Proxy]\n' + '\n'.join(f'N{i}=unknown,example.com,80' for i in range(12)) + '\n[Rule]\nFINAL,DIRECT')
add('wireguard-complete','surge','''[Proxy]
WG=wireguard,section-name=Home
[WireGuard Home]
private-key=private
self-ip=192.0.2.2
self-ip-v6=2001:db8::2
dns-server=192.0.2.53,2001:db8::53
mtu=1420
peer=(public-key=public,endpoint=[2001:db8::1]:51820,preshared-key=psk,keepalive=25,client-id=reserved,allowed-ips="0.0.0.0/0,::/0")
[Proxy Group]
Pool=select,WG
[Rule]
FINAL,Pool
''', catalog={})
add('combined-regex-groups','loon','''[Remote Proxy]
Feed=https://feed.example/sub
Off=https://feed.example/off,enabled=false
[Remote Filter]
One=NameRegex,Feed,filterkey=HK
Two=NameRegex,filterkey=JP
Unsupported=Country,Feed
[Proxy Group]
Pool=select,One,Two,Feed,DIRECT
Other=load-balance,Pool,include-other-group=Pool,algorithm=pcc,tolerance=20
Smart=smart,DIRECT,policy-path=https://feed.example/other,policy-regex-filter=US
[Rule]
FINAL,Pool
FINAL,DIRECT
[Remote Rule]
local.list,policy=Pool
https://rules.example/missing-policy.list
https://rules.example/a.list,policy=Pool,tag=Same
https://rules.example/b.list,policy=Pool,tag=Same
''', catalog={})

output = Path(__file__).with_name('corpus.jsonl')
with output.open('w') as stream:
    for row in rows:
        counter = [0]
        def next_id(prefix):
            counter[0] += 1
            return f'{prefix}_{counter[0]:012x}'
        reference._new_id = next_id
        try:
            plan = reference.parse_client_config(row['client'], row['content'])
            row['expected'] = reference.build_import(plan, row['shared'], row['defaults'])
        except Exception as error:
            row['error'] = str(error)
        stream.write(json.dumps(row, ensure_ascii=False) + '\n')
print(f'{len(rows)} synthetic parse/build cases')
