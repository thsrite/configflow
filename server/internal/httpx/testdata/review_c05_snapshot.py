#!/usr/bin/env python3
"""C05 source coverage of the offline proddiff snapshot, never export values.

Requires the same read-only snapshot mount and loopback-only namespace as
proddiff.sh. All requests and normalization use a disposable private copy.
There is no app startup/background loop; real route registration installs the
profile hooks, followed by the real auth gate. Output is static schema only.
"""
import ast
from collections import Counter, defaultdict
import contextlib
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
from urllib.parse import quote, urlencode

SOURCES = ('backend/common/profile_context.py', 'backend/common/oxidns_settings.py')
HITS = defaultdict(set)


def trace(frame, event, arg):
    if event == 'line':
        for source in SOURCES:
            if frame.f_code.co_filename.endswith('/' + source):
                HITS[source].add(frame.f_lineno)
    return trace


def run(snapshot):
    if {name for _, name in socket.if_nameindex()} != {'lo'}:
        raise RuntimeError('network isolation missing')
    if not os.statvfs(snapshot).f_flag & os.ST_RDONLY:
        raise RuntimeError('read-only snapshot mount missing')
    counts = Counter()
    logging.disable(logging.CRITICAL)
    with tempfile.TemporaryDirectory(prefix='c05-snapshot-') as temporary, contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
        data = Path(temporary) / 'data'
        shutil.copytree(snapshot, data)
        os.environ.update(DATA_DIR=str(data), LOG_FILE=str(Path(temporary) / 'app.log'), LOG_LEVEL='ERROR',
                          ADMIN_USERNAME='c05-admin', ADMIN_PASSWORD='c05-password',
                          JWT_SECRET_KEY='c05-synthetic-secret', SUB_STORE_BUILTIN_DIR='/nonexistent')
        db = sqlite3.connect(f'file:{data / "configflow.sqlite3"}?mode=ro', uri=True)
        profiles = [row[0] for row in db.execute("SELECT DISTINCT owner FROM fields WHERE scope='profile'")]
        db.close()
        from flask import Flask
        from backend.common.auth import generate_token
        from backend.common.config import get_config, reset_config_context
        from backend.common.oxidns_settings import normalize_oxidns_settings
        from backend.routes import register_blueprints
        from backend.routes.auth import setup_before_request
        app = Flask(__name__)
        register_blueprints(app)
        setup_before_request(app)
        client = app.test_client()
        headers = {'Authorization': f'Bearer {generate_token("c05-admin")}'}
        sys.settrace(trace)
        try:
            for identity in profiles:
                cfg = get_config(identity)
                settings = cfg.get('oxidns', {})
                mode = settings.get('config_mode', 'template')
                counts['saved-mode-' + (mode if mode in ('template', 'native') else 'other')] += 1
                for validate in (False, True):
                    try:
                        normalize_oxidns_settings(settings, validate_native_arguments=validate)
                        counts[f'normalize-{validate}-ok'] += 1
                    except Exception:
                        counts[f'normalize-{validate}-rejected'] += 1
                reset_config_context()
                for name, path in [('global-nodes', '/api/nodes'), ('query-rules', '/api/rules'),
                                   ('query-settings', '/api/oxidns/settings'),
                                   ('scoped-settings', f'/api/profiles/{quote(identity, safe="")}/oxidns/settings'),
                                   ('scoped-config', f'/api/config/{quote(identity, safe="")}/oxidns')]:
                    response = client.get(path + '?' + urlencode({'profile_id': identity}), headers=headers)
                    counts[name + '-' + str(response.status_code)] += 1
        finally:
            sys.settrace(None)
    coverage = {}
    for source in SOURCES:
        coverage[source] = {node.name: sorted(HITS[source] & set(range(node.lineno, node.end_lineno+1)))
                            for node in ast.walk(ast.parse(Path(source).read_text())) if isinstance(node, ast.FunctionDef)}
    output = {'counts': dict(counts), 'source_lines': coverage}
    Path('server/parity/review-c05-production.coverage.json').write_text(json.dumps(output, indent=2) + '\n')
    print(json.dumps({'counts': dict(counts)}))


if __name__ == '__main__':
    try:
        run(Path(sys.argv[1]).resolve())
    except Exception:
        # Tracebacks, request paths and exception values can contain secrets.
        raise SystemExit('C05 snapshot coverage failed; no snapshot values printed') from None
