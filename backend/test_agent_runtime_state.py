"""Heartbeats are kept in memory and laid over the registered Agent; only a changed version is written."""
from datetime import datetime, timedelta
import json
import logging
import sqlite3
import threading

from flask import Flask
import pytest

from backend.agents import presence_log, runtime_state
from backend.agents.manager import AgentManager
from backend.common import config as config_module
from backend.common.agent_manager import init_agent_manager
from backend.common.config_repository import ProfileRepository
from backend.routes import register_blueprints

METRICS = {
    "cpu": {"usage_percent": 12.5, "core_count": 4},
    "memory": {"total": 1024, "used": 512, "available": 512, "used_percent": 50.0},
    "disk": {"total": 2048, "used": 1024, "free": 1024, "used_percent": 50.0},
    "network": {"bytes_sent": 10, "bytes_recv": 20, "speed_sent": 1, "speed_recv": 2},
}


def setup(tmp_path):
    repository = ProfileRepository(tmp_path)
    config_module.set_repository(repository)
    manager = init_agent_manager()
    agent = manager.register_agent({"name": "edge", "host": "192.0.2.10", "version": "1.0.0"})
    return repository, manager, agent


def stored(repository, agent_id):
    return next(item for item in repository.get_system()["agents"] if item["id"] == agent_id)


def revision(repository):
    return repository.get_system()["_revision"]


def age_registration(repository, minutes=10):
    old = (datetime.now() - timedelta(minutes=minutes)).isoformat()

    def update(system):
        for agent in system["agents"]:
            agent["last_heartbeat"] = old
    repository.update_system_transaction(update)
    return old


def restart(repository):
    """A new process: memory is gone, the configuration document stays."""
    runtime_state.reset()
    return AgentManager(ProfileRepository(repository.data_dir))


def test_heartbeat_with_known_version_writes_nothing_and_is_visible_through_the_manager(tmp_path):
    repository, manager, agent = setup(tmp_path)
    before, current_revision = repository._store.fingerprint(), revision(repository)

    for _ in range(3):
        assert manager.update_heartbeat(agent["id"], {"version": "1.0.0", "service_status": "running",
                                                      "config_version": "0", "system_metrics": METRICS})

    assert repository._store.fingerprint() == before
    assert revision(repository) == current_revision
    assert not (tmp_path / "runtime.sqlite3").exists()
    for current in (manager.get_agent_by_id(agent["id"]), manager.get_all_agents()[0]):
        assert current["status"] == "online" and current["service_status"] == "running"
        assert current["system_metrics"] == METRICS
        assert {key: current[key] for key in ("cpu", "memory", "disk", "network")} == {key: METRICS[key] for key in ("cpu", "memory", "disk", "network")}


def test_a_changed_version_is_written_once_together_with_config_version(tmp_path):
    repository, manager, agent = setup(tmp_path)
    current_revision, registered = revision(repository), stored(repository, agent["id"])["last_heartbeat"]

    assert manager.update_heartbeat(agent["id"], {"version": "2.0.0", "config_version": "cfg-2"})
    assert revision(repository) == current_revision + 1
    assert stored(repository, agent["id"])["version"] == "2.0.0"
    assert stored(repository, agent["id"])["config_version"] == "cfg-2"
    # The registration time stays; liveness is not configuration.
    assert stored(repository, agent["id"])["last_heartbeat"] == registered

    assert manager.update_heartbeat(agent["id"], {"version": "2.0.0", "config_version": "cfg-2"})
    assert manager.update_heartbeat(agent["id"], {})
    assert revision(repository) == current_revision + 1

    assert manager.update_heartbeat(agent["id"], {"config_version": "cfg-3"})
    assert revision(repository) == current_revision + 2
    assert stored(repository, agent["id"])["version"] == "2.0.0"


def test_returned_agents_are_copies_of_the_remembered_state(tmp_path):
    _, manager, agent = setup(tmp_path)
    assert manager.update_heartbeat(agent["id"], {"system_metrics": METRICS})
    manager.get_agent_by_id(agent["id"])["system_metrics"]["cpu"]["usage_percent"] = 99
    assert manager.get_agent_by_id(agent["id"])["system_metrics"] == METRICS


def test_partial_heartbeats_keep_earlier_values_and_inherit_registered_state(tmp_path):
    repository, manager, agent = setup(tmp_path)
    repository.update_system_transaction(lambda system: system["agents"][0].update(service_status="running"))
    stamp = datetime.fromisoformat(manager.get_agent_by_id(agent["id"])["last_heartbeat"])

    assert manager.update_heartbeat(agent["id"], {})
    first = manager.get_agent_by_id(agent["id"])
    # The first heartbeat starts from what the configuration already held.
    assert first["version"] == "1.0.0" and first["service_status"] == "running"
    assert datetime.fromisoformat(first["last_heartbeat"]) >= stamp

    assert manager.update_heartbeat(agent["id"], {"system_metrics": METRICS})
    assert manager.update_heartbeat(agent["id"], {"version": "2.0.0"})
    current = manager.get_agent_by_id(agent["id"])
    assert current["version"] == "2.0.0" and current["system_metrics"] == METRICS


def test_re_registration_wins_until_the_next_heartbeat(tmp_path):
    _, manager, agent = setup(tmp_path)
    assert manager.update_heartbeat(agent["id"], {"version": "2.0.0", "system_metrics": METRICS})

    manager.register_agent({"name": "edge", "host": "192.0.2.10", "version": "3.0.0"}, existing_token=agent["token"])
    current = manager.get_agent_by_id(agent["id"])
    assert current["version"] == "3.0.0" and "system_metrics" not in current

    # The next heartbeat builds on the registration, not on the older remembered metrics.
    assert manager.update_heartbeat(agent["id"], {})
    current = manager.get_agent_by_id(agent["id"])
    assert current["version"] == "3.0.0" and "system_metrics" not in current


def test_unknown_agent_writes_and_remembers_nothing(tmp_path):
    repository, manager, _ = setup(tmp_path)
    before = repository._store.fingerprint()
    assert not manager.update_heartbeat("missing", {"version": "2.0.0"})
    assert repository._store.fingerprint() == before
    assert manager.runtime._entries == {}


def test_deleting_an_agent_forgets_its_heartbeat(tmp_path):
    _, manager, agent = setup(tmp_path)
    assert manager.update_heartbeat(agent["id"], {"system_metrics": METRICS})
    assert manager.runtime.record_heartbeat({"id": "stray"}, {"version": "9"}, manager.runtime.mark())
    # Only registered Agents are shown.
    assert [item["id"] for item in manager.get_all_agents()] == [agent["id"]]
    assert manager.delete_agent(agent["id"])
    assert set(manager.runtime._entries) == {"stray"}
    assert manager.get_all_agents() == []


def test_restart_keeps_the_reported_version_and_waits_for_the_next_heartbeat(tmp_path):
    repository, manager, agent = setup(tmp_path)
    registered = age_registration(repository)
    assert manager.update_heartbeat(agent["id"], {"version": "2.0.0", "service_status": "running", "system_metrics": METRICS})
    assert manager.get_all_agents()[0]["status"] == "online"

    restarted = restart(repository)
    listed = restarted.get_all_agents()[0]
    assert listed["version"] == "2.0.0"
    assert listed["status"] == "offline" and listed["last_heartbeat"] == registered
    assert "system_metrics" not in listed and "service_status" not in listed
    # get_agent_by_id never derives offline: startup syncs act on registered Agents.
    assert restarted.get_agent_by_id(agent["id"])["status"] == "online"

    assert restarted.update_heartbeat(agent["id"], {"version": "2.0.0", "service_status": "running"})
    listed = restarted.get_all_agents()[0]
    assert listed["status"] == "online" and listed["service_status"] == "running"


def test_every_manager_for_a_data_directory_shares_the_state(tmp_path):
    # Startup builds more than one AgentManager and background loops keep the first.
    repository, first, agent = setup(tmp_path)
    second = AgentManager(ProfileRepository(repository.data_dir))
    assert second.update_heartbeat(agent["id"], {"service_status": "running"})
    assert first.get_agent_by_id(agent["id"])["service_status"] == "running"
    other = AgentManager(ProfileRepository(tmp_path / "other"))
    assert other.runtime is not first.runtime and first.runtime is second.runtime


def test_domain_discovery_toggle_returns_the_live_agent(tmp_path):
    _, manager, agent = setup(tmp_path)
    assert manager.update_heartbeat(agent["id"], {"version": "2.0.0", "system_metrics": METRICS})
    updated = manager.set_domain_discovery(agent["id"], True)
    assert updated["version"] == "2.0.0" and updated["system_metrics"] == METRICS and updated["status"] == "online"


def test_a_newer_heartbeat_in_the_document_wins(tmp_path):
    repository, manager, agent = setup(tmp_path)
    assert manager.update_heartbeat(agent["id"], {"service_status": "running"})
    timestamp, _, fields = manager.runtime._entries[agent["id"]]
    manager.runtime._entries[agent["id"]] = (timestamp - 5, _, fields)
    later = datetime.now().isoformat()
    repository.update_system_transaction(
        lambda system: system["agents"][0].update(last_heartbeat=later, service_status="stopped"))
    current = manager.get_agent_by_id(agent["id"])
    assert current["service_status"] == "stopped" and current["last_heartbeat"] == later


def test_stale_heartbeat_reads_as_offline_only_in_the_list(tmp_path):
    repository, manager, agent = setup(tmp_path)
    age_registration(repository)
    assert manager.update_heartbeat(agent["id"], {})
    timestamp, _, fields = manager.runtime._entries[agent["id"]]
    manager.runtime._entries[agent["id"]] = (timestamp, (datetime.now() - timedelta(minutes=5)).isoformat(), fields)
    assert manager.get_all_agents()[0]["status"] == "offline"
    assert manager.get_agent_by_id(agent["id"])["status"] == "online"


def test_a_registration_time_ahead_of_the_clock_does_not_hide_heartbeats(tmp_path):
    repository, manager, agent = setup(tmp_path)
    ahead = (datetime.now() + timedelta(hours=8)).isoformat()
    repository.update_system_transaction(lambda system: system["agents"][0].update(last_heartbeat=ahead))
    assert manager.update_heartbeat(agent["id"], {"service_status": "running"})
    current = manager.get_agent_by_id(agent["id"])
    assert current["service_status"] == "running"
    assert datetime.fromisoformat(current["last_heartbeat"]) <= datetime.now()


def test_heartbeat_endpoint_records_state_and_rejects_unknown_agents(tmp_path):
    repository, manager, agent = setup(tmp_path)
    app = Flask(__name__)
    register_blueprints(app)
    client = app.test_client()
    headers = {"Authorization": f"Bearer {agent['token']}"}
    current_revision = revision(repository)
    assert client.post(f"/api/agents/{agent['id']}/heartbeat", headers=headers, json={"version": "1.0.0", "service_status": "running"}).status_code == 200
    assert revision(repository) == current_revision
    assert manager.get_agent_by_id(agent["id"])["service_status"] == "running"
    assert client.post(f"/api/agents/{agent['id']}/heartbeat", headers=headers, json={"version": "2.0.0"}).status_code == 200
    assert stored(repository, agent["id"])["version"] == "2.0.0"
    assert client.post("/api/agents/missing/heartbeat", headers=headers, json={}).status_code in (401, 404)


def test_a_failed_version_write_still_records_the_heartbeat_and_retries(tmp_path, monkeypatch):
    repository, manager, agent = setup(tmp_path)
    original = repository.update_system_transaction

    def unavailable(_update):
        raise OSError("disk full")
    monkeypatch.setattr(repository, "update_system_transaction", unavailable)
    with pytest.raises(OSError):
        manager.update_heartbeat(agent["id"], {"version": "2.0.0"})
    assert manager.get_agent_by_id(agent["id"])["version"] == "2.0.0"
    assert stored(repository, agent["id"])["version"] == "1.0.0"

    monkeypatch.setattr(repository, "update_system_transaction", original)
    assert manager.update_heartbeat(agent["id"], {"version": "2.0.0"})
    assert stored(repository, agent["id"])["version"] == "2.0.0"


def test_concurrent_heartbeats_are_all_recorded(tmp_path):
    _, manager, agent = setup(tmp_path)
    start, errors = threading.Barrier(8), []

    def beat(index):
        start.wait()
        try:
            for _ in range(20):
                manager.update_heartbeat(agent["id"], {"service_status": f"s{index}", "system_metrics": METRICS})
                manager.get_all_agents()
        except Exception as error:  # noqa: BLE001 - collected for the assertion
            errors.append(error)
    threads = [threading.Thread(target=beat, args=(index,)) for index in range(8)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()
    assert errors == []
    assert manager.get_agent_by_id(agent["id"])["service_status"].startswith("s")


def test_a_heartbeat_racing_a_delete_leaves_nothing_behind(tmp_path, monkeypatch):
    # The heartbeat looked the Agent up just before delete_agent committed.
    repository, manager, agent = setup(tmp_path)
    record = manager.runtime.record_heartbeat

    def deleted_meanwhile(*args):
        assert manager.delete_agent(agent["id"])
        return record(*args)
    monkeypatch.setattr(manager.runtime, "record_heartbeat", deleted_meanwhile)
    current_revision = revision(repository)

    assert not manager.update_heartbeat(agent["id"], {"version": "2.0.0", "system_metrics": METRICS})
    assert manager.runtime._entries == {}
    assert revision(repository) == current_revision + 1   # the delete only


def test_a_delete_after_the_heartbeat_is_recorded_skips_the_version_write(tmp_path, monkeypatch):
    repository, manager, agent = setup(tmp_path)
    record = manager.runtime.record_heartbeat

    def deleted_afterwards(*args):
        assert record(*args)
        assert manager.delete_agent(agent["id"])
        return True
    monkeypatch.setattr(manager.runtime, "record_heartbeat", deleted_afterwards)
    current_revision = revision(repository)

    assert not manager.update_heartbeat(agent["id"], {"version": "2.0.0", "config_version": "cfg-2"})
    assert manager.runtime._entries == {}
    assert revision(repository) == current_revision + 1
    assert repository.get_system()["agents"] == []


def test_an_agent_restored_after_its_deletion_is_heard_again(tmp_path):
    repository, manager, agent = setup(tmp_path)
    saved = stored(repository, agent["id"])
    assert manager.delete_agent(agent["id"])
    repository.update_system_transaction(lambda system: system["agents"].append(saved))
    assert manager.update_heartbeat(agent["id"], {"service_status": "running"})
    assert manager.get_agent_by_id(agent["id"])["service_status"] == "running"


# --- one-time import of #369's runtime.sqlite3 ---

def legacy_database(path, rows):
    connection = sqlite3.connect(path)
    connection.execute('CREATE TABLE agent_runtime (agent_id TEXT PRIMARY KEY, '
                       'heartbeat_at REAL NOT NULL, last_heartbeat TEXT NOT NULL, fields TEXT NOT NULL)')
    connection.executemany('INSERT INTO agent_runtime VALUES (?, ?, ?, ?)', rows)
    connection.commit()
    connection.close()


def two_agents(tmp_path):
    repository = ProfileRepository(tmp_path)
    manager = AgentManager(repository)
    fresh = manager.register_agent({"name": "fresh", "host": "192.0.2.10", "version": "1.0.0"})
    stale = manager.register_agent({"name": "stale", "host": "192.0.2.11", "version": "1.0.0"})
    age_registration(repository)
    return repository, fresh, stale


def runtime_logs(caplog):
    return [record.levelno for record in caplog.records if record.name == "backend.agents.runtime_state"]


def legacy_row(agent_id, moment, fields):
    return (agent_id, moment.timestamp(), moment.isoformat(), json.dumps(fields))


def test_upgrade_from_runtime_sqlite_keeps_versions_and_live_agents_then_removes_it(tmp_path, caplog):
    repository, fresh, stale = two_agents(tmp_path)
    now, older = datetime.now(), datetime.now() - timedelta(minutes=5)
    legacy_database(tmp_path / "runtime.sqlite3", [
        legacy_row(fresh["id"], now, {"version": "2.0.0", "service_status": "running", "system_metrics": METRICS}),
        legacy_row(stale["id"], older, {"version": "1.5.0"}),
        legacy_row("deleted", now, {"version": "9.9.9"}),
    ])
    (tmp_path / "runtime.sqlite3-journal").write_bytes(b"")
    (tmp_path / "runtime.sqlite3.corrupt-1").write_bytes(b"x")
    # An older heartbeat than the registration is superseded by the document.
    repository.update_system_transaction(lambda system: next(
        agent for agent in system["agents"] if agent["id"] == stale["id"]).update(last_heartbeat=datetime.now().isoformat()))
    current_revision = revision(repository)

    runtime_state.reset()
    with caplog.at_level(logging.INFO, logger="backend.agents.runtime_state"):
        manager = AgentManager(ProfileRepository(tmp_path))
        AgentManager(ProfileRepository(tmp_path))  # a second manager in the same start does nothing

    assert runtime_logs(caplog) == [logging.INFO]
    assert revision(repository) == current_revision + 1
    assert stored(repository, fresh["id"])["version"] == "2.0.0"
    assert stored(repository, stale["id"])["version"] == "1.0.0"
    agents = {agent["id"]: agent for agent in manager.get_all_agents()}
    assert agents[fresh["id"]]["status"] == "online" and agents[fresh["id"]]["system_metrics"] == METRICS
    assert agents[stale["id"]]["version"] == "1.0.0" and agents[stale["id"]]["status"] == "online"
    assert set(manager.runtime._entries) == {fresh["id"]}
    assert sorted(path.name for path in tmp_path.iterdir() if path.name.startswith("runtime.sqlite3")) == []


def test_upgrade_with_unchanged_versions_does_not_write_the_configuration(tmp_path):
    repository, fresh, _ = two_agents(tmp_path)
    legacy_database(tmp_path / "runtime.sqlite3", [legacy_row(fresh["id"], datetime.now(), {"version": "1.0.0"})])
    current_revision = revision(repository)
    runtime_state.reset()
    manager = AgentManager(ProfileRepository(tmp_path))
    assert revision(repository) == current_revision
    assert manager.get_agent_by_id(fresh["id"])["status"] == "online"
    assert not (tmp_path / "runtime.sqlite3").exists()


def test_a_damaged_runtime_sqlite_is_ignored_and_removed(tmp_path, caplog):
    repository, fresh, _ = two_agents(tmp_path)
    (tmp_path / "runtime.sqlite3").write_bytes(b"not a database" * 100)
    runtime_state.reset()
    with caplog.at_level(logging.INFO, logger="backend.agents.runtime_state"):
        manager = AgentManager(ProfileRepository(tmp_path))
    assert manager.get_agent_by_id(fresh["id"])["version"] == "1.0.0"
    assert manager.runtime._entries == {}
    assert not (tmp_path / "runtime.sqlite3").exists()
    assert runtime_logs(caplog) == [logging.WARNING, logging.INFO]


def test_undecodable_legacy_rows_are_skipped(tmp_path):
    repository, fresh, stale = two_agents(tmp_path)
    legacy_database(tmp_path / "runtime.sqlite3", [
        (fresh["id"], datetime.now().timestamp(), datetime.now().isoformat(), "{bad"),
        legacy_row(stale["id"], datetime.now(), {"version": "2.0.0"}),
    ])
    runtime_state.reset()
    manager = AgentManager(ProfileRepository(tmp_path))
    assert set(manager.runtime._entries) == {stale["id"]}
    assert stored(repository, stale["id"])["version"] == "2.0.0"


def test_a_failed_version_import_is_not_retried_over_newer_versions(tmp_path, monkeypatch, caplog):
    repository, fresh, _ = two_agents(tmp_path)
    legacy_database(tmp_path / "runtime.sqlite3", [legacy_row(fresh["id"], datetime.now(), {"version": "2.0.0"})])
    runtime_state.reset()

    def unavailable(self, _changes):
        raise OSError("disk full")
    with monkeypatch.context() as patch, caplog.at_level(logging.INFO, logger="backend.agents.runtime_state"):
        patch.setattr(AgentManager, "_save_reported_fields", unavailable)
        manager = AgentManager(ProfileRepository(tmp_path))
    assert runtime_logs(caplog) == [logging.WARNING, logging.INFO]
    # Still shown live meanwhile, and the file is gone either way.
    assert manager.get_agent_by_id(fresh["id"])["version"] == "2.0.0"
    assert not (tmp_path / "runtime.sqlite3").exists()

    # The next heartbeat writes the version it reports.
    assert manager.update_heartbeat(fresh["id"], {"version": "3.0.0"})
    restarted = restart(repository)
    assert stored(repository, fresh["id"])["version"] == "3.0.0"
    assert restarted.get_agent_by_id(fresh["id"])["version"] == "3.0.0"


def test_a_runtime_sqlite_that_cannot_be_removed_is_not_imported(tmp_path, monkeypatch, caplog):
    repository, fresh, _ = two_agents(tmp_path)
    database = tmp_path / "runtime.sqlite3"
    legacy_database(database, [legacy_row(fresh["id"], datetime.now(), {"version": "2.0.0"})])
    current_revision = revision(repository)
    unlink = type(database).unlink

    def refuse(self, *args, **kwargs):
        if self.name == "runtime.sqlite3":
            raise PermissionError("read-only")
        return unlink(self, *args, **kwargs)
    monkeypatch.setattr(type(database), "unlink", refuse)
    runtime_state.reset()
    with caplog.at_level(logging.INFO, logger="backend.agents.runtime_state"):
        manager = AgentManager(ProfileRepository(tmp_path))
    # Applying it now would make every later start apply the same old rows again.
    assert runtime_logs(caplog) == [logging.WARNING, logging.WARNING]
    assert revision(repository) == current_revision
    assert manager.runtime._entries == {}
    assert database.exists()


# --- presence logging across a restart ---

def presence_messages(caplog):
    return [record.getMessage() for record in caplog.records if record.name == "backend.agents.presence_log"]


def test_a_restart_does_not_log_every_agent_back_online(tmp_path, monkeypatch, caplog):
    monkeypatch.setattr(presence_log, "_known", {})
    repository, manager, agent = setup(tmp_path)
    age_registration(repository)
    restarted = restart(repository)
    with caplog.at_level(logging.INFO, logger="backend.agents.presence_log"):
        presence_log.observe(restarted.get_all_agents())   # offline: nothing heard since start
        assert restarted.update_heartbeat(agent["id"], {})
        presence_log.observe(restarted.get_all_agents())   # first heartbeat: a baseline, not a recovery
        timestamp, _, fields = restarted.runtime._entries[agent["id"]]
        restarted.runtime._entries[agent["id"]] = (timestamp, (datetime.now() - timedelta(minutes=5)).isoformat(), fields)
        presence_log.observe(restarted.get_all_agents())
        assert restarted.update_heartbeat(agent["id"], {})
        presence_log.observe(restarted.get_all_agents())
    messages = presence_messages(caplog)
    assert len(messages) == 2
    assert messages[0].startswith("Agent edge 已离线") and messages[1] == "Agent edge 已恢复在线"
