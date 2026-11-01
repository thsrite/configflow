import json
from flask import Flask
from urllib.parse import parse_qs, urlsplit

import pytest

from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints
from backend.converters.mihomo import get_mihomo_provider_downloads
from backend.converters.mosdns import get_mosdns_ruleset_downloads
from backend.routes.rules import get_ruleset_content
from backend.utils import subscription_cache
from backend.utils.rule_utils import get_rules_dir


def setup_repository(tmp_path, monkeypatch):
    repository = ProfileRepository(tmp_path)
    repository.create_profile({"id": "alpha", "name": "Alpha"})
    repository.create_profile({"id": "beta", "name": "Beta"})
    config_module.set_repository(repository)
    return repository


def test_raw_subscription_and_rule_caches_are_shared(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    app = Flask(__name__)

    with app.test_request_context("/", headers={"X-ConfigFlow-Profile": "alpha"}):
        subscription_cache.save_subscription_nodes("same", [{"name": "shared-node"}])
        alpha_cache = subscription_cache._get_cache_path("same")
        alpha_rules = get_rules_dir()
    with app.test_request_context("/", headers={"X-ConfigFlow-Profile": "beta"}):
        assert subscription_cache.load_subscription_cache("same")["nodes"][0]["name"] == "shared-node"
        beta_cache = subscription_cache._get_cache_path("same")
        beta_rules = get_rules_dir()
        subscription_cache.save_subscription_nodes("same", [{"name": "updated-node"}])

    with app.test_request_context("/", headers={"X-ConfigFlow-Profile": "alpha"}):
        assert subscription_cache.load_subscription_cache("same")["nodes"][0]["name"] == "updated-node"
    assert alpha_cache == beta_cache == str(repository.shared_cache_dir() / "same.json")
    assert alpha_rules == beta_rules == str(repository.shared_rules_dir())


def test_aggregation_provider_is_profile_scoped(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    app = Flask(__name__)
    register_blueprints(app)
    client = app.test_client()
    aggregation = {"id": "agg-same", "name": "Same", "subscriptions": [], "nodes": []}
    repository.save_shared({"subscription_aggregations": [aggregation]})
    for profile_id in ("alpha", "beta"):
        repository.save_profile(profile_id, {
            "proxy_groups": [{"id": "shared", "name": "Shared", "type": "select", "aggregations": ["agg-same"]}],
        })

    beta_path = repository.write_profile_text("beta", "providers/agg-same.yaml", "last-known-good-beta")
    alpha = client.get(
        "/api/profiles/alpha/aggregations/agg-same/provider",
        headers={"X-ConfigFlow-Profile": "beta"},
    )
    assert alpha.status_code == 200, alpha.get_data(as_text=True)
    alpha_path = repository.providers_dir("alpha") / "agg-same.yaml"
    alpha_content = alpha_path.read_bytes()
    assert alpha_content == alpha.data
    assert beta_path.read_text(encoding="utf-8") == "last-known-good-beta"

    beta = client.get(
        "/api/profiles/beta/aggregations/agg-same/provider",
        headers={"X-ConfigFlow-Profile": "alpha"},
    )
    assert beta.status_code == 200, beta.get_data(as_text=True)
    assert beta_path.read_bytes() == beta.data
    assert alpha_path.read_bytes() == alpha_content
    assert alpha_path != beta_path
    assert not (repository.providers_dir("default") / "agg-same.yaml").exists()


def test_generated_configuration_is_written_under_selected_profile(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    app = Flask(__name__)
    register_blueprints(app)
    monkeypatch.setattr("backend.routes.generate.generate_mihomo_config", lambda data, base_url="", **kwargs: "profile-config")

    response = app.test_client().post(
        "/api/generate/mihomo",
        headers={"X-ConfigFlow-Profile": "alpha"},
        json={},
    )

    assert response.status_code == 200
    assert (repository.generated_dir("alpha") / "config.yaml").read_text(encoding="utf-8") == "profile-config"
    assert not (repository.generated_dir("beta") / "config.yaml").exists()


def test_generated_provider_urls_include_the_selected_profile(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    repository.update_system_transaction(
        lambda system: system["system_config"].update({"server_domain": "http://configflow.test"})
    )
    repository.save_shared({"subscriptions": [{
        "id": "sub-1",
        "name": "Primary",
        "url": "https://example.test/sub",
        "enabled": True,
    }]})
    repository.save_profile("alpha", {
        "proxy_groups": [{
            "id": "group-1",
            "name": "Proxy",
            "type": "select",
            "enabled": True,
            "subscriptions": ["sub-1"],
        }],
    })
    config = repository.get_compat_config("alpha")

    downloads = get_mihomo_provider_downloads(config, base_url="http://fallback.test")

    assert downloads[0]["url"] == "http://configflow.test/api/profiles/alpha/subscriptions/sub-1/proxies"


def test_mosdns_rule_proxy_urls_use_url_encoded_internal_token_for_profile_and_legacy():
    rule_set = {
        "id": "rules-1", "name": "Rules", "itemType": "ruleset",
        "url": "https://rules.test/list?a=1&b=2",
    }
    common = {
        "system_config": {
            "server_domain": "https://config.test",
            "config_token": "legacy-public-token",
            "rule_proxy_token": "internal /&?",
        },
        "mosdns": {"direct_rulesets": ["rules-1"], "proxy_rulesets": []},
        "rule_configs": [rule_set],
    }

    profile_url = get_mosdns_ruleset_downloads({**common, "profile_id": "alpha"})[0]["url"]
    legacy_url = get_mosdns_ruleset_downloads(common)[0]["url"]

    assert profile_url == (
        "https://config.test/api/profiles/alpha/mosdns/rule-proxy"
        "?url=https%3A%2F%2Frules.test%2Flist%3Fa%3D1%26b%3D2&source_id=rules-1&token=internal%20%2F%26%3F"
    )
    assert legacy_url == (
        "https://config.test/api/mosdns/rule-proxy"
        "?url=https%3A%2F%2Frules.test%2Flist%3Fa%3D1%26b%3D2&source_id=rules-1&token=internal%20%2F%26%3F"
    )


def test_mosdns_refuses_to_generate_rule_proxy_url_without_internal_token():
    config = {
        "system_config": {"server_domain": "https://config.test", "config_token": ""},
        "mosdns": {"direct_rulesets": ["rules-1"], "proxy_rulesets": []},
        "rule_configs": [{
            "id": "rules-1", "name": "Rules", "itemType": "ruleset",
            "url": "https://rules.test/list",
        }],
    }

    with pytest.raises(ValueError, match="rule proxy token"):
        get_mosdns_ruleset_downloads(config)


def _assert_generated_default_url_authenticates(repository, monkeypatch):
    repository.update_system_transaction(
        lambda system: system["system_config"].update({"server_domain": "https://config.test"})
    )
    repository.save_shared({"rule_library": [{
        "id": "library-1", "name": "Rules", "source_type": "url",
        "url": "https://rules.test/list",
    }]})
    repository.save_profile("default", {
        "mosdns": {"direct_rulesets": ["rules-1"], "proxy_rulesets": []},
        "rule_configs": [{
            "id": "rules-1", "itemType": "ruleset", "library_rule_id": "library-1",
            "policy": "DIRECT", "enabled": True,
        }],
    })
    config_module.set_repository(repository)
    token = repository.get_system()["system_config"]["rule_proxy_token"]
    generated_url = get_mosdns_ruleset_downloads(repository.get_compat_config("default"))[0]["url"]
    parsed = urlsplit(generated_url)
    assert parse_qs(parsed.query)["token"] == [token]
    monkeypatch.setattr("backend.routes.mosdns._fetch_remote_content", lambda url, **kwargs: b"+.example.com")
    monkeypatch.setattr(
        "socket.getaddrinfo",
        lambda *args, **kwargs: [(2, 1, 6, "", ("93.184.216.34", 443))],
    )
    app = Flask(__name__)
    register_blueprints(app)
    response = app.test_client().get(f"{parsed.path}?{parsed.query}")
    assert response.status_code == 200
    assert response.get_data(as_text=True) == "domain:example.com"


def test_empty_data_directory_generates_mosdns_url_accepted_by_rule_proxy(tmp_path, monkeypatch):
    _assert_generated_default_url_authenticates(ProfileRepository(tmp_path), monkeypatch)


def test_legacy_empty_token_generates_mosdns_url_accepted_by_rule_proxy(tmp_path, monkeypatch):
    (tmp_path / "config.json").write_text(
        json.dumps({"system_config": {"config_token": ""}}),
        encoding="utf-8",
    )
    _assert_generated_default_url_authenticates(ProfileRepository(tmp_path), monkeypatch)


def test_generated_profile_rule_proxy_url_downloads_with_auth_enabled(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    repository.update_system_transaction(
        lambda system: system["system_config"].update({
            "server_domain": "https://config.test", "config_token": "profile token",
        })
    )
    repository.save_shared({"rule_library": [{
        "id": "library-1", "name": "Rules", "source_type": "url",
        "url": "https://rules.test/list",
    }]})
    repository.save_profile("alpha", {
        "mosdns": {"direct_rulesets": ["rules-1"], "proxy_rulesets": []},
        "rule_configs": [{
            "id": "rules-1", "itemType": "ruleset", "library_rule_id": "library-1",
            "policy": "DIRECT", "enabled": True,
        }],
    })
    config = repository.get_compat_config("alpha")
    generated_url = get_mosdns_ruleset_downloads(config)[0]["url"]
    parsed = urlsplit(generated_url)
    fetched = []
    monkeypatch.setattr("backend.common.auth.is_auth_enabled", lambda: True)
    monkeypatch.setattr("backend.routes.auth.is_auth_enabled", lambda: True)
    monkeypatch.setattr(
        "backend.routes.mosdns._fetch_remote_content",
        lambda url, **kwargs: fetched.append(url) or b"+.example.com",
    )
    monkeypatch.setattr(
        "socket.getaddrinfo",
        lambda *args, **kwargs: [(2, 1, 6, "", ("93.184.216.34", 443))],
    )
    app = Flask(__name__)
    register_blueprints(app)
    from backend.routes.auth import setup_before_request
    setup_before_request(app)

    response = app.test_client().get(f"{parsed.path}?{parsed.query}")

    assert response.status_code == 200, response.get_data(as_text=True)
    assert response.get_data(as_text=True) == "domain:example.com"
    assert fetched == ["https://rules.test/list"]


def test_rule_library_catalog_is_global_not_a_profile_alias(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    repository.save_shared({
        "rule_library": [{"id": "rule-1", "name": "Local", "source_type": "content", "content": "DOMAIN,example.test"}],
    })
    app = Flask(__name__)
    register_blueprints(app)

    client = app.test_client()
    catalogs = []
    for profile_id in ("alpha", "beta"):
        response = client.get("/api/rule-library", headers={"X-ConfigFlow-Profile": profile_id})
        assert response.status_code == 200
        catalogs.append(response.get_json())
        assert catalogs[-1][0]["id"] == "rule-1"
        assert repository.get_profile(profile_id)["rule_configs"] == []
        assert client.get(f"/api/profiles/{profile_id}/rule-library").status_code == 404
    assert catalogs[0] == catalogs[1]


def test_aggregation_workers_share_raw_cache_and_scope_generated_provider(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    repository.save_shared({
        "subscriptions": [{
            "id": "sub-1",
            "name": "Primary",
            "url": "https://example.test/sub",
            "enabled": True,
        }],
        "subscription_aggregations": [{
            "id": "agg-1", "name": "Aggregation", "subscriptions": ["sub-1"], "nodes": [],
        }],
    })
    repository.save_profile("alpha", {
        "proxy_groups": [{"id": "shared", "name": "Shared", "type": "select", "aggregations": ["agg-1"]}],
    })
    app = Flask(__name__)
    register_blueprints(app)
    client = app.test_client()
    monkeypatch.setattr(
        "backend.routes.aggregations.get_subscription_proxies_yaml",
        lambda sub_id, url: (
            'proxies: [{name: node-1, type: ss, server: proxy.test, port: 443, cipher: aes-128-gcm, password: secret}]',
            'test',
        ),
    )

    response = client.get(
        "/api/profiles/alpha/aggregations/agg-1/provider",
        headers={"X-ConfigFlow-Profile": "beta"},
    )
    assert response.status_code == 200, response.get_data(as_text=True)
    alpha_path = repository.providers_dir("alpha") / "agg-1.yaml"
    assert alpha_path.read_bytes() == response.data
    assert "node-1" in response.get_data(as_text=True)
    cache_path = repository.shared_cache_dir() / "sub-1.json"

    with app.test_request_context("/", headers={"X-ConfigFlow-Profile": "beta"}):
        cached = subscription_cache.load_subscription_cache("sub-1")
        assert cached["nodes"][0]["name"] == "node-1"
        assert subscription_cache._get_cache_path("sub-1") == str(cache_path)
        assert config_module.get_config("beta")["subscriptions"] == []
    beta_profile = repository.get_profile("beta")
    beta = client.get(
        "/api/profiles/beta/aggregations/agg-1/provider",
        headers={"X-ConfigFlow-Profile": "alpha"},
    )
    assert beta.status_code == 200, beta.get_data(as_text=True)
    assert alpha_path.read_bytes() == response.data
    assert (repository.providers_dir("beta") / "agg-1.yaml").read_bytes() == beta.data
    assert repository.get_profile("beta") == beta_profile
    assert not (repository.providers_dir("default") / "agg-1.yaml").exists()


def test_local_rule_refresh_uses_shared_repository_write(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    repository.save_shared({
        "rule_library": [{
            "id": "rule-1",
            "name": "Remote",
            "source_type": "url",
            "url": "https://example.test/rules",
        }],
    })
    app = Flask(__name__)
    register_blueprints(app)
    calls = []
    original_write = repository.write_shared_text

    def write_shared_text(relative_path, content):
        calls.append(relative_path)
        return original_write(relative_path, content)

    monkeypatch.setattr(repository, "write_shared_text", write_shared_text)
    monkeypatch.setattr(
        "backend.routes.rules.requests.get",
        lambda url, timeout, **kwargs: type("Response", (), {"status_code": 200, "content": b"remote.test", "raise_for_status": lambda self: None})(),
    )

    response = app.test_client().get(
        "/api/profiles/alpha/rules/local/Remote",
        headers={"X-ConfigFlow-Profile": "beta"},
    )

    assert response.status_code == 200
    from backend.utils.rule_utils import rule_cache_filename
    cache = rule_cache_filename(repository.get_shared()["rule_library"][0])
    assert calls == ["rules/" + cache]
    assert (repository.shared_rules_dir() / cache).read_text(encoding="utf-8") == "remote.test"


def test_rule_library_content_urls_include_the_selected_profile(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    repository.save_shared({
        "rule_library": [{"id": "rule-1", "name": "Local", "source_type": "content", "content": "DOMAIN,example.test"}],
    })
    repository.update_system_transaction(
        lambda system: system["system_config"].update({"server_domain": "https://config.test"})
    )
    for profile_id in ("alpha", "beta"):
        repository.save_profile(profile_id, {"rule_configs": [{
            "id": "selected-rule", "itemType": "ruleset", "library_rule_id": "rule-1",
            "policy": "DIRECT", "enabled": True,
        }]})
    app = Flask(__name__)
    register_blueprints(app)
    client = app.test_client()

    for profile_id in ("alpha", "beta"):
        response = client.get("/api/rules", headers={"X-ConfigFlow-Profile": profile_id})
        assert response.status_code == 200
        rule = response.get_json()[0]
        assert rule["library_rule_id"] == "rule-1"
        assert rule["name"] == "Local"
        assert rule["url"] == f"https://config.test/api/profiles/{profile_id}/rule-library/content/rule-1"
        assert "url" not in repository.get_profile(profile_id)["rule_configs"][0]
        content = client.get(urlsplit(rule["url"]).path)
        assert content.status_code == 200
        assert content.get_data(as_text=True) == "DOMAIN,example.test"


def test_rule_cache_workers_use_shared_rule_sources(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    repository.save_shared({
        "rule_library": [{
            "id": "rule-1",
            "name": "Remote",
            "source_type": "url",
            "url": "https://example.test/rules",
        }],
    })
    app = Flask(__name__)
    register_blueprints(app)
    seen_rules = []
    monkeypatch.setattr(
        "backend.utils.scheduled_refresh.refresh_rule_source",
        lambda rule, trigger: seen_rules.append(rule["id"]) or (b"", None),
    )

    response = app.test_client().post(
        "/api/rule-library/cache",
        headers={"X-ConfigFlow-Profile": "alpha"},
        json={"rule_ids": ["rule-1"]},
    )

    assert response.status_code == 200
    assert seen_rules == ["rule-1"]


def test_ruleset_content_cache_uses_shared_repository_write(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    app = Flask(__name__)
    calls = []
    original_write = repository.write_shared_text

    def write_shared_text(relative_path, content):
        calls.append(relative_path)
        return original_write(relative_path, content)

    monkeypatch.setattr(repository, "write_shared_text", write_shared_text)
    monkeypatch.setattr(
        "backend.routes.rules.requests.get",
        lambda url, timeout, **kwargs: type("Response", (), {"status_code": 200, "content": b"remote.test", "raise_for_status": lambda self: None})(),
    )

    with app.test_request_context("/", headers={"X-ConfigFlow-Profile": "alpha"}):
        content = get_ruleset_content(
            {"name": "Remote"},
            {"name": "Remote", "source_type": "url", "url": "https://example.test/rules"},
        )

    assert content == "remote.test"
    from backend.utils.rule_utils import rule_cache_filename
    cache = rule_cache_filename({"name": "Remote", "source_type": "url", "url": "https://example.test/rules"})
    assert calls == ["rules/" + cache]
    assert (repository.shared_rules_dir() / cache).read_text(encoding="utf-8") == content


def test_corrupt_subscription_cache_is_ignored(tmp_path, monkeypatch):
    repository = setup_repository(tmp_path, monkeypatch)
    repository.write_shared_text("subscribes/broken.json", "{")
    app = Flask(__name__)

    with app.test_request_context("/", headers={"X-ConfigFlow-Profile": "alpha"}):
        cached = subscription_cache.load_subscription_cache("broken")

    assert cached is None
