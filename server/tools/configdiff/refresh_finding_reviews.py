"""Render resolution provenance from recorded runs; never execute/merge cases."""
import argparse
from collections import Counter
import json
from pathlib import Path

from config_finding_resolutions import annotate
from harness import ROOT
from run import render_findings
from finding_reviews import reviews


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--report', type=Path, default=ROOT / 'server/tools/configdiff/testdata/results.json')
    parser.add_argument('--findings', type=Path, default=ROOT / 'server/parity/difftest-config-findings.md')
    args = parser.parse_args()
    report = json.loads(args.report.read_text())
    current = [item for item in report['results'] if item.get('group') in report['summary']['groups']]
    reviewed = reviews([], current, report.get('finding_reviews', []), report['summary'].get('baseline', ''))
    report['finding_reviews'] = reviewed
    accepted = {(item['case'], item['variant'], item['route']) for item in reviewed if item['status'] == 'accepted'}
    accepted_current = sum(item['state'] == 'different' and (item['case'], item['variant'], item['route']) in accepted for item in current)
    report['summary']['current_accepted_differences'] = accepted_current
    report['summary']['current_open_differences'] = sum(item['state'] == 'different' for item in current) - accepted_current
    args.report.write_text('{"summary":' + json.dumps(report['summary'], ensure_ascii=False, separators=(',', ':'))
        + ',\n"finding_reviews":' + json.dumps(reviewed, ensure_ascii=False, separators=(',', ':'))
        + ',\n"results":[\n' + ',\n'.join(json.dumps(item, ensure_ascii=False, separators=(',', ':'))
            for item in report['results']) + '\n]}\n')
    args.findings.write_text(render_findings(report['results'], reviewed))
    print(json.dumps({'recorded_rechecks': dict(Counter(item['status'] for item in reviewed)),
                      'actual_case_results_unchanged': len(report['results'])}, ensure_ascii=False))


if __name__ == '__main__':
    main()
