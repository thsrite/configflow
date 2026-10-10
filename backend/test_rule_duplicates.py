"""Duplicate scans keep complete results and expose when a response is ready."""
import logging

import pytest
from flask import Flask

from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints
from backend.routes import rules


@pytest.fixture
def duplicate_app(tmp_path, monkeypatch):
    repository = ProfileRepository(tmp_path)
    repository.save_shared({'rule_library': [
        {'id': 'a', 'name': 'A', 'source_type': 'content', 'content': '', 'enabled': True},
        {'id': 'b', 'name': 'B', 'source_type': 'content', 'content': '', 'enabled': True},
        {'id': 'empty', 'name': 'Empty', 'source_type': 'content', 'content': '', 'enabled': True},
        {'id': 'off', 'name': 'Off', 'source_type': 'content', 'content': 'DOMAIN-SUFFIX,example.com', 'enabled': False},
    ]})
    repository.save_profile('default', {'rule_configs': [
        {'id': 'direct', 'itemType': 'rule', 'rule_type': 'DOMAIN-SUFFIX', 'value': 'example.com', 'policy': 'DIRECT'},
        {'id': 'set-a', 'itemType': 'ruleset', 'library_rule_id': 'a', 'policy': 'DIRECT'},
        {'id': 'set-b', 'itemType': 'ruleset', 'library_rule_id': 'b', 'policy': 'Proxy'},
        {'id': 'empty', 'itemType': 'ruleset', 'library_rule_id': 'empty', 'policy': 'DIRECT'},
        {'id': 'off', 'itemType': 'ruleset', 'library_rule_id': 'off', 'policy': 'DIRECT'},
        {'id': 'disabled', 'itemType': 'rule', 'rule_type': 'DOMAIN-SUFFIX', 'value': 'example.com', 'enabled': False},
    ]})
    repository.write_shared_text('rules/A.list', '\n'.join([
        '# ignored', 'DOMAIN-SUFFIX,Example.com', 'DOMAIN-SUFFIX,example.com',
        'IP-CIDR6,2001:db8::/32,no-resolve', 'REGEX,^Foo$',
    ]))
    repository.write_shared_text('rules/B.list', '\n'.join([
        'domain:example.com', 'IP-CIDR6,2001:db8::/32', 'REGEX,^foo$',
    ]))
    config_module.set_repository(repository)
    monkeypatch.setattr('backend.common.auth.is_auth_enabled', lambda: False)
    app = Flask(__name__)
    register_blueprints(app)
    yield app, repository
    config_module.reset_config_context()


def test_duplicates_keep_all_occurrences_and_report_response_completion(duplicate_app, monkeypatch, caplog):
    app, _ = duplicate_app
    config_reads = []
    original_get_config = rules.get_config

    def read_config():
        config_reads.append(True)
        return original_get_config()

    monkeypatch.setattr(rules, 'get_config', read_config)
    with caplog.at_level(logging.INFO):
        response = app.test_client().post('/api/rules/find-duplicates')
    assert response.status_code == 200
    data = response.get_json()
    assert data['stats'] == {
        'rules_checked': 1, 'rulesets_checked': 2,
        'failed_rulesets': ['Empty'], 'duplicate_groups': 2,
    }
    domain, ipv6 = data['duplicates']
    assert (domain['rule_type'], domain['count'], domain['policy_conflict']) == ('DOMAIN-SUFFIX', 4, True)
    assert [item['priority'] for item in domain['occurrences']] == [1, 2, 2, 3]
    assert [item.get('line_no') for item in domain['occurrences']] == [None, 2, 3, 1]
    assert (ipv6['value'], ipv6['count'], ipv6['policy_conflict']) == ('2001:db8::/32', 2, True)
    # Cached sources reuse the request snapshot rather than resolving the full
    # configuration again for every source file.
    assert len(config_reads) == 1
    assert 'Duplicate scan ' in caplog.text
    assert 'scanned ruleset "A": 4 entries' in caplog.text
    assert 'matched: 2 duplicate groups' in caplog.text
    assert 'response ready: status=200 bytes=' in caplog.text


def test_cached_and_inline_sources_do_not_load_download_configuration(duplicate_app, monkeypatch):
    app, _ = duplicate_app

    def unexpected_config_read():
        pytest.fail('cached/inline content should not reload the configuration')

    monkeypatch.setattr(rules, 'get_config', unexpected_config_read)
    with app.test_request_context('/api/rules'):
        assert 'Example.com' in rules.get_ruleset_content({'name': 'A'})
        assert rules.get_ruleset_content({}, {
            'name': 'Inline', 'source_type': 'content', 'content': 'DOMAIN,example.com',
        }) == 'DOMAIN,example.com'
