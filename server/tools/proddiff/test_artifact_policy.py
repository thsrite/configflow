"""Synthetic proof controls for frozen artifact comparison; server-only."""
import copy
import hashlib
import io
import json
import tarfile
import tempfile
import time
import unittest
from pathlib import Path

from test_proddiff import pd, reply

ROUTE = '/api/generate/<service>/artifact'


def fixture(root, identity, content, auxiliary=b'example.com\n'):
    sha = lambda raw: hashlib.sha256(raw).hexdigest()
    data = {'config.yaml': content, 'rules/example.txt': auxiliary}
    manifest = dict(protocol_version=1, deployment_id=identity[::-1], agent_id='manual',
                    profile_id='default', service_type='mihomo', config_path='config.yaml',
                    files=[dict(path=path, role='config' if path == 'config.yaml' else 'custom',
                                size=len(raw), sha256=sha(raw)) for path, raw in data.items()])
    packed = io.BytesIO()
    with tarfile.open(fileobj=packed, mode='w:gz') as tar:
        for path, raw in [('manifest.json', json.dumps(manifest).encode())] + [('files/' + p, v) for p, v in data.items()]:
            entry = tarfile.TarInfo(path)
            entry.size = len(raw)
            tar.addfile(entry, io.BytesIO(raw))
    archive = packed.getvalue()
    public = dict(success=True, id=identity, deployment_id=manifest['deployment_id'],
                  agent_id='manual', profile_id='default', service_type='mihomo',
                  expires_at=int(time.time()) + 3600, content=content.decode(),
                  config_path='config.yaml', files=manifest['files'], target_ids=[],
                  subscription_hosts=None, subscription_dialers=None)
    record = {key: public[key] for key in ('id', 'deployment_id', 'agent_id', 'profile_id', 'service_type', 'expires_at')}
    record['bundle_meta'] = dict(manifest=manifest, sha256=sha(archive))
    folder = Path(root) / '.generated-previews' / identity
    folder.mkdir(parents=True)
    (folder / 'record.json').write_text(json.dumps(record))
    (folder / 'bundle.tar.gz').write_bytes(archive)
    return public, folder


class ArtifactProofTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.roots = [Path(self.temp.name) / name for name in ('python', 'go')]
        self.a, self.afolder = fixture(self.roots[0], 'a' * 31 + '1', b'dns: {enable: true}\n')
        self.b, self.bfolder = fixture(self.roots[1], 'b' * 31 + '2', b'dns:\n  enable: true\n')

    def compare(self, a=None, b=None, route=ROUTE):
        proofs = pd.ArtifactProofs(self.roots, pd.parsed)
        return pd.response_diffs(reply(a or self.a), reply(b or self.b), route, [], artifact_proofs=proofs)

    def test_semantic_content_with_proven_independent_ids_and_hashes(self):
        self.assertEqual([], self.compare())
        self.assertTrue(self.compare(route='/api/other'))

    def test_public_digest_cannot_be_normalized_without_actual_bytes(self):
        changed = copy.deepcopy(self.b)
        changed['files'][0]['sha256'] = '0' * 64
        self.assertIn(('body.files', 'invalid-artifact-proof-go'), self.compare(b=changed))

    def test_foreign_expired_and_corrupt_records_fail_proof(self):
        record_path = self.bfolder / 'record.json'
        original = record_path.read_bytes()
        for key, value in [('agent_id', 'foreign'), ('expires_at', 1)]:
            record = json.loads(original)
            record[key] = value
            record_path.write_text(json.dumps(record))
            self.assertIn(('body.files', 'invalid-artifact-proof-go'), self.compare())
        record_path.write_bytes(original)
        (self.bfolder / 'bundle.tar.gz').write_bytes(b'corrupt')
        self.assertIn(('body.files', 'invalid-artifact-proof-go'), self.compare())

    def test_auxiliary_bytes_are_compared_after_integrity_validation(self):
        import shutil
        shutil.rmtree(self.bfolder)
        self.b, _ = fixture(self.roots[1], self.b['id'], b'dns:\n  enable: true\n', b'changed.example\n')
        diffs = self.compare()
        self.assertTrue(diffs)
        self.assertFalse(any('invalid-artifact-proof' in kind for _, kind in diffs))

    def test_cached_proof_rejects_an_unstable_public_record(self):
        proofs = pd.ArtifactProofs(self.roots, pd.parsed)
        proofs.prove(1, self.b)
        changed = dict(self.b, content='foreign')
        with self.assertRaises(ValueError):
            proofs.prove(1, changed)


if __name__ == '__main__':
    unittest.main()
