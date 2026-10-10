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
    """A resource/profile mutation would invalidate an existing dependency."""

    def __init__(self, message, usages=None):
        super().__init__(message)
        self.usages = usages or []


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
        "loon": {"custom_config": ""},
    }


SHARED_FIELDS = ('subscriptions', 'nodes', 'subscription_aggregations', 'rule_library')
RESOURCE_FIELDS = SHARED_FIELDS[:3]
PROFILE_FIELDS = ('proxy_groups', 'rule_configs', 'mihomo', 'surge', 'mosdns', 'loon', 'domain_discovery')
BUILTIN_POLICIES = {'DIRECT', 'REJECT'}
RULE_SOURCE_FIELDS = ('name', 'url', 'behavior', 'content', 'source_type', 'format')

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

    def __init__(self, data_dir, default_config_factory=None, initial_config_factory=None):
        self.data_dir = Path(data_dir).expanduser().resolve()
        self.profiles_dir = self.data_dir / 'profiles'
        self.path = self.data_dir / 'config.json'
        self.migrations_dir = self.data_dir / 'migrations'
        self.initialization_lock_file = self.data_dir / '.config.lock'
        self._default_config_factory = default_config_factory
        self._initial_config_factory = initial_config_factory
        self._local = threading.local()
        self.data_dir.mkdir(parents=True, exist_ok=True)
        self.profiles_dir.mkdir(parents=True, exist_ok=True)
        with self._lock(self.initialization_lock_file):
            self._initialize_locked()

    def _legacy_defaults(self):
        return _deep_merge(_default_profile_config(), self._default_config_factory() if self._default_config_factory else {})

    def _empty_profile(self, profile_id, name, description=''):
        defaults = self._legacy_defaults()
        result = {key: copy.deepcopy(defaults.get(key, {} if key in ('mihomo', 'surge', 'mosdns', 'loon', 'domain_discovery') else []))
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

    def _read_recoverable(self, path, validator):
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
            corrupt = path.with_name(path.name + '.corrupt-' + uuid.uuid4().hex)
            if path.exists():
                shutil.copy2(path, corrupt)
            # Do not refresh the valid backup with the corrupt primary.
            content = json.dumps(recovered, ensure_ascii=False, indent=2) + '\n'
            temporary = self._write_temp(path, content)
            try:
                os.replace(temporary, path)
                self._fsync_dir(path.parent)
            finally:
                if temporary.exists():
                    temporary.unlink()
            return recovered


    def _initialize_locked(self):
        if self.path.exists() and not self.path.is_file():
            raise ProfileValidationError('config.json must be a regular file')
        system_file = self.data_dir / 'system.json'
        has_layout = system_file.exists() or system_file.with_suffix('.json.bak').exists()
        if has_layout:
            for path in (self.path, self.path.with_name('config.json.bak'), self.profiles_dir):
                self._check_migration_path(path)
        current = None
        if self.path.exists() or self.path.with_name('config.json.bak').exists():
            try:
                current = self._read_json(self.path)
            except ProfileRepositoryError:
                # A stale single-file config is not authoritative in a split
                # installation. Prefer a committed current-format backup, if any.
                if has_layout:
                    backup = self.path.with_name('config.json.bak')
                    if backup.is_file():
                        try:
                            candidate = self._read_json(backup)
                        except ProfileRepositoryError:
                            candidate = {}
                        if candidate.get('schema_version') is not None:
                            current = self._read_recoverable(
                                self.path, lambda value: self._validate_document(self._convert_import(value)))
                else:
                    current = self._read_recoverable(
                        self.path, lambda value: self._validate_document(self._convert_import(value)))
            if current is not None and current.get('schema_version') == self.SCHEMA_VERSION:
                current = self._read_recoverable(self.path, self._validate_document)
                if self._ensure_rule_proxy_token(current['system']):
                    self._write_json(self.path, current)
                return
        split_source = None
        if has_layout and (current is None or current.get('schema_version') is None):
            split_source = self._read_profile_layout()
            source = split_source
        else:
            if current is None or current.get('schema_version') is None:
                if any(self.profiles_dir.glob('*/config.json')) or any(self.profiles_dir.glob('*/config.json.bak')):
                    raise ProfileValidationError('发现旧配置文件但缺少 system.json 索引；请恢复完整数据目录，不能初始化空配置')
            source = current if current is not None else (self._initial_config_factory or self._legacy_defaults)()
        remaps = {}
        document = self._convert_import(source, resource_remaps=remaps)
        self._ensure_rule_proxy_token(document['system'])
        self._validate_document(document)
        if current is not None or split_source is not None:
            self._snapshot('migration', split_source['profiles'] if split_source else ())
        if split_source is not None:
            self._migrate_profile_cache(split_source, document, remaps)
        else:
            self._migrate_raw_cache()
        with self._commit_guard():
            self._write_json(self.path, document)

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
        self._check_migration_path(self.migrations_dir)
        destination = self._create_migration_snapshot_dir() / reason
        destination.mkdir()
        for name in ('config.json', 'config.json.bak', 'system.json', 'system.json.bak'):
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

    @contextmanager
    def _commit_guard(self):
        previous = self.path.read_bytes() if self.path.exists() else None
        try:
            yield
        except Exception:
            if previous is None:
                if self.path.exists():
                    self.path.unlink()
            elif not self.path.exists() or self.path.read_bytes() != previous:
                temporary = self._write_temp(self.path, previous)
                try:
                    os.replace(temporary, self.path)
                    self._fsync_dir(self.path.parent)
                finally:
                    if temporary.exists():
                        temporary.unlink()
            raise

    @contextmanager
    def _document(self, write=False):
        nested = getattr(self._local, 'document', None)
        if nested is not None:
            if write:
                raise ProfileInUse('不能在配置事务中再次开启写事务')
            yield nested
            return
        with self._lock(self.initialization_lock_file):
            document = self._read_recoverable(self.path, self._validate_document)
            self._local.document = document
            try:
                yield document
                if write:
                    self._ensure_rule_proxy_token(document['system'])
                    self._validate_document(document)
                    with self._commit_guard():
                        self._write_json(self.path, document)
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

    def list_profiles(self):
        with self._document() as document:
            shared_revision = int(document['shared'].get('_revision', 0))
            # revision 即生成配置的版本号（见 config_revision），界面据此判断 Agent 是否待更新
            return [dict(self._metadata(profile), revision=int(profile.get('_revision', 0)) + shared_revision)
                    for profile in document['profiles'].values()]

    def _profile_metadata(self, profile_id):
        return self._metadata(self.get_profile(profile_id))

    def get_profile(self, profile_id):
        with self._document() as document:
            return copy.deepcopy(self._profile(document, profile_id))

    def config_revision(self, profile_id):
        """生成配置的版本号：配置空间或共享资源任一变化都会让它增大，用于判断 Agent 是否待更新。"""
        with self._document() as document:
            profile = self._profile(document, profile_id)
            return int(profile.get('_revision', 0)) + int(document['shared'].get('_revision', 0))

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

    def delete_profile(self, profile_id):
        if profile_id == self.DEFAULT_PROFILE_ID:
            raise ProfileInUse('The default profile cannot be deleted')
        with self._lock(self._profile_operation_lock_path(profile_id)):
            tombstone = None
            directory = self.profile_dir(profile_id)
            try:
                with self._document(write=True) as document:
                    profile = self._profile(document, profile_id)
                    usages = [{'kind': 'agent', 'id': a['id'], 'name': a.get('name', a['id']), 'profile_id': profile_id, 'profile_name': profile['name']}
                              for a in document['system']['agents'] if a.get('profile_id') == profile_id]
                    if usages:
                        raise ProfileInUse(f'配置 {profile["name"]} 仍被 Agent 使用', usages)
                    if directory.exists():
                        self.migrations_dir.mkdir(exist_ok=True)
                        tombstone = self.migrations_dir / f'deleted-{profile_id}-{uuid.uuid4().hex}'
                        os.replace(directory, tombstone)
                    del document['profiles'][profile_id]
            except Exception:
                if tombstone is not None and tombstone.exists():
                    os.replace(tombstone, directory)
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

    def update_profile_transaction(self, profile_id, updater):
        with self._document(write=True) as document:
            current = self._profile(document, profile_id)
            previous_group_names = {group.get('name') for group in current['proxy_groups']}
            # Updaters may inspect hydrated resources but cannot persist copies.
            snapshot = self._resolve_profile(document, profile_id)
            updated = self._apply_updater(snapshot, updater)
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
        return copy.deepcopy(current)

    def update_profile_fields(self, profile_id, fields, baseline=None):
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
        return self.update_profile_transaction(profile_id, update)

    def save_profile(self, profile_id, data, baseline=None):
        return self.update_profile_fields(profile_id, data, baseline)


    def get_shared(self):
        with self._document() as document:
            return copy.deepcopy(document['shared'])

    def update_shared_transaction(self, updater):
        with self._document(write=True) as document:
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
        return copy.deepcopy(proposed)

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

    def update_system_transaction(self, updater):
        with self._document(write=True) as document:
            current = document['system']
            updated = self._apply_updater(copy.deepcopy(current), updater)
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
            return self._resource_usage(document, resource_type, resource_id)

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
        for rule in result['rule_configs']:
            if rule.get('itemType') == 'ruleset':
                resource = library[rule['library_rule_id']]
                for key in RULE_SOURCE_FIELDS:
                    if key in resource:
                        rule[key] = copy.deepcopy(resource[key])
                rule.setdefault('behavior', 'domain')
                if resource.get('source_type') == 'content':
                    rule['url'] = f'/api/profiles/{profile_id}/rule-library/content/{resource["id"]}'
                rule['library_enabled'] = resource.get('enabled', True)
        return result

    def get_compat_config(self, profile_id, *, resource_roots=None):
        with self._document() as document:
            return self._resolve_profile(document, profile_id, resource_roots)

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
        return self.save_profile(profile_id, data)

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
                for engine in ('mihomo', 'surge', 'mosdns'):
                    profile[engine]['custom_config'] = ''
                if isinstance(profile.get('loon'), dict):
                    profile['loon']['custom_config'] = ''
            result = sanitize_external_payload(result, system_config)
        return result

    def import_all(self, data):
        document = self._convert_import(data)
        self._ensure_rule_proxy_token(document['system'])
        self._validate_document(document)
        with self._document(write=True) as previous:
            self._snapshot('import')
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
            mosdns = profile.get('mosdns')
            if isinstance(mosdns, dict):
                for key in ('direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules'):
                    if isinstance(mosdns.get(key), list):
                        expected = 'ruleset' if key.endswith('rulesets') else 'rule'
                        mosdns[key] = [value for value in mosdns[key]
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
        if 'default' not in document['profiles']:
            raise ProfileValidationError('必须保留默认配置')
        system = document['system']
        if set(system) - self.SYSTEM_FIELDS:
            raise ProfileValidationError('system 节只接受全局设置、备份与 Agent')
        if not isinstance(system.get('system_config'), dict) or not isinstance(system.get('backup'), dict):
            raise ProfileValidationError('系统设置和备份设置必须是对象')
        _items_by_id(system.get('agents'), 'Agent')
        shared = document['shared']
        catalogs = {key: _items_by_id(shared.get(key), key) for key in SHARED_FIELDS}
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
        mosdns = profile['mosdns']
        for key in ('direct_rulesets', 'proxy_rulesets', 'direct_rules', 'proxy_rules'):
            expected = 'ruleset' if key.endswith('rulesets') else 'rule'
            for value in _id_list(mosdns.get(key, []), key):
                if value not in rules or rules[value]['itemType'] != expected:
                    raise ProfileInUse(f'MosDNS {key} 仍引用不存在的规则 {value}，请先取消选择')
        for setting in profile['surge'].get('smart_groups', []):
            if setting.get('group_id') not in groups:
                raise ProfileInUse('Surge Smart 设置引用的策略组不存在')
