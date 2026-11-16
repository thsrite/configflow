"""A10 review corpus: CPython references of generate_guide for the three Compose
methods (compose bundle, compose_direct, compose_compact), in the format of
a09_record.py, replayed by TestComposeReviewCorpusMatchesPython. The porter's
corpus calls the generators directly; this one goes through generate_guide
(validation order, _new_install before generate_bundle, create_host_identity
handling) with synthetic values only.

    PYTHONPATH=.:server/internal/installer/testdata ~/cf-test/.venv/bin/python \
        server/internal/installer/testdata/gen_review_a10.py [N]

Free-text values (data directory, identity path, server URL, interface name,
enrollment URL/token, image) are full of shell metacharacters, quotes,
backslashes, heredoc markers, Unicode line breaks (U+0085, U+2028, U+2029),
C0/C1 controls and YAML indicators; identity paths alias every managed part
of the data directory and its prefixes; images are YAML-ambiguous scalars;
names are long/upper-case for the compact project name.
"""
import gzip
import json
import os
import random
import sys
from pathlib import Path

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import a09_record as rec  # noqa: E402

rec._install()
from backend.agents.docker_ha_guide import generate_guide  # noqa: E402

N = int(sys.argv[1]) if len(sys.argv) > 1 else 3000
R = random.Random(20261118)
META = list("'\"$`\\;|&<>(){}[]*?!#~%^= \t") + ['$(id)', '`id`', '${HOME}', "'\"'\"'", 'EOF', 'CF_EOF', '$$', '\\n',
                                                '\x85', ' ', ' ', '\x0b', '\x0c', '\x1c', '\x1e', '\x7f', '\x1b', '\x9b',
                                                '﻿', '　', '\xa0', 'é', '例', '😀', 'İ', '- ', ': ', '#x', '&a', '*a', '!!str',
                                                '{{', '}}', '{{.Name}}', '%s', '\\x00']
SAFE = [c for c in META + list('abcXYZ019_-.') if c not in (',', ':', '\x00', '\r', '\n', '/')]
MANAGED = ['agent', 'oxidns', 'ha', '.ha-compose', '.ha-install', 'compose.yaml', 'prepare.sh', 'bootstrap.py',
           'enrollment.json', 'README.txt']


def text(n, pool):
    return ''.join(R.choice(pool) for _ in range(n))


def long_path(colon=False):
    """Long paths with spaces and YAML indicators: line folding at width 80/1000."""
    words = ['data', 'a b', 'x', '例子', '😀', '- y', '#z', "'q'", '"d"', '$V', '`c`', 'é', '\u2028', '\x85', '  ', '!t', '&a', '*b']
    words += ['k: v', 'a:b'] if colon else []
    target = R.choice([79, 80, 81, 120, 300, 999, 1000, 1001, 1500, 3000])
    text_ = '/srv/'
    while len(text_) < target:
        text_ += R.choice(words) + R.choice([' ', '', '/', '_'])
    text_ = text_[:target].rstrip('/') or '/srv'
    return text_.replace('//', '/x/').replace('/./', '/x/').replace('/../', '/x/')


def wild_path(colon=False):
    if R.random() < 0.15:
        return long_path(colon)
    pool = SAFE + ([':', 'a:b', '::'] if colon else [])
    parts = []
    for _ in range(R.randrange(1, 4)):
        part = text(R.randrange(1, 8), pool)
        if part in ('.', '..'):
            part = 'x' + part
        parts.append(part)
    return '/' + '/'.join(parts)


def data_dir(method):
    roll = R.random()
    if roll < 0.12:
        return R.choice(['/etc', '/etc/cf', '/usr/x', '/proc', '/run/cf', '/boot', '/var', '/var/lib', '/opt', '/srv', '/home',
                         '/root', '/tmp', '/volume1', '/volume1/docker', '/volume12/docker', '/volume1/docker/cf', '/volume/docker',
                         '/volume٣', '/volume1/Docker', '/var/lib/cf', '/opt/cf', '/sys', '/dev/x', '/sbin', '/bin/x',
                         '/etcetera', '/runs', '/srv/a:b', '/srv/' + 'x' * 4000, '/srv/a,b', '/srv/a\nb', '/srv/./a', '/srv//a', 'srv'])
    return wild_path(colon=method == 'compose_compact' and R.random() < 0.4)


def identity_path(data):
    roll = R.random()
    if roll < 0.35:
        part = R.choice(MANAGED)
        return R.choice([data, data + '/' + part, data + '/' + part + '/id', data + '/' + part + 'x', data + 'x',
                         data + '/' + part + '/' + part, data + '/id', data[:-1] if len(data) > 2 else data])
    if roll < 0.9:
        return wild_path()
    return R.choice([None, 5, True, [], {}, '', 'rel/id', '/', '/a/../b', '/a:b', '/a,b', '/a\nb'])


def server():
    host = R.choice(['h.test', 'x.test:8000', '[::1]', '[2001:db8::1]:9', '例子.测试', 'x.test:65535'])
    path = text(R.randrange(0, 12), [c for c in META + list('abc019@[]') if c not in
                                      (' ', '\t', '\x85', ' ', ' ', '﻿', '　', '\xa0', '\x0b', '\x0c',
                                       '\x1c', '\x1e', '?', '#', '- ', ': ', '#x', '!!str')])
    url = 'http' + R.choice(['', 's']) + '://' + host + '/' + path + R.choice(['', '/', '//'])
    if R.random() < 0.05:
        url = R.choice(['ftp://h.test', 'http://u:p@h.test', 'http://h.test/?q', 'http://h.test/#f', 'http://', 5, None,
                        'http://h.test/\x85', 'http://h.test/ '])
    return url


def image():
    return R.choice(['thsrite/config-flow-agent:latest', 'true', 'null', 'yes', 'on', 'off', '1e3', '0x1A', '0o17', '1.0',
                     '2001', '1_000', '12:30', '1:20:30', 'a:b', 'reg.test:5000/a/b@sha256:' + 'ab' * 32, 'a-' * 100,
                     'Y', 'n', '.inf', 'a' * 255, 'a' * 256, '-a', 'a b', '~', 'NaN', 'x' + text(R.randrange(0, 20), list('abcXYZ019._:/@-'))])


def agent_name():
    return R.choice(['dns-one', 'DNS.Upper_case-9', 'A' * 128, 'B' * 33 + 'c', 'x' * 129, 'a.b.c', '9z', '-x', 'é', 5, None,
                     'x' + text(R.randrange(0, 40), list('abcXYZ019_.-')), 'dns\n'])


def enrollment(srv):
    if R.random() < 0.25:
        return None
    base = srv if isinstance(srv, str) else 'http://h.test'
    url = base.rstrip('/') + '/api/agents/docker-oxidns-ha-enroll/' + ('ab' * 16 if R.random() < 0.8 else R.choice(['AB' * 16, 'ab' * 15, 'ab' * 17, 'g' * 32]))
    token = 'T' * 43 if R.random() < 0.7 else R.choice(['a-_' * 14 + 'z', 'T' * 42, 'T' * 44, 'T' * 42 + '=', text(43, list('abc019_-$`\'"'))])
    if R.random() < 0.05:
        url = R.choice(['', 'no-slash', base + '/x/' + 'ab' * 16 + '/', base + "/'$(id)'/" + 'ab' * 16])
    return {'url': url, 'token': token, 'expires_at': 1800000000}


def case():
    method = R.choice(['compose', 'compose_direct', 'compose_compact'])
    mode = R.choice(['host', 'macvlan'])
    data = wild_path(colon=method == 'compose_compact' and R.random() < 0.4)
    body = dict(install_method=method, server_url='http://config.test:8000', agent_name='dns-one', agent_ip='192.168.10.20',
                data_dir=data, network_mode=mode, oxidns_port=8082, ha_host_id_path=wild_path())
    if mode == 'macvlan':
        body.update(ha_parent='eth0', ha_subnet='192.168.10.0/24', ha_gateway=R.choice(['192.168.10.1', '']))
    if R.random() < 0.5:
        body['create_host_identity'] = R.choice([True, False])
    # Valid-biased: wild but mostly acceptable values in 1-3 fields; 25 % also break one field.
    wild = {
        'server_url': lambda: server(),
        'agent_name': lambda: R.choice(['DNS.Upper_case-9', 'A' * 128, 'B' * 33 + 'c', 'a.b.c', '9z', 'İx'[1:] + 'Z' * 40,
                                        'x' + text(R.randrange(0, 40), list('abcXYZ019_.-'))]),
        'agent_ip': lambda: R.choice(['192.0.2.10', 3232238100, '192.168.10.30', '10.0.0.1']),
        'image': lambda: image(),
        'ha_parent': lambda: R.choice(['enp1s0.100', 'br-lan', 'e' * 15, 'eth0' + text(R.randrange(1, 6), META)]),
        'ha_gateway': lambda: R.choice(['192.168.10.1', '', 3232238081, '192.168.10.254']),
        'oxidns_port': lambda: R.choice([1, 65535, 53, 8083, 10000]),
        'ha_host_id_path': lambda: identity_path(data),
        'enrollment': None,
    }
    for key in R.sample(list(wild), R.randrange(1, 4)):
        if key == 'enrollment' or (key in ('ha_parent', 'ha_gateway') and mode != 'macvlan'):
            continue
        body[key] = wild[key]()
    if R.random() < 0.25:
        key = R.choice(['data_dir', 'agent_name', 'agent_ip', 'network_mode', 'oxidns_port', 'image', 'create_host_identity',
                        'ha_subnet', 'ha_gateway', 'ha_parent', 'server_url', 'mode', 'ha_host_id_path'])
        body[key] = {'data_dir': lambda: data_dir(method), 'agent_name': agent_name,
                     'agent_ip': lambda: R.choice(['192.168.10.0', '', 'x', '1.2.3.4\n', None]),
                     'network_mode': lambda: R.choice(['bridge', '', 5]),
                     'oxidns_port': lambda: R.choice([0, 65536, True, '8082', 8082.0, 2 ** 70]),
                     'image': lambda: R.choice(['a' * 256, '-a', 'a b', '~', 5]),
                     'create_host_identity': lambda: R.choice([1, 'yes', None]),
                     'ha_subnet': lambda: R.choice(['192.168.10.0/16', '192.168.10.5/24', '10.0.0.0/8', 'x', '']),
                     'ha_gateway': lambda: R.choice(['192.168.10.20', '192.168.11.1', 'x', None]),
                     'ha_parent': lambda: R.choice(['e' * 16, '', 5, 'a b']),
                     'server_url': lambda: R.choice(['ftp://h.test', 'http://u:p@h.test', 'http://h.test/?q', 5, 'http://h.test/\u2028']),
                     'mode': lambda: R.choice(['migrate', 'other']),
                     'ha_host_id_path': lambda: R.choice([None, 5, '', 'rel/id', '/', '/a/../b', '/a:b'])}[key]()
    return body, enrollment(body['server_url'])


def call(label, function, *args):
    rec._TEST[0] = label
    try:
        function(*args)
    except Exception:  # noqa: BLE001 - recorded
        pass


for i in range(N):
    request, enroll = case()
    call(f'review-a10/guide/{i}', generate_guide, request, enroll)

rows = rec._RECORDS
# Keep the corpus small: the bundle zip is stored as its sha256 (content_sha256,
# moved to the end); the Go replay digests its own archive the same way. The
# zip/deflate port itself is compared byte for byte by
# TestReviewDeflateAndZipMatchPython and the porter's corpus.
for e in rows:
    bundle = (e.get('out') or {}).get('compose_bundle') if isinstance(e.get('out'), dict) else None
    if isinstance(bundle, dict) and 'content_base64' in bundle:
        import base64 as _b64
        import hashlib as _hashlib
        bundle['content_sha256'] = _hashlib.sha256(_b64.b64decode(bundle.pop('content_base64'))).hexdigest()
out = Path(__file__).with_name('docker_ha_compose_review.jsonl.gz')
with open(out, 'wb') as raw_file:
    with gzip.GzipFile(fileobj=raw_file, mode='wb', mtime=0) as fh:
        fh.write(('\n'.join(json.dumps(e, ensure_ascii=False) for e in rows) + '\n').encode())
answered = sum(1 for e in rows if 'out' in e or 'result' in e)
print(f'{len(rows)} calls -> {out}; not recordable: {rec._SKIPPED}')
