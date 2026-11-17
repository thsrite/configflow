"""Real Python reference for the registration-key security fix; run via rt.

This records the retired implementation's exposure. The Go HTTP regression
requires the opposite outcome. Preparation and routes are real; network I/O is
forbidden by the imported synthetic fixture.
"""
import json
from urllib.parse import urlencode

import pytest

from backend.test_agent_content_preparation import content_env  # noqa: F401
from backend.test_agent_push_diff_api import diff_env, push, settle  # noqa: F401


def test_python_registration_key_exposure_reference(diff_env, monkeypatch):
    key = 'synthetic-registration-key'
    encoded = 'synthetic%2Dregistration%2Dkey'
    monkeypatch.setenv('AGENT_REGISTRATION_KEY', key)
    client, _manager, config, _remote, _repository, ids, executor = diff_env
    agent_id = ids['mihomo']
    push(client, agent_id)
    config['rule_library'][0]['content'] += f'\n+.plain-{key}.example.com\n+.encoded-{encoded}.example.com'
    result = settle(client, executor, agent_id)
    assert key in json.dumps(result) and encoded in json.dumps(result)
    rule = next(file for file in result['files'] if file['role'] == 'rule')
    path = f'/api/agents/{agent_id}/push-diff/entries?'
    args = dict(version=result['version'], file=rule['path'], change='added')
    entries = client.get(path + urlencode(args))
    assert entries.status_code == 200
    assert key in json.dumps(entries.json) and encoded in json.dumps(entries.json)
    for query in (key, encoded):
        searched = client.get(path + urlencode(dict(args, q=query)))
        assert searched.status_code == 200 and searched.json['total'] == 1
    print('Python reference: registration key plaintext/encoded exposed in diff, entries and search; confirmed')


@pytest.mark.parametrize('field', ['journal', 'oxidns_ha_publication'])
def test_python_projection_secret_reference(field):
    from backend.routes.agents import _public_agent
    secret = 'synthetic-cross-key'
    record = dict(id='device', deployments=dict(old=dict(status='failed', api_key=secret)))
    record[field] = ('old ' + secret if field == 'journal' else
                     dict(status='failed', message='old ' + secret, members=[]))
    assert secret in json.dumps(_public_agent(record)[field])
    print('Python reference: cross-field deployment credential exposure; confirmed')
