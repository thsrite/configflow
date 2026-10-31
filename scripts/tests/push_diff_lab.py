#!/usr/bin/env python3
"""「待推送的变更」 on the isolated PVE lab (CT152 Mihomo Agent), through a private ConfigFlow.

Run on the test server only, from this checkout synced by rt. Reuses the
subscription-hosts lab harness (backup / private server and tunnels / publish /
restore) with its own work directory, and never touches other guests:

  backup          tar of CT152 /etc/mihomo + /opt/configflow-agent (deployment journal, cache.db), file manifest, state
  setup           private ConfigFlow data from this checkout: lab subscriptions, a manual SS node with a password,
                  a content rule set, the CT152 Agent; plus two Agent records that were never pushed / pushed by an
                  old version (no device behind them) for the dialog's no-baseline states
  serve / stop    private ConfigFlow (127.0.0.1:15951) and SSH tunnels to the CT152 Agent
  sub V [P]       fixture subscriptions served to ConfigFlow (see subscription_hosts_lab.SUBSCRIPTIONS)
  ct152-prepare   put back the rule file the CT152 deployment journal references (from the journal's own backup)
  publish LABEL   push to CT152 and wait for the result
  device LABEL    the managed files actually on CT152 (digests, main YAML, provider nodes, rule lines)
  change          real edits through the API: a hosts entry and controller secret, rule set lines,
                  the manual node's password, one more subscription node
  diff LABEL      GET /push-diff until it settles; asserts every credential shown is marked (hidden on request)
  check LABEL B A compare the dialog's lists (diff LABEL) with what changed on CT152 between device B and A
  extra-delete    delete the old-version Agent record (the dialog's request error case)
  restore         stop everything, put CT152 back from the backup and compare (journal, cache.db, file times)
"""
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import time

sys.path.insert(0, str(Path(__file__).resolve().parent))
import subscription_hosts_lab as base  # noqa: E402

ROOT = Path(__file__).resolve().parents[2]
base.WORK = Path('/tmp/cf-pushdiff-lab')
WORK = base.WORK
PASSWORD_BEFORE = 'lab-manual-pass-before-7c1e'
PASSWORD_AFTER = 'lab-manual-pass-after-93ab'
CONTROLLER_SECRET = 'lab-controller-secret-5d20'
RULES_BEFORE = '+.alpha.lab.invalid\n+.beta.lab.invalid\n+.gamma.lab.invalid'
RULES_AFTER = '+.alpha.lab.invalid\n+.gamma.lab.invalid\n+.delta.lab.invalid\n+.epsilon.lab.invalid'
HOSTS_DOMAIN = 'printer.lab.invalid'
NEVER_ID = 'lab-never-pushed'
LEGACY_ID = 'lab-old-version'

SEED_EXTRA = r'''
import json, sys
from pathlib import Path
from backend.common import config as store
from backend.common.config_repository import ProfileRepository
work, password, rules, extra = Path(sys.argv[1]), sys.argv[2], sys.argv[3], json.loads(sys.argv[4])
store.set_repository(ProfileRepository(work / 'data', default_config_factory=store.get_default_config,
                                       initial_config_factory=store.get_default_config))
repo = store.get_repository()
def shared(s):
    s['nodes'] = [{'id': 'lab-manual', 'name': '手动 SS', 'type': 'ss', 'server': 'manual.lab.invalid', 'port': 8388,
                   'enabled': True, 'params': {'cipher': 'aes-128-gcm', 'password': password}}]
repo.update_shared_transaction(shared)
rule = {'id': 'lab-direct', 'name': '实验直连', 'source_type': 'content', 'behavior': 'domain', 'format': 'text',
        'content': rules, 'enabled': True}
repo.create_rule_library_item(rule, rules)
def profile(p):
    p['proxy_groups'][0]['manual_nodes'] = ['lab-manual']
    ref = {'id': 'lab-direct-ref', 'itemType': 'ruleset', 'name': '实验直连', 'library_rule_id': 'lab-direct',
           'policy': 'DIRECT', 'enabled': True, 'behavior': 'domain'}
    p['rule_configs'] = [item for item in p['rule_configs'] if item['id'] != 'lab-direct-ref']
    p['rule_configs'].insert(len(p['rule_configs']) - 1, ref)
repo.update_profile_transaction(repo.default_profile_id(), profile)
from backend.common.agent_manager import get_agent_manager
manager = get_agent_manager()
def add(rows):
    rows[:] = [row for row in rows if row['id'] not in {item['id'] for item in extra}] + extra
manager._update_agents(add)
print(json.dumps({'agents': [a['id'] for a in manager.get_all_agents()]}, ensure_ascii=False))
'''


def save(name, value):
    return base.save(name, value)


def setup():
    """Fresh private data from this checkout (the server must be stopped)."""
    WORK.mkdir(mode=0o700, exist_ok=True)
    link = WORK / 'src-before'
    if not link.exists():
        link.symlink_to(ROOT)
    base.seed()
    now = time.strftime('%Y-%m-%dT%H:%M:%S')
    common = {'host': '127.0.0.1', 'port': 9, 'token': 'lab-placeholder-token', 'service_type': 'mihomo',
              'profile_id': 'default', 'deployment_method': 'shell', 'status': 'offline', 'version': '1.8.50-go',
              'enabled': True, 'last_heartbeat': now, 'created_at': now}
    extra = [{**common, 'id': NEVER_ID, 'name': '实验 · 从未推送', 'config_version': '0'},
             # Pushed by a version that kept neither file digests nor the bundle.
             {**common, 'id': LEGACY_ID, 'name': '实验 · 旧版本推送', 'config_version': '5e1f0c2a', 'config_revision': 3}]
    result = subprocess.run(base.python('before') + ['-c', SEED_EXTRA, str(WORK), PASSWORD_BEFORE, RULES_BEFORE,
                                                     json.dumps(extra, ensure_ascii=False)],
                            cwd=ROOT, env=base.environment(), capture_output=True, text=True, check=True)
    print(result.stdout.strip())


def api(method, path, body=None):
    status, data = base.api(method, path, body)
    assert status in (200, 201, 202), (method, path, status, data)
    return data


def change():
    """The edits a user makes before opening the dialog, through the same API as the pages."""
    node = next(item for item in api('GET', '/nodes') if item['id'] == 'lab-manual')
    node['params']['password'] = PASSWORD_AFTER
    api('PUT', '/nodes/lab-manual', node)
    rule = next(item for item in api('GET', '/rule-library') if item['id'] == 'lab-direct')
    rule.pop('has_ip_part', None)
    api('PUT', '/rule-library/lab-direct', {**rule, 'content': RULES_AFTER})
    custom = base.VARIANTS['fake-ip'] + f'secret: {CONTROLLER_SECRET}\nhosts:\n  {HOSTS_DOMAIN}: 198.51.100.30\n'
    api('POST', '/custom-config/mihomo', {'config': custom, 'provider_update_interval': base.PROVIDER_INTERVAL})
    base.sub('v1-e')
    base.refresh('change')
    print(json.dumps({'changed': ['nodes/lab-manual password', 'rule-library/lab-direct', 'custom hosts + secret',
                                  'subscription lab-airport v1 -> v1-e']}, ensure_ascii=False))


def diff(label, agent=None):
    agent = agent or base.agent_id()
    deadline = time.monotonic() + 180
    while True:
        status, body = base.api('GET', f'/agents/{agent}/push-diff')
        if status != 200 or body.get('status') != 'checking' or time.monotonic() > deadline:
            break
        time.sleep(1.5)
    save(f'evidence/{label}-push-diff.json', {'http_status': status, 'body': body})
    sys.path.insert(0, str(ROOT))
    from backend.agents.push_diff import without_secrets
    encoded = json.dumps(body, ensure_ascii=False)
    hidden = json.dumps(without_secrets(body), ensure_ascii=False)  # 隐藏敏感信息 on
    lab_token = base.lab_agent()['token']
    credentials = (('manual password before', PASSWORD_BEFORE), ('manual password after', PASSWORD_AFTER),
                   ('controller secret', CONTROLLER_SECRET), ('agent token', lab_token),
                   ('config token', base.credentials()['config_token']))
    leaks = [name for name, value in credentials if value in hidden]
    summary = {'status': status, 'result': body.get('status'), 'reason': body.get('reason'), 'counts': body.get('counts'),
               'files': [(item['path'], item['status'], item.get('summary')) for item in body.get('files') or []],
               'config': [(item['key'], item['change'], item.get('sensitive'), item.get('old'), item.get('new'),
                           item.get('list') and {k: item['list'][k] for k in ('before', 'after', 'added', 'removed', 'modified')})
                          for item in json.loads(hidden).get('config', {}).get('items', [])] if body.get('config') else [],
               'shown': [name for name, value in credentials if value in encoded],
               'leaks_when_hidden': leaks, 'bytes': len(encoded.encode())}
    print(json.dumps(summary, ensure_ascii=False, indent=1))
    assert not leaks, leaks
    assert lab_token not in encoded  # the Agent's own token is a stored credential: never sent
    return body


DEVICE_FILES = r'''import hashlib, json, os
base = '/etc/mihomo'
manifest = json.load(open(os.path.join(base, '.configflow-deployments/active-manifest.json')))
out = {'deployment_id': manifest.get('deployment_id'), 'files': {}}
for item in manifest['files']:
    path = os.path.join(base, item['path'])
    data = open(path, 'rb').read() if os.path.isfile(path) else None
    out['files'][item['path']] = {'role': item['role'], 'sha256': hashlib.sha256(data).hexdigest() if data is not None else None,
                                  'size': len(data) if data is not None else None,
                                  'text': data.decode('utf-8', 'replace') if data is not None and item['role'] != 'config' else None}
out['config'] = open(os.path.join(base, manifest.get('config_path', 'config.yaml')), encoding='utf-8').read()
print(json.dumps(out, ensure_ascii=False))
'''


def device(label):
    state = base.guest_json(152, DEVICE_FILES)
    path = WORK / f'evidence/{label}-device.json'
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(state, ensure_ascii=False))
    path.chmod(0o600)  # the deployed main file contains the controller secret
    print(json.dumps({'deployment_id': state['deployment_id'], 'files': {name: (item['role'], item['sha256'][:12] if item['sha256'] else None)
                                                                         for name, item in state['files'].items()}},
                     ensure_ascii=False, indent=1))


def check(label, before_label, after_label):
    """The dialog shown before the push against what the push really changed on CT152."""
    sys.path.insert(0, str(ROOT))
    from backend.agents.push_diff import diff_config, rule_entries
    import yaml
    shown = json.loads((WORK / f'evidence/{label}-push-diff.json').read_text())['body']
    before = json.loads((WORK / f'evidence/{before_label}-device.json').read_text())
    after = json.loads((WORK / f'evidence/{after_label}-device.json').read_text())

    def canonical(name, item):
        return 'config.yaml' if item['role'] == 'config' else name

    old = {canonical(name, item): item for name, item in before['files'].items()}
    new = {canonical(name, item): item for name, item in after['files'].items()}
    device_changes = {}
    for path in set(old) | set(new):
        if path not in old:
            device_changes[path] = 'added'
        elif path not in new:
            device_changes[path] = 'removed'
        elif old[path]['sha256'] != new[path]['sha256']:
            device_changes[path] = 'modified'
    dialog_changes = {('config.yaml' if item['role'] == 'config' else item['path']): item['status'] for item in shown['files']}
    result = {'device_changed_files': device_changes, 'dialog_changed_files': dialog_changes,
              'files_match': device_changes == dialog_changes}
    counts = {}
    for path, status in device_changes.items():
        if path == 'config.yaml':
            continue
        item_old, item_new = old.get(path), new.get(path)
        role = (item_new or item_old)['role']
        if role == 'rule':
            a = set(rule_entries(item_old['text'].encode())) if item_old else set()
            b = set(rule_entries(item_new['text'].encode())) if item_new else set()
            counts[path] = {'kind': 'rules', 'added': len(b - a), 'removed': len(a - b)}
        elif role == 'provider':
            names = lambda item: {proxy['name'] for proxy in (yaml.safe_load(item['text']) or {}).get('proxies', [])} if item else set()
            a, b = names(item_old), names(item_new)
            counts[path] = {'kind': 'nodes', 'added': len(b - a), 'removed': len(a - b)}
    shown_counts = {item['path']: {key: item['summary'].get(key) for key in ('kind', 'added', 'removed')}
                    for item in shown['files'] if item.get('summary') and item['summary']['kind'] in ('rules', 'nodes')}
    result.update(device_counts=counts, dialog_counts=shown_counts, counts_match=counts == shown_counts)
    # The device's own main files, before and after, compared by the same key paths.
    device_items = diff_config(before['config'].encode(), after['config'].encode())['items']
    keys = lambda items: sorted((item['key'], item['change'], bool(item['sensitive'])) for item in items)
    result.update(device_config_items=keys(device_items), dialog_config_items=keys(shown['config']['items']),
                  config_match=keys(device_items) == keys(shown['config']['items']))
    deployed = yaml.safe_load(after['config'])
    result['deployed_has'] = {'hosts': (deployed.get('hosts') or {}).get(HOSTS_DOMAIN),
                              'secret_set': deployed.get('secret') == CONTROLLER_SECRET,
                              'manual_password_after': any(proxy.get('password') == PASSWORD_AFTER for proxy in deployed.get('proxies', []))}
    save(f'evidence/{label}-check.json', result)
    print(json.dumps(result, ensure_ascii=False, indent=1))
    assert result['files_match'] and result['counts_match'] and result['config_match'], result


def extra_delete():
    api('DELETE', f'/agents/{LEGACY_ID}')
    print(json.dumps({'deleted': LEGACY_ID}))


def restore():
    base.restore()
    # Writing the TUN's stable_secret back switches its addr_gen_mode; a second pass restores the mode.
    base.verify()


if __name__ == '__main__':
    base.lab.guard(guests=False)
    action, args = sys.argv[1], sys.argv[2:]
    if action not in ('stop', 'sub', 'setup', 'diff', 'check', 'extra-delete', 'change'):
        base.lab.guard()
    {'backup': base.backup, 'setup': setup, 'serve': lambda: base.serve('before'), 'stop': base.stop, 'sub': base.sub,
     'ct152-prepare': base.ct152_prepare, 'publish': base.publish, 'device': device, 'change': change, 'diff': diff,
     'check': check, 'extra-delete': extra_delete, 'restore': restore, 'verify': base.verify}[action](*args)
