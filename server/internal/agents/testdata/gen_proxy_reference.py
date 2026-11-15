"""Proxy choice of requests 2.31 (trust_env=True) for Agent URLs: reference for proxy_test.go.

Each case sets os.environ (in the given order) and asks real requests what a
Session.request to the prepared URL would use: merge_environment_settings
(get_environ_proxies → should_bypass_proxies / getproxies) and
HTTPAdapter.get_connection (select_proxy, prepend_scheme_if_needed, parse_url,
proxy_manager_for with its Proxy-Authorization), with send()'s
LocationValueError → InvalidURL. Synthetic values, loopback only, no traffic.

Run on the test server:
  rt --back server/internal/agents/testdata/proxy_reference.json -- bash -lc \
    '~/cf-test/.venv/bin/python -I server/internal/agents/testdata/gen_proxy_reference.py \
     > server/internal/agents/testdata/proxy_reference.json'
"""
import json
import os

import requests
from requests.adapters import HTTPAdapter
from urllib3.exceptions import LocationValueError

P = 'http://127.0.0.1:3128'

URLS = [
    'http://127.0.0.1:8080/api/status',
    'http://localhost:8080/api/status',
    'http://127.1:8080/api/x',
    'http://0x7f000001:8080/api/x',
    'http://[::1]:8080/api/x',
    'http://[fe80::1%25eth0]:8080/api/x',
    'http://agent.local:80/api/x',
    'http://AGENT.Local:8080/api/x',
    'http://10.1.2.3:8080/api/x',
    'http://192.168.0.5:5000/api/x',
    'http://xn--bcher-kva.example:8080/api/x',
    'http://myhost:0/api/x',
    'http://myhost/api/x',
    'http://127.0.0.1:080/api/x',
    'http://k.example:8080/api/x',
]

NO_PROXY = [
    '127.0.0.1', 'localhost', '127.0.0.0/8', '10.0.0.0/8', '127.0.0.1:8080', '*', '.local', 'local',
    'agent.local', 'LOCALHOST', 'AGENT.LOCAL', ' 127.0.0.1 , example.com', '127.0.0.1/33', '127.0.0.1/0',
    '127.0.0.1/32', '127.0.0.2/32', '127.0.0.1/\uff18', '127.0.0.1/ 8', '127.0.0.1/\t8', '127.0.0.1/+8',
    '127.0.0.1/0_8', '127.1/8', '0x7f.0.0.1/8', '127.0.0.1\t/8', '::1', '[::1]', 'fe80::/10', 'myhost:8080',
    'ost', '*.local', '\tlocalhost', 'localhost\t', '.localhost', '..localhost', '\u212a.example',
    '.\u212a.EXAMPLE', 'host:8080', ',,', ',localhost,',
    '127.0.0.1,,localhost', 'example.com', '192.168.0.0/16', '192.168.0.5', '192.168.0.5:5000', ':5000',
    'myhost:0', 'myhost', 'fe80::1%eth0', 'fe80::1%25eth0', 'xn--bcher-kva.example', 'bücher.example',
    '0.0.0.0/1', '128.0.0.0/1', '127.0.0.1/1/2', '127.0.0.1/', '/8', 'localhost:8080', '127.0.0.1:080',
    '\u3000localhost', 'localhost\u2028',
]

PROXIES = [
    P, '127.0.0.1:3128', 'localhost:3128', 'http://user:pa%40ss@127.0.0.1:3128', 'http://user@127.0.0.1:3128',
    'http://:pw@127.0.0.1:3128', 'http://us%3Aer:p%3Aw@127.0.0.1:3128', 'https://proxy.local',
    'https://proxy.local:8443', 'socks5://127.0.0.1:1080', 'SOCKS5h://127.0.0.1:1080', 'socks://x',
    'ftp://127.0.0.1:21', 'http://:3128', 'http://127.0.0.1:0', 'http://127.0.0.1:99999', 'http://[::1]:3128',
    '//127.0.0.1:3128', 'http://HOST.Example:3128/path?q#f', 'http://\u00fc:pw@127.0.0.1:3128',
    'http://\u7528:pw@127.0.0.1:3128', 'http://127.0.0.1:3128 ', 'HTTP://127.0.0.1:3128', 'http://127.0.0.1:',
    'http://127.0.0.1:003128', '127.0.0.1', 'http://a@b@127.0.0.1:3128', 'http://us er:pw@127.0.0.1:3128',
    'http://127.0.0.1:3128/', 'http:127.0.0.1:3128', 'http://[::1]', '[::1]:3128', 'http://user:@127.0.0.1:3128',
    'http://us%zzer:pw@127.0.0.1:3128', 'http://127.0.0.1:3128?x', '/path', 'h:', 'a.b:3128',
]


def ask(env, url):
    os.environ.clear()
    for key, value in env:
        os.environ[key] = value
    try:
        session = requests.Session()
        settings = session.merge_environment_settings(url, {}, None, None, None)
        adapter = HTTPAdapter()
        try:
            conn = adapter.get_connection(url, settings['proxies'])
        except LocationValueError as error:
            raise requests.exceptions.InvalidURL(error)
        proxy = getattr(conn, 'proxy', None)
        if proxy is None:
            return {'proxy': None}
        return {'proxy': f'{proxy.scheme}://{proxy.host}:{proxy.port}',
                'auth': (conn.proxy_headers or {}).get('Proxy-Authorization')}
    except Exception as error:  # noqa: BLE001 - the class is the reference
        return {'error': type(error).__name__}


def cases():
    yield [], URLS[0]
    for name in ('HTTP_PROXY', 'http_proxy', 'ALL_PROXY', 'all_proxy', 'HTTPS_PROXY', 'Http_Proxy', 'FTP_PROXY'):
        for url in URLS[:3]:
            yield [(name, P)], url
    yield [('http_proxy', ''), ('HTTP_PROXY', P)], URLS[0]
    yield [('HTTP_PROXY', P), ('http_proxy', '')], URLS[0]
    yield [('HTTP_PROXY', 'http://127.0.0.1:1'), ('http_proxy', P)], URLS[0]
    yield [('http_proxy', P), ('HTTP_PROXY', 'http://127.0.0.1:1')], URLS[0]
    yield [('Http_Proxy', 'http://127.0.0.1:1'), ('HTTP_PROXY', P)], URLS[0]
    yield [('HTTP_PROXY', P), ('Http_Proxy', 'http://127.0.0.1:1')], URLS[0]
    yield [('REQUEST_METHOD', 'GET'), ('HTTP_PROXY', P)], URLS[0]
    yield [('REQUEST_METHOD', 'GET'), ('http_proxy', P)], URLS[0]
    yield [('REQUEST_METHOD', 'GET'), ('HTTP_PROXY', P), ('ALL_PROXY', 'http://127.0.0.1:9')], URLS[0]
    yield [('ALL_PROXY', 'http://127.0.0.1:9'), ('HTTP_PROXY', P)], URLS[0]
    yield [('HTTP://127.0.0.1_PROXY', 'http://127.0.0.1:9'), ('HTTP_PROXY', P)], URLS[0]
    yield [('ALL://127.0.0.1_PROXY', 'http://127.0.0.1:9'), ('HTTP_PROXY', P)], URLS[0]
    yield [('_PROXY', 'http://127.0.0.1:9')], URLS[0]
    for value in PROXIES:
        yield [('HTTP_PROXY', value)], URLS[0]
    for no in NO_PROXY:
        for url in URLS:
            yield [('HTTP_PROXY', P), ('NO_PROXY', no)], url
    for url in URLS[:3]:
        yield [('HTTP_PROXY', P), ('no_proxy', ''), ('NO_PROXY', '127.0.0.1,localhost')], url
        yield [('HTTP_PROXY', P), ('NO_PROXY', 'LOCALHOST'), ('no_proxy', 'example.com')], url
        yield [('HTTP_PROXY', P), ('no_proxy', 'example.com'), ('NO_PROXY', 'LOCALHOST')], url
        yield [('HTTP_PROXY', P), ('NO_PROXY', 'example.com'), ('no_proxy', 'LOCALHOST')], url
        yield [('HTTP_PROXY', P), ('No_Proxy', 'LOCALHOST')], url
        yield [('HTTP_PROXY', P), ('NO_PROXY', 'LOCALHOST'), ('no_proxy', '')], url
        yield [('NO_PROXY', '127.0.0.1')], url


print(json.dumps([{'env': env, 'url': url, 'result': ask(env, url)} for env, url in cases()],
                 ensure_ascii=False, indent=0))
