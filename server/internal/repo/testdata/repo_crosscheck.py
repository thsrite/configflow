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
    raise SystemExit(f'unknown operation {name}')


def run(directory, mode, operations):
    repository = open_repository(directory, mode)
    variables = {}
    records = []
    for op in operations:
        record = {'op': op['op']}
        try:
            if op['op'] == 'reopen':
                repository = open_repository(directory, mode)
            result = execute(repository, op, variables)
            if 'save' in op:
                variables[op['save']] = copy.deepcopy(result)
            record['result'] = result
        except Exception as error:  # the class and message are what is compared
            record['error'] = {'class': type(error).__name__, 'message': str(error),
                               'usages': getattr(error, 'usages', None)}
        record['dump'] = dump(directory)
        records.append(record)
    records.append({'op': 'layout', 'result': layout(directory), 'dump': dump(directory)})
    return records


def fingerprint(directory, mode):
    path = Path(directory) / 'configflow.sqlite3'
    before = list(ConfigStore(path, lambda: 2.0).fingerprint())
    repository = open_repository(directory, mode)
    opened = list(repository._store.fingerprint())
    exported = repository.export_all()
    return {'before': before, 'opened': opened, 'after': list(repository._store.fingerprint()),
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
