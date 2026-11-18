"""Regenerate synthetic pin fixtures from the real Python test helper.

Run from repo root with PYTHONPATH=. and the test-server Python environment.
"""
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace

from backend.common import config
from backend.common.config_repository import ProfileRepository
from backend.test_dns_follow import pin_rule

result = {}
with tempfile.TemporaryDirectory() as directory:
    for parked in (False, True):
        repository = ProfileRepository(Path(directory) / str(parked))
        config.set_repository(repository)
        pin_rule(SimpleNamespace(repository=repository), 'udp://127.0.0.1:1053', parked=parked)
        profile = repository.get_profile('default')
        key = 'rule_dns_parked' if parked else 'rule_actions'
        result['parked' if parked else 'active'] = {'rules': profile['rule_configs'], 'key': key, 'actions': profile['oxidns'][key]}
print(json.dumps(result, ensure_ascii=False, indent=2))
