from flask import Flask

from backend.common import config as config_module
from backend.common.agent_manager import init_agent_manager
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints


def setup_agent_app(tmp_path, monkeypatch):
    repository = ProfileRepository(tmp_path)
    repository.create_profile({"id": "alpha", "name": "Alpha"})
    repository.create_profile({"id": "beta", "name": "Beta"})
    config_module.set_repository(repository)
    init_agent_manager()
    app = Flask(__name__)
    register_blueprints(app)
    return app, repository


def test_agents_are_system_scoped_and_bind_profiles(tmp_path, monkeypatch):
    app, repository = setup_agent_app(tmp_path, monkeypatch)
    manager = init_agent_manager()

    legacy_result = manager.register_agent({"name": "legacy", "host": "127.0.0.1"})
    legacy = next(agent for agent in repository.get_system()["agents"] if agent["id"] == legacy_result["id"])
    bound_result = manager.register_agent(
        {"name": "bound", "host": "127.0.0.2", "profile_id": "alpha"}
    )
    bound = next(agent for agent in repository.get_system()["agents"] if agent["id"] == bound_result["id"])

    agents = repository.get_system()["agents"]
    assert next(agent for agent in agents if agent["id"] == legacy["id"])["profile_id"] == "default"
    assert next(agent for agent in agents if agent["id"] == bound["id"])["profile_id"] == "alpha"


def test_agent_config_endpoint_uses_bound_profile(tmp_path, monkeypatch):
    app, repository = setup_agent_app(tmp_path, monkeypatch)
    repository.save_profile("alpha", {"mihomo": {"custom_config": "log-level: debug"}})
    repository.save_profile("beta", {"mihomo": {"custom_config": "log-level: warning"}})
    manager = init_agent_manager()
    result = manager.register_agent(
        {"name": "bound", "host": "127.0.0.2", "profile_id": "alpha"}
    )
    monkeypatch.setattr(
        "backend.routes.agents.generate_agent_config",
        lambda data, agent, **kwargs: {
            "content": data["mihomo"]["custom_config"],
            "md5": "hash",
            "version": "hash",
        },
    )

    response = app.test_client().get(
        f"/api/agents/{result['id']}/config?token={result['token']}",
        headers={"X-ConfigFlow-Profile": "beta"},
    )

    assert response.status_code == 200
    assert response.get_json()["content"] == "log-level: debug"
    assert response.get_json()["profile_id"] == "alpha"


def test_push_config_uses_bound_profile_even_with_other_request_context(tmp_path, monkeypatch):
    app, repository = setup_agent_app(tmp_path, monkeypatch)
    repository.save_profile("alpha", {"mihomo": {"custom_config": "log-level: debug"}})
    repository.save_profile("beta", {"mihomo": {"custom_config": "log-level: warning"}})
    manager = init_agent_manager()
    result = manager.register_agent(
        {"name": "bound", "host": "127.0.0.2", "profile_id": "alpha"}
    )
    seen = {}

    monkeypatch.setattr(
        "backend.routes.agents.generate_mihomo_config",
        lambda data, **kwargs: seen.setdefault("custom_config", data["mihomo"]["custom_config"]),
    )
    monkeypatch.setattr("backend.routes.agents.get_mihomo_provider_downloads", lambda *args, **kwargs: [])
    monkeypatch.setattr("backend.routes.agents.get_mihomo_ruleset_downloads", lambda *args, **kwargs: [])
    monkeypatch.setattr(
        manager,
        "push_config_to_agent",
        lambda agent_id, content, extra_data=None, config_revision=None: {"success": True},
    )

    response = app.test_client().post(
        f"/api/agents/{result['id']}/push-config",
        headers={"X-ConfigFlow-Profile": "beta"},
        json={"restart": False},
    )

    assert response.status_code == 200
    assert seen["custom_config"] == "log-level: debug"
    assert response.get_json()["profile_id"] == "alpha"


def test_agent_registration_does_not_overwrite_system_agents_from_profile_snapshot(tmp_path, monkeypatch):
    app, repository = setup_agent_app(tmp_path, monkeypatch)
    manager = init_agent_manager()
    repository.update_system_transaction(
        lambda system: system.update({
            "agents": [{"id": "stable", "name": "stable", "host": "10.0.0.1", "profile_id": "default"}],
        })
    )

    with app.test_request_context("/", headers={"X-ConfigFlow-Profile": "alpha"}):
        manager.get_all_agents()
        repository.update_system_transaction(
            lambda system: system.update({"agents": [
                {"id": "stable", "name": "stable", "host": "10.0.0.1", "profile_id": "default"},
                {"id": "concurrent", "name": "concurrent", "host": "10.0.0.2", "profile_id": "beta"},
            ]}),
        )
        manager.register_agent({"name": "new", "host": "10.0.0.3", "profile_id": "alpha"})

    assert {agent["id"] for agent in repository.get_system()["agents"]} >= {"stable", "concurrent"}


def test_agent_mutations_persist_through_system_transactions(tmp_path, monkeypatch):
    _app, repository = setup_agent_app(tmp_path, monkeypatch)
    manager = init_agent_manager()
    registered = manager.register_agent({"name": "managed", "host": "10.0.0.9", "profile_id": "alpha"})
    agent_id = registered["id"]

    assert next(agent for agent in repository.get_system()["agents"] if agent["id"] == agent_id)["name"] == "managed"

    assert manager.update_agent(agent_id, {"name": "updated", "profile_id": "beta"})
    assert manager.update_heartbeat(agent_id, {"version": "2.0.0", "config_version": "heartbeat"})
    persisted = next(agent for agent in repository.get_system()["agents"] if agent["id"] == agent_id)
    assert persisted["name"] == "updated"
    assert persisted["profile_id"] == "beta"
    assert persisted["config_version"] == "heartbeat"
    # A changed version is persisted so a restart does not fall back to the registered one.
    assert persisted["version"] == "2.0.0"

    class SuccessfulResponse:
        status_code = 200

        @staticmethod
        def json():
            return {"success": True}

    monkeypatch.setattr("backend.agents.manager.requests.post", lambda *args, **kwargs: SuccessfulResponse())
    assert manager.push_config_to_agent(agent_id, "new config")["http_status"] == 409
    persisted = next(agent for agent in repository.get_system()["agents"] if agent["id"] == agent_id)
    assert persisted["config_version"] == "heartbeat"

    assert manager.delete_agent(agent_id)
    assert all(agent["id"] != agent_id for agent in repository.get_system()["agents"])


def test_agent_registration_validates_profile_inside_system_transaction(tmp_path, monkeypatch):
    repository = ProfileRepository(tmp_path)
    repository.create_profile({"id": "alpha"})
    manager = __import__("backend.agents.manager", fromlist=["AgentManager"]).AgentManager(repository)
    original_transaction = repository.update_system_transaction

    def delete_profile_then_run(updater):
        repository.delete_profile("alpha")
        return original_transaction(updater)

    monkeypatch.setattr(repository, "update_system_transaction", delete_profile_then_run)
    with __import__("pytest").raises(Exception):
        manager.register_agent({"name": "race", "host": "127.0.0.9", "profile_id": "alpha"})

    assert repository.get_system()["agents"] == []
