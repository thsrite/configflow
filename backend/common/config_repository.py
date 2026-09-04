"""Durable, profile-scoped configuration storage."""

from __future__ import annotations

import copy
import errno
import json
import math
import os
import re
import secrets
import shutil
import tempfile
import threading
import time
import uuid
from contextlib import contextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Dict, Iterator, List, Optional


class ProfileRepositoryError(Exception):
    """Base error for profile storage operations."""


class ProfileValidationError(ProfileRepositoryError, ValueError):
    """Raised when a profile identifier is unsafe or malformed."""


class ProfileNotFound(ProfileRepositoryError, KeyError):
    """Raised when a profile does not exist."""


class ProfileExists(ProfileRepositoryError, ValueError):
    """Raised when a profile identifier is already in use."""


class ProfileInUse(ProfileRepositoryError, ValueError):
    """Raised when an agent still references a profile."""


_PROFILE_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$")
_THREAD_LOCKS: Dict[str, threading.RLock] = {}
_THREAD_LOCKS_GUARD = threading.Lock()


def _now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def _deep_merge(base: Dict[str, Any], override: Dict[str, Any]) -> Dict[str, Any]:
    result: Dict[str, Any] = copy.deepcopy(base)
    for key, value in override.items():
        if isinstance(result.get(key), dict) and isinstance(value, dict):
            result[key] = _deep_merge(result[key], value)
        else:
            result[key] = copy.deepcopy(value)
    return result


_MISSING = object()


def _list_item_key(value: Any) -> Any:
    if isinstance(value, dict) and isinstance(value.get("id"), (str, int)):
        return ("id", value["id"])
    try:
        return ("value", json.dumps(value, sort_keys=True, ensure_ascii=False))
    except (TypeError, ValueError):
        return ("repr", repr(value))


def _three_way_merge(current: Any, baseline: Any, proposed: Any) -> Any:
    """Apply the caller's baseline-to-proposed delta to freshly read data."""
    if proposed == baseline:
        return copy.deepcopy(current)
    if isinstance(current, dict) and isinstance(baseline, dict) and isinstance(proposed, dict):
        result = copy.deepcopy(current)
        for key in baseline.keys() - proposed.keys():
            if result.get(key, _MISSING) == baseline[key]:
                result.pop(key, None)
        for key, value in proposed.items():
            old_value = baseline.get(key, _MISSING)
            if old_value is _MISSING:
                if key not in result:
                    result[key] = copy.deepcopy(value)
                elif isinstance(result[key], dict) and isinstance(value, dict):
                    result[key] = _deep_merge(result[key], value)
            elif value != old_value:
                result[key] = _three_way_merge(result.get(key), old_value, value)
        return result
    if isinstance(current, list) and isinstance(baseline, list) and isinstance(proposed, list):
        baseline_by_key = {_list_item_key(item): item for item in baseline}
        proposed_by_key = {_list_item_key(item): item for item in proposed}
        proposed_keys = [_list_item_key(item) for item in proposed]
        removed = baseline_by_key.keys() - proposed_by_key.keys()
        result = [copy.deepcopy(item) for item in current if _list_item_key(item) not in removed]
        result_positions = {_list_item_key(item): index for index, item in enumerate(result)}
        for key in proposed_keys:
            proposed_item = proposed_by_key[key]
            if key not in baseline_by_key:
                if key not in result_positions:
                    result.append(copy.deepcopy(proposed_item))
                    result_positions[key] = len(result) - 1
            elif proposed_item != baseline_by_key[key] and key in result_positions:
                index = result_positions[key]
                result[index] = _three_way_merge(result[index], baseline_by_key[key], proposed_item)

        baseline_order = [_list_item_key(item) for item in baseline]
        retained_proposed_order = [key for key in proposed_keys if key in baseline_by_key]
        retained_baseline_order = [key for key in baseline_order if key in proposed_by_key]
        if retained_proposed_order != retained_baseline_order:
            result_by_key = {_list_item_key(item): item for item in result}
            ordered_keys = [key for key in proposed_keys if key in result_by_key]
            ordered_keys.extend(key for key in result_by_key if key not in ordered_keys)
            result = [result_by_key[key] for key in ordered_keys]
        return result
    return copy.deepcopy(proposed)


def _default_profile_config() -> Dict[str, Any]:
    return {
        "subscriptions": [],
        "nodes": [],
        "subscription_aggregations": [],
        "rule_configs": [],
        "rule_library": [],
        "proxy_groups": [],
        "mihomo": {"custom_config": ""},
        "mosdns": {
            "direct_rulesets": [],
            "proxy_rulesets": [],
            "direct_rules": [],
            "proxy_rules": [],
            "local_dns": "",
            "remote_dns": "",
            "fallback_dns": "",
            "default_forward": "forward_remote",
            "custom_hosts": "",
            "custom_config": "",
            "custom_matches": [],
            "custom_match_position": "tail",
            "cache_enabled": True,
            "cache_size": 10240,
            "cache_lazy_ttl": 21600,
            "cache_dump_enabled": True,
            "cache_dump_file": "./cache.dump",
            "cache_dump_interval": 300,
        },
        "surge": {"custom_config": "", "smart_groups": []},
    }


class ProfileRepository:
    """Stores system metadata and isolated profile data under ``DATA_DIR``."""

    DEFAULT_PROFILE_ID = "default"
    SCHEMA_VERSION = 2
    SYSTEM_FILE_NAME = "system.json"
    LEGACY_FILE_NAME = "config.json"
    PROFILE_FIELDS = frozenset(
        {
            "subscriptions",
            "nodes",
            "subscription_aggregations",
            "rule_configs",
            "rule_library",
            "proxy_groups",
            "mihomo",
            "mosdns",
            "surge",
        }
    )
    SYSTEM_FIELDS = frozenset({"schema_version", "active_profile_id", "profiles", "agents", "system_config", "backup", "profile_id"})
    DERIVED_DIRS = ("subscribes", "providers", "rules", "generated")
    LOCK_TIMEOUT_ENV = "CONFIGFLOW_LOCK_TIMEOUT_SECONDS"
    DEFAULT_LOCK_TIMEOUT_SECONDS = 300.0
    MIN_LOCK_TIMEOUT_SECONDS = 0.1
    MAX_LOCK_TIMEOUT_SECONDS = 3600.0
    LOCK_POLL_INTERVAL_SECONDS = 0.05

    def __init__(
        self,
        data_dir: os.PathLike[str] | str,
        default_config_factory: Optional[Callable[[], Dict[str, Any]]] = None,
        initial_config_factory: Optional[Callable[[], Dict[str, Any]]] = None,
    ) -> None:
        self.data_dir = Path(data_dir).expanduser().resolve()
        self.profiles_dir = self.data_dir / "profiles"
        self.system_file = self.data_dir / self.SYSTEM_FILE_NAME
        self.legacy_file = self.data_dir / self.LEGACY_FILE_NAME
        self.migrations_dir = self.data_dir / "migrations"
        self.initialization_lock_file = self.data_dir / ".profile-repository.initialize.lock"
        self._default_config_factory = default_config_factory
        self._initial_config_factory = initial_config_factory
        self.data_dir.mkdir(parents=True, exist_ok=True)
        self.profiles_dir.mkdir(parents=True, exist_ok=True)
        self._initialize()

    def _profile_defaults(self) -> Dict[str, Any]:
        defaults = _default_profile_config()
        if self._default_config_factory:
            factory_data = self._default_config_factory() or {}
            defaults = _deep_merge(defaults, {k: v for k, v in factory_data.items() if k not in self.SYSTEM_FIELDS})
        return defaults

    def _new_system(self) -> Dict[str, Any]:
        timestamp = _now()
        return {
            "schema_version": self.SCHEMA_VERSION,
            "active_profile_id": self.DEFAULT_PROFILE_ID,
            "profiles": [
                {
                    "id": self.DEFAULT_PROFILE_ID,
                    "name": "Default",
                    "description": "Default configuration profile",
                    "created_at": timestamp,
                    "updated_at": timestamp,
                }
            ],
            "agents": [],
            "system_config": {
                "server_domain": "",
                "github_proxy_domain": "",
                "retired_rule_proxy_tokens": [],
            },
            "backup": {},
        }

    def _ensure_rule_proxy_token(self, system: Dict[str, Any]) -> bool:
        system_config = system.setdefault("system_config", {})
        retired = system_config.get("retired_rule_proxy_tokens")
        normalized_retired = []
        if isinstance(retired, list):
            for value in retired:
                if isinstance(value, str) and value and value not in normalized_retired:
                    normalized_retired.append(value)
        if retired != normalized_retired:
            system_config["retired_rule_proxy_tokens"] = normalized_retired
            changed = True
        else:
            changed = False
        token = system_config.get("rule_proxy_token")
        config_token = system_config.get("config_token")
        if isinstance(token, str) and token:
            if token != config_token:
                return changed
            if token not in normalized_retired:
                normalized_retired.append(token)
                system_config["retired_rule_proxy_tokens"] = normalized_retired
                changed = True
        while True:
            new_token = secrets.token_urlsafe(32)
            if new_token and new_token != config_token:
                break
        system_config["rule_proxy_token"] = new_token
        return True

    def rule_proxy_tokens_for_sanitization(self) -> set[str]:
        """Return persisted current and retired tokens for output sanitization only."""
        system_config = self.get_system().get("system_config", {})
        if not isinstance(system_config, dict):
            return set()
        values = [system_config.get("rule_proxy_token")]
        retired = system_config.get("retired_rule_proxy_tokens", [])
        if isinstance(retired, list):
            values.extend(retired)
        return {value for value in values if isinstance(value, str) and value}

    def _initialize(self) -> None:
        # Initialization is one transaction across system detection, legacy
        # snapshotting, profile staging/rename, and the system.json commit.
        # A waiter deliberately re-runs every state check after acquiring the
        # lock instead of acting on observations made before another process.
        with self._lock(self.initialization_lock_file):
            self._initialize_locked()

    def _recover_system_from_backup(self, error: Exception) -> Optional[Dict[str, Any]]:
        """Fall back to the sidecar backup when system.json cannot be parsed.

        A full disk can leave system.json empty or truncated. Without this
        fallback the process raises on every start and the supervisor gives up,
        even though a good backup is sitting next to the broken file.
        """
        backup_path = self.system_file.with_name(f"{self.system_file.name}.bak")
        if not backup_path.exists():
            return None
        try:
            system = self._read_json(backup_path)
        except ProfileRepositoryError:
            return None

        # Keep the damaged file for diagnosis instead of silently overwriting it.
        stamp = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
        corrupt_path = self.system_file.with_name(f"{self.system_file.name}.corrupt-{stamp}")
        try:
            os.replace(self.system_file, corrupt_path)
        except OSError:
            pass

        self._write_system(system)
        return system

    def _initialize_locked(self) -> None:
        if self.system_file.exists():
            try:
                system = self._read_json(self.system_file)
            except ProfileRepositoryError as exc:
                recovered = self._recover_system_from_backup(exc)
                if recovered is None:
                    raise
                system = recovered
            self._normalize_system(system)
            self._ensure_profile_files(system)
            return

        if self.legacy_file.exists():
            self._migrate_legacy()
            return

        system = self._new_system()
        self._ensure_profile_layout(self.DEFAULT_PROFILE_ID)
        initial_config = (
            self._initial_config_factory()
            if self._initial_config_factory
            else self._profile_defaults()
        )
        if not isinstance(initial_config, dict):
            initial_config = self._profile_defaults()
        for key in ("agents", "system_config", "backup"):
            if key not in initial_config:
                continue
            value = copy.deepcopy(initial_config[key])
            if isinstance(system.get(key), dict) and isinstance(value, dict):
                system[key] = _deep_merge(system[key], value)
            else:
                system[key] = value
        for agent in system.get("agents", []):
            if isinstance(agent, dict):
                agent.setdefault("profile_id", self.DEFAULT_PROFILE_ID)
        self._ensure_rule_proxy_token(system)
        initial_config = {
            key: copy.deepcopy(value)
            for key, value in initial_config.items()
            if key not in self.SYSTEM_FIELDS
        }
        self._write_profile_file(self.DEFAULT_PROFILE_ID, initial_config)
        self._write_system(system)

    def _read_json(self, path: Path) -> Dict[str, Any]:
        try:
            with path.open("r", encoding="utf-8") as handle:
                value = json.load(handle)
        except json.JSONDecodeError as exc:
            raise ProfileRepositoryError(f"Invalid JSON in {path}: {exc}") from exc
        if not isinstance(value, dict):
            raise ProfileRepositoryError(f"JSON object expected in {path}")
        return value

    def _normalize_system(self, system: Dict[str, Any], persist: bool = True) -> Dict[str, Any]:
        changed = False
        defaults = self._new_system()
        for key, value in defaults.items():
            if key not in system:
                system[key] = copy.deepcopy(value)
                changed = True
        if system.get("schema_version") != self.SCHEMA_VERSION:
            system["schema_version"] = self.SCHEMA_VERSION
            changed = True
        profiles = system.get("profiles")
        if not isinstance(profiles, list):
            profiles = []
            system["profiles"] = profiles
            changed = True
        seen = set()
        valid_profiles = []
        for profile in profiles:
            if not isinstance(profile, dict) or "id" not in profile:
                changed = True
                continue
            self.validate_profile_id(profile["id"])
            if profile["id"] in seen:
                changed = True
                continue
            seen.add(profile["id"])
            valid_profiles.append(profile)
        if not valid_profiles:
            valid_profiles = copy.deepcopy(defaults["profiles"])
            changed = True
        if not any(profile["id"] == self.DEFAULT_PROFILE_ID for profile in valid_profiles):
            valid_profiles.insert(0, copy.deepcopy(defaults["profiles"][0]))
            changed = True
        if valid_profiles != profiles:
            system["profiles"] = valid_profiles
            changed = True
        active = system.get("active_profile_id")
        if not isinstance(active, str) or active not in {p["id"] for p in valid_profiles}:
            system["active_profile_id"] = self.DEFAULT_PROFILE_ID
            changed = True
        if not isinstance(system.get("agents"), list):
            system["agents"] = []
            changed = True
        if not isinstance(system.get("system_config"), dict):
            system["system_config"] = {}
            changed = True
        if self._ensure_rule_proxy_token(system):
            changed = True
        if not isinstance(system.get("backup"), dict):
            system["backup"] = {}
            changed = True
        for agent in system["agents"]:
            if isinstance(agent, dict) and not agent.get("profile_id"):
                agent["profile_id"] = self.DEFAULT_PROFILE_ID
                changed = True
        if changed and persist:
            self._write_system(system)
        return system

    def _ensure_profile_layout(self, profile_id: str) -> Path:
        profile_dir = self.profile_dir(profile_id)
        profile_dir.mkdir(parents=True, exist_ok=True)
        for dirname in self.DERIVED_DIRS:
            (profile_dir / dirname).mkdir(parents=True, exist_ok=True)
        return profile_dir

    def _ensure_profile_files(self, system: Dict[str, Any]) -> None:
        for profile in system["profiles"]:
            profile_id = profile["id"]
            self._ensure_profile_layout(profile_id)
            path = self._profile_path(profile_id)
            if not path.exists():
                self._write_profile_file(profile_id, self._profile_defaults())

    def _migrate_legacy(self) -> None:
        legacy_data = self._read_json(self.legacy_file)
        migration_dir = self._create_migration_snapshot_dir()
        shutil.copy2(self.legacy_file, migration_dir / self.LEGACY_FILE_NAME)

        profile_data = {
            key: copy.deepcopy(value)
            for key, value in legacy_data.items()
            if key not in self.SYSTEM_FIELDS
        }
        profile_data = _deep_merge(self._profile_defaults(), profile_data)
        system = self._new_system()
        for key in ("agents", "system_config", "backup"):
            if key in legacy_data:
                system[key] = copy.deepcopy(legacy_data[key])
        for agent in system.get("agents", []):
            if isinstance(agent, dict):
                agent.setdefault("profile_id", self.DEFAULT_PROFILE_ID)
        self._ensure_rule_proxy_token(system)

        profile_dir = self.profile_dir(self.DEFAULT_PROFILE_ID)
        staging_dir = self.profiles_dir / f".{self.DEFAULT_PROFILE_ID}.migration-staging-{uuid.uuid4().hex}"
        backup_dir: Optional[Path] = None
        previous_system = self.system_file.read_bytes() if self.system_file.exists() else None
        installed_staging = False
        try:
            staging_dir.mkdir(parents=False, exist_ok=False)
            for dirname in self.DERIVED_DIRS:
                (staging_dir / dirname).mkdir()
            self._write_json(staging_dir / "config.json", profile_data)
            self._copy_legacy_derived_data(staging_dir)

            if profile_dir.exists():
                backup_dir = self.profiles_dir / f".{self.DEFAULT_PROFILE_ID}.migration-backup-{uuid.uuid4().hex}"
                os.replace(profile_dir, backup_dir)
            os.replace(staging_dir, profile_dir)
            installed_staging = True
            # system.json is the migration commit marker. Legacy files remain recoverable.
            self._write_system(system)
        except Exception:
            if installed_staging and profile_dir.exists():
                shutil.rmtree(profile_dir)
            if backup_dir is not None and backup_dir.exists():
                os.replace(backup_dir, profile_dir)
            if previous_system is None:
                if self.system_file.exists():
                    self.system_file.unlink()
            else:
                self.system_file.write_bytes(previous_system)
            if staging_dir.exists():
                shutil.rmtree(staging_dir)
            raise
        else:
            if backup_dir is not None and backup_dir.exists():
                shutil.rmtree(backup_dir)

    def _create_migration_snapshot_dir(self) -> Path:
        """Create a unique snapshot directory, retrying an actual mkdir race."""
        self.migrations_dir.mkdir(parents=True, exist_ok=True)
        timestamp = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
        for _ in range(16):
            candidate = self.migrations_dir / f"{timestamp}-{uuid.uuid4().hex}"
            try:
                candidate.mkdir(exist_ok=False)
            except FileExistsError:
                continue
            return candidate
        raise ProfileRepositoryError("Unable to allocate a unique migration snapshot directory")

    def _copy_legacy_derived_data(self, profile_dir: Optional[Path] = None) -> None:
        profile_dir = profile_dir or self.profile_dir(self.DEFAULT_PROFILE_ID)
        for dirname in self.DERIVED_DIRS[:-1]:
            source = self.data_dir / dirname
            if source.is_dir():
                shutil.copytree(source, profile_dir / dirname, dirs_exist_ok=True)
        generated = profile_dir / "generated"
        for filename in ("config.yaml", "config.conf"):
            source = self.data_dir / filename
            if source.is_file():
                shutil.copy2(source, generated / filename)

    @staticmethod
    def validate_profile_id(profile_id: str) -> str:
        if not isinstance(profile_id, str) or not _PROFILE_ID.fullmatch(profile_id):
            raise ProfileValidationError("Invalid profile id")
        return profile_id

    def profile_dir(self, profile_id: str) -> Path:
        self.validate_profile_id(profile_id)
        root = self.profiles_dir.resolve()
        candidate = (root / profile_id).resolve()
        try:
            candidate.relative_to(root)
        except ValueError as exc:  # defensive check if validation changes later
            raise ProfileValidationError("Profile path escapes profile directory") from exc
        return candidate

    def profile_path(self, profile_id: str, relative_path: str) -> Path:
        if not isinstance(relative_path, str) or not relative_path or "\x00" in relative_path:
            raise ProfileValidationError("Invalid profile relative path")
        profile_root = self.profile_dir(profile_id)
        candidate = (profile_root / relative_path).resolve()
        try:
            candidate.relative_to(profile_root)
        except ValueError as exc:
            raise ProfileValidationError("Profile path escapes profile directory") from exc
        return candidate

    def cache_dir(self, profile_id: str) -> Path:
        return self.profile_dir(profile_id) / "subscribes"

    def providers_dir(self, profile_id: str) -> Path:
        return self.profile_dir(profile_id) / "providers"

    def rules_dir(self, profile_id: str) -> Path:
        return self.profile_dir(profile_id) / "rules"

    def generated_dir(self, profile_id: str) -> Path:
        return self.profile_dir(profile_id) / "generated"

    def _profile_path(self, profile_id: str) -> Path:
        return self.profile_dir(profile_id) / "config.json"

    def _profile_operation_lock_path(self, profile_id: str) -> Path:
        self.validate_profile_id(profile_id)
        return self.profiles_dir / f".{profile_id}.operation.lock"

    def _thread_lock(self, path: Path) -> threading.RLock:
        key = str(path.resolve())
        with _THREAD_LOCKS_GUARD:
            return _THREAD_LOCKS.setdefault(key, threading.RLock())

    @classmethod
    def _lock_timeout_seconds(cls) -> float:
        raw_timeout = os.environ.get(cls.LOCK_TIMEOUT_ENV)
        if raw_timeout is None:
            return cls.DEFAULT_LOCK_TIMEOUT_SECONDS
        try:
            timeout = float(raw_timeout)
        except (TypeError, ValueError):
            return cls.DEFAULT_LOCK_TIMEOUT_SECONDS
        if not math.isfinite(timeout):
            return cls.DEFAULT_LOCK_TIMEOUT_SECONDS
        return min(cls.MAX_LOCK_TIMEOUT_SECONDS, max(cls.MIN_LOCK_TIMEOUT_SECONDS, timeout))

    @staticmethod
    def _lock_is_contended(exc: OSError) -> bool:
        return exc.errno in {errno.EACCES, errno.EAGAIN, errno.EDEADLK}

    def _acquire_file_lock(self, handle: Any) -> None:
        deadline = time.monotonic() + self._lock_timeout_seconds()
        while True:
            handle.seek(0)
            try:
                if os.name == "nt":
                    import msvcrt

                    msvcrt.locking(handle.fileno(), msvcrt.LK_NBLCK, 1)
                else:
                    import fcntl

                    fcntl.flock(handle.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                return
            except OSError as exc:
                if not self._lock_is_contended(exc):
                    raise
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise ProfileRepositoryError(
                        "Timed out waiting for profile repository lock"
                    ) from None
                time.sleep(min(self.LOCK_POLL_INTERVAL_SECONDS, remaining))

    @contextmanager
    def _lock(self, path: Path) -> Iterator[None]:
        path.parent.mkdir(parents=True, exist_ok=True)
        thread_lock = self._thread_lock(path)
        with thread_lock:
            with path.open("a+b") as handle:
                if handle.seek(0, os.SEEK_END) == 0:
                    handle.write(b"0")
                    handle.flush()
                self._acquire_file_lock(handle)
                try:
                    yield
                finally:
                    if os.name == "nt":
                        import msvcrt

                        handle.seek(0)
                        msvcrt.locking(handle.fileno(), msvcrt.LK_UNLCK, 1)
                    else:
                        import fcntl

                        fcntl.flock(handle.fileno(), fcntl.LOCK_UN)

    def _fsync_dir(self, directory: Path) -> None:
        if os.name == "nt":
            return
        dir_fd = os.open(directory, os.O_RDONLY)
        try:
            os.fsync(dir_fd)
        finally:
            os.close(dir_fd)

    def _write_temp(self, path: Path, content: str) -> Path:
        """Write content to a sibling temp file that is durable on return."""
        fd, temp_name = tempfile.mkstemp(prefix=f".{path.name}.", suffix=".tmp", dir=path.parent)
        temp_path = Path(temp_name)
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as handle:
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        # A full disk can leave a short file behind even when write() did not
        # raise; committing that would destroy the previous good content.
        written = temp_path.stat().st_size
        expected = len(content.encode("utf-8"))
        if written != expected:
            raise ProfileRepositoryError(
                f"Incomplete write for {path}: {written} of {expected} bytes"
            )
        return temp_path

    def _write_atomic(self, path: Path, content: str) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        backup_path = path.with_name(f"{path.name}.bak")
        temp_path: Optional[Path] = None
        previous: Optional[bytes] = None
        if path.exists():
            try:
                previous = path.read_bytes()
            except OSError:
                previous = None

        try:
            temp_path = self._write_temp(path, content)
            os.replace(temp_path, path)
            temp_path = None
            self._fsync_dir(path.parent)
        finally:
            if temp_path and temp_path.exists():
                temp_path.unlink()

        # The backup is refreshed only after the new content is committed, and
        # through its own temp file. Refreshing it first (the previous
        # behaviour) meant a full disk could truncate the backup while the
        # main write also failed, losing both copies at once.
        if previous is None:
            return
        backup_temp: Optional[Path] = None
        try:
            fd, temp_name = tempfile.mkstemp(
                prefix=f".{backup_path.name}.", suffix=".tmp", dir=backup_path.parent
            )
            backup_temp = Path(temp_name)
            with os.fdopen(fd, "wb") as handle:
                handle.write(previous)
                handle.flush()
                os.fsync(handle.fileno())
            if backup_temp.stat().st_size != len(previous):
                raise OSError("incomplete backup write")
            os.replace(backup_temp, backup_path)
            backup_temp = None
            self._fsync_dir(backup_path.parent)
        except OSError:
            # The primary write already succeeded; a stale but valid backup is
            # strictly better than a truncated one, so keep the old backup.
            pass
        finally:
            if backup_temp and backup_temp.exists():
                backup_temp.unlink()

    def _write_json(self, path: Path, data: Dict[str, Any]) -> None:
        self._write_atomic(path, json.dumps(data, ensure_ascii=False, indent=2) + "\n")

    def _write_system(self, system: Dict[str, Any]) -> None:
        with self._lock(self.system_file.with_name(f"{self.system_file.name}.lock")):
            self._write_json(self.system_file, system)

    @contextmanager
    def _system_transaction(self) -> Iterator[Dict[str, Any]]:
        lock_path = self.system_file.with_name(f"{self.system_file.name}.lock")
        with self._lock(lock_path):
            previous = self.system_file.read_bytes()
            try:
                system = self._read_json(self.system_file)
                self._normalize_system(system, persist=False)
                yield system
                self._normalize_system(system, persist=False)
                self._write_json(self.system_file, system)
            except Exception:
                self.system_file.write_bytes(previous)
                raise

    def _write_profile_file(self, profile_id: str, data: Dict[str, Any]) -> None:
        self._ensure_profile_layout(profile_id)
        path = self._profile_path(profile_id)
        with self._lock(path.with_name(f"{path.name}.lock")):
            self._write_json(path, data)

    def _profile_metadata(self, profile_id: str, system: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        self.validate_profile_id(profile_id)
        system = system or self.get_system()
        profile = next((p for p in system["profiles"] if p["id"] == profile_id), None)
        if profile is None:
            raise ProfileNotFound(profile_id)
        return copy.deepcopy(profile)

    def get_system(self) -> Dict[str, Any]:
        if not self.system_file.exists():
            self._initialize()
        lock_path = self.system_file.with_name(f"{self.system_file.name}.lock")
        with self._lock(lock_path):
            system = self._read_json(self.system_file)
            original = copy.deepcopy(system)
            self._normalize_system(system, persist=False)
            if system != original:
                self._write_json(self.system_file, system)
            return system

    def update_system_transaction(
        self,
        updater: Callable[[Dict[str, Any]], Optional[Dict[str, Any]]],
    ) -> Dict[str, Any]:
        """Read, mutate, and atomically write system data under its lock."""
        if not callable(updater):
            raise ProfileRepositoryError("System updater must be callable")
        with self._system_transaction() as system:
            replacement = updater(system)
            if replacement is not None:
                if not isinstance(replacement, dict):
                    raise ProfileRepositoryError("System updater must return an object or None")
                system.clear()
                system.update(copy.deepcopy(replacement))
                self._normalize_system(system, persist=False)
        return copy.deepcopy(system)

    def list_profiles(self) -> List[Dict[str, Any]]:
        return [copy.deepcopy(profile) for profile in self.get_system()["profiles"]]

    def active_profile_id(self) -> str:
        return self.get_system()["active_profile_id"]

    def get_profile(self, profile_id: str) -> Dict[str, Any]:
        with self._lock(self._profile_operation_lock_path(profile_id)):
            self._profile_metadata(profile_id)
            path = self._profile_path(profile_id)
            if not path.exists():
                self._write_profile_file(profile_id, self._profile_defaults())
            data = self._read_json(path)
        return _deep_merge(self._profile_defaults(), data)

    def get_compat_config(self, profile_id: str) -> Dict[str, Any]:
        result = self.get_profile(profile_id)
        system = self.get_system()
        result["profile_id"] = profile_id
        result["system_config"] = copy.deepcopy(system.get("system_config", {}))
        result["agents"] = copy.deepcopy(system.get("agents", []))
        result["backup"] = copy.deepcopy(system.get("backup", {}))
        return result

    def update_profile_transaction(
        self,
        profile_id: str,
        updater: Callable[[Dict[str, Any]], Optional[Dict[str, Any]]],
    ) -> Dict[str, Any]:
        """Read, mutate, and atomically write one profile under its file lock."""
        if not callable(updater):
            raise ProfileRepositoryError("Profile updater must be callable")

        path = self._profile_path(profile_id)
        with self._lock(self._profile_operation_lock_path(profile_id)):
            self._profile_metadata(profile_id)
            with self._lock(path.with_name(f"{path.name}.lock")):
                previous = path.read_bytes() if path.exists() else None
                current = self._profile_defaults()
                if path.exists():
                    current = _deep_merge(current, self._read_json(path))
                replacement = updater(current)
                updated = current if replacement is None else replacement
                if not isinstance(updated, dict):
                    raise ProfileRepositoryError("Profile updater must return an object or None")
                profile_data = {
                    key: copy.deepcopy(value)
                    for key, value in updated.items()
                    if key not in self.SYSTEM_FIELDS
                }
                profile_data = _deep_merge(self._profile_defaults(), profile_data)
                try:
                    self._write_json(path, profile_data)
                    with self._system_transaction() as system:
                        metadata = next((item for item in system["profiles"] if item["id"] == profile_id), None)
                        if metadata is None:
                            raise ProfileNotFound(profile_id)
                        metadata["updated_at"] = _now()
                except Exception:
                    if previous is None:
                        if path.exists():
                            path.unlink()
                    else:
                        path.write_bytes(previous)
                    raise
        return copy.deepcopy(profile_data)

    def update_profile_fields(
        self,
        profile_id: str,
        fields: Dict[str, Any],
        baseline: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        """Merge fields into fresh locked data, optionally using a stale baseline."""
        if not isinstance(fields, dict):
            raise ProfileRepositoryError("Profile fields must be an object")
        if baseline is not None and not isinstance(baseline, dict):
            raise ProfileRepositoryError("Profile baseline must be an object")

        def merge_fields(profile: Dict[str, Any]) -> None:
            for key, value in fields.items():
                if baseline is not None and key in baseline:
                    profile[key] = _three_way_merge(profile.get(key), baseline[key], value)
                else:
                    profile[key] = copy.deepcopy(value)

        return self.update_profile_transaction(profile_id, merge_fields)

    def save_profile(self, profile_id: str, data: Dict[str, Any]) -> Dict[str, Any]:
        if not isinstance(data, dict):
            raise ProfileRepositoryError("Profile data must be an object")
        profile_data = {
            key: copy.deepcopy(value)
            for key, value in data.items()
            if key not in self.SYSTEM_FIELDS
        }
        profile_data = _deep_merge(self._profile_defaults(), profile_data)
        path = self._profile_path(profile_id)
        with self._lock(self._profile_operation_lock_path(profile_id)):
            self._profile_metadata(profile_id)
            previous = path.read_bytes() if path.exists() else None
            try:
                self._write_profile_file(profile_id, profile_data)

                # Never replace system-owned agents from a profile compatibility snapshot.
                system_updates = {key: data[key] for key in ("system_config", "backup") if key in data}
                with self._system_transaction() as system:
                    metadata = next(profile for profile in system["profiles"] if profile["id"] == profile_id)
                    metadata["updated_at"] = _now()
                    if system_updates:
                        for key, value in system_updates.items():
                            if isinstance(system.get(key), dict) and isinstance(value, dict):
                                system[key] = _deep_merge(system[key], value)
                            else:
                                system[key] = copy.deepcopy(value)
            except Exception:
                if previous is None:
                    if path.exists():
                        path.unlink()
                else:
                    path.write_bytes(previous)
                raise
        return copy.deepcopy(profile_data)

    def create_profile(self, metadata: Dict[str, Any], clone_from: Optional[str] = None) -> Dict[str, Any]:
        if not isinstance(metadata, dict):
            raise ProfileRepositoryError("Profile metadata must be an object")
        profile_id = metadata.get("id") or f"profile_{uuid.uuid4().hex[:12]}"
        self.validate_profile_id(profile_id)
        source = self.get_profile(clone_from) if clone_from else self._profile_defaults()
        timestamp = _now()
        profile = {
            "id": profile_id,
            "name": str(metadata.get("name") or profile_id),
            "description": str(metadata.get("description") or ""),
            "created_at": timestamp,
            "updated_at": timestamp,
        }
        with self._lock(self._profile_operation_lock_path(profile_id)):
            profile_dir = self.profile_dir(profile_id)
            staging_dir = self.profiles_dir / f".{profile_id}.staging-{uuid.uuid4().hex}"
            backup_dir: Optional[Path] = None
            try:
                with self._system_transaction() as system:
                    if any(item["id"] == profile_id for item in system["profiles"]):
                        raise ProfileExists(profile_id)
                    if profile_dir.exists():
                        backup_dir = self.profiles_dir / f".{profile_id}.orphan-backup-{uuid.uuid4().hex}"
                        os.replace(profile_dir, backup_dir)

                    staging_dir.mkdir(parents=False, exist_ok=False)
                    for dirname in self.DERIVED_DIRS:
                        (staging_dir / dirname).mkdir()
                    self._write_json(staging_dir / "config.json", source)
                    os.replace(staging_dir, profile_dir)
                    system["profiles"].append(profile)
            except Exception:
                if profile_dir.exists():
                    os.replace(profile_dir, staging_dir)
                if backup_dir is not None and backup_dir.exists():
                    os.replace(backup_dir, profile_dir)
                if staging_dir.exists():
                    try:
                        shutil.rmtree(staging_dir)
                    except OSError:
                        pass
                raise

            # An unregistered pre-existing directory is recoverable until the
            # system index commit succeeds, then becomes best-effort cleanup.
            if backup_dir is not None and backup_dir.exists():
                try:
                    shutil.rmtree(backup_dir)
                except OSError:
                    pass
        return copy.deepcopy(profile)

    def clone_profile(self, source_id: str, metadata: Dict[str, Any]) -> Dict[str, Any]:
        return self.create_profile(metadata, clone_from=source_id)

    def update_profile(self, profile_id: str, updates: Dict[str, Any]) -> Dict[str, Any]:
        self.validate_profile_id(profile_id)
        if not isinstance(updates, dict):
            raise ProfileRepositoryError("Profile metadata must be an object")
        with self._system_transaction() as system:
            profile = next((p for p in system["profiles"] if p["id"] == profile_id), None)
            if profile is None:
                raise ProfileNotFound(profile_id)
            for key in ("name", "description"):
                if key in updates:
                    profile[key] = str(updates[key])
            profile["updated_at"] = _now()
            result = copy.deepcopy(profile)
        return result

    def activate_profile(self, profile_id: str) -> Dict[str, Any]:
        with self._system_transaction() as system:
            profile = self._profile_metadata(profile_id, system)
            system["active_profile_id"] = profile_id
        return profile

    def delete_profile(self, profile_id: str) -> None:
        self.validate_profile_id(profile_id)
        if profile_id == self.DEFAULT_PROFILE_ID:
            raise ProfileRepositoryError("The default profile cannot be deleted")
        with self._lock(self._profile_operation_lock_path(profile_id)):
            profile_dir = self.profile_dir(profile_id)
            tombstone: Optional[Path] = None
            try:
                with self._system_transaction() as system:
                    self._profile_metadata(profile_id, system)
                    if any(isinstance(agent, dict) and agent.get("profile_id", self.DEFAULT_PROFILE_ID) == profile_id for agent in system.get("agents", [])):
                        raise ProfileInUse(profile_id)
                    if profile_dir.exists():
                        tombstone = self.profiles_dir / f".{profile_id}.tombstone-{uuid.uuid4().hex}"
                        os.replace(profile_dir, tombstone)
                    system["profiles"] = [profile for profile in system["profiles"] if profile["id"] != profile_id]
                    if system.get("active_profile_id") == profile_id:
                        system["active_profile_id"] = self.DEFAULT_PROFILE_ID
            except Exception:
                if tombstone is not None and tombstone.exists():
                    os.replace(tombstone, profile_dir)
                raise

            # The index commit is authoritative. Cleanup is best-effort so a
            # partially removable directory remains recoverable as a tombstone.
            if tombstone is not None and tombstone.exists():
                try:
                    shutil.rmtree(tombstone)
                except OSError:
                    pass

    def export_profile(self, profile_id: str) -> Dict[str, Any]:
        return self.get_profile(profile_id)

    def import_profile(self, profile_id: str, data: Dict[str, Any]) -> Dict[str, Any]:
        if not isinstance(data, dict):
            raise ProfileRepositoryError("Imported profile must be an object")
        if isinstance(data.get("config"), dict):
            data = data["config"]
        profile_data = {
            key: value for key, value in data.items() if key not in self.SYSTEM_FIELDS
        }
        return self.save_profile(profile_id, profile_data)

    def write_generated(self, profile_id: str, filename: str, content: str) -> Path:
        if Path(filename).name != filename or filename not in {"config.yaml", "config.conf"}:
            raise ProfileValidationError("Invalid generated filename")
        path = self.generated_dir(profile_id) / filename
        with self._lock(self._profile_operation_lock_path(profile_id)):
            self._profile_metadata(profile_id)
            with self._lock(path.with_name(f"{path.name}.lock")):
                self._write_atomic(path, content)
        return path

    def write_profile_json(self, profile_id: str, relative_path: str, data: Dict[str, Any]) -> Path:
        path = self.profile_path(profile_id, relative_path)
        with self._lock(self._profile_operation_lock_path(profile_id)):
            self._profile_metadata(profile_id)
            with self._lock(path.with_name(f"{path.name}.lock")):
                self._write_json(path, data)
        return path

    def read_profile_json(self, profile_id: str, relative_path: str) -> Dict[str, Any]:
        return self._read_json(self.profile_path(profile_id, relative_path))

    def write_profile_text(self, profile_id: str, relative_path: str, content: str) -> Path:
        path = self.profile_path(profile_id, relative_path)
        with self._lock(self._profile_operation_lock_path(profile_id)):
            self._profile_metadata(profile_id)
            with self._lock(path.with_name(f"{path.name}.lock")):
                self._write_atomic(path, content)
        return path
