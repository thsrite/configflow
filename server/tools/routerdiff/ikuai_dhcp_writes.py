"""DHCP edit, readback and compensation through the public release API."""
import copy

from cases import Case, Ref
import dns
import ikuai_dhcp_guards


EDIT = {'peer': 'ikuai', '/body/func_name': 'dhcp_server', '/body/action': 'edit'}
READ = {'peer': 'ikuai', '/body/func_name': 'dhcp_server', '/body/action': 'show'}
READBACK = 'iKuai DHCP DNS 回读校验失败：DNS 或其他 DHCP 配置与提交值不一致'
MISSING = 'iKuai DHCP 目标配置不存在或标识不唯一'


def variants():
    for code in (1001, 1002, 1003, 1008, 3001):
        yield 'refused-' + str(code), {'write_code': code}
    yield 'refused-empty-details', {'write_code': 3001, 'response': {'code': 3001, 'message': '', 'details': []}}
    yield 'refused-mixed-details', {'write_code': 3001, 'response': {'code': 3001, 'message': 'synthetic refusal',
        'details': [None, False, 'ignored', {}, {'field': 'dns2', 'msg': 'synthetic second field'}]}}
    yield 'refused-code-only', {'write_code': 3999, 'response': {'code': 3999}}
    yield 'prechecked-domain-change', {'drift': True}
    yield 'lost-ack', {'lost_ack': True}
    yield 'missing-readback', {'rows': []}
    row = {**dns.ikuai_server(), 'dns1': '10.44.0.53', 'dns2': '10.44.0.53'}
    yield 'duplicate-readback', {'rows': [row, copy.deepcopy(row)]}
    yield 'changed-readback', {'rows': [{**row, 'lease': 121}]}
    yield 'blocked-readback', {'rows': [{**row, 'status': 0}]}
    yield 'readback-unavailable', {'read_errors': 1}
    yield 'recovery-read-unavailable', {'read_errors': 2}
    yield 'rollback-refused', {'rows': [{**row, 'lease': 121}], 'rollback_refused': True}
    yield 'late-verification-failure', {'rows': [], 'read_number': 2, 'late': True, 'entry': 'H0151',
        'message': '回读校验失败：DHCP DNS 的目标网段发生变化'}
    for label, rows in (
        ('multiple', [row, {**row, 'id': 2}]), ('id', [{**row, 'id': 2}]),
        ('network', [{**row, 'addr_pool': '10.44.0.100-10.44.1.200', 'netmask': '255.255.254.0'}]),
        ('outside', [{**row, 'addr_pool': '10.55.0.100-10.55.0.200', 'gateway': '10.55.0.1'}]),
    ):
        yield 'late-scope-' + label, {'rows': rows, 'read_number': 2, 'late': True, 'entry': 'H0151',
            'message': '回读校验失败：DHCP DNS 的目标网段发生变化'}
    yield 'late-blocker', {'rows': [{**row, 'status': 0}], 'read_number': 2, 'late': True, 'entry': 'H0152',
        'message': '回读校验失败：iKuai DHCP 服务未生效或无法确认运行状态'}
    for label, first, second in (('different', '10.44.0.54', '10.44.0.54'), ('inherited', '10.44.0.53', '')):
        yield 'late-dns-' + label, {'rows': [{**row, 'dns1': first, 'dns2': second}],
            'read_number': 2, 'late': True, 'entry': 'H0153', 'message': '回读校验失败：DHCP DNS 与所选服务器不一致'}


def cases(label, spec):
    prefix = 'ikuai-dhcp-write-' + label
    preview = 'R012/' + prefix
    seed = ikuai_dhcp_guards.base()
    # Independent plain-mask release control; the default realistic suffix bug
    # remains covered by its own unmodified-fake world.
    seed['presentation'] = {'static_mask_suffix': False}
    yield Case(preview, 'R012', 'POST', '/api/routers/r-ik/plan', 200,
        prepare=(seed, dns.online()), unchanged=False, effect='device-read',
        checks={'success': True, 'plan/blockers': [], 'plan/checks/dhcp_dns/dns': ['10.44.0.53']})
    body = {'fingerprint': Ref(preview, '/body/plan/fingerprint')}
    controls = []
    entries = []
    join = ''
    if 'write_code' in spec:
        code = spec['write_code']
        entries += ['H0273', 'H0277', 'H0278' if code in (1001, 1002, 1003, 1008) else 'H0279']
        for _ in range(2 if code == 1008 else 1):
            controls.append({'action': 'fault', 'match': EDIT, 'status': 200,
                'body': spec.get('response', {'code': code, 'message': 'synthetic DHCP edit refused',
                         'details': [{'field': 'dns1', 'msg': 'synthetic field refusal'}]})})
    elif spec.get('lost_ack'):
        entries += ['H0273', 'H0277']
        controls.append({'action': 'fault', 'match': EDIT, 'apply': True, 'disconnect': True})
    elif spec.get('drift'):
        join = prefix
        entries += ['H0268']
        controls += [
            # The first DHCP read rebuilds the preview. The next read is the
            # stage's fresh scope check, before the driver's own final check.
            {'action': 'fault', 'match': READ, 'remaining': 2, 'gate': prefix, 'apply': True, 'passthrough': True},
            {'rpc': 'begin', 'ident': join, 'method': 'POST', 'path': '/api/routers/r-ik/apply', 'document': body},
            {'rpc': 'await_gate', 'ident': join, 'gate': prefix},
            {'action': 'seed', 'ikuai': {'dhcp_server': [{**dns.ikuai_server(), 'domain': 'external-edit.example'}]}},
            {'action': 'release', 'gate': prefix},
        ]
    else:
        join = prefix
        controls += [
            {'action': 'fault', 'match': EDIT, 'gate': prefix, 'apply': True, 'passthrough': True},
            {'rpc': 'begin', 'ident': join, 'method': 'POST', 'path': '/api/routers/r-ik/apply', 'document': body},
            {'rpc': 'await_gate', 'ident': join, 'gate': prefix},
        ]
        entries += ['H0264'] if label in ('missing-readback', 'duplicate-readback') else []
        entries += ['H0265'] if label in ('changed-readback', 'blocked-readback', 'rollback-refused') else []
        if spec.get('late'):
            entries += [spec['entry']]
        else:
            entries += ['H0273', 'H0277'] if not spec.get('rollback_refused') else ['H0273', 'H0275', 'H0276']
        if spec.get('read_errors'):
            for _ in range(spec['read_errors']):
                controls.append({'action': 'fault', 'match': READ, 'status': 503,
                                 'body': {'message': 'synthetic DHCP read unavailable'}})
            if spec['read_errors'] == 2:
                entries += ['H0274']
        else:
            rows = spec['rows']
            controls.append({'action': 'fault', 'match': READ, 'remaining': spec.get('read_number', 1),
                'status': 200, 'body': {'code': 0, 'results': {'total': len(rows), 'data': rows}}})
        if spec.get('rollback_refused'):
            controls.append({'action': 'fault', 'match': {**EDIT, '/body/param/dns1': '10.44.0.54'},
                'status': 200, 'body': {'code': 3001, 'message': 'synthetic DHCP rollback refused'}})
        controls.append({'action': 'release', 'gate': prefix})
    checks = {'success': False, 'result/ok': False, 'result/failed_stage': 'dhcp_dns'}
    if spec.get('drift'):
        checks['result/log/-1/summary'] = 'iKuai DHCP 配置已变化，请重新预览'
    if label in ('missing-readback', 'duplicate-readback'):
        checks['result/log/-1/summary'] = MISSING
    elif label in ('changed-readback', 'blocked-readback'):
        checks['result/log/-1/summary'] = READBACK
    if spec.get('rollback_refused'):
        checks['result/rollback/0/status'] = 'failed'
        checks['result/log/-1/summary'] = READBACK + '；DHCP DNS 局部撤销失败：iKuai dhcp_server edit 失败：synthetic DHCP rollback refused'
    elif spec.get('late'):
        checks['result/rollback/0/stage'] = 'dhcp_dns'
        checks['result/rollback/0/status'] = 'ok'
        checks['result/log/-1/summary'] = spec['message']
    if 'write_code' in spec:
        summaries = {
            1001: 'iKuai 账号没有 dhcp_server 的访问权限（错误码 1001）。请在 iKuai 的用户组中授予读取权限，或改用管理员账号。',
            1002: 'iKuai 账号没有 dhcp_server 的执行权限（错误码 1002）。请在 iKuai 的用户组中授予修改权限，或改用管理员账号。',
            1003: 'iKuai 不提供 dhcp_server 接口（错误码 1003：synthetic DHCP edit refused）。当前固件可能不支持这项功能，请确认 iKuai 版本。',
            1008: 'iKuai 登录会话已失效（错误码 1008），重新登录后仍未恢复，请稍后重试。',
        }
        if spec['write_code'] in summaries:
            checks['result/log/-1/summary'] = summaries[spec['write_code']]
        elif label == 'refused-empty-details':
            checks['result/log/-1/summary'] = 'iKuai dhcp_server edit 失败：3001'
        elif label == 'refused-mixed-details':
            checks['result/log/-1/summary'] = 'iKuai dhcp_server edit 失败：synthetic refusal（None: None；dns2: synthetic second field）'
        elif label == 'refused-code-only':
            checks['result/log/-1/summary'] = 'iKuai dhcp_server edit 失败：3999'
        else:
            checks['result/log/-1/summary'] = 'iKuai dhcp_server edit 失败：synthetic DHCP edit refused（dns1: synthetic field refusal）'
    yield Case('R011/' + prefix, 'R011', 'POST', '/api/routers/r-ik/apply', 502, tuple(entries),
        document=body, prepare=tuple(controls), join=join, requires=('R012',),
        unchanged=False, effect='profile-write-device', checks=checks,
        foreign=(('ikuai', 'dhcp_static', 'id', 70),),
        stored={'routers/items/1/last_release/ok': False, 'routers/items/1/last_release/failed_stage': 'dhcp_dns'},
        fake_checks={'ikuai/dhcp_server/0': {**dns.ikuai_server(),
            **({'domain': 'external-edit.example'} if spec.get('drift') else {}),
            **({'dns1': '10.44.0.53', 'dns2': '10.44.0.53'} if spec.get('rollback_refused') else {})}},
        fake_lengths={'ikuai/static_rt': 0, 'ikuai/nat_rule': 0, 'ikuai/route_object': 0, 'ikuai/dhcp_static': 1},
        expected_requests=({'match': EDIT, 'count': 0 if spec.get('drift') else
            2 if spec.get('write_code') == 1008 or 'write_code' not in spec else 1},),
        request_order=(READ,) if spec.get('drift') else (EDIT,) if 'write_code' in spec else (EDIT, READ, EDIT))


def worlds():
    return [('dhcp-ikuai-write-' + label, dns.ikuai_options(),
             (lambda label=label, spec=spec: cases(label, spec),)) for label, spec in variants()]
