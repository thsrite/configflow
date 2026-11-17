"""Independent public-process pair and typed HTTP observations.

Only the seed subprocess imports the public Python repository fixture API. Every
product operation below crosses an actual TCP HTTP listener.
"""
import base64
import json
import os
from pathlib import Path
import select
import shutil
import subprocess
import sys
import tempfile

from inventory import ROOT
from seed import ADMIN, PASSWORD

HERE = Path(__file__).resolve().parent


def decode_response(response):
    content = base64.b64decode(response['body'])
    try:
        document = json.loads(content)
        kind = 'json'
    except (ValueError, UnicodeError):
        document, kind = content.decode(errors='replace'), 'text'
    return {**response, 'content': document, 'kind': kind}


class Side:
    def __init__(self, name, directory, binary, options):
        self.name, self.directory = name, Path(directory)
        (self.directory / 'options.json').write_text(json.dumps(options))
        self.stderr = (self.directory / 'worker.log').open('wb')
        self.process = subprocess.Popen(['bash', str(HERE / 'namespace.sh'), str(self.directory),
            sys.executable, str(HERE / 'worker.py'), name, str(binary)], cwd=ROOT,
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.stderr, text=True, bufsize=1)
        self.token = None
        self.observations = {}
        self.pending = {}

    def rpc(self, operation, **values):
        self.process.stdin.write(json.dumps({'operation': operation, **values}) + '\n')
        self.process.stdin.flush()
        if not select.select([self.process.stdout], [], [], 100)[0]:
            raise TimeoutError(self.name + ' namespace RPC timed out')
        line = self.process.stdout.readline()
        if not line:
            raise RuntimeError(self.name + ' namespace exited; inspect ' + str(self.directory / 'worker.log'))
        reply = json.loads(line)
        if not reply['ok']:
            raise RuntimeError(self.name + ' RPC: ' + reply['error'] + '\n' + reply.get('traceback', ''))
        return reply['result']

    def request(self, method, path, document=None, raw=None, headers=None, auth=True, **kwargs):
        headers = dict(headers or {})
        if document is not None:
            if raw is not None:
                raise ValueError('choose document or raw body')
            raw = json.dumps(document, ensure_ascii=False, separators=(',', ':')).encode()
            headers.setdefault('Content-Type', 'application/json')
        if auth and self.token:
            headers.setdefault('Authorization', 'Bearer ' + self.token)
        values = dict(method=method, path=path, headers=headers,
                      body=base64.b64encode(raw).decode() if raw is not None else None, **kwargs)
        return decode_response(self.rpc('request', **values))

    def start(self):
        self.rpc('start')
        response = self.request('POST', '/api/auth/login', document={'username': ADMIN, 'password': PASSWORD}, auth=False)
        if response['status'] != 200 or response['content'].get('success') is not True:
            raise RuntimeError(self.name + ' public login failed')
        self.token = response['content']['token']

    def close(self):
        if self.process.poll() is None:
            self.process.stdin.close()
            try:
                self.process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.process.terminate()
                self.process.wait(timeout=5)
        self.process.stdout.close()
        self.stderr.close()


class Pair:
    def __init__(self, binary, options=None, keep=None):
        self.root = Path(keep) if keep else Path(tempfile.mkdtemp(prefix='routerdiff-pair-'))
        self.root.mkdir(parents=True, exist_ok=True)
        self.keep = bool(keep)
        self.sides = {}
        options = options or {}
        seed_dir = self.root / 'seed'
        option_path = self.root / 'options.json'
        option_path.write_text(json.dumps(options))
        try:
            subprocess.run([sys.executable, str(HERE / 'seed.py'), str(seed_dir), '--options', str(option_path)],
                           cwd=ROOT, check=True, stdout=subprocess.DEVNULL)
            for name in ('python', 'go'):
                directory = self.root / name
                directory.mkdir()
                shutil.copytree(seed_dir, directory / 'data')
                side = Side(name, directory, binary, options)
                self.sides[name] = side
                side.start()
        except BaseException:
            self.close()
            raise

    def close(self):
        for side in self.sides.values():
            side.close()
        if not self.keep:
            shutil.rmtree(self.root)

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()

    @property
    def python(self):
        return self.sides['python']

    @property
    def go(self):
        return self.sides['go']
