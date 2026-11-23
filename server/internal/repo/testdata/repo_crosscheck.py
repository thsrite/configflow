#!/usr/bin/env python3
"""Python side of the repository cross-check (see crosscheck_test.go).

Runs the real backend.common.config_repository.ProfileRepository against data
directories shared with the Go repository. CONFIGFLOW_REPO must point at the
repository root.

    repo_crosscheck.py init DIR OUT MODE         open DIR (MODE factories|plain); OUT = state of the directory
    repo_crosscheck.py run DIR OUT MODE OPS      open DIR and run the operations of the JSON file OPS;
                                                 OUT = one record per operation (result or error, and a dump)
    repo_crosscheck.py fingerprint DIR OUT MODE  OUT = store fingerprint before opening, after opening and
                                                 after export_all, and the export
    repo_crosscheck.py hold DIR OUT MODE SECONDS hold .config.lock (no repository opened) for SECONDS;
                                                 prints "locked" once it is held
    repo_crosscheck.py try-write DIR OUT MODE    open DIR and add an Agent in a system transaction;
                                                 OUT = {"ok": true} or the error class and message
    repo_crosscheck.py crash-write DIR OUT MODE  the same write, but the process dies (exit 9) right
                                                 before COMMIT, with every statement executed
"""

import copy
import json
import os
import sqlite3
import sys
import time
from pathlib import Path

sys.path.insert(0, os.environ['CONFIGFLOW_REPO'])

from backend.common import config as config_module  # noqa: E402
from backend.common.config_repository import ProfileRepository  # noqa: E402
from backend.common.config_store import ConfigStore  # noqa: E402


def open_repository(directory, mode):
    if mode == 'factories':
        return ProfileRepository(directory, default_config_factory=config_module.get_default_config,
                                 initial_config_factory=config_module._get_initial_config)
    return ProfileRepository(directory)


def dump(directory):
    connection = sqlite3.connect(Path(directory) / 'configflow.sqlite3')
    result = {
        'meta': connection.execute('SELECT key, value FROM meta ORDER BY key').fetchall(),
        'sections': connection.execute('SELECT scope, owner, position FROM sections ORDER BY scope, owner').fetchall(),
        'fields': connection.execute('SELECT scope, owner, field, position, value, revision FROM fields '
                                     'ORDER BY scope, owner, field').fetchall(),
    }
    connection.close()
    return result


def layout(directory):
    root = Path(directory)
    entries = []
    for path in sorted(root.rglob('*')):
        relative = path.relative_to(root).as_posix()
        entries.append(relative + '/' if path.is_dir() else relative)
    return entries


def apply_patch(value, patch):
    for step in patch or []:
        target = value
        *parents, last = step['path']
        for key in parents:
            target = target[key]
        if 'set' in step:
            target[last] = copy.deepcopy(step['set'])
        elif 'append' in step:
            target[last].append(copy.deepcopy(step['append']))
        elif step.get('delete'):
            del target[last]
    return value


def resolve(value, variables):
    """{"$var": name, "patch": [...]} is a patched deep copy of a saved result."""
    if isinstance(value, dict) and '$var' in value:
        return apply_patch(copy.deepcopy(variables[value['$var']]), value.get('patch'))
    return value


def updater(patch):
    def update(current):
        apply_patch(current, patch)
    return update


def execute(repository, op, variables):
    name = op['op']
    arg = lambda key, default=None: resolve(op.get(key, default), variables)  # noqa: E731
    if name == 'create_profile':
        return repository.create_profile(arg('metadata'), clone_from=op.get('clone_from'))
    if name == 'clone_profile':
        return repository.clone_profile(op['source'], arg('metadata'))
    if name == 'update_profile':
        return repository.update_profile(op['id'], arg('updates'))
    if name == 'update_profile_fields':
        saved, revision = repository.update_profile_fields(op['id'], arg('fields'), arg('baseline'),
                                                           include_config_revision=True)
        return {'saved': saved, 'revision': revision}
    if name == 'update_profile_transaction':
        saved, revision = repository.update_profile_transaction(op['id'], updater(op['patch']),
                                                                include_config_revision=True)
        return {'saved': saved, 'revision': revision}
    if name == 'import_profile':
        return repository.import_profile(op['id'], arg('data'))
    if name == 'update_system_transaction':
        return repository.update_system_transaction(updater(op['patch']))
    if name == 'save_system':
        return repository.save_system(arg('snapshot'), arg('baseline'))
    if name == 'update_shared_transaction':
        return repository.update_shared_transaction(updater(op['patch']))
    if name == 'save_shared':
        return repository.save_shared(arg('snapshot'), arg('baseline'))
    if name == 'update_shared_and_profiles':
        def profile_update(profile_id, before, after):
            apply_patch(after, op['profile_patch'])
        shared, profiles = repository.update_shared_and_profiles_transaction(
            updater(op['shared_patch']), profile_update, op['ids'])
        return {'shared': shared, 'profiles': profiles}
    if name == 'create_rule_library_item':
        return repository.create_rule_library_item(arg('rule'), op['content'])
    if name == 'set_default_profile':
        return repository.set_default_profile(op['id'])
    if name == 'delete_profile':
        return repository.delete_profile(op['id'], op.get('new_default'))
    if name == 'export_all':
        return repository.export_all()
    if name == 'import_all':
        return repository.import_all(arg('data'))
    if name == 'reset_all':
        return repository.reset_all()
    if name == 'list_profiles':
        return repository.list_profiles()
    if name == 'get_profile':
        return repository.get_profile(op['id'])
    if name == 'get_shared':
        return repository.get_shared()
    if name == 'get_system':
        return repository.get_system()
    if name == 'get_compat_config':
        return repository.get_compat_config(op['id'], resource_roots=op.get('roots'))
    if name == 'get_publish_snapshot':
        config, revision = repository.get_publish_snapshot(op['id'], agents=op.get('agents', True))
        return {'config': config, 'revision': revision}
    if name == 'get_resource_usage':
        return repository.get_resource_usage(op['kind'], op['id'])
    if name == 'config_revision':
        return repository.config_revision(op['id'])
    if name == 'default_profile_id':
        return repository.default_profile_id()
    if name == 'get_profile_agent_bindings':
        return repository.get_profile_agent_bindings(op['id'])
    if name == 'reopen':
        return None
    result = execute_c04(repository, op, variables, arg)
    if result is not _UNKNOWN:
        return result
    raise SystemExit(f'unknown operation {name}')


_UNKNOWN = object()


def relative(repository, path):
    return os.path.relpath(path, repository.data_dir)


def plain_agents(agents):
    from backend.common.config_repository import AGENT_HISTORY_NOT_READ
    return [{key: '<not read>' if value is AGENT_HISTORY_NOT_READ else value for key, value in agent.items()}
            for agent in agents]


def execute_c04(repository, op, variables, arg):
    """Operations added by the C04 review (public API part B and config.py)."""
    name = op['op']
    if name == 'save_router_settings':
        return repository.save_router_settings(op['id'], arg('expected'), arg('replacement'))
    if name == 'save_router_publication':
        from backend.common.config_repository import _MISSING
        fields = arg('fields') or {}
        keywords = {key: fields.get(key, _MISSING) for key in
                    ('manifest', 'dns_guard', 'ipv6_fakeip', 'ipv6_routes', 'ipv6_pd_guard', 'last_release')}
        return repository.save_router_publication(op['id'], arg('expected'), **keywords)
    if name == 'import_client_profile':
        seen = {}

        def build(shared):
            seen['shared_revision'] = shared.get('_revision')
            return copy.deepcopy(arg('result'))
        result = repository.import_client_profile(arg('metadata'), build)
        return {'result': result, 'seen': seen}
    if name == 'update_profile_transaction_replace':
        saved, revision = repository.update_profile_transaction(
            op['id'], lambda current: copy.deepcopy(arg('replacement')), include_config_revision=True)
        return {'saved': saved, 'revision': revision}
    if name == 'get_agents':
        return plain_agents(repository.get_agents(history=op.get('history', True)))
    if name == 'get_system_settings':
        return repository.get_system_settings()
    if name == 'export_desensitized':
        # Marker redaction functions: the structure export_all builds around them is compared.
        import backend.common.config_export as config_export
        import backend.common.mihomo_easytier as mihomo_easytier
        import backend.routers.store as router_store
        saved = (config_export.sanitize_external_payload, config_export.redact_oxidns_settings,
                 router_store.redact_routers, mihomo_easytier.redact_settings)
        config_export.sanitize_external_payload = lambda payload, system_config: {
            'sanitized': payload, 'system_config': system_config}
        config_export.redact_oxidns_settings = lambda value: {'redacted_oxidns': value}
        router_store.redact_routers = lambda value: {'redacted_routers': value}
        mihomo_easytier.redact_settings = lambda value: {'redacted_easytier': value}
        try:
            return repository.export_all(desensitize=True)
        finally:
            (config_export.sanitize_external_payload, config_export.redact_oxidns_settings,
             router_store.redact_routers, mihomo_easytier.redact_settings) = saved
    if name == 'sanitization_tokens':
        # generated tokens (43 characters) differ between the runs: only counted
        def summary(tokens):
            return {'fixed': sorted(token for token in tokens if len(token) != 43),
                    'generated': sum(1 for token in tokens if len(token) == 43)}
        return {'rule_proxy': summary(repository.rule_proxy_tokens_for_sanitization()),
                'agents': summary(repository.agent_tokens_for_sanitization())}
    if name == 'profile_metadata':
        return repository._profile_metadata(op['id'])
    if name == 'write_generated':
        return relative(repository, repository.write_generated(op['id'], op['filename'], op['content']))
    if name == 'write_profile_json':
        return relative(repository, repository.write_profile_json(op['id'], op['path'], arg('data')))
    if name == 'write_profile_text':
        return relative(repository, repository.write_profile_text(op['id'], op['path'], op['content']))
    if name == 'read_profile_json':
        return repository.read_profile_json(op['id'], op['path'])
    if name == 'write_shared_json':
        return relative(repository, repository.write_shared_json(op['path'], arg('data')))
    if name == 'write_shared_text':
        return relative(repository, repository.write_shared_text(op['path'], op['content']))
    if name == 'read_shared_json':
        return repository.read_shared_json(op['path'])
    if name == 'raw_write':
        path = repository.data_dir / op['path']
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(bytes.fromhex(op['hex']))
        return None
    if name == 'read_file':
        path = repository.data_dir / op['path']
        return {'text': path.read_text(encoding='utf-8'), 'mode': oct(path.stat().st_mode & 0o777)}
    # config.py module functions; the main thread's ContextVars are the scope
    if name == 'cfg_get_config':
        config = config_module.get_config(op.get('id'))
        apply_patch(config, op.get('patch'))
        return copy.deepcopy(config)
    if name == 'cfg_save_config':
        return config_module.save_config(arg('config'), op.get('id'))
    if name == 'cfg_save_with_guard':
        return config_module.save_config_with_publish_guard(arg('config'), op['service'], op.get('id'))
    if name == 'cfg_update_config_transaction':
        return config_module.update_config_transaction(updater(op['patch']), op.get('id'))
    if name == 'cfg_safe_import_config':
        return config_module.safe_import_config(arg('data'), op.get('id'))
    if name == 'cfg_get_shared_config':
        return config_module.get_shared_config()
    if name == 'cfg_save_shared_config':
        return config_module.save_shared_config(arg('config'), arg('baseline'))
    if name == 'cfg_update_shared_config_transaction':
        return config_module.update_shared_config_transaction(updater(op['patch']))
    if name == 'cfg_update_shared_and_config':
        def profile_update(profile_id, before, after):
            apply_patch(after, op['profile_patch'])
        shared, profiles = config_module.update_shared_and_config_transaction(
            updater(op['shared_patch']), profile_update, op.get('ids'))
        return {'shared': shared, 'profiles': profiles}
    if name == 'cfg_get_system_config':
        return config_module.get_system_config()
    if name == 'cfg_save_system_config':
        return config_module.save_system_config(arg('config'), arg('baseline'))
    if name == 'cfg_reset':
        return config_module.reset_config_context()
    if name == 'cfg_resolve':
        from backend.common.profile_context import resolve_profile_id
        return resolve_profile_id(op.get('id'), op.get('fallback'))
    if name == 'cfg_get_resource_config':
        return config_module.get_resource_config(op['kind'], op.get('resource_id'), op.get('id'))
    return _UNKNOWN


def stored_state(directory):
    connection = sqlite3.connect(Path(directory) / 'configflow.sqlite3')
    meta = dict(connection.execute('SELECT key, value FROM meta').fetchall())
    sections = connection.execute('SELECT * FROM sections ORDER BY scope, owner').fetchall()
    fields = {(scope, owner, field): (value, revision) for scope, owner, field, value, revision in
              connection.execute('SELECT scope, owner, field, value, revision FROM fields')}
    connection.close()
    return meta, sections, fields


def without_last_release(text):
    routers = json.loads(text)
    for item in routers.get('items', []) if isinstance(routers, dict) else []:
        if isinstance(item, dict):
            item.pop('last_release', None)
    return routers


def align_state_only_publication(repository, directory, op, before):
    """Accepted Go change (O17, parity/behaviour-changes.md, router last_release
    in agent_state): a save_router_publication that changes no configuration
    member (only last_release, or nothing) advances neither the generation nor
    the profile _revision/updated_at; Python always does. When Python's commit
    changed exactly those counters and at most last_release in the profile's
    routers row, they are put back so both sides continue from the same
    configuration state. Any other change is left for the comparison."""
    after = stored_state(directory)
    (meta_before, sections_before, fields_before), (meta_after, sections_after, fields_after) = before, after
    if sections_before != sections_after or set(fields_before) != set(fields_after):
        return
    if {key for key in meta_before.keys() | meta_after.keys()
            if meta_before.get(key) != meta_after.get(key)} != {'generation', 'commit_id'}:
        return
    profile = op['id']
    counters = {('profile', profile, '_revision'), ('profile', profile, 'updated_at')}
    routers = ('profile', profile, 'routers')
    for key in fields_after:
        if key in counters or fields_before[key][0] == fields_after[key][0]:
            continue
        if key != routers or without_last_release(fields_before[key][0]) != without_last_release(fields_after[key][0]):
            return
    connection = sqlite3.connect(Path(directory) / 'configflow.sqlite3')
    with connection:
        for key in ('generation', 'commit_id'):
            connection.execute('UPDATE meta SET value=? WHERE key=?', (meta_before[key], key))
        for scope, owner, field in counters:
            value, revision = fields_before[(scope, owner, field)]
            connection.execute('UPDATE fields SET value=?, revision=? WHERE scope=? AND owner=? AND field=?',
                               (value, revision, scope, owner, field))
    connection.close()
    repository._drop_cache()


def run(directory, mode, operations):
    repository = open_repository(directory, mode)
    config_module.set_repository(repository)
    variables = {}
    records = []
    for op in operations:
        record = {'op': op['op']}
        before = stored_state(directory) if op['op'] == 'save_router_publication' else None
        try:
            if op['op'] == 'reopen':
                repository = open_repository(directory, mode)
                config_module.set_repository(repository)
            result = execute(repository, op, variables)
            if before is not None:
                align_state_only_publication(repository, directory, op, before)
            if 'save' in op:
                variables[op['save']] = copy.deepcopy(result)
            record['result'] = result
        except Exception as error:  # the class and message are what is compared
            record['error'] = {'class': type(error).__name__, 'message': str(error).replace(str(repository.data_dir), '<dir>'),
                               'usages': getattr(error, 'usages', None)}
        record['dump'] = dump(directory)
        records.append(record)
    records.append({'op': 'layout', 'result': layout(directory), 'dump': dump(directory)})
    return records


def stored_rows(path):
    import sqlite3
    with sqlite3.connect(path) as connection:
        return [connection.execute(f'SELECT * FROM {table} ORDER BY {order}').fetchall()
                for table, order in (('meta', 'key'), ('sections', 'scope, owner'),
                                     ('fields', 'scope, owner, field'))]


def fingerprint(directory, mode):
    path = Path(directory) / 'configflow.sqlite3'
    before = stored_rows(path)
    repository = open_repository(directory, mode)
    opened = stored_rows(path)
    exported = repository.export_all()
    return {'before': before, 'opened': opened, 'after': stored_rows(path),
            'export': exported, 'profiles': repository.list_profiles()}


def hold(directory, seconds):
    """Hold .config.lock the way ProfileRepository._lock does, without opening."""
    repository = ProfileRepository.__new__(ProfileRepository)
    lock = Path(directory).resolve() / '.config.lock'
    with ProfileRepository._lock(repository, lock):
        print('locked', flush=True)
        time.sleep(seconds)
    return {'held': seconds}


def try_write(directory, mode):
    try:
        repository = open_repository(directory, mode)
        repository.update_system_transaction(
            lambda system: system['agents'].append({'id': 'py-agent', 'name': 'Py', 'profile_id': 'default'}))
        return {'ok': True}
    except Exception as error:  # the class and message are what is compared
        return {'class': type(error).__name__, 'message': str(error)}


def crash_write(directory, mode):
    from backend.common.config_store import ConfigStore
    repository = open_repository(directory, mode)
    ConfigStore._commit = lambda self, connection: os._exit(9)
    repository.update_system_transaction(
        lambda system: system['agents'].append({'id': 'crashed', 'name': 'X', 'profile_id': 'default'}))
    raise SystemExit('not reached')


def main():
    command, directory, output, mode = sys.argv[1:5]
    if command == 'crash-write':
        crash_write(directory, mode)
    if command == 'hold':
        result = hold(directory, float(sys.argv[5]))
    elif command == 'try-write':
        result = try_write(directory, mode)
    elif command == 'init':
        repository = open_repository(directory, mode)
        result = {'export': repository.export_all(), 'dump': dump(directory), 'layout': layout(directory),
                  'fingerprint': list(repository._store.fingerprint())}
    elif command == 'run':
        result = run(directory, mode, json.loads(Path(sys.argv[5]).read_text(encoding='utf-8')))
    elif command == 'fingerprint':
        result = fingerprint(directory, mode)
    else:
        raise SystemExit(f'unknown command {command}')
    Path(output).write_text(json.dumps(result, ensure_ascii=False), encoding='utf-8')


if __name__ == '__main__':
    main()
