"""Verify only the synthetic fixture boundary, not product HTTP coverage."""
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest

from inventory import ROOT


class SeedTests(unittest.TestCase):
    def test_missing_agent_profile_does_not_reach_public_http_fixture(self):
        # A matrix legacy fallback is not reachable by merely omitting this
        # field: real public import validation rejects it first. Keep this
        # observation separate from any HTTP success/Go parity count.
        with tempfile.TemporaryDirectory(prefix='routerdiff-legacy-audit-') as directory:
            root = Path(directory)
            options = root / 'options.json'
            options.write_text(json.dumps({'system': {'agents': [{
                'id': 'legacy-missing-profile', 'name': '合成旧 Agent', 'service_type': 'oxidns',
                'host': '10.44.0.53', 'port': 18083, 'token': 'synthetic-long-legacy-token-0123456789'}]}}))
            result = subprocess.run([sys.executable, str(Path(__file__).with_name('seed.py')), str(root / 'data'),
                '--options', str(options)], cwd=ROOT, env={**os.environ, 'PYTHONDONTWRITEBYTECODE': '1'},
                capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn('ProfileValidationError', result.stderr)
            self.assertIn('绑定的配置不存在', result.stderr)

    def test_invalid_notes_are_refused_by_real_public_import_before_http(self):
        invalid = [None, [], 'notes', 1, False,
                   {'10.44.0.21': 'first', '10.44.0.21/32': 'same canonical address'},
                   {'::ffff:10.44.0.21': 'first', '10.44.0.21': 'same canonical address'},
                   {'10.47.' + str(number // 256) + '.' + str(number % 256): 'note'
                    for number in range(2049)}]
        for number, notes in enumerate(invalid):
            with self.subTest(variant=number), tempfile.TemporaryDirectory(prefix='routerdiff-notes-import-') as directory:
                root = Path(directory)
                options = root / 'options.json'
                options.write_text(json.dumps({'alpha': {'device_notes': notes}}))
                result = subprocess.run([sys.executable, str(Path(__file__).with_name('seed.py')), str(root / 'data'),
                    '--options', str(options)], cwd=ROOT, env={**os.environ, 'PYTHONDONTWRITEBYTECODE': '1'},
                    capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('ProfileValidationError', result.stderr)
                self.assertIn('设备备注地址重复' if number in (5, 6) else '设备备注必须为对象', result.stderr)

    def test_public_python_import_and_independent_snapshot(self):
        with tempfile.TemporaryDirectory(prefix='routerdiff-seed-test-') as directory:
            data = Path(directory) / 'data'
            result = subprocess.run([sys.executable, str(Path(__file__).with_name('seed.py')), str(data)],
                cwd=ROOT, env={**os.environ, 'PYTHONDONTWRITEBYTECODE': '1'}, capture_output=True, text=True, check=True)
            self.assertEqual(json.loads(result.stdout), {'profiles': ['alpha', 'beta'], 'default_profile': 'alpha'})
            with sqlite3.connect(data / 'configflow.sqlite3') as db:
                fields = {(s, o, f): json.loads(v) for s, o, f, v in db.execute('SELECT scope,owner,field,value FROM fields')}
            self.assertEqual(fields['system', '', 'agents'], [])
            self.assertFalse(fields['system', '', 'backup']['auto_backup'])
            for profile in ('alpha', 'beta'):
                self.assertEqual(fields['profile', profile, 'routers'], {'items': []})
                self.assertEqual(fields['profile', profile, 'rule_configs'], [])
                self.assertEqual(fields['profile', profile, 'device_notes'], {})
            self.assertEqual(fields['shared', '', 'rule_library'], [])


if __name__ == '__main__':
    unittest.main()
