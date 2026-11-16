"""Async preflight failures, final job reads, owner scope and restart."""
import copy

from discovery_jobs import Completion
from harness import Pair, json_body
from cases.discovery_reads import heartbeat
from cases.domain_discovery import options
from cases.subscriptions import require


def failures(binary, recorder):
    for kind, case, variant, message in [
        ('probe', 'R051-F13', 'no-agent', '没有可用于探测的 Agent：需要开启域名发现且 Agent 版本不低于 1.5.0-go'),
        ('probe', 'R051-F14', 'no-proxy', '请先设置默认代理规则集，并在当前配置中启用（探测会经它指向的策略组访问）'),
        ('region', 'R054-F13', 'region-disabled', '请先开启区域检测'),
        ('region', 'R054-F14', 'region-no-agent', '没有可用于区域检测的 Agent：需要开启域名发现且 Agent 版本不低于 1.6.0-go')]:
        opts = options()
        opts['agents'] = []
        if variant == 'no-proxy':
            opts['profile']['domain_discovery']['proxy_ruleset'] = None
        if variant == 'region-no-agent':
            opts['profile']['domain_discovery']['region_check_enabled'] = True
        with Pair(binary, recorder, options=opts) as pair:
            observer = Completion(pair, kind, message)
            route = 'POST /api/domain-discovery/' + ('probe' if kind == 'probe' else 'region/check')
            body = {'domains': ['example.com']} if kind == 'probe' else {'services': True}
            pair.request(case, route, body=json_body(body), expect=202, writes=True, runtime_writes=True, interaction=observer,
                         capture=observer.capture, projection=observer.projection, variant=variant)
            policy = copy.deepcopy(pair.policy)
            policy.times.add('body.job.finished_at')
            paths = ['/api/domain-discovery/probe/' + (ident or '0' * 32) for ident in observer.ids]
            pair.request('R052-S03', 'GET /api/domain-discovery/probe/<job_id>', paths=paths, expect=200, writes=True, policy=policy,
                         check=lambda r: require(r.json()['job']['status'] == 'failed' and r.json()['job']['message'] == message,
                                                 'final public poll retains exact preflight failure'), variant=variant)
            pair.request('R052-F09', 'GET /api/domain-discovery/probe/<job_id>', paths=[path + '?profile=other' for path in paths],
                         expect=404, writes=True, variant=variant)
            pair.restart()
            pair.request('R052-B01', 'GET /api/domain-discovery/probe/<job_id>', paths=paths, expect=404, writes=True, variant=variant)
    with Pair(binary, recorder) as pair:
        for ident in ('missing-job', '0' * 32):
            pair.request('R052-F08', 'GET /api/domain-discovery/probe/<job_id>', '/api/domain-discovery/probe/' + ident,
                         expect=404, writes=True, variant=ident)


def guards(binary, recorder):
    for case, values in [('F08', None), ('F08', []), ('F09', [1]), ('F09', ['192.0.2.1']),
                         ('F09', ['中国.example']), ('F10', [f'x{i}.example' for i in range(101)])]:
        with Pair(binary, recorder, options=options()) as pair:
            pair.request('R051-' + case, 'POST /api/domain-discovery/probe', body=json_body({'domains': values}), expect=400,
                         writes=True, variant=repr(values) if len(str(values)) < 100 else '101-values')
    # Python region_check.py:19 caps a region batch at ten input domains;
    # eleven is below the generic 100-domain guard and reaches this branch.
    for case, body in [('F08', {'domains': ['192.0.2.1']}),
                       ('F09', {'services': False, 'domains': [f'x{i}.example' for i in range(11)]}), ('F10', {'services': False})]:
        with Pair(binary, recorder, options=options()) as pair:
            pair.request('R054-' + case, 'POST /api/domain-discovery/region/check', body=json_body(body), expect=400, writes=True)
    for case, values in [('F08', None), ('B02', []), ('F09', [f'x{i}.example' for i in range(31)]), ('F10', ['example.com'])]:
        with Pair(binary, recorder, options=options()) as pair:
            pair.request('R060-' + case, 'POST /api/domain-discovery/website/identify', body=json_body({'domains': values}), expect=400, writes=True)
    opts = options()
    opts['agent_updates'] = {'domain_discovery_enabled': True}
    with Pair(binary, recorder, options=opts) as pair:
        heartbeat(pair, variant='website-no-record')
        pair.request('R060-F11', 'POST /api/domain-discovery/website/identify', body=json_body({'domains': ['example.com']}), expect=400, writes=True)


GROUPS = {'discovery-job-preflight': failures, 'discovery-job-guards': guards}
