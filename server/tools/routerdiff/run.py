"""Execute concrete recipes; report missing Go routes separately from defects."""
import argparse
from collections import Counter
from dataclasses import asdict
import json
from pathlib import Path
import re
import sys
import time

import cases
from compare import differences, logical_snapshot, response_view
from harness import Pair
from inventory import design_json, load
from seed import router


def shape(path):
    return re.sub(r'<(?:[^:>]+:)?[^>]+>|\{[^}]+\}', '{}', path)


def registered(manifest):
    return {(method, shape(path)) for method, path in
            (line.split('\t', 1) for line in manifest.splitlines())}


def execute(pair, case, available, routes):
    request = dict(method=case.method, path=case.path, document=case.document, raw=case.raw,
                   headers=case.headers, auth=case.auth)
    observations, snapshots = {}, {}
    implemented = (routes[case.route].method, shape(routes[case.route].path)) in available
    for name, side in pair.sides.items():
        if name == 'go' and not implemented:
            continue
        before = logical_snapshot(side.rpc('snapshot'))
        response = side.request(**request)
        response['method'] = case.method
        observations[name] = response_view(response)
        after = logical_snapshot(side.rpc('snapshot'))
        if after['fake']['errors']:
            raise RuntimeError(case.ident + ': fake contract error: ' + repr(after['fake']['errors']))
        snapshots[name] = {'before': before, 'after': after}
        if name == 'python':
            if response['status'] != case.expected:
                raise RuntimeError(f'{case.ident}: Python precondition expected {case.expected}, got '
                                   f'{response["status"]}: {response["content"]!r}')
            if case.unchanged:
                changed = differences(before, after)
                if changed:
                    raise RuntimeError(case.ident + ': Python supposedly rejected/read-only request changed state: '
                                       + repr([asdict(item) for item in changed[:5]]))
    found = []
    if implemented:
        found += differences(observations['python'], observations['go'], '/http')
        found += differences(snapshots['python']['after'], snapshots['go']['after'], '/state')
        if case.unchanged:
            found += differences(snapshots['go']['before'], snapshots['go']['after'], '/go-unexpected-write')
    # Auth values are synthetic but reports don't need bearer credentials.
    request['headers'] = {key: '<synthetic credential>' if key.lower() == 'authorization' else value
                          for key, value in request['headers'].items()}
    request['raw'] = request['raw'].decode(errors='replace') if request['raw'] is not None else None
    return {'case': case.ident, 'route': case.route, 'entries': list(case.entries), 'request': request,
            'status': 'different' if found else 'compared' if implemented else 'go-unimplemented',
            'python': observations['python'], 'go': observations.get('go'),
            'differences': [asdict(item) for item in found]}


def report(output, results, available):
    routes, entries = load()
    referenced = {ident for result in results for ident in result['entries']}
    unknown = referenced - entries.keys()
    if unknown:
        raise ValueError('recipes reference nonexistent design IDs: ' + repr(sorted(unknown)))
    counts = Counter(result['status'] for result in results)
    # Pending remains visible. A design entry may require further variants even
    # after a concrete recipe references it; completion is a separate audit.
    ledger = {ident: {'kind': entry.kind, 'source_line': entry.source_line,
                     'recipes': [item['case'] for item in results if ident in item['entries']],
                     'audit': 'variants-in-progress' if ident in referenced else 'pending'}
              for ident, entry in entries.items()}
    data = {'matrix_entries': len(entries), 'matrix_routes': len(routes),
        'implemented_routes': sum((route.method, shape(route.path)) in available for route in routes.values()),
        'recipe_counts': dict(counts), 'referenced_entries': len(referenced),
        'ledger': ledger, 'results': results}
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps({key: value for key, value in data.items() if key not in ('ledger', 'results')}, ensure_ascii=False))
    return data


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--keep', type=Path)
    args = parser.parse_args()
    routes, _ = load()
    results = []
    options = {'alpha': {'routers': {'items': [router(), router('r-ik', 'ikuai')]}},
               'beta': {'routers': {'items': [router()]}}}
    with Pair(args.binary, options, args.keep) as pair:
        available = registered(pair.go.rpc('manifest'))
        for group in (cases.authentication, cases.profile_selection_failures, cases.body_guards):
            started = time.monotonic()
            for case in group():
                results.append(execute(pair, case, available, routes))
            print(group.__name__ + ': ' + str(len(results)) + ' cumulative recipes, '
                  + format(time.monotonic() - started, '.2f') + 's', flush=True)
        data = report(args.output, results, available)
    return 1 if data['recipe_counts'].get('different') else 0


if __name__ == '__main__':
    sys.exit(main())
