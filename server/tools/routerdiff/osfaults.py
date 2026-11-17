"""Reversible OS failures restricted to this worker's private DATA_DIR mount."""
import os
from pathlib import Path
import re
import subprocess


class Faults:
    def __init__(self, root):
        self.root = Path(root)
        self.bound = False

    def control(self, action, **values):
        if os.environ.get('ROUTERDIFF_ISOLATED') != '1' or str(self.root) != '/tmp/routerdiff-active/data':
            raise RuntimeError('OS fault controller requires the private worker mount namespace')
        if action == 'readonly':
            enabled = values['enabled']
            if type(enabled) is not bool:
                raise ValueError('read-only mount state must be bool')
            if not self.bound:
                self.mount('--bind', str(self.root), str(self.root))
                self.bound = True
            self.mount('-o', 'remount,bind,' + ('ro' if enabled else 'rw'), str(self.root))
            actual = bool(os.statvfs(self.root).f_flag & os.ST_RDONLY)
            if actual != enabled:
                raise RuntimeError('private read-only fault did not reach the requested OS state')
            return {'read_only': actual}
        if action in ('receipt-directory', 'remove-receipt-directory'):
            relative = values['path']
            if not isinstance(relative, str) or not re.fullmatch(r'router-rule-sync/[0-9a-f]{64}\.json', relative):
                raise ValueError('receipt fault path must be an exact owned synthetic receipt filename')
            flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
            root = os.open(self.root, flags)
            try:
                if action == 'receipt-directory':
                    try:
                        os.mkdir('router-rule-sync', dir_fd=root)
                    except FileExistsError:
                        pass
                directory = os.open('router-rule-sync', flags, dir_fd=root)
                try:
                    name = relative.split('/')[1]
                    if action == 'receipt-directory':
                        os.mkdir(name, mode=0o700, dir_fd=directory)
                    else:
                        os.rmdir(name, dir_fd=directory)  # Empty injected directory only, never remove a real file.
                finally:
                    os.close(directory)
            finally:
                os.close(root)
            return {'injected_directory': action == 'receipt-directory'}
        raise ValueError('unsupported private filesystem fault')

    @staticmethod
    def mount(*arguments):
        result = subprocess.run(['sudo', '-n', 'mount', *arguments], capture_output=True, timeout=10)
        if result.returncode:
            raise RuntimeError('could not change the owned private DATA_DIR mount')
