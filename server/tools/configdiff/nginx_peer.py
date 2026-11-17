#!/usr/bin/python3
"""Restricted Nginx CLI peer for isolated Step B; never runs real Nginx."""
import json
import os
from pathlib import Path
import sys
from urllib.parse import urlsplit
from urllib.request import Request, ProxyHandler, build_opener


def main():
    args = sys.argv[1:]
    prefix = ['-c', '/etc/nginx/nginx.conf']
    action = 'check' if args == prefix + ['-t'] else 'reload' if args == prefix + ['-s', 'reload'] else ''
    endpoint = os.environ.get('CONFIGDIFF_NGINX_CONTROL', '')
    parsed = urlsplit(endpoint)
    if (os.environ.get('CONFIGDIFF_ISOLATED_LISTENER') != '1' or not action or parsed.scheme != 'http'
            or parsed.hostname != '127.0.0.1' or not parsed.port or parsed.path != '/_configdiff/nginx'):
        print('synthetic Nginx peer refuses unconfigured or unknown action', file=sys.stderr)
        return 97
    text = Path('/etc/nginx/nginx.conf').read_text()
    first, body = text.split('\n', 1)
    if not first.startswith('# configflow-web-listener '):
        return 96
    active = json.loads(first.removeprefix('# configflow-web-listener '))
    request = Request(endpoint, json.dumps({'action': action, 'active': active, 'body': body}).encode(),
        {'Authorization': 'Bearer synthetic-nginx-peer', 'Content-Type': 'application/json'})
    with build_opener(ProxyHandler({})).open(request, timeout=8) as response:
        reply = json.load(response)
    return 0 if reply == {'success': True} else 1


if __name__ == '__main__':
    sys.exit(main())
