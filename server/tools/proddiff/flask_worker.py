"""Test-only Flask launcher; disable named background starters before app import."""
import importlib
import os
import threading
from pathlib import Path

STARTERS = [
    ('backend.agents.dns_follow', 'start_loop'),
    ('backend.agents.oxidns_ha_publish', 'start_loop'),
    ('backend.agents.ipv6_verification', 'start_loop'),
    ('backend.agents.rule_sync', 'start_loop'),
    ('backend.agents.metrics_flush', 'start_loop'),
    ('backend.agents.oxidns_ranking_history', 'start_loop'),
    ('backend.utils.domain_discovery_service', 'start_auto_loop'),
    ('backend.utils.scheduled_refresh', 'start_loop'),
    ('backend.routers.rule_sync', 'start_loop'),
]

# Audit every thread the reference starts (names stay in the private run directory); run.py fails
# if a disabled loop starts anyway, e.g. through a starter bound before it was replaced.
_threads = Path(os.environ['PRODDIFF_PY_THREADS'])
_start = threading.Thread.start


def _audited_start(thread):
    with _threads.open('a') as audit:
        audit.write(thread.name + '\n')
    return _start(thread)


threading.Thread.start = _audited_start
for module, name in STARTERS:
    setattr(importlib.import_module(module), name, lambda *args, **kwargs: None)
from backend.utils.sub_store_runtime import runtime
runtime.start_monitor = lambda: None
from backend.app import app
Path(os.environ['PRODDIFF_PY_LOOPS']).write_text('\n'.join(m + '.' + n for m, n in STARTERS) + '\nsub-store-idle\n')
Path(os.environ['PRODDIFF_PY_ROUTES']).write_text(''.join(
    f'{method}\t{rule.rule}\n' for rule in app.url_map.iter_rules() if rule.endpoint != 'static'
    and rule.rule.startswith(('/api/', '/mcp')) for method in sorted(rule.methods - {'HEAD', 'OPTIONS'})))
from backend.agents import metrics_flush
metrics_flush.load_history()
from waitress import serve
serve(app, host='127.0.0.1', port=int(os.environ['CONFIGFLOW_BACKEND_PORT']), threads=16)
