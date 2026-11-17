"""A13 route oracle: inject only A14 preparation, retaining real Checker/diff/receipts."""
import hashlib
import runpy
from backend.agents.deployment_bundle import DeploymentBundle
from backend.routes import agents


def prepare(agent, config, **kwargs):
    assert kwargs['deployment_id'].startswith('check-')
    main = 'mode: global\nsecret: external-user-secret\n'
    rules = 'DOMAIN,diff-agent-token-0123456789.example.test\n'
    rules += ''.join(f'DOMAIN,新-{i:03d}.example.test\n' for i in range(80))
    files = {'config.yaml': (main.encode(), 'config'), 'rules/example.list': (rules.encode(), 'rule')}
    manifest = dict(protocol_version=1, deployment_id=kwargs['deployment_id'], agent_id=agent['id'],
                    profile_id='default', service_type='mihomo', config_path='config.yaml',
                    files=[dict(path=p, role=r, size=len(b), sha256=hashlib.sha256(b).hexdigest())
                           for p, (b, r) in files.items()])
    return DeploymentBundle.from_files(manifest, files, '')


agents._prepare_content_bundle = prepare
runpy.run_module('backend.app', run_name='__main__')
