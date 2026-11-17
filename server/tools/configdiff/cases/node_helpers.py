"""Public node worker failures, bounded settings and candidate selection."""
from harness import Pair, json_body
from probe import Exchange
from cases.config_backup import message
from cases.nodes import NODE
from cases.setup import Stable

ROUTE = 'POST /api/nodes/latency'


class WorkerStable(Stable):
    def __init__(self, pair, count=1):
        super().__init__(pair)
        self.worker = Exchange(pair, expected_count=count)

    def before(self, index):
        super().before(index)
        self.worker.before(index)

    def after(self, index, reply):
        super().after(index, reply)
        self.worker.after(index, reply)

    def compare(self, policy):
        return super().compare(policy) + self.worker.compare(policy)


def worker_failures(binary, recorder):
    invalid = '测速组件返回了无效结果，请检查组件版本'
    rows = [('H728', 'bad-executable', '测速组件无法启动，请检查可执行权限和 CPU 架构'),
            ('H729', 'exit-3', '节点测速 DNS 设置无效，请检查服务器地址及引导 DNS'),
            ('H730', 'exit-1', '测速组件异常退出，请检查组件版本后重试'),
            ('H731', 'version', '测速组件版本不匹配，请更新 ConfigFlow 镜像；源码部署请重新构建 backend/node-probe')]
    rows += [(case, mode, invalid) for case, modes in [
        ('H732', ['results-type']), ('H733', ['missing-result', 'wrong-result']),
        ('H734', ['latency-zero', 'latency-negative', 'latency-bool', 'latency-float', 'latency-string']),
        ('H735', ['latency-missing-error']),
        ('H736', ['invalid-json', 'node-no-version', 'node-no-results', 'node-no-name', 'node-array'])] for mode in modes]
    for case, mode, text in rows:
        with Pair(binary, recorder, options={'shared': {'nodes': [NODE]}, 'probe_case': mode}) as pair:
            if mode == 'bad-executable':
                (pair.root / 'synthetic-probe').write_bytes(b'synthetic invalid executable format\n')
            pair.request(case, ROUTE, body='{}', expect=503, writes=True, check=message(text),
                         interaction=WorkerStable(pair, 0 if mode == 'bad-executable' else 1), variant='node-worker/' + mode)


def settings_guards(binary, recorder):
    text = '节点测速设置无效，请检查测试地址及超时（500–10000 毫秒）'
    rows = [('H721', 'CONFIGFLOW_NODE_TEST_URL', value) for value in
            ['file:///synthetic', 'https:///missing', 'https://user:synthetic@probe.example/', 'https://[broken']]
    rows += [('H722', 'CONFIGFLOW_NODE_TEST_TIMEOUT_MS', value) for value in ['499', '10001']]
    rows += [('H723', 'CONFIGFLOW_NODE_TEST_TIMEOUT_MS', value) for value in ['synthetic', '1.5', '']]
    for case, key, value in rows:
        with Pair(binary, recorder, options={'environment': {key: value}, 'probe_case': 'valid', 'shared': {'nodes': [NODE]}}) as pair:
            pair.request(case, ROUTE, body='{}', expect=503, writes=True, check=message(text),
                         interaction=WorkerStable(pair, 0), variant='node-settings/' + key + '/' + value)
    with Pair(binary, recorder, options={'shared': {'nodes': [NODE]}, 'probe_case': 'valid',
              'system_config': {'node_test_dns_config': {'mode': 'invalid'}}}) as pair:
        pair.request('H724', ROUTE, body='{}', expect=503, writes=True,
                     check=message('请选择系统 DNS 或自定义 DNS'), interaction=WorkerStable(pair, 0), variant='invalid-persisted-dns')
    with Pair(binary, recorder, options={'shared': {'nodes': [NODE]}}) as pair:
        pair.request('H725', ROUTE, body='{}', expect=503, writes=True, interaction=Stable(pair),
            check=message('独立测速组件未安装，请更新 ConfigFlow 镜像；源码部署请先构建 backend/node-probe'), variant='missing-binary')


GROUPS = {'node-helper-worker-failures': worker_failures, 'node-helper-settings-guards': settings_guards}
