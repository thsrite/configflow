"""Account for every R/H recipe without calling partial coverage a pass."""
import argparse
from collections import Counter, defaultdict
import json
from pathlib import Path
import re

from inventory import MATRIX, cases, routes


def dispositions(text):
    block = text.split('<!-- CONFIGDIFF_PARTIAL_BEGIN -->', 1)[1].split('<!-- CONFIGDIFF_PARTIAL_END -->', 1)[0]
    result = {}
    for line in block.splitlines():
        if not line.startswith('| '):
            continue
        columns = [part.strip() for part in line.strip('|').split('|')]
        if not re.match(r'^[RH]\d', columns[0]):
            continue
        if len(columns) != 3 or not columns[1] or not columns[2]:
            raise ValueError('partial disposition needs IDs, kind and an explicit reason')
        for token in columns[0].split(','):
            token = token.strip()
            span = re.fullmatch(r'H(\d+)-H(\d+)', token)
            labels = [f'H{n:03}' for n in range(int(span[1]), int(span[2]) + 1)] if span else [token]
            for label in labels:
                if label in result:
                    raise ValueError('duplicate partial disposition: ' + label)
                result[label] = {'kind': columns[1], 'reason': columns[2]}
    return result


def evaluate(expected, results, declared):
    observed = {row['case'] for row in results} | {label for row in results for label in row.get('helper_cases', [])}
    missing = set(expected) - observed
    unknown = (set(declared) | {label for label in observed if re.fullmatch(r'R\d+-[SFB]\d+|H\d+', label)}) - set(expected)
    return {'exercised_reference_recipes': len(set(expected) & observed),
        'not_exercised': {label: {**expected[label], **declared[label]} for label in sorted(missing & declared.keys())},
        'unaccounted': sorted(missing - declared.keys()), 'unknown_labels': sorted(unknown)}


def audit(report, text):
    expected = {label: {'route': route} for label, route in cases().items()}
    sources = dict(re.findall(r'^### (K\d+) \[[^\]]+\]\(([^)#]+)', text, re.M))
    route_ids = {value: key for key, value in routes().items()}
    for label, rid, source, line in re.findall(r'^\| (H\d+) /[^|]+\| (R\d+) \| (K\d+):(\d+) \|', text, re.M):
        expected[label] = {'route': route_ids[rid], 'source': sources[source] + '#L' + line}
    value = evaluate(expected, report['results'], dispositions(text))
    covered = defaultdict(set)
    for row in report['results']:
        if row['case'] in expected and row['case'].startswith('R'):
            covered[row['route']].add(row['case'].split('-')[1][0])
    value.update(routes=len(routes()), request_variants=len(report['results']),
        raw_verdicts=dict(Counter(row['state'] for row in report['results'])),
        routes_missing_source_success_or_failure=sorted(route for route in routes() if not {'S', 'F'} <= covered[route]),
        runtime_design_errors=report['summary']['design_errors'],
        statement='Not-exercised recipes are explicit boundaries, never compared/passed. Go skips remain skips.')
    return value


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--report', type=Path, action='append')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    reports = args.report or [Path(__file__).parent / 'testdata/results.json']
    rows, errors = {}, []
    for path in reports:
        data = json.loads(path.read_text())
        for row in data['results']:
            rows[row['case'], row['variant'], row['route']] = row
        errors.extend(data['summary']['design_errors'])
    value = audit({'results': list(rows.values()), 'summary': {'design_errors': errors}}, MATRIX.read_text())
    value['source_reports'] = [str(path) for path in reports]
    args.output.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')
    print(json.dumps({key: value[key] for key in ('routes', 'request_variants', 'exercised_reference_recipes',
        'unaccounted', 'unknown_labels', 'routes_missing_source_success_or_failure', 'runtime_design_errors')}, ensure_ascii=False))
    return int(bool(value['unaccounted'] or value['unknown_labels']
        or value['routes_missing_source_success_or_failure'] or value['runtime_design_errors']))


if __name__ == '__main__':
    raise SystemExit(main())
