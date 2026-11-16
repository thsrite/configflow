"""Generator inventory, isolated worker orchestration, and four mandatory mutations."""
import collections
import copy
import json
import os
from pathlib import Path
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
    'router-script': 'No public Go router plan/script exporter exists.',
    'aggregation-provider': 'Go aggregation-provider hook still has its missing implementation.',
    'agent-deployment-bundle': 'No public Go deployment-bundle/final-device-render generator exists.',
}


def inventory(doc, implemented):
    jobs, coverage = [], {}
    profiles, agents = doc['profiles'], doc['system'].get('agents', [])
    default_profile = doc['system'].get('system_config', {}).get('default_profile_id') or 'default'
    for kind, (selector, output_format, route) in INVENTORY.items():
        mode = 'http' if route and tuple(route.split(' ', 1)) in implemented else 'generator'
        row = coverage['GENERATOR ' + kind] = {'mode': mode, 'cases': 0, 'success': 0, 'both-error': 0}
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
    for kind, reason in MISSING.items():
        coverage['GENERATOR ' + kind] = {'mode': 'Go 尚未实现', 'cases': missing_counts[kind], 'reason': reason}
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


def self_check(real_status, samples, compare):
    checks = {}
    broken = (real_status[0] + 1, real_status[1], real_status[2])
    checks['status'] = ('status', 'value') in compare(real_status, broken, '/self-check', [])
    # Deep JSON traversal passes through the same production deep comparator.
    good = (200, 'application/json', b'{"data":{"profiles":[{"config":{"dns":{"upstreams":[{"port":53}]}}}]}}')
    bad = (200, good[1], good[2].replace(b'53', b'54'))
    checks['json-leaf'] = ('body.data.profiles[0].config.dns.upstreams[0].port', 'value') in compare(good, bad, '/self-check', [])
    import yaml
    if 'yaml' not in samples or 'text' not in samples:
        raise RuntimeError('self-check requires actual successful YAML and text generator outputs')
    original = yaml.safe_load(samples['yaml'][2])
    changed = copy.deepcopy(original)
    def mutate(value):
        if isinstance(value, dict):
            for key, child in value.items():
                if isinstance(child, (dict, list)):
                    if mutate(child):
                        return True
                elif child is not None:
                    value[key] = not child if isinstance(child, bool) else child + 1 if isinstance(child, (int, float)) else str(child) + '-self-check'
                    return True
        elif isinstance(value, list):
            for index, child in enumerate(value):
                if isinstance(child, (dict, list)):
                    if mutate(child):
                        return True
                else:
                    value[index] = 'self-check-mutated'
                    return True
        return False
    if not mutate(changed):
        raise RuntimeError('no YAML leaf available for self-check')
    modified_yaml = (200, 'text/yaml', yaml.safe_dump(changed, allow_unicode=True).encode())
    checks['yaml-leaf'] = bool(compare(samples['yaml'], modified_yaml, '/self-check', []))
    lines = samples['text'][2].decode().splitlines()
    if not lines:
        raise RuntimeError('no text line available for self-check')
    lines[len(lines) // 2] += ' # self-check-mutated'
    modified_text = (200, 'text/plain', ('\n'.join(lines) + '\n').encode())
    checks['text-line'] = bool(compare(samples['text'], modified_text, '/self-check', []))
    if not all(checks.values()):
        raise RuntimeError('differential mutation self-check failed')
    return {name: 'PASS' for name in checks}
