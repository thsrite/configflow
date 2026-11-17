"""Filesystem comparison sensitivity, including links outside the owned tree."""
import base64
import os
from pathlib import Path
import tempfile
import unittest

from compare import differences
from filesystem import capture


class FilesystemTests(unittest.TestCase):
    def test_exact_exclusions_bytes_modes_links_and_empty_directories(self):
        with tempfile.TemporaryDirectory(prefix='routerdiff-files-') as directory:
            base = Path(directory)
            root = base / 'data'
            root.mkdir()
            outside = base / 'synthetic-outside'
            outside.mkdir()
            (outside / 'secret').write_text('synthetic bytes must not enter snapshot')
            for name in ('configflow.sqlite3', 'configflow.sqlite3-wal', 'app.log', 'job.lock', '.scheduler.lock'):
                (root / name).write_text('excluded')
            nested = root / 'nested'
            nested.mkdir(mode=0o700)
            (nested / 'app.log').write_bytes(b'keep nested log\x00\xff')
            (nested / 'configflow.sqlite3').write_text('not the root database')
            (nested / 'baseline.json').write_text('{"id":"exact"}')
            (nested / 'baseline.json').chmod(0o644)
            (root / 'empty').mkdir()
            (root / 'escape-file').symlink_to(outside / 'secret')
            (root / 'escape-directory').symlink_to(outside, target_is_directory=True)
            (root / 'broken').symlink_to('missing-owned-file')
            os.mkfifo(root / 'pipe', 0o600)
            original = capture(root)
            files, metadata = original
            self.assertEqual(set(files), {'nested/app.log', 'nested/configflow.sqlite3', 'nested/baseline.json'})
            self.assertEqual(base64.b64decode(files['nested/app.log']), b'keep nested log\x00\xff')
            self.assertEqual(metadata['nested'], {'kind': 'directory', 'mode': 0o700})
            self.assertEqual(metadata['escape-file']['kind'], 'symlink')
            self.assertEqual(metadata['escape-file']['target'], str(outside / 'secret'))
            self.assertEqual(metadata['broken']['target'], 'missing-owned-file')
            self.assertEqual(metadata['pipe']['kind'], 'special')
            self.assertIn('empty', metadata)
            self.assertNotIn('escape-directory/secret', metadata)
            self.assertNotIn('secret', str(files))
            for mutate, restore in (
                (lambda: (root / 'empty').rmdir(), lambda: (root / 'empty').mkdir()),
                (lambda: (nested / 'baseline.json').chmod(0o600), lambda: (nested / 'baseline.json').chmod(0o644)),
                (lambda: (nested / 'app.log').write_bytes(b'changed'), lambda: (nested / 'app.log').write_bytes(b'keep nested log\x00\xff')),
            ):
                mutate()
                self.assertTrue(differences(original, capture(root)))
                restore()
            (root / 'broken').unlink()
            (root / 'broken').symlink_to('changed-literal-target')
            self.assertTrue(differences(original, capture(root)))


if __name__ == '__main__':
    unittest.main()
