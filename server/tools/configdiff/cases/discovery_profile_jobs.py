"""Prove query/header/default ownership with distinct job preflight outcomes."""
import copy
from urllib.parse import urlencode

from discovery_jobs import Completion
from harness import Pair, json_body
from inventory import selector_rows
from sources import Source
from cases.config_backup import message
from cases.discovery_reads import heartbeat
from cases.domain_discovery import options
from cases.profile_reads import selected
from cases.pull_routes import SourceStable
from cases.setup import Stable
from cases.subscriptions import require

PROBE_MESSAGES = {'default': '请先设置默认代理规则集，并在当前配置中启用（探测会经它指向的策略组访问）',
                  'other': '没有可用于探测的 Agent：需要开启域名发现且 Agent 版本不低于 1.5.0-go'}
REGION_MESSAGES = {'default': '请先开启区域检测',
                   'other': '没有可用于区域检测的 Agent：需要开启域名发现且 Agent 版本不低于 1.6.0-go'}


class ScopedCompletion(Completion):
    def __init__(self, pair, kind, message, owner):
        super().__init__(pair, kind, message, profile=owner)
        self.stable = Stable(pair)

    def before(self, index):
        self.stable.before(index)

    def after(self, index, reply):
        super().after(index, reply)
        self.stable.after(index, reply)

    def compare(self, policy):
        return super().compare(policy) + self.stable.compare(policy)


def async_selectors(binary, recorder):
    for rid, route, recipes in selector_rows():
        if rid not in ('R051', 'R054'):
            continue
        for suffix, number in recipes:
            if number == 3:
                continue  # These routes have no explicit-profile URL alias.
            fixture = options()
            fixture['agents'] = []
            fixture['other_profile'] = copy.deepcopy(fixture['profile'])
            fixture['profile']['domain_discovery']['proxy_ruleset'] = None
            fixture['other_profile']['domain_discovery']['region_check_enabled'] = True
            with Pair(binary, recorder, options=fixture) as pair:
                query, header, owner = selected(route, number, pair.default_profile)
                path = route.split(' ', 1)[1] + ('?' + urlencode(query) if query else '')
                kind, messages = ('probe', PROBE_MESSAGES) if rid == 'R051' else ('region', REGION_MESSAGES)
                observer = ScopedCompletion(pair, kind, messages[owner], owner)
                def check(reply):
                    require(reply.headers.get('x-configflow-profile') == owner,
                            'accepted request explicitly names selected query/header/default profile')
                pair.request(rid + '-B' + format(suffix, '02'), route, path,
                    body=json_body({'domains': ['selected.example']} if kind == 'probe' else {'services': True}),
                    headers={'X-ConfigFlow-Profile': header}, expect=202, writes=True, runtime_writes=True,
                    interaction=observer, capture=observer.capture, projection=observer.projection,
                    check=check, variant='P' + str(number) + '/distinct-owned-job-failure')
                foreign = 'other' if owner == 'default' else 'default'
                paths = ['/api/domain-discovery/probe/' + (ident or 'missing') + '?profile=' + foreign for ident in observer.ids]
                pair.request('R052-F09', 'GET /api/domain-discovery/probe/<job_id>', paths=paths,
                    headers={'X-ConfigFlow-Profile': owner}, expect=404, writes=True, interaction=Stable(pair),
                    variant=rid + '/P' + str(number) + '/foreign-profile-cannot-poll-real-job')


def website_selectors(binary, recorder):
    for rid, route, recipes in selector_rows():
        if rid != 'R060':
            continue
        for suffix, number in recipes:
            if number == 3:
                continue
            with Source(b'website selector must not send an Agent request') as source:
                fixture = options()
                fixture['agent_updates'] = {'domain_discovery_enabled': True, 'port': source.server.server_port}
                fixture['other_profile'] = copy.deepcopy(fixture['profile'])
                with Pair(binary, recorder, options=fixture) as pair:
                    heartbeat(pair, variant='website-selector/P' + str(number))
                    query, header, owner = selected(route, number, pair.default_profile)
                    path = route.split(' ', 1)[1] + ('?' + urlencode(query) if query else '')
                    expected = ('selected.example 没有可用 Agent 最近采集的记录' if owner == 'default'
                                else '需要开启域名发现且版本不低于 1.6.0-go 的 Agent')
                    def check(reply):
                        message(expected)(reply)
                        require(reply.headers.get('x-configflow-profile') == owner,
                                'selected owner controls which real Agent admission guard is reached')
                    pair.request(rid + '-B' + format(suffix, '02'), route, path,
                        body=json_body({'domains': ['selected.example']}), headers={'X-ConfigFlow-Profile': header},
                        expect=400, writes=True, interaction=SourceStable(pair, source), check=check,
                        variant='P' + str(number) + '/distinct-owner-website-admission')


GROUPS = {'discovery-job-profile-selectors': async_selectors, 'discovery-website-profile-selectors': website_selectors}
