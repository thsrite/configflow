"""Observe the real Flask revision guard, retaining the real C36 write owner."""
import json
import os
from pathlib import Path
import runpy
from backend.common import setup_flow

from backend.routes import gateway_onboarding as route

if os.environ.get("A21_CONCURRENT_WRITE") == "1":
    takeover = route.onboarding_takeover
    def concurrent_metadata(repository, profile_id, profile):
        result = takeover(repository, profile_id, profile)
        repository.update_profile(profile_id, {"name": "Concurrent metadata"})
        return result
    route.onboarding_takeover = concurrent_metadata

original = setup_flow.adopt_owned_write

def observe(repository, profile_id, before, after):
    with open(os.environ['A21_ADOPT_TRACE'], 'a', encoding='utf-8') as trace:
        trace.write(json.dumps([before['_revision'], after['_revision']]) + '\n')
    return original(repository, profile_id, before, after)

setup_flow.adopt_owned_write = observe
runpy.run_path(str(Path(__file__).resolve().parents[3] / 'linuxgateway/difftest/testdata/reference.py'))
