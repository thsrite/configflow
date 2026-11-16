"""Named H057–H077 guards; each label makes its own actual public request."""
from discovery_jobs import Completion
from harness import Pair, json_body
from cases.config_backup import message
from cases.discovery_reads import heartbeat
from cases.domain_discovery import options


def branches(binary, recorder):
    asynchronous = [
        ('H068', 'probe', 'no-agent', '没有可用于探测的 Agent：需要开启域名发现且 Agent 版本不低于 1.5.0-go'),
        ('H069', 'probe', 'no-proxy', '请先设置默认代理规则集，并在当前配置中启用（探测会经它指向的策略组访问）'),
        ('H073', 'region', 'disabled', '请先开启区域检测'),
        ('H074', 'region', 'no-agent', '没有可用于区域检测的 Agent：需要开启域名发现且 Agent 版本不低于 1.6.0-go')]
    for case, kind, variant, text in asynchronous:
        opts = options()
        opts['agents'] = []
        if case == 'H069':
            opts['profile']['domain_discovery']['proxy_ruleset'] = None
        if case == 'H074':
            opts['profile']['domain_discovery']['region_check_enabled'] = True
        with Pair(binary, recorder, options=opts) as pair:
            observer = Completion(pair, kind, text)
            pair.request(case, 'POST /api/domain-discovery/' + ('probe' if kind == 'probe' else 'region/check'),
                         body=json_body({'domains': ['example.com']} if kind == 'probe' else {'services': True}),
                         expect=202, writes=True, runtime_writes=True, interaction=observer, capture=observer.capture, projection=observer.projection, variant=variant)
    vectors = [('H070', 'probe', {'domains': values}, 'domains 必须是 1-100 个有效域名')
               for values in (None, [], ['192.0.2.1'], ['x' + str(i) + '.example' for i in range(101)])]
    vectors += [('H076', 'region/check', {'services': False, 'domains': ['x' + str(i) + '.example' for i in range(11)]}, '一次最多检测 10 个域名'),
                ('H077', 'region/check', {'services': False}, '请选择要检测的服务或域名'),
                ('H057', 'website/identify', {'domains': ['x' + str(i) + '.example' for i in range(31)]}, '一次最多识别 30 个域名'),
                ('H058', 'website/identify', {'domains': ['example.com']}, '需要开启域名发现且版本不低于 1.6.0-go 的 Agent'),
                ('H066', 'ignore', {'values': ['192.0.2.1']}, 'values 必须是有效域名列表'),
                ('H067', 'ignore', {'values': ['overflow.example']}, '忽略列表最多 2000 条')]
    for case, route, body, text in vectors:
        opts = options()
        if case == 'H067':
            opts['profile']['domain_discovery']['ignored'] = ['x' + str(i) + '.example' for i in range(2000)]
        with Pair(binary, recorder, options=opts) as pair:
            pair.request(case, 'POST /api/domain-discovery/' + route, body=json_body(body), expect=400, writes=True,
                         check=message(text), variant=repr(body)[:80])
    opts = options()
    opts['agent_updates'] = {'domain_discovery_enabled': True}
    with Pair(binary, recorder, options=opts) as pair:
        heartbeat(pair, variant='H059')
        pair.request('H059', 'POST /api/domain-discovery/website/identify', body=json_body({'domains': ['example.com']}),
                     expect=400, writes=True, check=message('example.com 没有可用 Agent 最近采集的记录'))


GROUPS = {'discovery-named-branches': branches}
