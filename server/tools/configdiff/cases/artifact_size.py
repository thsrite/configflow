"""Reach the real 512 MiB metadata limit through public Surge settings.

An ASCII pre-section comment is ignored by generation but retained in the
preview source metadata. No production limit or serializer is replaced. Large
owned fixture files and request spools live on disk, not /tmp tmpfs.
"""
import copy
from pathlib import Path
import tempfile
import time

from compare import database, differences, files, structure
from downloads import capture_headers
from dependency_tls import require_native_network
from harness import Pair, json_body
from cases.subscriptions import require

LIMIT_MESSAGE = '生成结果超过临时存储上限，请减少配套文件后重试。'
GENERATED = 'profiles/default/generated/config.conf'


class MetadataLimit:
    def __init__(self, pair):
        self.pair, self.observed = pair, [None, None]

    def logs(self, index):
        return [self.pair.dirs[index] / 'app.log', self.pair.root / (str(index) + '.log')]

    def before(self, index):
        directory = self.pair.dirs[index]
        self.before_db, self.before_files = database(directory), files(directory)
        self.previous = (directory / GENERATED).read_bytes() if (directory / GENERATED).is_file() else b''
        self.offsets = [path.stat().st_size if path.exists() else 0 for path in self.logs(index)]

    def after(self, index, reply):
        expected = copy.deepcopy(self.before_files)
        expected[GENERATED + '.bak'] = {'mode': 0o600, 'content': structure(self.previous, filename=GENERATED + '.bak')}
        directory = self.pair.dirs[index]
        deadline = time.monotonic() + 2
        signal = False
        while time.monotonic() < deadline:
            diagnostic = reply.body + b'\n'.join(path.read_bytes()[offset:] if path.exists() else b''
                for path, offset in zip(self.logs(index), self.offsets))
            signal = LIMIT_MESSAGE.encode() in diagnostic
            if signal:
                break
            time.sleep(0.02)
        self.observed[index] = {'real_limit_diagnostic': signal,
            'all_database_rows_unchanged': database(directory) == self.before_db,
            'preview_root_never_created': not (directory / '.generated-previews').exists(),
            'only_actual_generated_backup_added': not differences(expected, files(directory))}
        if index == 0:
            require(all(self.observed[0].values()), 'real metadata bound rejects before private storage while retaining all rows and the actual child output: '
                    + repr(self.observed[0]) + '; files=' + repr(differences(expected, files(directory))))

    def compare(self, policy):
        return differences(*self.observed, 'artifact.real-size-limit', policy)


def metadata_limit(binary, recorder):
    require_native_network()
    disk = Path.home() / '.cache/configdiff-large'
    disk.mkdir(parents=True, exist_ok=True, mode=0o700)
    with tempfile.TemporaryDirectory(prefix='artifact-size-', dir=disk) as parent:
        comment = '#' + 'x' * (512 * 1024 * 1024 + 1) + '\n'
        require(len(comment) > 512 * 1024 * 1024, 'the actual source alone exceeds the unchanged metadata bound')
        options = {'temporary_root': parent, 'environment': {'TMPDIR': parent}, 'agents': [],
            'profile': {'name': 'SyntheticLarge', 'description': 'Synthetic size boundary'},
            'other_profile': {'name': 'SyntheticOther', 'description': 'Synthetic other profile'}}
        with Pair(binary, recorder, options=options) as pair:
            pair.request('R044-S01', 'POST /api/custom-config/surge', body=json_body({'config': comment}),
                expect=200, writes=True, timeout=180, variant='real-size-limit-public-settings-precondition',
                check=lambda reply: require(reply.json()['success'] is True, 'public setter accepts and persists the real large comment'))
            def child(reply):
                require(0 < len(reply.body) < 65536,
                        'normal public Surge generation succeeds and ignores the pre-section comment')
            pair.request('R072-S01', 'POST /api/generate/surge', body='{}', expect=200, writes=True,
                filename='surge.conf', capture=capture_headers, check=child, timeout=180,
                variant='real-size-limit-valid-small-child-precondition')
            for scoped in (False, True):
                route = ('POST /api/profiles/<profile_id>/generate/<service>/artifact' if scoped
                         else 'POST /api/generate/<service>/artifact')
                path = '/api/profiles/default/generate/surge/artifact' if scoped else '/api/generate/surge/artifact'
                pair.request('R126-F10' if scoped else 'R062-F10', route, path, body='{}', expect=500, writes=True,
                    interaction=MetadataLimit(pair), timeout=180, variant='real-512-MiB-metadata-limit-without-patching')


GROUPS = {'artifact-real-metadata-size-boundary': metadata_limit}
