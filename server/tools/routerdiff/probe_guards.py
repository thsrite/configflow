"""Normal probe controls and late list-row refusal, using actual device HTTP."""
from probes import probe, fault


def controls():
    capabilities = ['routes', 'nat_bypass', 'address_sets']
    for label, version in (('minimum', '7.1'), ('unicode', '7.24.5（合成）'), ('major', '8.0'),
                           ('zeroes', '07.01'), ('rc', '7.1rc1')):
        yield probe('guard-version-' + label, 'routeros',
            (fault('ros', '/rest/system/resource', status=200, body={'version': version, 'board-name': '合成型号🧪'}),),
            ok=True, checks={'probe/version': version, 'probe/model': '合成型号🧪', 'probe/capabilities': capabilities},
            entries=('H0835', 'H0841', 'H0842', 'H0843', 'R017-B04'))
    for label, version in (('minimum', '4.0.0'), ('unicode', '4.0.314（合成）')):
        yield probe('guard-version-' + label, 'ikuai',
            ({'action': 'fault', 'match': {'peer': 'ikuai', 'path': '/Action/call', '/body/func_name': 'sysstat'},
              'status': 200, 'body': {'Result': 10000, 'code': 0,
                'results': {'verinfo': {'version': version, 'verstring': '合成爱快型号🧪'}}}},),
            ok=True, checks={'probe/version': version, 'probe/model': '合成爱快型号🧪',
                             'probe/capabilities': capabilities + ['policy_routes']},
            entries=('H0241', 'H0242', 'R017-B04'))


def late_rows():
    capabilities = [('/rest/ip/route', 'routes'), ('/rest/ip/firewall/nat', 'nat_bypass'),
                    ('/rest/ip/firewall/address-list?list=cf-probe', 'address_sets')]
    for path, absent in capabilities:
        yield probe('guard-late-row-' + absent, 'routeros',
            (fault('ros', path, status=200, body=[{}, None]),), ok=True,
            checks={'probe/capabilities': [name for _, name in capabilities if name != absent]},
            entries=('H0835', 'H0843', 'R017-S03', 'R017-F13'))


def invalid_versions():
    values = (('null', None, '未知'), ('false', False, '未知'), ('true', True, 'True'),
              ('integer', 1, '1'), ('number', 1.5, '1.5'), ('empty-array', [], '未知'),
              ('array', ['wrong'], "['wrong']"), ('empty-object', {}, '未知'),
              ('object', {'unexpected': 'value'}, "{'unexpected': 'value'}"))
    for vendor in ('routeros', 'ikuai'):
        for label, value, shown in values:
            if vendor == 'routeros':
                controls = (fault('ros', '/rest/system/resource', status=200, body={'version': value}),)
                message, entry = '需要 RouterOS 7.1+，当前版本 ' + shown, 'H0842'
            else:
                controls = ({'action': 'fault', 'match': {'peer': 'ikuai', 'path': '/Action/call', '/body/func_name': 'sysstat'},
                             'status': 200, 'body': {'Result': 10000, 'code': 0,
                               'results': {'verinfo': {'version': value}}}},)
                message, entry = '只支持 iKuai 4.x，当前版本 ' + shown, 'H0241'
            yield probe('guard-invalid-version-' + label, vendor, controls, message,
                        entries=(entry,))
