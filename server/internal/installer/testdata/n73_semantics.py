"""Enrich hash-only legacy records with independent structural artifact hashes.

Run on the test server from repository root. Every reconstructed value must
match its old recorded hash before any new semantic reference is emitted.
"""
import base64
import copy
import gzip
import hashlib
import io
import json
from pathlib import Path
import zipfile
import yaml
from backend.agents.docker_ha_guide import generate_guide
from backend.agents.docker_ha_compose import generate_bundle

ROOT = Path('server/internal/installer/testdata')
refs = {}

def packed(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':')).encode()

def digest(value):
    return hashlib.sha256(packed(value)).hexdigest()

def zip_value(encoded):
    with zipfile.ZipFile(io.BytesIO(base64.b64decode(encoded))) as archive:
        result = {}
        for entry in archive.infolist():
            raw = archive.read(entry)
            result[entry.filename] = {'mode': entry.external_attr >> 16,
                'yaml': yaml.safe_load(raw) if entry.filename == 'compose.yaml' else None,
                'bytes': None if entry.filename == 'compose.yaml' else hashlib.sha256(raw).hexdigest()}
        return result

def remember_zip(encoded):
    manifest = zip_value(encoded)
    semantic = digest(manifest)
    raw = base64.b64decode(encoded)
    refs['zip:' + hashlib.sha256(raw).hexdigest()] = semantic
    refs['zip:' + hashlib.sha256(encoded.encode()).hexdigest()] = semantic
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        compose = archive.read('compose.yaml').decode()
    for identity in (hashlib.sha256(raw).hexdigest(), hashlib.sha256(encoded.encode()).hexdigest()):
        refs['manifest:' + identity] = packed(manifest).decode()
        refs['yaml:' + identity] = compose

def semantic(value, key):
    if key == 'compose' and isinstance(value, str):
        return yaml.safe_load(value)
    if isinstance(value, dict):
        result = {k: semantic(v, k) for k, v in value.items()}
        if 'content_base64' in value:
            remember_zip(value['content_base64'])
            result['content_base64'] = zip_value(value['content_base64'])
        return result
    if isinstance(value, list):
        return [semantic(v, '') for v in value]
    return value

for path in sorted(ROOT.glob('docker_ha*.jsonl.gz')):
    with gzip.open(path, 'rt') as stream:
        for line in stream:
            row = json.loads(line)
            if row.get('kind') == 'guide' and row.get('ok') and 'digests' in row:
                args = row['input']
                value = generate_guide(copy.deepcopy(args['request']), enrollment=copy.deepcopy(args.get('enrollment')))
                for key, old_hash in row['digests'].items():
                    if digest(value[key])[:24] != old_hash:
                        raise RuntimeError(f'legacy guide reference changed: {path.name}/{row["id"]}/{key}')
                    refs[key + ':' + old_hash] = digest(semantic(value[key], key))
            elif row.get('fn') == 'generate_guide' and 'out' in row:
                old = row['out']
                if isinstance(old, dict) and old.get('compose_bundle', {}).get('content_sha256'):
                    value = generate_guide(*copy.deepcopy(row['args']))
                    encoded = value['compose_bundle']['content_base64']
                    if hashlib.sha256(base64.b64decode(encoded)).hexdigest() != old['compose_bundle']['content_sha256']:
                        raise RuntimeError('recorded guide zip reference changed')
                    remember_zip(encoded)
            elif row.get('fn') == 'generate_bundle' and 'out' in row:
                info = row['out'][1]
                if info.get('content_base64'):
                    remember_zip(info['content_base64'])
                elif info.get('content_sha256'):
                    _, value = generate_bundle(*copy.deepcopy(row['args']))
                    encoded = value['content_base64']
                    if hashlib.sha256(encoded.encode()).hexdigest() != info['content_sha256']:
                        raise RuntimeError('recorded compose zip reference changed')
                    remember_zip(encoded)

with gzip.GzipFile(filename=str(ROOT / 'n73_semantics.json.gz'), mode='wb', mtime=0) as output:
    output.write(packed(refs))
print(f'independent semantic artifact references: {len(refs)}')
