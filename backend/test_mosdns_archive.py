"""A MosDNS ZIP is complete, profile-scoped, and never downloads itself."""
import io
import threading
import time
from urllib.parse import urlencode
import zipfile

import pytest
import requests
import yaml
from flask import Flask

from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints, generate, mosdns
from backend.utils.mosdns_archive import MAX_WORKERS, MosdnsArchiveError, build_mosdns_zip
from backend.utils.rule_utils import sanitize_rule_name


@pytest.fixture
def repository(tmp_path, monkeypatch):
    repo = ProfileRepository(tmp_path)
    monkeypatch.setattr(config_module, '_repository', repo)
    config_module.reset_config_context()
    repo.create_profile({'id': 'office', 'name': 'Office'})
    repo.save_shared({
        'rule_library': [
            {'id': 'home-rule', 'name': '家庭', 'source_type': 'content', 'behavior': 'domain',
             'content': 'DOMAIN,home.test', 'enabled': True},
            {'id': 'office-rule', 'name': '办公混合', 'source_type': 'content', 'behavior': 'classical',
             'content': 'DOMAIN,office.test\nIP-CIDR,203.0.113.0/24\nIP-CIDR6,2001:db8::/32', 'enabled': True},
        ],
    })
    for profile, source in [('default', 'home-rule'), ('office', 'office-rule')]:
        repo.save_profile(profile, {
            'rule_configs': [{'id': 'selected', 'itemType': 'ruleset', 'library_rule_id': source,
                              'policy': 'DIRECT', 'enabled': True}],
            'mosdns': {'direct_rulesets': ['selected']},
        })
    repo.update_system_transaction(lambda system: system['system_config'].update({
        'server_domain': 'https://unreachable.test/prefix',
        'rule_fetch_proxy': 'http://user:private-password@proxy.test:8080',
    }))
    yield repo
    config_module.reset_config_context()


def callback(original, *, profile='office', part=None):
    query = {'url': original, 'token': 'internal-secret'}
    if part:
        query['part'] = part
    return f'https://unreachable.test/prefix/api/profiles/{profile}/mosdns/rule-proxy?' + urlencode(query)


def download(url, path='rules/source.txt', name='测试规则'):
    return {'url': url, 'name': name, 'local_path': path}


def read_archive(buffer):
    with zipfile.ZipFile(buffer) as archive:
        return {name: archive.read(name).decode() for name in archive.namelist()}


def test_real_generation_resolves_inline_profile_without_self_http(repository, monkeypatch):
    def unavailable(*args, **kwargs):
        raise AssertionError('Inline rules must not make network requests')
    monkeypatch.setattr(mosdns, '_fetch_remote_content', unavailable)
    app = Flask(__name__)
    register_blueprints(app)
    result = app.test_client().post('/api/profiles/office/generate/mosdns', json={})
    assert result.status_code == 200
    files = read_archive(io.BytesIO(result.data))
    assert files['rules/办公混合.txt'] == 'full:office.test'
    assert '2001:db8::/32' in files['rules/办公混合_ip.txt']
    assert 'home.test' not in '\n'.join(files.values())
    document = yaml.safe_load(files['config.yaml'])
    for plugin in document['plugins']:
        args = plugin.get('args')
        if isinstance(args, dict):
            assert all(path.removeprefix('./') in files for path in args.get('files', []))


def test_shared_classical_library_references_reuse_files(repository, monkeypatch):
    profile = repository.get_profile('office')
    profile['rule_configs'].append({'id': 'selected-again', 'itemType': 'ruleset',
                                   'library_rule_id': 'office-rule', 'policy': 'PROXY', 'enabled': True})
    profile['mosdns']['proxy_rulesets'] = ['selected-again']
    repository.save_profile('office', profile)
    monkeypatch.setattr(mosdns, '_fetch_remote_content', lambda *a, **kw: pytest.fail('Self HTTP'))
    app = Flask(__name__)
    register_blueprints(app)
    result = app.test_client().post('/api/profiles/office/generate/mosdns', json={})
    assert result.status_code == 200
    with zipfile.ZipFile(io.BytesIO(result.data)) as archive:
        for name in ('rules/办公混合.txt', 'rules/办公混合_ip.txt'):
            assert archive.namelist().count(name) == 1
    files = read_archive(io.BytesIO(result.data))
    assert files['rules/办公混合.txt'] == 'full:office.test'
    assert files['rules/办公混合_ip.txt'] == '203.0.113.0/24\n2001:db8::/32'
    refs = [path for plugin in yaml.safe_load(files['config.yaml'])['plugins']
            if isinstance(plugin.get('args'), dict) for path in plugin['args'].get('files', [])]
    assert refs.count('./rules/办公混合.txt') == 2
    assert refs.count('./rules/办公混合_ip.txt') == 2


def test_remote_classical_source_fetched_once_and_preserves_snapshot(repository, monkeypatch):
    config = repository.get_compat_config('office')
    seen = []
    def fetch(url, config_data=None):
        assert config_data['profile_id'] == 'office'
        assert config_data['system_config']['rule_fetch_proxy'].endswith('@proxy.test:8080')
        seen.append(url)
        return 'DOMAIN,office.test\nIP-CIDR,203.0.113.0/24'
    monkeypatch.setattr(mosdns, '_fetch_remote_content', fetch)
    monkeypatch.setattr(mosdns, 'get_config', lambda: pytest.fail('Worker reselected request profile'))
    urls = [download(callback('https://rules.test/shared', part=part), f'rules/{part}.txt')
            for part in ('domain', 'ip')]
    files = read_archive(build_mosdns_zip(config, 'plugins: []', urls, []))
    assert seen == ['https://rules.test/shared']
    assert files['rules/domain.txt'] == 'full:office.test'
    assert files['rules/ip.txt'] == '203.0.113.0/24'


@pytest.mark.parametrize('proxy', ['', 'http://user:private-password@proxy.test:8080'])
def test_underlying_transport_keeps_github_mirror_and_proxy(repository, monkeypatch, proxy):
    config = repository.get_compat_config('office')
    config['system_config'].update({'rule_fetch_proxy': proxy, 'github_proxy_domain': 'https://mirror.test'})
    seen = []
    def fetch(self, url, **kwargs):
        seen.append((url, kwargs))
        response = requests.Response()
        response.status_code = 200
        response._content = b'DOMAIN,transport.test'
        response._content_consumed = True
        return response
    monkeypatch.setattr('backend.utils.rule_fetch._RuleSession.get', fetch)
    url = callback('https://raw.githubusercontent.com/example/rules/main/list')
    files = read_archive(build_mosdns_zip(config, 'plugins: []', [download(url)], []))
    assert files['rules/source.txt'] == 'full:transport.test'
    assert len(seen) == 1
    assert seen[0][0] == 'https://mirror.test/https://raw.githubusercontent.com/example/rules/main/list'
    assert 'internal-secret' not in seen[0][0]
    if proxy:
        assert seen[0][1]['proxies'] == {'http': proxy, 'https': proxy}
    else:
        assert 'proxies' not in seen[0][1]
    assert seen[0][1]['timeout'] == (3, 10)


def test_local_cache_callback_resolves_original_url_without_self_http(repository, monkeypatch):
    config = repository.get_compat_config('office')
    source = 'https://rules.test/remote'
    config['rule_library'].append({'id': 'remote', 'name': '远程规则', 'source_type': 'url', 'url': source})
    config['rule_configs'].append({'itemType': 'ruleset', 'library_rule_id': 'remote'})
    repository.write_shared_text('rules/远程规则.list', 'DOMAIN,cached.test')
    seen = []
    def fetch(url, config_data=None):
        seen.append(url)
        raise requests.Timeout('private-password')
    monkeypatch.setattr(mosdns, '_fetch_remote_content', fetch)
    url = callback('/api/profiles/office/rules/local/远程规则')
    files = read_archive(build_mosdns_zip(config, 'plugins: []', [download(url)], []))
    assert files['rules/source.txt'] == 'full:cached.test'
    assert seen == [source]


@pytest.mark.parametrize('cached', [False, True])
def test_remote_timeout_falls_back_only_to_matching_cached_source(repository, monkeypatch, cached, caplog):
    config = repository.get_compat_config('office')
    source = 'https://rules.test/list?token=source-secret'
    config['rule_configs'].append({'itemType': 'ruleset', 'name': '远程规则', 'url': source})
    if cached:
        repository.write_shared_text('rules/' + sanitize_rule_name('远程规则') + '.list', 'DOMAIN,cached.test')
    def fetch(*args, **kwargs):
        raise requests.Timeout('Timeout https://rules.test/?token=source-secret via private-password')
    monkeypatch.setattr(mosdns, '_fetch_remote_content', fetch)
    args = (config, 'plugins: []', [download(callback(source))], [])
    if cached:
        files = read_archive(build_mosdns_zip(*args))
        assert files['rules/source.txt'] == 'full:cached.test'
    else:
        with pytest.raises(MosdnsArchiveError, match='测试规则.*Timeout') as exc:
            build_mosdns_zip(*args)
        assert 'source-secret' not in str(exc.value)
    assert 'private-password' not in caplog.text
    assert 'source-secret' not in caplog.text


def test_remote_fetch_parallelism_is_real_and_bounded(repository, monkeypatch):
    config = repository.get_compat_config('office')
    barrier = threading.Barrier(MAX_WORKERS)
    lock = threading.Lock()
    active = peak = count = 0
    def fetch(url, config_data=None):
        nonlocal active, peak, count
        with lock:
            active += 1
            count += 1
            this_call = count
            peak = max(peak, active)
        if this_call <= MAX_WORKERS:
            barrier.wait(timeout=5)
        time.sleep(0.005)
        with lock:
            active -= 1
        return 'domain:parallel.test'
    monkeypatch.setattr(mosdns, '_fetch_remote_content', fetch)
    downloads = [download(callback(f'https://rules.test/{i}'), f'rules/{i}.txt') for i in range(19)]
    files = read_archive(build_mosdns_zip(config, 'plugins: []', downloads, []))
    assert peak == MAX_WORKERS
    assert count == 19
    assert len(files) == 20


def test_failed_source_cancels_queue_without_waiting_for_other_downloads(repository, monkeypatch):
    from concurrent.futures import ThreadPoolExecutor
    entered = threading.Barrier(MAX_WORKERS)
    release = threading.Event()
    calls = []
    lock = threading.Lock()
    def fetch(url, config_data=None):
        with lock:
            calls.append(url)
            first_batch = len(calls) <= MAX_WORKERS
        if first_batch:
            entered.wait(timeout=5)
        if url.endswith('/0'):
            raise requests.Timeout('source-secret')
        assert release.wait(timeout=5)
        return 'domain:event.test'
    monkeypatch.setattr(mosdns, '_fetch_remote_content', fetch)
    downloads = [download(callback(f'https://rules.test/{i}'), f'rules/{i}.txt') for i in range(80)]
    config = repository.get_compat_config('office')
    with ThreadPoolExecutor(max_workers=1) as runner:
        result = runner.submit(build_mosdns_zip, config, 'plugins: []', downloads, [])
        try:
            with pytest.raises(MosdnsArchiveError, match='Timeout'):
                result.result(timeout=3)
            # One worker can pick up a new job between failure and cancellation.
            assert len(calls) <= MAX_WORKERS + 1
        finally:
            release.set()


def test_size_budget_stops_fetching_before_loading_all_sources(repository, monkeypatch):
    from backend.utils import mosdns_archive
    monkeypatch.setattr(mosdns_archive, 'MAX_TOTAL_BYTES', 80)
    seen = []
    def fetch(url, config_data=None):
        seen.append(url)
        return 'domain:' + 'long-label.' * 8 + 'test'
    monkeypatch.setattr(mosdns, '_fetch_remote_content', fetch)
    downloads = [download(callback(f'https://rules.test/{i}'), f'rules/{i}.txt') for i in range(80)]
    with pytest.raises(MosdnsArchiveError, match='大小限制'):
        build_mosdns_zip(repository.get_compat_config('office'), 'plugins: []', downloads, [])
    assert len(seen) <= MAX_WORKERS


def test_file_budget_is_checked_before_fetching(repository, monkeypatch):
    from backend.utils import mosdns_archive
    monkeypatch.setattr(mosdns_archive, 'MAX_FILES', 2)
    monkeypatch.setattr(mosdns, '_fetch_remote_content', lambda *a, **kw: pytest.fail('Over budget fetch'))
    downloads = [download(callback(f'https://rules.test/{i}'), f'rules/{i}.txt') for i in range(2)]
    with pytest.raises(MosdnsArchiveError, match='文件数量限制'):
        build_mosdns_zip(repository.get_compat_config('office'), 'plugins: []', downloads, [])


@pytest.mark.parametrize('source', [
    callback('/api/profiles/default/rule-library/content/home-rule'),
    callback('/api/profiles/office/rule-library/content/home-rule'),
])
def test_wrong_profile_and_unselected_sources_never_use_self_http(repository, monkeypatch, source):
    monkeypatch.setattr(mosdns, '_fetch_remote_content', lambda *a, **kw: pytest.fail('Self HTTP'))
    with pytest.raises(MosdnsArchiveError):
        build_mosdns_zip(repository.get_compat_config('office'), 'plugins: []', [download(source)], [])


@pytest.mark.parametrize('path', ['../escape', '/absolute', './rules/../../escape', 'rules\\escape', 'config.yaml'])
def test_unsafe_or_duplicate_paths_rejected(repository, path):
    with pytest.raises(MosdnsArchiveError):
        build_mosdns_zip(repository.get_compat_config('office'), 'plugins: []', [], [{'path': path, 'content': 'x'}])


def test_missing_reference_rejected(repository):
    config = 'plugins:\n- type: domain_set\n  args:\n    files: [./rules/missing.txt]'
    with pytest.raises(MosdnsArchiveError, match='未打包'):
        build_mosdns_zip(repository.get_compat_config('office'), config, [], [])


def test_same_rule_path_with_different_contents_rejected(repository):
    with pytest.raises(MosdnsArchiveError, match='内容冲突'):
        build_mosdns_zip(repository.get_compat_config('office'), 'plugins: []', [], [
            {'path': './rules/same.txt', 'content': 'domain:a.test'},
            {'path': 'rules/same.txt', 'content': 'domain:b.test'},
        ])


def test_route_failure_is_json_not_partial_zip_and_has_safe_details(repository, monkeypatch, caplog):
    app = Flask(__name__)
    register_blueprints(app)
    monkeypatch.setattr(generate, 'get_mosdns_ruleset_downloads', lambda *a, **kw: [
        download(callback('https://rules.test/good'), 'rules/good.txt'),
        download(callback('https://rules.test/bad?token=source-secret'), 'rules/bad.txt', '失败规则'),
    ])
    def fetch(url, config_data=None):
        if 'bad' in url:
            response = requests.Response()
            response.status_code = 503
            raise requests.HTTPError('source-secret private-password internal-secret', response=response)
        return 'domain:ok.test'
    monkeypatch.setattr(mosdns, '_fetch_remote_content', fetch)
    result = app.test_client().post('/api/profiles/office/generate/mosdns', json={})
    assert result.status_code == 500
    assert result.is_json
    message = result.get_json()['message']
    assert '失败规则' in message and 'HTTPError' in message and '503' in message
    assert 'Content-Disposition' not in result.headers
    for secret in ('source-secret', 'private-password', 'internal-secret'):
        assert secret not in message + caplog.text
