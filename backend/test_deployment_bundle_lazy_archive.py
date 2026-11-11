"""A bundle made from its files packs the archive on first use.

Comparing content needs the manifest alone; listing changes reads a few files.
The delivered bytes must not depend on when they were packed.
"""
from concurrent.futures import ThreadPoolExecutor
import gzip
import hashlib
import io
import json
import tarfile

import pytest
import yaml

from backend.agents import deployment_bundle
from backend.agents.deployment_bundle import (DeploymentBundle, build_deployment_bundle, deployment_content_fingerprint,
                                              retarget_config_path)
from backend.agents.push_diff import build_push_diff
from backend.test_agent_push_diff import agent_for, base_config, bundle, provider, receipt_for, NODE


def eager_archive(manifest, files):
    """The archive as it was packed when every bundle was packed at once."""
    archive = io.BytesIO()
    with gzip.GzipFile(fileobj=archive, mode='wb', mtime=0) as compressed, \
            tarfile.open(fileobj=compressed, mode='w', format=tarfile.PAX_FORMAT) as tar:
        members = [('manifest.json', json.dumps(manifest, ensure_ascii=False).encode('utf-8'))]
        members.extend(('files/' + path, value) for path, (value, _) in sorted(files.items()))
        for path, value in members:
            member = tarfile.TarInfo(path)
            member.size = len(value)
            member.mode = 0o600
            tar.addfile(member, io.BytesIO(value))
    return archive.getvalue()


@pytest.fixture
def packs(monkeypatch):
    """Every time an archive is compressed."""
    made = []
    real = deployment_bundle._archive_bytes
    monkeypatch.setattr(deployment_bundle, '_archive_bytes', lambda manifest, files: made.append(1) or real(manifest, files))
    return made


def sample(**changes):
    return bundle(base_config(), providers={'Feed': provider([NODE, {**NODE, 'name': 'Node B'}])},
                  rules={'ruleset/Ads.list': '+.ads.example\n', 'ruleset/Same.list': '+.same.example\n'},
                  custom={'extra.txt': 'abc'}, **changes)


def test_building_and_fingerprinting_compress_nothing(packs):
    built = sample()
    assert deployment_content_fingerprint(built.manifest).startswith('v1:')
    assert {item['path'] for item in built.manifest['files']} >= {'config.yaml', 'providers/Feed.yaml', 'extra.txt'}
    assert built.config_version and 'packed=False' in repr(built)
    assert packs == []


def test_the_archive_is_the_same_bytes_whenever_it_is_packed(packs):
    first, second = sample(), sample()
    files = {path: (content, next(item['role'] for item in first.manifest['files'] if item['path'] == path))
             for path, content in first.files().items()}
    assert packs == []
    expected = eager_archive(first.manifest, files)
    assert first.archive == expected and first.sha256 == hashlib.sha256(expected).hexdigest()
    assert packs == [1]
    # Asked in the other order, and again: one compression per bundle.
    assert second.sha256 == first.sha256 and second.archive == expected
    assert first.archive is first.archive and first.sha256 == second.sha256
    assert packs == [1, 1] and 'packed=True' in repr(first)
    with tarfile.open(fileobj=io.BytesIO(first.archive), mode='r:gz') as archive:
        assert json.load(archive.extractfile('manifest.json')) == first.manifest
        assert archive.extractfile('files/extra.txt').read() == b'abc'


def test_files_read_the_same_before_and_after_packing(packs):
    built = sample()
    everything = built.files()
    assert set(everything) == {item['path'] for item in built.manifest['files']}
    assert all(hashlib.sha256(content).hexdigest() == next(
        item['sha256'] for item in built.manifest['files'] if item['path'] == path) for path, content in everything.items())
    assert built.files(['extra.txt', 'missing.txt']) == {'extra.txt': b'abc'} and built.files([]) == {}
    assert packs == []
    built.archive
    assert built.files() == everything and built.files(['extra.txt', 'missing.txt']) == {'extra.txt': b'abc'}
    # A bundle read back from disk has only its archive.
    stored = DeploymentBundle(built.manifest, built.archive, built.sha256, built.config_version)
    assert stored.files() == everything and stored == built and packs == [1]


def test_changing_what_was_passed_in_does_not_change_the_bundle():
    manifest = {'protocol_version': 1, 'service_type': 'mihomo', 'config_path': 'config.yaml',
                'files': [{'path': 'config.yaml', 'size': 3, 'sha256': hashlib.sha256(b'a: 1').hexdigest(), 'role': 'config'}]}
    files = {'config.yaml': (b'a: 1', 'config')}
    built = deployment_bundle._pack(manifest, files, 'v')
    files['config.yaml'] = (b'changed', 'config')
    files['added.txt'] = (b'x', 'custom')
    assert built.files() == {'config.yaml': b'a: 1'}
    assert built.archive == eager_archive(manifest, {'config.yaml': (b'a: 1', 'config')})


def test_retargeting_the_main_file_needs_no_compression_and_packs_the_same(packs):
    built, reference = sample(), sample()
    renamed = retarget_config_path(built, 'installed.yaml')
    assert renamed.manifest['config_path'] == 'installed.yaml' and packs == []
    assert retarget_config_path(built, 'config.yaml') is built
    # The same as retargeting a bundle that was packed first, as every bundle used to be.
    reference.archive
    assert renamed.archive == retarget_config_path(reference, 'installed.yaml').archive
    assert renamed.files()['installed.yaml'] == built.files()['config.yaml']
    assert deployment_content_fingerprint(renamed.manifest) == deployment_content_fingerprint(built.manifest)


def test_listing_changes_reads_files_without_compressing_either_side(packs):
    old = sample()
    new = bundle(base_config(), providers={'Feed': provider([NODE, {**NODE, 'name': 'Node C'}])},
                 rules={'ruleset/Ads.list': '+.ads.example\n+.more.example\n', 'ruleset/Same.list': '+.same.example\n'},
                 custom={'extra.txt': 'abc'})
    result = build_push_diff(agent_for(old), new, None, receipt=receipt_for(old))
    changed = {item['path']: item['status'] for item in result['files']}
    assert changed['ruleset/Ads.list'] == 'modified' and changed['providers/Feed.yaml'] == 'modified'
    assert packs == []
    # The same list from two packed bundles, as before.
    old.archive, new.archive
    assert build_push_diff(agent_for(old), new, None, receipt=receipt_for(old)) == result


def test_concurrent_first_uses_pack_once(packs):
    built = sample()
    with ThreadPoolExecutor(8) as pool:
        results = list(pool.map(lambda number: (built.archive, built.sha256) if number % 2 else (built.sha256, built.archive)[::-1],
                                range(32)))
    assert len(set(results)) == 1 and packs == [1]


def test_bundles_compare_by_what_they_deliver():
    first, second = sample(), sample()
    assert first == second and not first != second
    assert first != bundle(base_config(), custom={'extra.txt': 'other'}) and first != 'not a bundle'
    assert DeploymentBundle(first.manifest, first.archive, '0' * 64, first.config_version) != first
    with pytest.raises(TypeError):
        hash(first)


def test_a_content_check_and_its_change_list_compress_nothing(packs, monkeypatch):
    """The Agent page's comparison: synced or changed, the archive is never needed."""
    from backend.agents.content_sync import ContentSyncChecker
    from backend.test_agent_content_sync import DeferredExecutor
    executor, now = DeferredExecutor(), [100.0]
    checker = ContentSyncChecker(executor=executor, clock=lambda: now[0])
    config = {'profile_id': 'default', 'test_main': 'mode: rule\nrules: []\n', 'test_rule': 'DOMAIN,example.test\n'}
    agent = {'id': 'agent-test', 'profile_id': 'default', 'service_type': 'mihomo', 'config_version': 'applied-config'}

    def prepare(item, snapshot, *, base_url='', deployment_id='previous'):
        return build_deployment_bundle(item, snapshot, snapshot['test_main'],
                                       custom_files=[{'path': 'rules/example.list', 'content': snapshot['test_rule']}],
                                       base_url=base_url, deployment_id=deployment_id)
    previous = prepare(agent, config)
    agent['applied_content'] = {'fingerprint': deployment_content_fingerprint(previous.manifest), 'profile_id': 'default',
                                'service_type': 'mihomo', 'config_version': 'applied-config', 'deployment_id': 'previous',
                                'base_url': '', 'config_path': 'installed.yaml'}
    listed = []
    monkeypatch.setattr('backend.agents.push_diff.build_push_diff',
                        lambda item, candidate, repository, **_: listed.append(candidate.files(['rules/example.list']))
                        or {'status': 'changed', 'files': []})
    assert checker.get_content_sync(agent, config, '', prepare) == {'status': 'checking'}
    executor.run()
    assert checker.get_content_sync(agent, config, '', prepare)['status'] == 'synced'
    config['test_rule'] = 'DOMAIN,changed.example.test\n'
    checker.get_content_sync(agent, config, '', prepare)
    executor.run()
    assert checker.get_content_sync(agent, config, '', prepare)['status'] == 'changed'
    if listed:  # with the change list kept by the comparison itself
        assert listed == [{'rules/example.list': b'DOMAIN,changed.example.test\n'}]
    assert packs == []
