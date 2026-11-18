"""Prove frozen bytes before pairing independent manual-artifact identities.

Only comparison-launcher private copies are read. No output, extraction, product
hooks, route bypass or production data is introduced by this observer.
"""
import copy
import hashlib
import io
import json
import re
import tarfile
import threading
import time
import yaml
from pathlib import Path, PurePosixPath

ROUTES = {'/api/generate/<service>/artifact',
          '/api/profiles/<profile_id>/generate/<service>/artifact'}
ID = re.compile(r'[a-f0-9]{32}')


class ArtifactProofs:
    def __init__(self, roots, parse):
        self.roots = [Path(root) for root in roots]
        self.parse = parse
        self.cache = {}
        self.pairs = {'id': {}, 'deployment_id': {}}
        self.lock = threading.Lock()

    def prove(self, side, body):
        key = body['id']
        if not isinstance(key, str) or not ID.fullmatch(key):
            raise ValueError('invalid preview identity')
        cache_key = (side, key)
        with self.lock:
            cached = self.cache.get(cache_key)
        if cached is not None:
            if cached[0] != body:
                raise ValueError('unstable preview')
            return cached[1]
        if (body['agent_id'] != 'manual' or not body['profile_id'] or body['target_ids'] != []
                or not ID.fullmatch(body['deployment_id'])
                or type(body['expires_at']) is not int
                or not time.time() + 3400 < body['expires_at'] <= time.time() + 3601):
            raise ValueError('invalid artifact ownership or expiration')
        folder = self.roots[side] / '.generated-previews' / key
        if folder.is_symlink() or any((folder / name).is_symlink() for name in ('record.json', 'bundle.tar.gz')):
            raise ValueError('symlink artifact')
        record = json.loads((folder / 'record.json').read_bytes())
        for field in ('id', 'agent_id', 'profile_id', 'service_type', 'deployment_id', 'expires_at'):
            if record[field] != body[field]:
                raise ValueError('response differs from persisted preview')
        meta = record['bundle_meta']
        archive = (folder / 'bundle.tar.gz').read_bytes()
        if hashlib.sha256(archive).hexdigest() != meta['sha256']:
            raise ValueError('archive integrity')
        manifest = meta['manifest']
        if manifest['files'] != body['files'] or manifest['config_path'] != body['config_path']:
            raise ValueError('manifest differs from public view')
        for field in ('agent_id', 'profile_id', 'service_type', 'deployment_id'):
            if manifest[field] != body[field]:
                raise ValueError('manifest ownership')
        data = {}
        with tarfile.open(fileobj=io.BytesIO(archive), mode='r:gz') as tar:
            for member in tar:
                if not member.isfile() or member.name in data or member.size > 64 << 20:
                    raise ValueError('invalid archive member')
                data[member.name] = tar.extractfile(member).read()
        if json.loads(data.pop('manifest.json')) != manifest:
            raise ValueError('archive manifest mismatch')
        if set(data) != {'files/' + f['path'] for f in body['files']}:
            raise ValueError('archive membership mismatch')
        semantic = []
        for file in body['files']:
            path = file['path']
            if (not path or '\\' in path or PurePosixPath(path).is_absolute()
                    or any(part in ('', '.', '..') for part in path.split('/'))):
                raise ValueError('unsafe artifact path')
            raw = data['files/' + path]
            if file['size'] != len(raw) or file['sha256'] != hashlib.sha256(raw).hexdigest():
                raise ValueError('file integrity')
            scrubbed = raw.replace(str(self.roots[side]).encode(), b'<DATA_DIR>')
            if path == body['config_path'] and scrubbed.decode() != body['content']:
                raise ValueError('preview differs from main file')
            semantic.append({'path': path, 'role': file['role'],
                             'content': self.parse(scrubbed, '', '', path),
                             'eol': [b'\r\n' in raw, b'\r' in raw.replace(b'\r\n', b'')]})
        with self.lock:
            self.cache[cache_key] = (copy.deepcopy(body), semantic)
        return semantic

    def normalize(self, a, b):
        if not (isinstance(a, dict) and isinstance(b, dict)
                and a.get('success') is True and b.get('success') is True):
            return []
        proven = []
        for side, body in enumerate((a, b)):
            try:
                proven.append(self.prove(side, body))
            except (ValueError, OSError, KeyError, TypeError, tarfile.TarError, yaml.YAMLError):
                return [('body.files', 'invalid-artifact-proof-' + ('python', 'go')[side])]
        with self.lock:
            for field, pairs in self.pairs.items():
                left, right = a[field], b[field]
                if ((left in pairs and pairs[left] != right)
                        or any(k != left and v == right for k, v in pairs.items())):
                    return [('body.' + field, 'unstable-artifact-identity')]
                pairs[left] = right
        if abs(a['expires_at'] - b['expires_at']) > 120:
            return [('body.expires_at', 'artifact-expiration-skew')]
        for body, files in zip((a, b), proven):
            body['id'] = body['deployment_id'] = '<proven-independent-identity>'
            body['expires_at'] = '<proven-one-hour-expiration>'
            for file in body['files']:
                file['size'] = file['sha256'] = '<verified-frozen-bytes>'
            body['frozen_files'] = files
        return []
