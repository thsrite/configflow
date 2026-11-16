"""Mutation tests for every comparison class; run on the test server only."""
import copy
import json
import io
import zipfile
import tarfile
from pathlib import Path
import sqlite3
import tempfile
import unittest
from types import SimpleNamespace
import time

from compare import (Policy, differences, structure, response_diffs,
                     database_diffs, file_diffs)
from harness import Reply


class Comparators(unittest.TestCase):
    def test_managed_zip_assets_keep_bytes_and_exact_member_closure(self):
        from assets import zip_assets, raw_asset_diffs
        def archive(items):
            target = io.BytesIO()
            with zipfile.ZipFile(target, 'w') as output:
                for name, content in items:
                    output.writestr(name, content)
            return target.getvalue()
        original = [('config.yaml', b'plugins: []\n'), ('rules/native.txt', b'# original\r\ndomain:example.com\r\n'),
                    ('data/opaque.bin', b'\x00\xff\x01')]
        left = zip_assets(archive(original))
        self.assertEqual(raw_asset_diffs(left, zip_assets(archive(original)), Policy()), [])
        for changed in (original[:1] + [('rules/native.txt', original[1][1].replace(b'\r\n', b'\n'))] + original[2:],
                        original[:2], original + [('extra', b'unexpected')]):
            self.assertTrue(raw_asset_diffs(left, zip_assets(archive(changed)), Policy()))
        with self.assertRaises(ValueError):
            zip_assets(archive(original + [original[1]]))

    def test_original_response_and_rule_cache_keep_exact_newline_bytes(self):
        left = Reply(200, {'content-type': 'text/plain; charset=utf-8'}, b'# synthetic\r\nexample.com\n')
        right = Reply(200, dict(left.headers), b'# synthetic\nexample.com\n')
        self.assertEqual(response_diffs(left, right), [])
        self.assertTrue(response_diffs(left, right, raw_body=True))
        self.assertEqual(response_diffs(left, copy.deepcopy(left), raw_body=True), [])
        with tempfile.TemporaryDirectory() as temporary:
            dirs = [Path(temporary, name) for name in ('python', 'go')]
            for directory, value in zip(dirs, (left.body, right.body)):
                path = directory / 'shared/rules/by-id/synthetic.list'
                path.parent.mkdir(parents=True)
                path.write_bytes(value)
            self.assertTrue(file_diffs(*dirs, Policy()))
    def test_node_substore_protocol_requires_uri_query_and_cleanup(self):
        from substore import normalized_node, ProtocolError
        from urllib.parse import quote
        name, content = '_cf_tmp_node_convert_01234567', 'ss://synthetic@example.com#中文🧪+%'
        records = [{'method': 'POST', 'path': '/api/subs', 'body': {'name': name, 'url': 'https://example.com#noFlow', 'noFlow': True}},
                   {'method': 'GET', 'path': '/download/' + name + '/ClashMeta?content=' + quote(content, safe=''), 'body': None},
                   {'method': 'DELETE', 'path': '/api/sub/' + name, 'body': None}]
        self.assertEqual(len(normalized_node(records, content)), 3)
        for changed in (records[:2], [records[0], {**records[1], 'path': '/download/' + name + '/ClashMeta?content=wrong'}, records[2]],
                        [records[0], records[1], {**records[2], 'path': '/api/sub/wrong'}]):
            with self.assertRaises(ProtocolError):
                normalized_node(changed, content)
    def test_elapsed_milliseconds_are_finite_bounded_and_two_decimal_places(self):
        policy = Policy(start=100, end=101, elapsed_ms={'body.elapsed_time'})
        self.assertEqual(differences({'elapsed_time': 1.23}, {'elapsed_time': 900}, policy=policy), [])
        for value in (-1, True, '1.23', float('nan'), float('inf'), 3001, 1.234):
            self.assertTrue(differences({'elapsed_time': 1.23}, {'elapsed_time': value}, policy=policy))
    def test_composition_ids_require_prefix_shape_uniqueness_and_derived_cache(self):
        from composition import identities, capture, cache_name
        good = {'data': {'library': {'id': 'lib_0123456789ab'},
                         'rules': [{'id': 'ruleset_abcdef012345', 'itemType': 'ruleset'}]}}
        self.assertEqual(len(identities(good)), 2)
        for value in ({'data': {'rules': [] , 'library': {'id': 'bad'}}},
                      {'data': {'rules': [{'id': 'rule_0123456789ab'}, {'id': 'rule_0123456789ab'}]}},
                      {'data': {'rules': [{'id': 'rule_ABCDEF012345'}]}}, {'data': {'rules': None}}):
            with self.assertRaises(ValueError):
                identities(value)
        with tempfile.TemporaryDirectory() as temporary:
            import hashlib
            dirs = [Path(temporary, 'python'), Path(temporary, 'go')]
            other = copy.deepcopy(good)
            other['data']['library']['id'] = 'lib_abcdef012345'
            other['data']['rules'][0]['id'] = 'ruleset_0123456789ab'
            for directory, value in zip(dirs, (good, other)):
                path = directory / 'shared/rules/by-id' / (hashlib.sha256(value['data']['library']['id'].encode()).hexdigest() + '.list')
                path.parent.mkdir(parents=True)
                path.write_text('+.example.com\n')
            policy = Policy()
            replies = [Reply(200, {}, json.dumps(value).encode()) for value in (good, other)]
            self.assertEqual(capture(SimpleNamespace(dirs=dirs))(replies, policy), [])
            self.assertEqual(file_diffs(*dirs, policy), [])
            next(dirs[1].rglob('*.list')).write_text('+.wrong.example\n')
            self.assertTrue(file_diffs(*dirs, policy))
        remote = {'id': 'lib_0123456789ab', 'source_type': 'url', 'url': 'http://source.example/中文', 'behavior': 'domain', 'format': 'text'}
        self.assertTrue(cache_name(remote).startswith('shared/rules/by-source/'))
        self.assertNotEqual(cache_name(remote), cache_name({**remote, 'url': remote['url'] + '/changed'}))

    def test_rule_results_align_by_id_without_hiding_loss_or_wrong_result(self):
        from rule_library import by_id
        value = {'results': [{'id': 'b', 'success': True}, {'id': 'a', 'success': False}], 'failed_count': 1}
        reordered = {**value, 'results': list(reversed(value['results']))}
        self.assertEqual(differences(by_id(value), by_id(reordered)), [])
        for rows in ([{'id': 'b', 'success': True}], [{'id': 'b', 'success': True}] * 2,
                     [{'id': 'b', 'success': True}, {'id': 'a', 'success': True}]):
            self.assertTrue(differences(by_id(value), by_id({**value, 'results': rows})))
        from subscriptions import Exchange
        observer = Exchange(None, None, unordered=True)
        first = {'method': 'HEAD', 'path': '/a', 'user_agent': 'synthetic'}
        second = {'method': 'HEAD', 'path': '/b', 'user_agent': 'synthetic'}
        observer.transcripts = [[first, second], [second, first]]
        self.assertEqual(observer.compare(Policy()), [])
        observer.transcripts[1] = [second, {**first, 'method': 'GET'}]
        self.assertTrue(observer.compare(Policy()))

    def test_background_receipt_requires_exact_result_and_count(self):
        from task_lifecycle import receipt_errors
        value = {'run_count': 2, 'last_status': 'failed', 'last_trigger': 'manual', 'last_error': 'synthetic failure',
                 'consecutive_failures': 2, 'failure_count': 2}
        self.assertEqual(receipt_errors(value, count=2, status='failed', error='synthetic failure'), [])
        for field, wrong in [('run_count', 1), ('last_status', 'success'), ('last_trigger', 'schedule'),
                             ('last_error', ''), ('failure_count', 1), ('consecutive_failures', 0)]:
            changed = {**value, field: wrong}
            self.assertTrue(receipt_errors(changed, count=2, status='failed', error='synthetic failure'))

    def test_partial_and_no_body_responses(self):
        a = Reply(206, {'content-type': 'application/json', 'content-range': 'bytes 0-3/20'}, b'{"a"')
        self.assertEqual(response_diffs(a, copy.deepcopy(a)), [])
        b = copy.deepcopy(a)
        b.body = b'{"b"'
        self.assertTrue(response_diffs(a, b))
        b = copy.deepcopy(a)
        b.headers['content-range'] = 'bytes 1-4/20'
        self.assertTrue(response_diffs(a, b))
        for status in (204, 304):
            a = Reply(status, {'content-type': 'application/json'}, b'')
            self.assertEqual(response_diffs(a, copy.deepcopy(a)), [])
            b = copy.deepcopy(a)
            b.body = b'wrong'
            self.assertTrue(response_diffs(a, b))

    def test_http_status_and_headers_mutations(self):
        reply = Reply(200, {'content-type': 'application/json', 'etag': '"fixed"'}, b'{"success":true}')
        self.assertEqual(response_diffs(reply, copy.deepcopy(reply)), [])
        changed = copy.deepcopy(reply)
        changed.status = 201
        self.assertTrue(any(d.path == 'status' for d in response_diffs(reply, changed)))
        changed = copy.deepcopy(reply)
        changed.headers['etag'] = '"broken"'
        self.assertTrue(any(d.path == 'headers.etag' for d in response_diffs(reply, changed)))

    def test_allow_order_does_not_hide_missing_method(self):
        a = Reply(405, {'content-type': 'text/html', 'allow': 'HEAD, GET, OPTIONS'}, b'HTML')
        b = Reply(405, {'content-type': 'text/html', 'allow': 'GET, OPTIONS, HEAD'}, b'HTML')
        self.assertEqual(response_diffs(a, b), [])
        b.headers['allow'] = 'GET, OPTIONS'
        self.assertTrue(response_diffs(a, b))

    def test_internal_exception_keeps_chinese_prefix(self):
        policy = Policy(internal_errors={'body.message': '登录失败。原始错误：'})
        self.assertEqual(differences({'message': '登录失败。原始错误：AttributeError'},
                                    {'message': '登录失败。原始错误：decode error'}, policy=policy), [])
        self.assertTrue(differences({'message': '登录失败。原始错误：AttributeError'},
                                   {'message': 'wrong message'}, policy=policy))

    def test_nested_json_mutation_missing_and_type(self):
        value = {'a': [{'nested': {'number': 3, 'enabled': True}}]}
        changed = copy.deepcopy(value)
        changed['a'][0]['nested']['number'] = 4
        self.assertTrue(differences(value, changed))
        del changed['a'][0]['nested']['number']
        self.assertTrue(differences(value, changed))
        self.assertTrue(differences(True, 1))
        self.assertTrue(differences([1, 2], [2, 1]))

    def test_yaml_and_text_mutations(self):
        a = structure(b'hosts:\n  synthetic.example: 192.0.2.10\n', 'text/yaml')
        b = structure(b'hosts:\n  synthetic.example: 192.0.2.11\n', 'text/yaml')
        self.assertTrue(differences(a, b))
        self.assertTrue(differences(structure(b'[Rule]\nDOMAIN,example.com,DIRECT\n'),
                                    structure(b'[Rule]\nDOMAIN,example.com,REJECT\n')))

    def test_archive_and_binary_mutations(self):
        def zipped(content, directory=False):
            buffer = io.BytesIO()
            with zipfile.ZipFile(buffer, 'w') as archive:
                archive.writestr('rules.txt', content)
                if directory:
                    archive.writestr('empty/', '')
            return structure(buffer.getvalue())
        self.assertTrue(differences(zipped('DOMAIN,example.com,DIRECT'), zipped('DOMAIN,example.com,REJECT')))
        self.assertTrue(differences(zipped('same'), zipped('same', directory=True)))
        def tarred(content):
            buffer = io.BytesIO()
            with tarfile.open(fileobj=buffer, mode='w:gz') as archive:
                member = tarfile.TarInfo('rules.txt')
                member.size = len(content)
                archive.addfile(member, io.BytesIO(content))
            return structure(buffer.getvalue(), filename='bundle.tar.gz')
        self.assertTrue(differences(tarred(b'one'), tarred(b'two')))
        self.assertTrue(differences(structure(b'\xff\x00'), structure(b'\xff\x01')))

    def test_database_row_mutation(self):
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for directory in dirs:
                directory.mkdir()
                with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    db.execute('INSERT INTO meta VALUES (?,?)', ('generation', '1'))
                    db.execute('INSERT INTO sections VALUES (?,?,?)', ('profile', 'default', 0))
                    db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)',
                               ('profile', 'default', 'rule_configs', 0, '[{"policy":"DIRECT"}]', 1))
            self.assertEqual(database_diffs(*dirs, Policy()), [])
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute('UPDATE fields SET value=?', ('[{"policy":"REJECT"}]',))
            self.assertTrue(database_diffs(*dirs, Policy()))
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute('UPDATE fields SET value=?, revision=2', ('[{"policy":"DIRECT"}]',))
            self.assertTrue(database_diffs(*dirs, Policy()))

    def test_import_snapshot_must_contain_previous_rows(self):
        from compare import database
        from snapshots import snapshot_errors
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            backup = root / 'migrations/synthetic/import'
            backup.mkdir(parents=True)
            with sqlite3.connect(backup / 'configflow.sqlite3') as db:
                db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                 'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                db.execute('INSERT INTO meta VALUES (?,?)', ('generation', '1'))
                db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)',
                           ('profile', 'default', 'rule_configs', 0, '[{"policy":"DIRECT"}]', 1))
            before = database(backup)
            self.assertEqual(snapshot_errors(root, before, set()), [])
            with sqlite3.connect(backup / 'configflow.sqlite3') as db:
                db.execute('UPDATE fields SET revision=2')
            self.assertTrue(snapshot_errors(root, before, set()))
            self.assertTrue(snapshot_errors(root, before, {backup / 'configflow.sqlite3'}))

    def test_publish_guard_uses_profile_and_shared_revisions(self):
        from publish import capture, check
        from harness import DesignError
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for root in dirs:
                root.mkdir()
                with sqlite3.connect(root / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    for scope, owner, value in [('profile', 'other', 7), ('shared', '', 3)]:
                        db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)', (scope, owner, '_revision', 0, str(value), 1))
            pair = SimpleNamespace(dirs=dirs)
            correct = {'publish_guard': {'expected_profile_id': 'other', 'expected_service_type': 'mihomo', 'expected_config_revision': 10}}
            a = Reply(200, {}, json.dumps(correct).encode())
            check(pair, 'other', 'mihomo')(a)
            self.assertEqual(capture(pair, 'other', 'mihomo')([a, copy.deepcopy(a)], Policy()), [])
            for key, bad in [('expected_profile_id', 'default'), ('expected_service_type', 'surge'), ('expected_config_revision', 7)]:
                changed = copy.deepcopy(correct)
                changed['publish_guard'][key] = bad
                b = Reply(200, {}, json.dumps(changed).encode())
                self.assertTrue(capture(pair, 'other', 'mihomo')([a, b], Policy()))
                with self.assertRaises(DesignError):
                    check(pair, 'other', 'mihomo')(b)

    def test_subscription_exchange_and_cached_identity_mutation(self):
        from subscriptions import Exchange, capture_ids
        source = SimpleNamespace(requests=[])
        observer = Exchange(None, source)
        observer.transcripts = [[{'method': 'GET', 'path': '/feed', 'user_agent': 'clash.meta'}]] * 2
        self.assertEqual(observer.compare(Policy()), [])
        observer.transcripts[1] = []
        self.assertTrue(observer.compare(Policy()))
        observer.transcripts[1] = [{'method': 'GET', 'path': '/feed', 'user_agent': 'wrong'}]
        self.assertTrue(observer.compare(Policy()))
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for root, ids in zip(dirs, [('node_12345678', 'node_12345679'), ('node_abcdef01', 'node_abcdef02')]):
                cache = root / 'shared/subscribes'
                cache.mkdir(parents=True)
                (cache / 'sub-a.json').write_text(json.dumps({'nodes': [{'id': value} for value in ids]}))
            pair = SimpleNamespace(dirs=dirs)
            self.assertEqual(capture_ids(pair)([], Policy()), [])
            target = dirs[1] / 'shared/subscribes/sub-a.json'
            target.write_text(json.dumps({'nodes': [{'id': 'node_abcdef01'}, {'id': 'node_abcdef01'}]}))
            self.assertTrue(capture_ids(pair)([], Policy()))
            target.write_text(json.dumps({'nodes': [{'id': 'broken-id'}, {'id': 'node_abcdef02'}]}))
            self.assertTrue(capture_ids(pair)([], Policy()))

    def test_database_unilateral_commit_reports_difference(self):
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for root in dirs:
                root.mkdir()
                with sqlite3.connect(root / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    db.execute('INSERT INTO meta VALUES (?,?)', ('commit_id', json.dumps('a' * 32)))
            policy = Policy()
            self.assertEqual(database_diffs(*dirs, policy), [])
            with sqlite3.connect(dirs[1] / 'configflow.sqlite3') as db:
                db.execute('UPDATE meta SET value=?', (json.dumps('b' * 32),))
            self.assertTrue(any(d.kind == 'unstable-Go-commit' for d in database_diffs(*dirs, policy)))

    def test_blocked_exception_details_preserve_chinese_and_wrong_status(self):
        from subscriptions import blocked_error_projection as project
        a = {'message': '从订阅URL获取失败: request_failed (exception_type=SubscriptionFetchError)，且本地无缓存数据'}
        b = {'message': '从订阅URL获取失败: request_failed (exception_type=SubscriptionFetchError status=403)，且本地无缓存数据'}
        self.assertEqual(project(a, 0), project(b, 1))
        self.assertNotEqual(project(a, 0), project({'message': b['message'].replace('且本地无缓存数据', 'wrong')}, 1))
        self.assertNotEqual(project(a, 0), project({'message': b['message'].replace('403', '500')}, 1))

    def test_task_notification_evidence_uses_real_receipt(self):
        from subscriptions import notice_errors
        task = {'run_count': 1, 'last_run_at': '2026-01-01T00:00:00Z', 'last_status': 'failed', 'consecutive_failures': 1}
        item = {'evidence': '1:2026-01-01T00:00:00Z:failed', 'checked_at': task['last_run_at'],
                'last_seen_at': task['last_run_at'], 'failure_count': 1}
        self.assertEqual(notice_errors(item, task), [])
        for field, bad in [('evidence', '2:2026-01-01T00:00:00Z:failed'), ('failure_count', 0),
                           ('checked_at', '2025-01-01T00:00:00Z'), ('last_seen_at', 'wrong')]:
            changed = copy.deepcopy(item)
            changed[field] = bad
            self.assertTrue(notice_errors(changed, task))

    def test_substore_protocol_cleanup_and_local_payload_mutation(self):
        from substore import normalized, ProtocolError
        name = '_cf_tmp_sub-a_12345678'
        records = [{'method': 'POST', 'path': '/api/subs', 'body': {'name': name, 'url': 'http://source.example/feed#noFlow', 'noFlow': True, 'source': 'local', 'content': 'opaque-body'}},
                   {'method': 'GET', 'path': '/download/' + name + '/ClashMeta?noFlow=true', 'body': None},
                   {'method': 'DELETE', 'path': '/api/sub/' + name, 'body': None}]
        self.assertEqual(len(normalized(records, 'http://source.example/feed', 'opaque-body')), 3)
        with self.assertRaises(ProtocolError):
            normalized(records[:-1], 'http://source.example/feed', 'opaque-body')
        for key, bad in [('noFlow', False), ('content', 'wrong-body'), ('url', 'http://other.example')]:
            changed = copy.deepcopy(records)
            changed[0]['body'][key] = bad
            with self.assertRaises(ProtocolError):
                normalized(changed, 'http://source.example/feed', 'opaque-body')

    def test_provider_local_time_comment_and_filename_mutations(self):
        from datetime import datetime
        from zoneinfo import ZoneInfo
        from providers import comments, filename
        policy = Policy(start=1760000000, end=1760000001, tz='Asia/Shanghai')
        local = datetime.fromtimestamp(policy.start, ZoneInfo(policy.tz)).replace(tzinfo=None)
        raw = ('# Generated: ' + local.isoformat() + 'Z\n# Source: from local cache\nproxies: []\n').encode()
        valid = comments(raw, policy)
        with self.assertRaises(ValueError):
            comments(raw.replace(str(local.year).encode(), b'1990'), policy)
        changed = raw.replace(b'from local cache', b'changed source')
        self.assertTrue(differences(valid, comments(changed, policy)))
        reply = Reply(200, {'content-disposition': 'inline; filename="proxies_sub-a_' + local.strftime('%Y%m%d_%H%M%S') + '.yaml"'}, raw)
        filename(reply, policy)
        with self.assertRaises(ValueError):
            filename(Reply(200, {'content-disposition': reply.headers['content-disposition'].replace('sub-a', 'wrong')}, raw), policy)
        with self.assertRaises(ValueError):
            filename(Reply(200, {'content-disposition': reply.headers['content-disposition'].replace(str(local.year), '1990')}, raw), policy)

    def test_provider_fallback_and_rule_stale_headers_remain_observable(self):
        a = Reply(200, {'content-type': 'text/yaml'}, b'proxies: []')
        for key in ('x-configflow-provider', 'x-configflow-rule-cache', 'x-configflow-cache', 'warning'):
            b = copy.deepcopy(a)
            b.headers[key] = 'unexpected'
            self.assertTrue(response_diffs(a, b))

    def test_provider_fallback_elapsed_and_background_mutation(self):
        from provider_lifecycle import Fallback
        observer = object.__new__(Fallback)
        record = {'elapsed_valid': True, 'elapsed': 12.0, 'requests': [{'path': '/feed'}],
                  'fresh_after_release': True, 'quiescent_after_release': True, 'served_comments': ['# Source: fresh']}
        observer.observed = [record, copy.deepcopy(record)]
        self.assertEqual(observer.compare(Policy()), [])
        observer.observed[1]['elapsed_valid'] = False
        observer.observed[1]['elapsed'] = 14.0
        self.assertTrue(observer.compare(Policy()))
        observer.observed[1] = copy.deepcopy(record)
        observer.observed[1]['fresh_after_release'] = False
        self.assertTrue(observer.compare(Policy()))
        observer.observed[1] = copy.deepcopy(record)
        observer.observed[1]['requests'].append({'path': '/feed'})
        self.assertTrue(observer.compare(Policy()))

    def test_yaml_backup_compares_structure_and_changed_nodes(self):
        a = b'proxies:\n - {name: "\\U0001F9EA", type: socks5, port: 1080}\n'
        b = 'proxies:\n  - name: 🧪\n    type: socks5\n    port: 1080\n'.encode()
        self.assertEqual(structure(a, filename='sub.yaml.bak'), structure(b, filename='sub.yaml.bak'))
        self.assertTrue(differences(structure(a, filename='sub.yaml.bak'), structure(b.replace(b'1080', b'1081'), filename='sub.yaml.bak')))

    def test_all_concurrent_provider_responses_are_compared(self):
        from provider_lifecycle import response_batch_diffs
        a = [Reply(200, {'content-type': 'text/yaml'}, b'proxies: []') for _ in range(20)]
        b = copy.deepcopy(a)
        self.assertEqual(response_batch_diffs(a, b, Policy()), [])
        b[19].status = 401
        self.assertTrue(any(d.path == 'provider.client[19].status' for d in response_batch_diffs(a, b, Policy())))
        self.assertTrue(response_batch_diffs(a, b[:-1], Policy()))

    def test_merged_provider_detail_metadata_mutation(self):
        from datetime import datetime, timezone
        from providers import MergedMetadata
        policy = Policy(start=1760000000, end=1760000001)
        stamp = datetime.fromtimestamp(policy.start, timezone.utc).replace(tzinfo=None).isoformat()
        raw = ('# Generated: ' + stamp + 'Z\n# Subscriptions: 1\n# Total Proxies: 2\n#   - synthetic: 2 proxies (updated: ' + stamp + ')\nproxies: []').encode()
        observer = MergedMetadata(None, [('synthetic', 2)])
        observer.parse(raw, policy)
        for old, new in [(b'synthetic: 2', b'synthetic: 3'), (b'Total Proxies: 2', b'Total Proxies: 3'), (b'updated: 2025', b'updated: 1990')]:
            changed = raw.replace(old, new)
            if changed != raw:
                with self.assertRaises(ValueError):
                    observer.parse(changed, policy)

    def test_future_task_deadline_is_bounded_by_exact_interval(self):
        from datetime import datetime, timezone
        policy = Policy(start=1760000000, end=1760000001, future_times={'body.next': 1800})
        value = datetime.fromtimestamp(1760001800, timezone.utc).isoformat().replace('+00:00', 'Z')
        self.assertEqual(differences({'next': value}, {'next': value}, 'body', policy), [])
        wrong = datetime.fromtimestamp(1760003600, timezone.utc).isoformat().replace('+00:00', 'Z')
        self.assertTrue(differences({'next': value}, {'next': wrong}, 'body', policy))
        self.assertTrue(differences({'next': wrong}, {'next': wrong}, 'body', policy))

    def test_generated_file_mutation(self):
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for directory in dirs:
                directory.mkdir()
                (directory / 'rules.txt').write_text('DOMAIN,example.com,DIRECT\n')
            self.assertEqual(file_diffs(*dirs, Policy()), [])
            (dirs[1] / 'rules.txt').write_text('DOMAIN,example.com,REJECT\n')
            self.assertTrue(file_diffs(*dirs, Policy()))
            (dirs[1] / 'unexpected.txt').write_text('must not disappear')
            self.assertTrue(file_diffs(*dirs, Policy()))

    def test_identity_binding_is_injective_and_does_not_mask_neighbors(self):
        policy = Policy()
        policy.bind('profile_abcdef', 'profile_123456', r'profile_[0-9a-f]{6}', 'profile')
        self.assertEqual(differences({'id': 'profile_abcdef'}, {'id': 'profile_123456'}, policy=policy), [])
        self.assertTrue(differences({'id': 'profile_abcdef', 'name': '甲'},
                                    {'id': 'profile_123456', 'name': '乙'}, policy=policy))
        with self.assertRaises(AssertionError):
            policy.bind('profile_654321', 'profile_123456', r'profile_[0-9a-f]{6}', 'profile')

    def test_time_policy_validates_window_and_format(self):
        policy = Policy(times={'body.at'}, start=1760000000, end=1760000005)
        self.assertTrue(differences({'at': '1990-01-01T00:00:00Z'},
                                    {'at': '1990-01-01T00:00:00Z'}, policy=policy))
        self.assertTrue(differences({'at': 'nonsense'}, {'at': 'nonsense'}, policy=policy))

    def test_duration_is_bounded_and_keeps_other_fields(self):
        policy = Policy(durations={'body.ms'}, start=100, end=102)
        self.assertEqual(differences({'ms': 1}, {'ms': 39}, policy=policy), [])
        for value in (-1, 5000, True, '10'):
            self.assertTrue(differences({'ms': 1}, {'ms': value}, policy=policy))
        self.assertTrue(differences({'ms': 1, 'ok': True}, {'ms': 9, 'ok': False}, policy=policy))

    def test_dav_transcript_and_upload_mutation(self):
        from cases.webdav import Exchange
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for d in dirs:
                d.mkdir()
            exchange = Exchange(None, SimpleNamespace(dirs=dirs, tz='UTC'))
            value = {'requests': [{'method': 'HEAD', 'authorization': 'Basic synthetic',
                                   'path': '/backup/fixed.json', 'body': {'profile': {'rules': ['DIRECT']}}}],
                     'objects': {'/backup/fixed.json': {'profile': {'rules': ['DIRECT']}}}}
            exchange.observations = [copy.deepcopy(value), copy.deepcopy(value)]
            self.assertEqual(exchange.compare(Policy()), [])
            for field, broken in [('method', 'DELETE'), ('authorization', ''), ('body', {'profile': {}})]:
                exchange.observations[1] = copy.deepcopy(value)
                exchange.observations[1]['requests'][0][field] = broken
                self.assertTrue(exchange.compare(Policy()))

    def test_probe_stdin_mutation(self):
        from probe import Exchange
        with tempfile.TemporaryDirectory() as tmp:
            exchange = Exchange(SimpleNamespace(root=Path(tmp)))
            original = {'nodes': [{'name': '["node","node-a"]', 'server': '192.0.2.1'}], 'timeout_ms': 5000}
            exchange.inputs = [[copy.deepcopy(original)], [copy.deepcopy(original)]]
            self.assertEqual(exchange.compare(Policy()), [])
            exchange.inputs[1][0]['nodes'][0]['server'] = '192.0.2.2'
            self.assertTrue(exchange.compare(Policy()))
            exchange.inputs[1] = []
            self.assertTrue(exchange.compare(Policy()))

    def test_artifact_verified_digest_and_content_mutation(self):
        import hashlib
        from artifacts import validate, ArtifactError
        with tempfile.TemporaryDirectory() as tmp:
            def fixture(name, content):
                folder = Path(tmp, name, '.generated-previews', 'a' * 32)
                folder.mkdir(parents=True, exist_ok=True)
                digest = hashlib.sha256(content).hexdigest()
                manifest = {'agent_id': 'manual', 'deployment_id': 'b' * 32, 'profile_id': 'default',
                    'service_type': 'mihomo', 'config_path': 'config.yaml', 'files': [
                        {'path': 'config.yaml', 'role': 'config', 'size': len(content), 'sha256': digest}]}
                output = io.BytesIO()
                with tarfile.open(fileobj=output, mode='w:gz') as tar:
                    for path, value in [('manifest.json', json.dumps(manifest).encode()), ('files/config.yaml', content)]:
                        member = tarfile.TarInfo(path)
                        member.size, member.mode = len(value), 0o600
                        tar.addfile(member, io.BytesIO(value))
                archive = output.getvalue()
                record = {'id': 'a' * 32, 'deployment_id': 'b' * 32, 'expires_at': 3700, 'agent_id': 'manual',
                    'profile_id': 'default', 'service_type': 'mihomo', 'bundle_meta': {'manifest': manifest,
                        'sha256': hashlib.sha256(archive).hexdigest(), 'config_version': digest[:8]}}
                (folder / 'record.json').write_text(json.dumps(record))
                (folder / 'bundle.tar.gz').write_bytes(archive)
                return Path(tmp, name), folder
            left, _ = fixture('left', b'port: 8080\n')
            right, folder = fixture('right', b'port: 8080\n')
            self.assertEqual(differences(validate(left, 'a'*32, 100, 100)['projection'],
                                        validate(right, 'a'*32, 100, 100)['projection']), [])
            fixture('right', b'port: 8081\n')
            self.assertTrue(differences(validate(left, 'a'*32, 100, 100)['projection'],
                                       validate(right, 'a'*32, 100, 100)['projection']))
            record = json.loads((folder / 'record.json').read_text())
            record['bundle_meta']['sha256'] = '0' * 64
            (folder / 'record.json').write_text(json.dumps(record))
            with self.assertRaises(ArtifactError):
                validate(right, 'a'*32, 100, 100)

    def test_epoch_window_mutation(self):
        policy = Policy(start=100, end=105, epochs={'body.expires_at': 3600})
        self.assertEqual(differences({'expires_at': 3700}, {'expires_at': 3705}, policy=policy), [])
        for invalid in (1, 3800, True, '3700'):
            self.assertTrue(differences({'expires_at': 3700}, {'expires_at': invalid}, policy=policy))

    def test_damaged_file_and_binary_evidence_mutation(self):
        from harness import Recorder
        from compare import Difference
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for d in dirs:
                d.mkdir()
                (d / 'damaged.json.bak').write_bytes(b'{invalid\xff')
            self.assertEqual(file_diffs(*dirs, Policy()), [])
            (dirs[1] / 'damaged.json.bak').write_bytes(b'{invalid\xfe')
            diffs = file_diffs(*dirs, Policy())
            self.assertTrue(diffs)
            recorder = Recorder()
            recorder.record('self-check', '', 'files', 'self-check', 'different', diffs)
            self.assertIn('bytes_sha256', json.dumps(recorder.results, allow_nan=False))
        self.assertEqual(differences(float('nan'), float('nan')), [])
        self.assertTrue(differences(float('nan'), 1.0))
        recorder = Recorder()
        recorder.record('self-check', '', 'body', 'self-check', 'different',
                        [Difference('body', 'value', '\ud800', float('nan'))])
        json.dumps(recorder.results, ensure_ascii=False, allow_nan=False).encode('utf-8')

    def test_nested_database_backup_row_mutation(self):
        with tempfile.TemporaryDirectory() as tmp:
            roots = [Path(tmp, n) for n in ('python', 'go')]
            for root in roots:
                folder = root / 'migrations/fixed/import'
                folder.mkdir(parents=True)
                with sqlite3.connect(folder / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)', ('shared', '', 'nodes', 0, '[{"name":"original"}]', 1))
            self.assertEqual(file_diffs(*roots, Policy()), [])
            with sqlite3.connect(roots[1] / 'migrations/fixed/import/configflow.sqlite3') as db:
                db.execute('UPDATE fields SET value=?', ('[{"name":"changed"}]',))
            self.assertTrue(file_diffs(*roots, Policy()))

    def test_naive_timestamp_uses_selected_server_timezone(self):
        policy = Policy(times={'body.at'}, start=1767225600, end=1767225605, tz='Asia/Shanghai')
        self.assertEqual(differences({'at': '2026-01-01T08:00:00'}, {'at': '2026-01-01T08:00:01'}, policy=policy), [])
        self.assertTrue(differences({'at': '2026-01-01T00:00:00'}, {'at': '2026-01-01T08:00:01'}, policy=policy))

    def test_generated_ini_fields_and_order_mutation(self):
        a = b'[Proxy]\nnode = socks5, 192.0.2.1, 1080\n[Rule]\nDOMAIN,example.com,DIRECT\nFINAL,REJECT\n'
        b = b'[Proxy]\n node=socks5,192.0.2.1,1080\n\n[Rule]\n DOMAIN, example.com, DIRECT\nFINAL, REJECT\n'
        self.assertEqual(structure(a, filename='surge.conf'), structure(b, filename='surge.conf'))
        changed = b.replace(b'DIRECT', b'REJECT')
        self.assertTrue(differences(structure(a, filename='surge.conf'), structure(changed, filename='surge.conf')))
        changed = b.replace(b'DOMAIN, example.com, DIRECT\nFINAL, REJECT', b'FINAL, REJECT\nDOMAIN, example.com, DIRECT')
        self.assertTrue(differences(structure(a, filename='loon.lcf'), structure(changed, filename='loon.lcf')))

    def test_download_headers_describe_actual_bytes(self):
        from downloads import capture_headers
        from email.utils import formatdate
        policy = Policy(start=1767225600, end=1767225601)
        left = Reply(200, {'etag': '"left"', 'last-modified': formatdate(policy.start, usegmt=True), 'content-length': '3'}, b'abc')
        right = copy.deepcopy(left)
        right.headers['etag'] = '"right"'
        self.assertEqual(capture_headers([left, right], policy), [])
        for header, value in [('content-length', '4'), ('etag', 'unquoted'),
                               ('last-modified', 'Wed, 01 Jan 2020 00:00:00 GMT')]:
            broken = copy.deepcopy(right)
            broken.headers[header] = value
            self.assertTrue(capture_headers([left, broken], Policy(start=policy.start, end=policy.end)))

    def test_oxidns_etag_and_committed_guard_mutation(self):
        import hashlib
        from oxidns import etag, capture
        value = {'n': 9007199254740993, 'b': 1.25, 'a': '🧪'}
        literal = '{"a":"🧪","b":1.25,"n":9007199254740993}'
        self.assertEqual(etag(value), '"' + hashlib.sha256(literal.encode()).hexdigest() + '"')
        self.assertNotEqual(etag(value), etag({**value, 'b': 2.0}))
        with tempfile.TemporaryDirectory() as tmp:
            dirs = [Path(tmp, n) for n in ('python', 'go')]
            for directory in dirs:
                directory.mkdir()
                with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                    db.executescript('CREATE TABLE meta(key,value); CREATE TABLE sections(scope,owner,position); '
                                     'CREATE TABLE fields(scope,owner,field,position,value,revision);')
                    for scope, owner, field, item in [('profile', 'default', 'oxidns', value),
                                                     ('profile', 'default', '_revision', 2), ('shared', '', '_revision', 3)]:
                        db.execute('INSERT INTO fields VALUES (?,?,?,?,?,?)', (scope, owner, field, 0, json.dumps(item), 1))
            guard = {'expected_profile_id': 'default', 'expected_service_type': 'oxidns', 'expected_config_revision': 5}
            left = Reply(200, {'etag': etag(value)}, json.dumps({'publish_guard': guard}).encode())
            right = copy.deepcopy(left)
            observer = capture(SimpleNamespace(dirs=dirs))
            self.assertEqual(observer([left, right], Policy()), [])
            right.headers['etag'] = '"' + '0' * 64 + '"'
            self.assertTrue(observer([left, right], Policy()))
            right = copy.deepcopy(left)
            right.body = json.dumps({'publish_guard': {**guard, 'expected_config_revision': 4}}).encode()
            self.assertTrue(observer([left, right], Policy()))


if __name__ == '__main__':
    unittest.main()
