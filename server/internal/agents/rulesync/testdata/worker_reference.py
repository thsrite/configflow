"""Drive real Python reconciliation owners over synthetic persisted receipts.

The native panel call and live snapshot are device-boundary fakes. Agent HTTP
framing and untrusted archives have separate loopback boundary tests in Go.
"""
import network_guard  # noqa: F401
import json
import tempfile
from pathlib import Path
import pytest
from backend.test_agent_deployments import env
from backend.test_agent_rule_sync import sync_env, edit_library
from backend.agents import rule_sync, mihomo_panel
from backend.common import config as config_module

with tempfile.TemporaryDirectory() as temp, pytest.MonkeyPatch.context() as patch:
    patch.setattr(rule_sync, 'start_loop', lambda: None)
    values = sync_env.__wrapped__(env.__wrapped__(Path(temp), patch))
    repository,manager,agent,source,bundle = values
    patch.setattr(rule_sync, 'fetch_snapshot', lambda _m,_a,_b: bundle)
    calls=[]
    patch.setattr(mihomo_panel,'call',lambda _m,_id,resource,method,args,**kwargs: calls.append({'resource':resource,'method':method,'args':args}) or {})
    now=[100.0]
    worker=rule_sync.RuleSyncWorker(manager,clock=lambda:now[0])
    edit_library(repository,content='new.example.com')
    out=[]
    for at in (100.0,104.999,105.0,105.0,105.0):
        now[0]=at
        worker.run(agent['id'])
        state=rule_sync.status(manager,agent['id'])['state']
        state.pop('updated_at',None)
        out.append({'state':state,'refreshes':len(calls)})
    print(json.dumps({'steps':out,'calls':calls},ensure_ascii=False))
    config_module.reset_config_context()
