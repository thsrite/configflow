"""A17 oracle: real Flask, repository, follow owner, A14 and loopback HTTP."""
import contextlib
import io
import json
import os
from pathlib import Path
import sys

with contextlib.redirect_stdout(io.StringIO()):
    import yaml
    import requests
    from flask import Flask
    from backend.common import config
    from backend.common.config_repository import ProfileRepository
    from backend.common.agent_manager import get_agent_manager
    from backend.agents import dns_follow
    from backend.agents.deployment_bundle import build_deployment_bundle
    from backend.routes.agents import _prepare_transactional_content
    from backend.routes import register_blueprints
    from backend.routes.auth import setup_before_request

    data = json.load(sys.stdin)
    repository = ProfileRepository(Path(os.environ['DATA_DIR']))
    config.set_repository(repository)
    repository.save_system({'agents': data['agents']})
    repository.save_profile('default', {'oxidns': {'config_mode': 'native', 'native_config': data['draft']}})
    manager = get_agent_manager()
    # A fixed clock only; all business logic and production timeout arguments stay real.
    dns_follow._now = lambda: '2026-11-18T00:00:00Z'
    original_request = requests.sessions.Session.request
    def local_only(self, method, url, **kwargs):
        from urllib.parse import urlsplit
        assert urlsplit(url).hostname == '127.0.0.1', url
        return original_request(self, method, url, **kwargs)
    requests.sessions.Session.request = local_only

    def publish(agent_id, ident, baseline=False):
        agent = manager.get_agent_by_id(agent_id)
        source, revision = repository.get_publish_snapshot('default')
        if baseline:
            bundle = _prepare_transactional_content(agent, source, base_url='https://panel.example', deployment_id=ident)['bundle']
        else:
            bundle = build_deployment_bundle(agent, source, 'dns:\n  enable: true\n  listen: 0.0.0.0:2053\n', deployment_id=ident)
        result = manager.publish_deployment(agent_id, bundle, config_revision=revision, base_url='https://panel.example', resource_config=source)
        if result['status'] == 'verifying':
            result = manager.get_deployment(agent_id, ident)
        assert result['status'] == 'succeeded', result
    publish('target', 'target-baseline', True)
    publish('source', 'source-before-binding')
    app = Flask(__name__)
    register_blueprints(app)
    setup_before_request(app)
    client = app.test_client()
    from backend.common.auth import generate_token
    admin = {'Authorization': 'Bearer ' + generate_token('a17-admin')}
    path = '/api/agents/target/dns-follow'
    replies = []
    def request(method, body=None, auth=True, suffix=''):
        response = client.open(path+suffix, method=method, data=body, content_type='application/json', headers=admin if auth else {})
        replies.append({'status': response.status_code, 'body': '' if method == 'HEAD' else response.get_json()})
    request('GET', auth=False)
    request('GET')
    request('HEAD')
    request('PUT', data['body'])
    if replies[-1]['status'] == 200:
        if data['scenario'] == 'drift':
            def edit(profile):
                profile['oxidns']['native_config'] = profile['oxidns']['native_config'].replace('worker_threads: 2', 'worker_threads: 3')
            repository.update_profile_transaction('default', edit)
        publish('source', 'source-next')
        dns_follow.tick(manager)
        request('GET')
        if data['scenario'] in ('disabled-listener', 'drift'):
            request('POST', suffix='/retry')
        elif data['scenario'] == 'success':
            request('DELETE')
    target = manager.get_agent_by_id('target')
    settings = repository.get_compat_config('default')['oxidns']
    job = target.get('dns_follow_job')
    binding = target.get('dns_follow')
    proofs = True
    for reply in replies:
        body = reply['body']
        if not isinstance(body, dict):
            continue
        seen_binding, seen_state = body.get('binding'), body.get('state')
        if seen_binding and seen_state and seen_state.get('target_deployment_id'):
            expected = 'dns-' + dns_follow._digest([seen_binding['id'], seen_state['source_deployment_id'], seen_state.get('attempt', 0)])[:48]
            proofs = proofs and seen_state['target_deployment_id'] == expected
    if binding:
        entry = dns_follow._entry(dns_follow._document({'oxidns': settings})[1], binding)
        proofs = proofs and binding['upstream_signature'] == dns_follow._digest(entry)
    if job:
        entry = dns_follow._entry(dns_follow._document({'oxidns': job['candidate_settings']})[1], binding)
        proofs = proofs and job['candidate_signature'] == dns_follow._digest(entry)
    result = {'replies': replies, 'draft': yaml.safe_load(settings['native_config']),
              'state': target.get('dns_follow_state'), 'has_job': bool(job), 'proofs': proofs,
              'source_status': manager.get_agent_by_id('source')['latest_deployment']['status'],
              'target_status': target['latest_deployment']['status']}
print(json.dumps(result, ensure_ascii=False))
