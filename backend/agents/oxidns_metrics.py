"""Normalize the bounded, read-only OxiDNS Prometheus metrics snapshot.

OxiDNS 1.6.1 exports unprefixed names; prefixed aliases are accepted without
counting duplicate series twice. Missing or invalid observations stay unknown.
Counters describe the current plugin lifetime, not a fixed historical window.
"""
import math
import re

MAX_INPUT_BYTES = 2 * 1024 * 1024
MAX_SAMPLES = 10_000
MAX_LINE_LENGTH = 16 * 1024
MAX_LABELS = 32

_SERVER_FIELDS = {
    'server_request_total': 'requests',
    'server_failed_total': 'failed',
    'server_inflight': 'inflight',
    'server_latency_sum_ms': 'latency_sum_ms',
    'server_latency_count': 'latency_count',
}
_CACHE_FIELDS = {
    'cache_lookup_total': 'lookups',
    'cache_miss_total': 'misses',
    'cache_expired_total': 'expired',
    'cache_entry_count': 'entries',
}
_UPSTREAM_FIELDS = {
    'forward_upstream_query_total': 'requests',
    'forward_upstream_error_total': 'failed',
    'forward_upstream_latency_sum_ms': 'latency_sum_ms',
    'forward_upstream_latency_count': 'latency_count',
}
_FAMILIES = set(_SERVER_FIELDS) | set(_CACHE_FIELDS) | set(_UPSTREAM_FIELDS) | {'cache_hit_total'}
_SAMPLE = re.compile(
    r'(?P<name>[a-zA-Z_:][a-zA-Z0-9_:]*)'
    r'(?:\{(?P<labels>.*)\})?[ \t]+'
    r'(?P<value>\S+)(?:[ \t]+[+-]?\d+)?[ \t]*'
)
_LABEL = re.compile(r'([a-zA-Z_][a-zA-Z0-9_]*)[ \t]*=[ \t]*"((?:[^"\\]|\\["\\n])*)"')
_ESCAPE = re.compile(r'\\(["\\n])')
_NUMBER = re.compile(r'[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?')


def _labels(text):
    if text is None:
        return {}
    labels = {}
    position = 0
    while position < len(text):
        while position < len(text) and text[position] in ' \t':
            position += 1
        if position == len(text):
            break
        match = _LABEL.match(text, position)
        if not match:
            return None
        name, value = match.groups()
        if name in labels or len(labels) >= MAX_LABELS:
            return None
        labels[name] = _ESCAPE.sub(lambda m: '\n' if m[1] == 'n' else m[1], value)
        position = match.end()
        while position < len(text) and text[position] in ' \t':
            position += 1
        if position == len(text):
            break
        if text[position] != ',':
            return None
        position += 1
    return labels


def _number(text):
    if not _NUMBER.fullmatch(text):
        return None
    try:
        value = float(text)
        if not math.isfinite(value) or value < 0:
            return None
        # Preserve integer counters exactly instead of rounding through float.
        return int(text) if re.fullmatch(r'[+-]?\d+', text) else value
    except (ValueError, OverflowError):
        return None


def _sum(values):
    values = list(values)
    if not values or any(value is None for value in values):
        return None
    try:
        result = sum(values)
        return result if math.isfinite(result) else None
    except OverflowError:
        return None


def _divide(numerator, denominator, *, percentage=False):
    if numerator is None or denominator is None or denominator == 0:
        return None
    # Contradictory snapshots must not become a hit rate above 100%.
    if percentage and numerator > denominator:
        return None
    try:
        result = numerator / denominator
        if percentage:
            result *= 100
        return result if math.isfinite(result) else None
    except OverflowError:
        return None


def _observe(group, field, value):
    # Both aliases, or duplicate samples, represent one observation. Conflicts
    # remain unknown, including a valid value paired with a nonfinite sample.
    if field in group and group[field] != value:
        group[field] = None
    else:
        group[field] = value


def parse_metrics(text):
    """Return JSON-safe raw observations; reject oversized input with ValueError.

    Malformed samples are ignored. Recognizable nonfinite/negative values stay
    None, as do missing series. Aggregates require a known value for every
    observed listener/cache, so incomplete snapshots cannot imply zero traffic.
    """
    if not isinstance(text, str):
        raise ValueError('OxiDNS metrics must be text')
    if len(text) > MAX_INPUT_BYTES:
        raise ValueError('OxiDNS metrics exceed size limit')
    try:
        if len(text.encode('utf-8')) > MAX_INPUT_BYTES:
            raise ValueError('OxiDNS metrics exceed size limit')
    except UnicodeEncodeError:
        raise ValueError('OxiDNS metrics contain invalid text') from None

    server_values, cache_values, upstream_values = {}, {}, {}
    sample_count = 0
    for raw in text.splitlines():
        if len(raw) > MAX_LINE_LENGTH:
            raise ValueError('OxiDNS metrics line exceeds size limit')
        line = raw.strip()
        if not line or line.startswith('#'):
            continue
        sample_count += 1
        if sample_count > MAX_SAMPLES:
            raise ValueError('OxiDNS metrics exceed sample limit')
        match = _SAMPLE.fullmatch(line)
        if not match:
            continue
        family = match['name']
        if family.startswith('oxidns_'):
            family = family[len('oxidns_'):]
        if family not in _FAMILIES:
            continue
        labels = _labels(match['labels'])
        if not labels or not labels.get('plugin_tag'):
            continue
        tag = labels['plugin_tag']
        value = _number(match['value'])
        if family in _SERVER_FIELDS:
            protocol = labels.get('protocol')
            if not protocol:
                continue
            group = server_values.setdefault((tag, protocol), {})
            _observe(group, _SERVER_FIELDS[family], value)
        elif family in _UPSTREAM_FIELDS:
            upstream = labels.get('upstream')
            if not upstream:
                continue
            group = upstream_values.setdefault((tag, upstream), {})
            _observe(group, _UPSTREAM_FIELDS[family], value)
        elif family == 'cache_hit_total':
            kind = labels.get('kind')
            if kind not in ('fresh', 'stale'):
                continue
            group = cache_values.setdefault(tag, {})
            _observe(group, kind + '_hits', value)
        else:
            group = cache_values.setdefault(tag, {})
            _observe(group, _CACHE_FIELDS[family], value)

    return {
        'servers': [dict(tag=tag, protocol=protocol, **{
            field: values.get(field) for field in _SERVER_FIELDS.values()})
            for (tag, protocol), values in sorted(server_values.items())],
        'caches': [dict(tag=tag, **{field: values.get(field) for field in (
            'lookups', 'fresh_hits', 'stale_hits', 'misses', 'expired', 'entries')})
            for tag, values in sorted(cache_values.items())],
        'upstreams': [dict(tag=tag, upstream=upstream, **{
            field: values.get(field) for field in _UPSTREAM_FIELDS.values()})
            for (tag, upstream), values in sorted(upstream_values.items())],
    }


# Gauges (inflight and entries) describe current state and must not be reset.
_COUNTERS = {
    'servers': ('requests', 'failed', 'latency_sum_ms', 'latency_count'),
    'caches': ('lookups', 'fresh_hits', 'stale_hits', 'misses', 'expired'),
    'upstreams': ('requests', 'failed', 'latency_sum_ms', 'latency_count'),
}
_IDENTITIES = {'servers': ('tag', 'protocol'), 'caches': ('tag',),
               'upstreams': ('tag', 'upstream')}


def metrics_counters_restarted(current, baseline):
    """A proven counter drop requires a new durable statistics generation.

    Merely clipping a negative delta for one response fails when the restarted
    counter later catches up with the old baseline. Missing series alone do not
    prove a restart and remain unknown instead.
    """
    for kind, fields in _COUNTERS.items():
        identity = lambda row: tuple(row.get(key) for key in _IDENTITIES[kind])
        previous = {identity(row): row for row in baseline.get(kind, [])}
        for row in current.get(kind, []):
            old = previous.get(identity(row))
            if old is not None and any(row.get(field) is not None and old.get(field) is not None
                                       and row[field] < old[field] for field in fields):
                return True
    return False


def apply_metrics_baseline(current, baseline):
    """Subtract one persisted reset snapshot without mutating either input.

    Missing observations stay unknown. A decreased counter means this series
    restarted; its counters then describe its new lifetime, with a partial flag.
    The caller handles whole-core/configuration changes as a new generation.
    """
    adjusted, partial = {}, False
    for kind, fields in _COUNTERS.items():
        identity = lambda row: tuple(row.get(key) for key in _IDENTITIES[kind])
        previous = {identity(row): row for row in baseline.get(kind, [])}
        adjusted[kind] = []
        for row in current.get(kind, []):
            old = previous.get(identity(row))
            output = dict(row)
            if old is None:
                # A missing baseline does not prove that a series was created
                # after reset (it might have been absent in a partial scrape).
                partial = True
                output.update(dict.fromkeys(fields))
            elif any(row.get(field) is not None and old.get(field) is not None
                     and row[field] < old[field] for field in fields):
                partial = True
            else:
                for field in fields:
                    value, before = row.get(field), old.get(field)
                    output[field] = value - before if value is not None and before is not None else None
                    if output[field] is None:
                        partial = True
            adjusted[kind].append(output)
        if set(previous) - {identity(row) for row in current.get(kind, [])}:
            partial = True
    return adjusted, partial


def zero_baseline(snapshot):
    """New plugin lifetimes start at zero, including newly added series."""
    return {kind: [{**row, **dict.fromkeys(fields, 0)} for row in snapshot[kind]]
            for kind, fields in _COUNTERS.items()}


def add_observations(current, carried):
    """Carry counters across a publication, never gauges or averaged ratios.

    Retired series still contribute their historical counters. Their live
    gauges are zero; missing values in a present series remain unknown.
    """
    result = {}
    for kind, fields in _COUNTERS.items():
        identity = lambda row: tuple(row[key] for key in _IDENTITIES[kind])
        live = {identity(row): row for row in current[kind]}
        old = {identity(row): row for row in carried[kind]}
        rows = []
        for key in sorted(live.keys() | old.keys()):
            row = dict(live[key] if key in live else old[key])
            if key in live and key in old:
                row.update({field: _sum((live[key].get(field), old[key].get(field))) for field in fields})
            if key not in live:
                for gauge in ('inflight', 'entries'):
                    if gauge in row:
                        row[gauge] = 0
            rows.append(row)
        result[kind] = rows
    return result


def summarize_observations(snapshot):
    """Compute weighted dashboard ratios from raw or reset-adjusted counters."""
    servers = []
    for values in snapshot['servers']:
        servers.append({
            **{field: values.get(field) for field in ('tag', 'protocol', 'requests', 'failed', 'inflight')},
            'avg_latency_ms': _divide(values.get('latency_sum_ms'), values.get('latency_count')),
        })
    caches = []
    for values in snapshot['caches']:
        hits = _sum((values.get('fresh_hits'), values.get('stale_hits')))
        caches.append({
            **{field: values.get(field) for field in (
                'tag', 'lookups', 'fresh_hits', 'stale_hits', 'misses', 'expired', 'entries')},
            'hits': hits,
            'hit_rate': _divide(hits, values.get('lookups'), percentage=True),
        })
    cache_hit_rate = _divide(
        _sum(row['hits'] for row in caches),
        _sum(row['lookups'] for row in caches), percentage=True)
    if any(row['hits'] is not None and row['lookups'] is not None
           and row['hits'] > row['lookups'] for row in caches):
        cache_hit_rate = None
    upstreams = [{
        **{field: values.get(field) for field in ('tag', 'upstream', 'requests', 'failed')},
        'avg_latency_ms': _divide(values.get('latency_sum_ms'), values.get('latency_count')),
    } for values in snapshot.get('upstreams', [])]
    # Unknown counts sort last; stable identifiers resolve ties without hiding
    # idle upstreams. Latency is completed attempts, including failed attempts.
    upstreams.sort(key=lambda row: (row['requests'] is None, -(row['requests'] or 0), row['tag'], row['upstream']))
    return {
        'metrics': {
            'requests_total': _sum(row['requests'] for row in servers),
            'failed_total': _sum(row['failed'] for row in servers),
            'avg_latency_ms': _divide(
                _sum(row.get('latency_sum_ms') for row in snapshot['servers']),
                _sum(row.get('latency_count') for row in snapshot['servers'])),
            'inflight': _sum(row['inflight'] for row in servers),
            'cache_hit_rate': cache_hit_rate,
        },
        'caches': caches,
        'servers': servers,
        'upstreams': upstreams,
    }


def summarize_metrics(text):
    return summarize_observations(parse_metrics(text))
