"""What a push would change: the last applied bundle against the one to send now.

The baseline is the private receipt kept for the applied deployment
(``rule_sync_store``); the candidate is the bundle the content comparison
prepares (``content_sync``). Nothing here builds bundles or contacts devices.

Files are compared by the same per-file digests as the "changed" status.
Changed provider, rule and main configuration files are then summarised from
their bytes; the main YAML is compared key by key. Values are returned as they
are, with every credential marked: credential fields, URL credentials and long
opaque URL parts, also inside the unified diff. A display text is a plain
string when nothing in it is a credential, else a list of strings and
``{'secret': text}`` pieces, so the dialog hides them on request without
another round trip. Domains, addresses and ports are never marked. All lists
and strings are bounded so a large rule set or a big configuration yields a
small response.
"""
from collections import Counter
import difflib
import io
import json
import re
import tarfile
from urllib.parse import unquote, urlsplit

import yaml

from .deployment_bundle import deployment_content_digests

MAX_CONFIG_ROWS = 200
MAX_LIST_ENTRIES = 60
MAX_ENTRY_FIELDS = 20
MAX_FIELD_ITEMS = 10
MAX_VALUE_CHARS = 240
MAX_RAW_LINES = 1500
MAX_RAW_BYTES = 160 * 1024
MAX_RAW_INPUT_LINES = 60000
MAX_LISTED_FILES = 500
MAX_DEPTH = 64
CONFIG_ROLE = 'config'
HIDDEN = '已隐藏'
STORED = '[REDACTED]'

# A field whose value is a credential, matched on the normalized key name.
_SECRET_KEYS = frozenset({
    'password', 'passwd', 'uuid', 'private-key', 'privatekey', 'pre-shared-key', 'secret', 'token', 'auth',
    'auth-str', 'authentication', 'authorization', 'proxy-authorization', 'psk', 'obfs-password', 'short-id',
    'api-key', 'apikey', 'access-token', 'client-secret', 'cookie', 'private-key-passphrase',
})
_SECRET_SUFFIXES = ('-password', '-passwd', '-passphrase', '-secret', '-token', '-psk', '-private-key')
# URL query parameters carrying a credential (lowercase, '_' read as '-').
_SECRET_PARAMS = frozenset({
    'token', 'access-token', 'auth', 'auth-token', 'key', 'api-key', 'apikey', 'password', 'passwd', 'pass',
    'pwd', 'secret', 'sign', 'signature', 'sig', 'uuid', 'code', 'ticket', 'credential', 'session', 'sid',
})
# Keys of these mappings are data (domains, provider names), never field names.
_DATA_KEYED = frozenset({
    ('hosts',), ('dns', 'hosts'), ('dns', 'nameserver-policy'), ('dns', 'proxy-server-nameserver-policy'),
    ('rule-providers',), ('proxy-providers',), ('sub-rules',),
})
_URL = re.compile(r"[A-Za-z][A-Za-z0-9+.\-]*://[^\s'\"<>]+")
_OPAQUE = re.compile(r'(?=[A-Za-z0-9_\-=~]*[0-9])(?=[A-Za-z0-9_\-=~]*[A-Za-z])[A-Za-z0-9_\-=~]{24,}')
_USERINFO = re.compile(r'^([^\s/:@]+):([^\s/@]+)@(\S+)$')
_YAML_ITEM = re.compile(r'^\s*-\s+(.*?)\s*$')
_MISSING = object()


class _Budget:
    """Rows kept in a response; the rest are only counted."""

    def __init__(self, limit):
        self.limit, self.rows, self.omitted = limit, [], 0

    @property
    def full(self):
        return len(self.rows) >= self.limit

    def add(self, row):
        if len(self.rows) < self.limit:
            self.rows.append(row)
        else:
            self.omitted += 1


def _json(value, **options):
    try:
        return json.dumps(value, sort_keys=True, ensure_ascii=False, default=str, **options)
    except TypeError:  # YAML allows mixed key types, which cannot be sorted
        return json.dumps(value, ensure_ascii=False, default=str, **options)


def _merge(spans):
    """Sorted, non-empty, non-overlapping (start, end) ranges."""
    merged = []
    for start, end in sorted(spans):
        if end <= start:
            continue
        if merged and start <= merged[-1][1]:
            merged[-1][1] = max(merged[-1][1], end)
        else:
            merged.append([start, end])
    return [(start, end) for start, end in merged]


class _Text:
    """One display text, built from plain and credential pieces in order."""

    __slots__ = ('chunks',)

    def __init__(self):
        self.chunks = []

    def add(self, text, secret=False):
        if text:
            self.chunks.append((text, secret))
        return self

    def clipped(self, limit=MAX_VALUE_CHARS):
        total = sum(len(text) for text, _ in self.chunks)
        if total <= limit:
            return self
        out, left = _Text(), limit
        for text, secret in self.chunks:
            if left <= 0:
                break
            out.add(text[:left], secret)
            left -= len(text)
        return out.add(f'…（共 {total} 字符）')

    def value(self):
        """The response form: a plain string, or strings and ``{'secret': text}`` pieces."""
        pieces = []
        for text, secret in self.chunks:
            if pieces and pieces[-1][0] == secret:
                pieces[-1][1].append(text)
            else:
                pieces.append((secret, [text]))
        if not any(secret for secret, _ in pieces):
            return ''.join(text for _, texts in pieces for text in texts)
        return [{'secret': ''.join(texts)} if secret else ''.join(texts) for secret, texts in pieces]


def unpack(value):
    """A display text as (text, credential ranges)."""
    if value is None:
        return '', []
    if isinstance(value, str):
        return value, []
    text, ranges = [], []
    position = 0
    for piece in value:
        part = piece['secret'] if isinstance(piece, dict) else piece
        if isinstance(piece, dict):
            ranges.append((position, position + len(part)))
        text.append(part)
        position += len(part)
    return ''.join(text), ranges


# The display texts of a response: file names, values, list entries, raw diff lines.
_TEXT_KEYS = frozenset({'name', 'old', 'new', 'text'})
_TEXT_LIST_KEYS = frozenset({'added_items', 'removed_items', 'lines'})


def _map_texts(value, rewrite):
    """``value`` with ``rewrite`` applied to every display text below it, nothing else."""
    if isinstance(value, dict):
        return {key: (rewrite(item) if key in _TEXT_KEYS
                      else [rewrite(text) for text in item] if key in _TEXT_LIST_KEYS and isinstance(item, list)
                      else _map_texts(item, rewrite))
                for key, item in value.items()}
    if isinstance(value, list):
        return [_map_texts(item, rewrite) for item in value]
    return value


def without_secrets(value, placeholder=HIDDEN):
    """A response as the dialog shows it with 隐藏敏感信息 on: credential pieces replaced."""
    def hide(text):
        if isinstance(text, list):
            return ''.join(placeholder if isinstance(piece, dict) else piece for piece in text)
        return text
    return _map_texts(value, hide)


def is_secret_key(key):
    name = str(key).strip().lower().replace('_', '-')
    if '.' in name or ' ' in name:
        return False  # a domain or other data key, not a field name
    return name in _SECRET_KEYS or name.endswith(_SECRET_SUFFIXES)


def _secret_field(path, key):
    """``key`` of the mapping at ``path`` names a credential, not data such as a domain."""
    return tuple(path) not in _DATA_KEYED and is_secret_key(key)


def _secret_param(name):
    name = name.lower().replace('_', '-')
    return name in _SECRET_PARAMS or name.endswith(('-token', '-key', '-secret', '-password'))


def _query_spans(url, start, stop):
    spans, position = [], start
    for piece in url[start:stop].split('&'):
        name, equals, value = piece.partition('=')
        decoded_name, decoded_value = unquote(name.replace('+', ' ')), unquote(value.replace('+', ' '))
        if _OPAQUE.fullmatch(decoded_name):
            spans.append((position, position + len(name)))
        if equals and decoded_value and (_secret_param(decoded_name) or _OPAQUE.fullmatch(decoded_value)):
            spans.append((position + len(name) + 1, position + len(piece)))
        position += len(piece) + 1
    return spans


def _url_spans(url):
    """Credentials of one URL: user[:password], credential parameters, opaque path parts."""
    try:
        urlsplit(url)
    except ValueError:
        return [(0, len(url))]
    head = url.index('://') + 3
    netloc_end = min([index for index in (url.find(mark, head) for mark in '/?#') if index >= 0] or [len(url)])
    spans = []
    at = url.rfind('@', head, netloc_end)
    if at >= 0:  # user[:password]@host:port keeps the host and port
        spans.append((head, at))
    fragment = url.find('#', netloc_end)
    stop = fragment if fragment >= 0 else len(url)
    question = url.find('?', netloc_end, stop)
    position = netloc_end
    for segment in url[netloc_end:question if question >= 0 else stop].split('/'):
        if _OPAQUE.fullmatch(segment):
            spans.append((position, position + len(segment)))
        position += len(segment) + 1
    if question >= 0:
        spans.extend(_query_spans(url, question + 1, stop))
    return spans


def secret_spans(text):
    """Where credentials sit inside one string, as sorted (start, end) ranges.

    URL user info, credential parameters and long opaque path parts, and the
    ``user:password`` of ``user:password@host:port``. Hosts, addresses and
    ports are never inside a range.
    """
    if not isinstance(text, str) or not text or ('://' not in text and '@' not in text):
        return []
    spans = []
    for word in re.finditer(r'\S+', text):
        start, value = word.start(), word.group(0)
        if '://' in value:
            for url in _URL.finditer(value):
                offset = start + url.start()
                spans.extend((offset + a, offset + b) for a, b in _url_spans(url.group(0)))
        else:
            match = _USERINFO.match(value)
            if match:  # user:password@host:port, e.g. a SOCKS5 upstream
                spans.append((start, start + match.end(2)))
    return _merge(spans)


def _string_into(text, out, *, secret=False, escape=None):
    escape = escape or (lambda part: part)
    if secret:
        out.add(escape(text), True)
        return
    position = 0
    for start, end in secret_spans(text):
        out.add(escape(text[position:start]))
        out.add(escape(text[start:end]), True)
        position = end
    out.add(escape(text[position:]))


def _json_escape(text):
    return json.dumps(text, ensure_ascii=False)[1:-1]


def _json_key(key):
    """A mapping key as json.dumps writes it (true, null, 1.5), any other type as text."""
    if isinstance(key, str):
        return key
    if key is None or isinstance(key, (bool, int, float)):
        return json.dumps(key)
    return str(key)


def _json_into(value, path, out, *, sort_keys, secret=False, depth=0):
    """JSON text of ``value`` (display separators), credentials marked."""
    if depth > MAX_DEPTH:
        out.add('"…"', secret)
    elif isinstance(value, dict):
        items = list(value.items())
        if sort_keys:
            try:
                items.sort(key=lambda item: item[0])
            except TypeError:  # YAML allows mixed key types, which cannot be sorted
                pass
        out.add('{', secret)
        for index, (key, item) in enumerate(items):
            out.add(', ' if index else '', secret)
            out.add(json.dumps(_json_key(key), ensure_ascii=False) + ': ', secret)
            _json_into(item, (*path, str(key)), out, sort_keys=sort_keys,
                       secret=secret or _secret_field(path, key), depth=depth + 1)
        out.add('}', secret)
    elif isinstance(value, (list, tuple, set, frozenset)):
        out.add('[', secret)
        for index, item in enumerate(value):
            out.add(', ' if index else '', secret)
            _json_into(item, path, out, sort_keys=sort_keys, secret=secret, depth=depth + 1)
        out.add(']', secret)
    elif isinstance(value, str):
        out.add('"', secret)
        _string_into(value, out, secret=secret, escape=_json_escape)
        out.add('"', secret)
    else:
        out.add(json.dumps(value, ensure_ascii=False, default=str), secret)


def _scalar(value):
    if value is None:
        return 'null'
    if isinstance(value, bool):
        return 'true' if value else 'false'
    if isinstance(value, str):
        return value
    if isinstance(value, (int, float)):
        return repr(value)
    return str(value)


def _show(value, path=(), *, secret=False, sort_keys=False, clip=True):
    """Display text for one value: strings as they are, mappings and lists as JSON."""
    out = _Text()
    if isinstance(value, (dict, list, tuple, set, frozenset)):
        _json_into(value, path, out, sort_keys=sort_keys, secret=secret)
    elif isinstance(value, str):
        _string_into(value, out, secret=secret)
    else:
        out.add(_scalar(value), secret)
    return (out.clipped() if clip else out).value()


def without_stored(value, find):
    """Credentials ConfigFlow itself stores, replaced inside every display text below ``value``.

    ``find`` returns their ranges in a text (``config_export.stored_secret_finder``).
    A credential piece touching one becomes a hidden ``[REDACTED]``; the rest of
    the text stays readable instead of the response hook blanking all of it.
    """
    def replace(display):
        if not isinstance(display, (str, list)):
            return display
        text, ranges = unpack(display)
        found = find(text)
        if not found:
            return display
        out, position = _Text(), 0
        for start, end in _merge([*ranges, *found]):
            out.add(text[position:start])
            touched = any(a < end and b > start for a, b in found)
            out.add(STORED if touched else text[start:end], True)
            position = end
        return out.add(text[position:]).value()
    return _map_texts(value, replace)


def _same(left, right):
    if isinstance(left, dict) and isinstance(right, dict):
        return left.keys() == right.keys() and all(_same(left[key], right[key]) for key in left)
    if isinstance(left, list) and isinstance(right, list):
        return len(left) == len(right) and all(_same(a, b) for a, b in zip(left, right))
    return type(left) is type(right) and left == right


def _identity(items):
    """The key naming each entry of a list of mappings (name / tag), if unique."""
    if not items or not all(isinstance(item, dict) for item in items):
        return None
    for key in ('name', 'tag'):
        names = [item.get(key) for item in items]
        if all(isinstance(name, str) and name for name in names) and len(set(names)) == len(names):
            return key
    return None


def _order_changed(old_keys, new_keys):
    common = set(old_keys) & set(new_keys)
    return [key for key in old_keys if key in common] != [key for key in new_keys if key in common]


def _identity_text(item):
    # Typed, so the string "1" and the number 1 stay different entries.
    if isinstance(item, str):
        return 's' + item
    return 'j' + _json(item)


def _scalar_list_diff(old, new, path):
    # Typed identities: the string "1" and the number 1 are different entries.
    old_raw = [_identity_text(item) for item in old]
    new_raw = [_identity_text(item) for item in new]
    removed = Counter(old_raw) - Counter(new_raw)
    added = Counter(new_raw) - Counter(old_raw)
    entries, omitted = [], 0
    for change, counter, raws, items in (('added', added, new_raw, new), ('removed', removed, old_raw, old)):
        remaining = Counter(counter)
        for raw, item in zip(raws, items):
            if remaining[raw] <= 0:
                continue
            remaining[raw] -= 1
            if len(entries) < MAX_LIST_ENTRIES:  # only listed entries are rendered
                entries.append({'change': change, 'text': _show(item, path, sort_keys=True)})
            else:
                omitted += 1
    reordered = not added and not removed and old_raw != new_raw
    if not reordered and (added or removed):
        reordered = _order_changed(list(dict.fromkeys(old_raw)), list(dict.fromkeys(new_raw)))
    return {'before': len(old), 'after': len(new), 'added': sum(added.values()), 'removed': sum(removed.values()),
            'modified': 0, 'reordered': reordered, 'entries': entries, 'omitted': omitted}


def _entry_fields(old, new, path):
    """Field changes inside one named entry (a node, a proxy group, a plugin)."""
    budget = _Budget(MAX_ENTRY_FIELDS)
    _walk(old, new, (), budget, base=path, nested=True)
    return budget.rows, budget.omitted


def _named_list_diff(old, new, key, path):
    old_by = {item[key]: item for item in old}
    new_by = {item[key]: item for item in new}
    added = [name for name in new_by if name not in old_by]
    removed = [name for name in old_by if name not in new_by]
    modified = [name for name in new_by if name in old_by and not _same(old_by[name], new_by[name])]
    entries, omitted = [], 0
    for change, names in (('added', added), ('removed', removed), ('modified', modified)):
        for name in names:
            if len(entries) >= MAX_LIST_ENTRIES:
                omitted += 1
                continue
            entry = {'change': change, 'text': _show(name)}
            if change == 'modified':
                entry['fields'], entry['fields_omitted'] = _entry_fields(old_by[name], new_by[name], path)
            entries.append(entry)
    return {'before': len(old), 'after': len(new), 'added': len(added), 'removed': len(removed),
            'modified': len(modified), 'reordered': _order_changed(list(old_by), list(new_by)),
            'entries': entries, 'omitted': omitted, 'identity': key}


def _list_diff(old, new, path):
    key = _identity(old) if old else _identity(new)
    if key and (not old or _identity(old) == key) and (not new or _identity(new) == key):
        return _named_list_diff(old, new, key, path)
    return _scalar_list_diff(old, new, path)


def _brief_list(diff):
    """A list change inside an entry: a few changed items, no nested detail."""
    items = {'added': [], 'removed': []}
    for entry in diff['entries']:
        if entry['change'] in items and len(items[entry['change']]) < MAX_FIELD_ITEMS:
            items[entry['change']].append(entry['text'])
    return {'before': diff['before'], 'after': diff['after'], 'added': diff['added'], 'removed': diff['removed'],
            'modified': diff['modified'], 'reordered': diff['reordered'],
            'added_items': items['added'], 'removed_items': items['removed']}


def _row(budget, path, full, change, old=_MISSING, new=_MISSING, *, sensitive=False, listed=None, nested=False):
    """One changed item; ``sensitive`` marks a credential field, whose whole value is secret."""
    if budget.full:  # counted, not rendered
        budget.omitted += 1
        return
    row = {'key': '.'.join(path), 'change': change, 'sensitive': sensitive}
    if listed is not None:
        row['list'] = _brief_list(listed) if nested else listed
    else:
        row['old'] = None if old is _MISSING else _show(old, full, secret=sensitive)
        row['new'] = None if new is _MISSING else _show(new, full, secret=sensitive)
    budget.add(row)


def _walk(old, new, path, budget, *, base=(), nested=False):
    """Append one row per changed leaf, list or secret below ``path``."""
    full = (*base, *path)
    if old is _MISSING and new is _MISSING:
        return
    if len(full) and is_secret_key(full[-1]) and full[:-1] not in _DATA_KEYED:
        if old is _MISSING or new is _MISSING or not _same(old, new):
            _row(budget, path, full, 'added' if old is _MISSING else 'removed' if new is _MISSING else 'modified',
                 old, new, sensitive=True)
        return
    change = 'added' if old is _MISSING else 'removed' if new is _MISSING else 'modified'
    if change != 'modified' and full[:-1] in _DATA_KEYED:
        # A whole provider or policy entry appears or goes: one row, not one per field.
        _row(budget, path, full, change, old, new)
        return
    if isinstance(old, dict) and isinstance(new, dict) or (
            change != 'modified' and isinstance(old if new is _MISSING else new, dict)
            and (old if new is _MISSING else new)):
        old_map = old if isinstance(old, dict) else {}
        new_map = new if isinstance(new, dict) else {}
        keys = list(new_map) + [key for key in old_map if key not in new_map]
        for key in keys:
            _walk(old_map.get(key, _MISSING), new_map.get(key, _MISSING), (*path, str(key)), budget,
                  base=base, nested=nested)
        return
    if isinstance(old, list) and isinstance(new, list) or (
            change != 'modified' and isinstance(old if new is _MISSING else new, list)):
        old_list = old if isinstance(old, list) else []
        new_list = new if isinstance(new, list) else []
        if _same(old_list, new_list) and change == 'modified':
            return
        _row(budget, path, full, change, listed=_list_diff(old_list, new_list, full), nested=nested)
        return
    if change == 'modified' and _same(old, new):
        return
    _row(budget, path, full, change, old, new)


def _load_yaml(data):
    text = data.decode('utf-8')
    loader = getattr(yaml, 'CSafeLoader', None)
    if loader is not None:
        try:
            return yaml.load(text, Loader=loader)
        except Exception:  # libyaml differs on edge cases; PyYAML decides
            pass
    return yaml.safe_load(text)


class _Whole:
    """A credential inside the copy dumped for the raw diff: the whole scalar is secret."""

    __slots__ = ('value',)

    def __init__(self, value):
        self.value = value

    def __hash__(self):
        return hash(self.value)

    def __eq__(self, other):
        return isinstance(other, _Whole) and other.value == self.value


class _Partial(str):
    """A string whose ``spans`` (character ranges) are credentials."""


def _marked(value, path=(), depth=0, secret=False):
    """A copy for dumping: credential scalars wrapped, everything else as it is."""
    if depth > MAX_DEPTH:
        return '…'
    if isinstance(value, dict):
        return {(_Whole(key) if secret else key):
                _marked(item, (*path, str(key)), depth + 1, secret or _secret_field(path, key))
                for key, item in value.items()}
    if isinstance(value, list):
        return [_marked(item, path, depth + 1, secret) for item in value]
    if isinstance(value, (set, frozenset)):
        return {_marked(item, path, depth + 1, secret) for item in value}
    if secret:
        return _Whole(value)
    if isinstance(value, str):
        spans = secret_spans(value)
        if spans:
            marked = _Partial(value)
            marked.spans = spans
            return marked
    return value


class _Sink:
    """The dump's text, with its length so far (an offset for each scalar written)."""

    def __init__(self):
        self.parts, self.size = [], 0

    def write(self, data):
        self.parts.append(data)
        self.size += len(data)

    def flush(self):
        pass


def _mark_node(node):
    if isinstance(node, yaml.ScalarNode):
        node.cf_mark = True
    elif isinstance(node, yaml.SequenceNode):
        for item in node.value:
            _mark_node(item)
    elif isinstance(node, yaml.MappingNode):
        for key, item in node.value:
            _mark_node(key)
            _mark_node(item)


class _MarkingDumper(yaml.SafeDumper):
    """``yaml.safe_dump`` that records where each marked scalar landed in the text."""

    def __init__(self, stream, **options):
        super().__init__(stream, **options)
        self.cf_pending = None
        self.cf_scalars = []

    def serialize_node(self, node, parent, index):
        if isinstance(node, yaml.ScalarNode):
            self.cf_pending = getattr(node, 'cf_mark', None)
        super().serialize_node(node, parent, index)

    def emit(self, event):
        if isinstance(event, yaml.ScalarEvent):
            event.cf_mark, self.cf_pending = self.cf_pending, None
        super().emit(event)

    def process_scalar(self):
        mark = getattr(self.event, 'cf_mark', None)
        if mark is None:
            return super().process_scalar()
        if self.analysis is None:
            self.analysis = self.analyze_scalar(self.event.value)
        if self.style is None:
            self.style = self.choose_scalar_style()
        style, value, start = self.style, self.event.value, self.stream.size
        super().process_scalar()
        self.cf_scalars.append((start, self.stream.size, style, value, mark))


def _represent_whole(dumper, data):
    node = dumper.represent_data(data.value)
    _mark_node(node)
    return node


def _represent_partial(dumper, data):
    node = dumper.represent_str(str(data))
    node.cf_mark = data.spans
    return node


_MarkingDumper.add_representer(_Whole, _represent_whole)
_MarkingDumper.add_representer(_Partial, _represent_partial)
_BREAKS = ' \n\x85\u2028\u2029'


def _locate(text, start, end, style, value, spans):
    """Output ranges of credential ``spans`` of a plain or single-quoted scalar, else None.

    Both styles write every other character verbatim and in order (a quote
    doubled); only spaces and line breaks can turn into folds and indentation.
    Credential spans never hold whitespace, so matching the rest suffices.
    """
    quoted = style == "'"
    if style not in ('', "'") or (quoted and text[start:start + 1] != "'"):
        return None
    position, where = start + quoted, {}
    last = max(stop for _, stop in spans)
    for index, char in enumerate(value[:last]):
        if char in _BREAKS:
            continue
        while position < end and text[position] in _BREAKS:
            position += 1
        if position >= end or text[position] != char:
            return None
        where[index] = position
        position += 1
        if quoted and char == "'":
            if text[position:position + 1] != "'":
                return None
            position += 1
    located = []
    for first, stop in spans:
        if first not in where or stop - 1 not in where:
            return None
        located.append((where[first], where[stop - 1] + (2 if quoted and value[stop - 1] == "'" else 1)))
    return located


def _dump_marked(document):
    """``yaml.safe_dump`` text of ``document`` and the ranges of its credentials."""
    sink = _Sink()
    dumper = _MarkingDumper(sink, allow_unicode=True, sort_keys=False, default_flow_style=False)
    try:
        dumper.open()
        dumper.represent(_marked(document))
        dumper.close()
    finally:
        dumper.dispose()
    text = ''.join(sink.parts)
    ranges = []
    for start, end, style, value, mark in dumper.cf_scalars:
        while start < end and text[start] == ' ':  # the separator written before the scalar
            start += 1
        located = None if mark is True else _locate(text, start, end, style, value, mark)
        ranges.extend([(start, end)] if located is None else located)
    return text, _merge(ranges)


_LINE_END = re.compile(r'\r\n|[\n\r\x0b\x0c\x1c\x1d\x1e\x85\u2028\u2029]')


def _lines(text, ranges):
    """``text.splitlines()`` and, per line index, the credential ranges inside that line."""
    lines, marks, position, pending = [], {}, 0, 0
    while position < len(text):
        found = _LINE_END.search(text, position)
        stop, after = (found.start(), found.end()) if found else (len(text), len(text))
        inside = []
        while pending < len(ranges) and ranges[pending][0] < stop:
            first, last = ranges[pending]
            if last > position:
                inside.append((max(first, position) - position, min(last, stop) - position))
            if last <= stop:
                pending += 1
            else:
                break
        if inside:
            marks[len(lines)] = [(first, last) for first, last in inside if last > first]
        lines.append(text[position:stop])
        position = after
    return lines, marks


def _range(start, stop):
    """difflib's unified range: ``start,length`` (``start`` alone for one line)."""
    length = stop - start
    if length == 1:
        return str(start + 1)
    return f'{start + 1 if length else start},{length}'


def _unified(old_lines, new_lines, old_name, new_name):
    """``difflib.unified_diff`` lines, each as (prefix, side, line index) or (text, None, None)."""
    started = False
    for group in difflib.SequenceMatcher(None, old_lines, new_lines).get_grouped_opcodes(3):
        if not started:
            started = True
            yield '--- ' + old_name, None, None
            yield '+++ ' + new_name, None, None
        first, last = group[0], group[-1]
        yield f'@@ -{_range(first[1], last[2])} +{_range(first[3], last[4])} @@', None, None
        for tag, old_start, old_stop, new_start, new_stop in group:
            if tag == 'equal':
                for index in range(old_start, old_stop):
                    yield ' ', 'old', index
                continue
            if tag in ('replace', 'delete'):
                for index in range(old_start, old_stop):
                    yield '-', 'old', index
            if tag in ('replace', 'insert'):
                for index in range(new_start, new_stop):
                    yield '+', 'new', index


def _raw_diff(old_doc, new_doc, old_name, new_name):
    old_lines, old_marks = _lines(*_dump_marked(old_doc))
    new_lines, new_marks = _lines(*_dump_marked(new_doc))
    if len(old_lines) + len(new_lines) > MAX_RAW_INPUT_LINES:
        return {'lines': None, 'reason': 'too_large', 'total_lines': len(old_lines) + len(new_lines)}
    sides = {'old': (old_lines, old_marks), 'new': (new_lines, new_marks)}
    shown, size, total, full = [], 0, 0, False
    for prefix, side, index in _unified(old_lines, new_lines, 'a/' + old_name, 'b/' + new_name):
        total += 1
        if full:
            continue
        if side:
            lines, marks = sides[side]
            line, spans = lines[index], marks.get(index)
            text = prefix + line
        else:
            text, spans = prefix, None
        length = len(text.encode()) + 1
        if len(shown) >= MAX_RAW_LINES or size + length > MAX_RAW_BYTES:
            full = True  # keep the shown part contiguous
            continue
        size += length
        if spans:
            out, position = _Text().add(prefix), 0
            for first, last in spans:
                out.add(line[position:first]).add(line[first:last], True)
                position = last
            shown.append(out.add(line[position:]).value())
        else:
            shown.append(text)
    return {'lines': shown, 'truncated': total > len(shown), 'omitted_lines': total - len(shown)}


def diff_config(old_bytes, new_bytes, *, old_name='config.yaml', new_name='config.yaml'):
    """Key-path comparison of two YAML main files, bounded, credentials marked."""
    try:
        old_doc, new_doc = _load_yaml(old_bytes), _load_yaml(new_bytes)
    except (UnicodeDecodeError, yaml.YAMLError, RecursionError):
        return {'format': 'yaml', 'status': 'unparsable', 'items': [], 'omitted': 0}
    budget = _Budget(MAX_CONFIG_ROWS)
    _walk(old_doc, new_doc, (), budget)
    result = {'format': 'yaml', 'status': 'compared' if budget.rows else 'formatting', 'items': budget.rows,
              'omitted': budget.omitted, 'total': len(budget.rows) + budget.omitted}
    result['raw'] = _raw_diff(old_doc, new_doc, old_name, new_name)
    return result


def _nodes(data):
    document = _load_yaml(data)
    proxies = document.get('proxies') if isinstance(document, dict) else None
    if not isinstance(proxies, list):
        raise ValueError('not a node list')
    nodes = {}
    for index, item in enumerate(proxies):
        name = item.get('name') if isinstance(item, dict) else None
        nodes[name if isinstance(name, str) else f'#{index}'] = _json(item)
    return nodes


def _node_summary(old, new):
    try:
        before = _nodes(old) if old is not None else {}
        after = _nodes(new) if new is not None else {}
    except (UnicodeDecodeError, yaml.YAMLError, ValueError, RecursionError, AttributeError):
        return _size_summary(old, new)
    return {'kind': 'nodes', 'before': None if old is None else len(before), 'after': None if new is None else len(after),
            'added': len(after.keys() - before.keys()), 'removed': len(before.keys() - after.keys()),
            'modified': sum(1 for name in after.keys() & before.keys() if after[name] != before[name])}


def rule_entries(data):
    """Rule lines of a text or YAML payload rule file, without comments or blanks."""
    text = data.decode('utf-8')
    lines = text.splitlines()
    head = [line.strip() for line in lines[:200] if line.strip() and not line.lstrip().startswith('#')]
    if head and head[0].startswith('payload:'):
        entries = []
        for line in lines:
            match = _YAML_ITEM.match(line)
            if match:
                value = match.group(1)
                if len(value) >= 2 and value[0] == value[-1] and value[0] in '\'"':
                    value = value[1:-1]
                entries.append(value)
        return entries
    return [stripped for line in lines if (stripped := line.strip()) and not stripped.startswith(('#', '//'))]


def _rule_summary(old, new):
    try:
        before = rule_entries(old) if old is not None else []
        after = rule_entries(new) if new is not None else []
    except UnicodeDecodeError:  # binary rule sets (mrs)
        return _size_summary(old, new)
    old_count, new_count = Counter(before), Counter(after)
    return {'kind': 'rules', 'before': None if old is None else len(before), 'after': None if new is None else len(after),
            'added': sum((new_count - old_count).values()), 'removed': sum((old_count - new_count).values())}


def _size_summary(old, new):
    return {'kind': 'size', 'before': None if old is None else len(old), 'after': None if new is None else len(new)}


def _read(archive, paths):
    """Selected members of a bundle, in one pass over the compressed stream."""
    wanted = {'files/' + path: path for path in paths}
    found = {}
    if not wanted:
        return found
    with tarfile.open(fileobj=io.BytesIO(archive), mode='r:gz') as tar:
        for member in tar:
            path = wanted.get(member.name)
            if path is not None and member.isfile():
                found[path] = tar.extractfile(member).read()
                if len(found) == len(wanted):
                    break
    return found


def _canonical(entry):
    return 'config.yaml' if entry['role'] == CONFIG_ROLE else entry['path']


def _comparison_digests(manifest, comparison_config=None):
    if comparison_config:
        manifest = {**manifest, 'files': [comparison_config if item['role'] == CONFIG_ROLE else item
                                          for item in manifest['files']]}
    return deployment_content_digests(manifest)


def _public_file(entry, status, summary=None):
    item = {'path': entry['path'], 'role': entry.get('role'), 'status': status}
    if entry.get('name'):
        item['name'] = _show(entry['name'], clip=False)
    if summary is not None:
        item['summary'] = summary
    return item


_ROLE_ORDER = {CONFIG_ROLE: 0, 'provider': 1, 'rule': 2, 'custom': 3, None: 4}
_STATUS_ORDER = {'modified': 0, 'added': 1, 'removed': 2, 'unchanged': 3}


def build_push_diff(agent, bundle, repository, *, receipt=_MISSING):
    """File and main-configuration changes from the applied receipt to ``bundle``.

    ``agent`` is the snapshot whose ``applied_content`` passed the content
    comparison's preconditions; ``bundle`` is the candidate it prepared.
    """
    from . import rule_sync_store
    applied = agent.get('applied_content') or {}
    if receipt is _MISSING:
        receipt = rule_sync_store.load(repository, agent, with_bundle=True)
    baseline_digests = applied.get('files') if isinstance(applied.get('files'), dict) else None
    if baseline_digests is None and receipt is not None:
        baseline_digests = _comparison_digests(receipt['manifest'], receipt.get('comparison_config'))
    if baseline_digests is None:
        return {'status': 'changed', 'files': None, 'reason': 'baseline_files_missing'}
    if receipt is None:
        content_reason = 'service_without_bundle' if agent.get('service_type') == 'mosdns' else 'bundle_missing'
    else:
        content_reason = None

    new_entries = {_canonical(item): item for item in bundle.manifest['files']}
    new_digests = _comparison_digests(bundle.manifest, bundle.comparison_config)
    old_entries = {_canonical(item): item for item in receipt['manifest']['files']} if receipt else {}
    statuses = {}
    for path in set(new_digests) | set(baseline_digests):
        if path not in baseline_digests:
            statuses[path] = 'added'
        elif path not in new_digests:
            statuses[path] = 'removed'
        else:
            statuses[path] = 'unchanged' if baseline_digests[path] == new_digests[path] else 'modified'
    def entry_for(path):
        return new_entries.get(path) or old_entries.get(path) or {
            'path': path, 'role': CONFIG_ROLE if path == 'config.yaml' else None}

    def order(path):
        entry = entry_for(path)
        return (_ROLE_ORDER.get(entry.get('role'), 4), _STATUS_ORDER.get(statuses[path], 3),
                entry.get('name') or entry['path'])

    changed = sorted((path for path, status in statuses.items() if status != 'unchanged'), key=order)
    listed = changed[:MAX_LISTED_FILES]  # only listed files are read and summarised
    new_bytes = _read(bundle.archive, [new_entries[path]['path'] for path in listed if path in new_entries])
    old_bytes = (_read(receipt['bundle'].archive, [old_entries[path]['path'] for path in listed if path in old_entries])
                 if receipt else {})

    files, config = [], None
    for path in listed:
        status = statuses[path]
        entry = entry_for(path)
        old = old_bytes.get(old_entries[path]['path']) if path in old_entries else None
        new = new_bytes.get(new_entries[path]['path']) if path in new_entries else None
        known_old = status == 'added' or old is not None
        role = entry.get('role')
        summary = None
        if not known_old and status == 'modified':
            summary = None
        elif role == CONFIG_ROLE:
            if old is not None and new is not None:
                config = diff_config(old, new, old_name=old_entries[path]['path'], new_name=new_entries[path]['path'])
                summary = {'kind': 'config', 'changes': config.get('total', 0),
                           'before': len(old), 'after': len(new)}
            else:
                summary = _size_summary(old, new)
        elif role == 'provider':
            summary = _node_summary(old, new)
        elif role == 'rule':
            summary = _rule_summary(old, new)
        elif status != 'removed' or old is not None:
            summary = _size_summary(old, new)
        files.append(_public_file(entry, status, summary))
    if config is None and statuses.get('config.yaml') == 'modified':
        config = {'format': 'yaml', 'status': 'unavailable', 'reason': content_reason or 'bundle_missing',
                  'items': [], 'omitted': 0}
    unchanged = sorted((path for path, status in statuses.items() if status == 'unchanged'), key=order)
    counts = Counter(statuses.values())
    return {
        'status': 'changed',
        'content': 'digests' if receipt is None else 'full',
        'content_reason': content_reason,
        'counts': {key: counts.get(key, 0) for key in ('added', 'removed', 'modified', 'unchanged')},
        'files': files,
        'files_omitted': len(changed) - len(listed),
        'unchanged': [_public_file(new_entries[path], 'unchanged') for path in unchanged[:MAX_LISTED_FILES]],
        'unchanged_omitted': max(0, len(unchanged) - MAX_LISTED_FILES),
        'config': config,
    }
