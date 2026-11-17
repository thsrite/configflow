"""Loopback protocol fakes; all client requests and committed objects are visible."""
import base64
import copy
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import socket
import threading
from urllib.parse import quote, unquote, urlsplit
from xml.sax.saxutils import escape


class WebDAV:
    username, password = 'synthetic-dav-user', 'synthetic-dav-password'

    def __init__(self):
        self.before_reply = None
        owner = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def handle_request(self):
                size = int(self.headers.get('Content-Length', 0))
                body = self.rfile.read(size) if size else b''
                path = unquote(urlsplit(self.path).path)
                with owner.lock:
                    owner.requests.append({'method': self.command, 'path': self.path,
                                           'authorization': self.headers.get('Authorization', ''),
                                           'depth': self.headers.get('Depth', ''),
                                           'body': json.loads(body) if self.command == 'PUT' else body.decode(errors='replace')})
                    if owner.before_reply is None:
                        status, content = owner.respond(self.command, path, body)
                if owner.before_reply is not None:
                    owner.before_reply(self.command, path)
                    with owner.lock:
                        status, content = owner.respond(self.command, path, body)
                if status == 'disconnect':
                    self.connection.shutdown(socket.SHUT_RDWR)
                    self.connection.close()
                    return
                self.send_response(status)
                if self.command == 'PROPFIND':
                    self.send_header('Content-Type', 'application/xml; charset=utf-8')
                self.send_header('Content-Length', str(len(content)))
                self.end_headers()
                if self.command != 'HEAD':
                    self.wfile.write(content)

            do_HEAD = do_GET = do_MKCOL = do_PROPFIND = do_PUT = do_DELETE = handle_request
        self.lock = threading.RLock()
        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.url = 'http://127.0.0.1:' + str(self.server.server_port)
        self.reset()

    def reset(self, *, directories=('/', '/backup/'), objects=None, failures=None):
        with self.lock:
            self.directories = set(directories)
            self.objects = dict(objects or {})
            self.failures = dict(failures or {})
            self.requests = []

    def snapshot(self):
        with self.lock:
            return {'requests': copy.deepcopy(self.requests), 'directories': sorted(self.directories),
                    'objects': {p: json.loads(v) for p, v in self.objects.items()}}

    @property
    def basic(self):
        return 'Basic ' + base64.b64encode((self.username + ':' + self.password).encode()).decode()

    def respond(self, method, path, body):
        failure = self.failures.get((method, path), self.failures.get(method))
        if failure:
            return failure, b'synthetic failure; remote body must not leak'
        if method == 'HEAD':
            return (200 if path in self.objects or path.rstrip('/') + '/' in self.directories else 404), b''
        if method == 'MKCOL':
            parent = path.rstrip('/').rsplit('/', 1)[0] + '/'
            if parent not in self.directories:
                return 409, b''
            self.directories.add(path.rstrip('/') + '/')
            return 201, b''
        if method == 'PUT':
            self.objects[path] = body
            return 201, b''
        if method == 'DELETE':
            self.objects.pop(path, None)
            return 204, b''
        if method == 'PROPFIND':
            if path.rstrip('/') + '/' not in self.directories:
                return 404, b''
            members = [path.rstrip('/') + '/'] + sorted(p for p in self.objects if p.startswith(path.rstrip('/') + '/'))
            entries = []
            for name in members:
                resource = '<d:collection/>' if name.endswith('/') else ''
                entries.append('<d:response><d:href>' + escape(quote(name, safe='/')) + '</d:href><d:propstat><d:prop>'
                    '<d:resourcetype>' + resource + '</d:resourcetype><d:getcontentlength>0</d:getcontentlength>'
                    '<d:getlastmodified>Mon, 01 Jan 2024 00:00:00 GMT</d:getlastmodified></d:prop>'
                    '<d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>')
            return 207, ('<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">' + ''.join(entries) + '</d:multistatus>').encode()
        return 405, b''

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
