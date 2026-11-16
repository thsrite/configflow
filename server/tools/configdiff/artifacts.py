"""Validate artifact bytes before projecting derived sizes/digests for comparison.

Every digest/size is checked against its own actual member first. The projection
then compares full parsed members and all remaining metadata, so changing bytes
and recomputing the digest cannot make a changed configuration compare equal.
"""
import copy
import hashlib
import io
import json
from pathlib import Path
import re
import tarfile

from compare import Difference, differences, files, structure
from harness import DesignError


class ArtifactError(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise ArtifactError(message)


def validate(directory, key, start, end):
    require(isinstance(key, str) and re.fullmatch('[a-f0-9]{32}', key), 'invalid preview id')
    folder = Path(directory) / '.generated-previews' / key
    record = json.loads((folder / 'record.json').read_bytes())
    archive = (folder / 'bundle.tar.gz').read_bytes()
    require(record['id'] == key and re.fullmatch('[a-f0-9]{32}', record['deployment_id']), 'record identity')
    require(type(record['expires_at']) is int and start + 3598 <= record['expires_at'] <= end + 3602, 'TTL')
    meta = record['bundle_meta']
    require(meta['sha256'] == hashlib.sha256(archive).hexdigest(), 'archive digest')
    with tarfile.open(fileobj=io.BytesIO(archive)) as tar:
        members = tar.getmembers()
        require(len({m.name for m in members}) == len(members), 'duplicate member')
        require(all(m.isfile() and m.mode == 0o600 for m in members), 'member kind/mode')
        raw = {m.name: tar.extractfile(m).read() for m in members}
    manifest = json.loads(raw['manifest.json'])
    require(manifest == meta['manifest'], 'manifest differs from record')
    require(manifest['deployment_id'] == record['deployment_id'] and manifest['agent_id'] == record['agent_id']
            and manifest['profile_id'] == record['profile_id'] and manifest['service_type'] == record['service_type'], 'manifest identity')
    items = manifest['files']
    require(len({item['path'] for item in items}) == len(items), 'duplicate manifest path')
    require(set(raw) == {'manifest.json'} | {'files/' + item['path'] for item in items}, 'member closure')
    parsed = {}
    for item in items:
        path = item['path']
        require(not path.startswith('/') and '..' not in Path(path).parts and '\\' not in path, 'unsafe member path')
        value = raw['files/' + path]
        require(type(item['size']) is int and item['size'] == len(value), 'member size')
        require(item['sha256'] == hashlib.sha256(value).hexdigest(), 'member digest')
        parsed[path] = structure(value, filename=path)
    main = manifest['config_path']
    require(main in parsed, 'missing config path')
    require(meta['config_version'] == hashlib.sha256(raw['files/' + main]).hexdigest()[:8], 'config version')
    normalized = copy.deepcopy(record)
    normalized['bundle_meta']['sha256'] = {'verified_digest_of': 'bundle.tar.gz'}
    normalized['bundle_meta']['config_version'] = {'verified_digest_prefix_of': main, 'length': 8}
    for item in normalized['bundle_meta']['manifest']['files']:
        item['size'] = {'verified_length_of': item['path']}
        item['sha256'] = {'verified_digest_of': item['path']}
    # File permissions and any unexpected private files remain observable.
    modes = {p.relative_to(folder).as_posix(): p.stat().st_mode & 0o777 for p in folder.rglob('*') if p.is_file()}
    extras = {p.relative_to(folder).as_posix(): structure(p.read_bytes(), filename=p.name)
              for p in folder.rglob('*') if p.is_file() and p.name not in ('record.json', 'bundle.tar.gz')}
    return {'record': record, 'raw': raw, 'projection': {'record': normalized, 'members': parsed,
            'modes': modes, 'unexpected_files': extras}}


class Observer:
    def __init__(self, pair):
        self.pair = pair
        self.checked = [None, None]

    def reference(self, reply, *, secret=None):
        value = reply.json()
        try:
            checked = validate(self.pair.dirs[0], value.get('id'), self.pair.policy.start, __import__('time').time())
            self.checked[0] = checked
            self.public(value, 0)
            if secret:
                require(secret in value['content'], 'X01 artifact content exemption absent')
        except (ArtifactError, ValueError, KeyError, OSError, tarfile.TarError) as error:
            raise DesignError('Python artifact fixture invalid: ' + str(error)) from error

    def capture(self, replies, policy):
        self.checked[0] = validate(self.pair.dirs[0], replies[0].json()['id'], policy.start, policy.end)
        try:
            self.checked[1] = validate(self.pair.dirs[1], replies[1].json().get('id'), policy.start, policy.end)
            for field in ('id', 'deployment_id'):
                policy.bind(*[r['record'][field] for r in self.checked], r'[a-f0-9]{32}', 'artifact-' + field)
            policy.epochs['body.expires_at'] = 3600
            # Compare every private artifact, not just the most recent response.
            snapshots = []
            for index, directory in enumerate(self.pair.dirs):
                ordinary = {k: v for k, v in files(directory).items() if not k.startswith('.generated-previews/')}
                private = {}
                for folder in (directory / '.generated-previews').iterdir():
                    if folder.is_dir():
                        private[folder.name] = validate(directory, folder.name, policy.start, policy.end)['projection']
                        policy.epochs['artifact.files.private.' + folder.name + '.record.expires_at'] = 3600
                    elif folder.name != 'lock':
                        private[folder.name] = structure(folder.read_bytes(), filename=folder.name)
                snapshots.append({'ordinary': ordinary, 'private': private})
            return differences(*snapshots, path='artifact.files', policy=policy)
        except (ArtifactError, ValueError, KeyError, OSError, tarfile.TarError) as error:
            self.checked[1] = None
            return [Difference('artifact', 'invalid-Go-artifact', '<validated>', str(error))]

    def public(self, value, index):
        if self.checked[index] is None:
            return value
        checked = self.checked[index]
        record, raw = checked['record'], checked['raw']
        manifest = record['bundle_meta']['manifest']
        require(all(value.get(k) == record[k] for k in ('id', 'deployment_id', 'agent_id', 'profile_id', 'service_type', 'expires_at')), 'public identity')
        require(value['config_path'] == manifest['config_path'], 'public main path')
        require(value['content'].encode() == raw['files/' + manifest['config_path']], 'public content')
        require(value['files'] == manifest['files'], 'public file metadata')
        result = copy.deepcopy(value)
        for item in result['files']:
            item['size'] = {'verified_length_of': item['path']}
            item['sha256'] = {'verified_digest_of': item['path']}
        # Main content is YAML/INI and receives the same structural comparator.
        result['content'] = structure(value['content'].encode(), filename=value['config_path'])
        return result
