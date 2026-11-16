"""Mutation tests for every comparison class; run on the test server only."""
import copy
import json
from pathlib import Path
import sqlite3
import tempfile
import unittest

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


if __name__ == '__main__':
    unittest.main()
