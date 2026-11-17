"""Exact owned DATA_DIR observations without following filesystem links."""
import base64
import os
import stat


DATABASE_AND_LOG = frozenset(('configflow.sqlite3', 'configflow.sqlite3-wal',
    'configflow.sqlite3-shm', 'configflow.sqlite3-journal', 'app.log'))


def capture(root):
    """Keep bytes, permissions, empty directories and literal symlink targets.

    Every traversal uses directory descriptors and O_NOFOLLOW. A concurrent
    replacement must fail the snapshot rather than escape the owned directory.
    SQLite is observed separately through a read-only transaction. Only exact
    root DB/log names and reviewed *.lock basenames are excluded.
    """
    files, metadata = {}, {}
    flags = os.O_RDONLY | os.O_CLOEXEC | os.O_NOFOLLOW

    def walk(directory, prefix):
        for name in sorted(os.listdir(directory)):
            relative = prefix + name
            info = os.stat(name, dir_fd=directory, follow_symlinks=False)
            if (not stat.S_ISDIR(info.st_mode)
                    and (relative in DATABASE_AND_LOG or name.endswith('.lock'))):
                continue
            item = {'mode': stat.S_IMODE(info.st_mode)}
            if stat.S_ISLNK(info.st_mode):
                item.update(kind='symlink', target=os.readlink(name, dir_fd=directory))
            elif stat.S_ISDIR(info.st_mode):
                item['kind'] = 'directory'
                descriptor = os.open(name, flags | os.O_DIRECTORY, dir_fd=directory)
                try:
                    opened = os.fstat(descriptor)
                    if (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
                        raise RuntimeError('owned directory changed while taking snapshot')
                    walk(descriptor, relative + '/')
                finally:
                    os.close(descriptor)
            elif stat.S_ISREG(info.st_mode):
                item['kind'] = 'file'
                descriptor = os.open(name, flags | os.O_NONBLOCK, dir_fd=directory)
                with os.fdopen(descriptor, 'rb') as handle:
                    opened = os.fstat(handle.fileno())
                    if not stat.S_ISREG(opened.st_mode) or (opened.st_dev, opened.st_ino) != (info.st_dev, info.st_ino):
                        raise RuntimeError('owned file changed while taking snapshot')
                    files[relative] = base64.b64encode(handle.read()).decode()
            else:
                # Never open a FIFO/device/socket just to compare a state tree.
                item.update(kind='special', type=stat.S_IFMT(info.st_mode))
            metadata[relative] = item

    descriptor = os.open(root, flags | os.O_DIRECTORY)
    try:
        walk(descriptor, '')
    finally:
        os.close(descriptor)
    return files, metadata
