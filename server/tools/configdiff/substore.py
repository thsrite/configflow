"""Strict loopback Sub-Store protocol fake; conversion output is synthetic YAML."""
import copy
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import re
import threading
from urllib.parse import quote, unquote, urlsplit

from compare import Difference, differences
from harness import DesignError
from sources import FEED


class ProtocolError(ValueError):
    pass


class SubStore:
    def __init__(self, body=FEED, download_status=200):
        self.requests, self.events, self.created = [], [], {}
        self.body, self.download_status = body, download_status
        owner = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def handle_request(self):
                raw = self.rfile.read(int(self.headers.get('Content-Length', '0')))
                data = json.loads(raw) if raw else None
                record = {'method': self.command, 'path': self.path, 'body': data}
                owner.requests.append(record)
                owner.events.append({'side': 'store', **copy.deepcopy(record)})
                status, content = 404, b'{"error":"not-found"}'
                path = urlsplit(self.path).path
                if self.command == 'POST' and path == '/api/subs' and isinstance(data, dict) and isinstance(data.get('name'), str):
                    owner.created[data['name']] = data
                    status, content = 201, b'{"status":"success"}'
                elif self.command == 'GET' and path.startswith('/download/'):
                    name = unquote(path.split('/')[2])
                    if name in owner.created:
                        status, content = owner.download_status, owner.body
                elif self.command == 'DELETE' and path.startswith('/api/sub/'):
                    owner.created.pop(unquote(path.removeprefix('/api/sub/')), None)
                    status, content = 204, b''
                self.send_response(status)
                self.send_header('Content-Type', 'text/yaml; charset=utf-8' if self.command == 'GET' else 'application/json')
                self.send_header('Content-Length', str(len(content)))
                self.end_headers()
                self.wfile.write(content)
            do_GET = do_POST = do_DELETE = handle_request
        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.url = 'http://127.0.0.1:' + str(self.server.server_port)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()


def normalized(records, url, content):
    if len(records) != 3:
        raise ProtocolError('expected create/download/delete exactly once')
    create, download, delete = records
    name = (create.get('body') or {}).get('name')
    if not isinstance(name, str) or not re.fullmatch(r'_cf_tmp_sub-a_[0-9a-f]{8}', name):
        raise ProtocolError('temporary name is not _cf_tmp_<sub>_<8hex>')
    expected_url = url if '#' in url else url + '#noFlow'
    expected = {'name': name, 'url': expected_url, 'noFlow': True}
    if content is not None:
        expected.update(source='local', content=content)
    intended = [{'method': 'POST', 'path': '/api/subs', 'body': expected},
                {'method': 'GET', 'path': '/download/' + quote(name, safe='') + '/ClashMeta?noFlow=true', 'body': None},
                {'method': 'DELETE', 'path': '/api/sub/' + quote(name, safe=''), 'body': None}]
    if records != intended:
        raise ProtocolError('method/path/body/noFlow/local-body/cleanup differs from protocol')
    result = copy.deepcopy(records)
    result[0]['body']['name'] = '<validated-temporary-name>'
    result[1]['path'] = '/download/<validated-temporary-name>/ClashMeta?noFlow=true'
    result[2]['path'] = '/api/sub/<validated-temporary-name>'
    return result


class Exchange:
    def __init__(self, source, store, url, content, order):
        self.source, self.store, self.url, self.content, self.order = source, store, url, content, order
        self.transcripts = [None, None]
        source.before_reply = lambda path: store.events.append({'side': 'source', 'method': 'GET', 'path': path})

    def before(self, index):
        self.source.requests.clear()
        self.store.requests.clear()
        self.store.events.clear()

    def after(self, index, reply):
        records = copy.deepcopy(self.store.requests)
        sequence = [('source' if r['side'] == 'source' else r['method']) for r in self.store.events]
        errors = []
        try:
            records = normalized(records, self.url, self.content)
        except ProtocolError as error:
            errors.append(str(error))
        if sequence != self.order:
            errors.append('wrong source/create/download/delete order: ' + repr(sequence))
        if self.store.created:
            errors.append('temporary subscription leaked after request')
        self.transcripts[index] = {'requests': records, 'source': copy.deepcopy(self.source.requests),
                                   'sequence': sequence, 'errors': errors}
        if index == 0 and errors:
            raise DesignError('Python Sub-Store fixture did not follow intended protocol: ' + '; '.join(errors))

    def compare(self, policy):
        return differences(*self.transcripts, 'substore', policy)
