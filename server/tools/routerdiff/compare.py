"""Fail-closed comparisons; no key-name masks or Python-value substitution."""
from dataclasses import dataclass
import base64
from datetime import datetime, timezone
import json
import re


@dataclass(frozen=True)
class Difference:
    path: str
    python: object
    go: object
    category: str


def pointer(value):
    return str(value).replace('~', '~0').replace('/', '~1')


def differences(left, right, path=''):
    """Compare all typed leaves, object keys and array positions recursively."""
    if type(left) is not type(right):
        return [Difference(path or '/', left, right, 'type')]
    if isinstance(left, dict):
        result = []
        for key in sorted(left.keys() | right.keys()):
            child = path + '/' + pointer(key)
            if key not in left or key not in right:
                result.append(Difference(child, left.get(key, '<missing>'), right.get(key, '<missing>'), 'key'))
            else:
                result.extend(differences(left[key], right[key], child))
        return result
    if isinstance(left, list):
        result = []
        if len(left) != len(right):
            result.append(Difference(path + '/length', len(left), len(right), 'length'))
        for index, (a, b) in enumerate(zip(left, right)):
            result.extend(differences(a, b, path + '/' + str(index)))
        return result
    return [] if left == right else [Difference(path or '/', left, right, 'value')]


def headers(response):
    # HTTP framing and implementation branding differ independently of API
    # semantics. Length is still independently checked against raw body bytes.
    ignored = {'date', 'server', 'content-length', 'transfer-encoding', 'connection'}
    result = {}
    for name, value in response['headers']:
        key = name.lower()
        if key not in ignored:
            result.setdefault(key, []).append(value)
        if key == 'content-length' and response.get('method') != 'HEAD':
            if int(value) != len(base64.b64decode(response['body'])):
                raise ValueError('invalid HTTP Content-Length')
    if 'allow' in result:
        # Werkzeug builds Allow from a set: iteration order varies per process.
        # Retain every case-sensitive method and duplicate, normalize order only.
        methods = [part.strip() for value in result['allow'] for part in value.split(',')]
        if any(not re.fullmatch(r"[!#$%&'*+.^_`|~0-9A-Za-z-]+", method) for method in methods):
            raise ValueError('invalid HTTP Allow method')
        result['allow'] = [', '.join(sorted(methods))]
    return result


def response_view(response):
    return {'status': response['status'], 'headers': headers(response),
            'kind': response['kind'], 'body': response['content']}


def logical_snapshot(snapshot):
    """Decode logical JSON columns, preserving schema, position and revision."""
    result = json.loads(json.dumps(snapshot))
    for name, table in result['database'].items():
        if 'value' in table['columns']:
            index = table['columns'].index('value')
            for row in table['rows']:
                # Store values are JSON text; serialization whitespace is not a
                # public storage difference. Keep unparseable values verbatim.
                try:
                    row[index] = json.loads(row[index])
                except (ValueError, TypeError):
                    pass
        table['rows'].sort(key=lambda row: json.dumps(row[:2], ensure_ascii=False))
    for request in result['fake']['transcript']:
        # R04 accepted JSON whitespace only. Scripts remain exact string values.
        if request['body'] is not None:
            request.pop('raw')
            request['headers'].pop('content-length', None)
        # Client branding/connection reuse is not a device protocol contract.
        for key in ('user-agent', 'accept-encoding', 'connection'):
            request['headers'].pop(key, None)
    return result


class Bijection:
    """Scoped, validated dynamic identities; no regex replacement in messages."""
    def __init__(self, pattern):
        self.pattern = re.compile(pattern)
        self.forward, self.reverse = {}, {}

    def pair(self, left, right):
        if not all(isinstance(value, str) and self.pattern.fullmatch(value) for value in (left, right)):
            raise ValueError('invalid dynamic identity shape')
        if left in self.forward and self.forward[left] != right:
            raise ValueError('dynamic identity changed mapping')
        if right in self.reverse and self.reverse[right] != left:
            raise ValueError('dynamic identity mapping is not injective')
        self.forward[left], self.reverse[right] = right, left


def timestamp(value, start, end):
    """Validate an explicit source-proven runtime timestamp before pairing it."""
    if not isinstance(value, str):
        raise ValueError('timestamp must be text')
    if not re.fullmatch(r'[0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}:[0-9]{2}'
                        r'(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})?', value):
        raise ValueError('timestamp must contain an ISO date and time')
    parsed = datetime.fromisoformat(value.replace('Z', '+00:00'))
    parsed = parsed.replace(tzinfo=timezone.utc) if parsed.tzinfo is None else parsed
    if not start <= parsed.timestamp() <= end:
        raise ValueError('timestamp outside operation window')
    return parsed
