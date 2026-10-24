import copy
import io


import pytest
from flask import Flask, Request

from backend.common import auth as auth_module
from backend.common import config as config_module
from backend.common.agent_manager import get_agent_manager, init_agent_manager
from backend.common.auth import MAX_AUTH_TOKEN_LENGTH
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints


def _heartbeat_app(tmp_path):
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    manager = init_agent_manager()
    registered = manager.register_agent({"name": "secured", "host": "127.0.0.1"})
    app = Flask(__name__)
    register_blueprints(app)
    return app, repository, registered


@pytest.mark.parametrize(
    "authorization",
    [None, "Basic ignored", "Bearer wrong-token"],
    ids=["missing", "wrong-scheme", "wrong-token"],
)
def test_heartbeat_rejects_unauthorized_without_mutating_system(tmp_path, authorization):
    app, repository, registered = _heartbeat_app(tmp_path)
    before = copy.deepcopy(repository.get_system())
    headers = {"Authorization": authorization} if authorization else {}

    response = app.test_client().post(
        f"/api/agents/{registered['id']}/heartbeat",
        headers=headers,
        json={"version": "attacker", "config_version": "attacker"},
    )

    assert response.status_code == 401
    assert repository.get_system() == before


@pytest.mark.parametrize(
    "authorization",
    [
        "Bearer 令牌",
        "Bearer café",
        "Bearer token with spaces",
        "Bearer  token",
        "Bearer\twrong",
        f"Bearer {'x' * (MAX_AUTH_TOKEN_LENGTH + 1)}",
    ],
    ids=["unicode", "latin-unicode", "trailing-field", "double-space", "tab", "oversized"],
)
def test_heartbeat_rejects_non_ascii_or_malformed_bearer_without_mutating_system(
    tmp_path, authorization
):
    app, repository, registered = _heartbeat_app(tmp_path)
    before = copy.deepcopy(repository.get_system())

    response = app.test_client().post(
        f"/api/agents/{registered['id']}/heartbeat",
        headers={"Authorization": authorization},
        json={"version": "attacker"},
    )

    assert response.status_code == 401
    assert repository.get_system() == before


def test_heartbeat_accepts_matching_bearer_token(tmp_path):
    app, repository, registered = _heartbeat_app(tmp_path)

    response = app.test_client().post(
        f"/api/agents/{registered['id']}/heartbeat",
        headers={"Authorization": f"Bearer {registered['token']}"},
        json={"version": "2.0.0", "config_version": "cfg-2", "service_status": "running"},
    )

    assert response.status_code == 200
    # Heartbeat state lives in memory and is read through the manager.
    agent = get_agent_manager().get_agent_by_id(registered["id"])
    assert agent["version"] == "2.0.0"
    assert agent["config_version"] == "cfg-2"
    assert agent["service_status"] == "running"


def test_agent_token_is_redacted_from_ordinary_outputs_and_still_authorizes_heartbeat(tmp_path):
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    init_agent_manager()
    app = Flask(__name__)
    register_blueprints(app)
    client = app.test_client()

    registered_response = client.post(
        "/api/agents/register", json={"name": "one-time", "host": "10.0.0.8"}
    )
    assert registered_response.status_code == 200
    registered = registered_response.get_json()
    token = registered["token"]
    agent_id = registered["id"]
    assert token
    assert set(registered) == {"success", "id", "status", "is_new", "token"}

    ordinary_responses = [
        client.get("/api/agents"),
        client.get(f"/api/agents/{agent_id}"),
        client.get(f"/api/agents/{agent_id}/status"),
        client.get("/api/config/export?desensitize=true"),
        client.get("/api/profiles/default/export"),
    ]
    for response in ordinary_responses:
        assert response.status_code == 200
        assert token not in response.get_data(as_text=True)

    heartbeat = client.post(
        f"/api/agents/{agent_id}/heartbeat",
        headers={"Authorization": f"Bearer {token}"},
        json={"version": "2.1.0"},
    )
    assert heartbeat.status_code == 200
    assert token not in heartbeat.get_data(as_text=True)


def test_authenticated_global_backup_preserves_secrets_for_recovery(tmp_path, monkeypatch):
    monkeypatch.setattr(auth_module, "ADMIN_USERNAME", "admin")
    monkeypatch.setattr(auth_module, "ADMIN_PASSWORD", "admin-password")
    monkeypatch.delenv("AGENT_REGISTRATION_KEY", raising=False)
    repository = ProfileRepository(tmp_path / "source")
    repository.create_profile({"id": "alpha", "name": "Alpha"})
    repository.create_profile({"id": "beta", "name": "Beta"})
    repository.save_shared({"subscriptions": [{
        "id": "sub-1", "name": "Shared", "url": "https://example.test/sub?token=subscription-secret",
    }]})
    repository.save_profile("alpha", {
        "proxy_groups": [{"id": "shared", "name": "Shared", "type": "select", "subscriptions": ["sub-1"]}],
        "mihomo": {"custom_config": "log-level: debug"},
    })
    backup_settings = {
        "webdav_url": "https://backup.test", "webdav_username": "backup-user",
        "webdav_password": "webdav-secret-for-recovery",
    }
    repository.update_system_transaction(lambda system: system.update({"backup": backup_settings}))
    config_module.set_repository(repository)
    registered = init_agent_manager().register_agent({
        "name": "recoverable", "host": "10.0.0.8", "profile_id": "alpha",
    })
    app = Flask(__name__)
    register_blueprints(app)
    from backend.routes.auth import setup_before_request
    setup_before_request(app)
    client = app.test_client()
    admin_headers = {
        "Authorization": f"Bearer {auth_module.generate_token('admin')}",
        "X-ConfigFlow-Profile": "alpha",
    }

    for headers in ({}, {"Authorization": "Bearer wrong-token"},
                    {"Authorization": f"Bearer {registered['token']}"}):
        denied = client.get("/api/config/export", headers=headers)
        assert denied.status_code == 401
        assert registered["token"] not in denied.get_data(as_text=True)
        assert backup_settings["webdav_password"] not in denied.get_data(as_text=True)

    response = client.get("/api/config/export", headers=admin_headers)
    assert response.status_code == 200
    assert response.mimetype == "application/json"
    assert response.headers["Content-Disposition"].startswith("attachment;")
    exported = response.get_json()
    assert exported["schema_version"] == 5
    assert set(exported["profiles"]) == {"default", "alpha", "beta"}
    assert exported["shared"]["subscriptions"] == repository.get_shared()["subscriptions"]
    assert exported["profiles"]["alpha"]["proxy_groups"][0]["subscriptions"] == ["sub-1"]
    assert "resource_refs" not in exported["profiles"]["alpha"]
    assert "node_dialers" not in exported["profiles"]["alpha"]
    assert "subscriptions" not in exported["profiles"]["alpha"]
    assert exported["system"]["agents"][0]["token"] == registered["token"]
    assert exported["system"]["backup"] == backup_settings

    for path in (
        "/api/agents", f"/api/agents/{registered['id']}",
        f"/api/agents/{registered['id']}/status", "/api/backup/config",
        "/api/profiles/alpha/export", "/api/config/export?desensitize=true",
    ):
        ordinary = client.get(path, headers=admin_headers)
        assert ordinary.status_code == 200
        assert registered["token"] not in ordinary.get_data(as_text=True)
        assert backup_settings["webdav_password"] not in ordinary.get_data(as_text=True)

    restored = ProfileRepository(tmp_path / "restored")
    restored.import_all(exported)
    assert restored.get_system()["agents"] == repository.get_system()["agents"]
    assert restored.get_system()["backup"] == backup_settings
    assert restored.get_shared()["subscriptions"] == exported["shared"]["subscriptions"]
    assert restored.get_profile("alpha")["proxy_groups"] == exported["profiles"]["alpha"]["proxy_groups"]
    assert restored.get_profile("alpha")["mihomo"]["custom_config"] == "log-level: debug"
    config_module.set_repository(restored)
    init_agent_manager()
    heartbeat = client.post(
        f"/api/agents/{registered['id']}/heartbeat",
        headers={"Authorization": f"Bearer {registered['token']}"},
        json={"version": "restored"},
    )
    assert heartbeat.status_code == 200
    assert get_agent_manager().get_agent_by_id(registered["id"])["version"] == "restored"
    # A changed version is written to the restored configuration.
    assert restored.get_system()["agents"][0]["version"] == "restored"
    assert registered["token"] not in heartbeat.get_data(as_text=True)


def test_manual_agent_create_does_not_return_token_even_without_auth(tmp_path):
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    init_agent_manager()
    app = Flask(__name__)
    register_blueprints(app)

    response = app.test_client().post(
        "/api/agents", json={"name": "manual", "host": "10.0.0.9"}
    )

    assert response.status_code == 200
    persisted_token = repository.get_system()["agents"][0]["token"]
    assert persisted_token not in response.get_data(as_text=True)


@pytest.mark.parametrize(
    "payload",
    [
        [],
        {"unexpected": "value"},
        {"version": 2},
        {"version": "v" * 129},
        {"config_version": None},
        {"config_version": "c" * 129},
        {"service_status": False},
        {"service_status": "s" * 65},
        {"system_metrics": []},
        {"system_metrics": {"unexpected": {}}},
        {"system_metrics": {"cpu": {"unexpected": 1}}},
        {"system_metrics": {"cpu": {"usage_percent": "high"}}},
        {"system_metrics": {"memory": {"total": -1}}},
        {"system_metrics": {"collected_at": "t" * 65}},
    ],
)
def test_heartbeat_rejects_unapproved_or_malformed_fields_without_mutation(tmp_path, payload):
    app, repository, registered = _heartbeat_app(tmp_path)
    before = copy.deepcopy(repository.get_system())

    response = app.test_client().post(
        f"/api/agents/{registered['id']}/heartbeat",
        headers={"Authorization": f"Bearer {registered['token']}"},
        json=payload,
    )

    assert response.status_code == 400
    assert repository.get_system() == before


def test_heartbeat_rejects_malformed_json_without_mutation(tmp_path):
    app, repository, registered = _heartbeat_app(tmp_path)
    before = copy.deepcopy(repository.get_system())

    response = app.test_client().post(
        f"/api/agents/{registered['id']}/heartbeat",
        headers={
            "Authorization": f"Bearer {registered['token']}",
            "Content-Type": "application/json",
        },
        data=b'{"version":',
    )

    assert response.status_code == 400
    assert repository.get_system() == before


def test_heartbeat_rejects_oversized_body_without_mutation(tmp_path):
    app, repository, registered = _heartbeat_app(tmp_path)
    before = copy.deepcopy(repository.get_system())

    response = app.test_client().post(
        f"/api/agents/{registered['id']}/heartbeat",
        headers={
            "Authorization": f"Bearer {registered['token']}",
            "Content-Type": "application/json",
        },
        data=b" " * 32769,
    )

    assert response.status_code == 413
    assert repository.get_system() == before


class CountingStream(io.BytesIO):
    def __init__(self, payload):
        super().__init__(payload)
        self.bytes_returned = 0

    def read(self, size=-1):
        result = super().read(size)
        self.bytes_returned += len(result)
        return result

    def readinto(self, buffer):
        count = super().readinto(buffer)
        self.bytes_returned += count or 0
        return count


def _post_stream_without_content_length(app, registered, stream):
    return app.test_client().open(
        f"/api/agents/{registered['id']}/heartbeat",
        method="POST",
        headers={
            "Authorization": f"Bearer {registered['token']}",
            "Content-Type": "application/json",
        },
        environ_overrides={
            "wsgi.input": stream,
            "wsgi.input_terminated": True,
            "CONTENT_LENGTH": "",
        },
    )


def test_heartbeat_without_content_length_reads_only_max_plus_one_bytes(tmp_path, monkeypatch):
    app, repository, registered = _heartbeat_app(tmp_path)
    before = copy.deepcopy(repository.get_system())
    stream = CountingStream(b" " * (1024 * 1024))

    def fail_get_data(*args, **kwargs):
        raise AssertionError("heartbeat must not call Request.get_data")

    monkeypatch.setattr(Request, "get_data", fail_get_data)
    response = _post_stream_without_content_length(app, registered, stream)

    assert response.status_code == 413
    assert stream.bytes_returned == 32769
    assert repository.get_system() == before


def test_heartbeat_accepts_chunked_json_without_content_length(tmp_path):
    app, repository, registered = _heartbeat_app(tmp_path)
    stream = CountingStream(b'{"version":"chunked"}')

    response = _post_stream_without_content_length(app, registered, stream)

    assert response.status_code == 200
    assert stream.bytes_returned == len(b'{"version":"chunked"}')
    # Heartbeat state lives in memory and is read through the manager.
    agent = get_agent_manager().get_agent_by_id(registered["id"])
    assert agent["version"] == "chunked"


@pytest.mark.parametrize("raw_body", [b"\xff", b'{"version":'])
def test_heartbeat_rejects_malformed_encoding_or_json_without_mutation(tmp_path, raw_body):
    app, repository, registered = _heartbeat_app(tmp_path)
    before = copy.deepcopy(repository.get_system())

    response = _post_stream_without_content_length(app, registered, CountingStream(raw_body))

    assert response.status_code == 400
    assert repository.get_system() == before


def test_heartbeat_accepts_whitelisted_system_metrics(tmp_path):
    app, repository, registered = _heartbeat_app(tmp_path)
    metrics = {
        "cpu": {"usage_percent": 12.5, "core_count": 4},
        "memory": {"total": 1024, "used": 512, "available": 512, "used_percent": 50.0},
        "disk": {"total": 2048, "used": 1024, "free": 1024, "used_percent": 50.0},
        "network": {"bytes_sent": 10, "bytes_recv": 20, "speed_sent": 1, "speed_recv": 2},
        "collected_at": "2026-08-31T00:00:00Z",
    }

    response = app.test_client().post(
        f"/api/agents/{registered['id']}/heartbeat",
        headers={"Authorization": f"Bearer {registered['token']}"},
        json={"system_metrics": metrics},
    )

    assert response.status_code == 200
    # Heartbeat state lives in memory and is read through the manager.
    agent = get_agent_manager().get_agent_by_id(registered["id"])
    assert agent["system_metrics"] == metrics
