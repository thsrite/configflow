"""Independent R5 references. Original archives stay read-only; real Python runs
on documented typed input projections. Scheduling/history algorithms are not copied.
Run from repository root with the shared reference virtualenv on the test host.
"""
import copy
import datetime as dt
import hashlib
import json
import math
from pathlib import Path
import re
import tempfile
from unittest import mock
from backend.common import config as cm
from backend.common.config_repository import ProfileRepository
from backend.utils import scheduled_refresh as sr, subscription_health as health
from backend.routes import tasks

ROOT = Path(__file__).parent
ARCHIVE = ROOT / 'corpus.json'
NOW = dt.datetime(2026, 11, 1, 0, 0, 0, 250000, tzinfo=dt.timezone.utc)
GIVEN = NOW.replace(microsecond=0)

class Fixed(dt.datetime):
    @classmethod
    def now(cls, tz=None):
        return NOW if tz is not None else NOW.replace(tzinfo=None)

def integer(value, default=0):
    # Shared model.Int contract, independent of Go implementation.
    if isinstance(value, bool):
        return default
    if isinstance(value, str):
        if not re.fullmatch(r'-?[0-9]+', value):
            return default
        value = int(value)
    if not isinstance(value, (int, float)) or not math.isfinite(value) or int(value) != value:
        return default
    return int(value) if -(2**63) <= value < 2**63 else default

def strict_json(value):
    try:
        json.dumps(value, allow_nan=False)
        return True
    except ValueError:
        return False

def receipt(value):
    if not isinstance(value, dict) or not strict_json(value):
        return {}
    result = copy.deepcopy(value)
    count = result.get('consecutive_failures')
    # Shared taskstatus decimal-counter policy: reject Unicode digits and
    # nonempty containers as one native validation category. Feeding an invalid
    # decimal string to Python proves the refusal without duplicating backoff.
    if isinstance(count, str) and count.strip() and not re.fullmatch(r'[+-]?[0-9]+(?:_[0-9]+)*', count.strip()):
        result['consecutive_failures'] = 'invalid-native-counter'
    elif isinstance(count, (list, dict)) and count:
        result['consecutive_failures'] = 'invalid-native-counter'
    return result

def outcome(call):
    try:
        result = call()
        return {'output': result.isoformat() if isinstance(result, dt.datetime) else result}
    except Exception as exc:
        return {'error': type(exc).__name__}

def health_projection(repo, ident):
    data = health.load_health()
    entry = data.get(ident)
    if not isinstance(entry, dict):
        return
    history = entry.get('history')
    if not isinstance(history, list) or any(not isinstance(item, dict) for item in history):
        entry['history'] = []
        repo.write_shared_json(health.HEALTH_FILE, data)

def replay(row, project=True):
    with tempfile.TemporaryDirectory() as temp:
        repo = ProfileRepository(temp)
        cm.set_repository(repo)
        cm.reset_config_context()
        files = row['files']
        if files.get('health') is not None:
            repo.write_shared_text(health.HEALTH_FILE, files['health'])
        if 'shared' in files:
            shared = copy.deepcopy(files['shared'])
            for key, field in [('subscriptions','interval'), ('rule_library','update_interval')]:
                for item in shared.get(key, []):
                    if field in item:
                        item[field] = integer(item[field], 86400)
            repo.save_shared(shared)
            statuses = json.loads(files['statuses'])
            statuses = {key: receipt(value) for key, value in statuses.items()}
            (Path(temp)/'task-status.json').write_text(json.dumps(statuses))
            for ident, value in files.get('caches', {}).items():
                repo.write_shared_json('subscribes/'+ident+'.json', value)
            import os
            for file in files.get('rule_files', []):
                path = repo.write_shared_text('rules/'+file['name'], file['content'])
                os.utime(repo.shared_path('rules/'+file['name']), ns=(file['mtime_ns'],file['mtime_ns']))
        result = []
        for call in row['output']:
            label = call['call']
            def invoke():
                if label == 'rows':
                    statuses = sr.task_status.load()
                    shared = repo.get_shared()
                    return tasks._subscription_rows(shared,statuses)+tasks._rule_rows(shared,statuses)
                if label == 'due':
                    return [key for key,_,_ in sr._due(GIVEN)]
                if label.startswith('forget '):
                    health.forget(label.removeprefix('forget '))
                else:
                    ident, kwargs = json.loads(label)
                    if project:
                        health_projection(repo, ident)
                        kwargs['count'] = integer(kwargs.get('count'))
                    health.record_fetch(ident, **kwargs)
                return repo.read_shared_json(health.HEALTH_FILE)
            result.append({'call':label, **outcome(invoke)})
            cm.reset_config_context()
        return result

rows = json.loads(ARCHIVE.read_text())
references = {}
with mock.patch.object(health,'datetime',Fixed), mock.patch.object(sr,'datetime',Fixed):
    for index,row in enumerate(rows):
        fn,value=row['fn'],row['input']
        original = {key:row[key] for key in ('output','error') if key in row}
        native = original
        if fn in ('subscription_interval','rule_interval'):
            projected=copy.deepcopy(value)
            field='interval' if fn=='subscription_interval' else 'update_interval'
            if field in projected:
                projected[field]=integer(projected[field],86400)
            native=outcome(lambda: getattr(sr,fn)(projected))
        elif fn=='next_run_at':
            native=outcome(lambda: sr.next_run_at(receipt(value['entry']),value['interval'],value['baseline'],GIVEN))
        elif fn=='scenario':
            native={'output':replay(row)}
        elif fn=='describe_failure' and value['class'] in ('PermissionError','ProfileValidationError'):
            # R8: native diagnostic type, not an emulated Python OSError name.
            native={'output':'更新失败（'+('fs.PathError' if value['class']=='PermissionError' else 'repo.Error')+'）'}
        if native != original:
            references[str(index)]={'input':value,'original':original,'native':native,'decision':'R5' if fn!='describe_failure' else 'R8'}
# Keep the old edge input and original behavior evidence beside the typed oracle.
edge={'fn':'scenario','input':'health edge','files':{'health':'{"s":{"history":"ab"},"t":{"history":{"x":1}},"u":5,"v":{"history":7}}'},'output':[]}
for ident,kwargs in [('s',{'ok':False,'count':'3'}),('t',{'ok':False}),('u',{'ok':True,'from_cache':True}),('v',{'ok':False})]:
    edge['output'].append({'call':json.dumps([ident,kwargs],ensure_ascii=False)})
with mock.patch.object(health,'datetime',Fixed):
    edge_native=replay(edge)
    edge_original=replay(edge,project=False)
payload={'corpus_sha256':hashlib.sha256(ARCHIVE.read_bytes()).hexdigest(),'rows':references,'edge':edge,'edge_native':edge_native,'edge_original':edge_original}
(ROOT/'n89_expected.json').write_text(json.dumps(payload,ensure_ascii=False,separators=(',',':'))+'\n')
print('independent refresh references:',len(references))
