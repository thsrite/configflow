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
        def zipped(content):
            buffer = io.BytesIO()
            with zipfile.ZipFile(buffer, 'w') as archive:
                archive.writestr('rules.txt', content)
            return structure(buffer.getvalue())
        self.assertTrue(differences(zipped('DOMAIN,example.com,DIRECT'), zipped('DOMAIN,example.com,REJECT')))
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


if __name__ == '__main__':
    unittest.main()
