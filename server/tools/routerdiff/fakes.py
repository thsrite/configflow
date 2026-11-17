"""Independent stateful HTTP peers, with explicit gates and injected faults.

These are protocol fixtures, never substitutes for Python application functions.
Unsupported device operations fail visibly instead of manufacturing success.
"""
import base64
import copy
import hashlib
import ipaddress
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import re
import threading
import time
from urllib.parse import parse_qs, unquote, urlsplit

from seed import DEVICE_PASSWORD
from console import execute, UnsupportedScript


class ProtocolError(Exception):
    """An unsupported or invalid fixture request (must not count as coverage)."""


class World:
    def __init__(self):
        self.lock = threading.RLock()
        self.condition = threading.Condition(self.lock)
        self.transcript = []
        self.faults = []
        self.gates = {}
        self.entered_gates = {}
        self.errors = []
        self.ros = {'/system/resource': {'version': '7.24.5 (stable)', 'board-name': 'synthetic-router'},
                    '/ip/route': [], '/ip/firewall/nat': [], '/ip/firewall/mangle': [],
                    '/ip/firewall/filter': [], '/ip/firewall/address-list': [],
                    '/routing/table': [{'.id': '*1', 'name': 'main', 'fib': 'true'}]}
        self.ikuai = {'sysstat': {'verinfo': {'version': '4.0.314', 'verstring': 'synthetic-router'}}}
        self.agent = {}
        self.documents = {}
        self.webdav = {'files': {}, 'collections': []}
        self.hidden_refs = {}
        self.sequence = {'ros': 16, 'ikuai': 1}
        self.cookie = None
        self.cookie_generation = 0
        self.nat_pending = set()
        self.servers = []
        world = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = 'HTTP/1.1'

            def log_message(self, *args):
                pass

            def do_GET(self):
                self.serve()

            do_POST = do_GET
            do_PUT = do_GET
            do_PATCH = do_GET
            do_DELETE = do_GET
            do_HEAD = do_GET
            do_MKCOL = do_GET
            do_PROPFIND = do_GET

            def serve(self):
                raw = self.rfile.read(int(self.headers.get('Content-Length', '0')))
                try:
                    parsed = json.loads(raw) if raw else None
                except (ValueError, UnicodeError):
                    parsed = None
                record = {'peer': self.server.peer, 'method': self.command, 'path': self.path,
                          'body': parsed, 'raw': base64.b64encode(raw).decode(),
                          'headers': {key.lower(): value for key, value in self.headers.items()}}
                try:
                    status, headers, content = world.handle(record)
                except Exception as error:
                    with world.lock:
                        world.errors.append(type(error).__name__ + ': ' + str(error))
                    status, headers, content = 599, {}, {'fixture_error': str(error)}
                if content is None:  # Deliberate lost acknowledgement.
                    self.close_connection = True
                    return
                if not isinstance(content, bytes):
                    content = json.dumps(content, ensure_ascii=False, separators=(',', ':')).encode()
                    headers.setdefault('Content-Type', 'application/json')
                self.send_response(status)
                for key, value in headers.items():
                    self.send_header(key, value)
                self.send_header('Content-Length', str(len(content)))
                self.end_headers()
                try:
                    if self.command != 'HEAD':
                        self.wfile.write(content)
                except (BrokenPipeError, ConnectionResetError):
                    pass

        for peer, port in (('ros', 18081), ('ikuai', 18082), ('agent', 18083),
                           ('rules', 18084), ('proxy', 18085), ('webdav', 18086)):
            server = ThreadingHTTPServer(('0.0.0.0', port), Handler)
            server.daemon_threads = True
            server.peer = peer
            thread = threading.Thread(target=server.serve_forever, daemon=True)
            thread.start()
            self.servers.append(server)

    def handle(self, request):
        with self.condition:
            self.transcript.append(copy.deepcopy(request))
            self.condition.notify_all()
            fault = None
            for candidate in self.faults:
                if self.matches(request, candidate['match']):
                    candidate['remaining'] -= 1
                    if candidate['remaining'] <= 0:
                        self.faults.remove(candidate)
                        fault = candidate
                    break
            if fault and fault.get('gate'):
                self.entered_gates[fault['gate']].set()
                self.condition.notify_all()
        # Never hold the state mutex while waiting for a lock-test gate.
        if fault and fault.get('gate'):
            if not self.gates[fault['gate']].wait(timeout=90):
                raise ProtocolError('test controller failed to release current request gate')
        with self.lock:
            if fault and not fault.get('apply', False):
                return self.fault_response(fault)
            response = getattr(self, request['peer'] + '_request')(request)
            return self.fault_response(fault) if fault else response

    @staticmethod
    def matches(request, match):
        for key, expected in match.items():
            value = request
            for part in key.strip('/').split('/') if key.startswith('/') else (key,):
                if not isinstance(value, dict) or part not in value:
                    return False
                value = value[part]
            if value != expected:
                return False
        return True

    @staticmethod
    def fault_response(fault):
        content = fault.get('body', {'message': 'synthetic device refusal'})
        headers = dict(fault.get('headers', {}))
        if 'raw' in fault:
            content = base64.b64decode(fault['raw'])
        elif content is None:
            # JSON null is a real malformed device value, not a lost reply.
            content = b'null'
            headers.setdefault('Content-Type', 'application/json')
        if fault.get('disconnect'):
            content = None
        return fault.get('status', 500), headers, content

    def ros_request(self, request):
        expected = 'Basic ' + base64.b64encode(('test-admin:' + DEVICE_PASSWORD).encode()).decode()
        if request['headers'].get('authorization') != expected:
            return 401, {}, {'message': 'Unauthorized'}
        split = urlsplit(request['path'])
        path = unquote(split.path.removeprefix('/rest'))
        if not split.path.startswith('/rest/'):
            raise ProtocolError('ROS request outside /rest')
        method, body = request['method'], request['body']
        if method == 'POST' and path == '/execute':
            if not isinstance(body, dict) or set(body) != {'script', 'as-string'} or body['as-string'] != '':
                raise ProtocolError('execute requires script and as-string empty capability')
            try:
                return 200, {}, execute(body['script'], self.ros.get('/ip/firewall/address-list', []), self.ros_add)
            except UnsupportedScript as error:
                raise ProtocolError(str(error)) from error
        if method == 'POST' and path.endswith('/remove'):
            table = path[:-7]
            if not isinstance(body, dict) or set(body) != {'.id'} or not isinstance(body['.id'], str):
                raise ProtocolError('remove needs comma-separated .id')
            wanted = set(body['.id'].split(','))
            # Device order, deliberately independent of submitted ID order.
            for row in list(self.ros.get(table, [])):
                if row.get('.id') in wanted:
                    if row.get('dynamic') in ('true', 'yes', True) or row.get('read-only') in ('true', 'yes', True):
                        return 400, {}, {'message': 'cannot remove dynamic/read-only item'}
                    self.ros[table].remove(row)
            return 200, {}, {}
        if method == 'POST' and path == '/ping':
            if not isinstance(body, dict) or body.get('count') != '2' or body.get('interval') != '300ms':
                raise ProtocolError('unexpected ping form')
            if body.get('arp-ping') and not body.get('interface'):
                return 400, {}, {'message': 'missing interface'}
            return 200, {}, [{'received': '1', 'sent': '1'}, {'received': '2', 'sent': '2'}]
        if method == 'POST' and path.endswith('/print'):
            table = path[:-6]
            rows = copy.deepcopy(self.ros.get(table, []))
            if not isinstance(body, dict):
                raise ProtocolError('ROS print needs object')
            rows = self.ros_query(rows, body.get('.query', []))
            props = body.get('.proplist')
            if props:
                names = props.split(',') if isinstance(props, str) else props
                rows = [{key: value for key, value in row.items() if key in names} for row in rows]
            return 200, {}, rows
        if method == 'POST' and path.endswith('/make-static'):
            table = path[:-12]
            row = self.ros_row(table, body.get('.id'))
            row['dynamic'] = 'false'
            return 200, {}, {}
        table, _, ident = path.rpartition('/')
        if ident.startswith('*'):
            row = next((row for row in self.ros.get(table, []) if row.get('.id') == ident), None)
            if row is None:
                return 404, {}, {'message': 'no such item'}
            if method == 'GET':
                return 200, {}, copy.deepcopy(row)
            if row.get('dynamic') in ('true', 'yes', True) or row.get('read-only') in ('true', 'yes', True):
                return 400, {}, {'message': 'cannot change dynamic item'}
            if method == 'PATCH':
                row.update(copy.deepcopy(body))
                return 200, {}, copy.deepcopy(row)
            if method == 'DELETE':
                self.ros[table].remove(row)
                return 204, {}, b''
        if method == 'GET':
            result = copy.deepcopy(self.ros.get(path, []))
            if isinstance(result, list):
                for key, values in parse_qs(split.query).items():
                    if key != '.proplist':
                        result = [row for row in result if str(row.get(key, '')) == values[0]]
            if path == '/ip/firewall/nat':
                # First read can observe a new NAT rule before dependency
                # resolution; the following read sees it settled.
                for row in self.ros.get(path, []):
                    if row.get('.id') in self.nat_pending:
                        row['invalid'] = 'false'
                        self.nat_pending.remove(row['.id'])
            return 200, {}, result
        if method == 'PUT':
            if not isinstance(body, dict):
                raise ProtocolError('ROS PUT needs object')
            ident = self.ros_add(path, body)
            return 201, {}, copy.deepcopy(self.ros_row(path, ident))
        raise ProtocolError('unsupported ROS operation: ' + method + ' ' + path)

    def ros_row(self, table, ident):
        for row in self.ros.get(table, []):
            if row.get('.id') == ident:
                return row
        raise ProtocolError('ROS operation targets missing row')

    def ros_add(self, path, values):
        self.sequence['ros'] += 1
        row = {'.id': '*' + format(self.sequence['ros'], 'X'), **copy.deepcopy(values)}
        if path == '/ip/firewall/nat':
            row.update(invalid='true', dynamic='false')
            self.nat_pending.add(row['.id'])
        if path in ('/ip/route', '/ipv6/route'):
            row.setdefault('active', 'true')
        if path == '/ip/firewall/address-list' and row.get('address'):
            network = ipaddress.ip_network(row['address'], strict=False)
            row['address'] = str(network.network_address) if network.prefixlen == network.max_prefixlen else str(network)
        self.ros.setdefault(path, []).append(row)
        return row['.id']

    @staticmethod
    def ros_query(rows, expressions):
        # RouterOS API query words: equality tests and stack boolean operators.
        if isinstance(expressions, str):
            expressions = [expressions]
        def matches(row):
            stack = []
            for expression in expressions:
                expression = expression.removeprefix('?')
                if expression.startswith('#'):
                    for operation in expression[1:]:
                        if operation not in '|&' or len(stack) < 2:
                            raise ProtocolError('unsupported ROS boolean query/stack underflow')
                        right, left = stack.pop(), stack.pop()
                        stack.append(left or right if operation == '|' else left and right)
                elif '=' in expression:
                    key, value = expression.split('=', 1)
                    stack.append(str(row.get(key, '')) == value)
                else:
                    raise ProtocolError('unsupported ROS query word: ' + expression)
            return all(stack)
        return [row for row in rows if matches(row)]

    def ikuai_request(self, request):
        body = request['body']
        if len(base64.b64decode(request['raw'])) > 96 * 1024:
            return 200, {}, b''
        if request['method'] != 'POST' or not isinstance(body, dict):
            raise ProtocolError('iKuai requires POST JSON object')
        if request['path'] == '/Action/login':
            valid = body == {'username': 'test-admin', 'passwd': hashlib.md5(DEVICE_PASSWORD.encode()).hexdigest(),
                'pass': base64.b64encode(('salt_11' + DEVICE_PASSWORD).encode()).decode()}
            headers = {}
            if valid:
                self.cookie_generation += 1
                self.cookie = 'synthetic-session-' + str(self.cookie_generation)
                headers['Set-Cookie'] = 'sess_key=' + self.cookie + '; Path=/'
            return 200, headers, {
                'Result': 10000, 'code': 0 if valid else 1005, 'message': '' if valid else '账号或密码错误'}
        if request['path'] != '/Action/call':
            raise ProtocolError('unknown iKuai endpoint')
        if not self.cookie or 'sess_key=' + self.cookie not in request['headers'].get('cookie', ''):
            return self.ik_reply(code=1008)
        func, action, param = body.get('func_name'), body.get('action'), body.get('param')
        if not isinstance(param, dict):
            raise ProtocolError('invalid iKuai param')
        if action == 'show':
            rows = self.ikuai.get(func, [])
            if isinstance(rows, dict):
                return self.ik_reply(rows)
            offset, limit = map(int, param.get('limit', '0,1000').split(','))
            prefix = 'static_' if func == 'dhcp_static' and param.get('TYPE') == 'static_total,static_data' else ''
            if func == 'dhcp_static' and not prefix:
                # The real plain TYPE selects a different view, without DNS or
                # ownership fields. A caller forgetting static_ must fail.
                rows = self.ikuai.get('dhcp_lease', [])
            page = copy.deepcopy(rows[offset:offset + limit])
            for row in page:
                if func == 'static_rt' and isinstance(row.get('netmask'), str):
                    row['netmask'] = row['netmask'].split(' ')[0] + ' (synthetic display)'
                    row.setdefault('status', 1)
                if func == 'route_object':
                    row['tagname'] = row.get('group_name', '')
                    row['ref_count'] = self.group_refs(row)
                for key in ('src_addr', 'dst_addr', 'src_port', 'dst_port', 'nat_addr', 'nat_port'):
                    if row.get(key) == {'custom': [], 'object': []}:
                        row[key] = {}
            return self.ik_reply({prefix + 'total': len(rows), prefix + 'data': page})
        rows = self.ikuai.setdefault(func, [])
        if action == 'add':
            if param.get('tagname') and any(row.get('tagname') == param['tagname'] for row in rows):
                return self.ik_reply(code=3001)
            self.sequence['ikuai'] += 1
            row = {'id': self.sequence['ikuai'], **copy.deepcopy(param)}
            if func == 'route_object':
                row.pop('tagname', None)
                row['group_id'] = 'IPGP' + str(row['id'])
            rows.append(row)
            return self.ik_reply({})
        row = next((row for row in rows if row.get('id') == param.get('id')), None)
        if row is None:
            return self.ik_reply(code=3001)
        if action == 'edit':
            row.update(copy.deepcopy(param))
        elif action == 'del':
            if func == 'route_object' and self.group_refs(row):
                return self.ik_reply(code=3001)
            rows.remove(row)
        else:
            raise ProtocolError('unsupported iKuai action: ' + str(action))
        return self.ik_reply({})

    def group_refs(self, group):
        count = self.hidden_refs.get(group.get('group_id'), 0)
        for table in ('nat_rule', 'stream_ipport'):
            for row in self.ikuai.get(table, []):
                referenced = False
                for field in ('src_addr', 'dst_addr'):
                    value = row.get(field, {})
                    objects = value.get('object') if isinstance(value, dict) else None
                    if isinstance(objects, list) and any(isinstance(ref, dict) and ref.get('gid') == group.get('group_id')
                                                         for ref in objects):
                        referenced = True
                count += int(referenced)
        return count

    @staticmethod
    def ik_reply(results=None, code=0):
        return 200, {}, {'Result': 10000, 'code': code, 'results': results if results is not None else {}}

    def agent_request(self, request):
        path = urlsplit(request['path']).path
        host = request['headers'].get('host', '')
        agent = self.agent.get(host)
        if agent is None:
            return 404, {}, {'success': False, 'message': 'synthetic Agent endpoint unavailable'}
        if request['headers'].get('authorization') != 'Bearer ' + agent['token']:
            return 401, {}, {'success': False, 'message': 'synthetic Agent credential refused'}
        endpoints = agent['endpoints']
        if path == '/api/linux-gateway/forwarding' and request['method'] == 'POST':
            if request['body'] != {}:
                raise ProtocolError('forwarding persist requires an empty object')
            status = endpoints['/api/linux-gateway/status']
            value = status['ipv4_forwarding']
            if value.get('config') in ('foreign', 'symlink') or value.get('hook_script') == 'modified':
                return 409, {}, {'success': False, 'message': 'synthetic external forwarding file must be preserved'}
            value.update(current='1', persisted=True, config='owned', hook_script='owned')
            return 200, {}, {'success': True, 'ipv4_forwarding': copy.deepcopy(value),
                             'managed': copy.deepcopy(status.get('managed', {}))}
        if path not in endpoints:
            return 404, {}, {'success': False, 'message': 'synthetic Agent endpoint unavailable'}
        entry = endpoints[path]
        if path == '/api/return-routing':
            if request['method'] == 'PUT':
                spec = request['body']
                required = {'router_ip', 'table', 'priority', 'mark', 'safety_version'}
                if not isinstance(spec, dict) or not required <= spec.keys() or spec.keys() - required - {'proxy_dst'}:
                    raise ProtocolError('invalid Agent return-routing capability payload')
                ipaddress.IPv4Address(spec['router_ip'])
                if any(type(spec[key]) is not int for key in ('table', 'priority', 'safety_version')):
                    raise ProtocolError('return-routing allocation requires exact integer fields')
                if spec['safety_version'] not in (1, 2):
                    return 409, {}, {'success': False, 'message': 'synthetic unsupported return-routing protocol'}
                entry['managed'] = {**entry.get('managed', {}), 'spec': copy.deepcopy(spec), 'active': True, 'table_present': True}
            elif request['method'] == 'DELETE':
                entry['managed'] = {**entry.get('managed', {}), 'spec': None, 'active': False, 'table_present': False}
            elif request['method'] != 'GET':
                raise ProtocolError('unsupported Agent return-routing method')
        elif request['method'] != 'GET':
            raise ProtocolError('unsupported Agent endpoint mutation')
        return 200, {}, copy.deepcopy(entry)

    def rules_request(self, request):
        entry = self.documents.get(request['path'])
        if entry is None:
            return 404, {}, b'synthetic rule source absent'
        return entry.get('status', 200), entry.get('headers', {}), base64.b64decode(entry['body'])

    proxy_request = rules_request

    def webdav_request(self, request):
        path, method = request['path'], request['method']
        if method == 'MKCOL':
            if path in self.webdav['collections']:
                return 405, {}, b''
            self.webdav['collections'].append(path)
            return 201, {}, b''
        if method == 'PUT':
            self.webdav['files'][path] = request['raw']
            return 201, {}, b''
        if method in ('GET', 'HEAD'):
            if path not in self.webdav['files']:
                return 404, {}, b''
            return 200, {}, base64.b64decode(self.webdav['files'][path])
        if method == 'DELETE':
            self.webdav['files'].pop(path, None)
            return 204, {}, b''
        raise ProtocolError('WebDAV method not yet implemented: ' + method)

    def control(self, action, **values):
        with self.condition:
            if action == 'seed':
                for key in ('ros', 'ikuai', 'agent', 'documents', 'hidden_refs'):
                    if key in values:
                        getattr(self, key).update(copy.deepcopy(values[key]))
                for rows in self.ros.values():
                    for row in rows if isinstance(rows, list) else ():
                        ident = row.get('.id') if isinstance(row, dict) else None
                        if isinstance(ident, str) and re.fullmatch(r'\*[0-9a-fA-F]+', ident):
                            self.sequence['ros'] = max(self.sequence['ros'], int(ident[1:], 16))
                for rows in self.ikuai.values():
                    for row in rows if isinstance(rows, list) else ():
                        ident = row.get('id') if isinstance(row, dict) else None
                        if type(ident) is int:
                            self.sequence['ikuai'] = max(self.sequence['ikuai'], ident)
                return True
            if action == 'fault':
                values.setdefault('remaining', 1)
                if values.get('gate'):
                    self.gates[values['gate']] = threading.Event()
                    self.entered_gates[values['gate']] = threading.Event()
                self.faults.append(copy.deepcopy(values))
                return True
            if action == 'release':
                self.gates[values['gate']].set()
                return True
            if action == 'wait':
                if 'gate' in values:
                    if not self.condition.wait_for(self.entered_gates[values['gate']].is_set,
                                                   timeout=values.get('timeout', 10)):
                        raise TimeoutError('current fake fault gate not reached')
                    return True
                match, count = values['match'], values.get('count', 1)
                def reached():
                    return sum(self.matches(item, match) for item in self.transcript) >= count
                if not self.condition.wait_for(reached, timeout=values.get('timeout', 10)):
                    raise TimeoutError('fake transcript gate not reached')
                return True
            if action == 'clear_transcript':
                self.transcript.clear()
                return True
        raise ValueError('unknown fake control')

    def snapshot(self):
        with self.lock:
            return copy.deepcopy({'ros': self.ros, 'ikuai': self.ikuai, 'agent': self.agent,
                                  'webdav': self.webdav,
                                  'sessions': {'ikuai_cookie': self.cookie, 'generation': self.cookie_generation},
                                  'transcript': self.transcript, 'errors': self.errors})

    def release_all(self):
        for gate in self.gates.values():
            gate.set()

    def close(self):
        self.release_all()
        for server in self.servers:
            server.shutdown()
            server.server_close()
