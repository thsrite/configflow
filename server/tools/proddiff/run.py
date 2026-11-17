"""Offline production-snapshot differential. Console and baseline contain schema only."""
import ast
import collections
import contextlib
import concurrent.futures
import datetime
import errno
import fcntl
import hashlib
import io
import itertools
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import threading
import traceback
import urllib.error
import urllib.parse
import urllib.request
import zipfile
import yaml
from generators import run_workers, self_check, successful_output_regressions

ROOT = Path.cwd()
PARITY = ROOT / 'server/parity'
PARAM = re.compile(r'<(?:[^:>]+:)?([^>]+)>')
BASE_URL = 'http://127.0.0.1:5001'
READONLY_POST = json.loads((PARITY / 'proddiff-readonly-post.json').read_text())
PRIVATE_REPORT = None
# A key is public only if it is a literal schema identifier in the reference source.
KEYS = {'<key>', '<file>', 'status', 'Content-Type', 'body', 'content', 'config'}
for source in (ROOT / 'backend').rglob('*.py'):
    try:
        tree = ast.parse(source.read_text())
    except (SyntaxError, UnicodeError):
        continue
    KEYS.update(node.value for node in ast.walk(tree) if isinstance(node, ast.Constant)
                and isinstance(node.value, str) and re.fullmatch(r'[A-Za-z_][A-Za-z_0-9-]*', node.value))


def safe_key(key):
    return key if isinstance(key, str) and key in KEYS else '<key>'


HEADER_KEYS = {key.lower(): key for key in KEYS}
# Transport/framing headers of two different HTTP servers; everything else is compared.
IGNORED_HEADERS = {'date', 'server', 'content-length', 'connection', 'keep-alive', 'transfer-encoding', 'content-type'}


def safe_header(name):
    return HEADER_KEYS.get(name, '<key>')


class DuplicateKey(ValueError):
    """A parser would silently keep only one of two equal keys."""


def _unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise DuplicateKey('duplicate JSON key')
        result[key] = value
    return result


def load_json(body):
    return json.loads(body, object_pairs_hook=_unique_pairs)


class StrictLoader(yaml.SafeLoader):
    """SafeLoader that rejects duplicate mapping keys (PyYAML keeps the last; yaml.v3 consumers reject)."""

    def construct_mapping(self, node, deep=False):
        if isinstance(node, yaml.MappingNode):
            seen = set()
            for key_node, _ in node.value:
                if key_node.tag == 'tag:yaml.org,2002:merge':
                    continue
                key = self.construct_object(key_node, deep=True)
                try:
                    duplicate = key in seen
                    seen.add(key)
                except TypeError:
                    continue
                if duplicate:
                    raise yaml.constructor.ConstructorError(None, None, 'duplicate YAML key', key_node.start_mark)
        return super().construct_mapping(node, deep)


def load_yaml(text_value):
    return yaml.load(text_value, Loader=StrictLoader)


def differences(a, b, path='body'):
    if type(a) is not type(b):
        # Equal JSON/YAML numbers of different kinds stay a separate class: typed consumers
        # (Go Agent structs, yaml.v3 int fields) reject 1.0 where they expect 1.
        if type(a) in (int, float) and type(b) in (int, float) and a == b:
            return [(path, 'int-float')]
        return [(path, 'type')]
    if isinstance(a, dict):
        out = []
        for key in sorted(a.keys() | b.keys(), key=str):
            child = path + '.' + safe_key(key)
            if key not in a:
                out.append((child, 'extra-in-go'))
            elif key not in b:
                out.append((child, 'missing-in-go'))
            else:
                out.extend(differences(a[key], b[key], child))
        return out
    if isinstance(a, list):
        out = [] if len(a) == len(b) else [(path, 'length')]
        for i, (left, right) in enumerate(zip(a, b)):
            out.extend(differences(left, right, f'{path}[{i}]'))
        return out
    return [] if a == b else [(path, 'value')]


def text(value):
    return value.replace('\r\n', '\n').replace('\r', '\n').rstrip('\n') + '\n'


def line_endings(body):
    """Line-ending style of a text body; CRLF in a shell script is a functional change."""
    crlf = body.count(b'\r\n')
    return (crlf > 0, body.count(b'\r') - crlf > 0)


def parsed(body, content_type, route, member=''):
    if 'json' in content_type or member.endswith('.json'):
        return load_json(body)
    if body.startswith(b'PK\x03\x04'):
        result = {}
        with zipfile.ZipFile(io.BytesIO(body)) as archive:
            names = [info.filename for info in archive.infolist()]
            if len(names) != len(set(names)):
                raise DuplicateKey('duplicate ZIP member')
            for info in archive.infolist():
                if info.filename.endswith('/'):
                    continue
                raw = archive.read(info)
                result[info.filename] = parsed(raw, '', route, info.filename)
                result[info.filename + '#eol'] = list(line_endings(raw))
                # Unix mode bits: an install script that loses +x is a real regression.
                result[info.filename + '#mode'] = info.external_attr >> 16
        return result
    try:
        value = body.decode('utf-8')
    except UnicodeError:
        if 'yaml' in content_type or member.endswith(('.yaml', '.yml')):
            raise
        return body
    if ('yaml' in content_type or member.endswith(('.yaml', '.yml')) or
            (not member and re.search(r'/(mihomo|mosdns|oxidns)(/preview)?$', route))):
        return load_yaml(value)
    return text(value).splitlines()


def _walk(root, parts):
    """Yield (container, key) pairs addressed by a dotted path; `name[]` means every element."""
    current = [root]
    for index, part in enumerate(parts):
        last = index == len(parts) - 1
        every = part.endswith('[]')
        name = part[:-2] if every else part
        following = []
        for value in current:
            if not isinstance(value, dict) or name not in value:
                continue
            if every:
                if isinstance(value[name], list):
                    following.extend((value[name], i) for i in range(len(value[name])))
            else:
                following.append((value, name))
        if last:
            return following
        current = [container[key] for container, key in following]
    return []


def _timestamp(value):
    if isinstance(value, (int, float)) and not isinstance(value, bool):
        return float(value)
    if isinstance(value, str):
        try:
            return datetime.datetime.fromisoformat(value).timestamp()
        except ValueError:
            return None
    return None


def normalize_pair(rule, left, right):
    """Return the normalised pair, or None when the rule's preconditions do not hold (diff stays)."""
    if type(left) is not type(right) or isinstance(left, (dict, list)):
        return None
    if 'tolerance' in rule:
        shape = lambda value: re.sub(r'\d', '9', value) if isinstance(value, str) else type(value).__name__
        a, b = _timestamp(left), _timestamp(right)
        if a is None or b is None or shape(left) != shape(right) or abs(a - b) > rule['tolerance']:
            return None
        return '<runtime>', '<runtime>'
    return '<runtime>', '<runtime>'


def apply_rules(rules, route, a, b):
    """a/b are {'body': parsed body, 'header': {...}}; paths starting with `header.` address headers."""
    for rule in rules:
        if rule['route'] != route or 'path' not in rule:
            continue
        if rule['path'].startswith('header.'):
            section, parts = 'header', [rule['path'][len('header.'):]]
        else:
            section, parts = 'body', rule['path'].split('.')
        if not isinstance(a[section], dict) or not isinstance(b[section], dict):
            continue
        left_targets, right_targets = _walk(a[section], parts), _walk(b[section], parts)
        if 'pattern' in rule:
            # Masking is a per-value transform: masked values only become equal when everything
            # outside the pattern is equal, so each side is masked on its own (lists may differ in length).
            pattern = re.compile(rule['pattern'])
            for container, key in left_targets + right_targets:
                if isinstance(container[key], str):
                    container[key] = pattern.sub('<runtime>', container[key])
            continue
        if len(left_targets) != len(right_targets):
            continue
        for (lc, lk), (rc, rk) in zip(left_targets, right_targets):
            if lk != rk:
                continue
            if rule.get('unordered'):
                # Multiset comparison for lists whose order is not defined (concurrent writers).
                if isinstance(lc[lk], list) and isinstance(rc[rk], list):
                    order = lambda item: json.dumps(item, sort_keys=True, ensure_ascii=False, default=str)
                    lc[lk], rc[rk] = sorted(lc[lk], key=order), sorted(rc[rk], key=order)
                continue
            pair = normalize_pair(rule, lc[lk], rc[rk])
            if pair is not None:
                lc[lk], rc[rk] = pair


def plain_install_error(left, right, route):
    """L1 D8: only the exact original text error loses its attachment header."""
    messages = {404: '安装链接不存在或已过期，请在面板重新生成。\n',
                503: '安装脚本暂时不可用，请稍后重试。\n'}
    return (route == '/api/agents/i/<token>' and left[0] in messages
            and left[0] == right[0]
            and left[1] == right[1] == 'text/plain; charset=utf-8'
            and left[2] == right[2] == messages[left[0]].encode('utf-8')
            and left[3].get('content-disposition') == 'attachment; filename="configflow-install.sh"'
            and 'content-disposition' not in right[3])


def installer_rollback_baseline(reply, route):
    """Reverse only the fixed approved D3 source edits for the public script."""
    if route != '/api/agents/install-script' or reply[0] != 200 or not reply[1].startswith('text/plain'):
        return reply
    approved = json.loads((ROOT / 'server/internal/installer/installertest/l1-rollback-deltas.json').read_text())
    try:
        script = reply[2].decode('utf-8')
    except UnicodeError:
        return reply
    if approved['module'] not in script:
        return reply
    edits = [(item['after'].replace('{agent_install_rollback}', approved['module']), item['before'])
             for item in approved['deltas']]
    edits += [(item['after'], item['before']) for item in approved.get('rendered', [])]
    for after, before in sorted(edits, key=lambda item: len(item[0]), reverse=True):
        script = script.replace(after, before)
    return (reply[0], reply[1], script.encode('utf-8'), *reply[3:])


def approved_c07_readiness(left, right, route):
    """Recognize only C07's approved, temporary missing-owner list response.

    Headers still compare normally. Once R14/A16/A17 wire their real rows this
    branch stops applying automatically; no generic 503/error waiver is used.
    """
    templates = {'/api/tasks': ('tasks', '任务模块尚未就绪：'),
                 '/api/notifications': ('items', '通知模块尚未就绪：')}
    if route not in templates or left[0] != 200 or right[0] != 503:
        return False
    if 'application/json' not in left[1] or 'application/json' not in right[1]:
        return False
    try:
        before, after = load_json(left[2]), load_json(right[2])
    except (ValueError, UnicodeError):
        return False
    field, message = templates[route]
    keys = {'success', field} | ({'checked_at'} if field == 'items' else set())
    return (isinstance(before, dict) and set(before) == keys and before.get('success') is True
            and isinstance(before.get(field), list)
            and (field != 'items' or isinstance(before.get('checked_at'), str))
            and after == {'success': False, 'message': message + 'router_rule_sync、agent',
                          'missing_slots': ['router_rule_sync', 'agent']})


class AgentComparisonIdentities:
    """Pair independent cache identities, checking format, stability and uniqueness."""
    def __init__(self):
        self.lock = threading.Lock()
        self.pairs = {'checked_at': {}, 'version': {}}

    def pair(self, key, a, b):
        if not isinstance(a, str) or not isinstance(b, str):
            return False
        if key == 'version':
            if not re.fullmatch('[a-f0-9]{16}', a) or not re.fullmatch('[a-f0-9]{16}', b):
                return False
        else:
            shape = r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{6})?Z'
            if not re.fullmatch(shape, a) or not re.fullmatch(shape, b):
                return False
            try:
                if abs(datetime.datetime.fromisoformat(a).timestamp() - datetime.datetime.fromisoformat(b).timestamp()) > 600:
                    return False
            except ValueError:
                return False
        with self.lock:
            pairs = self.pairs[key]
            if a in pairs and pairs[a] != b:
                return False
            if any(other != a and value == b for other, value in pairs.items()):
                return False
            pairs[a] = b
        return True

    def normalize_result(self, a, b):
        if isinstance(a, dict) and isinstance(b, dict):
            for key in ('checked_at', 'version'):
                if key in a and key in b and self.pair(key, a[key], b[key]):
                    a[key] = b[key] = '<paired-cache-identity>'

    def normalize(self, a, b, route):
        if route == '/api/agents/<agent_id>/push-diff':
            self.normalize_result(a, b)
        elif route == '/api/agents' and isinstance(a, list) and isinstance(b, list):
            for left, right in zip(a, b):
                if isinstance(left, dict) and isinstance(right, dict):
                    self.normalize_result(left.get('content_sync'), right.get('content_sync'))
        elif route == '/api/agents/<agent_id>' and isinstance(a, dict) and isinstance(b, dict):
            self.normalize_result(a.get('content_sync'), b.get('content_sync'))


AGENT_COMPARISON_ROUTES = {'/api/agents', '/api/agents/<agent_id>', '/api/agents/<agent_id>/push-diff'}


def same_agent_transport_target(python_message, go_message):
    """Recognize native offline-Agent exception text, retaining the target proof."""
    if not isinstance(python_message, str) or not isinstance(go_message, str):
        return False
    py = re.match(r"^HTTPConnectionPool\(host='([^']+)', port=(\d+)\):", python_message)
    go = re.match(r'^((?:Get|Post)) "(http://[^" ]+)": ', go_message)
    if not py or not go:
        return False
    target = urllib.parse.urlsplit(go.group(2))
    try:
        if target.hostname != py.group(1) or (target.port or 80) != int(py.group(2)):
            return False
    except ValueError:
        return False
    path = re.search(r' url: ([^ ]+)', python_message)
    return path is None or path.group(1) == target.path


def response_diffs(left, right, route, rules, agent_identities=None):
    if approved_c07_readiness(left, right, route):
        # Compare the approved availability contract instead of exposing a
        # partial task list. Keep both original content types and headers.
        left = (right[0], left[1], right[2], *left[3:])
    right = installer_rollback_baseline(right, route)
    if plain_install_error(left, right, route):
        # Body/status/type already agree exactly. Assert only the approved
        # attachment-header removal; every other header still compares.
        headers = dict(left[3])
        del headers['content-disposition']
        left = (*left[:3], headers)
    out = []
    if (left[0] == 0) != (right[0] == 0):
        return [('status', 'transport-one-sided')]
    if left[0] != right[0]:
        out.append(('status', 'value'))
    if left[1].lower().replace(' ', '') != right[1].lower().replace(' ', ''):
        out.append(('Content-Type', 'value'))
    if left[0] == 0 and right[0] == 0:
        # Both sides hung or refused: never an agreement.
        out.append(('status', 'transport-both'))
    if left[0] == 0 or right[0] == 0:
        return out
    values = []
    for label, reply in (('python', left), ('go', right)):
        try:
            values.append(parsed(reply[2], reply[1], route))
        except (ValueError, UnicodeError, yaml.YAMLError, zipfile.BadZipFile):
            out.append(('body', 'parse-' + label))
    if ('json' not in left[1] and 'json' not in right[1] and not left[2].startswith(b'PK\x03\x04')
            and line_endings(left[2]) != line_endings(right[2])):
        out.append(('body', 'line-endings'))
    a = {'body': values[0] if len(values) == 2 else None,
         'header': {k: v for k, v in (left[3] if len(left) > 3 else {}).items() if k not in IGNORED_HEADERS}}
    b = {'body': values[1] if len(values) == 2 else None,
         'header': {k: v for k, v in (right[3] if len(right) > 3 else {}).items() if k not in IGNORED_HEADERS}}
    if len(values) == 2 and isinstance(a['body'], dict) and isinstance(b['body'], dict):
        # Generated YAML embedded in JSON is compared structurally too.
        for key in ('content', 'config', 'compose'):
            if isinstance(a['body'].get(key), str) and isinstance(b['body'].get(key), str):
                left_text, right_text = a['body'][key], b['body'][key]
                # Agent pull has three YAML services and one INI service (Surge).
                # Only this route's content gets the explicit mapping contract:
                # OxiDNS often has no root `key: value` line for the old heuristic.
                if route == '/api/agents/<agent_id>/config' and key == 'content':
                    try:
                        left_yaml, right_yaml = load_yaml(left_text), load_yaml(right_text)
                        if isinstance(left_yaml, dict) and isinstance(right_yaml, dict):
                            a['body'][key], b['body'][key] = left_yaml, right_yaml
                            if left_yaml == right_yaml:
                                # Digests identify each side's exact generated bytes.
                                # Verify both before treating native YAML formatting as equal.
                                for field, length in (('md5', 32), ('version', 8)):
                                    if field in a['body'] and field in b['body']:
                                        expected = [hashlib.md5(t.encode()).hexdigest()[:length]
                                                    for t in (left_text, right_text)]
                                        if a['body'][field] == expected[0] and b['body'][field] == expected[1]:
                                            a['body'][field] = b['body'][field] = '<verified-content-digest>'
                                        else:
                                            out.append(('body.' + field, 'invalid-content-digest'))
                            continue
                    except yaml.YAMLError:
                        pass

                service_yaml = key == 'compose' or any('/' + service in route for service in ('mihomo', 'mosdns', 'oxidns')) or bool(re.search(r'(?m)^[A-Za-z_][A-Za-z_0-9-]*: ', left_text))
                if line_endings(left_text.encode()) != line_endings(right_text.encode()):
                    out.append(('body.' + key, 'line-endings'))
                try:
                    a['body'][key], b['body'][key] = ((load_yaml(left_text), load_yaml(right_text)) if service_yaml
                                                      else (text(left_text).splitlines(), text(right_text).splitlines()))
                except yaml.YAMLError:
                    pass
    # A14 generate returns str(transport error) on an offline Agent. PORTING
    # explicitly does not chase requests vs net/http internal exception text.
    # Only this route, a failed 400 envelope and the same proven target qualify;
    # an A18 refusal or any Chinese/user error still differs normally.
    if (route == '/api/agents/<agent_id>/generate-config' and left[0] == right[0] == 400
            and isinstance(a['body'], dict) and isinstance(b['body'], dict)
            and a['body'].get('success') is False and b['body'].get('success') is False
            and same_agent_transport_target(a['body'].get('message'), b['body'].get('message'))):
        a['body']['message'] = b['body']['message'] = '<same-target-native-transport-error>'
    if agent_identities is not None and route in AGENT_COMPARISON_ROUTES:
        agent_identities.normalize(a['body'], b['body'], route)
    apply_rules(rules, route, a, b)
    if len(left) > 3 and len(right) > 3:
        for name in sorted(a['header'].keys() | b['header'].keys()):
            path = 'header.' + safe_header(name)
            if name not in a['header']:
                out.append((path, 'extra-in-go'))
            elif name not in b['header']:
                out.append((path, 'missing-in-go'))
            elif a['header'][name] != b['header'][name]:
                out.append((path, 'value'))
    if len(values) == 2:
        out.extend(differences(a['body'], b['body']))
    return out


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args):
        return None


def send(port, method, path, token='', body=None, timeout=20):
    headers = {'Host': '127.0.0.1:5001', 'X-Forwarded-Host': '127.0.0.1:5001',
               'X-Forwarded-Proto': 'http', 'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(f'http://127.0.0.1:{port}' + path,
                                     data=json.dumps(body).encode() if body is not None else None,
                                     headers=headers, method=method)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        response = opener.open(request, timeout=timeout)
    except urllib.error.HTTPError as error:
        response = error
    except (OSError, TimeoutError):
        return 0, '', b'', {}
    with response:
        headers = {name.lower(): ', '.join(response.headers.get_all(name)) for name in set(response.headers.keys())}
        return response.code, response.headers.get('Content-Type', ''), response.read(), headers


def scrub(reply, data_dir):
    """Each server runs on its own temporary data copy; its absolute path is not a difference."""
    marker = str(data_dir)
    return (reply[0], reply[1], reply[2].replace(marker.encode(), b'<DATA_DIR>'),
            {name: value.replace(marker, '<DATA_DIR>') for name, value in reply[3].items()})


def document(data):
    connection = sqlite3.connect(f'file:{data / "configflow.sqlite3"}?mode=ro', uri=True)
    result = {'system': {}, 'shared': {}, 'profiles': {}}
    with connection:
        for scope, owner, field, value in connection.execute('SELECT scope, owner, field, value FROM fields'):
            section = result['profiles'].setdefault(owner, {}) if scope == 'profile' else result.setdefault(scope, {})
            section[field] = json.loads(value)
    connection.close()
    return result


def ids(rows, field='id'):
    return [str(row[field]) for row in rows if isinstance(row, dict) and row.get(field) is not None]


def cases(rows, doc, data):
    profiles = doc['profiles']
    agents = doc['system'].get('agents', [])
    # Opaque job/artifact IDs are read only from local metadata, never printed.
    stored = collections.defaultdict(list)
    preview_files = {}
    for agent in agents:
        stored['deployments'].extend(str(key) for key in (agent.get('deployments') or {}))
    command_db = data / 'install-commands/snapshots.sqlite3'
    if command_db.exists():
        with sqlite3.connect(f'file:{command_db}?mode=ro', uri=True) as connection:
            stored['tokens'] = [row[0] for row in connection.execute('SELECT token FROM snapshots')]

    for name in ('.generated-previews', 'deployments', 'setup-sessions', 'gateway-installations', 'probes'):
        folder = data / name
        if folder.exists():
            for file in folder.rglob('*.json'):
                try:
                    value = json.loads(file.read_bytes())
                    if isinstance(value, dict) and value.get('id'):
                        stored[name].append(str(value['id']))
                        if name == '.generated-previews':
                            preview_files[str(value['id'])] = [entry['path'] for entry in value.get('bundle_meta', {}).get('manifest', {}).get('files', [])]
                            if not preview_files[str(value['id'])]:
                                preview_files[str(value['id'])] = ['surge.conf']
                except (OSError, ValueError):
                    pass
    for method, route, *_ in rows:
        generate = method == 'POST' and route in READONLY_POST
        if method != 'GET' and not generate:
            continue
        for profile_index, (profile_id, profile) in enumerate(profiles.items()):
            config = dict(doc['shared'], **profile)
            params = {
                'profile_id': [profile_id], 'agent_id': ids(agents),
                'router_id': ids(config.get('routers', {}).get('items', [])),
                'sub_id': ids(config.get('subscriptions', [])),
                'agg_id': ids(config.get('subscription_aggregations', [])),
                'resource_id': ids(config.get('subscription_aggregations' if '/aggregations/' in route else 'subscriptions', [])),
                'rule_id': ids(config.get('rule_library', [])),
                'name': [str(x.get('name', x.get('id'))) for x in config.get('rule_library', []) if isinstance(x, dict)],
                'service': ['mihomo', 'surge', 'loon', 'mosdns', 'oxidns'],
                'operation': ['status'] if method == 'GET' else ['preview'], 'resource': ['configs', 'proxies', 'rules', 'connections', 'version'],
                'filename': ['loon.lcf'] if '/loon/' in route else ['configflow-agent-linux-amd64'],
                'action': ['file', 'download'], 'preview_id': stored['.generated-previews'],
                'deployment_id': stored['deployments'], 'job_id': stored['probes'],
                'session_id': stored['gateway-installations'] if '/gateway-installations/' in route else stored['setup-sessions'],
                'record_id': [], 'token': stored['tokens'],
            }
            names = PARAM.findall(route)
            no_data = any(not params.get(name) for name in names)
            for values in itertools.product(*(params.get(name) or ['proddiff-missing'] for name in names)):
                mapping = dict(zip(names, values))
                path = PARAM.sub(lambda match: urllib.parse.quote(mapping[match[1]], safe=''), route)
                query = {'profile_id': profile_id}
                agent = next((a for a in agents if str(a.get('id')) == mapping.get('agent_id')), {})
                if route.endswith('/config') and '/agents/' in route:
                    query['token'] = agent.get('token', '')
                body = None
                if method == 'POST':
                    body = {'base_url': BASE_URL}
                    if route.endswith('/docker-oxidns-ha-guide'):
                        body.update(action='inspect')
                    elif route.endswith('/generate-config'):
                        body.update(expected_profile_id=agent.get('profile_id') or profile_id,
                                    expected_service_type=agent.get('service_type'))
                    elif '/oxidns/' in route:
                        body.update(settings=config.get('oxidns', {}), changes={},
                                    native_config=config.get('oxidns', {}).get('native_config', ''))
                    elif '/linux-gateway/' in route:
                        body.update(settings=config.get('linux_gateway', {}))
                    elif route.endswith('/preview-regex'):
                        body.update(source='subscription', source_id=next(iter(ids(config.get('subscriptions', []))), ''), regex='.*')
                def emit(variant):
                    yield method, route, path + '?' + urllib.parse.urlencode(variant), body, no_data
                    # Once per route, also let each server resolve the default profile itself.
                    if profile_index == 0 and '<profile_id>' not in route and method == 'GET':
                        rest = {k: v for k, v in variant.items() if k != 'profile_id'}
                        yield method, route, path + ('?' + urllib.parse.urlencode(rest) if rest else ''), body, no_data

                if '/subscriptions/' in route and route.endswith('/proxies'):
                    for output_format in ('mihomo', 'surge', 'loon'):
                        variant = dict(query, format=output_format)
                        yield from emit(variant)
                elif route.endswith('/file') or mapping.get('action') == 'file':
                    for filename in preview_files.get(mapping.get('preview_id'), ['config.yaml']):
                        variant = dict(query, path=filename)
                        yield from emit(variant)
                elif route in ('/api/agents/install-script', '/api/agents/docker-agent-compose', '/api/agents/docker-agent-run'):
                    for target in agents or [{}]:
                        variant = dict(query, server_url=doc['system'].get('system_config', {}).get('server_domain') or BASE_URL,
                                       name=target.get('name', 'proddiff'), agent_name=target.get('name', 'proddiff'),
                                       agent_ip=target.get('host', ''), type=target.get('service_type', 'mihomo'),
                                       port=target.get('port', 8080), enable_mihomo='true', enable_oxidns='true')
                        yield from emit(variant)
                else:
                    yield from emit(query)


def digest_tree(root):
    digest = hashlib.sha256()
    for file in sorted(root.rglob('*')):
        if file.is_symlink():
            raise RuntimeError('snapshot symlinks are not supported')
        if file.is_file():
            digest.update(str(file.relative_to(root)).encode())
            with file.open('rb') as stream:
                for chunk in iter(lambda: stream.read(1024 * 1024), b''):
                    digest.update(chunk)
    return digest.digest()

# Background starters disabled in flask_worker.py; none of these thread names may ever start.
PY_DISABLED_THREADS = {'dns-follow', 'oxidns-ha-publish', 'ipv6-verification-scheduler', 'agent-rule-sync-loop',
                       'metrics-history-flush', 'oxidns-ranking-history', 'domain-discovery-auto',
                       'scheduled-refresh', 'router-rule-sync', 'sub-store-idle'}
RULE_FIELDS = {'route', 'path', 'reason', 'pattern', 'tolerance', 'bracket', 'unordered'}


def assert_no_egress():
    """Prove the namespace has no route out: interfaces, host IPC resolvers, DNS and TCP."""
    if [name for _, name in socket.if_nameindex()] != ['lo']:
        raise RuntimeError('network isolation missing')
    # Filesystem sockets are not network-namespaced: systemd-resolved (nss-resolve), nscd,
    # D-Bus and Docker would all carry requests out of an otherwise empty netns.
    for path in ('/run/systemd/resolve/io.systemd.Resolve', '/run/nscd/socket', '/run/dbus/system_bus_socket',
                 '/run/docker.sock', '/var/run/docker.sock'):
        if os.path.exists(path):
            raise RuntimeError('host IPC socket visible in namespace')
    try:
        socket.getaddrinfo('example.com', 80)
    except socket.gaierror:
        pass
    else:
        raise RuntimeError('DNS resolution escapes the namespace')
    for family, address in ((socket.AF_INET, ('192.0.2.1', 80)), (socket.AF_INET6, ('2001:db8::1', 80))):
        with socket.socket(family, socket.SOCK_STREAM) as probe:
            probe.settimeout(2)
            try:
                probe.connect(address)
            except OSError as error:
                if error.errno not in (errno.ENETUNREACH, errno.EHOSTUNREACH, errno.EADDRNOTAVAIL):
                    raise RuntimeError('unexpected egress probe result') from None
            else:
                raise RuntimeError('egress connection succeeded')


def lock_runs(private):
    """Shared lock for the run; whoever gets it exclusively first removes copies of killed runs."""
    handle = (private / '.lock').open('a')
    try:
        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError:
        pass
    else:
        for stale in private.glob('run-*/copies-*'):
            shutil.rmtree(stale, ignore_errors=True)
    fcntl.flock(handle, fcntl.LOCK_SH)
    return handle


def load_rules(rows):
    routes = {row[1] for row in rows}
    rules = json.loads((PARITY / 'proddiff-normalize.json').read_text())
    for rule in rules:
        valid = (set(rule) <= RULE_FIELDS and rule.get('reason') and rule.get('route') in routes
                 and '*' not in rule['route'] + rule.get('path', '')
                 and (('path' not in rule) if rule.get('bracket') else bool(rule.get('path')))
                 and sum(key in rule for key in ('pattern', 'tolerance', 'unordered')) <= 1)
        if not valid:
            raise RuntimeError('normalization requires an exact route/path and reason')
        if 'pattern' in rule:
            re.compile(rule['pattern'])
    return rules


def phase(case):
    """Reads first (parallel), then read-only POSTs that may stage artifacts (parallel),
    then log readers serially: they observe what every earlier request logged."""
    method, route = case[0], case[1]
    return 2 if route.startswith('/api/logs/') else 1 if method != 'GET' else 0


def compare_http(python, go, diffs, bracket=False, attempts=3):
    """Recover only one-sided transport failures, then compare adjacent HTTP replies.

    Each side has a budget of three transport retries per case (40/80/160 s).
    Windowed GETs may additionally alternate sides for at most three bracket rounds.
    A transport failure or differing HTTP status stops bracketing immediately.
    Returns (python reply, go reply, bracket rescue round, per-side transport retries).
    """
    from content_policy import checked_reply

    def observe(send):
        def request(*, timeout):
            return checked_reply(send(timeout=timeout))
        return request

    senders = tuple(observe(send) for send in (python, go))
    replies = [senders[0](timeout=20), senders[1](timeout=20)]
    retries = [0, 0]
    last = 1

    def recover(index):
        while replies[index][0] == 0 and retries[index] < 3:
            retries[index] += 1
            replies[index] = senders[index](timeout=20 * 2 ** retries[index])

    if (replies[0][0] == 0) != (replies[1][0] == 0):
        last = 0 if replies[0][0] == 0 else 1
        recover(last)

    def cannot_bracket():
        return not replies[0][0] or not replies[1][0] or replies[0][0] != replies[1][0]

    if cannot_bracket() or not bracket or not diffs(*replies):
        return *replies, 0, retries
    for step in range(attempts * 2):
        # Recovery may have made Python the latest reply. Always ask the other side
        # next, so equality never uses a Python reply separated from Go by another reply.
        last = 1 - last
        replies[last] = senders[last](timeout=20)
        recover(last)
        if cannot_bracket():
            return *replies, 0, retries
        if not diffs(*replies):
            return *replies, 1 + step // 2, retries
    return *replies, 0, retries


def stop_processes(processes):
    for process in processes:
        if process.poll() is None:
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
        process.wait()


def migrate_baselines(go_copy, report):
    """Test-copy-only format migration; no production or source snapshot writes."""
    proof = report / 'baseline-format'
    proof.mkdir(mode=0o700)
    database = go_copy / 'configflow.sqlite3'
    def rows():
        with sqlite3.connect(database) as connection:
            return (list(connection.execute('SELECT scope, owner, field, position, value, revision FROM fields ORDER BY scope,owner,field')),
                    list(connection.execute('SELECT key,value FROM meta ORDER BY key')))
    old = document(go_copy).get('system', {}).get('agents', [])
    (proof / 'agents-before.json').write_text(json.dumps(old, ensure_ascii=False))
    if not any(isinstance(agent, dict) and agent.get('applied_content') for agent in old):
        (proof / 'agents-after.json').write_text(json.dumps(old, ensure_ascii=False))
        (proof / 'summary.json').write_text(json.dumps({'converted': 0, 'proof': 'no historical baseline'}))
        return
    before = rows()
    env = dict(os.environ, DATA_DIR=str(go_copy), PRODDIFF_BASELINE_MIGRATION='1',
               LOG_LEVEL='ERROR', LOG_FILE=str(proof / 'worker.log'), SUB_STORE_BUILTIN_DIR='/nonexistent')
    with (proof / 'worker.log').open('wb') as log:
        subprocess.run([os.environ['PRODDIFF_BASELINE_TOOL'], str(go_copy), str(proof)],
                       env=env, stdout=log, stderr=log, timeout=600, check=True)
    if rows() != before:
        raise RuntimeError('baseline proof worker unexpectedly modified database')
    new = json.loads((proof / 'agents-after.json').read_text())
    old_projection, new_projection = json.loads(json.dumps(old)), json.loads(json.dumps(new))
    for records in (old_projection, new_projection):
        for record in records:
            if isinstance(record, dict) and isinstance(record.get('applied_content'), dict):
                for field in ('fingerprint', 'files', 'deployed_config_sha256'):
                    record['applied_content'].pop(field, None)
    if old_projection != new_projection:
        raise RuntimeError('baseline migration changed a field outside its private format identity')
    with sqlite3.connect(database) as connection:
        cursor = connection.execute("UPDATE fields SET value=? WHERE scope='system' AND owner='' AND field='agents'",
                                    ((proof / 'agents-after.json').read_text(),))
        if cursor.rowcount != 1:
            raise RuntimeError('baseline migration did not patch exactly one existing row')
    after = rows()
    expected = [(scope, owner, field, position, (proof / 'agents-after.json').read_text() if (scope, owner, field) == ('system', '', 'agents') else value, revision)
                for scope, owner, field, position, value, revision in before[0]]
    if after != (expected, before[1]):
        raise RuntimeError('baseline migration changed database revision metadata')


def main():
    global PRIVATE_REPORT
    load_start = Path('/proc/loadavg').read_text().strip()
    assert_no_egress()
    snapshot, binary = map(Path, sys.argv[1:3])
    record = '--record-baseline' in sys.argv[3:]
    if not os.statvfs(snapshot).f_flag & os.ST_RDONLY:
        raise RuntimeError('snapshot mount is writable')
    before = digest_tree(snapshot)
    private = Path.home() / 'cf-prod-snapshot/runs'
    run_lock = lock_runs(private)
    report = Path(tempfile.mkdtemp(prefix='run-', dir=private))
    PRIVATE_REPORT = report
    processes = []
    def stop_signal(signum, frame):
        raise RuntimeError('interrupted')
    for signum in (signal.SIGTERM, signal.SIGINT, signal.SIGHUP):
        signal.signal(signum, stop_signal)
    rows = [line.split('\t') for line in (PARITY / 'routes.tsv').read_text().splitlines() if line and not line.startswith('#')]
    rules = load_rules(rows)
    bracket = {rule['route'] for rule in rules if rule.get('bracket')}
    try:
        with tempfile.TemporaryDirectory(prefix='copies-', dir=report) as temporary, contextlib.ExitStack() as cleanup:
            cleanup.callback(stop_processes, processes)
            copies = [Path(temporary) / name for name in ('python', 'go')]
            for data in copies:
                shutil.copytree(snapshot, data)
            migrate_baselines(copies[1], report)
            doc = document(copies[0])
            if not doc['profiles']:
                raise RuntimeError('snapshot has no profiles')
            for index, data in enumerate(copies):
                env = dict(os.environ, DATA_DIR=str(data), LOG_FILE=str(data / 'app.log'), LOG_LEVEL='ERROR',
                           ADMIN_USERNAME='proddiff-admin', ADMIN_PASSWORD='proddiff-test-password',
                           JWT_SECRET_KEY='proddiff-test-only-shared-secret', CONFIGFLOW_BACKEND_HOST='127.0.0.1',
                           CONFIGFLOW_BACKEND_PORT=str(15001 + index), CONFIGFLOW_FRONTEND_DIR='',
                           SUB_STORE_BUILTIN_DIR='/nonexistent', PRODDIFF_ROUTES=str(report / 'go-routes.tsv'),
                           PRODDIFF_PY_ROUTES=str(report / 'python-routes.tsv'),
                           PRODDIFF_PY_LOOPS=str(report / 'python-loops.txt'),
                           PRODDIFF_PY_THREADS=str(report / 'python-threads.txt'))
                command = [sys.executable, 'server/tools/proddiff/flask_worker.py'] if index == 0 else [str(binary)]
                with (report / ('python.log' if index == 0 else 'go.log')).open('wb') as log:
                    processes.append(subprocess.Popen(command, env=env, stdout=log, stderr=log, start_new_session=True))
            tokens = []
            ready = [None, report / 'go-routes.tsv.ready']
            for index in range(2):
                for _ in range(450):
                    if processes[index].poll() is not None:
                        raise RuntimeError('server startup failed; inspect private logs')
                    reply = send(15001 + index, 'GET', '/api/auth/status')
                    # Go serves before lifecycle.Start; wait until its startup work finished.
                    if reply[0] == 200 and (ready[index] is None or ready[index].exists()):
                        break
                    time.sleep(.2)
                else:
                    raise RuntimeError('server startup timeout')
                login = send(15001 + index, 'POST', '/api/auth/login', body={'username': 'proddiff-admin', 'password': 'proddiff-test-password'})
                if login[0] != 200:
                    raise RuntimeError('test login failed')
                tokens.append(json.loads(login[2])['token'])
            implemented = set(tuple(line.split('\t')) for line in (report / 'go-routes.tsv').read_text().splitlines())
            known = {(row[0], row[1]) for row in rows}
            python_routes = set(tuple(line.split('\t')) for line in (report / 'python-routes.tsv').read_text().splitlines())
            # A route registered under another template would be silently reported as not implemented.
            unmapped = {'UNMAPPED-GO-ROUTE\t' + ' '.join(route) for route in implemented - known}
            unmapped |= {'UNMAPPED-PYTHON-ROUTE\t' + ' '.join(route) for route in python_routes - known}
            go_loops = set((report / 'go-routes.tsv.loops').read_text().split())
            go_startups = set((report / 'go-routes.tsv.startups').read_text().split())
            found, generator_coverage, generator_samples = run_workers(
                snapshot, Path(temporary), report, Path(os.environ['PRODDIFF_GENERATOR']),
                doc, implemented, response_diffs)
            real = scrub(send(15002, 'GET', '/api/auth/status', tokens[1]), copies[1])
            counts = collections.Counter()
            coverage, failures = {}, collections.Counter()
            http_successes = collections.defaultdict(set)
            all_cases = list(cases(rows, doc, copies[0]))

            agent_identities = AgentComparisonIdentities()
            def ask(index, method, route, path, body, timeout):
                from content_policy import REQUEST_SECONDS, ROUTES
                if method == 'GET' and route in ROUTES:
                    timeout = max(timeout, REQUEST_SECONDS)
                return scrub(send(15001 + index, method, path, tokens[index], body, timeout=timeout), copies[index])

            def compare(case):
                method, route, path, body, no_data = case
                if (method, route) not in implemented:
                    return case, None, None, 0, [0, 0]
                python = lambda timeout: ask(0, method, route, path, body, timeout)
                go = lambda timeout: ask(1, method, route, path, body, timeout)
                return (case,) + compare_http(python, go, lambda a, b: response_diffs(a, b, route, rules, agent_identities),
                                              bracket=method == 'GET' and route in bracket)

            def results():
                with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
                    for stage in (0, 1):
                        yield from pool.map(compare, [case for case in all_cases if phase(case) == stage])
                yield from map(compare, [case for case in all_cases if phase(case) == 2])

            json_sample = None
            transport_retries = {'python': 0, 'go': 0}
            with (report / 'responses.jsonl').open('w') as responses:
                for case, left, right, retries, transport in results():
                    method, route, path, body, no_data = case
                    key = method + ' ' + route
                    status = 'Go 尚未实现' if left is None else 'compared'
                    row = coverage.setdefault(key, collections.Counter())
                    row[status] += 1
                    row['no-snapshot-parameter'] += int(no_data)
                    if left is None:
                        continue
                    for index, label in enumerate(transport_retries):
                        transport_retries[label] += transport[index]
                    counts['bracket-rescued'] += int(retries > 0)
                    responses.write(json.dumps({'method': method, 'route': route, 'path': path,
                                                'python': [left[0], left[1], left[2].decode('utf-8', errors='replace'), left[3]],
                                                'go': [right[0], right[1], right[2].decode('utf-8', errors='replace'), right[3]]}, ensure_ascii=False) + '\n')
                    counts['compared'] += 1
                    if 200 <= right[0] < 300:
                        if 'yaml' in right[1]:
                            generator_samples.setdefault('yaml', right)
                        if 'text/plain' in right[1] and ('/config/' in route or '/generate/' in route or route.endswith('/install-script')):
                            generator_samples.setdefault('text', right)
                        if 'json' in right[1] and (json_sample is None or len(right[2]) > len(json_sample[2])):
                            json_sample = right
                        if set(right[3]) - IGNORED_HEADERS:
                            generator_samples.setdefault('headers', right)
                    for generator_key, row in generator_coverage.items():
                        name = generator_key.removeprefix('GENERATOR ')
                        if row['mode'] != 'http' or row['route'] != key:
                            continue
                        if name.startswith('subscription-'):
                            selected_format = urllib.parse.parse_qs(urllib.parse.urlsplit(path).query).get('format', ['mihomo'])[0]
                            if name != 'subscription-' + selected_format:
                                continue
                        if name in ('agent-mihomo', 'agent-mosdns', 'agent-oxidns', 'agent-surge'):
                            agent_id = urllib.parse.unquote(urllib.parse.urlsplit(path).path.split('/')[3])
                            agent = next((a for a in doc['system'].get('agents', []) if str(a.get('id')) == agent_id), {})
                            if name != 'agent-' + agent.get('service_type', 'mihomo'):
                                continue
                        row['compared'] = row.get('compared', 0) + 1
                        if 200 <= left[0] < 300 and 200 <= right[0] < 300:
                            identity = urllib.parse.urlsplit(path).path
                            if name.startswith('subscription-'):
                                identity += '?' + urllib.parse.parse_qs(urllib.parse.urlsplit(path).query).get('profile_id', [''])[0]
                            http_successes[generator_key].add(identity)
                            row['success'] = len(http_successes[generator_key])
                    for label, reply in (('python', left), ('go', right)):
                        if reply[0] == 0 or re.search(rb'ConnectionError|Network is unreachable|No route to host|Connection refused|Failed to establish|connect: network|dial tcp|timed out', reply[2], re.I):
                            failures[label + '-connection-failed'] += 1
                    if left[0] == 0 or right[0] == 0:
                        failures['both-transport-failure' if left[0] == right[0] == 0 else 'one-sided-transport-failure'] += 1
                    for field, kind in response_diffs(left, right, route, rules):
                        found.add((key, field, kind))
                        domain = 'agents' if '/agents' in route or route.startswith('GENERATOR agent-') else 'routers' if '/routers' in route or route.startswith('GENERATOR router-') else 'config'
                        counts[domain + ':' + kind] += 1
            if json_sample is not None:
                generator_samples['http-json'] = json_sample
            checks = self_check(real, generator_samples, response_diffs, safe_key, safe_header)
            print('self-check: ' + ' '.join(name + '=PASS' for name in checks), flush=True)
            started_threads = set((report / 'python-threads.txt').read_text().split('\n')) if (report / 'python-threads.txt').exists() else set()
            if started_threads & PY_DISABLED_THREADS:
                raise RuntimeError('a disabled Python background loop started')
            coverage.update(generator_coverage)
            baseline_path = PARITY / 'proddiff-baseline.txt'
            baseline = set()
            for line in baseline_path.read_text().splitlines():
                if line and not line.startswith('#'):
                    method_route, field, kind, reason = line.split('\t', 3)
                    if not record and (not reason.strip() or reason.strip() == 'open'):
                        raise RuntimeError('baseline entry needs a reason')
                    baseline.add((method_route, field, kind))
            reasons = {}
            for line in baseline_path.read_text().splitlines():
                if line and not line.startswith('#'):
                    parts = line.split('\t', 3)
                    reasons[tuple(parts[:3])] = parts[3]
            formatted = '\n'.join('\t'.join(row) + '\t' + reasons.get(row, 'open') for row in sorted(found)) + '\n'
            (report / 'differences.tsv').write_text(formatted)
            if record:
                baseline_path.write_text('# Route template\tkey path\tdifference type\treason (entries marked open must be triaged before commit)\n' + formatted)
                baseline = found
            new, stale = found - baseline, baseline - found
            coverage_path = PARITY / 'proddiff-coverage.txt'
            current_coverage = {key for key, row in coverage.items() if row.get('compared')}
            current_coverage.update(key + ' success' for key, row in generator_coverage.items() if row.get('success'))
            # A route that loses every snapshot-derived parameter keeps only its error branch.
            current_coverage.update(key + ' snapshot-parameter' for key, row in coverage.items()
                                    if not key.startswith('GENERATOR ') and row.get('compared', 0) > row.get('no-snapshot-parameter', 0))
            previous_coverage = {line for line in coverage_path.read_text().splitlines() if line and not line.startswith('#')}
            if record:
                coverage_path.write_text('# Previously compared route templates; removals fail the gate.\n' + '\n'.join(sorted(current_coverage)) + '\n')
                previous_coverage = current_coverage
            lost_coverage = previous_coverage - current_coverage
            generator_floor_path = PARITY / 'proddiff-generator-coverage.json'
            if record:
                generator_floor_path.write_text(json.dumps(generator_coverage, ensure_ascii=False, indent=2) + '\n')
            generator_floors = json.loads(generator_floor_path.read_text())
            lost_coverage.update(successful_output_regressions(generator_floors, generator_coverage))
            unique_counts = collections.Counter()
            for route, field, kind in found:
                domain = 'agents' if '/agents' in route or route.startswith('GENERATOR agent-') else 'routers' if '/routers' in route or route.startswith('GENERATOR router-') else 'config'
                unique_counts[domain + ':' + kind] += 1
            http_rows = {key: row for key, row in coverage.items() if not key.startswith('GENERATOR ')}
            summary = {'GET_routes_compared': sum(key.startswith('GET ') and row.get('compared', 0) > 0 for key, row in http_rows.items()),
                       'GET_routes_error_branch_only': sum(key.startswith('GET ') and 0 < row.get('compared', 0) <= row.get('no-snapshot-parameter', 0) for key, row in http_rows.items()),
                       'GET_Go_not_implemented': sum(key.startswith('GET ') and row.get('Go 尚未实现', 0) > 0 for key, row in http_rows.items()),
                       'python_disabled_background': len((report / 'python-loops.txt').read_text().splitlines()),
                       'go_disabled_background': len(go_loops - go_startups),
                       'go_startup_once': sorted(go_startups),
                       'python_threads_started': len(started_threads - {''}),
                       'unique_differences_by_domain_type': dict(sorted(unique_counts.items())),
                       'no_snapshot_parameter_cases': sum(row.get('no-snapshot-parameter', 0) for row in http_rows.values()),
                       'GET_routes_total': sum(row[0] == 'GET' for row in rows),
                       'routes_total': len(http_rows), 'routes_compared': sum(row.get('compared', 0) > 0 for row in http_rows.values()),
                       'Go_not_implemented': sum(row.get('Go 尚未实现', 0) > 0 for row in coverage.values()),
                       'generators': generator_coverage, 'self_checks': checks,
                       'cases': len(all_cases), 'counts': dict(counts), 'connection_failures': dict(failures),
                       'transport_retries': transport_retries,
                       'loadavg': {'start': load_start, 'end': Path('/proc/loadavg').read_text().strip()},
                       'coverage_regressions': len(lost_coverage), 'unmapped_routes': len(unmapped),
                       'unique_differences': len(found), 'new': len(new), 'stale': len(stale), 'self_check': 'PASS'}
            (report / 'summary.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2))
            (report / 'coverage.json').write_text(json.dumps(coverage, ensure_ascii=False, indent=2))
            phase2 = [
                'POST /api/profiles', 'POST /api/profiles/<profile_id>/clone',
                'POST /api/config/import', 'POST /api/subscriptions/<sub_id>/fetch',
                'POST /api/rules/compose', 'PUT /api/agents/<agent_id>',
                'POST /api/agents/<agent_id>/heartbeat', 'POST /api/agents/<agent_id>/push-config',
                'POST /api/routers/<router_id>/apply', 'POST /api/routers/<router_id>/routes/apply',
            ]
            (report / 'report.md').write_text(
                '# Production differential\n\n```json\n' + json.dumps(summary, ensure_ascii=False, indent=2)
                + '\n```\n\nDifferences: route template / schema path / type only.\n\n'
                + formatted + '\nPhase two: replay writes with DB/file/device-effect comparison.\n\n'
                + '\n'.join('- ' + route for route in phase2) + '\n')
            # A safe report is deliberately stored separately from private server logs.
            print(json.dumps(summary, ensure_ascii=False), flush=True)
            print(f'baseline: new={len(new)} stale={len(stale)}' + ('; update baseline' if stale else ''), flush=True)
            for entry in sorted(new):
                print('NEW\t' + '\t'.join(entry), flush=True)
            for route in sorted(lost_coverage):
                print('COVERAGE-REGRESSION\t' + route, flush=True)
            for route in sorted(unmapped):
                print(route, flush=True)
            return int(bool(new or stale or lost_coverage or unmapped or failures['one-sided-transport-failure']))
    finally:
        stop_processes(processes)
        run_lock.close()
        if digest_tree(snapshot) != before:
            raise RuntimeError('snapshot changed')


if __name__ == '__main__':
    try:
        sys.exit(main())
    except Exception as error:
        if PRIVATE_REPORT is not None:
            (PRIVATE_REPORT / 'error.txt').write_text(traceback.format_exc())
        # No traceback/exception values: these can contain snapshot secrets.
        print('proddiff: infrastructure failure (' + type(error).__name__ + '); inspect private run directory', flush=True)
        sys.exit(2)
