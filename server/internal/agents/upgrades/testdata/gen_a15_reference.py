"""Reference values for A15 from the real Python modules (run from the repo root):

PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/upgrades/testdata/gen_a15_reference.py \
    > server/internal/agents/upgrades/testdata/a15_reference.json
"""
import json

from backend.agents import address_follow

inputs = [
    '192.168.0.10', '10.0.0.1', '172.16.0.1', '172.17.0.2', '172.31.255.255', '172.32.0.1', '172.15.1.1',
    '127.0.0.1', '127.255.255.254', '169.254.1.1', '169.253.1.1', '0.0.0.0', '0.0.0.1', '224.0.0.1', '239.255.255.255',
    '240.0.0.1', '255.255.255.255', '223.255.255.255', '100.64.0.1', '192.0.2.11', '198.51.100.1',
    '01.2.3.4', '1.2.3.04', '1.2.3.4 ', ' 1.2.3.4', '1.2.3', '1.2.3.4.5', '256.1.1.1', '1.2.3.4/32', '1.2.3.4%eth0',
    '１.2.3.4', '1.2.3.٤', '1..3.4', '', 'fe80::1', '::ffff:1.2.3.4', 'dns.example', '1.2.3.4:80', '+1.2.3.4',
    '1.2.3.0x4', '0000.1.2.3', '000.1.2.3', '0.0.0.00', '1.2.3.4\n', None, 12, True, [], {},
]
usable = [{'input': value, 'output': address_follow.usable(value)} for value in inputs]

def error(fn):
    try:
        fn()
    except Exception as exc:
        return type(exc).__name__ + ': ' + str(exc)
    return None

errors = {
    'str_index': error(lambda: 'abc logs'['logs']),
    'list_index': error(lambda: ['logs']['logs']),
    'none_in': error(lambda: 'logs' in None),
    'int_in': error(lambda: 'logs' in 5),
    'float_in': error(lambda: 'logs' in 5.5),
    'bool_in': error(lambda: 'logs' in True),
    'list_get': error(lambda: [].get('success')),
    'int_iter': error(lambda: [v for v in 5]),
    'bool_iter': error(lambda: [v for v in True]),
}
print(json.dumps({'usable': usable, 'errors': errors}, ensure_ascii=False, indent=1))
