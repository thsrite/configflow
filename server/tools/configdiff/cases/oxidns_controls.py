"""G11: enabled/disabled/default/custom OxiDNS template control output."""
import copy

import yaml

from harness import Pair, json_body
from oxidns import check as etag_check, capture as etag_capture
from cases.helpers import profile_policy
from cases.rule_library import DOMAIN
from cases.subscriptions import require

CONTROLS = ('cache', 'probe-cache', 'api', 'log', 'query_recorder', 'ip_selector', 'dynamic_domain_set',
            'adguard', 'ros_address_list', 'ros_route', 'rule_update')
ROS = {'address': '192.0.2.1:8728', 'username': 'synthetic-user', 'password': 'synthetic-router-password'}


def settings_for(control, shape):
    enabled, custom = shape != 'off', shape == 'custom'
    value = {'direct_rulesets': ['control-rule']}
    if control == 'cache':
        value['cache_enabled'] = enabled
        if custom:
            value.update(cache_size=2048, cache_lazy_ttl=0, cache_dump_enabled=True,
                         cache_dump_file='./state/custom-cache.dump', cache_dump_interval=60)
    elif control == 'probe-cache':
        value['probe_cache_enabled'] = enabled
        if custom:
            value['probe_cache_size'] = 512
    elif control == 'log':
        value['log'] = {'level': 'debug' if custom else 'info' if enabled else 'off'}
        if custom:
            value['log'].update(file='./state/control.log', rotation='hourly', max_files=3)
    else:
        section = value[control] = {'enabled': enabled}
        if control in ('ros_address_list', 'ros_route'):
            section['args'] = copy.deepcopy(ROS)
            if control == 'ros_route':
                section['args'].update(routing_table='synthetic-table', gateway4='192.0.2.2')
            if custom:
                section['args'].update({'async': False, 'min_ttl': 0, 'max_ttl': 900, 'comment_prefix': '合成🧪',
                                        'cleanup_on_shutdown': False, 'tls': {'insecure': True}})
                if control == 'ros_route':
                    section['args'].update(gateway6='2001:db8::2', distance=120)
                else:
                    section['args'].update(address_list4='synthetic-v4', address_list6='synthetic-v6')
        elif control == 'adguard':
            section['content'] = '||ads.example^\n'
            if custom:
                section.update(mode='refused', urls=['https://rules.example/adguard'])
        elif custom:
            if control == 'api':
                section.update(listen='127.0.0.1:19200', username='synthetic', password='synthetic-api-password',
                               webui_root='./webui', ssl={'cert': './tls/cert.pem', 'key': './tls/key.pem'})
            elif control == 'query_recorder':
                section['args'] = {'path': './state/custom-queries.db', 'queue_size': 512, 'batch_size': 64,
                                   'flush_interval_ms': 100, 'memory_tail': 0, 'retention_days': 2,
                                   'cleanup_interval_hours': 2, 'reader_concurrency': 1}
            elif control == 'ip_selector':
                section['args'] = {'selection_mode': 'first_success', 'probe_methods': ['tcp:443'], 'top_n': 2,
                                   'probe_stagger': 0, 'dnssec_policy': 'skip', 'cache': {'enabled': False}}
            elif control == 'dynamic_domain_set':
                section.update(route='proxy', learn=True, args={'path': './state/custom-learned.txt',
                               'bootstrap_rules': ['domain:example.com']}, learn_args={'phase': 'before',
                               'questions': 'all', 'rule_kind': 'domain', 'error_mode': 'stop'})
            elif control == 'rule_update':
                section['args'] = {'schedule': '15 * * * *', 'timezone': 'UTC', 'timeout': '30s'}
    return value


def draft_projection(value, index, key='config'):
    """Read the schema-known string log.level without YAML 1.1's off bool."""
    result = copy.deepcopy(value)
    if not isinstance(result, dict) or not isinstance(result.get(key), str):
        return result
    text = result[key]
    result[key] = yaml.safe_load(text)
    if index == 1 and isinstance(result[key], dict):
        root = yaml.compose(text, Loader=yaml.SafeLoader)
        if isinstance(root, yaml.MappingNode):
            for root_key, child in root.value:
                if root_key.value != 'log' or not isinstance(child, yaml.MappingNode):
                    continue
                for name, scalar in child.value:
                    if (name.value == 'level' and isinstance(scalar, yaml.ScalarNode)
                            and scalar.style is None and scalar.value == 'off'
                            and text[scalar.start_mark.index:scalar.end_mark.index] == 'off'
                            and isinstance(result[key].get('log'), dict)):
                        result[key]['log']['level'] = 'off'
    return result


def verify(document, control, shape):
    enabled, custom = shape != 'off', shape == 'custom'
    plugins = {item['tag']: item for item in document['plugins']}
    tag = {'cache': 'lazy_cache', 'probe-cache': 'local_probe_cache', 'dynamic_domain_set': 'dynamic_domains',
           'adguard': 'adguard_rules', 'rule_update': 'rules_cron'}.get(control, control)
    if control == 'api':
        require(('api' in document) is enabled, 'API is present only when enabled')
        if custom:
            require(document['api']['http']['auth']['password'] == 'synthetic-api-password'
                    and document['api']['http']['ssl']['key'] == './tls/key.pem', 'generated API credentials and TLS references remain exact')
    elif control == 'log':
        require(document['log']['level'] == ('debug' if custom else 'info' if enabled else 'off'), 'logging level is exact')
        if custom:
            require(document['log']['rotation'] == {'type': 'hourly', 'max_files': 3}, 'custom log rotation is retained')
    else:
        require((tag in plugins) is enabled, 'only enabled feature emits its plugin: ' + control)
        if not enabled:
            return
        args = plugins[tag]['args']
        if control == 'cache':
            require(args['short_circuit'] is True, 'main cache short-circuits')
            require(('lazy_cache_ttl' in args) is not custom, 'zero lazy TTL is omitted rather than emitted as invalid zero')
            if custom:
                require(args['dump_file'] == './state/custom-cache.dump' and args['size'] == 2048, 'cache dump is a runtime state reference')
        elif control == 'probe-cache':
            require(args['short_circuit'] is False and args['size'] == (512 if custom else 2048), 'isolated probe cache never short-circuits the caller')
        elif control in ('ros_address_list', 'ros_route'):
            require(args['password'] == ROS['password'] and args['address'] == ROS['address'], 'generated RouterOS connection parameters retain synthetic secrets')
            if custom:
                require(args['tls'] == {'insecure': True} and args['async'] is False, 'TLS and asynchronous flags preserve type/value')
        elif control == 'dynamic_domain_set':
            require(('learn_classified_domain' in plugins) is custom, 'learning plugin is opt-in independently of dynamic classification')
        elif control == 'rule_update':
            require(plugins['rules_download']['args']['downloads'] and '$rules_refresh' in args['jobs'][0]['executors'], 'cron references the materialized download/reload chain')
            require(args['timezone'] == ('UTC' if custom else 'Asia/Shanghai'), 'cron timezone survives generation')


def matrix(binary, recorder):
    for control in CONTROLS:
        for shape in ('off', 'defaults', 'custom'):
            for scoped in (False, True):
                prefix = '/api/profiles/default/oxidns' if scoped else '/api/oxidns'
                template = '/api/profiles/<profile_id>/oxidns' if scoped else '/api/oxidns'
                generate_prefix = '/api/profiles/default/generate' if scoped else '/api/generate'
                generate_template = '/api/profiles/<profile_id>/generate' if scoped else '/api/generate'
                opts = {'shared': {'rule_library': [DOMAIN]}, 'profile': {'rule_configs': [
                    {'id': 'control-rule', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT'}]}}
                settings = settings_for(control, shape)
                variant = control + '/' + shape + '/' + str(scoped)
                with Pair(binary, recorder, options=opts) as pair:
                    pair.policy = profile_policy(pair)
                    def check(reply, key='config'):
                        verify(yaml.safe_load(reply.json()[key]), control, shape)
                    pair.request('G11', 'POST ' + template + '/render', prefix + '/render',
                                 body=json_body({'settings': settings, 'base_url': 'http://panel.example'}),
                                 expect=200, writes=True, projection=draft_projection, check=check, variant=variant + '/draft')
                    pair.request('G11', 'POST ' + template + '/settings', prefix + '/settings', body=json_body(settings),
                                 expect=200, writes=True, check=etag_check(pair), capture=etag_capture(pair), variant=variant + '/save')
                    pair.request('G11', 'POST ' + generate_template + '/oxidns/preview', generate_prefix + '/oxidns/preview',
                                 body=json_body({'base_url': 'http://panel.example'}), expect=200, writes=True,
                                 projection=lambda value, index: draft_projection(value, index, 'content'),
                                 check=lambda r: check(r, 'content'), variant=variant + '/saved-preview')


GROUPS = {'oxidns-control-output': matrix}
