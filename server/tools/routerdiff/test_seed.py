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
