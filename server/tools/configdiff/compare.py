"""Strict HTTP, structured-output, SQLite-row and file comparators.

No blanket masks: volatile values require an explicit path or a validated
one-to-one identity binding. The same functions are used by mutation self-tests.
"""
from dataclasses import dataclass, field
from datetime import datetime, timezone
import hashlib
import io
import json
import math
from pathlib import Path
import re
import sqlite3
import tarfile
import zipfile
import yaml
from zoneinfo import ZoneInfo


@dataclass
class Difference:
    path: str
    kind: str
    python: object
    go: object


@dataclass
class Policy:
    times: set = field(default_factory=set)
    durations: set = field(default_factory=set)
    epochs: dict = field(default_factory=dict)
    future_times: dict = field(default_factory=dict)
    tz: str = 'UTC'
    # Only separately diagnosed CPython exception messages may use this exact path.
    internal_errors: dict = field(default_factory=dict)
    start: float = 0
    end: float = float('inf')
    identities: dict = field(default_factory=dict)
    reverse: dict = field(default_factory=dict)

    def bind(self, left, right, pattern, label):
        if not isinstance(left, str) or not isinstance(right, str):
            raise AssertionError(f'{label}: identities must be strings')
        if not re.fullmatch(pattern, left) or not re.fullmatch(pattern, right):
            raise AssertionError(f'{label}: invalid identity format')
        if left in self.identities and self.identities[left] != right:
            raise AssertionError(f'{label}: unstable Python identity')
        if right in self.reverse and self.reverse[right] != left:
            raise AssertionError(f'{label}: non-injective identity')
        self.identities[left], self.reverse[right] = right, left


def timestamp(value, policy, offset=0):
    if not isinstance(value, str):
        return False
    try:
        parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
        if parsed.tzinfo is None:
            parsed = parsed.replace(tzinfo=ZoneInfo(policy.tz))
        return policy.start + offset - 2 <= parsed.timestamp() <= policy.end + offset + 2
    except (ValueError, OverflowError):
        return False


def differences(left, right, path='body', policy=None):
    policy = policy or Policy()
    if path in policy.epochs:
        offset = policy.epochs[path]
        if any(type(v) is not int or not policy.start + offset - 2 <= v <= policy.end + offset + 2 for v in (left, right)):
            return [Difference(path, 'invalid-epoch-window', left, right)]
        return []
    if path in policy.durations:
        maximum = max(0, (policy.end - policy.start + 2) * 1000)
        if any(type(v) is not int or not 0 <= v <= maximum for v in (left, right)):
            return [Difference(path, 'invalid-duration', left, right)]
        return []
    if path in policy.internal_errors:
        if type(left) is not str or type(right) is not str or not left or not right:
            return [Difference(path, 'invalid-internal-error', left, right)]
        prefix = policy.internal_errors[path]
        if not left.startswith(prefix) or not right.startswith(prefix):
            return [Difference(path, 'internal-error-prefix', left, right)]
        return []
    if path in policy.times or path in policy.future_times:
        offset = policy.future_times.get(path, 0)
        if not timestamp(left, policy, offset) or not timestamp(right, policy, offset):
            return [Difference(path, 'invalid-time-window', left, right)]
        # Format and zone remain observable; only independently sampled value varies.
        def shape(value):
            return re.sub(r'\d', '0', value)
        if shape(left) != shape(right):
            return [Difference(path, 'time-format', left, right)]
        return []
    if isinstance(left, str) and isinstance(right, str) and policy.identities.get(left) == right:
        return []
    if type(left) is not type(right):
        # JSON numeric spellings do not carry a type; bool always remains distinct.
        if type(left) in (int, float) and type(right) in (int, float) and left == right:
            return []
        return [Difference(path, 'type', left, right)]
    if isinstance(left, dict):
        out = []
        mapped = {policy.identities.get(k, k): k for k in left}
        for key in sorted(mapped.keys() | right.keys(), key=str):
            original = mapped.get(key, key)
            child = path + '.' + str(original)
            if key not in mapped:
                out.append(Difference(child, 'extra-in-go', None, right[key]))
            elif key not in right:
                out.append(Difference(child, 'missing-in-go', left[original], None))
            else:
                out.extend(differences(left[original], right[key], child, policy))
        return out
    if isinstance(left, list):
        out = [] if len(left) == len(right) else [Difference(path, 'length', len(left), len(right))]
        for index, (a, b) in enumerate(zip(left, right)):
            out.extend(differences(a, b, f'{path}[{index}]', policy))
        return out
    if isinstance(left, float) and math.isnan(left) and math.isnan(right):
        return []
    return [] if left == right else [Difference(path, 'value', left, right)]


def structure(body, content_type='', filename=''):
    if 'json' in content_type or filename.endswith(('.json', '.json.bak')):
        return json.loads(body)
    if body.startswith(b'PK\x03\x04'):
        with zipfile.ZipFile(io.BytesIO(body)) as archive:
            if len(set(archive.namelist())) != len(archive.namelist()):
                raise ValueError('duplicate ZIP member')
            return {n: {'archive_directory': True} if n.endswith('/') else structure(archive.read(n), '', n)
                    for n in archive.namelist()}
    if body.startswith(b'\x1f\x8b') and filename.endswith(('.tar.gz', '.tgz')):
        with tarfile.open(fileobj=io.BytesIO(body)) as archive:
            members = archive.getmembers()
            if len({m.name for m in members}) != len(members):
                raise ValueError('duplicate tar member')
            if any(not m.isfile() and not m.isdir() for m in members):
                raise ValueError('non-file tar member')
            return {m.name: {'mode': m.mode, 'archive_directory': True} if m.isdir() else
                    {'mode': m.mode, 'content': structure(archive.extractfile(m).read(), '', m.name)} for m in members}
    if 'yaml' in content_type or filename.endswith(('.yaml', '.yml', '.yaml.bak', '.yml.bak')):
        return yaml.safe_load(body)
    if filename in ('surge.conf', 'loon.lcf') or filename.endswith(('/generated/config.conf', '/generated/loon.lcf')):
        return ini_structure(body.decode('utf-8'))
    try:
        return body.decode('utf-8').replace('\r\n', '\n').replace('\r', '\n').splitlines()
    except UnicodeError:
        return body


def ini_structure(text):
    """Known generated Surge/Loon text, preserving section and rule order."""
    def fields(value):
        parts, start, quoted, escaped = [], 0, False, False
        for i, char in enumerate(value):
            if escaped:
                escaped = False
            elif char == '\\':
                escaped = True
            elif char == '"':
                quoted = not quoted
            elif char == ',' and not quoted:
                parts.append(value[start:i].strip())
                start = i + 1
        parts.append(value[start:].strip())
        def token(value):
            if value.startswith('"') and value.endswith('"'):
                try:
                    return json.loads(value)
                except ValueError:
                    pass
            return value
        return [token(v) for v in parts]
    result = {'preamble': [], 'sections': []}
    entries = result['preamble']
    section = ''
    for raw in text.replace('\r\n', '\n').replace('\r', '\n').splitlines():
        line = raw.strip()
        if not line:
            continue
        if line.startswith('[') and line.endswith(']'):
            section = line[1:-1].strip()
            item = {'name': section, 'entries': []}
            result['sections'].append(item)
            entries = item['entries']
        elif line.startswith(('#', ';', '//')):
            # Keep directives and comments, including user-visible metadata.
            entries.append({'comment': line})
        elif '=' in line and section.lower() not in ('rule', 'rules', 'script'):
            key, value = line.split('=', 1)
            entries.append({'key_fields': fields(key.strip()), 'values': fields(value)})
        else:
            entries.append({'values': fields(line)})
    return result


HEADERS = ('content-type', 'content-disposition', 'cache-control', 'etag', 'x-configflow-profile',
           'location', 'allow', 'access-control-allow-origin', 'access-control-allow-headers',
           'access-control-allow-methods', 'access-control-expose-headers', 'vary',
           'x-configflow-provider', 'x-configflow-rule-cache', 'warning',
           'last-modified', 'accept-ranges', 'content-range', 'referrer-policy', 'x-content-type-options')


def response_diffs(left, right, policy=None, *, method='GET', filename='', projection=None):
    policy = policy or Policy()
    out = differences(left.status, right.status, 'status', policy)
    for key in HEADERS:
        a, b = left.headers.get(key, ''), right.headers.get(key, '')
        if key in ('allow', 'access-control-allow-methods', 'access-control-allow-headers', 'vary'):
            # HTTP token-list order is not semantic; keep duplicates and missing tokens observable.
            a, b = [sorted(part.strip() for part in v.split(',')) if v else [] for v in (a, b)]
        out.extend(differences(a, b, 'headers.' + key, policy))
    for label, response in (('python', left), ('go', right)):
        if method == 'HEAD' and response.body:
            out.append(Difference('body', 'nonempty-HEAD-' + label, len(response.body), None))
    # O6: Werkzeug HTML internals are not a user-facing byte contract.
    if method == 'HEAD':
        return out
    if left.status == right.status and left.status in (204, 304):
        for label, response in (('python', left), ('go', right)):
            if response.body:
                out.append(Difference('body', 'nonempty-' + str(response.status) + '-' + label,
                                      len(response.body), None))
        return out
    if left.status == right.status == 206:
        # A byte range can be an incomplete JSON/YAML/ZIP document. Compare the
        # actual fragment, with Content-Range already checked above.
        return out + differences(left.body, right.body, 'body.range-bytes', policy)
    ct_left, ct_right = left.headers.get('content-type', ''), right.headers.get('content-type', '')
    if left.status >= 400 and 'json' not in ct_left and 'json' not in ct_right:
        return out
    try:
        a, b = structure(left.body, ct_left, filename), structure(right.body, ct_right, filename)
        if projection:
            a, b = projection(a, 0), projection(b, 1)
    except (ValueError, UnicodeError, yaml.YAMLError, zipfile.BadZipFile, tarfile.TarError) as error:
        return out + [Difference('body', 'parse-error', type(error).__name__, '')]
    out.extend(differences(a, b, 'body', policy))
    return out


def database(directory):
    path = Path(directory, 'configflow.sqlite3')
    if not path.exists():
        raise AssertionError('server config store is missing')
    with sqlite3.connect(f'file:{path}?mode=ro', uri=True) as connection:
        metadata = {k: json.loads(v) for k, v in connection.execute('SELECT key,value FROM meta ORDER BY key')}
        sections = [list(row) for row in connection.execute('SELECT scope,owner,position FROM sections ORDER BY scope,owner')]
        fields = {f'{scope}/{owner}/{name}': {'position': position, 'revision': revision, 'value': json.loads(value)}
                  for scope, owner, name, position, value, revision in connection.execute(
                      'SELECT scope,owner,field,position,value,revision FROM fields ORDER BY scope,owner,field')}
    return {'meta': metadata, 'sections': sections, 'fields': fields}


def database_diffs(left, right, policy):
    a, b = database(left), database(right)
    out = []
    if 'commit_id' in a['meta'] and 'commit_id' in b['meta']:
        aid, bid = a['meta']['commit_id'], b['meta']['commit_id']
        pattern = r'[0-9a-f]{32}'
        if not all(isinstance(value, str) and re.fullmatch(pattern, value) for value in (aid, bid)):
            out.append(Difference('db.meta.commit_id', 'invalid-commit-identity', aid, bid))
        elif aid in policy.identities and policy.identities[aid] != bid:
            # A rejected Python request can leave its commit unchanged while
            # Go mistakenly commits. Report it; never rebind or abort the run.
            out.append(Difference('db.meta.commit_id', 'unstable-Go-commit', policy.identities[aid], bid))
        elif bid in policy.reverse and policy.reverse[bid] != aid:
            out.append(Difference('db.meta.commit_id', 'reused-Go-commit', aid, bid))
        else:
            policy.bind(aid, bid, pattern, 'commit-id')
    return out + differences(a, b, 'db', policy)


# Logs/lock files are compared in X07/T26, not silently included with runtime noise.
# Seed export is setup-only. SQLite is compared as rows, never physical page bytes.
RUNTIME_FILES = re.compile(r'^(app\.log(?:\.\d+)?|configflow\.sqlite3(?:-wal|-shm|-journal)?|configdiff-seed\.json)$|(^|/)[^/]+\.lock$')


def files(directory):
    root = Path(directory)
    out = {}
    for path in sorted(root.rglob('*')):
        relative = path.relative_to(root).as_posix()
        if path.is_symlink():
            out[relative] = {'symlink': str(path.readlink())}
        elif path.is_file() and not RUNTIME_FILES.search(relative):
            raw = path.read_bytes()
            try:
                content = database(path.parent) if path.name == 'configflow.sqlite3' else structure(raw, '', relative)
            except (ValueError, UnicodeError, AssertionError, sqlite3.Error, yaml.YAMLError, zipfile.BadZipFile, tarfile.TarError):
                # A deliberately damaged cache/backup is still observable data.
                # Preserve its complete raw bytes, do not drop or repair it.
                content = {'invalid_structured_file_bytes': raw}
            out[relative] = {'mode': path.stat().st_mode & 0o777, 'content': content}
    return out


def file_diffs(left, right, policy):
    return differences(files(left), files(right), 'files', policy)
