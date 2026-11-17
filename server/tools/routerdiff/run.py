"""Execute concrete recipes; report missing Go routes separately from defects."""
import argparse
import base64
from collections import Counter
from dataclasses import asdict
import json
from pathlib import Path
import re
import sys
import time

import cases
import guards
import workflows
import fixtures
from compare import differences, logical_snapshot, response_view, timestamp
from harness import Pair
from harness import decode_response
from inventory import design_json, load
from invariants import configuration_only, profile_write
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
    implemented = all((routes[ident].method, shape(routes[ident].path)) in available
                      for ident in (case.route, *case.requires))
    for name, side in pair.sides.items():
        if name == 'go' and not implemented:
            continue
        for control in case.prepare:
            if control.get('rpc') == 'begin':
                values = {key: value for key, value in control.items() if key != 'rpc'}
                ident = values['ident']
                side.pending[ident] = {'before': logical_snapshot(side.rpc('snapshot')), 'started': time.time()}
                values['headers'] = {'Authorization': 'Bearer ' + side.token, **values.get('headers', {})}
                values.setdefault('body', None)
                side.rpc('async', **values)
            else:
                side.rpc('fake', **control)
        before = side.pending[case.join]['before'] if case.join else logical_snapshot(side.rpc('snapshot'))
        own_request = {**request, 'document': cases.resolve(case.document, side.observations)}
        started = side.pending[case.join]['started'] if case.join else time.time()
        response = decode_response(side.rpc('join', ident=case.join)) if case.join else side.request(**own_request)
        if case.join:
            del side.pending[case.join]
        ended = time.time()
        response['method'] = case.method
        observations[name] = response_view(response)
        side.observations[case.ident] = observations[name]
        after = logical_snapshot(side.rpc('snapshot'))
        if after['fake']['errors']:
            raise RuntimeError(case.ident + ': fake contract error: ' + repr(after['fake']['errors']))
        snapshots[name] = {'before': before, 'after': after}
        if name == 'python':
            if response['status'] != case.expected:
                raise RuntimeError(f'{case.ident}: Python precondition expected {case.expected}, got '
                                   f'{response["status"]}: {response["content"]!r}')
            if case.message is not None and response['content'].get('message') != case.message:
                raise RuntimeError(case.ident + ': wrong Python guard reached: ' + repr(response['content']))
            for path in case.absent:
                try:
                    cases.at(response['content'], path)
                except KeyError:
                    pass
                else:
                    raise RuntimeError(case.ident + ': private/unknown field leaked at ' + path)
            if case.html_error:
                content_type = dict((key.lower(), value) for key, value in response['headers']).get('content-type', '')
                if response['kind'] != 'text' or not content_type.startswith('text/html'):
                    raise RuntimeError(case.ident + ': expected the actual uncaught HTML error boundary')
            for path, expected in case.checks.items():
                actual = cases.at(response['content'], path)
                if differences(actual, expected):
                    raise RuntimeError(case.ident + ': Python precondition at ' + path + ': ' + repr(actual))
            for path, pattern in case.formats.items():
                actual = cases.at(response['content'], path)
                if not isinstance(actual, str) or not re.fullmatch(pattern, actual):
                    raise RuntimeError(case.ident + ': invalid Python identity/hash at ' + path)
            for path in case.times:
                timestamp(cases.at(response['content'], path), started - 1, ended + 1)
            if case.effect in ('profile-write', 'profile-write-device'):
                profile_write(before, after, case.profile)
                if case.effect == 'profile-write':
                    configuration_only(before, after)
            if case.effect == 'device-only':
                if differences(before['database'], after['database']):
                    raise RuntimeError(case.ident + ': manual device operation modified configuration storage')
            if case.effect == 'device-read':
                if differences(before['database'], after['database']):
                    raise RuntimeError(case.ident + ': device read modified configuration storage')
                configuration_only(before, after)
            for peer, table, identity_key, identity in case.foreign:
                def objects(snapshot):
                    return [row for row in snapshot['fake'][peer][table] if row.get(identity_key) == identity]
                if not objects(before) or differences(objects(before), objects(after)):
                    raise RuntimeError(case.ident + ': foreign device object changed or disappeared')
            if case.unchanged:
                changed = differences(before, after)
                if changed:
                    raise RuntimeError(case.ident + ': Python supposedly rejected/read-only request changed state: '
                                       + repr([asdict(item) for item in changed[:5]]))
    found = []
    if implemented:
        if case.html_error and observations['go']['status'] == 500 and observations['go']['kind'] == 'text':
            # X03 explicitly excludes implementation-specific HTML prose only
            # for these individually named Python uncaught-exception cases.
            for observation in observations.values():
                observation['body'] = '<HTML 500 body excluded by reviewed X03>'
        found += differences(observations['python'], observations['go'], '/http')
        found += differences(snapshots['python']['after'], snapshots['go']['after'], '/state')
        if case.unchanged:
            found += differences(snapshots['go']['before'], snapshots['go']['after'], '/go-unexpected-write')
    # Auth values are synthetic but reports don't need bearer credentials.
    request['document'] = cases.resolve(case.document, pair.python.observations)
    request['headers'] = {key: '<synthetic credential>' if key.lower() == 'authorization' else value
                          for key, value in request['headers'].items()}
    request['raw'] = request['raw'].decode(errors='replace') if request['raw'] is not None else None
    return {'case': case.ident, 'route': case.route, 'entries': list(case.entries), 'request': request,
            'status': 'different' if found else 'compared' if implemented else 'go-unimplemented',
            'python': observations['python'], 'go': observations.get('go'),
            'differences': [asdict(item) for item in found]}


def report(output, results, available, announce=True, error=None):
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
        'complete': False, 'remaining_audits': len(entries),
        'ledger': ledger, 'results': results}
    if error:
        data['fixture_error'] = error
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix(output.suffix + '.tmp')
    temporary.write_text(json.dumps(data, ensure_ascii=False, indent=2) + '\n')
    temporary.replace(output)
    if announce:
        print(json.dumps({key: value for key, value in data.items() if key not in ('ledger', 'results')}, ensure_ascii=False))
    return data


def findings(path, data):
    lines = ['# Routers Step B 黑盒差分发现', '',
        '状态：进行中。矩阵审计尚未完成；不能据此宣称 routers 已通过验收。', '',
        f'矩阵：{data["matrix_routes"]} 条路由、{data["matrix_entries"]} 个设计条目。',
        f'公开 Go 入口实际注册本领域路由：{data["implemented_routes"]} 条。',
        '执行配方统计：`' + json.dumps(data['recipe_counts'], ensure_ascii=False, sort_keys=True) + '`。',
        f'已有执行配方引用的设计条目：{data["referenced_entries"]}；逐条变体审计仍未完成。', '',
        '“Go 尚未实现”单独计数，不算失败，也不算差分通过。路由层 R15/R17 未注册时，',
        '即使底层 R01–R06 已移植，也不能声称 HTTP 行为已比较。', '',
        '仅使用合成数据；两端独立 DATA_DIR、网络命名空间和设备 fake；未访问生产或真实设备。',
        '比较器会检查状态、语义响应头、类型/键/值/数组顺序、完整逻辑数据库、文件与设备状态。',
        'Python 前置条件失败单列为夹具错误，不算 Go 发现；实际比较为零时不能下“无缺陷”结论。', '']
    changed = [result for result in data['results'] if result['differences']]
    if not changed:
        lines += ['目前没有可提交给移植者的已比较差异。已实现路由为零，故尚无产品缺陷结论。', '']
    for index, result in enumerate(changed, 1):
        severity = 'P1' if result['request']['method'] != 'GET' else 'P2'
        owner = 'R17（客户端 HTTP）' if result['route'] in ('R002', 'R003', 'R004', 'R005', 'R006') else 'R15（路由 HTTP）'
        lines += [f'## F{index:03} · {severity} · {result["case"]}', '',
                  '- 用例编号：' + ', '.join(result['entries']),
                  '- 建议负责模块：' + owner + '；设备传输差异另交 R04/R05，发布与同步交 R12/R14。',
                  '- 请求：`' + json.dumps(result['request'], ensure_ascii=False) + '`',
                  '- Python：`' + json.dumps(result['python'], ensure_ascii=False) + '`',
                  '- Go：`' + json.dumps(result['go'], ensure_ascii=False) + '`', '',
                  '| 路径 | Python | Go | 类别 |', '|---|---|---|---|']
        for difference in result['differences']:
            escape = lambda value: json.dumps(value, ensure_ascii=False).replace('|', '&#124;').replace('\n', '<br>')
            lines.append('| ' + ' | '.join(escape(difference[key]) for key in ('path', 'python', 'go', 'category')) + ' |')
        lines.append('')
    lines += ['后续仍须补齐：其余 H 公开可达性与复合 guard 变体、设备失败/补偿、完整生成形态、',
              'Agent/备份/代理、后台轮询、断连/重启、动态值内容绑定和 O12 分类。详见进度文件与执行 ledger。', '']
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text('\n'.join(lines))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--keep', type=Path)
    parser.add_argument('--findings', type=Path)
    parser.add_argument('--allow-incomplete', action='store_true',
                        help='iteration only: allow pending coverage audits; default gate exits 2')
    args = parser.parse_args()
    routes, _ = load()
    results = []
    options = {'alpha': {'routers': {'items': [router(), router('r-ik', 'ikuai')]}},
               'beta': {'routers': {'items': [router()]}}}
    worlds = [('base', options, (cases.authentication, cases.profile_selection_failures, cases.body_guards,
                      guards.router_settings, guards.client_draft, guards.fixed_ip_shape,
                      guards.route_edit_shape, guards.workspace_shape, guards.rule_sync_shape,
                      workflows.client_drafts, workflows.workspace_drafts, workflows.router_updates,
                      workflows.runtime_reads, workflows.sync_settings, workflows.manual_routes, workflows.job_locks)),
              ('stored', fixtures.stored_options(), (fixtures.receipts, fixtures.stored_guards))]
    available = None
    try:
        for name, options, groups in worlds:
            keep = args.keep / name if args.keep else None
            with Pair(args.binary, options, keep) as pair:
                current = registered(pair.go.rpc('manifest'))
                if available is not None and current != available:
                    raise RuntimeError('public route inventory changed between synthetic worlds')
                available = current
                for group in groups:
                    started = time.monotonic()
                    for case in group():
                        results.append(execute(pair, case, available, routes))
                    report(args.output, results, available, announce=False)
                    print(name + '/' + group.__name__ + ': ' + str(len(results)) + ' cumulative recipes, '
                          + format(time.monotonic() - started, '.2f') + 's', flush=True)
    except Exception as error:
        report(args.output, results, available or set(), error=str(error))
        raise
    data = report(args.output, results, available)
    if args.findings:
        findings(args.findings, data)
    if data['recipe_counts'].get('different'):
        return 1
    return 0 if args.allow_incomplete or data['complete'] else 2


if __name__ == '__main__':
    sys.exit(main())
