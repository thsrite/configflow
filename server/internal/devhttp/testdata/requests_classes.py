"""Which requests exception class a device failure raises (requests as the backend pinned it).

Run on the test server: ~/cf-test/.venv/bin/python requests_classes.py
Prints one JSON object {case: class name}; devhttp_test.go holds the same table.
Synthetic loopback servers only.
"""
import json
import socket
import ssl
import subprocess
import tempfile
import threading
import time

import requests


def serve(handler):
    listener = socket.socket()
    listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    listener.bind(('127.0.0.1', 0))
    listener.listen(16)

    def loop():
        while True:
            try:
                conn, _ = listener.accept()
            except OSError:
                return
            threading.Thread(target=handler, args=(conn,), daemon=True).start()
    threading.Thread(target=loop, daemon=True).start()
    return listener, listener.getsockname()[1]


def read_request(conn):
    data = b''
    while b'\r\n\r\n' not in data:
        chunk = conn.recv(4096)
        if not chunk:
            return
        data += chunk


def hang(conn):
    time.sleep(3)
    conn.close()


def close_after_request(conn):
    read_request(conn)
    conn.close()


def short_body(conn):
    read_request(conn)
    conn.sendall(b'HTTP/1.1 200 OK\r\nContent-Length: 100\r\nContent-Type: application/json\r\n\r\n[1,2')
    conn.close()


def stall_body(conn):
    read_request(conn)
    conn.sendall(b'HTTP/1.1 200 OK\r\nContent-Length: 100\r\nContent-Type: application/json\r\n\r\n[1,2')
    time.sleep(3)
    conn.close()


def garbage(conn):
    read_request(conn)
    conn.sendall(b'NOT HTTP AT ALL\r\n\r\n')
    conn.close()


def plain_http(conn):
    try:
        conn.recv(4096)
        conn.sendall(b'HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n')
    finally:
        conn.close()


def tls_server(handler_body):
    directory = tempfile.mkdtemp()
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-keyout', f'{directory}/k.pem',
                    '-out', f'{directory}/c.pem', '-days', '1', '-subj', '/CN=router.invalid'],
                   check=True, capture_output=True)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(f'{directory}/c.pem', f'{directory}/k.pem')

    def handler(conn):
        try:
            tls = context.wrap_socket(conn, server_side=True)
        except (ssl.SSLError, OSError):
            conn.close()
            return
        handler_body(tls)
    return serve(handler)


def ok(conn):
    read_request(conn)
    conn.sendall(b'HTTP/1.1 200 OK\r\nContent-Length: 2\r\nContent-Type: application/json\r\n\r\n[]')
    conn.close()


def attempt(url, **kwargs):
    session = requests.Session()
    session.trust_env = False
    try:
        session.get(url, timeout=kwargs.pop('timeout', 0.5), allow_redirects=False, **kwargs).json()
    except requests.RequestException as error:
        return error.__class__.__name__
    except ValueError as error:
        return 'ValueError:' + error.__class__.__name__
    return 'ok'


def main():
    refused = socket.socket()
    refused.bind(('127.0.0.1', 0))
    port = refused.getsockname()[1]
    refused.close()
    result = {'refused': attempt(f'http://127.0.0.1:{port}/rest')}
    for name, handler in (('read_timeout', hang), ('closed_after_request', close_after_request),
                          ('short_body', short_body), ('stalled_body', stall_body), ('garbage', garbage)):
        _, port = serve(handler)
        result[name] = attempt(f'http://127.0.0.1:{port}/rest')
    _, port = serve(hang)
    result['tls_handshake_timeout'] = attempt(f'https://127.0.0.1:{port}/rest', verify=False)
    _, port = serve(plain_http)
    result['tls_to_plain_http'] = attempt(f'https://127.0.0.1:{port}/rest', verify=False)
    _, port = tls_server(ok)
    result['self_signed_verify'] = attempt(f'https://127.0.0.1:{port}/rest', verify=True)
    result['self_signed_no_verify'] = attempt(f'https://127.0.0.1:{port}/rest', verify=False)
    result['bad_host'] = attempt('http://router.invalid/rest')
    result['connect_timeout'] = attempt('http://10.255.255.1/rest', timeout=0.3)
    print(json.dumps(result, indent=1, sort_keys=True))


main()
