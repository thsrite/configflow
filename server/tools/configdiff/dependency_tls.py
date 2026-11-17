"""Loopback TLS GitHub dependency API fake; no tunnel ever leaves the process."""
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import platform
import socket
import ssl
import subprocess
import tempfile
import threading

from harness import DesignError

RELEASE_PATH = '/repos/sub-store-org/Sub-Store/releases/latest'


def require_native_network():
    if (platform.system() != 'Linux' or 'microsoft' in platform.release().lower()
            or os.environ.get('CONFIGDIFF_ISOLATED_NETWORK') != '1'
            or {name for _, name in socket.if_nameindex()} != {'lo'}):
        raise DesignError('dependency TLS fixtures require .32 native_network.sh (isolated loopback network)')


class GitHub:
    def __init__(self):
        require_native_network()
        self.temporary = tempfile.TemporaryDirectory(prefix='configdiff-github-tls-')
        root = Path(self.temporary.name)
        self.cert, key = root / 'ca.pem', root / 'key.pem'
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
            '-config', '/dev/null', '-subj', '/CN=api.github.com',
            '-addext', 'subjectAltName=DNS:api.github.com,DNS:downloads.example',
            '-addext', 'basicConstraints=critical,CA:TRUE', '-keyout', str(key), '-out', str(self.cert)],
            check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15)
        self.context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        self.context.load_cert_chain(self.cert, key)
        self.requests, self.status = [], 200
        self.bundle = b'"use strict";\n' + b' ' * (512 * 1024)
        self.release = {'tag_name': 'v2.42.1', 'published_at': '2026-01-01T00:00:00Z',
            'html_url': 'https://release.example/synthetic', 'body': 'synthetic release notes', 'assets': [{
                'name': 'sub-store.bundle.js', 'browser_download_url': 'https://downloads.example/sub-store.bundle.js',
                'size': len(self.bundle), 'digest': 'sha256:' + hashlib.sha256(self.bundle).hexdigest()}]}
        owner = self
        class Inner(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                length = int(self.headers.get('Content-Length', '0'))
                owner.requests.append({'method': self.command, 'path': self.path,
                    'authorization': self.headers.get('Authorization', ''), 'body': self.rfile.read(length).decode(),
                    **({'accept': self.headers.get('Accept', ''), 'user_agent': self.headers.get('User-Agent', '')}
                       if self.path == RELEASE_PATH else {})})
                if self.path == RELEASE_PATH:
                    status, body = owner.status, json.dumps(owner.release).encode()
                elif self.path == '/sub-store.bundle.js':
                    status, body = 200, owner.bundle
                else:
                    status, body = 404, b'synthetic unexpected TLS path'
                self.send_response(status)
                self.send_header('Content-Type', 'application/json' if self.path == RELEASE_PATH else 'application/javascript')
                self.send_header('Content-Length', str(len(body)))
                self.send_header('Connection', 'close')
                self.end_headers()
                self.wfile.write(body)

            do_POST = do_PUT = do_DELETE = do_PATCH = do_GET

        class Proxy(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_CONNECT(self):
                owner.requests.append({'method': 'CONNECT', 'target': self.path})
                if self.path not in ('api.github.com:443', 'downloads.example:443'):
                    self.send_error(502)
                    return
                self.send_response(200)
                self.end_headers()
                self.wfile.flush()
                self.close_connection = True
                self.connection.settimeout(5)
                with owner.context.wrap_socket(self.connection, server_side=True) as connection:
                    Inner(connection, self.client_address, self.server)

            def do_GET(self):
                owner.requests.append({'method': self.command, 'unexpected_plaintext_path': self.path})
                self.send_error(502)

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Proxy)
        self.url = 'http://127.0.0.1:' + str(self.server.server_port)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
        self.temporary.cleanup()


def release_request():
    return [{'method': 'CONNECT', 'target': 'api.github.com:443'},
            {'method': 'GET', 'path': RELEASE_PATH, 'authorization': '', 'body': '',
             'accept': 'application/vnd.github+json', 'user_agent': 'ConfigFlow'}]
