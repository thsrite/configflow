"""Transport differential for rule_fetch.request_rule (review C14): real
requests 2.31 / urllib3 2.8 against a scripted raw HTTP server on 127.0.0.1.
Each scenario lists the raw bytes the server sends per connection and what it
does afterwards (close, reset, hang); the result (status, body, url, or the
exception class and text) and every request head the server saw are recorded.
fetch_c14_test.go replays the scenarios against the Go client with the same
server. Loopback only, synthetic values.

Run: PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_fetch_c14.py
"""
import base64
import gzip
import json
import os
import socket
import struct
import threading
import types
import zlib

from backend.utils import rule_fetch as rf

HEADERS = ('host', 'proxy-authorization', 'accept-encoding', 'accept', 'user-agent', 'authorization')


class RawServer:
    def __init__(self, script):
        self.script = script
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.sock.bind(('127.0.0.1', 0))
        self.sock.listen(16)
        self.port = self.sock.getsockname()[1]
        self.seen = []
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.thread.start()

    def run(self):
        index = 0
        self.sock.settimeout(0.2)
        while not self.stop.is_set():
            try:
                conn, _ = self.sock.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            step = self.script[index] if index < len(self.script) else {
                'raw': 'HTTP/1.1 500 Unscripted\r\nContent-Length: 0\r\n\r\n', 'then': 'close'}
            index += 1
            threading.Thread(target=self.serve, args=(conn, step), daemon=True).start()

    def serve(self, conn, step):
        conn.settimeout(5)
        data = b''
        while b'\r\n\r\n' not in data and len(data) < 65536:
            try:
                chunk = conn.recv(4096)
            except OSError:
                break
            if not chunk:
                break
            data += chunk
        head = data.split(b'\r\n\r\n', 1)[0].decode('latin-1').split('\r\n')
        entry = {'line': head[0].replace(str(self.port), '{PORT}')}
        for line in head[1:]:
            name, _, value = line.partition(':')
            if name.strip().lower() in HEADERS:
                entry[name.strip().lower()] = value.strip().replace(str(self.port), '{PORT}')
        self.seen.append(entry)
        raw = step['raw']
        payload = base64.b64decode(raw[4:]) if raw.startswith('b64:') else raw.replace('{PORT}', str(self.port)).encode('latin-1')
        try:
            conn.sendall(payload)
            if step['then'] == 'reset':
                conn.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack('ii', 1, 0))
            elif step['then'] == 'hang':
                self.stop.wait(3)
        except OSError:
            pass
        conn.close()

    def close(self):
        self.stop.set()
        self.sock.close()


def b64(data):
    return 'b64:' + base64.b64encode(data).decode()


OK = 'HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello'


def redirect(location, status='302 Found', body='', extra=''):
    return f'HTTP/1.1 {status}\r\nLocation: {location}\r\n{extra}Content-Length: {len(body)}\r\n\r\n{body}'


gz = gzip.compress(b'gzip body')
zl = zlib.compress(b'zlib body')


def encoded(encoding, body):
    if isinstance(body, str):
        body = body.encode()
    return b64(b'HTTP/1.1 200 OK\r\nContent-Encoding: ' + encoding.encode() + b'\r\nContent-Length: %d\r\n\r\n' % len(body) + body)


DECODING = [('gzip truncated', 'gzip', gz[:-6]), ('gzip header only', 'gzip', gz[:5]), ('gzip trailing garbage', 'gzip', gz + b'garbage!'),
            ('gzip two members', 'gzip', gz + gzip.compress(b' second')), ('gzip bad crc', 'gzip', gz[:-8] + b'\x00\x00\x00\x00' + gz[-4:]),
            ('gzip bad second crc', 'gzip', gz + gzip.compress(b'two')[:-8] + b'\x00' * 4 + gzip.compress(b'two')[-4:]),
            ('gzip empty', 'gzip', b''), ('gzip upper', 'GZIP', gz), ('gzip spaced', ' gzip', gz), ('x-gzip', 'x-gzip', gz),
            ('zstd left alone', 'zstd', b'\x28\xb5\x2f\xfdraw'), ('br left alone', 'br', b'brotli?'), ('identity', 'identity', b'plain'),
            ('gzip then zstd', 'gzip, zstd', gz), ('zstd then gzip', 'zstd, gzip', gz), ('deflate, gzip', 'deflate, gzip', gzip.compress(zl)),
            ('gzip,', 'gzip,', gz), ('six codings', 'gzip, gzip, gzip, gzip, gzip, gzip', gz), ('deflate zlib', 'deflate', zl),
            ('deflate truncated', 'deflate', zl[:-5]), ('deflate bad adler', 'deflate', zl[:-4] + b'\x00\x00\x00\x00'),
            ('deflate trailing', 'deflate', zl + b'trailing'), ('deflate garbage', 'deflate', b'not deflate at all'),
            ('deflate one byte', 'deflate', b'x'), ('deflate fdict', 'deflate', b'\x78\xbb' + zl[2:]),
            ('two content-encoding fields', None, gz)]
DECODING_SCENARIOS = []
for name, encoding, body in DECODING:
    if encoding is None:
        raw = b64(b'HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nContent-Encoding: zstd\r\nContent-Length: %d\r\n\r\n' % len(body) + body)
    else:
        raw = encoded(encoding, body)
    DECODING_SCENARIOS.append({'name': name, 'script': [{'raw': raw, 'then': 'close'}] * 3})
    DECODING_SCENARIOS.append({'name': name + ' stream', 'opts': {'stream': True}, 'script': [{'raw': raw, 'then': 'close'}] * 3})
SCENARIOS = DECODING_SCENARIOS + [
    {'name': 'plain', 'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'head', 'opts': {'method': 'head'}, 'script': [{'raw': 'HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\n', 'then': 'close'}]},
    {'name': 'utf8 location', 'script': [{'raw': b64(b'HTTP/1.1 301 Moved\r\nLocation: /\xc3\xa9t\xc3\xa9?q=\xe4\xb8\xad\r\nContent-Length: 0\r\n\r\n'), 'then': 'close'},
                                          {'raw': OK, 'then': 'close'}]},
    {'name': 'latin1 location', 'script': [{'raw': b64(b'HTTP/1.1 302 Found\r\nLocation: /caf\xe9\r\nContent-Length: 0\r\n\r\n'), 'then': 'close'},
                                            {'raw': OK, 'then': 'close'}]},
    {'name': 'latin1 location stream', 'opts': {'stream': True},
     'script': [{'raw': b64(b'HTTP/1.1 302 Found\r\nLocation: /caf\xe9\r\nContent-Length: 0\r\n\r\n'), 'then': 'close'},
                {'raw': OK, 'then': 'close'}]},
    {'name': 'no location', 'script': [{'raw': 'HTTP/1.1 302 Found\r\nContent-Length: 0\r\n\r\n', 'then': 'close'}]},
    {'name': 'empty location', 'script': [{'raw': 'HTTP/1.1 302 Found\r\nLocation: \r\nContent-Length: 0\r\n\r\n', 'then': 'close'}]},
    {'name': 'two locations', 'script': [{'raw': 'HTTP/1.1 302 Found\r\nLocation: /a\r\nLocation: /b\r\nContent-Length: 0\r\n\r\n', 'then': 'close'},
                                         {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect body cut', 'script': [{'raw': 'HTTP/1.1 302 Found\r\nLocation: /next\r\nContent-Length: 100\r\n\r\nshort', 'then': 'close'},
                                             {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect body cut stream', 'opts': {'stream': True},
     'script': [{'raw': 'HTTP/1.1 302 Found\r\nLocation: /next\r\nContent-Length: 100\r\n\r\nshort', 'then': 'close'},
                {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect body stalls', 'script': [{'raw': 'HTTP/1.1 302 Found\r\nLocation: /next\r\nContent-Length: 100\r\n\r\nshort', 'then': 'hang'},
                                                {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect bad chunk', 'script': [{'raw': 'HTTP/1.1 302 Found\r\nLocation: /next\r\nTransfer-Encoding: chunked\r\n\r\nzz\r\n', 'then': 'close'},
                                              {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect bad gzip', 'script': [{'raw': 'HTTP/1.1 302 Found\r\nLocation: /next\r\nContent-Encoding: gzip\r\nContent-Length: 4\r\n\r\nnope', 'then': 'close'},
                                             {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect chunked cut', 'script': [{'raw': 'HTTP/1.1 302 Found\r\nLocation: /next\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nsh', 'then': 'close'}] * 3},
    {'name': 'redirect body cut https proxy path', 'opts': {'max_bytes': 1000, 'stream': True},
     'script': [{'raw': 'HTTP/1.1 302 Found\r\nLocation: /next\r\nContent-Length: 100\r\n\r\nshort', 'then': 'close'},
                {'raw': OK, 'then': 'close'}]},
    {'name': 'no location body cut', 'script': [{'raw': 'HTTP/1.1 302 Found\r\nContent-Length: 100\r\n\r\nshort', 'then': 'close'}] * 3},
    {'name': 'body cut', 'script': [{'raw': 'HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort', 'then': 'close'}] * 3},
    {'name': 'body cut then ok', 'script': [{'raw': 'HTTP/1.1 200 OK\r\nContent-Length: 100\r\n\r\nshort', 'then': 'close'},
                                            {'raw': OK, 'then': 'close'}]},
    {'name': 'reset', 'script': [{'raw': '', 'then': 'reset'}] * 3},
    {'name': 'closed before response', 'script': [{'raw': '', 'then': 'close'}] * 3},
    {'name': 'bad chunk length', 'script': [{'raw': 'HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nzz\r\n', 'then': 'close'}] * 3},
    {'name': 'chunked cut', 'script': [{'raw': 'HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhel', 'then': 'close'}] * 3},
    {'name': 'bad gzip', 'script': [{'raw': 'HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nContent-Length: 4\r\n\r\nnope', 'then': 'close'}] * 3},
    {'name': 'gzip', 'script': [{'raw': b64(b'HTTP/1.1 200 OK\r\nContent-Encoding: gzip\r\nContent-Length: %d\r\n\r\n' % len(gz) + gz), 'then': 'close'}]},
    {'name': 'raw deflate', 'script': [{'raw': b64(b'HTTP/1.1 200 OK\r\nContent-Encoding: deflate\r\nContent-Length: 9\r\n\r\n' + zlib.compress(b'abc')[2:-4] + b'\x00' * (9 - len(zlib.compress(b'abc')[2:-4]))), 'then': 'close'}]},
    {'name': 'not found', 'script': [{'raw': 'HTTP/1.1 404 Not Found\r\nContent-Length: 3\r\n\r\nnah', 'then': 'close'}]},
    {'name': 'not found bounded', 'opts': {'max_bytes': 100, 'stream': True},
     'script': [{'raw': 'HTTP/1.1 404 Not Found\r\nContent-Length: 3\r\n\r\nnah', 'then': 'close'}]},
    {'name': 'odd reason', 'script': [{'raw': b64(b'HTTP/1.1 503 Sp\xe9cial reason\r\nContent-Length: 0\r\n\r\n'), 'then': 'close'}],
     'opts': {'max_bytes': 10, 'stream': True}},
    {'name': 'too large', 'opts': {'max_bytes': 3, 'stream': True}, 'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'exact size', 'opts': {'max_bytes': 5, 'stream': True}, 'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'too many redirects', 'script': [{'raw': redirect('/r%d' % i), 'then': 'close'} for i in range(5)]},
    {'name': 'max redirects 0', 'opts': {'max_redirects': 0}, 'script': [{'raw': redirect('/r'), 'then': 'close'}]},
    {'name': 'redirect 303', 'script': [{'raw': redirect('/see', '303 See Other'), 'then': 'close'}, {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect relative', 'url': 'http://127.0.0.1:{PORT}/a/b/c?x=1',
     'script': [{'raw': redirect('../d?y=2'), 'then': 'close'}, {'raw': redirect('?z'), 'then': 'close'}, {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect scheme relative', 'script': [{'raw': redirect('//127.0.0.1:{PORT}/s'), 'then': 'close'}, {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect requote', 'script': [{'raw': redirect('/a b"c<d>|e^f`g{h}%7Ei%41%zz'), 'then': 'close'}, {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect bad port', 'script': [{'raw': redirect('http://127.0.0.1:99999/x'), 'then': 'close'}]},
    {'name': 'redirect bad ipv6', 'script': [{'raw': redirect('http://[::1/x'), 'then': 'close'}]},
    {'name': 'redirect ftp', 'script': [{'raw': redirect('ftp://127.0.0.1/x'), 'then': 'close'}]},
    {'name': 'redirect fragment', 'script': [{'raw': redirect('/frag#part'), 'then': 'close'}, {'raw': OK, 'then': 'close'}]},
    {'name': 'redirect fullwidth', 'script': [{'raw': b64('HTTP/1.1 302 Found\r\nLocation: http://a／b/x\r\nContent-Length: 0\r\n\r\n'.encode()), 'then': 'close'}]},
    {'name': 'url requote', 'url': 'http://127.0.0.1:{PORT}/p a"t%7eh?q=a b&r=%zz', 'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'url empty path', 'url': 'http://127.0.0.1:{PORT}', 'script': [{'raw': 'HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n', 'then': 'close'}],
     'opts': {'max_bytes': 10, 'stream': True}},
    {'name': 'url userinfo', 'url': 'http://us%40er:p%3Ass@127.0.0.1:{PORT}/u', 'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'proxy auth', 'url': 'http://rules.test/list', 'proxy': 'http://user:pa%20ss@127.0.0.1:{PORT}',
     'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'proxy user only', 'url': 'http://rules.test/list', 'proxy': 'http://user@127.0.0.1:{PORT}',
     'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'proxy empty user', 'url': 'http://rules.test/list', 'proxy': 'http://:pw@127.0.0.1:{PORT}',
     'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'proxy bad escape', 'url': 'http://rules.test/list', 'proxy': 'http://u%zz:p%41@127.0.0.1:{PORT}',
     'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'proxy latin1', 'url': 'http://rules.test/list', 'proxy': 'http://us\xe9r:p\xe4ss@127.0.0.1:{PORT}',
     'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'proxy non latin1', 'url': 'http://rules.test/list', 'proxy': 'http://中:pw@127.0.0.1:{PORT}',
     'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'proxy redirect', 'url': 'http://rules.test/a', 'proxy': 'http://u:p@127.0.0.1:{PORT}',
     'script': [{'raw': redirect('http://other.test/b'), 'then': 'close'}, {'raw': OK, 'then': 'close'}]},
    {'name': 'proxy 407', 'url': 'http://rules.test/a', 'proxy': 'http://127.0.0.1:{PORT}',
     'script': [{'raw': 'HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\n\r\n', 'then': 'close'}]},
    {'name': 'proxy refused', 'url': 'http://rules.test/a', 'proxy': 'http://127.0.0.1:1', 'script': []},
    {'name': 'refused', 'url': 'http://127.0.0.1:1/x', 'script': []},
    {'name': 'invalid proxy setting', 'url': 'http://rules.test/a', 'proxy': 'http://127.0.0.1:{PORT}/path', 'script': []},
    {'name': 'internal url without app', 'url': 'http://127.0.0.1:{PORT}/api/rules', 'server_domain': 'http://127.0.0.1:{PORT}',
     'proxy': 'http://127.0.0.1:1', 'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'internal redirect leaves', 'url': 'http://127.0.0.1:{PORT}/api/rules', 'server_domain': 'http://127.0.0.1:{PORT}',
     'script': [{'raw': redirect('/elsewhere'), 'then': 'close'}]},
    {'name': 'missing ca bundle http', 'opts': {'verify': '/nonexistent/ca.pem'}, 'script': [{'raw': OK, 'then': 'close'}]},
    {'name': 'missing ca bundle https', 'url': 'https://127.0.0.1:{PORT}/x', 'opts': {'verify': '/nonexistent/ca.pem'}, 'script': []},
    {'name': 'missing ca bundle https proxy', 'url': 'http://rules.test/a', 'proxy': 'https://127.0.0.1:{PORT}',
     'opts': {'verify': '/nonexistent/ca.pem'}, 'script': []},
]


def run(scenario):
    server = RawServer(scenario['script'])
    port = str(server.port)
    url = scenario.get('url', 'http://127.0.0.1:{PORT}/rules.list').replace('{PORT}', port)
    system = {}
    if 'proxy' in scenario:
        system['rule_fetch_proxy'] = scenario['proxy'].replace('{PORT}', port)
    if 'server_domain' in scenario:
        system['server_domain'] = scenario['server_domain'].replace('{PORT}', port)
    opts = dict(scenario.get('opts', {}))
    result = {}
    try:
        response = rf.request_rule(url, config_data={'system_config': system}, timeout=(1, 1), **opts)
        if opts.get('stream') and not opts.get('max_bytes'):
            body = b''.join(response.iter_content(65536))
        else:
            body = response.content
        result['ok'] = {'status': response.status_code, 'reason': response.reason, 'url': response.url.replace(port, '{PORT}'),
                        'body': body.hex()}
        response.close()
    except Exception as exc:  # noqa: BLE001
        result['err'] = {'class': type(exc).__name__, 'text': str(exc).replace(port, '{PORT}')}
    server.close()
    return {**scenario, 'result': result, 'seen': server.seen}


rf.time = types.SimpleNamespace(sleep=lambda seconds: None)
rows = [run(scenario) for scenario in SCENARIOS]
out = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'fetch_c14.json')
with open(out, 'w') as fh:
    json.dump(rows, fh, ensure_ascii=False, indent=1)
for row in rows:
    print(row['name'], json.dumps(row['result'], ensure_ascii=False)[:160], len(row['seen']))
