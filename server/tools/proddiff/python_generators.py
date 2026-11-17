"""Invoke public Python generators against a private repository copy; no app/loops."""
import json
from hashlib import sha256
import sys
from pathlib import Path

from backend.common.config import get_config, get_system_config, reset_config_context
from backend.converters.mihomo import generate_mihomo_config, get_mihomo_provider_downloads, get_mihomo_ruleset_downloads
from backend.converters.surge import generate_surge_config, convert_proxies_to_surge_text
from backend.converters.loon import generate_loon_config, convert_proxies_to_loon_text
from backend.converters.mosdns import generate_mosdns_config, get_mosdns_custom_files, get_mosdns_ruleset_downloads
from backend.converters.oxidns import generate_oxidns_config, get_oxidns_custom_files, get_oxidns_ruleset_downloads
from backend.routes.aggregations import generate_aggregation_provider
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
    if kind == 'agent-deployment-bundle':
        from backend.agents.deployment_bundle import build_deployment_bundle
        from backend.utils.provider_delivery import prepare_provider_bundle
        import base64
        service=agent.get('service_type','mihomo')
        providers=downloads=custom=()
        if service=='mihomo':
            content=generate_mihomo_config(cfg,**kwargs)
            providers=prepare_provider_bundle(cfg,yaml.safe_load(content),render_all=True,subscription_fetch=offline_fetch)
        elif service=='mosdns':
            content=generate_mosdns_config(cfg,base_url=BASE_URL)
            downloads=get_mosdns_ruleset_downloads(cfg,BASE_URL);custom=get_mosdns_custom_files(cfg)
        else:
            content=generate_oxidns_config(cfg,base_url=BASE_URL)
            downloads=get_oxidns_ruleset_downloads(cfg,BASE_URL);custom=get_oxidns_custom_files(cfg)
        bundle=build_deployment_bundle(agent,cfg,content,provider_downloads=providers,ruleset_downloads=downloads,
            custom_files=custom,base_url=BASE_URL,deployment_id='proddiff-bundle')
        files=bundle.files();entries=[]
        for item in bundle.manifest['files']:
            entry={key:item[key] for key in ('path','role','name') if key in item}
            data = files[item['path']]
            if item['size'] != len(data) or item['sha256'] != sha256(data).hexdigest():
                raise ValueError('bundle file integrity mismatch')
            if item['role'] in ('config', 'provider'):entry['document']=yaml.safe_load(data)
            else:
                entry.update(size=item['size'],sha256=item['sha256'])
                value=base64.b64encode(files[item['path']]).decode()
                if value:entry['content']=value
            entries.append(entry)
        return json.dumps(dict(protocol_version=1,service_type=service,config_path=bundle.manifest['config_path'],files=entries),ensure_ascii=False)
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
    if kind == 'aggregation-provider':
        agg = next(item for item in cfg['subscription_aggregations'] if item['id'] == job['agg_id'])
        main = yaml.safe_load(generate_mihomo_config(cfg, base_url=BASE_URL, preflight_providers=False))
        return generate_aggregation_provider(agg, config=cfg, main_config=main, persist=False,
                                             subscription_fetch=offline_fetch, source_prepared=True)['content']
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
