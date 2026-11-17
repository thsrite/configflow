"""H099–H105: actual external worker failures and strict DNS stdout schema."""
import time

from compare import Difference, database, files
from harness import Pair, json_body
from probe import Exchange
from cases.config_backup import message
from cases.subscriptions import require

ROUTE = 'POST /api/nodes/latency/dns/check'


def helper_failures(binary, recorder):
    invalid = 'DNS 检测组件返回了无效结果，请检查组件版本'
    vectors = [('H099', 'bad-executable', '测速组件无法启动，请检查可执行权限和 CPU 架构'),
        ('H100', 'exit-1', 'DNS 检测组件异常退出，请检查组件版本后重试'),
        ('H100', 'exit-3', 'DNS 检测组件异常退出，请检查组件版本后重试'),
        ('H101', 'version', '测速组件版本不匹配，请更新 ConfigFlow 镜像；源码部署请重新构建 backend/node-probe')]
    vectors += [(case, mode, invalid) for case, modes in [
        ('H102', ['dns-success-type', 'dns-addresses-type', 'dns-address-number']),
        ('H103', ['dns-bad-latency', 'dns-negative-latency', 'dns-string-latency']),
        ('H104', ['dns-empty', 'dns-fake-ip', 'dns-fake-ip-upper']),
        ('H105', ['invalid-json', 'dns-array', 'dns-no-version', 'dns-no-result', 'dns-invalid-ip'])] for mode in modes]
    for case, mode, text in vectors:
        with Pair(binary, recorder, options={'probe_case': mode}) as pair:
            if mode == 'bad-executable':
                # Remains executable and discoverable; execve itself must fail,
                # rather than the earlier missing/not-executable guard.
                (pair.root / 'synthetic-probe').write_bytes(b'synthetic invalid executable format\n')
            before = database(pair.dirs[0]), files(pair.dirs[0])
            def check(reply):
                message(text)(reply)
                require((database(pair.dirs[0]), files(pair.dirs[0])) == before,
                        'worker failure exposes only the exact safe hint and never commits settings or a cache')
            pair.request(case, ROUTE, body=json_body({'dns': {'mode': 'custom', 'servers': ['192.0.2.53']}}),
                expect=503, writes=True, check=check, interaction=Exchange(pair, expected_count=0 if mode == 'bad-executable' else 1),
                variant='worker-schema/' + mode)


def boundary_inputs(binary, recorder):
    with Pair(binary, recorder, options={'probe_case': 'valid'}) as pair:
        for case, dns in [('R099-B01', {'mode': 'system', 'servers': []}),
            ('R099-B02', {'mode': 'custom', 'servers': ['192.0.2.54:015353', '192.0.2.53:53', '192.0.2.54:15353'],
                          'bootstrap': ['192.0.2.55:53', '192.0.2.56']})]:
            before = database(pair.dirs[0]), files(pair.dirs[0])
            observer = Exchange(pair)
            def check(reply):
                require(reply.json() == {'success': True, 'addresses': ['192.0.2.10', '192.0.2.11'], 'latency_ms': 7,
                                          'message': '解析成功，返回真实 IP'}, 'worker addresses deduplicate without losing first occurrence and integer latency')
                payload = observer.inputs[0][0]
                require(payload == {'operation': 'dns_check', 'timeout_ms': 5000,
                    'dns_servers': [] if dns['mode'] == 'system' else ['192.0.2.54:15353', '192.0.2.53'],
                    'dns_bootstrap': [] if dns['mode'] == 'system' else ['192.0.2.55', '192.0.2.56']},
                    'external process receives exact normalized ordered DNS/bootstrap and production timeout')
                require((database(pair.dirs[0]), files(pair.dirs[0])) == before, 'DNS check does not save the draft or write cache')
            pair.request(case, ROUTE, body=json_body({'dns': dns}), expect=200, writes=True,
                check=check, interaction=observer, variant='worker-ordered-inputs')
    for index, dns in enumerate([{'mode': 'invalid'}, {'mode': 'custom', 'servers': []}, {'mode': 'custom', 'servers': 'bad'}]):
        with Pair(binary, recorder, options={'system_config': {'node_test_dns_config': dns}}) as pair:
            before = database(pair.dirs[0]), files(pair.dirs[0])
            pair.request('R100-F08', 'GET /api/nodes/latency/settings', expect=400, writes=True,
                check=lambda r: require((database(pair.dirs[0]), files(pair.dirs[0])) == before, 'invalid durable DNS settings are reported without silently repairing them'),
                variant='invalid-persisted/' + str(index))


def returned_failures(binary, recorder):
    texts = {'config': 'DNS 设置无效，请检查地址和引导 DNS', 'dns_bootstrap': '引导 DNS 无法解析上游地址，请检查引导 DNS',
             'dns_fake_ip': 'DNS 返回了 Fake-IP，请改用返回真实 IP 的解析路径', 'timeout': 'DNS 查询超时，请检查网络和服务状态',
             'tls': 'DNS TLS 验证失败，请检查上游地址和证书', 'dns': 'DNS 未返回有效地址，请检查解析路径'}
    for code in [*texts, 'synthetic-unknown']:
        with Pair(binary, recorder, options={'probe_case': 'dns-error-' + code}) as pair:
            expected_code = code if code in texts else 'dns'
            pair.request('R099-S02', ROUTE, body=json_body({'dns': {'mode': 'system'}}), expect=200, writes=True,
                check=lambda r: require(r.json() == {'success': False, 'addresses': [], 'latency_ms': 7,
                    'error_code': expected_code, 'message': texts[expected_code]}, 'reported DNS failure remains HTTP200 with exact safe code/message, unknown code falls back to dns'),
                interaction=Exchange(pair), variant='reported-failure/' + code)


GROUPS = {'dns-worker-helper-failures': helper_failures, 'dns-worker-boundary-inputs': boundary_inputs,
          'dns-worker-returned-failures': returned_failures}


class ProbeDeadline(Exchange):
    def __init__(self, pair):
        super().__init__(pair)
        self.elapsed = [None, None]

    def before(self, index):
        super().before(index)
        self.started = time.monotonic()

    def after(self, index, reply):
        self.elapsed[index] = time.monotonic() - self.started
        if index == 0:
            require(type(self.elapsed[0]) in (int, float) and 9 <= self.elapsed[0] <= 13,
                    'Python kills the actual sleeping worker at its unmodified 10-second production deadline')
        super().after(index, reply)

    def compare(self, policy):
        value = self.elapsed[1]
        out = super().compare(policy)
        if type(value) not in (int, float) or not 9 <= value <= 13:
            out += [Difference('probe.dns.elapsed', 'outside-production-worker-deadline', '9–13 seconds around 10s', value)]
        return out


def production_deadline(binary, recorder):
    with Pair(binary, recorder, options={'probe_case': 'timeout'}) as pair:
        before = database(pair.dirs[0]), files(pair.dirs[0])
        def check(reply):
            require(reply.json() == {'success': False, 'addresses': [], 'error_code': 'timeout',
                'message': 'DNS 查询超时，请检查网络和服务状态'}, 'a real killed worker timeout is HTTP200 with no invented latency field')
            require((database(pair.dirs[0]), files(pair.dirs[0])) == before, 'timed out worker never writes cache or settings')
        pair.request('T14', ROUTE, body=json_body({'dns': {'mode': 'system'}}), expect=200, writes=True,
                     check=check, interaction=ProbeDeadline(pair), variant='actual-10-second-worker-timeout')


def success_forms(binary, recorder):
    prefix = 'https://192.0.2.53/'
    vectors = [('four-servers-and-bootstrap', {'mode': 'custom',
        'servers': ['192.0.2.53', '192.0.2.54:15353', 'https://dns.example/dns-query', 'tls://dns.example:853'],
        'bootstrap': ['192.0.2.55:53']},
        ['192.0.2.53', '192.0.2.54:15353', 'https://dns.example/dns-query', 'tls://dns.example'], ['192.0.2.55']),
        ('2048-character-DoH', {'mode': 'custom', 'servers': [prefix + 'x' * (2048 - len(prefix))]},
         [prefix + 'x' * (2048 - len(prefix))], [])]
    with Pair(binary, recorder, options={'probe_case': 'valid'}) as pair:
        for variant, dns, servers, bootstrap in vectors:
            observer = Exchange(pair)
            before = database(pair.dirs[0]), files(pair.dirs[0])
            def check(reply):
                require(reply.json()['success'] is True and observer.inputs[0] == [{'operation': 'dns_check', 'timeout_ms': 5000,
                    'dns_servers': servers, 'dns_bootstrap': bootstrap}], 'every supported upstream is passed to the actual executable in exact stable normalized order')
                require((database(pair.dirs[0]), files(pair.dirs[0])) == before, 'maximum valid DNS draft remains unsaved')
            pair.request('T14', ROUTE, body=json_body({'dns': dns}), expect=200, writes=True, check=check,
                         interaction=observer, variant=variant)


GROUPS.update({'dns-worker-production-deadline': production_deadline, 'dns-worker-success-forms': success_forms})
