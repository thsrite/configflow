"""Synthetic executable implementing the public node-probe stdin/stdout protocol."""
import json
import os
from pathlib import Path
import sys
import time


def install(root, python, mode):
    binary = Path(root) / 'synthetic-probe'
    # A real child process is spawned by each backend through its documented env.
    # This file never resolves DNS or sends network traffic.
    binary.write_text('#!' + python + '\n' + Path(__file__).read_text() + '\n')
    binary.chmod(0o755)
    return str(binary)


class Exchange:
    def __init__(self, pair, expected_count=1):
        self.path = pair.root / 'probe-input.jsonl'
        self.inputs = []
        self.expected_count = expected_count

    def before(self, index):
        self.path.write_text('')

    def after(self, index, reply):
        values = [json.loads(line) for line in self.path.read_text().splitlines()]
        if index == 0 and len(values) != self.expected_count:
            from harness import DesignError
            raise DesignError('Python did not invoke expected number of fake probe processes')
        self.inputs.append(values)

    def compare(self, policy):
        from compare import differences
        return differences(*self.inputs, path='probe.stdin', policy=policy)


def main():
    value = json.load(sys.stdin)
    with open(os.environ['CONFIGDIFF_PROBE_INPUT'], 'a') as out:
        out.write(json.dumps(value, ensure_ascii=False) + '\n')
    mode = os.environ.get('CONFIGDIFF_PROBE_CASE', 'valid')
    if mode == 'timeout':
        time.sleep(20)
    if mode in ('exit-1', 'exit-3'):
        print('synthetic secret must not leak', file=sys.stderr)
        return int(mode[-1])
    if mode == 'invalid-json':
        print('{invalid')
        return 0
    if value.get('operation') == 'dns_check':
        response = {'version': 4, 'dns_check': {'success': True, 'addresses': ['192.0.2.10', '192.0.2.10', '192.0.2.11'], 'latency_ms': 7}}
        if mode == 'dns-empty':
            response['dns_check']['addresses'] = []
        elif mode == 'dns-fake-ip':
            response['dns_check']['addresses'] = ['198.18.0.1']
        elif mode == 'dns-invalid-ip':
            response['dns_check']['addresses'] = ['not-an-ip']
        elif mode == 'dns-bad-latency':
            response['dns_check']['latency_ms'] = True
        elif mode == 'dns-failure':
            response['dns_check'].update(success=False, addresses=[], error_code='tls')
    else:
        response = {'version': 4, 'results': [{'name': n['name'], 'latency': 7} for n in value.get('nodes', [])]}
        if mode == 'missing-result':
            response['results'] = []
        elif mode == 'results-type':
            response['results'] = {}
        elif mode in ('latency-zero', 'latency-bool', 'latency-missing-error'):
            response['results'][0]['latency'] = {'latency-zero': 0, 'latency-bool': True, 'latency-missing-error': None}[mode]
    if mode == 'version':
        response['version'] = 3
    print(json.dumps(response, ensure_ascii=False))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
