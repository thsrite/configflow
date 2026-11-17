"""Child-error propagation and actual EACCES while saving private previews."""
import copy
import hashlib
import os

from artifacts import Observer
from compare import database, differences, files, response_diffs
from harness import Pair, send
from cases.setup import Stable
from cases.subscriptions import require


class Propagation(Stable):
    def __init__(self, pair, child):
        super().__init__(pair)
        self.child, self.observed = child, [None, None]

    def before(self, index):
        super().before(index)
        self.original = send(self.pair.bases[index], 'POST', self.child, '[1]',
            {'Content-Type': 'application/json', 'Authorization': 'Bearer ' + self.pair.tokens[index],
             'X-ConfigFlow-Profile': 'default'})
        if index == 0:
            require(self.original.status == 500, 'real child generation returns an error before any generated write')

    def after(self, index, reply):
        super().after(index, reply)
        self.observed[index] = [vars(item) for item in response_diffs(self.original, reply, method='POST')]
        if index == 0:
            require(not self.observed[0], 'artifact forwards the complete own child error status/headers/body unchanged')

    def compare(self, policy):
        return super().compare(policy) + differences(*self.observed, 'artifact.child-propagation', policy)


def child_errors(binary, recorder):
    for scoped in (False, True):
        prefix = '/api/profiles/default/generate' if scoped else '/api/generate'
        template = '/api/profiles/<profile_id>/generate' if scoped else '/api/generate'
        with Pair(binary, recorder) as pair:
            child = 'POST ' + template + '/mihomo'
            if not pair.route_available(child):
                pair.unimplemented_mutations.add(child)
            pair.request('R126-F09' if scoped else 'R062-F09', 'POST ' + template + '/<service>/artifact',
                prefix + '/mihomo/artifact', body='[1]', expect=500, writes=True,
                interaction=Propagation(pair, prefix + '/mihomo'), variant='own-child-full-error-propagated-without-preview')


def private_hashes(root):
    # The existing artifact comparator also excludes only this runtime lock;
    # every record/archive/extra file remains observable.
    return {path.relative_to(root).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in root.rglob('*') if path.is_file() and path.name != 'lock'}


class PreviewFault:
    def __init__(self, pair):
        self.pair, self.observed = pair, [None, None]

    def before(self, index):
        directory = self.pair.dirs[index]
        self.root = directory / '.generated-previews'
        self.root.mkdir(mode=0o700, exist_ok=True)
        self.before_db, self.before_files = database(directory), files(directory)
        self.before_private = private_hashes(self.root)
        self.root.chmod(0o500)
        self.active = not os.access(self.root, os.W_OK)
        if index == 0:
            require(self.active, 'normal user cannot create the next preview inside a real readonly directory')

    def after(self, index, reply):
        directory = self.pair.dirs[index]
        try:
            retained = self.root.is_dir() and self.root.stat().st_mode & 0o777 == 0o500
            same_private = private_hashes(self.root) == self.before_private
        except OSError:
            retained = same_private = False
        finally:
            if self.root.exists():
                self.root.chmod(0o700)
        expected = copy.deepcopy(self.before_files)
        main = 'profiles/default/generated/config.yaml'
        expected[main + '.bak'] = copy.deepcopy(expected.get(main, {'missing-precondition-output': True}))
        self.observed[index] = {'fault_active': self.active, 'fault_mode_retained': retained,
            'old_private_bytes_unchanged': same_private,
            'all_database_rows_unchanged': database(directory) == self.before_db,
            'only_generated_predecessor_backup_added': not differences(expected, files(directory))}
        if index == 0:
            require(all(self.observed[0].values()), 'preview save fails after child output, preserving every private byte/row and adding only actual generated backup: '
                + repr(self.observed[0]) + '; files=' + repr(differences(expected, files(directory))))

    def compare(self, policy):
        return differences(*self.observed, 'artifact.preview-permission', policy)


def save_permissions(binary, recorder):
    for scoped in (False, True):
        prefix = '/api/profiles/default/generate' if scoped else '/api/generate'
        template = '/api/profiles/<profile_id>/generate' if scoped else '/api/generate'
        route = 'POST ' + template + '/<service>/artifact'
        with Pair(binary, recorder) as pair:
            initial = Observer(pair)
            replies = pair.request('T08', route, prefix + '/mihomo/artifact', body='{}', expect=200, writes=True, files=False,
                check=initial.reference, capture=initial.capture, projection=initial.public,
                variant='permission-precondition/' + str(scoped))
            try:
                pair.request('R126-F11' if scoped else 'R062-F11', route, prefix + '/mihomo/artifact', body='{}',
                    expect=500, writes=True, files=False, interaction=PreviewFault(pair),
                    capture=lambda _errors, policy: initial.capture(replies, policy),
                    variant='real-readonly-preview-root-retains-earlier-artifact')
            finally:
                for directory in pair.dirs:
                    root = directory / '.generated-previews'
                    if root.exists():
                        root.chmod(0o700)


GROUPS = {'artifact-child-error-propagation': child_errors, 'artifact-real-save-permissions': save_permissions}
