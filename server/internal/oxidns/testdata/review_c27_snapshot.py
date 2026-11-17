#!/usr/bin/env python3
"""C27 coverage on a disposable production-snapshot copy; schema-only output.

Run on .32 inside proddiff's read-only mount and loopback-only namespace.
The framework's delivered generation is supplemented with C27 public read/no-op
calls; these are not a claim that the still-missing C32 routes are implemented.
"""
import ast
from collections import Counter
import contextlib
from copy import deepcopy
import io
import json
import logging
import os
from pathlib import Path
import shutil
import socket
import sqlite3
import sys
import tempfile

SOURCE = 'backend/common/rule_dns.py'
HITS = set()


def trace(frame, event, arg):
    if event == 'line' and frame.f_code.co_filename.endswith('/' + SOURCE):
        HITS.add(frame.f_lineno)
    return trace


def run(snapshot):
    if {name for _, name in socket.if_nameindex()} != {'lo'} or not os.statvfs(snapshot).f_flag & os.ST_RDONLY:
        raise RuntimeError('snapshot isolation missing')
    logging.disable(logging.CRITICAL)
    counts = Counter()
    with tempfile.TemporaryDirectory(prefix='c27-snapshot-') as temporary, contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
        data = Path(temporary) / 'data'
        shutil.copytree(snapshot, data)
        os.environ.update(DATA_DIR=str(data), LOG_FILE=str(Path(temporary) / 'app.log'), LOG_LEVEL='ERROR', SUB_STORE_BUILTIN_DIR='/nonexistent')
        db = sqlite3.connect(f'file:{data / "configflow.sqlite3"}?mode=ro', uri=True)
        profiles = [row[0] for row in db.execute("SELECT DISTINCT owner FROM fields WHERE scope='profile'")]
        db.close()
        from backend.common.config import get_config, reset_config_context
        from backend.common.rule_dns import apply_rule_dns, explicit_route, rule_dns_state
        from backend.converters.oxidns import generate_oxidns_config, get_oxidns_custom_files, get_oxidns_ruleset_downloads
        sys.settrace(trace)
        try:
            for identity in profiles:
                cfg = get_config(identity)
                counts['profiles'] += 1
                settings = cfg.get('oxidns', {})
                for field in ('rule_actions', 'rule_dns_follow', 'rule_dns_parked', 'direct_rules', 'proxy_rules', 'direct_rulesets', 'proxy_rulesets'):
                    counts['saved-' + field] += len(settings.get(field) or ())
                operations = {
                    'delivered-config': lambda: generate_oxidns_config(cfg, 'http://127.0.0.1:5001'),
                    'delivered-files': lambda: get_oxidns_custom_files(cfg),
                    'delivered-downloads': lambda: get_oxidns_ruleset_downloads(cfg, 'http://127.0.0.1:5001'),
                    'read-model': lambda: rule_dns_state(cfg),
                    'noop-write': lambda: apply_rule_dns(deepcopy(cfg), deepcopy(cfg), {}),
                }
                for name, call in operations.items():
                    try:
                        call()
                        counts[name + '-ok'] += 1
                    except Exception:
                        counts[name + '-rejected'] += 1
                for item in cfg.get('rule_configs', []):
                    if isinstance(item, dict) and isinstance(item.get('id'), str):
                        explicit_route(cfg, item['id'])
                        counts['explicit-read'] += 1
                reset_config_context()
        finally:
            sys.settrace(None)
    coverage = {node.name: sorted(HITS & set(range(node.lineno, node.end_lineno+1)))
                for node in ast.walk(ast.parse(Path(SOURCE).read_text())) if isinstance(node, ast.FunctionDef)}
    Path('server/parity/review-c27-production.coverage.json').write_text(json.dumps({'counts': dict(counts), 'source_lines': coverage}, indent=2) + '\n')
    print(json.dumps({'counts': dict(counts)}))


if __name__ == '__main__':
    try:
        run(Path(sys.argv[1]).resolve())
    except Exception:
        raise SystemExit('C27 snapshot coverage failed; no snapshot values printed') from None
