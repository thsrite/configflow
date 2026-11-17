"""Wizard finish rechecks missing forwarded rules against actual HTTP/cache."""
import copy

from compare import database, differences, files
from composition import cache_name
from sources import Source
from subscriptions import Exchange
from cases.config_backup import message
from cases.setup import ACTION, PATH, PID, call, row, session
from cases.setup_completion import prepared, publication, StatusExchange
from cases.subscriptions import require


class ForwardedRead:
    def __init__(self, pair, agents, source, *, new_cache=None):
        self.pair, self.source, self.new_cache = pair, source, new_cache
        self.status = StatusExchange(pair, agents)
        self.wire = Exchange(pair, source)
        self.violations = [None, None]

    def before(self, index):
        self.status.before(index)
        self.wire.before(index)
        self.before_values = database(self.pair.dirs[index]), files(self.pair.dirs[index])

    def after(self, index, reply):
        self.status.after(index, reply)
        self.wire.after(index, reply)
        expected = copy.deepcopy(self.before_values[1])
        if self.new_cache:
            expected[self.new_cache] = {'mode': 0o600, 'content': self.source.body}
        self.violations[index] = [vars(item) for item in (
            differences(self.before_values[0], database(self.pair.dirs[index]), 'forwarded.SQL')
            + differences(expected, files(self.pair.dirs[index]), 'forwarded.files'))]
        if index == 0:
            require(not self.violations[0], 'failed finish only writes its prescribed new rule cache: ' + repr(self.violations[0][:4]))

    def compare(self, policy):
        return (self.status.compare(policy) + self.wire.compare(policy)
            + differences(*self.violations, 'setup.forwarded.storage-proof', policy))


def missing(binary, recorder):
    with Source(b'198.51.100.0/24\n') as source, prepared(binary, recorder, 'router', 'forwarded-rules') as (pair, agents, outbound):
        call(pair, 'R202-S03', 'POST /api/settings/rule-fetch-proxy', body={'rule_fetch_proxy': ''},
            variant='forwarded-rules/remove-synthetic-blocking-proxy', interaction=Exchange(pair, outbound, expected=0))
        library = {'id': 'setup-forwarded-library', 'name': '待下载的转发规则🧪', 'source_type': 'url',
            'url': source.url + '/forwarded-ip', 'behavior': 'ipcidr', 'format': 'text', 'enabled': True, 'update_interval': 0}
        call(pair, 'R165-S03', 'POST /api/rule-library', body=library,
            variant='forwarded-rules/real-create-and-download', interaction=Exchange(pair, source))
        rule_id = 'setup-forwarded-rule'
        call(pair, 'R179-S01', 'POST /api/rule-sets', '/api/rule-sets?profile=' + PID,
            body={'id': rule_id, 'library_rule_id': library['id'], 'policy': 'DIRECT'},
            variant='forwarded-rules/real-profile-reference')
        mihomo = copy.deepcopy(row(pair)['mihomo'])
        mihomo['router_split']['route_rule_set_ids'] = [rule_id]
        call(pair, 'R137-S01', 'POST /api/profiles/<profile_id>/import', '/api/profiles/' + PID + '/import',
            body={'mihomo': mihomo}, variant='forwarded-rules/real-full-TUN-forwarding-selection')
        # Compatibility/publication receipt is an explicit synthetic boundary,
        # as in the existing completion fixtures; no Agent release is claimed.
        publication(pair, agents)
        cached = cache_name(library)
        for directory in pair.dirs:
            for suffix in ('', '.bak'):
                (directory / (cached + suffix)).unlink(missing_ok=True)
        source.status = 403
        baseline = database(pair.dirs[0])
        def checked(reply):
            value = reply.json()
            require(value['route_instructions']['incomplete'] is True
                and value['route_instructions']['missing_rule_sets'] == [library['name']]
                and value['step'] == value['max_step'] == 4 and database(pair.dirs[0]) == baseline,
                'real failed source read leaves the checked static route table explicitly incomplete')
        # The check persists its session normally; the finish failures below
        # must retain that exact previously confirmed session file.
        class Checked(ForwardedRead):
            def after(self, index, reply):
                self.status.after(index, reply)
                self.wire.after(index, reply)
            def compare(self, policy):
                return self.status.compare(policy) + self.wire.compare(policy)
        call(pair, 'R211-S10', ACTION, PATH + '/check', body={}, check=checked,
            interaction=Checked(pair, agents, source), variant='forwarded-rules/real-403-shown-missing')
        body = {'ack_routes': True, 'ack_dns': True}
        call(pair, 'H1001', ACTION, PATH + '/finish', body=body, expect=409,
            check=message('主路由转发规则集「' + library['name'] + '」的 IP 段仍未下载，静态路由说明不完整。'
                '请检查 ConfigFlow 服务器能否访问规则地址后重新检查。'),
            interaction=ForwardedRead(pair, agents, source), variant='still-missing-at-real-finish-recheck')
        source.status = 200
        call(pair, 'H1002', ACTION, PATH + '/finish', body=body, expect=409,
            check=message('主路由转发规则集「' + library['name'] + '」的 IP 段已下载，静态路由表有新增网段。'
                '请重新检查，按更新后的列表配置主路由后再确认。'),
            interaction=ForwardedRead(pair, agents, source, new_cache=cached),
            variant='download-now-succeeds-but-browser-confirmed-old-incomplete-table')
        require(session(pair)['route_instructions']['incomplete'] is True,
            'rejected finish does not pretend the browser saw its newly downloaded ranges')


GROUPS = {'setup-finish-real-forwarded-rule-download-guards': missing}
