"""Invoke public Python generators against a private repository copy; no app/loops."""
import json
import sys
from pathlib import Path

from backend.common.config import get_config, get_system_config, reset_config_context
from backend.converters.mihomo import generate_mihomo_config, get_mihomo_provider_downloads, get_mihomo_ruleset_downloads
from backend.converters.surge import generate_surge_config, convert_proxies_to_surge_text
from backend.converters.loon import generate_loon_config, convert_proxies_to_loon_text
from backend.converters.mosdns import generate_mosdns_config, get_mosdns_custom_files, get_mosdns_ruleset_downloads
from backend.converters.oxidns import generate_oxidns_config, get_oxidns_custom_files, get_oxidns_ruleset_downloads
from backend.utils.provider_delivery import DeliverySnapshot, prepare_subscription_provider
from backend.utils.proxy_server_dns import reported_core_version
import yaml

BASE_URL = 'http://127.0.0.1:5001'


def offline_fetch(*args, **kwargs):
    raise ConnectionError('offline')


def generate(job):
    cfg = get_config(job['profile_id'])
    kind = job['kind']
    kwargs = dict(base_url=BASE_URL, preflight_providers=False)
    if job.get('agent_id'):
        agent = next(item for item in get_system_config()['agents'] if item['id'] == job['agent_id'])
        kwargs.update(for_agent=True, region_probe=bool(agent.get('domain_discovery_enabled')),
                      mihomo_core_version=reported_core_version(agent))
    if kind in ('mihomo', 'agent-mihomo'):
        return generate_mihomo_config(cfg, **kwargs)
    for service, function in [('surge', generate_surge_config), ('loon', generate_loon_config),
                              ('mosdns', generate_mosdns_config), ('oxidns', generate_oxidns_config)]:
        if kind in (service, 'agent-' + service):
            return function(cfg, base_url=BASE_URL)
    auxiliary = {
        'mosdns-files': lambda: get_mosdns_custom_files(cfg),
        'mosdns-downloads': lambda: get_mosdns_ruleset_downloads(cfg, BASE_URL),
        'oxidns-files': lambda: get_oxidns_custom_files(cfg),
        'oxidns-downloads': lambda: get_oxidns_ruleset_downloads(cfg, BASE_URL),
        'mihomo-providers': lambda: get_mihomo_provider_downloads(cfg, BASE_URL),
        'mihomo-rules': lambda: get_mihomo_ruleset_downloads(cfg, BASE_URL),
    }
    if kind in auxiliary:
        return json.dumps(auxiliary[kind](), ensure_ascii=False)
    if kind.startswith('subscription-'):
        sub = next(item for item in cfg['subscriptions'] if item['id'] == job['sub_id'])
        main = yaml.safe_load(generate_mihomo_config(cfg, base_url=BASE_URL, preflight_providers=False))
        snapshot = DeliverySnapshot.capture(job['profile_id'], main, profile=cfg)
        result = prepare_subscription_provider(sub, cfg, snapshot, fetch=offline_fetch, source_prepared=True)
        if kind == 'subscription-mihomo':
            return result['content']
        proxies = yaml.safe_load(result['content'])['proxies']
        return (convert_proxies_to_surge_text if kind == 'subscription-surge' else convert_proxies_to_loon_text)(proxies)
    raise ValueError('unknown generator kind')


with Path(sys.argv[2]).open('x') as output:
    for job in json.loads(Path(sys.argv[1]).read_bytes()):
        result = {'index': job['index'], 'status': 'ok', 'content': ''}
        try:
            result['content'] = generate(job)
        except Exception as error:
            result.update(status='error', error=str(error))
        finally:
            reset_config_context()
        output.write(json.dumps(result, ensure_ascii=False) + '\n')
        output.flush()
