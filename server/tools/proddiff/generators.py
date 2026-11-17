"""Generator inventory, isolated worker orchestration, and four mandatory mutations."""
import copy
import json
import os
import subprocess
import sys

# Static names/routes only. Opaque snapshot selectors exist only in private jobs.
INVENTORY = {
    'mihomo': ('profile', 'yaml', 'GET /api/config/<profile_id>/mihomo'),
    'surge': ('profile', 'text', 'GET /api/config/<profile_id>/surge'),
    'loon': ('profile', 'text', 'GET /api/config/<profile_id>/loon'),
    'mosdns': ('profile', 'yaml', 'GET /api/config/<profile_id>/mosdns'),
    'oxidns': ('profile', 'yaml', 'GET /api/config/<profile_id>/oxidns'),
    'mosdns-files': ('profile', 'json', None),
    'mosdns-downloads': ('profile', 'json', None),
    'oxidns-files': ('profile', 'json', None),
    'oxidns-downloads': ('profile', 'json', None),
    'mihomo-providers': ('profile', 'json', None),
    'mihomo-rules': ('profile', 'json', None),
    'subscription-mihomo': ('subscription', 'yaml', 'GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies'),
    'subscription-surge': ('subscription', 'text', 'GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies'),
    'subscription-loon': ('subscription', 'text', 'GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies'),
    'agent-mihomo': ('agent', 'yaml', 'GET /api/agents/<agent_id>/config'),
    'agent-mosdns': ('agent', 'yaml', 'GET /api/agents/<agent_id>/config'),
    'agent-oxidns': ('agent', 'yaml', 'GET /api/agents/<agent_id>/config'),
    'agent-surge': ('agent', 'text', 'GET /api/agents/<agent_id>/config'),
}
MISSING = {
    'router-script': ('POST /api/routers/<router_id>/plan', 'No public Go router plan/script exporter exists.'),
    'aggregation-provider': ('GET /api/aggregations/<agg_id>/provider', 'Go aggregation-provider hook still has its missing implementation.'),
    'agent-deployment-bundle': ('POST /api/agents/<agent_id>/generate-config', 'No public Go deployment-bundle/final-device-render generator exists.'),
}


def inventory(doc, implemented):
    jobs, coverage = [], {}
    profiles, agents = doc['profiles'], doc['system'].get('agents', [])
    default_profile = doc['system'].get('system_config', {}).get('default_profile_id') or 'default'
    for kind, (selector, output_format, route) in INVENTORY.items():
        mode = 'http' if route and tuple(route.split(' ', 1)) in implemented else 'generator'
        row = coverage['GENERATOR ' + kind] = {'mode': mode, 'route': route, 'cases': 0, 'success': 0, 'both-error': 0}
        for profile_id, profile in profiles.items():
            config = dict(doc['shared'], **profile)
            if selector == 'profile':
                entities = [{}]
            elif selector == 'subscription':
                entities = config.get('subscriptions', [])
            else:
                entities = [a for a in agents if (a.get('profile_id') or default_profile) == profile_id
                            and 'agent-' + a.get('service_type', 'mihomo') == kind]
            for entity in entities:
                row['cases'] += 1
                if mode == 'generator':
                    jobs.append({'index': len(jobs), 'kind': kind, 'profile_id': profile_id,
                                 'sub_id': entity.get('id', '') if selector == 'subscription' else '',
                                 'agent_id': entity.get('id', '') if selector == 'agent' else '',
                                 'format': output_format})
    missing_counts = {
        'router-script': sum(len(profile.get('routers', {}).get('items', [])) for profile in profiles.values()),
        'aggregation-provider': sum(len(dict(doc['shared'], **profile).get('aggregations', [])) for profile in profiles.values()),
        'agent-deployment-bundle': len(agents),
    }
    for kind, (route, reason) in MISSING.items():
        mode = 'http' if tuple(route.split(' ', 1)) in implemented else 'Go 尚未实现'
        coverage['GENERATOR ' + kind] = {'mode': mode, 'route': route, 'cases': missing_counts[kind], 'success': 0, 'reason': reason}
    return jobs, coverage


def run_workers(snapshot, copies_parent, report, binary, doc, implemented, compare):
    jobs, coverage = inventory(doc, implemented)
    manifest = report / 'generator-jobs.json'
    manifest.write_text(json.dumps(jobs))
    outputs = []
    import shutil
    for label in ('python', 'go'):
        data = copies_parent / ('generator-' + label)
        shutil.copytree(snapshot, data)
        output = report / ('generator-' + label + '.jsonl')
        env = dict(os.environ, DATA_DIR=str(data), LOG_FILE=str(report / ('generator-' + label + '.log')),
                   LOG_LEVEL='ERROR', SUB_STORE_BUILTIN_DIR='/nonexistent',
                   CONFIGFLOW_BACKEND_HOST='127.0.0.1', CONFIGFLOW_BACKEND_PORT='5001')
        command = [sys.executable, 'server/tools/proddiff/python_generators.py'] if label == 'python' else [str(binary)]
        with (report / ('generator-' + label + '.log')).open('wb') as log:
            subprocess.run(command + [str(manifest), str(output)], env=env, stdout=log, stderr=log,
                           check=True, timeout=600)
        results = [json.loads(line) for line in output.read_text().splitlines()]
        if len(results) != len(jobs) or [r['index'] for r in results] != list(range(len(jobs))):
            raise RuntimeError('generator worker result coverage mismatch')
        outputs.append(results)
    found = set()
    samples = {}
    for job, left, right in zip(jobs, *outputs):
        key = 'GENERATOR ' + job['kind']
        row = coverage[key]
        row['compared'] = row.get('compared', 0) + 1
        if left['status'] != 'ok' or right['status'] != 'ok':
            row['python-error'] = row.get('python-error', 0) + (left['status'] != 'ok')
            row['go-error'] = row.get('go-error', 0) + (right['status'] != 'ok')
            if left['status'] == right['status']:
                row['both-error'] += 1
                # Rejection agreement is separate from successful output coverage.
                if left['error'] != right['error']:
                    found.add((key, 'error.message', 'value'))
            else:
                found.add((key, 'status', 'generation-error-go' if right['status'] != 'ok' else 'generation-error-python'))
            continue
        row['success'] += 1
        mime = {'yaml': 'text/yaml', 'json': 'application/json', 'text': 'text/plain'}[job['format']]
        a, b = (200, mime, left['content'].encode()), (200, mime, right['content'].encode())
        for field, kind in compare(a, b, '/generator/' + job['kind'], []):
            found.add((key, field, kind))
        samples.setdefault(job['format'], b)
    return found, coverage, samples


def successful_output_regressions(expected, current):
    """Keep a real output from silently becoming an agreed rejection."""
    return {key + ' successful-output-count' for key, previous in expected.items()
            if current.get(key, {}).get('success', 0) < previous.get('success', 0)}


def _walk_values(value, keys=()):
    """Yield (keys, value) for every node, depth first; keys is a tuple of dict keys/list indexes."""
    yield keys, value
    children = value.items() if isinstance(value, dict) else enumerate(value) if isinstance(value, list) else ()
    for key, child in children:
        yield from _walk_values(child, keys + (key,))


def _locate(document, keys):
    for key in keys:
        document = document[key]
    return document


def self_check(real_status, samples, compare, safe_key=str, safe_header=str):
    """Mutate real Go outputs and require the production comparator to report each exact change."""
    import yaml
    checks = {}
    for required in ('yaml', 'text', 'headers', 'http-json'):
        if required not in samples:
            raise RuntimeError('self-check requires actual successful Go outputs: ' + required)

    def detects(name, left, right, expected):
        checks[name] = expected in compare(left, right, '/self-check', [])

    def agrees(name, left, right):
        checks[name] = compare(left, right, '/self-check', []) == []

    def shown(document, keys):
        # The comparator prints schema-safe paths; build the expected path the same way.
        path = 'body'
        for key in keys:
            path += f'[{key}]' if isinstance(document, list) else '.' + safe_key(key)
            document = document[key]
        return path

    def kind(old, new):
        return 'value' if type(old) is type(new) else 'type'

    def bump(value):
        if isinstance(value, bool):
            return not value
        if isinstance(value, (int, float)):
            return value + 1
        return str(value) + '-self-check' if value is not None else 0

    # HTTP status and headers of a real Go reply.
    agrees('control-real-reply', real_status, real_status)
    detects('status', real_status, (real_status[0] + 1,) + tuple(real_status[1:]), ('status', 'value'))
    detects('content-type', real_status, (real_status[0], 'text/plain') + tuple(real_status[2:]), ('Content-Type', 'value'))
    detects('transport-both', (0, '', b'', {}), (0, '', b'', {}), ('status', 'transport-both'))
    reply = samples['headers']
    name = sorted(set(reply[3]) - {'date', 'server', 'content-length', 'connection', 'keep-alive', 'transfer-encoding', 'content-type'})[0]
    changed = dict(reply[3], **{name: reply[3][name] + '-self-check'})
    detects('header-value', reply, reply[:3] + (changed,), ('header.' + safe_header(name), 'value'))
    dropped = {key: value for key, value in reply[3].items() if key != name}
    detects('header-missing', reply, reply[:3] + (dropped,), ('header.' + safe_header(name), 'missing-in-go'))

    # Deep JSON traversal: synthetic seven-level control document.
    good = (200, 'application/json', b'{"data":{"profiles":[{"config":{"dns":{"upstreams":[{"port":53}]}}}]}}')
    port = 'body.data.profiles[0].config.dns.upstreams[0].port'
    detects('json-leaf', good, (200, good[1], good[2].replace(b'53', b'54')), (port, 'value'))
    detects('json-int-float', good, (200, good[1], good[2].replace(b'53', b'53.0')), (port, 'int-float'))
    detects('json-int-string', good, (200, good[1], good[2].replace(b'53', b'"53"')), (port, 'type'))
    detects('json-duplicate-key', good, (200, good[1], good[2].replace(b'{"port":53}', b'{"port":53,"port":53}')), ('body', 'parse-go'))
    rules = (200, 'application/json', b'{"rules":["DOMAIN,a.example,DIRECT","MATCH,PROXY"]}')
    detects('json-list-order', rules, (200, rules[1], b'{"rules":["MATCH,PROXY","DOMAIN,a.example,DIRECT"]}'), ('body.rules[0]', 'value'))

    # Real JSON reply: re-encoding is no difference; each structural mutation is reported at its path.
    sample = samples['http-json'][:3]
    original = json.loads(sample[2])
    encode = lambda value: (200, sample[1], json.dumps(value, ensure_ascii=False, indent=1, sort_keys=True).encode())
    agrees('control-json-reencoded', sample, encode(original))
    nodes = list(_walk_values(original))
    leaves = [(keys, value) for keys, value in nodes if keys and not isinstance(value, (dict, list))]
    objects = [(keys, value) for keys, value in nodes if isinstance(value, dict) and value]
    lists = [(keys, value) for keys, value in nodes if isinstance(value, list) and len(value) >= 2 and value[0] != value[1]]
    if not leaves or not objects:
        raise RuntimeError('real JSON self-check sample has no leaf or object')
    keys, old = leaves[-1]
    mutated = copy.deepcopy(original)
    _locate(mutated, keys[:-1])[keys[-1]] = bump(old)
    detects('json-real-leaf', sample, encode(mutated), (shown(original, keys), kind(old, bump(old))))
    keys, obj = objects[-1]
    first = next(iter(obj))
    mutated = copy.deepcopy(original)
    del _locate(mutated, keys)[first]
    detects('json-missing-key', sample, encode(mutated), (shown(original, keys + (first,)), 'missing-in-go'))
    mutated = copy.deepcopy(original)
    _locate(mutated, keys)['self_check_extra'] = ''
    detects('json-extra-key', sample, encode(mutated), (shown(original, keys) + '.' + safe_key('self_check_extra'), 'extra-in-go'))
    mutated = copy.deepcopy(original)
    replacement = None if obj[first] is not None else ''
    _locate(mutated, keys)[first] = replacement
    detects('json-null-vs-value', sample, encode(mutated), (shown(original, keys + (first,)), 'type'))
    if lists:
        keys, items = lists[0]
        mutated = copy.deepcopy(original)
        moved = _locate(mutated, keys)
        moved[0], moved[1] = moved[1], moved[0]
        found = compare(sample, encode(mutated), '/self-check', [])
        prefix = shown(original, keys) + '['
        checks['json-real-list-order'] = bool(found) and all(path.startswith(prefix) for path, _ in found)

    # Real YAML: a re-dump is no difference, a leaf change is reported at its path, duplicates fail.
    loaded = yaml.safe_load(samples['yaml'][2])
    if not isinstance(loaded, dict) or not loaded:
        raise RuntimeError('YAML self-check needs a top-level mapping')
    agrees('control-yaml-redumped', samples['yaml'][:3], (200, 'text/yaml', yaml.safe_dump(loaded, allow_unicode=True, sort_keys=True).encode()))
    leaves = [(keys, value) for keys, value in _walk_values(loaded) if keys and value is not None and not isinstance(value, (dict, list))]
    if not leaves:
        raise RuntimeError('no YAML leaf available for self-check')
    keys, old = leaves[0]
    changed = copy.deepcopy(loaded)
    _locate(changed, keys[:-1])[keys[-1]] = bump(old)
    detects('yaml-leaf', samples['yaml'][:3], (200, 'text/yaml', yaml.safe_dump(changed, allow_unicode=True).encode()), (shown(loaded, keys), kind(old, bump(old))))
    duplicate = samples['yaml'][2].rstrip(b'\n') + b'\n' + yaml.safe_dump({next(iter(loaded)): None}).encode()
    detects('yaml-duplicate-key', samples['yaml'][:3], (200, 'text/yaml', duplicate), ('body', 'parse-go'))

    # Real text: one changed line, one inserted line and CRLF line endings are all reported.
    lines = samples['text'][2].decode().splitlines()
    if not lines:
        raise RuntimeError('no text line available for self-check')
    middle = len(lines) // 2
    edited = list(lines)
    edited[middle] += ' # self-check-mutated'
    detects('text-line', samples['text'][:3], (200, 'text/plain', ('\n'.join(edited) + '\n').encode()), (f'body[{middle}]', 'value'))
    inserted = lines[:middle] + ['# self-check-inserted'] + lines[middle:]
    detects('text-insert', samples['text'][:3], (200, 'text/plain', ('\n'.join(inserted) + '\n').encode()), ('body', 'length'))
    detects('text-line-endings', samples['text'][:3], (200, 'text/plain', ('\r\n'.join(lines) + '\r\n').encode()), ('body', 'line-endings'))
    if not all(checks.values()):
        raise RuntimeError('differential mutation self-check failed: ' + ','.join(sorted(k for k, v in checks.items() if not v)))
    return {name: 'PASS' for name in checks}
