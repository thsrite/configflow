"""Real Python forced candidate owner with an unchanged library declaration."""
import base64
import contextlib
import io
import hashlib
import json
import os
from pathlib import Path
import sys
with contextlib.redirect_stdout(io.StringIO()):
    from backend.common import config
    from backend.common.config_repository import ProfileRepository
    from backend.agents.deployment_bundle import DeploymentBundle
    from backend.agents import rule_sync, rule_sync_store
    from backend.utils.scheduled_refresh import refresh_rule_source
    data = json.load(sys.stdin)
    source = data['source']
    repo = ProfileRepository(Path(os.environ['DATA_DIR']))
    config.set_repository(repo)
    repo.save_shared({'rule_library': source['rule_library']})
    rule = source['rule_library'][0]
    refresh_rule_source(rule, 'manual', config_data=source, fetch=lambda _url: b'second.example\n')
    archive = base64.b64decode(data['archive'])
    bundle = DeploymentBundle(data['manifest'], archive, hashlib.sha256(archive).hexdigest(), data['version'])
    baseline = {'bundle': bundle, 'source': source, 'base_url': 'https://panel.example'}
    agent = {'id': 'primary', 'service_type': 'oxidns', 'profile_id': 'default'}
    candidate = rule_sync.prepare_candidate(agent, source, baseline, {'library': True, 'rules': False},
                                           'forced-python', force_refresh=True)
    files = rule_sync_store.bundle_files(candidate)
    rules = [value for value, role in files.values() if role == 'rule']
    result = {'Fresh': any(b'second.example' in value for value in rules),
              'Old': any(b'first.example' in value for value in rules),
              'Reload': candidate.manifest['reload_only']}
print(json.dumps(result))
