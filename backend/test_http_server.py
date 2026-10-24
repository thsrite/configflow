"""Exercise the production WSGI transport and request cleanup on reused workers."""
from contextlib import contextmanager
import gc
import hashlib
import io
import threading
from unittest.mock import Mock
import weakref

from flask import Flask, g, jsonify, request, send_file
from flask_cors import CORS
import pytest
import requests
from waitress import create_server

from backend.http_server import run_http_server, server_options
from backend.http_queue import QueueWaitDispatcher


@contextmanager
def running_server(app, *, threads=1):
    options = dict(server_options(), host='127.0.0.1', port=0, threads=threads,
                   asyncore_loop_timeout=0.05)
    dispatcher = QueueWaitDispatcher()
    dispatcher.set_thread_count(threads)
    server = create_server(app, map={}, _dispatcher=dispatcher, **options)
    loop = threading.Thread(target=server.run, daemon=True)
    loop.start()
    session = requests.Session()
    session.trust_env = False
    base = f'http://127.0.0.1:{server.effective_port}'

    def call(method, path, **kwargs):
        # New TCP connections must reuse workers too, not only keep-alive calls.
        headers = {'Connection': 'close', **kwargs.pop('headers', {})}
        return session.request(method, base + path, headers=headers, timeout=5, **kwargs)

    try:
        yield call, server
    finally:
        session.close()
        server.task_dispatcher.shutdown(timeout=3)
        for channel in list(server._map.values()):
            channel.close()
        loop.join(3)
        assert not loop.is_alive()


def test_production_uses_reusable_workers_and_debug_keeps_reloader(monkeypatch):
    monkeypatch.delenv('CONFIGFLOW_HTTP_THREADS', raising=False)
    app, serve = Mock(), Mock()
    monkeypatch.setattr('waitress.serve', serve)
    run_http_server(app)
    serve.assert_called_once()
    assert serve.call_args.args == (app,)
    options = dict(serve.call_args.kwargs)
    dispatcher = options.pop('_dispatcher')
    assert isinstance(dispatcher, QueueWaitDispatcher)
    assert not dispatcher._monitor.is_alive()
    assert options == dict(host='0.0.0.0', port=5001, threads=8,
                           channel_timeout=300, clear_untrusted_proxy_headers=False)
    app.run.assert_not_called()
    run_http_server(app, debug=True)
    # The debug reloader passes SIGTERM on to the serving child (test_agent_metrics_history).
    app.run.assert_called_once_with(host='0.0.0.0', port=5001, debug=True, reloader_type='configflow')


@pytest.mark.parametrize('value', ['', 'zero', '0', '-1', '65', '2.5'])
def test_invalid_pool_size_fails_at_startup(monkeypatch, value):
    monkeypatch.setenv('CONFIGFLOW_HTTP_THREADS', value)
    with pytest.raises(ValueError, match='CONFIGFLOW_HTTP_THREADS'):
        server_options()


def test_pool_size_can_be_configured(monkeypatch):
    monkeypatch.setenv('CONFIGFLOW_HTTP_THREADS', '12')
    assert server_options()['threads'] == 12


def test_reused_worker_releases_profiles_and_failed_request_state(tmp_path, monkeypatch):
    from backend.common import config as config_module
    from backend.common.config_repository import ProfileRepository
    from backend.common.profile_context import install_profile_context

    repository = ProfileRepository(tmp_path)
    for profile_id in ('alpha', 'beta'):
        repository.create_profile({'id': profile_id, 'name': profile_id})
    monkeypatch.setattr(config_module, '_repository', repository)
    app = Flask(__name__)
    # pytest retains logged exception tracebacks. Keep that test-only owner
    # from masking whether Flask/Waitress themselves release request objects.
    monkeypatch.setattr(app.logger, 'disabled', True)
    starts, refs, workers = [], [], []

    class TemporaryPayload(dict):
        pass

    @app.before_request
    def check_previous_request_was_released():
        starts.append((config_module._CONFIG_CACHE.get(), config_module._CONFIG_BASELINES.get(),
                       getattr(repository._local, 'document', None), getattr(g, 'marker', None)))
        workers.append(threading.current_thread())

    install_profile_context(app)

    @app.get('/api/profiles/<profile_id>/snapshot')
    def snapshot(profile_id):
        data = config_module.get_config()
        assert 'temporary' not in data
        data['temporary'] = TemporaryPayload(profile=profile_id)
        refs.append(weakref.ref(data['temporary']))
        g.marker = profile_id
        with repository._document():
            if request.args.get('fail'):
                raise RuntimeError('expected test failure')
        return jsonify(profile=data['profile_id'])

    with running_server(app) as (call, _):
        for profile_id in ('alpha', 'beta', 'alpha'):
            response = call('GET', f'/api/profiles/{profile_id}/snapshot')
            assert response.status_code == 200
            assert response.json() == {'profile': profile_id}
            assert response.headers['X-ConfigFlow-Profile'] == profile_id
        assert call('GET', '/api/profiles/alpha/snapshot?fail=1').status_code == 500
        assert call('GET', '/api/profiles/beta/snapshot').json() == {'profile': 'beta'}
    assert len({id(worker) for worker in workers}) == 1
    assert len(starts) == 5 and all(state == (None, None, None, None) for state in starts)
    gc.collect()
    assert all(ref() is None for ref in refs)


def test_proxy_headers_auth_mcp_upload_and_file_responses(tmp_path, monkeypatch):
    from backend.common import config as config_module
    from backend.common.auth import generate_token, require_auth
    from backend.common.config_repository import ProfileRepository
    from backend.mcp_server import mcp_bp
    from backend.mcp_server.invoker import call_api

    repository = ProfileRepository(tmp_path)
    monkeypatch.setattr(config_module, '_repository', repository)
    monkeypatch.setattr('backend.common.auth.is_auth_enabled', lambda: True)
    monkeypatch.setattr('backend.mcp_server.auth.is_auth_enabled', lambda: True)
    monkeypatch.setattr('backend.mcp_server.server.has_tool', lambda name: name == 'protected')
    monkeypatch.setattr('backend.mcp_server.server.call_tool',
                        lambda name, arguments: call_api('GET', '/protected'))
    app = Flask(__name__)
    CORS(app)
    app.register_blueprint(mcp_bp)
    body = b'configflow-transfer\x00' * 65536  # Exceeds Waitress's in-memory input buffer.

    @app.route('/protected', methods=['GET', 'POST'])
    @require_auth
    def protected():
        return jsonify(success=True, host=request.headers.get('X-Forwarded-Host'),
                       scheme=request.headers.get('X-Forwarded-Proto'),
                       forwarded=request.headers.get('X-Forwarded-For'))

    @app.post('/upload')
    @require_auth
    def upload():
        return jsonify(digest=hashlib.sha256(request.get_data()).hexdigest())

    @app.get('/download')
    @require_auth
    def download():
        return send_file(io.BytesIO(body), as_attachment=True, download_name='fixture.bin',
                         mimetype='application/octet-stream')

    headers = {'Authorization': f'Bearer {generate_token("admin")}',
               'X-Forwarded-Host': 'config.example:8443', 'X-Forwarded-Proto': 'https',
               'X-Forwarded-For': '198.51.100.10, 192.0.2.1'}
    with running_server(app) as (call, _):
        assert call('GET', '/protected').status_code == 401
        response = call('GET', '/protected', headers=headers)
        assert response.status_code == 200
        assert response.json() == {'success': True, 'host': 'config.example:8443', 'scheme': 'https',
                                   'forwarded': '198.51.100.10, 192.0.2.1'}
        preflight = call('OPTIONS', '/protected', headers={
            'Origin': 'https://client.example', 'Access-Control-Request-Method': 'POST',
            'Access-Control-Request-Headers': 'Authorization',
        })
        assert preflight.status_code == 200
        assert preflight.headers['Access-Control-Allow-Origin'] == 'https://client.example'
        assert call('POST', '/upload', data=body, headers=headers).json() == {
            'digest': hashlib.sha256(body).hexdigest(),
        }
        response = call('GET', '/download', headers=headers)
        assert response.status_code == 200 and response.content == body
        head = call('HEAD', '/download', headers=headers)
        assert head.status_code == 200 and head.content == b''
        assert head.headers['Content-Length'] == str(len(body))
        partial = call('GET', '/download', headers={**headers, 'Range': 'bytes=100-199'})
        assert partial.status_code == 206 and partial.content == body[100:200]
        payload = {'jsonrpc': '2.0', 'id': 1, 'method': 'tools/call',
                   'params': {'name': 'protected', 'arguments': {}}}
        assert call('POST', '/mcp', json=payload).status_code == 401
        # Only one worker: an MCP call into REST must not wait for its own pool.
        mcp = call('POST', '/mcp', json=payload, headers=headers)
        assert mcp.status_code == 200 and mcp.json()['result']['isError'] is False
        assert call('GET', '/mcp', headers=headers).status_code == 405


def test_long_request_does_not_block_an_available_worker():
    app = Flask(__name__)
    entered, release = threading.Event(), threading.Event()
    results = []

    @app.get('/slow')
    def slow():
        entered.set()
        assert release.wait(3)
        return 'finished'

    @app.get('/ready')
    def ready():
        return 'ready'

    with running_server(app, threads=2) as (call, _):
        caller = threading.Thread(target=lambda: results.append(call('GET', '/slow').text))
        caller.start()
        try:
            assert entered.wait(3)
            assert call('GET', '/ready').text == 'ready'
        finally:
            release.set()
            caller.join(3)
        assert not caller.is_alive() and results == ['finished']
