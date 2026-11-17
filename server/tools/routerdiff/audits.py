"""Explicit variant audits, separate from inventory parsing and recipe execution.

An entry has no completed audit unless it appears here with source evidence and
every named required variant completed its real Python HTTP assertions. A report
with only another world's similarly named recipe never satisfies a requirement.
This is test coverage, not a claim that an unregistered Go route was compared.
"""
from dataclasses import dataclass

from inventory import load


@dataclass(frozen=True)
class Audit:
    sources: tuple
    rationale: str
    requirements: tuple


def declared():
    routes, _ = load()
    audits = {}
    variants = ('missing header', 'wrong signature', 'expired JWT', 'empty claims',
                'double space Bearer', '8193-byte credential', 'valid Agent capability')
    labels = ('config-token', 'download-token', 'agent-token', 'admin-jwt-query', 'device-password', 'config-cookie')
    for route in routes:
        for number, description in enumerate(variants, 1):
            ident = route + '-F' + format(number, '02')
            required = [('guards', ident + '/A' + str(number), 401)]
            if number == 1:
                required += [('guards', route + '/no-admin-header-' + label, 401) for label in labels]
            audits[ident] = Audit(
                ('backend/routes/auth.py:35', 'backend/common/auth.py:23', 'server/parity/difftest-routers.md:20'),
                'A' + str(number) + ': ' + description + '; valid alpha/default profile, authentication enabled. '
                'Every refusal checks full DB/files/device state unchanged. A1 also includes all six alternative '
                'query/header/cookie credentials. Agent capability belongs to the publicly seeded auth-agent, '
                'not an invented invalid token; its heartbeat acceptance is separately tested over real public HTTP. '
                'The auth world itself makes no preparatory mutation. Authentication-off successes remain a separate fixture audit.',
                tuple(required))
    profile_sources = ('backend/common/profile_context.py:53', 'backend/common/profile_context.py:93',
                       'backend/routes/auth.py:35', 'server/parity/difftest-routers.md:222')
    for route in routes:
        if route == 'R001':
            continue
        for suffix, status in (('F08', 404), ('F09', 400)):
            ident = route + '-' + suffix
            audits[ident] = Audit(profile_sources,
                'Authenticated invalid/missing profile, each of query profile, query profile_id and header separately. '
                'No handler/device/DB write. Combined selector precedence is a different X02 audit, still pending.',
                tuple(('guards', ident + '/' + selector, status) for selector in ('profile', 'profile_id', 'header')))
        ident = route + '-F90'
        audits[ident] = Audit(profile_sources,
            'Profile middleware rejects before authentication: absent bearer with each invalid/missing selector. '
            'All six concrete combinations and unchanged full state are required.',
            tuple(('guards', ident + '/' + suffix + '/' + selector, status)
                  for suffix, status in (('F08', 404), ('F09', 400)) for selector in ('profile', 'profile_id', 'header')))
    global_sources = (*profile_sources, 'backend/routes/upstream_router_pending.py:20', 'backend/routers/pending.py:165')
    for ident, case, status in (
        ('R001-F13', 'R001-F13/explicit', 400), ('R001-F14', 'R001-F14/explicit', 404),
        ('R001-F90', 'R001-F90/no-bearer-missing-profile', 401),
        ('R001-B90', 'R001-B90/empty-explicit-profile', 400),
    ):
        audits[ident] = Audit(global_sources,
            'Global agents blueprint ignores request profile selectors before auth; only explicit query profile '
            'is validated inside the authenticated upstream handler. Original R001-F08/F09 stay unclosed, '
            'pending their explicit O6 supersession audit; no invalid global-guard assumption is reused.',
            (('guards', case, status),))
    audits['R001-F91'] = Audit(global_sources,
        'Header and profile_id cannot select scope on a global agents route. Two empty-linked variants plus '
        'a genuinely linked alpha/beta case with invalid header prove it; all semantic response headers remain compared.',
        (('runtime-reads', 'R001-F91/global-0', 200), ('runtime-reads', 'R001-F91/global-1', 200),
         ('upstream-linked', 'R001/linked-header-does-not-scope', 200)))
    # Explicit reviewed input positions, not "whatever cases currently claim H".
    # The integrity test fails if a later input insertion renumbers these cases.
    def settings_inputs(groups, field='dns_guard', reset=False, ikuai=True):
        required = []
        for route in ('R008', 'R010'):
            for start, count in groups:
                for offset in range(count):
                    label = str(start + offset) + '-' + field + '-' + str(0 if reset else offset)
                    required.append(('guards', route + '/' + label, 400))
                    if ikuai:
                        required.append(('guards', route + '/iKuai-shared-' + label, 400))
        return tuple(required)
    def vendor_inputs(label):
        return tuple(('guards', case, 400) for case in (
            'R008/' + label, 'R010/' + label, 'R010/' + label + '-existing-iKuai'))
    settings = {
        'H0154': settings_inputs(((67, 5),)),
        'H0155': settings_inputs(((72, 6), (78, 6), (84, 6))),
        'H0156': settings_inputs(((90, 10),)),
        'H0157': settings_inputs(((100, 7),), reset=True),
        'H0158': settings_inputs(((100, 7),), reset=True),
        'H0159': settings_inputs(((107, 1),)),
        'H0160': vendor_inputs('iKuai-cannot-enable-DNS-guard'),
        'H0161': settings_inputs(((108, 1),), ikuai=False),
        'H0491': settings_inputs(((109, 8),), field='ipv6_pd_guard'),
        'H0492': vendor_inputs('iKuai-cannot-enable-PD-guard'),
    }
    settings['H1243'] = settings['H1244'] = tuple(sorted(set(
        required for ident in ('H0154', 'H0155', 'H0156', 'H0157', 'H0158', 'H0159', 'H0160', 'H0161')
        for required in settings[ident])))
    settings['H1246'] = settings['H1247'] = tuple(sorted(set(settings['H0491'] + settings['H0492'])))
    for ident, required in settings.items():
        audits[ident] = Audit(
            ('backend/routers/dns_guard.py:48', 'backend/routers/ipv6_pd_guard.py:116', 'backend/routers/store.py:103'),
            'Only public create/update enter these settings validators. The original H route-scope column omitted '
            'R008/R010; it is not proof that report/plan reaches the guard. Shape, all three boolean fields, '
            'non-string bypass members, empty/whitespace/CJK/fullwidth/IPv6/bad-prefix strings, pre-dedup65 cap '
            'and both vendors are explicit. Missing-DNS rejection is RouterOS-only: iKuai stops at the preceding '
            'unsupported-vendor guard. Vendor tests cover creation, type-changing edit and existing iKuai edit. '
            'The parent catch/rethrow entries require every callee error family, not merely one400.',
            required)
    # Client guard evidence uses exact source-reviewed values rather than
    # collecting every recipe that happens to claim a helper identifier.
    invalid_ranges = {
        'H0071': (None, 0, False, [], {}),
        'H0072': ('', 'bad', '10.44.0.1/1/2', '10.44.0.1%lo', '地址', '１０.４４.０.１'),
        'H0073': ('10.44.0.1/', '10.44.0.1/-1', '10.44.0.1/+1', '10.44.0.1/3a',
                  '10.44.0.1/0001', '10.44.0.1/ 24', '10.44.0.1/２４'),
        'H0074': ('10.44.0.1/33', 'fd42:cf::1/129'),
        'H0075': ('::ffff:10.44.0.1/95',),
        'H0076': ('224.0.0.1', '224.0.0.1/24', 'ff02::1', 'ff02::1/64'),
        'H0077': ('0.0.0.0', '0.0.0.0/32', '::', '::/128'),
    }
    client_requirements = {
        ident: tuple(('guards', 'R003/ip-' + repr(value), 400) for value in values)
        for ident, values in invalid_ranges.items()}
    translated = tuple(requirement for required in client_requirements.values() for requirement in required)
    translated += tuple(('guards', 'R003/ip-' + repr(value), 400)
                        for value in ('10.44.0.999', '1:2:3:4:5:6:7:8:9'))
    client_requirements['H0078'] = client_requirements['H0079'] = translated
    names = [None, 0, False, [], {}, 'x' * 81, '行🧪' * 40 + '尾', ' ' * 81]
    names += ['control' + chr(number) for number in range(32)] + ['del\x7f']
    client_requirements['H0068'] = tuple(('guards', 'R003/' + field + '-' + repr(value), 400)
        for field in ('name', 'previous_name') for value in names)
    client_requirements['H0023'] = tuple(('guards', 'R003/' + field + '-' + repr(value), 400)
        for field in ('mode', 'previous_mode') for value in (None, 0, False, [], {}, 'SMART', 'proxy'))
    client_requirements['H0024'] = (('guards', 'R003/previous_name-stale', 409),
                                   ('guards', 'R003/previous_mode-stale', 409))
    for ident, required in client_requirements.items():
        audits[ident] = Audit(
            ('backend/common/client_devices.py:28', 'backend/common/device_notes.py:18',
             'backend/common/oxidns_device_policy.py:22', 'backend/routes/client_devices.py:55'),
            'R003 public draft update reaches these helpers before transaction writes. Each compound type/length/'
            'character condition, both name fields, both mode fields, IPv4/IPv6 prefix limits, mapped-prefix lower '
            'bound, host/network multicast, unspecified hosts and actual ipaddress parse errors are required. '
            'The outer ValueError translation requires every family. Stale name and mode are separate409 variants. '
            'Name length is checked before trim; all32 C0 controls and DEL are individually rejected. '
            'Canonical accepted host/network/mapped forms and Unicode80 limits are separate positive controls; '
            'these guard audits do not claim policy graph rewriting, device I/O or all client route coverage.',
            required)
    policy_variants = {
        'H0080': tuple('shape-' + label for label in ('null', 'false', 'number', 'string', 'object', '513')),
        'H0081': tuple('row-' + label for label in ('null', 'false', 'number', 'string', 'array', 'extra-key')),
        'H0082': tuple('duplicate-' + label for label in ('host-prefix', 'mapped-host', 'ipv6-compression', 'network-mask')),
        'H0083': tuple('name-' + label for label in ('null', 'false', 'number', 'array', 'object',
                                                   'ascii81', 'unicode81', 'space81'))
                  + tuple('name-control' + str(number) for number in range(32)),
        'H0084': tuple('mode-' + label for label in ('null', 'false', 'number', 'array', 'object',
                                                   'empty', 'upper', 'proxy', 'missing')),
    }
    for ident, labels in policy_variants.items():
        audits[ident] = Audit(
            ('backend/common/oxidns_device_policy.py:54', 'backend/common/device_notes.py:36',
             'backend/common/client_devices.py:18', 'backend/routers/policy_workspace.py:18',
             'backend/common/config_repository.py:2577'),
            'Real public import permits these invalid legacy policy rows (unlike invalid device_notes). '
            'R002 read, R003 draft write and R014 workspace each reach the guard and return its exact400; '
            'full state remains unchanged, no device I/O. All type/length/member/duplicate/name/mode '
            'families require explicit variants, including every C0 control, canonical mapped/network '
            'duplicates and missing mode. DEL is accepted and removed only for legacy policy names, '
            'with a separate successful read/rename fixture; it is not confused with normalize_name. '
            '512 rows and80 Unicode characters are separate positive controls. These helper audits do '
            'not claim every policy graph mutation or all public router call sites.',
            tuple(('stored-policy-' + label, route + '/stored-policy-' + label, 400)
                  for label in labels for route in ('R002', 'R003', 'R014')))
    client_successes = {
        'S01': (('client-drafts', 'R003-S01/0'), ('notes-capacity', 'R003/notes-2048-freed-slot-accepted')),
        'S02': (('client-drafts', 'R003-S02/1'), ('client-drafts', 'R003-S02/3'),
                ('client-mode-notes', 'R003/mode-note-smart-with-name')),
        'S03': (('client-drafts', 'R003-S03/2'), ('client-mode-notes', 'R003/mode-note-create-direct')),
        'S04': (('client-drafts', 'R003-S04/4'),),
        'S05': (('client-drafts', 'R003-S05/6'), ('client-mode-notes', 'R003/mode-note-clear-mode-retains-same-name')),
        'S06': (('client-drafts', 'R003-S06/5'), ('client-mode-notes', 'R003/mode-note-empty-note-hides-legacy-name')),
        'S07': (('client-drafts', 'R003-S07/8'), ('client-canonical-names', 'R003/remove-note-with-normalized-previous'),
                ('client-mode-notes', 'R003/mode-note-clear-policy-removes-hidden-name'),
                ('notes-keep-reservation', 'R003/note-remove-forgets-binding-keeps-reservation')),
        'S08': (('client-drafts', 'R003-S08/7'), ('client-mode-notes', 'R003/mode-note-rename-preserves-native-and-legacy-name'),
                ('stored-policy-legacy', 'R003/stored-policy-legacy-rename-keeps-raw-rows'),
                ('notes-capacity', 'R003/notes-2048-replace-existing'),
                ('notes-keep-reservation', 'R003/note-rename-preserves-real-binding')),
    }
    for suffix, required in client_successes.items():
        audits['R003-' + suffix] = Audit(
            ('backend/routes/client_devices.py:55', 'backend/common/client_devices.py:39',
             'backend/common/device_notes.py:52'),
            'Actual public row transitions with own previous values: new smart/direct and transitions, '
            'name-only vs policy changes, legacy-name hiding/cleanup and Unicode/mapped address removal. '
            'S07 additionally requires a genuinely previewed/applied DHCP reservation: local binding is '
            'forgotten while the complete device state survives with zero device requests. S08 requires '
            'full native/settings/binding retention for normal direct policies, canonical legacy rows, '
            'the2048-note boundary and an actual DHCP binding. R015 stale names_revision vs modern '
            'editor success is tested separately, not inferred from the devices response.',
            tuple((world, name, 200) for world, name in required))
    return audits


def evaluate(results):
    declarations = declared()
    completed = {(row.get('world'), row['case']): row for row in results}
    verdicts = {}
    for ident, audit in declarations.items():
        missing = []
        for world, case, status in audit.requirements:
            row = completed.get((world, case))
            if (row is None or row.get('python', {}).get('status') != status
                    or ident not in row.get('entries', ())):
                missing.append(world + ':' + case)
        verdicts[ident] = {'audit': 'variants-pending' if missing else 'variants-audited',
                          'sources': list(audit.sources), 'rationale': audit.rationale,
                          'required_variants': [world + ':' + case for world, case, _ in audit.requirements],
                          'missing_variants': missing}
    return verdicts
