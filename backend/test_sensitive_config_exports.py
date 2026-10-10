import copy
import json
import sys
import time
import types
import urllib.parse

import pytest
from flask import Flask

from backend.common import config as config_module
from backend.common import config_export
from backend.common.config_export import sanitize_config_for_output, sanitize_external_payload
from backend.common.config_repository import ProfileRepository
from backend.mcp_server import mcp_bp
from backend.routes import register_blueprints


RULE_PROXY_SECRET = "rule proxy/&?=秘密"
CONFIG_TOKEN = "managed-config-token-remains-visible"
REDACTED = "[REDACTED]"


@pytest.mark.parametrize(
    "value,secrets,expected",
    [
        ("ordinary-rule.example", {"private-token"}, False),
        ("prefix-private-token-suffix", {"private-token"}, True),
        ("前缀秘密后缀", {"秘密"}, True),
        ("encoded%20value+with%2520layers", set(), False),
    ],
)
def test_secret_check_skips_decoders_when_they_cannot_change_the_result(
    monkeypatch, value, secrets, expected
):
    def unexpected_decode(_value):
        pytest.fail("This input does not require URL decoding")

    monkeypatch.setattr(config_export.urllib.parse, "unquote", unexpected_decode)
    monkeypatch.setattr(config_export.urllib.parse, "unquote_plus", unexpected_decode)

    assert config_export._contains_encoded_secret(value, secrets) is expected


@pytest.mark.parametrize(
    "value,secret,expected",
    [
        ("prefix-secret+value-suffix", "secret value", True),
        ("prefix-%73ecret%20value-suffix", "secret value", True),
        ("prefix-secret%252Bvalue-suffix", "secret+value", True),
        ("prefix-secret%2520value-suffix", "secret value", True),
        ("%broken-secret+value-%ZZ", "secret value", True),
        ("ordinary%20rule+name", "private-token", False),
    ],
)
def test_secret_check_preserves_percent_and_plus_decoding_boundaries(value, secret, expected):
    assert config_export._contains_encoded_secret(value, {secret}) is expected


def test_sanitizer_reuses_string_checks_for_keys_and_values_without_skipping_redaction(monkeypatch):
    token = "private secret"
    monkeypatch.setattr(config_export, "_repository_rule_proxy_tokens", lambda: [token])
    monkeypatch.setattr(config_export, "_repository_agent_tokens", lambda: [])
    original_check = config_export._contains_encoded_secret
    checks = {}

    def count_check(value, secrets):
        checks[value] = checks.get(value, 0) + 1
        return original_check(value, secrets)

    monkeypatch.setattr(config_export, "_contains_encoded_secret", count_check)
    encoded = "private%2520secret"
    payload = [{"safe": "example.test", encoded: encoded} for _ in range(1000)]

    sanitized = sanitize_external_payload(payload)

    assert sanitized == [{"safe": "example.test", REDACTED: REDACTED} for _ in range(1000)]
    assert checks == {"safe": 1, "example.test": 1, encoded: 1}
    assert payload[0][encoded] == encoded


def test_sanitizer_string_cache_is_bounded_and_does_not_retain_large_values(monkeypatch):
    token = "private-token"
    monkeypatch.setattr(config_export, "_repository_rule_proxy_tokens", lambda: [token])
    monkeypatch.setattr(config_export, "_repository_agent_tokens", lambda: [])
    original_check = config_export._contains_encoded_secret
    checks = {}

    def count_check(value, secrets):
        checks[value] = checks.get(value, 0) + 1
        return original_check(value, secrets)

    monkeypatch.setattr(config_export, "_contains_encoded_secret", count_check)
    large = "x" * 2048 + token
    payload = [f"safe-{index}" for index in range(4097)] + ["safe-0", large, large]

    sanitized = sanitize_external_payload(payload)

    assert sanitized[:-2] == payload[:-2]
    assert sanitized[-2:] == [REDACTED, REDACTED]
    assert checks["safe-0"] == 2
    assert checks[large] == 2


def test_sanitizer_does_not_reuse_string_decisions_across_token_changes(monkeypatch):
    tokens = []
    monkeypatch.setattr(config_export, "_repository_rule_proxy_tokens", lambda: tokens)
    monkeypatch.setattr(config_export, "_repository_agent_tokens", lambda: [])
    payload = {"value": "prefix-new-secret-suffix"}

    assert sanitize_external_payload(payload) == payload
    tokens.append("new-secret")
    assert sanitize_external_payload(payload) == {"value": REDACTED}
    tokens.clear()
    assert sanitize_external_payload(payload) == payload


def _nested_list(depth, leaf="leaf"):
    value = leaf
    for _ in range(depth):
        value = [value]
    return value


def _descend_singleton_lists(value, count):
    for _ in range(count):
        assert isinstance(value, list) and len(value) == 1
        value = value[0]
    return value


def _secret_forms(token=RULE_PROXY_SECRET):
    forms = {token}
    frontier = {token}
    for _ in range(4):
        frontier = {
            encoded
            for value in frontier
            for encoded in (
                urllib.parse.quote(value, safe=""),
                urllib.parse.quote_plus(value, safe=""),
            )
        }
        forms.update(frontier)
    forms.update(
        value.replace("%2F", "%2f").replace("%26", "%26").replace("%3F", "%3f")
        for value in tuple(forms)
    )
    return forms


def _assert_all_secret_forms_absent(content):
    text = content.decode() if isinstance(content, bytes) else content
    for secret_form in _secret_forms():
        assert secret_form not in text


def test_sanitize_external_payload_uses_repository_token_and_scrubs_nested_encoded_forms(tmp_path):
    repository = ProfileRepository(tmp_path)
    repository.save_system({"system_config": {"rule_proxy_token": RULE_PROXY_SECRET}})
    config_module.set_repository(repository)
    encoded = urllib.parse.quote_plus(RULE_PROXY_SECRET, safe="")
    repeated = urllib.parse.quote(encoded, safe="").replace("%2F", "%2f")
    payload = {
        "system_config": {"rule_proxy_token": "decoy"},
        "nested": [
            f"raw={RULE_PROXY_SECRET}",
            {"url": f"https://example.test/?token={encoded}"},
            (f"again={repeated}",),
        ],
        f"encoded-key-{encoded}": "value",
        "rule_proxy_token": "another-decoy",
    }

    sanitized = sanitize_external_payload(payload, payload["system_config"])
    serialized = json.dumps(sanitized, ensure_ascii=False)

    assert "rule_proxy_token" not in serialized
    _assert_all_secret_forms_absent(serialized)
    assert RULE_PROXY_SECRET in repository.rule_proxy_tokens_for_sanitization()
    assert repository.get_system()["system_config"]["rule_proxy_token"] == RULE_PROXY_SECRET


def _encode_layers(value, layers, mode):
    encoders = {
        "quote": lambda item: urllib.parse.quote(item, safe=""),
        "quote_plus": lambda item: urllib.parse.quote_plus(item, safe=""),
    }
    for layer in range(layers):
        selected = mode if mode != "mixed" else ("quote", "quote_plus")[layer % 2]
        value = encoders[selected](value)
    return value


@pytest.mark.parametrize("depth", [1100, 5000])
def test_sanitize_external_payload_redacts_subtree_beyond_safe_depth_without_recursion(
    tmp_path, depth
):
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    payload = {"deep": _nested_list(depth), "safe": "preserved"}

    sanitized = sanitize_external_payload(payload)

    assert sanitized["safe"] == "preserved"
    assert _descend_singleton_lists(sanitized["deep"], 127) == REDACTED
    assert _descend_singleton_lists(payload["deep"], depth) == "leaf"


def test_sanitize_external_payload_redacts_only_cyclic_branch_and_does_not_mutate_input(
    tmp_path
):
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    cyclic = {"safe": ["unchanged"]}
    cyclic["inside"] = {"back": cyclic}
    payload = {"cyclic": cyclic, "ordinary": {"value": 1}}

    sanitized = sanitize_external_payload(payload)

    assert sanitized == {
        "cyclic": {"safe": ["unchanged"], "inside": {"back": REDACTED}},
        "ordinary": {"value": 1},
    }
    assert cyclic["inside"]["back"] is cyclic
    assert sanitized["cyclic"] is not cyclic
    assert sanitized["cyclic"]["safe"] is not cyclic["safe"]


def test_sanitize_external_payload_handles_wide_container_with_bounded_overhead(tmp_path):
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    payload = {f"key-{index}": [index, "safe"] for index in range(20_000)}

    started = time.perf_counter()
    sanitized = sanitize_external_payload(payload)
    elapsed = time.perf_counter() - started

    assert sanitized == payload
    assert sanitized is not payload
    assert sanitized["key-19999"] is not payload["key-19999"]
    assert elapsed < 5.0


@pytest.mark.parametrize('depth', [8, 5000])
def test_after_request_bounds_raw_json_depth_and_never_exposes_secrets(tmp_path, depth):
    app, _ = _app_with_secrets(tmp_path)
    from flask.wrappers import Response

    raw_json = '[' * depth + json.dumps(RULE_PROXY_SECRET) + ']' * depth

    @app.get('/api/deep-json')
    def deep_json():
        return Response(raw_json, mimetype='application/json')

    response = app.test_client().get('/api/deep-json')

    assert response.status_code == 200
    assert response.is_json
    _assert_all_secret_forms_absent(response.get_data())
    body = response.get_json()
    output_depth = 0
    while isinstance(body, list):
        assert len(body) == 1
        output_depth += 1
        assert output_depth <= 128
        body = body[0]
    assert body == REDACTED


def test_sanitize_config_for_output_does_not_deepcopy_pathological_input(tmp_path):
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    config = {"deep": _nested_list(1100), "system_config": {}}

    sanitized = sanitize_config_for_output(config)

    assert _descend_singleton_lists(sanitized["deep"], 127) == REDACTED
    assert _descend_singleton_lists(config["deep"], 1100) == "leaf"


@pytest.mark.parametrize("layers", range(21))
@pytest.mark.parametrize("mode", ["quote", "quote_plus", "mixed"])
def test_sanitize_external_payload_redacts_entire_string_at_zero_to_twenty_encoding_layers(
    tmp_path, layers, mode
):
    token = "令牌 +/%?=🔐"
    repository = ProfileRepository(tmp_path)
    repository.save_system({"system_config": {"rule_proxy_token": token}})
    config_module.set_repository(repository)
    encoded = _encode_layers(token, layers, mode)

    original = {
        "rule_proxy_token": token,
        "raw": f"prefix::{encoded}::suffix",
        "safe": "unchanged",
    }
    sanitized = sanitize_external_payload(original)

    assert sanitized == {"raw": REDACTED, "safe": "unchanged"}
    assert original["raw"] == f"prefix::{encoded}::suffix"
    assert repository.get_system()["system_config"]["rule_proxy_token"] == token


def _app_with_secrets(tmp_path):
    repository = ProfileRepository(tmp_path)
    encoded = urllib.parse.quote_plus(RULE_PROXY_SECRET, safe="")
    repeated = urllib.parse.quote(encoded, safe="")
    repository.save_system({"system_config": {
        "rule_proxy_token": RULE_PROXY_SECRET,
        "config_token": CONFIG_TOKEN,
        "server_domain": f"https://config.test/{RULE_PROXY_SECRET}",
        "sub_store_url": f"https://store.test/?token={encoded}",
    }})
    repository.save_shared({
        "subscriptions": [{"id": "sub-1", "name": "Secret subscription", "url": f"https://secret.test/sub?token={repeated}"}],
        "nodes": [{"id": "node-1", "name": "Secret node", "proxy_string": f"ss://secret#{RULE_PROXY_SECRET}"}],
        "rule_library": [{"id": "remote-rules", "name": "Rules", "source_type": "url", "url": "https://rules.test/list", "behavior": "classical"}],
    })
    repository.save_profile("default", {
        "rule_configs": [{"id": "rules-1", "itemType": "ruleset", "library_rule_id": "remote-rules", "policy": "DIRECT"}],
        "mosdns": {"direct_rulesets": ["rules-1"], "proxy_rulesets": []},
    })
    config_module.set_repository(repository)
    app = Flask(__name__)
    register_blueprints(app)
    app.register_blueprint(mcp_bp)
    return app, repository


def _assert_rule_proxy_secret_absent(response):
    assert response.status_code == 200
    assert b"rule_proxy_token" not in response.get_data()
    _assert_all_secret_forms_absent(response.get_data())


def test_full_backup_requires_admin_auth_and_preserves_restorable_credentials(tmp_path, monkeypatch):
    from backend.common.auth import generate_token

    app, repository = _app_with_secrets(tmp_path)
    monkeypatch.setattr("backend.common.auth.is_auth_enabled", lambda: True)
    client = app.test_client()
    assert client.get("/api/config/export").status_code == 401
    response = client.get("/api/config/export", headers={"Authorization": f"Bearer {generate_token('admin')}"})
    assert response.status_code == 200
    exported = json.loads(response.get_data())
    assert exported["system"]["system_config"]["config_token"] == CONFIG_TOKEN
    assert exported["system"]["system_config"]["rule_proxy_token"] == RULE_PROXY_SECRET
    restored = ProfileRepository(tmp_path / "restored")
    previous_token = restored.get_system()["system_config"]["rule_proxy_token"]
    restored.import_all(exported)
    restored_settings = restored.get_system()["system_config"]
    expected_settings = repository.get_system()["system_config"]
    assert previous_token in restored_settings['retired_rule_proxy_tokens']
    assert {key: value for key, value in restored_settings.items() if key != 'retired_rule_proxy_tokens'} == {
        key: value for key, value in expected_settings.items() if key != 'retired_rule_proxy_tokens'
    }
    assert restored.get_shared()["subscriptions"] == repository.get_shared()["subscriptions"]


def test_desensitized_backup_removes_credentials_without_changing_live_state(tmp_path):
    app, repository = _app_with_secrets(tmp_path)
    response = app.test_client().get("/api/config/export?desensitize=true")
    _assert_rule_proxy_secret_absent(response)
    assert CONFIG_TOKEN.encode() not in response.get_data()
    exported = json.loads(response.get_data())
    assert "config_token" not in exported["system"]["system_config"]
    assert exported["shared"]["subscriptions"][0]["url"] == REDACTED
    assert repository.get_system()["system_config"]["rule_proxy_token"] == RULE_PROXY_SECRET


def test_profile_export_and_explicit_settings_responses_do_not_leak_rule_proxy_token(tmp_path):
    app, _ = _app_with_secrets(tmp_path)
    client = app.test_client()

    responses = [
        client.get("/api/profiles/default/export"),
        client.get("/api/server-domain"),
        client.get("/api/config-token"),
        client.get("/api/backup/config"),
        client.get("/api/settings/subscription-aggregation"),
    ]

    for response in responses:
        _assert_rule_proxy_secret_absent(response)
    assert responses[2].get_json() == {"config_token": CONFIG_TOKEN}


def test_all_ordinary_json_api_responses_scrub_embedded_repository_token(tmp_path):
    app, _ = _app_with_secrets(tmp_path)

    @app.get("/api/synthetic-json")
    def synthetic_json():
        from flask import jsonify

        return jsonify(
            {
                "rule_proxy_token": RULE_PROXY_SECRET,
                "arbitrary": urllib.parse.quote_plus(RULE_PROXY_SECRET, safe=""),
            }
        )

    client = app.test_client()
    responses = [
        client.get("/api/server-domain"),
        client.get("/api/subscriptions"),
        client.get("/api/nodes"),
        client.get("/api/profiles/default/export"),
        client.get("/api/synthetic-json"),
    ]

    for response in responses:
        _assert_rule_proxy_secret_absent(response)


def test_config_token_management_scrubs_legacy_value_embedding_internal_token(tmp_path):
    app, repository = _app_with_secrets(tmp_path)
    managed_value = f"managed::{RULE_PROXY_SECRET}"
    repository.save_system({"system_config": {"config_token": managed_value}})

    response = app.test_client().get("/api/config-token")

    assert response.status_code == 200
    assert response.get_json() == {"config_token": REDACTED}
    assert RULE_PROXY_SECRET not in response.get_data(as_text=True)


def test_agent_tokens_are_centrally_scrubbed_from_api_profile_export_and_mcp(tmp_path, monkeypatch):
    app, repository = _app_with_secrets(tmp_path)
    from backend.common.agent_manager import init_agent_manager

    registered = init_agent_manager().register_agent(
        {"name": "central-secret", "host": "10.0.0.30"}
    )
    agent_token = registered["token"]
    encoded = _encode_layers(agent_token, 10, "mixed")
    monkeypatch.setattr("backend.mcp_server.server.has_tool", lambda name: True)
    monkeypatch.setattr(
        "backend.mcp_server.server.call_tool",
        lambda name, arguments: {
            "agent": registered["agent"],
            f"key::{encoded}": f"value::{encoded}",
        },
    )

    responses = [
        app.test_client().get("/api/agents"),
        app.test_client().get(f"/api/agents/{registered['id']}"),
        app.test_client().get("/api/profiles/default/export"),
        _mcp_call(app.test_client(), "synthetic_agent_payload"),
    ]
    for response in responses:
        assert response.status_code == 200
        assert agent_token not in response.get_data(as_text=True)
        assert encoded not in response.get_data(as_text=True)
    assert repository.get_system()["agents"][0]["token"] == agent_token


def test_retired_internal_token_is_scrubbed_from_keys_and_values_after_multiple_restarts(tmp_path):
    repository = ProfileRepository(tmp_path)
    system = repository.get_system()
    system["system_config"]["retired_rule_proxy_tokens"] = [RULE_PROXY_SECRET]
    repository.save_system(system)

    for restart in range(3):
        repository = ProfileRepository(tmp_path)
        config_module.set_repository(repository)
        encoded = _encode_layers(RULE_PROXY_SECRET, 12, "mixed")
        payload = {
            f"field::{encoded}": f"value::{encoded}",
            "retired_rule_proxy_tokens": [RULE_PROXY_SECRET],
            "safe": restart,
        }
        serialized = json.dumps(sanitize_external_payload(payload), ensure_ascii=False)
        _assert_all_secret_forms_absent(serialized)
        assert "retired_rule_proxy_tokens" not in serialized
        assert json.loads(serialized)["safe"] == restart


def test_webdav_backup_contains_full_restorable_system_and_shared_data(tmp_path, monkeypatch):
    app, _ = _app_with_secrets(tmp_path)
    uploaded = {}

    class FakeClient:
        def __init__(self, options):
            self.options = options

        def check(self, path):
            return True

        def upload_sync(self, *, remote_path, local_path):
            uploaded["remote_path"] = remote_path
            with open(local_path, "rb") as handle:
                uploaded["content"] = handle.read()

    package = types.ModuleType("webdav3")
    client_module = types.ModuleType("webdav3.client")
    client_module.Client = FakeClient
    monkeypatch.setitem(sys.modules, "webdav3", package)
    monkeypatch.setitem(sys.modules, "webdav3.client", client_module)

    response = app.test_client().post(
        "/api/backup/now",
        json={
            "webdav_url": "https://dav.test",
            "webdav_username": "user",
            "webdav_password": "password",
            "webdav_path": "/backups/",
        },
    )

    assert response.status_code == 200
    exported = json.loads(uploaded["content"])
    assert exported["system"]["system_config"]["rule_proxy_token"] == RULE_PROXY_SECRET
    assert exported["system"]["system_config"]["config_token"] == CONFIG_TOKEN
    restored = ProfileRepository(tmp_path / "webdav-restored")
    restored.import_all(exported)
    assert restored.get_shared()["subscriptions"] == exported["shared"]["subscriptions"]


def test_rule_proxy_authentication_still_uses_migrated_internal_token(tmp_path, monkeypatch):
    app, repository = _app_with_secrets(tmp_path)
    current_token = repository.get_system()["system_config"]["rule_proxy_token"]
    monkeypatch.setattr(
        "socket.getaddrinfo",
        lambda *args, **kwargs: [(2, 1, 6, "", ("93.184.216.34", 443))],
    )
    monkeypatch.setattr(
        "backend.routes.mosdns._fetch_remote_content",
        lambda url: "domain:internal-path-still-works",
    )

    response = app.test_client().get(
        "/api/mosdns/rule-proxy",
        query_string={
            "url": "https://example.com/rules.txt",
            "token": current_token,
        },
    )

    assert response.status_code == 200
    assert response.get_data(as_text=True) == "domain:internal-path-still-works"


def test_legacy_unicode_token_survives_restarts_and_is_scrubbed_from_api_and_mcp(
    tmp_path, monkeypatch
):
    token = "旧令牌 +/&?=秘密🔐"
    encoded = _encode_layers(token, 20, "mixed")
    legacy = {
        "system_config": {
            "rule_proxy_token": token,
            "config_token": CONFIG_TOKEN,
            "server_domain": f"https://config.test/{encoded}",
        },
        "subscriptions": [],
        "nodes": [],
    }
    (tmp_path / "config.json").write_text(
        json.dumps(legacy, ensure_ascii=False), encoding="utf-8"
    )
    uploads = []

    class FakeClient:
        def __init__(self, options):
            self.options = options

        def check(self, path):
            return True

        def upload_sync(self, *, remote_path, local_path):
            with open(local_path, "rb") as handle:
                uploads.append(handle.read())

    package = types.ModuleType("webdav3")
    client_module = types.ModuleType("webdav3.client")
    client_module.Client = FakeClient
    monkeypatch.setitem(sys.modules, "webdav3", package)
    monkeypatch.setitem(sys.modules, "webdav3.client", client_module)
    monkeypatch.setattr("backend.mcp_server.server.has_tool", lambda name: True)
    monkeypatch.setattr(
        "backend.mcp_server.server.call_tool",
        lambda name, arguments: {
            "rule_proxy_token": token,
            "result": f"prefix::{encoded}::suffix",
        },
    )
    monkeypatch.setattr(
        "backend.routes.mosdns._fetch_remote_content", lambda url: "domain:restart-ok"
    )
    monkeypatch.setattr(
        "socket.getaddrinfo",
        lambda *args, **kwargs: [(2, 1, 6, "", ("93.184.216.34", 443))],
    )

    for restart in range(3):
        repository = ProfileRepository(tmp_path)
        config_module.set_repository(repository)
        before = copy.deepcopy(repository.get_system())
        assert before["system_config"]["rule_proxy_token"] == token

        app = Flask(__name__)
        register_blueprints(app)
        app.register_blueprint(mcp_bp)

        @app.get(f"/api/restart-{restart}")
        def synthetic_restart_payload(encoded_value=encoded):
            from flask import jsonify

            return jsonify(
                {
                    "rule_proxy_token": token,
                    "result": f"prefix::{encoded_value}::suffix",
                }
            )

        client = app.test_client()
        api_response = client.get(f"/api/restart-{restart}")
        assert api_response.get_json() == {"result": REDACTED}

        mcp_response = _mcp_call(client, "synthetic_restart_payload")
        _assert_rule_proxy_secret_absent(mcp_response)
        assert token.encode() not in mcp_response.get_data()
        assert REDACTED.encode() in mcp_response.get_data()

        backup_response = client.post(
            "/api/backup/now",
            json={
                "webdav_url": "https://dav.test",
                "webdav_username": "user",
                "webdav_password": "password",
                "webdav_path": "/backups/",
            },
        )
        assert backup_response.status_code == 200
        backup = json.loads(uploads[-1])
        assert backup["system"]["system_config"]["rule_proxy_token"] == token
        assert backup["system"]["system_config"]["server_domain"] == legacy["system_config"]["server_domain"]

        auth_response = client.get(
            "/api/mosdns/rule-proxy",
            query_string={"url": "https://example.com/rules", "token": token},
        )
        assert auth_response.status_code == 200
        assert auth_response.get_data(as_text=True) == "domain:restart-ok"
        assert repository.get_system() == before


def _mcp_call(client, name, arguments=None):
    response = client.post(
        "/mcp",
        headers={"Authorization": f"Bearer {CONFIG_TOKEN}"},
        json={
            "jsonrpc": "2.0",
            "id": name,
            "method": "tools/call",
            "params": {"name": name, "arguments": arguments or {}},
        },
    )
    assert response.status_code == 200
    return response


def test_mcp_globally_redacts_rule_proxy_token_from_nested_tool_payload(tmp_path, monkeypatch):
    app, _ = _app_with_secrets(tmp_path)
    monkeypatch.setattr("backend.mcp_server.server.has_tool", lambda name: True)
    monkeypatch.setattr(
        "backend.mcp_server.server.call_tool",
        lambda name, arguments: {
            "rule_proxy_token": RULE_PROXY_SECRET,
            "nested": [f"https://config.test/rules?token={RULE_PROXY_SECRET}"],
            "config_token": CONFIG_TOKEN,
        },
    )

    response = _mcp_call(app.test_client(), "synthetic_secret_payload")

    _assert_rule_proxy_secret_absent(response)
    assert CONFIG_TOKEN.encode() in response.get_data()


@pytest.mark.parametrize(
    ("tool_name", "arguments"),
    [
        ("manage_config_backup", {"action": "export", "desensitize": False}),
        ("manage_config_backup", {"action": "export", "desensitize": True}),
        ("get_settings", {}),
        ("preview_config", {"target": "mosdns", "base_url": "https://config.test"}),
    ],
)
def test_mcp_config_and_settings_responses_do_not_leak_rule_proxy_token(
    tmp_path, tool_name, arguments
):
    app, _ = _app_with_secrets(tmp_path)

    response = _mcp_call(app.test_client(), tool_name, arguments)

    _assert_rule_proxy_secret_absent(response)
    if tool_name != "preview_config" and not arguments.get("desensitize"):
        assert CONFIG_TOKEN.encode() in response.get_data()
