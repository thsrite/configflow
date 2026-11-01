"""Private, expiring generated artifacts. Publication never rerenders these bytes."""
from copy import deepcopy
import hashlib
import io
import json
from pathlib import Path
import re
import shutil
import time
import uuid
import zipfile

from .deployment_bundle import DeploymentBundle, _pack, retarget_config_path
from .rule_sync_store import _write, bundle_files

TTL = 3600
MAX_PREVIEWS = 24
MAX_BYTES = 512 * 1024 * 1024


class PreviewChanged(ValueError):
    pass


def agent_identity(agent):
    from backend.utils.proxy_server_dns import reported_core_version
    return {**{key: agent.get(key) for key in (
        'id', 'profile_id', 'service_type', 'host', 'port', 'token', 'enabled',
        'version', 'deployment_method', 'domain_discovery_enabled', 'oxidns_ha')},
        'core_version': reported_core_version(agent)}


def input_digest(config):
    # Agent heartbeats/publication history are not configuration edits.
    value = {key: item for key, item in config.items() if key not in
             ('agents', '_revision', 'updated_at', 'created_at', 'profile_name', 'name', 'description', 'backup')}
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False).encode()).hexdigest()


def finalize_bundle(manager, agent, bundle, config):
    """Include target-side adaptation before showing any generated configuration."""
    capability = manager._deployment_request(agent, 'get', 'capabilities', timeout=5)
    if (1 not in capability.get('final_config_protocols', [])
            or capability.get('service_type') != agent['service_type']):
        raise ValueError('请先更新目标 Agent，再生成最终下发配置。')
    bundle = retarget_config_path(bundle, capability.get('config_path', 'config.yaml'))
    comparison = next(deepcopy(item) for item in bundle.manifest['files'] if item['role'] == 'config')
    if agent['service_type'] == 'mihomo':
        from .mihomo_resources import prepare_mihomo_resources
        bundle = prepare_mihomo_resources(bundle, capability, config)
    files = bundle_files(bundle)
    path = bundle.manifest['config_path']
    rendered = manager._deployment_request(agent, 'post', 'config/final-preview',
        json={'content': files[path][0].decode('utf-8')}, timeout=30)
    if rendered.get('success') is not True or not isinstance(rendered.get('content'), str):
        raise ValueError(rendered.get('message') or '设备无法生成最终配置，请检查 Agent 状态。')
    content = rendered['content'].encode('utf-8')
    files[path] = (content, 'config')
    manifest = deepcopy(bundle.manifest)
    manifest['final_config'] = True
    for item in manifest['files']:
        if item['role'] == 'config':
            item.update(size=len(content), sha256=hashlib.sha256(content).hexdigest())
    return _pack(manifest, files, hashlib.sha256(content).hexdigest()[:8], comparison_config=comparison)


def save(repository, agent, config, revision, base_url, prepared, *, members):
    root = Path(repository.data_dir) / '.generated-previews'
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Bound private artifacts across workers and restarts. A missing/evicted
    # preview always requires regeneration, never fallback to a fresh render.
    import fcntl
    with (root / 'lock').open('a+b') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        folders = sorted((p for p in root.iterdir() if p.is_dir()), key=lambda p: p.stat().st_mtime)
        total = sum(f.stat().st_size for p in folders for f in p.iterdir() if f.is_file())
        incoming = len(prepared['bundle'].archive) if prepared.get('bundle') else len(prepared['config_content'].encode())
        while folders and (len(folders) >= MAX_PREVIEWS or total + incoming > MAX_BYTES
                           or folders[0].stat().st_mtime < time.time() - TTL):
            old = folders.pop(0)
            total -= sum(f.stat().st_size for f in old.iterdir() if f.is_file())
            shutil.rmtree(old)
        key = uuid.uuid4().hex
        folder = root / key
        folder.mkdir(mode=0o700)
        bundle = prepared.get('bundle')
        source = deepcopy(config)
        source.pop('agents', None)
        record = {'id': key, 'expires_at': int(time.time()) + TTL,
            'agent_id': agent['id'], 'profile_id': config['profile_id'], 'service_type': agent['service_type'],
            'identities': {member['id']: agent_identity(member) for member in members},
            'config_revision': revision, 'input_digest': input_digest(config), 'source': source,
            'base_url': base_url, 'deployment_id': bundle.manifest['deployment_id'] if bundle else uuid.uuid4().hex,
            'config_content': prepared['config_content'],
            'prepared_providers': prepared.get('prepared_providers', [])}
        if bundle:
            record['bundle_meta'] = {'manifest': bundle.manifest, 'sha256': bundle.sha256,
                'config_version': bundle.config_version, 'comparison_config': bundle.comparison_config}
            _write(folder / 'bundle.tar.gz', bundle.archive)
        _write(folder / 'record.json', json.dumps(record, ensure_ascii=False).encode())
    return load(repository, key, agent['id'])


def load(repository, key, agent_id):
    if not isinstance(key, str) or not re.fullmatch(r'[a-f0-9]{32}', key):
        raise PreviewChanged('生成结果无效，请重新生成。')
    folder = Path(repository.data_dir) / '.generated-previews' / key
    try:
        record = json.loads((folder / 'record.json').read_bytes())
        if record['agent_id'] != agent_id or record['expires_at'] <= time.time():
            raise ValueError('stale')
        if meta := record.get('bundle_meta'):
            archive = (folder / 'bundle.tar.gz').read_bytes()
            if hashlib.sha256(archive).hexdigest() != meta['sha256']:
                raise ValueError('digest')
            record['bundle'] = DeploymentBundle(meta['manifest'], archive, meta['sha256'],
                meta['config_version'], meta.get('comparison_config'))
        return record
    except (OSError, ValueError, KeyError, TypeError):
        raise PreviewChanged('生成结果已过期或不可用，请重新生成后推送。') from None


def require_current(manager, record):
    from backend.common.config_repository import ProfileRepositoryError
    try:
        config, revision = manager.repository.get_publish_snapshot(record['profile_id'])
    except ProfileRepositoryError:
        raise PreviewChanged('生成时的配置空间已不可用，请重新选择并生成。') from None
    if revision != record['config_revision'] or input_digest(config) != record['input_digest']:
        raise PreviewChanged('配置已改变，请重新生成并检查后推送。')
    for key, identity in record['identities'].items():
        current = manager.get_agent_by_id(key)
        if not current or agent_identity(current) != identity:
            raise PreviewChanged('目标设备或绑定已改变，请重新生成并检查后推送。')


def public(record):
    bundle = record.get('bundle')
    files = bundle_files(bundle) if bundle else {'surge.conf': (record['config_content'].encode(), 'config')}
    path = bundle.manifest['config_path'] if bundle else 'surge.conf'
    return {key: record[key] for key in ('id', 'deployment_id', 'agent_id', 'profile_id', 'service_type', 'expires_at')} | {
        'content': files[path][0].decode('utf-8'), 'config_path': path,
        'files': [{ 'path': name, 'size': len(value), 'sha256': hashlib.sha256(value).hexdigest(), 'role': role}
                  for name, (value, role) in files.items()],
        'target_ids': list(record['identities'])}


def download(record):
    files = bundle_files(record['bundle']) if record.get('bundle') else {
        'surge.conf': (record['config_content'].encode(), 'config')}
    result = io.BytesIO()
    with zipfile.ZipFile(result, 'w', zipfile.ZIP_DEFLATED) as archive:
        for path, (value, _) in files.items():
            archive.writestr(path, value)
    result.seek(0)
    return result
