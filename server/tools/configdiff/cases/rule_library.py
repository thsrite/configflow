"""Shared rule-library CRUD, editable drafts and pure validation."""
import copy
import hashlib
import json

from compare import database
from harness import Pair, json_body
from sources import Source
from subscriptions import Exchange
from cases.subscriptions import require
from cases.config_backup import message

DOMAIN = {'id': 'lib-a', 'name': '域名🧪', 'source_type': 'content', 'behavior': 'domain',
          'format': 'text', 'content': '+.example.com\n', 'enabled': True, 'update_interval': 0}
LIBRARY = [DOMAIN,
           {**DOMAIN, 'id': 'lib-ip', 'name': '地址', 'behavior': 'ipcidr', 'content': '192.0.2.0/24\n'},
           {**DOMAIN, 'id': 'lib-classical', 'name': '混合', 'behavior': 'classical', 'content': 'DOMAIN,example.com\nIP-CIDR,192.0.2.0/24,no-resolve\n'},
           {'id': 'lib-url', 'name': '远端', 'source_type': 'url', 'behavior': 'domain', 'format': 'text',
            'url': 'http://source.example/rules', 'enabled': False, 'update_interval': 0}]


def options():
    return {'shared': {'rule_library': copy.deepcopy(LIBRARY)}}


def library(pair):
    return database(pair.dirs[0])['fields']['shared//rule_library']['value']


def revision(rule):
    return hashlib.sha256(json.dumps(rule, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def content_cache(directory, ident):
    return directory / 'shared/rules/by-id' / (hashlib.sha256(ident.encode()).hexdigest() + '.list')


def crud(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case in ('S01', 'B01', 'B02'):
            pair.request('R164-' + case, 'GET /api/rule-library', expect=200, writes=True,
                         check=lambda r: require([item['has_ip_part'] for item in r.json()] == [False, True, True, False], 'declared and known classical IP parts'))
        for case, fields in [('S01', {'content': '+.example.org\n'}),
                              ('S02', {'format': 'yaml', 'content': 'payload:\n  - +.example.org\n'}),
                              ('B01', {'name': '中文🧪"\\,', 'content': '中文.example\n'}),
                              ('B02', {'url': 'http://unused.example', 'has_ip_part': True}),
                              ('B03', {'content': ''})]:
            data = {**DOMAIN, 'id': 'created-' + case, 'name': '创建-' + case, **fields}
            def check(reply):
                saved = next(rule for rule in library(pair) if rule['id'] == data['id'])
                require('url' not in saved and 'has_ip_part' not in saved, 'content source strips URL and derived has_ip_part')
                require(content_cache(pair.dirs[0], data['id']).read_text() == data['content'], 'exact handwritten bytes saved at stable resource cache path')
            pair.request('R165-' + case, 'POST /api/rule-library', body=json_body(data), expect=200, writes=True, check=check)
        for case, data, status, expected in [
            ('F08', [1], 400, '规则库请求必须是 JSON 对象'), ('F09', {}, 400, '规则库条目必须有 ID'),
            ('F10', {'id': ''}, 400, '规则库条目必须有 ID'), ('F11', {'id': 1}, 400, '规则库条目必须有 ID'),
            ('F12', DOMAIN, 409, '规则库 ID 已存在，请刷新后重试'),
            ('F13', {**DOMAIN, 'id': 'invalid-domain', 'content': 'not a domain'}, 400, None),
            ('F14', {**DOMAIN, 'id': 'invalid-source', 'source_type': 'unknown'}, 400, None)]:
            before = database(pair.dirs[0])
            def check(reply):
                require(database(pair.dirs[0]) == before, 'invalid library creation leaves all rows and revisions unchanged')
                if expected:
                    message(expected)(reply)
            pair.request('R165-' + case, 'POST /api/rule-library', body=json_body(data), expect=status, writes=True, check=check)
        for case, patch in [('S01', {'name': '改名🧪'}), ('S03', {'content': '+.changed.example\n'}),
                             ('B01', {'name': '保留间隔'}), ('B02', {'name': '新中文🧪'})]:
            data = {**next(rule for rule in library(pair) if rule['id'] == 'lib-a'), **patch}
            data.pop('update_interval')
            pair.request('R167-' + case, 'PUT /api/rule-library/<rule_id>', '/api/rule-library/lib-a',
                         body=json_body(data), expect=200, writes=True,
                         check=lambda r: require(next(rule for rule in library(pair) if rule['id'] == 'lib-a')['update_interval'] == 0,
                                                 'editing full rule preserves omitted scheduler interval'))
        for case, path, data, status in [('F08', 'lib-a', [], 400), ('F09', 'missing', DOMAIN, 404),
                                        ('F10', 'lib-a', {**DOMAIN, 'source_type': 'url', 'url': ''}, 400),
                                        ('F13', 'lib-a', {**DOMAIN, 'content': 'DOMAIN,example.com'}, 400)]:
            pair.request('R167-' + case, 'PUT /api/rule-library/<rule_id>', '/api/rule-library/' + path,
                         body=json_body(data), expect=status, writes=True)
        pair.request('R166-S01', 'DELETE /api/rule-library/<rule_id>', '/api/rule-library/lib-a', expect=200, writes=True,
                     check=lambda r: require(not content_cache(pair.dirs[0], 'lib-a').exists(), 'deletion removes exact stable cache file'))
        pair.request('R166-F08', 'DELETE /api/rule-library/<rule_id>', '/api/rule-library/missing', expect=404, writes=True)
        pair.request('R166-B01', 'DELETE /api/rule-library/<rule_id>', '/api/rule-library/lib-ip', expect=200, writes=True)
    with Pair(binary, recorder) as pair:
        pair.request('R164-S02', 'GET /api/rule-library', expect=200, writes=True, check=lambda r: require(r.json() == [], 'empty library'))


def drafts(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case, ident in [('S01', 'lib-a'), ('S02', 'lib-url')]:
            pair.request('R168-' + case, 'GET /api/rule-library/<rule_id>/draft-content', '/api/rule-library/' + ident + '/draft-content',
                         expect=200, writes=True, check=lambda r: require(r.json()['revision'] == revision(next(rule for rule in library(pair) if rule['id'] == ident)),
                                                                         'draft revision hashes exact full stored declaration'))
        pair.request('R168-F08', 'GET /api/rule-library/<rule_id>/draft-content', '/api/rule-library/missing/draft-content', expect=404, writes=True)
        old = revision(library(pair)[0])
        for case, content in [('S01', '+.saved.example\n'), ('B01', '')]:
            body = {'revision': revision(library(pair)[0]), 'content': content}
            def check(reply):
                require(library(pair)[0]['content'] == content and content_cache(pair.dirs[0], 'lib-a').read_text() == content,
                        'draft commit updates exact source and stable cache bytes')
                require(reply.json()['revision'] == revision(library(pair)[0]), 'draft response revision describes actual post-write declaration')
            pair.request('R169-' + case, 'PUT /api/rule-library/<rule_id>/draft-content', '/api/rule-library/lib-a/draft-content',
                         body=json_body(body), expect=200, writes=True, check=check)
        pair.request('R168-B02', 'GET /api/rule-library/<rule_id>/draft-content', '/api/rule-library/lib-a/draft-content',
                     expect=200, writes=True, check=lambda r: require(r.json()['rule']['content'] == '', 'empty draft is actually read after saving empty content'))
        for case, body, ident, status in [
            ('F08', [], 'lib-a', 400), ('F09', {'content': ''}, 'lib-a', 400),
            ('F10', {'revision': old}, 'lib-a', 400), ('F11', {'revision': old, 'content': '', 'extra': 1}, 'lib-a', 400),
            ('F12', {'revision': 1, 'content': ''}, 'lib-a', 400), ('F13', {'revision': old, 'content': 1}, 'lib-a', 400),
            ('F14', {'revision': old, 'content': ''}, 'missing', 404), ('F15', {'revision': old, 'content': ''}, 'lib-a', 409),
            ('F16', {'revision': revision(library(pair)[3]), 'content': ''}, 'lib-url', 400),
            ('F17', {'revision': revision(library(pair)[0]), 'content': 'not a domain'}, 'lib-a', 400)]:
            before = database(pair.dirs[0])
            pair.request('R169-' + case, 'PUT /api/rule-library/<rule_id>/draft-content', '/api/rule-library/' + ident + '/draft-content',
                         body=json_body(body), expect=status, writes=True,
                         check=lambda r: require(database(pair.dirs[0]) == before, 'rejected draft leaves database unchanged'))


def check_content(binary, recorder):
    with Pair(binary, recorder) as pair:
        for case, data in [('S01', {'behavior': 'domain', 'format': 'text', 'content': '+.example.com\n'}),
                            ('S02', {'behavior': 'classical', 'format': 'text', 'content': 'example.com\n'}),
                            ('S03', {'behavior': 'domain', 'content': '+.example.com\n', 'append_lines': ['second.example']}),
                            ('B01', {'behavior': 'domain', 'content': ''}), ('B02', {'behavior': 'domain', 'content': '中文.example\n'}),
                            ('B03', {'behavior': 'domain', 'content': '+.example.com\n', 'append_lines': []}),
                            ('F13', {'behavior': 'domain', 'content': 'not a domain'})]:
            def check(reply):
                require(bool(reply.json()['errors']) == (case == 'F13'), 'inspection reports bad content without raising transport error')
                if case == 'S02':
                    require(reply.json()['content'] == data['content'] and reply.json()['changes'] == [], 'classical draft remains handwritten')
            pair.request('R171-' + case, 'POST /api/rule-library/check-content', body=json_body(data), expect=200, writes=True, check=check)
        for case, data in [('F08', []), ('F08', {}), ('F09', {'content': 1}),
                            ('F10', {'content': '', 'append_lines': 'a'}), ('F11', {'content': '', 'append_lines': [1]}),
                            ('F12', {'content': '', 'append_lines': ['a\nb']}), ('F12', {'content': '', 'append_lines': ['a\rb']})]:
            pair.request('R171-' + case, 'POST /api/rule-library/check-content', body=json_body(data), expect=400,
                         writes=True, variant=repr(data) if case in ('F08', 'F12') else '')


def public_content(binary, recorder):
    opts = options()
    opts['shared']['rule_library'].append({**DOMAIN, 'id': 'lib-empty', 'name': '空内容', 'content': ''})
    opts['shared']['rule_library'].append({**DOMAIN, 'id': 'lib-unicode', 'name': '中文', 'behavior': 'classical', 'content': '# 中文🧪\r\nDOMAIN,example.com\n'})
    with Pair(binary, recorder, options=opts) as pair:
        for rid, root in [(172, '/api/rule-library/content'), (150, '/api/profiles/default/rule-library/content')]:
            route = 'GET ' + ('/api/rule-library/content/<rule_id>' if rid == 172 else '/api/profiles/<profile_id>/rule-library/content/<rule_id>')
            for case, ident, status in [('S01', 'lib-a', 200), ('S02', 'lib-empty', 200), ('B01', 'lib-unicode', 200),
                                       ('B02', 'lib-a', 200), ('F01', 'missing', 404), ('F02', 'lib-url', 400)]:
                def check(reply):
                    if status == 200:
                        require(reply.body == next(rule for rule in library(pair) if rule['id'] == ident)['content'].encode(), 'public content returns exact original UTF-8 and newline bytes')
                pair.request(f'R{rid:03}-' + case, route, root + '/' + ident, auth='', expect=status, writes=True, check=check, raw_body=True)


def remote(binary, recorder):
    import struct
    import zstandard
    with Source(b'+.example.net\n') as source:
        # This route validates only the compressed envelope; core trie decoding
        # is outside its contract. Preserve the accepted opaque body verbatim.
        source.responses['/mrs'] = (200, zstandard.ZstdCompressor(write_checksum=True).compress(b'MRS\x01\x00' + struct.pack('>qq', 0, 0) + b'\x00'),
                                    {'Content-Type': 'application/octet-stream'})
        with Pair(binary, recorder) as pair:
            for case in ('S03', 'B02'):
                data = {**DOMAIN, 'id': 'remote-' + case, 'name': '远端-' + case, 'source_type': 'url', 'url': source.url + '/rules',
                        'content': 'must-remove', 'has_ip_part': True}
                pair.request('R165-' + case, 'POST /api/rule-library', body=json_body(data), expect=200, writes=True,
                             interaction=Exchange(pair, source), variant='remote' if case == 'B02' else '',
                             check=lambda r: require('content' not in r.json()['data'] and 'has_ip_part' not in r.json()['data'], 'URL source strips inline and derived fields'))
            pair.request('R165-S04', 'POST /api/rule-library',
                         body=json_body({**DOMAIN, 'id': 'remote-mrs', 'name': '二进制', 'source_type': 'url', 'format': 'mrs', 'url': source.url + '/mrs'}),
                         expect=200, writes=True, interaction=Exchange(pair, source))
            data = {**next(rule for rule in library(pair) if rule['id'] == 'remote-S03'), 'url': source.url + '/changed'}
            pair.request('R167-S02', 'PUT /api/rule-library/<rule_id>', '/api/rule-library/remote-S03',
                         body=json_body(data), expect=200, writes=True, interaction=Exchange(pair, source))
            source.status = 403
            before = database(pair.dirs[0])
            for case, method, path, body in [('R165-F15', 'POST', '/api/rule-library', {**data, 'id': 'blocked-new'}),
                                            ('R167-F11', 'PUT', '/api/rule-library/remote-S03', {**data, 'url': source.url + '/blocked'})]:
                pair.request(case, method + ' /api/rule-library' + ('/<rule_id>' if method == 'PUT' else ''), path,
                             body=json_body(body), expect=502, writes=True, interaction=Exchange(pair, source),
                             check=lambda r: require(database(pair.dirs[0]) == before, 'blocked source download does not publish proposed declaration'))


def proxy_domains(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.request('R173-S01', 'GET /api/rule-library/proxy-domains', expect=200, writes=True)
        for case, body in [('S01', {'proxy_domains': '  https://proxy.example/  '}), ('B01', {'proxy_domains': 'https://proxy.example/中文🧪/%25?x=1'}),
                            ('S02', {'proxy_domains': ''}), ('B02', {})]:
            expected = body.get('proxy_domains', '').strip()
            pair.request('R174-' + case, 'POST /api/rule-library/proxy-domains', body=json_body(body), expect=200, writes=True,
                         check=lambda r: require(database(pair.dirs[0])['fields']['system//system_config']['value']['github_proxy_domain'] == expected, 'trimmed proxy prefix saved exactly'))
            pair.request('R173-' + ('B01' if case == 'B01' else 'S02'), 'GET /api/rule-library/proxy-domains',
                         expect=200, writes=True, variant=case)
        for case, body in [('F08', {'proxy_domains': 1}), ('F09', [1])]:
            pair.request('R174-' + case, 'POST /api/rule-library/proxy-domains', body=json_body(body), expect=500, writes=True)


GROUPS = {'library-crud': crud, 'library-drafts': drafts, 'library-check-content': check_content,
          'library-public-content': public_content, 'library-remote': remote, 'library-proxy-domains': proxy_domains}


def reorder(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case, data, status in [('S01', {'ids': ['lib-ip']}, 200), ('S02', {'ids': ['lib-ip'], 'position': 'bottom'}, 200),
                                   ('S03', {'rules': [{'id': 'lib-classical', 'name': 'do-not-store'}]}, 200),
                                   ('B01', {'ids': []}, 200), ('B02', {'rules': [{'id': 'lib-a', 'content': 'do-not-store'}]}, 200),
                                   ('F08', {'rules': [1]}, 404), ('F09', {'ids': 'lib-a'}, 404),
                                   ('F10', {'ids': ['lib-a', 'lib-a']}, 404), ('F10', {'ids': [1]}, 404),
                                   ('F11', {'ids': ['missing']}, 404), ('F12', [1], 500)]:
            before = {rule['id']: rule for rule in library(pair)}
            pair.request('R175-' + case, 'POST /api/rule-library/reorder', body=json_body(data), expect=status, writes=True,
                         check=lambda r: require({rule['id']: rule for rule in library(pair)} == before, 'reorder retains exact declarations'),
                         variant=repr(data) if case == 'F10' else '')
    opts = options()
    opts['profile'] = {'rule_configs': [{'id': 'composed', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT'}]}
    opts['other_profile'] = {'rule_configs': [{'id': 'other-composed', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT'}]}
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R166-F09', 'DELETE /api/rule-library/<rule_id>', '/api/rule-library/lib-a', expect=409, writes=True)
        pair.request('R168-B01', 'GET /api/rule-library/<rule_id>/draft-content', '/api/rule-library/lib-a/draft-content',
                     expect=200, writes=True, check=lambda r: require(r.json()['usage_count'] == 2, 'resource usage includes both independent profiles'))


def network_checks(binary, recorder):
    from rule_library import by_id
    with Source(b'+.example.com\n') as source:
        source.responses['/missing'] = (404, b'missing', {})
        source.responses['/redirect'] = (302, b'', {'Location': source.url + '/rules'})
        with Pair(binary, recorder) as pair:
            for case, path, status, count in [('S01', '/rules', 200, 1), ('S02', '/missing', 404, 1),
                                              ('B01', '/redirect', 200, 2), ('B02', '/%E4%B8%AD/%25', 200, 1)]:
                pair.request('R177-' + case, 'POST /api/rule-library/test-single', body=json_body({'url': source.url + path}),
                             expect=200, writes=True, interaction=Exchange(pair, source, expected=count),
                             check=lambda r: require(r.json()['status_code'] == status and r.json()['available'] is (status == 200), 'single GET availability is exact-200 only'))
            for case, body, status in [('F08', {}, 400), ('F09', {'url': ''}, 400),
                                       ('F10', {'url': 'ftp://synthetic.invalid/rules'}, 500),
                                       ('F11', {'url': 'http://127.0.0.1:1/rules'}, 500)]:
                pair.request('R177-' + case, 'POST /api/rule-library/test-single', body=json_body(body), expect=status,
                             writes=True, interaction=Exchange(pair, source, expected=0))
        opts = options()
        opts['shared']['rule_library'][3].update(url=source.url + '/rules', enabled=True)
        opts['shared']['rule_library'].append({**opts['shared']['rule_library'][3], 'id': 'lib-fail', 'name': '失败源', 'url': source.url + '/missing'})
        with Pair(binary, recorder, options=opts) as pair:
            for case in ('S01', 'S02', 'S03', 'F09', 'B02'):
                exchange = Exchange(pair, source, expected=2, unordered=True)
                def check(reply):
                    data = reply.json()
                    require(data['total_count'] == 2 and data['skipped_count'] == 3 and data['failed_count'] == 1,
                            'bulk HEAD visits only URL sources and counts failure')
                    rows = {rule['id']: rule for rule in library(pair)}
                    require(rows['lib-url']['enabled'] is True and rows['lib-fail']['enabled'] is False,
                            'only failed source is automatically disabled')
                    require(all(r['method'] == 'HEAD' for r in exchange.transcripts[0]), 'bulk connectivity uses HEAD rather than downloading bodies')
                pair.request('R176-' + case, 'POST /api/rule-library/test', body='{}', expect=200, writes=True,
                             interaction=exchange, projection=by_id, check=check)
        source.responses.pop('/missing')
        with Pair(binary, recorder, options=opts) as pair:
            pair.request('R176-B03', 'POST /api/rule-library/test', body='{}', expect=200, writes=True,
                         interaction=Exchange(pair, source, expected=2, unordered=True), projection=by_id,
                         check=lambda r: require(all(rule['enabled'] for rule in library(pair)), 'all-success checks leave enabled preferences intact'))
    with Pair(binary, recorder) as pair:
        pair.request('R176-B01', 'POST /api/rule-library/test', body='{}', expect=200, writes=True,
                     check=lambda r: require(r.json()['results'] == [] and r.json()['total_count'] == 0, 'empty URL library has no checks'))


def cache(binary, recorder):
    from rule_library import by_id
    from task_lifecycle import configure
    from subscriptions import capture_notice
    with Source(b'+.example.com\n') as source:
        opts = options()
        opts['shared']['rule_library'][3].update(url=source.url + '/rules', enabled=True)
        with Pair(binary, recorder, options=opts) as pair:
            configure(pair, 'rule_source:lib-url')
            original = database(pair.dirs[0])
            for case, ids in [('S01', ['lib-a', 'lib-url']), ('S02', ['lib-url']), ('B02', ['lib-a', 'lib-ip', 'lib-url'])]:
                pair.request('R170-' + case, 'POST /api/rule-library/cache', body=json_body({'rule_ids': ids}), expect=200,
                             writes=True, interaction=Exchange(pair, source), projection=by_id,
                             check=lambda r: require(r.json()['success_count'] == len(ids) and database(pair.dirs[0]) == original,
                                                     'cache refresh succeeds without editing source preferences'))
            before_files = {path.relative_to(pair.dirs[0]).as_posix(): path.read_bytes()
                            for path in (pair.dirs[0] / 'shared/rules').rglob('*.list')}
            source.status = 403
            for case in ('S03', 'F11', 'B03'):
                def check(reply):
                    result = reply.json()['results'][0]
                    require(result['success'] is False and result['cache_retained'] is True and result['error'] == '上游返回 HTTP 403',
                            'failed refresh preserves valid bytes and exact failure receipt')
                    require(database(pair.dirs[0]) == original, 'failed refresh does not disable source')
                    require(all((pair.dirs[0] / name).read_bytes() == value for name, value in before_files.items()), 'failed refresh preserves every prior cache byte')
                pair.request('R170-' + case, 'POST /api/rule-library/cache', body=json_body({'rule_ids': ['lib-url']}), expect=200,
                             writes=True, interaction=Exchange(pair, source), projection=by_id, check=check,
                             capture=capture_notice(pair, 'rule_source:lib-url'))
            for case, body, status in [('F08', {}, 400), ('F09', {'rule_ids': []}, 400),
                                       ('F10', {'rule_ids': ['missing']}, 404), ('F12', [1], 500)]:
                pair.request('R170-' + case, 'POST /api/rule-library/cache', body=json_body(body), expect=status,
                             writes=True, interaction=Exchange(pair, source, expected=0))
    opts = {'shared': {'rule_library': [{**DOMAIN, 'content': ''}]}}
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R170-B01', 'POST /api/rule-library/cache', body=json_body({'rule_ids': ['lib-a']}), expect=200, writes=True,
                     check=lambda r: require(content_cache(pair.dirs[0], 'lib-a').read_bytes() == b'', 'empty handwritten cache remains valid empty file'))


GROUPS.update({'library-reorder': reorder, 'library-network-checks': network_checks, 'library-cache': cache})


def concurrent_draft(binary, recorder):
    from concurrent.futures import ThreadPoolExecutor
    import threading
    import time
    from compare import database_diffs, file_diffs, response_diffs
    from harness import DesignError, send
    with Pair(binary, recorder, options=options()) as pair:
        body = json_body({'revision': revision(library(pair)[0]), 'content': '+.concurrent.example\n'})
        route = 'PUT /api/rule-library/<rule_id>/draft-content'
        available = pair.route_available(route)
        results = []
        for index, base in enumerate(pair.bases):
            if index and not available:
                break
            barrier = threading.Barrier(2)
            def request(_):
                barrier.wait(timeout=5)
                return send(base, 'PUT', '/api/rule-library/lib-a/draft-content', body,
                            {'Authorization': 'Bearer ' + pair.tokens[index], 'Content-Type': 'application/json'})
            with ThreadPoolExecutor(max_workers=2) as pool:
                results.append(sorted(pool.map(request, range(2)), key=lambda r: r.status))
        if [r.status for r in results[0]] != [200, 409]:
            raise DesignError('Python identical-revision race did not produce one success and one conflict')
        require(content_cache(pair.dirs[0], 'lib-a').read_text() == '+.concurrent.example\n', 'CAS winner persisted exact cache bytes')
        pair.policy.end = time.time()
        diffs = []
        if available:
            for left, right in zip(*results):
                diffs += response_diffs(left, right, pair.policy)
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        recorder.record('R169-B02', 'same-revision-two-clients', route, 'B',
                        'go-unimplemented' if not available else 'different' if diffs else 'compared', diffs,
                        python_statuses=[r.status for r in results[0]], go_statuses=[r.status for r in results[1]] if available else None)


GROUPS['library-concurrent-draft'] = concurrent_draft
