"""Run config matrix groups against real, independently seeded public servers."""
import argparse
from collections import Counter
import importlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import traceback

from build import build
from harness import ROOT, Recorder, DesignError
from inventory import routes, cases


def render_findings(results):
    lines = ['# Config Step B 差异发现', '',
             '仅合成数据；来源为公开 Flask↔Go HTTP、SQLite 行及文件比较。未修改业务代码。', '',
             'Go 尚未实现单独计数，不列为业务失败。以下待协调人派移植 agent，不能用基线吞掉。', '']
    for r in results:
        if r['state'] not in ('different', 'O12-separate'):
            continue
        lines += [f'## {r["case"]} / {r["variant"]}'.rstrip(), '', f'路由：`{r["route"]}`；状态：{r["state"]}', '',
                  '| 路径 | 类型 | Python | Go |', '|---|---|---|---|']
        def display(v):
            text = repr(v)
            return text[:650].replace('|', '\\|').replace('\n', '<br>')
        lines += [f'| `{d["path"]}` | {d["kind"]} | {display(d["python"])} | {display(d["go"])} |'
                  for d in r['differences']]
        lines.append('')
    if not any(r['state'] in ('different', 'O12-separate') for r in results):
        lines += ['当前已执行用例未发现差异；不代表尚未实现或未运行的条目通过。', '']
    if any(r.get('group') == 'discovery-mixed-order' for r in results):
        lines += ['## Oracle 限制：混合目标采纳的顺序', '',
                  '路由：`POST /api/domain-discovery/apply`；用例：R046-S03；类型：Python 集合遍历顺序不稳定。', '',
                  '未固定 hash seed 的首轮合成请求中，Python added.target 为 `[proxy, direct]`，Go 为 `[direct, proxy]`，'
                  '同一批 history 顺序也不同；每个目标的内容一致。Python 来源为 '
                  '`backend/utils/domain_discovery_service.py:567` 的 target 集合遍历。', '',
                  '当前独立用例固定 `PYTHONHASHSEED=0`，保留原始 HTTP 数组与数据库 history 顺序比较；'
                  '该环境下实际比较通过。其他 Python hash 环境下的顺序不作通过保证，留作部分覆盖/审查项，'
                  '没有对数组排序以吞掉差异。普通多次写入之间等待下一秒，避免秒级时间戳产生不同的排序平局。', '']
    return '\n'.join(lines)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--groups', default='all')
    parser.add_argument('--binary', type=Path)
    parser.add_argument('--output', type=Path, default=ROOT / 'server/tools/configdiff/testdata/results.json')
    parser.add_argument('--findings', type=Path, default=ROOT / 'server/parity/difftest-config-findings.md')
    args = parser.parse_args()
    # Self-check uses the exact comparison functions that report real differences.
    subprocess.run([sys.executable, '-m', 'unittest', '-v', 'test_compare'],
                   cwd=Path(__file__).parent, check=True)
    matrix = routes()
    from cases.crosscutting import GROUPS as crosscutting
    groups = dict(crosscutting)
    from cases.auth import GROUPS as auth
    groups.update(auth)
    from cases.redaction import GROUPS as redaction
    groups.update(redaction)
    from cases.admin_auth import GROUPS as admin_auth
    groups.update(admin_auth)
    from cases.settings import GROUPS as settings
    groups.update(settings)
    from cases.webdav import GROUPS as webdav
    groups.update(webdav)
    from cases.mosdns import GROUPS as mosdns
    groups.update(mosdns)
    from cases.nodes import GROUPS as nodes
    groups.update(nodes)
    from cases.probes import GROUPS as probes
    groups.update(probes)
    from cases.artifact import GROUPS as artifact
    groups.update(artifact)
    from cases.scheduler import GROUPS as scheduler
    groups.update(scheduler)
    from cases.http_shell import GROUPS as http_shell
    groups.update(http_shell)
    from cases.profiles import GROUPS as profiles
    groups.update(profiles)
    from cases.profile_guards import GROUPS as profile_guards
    groups.update(profile_guards)
    from cases.logs import GROUPS as logs
    groups.update(logs)
    from cases.notifications import GROUPS as notifications
    groups.update(notifications)
    from cases.stats import GROUPS as stats
    groups.update(stats)
    from cases.json_boundary import GROUPS as json_boundary
    groups.update(json_boundary)
    from cases.generate import GROUPS as generate
    groups.update(generate)
    from cases.pulls import GROUPS as pulls
    groups.update(pulls)
    from cases.oxidns import GROUPS as oxidns
    groups.update(oxidns)
    from cases.config_backup import GROUPS as config_backup
    groups.update(config_backup)
    from cases.custom_config import GROUPS as custom_config
    groups.update(custom_config)
    from cases.mihomo_guards import GROUPS as mihomo_guards
    groups.update(mihomo_guards)
    from cases.client_import import GROUPS as client_import
    groups.update(client_import)
    from cases.subscriptions import GROUPS as subscriptions
    groups.update(subscriptions)
    from cases.subscription_fetch import GROUPS as subscription_fetch
    groups.update(subscription_fetch)
    from cases.subscription_providers import GROUPS as subscription_providers
    groups.update(subscription_providers)
    from cases.provider_lifecycle import GROUPS as provider_lifecycle
    groups.update(provider_lifecycle)
    from cases.merged_provider import GROUPS as merged_provider
    groups.update(merged_provider)
    from cases.tasks import GROUPS as tasks
    groups.update(tasks)
    from cases.groups import GROUPS as managed_groups
    groups.update(managed_groups)
    from cases.rule_library import GROUPS as rule_library
    groups.update(rule_library)
    from cases.rules import GROUPS as profile_rules
    groups.update(profile_rules)
    from cases.rule_dns import GROUPS as rule_dns
    groups.update(rule_dns)
    from cases.compose import GROUPS as compose
    groups.update(compose)
    from cases.rule_io import GROUPS as rule_io
    groups.update(rule_io)
    from cases.generator_shapes import GROUPS as generator_shapes
    groups.update(generator_shapes)
    from cases.generator_rules import GROUPS as generator_rules
    groups.update(generator_rules)
    from cases.rule_syntax import GROUPS as rule_syntax
    groups.update(rule_syntax)
    from cases.ruleset_shapes import GROUPS as ruleset_shapes
    groups.update(ruleset_shapes)
    from cases.generator_dns import GROUPS as generator_dns
    groups.update(generator_dns)
    from cases.generator_urls import GROUPS as generator_urls
    groups.update(generator_urls)
    from cases.backup_shapes import GROUPS as backup_shapes
    groups.update(backup_shapes)
    from cases.subscription_hosts import GROUPS as subscription_hosts
    groups.update(subscription_hosts)
    from cases.oxidns_controls import GROUPS as oxidns_controls
    groups.update(oxidns_controls)
    from cases.oxidns_assets import GROUPS as oxidns_assets
    groups.update(oxidns_assets)
    from cases.oxidns_editor import GROUPS as oxidns_editor
    groups.update(oxidns_editor)
    from cases.aggregations import GROUPS as aggregations
    groups.update(aggregations)
    from cases.aggregation_providers import GROUPS as aggregation_providers
    groups.update(aggregation_providers)
    from cases.domain_discovery import GROUPS as domain_discovery
    groups.update(domain_discovery)
    from cases.discovery_writes import GROUPS as discovery_writes
    groups.update(discovery_writes)
    requested = list(groups) if args.groups == 'all' else args.groups.split(',')
    unknown = set(requested) - groups.keys()
    if unknown:
        parser.error('unknown groups: ' + ','.join(sorted(unknown)))
    recorder, errors = Recorder(), []
    started = time.time()
    with tempfile.TemporaryDirectory(prefix='configdiff-build-') as temporary:
        binary = args.binary or build(ROOT, Path(temporary))
        from mutation import verify_harness
        verify_harness(binary)
        for group in requested:
            print('GROUP ' + group, flush=True)
            first_result = len(recorder.results)
            try:
                groups[group](binary, recorder)
            except Exception as error:
                errors.append({'group': group, 'error': str(error), 'type': type(error).__name__})
                traceback.print_exc()
            for result in recorder.results[first_result:]:
                result['group'] = group
    matrix_cases = cases()
    for r in recorder.results:
        if r['case'].startswith('R') and matrix_cases.get(r['case']) != r['route']:
            errors.append({'group': 'inventory', 'error': 'case ID does not name its matrix route: ' + r['case']})
        # G16 needs the Agent's authenticated public core-version report to
        # distinguish old from unknown cores. This is a protocol prerequisite,
        # not a new config-domain route or a catch-all for misspelled routes.
        auxiliary = r['case'] == 'G16' and r['route'] == 'POST /api/agents/<agent_id>/traffic-report'
        if r['route'] != 'startup' and r['route'] not in matrix and not auxiliary:
            errors.append({'group': 'inventory', 'error': 'case names unknown route: ' + r['route']})
    previous = json.loads(args.output.read_text()).get('results', []) if args.output.exists() else []
    # Rerunning one group replaces its earlier verdicts, preserving other batches.
    replacement_keys = {(r['case'], r['variant'], r['route']) for r in recorder.results}
    previous = [r for r in previous if r.get('group') and r['group'] not in requested]
    combined = previous + recorder.results
    keys = [(r['case'], r['variant'], r['route']) for r in combined]
    if len(set(keys)) != len(keys):
        errors.append({'group': 'inventory', 'error': 'duplicate case/variant/route key'})
    combined.sort(key=lambda r: (r['route'], r['case'], r['variant']))
    current_counts = Counter(r['state'] for r in recorder.results)
    summary = {'groups': requested, 'current': dict(current_counts),
               'end_to_end_mutation_check': True,
               'cumulative': dict(Counter(r['state'] for r in combined)),
               'categories': dict(Counter(r['category'] for r in combined)),
               'named_matrix_cases': len({r['case'] for r in combined if r['case'].startswith('R')}),
               'named_matrix_categories': dict(Counter(case.split('-')[1][0] for case in {r['case'] for r in combined if r['case'].startswith('R')})),
               'named_auxiliary_categories': dict(Counter('H' if c.startswith('H') else 'G' if c.startswith('G') else c
                    for c in {r['case'] for r in combined if not r['case'].startswith('R')})),
               'request_variants': len(combined), 'design_errors': errors,
               'seconds': round(time.time() - started, 2)}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text('{"summary":' + json.dumps(summary, ensure_ascii=False, separators=(',', ':')) +
                           ',\n"results":[\n' +
                           ',\n'.join(json.dumps(r, ensure_ascii=False, separators=(',', ':')) for r in combined) + '\n]}\n')
    args.findings.write_text(render_findings(combined))
    print('SUMMARY ' + json.dumps(summary, ensure_ascii=False), flush=True)
    return 2 if errors else 1 if current_counts['different'] else 0


if __name__ == '__main__':
    raise SystemExit(main())
