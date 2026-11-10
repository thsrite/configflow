"""Private, immutable publication receipts used to isolate automatic rule edits."""
from copy import deepcopy
import hashlib
import io
import json
import os
import re
from pathlib import Path
import tarfile
import tempfile

from .deployment_bundle import DeploymentBundle, deployment_content_digests, deployment_content_fingerprint


def root_for(repository, agent_id):
    root = Path(repository.data_dir) / '.agent-rule-sync' / hashlib.sha256(agent_id.encode()).hexdigest()
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    return root


def _receipt(repository, agent_id, deployment_id):
    return root_for(repository, agent_id) / hashlib.sha256(deployment_id.encode()).hexdigest()


def _write(path, value):
    descriptor, temporary = tempfile.mkstemp(prefix='.write-', dir=path.parent)
    try:
        with os.fdopen(descriptor, 'wb') as stream:
            stream.write(value)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def capture(repository, agent_id, bundle, source, base_url, *, comparison_config=None):
    """Prepare before upload; only a confirmed applied_content pointer enrolls it."""
    deployment_content_fingerprint(bundle.manifest)
    folder = _receipt(repository, agent_id, bundle.manifest['deployment_id'])
    folder.mkdir(mode=0o700, exist_ok=True)
    source = deepcopy(source)
    # Agent credentials/history are unrelated to rendering any core's rules.
    source.pop('agents', None)
    config_entry = next(item for item in bundle.manifest['files'] if item['role'] == 'config')
    config_digest = config_entry['sha256']
    record = {'source': source, 'manifest': bundle.manifest, 'sha256': bundle.sha256,
              'config_version': config_digest, 'base_url': base_url,
              'comparison_config': deepcopy(comparison_config or config_entry)}
    _write(folder / f'bundle-{bundle.sha256}.tar.gz', bundle.archive)
    _write(folder / 'receipt.json', json.dumps(record, ensure_ascii=False).encode())
    archives = sorted(folder.glob('bundle-*.tar.gz'), key=lambda item: item.stat().st_mtime, reverse=True)
    for old in archives[2:]:
        old.unlink(missing_ok=True)
    # Keep the current applied receipt and in-flight publication. Files are
    # private, bounded to the same retained deployment history as the manager.
    agent = next((item for item in repository.get_system().get('agents', []) if item['id'] == agent_id), {})
    retained = {hashlib.sha256(str(value).encode()).hexdigest() for value in
                [bundle.manifest['deployment_id'], (agent.get('applied_content') or {}).get('deployment_id', ''),
                 *(key for key, value in agent.get('deployments', {}).items()
                   if value.get('status') not in ('succeeded', 'failed', 'rolled_back'))]}
    import shutil
    for child in folder.parent.iterdir():
        if child.is_dir() and child.name not in retained:
            shutil.rmtree(child)


def stamp(repository, agent):
    """Identity of the receipt file load() would read, or None without one.

    Receipts are replaced atomically, so the same stamp means the same bytes.
    """
    applied = agent.get('applied_content') or {}
    if not applied.get('deployment_id'):
        return None
    try:
        stat = (_receipt(repository, agent['id'], applied['deployment_id']) / 'receipt.json').stat()
    except OSError:
        return None
    return stat.st_ino, stat.st_size, stat.st_mtime_ns


def load(repository, agent, *, with_bundle=True):
    applied = agent.get('applied_content') or {}
    if (not applied.get('deployment_id') or applied.get('profile_id') != agent.get('profile_id', 'default')
            or applied.get('service_type') != agent.get('service_type')
            or applied.get('config_version') != agent.get('config_version')):
        return None
    folder = _receipt(repository, agent['id'], applied['deployment_id'])
    try:
        record = json.loads((folder / 'receipt.json').read_bytes())
        if (record['manifest']['deployment_id'] != applied['deployment_id']
                or record['manifest']['profile_id'] != applied['profile_id']
                or record['manifest']['service_type'] != applied['service_type']
                or not re.fullmatch(r'(?:[0-9a-f]{8}|[0-9a-f]{64})', applied['config_version'])):
            return None
        if with_bundle:
            archive = (folder / f'bundle-{record["sha256"]}.tar.gz').read_bytes()
            if hashlib.sha256(archive).hexdigest() != record['sha256']:
                return None
            record['bundle'] = DeploymentBundle(record['manifest'], archive, record['sha256'], record['config_version'])
        return record
    except (OSError, ValueError, KeyError, TypeError):
        return None


def advance_content(repository, agent, source, bundle):
    """Keep real live bytes after refresh; never pin stale rule files later."""
    record = load(repository, agent)
    if record is None:
        return False
    comparison = record.get('comparison_config')
    capture(repository, agent['id'], bundle, source, record.get('base_url', ''), comparison_config=comparison)
    outcome = {'accepted': False}
    def acknowledge(system):
        current = next((item for item in system.get('agents', []) if item['id'] == agent['id']), None)
        if current is None or current.get('applied_content') != agent.get('applied_content'):
            return
        latest = current.get('latest_deployment') or {}
        if latest and latest.get('status') not in ('succeeded', 'failed', 'rolled_back'):
            return
        # Verified full-file digests can advance the comparison baseline without
        # accepting any unrelated source drafts as deployed.
        # Agent AIO/gateway adaptation can change DNS ports/YAML formatting.
        # For desired-file comparison keep the original server main digest;
        # actual live bytes remain in the private receipt for later isolation.
        comparison_manifest = deepcopy(bundle.manifest)
        if comparison:
            comparison_manifest['files'] = [comparison if item['role'] == 'config' else item
                                            for item in comparison_manifest['files']]
        fingerprint = deployment_content_fingerprint(comparison_manifest)
        files = deployment_content_digests(comparison_manifest)
        current['applied_content'].update(fingerprint=fingerprint, files=files)
        deployment_id = current['applied_content']['deployment_id']
        for state in (current.get('deployments', {}).get(deployment_id), current.get('latest_deployment')):
            if state and state.get('deployment_id') == deployment_id and state.get('prepared_content'):
                state['prepared_content'].update(fingerprint=fingerprint, files=files)
        outcome['accepted'] = True
    repository.update_system_transaction(acknowledge)
    return outcome['accepted']


def bundle_files(bundle):
    with tarfile.open(fileobj=io.BytesIO(bundle.archive), mode='r:gz') as archive:
        return {item['path']: (archive.extractfile('files/' + item['path']).read(), item['role'])
                for item in bundle.manifest['files']}
