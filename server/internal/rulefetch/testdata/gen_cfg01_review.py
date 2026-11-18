"""CFG-01 independent security review oracle: real Python, loopback and synthetic hosts only.

Run only through rt from the repository root:
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_cfg01_review.py

network: real request_rule against loopback fakes. A request whose path ends in
/landing (on either fake) is a forbidden destination.
classify: real is_internal_rule_url for host spellings that cannot be dialled
without DNS (IDN, trailing dot, numeric IPv4 forms); no request is made.
"""
import json
import socket
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

from backend.utils.rule_fetch import is_internal_rule_url, request_rule

ESCAPE = ['{external}/landing']
network = []


def add(name, start, src=(), ext=(), domain='{origin}', family='v4', status=302):
    network.append(dict(name=name, start=start, source_locations=list(src), external_locations=list(ext),
                        server_domain=domain, family=family, status=status))


# Initial URL spellings that must not leave the API before the first redirect.
for name, path in [
    ('init-dot-slash-dot', '/api/./../redirect'), ('init-encoded-single-dot', '/api/%2e/../redirect'),
    ('init-half-encoded', '/api/.%2e/redirect'), ('init-mixed-case', '/api/%2E%2e/redirect'),
    ('init-double-upper', '/api/%252E%252E/redirect'), ('init-triple', '/api/%25252e%25252e/redirect'),
    ('init-encoded-slash', '/api/..%2fredirect'), ('init-encoded-slash-upper', '/api/..%2Fredirect'),
    ('init-encoded-backslash-lead', '/api/%5c..%5credirect'), ('init-double-slash', '/api//../redirect'),
    ('init-many-slashes', '/api///../../redirect'), ('init-semicolon', '/api/..;/redirect'),
    ('init-fragment', '/api/../redirect#frag'),
]:
    add(name, '{origin}' + path, ESCAPE)
add('init-userinfo', 'http://user:secret@{host}/api/../redirect', ESCAPE)
add('init-port-leading-zero', 'http://{hostname}:0{port}/api/../redirect', ESCAPE)
add('init-localhost-case', 'http://LocalHost:{port}/api/../redirect', ESCAPE, domain='http://localhost:{port}')
add('alias-ip-for-localhost', '{origin}/api/../redirect', ESCAPE, domain='http://localhost:{port}')
add('v6-same', '{origin}/api/../redirect', ESCAPE, family='v6')
add('v6-expanded', 'http://[0:0:0:0:0:0:0:1]:{port}/api/../redirect', ESCAPE, family='v6')

# Redirect targets from inside the API.
for name, location in [
    ('redir-dot-slash-dot', '/api/./../landing'), ('redir-encoded-mixed', '/api/%2E%2e/landing'),
    ('redir-encoded-slash', '/api/..%2flanding'), ('redir-encoded-backslash', '/api/..%5Clanding'),
    ('redir-scheme-relative', '//{ehost}/landing'), ('redir-backslash-scheme-relative', '\\\\{ehost}/landing'),
    ('redir-slash-backslash', '/\\{ehost}/landing'), ('redir-userinfo-external', 'http://{host}@{ehost}/landing'),
    ('redir-file', 'file:///landing'), ('redir-data', 'data:text/plain,landing'),
    ('redir-gopher', 'gopher://{ehost}/landing'), ('redir-ftp', 'ftp://{ehost}/landing'),
    ('redir-http-colon-slash', 'http:/landing'), ('redir-relative-up', '../landing'),
    ('redir-tab', '/api/\t../landing'),
]:
    add(name, '{origin}/api/start', [location])
for status in (301, 303, 307, 308):
    add(f'redir-status-{status}', '{origin}/api/start', ESCAPE, status=status)
add('redir-hop-4-dot', '{origin}/api/start', ['/api/s1', '/api/s2', '/api/s3', '/api/../landing'])
add('redir-hop-3-external', '{origin}/api/start', ['/api/s1', '/api/s2', '{external}/landing'])
add('redir-http-colon-relative', '{origin}/api/start', ['http:ok'])
add('redir-userinfo-same', '{origin}/api/start', ['http://user@{host}/api/ok'])
add('redir-port-leading-zero-same', '{origin}/api/start', ['http://{hostname}:0{port}/api/ok'])
add('redir-absolute-dot-back-into-api', '{origin}/api/start', ['{origin}/api/../api/ok'])
add('redir-fragment', '{origin}/api/start', ['/api/ok#/../../landing'])
add('redir-query-nested', '{origin}/api/start', ['/api/ok?next={external}/landing'])
add('redir-relative-ok', '{origin}/api/start', ['ok'])

# An external source redirecting into the API with an un-normalized absolute Location.
add('ext-into-api-dot', '{external}/source', ESCAPE, ['{origin}/api/../redirect'])
add('ext-into-api-scheme-relative', '{external}/source', ESCAPE, ['//{host}/api/../redirect'])
add('ext-into-api-encoded', '{external}/source', ESCAPE, ['{origin}/api/%2e%2e/redirect'])
add('ext-into-api-ok', '{external}/source', [], ['{origin}/api/ok'])

# A server_domain served below a path prefix.
add('prefix-ok', '{origin}/cf/api/ok', domain='{origin}/cf')
add('prefix-redirect-outside', '{origin}/cf/api/start', ['/api/landing'], domain='{origin}/cf')
add('prefix-dot-redirect', '{origin}/cf/api/../redirect', ESCAPE, domain='{origin}/cf')
add('prefix-dot-out-no-redirect', '{origin}/cf/api/../../api/ok', domain='{origin}/cf')

# Ordinary sources that must keep working.
add('legit-query', '{origin}/api/rules?url=http%3A%2F%2Fexample.com%2Fa&x=1')
add('legit-encoded-slash', '{origin}/api/a%2Fb')
add('legit-percent', '{origin}/api/50%25off')
add('legit-external-query', '{external}/rules.txt?token=abc&v=1')
add('legit-external-redirect', '{external}/source', [], ['/rules.txt'])

classify = []
for name, domain, url in [
    ('idn-upper', 'http://bücher.example', 'http://BÜCHER.example/api/../redirect'),
    ('idn-punycode', 'http://bücher.example', 'http://xn--bcher-kva.example/api/../redirect'),
    ('idn-fullwidth', 'http://panel.example', 'http://ｐａｎｅｌ.example/api/../redirect'),
    ('trailing-dot', 'http://panel.example', 'http://panel.example./api/../redirect'),
    ('ascii-upper', 'http://panel.example', 'http://PANEL.Example/api/../redirect'),
    ('explicit-default-port', 'http://panel.example', 'http://panel.example:80/api/../redirect'),
    ('leading-zero-default-port', 'http://panel.example', 'http://panel.example:080/api/../redirect'),
    ('https-explicit-default-port', 'https://panel.example', 'https://panel.example:443/api/../redirect'),
    ('scheme-mismatch', 'http://panel.example', 'https://panel.example/api/../redirect'),
    ('userinfo', 'http://panel.example', 'http://u:p@panel.example/api/../redirect'),
    ('percent-host', 'http://panel.example', 'http://panel%2Eexample/api/../redirect'),
    ('ipv4-decimal', 'http://127.0.0.1:8080', 'http://2130706433:8080/api/../redirect'),
    ('ipv4-octal', 'http://127.0.0.1:8080', 'http://0177.0.0.1:8080/api/../redirect'),
    ('ipv4-hex', 'http://127.0.0.1:8080', 'http://0x7f.0.0.1:8080/api/../redirect'),
    ('ipv4-short', 'http://127.0.0.1:8080', 'http://127.1:8080/api/../redirect'),
    ('ipv4-any', 'http://127.0.0.1:8080', 'http://0.0.0.0:8080/api/../redirect'),
    ('ipv4-mapped-v6', 'http://127.0.0.1:8080', 'http://[::ffff:127.0.0.1]:8080/api/../redirect'),
    ('localhost-for-ip', 'http://127.0.0.1:8080', 'http://localhost:8080/api/../redirect'),
    ('v6-expanded', 'http://[::1]:8080', 'http://[0:0:0:0:0:0:0:1]:8080/api/../redirect'),
    ('v6-upper', 'http://[fd00::a]:8080', 'http://[FD00::A]:8080/api/../redirect'),
]:
    config = {'system_config': {'server_domain': domain}}
    try:
        internal = is_internal_rule_url(url, config)
        error = None
    except ValueError as exc:
        internal, error = None, str(exc)
    classify.append(dict(name=name, server_domain=domain, url=url, python_internal=internal, python_error=error))
    print('classify', name, internal, error, flush=True)


class V6Server(ThreadingHTTPServer):
    address_family = socket.AF_INET6


for case in network:
    state = {'source_paths': [], 'external_paths': [], 'forbidden': 0}

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            external = self.server is destination
            paths = state['external_paths' if external else 'source_paths']
            locations = case['external_locations' if external else 'source_locations']
            index = len(paths)
            paths.append(self.path)
            status, location = 200, None
            if urlsplit(self.path).path.endswith('/landing'):
                state['forbidden'] += 1
            elif index < len(locations):
                status, location = case['status'], fill(locations[index])
            self.send_response(status)
            if location:
                self.send_header('Location', location)
            self.send_header('Content-Length', '2')
            self.end_headers()
            self.wfile.write(b'ok')

    if case['family'] == 'v6':
        source = V6Server(('::1', 0), Handler)
        hostname = '[::1]'
    else:
        source = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        hostname = '127.0.0.1'
    destination = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    values = {'hostname': hostname, 'port': str(source.server_port),
              'host': f'{hostname}:{source.server_port}', 'ehost': f'127.0.0.1:{destination.server_port}'}
    values['origin'] = 'http://' + values['host']
    values['external'] = 'http://' + values['ehost']

    def fill(text):
        for key in ('origin', 'external', 'hostname', 'host', 'ehost', 'port'):
            text = text.replace('{' + key + '}', values[key])
        return text

    threads = [threading.Thread(target=s.serve_forever, kwargs={'poll_interval': .01}) for s in (source, destination)]
    for thread in threads:
        thread.start()
    outcome = {'refused': False}
    try:
        response = request_rule(fill(case['start']), config_data={'system_config': {'server_domain': fill(case['server_domain'])}},
                                max_redirects=8, timeout=(2, 2))
        outcome.update(status=response.status_code, body=response.text)
        response.close()
    except Exception as error:  # the oracle records every Python outcome
        outcome.update(refused=True, error_class=type(error).__name__,
                       error=str(error).replace(values['ehost'], '{ehost}').replace(values['host'], '{host}'))
    finally:
        for s in (source, destination):
            s.shutdown()
            s.server_close()
        for thread in threads:
            thread.join()
    outcome.update(state)
    case['python'] = outcome
    print(case['name'], json.dumps(outcome), flush=True)

Path(__file__).with_name('cfg01_review.json').write_text(
    json.dumps({'network': network, 'classify': classify}, ensure_ascii=False, indent=2) + '\n')
