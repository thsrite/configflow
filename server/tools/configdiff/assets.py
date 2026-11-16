"""Compare managed attachments as original bytes in ZIPs and frozen artifacts."""
import io
import zipfile

from compare import differences
from harness import DesignError


def zip_assets(body):
    with zipfile.ZipFile(io.BytesIO(body)) as archive:
        names = archive.namelist()
        if len(set(names)) != len(names):
            raise ValueError('duplicate managed ZIP member')
        return {name: archive.read(name) for name in names if name != 'config.yaml'}


def raw_asset_diffs(left, right, policy):
    return differences(left, right, 'managed.assets', policy)


class Assets:
    def __init__(self, expected, observer=None):
        self.expected, self.observer = expected, observer

    def read(self, reply, index=0):
        if self.observer:
            raw = self.observer.checked[index]['raw']
            return {key.removeprefix('files/'): value for key, value in raw.items()
                    if key.startswith('files/') and key != 'files/config.yaml'}
        return zip_assets(reply.body)

    def reference(self, reply):
        if self.observer:
            self.observer.reference(reply)
        actual = self.read(reply)
        if actual != self.expected:
            raise DesignError('Python managed-asset closure/bytes differ: expected ' + repr(sorted(self.expected))
                              + ', actual ' + repr(sorted(actual)))
        if any(path.startswith('state/') for path in actual):
            raise DesignError('Python configuration bundle overwrote mutable runtime state')

    def capture(self, replies, policy):
        out = self.observer.capture(replies, policy) if self.observer else []
        try:
            return out + raw_asset_diffs(self.read(replies[0]), self.read(replies[1], 1), policy)
        except (KeyError, TypeError, ValueError, zipfile.BadZipFile) as error:
            from compare import Difference
            return out + [Difference('managed.assets', 'invalid-Go-assets', '<original managed bytes>', str(error))]
