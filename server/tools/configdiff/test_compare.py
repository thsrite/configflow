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
