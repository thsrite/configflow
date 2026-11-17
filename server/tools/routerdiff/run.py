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
import probes
import releases
import clients
import sync
import upstream
import dns
import gateway
import disconnect
import routing
import policy_view
from compare import differences, logical_snapshot, response_view, timestamp
from harness import Pair
from harness import decode_response
from inventory import design_json, load
from invariants import configuration_only, profile_write, fields
from seed import router, AGENT_TOKEN, CONFIG_TOKEN
from fakes import World
from addresses import report_aliases
from tokens import inspect_python_token
from seed import JWT_SECRET
from bugs import BUGS


def shape(path):
    return re.sub(r'<(?:[^:>]+:)?[^>]+>|\{[^}]+\}', '{}', path)


def auth_off(options):
    return {**options, 'environment': {**options.get('environment', {}), 'ADMIN_USERNAME': '', 'ADMIN_PASSWORD': ''}}


def registered(manifest):
    return {(method, shape(path)) for method, path in
            (line.split('\t', 1) for line in manifest.splitlines())}


def poll(side, values, expected, seconds, strict=True):
    if values['method'] != 'GET':
        raise ValueError('eventual assertions may only repeat public GETs')
    deadline = time.monotonic() + seconds
    while True:
        response = side.request(**values)
        try:
            ready = all(cases.at(response['content'], path) == value for path, value in expected.items())
        except (KeyError, IndexError, TypeError):
            ready = False
        if response['status'] == 200 and ready:
            return response
        if time.monotonic() >= deadline:
            if strict:
                raise RuntimeError('public steady-state wait expired: ' + repr(response['content']))
            return response  # A Go failure is a compared observation, not a fixture error.
        time.sleep(.1)


def wait_guard(side, path, seconds, strict=True):
    # Retry only the deliberate non-object rejection while L1 is busy. Never
    # retry an accepted write or any other status; all storage remains compared.
    deadline = time.monotonic() + seconds
    while True:
        response = side.request(method='PUT', path=path, document=[])
        if response['status'] != 409 or time.monotonic() >= deadline:
            if strict and response['status'] != 400:
                raise RuntimeError('reference public write guard did not settle to validation refusal')
            return response
        time.sleep(.1)


def execute(pair, case, available, routes):
    if not 0 <= case.pause_before <= 605:
        raise ValueError('real-time boundary wait exceeds reviewed fixture limit')
    if case.pause_before:
        cases.resolve(case.document, pair.python.observations)  # Fail missing prerequisites before a long wait.
        deadline = time.monotonic() + case.pause_before
        while (remaining := deadline - time.monotonic()) > 0:
            time.sleep(min(20, remaining))
            print(case.ident + ': real-time ageing ' + str(round(case.pause_before - max(0, deadline - time.monotonic())))
                  + '/' + str(case.pause_before) + 's', flush=True)
    request = dict(method=case.method, path=case.path, document=case.document, raw=case.raw,
                   headers=case.headers, auth=case.auth)
    observations, snapshots, flow_requests, flow_checks = {}, {}, {}, {}
    missing_routes = [ident for ident in (case.route, *case.requires)
                      if (routes[ident].method, shape(routes[ident].path)) not in available]
    implemented = not missing_routes and not pair.go.missing_effects
    blocked_reference = None
    for name, side in pair.sides.items():
        if name == 'go' and not implemented:
            continue
        try:
            preparation = cases.resolve(list(case.prepare), side.observations)
        except (KeyError, IndexError, TypeError) as error:
            if name != 'go':
                raise
            blocked_reference = 'Go preparation lacks a required own-side HTTP reference: ' + str(error)
            break
        before_preparation = logical_snapshot(side.rpc('snapshot')) if case.capture_before_prepare else None
        flow_requests[name] = []
        flow_checks[name] = []
        for control in preparation:
            if control.get('rpc') == 'restart':
                side.rpc('stop', kill=control.get('kill', False))
                side.start()  # Same production entry and fresh public login.
            elif control.get('rpc') == 'http':
                values = {key: value for key, value in control.items() if key not in ('rpc', 'expected')}
                reply = side.request(**values)
                if reply['status'] != control['expected'] or reply['content'].get('success') is not True:
                    raise RuntimeError(case.ident + ': public HTTP fixture setup failed on ' + name)
            elif control.get('rpc') == 'open_request':
                values = {key: value for key, value in control.items() if key not in ('rpc', 'document')}
                document = control.get('document')
                flow_requests[name].append({'method': values['method'], 'path': values['path'], 'document': document})
                values['headers'] = {'Content-Type': 'application/json', **values.get('headers', {})}
                if side.token:
                    values['headers'].setdefault('Authorization', 'Bearer ' + side.token)
                values['body'] = base64.b64encode(json.dumps(document, ensure_ascii=False, separators=(',', ':')).encode()).decode() \
                                 if document is not None else None
                side.rpc('open_request', **values)
            elif control.get('rpc') == 'abandon':
                side.rpc('abandon', ident=control['ident'])
            elif control.get('rpc') == 'assert_http':
                values = {key: value for key, value in control.items() if key not in ('rpc', 'expected')}
                reply = side.request(**values)
                flow_checks[name].append({'request': values, 'response': response_view(reply)})
                if name == 'python' and reply['status'] != control['expected']:
                    raise RuntimeError(case.ident + ': reference flow guard reached the wrong HTTP status')
            elif control.get('rpc') == 'wait_guard':
                reply = wait_guard(side, control['path'], control.get('seconds', 30), strict=name == 'python')
                flow_checks[name].append({'request': {'method': 'PUT', 'path': control['path'], 'document': []},
                                          'response': response_view(reply)})
            elif control.get('rpc') == 'begin':
                values = {key: value for key, value in control.items() if key != 'rpc'}
                ident = values['ident']
                side.pending[ident] = {'before': logical_snapshot(side.rpc('snapshot')), 'started': time.time()}
                values['headers'] = {'Authorization': 'Bearer ' + side.token, **values.get('headers', {})}
                values.setdefault('body', None)
                side.rpc('async', **values)
            else:
                side.rpc('fake', **control)
        before = (before_preparation if before_preparation is not None else
                  side.pending[case.join]['before'] if case.join else logical_snapshot(side.rpc('snapshot')))
        try:
            own_request = {**request, 'document': cases.resolve(case.document, side.observations),
                           'path': cases.resolve(case.path, side.observations)}
        except (KeyError, IndexError, TypeError) as error:
            if name != 'go':
                raise
            blocked_reference = 'Go previous HTTP response lacks a required own-side reference: ' + str(error)
            break
        started = side.pending[case.join]['started'] if case.join else time.time()
        log_position = side.rpc('log_position') if name == 'python' and case.console_patterns else None
        try:
            response = (decode_response(side.rpc('join', ident=case.join)) if case.join else
                        poll(side, own_request, case.wait_until, case.wait_seconds, strict=name == 'python') if case.wait_until else
                        side.request(**own_request))
        finally:
            for control in case.finalize:
                side.rpc('fake', **control)
        settled = [response_view(poll(side, {'method': 'GET', 'path': control['path']}, control['expect'],
                                     control.get('seconds', 30), strict=name == 'python')) for control in case.after_poll]
        if case.join:
            del side.pending[case.join]
        ended = time.time()
        response['method'] = case.method
        observations[name] = response_view(response)
        if settled:
            observations[name]['settled_reads'] = settled
        if flow_checks[name]:
            observations[name]['flow_checks'] = flow_checks[name]
        side.observations[case.ident] = observations[name]
        after = logical_snapshot(side.rpc('snapshot'))
        if after['fake']['errors']:
            raise RuntimeError(case.ident + ': fake contract error: ' + repr(after['fake']['errors']))
        snapshots[name] = {'before': before, 'after': after}
        if name == 'python':
            if response['status'] != case.expected:
                raise RuntimeError(f'{case.ident}: Python precondition expected {case.expected}, got '
                                   f'{response["status"]}: {response["content"]!r}')
            if case.console_patterns:
                output = side.rpc('logs', start=log_position, limit=8192)
                if not all(re.search(pattern, output) for pattern in case.console_patterns):
                    raise RuntimeError(case.ident + ': current reference exception does not match its expected source guard')
            actual_headers = {key.lower(): value for key, value in response['headers']}
            for key, expected in case.header_checks.items():
                if actual_headers.get(key.lower()) != expected:
                    raise RuntimeError(case.ident + ': Python header precondition failed: ' + key)
            if case.allow_methods:
                actual = observations[name]['headers'].get('allow')
                expected = [', '.join(sorted(case.allow_methods))]
                if actual != expected:
                    raise RuntimeError(case.ident + ': Python Allow method set differs: ' + repr(actual))
            if case.method == 'HEAD' and response['content'] != '':
                raise RuntimeError(case.ident + ': HEAD unexpectedly returned a response body')
            if case.message is not None and response['content'].get('message') != case.message:
                raise RuntimeError(case.ident + ': wrong Python guard reached: ' + repr(response['content']))
            if case.route == 'R006' and case.method == 'POST' and response['status'] == 200:
                inspect_python_token(response['content']['token'], own_request['document']['profile_id'],
                                     side.options.get('environment', {}).get('JWT_SECRET_KEY', JWT_SECRET))
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
                if differences(actual, cases.resolve(expected, side.observations)):
                    raise RuntimeError(case.ident + ': Python precondition at ' + path + ': ' + repr(actual))
            for path, pattern in case.formats.items():
                actual = cases.at(response['content'], path)
                if not isinstance(actual, str) or not re.fullmatch(pattern, actual):
                    raise RuntimeError(case.ident + ': invalid Python identity/hash at ' + path)
            for path in case.times:
                timestamp(cases.at(response['content'], path), started - 1, ended + 1)
            if ended - started < case.elapsed_min:
                raise RuntimeError(case.ident + ': expected real transport wait did not occur')
            transcript = after['fake']['transcript'][len(before['fake']['transcript']):]
            for requirement in case.expected_requests:
                count = sum(World.matches(item, requirement['match']) for item in transcript)
                if count != requirement['count']:
                    raise RuntimeError(case.ident + ': outbound protocol count mismatch: ' + repr(requirement['match']))
            cursor = 0
            for match in case.request_order:
                while cursor < len(transcript) and not World.matches(transcript[cursor], match):
                    cursor += 1
                if cursor == len(transcript):
                    raise RuntimeError(case.ident + ': outbound transaction order mismatch: ' + repr(match))
                cursor += 1
            if case.effect in ('profile-write', 'profile-write-device', 'router-create', 'router-delete'):
                profile_write(before, after, case.profile)
                if case.effect != 'profile-write-device':
                    configuration_only(before, after)
            for path, expected in case.stored.items():
                actual = cases.at(fields(after, 'profile', case.profile), path)
                if differences(actual, cases.resolve(expected, side.observations)):
                    raise RuntimeError(case.ident + ': stored precondition failed at ' + path + ': ' + repr(actual))
            for path in case.stored_absent:
                try:
                    cases.at(fields(after, 'profile', case.profile), path)
                except KeyError:
                    pass
                else:
                    raise RuntimeError(case.ident + ': ignored input unexpectedly persisted at ' + path)
            for path, expected in case.fake_checks.items():
                if differences(cases.at(after['fake'], path), expected):
                    raise RuntimeError(case.ident + ': fake device postcondition failed at ' + path)
            for path, expected in case.fake_lengths.items():
                if len(cases.at(after['fake'], path)) != expected:
                    raise RuntimeError(case.ident + ': fake device length postcondition failed at ' + path)
            for pattern, expected in case.file_contents.items():
                matching = [value for path, value in after['files'].items() if re.fullmatch(pattern, path)]
                if len(matching) != 1 or base64.b64decode(matching[0]) != expected.encode('utf-8'):
                    raise RuntimeError(case.ident + ': generated/cache file content postcondition failed: ' + pattern)
            if case.effect == 'router-create':
                old = fields(before, 'profile', case.profile)['routers']['items']
                new = fields(after, 'profile', case.profile)['routers']['items']
                if len(new) != len(old) + 1 or differences(new[:-1], old) or new[-1]['id'] != response['content']['router']['id']:
                    raise RuntimeError(case.ident + ': create did not append exactly the returned router')
                if len({row['id'] for row in new}) != len(new):
                    raise RuntimeError(case.ident + ': create generated duplicate router identity')
            if case.effect == 'router-delete':
                old = fields(before, 'profile', case.profile)['routers']['items']
                new = fields(after, 'profile', case.profile)['routers']['items']
                removed = own_request['path'].split('?')[0].rsplit('/', 1)[-1]
                if len(old) != len(new) + 1 or differences([row for row in old if row['id'] != removed], new):
                    raise RuntimeError(case.ident + ': delete did not remove exactly the selected record')
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
            for peer, table, criteria in case.preserved:
                def selected(snapshot):
                    return [row for row in snapshot['fake'][peer][table] if World.matches(row, criteria)]
                if not selected(before) or differences(selected(before), selected(after)):
                    raise RuntimeError(case.ident + ': expected retained device objects changed or disappeared')
            if case.unchanged:
                changed = differences(before, after)
                if changed:
                    raise RuntimeError(case.ident + ': Python supposedly rejected/read-only request changed state: '
                                       + repr([asdict(item) for item in changed[:5]]))
    found = []
    go_bug_outcome = None
    if implemented and not blocked_reference:
        if case.html_error and observations['go']['status'] == 500 and observations['go']['kind'] == 'text':
            # X03 explicitly excludes implementation-specific HTML prose only
            # for these individually named Python uncaught-exception cases.
            for observation in observations.values():
                observation['body'] = '<HTML 500 body excluded by reviewed X03>'
        found += differences(observations['python'], observations['go'], '/http')
        found += differences(snapshots['python']['after'], snapshots['go']['after'], '/state')
        if case.unchanged:
            found += differences(snapshots['go']['before'], snapshots['go']['after'], '/go-unexpected-write')
        if case.python_bug == 'IKUAI_NETMASK_SUFFIX_ROLLBACK':
            before_ids = {row['id'] for row in snapshots['go']['before']['fake']['ikuai']['static_rt']}
            created = [row for row in snapshots['go']['after']['fake']['ikuai']['static_rt'] if row['id'] not in before_ids]
            go_bug_outcome = {'http_status': observations['go']['status'], 'created_static_rows_remaining': len(created)}
            if observations['go']['status'] >= 400 and created:
                found.append({'path': '/safety/failed-release-created-static-routes', 'python': 'Go failure must compensate cleanly',
                              'go': len(created), 'category': 'safety'})
        if case.python_bug == 'AGENT_FORWARD_FILTER_UNHASHABLE':
            body = observations['go']['body']
            gateways = body.get('gateways') if isinstance(body, dict) else None
            usable = (observations['go']['status'] == 200 and isinstance(gateways, list) and len(gateways) == 2
                      and body.get('success') is True and all(isinstance(entry, dict) for entry in gateways)
                      and gateways[0].get('agent_id') == 'm-a' and gateways[1].get('agent_id') == 'm-b'
                      and gateways[1].get('state') == 'needs_persist')
            go_bug_outcome = {'http_status': observations['go']['status'], 'unaffected_gateway_preserved': usable}
            if not usable:
                found.append({'path': '/safety/malformed-Agent-metadata-hides-unaffected-gateway',
                              'python': 'Go must contain one malformed Agent reply and preserve the healthy gateway',
                              'go': observations['go']['status'], 'category': 'safety'})
        if case.python_bug in ('HEAD_CREATES_ROUTER', 'HEAD_PERSISTS_FORWARDING'):
            before, after = snapshots['go']['before'], snapshots['go']['after']
            changed = bool(differences(before['database'], after['database'])) or any(
                differences(before['fake'][key], after['fake'][key]) for key in ('ros', 'ikuai', 'agent'))
            transcript = after['fake']['transcript'][len(before['fake']['transcript']):]
            posts = sum(item['method'] == 'POST' and item['peer'] == 'agent' for item in transcript)
            reads = sum(item['method'] == 'GET' and item['peer'] == 'agent' for item in transcript)
            go_bug_outcome = {'http_status': observations['go']['status'], 'configuration_or_device_changed': changed,
                              'agent_post_requests': posts, 'agent_get_requests': reads}
            if changed or observations['go']['status'] != 200 or posts or (case.route == 'R025' and reads != 2):
                found.append({'path': '/safety/HEAD-must-execute-read-only-GET',
                              'python': 'Go HEAD must read, never execute the POST branch',
                              'go': go_bug_outcome, 'category': 'safety'})
    # Auth values are synthetic but reports don't need bearer credentials.
    request['document'] = cases.resolve(case.document, pair.python.observations)
    request['path'] = cases.resolve(case.path, pair.python.observations)
    request['headers'] = {key: '<synthetic credential>' if key.lower() == 'authorization' else value
                          for key, value in request['headers'].items()}
    request['raw'] = request['raw'].decode(errors='replace') if request['raw'] is not None else None
    if flow_requests.get('python'):
        request['preceding_abandoned_requests'] = flow_requests['python']
    safety = any((item.get('category') if isinstance(item, dict) else item.category) == 'safety' for item in found)
    status = ('blocked-by-difference' if blocked_reference else 'go-unimplemented' if not implemented else 'different' if safety else
              'reference-bug-observation' if case.python_bug else 'different' if found else 'compared')
    unimplemented_reason = {'missing_routes': sorted(set(missing_routes)),
                            'prior_missing_mutations': [item['case'] for item in pair.go.missing_effects]}
    if not implemented and case.effect in ('profile-write', 'profile-write-device', 'router-create', 'router-delete', 'device-only'):
        pair.go.missing_effects.append({'case': case.ident, 'missing_routes': sorted(set(missing_routes))})
    python_bug_outcome = None
    if case.python_bug == 'IKUAI_NETMASK_SUFFIX_ROLLBACK':
        python_bug_outcome = {'created_static_rows_remaining': len(snapshots['python']['after']['fake']['ikuai']['static_rt']),
                              'static_rt_after': snapshots['python']['after']['fake']['ikuai']['static_rt']}
    elif case.python_bug == 'AGENT_FORWARD_FILTER_UNHASHABLE':
        python_bug_outcome = {'http_status': observations['python']['status'], 'unaffected_gateway_hidden': True}
    elif case.python_bug in ('HEAD_CREATES_ROUTER', 'HEAD_PERSISTS_FORWARDING'):
        before, after = snapshots['python']['before'], snapshots['python']['after']
        transcript = after['fake']['transcript'][len(before['fake']['transcript']):]
        python_bug_outcome = {'http_status': observations['python']['status'],
            'configuration_changed': bool(differences(before['database'], after['database'])),
            'agent_post_requests': sum(item['method'] == 'POST' and item['peer'] == 'agent' for item in transcript),
            'agent_get_requests': sum(item['method'] == 'GET' and item['peer'] == 'agent' for item in transcript)}
    return {'case': case.ident, 'route': case.route, 'entries': list(case.entries), 'request': request,
            'status': status,
            'unimplemented_reason': unimplemented_reason if not implemented else None,
            'blocked_reference': blocked_reference,
            'python': observations['python'], 'go': observations.get('go'),
            'python_bug': case.python_bug, 'python_bug_outcome': python_bug_outcome, 'go_bug_outcome': go_bug_outcome,
            'differences': [asdict(item) if not isinstance(item, dict) else item for item in found]}


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
                     'recipes': [item.get('world', '<self-check>') + ':' + item['case'] for item in results if ident in item['entries']],
                     'audit': 'variants-in-progress' if ident in referenced else 'pending'}
              for ident, entry in entries.items()}
    data = {'matrix_entries': len(entries), 'matrix_routes': len(routes),
        'implemented_routes': sum((route.method, shape(route.path)) in available for route in routes.values()),
        'recipe_counts': dict(counts), 'referenced_entries': len(referenced),
        'executed_worlds': sorted({item.get('world', '<self-check>') for item in results}),
        'python_bugs': sorted({item['python_bug'] for item in results if item.get('python_bug')}),
        'complete': False, 'remaining_audits': len(entries),
        'ledger': ledger, 'results': results}
    if error:
        data['fixture_error'] = error
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix(output.suffix + '.tmp')
    temporary.write_text(json.dumps(report_aliases(data), ensure_ascii=False, indent=2) + '\n')
    temporary.replace(output)
    if announce:
        print(json.dumps({key: value for key, value in data.items() if key not in ('ledger', 'results')}, ensure_ascii=False))
    return data


def findings(path, data):
    data = report_aliases(data)
    lines = ['# Routers Step B 黑盒差分发现', '',
        '状态：进行中。矩阵审计尚未完成；不能据此宣称 routers 已通过验收。', '',
        f'矩阵：{data["matrix_routes"]} 条路由、{data["matrix_entries"]} 个设计条目。',
        f'公开 Go 入口实际注册本领域路由：{data["implemented_routes"]} 条。',
        '执行配方统计：`' + json.dumps(data['recipe_counts'], ensure_ascii=False, sort_keys=True) + '`。',
        '实际执行世界（统计仅涵盖这些世界）：`' + ', '.join(data['executed_worlds']) + '`。',
        f'已有执行配方引用的设计条目：{data["referenced_entries"]}；逐条变体审计仍未完成。', '',
        '“Go 尚未实现”单独计数，不算失败，也不算差分通过。路由层 R15/R17 未注册时，',
        '即使底层 R01–R06 已移植，也不能声称 HTTP 行为已比较。', '',
        '仅使用合成数据；两端独立 DATA_DIR、网络命名空间和设备 fake；未访问生产或真实设备。',
        '比较器会检查状态、语义响应头、类型/键/值/数组顺序、完整逻辑数据库、文件与设备状态。',
        'Python 前置条件失败单列为夹具错误，不算 Go 发现；实际比较为零时不能下“无缺陷”结论。', '']
    changed = [result for result in data['results'] if result['status'] == 'different']
    if not changed:
        lines += ['目前没有已比较的 Go 缺陷结论。Python 参考异常单独记录；“Go 尚未实现”不算差分通过。', '']
    for result in (item for item in data['results'] if item.get('python_bug')):
        bug = BUGS[result['python_bug']]
        lines += ['## Python bug · ' + bug.severity + ' · ' + result['python_bug'], '',
                  '- 用例：' + result.get('world', '<self-check>') + ':' + result['case'] + '；矩阵：' + ', '.join(result['entries']),
                  '- 建议负责模块：' + bug.owner + '。', '- ' + bug.summary,
                  '- 请求：`' + json.dumps(result['request'], ensure_ascii=False) + '`',
                  '- Python：`' + json.dumps(result['python'], ensure_ascii=False) + '`',
                  '- Python 设备终态：`' + json.dumps(result['python_bug_outcome'], ensure_ascii=False) + '`',
                  '- Go：' + (('Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`' +
                              json.dumps(result.get('unimplemented_reason'), ensure_ascii=False) + '`')
                             if result['status'] == 'go-unimplemented' else
                             '前置 Go 响应差异阻止后续请求，未使用 Python 的值代替 Go：`' + str(result['blocked_reference']) + '`'
                             if result['go'] is None else
                             '`' + json.dumps({'response': result['go'], 'outcome': result['go_bug_outcome']}, ensure_ascii=False) + '`'),
                  *['- ' + item for item in bug.followups], '']
    for index, result in enumerate(changed, 1):
        severity = 'P1' if result['request']['method'] != 'GET' else 'P2'
        owner = 'R17（客户端 HTTP）' if result['route'] in ('R002', 'R003', 'R004', 'R005', 'R006') else 'R15（路由 HTTP）'
        if result.get('python_bug'):
            owner = BUGS[result['python_bug']].owner
        if result['python']['status'] == 401 and result['go'] and result['go']['status'] < 400:
            severity = 'P1'
        lines += [f'## F{index:03} · {severity} · ' + result.get('world', '<self-check>') + ':' + result['case'], '',
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
    parser.add_argument('--world', action='append', help='iteration only: run these named isolated worlds')
    args = parser.parse_args()
    routes, _ = load()
    results = []
    options = {'alpha': {'routers': {'items': [router(), router('r-ik', 'ikuai')]}},
               'beta': {'routers': {'items': [router()]}}}
    guard_options = {**options, 'system': {'agents': [{'id': 'auth-agent', 'name': '合成鉴权 Agent',
        'service_type': 'oxidns', 'profile_id': 'alpha', 'host': '10.44.0.53', 'port': 18083,
        'version': '1.99.0-go', 'status': 'offline', 'enabled': True, 'token': AGENT_TOKEN}],
        'system_config': {'config_token': CONFIG_TOKEN}}}
    worlds = [('guards', guard_options, (cases.authentication, cases.query_credentials, cases.profile_selection_failures, cases.body_guards,
                      guards.router_settings, guards.client_draft, guards.fixed_ip_shape,
                      guards.route_edit_shape, guards.workspace_shape, guards.rule_sync_shape)),
              ('client-drafts', options, (workflows.client_drafts,)),
              ('workspace-drafts', options, (workflows.workspace_drafts, workflows.router_updates)),
              ('runtime-reads', options, (workflows.runtime_reads,)),
              ('sync-settings', options, (workflows.sync_settings,)),
              ('manual-routes', options, (workflows.manual_routes,)),
              ('job-locks', options, (workflows.job_locks,)),
              ('router-creations', options, (workflows.router_creates,)),
              ('unlinked-inspection', options, (workflows.unlinked_inspection,)),
              ('agent-scope', fixtures.agent_options(), (fixtures.agent_scope,)),
              ('forwarding', fixtures.agent_options(), (fixtures.forwarding,)),
              ('forwarding-failures', fixtures.agent_options(),
                    (gateway.reads, gateway.writes, gateway.response_limits, gateway.display_filter, gateway.malformed_filter_bug)),
              ('forwarding-uncertain', fixtures.agent_options(), (gateway.uncertain, gateway.timeouts)),
              ('probe-errors', options, (probes.routeros, probes.ikuai)),
              ('probe-tls', probes.tls_options(), (probes.tls,)),
              ('probe-ipv6', probes.ipv6_options(), (probes.ipv6,)),
              ('release-ros', releases.options(), (releases.routeros_basic,)),
              ('release-ik-suffix', releases.options(), (releases.ikuai_suffix_bug,)),
              ('release-ik-plain', releases.options(), (releases.ikuai_plain,)),
              ('fixed-ip', clients.options(), (clients.fixed_ip,)),
              ('fixed-ip-guards', clients.options(), (clients.fixed_preview_guards,)),
              ('fixed-ip-stale', clients.options(), (clients.fixed_stale,)),
              ('fixed-ip-uncertain', clients.options(), (clients.fixed_uncertain,)),
              ('fixed-ip-batch-rollback', clients.options(), (clients.fixed_batch_rollback,)),
              ('fixed-ip-64', clients.batch_options(), (clients.batch64,)),
              ('fixed-ip-restart-stable', clients.options(), (clients.fixed_restart_stable,)),
              ('fixed-ip-restart-random', clients.random_key_options(), (clients.fixed_restart_random,)),
              ('fixed-ip-expiry', clients.options(), (clients.fixed_expiry,)),
              ('fixed-ip-binding-cap', clients.binding_limit_options(), (clients.binding_limit,)),
              ('fixed-ip-long-id', clients.options(), (clients.long_binding_id,)),
              ('discovery', clients.options(), (clients.discovery,)),
              ('discovery-failures', clients.options(), (clients.discovery_failures,)),
              ('rule-sync', sync.options(), (sync.manual_refresh,)),
              ('rule-sync-download-failures', sync.options(), (sync.download_failures,)),
              ('rule-sync-proxy', sync.proxy_options(), (sync.proxy_refresh,)),
              ('upstream-linked', upstream.options(), (upstream.linked,)),
              ('dns-guard', dns.options(), (dns.guard,)),
              ('dhcp-ikuai-plain', dns.ikuai_options(), (dns.ikuai_dhcp,)),
              ('dns-ha', dns.ha_options(), (dns.ha,)),
              ('disconnect-release', releases.options(), (disconnect.release,)),
              ('disconnect-fixed', clients.options(), (disconnect.fixed,)),
              ('disconnect-manual', options, (disconnect.manual,)),
              ('policy-view', policy_view.options(), (policy_view.reads,)),
              ('routing-methods', options, (routing.options_and_dispatch, routing.local_heads)),
              ('head-discovery', clients.options(), (routing.discovery_head,)),
              ('head-upstream', upstream.options(), (routing.upstream_head,)),
              ('head-collection-empty', options, (routing.head_collection_empty,)),
              ('head-collection-body', options, (routing.head_collection_body,)),
              ('head-forwarding-empty', fixtures.agent_options(), (routing.head_forwarding_empty,)),
              ('head-forwarding-body', fixtures.agent_options(), (routing.head_forwarding_body,)),
              ('auth-off-editors', auth_off(options), (workflows.client_drafts, workflows.workspace_drafts,
                    workflows.router_updates, workflows.runtime_reads, workflows.sync_settings,
                    workflows.manual_routes, workflows.router_creates, workflows.unlinked_inspection, probes.auth_disabled)),
              ('auth-off-release', auth_off(releases.options()), (releases.routeros_basic, releases.ikuai_plain)),
              ('auth-off-fixed', auth_off(clients.options()), (clients.fixed_ip,)),
              ('auth-off-discovery', auth_off(clients.options()), (clients.discovery,)),
              ('auth-off-forwarding', auth_off(fixtures.agent_options()), (fixtures.forwarding,)),
              ('auth-off-sync', auth_off(sync.options()), (sync.manual_refresh,)),
              ('stored', fixtures.stored_options(), (fixtures.receipts, fixtures.stored_guards))]
    if args.world:
        unknown = set(args.world) - {name for name, _, _ in worlds}
        if unknown:
            parser.error('unknown isolated world: ' + ', '.join(sorted(unknown)))
        worlds = [item for item in worlds if item[0] in args.world]
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
                        result = execute(pair, case, available, routes)
                        result['world'] = name
                        results.append(result)
                    report(args.output, results, available, announce=False)
                    print(name + '/' + group.__name__ + ': ' + str(len(results)) + ' cumulative recipes, '
                          + format(time.monotonic() - started, '.2f') + 's', flush=True)
    except Exception as error:
        report(args.output, results, available or set(), error=str(error))
        # Runtime global classification fixtures must not escape through a
        # diagnostic traceback while their exact values still drive comparisons.
        raise RuntimeError(report_aliases(str(error))) from None
    data = report(args.output, results, available)
    if args.findings:
        findings(args.findings, data)
    if data['recipe_counts'].get('different'):
        return 1
    if data['recipe_counts'].get('reference-bug-observation'):
        return 2  # Recorded separately: requires outcome review, never a false pass.
    if data['recipe_counts'].get('blocked-by-difference'):
        return 1
    return 0 if args.allow_incomplete or data['complete'] else 2


if __name__ == '__main__':
    sys.exit(main())
