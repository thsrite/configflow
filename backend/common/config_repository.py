"""Durable, profile-scoped configuration storage."""

from __future__ import annotations

import copy
import errno
import hashlib
import json
import logging
import math
import os
import re
import secrets
import shutil
import sqlite3
import tempfile
import threading
import time
import uuid
from contextlib import ExitStack, contextmanager
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Dict, Iterator, List, Optional

from backend.common.config_store import (
    ConfigStorageError, ConfigStore, StoreConflict, StoreCorrupt, StoreError, StoreUnsupported,
)
from backend.common.oxidns_settings import get_default_oxidns_settings

logger = logging.getLogger(__name__)


class ProfileRepositoryError(Exception):
    """Base error for profile storage operations."""


class ProfileValidationError(ProfileRepositoryError, ValueError):
    """Raised when a profile identifier is unsafe or malformed."""


class ProfileNotFound(ProfileRepositoryError, KeyError):
    """Raised when a profile does not exist."""


class ProfileExists(ProfileRepositoryError, ValueError):
    """Raised when a profile identifier is already in use."""


class RuleLibraryCreationUncertain(ProfileRepositoryError):
    """A new rule may have committed despite a storage/rollback failure."""


class ProfileInUse(ProfileRepositoryError, ValueError):
    """A resource/profile mutation would invalidate an existing dependency."""

    def __init__(self, message, usages=None):
        super().__init__(message)
        self.usages = usages or []


class NodeDeletionChanged(ProfileInUse):
    """The reviewed node deletion no longer matches the current references."""


_PROFILE_ID = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$")
_THREAD_LOCKS: Dict[str, threading.RLock] = {}
_THREAD_LOCKS_GUARD = threading.Lock()
# File locks held by the current thread: flock on a second descriptor of the
# same file would wait for this thread itself.
_HELD_LOCKS = threading.local()


def _now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


# Every version fills these in at startup: they never show an edit.
_STARTUP_SETTINGS = ('rule_proxy_token', 'retired_rule_proxy_tokens')


def _content_digest(value: Dict[str, Any]) -> str:
    """Digest of a parsed config.json, independent of layout and key order."""
    value = copy.deepcopy(value)
    system = value.get('system')
    for settings in (value.get('system_config'), system.get('system_config') if isinstance(system, dict) else None):
        if isinstance(settings, dict):
            for key in _STARTUP_SETTINGS:
                settings.pop(key, None)
    text = json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(',', ':'))
    return hashlib.sha256(text.encode('utf-8')).hexdigest()


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
        "oxidns": get_default_oxidns_settings(),
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
        "loon": {"custom_config": ""},
        "routers": {"items": []},
    }


SHARED_FIELDS = ('subscriptions', 'nodes', 'subscription_aggregations', 'rule_library')
RESOURCE_FIELDS = SHARED_FIELDS[:3]
PROFILE_FIELDS = ('proxy_groups', 'rule_configs', 'mihomo', 'surge', 'mosdns', 'oxidns', 'loon', 'domain_discovery', 'routers', 'device_notes', 'client_fixed_ips')
BUILTIN_POLICIES = {'DIRECT', 'REJECT'}
RULE_SOURCE_FIELDS = ('name', 'url', 'behavior', 'content', 'source_type', 'format')
# Per-rule OxiDNS DNS fields keyed by (or listing) rule_configs ids.
RULE_DNS_FIELDS = ('rule_actions', 'rule_dns_follow', 'rule_dns_parked')
ROUTER_IDENTITY_FIELDS = ('type', 'endpoint')
ROUTER_CONNECTION_FIELDS = (*ROUTER_IDENTITY_FIELDS, 'username', 'password', 'verify_tls')
# Device ownership receipts; only a publication under the router's lock may change them.
ROUTER_MANIFEST_FIELDS = ('ros_policy_state', 'dns_guard_state', 'ipv6_fakeip_state', 'ipv6_routes_state',
                          'ipv6_pd_guard_state')

def _items_by_id(items, label):
    if not isinstance(items, list):
        raise ProfileValidationError(f'{label} 必须是列表')
    result = {}
    for item in items:
        if not isinstance(item, dict) or not isinstance(item.get('id'), str) or not item['id']:
            raise ProfileValidationError(f'{label} 中每一项必须有 ID')
        if item['id'] in result:
            raise ProfileValidationError(f'{label} 存在重复 ID：{item["id"]}')
        result[item['id']] = item
    return result

def _id_list(values, label):
    if not isinstance(values, list) or any(not isinstance(value, str) for value in values):
        raise ProfileValidationError(f'{label} 只能包含资源 ID，不能设置资源覆盖')
    if len(set(values)) != len(values):
        raise ProfileValidationError(f'{label} 含有重复引用')
    return values


def _prune_rule_dns(profile):
    """Drop per-rule OxiDNS DNS entries of rules the profile no longer has."""
    oxidns = profile.get('oxidns')
    if not isinstance(oxidns, dict) or not isinstance(profile.get('rule_configs'), list):
        return
    ids = {rule.get('id') for rule in profile['rule_configs'] if isinstance(rule, dict)}
    for key in RULE_DNS_FIELDS:
        value = oxidns.get(key)
        if isinstance(value, dict):
            oxidns[key] = {identity: item for identity, item in value.items() if identity in ids}
        elif isinstance(value, list):
            oxidns[key] = [identity for identity in value if identity in ids]


def _collect_resource_refs(profile, catalogs, resource_roots=None):
    """Derive the resource closure from composition, never a second allowlist."""
    selected = {field: set() for field in RESOURCE_FIELDS}
    groups = _items_by_id(profile.get('proxy_groups', []), '策略组')

    def select(kind, value, *, allow_builtin=False):
        if not isinstance(value, str) or not value.strip():
            raise ProfileValidationError('资源引用必须是非空 ID')
        if allow_builtin and kind == 'nodes' and value in BUILTIN_POLICIES:
            return
        if value not in catalogs[kind]:
            raise ProfileInUse(f'引用的共享资源不存在：{kind}/{value}')
        selected[kind].add(value)

    for group in groups.values():
        if group.get('type') == 'chain':
            chain = group.get('chain')
            if not isinstance(chain, dict) or set(chain) != {'entry', 'exit'}:
                raise ProfileValidationError('代理链必须指定前置和落地引用')
            for field in ('entry', 'exit'):
                reference = chain[field]
                if (not isinstance(reference, dict) or set(reference) != {'type', 'id'} or
                        reference.get('type') not in ('node', 'group') or
                        not isinstance(reference.get('id'), str) or not reference['id'].strip()):
                    raise ProfileValidationError('代理链引用必须是 node/group type 与非空 id')
                if reference['type'] == 'node':
                    select('nodes', reference['id'])
                elif reference['id'] not in groups:
                    raise ProfileInUse(f'代理链引用的策略组不存在：{reference["id"]}')
            continue
        for field, kind in (('subscriptions', 'subscriptions'), ('manual_nodes', 'nodes'),
                            ('aggregations', 'subscription_aggregations')):
            for value in _id_list(group.get(field, []), field):
                select(kind, value, allow_builtin=field == 'manual_nodes')
        if group.get('source') is not None and not isinstance(group['source'], str):
            raise ProfileValidationError('策略组 source 必须是字符串')
        legacy_kind = {'node': 'nodes', 'subscription': 'subscriptions',
                       'aggregation': 'subscription_aggregations'}.get(group.get('source'))
        if legacy_kind:
            for value in _id_list(group.get('proxies', []), 'proxies'):
                select(legacy_kind, value, allow_builtin=legacy_kind == 'nodes')
        order = group.get('proxies_order', [])
        if not isinstance(order, list):
            raise ProfileValidationError('策略组排序必须是数组')
        for item in order:
            if not isinstance(item, dict):
                raise ProfileValidationError('策略组排序项必须是 type/id 对象')
            kind = {'node': 'nodes', 'subscription': 'subscriptions',
                    'aggregation': 'subscription_aggregations'}.get(item.get('type'))
            if kind:
                select(kind, item.get('id'), allow_builtin=kind == 'nodes')

    for kind, roots in (resource_roots or {}).items():
        for value in (catalogs[kind] if roots is None else roots):
            if value in catalogs[kind]:
                select(kind, value)

    for aggregation_id in selected['subscription_aggregations']:
        aggregation = catalogs['subscription_aggregations'][aggregation_id]
        for kind in ('nodes', 'subscriptions'):
            for value in _id_list(aggregation.get(kind, []), f'聚合 {aggregation_id} 的 {kind}'):
                select(kind, value)

    from backend.utils.dialer_references import raw_dialer
    nodes_by_name = {}
    for node in catalogs['nodes'].values():
        nodes_by_name.setdefault(node.get('name'), []).append(node['id'])
    groups_by_name = {}
    for group in groups.values():
        groups_by_name.setdefault(group.get('name'), []).append(group['id'])
    pending = list(selected['nodes'])
    visited = set()
    while pending:
        node_id = pending.pop()
        if node_id in visited:
            continue
        visited.add(node_id)
        node = catalogs['nodes'][node_id]
        if node.get('subscription_id'):
            select('subscriptions', node['subscription_id'])
        if not node.get('enabled', True):
            continue
        target = raw_dialer(node)
        if target is None:
            continue
        if not isinstance(target, str) or not target.strip():
            raise ProfileValidationError('原始 dialer-proxy 必须是非空名称')
        if target in BUILTIN_POLICIES:
            continue
        node_targets = nodes_by_name.get(target, [])
        if len(node_targets) + len(groups_by_name.get(target, [])) != 1:
            raise ProfileInUse(f'原始拨号代理目标不存在或名称不唯一：{target}')
        if node_targets:
            select('nodes', node_targets[0])
            pending.append(node_targets[0])
    return selected

class ProfileRepository:
    """One locked atomic document; only derived artifacts are profile-local."""
    DEFAULT_PROFILE_ID = 'default'
    SCHEMA_VERSION = 5
    SHARED_FIELDS = SHARED_FIELDS
    PROFILE_FIELDS = frozenset(PROFILE_FIELDS)
    SYSTEM_FIELDS = frozenset({'system_config', 'backup', 'agents', '_revision'})
    DERIVED_DIRS = ('providers', 'rules', 'generated')
    LOCK_TIMEOUT_ENV = 'CONFIGFLOW_LOCK_TIMEOUT_SECONDS'
    DEFAULT_LOCK_TIMEOUT_SECONDS = 300.0
    MIN_LOCK_TIMEOUT_SECONDS = 0.1
    MAX_LOCK_TIMEOUT_SECONDS = 3600.0
    LOCK_POLL_INTERVAL_SECONDS = 0.05
    DOCUMENT_CACHE_MAX_BYTES = 8 * 1024 * 1024
    DATABASE_FILENAME = 'configflow.sqlite3'
    LEGACY_FILENAME = 'config.json'

    def __init__(self, data_dir, default_config_factory=None, initial_config_factory=None):
        self.data_dir = Path(data_dir).expanduser().resolve()
        self.profiles_dir = self.data_dir / 'profiles'
        self.db_path = self.data_dir / self.DATABASE_FILENAME
        # Pre-migration copy; read only to detect edits made by older versions.
        self.legacy_path = self.data_dir / self.LEGACY_FILENAME
        self.migrations_dir = self.data_dir / 'migrations'
        self.initialization_lock_file = self.data_dir / '.config.lock'
        self._default_config_factory = default_config_factory
        self._initial_config_factory = initial_config_factory
        self._local = threading.local()
        self._store = ConfigStore(self.db_path, self._lock_timeout_seconds)
        # One validated generation only, keyed by the database head; public
        # readers return independent values.
        self._document_cache = None
        self._cache_lock = threading.Lock()
        # (head, 令牌集合)，见 _sanitization_tokens
        self._sanitization_tokens_cache = None
        # 本进程提交过的写事务数；请求内记住的配置空间校验结果据此失效
        self.commit_count = 0
        self.data_dir.mkdir(parents=True, exist_ok=True)
        self.profiles_dir.mkdir(parents=True, exist_ok=True)
        with self._lock(self.initialization_lock_file):
            self._open_locked()

    def _legacy_defaults(self):
        return _deep_merge(_default_profile_config(), self._default_config_factory() if self._default_config_factory else {})

    def _empty_profile(self, profile_id, name, description=''):
        defaults = self._legacy_defaults()
        result = {key: copy.deepcopy(defaults.get(key, {} if key in ('mihomo', 'surge', 'mosdns', 'oxidns', 'loon', 'domain_discovery', 'routers', 'device_notes', 'client_fixed_ips') else []))
                  for key in PROFILE_FIELDS}
        result.update(id=profile_id, name=name, description=description, _revision=0,
                      created_at=_now(), updated_at=_now())
        return result

    def _read_json(self, path):
        try:
            with path.open(encoding='utf-8') as handle:
                value = json.load(handle)
        except (ValueError, OSError) as error:
            raise ProfileRepositoryError(f'Cannot read configuration {path.name}: {error}') from error
        if not isinstance(value, dict):
            raise ProfileValidationError(f'JSON object expected in {path.name}')
        return value

    def _read_with_backup(self, path, validator):
        """Read a legacy file, falling back to its .bak; never repairs either."""
        try:
            current = self._read_json(path)
            validator(current)
            return current
        except ProfileRepositoryError as error:
            backup = path.with_name(path.name + '.bak')
            if not backup.is_file():
                raise
            try:
                recovered = self._read_json(backup)
                validator(recovered)
            except ProfileRepositoryError:
                raise error
            return recovered

    def _initial_document_locked(self):
        """The validated document of the pre-database layout (or the defaults).

        Returns (document, source, split_source, remaps, has_legacy). Nothing on
        disk is written: every legacy file stays as it is.
        """
        path = self.legacy_path
        if path.exists() and not path.is_file():
            raise ProfileValidationError('config.json must be a regular file')
        system_file = self.data_dir / 'system.json'
        has_layout = system_file.exists() or system_file.with_suffix('.json.bak').exists()
        if has_layout:
            for candidate in (path, path.with_name('config.json.bak'), self.profiles_dir):
                self._check_migration_path(candidate)
        current = None
        if path.exists() or path.with_name('config.json.bak').exists():
            try:
                current = self._read_json(path)
            except ProfileRepositoryError:
                # A stale single-file config is not authoritative in a split
                # installation. Prefer a committed current-format backup, if any.
                if has_layout:
                    backup = path.with_name('config.json.bak')
                    if backup.is_file():
                        try:
                            candidate = self._read_json(backup)
                        except ProfileRepositoryError:
                            candidate = {}
                        if candidate.get('schema_version') is not None:
                            current = self._read_with_backup(
                                path, lambda value: self._validate_document(self._convert_import(value)))
                else:
                    current = self._read_with_backup(
                        path, lambda value: self._validate_document(self._convert_import(value)))
            if current is not None and current.get('schema_version') == self.SCHEMA_VERSION:
                document = self._read_with_backup(path, self._validate_document)
                self._ensure_rule_proxy_token(document['system'])
                self._validate_document(document)
                return document, 'config-v5', None, {}, True
        split_source = None
        if has_layout and (current is None or current.get('schema_version') is None):
            split_source = self._read_profile_layout()
            source = split_source
            label = 'split-v2'
        else:
            if current is None or current.get('schema_version') is None:
                if any(self.profiles_dir.glob('*/config.json')) or any(self.profiles_dir.glob('*/config.json.bak')):
                    raise ProfileValidationError('发现旧配置文件但缺少 system.json 索引；请恢复完整数据目录，不能初始化空配置')
            source = current if current is not None else (self._initial_config_factory or self._legacy_defaults)()
            label = 'legacy-single' if current is not None else 'defaults'
        remaps = {}
        document = self._convert_import(source, resource_remaps=remaps)
        self._ensure_rule_proxy_token(document['system'])
        self._validate_document(document)
        return document, label, split_source, remaps, current is not None or split_source is not None

    def _migrate_to_store_locked(self):
        """Build the database from the legacy layout; legacy files stay untouched."""
        document, source, split_source, remaps, has_legacy = self._initial_document_locked()
        snapshot = None
        if has_legacy:
            snapshot = self._snapshot('migration', split_source['profiles'] if split_source else ())
        if split_source is not None:
            self._migrate_profile_cache(split_source, document, remaps)
        elif source != 'config-v5':
            self._migrate_raw_cache()
        now = _now()
        migration = {'source': source, 'at': now,
                     'snapshot': str(snapshot.relative_to(self.data_dir)) if snapshot else None}
        # data_source: whether config.json holds this data (it is a stale,
        # non-authoritative file next to a split layout or missing otherwise).
        record = self._legacy_record(document, 'migrated', now,
                                     data_source=source in ('legacy-single', 'config-v5'))
        entry = self._store.create(document, meta={'migration': migration, 'legacy_config': record})
        self._set_cache(entry)
        if has_legacy:
            logger.info('已把旧配置（%s）迁移到 %s；config.json 保留为迁移前副本，不再写入',
                        source, self.DATABASE_FILENAME)

    def _legacy_fingerprint(self):
        """(sha256, size) of config.json as it is now, or (None, None)."""
        path = self.legacy_path
        if path.is_symlink() or not path.is_file():
            return None, None
        content = path.read_bytes()
        return hashlib.sha256(content).hexdigest(), len(content)

    @staticmethod
    def _revisions_of(document):
        return {'system': document['system'].get('_revision', 0),
                'shared': document['shared'].get('_revision', 0),
                'profiles': {key: profile.get('_revision', 0) for key, profile in document['profiles'].items()}}

    def _legacy_content(self):
        """_content_digest of config.json, or None when it cannot be read."""
        try:
            return _content_digest(self._read_json(self.legacy_path))
        except ProfileRepositoryError:
            return None

    def _legacy_is_current(self):
        """Whether config.json is a current-schema document (not a pre-schema root)."""
        try:
            return self._read_json(self.legacy_path).get('schema_version') == self.SCHEMA_VERSION
        except ProfileRepositoryError:
            return False

    def _legacy_record(self, document, status, at, *, data_source):
        sha256, size = self._legacy_fingerprint()
        # revisions: baseline of current-schema files; content: of the older
        # formats, which carry no revision counters.
        return {'sha256': sha256, 'size': size, 'revisions': self._revisions_of(document),
                'content': self._legacy_content() if sha256 is not None else None,
                'status': status, 'at': at, 'data_source': data_source and sha256 is not None}

    def _open_locked(self):
        """Startup under .config.lock: migrate, check, recover and load the database."""
        self._store.discard_stale_temporaries()
        if self.db_path.is_symlink() or (self.db_path.exists() and not self.db_path.is_file()):
            raise ProfileValidationError(f'{self.DATABASE_FILENAME} 必须是普通文件')
        if not self.db_path.exists():
            journal = self.db_path.with_name(self.db_path.name + '-journal')
            if journal.exists():
                # Never apply a journal left by another file to a new database.
                os.replace(journal, journal.with_name(f'{journal.name}.orphan-{time.time_ns()}'))
            earlier = self._earlier_databases()
            if earlier:
                logger.error('%s 不存在，但数据目录里有它以前的副本（%s）；将从旧配置文件重新迁移，'
                             '之后的修改不在迁移结果中，请核对并按需从这些副本恢复',
                             self.DATABASE_FILENAME, '、'.join(earlier))
            self._migrate_to_store_locked()
        else:
            try:
                self._store.check()
            except StoreUnsupported as error:
                raise ProfileRepositoryError(
                    f'配置数据库由更新版本的 ConfigFlow 创建（{error}），当前版本无法读取；'
                    '请升级到对应版本，或用“导出全部数据”的备份恢复。数据库未改动。') from error
            except StoreCorrupt as error:
                self._rebuild_corrupt_locked(error)
            except (sqlite3.Error, StoreError) as error:
                raise ConfigStorageError(
                    f'无法打开配置数据库 {self.DATABASE_FILENAME}：{error}；数据库未改动') from error
        try:
            self._read_entry(locked=True)
        except ConfigStorageError:
            raise
        except (ProfileRepositoryError, ValueError, TypeError) as error:
            raise ProfileRepositoryError(f'配置数据库内容未通过校验：{error}') from error
        self._import_changed_legacy_locked()
        entry = self._read_entry(locked=True)
        if self._ensure_rule_proxy_token(copy.deepcopy(entry.document['system'])):
            with self._write_locked():
                pass

    def _earlier_databases(self):
        """Damaged copies and snapshots that show a database existed here before."""
        paths = sorted(self.data_dir.glob(f'{self.DATABASE_FILENAME}.corrupt-*'))
        if self.migrations_dir.is_dir():
            paths += sorted(self.migrations_dir.glob(f'*/*/{self.DATABASE_FILENAME}'))[-3:]
        return [str(path.relative_to(self.data_dir)) for path in paths
                if not path.name.endswith('-journal')]

    def _rebuild_corrupt_locked(self, error):
        """Replace a physically damaged database from a config.json known to match it."""
        record = self._store.salvage_meta()
        legacy = (record or {}).get('legacy_config') if isinstance(record, dict) else None
        sha256, size = self._legacy_fingerprint()
        if record is None:
            # Nothing tells which file the data came from: config.json is a
            # source unless it is the stale (pre-schema) root of a split
            # installation. Older versions keep system.json after migrating a
            # split layout to a schema-5 config.json, which is then authoritative.
            usable = sha256 is not None and (
                not (self.data_dir / 'system.json').exists() or self._legacy_is_current())
        else:
            usable = (sha256 is not None and isinstance(legacy, dict) and legacy.get('data_source') is True and
                      legacy.get('sha256') == sha256 and legacy.get('size') == size)
        if not usable:
            raise ProfileRepositoryError(
                f'配置数据库 {self.DATABASE_FILENAME} 已损坏（{error}），且没有可安全使用的恢复来源；'
                f'请停止服务后恢复：{self._RESTORE_HINT}损坏文件未改动。') from error
        try:
            document = self._read_with_backup(
                self.legacy_path, lambda value: self._validate_document(self._convert_import(value)))
            document = self._convert_import(document)
            self._ensure_rule_proxy_token(document['system'])
            self._validate_document(document)
        except ProfileRepositoryError as failure:
            raise ProfileRepositoryError(
                f'配置数据库 {self.DATABASE_FILENAME} 已损坏（{error}），config.json 也无法使用（{failure}）；'
                f'请停止服务后恢复：{self._RESTORE_HINT}损坏文件未改动。') from failure
        now = _now()
        corrupt = self._store.corrupt_target()
        corrupt_name = corrupt.name
        meta = {'migration': {'source': 'corrupt-rebuild', 'at': now, 'snapshot': None,
                              'corrupt_file': corrupt_name, 'error': str(error)},
                'legacy_config': legacy if isinstance(legacy, dict) else
                self._legacy_record(document, 'migrated', now, data_source=True),
                'recovered_from': {'source': 'config.json', 'at': now, 'corrupt_file': corrupt_name}}
        temporary, entry = self._store.build_temporary(document, meta)
        try:
            self._store.preserve_corrupt(corrupt)
            self._store.install(temporary)
        finally:
            if temporary.exists():
                self._store.discard_temporary(temporary)
        self._set_cache(entry)
        snapshots = sorted(self.migrations_dir.glob(f'*/*/{self.DATABASE_FILENAME}'))[-3:]
        logger.error('配置数据库已损坏（%s），原文件已保留为 %s，并已从 config.json 重建。'
                     '迁移（或上次导入旧配置）之后的全部修改都不在重建结果中；'
                     '可用的数据库快照：%s', error, corrupt.name,
                     '、'.join(str(path.relative_to(self.data_dir)) for path in snapshots) or '无')

    _RESTORE_HINT = ('用“导出全部数据”的备份导入；或把 migrations/ 下 import/、legacy-config/ 快照里的 '
                     'configflow.sqlite3 复制回数据目录；migration/ 快照只有迁移前的旧配置文件，需放回后重新迁移或导入。')

    def _import_changed_legacy_locked(self):
        """Import config.json once if an older version edited it during a rollback."""
        if self.legacy_path.is_symlink() or not self.legacy_path.is_file():
            return
        record = self._store.meta('legacy_config')
        record = record if isinstance(record, dict) else {}
        sha256, size = self._legacy_fingerprint()
        if sha256 == record.get('sha256') and size == record.get('size'):
            return
        now = _now()
        rejected = {**record, 'sha256': sha256, 'size': size, 'status': 'rejected', 'at': now, 'data_source': False}
        try:
            raw = self._read_json(self.legacy_path)
            converted = self._convert_import(raw)
            self._ensure_rule_proxy_token(converted['system'])
            self._validate_document(converted)
        except Exception as error:  # any unusable content: keep the database as it is
            logger.warning('config.json 在迁移后被修改，但内容无法导入（%s）；已忽略，数据库未改动', error)
            self._commit_meta_locked({'legacy_config': {**rejected, 'error': str(error)}})
            return
        baseline = record.get('revisions') if isinstance(record.get('revisions'), dict) else {}
        revisions = self._revisions_of(converted)
        old_profiles = baseline.get('profiles') if isinstance(baseline.get('profiles'), dict) else {}

        def above(value, old):
            return isinstance(value, int) and (not isinstance(old, int) or value > old)

        # Older versions rewrite config.json at startup without edits (token,
        # re-migration, defaults). Only a revision bump or a changed profile set
        # shows an edit; anything else would replace newer data with stale data.
        current_schema = raw.get('schema_version') == self.SCHEMA_VERSION
        content = _content_digest(raw)
        same_content = content == record.get('content')
        if not current_schema and record.get('content') is not None:
            # The original single-file format (and schema 2) has no revision
            # counters: any change of its content is an edit.
            edited = not same_content
        else:
            # No content baseline (config.json was missing or unreadable at
            # migration): as for current-schema files, only revisions count.
            edited = (above(revisions['system'], baseline.get('system')) or
                      above(revisions['shared'], baseline.get('shared')) or
                      set(revisions['profiles']) != set(old_profiles) or
                      any(above(value, old_profiles.get(key)) for key, value in revisions['profiles'].items()))
        if not edited:
            logger.info('config.json 在迁移后被改写，但没有新的修改；已记录，不导入')
            # Same data as recorded: it stays a source exactly when the
            # previous file was one (e.g. only a token was added).
            self._commit_meta_locked({'legacy_config': {
                **record, 'sha256': sha256, 'size': size, 'status': 'ignored', 'at': now,
                'data_source': record.get('data_source') is True and (current_schema or same_content)}})
            return
        meta = {'legacy_config': {'sha256': sha256, 'size': size, 'revisions': revisions, 'content': content,
                                  'status': 'imported', 'at': now, 'data_source': True},
                'last_legacy_import': {'at': now, 'sha256': sha256, 'snapshot': None}}
        try:
            with self._write_locked(meta=meta) as (previous, document):
                snapshot = self._apply_import(document, converted, 'legacy-config')
                meta['last_legacy_import']['snapshot'] = str(snapshot.relative_to(self.data_dir))
        except ProfileRepositoryError as error:
            # E.g. it would drop a router's published ownership record. Never
            # leave the service unable to start over a rollback leftover.
            logger.warning('config.json 在迁移后被修改，但导入被拒绝（%s）；已忽略，数据库未改动', error)
            self._commit_meta_locked({'legacy_config': {**rejected, 'error': str(error)}})
            return
        logger.warning('检测到旧版本在回退期间修改了 config.json，已导入为当前配置；'
                       '导入前的数据库已备份到 %s', meta['last_legacy_import']['snapshot'])

    def _commit_meta_locked(self, meta):
        with self._write_locked(meta=meta):
            pass

    def _check_migration_path(self, path):
        for part in (path, *path.parents):
            if part == self.data_dir:
                return
            if part.is_symlink():
                raise ProfileValidationError('旧配置路径不能是符号链接')
        raise ProfileValidationError('旧配置路径不在数据目录内')

    def _read_legacy_json(self, path, validator):
        """Read old files/backups without repairing them before migration commits."""
        error = None
        candidates = (path, path.with_name(path.name + '.bak'))
        for candidate in candidates:
            self._check_migration_path(candidate)
        for candidate in candidates:
            try:
                value = self._read_json(candidate)
            except ProfileRepositoryError as failure:
                if error is None:
                    error = failure
                continue
            # A readable but unsupported/invalid index is not a corrupt JSON
            # file: do not silently downgrade it to an older backup.
            validator(value)
            return value
        raise error

    @classmethod
    def _validate_legacy_index(cls, system):
        if system.get('schema_version') != 2:
            raise ProfileValidationError('不支持的旧配置索引版本；请保留原数据，不要重置')
        profiles = _items_by_id(system.get('profiles'), '旧配置索引')
        if 'default' not in profiles:
            raise ProfileValidationError('旧配置索引缺少默认配置')
        for profile_id in profiles:
            cls.validate_profile_id(profile_id)

    def _read_profile_layout(self):
        system = self._read_legacy_json(self.data_dir / 'system.json', self._validate_legacy_index)
        profiles = {}
        for metadata in system['profiles']:
            profile_id = metadata['id']
            path = self.profiles_dir / profile_id / 'config.json'
            profiles[profile_id] = self._read_legacy_json(path, lambda value: None)
        return {'schema_version': 2, 'system': system, 'profiles': profiles}

    def _migrate_profile_cache(self, source, document, remaps):
        """Copy only caches belonging to the selected old resource definitions."""
        from backend.utils.rule_utils import sanitize_rule_name
        libraries = {item['id']: item for item in document['shared']['rule_library']}
        targets = set()
        copied = set()
        for profile_id, old in source['profiles'].items():
            for subscription in old.get('subscriptions', []):
                old_id = subscription.get('id')
                new_id = remaps[profile_id]['subscriptions'].get(old_id)
                if new_id is None:
                    continue
                filename = old_id.replace('/', '_').replace('\\', '_') + '.json'
                path = self.profile_path(profile_id, 'subscribes/' + filename)
                target = self.shared_path('subscribes/' + new_id.replace('/', '_').replace('\\', '_') + '.json')
                targets.add(target)
                if path.is_file() and target not in copied:
                    payload = self._read_json(path)
                    payload['subscription_id'] = new_id
                    for node in payload.get('nodes', []):
                        if isinstance(node, dict) and node.get('subscription_id') == old_id:
                            node['subscription_id'] = new_id
                    target.parent.mkdir(parents=True, exist_ok=True)
                    self._write_json(target, payload)
                    copied.add(target)
            for new_id, old_name in remaps[profile_id]['rule_cache'].items():
                library = libraries[new_id]
                path = self.profile_path(profile_id, 'rules/' + sanitize_rule_name(old_name) + '.list')
                target = self.shared_path('rules/' + sanitize_rule_name(library['name']) + '.list')
                targets.add(target)
                if target in copied:
                    continue
                if library.get('source_type') == 'content':
                    target.parent.mkdir(parents=True, exist_ok=True)
                    self._write_atomic(target, library.get('content', ''))
                    copied.add(target)
                elif path.is_file():
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(path, target)
                    copied.add(target)
        # Shared caches from an interrupted/incorrect older migration cannot
        # substitute for missing profile-local caches with different sources.
        for target in targets - copied:
            if target.is_file():
                target.unlink()


    def _snapshot(self, reason, legacy_profiles=()):
        """Copy the database (and, for migrations, the legacy files) aside.

        Callers hold .config.lock and no SQLite transaction.
        """
        self._check_migration_path(self.migrations_dir)
        destination = self._create_migration_snapshot_dir() / reason
        destination.mkdir()
        if self.db_path.is_file():
            self._store.backup_to(destination / self.DATABASE_FILENAME)
        legacy_names = ('config.json', 'config.json.bak', 'system.json', 'system.json.bak')
        for name in legacy_names if reason in ('migration', 'legacy-config') else ():
            source = self.data_dir / name
            self._check_migration_path(source)
            if source.is_file():
                shutil.copy2(source, destination / name)
        for profile_id in legacy_profiles:
            for name in ('config.json', 'config.json.bak'):
                self._check_migration_path(self.profiles_dir / profile_id / name)
                source = self.profile_path(profile_id, name)
                if source.is_file():
                    target = destination / 'profiles' / profile_id / name
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy2(source, target)
        for path in destination.rglob('*'):
            if path.is_file():
                with path.open('rb') as handle:
                    os.fsync(handle.fileno())
        directories = [path for path in destination.rglob('*') if path.is_dir()]
        for directory in sorted(directories, key=lambda path: len(path.parts), reverse=True):
            self._fsync_dir(directory)
        for directory in (destination, destination.parent, self.migrations_dir, self.data_dir):
            self._fsync_dir(directory)
        return destination

    def _migrate_raw_cache(self):
        for kind in ('subscribes', 'rules'):
            source = self.data_dir / kind
            target = self.shared_path(kind)
            target.mkdir(parents=True, exist_ok=True)
            if source.is_dir():
                for item in source.iterdir():
                    if item.is_file() and not (target / item.name).exists():
                        shutil.copy2(item, target / item.name)

    def _load_entry(self):
        with self._store.reading() as connection:  # head and rows from one snapshot
            head = self._store.head(connection)
            cached = self._document_cache
            if cached is not None and cached.head == head:
                return cached
            entry = self._store.load(connection)
        self._validate_document(entry.document)
        self._set_cache(entry)
        return entry

    def _read_entry(self, *, locked=False):
        """The current validated document; reads never need .config.lock."""
        try:
            try:
                return self._load_entry()
            except (ConfigStorageError, ProfileRepositoryError, StoreError, ValueError, TypeError):
                # Network filesystems may not honour SQLite's own locks: retry
                # once serialized with writers before reporting the failure.
                if locked or self._holds_lock(self.initialization_lock_file):
                    raise
            with self._lock(self.initialization_lock_file):
                return self._load_entry()
        except StoreConflict as error:
            raise ProfileRepositoryError('配置数据库在事务外被修改，已放弃本次写入，请重试') from error
        except StoreError as error:
            raise ProfileRepositoryError(f'配置数据库内容无效：{error}') from error

    def _set_cache(self, entry):
        if entry.size > self.DOCUMENT_CACHE_MAX_BYTES:
            return
        # Always the latest loaded or committed entry: every read compares its
        # head with the database, so an older one only costs a reload, while a
        # guard on the generation number would keep a stale entry after a
        # restored (older) database reaches the same generation again.
        with self._cache_lock:
            self._document_cache = entry

    def _drop_cache(self):
        with self._cache_lock:
            self._document_cache = None

    @staticmethod
    def _router_records(document):
        records = {}
        for profile_id, profile in document['profiles'].items():
            routers = profile.get('routers') or {}
            if not isinstance(routers, dict):
                raise ProfileValidationError('路由器设置必须是对象')
            for router_id, router in _items_by_id(routers.get('items', []), '路由器').items():
                records[(profile_id, router_id)] = router
        return records

    @contextmanager
    def _router_commit_guard(self, previous, proposed, locked_routers=(), policy_router=None):
        """Protect device ownership under the document lock, through COMMIT."""
        from backend.routers.rule_sync_store import job_lock

        old_routers = self._router_records(previous)
        new_routers = self._router_records(proposed)
        changed = {}
        for key, old in old_routers.items():
            new = new_routers.get(key)
            connection_changed = new is None or any(
                old.get(field) != new.get(field) for field in ROUTER_CONNECTION_FIELDS)
            identity_changed = new is None or any(
                old.get(field) != new.get(field) for field in ROUTER_IDENTITY_FIELDS)
            manifest_changed = new is None or any(
                old.get(field) != new.get(field) for field in ROUTER_MANIFEST_FIELDS)
            # Credentials may have expired on the same device. Keep its exact
            # ownership receipt while allowing access to be repaired; every
            # connection change still takes the publication lock below.
            for field, label in (('ros_policy_state', '策略路由'), ('dns_guard_state', '强制 DNS 规则'),
                                 ('ipv6_fakeip_state', 'IPv6 Fake-IP 路由'),
                                 ('ipv6_routes_state', 'IPv6 规则集路由'),
                                 ('ipv6_pd_guard_state', 'IPv6 前缀变化保护')):
                field_changed = new is None or old.get(field) != new.get(field)
                if old.get(field) and (identity_changed or (field_changed and key != policy_router)):
                    profile_id, router_id = key
                    raise ProfileInUse(f'路由器仍有已发布的{label}；请先发布并核对撤回，再修改类型、地址或删除发布记录',
                                       [{'kind': 'router', 'id': router_id, 'name': old.get('name', router_id),
                                         'profile_id': profile_id,
                                         'profile_name': previous['profiles'][profile_id]['name']}])
            if connection_changed or manifest_changed:
                changed[key] = old
        # Never wait for a job while holding the document lock: a publisher may
        # itself be waiting to persist its readback. A failed acquisition rejects
        # the whole transaction, including first publication without a manifest.
        with ExitStack() as jobs:
            connections = set()
            for (profile_id, router_id), router in changed.items():
                if (profile_id, router_id) in locked_routers:
                    continue
                connection = (router.get('type'), str(router.get('endpoint') or '').rstrip('/'))
                if connection in connections:
                    continue
                if not jobs.enter_context(job_lock(profile_id, router_id, router)):
                    raise ProfileInUse('路由器正在发布或同步，不能替换连接或删除记录，请稍后重试')
                connections.add(connection)
            yield

    @contextmanager
    def _document(self, write=False, *, locked_routers=(), policy_router=None):
        nested = getattr(self._local, 'document', None)
        if nested is not None:
            if write:
                raise ProfileInUse('不能在配置事务中再次开启写事务')
            yield nested
            return
        if not write:
            document = self._read_entry().document
            self._local.document = document
            try:
                yield document
            finally:
                self._local.document = None
            return
        with self._lock(self.initialization_lock_file):
            with self._write_locked(locked_routers=locked_routers, policy_router=policy_router) as (previous, document):
                yield document
        if document['system'].get('backup', {}).get('auto_backup'):
            from backend.utils.webdav_backup import configuration_committed
            configuration_committed(self, previous, document)

    @contextmanager
    def _committed_source(self):
        """产出 (document, committed)。

        committed 为真时 document 是已提交的一代：写事务只修改它的深拷贝，提交后
        生成新的 Entry，所以调用方可以长期持有这个引用作为三方合并基线的来源，
        但绝不能修改它。事务内读到的是仍会被修改的工作副本，committed 为假。
        """
        committed = getattr(self._local, 'document', None) is None
        with self._document() as document:
            yield document, committed

    @contextmanager
    def _write_locked(self, *, locked_routers=(), policy_router=None, meta=None):
        """One write transaction; the caller holds .config.lock.

        Yields (previous, document): the committed generation and the copy to
        mutate. Only rows that changed are written, in one SQLite transaction.
        """
        if getattr(self._local, 'document', None) is not None:
            raise ProfileInUse('不能在配置事务中再次开启写事务')
        base = self._read_entry(locked=True)
        previous = base.document
        document = copy.deepcopy(previous)
        self._local.document = document
        try:
            yield previous, document
            self._ensure_rule_proxy_token(document['system'])
            self._validate_document(document)
            with self._router_commit_guard(previous, document, locked_routers, policy_router):
                try:
                    committed = self._store.commit(base, document, meta=meta)
                except StoreConflict as error:
                    self._drop_cache()
                    raise ProfileRepositoryError('配置数据库在事务外被修改，已放弃本次写入，请重试') from error
                except BaseException:
                    self._drop_cache()
                    raise
            if committed is not None:
                self._set_cache(committed)
                self.commit_count += 1
        finally:
            self._local.document = None

    @staticmethod
    def _profile(document, profile_id):
        ProfileRepository.validate_profile_id(profile_id)
        if profile_id not in document['profiles']:
            raise ProfileNotFound(profile_id)
        return document['profiles'][profile_id]

    @staticmethod
    def _metadata(profile):
        return {key: copy.deepcopy(profile.get(key, '')) for key in ('id', 'name', 'description', 'created_at', 'updated_at')}

    @classmethod
    def _default_profile_id(cls, document):
        """默认配置空间可以切换：记录在 system_config.default_profile_id，未设置时沿用 default。"""
        return document['system']['system_config'].get('default_profile_id') or cls.DEFAULT_PROFILE_ID

    def default_profile_id(self):
        with self._document() as document:
            return self._default_profile_id(document)

    def list_profiles(self):
        with self._document() as document:
            shared_revision = int(document['shared'].get('_revision', 0))
            default_id = self._default_profile_id(document)
            bindings = {}
            for agent in document['system']['agents']:
                bindings.setdefault(agent.get('profile_id') or default_id, []).append(self.agent_binding_summary(agent))
            # revision 即生成配置的版本号（见 config_revision），界面据此判断 Agent 是否待更新
            return [dict(self._metadata(profile), revision=int(profile.get('_revision', 0)) + shared_revision,
                         is_default=profile['id'] == default_id, bound_agents=bindings.get(profile['id'], []))
                    for profile in document['profiles'].values()]

    @staticmethod
    def agent_binding_summary(agent):
        """Only expose device identity here, never credentials or deployment payloads."""
        return {'id': agent['id'], 'name': agent.get('name') or agent['id'],
                'host': agent.get('host', ''), 'port': agent.get('port'),
                'service_type': agent.get('service_type') or 'mihomo'}

    def get_profile_agent_bindings(self, profile_id):
        with self._document() as document:
            profile = self._profile(document, profile_id)
            default_id = self._default_profile_id(document)
            return {'profile': self._metadata(profile), 'agents': [self.agent_binding_summary(agent)
                    for agent in document['system']['agents']
                    if (agent.get('profile_id') or default_id) == profile_id]}

    def _profile_metadata(self, profile_id):
        # 只取元数据，不复制整个 profile（含规则、路由器发布清单等）
        with self._document() as document:
            return self._metadata(self._profile(document, profile_id))

    def get_profile(self, profile_id):
        with self._document() as document:
            return copy.deepcopy(self._profile(document, profile_id))

    def config_revision(self, profile_id):
        """生成配置的版本号：配置空间或共享资源任一变化都会让它增大，用于判断 Agent 是否待更新。"""
        with self._document() as document:
            profile = self._profile(document, profile_id)
            return int(profile.get('_revision', 0)) + int(document['shared'].get('_revision', 0))

    def get_publish_snapshot(self, profile_id):
        """Capture generated inputs and their revision from the same read snapshot."""
        with self._document() as document:
            profile = self._profile(document, profile_id)
            revision = int(profile.get('_revision', 0)) + int(document['shared'].get('_revision', 0))
            return self._resolve_profile(document, profile_id), revision

    def create_profile(self, metadata, clone_from=None):
        if not isinstance(metadata, dict):
            raise ProfileValidationError('Profile metadata must be an object')
        profile_id = self.validate_profile_id(metadata.get('id') or f'profile_{uuid.uuid4().hex[:12]}')
        name = metadata.get('name') or profile_id
        if not isinstance(name, str) or not name.strip():
            raise ProfileValidationError('配置名称不能为空')
        with self._document(write=True) as document:
            if profile_id in document['profiles']:
                raise ProfileExists(profile_id)
            profile = copy.deepcopy(self._profile(document, clone_from)) if clone_from else self._empty_profile(profile_id, name)
            if clone_from:
                # Router credentials, sync settings and ownership belong to the source devices.
                profile['routers'] = {'items': []}
                profile['client_fixed_ips'] = {}
                from backend.agents.node_test_dns import strip_owned_for_clone
                try:
                    strip_owned_for_clone(document['system'], clone_from, profile)
                except ValueError as error:
                    raise ProfileInUse(str(error)) from None
            profile.update(id=profile_id, name=name.strip(), description=str(metadata.get('description') or ''),
                           created_at=_now(), updated_at=_now(), _revision=0)
            document['profiles'][profile_id] = profile
        return self._metadata(profile)

    def import_client_profile(self, metadata, build):
        """新建配置空间并追加共享资源，二者在同一事务中校验和写入。

        build(shared_snapshot) 返回 {'shared': {字段: [新增资源]}, 'profile': {配置字段}, ...}，
        校验失败时不写入任何内容。
        """
        if not isinstance(metadata, dict):
            raise ProfileValidationError('Profile metadata must be an object')
        profile_id = self.validate_profile_id(metadata.get('id') or f'profile_{uuid.uuid4().hex[:12]}')
        name = metadata.get('name')
        if not isinstance(name, str) or not name.strip():
            raise ProfileValidationError('配置名称不能为空')
        with self._document(write=True) as document:
            if profile_id in document['profiles']:
                raise ProfileExists(profile_id)
            result = build(copy.deepcopy(document['shared']))
            shared = document['shared']
            additions = result.get('shared', {})
            if any(additions.get(field) for field in SHARED_FIELDS):
                for field in SHARED_FIELDS:
                    shared[field].extend(copy.deepcopy(additions.get(field, [])))
                shared['_revision'] += 1
            profile = self._empty_profile(profile_id, name.strip())
            for key, value in result.get('profile', {}).items():
                if key not in PROFILE_FIELDS:
                    raise ProfileValidationError(f'导入结果包含未知字段：{key}')
                profile[key] = _deep_merge(profile[key], value) if isinstance(profile[key], dict) else copy.deepcopy(value)
            profile['description'] = str(metadata.get('description') or '')
            document['profiles'][profile_id] = profile
        return {**result, 'profile': self._metadata(profile)}

    def clone_profile(self, source_id, metadata):
        return self.create_profile(metadata, clone_from=source_id)

    def update_profile(self, profile_id, updates):
        if not isinstance(updates, dict) or set(updates) - {'name', 'description'}:
            raise ProfileValidationError('只能修改配置名称和说明')
        with self._document(write=True) as document:
            profile = self._profile(document, profile_id)
            for key, value in updates.items():
                if not isinstance(value, str) or (key == 'name' and not value.strip()):
                    raise ProfileValidationError('配置名称和说明必须是字符串，名称不能为空')
                profile[key] = value.strip() if key == 'name' else value
            profile['_revision'] += 1
            profile['updated_at'] = _now()
        return self._metadata(profile)

    def set_default_profile(self, profile_id):
        """Make another profile the default; returns whether it changed.

        Stored Agents always name their profile, so no device moves. Only
        unscoped configuration links, newly registered Agents and MCP tools
        follow the new default.
        """
        with self._document(write=True) as document:
            self._profile(document, profile_id)
            if self._default_profile_id(document) == profile_id:
                return False
            document['system']['system_config']['default_profile_id'] = profile_id
            document['system']['_revision'] += 1
            return True

    def delete_profile(self, profile_id, new_default_id=None):
        """删除配置空间；删除默认配置时必须指定接任默认的配置空间（new_default_id）。"""
        with self._lock(self._profile_operation_lock_path(profile_id)), ExitStack() as jobs:
            tombstone = None
            moved = False
            exported = None
            directory = self.profile_dir(profile_id)
            locked_routers = set()
            try:
                with self._document(write=True, locked_routers=locked_routers) as document:
                    profile = self._profile(document, profile_id)
                    default_id = self._default_profile_id(document)
                    is_default = profile_id == default_id
                    if is_default:
                        if len(document['profiles']) < 2:
                            raise ProfileInUse('只有一个配置空间时不能删除默认配置')
                        if not new_default_id:
                            raise ProfileInUse('删除默认配置时需要指定新的默认配置空间')
                        if new_default_id == profile_id or new_default_id not in document['profiles']:
                            raise ProfileValidationError(f'新的默认配置空间不存在：{new_default_id}')
                    usages = [{'kind': 'agent', 'id': a['id'], 'name': a.get('name') or a['id'], 'profile_id': profile_id, 'profile_name': profile['name']}
                              for a in document['system']['agents'] if a.get('profile_id') == profile_id]
                    if usages:
                        names = '、'.join(usage['name'] for usage in usages)
                        raise ProfileInUse(f'配置「{profile["name"]}」仍被以下 Agent 使用：{names}', usages)
                    # Keep job locks until after the document commits, and take
                    # them before moving profile-local artifacts out of the way.
                    without_profile = {'profiles': {key: value for key, value in document['profiles'].items()
                                                    if key != profile_id}}
                    jobs.enter_context(self._router_commit_guard(document, without_profile))
                    locked_routers.update(key for key in self._router_records(document) if key[0] == profile_id)
                    self.migrations_dir.mkdir(exist_ok=True)
                    tombstone = self.migrations_dir / f'deleted-{profile_id}-{uuid.uuid4().hex}'
                    if directory.exists():
                        os.replace(directory, tombstone)
                        moved = True
                    else:
                        tombstone.mkdir(parents=True)
                    # The database keeps no old generations: retain the deleted
                    # profile itself (export_profile shape, 0600: it holds credentials).
                    exported = tombstone / 'deleted-profile.json'
                    if exported.exists():
                        exported = tombstone / f'deleted-profile-{uuid.uuid4().hex}.json'
                    temporary = self._write_temp(exported, json.dumps(profile, ensure_ascii=False, indent=2) + '\n')
                    try:
                        os.replace(temporary, exported)
                    finally:
                        temporary.unlink(missing_ok=True)
                    self._fsync_dir(tombstone)
                    del document['profiles'][profile_id]
                    if is_default:
                        document['system']['system_config']['default_profile_id'] = new_default_id
                        document['system']['_revision'] += 1
            except Exception:
                if exported is not None:
                    exported.unlink(missing_ok=True)
                if tombstone is not None and tombstone.exists():
                    if moved:
                        os.replace(tombstone, directory)
                    else:
                        shutil.rmtree(tombstone, ignore_errors=True)
                raise

    @staticmethod
    def _apply_updater(current, updater):
        if not callable(updater):
            raise ProfileValidationError('Updater must be callable')
        replacement = updater(current)
        if replacement is not None:
            if not isinstance(replacement, dict):
                raise ProfileValidationError('Updater must return an object or None')
            return copy.deepcopy(replacement)
        return current

    def _update_profile_locked(self, document, profile_id, updater):
        """Apply one profile updater inside an open write transaction."""
        current = self._profile(document, profile_id)
        previous_group_names = {group.get('name') for group in current['proxy_groups']}
        # Updaters may inspect hydrated resources but cannot persist copies.
        snapshot = self._resolve_profile(document, profile_id)
        updated = self._apply_updater(snapshot, updater)
        from backend.agents.node_test_dns import guard_profile_change
        try:
            guard_profile_change(document['system'], profile_id, current, updated)
        except ValueError as error:
            raise ProfileInUse(str(error)) from None
        for key in PROFILE_FIELDS:
            if key in updated:
                current[key] = copy.deepcopy(updated[key])
        removed_names = previous_group_names - {group.get('name') for group in current['proxy_groups']}
        for rule in current['rule_configs']:
            if rule.get('policy') in removed_names:
                raise ProfileInUse(f'配置 {current["name"]} 的规则仍引用被删除策略组：{rule["policy"]}',
                                   [{'kind': 'profile', 'id': profile_id, 'name': current['name']}])
        for rule in current['rule_configs']:
            if rule.get('itemType') == 'ruleset':
                for key in (*RULE_SOURCE_FIELDS, 'library_enabled'):
                    rule.pop(key, None)
        current['_revision'] += 1
        current['updated_at'] = _now()
        return current

    def update_profile_transaction(self, profile_id, updater, *, include_config_revision=False):
        with self._document(write=True) as document:
            current = self._update_profile_locked(document, profile_id, updater)
            revision = int(current['_revision']) + int(document['shared'].get('_revision', 0))
        saved = copy.deepcopy(current)
        return (saved, revision) if include_config_revision else saved

    def save_router_settings(self, profile_id, expected_router, replacement):
        """Commit PUT/DELETE while the caller holds this router's job_lock."""
        router_id = expected_router['id']
        key = (profile_id, router_id)
        with self._document(write=True, locked_routers={key}) as document:
            profile = self._profile(document, profile_id)
            current = self._router_records(document).get(key)
            if current != expected_router:
                raise ProfileInUse('路由器设置已变化，请刷新后重试')
            if replacement is not None and replacement.get('id') != router_id:
                raise ProfileValidationError('不能修改路由器 ID')
            items = profile['routers']['items']
            index = next(index for index, item in enumerate(items) if item['id'] == router_id)
            if replacement is None:
                del items[index]
            else:
                items[index] = copy.deepcopy(replacement)
            profile['_revision'] += 1
            profile['updated_at'] = _now()

    def save_router_publication(self, profile_id, expected_router, *, manifest=_MISSING, dns_guard=_MISSING,
                                ipv6_fakeip=_MISSING, ipv6_routes=_MISSING, ipv6_pd_guard=_MISSING,
                                last_release=_MISSING):
        """Persist verified readback/rollback under the caller's held job_lock.

        This is the only existing-record write allowed to replace or clear an
        active manifest. It cannot delete a router or alter its connection, and
        unrelated edits made during the device operation are retained.
        """
        router_id = expected_router['id']
        key = (profile_id, router_id)
        with self._document(write=True, locked_routers={key}, policy_router=key) as document:
            profile = self._profile(document, profile_id)
            current = self._router_records(document).get(key)
            if current is None or any(current.get(field) != expected_router.get(field)
                                      for field in ROUTER_CONNECTION_FIELDS):
                raise ProfileInUse('路由器连接已变化，无法保存发布回读记录')
            for field, value in (('ros_policy_state', manifest), ('dns_guard_state', dns_guard),
                                 ('ipv6_fakeip_state', ipv6_fakeip), ('ipv6_routes_state', ipv6_routes),
                                 ('ipv6_pd_guard_state', ipv6_pd_guard)):
                if value is _MISSING:
                    continue
                if value:
                    current[field] = copy.deepcopy(value)
                else:
                    current.pop(field, None)
            if last_release is not _MISSING:
                current['last_release'] = copy.deepcopy(last_release)
            profile['_revision'] += 1
            profile['updated_at'] = _now()

    def update_profile_fields(self, profile_id, fields, baseline=None, *, include_config_revision=False):
        if not isinstance(fields, dict) or (baseline is not None and not isinstance(baseline, dict)):
            raise ProfileValidationError('Profile fields and baseline must be objects')
        if set(fields) & {'resource_refs', 'node_dialers'}:
            raise ProfileValidationError('资源由策略组直接引用，拨号组合请使用代理链类型')
        def update(current):
            if baseline is None and '_revision' in fields and fields['_revision'] != current['_revision']:
                raise ProfileInUse('配置已更新，请刷新后重试')
            for key, value in fields.items():
                if key in PROFILE_FIELDS:
                    current[key] = _three_way_merge(current.get(key), baseline[key], value) if baseline is not None and key in baseline else copy.deepcopy(value)
        return self.update_profile_transaction(profile_id, update, include_config_revision=include_config_revision)

    def save_profile(self, profile_id, data, baseline=None, *, include_config_revision=False):
        return self.update_profile_fields(profile_id, data, baseline, include_config_revision=include_config_revision)


    def get_shared(self):
        with self._document() as document:
            return copy.deepcopy(document['shared'])

    def get_shared_with_source(self):
        """共享资源副本，以及它所读的已提交 shared 节（见 _committed_source）。"""
        with self._committed_source() as (document, committed):
            return copy.deepcopy(document['shared']), (document['shared'] if committed else None)

    def create_rule_library_item(self, rule, content):
        """Commit a new source and its prepared cache, never replace an existing ID.

        Fetching happens before this method. Keep the cache lock through the
        document commit so a failed commit can remove only this creation's file,
        before another writer can publish data at the same resource identity.
        """
        from backend.utils.rule_utils import rule_cache_filename

        _items_by_id([rule], 'rule_library')
        if not isinstance(content, (str, bytes)):
            raise ProfileValidationError('规则内容必须是文本或二进制规则数据')
        path = self.shared_path(os.path.join('rules', rule_cache_filename(rule)))
        preserve_cache = False
        owns_cache = False

        def remove_uncommitted_cache():
            if not preserve_cache:
                path.unlink(missing_ok=True)

        with ExitStack() as artifacts:
            try:
                with self._document(write=True) as document:
                    shared = document['shared']
                    if any(item['id'] == rule['id'] for item in shared['rule_library']):
                        raise ProfileExists('规则库 ID 已存在，请刷新后重试')
                    artifacts.enter_context(self._lock(path.with_name(path.name + '.lock')))
                    # A previous/orphaned cache also belongs to an existing identity;
                    # importing a new source must not overwrite its custom bytes.
                    if path.exists() or path.with_name(path.name + '.bak').exists():
                        raise ProfileExists('规则缓存已存在，请使用新的规则 ID')
                    owns_cache = True
                    artifacts.callback(remove_uncommitted_cache)
                    shared['rule_library'].append(copy.deepcopy(rule))
                    shared['_revision'] += 1
                    self._write_atomic(path, content)
            except Exception as error:
                if owns_cache:
                    # A failed commit normally rolls back. If the outcome is
                    # unknown (COMMIT reported an error), retain the cache for
                    # any possibly committed source. Reads need no document
                    # lock, so the cache lock order is unaffected.
                    try:
                        current = self._store.load().document
                        preserve_cache = any(item.get('id') == rule['id']
                                             for item in current['shared']['rule_library'])
                    except Exception:
                        preserve_cache = True
                    if preserve_cache:
                        raise RuleLibraryCreationUncertain('规则保存结果待确认，请刷新列表核对，勿重复导入') from error
                raise
            preserve_cache = True
        return copy.deepcopy(rule)

    def _apply_shared_locked(self, document, updater):
        """Apply one shared updater inside an open write transaction."""
        current = document['shared']
        updated = self._apply_updater(copy.deepcopy(current), updater)
        proposed = {key: copy.deepcopy(updated.get(key, current[key])) for key in SHARED_FIELDS}
        for kind in SHARED_FIELDS:
            remaining = _items_by_id(proposed[kind], kind)
            for resource in current[kind]:
                disabled = kind != 'rule_library' and resource.get('enabled', True) and not remaining.get(resource['id'], {}).get('enabled', True)
                if resource['id'] not in remaining or disabled:
                    usages = self._resource_usage(document, kind, resource['id'])
                    if usages:
                        raise ProfileInUse(f'资源 {resource.get("name", resource["id"])} 仍被引用：' + '、'.join(u['name'] for u in usages), usages)
        proposed['_revision'] = current['_revision'] + 1
        document['shared'] = proposed
        return proposed

    def update_shared_transaction(self, updater):
        with self._document(write=True) as document:
            proposed = self._apply_shared_locked(document, updater)
        return copy.deepcopy(proposed)

    def _node_deletion_plan(self, document, node_ids):
        from backend.common.node_deletion import plan_node_deletion
        preview, candidate = plan_node_deletion(document, node_ids)
        if not preview['blockers']:
            try:
                self._validate_document(candidate)
            except ProfileRepositoryError as error:
                preview['blockers'].append(str(error))
        return preview, candidate

    def preview_node_deletion(self, node_ids):
        with self._document() as document:
            preview, _ = self._node_deletion_plan(document, node_ids)
        return preview

    def delete_nodes_with_references(self, node_ids, confirmation):
        if not isinstance(confirmation, str) or not re.fullmatch(r'[a-f0-9]{64}', confirmation):
            raise ProfileValidationError('请先查看并确认删除影响')
        with self._document(write=True) as document:
            preview, candidate = self._node_deletion_plan(document, node_ids)
            if not secrets.compare_digest(confirmation, preview['confirmation']):
                raise NodeDeletionChanged('节点或引用关系已变化，请重新查看删除影响并确认')
            if preview['blockers']:
                raise ProfileInUse('无法删除：' + '；'.join(preview['blockers']), preview['usages'])
            changed_profiles = {group['profile_id'] for group in preview['groups']}
            for profile_id in changed_profiles:
                def update(profile, profile_id=profile_id):
                    profile['proxy_groups'] = candidate['profiles'][profile_id]['proxy_groups']
                self._update_profile_locked(document, profile_id, update)
            # Detach first inside the same transaction, then retain the normal
            # shared-resource deletion guard and the final document validation.
            document['shared']['subscription_aggregations'] = candidate['shared']['subscription_aggregations']
            def remove(shared):
                shared['nodes'] = candidate['shared']['nodes']
            self._apply_shared_locked(document, remove)
        return preview

    def update_shared_and_profiles_transaction(self, shared_updater, profile_updater, profile_ids):
        """Update shared resources and profiles in one commit; any failure writes nothing.

        profile_ids is a list, or a callable receiving the current (shared,
        profiles) sections read-only and returning one. profile_updater(profile_id,
        before, after) sees the profile resolved against the shared data before
        and after shared_updater, and edits after like any profile updater.
        Each section's revision advances once.
        """
        with self._document(write=True) as document:
            selected = profile_ids(document['shared'], document['profiles']) if callable(profile_ids) else profile_ids
            selected = list(dict.fromkeys(selected))
            before = {profile_id: self._resolve_profile(document, profile_id) for profile_id in selected}
            shared = self._apply_shared_locked(document, shared_updater)
            profiles = {}
            for profile_id in selected:
                def update(snapshot, profile_id=profile_id):
                    return profile_updater(profile_id, before[profile_id], snapshot)
                profiles[profile_id] = self._update_profile_locked(document, profile_id, update)
            result = copy.deepcopy(shared), copy.deepcopy(profiles)
        return result

    def save_shared(self, snapshot, baseline=None):
        if not isinstance(snapshot, dict):
            raise ProfileValidationError('Shared data must be an object')
        def update(current):
            if baseline is None and '_revision' in snapshot and snapshot['_revision'] != current['_revision']:
                raise ProfileInUse('共享资源已更新，请刷新后重试')
            for key in SHARED_FIELDS:
                if key in snapshot:
                    current[key] = _three_way_merge(current[key], baseline[key], snapshot[key]) if baseline is not None and key in baseline else copy.deepcopy(snapshot[key])
        return self.update_shared_transaction(update)

    def get_system(self):
        with self._document() as document:
            return copy.deepcopy(document['system'])

    def get_system_with_source(self):
        """系统节副本，以及它所读的已提交 system 节（见 _committed_source）。"""
        with self._committed_source() as (document, committed):
            return copy.deepcopy(document['system']), (document['system'] if committed else None)

    def update_system_transaction(self, updater):
        with self._document(write=True) as document:
            current = document['system']
            updated = self._apply_updater(copy.deepcopy(current), updater)
            from backend.agents.node_test_dns import guard_system_change
            try:
                guard_system_change(current, updated)
            except ValueError as error:
                raise ProfileInUse(str(error)) from None
            document['system'] = {key: copy.deepcopy(updated.get(key, current[key])) for key in ('system_config', 'backup', 'agents')}
            document['system']['_revision'] = current['_revision'] + 1
            self._ensure_rule_proxy_token(document['system'])
            result = copy.deepcopy(document['system'])
        return result

    def save_system(self, snapshot, baseline=None):
        if not isinstance(snapshot, dict):
            raise ProfileValidationError('System data must be an object')
        def update(current):
            if baseline is None and '_revision' in snapshot and snapshot['_revision'] != current['_revision']:
                raise ProfileInUse('系统设置已更新，请刷新后重试')
            for key in ('system_config', 'backup', 'agents'):
                if key in snapshot:
                    if baseline is not None and key in baseline:
                        current[key] = _three_way_merge(current[key], baseline[key], snapshot[key])
                    elif isinstance(current[key], dict) and isinstance(snapshot[key], dict):
                        current[key] = _deep_merge(current[key], snapshot[key])
                    else:
                        current[key] = copy.deepcopy(snapshot[key])
        return self.update_system_transaction(update)

    def get_resource_usage(self, resource_type, resource_id):
        if resource_type not in SHARED_FIELDS:
            raise ProfileValidationError('未知资源类型')
        with self._document() as document:
            return copy.deepcopy(self._resource_usage(document, resource_type, resource_id))

    def _resolve_profile(self, document, profile_id, resource_roots=None):
        profile = self._profile(document, profile_id)
        shared = document['shared']
        result = copy.deepcopy(profile)
        result['profile_id'] = profile_id
        for key in ('system_config', 'backup', 'agents'):
            result[key] = copy.deepcopy(document['system'][key])
        catalogs = {field: _items_by_id(shared[field], field) for field in RESOURCE_FIELDS}
        selected = _collect_resource_refs(profile, catalogs, resource_roots)
        for field in RESOURCE_FIELDS:
            result[field] = [copy.deepcopy(item) for item in shared[field] if item['id'] in selected[field]]
        result['rule_library'] = copy.deepcopy(shared['rule_library'])
        library = {item['id']: item for item in shared['rule_library']}
        from backend.common.rule_dns import hydrate_ruleset
        for rule in result['rule_configs']:
            if rule.get('itemType') == 'ruleset':
                hydrate_ruleset(rule, library[rule['library_rule_id']], profile_id)
        return result

    def get_compat_config(self, profile_id, *, resource_roots=None):
        with self._document() as document:
            return self._resolve_profile(document, profile_id, resource_roots)

    def get_compat_config_with_source(self, profile_id):
        """兼容视图，以及解析它的已提交文档（见 _committed_source）。"""
        with self._committed_source() as (document, committed):
            return self._resolve_profile(document, profile_id), (document if committed else None)

    def compat_config_from(self, document, profile_id):
        """用 get_compat_config_with_source 返回的文档重新解析，结果与当时的视图相同。"""
        return self._resolve_profile(document, profile_id)

    def export_profile(self, profile_id):
        return self.get_profile(profile_id)

    def import_profile(self, profile_id, data):
        if not isinstance(data, dict):
            raise ProfileValidationError('Imported profile must be an object')
        data = data.get('config', data)
        if not isinstance(data, dict) or any(key in data for key in SHARED_FIELDS):
            raise ProfileValidationError('独立配置导入只接受引用，不接受共享资源副本')
        if set(data) & {'resource_refs', 'node_dialers'}:
            raise ProfileValidationError('独立配置导入仅支持当前格式')
        def update(current):
            if '_revision' in data and data['_revision'] != current['_revision']:
                raise ProfileInUse('配置已更新，请刷新后重试')
            for key, value in data.items():
                if key in PROFILE_FIELDS:
                    current[key] = copy.deepcopy(value)
            # Imported rules replace the composition; per-rule DNS of rules it
            # no longer has would otherwise refuse the whole import.
            _prune_rule_dns(current)
        return self.update_profile_transaction(profile_id, update)

    def export_all(self, desensitize=False):
        with self._document() as document:
            result = copy.deepcopy(document)
        if desensitize:
            from backend.common.config_export import sanitize_external_payload
            system_config = copy.deepcopy(result['system']['system_config'])
            result['system']['system_config'] = {key: value for key, value in system_config.items()
                                                  if key not in {'config_token', 'rule_proxy_token', 'retired_rule_proxy_tokens'}}
            result['system']['backup'] = {}
            result['system']['agents'] = []
            for subscription in result['shared']['subscriptions']:
                subscription['url'] = '[REDACTED]'
            for node in result['shared']['nodes']:
                for key in ('server', 'password', 'uuid', 'private-key', 'proxy_string'):
                    if key in node:
                        node[key] = '[REDACTED]'
                node['params'] = {}
            for profile in result['profiles'].values():
                from backend.common.config_export import redact_oxidns_settings
                if 'oxidns' in profile:
                    profile['oxidns'] = redact_oxidns_settings(profile['oxidns'])
                if 'routers' in profile:
                    from backend.routers.store import redact_routers
                    profile['routers'] = redact_routers(profile['routers'])
                for engine in ('mihomo', 'surge', 'mosdns'):
                    profile[engine]['custom_config'] = ''
                # Recovery snapshots contain the original opaque YAML and DNS
                # credentials. Human-shareable exports must omit them entirely.
                profile['mihomo'].pop('linux_gateway_restore_point', None)
                if isinstance(profile.get('loon'), dict):
                    profile['loon']['custom_config'] = ''
            result = sanitize_external_payload(result, system_config)
        return result

    def _prepare_import(self, data):
        document = self._convert_import(data)
        self._ensure_rule_proxy_token(document['system'])
        self._validate_document(document)
        return document

    def _apply_import(self, previous, document, reason):
        """Replace the open transaction's document; returns the snapshot taken first."""
        snapshot = self._snapshot(reason)
        settings = document['system']['system_config']
        old_settings = previous['system']['system_config']
        retired = settings.setdefault('retired_rule_proxy_tokens', [])
        for token in [old_settings.get('rule_proxy_token'), *old_settings.get('retired_rule_proxy_tokens', [])]:
            if isinstance(token, str) and token and token != settings.get('rule_proxy_token') and token not in retired:
                retired.append(token)
        document['system']['_revision'] = previous['system']['_revision'] + 1
        document['shared']['_revision'] = previous['shared']['_revision'] + 1
        for key, profile in document['profiles'].items():
            profile['_revision'] = previous['profiles'].get(key, {}).get('_revision', 0) + 1
        previous.clear()
        previous.update(document)
        return snapshot

    def import_all(self, data):
        document = self._prepare_import(data)
        with self._document(write=True) as previous:
            self._apply_import(previous, document, 'import')
        return self.export_all()

    def reset_all(self):
        return self.import_all((self._initial_config_factory or self._legacy_defaults)())

    def shared_path(self, relative_path):
        if not isinstance(relative_path, str) or not relative_path or '\x00' in relative_path:
            raise ProfileValidationError('Invalid shared relative path')
        root = (self.data_dir / 'shared').resolve()
        candidate = (root / relative_path).resolve()
        try:
            root.relative_to(self.data_dir)
            candidate.relative_to(root)
        except ValueError as error:
            raise ProfileValidationError('Shared path escapes shared directory') from error
        return candidate

    def cache_dir(self, profile_id=None):
        if profile_id is not None:
            self.validate_profile_id(profile_id)
        return self.shared_path('subscribes')

    def shared_rules_dir(self):
        return self.shared_path('rules')

    def shared_cache_dir(self):
        return self.cache_dir()

    def read_shared_json(self, relative_path):
        path = self.shared_path(relative_path)
        with self._lock(path.with_name(path.name + '.lock')):
            return self._read_json(path)

    def write_shared_json(self, relative_path, data):
        path = self.shared_path(relative_path)
        with self._lock(path.with_name(path.name + '.lock')):
            self._write_json(path, data)
        return path

    def write_shared_text(self, relative_path, content):
        path = self.shared_path(relative_path)
        with self._lock(path.with_name(path.name + '.lock')):
            self._write_atomic(path, content)
        return path

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

    @staticmethod
    def _sanitization_tokens_of(system):
        """(规则代理令牌, Agent 令牌)，只读取，不复制 system。"""
        system_config = system.get("system_config", {})
        values = []
        if isinstance(system_config, dict):
            values.append(system_config.get("rule_proxy_token"))
            retired = system_config.get("retired_rule_proxy_tokens", [])
            if isinstance(retired, list):
                values.extend(retired)
        agents = system.get("agents", [])
        return (
            frozenset(value for value in values if isinstance(value, str) and value),
            frozenset(agent["token"] for agent in (agents if isinstance(agents, list) else [])
                      if isinstance(agent, dict) and isinstance(agent.get("token"), str) and agent["token"]),
        )

    def _sanitization_tokens(self):
        """当前一代文档里的令牌，按 store head 记住。

        直接读已提交文档，不深拷贝 system（含全部 Agent 的发布历史）。任何提交
        （包括令牌轮换）都会换 head，下一次读取就拿到新令牌；数据库被其他进程
        改写时，head 比较同样会发现。这些令牌本来就在常驻的文档缓存里，这里只多
        记住它们的集合，没有扩大凭据在进程内的暴露面；文档太大不进缓存时，也只
        多常驻这几个令牌字符串，不多常驻文档。
        """
        if getattr(self._local, 'document', None) is not None:
            # 事务内读到的是仍会被修改的工作副本，不缓存
            with self._document() as document:
                return self._sanitization_tokens_of(document['system'])
        entry = self._read_entry()
        cached = self._sanitization_tokens_cache
        if cached is not None and cached[0] == entry.head:
            return cached[1]
        tokens = self._sanitization_tokens_of(entry.document['system'])
        self._sanitization_tokens_cache = (entry.head, tokens)
        return tokens

    def rule_proxy_tokens_for_sanitization(self) -> set[str]:
        """Return persisted current and retired tokens for output sanitization only."""
        return set(self._sanitization_tokens()[0])

    def agent_tokens_for_sanitization(self) -> frozenset[str]:
        """Persisted Agent credentials, for output sanitization only."""
        return self._sanitization_tokens()[1]

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
            root.relative_to(self.data_dir)
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

    def providers_dir(self, profile_id: str) -> Path:
        return self.profile_dir(profile_id) / "providers"

    def rules_dir(self, profile_id: str) -> Path:
        return self.profile_dir(profile_id) / "rules"

    def generated_dir(self, profile_id: str) -> Path:
        return self.profile_dir(profile_id) / "generated"

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

    @staticmethod
    def _holds_lock(path: Path) -> bool:
        return str(path.resolve()) in getattr(_HELD_LOCKS, "paths", ())

    @contextmanager
    def _lock(self, path: Path) -> Iterator[None]:
        path.parent.mkdir(parents=True, exist_ok=True)
        thread_lock = self._thread_lock(path)
        key = str(path.resolve())
        with thread_lock:
            with path.open("a+b") as handle:
                if handle.seek(0, os.SEEK_END) == 0:
                    handle.write(b"0")
                    handle.flush()
                self._acquire_file_lock(handle)
                held = getattr(_HELD_LOCKS, "paths", None)
                if held is None:
                    held = _HELD_LOCKS.paths = {}
                held[key] = held.get(key, 0) + 1
                try:
                    yield
                finally:
                    held[key] -= 1
                    if not held[key]:
                        del held[key]
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

    def _write_temp(self, path: Path, content: str | bytes) -> Path:
        """Write and fsync a complete sibling file, removing failed short writes."""
        content = content.encode('utf-8') if isinstance(content, str) else content
        fd, temp_name = tempfile.mkstemp(prefix=f'.{path.name}.', suffix='.tmp', dir=path.parent)
        temp_path = Path(temp_name)
        try:
            with os.fdopen(fd, 'wb') as handle:
                handle.write(content)
                handle.flush()
                os.fsync(handle.fileno())
            written = temp_path.stat().st_size
            expected = len(content)
            if written != expected:
                raise ProfileRepositoryError(f'Incomplete write for {path}: {written} of {expected} bytes')
            return temp_path
        except Exception:
            temp_path.unlink(missing_ok=True)
            raise

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

    def write_generated(self, profile_id: str, filename: str, content: str) -> Path:
        if Path(filename).name != filename or filename not in {"config.yaml", "config.conf", "loon.lcf"}:
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

    @staticmethod
    def _scope_legacy_dialers(profiles):
        """Bind raw dialers locally before merging independent node catalogs."""
        from backend.utils.dialer_references import raw_dialer, validate_shapes, DialerReferenceError
        from backend.converters.mihomo import _parse_structured_proxy_string

        profiles = copy.deepcopy(profiles)
        definitions = {}
        group_names = set()
        used_names = set(BUILTIN_POLICIES)
        edges = []
        targets = set()
        for profile_id, profile in profiles.items():
            if not isinstance(profile, dict):
                raise ProfileValidationError('旧配置内容必须是对象')
            try:
                validate_shapes(profile, require_ids=False)
            except DialerReferenceError as error:
                raise ProfileValidationError(str(error)) from error
            nodes = profile.get('nodes', [])
            groups = profile.get('proxy_groups', [])
            subscriptions = _items_by_id(profile.get('subscriptions', []), '旧配置订阅')
            subscription_identities = {
                key: json.dumps(value, sort_keys=True, ensure_ascii=False)
                for key, value in subscriptions.items()
            }
            for node in nodes:
                name = node['name']
                subscription_id = node.get('subscription_id')
                if subscription_id and (not isinstance(subscription_id, str) or subscription_id not in subscriptions):
                    raise ProfileValidationError(f'旧配置 {profile_id} 的节点引用的订阅不存在')
                # Remapping different subscription definitions also splits
                # otherwise identical nodes. Match that identity before aliases
                # are assigned, so downstream raw dialers stay profile-local.
                identity = (json.dumps(node, sort_keys=True, ensure_ascii=False),
                            subscription_identities.get(subscription_id) if subscription_id else None)
                definitions.setdefault(name, set()).add(identity)
                used_names.add(name)
            group_names.update(group['name'] for group in groups)
            used_names.update(group['name'] for group in groups)
            for node in nodes:
                if not node.get('enabled', True):
                    continue
                target = raw_dialer(node)
                if target is None:
                    continue
                if not isinstance(target, str) or not target.strip():
                    raise ProfileValidationError('原始 dialer-proxy 必须是非空名称')
                if target in BUILTIN_POLICIES:
                    continue
                matches = [('node', item) for item in nodes if item['name'] == target and item.get('enabled', True)]
                matches += [('group', item) for item in groups if item['name'] == target and item.get('enabled', True)]
                if len(matches) != 1:
                    raise ProfileValidationError(f'旧配置 {profile_id} 的原始拨号目标不存在或名称不唯一：{target}')
                targets.add(target)
                kind, destination = matches[0]
                edges.append((node, kind, destination, target))

        scoped = {name for name in targets if name in definitions
                  and (len(definitions[name]) > 1 or name in group_names)}
        # A shared-looking B -> A also becomes profile-specific when A differs.
        # Propagate this through multi-hop raw chains before assigning any names.
        while True:
            dependent = {node['name'] for node, kind, destination, target in edges
                         if kind == 'node' and target in scoped and node['name'] in targets}
            if dependent <= scoped:
                break
            scoped.update(dependent)
        for profile_id, profile in profiles.items():
            renamed = {}
            for node in profile.get('nodes', []):
                old_name = node['name']
                if old_name not in scoped:
                    continue
                candidate = f'{old_name} ({profile_id})'
                index = 2
                while candidate in used_names:
                    candidate = f'{old_name} ({profile_id}-{index})'
                    index += 1
                used_names.add(candidate)
                node['name'] = candidate
                if node.get('enabled', True):
                    renamed[old_name] = candidate
            local_groups = {group['name'] for group in profile.get('proxy_groups', [])}
            for rule in profile.get('rule_configs', []):
                policy = rule.get('policy')
                if policy in renamed and policy not in local_groups:
                    rule['policy'] = renamed[policy]
        for node, kind, destination, old_target in edges:
            if kind != 'node' or destination['name'] == old_target:
                continue
            if node.get('proxy_string'):
                parsed = _parse_structured_proxy_string(node['proxy_string'])
                parsed['dialer-proxy'] = destination['name']
                node['proxy_string'] = json.dumps(parsed, ensure_ascii=False)
            else:
                node['params']['dialer-proxy'] = destination['name']
        return profiles

    def _convert_import(self, source, *, resource_remaps=None):
        if not isinstance(source, dict):
            raise ProfileValidationError('导入内容必须是 JSON 对象')
        version = source.get('schema_version')
        if version == self.SCHEMA_VERSION:
            document = copy.deepcopy(source)
            self._validate_document(document)
            return document
        if version == 2:
            old_system = source.get('system')
            old_profiles = source.get('profiles')
            if not isinstance(old_system, dict) or not isinstance(old_profiles, dict):
                raise ProfileValidationError('旧多配置备份必须包含 system 索引和全部 profiles 配置内容')
            self._validate_legacy_index(old_system)
            metadata = _items_by_id(old_system['profiles'], '旧配置索引')
            if set(metadata) != set(old_profiles):
                raise ProfileValidationError('旧多配置备份的索引与配置内容不一致')
            old_profiles = self._scope_legacy_dialers(old_profiles)
        elif version is None and not set(source) & {'profiles', 'shared', 'resource_refs', 'node_dialers'}:
            old_system = source
            old_profiles = {'default': source}
            metadata = {'default': {}}
        else:
            raise ProfileValidationError('仅支持原版单配置、schema 2 完整多配置备份或当前格式；未修改原数据')
        document = {
            'schema_version': self.SCHEMA_VERSION,
            'system': {key: copy.deepcopy(old_system.get(key, [] if key == 'agents' else {}))
                       for key in ('system_config', 'backup', 'agents')},
            'shared': {key: [] for key in SHARED_FIELDS},
            'profiles': {},
        }
        document['system']['_revision'] = old_system.get('_revision', 0)
        document['shared']['_revision'] = 0
        settings = document['system']['system_config']
        if not isinstance(settings, dict):
            raise ProfileValidationError('系统设置必须是对象')
        settings.setdefault('server_domain', '')
        settings.setdefault('github_proxy_domain', '')
        settings.setdefault('rule_fetch_proxy', '')
        if not isinstance(settings['github_proxy_domain'], str):
            settings['github_proxy_domain'] = ''

        from backend.utils.rule_utils import sanitize_rule_name
        catalogs = {kind: {} for kind in SHARED_FIELDS}
        identities = {kind: {} for kind in SHARED_FIELDS}
        names = {kind: set() for kind in SHARED_FIELDS}
        names['subscription_aggregations'] = names['subscriptions']

        def merge_resource(kind, resource, profile_id):
            old_id = resource['id']
            identity = (old_id, json.dumps(resource, sort_keys=True, ensure_ascii=False))
            if identity in identities[kind]:
                return identities[kind][identity]
            if old_id in catalogs[kind]:
                resource['id'] = f'{old_id[:40]}_{uuid.uuid4().hex[:12]}'
                while resource['id'] in catalogs[kind]:
                    resource['id'] = f'{old_id[:40]}_{uuid.uuid4().hex[:12]}'
            # Providers and rule libraries are emitted as name-keyed maps.
            # Nodes keep their names: raw dialer-proxy values refer to them.
            name = resource.get('name')
            name_key = sanitize_rule_name(name) if kind == 'rule_library' else name
            if kind != 'nodes' and isinstance(name, str) and name and name_key in names[kind]:
                suffix = profile_id
                index = 1
                while name_key in names[kind]:
                    resource['name'] = f'{name[:max(1, 196 - len(suffix))]} ({suffix})'
                    name_key = sanitize_rule_name(resource['name']) if kind == 'rule_library' else resource['name']
                    index += 1
                    suffix = f'{profile_id}-{index}'
            catalogs[kind][resource['id']] = resource
            identities[kind][identity] = resource['id']
            names[kind].add(name_key)
            document['shared'][kind].append(resource)
            return resource['id']

        for profile_id, raw_profile in old_profiles.items():
            self.validate_profile_id(profile_id)
            if not isinstance(raw_profile, dict):
                raise ProfileValidationError('旧配置内容必须是对象')
            if set(raw_profile) & {'resource_refs', 'node_dialers', 'schema_version', 'shared', 'profiles'}:
                raise ProfileValidationError('旧配置包含不支持的中间格式字段；未修改原数据')
            old = _deep_merge(self._legacy_defaults(), raw_profile)
            old.update(copy.deepcopy(metadata[profile_id]))
            remaps = {kind: {} for kind in SHARED_FIELDS}
            remaps['rule_cache'] = {}

            def remap_reference(kind, value):
                if kind == 'nodes' and value in BUILTIN_POLICIES:
                    return value
                if value not in remaps[kind]:
                    raise ProfileValidationError(f'旧配置 {profile_id} 引用的本地资源不存在：{kind}/{value}')
                return remaps[kind][value]

            for kind in SHARED_FIELDS:
                resources = old[kind]
                if not isinstance(resources, list) or any(not isinstance(item, dict) for item in resources):
                    raise ProfileValidationError(f'{kind} 必须是资源对象数组')
                for resource in resources:
                    resource.setdefault('id', f'{kind}_{uuid.uuid4().hex[:12]}')
                _items_by_id(resources, kind)
                for raw in resources:
                    resource = copy.deepcopy(raw)
                    old_id = resource['id']
                    if kind == 'nodes' and resource.get('subscription_id'):
                        value = resource['subscription_id']
                        resource['subscription_id'] = remap_reference('subscriptions', value)
                    if kind == 'subscription_aggregations':
                        for field in ('subscriptions', 'nodes'):
                            if field in resource:
                                resource[field] = [remap_reference(field, value)
                                                   for value in _id_list(resource[field], field)]
                    remaps[kind][old_id] = merge_resource(kind, resource, profile_id)
                    if kind == 'rule_library':
                        remaps['rule_cache'][remaps[kind][old_id]] = raw.get('name', '')
            if resource_remaps is not None:
                resource_remaps[profile_id] = remaps
            profile = self._empty_profile(profile_id, old.get('name', '默认配置' if profile_id == 'default' else profile_id), old.get('description', ''))
            for key in (*PROFILE_FIELDS, 'created_at', 'updated_at', '_revision'):
                if key in old:
                    profile[key] = copy.deepcopy(old[key])
            for group in profile['proxy_groups']:
                for field, kind in (('subscriptions', 'subscriptions'), ('manual_nodes', 'nodes'), ('aggregations', 'subscription_aggregations')):
                    if field in group:
                        group[field] = [remap_reference(kind, value) for value in _id_list(group[field], field)]
                legacy_kind = {'node': 'nodes', 'subscription': 'subscriptions', 'aggregation': 'subscription_aggregations'}.get(group.get('source'))
                if legacy_kind and 'proxies' in group:
                    group['proxies'] = [remap_reference(legacy_kind, value) for value in _id_list(group['proxies'], 'proxies')]
            # proxies_order is only an ordering hint. Old versions left entries
            # behind when the referenced group/resource was deleted, and the
            # converters skipped them; drop them instead of failing the upgrade.
            group_ids = {group.get('id') for group in profile['proxy_groups']}
            for group in profile['proxy_groups']:
                if not isinstance(group.get('proxies_order'), list):
                    continue
                order = []
                for item in group['proxies_order']:
                    if not isinstance(item, dict):
                        continue
                    value = item.get('id')
                    if item.get('type') == 'strategy':
                        if value not in group_ids or value == group.get('id'):
                            continue
                    else:
                        kind = {'node': 'nodes', 'subscription': 'subscriptions', 'aggregation': 'subscription_aggregations'}.get(item.get('type'))
                        if not kind:
                            continue
                        if not (kind == 'nodes' and value in BUILTIN_POLICIES):
                            if value not in remaps[kind]:
                                continue
                            item['id'] = remaps[kind][value]
                    order.append(item)
                group['proxies_order'] = order
            if not profile['rule_configs']:
                profile['rule_configs'] = [{**item, 'itemType': 'rule'} for item in old.get('rules', [])] + [
                    {**item, 'itemType': 'ruleset'} for item in old.get('rule_sets', [])]
            rule_names = {item['name']: catalogs['rule_library'][remaps['rule_library'][item['id']]]['name']
                          for item in old['rule_library']}
            for rule in profile['rule_configs']:
                rule.setdefault('id', f'rule_{uuid.uuid4().hex[:12]}')
                if rule.get('itemType') != 'ruleset':
                    continue
                old_library_id = rule.get('library_rule_id')
                library_id = remap_reference('rule_library', old_library_id) if old_library_id else None
                if library_id is None:
                    resource = {key: copy.deepcopy(rule[key]) for key in RULE_SOURCE_FIELDS if key in rule}
                    resource.update(id=f'migrated_rule_{rule["id"]}', enabled=True)
                    resource.setdefault('name', rule.get('name') or resource['id'])
                    resource.setdefault('source_type', 'content' if resource.get('content') else 'url')
                    library_id = merge_resource('rule_library', resource, profile_id)
                else:
                    # Old rules could customize their emitted name/behavior even
                    # when the content came from a library. Preserve that variant.
                    original = next(item for item in old['rule_library'] if item['id'] == old_library_id)
                    overrides = {key: rule[key] for key in ('name', 'behavior', 'format')
                                 if key in rule and rule[key] != original.get(key)}
                    if overrides:
                        resource = {**copy.deepcopy(original), **overrides, 'id': f'migrated_rule_{rule["id"]}'}
                        library_id = merge_resource('rule_library', resource, profile_id)
                        remaps['rule_cache'][library_id] = original.get('name', '')
                if rule.get('name'):
                    rule_names[rule['name']] = catalogs['rule_library'][library_id]['name']
                rule['library_rule_id'] = library_id
                for key in RULE_SOURCE_FIELDS:
                    rule.pop(key, None)
            for rule in profile['rule_configs']:
                if rule.get('itemType') == 'rule' and rule.get('rule_type') == 'RULE-SET':
                    rule['value'] = rule_names.get(rule.get('value'), rule.get('value'))
            # Same as proxies_order: generators skip selections whose rule or
            # group no longer exists, so drop them instead of failing the upgrade.
            item_types = {rule.get('id'): rule.get('itemType') for rule in profile['rule_configs']}
            for engine in ('mosdns', 'oxidns'):
                dns_settings = profile.get(engine)
                if isinstance(dns_settings, dict):
                    for key in ('direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules'):
                        if isinstance(dns_settings.get(key), list):
                            expected = 'ruleset' if key.endswith('rulesets') else 'rule'
                            dns_settings[key] = [value for value in dns_settings[key]
                                                 if isinstance(value, str) and item_types.get(value) == expected]
            surge = profile.get('surge')
            if isinstance(surge, dict) and isinstance(surge.get('smart_groups'), list):
                surge['smart_groups'] = [setting for setting in surge['smart_groups']
                                         if isinstance(setting, dict) and setting.get('group_id') in group_ids]
            document['profiles'][profile_id] = profile
        for agent in document['system']['agents']:
            if version == 2:
                agent.setdefault('profile_id', 'default')
            else:
                agent['profile_id'] = 'default'
        return document

    @staticmethod
    def _resource_usage(document, kind, resource_id):
        usages = []
        if kind in ('subscriptions', 'nodes'):
            for aggregation in document['shared']['subscription_aggregations']:
                if resource_id in aggregation.get(kind, []):
                    usages.append({'kind': 'aggregation', 'id': aggregation['id'], 'name': aggregation.get('name', aggregation['id'])})
        if kind == 'subscriptions':
            for node in document['shared']['nodes']:
                if node.get('subscription_id') == resource_id:
                    usages.append({'kind': 'node', 'id': node['id'], 'name': node.get('name', node['id'])})
        catalogs = {field: _items_by_id(document['shared'][field], field) for field in RESOURCE_FIELDS}
        for profile in document['profiles'].values():
            referenced = resource_id in _collect_resource_refs(profile, catalogs).get(kind, set())
            if kind == 'rule_library':
                referenced = any(rule.get('library_rule_id') == resource_id for rule in profile['rule_configs'])
            if referenced:
                usages.append({'kind': 'profile', 'id': profile['id'], 'name': profile['name'],
                               'profile_id': profile['id'], 'profile_name': profile['name']})
        return usages

    def _validate_document(self, document):
        if document.get('schema_version') != self.SCHEMA_VERSION:
            raise ProfileValidationError('配置存储版本不匹配')
        for key in ('system', 'shared', 'profiles'):
            if not isinstance(document.get(key), dict):
                raise ProfileValidationError(f'缺少配置节：{key}')
        system = document['system']
        if not isinstance(system.get('system_config'), dict):
            raise ProfileValidationError('系统设置和备份设置必须是对象')
        default_id = system['system_config'].get('default_profile_id')
        if default_id is not None and not isinstance(default_id, str):
            raise ProfileValidationError('默认配置空间 ID 必须是字符串')
        if self._default_profile_id(document) not in document['profiles']:
            raise ProfileValidationError('必须保留默认配置')
        if set(system) - self.SYSTEM_FIELDS:
            raise ProfileValidationError('system 节只接受全局设置、备份与 Agent')
        if not isinstance(system.get('system_config'), dict) or not isinstance(system.get('backup'), dict):
            raise ProfileValidationError('系统设置和备份设置必须是对象')
        _items_by_id(system.get('agents'), 'Agent')
        shared = document['shared']
        catalogs = {key: _items_by_id(shared.get(key), key) for key in SHARED_FIELDS}
        from backend.utils.health_check import validate_health
        from backend.utils.subscription_refresh import validate_refresh_interval
        try:
            for source in catalogs['subscriptions'].values():
                if 'interval' in source:
                    validate_refresh_interval(source['interval'])
            for kind in ('subscriptions', 'subscription_aggregations'):
                for source in catalogs[kind].values():
                    if 'health_check' in source:
                        validate_health(source['health_check'], provider=True)
        except ValueError as error:
            raise ProfileValidationError(str(error)) from error
        from backend.utils.dialer_references import validate_shapes, DialerReferenceError
        try:
            validate_shapes({'nodes': shared['nodes']})
        except DialerReferenceError as error:
            raise ProfileValidationError(str(error)) from error
        for node in catalogs['nodes'].values():
            if 'dialer_ref' in node:
                raise ProfileValidationError('共享节点不能保存拨号引用，请在策略组中创建代理链')
            if node.get('subscription_id') and node['subscription_id'] not in catalogs['subscriptions']:
                raise ProfileValidationError('节点引用的订阅不存在')
        for resource in catalogs['rule_library'].values():
            if 'policy' in resource or 'target' in resource:
                raise ProfileValidationError('规则仓库不包含目标策略，请在规则配置中设置')
        library_names = [resource.get('name') for resource in catalogs['rule_library'].values()]
        if any(not isinstance(name, str) or not name for name in library_names) or len(set(library_names)) != len(library_names):
            raise ProfileValidationError('共享规则仓库名称必须非空且唯一')
        for aggregation in catalogs['subscription_aggregations'].values():
            for field in ('subscriptions', 'nodes'):
                for value in _id_list(aggregation.get(field, []), f'聚合 {aggregation.get("name")} 的 {field}'):
                    if value not in catalogs[field] and not (field == 'nodes' and value in BUILTIN_POLICIES):
                        raise ProfileValidationError(f'聚合 {aggregation.get("name")} 引用了不存在的资源：{value}')
        for profile_id, profile in document['profiles'].items():
            self.validate_profile_id(profile_id)
            if not isinstance(profile, dict) or profile.get('id') != profile_id:
                raise ProfileValidationError('配置内容与 ID 不一致')
            try:
                self._validate_profile(profile, catalogs)
                resolved = self._resolve_profile(document, profile_id)
                from backend.utils.dialer_references import validate_dialers, DialerReferenceError
                try:
                    validate_dialers(resolved)
                except DialerReferenceError as error:
                    raise ProfileInUse(str(error)) from error
            except ProfileInUse as error:
                usages = error.usages or [{'kind': 'profile', 'id': profile_id, 'name': profile['name'],
                                           'profile_id': profile_id, 'profile_name': profile['name']}]
                raise ProfileInUse(f'配置 {profile["name"]}: {error}', usages) from error
        for agent in _items_by_id(system.get('agents'), 'Agent').values():
            if agent.get('profile_id') not in document['profiles']:
                raise ProfileValidationError(f'Agent {agent.get("name", agent["id"])} 绑定的配置不存在')
        for section in (system, shared, *document['profiles'].values()):
            if not isinstance(section.get('_revision'), int) or section['_revision'] < 0:
                raise ProfileValidationError('配置修订号无效')

    @staticmethod
    def _validate_profile(profile, catalogs):
        if any(key in profile for key in SHARED_FIELDS):
            raise ProfileValidationError('独立配置不能保存共享资源副本')
        if not isinstance(profile.get('name'), str) or not profile['name'].strip():
            raise ProfileValidationError('配置名称不能为空')
        if set(profile) & {'resource_refs', 'node_dialers'}:
            raise ProfileValidationError('独立配置不接受资源白名单或节点拨号覆盖字段')
        from backend.utils.dialer_references import validate_shapes, DialerReferenceError
        try:
            validate_shapes(profile)
        except DialerReferenceError as error:
            raise ProfileValidationError(str(error)) from error
        _collect_resource_refs(profile, catalogs)
        groups = _items_by_id(profile.get('proxy_groups'), '策略组')
        names = [group.get('name') for group in groups.values()]
        if any(not isinstance(name, str) or not name for name in names):
            raise ProfileValidationError('策略组名称必须非空')
        group_names = set(names)
        if len(group_names) != len(groups) or group_names & BUILTIN_POLICIES:
            raise ProfileValidationError('策略组名称必须唯一，且不能使用 DIRECT 或 REJECT')
        for group in groups.values():
            from backend.utils.health_check import validate_health
            try:
                validate_health(group)
            except ValueError as error:
                raise ProfileValidationError(str(error)) from error
            label = f'策略组“{group.get("name")}”'
            for field, kind in (('subscriptions', 'subscriptions'), ('manual_nodes', 'nodes'), ('aggregations', 'subscription_aggregations')):
                for value in _id_list(group.get(field, []), label + field):
                    if value not in catalogs[kind] and not (kind == 'nodes' and value in BUILTIN_POLICIES):
                        raise ProfileInUse(f'{label} 引用了不存在的资源：{value}')
            legacy_kind = {'node': 'nodes', 'subscription': 'subscriptions', 'aggregation': 'subscription_aggregations'}.get(group.get('source'))
            if legacy_kind:
                for value in _id_list(group.get('proxies', []), label + 'proxies'):
                    if value not in catalogs[legacy_kind] and not (legacy_kind == 'nodes' and value in BUILTIN_POLICIES):
                        raise ProfileInUse(f'{label} 引用了不存在的资源：{value}')
            targets = list(group.get('include_groups', []))
            if group.get('follow_group'):
                targets.append(group['follow_group'])
            for target in targets:
                if target not in groups or target == group['id']:
                    raise ProfileInUse(f'{label} 引用了无效的策略组：{target}')
            if group.get('follow_group') and groups[group['follow_group']].get('type') == 'chain':
                raise ProfileValidationError('代理链请通过引用策略使用，不能作为跟随目标')
            for item in group.get('proxies_order', []):
                kind = item.get('type')
                value = item.get('id')
                available = {'node': set(catalogs['nodes']) | BUILTIN_POLICIES,
                             'strategy': set(groups), 'aggregation': set(catalogs['subscription_aggregations']),
                             'subscription': set(catalogs['subscriptions'])}.get(kind)
                if available is None or value not in available or (kind == 'strategy' and value == group['id']):
                    raise ProfileInUse(f'{label} 的排序列表包含无效引用：{value}')
        rules = _items_by_id(profile.get('rule_configs'), '规则配置')
        for rule in rules.values():
            if rule.get('itemType') not in ('rule', 'ruleset'):
                raise ProfileValidationError('规则类型必须为 rule 或 ruleset')
            if rule['itemType'] == 'ruleset' and rule.get('library_rule_id') not in catalogs['rule_library']:
                raise ProfileValidationError('规则集必须引用全局规则仓库，不能单独设置来源')
        for engine in ('mihomo', 'surge', 'mosdns'):
            if not isinstance(profile.get(engine), dict):
                raise ProfileValidationError(f'{engine} 参数必须是对象')
        # 旧版本保存的配置没有 loon 字段，缺省视为空对象
        if not isinstance(profile.get('loon', {}), dict):
            raise ProfileValidationError('loon 参数必须是对象')
        # 域名发现设置同样是后加字段，缺省为空对象
        if not isinstance(profile.get('domain_discovery', {}), dict):
            raise ProfileValidationError('domain_discovery 参数必须是对象')
        from backend.common.device_notes import normalize_notes
        from backend.common.oxidns_settings import OxidnsSettingsError
        try:
            normalize_notes(profile.get('device_notes', {}))
            from backend.common.client_fixed_ip import validate_bindings
            validate_bindings(profile.get('client_fixed_ips', {}))
        except OxidnsSettingsError as exc:
            raise ProfileValidationError(str(exc)) from exc
        # 上游路由器同样是后加字段，缺省为空对象
        routers = profile.get('routers', {})
        if not isinstance(routers, dict) or not isinstance(routers.get('items', []), list):
            raise ProfileValidationError('routers 参数必须是包含 items 列表的对象')
        # OxiDNS is additive: pre-existing schema-5 profiles may omit it.
        if not isinstance(profile.get('oxidns', {}), dict):
            raise ProfileValidationError('oxidns 参数必须是对象')
        for engine, label in (('mosdns', 'MosDNS'), ('oxidns', 'OxiDNS')):
            dns_settings = profile.get(engine, {})
            for key in ('direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules'):
                expected = 'ruleset' if key.endswith('rulesets') else 'rule'
                for value in _id_list(dns_settings.get(key, []), key):
                    if value not in rules or rules[value]['itemType'] != expected:
                        raise ProfileInUse(f'{label} {key} 仍引用不存在的规则 {value}，请先取消选择')
        oxidns = profile.get('oxidns', {})
        from backend.common.rule_dns import normalize_action
        for key in RULE_DNS_FIELDS:
            value = oxidns.get(key, [])
            if not isinstance(value, (dict, list)) or any(not isinstance(identity, str) for identity in value):
                raise ProfileValidationError(f'OxiDNS {key} 必须以规则 ID 为键')
            for identity in value:
                if identity not in rules:
                    raise ProfileInUse(f'OxiDNS DNS 处理仍引用不存在的规则 {identity}，请先取消')
            if key == 'rule_dns_follow' or not isinstance(value, dict):
                continue
            # Imports skip the OxiDNS settings normalizer, but the rules page and
            # generation read these actions directly.
            for identity, action in value.items():
                try:
                    if action is not None:
                        normalize_action(action, parked=key == 'rule_dns_parked')
                except OxidnsSettingsError as exc:
                    raise ProfileValidationError(f'OxiDNS {key} 中规则 {identity} 的 DNS 处理无效：{exc}') from exc
        for setting in profile['surge'].get('smart_groups', []):
            if setting.get('group_id') not in groups:
                raise ProfileInUse('Surge Smart 设置引用的策略组不存在')
