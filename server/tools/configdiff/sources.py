"""Stateful loopback subscription/rule HTTP source; no external requests."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import socket
import threading


FEED = b'proxies:\n  - {name: synthetic-node, type: socks5, server: 192.0.2.1, port: 1080}\n'


class Source:
    def __init__(self, body=FEED, *, host='127.0.0.1', port=0):
        self.requests, self.body = [], body
        self.status = 200
        self.omit_content_length = False
        self.record_wire = False
        self.record_body = False
        self.headers = {'Content-Type': 'text/yaml; charset=utf-8'}
        self.responses = {}
        self.before_reply = lambda _path: None
        self.after_reply = lambda _path: None
        owner = self
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_GET(self):
                self.respond()

            def do_HEAD(self):
                self.respond(head=True)

            # Unexpected methods must remain observable, especially CONNECT
            # when this fixture blocks HTTPS origins as a forward proxy.
            do_POST = do_PUT = do_PATCH = do_DELETE = do_OPTIONS = do_GET

            def do_CONNECT(self):
                # This fake never opens a tunnel. Reject it explicitly while
                # recording it, rather than falsely acknowledging TLS support.
                self.respond(status_override=owner.status if owner.status >= 400 else 501)

            def respond(self, head=False, status_override=None):
                owner.requests.append({'method': self.command, 'path': self.path,
                                       'authorization': self.headers.get('Authorization', ''),
                                       'user_agent': self.headers.get('User-Agent', '')})
                if owner.record_body:
                    length = int(self.headers.get('Content-Length', '0'))
                    owner.requests[-1].update(body_hex=self.rfile.read(length).hex(),
                        transfer_encoding=self.headers.get('Transfer-Encoding', ''))
                if owner.record_wire or self.command == 'CONNECT':
                    owner.requests[-1].update(request_line=self.requestline, host=self.headers.get('Host', ''),
                                              proxy_authorization=self.headers.get('Proxy-Authorization', ''))
                owner.before_reply(self.path)
                status, body, headers = owner.responses.get(self.path, (owner.status, owner.body, owner.headers))
                if status_override is not None:
                    status = status_override
                self.send_response(status)
                for key, value in headers.items():
                    self.send_header(key, value)
                if not owner.omit_content_length and not any(key.lower() == 'content-length' for key in headers):
                    self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                if not head:
                    try:
                        self.wfile.write(body)
                    except (BrokenPipeError, ConnectionResetError):
                        pass  # Size-limit consumers may close immediately after headers.
                    else:
                        self.wfile.flush()
                        owner.after_reply(self.path)
                else:
                    owner.after_reply(self.path)
        server_type = ThreadingHTTPServer
        if host == '::1':
            class IPv6Server(ThreadingHTTPServer):
                address_family = socket.AF_INET6
            server_type = IPv6Server
        elif host != '127.0.0.1':
            raise ValueError('synthetic source must use IPv4 or IPv6 loopback')
        self.server = server_type((host, port), Handler)
        self.url = 'http://' + ('[::1]' if host == '::1' else host) + ':' + str(self.server.server_port)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *_args):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
