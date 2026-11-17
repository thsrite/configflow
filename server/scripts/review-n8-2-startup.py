"""Verify safe startup recovery on synthetic corrupt databases only.

Usage: PYTHONPATH=. python server/scripts/review-n8-2-startup.py /tmp/configflow
No private snapshot or real network endpoint is used.
"""
import contextlib
import logging
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile

logging.disable(logging.CRITICAL)
from backend.common.config_repository import ProfileRepository


def main():
    binary = str(Path(sys.argv[1]).resolve())
    secret = 'synthetic-secret-never-in-startup-log'
    cases = {
        'json': ('{"token":"'+secret+'",]').encode(),
        'utf8': ('"'+secret).encode()+b'\xff"',
        'large': ('["'+'x'*(10 << 20)+secret+'"').encode(),
    }
    for name, content in cases.items():
        with tempfile.TemporaryDirectory(prefix='n82-startup-') as tmp:
            directory = Path(tmp)
            with open(os.devnull, 'w') as sink, contextlib.redirect_stdout(sink), contextlib.redirect_stderr(sink):
                ProfileRepository(directory)
            path = directory / 'configflow.sqlite3'
            with sqlite3.connect(path) as connection:
                connection.execute("INSERT INTO fields(scope,owner,field,position,value,revision) VALUES('profile','default','corrupt_future',999,CAST(? AS TEXT),1)", (content,))
            before = path.read_bytes()
            logfile = directory / 'startup.log'
            env = dict(os.environ, DATA_DIR=tmp, LOG_FILE=str(logfile),
                       LOG_LEVEL='CRITICAL', ADMIN_USERNAME='review',
                       ADMIN_PASSWORD='synthetic-admin-password',
                       CONFIGFLOW_BACKEND_HOST='127.0.0.1', CONFIGFLOW_BACKEND_PORT='18080')
            result = subprocess.run([binary], env=env, capture_output=True, timeout=20)
            assert result.returncode == 1, name
            for output in [result.stderr.decode(), logfile.read_text()]:
                assert secret not in output and len(output) < 3000, name
                for part in [str(path), 'fields', 'profile/default/corrupt_future', '字节',
                             '请先停止服务并保留原文件', '从可用备份恢复', '最后一个能正常读取此库的版本']:
                    assert part in output, (name, part)
            assert path.read_bytes() == before, name
    print('3 synthetic corrupt startup cases: exit=1, safe log+stderr, original database bytes unchanged')


if __name__ == '__main__':
    main()
